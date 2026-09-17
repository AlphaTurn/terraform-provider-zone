package zoneapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// MySQLDatabase is a database on a webhosting service.
//
// There is no PUT for a database, so nothing about one can be changed in
// place — not even the comment, which the schema calls writable.
type MySQLDatabase struct {
	Name             string
	Comment          string
	DiskUsage        string
	DiskUsageHuman   string
	DiskUsageUpdated string
	ResourceURL      string
}

func (d *MySQLDatabase) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := decodeFields(data, &fields); err != nil {
		return err
	}

	d.Name, _ = coerceString(fields["name"])
	if d.Name == "" {
		d.Name, _ = coerceString(fields["identificator"])
	}
	for target, field := range map[*string]string{
		&d.Comment: "comment",
		// Documented and returned as a string, unlike a mail account's
		// disk_usage, which is a number. Kept as text rather than coerced, so
		// nothing is lost if it ever exceeds an int64.
		&d.DiskUsage:        "disk_usage",
		&d.DiskUsageHuman:   "disk_usage_human",
		&d.DiskUsageUpdated: "disk_usage_updated",
		&d.ResourceURL:      "resource_url",
	} {
		*target, _ = coerceString(fields[field])
	}
	return nil
}

// MySQLDatabaseInput is a database as submitted. Collation is create-only and
// the API never returns it.
type MySQLDatabaseInput struct {
	Name      string `json:"name"`
	Comment   string `json:"comment,omitempty"`
	Collation string `json:"collation,omitempty"`
}

// MySQLAccount is a database user.
type MySQLAccount struct {
	Username    string
	Comment     string
	RequireSSL  bool
	Hosts       []string
	ResourceURL string
}

func (a *MySQLAccount) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := decodeFields(data, &fields); err != nil {
		return err
	}

	a.Username, _ = coerceString(fields["username"])
	if a.Username == "" {
		a.Username, _ = coerceString(fields["identificator"])
	}
	a.Comment, _ = coerceString(fields["comment"])
	a.ResourceURL, _ = coerceString(fields["resource_url"])
	a.RequireSSL, _ = coerceBool(fields["require_ssl"])
	a.Hosts, _ = coerceStringSlice(fields["hosts"])
	return nil
}

// MySQLAccountInput is an account as submitted.
//
// RequireSSL is a plain bool rather than a pointer on purpose: these PUTs
// replace the account, so omitting the field would silently clear it.
type MySQLAccountInput struct {
	Username   string   `json:"username,omitempty"`
	Comment    string   `json:"comment,omitempty"`
	Password   string   `json:"password,omitempty"`
	RequireSSL bool     `json:"require_ssl"`
	Hosts      []string `json:"hosts,omitempty"`
}

// MySQLPermission is one account's access to one database. Its identity is the
// pair, which is why the API has no POST for it: a grant is a fact about two
// things that already exist, not an object with a birthday.
type MySQLPermission struct {
	Username    string
	Database    string
	Permissions []string
	ResourceURL string
}

func (p *MySQLPermission) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := decodeFields(data, &fields); err != nil {
		return err
	}

	p.Username, _ = coerceString(fields["username"])
	p.Database, _ = coerceString(fields["database"])
	p.ResourceURL, _ = coerceString(fields["resource_url"])
	p.Permissions, _ = coerceStringSlice(fields["permissions"])
	return nil
}

// MySQLPermissionInput is the only writable part of a grant.
type MySQLPermissionInput struct {
	Permissions []string `json:"permissions"`
}

func mysqlCollectionPath(service string) string {
	return vserverPath(service) + "/database/mysql"
}

func mysqlItemPath(service, name string) string {
	return mysqlCollectionPath(service) + "/" + url.PathEscape(name)
}

func mysqlAccountCollectionPath(service string) string {
	return mysqlCollectionPath(service) + "/account"
}

func mysqlAccountItemPath(service, username string) string {
	return mysqlAccountCollectionPath(service) + "/" + url.PathEscape(username)
}

func mysqlPermissionCollectionPath(service, username string) string {
	return mysqlAccountItemPath(service, username) + "/permission"
}

func mysqlPermissionItemPath(service, username, database string) string {
	return mysqlPermissionCollectionPath(service, username) + "/" + url.PathEscape(database)
}

// ListMySQLDatabases returns a service's databases.
func (c *Client) ListMySQLDatabases(ctx context.Context, service string) ([]MySQLDatabase, error) {
	raws, err := c.listPaged(ctx, mysqlCollectionPath(service))
	if err != nil {
		return nil, err
	}
	return decodeList(raws, func(d *MySQLDatabase) string { return strings.ToLower(d.Name) })
}

// GetMySQLDatabase reads one database from the service's cached listing.
func (c *Client) GetMySQLDatabase(ctx context.Context, service, name string) (*MySQLDatabase, error) {
	databases, err := c.ListMySQLDatabases(ctx, service)
	if err != nil {
		return nil, err
	}
	for i := range databases {
		if strings.EqualFold(databases[i].Name, name) {
			return &databases[i], nil
		}
	}
	return nil, fmt.Errorf("zone.eu: database %q on service %q: %w", name, service, ErrNotFound)
}

