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

// Command agentctl is a kubectl-style CLI for the agent-operator.
//
// Usage:
//
//	agentctl trigger <agent-name> -n <namespace> -p "your prompt"
//	agentctl status <run-id> -n <namespace>
//	agentctl logs <run-id> -n <namespace>
//	agentctl send <run-id> -n <namespace> -m "follow-up message"
//	agentctl agents -n <namespace>
//	agentctl runs -n <namespace>
//
// Configuration (in order of precedence):
//  1. Flags: --gateway, --token
//  2. Environment: AGENT_GATEWAY_URL, AGENT_GATEWAY_TOKEN
//  3. Config file: ~/.agentctl/config
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/nickmerrett/agent-operator/sdk/go/client"
)

const usage = `agentctl — agent-operator CLI

Usage:
  agentctl trigger <agent> -n <namespace> -p <prompt> [--wait] [--callback <url>]
  agentctl status  <run-id> -n <namespace>
  agentctl logs    <run-id> -n <namespace>
  agentctl send    <run-id> -n <namespace> -m <message>
  agentctl runs    -n <namespace>
  agentctl version

Flags:
  -n, --namespace   Kubernetes namespace (required)
  -p, --prompt      Prompt text for trigger
  -m, --message     Follow-up message for send
  --wait            Wait for run to complete after trigger (default: false)
  --gateway         Gateway base URL (env: AGENT_GATEWAY_URL)
  --token           Bearer token    (env: AGENT_GATEWAY_TOKEN)

Examples:
  agentctl trigger research-agent -n team-alpha -p "Summarise the Go 1.23 release notes" --wait
  agentctl status research-agent-abc123 -n team-alpha
  agentctl logs research-agent-abc123 -n team-alpha
  agentctl send research-agent-abc123 -n team-alpha -m "Also include breaking changes"
  agentctl runs -n team-alpha
`

func main() {
	args := os.Args[1:]
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		fmt.Print(usage)
		os.Exit(0)
	}

	if args[0] == "version" {
		fmt.Println("agentctl v0.1.0 (agent-operator)")
		os.Exit(0)
	}

	cfg := loadConfig(args)
	c := client.New(cfg.gateway, cfg.token)
	ctx := context.Background()

	switch args[0] {
	case "trigger":
		cmdTrigger(ctx, c, cfg)
	case "status":
		cmdStatus(ctx, c, cfg)
	case "logs":
		cmdLogs(ctx, c, cfg)
	case "send":
		cmdSend(ctx, c, cfg)
	case "runs":
		cmdRuns(ctx, c, cfg)
	default:
		fatalf("unknown command %q — run agentctl --help", args[0])
	}
}

// ── Commands ──────────────────────────────────────────────────────────────────

func cmdTrigger(ctx context.Context, c *client.Client, cfg *config) {
	if cfg.arg == "" {
		fatalf("trigger requires an agent name: agentctl trigger <agent-name>")
	}
	if cfg.prompt == "" {
		fatalf("trigger requires a prompt: -p \"your prompt\"")
	}
	if cfg.namespace == "" {
		fatalf("trigger requires a namespace: -n <namespace>")
	}

	run, err := c.Trigger(ctx, cfg.namespace, cfg.arg, client.TriggerRequest{
		Prompt:      cfg.prompt,
		CallbackURL: cfg.callbackURL,
	})
	if err != nil {
		fatalf("trigger failed: %v", err)
	}

	fmt.Printf("✓ AgentRun started\n")
	fmt.Printf("  Run ID:     %s\n", run.RunID)
	fmt.Printf("  Result key: %s\n", run.ResultKey)
	fmt.Printf("  Status URL: %s\n", run.StatusURL)

	if cfg.wait {
		fmt.Printf("\nWaiting for completion")
		waitCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
		defer cancel()

		go func() {
			for {
				select {
				case <-waitCtx.Done():
					return
				case <-time.After(2 * time.Second):
					fmt.Print(".")
				}
			}
		}()

		result, err := run.Wait(waitCtx)
		fmt.Println()
		if err != nil {
			fatalf("wait failed: %v", err)
		}
		fmt.Printf("\n✓ Completed (phase: %s)\n", result.Phase)
		fmt.Printf("  Result key: %s\n", result.ResultKey)
		fmt.Printf("  Completed:  %s\n", result.CompletionTime)
		if result.Phase != "Succeeded" {
			fmt.Printf("\n⚠  Run did not succeed — check logs:\n")
			fmt.Printf("  agentctl logs %s -n %s\n", result.RunID, cfg.namespace)
			os.Exit(1)
		}
	}
}

