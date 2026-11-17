package registration

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestRegisterAndClose(t *testing.T) {
	var mu sync.Mutex
	var methods []string
	var tokens []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var input map[string]any
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Errorf("decode request: %v", err)
		}
		mu.Lock()
		methods = append(methods, r.Method)
		tokens = append(tokens, input["token"].(string))
		mu.Unlock()
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"instanceId": "instance-1", "leaseTtlSeconds": 30})
	}))
	defer server.Close()
	lease, err := NewClient(server.URL).Register(context.Background(), "orders", "127.0.0.1", 8081)
	if err != nil {
		t.Fatal(err)
	}
	if lease.InstanceID() != "instance-1" {
		t.Fatalf("unexpected instance ID: %s", lease.InstanceID())
	}
	if err := lease.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := lease.Close(context.Background()); err != nil {
		t.Fatalf("second close should be safe: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(methods) != 2 || methods[0] != http.MethodPost || methods[1] != http.MethodDelete {
		t.Fatalf("unexpected methods: %v", methods)
	}
	if tokens[0] != tokens[1] || len(tokens[0]) != 64 || strings.Trim(tokens[0], "0123456789abcdef") != "" {
		t.Fatal("registration token was not a stable random hex token")
	}
}
