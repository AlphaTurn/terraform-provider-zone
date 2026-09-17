# Grants are imported as "<service name>/<username>/<database>". A grant that
# already exists must be imported rather than created: the endpoint has no
# POST, so creating one would overwrite whatever privileges were already there.
terraform import zone_mysql_permission.app "virt1.example.com/d10001_app/d10001_app"
