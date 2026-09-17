package provider

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
)

// importParts splits an import ID into exactly n slash-separated, non-empty
// parts, explaining the expected shape when it does not.
//
// shape is written the way the docs write it, for example "<domain>/<contact
// id>", and example is a real one.
func importParts(diags *diag.Diagnostics, id string, n int, shape, example string) ([]string, bool) {
	parts := strings.Split(id, "/")
	if len(parts) != n {
		diags.AddError(
			"Invalid import ID",
			fmt.Sprintf("Expected %q, for example %q, but got %q.", shape, example, id),
		)
		return nil, false
	}
	for _, part := range parts {
		if part == "" {
			diags.AddError(
				"Invalid import ID",
				fmt.Sprintf("Expected %q, for example %q, but %q has an empty part.", shape, example, id),
			)
			return nil, false
		}
	}
	return parts, true
}

// importKey splits an import ID into a first segment and the whole remainder.
//
// This exists for keys that may themselves contain a slash: an SSH whitelist
// entry is identified by an IP that can carry a prefix length, and splitting
// "srv/217.128.0.0/24" into three parts would be wrong.
func importKey(diags *diag.Diagnostics, id, shape, example string) (string, string, bool) {
	first, rest, found := strings.Cut(id, "/")
	if !found || first == "" || rest == "" {
		diags.AddError(
			"Invalid import ID",
			fmt.Sprintf("Expected %q, for example %q, but got %q.", shape, example, id),
		)
		return "", "", false
	}
	return first, rest, true
}

// parseImportInt64 reads a numeric part of an import ID.
func parseImportInt64(diags *diag.Diagnostics, raw string) (int64, bool) {
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		diags.AddError(
			"Invalid import ID",
			fmt.Sprintf("Expected a numeric identifier, but %q could not be parsed.\n\n"+
				"Identifiers are visible in the resource_url of an existing object, or from the API.", raw),
		)
		return 0, false
	}
	return id, true
}
