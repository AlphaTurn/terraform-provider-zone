package provider

import (
	"context"
	"slices"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/AlphaTurn/terraform-provider-zone/internal/zoneapi"
)

// NewVServersDataSource returns the webhosting services data source.
func NewVServersDataSource() datasource.DataSource { return &vserversDataSource{} }

type vserversDataSource struct {
	dataSourceClient
}

var (
	_ datasource.DataSource              = (*vserversDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*vserversDataSource)(nil)
)

var vserverAttributeTypes = map[string]attr.Type{
	"name":                 types.StringType,
	"delegated":            types.StringType,
	"package":              types.StringType,
	"group":                types.StringType,
	"homedir":              types.StringType,
	"mysql_host":           types.StringType,
	"imap_host":            types.StringType,
	"pop3_host":            types.StringType,
	"smtp_host":            types.StringType,
	"mail_platform_type":   types.StringType,
	"disk_size":            types.Int64Type,
	"disk_usage_files":     types.Int64Type,
	"disk_usage_email":     types.Int64Type,
	"disk_usage_databases": types.Int64Type,
	"cron_limit":           types.Int64Type,
	"alias_limit":          types.Int64Type,
	"hosts":                types.SetType{ElemType: types.StringType},
	"aliases":              types.SetType{ElemType: types.StringType},
	"package_features":     types.SetType{ElemType: types.StringType},
	"resource_url":         types.StringType,
}

type vserversDataSourceModel struct {
	Services types.List `tfsdk:"services"`
}

func (d *vserversDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_vservers"
}

func (d *vserversDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Reads the webhosting services on the account.\n\n" +
			"Every `zone_mail_*`, `zone_mysql_*`, `zone_ssl_*`, `zone_ssh_*`, `zone_ftp_*` and " +
			"`zone_crontab` resource is scoped by a `service_name`, and this is how to find out what " +
			"those names are. A service name often looks like a domain, but it is a different " +
			"namespace from a DNS zone and the two need not match.\n\n" +
			"~> **A delegated service may not be fully readable.** When `delegated` is set, the " +
			"service belongs to another ZoneID user, and some of its endpoints answer `403` for this " +
			"account while others answer normally.",
		Attributes: map[string]schema.Attribute{
			"services": schema.ListNestedAttribute{
				Computed:            true,
				MarkdownDescription: "The services found, sorted by name.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"name":                 schema.StringAttribute{Computed: true, MarkdownDescription: "Service name. This is the `service_name` the other resources take."},
						"delegated":            schema.StringAttribute{Computed: true, MarkdownDescription: "ZoneID user this service is delegated to, or null."},
						"package":              schema.StringAttribute{Computed: true, MarkdownDescription: "Hosting package handle."},
						"group":                schema.StringAttribute{Computed: true, MarkdownDescription: "Unix group the service runs as."},
						"homedir":              schema.StringAttribute{Computed: true, MarkdownDescription: "Absolute path of the service's home directory."},
						"mysql_host":           schema.StringAttribute{Computed: true, MarkdownDescription: "Hostname to connect MySQL clients to."},
						"imap_host":            schema.StringAttribute{Computed: true, MarkdownDescription: "IMAP hostname."},
						"pop3_host":            schema.StringAttribute{Computed: true, MarkdownDescription: "POP3 hostname."},
						"smtp_host":            schema.StringAttribute{Computed: true, MarkdownDescription: "SMTP hostname."},
						"mail_platform_type":   schema.StringAttribute{Computed: true, MarkdownDescription: "Which mail platform serves this service."},
						"disk_size":            schema.Int64Attribute{Computed: true, MarkdownDescription: "Disk quota in bytes."},
						"disk_usage_files":     schema.Int64Attribute{Computed: true, MarkdownDescription: "Bytes used by files."},
						"disk_usage_email":     schema.Int64Attribute{Computed: true, MarkdownDescription: "Bytes used by mail."},
						"disk_usage_databases": schema.Int64Attribute{Computed: true, MarkdownDescription: "Bytes used by databases."},
						"cron_limit":           schema.Int64Attribute{Computed: true, MarkdownDescription: "How many crontab entries the package allows."},
						"alias_limit":          schema.Int64Attribute{Computed: true, MarkdownDescription: "How many aliases the package allows."},
						"hosts":                schema.SetAttribute{Computed: true, ElementType: types.StringType, MarkdownDescription: "Hostnames served by this service."},
						"aliases":              schema.SetAttribute{Computed: true, ElementType: types.StringType, MarkdownDescription: "Alias hostnames."},
						"package_features":     schema.SetAttribute{Computed: true, ElementType: types.StringType, MarkdownDescription: "Features the package includes."},
						"resource_url":         schema.StringAttribute{Computed: true, MarkdownDescription: "API URL of this service."},
					},
				},
			},
		},
	}
}

func (d *vserversDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config vserversDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	services, err := d.client.ListVServers(ctx)
	if err != nil {
		addAPIError(&resp.Diagnostics, "Could not read the account's webhosting services", err, nil)
		return
	}

	slices.SortFunc(services, func(a, b zoneapi.VServer) int {
		return strings.Compare(a.Name, b.Name)
	})

	elements := make([]attr.Value, 0, len(services))
	for _, service := range services {
		object, diags := vserverObject(service)
		resp.Diagnostics.Append(diags...)
		if resp.Diagnostics.HasError() {
			return
		}
		elements = append(elements, object)
	}

	list, diags := types.ListValue(types.ObjectType{AttrTypes: vserverAttributeTypes}, elements)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	config.Services = list
	resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
}

func vserverObject(service zoneapi.VServer) (types.Object, diag.Diagnostics) {
	var diags diag.Diagnostics

	hosts, hostDiags := stringSetValue(service.Hosts)
	diags.Append(hostDiags...)
	aliases, aliasDiags := stringSetValue(service.Aliases)
	diags.Append(aliasDiags...)
	features, featureDiags := stringSetValue(service.Features)
	diags.Append(featureDiags...)
	if diags.HasError() {
		return types.ObjectNull(vserverAttributeTypes), diags
	}

	object, objectDiags := types.ObjectValue(vserverAttributeTypes, map[string]attr.Value{
		"name":                 types.StringValue(service.Name),
		"delegated":            stringPointerValue(service.Delegated),
		"package":              optionalString(service.Package),
		"group":                optionalString(service.Group),
		"homedir":              optionalString(service.Homedir),
		"mysql_host":           optionalString(service.MySQLHost),
		"imap_host":            optionalString(service.IMAPHost),
		"pop3_host":            optionalString(service.POP3Host),
		"smtp_host":            optionalString(service.SMTPHost),
		"mail_platform_type":   optionalString(service.MailPlatform),
		"disk_size":            types.Int64Value(service.DiskSize),
		"disk_usage_files":     types.Int64Value(service.DiskUsageFiles),
		"disk_usage_email":     types.Int64Value(service.DiskUsageMail),
		"disk_usage_databases": types.Int64Value(service.DiskUsageDB),
		"cron_limit":           types.Int64Value(service.CronLimit),
		"alias_limit":          types.Int64Value(service.AliasLimit),
		"hosts":                hosts,
		"aliases":              aliases,
		"package_features":     features,
		"resource_url":         optionalString(service.ResourceURL),
	})
	diags.Append(objectDiags...)
	return object, diags
}
