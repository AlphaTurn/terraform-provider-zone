resource "zone_dns_mx_record" "primary" {
  zone        = "example.com"
  name        = "example.com"
  destination = "zonemx.eu"
  priority    = 10
}
