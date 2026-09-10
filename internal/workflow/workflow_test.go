package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/example/connectorctl/internal/model"
	"github.com/example/connectorctl/internal/yaml"
)

type fakeConnect struct {
	server  *httptest.Server
	mu      sync.RWMutex
	config  map[string]map[string]string
	plugins map[string]struct{}
	active  atomic.Int32
	max     atomic.Int32
}

func newFakeConnect(t *testing.T) *fakeConnect {
	t.Helper()
	f := &fakeConnect{
		config:  map[string]map[string]string{},
		plugins: map[string]struct{}{},
	}
	f.server = httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(f.server.Close)
	return f
}

func (f *fakeConnect) add(name string, cfg map[string]string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	copyCfg := map[string]string{}
	for k, v := range cfg {
		copyCfg[k] = v
	}
	f.config[name] = copyCfg
	if className := copyCfg["connector.class"]; className != "" {
		f.plugins[className] = struct{}{}
	}
}

func (f *fakeConnect) install(className string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.plugins[className] = struct{}{}
}

func (f *fakeConnect) handle(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	current := f.active.Add(1)
	defer f.active.Add(-1)
	for {
		previous := f.max.Load()
		if current <= previous || f.max.CompareAndSwap(previous, current) {
			break
		}
	}
	if strings.Contains(r.URL.Path, "/config") || strings.Contains(r.URL.Path, "/status") {
		time.Sleep(15 * time.Millisecond)
	}

	if r.Method == http.MethodGet && r.URL.Path == "/connectors" {
		f.mu.RLock()
		names := make([]string, 0, len(f.config))
		for name := range f.config {
			names = append(names, name)
		}
		f.mu.RUnlock()
		sort.Strings(names)
		_ = json.NewEncoder(w).Encode(names)
		return
	}
	if r.Method == http.MethodGet && r.URL.Path == "/connector-plugins" {
		f.mu.RLock()
		classes := make([]string, 0, len(f.plugins))
		for className := range f.plugins {
			classes = append(classes, className)
		}
		f.mu.RUnlock()
		sort.Strings(classes)
		plugins := make([]map[string]string, 0, len(classes))
		for _, className := range classes {
			plugins = append(plugins, map[string]string{"class": className, "type": "connector", "version": "test"})
		}
		_ = json.NewEncoder(w).Encode(plugins)
		return
	}
	if r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/connector-plugins/") && strings.HasSuffix(r.URL.Path, "/config/validate") {
		_ = json.NewEncoder(w).Encode(map[string]any{"name": "test", "error_count": 0})
		return
	}
	if strings.HasPrefix(r.URL.Path, "/connectors/") {
		remainder := strings.TrimPrefix(r.URL.Path, "/connectors/")
		parts := strings.Split(remainder, "/")
		if len(parts) != 2 {
			http.NotFound(w, r)
			return
		}
		name, suffix := parts[0], parts[1]
		f.mu.RLock()
		cfg, exists := f.config[name]
		copyCfg := map[string]string{}
		for k, v := range cfg {
			copyCfg[k] = v
		}
		f.mu.RUnlock()
		if !exists {
			http.NotFound(w, r)
			return
		}
		switch {
		case r.Method == http.MethodGet && suffix == "config":
			_ = json.NewEncoder(w).Encode(copyCfg)
			return
		case r.Method == http.MethodGet && suffix == "status":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"name":      name,
				"connector": map[string]any{"state": "RUNNING", "worker_id": "worker"},
				"tasks":     []map[string]any{{"id": 0, "state": "RUNNING", "worker_id": "worker"}},
			})
			return
		}
	}
	http.NotFound(w, r)
}

func httpConfig(url string) map[string]string {
	return map[string]string{
		"connector.class": "io.example.HttpConnector",
		"tasks.max":       "1",
		"topics":          "topic-a",
		"http.api.url":    url,
	}
}

