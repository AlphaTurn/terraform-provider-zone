resource "zone_dns_caa_record" "letsencrypt" {
  zone        = "example.com"
  name        = "example.com"
  destination = "letsencrypt.org"
  tag         = "issue"
}

resource "zone_dns_caa_record" "report" {
  zone        = "example.com"
  name        = "example.com"
  destination = "mailto:security@example.com"
  tag         = "iodef"
}
