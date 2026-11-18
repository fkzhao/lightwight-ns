package health

import (
	"context"
	"crypto/tls"
	"encoding/pem"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/fkzhao/lightwight-ns/core/internal/registry"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

func TestStandardGRPCHealthAndTLS(t *testing.T) {
	for _, secure := range []bool{false, true} {
		t.Run(map[bool]string{false: "plaintext", true: "tls"}[secure], func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			check := registry.HealthCheck{Type: "grpc", GRPCService: "orders", TimeoutMillis: 500}
			var options []grpc.ServerOption
			if secure {
				certificateServer := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
				certificate := certificateServer.TLS.Certificates[0]
				certificateServer.Close()
				options = append(options, grpc.Creds(credentials.NewTLS(&tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12})))
				check.TLS = true
				check.CAPEM = string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate.Certificate[0]}))
			}
			server := grpc.NewServer(options...)
			healthServer := health.NewServer()
			healthServer.SetServingStatus("orders", healthpb.HealthCheckResponse_SERVING)
			healthpb.RegisterHealthServer(server, healthServer)
			go server.Serve(listener)
			defer server.Stop()
			instance := registry.Instance{Address: "127.0.0.1", Port: listener.Addr().(*net.TCPAddr).Port}
			checker := Checker{}
			if err := checker.probeError(context.Background(), check, instance); err != nil {
				t.Fatal(err)
			}
			healthServer.SetServingStatus("orders", healthpb.HealthCheckResponse_NOT_SERVING)
			if err := checker.probeError(context.Background(), check, instance); err == nil {
				t.Fatal("NOT_SERVING accepted")
			}
			check.GRPCService = "missing"
			if err := checker.probeError(context.Background(), check, instance); err == nil {
				t.Fatal("missing gRPC health service accepted")
			}
		})
	}
}

func TestGRPCProbeTimeout(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		connection, err := listener.Accept()
		if err == nil {
			defer connection.Close()
			buffer := make([]byte, 4096)
			for {
				if _, err := connection.Read(buffer); err != nil {
					return
				}
			}
		}
	}()
	started := time.Now()
	err = (&Checker{}).probeError(context.Background(), registry.HealthCheck{Type: "grpc", TimeoutMillis: 50}, registry.Instance{Address: "127.0.0.1", Port: listener.Addr().(*net.TCPAddr).Port})
	if err == nil || time.Since(started) > time.Second {
		t.Fatalf("gRPC timeout was not enforced: %v", err)
	}
}
