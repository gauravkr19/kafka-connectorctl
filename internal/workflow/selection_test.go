package workflow

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestReadSingleColumnCSV(t *testing.T) {
	path := filepath.Join(t.TempDir(), "connectors.csv")
	if err := os.WriteFile(path, []byte("connector_name\nngil-sit-a\nngil-sit-b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := readNamesFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"ngil-sit-a", "ngil-sit-b"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("readNamesFile() = %#v, want %#v", got, want)
	}
}

func TestExplicitSelectionCannotBypassBatchPolicy(t *testing.T) {
	names := []string{"ngil-sit-a", "ngil-sit-b", "ngil-sit-c"}
	_, err := selectNames(nil, `^ngil-(?i:sit)-`, SelectionOptions{Names: names}, false, 2, 2)
	if err == nil {
		t.Fatal("expected an oversized explicit selection to fail")
	}
	got, err := selectNames(nil, `^ngil-(?i:sit)-`, SelectionOptions{Names: names, BatchSize: 2}, false, 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"ngil-sit-a", "ngil-sit-b"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("bounded selection = %#v, want %#v", got, want)
	}
}

func TestDefaultMigratedNameReplacesOnlyEnvironmentToken(t *testing.T) {
	got := defaultMigratedName("ngil-sit-my-sit-app", "sit", "e2e")
	want := "ngil-e2e-my-sit-app"
	if got != want {
		t.Fatalf("defaultMigratedName() = %q, want %q", got, want)
	}
}

func TestPluginInstalledDoesNotUseShortNameForQualifiedClass(t *testing.T) {
	installed := map[string]struct{}{"other.vendor.PostgresConnector": {}}
	if pluginInstalled("io.debezium.connector.postgresql.PostgresConnector", installed) {
		t.Fatal("fully qualified class must not match an unrelated short-name collision")
	}
	if !pluginInstalled("PostgresConnector", installed) {
		t.Fatal("simple class alias should match an installed class short name")
	}
}
