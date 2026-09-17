package provider

import (
	"fmt"
	"regexp"
	"strconv"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

const testSSHKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIStubKeyMaterialNotARealKey deploy@example"

func TestAccSSHSettings_adoptAndToggleAccess(t *testing.T) {
	stub, baseURL := startStub(t)

	config := func(access string) string {
		return providerConfig(baseURL) + fmt.Sprintf(`
resource "zone_ssh_settings" "this" {
  service_name = %q
  access       = %q
}
`, stubServiceName, access)
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config("public"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("zone_ssh_settings.this", "access", "public"),
					resource.TestCheckResourceAttrSet("zone_ssh_settings.this", "username"),
					resource.TestCheckResourceAttrSet("zone_ssh_settings.this", "ipv4"),
					// Null on the wire, so it must read as null rather than "".
					resource.TestCheckNoResourceAttr("zone_ssh_settings.this", "ipv6"),
					// A map keyed by algorithm, not the array the schema claims.
					resource.TestCheckResourceAttr("zone_ssh_settings.this", "server_fingerprints.ED25519", "SHA256:ed25519fingerprint"),
					resource.TestCheckResourceAttr("zone_ssh_settings.this", "server_fingerprints.%", "3"),
				),
			},
			{
				Config:   config("public"),
				PlanOnly: true,
			},
			{
				Config: config("whitelist"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("zone_ssh_settings.this", "access", "whitelist"),
					func(*terraform.State) error {
						if got := stub.SSHAccess(stubServiceName); got != "whitelist" {
							return fmt.Errorf("the API has access = %q", got)
						}
						return nil
					},
				),
			},
			{
				ResourceName:                         "zone_ssh_settings.this",
				ImportState:                          true,
				ImportStateVerify:                    true,
				ImportStateVerifyIdentifierAttribute: "service_name",
				ImportStateId:                        stubServiceName,
			},
		},
	})

	// Destroy is a warning, not an operation: access stays as it was.
	if got := stub.SSHAccess(stubServiceName); got != "whitelist" {
		t.Errorf("destroy changed access to %q; it should have been left alone", got)
	}
}

func TestAccSSHPublicKey_lifecycle(t *testing.T) {
	stub, baseURL := startStub(t)

	config := func(comment string) string {
		return providerConfig(baseURL) + fmt.Sprintf(`
resource "zone_ssh_public_key" "deploy" {
  service_name = %q
  public_key   = %q
  comment      = %q
}
`, stubServiceName, testSSHKey, comment)
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config("deploy key"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("zone_ssh_public_key.deploy", "comment", "deploy key"),
					resource.TestCheckResourceAttr("zone_ssh_public_key.deploy", "type", "ed25519"),
					resource.TestCheckResourceAttrSet("zone_ssh_public_key.deploy", "fingerprint"),
					// Null for Ed25519, a quoted number for RSA.
					resource.TestCheckNoResourceAttr("zone_ssh_public_key.deploy", "size"),
					resource.TestCheckNoResourceAttr("zone_ssh_public_key.deploy", "last_used"),
				),
			},
			{
				Config:   config("deploy key"),
				PlanOnly: true,
			},
			{
				// No PUT exists, so changing the comment must replace the key.
				Config: config("renamed"),
				Check:  resource.TestCheckResourceAttr("zone_ssh_public_key.deploy", "comment", "renamed"),
			},
		},
	})

	if got := stub.CountSSHPublicKeys(stubServiceName); got != 0 {
		t.Errorf("%d keys survived destroy, want 0", got)
	}
}

// Nobody has a key's numeric id to hand; they have the fingerprint.
func TestAccSSHPublicKey_importsByFingerprint(t *testing.T) {
	stub, baseURL := startStub(t)
	id := stub.SeedSSHPublicKey(stubServiceName, testSSHKey, "seeded")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerConfig(baseURL) + fmt.Sprintf(`
resource "zone_ssh_public_key" "seeded" {
  service_name = %q
  public_key   = %q
  comment      = "seeded"
}
`, stubServiceName, testSSHKey),
				ResourceName:  "zone_ssh_public_key.seeded",
				ImportState:   true,
				ImportStateId: fmt.Sprintf("%s/SHA256:stub%d", stubServiceName, id),
				// Not ImportStateVerify: the step applies the configuration as
				// well, so there are two keys with this material and the
				// imported one is deliberately the seeded one rather than the
				// applied one. What matters is that the fingerprint resolved.
				ImportStateCheck: func(states []*terraform.InstanceState) error {
					if len(states) != 1 {
						return fmt.Errorf("imported %d objects, want 1", len(states))
					}
					if got := states[0].Attributes["comment"]; got != "seeded" {
						return fmt.Errorf("comment = %q, want \"seeded\": the fingerprint resolved to the wrong key", got)
					}
					if got := states[0].Attributes["id"]; got != strconv.FormatInt(id, 10) {
						return fmt.Errorf("id = %q, want %d", got, id)
					}
					return nil
				},
			},
		},
	})
}

