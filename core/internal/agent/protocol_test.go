package agent

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"testing"

	"github.com/fkzhao/lightwight-ns/core/internal/registry"
)

func TestJSONRPCDiscovery(t *testing.T) {
	state := &State{}
	state.Apply(registry.Snapshot{Version: 4, Services: []registry.Service{{
		Name: "orders", Instances: []registry.Instance{
			{Address: "10.0.0.1", Port: 8080, Enabled: true, Health: registry.HealthHealthy},
			{Address: "10.0.0.2", Port: 8080, Enabled: true, Health: registry.HealthUnhealthy},
		},
	}}})
	client, server := net.Pipe()
	defer client.Close()
	go ServeConn(server, state)
	if _, err := fmt.Fprintln(client, `{"jsonrpc":"2.0","id":1,"method":"discovery.resolve","params":{"service":"orders"}}`); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(client).ReadBytes('\n')
	if err != nil {
		t.Fatal(err)
	}
	var response struct {
		ID     int `json:"id"`
		Result struct {
			RegistryVersion uint64              `json:"registryVersion"`
			Endpoints       []registry.Endpoint `json:"endpoints"`
		} `json:"result"`
	}
	if err := json.Unmarshal(line, &response); err != nil {
		t.Fatal(err)
	}
	if response.ID != 1 || response.Result.RegistryVersion != 4 || len(response.Result.Endpoints) != 1 || response.Result.Endpoints[0].Address != "10.0.0.1" {
		t.Fatalf("unexpected JSON-RPC response: %s", line)
	}
}
