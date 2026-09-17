resource "zone_dns_aaaa_record" "www" {
  zone        = "example.com"
  name        = "www.example.com"
  destination = "2001:db8::1"
}
