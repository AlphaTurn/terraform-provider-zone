package zoneapi

import (
	"encoding/json"
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
