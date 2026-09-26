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

// AgentPhase represents the readiness phase of an Agent.
// +kubebuilder:validation:Enum=Ready;NotReady
type AgentPhase string

const (
	AgentPhaseReady    AgentPhase = "Ready"
	AgentPhaseNotReady AgentPhase = "NotReady"
)

// AgentSpec defines the desired state of Agent.
type AgentSpec struct {
	// LLMProvider is the name of the LLMProvider CR in the same namespace.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	LLMProvider string `json:"llmProvider"`

	// MemoryProvider is the name of the MemoryProvider CR in the same namespace.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	MemoryProvider string `json:"memoryProvider"`

	// SystemPrompt is the initial system-level instruction given to the LLM.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	SystemPrompt string `json:"systemPrompt"`

	// Tools is the list of Tool CR names (in the same namespace) available to this agent.
	// +optional
	Tools []string `json:"tools,omitempty"`

	// MCPServers is the list of MCPServer CR names (in the same namespace) whose
	// discovered tools are made available to this agent.
	// +optional
	MCPServers []string `json:"mcpServers,omitempty"`

	// MaxIterations is the maximum number of reasoning loop iterations before the
	// agent terminates with a failure.
	// +kubebuilder:default=25
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=100
	// +optional
	MaxIterations int32 `json:"maxIterations,omitempty"`

	// TimeoutSeconds is the maximum wall-clock time (in seconds) an AgentRun may run.
	// +kubebuilder:default=300
	// +kubebuilder:validation:Minimum=10
	// +optional
	TimeoutSeconds int64 `json:"timeoutSeconds,omitempty"`
}

// AgentStatus defines the observed state of Agent.
type AgentStatus struct {
	// Phase is the overall readiness of the agent and all its dependencies.
	// +optional
	Phase AgentPhase `json:"phase,omitempty"`

	// Conditions holds detailed per-dependency status conditions.
	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=agent
// +kubebuilder:printcolumn:name="LLMProvider",type=string,JSONPath=`.spec.llmProvider`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// Agent defines an autonomous agent — its LLM, memory backend, system prompt, and tools.
type Agent struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AgentSpec   `json:"spec,omitempty"`
	Status AgentStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// AgentList contains a list of Agent.
type AgentList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Agent `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Agent{}, &AgentList{})
}
