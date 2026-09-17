package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// stubAPI is an in-memory stand-in for the zone.eu API.
//
// It reproduces the quirks the real API actually exhibits, because those are
// what the provider has to survive: every response is an array; most endpoints
// render the record id as a quoted string while SRV renders it as a number;
// comment comes back null; and records carry modify/delete capability flags.
// Those behaviours were confirmed against the live API, not guessed.
type stubAPI struct {
	mu      sync.Mutex
	zones   map[string]*stubZone
	records map[string][]map[string]any
	nextID  int64

	// Requests counts served requests, so tests can assert that a refresh costs
	// one listing per record type rather than one per record.
	Requests atomic.Int64
}

type stubZone struct {
	Active bool
	IPv6   bool
	DNSSEC bool
}

func newStubAPI() *stubAPI {
	return &stubAPI{
		zones: map[string]*stubZone{
			"example.com": {Active: true, IPv6: false, DNSSEC: true},
		},
		records: make(map[string][]map[string]any),
		nextID:  100000,
	}
}

// Start serves the stub and returns its base URL.
func (s *stubAPI) Start(t *testing.T) string {
	t.Helper()
	server := httptest.NewServer(s)
	t.Cleanup(server.Close)
	return server.URL
}

// SeedRecord inserts a record directly, for setting up state the provider did
// not create — a zone.eu-managed record, say, or drift.
func (s *stubAPI) SeedRecord(zone, recordType string, record map[string]any) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.nextID++
	id := s.nextID
	stored := map[string]any{
		"id":     strconv.FormatInt(id, 10),
		"modify": true,
		"delete": true,
	}
	for key, value := range record {
		stored[key] = value
	}
	stored["resource_url"] = fmt.Sprintf("https://api.zone.eu/v2/dns/%s/%s/%d", zone, recordType, id)
	stored["comment"] = nil

	key := zone + "/" + recordType
	s.records[key] = append(s.records[key], stored)
	return id
}

// RemoveRecord deletes a record behind the provider's back, simulating someone
// editing the zone in the ZoneID panel.
func (s *stubAPI) RemoveRecord(zone, recordType string, id int64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := zone + "/" + recordType
	kept := make([]map[string]any, 0, len(s.records[key]))
	for _, record := range s.records[key] {
		if recordID(record) != id {
			kept = append(kept, record)
		}
	}
	s.records[key] = kept
}

// RecordIDs returns the ids of a zone's records of one type.
func (s *stubAPI) RecordIDs(zone, recordType string) []int64 {
	s.mu.Lock()
	defer s.mu.Unlock()

	records := s.records[zone+"/"+recordType]
	ids := make([]int64, 0, len(records))
	for _, record := range records {
		ids = append(ids, recordID(record))
	}
	return ids
}

// SetFlags changes a record's modify/delete capability flags, so a test can
// lock a record and later unlock it for cleanup.
func (s *stubAPI) SetFlags(zone, recordType string, id int64, modify, deletable bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, record := range s.records[zone+"/"+recordType] {
		if recordID(record) == id {
			record["modify"] = modify
			record["delete"] = deletable
		}
	}
}

// CountRecords reports how many records of a type the zone holds.
func (s *stubAPI) CountRecords(zone, recordType string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.records[zone+"/"+recordType])
}

func recordID(record map[string]any) int64 {
	switch value := record["id"].(type) {
	case string:
		id, _ := strconv.ParseInt(value, 10, 64)
		return id
	case int64:
		return value
	case float64:
		return int64(value)
	case json.Number:
		id, _ := value.Int64()
		return id
	}
	return 0
}

