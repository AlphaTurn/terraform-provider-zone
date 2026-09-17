package zoneapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// MailAccount is a mailbox on a webhosting service.
//
// zonecloud and premium are deliberately not modelled. Both describe separate
// products, both are null on some accounts and an object on others, and neither
// has a consumer here; premium also carries prices, which puts it in the same
// category as the ordering endpoints.
type MailAccount struct {
	Address          string
	Comment          string
	Spamlevel        string
	TwoFactorAuth    bool
	Autoreply        bool
	ForwardAddresses []string
	DisabledFeatures []string
	DiskSize         int64
	DiskUsage        int64
	DiskUsageHuman   string
	ResourceURL      string

	// DeletedAt is set on an archived mailbox. An archived account still
	// appears in the listing, so this is how a tombstone is told from a live
	// mailbox.
	DeletedAt *string
}

func (a *MailAccount) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := decodeFields(data, &fields); err != nil {
		return err
	}

	for target, field := range map[*string]string{
		&a.Address:        "address",
		&a.Comment:        "comment",
		&a.Spamlevel:      "spamlevel",
		&a.DiskUsageHuman: "disk_usage_human",
		&a.ResourceURL:    "resource_url",
	} {
		*target, _ = coerceString(fields[field])
	}

	a.TwoFactorAuth, _ = coerceBool(fields["two_factor_auth"])
	a.Autoreply, _ = coerceBool(fields["autoreply"])
	a.DiskSize, _ = coerceInt64(fields["disk_size"])
	a.DiskUsage, _ = coerceInt64(fields["disk_usage"])
	a.ForwardAddresses, _ = coerceStringSlice(fields["fwd_addresses"])
	a.DisabledFeatures, _ = coerceStringSlice(fields["disabled_features"])
	a.DeletedAt = coerceOptionalString(fields["deleted_at"])
	return nil
}

// Archived reports whether this is a tombstone rather than a live mailbox.
func (a *MailAccount) Archived() bool { return a.DeletedAt != nil }

// MailAccountInput is a mailbox as submitted.
type MailAccountInput struct {
	Address          string   `json:"address,omitempty"`
	Password         string   `json:"password,omitempty"`
	Comment          string   `json:"comment,omitempty"`
	Spamlevel        string   `json:"spamlevel,omitempty"`
	ForwardAddresses []string `json:"fwd_addresses,omitempty"`
}

// MailForwarder is an address that forwards rather than storing mail.
type MailForwarder struct {
	Address          string
	Comment          string
	Autoreply        bool
	ForwardAddresses []string
	DisabledFeatures []string
	MailToHTTPURL    string
	ResourceURL      string
}

func (f *MailForwarder) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := decodeFields(data, &fields); err != nil {
		return err
	}

	for target, field := range map[*string]string{
		&f.Address:       "address",
		&f.Comment:       "comment",
		&f.MailToHTTPURL: "mail_to_http_url",
		&f.ResourceURL:   "resource_url",
	} {
		*target, _ = coerceString(fields[field])
	}

	f.Autoreply, _ = coerceBool(fields["autoreply"])
	f.ForwardAddresses, _ = coerceStringSlice(fields["fwd_addresses"])
	f.DisabledFeatures, _ = coerceStringSlice(fields["disabled_features"])
	return nil
}

// MailForwarderInput is a forwarder as submitted.
type MailForwarderInput struct {
	Address          string   `json:"address,omitempty"`
	Comment          string   `json:"comment,omitempty"`
	ForwardAddresses []string `json:"fwd_addresses"`
}

// Autoreply is a vacation message. The same object hangs off both mail accounts
// and forwarders, from separate endpoints with identical shapes.
type Autoreply struct {
	Enabled     bool
	FromName    string
	Subject     string
	Body        string
	DateStart   string
	DateEnd     string
	ResourceURL string
}

func (a *Autoreply) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := decodeFields(data, &fields); err != nil {
		return err
	}

	for target, field := range map[*string]string{
		&a.FromName:    "fromname",
		&a.Subject:     "subject",
		&a.Body:        "body",
		&a.DateStart:   "datestart",
		&a.DateEnd:     "dateend",
		&a.ResourceURL: "resource_url",
	} {
		*target, _ = coerceString(fields[field])
	}
	a.Enabled, _ = coerceBool(fields["is_enabled"])
	return nil
}

// AutoreplyInput is an autoreply as submitted.
type AutoreplyInput struct {
	Enabled   bool   `json:"is_enabled"`
	FromName  string `json:"fromname,omitempty"`
	Subject   string `json:"subject,omitempty"`
	Body      string `json:"body,omitempty"`
	DateStart string `json:"datestart,omitempty"`
	DateEnd   string `json:"dateend,omitempty"`
}

