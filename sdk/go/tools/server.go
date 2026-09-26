/*
Copyright 2024 Nick Merrett.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package tools provides the agent-operator Tool Server SDK.
//
// It allows you to write custom tools as a Go service and expose them
// as an MCP-compatible HTTP server that the agent-operator can discover
// and call via an MCPServer CR.
//
// Quick start:
//
//	srv := tools.NewServer(":8080")
//
//	srv.Register(tools.Tool{
//	    Name:        "search-confluence",
//	    Description: "Search Confluence pages by keyword",
//	    InputSchema: tools.ObjectSchema(tools.Properties{
//	        "query": tools.StringProp("The search query"),
//	    }, "query"),
//	    Handler: func(ctx context.Context, input map[string]interface{}) (map[string]interface{}, error) {
//	        query, _ := input["query"].(string)
//	        // ... call Confluence API ...
//	        return map[string]interface{}{"results": results}, nil
//	    },
//	})
//
//	log.Fatal(srv.Start())
//
// Then point an MCPServer CR at your service:
//
//	apiVersion: agent.runtime.io/v1alpha1
//	kind: MCPServer
//	spec:
//	  transport: http
//	  endpoint: http://my-tool-server.my-namespace.svc.cluster.local:8080
//	  autoDiscoverTools: true
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// Handler is the function signature for a tool implementation.
type Handler func(ctx context.Context, input map[string]interface{}) (map[string]interface{}, error)

// Tool defines a single tool exposed by the server.
type Tool struct {
	// Name is the tool's unique identifier. Must match the name the LLM will use to call it.
	Name string
	// Description is shown to the LLM to help it decide when to use this tool.
	Description string
	// InputSchema is a JSON Schema object describing the tool's input parameters.
	// Use the helper functions ObjectSchema, StringProp, etc. to build it.
	InputSchema map[string]interface{}
	// Handler is the function called when the tool is invoked.
	Handler Handler
}

// Server is an MCP-compatible HTTP tool server.
type Server struct {
	addr  string
	mu    sync.RWMutex
	tools map[string]Tool
}

// NewServer creates a new tool server that listens on addr (e.g. ":8080").
func NewServer(addr string) *Server {
	return &Server{
		addr:  addr,
		tools: make(map[string]Tool),
	}
}

// Register adds a tool to the server.
// Panics if a tool with the same name is already registered.
func (s *Server) Register(t Tool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.tools[t.Name]; exists {
		panic(fmt.Sprintf("agent-operator/tools: tool %q already registered", t.Name))
	}
	s.tools[t.Name] = t
}

// Start begins serving MCP requests. This is a blocking call.
func (s *Server) Start() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleRPC)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	srv := &http.Server{
		Addr:         s.addr,
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 60 * time.Second,
	}
	return srv.ListenAndServe()
}

// handleRPC handles all JSON-RPC 2.0 requests.
func (s *Server) handleRPC(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeRPCError(w, 0, -32700, "parse error: "+err.Error())
		return
	}

	var req rpcRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeRPCError(w, 0, -32700, "parse error: "+err.Error())
		return
	}

	switch req.Method {
	case "tools/list":
		s.handleListTools(w, req)
	case "tools/call":
		s.handleCallTool(w, r, req)
	default:
		writeRPCError(w, req.ID, -32601, fmt.Sprintf("method not found: %s", req.Method))
	}
}

func (s *Server) handleListTools(w http.ResponseWriter, req rpcRequest) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	toolDefs := make([]toolDefinition, 0, len(s.tools))
	for _, t := range s.tools {
		toolDefs = append(toolDefs, toolDefinition{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: t.InputSchema,
		})
	}

	writeRPCResult(w, req.ID, map[string]interface{}{
		"tools": toolDefs,
	})
}

func (s *Server) handleCallTool(w http.ResponseWriter, r *http.Request, req rpcRequest) {
	var params callToolParams
	if req.Params != nil {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			writeRPCError(w, req.ID, -32602, "invalid params: "+err.Error())
			return
		}
	}

	s.mu.RLock()
	tool, ok := s.tools[params.Name]
	s.mu.RUnlock()

	if !ok {
		writeRPCError(w, req.ID, -32602, fmt.Sprintf("tool %q not found", params.Name))
		return
	}

	output, err := tool.Handler(r.Context(), params.Arguments)
	if err != nil {
		// Tool errors are returned as MCP content with isError=true,
		// not as JSON-RPC errors — the LLM can observe and recover.
		writeRPCResult(w, req.ID, map[string]interface{}{
			"content": []map[string]interface{}{
				{"type": "text", "text": fmt.Sprintf("ERROR: %v", err)},
			},
			"isError": true,
		})
		return
	}

	// Serialise the output map as a JSON text content block.
	outputJSON, _ := json.Marshal(output)
	writeRPCResult(w, req.ID, map[string]interface{}{
		"content": []map[string]interface{}{
			{"type": "text", "text": string(outputJSON)},
		},
	})
}

// ── JSON-RPC helpers ──────────────────────────────────────────────────────────

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type callToolParams struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments"`
}

type toolDefinition struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"inputSchema,omitempty"`
}

func writeRPCResult(w http.ResponseWriter, id int, result interface{}) {
	data, _ := json.Marshal(map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      id,
		"result":  result,
	})
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func writeRPCError(w http.ResponseWriter, id, code int, message string) {
	data, _ := json.Marshal(map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      id,
		"error":   map[string]interface{}{"code": code, "message": message},
	})
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK) // JSON-RPC errors still return HTTP 200
	_, _ = w.Write(data)
}
