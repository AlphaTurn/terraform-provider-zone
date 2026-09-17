package provider

import (
	"context"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/AlphaTurn/terraform-provider-zone/internal/zoneapi"
)

// NewFTPUserResource returns the FTP account resource.
func NewFTPUserResource() resource.Resource { return &ftpUserResource{} }

type ftpUserResource struct {
	resourceClient
}

var (
	_ resource.Resource                = (*ftpUserResource)(nil)
	_ resource.ResourceWithConfigure   = (*ftpUserResource)(nil)
	_ resource.ResourceWithImportState = (*ftpUserResource)(nil)
)

type ftpUserResourceModel struct {
	ServiceName       types.String `tfsdk:"service_name"`
	ID                types.Int64  `tfsdk:"id"`
	Username          types.String `tfsdk:"username"`
	UsernameSystem    types.String `tfsdk:"username_system"`
	Password          types.String `tfsdk:"password"`
	PasswordVersion   types.Int64  `tfsdk:"password_version"`
	Directory         types.String `tfsdk:"directory"`
	RequireTLS        types.Bool   `tfsdk:"require_tls"`
	AccessProfile     types.String `tfsdk:"access_profile"`
	AllowedOperations types.Set    `tfsdk:"allowed_operations"`
	AccessCountries   types.Set    `tfsdk:"access_countries"`
	ResourceURL       types.String `tfsdk:"resource_url"`
}

// ftpAllowedOperations is the set of operations the live API accepts, from
// OPTIONS /vserver/{service}/ftp/user. The published description declares no
// enum for this field at all.
var ftpAllowedOperations = []string{
	"allow_download", "allow_upload", "allow_delete", "allow_rename", "allow_chmod",
	"allow_mkdir", "allow_rmdir", "allow_chdir", "allow_list",
	"allow_upload_new", "allow_upload_resume", "allow_upload_overwrite", "allow_upload_viruses",
}

func (r *ftpUserResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_ftp_user"
}

func (r *ftpUserResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "An FTP account on a webhosting service.\n\n" + writeOnlyNote,
		Attributes: map[string]schema.Attribute{
			"service_name": serviceNameAttribute(),
			"id": schema.Int64Attribute{
				Computed:            true,
				MarkdownDescription: "Numeric identifier assigned by zone.eu.",
				PlanModifiers:       []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
			},
			"username": schema.StringAttribute{
				Optional: true,
				Computed: true,
				MarkdownDescription: "A custom username for the account, used as an alias for the " +
					"generated one. Leave it unset to log in with `username_system`.",
				Validators: []validator.String{noPathSeparator()},
			},
			"username_system": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The username zone.eu generated. Always valid for logging in.",
			},
			"password":         passwordAttribute("The account's password."),
			"password_version": passwordVersionAttribute(),
			"directory": schema.StringAttribute{
				Optional: true,
				Computed: true,
				MarkdownDescription: "The directory the account is confined to, as an absolute path " +
					"on the server. Read it off the `homedir` of the `zone_vservers` data source.",
			},
			"require_tls": schema.BoolAttribute{
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(true),
				MarkdownDescription: "Whether the account may only connect over TLS.",
			},
			"access_profile": schema.StringAttribute{
				Optional: true,
				Computed: true,
				MarkdownDescription: "How access is restricted: `whitelist` to allow only the " +
					"addresses in `zone_ftp_ip_whitelist`, `whitelist_or_tls` to also allow TLS " +
					"connections from anywhere, or `unsafe` to allow anything — including plain FTP, " +
					"which sends the password in clear text.",
				Validators: []validator.String{
					stringvalidator.OneOf("whitelist", "whitelist_or_tls", "unsafe"),
				},
			},
			"allowed_operations": schema.SetAttribute{
				Optional:    true,
				Computed:    true,
				ElementType: types.StringType,
				MarkdownDescription: "What the account may do. Accepted values are " +
					"`allow_download`, `allow_upload`, `allow_delete`, `allow_rename`, " +
					"`allow_chmod`, `allow_mkdir`, `allow_rmdir`, `allow_chdir`, `allow_list`, " +
					"`allow_upload_new`, `allow_upload_resume`, `allow_upload_overwrite` and " +
					"`allow_upload_viruses`.",
				Validators: []validator.Set{
					setvalidator.ValueStringsAre(stringvalidator.OneOf(ftpAllowedOperations...)),
				},
			},
			"access_countries": schema.SetAttribute{
				Optional:    true,
				Computed:    true,
				ElementType: types.StringType,
				MarkdownDescription: "Two-letter country codes the account may connect from. The " +
					"accepted list is every ISO country code, so it is not validated here; an " +
					"unaccepted value is reported against this argument.",
			},
			"resource_url": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "API URL of this account.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *ftpUserResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan ftpUserResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	input, diags := ftpUserInput(ctx, plan, req.Config)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	service := plan.ServiceName.ValueString()
	created, err := r.client.CreateFTPUser(ctx, service, input)
	if err != nil {
		addAPIError(&resp.Diagnostics, "Could not create the FTP account", err, ftpUserAttributes)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, modelFromFTPUser(service, created, plan))...)
}

