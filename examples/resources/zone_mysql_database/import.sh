# Databases are imported as "<service name>/<database name>". The collation is
# not imported, because the API never reports it.
terraform import zone_mysql_database.app "virt1.example.com/d10001_app"
