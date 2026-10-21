package vps

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hostinger/terraform-provider-hostinger/hostinger"
)

func TestReadScriptOrGone(t *testing.T) {
	status := http.StatusNotFound
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer t" || r.URL.Path != "/api/vps/v1/post-install-scripts/42" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.WriteHeader(status)
	}))
	defer srv.Close()
	client := hostinger.NewHostingerClient("t", "test")
	client.BaseURL = srv.URL

	failing := func(context.Context, *schema.ResourceData, any) diag.Diagnostics {
		return diag.Errorf("read post-install script failed")
	}
	res := &schema.Resource{Schema: map[string]*schema.Schema{"name": {Type: schema.TypeString, Optional: true}}}

	d := res.TestResourceData()
	d.SetId("42")
	if diags := readScriptOrGone(failing)(context.Background(), d, client); diags.HasError() || d.Id() != "" {
		t.Errorf("deleted script: diags=%v id=%q, want no error and an empty ID", diags, d.Id())
	}

	status = http.StatusInternalServerError
	d.SetId("42")
	if diags := readScriptOrGone(failing)(context.Background(), d, client); !diags.HasError() || d.Id() != "42" {
		t.Errorf("API error: diags=%v id=%q, want the original error", diags, d.Id())
	}

	ok := func(context.Context, *schema.ResourceData, any) diag.Diagnostics { return nil }
	if diags := readScriptOrGone(ok)(context.Background(), d, client); diags.HasError() || d.Id() != "42" {
		t.Errorf("successful read changed: diags=%v id=%q", diags, d.Id())
	}
}
