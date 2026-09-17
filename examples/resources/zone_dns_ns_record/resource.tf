resource "zone_dns_ns_record" "delegated" {
  zone        = "example.com"
  name        = "internal.example.com"
  destination = "ns1.example.net"
}
