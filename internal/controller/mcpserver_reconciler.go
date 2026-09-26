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

package controller

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	agentv1alpha1 "github.com/nickmerrett/agent-operator/api/v1alpha1"
	internalmcp "github.com/nickmerrett/agent-operator/internal/mcp"
)

// MCPServerReconciler reconciles MCPServer resources.
//
// +kubebuilder:rbac:groups=agent.runtime.io,resources=mcpservers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=agent.runtime.io,resources=mcpservers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
type MCPServerReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

func (r *MCPServerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var server agentv1alpha1.MCPServer
	if err := r.Get(ctx, req.NamespacedName, &server); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	token := ""
	if server.Spec.CredentialsSecret != "" {
		var secret corev1.Secret
		if err := r.Get(ctx, types.NamespacedName{
			Name:      server.Spec.CredentialsSecret,
			Namespace: server.Namespace,
		}, &secret); err != nil {
			return r.setNotReady(ctx, &server, "CredentialsSecretNotFound",
				fmt.Sprintf("secret %q not found: %v", server.Spec.CredentialsSecret, err))
		}
		token = string(secret.Data["token"])
	}

	mcpClient, err := internalmcp.NewClient(string(server.Spec.Transport), server.Spec.Endpoint, token)
	if err != nil {
		return r.setNotReady(ctx, &server, "InvalidTransport", err.Error())
	}

	var discovered []agentv1alpha1.MCPToolDefinition

	if server.Spec.AutoDiscoverTools {
		toolDefs, err := mcpClient.ListTools(ctx)
		if err != nil {
			return r.setNotReady(ctx, &server, "DiscoveryFailed",
				fmt.Sprintf("failed to list tools: %v", err))
		}
		for _, td := range toolDefs {
			discovered = append(discovered, agentv1alpha1.MCPToolDefinition{
				Name:        td.Name,
				Description: td.Description,
			})
		}
		logger.Info("Discovered MCP tools", "server", server.Name, "count", len(discovered))
	}

	now := metav1.NewTime(time.Now())
	server.Status.DiscoveredTools = discovered
	server.Status.LastDiscoveryTime = &now
	server.Status.Phase = "Ready"
	meta.SetStatusCondition(&server.Status.Conditions, metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionTrue,
		Reason:             "Connected",
		Message:            fmt.Sprintf("Connected to MCP server, discovered %d tools", len(discovered)),
		ObservedGeneration: server.Generation,
	})

	if err := r.Status().Update(ctx, &server); err != nil {
		return ctrl.Result{}, err
	}

	// Re-reconcile every 5 minutes to pick up new tools.
	return ctrl.Result{RequeueAfter: 5 * time.Minute}, nil
}

func (r *MCPServerReconciler) setNotReady(ctx context.Context, s *agentv1alpha1.MCPServer, reason, message string) (ctrl.Result, error) {
	s.Status.Phase = "NotReady"
	meta.SetStatusCondition(&s.Status.Conditions, metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionFalse,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: s.Generation,
	})
	_ = r.Status().Update(ctx, s)
	return ctrl.Result{}, fmt.Errorf("%s: %s", reason, message)
}

func (r *MCPServerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&agentv1alpha1.MCPServer{}).
		Complete(r)
}
