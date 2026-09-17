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
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/AlphaTurn/terraform-provider-zone/internal/zoneapi"
)

// NewDomainResource returns the domain settings resource.
func NewDomainResource() resource.Resource { return &domainResource{} }

type domainResource struct {
	resourceClient
}

var (
	_ resource.Resource                = (*domainResource)(nil)
	_ resource.ResourceWithConfigure   = (*domainResource)(nil)
	_ resource.ResourceWithImportState = (*domainResource)(nil)
)

type domainResourceModel struct {
	Name                 types.String `tfsdk:"name"`
	RenewalNotifications types.Bool   `tfsdk:"renewal_notifications"`
	SigningRequired      types.Bool   `tfsdk:"signing_required"`
	NameserversCustom    types.Bool   `tfsdk:"nameservers_custom"`
	Expires              types.String `tfsdk:"expires"`
	Expired              types.Bool   `tfsdk:"expired"`
	DNSSEC               types.Bool   `tfsdk:"dnssec"`
	DNSSECSupported      types.Bool   `tfsdk:"dnssec_supported"`
	Autorenew            types.Bool   `tfsdk:"autorenew"`
	Delegated            types.String `tfsdk:"delegated"`
	RenewOrder           types.String `tfsdk:"renew_order"`
	HasPendingTrade      types.Int64  `tfsdk:"has_pending_trade"`
	HasPendingDNSSEC     types.Bool   `tfsdk:"has_pending_dnssec"`
	Reactivate           types.Bool   `tfsdk:"reactivate"`
	AuthKeyEnabled       types.Bool   `tfsdk:"auth_key_enabled"`
	ResourceURL          types.String `tfsdk:"resource_url"`
}

func (r *domainResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_domain"
}

func (r *domainResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Settings of a registered domain.\n\n" +
			"This is a settings resource, not a registration resource: the API exposes a domain's " +
			"renewal and signing preferences, and nothing that would buy, transfer or release one. " +
			"Almost every field below is therefore read-only.\n\n" +
			adoptNote + "\n\n" + unverifiedWriteNote,
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "Domain name, for example `example.com`. Changing this adopts a " +
					"different domain.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators:    []validator.String{noPathSeparator()},
			},
			"renewal_notifications": schema.BoolAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Whether zone.eu sends renewal reminders for this domain.",
			},
			"signing_required": schema.BoolAttribute{
				Optional: true,
				MarkdownDescription: "Whether DNSSEC signing is required for this domain.\n\n" +
					"The API accepts this but never returns it, so it cannot be refreshed: Terraform " +
					"keeps whatever was last applied and will not notice a change made in the ZoneID " +
					"panel. Leave it unset to not manage it at all.",
			},
			"nameservers_custom": schema.BoolAttribute{
				Computed: true,
				MarkdownDescription: "Whether the domain uses nameservers other than zone.eu's. " +
					"Read-only here: the API documents that only `false` may be written, and the " +
					"delegation itself is managed with `zone_domain_nameservers`.",
			},
			"expires": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "When the registration expires, as an ISO 8601 timestamp.",
			},
			"expired": schema.BoolAttribute{
				Computed:            true,
				MarkdownDescription: "Whether the registration has lapsed.",
			},
			"dnssec": schema.BoolAttribute{
				Computed:            true,
				MarkdownDescription: "Whether the domain is DNSSEC signed.",
			},
			"dnssec_supported": schema.BoolAttribute{
				Computed:            true,
				MarkdownDescription: "Whether the registry supports DNSSEC for this TLD.",
			},
			"autorenew": schema.BoolAttribute{
				Computed:            true,
				MarkdownDescription: "Whether zone.eu renews the domain automatically. Set in the ZoneID panel.",
			},
			"delegated": schema.StringAttribute{
				Computed: true,
				MarkdownDescription: "The ZoneID username this domain is delegated to, or null when it " +
					"is not delegated.",
			},
			"renew_order": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The pending renewal order, or null when there is none.",
			},
			"has_pending_trade": schema.Int64Attribute{
				Computed:            true,
				MarkdownDescription: "Identifier of an in-progress trade, or null when there is none.",
			},
			"has_pending_dnssec": schema.BoolAttribute{
				Computed:            true,
				MarkdownDescription: "Whether a DNSSEC change is still being applied at the registry.",
			},
			"reactivate": schema.BoolAttribute{
				Computed:            true,
				MarkdownDescription: "Whether an expired domain can still be reactivated.",
			},
			"auth_key_enabled": schema.BoolAttribute{
				Computed:            true,
				MarkdownDescription: "Whether this TLD uses an authorisation key for transfers.",
			},
			"resource_url": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "API URL of this domain.",
			},
		},
	}
}