func TestEnvironmentAdoptionBatchesInventoryAndVerify(t *testing.T) {
	fake := newFakeConnect(t)
	fake.add("ngil-sit-a", httpConfig("https://a"))
	fake.add("ngil-sit-b", httpConfig("https://b"))
	fake.add("ngil-sit-c", httpConfig("https://c"))
	fake.add("ngil-e2e-other", httpConfig("https://other"))

	root := t.TempDir()
	configDir := writeWorkflowConfig(t, root, map[string]profileFixture{
		"default": {URL: fake.server.URL, AdoptionSource: true, ConnectName: "connect-default"},
	}, 2, false)
	service, err := NewService(configDir, root, "", "oc")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	run, _, err := service.Adopt(ctx, AdoptOptions{Cluster: "dev", LogicalEnv: "sit", Selection: SelectionOptions{AllEnv: true}, DryRun: false, ValidateLive: true})
	if err != nil {
		t.Fatalf("first Adopt() error = %v; run=%#v", err, run)
	}
	if run.Succeeded != 2 || run.Failed != 0 {
		t.Fatalf("expected first bounded batch of 2, got %#v", run)
	}
	run, _, err = service.Adopt(ctx, AdoptOptions{Cluster: "dev", LogicalEnv: "sit", Selection: SelectionOptions{AllEnv: true}, DryRun: false, ValidateLive: true})
	if err != nil {
		t.Fatalf("second Adopt() error = %v; run=%#v", err, run)
	}
	if run.Succeeded != 1 {
		t.Fatalf("expected remaining connector only, got %#v", run)
	}
	if fake.max.Load() < 2 {
		t.Fatalf("expected concurrent REST activity, maximum in-flight requests was %d", fake.max.Load())
	}

	inventory, err := service.Inventory(ctx, "dev", "sit")
	if err != nil {
		t.Fatal(err)
	}
	if inventory.LiveCount != 3 || inventory.GitCount != 3 || len(inventory.Pending) != 0 || len(inventory.Adopted) != 3 {
		t.Fatalf("unexpected inventory: %#v", inventory)
	}
	validation, err := service.ValidateRepository()
	if err != nil {
		t.Fatalf("ValidateRepository() error = %v, result=%#v", err, validation)
	}
	if validation.FilesChecked != 3 {
		t.Fatalf("expected 3 files checked, got %#v", validation)
	}
	verify, _, err := service.Verify(ctx, VerifyOptions{Cluster: "dev", LogicalEnv: "sit", Selection: SelectionOptions{AllEnv: true, UnsafeAll: true}})
	if err != nil || verify.Succeeded != 3 {
		t.Fatalf("Verify() run=%#v error=%v", verify, err)
	}
}

func TestCreateUpdateAndEnvironmentMigration(t *testing.T) {
	fake := newFakeConnect(t)
	fake.install("io.example.HttpConnector")
	root := t.TempDir()
	configDir := writeWorkflowConfig(t, root, map[string]profileFixture{
		"default": {URL: fake.server.URL, AdoptionSource: true, ConnectName: "connect-default"},
	}, 5, false)
	service, err := NewService(configDir, root, "", "oc")
	if err != nil {
		t.Fatal(err)
	}
	input := filepath.Join(root, "new.json")
	writeJSONFile(t, input, map[string]any{
		"name":   "ngil-sit-new-http",
		"config": httpConfig("https://service-sit.example/api"),
	})
	ctx := context.Background()
	created, _, err := service.Create(ctx, CreateOptions{Cluster: "dev", LogicalEnv: "sit", InputPath: input, DryRun: false, ValidateLive: true, CheckLive: true})
	if err != nil || created.Succeeded != 1 {
		t.Fatalf("Create() run=%#v error=%v", created, err)
	}
	fake.add("ngil-sit-new-http", httpConfig("https://service-sit.example/api"))

	updatedCfg := httpConfig("https://service-sit.example/v2")
	writeJSONFile(t, input, map[string]any{"name": "ngil-sit-new-http", "config": updatedCfg})
	updated, _, err := service.Update(ctx, CreateOptions{Cluster: "dev", LogicalEnv: "sit", InputPath: input, DryRun: false, ValidateLive: true, CheckLive: true})
	if err != nil || updated.Succeeded != 1 {
		t.Fatalf("Update() run=%#v error=%v", updated, err)
	}

	planPath := filepath.Join(root, "plan.yaml")
	if err := os.WriteFile(planPath, []byte(`
apiVersion: connectorctl.io/v1alpha1
configReplacements:
  - fields: [http.api.url]
    from: sit
    to: e2e
`), 0o644); err != nil {
		t.Fatal(err)
	}
	migrated, _, err := service.Migrate(ctx, MigrateOptions{
		SourceCluster: "dev", SourceEnv: "sit", TargetCluster: "dev", TargetEnv: "e2e",
		Selection: SelectionOptions{Names: []string{"ngil-sit-new-http"}}, PlanPath: planPath,
		DryRun: false, ValidateLive: true, CheckLive: true,
	})
	if err != nil || migrated.Succeeded != 1 {
		t.Fatalf("Migrate() run=%#v error=%v", migrated, err)
	}
	manifest, err := service.Repository.FindByConnectorName("connectors/dev/e2e", "ngil-e2e-new-http")
	if err != nil {
		t.Fatal(err)
	}
	if got := manifest.Connector.Spec.Configs["http.api.url"]; got != "https://service-e2e.example/v2" {
		t.Fatalf("migration replacement failed: %q", got)
	}
	if _, err := service.ValidateRepository(); err != nil {
		t.Fatalf("ValidateRepository() after migration: %v", err)
	}
}

