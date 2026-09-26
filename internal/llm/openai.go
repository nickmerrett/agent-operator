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

// openAIClient implements Client for OpenAI-compatible APIs (OpenAI, Azure OpenAI, etc.)
// API reference: POST /v1/chat/completions
type openAIClient struct {
	endpoint  string
	model     string
	apiKey    string
	maxTokens int
	temp      float64
	http      *http.Client
}

type openAIRequest struct {
	Model       string             `json:"model"`
	Messages    []openAIMessage    `json:"messages"`
	Tools       []ToolDefinition   `json:"tools,omitempty"`
	ToolChoice  string             `json:"tool_choice,omitempty"`
	MaxTokens   int                `json:"max_tokens,omitempty"`
	Temperature float64            `json:"temperature"`
}

type openAIMessage struct {
	Role       string             `json:"role"`
	Content    string             `json:"content,omitempty"`
	ToolCallID string             `json:"tool_call_id,omitempty"`
	Name       string             `json:"name,omitempty"`
	ToolCalls  []openAIToolCall   `json:"tool_calls,omitempty"`
}

type openAIToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type openAIResponse struct {
	Choices []struct {
		Message struct {
			Content   string           `json:"content"`
			ToolCalls []openAIToolCall `json:"tool_calls"`
		} `json:"message"`
	} `json:"choices"`
	Usage struct {
		TotalTokens int `json:"total_tokens"`
	} `json:"usage"`
}

func (c *openAIClient) Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error) {
	msgs := make([]openAIMessage, 0, len(req.Messages))
	for _, m := range req.Messages {
		msgs = append(msgs, openAIMessage{
			Role:       string(m.Role),
			Content:    m.Content,
			ToolCallID: m.ToolCallID,
			Name:       m.Name,
		})
	}

	var toolChoice string
	if len(req.Tools) > 0 {
		toolChoice = "auto"
	}

	body := openAIRequest{
		Model:       c.model,
		Messages:    msgs,
		Tools:       req.Tools,
		ToolChoice:  toolChoice,
		MaxTokens:   c.maxTokens,
		Temperature: req.Temperature,
	}

	data, err := json.Marshal(body)
	if err != nil {
		return CompletionResponse{}, fmt.Errorf("openai: marshal: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.endpoint+"/v1/chat/completions", bytes.NewReader(data))
	if err != nil {
		return CompletionResponse{}, fmt.Errorf("openai: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return CompletionResponse{}, fmt.Errorf("openai: http: %w", err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return CompletionResponse{}, fmt.Errorf("openai: status %d: %s", resp.StatusCode, raw)
	}

	var oai openAIResponse
	if err := json.Unmarshal(raw, &oai); err != nil {
		return CompletionResponse{}, fmt.Errorf("openai: unmarshal: %w", err)
	}
	if len(oai.Choices) == 0 {
		return CompletionResponse{}, fmt.Errorf("openai: no choices in response")
	}

	choice := oai.Choices[0].Message
	var toolCalls []ToolCall
	for _, tc := range choice.ToolCalls {
		toolCalls = append(toolCalls, ToolCall{
			ID:        tc.ID,
			Name:      tc.Function.Name,
			Arguments: tc.Function.Arguments,
		})
	}

	return CompletionResponse{
		Content:    choice.Content,
		ToolCalls:  toolCalls,
		TokensUsed: oai.Usage.TotalTokens,
	}, nil
}

// NewOpenAIClient creates an OpenAI-compatible LLM client.
func NewOpenAIClient(endpoint, model, apiKey string, params map[string]string) Client {
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
	return &openAIClient{
		endpoint:  endpoint,
		model:     model,
		apiKey:    apiKey,
		maxTokens: maxTokens,
		temp:      temp,
		http:      &http.Client{Timeout: 120 * time.Second},
	}
}
