resource "zone_dns_srv_record" "autodiscover" {
  zone        = "example.com"
  name        = "_autodiscover._tcp.example.com"
  destination = "mailconfig.zone.eu"
  priority    = 0
  weight      = 0
  port        = 443
}