func TestAdoptionPreservesSourceConnectProfile(t *testing.T) {
	defaultConnect := newFakeConnect(t)
	dbConnect := newFakeConnect(t)
	defaultConnect.add("ngil-sit-http", httpConfig("https://http"))
	dbConnect.add("ngil-sit-ciam-pgsql", map[string]string{
		"connector.class": "io.debezium.connector.postgresql.PostgresConnector",
		"tasks.max":       "1", "database.hostname": "db", "database.user": "plain-user", "database.password": "plain-password", "topic.prefix": "ciam",
	})
	root := t.TempDir()
	configDir := writeWorkflowConfig(t, root, map[string]profileFixture{
		"default":     {URL: defaultConnect.server.URL, AdoptionSource: true, ConnectName: "connect-default"},
		"db-rotating": {URL: dbConnect.server.URL, AdoptionSource: true, ConnectName: "connect-db"},
	}, 10, true)
	mappingDir := filepath.Join(configDir, "mappings", "dev")
	if err := os.MkdirAll(mappingDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mappingDir, "sit.yaml"), []byte(`
apiVersion: connectorctl.io/v1alpha1
connectors:
  ngil-sit-ciam-pgsql:
    fields:
      database.user:
        secretName: ciam-pg
        secretKey: username
      database.password:
        secretName: ciam-pg
        secretKey: password
`), 0o644); err != nil {
		t.Fatal(err)
	}
	service, err := NewService(configDir, root, "", "oc")
	if err != nil {
		t.Fatal(err)
	}
	reportsDir := filepath.Join(root, "reports")
	service.ReportsDir = reportsDir
	run, reportPath, err := service.Adopt(context.Background(), AdoptOptions{
		Cluster: "dev", LogicalEnv: "sit", Selection: SelectionOptions{AllEnv: true}, DryRun: false, ValidateLive: true,
	})
	if err != nil || run.Succeeded != 2 {
		t.Fatalf("Adopt() run=%#v error=%v", run, err)
	}
	pg, err := service.Repository.FindByConnectorName("connectors/dev/sit", "ngil-sit-ciam-pgsql")
	if err != nil {
		t.Fatal(err)
	}
	if got := pg.Connector.Metadata.Labels["connectorctl.io/connect-profile"]; got != "db-rotating" {
		t.Fatalf("PG profile = %q, want db-rotating", got)
	}
	if pg.Connector.Spec.Configs["database.password"] != "${file:/mnt/secrets/ciam-pg/custom.properties:password}" {
		t.Fatalf("secret mapping not applied: %#v", pg.Connector.Spec.Configs)
	}
	reportData, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(reportData), "plain-password") || strings.Contains(string(reportData), "plain-user") {
		t.Fatal("plaintext credentials leaked into the run report")
	}
	httpManifest, err := service.Repository.FindByConnectorName("connectors/dev/sit", "ngil-sit-http")
	if err != nil {
		t.Fatal(err)
	}
	if got := httpManifest.Connector.Metadata.Labels["connectorctl.io/connect-profile"]; got != "default" {
		t.Fatalf("HTTP profile = %q, want default", got)
	}
}

type profileFixture struct {
	URL            string
	AdoptionSource bool
	ConnectName    string
}

