package main

import (
	"context"
	"errors"
	"log"
	"net"
	"net/rpc"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/fkzhao/lightwight-ns/internal/agent"
)

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func main() {
	center := env("NS_CENTER_RPC_ADDR", "127.0.0.1:9090")
	socket := env("NS_AGENT_SOCKET", "/tmp/lightwight-ns/agent.sock")
	interval, err := time.ParseDuration(env("NS_SYNC_INTERVAL", "5s"))
	if err != nil || interval <= 0 {
		log.Fatal("NS_SYNC_INTERVAL must be a positive duration")
	}
	if err := os.MkdirAll(filepath.Dir(socket), 0750); err != nil {
		log.Fatal(err)
	}
	if info, err := os.Lstat(socket); err == nil {
		if info.Mode()&os.ModeSocket == 0 {
			log.Fatalf("%s exists and is not a socket", socket)
		}
		existing, dialErr := net.DialTimeout("unix", socket, 300*time.Millisecond)
		if dialErr == nil {
			existing.Close()
			log.Fatalf("agent already listening on %s", socket)
		}
		if err := os.Remove(socket); err != nil {
			log.Fatal(err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		log.Fatal(err)
	}
	listener, err := net.Listen("unix", socket)
	if err != nil {
		log.Fatal(err)
	}
	defer listener.Close()
	defer os.Remove(socket)
	if err := os.Chmod(socket, 0660); err != nil {
		log.Fatal(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	state := &agent.State{}
	rpcServer := rpc.NewServer()
	if err := rpcServer.RegisterName("Agent", &agent.LocalRPC{State: state}); err != nil {
		log.Fatal(err)
	}
	go func() {
		for {
			if err := agent.SyncOnce(ctx, state, center); err != nil {
				log.Printf("registry sync: %v", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(interval):
			}
		}
	}()
	go func() { <-ctx.Done(); listener.Close() }()
	log.Printf("local agent listening on %s", socket)
	for {
		connection, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Printf("accept local connection: %v", err)
			continue
		}
		go rpcServer.ServeConn(connection)
	}
}
