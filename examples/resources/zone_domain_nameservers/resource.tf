# Delegation is managed as one whole set, because the API replaces it in a
# single operation and registries require at least two nameservers.
resource "zone_domain_nameservers" "example" {
  domain = "example.com"

  nameserver = [
    { hostname = "ns.zone.eu" },
    { hostname = "ns2.zone.eu" },
    { hostname = "ns3.zone.eu" },
  ]
}

# Glue addresses are only needed for a nameserver inside the domain it serves.
resource "zone_domain_nameservers" "self_hosted" {
  domain = "example.net"

  nameserver = [
    {
      hostname = "ns1.example.net"
      ip       = ["203.0.113.10", "2001:db8::10"]
    },
    {
      hostname = "ns2.example.net"
      ip       = ["203.0.113.11"]
    },
  ]
}
