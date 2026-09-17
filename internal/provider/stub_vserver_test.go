package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// The webhosting half of the stub: mail, MySQL and SSL.
//
// As elsewhere it reproduces observed behaviour rather than the published
// schema: the mail, database and certificate listings paginate; a certificate
// read returns private_key as an empty string; a mail account's autoreply flag
// is read-only and flips when an autoreply is written; and an archived mailbox
// stays in the listing with deleted_at set.

type stubService struct {
	mailAccounts   []map[string]any
	mailForwarders []map[string]any
	autoreplies    map[string]map[string]any
	dkim           string
	databases      []map[string]any
	dbAccounts     []map[string]any
	permissions    map[string][]map[string]any
	certificates   []map[string]any
}

const stubServiceName = "virt1.example.com"

func newStubServices() map[string]*stubService {
	return map[string]*stubService{
		stubServiceName: {
			autoreplies: make(map[string]map[string]any),
			permissions: make(map[string][]map[string]any),
		},
	}
}

// SeedMailAccount inserts a mailbox directly. An archived one is how a
// tombstone is set up.
func (s *stubAPI) SeedMailAccount(service, address string, archived bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	account := map[string]any{
		"address":           address,
		"comment":           "",
		"spamlevel":         "medium",
		"two_factor_auth":   false,
		"autoreply":         false,
		"fwd_addresses":     []any{},
		"disabled_features": []any{},
		"disk_size":         8589934592,
		"disk_usage":        14864,
		"deleted_at":        nil,
		"resource_url":      "https://api.zone.eu/v2/vserver/" + service + "/mail/account/" + address,
	}
	if archived {
		account["deleted_at"] = "2026-01-01T00:00:00+02:00"
	}
	s.services[service].mailAccounts = append(s.services[service].mailAccounts, account)
}

// SeedMySQLPermission grants privileges directly, so a test can exercise the
// refusal to adopt an existing grant.
func (s *stubAPI) SeedMySQLPermission(service, username, database string, permissions []string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	granted := make([]any, 0, len(permissions))
	for _, permission := range permissions {
		granted = append(granted, permission)
	}
	s.services[service].permissions[username] = append(s.services[service].permissions[username], map[string]any{
		"username":     username,
		"database":     database,
		"permissions":  granted,
		"resource_url": "https://api.zone.eu/v2/vserver/" + service + "/database/mysql/account/" + username + "/permission/" + database,
	})
}

// CountMailAccounts reports how many mailboxes a service holds, archived ones
// included.
func (s *stubAPI) CountMailAccounts(service string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.services[service].mailAccounts)
}

// CountDatabases reports how many databases a service holds.
func (s *stubAPI) CountDatabases(service string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.services[service].databases)
}

// CountCertificates reports how many certificates a service holds.
func (s *stubAPI) CountCertificates(service string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.services[service].certificates)
}

// LastPrivateKey returns the key most recently sent to the stub, so a test can
// prove the key reached the API even though it is absent from state.
func (s *stubAPI) LastPrivateKey() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastPrivateKey
}

// LastPassword returns the password most recently sent to the stub.
func (s *stubAPI) LastPassword() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastPassword
}

func (s *stubAPI) serveVServerService(w http.ResponseWriter, r *http.Request, segments []string) {
	service := segments[1]
	s.mu.Lock()
	_, exists := s.services[service]
	s.mu.Unlock()
	if !exists {
		s.fail(w, http.StatusNotFound, "Service not found")
		return
	}

	rest := segments[2:]
	switch {
	case rest[0] == "mail":
		s.serveMail(w, r, service, rest[1:])
	case rest[0] == "database" && len(rest) > 1 && rest[1] == "mysql":
		s.serveMySQL(w, r, service, rest[2:])
	case rest[0] == "ssl":
		s.serveSSL(w, r, service, rest[1:])
	default:
		s.fail(w, http.StatusNotFound, "Unknown endpoint")
	}
}

func (s *stubAPI) serveMail(w http.ResponseWriter, r *http.Request, service string, rest []string) {
	if len(rest) == 0 {
		s.fail(w, http.StatusNotFound, "Unknown endpoint")
		return
	}

	switch {
	case rest[0] == "dkim" && len(rest) == 1:
		s.serveDKIM(w, r, service)
	case rest[0] == "account":
		s.serveMailCollection(w, r, service, "account", rest[1:])
	case rest[0] == "forwarder":
		s.serveMailCollection(w, r, service, "forwarder", rest[1:])
	default:
		s.fail(w, http.StatusNotFound, "Unknown endpoint")
	}
}

