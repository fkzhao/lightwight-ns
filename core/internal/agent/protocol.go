package agent

import (
	"bufio"
	"encoding/json"
	"errors"
	"net"

	"github.com/fkzhao/lightwight-ns/core/internal/registry"
)

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// ServeConn handles line-delimited JSON-RPC 2.0 requests on one connection.
func ServeConn(connection net.Conn, state *State) {
	defer connection.Close()
	scanner := bufio.NewScanner(connection)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	writer := bufio.NewWriter(connection)
	for scanner.Scan() {
		reply := handleRequest(scanner.Bytes(), state)
		encoded, err := json.Marshal(reply)
		if err != nil {
			return
		}
		if _, err := writer.Write(append(encoded, '\n')); err != nil {
			return
		}
		if err := writer.Flush(); err != nil {
			return
		}
	}
}

func handleRequest(line []byte, state *State) response {
	reply := response{JSONRPC: "2.0", ID: json.RawMessage("null")}
	var input request
	if err := json.Unmarshal(line, &input); err != nil {
		reply.Error = &rpcError{Code: -32700, Message: "parse error"}
		return reply
	}
	if len(input.ID) > 0 {
		reply.ID = input.ID
	}
	if input.JSONRPC != "2.0" || len(input.ID) == 0 || input.Method == "" {
		reply.Error = &rpcError{Code: -32600, Message: "invalid request"}
		return reply
	}
	switch input.Method {
	case "agent.status":
		reply.Result = state.Status()
	case "discovery.resolveV2":
		var params registry.ResolveQuery
		if err := json.Unmarshal(input.Params, &params); err != nil || params.Service == "" {
			reply.Error = &rpcError{Code: -32602, Message: "invalid params"}
			return reply
		}
		result, err := state.ResolveV2(params)
		if err != nil {
			reply.Error = resolveError(err)
		} else {
			reply.Result = result
		}
	case "discovery.resolve":
		var params struct {
			Service string `json:"service"`
		}
		if err := json.Unmarshal(input.Params, &params); err != nil || params.Service == "" {
			reply.Error = &rpcError{Code: -32602, Message: "invalid params"}
			return reply
		}
		resolved, err := state.Resolve(params.Service)
		if err != nil {
			reply.Error = resolveError(err)
			return reply
		}
		reply.Result = struct {
			RegistryVersion uint64              `json:"registryVersion"`
			Endpoints       []registry.Endpoint `json:"endpoints"`
		}{RegistryVersion: resolved.Version, Endpoints: resolved.Endpoints}
	default:
		reply.Error = &rpcError{Code: -32601, Message: "method not found"}
	}
	return reply
}

func resolveError(err error) *rpcError {
	var domain registry.DomainError
	if errors.As(err, &domain) {
		if domain.Code == "not_found" {
			return &rpcError{Code: -32004, Message: "service not found"}
		}
		if domain.Code == "stale_snapshot" {
			return &rpcError{Code: -32002, Message: "agent snapshot expired"}
		}
	}
	return &rpcError{Code: -32001, Message: "agent not ready"}
}
