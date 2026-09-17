# A technical contact. Registries normalise contact details, so anything left
# unset takes the value the registry already holds.
resource "zone_domain_contact" "tech" {
  domain = "example.com"
  role   = "tech"

  name    = "Example Operations"
  email   = "noc@example.com"
  voice   = "+372.5551234"
  country = "EE"
}

# An .ee registrant needs an identity code, which is what the ext_ident fields
# are for. Which of them a registry wants depends on the TLD.
resource "zone_domain_contact" "registrant" {
  domain = "example.com"
  role   = "registrant"

  name         = "Example OU"
  organization = "Example OU"
  email        = "owner@example.com"
  voice        = "+372.5551234"
  country      = "EE"
  city         = "Tallinn"
  street       = "Narva mnt 1"
  postalcode   = "10117"

  ext_ident      = "12345678"
  ext_ident_type = "company_number"
  ext_ident_cc   = "EE"
}
