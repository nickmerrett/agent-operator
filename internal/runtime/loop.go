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
// ephemeral AgentRun pod.
//
// The loop follows the ReAct pattern (Reason + Act):
//
//	Think (LLM) → Act (parallel tool calls) → Observe (tool results) → repeat
//
// Improvements over a basic loop:
//   - Parallel tool execution: all tool calls in a single LLM response run concurrently
//   - Per-tool retry with exponential backoff on transient errors
//   - Structured trace events emitted to Redis for observability
//   - Inbound message subscription: accepts follow-up messages mid-run
//   - Graceful timeout and maxIterations enforcement
package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/nickmerrett/agent-operator/internal/llm"
	"github.com/nickmerrett/agent-operator/internal/memory"
	"github.com/nickmerrett/agent-operator/internal/tools"
)

// LoopConfig holds all the configuration needed to run one agent loop.
type LoopConfig struct {
	AgentName     string
	AgentRunName  string
	SystemPrompt  string
	MaxIterations int32
	Timeout       time.Duration

	LLMClient   llm.Client
	MemoryStore memory.Store
	// Tools maps tool name → executor for all tools available to this agent.
	Tools map[string]tools.Executor
	// ToolSpecs are the LLM-facing definitions for all available tools.
	ToolSpecs []llm.ToolDefinition

	// Policy is the resolved effective policy for this run.
	// If nil, platform defaults are used.
	Policy *EffectivePolicy
}

// TraceEvent is a single observability event emitted during a run.
// Events are stored as JSON in Redis under agentrun:{name}:trace (list).
type TraceEvent struct {
	Iteration int         `json:"iteration"`
	Type      string      `json:"type"` // "think" | "tool_call" | "tool_result" | "inbound" | "final"
	Timestamp string      `json:"timestamp"`
	Data      interface{} `json:"data,omitempty"`
}

// toolResult holds the outcome of a single tool execution.
type toolResult struct {
	ToolCallID string
	ToolName   string
	Output     string
	Err        error
}

const (
	maxToolRetries    = 3
	toolRetryBaseWait = 500 * time.Millisecond
)

