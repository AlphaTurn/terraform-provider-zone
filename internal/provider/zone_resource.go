package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/AlphaTurn/terraform-provider-zone/internal/zoneapi"
)

// NewZoneResource returns the zone settings resource.
func NewZoneResource() resource.Resource { return &zoneResource{} }

type zoneResource struct {
	client *zoneapi.Client
}

var (
	_ resource.Resource                = (*zoneResource)(nil)
	_ resource.ResourceWithConfigure   = (*zoneResource)(nil)
	_ resource.ResourceWithImportState = (*zoneResource)(nil)
)

type zoneResourceModel struct {
	Name        types.String `tfsdk:"name"`
	Active      types.Bool   `tfsdk:"active"`
	IPv6        types.Bool   `tfsdk:"ipv6"`
	DNSSEC      types.Bool   `tfsdk:"dnssec"`
	ResourceURL types.String `tfsdk:"resource_url"`
}

func (r *zoneResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_dns_zone"
}

func (r *zoneResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Settings of an existing DNS zone.\n\n" +
			"~> **This resource does not create or destroy zones.** The zone.eu API has no endpoint " +
			"for either: a zone appears when a domain or hosting service is bought and goes away with " +
			"it. Applying this resource adopts the zone that already exists and manages its settings; " +
			"destroying it stops managing those settings and leaves the zone in place.",
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Zone name, for example `example.com`. Changing this adopts a different zone.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"active": schema.BoolAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Whether zone.eu serves this zone.",
			},
			"ipv6": schema.BoolAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Whether zone.eu's IPv6 support is enabled for this zone.",
			},
			"dnssec": schema.BoolAttribute{
				Computed:            true,
				MarkdownDescription: "Whether the zone is DNSSEC signed. Managed by zone.eu, not by this provider.",
			},
			"resource_url": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "API URL of this zone.",
			},
		},
	}
}

func (r *zoneResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	client, ok := req.ProviderData.(*zoneapi.Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected provider data",
			fmt.Sprintf("Expected *zoneapi.Client but got %T. This is a bug in the provider.", req.ProviderData),
		)
		return
	}
	r.client = client
}

// Create adopts an existing zone. There is nothing to create on zone.eu's side,
// so this reads the zone to confirm it is there and then applies whichever
// settings were configured.
func (r *zoneResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan zoneResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	name := plan.Name.ValueString()
	current, err := r.client.GetZone(ctx, name)
	if err != nil {
		if zoneapi.IsNotFound(err) {
			resp.Diagnostics.AddAttributeError(
				path.Root("name"),
				"DNS zone not found",
				fmt.Sprintf("No DNS zone named %q is available on this ZoneID account.\n\n"+
					"Zones cannot be created through the API; one appears when the domain or hosting "+
					"service is bought. Check the name, and that the account has access to it.", name),
			)
			return
		}
		resp.Diagnostics.AddError("Could not read DNS zone", err.Error())
		return
	}

	zone, diags := r.applySettings(ctx, name, plan, current)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, modelFromZone(zone))...)
}

func (r *zoneResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state zoneResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	zone, err := r.client.GetZone(ctx, state.Name.ValueString())
	if err != nil {
		if zoneapi.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Could not read DNS zone", err.Error())
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, modelFromZone(zone))...)
}

func (r *zoneResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan zoneResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	name := plan.Name.ValueString()
	current, err := r.client.GetZone(ctx, name)
	if err != nil {
		resp.Diagnostics.AddError("Could not read DNS zone", err.Error())
		return
	}

	zone, diags := r.applySettings(ctx, name, plan, current)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, modelFromZone(zone))...)
}

// Delete stops managing the zone. The API offers no way to remove one, so this
// only drops it from state, and says so rather than appearing to have deleted
// something.
func (r *zoneResource) Delete(_ context.Context, _ resource.DeleteRequest, resp *resource.DeleteResponse) {
	resp.Diagnostics.AddWarning(
		"DNS zone left in place",
		"The zone.eu API cannot delete DNS zones, so the zone and its records still exist and are "+
			"still served. Terraform has only stopped managing its settings.",
	)
}

func (r *zoneResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), req.ID)...)
}

// applySettings writes the configured settings, skipping the request when the
// zone already matches. Unset arguments keep whatever the zone has, so adopting
// a zone without specifying anything changes nothing.
func (r *zoneResource) applySettings(ctx context.Context, name string, plan zoneResourceModel, current *zoneapi.Zone) (*zoneapi.Zone, diag.Diagnostics) {
	var diags diag.Diagnostics

	desired := zoneapi.ZoneSettings{Active: current.Active, IPv6: current.IPv6}
	if !plan.Active.IsNull() && !plan.Active.IsUnknown() {
		desired.Active = plan.Active.ValueBool()
	}
	if !plan.IPv6.IsNull() && !plan.IPv6.IsUnknown() {
		desired.IPv6 = plan.IPv6.ValueBool()
	}

	// With only 60 requests a minute to spend, a write that changes nothing is
	// worth skipping.
	if desired.Active == current.Active && desired.IPv6 == current.IPv6 {
		return current, diags
	}

	updated, err := r.client.UpdateZone(ctx, name, desired)
	if err != nil {
		diags.AddError("Could not update DNS zone settings", err.Error())
		return nil, diags
	}
	return updated, diags
}

func modelFromZone(zone *zoneapi.Zone) zoneResourceModel {
	return zoneResourceModel{
		Name:        types.StringValue(zone.Identificator),
		Active:      types.BoolValue(zone.Active),
		IPv6:        types.BoolValue(zone.IPv6),
		DNSSEC:      types.BoolValue(zone.DNSSEC),
		ResourceURL: types.StringValue(zone.ResourceURL),
	}
}