// CreateMySQLDatabase creates a database.
func (c *Client) CreateMySQLDatabase(ctx context.Context, service string, input MySQLDatabaseInput) (*MySQLDatabase, error) {
	defer c.invalidateList(mysqlCollectionPath(service))

	var created MySQLDatabase
	if err := c.doOne(ctx, http.MethodPost, mysqlCollectionPath(service), input, &created); err != nil {
		return nil, err
	}
	if created.Name == "" {
		created.Name = input.Name
	}
	return &created, nil
}

// DeleteMySQLDatabase drops a database and everything in it.
func (c *Client) DeleteMySQLDatabase(ctx context.Context, service, name string) error {
	defer c.invalidateList(mysqlCollectionPath(service))

	if _, err := c.do(ctx, http.MethodDelete, mysqlItemPath(service, name), nil); err != nil {
		if IsNotFound(err) {
			return nil
		}
		return err
	}
	return nil
}

// ListMySQLAccounts returns a service's database users.
func (c *Client) ListMySQLAccounts(ctx context.Context, service string) ([]MySQLAccount, error) {
	raws, err := c.listPaged(ctx, mysqlAccountCollectionPath(service))
	if err != nil {
		return nil, err
	}
	return decodeList(raws, func(a *MySQLAccount) string { return strings.ToLower(a.Username) })
}

// GetMySQLAccount reads one account from the service's cached listing.
func (c *Client) GetMySQLAccount(ctx context.Context, service, username string) (*MySQLAccount, error) {
	accounts, err := c.ListMySQLAccounts(ctx, service)
	if err != nil {
		return nil, err
	}
	for i := range accounts {
		if strings.EqualFold(accounts[i].Username, username) {
			return &accounts[i], nil
		}
	}
	return nil, fmt.Errorf("zone.eu: database account %q on service %q: %w", username, service, ErrNotFound)
}

// CreateMySQLAccount creates a database user.
func (c *Client) CreateMySQLAccount(ctx context.Context, service string, input MySQLAccountInput) (*MySQLAccount, error) {
	defer c.invalidateList(mysqlAccountCollectionPath(service))

	var created MySQLAccount
	if err := c.doOne(ctx, http.MethodPost, mysqlAccountCollectionPath(service), input, &created); err != nil {
		return nil, err
	}
	if created.Username == "" {
		created.Username = input.Username
	}
	return &created, nil
}

// UpdateMySQLAccount replaces a database user. The username is read-only in
// update mode, so it is not sent.
func (c *Client) UpdateMySQLAccount(ctx context.Context, service, username string, input MySQLAccountInput) (*MySQLAccount, error) {
	defer c.invalidateList(mysqlAccountCollectionPath(service))

	input.Username = ""
	var updated MySQLAccount
	if err := c.doOne(ctx, http.MethodPut, mysqlAccountItemPath(service, username), input, &updated); err != nil {
		return nil, err
	}
	if updated.Username == "" {
		updated.Username = username
	}
	return &updated, nil
}

// DeleteMySQLAccount removes a database user.
func (c *Client) DeleteMySQLAccount(ctx context.Context, service, username string) error {
	defer c.invalidateList(mysqlAccountCollectionPath(service))

	if _, err := c.do(ctx, http.MethodDelete, mysqlAccountItemPath(service, username), nil); err != nil {
		if IsNotFound(err) {
			return nil
		}
		return err
	}
	return nil
}

// ListMySQLPermissions returns one account's grants.
func (c *Client) ListMySQLPermissions(ctx context.Context, service, username string) ([]MySQLPermission, error) {
	raws, err := c.list(ctx, mysqlPermissionCollectionPath(service, username))
	if err != nil {
		return nil, err
	}
	return decodeList(raws, func(p *MySQLPermission) string { return strings.ToLower(p.Database) })
}

// GetMySQLPermission reads one grant from the account's cached grant listing.
func (c *Client) GetMySQLPermission(ctx context.Context, service, username, database string) (*MySQLPermission, error) {
	permissions, err := c.ListMySQLPermissions(ctx, service, username)
	if err != nil {
		return nil, err
	}
	for i := range permissions {
		if strings.EqualFold(permissions[i].Database, database) {
			return &permissions[i], nil
		}
	}
	return nil, fmt.Errorf(
		"zone.eu: grant for %q on database %q of service %q: %w", username, database, service, ErrNotFound,
	)
}

// SetMySQLPermission writes one grant. The endpoint has no POST: creating and
// updating a grant are the same PUT.
func (c *Client) SetMySQLPermission(
	ctx context.Context, service, username, database string, input MySQLPermissionInput,
) (*MySQLPermission, error) {
	defer c.invalidateList(mysqlPermissionCollectionPath(service, username))

	var stored MySQLPermission
	err := c.doOne(ctx, http.MethodPut, mysqlPermissionItemPath(service, username, database), input, &stored)
	if err != nil {
		return nil, err
	}
	if stored.Username == "" {
		stored.Username = username
	}
	if stored.Database == "" {
		stored.Database = database
	}
	return &stored, nil
}

// DeleteMySQLPermission revokes a grant, leaving the account and the database
// in place.
func (c *Client) DeleteMySQLPermission(ctx context.Context, service, username, database string) error {
	defer c.invalidateList(mysqlPermissionCollectionPath(service, username))

	_, err := c.do(ctx, http.MethodDelete, mysqlPermissionItemPath(service, username, database), nil)
	if err != nil {
		if IsNotFound(err) {
			return nil
		}
		return err
	}
	return nil
}
