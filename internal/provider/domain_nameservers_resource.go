package provider

import (
	"context"
	"slices"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/AlphaTurn/terraform-provider-zone/internal/zoneapi"
)

// NewDomainNameserversResource returns the domain delegation resource.
func NewDomainNameserversResource() resource.Resource { return &domainNameserversResource{} }

type domainNameserversResource struct {
	resourceClient
}

var (
	_ resource.Resource                = (*domainNameserversResource)(nil)
	_ resource.ResourceWithConfigure   = (*domainNameserversResource)(nil)
	_ resource.ResourceWithImportState = (*domainNameserversResource)(nil)
)

// nameserverAttributeTypes is the shape of one nameserver.
var nameserverAttributeTypes = map[string]attr.Type{
	"hostname": types.StringType,
	"ip":       types.SetType{ElemType: types.StringType},
}

type domainNameserversResourceModel struct {
	Domain      types.String `tfsdk:"domain"`
	Nameservers types.Set    `tfsdk:"nameserver"`
}

func (r *domainNameserversResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_domain_nameservers"
}

func (r *domainNameserversResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "The whole nameserver delegation of a domain.\n\n" +
			"This is one resource for the entire set rather than one per nameserver, because that is " +
			"how the API and the registries work: the endpoint takes an array and replaces the " +
			"delegation in one operation, and most registries refuse a delegation of a single " +
			"nameserver. A per-nameserver resource would ask Terraform to add them one at a time and " +
			"could leave a domain half-delegated.\n\n" +
			"~> **Destroying this resource does not undelegate the domain.** There is no such thing " +
			"as a domain with no nameservers, so destroy only stops Terraform managing the " +
			"delegation.\n\n" +
			"~> **Do not also set `nameservers_custom` on `zone_domain`.** The two would fight on " +
			"every apply, and each apply is a registry-visible delegation change.",
		Attributes: map[string]schema.Attribute{
			"domain": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Domain to delegate, for example `example.com`.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators:          []validator.String{noPathSeparator()},
			},
			"nameserver": schema.SetNestedAttribute{
				Required: true,
				MarkdownDescription: "The nameservers the domain is delegated to. Replaces the " +
					"delegation wholesale, so this must list every nameserver, not just additions.",
				Validators: []validator.Set{setvalidator.SizeAtLeast(2)},
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"hostname": schema.StringAttribute{
							Required:            true,
							MarkdownDescription: "Nameserver hostname, for example `ns.zone.eu`.",
							Validators:          []validator.String{noPathSeparator()},
						},
						"ip": schema.SetAttribute{
							Optional:    true,
							ElementType: types.StringType,
							MarkdownDescription: "Glue addresses for this nameserver. Only needed when " +
								"the nameserver is inside the domain it serves; leave unset otherwise.",
							Validators: []validator.Set{
								setvalidator.ValueStringsAre(isIPOrPrefix()),
							},
						},
					},
				},
			},
		},
	}
}

func (r *domainNameserversResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	r.apply(ctx, req.Plan.Get, &resp.State, &resp.Diagnostics)
}

func (r *domainNameserversResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	r.apply(ctx, req.Plan.Get, &resp.State, &resp.Diagnostics)
}

func (r *domainNameserversResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state domainNameserversResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	domain := state.Domain.ValueString()
	nameservers, err := r.client.ListNameservers(ctx, domain)
	if err != nil {
		if zoneapi.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		addAPIError(&resp.Diagnostics, "Could not read the domain's nameservers", err, nameserverAttributes)
		return
	}
	if len(nameservers) == 0 {
		// A domain with no delegation at all is not a state the registry keeps,
		// so this means the domain itself is gone.
		resp.State.RemoveResource(ctx)
		return
	}

	set, diags := nameserverSetValue(nameservers)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, domainNameserversResourceModel{
		Domain:      types.StringValue(domain),
		Nameservers: set,
	})...)
}

