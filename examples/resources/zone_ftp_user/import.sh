# Either the numeric id or the username works. The password is not imported,
# because no state ever holds one.
terraform import zone_ftp_user.deploy "virt1.example.com/deploy"
terraform import zone_ftp_user.deploy "virt1.example.com/155594"
