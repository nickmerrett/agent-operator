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
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/nickmerrett/agent-operator/internal/tools"
)

// shellAllowList is the set of permitted command prefixes for shell-exec.
// Configurable via SHELL_ALLOW_LIST env var (colon-separated).
var shellAllowList = []string{"echo", "cat", "ls", "grep", "awk", "sed", "sort", "wc", "head", "tail", "find", "curl", "wget", "python3", "python", "node", "jq", "yq"}

func init() {
	if env := os.Getenv("SHELL_ALLOW_LIST"); env != "" {
		shellAllowList = strings.Split(env, ":")
	}
}

func isAllowed(cmd string) bool {
	// Extract the base command (first token).
	parts := strings.Fields(cmd)
	if len(parts) == 0 {
		return false
	}
	base := parts[0]
	// Strip any path prefix.
	if idx := strings.LastIndex(base, "/"); idx >= 0 {
		base = base[idx+1:]
	}
	for _, allowed := range shellAllowList {
		if base == allowed {
			return true
		}
	}
	return false
}

func shellExecSpec() tools.Spec {
	return tools.Spec{
		Name:        "shell-exec",
		Description: "Execute a shell command and return its stdout, stderr, and exit code. Only permitted commands from the allow-list may be run.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"command": map[string]interface{}{
					"type":        "string",
					"description": "The shell command to execute",
				},
				"timeoutSeconds": map[string]interface{}{
					"type":        "integer",
					"description": "Command timeout in seconds (default: 30)",
				},
				"workingDir": map[string]interface{}{
					"type":        "string",
					"description": "Working directory for the command (default: /workspace)",
				},
			},
			"required": []string{"command"},
		},
	}
}

type shellExecExecutor struct{}

func (e *shellExecExecutor) Execute(ctx context.Context, input map[string]interface{}) (map[string]interface{}, error) {
	command, _ := input["command"].(string)
	if command == "" {
		return nil, fmt.Errorf("shell-exec: command is required")
	}
	if !isAllowed(command) {
		return nil, fmt.Errorf("shell-exec: command %q is not in the allow-list", strings.Fields(command)[0])
	}

	timeout := 30 * time.Second
	if t, ok := input["timeoutSeconds"].(float64); ok && t > 0 {
		timeout = time.Duration(t) * time.Second
	}

	workDir := "/workspace"
	if wd, ok := input["workingDir"].(string); ok && wd != "" {
		workDir = wd
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", command)
	cmd.Dir = workDir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return nil, fmt.Errorf("shell-exec: run: %w", err)
		}
	}

	return map[string]interface{}{
		"stdout":   stdout.String(),
		"stderr":   stderr.String(),
		"exitCode": exitCode,
	}, nil
}
