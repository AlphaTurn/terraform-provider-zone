package provider

import (
	"fmt"
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// importIDFor builds the "<zone>/<id>" import ID from a resource already in state.
func importIDFor(resourceName string) resource.ImportStateIdFunc {
	return func(state *terraform.State) (string, error) {
		rs, ok := state.RootModule().Resources[resourceName]
		if !ok {
			return "", fmt.Errorf("resource %s is not in state", resourceName)
		}
		return rs.Primary.Attributes["zone"] + "/" + rs.Primary.Attributes["id"], nil
	}
}

// TestMain enables the acceptance harness for this package.
//
// These tests run against the in-process stub in stub_test.go, not against
// zone.eu: they need no credentials, cost nothing and touch no real DNS, so
// gating them behind a manually set TF_ACC would only mean nobody runs them.
// They do need a terraform binary on PATH.
func TestMain(m *testing.M) {
	if err := os.Setenv("TF_ACC", "1"); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}

var testProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"zone": providerserver.NewProtocol6WithError(New("test")()),
}

// providerConfig points the provider at the stub. Credentials are arbitrary;
// the stub only checks that both are present, as the real API's basic auth does.
//
// The rate limit is lifted because the stub has none: left at the default the
// suite would spend most of its time being paced for an API it never calls.
// Pacing itself is covered by the client's own tests.
func providerConfig(baseURL string) string {
	return fmt.Sprintf(`
provider "zone" {
  username   = "tester"
  api_token  = "test-token"
  base_url   = %q
  rate_limit = 600000
}
`, baseURL)
}

func startStub(t *testing.T) (*stubAPI, string) {
	t.Helper()
	stub := newStubAPI()
	return stub, stub.Start(t)
}

func TestAccARecord_lifecycle(t *testing.T) {
	stub, baseURL := startStub(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerConfig(baseURL) + `
resource "zone_dns_a_record" "www" {
  zone        = "example.com"
  name        = "www.example.com"
  destination = "203.0.113.10"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("zone_dns_a_record.www", "name", "www.example.com"),
					resource.TestCheckResourceAttr("zone_dns_a_record.www", "destination", "203.0.113.10"),
					resource.TestCheckResourceAttr("zone_dns_a_record.www", "modifiable", "true"),
					resource.TestCheckResourceAttr("zone_dns_a_record.www", "deletable", "true"),
					resource.TestCheckResourceAttrSet("zone_dns_a_record.www", "id"),
					resource.TestCheckResourceAttrSet("zone_dns_a_record.www", "resource_url"),
					// The API returns comment as null, which must not read as "".
					resource.TestCheckNoResourceAttr("zone_dns_a_record.www", "comment"),
				),
			},
			{
				// Updating in place, not replacing.
				Config: providerConfig(baseURL) + `
resource "zone_dns_a_record" "www" {
  zone        = "example.com"
  name        = "www.example.com"
  destination = "203.0.113.20"
}
`,
				Check: resource.TestCheckResourceAttr("zone_dns_a_record.www", "destination", "203.0.113.20"),
			},
			{
				// Renaming is an update too: the API allows it.
				Config: providerConfig(baseURL) + `
resource "zone_dns_a_record" "www" {
  zone        = "example.com"
  name        = "web.example.com"
  destination = "203.0.113.20"
}
`,
				Check: resource.TestCheckResourceAttr("zone_dns_a_record.www", "name", "web.example.com"),
			},
			{
				ResourceName:      "zone_dns_a_record.www",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateIdFunc: importIDFor("zone_dns_a_record.www"),
			},
		},
	})

	if remaining := stub.CountRecords("example.com", "a"); remaining != 0 {
		t.Errorf("records left after destroy = %d, want 0", remaining)
	}
}

func TestAccMXRecord_priority(t *testing.T) {
	_, baseURL := startStub(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerConfig(baseURL) + `
resource "zone_dns_mx_record" "mail" {
  zone        = "example.com"
  name        = "example.com"
  destination = "mx.example.net"
  priority    = 10
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("zone_dns_mx_record.mail", "priority", "10"),
					resource.TestCheckResourceAttr("zone_dns_mx_record.mail", "name", "example.com"),
				),
			},
			{
				Config: providerConfig(baseURL) + `
resource "zone_dns_mx_record" "mail" {
  zone        = "example.com"
  name        = "example.com"
  destination = "mx.example.net"
  priority    = 20
}
`,
				Check: resource.TestCheckResourceAttr("zone_dns_mx_record.mail", "priority", "20"),
			},
		},
	})
}

