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

// ClusterPolicyReconciler reconciles ClusterPolicy resources.
//
// +kubebuilder:rbac:groups=agent.runtime.io,resources=clusterpolicies,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=agent.runtime.io,resources=clusterpolicies/status,verbs=get;update;patch
type ClusterPolicyReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

func (r *ClusterPolicyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var policy agentv1alpha1.ClusterPolicy
	if err := r.Get(ctx, req.NamespacedName, &policy); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Validate: ceilings must be positive.
	if policy.Spec.Run.MaxTokensPerRun < 1000 && policy.Spec.Run.MaxTokensPerRun != 0 {
		return r.setNotReady(ctx, &policy, "InvalidRunConfig",
			"maxTokensPerRun must be at least 1000")
	}

	// Ensure the cloud metadata endpoint is always in the blocked list.
	hasMetadata := false
	for _, d := range policy.Spec.Egress.BlockedDomains {
		if d == "169.254.169.254" {
			hasMetadata = true
			break
		}
	}
	if !hasMetadata {
		policy.Spec.Egress.BlockedDomains = append(policy.Spec.Egress.BlockedDomains, "169.254.169.254")
		if err := r.Update(ctx, &policy); err != nil {
			return ctrl.Result{}, err
		}
	}

	logger.Info("ClusterPolicy ready", "name", policy.Name)
	return r.setReady(ctx, &policy)
}

func (r *ClusterPolicyReconciler) setReady(ctx context.Context, p *agentv1alpha1.ClusterPolicy) (ctrl.Result, error) {
	p.Status.Phase = "Ready"
	meta.SetStatusCondition(&p.Status.Conditions, metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionTrue,
		Reason:             "Validated",
		Message:            "ClusterPolicy validated successfully",
		ObservedGeneration: p.Generation,
	})
	return ctrl.Result{}, r.Status().Update(ctx, p)
}

func (r *ClusterPolicyReconciler) setNotReady(ctx context.Context, p *agentv1alpha1.ClusterPolicy, reason, message string) (ctrl.Result, error) {
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

func (r *ClusterPolicyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&agentv1alpha1.ClusterPolicy{}).
		Complete(r)
}
