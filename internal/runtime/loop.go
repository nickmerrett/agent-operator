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

// Package runtime implements the agentic reasoning loop that runs inside each
// ephemeral AgentRun pod. It drives the ReAct pattern:
//
//	Think (LLM) → Act (tool call) → Observe (tool result) → repeat
package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/nickmerrett/agent-operator/internal/llm"
	"github.com/nickmerrett/agent-operator/internal/memory"
	"github.com/nickmerrett/agent-operator/internal/tools"
)

// LoopConfig holds all the configuration needed to run one agent loop.
type LoopConfig struct {
	AgentName    string
	AgentRunName string
	SystemPrompt string
	MaxIterations int32
	Timeout      time.Duration

	LLMClient   llm.Client
	MemoryStore memory.Store
	// Tools maps tool name → executor for all tools available to this agent.
	Tools map[string]tools.Executor
	// ToolSpecs are the LLM-facing definitions for all available tools.
	ToolSpecs []llm.ToolDefinition
}

// Run executes the agentic loop and returns the final Result.
func Run(ctx context.Context, cfg LoopConfig, prompt string) Result {
	ctx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()

	// Build the initial message list: system prompt + episodic memory + user prompt.
	messages := buildInitialMessages(ctx, cfg)
	messages = append(messages, llm.Message{Role: llm.RoleUser, Content: prompt})

	// Persist the initial messages to working memory.
	for _, m := range messages {
		_ = cfg.MemoryStore.AppendMessage(ctx, cfg.AgentRunName, m)
	}

	tokensUsed := 0

	for i := int32(0); i < cfg.MaxIterations; i++ {
		// ── Think: ask the LLM ───────────────────────────────────────────────
		resp, err := cfg.LLMClient.Complete(ctx, llm.CompletionRequest{
			Messages: messages,
			Tools:    cfg.ToolSpecs,
		})
		if err != nil {
			return Result{
				Error:      fmt.Sprintf("LLM error on iteration %d: %v", i, err),
				Iterations: int(i),
				TokensUsed: tokensUsed,
			}
		}
		tokensUsed += resp.TokensUsed

		// ── Final answer: no tool call ────────────────────────────────────────
		if len(resp.ToolCalls) == 0 {
			assistantMsg := llm.Message{Role: llm.RoleAssistant, Content: resp.Content}
			_ = cfg.MemoryStore.AppendMessage(ctx, cfg.AgentRunName, assistantMsg)
			return Result{
				Answer:     resp.Content,
				Iterations: int(i) + 1,
				TokensUsed: tokensUsed,
			}
		}

		// ── Act: execute tool calls ───────────────────────────────────────────
		// Append the assistant's tool call message.
		toolCallMsg := buildToolCallMessage(resp.ToolCalls)
		messages = append(messages, toolCallMsg)
		_ = cfg.MemoryStore.AppendMessage(ctx, cfg.AgentRunName, toolCallMsg)

		// Execute each tool call and append the observation.
		for _, tc := range resp.ToolCalls {
			observation := executeToolCall(ctx, cfg.Tools, tc)
			obsMsg := llm.Message{
				Role:       llm.RoleTool,
				Content:    observation,
				ToolCallID: tc.ID,
				Name:       tc.Name,
			}
			messages = append(messages, obsMsg)
			_ = cfg.MemoryStore.AppendMessage(ctx, cfg.AgentRunName, obsMsg)
		}

		// Check for inbound follow-up messages (non-blocking).
		select {
		case <-ctx.Done():
			return Result{
				Error:      "context deadline exceeded",
				Iterations: int(i) + 1,
				TokensUsed: tokensUsed,
			}
		default:
		}
	}

	return Result{
		Error:      fmt.Sprintf("reached maximum iterations (%d) without a final answer", cfg.MaxIterations),
		Iterations: int(cfg.MaxIterations),
		TokensUsed: tokensUsed,
	}
}

// buildInitialMessages constructs the initial message list from system prompt + episodic memory.
func buildInitialMessages(ctx context.Context, cfg LoopConfig) []llm.Message {
	var msgs []llm.Message

	systemContent := cfg.SystemPrompt
	// Prepend episodic memory summary if available.
	episodes, err := cfg.MemoryStore.GetEpisodes(ctx, cfg.AgentName, 5)
	if err == nil && len(episodes) > 0 {
		var sb strings.Builder
		sb.WriteString("\n\n## Relevant past interactions:\n")
		for _, ep := range episodes {
			sb.WriteString(fmt.Sprintf("- [%s] %s\n", ep.Timestamp.Format(time.RFC3339), ep.Summary))
		}
		systemContent += sb.String()
	}

	msgs = append(msgs, llm.Message{Role: llm.RoleSystem, Content: systemContent})
	return msgs
}

// buildToolCallMessage creates an assistant message that records the tool call(s).
func buildToolCallMessage(toolCalls []llm.ToolCall) llm.Message {
	// Serialise tool calls as JSON so the message is inspectable.
	data, _ := json.Marshal(toolCalls)
	return llm.Message{
		Role:    llm.RoleAssistant,
		Content: string(data),
	}
}

// executeToolCall runs a single tool call and returns the observation string.
// Errors are captured as observations rather than panics — the agent can recover.
func executeToolCall(ctx context.Context, toolMap map[string]tools.Executor, tc llm.ToolCall) string {
	executor, ok := toolMap[tc.Name]
	if !ok {
		return fmt.Sprintf("ERROR: tool %q not found", tc.Name)
	}

	var args map[string]interface{}
	if err := json.Unmarshal([]byte(tc.Arguments), &args); err != nil {
		return fmt.Sprintf("ERROR: could not parse arguments for tool %q: %v", tc.Name, err)
	}

	toolCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	output, err := executor.Execute(toolCtx, args)
	if err != nil {
		return fmt.Sprintf("ERROR from tool %q: %v", tc.Name, err)
	}

	data, err := json.Marshal(output)
	if err != nil {
		return fmt.Sprintf("ERROR: could not serialise output from tool %q: %v", tc.Name, err)
	}
	return string(data)
}
