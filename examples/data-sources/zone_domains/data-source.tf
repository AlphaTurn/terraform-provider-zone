# Every domain on the account.
data "zone_domains" "all" {}

# Filtering happens on zone.eu's side, so a filter costs fewer requests on a
# large account rather than more. Note that name_contains is a substring match.
data "zone_domains" "expiring" {
  needs_renewal = true
}

output "expiring_soon" {
  value = [for domain in data.zone_domains.expiring.domains : domain.name]
}
