# Takes effect for the FTP accounts whose access_profile requires a whitelist.
resource "zone_ftp_ip_whitelist" "office" {
  service_name = "virt1.example.com"
  ip           = "203.0.113.4"
}
