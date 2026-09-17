package zoneapi

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
)

// VServer is a virtual server — a webhosting service. Its name is the
// service_name every /vserver endpoint is scoped by.
//
// The listing endpoint this reads is not in zone.eu's published API
// description at all, which documents nothing shallower than
// /vserver/{service_name}/..., but it exists and it is the only way to
// discover which service names an account may use.
type VServer struct {
	Name           string
	Identificator  string
	ResourceURL    string
	Group          string
	Homedir        string
	Package        string
	MySQLHost      string
	IMAPHost       string
	POP3Host       string
	SMTPHost       string
	MailPlatform   string
	DiskSize       int64
	DiskUsageFiles int64
	DiskUsageMail  int64
	DiskUsageDB    int64
	CronLimit      int64
	AliasLimit     int64
	Hosts          []string
	Aliases        []string
	Features       []string

	// Delegated names the ZoneID user this service is delegated to, and is
	// null when it is not delegated. A delegated service can refuse some
	// endpoints with a 403 while allowing others.
	Delegated *string
}

func (v *VServer) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := decodeFields(data, &fields); err != nil {
		return err
	}

	for target, field := range map[*string]string{
		&v.Name:          "name",
		&v.Identificator: "identificator",
		&v.ResourceURL:   "resource_url",
		&v.Group:         "group",
		&v.Homedir:       "homedir",
		&v.Package:       "package",
		&v.MySQLHost:     "mysql_host",
		&v.IMAPHost:      "imap_host",
		&v.POP3Host:      "pop3_host",
		&v.SMTPHost:      "smtp_host",
		&v.MailPlatform:  "mail_platform_type",
	} {
		*target, _ = coerceString(fields[field])
	}

	for target, field := range map[*int64]string{
		&v.DiskSize:       "disk_size",
		&v.DiskUsageFiles: "disk_usage_files",
		&v.DiskUsageMail:  "disk_usage_email",
		&v.DiskUsageDB:    "disk_usage_databases",
		&v.CronLimit:      "cron_limit",
		&v.AliasLimit:     "alias_limit",
	} {
		*target, _ = coerceInt64(fields[field])
	}

	v.Hosts, _ = coerceStringSlice(fields["hosts"])
	v.Aliases, _ = coerceStringSlice(fields["aliases"])
	v.Features, _ = coerceStringSlice(fields["package_features"])
	v.Delegated = coerceOptionalString(fields["delegated"])
	return nil
}

func vserverPath(service string) string { return "/vserver/" + url.PathEscape(service) }

// ListVServers returns the webhosting services the account can use.
func (c *Client) ListVServers(ctx context.Context) ([]VServer, error) {
	raws, err := c.listPaged(ctx, "/vserver")
	if err != nil {
		return nil, err
	}
	return decodeList(raws, func(v *VServer) string { return strings.ToLower(v.Name) })
}
