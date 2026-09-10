# connectorctl

`connectorctl` is a Go CLI for migrating Kafka Connect connectors created through REST/UI management into Confluent for Kubernetes (CFK) `Connector` custom resources managed through Git and ArgoCD.

It supports:

- environment-wide, list, CSV, or single-connector adoption;
- concurrent and rate-limited Kafka Connect REST discovery;
- CREATE and UPDATE from raw Kafka Connect JSON;
- Git-to-Git environment migration for new logical environments;
- inventory, repository validation, and post-sync runtime verification;
- annotation-based status, pause, resume, connector restart, and task restart;
- class-aware folder placement and validation;
- guided replacement of plaintext-sensitive settings with external references;
- rollback-capable batch updates to the local Git worktree.

## Intended control flow

```text
Jenkins parameters
       |
       v
connectorctl (download, classify, validate, render)
       |
       v
Git branch / merge request
       |
       v
ArgoCD
       |
       v
CFK Connector CR
       |
       v
Kafka Connect
```

`connectorctl` does **not** run `oc apply` for normal connector deployment, push Git commits, merge requests, invoke ArgoCD sync, or write credential values to Vault. Those boundaries are intentional.

## Git layout

Generated resources use:

```text
connectors/
├── dev/
│   ├── sit/
│   │   ├── postgres/
│   │   │   └── ngil-sit-ciam-pgsql.yaml
│   │   ├── oracle-cdc/
│   │   └── http-sink/
│   ├── e2e/
│   └── prf/
└── preprod/
    ├── reg/
    ├── pps/
    └── ide/
```

The hierarchy is **physical cluster -> logical environment -> plugin alias -> connector**. One connector per file gives useful Git history, review, rollback, inventory, and ownership at the scale of hundreds of connectors.

## Safety properties

- Logical environments are mapped to one physical cluster and a wrong pairing fails before any REST call.
- Connector class is read from `connector.class`; it is never inferred from the connector name.
- Unknown classes fail closed.
- Sensitive values cannot be written to generated CRs without an accepted external reference.
- ADOPT preserves the Connect profile on which the connector currently runs.
- CREATE and MIGRATE reject target names already found in live Connect when live checks are enabled.
- Multi-connector worktree mutations are preflighted and applied as one rollback-capable batch.
- If one connector in a write batch fails validation, successful items are reported as skipped and no CR files are written.
- Dry runs create inspectable YAML previews under `rendered/<run-id>/` without changing the connector source tree.
- All live REST access is bounded by worker limits, per-profile rate limits, timeouts, and retries.

## Build and test

Minimum Go version: **1.23**.

```bash
make test
make race
make vet
make build
```

Or directly:

```bash
GOTOOLCHAIN=local go test ./...
GOTOOLCHAIN=local go test -race ./...
CGO_ENABLED=0 go build -trimpath -o bin/connectorctl ./cmd/connectorctl
```

The YAML implementation is included under `internal/yaml`, so the project has no runtime Go module download requirement. Its upstream license and notice are retained in that directory.

## Configure before running

Update these registry files:

```text
config/platforms.yaml
config/plugins.yaml
config/policies.yaml
config/secret-profiles.yaml
config/mappings/<cluster>/<environment>.yaml
```

At minimum:

1. Replace the example Connect Route URLs.
2. Set the real namespace, Connect CR names, `connectRest` authentication references, and Argo application names.
3. Add all exact connector classes returned by `GET /connector-plugins` to `plugins.yaml`.
4. Add required keys and sensitive keys/patterns for each of the nine plugin families.
5. Add per-connector secret mappings before adopting connectors that currently contain plaintext-sensitive values.
6. Export the REST authentication environment variables referenced by `platforms.yaml`.

Validate configuration without contacting Connect:

```bash
./bin/connectorctl --config-dir config config-validate
```

## Common commands

Global flags must appear before the command.

### Inventory an environment

Inventory checks **all configured Connect profiles** for the physical cluster.

```bash
./bin/connectorctl inventory --cluster dev --env sit
```

