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
	"context"
	"fmt"
)

// adapter wraps an MCP Client and a specific tool name behind the tools.Executor interface.
// This allows MCP tools to be called identically to built-in tools from the agent runtime.
type adapter struct {
	client   Client
	toolName string
}

// Execute calls the named MCP tool via the MCP client.
func (a *adapter) Execute(ctx context.Context, input map[string]interface{}) (map[string]interface{}, error) {
	result, err := a.client.CallTool(ctx, a.toolName, input)
	if err != nil {
		return nil, fmt.Errorf("mcp tool %q: %w", a.toolName, err)
	}
	return result, nil
}

// NewToolAdapter creates a tools.Executor that delegates to the given MCP client + tool name.
// The returned value satisfies the tools.Executor interface without importing it
// (to avoid circular imports — callers cast via interface{}).
func NewToolAdapter(client Client, toolName string) interface {
	Execute(ctx context.Context, input map[string]interface{}) (map[string]interface{}, error)
} {
	return &adapter{client: client, toolName: toolName}
}
