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

// This file is an example custom tool server built with the agent-operator ADK.
// It exposes two tools via MCP HTTP, discoverable by an MCPServer CR.
//
// Deploy this as a pod, then create:
//
//	apiVersion: agent.runtime.io/v1alpha1
//	kind: MCPServer
//	metadata:
//	  name: example-tools
//	  namespace: team-alpha
//	spec:
//	  transport: http
//	  endpoint: http://example-tools.team-alpha.svc.cluster.local:8080
//	  autoDiscoverTools: true
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/nickmerrett/agent-operator/sdk/go/tools"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	srv := tools.NewServer(":" + port)

	// ── Tool 1: echo ──────────────────────────────────────────────────────────
	srv.Register(tools.Tool{
		Name:        "echo",
		Description: "Echoes the input message back. Useful for testing.",
		InputSchema: tools.ObjectSchema(tools.Properties{
			"message": tools.StringProp("The message to echo"),
		}, "message"),
		Handler: func(ctx context.Context, input map[string]interface{}) (map[string]interface{}, error) {
			msg, _ := input["message"].(string)
			return map[string]interface{}{
				"echo": msg,
			}, nil
		},
	})

	// ── Tool 2: word-count ────────────────────────────────────────────────────
	srv.Register(tools.Tool{
		Name:        "word-count",
		Description: "Counts the number of words in a given text.",
		InputSchema: tools.ObjectSchema(tools.Properties{
			"text": tools.StringProp("The text to count words in"),
		}, "text"),
		Handler: func(ctx context.Context, input map[string]interface{}) (map[string]interface{}, error) {
			text, _ := input["text"].(string)
			words := strings.Fields(text)
			return map[string]interface{}{
				"wordCount":  len(words),
				"charCount":  len(text),
				"lineCount":  strings.Count(text, "\n") + 1,
			}, nil
		},
	})

	fmt.Printf("Example tool server starting on :%s\n", port)
	log.Fatal(srv.Start())
}
