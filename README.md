# provider-hostinger

A [Crossplane](https://crossplane.io) provider for [Hostinger](https://www.hostinger.com):
VPS instances, SSH keys, post-install scripts and DNS records.

It is generated with [Upjet](https://github.com/crossplane/upjet) from the
[Hostinger Terraform provider](https://registry.terraform.io/providers/hostinger/hostinger/latest)
(currently v0.1.23) and has the same resources, arguments and behavior. The
Terraform provider is compiled into the controller and called in-process
("no-fork"), so there is no Terraform binary or plugin in the image.

## Managed resources

Every resource comes in both Crossplane API variants: the cluster-scoped
`*.hostinger.crossplane.io` (Crossplane v1 and v2) and the namespaced
`*.hostinger.m.crossplane.io` (Crossplane v2).

| Kind | Group | Terraform | External name |
|---|---|---|---|
| `VPS` | `vps.hostinger[.m].crossplane.io/v1alpha1` | `hostinger_vps` | VPS ID, e.g. `1268054` |
| `PostInstallScript` | `vps.hostinger[.m].crossplane.io/v1alpha1` | `hostinger_vps_post_install_script` | script ID |
| `SSHKey` | `vps.hostinger[.m].crossplane.io/v1alpha1` | `hostinger_vps_ssh_key` | public key ID |
| `Record` | `dns.hostinger[.m].crossplane.io/v1alpha1` | `hostinger_dns_record` | `<name>\|<type>\|<value>` (the zone is `spec.forProvider.zone`) |

Cross-resource references:

- `VPS.spec.forProvider.postInstallScriptId`: `postInstallScriptIdRef` / `postInstallScriptIdSelector` → `PostInstallScript`
- `VPS.spec.forProvider.sshKeyIds`: `sshKeyIdsRefs` / `sshKeyIdsSelector` → `SSHKey`

The VPS root `password` is read from a Secret (`passwordSecretRef`). The
Terraform provider marks the SSH key as sensitive; it is the public half of
the key pair, so here it is a plain `spec.forProvider.key` field that can live
in Git.

The Terraform provider's three data sources (`hostinger_vps_templates`,
`hostinger_vps_data_centers`, `hostinger_vps_plans`) have no managed resource
equivalent. Crossplane manages resources rather than querying catalogs, and
Upjet doesn't generate data sources. Look the IDs up in hPanel or with the
[Hostinger API](https://developers.hostinger.com) (`GET /api/vps/v1/templates`,
`/api/vps/v1/data-centers`, `/api/billing/v1/catalog`).

## Install

```yaml
apiVersion: pkg.crossplane.io/v1
kind: Provider
metadata:
  name: provider-hostinger
spec:
  package: ghcr.io/hayone1/provider-hostinger:v0.1.0
```

Create an API token in hPanel (Profile → API), then:

```sh
kubectl -n crossplane-system create secret generic hostinger-creds \
  --from-literal=credentials="$HOSTINGER_API_TOKEN"
```

The credentials may be the bare token or JSON: `{"api_token": "..."}`.

```yaml
apiVersion: hostinger.crossplane.io/v1beta1        # cluster-scoped resources
kind: ProviderConfig
metadata:
  name: default
spec:
  credentials:
    source: Secret
    secretRef: {name: hostinger-creds, namespace: crossplane-system, key: credentials}
---
apiVersion: hostinger.m.crossplane.io/v1beta1      # namespaced resources
kind: ClusterProviderConfig                        # or a ProviderConfig per namespace
metadata:
  name: default
spec:
  credentials:
    source: Secret
    secretRef: {name: hostinger-creds, namespace: crossplane-system, key: credentials}
```

`spec.endpoint` overrides the API base URL (default
`https://developers.hostinger.com`), for proxies or a mock API.

## Example

```yaml
apiVersion: vps.hostinger.m.crossplane.io/v1alpha1
kind: SSHKey
metadata:
  name: laptop
  namespace: default
  labels: {owner: platform-team}
spec:
  forProvider:
    name: platform-team laptop
    key: ssh-ed25519 AAAA... platform@example.com
  providerConfigRef: {kind: ClusterProviderConfig, name: default}
---
apiVersion: vps.hostinger.m.crossplane.io/v1alpha1
kind: VPS
metadata:
  name: web-01
  namespace: default
spec:
  forProvider:
    plan: hostingercom-vps-kvm2-usd-1m
    dataCenterId: 13
    templateId: 1002
    hostname: web-01.example.com
    passwordSecretRef: {name: web-01-root, key: password}
    sshKeyIdsSelector:
      matchLabels: {owner: platform-team}
  providerConfigRef: {kind: ClusterProviderConfig, name: default}
```

`status.atProvider` reports `ipv4Address`, `ipv6Address`, `status` and
`vpsId`. More in [examples/](examples): cluster-scoped and namespaced
manifests for every kind.

## Differences from the Terraform provider

| | Terraform | This provider |
|---|---|---|
| SSH public key | sensitive argument | plain `spec.forProvider.key`, so it can live in Git |
| `PostInstallScript` that no longer exists | Read fails | reported as gone; without this, Crossplane could never finish deleting one (its delete path observes the script after removing it), and a script deleted outside Crossplane would fail forever |
| API endpoint | fixed | `ProviderConfig.spec.endpoint` |
| Credentials | `api_token` argument / `HOSTINGER_API_TOKEN` | Secret, environment or filesystem source; JSON `{"api_token": …}` or the bare token |
| Data sources | three | none (see above) |

## Things to know

These come from the Hostinger API and the Terraform provider, and apply here
too:

- **Creating a `VPS` buys a subscription** with the account's default payment
  method (or `paymentMethodId`). **Deleting it cancels the subscription.** For
  servers that must outlive their manifest, use `deletionPolicy: Orphan`, or
  `managementPolicies: ["Observe"]` to only track an existing VPS.
- Changing `templateId` **reinstalls the operating system** (`recreate`).
  `plan`, `dataCenterId` and `password` can't change after creation. Changing
  them would mean a new VPS.
- `sshKeyIds` only ever attaches keys. Removing an ID from the list does not
  detach the key from the server.
- `Record` fields can't change; Crossplane reports an error instead of
  replacing the record. Change `name`, `type` or `value` by creating a new
  `Record` and deleting the old one. With `overwrite: true` a record replaces
  existing records of the same name and type; that is needed to adopt names
  that already have records.
- The DNS zone is eventually consistent; a new record can take up to 30 s to
  show up, which the provider waits for.

Importing existing resources: set the `crossplane.io/external-name`
annotation to the ID from the table above (for DNS records,
`<name>|<type>|<value>` plus `spec.forProvider.zone`), optionally with
`managementPolicies: ["Observe"]`.

## Development

```sh
make submodules          # crossplane/build, once
make generate            # regenerate APIs, controllers, CRDs and examples-generated
go test ./...            # unit tests
make run                 # run the controller against the current kube context
./test/e2e/run.sh        # kind + CRDs + controller + mock Hostinger API, every kind, both scopes
make build               # provider image and Crossplane package
```

`test/mockapi` is an in-memory stand-in for the Hostinger API endpoints the
Terraform provider calls. The end-to-end test points a ProviderConfig's
`endpoint` at it and creates, updates, references and deletes every kind,
without an account and without buying anything.

To move to a new version of the Terraform provider: update
`github.com/hostinger/terraform-provider-hostinger` in `go.mod` and
`TERRAFORM_PROVIDER_VERSION` in the `Makefile` to the same version, then run
`make generate`.

The upstream documentation has no registry front matter, which Upjet's
metadata scraper needs. `hack/add-docs-prelude.sh` adds it to the pulled
docs during `make generate`.

## License

Apache 2.0.
