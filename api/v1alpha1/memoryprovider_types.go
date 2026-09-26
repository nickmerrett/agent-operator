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

// MemoryProviderType defines the supported memory backend types.
// +kubebuilder:validation:Enum=redis
type MemoryProviderType string

const (
	MemoryProviderTypeRedis MemoryProviderType = "redis"
)

// MemoryProviderSpec defines the desired state of MemoryProvider.
type MemoryProviderSpec struct {
	// Type is the memory backend type. Currently only "redis" is supported.
	// +kubebuilder:validation:Required
	Type MemoryProviderType `json:"type"`

	// ConnectionSecret is the name of the Kubernetes Secret containing Redis connection
	// details. Expected keys: host, port, password (password is optional).
	// +kubebuilder:validation:Required
	ConnectionSecret string `json:"connectionSecret"`

	// WorkingMemoryTTL is the TTL in seconds for per-run working memory (message history).
	// Defaults to 3600 (1 hour).
	// +kubebuilder:default=3600
	// +kubebuilder:validation:Minimum=60
	// +optional
	WorkingMemoryTTL int64 `json:"workingMemoryTTL,omitempty"`

	// EpisodicMemoryEnabled controls whether conversation summaries are persisted across runs.
	// +kubebuilder:default=false
	// +optional
	EpisodicMemoryEnabled bool `json:"episodicMemoryEnabled,omitempty"`

	// EpisodicMemoryTTL is the TTL in seconds for episodic memory entries.
	// Defaults to 604800 (7 days). Only used when EpisodicMemoryEnabled is true.
	// +kubebuilder:default=604800
	// +kubebuilder:validation:Minimum=3600
	// +optional
	EpisodicMemoryTTL int64 `json:"episodicMemoryTTL,omitempty"`
}

// MemoryProviderStatus defines the observed state of MemoryProvider.
type MemoryProviderStatus struct {
	// Phase summarises the provider's readiness.
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
// +kubebuilder:resource:scope=Namespaced,shortName=memp
// +kubebuilder:printcolumn:name="Type",type=string,JSONPath=`.spec.type`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// MemoryProvider defines a memory backend that agents use for working and episodic memory.
type MemoryProvider struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   MemoryProviderSpec   `json:"spec,omitempty"`
	Status MemoryProviderStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// MemoryProviderList contains a list of MemoryProvider.
type MemoryProviderList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []MemoryProvider `json:"items"`
}

func init() {
	SchemeBuilder.Register(&MemoryProvider{}, &MemoryProviderList{})
}
