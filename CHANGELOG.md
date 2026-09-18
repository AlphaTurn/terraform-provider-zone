# Changelog

## 0.2.0 (2026-09-18)

FEATURES:

* **New Resource:** `zone_domain`, for the settings of a registered domain
* **New Resource:** `zone_domain_nameservers`, for a domain's whole delegation
* **New Resource:** `zone_domain_contact`
* **New Data Source:** `zone_domains`
* **New Data Source:** `zone_vservers`, listing the webhosting services on the
  account and the `service_name` values the webhosting resources take
* **New Resource:** `zone_mail_account`
* **New Resource:** `zone_mail_forwarder`
* **New Resource:** `zone_mail_autoreply`, for either a mailbox or a forwarder
* **New Resource:** `zone_mail_dkim`
* **New Resource:** `zone_mysql_database`
* **New Resource:** `zone_mysql_account`
* **New Resource:** `zone_mysql_permission`
* **New Resource:** `zone_ssl_certificate`
* **New Resource:** `zone_ssh_settings`
* **New Resource:** `zone_ssh_public_key`
* **New Resource:** `zone_ssh_whitelist_ip`
* **New Resource:** `zone_ftp_user`
* **New Resource:** `zone_ftp_ip_whitelist`
* **New Resource:** `zone_crontab`

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
* Passwords and private keys are write-only arguments, which need Terraform
  1.11 or newer. They are sent to zone.eu and never written to state. Terraform
  cannot detect a change to a value it does not keep, so the mail, database and
  FTP account resources carry a `password_version` to force a re-send; a
  certificate needs none, because its key always changes alongside a
  `certificate` that is in state.
* `zone_mysql_database` cannot be modified at all: the API has no endpoint for
  it, so every argument forces replacement, and replacing a database drops it.
  The examples set `prevent_destroy` for that reason.
* `zone_mysql_permission` refuses to adopt a grant that already exists. Its
  endpoint has no `POST`, so a create could not tell granting from silently
  replacing privileges nobody asked to change; import the grant instead.
* `zone_mail_account.two_factor_auth` can only be set to `false`, which is all
  the API allows — the mailbox's owner turns it on in webmail. Asking for `true`
  is refused during validation rather than by a 422 mid-apply.
* An archived mailbox reads as missing rather than being adopted as a
  tombstone Terraform could never reconcile.
* `zone_mail_autoreply` is a separate resource because the same object hangs off
  both mailboxes and forwarders. Destroying one disables it, which is as close
  to deletion as its endpoint allows.
* `zone_crontab`'s argument names come from the live API's `OPTIONS` response,
  not from zone.eu's published description, which disagrees with it: `OPTIONS`
  reports `exec_type`, `nice` and `schedule_type` where the document says
  `type` and `priority` and omits the third entirely. No account available for
  testing had a crontab, so no read could settle it. Reads accept either
  spelling; writes use the `OPTIONS` names.
* `zone_ssh_public_key`, `zone_ssh_whitelist_ip` and `zone_ftp_ip_whitelist`
  have no update operation at all, so every argument forces replacement. They
  can be imported by their natural key — a fingerprint or an address — as well
  as by their numeric id, since that is what a person actually has.
* `zone_ftp_ip_whitelist`'s create payload is inferred. The published
  description marks every field of that object read-only while still requiring
  a request body, which cannot both be true, so the provider sends the address.
* Setting `zone_ssh_settings.access` to `whitelist` with no whitelist entries
  locks you out of the server. Terraform cannot express that across resources,
  so the documentation and examples pair them.
* The create, update and delete paths of the webhosting resources are exercised
  against the stub only. Proving them would have meant writing to a production
  hosting service; every read path is verified against the live API, including
  against services that hold real SSH keys, FTP users, databases and
  certificates.

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
