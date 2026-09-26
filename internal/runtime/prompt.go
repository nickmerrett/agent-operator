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

// Package runtime — prompt.go
// Composes the full system prompt from all layers:
//
//  1. ClusterPolicy.preamble      (platform admin, injected first)
//  2. AgentPolicy.prompt.preamble (namespace admin)
//  3. Agent system prompt         (inline or rendered PromptTemplate)
//  4. AgentPolicy.safetyRules     (namespace admin)
//  5. ClusterPolicy.safetyRules   (platform admin, injected last)
//  6. Episodic memory summary     (runtime, from Redis)
//  7. AgentPolicy.epilogue        (namespace admin, final in system turn)
package runtime

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"text/template"

	agentv1alpha1 "github.com/nickmerrett/agent-operator/api/v1alpha1"
	"github.com/nickmerrett/agent-operator/internal/memory"
)

// PromptComposerConfig holds everything needed to build the full system prompt.
type PromptComposerConfig struct {
	Agent         agentv1alpha1.AgentSpec
	PromptTmpl    *agentv1alpha1.PromptTemplate // nil if Agent uses inline systemPrompt
	AgentPolicy   *agentv1alpha1.AgentPolicy    // nil if no policy in namespace
	ClusterPolicy *agentv1alpha1.ClusterPolicy  // nil if no cluster policy
	MemoryStore   memory.Store
	AgentName     string
}

// ComposeSystemPrompt builds the full layered system prompt for a run.
func ComposeSystemPrompt(ctx context.Context, cfg PromptComposerConfig) (string, error) {
	var sb strings.Builder

	// ── Layer 1: ClusterPolicy preamble ──────────────────────────────────────
	if cfg.ClusterPolicy != nil && cfg.ClusterPolicy.Spec.Prompt.Preamble != "" {
		sb.WriteString(cfg.ClusterPolicy.Spec.Prompt.Preamble)
		sb.WriteString("\n\n")
	}

	// ── Layer 2: AgentPolicy preamble ─────────────────────────────────────────
	if cfg.AgentPolicy != nil && cfg.AgentPolicy.Spec.Prompt.Preamble != "" {
		sb.WriteString(cfg.AgentPolicy.Spec.Prompt.Preamble)
		sb.WriteString("\n\n")
	}

	// ── Layer 3: Agent system prompt (inline or rendered template) ────────────
	agentPrompt, err := resolveAgentPrompt(cfg)
	if err != nil {
		return "", fmt.Errorf("resolve agent prompt: %w", err)
	}
	if agentPrompt != "" {
		sb.WriteString(agentPrompt)
		sb.WriteString("\n\n")
	}

	// ── Layer 4: AgentPolicy safety rules ────────────────────────────────────
	if cfg.AgentPolicy != nil && len(cfg.AgentPolicy.Spec.Prompt.SafetyRules) > 0 {
		sb.WriteString("## Safety Rules\n")
		for _, rule := range cfg.AgentPolicy.Spec.Prompt.SafetyRules {
			sb.WriteString(fmt.Sprintf("- %s\n", rule))
		}
		sb.WriteString("\n")
	}

	// ── Layer 5: ClusterPolicy safety rules ───────────────────────────────────
	if cfg.ClusterPolicy != nil && len(cfg.ClusterPolicy.Spec.Prompt.SafetyRules) > 0 {
		sb.WriteString("## Platform Rules\n")
		for _, rule := range cfg.ClusterPolicy.Spec.Prompt.SafetyRules {
			sb.WriteString(fmt.Sprintf("- %s\n", rule))
		}
		sb.WriteString("\n")
	}

	// ── Layer 6: Episodic memory summary ─────────────────────────────────────
	if cfg.MemoryStore != nil {
		episodes, err := cfg.MemoryStore.GetEpisodes(ctx, cfg.AgentName, 5)
		if err == nil && len(episodes) > 0 {
			sb.WriteString("## Relevant past interactions\n")
			for _, ep := range episodes {
				sb.WriteString(fmt.Sprintf("- [%s] %s\n", ep.Timestamp.Format("2006-01-02 15:04"), ep.Summary))
			}
			sb.WriteString("\n")
		}
	}

	// ── Layer 7: AgentPolicy epilogue ─────────────────────────────────────────
	if cfg.AgentPolicy != nil && cfg.AgentPolicy.Spec.Prompt.Epilogue != "" {
		sb.WriteString(cfg.AgentPolicy.Spec.Prompt.Epilogue)
		sb.WriteString("\n")
	}

	return strings.TrimSpace(sb.String()), nil
}

// resolveAgentPrompt returns the agent's system prompt — either the inline
// SystemPrompt string, or the rendered PromptTemplate.
func resolveAgentPrompt(cfg PromptComposerConfig) (string, error) {
	// Inline prompt takes precedence.
	if cfg.Agent.SystemPrompt != "" {
		return cfg.Agent.SystemPrompt, nil
	}

	// No template ref either — return empty (ClusterPolicy preamble is still injected).
	if cfg.PromptTmpl == nil {
		return "", nil
	}

	// Build template variables: defaults first, then agent-supplied vars.
	vars := make(map[string]string)
	for _, v := range cfg.PromptTmpl.Spec.Variables {
		if v.Default != "" {
			vars[v.Name] = v.Default
		}
	}
	for k, v := range cfg.Agent.SystemPromptVars {
		vars[k] = v
	}

	// Validate required variables are present.
	for _, v := range cfg.PromptTmpl.Spec.Variables {
		if v.Required {
			if val, ok := vars[v.Name]; !ok || val == "" {
				return "", fmt.Errorf("required prompt variable %q not supplied", v.Name)
			}
		}
	}

	// Render the template.
	tmpl, err := template.New(cfg.PromptTmpl.Name).Parse(cfg.PromptTmpl.Spec.Template)
	if err != nil {
		return "", fmt.Errorf("parse prompt template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, vars); err != nil {
		return "", fmt.Errorf("render prompt template: %w", err)
	}

	return buf.String(), nil
}
