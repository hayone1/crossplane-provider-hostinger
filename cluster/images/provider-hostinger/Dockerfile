# The Hostinger Terraform provider is compiled into the provider binary
# ("no-fork" mode), so the image needs neither Terraform nor the plugin.
FROM gcr.io/distroless/static:nonroot

ARG TARGETOS
ARG TARGETARCH

ADD "bin/${TARGETOS}_${TARGETARCH}/provider" /usr/local/bin/provider

USER 65532
EXPOSE 8080

ENTRYPOINT ["provider"]
