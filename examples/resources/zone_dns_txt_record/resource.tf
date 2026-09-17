resource "zone_dns_txt_record" "spf" {
  zone        = "example.com"
  name        = "example.com"
  destination = "v=spf1 a mx include:_spf.zone.eu -all"
}

resource "zone_dns_txt_record" "dmarc" {
  zone        = "example.com"
  name        = "_dmarc.example.com"
  destination = "v=DMARC1; p=quarantine"
}
