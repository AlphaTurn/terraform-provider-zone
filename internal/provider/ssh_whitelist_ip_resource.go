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

// NewSSHWhitelistIPResource returns the SSH whitelist resource.
func NewSSHWhitelistIPResource() resource.Resource { return &sshWhitelistIPResource{} }

type sshWhitelistIPResource struct {
	resourceClient
}

var (
	_ resource.Resource                = (*sshWhitelistIPResource)(nil)
	_ resource.ResourceWithConfigure   = (*sshWhitelistIPResource)(nil)
	_ resource.ResourceWithImportState = (*sshWhitelistIPResource)(nil)
)

type sshWhitelistIPResourceModel struct {
	ServiceName types.String `tfsdk:"service_name"`
	ID          types.Int64  `tfsdk:"id"`
	IP          types.String `tfsdk:"ip"`
	Comment     types.String `tfsdk:"comment"`
	ResourceURL types.String `tfsdk:"resource_url"`
}

func (r *sshWhitelistIPResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_ssh_whitelist_ip"
}

func (r *sshWhitelistIPResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "An address allowed to reach a webhosting service's SSH.\n\n" +
			"These only take effect when `zone_ssh_settings` has `access = \"whitelist\"`; with " +
			"`access = \"public\"` they are recorded and ignored.\n\n" + replaceOnlyNote,
		Attributes: map[string]schema.Attribute{
			"service_name": serviceNameAttribute(),
			"id": schema.Int64Attribute{
				Computed:            true,
				MarkdownDescription: "Numeric identifier assigned by zone.eu.",
				PlanModifiers:       []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
			},
			"ip": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "The address to allow. A network may be given with a prefix " +
					"length, for example `217.128.0.0/24`.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators:    []validator.String{isIPOrPrefix()},
			},
			"comment": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "A note on the entry, shown in the ZoneID panel.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"resource_url": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "API URL of this entry.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *sshWhitelistIPResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan sshWhitelistIPResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	service := plan.ServiceName.ValueString()
	created, err := r.client.CreateSSHWhitelistIP(ctx, service, zoneapi.SSHWhitelistIPInput{
		IP:      plan.IP.ValueString(),
		Comment: plan.Comment.ValueString(),
	})
	if err != nil {
		addAPIError(&resp.Diagnostics, "Could not allow the address", err, sshWhitelistAttributes)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, modelFromSSHWhitelistIP(service, created, plan))...)
}

func (r *sshWhitelistIPResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state sshWhitelistIPResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	service := state.ServiceName.ValueString()

	var entry *zoneapi.SSHWhitelistIP
	var err error
	if known(state.ID) && state.ID.ValueInt64() != 0 {
		entry, err = r.client.GetSSHWhitelistIP(ctx, service, state.ID.ValueInt64())
	} else {
		entry, err = r.client.FindSSHWhitelistIP(ctx, service, state.IP.ValueString())
	}
	if err != nil {
		if zoneapi.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		addAPIError(&resp.Diagnostics, "Could not read the whitelist entry", err, sshWhitelistAttributes)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, modelFromSSHWhitelistIP(service, entry, state))...)
}

func (r *sshWhitelistIPResource) Update(_ context.Context, _ resource.UpdateRequest, resp *resource.UpdateResponse) {
	addNoUpdateError(&resp.Diagnostics, "A whitelist entry")
}

func (r *sshWhitelistIPResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state sshWhitelistIPResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteSSHWhitelistIP(ctx, state.ServiceName.ValueString(), state.ID.ValueInt64())
	if err != nil {
		addAPIError(&resp.Diagnostics, "Could not remove the whitelist entry", err, sshWhitelistAttributes)
	}
}

// ImportState accepts "<service name>/<entry id>" or "<service name>/<ip>".
//
// The address form is what a person actually has, and it may carry a prefix
// length — which is why this splits on the first slash only rather than
// counting parts.
func (r *sshWhitelistIPResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	service, key, ok := importKey(
		&resp.Diagnostics, req.ID,
		"<service name>/<entry id or IP>", "virt1.example.com/217.128.0.0/24",
	)
	if !ok {
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("service_name"), service)...)
	if id, err := strconv.ParseInt(key, 10, 64); err == nil {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id)...)
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("ip"), key)...)
}

var sshWhitelistAttributes = map[string]string{
	"ip":      "ip",
	"comment": "comment",
}

func modelFromSSHWhitelistIP(
	service string, entry *zoneapi.SSHWhitelistIP, desired sshWhitelistIPResourceModel,
) sshWhitelistIPResourceModel {
	ip := optionalString(entry.IP)
	if entry.IP == "" && known(desired.IP) {
		ip = desired.IP
	}
	comment := optionalString(entry.Comment)
	if entry.Comment == "" && known(desired.Comment) {
		comment = desired.Comment
	}

	return sshWhitelistIPResourceModel{
		ServiceName: types.StringValue(service),
		ID:          types.Int64Value(entry.ID),
		IP:          ip,
		Comment:     comment,
		ResourceURL: optionalString(entry.ResourceURL),
	}
}
