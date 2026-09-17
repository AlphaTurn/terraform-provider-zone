package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// stringSetValue converts a list of strings from the API into a Terraform set.
//
// Sets rather than lists, throughout: the API returns these collections in an
// order it chooses, not the order they were configured, and a list would show a
// permanent diff every time the server reordered one. Nothing the provider
// sends in a collection has meaningful order.
//
// A nil slice becomes a null set rather than an empty one, so "the API said
// nothing" stays distinguishable from "the API said none".
func stringSetValue(values []string) (types.Set, diag.Diagnostics) {
	if values == nil {
		return types.SetNull(types.StringType), nil
	}

	elements := make([]attr.Value, 0, len(values))
	for _, value := range values {
		elements = append(elements, types.StringValue(value))
	}
	return types.SetValue(types.StringType, elements)
}

// setToStrings reads a Terraform set into a list of strings. A null or unknown
// set yields nil, which callers omit from a request rather than sending as an
// empty collection.
func setToStrings(ctx context.Context, set types.Set, diags *diag.Diagnostics) []string {
	if set.IsNull() || set.IsUnknown() {
		return nil
	}

	var values []string
	diags.Append(set.ElementsAs(ctx, &values, false)...)
	return values
}
