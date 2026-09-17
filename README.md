# Terraform provider for zone.eu

Manage [zone.eu](https://www.zone.eu) DNS as code, through the ZoneID API v2.

zone.eu is one of the larger Estonian hosting and domain registrars, and its API
is good, but until now there was no Terraform or OpenTofu provider for it — DNS
changes meant clicking through the ZoneID panel or writing `curl` scripts. This
provider covers the DNS surface: all eleven record types, plus zone settings.

> Community project. Not affiliated with, endorsed by, or supported by zone.eu.

```hcl
terraform {
  required_providers {
    zone = {
      source  = "AlphaTurn/zone"
      version = "~> 0.1"
    }
  }
}

provider "zone" {} # reads ZONE_USERNAME and ZONE_API_TOKEN

resource "zone_dns_a_record" "www" {
  zone        = "example.com"
  name        = "www.example.com"
  destination = "203.0.113.10"
}

resource "zone_dns_mx_record" "mail" {
  zone        = "example.com"
  name        = "example.com"
  destination = "zonemx.eu"
  priority    = 10
}
```

## Authentication

The API uses HTTP basic auth: your **ZoneID username** and an **API token**
generated under ZoneID account management. Keep the token in the environment
rather than in configuration — anything written into a `provider` block ends up
in version control and in state.

```sh
export ZONE_USERNAME="your-zoneid-username"   # not your email address
export ZONE_API_TOKEN="..."
```

## What it manages

| Resource | |
|---|---|
| `zone_dns_a_record` · `zone_dns_aaaa_record` | addresses |
| `zone_dns_cname_record` · `zone_dns_ns_record` · `zone_dns_txt_record` | aliases, delegation, text |
| `zone_dns_mx_record` · `zone_dns_srv_record` | mail and service records |
| `zone_dns_caa_record` · `zone_dns_tlsa_record` · `zone_dns_sshfp_record` | certificate and key pinning |
| `zone_dns_url_record` | zone.eu's HTTP redirect record |
| `zone_dns_zone` | settings of an existing zone |

Domains:

| Resource | |
|---|---|
| `zone_domain` | renewal and signing settings of a registered domain |
| `zone_domain_nameservers` | a domain's whole nameserver delegation |
| `zone_domain_contact` | a registry contact attached to a domain |

Webhosting, all scoped by a `service_name` from the `zone_vservers` data
source:

| Resource | |
|---|---|
| `zone_mail_account` · `zone_mail_forwarder` | mailboxes and forwarding addresses |
| `zone_mail_autoreply` | vacation messages, on either of the above |
| `zone_mail_dkim` | DKIM signing for outgoing mail |
| `zone_mysql_database` · `zone_mysql_account` · `zone_mysql_permission` | databases, users and grants |
| `zone_ssl_certificate` | a TLS certificate and its key |

Data sources: `zone_dns_zone`, `zone_dns_records`, `zone_domains` and
`zone_vservers`.

Full reference, including every argument, lives in [`docs/`](docs/) and on the
Terraform Registry.

## Four things that will surprise you

These are properties of the zone.eu API, not choices this provider made. They
are worth knowing before you write your first config.

**There is no TTL.** No record type exposes one, so no resource has a `ttl`
argument. zone.eu manages TTLs itself. This is the first thing everyone arriving
from Route 53 or Cloudflare looks for.

**Record names are fully qualified.** zone.eu stores `www.example.com`, not
`www`. A bare label is rejected during validation, with a message telling you
what to write instead. Use the zone name itself for a record at the apex.

**Zones cannot be created or destroyed.** A zone appears when you buy a domain
or hosting service and goes away with it; the API has no endpoint for either.
`zone_dns_zone` adopts a zone that already exists and manages its settings.
Destroying it stops managing those settings and leaves the zone serving.

**Some records are zone.eu's, not yours.** Delegation `NS` records and certain
mail records come back with `modify` and `delete` denied. Those surface as the
read-only `modifiable` and `deletable` attributes, and trying to change one
produces an explanation rather than a bare HTTP 400. Use `terraform state rm` to
stop managing such a record without trying to remove it.

## Secrets, and what is not in your state file

Passwords and private keys are **write-only arguments**: they are sent to
zone.eu and never written to state. State is not an encrypted store, and a
secret in it is a secret in every backup and CI artefact the file passes
through. This needs Terraform 1.11 or newer; everything else in the provider
works from 1.8.

The trade is that Terraform cannot detect a change to a value it does not keep.
For a certificate that does not matter, because a new key arrives with a new
`certificate`, which *is* in state. For a password there is nothing else to
notice, so each of those resources has a `password_version` you bump to say
"send it again".

## The rate limit, and what the provider does about it

zone.eu allows **60 requests per minute per IP**. That is low enough to matter:
Terraform refreshes every resource in state on every plan, at a default
parallelism of 10, so a naive provider would spend a medium zone's plan being
throttled.

Two things keep it inside the budget:

- **Listings are shared.** The record endpoints are unpaginated — one `GET`
  returns every A record in the zone — so reads are answered from a briefly
  cached listing per zone and record type. Refreshing 100 records costs one
  request, not 100. Concurrent reads of the same listing collapse into a single
  fetch rather than racing.
- **Requests are paced.** A client-side limiter holds a sustained 55 requests a
  minute, leaving headroom for retries and for anything else sharing your IP.
  The provider also watches `X-Ratelimit-Remaining` and slows down further when
  the budget runs low, because that budget is shared with every other process on
  the same address. A 429 is retried, honouring `Retry-After`.

Both are tunable via `rate_limit` and `max_retries` on the provider, though
raising `rate_limit` above 60 causes throttling rather than speed.

## Importing existing records

Records import as `<zone>/<record id>`; the id is visible in the `resource_url`
of the `zone_dns_records` data source.

```sh
terraform import zone_dns_a_record.www "example.com/835013"
terraform import zone_dns_zone.example "example.com"   # zones import by name
```

## Development

Needs Go 1.25+ and a `terraform` binary on PATH.

```sh
make build   # compile the provider
make test    # unit tests + the acceptance suite
make docs    # regenerate docs/ from the schema and examples
make lint
```

`make test` runs the full resource lifecycle — create, update, import, drift,
destroy, error paths — against an in-process stub of the zone.eu API. It needs
no credentials and touches no real DNS, so it runs on every push.

To try the provider against your own zone before it is published, point
Terraform at a local build:

```hcl
# ~/.terraformrc
provider_installation {
  dev_overrides { "AlphaTurn/zone" = "/path/to/terraform-provider-zone" }
  direct {}
}
```

`api/zone-openapi.json` is the API description extracted from zone.eu's ReDoc
documentation page, checked in so the mapping between resources and endpoints
can be audited. Where the live API and that document disagree — and they do, on
scalar types — the provider follows the live API and tolerates both.

## What is not covered yet

v0.1 is DNS only: 23 of the API's 93 endpoints. Domain management, webhosting
(mail, MySQL, SSL, FTP, cron), and cloud servers are not implemented.
[`ROADMAP.md`](ROADMAP.md) breaks down the remaining 70 endpoints, the order
they are worth adding in, and the groundwork each needs — pagination being the
main one, since DNS is the only area that does not paginate.

Contributions welcome.

## Licence

[MPL-2.0](LICENSE).
