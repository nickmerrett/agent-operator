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

// Package runtime — policy.go
// Resolves the effective policy for a run by merging ClusterPolicy (hard ceilings)
// with namespace AgentPolicy (per-team configuration), applying the principle:
// namespace values are honoured unless they exceed the cluster ceiling.
package runtime

import (
	agentv1alpha1 "github.com/nickmerrett/agent-operator/api/v1alpha1"
)

// EffectivePolicy is the fully resolved policy applied to a single AgentRun.
// It is computed once at startup from ClusterPolicy + AgentPolicy and passed
// into the runtime loop — the loop never reads CRs directly.
type EffectivePolicy struct {
	// Token controls
	MaxTokensPerRun         int64
	WarningThresholdPercent int32
	QuotaPerHour            int64
	QuotaPerDay             int64

	// Loop controls
	MaxIterations                  int32
	MaxToolCallsPerRun             int32
	MaxCallsPerTool                int32
	MinIterationDelayMs            int32
	LoopDetectionEnabled           bool
	LoopDetectionMaxIdenticalCalls int32
	OnLimitExceeded                string // GracefulStop | HardStop

	// Tool access
	ToolDefaultPolicy string // Allow | Deny
	ToolRules         []agentv1alpha1.AgentPolicyToolRule

	// Egress
	EgressAllowedDomains []string
	EgressBlockedDomains []string
	EgressRequireTLS     bool
	EgressAuditLog       bool

	// Content
	PIIRedaction             bool
	PromptInjectionDetection bool
	BlockedInputPatterns     []string

	// Audit
	AuditConversationLog bool
	AuditToolCallLog     bool
	AuditUsageLog        bool
	AuditWebhookURL      string
}

// platformDefaults are the built-in fallback values used when no policy CRs exist.
var platformDefaults = EffectivePolicy{
	MaxTokensPerRun:                50000,
	WarningThresholdPercent:        80,
	MaxIterations:                  25,
	MaxToolCallsPerRun:             100,
	OnLimitExceeded:                "GracefulStop",
	ToolDefaultPolicy:              "Allow",
	LoopDetectionEnabled:           false,
	LoopDetectionMaxIdenticalCalls: 3,
	// Always block cloud metadata endpoint.
	EgressBlockedDomains: []string{"169.254.169.254"},
}

// ResolvePolicy computes the EffectivePolicy for a run.
// clusterPolicy and agentPolicy may be nil if no CRs exist.
// agentMaxIterations is the value from Agent.spec.maxIterations.
func ResolvePolicy(
	clusterPolicy *agentv1alpha1.ClusterPolicy,
	agentPolicy *agentv1alpha1.AgentPolicy,
	agentMaxIterations int32,
) EffectivePolicy {
	ep := platformDefaults

	// ── Apply AgentPolicy (namespace level) ───────────────────────────────────
	if agentPolicy != nil {
		r := agentPolicy.Spec.Run

		if r.MaxTokensPerRun > 0 {
			ep.MaxTokensPerRun = r.MaxTokensPerRun
		}
		if r.WarningThresholdPercent > 0 {
			ep.WarningThresholdPercent = r.WarningThresholdPercent
		}
		if r.MaxIterations > 0 {
			ep.MaxIterations = r.MaxIterations
		}
		if r.MaxToolCallsPerRun > 0 {
			ep.MaxToolCallsPerRun = r.MaxToolCallsPerRun
		}
		if r.QuotaPerHour > 0 {
			ep.QuotaPerHour = r.QuotaPerHour
		}
		if r.QuotaPerDay > 0 {
			ep.QuotaPerDay = r.QuotaPerDay
		}
		if r.MaxCallsPerTool > 0 {
			ep.MaxCallsPerTool = r.MaxCallsPerTool
		}
		if r.MinIterationDelayMs > 0 {
			ep.MinIterationDelayMs = r.MinIterationDelayMs
		}
		ep.LoopDetectionEnabled = r.LoopDetectionEnabled
		if r.LoopDetectionMaxIdenticalCalls > 0 {
			ep.LoopDetectionMaxIdenticalCalls = r.LoopDetectionMaxIdenticalCalls
		}
		if r.OnLimitExceeded != "" {
			ep.OnLimitExceeded = r.OnLimitExceeded
		}

		// Tool policy
		if agentPolicy.Spec.Tools.DefaultPolicy != "" {
			ep.ToolDefaultPolicy = agentPolicy.Spec.Tools.DefaultPolicy
		}
		ep.ToolRules = agentPolicy.Spec.Tools.Rules

		// Egress
		ep.EgressAllowedDomains = agentPolicy.Spec.Egress.AllowedDomains
		ep.EgressBlockedDomains = mergeDomains(platformDefaults.EgressBlockedDomains, agentPolicy.Spec.Egress.BlockedDomains)
		ep.EgressRequireTLS = agentPolicy.Spec.Egress.RequireTLS
		ep.EgressAuditLog = agentPolicy.Spec.Egress.AuditLog

		// Content
		ep.PIIRedaction = agentPolicy.Spec.Content.PIIRedaction
		ep.PromptInjectionDetection = agentPolicy.Spec.Content.PromptInjectionDetection
		ep.BlockedInputPatterns = agentPolicy.Spec.Content.BlockedInputPatterns

		// Audit
		ep.AuditConversationLog = agentPolicy.Spec.Audit.ConversationLog
		ep.AuditToolCallLog = agentPolicy.Spec.Audit.ToolCallLog
		ep.AuditUsageLog = agentPolicy.Spec.Audit.UsageLog
		ep.AuditWebhookURL = agentPolicy.Spec.Audit.WebhookURL
	}

	// ── Apply ClusterPolicy ceilings (always wins) ────────────────────────────
	if clusterPolicy != nil {
		c := clusterPolicy.Spec.Run

		// Cluster ceilings cap namespace values — namespace can be lower, never higher.
		if c.MaxTokensPerRun > 0 && ep.MaxTokensPerRun > c.MaxTokensPerRun {
			ep.MaxTokensPerRun = c.MaxTokensPerRun
		}
		if c.MaxIterations > 0 && ep.MaxIterations > c.MaxIterations {
			ep.MaxIterations = c.MaxIterations
		}
		if c.MaxToolCallsPerRun > 0 && ep.MaxToolCallsPerRun > c.MaxToolCallsPerRun {
			ep.MaxToolCallsPerRun = c.MaxToolCallsPerRun
		}

		// Merge cluster blocked domains (always enforced on top of namespace list).
		ep.EgressBlockedDomains = mergeDomains(ep.EgressBlockedDomains, clusterPolicy.Spec.Egress.BlockedDomains)
	}

	// ── Apply Agent CR maxIterations (take the lower of agent and policy) ─────
	if agentMaxIterations > 0 && agentMaxIterations < ep.MaxIterations {
		ep.MaxIterations = agentMaxIterations
	}

	return ep
}

// mergeDomains merges two domain lists, deduplicating entries.
func mergeDomains(a, b []string) []string {
	seen := make(map[string]bool, len(a)+len(b))
	result := make([]string, 0, len(a)+len(b))
	for _, d := range append(a, b...) {
		if !seen[d] {
			seen[d] = true
			result = append(result, d)
		}
	}
	return result
}
