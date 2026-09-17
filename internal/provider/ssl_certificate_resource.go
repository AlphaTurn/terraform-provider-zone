package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
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

// NewSSLCertificateResource returns the SSL certificate resource.
func NewSSLCertificateResource() resource.Resource { return &sslCertificateResource{} }

type sslCertificateResource struct {
	resourceClient
}

var (
	_ resource.Resource                = (*sslCertificateResource)(nil)
	_ resource.ResourceWithConfigure   = (*sslCertificateResource)(nil)
	_ resource.ResourceWithImportState = (*sslCertificateResource)(nil)
)

type sslCertificateResourceModel struct {
	ServiceName   types.String `tfsdk:"service_name"`
	ID            types.String `tfsdk:"id"`
	Name          types.String `tfsdk:"name"`
	Certificate   types.String `tfsdk:"certificate"`
	CACertificate types.String `tfsdk:"ca_certificate"`
	PrivateKey    types.String `tfsdk:"private_key"`
	Hosts         types.Set    `tfsdk:"hosts"`
	CommonName    types.String `tfsdk:"common_name"`
	LetsEncrypt   types.Bool   `tfsdk:"letsencrypt"`
	Connected     types.Bool   `tfsdk:"connected"`
	Created       types.String `tfsdk:"created"`
	Expires       types.String `tfsdk:"expires"`
	ResourceURL   types.String `tfsdk:"resource_url"`
}

func (r *sslCertificateResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_ssl_certificate"
}

func (r *sslCertificateResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A TLS certificate installed on a webhosting service.\n\n" +
			"~> **`private_key` is a write-only argument and needs Terraform 1.11 or newer.** The " +
			"key is sent when the certificate is installed and is never written to state. That is " +
			"the whole point: state is not an encrypted store, and a private key in it is a private " +
			"key in every backup and every CI artefact that state passes through. It also matches " +
			"the API, which accepts the key and returns it as an empty string.\n\n" +
			"~> **Changing only the key produces no plan.** Terraform cannot detect a change to a " +
			"value it does not keep. In practice this rarely matters, because a new key comes with " +
			"a new `certificate`, which *is* in state, and an update re-sends both. To force a " +
			"re-upload on its own, use `terraform apply -replace`.",
		Attributes: map[string]schema.Attribute{
			"service_name": serviceNameAttribute(),
			"id": schema.StringAttribute{
				Computed: true,
				MarkdownDescription: "Identifier assigned by zone.eu. Documented as a number and " +
					"returned as a string, so it is a string here.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "A name for the certificate, shown in the ZoneID panel.",
			},
			"certificate": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "The certificate itself, PEM encoded. Pass the file's contents, " +
					"for example `file(\"cert.pem\")`.",
				Validators: []validator.String{isPEMBlock("CERTIFICATE")},
			},
			"ca_certificate": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "The issuer chain, PEM encoded. Most certificates need one, or " +
					"clients that do not already hold the intermediate will reject the connection.",
				Validators: []validator.String{isPEMBlock("CERTIFICATE")},
			},
			"private_key": schema.StringAttribute{
				Required:  true,
				WriteOnly: true,
				Sensitive: true,
				MarkdownDescription: "The certificate's private key, PEM encoded. Write-only: it is " +
					"sent to zone.eu and never stored in state.",
				Validators: []validator.String{isPEMBlock("PRIVATE KEY")},
			},
			"hosts": schema.SetAttribute{
				Optional:            true,
				Computed:            true,
				ElementType:         types.StringType,
				MarkdownDescription: "Hostnames on the service that should use this certificate.",
				Validators:          []validator.Set{setvalidator.SizeAtLeast(1)},
			},
			"common_name": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The certificate's common name, as zone.eu parsed it.",
			},
			"letsencrypt": schema.BoolAttribute{
				Computed: true,
				MarkdownDescription: "Whether this is one of zone.eu's automatically renewed Let's " +
					"Encrypt certificates. Those are managed by zone.eu, not by this provider.",
			},
			"connected": schema.BoolAttribute{
				Computed:            true,
				MarkdownDescription: "Whether the certificate is in use by an SSL service.",
			},
			"created": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "When the certificate was installed.",
			},
			"expires": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "When the certificate expires.",
			},
			"resource_url": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "API URL of this certificate.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *sslCertificateResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan sslCertificateResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	input, diags := certificateInput(ctx, plan, req.Config)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	service := plan.ServiceName.ValueString()
	created, err := r.client.CreateCertificate(ctx, service, input)
	if err != nil {
		addAPIError(&resp.Diagnostics, "Could not install the certificate", err, certificateAttributes)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, modelFromCertificate(service, created, plan))...)
}

