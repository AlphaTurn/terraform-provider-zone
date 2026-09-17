package provider

import (
	"fmt"
	"regexp"
	"strconv"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func TestAccDomain_settings(t *testing.T) {
	stub, baseURL := startStub(t)

	settings := func(notifications bool) string {
		return providerConfig(baseURL) + fmt.Sprintf(`
resource "zone_domain" "this" {
  name                  = "example.com"
  renewal_notifications = %t
}
`, notifications)
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: settings(true),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("zone_domain.this", "name", "example.com"),
					resource.TestCheckResourceAttr("zone_domain.this", "renewal_notifications", "true"),
					resource.TestCheckResourceAttr("zone_domain.this", "dnssec", "true"),
					resource.TestCheckResourceAttr("zone_domain.this", "nameservers_custom", "false"),
					resource.TestCheckResourceAttr("zone_domain.this", "expired", "false"),
					// The live API returns no resource_url on a domain, so the
					// provider fills in the URL the object would have had.
					resource.TestCheckResourceAttrSet("zone_domain.this", "resource_url"),
					// Null fields must read as null, not as empty strings.
					resource.TestCheckNoResourceAttr("zone_domain.this", "delegated"),
					resource.TestCheckNoResourceAttr("zone_domain.this", "renew_order"),
					resource.TestCheckNoResourceAttr("zone_domain.this", "has_pending_trade"),
				),
			},
			{
				// The step after an apply must produce an empty plan, which is
				// the real check that every field round-trips unchanged.
				Config:   settings(true),
				PlanOnly: true,
			},
			{
				Config: settings(false),
				Check: resource.TestCheckResourceAttr(
					"zone_domain.this", "renewal_notifications", "false"),
			},
			{
				ResourceName:      "zone_domain.this",
				ImportState:       true,
				ImportStateId:     "example.com",
				ImportStateVerify: true,
				// The resource has no id attribute: a domain is identified by
				// its name, exactly as zone_dns_zone is.
				ImportStateVerifyIdentifierAttribute: "name",
			},
		},
	})

	if stub.Requests.Load() == 0 {
		t.Error("the stub served no requests, so this tested nothing")
	}
}

// signing_required is accepted by the API and never returned, so it can only
// come from prior state. If the provider tried to refresh it, the plan after an
// apply would never be empty.
func TestAccDomain_signingRequiredSurvivesRefresh(t *testing.T) {
	_, baseURL := startStub(t)

	config := providerConfig(baseURL) + `
resource "zone_domain" "this" {
  name             = "example.com"
  signing_required = true
}
`

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config,
				Check:  resource.TestCheckResourceAttr("zone_domain.this", "signing_required", "true"),
			},
			{
				Config:   config,
				PlanOnly: true,
			},
		},
	})
}

func TestAccDomain_missingDomainIsExplained(t *testing.T) {
	_, baseURL := startStub(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerConfig(baseURL) + `
resource "zone_domain" "this" {
  name = "not-on-this-account.com"
}
`,
				ExpectError: regexp.MustCompile(`Domain not found`),
			},
		},
	})
}

func TestAccDomainNameservers_lifecycle(t *testing.T) {
	stub, baseURL := startStub(t)

	two := providerConfig(baseURL) + `
resource "zone_domain_nameservers" "this" {
  domain = "example.com"

  nameserver = [
    { hostname = "ns.zone.eu" },
    { hostname = "ns2.zone.eu" },
  ]
}
`
	three := providerConfig(baseURL) + `
resource "zone_domain_nameservers" "this" {
  domain = "example.com"

  nameserver = [
    { hostname = "ns.zone.eu" },
    { hostname = "ns2.zone.eu" },
    {
      hostname = "ns3.example.com"
      ip       = ["203.0.113.9"]
    },
  ]
}
`

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: two,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("zone_domain_nameservers.this", "nameserver.#", "2"),
					func(*terraform.State) error { return nil },
				),
			},
			{
				Config: three,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("zone_domain_nameservers.this", "nameserver.#", "3"),
					func(*terraform.State) error {
						if got := len(stub.Nameservers("example.com")); got != 3 {
							return fmt.Errorf("the stub holds %d nameservers, want 3", got)
						}
						return nil
					},
				),
			},
			{
				// Shrinking back has to remove the extra one rather than leave
				// it behind, which is the whole point of a whole-set resource.
				Config: two,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("zone_domain_nameservers.this", "nameserver.#", "2"),
					func(*terraform.State) error {
						if got := len(stub.Nameservers("example.com")); got != 2 {
							return fmt.Errorf("the stub holds %d nameservers, want 2", got)
						}
						return nil
					},
				),
			},
		},
	})
}

// Most registries refuse a delegation of one nameserver, so the schema refuses
// it first, with a message instead of a 422.
func TestAccDomainNameservers_rejectsASingleNameserver(t *testing.T) {
	_, baseURL := startStub(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerConfig(baseURL) + `
resource "zone_domain_nameservers" "this" {
  domain = "example.com"

  nameserver = [
    { hostname = "ns.zone.eu" },
  ]
}
`,
				ExpectError: regexp.MustCompile(`at least 2 elements`),
			},
		},
	})
}

