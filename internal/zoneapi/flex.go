package zoneapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// The API's own OpenAPI description is inconsistent about scalar types: fields
// declared as integers carry quoted examples and defaults (the URL record's
// redirect type is documented as an integer with a default of "301"), and
// booleans have been observed as 0/1. Rather than fail an entire read over a
// type mismatch in one field, values are coerced from whichever form arrives.
//
// This is tolerance on the way in only. Writes always send the documented type.

func coerceString(raw json.RawMessage) (string, bool) {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s, true
	}

	var number json.Number
	if err := json.Unmarshal(raw, &number); err == nil {
		return number.String(), true
	}
	return "", false
}

func coerceInt64(raw json.RawMessage) (int64, bool) {
	var number json.Number
	if err := json.Unmarshal(raw, &number); err == nil {
		if n, err := number.Int64(); err == nil {
			return n, true
		}
		return 0, false
	}

	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		if n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64); err == nil {
			return n, true
		}
	}
	return 0, false
}

func coerceBool(raw json.RawMessage) (bool, bool) {
	var b bool
	if err := json.Unmarshal(raw, &b); err == nil {
		return b, true
	}

	var number json.Number
	if err := json.Unmarshal(raw, &number); err == nil {
		if n, err := number.Int64(); err == nil {
			return n != 0, true
		}
	}

	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		if parsed, err := strconv.ParseBool(strings.TrimSpace(s)); err == nil {
			return parsed, true
		}
	}
	return false, false
}

// valueToInt64 coerces an already-decoded value, as found in [Record.Extra].
func valueToInt64(value any) (int64, bool) {
	switch v := value.(type) {
	case json.Number:
		n, err := v.Int64()
		return n, err == nil
	case int64:
		return v, true
	case int:
		return int64(v), true
	case float64:
		return int64(v), v == float64(int64(v))
	case string:
		n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		return n, err == nil
	default:
		return 0, false
	}
}

// valueToString coerces an already-decoded value, as found in [Record.Extra].
func valueToString(value any) (string, bool) {
	switch v := value.(type) {
	case string:
		return v, true
	case json.Number:
		return v.String(), true
	case bool:
		return strconv.FormatBool(v), true
	default:
		return "", false
	}
}

// coerceOptionalString is coerceString for a field that is meaningfully null.
//
// Several fields distinguish "no value" from the empty string: a domain's
// delegated is null when the domain is not delegated, and renew_order is null
// when no renewal is pending. Flattening those to "" would report a state the
// API never described.
func coerceOptionalString(raw json.RawMessage) *string {
	if isJSONNull(raw) {
		return nil
	}
	if value, ok := coerceString(raw); ok {
		return &value
	}
	return nil
}

// coerceOptionalInt64 is coerceInt64 for a field that is meaningfully null,
// such as a domain's has_pending_trade.
func coerceOptionalInt64(raw json.RawMessage) *int64 {
	if isJSONNull(raw) {
		return nil
	}
	if value, ok := coerceInt64(raw); ok {
		return &value
	}
	return nil
}

// coerceStringSlice reads an array of strings, tolerating a bare string where
// the API documents a list.
func coerceStringSlice(raw json.RawMessage) ([]string, bool) {
	if isJSONNull(raw) {
		return nil, false
	}

	var list []string
	if err := json.Unmarshal(raw, &list); err == nil {
		return list, true
	}
	if single, ok := coerceString(raw); ok {
		return []string{single}, true
	}
	return nil, false
}

// coerceStringMap reads an object of strings.
//
// This exists for a server_fingerprints, which the published schema calls an
// array of strings and the live API returns as an object keyed by algorithm. An
// array yields nothing rather than an error, because tolerance on the way in is
// this package's house rule.
func coerceStringMap(raw json.RawMessage) (map[string]string, bool) {
	if isJSONNull(raw) {
		return nil, false
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, false
	}

	out := make(map[string]string, len(fields))
	for key, value := range fields {
		if text, ok := coerceString(value); ok {
			out[key] = text
		}
	}
	return out, true
}

func isJSONNull(raw json.RawMessage) bool {
	return len(raw) == 0 || string(bytes.TrimSpace(raw)) == "null"
}

// decodeFields splits an object into its raw fields, preserving numbers in a
// form the coerce helpers can read either way.
//
// Every read model in this package starts here rather than with struct tags,
// because the API's scalar types cannot be trusted and a single surprising
// field should not fail a whole read.
func decodeFields(data []byte, fields *map[string]json.RawMessage) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(fields); err != nil {
		return fmt.Errorf("zone.eu: decoding object: %w", err)
	}
	return nil
}
