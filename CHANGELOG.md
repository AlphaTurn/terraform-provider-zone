# Changelog

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
