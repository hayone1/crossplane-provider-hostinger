# Handoff: crossplane-provider-hostinger

State as of 2026-10-21. One local commit, no remote yet.

## What it is
Upjet-generated Crossplane provider for the Hostinger Terraform provider
v0.1.23, in no-fork mode (Terraform provider compiled in; distroless image).
Resources, both cluster-scoped (`*.hostinger.crossplane.io`) and namespaced
(`*.hostinger.m.crossplane.io`): `VPS`, `PostInstallScript`, `SSHKey`
(group `vps`), `Record` (group `dns`). User docs: `README.md`.

## Hand-written parts (everything else is generated)
- `config/provider.go`, `config/external_name.go`, `config/vps`, `config/dns`
- `internal/clients/hostinger.go`: credentials (JSON or bare token), `spec.endpoint` override
- `apis/*/v1beta1/types.go`: added `ProviderConfigSpec.Endpoint`
- `config/vps/config.go` `readScriptOrGone`: fixes upstream bug where deleting a
  PostInstallScript hung forever (verified by disabling it)
- `hack/add-docs-prelude.sh`: adds front matter upstream docs lack (scraper needs it)
- `test/mockapi`, `test/e2e/run.sh`, `examples/`

## Day to day
```sh
make submodules            # once (build/ submodule)
make generate              # needs goimports in PATH; regenerates apis/controllers/CRDs
go test ./config/... ./internal/...
./test/e2e/run.sh          # kind + CRDs + controller + mock API, both scopes, ~5 min
make build PLATFORMS=linux_amd64
make local-deploy          # kind + Crossplane + local package (use a separate KUBECONFIG)
```
`make` lives in `~/.local/bin` (extracted .deb), Go in `~/.local/go`.

## Verified
Unit tests pass; mock e2e passes for both scopes (create, references,
update, delete); image + xpkg build; package installs Healthy in Crossplane
and reconciles in-cluster. Never run against the real Hostinger API.

## Upgrading the Terraform provider
Bump `github.com/hostinger/terraform-provider-hostinger` in `go.mod` and
`TERRAFORM_PROVIDER_VERSION` in `Makefile` together, then `make generate`.
Re-check upstream for new resources (add to `ExternalNameConfigs`) and
whether the PostInstallScript Read bug is fixed.

## Gotchas
- Creating a VPS buys a subscription; deleting cancels it. Real-API tests cost money.
- Namespaced `ProviderConfig.secretRef.namespace` is required by schema but
  ignored (secret is read from the MR's namespace).
- Upstream data sources have no Crossplane equivalent; not implemented.
- `.github/workflows/e2e.yaml` (uptest via `/test-examples`) needs real
  credentials; `e2e-mock.yml` is the free one.

## Open / next
- Create `github.com/hayone1/crossplane-provider-hostinger`, push, then
  publish via `publish-provider-package.yml` (registry is `ghcr.io/hayone1`
  in the Makefile; README's install snippet assumes `v0.1.0`).
- Real-API smoke test with a token (SSHKey/Record/PostInstallScript are free;
  VPS is not).
