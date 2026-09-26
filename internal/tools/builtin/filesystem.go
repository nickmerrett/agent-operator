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
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nickmerrett/agent-operator/internal/tools"
)

// allowedBasePaths is the set of allowed root paths for filesystem operations.
// In AgentRun pods this is set from the ALLOWED_FS_PATHS env var.
var allowedBasePaths = []string{"/workspace", "/tmp"}

func init() {
	if env := os.Getenv("ALLOWED_FS_PATHS"); env != "" {
		allowedBasePaths = strings.Split(env, ":")
	}
}

func safePath(path string) (string, error) {
	clean := filepath.Clean(path)
	for _, base := range allowedBasePaths {
		if strings.HasPrefix(clean, filepath.Clean(base)) {
			return clean, nil
		}
	}
	return "", fmt.Errorf("filesystem: path %q is outside allowed base paths %v", path, allowedBasePaths)
}

// ── filesystem-read ───────────────────────────────────────────────────────────

func fsReadSpec() tools.Spec {
	return tools.Spec{
		Name:        "filesystem-read",
		Description: "Read the contents of a file from the filesystem.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]interface{}{
					"type":        "string",
					"description": "Absolute path to the file to read",
				},
			},
			"required": []string{"path"},
		},
	}
}

type fsReadExecutor struct{}

func (e *fsReadExecutor) Execute(_ context.Context, input map[string]interface{}) (map[string]interface{}, error) {
	path, _ := input["path"].(string)
	safe, err := safePath(path)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(safe)
	if err != nil {
		return nil, fmt.Errorf("filesystem-read: %w", err)
	}
	return map[string]interface{}{
		"content": string(data),
		"size":    len(data),
	}, nil
}

// ── filesystem-write ──────────────────────────────────────────────────────────

func fsWriteSpec() tools.Spec {
	return tools.Spec{
		Name:        "filesystem-write",
		Description: "Write content to a file on the filesystem. Creates the file if it does not exist.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]interface{}{
					"type":        "string",
					"description": "Absolute path to the file to write",
				},
				"content": map[string]interface{}{
					"type":        "string",
					"description": "Content to write to the file",
				},
			},
			"required": []string{"path", "content"},
		},
	}
}

type fsWriteExecutor struct{}

func (e *fsWriteExecutor) Execute(_ context.Context, input map[string]interface{}) (map[string]interface{}, error) {
	path, _ := input["path"].(string)
	content, _ := input["content"].(string)
	safe, err := safePath(path)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(safe), 0755); err != nil {
		return nil, fmt.Errorf("filesystem-write: mkdir: %w", err)
	}
	if err := os.WriteFile(safe, []byte(content), 0644); err != nil {
		return nil, fmt.Errorf("filesystem-write: %w", err)
	}
	return map[string]interface{}{
		"success": true,
		"path":    safe,
		"size":    len(content),
	}, nil
}

// ── filesystem-list ───────────────────────────────────────────────────────────

func fsListSpec() tools.Spec {
	return tools.Spec{
		Name:        "filesystem-list",
		Description: "List the contents of a directory on the filesystem.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]interface{}{
					"type":        "string",
					"description": "Absolute path to the directory to list",
				},
			},
			"required": []string{"path"},
		},
	}
}

type fsListExecutor struct{}

func (e *fsListExecutor) Execute(_ context.Context, input map[string]interface{}) (map[string]interface{}, error) {
	path, _ := input["path"].(string)
	safe, err := safePath(path)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(safe)
	if err != nil {
		return nil, fmt.Errorf("filesystem-list: %w", err)
	}
	items := make([]map[string]interface{}, 0, len(entries))
	for _, entry := range entries {
		info, _ := entry.Info()
		item := map[string]interface{}{
			"name":  entry.Name(),
			"isDir": entry.IsDir(),
		}
		if info != nil {
			item["size"] = info.Size()
			item["modTime"] = info.ModTime().String()
		}
		items = append(items, item)
	}
	return map[string]interface{}{
		"entries": items,
		"count":   len(items),
	}, nil
}
