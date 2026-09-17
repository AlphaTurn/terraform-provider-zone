package zoneapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// Domain is a registered domain as the API reports it.
//
// Almost every field is read-only: a domain's lifecycle belongs to the registry
// and to zone.eu's ordering system, not to this API. Only renewal_notifications,
// signing_required and nameservers_custom can be written.
type Domain struct {
	Identificator        string
	Name                 string
	Expires              string
	Expired              bool
	DNSSEC               bool
	DNSSECSupported      bool
	Autorenew            bool
	RenewalNotifications bool
	NameserversCustom    bool
	Reactivate           bool
	HasPendingDNSSEC     bool
	AuthKeyEnabled       bool
	ResourceURL          string

	// Delegated names the ZoneID user a domain is delegated to, and is null
	// when it is not delegated at all.
	Delegated *string
	// RenewOrder is null when no renewal is pending.
	RenewOrder *string
	// HasPendingTrade is null unless a trade is in progress.
	HasPendingTrade *int64
}

// DomainSettings is the writable part of a domain.
//
// Every field is a pointer so that an unset argument is left out of the request
// entirely: PUT /domain/{name} declares no request body at all in the published
// schema, so whether it merges or replaces is unknown, and sending a field
// nobody asked to change could turn off a setting made in the ZoneID panel.
type DomainSettings struct {
	RenewalNotifications *bool `json:"renewal_notifications,omitempty"`
	SigningRequired      *bool `json:"signing_required,omitempty"`
	NameserversCustom    *bool `json:"nameservers_custom,omitempty"`
}

// DomainFilter narrows a domain listing. These are query parameters, and the
// zero value asks for everything.
type DomainFilter struct {
	// NameContains is a substring match, not an exact one.
	NameContains string
	Renewable    *bool
	Delegated    *bool
	NeedsRenewal *bool
}

func (f DomainFilter) options() []requestOption {
	opts := []requestOption{withQuery("name", f.NameContains)}
	if f.Renewable != nil {
		opts = append(opts, withBoolQuery("renewable", *f.Renewable))
	}
	if f.Delegated != nil {
		opts = append(opts, withBoolQuery("delegated", *f.Delegated))
	}
	if f.NeedsRenewal != nil {
		opts = append(opts, withBoolQuery("needs_renewal", *f.NeedsRenewal))
	}
	return opts
}

func (d *Domain) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := decodeFields(data, &fields); err != nil {
		return err
	}

	d.Identificator, _ = coerceString(fields["identificator"])
	d.Name, _ = coerceString(fields["name"])
	d.Expires, _ = coerceString(fields["expires"])
	d.ResourceURL, _ = coerceString(fields["resource_url"])
	d.Expired, _ = coerceBool(fields["expired"])
	d.DNSSEC, _ = coerceBool(fields["dnssec"])
	d.DNSSECSupported, _ = coerceBool(fields["dnssec_supported"])
	d.Autorenew, _ = coerceBool(fields["autorenew"])
	d.RenewalNotifications, _ = coerceBool(fields["renewal_notifications"])
	d.NameserversCustom, _ = coerceBool(fields["nameservers_custom"])
	d.Reactivate, _ = coerceBool(fields["reactivate"])
	d.HasPendingDNSSEC, _ = coerceBool(fields["has_pending_dnssec"])
	d.AuthKeyEnabled, _ = coerceBool(fields["auth_key_enabled"])
	d.Delegated = coerceOptionalString(fields["delegated"])
	d.RenewOrder = coerceOptionalString(fields["renew_order"])
	d.HasPendingTrade = coerceOptionalInt64(fields["has_pending_trade"])

	// The live API answers /domain/{name} without a resource_url, unlike most
	// objects, so fall back to the path it would have been.
	if d.ResourceURL == "" && d.Name != "" {
		d.ResourceURL = DefaultBaseURL + domainPath(d.Name)
	}
	return nil
}

// Nameserver is one delegation record for a domain.
type Nameserver struct {
	Hostname    string
	IP          []string
	ResourceURL string
}

func (n *Nameserver) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := decodeFields(data, &fields); err != nil {
		return err
	}

	n.Hostname, _ = coerceString(fields["hostname"])
	n.ResourceURL, _ = coerceString(fields["resource_url"])
	n.IP, _ = coerceStringSlice(fields["ip"])
	return nil
}

// NameserverInput is a nameserver as submitted.
type NameserverInput struct {
	Hostname string   `json:"hostname"`
	IP       []string `json:"ip,omitempty"`
}

