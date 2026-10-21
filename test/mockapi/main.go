// mockapi is an in-memory stand-in for the parts of the Hostinger API the
// Terraform provider calls. It exists so the provider can be tested end to
// end without an account, and without buying a VPS.
//
//	go run ./test/mockapi -addr 127.0.0.1:8085 -token test-token
//
// GET /_requests returns the requests received so far, for assertions.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
)

type publicKey struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Key  string `json:"key"`
}

type script struct {
	ID      int    `json:"id"`
	Name    string `json:"name"`
	Content string `json:"content"`
}

type ip struct {
	Address string `json:"address"`
}

type vm struct {
	ID             int    `json:"id"`
	SubscriptionID string `json:"subscription_id"`
	Hostname       string `json:"hostname"`
	State          string `json:"state"`
	IPv4           []ip   `json:"ipv4"`
	IPv6           []ip   `json:"ipv6"`
	Plan           string `json:"plan,omitempty"`
	DataCenterID   int    `json:"data_center_id,omitempty"`
	TemplateID     int    `json:"template_id,omitempty"`
	keys           map[int]bool
}

type dnsRecord struct {
	Content    string `json:"content"`
	IsDisabled bool   `json:"is_disabled"`
}

type dnsEntry struct {
	Name    string      `json:"name"`
	Type    string      `json:"type"`
	TTL     int         `json:"ttl"`
	Records []dnsRecord `json:"records"`
}

type server struct {
	token string

	mu       sync.Mutex
	nextID   int
	keys     map[int]*publicKey
	scripts  map[int]*script
	vms      map[int]*vm
	zones    map[string][]dnsEntry
	requests []string
}

const (
	plan       = "hostingercom-vps-kvm2-usd-1m"
	templateID = 1002
	dataCenter = 13
)

func newServer(token string) *server {
	return &server{
		token: token, nextID: 1000,
		keys: map[int]*publicKey{}, scripts: map[int]*script{}, vms: map[int]*vm{},
		zones: map[string][]dnsEntry{"example.com": {}},
	}
}

