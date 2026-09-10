connectorctl – Human Input Guide
This guide summarizes the human/Jenkins inputs confirmed from the current `connectorctl` code.
Recommended Jenkins Layout
Put Operation first, so Jenkins can show only the fields relevant to that operation.
```text
Operation:         ADOPT
Physical Cluster: DEV
Logical Env:      SIT

Connector names:
ngil-sit-orders-pg
ngil-sit-customer-pg

Change Ticket:    CHG123456
Dry Run:          Yes
```
For ADOPT, the multiline connector-name field is a good human-facing design.
---
ADOPT
Human input
ADOPT requires:
Physical cluster
Logical environment
One or more exact connector names
Optional/required change ticket and validation options according to policy
Recommended Jenkins connector field:
```text
ngil-sit-orders-pg
ngil-sit-customer-pg
ngil-sit-payment-pg
```
Jenkins can write this to a temporary text file and invoke:
```bash
connectorctl adopt   --cluster dev   --env sit   --names-file connectors.txt
```
Other supported CLI forms
Single name:
```bash
--name ngil-sit-orders-pg
```
Repeated flag:
```bash
--name ngil-sit-orders-pg --name ngil-sit-customer-pg
```
Comma-separated:
```bash
--name ngil-sit-orders-pg,ngil-sit-customer-pg
```
Names file:
```bash
--names-file connectors.txt
```
`readNamesFile()` supports:
One connector per line.
CSV with one connector per row in the first column.
Example CSV:
```csv
connector_name
ngil-sit-orders-pg
ngil-sit-customer-pg
```
A single row such as:
```text
connector-a,connector-b,connector-c
```
should not be used with `--names-file`, because the current CSV reader consumes only the first column.
Code path to remember
```text
selectionFlags / stringList.Set()
        ↓
SelectionOptions
        ↓
selectNames()
        ↓
applyBatchLimits()
        ↓
Service.Adopt()
        ↓
runConcurrent(names)
```
Important functions:
`stringList.Set()` – parses repeated/comma-separated `--name`
`readNamesFile()` – parses newline/CSV files
`selectNames()` – deduplicates and validates selected names
`Service.Adopt()` – performs adoption
`runConcurrent()` – processes selected connectors concurrently
ADOPT needs only connector names because Go retrieves each connector's actual configuration from the live Connect REST API.
Already-managed connectors found in Git are skipped.
---
CREATE
CREATE requires connector configuration JSON.
Single CREATE
```text
Operation:         CREATE
Physical Cluster: DEV
Logical Env:      SIT
JSON input:        connector.json
```
CLI:
```bash
--input connector.json
```
Optional single-file name override:
```bash
--connector-name ngil-sit-orders-pg
```
Bulk CREATE
Pass a directory:
```text
create-inputs/
├── connector-a.json
├── connector-b.json
└── connector-c.json
```
CLI:
```bash
--input create-inputs/
```
`LoadJSONInputs()` loads all `.json` files directly inside the directory. It does not recurse into subdirectories.
Connector-name resolution
`parseJSONFile()` determines the connector name in this order:
```text
1. --connector-name
2. wrapper JSON "name"
3. filename without .json
```
Wrapper JSON:
```json
{
  "name": "ngil-sit-orders-pg",
  "config": {
    "connector.class": "...",
    "tasks.max": "1"
  }
}
```
Here the wrapper `"name"` is used.
Raw config JSON:
```json
{
  "connector.class": "...",
  "tasks.max": "1"
}
```
If its filename is:
```text
ngil-sit-orders-pg.json
```
then the connector name becomes:
```text
ngil-sit-orders-pg
```
For bulk directory input, connector names therefore come from either the wrapper `"name"` or each JSON filename.
Code path to remember
```text
runCreateOrUpdate()
        ↓
CreateOptions
        ↓
Service.Create()
        ↓
createOrUpdate()
        ↓
repository.LoadJSONInputs()
        ↓
parseJSONFile()
        ↓
applyBatchLimits()
        ↓
runConcurrent(names)
```
---
UPDATE
UPDATE uses the same input format as CREATE:
```bash
--input connector.json
```
or:
```bash
--input update-inputs/
```
The same connector-name precedence applies:
```text
--connector-name
    ↓
wrapper "name"
    ↓
filename
```
Code path:
```text
runCreateOrUpdate(update=true)
        ↓
Service.Update()
        ↓
createOrUpdate()
```
---
DELETE
The DELETE input contract has not yet been verified from the supplied code excerpts.
Before finalizing its Jenkins fields, inspect the DELETE CLI/service code for:
exact-name versus bulk support
change-ticket requirements
Git-only versus live deletion behavior
dry-run/approval controls
---
STATUS / PAUSE / RESUME / RESTART / RESTART-TASK
The exact Jenkins input contract for these runtime operations has not yet been verified from the supplied code excerpts.
Their CLI/service implementation should be checked before publishing final Jenkins fields.
At minimum, connector name is expected; `RESTART-TASK` may additionally require a task ID.
---
Environment Name Matching
`matchesEnv()` performs:
```go
re, err := regexp.Compile(pattern)
return err == nil && re.MatchString(name)
```
Go regex matching is case-sensitive by default.
For:
```yaml
nameRegex: '^ngil-(?i:sit)-'
```
these match:
```text
ngil-sit-...
ngil-SIT-...
ngil-Sit-...
```
but this does not:
```text
NGIL-sit-...
```
because only the `sit` group is case-insensitive.
The regex should be treated as an environment validation guardrail, not as the authority deciding which connectors ADOPT processes.
---
Recommended Jenkins UX
ADOPT
```text
Operation:         ADOPT
Physical Cluster: DEV
Logical Env:      SIT

Connector names:
ngil-sit-orders-pg
ngil-sit-customer-pg

Change Ticket:    CHG123456
Dry Run:          Yes
```
Jenkins should convert the multiline connector field into a newline-separated `connectors.txt` and pass:
```bash
--names-file connectors.txt
```
CREATE / UPDATE
Prefer JSON input rather than asking for connector names separately:
```text
Operation:         CREATE
Physical Cluster: DEV
Logical Env:      SIT
Connector JSON:   <uploaded/staged JSON input>
Change Ticket:    CHG123456
Dry Run:          Yes
```
For bulk CREATE/UPDATE, Jenkins should stage multiple JSON files into one workspace directory and pass the directory to `--input`.
---
Quick Mental Model
```text
ADOPT
  Human supplies connector names
  Go fetches config from live Connect REST

CREATE
  Human supplies JSON file(s)

UPDATE
  Human supplies JSON file(s)

DELETE / runtime operations
  Verify their CLI/service code before fixing Jenkins inputs
```