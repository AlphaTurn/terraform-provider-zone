package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/AlphaTurn/terraform-provider-zone/internal/zoneapi"
)

// NewSSHSettingsResource returns the SSH settings resource.
func NewSSHSettingsResource() resource.Resource { return &sshSettingsResource{} }

type sshSettingsResource struct {
	resourceClient
}

var (
	_ resource.Resource                = (*sshSettingsResource)(nil)
	_ resource.ResourceWithConfigure   = (*sshSettingsResource)(nil)
	_ resource.ResourceWithImportState = (*sshSettingsResource)(nil)
)

type sshSettingsResourceModel struct {
	ServiceName  types.String `tfsdk:"service_name"`
	Access       types.String `tfsdk:"access"`
	Username     types.String `tfsdk:"username"`
	IPv4         types.String `tfsdk:"ipv4"`
	IPv6         types.String `tfsdk:"ipv6"`
	Webhosts     types.Set    `tfsdk:"webhosts"`
	Fingerprints types.Map    `tfsdk:"server_fingerprints"`
	ResourceURL  types.String `tfsdk:"resource_url"`
}

func (r *sshSettingsResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_ssh_settings"
}

func (r *sshSettingsResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "SSH access settings for a webhosting service.\n\n" +
			"There is exactly one of these per service and only one writable field, so this is a " +
			"settings resource in the same sense as `zone_dns_zone`.\n\n" +
			"!> **`access = \"whitelist\"` with no whitelist entries locks you out of your own " +
			"server.** Terraform cannot express that constraint across resources, so pair this with " +
			"at least one `zone_ssh_whitelist_ip` and apply them together.\n\n" +
			adoptNote,
		Attributes: map[string]schema.Attribute{
			"service_name": serviceNameAttribute(),
			"access": schema.StringAttribute{
				Optional: true,
				Computed: true,
				MarkdownDescription: "Who may connect: `public` for anywhere, or `whitelist` to " +
					"allow only the addresses listed in `zone_ssh_whitelist_ip`.",
				Validators: []validator.String{
					stringvalidator.OneOf("public", "whitelist"),
				},
			},
			"username": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The SSH username, assigned by zone.eu.",
			},
			"ipv4": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The server's IPv4 address.",
			},
			"ipv6": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The server's IPv6 address, or null when it has none.",
			},
			"webhosts": schema.SetAttribute{
				Computed:            true,
				ElementType:         types.StringType,
				MarkdownDescription: "Hostnames served from this service.",
			},
			"server_fingerprints": schema.MapAttribute{
				Computed:    true,
				ElementType: types.StringType,
				MarkdownDescription: "The server's host key fingerprints, keyed by algorithm — " +
					"read one with `server_fingerprints[\"ED25519\"]`.\n\n" +
					"A map rather than fixed attributes because the key set belongs to zone.eu: " +
					"naming `DSA`, `ECDSA`, `ED25519` and `RSA` in the schema would break the day " +
					"one is dropped or another added.",
			},
			"resource_url": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "API URL of these settings.",
			},
		},
	}
}

// Create adopts the settings that already exist, then applies what was asked
// for. There is nothing to create: a service has SSH settings from birth.
func (r *sshSettingsResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan sshSettingsResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	r.apply(ctx, plan, &resp.State, &resp.Diagnostics)
}

func (r *sshSettingsResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan sshSettingsResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	r.apply(ctx, plan, &resp.State, &resp.Diagnostics)
}

func (r *sshSettingsResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state sshSettingsResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	service := state.ServiceName.ValueString()
	settings, err := r.client.GetSSHSettings(ctx, service)
	if err != nil {
		if zoneapi.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		addAPIError(&resp.Diagnostics, "Could not read the SSH settings", err, sshSettingsAttributes)
		return
	}

	model, diags := modelFromSSHSettings(service, settings)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, model)...)
}

// Delete stops managing the settings; SSH itself is part of the service.
func (r *sshSettingsResource) Delete(_ context.Context, _ resource.DeleteRequest, resp *resource.DeleteResponse) {
	resp.Diagnostics.AddWarning(
		"SSH settings left as they are",
		"SSH is part of the hosting service and the API cannot remove its settings, so access is "+
			"unchanged. Terraform has only stopped managing it.",
	)
}

func (r *sshSettingsResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("service_name"), req.ID)...)
}

var sshSettingsAttributes = map[string]string{"access": "access"}

// apply writes access when it was configured and differs, and otherwise just
// reads. With 60 requests a minute to spend, a write that changes nothing is
// worth skipping.
func (r *sshSettingsResource) apply(
	ctx context.Context, plan sshSettingsResourceModel, state *tfsdk.State, diags *diag.Diagnostics,
) {
	service := plan.ServiceName.ValueString()

	current, err := r.client.GetSSHSettings(ctx, service)
	if err != nil {
		addAPIError(diags, "Could not read the SSH settings", err, sshSettingsAttributes)
		return
	}

	settings := current
	if known(plan.Access) && plan.Access.ValueString() != current.Access {
		updated, err := r.client.UpdateSSHSettings(ctx, service, zoneapi.SSHSettingsInput{
			Access: plan.Access.ValueString(),
		})
		if err != nil {
			addAPIError(diags, "Could not update the SSH settings", err, sshSettingsAttributes)
			return
		}
		settings = updated
	}

	model, modelDiags := modelFromSSHSettings(service, settings)
	diags.Append(modelDiags...)
	if diags.HasError() {
		return
	}
	diags.Append(state.Set(ctx, model)...)
}

func modelFromSSHSettings(service string, settings *zoneapi.SSHSettings) (sshSettingsResourceModel, diag.Diagnostics) {
	var diags diag.Diagnostics

	webhosts, hostDiags := stringSetValue(settings.Webhosts)
	diags.Append(hostDiags...)

	fingerprints := types.MapNull(types.StringType)
	if settings.Fingerprints != nil {
		values := make(map[string]attr.Value, len(settings.Fingerprints))
		for algorithm, fingerprint := range settings.Fingerprints {
			values[algorithm] = types.StringValue(fingerprint)
		}
		built, mapDiags := types.MapValue(types.StringType, values)
		diags.Append(mapDiags...)
		fingerprints = built
	}

	return sshSettingsResourceModel{
		ServiceName:  types.StringValue(service),
		Access:       optionalString(settings.Access),
		Username:     optionalString(settings.Username),
		IPv4:         optionalString(settings.IPv4),
		IPv6:         stringPointerValue(settings.IPv6),
		Webhosts:     webhosts,
		Fingerprints: fingerprints,
		ResourceURL:  optionalString(settings.ResourceURL),
	}, diags
}
