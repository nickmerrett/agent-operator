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

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	agentv1alpha1 "github.com/nickmerrett/agent-operator/api/v1alpha1"
)

// AgentReconciler reconciles Agent resources.
// It validates all dependency references and sets the Agent's Ready condition.
//
// +kubebuilder:rbac:groups=agent.runtime.io,resources=agents,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=agent.runtime.io,resources=agents/status,verbs=get;update;patch
type AgentReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

func (r *AgentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var agent agentv1alpha1.Agent
	if err := r.Get(ctx, req.NamespacedName, &agent); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	ns := agent.Namespace
	conditions := make([]metav1.Condition, 0)
	allReady := true

	// Check LLMProvider.
	var llmProvider agentv1alpha1.LLMProvider
	if err := r.Get(ctx, types.NamespacedName{Name: agent.Spec.LLMProvider, Namespace: ns}, &llmProvider); err != nil {
		conditions = append(conditions, notReadyCondition("LLMProviderReady", fmt.Sprintf("LLMProvider %q not found: %v", agent.Spec.LLMProvider, err)))
		allReady = false
	} else if llmProvider.Status.Phase != "Ready" {
		conditions = append(conditions, notReadyCondition("LLMProviderReady", fmt.Sprintf("LLMProvider %q is not Ready (phase: %s)", agent.Spec.LLMProvider, llmProvider.Status.Phase)))
		allReady = false
	} else {
		conditions = append(conditions, readyCondition("LLMProviderReady", fmt.Sprintf("LLMProvider %q is Ready", agent.Spec.LLMProvider)))
	}

	// Check MemoryProvider.
	var memProvider agentv1alpha1.MemoryProvider
	if err := r.Get(ctx, types.NamespacedName{Name: agent.Spec.MemoryProvider, Namespace: ns}, &memProvider); err != nil {
		conditions = append(conditions, notReadyCondition("MemoryProviderReady", fmt.Sprintf("MemoryProvider %q not found: %v", agent.Spec.MemoryProvider, err)))
		allReady = false
	} else if memProvider.Status.Phase != "Ready" {
		conditions = append(conditions, notReadyCondition("MemoryProviderReady", fmt.Sprintf("MemoryProvider %q is not Ready (phase: %s)", agent.Spec.MemoryProvider, memProvider.Status.Phase)))
		allReady = false
	} else {
		conditions = append(conditions, readyCondition("MemoryProviderReady", fmt.Sprintf("MemoryProvider %q is Ready", agent.Spec.MemoryProvider)))
	}

	// Check each Tool.
	for _, toolName := range agent.Spec.Tools {
		var tool agentv1alpha1.Tool
		if err := r.Get(ctx, types.NamespacedName{Name: toolName, Namespace: ns}, &tool); err != nil {
			conditions = append(conditions, notReadyCondition("ToolReady/"+toolName, fmt.Sprintf("Tool %q not found: %v", toolName, err)))
			allReady = false
		} else if tool.Status.Phase != "Ready" {
			conditions = append(conditions, notReadyCondition("ToolReady/"+toolName, fmt.Sprintf("Tool %q is not Ready", toolName)))
			allReady = false
		} else {
			conditions = append(conditions, readyCondition("ToolReady/"+toolName, fmt.Sprintf("Tool %q is Ready", toolName)))
		}
	}

	// Check each MCPServer.
	for _, mcpName := range agent.Spec.MCPServers {
		var mcpServer agentv1alpha1.MCPServer
		if err := r.Get(ctx, types.NamespacedName{Name: mcpName, Namespace: ns}, &mcpServer); err != nil {
			conditions = append(conditions, notReadyCondition("MCPServerReady/"+mcpName, fmt.Sprintf("MCPServer %q not found: %v", mcpName, err)))
			allReady = false
		} else if mcpServer.Status.Phase != "Ready" {
			conditions = append(conditions, notReadyCondition("MCPServerReady/"+mcpName, fmt.Sprintf("MCPServer %q is not Ready", mcpName)))
			allReady = false
		} else {
			conditions = append(conditions, readyCondition("MCPServerReady/"+mcpName, fmt.Sprintf("MCPServer %q is Ready", mcpName)))
		}
	}

	if allReady {
		agent.Status.Phase = agentv1alpha1.AgentPhaseReady
		logger.Info("Agent ready", "name", agent.Name)
	} else {
		agent.Status.Phase = agentv1alpha1.AgentPhaseNotReady
	}

	// Replace all conditions.
	agent.Status.Conditions = nil
	for _, c := range conditions {
		c.ObservedGeneration = agent.Generation
		meta.SetStatusCondition(&agent.Status.Conditions, c)
	}

	return ctrl.Result{}, r.Status().Update(ctx, &agent)
}

func (r *AgentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// Re-reconcile Agent when any of its dependency types change.
	mapToAgents := handler.MapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
		var agentList agentv1alpha1.AgentList
		if err := mgr.GetClient().List(ctx, &agentList, client.InNamespace(obj.GetNamespace())); err != nil {
			return nil
		}
		var requests []reconcile.Request
		for _, a := range agentList.Items {
			requests = append(requests, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: a.Name, Namespace: a.Namespace},
			})
		}
		return requests
	})

	return ctrl.NewControllerManagedBy(mgr).
		For(&agentv1alpha1.Agent{}).
		Watches(&agentv1alpha1.LLMProvider{}, handler.EnqueueRequestsFromMapFunc(mapToAgents)).
		Watches(&agentv1alpha1.MemoryProvider{}, handler.EnqueueRequestsFromMapFunc(mapToAgents)).
		Watches(&agentv1alpha1.Tool{}, handler.EnqueueRequestsFromMapFunc(mapToAgents)).
		Watches(&agentv1alpha1.MCPServer{}, handler.EnqueueRequestsFromMapFunc(mapToAgents)).
		Complete(r)
}

func readyCondition(condType, message string) metav1.Condition {
	return metav1.Condition{
		Type:    condType,
		Status:  metav1.ConditionTrue,
		Reason:  "Ready",
		Message: message,
	}
}

func notReadyCondition(condType, message string) metav1.Condition {
	return metav1.Condition{
		Type:    condType,
		Status:  metav1.ConditionFalse,
		Reason:  "NotReady",
		Message: message,
	}
}
