# Turning on DKIM signing generates a key. Nothing about it is configurable:
# the API's create endpoint takes no body at all.
resource "zone_mail_dkim" "example" {
  service_name = "virt1.example.com"
}

# The signature is only verifiable once the public key is published in DNS,
# which this provider can do as well.
resource "zone_dns_txt_record" "dkim" {
  zone        = "example.com"
  name        = "zonemta._domainkey.example.com"
  destination = zone_mail_dkim.example.public_key
}
