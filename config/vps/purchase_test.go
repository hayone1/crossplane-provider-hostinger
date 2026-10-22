package vps

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hostinger/terraform-provider-hostinger/hostinger"
)

// fakeAPI serves the VPS calls createVPS makes. A purchase answers with
// status; a 202 makes the VPS appear in the "initial" state on a later list.
type fakeAPI struct {
	mu        sync.Mutex
	status    int
	setupFail bool
	vms       []hostinger.VirtualMachine
	pending   *hostinger.VirtualMachine
	purchases int
	setups    int
	lists     int
}

func (f *fakeAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	write := func(status int, v any) {
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(v)
	}
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/api/vps/v1/virtual-machines":
		// The pending purchase shows up on the second list after it.
		if f.lists++; f.pending != nil && f.lists > 2 {
			f.vms = append(f.vms, *f.pending)
			f.pending = nil
		}
		write(http.StatusOK, f.vms)
	case r.Method == http.MethodPost && r.URL.Path == "/api/vps/v1/virtual-machines":
		f.purchases++
		vm := hostinger.VirtualMachine{ID: 100 + f.purchases, State: "running"}
		if f.status == http.StatusAccepted {
			vm.State = stateInitial
			f.pending = &vm
			write(http.StatusAccepted, map[string]any{"status": "payment_initiated"})
			return
		}
		f.vms = append(f.vms, vm)
		write(f.status, map[string]any{"virtual_machine": vm})
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/setup"):
		f.setups++
		if f.setupFail {
			write(http.StatusUnprocessableEntity, map[string]string{"message": "nope"})
			return
		}
		for i := range f.vms {
			if r.URL.Path == fmt.Sprintf("/api/vps/v1/virtual-machines/%d/setup", f.vms[i].ID) {
				f.vms[i].State = "running"
			}
		}
		write(http.StatusOK, map[string]any{})
	case r.Method == http.MethodGet:
		for _, vm := range f.vms {
			if r.URL.Path == fmt.Sprintf("/api/vps/v1/virtual-machines/%d", vm.ID) {
				write(http.StatusOK, vm)
				return
			}
		}
		write(http.StatusNotFound, map[string]string{"message": "not found"})
	default:
		write(http.StatusBadRequest, map[string]string{"message": r.Method + " " + r.URL.Path})
	}
}

func TestCreateVPS(t *testing.T) {
	pollInterval = time.Millisecond
	read := func(context.Context, *schema.ResourceData, any) diag.Diagnostics { return nil }
	res := &schema.Resource{Schema: map[string]*schema.Schema{
		"plan":                   {Type: schema.TypeString, Optional: true},
		"data_center_id":         {Type: schema.TypeInt, Optional: true},
		"template_id":            {Type: schema.TypeInt, Optional: true},
		"hostname":               {Type: schema.TypeString, Optional: true},
		"password":               {Type: schema.TypeString, Optional: true},
		"post_install_script_id": {Type: schema.TypeInt, Optional: true},
		"payment_method_id":      {Type: schema.TypeInt, Optional: true},
		"ssh_key_ids":            {Type: schema.TypeList, Optional: true, Elem: &schema.Schema{Type: schema.TypeInt}},
		"vps_id":                 {Type: schema.TypeInt, Computed: true},
	}}

	cases := map[string]struct {
		api           *fakeAPI
		wantErr       bool
		wantID        string
		wantPurchases int
		wantSetups    int
	}{
		"bought": {
			api:    &fakeAPI{status: http.StatusOK},
			wantID: "101", wantPurchases: 1,
		},
		"payment pending": {
			api:    &fakeAPI{status: http.StatusAccepted},
			wantID: "101", wantPurchases: 1, wantSetups: 1,
		},
		"adopts a VPS that is not set up": {
			api: &fakeAPI{status: http.StatusOK, vms: []hostinger.VirtualMachine{
				{ID: 7, State: "running"}, {ID: 8, State: stateInitial},
			}},
			wantID: "8", wantSetups: 1,
		},
		"setup fails": {
			api:     &fakeAPI{status: http.StatusAccepted, setupFail: true},
			wantErr: true, wantPurchases: 1, wantSetups: 1,
		},
		"purchase fails": {
			api:     &fakeAPI{status: http.StatusUnprocessableEntity},
			wantErr: true, wantPurchases: 1,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(tc.api)
			defer srv.Close()
			client := hostinger.NewHostingerClient("t", "test")
			client.BaseURL = srv.URL
			d := res.TestResourceData()
			_ = d.Set("plan", "hostingercom-vps-kvm1-usd-1m")

			diags := createVPS(read)(context.Background(), d, client)
			if diags.HasError() != tc.wantErr {
				t.Fatalf("diags = %v, want error %v", diags, tc.wantErr)
			}
			if d.Id() != tc.wantID {
				t.Errorf("ID = %q, want %q", d.Id(), tc.wantID)
			}
			if tc.api.purchases != tc.wantPurchases || tc.api.setups != tc.wantSetups {
				t.Errorf("purchases, setups = %d, %d, want %d, %d", tc.api.purchases, tc.api.setups, tc.wantPurchases, tc.wantSetups)
			}
		})
	}
}
