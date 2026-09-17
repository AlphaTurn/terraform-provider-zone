package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// The /domain and /vserver half of the stub.
//
// Like the DNS half, it reproduces what the live API actually does rather than
// what its schema says: both listings paginate with X-Pager-* headers at a
// default of 10 a page, a domain read carries no resource_url, signing_required
// is accepted and never echoed back, and a contact's identificator arrives as
// a quoted string although it is documented as a number.

type stubDomain struct {
	RenewalNotifications bool
	NameserversCustom    bool
	DNSSEC               bool
}

func newStubDomains() map[string]*stubDomain {
	return map[string]*stubDomain{
		"example.com": {RenewalNotifications: true, NameserversCustom: false, DNSSEC: true},
		"example.net": {RenewalNotifications: false, NameserversCustom: true, DNSSEC: false},
	}
}

func newStubNameservers() map[string][]map[string]any {
	return map[string][]map[string]any{
		"example.com": {
			{"hostname": "ns.zone.eu", "ip": []any{"195.43.87.1"}},
			{"hostname": "ns2.zone.eu", "ip": []any{"195.43.87.2"}},
		},
	}
}

func newStubContacts() map[string][]map[string]any {
	return map[string][]map[string]any{
		"example.com": {
			{
				"identificator": "500001",
				"role":          "registrant",
				"type":          "original",
				"name":          "Example Owner",
				"email":         "owner@example.com",
				"country":       "EE",
			},
		},
	}
}

func newStubVServers() []map[string]any {
	return []map[string]any{
		{
			"identificator":        "virt1.example.com",
			"name":                 "virt1.example.com",
			"resource_url":         "https://api.zone.eu/v2/vserver/virt1.example.com",
			"delegated":            nil,
			"group":                "virt10001",
			"homedir":              "/data01/virt10001",
			"package":              "Base",
			"mysql_host":           "d10001.mysql.zonevs.eu",
			"imap_host":            "mail.zone.eu",
			"pop3_host":            "mail.zone.eu",
			"smtp_host":            "smtp.zone.eu",
			"mail_platform_type":   "zmail",
			"disk_size":            274877906944,
			"disk_usage_files":     3551232,
			"disk_usage_email":     44029073,
			"disk_usage_databases": 0,
			"cron_limit":           2,
			"alias_limit":          50,
			"hosts":                []any{"virt1.example.com"},
			"aliases":              []any{},
			"package_features":     []any{"ssh", "cron", "mysql"},
		},
	}
}

// SeedDomainContact inserts a contact directly, for state the provider did not
// create.
func (s *stubAPI) SeedDomainContact(domain string, contact map[string]any) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.nextID++
	id := strconv.FormatInt(s.nextID, 10)
	stored := map[string]any{"identificator": id, "type": "original"}
	for key, value := range contact {
		stored[key] = value
	}
	stored["resource_url"] = fmt.Sprintf("https://api.zone.eu/v2/domain/%s/contact/%s", domain, id)

	s.contacts[domain] = append(s.contacts[domain], stored)
	return id
}

// SeedDomain adds a domain, so a test can build a listing long enough to
// paginate.
func (s *stubAPI) SeedDomain(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.domains[name] = &stubDomain{RenewalNotifications: true, DNSSEC: false}
}

// CountContacts reports how many contacts a domain holds.
func (s *stubAPI) CountContacts(domain string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.contacts[domain])
}

// Nameservers returns a domain's delegation, for asserting on it after an apply.
func (s *stubAPI) Nameservers(domain string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]string, 0, len(s.nameservers[domain]))
	for _, ns := range s.nameservers[domain] {
		hostname, _ := ns["hostname"].(string)
		out = append(out, hostname)
	}
	return out
}

// respondPaged answers the way the paginated endpoints do, so the provider's
// page following is exercised rather than merely compiled.
func (s *stubAPI) respondPaged(w http.ResponseWriter, r *http.Request, items []map[string]any) {
	limit := 10
	if value, err := strconv.Atoi(r.Header.Get("x-pager-limit")); err == nil && value > 0 {
		limit = value
	}
	page := 1
	if value, err := strconv.Atoi(r.Header.Get("x-pager-page")); err == nil && value > 0 {
		page = value
	}

	pages := (len(items) + limit - 1) / limit
	if pages < 1 {
		pages = 1
	}
	w.Header().Set("X-Pager-Enabled", "1")
	w.Header().Set("X-Pager-Items", strconv.Itoa(len(items)))
	w.Header().Set("X-Pager-Limit", strconv.Itoa(limit))
	w.Header().Set("X-Pager-Page", strconv.Itoa(page))
	w.Header().Set("X-Pager-Pages", strconv.Itoa(pages))

	start := min((page-1)*limit, len(items))
	end := min(start+limit, len(items))
	s.respond(w, http.StatusOK, items[start:end])
}

