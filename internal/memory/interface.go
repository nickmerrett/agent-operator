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

// Package memory provides a common interface for agent memory backends and
// a concrete Redis implementation.
//
// Redis key schema:
//
//	Working memory (per AgentRun message history):
//	  agentrun:{agentrun-name}:messages   — Redis list of JSON-encoded Message objects
//
//	Result (written by agent-runtime on completion):
//	  agentrun:{agentrun-name}:result     — Redis hash: result, tokensUsed, iterations, error
//
//	Inbound interaction channel (gateway → runtime):
//	  agentrun:{agentrun-name}:inbound    — Redis pub/sub channel
//
//	Episodic memory (summaries across runs):
//	  agent:{agent-name}:episodes         — Redis sorted set, score = unix timestamp
package memory

import (
	"context"
	"time"

	"github.com/nickmerrett/agent-operator/internal/llm"
)

// Store is the common interface for all memory backends.
type Store interface {
	// AppendMessage appends a message to the working memory for an AgentRun.
	AppendMessage(ctx context.Context, agentRunName string, msg llm.Message) error

	// GetMessages retrieves the full working memory message list for an AgentRun.
	GetMessages(ctx context.Context, agentRunName string) ([]llm.Message, error)

	// SetResult writes the final result for an AgentRun.
	SetResult(ctx context.Context, agentRunName string, result Result) error

	// GetResult retrieves the result for an AgentRun (returns nil if not yet written).
	GetResult(ctx context.Context, agentRunName string) (*Result, error)

	// PublishInbound publishes a follow-up message from the gateway to a running AgentRun.
	PublishInbound(ctx context.Context, agentRunName string, message string) error

	// SubscribeInbound subscribes to inbound messages for a running AgentRun.
	// Returns a channel that receives messages and a cancel function to unsubscribe.
	SubscribeInbound(ctx context.Context, agentRunName string) (<-chan string, func(), error)

	// AppendEpisode persists a summary entry to the agent's episodic memory.
	AppendEpisode(ctx context.Context, agentName string, summary string, ts time.Time) error

	// GetEpisodes retrieves the N most recent episodic memory entries for an agent.
	GetEpisodes(ctx context.Context, agentName string, limit int) ([]Episode, error)

	// Ping verifies the backend is reachable.
	Ping(ctx context.Context) error
}

// Result holds the outcome of an AgentRun as stored in Redis.
type Result struct {
	Result     string `json:"result"`
	TokensUsed int    `json:"tokensUsed"`
	Iterations int    `json:"iterations"`
	Error      string `json:"error,omitempty"`
}

// Episode is a single episodic memory entry.
type Episode struct {
	Summary   string
	Timestamp time.Time
}
