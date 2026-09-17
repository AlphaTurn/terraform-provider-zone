package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/boolvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
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

// NewMailAccountResource returns the mailbox resource.
func NewMailAccountResource() resource.Resource { return &mailAccountResource{} }

type mailAccountResource struct {
	resourceClient
}

var (
	_ resource.Resource                = (*mailAccountResource)(nil)
	_ resource.ResourceWithConfigure   = (*mailAccountResource)(nil)
	_ resource.ResourceWithImportState = (*mailAccountResource)(nil)
)

type mailAccountResourceModel struct {
	ServiceName      types.String `tfsdk:"service_name"`
	Address          types.String `tfsdk:"address"`
	Password         types.String `tfsdk:"password"`
	PasswordVersion  types.Int64  `tfsdk:"password_version"`
	Comment          types.String `tfsdk:"comment"`
	Spamlevel        types.String `tfsdk:"spamlevel"`
	TwoFactorAuth    types.Bool   `tfsdk:"two_factor_auth"`
	ForwardAddresses types.Set    `tfsdk:"forward_addresses"`
	Autoreply        types.Bool   `tfsdk:"autoreply"`
	DisabledFeatures types.Set    `tfsdk:"disabled_features"`
	DiskSize         types.Int64  `tfsdk:"disk_size"`
	DiskUsage        types.Int64  `tfsdk:"disk_usage"`
	ResourceURL      types.String `tfsdk:"resource_url"`
}

func (r *mailAccountResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_mail_account"
}

func (r *mailAccountResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A mailbox on a webhosting service.\n\n" +
			"!> **Destroying this deletes the mailbox and the mail in it.**\n\n" +
			"~> **`address` does not force replacement, deliberately.** The address is the " +
			"mailbox's identifier in the API, so renaming one is either an in-place move or a " +
			"refusal. Marking it for replacement would mean a single-character typo destroyed a " +
			"mailbox and its mail; as it is, a rename the API will not do fails the apply and " +
			"nothing is lost.\n\n" +
			writeOnlyNote,
		Attributes: map[string]schema.Attribute{
			"service_name": serviceNameAttribute(),
			"address": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "The mailbox address, for example `info@example.com`. The domain " +
					"must be one the service serves.",
				Validators: []validator.String{isEmailAddress()},
			},
			"password":         passwordAttribute("The mailbox password."),
			"password_version": passwordVersionAttribute(),
			"comment": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "A note on the mailbox, shown in the ZoneID panel.",
			},
			"spamlevel": schema.StringAttribute{
				Optional: true,
				Computed: true,
				MarkdownDescription: "How aggressively to filter spam: `none`, `low`, `medium` or " +
					"`high`.",
				Validators: []validator.String{
					stringvalidator.OneOf("none", "low", "medium", "high"),
				},
			},
			"two_factor_auth": schema.BoolAttribute{
				Optional: true,
				Computed: true,
				MarkdownDescription: "Whether two-factor authentication is on for this mailbox.\n\n" +
					"The API can only turn this **off**; the mailbox's owner turns it on in webmail. " +
					"So `false` means \"turn it off if the owner enabled it\", and `true` is " +
					"rejected during validation rather than by the API after an apply has started. " +
					"Leave it unset to let the owner decide.",
				Validators: []validator.Bool{boolvalidator.Equals(false)},
			},
			"forward_addresses": schema.SetAttribute{
				Optional:    true,
				Computed:    true,
				ElementType: types.StringType,
				MarkdownDescription: "Addresses that a copy of incoming mail is also sent to. The " +
					"mailbox still keeps its own copy; for forwarding without storage use " +
					"`zone_mail_forwarder`.",
				Validators: []validator.Set{setvalidator.ValueStringsAre(isEmailAddress())},
			},
			"autoreply": schema.BoolAttribute{
				Computed: true,
				MarkdownDescription: "Whether an autoreply is active. Read-only here: the autoreply " +
					"itself is a `zone_mail_autoreply` resource.",
			},
			"disabled_features": schema.SetAttribute{
				Computed:            true,
				ElementType:         types.StringType,
				MarkdownDescription: "Features zone.eu has disabled for this mailbox.",
			},
			"disk_size": schema.Int64Attribute{
				Computed:            true,
				MarkdownDescription: "The mailbox quota in bytes.",
			},
			"disk_usage": schema.Int64Attribute{
				Computed:            true,
				MarkdownDescription: "Bytes currently stored.",
			},
			"resource_url": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "API URL of this mailbox.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *mailAccountResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan mailAccountResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	input, diags := mailAccountInput(ctx, plan, req.Config)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	service := plan.ServiceName.ValueString()
	created, err := r.client.CreateMailAccount(ctx, service, input)
	if err != nil {
		addAPIError(&resp.Diagnostics, "Could not create the mailbox", err, mailAccountAttributes)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, modelFromMailAccount(service, created, plan))...)
}

