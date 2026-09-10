# Testing

The automated suite includes:

- strict registry loading and wrong environment/cluster guardrails;
- REST authentication, retries, response normalization, and rate-limited concurrent access;
- class mapping and deterministic CFK rendering;
- plaintext secret rejection and secret reference injection;
- environment-wide bounded adoption;
- multi-profile source preservation;
- CREATE, UPDATE, and Git-to-Git MIGRATE;
- inventory and post-sync verification;
- repository validation;
- dry-run previews;
- no partial worktree write when one item in a batch fails;
- batch write/remove preflight.

Run:

```bash
make test
make race
make vet
```

The integration-style workflow tests use in-process fake Kafka Connect REST servers and do not require a cluster.

Before enabling a real Argo sync, also test against the target cluster:

```bash
connectorctl adopt ... --server-dry-run
connectorctl repo-validate
```

Then use one non-critical connector to confirm the exact CFK version's behavior when a CR is introduced for a connector that already exists in Connect.
