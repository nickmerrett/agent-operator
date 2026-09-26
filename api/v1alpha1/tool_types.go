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
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// BuiltInToolName defines the set of available built-in tools.
// +kubebuilder:validation:Enum=tool-finder;http-request;filesystem-read;filesystem-write;filesystem-list;shell-exec;kubectl-get;kubectl-apply
type BuiltInToolName string

const (
	BuiltInToolFinder        BuiltInToolName = "tool-finder"
	BuiltInToolHTTPRequest   BuiltInToolName = "http-request"
	BuiltInToolFSRead        BuiltInToolName = "filesystem-read"
	BuiltInToolFSWrite       BuiltInToolName = "filesystem-write"
	BuiltInToolFSList        BuiltInToolName = "filesystem-list"
	BuiltInToolShellExec     BuiltInToolName = "shell-exec"
	BuiltInToolKubectlGet    BuiltInToolName = "kubectl-get"
	BuiltInToolKubectlApply  BuiltInToolName = "kubectl-apply"
)

// ToolSpec defines the desired state of Tool.
type ToolSpec struct {
	// Description is a human-readable description of what the tool does.
	// This is used by the tool-finder and surfaced to the LLM.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Description string `json:"description"`

	// BuiltIn references a built-in tool from the operator's catalogue.
	// Mutually exclusive with Image.
	// +optional
	BuiltIn BuiltInToolName `json:"builtIn,omitempty"`

	// Image is a container image that implements the tool interface.
	// Used for custom tools. Mutually exclusive with BuiltIn.
	// +optional
	Image string `json:"image,omitempty"`

	// InputSchema is a JSON Schema (draft-07) describing the tool's input parameters.
	// Follows the MCP tool specification.
	// +optional
	InputSchema *apiextensionsv1.JSON `json:"inputSchema,omitempty"`

	// OutputSchema is a JSON Schema (draft-07) describing the tool's output.
	// +optional
	OutputSchema *apiextensionsv1.JSON `json:"outputSchema,omitempty"`
}

// ToolStatus defines the observed state of Tool.
type ToolStatus struct {
	// Phase summarises the tool's readiness.
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
// +kubebuilder:resource:scope=Namespaced,shortName=tool
// +kubebuilder:printcolumn:name="BuiltIn",type=string,JSONPath=`.spec.builtIn`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// Tool defines a capability that an agent can invoke during its reasoning loop.
// Tools follow the MCP JSON schema specification for their input/output contracts.
type Tool struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ToolSpec   `json:"spec,omitempty"`
	Status ToolStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ToolList contains a list of Tool.
type ToolList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Tool `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Tool{}, &ToolList{})
}
