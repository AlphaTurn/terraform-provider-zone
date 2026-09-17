# The API cannot modify an authorised key, so changing either argument here
# destroys and recreates it.
resource "zone_ssh_public_key" "deploy" {
  service_name = "virt1.example.com"
  public_key   = file("~/.ssh/id_ed25519.pub")
  comment      = "Deploy key"
}