// A private key in this argument would be a serious mistake, so it is caught
// before anything is sent.
func TestAccSSHPublicKey_rejectsAPrivateKey(t *testing.T) {
	_, baseURL := startStub(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerConfig(baseURL) + fmt.Sprintf(`
resource "zone_ssh_public_key" "wrong" {
  service_name = %q
  public_key   = %q
}
`, stubServiceName, testPrivateKey),
				ExpectError: regexp.MustCompile(`private key must never be sent`),
			},
		},
	})
}

func TestAccSSHWhitelistIP_lifecycle(t *testing.T) {
	_, baseURL := startStub(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerConfig(baseURL) + fmt.Sprintf(`
resource "zone_ssh_whitelist_ip" "office" {
  service_name = %q
  ip           = "217.128.0.0/24"
  comment      = "office"
}

resource "zone_ssh_whitelist_ip" "home" {
  service_name = %q
  ip           = "2001:db8::1"
  comment      = "home"
}
`, stubServiceName, stubServiceName),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("zone_ssh_whitelist_ip.office", "ip", "217.128.0.0/24"),
					resource.TestCheckResourceAttr("zone_ssh_whitelist_ip.home", "ip", "2001:db8::1"),
					resource.TestCheckResourceAttrSet("zone_ssh_whitelist_ip.office", "id"),
				),
			},
		},
	})
}

func TestAccSSHWhitelistIP_rejectsANonAddress(t *testing.T) {
	_, baseURL := startStub(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerConfig(baseURL) + fmt.Sprintf(`
resource "zone_ssh_whitelist_ip" "bad" {
  service_name = %q
  ip           = "office.example.com"
}
`, stubServiceName),
				ExpectError: regexp.MustCompile(`not a valid IP address`),
			},
		},
	})
}

func TestAccFTPUser_lifecycle(t *testing.T) {
	stub, baseURL := startStub(t)

	config := func(profile string) string {
		return providerConfig(baseURL) + fmt.Sprintf(`
resource "zone_ftp_user" "deploy" {
  service_name = %q
  username     = "deploy"
  password     = "correct-horse-battery"
  directory    = "/data01/virt10001/domeenid"

  access_profile     = %q
  allowed_operations = ["allow_download", "allow_upload", "allow_list"]
  access_countries   = ["EE", "FI"]
}
`, stubServiceName, profile)
	}

	resource.Test(t, resource.TestCase{
		TerraformVersionChecks:   writeOnlyTest,
		ProtoV6ProviderFactories: testProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config("whitelist_or_tls"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("zone_ftp_user.deploy", "username", "deploy"),
					resource.TestCheckResourceAttrSet("zone_ftp_user.deploy", "username_system"),
					resource.TestCheckResourceAttr("zone_ftp_user.deploy", "require_tls", "true"),
					resource.TestCheckResourceAttr("zone_ftp_user.deploy", "allowed_operations.#", "3"),
					resource.TestCheckResourceAttr("zone_ftp_user.deploy", "access_countries.#", "2"),
					resource.TestCheckNoResourceAttr("zone_ftp_user.deploy", "password"),
					func(*terraform.State) error {
						if got := stub.LastPassword(); got != "correct-horse-battery" {
							return fmt.Errorf("the API received password %q", got)
						}
						return nil
					},
				),
			},
			{
				Config:   config("whitelist_or_tls"),
				PlanOnly: true,
			},
			{
				Config: config("whitelist"),
				Check: resource.TestCheckResourceAttr(
					"zone_ftp_user.deploy", "access_profile", "whitelist"),
			},
		},
	})
}

func TestAccFTPUser_rejectsAnUnknownOperation(t *testing.T) {
	_, baseURL := startStub(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerConfig(baseURL) + fmt.Sprintf(`
resource "zone_ftp_user" "deploy" {
  service_name       = %q
  allowed_operations = ["allow_everything"]
}
`, stubServiceName),
				ExpectError: regexp.MustCompile(`value must be one of`),
			},
		},
	})
}

