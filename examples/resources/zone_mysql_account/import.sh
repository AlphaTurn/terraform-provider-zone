# Accounts are imported as "<service name>/<username>". The password is not
# imported, because no state ever holds one.
terraform import zone_mysql_account.app "virt1.example.com/d10001_app"
