package vps

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/hostinger/terraform-provider-hostinger/hostinger"
)

func TestAttachKeys(t *testing.T) {
	var added []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/vps/v1/public-keys":
			// One key per page, so the lookup must follow the pages.
			keys := map[string]string{"1": `{"id": 1, "key": "ssh-ed25519 AAA one"}`, "2": `{"id": 2, "key": "ssh-ed25519 BBB two"}`}
			_, _ = fmt.Fprintf(w, `{"data": [%s], "meta": {"current_page": 1, "per_page": 1, "total": 2}}`, keys[r.URL.Query().Get("page")])
		case r.URL.Path == "/api/vps/v1/virtual-machines/7/ssh-keys" && r.Method == http.MethodGet:
			_, _ = w.Write([]byte(`[{"key": "ssh-ed25519 AAA"}]`))
		case r.URL.Path == "/api/vps/v1/virtual-machines/7/ssh-keys" && r.Method == http.MethodPost:
			var body struct {
				Keys []string `json:"keys"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			added = append(added, body.Keys...)
			_, _ = w.Write([]byte(`[]`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	client := hostinger.NewHostingerClient("t", "test")
	client.BaseURL = srv.URL

	if err := attachKeys(context.Background(), client, 7, []int{1, 2}); err != nil {
		t.Fatal(err)
	}
	// Key 1 is installed already, without its comment.
	if want := []string{"ssh-ed25519 BBB two"}; !reflect.DeepEqual(added, want) {
		t.Errorf("added %q, want %q", added, want)
	}
	if err := attachKeys(context.Background(), client, 7, []int{3}); err == nil {
		t.Error("unknown key ID: no error")
	}
}
