package dnsrecord

import (
	"context"
	"net/netip"

	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

// IsIPv4 requires a bare IPv4 address.
func IsIPv4() validator.String { return ipValidator{want: ipv4} }

// IsIPv6 requires a bare IPv6 address.
func IsIPv6() validator.String { return ipValidator{want: ipv6} }

type ipFamily int

const (
	ipv4 ipFamily = iota
	ipv6
)

type ipValidator struct {
	want ipFamily
}

func (v ipValidator) Description(context.Context) string {
	if v.want == ipv4 {
		return "must be an IPv4 address"
	}
	return "must be an IPv6 address"
}

func (v ipValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v ipValidator) ValidateString(ctx context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}

	value := req.ConfigValue.ValueString()
	addr, err := netip.ParseAddr(value)
	if err != nil {
		resp.Diagnostics.AddAttributeError(
			req.Path,
			"Invalid IP address",
			v.Description(ctx)+", but "+value+" could not be parsed as one.",
		)
		return
	}

	// Is4In6 catches ::ffff:1.2.3.4, which parses as IPv6 but addresses an IPv4
	// host; an AAAA record pointing at one is almost never what was meant.
	switch {
	case v.want == ipv4 && !addr.Is4():
		resp.Diagnostics.AddAttributeError(
			req.Path,
			"Expected an IPv4 address",
			"An A record takes an IPv4 address, but "+value+" is IPv6. Use zone_dns_aaaa_record instead.",
		)
	case v.want == ipv6 && (!addr.Is6() || addr.Is4In6()):
		resp.Diagnostics.AddAttributeError(
			req.Path,
			"Expected an IPv6 address",
			"An AAAA record takes an IPv6 address, but "+value+" is IPv4. Use zone_dns_a_record instead.",
		)
	}
}
