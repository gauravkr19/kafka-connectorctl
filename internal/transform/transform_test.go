package transform

import (
	"strings"
	"testing"

	"github.com/example/connectorctl/internal/config"
	"github.com/example/connectorctl/internal/model"
)

func TestKubernetesNameStableAndValid(t *testing.T) {
	input := "NGIL_SIT.Some Connector.With.A.Very.Long.Name-That-Exceeds-The-Normal-Kubernetes-Label-Limit-1234567890"
	one := KubernetesName(input)
	two := KubernetesName(input)
	if one != two {
		t.Fatalf("name is not stable: %q vs %q", one, two)
	}
	if len(one) > 63 {
		t.Fatalf("name exceeds 63 chars: %d", len(one))
	}
	if strings.ContainsAny(one, "_. ") || one != strings.ToLower(one) {
		t.Fatalf("name is not a DNS label: %q", one)
	}
}

func TestNormalizeBuildAndMarshal(t *testing.T) {
	raw := map[string]string{
		"connector.class": "io.example.HttpConnector",
		"tasks.max":       "2",
		"topics":          "topic-a",
	}
	n, err := Normalize("ngil-sit-http", "dev", "sit", "http", "created", "CHG1", raw, "default")
	if err != nil {
		t.Fatal(err)
	}
	cluster := config.Cluster{Namespace: "ns", ConnectProfiles: map[string]config.ConnectProfile{
		"default": {ConnectClusterRef: &model.NamespacedNameRef{Name: "connect"}, ConnectRest: map[string]any{"endpoint": "https://connect"}},
	}}
	cr, err := NewRenderer().Build(n, cluster)
	if err != nil {
		t.Fatal(err)
	}
	if cr.Spec.Class != "io.example.HttpConnector" || cr.Spec.TaskMax != 2 || cr.Spec.Configs["topics"] != "topic-a" {
		t.Fatalf("unexpected CR: %#v", cr)
	}
	if cr.Metadata.Labels["connectorctl.io/connect-profile"] != "default" {
		t.Fatalf("missing profile label: %#v", cr.Metadata.Labels)
	}
	data, err := NewRenderer().Marshal(cr)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, expected := range []string{"apiVersion: platform.confluent.io/v1beta1", "kind: Connector", "taskMax: 2", "topics: topic-a"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("rendered YAML does not contain %q:\n%s", expected, text)
		}
	}
}

func TestAdoptionDiffTreatsSecretReferenceAndDefaultTaskAsEquivalent(t *testing.T) {
	live := map[string]string{"connector.class": "io.example.C", "password": "plain", "name": "ignored"}
	generated := map[string]string{"connector.class": "io.example.C", "tasks.max": "1", "password": "${file:/x:p}"}
	if diffs := AdoptionDiff(live, generated, []string{"password"}); len(diffs) != 0 {
		t.Fatalf("unexpected diff: %#v", diffs)
	}
}
