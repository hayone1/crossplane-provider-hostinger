# The Hostinger Terraform provider is compiled into the provider binary
# ("no-fork" mode), so the image needs neither Terraform nor the plugin.
FROM gcr.io/distroless/static:nonroot@sha256:25ac8f0c3b2334a9eb05452266bd4697864a8c8ef0ef71e5e6480d7f92868329

ARG TARGETOS
ARG TARGETARCH

ADD "bin/${TARGETOS}_${TARGETARCH}/provider" /usr/local/bin/provider

USER 65532
EXPOSE 8080

ENTRYPOINT ["provider"]