func (s *stubAPI) serveDKIM(w http.ResponseWriter, r *http.Request, service string) {
	s.mu.Lock()
	current := s.services[service].dkim
	s.mu.Unlock()

	switch r.Method {
	case http.MethodGet:
		if current == "" {
			// The endpoint answers with an empty object rather than a 404.
			s.respond(w, http.StatusOK, []map[string]any{{}})
			return
		}
		s.respond(w, http.StatusOK, []map[string]any{{"publickey": current}})

	case http.MethodPost:
		s.mu.Lock()
		s.nextID++
		key := fmt.Sprintf("v=DKIM1; k=rsa; p=STUBKEY%d", s.nextID)
		s.services[service].dkim = key
		s.mu.Unlock()
		s.respond(w, http.StatusCreated, []map[string]any{{"publickey": key}})

	case http.MethodDelete:
		s.mu.Lock()
		s.services[service].dkim = ""
		s.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)

	default:
		s.fail(w, http.StatusNotFound, "Unknown endpoint")
	}
}

// serveMailCollection handles accounts and forwarders, which differ only in
// their fields.
func (s *stubAPI) serveMailCollection(w http.ResponseWriter, r *http.Request, service, kind string, rest []string) {
	if len(rest) == 0 {
		s.serveMailList(w, r, service, kind)
		return
	}

	address := rest[0]
	if len(rest) == 2 && rest[1] == "autoreply" {
		s.serveAutoreply(w, r, service, kind, address)
		return
	}
	if len(rest) != 1 {
		s.fail(w, http.StatusNotFound, "Unknown endpoint")
		return
	}
	s.serveMailItem(w, r, service, kind, address)
}

func (s *stubAPI) mailList(service, kind string) []map[string]any {
	if kind == "forwarder" {
		return s.services[service].mailForwarders
	}
	return s.services[service].mailAccounts
}

func (s *stubAPI) setMailList(service, kind string, list []map[string]any) {
	if kind == "forwarder" {
		s.services[service].mailForwarders = list
	} else {
		s.services[service].mailAccounts = list
	}
}

func (s *stubAPI) serveMailList(w http.ResponseWriter, r *http.Request, service, kind string) {
	switch r.Method {
	case http.MethodGet:
		s.mu.Lock()
		items := append([]map[string]any(nil), s.mailList(service, kind)...)
		s.mu.Unlock()
		s.respondPaged(w, r, items)

	case http.MethodPost:
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			s.fail(w, http.StatusBadRequest, "Malformed body")
			return
		}
		address, _ := payload["address"].(string)
		if !strings.Contains(address, "@") {
			s.failValidation(w, map[string][]string{"address": {"is not a valid address"}})
			return
		}
		if password, ok := payload["password"].(string); ok && password != "" {
			s.mu.Lock()
			s.lastPassword = password
			s.mu.Unlock()
		}

		s.mu.Lock()
		stored := newMailObject(service, kind, address)
		applyMailPayload(stored, payload)
		s.setMailList(service, kind, append(s.mailList(service, kind), stored))
		s.mu.Unlock()

		s.respond(w, http.StatusCreated, []map[string]any{stored})

	default:
		s.fail(w, http.StatusNotFound, "Unknown endpoint")
	}
}

func newMailObject(service, kind, address string) map[string]any {
	object := map[string]any{
		"address":           address,
		"comment":           "",
		"autoreply":         false,
		"fwd_addresses":     []any{},
		"disabled_features": []any{},
		"resource_url":      "https://api.zone.eu/v2/vserver/" + service + "/mail/" + kind + "/" + address,
	}
	if kind == "account" {
		object["spamlevel"] = "medium"
		object["two_factor_auth"] = false
		object["disk_size"] = 8589934592
		object["disk_usage"] = 0
		object["deleted_at"] = nil
	} else {
		object["mail_to_http_url"] = ""
	}
	return object
}

func applyMailPayload(stored, payload map[string]any) {
	for _, field := range []string{"comment", "spamlevel", "address"} {
		if value, ok := payload[field]; ok {
			stored[field] = value
		}
	}
	if value, ok := payload["fwd_addresses"]; ok {
		if list, isList := value.([]any); isList {
			stored["fwd_addresses"] = list
		}
	}
}