func (r *mailAccountResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state mailAccountResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	service := state.ServiceName.ValueString()
	account, err := r.client.GetMailAccount(ctx, service, state.Address.ValueString())
	if err != nil {
		if zoneapi.IsNotFound(err) {
			// Also the path an archived mailbox takes: the client reports a
			// tombstone as missing, because adopting one gives Terraform a
			// resource it can never reconcile.
			resp.State.RemoveResource(ctx)
			return
		}
		addAPIError(&resp.Diagnostics, "Could not read the mailbox", err, mailAccountAttributes)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, modelFromMailAccount(service, account, state))...)
}

func (r *mailAccountResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state mailAccountResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	input, diags := mailAccountInput(ctx, plan, req.Config)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	service := plan.ServiceName.ValueString()
	updated, err := r.client.UpdateMailAccount(ctx, service, state.Address.ValueString(), input)
	if err != nil {
		addAPIError(&resp.Diagnostics, "Could not update the mailbox", err, mailAccountAttributes)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, modelFromMailAccount(service, updated, plan))...)
}

func (r *mailAccountResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state mailAccountResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteMailAccount(ctx, state.ServiceName.ValueString(), state.Address.ValueString())
	if err != nil {
		addAPIError(&resp.Diagnostics, "Could not delete the mailbox", err, mailAccountAttributes)
	}
}

// ImportState accepts "<service name>/<address>".
func (r *mailAccountResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	service, address, ok := importKey(
		&resp.Diagnostics, req.ID, "<service name>/<address>", "virt1.example.com/info@example.com",
	)
	if !ok {
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("service_name"), service)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("address"), address)...)
}

var mailAccountAttributes = map[string]string{
	"address":         "address",
	"password":        "password",
	"comment":         "comment",
	"spamlevel":       "spamlevel",
	"fwd_addresses":   "forward_addresses",
	"two_factor_auth": "two_factor_auth",
}

func mailAccountInput(ctx context.Context, plan mailAccountResourceModel, config configSource) (zoneapi.MailAccountInput, diag.Diagnostics) {
	var diags diag.Diagnostics

	password := writeOnlyString(ctx, config, "password", &diags)
	if diags.HasError() {
		return zoneapi.MailAccountInput{}, diags
	}

	return zoneapi.MailAccountInput{
		Address:          plan.Address.ValueString(),
		Password:         password,
		Comment:          plan.Comment.ValueString(),
		Spamlevel:        plan.Spamlevel.ValueString(),
		ForwardAddresses: setToStrings(ctx, plan.ForwardAddresses, &diags),
	}, diags
}

func modelFromMailAccount(
	service string, account *zoneapi.MailAccount, desired mailAccountResourceModel,
) mailAccountResourceModel {
	forwards, _ := stringSetValue(account.ForwardAddresses)
	if account.ForwardAddresses == nil && known(desired.ForwardAddresses) {
		forwards = desired.ForwardAddresses
	}
	features, _ := stringSetValue(account.DisabledFeatures)

	return mailAccountResourceModel{
		ServiceName: types.StringValue(service),
		Address:     types.StringValue(account.Address),
		// Write-only: never read back, never stored.
		Password:         types.StringNull(),
		PasswordVersion:  desired.PasswordVersion,
		Comment:          optionalString(account.Comment),
		Spamlevel:        optionalString(account.Spamlevel),
		TwoFactorAuth:    types.BoolValue(account.TwoFactorAuth),
		ForwardAddresses: forwards,
		Autoreply:        types.BoolValue(account.Autoreply),
		DisabledFeatures: features,
		DiskSize:         types.Int64Value(account.DiskSize),
		DiskUsage:        types.Int64Value(account.DiskUsage),
		ResourceURL:      optionalString(account.ResourceURL),
	}
}
