package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// The SSH, FTP and crontab routes of the stub.
//
// Quirks reproduced from the live API: the SSH key and whitelist listings do
// not paginate while the FTP ones do; server_fingerprints is an object keyed by
// algorithm rather than the array its schema claims; an SSH key's size is a
// quoted number on RSA and null on Ed25519; and the crontab endpoint uses
// exec_type, nice and schedule_type rather than the names its schema uses.

// SeedSSHPublicKey authorises a key directly.
func (s *stubAPI) SeedSSHPublicKey(service, publicKey, comment string) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.nextID++
	id := s.nextID
	s.services[service].publicKeys = append(s.services[service].publicKeys, sshKeyBody(service, id, publicKey, comment))
	return id
}

// CountSSHPublicKeys reports how many keys a service has authorised.
func (s *stubAPI) CountSSHPublicKeys(service string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.services[service].publicKeys)
}

// SSHAccess reports the service's current access mode.
func (s *stubAPI) SSHAccess(service string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.services[service].sshAccess
}

// CountCrontabs reports how many jobs a service has scheduled.
func (s *stubAPI) CountCrontabs(service string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.services[service].crontabs)
}

// LastCrontabPayload returns the body of the last crontab write, so a test can
// pin which field names went on the wire — the one thing about this endpoint
// that could not be settled against the live API.
func (s *stubAPI) LastCrontabPayload() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastCrontabPayload
}

func sshKeyBody(service string, id int64, publicKey, comment string) map[string]any {
	body := map[string]any{
		"identificator": id,
		"public_key":    publicKey,
		"comment":       comment,
		"fingerprint":   fmt.Sprintf("SHA256:stub%d", id),
		"created":       "2026-09-18 00:00:00",
		"created_human": "just now",
		"last_used":     nil,
		"resource_url":  fmt.Sprintf("https://api.zone.eu/v2/vserver/%s/ssh/publickey/%d", service, id),
	}

	// Type and size are derived from the key, and size is a quoted number on
	// RSA and null on Ed25519.
	switch {
	case strings.HasPrefix(publicKey, "ssh-rsa"):
		body["type"] = "rsa"
		body["size"] = "2048"
	default:
		body["type"] = "ed25519"
		body["size"] = nil
	}
	return body
}

func (s *stubAPI) serveSSH(w http.ResponseWriter, r *http.Request, service string, rest []string) {
	switch {
	case len(rest) == 0:
		s.serveSSHSettings(w, r, service)
	case rest[0] == "publickey" && len(rest) == 1:
		s.serveSSHKeyList(w, r, service)
	case rest[0] == "publickey" && len(rest) == 2:
		s.serveSSHKeyItem(w, r, service, rest[1])
	case rest[0] == "whitelist" && len(rest) == 1:
		s.serveSSHWhitelistList(w, r, service)
	case rest[0] == "whitelist" && len(rest) == 2:
		s.serveSSHWhitelistItem(w, r, service, rest[1])
	default:
		s.fail(w, http.StatusNotFound, "Unknown endpoint")
	}
}

