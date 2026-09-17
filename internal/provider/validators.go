package provider

import (
	"context"
	"fmt"
	"net/netip"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

// stringCheck adapts a plain function into a string validator, which is all
// these need: each one reports a single sentence naming the fix.
type stringCheck struct {
	description string
	check       func(string) error
}

func (v stringCheck) Description(context.Context) string         { return v.description }
func (v stringCheck) MarkdownDescription(context.Context) string { return v.description }

func (v stringCheck) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	if err := v.check(req.ConfigValue.ValueString()); err != nil {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid value", err.Error())
	}
}

// isIPOrPrefix accepts an address or a network, since the whitelist endpoints
// document prefixes such as 217.128.0.0/24 alongside bare addresses.
func isIPOrPrefix() validator.String {
	return stringCheck{
		description: "must be an IPv4 or IPv6 address, optionally with a prefix length",
		check: func(value string) error {
			if strings.Contains(value, "/") {
				if _, err := netip.ParsePrefix(value); err != nil {
					return fmt.Errorf("%q is not a valid IP network. Write it as 217.128.0.0/24.", value)
				}
				return nil
			}
			if _, err := netip.ParseAddr(value); err != nil {
				return fmt.Errorf(
					"%q is not a valid IP address. Use an IPv4 or IPv6 address, or a network such as 217.128.0.0/24.",
					value,
				)
			}
			return nil
		},
	}
}

// isEmailAddress is a deliberately shallow check: the registry and the mail
// server are the real authorities, and a strict local pattern would reject
// addresses they accept. It catches the mistakes worth catching before an
// apply.
func isEmailAddress() validator.String {
	return stringCheck{
		description: "must be an email address",
		check: func(value string) error {
			local, domain, found := strings.Cut(value, "@")
			if !found || local == "" || domain == "" || strings.Contains(domain, "@") {
				return fmt.Errorf("%q is not an email address. It needs a local part, an @ and a domain.", value)
			}
			if !strings.Contains(domain, ".") {
				return fmt.Errorf("%q has no domain suffix, so it cannot receive mail.", value)
			}
			return nil
		},
	}
}

// noPathSeparator rejects values that would not survive being interpolated into
// a URL path, which is where every identifier in this API ends up.
func noPathSeparator() validator.String {
	return stringCheck{
		description: "must not contain a slash",
		check: func(value string) error {
			if strings.ContainsAny(value, "/\\") {
				return fmt.Errorf("%q contains a slash, which cannot appear in a zone.eu identifier.", value)
			}
			if strings.TrimSpace(value) != value {
				return fmt.Errorf("%q has leading or trailing whitespace.", value)
			}
			return nil
		},
	}
}
