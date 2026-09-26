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

package builtin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer/yaml"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/nickmerrett/agent-operator/internal/tools"
)

// kubectlGetSpec is the kubectl-get tool.
func kubectlGetSpec() tools.Spec {
	return tools.Spec{
		Name:        "kubectl-get",
		Description: "Get a Kubernetes resource by kind, name, and namespace. Returns the resource as JSON.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"kind": map[string]interface{}{
					"type":        "string",
					"description": "Kubernetes resource kind (e.g. Pod, Deployment, ConfigMap)",
				},
				"name": map[string]interface{}{
					"type":        "string",
					"description": "Resource name",
				},
				"namespace": map[string]interface{}{
					"type":        "string",
					"description": "Kubernetes namespace",
				},
				"apiVersion": map[string]interface{}{
					"type":        "string",
					"description": "API version (e.g. apps/v1). Defaults to v1 for core resources.",
				},
			},
			"required": []string{"kind", "name", "namespace"},
		},
	}
}

type kubectlGetExecutor struct {
	Client client.Client
}

func (e *kubectlGetExecutor) Execute(ctx context.Context, input map[string]interface{}) (map[string]interface{}, error) {
	if e.Client == nil {
		return nil, fmt.Errorf("kubectl-get: kubernetes client not available")
	}
	kind, _ := input["kind"].(string)
	name, _ := input["name"].(string)
	namespace, _ := input["namespace"].(string)
	apiVersion, _ := input["apiVersion"].(string)
	if apiVersion == "" {
		apiVersion = "v1"
	}

	gvk := parseGVK(apiVersion, kind)
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(gvk)

	if err := e.Client.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, obj); err != nil {
		return nil, fmt.Errorf("kubectl-get: %w", err)
	}

	data, err := json.Marshal(obj.Object)
	if err != nil {
		return nil, fmt.Errorf("kubectl-get: marshal: %w", err)
	}

	return map[string]interface{}{
		"resource": string(data),
		"kind":     kind,
		"name":     name,
		"namespace": namespace,
	}, nil
}

// kubectlApplySpec is the kubectl-apply tool.
func kubectlApplySpec() tools.Spec {
	return tools.Spec{
		Name:        "kubectl-apply",
		Description: "Apply a Kubernetes manifest (YAML or JSON string) to the cluster using server-side apply.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"manifest": map[string]interface{}{
					"type":        "string",
					"description": "YAML or JSON Kubernetes manifest to apply",
				},
				"namespace": map[string]interface{}{
					"type":        "string",
					"description": "Target namespace (overrides namespace in the manifest if set)",
				},
			},
			"required": []string{"manifest"},
		},
	}
}

type kubectlApplyExecutor struct {
	Client     client.Client
	RESTMapper meta.RESTMapper
}

func (e *kubectlApplyExecutor) Execute(ctx context.Context, input map[string]interface{}) (map[string]interface{}, error) {
	if e.Client == nil {
		return nil, fmt.Errorf("kubectl-apply: kubernetes client not available")
	}
	manifest, _ := input["manifest"].(string)
	if manifest == "" {
		return nil, fmt.Errorf("kubectl-apply: manifest is required")
	}

	// Decode the manifest into an unstructured object.
	decoder := yaml.NewDecodingSerializer(unstructured.UnstructuredJSONScheme)
	obj := &unstructured.Unstructured{}
	_, _, err := decoder.Decode([]byte(manifest), nil, obj)
	if err != nil {
		// Try JSON if YAML fails.
		if jsonErr := json.NewDecoder(bytes.NewBufferString(manifest)).Decode(&obj.Object); jsonErr != nil {
			return nil, fmt.Errorf("kubectl-apply: decode manifest: %w (yaml: %v, json: %v)", err, err, jsonErr)
		}
	}

	if ns, ok := input["namespace"].(string); ok && ns != "" {
		obj.SetNamespace(ns)
	}

	force := true
	if err := e.Client.Patch(ctx, obj, client.Apply, &client.PatchOptions{
		FieldManager: "agent-operator",
		Force:        &force,
	}); err != nil {
		return nil, fmt.Errorf("kubectl-apply: patch: %w", err)
	}

	return map[string]interface{}{
		"success":   true,
		"name":      obj.GetName(),
		"namespace": obj.GetNamespace(),
		"kind":      obj.GetKind(),
	}, nil
}

func parseGVK(apiVersion, kind string) schema.GroupVersionKind {
	parts := strings.SplitN(apiVersion, "/", 2)
	if len(parts) == 1 {
		return schema.GroupVersionKind{Group: "", Version: parts[0], Kind: kind}
	}
	return schema.GroupVersionKind{Group: parts[0], Version: parts[1], Kind: kind}
}
