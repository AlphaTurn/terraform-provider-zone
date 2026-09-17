package zoneapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
)

// SSHSettings is a service's SSH configuration. Only access is writable.
type SSHSettings struct {
	Username string
	Access   string
	IPv4     string
	IPv6     *string
	Webhosts []string

	// Fingerprints is keyed by algorithm. The published schema calls this an
	// array of strings; the live API returns an object with DSA, ECDSA,
	// ED25519 and RSA keys, so it is modelled as a map. A fixed set of
	// attributes would break the day zone.eu drops DSA or adds an algorithm.
	Fingerprints map[string]string

	ResourceURL string
}

func (s *SSHSettings) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := decodeFields(data, &fields); err != nil {
		return err
	}

	s.Username, _ = coerceString(fields["username"])
	s.Access, _ = coerceString(fields["access"])
	s.IPv4, _ = coerceString(fields["ipv4"])
	s.ResourceURL, _ = coerceString(fields["resource_url"])
	s.IPv6 = coerceOptionalString(fields["ipv6"])
	s.Webhosts, _ = coerceStringSlice(fields["webhosts"])
	s.Fingerprints, _ = coerceStringMap(fields["server_fingerprints"])
	return nil
}

// SSHSettingsInput is the writable part of a service's SSH configuration.
type SSHSettingsInput struct {
	Access string `json:"access"`
}

// SSHPublicKey is an authorised key. There is no PUT, so nothing about one can
// be changed in place.
type SSHPublicKey struct {
	ID          int64
	PublicKey   string
	Comment     string
	Fingerprint string
	Type        string
	Created     string
	LastUsed    *string

	// Size is derived from the key by zone.eu. It arrives as a quoted number
	// on RSA keys and as null on Ed25519, where the schema says integer.
	Size *int64

	ResourceURL string
}

func (k *SSHPublicKey) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := decodeFields(data, &fields); err != nil {
		return err
	}

	k.ID, _ = coerceInt64(fields["identificator"])
	for target, field := range map[*string]string{
		&k.PublicKey:   "public_key",
		&k.Comment:     "comment",
		&k.Fingerprint: "fingerprint",
		&k.Type:        "type",
		&k.Created:     "created",
		&k.ResourceURL: "resource_url",
	} {
		*target, _ = coerceString(fields[field])
	}
	k.LastUsed = coerceOptionalString(fields["last_used"])
	k.Size = coerceOptionalInt64(fields["size"])
	return nil
}

// SSHPublicKeyInput is a key as submitted.
type SSHPublicKeyInput struct {
	PublicKey string `json:"public_key"`
	Comment   string `json:"comment,omitempty"`
}

// SSHWhitelistIP is an address allowed to reach SSH. There is no PUT here
// either.
type SSHWhitelistIP struct {
	ID          int64
	IP          string
	Comment     string
	ResourceURL string
}

func (w *SSHWhitelistIP) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := decodeFields(data, &fields); err != nil {
		return err
	}

	w.ID, _ = coerceInt64(fields["identificator"])
	w.IP, _ = coerceString(fields["ip"])
	w.Comment, _ = coerceString(fields["comment"])
	w.ResourceURL, _ = coerceString(fields["resource_url"])
	return nil
}

// SSHWhitelistIPInput is a whitelist entry as submitted.
type SSHWhitelistIPInput struct {
	IP      string `json:"ip"`
	Comment string `json:"comment,omitempty"`
}

func sshPath(service string) string { return vserverPath(service) + "/ssh" }

func sshPublicKeyCollectionPath(service string) string { return sshPath(service) + "/publickey" }

func sshPublicKeyItemPath(service string, id int64) string {
	return sshPublicKeyCollectionPath(service) + "/" + strconv.FormatInt(id, 10)
}

func sshWhitelistCollectionPath(service string) string { return sshPath(service) + "/whitelist" }

func sshWhitelistItemPath(service string, id int64) string {
	return sshWhitelistCollectionPath(service) + "/" + strconv.FormatInt(id, 10)
}

// GetSSHSettings reads a service's SSH configuration.
func (c *Client) GetSSHSettings(ctx context.Context, service string) (*SSHSettings, error) {
	var settings SSHSettings
	if err := c.doOne(ctx, http.MethodGet, sshPath(service), nil, &settings); err != nil {
		return nil, err
	}
	return &settings, nil
}

// UpdateSSHSettings writes a service's SSH configuration.
func (c *Client) UpdateSSHSettings(ctx context.Context, service string, input SSHSettingsInput) (*SSHSettings, error) {
	var updated SSHSettings
	if err := c.doOne(ctx, http.MethodPut, sshPath(service), input, &updated); err != nil {
		return nil, err
	}
	return &updated, nil
}

