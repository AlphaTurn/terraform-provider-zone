package provider

import (
	"fmt"
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
