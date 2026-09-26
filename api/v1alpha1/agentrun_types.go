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

// AgentRunPhase represents the lifecycle phase of an AgentRun.
// +kubebuilder:validation:Enum=Pending;Running;Succeeded;Failed;Timedout
type AgentRunPhase string

const (
	AgentRunPhasePending   AgentRunPhase = "Pending"
	AgentRunPhaseRunning   AgentRunPhase = "Running"
	AgentRunPhaseSucceeded AgentRunPhase = "Succeeded"
	AgentRunPhaseFailed    AgentRunPhase = "Failed"
	AgentRunPhaseTimedout  AgentRunPhase = "Timedout"
)

// AgentRunInput holds the trigger input for an AgentRun.
type AgentRunInput struct {
	// Prompt is the user's natural-language instruction to the agent.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Prompt string `json:"prompt"`

	// Context is an optional map of key-value pairs injected into the agent's context.
	// +optional
	Context map[string]string `json:"context,omitempty"`
}

// AgentRunSpec defines the desired state of AgentRun.
type AgentRunSpec struct {
	// AgentRef is the name of the Agent CR (in the same namespace) to run.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	AgentRef string `json:"agentRef"`

	// Input holds the trigger prompt and optional context.
	// +kubebuilder:validation:Required
	Input AgentRunInput `json:"input"`

	// CallbackURL is an optional HTTP endpoint. The agent-runtime binary POSTs the
	// result directly to this URL on completion. The operator is not involved.
	// +optional
	CallbackURL string `json:"callbackURL,omitempty"`
}

// AgentRunStatus defines the observed state of AgentRun.
type AgentRunStatus struct {
	// Phase is the current lifecycle phase of this run.
	// +optional
	Phase AgentRunPhase `json:"phase,omitempty"`

	// PodName is the name of the ephemeral pod running the agent-runtime binary.
	// +optional
	PodName string `json:"podName,omitempty"`

	// RedisResultKey is the Redis key where the agent-runtime writes its result.
	// Callers can read the result directly from Redis using this key.
	// Format: agentrun:{agentrun-name}:result
	// +optional
	RedisResultKey string `json:"redisResultKey,omitempty"`

	// StartTime is when the AgentRun pod started.
	// +optional
	StartTime *metav1.Time `json:"startTime,omitempty"`

	// CompletionTime is when the AgentRun pod completed (succeeded, failed, or timedout).
	// +optional
	CompletionTime *metav1.Time `json:"completionTime,omitempty"`

	// Conditions holds detailed status conditions.
	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=ar
// +kubebuilder:printcolumn:name="Agent",type=string,JSONPath=`.spec.agentRef`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Pod",type=string,JSONPath=`.status.podName`
// +kubebuilder:printcolumn:name="Start",type=date,JSONPath=`.status.startTime`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// AgentRun represents a single ephemeral execution of an Agent.
// Created by the HTTP gateway when a trigger request arrives.
// The operator spawns a pod running the agent-runtime binary which drives
// the ReAct reasoning loop and writes its result directly to Redis.
type AgentRun struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AgentRunSpec   `json:"spec,omitempty"`
	Status AgentRunStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// AgentRunList contains a list of AgentRun.
type AgentRunList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AgentRun `json:"items"`
}

func init() {
	SchemeBuilder.Register(&AgentRun{}, &AgentRunList{})
}
