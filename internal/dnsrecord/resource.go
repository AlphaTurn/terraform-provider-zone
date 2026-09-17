package dnsrecord

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/AlphaTurn/terraform-provider-zone/internal/zoneapi"
)

// ttlNote appears on every record resource. Users arriving from Route 53 or
// Cloudflare reach for a ttl argument immediately, and the API simply has no
// such field, so saying nothing would look like an omission in the provider.
const ttlNote = "~> **There is no `ttl` argument.** The zone.eu API does not expose a TTL on DNS " +
	"records, so neither does this provider. TTLs are managed by zone.eu."

// attributeSource is satisfied by both tfsdk.Plan and tfsdk.State, so config and
// state can be read through one code path.
type attributeSource interface {
	GetAttribute(ctx context.Context, path path.Path, target any) diag.Diagnostics
}

// Resource is the Terraform resource for a single DNS record type. All eleven
// types share this implementation, differing only in their [Definition].
type Resource struct {
	definition Definition
	client     *zoneapi.Client
}

// NewResource returns a constructor for the given record type's resource.
func NewResource(definition Definition) func() resource.Resource {
	return func() resource.Resource {
		return &Resource{definition: definition}
	}
}

var (
	_ resource.Resource                   = (*Resource)(nil)
	_ resource.ResourceWithConfigure      = (*Resource)(nil)
	_ resource.ResourceWithImportState    = (*Resource)(nil)
	_ resource.ResourceWithValidateConfig = (*Resource)(nil)
)

func (r *Resource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_" + r.definition.TypeName
}

func (r *Resource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	attributes := map[string]schema.Attribute{
		"zone": schema.StringAttribute{
			Required: true,
			MarkdownDescription: "The DNS zone holding this record, for example `example.com`. " +
				"Zones are not created by this provider: one exists for each domain or hosting " +
				"service on the account. Changing this forces a new record.",
			PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
		},
		"name": schema.StringAttribute{
			Required: true,
			MarkdownDescription: "The fully qualified record name, for example `www.example.com`. " +
				"zone.eu stores names in full rather than relative to the zone, so a bare `www` " +
				"is rejected. Use the zone name itself for a record at the apex.",
		},
		"destination": schema.StringAttribute{
			Required:            true,
			MarkdownDescription: r.definition.DestinationDescription,
			Validators:          r.definition.DestinationValidators,
		},
		"id": schema.Int64Attribute{
			Computed:            true,
			MarkdownDescription: "Numeric identifier assigned by zone.eu.",
			PlanModifiers:       []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
		},
		"resource_url": schema.StringAttribute{
			Computed:            true,
			MarkdownDescription: "API URL of this record.",
			PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
		},
		"comment": schema.StringAttribute{
			Computed:            true,
			MarkdownDescription: "Note attached to the record by zone.eu. Read-only.",
		},
		"modifiable": schema.BoolAttribute{
			Computed: true,
			MarkdownDescription: "Whether zone.eu allows this record to be changed. Records it " +
				"manages itself report `false`, and an update to one will fail.",
		},
		"deletable": schema.BoolAttribute{
			Computed: true,
			MarkdownDescription: "Whether zone.eu allows this record to be removed. Records it " +
				"manages itself report `false`, and a destroy of one will fail.",
		},
	}

	for _, extra := range r.definition.Extras {
		attributes[extra.Name] = extra.schemaAttribute()
	}

	resp.Schema = schema.Schema{
		MarkdownDescription: r.definition.Description + "\n\n" + ttlNote,
		Attributes:          attributes,
	}
}

func (e Extra) schemaAttribute() schema.Attribute {
	if e.Kind == KindString {
		return schema.StringAttribute{
			Required:            e.Required,
			Optional:            !e.Required,
			MarkdownDescription: e.Description,
			Validators:          e.StringValidators,
		}
	}

	attribute := schema.Int64Attribute{
		Required:            e.Required,
		Optional:            !e.Required,
		MarkdownDescription: e.Description,
		Validators:          e.Int64Validators,
	}
	if e.Int64Default != nil {
		// A default has to be Computed so the framework may fill it in.
		attribute.Computed = true
		attribute.Default = int64default.StaticInt64(*e.Int64Default)
	}
	return attribute
}

func (r *Resource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return // Configure runs before the provider is configured during validation.
	}

	client, ok := req.ProviderData.(*zoneapi.Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected provider data",
			fmt.Sprintf("Expected *zoneapi.Client but got %T. This is a bug in the provider.", req.ProviderData),
		)
		return
	}
	r.client = client
}

