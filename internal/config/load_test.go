package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadBundleAndEnvironmentGuardrail(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "platforms.yaml", `
apiVersion: connectorctl.io/v1alpha1
clusters:
  dev:
    enabled: true
    namespace: ns-dev
    gitRoot: connectors/dev
    logicalEnvironments:
      sit:
        nameRegex: '^ngil-(?i:sit)-'
        defaultConnectProfile: default
    connectProfiles:
      default:
        adoptionSource: true
        rest:
          baseURL: https://connect-dev.example
          auth:
            type: none
        connectClusterRef:
          name: connect-dev
  preprod:
    enabled: true
    namespace: ns-preprod
    gitRoot: connectors/preprod
    logicalEnvironments:
      reg:
        nameRegex: '^ngil-(?i:reg)-'
        defaultConnectProfile: default
    connectProfiles:
      default:
        adoptionSource: true
        rest:
          baseURL: https://connect-preprod.example
          auth:
            type: none
        connectClusterRef:
          name: connect-preprod
`)
	writeTestFile(t, dir, "plugins.yaml", `
apiVersion: connectorctl.io/v1alpha1
plugins:
  http:
    classes: [io.example.HttpConnector]
`)
	writeTestFile(t, dir, "policies.yaml", validPoliciesYAML())
	writeTestFile(t, dir, "secret-profiles.yaml", validSecretProfilesYAML())

	bundle, err := LoadBundle(dir)
	if err != nil {
		t.Fatalf("LoadBundle() error = %v", err)
	}
	if _, _, err := bundle.ResolveTarget("dev", "sit"); err != nil {
		t.Fatalf("ResolveTarget(dev,sit) error = %v", err)
	}
	_, _, err = bundle.ResolveTarget("preprod", "sit")
	if err == nil || !strings.Contains(err.Error(), "mapped to physical cluster DEV") {
		t.Fatalf("expected mapping guardrail error, got %v", err)
	}
}

func TestLoadBundleRejectsUnknownField(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "platforms.yaml", `
apiVersion: connectorctl.io/v1alpha1
unknownTopLevel: true
clusters: {}
`)
	writeTestFile(t, dir, "plugins.yaml", "apiVersion: connectorctl.io/v1alpha1\nplugins: {}\n")
	writeTestFile(t, dir, "policies.yaml", validPoliciesYAML())
	writeTestFile(t, dir, "secret-profiles.yaml", validSecretProfilesYAML())
	_, err := LoadBundle(dir)
	if err == nil || !strings.Contains(err.Error(), "field unknownTopLevel not found") {
		t.Fatalf("expected strict YAML field error, got %v", err)
	}
}

func validPoliciesYAML() string {
	return `
apiVersion: connectorctl.io/v1alpha1
defaults:
  concurrency: 2
  maxConcurrency: 4
  batchSize: 2
adoption:
  maxBatchSize: 5
  allowWholeEnvironment: false
  requireFunctionalDiff: true
create:
  requireDryRun: false
update:
  requireDryRun: false
delete:
  requireManualApproval: true
  requireChangeTicket: true
validation:
  verifyPluginInstalled: false
  validateAgainstConnectAPI: false
  serverDryRun: false
  rejectPlaintextSecrets: true
operations:
  allowed: [status]
`
}

func validSecretProfilesYAML() string {
	return `
apiVersion: connectorctl.io/v1alpha1
profiles:
  mounted-file:
    type: kubernetes-mounted-secret
    mountRoot: /mnt/secrets
    defaultFile: custom.properties
security:
  neverPersistPlaintext: true
  neverLogSecretValues: true
`
}

func writeTestFile(t *testing.T, root, name, contents string) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.TrimSpace(contents)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}
