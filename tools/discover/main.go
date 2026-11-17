package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"

	"github.com/fkzhao/lightwight-ns/sdk/go/discovery"
)

func main() {
	tcp := flag.Bool("tcp", false, "connect to the agent over TCP RPC")
	flag.Usage = func() { fmt.Fprintln(os.Stderr, "usage: go run ./tools/discover [-tcp] <service-name>") }
	flag.Parse()
	if flag.NArg() != 1 || flag.Arg(0) == "" {
		flag.Usage()
		os.Exit(2)
	}
	var sdk *discovery.Client
	var err error
	address := ""
	if *tcp {
		address = os.Getenv("NS_AGENT_TCP_ADDR")
		if address == "" {
			address = discovery.DefaultTCPAddress
		}
		sdk, err = discovery.DialTCP(address)
	} else {
		address = os.Getenv("NS_AGENT_SOCKET")
		if address == "" {
			address = discovery.DefaultSocketPath
		}
		sdk, err = discovery.DialUnix(address)
	}
	if err != nil {
		if !*tcp && errors.Is(err, os.ErrNotExist) {
			log.Fatalf("local agent socket %s does not exist; start the agent with: go run ./core/agent", address)
		}
		log.Fatalf("connect to local agent at %s: %v", address, err)
	}
	defer sdk.Close()
	endpoints, err := sdk.Resolve(context.Background(), flag.Arg(0))
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
