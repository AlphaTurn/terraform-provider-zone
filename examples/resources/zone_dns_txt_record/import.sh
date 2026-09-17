# Import an existing record with "<zone>/<record id>".
# The record id appears in the resource_url of the zone_dns_records data source.
terraform import zone_dns_txt_record.example "example.com/12345"
