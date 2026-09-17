package zoneapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
)

// FTPUser is an FTP account on a webhosting service.
type FTPUser struct {
	ID                int64
	Username          string
	UsernameSystem    string
	Directory         string
	RequireTLS        bool
	AccessProfile     string
	AllowedOperations []string
	AccessCountries   []string
	ResourceURL       string
}

func (u *FTPUser) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := decodeFields(data, &fields); err != nil {
		return err
	}

	u.ID, _ = coerceInt64(fields["identificator"])
	for target, field := range map[*string]string{
		&u.Username:       "username",
		&u.UsernameSystem: "username_system",
		&u.Directory:      "directory",
		&u.AccessProfile:  "access_profile",
		&u.ResourceURL:    "resource_url",
	} {
		*target, _ = coerceString(fields[field])
	}
	u.RequireTLS, _ = coerceBool(fields["require_tls"])
	u.AllowedOperations, _ = coerceStringSlice(fields["allowed_operations"])
	u.AccessCountries, _ = coerceStringSlice(fields["access_countries"])
	return nil
}

// FTPUserInput is an FTP account as submitted.
type FTPUserInput struct {
	Username          string   `json:"username,omitempty"`
	Password          string   `json:"password,omitempty"`
	Directory         string   `json:"directory,omitempty"`
	RequireTLS        bool     `json:"require_tls"`
	AccessProfile     string   `json:"access_profile,omitempty"`
	AllowedOperations []string `json:"allowed_operations,omitempty"`
	AccessCountries   []string `json:"access_countries,omitempty"`
}

// FTPIPWhitelist is an address allowed to reach FTP.
//
// The published schema marks every field of this object read-only, including
// ip, while still requiring a body on create. That cannot both be true, so the
// create payload is inferred to be the address — the same shape the SSH
// whitelist endpoint documents properly. It has not been proven on the wire,
// because doing so would mean writing to a production service.
type FTPIPWhitelist struct {
	ID          int64
	IP          string
	Country     string
	Created     string
	ResourceURL string
}

func (w *FTPIPWhitelist) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := decodeFields(data, &fields); err != nil {
		return err
	}

	w.ID, _ = coerceInt64(fields["identificator"])
	w.IP, _ = coerceString(fields["ip"])
	w.Country, _ = coerceString(fields["country"])
	w.Created, _ = coerceString(fields["created"])
	w.ResourceURL, _ = coerceString(fields["resource_url"])
	return nil
}

// FTPIPWhitelistInput is a whitelist entry as submitted.
type FTPIPWhitelistInput struct {
	IP string `json:"ip"`
}

func ftpUserCollectionPath(service string) string { return vserverPath(service) + "/ftp/user" }

func ftpUserItemPath(service string, id int64) string {
	return ftpUserCollectionPath(service) + "/" + strconv.FormatInt(id, 10)
}

func ftpWhitelistCollectionPath(service string) string {
	return vserverPath(service) + "/ftp/ipwhitelist"
}

func ftpWhitelistItemPath(service string, id int64) string {
	return ftpWhitelistCollectionPath(service) + "/" + strconv.FormatInt(id, 10)
}

// ListFTPUsers returns a service's FTP accounts.
func (c *Client) ListFTPUsers(ctx context.Context, service string) ([]FTPUser, error) {
	raws, err := c.listPaged(ctx, ftpUserCollectionPath(service))
	if err != nil {
		return nil, err
	}
	return decodeList(raws, func(u *FTPUser) string { return strconv.FormatInt(u.ID, 10) })
}

// GetFTPUser reads one account from the service's cached listing.
func (c *Client) GetFTPUser(ctx context.Context, service string, id int64) (*FTPUser, error) {
	users, err := c.ListFTPUsers(ctx, service)
	if err != nil {
		return nil, err
	}
	for i := range users {
		if users[i].ID == id {
			return &users[i], nil
		}
	}
	return nil, fmt.Errorf("zone.eu: FTP user %d on service %q: %w", id, service, ErrNotFound)
}

