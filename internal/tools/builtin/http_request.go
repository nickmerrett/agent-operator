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

package builtin

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/nickmerrett/agent-operator/internal/tools"
)

func httpRequestSpec() tools.Spec {
	return tools.Spec{
		Name:        "http-request",
		Description: "Make an HTTP request to an external URL. Supports GET, POST, PUT, DELETE, PATCH.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"url": map[string]interface{}{
					"type":        "string",
					"description": "The URL to request",
				},
				"method": map[string]interface{}{
					"type":        "string",
					"enum":        []string{"GET", "POST", "PUT", "DELETE", "PATCH"},
					"description": "HTTP method",
				},
				"headers": map[string]interface{}{
					"type":                 "object",
					"additionalProperties": map[string]interface{}{"type": "string"},
					"description":          "Optional HTTP headers as key-value pairs",
				},
				"body": map[string]interface{}{
					"type":        "string",
					"description": "Optional request body",
				},
				"timeoutSeconds": map[string]interface{}{
					"type":        "integer",
					"description": "Request timeout in seconds (default: 30)",
				},
			},
			"required": []string{"url", "method"},
		},
	}
}

type httpRequestExecutor struct{}

func (e *httpRequestExecutor) Execute(ctx context.Context, input map[string]interface{}) (map[string]interface{}, error) {
	url, _ := input["url"].(string)
	method, _ := input["method"].(string)
	if url == "" || method == "" {
		return nil, fmt.Errorf("http-request: url and method are required")
	}
	method = strings.ToUpper(method)

	bodyStr, _ := input["body"].(string)
	timeout := 30 * time.Second
	if t, ok := input["timeoutSeconds"].(float64); ok && t > 0 {
		timeout = time.Duration(t) * time.Second
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var bodyReader io.Reader
	if bodyStr != "" {
		bodyReader = bytes.NewBufferString(bodyStr)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("http-request: build request: %w", err)
	}

	if headers, ok := input["headers"].(map[string]interface{}); ok {
		for k, v := range headers {
			req.Header.Set(k, fmt.Sprintf("%v", v))
		}
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http-request: execute: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1MB limit

	respHeaders := make(map[string]string)
	for k, v := range resp.Header {
		respHeaders[k] = strings.Join(v, ", ")
	}

	return map[string]interface{}{
		"statusCode": resp.StatusCode,
		"body":       string(respBody),
		"headers":    respHeaders,
	}, nil
}
