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

// NewFTPIPWhitelistResource returns the FTP whitelist resource.
func NewFTPIPWhitelistResource() resource.Resource { return &ftpIPWhitelistResource{} }

type ftpIPWhitelistResource struct {
	resourceClient
}

var (
	_ resource.Resource                = (*ftpIPWhitelistResource)(nil)
	_ resource.ResourceWithConfigure   = (*ftpIPWhitelistResource)(nil)
	_ resource.ResourceWithImportState = (*ftpIPWhitelistResource)(nil)
)

type ftpIPWhitelistResourceModel struct {
	ServiceName types.String `tfsdk:"service_name"`
	ID          types.Int64  `tfsdk:"id"`
	IP          types.String `tfsdk:"ip"`
	Country     types.String `tfsdk:"country"`
	Created     types.String `tfsdk:"created"`
	ResourceURL types.String `tfsdk:"resource_url"`
}

func (r *ftpIPWhitelistResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_ftp_ip_whitelist"
}

func (r *ftpIPWhitelistResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "An address allowed to reach a webhosting service's FTP.\n\n" +
			"These take effect for the FTP accounts whose `access_profile` requires a whitelist.\n\n" +
			replaceOnlyNote + "\n\n" +
			"~> **The create payload for this endpoint is inferred.** zone.eu's published " +
			"description marks every field of this object read-only, including `ip`, while still " +
			"requiring a request body — which cannot both be true. The provider sends the address, " +
			"which is what the equivalent SSH endpoint documents properly. This could not be proven " +
			"without writing to a production service; please report what happens.",
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
			"country": schema.StringAttribute{
				Computed: true,
				MarkdownDescription: "The country zone.eu attributes the address to, as a two-letter " +
					"code.",
			},
			"created": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "When the entry was added.",
			},
			"resource_url": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "API URL of this entry.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *ftpIPWhitelistResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan ftpIPWhitelistResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	service := plan.ServiceName.ValueString()
	created, err := r.client.CreateFTPWhitelistIP(ctx, service, zoneapi.FTPIPWhitelistInput{
		IP: plan.IP.ValueString(),
	})
	if err != nil {
		addAPIError(&resp.Diagnostics, "Could not allow the address", err, ftpWhitelistAttributes)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, modelFromFTPWhitelistIP(service, created))...)
}

func (r *ftpIPWhitelistResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state ftpIPWhitelistResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	service := state.ServiceName.ValueString()

	var entry *zoneapi.FTPIPWhitelist
	var err error
	if known(state.ID) && state.ID.ValueInt64() != 0 {
		entry, err = r.client.GetFTPWhitelistIP(ctx, service, state.ID.ValueInt64())
	} else {
		entry, err = r.client.FindFTPWhitelistIP(ctx, service, state.IP.ValueString())
	}
	if err != nil {
		if zoneapi.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		addAPIError(&resp.Diagnostics, "Could not read the whitelist entry", err, ftpWhitelistAttributes)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, modelFromFTPWhitelistIP(service, entry))...)
}

func (r *ftpIPWhitelistResource) Update(_ context.Context, _ resource.UpdateRequest, resp *resource.UpdateResponse) {
	addNoUpdateError(&resp.Diagnostics, "A whitelist entry")
}

func (r *ftpIPWhitelistResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state ftpIPWhitelistResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteFTPWhitelistIP(ctx, state.ServiceName.ValueString(), state.ID.ValueInt64())
	if err != nil {
		addAPIError(&resp.Diagnostics, "Could not remove the whitelist entry", err, ftpWhitelistAttributes)
	}
}

// ImportState accepts "<service name>/<entry id>" or "<service name>/<ip>".
func (r *ftpIPWhitelistResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	service, key, ok := importKey(
		&resp.Diagnostics, req.ID,
		"<service name>/<entry id or IP>", "virt1.example.com/203.0.113.4",
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

var ftpWhitelistAttributes = map[string]string{"ip": "ip"}

func modelFromFTPWhitelistIP(service string, entry *zoneapi.FTPIPWhitelist) ftpIPWhitelistResourceModel {
	return ftpIPWhitelistResourceModel{
		ServiceName: types.StringValue(service),
		ID:          types.Int64Value(entry.ID),
		IP:          types.StringValue(entry.IP),
		Country:     optionalString(entry.Country),
		Created:     optionalString(entry.Created),
		ResourceURL: optionalString(entry.ResourceURL),
	}
}
