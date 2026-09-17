# The webhosting services on the account. Every zone_mail_*, zone_mysql_*,
# zone_ssl_*, zone_ssh_*, zone_ftp_* and zone_crontab resource is scoped by one
# of these names, so this is how to find out what they are.
data "zone_vservers" "all" {}

output "service_names" {
  value = [for service in data.zone_vservers.all.services : service.name]
}

# A service name often looks like a domain, but it is a different namespace
# from a DNS zone and the two need not match.
output "mysql_hosts" {
  value = {
    for service in data.zone_vservers.all.services : service.name => service.mysql_host
  }
}
