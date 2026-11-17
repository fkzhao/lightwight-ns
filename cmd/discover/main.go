package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"

	"github.com/fkzhao/lightwight-ns/client"
)

func main() {
	if len(os.Args) != 2 || os.Args[1] == "" {
		fmt.Fprintln(os.Stderr, "usage: go run ./cmd/discover <service-name>")
		os.Exit(2)
	}
	socket := os.Getenv("NS_AGENT_SOCKET")
	if socket == "" {
		socket = client.DefaultSocketPath
	}
	discovery, err := client.Dial(socket)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			log.Fatalf("local agent socket %s does not exist; start the agent with: go run ./cmd/agent", socket)
		}
		log.Fatal(err)
	}
	defer discovery.Close()
	endpoints, err := discovery.Discover(os.Args[1])
	if err != nil {
		log.Fatal(err)
	}
	addresses := make([]string, 0, len(endpoints))
	for _, endpoint := range endpoints {
		addresses = append(addresses, net.JoinHostPort(endpoint.Address, strconv.Itoa(endpoint.Port)))
	}
	if err := json.NewEncoder(os.Stdout).Encode(addresses); err != nil {
		log.Fatal(err)
	}
}
