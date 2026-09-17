resource "zone_dns_cname_record" "shop" {
  zone        = "example.com"
  name        = "shop.example.com"
  destination = "shops.example.net"
}
