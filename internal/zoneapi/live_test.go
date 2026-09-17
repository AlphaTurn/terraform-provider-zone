package zoneapi

import (
	"context"
	"os"
	"testing"
)

// Live read-only checks against a real zone.eu account.
//
// They are skipped unless ZONE_USERNAME and ZONE_API_TOKEN are set, so an
// ordinary `go test ./...` never reaches them. Every one of them is a GET:
// nothing here creates, changes or deletes anything, because the accounts these
// run against are production.
//
// They exist because this API's published description is wrong in ways that
// only a real response reveals — a field documented as an integer arriving
// quoted, an array arriving as an object, a field the schema never mentions.
// A decode that silently produces a zero value is exactly the bug these catch.
func liveClient(t *testing.T) *Client {
	t.Helper()

	username, token := os.Getenv("ZONE_USERNAME"), os.Getenv("ZONE_API_TOKEN")
	if username == "" || token == "" {
		t.Skip("set ZONE_USERNAME and ZONE_API_TOKEN to run live tests")
	}

	client, err := New(username, token)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return client
}

func TestLiveDomainRoundTrip(t *testing.T) {
	client := liveClient(t)
	ctx := context.Background()

	domains, err := client.ListDomains(ctx, DomainFilter{})
	if err != nil {
		t.Fatalf("ListDomains: %v", err)
	}
	if len(domains) == 0 {
		t.Skip("the account has no domains to read")
	}

	for _, domain := range domains {
		if domain.Name == "" {
			t.Error("a domain decoded with no name, so the listing schema is wrong")
		}
		if domain.Expires == "" {
			t.Errorf("%s decoded with no expiry", domain.Name)
		}
	}

	// The item endpoint is a different code path from the listing, and it is
	// the one every zone_domain read uses.
	name := domains[0].Name
	single, err := client.GetDomain(ctx, name)
	if err != nil {
		t.Fatalf("GetDomain(%q): %v", name, err)
	}
	if single.Name != name {
		t.Errorf("GetDomain(%q) returned name %q", name, single.Name)
	}
	if single.ResourceURL == "" {
		t.Error("resource_url was neither returned nor filled in")
	}
}

func TestLiveDomainNameserversAndContacts(t *testing.T) {
	client := liveClient(t)
	ctx := context.Background()

	domains, err := client.ListDomains(ctx, DomainFilter{})
	if err != nil {
		t.Fatalf("ListDomains: %v", err)
	}
	if len(domains) == 0 {
		t.Skip("the account has no domains to read")
	}
	name := domains[0].Name

	nameservers, err := client.ListNameservers(ctx, name)
	if err != nil {
		t.Fatalf("ListNameservers(%q): %v", name, err)
	}
	if len(nameservers) == 0 {
		t.Errorf("%s decoded with no nameservers, which no registered domain has", name)
	}
	for _, ns := range nameservers {
		if ns.Hostname == "" {
			t.Errorf("a nameserver of %s decoded with no hostname", name)
		}
	}

	contacts, err := client.ListContacts(ctx, name)
	if err != nil {
		t.Fatalf("ListContacts(%q): %v", name, err)
	}
	for _, contact := range contacts {
		// The identificator is documented as a number and arrives quoted, so
		// an empty one here means the coercion is not doing its job.
		if contact.Identificator == "" {
			t.Errorf("a contact of %s decoded with no identificator", name)
		}
		if contact.Role == "" {
			t.Errorf("contact %s of %s decoded with no role", contact.Identificator, name)
		}
	}
}

// /vserver is not in zone.eu's published API description at all, so its schema
// rests entirely on what the wire says.
func TestLiveVServers(t *testing.T) {
	client := liveClient(t)

	services, err := client.ListVServers(context.Background())
	if err != nil {
		t.Fatalf("ListVServers: %v", err)
	}
	if len(services) == 0 {
		t.Skip("the account has no webhosting services to read")
	}

	for _, service := range services {
		if service.Name == "" {
			t.Error("a service decoded with no name")
		}
		if service.DiskSize == 0 {
			t.Errorf("%s decoded with a disk quota of zero, which no package has", service.Name)
		}
	}
}