func (r *sslCertificateResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state sslCertificateResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	service := state.ServiceName.ValueString()
	certificate, err := r.client.GetCertificate(ctx, service, state.ID.ValueString())
	if err != nil {
		if zoneapi.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		addAPIError(&resp.Diagnostics, "Could not read the certificate", err, certificateAttributes)
		return
	}

	// modelFromCertificate leaves private_key null. The framework also strips
	// write-only values from state after every operation, but not writing it in
	// the first place is what makes that a backstop rather than the mechanism.
	resp.Diagnostics.Append(resp.State.Set(ctx, modelFromCertificate(service, certificate, state))...)
}

func (r *sslCertificateResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state sslCertificateResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	input, diags := certificateInput(ctx, plan, req.Config)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	service := plan.ServiceName.ValueString()
	updated, err := r.client.UpdateCertificate(ctx, service, state.ID.ValueString(), input)
	if err != nil {
		addAPIError(&resp.Diagnostics, "Could not update the certificate", err, certificateAttributes)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, modelFromCertificate(service, updated, plan))...)
}

func (r *sslCertificateResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state sslCertificateResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteCertificate(ctx, state.ServiceName.ValueString(), state.ID.ValueString())
	if err != nil {
		addAPIError(&resp.Diagnostics, "Could not remove the certificate", err, certificateAttributes)
	}
}

// ImportState accepts "<service name>/<certificate id>".
//
// An imported certificate has no private key in state, because no state ever
// holds one. The next apply re-sends whatever the configuration supplies.
func (r *sslCertificateResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts, ok := importParts(&resp.Diagnostics, req.ID, 2, "<service name>/<certificate id>", "virt1.example.com/163957")
	if !ok {
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("service_name"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), parts[1])...)
}

var certificateAttributes = map[string]string{
	"name":           "name",
	"certificate":    "certificate",
	"ca_certificate": "ca_certificate",
	"private_key":    "private_key",
	"hosts":          "hosts",
}

// configSource is satisfied by tfsdk.Config, so the write-only key can be read
// from the configuration in both Create and Update.
type configSource interface {
	GetAttribute(ctx context.Context, path path.Path, target any) diag.Diagnostics
}

// certificateInput builds the request, taking the private key from the
// configuration rather than the plan -- see writeOnlyString for why.
func certificateInput(ctx context.Context, plan sslCertificateResourceModel, config configSource) (zoneapi.CertificateInput, diag.Diagnostics) {
	var diags diag.Diagnostics

	privateKey := writeOnlyString(ctx, config, "private_key", &diags)
	if diags.HasError() {
		return zoneapi.CertificateInput{}, diags
	}

	return zoneapi.CertificateInput{
		Name:          plan.Name.ValueString(),
		Certificate:   plan.Certificate.ValueString(),
		PrivateKey:    privateKey,
		CACertificate: plan.CACertificate.ValueString(),
		Hosts:         setToStrings(ctx, plan.Hosts, &diags),
	}, diags
}

// modelFromCertificate maps a certificate into state. desired supplies the
// values the API accepts without echoing back, the way dnsrecord's writeState
// does, so an apply does not fail with an inconsistent result.
func modelFromCertificate(
	service string, certificate *zoneapi.Certificate, desired sslCertificateResourceModel,
) sslCertificateResourceModel {
	hosts, _ := stringSetValue(certificate.Hosts)
	if certificate.Hosts == nil && known(desired.Hosts) {
		hosts = desired.Hosts
	}

	caCertificate := optionalString(certificate.CACertificate)
	if certificate.CACertificate == "" && known(desired.CACertificate) {
		caCertificate = desired.CACertificate
	}

	return sslCertificateResourceModel{
		ServiceName:   types.StringValue(service),
		ID:            types.StringValue(certificate.ID),
		Name:          optionalString(certificate.Name),
		Certificate:   optionalString(certificate.Certificate),
		CACertificate: caCertificate,
		// Never populated: the key is write-only and the API returns "".
		PrivateKey:  types.StringNull(),
		Hosts:       hosts,
		CommonName:  optionalString(certificate.CommonName),
		LetsEncrypt: types.BoolValue(certificate.LetsEncrypt),
		Connected:   types.BoolValue(certificate.Connected),
		Created:     optionalString(certificate.Created),
		Expires:     optionalString(certificate.Expires),
		ResourceURL: optionalString(certificate.ResourceURL),
	}
}
