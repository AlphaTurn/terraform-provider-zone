package provider

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/AlphaTurn/terraform-provider-zone/internal/zoneapi"
)

// NewRecordsDataSource returns the DNS records data source.
func NewRecordsDataSource() datasource.DataSource { return &recordsDataSource{} }

type recordsDataSource struct {
	client *zoneapi.Client
}

var (
	_ datasource.DataSource              = (*recordsDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*recordsDataSource)(nil)
)

// recordAttributeTypes is the shape of one element of the records list.
//
// Record types carry different extra fields, so the element type is their union
// and every type-specific attribute is null on records that do not have it.
// One flat list is far easier to filter and iterate over in HCL than eleven
// separately typed ones.
var recordAttributeTypes = map[string]attr.Type{
	"type":              types.StringType,
	"id":                types.Int64Type,
	"name":              types.StringType,
	"destination":       types.StringType,
	"resource_url":      types.StringType,
	"comment":           types.StringType,
	"modifiable":        types.BoolType,
	"deletable":         types.BoolType,
	"priority":          types.Int64Type,
	"weight":            types.Int64Type,
	"port":              types.Int64Type,
	"flag":              types.Int64Type,
	"tag":               types.StringType,
	"certificate_usage": types.Int64Type,
	"selector":          types.Int64Type,
	"matching_type":     types.Int64Type,
	"algorithm":         types.Int64Type,
	"fingerprint_type":  types.Int64Type,
	"redirect_code":     types.Int64Type,
}

// extraAttributeSources maps each union attribute to the API field it reads,
// and the record types on which it is meaningful.
var extraAttributeSources = []struct {
	attribute string
	apiField  string
	types     []zoneapi.RecordType
	isString  bool
}{
	{attribute: "priority", apiField: "priority", types: []zoneapi.RecordType{zoneapi.RecordTypeMX, zoneapi.RecordTypeSRV}},
	{attribute: "weight", apiField: "weight", types: []zoneapi.RecordType{zoneapi.RecordTypeSRV}},
	{attribute: "port", apiField: "port", types: []zoneapi.RecordType{zoneapi.RecordTypeSRV}},
	{attribute: "flag", apiField: "flag", types: []zoneapi.RecordType{zoneapi.RecordTypeCAA}},
	{attribute: "tag", apiField: "tag", types: []zoneapi.RecordType{zoneapi.RecordTypeCAA}, isString: true},
	{attribute: "certificate_usage", apiField: "certificate_usage", types: []zoneapi.RecordType{zoneapi.RecordTypeTLSA}},
	{attribute: "selector", apiField: "selector", types: []zoneapi.RecordType{zoneapi.RecordTypeTLSA}},
	{attribute: "matching_type", apiField: "matching_type", types: []zoneapi.RecordType{zoneapi.RecordTypeTLSA}},
	{attribute: "algorithm", apiField: "algorithm", types: []zoneapi.RecordType{zoneapi.RecordTypeSSHFP}},
	{attribute: "fingerprint_type", apiField: "type", types: []zoneapi.RecordType{zoneapi.RecordTypeSSHFP}},
	{attribute: "redirect_code", apiField: "type", types: []zoneapi.RecordType{zoneapi.RecordTypeURL}},
}

type recordsDataSourceModel struct {
	Zone    types.String `tfsdk:"zone"`
	Type    types.String `tfsdk:"type"`
	Records types.List   `tfsdk:"records"`
}

func (d *recordsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_dns_records"
}

func (d *recordsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Reads the DNS records in a zone, optionally of one type.\n\n" +
			"Leaving `type` unset reads every type, which costs one API request per type. Since " +
			"zone.eu allows 60 requests a minute, set `type` when only one kind is needed.",
		Attributes: map[string]schema.Attribute{
			"zone": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Zone to read, for example `example.com`.",
			},
			"type": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "Restrict the result to one record type: " +
					"`a`, `aaaa`, `cname`, `ns`, `mx`, `txt`, `srv`, `caa`, `tlsa`, `sshfp` or `url`. " +
					"All types are read when unset.",
			},
			"records": schema.ListNestedAttribute{
				Computed: true,
				MarkdownDescription: "The records found, sorted by type and then name. Attributes that " +
					"do not apply to a record's type are null.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"type":              schema.StringAttribute{Computed: true, MarkdownDescription: "Record type."},
						"id":                schema.Int64Attribute{Computed: true, MarkdownDescription: "Identifier assigned by zone.eu."},
						"name":              schema.StringAttribute{Computed: true, MarkdownDescription: "Fully qualified record name."},
						"destination":       schema.StringAttribute{Computed: true, MarkdownDescription: "Record value."},
						"resource_url":      schema.StringAttribute{Computed: true, MarkdownDescription: "API URL of this record."},
						"comment":           schema.StringAttribute{Computed: true, MarkdownDescription: "Note attached by zone.eu."},
						"modifiable":        schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether zone.eu allows this record to be changed."},
						"deletable":         schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether zone.eu allows this record to be removed."},
						"priority":          schema.Int64Attribute{Computed: true, MarkdownDescription: "MX and SRV only."},
						"weight":            schema.Int64Attribute{Computed: true, MarkdownDescription: "SRV only."},
						"port":              schema.Int64Attribute{Computed: true, MarkdownDescription: "SRV only."},
						"flag":              schema.Int64Attribute{Computed: true, MarkdownDescription: "CAA only."},
						"tag":               schema.StringAttribute{Computed: true, MarkdownDescription: "CAA only."},
						"certificate_usage": schema.Int64Attribute{Computed: true, MarkdownDescription: "TLSA only."},
						"selector":          schema.Int64Attribute{Computed: true, MarkdownDescription: "TLSA only."},
						"matching_type":     schema.Int64Attribute{Computed: true, MarkdownDescription: "TLSA only."},
						"algorithm":         schema.Int64Attribute{Computed: true, MarkdownDescription: "SSHFP only."},
						"fingerprint_type":  schema.Int64Attribute{Computed: true, MarkdownDescription: "SSHFP only."},
						"redirect_code":     schema.Int64Attribute{Computed: true, MarkdownDescription: "URL redirects only."},
					},
				},
			},
		},
	}
}

