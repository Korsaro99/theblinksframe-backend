# syntax=docker/dockerfile:1

# The Blinks API image. Built and pushed with `make publish` (deploy/README.md).
#
#   docker build --platform linux/amd64 -t blinks-api .
#
# Two stages: "build" has the Go toolchain and the module cache; "runtime" gets only the
# compiled binary, so the image is small and has no compiler in it.

ARG GO_VERSION=1.27.1

# ---------------------------------------------------------------- build
# Runs on the machine doing the build ($BUILDPLATFORM, e.g. an arm64 Mac), never emulated: Go
# cross-compiles for the target CPU itself (GOARCH below), which is much faster than emulation.
FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-alpine AS build
WORKDIR /src

# Dependencies first: this layer is cached until go.mod/go.sum change.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
# Set by `docker build --platform`: linux/amd64 for the Oracle x86 VM.
ARG TARGETOS TARGETARCH
# CGO off gives a static binary that needs no C libraries in the runtime image. -trimpath keeps
# the Mac's folder names out of the binary; -s -w drops debug symbols.
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags='-s -w' -o /out/api ./cmd/api

# ---------------------------------------------------------------- runtime
# Alpine rather than an empty image: it has the CA certificates outgoing HTTPS needs (payment
# providers, M4) and the wget the health check uses. Time zones are compiled into the binary.
FROM alpine:3.22 AS runtime
RUN adduser -D -H -u 10001 api
COPY --from=build /out/api /usr/local/bin/api

# Not root: if the API were ever compromised, it can't change the image or bind to low ports.
USER api
EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
  CMD wget -qO- http://127.0.0.1:8080/healthz || exit 1

# Every setting comes from environment variables (.env.example, deploy/compose.yaml).
ENTRYPOINT ["/usr/local/bin/api"]
