package provider

import (
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"

	"github.com/AlphaTurn/terraform-provider-zone/internal/zoneapi"
)

// addAPIError turns a client error into diagnostics, attaching the API's
// per-field validation messages to the attributes that set them so the user is
// pointed at the line to fix rather than handed one opaque message.
//
// attributes maps API field name to Terraform attribute name. Fields absent
// from it fall into the summary instead: attaching a message to an attribute
// the schema does not have would replace the API's explanation with a framework
// error about an invalid path.
func addAPIError(diags *diag.Diagnostics, summary string, err error, attributes map[string]string) {
	apiErr, ok := zoneapi.AsAPIError(err)
	if !ok {
		diags.AddError(summary, err.Error())
		return
	}

	switch {
	case zoneapi.IsPaymentRequired(err):
		diags.AddError(
			summary,
			"zone.eu reports that this needs a paid package upgrade, so the operation did not happen.\n\n"+
				"This is not a problem with the configuration: the account's current package does not "+
				"include it. Upgrade the service in the ZoneID panel, then apply again.\n\n"+
				apiErr.Error(),
		)
		return
	case zoneapi.IsForbidden(err):
		diags.AddError(
			summary,
			"zone.eu refused this request for this account.\n\n"+
				"A service delegated to another ZoneID user answers this way on some endpoints while "+
				"allowing others, so the credentials are probably fine and this particular service or "+
				"endpoint is not available to them.\n\n"+
				apiErr.Error(),
		)
		return
	}

	attached := false
	for field, messages := range apiErr.FieldErrors {
		attribute, known := attributes[field]
		if !known {
			continue
		}
		diags.AddAttributeError(
			path.Root(attribute),
			"zone.eu rejected this value",
			strings.Join(messages, "\n"),
		)
		attached = true
	}
	if attached {
		return
	}

	diags.AddError(summary, apiErr.Error())
}

// addNoUpdateError reports an Update that should have been a replacement.
//
// Several endpoints have no update operation, so every writable attribute on
// those resources forces replacement and their Update is unreachable. Saying so
// beats doing nothing quietly, which would look like a successful apply that
// changed nothing.
func addNoUpdateError(diags *diag.Diagnostics, what string) {
	diags.AddError(
		what+" cannot be modified",
		"The zone.eu API has no endpoint for changing this, so every argument on the resource "+
			"forces replacement and this code path should be unreachable.\n\n"+
			"Reaching it is a bug in the provider; please report it.",
	)
}
