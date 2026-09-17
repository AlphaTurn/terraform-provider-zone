package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/AlphaTurn/terraform-provider-zone/internal/zoneapi"
)

// NewDomainContactResource returns the domain contact resource.
func NewDomainContactResource() resource.Resource { return &domainContactResource{} }

type domainContactResource struct {
	resourceClient
}

var (
	_ resource.Resource                   = (*domainContactResource)(nil)
	_ resource.ResourceWithConfigure      = (*domainContactResource)(nil)
	_ resource.ResourceWithImportState    = (*domainContactResource)(nil)
	_ resource.ResourceWithValidateConfig = (*domainContactResource)(nil)
)

type domainContactResourceModel struct {
	Domain         types.String `tfsdk:"domain"`
	ID             types.String `tfsdk:"id"`
	Role           types.String `tfsdk:"role"`
	Type           types.String `tfsdk:"type"`
	Name           types.String `tfsdk:"name"`
	FirstName      types.String `tfsdk:"first_name"`
	LastName       types.String `tfsdk:"last_name"`
	Organization   types.String `tfsdk:"organization"`
	Email          types.String `tfsdk:"email"`
	Voice          types.String `tfsdk:"voice"`
	Fax            types.String `tfsdk:"fax"`
	Country        types.String `tfsdk:"country"`
	State          types.String `tfsdk:"state"`
	City           types.String `tfsdk:"city"`
	Street         types.String `tfsdk:"street"`
	Postalcode     types.String `tfsdk:"postalcode"`
	RegistryHandle types.String `tfsdk:"registry_handle"`
	ResourceURL    types.String `tfsdk:"resource_url"`

	ExtLanguage             types.String `tfsdk:"ext_language"`
	ExtIdent                types.String `tfsdk:"ext_ident"`
	ExtIdentType            types.String `tfsdk:"ext_ident_type"`
	ExtIdentCC              types.String `tfsdk:"ext_ident_cc"`
	ExtVATNr                types.String `tfsdk:"ext_vatnr"`
	ExtDepartment           types.String `tfsdk:"ext_department"`
	ExtPassport             types.String `tfsdk:"ext_passport"`
	ExtLegalForm            types.String `tfsdk:"ext_legal_form"`
	ExtCountryOfCitizenship types.String `tfsdk:"ext_country_of_citizenship"`
}

func (r *domainContactResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_domain_contact"
}

// registryField is an optional contact field the registry may normalise, so
// each one is Optional and Computed.
func registryField(description string) schema.StringAttribute {
	return schema.StringAttribute{
		Optional:            true,
		Computed:            true,
		MarkdownDescription: description,
	}
}

