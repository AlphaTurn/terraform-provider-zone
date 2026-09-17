resource "zone_dns_a_record" "www" {
  zone        = "example.com"
  name        = "www.example.com"
  destination = "203.0.113.10"
}

# A record at the zone apex uses the zone name itself.
resource "zone_dns_a_record" "apex" {
  zone        = "example.com"
  name        = "example.com"
  destination = "203.0.113.10"
}
