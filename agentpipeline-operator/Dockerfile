# Minimal runtime image for the manager binary.
#
# Cross-compilation strategy (ARM Mac → amd64 cluster):
#   The Go binary is cross-compiled on the HOST before the image build
#   (see Makefile `make docker-build`). Go handles this natively via
#   GOOS=linux GOARCH=amd64 — no emulation needed.
#
#   This Dockerfile just copies the pre-built binary into a distroless
#   image. No RUN instructions = no QEMU = no segfault on ARM Macs.
#
# Why not compile inside the container?
#   podman runs RUN instructions using the container's arch. If we set
#   --platform=linux/amd64, podman emulates amd64 via QEMU for ALL
#   RUN commands (go mod download, go build...) which segfaults.
#   Building natively on the host and just COPYing the binary avoids this.

FROM --platform=linux/amd64 gcr.io/distroless/static:nonroot
WORKDIR /
COPY bin/manager .
USER 65532:65532

ENTRYPOINT ["/manager"]