// SRV carries the most type-specific fields, and is the one type whose id the
// real API renders as a number rather than a quoted string.
func TestAccSRVRecord_allFields(t *testing.T) {
	_, baseURL := startStub(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerConfig(baseURL) + `
resource "zone_dns_srv_record" "sip" {
  zone        = "example.com"
  name        = "_sip._tcp.example.com"
  destination = "sip.example.com"
  priority    = 10
  weight      = 20
  port        = 5060
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("zone_dns_srv_record.sip", "priority", "10"),
					resource.TestCheckResourceAttr("zone_dns_srv_record.sip", "weight", "20"),
					resource.TestCheckResourceAttr("zone_dns_srv_record.sip", "port", "5060"),
					resource.TestCheckResourceAttrSet("zone_dns_srv_record.sip", "id"),
				),
			},
		},
	})
}

func TestAccCAARecord_flagDefaults(t *testing.T) {
	_, baseURL := startStub(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories,
		Steps: []resource.TestStep{
			{
				// flag is omitted, so the schema default must supply 0.
				Config: providerConfig(baseURL) + `
resource "zone_dns_caa_record" "le" {
  zone        = "example.com"
  name        = "example.com"
  destination = "letsencrypt.org"
  tag         = "issue"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("zone_dns_caa_record.le", "flag", "0"),
					resource.TestCheckResourceAttr("zone_dns_caa_record.le", "tag", "issue"),
				),
			},
		},
	})
}

func TestAccURLRecord_redirectCodeDefaults(t *testing.T) {
	_, baseURL := startStub(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerConfig(baseURL) + `
resource "zone_dns_url_record" "go" {
  zone        = "example.com"
  name        = "go.example.com"
  destination = "https://example.org/landing"
}
`,
				Check: resource.TestCheckResourceAttr("zone_dns_url_record.go", "redirect_code", "301"),
			},
			{
				Config: providerConfig(baseURL) + `
resource "zone_dns_url_record" "go" {
  zone          = "example.com"
  name          = "go.example.com"
  destination   = "https://example.org/landing"
  redirect_code = 302
}
`,
				Check: resource.TestCheckResourceAttr("zone_dns_url_record.go", "redirect_code", "302"),
			},
		},
	})
}

func TestAccTXTRecord_longValue(t *testing.T) {
	_, baseURL := startStub(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerConfig(baseURL) + `
resource "zone_dns_txt_record" "spf" {
  zone        = "example.com"
  name        = "example.com"
  destination = "v=spf1 a mx include:_spf.zone.eu -all"
}
`,
				Check: resource.TestCheckResourceAttr("zone_dns_txt_record.spf", "destination", "v=spf1 a mx include:_spf.zone.eu -all"),
			},
		},
	})
}

// A name outside the zone is caught during validation, before any request.
func TestAccRecord_relativeNameRejected(t *testing.T) {
	_, baseURL := startStub(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerConfig(baseURL) + `
resource "zone_dns_a_record" "bad" {
  zone        = "example.com"
  name        = "www"
  destination = "203.0.113.10"
}
`,
				ExpectError: regexp.MustCompile(`must be fully qualified within the zone`),
			},
		},
	})
}

func TestAccARecord_rejectsIPv6(t *testing.T) {
	_, baseURL := startStub(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerConfig(baseURL) + `
resource "zone_dns_a_record" "bad" {
  zone        = "example.com"
  name        = "www.example.com"
  destination = "2001:db8::1"
}
`,
				ExpectError: regexp.MustCompile(`Expected an IPv4 address`),
			},
		},
	})
}

