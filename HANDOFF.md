# Handoff: crossplane-provider-hostinger

State as of 2026-10-22. Pushed to github.com/hayone1/crossplane-provider-hostinger.

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
- `config/vps/purchase.go` (Create), `sshkeys.go` (Update), `delete.go`
  (Delete) replace upstream VPS CRUD, which is broken against the real API:
  HTTP 202 purchases were retried (bought twice), SSH keys went through
  removed/no-op endpoints, and subscription cancel is a removed route
- `hack/add-docs-prelude.sh`: adds front matter upstream docs lack (scraper needs it)
- `test/mockapi`, `test/e2e/run.sh`, `examples/`

## Workflow
`main` is protected by the "Protect main" ruleset: pull requests only
(squash/rebase, no approvals needed), required checks lint, unit-tests,
check-diff, local-deploy, e2e, govulncheck, gitleaks, codeql, branch up to
date. PR titles must be Conventional Commits (they become the release
commit). Dependabot ignores terraform-plugin-* (pinned to what upjet uses).

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
Unit tests pass; mock e2e passes for both scopes; image + xpkg build;
package installs Healthy in Crossplane. Real API (2026-10-22, zone
softwarecloud.io): full e2e passes in both scopes, VPS covered by importing
one existing VPS (`REAL=1 ZONE=… VPS_SCOPE=… VPS_ID=… POLL=1m`). The
purchase path (incl. HTTP 202 + setup + adopt) was exercised for real once.

## Upgrading the Terraform provider
Bump `github.com/hostinger/terraform-provider-hostinger` in `go.mod` and
`TERRAFORM_PROVIDER_VERSION` in `Makefile` together, then `make generate`.
Re-check upstream for new resources (add to `ExternalNameConfigs`) and
whether the PostInstallScript Read bug is fixed.

## Gotchas
- Creating a VPS buys a subscription; deleting only turns off auto-renewal
  (the API can't cancel any more). Real-API tests cost money.
- The real API rate limits hard (Cloudflare 1015, ~2 h lockout); use POLL=1m.
- Root password: only `# + ? @` as special characters.
- Post-install scripts return Unauthorized until the account has a VPS.
- Namespaced `ProviderConfig.secretRef.namespace` is required by schema but
  ignored (secret is read from the MR's namespace).
- Upstream data sources have no Crossplane equivalent; not implemented.
- `.github/workflows/e2e.yaml` (uptest via `/test-examples`) needs real
  credentials; `e2e-mock.yml` is the free one.

## Open / next
- Releases are automatic (`.github/workflows/release.yml`): after CI passes
  on main, `hack/next-version.sh` picks the next semver tag from Conventional
  Commit subjects, the package is pushed to `ghcr.io/hayone1` and mirrored
  to `xpkg.upbound.io/codejourney` (secrets `XPKG_MIRROR_*`), then tagged.
  Before pushing it scans the image with Grype (fails on fixable critical);
  after, it signs (cosign v3, keyless), attests SLSA provenance + SBOM and
  creates the GitHub release. `security.yml` (govulncheck, CodeQL, gitleaks,
  dependency review), `scorecard.yml` and Dependabot run alongside. The Go
  toolchain is pinned in go.mod (`toolchain`); bump it for stdlib CVEs.
- Upstream bugs reported to hostinger/terraform-provider-hostinger; drop the
  CRUD overrides once fixed.
