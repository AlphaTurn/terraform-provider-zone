package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/AlphaTurn/terraform-provider-zone/internal/zoneapi"
)

// NewMySQLPermissionResource returns the MySQL grant resource.
func NewMySQLPermissionResource() resource.Resource { return &mysqlPermissionResource{} }

type mysqlPermissionResource struct {
	resourceClient
}

var (
	_ resource.Resource                = (*mysqlPermissionResource)(nil)
	_ resource.ResourceWithConfigure   = (*mysqlPermissionResource)(nil)
	_ resource.ResourceWithImportState = (*mysqlPermissionResource)(nil)
)

type mysqlPermissionResourceModel struct {
	ServiceName types.String `tfsdk:"service_name"`
	Username    types.String `tfsdk:"username"`
	Database    types.String `tfsdk:"database"`
	Permissions types.Set    `tfsdk:"permissions"`
	ResourceURL types.String `tfsdk:"resource_url"`
}

func (r *mysqlPermissionResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_mysql_permission"
}

func (r *mysqlPermissionResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "One MySQL account's access to one database.\n\n" +
			"This is its own resource rather than an argument on `zone_mysql_account` because that " +
			"is what it is: the API gives a grant its own endpoint, and revoking one drops neither " +
			"the account nor the database. Folding it into the account would also mean the account " +
			"resource had to reconcile children with their own endpoints, which produces a " +
			"perpetual diff the moment somebody grants access in phpMyAdmin.\n\n" +
			"~> **Destroying this revokes the grant only.** The account and the database both " +
			"survive.",
		Attributes: map[string]schema.Attribute{
			"service_name": serviceNameAttribute(),
			"username": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "The account being granted access. Changing it forces a new grant.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators:          []validator.String{noPathSeparator()},
			},
			"database": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "The database being granted on. Changing it forces a new grant.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators:          []validator.String{noPathSeparator()},
			},
			"permissions": schema.SetAttribute{
				Required:    true,
				ElementType: types.StringType,
				MarkdownDescription: "The privileges to grant, for example `SELECT`, `INSERT`, " +
					"`UPDATE`, `DELETE`. zone.eu publishes no list of accepted values in its API " +
					"description, so these are not validated locally; the authoritative list comes " +
					"from `OPTIONS /vserver/{service}/database/mysql/account/{username}/permission`, " +
					"and an unaccepted value is reported against this argument.",
				Validators: []validator.Set{setvalidator.SizeAtLeast(1)},
			},
			"resource_url": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "API URL of this grant.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

// Create refuses to adopt a grant that already exists.
//
// The endpoint has no POST: creating and updating a grant are the same PUT, so
// a create cannot tell "granted" from "silently replaced somebody else's
// privileges". Checking first costs one request and turns a silent overwrite
// into an instruction to import, which is the same stance zone_dns_zone takes
// on adopting a zone.
func (r *mysqlPermissionResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan mysqlPermissionResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	service := plan.ServiceName.ValueString()
	username := plan.Username.ValueString()
	database := plan.Database.ValueString()

	existing, err := r.client.GetMySQLPermission(ctx, service, username, database)
	switch {
	case err == nil && len(existing.Permissions) > 0:
		resp.Diagnostics.AddError(
			"Grant already exists",
			fmt.Sprintf(
				"%q already has privileges on %q, and this resource would replace them without "+
					"Terraform ever having seen what they were.\n\n"+
					"Import the existing grant instead:\n\n"+
					"    terraform import <this resource> \"%s/%s/%s\"",
				username, database, service, username, database,
			),
		)
		return
	case err != nil && !zoneapi.IsNotFound(err):
		addAPIError(&resp.Diagnostics, "Could not check for an existing grant", err, mysqlPermissionAttributes)
		return
	}

	r.write(ctx, plan, &resp.State, &resp.Diagnostics)
}

func (r *mysqlPermissionResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state mysqlPermissionResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	service := state.ServiceName.ValueString()
	grant, err := r.client.GetMySQLPermission(ctx, service, state.Username.ValueString(), state.Database.ValueString())
	if err != nil {
		if zoneapi.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		addAPIError(&resp.Diagnostics, "Could not read the grant", err, mysqlPermissionAttributes)
		return
	}
	if len(grant.Permissions) == 0 {
		// An empty privilege set is how a revoked grant reads: the endpoint
		// answers, but there is nothing granted.
		resp.State.RemoveResource(ctx)
		return
	}

	permissions, diags := stringSetValue(grant.Permissions)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, mysqlPermissionResourceModel{
		ServiceName: types.StringValue(service),
		Username:    types.StringValue(grant.Username),
		Database:    types.StringValue(grant.Database),
		Permissions: permissions,
		ResourceURL: optionalString(grant.ResourceURL),
	})...)
}

func (r *mysqlPermissionResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan mysqlPermissionResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	r.write(ctx, plan, &resp.State, &resp.Diagnostics)
}

func (r *mysqlPermissionResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state mysqlPermissionResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteMySQLPermission(
		ctx, state.ServiceName.ValueString(), state.Username.ValueString(), state.Database.ValueString(),
	)
	if err != nil {
		addAPIError(&resp.Diagnostics, "Could not revoke the grant", err, mysqlPermissionAttributes)
	}
}

// ImportState accepts "<service name>/<username>/<database>".
func (r *mysqlPermissionResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts, ok := importParts(
		&resp.Diagnostics, req.ID, 3,
		"<service name>/<username>/<database>", "virt1.example.com/d12345_app/d12345_appdb",
	)
	if !ok {
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("service_name"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("username"), parts[1])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("database"), parts[2])...)
}

var mysqlPermissionAttributes = map[string]string{
	"permissions": "permissions",
	"username":    "username",
	"database":    "database",
}

// write is the single PUT that both Create and Update perform.
func (r *mysqlPermissionResource) write(
	ctx context.Context, plan mysqlPermissionResourceModel, state *tfsdk.State, diags *diag.Diagnostics,
) {
	permissions := setToStrings(ctx, plan.Permissions, diags)
	if diags.HasError() {
		return
	}

	service := plan.ServiceName.ValueString()
	stored, err := r.client.SetMySQLPermission(
		ctx, service, plan.Username.ValueString(), plan.Database.ValueString(),
		zoneapi.MySQLPermissionInput{Permissions: permissions},
	)
	if err != nil {
		addAPIError(diags, "Could not write the grant", err, mysqlPermissionAttributes)
		return
	}

	// State records what was asked for: the API does not promise to echo the
	// privilege names in the case they were sent.
	diags.Append(state.Set(ctx, mysqlPermissionResourceModel{
		ServiceName: types.StringValue(service),
		Username:    types.StringValue(plan.Username.ValueString()),
		Database:    types.StringValue(plan.Database.ValueString()),
		Permissions: plan.Permissions,
		ResourceURL: optionalString(stored.ResourceURL),
	})...)
}
