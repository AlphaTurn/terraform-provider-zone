# Restricting SSH to a whitelist, with the whitelist alongside it. Applying
# access = "whitelist" without any entries would lock you out, and Terraform
# cannot express that constraint across resources -- so keep them together.
resource "zone_ssh_settings" "example" {
  service_name = "virt1.example.com"
  access       = "whitelist"
}

resource "zone_ssh_whitelist_ip" "office" {
  service_name = zone_ssh_settings.example.service_name
  ip           = "217.128.0.0/24"
  comment      = "Office"
}

# The host key fingerprints are keyed by algorithm.
output "host_key" {
  value = zone_ssh_settings.example.server_fingerprints["ED25519"]
}