func (s *stubAPI) findMail(service, kind, address string) map[string]any {
	for _, item := range s.mailList(service, kind) {
		if stored, _ := item["address"].(string); strings.EqualFold(stored, address) {
			return item
		}
	}
	return nil
}

func (s *stubAPI) serveMailItem(w http.ResponseWriter, r *http.Request, service, kind, address string) {
	s.mu.Lock()
	found := s.findMail(service, kind, address)
	s.mu.Unlock()

	if found == nil {
		s.fail(w, http.StatusNotFound, "Address not found")
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
		if password, ok := payload["password"].(string); ok && password != "" {
			s.mu.Lock()
			s.lastPassword = password
			s.mu.Unlock()
		}

		s.mu.Lock()
		applyMailPayload(found, payload)
		s.mu.Unlock()
		s.respond(w, http.StatusOK, []map[string]any{found})

	case http.MethodDelete:
		s.mu.Lock()
		kept := make([]map[string]any, 0, len(s.mailList(service, kind)))
		for _, item := range s.mailList(service, kind) {
			if stored, _ := item["address"].(string); !strings.EqualFold(stored, address) {
				kept = append(kept, item)
			}
		}
		s.setMailList(service, kind, kept)
		s.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)

	default:
		s.fail(w, http.StatusNotFound, "Unknown endpoint")
	}
}

func (s *stubAPI) serveAutoreply(w http.ResponseWriter, r *http.Request, service, kind, address string) {
	key := kind + "/" + address

	s.mu.Lock()
	parent := s.findMail(service, kind, address)
	s.mu.Unlock()
	if parent == nil {
		s.fail(w, http.StatusNotFound, "Address not found")
		return
	}

	switch r.Method {
	case http.MethodGet:
		s.mu.Lock()
		stored, exists := s.services[service].autoreplies[key]
		s.mu.Unlock()
		if !exists {
			// An address with no autoreply still answers, with everything off.
			s.respond(w, http.StatusOK, []map[string]any{emptyAutoreply(service, kind, address)})
			return
		}
		s.respond(w, http.StatusOK, []map[string]any{stored})

	case http.MethodPut:
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			s.fail(w, http.StatusBadRequest, "Malformed body")
			return
		}

		stored := emptyAutoreply(service, kind, address)
		for _, field := range []string{"is_enabled", "fromname", "subject", "body", "datestart", "dateend"} {
			if value, ok := payload[field]; ok {
				stored[field] = value
			}
		}

		s.mu.Lock()
		s.services[service].autoreplies[key] = stored
		// The parent listing reports whether an autoreply is active, which is
		// why writing one has to invalidate the parent's cached listing too.
		parent["autoreply"], _ = stored["is_enabled"].(bool)
		s.mu.Unlock()

		s.respond(w, http.StatusOK, []map[string]any{stored})

	default:
		s.fail(w, http.StatusNotFound, "Unknown endpoint")
	}
}

func emptyAutoreply(service, kind, address string) map[string]any {
	return map[string]any{
		"is_enabled":   false,
		"fromname":     "",
		"subject":      "",
		"body":         "",
		"datestart":    nil,
		"dateend":      nil,
		"resource_url": "https://api.zone.eu/v2/vserver/" + service + "/mail/" + kind + "/" + address + "/autoreply",
	}
}

func (s *stubAPI) serveMySQL(w http.ResponseWriter, r *http.Request, service string, rest []string) {
	switch {
	case len(rest) == 0:
		s.serveDatabaseList(w, r, service)
	// The literal "account" has to win over a database named "account", which
	// is the same order the real API must use.
	case rest[0] == "account" && len(rest) == 1:
		s.serveDBAccountList(w, r, service)
	case rest[0] == "account" && len(rest) == 2:
		s.serveDBAccountItem(w, r, service, rest[1])
	case rest[0] == "account" && len(rest) == 3 && rest[2] == "permission":
		s.servePermissionList(w, r, service, rest[1])
	case rest[0] == "account" && len(rest) == 4 && rest[2] == "permission":
		s.servePermissionItem(w, r, service, rest[1], rest[3])
	case len(rest) == 1:
		s.serveDatabaseItem(w, r, service, rest[0])
	default:
		s.fail(w, http.StatusNotFound, "Unknown endpoint")
	}
}

