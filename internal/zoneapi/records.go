package zoneapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

// RecordType is a DNS record type, spelled as the path segment that serves it.
type RecordType string

// The eleven record types the API exposes. URL is a zone.eu extension that
// redirects a hostname to a URL rather than a standard DNS record type.
const (
	RecordTypeA     RecordType = "a"
	RecordTypeAAAA  RecordType = "aaaa"
	RecordTypeCNAME RecordType = "cname"
	RecordTypeNS    RecordType = "ns"
	RecordTypeMX    RecordType = "mx"
	RecordTypeTXT   RecordType = "txt"
	RecordTypeSRV   RecordType = "srv"
	RecordTypeCAA   RecordType = "caa"
	RecordTypeTLSA  RecordType = "tlsa"
	RecordTypeSSHFP RecordType = "sshfp"
	RecordTypeURL   RecordType = "url"
)

// AllRecordTypes lists every supported type, for callers that sweep a zone.
var AllRecordTypes = []RecordType{
	RecordTypeA,
	RecordTypeAAAA,
	RecordTypeCNAME,
	RecordTypeNS,
	RecordTypeMX,
	RecordTypeTXT,
	RecordTypeSRV,
	RecordTypeCAA,
	RecordTypeTLSA,
	RecordTypeSSHFP,
	RecordTypeURL,
}

// Record is a DNS record.
//
// Every type shares the fields below; type-specific ones (a priority, a CAA tag,
// a TLSA selector) live in Extra, keyed by their API field name. That keeps one
// CRUD implementation serving all eleven endpoints, which differ only in those
// extras.
//
// There is no TTL. The API does not expose one on any record type.
type Record struct {
	// ID is the record's identifier, used in per-record URLs. Server-assigned.
	ID int64
	// Name is the fully qualified record name, for example "www.example.com".
	Name string
	// Destination is the record's value: an address, a hostname, text content.
	Destination string
	// ResourceURL is the API URL for this record. Server-assigned.
	ResourceURL string
	// Comment is a server-side note. Read-only.
	Comment string
	// Modifiable and Deletable report whether the API permits changing or
	// removing this record. zone.eu marks some records as managed on its side,
	// and rejects writes to them.
	Modifiable bool
	Deletable  bool
	// Extra holds type-specific fields keyed by API field name.
	Extra map[string]any
}

// Read-only fields, never sent back on a write.
const (
	fieldID          = "id"
	fieldName        = "name"
	fieldDestination = "destination"
	fieldResourceURL = "resource_url"
	fieldComment     = "comment"
	fieldModify      = "modify"
	fieldDelete      = "delete"
)

// UnmarshalJSON splits the flat API object into the shared fields and Extra.
func (r *Record) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(&raw); err != nil {
		return fmt.Errorf("zone.eu: decoding record: %w", err)
	}

	*r = Record{Extra: make(map[string]any)}

	for key, value := range raw {
		switch key {
		case fieldID:
			r.ID, _ = coerceInt64(value)
		case fieldName:
			r.Name, _ = coerceString(value)
		case fieldDestination:
			r.Destination, _ = coerceString(value)
		case fieldResourceURL:
			r.ResourceURL, _ = coerceString(value)
		case fieldComment:
			r.Comment, _ = coerceString(value)
		case fieldModify:
			r.Modifiable, _ = coerceBool(value)
		case fieldDelete:
			r.Deletable, _ = coerceBool(value)
		default:
			var decoded any
			d := json.NewDecoder(bytes.NewReader(value))
			d.UseNumber()
			if err := d.Decode(&decoded); err == nil {
				r.Extra[key] = decoded
			}
		}
	}
	return nil
}

// MarshalJSON emits only the writable fields. id, resource_url, comment, modify
// and delete are read-only, and sending them back is at best ignored.
func (r Record) MarshalJSON() ([]byte, error) {
	payload := make(map[string]any, len(r.Extra)+2)
	for key, value := range r.Extra {
		payload[key] = value
	}
	payload[fieldName] = r.Name
	payload[fieldDestination] = r.Destination
	return json.Marshal(payload)
}