// Run executes the agentic loop and returns the final Result.
func Run(ctx context.Context, cfg LoopConfig, prompt string) Result {
	ctx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()

	// Resolve effective policy — use platform defaults if nil.
	policy := cfg.Policy
	if policy == nil {
		defaults := platformDefaults
		policy = &defaults
	}

	// Apply policy ceiling to maxIterations.
	maxIter := cfg.MaxIterations
	if policy.MaxIterations > 0 && policy.MaxIterations < maxIter {
		maxIter = policy.MaxIterations
	}

	// Subscribe to inbound messages (follow-ups from the gateway).
	inbound, unsubscribe, err := cfg.MemoryStore.SubscribeInbound(ctx, cfg.AgentRunName)
	if err != nil {
		// Non-fatal — run without inbound support.
		inbound = make(chan string)
	}
	defer unsubscribe()

	// Build initial message list.
	messages := buildInitialMessages(ctx, cfg)
	messages = append(messages, llm.Message{Role: llm.RoleUser, Content: prompt})

	// Persist initial messages to working memory.
	for _, m := range messages {
		_ = cfg.MemoryStore.AppendMessage(ctx, cfg.AgentRunName, m)
	}

	tokensUsed := 0
	totalToolCalls := 0
	perToolCalls := make(map[string]int32) // tool name → call count
	identicalCallCount := make(map[string]int32) // hash(tool+args) → count

	for i := int32(0); i < maxIter; i++ {
		// ── Drain any inbound messages before thinking ────────────────────────
		messages = drainInbound(inbound, messages, ctx)

		// ── Optional iteration delay (rate limiting) ──────────────────────────
		if policy.MinIterationDelayMs > 0 && i > 0 {
			select {
			case <-ctx.Done():
				return Result{Error: "context cancelled", Iterations: int(i), TokensUsed: tokensUsed}
			case <-time.After(time.Duration(policy.MinIterationDelayMs) * time.Millisecond):
			}
		}

		// ── Think: call the LLM ───────────────────────────────────────────────
		emitTrace(ctx, cfg, i, "think", map[string]interface{}{
			"messageCount": len(messages),
			"tokensUsed":   tokensUsed,
		})

		resp, err := cfg.LLMClient.Complete(ctx, llm.CompletionRequest{
			Messages:  messages,
			Tools:     cfg.ToolSpecs,
		})
		if err != nil {
			return Result{
				Error:      fmt.Sprintf("LLM error on iteration %d: %v", i, err),
				Iterations: int(i),
				TokensUsed: tokensUsed,
			}
		}
		tokensUsed += resp.TokensUsed

		// ── Token budget enforcement ───────────────────────────────────────────
		warningTokens := int64(policy.WarningThresholdPercent) * policy.MaxTokensPerRun / 100
		if int64(tokensUsed) >= policy.MaxTokensPerRun {
			// Hard limit hit.
			if policy.OnLimitExceeded == "HardStop" {
				return Result{
					Error:      fmt.Sprintf("token budget exhausted (%d tokens, limit %d)", tokensUsed, policy.MaxTokensPerRun),
					Iterations: int(i) + 1,
					TokensUsed: tokensUsed,
				}
			}
			// GracefulStop: inject wrap-up instruction and do one final LLM call.
			return gracefulStop(ctx, cfg, messages, tokensUsed, int(i)+1,
				fmt.Sprintf("Token budget reached (%d/%d tokens used). Provide your best final answer now with the information you have gathered so far.", tokensUsed, policy.MaxTokensPerRun))
		}
		if int64(tokensUsed) >= warningTokens && policy.WarningThresholdPercent > 0 {
			// Inject a soft warning into the conversation.
			warnMsg := fmt.Sprintf("[System] You have used %d of %d tokens. Please wrap up your answer soon.", tokensUsed, policy.MaxTokensPerRun)
			messages = append(messages, llm.Message{Role: llm.RoleUser, Content: warnMsg})
			_ = cfg.MemoryStore.AppendMessage(ctx, cfg.AgentRunName, llm.Message{Role: llm.RoleUser, Content: warnMsg})
		}

		// ── Final answer: no tool calls ────────────────────────────────────────
		if len(resp.ToolCalls) == 0 {
			answer := strings.TrimSpace(resp.Content)
			assistantMsg := llm.Message{Role: llm.RoleAssistant, Content: answer}
			_ = cfg.MemoryStore.AppendMessage(ctx, cfg.AgentRunName, assistantMsg)
			emitTrace(ctx, cfg, i, "final", map[string]interface{}{
				"answerLength": len(answer),
				"tokensUsed":   tokensUsed,
			})
			return Result{
				Answer:     answer,
				Iterations: int(i) + 1,
				TokensUsed: tokensUsed,
			}
		}

		// ── Act: execute all tool calls in parallel ───────────────────────────

		// Total tool call ceiling check.
		totalToolCalls += len(resp.ToolCalls)
		if int32(totalToolCalls) > policy.MaxToolCallsPerRun {
			if policy.OnLimitExceeded == "HardStop" {
				return Result{
					Error:      fmt.Sprintf("tool call limit exceeded (%d calls, limit %d)", totalToolCalls, policy.MaxToolCallsPerRun),
					Iterations: int(i) + 1,
					TokensUsed: tokensUsed,
				}
			}
			return gracefulStop(ctx, cfg, messages, tokensUsed, int(i)+1,
				fmt.Sprintf("Tool call limit reached (%d/%d calls used). Provide your best final answer now.", totalToolCalls, policy.MaxToolCallsPerRun))
		}

		// Per-tool call ceiling + loop detection.
		for _, tc := range resp.ToolCalls {
			if policy.MaxCallsPerTool > 0 {
				perToolCalls[tc.Name]++
				if perToolCalls[tc.Name] > policy.MaxCallsPerTool {
					return gracefulStop(ctx, cfg, messages, tokensUsed, int(i)+1,
						fmt.Sprintf("Tool %q has been called %d times (limit %d). Provide your best final answer now.", tc.Name, perToolCalls[tc.Name], policy.MaxCallsPerTool))
				}
			}
			if policy.LoopDetectionEnabled {
				key := tc.Name + "|" + tc.Arguments
				identicalCallCount[key]++
				if identicalCallCount[key] >= policy.LoopDetectionMaxIdenticalCalls {
					return gracefulStop(ctx, cfg, messages, tokensUsed, int(i)+1,
						fmt.Sprintf("Detected repeated identical call to tool %q (%d times). You appear to be in a loop. Provide your best answer with what you know.", tc.Name, identicalCallCount[key]))
				}
			}
		}

		emitTrace(ctx, cfg, i, "tool_call", map[string]interface{}{
			"tools": toolCallNames(resp.ToolCalls),
			"count": len(resp.ToolCalls),
		})

		// Record the assistant's tool call intent.
		toolCallMsg := llm.Message{
			Role:    llm.RoleAssistant,
			Content: marshalToolCalls(resp.ToolCalls),
		}
		messages = append(messages, toolCallMsg)
		_ = cfg.MemoryStore.AppendMessage(ctx, cfg.AgentRunName, toolCallMsg)

		// Execute all tool calls concurrently.
		results := executeToolCallsParallel(ctx, cfg.Tools, resp.ToolCalls)

		// Append observations in original order.
		for _, tr := range results {
			obsMsg := llm.Message{
				Role:       llm.RoleTool,
				Content:    tr.Output,
				ToolCallID: tr.ToolCallID,
				Name:       tr.ToolName,
			}
			messages = append(messages, obsMsg)
			_ = cfg.MemoryStore.AppendMessage(ctx, cfg.AgentRunName, obsMsg)

			emitTrace(ctx, cfg, i, "tool_result", map[string]interface{}{
				"tool":    tr.ToolName,
				"hasErr":  tr.Err != nil,
				"outputLen": len(tr.Output),
			})
		}

		// Check context deadline after each iteration.
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

	// maxIter reached.
	if policy.OnLimitExceeded == "HardStop" {
		return Result{
			Error:      fmt.Sprintf("reached maximum iterations (%d) without a final answer", maxIter),
			Iterations: int(maxIter),
			TokensUsed: tokensUsed,
		}
	}
	return gracefulStop(ctx, cfg, messages, tokensUsed, int(maxIter),
		fmt.Sprintf("You have reached the maximum number of reasoning steps (%d). Provide your best final answer now with what you have gathered.", maxIter))
}

// gracefulStop injects a wrap-up message and does one final LLM call to get a
// partial-but-useful answer rather than returning an empty error result.
func gracefulStop(ctx context.Context, cfg LoopConfig, messages []llm.Message, tokensUsed, iterations int, wrapUpMsg string) Result {
	messages = append(messages, llm.Message{Role: llm.RoleUser, Content: wrapUpMsg})
	resp, err := cfg.LLMClient.Complete(ctx, llm.CompletionRequest{
		Messages: messages,
		// No tools on the final call — we want a text answer.
	})
	if err != nil {
		return Result{
			Error:      fmt.Sprintf("graceful stop LLM call failed: %v", err),
			Iterations: iterations,
			TokensUsed: tokensUsed,
		}
	}
	return Result{
		Answer:     strings.TrimSpace(resp.Content),
		Iterations: iterations,
		TokensUsed: tokensUsed + resp.TokensUsed,
	}
}

// ── Parallel tool execution ───────────────────────────────────────────────────

// executeToolCallsParallel runs all tool calls concurrently and returns results
// in the same order as the input slice.
func executeToolCallsParallel(ctx context.Context, toolMap map[string]tools.Executor, calls []llm.ToolCall) []toolResult {
	results := make([]toolResult, len(calls))
	var wg sync.WaitGroup

	for idx, tc := range calls {
		wg.Add(1)
		go func(i int, call llm.ToolCall) {
			defer wg.Done()
			output, err := executeWithRetry(ctx, toolMap, call)
			results[i] = toolResult{
				ToolCallID: call.ID,
				ToolName:   call.Name,
				Output:     output,
				Err:        err,
			}
		}(idx, tc)
	}

	wg.Wait()
	return results
}

// executeWithRetry executes a single tool call with exponential backoff retry
// on transient errors. Tool errors are always returned as observations —
// they never crash the loop.
func executeWithRetry(ctx context.Context, toolMap map[string]tools.Executor, tc llm.ToolCall) (string, error) {
	executor, ok := toolMap[tc.Name]
	if !ok {
		errMsg := fmt.Sprintf("ERROR: tool %q not found in available tools", tc.Name)
		return errMsg, fmt.Errorf("tool not found: %s", tc.Name)
	}

	var args map[string]interface{}
	if err := json.Unmarshal([]byte(tc.Arguments), &args); err != nil {
		errMsg := fmt.Sprintf("ERROR: could not parse arguments for tool %q: %v", tc.Name, err)
		return errMsg, err
	}

	var lastErr error
	for attempt := 0; attempt < maxToolRetries; attempt++ {
		if attempt > 0 {
			wait := toolRetryBaseWait * time.Duration(1<<uint(attempt-1))
			select {
			case <-ctx.Done():
				return fmt.Sprintf("ERROR: context cancelled while retrying tool %q", tc.Name), ctx.Err()
			case <-time.After(wait):
			}
		}

		toolCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		output, err := executor.Execute(toolCtx, args)
		cancel()

		if err == nil {
			data, _ := json.Marshal(output)
			return string(data), nil
		}
		lastErr = err

		// Only retry on what look like transient errors.
		if !isTransientError(err) {
			break
		}
	}

	// Return error as an observation — the agent can reason about it.
	return fmt.Sprintf("ERROR from tool %q (after %d attempts): %v", tc.Name, maxToolRetries, lastErr), lastErr
}

// isTransientError returns true for errors that are worth retrying.
func isTransientError(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "timeout") ||
		strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "temporary") ||
		strings.Contains(msg, "503") ||
		strings.Contains(msg, "429")
}

