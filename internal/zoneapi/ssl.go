package zoneapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

// Certificate is a TLS certificate installed on a webhosting service.
//
// The private key is deliberately absent from this type. The API declares
// private_key as a required, readable field, but a real read returns it as an
// empty string — it is write-only in practice whatever the schema says — so
// there is nothing to model and nothing that could leak into state.
type Certificate struct {
	ID            string
	Name          string
	CommonName    string
	Hosts         []string
	Certificate   string
	CACertificate string
	LetsEncrypt   bool
	Connected     bool
	Created       string
	Expires       string
	ResourceURL   string
}

func (c *Certificate) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := decodeFields(data, &fields); err != nil {
		return err
	}

	// Documented as an integer, returned quoted. It is only ever a path
	// segment and an import key, so it stays text and both forms decode.
	c.ID, _ = coerceString(fields["id"])
	if c.ID == "" {
		c.ID, _ = coerceString(fields["identificator"])
	}

	for target, field := range map[*string]string{
		&c.Name:          "name",
		&c.CommonName:    "cn",
		&c.Certificate:   "certificate",
		&c.CACertificate: "ca_certificate",
		&c.Created:       "created",
		&c.Expires:       "expires",
		&c.ResourceURL:   "resource_url",
	} {
		*target, _ = coerceString(fields[field])
	}

	c.LetsEncrypt, _ = coerceBool(fields["letsencrypt"])
	c.Connected, _ = coerceBool(fields["connected"])
	c.Hosts, _ = coerceStringSlice(fields["hosts"])
	return nil
}

// CertificateInput is a certificate as submitted. The private key is sent and
// never stored.
type CertificateInput struct {
	Name          string   `json:"name"`
	Certificate   string   `json:"certificate"`
	PrivateKey    string   `json:"private_key"`
	CACertificate string   `json:"ca_certificate,omitempty"`
	Hosts         []string `json:"hosts,omitempty"`
}

func certificateCollectionPath(service string) string {
	return vserverPath(service) + "/ssl"
}

func certificateItemPath(service, id string) string {
	return certificateCollectionPath(service) + "/" + url.PathEscape(id)
}

// ListCertificates returns a service's certificates.
//
// This endpoint paginates on the wire although its published schema declares no
// pager parameters, which is why listPaged rather than list.
func (c *Client) ListCertificates(ctx context.Context, service string) ([]Certificate, error) {
	raws, err := c.listPaged(ctx, certificateCollectionPath(service))
	if err != nil {
		return nil, err
	}
	return decodeList(raws, func(cert *Certificate) string { return cert.ID })
}

// GetCertificate reads one certificate from the service's cached listing.
//
// The listing carries the full certificate and chain, verified against the live
// API, so there is nothing the per-item endpoint would add and a refresh of
// twenty certificates costs one request instead of twenty.
func (c *Client) GetCertificate(ctx context.Context, service, id string) (*Certificate, error) {
	certificates, err := c.ListCertificates(ctx, service)
	if err != nil {
		return nil, err
	}
	for i := range certificates {
		if certificates[i].ID == id {
			return &certificates[i], nil
		}
	}
	return nil, fmt.Errorf("zone.eu: certificate %q on service %q: %w", id, service, ErrNotFound)
}

// CreateCertificate installs a certificate.
func (c *Client) CreateCertificate(ctx context.Context, service string, input CertificateInput) (*Certificate, error) {
	defer c.invalidateList(certificateCollectionPath(service))

	var created Certificate
	if err := c.doOne(ctx, http.MethodPost, certificateCollectionPath(service), input, &created); err != nil {
		return nil, err
	}
	return &created, nil
}

// UpdateCertificate replaces a certificate. The API declares the key required
// on this endpoint too, so an update re-sends it.
func (c *Client) UpdateCertificate(ctx context.Context, service, id string, input CertificateInput) (*Certificate, error) {
	defer c.invalidateList(certificateCollectionPath(service))

	var updated Certificate
	if err := c.doOne(ctx, http.MethodPut, certificateItemPath(service, id), input, &updated); err != nil {
		return nil, err
	}
	if updated.ID == "" {
		updated.ID = id
	}
	return &updated, nil
}

// DeleteCertificate removes a certificate.
func (c *Client) DeleteCertificate(ctx context.Context, service, id string) error {
	defer c.invalidateList(certificateCollectionPath(service))

	if _, err := c.do(ctx, http.MethodDelete, certificateItemPath(service, id), nil); err != nil {
		if IsNotFound(err) {
			return nil
		}
		return err
	}
	return nil
}