func (s *stubAPI) serveSSHSettings(w http.ResponseWriter, r *http.Request, service string) {
	switch r.Method {
	case http.MethodGet:
	case http.MethodPut:
		var payload struct {
			Access string `json:"access"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			s.fail(w, http.StatusBadRequest, "Malformed body")
			return
		}
		if payload.Access != "public" && payload.Access != "whitelist" {
			s.failValidation(w, map[string][]string{"access": {"is not a valid value"}})
			return
		}
		s.mu.Lock()
		s.services[service].sshAccess = payload.Access
		s.mu.Unlock()
	default:
		s.fail(w, http.StatusNotFound, "Unknown endpoint")
		return
	}

	s.mu.Lock()
	access := s.services[service].sshAccess
	s.mu.Unlock()

	s.respond(w, http.StatusOK, []map[string]any{{
		"identificator": 1,
		"username":      "virt10001",
		"access":        access,
		"ipv4":          "195.43.87.10",
		"ipv6":          nil,
		"webhosts":      []any{service},
		// An object keyed by algorithm, not the array the schema declares.
		"server_fingerprints": map[string]any{
			"RSA":     "SHA256:rsafingerprint",
			"ECDSA":   "SHA256:ecdsafingerprint",
			"ED25519": "SHA256:ed25519fingerprint",
		},
		"resource_url": "https://api.zone.eu/v2/vserver/" + service + "/ssh",
	}})
}

func (s *stubAPI) serveSSHKeyList(w http.ResponseWriter, r *http.Request, service string) {
	switch r.Method {
	case http.MethodGet:
		s.mu.Lock()
		defer s.mu.Unlock()
		keys := s.services[service].publicKeys
		if keys == nil {
			keys = []map[string]any{}
		}
		// Not paginated on the live API, so no pager headers here either.
		s.respond(w, http.StatusOK, keys)

	case http.MethodPost:
		var payload struct {
			PublicKey string `json:"public_key"`
			Comment   string `json:"comment"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			s.fail(w, http.StatusBadRequest, "Malformed body")
			return
		}
		if len(strings.Fields(payload.PublicKey)) < 2 {
			s.failValidation(w, map[string][]string{"public_key": {"is not a valid key"}})
			return
		}

		s.mu.Lock()
		s.nextID++
		body := sshKeyBody(service, s.nextID, payload.PublicKey, payload.Comment)
		s.services[service].publicKeys = append(s.services[service].publicKeys, body)
		s.mu.Unlock()

		s.respond(w, http.StatusCreated, []map[string]any{body})

	default:
		s.fail(w, http.StatusNotFound, "Unknown endpoint")
	}
}

func (s *stubAPI) serveSSHKeyItem(w http.ResponseWriter, r *http.Request, service, rawID string) {
	id, err := strconv.ParseInt(rawID, 10, 64)
	if err != nil {
		s.fail(w, http.StatusBadRequest, "Invalid identifier")
		return
	}

	s.mu.Lock()
	index := indexByIdentificator(s.services[service].publicKeys, id)
	s.mu.Unlock()
	if index < 0 {
		s.fail(w, http.StatusNotFound, "Key not found")
		return
	}

	switch r.Method {
	case http.MethodGet:
		s.mu.Lock()
		body := s.services[service].publicKeys[index]
		s.mu.Unlock()
		s.respond(w, http.StatusOK, []map[string]any{body})

	case http.MethodDelete:
		s.mu.Lock()
		keys := s.services[service].publicKeys
		s.services[service].publicKeys = append(keys[:index:index], keys[index+1:]...)
		s.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)

	default:
		// No PUT: that is why every argument on the resource replaces.
		s.fail(w, http.StatusNotFound, "Unknown endpoint")
	}
}

