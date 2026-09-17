package provider

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfversion"
)

// Test certificates. These are syntactically PEM and semantically nothing: the
// stub checks the shape and records what arrived, so no real key is needed and
// none should be committed.
const (
	testCertificate = `-----BEGIN CERTIFICATE-----
c3R1YiBjZXJ0aWZpY2F0ZSBib2R5LCBub3QgYSByZWFsIGNlcnRpZmljYXRl
-----END CERTIFICATE-----`

	testCACertificate = `-----BEGIN CERTIFICATE-----
c3R1YiBpc3N1ZXIgY2hhaW4sIG5vdCBhIHJlYWwgY2VydGlmaWNhdGU=
-----END CERTIFICATE-----`

	testPrivateKey = `-----BEGIN PRIVATE KEY-----
c3R1YiBwcml2YXRlIGtleSwgbm90IGEgcmVhbCBrZXk=
-----END PRIVATE KEY-----`

	testOtherPrivateKey = `-----BEGIN PRIVATE KEY-----
YSBzZWNvbmQgc3R1YiBwcml2YXRlIGtleSwgYWxzbyBub3QgcmVhbA==
-----END PRIVATE KEY-----`
)

// writeOnlyTest gates a test on the Terraform version that supports write-only
// attributes, so the 1.8 leg of the CI matrix skips rather than fails.
var writeOnlyTest = []tfversion.TerraformVersionCheck{
	tfversion.SkipBelow(tfversion.Version1_11_0),
}

// The point of a write-only attribute: the key reaches zone.eu and never
// reaches state.
func TestAccSSLCertificate_privateKeyIsSentButNotStored(t *testing.T) {
	stub, baseURL := startStub(t)

	config := func(key string) string {
		return providerConfig(baseURL) + fmt.Sprintf(`
resource "zone_ssl_certificate" "this" {
  service_name   = %q
  name           = "example.com wildcard"
  certificate    = %q
  ca_certificate = %q
  private_key    = %q
  hosts          = ["example.com"]
}
`, stubServiceName, testCertificate, testCACertificate, key)
	}

	resource.Test(t, resource.TestCase{
		TerraformVersionChecks:   writeOnlyTest,
		ProtoV6ProviderFactories: testProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config(testPrivateKey),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("zone_ssl_certificate.this", "id"),
					resource.TestCheckResourceAttr("zone_ssl_certificate.this", "name", "example.com wildcard"),
					resource.TestCheckResourceAttr("zone_ssl_certificate.this", "hosts.#", "1"),
					// The whole design in one assertion.
					resource.TestCheckNoResourceAttr("zone_ssl_certificate.this", "private_key"),
					func(*terraform.State) error {
						if got := stub.LastPrivateKey(); got != testPrivateKey {
							return fmt.Errorf("the API received a private key of %d bytes, want the configured one", len(got))
						}
						return nil
					},
				),
			},
			{
				// A write-only value must not make the plan permanently dirty.
				Config:   config(testPrivateKey),
				PlanOnly: true,
			},
		},
	})

	if got := stub.CountCertificates(stubServiceName); got != 0 {
		t.Errorf("%d certificates survived destroy, want 0", got)
	}
}

// Rotating the certificate re-sends the key, which is why a certificate needs
// no password_version companion: the thing in state changes with it.
func TestAccSSLCertificate_updateResendsTheKey(t *testing.T) {
	stub, baseURL := startStub(t)

	config := func(name, key string) string {
		return providerConfig(baseURL) + fmt.Sprintf(`
resource "zone_ssl_certificate" "this" {
  service_name = %q
  name         = %q
  certificate  = %q
  private_key  = %q
}
`, stubServiceName, name, testCertificate, key)
	}

	resource.Test(t, resource.TestCase{
		TerraformVersionChecks:   writeOnlyTest,
		ProtoV6ProviderFactories: testProviderFactories,
		Steps: []resource.TestStep{
			{Config: config("first", testPrivateKey)},
			{
				Config: config("renewed", testOtherPrivateKey),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("zone_ssl_certificate.this", "name", "renewed"),
					func(*terraform.State) error {
						if got := stub.LastPrivateKey(); got != testOtherPrivateKey {
							return fmt.Errorf("the update did not re-send the new key")
						}
						return nil
					},
				),
			},
		},
	})
}

