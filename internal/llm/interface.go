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

// Package llm provides a common interface for LLM backends and concrete adapters
// for watsonx, OpenAI-compatible, Ollama, and Anthropic APIs.
package llm

import "context"

// Role identifies who authored a message in a conversation.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// Message is a single turn in a conversation.
type Message struct {
	Role       Role   `json:"role"`
	Content    string `json:"content"`
	// ToolCallID is populated for RoleTool messages — links the observation back
	// to the tool call that generated it.
	ToolCallID string `json:"tool_call_id,omitempty"`
	// Name is the tool name for RoleTool messages.
	Name       string `json:"name,omitempty"`
}

// ToolCall represents a tool invocation requested by the LLM.
type ToolCall struct {
	// ID is the unique identifier for this tool call (from the LLM).
	ID       string `json:"id"`
	// Name is the tool to invoke.
	Name     string `json:"name"`
	// Arguments is the raw JSON arguments string.
	Arguments string `json:"arguments"`
}

// ToolDefinition describes a tool available to the LLM (OpenAI function-calling format).
type ToolDefinition struct {
	Type     string         `json:"type"` // always "function"
	Function FunctionDef    `json:"function"`
}

// FunctionDef is the function definition within a ToolDefinition.
type FunctionDef struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Parameters  interface{} `json:"parameters"` // JSON Schema object
}

// CompletionRequest is the input to a Complete call.
type CompletionRequest struct {
	Messages []Message
	Tools    []ToolDefinition
	// MaxTokens overrides the provider default when non-zero.
	MaxTokens   int
	Temperature float64
}

// CompletionResponse is the output of a Complete call.
type CompletionResponse struct {
	// Content is the text response when the LLM did not request a tool call.
	Content   string
	// ToolCalls is populated when the LLM wants to invoke one or more tools.
	ToolCalls []ToolCall
	// TokensUsed is the total token count for this completion.
	TokensUsed int
}

// Client is the common interface implemented by all LLM adapters.
type Client interface {
	// Complete sends a conversation to the LLM and returns its response.
	// The response may contain either a text answer or tool call requests.
	Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error)
}
