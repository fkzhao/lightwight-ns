package discovery

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
)

func TestResolveOverBothTransports(t *testing.T) {
	for _, network := range []string{"tcp", "unix"} {
		t.Run(network, func(t *testing.T) {
			address := "127.0.0.1:0"
			if network == "unix" {
				dir, err := os.MkdirTemp("/tmp", "ns-sdk-")
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { os.RemoveAll(dir) })
				address = filepath.Join(dir, "agent.sock")
			}
			listener, err := net.Listen(network, address)
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			go func() {
				connection, err := listener.Accept()
				if err != nil {
					return
				}
				defer connection.Close()
				scanner := bufio.NewScanner(connection)
				for id := 1; id <= 2 && scanner.Scan(); id++ {
					result := `{"registryVersion":7,"endpoints":[{"address":"10.0.0.1","port":8080}]}`
					if id == 2 {
						result = `{"ready":true,"registryVersion":7,"lastSyncAt":"2026-01-01T00:00:00Z"}`
					}
					fmt.Fprintf(connection, `{"jsonrpc":"2.0","id":%d,"result":%s}`+"\n", id, result)
				}
			}()
			var sdk *Client
			if network == "tcp" {
				sdk, err = DialTCP(listener.Addr().String())
			} else {
				sdk, err = DialUnix(listener.Addr().String())
			}
			if err != nil {
				t.Fatal(err)
			}
			defer sdk.Close()
			endpoints, err := sdk.Resolve(context.Background(), "orders")
			if err != nil || len(endpoints) != 1 || endpoints[0].Address != "10.0.0.1" {
				t.Fatalf("unexpected endpoints: %+v, %v", endpoints, err)
			}
			status, err := sdk.AgentStatus(context.Background())
			if err != nil || !status.Ready || status.RegistryVersion != 7 {
				t.Fatalf("unexpected status: %+v, %v", status, err)
			}
		})
	}
}
