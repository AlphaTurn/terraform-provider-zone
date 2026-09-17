package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/AlphaTurn/terraform-provider-zone/internal/zoneapi"
)

// NewMySQLDatabaseResource returns the MySQL database resource.
func NewMySQLDatabaseResource() resource.Resource { return &mysqlDatabaseResource{} }

type mysqlDatabaseResource struct {
	resourceClient
}

var (
	_ resource.Resource                = (*mysqlDatabaseResource)(nil)
	_ resource.ResourceWithConfigure   = (*mysqlDatabaseResource)(nil)
	_ resource.ResourceWithImportState = (*mysqlDatabaseResource)(nil)
)

type mysqlDatabaseResourceModel struct {
	ServiceName      types.String `tfsdk:"service_name"`
	Name             types.String `tfsdk:"name"`
	Comment          types.String `tfsdk:"comment"`
	Collation        types.String `tfsdk:"collation"`
	DiskUsage        types.String `tfsdk:"disk_usage"`
	DiskUsageUpdated types.String `tfsdk:"disk_usage_updated"`
	ResourceURL      types.String `tfsdk:"resource_url"`
}

func (r *mysqlDatabaseResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_mysql_database"
}

func (r *mysqlDatabaseResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	// This warning is not boilerplate. The API has no PUT for a database at
	// all, so every configurable argument forces replacement, and replacing a
	// database means dropping it.
	const destructiveNote = "!> **Every change to this resource destroys the database and all its " +
		"data.** The API has no endpoint for modifying a database — not even its comment — so " +
		"Terraform can only replace one, and replacing means `DROP DATABASE`. Add a " +
		"`lifecycle { prevent_destroy = true }` block to anything you care about, and take a dump " +
		"before changing a comment."

	resp.Schema = schema.Schema{
		MarkdownDescription: "A MySQL database on a webhosting service.\n\n" + destructiveNote,
		Attributes: map[string]schema.Attribute{
			"service_name": serviceNameAttribute(),
			"name": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "Database name. zone.eu prefixes these per service, so this is " +
					"usually of the form `d12345_appname`; use the name the panel or the API shows.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators:    []validator.String{noPathSeparator()},
			},
			"comment": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "A note on the database, shown in the ZoneID panel. " +
					"**Changing this replaces the database and loses its data**, because the API " +
					"offers no way to modify one.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"collation": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "Collation for the new database, for example " +
					"`utf8mb4_unicode_ci`. Can only be chosen at creation and is never returned by " +
					"the API, so Terraform keeps what was last applied. zone.eu's default is " +
					"`utf8mb4_unicode_ci`; the full list of several hundred is available from " +
					"`OPTIONS /vserver/{service}/database/mysql`.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"disk_usage": schema.StringAttribute{
				Computed: true,
				MarkdownDescription: "Bytes the database occupies. A string, because that is what " +
					"the API returns here, unlike the numeric usage on a mail account.",
			},
			"disk_usage_updated": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "When the usage figure was last measured.",
			},
			"resource_url": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "API URL of this database.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *mysqlDatabaseResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan mysqlDatabaseResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	service := plan.ServiceName.ValueString()
	created, err := r.client.CreateMySQLDatabase(ctx, service, zoneapi.MySQLDatabaseInput{
		Name:      plan.Name.ValueString(),
		Comment:   plan.Comment.ValueString(),
		Collation: plan.Collation.ValueString(),
	})
	if err != nil {
		addAPIError(&resp.Diagnostics, "Could not create the database", err, mysqlDatabaseAttributes)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, modelFromMySQLDatabase(service, created, plan))...)
}

func (r *mysqlDatabaseResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state mysqlDatabaseResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	service := state.ServiceName.ValueString()
	database, err := r.client.GetMySQLDatabase(ctx, service, state.Name.ValueString())
	if err != nil {
		if zoneapi.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		addAPIError(&resp.Diagnostics, "Could not read the database", err, mysqlDatabaseAttributes)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, modelFromMySQLDatabase(service, database, state))...)
}

// Update cannot happen: every writable attribute forces replacement. It exists
// because the interface requires it, and says so rather than silently doing
// nothing.
func (r *mysqlDatabaseResource) Update(_ context.Context, _ resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError(
		"Databases cannot be modified",
		"The zone.eu API has no endpoint for changing a MySQL database, so every argument on this "+
			"resource forces replacement and this code path should be unreachable.\n\n"+
			"Reaching it is a bug in the provider; please report it.",
	)
}

func (r *mysqlDatabaseResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state mysqlDatabaseResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteMySQLDatabase(ctx, state.ServiceName.ValueString(), state.Name.ValueString())
	if err != nil {
		addAPIError(&resp.Diagnostics, "Could not drop the database", err, mysqlDatabaseAttributes)
	}
}

// ImportState accepts "<service name>/<database name>".
func (r *mysqlDatabaseResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts, ok := importParts(&resp.Diagnostics, req.ID, 2, "<service name>/<database name>", "virt1.example.com/d12345_app")
	if !ok {
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("service_name"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), parts[1])...)
}

var mysqlDatabaseAttributes = map[string]string{
	"name":      "name",
	"comment":   "comment",
	"collation": "collation",
}

func modelFromMySQLDatabase(
	service string, database *zoneapi.MySQLDatabase, desired mysqlDatabaseResourceModel,
) mysqlDatabaseResourceModel {
	comment := optionalString(database.Comment)
	if database.Comment == "" && known(desired.Comment) {
		comment = desired.Comment
	}

	return mysqlDatabaseResourceModel{
		ServiceName: types.StringValue(service),
		Name:        types.StringValue(database.Name),
		Comment:     comment,
		// Never returned by the API, so it can only come from configuration.
		Collation:        desired.Collation,
		DiskUsage:        optionalString(database.DiskUsage),
		DiskUsageUpdated: optionalString(database.DiskUsageUpdated),
		ResourceURL:      optionalString(database.ResourceURL),
	}
}
