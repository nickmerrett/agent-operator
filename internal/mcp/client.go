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

package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
)

// Client communicates with an MCP server.
type Client interface {
	// ListTools returns the tool definitions available on the MCP server.
	ListTools(ctx context.Context) ([]ToolDefinition, error)
	// CallTool invokes a named tool with the given arguments.
	CallTool(ctx context.Context, name string, args map[string]interface{}) (map[string]interface{}, error)
}

// NewClient creates an MCP client for the given transport and endpoint.
// Supported transports: "http", "sse".
func NewClient(transport, endpoint, token string) (Client, error) {
	switch transport {
	case "http":
		return &httpClient{
			endpoint: endpoint,
			token:    token,
			http:     &http.Client{Timeout: 60 * time.Second},
		}, nil
	case "sse":
		return &sseClient{
			endpoint: endpoint,
			token:    token,
			http:     &http.Client{Timeout: 120 * time.Second},
		}, nil
	default:
		return nil, fmt.Errorf("mcp: unsupported transport %q", transport)
	}
}

// ── HTTP transport ────────────────────────────────────────────────────────────

type httpClient struct {
	endpoint string
	token    string
	http     *http.Client
	idSeq    atomic.Int64
}

func (c *httpClient) call(ctx context.Context, method string, params interface{}) (json.RawMessage, error) {
	id := int(c.idSeq.Add(1))
	req := rpcRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}
	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("mcp/http: marshal: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("mcp/http: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("mcp/http: do: %w", err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("mcp/http: status %d: %s", resp.StatusCode, raw)
	}

	var rpcResp rpcResponse
	if err := json.Unmarshal(raw, &rpcResp); err != nil {
		return nil, fmt.Errorf("mcp/http: unmarshal: %w", err)
	}
	if rpcResp.Error != nil {
		return nil, fmt.Errorf("mcp/http: rpc error %d: %s", rpcResp.Error.Code, rpcResp.Error.Message)
	}
	return rpcResp.Result, nil
}

func (c *httpClient) ListTools(ctx context.Context) ([]ToolDefinition, error) {
	result, err := c.call(ctx, "tools/list", ListToolsRequest{})
	if err != nil {
		return nil, err
	}
	var resp ListToolsResponse
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, fmt.Errorf("mcp/http: unmarshal ListTools: %w", err)
	}
	return resp.Tools, nil
}

func (c *httpClient) CallTool(ctx context.Context, name string, args map[string]interface{}) (map[string]interface{}, error) {
	result, err := c.call(ctx, "tools/call", CallToolRequest{Name: name, Arguments: args})
	if err != nil {
		return nil, err
	}
	var resp CallToolResponse
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, fmt.Errorf("mcp/http: unmarshal CallTool: %w", err)
	}
	if resp.IsError {
		errMsg := ""
		for _, c := range resp.Content {
			errMsg += c.Text
		}
		return nil, fmt.Errorf("mcp tool error: %s", errMsg)
	}
	// Flatten text content blocks into a single output map.
	var sb strings.Builder
	for _, block := range resp.Content {
		sb.WriteString(block.Text)
	}
	return map[string]interface{}{"output": sb.String()}, nil
}

// ── SSE transport ─────────────────────────────────────────────────────────────

// sseClient uses the SSE transport: sends a JSON-RPC request and reads the
// response as a Server-Sent Event stream, taking the first event as the result.
type sseClient struct {
	endpoint string
	token    string
	http     *http.Client
	idSeq    atomic.Int64
}

func (c *sseClient) call(ctx context.Context, method string, params interface{}) (json.RawMessage, error) {
	id := int(c.idSeq.Add(1))
	req := rpcRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}
	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("mcp/sse: marshal: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("mcp/sse: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	if c.token != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("mcp/sse: do: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("mcp/sse: status %d: %s", resp.StatusCode, raw)
	}

	// Read SSE events until we get a "message" event with a data field.
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			payload := strings.TrimPrefix(line, "data: ")
			var rpcResp rpcResponse
			if err := json.Unmarshal([]byte(payload), &rpcResp); err != nil {
				continue
			}
			if rpcResp.Error != nil {
				return nil, fmt.Errorf("mcp/sse: rpc error %d: %s", rpcResp.Error.Code, rpcResp.Error.Message)
			}
			return rpcResp.Result, nil
		}
	}
	return nil, fmt.Errorf("mcp/sse: no result received from stream")
}

func (c *sseClient) ListTools(ctx context.Context) ([]ToolDefinition, error) {
	result, err := c.call(ctx, "tools/list", ListToolsRequest{})
	if err != nil {
		return nil, err
	}
	var resp ListToolsResponse
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, fmt.Errorf("mcp/sse: unmarshal ListTools: %w", err)
	}
	return resp.Tools, nil
}

func (c *sseClient) CallTool(ctx context.Context, name string, args map[string]interface{}) (map[string]interface{}, error) {
	result, err := c.call(ctx, "tools/call", CallToolRequest{Name: name, Arguments: args})
	if err != nil {
		return nil, err
	}
	var resp CallToolResponse
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, fmt.Errorf("mcp/sse: unmarshal CallTool: %w", err)
	}
	if resp.IsError {
		errMsg := ""
		for _, block := range resp.Content {
			errMsg += block.Text
		}
		return nil, fmt.Errorf("mcp tool error: %s", errMsg)
	}
	var sb strings.Builder
	for _, block := range resp.Content {
		sb.WriteString(block.Text)
	}
	return map[string]interface{}{"output": sb.String()}, nil
}
