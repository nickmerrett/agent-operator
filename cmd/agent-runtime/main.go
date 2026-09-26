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

// Command agent-runtime is the brain binary that runs inside each ephemeral AgentRun pod.
// It reads configuration from environment variables injected by the AgentRun reconciler,
// drives the ReAct agentic loop, writes its result to Redis, and exits.
package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	agentv1alpha1 "github.com/nickmerrett/agent-operator/api/v1alpha1"
	"github.com/nickmerrett/agent-operator/internal/llm"
	"github.com/nickmerrett/agent-operator/internal/memory"
	internalmcp "github.com/nickmerrett/agent-operator/internal/mcp"
	agentruntime "github.com/nickmerrett/agent-operator/internal/runtime"
	"github.com/nickmerrett/agent-operator/internal/tools"
	"github.com/nickmerrett/agent-operator/internal/tools/builtin"
)

var scheme = k8sruntime.NewScheme()

func init() {
	_ = clientgoscheme.AddToScheme(scheme)
	_ = agentv1alpha1.AddToScheme(scheme)
}

func main() {
	ctx := context.Background()

	// ── Read environment ─────────────────────────────────────────────────────
	agentName := mustEnv("AGENT_NAME")
	agentNamespace := mustEnv("AGENT_NAMESPACE")
	agentRunName := mustEnv("AGENTRUN_NAME")
	promptB64 := mustEnv("INPUT_PROMPT")
	callbackURL := os.Getenv("CALLBACK_URL")

	promptBytes, err := base64.StdEncoding.DecodeString(promptB64)
	if err != nil {
		fatalf("decode INPUT_PROMPT: %v", err)
	}
	prompt := string(promptBytes)

	// ── Build in-cluster Kubernetes client ───────────────────────────────────
	k8sClient, err := client.New(ctrl.GetConfigOrDie(), client.Options{Scheme: scheme})
	if err != nil {
		fatalf("build k8s client: %v", err)
	}

	// ── Load Agent CR ────────────────────────────────────────────────────────
	var agent agentv1alpha1.Agent
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: agentName, Namespace: agentNamespace}, &agent); err != nil {
		fatalf("get Agent CR: %v", err)
	}

	// ── Load LLMProvider and credentials ─────────────────────────────────────
	var llmProvider agentv1alpha1.LLMProvider
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: agent.Spec.LLMProvider, Namespace: agentNamespace}, &llmProvider); err != nil {
		fatalf("get LLMProvider CR: %v", err)
	}
	llmCreds := secretFromEnv() // credentials injected as env vars via EnvFrom
	llmClient, err := llm.NewClientFromSpec(llmProvider.Spec, llmCreds)
	if err != nil {
		fatalf("build LLM client: %v", err)
	}

	// ── Load MemoryProvider and build store ───────────────────────────────────
	var memProvider agentv1alpha1.MemoryProvider
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: agent.Spec.MemoryProvider, Namespace: agentNamespace}, &memProvider); err != nil {
		fatalf("get MemoryProvider CR: %v", err)
	}
	memStore, err := memory.NewStoreFromSpec(memProvider.Spec, secretFromEnv())
	if err != nil {
		fatalf("build memory store: %v", err)
	}

	// ── Register built-in tools and build tool map ────────────────────────────
	cat := tools.Global()
	builtin.RegisterAll(cat, k8sClient)

	toolMap := make(map[string]tools.Executor)
	toolSpecs := make([]llm.ToolDefinition, 0)

	for _, toolName := range agent.Spec.Tools {
		var toolCR agentv1alpha1.Tool
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: toolName, Namespace: agentNamespace}, &toolCR); err != nil {
			logf("WARNING: tool %q not found, skipping: %v", toolName, err)
			continue
		}
		if toolCR.Spec.BuiltIn != "" {
			entry, ok := cat.Get(string(toolCR.Spec.BuiltIn))
			if !ok {
				logf("WARNING: built-in tool %q not in catalogue, skipping", toolCR.Spec.BuiltIn)
				continue
			}
			toolMap[string(toolCR.Spec.BuiltIn)] = entry.Executor
			toolSpecs = append(toolSpecs, entry.Spec.ToLLMToolDefinition())
		}
		// Custom image tools are not yet supported in the in-process runtime (v1).
	}

	// ── Add MCP server tools ──────────────────────────────────────────────────
	for _, mcpName := range agent.Spec.MCPServers {
		var mcpServer agentv1alpha1.MCPServer
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: mcpName, Namespace: agentNamespace}, &mcpServer); err != nil {
			logf("WARNING: MCPServer %q not found, skipping: %v", mcpName, err)
			continue
		}
		token := ""
		if mcpServer.Spec.CredentialsSecret != "" {
			token = os.Getenv("MCP_TOKEN_" + strings.ToUpper(strings.ReplaceAll(mcpName, "-", "_")))
		}
		mcpClient, err := internalmcp.NewClient(string(mcpServer.Spec.Transport), mcpServer.Spec.Endpoint, token)
		if err != nil {
			logf("WARNING: cannot connect to MCPServer %q: %v", mcpName, err)
			continue
		}
		for _, td := range mcpServer.Status.DiscoveredTools {
			toolMap[td.Name] = &mcpToolAdapter{client: mcpClient, name: td.Name}
			toolSpecs = append(toolSpecs, llm.ToolDefinition{
				Type: "function",
				Function: llm.FunctionDef{
					Name:        td.Name,
					Description: td.Description,
				},
			})
		}
	}

	// ── Run the agentic loop ──────────────────────────────────────────────────
	timeout := time.Duration(agent.Spec.TimeoutSeconds) * time.Second
	if timeout == 0 {
		timeout = 5 * time.Minute
	}

	cfg := agentruntime.LoopConfig{
		AgentName:     agentName,
		AgentRunName:  agentRunName,
		SystemPrompt:  agent.Spec.SystemPrompt,
		MaxIterations: agent.Spec.MaxIterations,
		Timeout:       timeout,
		LLMClient:     llmClient,
		MemoryStore:   memStore,
		Tools:         toolMap,
		ToolSpecs:     toolSpecs,
	}

	result := agentruntime.Run(ctx, cfg, prompt)

	// ── Write result to Redis ─────────────────────────────────────────────────
	if err := memStore.SetResult(ctx, agentRunName, memory.Result{
		Result:     result.Answer,
		TokensUsed: result.TokensUsed,
		Iterations: result.Iterations,
		Error:      result.Error,
	}); err != nil {
		logf("WARNING: failed to write result to Redis: %v", err)
	}

	// ── Persist episodic memory ───────────────────────────────────────────────
	if memProvider.Spec.EpisodicMemoryEnabled && result.Answer != "" {
		summary := fmt.Sprintf("Prompt: %s | Answer summary: %s", truncate(prompt, 100), truncate(result.Answer, 200))
		_ = memStore.AppendEpisode(ctx, agentName, summary, time.Now())
	}

	// ── Fire callback if set ──────────────────────────────────────────────────
	if callbackURL != "" {
		postCallback(callbackURL, result)
	}

	// Exit with non-zero if there was an error.
	if result.Error != "" {
		logf("AgentRun completed with error: %s", result.Error)
		os.Exit(1)
	}

	logf("AgentRun completed successfully in %d iterations, %d tokens", result.Iterations, result.TokensUsed)
}

