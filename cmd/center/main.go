package main

import (
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
	for _, kind := range []string{"services", "machines", "instances"} {
		kind := kind
		mux.HandleFunc("/api/"+kind, func(w http.ResponseWriter, r *http.Request) {
			handleResource(store, kind, "", w, r)
		})
		mux.HandleFunc("/api/"+kind+"/", func(w http.ResponseWriter, r *http.Request) {
			id := strings.TrimPrefix(r.URL.Path, "/api/"+kind+"/")
			if id == "" || strings.Contains(id, "/") {
				writeError(w, http.StatusNotFound, "not_found")
				return
			}
			handleResource(store, kind, id, w, r)
		})
	}
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

func handleResource(store *registry.Store, kind, id string, w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodPut && r.Method != http.MethodDelete {
		w.Header().Set("Allow", "POST, PUT, DELETE")
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed")
		return
	}
	if r.Method == http.MethodPost && id != "" || r.Method != http.MethodPost && id == "" {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	if r.Method == http.MethodDelete {
		var err error
		switch kind {
		case "services":
			err = store.DeleteService(id)
		case "machines":
			err = store.DeleteMachine(id)
		case "instances":
			err = store.DeleteInstance(id)
		}
		if err != nil {
			respondError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	status := http.StatusOK
	if r.Method == http.MethodPost {
		status = http.StatusCreated
	}
	switch kind {
	case "services":
		var input registry.Service
		if !decode(w, r, &input) {
			return
		}
		item, err := store.SaveService(id, input)
		if err != nil {
			respondError(w, err)
			return
		}
		writeJSON(w, status, item)
	case "machines":
		var input registry.Machine
		if !decode(w, r, &input) {
			return
		}
		item, err := store.SaveMachine(id, input)
		if err != nil {
			respondError(w, err)
			return
		}
		writeJSON(w, status, item)
	case "instances":
		var input registry.Instance
		if !decode(w, r, &input) {
			return
		}
		item, err := store.SaveInstance(id, input)
		if err != nil {
			respondError(w, err)
			return
		}
		writeJSON(w, status, item)
	default:
		writeError(w, http.StatusNotFound, "not_found")
	}
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