// A malformed key must be reported without its contents ever being printed.
func TestAccSSLCertificate_rejectsAMisplacedKey(t *testing.T) {
	_, baseURL := startStub(t)

	resource.Test(t, resource.TestCase{
		TerraformVersionChecks:   writeOnlyTest,
		ProtoV6ProviderFactories: testProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerConfig(baseURL) + fmt.Sprintf(`
resource "zone_ssl_certificate" "this" {
  service_name = %q
  name         = "swapped"
  certificate  = %q
  private_key  = %q
}
`, stubServiceName, testPrivateKey, testCertificate),
				ExpectError: regexp.MustCompile(`BEGIN PRIVATE KEY`),
			},
		},
	})
}

func TestAccMySQLDatabase_lifecycle(t *testing.T) {
	stub, baseURL := startStub(t)

	config := func(comment string) string {
		return providerConfig(baseURL) + fmt.Sprintf(`
resource "zone_mysql_database" "this" {
  service_name = %q
  name         = "d10001_app"
  comment      = %q
  collation    = "utf8mb4_unicode_ci"
}
`, stubServiceName, comment)
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config("application data"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("zone_mysql_database.this", "name", "d10001_app"),
					resource.TestCheckResourceAttr("zone_mysql_database.this", "comment", "application data"),
					// Never returned by the API, so it can only come from config.
					resource.TestCheckResourceAttr("zone_mysql_database.this", "collation", "utf8mb4_unicode_ci"),
					resource.TestCheckResourceAttr("zone_mysql_database.this", "disk_usage", "0"),
				),
			},
			{
				Config:   config("application data"),
				PlanOnly: true,
			},
			{
				// There is no PUT, so this must plan as a replacement rather
				// than an in-place update.
				Config: config("renamed"),
				Check:  resource.TestCheckResourceAttr("zone_mysql_database.this", "comment", "renamed"),
			},
			{
				ResourceName:                         "zone_mysql_database.this",
				ImportState:                          true,
				ImportStateVerify:                    true,
				ImportStateVerifyIdentifierAttribute: "name",
				ImportStateId:                        stubServiceName + "/d10001_app",
				// collation is not readable, so an imported database cannot
				// have one in state.
				ImportStateVerifyIgnore: []string{"collation"},
			},
		},
	})

	if got := stub.CountDatabases(stubServiceName); got != 0 {
		t.Errorf("%d databases survived destroy, want 0", got)
	}
}

func TestAccMySQLAccountAndPermission_lifecycle(t *testing.T) {
	stub, baseURL := startStub(t)

	config := func(permissions string) string {
		return providerConfig(baseURL) + fmt.Sprintf(`
resource "zone_mysql_database" "db" {
  service_name = %q
  name         = "d10001_app"
}

resource "zone_mysql_account" "app" {
  service_name = %q
  username     = "d10001_app"
  password     = "correct-horse-battery"
  require_ssl  = true
  hosts        = ["ws", "pma"]
}

resource "zone_mysql_permission" "app" {
  service_name = %q
  username     = zone_mysql_account.app.username
  database     = zone_mysql_database.db.name
  permissions  = %s
}
`, stubServiceName, stubServiceName, stubServiceName, permissions)
	}

	resource.Test(t, resource.TestCase{
		TerraformVersionChecks:   writeOnlyTest,
		ProtoV6ProviderFactories: testProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config(`["SELECT", "INSERT"]`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("zone_mysql_account.app", "require_ssl", "true"),
					resource.TestCheckResourceAttr("zone_mysql_account.app", "hosts.#", "2"),
					resource.TestCheckNoResourceAttr("zone_mysql_account.app", "password"),
					resource.TestCheckResourceAttr("zone_mysql_permission.app", "permissions.#", "2"),
					func(*terraform.State) error {
						if got := stub.LastPassword(); got != "correct-horse-battery" {
							return fmt.Errorf("the API received password %q", got)
						}
						return nil
					},
				),
			},
			{
				Config:   config(`["SELECT", "INSERT"]`),
				PlanOnly: true,
			},
			{
				Config: config(`["SELECT"]`),
				Check: resource.TestCheckResourceAttr(
					"zone_mysql_permission.app", "permissions.#", "1"),
			},
		},
	})
}