// Delete leaves the delegation in place; see the resource description.
func (r *domainNameserversResource) Delete(_ context.Context, _ resource.DeleteRequest, resp *resource.DeleteResponse) {
	resp.Diagnostics.AddWarning(
		"Nameserver delegation left in place",
		"A domain cannot have no nameservers, so the delegation is unchanged and the domain still "+
			"resolves. Terraform has only stopped managing it.",
	)
}

func (r *domainNameserversResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("domain"), req.ID)...)
}

type attributeGetter func(ctx context.Context, target any) diag.Diagnostics

// apply writes the whole delegation. Create and Update are the same operation
// here, because the endpoint replaces the set either way.
func (r *domainNameserversResource) apply(
	ctx context.Context, get attributeGetter, state *tfsdk.State, diags *diag.Diagnostics,
) {
	var plan domainNameserversResourceModel
	diags.Append(get(ctx, &plan)...)
	if diags.HasError() {
		return
	}

	domain := plan.Domain.ValueString()
	desired, readDiags := nameserverInputs(ctx, plan.Nameservers)
	diags.Append(readDiags...)
	if diags.HasError() {
		return
	}

	stored, err := r.client.SetNameservers(ctx, domain, desired)
	if err != nil {
		addAPIError(diags, "Could not set the domain's nameservers", err, nameserverAttributes)
		return
	}

	// The endpoint is documented as taking the whole set, but that is inferred
	// from its array body rather than stated. If it turns out to add rather than
	// replace, anything left over has to go, or the delegation would accumulate
	// every nameserver ever configured.
	wanted := make([]string, 0, len(desired))
	for _, ns := range desired {
		wanted = append(wanted, strings.ToLower(ns.Hostname))
	}
	for _, existing := range stored {
		if slices.Contains(wanted, strings.ToLower(existing.Hostname)) {
			continue
		}
		if err := r.client.DeleteNameserver(ctx, domain, existing.Hostname); err != nil {
			addAPIError(diags, "Could not remove a nameserver the configuration no longer lists", err, nameserverAttributes)
			return
		}
	}

	// State records what was asked for rather than what came back: the response
	// may carry leftovers that were just deleted, and glue addresses are not
	// echoed for nameservers outside the domain.
	diags.Append(state.Set(ctx, plan)...)
}

var nameserverAttributes = map[string]string{
	"hostname": "nameserver",
	"ip":       "nameserver",
}

func nameserverInputs(ctx context.Context, set types.Set) ([]zoneapi.NameserverInput, diag.Diagnostics) {
	var diags diag.Diagnostics

	var models []struct {
		Hostname types.String `tfsdk:"hostname"`
		IP       types.Set    `tfsdk:"ip"`
	}
	diags.Append(set.ElementsAs(ctx, &models, false)...)
	if diags.HasError() {
		return nil, diags
	}

	inputs := make([]zoneapi.NameserverInput, 0, len(models))
	for _, model := range models {
		inputs = append(inputs, zoneapi.NameserverInput{
			Hostname: model.Hostname.ValueString(),
			IP:       setToStrings(ctx, model.IP, &diags),
		})
	}
	return inputs, diags
}

func nameserverSetValue(nameservers []zoneapi.Nameserver) (types.Set, diag.Diagnostics) {
	var diags diag.Diagnostics

	elements := make([]attr.Value, 0, len(nameservers))
	for _, ns := range nameservers {
		ips, ipDiags := stringSetValue(ns.IP)
		diags.Append(ipDiags...)

		object, objectDiags := types.ObjectValue(nameserverAttributeTypes, map[string]attr.Value{
			"hostname": types.StringValue(ns.Hostname),
			"ip":       ips,
		})
		diags.Append(objectDiags...)
		elements = append(elements, object)
	}
	if diags.HasError() {
		return types.SetNull(types.ObjectType{AttrTypes: nameserverAttributeTypes}), diags
	}

	set, setDiags := types.SetValue(types.ObjectType{AttrTypes: nameserverAttributeTypes}, elements)
	diags.Append(setDiags...)
	return set, diags
}
