package vps

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hostinger/terraform-provider-hostinger/hostinger"
)

func TestDeleteVPS(t *testing.T) {
	cases := map[string]struct {
		exists        bool
		disableStatus int
		autoRenewed   bool
		wantErr       bool
	}{
		"auto-renewal turned off":  {exists: true, disableStatus: http.StatusOK},
		"auto-renewal already off": {exists: true, disableStatus: http.StatusUnprocessableEntity},
		"turning off fails":        {exists: true, disableStatus: http.StatusInternalServerError, autoRenewed: true, wantErr: true},
		"VPS gone":                 {},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/vps/v1/virtual-machines/7":
					if !tc.exists {
						w.WriteHeader(http.StatusNotFound)
						return
					}
					_, _ = w.Write([]byte(`{"id": 7, "subscription_id": "sub"}`))
				case "/api/billing/v1/subscriptions/sub/auto-renewal/disable":
					w.WriteHeader(tc.disableStatus)
				case "/api/billing/v1/subscriptions":
					_, _ = fmt.Fprintf(w, `[{"id": "sub", "is_auto_renewed": %t}]`, tc.autoRenewed)
				default:
					w.WriteHeader(http.StatusBadRequest)
				}
			}))
			defer srv.Close()
			client := hostinger.NewHostingerClient("t", "test")
			client.BaseURL = srv.URL
			d := (&schema.Resource{Schema: map[string]*schema.Schema{}}).TestResourceData()
			d.SetId("7")

			diags := deleteVPS(context.Background(), d, client)
			if diags.HasError() != tc.wantErr {
				t.Fatalf("diags = %v, want error %v", diags, tc.wantErr)
			}
			if gone := d.Id() == ""; gone == tc.wantErr {
				t.Errorf("ID = %q after delete", d.Id())
			}
		})
	}
}
