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

package client

import (
	"context"
	"fmt"
	"time"
)

// terminalPhases are AgentRun phases that indicate the run has finished.
var terminalPhases = map[string]bool{
	"Succeeded": true,
	"Failed":    true,
	"Timedout":  true,
}

// Run is a handle to an in-flight or completed AgentRun.
// Obtained from Client.Trigger.
type Run struct {
	client    *Client
	namespace string

	// RunID is the AgentRun CR name.
	RunID string
	// ResultKey is the Redis key where the result will be written.
	ResultKey string
	// StatusURL is the gateway URL to poll for status.
	StatusURL string
}

// Result is the outcome of a completed AgentRun as returned by Wait.
type Result struct {
	// Phase is the final phase: Succeeded, Failed, or Timedout.
	Phase string
	// RunID is the AgentRun name.
	RunID string
	// ResultKey is the Redis key containing the full result payload.
	// Callers can read this key directly from Redis for the answer text.
	ResultKey string
	// StartTime is when the run started (RFC3339 string, may be empty).
	StartTime string
	// CompletionTime is when the run completed (RFC3339 string, may be empty).
	CompletionTime string
}

// Wait polls the gateway until the AgentRun reaches a terminal phase
// (Succeeded, Failed, or Timedout) and returns the result.
//
// pollInterval controls how often to poll (default: 2 seconds).
// The context deadline controls the overall wait timeout.
func (r *Run) Wait(ctx context.Context, pollInterval ...time.Duration) (*Result, error) {
	interval := 2 * time.Second
	if len(pollInterval) > 0 && pollInterval[0] > 0 {
		interval = pollInterval[0]
	}

	for {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("wait: context cancelled while waiting for run %q", r.RunID)
		case <-time.After(interval):
		}

		status, err := r.client.Status(ctx, r.namespace, r.RunID)
		if err != nil {
			// Transient error — keep polling.
			continue
		}

		if terminalPhases[status.Phase] {
			return &Result{
				Phase:          status.Phase,
				RunID:          r.RunID,
				ResultKey:      status.RedisResultKey,
				StartTime:      status.StartTime,
				CompletionTime: status.CompletionTime,
			}, nil
		}
	}
}

// Send sends a follow-up message to this in-flight run.
// Convenience wrapper around Client.SendMessage.
func (r *Run) Send(ctx context.Context, message string) error {
	return r.client.SendMessage(ctx, r.namespace, r.RunID, message)
}

// Logs fetches the pod logs for this run.
// Convenience wrapper around Client.Logs.
func (r *Run) Logs(ctx context.Context) (string, error) {
	return r.client.Logs(ctx, r.namespace, r.RunID)
}