func (r *domainContactResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	const extNote = "Registry extension, meaningful only for some TLDs. "

	resp.Schema = schema.Schema{
		MarkdownDescription: "A registry contact attached to a domain.\n\n" +
			"Registries normalise contact details and some reject changes outright, so most " +
			"arguments here are optional and computed: whatever is not configured takes the value " +
			"the registry holds.\n\n" +
			"~> **The registrant contact usually cannot be replaced this way.** Registries treat a " +
			"change of registrant as a trade, with its own process and often a fee. Expect the API " +
			"to refuse a destroy or a role change on a registrant.",
		Attributes: map[string]schema.Attribute{
			"domain": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Domain this contact belongs to, for example `example.com`.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators:          []validator.String{noPathSeparator()},
			},
			"id": schema.StringAttribute{
				Computed: true,
				MarkdownDescription: "Identifier assigned by zone.eu. Documented as a number and " +
					"returned as a string, so it is a string here and both forms are accepted.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"role": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "What this contact is for: `registrant`, `admin` or `tech`. " +
					"Changing it forces a new contact.",
				Validators: []validator.String{
					stringvalidator.OneOf("registrant", "admin", "tech"),
				},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"type": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Contact type as zone.eu classifies it. Read-only.",
			},
			"name":         registryField("Full name. Set this or `first_name` and `last_name`."),
			"first_name":   registryField("Given name."),
			"last_name":    registryField("Family name."),
			"organization": registryField("Organisation, when the contact is a company."),
			"email": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Contact email address.",
				Validators:          []validator.String{isEmailAddress()},
			},
			"voice":      registryField("Telephone number in international form, for example `+372.5551234`."),
			"fax":        registryField("Fax number, if the registry still asks for one."),
			"country":    registryField("Two-letter ISO country code, for example `EE`."),
			"state":      registryField("State or county, where the registry uses one."),
			"city":       registryField("City."),
			"street":     registryField("Street address."),
			"postalcode": registryField("Postal code."),
			"registry_handle": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The registry's own identifier for this contact. Read-only.",
			},
			"resource_url": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "API URL of this contact.",
			},

			"ext_language": registryField(extNote + "Preferred language."),
			"ext_ident": registryField(extNote + "Identity or registration number. Required for an " +
				"`.ee` registrant, together with `ext_ident_type` and `ext_ident_cc`."),
			"ext_ident_type": schema.StringAttribute{
				Optional: true,
				Computed: true,
				MarkdownDescription: extNote + "What `ext_ident` holds: `private_number`, " +
					"`company_number` or `birthday`.",
				Validators: []validator.String{
					stringvalidator.OneOf("private_number", "company_number", "birthday"),
				},
			},
			"ext_ident_cc":               registryField(extNote + "Country that issued `ext_ident`, as a two-letter code."),
			"ext_vatnr":                  registryField(extNote + "VAT number."),
			"ext_department":             registryField(extNote + "Department within the organisation."),
			"ext_passport":               registryField(extNote + "Passport number."),
			"ext_legal_form":             registryField(extNote + "Legal form of the organisation."),
			"ext_country_of_citizenship": registryField(extNote + "Country of citizenship."),
		},
	}
}

// ValidateConfig insists on something to call the contact by. Registries reject
// a contact with no name, and saying so before an apply is kinder than a 422.
func (r *domainContactResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var config domainContactResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if known(config.Name) || known(config.FirstName) || known(config.LastName) {
		return
	}
	resp.Diagnostics.AddAttributeError(
		path.Root("name"),
		"Contact needs a name",
		"Registries reject a contact with no name. Set `name` to the full name, or set "+
			"`first_name` and `last_name`.",
	)
}

func (r *domainContactResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan domainContactResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	domain := plan.Domain.ValueString()
	created, err := r.client.CreateContact(ctx, domain, contactInput(plan))
	if err != nil {
		addAPIError(&resp.Diagnostics, "Could not create the domain contact", err, contactAttributes)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, modelFromContact(domain, created))...)
}

func (r *domainContactResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state domainContactResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	domain := state.Domain.ValueString()
	contact, err := r.client.GetContact(ctx, domain, state.ID.ValueString())
	if err != nil {
		if zoneapi.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		addAPIError(&resp.Diagnostics, "Could not read the domain contact", err, contactAttributes)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, modelFromContact(domain, contact))...)
}

func (r *domainContactResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state domainContactResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	domain := plan.Domain.ValueString()
	updated, err := r.client.UpdateContact(ctx, domain, state.ID.ValueString(), contactInput(plan))
	if err != nil {
		addAPIError(&resp.Diagnostics, "Could not update the domain contact", err, contactAttributes)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, modelFromContact(domain, updated))...)
}

func (r *domainContactResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state domainContactResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteContact(ctx, state.Domain.ValueString(), state.ID.ValueString())
	if err != nil {
		addAPIError(&resp.Diagnostics, "Could not delete the domain contact", err, contactAttributes)
	}
}

// ImportState accepts "<domain>/<contact id>".
func (r *domainContactResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts, ok := importParts(&resp.Diagnostics, req.ID, 2, "<domain>/<contact id>", "example.com/12345")
	if !ok {
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("domain"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), parts[1])...)
}

