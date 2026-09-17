package zoneapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
)

// ErrNotFound reports that the API has no such resource. The API signals this
// both with a 404 and, on single-resource reads, with a 200 carrying an empty
// array, so callers should test with [IsNotFound] rather than comparing status
// codes themselves.
var ErrNotFound = errors.New("zone.eu: resource not found")

// APIError describes a non-success response from the zone.eu API.
//
// Validation failures arrive as a 422 whose body mirrors the shape of the
// submitted resource, mapping each offending field to its messages (for example
// {"name": ["is invalid"]}). Those are collected into FieldErrors so a caller
// can attach each message to the attribute that actually caused it instead of
// dumping one opaque blob on the user. Every other failure carries a human
// readable string in the X-Status-Message header.
type APIError struct {
	StatusCode  int
	Method      string
	Path        string
	Message     string
	FieldErrors map[string][]string
	Body        string
}

func (e *APIError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "zone.eu API: %s %s returned %d", e.Method, e.Path, e.StatusCode)
	if e.Message != "" {
		fmt.Fprintf(&b, ": %s", e.Message)
	}
	if len(e.FieldErrors) > 0 {
		fields := make([]string, 0, len(e.FieldErrors))
		for f := range e.FieldErrors {
			fields = append(fields, f)
		}
		sort.Strings(fields)

		parts := make([]string, 0, len(fields))
		for _, f := range fields {
			parts = append(parts, fmt.Sprintf("%s: %s", f, strings.Join(e.FieldErrors[f], "; ")))
		}
		fmt.Fprintf(&b, " (%s)", strings.Join(parts, ", "))
	}
	return b.String()
}

// IsNotFound reports whether err means the resource does not exist.
func IsNotFound(err error) bool {
	if errors.Is(err, ErrNotFound) {
		return true
	}
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound
}

// IsPaymentRequired reports whether err is the API's 402, which it returns when
// an operation needs a paid package upgrade rather than when a request is
// malformed.
//
// This is worth distinguishing because the fix is a purchase, not a change to
// the configuration, and because it means the operation did not happen: a
// generic "bad request" would send the user looking for a mistake in their HCL
// that is not there.
func IsPaymentRequired(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusPaymentRequired
}

// IsForbidden reports whether err is the API's 403.
//
// A service delegated to another ZoneID user answers 403 on some of its
// endpoints while answering 200 on others, so this is not an authentication
// problem and must not be treated as one. It is emphatically not [IsNotFound]
// either: reporting a forbidden resource as missing would make Terraform drop
// it from state and then offer to create it again.
func IsForbidden(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusForbidden
}

// AsAPIError extracts the underlying [APIError], if there is one.
func AsAPIError(err error) (*APIError, bool) {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr, true
	}
	return nil, false
}

// parseErrorBody pulls whatever diagnostics it can out of an error response.
// The API is not consistent here: the documented shape is {"messages": [...]},
// but validation errors come back keyed by field, sometimes with a bare string
// instead of a list, and either form may be wrapped in the usual array envelope.
func parseErrorBody(body []byte) (messages []string, fieldErrors map[string][]string) {
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return nil, nil
	}

	if body[0] == '[' {
		var arr []json.RawMessage
		if err := json.Unmarshal(body, &arr); err != nil || len(arr) == 0 {
			return nil, nil
		}
		return parseErrorBody(arr[0])
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, nil
	}

	if m, ok := raw["messages"]; ok {
		var msgs []string
		if err := json.Unmarshal(m, &msgs); err == nil && len(msgs) > 0 {
			return msgs, nil
		}
	}

	fields := make(map[string][]string, len(raw))
	for field, value := range raw {
		var list []string
		if err := json.Unmarshal(value, &list); err == nil {
			if len(list) > 0 {
				fields[field] = list
			}
			continue
		}
		var single string
		if err := json.Unmarshal(value, &single); err == nil && single != "" {
			fields[field] = []string{single}
		}
	}
	if len(fields) == 0 {
		return nil, nil
	}
	return nil, fields
}
