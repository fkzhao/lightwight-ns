package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"

	"github.com/fkzhao/lightwight-ns/client"
)

func main() {
	tcp := flag.Bool("tcp", false, "connect to the agent over TCP RPC")
	flag.Usage = func() { fmt.Fprintln(os.Stderr, "usage: go run ./discover [-tcp] <service-name>") }
	flag.Parse()
	if flag.NArg() != 1 || flag.Arg(0) == "" {
		flag.Usage()
		os.Exit(2)
	}
	var discovery *client.Client
	var err error
	address := ""
	if *tcp {
		address = os.Getenv("NS_AGENT_TCP_ADDR")
		if address == "" {
			address = client.DefaultTCPAddress
		}
		discovery, err = client.DialTCP(address)
	} else {
		address = os.Getenv("NS_AGENT_SOCKET")
		if address == "" {
			address = client.DefaultSocketPath
		}
		discovery, err = client.Dial(address)
	}
	if err != nil {
		if !*tcp && errors.Is(err, os.ErrNotExist) {
			log.Fatalf("local agent socket %s does not exist; start the agent with: go run ./local-agent", address)
		}
		log.Fatalf("connect to local agent at %s: %v", address, err)
	}
	defer discovery.Close()
	endpoints, err := discovery.Discover(flag.Arg(0))
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