func (d *recordsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	client, ok := req.ProviderData.(*zoneapi.Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected provider data",
			fmt.Sprintf("Expected *zoneapi.Client but got %T. This is a bug in the provider.", req.ProviderData),
		)
		return
	}
	d.client = client
}

func (d *recordsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config recordsDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	wanted, diags := requestedTypes(config.Type)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	zone := config.Zone.ValueString()
	elements := make([]attr.Value, 0)

	for _, recordType := range wanted {
		records, err := d.client.ListRecords(ctx, zone, recordType)
		if err != nil {
			if zoneapi.IsNotFound(err) {
				resp.Diagnostics.AddError(
					"DNS zone not found",
					fmt.Sprintf("No DNS zone named %q is available on this ZoneID account.", zone),
				)
				return
			}
			resp.Diagnostics.AddError(
				fmt.Sprintf("Could not read %s records", strings.ToUpper(string(recordType))),
				err.Error(),
			)
			return
		}

		slices.SortFunc(records, func(a, b zoneapi.Record) int {
			if byName := strings.Compare(a.Name, b.Name); byName != 0 {
				return byName
			}
			return strings.Compare(a.Destination, b.Destination)
		})

		for _, record := range records {
			element, elementDiags := recordObject(recordType, record)
			resp.Diagnostics.Append(elementDiags...)
			if resp.Diagnostics.HasError() {
				return
			}
			elements = append(elements, element)
		}
	}

	list, listDiags := types.ListValue(types.ObjectType{AttrTypes: recordAttributeTypes}, elements)
	resp.Diagnostics.Append(listDiags...)
	if resp.Diagnostics.HasError() {
		return
	}

	config.Records = list
	resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
}

// requestedTypes resolves the type filter, keeping AllRecordTypes order so the
// result is stable between reads.
func requestedTypes(filter types.String) ([]zoneapi.RecordType, diag.Diagnostics) {
	var diags diag.Diagnostics

	if filter.IsNull() || filter.IsUnknown() || filter.ValueString() == "" {
		return zoneapi.AllRecordTypes, diags
	}

	wanted := zoneapi.RecordType(strings.ToLower(filter.ValueString()))
	if !slices.Contains(zoneapi.AllRecordTypes, wanted) {
		names := make([]string, 0, len(zoneapi.AllRecordTypes))
		for _, recordType := range zoneapi.AllRecordTypes {
			names = append(names, string(recordType))
		}
		diags.AddAttributeError(
			path.Root("type"),
			"Unknown record type",
			fmt.Sprintf("%q is not a record type this provider manages. Valid values are: %s.",
				filter.ValueString(), strings.Join(names, ", ")),
		)
		return nil, diags
	}
	return []zoneapi.RecordType{wanted}, diags
}

func recordObject(recordType zoneapi.RecordType, record zoneapi.Record) (types.Object, diag.Diagnostics) {
	values := map[string]attr.Value{
		"type":         types.StringValue(string(recordType)),
		"id":           types.Int64Value(record.ID),
		"name":         types.StringValue(record.Name),
		"destination":  types.StringValue(record.Destination),
		"resource_url": types.StringValue(record.ResourceURL),
		"comment":      types.StringNull(),
		"modifiable":   types.BoolValue(record.Modifiable),
		"deletable":    types.BoolValue(record.Deletable),
	}
	if record.Comment != "" {
		values["comment"] = types.StringValue(record.Comment)
	}

	// Start every type-specific attribute null, then fill the ones this record's
	// type actually carries.
	for attribute, attributeType := range recordAttributeTypes {
		if _, already := values[attribute]; already {
			continue
		}
		if attributeType == types.StringType {
			values[attribute] = types.StringNull()
		} else {
			values[attribute] = types.Int64Null()
		}
	}

	for _, source := range extraAttributeSources {
		if !slices.Contains(source.types, recordType) {
			continue
		}
		if source.isString {
			if value, ok := record.ExtraString(source.apiField); ok {
				values[source.attribute] = types.StringValue(value)
			}
			continue
		}
		if value, ok := record.ExtraInt64(source.apiField); ok {
			values[source.attribute] = types.Int64Value(value)
		}
	}

	return types.ObjectValue(recordAttributeTypes, values)
}
