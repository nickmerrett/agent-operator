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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

// ollamaClient implements Client for Ollama.
// API reference: POST /api/chat
type ollamaClient struct {
	endpoint  string
	model     string
	maxTokens int
	temp      float64
	http      *http.Client
}

type ollamaRequest struct {
	Model    string          `json:"model"`
	Messages []ollamaMessage `json:"messages"`
	Stream   bool            `json:"stream"`
	Options  ollamaOptions   `json:"options,omitempty"`
	Tools    []ToolDefinition `json:"tools,omitempty"`
}

type ollamaMessage struct {
	Role      string           `json:"role"`
	Content   string           `json:"content"`
	ToolCalls []ollamaToolCall `json:"tool_calls,omitempty"`
}

type ollamaToolCall struct {
	Function struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	} `json:"function"`
}

type ollamaOptions struct {
	NumPredict  int     `json:"num_predict,omitempty"`
	Temperature float64 `json:"temperature,omitempty"`
}

type ollamaResponse struct {
	Message struct {
		Content   string           `json:"content"`
		ToolCalls []ollamaToolCall `json:"tool_calls"`
	} `json:"message"`
	PromptEvalCount int `json:"prompt_eval_count"`
	EvalCount       int `json:"eval_count"`
}

func (c *ollamaClient) Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error) {
	msgs := make([]ollamaMessage, 0, len(req.Messages))
	for _, m := range req.Messages {
		msgs = append(msgs, ollamaMessage{
			Role:    string(m.Role),
			Content: m.Content,
		})
	}

	body := ollamaRequest{
		Model:    c.model,
		Messages: msgs,
		Stream:   false,
		Options: ollamaOptions{
			NumPredict:  c.maxTokens,
			Temperature: req.Temperature,
		},
		Tools: req.Tools,
	}

	data, err := json.Marshal(body)
	if err != nil {
		return CompletionResponse{}, fmt.Errorf("ollama: marshal: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.endpoint+"/api/chat", bytes.NewReader(data))
	if err != nil {
		return CompletionResponse{}, fmt.Errorf("ollama: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return CompletionResponse{}, fmt.Errorf("ollama: http: %w", err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return CompletionResponse{}, fmt.Errorf("ollama: status %d: %s", resp.StatusCode, raw)
	}

	var ol ollamaResponse
	if err := json.Unmarshal(raw, &ol); err != nil {
		return CompletionResponse{}, fmt.Errorf("ollama: unmarshal: %w", err)
	}

	var toolCalls []ToolCall
	for i, tc := range ol.Message.ToolCalls {
		toolCalls = append(toolCalls, ToolCall{
			ID:        fmt.Sprintf("ollama-tc-%d", i),
			Name:      tc.Function.Name,
			Arguments: string(tc.Function.Arguments),
		})
	}

	return CompletionResponse{
		Content:    ol.Message.Content,
		ToolCalls:  toolCalls,
		TokensUsed: ol.PromptEvalCount + ol.EvalCount,
	}, nil
}

// NewOllamaClient creates an Ollama LLM client.
func NewOllamaClient(endpoint, model string, params map[string]string) Client {
	maxTokens := 4096
	temp := 0.7
	if v, ok := params["maxTokens"]; ok {
		if n, err := strconv.Atoi(v); err == nil {
			maxTokens = n
		}
	}
	if v, ok := params["temperature"]; ok {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			temp = f
		}
	}
	return &ollamaClient{
		endpoint:  endpoint,
		model:     model,
		maxTokens: maxTokens,
		temp:      temp,
		http:      &http.Client{Timeout: 120 * time.Second},
	}
}
