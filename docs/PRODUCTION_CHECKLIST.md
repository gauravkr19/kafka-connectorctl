# Production readiness checklist

Before the first real adoption batch:

- Replace every `.example.invalid` endpoint and verify TLS trust; do not enable `insecureSkipVerify` unless explicitly approved.
- Register every exact class returned by each target `/connector-plugins` endpoint.
- Review required and sensitive keys for representative configs from every plugin family, including JAAS strings, tokens, truststore/keystore passwords, and credentials embedded in URLs.
- Mark every existing Connect worker that contains connectors with `adoptionSource: true`, or select it explicitly with `--source-profile`.
- Confirm each generated `${file:...}` path is backed by a Secret mounted into the selected Connect CR.
- Confirm `connectClusterRef` and `connectRest` match the installed CFK CRD and the authentication Secret format.
- Run `config-validate`, `repo-validate`, unit tests, race tests, and a server-side dry run.
- Test ADOPT first with one non-critical connector, then a small class-specific batch, before enabling larger environment batches.
- Keep ArgoCD pruning disabled during initial adoption and use manual sync until the no-impact behavior is verified in the installed CFK/Connect versions.
- Compare live REST config, generated CR, connector state, task state, and message flow after the first sync.
- Test VSO/Secret rotation separately and observe whether the Connect StatefulSet rolls or a connector restart is required.
- Restrict Jenkins credentials and RBAC: REST read/validate for migration, Git write through review, and only `get/patch` on Connector CRs for operations.