// AutoreplyTarget is which kind of address an autoreply belongs to. The two
// endpoints are separate and their objects are identical, so the parent has to
// be named rather than discovered.
type AutoreplyTarget string

const (
	// AutoreplyOnAccount is an autoreply on a mailbox.
	AutoreplyOnAccount AutoreplyTarget = "account"
	// AutoreplyOnForwarder is an autoreply on a forwarder.
	AutoreplyOnForwarder AutoreplyTarget = "forwarder"
)

// DKIM is the public half of a service's DKIM signing key. The endpoint has no
// schema in the published description at all; this is what it returns.
type DKIM struct {
	PublicKey string
}

func (d *DKIM) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := decodeFields(data, &fields); err != nil {
		return err
	}
	d.PublicKey, _ = coerceString(fields["publickey"])
	return nil
}

func mailAccountCollectionPath(service string) string {
	return vserverPath(service) + "/mail/account"
}

func mailAccountItemPath(service, address string) string {
	return mailAccountCollectionPath(service) + "/" + url.PathEscape(address)
}

func mailForwarderCollectionPath(service string) string {
	return vserverPath(service) + "/mail/forwarder"
}

func mailForwarderItemPath(service, address string) string {
	return mailForwarderCollectionPath(service) + "/" + url.PathEscape(address)
}

func autoreplyPath(service string, target AutoreplyTarget, address string) string {
	if target == AutoreplyOnForwarder {
		return mailForwarderItemPath(service, address) + "/autoreply"
	}
	return mailAccountItemPath(service, address) + "/autoreply"
}

func dkimPath(service string) string { return vserverPath(service) + "/mail/dkim" }

// ListMailAccounts returns a service's mailboxes, archived ones included.
func (c *Client) ListMailAccounts(ctx context.Context, service string) ([]MailAccount, error) {
	raws, err := c.listPaged(ctx, mailAccountCollectionPath(service))
	if err != nil {
		return nil, err
	}
	return decodeList(raws, func(a *MailAccount) string { return strings.ToLower(a.Address) })
}

// GetMailAccount reads one mailbox from the service's cached listing.
//
// An archived mailbox reports as missing. It is still in the listing, but
// adopting a tombstone would give Terraform a resource it can never reconcile,
// and the address is free to be created again.
func (c *Client) GetMailAccount(ctx context.Context, service, address string) (*MailAccount, error) {
	accounts, err := c.ListMailAccounts(ctx, service)
	if err != nil {
		return nil, err
	}
	for i := range accounts {
		// The API does not promise to echo the case it was given.
		if !strings.EqualFold(accounts[i].Address, address) {
			continue
		}
		if accounts[i].Archived() {
			break
		}
		return &accounts[i], nil
	}
	return nil, fmt.Errorf("zone.eu: mail account %q on service %q: %w", address, service, ErrNotFound)
}

// CreateMailAccount creates a mailbox.
func (c *Client) CreateMailAccount(ctx context.Context, service string, input MailAccountInput) (*MailAccount, error) {
	defer c.invalidateList(mailAccountCollectionPath(service))

	var created MailAccount
	if err := c.doOne(ctx, http.MethodPost, mailAccountCollectionPath(service), input, &created); err != nil {
		return nil, err
	}
	if created.Address == "" {
		created.Address = input.Address
	}
	return &created, nil
}

// UpdateMailAccount replaces a mailbox's settings.
func (c *Client) UpdateMailAccount(ctx context.Context, service, address string, input MailAccountInput) (*MailAccount, error) {
	defer c.invalidateList(mailAccountCollectionPath(service))

	var updated MailAccount
	if err := c.doOne(ctx, http.MethodPut, mailAccountItemPath(service, address), input, &updated); err != nil {
		return nil, err
	}
	if updated.Address == "" {
		updated.Address = address
	}
	return &updated, nil
}

// DeleteMailAccount removes a mailbox and the mail in it.
func (c *Client) DeleteMailAccount(ctx context.Context, service, address string) error {
	defer c.invalidateList(mailAccountCollectionPath(service))

	if _, err := c.do(ctx, http.MethodDelete, mailAccountItemPath(service, address), nil); err != nil {
		if IsNotFound(err) {
			return nil
		}
		return err
	}
	return nil
}

// ListMailForwarders returns a service's forwarders.
func (c *Client) ListMailForwarders(ctx context.Context, service string) ([]MailForwarder, error) {
	raws, err := c.listPaged(ctx, mailForwarderCollectionPath(service))
	if err != nil {
		return nil, err
	}
	return decodeList(raws, func(f *MailForwarder) string { return strings.ToLower(f.Address) })
}

