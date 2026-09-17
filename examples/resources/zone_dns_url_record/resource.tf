resource "zone_dns_url_record" "shortlink" {
  zone        = "example.com"
  name        = "go.example.com"
  destination = "https://www.example.com/landing"

  # 301 when omitted.
  redirect_code = 302
}