It returns live count, Git count, adopted connectors, pending connectors, Git-only connectors, and counts per Connect profile.

### Adopt an entire logical environment in bounded batches

Dry run the next policy-sized batch:

```bash
./bin/connectorctl \
  --config-dir config \
  --repo-root . \
  adopt \
  --cluster dev \
  --env sit \
  --all-env \
  --validate-live
```

Inspect the `previewPath` values in the JSON result. The rendered files are written under `rendered/<run-id>/connectors/dev/sit/...`.

Write the validated batch to the worktree:

```bash
./bin/connectorctl adopt \
  --cluster dev \
  --env sit \
  --all-env \
  --validate-live \
  --server-dry-run \
  --ticket CHG000001 \
  --write
```

Repeat the command for the next batch. Already managed connectors are excluded automatically in `--all-env` mode.

Adopt a list:

```bash
./bin/connectorctl adopt \
  --cluster preprod \
  --env reg \
  --name ngil-reg-a,ngil-reg-b \
  --validate-live
```

Adopt names from CSV or a newline file:

```bash
./bin/connectorctl adopt \
  --cluster preprod \
  --env reg \
  --names-file reg-connectors.csv
```

CSV format:

```csv
connector_name
ngil-reg-a
ngil-reg-b
```

When a physical cluster has multiple existing Connect workers, mark every source profile with `adoptionSource: true`, or restrict a run explicitly:

```bash
./bin/connectorctl adopt \
  --cluster dev \
  --env sit \
  --source-profile default \
  --all-env
```

### Create connectors from JSON

A single file may be a raw `/config` object:

```json
{
  "connector.class": "io.confluent.connect.http.HttpSinkConnector",
  "tasks.max": "1",
  "topics": "topic-a",
  "http.api.url": "https://service.example/api"
}
```

Supply the name separately:

```bash
./bin/connectorctl create \
  --cluster dev \
  --env sit \
  --input request.json \
  --connector-name ngil-sit-consent-dncr \
  --validate-live
```

The file may also use the Kafka Connect create wrapper:

```json
{
  "name": "ngil-sit-consent-dncr",
  "config": {
    "connector.class": "io.confluent.connect.http.HttpSinkConnector",
    "tasks.max": "1"
  }
}
```

For multiple connectors, provide a directory containing one JSON file per connector. The wrapper `name` is preferred; otherwise the filename becomes the connector name. Bulk CREATE/UPDATE uses the same blast-radius limit as adoption; use `--batch-size` and `--batch-offset` when the directory contains more than the policy maximum. `--unsafe-all` is accepted only when explicitly enabled by policy.

### Update a managed connector

After adoption, Git is authoritative. UPDATE requires the connector already to exist in Git and, by default, live Connect. The supplied JSON must be the complete desired connector configuration, not a partial merge patch.

```bash
./bin/connectorctl update \
  --cluster dev \
  --env sit \
  --input changed-config.json \
  --connector-name ngil-sit-consent-dncr \
  --validate-live \
  --server-dry-run \
  --write
```

### Create a new logical environment from Git

MIGRATE clones already managed CRs rather than returning to live REST as the source of truth.

```bash
./bin/connectorctl migrate \
  --source-cluster dev \
  --source-env sit \
  --target-cluster dev \
  --target-env e2e \
  --all-env \
  --plan examples/plans/sit-to-e2e.yaml \
  --validate-live
```

The plan changes only explicitly listed configuration fields. Sensitive fields must have target-environment mappings; source secret references are never silently reused.

### Validate the generated repository

```bash
./bin/connectorctl repo-validate
```

This checks structure, duplicate identities, environment and class placement, profile discovery data, required plugin fields, and external references for every sensitive key.

### Verify after ArgoCD sync

```bash
./bin/connectorctl verify \
  --cluster dev \
  --env sit \
  --all-env \
  --batch-size 25
```

A connector fails verification if its connector state or any task state is not `RUNNING`.

### Operational actions

Status uses Connect REST. Pause, resume, restart, and task restart annotate the CFK `Connector` CR through the configured `oc`/`kubectl` executable.