// A grant cannot be created with a PUT that might be overwriting somebody
// else's privileges, so an existing grant has to be imported instead.
func TestAccMySQLPermission_refusesToAdoptAnExistingGrant(t *testing.T) {
	stub, baseURL := startStub(t)
	stub.SeedMySQLPermission(stubServiceName, "d10001_app", "d10001_db", []string{"ALL PRIVILEGES"})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerConfig(baseURL) + fmt.Sprintf(`
resource "zone_mysql_permission" "app" {
  service_name = %q
  username     = "d10001_app"
  database     = "d10001_db"
  permissions  = ["SELECT"]
}
`, stubServiceName),
				ExpectError: regexp.MustCompile(`Grant already exists`),
			},
		},
	})
}

func TestAccMailAccount_lifecycle(t *testing.T) {
	stub, baseURL := startStub(t)

	config := func(comment, spamlevel string) string {
		return providerConfig(baseURL) + fmt.Sprintf(`
resource "zone_mail_account" "info" {
  service_name = %q
  address      = "info@example.com"
  password     = "correct-horse-battery"
  comment      = %q
  spamlevel    = %q
}
`, stubServiceName, comment, spamlevel)
	}

	resource.Test(t, resource.TestCase{
		TerraformVersionChecks:   writeOnlyTest,
		ProtoV6ProviderFactories: testProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config("reception", "medium"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("zone_mail_account.info", "address", "info@example.com"),
					resource.TestCheckResourceAttr("zone_mail_account.info", "comment", "reception"),
					resource.TestCheckResourceAttr("zone_mail_account.info", "spamlevel", "medium"),
					resource.TestCheckResourceAttr("zone_mail_account.info", "autoreply", "false"),
					resource.TestCheckResourceAttr("zone_mail_account.info", "two_factor_auth", "false"),
					resource.TestCheckNoResourceAttr("zone_mail_account.info", "password"),
					resource.TestCheckResourceAttrSet("zone_mail_account.info", "disk_size"),
				),
			},
			{
				Config:   config("reception", "medium"),
				PlanOnly: true,
			},
			{
				Config: config("reception desk", "high"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("zone_mail_account.info", "comment", "reception desk"),
					resource.TestCheckResourceAttr("zone_mail_account.info", "spamlevel", "high"),
				),
			},
			{
				ResourceName:                         "zone_mail_account.info",
				ImportState:                          true,
				ImportStateVerify:                    true,
				ImportStateVerifyIdentifierAttribute: "address",
				ImportStateId:                        stubServiceName + "/info@example.com",
			},
		},
	})

	if got := stub.CountMailAccounts(stubServiceName); got != 0 {
		t.Errorf("%d mailboxes survived destroy, want 0", got)
	}
}

// Two-factor authentication can only be turned off through the API, so asking
// for it is refused during validation rather than by a 422 mid-apply.
func TestAccMailAccount_rejectsEnablingTwoFactorAuth(t *testing.T) {
	_, baseURL := startStub(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerConfig(baseURL) + fmt.Sprintf(`
resource "zone_mail_account" "info" {
  service_name    = %q
  address         = "info@example.com"
  two_factor_auth = true
}
`, stubServiceName),
				ExpectError: regexp.MustCompile(`two_factor_auth`),
			},
		},
	})
}

// An archived mailbox is still in the listing. Adopting a tombstone would give
// Terraform a resource it could never reconcile.
func TestAccMailAccount_archivedMailboxReadsAsMissing(t *testing.T) {
	stub, baseURL := startStub(t)
	stub.SeedMailAccount(stubServiceName, "gone@example.com", true)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerConfig(baseURL) + fmt.Sprintf(`
resource "zone_mail_account" "gone" {
  service_name = %q
  address      = "gone@example.com"
}
`, stubServiceName),
				ResourceName:  "zone_mail_account.gone",
				ImportState:   true,
				ImportStateId: stubServiceName + "/gone@example.com",
				ExpectError:   regexp.MustCompile(`(?s)not found|Cannot import`),
			},
		},
	})
}

