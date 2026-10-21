package clients

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/crossplane/crossplane-runtime/v2/pkg/resource"
	"github.com/crossplane/upjet/v2/pkg/terraform"
	terraformSDK "github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	"github.com/hostinger/terraform-provider-hostinger/hostinger"
	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	clusterv1beta1 "github.com/hayone1/crossplane-provider-hostinger/apis/cluster/v1beta1"
	namespacedv1beta1 "github.com/hayone1/crossplane-provider-hostinger/apis/namespaced/v1beta1"
)

const (
	// keyAPIToken is the credentials key, and the Terraform provider
	// argument, that holds the Hostinger API token.
	keyAPIToken = "api_token"

	// error messages
	errNoProviderConfig     = "no providerConfigRef provided"
	errGetProviderConfig    = "cannot get referenced ProviderConfig"
	errTrackUsage           = "cannot track ProviderConfig usage"
	errExtractCredentials   = "cannot extract credentials"
	errUnmarshalCredentials = "cannot unmarshal hostinger credentials as JSON"
	errNoAPIToken           = "credentials do not contain an " + keyAPIToken
	errConfigureProvider    = "cannot configure the Hostinger Terraform provider"
)

// TerraformSetupBuilder returns a terraform.SetupFn that configures the
// in-process ("no-fork") Hostinger Terraform provider with the credentials of
// the ProviderConfig a managed resource references.
func TerraformSetupBuilder() terraform.SetupFn {
	return func(ctx context.Context, client client.Client, mg resource.Managed) (terraform.Setup, error) {
		pcSpec, err := resolveProviderConfig(ctx, client, mg)
		if err != nil {
			return terraform.Setup{}, errors.Wrap(err, "cannot resolve provider config")
		}

		data, err := resource.CommonCredentialExtractor(ctx, pcSpec.Credentials.Source, client, pcSpec.Credentials.CommonCredentialSelectors)
		if err != nil {
			return terraform.Setup{}, errors.Wrap(err, errExtractCredentials)
		}
		token, err := apiToken(data)
		if err != nil {
			return terraform.Setup{}, err
		}

		ps := terraform.Setup{
			Configuration: map[string]any{
				keyAPIToken: token,
			},
		}
		if err := configureNoForkClient(ctx, &ps, pcSpec.Endpoint); err != nil {
			return terraform.Setup{}, err
		}
		return ps, nil
	}
}

// apiToken extracts the API token from the credentials. The canonical format is
// a JSON object, {"api_token": "..."}, but a bare token is accepted too so a
// Secret can be created with `--from-literal=credentials=$TOKEN`.
func apiToken(data []byte) (string, error) {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return "", errors.New(errNoAPIToken)
	}
	if !strings.HasPrefix(trimmed, "{") {
		return trimmed, nil
	}
	creds := map[string]string{}
	if err := json.Unmarshal([]byte(trimmed), &creds); err != nil {
		return "", errors.Wrap(err, errUnmarshalCredentials)
	}
	if creds[keyAPIToken] == "" {
		return "", errors.New(errNoAPIToken)
	}
	return creds[keyAPIToken], nil
}

// configureNoForkClient configures a Hostinger Terraform provider instance and
// stores its API client as the provider meta used by the no-fork external
// clients.
func configureNoForkClient(ctx context.Context, ps *terraform.Setup, endpoint string) error {
	p := hostinger.Provider()
	if diags := p.Configure(ctx, terraformSDK.NewResourceConfigRaw(ps.Configuration)); diags.HasError() {
		return errors.Errorf("%s: %v", errConfigureProvider, diags)
	}
	meta, ok := p.Meta().(*hostinger.HostingerClient)
	if !ok {
		return errors.Errorf("%s: unexpected provider meta type %T", errConfigureProvider, p.Meta())
	}
	if endpoint != "" {
		meta.BaseURL = strings.TrimSuffix(endpoint, "/")
	}
	ps.Meta = meta
	return nil
}

func toSharedPCSpec(pc *clusterv1beta1.ProviderConfig) (*namespacedv1beta1.ProviderConfigSpec, error) {
	if pc == nil {
		return nil, nil
	}
	data, err := json.Marshal(pc.Spec)
	if err != nil {
		return nil, err
	}

	var mSpec namespacedv1beta1.ProviderConfigSpec
	err = json.Unmarshal(data, &mSpec)
	return &mSpec, err
}

func resolveProviderConfig(ctx context.Context, crClient client.Client, mg resource.Managed) (*namespacedv1beta1.ProviderConfigSpec, error) {
	switch managed := mg.(type) {
	case resource.LegacyManaged: //nolint:staticcheck // still handling cluster-scoped behavior
		return resolveLegacy(ctx, crClient, managed)
	case resource.ModernManaged:
		return resolveModern(ctx, crClient, managed)
	default:
		return nil, errors.New("resource is not a managed resource")
	}
}

func resolveLegacy(ctx context.Context, client client.Client, mg resource.LegacyManaged) (*namespacedv1beta1.ProviderConfigSpec, error) { //nolint:staticcheck // still handling cluster-scoped behavior
	configRef := mg.GetProviderConfigReference()
	if configRef == nil {
		return nil, errors.New(errNoProviderConfig)
	}
	pc := &clusterv1beta1.ProviderConfig{}
	if err := client.Get(ctx, types.NamespacedName{Name: configRef.Name}, pc); err != nil {
		return nil, errors.Wrap(err, errGetProviderConfig)
	}

	t := resource.NewLegacyProviderConfigUsageTracker(client, &clusterv1beta1.ProviderConfigUsage{})
	if err := t.Track(ctx, mg); err != nil {
		return nil, errors.Wrap(err, errTrackUsage)
	}

	return toSharedPCSpec(pc)
}

func resolveModern(ctx context.Context, crClient client.Client, mg resource.ModernManaged) (*namespacedv1beta1.ProviderConfigSpec, error) {
	configRef := mg.GetProviderConfigReference()
	if configRef == nil {
		return nil, errors.New(errNoProviderConfig)
	}

	pcRuntimeObj, err := crClient.Scheme().New(namespacedv1beta1.SchemeGroupVersion.WithKind(configRef.Kind))
	if err != nil {
		return nil, errors.Wrap(err, "unknown GVK for ProviderConfig")
	}
	pcObj, ok := pcRuntimeObj.(client.Object)
	if !ok {
		// This indicates a programming error, types are not properly generated
		return nil, errors.New(" is not an Object")
	}

	// Namespace will be ignored if the PC is a cluster-scoped type
	if err := crClient.Get(ctx, types.NamespacedName{Name: configRef.Name, Namespace: mg.GetNamespace()}, pcObj); err != nil {
		return nil, errors.Wrap(err, errGetProviderConfig)
	}

	var pcSpec namespacedv1beta1.ProviderConfigSpec
	pcu := &namespacedv1beta1.ProviderConfigUsage{}
	switch pc := pcObj.(type) {
	case *namespacedv1beta1.ProviderConfig:
		pcSpec = pc.Spec
		if pcSpec.Credentials.SecretRef != nil {
			pcSpec.Credentials.SecretRef.Namespace = mg.GetNamespace()
		}
	case *namespacedv1beta1.ClusterProviderConfig:
		pcSpec = pc.Spec
	default:
		return nil, errors.New("unknown provider config type")
	}
	t := resource.NewProviderConfigUsageTracker(crClient, pcu)
	if err := t.Track(ctx, mg); err != nil {
		return nil, errors.Wrap(err, errTrackUsage)
	}
	return &pcSpec, nil
}
