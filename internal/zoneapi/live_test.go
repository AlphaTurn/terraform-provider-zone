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

// liveService picks a real webhosting service to read from. ZONE_TEST_SERVICE
// names one explicitly; otherwise the first the account can see is used, which
// is fine because everything below is a GET.
func liveService(t *testing.T, client *Client) string {
	t.Helper()

	if name := os.Getenv("ZONE_TEST_SERVICE"); name != "" {
		return name
	}

	services, err := client.ListVServers(context.Background())
	if err != nil {
		t.Fatalf("ListVServers: %v", err)
	}
	if len(services) == 0 {
		t.Skip("the account has no webhosting services to read")
	}
	return services[0].Name
}

func TestLiveMailReads(t *testing.T) {
	client := liveClient(t)
	ctx := context.Background()
	service := liveService(t, client)

	accounts, err := client.ListMailAccounts(ctx, service)
	if err != nil {
		if IsForbidden(err) {
			t.Skipf("%s is delegated and refuses this endpoint for this account", service)
		}
		t.Fatalf("ListMailAccounts(%q): %v", service, err)
	}

	for _, account := range accounts {
		if account.Address == "" {
			t.Error("a mail account decoded with no address")
		}
		// disk_size is a number on the wire here, unlike a database's
		// disk_usage, which is a string. A zero would mean the coercion picked
		// the wrong one.
		if account.DiskSize == 0 && !account.Archived() {
			t.Errorf("%s decoded with a quota of zero", account.Address)
		}
	}

	if _, err := client.ListMailForwarders(ctx, service); err != nil && !IsForbidden(err) {
		t.Errorf("ListMailForwarders(%q): %v", service, err)
	}

	// An autoreply read needs a live address to hang off.
	for _, account := range accounts {
		if account.Archived() {
			continue
		}
		if _, err := client.GetAutoreply(ctx, service, AutoreplyOnAccount, account.Address); err != nil {
			t.Errorf("GetAutoreply(%q): %v", account.Address, err)
		}
		break
	}
}

func TestLiveMySQLReads(t *testing.T) {
	client := liveClient(t)
	ctx := context.Background()
	service := liveService(t, client)

	databases, err := client.ListMySQLDatabases(ctx, service)
	if err != nil {
		if IsForbidden(err) {
			t.Skipf("%s is delegated and refuses this endpoint for this account", service)
		}
		t.Fatalf("ListMySQLDatabases(%q): %v", service, err)
	}
	for _, database := range databases {
		if database.Name == "" {
			t.Error("a database decoded with no name")
		}
	}

	accounts, err := client.ListMySQLAccounts(ctx, service)
	if err != nil && !IsForbidden(err) {
		t.Fatalf("ListMySQLAccounts(%q): %v", service, err)
	}
	for _, account := range accounts {
		if account.Username == "" {
			t.Error("a database account decoded with no username")
		}
	}

	// Grants hang off an account, so this only runs when there is one.
	if len(accounts) > 0 {
		_, err := client.ListMySQLPermissions(ctx, service, accounts[0].Username)
		if err != nil && !IsForbidden(err) {
			t.Errorf("ListMySQLPermissions(%q): %v", accounts[0].Username, err)
		}
	}
}

// The certificate listing is what GetCertificate answers from, so this is also
// the check that the listing carries the full certificate rather than a
// summary. A summary would produce permanent phantom drift.
func TestLiveCertificateReads(t *testing.T) {
	client := liveClient(t)
	ctx := context.Background()
	service := liveService(t, client)

	certificates, err := client.ListCertificates(ctx, service)
	if err != nil {
		if IsForbidden(err) {
			t.Skipf("%s is delegated and refuses this endpoint for this account", service)
		}
		t.Fatalf("ListCertificates(%q): %v", service, err)
	}
	if len(certificates) == 0 {
		t.Skip("the service has no certificates to read")
	}

	for _, certificate := range certificates {
		if certificate.ID == "" {
			t.Error("a certificate decoded with no id")
		}
		if certificate.Certificate == "" {
			t.Errorf("certificate %s came back without its body, so the listing is a summary "+
				"and reads must use the item endpoint instead", certificate.ID)
		}
		if certificate.Expires == "" {
			t.Errorf("certificate %s decoded with no expiry", certificate.ID)
		}
	}
}
