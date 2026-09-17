# Adopts the zone that already exists for a domain or hosting service. This
# resource never creates or destroys zones; the API cannot.
resource "zone_dns_zone" "example" {
  name = "example.com"
  ipv6 = true
}
