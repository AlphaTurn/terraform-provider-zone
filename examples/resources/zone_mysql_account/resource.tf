# A database user. It can reach no database until a zone_mysql_permission
# grants it access.
resource "zone_mysql_account" "app" {
  service_name = "virt1.example.com"
  username     = "d10001_app"

  password         = var.database_password
  password_version = 1

  require_ssl = true

  # As well as IP addresses, zone.eu accepts three special values: ws for the
  # service's own web servers, pma for phpMyAdmin and vpn for zone.eu's VPN.
  hosts = ["ws", "pma"]

  comment = "Production application"
}

variable "database_password" {
  type      = string
  sensitive = true
}
