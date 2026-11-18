package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/rpc"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/fkzhao/lightwight-ns/core/internal/health"
	"github.com/fkzhao/lightwight-ns/core/internal/registry"
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
	store, err := registry.NewStore(databasePath())
	if err != nil {
		return err
	}
	defer store.Close()
	interval, err := time.ParseDuration(env("NS_HEALTH_INTERVAL", "5s"))
	if err != nil || interval <= 0 {
		return errors.New("NS_HEALTH_INTERVAL must be a positive duration")
	}
	timeout, err := time.ParseDuration(env("NS_HEALTH_TIMEOUT", "2s"))
	if err != nil || timeout <= 0 {
		return errors.New("NS_HEALTH_TIMEOUT must be a positive duration")
	}
	concurrency, err := strconv.Atoi(env("NS_HEALTH_CONCURRENCY", "128"))
	if err != nil || concurrency < 1 || concurrency > 4096 {
		return errors.New("NS_HEALTH_CONCURRENCY must be between 1 and 4096")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	checkerDone := make(chan struct{})
	go func() {
		defer close(checkerDone)
		(&health.Checker{Store: store, Interval: interval, Timeout: timeout, Concurrency: concurrency}).Run(ctx)
	}()
	defer func() { stop(); <-checkerDone }()
	rpcServer := rpc.NewServer()
	if err := rpcServer.RegisterName("Registry", &registry.RPCServer{Store: store}); err != nil {
		return err
	}
	rpcAddr := env("NS_RPC_ADDR", "127.0.0.1:9090")
	rpcListener, err := net.Listen("tcp", rpcAddr)
	if err != nil {
		return err
	}
	defer rpcListener.Close()
	go rpcServer.Accept(rpcListener)
	log.Printf("RPC listening on %s", rpcAddr)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /api/snapshot", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, store.Snapshot())
	})
	mux.HandleFunc("/api/services", func(w http.ResponseWriter, r *http.Request) { handleResource(store, w, r) })
	mux.HandleFunc("/api/services/", func(w http.ResponseWriter, r *http.Request) { handleResource(store, w, r) })
	mux.HandleFunc("/api/registrations", func(w http.ResponseWriter, r *http.Request) { handleRegistration(store, w, r) })
	mux.HandleFunc("/api/registrations/", func(w http.ResponseWriter, r *http.Request) { handleRegistration(store, w, r) })
	serveFrontend(mux, env("NS_WEB_DIST", "web/dist"))
	addr := env("NS_HTTP_ADDR", "127.0.0.1:8080")
	log.Printf("HTTP listening on %s", addr)
	server := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Printf("HTTP shutdown: %v", err)
		}
	}()
	err = server.ListenAndServe()
	stop()
	<-shutdownDone
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func databasePath() string {
	if path := os.Getenv("NS_DB_FILE"); path != "" {
		return path
	}
	if path := os.Getenv("NS_DATA_FILE"); path != "" {
		if strings.HasSuffix(path, ".json") {
			return strings.TrimSuffix(path, ".json") + ".db"
		}
		return path
	}
	return "data/registry.db"
}

func serveFrontend(mux *http.ServeMux, dir string) {
	index := filepath.Join(dir, "index.html")
	if _, err := os.Stat(index); err != nil {
		return
	}
	files := http.FileServer(http.Dir(dir))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			writeError(w, http.StatusNotFound, "not_found")
			return
		}
		path := filepath.Join(dir, filepath.Clean("/"+r.URL.Path))
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			files.ServeHTTP(w, r)
			return
		}
		http.ServeFile(w, r, index)
	})
}

