resource "zone_dns_sshfp_record" "gateway" {
  zone             = "example.com"
  name             = "gateway.example.com"
  destination      = "5f2f2e0676798a0273572bc77b99d6319a560fd5"
  algorithm        = 4 # Ed25519
  fingerprint_type = 2 # SHA-256
}
