# A mailbox. The password is write-only: it is sent to zone.eu and never
# written to state, so Terraform cannot detect a change made in webmail.
resource "zone_mail_account" "info" {
  service_name = "virt1.example.com"
  address      = "info@example.com"

  password         = var.mailbox_password
  password_version = 1

  comment   = "Reception"
  spamlevel = "medium"
}

# Rotating the password means changing password_version as well, because
# Terraform cannot see a change to a value it does not keep.
variable "mailbox_password" {
  type      = string
  sensitive = true
}