// mustEnv reads a required environment variable or exits.
func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		fatalf("required environment variable %q is not set", key)
	}
	return v
}

// secretFromEnv reads all environment variables and returns them as a credentials map.
// Credentials are injected via EnvFrom on the pod spec.
func secretFromEnv() map[string]string {
	m := make(map[string]string)
	for _, e := range os.Environ() {
		parts := strings.SplitN(e, "=", 2)
		if len(parts) == 2 {
			// Lower-case the key so it matches the secret key conventions.
			m[strings.ToLower(parts[0])] = parts[1]
		}
	}
	return m
}

// postCallback POSTs the result to the callbackURL.
func postCallback(url string, result agentruntime.Result) {
	data, _ := json.Marshal(result)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		logf("callback: build request: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		logf("callback: post: %v", err)
		return
	}
	defer resp.Body.Close()
	logf("callback: posted result to %s (status %d)", url, resp.StatusCode)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func logf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "[agent-runtime] "+format+"\n", args...)
}

func fatalf(format string, args ...interface{}) {
	logf("FATAL: "+format, args...)
	os.Exit(1)
}

// mcpToolAdapter adapts an MCP client + tool name to the tools.Executor interface.
type mcpToolAdapter struct {
	client internalmcp.Client
	name   string
}

func (a *mcpToolAdapter) Execute(ctx context.Context, input map[string]interface{}) (map[string]interface{}, error) {
	return a.client.CallTool(ctx, a.name, input)
}