func cmdStatus(ctx context.Context, c *client.Client, cfg *config) {
	if cfg.arg == "" {
		fatalf("status requires a run ID: agentctl status <run-id>")
	}
	if cfg.namespace == "" {
		fatalf("status requires a namespace: -n <namespace>")
	}

	status, err := c.Status(ctx, cfg.namespace, cfg.arg)
	if err != nil {
		fatalf("status failed: %v", err)
	}

	if cfg.jsonOutput {
		printJSON(status)
		return
	}

	fmt.Printf("Run ID:      %s\n", status.RunID)
	fmt.Printf("Phase:       %s\n", status.Phase)
	fmt.Printf("Pod:         %s\n", orDash(status.PodName))
	fmt.Printf("Result key:  %s\n", orDash(status.RedisResultKey))
	fmt.Printf("Started:     %s\n", orDash(status.StartTime))
	fmt.Printf("Completed:   %s\n", orDash(status.CompletionTime))
}

func cmdLogs(ctx context.Context, c *client.Client, cfg *config) {
	if cfg.arg == "" {
		fatalf("logs requires a run ID: agentctl logs <run-id>")
	}
	if cfg.namespace == "" {
		fatalf("logs requires a namespace: -n <namespace>")
	}

	logs, err := c.Logs(ctx, cfg.namespace, cfg.arg)
	if err != nil {
		fatalf("logs failed: %v", err)
	}
	fmt.Print(logs)
}

func cmdSend(ctx context.Context, c *client.Client, cfg *config) {
	if cfg.arg == "" {
		fatalf("send requires a run ID: agentctl send <run-id>")
	}
	if cfg.message == "" {
		fatalf("send requires a message: -m \"your message\"")
	}
	if cfg.namespace == "" {
		fatalf("send requires a namespace: -n <namespace>")
	}

	if err := c.SendMessage(ctx, cfg.namespace, cfg.arg, cfg.message); err != nil {
		fatalf("send failed: %v", err)
	}
	fmt.Println("✓ Message sent")
}

func cmdRuns(ctx context.Context, c *client.Client, cfg *config) {
	if cfg.namespace == "" {
		fatalf("runs requires a namespace: -n <namespace>")
	}
	// The gateway doesn't have a list-runs endpoint yet — direct users to kubectl.
	fmt.Printf("Tip: list runs with kubectl:\n\n")
	fmt.Printf("  kubectl get agentruns -n %s\n\n", cfg.namespace)
	fmt.Printf("Or watch live:\n\n")
	fmt.Printf("  kubectl get agentruns -n %s -w\n", cfg.namespace)
}

// ── Config parsing ────────────────────────────────────────────────────────────

type config struct {
	arg         string // first positional arg after the command
	namespace   string
	prompt      string
	message     string
	callbackURL string
	gateway     string
	token       string
	wait        bool
	jsonOutput  bool
}

func loadConfig(args []string) *config {
	cfg := &config{
		gateway: envOr("AGENT_GATEWAY_URL", "http://localhost:8080"),
		token:   envOr("AGENT_GATEWAY_TOKEN", ""),
	}

	// First arg after command is the positional subject (agent name or run ID).
	if len(args) > 1 && !isFlag(args[1]) {
		cfg.arg = args[1]
	}

	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "-n", "--namespace":
			cfg.namespace = nextArg(args, i)
			i++
		case "-p", "--prompt":
			cfg.prompt = nextArg(args, i)
			i++
		case "-m", "--message":
			cfg.message = nextArg(args, i)
			i++
		case "--callback":
			cfg.callbackURL = nextArg(args, i)
			i++
		case "--gateway":
			cfg.gateway = nextArg(args, i)
			i++
		case "--token":
			cfg.token = nextArg(args, i)
			i++
		case "--wait":
			cfg.wait = true
		case "--json":
			cfg.jsonOutput = true
		}
	}

	if cfg.gateway == "" {
		fatalf("gateway URL required: set --gateway or AGENT_GATEWAY_URL")
	}
	if cfg.token == "" {
		fatalf("token required: set --token or AGENT_GATEWAY_TOKEN")
	}

	return cfg
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func printJSON(v interface{}) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

func printTable(headers []string, rows [][]string) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, joinTab(headers))
	for _, row := range rows {
		fmt.Fprintln(w, joinTab(row))
	}
	w.Flush()
}

func joinTab(cols []string) string {
	result := ""
	for i, c := range cols {
		if i > 0 {
			result += "\t"
		}
		result += c
	}
	return result
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func isFlag(s string) bool {
	return len(s) > 0 && s[0] == '-'
}

func nextArg(args []string, i int) string {
	if i+1 >= len(args) {
		fatalf("flag %q requires a value", args[i])
	}
	return args[i+1]
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func fatalf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "error: "+format+"\n", args...)
	os.Exit(1)
}

// suppress unused import warning for tabwriter
var _ = tabwriter.NewWriter
var _ = printTable
