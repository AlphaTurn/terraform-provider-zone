# A system job. zone.eu substitutes [[$PHP]] and friends, which is how to
# invoke the right interpreter without hardcoding a path that will change.
resource "zone_crontab" "nightly_import" {
  service_name = "virt1.example.com"
  name         = "Nightly import"

  exec_type = "system"
  command   = "[[$PHP]] [[$ACC_HOMEDIR_A]]/bin/import.php"
  schedule  = "15 3 * * *"

  report       = "onerror"
  report_email = "ops@example.com"

  # Only accepted on a system job.
  timezone      = "Europe/Tallinn"
  runtime_limit = 3600
  nice          = "low"
}

# An HTTP job just fetches a URL, and must not set timezone or runtime_limit.
resource "zone_crontab" "warm_cache" {
  service_name = "virt1.example.com"
  name         = "Warm the cache"

  exec_type = "http"
  command   = "https://www.example.com/cron/warm"
  schedule  = "*/15 * * * *"

  report = "never"
}
