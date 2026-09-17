# A forwarder has no mailbox, no quota and no password: mail to this address is
# passed straight on to somebody else's inbox.
resource "zone_mail_forwarder" "sales" {
  service_name = "virt1.example.com"
  address      = "sales@example.com"

  forward_addresses = [
    "owner@example.com",
    "partner@example.net",
  ]

  comment = "Sales enquiries"
}