func (s *stubAPI) serveDatabaseList(w http.ResponseWriter, r *http.Request, service string) {
	switch r.Method {
	case http.MethodGet:
		s.mu.Lock()
		items := append([]map[string]any(nil), s.services[service].databases...)
		s.mu.Unlock()
		s.respondPaged(w, r, items)

	case http.MethodPost:
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			s.fail(w, http.StatusBadRequest, "Malformed body")
			return
		}
		name, _ := payload["name"].(string)
		if name == "" {
			s.failValidation(w, map[string][]string{"name": {"is required"}})
			return
		}

		comment, _ := payload["comment"].(string)
		stored := map[string]any{
			"name":               name,
			"identificator":      name,
			"comment":            comment,
			"disk_usage":         "0",
			"disk_usage_human":   "0 B",
			"disk_usage_updated": "2026-09-18T00:00:00+03:00",
			"resource_url":       "https://api.zone.eu/v2/vserver/" + service + "/database/mysql/" + name,
		}
		// collation is accepted and never echoed back, as on the live API.

		s.mu.Lock()
		s.services[service].databases = append(s.services[service].databases, stored)
		s.mu.Unlock()
		s.respond(w, http.StatusCreated, []map[string]any{stored})

	default:
		s.fail(w, http.StatusNotFound, "Unknown endpoint")
	}
}

func (s *stubAPI) serveDatabaseItem(w http.ResponseWriter, r *http.Request, service, name string) {
	s.mu.Lock()
	var found map[string]any
	for _, database := range s.services[service].databases {
		if stored, _ := database["name"].(string); strings.EqualFold(stored, name) {
			found = database
			break
		}
	}
	s.mu.Unlock()

	if found == nil {
		s.fail(w, http.StatusNotFound, "Database not found")
		return
	}

	switch r.Method {
	case http.MethodGet:
		s.respond(w, http.StatusOK, []map[string]any{found})

	case http.MethodDelete:
		s.mu.Lock()
		kept := make([]map[string]any, 0, len(s.services[service].databases))
		for _, database := range s.services[service].databases {
			if stored, _ := database["name"].(string); !strings.EqualFold(stored, name) {
				kept = append(kept, database)
			}
		}
		s.services[service].databases = kept
		s.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)

	default:
		// There is no PUT for a database, and that is the whole reason every
		// argument on the resource forces replacement.
		s.fail(w, http.StatusNotFound, "Unknown endpoint")
	}
}

func (s *stubAPI) serveDBAccountList(w http.ResponseWriter, r *http.Request, service string) {
	switch r.Method {
	case http.MethodGet:
		s.mu.Lock()
		items := append([]map[string]any(nil), s.services[service].dbAccounts...)
		s.mu.Unlock()
		s.respondPaged(w, r, items)

	case http.MethodPost:
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			s.fail(w, http.StatusBadRequest, "Malformed body")
			return
		}
		username, _ := payload["username"].(string)
		if username == "" {
			s.failValidation(w, map[string][]string{"username": {"is required"}})
			return
		}
		if password, ok := payload["password"].(string); ok && password != "" {
			s.mu.Lock()
			s.lastPassword = password
			s.mu.Unlock()
		}

		stored := map[string]any{
			"username":      username,
			"identificator": username,
			"comment":       "",
			"require_ssl":   false,
			"hosts":         []any{},
			"resource_url":  "https://api.zone.eu/v2/vserver/" + service + "/database/mysql/account/" + username,
		}
		applyDBAccountPayload(stored, payload)

		s.mu.Lock()
		s.services[service].dbAccounts = append(s.services[service].dbAccounts, stored)
		s.mu.Unlock()
		s.respond(w, http.StatusCreated, []map[string]any{stored})

	default:
		s.fail(w, http.StatusNotFound, "Unknown endpoint")
	}
}

func applyDBAccountPayload(stored, payload map[string]any) {
	if value, ok := payload["comment"]; ok {
		stored["comment"] = value
	}
	if value, ok := payload["require_ssl"]; ok {
		stored["require_ssl"] = value
	}
	if value, ok := payload["hosts"]; ok {
		if list, isList := value.([]any); isList {
			stored["hosts"] = list
		}
	}
}