func TestAccDomainContact_lifecycle(t *testing.T) {
	stub, baseURL := startStub(t)

	config := func(email string) string {
		return providerConfig(baseURL) + fmt.Sprintf(`
resource "zone_domain_contact" "tech" {
  domain     = "example.com"
  role       = "tech"
  name       = "Example Tech"
  email      = %q
  country    = "EE"
  voice      = "+372.5551234"
}
`, email)
	}

	before := stub.CountContacts("example.com")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config("tech@example.com"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("zone_domain_contact.tech", "role", "tech"),
					resource.TestCheckResourceAttr("zone_domain_contact.tech", "email", "tech@example.com"),
					resource.TestCheckResourceAttrSet("zone_domain_contact.tech", "id"),
					resource.TestCheckResourceAttr("zone_domain_contact.tech", "type", "original"),
				),
			},
			{
				Config:   config("tech@example.com"),
				PlanOnly: true,
			},
			{
				Config: config("noc@example.com"),
				Check: resource.TestCheckResourceAttr(
					"zone_domain_contact.tech", "email", "noc@example.com"),
			},
			{
				ResourceName:      "zone_domain_contact.tech",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateIdFunc: contactImportID("zone_domain_contact.tech"),
			},
		},
	})

	if got := stub.CountContacts("example.com"); got != before {
		t.Errorf("the domain holds %d contacts after destroy, want the original %d", got, before)
	}
}

func TestAccDomainContact_needsAName(t *testing.T) {
	_, baseURL := startStub(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerConfig(baseURL) + `
resource "zone_domain_contact" "tech" {
  domain = "example.com"
  role   = "tech"
  email  = "tech@example.com"
}
`,
				ExpectError: regexp.MustCompile(`Contact needs a name`),
			},
		},
	})
}

func TestAccDomainContact_rejectsAnUnknownRole(t *testing.T) {
	_, baseURL := startStub(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerConfig(baseURL) + `
resource "zone_domain_contact" "tech" {
  domain = "example.com"
  role   = "billing"
  name   = "Example Billing"
}
`,
				ExpectError: regexp.MustCompile(`Attribute role value must be one of`),
			},
		},
	})
}

func TestAccDomains_dataSource(t *testing.T) {
	_, baseURL := startStub(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerConfig(baseURL) + `
data "zone_domains" "all" {}

data "zone_domains" "filtered" {
  name_contains = "example.net"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.zone_domains.all", "domains.#", "2"),
					// Sorted by name, so .com comes first.
					resource.TestCheckResourceAttr("data.zone_domains.all", "domains.0.name", "example.com"),
					resource.TestCheckResourceAttr("data.zone_domains.all", "domains.1.name", "example.net"),
					resource.TestCheckResourceAttr("data.zone_domains.filtered", "domains.#", "1"),
					resource.TestCheckResourceAttr("data.zone_domains.filtered", "domains.0.name", "example.net"),
					resource.TestCheckResourceAttr("data.zone_domains.filtered", "domains.0.nameservers_custom", "true"),
				),
			},
		},
	})
}

// The listing paginates at ten a page by default and the provider asks for a
// hundred. Either way it has to return every domain: a short read would make
// Terraform propose deleting whatever fell off the end.
func TestAccDomains_readsEveryPage(t *testing.T) {
	stub, baseURL := startStub(t)

	const extra = 25
	for i := range extra {
		stub.SeedDomain(fmt.Sprintf("seeded-%02d.example", i))
	}
	want := extra + 2 // The two the stub starts with.

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerConfig(baseURL) + `
data "zone_domains" "all" {}
`,
				Check: resource.TestCheckResourceAttr(
					"data.zone_domains.all", "domains.#", strconv.Itoa(want)),
			},
		},
	})
}

func TestAccVServers_dataSource(t *testing.T) {
	_, baseURL := startStub(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerConfig(baseURL) + `
data "zone_vservers" "all" {}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.zone_vservers.all", "services.#", "1"),
					resource.TestCheckResourceAttr("data.zone_vservers.all", "services.0.name", "virt1.example.com"),
					resource.TestCheckResourceAttr("data.zone_vservers.all", "services.0.mysql_host", "d10001.mysql.zonevs.eu"),
					resource.TestCheckResourceAttr("data.zone_vservers.all", "services.0.cron_limit", "2"),
					resource.TestCheckResourceAttr("data.zone_vservers.all", "services.0.package_features.#", "3"),
					resource.TestCheckNoResourceAttr("data.zone_vservers.all", "services.0.delegated"),
				),
			},
		},
	})
}

// contactImportID builds the "<domain>/<contact id>" import ID from state.
func contactImportID(resourceName string) resource.ImportStateIdFunc {
	return func(state *terraform.State) (string, error) {
		rs, ok := state.RootModule().Resources[resourceName]
		if !ok {
			return "", fmt.Errorf("%s is not in state", resourceName)
		}
		return rs.Primary.Attributes["domain"] + "/" + rs.Primary.Attributes["id"], nil
	}
}
