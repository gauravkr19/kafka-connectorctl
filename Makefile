SHELL := /bin/sh
VERSION ?= dev
BINARY := bin/connectorctl
LDFLAGS := -s -w -X github.com/example/connectorctl/internal/cli.version=$(VERSION)

.PHONY: all build fmt fmt-check test race vet check clean docker

all: check build

build:
	mkdir -p bin
	CGO_ENABLED=0 GOTOOLCHAIN=local go build -trimpath -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/connectorctl

fmt:
	gofmt -w $$(find cmd internal -name '*.go' -type f)

fmt-check:
	@files="$$(gofmt -l $$(find cmd internal -name '*.go' -type f))"; \
	if [ -n "$$files" ]; then echo "Go files require gofmt:"; echo "$$files"; exit 1; fi

test:
	GOTOOLCHAIN=local go test ./...

race:
	GOTOOLCHAIN=local go test -race ./...

vet:
	GOTOOLCHAIN=local go vet ./...

check: fmt-check test vet

clean:
	rm -rf bin reports/* rendered/*
	touch reports/.gitkeep rendered/.gitkeep

docker:
	docker build --build-arg VERSION=$(VERSION) -t connectorctl:$(VERSION) .
