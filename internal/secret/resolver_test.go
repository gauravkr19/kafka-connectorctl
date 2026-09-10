package secret

import (
	"strings"
	"testing"

	"github.com/example/connectorctl/internal/config"
)

func TestResolverReplacesMappedSensitiveFields(t *testing.T) {
	resolver := NewResolver(config.SecretProfilesFile{Profiles: map[string]config.SecretProfile{
		"mounted-file": {Type: "kubernetes-mounted-secret", MountRoot: "/mnt/secrets", DefaultFile: "custom.properties"},
	}})
	cfg := map[string]string{"database.user": "plain-user", "database.password": "plain-password", "database.hostname": "db"}
	mapping := config.ConnectorMapping{Fields: map[string]config.SecretFieldRef{
		"database.user":     {SecretName: "pg", SecretKey: "username"},
		"database.password": {SecretName: "pg", SecretKey: "password"},
	}}
	got, refs, err := resolver.Resolve("connector", cfg, []string{"database.user", "database.password"}, mapping)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if got["database.user"] != "${file:/mnt/secrets/pg/custom.properties:username}" {
		t.Fatalf("unexpected user reference %q", got["database.user"])
	}
	if got["database.password"] != "${file:/mnt/secrets/pg/custom.properties:password}" {
		t.Fatalf("unexpected password reference %q", got["database.password"])
	}
	if len(refs) != 2 {
		t.Fatalf("expected two applied refs, got %d", len(refs))
	}
	if cfg["database.password"] != "plain-password" {
		t.Fatal("resolver mutated its input map")
	}
}

func TestResolverFailsClosedWithoutMapping(t *testing.T) {
	resolver := NewResolver(config.SecretProfilesFile{Profiles: map[string]config.SecretProfile{"mounted-file": {Type: "mounted"}}})
	_, _, err := resolver.Resolve("connector", map[string]string{"password": "plain"}, []string{"password"}, config.ConnectorMapping{})
	if err == nil || !strings.Contains(err.Error(), "plaintext-sensitive") {
		t.Fatalf("expected plaintext rejection, got %v", err)
	}
}

func TestResolverPreservesExternalReference(t *testing.T) {
	resolver := NewResolver(config.SecretProfilesFile{Profiles: map[string]config.SecretProfile{"mounted-file": {Type: "mounted"}}})
	value := "${file:/mnt/secrets/x/custom.properties:password}"
	got, refs, err := resolver.Resolve("connector", map[string]string{"password": value}, []string{"password"}, config.ConnectorMapping{})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if got["password"] != value || len(refs) != 0 {
		t.Fatalf("external reference was not preserved: %#v %#v", got, refs)
	}
}
