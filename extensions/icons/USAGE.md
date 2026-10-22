# Usage

`icon.svg` is the package icon shown by the Upbound Marketplace: a server
stack inside a reconcile loop. It is original artwork for this project
(Apache-2.0, like the rest of the repository) and contains no Hostinger
marks, logos or brand colors.

Don't replace it with the Hostinger logo or anything resembling it. Hostinger's
[brand guidelines](https://www.hostinger.com/legal/brand-guidelines-permissions)
don't license its logos for third-party products, and ask that its marks and
look (logos, color combinations, product icons) aren't used in a way that
suggests affiliation or endorsement.

The image must:

- be named `icon.svg`
- have square dimensions
- be a valid [SVG](https://www.svgviewer.dev/)

The release workflow attaches `extensions/` to every published package with
`up alpha xpkg append`; `meta.crossplane.io/iconURI` in
`package/crossplane.yaml` points at this file as a fallback.
