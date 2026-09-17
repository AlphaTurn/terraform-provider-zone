# Every record in the zone. Costs one API request per record type.
data "zone_dns_records" "all" {
  zone = "example.com"
}

# Just the A records, for one request.
data "zone_dns_records" "addresses" {
  zone = "example.com"
  type = "a"
}

# Records zone.eu manages itself and will not let you change.
output "locked_records" {
  value = [
    for record in data.zone_dns_records.all.records :
    "${record.type} ${record.name}" if !record.modifiable
  ]
}