func (r *ftpUserResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state ftpUserResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	service := state.ServiceName.ValueString()

	var user *zoneapi.FTPUser
	var err error
	if known(state.ID) && state.ID.ValueInt64() != 0 {
		user, err = r.client.GetFTPUser(ctx, service, state.ID.ValueInt64())
	} else {
		user, err = r.client.FindFTPUser(ctx, service, state.Username.ValueString())
	}
	if err != nil {
		if zoneapi.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		addAPIError(&resp.Diagnostics, "Could not read the FTP account", err, ftpUserAttributes)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, modelFromFTPUser(service, user, state))...)
}

func (r *ftpUserResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state ftpUserResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	input, diags := ftpUserInput(ctx, plan, req.Config)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	service := plan.ServiceName.ValueString()
	updated, err := r.client.UpdateFTPUser(ctx, service, state.ID.ValueInt64(), input)
	if err != nil {
		addAPIError(&resp.Diagnostics, "Could not update the FTP account", err, ftpUserAttributes)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, modelFromFTPUser(service, updated, plan))...)
}

func (r *ftpUserResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state ftpUserResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteFTPUser(ctx, state.ServiceName.ValueString(), state.ID.ValueInt64())
	if err != nil {
		addAPIError(&resp.Diagnostics, "Could not delete the FTP account", err, ftpUserAttributes)
	}
}

// ImportState accepts "<service name>/<account id>" or "<service name>/<username>".
func (r *ftpUserResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	service, key, ok := importKey(
		&resp.Diagnostics, req.ID,
		"<service name>/<account id or username>", "virt1.example.com/deploy",
	)
	if !ok {
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("service_name"), service)...)
	if id, err := strconv.ParseInt(key, 10, 64); err == nil {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id)...)
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("username"), key)...)
}

var ftpUserAttributes = map[string]string{
	"username":           "username",
	"password":           "password",
	"directory":          "directory",
	"require_tls":        "require_tls",
	"access_profile":     "access_profile",
	"allowed_operations": "allowed_operations",
	"access_countries":   "access_countries",
}

func ftpUserInput(ctx context.Context, plan ftpUserResourceModel, config configSource) (zoneapi.FTPUserInput, diag.Diagnostics) {
	var diags diag.Diagnostics

	password := writeOnlyString(ctx, config, "password", &diags)
	if diags.HasError() {
		return zoneapi.FTPUserInput{}, diags
	}

	return zoneapi.FTPUserInput{
		Username:          plan.Username.ValueString(),
		Password:          password,
		Directory:         plan.Directory.ValueString(),
		RequireTLS:        plan.RequireTLS.ValueBool(),
		AccessProfile:     plan.AccessProfile.ValueString(),
		AllowedOperations: setToStrings(ctx, plan.AllowedOperations, &diags),
		AccessCountries:   setToStrings(ctx, plan.AccessCountries, &diags),
	}, diags
}

func modelFromFTPUser(service string, user *zoneapi.FTPUser, desired ftpUserResourceModel) ftpUserResourceModel {
	operations, _ := stringSetValue(user.AllowedOperations)
	if user.AllowedOperations == nil && known(desired.AllowedOperations) {
		operations = desired.AllowedOperations
	}
	countries, _ := stringSetValue(user.AccessCountries)
	if user.AccessCountries == nil && known(desired.AccessCountries) {
		countries = desired.AccessCountries
	}

	username := optionalString(user.Username)
	if user.Username == "" && known(desired.Username) {
		username = desired.Username
	}

	return ftpUserResourceModel{
		ServiceName:    types.StringValue(service),
		ID:             types.Int64Value(user.ID),
		Username:       username,
		UsernameSystem: optionalString(user.UsernameSystem),
		// Write-only: never read back, never stored.
		Password:          types.StringNull(),
		PasswordVersion:   desired.PasswordVersion,
		Directory:         optionalString(user.Directory),
		RequireTLS:        types.BoolValue(user.RequireTLS),
		AccessProfile:     optionalString(user.AccessProfile),
		AllowedOperations: operations,
		AccessCountries:   countries,
		ResourceURL:       optionalString(user.ResourceURL),
	}
}
