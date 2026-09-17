package zoneapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

// Zone is a DNS zone.
//
// Zones cannot be created or deleted through the API: one appears when a domain
// or hosting service is bought and disappears with it. Only the settings below
// are writable.
type Zone struct {
	// Identificator is the zone's name, for example "example.com".
	Identificator string
	// ResourceURL is the API URL for this zone. Server-assigned.
	ResourceURL string
	// Active reports whether the zone is served.
	Active bool
	// IPv6 reports whether zone.eu's IPv6 support is enabled for the zone.
	IPv6 bool
	// DNSSEC reports whether the zone is signed. Read-only, and not part of the
	// published schema, but the API returns it.
	DNSSEC bool
}

// UnmarshalJSON decodes a zone, tolerating the loose scalar typing the API uses
// elsewhere.
func (z *Zone) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(&raw); err != nil {
		return fmt.Errorf("zone.eu: decoding zone: %w", err)
	}

	*z = Zone{}
	if value, ok := raw["identificator"]; ok {
		z.Identificator, _ = coerceString(value)
	}
	if value, ok := raw["resource_url"]; ok {
		z.ResourceURL, _ = coerceString(value)
	}
	if value, ok := raw["active"]; ok {
		z.Active, _ = coerceBool(value)
	}
	if value, ok := raw["ipv6"]; ok {
		z.IPv6, _ = coerceBool(value)
	}
	if value, ok := raw["dnssec"]; ok {
		z.DNSSEC, _ = coerceBool(value)
	}
	return nil
}

// ZoneSettings is the writable part of a zone.
type ZoneSettings struct {
	Active bool `json:"active"`
	IPv6   bool `json:"ipv6"`
}

func zonePath(zone string) string {
	return "/dns/" + url.PathEscape(zone)
}

// GetZone reads a zone's settings.
func (c *Client) GetZone(ctx context.Context, zone string) (*Zone, error) {
	var result Zone
	if err := c.doOne(ctx, http.MethodGet, zonePath(zone), nil, &result); err != nil {
		return nil, err
	}
	if result.Identificator == "" {
		result.Identificator = zone
	}
	return &result, nil
}

// UpdateZone writes a zone's settings.
func (c *Client) UpdateZone(ctx context.Context, zone string, settings ZoneSettings) (*Zone, error) {
	var result Zone
	if err := c.doOne(ctx, http.MethodPut, zonePath(zone), settings, &result); err != nil {
		return nil, err
	}
	if result.Identificator == "" {
		result.Identificator = zone
	}
	return &result, nil
}