func (s *stubAPI) serveDBAccountItem(w http.ResponseWriter, r *http.Request, service, username string) {
	s.mu.Lock()
	var found map[string]any
	for _, account := range s.services[service].dbAccounts {
		if stored, _ := account["username"].(string); strings.EqualFold(stored, username) {
			found = account
			break
		}
	}
	s.mu.Unlock()

	if found == nil {
		s.fail(w, http.StatusNotFound, "Account not found")
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
		// The username is read-only on update, and the client does not send it.
		if _, sent := payload["username"]; sent {
			s.failValidation(w, map[string][]string{"username": {"is read-only in update mode"}})
			return
		}
		if password, ok := payload["password"].(string); ok && password != "" {
			s.mu.Lock()
			s.lastPassword = password
			s.mu.Unlock()
		}

		s.mu.Lock()
		applyDBAccountPayload(found, payload)
		s.mu.Unlock()
		s.respond(w, http.StatusOK, []map[string]any{found})

	case http.MethodDelete:
		s.mu.Lock()
		kept := make([]map[string]any, 0, len(s.services[service].dbAccounts))
		for _, account := range s.services[service].dbAccounts {
			if stored, _ := account["username"].(string); !strings.EqualFold(stored, username) {
				kept = append(kept, account)
			}
		}
		s.services[service].dbAccounts = kept
		delete(s.services[service].permissions, username)
		s.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)

	default:
		s.fail(w, http.StatusNotFound, "Unknown endpoint")
	}
}

func (s *stubAPI) servePermissionList(w http.ResponseWriter, r *http.Request, service, username string) {
	if r.Method != http.MethodGet {
		s.fail(w, http.StatusNotFound, "Unknown endpoint")
		return
	}

	s.mu.Lock()
	grants := append([]map[string]any(nil), s.services[service].permissions[username]...)
	s.mu.Unlock()
	s.respond(w, http.StatusOK, grants)
}