// ValidateConfig checks the record name against the zone.
//
// The API stores names fully qualified, and rejects a bare label. Catching that
// at validation gives a useful message with the fix in it, instead of a 422
// after the user has already run apply.
func (r *Resource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var zone, name types.String
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("zone"), &zone)...)
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("name"), &name)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if zone.IsNull() || zone.IsUnknown() || name.IsNull() || name.IsUnknown() {
		return
	}

	zoneName := strings.ToLower(strings.TrimSuffix(zone.ValueString(), "."))
	recordName := strings.ToLower(strings.TrimSuffix(name.ValueString(), "."))
	if recordName == zoneName || strings.HasSuffix(recordName, "."+zoneName) {
		return
	}

	resp.Diagnostics.AddAttributeError(
		path.Root("name"),
		"Record name must be fully qualified within the zone",
		fmt.Sprintf(
			"zone.eu stores record names in full, not relative to the zone, so %q is not a name in zone %q.\n\n"+
				"Use %q for a record at that label, or %q for the zone apex.",
			name.ValueString(), zone.ValueString(),
			name.ValueString()+"."+zone.ValueString(), zone.ValueString(),
		),
	)
}

// readRecord builds an API record from a plan or state.
func (r *Resource) readRecord(ctx context.Context, source attributeSource) (string, zoneapi.Record, diag.Diagnostics) {
	var diags diag.Diagnostics
	var zone, name, destination types.String

	diags.Append(source.GetAttribute(ctx, path.Root("zone"), &zone)...)
	diags.Append(source.GetAttribute(ctx, path.Root("name"), &name)...)
	diags.Append(source.GetAttribute(ctx, path.Root("destination"), &destination)...)
	if diags.HasError() {
		return "", zoneapi.Record{}, diags
	}

	record := zoneapi.Record{
		Name:        name.ValueString(),
		Destination: destination.ValueString(),
	}

	for _, extra := range r.definition.Extras {
		if extra.Kind == KindString {
			var value types.String
			diags.Append(source.GetAttribute(ctx, path.Root(extra.Name), &value)...)
			if !value.IsNull() && !value.IsUnknown() {
				record.SetExtra(extra.APIName(), value.ValueString())
			}
			continue
		}

		var value types.Int64
		diags.Append(source.GetAttribute(ctx, path.Root(extra.Name), &value)...)
		if !value.IsNull() && !value.IsUnknown() {
			record.SetExtra(extra.APIName(), value.ValueInt64())
		}
	}

	return zone.ValueString(), record, diags
}

// writeState stores an API record.
//
// desired, when non-nil, supplies values for type-specific fields the API did
// not echo back. Terraform requires every configured attribute to hold its
// planned value after an apply, and an endpoint that accepts a field without
// returning it would otherwise produce an "inconsistent result" error rather
// than the record the user asked for.
func (r *Resource) writeState(ctx context.Context, state *tfsdk.State, zone string, record *zoneapi.Record, desired *zoneapi.Record) diag.Diagnostics {
	var diags diag.Diagnostics

	diags.Append(state.SetAttribute(ctx, path.Root("zone"), zone)...)
	diags.Append(state.SetAttribute(ctx, path.Root("id"), record.ID)...)
	diags.Append(state.SetAttribute(ctx, path.Root("name"), record.Name)...)
	diags.Append(state.SetAttribute(ctx, path.Root("destination"), record.Destination)...)
	diags.Append(state.SetAttribute(ctx, path.Root("resource_url"), record.ResourceURL)...)
	diags.Append(state.SetAttribute(ctx, path.Root("modifiable"), record.Modifiable)...)
	diags.Append(state.SetAttribute(ctx, path.Root("deletable"), record.Deletable)...)

	if record.Comment == "" {
		diags.Append(state.SetAttribute(ctx, path.Root("comment"), types.StringNull())...)
	} else {
		diags.Append(state.SetAttribute(ctx, path.Root("comment"), record.Comment)...)
	}

	for _, extra := range r.definition.Extras {
		attributePath := path.Root(extra.Name)
		apiField := extra.APIName()

		if extra.Kind == KindString {
			value, ok := record.ExtraString(apiField)
			if !ok && desired != nil {
				value, ok = desired.ExtraString(apiField)
			}
			if !ok {
				diags.Append(state.SetAttribute(ctx, attributePath, types.StringNull())...)
				continue
			}
			diags.Append(state.SetAttribute(ctx, attributePath, value)...)
			continue
		}

		value, ok := record.ExtraInt64(apiField)
		if !ok && desired != nil {
			value, ok = desired.ExtraInt64(apiField)
		}
		if !ok {
			diags.Append(state.SetAttribute(ctx, attributePath, types.Int64Null())...)
			continue
		}
		diags.Append(state.SetAttribute(ctx, attributePath, value)...)
	}

	return diags
}

