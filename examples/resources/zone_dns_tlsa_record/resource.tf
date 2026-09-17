resource "zone_dns_tlsa_record" "mail" {
  zone              = "example.com"
  name              = "_25._tcp.mail.example.com"
  destination       = "20de66d3b3bddb54e6c20bd9f288db3fc080596096cb9066999ff610575e6fa5"
  certificate_usage = 3 # DANE-EE
  selector          = 1 # public key
  matching_type     = 1 # SHA-256
}
