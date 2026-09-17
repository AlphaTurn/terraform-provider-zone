package provider

import (
	"context"
	"slices"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/AlphaTurn/terraform-provider-zone/internal/zoneapi"
)

// NewDomainsDataSource returns the domains data source.
func NewDomainsDataSource() datasource.DataSource { return &domainsDataSource{} }

type domainsDataSource struct {
	dataSourceClient
}

var (
	_ datasource.DataSource              = (*domainsDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*domainsDataSource)(nil)
)

// domainAttributeTypes is the shape of one element of the domains list.
var domainAttributeTypes = map[string]attr.Type{
	"name":                  types.StringType,
	"expires":               types.StringType,
	"expired":               types.BoolType,
	"autorenew":             types.BoolType,
	"dnssec":                types.BoolType,
	"dnssec_supported":      types.BoolType,
	"renewal_notifications": types.BoolType,
	"nameservers_custom":    types.BoolType,
	"reactivate":            types.BoolType,
	"has_pending_dnssec":    types.BoolType,
	"auth_key_enabled":      types.BoolType,
	"delegated":             types.StringType,
	"renew_order":           types.StringType,
	"has_pending_trade":     types.Int64Type,
	"resource_url":          types.StringType,
}

type domainsDataSourceModel struct {
	NameContains types.String `tfsdk:"name_contains"`
	Renewable    types.Bool   `tfsdk:"renewable"`
	Delegated    types.Bool   `tfsdk:"delegated"`
	NeedsRenewal types.Bool   `tfsdk:"needs_renewal"`
	Domains      types.List   `tfsdk:"domains"`
}

func (d *domainsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_domains"
}

func (d *domainsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Reads the domains on the account, newest registration details included.\n\n" +
			"The listing is paginated by the API at 100 domains a request, and this follows every " +
			"page. Filtering happens on zone.eu's side, so a filter costs fewer requests on a large " +
			"account rather than more.",
		Attributes: map[string]schema.Attribute{
			"name_contains": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "Keep only domains whose name contains this text. This is a " +
					"**substring** match, not an exact one: `example` also matches " +
					"`myexample.com`.",
			},
			"renewable": schema.BoolAttribute{
				Optional:            true,
				MarkdownDescription: "Keep only domains that can (or cannot) be renewed.",
			},
			"delegated": schema.BoolAttribute{
				Optional:            true,
				MarkdownDescription: "Keep only domains that are (or are not) delegated to another ZoneID user.",
			},
			"needs_renewal": schema.BoolAttribute{
				Optional:            true,
				MarkdownDescription: "Keep only domains expiring within 30 days.",
			},
			"domains": schema.ListNestedAttribute{
				Computed:            true,
				MarkdownDescription: "The domains found, sorted by name.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"name":                  schema.StringAttribute{Computed: true, MarkdownDescription: "Domain name."},
						"expires":               schema.StringAttribute{Computed: true, MarkdownDescription: "When the registration expires."},
						"expired":               schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether the registration has lapsed."},
						"autorenew":             schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether zone.eu renews it automatically."},
						"dnssec":                schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether the domain is DNSSEC signed."},
						"dnssec_supported":      schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether the registry supports DNSSEC for this TLD."},
						"renewal_notifications": schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether renewal reminders are on."},
						"nameservers_custom":    schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether the domain uses nameservers other than zone.eu's."},
						"reactivate":            schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether an expired domain can still be reactivated."},
						"has_pending_dnssec":    schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether a DNSSEC change is still being applied."},
						"auth_key_enabled":      schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether this TLD uses a transfer authorisation key."},
						"delegated":             schema.StringAttribute{Computed: true, MarkdownDescription: "ZoneID user the domain is delegated to, or null."},
						"renew_order":           schema.StringAttribute{Computed: true, MarkdownDescription: "Pending renewal order, or null."},
						"has_pending_trade":     schema.Int64Attribute{Computed: true, MarkdownDescription: "Identifier of an in-progress trade, or null."},
						"resource_url":          schema.StringAttribute{Computed: true, MarkdownDescription: "API URL of this domain."},
					},
				},
			},
		},
	}
}

func (d *domainsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config domainsDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	filter := zoneapi.DomainFilter{NameContains: config.NameContains.ValueString()}
	if known(config.Renewable) {
		value := config.Renewable.ValueBool()
		filter.Renewable = &value
	}
	if known(config.Delegated) {
		value := config.Delegated.ValueBool()
		filter.Delegated = &value
	}
	if known(config.NeedsRenewal) {
		value := config.NeedsRenewal.ValueBool()
		filter.NeedsRenewal = &value
	}

	domains, err := d.client.ListDomains(ctx, filter)
	if err != nil {
		addAPIError(&resp.Diagnostics, "Could not read the account's domains", err, nil)
		return
	}

	slices.SortFunc(domains, func(a, b zoneapi.Domain) int {
		return strings.Compare(a.Name, b.Name)
	})

	elements := make([]attr.Value, 0, len(domains))
	for _, domain := range domains {
		object, diags := domainObject(domain)
		resp.Diagnostics.Append(diags...)
		if resp.Diagnostics.HasError() {
			return
		}
		elements = append(elements, object)
	}

	list, diags := types.ListValue(types.ObjectType{AttrTypes: domainAttributeTypes}, elements)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	config.Domains = list
	resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
}

func domainObject(domain zoneapi.Domain) (types.Object, diag.Diagnostics) {
	return types.ObjectValue(domainAttributeTypes, map[string]attr.Value{
		"name":                  types.StringValue(domain.Name),
		"expires":               optionalString(domain.Expires),
		"expired":               types.BoolValue(domain.Expired),
		"autorenew":             types.BoolValue(domain.Autorenew),
		"dnssec":                types.BoolValue(domain.DNSSEC),
		"dnssec_supported":      types.BoolValue(domain.DNSSECSupported),
		"renewal_notifications": types.BoolValue(domain.RenewalNotifications),
		"nameservers_custom":    types.BoolValue(domain.NameserversCustom),
		"reactivate":            types.BoolValue(domain.Reactivate),
		"has_pending_dnssec":    types.BoolValue(domain.HasPendingDNSSEC),
		"auth_key_enabled":      types.BoolValue(domain.AuthKeyEnabled),
		"delegated":             stringPointerValue(domain.Delegated),
		"renew_order":           stringPointerValue(domain.RenewOrder),
		"has_pending_trade":     int64PointerValue(domain.HasPendingTrade),
		"resource_url":          optionalString(domain.ResourceURL),
	})
}
