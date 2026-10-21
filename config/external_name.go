package config

import (
	"github.com/crossplane/upjet/v2/pkg/config"
)

// ExternalNameConfigs contains all external name configurations for this
// provider. Only resources listed here are generated.
//
// Every Hostinger resource gets its identifier from the API, so the external
// name is the Terraform ID:
//
//   - hostinger_vps:                     numeric virtual machine ID ("1268054")
//   - hostinger_vps_post_install_script: numeric script ID
//   - hostinger_vps_ssh_key:             numeric public key ID
//   - hostinger_dns_record:              "<name>|<type>|<value>"; the zone is a
//     spec parameter. To import an existing record set the external-name
//     annotation to that string and fill in spec.forProvider.zone.
var ExternalNameConfigs = map[string]config.ExternalName{
	"hostinger_vps":                     config.IdentifierFromProvider,
	"hostinger_vps_post_install_script": config.IdentifierFromProvider,
	"hostinger_vps_ssh_key":             config.IdentifierFromProvider,
	"hostinger_dns_record":              config.IdentifierFromProvider,
}

// ExternalNameConfigurations applies all external name configs listed in the
// table ExternalNameConfigs.
func ExternalNameConfigurations() config.ResourceOption {
	return func(r *config.Resource) {
		if e, ok := ExternalNameConfigs[r.Name]; ok {
			r.ExternalName = e
		}
	}
}

// ExternalNameConfigured returns the list of all resources whose external name
// is configured manually.
func ExternalNameConfigured() []string {
	l := make([]string, 0, len(ExternalNameConfigs))
	for name := range ExternalNameConfigs {
		// $ is added to match the exact string since the format is regex.
		l = append(l, "^"+name+"$")
	}
	return l
}
