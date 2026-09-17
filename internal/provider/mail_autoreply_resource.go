package provider

import (
	"context"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/AlphaTurn/terraform-provider-zone/internal/zoneapi"
)

// NewMailAutoreplyResource returns the autoreply resource.
func NewMailAutoreplyResource() resource.Resource { return &mailAutoreplyResource{} }

type mailAutoreplyResource struct {
	resourceClient
}

var (
	_ resource.Resource                   = (*mailAutoreplyResource)(nil)
	_ resource.ResourceWithConfigure      = (*mailAutoreplyResource)(nil)
	_ resource.ResourceWithImportState    = (*mailAutoreplyResource)(nil)
	_ resource.ResourceWithValidateConfig = (*mailAutoreplyResource)(nil)
)

type mailAutoreplyResourceModel struct {
	ServiceName types.String `tfsdk:"service_name"`
	Target      types.String `tfsdk:"target"`
	Address     types.String `tfsdk:"address"`
	Enabled     types.Bool   `tfsdk:"enabled"`
	FromName    types.String `tfsdk:"from_name"`
	Subject     types.String `tfsdk:"subject"`
	Body        types.String `tfsdk:"body"`
	DateStart   types.String `tfsdk:"date_start"`
	DateEnd     types.String `tfsdk:"date_end"`
	ResourceURL types.String `tfsdk:"resource_url"`
}

func (r *mailAutoreplyResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_mail_autoreply"
}

func (r *mailAutoreplyResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "An automatic reply on a mailbox or a forwarder.\n\n" +
			"This is a separate resource rather than a block on `zone_mail_account` because the " +
			"identical object hangs off both mailboxes **and** forwarders, from two endpoints. " +
			"Folding it into the mailbox would leave forwarder autoreplies unmanageable. It also " +
			"matches how people work: a vacation message is set and cleared on a different " +
			"schedule from provisioning a mailbox.\n\n" +
			"~> **Destroying this disables the autoreply rather than leaving it in place.** The " +
			"endpoint has no delete, but it can express \"off\", so that is what destroy does — " +
			"unlike the other resources here whose objects the API cannot remove.",
		Attributes: map[string]schema.Attribute{
			"service_name": serviceNameAttribute(),
			"target": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "Whether `address` names a mailbox or a forwarder: `account` " +
					"or `forwarder`. The two are separate endpoints serving the same object, so " +
					"this has to be stated rather than guessed — guessing would mean a speculative " +
					"extra request on every read, out of a budget of 60 a minute.",
				Validators: []validator.String{
					stringvalidator.OneOf(
						string(zoneapi.AutoreplyOnAccount),
						string(zoneapi.AutoreplyOnForwarder),
					),
				},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"address": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "The mailbox or forwarder address this autoreply belongs to.",
				Validators:          []validator.String{isEmailAddress()},
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"enabled": schema.BoolAttribute{
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(true),
				MarkdownDescription: "Whether the autoreply is sending. Set it to `false` to keep the text but stop replying.",
			},
			"body": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "The message body.",
			},
			"subject": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString(""),
				MarkdownDescription: "The reply's subject line.",
			},
			"from_name": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString(""),
				MarkdownDescription: "The name the reply appears to come from.",
			},
			"date_start": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "First day to reply, as `YYYY-MM-DD`. Leave both dates unset to " +
					"reply until the autoreply is disabled.",
			},
			"date_end": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Last day to reply, as `YYYY-MM-DD`.",
			},
			"resource_url": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "API URL of this autoreply.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

const autoreplyDateLayout = "2006-01-02"

// ValidateConfig checks the dates, because a reversed range is a mistake that
// silently produces an autoreply that never fires.
func (r *mailAutoreplyResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var config mailAutoreplyResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	start := autoreplyDate(&resp.Diagnostics, "date_start", config.DateStart)
	end := autoreplyDate(&resp.Diagnostics, "date_end", config.DateEnd)
	if resp.Diagnostics.HasError() || start == nil || end == nil {
		return
	}

	if end.Before(*start) {
		resp.Diagnostics.AddAttributeError(
			path.Root("date_end"),
			"Autoreply ends before it starts",
			"date_end is earlier than date_start, so this autoreply would never send a reply.",
		)
	}
}

