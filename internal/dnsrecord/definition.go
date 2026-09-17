// Package dnsrecord implements the zone.eu DNS record resources.
//
// The API serves each record type from its own endpoint, but those endpoints
// differ only in a handful of type-specific fields: a priority on MX, a tag on
// CAA, a selector on TLSA. So the CRUD is written once, in resource.go, and
// each record type is described declaratively by a [Definition] here.
package dnsrecord

import (
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"

	"github.com/AlphaTurn/terraform-provider-zone/internal/zoneapi"
)

// Kind is the Terraform type of a type-specific attribute.
type Kind int

const (
	// KindInt64 is a whole-number attribute.
	KindInt64 Kind = iota
	// KindString is a string attribute.
	KindString
)

// Extra is an attribute present on only some record types.
type Extra struct {
	// Name is the Terraform attribute name.
	Name string
	// APIField is the JSON field name, when it differs from Name. Two record
	// types call a field "type", which would be needlessly confusing on a
	// resource that is already named after its type.
	APIField string
	// Description documents the attribute.
	Description string
	// Kind is the attribute's type.
	Kind Kind
	// Required marks an attribute the record is meaningless without.
	Required bool
	// Int64Default, when set, supplies a default for an optional integer
	// attribute, matching what the API would have chosen anyway.
	Int64Default *int64

	Int64Validators  []validator.Int64
	StringValidators []validator.String
}

// APIName is the JSON field this attribute maps to.
func (e Extra) APIName() string {
	if e.APIField != "" {
		return e.APIField
	}
	return e.Name
}

// Definition describes one record type's Terraform resource.
type Definition struct {
	// TypeName is the resource name without the provider prefix, so
	// "dns_a_record" becomes zone_dns_a_record.
	TypeName string
	// RecordType is the API path segment serving this type.
	RecordType zoneapi.RecordType
	// Description documents the resource.
	Description string
	// DestinationDescription documents what destination means here, which
	// varies from an IP address to free text.
	DestinationDescription string
	// DestinationValidators constrain destination.
	DestinationValidators []validator.String
	// Extras are this type's additional attributes.
	Extras []Extra
}

func int64Ptr(v int64) *int64 { return &v }

