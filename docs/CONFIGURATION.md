# Configuration registries

All YAML is decoded with unknown-field rejection.

## `platforms.yaml`

Defines the infrastructure and environment guardrail.

```yaml
clusters:
  dev:
    enabled: true
    namespace: dev-datalake
    gitRoot: connectors/dev
    argoApplication: kafka-connectors-dev
    logicalEnvironments:
      sit:
        nameRegex: '^ngil-(?i:sit)-'
        defaultConnectProfile: default
    connectProfiles:
      default:
        adoptionSource: true
        rest:
          baseURL: https://connect-route
          auth:
            type: basic
            usernameEnv: CONNECT_DEV_USER
            passwordEnv: CONNECT_DEV_PASSWORD
          timeout: 30s
          rps: 12
          burst: 12
          retries: 4
        connectClusterRef:
          name: connect-dev
          namespace: dev-datalake
        connectRest:
          endpoint: https://connect-route
          authentication:
            type: bearer
            bearer:
              secretRef: v2-kafka-apikeys
```

The management `rest` block is used by `connectorctl`. `connectClusterRef` and `connectRest` are copied into generated CFK CRs.

## `plugins.yaml`

```yaml
plugins:
  postgres:
    classes:
      - io.debezium.connector.postgresql.PostgresConnector
    type: source
    defaultConnectProfile: db-rotating
    required:
      - database.hostname
      - database.user
      - database.password
    sensitive:
      - database.user
      - database.password
    sensitivePatterns:
      - '(?i)^database\..*(password|secret|token)$'
```

Use exact class strings observed from the target Connect clusters. One class may belong to only one alias.

## `policies.yaml`

Controls concurrency, initial adoption batch size, write requirements, validation, and allowed operational actions.

`create.requireDryRun` and `update.requireDryRun` mean a write invocation must include `--server-dry-run`; a previous local dry run cannot be proven statelessly by the CLI.

## `secret-profiles.yaml`

Profiles describe reference construction only. They do not contain credential values.

```yaml
profiles:
  vault-db-static:
    type: VaultDynamicSecret-static-role
    mountRoot: /mnt/secrets
    defaultFile: custom.properties
```

## `mappings/<cluster>/<env>.yaml`

Per-connector sensitive-field mappings and optional Connect profile selection:

```yaml
connectors:
  ngil-sit-ciam-pgsql:
    fields:
      database.password:
        profile: vault-db-static
        secretName: sit-ciam-postgres
        secretKey: password
```

During ADOPT, a mapping cannot override the actual source profile. This prevents an accidental worker relocation disguised as adoption.

## Migration plan

```yaml
apiVersion: connectorctl.io/v1alpha1
configReplacements:
  - fields:
      - database.hostname
      - http.api.url
    from: sit
    to: e2e
overrides:
  ngil-sit-special:
    targetName: ngil-e2e-special-v2
    connectProfile: default
    set:
      topics: e2e-topic
    remove:
      - obsolete.setting
```

Replacement rules are field-scoped; the tool does not globally replace arbitrary strings across every connector property.
