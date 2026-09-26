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

package llm

import (
	"encoding/json"
	"fmt"
	"strings"
)

// buildPromptFromMessages constructs a single text prompt from a message history.
// Used for providers that do not support the chat completions format natively
// (e.g. watsonx text generation endpoint).
// Tools are described in the system prompt so the model knows what it can call.
func buildPromptFromMessages(messages []Message, tools []ToolDefinition) string {
	var sb strings.Builder

	if len(tools) > 0 {
		sb.WriteString("You have access to the following tools. To call a tool, respond with a JSON object of the form:\n")
		sb.WriteString(`{"tool_call": {"name": "<tool_name>", "arguments": {<args>}}}\n`)
		sb.WriteString("If you do not need a tool, respond with your final answer directly.\n\n")
		sb.WriteString("Available tools:\n")
		for _, t := range tools {
			sb.WriteString(fmt.Sprintf("- %s: %s\n", t.Function.Name, t.Function.Description))
		}
		sb.WriteString("\n")
	}

	for _, m := range messages {
		switch m.Role {
		case RoleSystem:
			sb.WriteString("[System]: ")
		case RoleUser:
			sb.WriteString("[User]: ")
		case RoleAssistant:
			sb.WriteString("[Assistant]: ")
		case RoleTool:
			sb.WriteString(fmt.Sprintf("[Tool(%s)]: ", m.Name))
		}
		sb.WriteString(m.Content)
		sb.WriteString("\n")
	}
	sb.WriteString("[Assistant]: ")
	return sb.String()
}

// textToolCall is the JSON shape we expect from prompt-based providers.
type textToolCall struct {
	ToolCall struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	} `json:"tool_call"`
}

// parseToolCallsFromText attempts to extract a JSON tool call from a text response.
// Returns the parsed tool calls and the remaining text (the non-JSON part).
func parseToolCallsFromText(text string) ([]ToolCall, string) {
	text = strings.TrimSpace(text)
	// Find the first '{' and try to parse from there.
	start := strings.Index(text, "{")
	if start == -1 {
		return nil, text
	}
	candidate := text[start:]
	var tc textToolCall
	if err := json.Unmarshal([]byte(candidate), &tc); err != nil {
		return nil, text
	}
	if tc.ToolCall.Name == "" {
		return nil, text
	}
	return []ToolCall{{
		ID:        "prompt-tc-0",
		Name:      tc.ToolCall.Name,
		Arguments: string(tc.ToolCall.Arguments),
	}}, text[:start]
}