func TestAccAAAARecord_rejectsIPv4(t *testing.T) {
	_, baseURL := startStub(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerConfig(baseURL) + `
resource "zone_dns_aaaa_record" "bad" {
  zone        = "example.com"
  name        = "www.example.com"
  destination = "203.0.113.10"
}
`,
				ExpectError: regexp.MustCompile(`Expected an IPv6 address`),
			},
		},
	})
}

func TestAccCAARecord_rejectsUnknownTag(t *testing.T) {
	_, baseURL := startStub(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerConfig(baseURL) + `
resource "zone_dns_caa_record" "bad" {
  zone        = "example.com"
  name        = "example.com"
  destination = "letsencrypt.org"
  tag         = "nonsense"
}
`,
				ExpectError: regexp.MustCompile(`Attribute tag value must be one of`),
			},
		},
	})
}

// A record removed in the ZoneID panel must come back as a planned create, not
// as a failed refresh.
func TestAccARecord_driftRecreates(t *testing.T) {
	stub, baseURL := startStub(t)

	config := providerConfig(baseURL) + `
resource "zone_dns_a_record" "www" {
  zone        = "example.com"
  name        = "www.example.com"
  destination = "203.0.113.10"
}
`

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config,
			},
			{
				PreConfig: func() {
					// Delete it behind Terraform's back.
					for _, id := range stub.RecordIDs("example.com", "a") {
						stub.RemoveRecord("example.com", "a", id)
					}
				},
				Config: config,
				Check:  resource.TestCheckResourceAttr("zone_dns_a_record.www", "destination", "203.0.113.10"),
			},
		},
	})
}

// zone.eu marks some records as its own. Attempting to change one should say so
// plainly rather than surfacing a bare 400.
func TestAccRecord_immutableIsExplained(t *testing.T) {
	stub, baseURL := startStub(t)

	id := stub.SeedRecord("example.com", "ns", map[string]any{
		"name":        "example.com",
		"destination": "ns.zone.eu",
		"modify":      false,
		"delete":      false,
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:             providerConfig(baseURL) + lockedRecordConfig,
				ResourceName:       "zone_dns_ns_record.locked",
				ImportState:        true,
				ImportStateId:      fmt.Sprintf("example.com/%d", id),
				ImportStatePersist: true,
				ImportStateCheck: func(states []*terraform.InstanceState) error {
					if len(states) != 1 {
						return fmt.Errorf("expected one imported instance, got %d", len(states))
					}
					if got := states[0].Attributes["modifiable"]; got != "false" {
						return fmt.Errorf("modifiable = %q, want false", got)
					}
					if got := states[0].Attributes["deletable"]; got != "false" {
						return fmt.Errorf("deletable = %q, want false", got)
					}
					return nil
				},
			},
			{
				Config: providerConfig(baseURL) + `
resource "zone_dns_ns_record" "locked" {
  zone        = "example.com"
  name        = "example.com"
  destination = "ns.elsewhere.example"
}
`,
				ExpectError: regexp.MustCompile(`Record cannot be modified`),
			},
			{
				// Destroying it is refused just as clearly.
				Config:      providerConfig(baseURL) + lockedRecordConfig,
				Destroy:     true,
				ExpectError: regexp.MustCompile(`Record cannot be deleted`),
			},
			{
				// Unlock it so the harness can tear down, and confirm the
				// capability flags are picked up again on refresh.
				PreConfig: func() { stub.SetFlags("example.com", "ns", id, true, true) },
				Config:    providerConfig(baseURL) + lockedRecordConfig,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("zone_dns_ns_record.locked", "modifiable", "true"),
					resource.TestCheckResourceAttr("zone_dns_ns_record.locked", "deletable", "true"),
				),
			},
		},
	})
}

