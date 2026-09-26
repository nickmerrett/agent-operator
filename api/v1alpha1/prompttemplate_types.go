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

// PromptTemplateVariable defines a variable that can be substituted into the template.
type PromptTemplateVariable struct {
	// Name is the variable name used in the template (e.g. "Domain" for {{.Domain}}).
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// Default is the value used when the variable is not supplied at render time.
	// +optional
	Default string `json:"default,omitempty"`

	// Required indicates the template cannot render without this variable.
	// +kubebuilder:default=false
	// +optional
	Required bool `json:"required,omitempty"`

	// Description is a human-readable explanation of what this variable controls.
	// +optional
	Description string `json:"description,omitempty"`
}

// PromptTemplateSpec defines the desired state of PromptTemplate.
type PromptTemplateSpec struct {
	// Description is a human-readable summary of this template's purpose.
	// +optional
	Description string `json:"description,omitempty"`

	// Template is the prompt text, optionally using Go template syntax for variable
	// substitution (e.g. {{.Domain}}, {{if .Focus}}...{{end}}).
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Template string `json:"template"`

	// Variables declares the variables available for substitution in the template.
	// +optional
	Variables []PromptTemplateVariable `json:"variables,omitempty"`
}

// PromptTemplateStatus defines the observed state of PromptTemplate.
type PromptTemplateStatus struct {
	// Phase summarises the template's validity.
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
// +kubebuilder:resource:scope=Namespaced,shortName=pt
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// PromptTemplate defines a reusable, parameterised prompt fragment.
// Referenced by Agent.spec.systemPromptRef to compose the agent's system prompt.
type PromptTemplate struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   PromptTemplateSpec   `json:"spec,omitempty"`
	Status PromptTemplateStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// PromptTemplateList contains a list of PromptTemplate.
type PromptTemplateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PromptTemplate `json:"items"`
}

func init() {
	SchemeBuilder.Register(&PromptTemplate{}, &PromptTemplateList{})
}
