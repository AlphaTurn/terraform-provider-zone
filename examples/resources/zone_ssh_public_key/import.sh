# Either the numeric id or the fingerprint works, and the fingerprint is the
# one you actually have -- from ssh-keygen -lf ~/.ssh/id_ed25519.pub.
terraform import zone_ssh_public_key.deploy "virt1.example.com/SHA256:abc123..."
terraform import zone_ssh_public_key.deploy "virt1.example.com/59386"