func (s *stubAPI) servePermissionItem(w http.ResponseWriter, r *http.Request, service, username, database string) {
	switch r.Method {
	case http.MethodGet:
		s.mu.Lock()
		defer s.mu.Unlock()
		for _, grant := range s.services[service].permissions[username] {
			if stored, _ := grant["database"].(string); strings.EqualFold(stored, database) {
				s.respond(w, http.StatusOK, []map[string]any{grant})
				return
			}
		}
		// A grant that does not exist reads as an empty envelope, which the
		// client turns into a not-found.
		s.respond(w, http.StatusOK, []map[string]any{})

	case http.MethodPut:
		var payload struct {
			Permissions []string `json:"permissions"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			s.fail(w, http.StatusBadRequest, "Malformed body")
			return
		}
		if len(payload.Permissions) == 0 {
			s.failValidation(w, map[string][]string{"permissions": {"is required"}})
			return
		}

		granted := make([]any, 0, len(payload.Permissions))
		for _, permission := range payload.Permissions {
			granted = append(granted, permission)
		}
		stored := map[string]any{
			"username":     username,
			"database":     database,
			"permissions":  granted,
			"resource_url": "https://api.zone.eu/v2/vserver/" + service + "/database/mysql/account/" + username + "/permission/" + database,
		}

		s.mu.Lock()
		kept := make([]map[string]any, 0, len(s.services[service].permissions[username])+1)
		for _, grant := range s.services[service].permissions[username] {
			if existing, _ := grant["database"].(string); !strings.EqualFold(existing, database) {
				kept = append(kept, grant)
			}
		}
		s.services[service].permissions[username] = append(kept, stored)
		s.mu.Unlock()

		s.respond(w, http.StatusOK, []map[string]any{stored})

	case http.MethodDelete:
		s.mu.Lock()
		kept := make([]map[string]any, 0, len(s.services[service].permissions[username]))
		for _, grant := range s.services[service].permissions[username] {
			if existing, _ := grant["database"].(string); !strings.EqualFold(existing, database) {
				kept = append(kept, grant)
			}
		}
		s.services[service].permissions[username] = kept
		s.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)

	default:
		s.fail(w, http.StatusNotFound, "Unknown endpoint")
	}
}

func (s *stubAPI) serveSSL(w http.ResponseWriter, r *http.Request, service string, rest []string) {
	switch {
	case len(rest) == 0:
		s.serveCertificateList(w, r, service)
	case len(rest) == 1:
		s.serveCertificateItem(w, r, service, rest[0])
	default:
		s.fail(w, http.StatusNotFound, "Unknown endpoint")
	}
}

func (s *stubAPI) serveCertificateList(w http.ResponseWriter, r *http.Request, service string) {
	switch r.Method {
	case http.MethodGet:
		s.mu.Lock()
		items := append([]map[string]any(nil), s.services[service].certificates...)
		s.mu.Unlock()
		// /ssl paginates on the wire although its schema declares no pager.
		s.respondPaged(w, r, items)

	case http.MethodPost:
		payload, ok := s.readCertificate(w, r)
		if !ok {
			return
		}

		s.mu.Lock()
		s.nextID++
		id := strconv.FormatInt(s.nextID, 10)
		stored := certificateBody(service, id, payload)
		s.services[service].certificates = append(s.services[service].certificates, stored)
		s.mu.Unlock()

		s.respond(w, http.StatusCreated, []map[string]any{stored})

	default:
		s.fail(w, http.StatusNotFound, "Unknown endpoint")
	}
}

func (s *stubAPI) serveCertificateItem(w http.ResponseWriter, r *http.Request, service, id string) {
	s.mu.Lock()
	index := -1
	for i, certificate := range s.services[service].certificates {
		if stored, _ := certificate["id"].(string); stored == id {
			index = i
			break
		}
	}
	s.mu.Unlock()

	if index < 0 {
		s.fail(w, http.StatusNotFound, "Certificate not found")
		return
	}

	switch r.Method {
	case http.MethodGet:
		s.mu.Lock()
		found := s.services[service].certificates[index]
		s.mu.Unlock()
		s.respond(w, http.StatusOK, []map[string]any{found})

	case http.MethodPut:
		payload, ok := s.readCertificate(w, r)
		if !ok {
			return
		}
		s.mu.Lock()
		stored := certificateBody(service, id, payload)
		s.services[service].certificates[index] = stored
		s.mu.Unlock()
		s.respond(w, http.StatusOK, []map[string]any{stored})

	case http.MethodDelete:
		s.mu.Lock()
		certificates := s.services[service].certificates
		s.services[service].certificates = append(certificates[:index:index], certificates[index+1:]...)
		s.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)

	default:
		s.fail(w, http.StatusNotFound, "Unknown endpoint")
	}
}

type stubCertificatePayload struct {
	Name          string   `json:"name"`
	Certificate   string   `json:"certificate"`
	PrivateKey    string   `json:"private_key"`
	CACertificate string   `json:"ca_certificate"`
	Hosts         []string `json:"hosts"`
}

// readCertificate decodes a certificate payload and records the key, which is
// how a test proves the key was sent despite never appearing in state.
func (s *stubAPI) readCertificate(w http.ResponseWriter, r *http.Request) (stubCertificatePayload, bool) {
	var payload stubCertificatePayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		s.fail(w, http.StatusBadRequest, "Malformed body")
		return payload, false
	}
	if payload.Name == "" || payload.Certificate == "" {
		s.failValidation(w, map[string][]string{"certificate": {"is required"}})
		return payload, false
	}
	if payload.PrivateKey == "" {
		// Catching this in the stub is the point: a provider that read the key
		// from the plan rather than the config would send nothing at all.
		s.failValidation(w, map[string][]string{"private_key": {"is required"}})
		return payload, false
	}

	s.mu.Lock()
	s.lastPrivateKey = payload.PrivateKey
	s.mu.Unlock()
	return payload, true
}

func certificateBody(service, id string, payload stubCertificatePayload) map[string]any {
	hosts := make([]any, 0, len(payload.Hosts))
	for _, host := range payload.Hosts {
		hosts = append(hosts, host)
	}

	return map[string]any{
		"id":             id,
		"identificator":  id,
		"name":           payload.Name,
		"cn":             "example.com",
		"certificate":    payload.Certificate,
		"ca_certificate": payload.CACertificate,
		// The live API returns the key as an empty string, never its value.
		"private_key":  "",
		"letsencrypt":  false,
		"connected":    false,
		"created":      "2026-09-18T00:00:00+03:00",
		"expires":      "2027-09-18T00:00:00+03:00",
		"hosts":        hosts,
		"resource_url": "https://api.zone.eu/v2/vserver/" + service + "/ssl/" + id,
	}
}
