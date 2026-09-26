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

// watsonxClient implements Client for IBM watsonx.ai.
// API reference: POST /ml/v1/text/generation
type watsonxClient struct {
	endpoint  string
	model     string
	apiKey    string
	projectID string
	maxTokens int
	temp      float64
	http      *http.Client
}

type watsonxRequest struct {
	ModelID    string             `json:"model_id"`
	Input      string             `json:"input"`
	Parameters watsonxParameters `json:"parameters"`
	ProjectID  string             `json:"project_id"`
}

type watsonxParameters struct {
	DecodingMethod string `json:"decoding_method"`
	MaxNewTokens   int    `json:"max_new_tokens"`
	Temperature    float64 `json:"temperature,omitempty"`
}

type watsonxResponse struct {
	Results []struct {
		GeneratedText string `json:"generated_text"`
		GeneratedTokenCount int `json:"generated_token_count"`
		InputTokenCount     int `json:"input_token_count"`
	} `json:"results"`
}

// watsonx does not support native function calling — we use a prompt-based
// tool-use approach: the tool list is injected into the system prompt and
// the model is asked to respond with a structured JSON tool call.
func (c *watsonxClient) Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error) {
	// Build a single prompt string from the message history.
	prompt := buildPromptFromMessages(req.Messages, req.Tools)

	body := watsonxRequest{
		ModelID: c.model,
		Input:   prompt,
		Parameters: watsonxParameters{
			DecodingMethod: "greedy",
			MaxNewTokens:   c.maxTokens,
			Temperature:    req.Temperature,
		},
		ProjectID: c.projectID,
	}

	data, err := json.Marshal(body)
	if err != nil {
		return CompletionResponse{}, fmt.Errorf("watsonx: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.endpoint+"/ml/v1/text/generation?version=2023-05-29", bytes.NewReader(data))
	if err != nil {
		return CompletionResponse{}, fmt.Errorf("watsonx: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return CompletionResponse{}, fmt.Errorf("watsonx: http: %w", err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return CompletionResponse{}, fmt.Errorf("watsonx: status %d: %s", resp.StatusCode, raw)
	}

	var wx watsonxResponse
	if err := json.Unmarshal(raw, &wx); err != nil {
		return CompletionResponse{}, fmt.Errorf("watsonx: unmarshal response: %w", err)
	}
	if len(wx.Results) == 0 {
		return CompletionResponse{}, fmt.Errorf("watsonx: empty results")
	}

	result := wx.Results[0]
	tokens := result.GeneratedTokenCount + result.InputTokenCount

	// Attempt to parse a tool call from the generated text.
	toolCalls, remaining := parseToolCallsFromText(result.GeneratedText)
	return CompletionResponse{
		Content:    remaining,
		ToolCalls:  toolCalls,
		TokensUsed: tokens,
	}, nil
}

// NewWatsonxClient creates a watsonx LLM client from provider config values.
func NewWatsonxClient(endpoint, model, apiKey, projectID string, params map[string]string) Client {
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
	return &watsonxClient{
		endpoint:  endpoint,
		model:     model,
		apiKey:    apiKey,
		projectID: projectID,
		maxTokens: maxTokens,
		temp:      temp,
		http:      &http.Client{Timeout: 120 * time.Second},
	}
}
