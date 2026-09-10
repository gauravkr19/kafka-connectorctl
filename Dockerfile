FROM golang:1.23 AS build
ARG VERSION=dev
WORKDIR /src
COPY . .
RUN GOTOOLCHAIN=local go test ./... && \
    CGO_ENABLED=0 GOTOOLCHAIN=local go build -trimpath \
      -ldflags "-s -w -X github.com/example/connectorctl/internal/cli.version=${VERSION}" \
      -o /out/connectorctl ./cmd/connectorctl

FROM registry.access.redhat.com/ubi9/ubi-minimal:latest
COPY --from=build /out/connectorctl /usr/local/bin/connectorctl
WORKDIR /workspace
USER 1001
ENTRYPOINT ["/usr/local/bin/connectorctl"]
