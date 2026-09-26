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

// Package builtin provides implementations of all built-in tools and registers
// them in the global catalogue at init time.
package builtin

import (
	"context"

	"github.com/nickmerrett/agent-operator/internal/tools"
)

// RegisterAll registers all built-in tools into the global catalogue.
// Call this once during operator/runtime startup.
func RegisterAll(cat *tools.Catalogue) {
	cat.Register(tools.Entry{Spec: toolFinderSpec(), Executor: &toolFinderExecutor{cat: cat}})
	cat.Register(tools.Entry{Spec: httpRequestSpec(), Executor: &httpRequestExecutor{}})
	cat.Register(tools.Entry{Spec: fsReadSpec(), Executor: &fsReadExecutor{}})
	cat.Register(tools.Entry{Spec: fsWriteSpec(), Executor: &fsWriteExecutor{}})
	cat.Register(tools.Entry{Spec: fsListSpec(), Executor: &fsListExecutor{}})
	cat.Register(tools.Entry{Spec: shellExecSpec(), Executor: &shellExecExecutor{}})
	cat.Register(tools.Entry{Spec: kubectlGetSpec(), Executor: &kubectlGetExecutor{}})
	cat.Register(tools.Entry{Spec: kubectlApplySpec(), Executor: &kubectlApplyExecutor{}})
}

// toolFinderSpec is the tool-finder tool — searches the catalogue by keyword.
func toolFinderSpec() tools.Spec {
	return tools.Spec{
		Name:        "tool-finder",
		Description: "Search the available tool catalogue by keyword or capability description. Use this when you need to discover what tools are available before deciding which to use.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query": map[string]interface{}{
					"type":        "string",
					"description": "Keyword or capability description to search for",
				},
			},
			"required": []string{"query"},
		},
	}
}

type toolFinderExecutor struct {
	cat *tools.Catalogue
}

func (e *toolFinderExecutor) Execute(_ context.Context, input map[string]interface{}) (map[string]interface{}, error) {
	query, _ := input["query"].(string)
	specs := e.cat.Find(query)
	results := make([]map[string]interface{}, 0, len(specs))
	for _, s := range specs {
		results = append(results, map[string]interface{}{
			"name":        s.Name,
			"description": s.Description,
		})
	}
	return map[string]interface{}{
		"tools": results,
		"count": len(results),
	}, nil
}
