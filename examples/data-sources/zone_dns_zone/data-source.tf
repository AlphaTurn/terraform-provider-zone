data "zone_dns_zone" "example" {
  name = "example.com"
}

output "zone_is_served" {
  value = data.zone_dns_zone.example.active
}
