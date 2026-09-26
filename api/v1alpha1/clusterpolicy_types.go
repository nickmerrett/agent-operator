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

// ClusterPolicyPrompt defines platform-level prompt controls applied to every run
// in every namespace. Cannot be overridden by namespace-level AgentPolicy.
type ClusterPolicyPrompt struct {
	// Preamble is injected at the very top of every agent's system prompt,
	// before any namespace-level or agent-level prompt content.
	// +optional
	Preamble string `json:"preamble,omitempty"`

	// SafetyRules are injected as hard constraints into every agent's system prompt.
	// +optional
	SafetyRules []string `json:"safetyRules,omitempty"`
}

// ClusterPolicyRun defines platform-level hard ceilings on token and loop usage.
// Namespace AgentPolicy values are capped at these ceilings — they can be lower
// but never higher.
type ClusterPolicyRun struct {
	// MaxTokensPerRun is the absolute platform ceiling on tokens per run.
	// Namespace AgentPolicy.run.maxTokensPerRun cannot exceed this.
	// Defaults to 100000.
	// +kubebuilder:default=100000
	// +kubebuilder:validation:Minimum=1000
	// +optional
	MaxTokensPerRun int64 `json:"maxTokensPerRun,omitempty"`

	// MaxIterations is the absolute platform ceiling on loop iterations.
	// +kubebuilder:default=50
	// +kubebuilder:validation:Minimum=1
	// +optional
	MaxIterations int32 `json:"maxIterations,omitempty"`

	// MaxToolCallsPerRun is the absolute platform ceiling on tool calls per run.
	// +kubebuilder:default=200
	// +kubebuilder:validation:Minimum=1
	// +optional
	MaxToolCallsPerRun int32 `json:"maxToolCallsPerRun,omitempty"`
}

// ClusterPolicyEgress defines platform-level egress blocks that apply to every
// namespace and cannot be overridden.
type ClusterPolicyEgress struct {
	// BlockedDomains is the platform-wide list of always-blocked domain patterns.
	// These are merged with namespace-level AgentPolicy.egress.blockedDomains.
	// The cloud metadata endpoint (169.254.169.254) is always included.
	// +optional
	BlockedDomains []string `json:"blockedDomains,omitempty"`
}

// ClusterPolicySpec defines the desired state of ClusterPolicy.
type ClusterPolicySpec struct {
	// Prompt defines platform-level prompt controls injected into every run.
	// +optional
	Prompt ClusterPolicyPrompt `json:"prompt,omitempty"`

	// Run defines platform-level hard ceilings on token and loop usage.
	// +optional
	Run ClusterPolicyRun `json:"run,omitempty"`

	// Egress defines platform-level egress blocks merged with namespace policy.
	// +optional
	Egress ClusterPolicyEgress `json:"egress,omitempty"`
}

// ClusterPolicyStatus defines the observed state of ClusterPolicy.
type ClusterPolicyStatus struct {
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
// +kubebuilder:resource:scope=Cluster,shortName=cp
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="MaxTokens",type=integer,JSONPath=`.spec.run.maxTokensPerRun`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// ClusterPolicy defines platform-level guardrails applied to every agent run
// in every namespace. Only cluster admins can create or modify this resource.
// A single ClusterPolicy named "default" is active at a time.
type ClusterPolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ClusterPolicySpec   `json:"spec,omitempty"`
	Status ClusterPolicyStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ClusterPolicyList contains a list of ClusterPolicy.
type ClusterPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ClusterPolicy `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ClusterPolicy{}, &ClusterPolicyList{})
}
