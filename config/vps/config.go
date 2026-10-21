// Package vps configures the managed resources of the vps API group.
package vps

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	ujconfig "github.com/crossplane/upjet/v2/pkg/config"
	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hostinger/terraform-provider-hostinger/hostinger"
)

const shortGroup = "vps"

// Configure configures the VPS resources.
func Configure(p *ujconfig.Provider) {
	p.AddResourceConfigurator("hostinger_vps", func(r *ujconfig.Resource) {
		r.ShortGroup = shortGroup
		r.Kind = "VPS"
		// Creating a VPS purchases a subscription and installs the OS
		// template, and changing template_id reinstalls it. Run those calls
		// in the background so they don't block the reconciler.
		r.UseAsync = true

		// The Terraform attributes are numeric IDs; the referenced
		// resources' external names are exactly those IDs.
		r.References["post_install_script_id"] = ujconfig.Reference{
			TerraformName: "hostinger_vps_post_install_script",
		}
		r.References["ssh_key_ids"] = ujconfig.Reference{
			TerraformName: "hostinger_vps_ssh_key",
		}
	})

	p.AddResourceConfigurator("hostinger_vps_post_install_script", func(r *ujconfig.Resource) {
		r.ShortGroup = shortGroup
		r.Kind = "PostInstallScript"
		// The schema-only provider used for code generation has no CRUD.
		if read := r.TerraformResource.ReadContext; read != nil {
			r.TerraformResource.ReadContext = readScriptOrGone(read)
		}
	})

	p.AddResourceConfigurator("hostinger_vps_ssh_key", func(r *ujconfig.Resource) {
		r.ShortGroup = shortGroup
		r.Kind = "SSHKey"
		// The Terraform provider marks the key as sensitive so it is hidden
		// from plan output, but it is the *public* half of the key pair.
		// Keeping it a plain spec field lets it live in Git next to the rest
		// of the manifest instead of requiring a Secret per key.
		if s, ok := r.TerraformResource.Schema["key"]; ok {
			s.Sensitive = false
		}
	})
}

// readScriptOrGone wraps the post-install script Read. Upstream, Read fails
// when the script doesn't exist instead of clearing the ID, so Crossplane
// could never observe a deleted script and a PostInstallScript would stay
// stuck in deletion. When Read fails and the script is gone, the resource is
// reported as absent.
func readScriptOrGone(read schema.ReadContextFunc) schema.ReadContextFunc {
	return func(ctx context.Context, d *schema.ResourceData, meta any) diag.Diagnostics {
		diags := read(ctx, d, meta)
		if !diags.HasError() {
			return diags
		}
		c, ok := meta.(*hostinger.HostingerClient)
		if !ok {
			return diags
		}
		if gone, err := scriptNotFound(ctx, c, d.Id()); err != nil || !gone {
			return diags
		}
		d.SetId("")
		return nil
	}
}

func scriptNotFound(ctx context.Context, c *hostinger.HostingerClient, id string) (bool, error) {
	u := fmt.Sprintf("%s/api/vps/v1/post-install-scripts/%s", c.BaseURL, url.PathEscape(id))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Accept", "application/json")
	hc := c.HTTPClient
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		return false, err
	}
	_ = resp.Body.Close()
	return resp.StatusCode == http.StatusNotFound, nil
}
