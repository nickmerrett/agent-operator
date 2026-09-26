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

// Package tools provides the ToolExecutor interface, the built-in tool catalogue,
// and the MCP adapter that wraps external MCP tools behind the same interface.
package tools

import (
	"context"

	"github.com/nickmerrett/agent-operator/internal/llm"
)

// Executor is the interface that every tool must implement.
type Executor interface {
	// Execute runs the tool with the given input and returns its output.
	// Both input and output are free-form maps matching the tool's JSON Schema.
	Execute(ctx context.Context, input map[string]interface{}) (map[string]interface{}, error)
}

// Spec describes a tool — its name, description, and JSON Schema for input parameters.
// This is what gets sent to the LLM as a ToolDefinition.
type Spec struct {
	Name        string
	Description string
	// InputSchema is a JSON Schema object (map[string]interface{}) describing inputs.
	InputSchema map[string]interface{}
}

// ToLLMToolDefinition converts a Spec to the llm.ToolDefinition format expected by LLM clients.
func (s Spec) ToLLMToolDefinition() llm.ToolDefinition {
	return llm.ToolDefinition{
		Type: "function",
		Function: llm.FunctionDef{
			Name:        s.Name,
			Description: s.Description,
			Parameters:  s.InputSchema,
		},
	}
}

// Entry binds a Spec to its Executor.
type Entry struct {
	Spec     Spec
	Executor Executor
}