func TestAccFTPIPWhitelist_lifecycle(t *testing.T) {
	_, baseURL := startStub(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerConfig(baseURL) + fmt.Sprintf(`
resource "zone_ftp_ip_whitelist" "office" {
  service_name = %q
  ip           = "203.0.113.4"
}
`, stubServiceName),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("zone_ftp_ip_whitelist.office", "ip", "203.0.113.4"),
					resource.TestCheckResourceAttr("zone_ftp_ip_whitelist.office", "country", "EE"),
					resource.TestCheckResourceAttrSet("zone_ftp_ip_whitelist.office", "created"),
				),
			},
		},
	})
}

func TestAccCrontab_lifecycle(t *testing.T) {
	stub, baseURL := startStub(t)

	config := func(schedule string) string {
		return providerConfig(baseURL) + fmt.Sprintf(`
resource "zone_crontab" "nightly" {
  service_name = %q
  name         = "nightly import"
  exec_type    = "system"
  command      = "[[$PHP]] [[$ACC_HOMEDIR_A]]/bin/import.php"
  schedule     = %q

  report        = "onerror"
  report_email  = "ops@example.com"
  nice          = "low"
  timezone      = "Europe/Tallinn"
  runtime_limit = 3600
}
`, stubServiceName, schedule)
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config("15 3 * * *"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("zone_crontab.nightly", "exec_type", "system"),
					resource.TestCheckResourceAttr("zone_crontab.nightly", "nice", "low"),
					resource.TestCheckResourceAttr("zone_crontab.nightly", "runtime_limit", "3600"),
					resource.TestCheckResourceAttr("zone_crontab.nightly", "active", "true"),
					resource.TestCheckResourceAttr("zone_crontab.nightly", "schedule_type", "basic"),
					// The field names are the one thing about this endpoint the
					// live API could not settle, so pin what goes on the wire.
					func(*terraform.State) error {
						payload := stub.LastCrontabPayload()
						for _, field := range []string{"exec_type", "nice", "schedule_type"} {
							if _, sent := payload[field]; !sent {
								return fmt.Errorf("the request did not carry %q; it sent %v", field, keysOf(payload))
							}
						}
						for _, absent := range []string{"type", "priority"} {
							if _, sent := payload[absent]; sent {
								return fmt.Errorf("the request carried the published schema's %q", absent)
							}
						}
						return nil
					},
				),
			},
			{
				Config:   config("15 3 * * *"),
				PlanOnly: true,
			},
			{
				Config: config("30 4 * * *"),
				Check:  resource.TestCheckResourceAttr("zone_crontab.nightly", "schedule", "30 4 * * *"),
			},
			{
				ResourceName:      "zone_crontab.nightly",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateIdFunc: crontabImportID("zone_crontab.nightly"),
			},
		},
	})

	if got := stub.CountCrontabs(stubServiceName); got != 0 {
		t.Errorf("%d jobs survived destroy, want 0", got)
	}
}

// timezone and runtime_limit are only accepted on a system job, and the API
// rejects them otherwise. Saying so at validation names the fix.
func TestAccCrontab_rejectsSystemOnlyArgumentsOnAnHTTPJob(t *testing.T) {
	_, baseURL := startStub(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerConfig(baseURL) + fmt.Sprintf(`
resource "zone_crontab" "ping" {
  service_name = %q
  name         = "ping"
  exec_type    = "http"
  command      = "https://example.com/cron"
  schedule     = "*/5 * * * *"
  timezone     = "UTC"
}
`, stubServiceName),
				ExpectError: regexp.MustCompile(`timezone only applies to a system job`),
			},
		},
	})
}

func TestAccCrontab_rejectsAnUnacceptedRuntimeLimit(t *testing.T) {
	_, baseURL := startStub(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerConfig(baseURL) + fmt.Sprintf(`
resource "zone_crontab" "nightly" {
  service_name  = %q
  name          = "nightly"
  exec_type     = "system"
  command       = "/bin/true"
  schedule      = "0 0 * * *"
  runtime_limit = 7200
}
`, stubServiceName),
				ExpectError: regexp.MustCompile(`value must be one of`),
			},
		},
	})
}

func crontabImportID(resourceName string) resource.ImportStateIdFunc {
	return func(state *terraform.State) (string, error) {
		rs, ok := state.RootModule().Resources[resourceName]
		if !ok {
			return "", fmt.Errorf("%s is not in state", resourceName)
		}
		return rs.Primary.Attributes["service_name"] + "/" + rs.Primary.Attributes["id"], nil
	}
}

func keysOf(payload map[string]any) []string {
	names := make([]string, 0, len(payload))
	for name := range payload {
		names = append(names, name)
	}
	return names
}
