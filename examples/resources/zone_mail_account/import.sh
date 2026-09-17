# Mailboxes are imported as "<service name>/<address>". The password is not
# imported, because no state ever holds one; the next apply sends whatever the
# configuration supplies.
terraform import zone_mail_account.info "virt1.example.com/info@example.com"
