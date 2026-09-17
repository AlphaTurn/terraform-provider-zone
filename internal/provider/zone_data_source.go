package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/AlphaTurn/terraform-provider-zone/internal/zoneapi"
)

// NewZoneDataSource returns the DNS zone data source.
func NewZoneDataSource() datasource.DataSource { return &zoneDataSource{} }

type zoneDataSource struct {
	dataSourceClient
}

var (
	_ datasource.DataSource              = (*zoneDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*zoneDataSource)(nil)
)

func (d *zoneDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_dns_zone"
}

func (d *zoneDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Looks up an existing DNS zone, to confirm it is available to the account " +
			"and to read its settings.",
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Zone name, for example `example.com`.",
			},
			"active": schema.BoolAttribute{
				Computed:            true,
				MarkdownDescription: "Whether zone.eu serves this zone.",
			},
			"ipv6": schema.BoolAttribute{
				Computed:            true,
				MarkdownDescription: "Whether zone.eu's IPv6 support is enabled for this zone.",
			},
			"dnssec": schema.BoolAttribute{
				Computed:            true,
				MarkdownDescription: "Whether the zone is DNSSEC signed.",
			},
			"resource_url": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "API URL of this zone.",
			},
		},
	}
}

func (d *zoneDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config zoneResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	name := config.Name.ValueString()
	zone, err := d.client.GetZone(ctx, name)
	if err != nil {
		if zoneapi.IsNotFound(err) {
			resp.Diagnostics.AddError(
				"DNS zone not found",
				fmt.Sprintf("No DNS zone named %q is available on this ZoneID account.", name),
			)
			return
		}
		resp.Diagnostics.AddError("Could not read DNS zone", err.Error())
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, zoneResourceModel{
		Name:        types.StringValue(zone.Identificator),
		Active:      types.BoolValue(zone.Active),
		IPv6:        types.BoolValue(zone.IPv6),
		DNSSEC:      types.BoolValue(zone.DNSSEC),
		ResourceURL: types.StringValue(zone.ResourceURL),
	})...)
}
