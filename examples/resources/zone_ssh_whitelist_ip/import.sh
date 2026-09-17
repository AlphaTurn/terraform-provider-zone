# Either the numeric id or the address works. The address may carry a prefix
# length, and the import ID is split on its first slash only.
terraform import zone_ssh_whitelist_ip.office "virt1.example.com/217.128.0.0/24"
terraform import zone_ssh_whitelist_ip.office "virt1.example.com/81489"