// ExtraInt64 reads a type-specific integer field.
func (r Record) ExtraInt64(key string) (int64, bool) {
	value, ok := r.Extra[key]
	if !ok {
		return 0, false
	}
	return valueToInt64(value)
}

// ExtraString reads a type-specific string field.
func (r Record) ExtraString(key string) (string, bool) {
	value, ok := r.Extra[key]
	if !ok {
		return "", false
	}
	return valueToString(value)
}

// SetExtra sets a type-specific field.
func (r *Record) SetExtra(key string, value any) {
	if r.Extra == nil {
		r.Extra = make(map[string]any)
	}
	r.Extra[key] = value
}

func recordCollectionPath(zone string, recordType RecordType) string {
	return "/dns/" + url.PathEscape(zone) + "/" + string(recordType)
}

func recordItemPath(zone string, recordType RecordType, id int64) string {
	return recordCollectionPath(zone, recordType) + "/" + strconv.FormatInt(id, 10)
}

func recordCacheKey(zone string, recordType RecordType) string {
	return zone + "/" + string(recordType)
}

// ListRecords returns every record of one type in a zone.
//
// The listing endpoint is unpaginated, so this is a single request regardless of
// how many records the zone holds, and the result is cached briefly and shared
// between concurrent callers.
func (c *Client) ListRecords(ctx context.Context, zone string, recordType RecordType) ([]Record, error) {
	raws, err := c.cache.fetch(recordCacheKey(zone, recordType), func() ([]json.RawMessage, error) {
		return c.do(ctx, http.MethodGet, recordCollectionPath(zone, recordType), nil)
	})
	if err != nil {
		return nil, err
	}

	records := make([]Record, 0, len(raws))
	for _, raw := range raws {
		var record Record
		if err := decode(raw, &record); err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, nil
}

// GetRecord returns one record by ID.
//
// It is answered from the zone's listing rather than by fetching the per-record
// endpoint. Terraform reads every resource in state on every plan, so a direct
// GET would cost one request per record; going through the cached listing costs
// one request per record type no matter how many records are managed.
func (c *Client) GetRecord(ctx context.Context, zone string, recordType RecordType, id int64) (*Record, error) {
	records, err := c.ListRecords(ctx, zone, recordType)
	if err != nil {
		return nil, err
	}

	for i := range records {
		if records[i].ID == id {
			return &records[i], nil
		}
	}
	return nil, fmt.Errorf("zone.eu: %s record %d in zone %q: %w", recordType, id, zone, ErrNotFound)
}

// CreateRecord adds a record to a zone and returns it as stored.
func (c *Client) CreateRecord(ctx context.Context, zone string, recordType RecordType, record Record) (*Record, error) {
	defer c.cache.invalidate(recordCacheKey(zone, recordType))

	var created Record
	if err := c.doOne(ctx, http.MethodPost, recordCollectionPath(zone, recordType), record, &created); err != nil {
		return nil, err
	}
	return &created, nil
}

// UpdateRecord replaces a record and returns it as stored.
func (c *Client) UpdateRecord(ctx context.Context, zone string, recordType RecordType, id int64, record Record) (*Record, error) {
	defer c.cache.invalidate(recordCacheKey(zone, recordType))

	var updated Record
	if err := c.doOne(ctx, http.MethodPut, recordItemPath(zone, recordType, id), record, &updated); err != nil {
		return nil, err
	}
	return &updated, nil
}

// DeleteRecord removes a record. Deleting an already-absent record is not an
// error, so that a destroy after an out-of-band removal still converges.
func (c *Client) DeleteRecord(ctx context.Context, zone string, recordType RecordType, id int64) error {
	defer c.cache.invalidate(recordCacheKey(zone, recordType))

	if _, err := c.do(ctx, http.MethodDelete, recordItemPath(zone, recordType, id), nil); err != nil {
		if IsNotFound(err) {
			return nil
		}
		return err
	}
	return nil
}