func (s *stubAPI) serveVServer(w http.ResponseWriter, r *http.Request, segments []string) {
	if len(segments) == 1 {
		if r.Method != http.MethodGet {
			s.fail(w, http.StatusNotFound, "Unknown endpoint")
			return
		}

		s.mu.Lock()
		services := append([]map[string]any(nil), s.vservers...)
		s.mu.Unlock()

		s.respondPaged(w, r, services)
		return
	}
	if len(segments) < 3 {
		s.fail(w, http.StatusNotFound, "Unknown endpoint")
		return
	}

	s.serveVServerService(w, r, segments)
}

func (s *stubAPI) serveDomain(w http.ResponseWriter, r *http.Request, segments []string) {
	if len(segments) == 1 {
		s.serveDomainList(w, r)
		return
	}

	name := segments[1]
	s.mu.Lock()
	domain, exists := s.domains[name]
	s.mu.Unlock()
	if !exists {
		s.fail(w, http.StatusNotFound, "Domain not found")
		return
	}

	switch {
	case len(segments) == 2:
		s.serveDomainItem(w, r, name, domain)
	case len(segments) == 3 && segments[2] == "nameserver":
		s.serveNameserverCollection(w, r, name)
	case len(segments) == 4 && segments[2] == "nameserver":
		s.serveNameserverItem(w, r, name, segments[3])
	case len(segments) == 3 && segments[2] == "contact":
		s.serveContactCollection(w, r, name)
	case len(segments) == 4 && segments[2] == "contact":
		s.serveContactItem(w, r, name, segments[3])
	default:
		s.fail(w, http.StatusNotFound, "Unknown endpoint")
	}
}

func (s *stubAPI) serveDomainList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.fail(w, http.StatusNotFound, "Unknown endpoint")
		return
	}

	contains := r.URL.Query().Get("name")

	s.mu.Lock()
	names := make([]string, 0, len(s.domains))
	for name := range s.domains {
		names = append(names, name)
	}
	s.mu.Unlock()

	items := make([]map[string]any, 0, len(names))
	for _, name := range names {
		if contains != "" && !strings.Contains(name, contains) {
			continue
		}
		s.mu.Lock()
		body := domainBody(name, s.domains[name])
		s.mu.Unlock()
		items = append(items, body)
	}

	s.respondPaged(w, r, items)
}

func (s *stubAPI) serveDomainItem(w http.ResponseWriter, r *http.Request, name string, domain *stubDomain) {
	switch r.Method {
	case http.MethodGet:
	case http.MethodPut:
		var payload struct {
			RenewalNotifications *bool `json:"renewal_notifications"`
			SigningRequired      *bool `json:"signing_required"`
			NameserversCustom    *bool `json:"nameservers_custom"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			s.fail(w, http.StatusBadRequest, "Malformed body")
			return
		}
		// The API documents that only false may be written here.
		if payload.NameserversCustom != nil && *payload.NameserversCustom {
			s.failValidation(w, map[string][]string{"nameservers_custom": {"only false allowed"}})
			return
		}

		s.mu.Lock()
		if payload.RenewalNotifications != nil {
			domain.RenewalNotifications = *payload.RenewalNotifications
		}
		if payload.NameserversCustom != nil {
			domain.NameserversCustom = *payload.NameserversCustom
		}
		s.mu.Unlock()
		// signing_required is deliberately accepted and not stored: the live API
		// never returns it either.
	default:
		s.fail(w, http.StatusNotFound, "Unknown endpoint")
		return
	}

	s.mu.Lock()
	body := domainBody(name, domain)
	s.mu.Unlock()
	s.respond(w, http.StatusOK, []map[string]any{body})
}

// domainBody renders a domain, without a resource_url, as the live API does.
func domainBody(name string, domain *stubDomain) map[string]any {
	return map[string]any{
		"identificator":         name,
		"name":                  name,
		"expires":               "2027-09-01T00:00:00+03:00",
		"expired":               false,
		"dnssec":                domain.DNSSEC,
		"dnssec_supported":      true,
		"autorenew":             false,
		"renewal_notifications": domain.RenewalNotifications,
		"nameservers_custom":    domain.NameserversCustom,
		"reactivate":            false,
		"has_pending_dnssec":    false,
		"has_pending_trade":     nil,
		"renew_order":           nil,
		"delegated":             nil,
		"auth_key_enabled":      true,
		"_links":                map[string]any{"contact": "...", "nameserver": "..."},
	}
}

func (s *stubAPI) serveNameserverCollection(w http.ResponseWriter, r *http.Request, domain string) {
	switch r.Method {
	case http.MethodGet:
		s.mu.Lock()
		defer s.mu.Unlock()
		s.respond(w, http.StatusOK, withNameserverURLs(domain, s.nameservers[domain]))

	case http.MethodPost:
		var payload []struct {
			Hostname string   `json:"hostname"`
			IP       []string `json:"ip"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			s.fail(w, http.StatusBadRequest, "Malformed body")
			return
		}
		if len(payload) < 2 {
			s.failValidation(w, map[string][]string{"hostname": {"at least two nameservers are required"}})
			return
		}

		stored := make([]map[string]any, 0, len(payload))
		for _, ns := range payload {
			entry := map[string]any{"hostname": ns.Hostname}
			if len(ns.IP) > 0 {
				ips := make([]any, 0, len(ns.IP))
				for _, ip := range ns.IP {
					ips = append(ips, ip)
				}
				entry["ip"] = ips
			}
			stored = append(stored, entry)
		}

		s.mu.Lock()
		s.nameservers[domain] = stored
		s.mu.Unlock()

		s.respond(w, http.StatusCreated, withNameserverURLs(domain, stored))

	default:
		s.fail(w, http.StatusNotFound, "Unknown endpoint")
	}
}

