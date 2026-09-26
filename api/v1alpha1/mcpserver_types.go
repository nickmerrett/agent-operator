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

// MCPTransport defines the supported MCP server transports.
// +kubebuilder:validation:Enum=http;sse
type MCPTransport string

const (
	MCPTransportHTTP MCPTransport = "http"
	MCPTransportSSE  MCPTransport = "sse"
)

// MCPToolDefinition represents a tool discovered from an MCP server.
type MCPToolDefinition struct {
	// Name is the tool's identifier as returned by the MCP server.
	Name string `json:"name"`

	// Description is the tool's human-readable description.
	Description string `json:"description"`

	// InputSchema is the JSON Schema for the tool's input parameters.
	// +optional
	InputSchema *apiextensionsv1.JSON `json:"inputSchema,omitempty"`
}

// MCPServerSpec defines the desired state of MCPServer.
type MCPServerSpec struct {
	// Transport defines the protocol used to communicate with the MCP server.
	// +kubebuilder:validation:Required
	Transport MCPTransport `json:"transport"`

	// Endpoint is the URL of the MCP server.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Endpoint string `json:"endpoint"`

	// CredentialsSecret is the optional name of a Kubernetes Secret containing
	// auth credentials (e.g. a Bearer token under key "token").
	// +optional
	CredentialsSecret string `json:"credentialsSecret,omitempty"`

	// AutoDiscoverTools controls whether the operator introspects the server
	// and populates status.discoveredTools on reconciliation.
	// +kubebuilder:default=true
	// +optional
	AutoDiscoverTools bool `json:"autoDiscoverTools,omitempty"`
}

// MCPServerStatus defines the observed state of MCPServer.
type MCPServerStatus struct {
	// Phase summarises the server's connectivity status.
	// +optional
	Phase string `json:"phase,omitempty"`

	// DiscoveredTools is the list of tools reported by the MCP server.
	// Populated when spec.autoDiscoverTools is true.
	// +optional
	DiscoveredTools []MCPToolDefinition `json:"discoveredTools,omitempty"`

	// LastDiscoveryTime is when the tool list was last refreshed.
	// +optional
	LastDiscoveryTime *metav1.Time `json:"lastDiscoveryTime,omitempty"`

	// Conditions holds detailed status conditions.
	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=mcps
// +kubebuilder:printcolumn:name="Transport",type=string,JSONPath=`.spec.transport`
// +kubebuilder:printcolumn:name="Endpoint",type=string,JSONPath=`.spec.endpoint`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Tools",type=integer,JSONPath=`.status.discoveredTools`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// MCPServer points to an external MCP-compatible tool server.
// The operator discovers the server's tool list and makes them available to agents.
type MCPServer struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   MCPServerSpec   `json:"spec,omitempty"`
	Status MCPServerStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// MCPServerList contains a list of MCPServer.
type MCPServerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []MCPServer `json:"items"`
}

func init() {
	SchemeBuilder.Register(&MCPServer{}, &MCPServerList{})
}
