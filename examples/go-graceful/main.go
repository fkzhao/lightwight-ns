package main

import (
	"context"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/fkzhao/lightwight-ns/sdk/go/registration"
)

// The service definition must use HTTP /health or TCP health checks.
func main() {
	address := os.Getenv("NS_SERVICE_ADDR")
	if address == "" {
		address = "127.0.0.1:8081"
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		log.Fatal(err)
	}
	defer listener.Close()
	host, portString, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		log.Fatal(err)
	}
	port, err := strconv.Atoi(portString)
	if err != nil {
		log.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("hello\n")) })
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			log.Print(err)
		}
	}()
	lease, err := registration.NewClient(os.Getenv("NS_CENTER_HTTP_URL")).Register(context.Background(), "order-service", host, port)
	if err != nil {
		server.Close()
		log.Fatal(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := lease.Drain(shutdown); err != nil {
		log.Printf("drain: %v", err)
	}
	if err := server.Shutdown(shutdown); err != nil {
		server.Close()
		log.Print(err)
	}
	cleanup, cancelCleanup := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancelCleanup()
	if err := lease.Close(cleanup); err != nil {
		log.Printf("deregister: %v", err)
	}
}
