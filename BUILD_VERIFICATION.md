# Build verification

Version: `0.1.0`

Verified in the delivery environment with Go 1.23.2:

```text
make check
go test -race ./...
make build VERSION=0.1.0
connectorctl config-validate
connectorctl repo-validate
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build ...
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build ...
```

The unit/integration suite uses concurrent fake Kafka Connect REST servers and covers REST retries, environment adoption batches, profile-aware discovery, CREATE, UPDATE, Git-to-Git environment migration, inventory, verification, secret replacement, repository validation, dry-run previews, and rollback-capable worktree batches.

The Dockerfile was inspected and is included, but an image build was not executed because Docker/Podman is not installed in the delivery runtime. The final image also needs an approved `oc`/`kubectl` client when server-side dry-run or annotation operations are used.
