# A grant is its own resource because that is what the API makes it: revoking
# one drops neither the account nor the database.
resource "zone_mysql_permission" "app" {
  service_name = "virt1.example.com"
  username     = zone_mysql_account.app.username
  database     = zone_mysql_database.app.name

  permissions = [
    "SELECT",
    "INSERT",
    "UPDATE",
    "DELETE",
  ]
}