// ListSSHPublicKeys returns a service's authorised keys.
func (c *Client) ListSSHPublicKeys(ctx context.Context, service string) ([]SSHPublicKey, error) {
	raws, err := c.list(ctx, sshPublicKeyCollectionPath(service))
	if err != nil {
		return nil, err
	}
	return decodeList(raws, func(k *SSHPublicKey) string { return strconv.FormatInt(k.ID, 10) })
}

// GetSSHPublicKey reads one key from the service's cached listing.
func (c *Client) GetSSHPublicKey(ctx context.Context, service string, id int64) (*SSHPublicKey, error) {
	keys, err := c.ListSSHPublicKeys(ctx, service)
	if err != nil {
		return nil, err
	}
	for i := range keys {
		if keys[i].ID == id {
			return &keys[i], nil
		}
	}
	return nil, fmt.Errorf("zone.eu: SSH key %d on service %q: %w", id, service, ErrNotFound)
}

// FindSSHPublicKey looks a key up by fingerprint, so an import can name
// something a human can read off `ssh-keygen -lf`.
func (c *Client) FindSSHPublicKey(ctx context.Context, service, fingerprint string) (*SSHPublicKey, error) {
	keys, err := c.ListSSHPublicKeys(ctx, service)
	if err != nil {
		return nil, err
	}
	for i := range keys {
		if keys[i].Fingerprint == fingerprint {
			return &keys[i], nil
		}
	}
	return nil, fmt.Errorf("zone.eu: SSH key %q on service %q: %w", fingerprint, service, ErrNotFound)
}

// CreateSSHPublicKey authorises a key.
func (c *Client) CreateSSHPublicKey(ctx context.Context, service string, input SSHPublicKeyInput) (*SSHPublicKey, error) {
	defer c.invalidateList(sshPublicKeyCollectionPath(service))

	var created SSHPublicKey
	err := c.doOne(ctx, http.MethodPost, sshPublicKeyCollectionPath(service), input, &created)
	if err != nil {
		return nil, err
	}
	return &created, nil
}

// DeleteSSHPublicKey removes a key.
func (c *Client) DeleteSSHPublicKey(ctx context.Context, service string, id int64) error {
	defer c.invalidateList(sshPublicKeyCollectionPath(service))

	if _, err := c.do(ctx, http.MethodDelete, sshPublicKeyItemPath(service, id), nil); err != nil {
		if IsNotFound(err) {
			return nil
		}
		return err
	}
	return nil
}

// ListSSHWhitelist returns the addresses allowed to reach SSH.
func (c *Client) ListSSHWhitelist(ctx context.Context, service string) ([]SSHWhitelistIP, error) {
	raws, err := c.list(ctx, sshWhitelistCollectionPath(service))
	if err != nil {
		return nil, err
	}
	return decodeList(raws, func(w *SSHWhitelistIP) string { return strconv.FormatInt(w.ID, 10) })
}

// GetSSHWhitelistIP reads one whitelist entry from the cached listing.
func (c *Client) GetSSHWhitelistIP(ctx context.Context, service string, id int64) (*SSHWhitelistIP, error) {
	entries, err := c.ListSSHWhitelist(ctx, service)
	if err != nil {
		return nil, err
	}
	for i := range entries {
		if entries[i].ID == id {
			return &entries[i], nil
		}
	}
	return nil, fmt.Errorf("zone.eu: SSH whitelist entry %d on service %q: %w", id, service, ErrNotFound)
}

// FindSSHWhitelistIP looks an entry up by address, which is how a person knows
// it rather than by its numeric id.
func (c *Client) FindSSHWhitelistIP(ctx context.Context, service, ip string) (*SSHWhitelistIP, error) {
	entries, err := c.ListSSHWhitelist(ctx, service)
	if err != nil {
		return nil, err
	}
	for i := range entries {
		if entries[i].IP == ip {
			return &entries[i], nil
		}
	}
	return nil, fmt.Errorf("zone.eu: SSH whitelist entry %q on service %q: %w", ip, service, ErrNotFound)
}

// CreateSSHWhitelistIP allows an address.
func (c *Client) CreateSSHWhitelistIP(ctx context.Context, service string, input SSHWhitelistIPInput) (*SSHWhitelistIP, error) {
	defer c.invalidateList(sshWhitelistCollectionPath(service))

	var created SSHWhitelistIP
	err := c.doOne(ctx, http.MethodPost, sshWhitelistCollectionPath(service), input, &created)
	if err != nil {
		return nil, err
	}
	return &created, nil
}

// DeleteSSHWhitelistIP disallows an address.
func (c *Client) DeleteSSHWhitelistIP(ctx context.Context, service string, id int64) error {
	defer c.invalidateList(sshWhitelistCollectionPath(service))

	if _, err := c.do(ctx, http.MethodDelete, sshWhitelistItemPath(service, id), nil); err != nil {
		if IsNotFound(err) {
			return nil
		}
		return err
	}
	return nil
}
