# Certificates are imported as "<service name>/<certificate id>". The private
# key is not imported, because no state ever holds one; the next apply sends
# whatever the configuration supplies.
terraform import zone_ssl_certificate.example "virt1.example.com/163957"
