package connect

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/example/connectorctl/internal/config"
)

func TestClientRESTOperationsAndRetry(t *testing.T) {
	var listCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/connectors":
			if listCalls.Add(1) == 1 {
				http.Error(w, `{"message":"rebalance"}`, http.StatusServiceUnavailable)
				return
			}
			_ = json.NewEncoder(w).Encode([]string{"b", "a"})
		case r.Method == http.MethodGet && r.URL.Path == "/connectors/a/config":
			_ = json.NewEncoder(w).Encode(map[string]any{"connector.class": "io.example.C", "tasks.max": json.Number("3"), "enabled": true})
		case r.Method == http.MethodGet && r.URL.Path == "/connectors/a/status":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"name": "a", "connector": map[string]any{"state": "RUNNING", "worker_id": "w"},
				"tasks": []map[string]any{{"id": 0, "state": "RUNNING", "worker_id": "w"}},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/connector-plugins":
			_ = json.NewEncoder(w).Encode([]map[string]string{{"class": "io.example.C", "type": "source", "version": "1"}})
		case r.Method == http.MethodPut && r.URL.Path == "/connector-plugins/io.example.C/config/validate":
			_ = json.NewEncoder(w).Encode(map[string]any{"name": "io.example.C", "error_count": 0})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := NewClient(config.RESTConfig{
		BaseURL: server.URL, Auth: config.RESTAuth{Type: "none"}, Timeout: "3s", RPS: 100, Burst: 10, Retries: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	names, err := client.ListConnectors(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 2 || names[0] != "a" || names[1] != "b" || listCalls.Load() != 2 {
		t.Fatalf("unexpected list/retry result: %#v calls=%d", names, listCalls.Load())
	}
	cfg, err := client.GetConfig(ctx, "a")
	if err != nil {
		t.Fatal(err)
	}
	if cfg["tasks.max"] != "3" || cfg["enabled"] != "true" {
		t.Fatalf("unexpected normalized config: %#v", cfg)
	}
	status, err := client.GetStatus(ctx, "a")
	if err != nil || status.Connector.State != "RUNNING" || len(status.Tasks) != 1 {
		t.Fatalf("unexpected status %#v err=%v", status, err)
	}
	plugins, err := client.ListPlugins(ctx)
	if err != nil || len(plugins) != 1 || plugins[0].Class != "io.example.C" {
		t.Fatalf("unexpected plugins %#v err=%v", plugins, err)
	}
	validation, err := client.ValidateConfig(ctx, "io.example.C", cfg)
	if err != nil || validation.ErrorCount != 0 {
		t.Fatalf("unexpected validation %#v err=%v", validation, err)
	}
}

func TestAPIErrorSuppressesResponseMessage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error_code":400,"message":"password=do-not-log"}`))
	}))
	defer server.Close()
	client, err := NewClient(config.RESTConfig{BaseURL: server.URL, Auth: config.RESTAuth{Type: "none"}, Timeout: "2s", RPS: 100, Burst: 1, Retries: 1})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.ListConnectors(context.Background())
	if err == nil {
		t.Fatal("expected REST error")
	}
	if strings.Contains(err.Error(), "do-not-log") || !strings.Contains(err.Error(), "response message suppressed") {
		t.Fatalf("unsafe or unexpected error: %v", err)
	}
}