// Contact is a registry contact attached to a domain.
//
// The ext_ fields are registry extensions whose meaning is per TLD. They are
// not optional decoration for .ee, where a registrant contact needs an identity
// code, which is why they are modelled rather than skipped.
type Contact struct {
	Identificator  string
	Role           string
	Type           string
	Name           string
	FirstName      string
	LastName       string
	Organization   string
	Email          string
	Voice          string
	Fax            string
	Country        string
	State          string
	City           string
	Street         string
	Postalcode     string
	RegistryHandle string
	ResourceURL    string

	ExtLanguage             string
	ExtIdent                string
	ExtIdentType            string
	ExtIdentCC              string
	ExtVATNr                string
	ExtDepartment           string
	ExtPassport             string
	ExtLegalForm            string
	ExtCountryOfCitizenship string
}

func (c *Contact) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := decodeFields(data, &fields); err != nil {
		return err
	}

	// The identificator is documented as an integer and arrives as a quoted
	// string. It is only ever a path segment and an import key, so it is kept
	// as text and neither form has to win.
	c.Identificator, _ = coerceString(fields["identificator"])

	for target, field := range map[*string]string{
		&c.Role:                    "role",
		&c.Type:                    "type",
		&c.Name:                    "name",
		&c.FirstName:               "first_name",
		&c.LastName:                "last_name",
		&c.Organization:            "organization",
		&c.Email:                   "email",
		&c.Voice:                   "voice",
		&c.Fax:                     "fax",
		&c.Country:                 "country",
		&c.State:                   "state",
		&c.City:                    "city",
		&c.Street:                  "street",
		&c.Postalcode:              "postalcode",
		&c.RegistryHandle:          "registry_handle",
		&c.ResourceURL:             "resource_url",
		&c.ExtLanguage:             "ext_language",
		&c.ExtIdent:                "ext_ident",
		&c.ExtIdentType:            "ext_ident_type",
		&c.ExtIdentCC:              "ext_ident_cc",
		&c.ExtVATNr:                "ext_vatnr",
		&c.ExtDepartment:           "ext_department",
		&c.ExtPassport:             "ext_passport",
		&c.ExtLegalForm:            "ext_legal_form",
		&c.ExtCountryOfCitizenship: "ext_country_of_citizenship",
	} {
		*target, _ = coerceString(fields[field])
	}
	return nil
}

// ContactInput is a contact as submitted. Read-only fields are absent rather
// than being dropped by a custom marshaller, and every optional field is
// omitted when empty so a partial contact does not blank what the registry
// already holds.
type ContactInput struct {
	Role         string `json:"role,omitempty"`
	Name         string `json:"name,omitempty"`
	FirstName    string `json:"first_name,omitempty"`
	LastName     string `json:"last_name,omitempty"`
	Organization string `json:"organization,omitempty"`
	Email        string `json:"email,omitempty"`
	Voice        string `json:"voice,omitempty"`
	Fax          string `json:"fax,omitempty"`
	Country      string `json:"country,omitempty"`
	State        string `json:"state,omitempty"`
	City         string `json:"city,omitempty"`
	Street       string `json:"street,omitempty"`
	Postalcode   string `json:"postalcode,omitempty"`

	ExtLanguage             string `json:"ext_language,omitempty"`
	ExtIdent                string `json:"ext_ident,omitempty"`
	ExtIdentType            string `json:"ext_ident_type,omitempty"`
	ExtIdentCC              string `json:"ext_ident_cc,omitempty"`
	ExtVATNr                string `json:"ext_vatnr,omitempty"`
	ExtDepartment           string `json:"ext_department,omitempty"`
	ExtPassport             string `json:"ext_passport,omitempty"`
	ExtLegalForm            string `json:"ext_legal_form,omitempty"`
	ExtCountryOfCitizenship string `json:"ext_country_of_citizenship,omitempty"`
}

func domainPath(name string) string { return "/domain/" + url.PathEscape(name) }

func nameserverCollectionPath(domain string) string { return domainPath(domain) + "/nameserver" }

func nameserverItemPath(domain, hostname string) string {
	return nameserverCollectionPath(domain) + "/" + url.PathEscape(hostname)
}

func contactCollectionPath(domain string) string { return domainPath(domain) + "/contact" }

func contactItemPath(domain, id string) string {
	return contactCollectionPath(domain) + "/" + url.PathEscape(id)
}

// ListDomains returns the account's domains, following the pager.
func (c *Client) ListDomains(ctx context.Context, filter DomainFilter) ([]Domain, error) {
	raws, err := c.listPaged(ctx, "/domain", filter.options()...)
	if err != nil {
		return nil, err
	}
	return decodeList(raws, func(d *Domain) string { return strings.ToLower(d.Name) })
}