func TestAccMailForwarderAndAutoreply_lifecycle(t *testing.T) {
	_, baseURL := startStub(t)

	config := func(enabled bool) string {
		return providerConfig(baseURL) + fmt.Sprintf(`
resource "zone_mail_forwarder" "sales" {
  service_name      = %q
  address           = "sales@example.com"
  forward_addresses = ["owner@example.com", "backup@example.net"]
  comment           = "sales enquiries"
}

resource "zone_mail_autoreply" "sales" {
  service_name = %q
  target       = "forwarder"
  address      = zone_mail_forwarder.sales.address
  enabled      = %t
  subject      = "Out of office"
  body         = "We are closed until January."
  date_start   = "2026-12-24"
  date_end     = "2027-01-02"
}
`, stubServiceName, stubServiceName, enabled)
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config(true),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("zone_mail_forwarder.sales", "forward_addresses.#", "2"),
					resource.TestCheckResourceAttr("zone_mail_autoreply.sales", "enabled", "true"),
					resource.TestCheckResourceAttr("zone_mail_autoreply.sales", "subject", "Out of office"),
					resource.TestCheckResourceAttr("zone_mail_autoreply.sales", "date_start", "2026-12-24"),
				),
			},
			{
				Config:   config(true),
				PlanOnly: true,
			},
			{
				// Writing the autoreply flips the parent's read-only autoreply
				// flag, which is only observable after a refresh: Terraform
				// does not re-read the forwarder mid-apply because a different
				// resource changed something about it. This step re-applies the
				// same configuration, so the check runs against refreshed
				// state, and it fails if the write did not invalidate the
				// parent's cached listing.
				Config: config(true),
				Check: resource.TestCheckResourceAttr(
					"zone_mail_forwarder.sales", "autoreply", "true"),
			},
			{
				Config: config(false),
				Check:  resource.TestCheckResourceAttr("zone_mail_autoreply.sales", "enabled", "false"),
			},
			{
				ResourceName:                         "zone_mail_autoreply.sales",
				ImportState:                          true,
				ImportStateVerify:                    true,
				ImportStateVerifyIdentifierAttribute: "address",
				ImportStateId:                        stubServiceName + "/forwarder/sales@example.com",
			},
		},
	})
}

func TestAccMailAutoreply_rejectsAReversedDateRange(t *testing.T) {
	_, baseURL := startStub(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerConfig(baseURL) + fmt.Sprintf(`
resource "zone_mail_autoreply" "backwards" {
  service_name = %q
  target       = "account"
  address      = "info@example.com"
  body         = "Away."
  date_start   = "2027-01-02"
  date_end     = "2026-12-24"
}
`, stubServiceName),
				ExpectError: regexp.MustCompile(`Autoreply ends before it starts`),
			},
		},
	})
}

func TestAccMailDKIM_lifecycle(t *testing.T) {
	_, baseURL := startStub(t)

	config := providerConfig(baseURL) + fmt.Sprintf(`
resource "zone_mail_dkim" "this" {
  service_name = %q
}
`, stubServiceName)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("zone_mail_dkim.this", "public_key"),
					resource.TestMatchResourceAttr("zone_mail_dkim.this", "public_key", regexp.MustCompile(`^v=DKIM1`)),
				),
			},
			{
				Config:   config,
				PlanOnly: true,
			},
			{
				ResourceName:                         "zone_mail_dkim.this",
				ImportState:                          true,
				ImportStateVerify:                    true,
				ImportStateVerifyIdentifierAttribute: "service_name",
				ImportStateId:                        stubServiceName,
			},
		},
	})
}

// service_name is not a DNS zone, so a wrong one has to say so rather than
// producing a confusing decode failure.
func TestAccWebhosting_unknownServiceIsReported(t *testing.T) {
	_, baseURL := startStub(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerConfig(baseURL) + `
resource "zone_mysql_database" "this" {
  service_name = "not-a-service.example"
  name         = "d1_x"
}
`,
				ExpectError: regexp.MustCompile(`Service not found`),
			},
		},
	})
}
