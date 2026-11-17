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
	"path/filepath"
	"strings"
	"time"

	"github.com/fkzhao/lightwight-ns/internal/health"
	"github.com/fkzhao/lightwight-ns/internal/registry"
)

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func main() {
	store, err := registry.NewStore(env("NS_DATA_FILE", "data/registry.json"))
	if err != nil {
		log.Fatal(err)
	}
	interval, err := time.ParseDuration(env("NS_HEALTH_INTERVAL", "5s"))
	if err != nil || interval <= 0 {
		log.Fatal("NS_HEALTH_INTERVAL must be a positive duration")
	}
	go (&health.Checker{Store: store, Interval: interval}).Run(context.Background())
	rpcServer := rpc.NewServer()
	if err := rpcServer.RegisterName("Registry", &registry.RPCServer{Store: store}); err != nil {
		log.Fatal(err)
	}
	rpcAddr := env("NS_RPC_ADDR", "127.0.0.1:9090")
	rpcListener, err := net.Listen("tcp", rpcAddr)
	if err != nil {
		log.Fatal(err)
	}
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
	serveFrontend(mux, env("NS_WEB_DIST", "web/dist"))
	addr := env("NS_HTTP_ADDR", "127.0.0.1:8080")
	log.Printf("HTTP listening on %s", addr)
	server := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	log.Fatal(server.ListenAndServe())
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
		if strings.HasPrefix(domain.Code, "duplicate_") || strings.HasSuffix(domain.Code, "_has_instances") || domain.Code == "port_in_use" {
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