func (r *Resource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	zone, record, diags := r.readRecord(ctx, req.Plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	created, err := r.client.CreateRecord(ctx, zone, r.definition.RecordType, record)
	if err != nil {
		r.addAPIError(&resp.Diagnostics, "create", err)
		return
	}

	resp.Diagnostics.Append(r.writeState(ctx, &resp.State, zone, created, &record)...)
}

func (r *Resource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var zone types.String
	var id types.Int64

	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("zone"), &zone)...)
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("id"), &id)...)
	if resp.Diagnostics.HasError() {
		return
	}

	record, err := r.client.GetRecord(ctx, zone.ValueString(), r.definition.RecordType, id.ValueInt64())
	if err != nil {
		if zoneapi.IsNotFound(err) {
			// Removed outside Terraform. Dropping it from state lets the next
			// plan offer to recreate it rather than failing the refresh.
			resp.State.RemoveResource(ctx)
			return
		}
		r.addAPIError(&resp.Diagnostics, "read", err)
		return
	}

	resp.Diagnostics.Append(r.writeState(ctx, &resp.State, zone.ValueString(), record, nil)...)
}

func (r *Resource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	zone, record, diags := r.readRecord(ctx, req.Plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	var id types.Int64
	var modifiable types.Bool
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("id"), &id)...)
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("modifiable"), &modifiable)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if !modifiable.IsNull() && !modifiable.IsUnknown() && !modifiable.ValueBool() {
		resp.Diagnostics.AddError(
			"Record cannot be modified",
			fmt.Sprintf(
				"zone.eu reports that %s record %d in zone %q is managed on its side and will not accept changes.\n\n"+
					"Records like this are created with the zone and maintained by zone.eu. Remove this "+
					"resource from the configuration, or change the record in the ZoneID panel if it allows it.",
				strings.ToUpper(string(r.definition.RecordType)), id.ValueInt64(), zone,
			),
		)
		return
	}

	updated, err := r.client.UpdateRecord(ctx, zone, r.definition.RecordType, id.ValueInt64(), record)
	if err != nil {
		r.addAPIError(&resp.Diagnostics, "update", err)
		return
	}

	resp.Diagnostics.Append(r.writeState(ctx, &resp.State, zone, updated, &record)...)
}

func (r *Resource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var zone types.String
	var id types.Int64
	var deletable types.Bool

	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("zone"), &zone)...)
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("id"), &id)...)
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("deletable"), &deletable)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if !deletable.IsNull() && !deletable.IsUnknown() && !deletable.ValueBool() {
		resp.Diagnostics.AddError(
			"Record cannot be deleted",
			fmt.Sprintf(
				"zone.eu reports that %s record %d in zone %q is managed on its side and will not accept deletion.\n\n"+
					"Use `terraform state rm` to stop managing it without attempting to remove it.",
				strings.ToUpper(string(r.definition.RecordType)), id.ValueInt64(), zone.ValueString(),
			),
		)
		return
	}

	if err := r.client.DeleteRecord(ctx, zone.ValueString(), r.definition.RecordType, id.ValueInt64()); err != nil {
		r.addAPIError(&resp.Diagnostics, "delete", err)
	}
}

// ImportState accepts "<zone>/<record id>". Terraform calls Read afterwards to
// fill in the rest.
func (r *Resource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	zone, rawID, found := strings.Cut(req.ID, "/")
	if !found || zone == "" || rawID == "" {
		resp.Diagnostics.AddError(
			"Invalid import ID",
			fmt.Sprintf("Expected \"<zone>/<record id>\", for example \"example.com/12345\", but got %q.", req.ID),
		)
		return
	}

	id, err := strconv.ParseInt(rawID, 10, 64)
	if err != nil {
		resp.Diagnostics.AddError(
			"Invalid import ID",
			fmt.Sprintf("Expected a numeric record ID after the zone, but %q could not be parsed.\n\n"+
				"Record IDs are visible in the resource_url of an existing record, or from the API.", rawID),
		)
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("zone"), zone)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id)...)
}

// attributeForAPIField maps a field name from an API error back to the
// Terraform attribute that set it.
func (r *Resource) attributeForAPIField(field string) (string, bool) {
	switch field {
	case "name", "destination":
		return field, true
	}
	for _, extra := range r.definition.Extras {
		if extra.APIName() == field {
			return extra.Name, true
		}
	}
	return "", false
}

// addAPIError turns a client error into diagnostics, attaching validation
// failures to the attribute that caused them so the user is pointed at the line
// to fix rather than handed one opaque message.
func (r *Resource) addAPIError(diags *diag.Diagnostics, action string, err error) {
	summary := fmt.Sprintf("Could not %s %s record", action, strings.ToUpper(string(r.definition.RecordType)))

	apiErr, ok := zoneapi.AsAPIError(err)
	if !ok {
		diags.AddError(summary, err.Error())
		return
	}

	attached := false
	for field, messages := range apiErr.FieldErrors {
		attribute, known := r.attributeForAPIField(field)
		if !known {
			continue
		}
		diags.AddAttributeError(
			path.Root(attribute),
			"zone.eu rejected this value",
			strings.Join(messages, "\n"),
		)
		attached = true
	}
	if attached {
		return
	}

	diags.AddError(summary, apiErr.Error())
}