func (s *stubAPI) serveSSHWhitelistList(w http.ResponseWriter, r *http.Request, service string) {
	switch r.Method {
	case http.MethodGet:
		s.mu.Lock()
		defer s.mu.Unlock()
		entries := s.services[service].sshWhitelist
		if entries == nil {
			entries = []map[string]any{}
		}
		s.respond(w, http.StatusOK, entries)

	case http.MethodPost:
		var payload struct {
			IP      string `json:"ip"`
			Comment string `json:"comment"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			s.fail(w, http.StatusBadRequest, "Malformed body")
			return
		}
		if payload.IP == "" {
			s.failValidation(w, map[string][]string{"ip": {"is required"}})
			return
		}

		s.mu.Lock()
		s.nextID++
		body := map[string]any{
			"identificator": s.nextID,
			"ip":            payload.IP,
			"comment":       payload.Comment,
			"resource_url":  fmt.Sprintf("https://api.zone.eu/v2/vserver/%s/ssh/whitelist/%d", service, s.nextID),
		}
		s.services[service].sshWhitelist = append(s.services[service].sshWhitelist, body)
		s.mu.Unlock()

		s.respond(w, http.StatusCreated, []map[string]any{body})

	default:
		s.fail(w, http.StatusNotFound, "Unknown endpoint")
	}
}

func (s *stubAPI) serveSSHWhitelistItem(w http.ResponseWriter, r *http.Request, service, rawID string) {
	id, err := strconv.ParseInt(rawID, 10, 64)
	if err != nil {
		s.fail(w, http.StatusBadRequest, "Invalid identifier")
		return
	}

	s.mu.Lock()
	index := indexByIdentificator(s.services[service].sshWhitelist, id)
	s.mu.Unlock()
	if index < 0 {
		s.fail(w, http.StatusNotFound, "Entry not found")
		return
	}

	switch r.Method {
	case http.MethodGet:
		s.mu.Lock()
		body := s.services[service].sshWhitelist[index]
		s.mu.Unlock()
		s.respond(w, http.StatusOK, []map[string]any{body})

	case http.MethodDelete:
		s.mu.Lock()
		entries := s.services[service].sshWhitelist
		s.services[service].sshWhitelist = append(entries[:index:index], entries[index+1:]...)
		s.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)

	default:
		s.fail(w, http.StatusNotFound, "Unknown endpoint")
	}
}

func (s *stubAPI) serveFTP(w http.ResponseWriter, r *http.Request, service string, rest []string) {
	switch {
	case rest[0] == "user" && len(rest) == 1:
		s.serveFTPUserList(w, r, service)
	case rest[0] == "user" && len(rest) == 2:
		s.serveFTPUserItem(w, r, service, rest[1])
	case rest[0] == "ipwhitelist" && len(rest) == 1:
		s.serveFTPWhitelistList(w, r, service)
	case rest[0] == "ipwhitelist" && len(rest) == 2:
		s.serveFTPWhitelistItem(w, r, service, rest[1])
	default:
		s.fail(w, http.StatusNotFound, "Unknown endpoint")
	}
}

func ftpUserBody(service string, id int64, payload map[string]any) map[string]any {
	body := map[string]any{
		"identificator":      id,
		"username":           "",
		"username_system":    fmt.Sprintf("virt10001_%d", id),
		"directory":          "/data01/virt10001",
		"require_tls":        true,
		"access_profile":     "whitelist_or_tls",
		"allowed_operations": []any{"allow_download", "allow_list"},
		"access_countries":   []any{"EE"},
		"resource_url":       fmt.Sprintf("https://api.zone.eu/v2/vserver/%s/ftp/user/%d", service, id),
	}
	applyFTPUserPayload(body, payload)
	return body
}

func applyFTPUserPayload(body, payload map[string]any) {
	for _, field := range []string{"username", "directory", "require_tls", "access_profile"} {
		if value, ok := payload[field]; ok {
			body[field] = value
		}
	}
	for _, field := range []string{"allowed_operations", "access_countries"} {
		if value, ok := payload[field]; ok {
			if list, isList := value.([]any); isList {
				body[field] = list
			}
		}
	}
}

func (s *stubAPI) serveFTPUserList(w http.ResponseWriter, r *http.Request, service string) {
	switch r.Method {
	case http.MethodGet:
		s.mu.Lock()
		items := append([]map[string]any(nil), s.services[service].ftpUsers...)
		s.mu.Unlock()
		s.respondPaged(w, r, items)

	case http.MethodPost:
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
		s.nextID++
		body := ftpUserBody(service, s.nextID, payload)
		s.services[service].ftpUsers = append(s.services[service].ftpUsers, body)
		s.mu.Unlock()

		s.respond(w, http.StatusCreated, []map[string]any{body})

	default:
		s.fail(w, http.StatusNotFound, "Unknown endpoint")
	}
}

func (s *stubAPI) serveFTPUserItem(w http.ResponseWriter, r *http.Request, service, rawID string) {
	id, err := strconv.ParseInt(rawID, 10, 64)
	if err != nil {
		s.fail(w, http.StatusBadRequest, "Invalid identifier")
		return
	}

	s.mu.Lock()
	index := indexByIdentificator(s.services[service].ftpUsers, id)
	s.mu.Unlock()
	if index < 0 {
		s.fail(w, http.StatusNotFound, "Account not found")
		return
	}

	switch r.Method {
	case http.MethodGet:
		s.mu.Lock()
		body := s.services[service].ftpUsers[index]
		s.mu.Unlock()
		s.respond(w, http.StatusOK, []map[string]any{body})

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
		body := s.services[service].ftpUsers[index]
		applyFTPUserPayload(body, payload)
		s.mu.Unlock()
		s.respond(w, http.StatusOK, []map[string]any{body})

	case http.MethodDelete:
		s.mu.Lock()
		users := s.services[service].ftpUsers
		s.services[service].ftpUsers = append(users[:index:index], users[index+1:]...)
		s.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)

	default:
		s.fail(w, http.StatusNotFound, "Unknown endpoint")
	}
}

func (s *stubAPI) serveFTPWhitelistList(w http.ResponseWriter, r *http.Request, service string) {
	switch r.Method {
	case http.MethodGet:
		s.mu.Lock()
		items := append([]map[string]any(nil), s.services[service].ftpWhitelist...)
		s.mu.Unlock()
		s.respondPaged(w, r, items)

	case http.MethodPost:
		// The published schema marks every field of this object read-only while
		// still requiring a body. The provider sends the address; this accepts
		// it, which is the behaviour being assumed and flagged as unverified.
		var payload struct {
			IP string `json:"ip"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			s.fail(w, http.StatusBadRequest, "Malformed body")
			return
		}
		if payload.IP == "" {
			s.failValidation(w, map[string][]string{"ip": {"is required"}})
			return
		}

		s.mu.Lock()
		s.nextID++
		body := map[string]any{
			"identificator": s.nextID,
			"ip":            payload.IP,
			"country":       "EE",
			"created":       "2026-09-18T00:00:00+03:00",
			"resource_url":  fmt.Sprintf("https://api.zone.eu/v2/vserver/%s/ftp/ipwhitelist/%d", service, s.nextID),
		}
		s.services[service].ftpWhitelist = append(s.services[service].ftpWhitelist, body)
		s.mu.Unlock()

		s.respond(w, http.StatusCreated, []map[string]any{body})

	default:
		s.fail(w, http.StatusNotFound, "Unknown endpoint")
	}
}

