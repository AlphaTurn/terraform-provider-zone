package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/AlphaTurn/terraform-provider-zone/internal/zoneapi"
)

// NewMailDKIMResource returns the DKIM signing resource.
func NewMailDKIMResource() resource.Resource { return &mailDKIMResource{} }

type mailDKIMResource struct {
	resourceClient
}

var (
	_ resource.Resource                = (*mailDKIMResource)(nil)
	_ resource.ResourceWithConfigure   = (*mailDKIMResource)(nil)
	_ resource.ResourceWithImportState = (*mailDKIMResource)(nil)
)

type mailDKIMResourceModel struct {
	ServiceName types.String `tfsdk:"service_name"`
	PublicKey   types.String `tfsdk:"public_key"`
}

func (r *mailDKIMResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_mail_dkim"
}

func (r *mailDKIMResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "DKIM signing for a webhosting service's outgoing mail.\n\n" +
			"There is nothing to configure: the API's create endpoint takes no body at all, so this " +
			"resource is a switch that returns the resulting public key. Publish that key in DNS " +
			"for receivers to verify the signature.\n\n" +
			"~> **There is no way to rotate a key in place.** Destroying and recreating this " +
			"resource generates a new one, and between the two the DNS record published for the " +
			"old key no longer matches, so signatures fail to verify until the record is updated. " +
			"Rotate deliberately, not incidentally.",
		Attributes: map[string]schema.Attribute{
			"service_name": serviceNameAttribute(),
			"public_key": schema.StringAttribute{
				Computed: true,
				MarkdownDescription: "The public half of the signing key, as zone.eu generated it. " +
					"This is what goes into the DKIM `TXT` record — which this provider can also " +
					"manage, with `zone_dns_txt_record`.",
			},
		},
	}
}

func (r *mailDKIMResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan mailDKIMResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	service := plan.ServiceName.ValueString()
	dkim, err := r.client.EnableDKIM(ctx, service)
	if err != nil {
		addAPIError(&resp.Diagnostics, "Could not enable DKIM signing", err, nil)
		return
	}

	// The create response has been observed to carry no key, so fall back to a
	// read rather than storing an empty one.
	if dkim.PublicKey == "" {
		if fetched, err := r.client.GetDKIM(ctx, service); err == nil {
			dkim = fetched
		}
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, mailDKIMResourceModel{
		ServiceName: types.StringValue(service),
		PublicKey:   optionalString(dkim.PublicKey),
	})...)
}

func (r *mailDKIMResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state mailDKIMResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	service := state.ServiceName.ValueString()
	dkim, err := r.client.GetDKIM(ctx, service)
	if err != nil {
		if zoneapi.IsNotFound(err) {
			// Signing was turned off outside Terraform.
			resp.State.RemoveResource(ctx)
			return
		}
		addAPIError(&resp.Diagnostics, "Could not read the DKIM key", err, nil)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, mailDKIMResourceModel{
		ServiceName: types.StringValue(service),
		PublicKey:   optionalString(dkim.PublicKey),
	})...)
}

// Update cannot happen: the resource has one configurable argument and it
// forces replacement.
func (r *mailDKIMResource) Update(_ context.Context, _ resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError(
		"DKIM signing cannot be modified",
		"The API can only turn DKIM signing on or off, so this resource has nothing to update and "+
			"this code path should be unreachable.\n\n"+
			"Reaching it is a bug in the provider; please report it.",
	)
}

func (r *mailDKIMResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state mailDKIMResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.DisableDKIM(ctx, state.ServiceName.ValueString()); err != nil {
		addAPIError(&resp.Diagnostics, "Could not disable DKIM signing", err, nil)
	}
}

// ImportState accepts the service name on its own.
func (r *mailDKIMResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("service_name"), req.ID)...)
}
