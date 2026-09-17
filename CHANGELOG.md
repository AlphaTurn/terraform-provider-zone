# Changelog

## Unreleased

FEATURES:

* **New Resource:** `zone_domain`, for the settings of a registered domain
* **New Resource:** `zone_domain_nameservers`, for a domain's whole delegation
* **New Resource:** `zone_domain_contact`
* **New Data Source:** `zone_domains`
* **New Data Source:** `zone_vservers`, listing the webhosting services on the
  account and the `service_name` values the webhosting resources take

ENHANCEMENTS:

* The client now follows the API's pager. DNS is the only area that does not
  paginate; everywhere else the default page is 10 items, so a client that
  ignored it would see only the first 10 mail accounts or domains. Listings ask
  for the maximum page size of 100, which makes a collection of up to 100 items
  cost one request rather than ten.
* `402 Payment Required` and `403 Forbidden` now produce their own explanations.
  A 402 means an operation needs a paid package upgrade rather than a change to
  the configuration; a 403 is what a service delegated to another ZoneID user
  returns on some of its endpoints, and is not treated as a missing resource.

NOTES:

* Domains cannot be registered, transferred or released through the API.
  `zone_domain` adopts a domain that already exists and manages its settings,
  and destroying it leaves the registration alone.
* `zone_domain.signing_required` is accepted by the API and never returned by
  it, so Terraform keeps whatever was last applied and cannot detect a change
  made in the ZoneID panel.
* `zone_domain_nameservers` manages the delegation as one set rather than one
  resource per nameserver, because the endpoint replaces the whole set and most
  registries refuse a delegation of fewer than two nameservers.
* The update path of `zone_domain` is unverified against the live API, for the
  same reason `zone_dns_zone`'s is: the published description declares no
  request body, and exercising it would have changed a production domain.

## 0.1.0 (2026-09-17)

First release. Covers the DNS surface of the zone.eu ZoneID API v2.

FEATURES:

* **New Provider:** `zone`, authenticating with a ZoneID username and API token
* **New Resource:** `zone_dns_a_record`
* **New Resource:** `zone_dns_aaaa_record`
* **New Resource:** `zone_dns_cname_record`
* **New Resource:** `zone_dns_ns_record`
* **New Resource:** `zone_dns_txt_record`
* **New Resource:** `zone_dns_mx_record`
* **New Resource:** `zone_dns_srv_record`
* **New Resource:** `zone_dns_caa_record`
* **New Resource:** `zone_dns_tlsa_record`
* **New Resource:** `zone_dns_sshfp_record`
* **New Resource:** `zone_dns_url_record`
* **New Resource:** `zone_dns_zone`, for the settings of an existing zone
* **New Data Source:** `zone_dns_zone`
* **New Data Source:** `zone_dns_records`

NOTES:

* Records have no `ttl` argument, because the API exposes no TTL field.
* Record names are fully qualified; a bare label is rejected during validation.
* Zones cannot be created or destroyed through the API. `zone_dns_zone` adopts
  an existing zone and manages its settings.
* Records that zone.eu manages itself report `modifiable` or `deletable` as
  false, and changing one reports why rather than surfacing a bare HTTP error.
* The provider paces itself against the API's limit of 60 requests per minute
  per IP, and answers record reads from a shared per-zone listing so that a
  refresh costs one request per record type rather than one per record.