// Definitions returns every record type the provider manages.
func Definitions() []Definition {
	return []Definition{
		{
			TypeName:               "dns_a_record",
			RecordType:             zoneapi.RecordTypeA,
			Description:            "An `A` record, mapping a hostname to an IPv4 address.",
			DestinationDescription: "The IPv4 address the name resolves to.",
			DestinationValidators:  []validator.String{IsIPv4()},
		},
		{
			TypeName:               "dns_aaaa_record",
			RecordType:             zoneapi.RecordTypeAAAA,
			Description:            "An `AAAA` record, mapping a hostname to an IPv6 address.",
			DestinationDescription: "The IPv6 address the name resolves to.",
			DestinationValidators:  []validator.String{IsIPv6()},
		},
		{
			TypeName:               "dns_cname_record",
			RecordType:             zoneapi.RecordTypeCNAME,
			Description:            "A `CNAME` record, aliasing one name to another.",
			DestinationDescription: "The canonical hostname this name is an alias for.",
		},
		{
			TypeName:               "dns_ns_record",
			RecordType:             zoneapi.RecordTypeNS,
			Description:            "An `NS` record, delegating a subdomain to other nameservers.",
			DestinationDescription: "The hostname of the nameserver authoritative for the name.",
		},
		{
			TypeName:               "dns_txt_record",
			RecordType:             zoneapi.RecordTypeTXT,
			Description:            "A `TXT` record, holding free-form text such as SPF or domain verification tokens.",
			DestinationDescription: "The text content of the record.",
		},
		{
			TypeName:               "dns_mx_record",
			RecordType:             zoneapi.RecordTypeMX,
			Description:            "An `MX` record, naming a mail exchanger for the domain.",
			DestinationDescription: "The hostname of the mail exchanger.",
			Extras: []Extra{
				{
					Name:            "priority",
					Description:     "Preference for this mail exchanger. Lower values are tried first.",
					Kind:            KindInt64,
					Required:        true,
					Int64Validators: []validator.Int64{int64validator.Between(0, 65535)},
				},
			},
		},
		{
			TypeName:               "dns_srv_record",
			RecordType:             zoneapi.RecordTypeSRV,
			Description:            "An `SRV` record, advertising the host and port of a service.",
			DestinationDescription: "The hostname of the machine providing the service.",
			Extras: []Extra{
				{
					Name:            "priority",
					Description:     "Priority of this target. Lower values are tried first.",
					Kind:            KindInt64,
					Required:        true,
					Int64Validators: []validator.Int64{int64validator.Between(0, 65535)},
				},
				{
					Name:            "weight",
					Description:     "Relative weight for targets sharing a priority.",
					Kind:            KindInt64,
					Required:        true,
					Int64Validators: []validator.Int64{int64validator.Between(0, 65535)},
				},
				{
					Name:            "port",
					Description:     "TCP or UDP port the service listens on.",
					Kind:            KindInt64,
					Required:        true,
					Int64Validators: []validator.Int64{int64validator.Between(0, 65535)},
				},
			},
		},
		{
			TypeName:               "dns_caa_record",
			RecordType:             zoneapi.RecordTypeCAA,
			Description:            "A `CAA` record, declaring which certificate authorities may issue for the domain.",
			DestinationDescription: "The property value, such as the CA's domain for an `issue` tag.",
			Extras: []Extra{
				{
					Name:            "flag",
					Description:     "Flags byte. `0` is the usual value; `128` marks the property critical.",
					Kind:            KindInt64,
					Int64Default:    int64Ptr(0),
					Int64Validators: []validator.Int64{int64validator.Between(0, 255)},
				},
				{
					Name:        "tag",
					Description: "The property being set: `issue`, `issuewild` or `iodef`.",
					Kind:        KindString,
					Required:    true,
					StringValidators: []validator.String{
						stringvalidator.OneOf("issue", "issuewild", "iodef"),
					},
				},
			},
		},
		{
			TypeName:               "dns_tlsa_record",
			RecordType:             zoneapi.RecordTypeTLSA,
			Description:            "A `TLSA` record, binding a certificate to a name for DANE.",
			DestinationDescription: "The certificate association data, as a hex string.",
			Extras: []Extra{
				{
					Name:            "certificate_usage",
					Description:     "Certificate usage: `0` PKIX-TA, `1` PKIX-EE, `2` DANE-TA, `3` DANE-EE.",
					Kind:            KindInt64,
					Required:        true,
					Int64Validators: []validator.Int64{int64validator.Between(0, 3)},
				},
				{
					Name:            "selector",
					Description:     "Which part of the certificate is matched: `0` full certificate, `1` public key.",
					Kind:            KindInt64,
					Required:        true,
					Int64Validators: []validator.Int64{int64validator.Between(0, 1)},
				},
				{
					Name:            "matching_type",
					Description:     "How the association data is presented: `0` exact, `1` SHA-256, `2` SHA-512.",
					Kind:            KindInt64,
					Required:        true,
					Int64Validators: []validator.Int64{int64validator.Between(0, 2)},
				},
			},
		},
		{
			TypeName:               "dns_sshfp_record",
			RecordType:             zoneapi.RecordTypeSSHFP,
			Description:            "An `SSHFP` record, publishing an SSH host key fingerprint.",
			DestinationDescription: "The host key fingerprint, as a hex string.",
			Extras: []Extra{
				{
					Name:            "algorithm",
					Description:     "Host key algorithm: `1` RSA, `2` DSA, `3` ECDSA, `4` Ed25519.",
					Kind:            KindInt64,
					Required:        true,
					Int64Validators: []validator.Int64{int64validator.Between(1, 4)},
				},
				{
					Name: "fingerprint_type",
					// The API calls this "type", which would read as the record
					// type on a resource already named after one.
					APIField:        "type",
					Description:     "Fingerprint hash: `1` SHA-1, `2` SHA-256.",
					Kind:            KindInt64,
					Required:        true,
					Int64Validators: []validator.Int64{int64validator.Between(1, 2)},
				},
			},
		},
		{
			TypeName:   "dns_url_record",
			RecordType: zoneapi.RecordTypeURL,
			Description: "A `URL` redirect. This is a zone.eu feature rather than a DNS record type: " +
				"zone.eu serves an HTTP redirect for the name. The name cannot collide with an " +
				"`A`, `AAAA` or `CNAME` record.",
			DestinationDescription: "The URL visitors are redirected to.",
			Extras: []Extra{
				{
					Name: "redirect_code",
					// Also "type" in the API, for the same reason as SSHFP.
					APIField:        "type",
					Description:     "HTTP status used for the redirect: `301` permanent or `302` temporary.",
					Kind:            KindInt64,
					Int64Default:    int64Ptr(301),
					Int64Validators: []validator.Int64{int64validator.OneOf(301, 302)},
				},
			},
		},
	}
}