func handleResource(store *registry.Store, w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/services")
	if path == "" {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed")
			return
		}
		var input registry.Service
		if !decode(w, r, &input) {
			return
		}
		item, err := store.SaveService("", input)
		if err != nil {
			respondError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, item)
		return
	}
	if !strings.HasPrefix(path, "/") {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	for _, part := range parts {
		if part == "" {
			writeError(w, http.StatusNotFound, "not_found")
			return
		}
	}
	serviceID := parts[0]
	if len(parts) == 1 {
		switch r.Method {
		case http.MethodPut:
			var input registry.Service
			if !decode(w, r, &input) {
				return
			}
			item, err := store.SaveService(serviceID, input)
			if err != nil {
				respondError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, item)
		case http.MethodDelete:
			if err := store.DeleteService(serviceID); err != nil {
				respondError(w, err)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed")
		}
		return
	}
	if len(parts) == 4 && parts[1] == "instances" && r.Method == http.MethodPost {
		var enabled *bool
		draining := parts[3] == "drain"
		if parts[3] == "enabled" {
			var input struct {
				Enabled bool `json:"enabled"`
			}
			if !decode(w, r, &input) {
				return
			}
			enabled = &input.Enabled
		} else if !draining {
			writeError(w, http.StatusNotFound, "not_found")
			return
		}
		if err := store.SetInstanceState(serviceID, parts[2], enabled, draining); err != nil {
			respondError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if parts[1] != "instances" || len(parts) > 3 {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	if len(parts) == 2 && r.Method == http.MethodPost || len(parts) == 3 && r.Method == http.MethodPut {
		var input registry.Instance
		if !decode(w, r, &input) {
			return
		}
		instanceID := ""
		status := http.StatusCreated
		if len(parts) == 3 {
			instanceID = parts[2]
			status = http.StatusOK
		}
		item, err := store.SaveInstance(serviceID, instanceID, input)
		if err != nil {
			respondError(w, err)
			return
		}
		writeJSON(w, status, item)
		return
	}
	if len(parts) == 3 && r.Method == http.MethodDelete {
		if err := store.DeleteInstance(serviceID, parts[2]); err != nil {
			respondError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeError(w, http.StatusMethodNotAllowed, "method_not_allowed")
}

func handleRegistration(store *registry.Store, w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/registrations")
	if path == "" {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed")
			return
		}
		var input registry.RegistrationInput
		if !decode(w, r, &input) {
			return
		}
		result, err := store.RegisterInstance(input)
		if err != nil {
			respondError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
		return
	}
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	if !strings.HasPrefix(path, "/") || len(parts) < 1 || parts[0] == "" || len(parts) > 2 {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	var input registry.RegistrationToken
	if len(parts) == 2 && parts[1] == "drain" && r.Method == http.MethodPost {
		if !decode(w, r, &input) {
			return
		}
		if err := store.DrainRegistration(parts[0], input.Token); err != nil {
			respondError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if len(parts) == 2 && parts[1] == "renew" && r.Method == http.MethodPost {
		if !decode(w, r, &input) {
			return
		}
		result, err := store.RenewRegistration(parts[0], input.Token)
		if err != nil {
			respondError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
		return
	}
	if len(parts) == 1 && r.Method == http.MethodDelete {
		if !decode(w, r, &input) {
			return
		}
		if err := store.DeregisterInstance(parts[0], input.Token); err != nil {
			respondError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeError(w, http.StatusMethodNotAllowed, "method_not_allowed")
}

func decode(w http.ResponseWriter, r *http.Request, value any) bool {
	defer r.Body.Close()
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json")
		return false
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		writeError(w, http.StatusBadRequest, "invalid_json")
		return false
	}
	return true
}

func respondError(w http.ResponseWriter, err error) {
	var domain registry.DomainError
	if errors.As(err, &domain) {
		status := http.StatusBadRequest
		if domain.Code == "not_found" {
			status = http.StatusNotFound
		}
		if domain.Code == "registration_forbidden" {
			status = http.StatusForbidden
		}
		if domain.Code == "registration_expired" || domain.Code == "registration_retired" {
			status = http.StatusGone
		}
		if strings.HasPrefix(domain.Code, "duplicate_") || strings.HasSuffix(domain.Code, "_has_instances") || domain.Code == "port_in_use" || domain.Code == "registration_conflict" {
			status = http.StatusConflict
		}
		writeError(w, status, domain.Code)
		return
	}
	log.Print(err)
	writeError(w, http.StatusInternalServerError, "internal_error")
}

func writeError(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, map[string]string{"code": code})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Print(fmt.Errorf("write response: %w", err))
	}
}
