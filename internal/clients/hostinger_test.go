package clients

import (
	"context"
	"testing"

	xpv2 "github.com/crossplane/crossplane/apis/v2/core/v2"
	"github.com/hostinger/terraform-provider-hostinger/hostinger"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	clusterdns "github.com/hayone1/crossplane-provider-hostinger/apis/cluster/dns/v1alpha1"
	clusterv1beta1 "github.com/hayone1/crossplane-provider-hostinger/apis/cluster/v1beta1"
	namespaceddns "github.com/hayone1/crossplane-provider-hostinger/apis/namespaced/dns/v1alpha1"
	namespacedv1beta1 "github.com/hayone1/crossplane-provider-hostinger/apis/namespaced/v1beta1"
)

func TestAPIToken(t *testing.T) {
	for name, tc := range map[string]struct {
		data    string
		want    string
		wantErr bool
	}{
		"json":         {data: `{"api_token": "abc"}`, want: "abc"},
		"bare":         {data: "  abc\n", want: "abc"},
		"empty":        {data: " ", wantErr: true},
		"missing key":  {data: `{"token": "abc"}`, wantErr: true},
		"invalid json": {data: `{"api_token": `, wantErr: true},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := apiToken([]byte(tc.data))
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
			if got != tc.want {
				t.Errorf("token = %q, want %q", got, tc.want)
			}
		})
	}
}

func scheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{
		clientgoscheme.AddToScheme, clusterv1beta1.SchemeBuilder.AddToScheme, namespacedv1beta1.SchemeBuilder.AddToScheme,
		clusterdns.SchemeBuilder.AddToScheme, namespaceddns.SchemeBuilder.AddToScheme,
	} {
		if err := add(s); err != nil {
			t.Fatal(err)
		}
	}
	return s
}

func secret(ns string) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "creds", Namespace: ns},
		Data:       map[string][]byte{"credentials": []byte(`{"api_token":"s3cr3t"}`)},
	}
}

func selector(ns string) xpv2.CommonCredentialSelectors {
	return xpv2.CommonCredentialSelectors{SecretRef: &xpv2.SecretKeySelector{
		SecretReference: xpv2.SecretReference{Name: "creds", Namespace: ns}, Key: "credentials",
	}}
}

func TestSetupClusterScoped(t *testing.T) {
	pc := &clusterv1beta1.ProviderConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "default"},
		Spec: clusterv1beta1.ProviderConfigSpec{
			Endpoint:    "http://127.0.0.1:8085/",
			Credentials: clusterv1beta1.ProviderCredentials{Source: xpv2.CredentialsSourceSecret, CommonCredentialSelectors: selector("crossplane-system")},
		},
	}
	c := fake.NewClientBuilder().WithScheme(scheme(t)).WithObjects(pc, secret("crossplane-system")).Build()
	mr := &clusterdns.Record{ObjectMeta: metav1.ObjectMeta{Name: "www", UID: "uid-1"}}
	mr.Spec.ProviderConfigReference = &xpv2.Reference{Name: "default"}

	ps, err := TerraformSetupBuilder()(context.Background(), c, mr)
	if err != nil {
		t.Fatal(err)
	}
	if ps.Configuration[keyAPIToken] != "s3cr3t" {
		t.Errorf("configuration = %v", ps.Configuration)
	}
	meta, ok := ps.Meta.(*hostinger.HostingerClient)
	if !ok {
		t.Fatalf("meta is %T", ps.Meta)
	}
	if meta.BaseURL != "http://127.0.0.1:8085" || meta.Token != "s3cr3t" {
		t.Errorf("client = %s %s", meta.BaseURL, meta.Token)
	}
}

func TestSetupNamespaced(t *testing.T) {
	// A namespaced ProviderConfig reads its Secret from the managed
	// resource's namespace, whatever the reference says.
	pc := &namespacedv1beta1.ProviderConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "team", Namespace: "apps"},
		Spec: namespacedv1beta1.ProviderConfigSpec{
			Credentials: namespacedv1beta1.ProviderCredentials{Source: xpv2.CredentialsSourceSecret, CommonCredentialSelectors: selector("elsewhere")},
		},
	}
	c := fake.NewClientBuilder().WithScheme(scheme(t)).WithObjects(pc, secret("apps")).Build()
	mr := &namespaceddns.Record{ObjectMeta: metav1.ObjectMeta{Name: "www", Namespace: "apps", UID: "uid-2"}}
	mr.Spec.ProviderConfigReference = &xpv2.ProviderConfigReference{Kind: "ProviderConfig", Name: "team"}

	ps, err := TerraformSetupBuilder()(context.Background(), c, mr)
	if err != nil {
		t.Fatal(err)
	}
	meta := ps.Meta.(*hostinger.HostingerClient)
	if meta.BaseURL != "https://developers.hostinger.com" || meta.Token != "s3cr3t" {
		t.Errorf("client = %s %s", meta.BaseURL, meta.Token)
	}
}

func TestSetupMissingProviderConfig(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(scheme(t)).Build()
	mr := &clusterdns.Record{ObjectMeta: metav1.ObjectMeta{Name: "www", UID: "uid-1"}}
	mr.Spec.ProviderConfigReference = &xpv2.Reference{Name: "missing"}
	if _, err := TerraformSetupBuilder()(context.Background(), c, mr); err == nil {
		t.Error("expected an error for a missing ProviderConfig")
	}
}
