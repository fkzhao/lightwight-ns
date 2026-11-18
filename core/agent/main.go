package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/fkzhao/lightwight-ns/core/internal/agent"
)

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	center := env("NS_CENTER_RPC_ADDR", "127.0.0.1:9090")
	socket := env("NS_AGENT_SOCKET", "/tmp/lightwight-ns/agent.sock")
	tcpAddress := env("NS_AGENT_TCP_ADDR", "127.0.0.1:9091")
	interval, err := time.ParseDuration(env("NS_SYNC_INTERVAL", "5s"))
	if err != nil || interval <= 0 {
		return errors.New("NS_SYNC_INTERVAL must be a positive duration")
	}
	if err := os.MkdirAll(filepath.Dir(socket), 0750); err != nil {
		return err
	}
	if info, err := os.Lstat(socket); err == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return fmt.Errorf("%s exists and is not a socket", socket)
		}
		existing, dialErr := net.DialTimeout("unix", socket, 300*time.Millisecond)
		if dialErr == nil {
			existing.Close()
			return fmt.Errorf("agent already listening on %s", socket)
		}
		if err := os.Remove(socket); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	unixListener, err := net.Listen("unix", socket)
	if err != nil {
		return err
	}
	defer unixListener.Close()
	defer os.Remove(socket)
	if err := os.Chmod(socket, 0660); err != nil {
		return err
	}
	tcpListener, err := net.Listen("tcp", tcpAddress)
	if err != nil {
		return fmt.Errorf("listen on agent TCP address %s: %w", tcpAddress, err)
	}
	defer tcpListener.Close()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	maxStale, err := time.ParseDuration(env("NS_AGENT_MAX_STALE", "30s"))
	if err != nil || maxStale < time.Second {
		return errors.New("NS_AGENT_MAX_STALE must be at least 1s")
	}
	state := &agent.State{MaxStale: maxStale}
	go func() {
		for {
			if err := agent.WatchOnce(ctx, state, center); err != nil {
				log.Printf("registry sync: %v", err)
			} else {
				continue
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(interval):
			}
		}
	}()
	go func() { <-ctx.Done(); unixListener.Close(); tcpListener.Close() }()
	log.Printf("local agent listening on unix %s and tcp %s", socket, tcpListener.Addr())
	go serveRPC(ctx, state, unixListener)
	serveRPC(ctx, state, tcpListener)
	return nil
}

func serveRPC(ctx context.Context, state *agent.State, listener net.Listener) {
	for {
		connection, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Printf("accept %s connection: %v", listener.Addr(), err)
			continue
		}
		go agent.ServeConn(connection, state)
	}
}
