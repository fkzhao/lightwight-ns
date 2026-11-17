package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"

	"github.com/fkzhao/lightwight-ns/sdk/go/discovery"
	"github.com/fkzhao/lightwight-ns/sdk/go/registration"
)

func main() {
	if len(os.Args) < 3 {
		log.Fatal("usage: go run ./examples/go discover SERVICE | register SERVICE IP PORT")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	switch os.Args[1] {
	case "discover":
		agent, err := discovery.DialTCP(os.Getenv("NS_AGENT_TCP_ADDR"))
		if err != nil {
			log.Fatal(err)
		}
		defer agent.Close()
		endpoints, err := agent.Resolve(ctx, os.Args[2])
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println(endpoints)
	case "register":
		if len(os.Args) != 5 {
			log.Fatal("usage: go run ./examples/go register SERVICE IP PORT")
		}
		port, err := strconv.Atoi(os.Args[4])
		if err != nil {
			log.Fatal(err)
		}
		lease, err := registration.NewClient(os.Getenv("NS_CENTER_HTTP_URL")).Register(ctx, os.Args[2], os.Args[3], port)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println("registered", lease.InstanceID())
		<-ctx.Done()
		if err := lease.Close(context.Background()); err != nil {
			log.Print(err)
		}
	default:
		log.Fatal("expected discover or register")
	}
}