func writeWorkflowConfig(t *testing.T, root string, profiles map[string]profileFixture, batchSize int, includePostgres bool) string {
	t.Helper()
	configDir := filepath.Join(root, "config")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	var profileYAML strings.Builder
	profileNames := make([]string, 0, len(profiles))
	for name := range profiles {
		profileNames = append(profileNames, name)
	}
	sort.Strings(profileNames)
	for _, name := range profileNames {
		profile := profiles[name]
		fmt.Fprintf(&profileYAML, `
      %s:
        adoptionSource: %t
        rest:
          baseURL: %s
          auth:
            type: none
          timeout: 5s
          rps: 100
          burst: 20
          retries: 1
        connectClusterRef:
          name: %s
          namespace: test-ns
        connectRest:
          endpoint: %s
`, name, profile.AdoptionSource, profile.URL, profile.ConnectName, profile.URL)
	}
	platforms := fmt.Sprintf(`
apiVersion: connectorctl.io/v1alpha1
clusters:
  dev:
    enabled: true
    namespace: test-ns
    gitRoot: connectors/dev
    logicalEnvironments:
      sit:
        nameRegex: '^ngil-(?i:sit)-'
        defaultConnectProfile: default
      e2e:
        nameRegex: '^ngil-(?i:e2e)-'
        defaultConnectProfile: default
    connectProfiles:%s
`, profileYAML.String())
	writeText(t, filepath.Join(configDir, "platforms.yaml"), platforms)
	plugins := `
apiVersion: connectorctl.io/v1alpha1
plugins:
  http:
    classes: [io.example.HttpConnector]
    type: sink
    defaultConnectProfile: default
`
	if includePostgres {
		plugins += `
  postgres:
    classes: [io.debezium.connector.postgresql.PostgresConnector]
    type: source
    defaultConnectProfile: db-rotating
    sensitive: [database.user, database.password]
`
	}
	writeText(t, filepath.Join(configDir, "plugins.yaml"), plugins)
	writeText(t, filepath.Join(configDir, "policies.yaml"), fmt.Sprintf(`
apiVersion: connectorctl.io/v1alpha1
defaults:
  concurrency: 4
  maxConcurrency: 8
  batchSize: %d
adoption:
  maxBatchSize: 20
  allowWholeEnvironment: true
  requireFunctionalDiff: true
create:
  requireDryRun: false
update:
  requireDryRun: false
delete:
  requireManualApproval: true
  requireChangeTicket: true
validation:
  verifyPluginInstalled: true
  validateAgainstConnectAPI: true
  serverDryRun: false
  rejectPlaintextSecrets: true
  genericSensitivePatterns:
    - '(?i)(^|[._-])(password|secret|token)([._-]|$)'
operations:
  allowed: [status, pause, resume, restart, restart-task]
`, batchSize))
	writeText(t, filepath.Join(configDir, "secret-profiles.yaml"), `
apiVersion: connectorctl.io/v1alpha1
profiles:
  mounted-file:
    type: kubernetes-mounted-secret
    mountRoot: /mnt/secrets
    defaultFile: custom.properties
security:
  neverPersistPlaintext: true
  neverLogSecretValues: true
`)
	return configDir
}

