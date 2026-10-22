# Security policy

## Reporting a vulnerability

Please report vulnerabilities privately through
[GitHub's private vulnerability reporting](https://github.com/hayone1/crossplane-provider-hostinger/security/advisories/new),
not in a public issue. You can expect a first reply within a week.

Only the latest release is supported; fixes ship as a new patch release.

## Verifying a release

Every release is signed with [cosign](https://github.com/sigstore/cosign)
(keyless, through GitHub Actions) and carries SLSA build provenance and an
SPDX SBOM. The release notes give the commands for that release; in general
(cosign v3 or later, older versions report "no signatures found"):

```sh
DIGEST=$(crane digest ghcr.io/hayone1/provider-hostinger:v0.1.1)
cosign verify ghcr.io/hayone1/provider-hostinger@$DIGEST \
  --certificate-identity-regexp '^https://github.com/hayone1/crossplane-provider-hostinger/\.github/workflows/release\.yml@' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
gh attestation verify oci://ghcr.io/hayone1/provider-hostinger@$DIGEST \
  --repo hayone1/crossplane-provider-hostinger
```

The same digest is published as
`xpkg.upbound.io/codejourney/provider-hostinger` and is signed there too.
