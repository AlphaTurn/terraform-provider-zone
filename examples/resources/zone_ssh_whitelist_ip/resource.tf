# A single address, or a network with a prefix length.
resource "zone_ssh_whitelist_ip" "office" {
  service_name = "virt1.example.com"
  ip           = "217.128.0.0/24"
  comment      = "Office"
}

resource "zone_ssh_whitelist_ip" "ci" {
  service_name = "virt1.example.com"
  ip           = "2001:db8::10"
  comment      = "CI runner"
}
