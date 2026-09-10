# Jenkins integration

## Recommended parameters

| Parameter | Values |
|---|---|
| `PHYSICAL_CLUSTER` | `dev`, `preprod`, later `prod` |
| `LOGICAL_ENV` | environment key; converter independently validates the mapping |
| `ACTION` | `ADOPT`, `CREATE`, `UPDATE`, `MIGRATE`, `DELETE`, `STATUS`, `PAUSE`, `RESUME`, `RESTART`, `RESTART_TASK` |
| `SELECTOR_MODE` | `SINGLE`, `LIST`, `CSV`, `ENVIRONMENT` |
| `CONNECTORS` | comma/newline list when applicable |
| `INPUT_PATH` | JSON file or directory for CREATE/UPDATE |
| `SOURCE_ENV` | MIGRATE only |
| `MIGRATION_PLAN` | MIGRATE only |
| `DRY_RUN_ONLY` | default `true` |
| `CHANGE_TICKET` | required by policy for destructive operations |
| `TASK_ID` | restart-task only |

Do not ask users for Route URLs, namespace, `connectClusterRef`, `connectRest`, bearer Secret name, Git path, or Argo Application. Those are platform registries.

## Pipeline sequence

1. Checkout into a clean workspace.
2. Build/test the pinned source revision.
3. Run `config-validate`.
4. Export Connect REST credentials through Jenkins credentials binding.
5. Run the requested operation without `--write`.
6. Archive JSON report and rendered YAML preview.
7. Run human or policy approval.
8. Re-run against the same revision/input with `--write`.
9. Run `repo-validate` and `git diff --check`.
10. Commit to a branch and create an MR/PR.
11. Merge through normal approval.
12. Let ArgoCD reconcile.
13. Run `verify` after sync.

The second run is intentional. It protects against approving an unvalidated command while allowing the write invocation to re-check live state immediately before changing Git.

## Credentials

REST credentials are read from environment-variable names configured in `platforms.yaml`. Use `withCredentials` and keep shell tracing disabled around credential exports. The tool never includes those credentials in reports.

## Operations

Route `STATUS`, `PAUSE`, `RESUME`, `RESTART`, and `RESTART_TASK` to a separate restricted Jenkins stage or job. Its service account needs `get` and `patch` on CFK Connector resources, not broad write access to Secrets or Connect workloads.

An illustrative pipeline is provided as `Jenkinsfile.example`; adapt Git push and MR creation to the client Git service and shared libraries.
