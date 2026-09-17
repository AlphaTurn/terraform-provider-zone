package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// writeOnlyNote explains write-only arguments once, for the several resources
// that have one.
const writeOnlyNote = "~> **Secrets here are write-only arguments and need Terraform 1.11 or newer.** " +
	"They are sent to zone.eu and never written to state, because state is not an encrypted store: " +
	"a secret in it is a secret in every backup and CI artefact the state file passes through. The " +
	"API agrees — it accepts these fields and never returns them."

// passwordAttribute is the write-only password argument.
//
// The length bound comes from the API, which documents 10 to 64 characters for
// mail and database accounts. It is safe to validate locally because
// LengthBetween reports only the length it found, never the value.
func passwordAttribute(description string) schema.StringAttribute {
	return schema.StringAttribute{
		Optional:  true,
		WriteOnly: true,
		Sensitive: true,
		MarkdownDescription: description + "\n\nWrite-only: sent to zone.eu and never stored in " +
			"state. Between 10 and 64 characters.",
		Validators: []validator.String{stringvalidator.LengthBetween(10, 64)},
	}
}

// passwordVersionAttribute is the companion that makes a password rotation
// visible to Terraform.
//
// A write-only value is absent from state and from the plan, so changing only
// the password produces no diff and nothing happens. Unlike a certificate,
// whose key always changes alongside a certificate body that *is* in state, a
// password has nothing else bound to it — so there has to be something the user
// can change to say "send it again".
func passwordVersionAttribute() schema.Int64Attribute {
	return schema.Int64Attribute{
		Optional: true,
		MarkdownDescription: "Change this to re-send `password`. Terraform cannot see a change to a " +
			"write-only value, so rotating the password means changing this number as well — any " +
			"new value will do. A password changed outside Terraform is undetectable by definition.",
		Validators: []validator.Int64{int64validator.AtLeast(0)},
	}
}

// writeOnlyString reads a write-only argument from the configuration.
//
// This is the one mechanical trap in a write-only attribute, and it is worth
// having in one place: the framework nulls write-only values out of the plan,
// so reading one from req.Plan silently yields an empty string and sends an
// empty secret.
func writeOnlyString(ctx context.Context, config configSource, name string, diags *diag.Diagnostics) string {
	var value types.String
	diags.Append(config.GetAttribute(ctx, path.Root(name), &value)...)
	if diags.HasError() {
		return ""
	}
	return value.ValueString()
}