// Create adopts a domain that already exists, then applies whatever settings
// were configured.
func (r *domainResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan domainResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	name := plan.Name.ValueString()
	current, err := r.client.GetDomain(ctx, name)
	if err != nil {
		if zoneapi.IsNotFound(err) {
			resp.Diagnostics.AddAttributeError(
				path.Root("name"),
				"Domain not found",
				fmt.Sprintf("No domain named %q is available on this ZoneID account.\n\n"+
					"Domains cannot be registered through this API; one appears when it is bought or "+
					"transferred in. Check the name, and that the account has access to it.", name),
			)
			return
		}
		addAPIError(&resp.Diagnostics, "Could not read domain", err, domainAttributes)
		return
	}

	domain, diags := r.applySettings(ctx, name, plan, current)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, modelFromDomain(domain, plan.SigningRequired))...)
}

func (r *domainResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state domainResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	domain, err := r.client.GetDomain(ctx, state.Name.ValueString())
	if err != nil {
		if zoneapi.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		addAPIError(&resp.Diagnostics, "Could not read domain", err, domainAttributes)
		return
	}

	// signing_required is carried over from prior state: the API accepts it and
	// never echoes it, so a refresh has nothing to say about it.
	resp.Diagnostics.Append(resp.State.Set(ctx, modelFromDomain(domain, state.SigningRequired))...)
}

func (r *domainResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan domainResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	name := plan.Name.ValueString()
	current, err := r.client.GetDomain(ctx, name)
	if err != nil {
		addAPIError(&resp.Diagnostics, "Could not read domain", err, domainAttributes)
		return
	}

	domain, diags := r.applySettings(ctx, name, plan, current)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, modelFromDomain(domain, plan.SigningRequired))...)
}

// Delete stops managing the domain. The API cannot release a registration, and
// pretending otherwise would be the most alarming possible lie.
func (r *domainResource) Delete(_ context.Context, _ resource.DeleteRequest, resp *resource.DeleteResponse) {
	resp.Diagnostics.AddWarning(
		"Domain left registered",
		"The zone.eu API cannot release or delete a domain registration, so the domain is still "+
			"registered and still resolves. Terraform has only stopped managing its settings.",
	)
}

func (r *domainResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), req.ID)...)
}

// applySettings writes the configured settings, skipping the request when
// nothing would change. Unset arguments leave the domain as it is.
func (r *domainResource) applySettings(
	ctx context.Context, name string, plan domainResourceModel, current *zoneapi.Domain,
) (*zoneapi.Domain, diag.Diagnostics) {
	var diags diag.Diagnostics

	var settings zoneapi.DomainSettings
	if known(plan.RenewalNotifications) && plan.RenewalNotifications.ValueBool() != current.RenewalNotifications {
		value := plan.RenewalNotifications.ValueBool()
		settings.RenewalNotifications = &value
	}
	if known(plan.SigningRequired) {
		// There is nothing to compare against, since the API never reports it,
		// so this is always sent when it is configured.
		value := plan.SigningRequired.ValueBool()
		settings.SigningRequired = &value
	}

	// With only 60 requests a minute to spend, a write that changes nothing is
	// worth skipping.
	if settings.RenewalNotifications == nil && settings.SigningRequired == nil {
		return current, diags
	}

	updated, err := r.client.UpdateDomain(ctx, name, settings)
	if err != nil {
		addAPIError(&diags, "Could not update domain settings", err, domainAttributes)
		return nil, diags
	}
	return updated, diags
}

// domainAttributes maps the API's field names onto this schema's attributes, so
// a validation failure lands on the argument that caused it.
var domainAttributes = map[string]string{
	"renewal_notifications": "renewal_notifications",
	"signing_required":      "signing_required",
	"nameservers_custom":    "nameservers_custom",
}

func modelFromDomain(domain *zoneapi.Domain, signingRequired types.Bool) domainResourceModel {
	return domainResourceModel{
		Name:                 types.StringValue(domain.Name),
		RenewalNotifications: types.BoolValue(domain.RenewalNotifications),
		SigningRequired:      signingRequired,
		NameserversCustom:    types.BoolValue(domain.NameserversCustom),
		Expires:              optionalString(domain.Expires),
		Expired:              types.BoolValue(domain.Expired),
		DNSSEC:               types.BoolValue(domain.DNSSEC),
		DNSSECSupported:      types.BoolValue(domain.DNSSECSupported),
		Autorenew:            types.BoolValue(domain.Autorenew),
		Delegated:            stringPointerValue(domain.Delegated),
		RenewOrder:           stringPointerValue(domain.RenewOrder),
		HasPendingTrade:      int64PointerValue(domain.HasPendingTrade),
		HasPendingDNSSEC:     types.BoolValue(domain.HasPendingDNSSEC),
		Reactivate:           types.BoolValue(domain.Reactivate),
		AuthKeyEnabled:       types.BoolValue(domain.AuthKeyEnabled),
		ResourceURL:          optionalString(domain.ResourceURL),
	}
}
