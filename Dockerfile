# Container image for Modbus TCP use. It packages the release binaries as
# they are (downloaded and checksum-verified by .github/workflows/image.yml);
# nothing is compiled or run while building.
FROM scratch

ARG TARGETARCH
ARG TARGETVARIANT

# linux/arm/v7 resolves to modtop-linux-armv7.
COPY dist/modtop-linux-${TARGETARCH}${TARGETVARIANT} /modtop

USER 65532:65532
ENTRYPOINT ["/modtop"]
