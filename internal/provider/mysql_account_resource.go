package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/AlphaTurn/terraform-provider-zone/internal/zoneapi"
)

// NewMySQLAccountResource returns the MySQL account resource.
func NewMySQLAccountResource() resource.Resource { return &mysqlAccountResource{} }

type mysqlAccountResource struct {
	resourceClient
}

var (
	_ resource.Resource                = (*mysqlAccountResource)(nil)
	_ resource.ResourceWithConfigure   = (*mysqlAccountResource)(nil)
	_ resource.ResourceWithImportState = (*mysqlAccountResource)(nil)
)

type mysqlAccountResourceModel struct {
	ServiceName     types.String `tfsdk:"service_name"`
	Username        types.String `tfsdk:"username"`
	Comment         types.String `tfsdk:"comment"`
	Password        types.String `tfsdk:"password"`
	PasswordVersion types.Int64  `tfsdk:"password_version"`
	RequireSSL      types.Bool   `tfsdk:"require_ssl"`
	Hosts           types.Set    `tfsdk:"hosts"`
	ResourceURL     types.String `tfsdk:"resource_url"`
}

func (r *mysqlAccountResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_mysql_account"
}

func (r *mysqlAccountResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A MySQL user on a webhosting service.\n\n" +
			"An account has no access to any database until a `zone_mysql_permission` grants it.\n\n" +
			writeOnlyNote,
		Attributes: map[string]schema.Attribute{
			"service_name": serviceNameAttribute(),
			"username": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "Username. zone.eu prefixes these per service, so this is " +
					"usually of the form `d12345_appname`. The API treats it as read-only on " +
					"update, so changing it forces a new account.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators:    []validator.String{noPathSeparator()},
			},
			"comment":          schema.StringAttribute{Optional: true, MarkdownDescription: "A note on the account, shown in the ZoneID panel."},
			"password":         passwordAttribute("The account's password."),
			"password_version": passwordVersionAttribute(),
			"require_ssl": schema.BoolAttribute{
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(false),
				MarkdownDescription: "Whether the account may only connect over TLS.",
			},
			"hosts": schema.SetAttribute{
				Optional:    true,
				Computed:    true,
				ElementType: types.StringType,
				MarkdownDescription: "Where the account may connect from. As well as IP addresses, " +
					"zone.eu accepts three special values: `ws` for the service's own web servers, " +
					"`pma` for phpMyAdmin and `vpn` for zone.eu's VPN. An account with no hosts " +
					"cannot connect from anywhere.",
			},
			"resource_url": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "API URL of this account.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *mysqlAccountResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan mysqlAccountResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	input, diags := mysqlAccountInput(ctx, plan, req.Config)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	service := plan.ServiceName.ValueString()
	created, err := r.client.CreateMySQLAccount(ctx, service, input)
	if err != nil {
		addAPIError(&resp.Diagnostics, "Could not create the database account", err, mysqlAccountAttributes)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, modelFromMySQLAccount(service, created, plan))...)
}

func (r *mysqlAccountResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state mysqlAccountResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	service := state.ServiceName.ValueString()
	account, err := r.client.GetMySQLAccount(ctx, service, state.Username.ValueString())
	if err != nil {
		if zoneapi.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		addAPIError(&resp.Diagnostics, "Could not read the database account", err, mysqlAccountAttributes)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, modelFromMySQLAccount(service, account, state))...)
}

func (r *mysqlAccountResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan mysqlAccountResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	input, diags := mysqlAccountInput(ctx, plan, req.Config)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	service := plan.ServiceName.ValueString()
	updated, err := r.client.UpdateMySQLAccount(ctx, service, plan.Username.ValueString(), input)
	if err != nil {
		addAPIError(&resp.Diagnostics, "Could not update the database account", err, mysqlAccountAttributes)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, modelFromMySQLAccount(service, updated, plan))...)
}

func (r *mysqlAccountResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state mysqlAccountResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteMySQLAccount(ctx, state.ServiceName.ValueString(), state.Username.ValueString())
	if err != nil {
		addAPIError(&resp.Diagnostics, "Could not delete the database account", err, mysqlAccountAttributes)
	}
}

// ImportState accepts "<service name>/<username>".
//
// An imported account has no password in state, because no state ever holds
// one. The next apply sends whatever the configuration supplies.
func (r *mysqlAccountResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts, ok := importParts(&resp.Diagnostics, req.ID, 2, "<service name>/<username>", "virt1.example.com/d12345_app")
	if !ok {
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("service_name"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("username"), parts[1])...)
}

var mysqlAccountAttributes = map[string]string{
	"username":    "username",
	"comment":     "comment",
	"password":    "password",
	"require_ssl": "require_ssl",
	"hosts":       "hosts",
}

func mysqlAccountInput(ctx context.Context, plan mysqlAccountResourceModel, config configSource) (zoneapi.MySQLAccountInput, diag.Diagnostics) {
	var diags diag.Diagnostics

	password := writeOnlyString(ctx, config, "password", &diags)
	if diags.HasError() {
		return zoneapi.MySQLAccountInput{}, diags
	}

	return zoneapi.MySQLAccountInput{
		Username:   plan.Username.ValueString(),
		Comment:    plan.Comment.ValueString(),
		Password:   password,
		RequireSSL: plan.RequireSSL.ValueBool(),
		Hosts:      setToStrings(ctx, plan.Hosts, &diags),
	}, diags
}

func modelFromMySQLAccount(
	service string, account *zoneapi.MySQLAccount, desired mysqlAccountResourceModel,
) mysqlAccountResourceModel {
	hosts, _ := stringSetValue(account.Hosts)
	if account.Hosts == nil && known(desired.Hosts) {
		hosts = desired.Hosts
	}

	comment := optionalString(account.Comment)
	if account.Comment == "" && known(desired.Comment) {
		comment = desired.Comment
	}

	return mysqlAccountResourceModel{
		ServiceName: types.StringValue(service),
		Username:    types.StringValue(account.Username),
		Comment:     comment,
		// Write-only: never read back, never stored.
		Password:        types.StringNull(),
		PasswordVersion: desired.PasswordVersion,
		RequireSSL:      types.BoolValue(account.RequireSSL),
		Hosts:           hosts,
		ResourceURL:     optionalString(account.ResourceURL),
	}
}