// ── Inbound message handling ──────────────────────────────────────────────────

// drainInbound non-blockingly drains any queued inbound messages and appends
// them as user turns to the message history.
func drainInbound(inbound <-chan string, messages []llm.Message, ctx context.Context) []llm.Message {
	for {
		select {
		case msg, ok := <-inbound:
			if !ok {
				return messages
			}
			messages = append(messages, llm.Message{
				Role:    llm.RoleUser,
				Content: "[Follow-up] " + msg,
			})
		case <-ctx.Done():
			return messages
		default:
			return messages
		}
	}
}

// ── Trace emission ────────────────────────────────────────────────────────────

// emitTrace appends a structured trace event to Redis.
// Failures are silently ignored — tracing must not affect the run.
func emitTrace(ctx context.Context, cfg LoopConfig, iteration int32, eventType string, data interface{}) {
	event := TraceEvent{
		Iteration: int(iteration),
		Type:      eventType,
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		Data:      data,
	}
	eventJSON, err := json.Marshal(event)
	if err != nil {
		return
	}
	// Publish as a message to the agentrun trace channel.
	// The memory.Store doesn't have a dedicated trace method, so we reuse
	// AppendMessage with a special role to keep things simple.
	_ = cfg.MemoryStore.AppendMessage(ctx, cfg.AgentRunName+":trace", llm.Message{
		Role:    "trace",
		Content: string(eventJSON),
	})
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func buildInitialMessages(ctx context.Context, cfg LoopConfig) []llm.Message {
	systemContent := cfg.SystemPrompt

	episodes, err := cfg.MemoryStore.GetEpisodes(ctx, cfg.AgentName, 5)
	if err == nil && len(episodes) > 0 {
		var sb strings.Builder
		sb.WriteString("\n\n## Relevant past interactions:\n")
		for _, ep := range episodes {
			sb.WriteString(fmt.Sprintf("- [%s] %s\n", ep.Timestamp.Format(time.RFC3339), ep.Summary))
		}
		systemContent += sb.String()
	}

	return []llm.Message{{Role: llm.RoleSystem, Content: systemContent}}
}

func marshalToolCalls(calls []llm.ToolCall) string {
	data, _ := json.Marshal(calls)
	return string(data)
}

func toolCallNames(calls []llm.ToolCall) []string {
	names := make([]string, len(calls))
	for i, c := range calls {
		names[i] = c.Name
	}
	return names
}
