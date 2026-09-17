# The API has no endpoint for modifying a database, so every argument here
# forces replacement -- and replacing a database drops it. prevent_destroy is
# not paranoia on this resource.
resource "zone_mysql_database" "app" {
  service_name = "virt1.example.com"
  name         = "d10001_app"
  comment      = "Production application data"

  # Chosen at creation only, and never returned by the API.
  collation = "utf8mb4_unicode_ci"

  lifecycle {
    prevent_destroy = true
  }
}