func (s *stubAPI) serveFTPWhitelistItem(w http.ResponseWriter, r *http.Request, service, rawID string) {
	id, err := strconv.ParseInt(rawID, 10, 64)
	if err != nil {
		s.fail(w, http.StatusBadRequest, "Invalid identifier")
		return
	}

	s.mu.Lock()
	index := indexByIdentificator(s.services[service].ftpWhitelist, id)
	s.mu.Unlock()
	if index < 0 {
		s.fail(w, http.StatusNotFound, "Entry not found")
		return
	}

	switch r.Method {
	case http.MethodGet:
		s.mu.Lock()
		body := s.services[service].ftpWhitelist[index]
		s.mu.Unlock()
		s.respond(w, http.StatusOK, []map[string]any{body})

	case http.MethodDelete:
		s.mu.Lock()
		entries := s.services[service].ftpWhitelist
		s.services[service].ftpWhitelist = append(entries[:index:index], entries[index+1:]...)
		s.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)

	default:
		s.fail(w, http.StatusNotFound, "Unknown endpoint")
	}
}

func (s *stubAPI) serveCrontab(w http.ResponseWriter, r *http.Request, service string, rest []string) {
	switch {
	case len(rest) == 0:
		s.serveCrontabList(w, r, service)
	case len(rest) == 1:
		s.serveCrontabItem(w, r, service, rest[0])
	default:
		s.fail(w, http.StatusNotFound, "Unknown endpoint")
	}
}

