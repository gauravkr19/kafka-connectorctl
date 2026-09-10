# Architecture

## Ownership boundaries

| Component | Responsibility |
|---|---|
| Jenkins | Collect parameters, inject REST credentials, execute dry run/write, create branch/MR, enforce approval |
| connectorctl | Guardrails, REST discovery, concurrency, class lookup, secret reference resolution, validation, CFK rendering, worktree changes, reports |
| Git | Desired-state source of truth after adoption |
| ArgoCD | Diff and reconciliation only |
| CFK | Reconcile `Connector` CRs through Connect REST |
| Vault/VSO/Kubernetes | Create and rotate the referenced credentials |

## Resource transformation

For every Kafka Connect config:

```text
connector.class -> spec.class
tasks.max       -> spec.taskMax
remaining keys  -> spec.configs
connector name  -> spec.name
safe name       -> metadata.name
```

CFK-only discovery and management fields come from `platforms.yaml`:

```text
spec.connectClusterRef
spec.connectRest
metadata.namespace
spec.restartPolicy
```

No renderer infers those values from the downloaded Kafka Connect config because they are not connector plugin properties.

## Plugin registry

All connector classes use the same structural conversion. Plugin-specific behavior is limited to:

- alias/folder name;
- required keys;
- sensitive exact keys and patterns;
- default Connect profile;
- source/sink metadata.

Unknown classes fail before rendering.

## Profile-aware discovery

A physical Kafka cluster may have several Connect CRs. Each is a `connectProfile` with:

- its management REST Route and credentials;
- its CFK `connectClusterRef` and `connectRest` values;
- rate and retry settings;
- whether it is searched for existing connectors during adoption.

A connector name found on more than one profile is treated as an error.

## Concurrency

- Connect profiles are queried concurrently.
- Connector configs/statuses are fetched through a bounded worker pool.
- Every Connect client has its own token-bucket rate limiter.
- Retryable network errors and HTTP 409, 429, and selected 5xx responses use bounded exponential backoff.
- The policy caps user-requested worker count.

## Batch consistency

Rendering and live validation happen concurrently, but worktree mutation is deferred.

1. Every output path is preflighted.
2. Every connector in the selected batch must pass.
3. Writes/removals are applied as a batch.
4. If an apply step fails, previously changed paths are restored.
5. If validation of one connector fails, no source-of-truth CR is written for the batch.

Dry-run preview files are outside the connector source tree and may be produced for successful items even when another item fails. This supports diagnosis without creating partial Git desired state.

## ADOPT

- Discover names from configured adoption-source profiles.
- Filter only by the anchored logical-environment naming rule.
- Fetch `/config` and `/status` for each selected connector.
- Read `connector.class` from config.
- Preserve the source profile.
- Externalize sensitive values according to mappings.
- Validate class installation and configuration against that same profile.
- Compare all non-sensitive fields with the generated CFK representation.
- Stage and write the CR batch.

## CREATE

- Read one JSON file or a directory of files.
- Accept raw config or `{name, config}` wrapper format.
- Reject a name already present in any target Connect profile when live checking is enabled.
- Select the profile from connector mapping, plugin default, or environment default.
- Validate and stage the final CR.
- Apply the configured batch limit to JSON input directories as well as name/CSV selections.

## UPDATE

- Locate the existing Git CR first.
- Preserve its Connect profile.
- Require the live connector by default.
- If class alias changes, move the file to the new class directory as part of the same worktree batch.

## MIGRATE

- Use source Git CRs, not live REST, as input.
- Replace the logical environment token in the connector name.
- Apply only explicit field replacements and overrides.
- Require target mappings for every sensitive field.
- Reject names already present live or in Git.

## DELETE and operations

DELETE removes Git files; it never directly calls Kafka Connect DELETE. Operational actions use CFK annotations and remain outside Git desired configuration.
