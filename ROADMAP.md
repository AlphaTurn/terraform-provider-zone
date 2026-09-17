# Roadmap

v0.1.0 covers DNS: 23 of the ZoneID API's 93 endpoints. This is what the other
70 are, in the order they are worth doing, and what each one costs.

Endpoint inventory is derived from [`api/zone-openapi.json`](api/zone-openapi.json),
which is the spec extracted from zone.eu's ReDoc page and checked in for exactly
this purpose. Where that document and the live API disagree — and they do — the
live API wins; see "Trust the wire, not the spec" below.

## Shipped

| Area | Endpoints | Status |
|---|---|---|
| `/dns` | 23 | v0.1.0 — 11 record types, zone settings, 2 data sources |

## Proposed order

### v0.2 — `/domain` (8 endpoints)

The natural next step: same mental model as DNS, no secrets, and the one area
that pairs directly with what already exists — you delegate a domain's
nameservers and manage its zone in one configuration.

| Endpoint | Proposed |
|---|---|
| `GET/PUT /domain/{name}` | `zone_domain` resource (settings only) |
| `GET /domain` | `zone_domains` data source |
| `GET/POST /domain/{name}/nameserver`, `GET/PUT/DELETE .../{hostname}` | `zone_domain_nameserver` resource |
| `GET/POST /domain/{name}/contact`, `GET/PUT/DELETE .../{id}` | `zone_domain_contact` resource |
| `POST /domain/{name}/contact/tech/default` | fold into the contact resource |
| `GET/PUT /domain/{name}/preferences` | fold into `zone_domain` |

Watch for: almost every field on the `Domain` schema is `readOnly` — only
`renewal_notifications`, `signing_required` and `nameservers_custom` are
writable, and `nameservers_custom` documents "only false allowed". So
`zone_domain` is a thin settings resource, not a domain registration resource.
Like `zone_dns_zone`, it adopts rather than creates.

`GET /domain` paginates and offers filters (`name`, `renewable`, `delegated`,
`needs_renewal`) that map nicely onto data source arguments.

### v0.3 — webhosting essentials: mail, MySQL, SSL (~20 endpoints)

The highest practical value in `/vserver`, and the first area that handles
secrets.

| Endpoint group | Proposed |
|---|---|
| `mail/account` (+ `autoreply`, `premium`, `asp`) | `zone_mail_account`, `zone_mail_autoreply` |
| `mail/forwarder` (+ `autoreply`) | `zone_mail_forwarder` |
| `mail/dkim` | `zone_mail_dkim` (GET/POST/DELETE, no update) |
| `database/mysql` + `/{database_name}` | `zone_mysql_database` (no PUT — create/delete only) |
| `database/mysql/account` + `/{username}` | `zone_mysql_account` |
| `.../permission/{database_name}` | `zone_mysql_permission` |
| `ssl` + `/{id}` | `zone_ssl_certificate` |

Watch for:

- **Passwords are `writeOnly`** on `MailAccount`, `MySQLAccount` and `FtpUser` —
  sent on create, never returned. Drift on them is undetectable by definition.
  Use Terraform's write-only attributes rather than storing them in state, and
  document that rotating a password requires changing the configuration.
- **`VServerSSLCerificate.private_key` is `required` and *not* `writeOnly`**, so
  a naive implementation puts private keys in plaintext state. This one needs a
  deliberate decision before any code is written.
- `zone_mysql_database` has no `PUT`: changing anything but the comment means
  replacement. `collation` is documented as create-only.
- `MailAccount.two_factor_auth` "can only be set to false from API" — a one-way
  attribute, which Terraform models badly. Probably expose read-only.

### v0.4 — access and scheduling (~11 endpoints)

| Endpoint group | Proposed |
|---|---|
| `ssh` | `zone_ssh_settings` |
| `ssh/publickey` | `zone_ssh_public_key` (no update — create/delete) |
| `ssh/whitelist` | `zone_ssh_whitelist_ip` (no update) |
| `ftp/user` | `zone_ftp_user` |
| `ftp/ipwhitelist` | `zone_ftp_ip_whitelist` (no update) |
| `crontab` | `zone_crontab` |

