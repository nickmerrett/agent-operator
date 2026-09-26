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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// AgentPolicyPrompt defines prompt-layer controls for the namespace.
type AgentPolicyPrompt struct {
	// Preamble is injected at the top of the system prompt for every run
	// in this namespace, before the Agent's own system prompt.
	// Off by default (empty string).
	// +optional
	Preamble string `json:"preamble,omitempty"`

	// SafetyRules is a list of safety instructions appended to the system prompt.
	// Off by default (empty list).
	// +optional
	SafetyRules []string `json:"safetyRules,omitempty"`

	// Epilogue is appended after the Agent's system prompt and safety rules.
	// Off by default (empty string).
	// +optional
	Epilogue string `json:"epilogue,omitempty"`
}

// AgentPolicyRun defines token and loop controls for the namespace.
type AgentPolicyRun struct {
	// MaxTokensPerRun is the hard token ceiling per AgentRun.
	// Run is gracefully stopped when this is reached.
	// Defaults to 50000.
	// +kubebuilder:default=50000
	// +kubebuilder:validation:Minimum=1000
	// +optional
	MaxTokensPerRun int64 `json:"maxTokensPerRun,omitempty"`

	// WarningThresholdPercent is the percentage of MaxTokensPerRun at which the
	// agent is instructed to wrap up its answer gracefully.
	// Defaults to 80. Set to 0 to disable.
	// +kubebuilder:default=80
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=100
	// +optional
	WarningThresholdPercent int32 `json:"warningThresholdPercent,omitempty"`

	// MaxIterations is the platform ceiling on reasoning loop iterations.
	// Overrides Agent.spec.maxIterations if lower.
	// Defaults to 50.
	// +kubebuilder:default=50
	// +kubebuilder:validation:Minimum=1
	// +optional
	MaxIterations int32 `json:"maxIterations,omitempty"`

	// MaxToolCallsPerRun is the hard ceiling on total tool calls in one run.
	// Defaults to 100.
	// +kubebuilder:default=100
	// +kubebuilder:validation:Minimum=1
	// +optional
	MaxToolCallsPerRun int32 `json:"maxToolCallsPerRun,omitempty"`

	// QuotaPerHour is the maximum tokens consumed across all runs in this namespace
	// per rolling hour. 0 disables quota enforcement (default).
	// +kubebuilder:default=0
	// +optional
	QuotaPerHour int64 `json:"quotaPerHour,omitempty"`

	// QuotaPerDay is the maximum tokens consumed across all runs in this namespace
	// per rolling 24-hour window. 0 disables quota enforcement (default).
	// +kubebuilder:default=0
	// +optional
	QuotaPerDay int64 `json:"quotaPerDay,omitempty"`

	// MaxCallsPerTool limits how many times a single tool can be called in one run.
	// 0 disables this limit (default).
	// +kubebuilder:default=0
	// +optional
	MaxCallsPerTool int32 `json:"maxCallsPerTool,omitempty"`

	// MinIterationDelayMs is the minimum millisecond delay between LLM calls.
	// Useful for rate limiting against provider APIs. 0 disables (default).
	// +kubebuilder:default=0
	// +optional
	MinIterationDelayMs int32 `json:"minIterationDelayMs,omitempty"`

	// LoopDetectionEnabled enables detection of repeated identical tool calls.
	// Off by default.
	// +kubebuilder:default=false
	// +optional
	LoopDetectionEnabled bool `json:"loopDetectionEnabled,omitempty"`

	// LoopDetectionMaxIdenticalCalls is the number of identical tool+args calls
	// before the loop is considered stuck. Only used when LoopDetectionEnabled=true.
	// +kubebuilder:default=3
	// +optional
	LoopDetectionMaxIdenticalCalls int32 `json:"loopDetectionMaxIdenticalCalls,omitempty"`

	// OnLimitExceeded defines what happens when token or loop limits are hit.
	// GracefulStop (default) injects a wrap-up instruction and waits for a final answer.
	// HardStop terminates immediately.
	// +kubebuilder:validation:Enum=GracefulStop;HardStop
	// +kubebuilder:default=GracefulStop
	// +optional
	OnLimitExceeded string `json:"onLimitExceeded,omitempty"`
}

// AgentPolicyToolRule defines access control for a single tool.
type AgentPolicyToolRule struct {
	// Tool is the tool name this rule applies to.
	// +kubebuilder:validation:Required
	Tool string `json:"tool"`

	// Allow controls whether the tool is permitted.
	// +kubebuilder:validation:Required
	Allow bool `json:"allow"`

	// Reason is a human-readable explanation shown to the agent when the tool is blocked.
	// +optional
	Reason string `json:"reason,omitempty"`
}