func autoreplyDate(diags *diag.Diagnostics, attribute string, value types.String) *time.Time {
	if !known(value) || value.ValueString() == "" {
		return nil
	}

	parsed, err := time.Parse(autoreplyDateLayout, value.ValueString())
	if err != nil {
		diags.AddAttributeError(
			path.Root(attribute),
			"Invalid date",
			"Autoreply dates are written as YYYY-MM-DD, for example 2026-07-01.",
		)
		return nil
	}
	return &parsed
}

func (r *mailAutoreplyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan mailAutoreplyResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	r.write(ctx, plan, autoreplyInput(plan), &resp.State, &resp.Diagnostics)
}

func (r *mailAutoreplyResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan mailAutoreplyResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	r.write(ctx, plan, autoreplyInput(plan), &resp.State, &resp.Diagnostics)
}

func (r *mailAutoreplyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state mailAutoreplyResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	autoreply, err := r.client.GetAutoreply(
		ctx, state.ServiceName.ValueString(),
		zoneapi.AutoreplyTarget(state.Target.ValueString()), state.Address.ValueString(),
	)
	if err != nil {
		if zoneapi.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		addAPIError(&resp.Diagnostics, "Could not read the autoreply", err, autoreplyAttributes)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, modelFromAutoreply(state, autoreply))...)
}

// Delete disables the autoreply. The endpoint has no DELETE, but unlike a
// domain or a zone, the destroyed state is expressible here.
func (r *mailAutoreplyResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state mailAutoreplyResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	_, err := r.client.SetAutoreply(
		ctx, state.ServiceName.ValueString(),
		zoneapi.AutoreplyTarget(state.Target.ValueString()), state.Address.ValueString(),
		zoneapi.AutoreplyInput{Enabled: false},
	)
	if err != nil {
		addAPIError(&resp.Diagnostics, "Could not disable the autoreply", err, autoreplyAttributes)
	}
}

// ImportState accepts "<service name>/<target>/<address>".
func (r *mailAutoreplyResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts, ok := importParts(
		&resp.Diagnostics, req.ID, 3,
		"<service name>/<target>/<address>", "virt1.example.com/account/info@example.com",
	)
	if !ok {
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("service_name"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("target"), parts[1])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("address"), parts[2])...)
}

var autoreplyAttributes = map[string]string{
	"is_enabled": "enabled",
	"fromname":   "from_name",
	"subject":    "subject",
	"body":       "body",
	"datestart":  "date_start",
	"dateend":    "date_end",
}

func autoreplyInput(plan mailAutoreplyResourceModel) zoneapi.AutoreplyInput {
	return zoneapi.AutoreplyInput{
		Enabled:   plan.Enabled.ValueBool(),
		FromName:  plan.FromName.ValueString(),
		Subject:   plan.Subject.ValueString(),
		Body:      plan.Body.ValueString(),
		DateStart: plan.DateStart.ValueString(),
		DateEnd:   plan.DateEnd.ValueString(),
	}
}

func (r *mailAutoreplyResource) write(
	ctx context.Context, plan mailAutoreplyResourceModel, input zoneapi.AutoreplyInput,
	state *tfsdk.State, diags *diag.Diagnostics,
) {
	stored, err := r.client.SetAutoreply(
		ctx, plan.ServiceName.ValueString(),
		zoneapi.AutoreplyTarget(plan.Target.ValueString()), plan.Address.ValueString(), input,
	)
	if err != nil {
		addAPIError(diags, "Could not write the autoreply", err, autoreplyAttributes)
		return
	}

	diags.Append(state.Set(ctx, modelFromAutoreply(plan, stored))...)
}

func modelFromAutoreply(plan mailAutoreplyResourceModel, autoreply *zoneapi.Autoreply) mailAutoreplyResourceModel {
	return mailAutoreplyResourceModel{
		ServiceName: plan.ServiceName,
		Target:      plan.Target,
		Address:     plan.Address,
		Enabled:     types.BoolValue(autoreply.Enabled),
		FromName:    types.StringValue(autoreply.FromName),
		Subject:     types.StringValue(autoreply.Subject),
		Body:        types.StringValue(autoreply.Body),
		DateStart:   optionalString(autoreply.DateStart),
		DateEnd:     optionalString(autoreply.DateEnd),
		ResourceURL: optionalString(autoreply.ResourceURL),
	}
}
