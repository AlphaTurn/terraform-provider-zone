package provider

import (
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// Live tests run against a real zone.eu account.
//
// They are skipped unless ZONE_USERNAME, ZONE_API_TOKEN and ZONE_TEST_ZONE are
// all set, so an ordinary `go test ./...` never reaches them. ZONE_TEST_ZONE
// must name a disposable zone: these create and delete real DNS records and
// spend a real rate-limit budget.
//
// Everything they touch sits under a _tf-provider-test label, and no existing
// record is ever read-modify-written, so a mistake here cannot damage records
// the zone already had.
const liveRecordLabel = "_tf-provider-test"

func liveZone(t *testing.T) string {
	t.Helper()

	zone := os.Getenv("ZONE_TEST_ZONE")
	if zone == "" || os.Getenv("ZONE_USERNAME") == "" || os.Getenv("ZONE_API_TOKEN") == "" {
		t.Skip("set ZONE_USERNAME, ZONE_API_TOKEN and ZONE_TEST_ZONE to run live tests")
	}
	return zone
}

// liveProviderConfig leaves credentials to the environment and keeps the real
// default rate limit, since the point is to exercise the real API.
const liveProviderConfig = `
provider "zone" {}
`

func TestLiveTXTRecordLifecycle(t *testing.T) {
	zone := liveZone(t)
	name := fmt.Sprintf("%s.%s", liveRecordLabel, zone)

	config := func(value string) string {
		return liveProviderConfig + fmt.Sprintf(`
resource "zone_dns_txt_record" "probe" {
  zone        = %q
  name        = %q
  destination = %q
}
`, zone, name, value)
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config("terraform-provider-zone live test"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("zone_dns_txt_record.probe", "name", name),
					resource.TestCheckResourceAttr("zone_dns_txt_record.probe", "modifiable", "true"),
					resource.TestCheckResourceAttrSet("zone_dns_txt_record.probe", "id"),
				),
			},
			{
				// The step after an apply must produce an empty plan, which is
				// the real check that every field round-trips through the API
				// unchanged.
				Config:   config("terraform-provider-zone live test"),
				PlanOnly: true,
			},
			{
				Config: config("terraform-provider-zone live test, updated"),
				Check: resource.TestCheckResourceAttr(
					"zone_dns_txt_record.probe", "destination", "terraform-provider-zone live test, updated"),
			},
			{
				ResourceName:      "zone_dns_txt_record.probe",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateIdFunc: importIDFor("zone_dns_txt_record.probe"),
			},
		},
	})
}

// A zone read is the cheapest possible check that credentials and endpoint are
// right, and that the zone is reachable from this account.
func TestLiveZoneDataSource(t *testing.T) {
	zone := liveZone(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: liveProviderConfig + fmt.Sprintf(`
data "zone_dns_zone" "this" {
  name = %q
}
`, zone),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.zone_dns_zone.this", "name", zone),
					resource.TestCheckResourceAttrSet("data.zone_dns_zone.this", "active"),
					resource.TestCheckResourceAttrSet("data.zone_dns_zone.this", "resource_url"),
				),
			},
		},
	})
}
