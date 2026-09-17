package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/AlphaTurn/terraform-provider-zone/internal/zoneapi"
)

// resourceClient supplies the API client to a resource and satisfies
// resource.ResourceWithConfigure, so a resource embeds it instead of repeating
// the same type assertion.
//
// There is one client for a whole Terraform run: the rate limiter and the
// listing caches live on it, and they only do their job if every resource
// shares them.
type resourceClient struct {
	client *zoneapi.Client
}

func (c *resourceClient) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return // Configure runs before the provider is configured during validation.
	}
	if client, ok := clientFromProviderData(req.ProviderData, &resp.Diagnostics); ok {
		c.client = client
	}
}

// dataSourceClient is [resourceClient] for data sources.
type dataSourceClient struct {
	client *zoneapi.Client
}

func (c *dataSourceClient) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	if client, ok := clientFromProviderData(req.ProviderData, &resp.Diagnostics); ok {
		c.client = client
	}
}

func clientFromProviderData(data any, diags *diag.Diagnostics) (*zoneapi.Client, bool) {
	client, ok := data.(*zoneapi.Client)
	if !ok {
		diags.AddError(
			"Unexpected provider data",
			fmt.Sprintf("Expected *zoneapi.Client but got %T. This is a bug in the provider.", data),
		)
		return nil, false
	}
	return client, true
}
