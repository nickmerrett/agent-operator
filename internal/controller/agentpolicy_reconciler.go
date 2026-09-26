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
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	agentv1alpha1 "github.com/nickmerrett/agent-operator/api/v1alpha1"
)

// AgentPolicyReconciler reconciles AgentPolicy resources.
//
// +kubebuilder:rbac:groups=agent.runtime.io,resources=agentpolicies,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=agent.runtime.io,resources=agentpolicies/status,verbs=get;update;patch
type AgentPolicyReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

func (r *AgentPolicyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var policy agentv1alpha1.AgentPolicy
	if err := r.Get(ctx, req.NamespacedName, &policy); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Validate: if any audit log option is enabled, webhookURL must be set.
	audit := policy.Spec.Audit
	if (audit.ConversationLog || audit.ToolCallLog || audit.UsageLog) && audit.WebhookURL == "" {
		return r.setNotReady(ctx, &policy, "InvalidAuditConfig",
			"audit logging is enabled but webhookURL is not set")
	}

	// Validate: if egress requireTLS is true and allowedDomains is non-empty,
	// all domains should be https-compatible (warn only — not a hard error).

	// Validate token limits are within reasonable bounds.
	if policy.Spec.Run.MaxTokensPerRun > 0 && policy.Spec.Run.WarningThresholdPercent > 100 {
		return r.setNotReady(ctx, &policy, "InvalidRunConfig",
			"warningThresholdPercent must be between 0 and 100")
	}

	logger.Info("AgentPolicy ready", "name", policy.Name, "namespace", policy.Namespace)
	return r.setReady(ctx, &policy)
}

func (r *AgentPolicyReconciler) setReady(ctx context.Context, p *agentv1alpha1.AgentPolicy) (ctrl.Result, error) {
	p.Status.Phase = "Ready"
	meta.SetStatusCondition(&p.Status.Conditions, metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionTrue,
		Reason:             "Validated",
		Message:            "AgentPolicy validated successfully",
		ObservedGeneration: p.Generation,
	})
	return ctrl.Result{}, r.Status().Update(ctx, p)
}

func (r *AgentPolicyReconciler) setNotReady(ctx context.Context, p *agentv1alpha1.AgentPolicy, reason, message string) (ctrl.Result, error) {
	p.Status.Phase = "NotReady"
	meta.SetStatusCondition(&p.Status.Conditions, metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionFalse,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: p.Generation,
	})
	_ = r.Status().Update(ctx, p)
	return ctrl.Result{}, fmt.Errorf("%s: %s", reason, message)
}

func (r *AgentPolicyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&agentv1alpha1.AgentPolicy{}).
		Complete(r)
}
