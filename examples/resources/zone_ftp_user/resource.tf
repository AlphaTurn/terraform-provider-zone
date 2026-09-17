# An FTP account confined to one directory. access_profile = "unsafe" would
# permit plain FTP, which sends the password in clear text.
resource "zone_ftp_user" "deploy" {
  service_name = "virt1.example.com"
  username     = "deploy"

  password         = var.ftp_password
  password_version = 1

  directory      = "/data01/virt10001/domeenid/www.example.com"
  require_tls    = true
  access_profile = "whitelist_or_tls"

  allowed_operations = [
    "allow_list",
    "allow_chdir",
    "allow_download",
    "allow_upload",
    "allow_upload_overwrite",
  ]

  access_countries = ["EE", "FI"]
}

variable "ftp_password" {
  type      = string
  sensitive = true
}
