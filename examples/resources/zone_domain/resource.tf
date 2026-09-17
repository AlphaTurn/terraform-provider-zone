# Adopts a domain that is already registered and manages its settings. This
# resource cannot register, transfer or release a domain: the API has no
# endpoint for any of that.
resource "zone_domain" "example" {
  name = "example.com"

  # Renewal reminders by email.
  renewal_notifications = true
}
