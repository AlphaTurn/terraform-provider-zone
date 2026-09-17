package provider

import (
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// known reports whether a configured value is usable — neither absent from the
// configuration nor waiting on another resource's apply.
func known(value attr.Value) bool {
	return !value.IsNull() && !value.IsUnknown()
}

// optionalString maps the API's empty string to a null attribute.
//
// The API uses "" for fields it has nothing to say about, and showing that as
// an empty string in state suggests a value was set deliberately. Record
// comments already take this treatment; the rest of the provider follows.
func optionalString(value string) types.String {
	if value == "" {
		return types.StringNull()
	}
	return types.StringValue(value)
}

func stringPointerValue(value *string) types.String {
	if value == nil {
		return types.StringNull()
	}
	return optionalString(*value)
}

func int64PointerValue(value *int64) types.Int64 {
	if value == nil {
		return types.Int64Null()
	}
	return types.Int64Value(*value)
}
