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

// Package client provides a Go SDK for interacting with the agent-operator HTTP gateway.
//
// Quick start:
//
//	c := client.New("https://agent-gateway.example.com", "my-bearer-token")
//
//	run, err := c.Trigger(ctx, "team-alpha", "research-agent", client.TriggerRequest{
//	    Prompt: "What is the Go memory model?",
//	})
//
//	result, err := run.Wait(ctx)
//	fmt.Println(result.Answer)
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client is the agent-operator gateway SDK client.
type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

// New creates a new Client.
//
//	baseURL  — the gateway base URL, e.g. "https://agent-gateway.example.com"
//	token    — the Bearer token for the target namespace (agent-gateway-token secret)
func New(baseURL, token string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// ── Trigger ───────────────────────────────────────────────────────────────────

// TriggerRequest is the input to Trigger.
type TriggerRequest struct {
	// Prompt is the natural-language instruction for the agent.
	Prompt string `json:"prompt"`
	// Context is an optional map of key-value pairs injected into the agent's context.
	Context map[string]string `json:"context,omitempty"`
	// CallbackURL is an optional webhook URL. The runtime POSTs the result here on completion.
	CallbackURL string `json:"callbackURL,omitempty"`
}

// TriggerResponse is returned by a successful Trigger call.
type TriggerResponse struct {
	RunID          string `json:"runId"`
	StatusURL      string `json:"statusURL"`
	RedisResultKey string `json:"resultKey"`
}

// Trigger starts a new AgentRun and returns a Run handle for polling/waiting.
func (c *Client) Trigger(ctx context.Context, namespace, agentName string, req TriggerRequest) (*Run, error) {
	url := fmt.Sprintf("%s/v1/agents/%s/%s/run", c.baseURL, namespace, agentName)

	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("trigger: marshal: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("trigger: build request: %w", err)
	}
	c.setHeaders(httpReq)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("trigger: http: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusAccepted {
		return nil, fmt.Errorf("trigger: unexpected status %d: %s", resp.StatusCode, body)
	}

	var tr TriggerResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		return nil, fmt.Errorf("trigger: unmarshal response: %w", err)
	}

	return &Run{
		client:    c,
		namespace: namespace,
		RunID:     tr.RunID,
		ResultKey: tr.RedisResultKey,
		StatusURL: tr.StatusURL,
	}, nil
}

// ── Status ────────────────────────────────────────────────────────────────────

// RunStatus is the current state of an AgentRun.
type RunStatus struct {
	RunID          string `json:"runId"`
	Phase          string `json:"phase"`
	PodName        string `json:"podName,omitempty"`
	RedisResultKey string `json:"resultKey,omitempty"`
	StartTime      string `json:"startTime,omitempty"`
	CompletionTime string `json:"completionTime,omitempty"`
}

// Status fetches the current status of a run by ID.
func (c *Client) Status(ctx context.Context, namespace, runID string) (*RunStatus, error) {
	url := fmt.Sprintf("%s/v1/runs/%s/%s", c.baseURL, namespace, runID)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("status: build request: %w", err)
	}
	c.setHeaders(httpReq)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("status: http: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status: unexpected status %d: %s", resp.StatusCode, body)
	}

	var rs RunStatus
	if err := json.Unmarshal(body, &rs); err != nil {
		return nil, fmt.Errorf("status: unmarshal: %w", err)
	}
	return &rs, nil
}

// ── Interact ──────────────────────────────────────────────────────────────────

// SendMessage sends a follow-up message to an in-flight AgentRun.
// Returns an error if the run is not in Running phase.
func (c *Client) SendMessage(ctx context.Context, namespace, runID, message string) error {
	url := fmt.Sprintf("%s/v1/runs/%s/%s/message", c.baseURL, namespace, runID)

	data, _ := json.Marshal(map[string]string{"message": message})
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("send-message: build request: %w", err)
	}
	c.setHeaders(httpReq)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("send-message: http: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("send-message: unexpected status %d: %s", resp.StatusCode, body)
	}
	return nil
}

// ── Logs ──────────────────────────────────────────────────────────────────────

// Logs fetches the pod logs for a run and returns them as a string.
func (c *Client) Logs(ctx context.Context, namespace, runID string) (string, error) {
	url := fmt.Sprintf("%s/v1/runs/%s/%s/logs", c.baseURL, namespace, runID)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("logs: build request: %w", err)
	}
	c.setHeaders(httpReq)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("logs: http: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("logs: unexpected status %d: %s", resp.StatusCode, body)
	}
	return string(body), nil
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func (c *Client) setHeaders(r *http.Request) {
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer "+c.token)
}