// AgentPolicyTools defines tool access controls for the namespace.
type AgentPolicyTools struct {
	// DefaultPolicy controls whether unlisted tools are allowed or denied.
	// Allow (default) permits all tools not explicitly blocked.
	// +kubebuilder:validation:Enum=Allow;Deny
	// +kubebuilder:default=Allow
	// +optional
	DefaultPolicy string `json:"defaultPolicy,omitempty"`

	// Rules is the list of per-tool access rules.
	// Off by default (empty list — all tools allowed).
	// +optional
	Rules []AgentPolicyToolRule `json:"rules,omitempty"`
}

// AgentPolicyEgress defines outbound network access controls.
type AgentPolicyEgress struct {
	// AllowedDomains is the list of domain patterns the http-request tool may call.
	// Empty list means all domains are allowed (default).
	// Supports wildcards: "*.ibm.com"
	// +optional
	AllowedDomains []string `json:"allowedDomains,omitempty"`

	// BlockedDomains is the list of domain patterns always blocked.
	// The cloud metadata endpoint is always blocked regardless of this list.
	// +optional
	BlockedDomains []string `json:"blockedDomains,omitempty"`

	// RequireTLS enforces HTTPS for all outbound http-request calls.
	// Off by default.
	// +kubebuilder:default=false
	// +optional
	RequireTLS bool `json:"requireTLS,omitempty"`

	// AuditLog enables logging of all outbound HTTP calls to Redis.
	// Off by default.
	// +kubebuilder:default=false
	// +optional
	AuditLog bool `json:"auditLog,omitempty"`
}

// AgentPolicyContent defines content filtering controls.
type AgentPolicyContent struct {
	// PIIRedaction enables scanning and redacting PII from agent outputs.
	// Off by default.
	// +kubebuilder:default=false
	// +optional
	PIIRedaction bool `json:"piiRedaction,omitempty"`

	// PromptInjectionDetection enables scanning inputs for prompt injection patterns.
	// Off by default.
	// +kubebuilder:default=false
	// +optional
	PromptInjectionDetection bool `json:"promptInjectionDetection,omitempty"`

	// BlockedInputPatterns is a list of regex patterns that cause the run to be
	// rejected before reaching the LLM. Off by default (empty list).
	// +optional
	BlockedInputPatterns []string `json:"blockedInputPatterns,omitempty"`
}

// AgentPolicyAudit defines audit logging controls.
type AgentPolicyAudit struct {
	// ConversationLog enables shipping full conversation history to a webhook.
	// Off by default.
	// +kubebuilder:default=false
	// +optional
	ConversationLog bool `json:"conversationLog,omitempty"`

	// ToolCallLog enables logging every tool call and result.
	// Off by default.
	// +kubebuilder:default=false
	// +optional
	ToolCallLog bool `json:"toolCallLog,omitempty"`

	// UsageLog enables per-run token usage logging for chargeback.
	// Off by default.
	// +kubebuilder:default=false
	// +optional
	UsageLog bool `json:"usageLog,omitempty"`

	// WebhookURL is the endpoint that receives audit events.
	// Required when any log option is enabled.
	// +optional
	WebhookURL string `json:"webhookURL,omitempty"`
}

// AgentPolicySpec defines the desired state of AgentPolicy.
type AgentPolicySpec struct {
	// Prompt defines namespace-level prompt injection controls.
	// All fields off by default.
	// +optional
	Prompt AgentPolicyPrompt `json:"prompt,omitempty"`

	// Run defines token and loop controls.
	// Sensible defaults applied — see field descriptions.
	// +optional
	Run AgentPolicyRun `json:"run,omitempty"`

	// Tools defines tool access controls.
	// Default: all tools allowed.
	// +optional
	Tools AgentPolicyTools `json:"tools,omitempty"`

	// Egress defines outbound network access controls.
	// Default: all domains allowed except cloud metadata endpoint.
	// +optional
	Egress AgentPolicyEgress `json:"egress,omitempty"`

	// Content defines content filtering controls.
	// All fields off by default.
	// +optional
	Content AgentPolicyContent `json:"content,omitempty"`

	// Audit defines audit logging controls.
	// All fields off by default.
	// +optional
	Audit AgentPolicyAudit `json:"audit,omitempty"`
}

// AgentPolicyStatus defines the observed state of AgentPolicy.
type AgentPolicyStatus struct {
	// Phase summarises the policy's validity.
	// +optional
	Phase string `json:"phase,omitempty"`

	// Conditions holds detailed status conditions.
	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=ap
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="MaxTokens",type=integer,JSONPath=`.spec.run.maxTokensPerRun`
// +kubebuilder:printcolumn:name="MaxIter",type=integer,JSONPath=`.spec.run.maxIterations`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// AgentPolicy defines all namespace-level guardrails for agent runs.
// A single AgentPolicy named "default" applies to all agents in the namespace.
// All controls are off by default — opt in to what you need.
type AgentPolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AgentPolicySpec   `json:"spec,omitempty"`
	Status AgentPolicyStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// AgentPolicyList contains a list of AgentPolicy.
type AgentPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AgentPolicy `json:"items"`
}

func init() {
	SchemeBuilder.Register(&AgentPolicy{}, &AgentPolicyList{})
}
