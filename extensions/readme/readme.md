# Hostinger Provider

`provider-hostinger` manages [Hostinger](https://www.hostinger.com) resources
with Crossplane: VPS instances, the SSH keys attached to them, reusable
post-install scripts and DNS records.

It is generated with [Upjet](https://github.com/crossplane/upjet) from the
[Hostinger Terraform provider](https://registry.terraform.io/providers/hostinger/hostinger/latest),
which it runs in-process. Every managed resource comes in two variants: the
cluster-scoped `*.hostinger.crossplane.io` API and the namespaced
`*.hostinger.m.crossplane.io` API introduced with Crossplane v2.

| Kind | API groups | Terraform resource |
|---|---|---|
| `VPS` | `vps.hostinger.crossplane.io`, `vps.hostinger.m.crossplane.io` | `hostinger_vps` |
| `PostInstallScript` | `vps.hostinger.crossplane.io`, `vps.hostinger.m.crossplane.io` | `hostinger_vps_post_install_script` |
| `SSHKey` | `vps.hostinger.crossplane.io`, `vps.hostinger.m.crossplane.io` | `hostinger_vps_ssh_key` |
| `Record` | `dns.hostinger.crossplane.io`, `dns.hostinger.m.crossplane.io` | `hostinger_dns_record` |

See the [examples](https://github.com/hayone1/crossplane-provider-hostinger/tree/main/examples).