func (s *stubAPI) serveNameserverItem(w http.ResponseWriter, r *http.Request, domain, hostname string) {
	if r.Method != http.MethodDelete {
		s.fail(w, http.StatusNotFound, "Unknown endpoint")
		return
	}

	s.mu.Lock()
	kept := make([]map[string]any, 0, len(s.nameservers[domain]))
	found := false
	for _, ns := range s.nameservers[domain] {
		if name, _ := ns["hostname"].(string); strings.EqualFold(name, hostname) {
			found = true
			continue
		}
		kept = append(kept, ns)
	}
	s.nameservers[domain] = kept
	s.mu.Unlock()

	if !found {
		s.fail(w, http.StatusNotFound, "Nameserver not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func withNameserverURLs(domain string, nameservers []map[string]any) []map[string]any {
	out := make([]map[string]any, 0, len(nameservers))
	for _, ns := range nameservers {
		copied := map[string]any{}
		for key, value := range ns {
			copied[key] = value
		}
		hostname, _ := ns["hostname"].(string)
		copied["resource_url"] = fmt.Sprintf("https://api.zone.eu/v2/domain/%s/nameserver/%s", domain, hostname)
		out = append(out, copied)
	}
	return out
}

func (s *stubAPI) serveContactCollection(w http.ResponseWriter, r *http.Request, domain string) {
	switch r.Method {
	case http.MethodGet:
		s.mu.Lock()
		defer s.mu.Unlock()
		contacts := s.contacts[domain]
		if contacts == nil {
			contacts = []map[string]any{}
		}
		s.respond(w, http.StatusOK, contacts)

	case http.MethodPost:
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			s.fail(w, http.StatusBadRequest, "Malformed body")
			return
		}
		if email, _ := payload["email"].(string); email != "" && !strings.Contains(email, "@") {
			s.failValidation(w, map[string][]string{"email": {"is not a valid address"}})
			return
		}

		id := s.SeedDomainContact(domain, payload)

		s.mu.Lock()
		defer s.mu.Unlock()
		for _, contact := range s.contacts[domain] {
			if contact["identificator"] == id {
				s.respond(w, http.StatusCreated, []map[string]any{contact})
				return
			}
		}
		s.fail(w, http.StatusInternalServerError, "Contact vanished")

	default:
		s.fail(w, http.StatusNotFound, "Unknown endpoint")
	}
}

func (s *stubAPI) serveContactItem(w http.ResponseWriter, r *http.Request, domain, id string) {
	s.mu.Lock()
	var found map[string]any
	for _, contact := range s.contacts[domain] {
		if contact["identificator"] == id {
			found = contact
			break
		}
	}
	s.mu.Unlock()

	if found == nil {
		s.fail(w, http.StatusNotFound, "Contact not found")
		return
	}

	switch r.Method {
	case http.MethodGet:
		s.respond(w, http.StatusOK, []map[string]any{found})

	case http.MethodPut:
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			s.fail(w, http.StatusBadRequest, "Malformed body")
			return
		}

		s.mu.Lock()
		for key, value := range payload {
			found[key] = value
		}
		s.mu.Unlock()
		s.respond(w, http.StatusOK, []map[string]any{found})

	case http.MethodDelete:
		// Registries treat replacing the registrant as a trade, so the API
		// refuses it. The provider has to explain that rather than retry.
		if role, _ := found["role"].(string); role == "registrant" {
			s.fail(w, http.StatusBadRequest, "The registrant contact cannot be removed")
			return
		}

		s.mu.Lock()
		kept := make([]map[string]any, 0, len(s.contacts[domain]))
		for _, contact := range s.contacts[domain] {
			if contact["identificator"] != id {
				kept = append(kept, contact)
			}
		}
		s.contacts[domain] = kept
		s.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)

	default:
		s.fail(w, http.StatusNotFound, "Unknown endpoint")
	}
}