func (s *stubAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.Requests.Add(1)

	if username, token, ok := r.BasicAuth(); !ok || username == "" || token == "" {
		s.fail(w, http.StatusUnauthorized, "Authentication required")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-RateLimit-Limit", "60")
	w.Header().Set("X-RateLimit-Remaining", "59")

	segments := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(segments) < 2 || segments[0] != "dns" {
		s.fail(w, http.StatusNotFound, "Unknown endpoint")
		return
	}

	zone := segments[1]
	s.mu.Lock()
	zoneState, zoneExists := s.zones[zone]
	s.mu.Unlock()
	if !zoneExists {
		s.fail(w, http.StatusNotFound, "Zone not found")
		return
	}

	switch len(segments) {
	case 2:
		s.serveZone(w, r, zoneState)
	case 3:
		s.serveCollection(w, r, zone, segments[2])
	case 4:
		id, err := strconv.ParseInt(segments[3], 10, 64)
		if err != nil {
			s.fail(w, http.StatusBadRequest, "Invalid identifier")
			return
		}
		s.serveItem(w, r, zone, segments[2], id)
	default:
		s.fail(w, http.StatusNotFound, "Unknown endpoint")
	}
}

func (s *stubAPI) serveZone(w http.ResponseWriter, r *http.Request, zone *stubZone) {
	if r.Method == http.MethodPut {
		var payload struct {
			Active *bool `json:"active"`
			IPv6   *bool `json:"ipv6"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			s.fail(w, http.StatusBadRequest, "Malformed body")
			return
		}
		s.mu.Lock()
		if payload.Active != nil {
			zone.Active = *payload.Active
		}
		if payload.IPv6 != nil {
			zone.IPv6 = *payload.IPv6
		}
		s.mu.Unlock()
	}

	s.mu.Lock()
	body := map[string]any{
		"resource_url":  "https://api.zone.eu/v2/dns/example.com",
		"identificator": "example.com",
		"active":        zone.Active,
		"ipv6":          zone.IPv6,
		"domain":        true,
		"dnssec":        zone.DNSSEC,
	}
	s.mu.Unlock()

	s.respond(w, http.StatusOK, []map[string]any{body})
}

func (s *stubAPI) serveCollection(w http.ResponseWriter, r *http.Request, zone, recordType string) {
	key := zone + "/" + recordType

	switch r.Method {
	case http.MethodGet:
		s.mu.Lock()
		defer s.mu.Unlock()
		records := s.records[key]
		if records == nil {
			records = []map[string]any{}
		}
		s.respond(w, http.StatusOK, records)

	case http.MethodPost:
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			s.fail(w, http.StatusBadRequest, "Malformed body")
			return
		}
		if name, _ := payload["name"].(string); !strings.HasSuffix(name, zone) {
			s.failValidation(w, map[string][]string{"name": {"must be a name within the zone"}})
			return
		}

		s.mu.Lock()
		s.nextID++
		id := s.nextID
		stored := map[string]any{}
		for field, value := range payload {
			stored[field] = value
		}
		// SRV is the one type the live API renders the id as a number for.
		if recordType == "srv" {
			stored["id"] = id
		} else {
			stored["id"] = strconv.FormatInt(id, 10)
		}
		stored["resource_url"] = fmt.Sprintf("https://api.zone.eu/v2/dns/%s/%s/%d", zone, recordType, id)
		stored["comment"] = nil
		stored["modify"] = true
		stored["delete"] = true
		s.records[key] = append(s.records[key], stored)
		result := cloneRecord(stored)
		s.mu.Unlock()

		s.respond(w, http.StatusCreated, []map[string]any{result})

	default:
		s.fail(w, http.StatusBadRequest, "Operation not supported")
	}
}

func (s *stubAPI) serveItem(w http.ResponseWriter, r *http.Request, zone, recordType string, id int64) {
	key := zone + "/" + recordType

	s.mu.Lock()
	index := -1
	for i, record := range s.records[key] {
		if recordID(record) == id {
			index = i
			break
		}
	}
	if index < 0 {
		s.mu.Unlock()
		s.fail(w, http.StatusNotFound, "Record not found")
		return
	}
	existing := s.records[key][index]
	s.mu.Unlock()

	switch r.Method {
	case http.MethodGet:
		s.respond(w, http.StatusOK, []map[string]any{cloneRecord(existing)})

	case http.MethodPut:
		if modify, ok := existing["modify"].(bool); ok && !modify {
			s.fail(w, http.StatusBadRequest, "Operation not supported")
			return
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			s.fail(w, http.StatusBadRequest, "Malformed body")
			return
		}

		s.mu.Lock()
		for field, value := range payload {
			existing[field] = value
		}
		result := cloneRecord(existing)
		s.mu.Unlock()

		s.respond(w, http.StatusOK, []map[string]any{result})

	case http.MethodDelete:
		if deletable, ok := existing["delete"].(bool); ok && !deletable {
			s.fail(w, http.StatusBadRequest, "Operation not supported")
			return
		}
		s.mu.Lock()
		s.records[key] = append(s.records[key][:index], s.records[key][index+1:]...)
		s.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)

	default:
		s.fail(w, http.StatusBadRequest, "Operation not supported")
	}
}

func cloneRecord(record map[string]any) map[string]any {
	clone := make(map[string]any, len(record))
	for key, value := range record {
		clone[key] = value
	}
	return clone
}

func (s *stubAPI) respond(w http.ResponseWriter, status int, body any) {
	w.Header().Set("X-Status-Message", "OK")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func (s *stubAPI) fail(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Status-Message", message)
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"messages": []string{message}})
}

func (s *stubAPI) failValidation(w http.ResponseWriter, fields map[string][]string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Status-Message", "Validation failed")
	w.WriteHeader(http.StatusUnprocessableEntity)
	_ = json.NewEncoder(w).Encode(fields)
}