func writeText(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.TrimSpace(text)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeJSONFile(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	writeText(t, path, string(data))
}

func loadConnectorFile(t *testing.T, path string) model.CFKConnector {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var cr model.CFKConnector
	if err := yaml.Unmarshal(data, &cr); err != nil {
		t.Fatal(err)
	}
	return cr
}

func TestAdoptionFailureDoesNotPartiallyWriteGit(t *testing.T) {
	fake := newFakeConnect(t)
	fake.add("ngil-sit-good", httpConfig("https://good"))
	fake.add("ngil-sit-unknown", map[string]string{
		"connector.class": "io.example.UnknownConnector",
		"tasks.max":       "1",
	})
	root := t.TempDir()
	configDir := writeWorkflowConfig(t, root, map[string]profileFixture{
		"default": {URL: fake.server.URL, AdoptionSource: true, ConnectName: "connect-default"},
	}, 10, false)
	service, err := NewService(configDir, root, "", "oc")
	if err != nil {
		t.Fatal(err)
	}
	run, _, err := service.Adopt(context.Background(), AdoptOptions{
		Cluster: "dev", LogicalEnv: "sit", Selection: SelectionOptions{AllEnv: true}, DryRun: false,
	})
	if err == nil || run.Failed != 1 || run.Skipped != 1 || run.Succeeded != 0 {
		t.Fatalf("expected one failed item and one unapplied/skipped item, run=%#v err=%v", run, err)
	}
	manifests, loadErr := service.Repository.LoadConnectors("connectors/dev/sit")
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if len(manifests) != 0 {
		t.Fatalf("failed batch must not partially modify Git, found %d manifests", len(manifests))
	}
}

func TestDryRunWritesPreviewButNotGit(t *testing.T) {
	fake := newFakeConnect(t)
	fake.add("ngil-sit-preview", httpConfig("https://preview"))
	root := t.TempDir()
	configDir := writeWorkflowConfig(t, root, map[string]profileFixture{
		"default": {URL: fake.server.URL, AdoptionSource: true, ConnectName: "connect-default"},
	}, 10, false)
	previewRoot := filepath.Join(root, "rendered")
	service, err := NewService(configDir, root, "", "oc", previewRoot)
	if err != nil {
		t.Fatal(err)
	}
	run, _, err := service.Adopt(context.Background(), AdoptOptions{
		Cluster: "dev", LogicalEnv: "sit", Selection: SelectionOptions{Names: []string{"ngil-sit-preview"}}, DryRun: true,
	})
	if err != nil || run.Succeeded != 1 {
		t.Fatalf("dry-run adoption run=%#v err=%v", run, err)
	}
	if run.Items[0].PreviewPath == "" {
		t.Fatal("expected previewPath in dry-run result")
	}
	if _, err := os.Stat(run.Items[0].PreviewPath); err != nil {
		t.Fatalf("preview file was not written: %v", err)
	}
	manifests, err := service.Repository.LoadConnectors("connectors/dev/sit")
	if err != nil {
		t.Fatal(err)
	}
	if len(manifests) != 0 {
		t.Fatalf("dry run wrote %d Git manifests", len(manifests))
	}
}

func TestInventoryIncludesNonAdoptionSourceProfiles(t *testing.T) {
	defaultConnect := newFakeConnect(t)
	dedicatedConnect := newFakeConnect(t)
	defaultConnect.add("ngil-sit-default", httpConfig("https://default"))
	dedicatedConnect.add("ngil-sit-dedicated", httpConfig("https://dedicated"))
	root := t.TempDir()
	configDir := writeWorkflowConfig(t, root, map[string]profileFixture{
		"default":   {URL: defaultConnect.server.URL, AdoptionSource: true, ConnectName: "connect-default"},
		"dedicated": {URL: dedicatedConnect.server.URL, AdoptionSource: false, ConnectName: "connect-dedicated"},
	}, 10, false)
	service, err := NewService(configDir, root, "", "oc")
	if err != nil {
		t.Fatal(err)
	}
	inventory, err := service.Inventory(context.Background(), "dev", "sit")
	if err != nil {
		t.Fatal(err)
	}
	if inventory.LiveCount != 2 || inventory.LiveByProfile["default"] != 1 || inventory.LiveByProfile["dedicated"] != 1 {
		t.Fatalf("inventory omitted a configured Connect profile: %#v", inventory)
	}
}

func TestDeleteUsesWorktreeBatch(t *testing.T) {
	fake := newFakeConnect(t)
	fake.add("ngil-sit-delete-a", httpConfig("https://a"))
	fake.add("ngil-sit-delete-b", httpConfig("https://b"))
	root := t.TempDir()
	configDir := writeWorkflowConfig(t, root, map[string]profileFixture{
		"default": {URL: fake.server.URL, AdoptionSource: true, ConnectName: "connect-default"},
	}, 10, false)
	service, err := NewService(configDir, root, "", "oc")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.Adopt(context.Background(), AdoptOptions{
		Cluster: "dev", LogicalEnv: "sit", Selection: SelectionOptions{AllEnv: true, UnsafeAll: true}, DryRun: false,
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.Delete(context.Background(), DeleteOptions{
		Cluster: "dev", LogicalEnv: "sit", Selection: SelectionOptions{AllEnv: true, UnsafeAll: true},
		DryRun: false, ChangeTicket: "CHG-1", Confirm: true,
	}); err != nil {
		t.Fatal(err)
	}
	manifests, err := service.Repository.LoadConnectors("connectors/dev/sit")
	if err != nil {
		t.Fatal(err)
	}
	if len(manifests) != 0 {
		t.Fatalf("delete left %d manifests", len(manifests))
	}
}