// GetMailForwarder reads one forwarder from the service's cached listing.
func (c *Client) GetMailForwarder(ctx context.Context, service, address string) (*MailForwarder, error) {
	forwarders, err := c.ListMailForwarders(ctx, service)
	if err != nil {
		return nil, err
	}
	for i := range forwarders {
		if strings.EqualFold(forwarders[i].Address, address) {
			return &forwarders[i], nil
		}
	}
	return nil, fmt.Errorf("zone.eu: mail forwarder %q on service %q: %w", address, service, ErrNotFound)
}

// CreateMailForwarder creates a forwarder.
func (c *Client) CreateMailForwarder(ctx context.Context, service string, input MailForwarderInput) (*MailForwarder, error) {
	defer c.invalidateList(mailForwarderCollectionPath(service))

	var created MailForwarder
	if err := c.doOne(ctx, http.MethodPost, mailForwarderCollectionPath(service), input, &created); err != nil {
		return nil, err
	}
	if created.Address == "" {
		created.Address = input.Address
	}
	return &created, nil
}

// UpdateMailForwarder replaces a forwarder.
func (c *Client) UpdateMailForwarder(ctx context.Context, service, address string, input MailForwarderInput) (*MailForwarder, error) {
	defer c.invalidateList(mailForwarderCollectionPath(service))

	var updated MailForwarder
	if err := c.doOne(ctx, http.MethodPut, mailForwarderItemPath(service, address), input, &updated); err != nil {
		return nil, err
	}
	if updated.Address == "" {
		updated.Address = address
	}
	return &updated, nil
}

// DeleteMailForwarder removes a forwarder.
func (c *Client) DeleteMailForwarder(ctx context.Context, service, address string) error {
	defer c.invalidateList(mailForwarderCollectionPath(service))

	if _, err := c.do(ctx, http.MethodDelete, mailForwarderItemPath(service, address), nil); err != nil {
		if IsNotFound(err) {
			return nil
		}
		return err
	}
	return nil
}

// GetAutoreply reads an address's autoreply. There is no collection to cache
// here: the endpoint serves exactly one object per address.
func (c *Client) GetAutoreply(ctx context.Context, service string, target AutoreplyTarget, address string) (*Autoreply, error) {
	var autoreply Autoreply
	err := c.doOne(ctx, http.MethodGet, autoreplyPath(service, target, address), nil, &autoreply)
	if err != nil {
		return nil, err
	}
	return &autoreply, nil
}

// SetAutoreply writes an address's autoreply. The endpoint is PUT-only, so
// this both creates and updates, and disabling is how one is removed.
func (c *Client) SetAutoreply(
	ctx context.Context, service string, target AutoreplyTarget, address string, input AutoreplyInput,
) (*Autoreply, error) {
	// The parent listing reports whether an address has an autoreply at all,
	// so it has to be dropped too.
	if target == AutoreplyOnForwarder {
		defer c.invalidateList(mailForwarderCollectionPath(service))
	} else {
		defer c.invalidateList(mailAccountCollectionPath(service))
	}

	var stored Autoreply
	err := c.doOne(ctx, http.MethodPut, autoreplyPath(service, target, address), input, &stored)
	if err != nil {
		return nil, err
	}
	return &stored, nil
}

// GetDKIM reads a service's DKIM public key.
func (c *Client) GetDKIM(ctx context.Context, service string) (*DKIM, error) {
	raws, err := c.list(ctx, dkimPath(service))
	if err != nil {
		return nil, err
	}
	if len(raws) == 0 {
		return nil, fmt.Errorf("zone.eu: DKIM on service %q: %w", service, ErrNotFound)
	}

	var dkim DKIM
	if err := decode(raws[0], &dkim); err != nil {
		return nil, err
	}
	if dkim.PublicKey == "" {
		// The endpoint answers with an empty object rather than a 404 when
		// signing is off.
		return nil, fmt.Errorf("zone.eu: DKIM on service %q: %w", service, ErrNotFound)
	}
	return &dkim, nil
}

// EnableDKIM turns on DKIM signing. The endpoint takes no body: there is
// nothing to configure and no way to rotate a key except off and on again.
func (c *Client) EnableDKIM(ctx context.Context, service string) (*DKIM, error) {
	defer c.invalidateList(dkimPath(service))

	var created DKIM
	if err := c.doOne(ctx, http.MethodPost, dkimPath(service), nil, &created); err != nil {
		return nil, err
	}
	return &created, nil
}

// DisableDKIM turns off DKIM signing.
func (c *Client) DisableDKIM(ctx context.Context, service string) error {
	defer c.invalidateList(dkimPath(service))

	if _, err := c.do(ctx, http.MethodDelete, dkimPath(service), nil); err != nil {
		if IsNotFound(err) {
			return nil
		}
		return err
	}
	return nil
}