// GetDomain reads one domain.
//
// This deliberately does not answer from the /domain listing the way GetRecord
// answers from a zone's records. That listing is account-wide, so a reseller
// with 800 domains would pay eight requests to read three managed ones, and the
// cost would not shrink as the managed set shrank. One request per managed
// domain is at least proportional to what was asked for.
func (c *Client) GetDomain(ctx context.Context, name string) (*Domain, error) {
	var domain Domain
	if err := c.doOne(ctx, http.MethodGet, domainPath(name), nil, &domain); err != nil {
		return nil, err
	}
	if domain.Name == "" {
		domain.Name = name
	}
	return &domain, nil
}

// UpdateDomain writes a domain's settings.
//
// The published schema declares no request body for this endpoint, so the
// payload is inferred from the shape of a read. It has not been exercised
// against the live API: doing so would have changed the renewal settings of a
// production domain.
func (c *Client) UpdateDomain(ctx context.Context, name string, settings DomainSettings) (*Domain, error) {
	defer c.invalidateList("/domain")

	var updated Domain
	if err := c.doOne(ctx, http.MethodPut, domainPath(name), settings, &updated); err != nil {
		return nil, err
	}
	if updated.Name == "" {
		updated.Name = name
	}
	return &updated, nil
}

// ListNameservers returns a domain's delegation.
func (c *Client) ListNameservers(ctx context.Context, domain string) ([]Nameserver, error) {
	raws, err := c.list(ctx, nameserverCollectionPath(domain))
	if err != nil {
		return nil, err
	}
	return decodeList(raws, func(n *Nameserver) string { return strings.ToLower(n.Hostname) })
}

// SetNameservers replaces a domain's delegation.
//
// The endpoint takes an array, which is why this is a whole-set operation and
// there is no "add one nameserver" call: a registry takes a delegation change
// atomically, and most TLDs refuse a set of one.
func (c *Client) SetNameservers(ctx context.Context, domain string, nameservers []NameserverInput) ([]Nameserver, error) {
	defer c.invalidateList(nameserverCollectionPath(domain))

	raws, err := c.do(ctx, http.MethodPost, nameserverCollectionPath(domain), nameservers)
	if err != nil {
		return nil, err
	}
	return decodeList(raws, func(n *Nameserver) string { return strings.ToLower(n.Hostname) })
}

// DeleteNameserver removes one nameserver from a domain's delegation.
func (c *Client) DeleteNameserver(ctx context.Context, domain, hostname string) error {
	defer c.invalidateList(nameserverCollectionPath(domain))

	if _, err := c.do(ctx, http.MethodDelete, nameserverItemPath(domain, hostname), nil); err != nil {
		if IsNotFound(err) {
			return nil
		}
		return err
	}
	return nil
}

// ListContacts returns a domain's registry contacts.
func (c *Client) ListContacts(ctx context.Context, domain string) ([]Contact, error) {
	raws, err := c.list(ctx, contactCollectionPath(domain))
	if err != nil {
		return nil, err
	}
	return decodeList(raws, func(c *Contact) string { return c.Identificator })
}

// GetContact reads one contact, from the domain's cached contact listing.
//
// A domain has a handful of contacts and Terraform reads every resource in
// state on every plan, so one listing per domain costs less than one request
// per contact.
func (c *Client) GetContact(ctx context.Context, domain, id string) (*Contact, error) {
	contacts, err := c.ListContacts(ctx, domain)
	if err != nil {
		return nil, err
	}
	for i := range contacts {
		if contacts[i].Identificator == id {
			return &contacts[i], nil
		}
	}
	return nil, fmt.Errorf("zone.eu: contact %q on domain %q: %w", id, domain, ErrNotFound)
}

// CreateContact adds a contact to a domain.
func (c *Client) CreateContact(ctx context.Context, domain string, contact ContactInput) (*Contact, error) {
	defer c.invalidateList(contactCollectionPath(domain))

	var created Contact
	if err := c.doOne(ctx, http.MethodPost, contactCollectionPath(domain), contact, &created); err != nil {
		return nil, err
	}
	return &created, nil
}

// UpdateContact replaces a contact.
func (c *Client) UpdateContact(ctx context.Context, domain, id string, contact ContactInput) (*Contact, error) {
	defer c.invalidateList(contactCollectionPath(domain))

	var updated Contact
	if err := c.doOne(ctx, http.MethodPut, contactItemPath(domain, id), contact, &updated); err != nil {
		return nil, err
	}
	if updated.Identificator == "" {
		updated.Identificator = id
	}
	return &updated, nil
}

// DeleteContact removes a contact.
func (c *Client) DeleteContact(ctx context.Context, domain, id string) error {
	defer c.invalidateList(contactCollectionPath(domain))

	if _, err := c.do(ctx, http.MethodDelete, contactItemPath(domain, id), nil); err != nil {
		if IsNotFound(err) {
			return nil
		}
		return err
	}
	return nil
}
