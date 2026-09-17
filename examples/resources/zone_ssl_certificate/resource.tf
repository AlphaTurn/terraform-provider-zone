# private_key is a write-only argument: it is sent to zone.eu and never written
# to state. That needs Terraform 1.11 or newer.
resource "zone_ssl_certificate" "example" {
  service_name = "virt1.example.com"
  name         = "example.com 2026"

  certificate    = file("${path.module}/certs/example.com.crt")
  ca_certificate = file("${path.module}/certs/example.com.chain.crt")
  private_key    = file("${path.module}/certs/example.com.key")

  hosts = ["example.com", "www.example.com"]
}

output "expires" {
  value = zone_ssl_certificate.example.expires
}