var contactAttributes = map[string]string{
	"role":                       "role",
	"name":                       "name",
	"first_name":                 "first_name",
	"last_name":                  "last_name",
	"organization":               "organization",
	"email":                      "email",
	"voice":                      "voice",
	"fax":                        "fax",
	"country":                    "country",
	"state":                      "state",
	"city":                       "city",
	"street":                     "street",
	"postalcode":                 "postalcode",
	"ext_language":               "ext_language",
	"ext_ident":                  "ext_ident",
	"ext_ident_type":             "ext_ident_type",
	"ext_ident_cc":               "ext_ident_cc",
	"ext_vatnr":                  "ext_vatnr",
	"ext_department":             "ext_department",
	"ext_passport":               "ext_passport",
	"ext_legal_form":             "ext_legal_form",
	"ext_country_of_citizenship": "ext_country_of_citizenship",
}

func contactInput(model domainContactResourceModel) zoneapi.ContactInput {
	return zoneapi.ContactInput{
		Role:                    model.Role.ValueString(),
		Name:                    model.Name.ValueString(),
		FirstName:               model.FirstName.ValueString(),
		LastName:                model.LastName.ValueString(),
		Organization:            model.Organization.ValueString(),
		Email:                   model.Email.ValueString(),
		Voice:                   model.Voice.ValueString(),
		Fax:                     model.Fax.ValueString(),
		Country:                 model.Country.ValueString(),
		State:                   model.State.ValueString(),
		City:                    model.City.ValueString(),
		Street:                  model.Street.ValueString(),
		Postalcode:              model.Postalcode.ValueString(),
		ExtLanguage:             model.ExtLanguage.ValueString(),
		ExtIdent:                model.ExtIdent.ValueString(),
		ExtIdentType:            model.ExtIdentType.ValueString(),
		ExtIdentCC:              model.ExtIdentCC.ValueString(),
		ExtVATNr:                model.ExtVATNr.ValueString(),
		ExtDepartment:           model.ExtDepartment.ValueString(),
		ExtPassport:             model.ExtPassport.ValueString(),
		ExtLegalForm:            model.ExtLegalForm.ValueString(),
		ExtCountryOfCitizenship: model.ExtCountryOfCitizenship.ValueString(),
	}
}

func modelFromContact(domain string, contact *zoneapi.Contact) domainContactResourceModel {
	return domainContactResourceModel{
		Domain:                  types.StringValue(domain),
		ID:                      types.StringValue(contact.Identificator),
		Role:                    optionalString(contact.Role),
		Type:                    optionalString(contact.Type),
		Name:                    optionalString(contact.Name),
		FirstName:               optionalString(contact.FirstName),
		LastName:                optionalString(contact.LastName),
		Organization:            optionalString(contact.Organization),
		Email:                   optionalString(contact.Email),
		Voice:                   optionalString(contact.Voice),
		Fax:                     optionalString(contact.Fax),
		Country:                 optionalString(contact.Country),
		State:                   optionalString(contact.State),
		City:                    optionalString(contact.City),
		Street:                  optionalString(contact.Street),
		Postalcode:              optionalString(contact.Postalcode),
		RegistryHandle:          optionalString(contact.RegistryHandle),
		ResourceURL:             optionalString(contact.ResourceURL),
		ExtLanguage:             optionalString(contact.ExtLanguage),
		ExtIdent:                optionalString(contact.ExtIdent),
		ExtIdentType:            optionalString(contact.ExtIdentType),
		ExtIdentCC:              optionalString(contact.ExtIdentCC),
		ExtVATNr:                optionalString(contact.ExtVATNr),
		ExtDepartment:           optionalString(contact.ExtDepartment),
		ExtPassport:             optionalString(contact.ExtPassport),
		ExtLegalForm:            optionalString(contact.ExtLegalForm),
		ExtCountryOfCitizenship: optionalString(contact.ExtCountryOfCitizenship),
	}
}
