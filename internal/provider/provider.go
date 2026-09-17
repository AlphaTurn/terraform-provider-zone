// Package provider implements the zone.eu Terraform provider.
package provider

import (
	"context"
	"os"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/AlphaTurn/terraform-provider-zone/internal/dnsrecord"
	"github.com/AlphaTurn/terraform-provider-zone/internal/zoneapi"
)

// Environment variables consulted when the matching argument is not set.
const (
	envUsername = "ZONE_USERNAME"
	envAPIToken = "ZONE_API_TOKEN"
	envBaseURL  = "ZONE_BASE_URL"
)

type zoneProvider struct {
	version string
}

// New returns the provider constructor. version is stamped into the User-Agent.
func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &zoneProvider{version: version}
	}
}

var _ provider.Provider = (*zoneProvider)(nil)

type providerModel struct {
	Username   types.String `tfsdk:"username"`
	APIToken   types.String `tfsdk:"api_token"`
	BaseURL    types.String `tfsdk:"base_url"`
	RateLimit  types.Int64  `tfsdk:"rate_limit"`
	MaxRetries types.Int64  `tfsdk:"max_retries"`
}

func (p *zoneProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "zone"
	resp.Version = p.version
}

func (p *zoneProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages DNS records hosted by [zone.eu](https://www.zone.eu) through the ZoneID API v2.\n\n" +
			"Credentials are an API token generated under ZoneID account management, paired with the " +
			"ZoneID username it belongs to.",
		Attributes: map[string]schema.Attribute{
			"username": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "ZoneID username. May also be supplied with the `" + envUsername +
					"` environment variable.",
			},
			"api_token": schema.StringAttribute{
				Optional:  true,
				Sensitive: true,
				MarkdownDescription: "ZoneID API token, generated under account management. May also be " +
					"supplied with the `" + envAPIToken + "` environment variable, which is the better " +
					"place for it: a token in configuration ends up in version control and in state.",
			},
			"base_url": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "API endpoint. Defaults to `" + zoneapi.DefaultBaseURL +
					"`. Mainly useful for pointing tests at a stub server.",
			},
			"rate_limit": schema.Int64Attribute{
				Optional: true,
				MarkdownDescription: "Sustained request rate in requests per minute. Defaults to `" +
					strconv.Itoa(zoneapi.DefaultRateLimit) + "`. zone.eu enforces 60 per minute per IP, " +
					"and the default leaves headroom for retries and for anything else calling the API " +
					"from the same address. Raising it past 60 will cause throttling, not speed.",
			},
			"max_retries": schema.Int64Attribute{
				Optional: true,
				MarkdownDescription: "How many times to retry a rate-limited, conflicted or failed " +
					"request. Defaults to `" + strconv.Itoa(zoneapi.DefaultMaxRetries) + "`.",
			},
		},
	}
}

func (p *zoneProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var config providerModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Unknown values mean another resource has to be applied first. Say so
	// plainly rather than failing later with an empty credential.
	for _, unknown := range []struct {
		value     types.String
		attribute string
		env       string
	}{
		{config.Username, "username", envUsername},
		{config.APIToken, "api_token", envAPIToken},
	} {
		if unknown.value.IsUnknown() {
			resp.Diagnostics.AddAttributeError(
				path.Root(unknown.attribute),
				"Credential is not known at plan time",
				"The provider cannot be configured with a value that is only known after apply. "+
					"Set "+unknown.env+" in the environment, or apply whatever produces this value first.",
			)
		}
	}
	if resp.Diagnostics.HasError() {
		return
	}

	username := firstNonEmpty(config.Username.ValueString(), os.Getenv(envUsername))
	apiToken := firstNonEmpty(config.APIToken.ValueString(), os.Getenv(envAPIToken))
	baseURL := firstNonEmpty(config.BaseURL.ValueString(), os.Getenv(envBaseURL))

	if username == "" {
		resp.Diagnostics.AddAttributeError(
			path.Root("username"),
			"Missing ZoneID username",
			"Set the username argument on the provider, or the "+envUsername+" environment variable. "+
				"This is the ZoneID account the API token belongs to, not an email address.",
		)
	}
	if apiToken == "" {
		resp.Diagnostics.AddAttributeError(
			path.Root("api_token"),
			"Missing ZoneID API token",
			"Set the api_token argument on the provider, or the "+envAPIToken+" environment variable. "+
				"Tokens are generated under ZoneID account management.",
		)
	}
	if resp.Diagnostics.HasError() {
		return
	}

	options := []zoneapi.Option{
		zoneapi.WithBaseURL(baseURL),
		zoneapi.WithUserAgent("terraform-provider-zone/" + p.version),
	}
	if !config.RateLimit.IsNull() && !config.RateLimit.IsUnknown() {
		options = append(options, zoneapi.WithRateLimit(int(config.RateLimit.ValueInt64())))
	}
	if !config.MaxRetries.IsNull() && !config.MaxRetries.IsUnknown() {
		options = append(options, zoneapi.WithMaxRetries(int(config.MaxRetries.ValueInt64())))
	}

	client, err := zoneapi.New(username, apiToken, options...)
	if err != nil {
		resp.Diagnostics.AddError("Could not create the zone.eu API client", err.Error())
		return
	}

	// One client for the whole run: the rate limiter and the record cache live
	// on it, and they only do their job if every resource shares them.
	resp.DataSourceData = client
	resp.ResourceData = client
}

func (p *zoneProvider) Resources(context.Context) []func() resource.Resource {
	definitions := dnsrecord.Definitions()

	resources := make([]func() resource.Resource, 0, len(definitions)+1)
	for _, definition := range definitions {
		resources = append(resources, dnsrecord.NewResource(definition))
	}
	resources = append(resources, NewZoneResource)
	return resources
}

func (p *zoneProvider) DataSources(context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewZoneDataSource,
		NewRecordsDataSource,
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