func (s *server) id() int {
	s.nextID++
	return s.nextID
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func notFound(w http.ResponseWriter) {
	writeJSON(w, http.StatusNotFound, map[string]string{"message": "Not found"})
}

func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/_requests" {
		s.mu.Lock()
		defer s.mu.Unlock()
		writeJSON(w, http.StatusOK, s.requests)
		return
	}
	if r.Header.Get("Authorization") != "Bearer "+s.token {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"message": "Unauthenticated."})
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests = append(s.requests, r.Method+" "+r.URL.Path)
	log.Printf("%s %s", r.Method, r.URL.Path)

	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	switch {
	case match(parts, "api", "billing", "v1", "catalog"):
		writeJSON(w, http.StatusOK, []any{map[string]any{"name": "KVM 2", "prices": []any{map[string]any{"id": plan}}}})
	case match(parts, "api", "vps", "v1", "templates"):
		writeJSON(w, http.StatusOK, []any{map[string]any{"id": templateID, "name": "Debian 12"}})
	case match(parts, "api", "vps", "v1", "data-centers"):
		writeJSON(w, http.StatusOK, []any{map[string]any{"id": dataCenter, "name": "fr-par"}})

	case match(parts, "api", "vps", "v1", "public-keys"):
		switch r.Method {
		case http.MethodGet:
			var data []*publicKey
			for _, k := range s.keys {
				data = append(data, k)
			}
			writeJSON(w, http.StatusOK, map[string]any{"data": data})
		case http.MethodPost:
			var k publicKey
			_ = json.NewDecoder(r.Body).Decode(&k)
			k.ID = s.id()
			s.keys[k.ID] = &k
			writeJSON(w, http.StatusOK, k)
		}
	case match(parts, "api", "vps", "v1", "public-keys", "attach", "*"):
		v := s.vms[atoi(parts[5])]
		if v == nil {
			notFound(w)
			return
		}
		var body struct {
			IDs []int `json:"ids"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		for _, id := range body.IDs {
			if s.keys[id] == nil {
				writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"message": fmt.Sprintf("public key %d not found", id)})
				return
			}
			v.keys[id] = true
		}
		writeJSON(w, http.StatusOK, map[string]string{"message": "ok"})
	case match(parts, "api", "vps", "v1", "public-keys", "*") && r.Method == http.MethodDelete:
		id := atoi(parts[4])
		if s.keys[id] == nil {
			notFound(w)
			return
		}
		delete(s.keys, id)
		writeJSON(w, http.StatusOK, map[string]string{"message": "deleted"})

	case match(parts, "api", "vps", "v1", "post-install-scripts") && r.Method == http.MethodPost:
		var sc script
		_ = json.NewDecoder(r.Body).Decode(&sc)
		sc.ID = s.id()
		s.scripts[sc.ID] = &sc
		writeJSON(w, http.StatusOK, sc)
	case match(parts, "api", "vps", "v1", "post-install-scripts", "*"):
		id := atoi(parts[4])
		sc := s.scripts[id]
		if sc == nil {
			notFound(w)
			return
		}
		switch r.Method {
		case http.MethodGet:
			writeJSON(w, http.StatusOK, sc)
		case http.MethodPut:
			_ = json.NewDecoder(r.Body).Decode(sc)
			sc.ID = id
			writeJSON(w, http.StatusOK, sc)
		case http.MethodDelete:
			delete(s.scripts, id)
			writeJSON(w, http.StatusOK, map[string]string{"message": "deleted"})
		}

	case match(parts, "api", "vps", "v1", "virtual-machines") && r.Method == http.MethodPost:
		var req struct {
			ItemID string `json:"item_id"`
			Setup  struct {
				DataCenterID        int     `json:"data_center_id"`
				TemplateID          int     `json:"template_id"`
				Hostname            *string `json:"hostname"`
				PostInstallScriptID *int    `json:"post_install_script_id"`
			} `json:"setup"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Setup.PostInstallScriptID != nil && s.scripts[*req.Setup.PostInstallScriptID] == nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"message": "post install script not found"})
			return
		}
		id := s.id()
		hostname := fmt.Sprintf("srv%d.hstgr.cloud", id)
		if req.Setup.Hostname != nil {
			hostname = *req.Setup.Hostname
		}
		v := &vm{ID: id, SubscriptionID: fmt.Sprintf("sub-%d", id), Hostname: hostname, State: "running",
			IPv4: []ip{{Address: fmt.Sprintf("203.0.113.%d", id%250)}}, IPv6: []ip{{Address: fmt.Sprintf("2001:db8::%d", id)}},
			Plan: req.ItemID, DataCenterID: req.Setup.DataCenterID, TemplateID: req.Setup.TemplateID, keys: map[int]bool{}}
		s.vms[id] = v
		writeJSON(w, http.StatusOK, map[string]any{
			"order":           map[string]any{"id": id, "subscription_id": v.SubscriptionID, "status": "completed"},
			"virtual_machine": v,
		})
	case match(parts, "api", "vps", "v1", "virtual-machines") && r.Method == http.MethodGet:
		var list []*vm
		for _, v := range s.vms {
			list = append(list, v)
		}
		writeJSON(w, http.StatusOK, list)
	case match(parts, "api", "vps", "v1", "virtual-machines", "*"):
		v := s.vms[atoi(parts[4])]
		if v == nil {
			notFound(w)
			return
		}
		writeJSON(w, http.StatusOK, v)
	case match(parts, "api", "vps", "v1", "virtual-machines", "*", "hostname"):
		v := s.vms[atoi(parts[4])]
		if v == nil {
			notFound(w)
			return
		}
		var body struct {
			Hostname string `json:"hostname"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		v.Hostname = body.Hostname
		writeJSON(w, http.StatusOK, v)
	case match(parts, "api", "vps", "v1", "virtual-machines", "*", "recreate"):
		v := s.vms[atoi(parts[4])]
		if v == nil {
			notFound(w)
			return
		}
		var body struct {
			TemplateID int `json:"template_id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		v.TemplateID = body.TemplateID
		writeJSON(w, http.StatusOK, v)
	case match(parts, "api", "vps", "v1", "virtual-machines", "*", "public-keys"):
		v := s.vms[atoi(parts[4])]
		if v == nil {
			notFound(w)
			return
		}
		var data []map[string]int
		for id := range v.keys {
			data = append(data, map[string]int{"id": id})
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": data})

	case match(parts, "api", "billing", "v1", "subscriptions", "*"):
		var found *vm
		for _, v := range s.vms {
			if v.SubscriptionID == parts[4] {
				found = v
			}
		}
		if found == nil {
			notFound(w)
			return
		}
		switch r.Method {
		case http.MethodGet:
			writeJSON(w, http.StatusOK, map[string]any{"id": found.SubscriptionID, "status": "active", "item_id": found.Plan})
		case http.MethodDelete:
			// Cancelling a subscription removes the VPS in this mock; the
			// real API removes it at the end of the billing period.
			delete(s.vms, found.ID)
			w.WriteHeader(http.StatusNoContent)
		}

	case match(parts, "api", "dns", "v1", "zones", "*"):
		zone := parts[4]
		entries, ok := s.zones[zone]
		if !ok {
			notFound(w)
			return
		}
		switch r.Method {
		case http.MethodGet:
			if entries == nil {
				entries = []dnsEntry{}
			}
			writeJSON(w, http.StatusOK, entries)
		case http.MethodPut:
			var body struct {
				Overwrite bool       `json:"overwrite"`
				Zone      []dnsEntry `json:"zone"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			for _, e := range body.Zone {
				merged := false
				for i := range entries {
					if strings.EqualFold(entries[i].Name, e.Name) && strings.EqualFold(entries[i].Type, e.Type) {
						if body.Overwrite {
							entries[i] = e
						} else {
							entries[i].Records = append(entries[i].Records, e.Records...)
						}
						merged = true
					}
				}
				if !merged {
					entries = append(entries, e)
				}
			}
			s.zones[zone] = entries
			writeJSON(w, http.StatusOK, map[string]string{"message": "ok"})
		case http.MethodDelete:
			var body struct {
				Filters []struct {
					Name string `json:"name"`
					Type string `json:"type"`
				} `json:"filters"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			var kept []dnsEntry
			for _, e := range entries {
				drop := false
				for _, f := range body.Filters {
					if strings.EqualFold(e.Name, f.Name) && strings.EqualFold(e.Type, f.Type) {
						drop = true
					}
				}
				if !drop {
					kept = append(kept, e)
				}
			}
			s.zones[zone] = kept
			writeJSON(w, http.StatusOK, map[string]string{"message": "ok"})
		}
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "no such route: " + r.Method + " " + r.URL.Path})
	}
}

// match compares path segments; "*" matches any one segment.
func match(parts []string, want ...string) bool {
	if len(parts) != len(want) {
		return false
	}
	for i := range want {
		if want[i] != "*" && want[i] != parts[i] {
			return false
		}
	}
	return true
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

func main() {
	addr := flag.String("addr", "127.0.0.1:8085", "listen address")
	token := flag.String("token", "test-token", "API token the mock accepts")
	flag.Parse()
	log.Printf("mock Hostinger API on %s", *addr)
	log.Fatal(http.ListenAndServe(*addr, newServer(*token)))
}
