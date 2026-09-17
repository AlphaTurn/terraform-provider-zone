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
//
// check returns the explanation to show, or an empty string when the value is
// acceptable. It deliberately does not return an error: these are complete
// sentences written for the person reading the plan, not Go error strings, and
// making them errors would mean writing them in a style that reads badly in a
// diagnostic.
type stringCheck struct {
	description string
	check       func(string) string
}

func (v stringCheck) Description(context.Context) string         { return v.description }
func (v stringCheck) MarkdownDescription(context.Context) string { return v.description }

func (v stringCheck) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	if problem := v.check(req.ConfigValue.ValueString()); problem != "" {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid value", problem)
	}
}

// isIPOrPrefix accepts an address or a network, since the whitelist endpoints
// document prefixes such as 217.128.0.0/24 alongside bare addresses.
func isIPOrPrefix() validator.String {
	return stringCheck{
		description: "must be an IPv4 or IPv6 address, optionally with a prefix length",
		check: func(value string) string {
			if strings.Contains(value, "/") {
				if _, err := netip.ParsePrefix(value); err != nil {
					return fmt.Sprintf("%q is not a valid IP network. Write it as 217.128.0.0/24.", value)
				}
				return ""
			}
			if _, err := netip.ParseAddr(value); err != nil {
				return fmt.Sprintf(
					"%q is not a valid IP address. Use an IPv4 or IPv6 address, or a network such as 217.128.0.0/24.",
					value,
				)
			}
			return ""
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
		check: func(value string) string {
			local, domain, found := strings.Cut(value, "@")
			if !found || local == "" || domain == "" || strings.Contains(domain, "@") {
				return fmt.Sprintf("%q is not an email address. It needs a local part, an @ and a domain.", value)
			}
			if !strings.Contains(domain, ".") {
				return fmt.Sprintf("%q has no domain suffix, so it cannot receive mail.", value)
			}
			return ""
		},
	}
}

// noPathSeparator rejects values that would not survive being interpolated into
// a URL path, which is where every identifier in this API ends up.
func noPathSeparator() validator.String {
	return stringCheck{
		description: "must not contain a slash",
		check: func(value string) string {
			if strings.ContainsAny(value, "/\\") {
				return fmt.Sprintf("%q contains a slash, which cannot appear in a zone.eu identifier.", value)
			}
			if strings.TrimSpace(value) != value {
				return fmt.Sprintf("%q has leading or trailing whitespace.", value)
			}
			return ""
		},
	}
}

// isPEMBlock checks that a value looks like the PEM block it is supposed to be.
//
// This is hand-written rather than a stringvalidator.RegexMatches on purpose.
// That validator builds its diagnostic with the offending value interpolated
// into the message, which would print a malformed private key in full to the
// terminal and into any log capturing it. Nothing here ever reports the value:
// only what kind of block was found instead.
func isPEMBlock(kind string) validator.String {
	want := "-----BEGIN " + kind + "-----"

	return stringCheck{
		description: "must be a PEM-encoded " + strings.ToLower(kind),
		check: func(value string) string {
			trimmed := strings.TrimSpace(value)
			if trimmed == "" {
				return fmt.Sprintf("is empty, but a %s is required.", strings.ToLower(kind))
			}
			if strings.Contains(trimmed, want) {
				return ""
			}

			// Naming the block that was supplied is the single most useful
			// thing to say, and it is safe: a BEGIN line carries no secret.
			if begin := pemBeginLine(trimmed); begin != "" {
				return fmt.Sprintf(
					"is a %q block, but a %q block is required here. Check that the right file is "+
						"in the right argument.", begin, want,
				)
			}
			return fmt.Sprintf(
				"is not PEM encoded: no %q line was found. Supply the file's contents, for example "+
					"with file(\"cert.pem\"), rather than its path.", want,
			)
		},
	}
}

// pemBeginLine returns the BEGIN line of a PEM block, or "" if there is none.
func pemBeginLine(value string) string {
	for line := range strings.SplitSeq(value, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "-----BEGIN ") && strings.HasSuffix(line, "-----") {
			return line
		}
	}
	return ""
}

// isSSHPublicKey checks the shape of an OpenSSH public key line.
//
// It catches the two mistakes people actually make: pasting a private key, and
// passing a path instead of the file's contents. It does not try to validate
// the key material, which is the server's job.
func isSSHPublicKey() validator.String {
	return stringCheck{
		description: "must be an OpenSSH public key",
		check: func(value string) string {
			trimmed := strings.TrimSpace(value)
			if strings.HasPrefix(trimmed, "-----BEGIN") {
				return fmt.Sprintf(
					"is a PEM block, not an OpenSSH public key. This wants the single line from a " +
						".pub file, and a private key must never be sent here.",
				)
			}

			fields := strings.Fields(trimmed)
			if len(fields) < 2 {
				return fmt.Sprintf(
					"is not an OpenSSH public key: it should read like \"ssh-ed25519 AAAA... comment\". " +
						"Pass the file's contents, for example with file(\"~/.ssh/id_ed25519.pub\").",
				)
			}

			algorithm := fields[0]
			for _, prefix := range []string{"ssh-", "ecdsa-", "sk-"} {
				if strings.HasPrefix(algorithm, prefix) {
					return ""
				}
			}
			return fmt.Sprintf("names an unrecognised key algorithm %q.", algorithm)
		},
	}
}
