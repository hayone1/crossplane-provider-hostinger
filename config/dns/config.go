// Package dns configures the managed resources of the dns API group.
package dns

import (
	ujconfig "github.com/crossplane/upjet/v2/pkg/config"
)

// Configure configures the DNS resources.
func Configure(p *ujconfig.Provider) {
	p.AddResourceConfigurator("hostinger_dns_record", func(r *ujconfig.Resource) {
		r.ShortGroup = "dns"
		r.Kind = "Record"
	})
}
