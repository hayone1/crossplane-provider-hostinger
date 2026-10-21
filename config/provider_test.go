package config

import (
	"testing"

	ujconfig "github.com/crossplane/upjet/v2/pkg/config"
)

func TestProviders(t *testing.T) {
	want := map[string]struct {
		group, kind string
		refs        []string
	}{
		"hostinger_vps":                     {"vps", "VPS", []string{"post_install_script_id", "ssh_key_ids"}},
		"hostinger_vps_post_install_script": {"vps", "PostInstallScript", nil},
		"hostinger_vps_ssh_key":             {"vps", "SSHKey", nil},
		"hostinger_dns_record":              {"dns", "Record", nil},
	}
	for name, get := range map[string]func(bool) (*ujconfig.Provider, error){
		"cluster": GetProvider, "namespaced": GetProviderNamespaced,
	} {
		for _, generation := range []bool{true, false} {
			p, err := get(generation)
			if err != nil {
				t.Fatalf("%s (generation=%v): %v", name, generation, err)
			}
			if len(p.Resources) != len(want) {
				t.Errorf("%s: %d resources, want %d", name, len(p.Resources), len(want))
			}
			for tf, w := range want {
				r, ok := p.Resources[tf]
				if !ok {
					t.Errorf("%s: %s not configured", name, tf)
					continue
				}
				if r.ShortGroup != w.group || r.Kind != w.kind {
					t.Errorf("%s: %s is %s/%s, want %s/%s", name, tf, r.ShortGroup, r.Kind, w.group, w.kind)
				}
				if !r.ShouldUseTerraformPluginSDKClient() {
					t.Errorf("%s: %s must use the in-process SDK client", name, tf)
				}
				for _, ref := range w.refs {
					if _, ok := r.References[ref]; !ok {
						t.Errorf("%s: %s has no reference for %s", name, tf, ref)
					}
				}
			}
			if !p.Resources["hostinger_vps"].UseAsync {
				t.Errorf("%s: VPS operations must be asynchronous", name)
			}
			if p.Resources["hostinger_vps_ssh_key"].TerraformResource.Schema["key"].Sensitive {
				t.Errorf("%s: the public key must be a plain field", name)
			}
			if !p.Resources["hostinger_vps"].TerraformResource.Schema["password"].Sensitive {
				t.Errorf("%s: the VPS password must stay sensitive", name)
			}
		}
	}
}

func TestEveryResourceHasAnExternalName(t *testing.T) {
	p, err := GetProvider(true)
	if err != nil {
		t.Fatal(err)
	}
	for name := range p.Resources {
		if _, ok := ExternalNameConfigs[name]; !ok {
			t.Errorf("%s has no external name configuration", name)
		}
	}
}