// crontabBody echoes back the field names the live OPTIONS response reports,
// which is what the provider writes. A provider sending the published schema's
// names instead would fail the validation below.
func crontabBody(service string, id int64, payload map[string]any) map[string]any {
	body := map[string]any{
		"identificator": id,
		"resource_url":  fmt.Sprintf("https://api.zone.eu/v2/vserver/%s/crontab/%d", service, id),
	}
	for _, field := range []string{
		"name", "command", "schedule", "exec_type", "schedule_type",
		"report", "report_email", "nice", "timezone", "runtime_limit", "active",
	} {
		if value, ok := payload[field]; ok {
			body[field] = value
		}
	}
	return body
}

func (s *stubAPI) serveCrontabList(w http.ResponseWriter, r *http.Request, service string) {
	switch r.Method {
	case http.MethodGet:
		s.mu.Lock()
		defer s.mu.Unlock()
		jobs := s.services[service].crontabs
		if jobs == nil {
			jobs = []map[string]any{}
		}
		// Not paginated on the live API.
		s.respond(w, http.StatusOK, jobs)

	case http.MethodPost:
		payload, ok := s.readCrontab(w, r)
		if !ok {
			return
		}

		s.mu.Lock()
		s.nextID++
		body := crontabBody(service, s.nextID, payload)
		s.services[service].crontabs = append(s.services[service].crontabs, body)
		s.mu.Unlock()

		s.respond(w, http.StatusCreated, []map[string]any{body})

	default:
		s.fail(w, http.StatusNotFound, "Unknown endpoint")
	}
}

// readCrontab validates a crontab payload the way the live API's own enums
// imply, and records it so a test can pin the field names sent.
func (s *stubAPI) readCrontab(w http.ResponseWriter, r *http.Request) (map[string]any, bool) {
	var payload map[string]any
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		s.fail(w, http.StatusBadRequest, "Malformed body")
		return nil, false
	}

	s.mu.Lock()
	s.lastCrontabPayload = payload
	s.mu.Unlock()

	execType, _ := payload["exec_type"].(string)
	if execType != "http" && execType != "system" {
		s.failValidation(w, map[string][]string{"exec_type": {"is required"}})
		return nil, false
	}
	// Both are only accepted on a system job.
	if execType != "system" {
		for _, field := range []string{"timezone", "runtime_limit"} {
			if _, sent := payload[field]; sent {
				s.failValidation(w, map[string][]string{field: {"is only available for system crontabs"}})
				return nil, false
			}
		}
	}
	return payload, true
}

func (s *stubAPI) serveCrontabItem(w http.ResponseWriter, r *http.Request, service, rawID string) {
	id, err := strconv.ParseInt(rawID, 10, 64)
	if err != nil {
		s.fail(w, http.StatusBadRequest, "Invalid identifier")
		return
	}

	s.mu.Lock()
	index := indexByIdentificator(s.services[service].crontabs, id)
	s.mu.Unlock()
	if index < 0 {
		s.fail(w, http.StatusNotFound, "Crontab not found")
		return
	}

	switch r.Method {
	case http.MethodGet:
		s.mu.Lock()
		body := s.services[service].crontabs[index]
		s.mu.Unlock()
		s.respond(w, http.StatusOK, []map[string]any{body})

	case http.MethodPut:
		payload, ok := s.readCrontab(w, r)
		if !ok {
			return
		}
		s.mu.Lock()
		body := crontabBody(service, id, payload)
		s.services[service].crontabs[index] = body
		s.mu.Unlock()
		s.respond(w, http.StatusOK, []map[string]any{body})

	case http.MethodDelete:
		s.mu.Lock()
		jobs := s.services[service].crontabs
		s.services[service].crontabs = append(jobs[:index:index], jobs[index+1:]...)
		s.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)

	default:
		s.fail(w, http.StatusNotFound, "Unknown endpoint")
	}
}

// indexByIdentificator finds an object by its numeric identificator, which
// arrives as a bare integer on these endpoints.
func indexByIdentificator(items []map[string]any, id int64) int {
	for i, item := range items {
		switch value := item["identificator"].(type) {
		case int64:
			if value == id {
				return i
			}
		case int:
			if int64(value) == id {
				return i
			}
		case float64:
			if int64(value) == id {
				return i
			}
		}
	}
	return -1
}