`VServerCrontab` is the best-specified schema in the whole API — real enums for
`type`, `report`, `priority`, `timezone` and `runtime_limit` — so it maps to a
well-validated resource with little guesswork. Note `timezone` and
`runtime_limit` are only valid when `type == "system"`, which is a cross-field
rule for `ValidateConfig`.

### v0.5 — runtimes and networking (~17 endpoints)

PM2, Redis, port forwarding, dedicated IPs, ZoneCloud, turbo, billing resources.

Watch for: this group is where the API stops being Terraform-shaped.
`start`, `stop`, `restart` and `regenerateauth` are **actions, not state** —
they have no place in a resource's CRUD. Either leave them out, or expose them
as Terraform actions. Redis has no `DELETE` and no `PUT` at all, so
`zone_redis` can only be created and read. `portforward/{id}` takes `POST`
rather than `PUT` for updates, which is unusual enough to verify on the wire
first.

### Deliberately out of scope: `/order` and `/cloud` (9 endpoints)

`POST /order/cloud` provisions a billable VPS. `POST /order/domain/renew` spends
money. `DELETE /cloud/{name}` destroys a server.

A `terraform apply` that quietly bills the account is a bad failure mode, and
`terraform destroy` deleting a VPS because a resource was renamed is worse. If
these are ever added they need a deliberate design — an explicit opt-in
argument, or read-only data sources over the ordering endpoints so at least the
state is visible in Terraform without being mutable from it.

Recommendation: leave ordering out and model `/cloud` as data sources only,
unless there is a concrete need.

## Cross-cutting work these depend on

**Pagination.** DNS declares no pager parameters — one `GET` returns every
record of a type, which is why the listing cache works as it does. That does
not generalise. Twelve non-DNS endpoints paginate, defaulting to **10 items per
page** with a maximum of 100, via the `x-pager-page` / `x-pager-limit` request
headers and `x-pager-*` response headers:

```
/domain                                   /vserver/{s}/mail/account
/cloud/{service_name}                     /vserver/{s}/mail/forwarder
/order/domain/                            /vserver/{s}/ftp/user
/vserver/{s}/database/mysql               /vserver/{s}/ftp/ipwhitelist
/vserver/{s}/database/mysql/account       /vserver/{s}/portforward
/vserver/{s}/zonecloud                    /vserver/{s}/turbo/{id}
```

A client that ignores this silently sees only the first 10 mail accounts. So
`internal/zoneapi` needs page-following before v0.3, and `listCache` needs to
cache the assembled result rather than one response. Sorting is available on a
few endpoints via `x-order-by` / `x-order-dir`.

**The rate limit gets tighter, not looser.** 60 requests per minute per IP is
the whole budget across every resource type. Paginated listings cost several
requests each. The per-`(zone, type)` listing cache that makes DNS cheap needs
an equivalent for each new area, keyed by whatever the natural collection is —
otherwise adding webhosting resources will make DNS plans start failing.

**`service_name` is not a DNS zone.** For `/vserver` it is the virtual server
name and for `/cloud` a VPS hostname like `uvn-XX-XXX.tll01.zonevs.eu`. They
often look like domains but are a different namespace, so the FQDN validation
used on DNS record names must not be copied over.

**Trust the wire, not the spec.** Confirmed discrepancies in the published
document, all handled by the coercion helpers in `internal/zoneapi/flex.go`:
record `id` arrives as a quoted string on most DNS endpoints but as a bare
integer on SRV; `X-Status-Message` corrupts non-ASCII Estonian text while the
undocumented `X-Status-Message-Base64` does not; and the zone object returns
`dnssec` and `domain` fields that the spec never mentions. Expect more of this
in unexplored areas — verify every schema against a real response before
trusting it.

**Actions are not resources.** `start`, `stop`, `restart`, `regenerateauth`,
`/cloud/{name}/action/{action}` and `POST /vserver/{name}/turbo` all perform an
operation rather than describing desired state. Terraform has no natural place
for them in CRUD, and forcing them in produces resources that "drift" every
plan.

## Known gap in what has shipped

`zone_dns_zone`'s update path has never been exercised against the live API:
testing it would have toggled IPv6 on a production zone, and the spec declares
no request body for `PUT /dns/{zone}`, so the payload shape is inferred. Verify
it on a disposable zone before relying on it.
