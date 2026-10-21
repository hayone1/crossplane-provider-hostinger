package config

import (
	// Note(turkenh): we are importing this to embed provider schema document
	_ "embed"

	ujconfig "github.com/crossplane/upjet/v2/pkg/config"
	conversiontfjson "github.com/crossplane/upjet/v2/pkg/types/conversion/tfjson"
	tfjson "github.com/hashicorp/terraform-json"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hostinger/terraform-provider-hostinger/hostinger"
	"github.com/pkg/errors"

	"github.com/hayone1/crossplane-provider-hostinger/config/dns"
	"github.com/hayone1/crossplane-provider-hostinger/config/vps"
)

const (
	resourcePrefix = "hostinger"
	modulePath     = "github.com/hayone1/crossplane-provider-hostinger"

	rootGroupCluster    = "hostinger.crossplane.io"
	rootGroupNamespaced = "hostinger.m.crossplane.io"
)

//go:embed schema.json
var providerSchema string

//go:embed provider-metadata.yaml
var providerMetadata string

// configurators are applied to both the cluster-scoped and the namespaced
// provider. References are declared by Terraform resource name, so the same
// configuration resolves to the right Go types in either API variant.
var configurators = []func(provider *ujconfig.Provider){
	vps.Configure,
	dns.Configure,
}

// GetProvider returns the configuration of the legacy cluster-scoped
// managed resources (hostinger.crossplane.io).
//
// generationProvider must be true when the configuration is used by the code
// generator: the CRD types are then derived from the JSON schema instead of the
// in-process Terraform provider, which keeps numeric types intact.
func GetProvider(generationProvider bool) (*ujconfig.Provider, error) {
	return buildProvider(rootGroupCluster, generationProvider)
}

// GetProviderNamespaced returns the configuration of the namespaced managed
// resources (hostinger.m.crossplane.io) introduced with Crossplane v2.
func GetProviderNamespaced(generationProvider bool) (*ujconfig.Provider, error) {
	return buildProvider(rootGroupNamespaced, generationProvider,
		ujconfig.WithExampleManifestConfiguration(ujconfig.ExampleManifestConfiguration{
			ManagedResourceNamespace: "default",
		}))
}

func buildProvider(rootGroup string, generationProvider bool, opts ...ujconfig.ProviderOption) (*ujconfig.Provider, error) {
	var p *schema.Provider
	if generationProvider {
		var err error
		if p, err = providerFromSchema(providerSchema); err != nil {
			return nil, errors.Wrap(err, "cannot build the Terraform provider from the JSON schema")
		}
	} else {
		p = hostinger.Provider()
	}

	opts = append([]ujconfig.ProviderOption{
		ujconfig.WithShortName("hostinger"),
		ujconfig.WithRootGroup(rootGroup),
		ujconfig.WithFeaturesPackage("internal/features"),
		// Every resource runs through the in-process ("no-fork") Terraform
		// plugin SDK v2 provider, so the CLI-based include list stays empty.
		ujconfig.WithIncludeList([]string{}),
		ujconfig.WithTerraformPluginSDKIncludeList(ExternalNameConfigured()),
		ujconfig.WithTerraformProvider(p),
		ujconfig.WithDefaultResourceOptions(ExternalNameConfigurations()),
	}, opts...)

	pc := ujconfig.NewProvider([]byte(providerSchema), resourcePrefix, modulePath, []byte(providerMetadata), opts...)
	for _, configure := range configurators {
		configure(pc)
	}
	pc.ConfigureResources()
	return pc, nil
}

// providerFromSchema builds a schema-only Terraform provider out of the JSON
// schema produced by `terraform providers schema -json`.
func providerFromSchema(s string) (*schema.Provider, error) {
	ps := tfjson.ProviderSchemas{}
	if err := ps.UnmarshalJSON([]byte(s)); err != nil {
		return nil, errors.Wrap(err, "cannot unmarshal the provider schema")
	}
	if len(ps.Schemas) != 1 {
		return nil, errors.Errorf("there should be exactly 1 provider schema but there are %d", len(ps.Schemas))
	}
	var rs map[string]*tfjson.Schema
	for _, v := range ps.Schemas {
		rs = v.ResourceSchemas
	}
	return &schema.Provider{ResourcesMap: conversiontfjson.GetV2ResourceMap(rs)}, nil
}
