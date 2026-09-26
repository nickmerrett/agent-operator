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

// LLMProviderType defines the supported LLM backend types.
// +kubebuilder:validation:Enum=watsonx;openai;ollama;anthropic
type LLMProviderType string

const (
	LLMProviderTypeWatsonx   LLMProviderType = "watsonx"
	LLMProviderTypeOpenAI    LLMProviderType = "openai"
	LLMProviderTypeOllama    LLMProviderType = "ollama"
	LLMProviderTypeAnthropic LLMProviderType = "anthropic"
)

// LLMProviderSpec defines the desired state of LLMProvider.
type LLMProviderSpec struct {
	// Type is the LLM backend type.
	// +kubebuilder:validation:Required
	Type LLMProviderType `json:"type"`

	// Endpoint is the base URL for the LLM API.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Endpoint string `json:"endpoint"`

	// Model is the model identifier to use.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Model string `json:"model"`

	// CredentialsSecret is the name of the Kubernetes Secret containing auth credentials.
	// The secret must exist in the same namespace as this LLMProvider.
	// +kubebuilder:validation:Required
	CredentialsSecret string `json:"credentialsSecret"`

	// Parameters are optional provider-specific parameters (e.g. temperature, maxTokens).
	// All values are strings to allow embedding in environment variables.
	// +optional
	Parameters map[string]string `json:"parameters,omitempty"`
}

// LLMProviderStatus defines the observed state of LLMProvider.
type LLMProviderStatus struct {
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
// +kubebuilder:resource:scope=Namespaced,shortName=llmp
// +kubebuilder:printcolumn:name="Type",type=string,JSONPath=`.spec.type`
// +kubebuilder:printcolumn:name="Model",type=string,JSONPath=`.spec.model`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// LLMProvider defines an LLM backend that agents can use.
type LLMProvider struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   LLMProviderSpec   `json:"spec,omitempty"`
	Status LLMProviderStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// LLMProviderList contains a list of LLMProvider.
type LLMProviderList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []LLMProvider `json:"items"`
}

func init() {
	SchemeBuilder.Register(&LLMProvider{}, &LLMProviderList{})
}