```bash
./bin/connectorctl ops \
  --cluster dev \
  --env sit \
  --operation restart \
  --name ngil-sit-consent-dncr \
  --write
```

```bash
./bin/connectorctl ops \
  --cluster dev \
  --env sit \
  --operation restart-task \
  --task-id 0 \
  --name ngil-sit-consent-dncr \
  --write
```

Configure ArgoCD to ignore only the four transient operational annotations. An example is under `examples/argocd/`.

### Delete

DELETE removes the managed YAML from the worktree. Whether that removes the live connector depends on ArgoCD pruning policy.

```bash
./bin/connectorctl delete \
  --cluster dev \
  --env sit \
  --name ngil-sit-consent-dncr \
  --ticket CHG000002 \
  --confirm-delete \
  --write
```

Use stronger review and approval for deletion than for normal updates.


## Deliberate implementation boundaries

- The converter creates and validates secret **references**; it does not create Vault roles, write credential values, or patch Connect secret mounts. Verify those prerequisites before ArgoCD sync.
- Git branch creation, commit/push, merge-request creation, approval, and ArgoCD sync remain Jenkins responsibilities.
- Replace the sample `github.com/example/connectorctl` module path when importing this project into an organizational repository; it does not affect local compilation.
- `config/plugins.yaml` is intentionally incomplete. Bulk migration must not begin until all exact classes and sensitive fields used in the estate are registered.

## Secrets

The converter never persists or logs plaintext-sensitive values. It accepts already externalized `${file:...}`, `${secret:...}`, and `${env:...}` references, or replaces a detected sensitive key using a mapping such as:

```yaml
apiVersion: connectorctl.io/v1alpha1
connectors:
  ngil-sit-ciam-pgsql:
    fields:
      database.user:
        profile: vault-db-static
        secretName: sit-ciam-postgres
        secretKey: username
      database.password:
        profile: vault-db-static
        secretName: sit-ciam-postgres
        secretKey: password
```

This generates:

```text
${file:/mnt/secrets/sit-ciam-postgres/custom.properties:password}
```

The referenced Kubernetes Secret, VSO resource, and Connect volume mount must exist independently. The tool validates references; it does not write credentials into Vault or create arbitrary Vault roles.

## Connect profile relocation

ADOPT deliberately preserves the source Connect profile. It will not silently move an existing PostgreSQL or Oracle connector from the current Connect worker to a new rotating-DB worker. Such a move is a separate cutover requiring duplicate-name, offset/state, downtime, and rollback planning.

## Reports

Every workflow writes a JSON report by default under `reports/`. Reports contain connector names, paths, plugin aliases, Connect profiles, status, warnings, and errors, but not downloaded secret values.

## Container image

```bash
docker build --build-arg VERSION=0.1.0 -t connectorctl:0.1.0 .
```

The runtime image contains `connectorctl` but not the OpenShift CLI. REST-driven commands work directly. `--server-dry-run` and annotation operations require an image derived from this one with a compatible `oc` or `kubectl` binary installed, or execution on a Jenkins agent that already provides it.

## Recommended rollout

1. Finalize the nine plugin registry entries.
2. Prove one non-critical connector in SIT.
3. Prove one connector with secret replacement.
4. Adopt batches of 5-10 connectors by class or environment.
5. Run inventory and verify after every Argo sync.
6. Increase to 20-25 only after repeated clean batches.
7. Enable an unbounded environment run only after the adoption behavior is proven for your exact CFK version.

See `docs/ARCHITECTURE.md`, `docs/CONFIGURATION.md`, and `docs/JENKINS.md` for the detailed design.

## Container note

The supplied Dockerfile builds a small conversion/runtime image. Mount the checked-out Git repository (including `config/` and `connectors/`) at `/workspace`. The final UBI image does not bundle `oc` or `kubectl`; commands using `--server-dry-run` and annotation-based operations require the executable selected by `--kubectl-bin` to be available in the Jenkins agent/container. Use an approved OpenShift CLI base image, a sidecar/tool container, or add the client binary through the organization's normal image supply chain.
# kafka-connectorctl
