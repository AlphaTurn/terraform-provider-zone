# An autoreply on a mailbox. target says which endpoint to use, because the
# same object hangs off both mailboxes and forwarders.
resource "zone_mail_autoreply" "holidays" {
  service_name = "virt1.example.com"
  target       = "account"
  address      = zone_mail_account.info.address

  subject = "Out of office"
  body    = <<-EOT
    Thank you for your message. Our office is closed until 2 January.

    For anything urgent, please write to urgent@example.com.
  EOT

  date_start = "2026-12-24"
  date_end   = "2027-01-02"
}

# The same resource, on a forwarder rather than a mailbox.
resource "zone_mail_autoreply" "sales_closed" {
  service_name = "virt1.example.com"
  target       = "forwarder"
  address      = zone_mail_forwarder.sales.address

  body    = "Sales enquiries are handled from 2 January."
  enabled = false
}