const lockedRecordConfig = `
resource "zone_dns_ns_record" "locked" {
  zone        = "example.com"
  name        = "example.com"
  destination = "ns.zone.eu"
}
`

func TestAccZone_settings(t *testing.T) {
	_, baseURL := startStub(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerConfig(baseURL) + `
resource "zone_dns_zone" "this" {
  name = "example.com"
  ipv6 = true
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("zone_dns_zone.this", "ipv6", "true"),
					resource.TestCheckResourceAttr("zone_dns_zone.this", "active", "true"),
					resource.TestCheckResourceAttr("zone_dns_zone.this", "dnssec", "true"),
				),
			},
			{
				Config: providerConfig(baseURL) + `
resource "zone_dns_zone" "this" {
  name = "example.com"
  ipv6 = false
}
`,
				Check: resource.TestCheckResourceAttr("zone_dns_zone.this", "ipv6", "false"),
			},
		},
	})
}

func TestAccZone_missingZoneIsExplained(t *testing.T) {
	_, baseURL := startStub(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerConfig(baseURL) + `
resource "zone_dns_zone" "missing" {
  name = "nosuchzone.example"
}
`,
				ExpectError: regexp.MustCompile(`DNS zone not found`),
			},
		},
	})
}

func TestAccRecordsDataSource(t *testing.T) {
	stub, baseURL := startStub(t)

	stub.SeedRecord("example.com", "a", map[string]any{"name": "one.example.com", "destination": "203.0.113.1"})
	stub.SeedRecord("example.com", "a", map[string]any{"name": "two.example.com", "destination": "203.0.113.2"})
	stub.SeedRecord("example.com", "mx", map[string]any{"name": "example.com", "destination": "mx.example.net", "priority": 10})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerConfig(baseURL) + `
data "zone_dns_records" "a_only" {
  zone = "example.com"
  type = "a"
}

data "zone_dns_records" "everything" {
  zone = "example.com"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.zone_dns_records.a_only", "records.#", "2"),
					resource.TestCheckResourceAttr("data.zone_dns_records.a_only", "records.0.name", "one.example.com"),
					resource.TestCheckResourceAttr("data.zone_dns_records.a_only", "records.0.type", "a"),
					// Type-specific attributes are null on types that lack them.
					resource.TestCheckNoResourceAttr("data.zone_dns_records.a_only", "records.0.priority"),
					resource.TestCheckResourceAttr("data.zone_dns_records.everything", "records.#", "3"),
					resource.TestCheckResourceAttr("data.zone_dns_records.everything", "records.2.type", "mx"),
					resource.TestCheckResourceAttr("data.zone_dns_records.everything", "records.2.priority", "10"),
				),
			},
		},
	})
}

func TestAccRecordsDataSource_rejectsUnknownType(t *testing.T) {
	_, baseURL := startStub(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerConfig(baseURL) + `
data "zone_dns_records" "bad" {
  zone = "example.com"
  type = "spf"
}
`,
				ExpectError: regexp.MustCompile(`Unknown record type`),
			},
		},
	})
}

func TestAccZoneDataSource(t *testing.T) {
	_, baseURL := startStub(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerConfig(baseURL) + `
data "zone_dns_zone" "this" {
  name = "example.com"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.zone_dns_zone.this", "active", "true"),
					resource.TestCheckResourceAttr("data.zone_dns_zone.this", "dnssec", "true"),
					resource.TestCheckResourceAttr("data.zone_dns_zone.this", "resource_url", "https://api.zone.eu/v2/dns/example.com"),
				),
			},
		},
	})
}

// Missing credentials should name the environment variable, not fail later with
// an empty Authorization header.
func TestAccProvider_missingCredentials(t *testing.T) {
	t.Setenv(envUsername, "")
	t.Setenv(envAPIToken, "")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
provider "zone" {}

data "zone_dns_zone" "this" {
  name = "example.com"
}
`,
				ExpectError: regexp.MustCompile(`Missing ZoneID (username|API token)`),
			},
		},
	})
}