// FindFTPUser looks an account up by its alias username, which is how a person
// knows it rather than by its numeric id.
func (c *Client) FindFTPUser(ctx context.Context, service, username string) (*FTPUser, error) {
	users, err := c.ListFTPUsers(ctx, service)
	if err != nil {
		return nil, err
	}
	for i := range users {
		if users[i].Username == username || users[i].UsernameSystem == username {
			return &users[i], nil
		}
	}
	return nil, fmt.Errorf("zone.eu: FTP user %q on service %q: %w", username, service, ErrNotFound)
}

// CreateFTPUser creates an FTP account.
func (c *Client) CreateFTPUser(ctx context.Context, service string, input FTPUserInput) (*FTPUser, error) {
	defer c.invalidateList(ftpUserCollectionPath(service))

	var created FTPUser
	if err := c.doOne(ctx, http.MethodPost, ftpUserCollectionPath(service), input, &created); err != nil {
		return nil, err
	}
	return &created, nil
}

// UpdateFTPUser replaces an FTP account's settings.
func (c *Client) UpdateFTPUser(ctx context.Context, service string, id int64, input FTPUserInput) (*FTPUser, error) {
	defer c.invalidateList(ftpUserCollectionPath(service))

	var updated FTPUser
	if err := c.doOne(ctx, http.MethodPut, ftpUserItemPath(service, id), input, &updated); err != nil {
		return nil, err
	}
	if updated.ID == 0 {
		updated.ID = id
	}
	return &updated, nil
}

// DeleteFTPUser removes an FTP account.
func (c *Client) DeleteFTPUser(ctx context.Context, service string, id int64) error {
	defer c.invalidateList(ftpUserCollectionPath(service))

	if _, err := c.do(ctx, http.MethodDelete, ftpUserItemPath(service, id), nil); err != nil {
		if IsNotFound(err) {
			return nil
		}
		return err
	}
	return nil
}

// ListFTPWhitelist returns the addresses allowed to reach FTP.
func (c *Client) ListFTPWhitelist(ctx context.Context, service string) ([]FTPIPWhitelist, error) {
	raws, err := c.listPaged(ctx, ftpWhitelistCollectionPath(service))
	if err != nil {
		return nil, err
	}
	return decodeList(raws, func(w *FTPIPWhitelist) string { return strconv.FormatInt(w.ID, 10) })
}

// GetFTPWhitelistIP reads one entry from the cached listing.
func (c *Client) GetFTPWhitelistIP(ctx context.Context, service string, id int64) (*FTPIPWhitelist, error) {
	entries, err := c.ListFTPWhitelist(ctx, service)
	if err != nil {
		return nil, err
	}
	for i := range entries {
		if entries[i].ID == id {
			return &entries[i], nil
		}
	}
	return nil, fmt.Errorf("zone.eu: FTP whitelist entry %d on service %q: %w", id, service, ErrNotFound)
}

// FindFTPWhitelistIP looks an entry up by address.
func (c *Client) FindFTPWhitelistIP(ctx context.Context, service, ip string) (*FTPIPWhitelist, error) {
	entries, err := c.ListFTPWhitelist(ctx, service)
	if err != nil {
		return nil, err
	}
	for i := range entries {
		if entries[i].IP == ip {
			return &entries[i], nil
		}
	}
	return nil, fmt.Errorf("zone.eu: FTP whitelist entry %q on service %q: %w", ip, service, ErrNotFound)
}

// CreateFTPWhitelistIP allows an address.
func (c *Client) CreateFTPWhitelistIP(ctx context.Context, service string, input FTPIPWhitelistInput) (*FTPIPWhitelist, error) {
	defer c.invalidateList(ftpWhitelistCollectionPath(service))

	var created FTPIPWhitelist
	err := c.doOne(ctx, http.MethodPost, ftpWhitelistCollectionPath(service), input, &created)
	if err != nil {
		return nil, err
	}
	if created.IP == "" {
		created.IP = input.IP
	}
	return &created, nil
}

// DeleteFTPWhitelistIP disallows an address.
func (c *Client) DeleteFTPWhitelistIP(ctx context.Context, service string, id int64) error {
	defer c.invalidateList(ftpWhitelistCollectionPath(service))

	if _, err := c.do(ctx, http.MethodDelete, ftpWhitelistItemPath(service, id), nil); err != nil {
		if IsNotFound(err) {
			return nil
		}
		return err
	}
	return nil
}
