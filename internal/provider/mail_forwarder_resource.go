package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/AlphaTurn/terraform-provider-zone/internal/zoneapi"
)

// NewMailForwarderResource returns the mail forwarder resource.
func NewMailForwarderResource() resource.Resource { return &mailForwarderResource{} }

type mailForwarderResource struct {
	resourceClient
}

var (
	_ resource.Resource                = (*mailForwarderResource)(nil)
	_ resource.ResourceWithConfigure   = (*mailForwarderResource)(nil)
	_ resource.ResourceWithImportState = (*mailForwarderResource)(nil)
)

type mailForwarderResourceModel struct {
	ServiceName      types.String `tfsdk:"service_name"`
	Address          types.String `tfsdk:"address"`
	Comment          types.String `tfsdk:"comment"`
	ForwardAddresses types.Set    `tfsdk:"forward_addresses"`
	Autoreply        types.Bool   `tfsdk:"autoreply"`
	DisabledFeatures types.Set    `tfsdk:"disabled_features"`
	MailToHTTPURL    types.String `tfsdk:"mail_to_http_url"`
	ResourceURL      types.String `tfsdk:"resource_url"`
}

func (r *mailForwarderResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_mail_forwarder"
}

func (r *mailForwarderResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "An address that forwards mail without storing it.\n\n" +
			"Unlike `zone_mail_account`, a forwarder has no mailbox, no quota and no password. Use " +
			"it for addresses such as `info@` that should land in somebody's existing inbox.",
		Attributes: map[string]schema.Attribute{
			"service_name": serviceNameAttribute(),
			"address": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "The forwarding address, for example `info@example.com`. As " +
					"with a mailbox, this is the API's identifier for the forwarder, so it does not " +
					"force replacement — see `zone_mail_account` for why.",
				Validators: []validator.String{isEmailAddress()},
			},
			"forward_addresses": schema.SetAttribute{
				Required:            true,
				ElementType:         types.StringType,
				MarkdownDescription: "Where mail to this address is sent. At least one is required.",
				Validators: []validator.Set{
					setvalidator.SizeAtLeast(1),
					setvalidator.ValueStringsAre(isEmailAddress()),
				},
			},
			"comment": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "A note on the forwarder, shown in the ZoneID panel.",
			},
			"autoreply": schema.BoolAttribute{
				Computed: true,
				MarkdownDescription: "Whether an autoreply is active. Read-only here: the autoreply " +
					"itself is a `zone_mail_autoreply` resource with `target = \"forwarder\"`.",
			},
			"disabled_features": schema.SetAttribute{
				Computed:            true,
				ElementType:         types.StringType,
				MarkdownDescription: "Features zone.eu has disabled for this forwarder.",
			},
			"mail_to_http_url": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "URL that mail to this address is posted to, when so configured.",
			},
			"resource_url": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "API URL of this forwarder.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *mailForwarderResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan mailForwarderResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	service := plan.ServiceName.ValueString()
	created, err := r.client.CreateMailForwarder(ctx, service, zoneapi.MailForwarderInput{
		Address:          plan.Address.ValueString(),
		Comment:          plan.Comment.ValueString(),
		ForwardAddresses: setToStrings(ctx, plan.ForwardAddresses, &resp.Diagnostics),
	})
	if err != nil {
		addAPIError(&resp.Diagnostics, "Could not create the mail forwarder", err, mailForwarderAttributes)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, modelFromMailForwarder(service, created, plan))...)
}

func (r *mailForwarderResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state mailForwarderResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	service := state.ServiceName.ValueString()
	forwarder, err := r.client.GetMailForwarder(ctx, service, state.Address.ValueString())
	if err != nil {
		if zoneapi.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		addAPIError(&resp.Diagnostics, "Could not read the mail forwarder", err, mailForwarderAttributes)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, modelFromMailForwarder(service, forwarder, state))...)
}

func (r *mailForwarderResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state mailForwarderResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	service := plan.ServiceName.ValueString()
	updated, err := r.client.UpdateMailForwarder(ctx, service, state.Address.ValueString(), zoneapi.MailForwarderInput{
		Address:          plan.Address.ValueString(),
		Comment:          plan.Comment.ValueString(),
		ForwardAddresses: setToStrings(ctx, plan.ForwardAddresses, &resp.Diagnostics),
	})
	if err != nil {
		addAPIError(&resp.Diagnostics, "Could not update the mail forwarder", err, mailForwarderAttributes)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, modelFromMailForwarder(service, updated, plan))...)
}

func (r *mailForwarderResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state mailForwarderResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteMailForwarder(ctx, state.ServiceName.ValueString(), state.Address.ValueString())
	if err != nil {
		addAPIError(&resp.Diagnostics, "Could not delete the mail forwarder", err, mailForwarderAttributes)
	}
}

// ImportState accepts "<service name>/<address>".
func (r *mailForwarderResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	service, address, ok := importKey(
		&resp.Diagnostics, req.ID, "<service name>/<address>", "virt1.example.com/info@example.com",
	)
	if !ok {
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("service_name"), service)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("address"), address)...)
}

var mailForwarderAttributes = map[string]string{
	"address":       "address",
	"comment":       "comment",
	"fwd_addresses": "forward_addresses",
}

func modelFromMailForwarder(
	service string, forwarder *zoneapi.MailForwarder, desired mailForwarderResourceModel,
) mailForwarderResourceModel {
	forwards, _ := stringSetValue(forwarder.ForwardAddresses)
	if forwarder.ForwardAddresses == nil && known(desired.ForwardAddresses) {
		forwards = desired.ForwardAddresses
	}
	features, _ := stringSetValue(forwarder.DisabledFeatures)

	return mailForwarderResourceModel{
		ServiceName:      types.StringValue(service),
		Address:          types.StringValue(forwarder.Address),
		Comment:          optionalString(forwarder.Comment),
		ForwardAddresses: forwards,
		Autoreply:        types.BoolValue(forwarder.Autoreply),
		DisabledFeatures: features,
		MailToHTTPURL:    optionalString(forwarder.MailToHTTPURL),
		ResourceURL:      optionalString(forwarder.ResourceURL),
	}
}
