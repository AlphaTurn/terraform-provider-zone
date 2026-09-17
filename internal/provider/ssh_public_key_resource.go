package provider

import (
	"context"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/AlphaTurn/terraform-provider-zone/internal/zoneapi"
)

// NewSSHPublicKeyResource returns the SSH public key resource.
func NewSSHPublicKeyResource() resource.Resource { return &sshPublicKeyResource{} }

type sshPublicKeyResource struct {
	resourceClient
}

var (
	_ resource.Resource                = (*sshPublicKeyResource)(nil)
	_ resource.ResourceWithConfigure   = (*sshPublicKeyResource)(nil)
	_ resource.ResourceWithImportState = (*sshPublicKeyResource)(nil)
)

type sshPublicKeyResourceModel struct {
	ServiceName types.String `tfsdk:"service_name"`
	ID          types.Int64  `tfsdk:"id"`
	PublicKey   types.String `tfsdk:"public_key"`
	Comment     types.String `tfsdk:"comment"`
	Fingerprint types.String `tfsdk:"fingerprint"`
	Type        types.String `tfsdk:"type"`
	Size        types.Int64  `tfsdk:"size"`
	Created     types.String `tfsdk:"created"`
	LastUsed    types.String `tfsdk:"last_used"`
	ResourceURL types.String `tfsdk:"resource_url"`
}

func (r *sshPublicKeyResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_ssh_public_key"
}

func (r *sshPublicKeyResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "An SSH public key authorised to log in to a webhosting service.\n\n" +
			replaceOnlyNote,
		Attributes: map[string]schema.Attribute{
			"service_name": serviceNameAttribute(),
			"id": schema.Int64Attribute{
				Computed:            true,
				MarkdownDescription: "Numeric identifier assigned by zone.eu.",
				PlanModifiers:       []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
			},
			"public_key": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "The key itself, in OpenSSH format — the one line from a " +
					"`.pub` file, for example `file(\"~/.ssh/id_ed25519.pub\")`.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators:    []validator.String{isSSHPublicKey()},
			},
			"comment": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "A note on the key, shown in the ZoneID panel.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"fingerprint": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The key's fingerprint, as zone.eu computed it. Also accepted as an import ID.",
			},
			"type": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Key algorithm, derived from the key itself, for example `ed25519` or `rsa`.",
			},
			"size": schema.Int64Attribute{
				Computed: true,
				MarkdownDescription: "Key size in bits, where that is meaningful. Null for key types " +
					"with a fixed size, such as Ed25519.",
			},
			"created": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "When the key was authorised.",
			},
			"last_used": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "When the key was last used to log in, or null if it never has been.",
			},
			"resource_url": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "API URL of this key.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *sshPublicKeyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan sshPublicKeyResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	service := plan.ServiceName.ValueString()
	created, err := r.client.CreateSSHPublicKey(ctx, service, zoneapi.SSHPublicKeyInput{
		PublicKey: plan.PublicKey.ValueString(),
		Comment:   plan.Comment.ValueString(),
	})
	if err != nil {
		addAPIError(&resp.Diagnostics, "Could not authorise the SSH key", err, sshPublicKeyAttributes)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, modelFromSSHPublicKey(service, created, plan))...)
}

func (r *sshPublicKeyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state sshPublicKeyResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	service := state.ServiceName.ValueString()
	key, err := r.lookup(ctx, service, state)
	if err != nil {
		if zoneapi.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		addAPIError(&resp.Diagnostics, "Could not read the SSH key", err, sshPublicKeyAttributes)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, modelFromSSHPublicKey(service, key, state))...)
}

// lookup finds the key by id, or by fingerprint when an import supplied one
// instead.
func (r *sshPublicKeyResource) lookup(ctx context.Context, service string, state sshPublicKeyResourceModel) (*zoneapi.SSHPublicKey, error) {
	if known(state.ID) && state.ID.ValueInt64() != 0 {
		return r.client.GetSSHPublicKey(ctx, service, state.ID.ValueInt64())
	}
	return r.client.FindSSHPublicKey(ctx, service, state.Fingerprint.ValueString())
}

func (r *sshPublicKeyResource) Update(_ context.Context, _ resource.UpdateRequest, resp *resource.UpdateResponse) {
	addNoUpdateError(&resp.Diagnostics, "An authorised SSH key")
}

func (r *sshPublicKeyResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state sshPublicKeyResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteSSHPublicKey(ctx, state.ServiceName.ValueString(), state.ID.ValueInt64())
	if err != nil {
		addAPIError(&resp.Diagnostics, "Could not remove the SSH key", err, sshPublicKeyAttributes)
	}
}

// ImportState accepts "<service name>/<key id>" or "<service name>/<fingerprint>".
//
// Both forms exist because nobody reads a key's numeric id out of the panel;
// they have the fingerprint from `ssh-keygen -lf`. The fingerprint itself
// contains no slash, so splitting on the first one is unambiguous.
func (r *sshPublicKeyResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	service, key, ok := importKey(
		&resp.Diagnostics, req.ID,
		"<service name>/<key id or fingerprint>", "virt1.example.com/SHA256:abc123...",
	)
	if !ok {
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("service_name"), service)...)
	if id, err := strconv.ParseInt(key, 10, 64); err == nil {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id)...)
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("fingerprint"), key)...)
}

var sshPublicKeyAttributes = map[string]string{
	"public_key": "public_key",
	"comment":    "comment",
}

func modelFromSSHPublicKey(
	service string, key *zoneapi.SSHPublicKey, desired sshPublicKeyResourceModel,
) sshPublicKeyResourceModel {
	// The create response has been seen to omit the key it was just given, and
	// Terraform requires a configured attribute to hold its planned value.
	publicKey := optionalString(key.PublicKey)
	if key.PublicKey == "" && known(desired.PublicKey) {
		publicKey = desired.PublicKey
	}
	comment := optionalString(key.Comment)
	if key.Comment == "" && known(desired.Comment) {
		comment = desired.Comment
	}

	return sshPublicKeyResourceModel{
		ServiceName: types.StringValue(service),
		ID:          types.Int64Value(key.ID),
		PublicKey:   publicKey,
		Comment:     comment,
		Fingerprint: optionalString(key.Fingerprint),
		Type:        optionalString(key.Type),
		Size:        int64PointerValue(key.Size),
		Created:     optionalString(key.Created),
		LastUsed:    stringPointerValue(key.LastUsed),
		ResourceURL: optionalString(key.ResourceURL),
	}
}
