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
	"github.com/nickmerrett/agent-operator/internal/tools"
)

// ToolReconciler reconciles Tool resources.
//
// +kubebuilder:rbac:groups=agent.runtime.io,resources=tools,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=agent.runtime.io,resources=tools/status,verbs=get;update;patch
type ToolReconciler struct {
	client.Client
	Scheme    *runtime.Scheme
	Catalogue *tools.Catalogue
}

func (r *ToolReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var tool agentv1alpha1.Tool
	if err := r.Get(ctx, req.NamespacedName, &tool); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Validate: either builtIn or image must be set.
	if tool.Spec.BuiltIn == "" && tool.Spec.Image == "" {
		return r.setNotReady(ctx, &tool, "InvalidSpec", "either builtIn or image must be set")
	}
	if tool.Spec.BuiltIn != "" && tool.Spec.Image != "" {
		return r.setNotReady(ctx, &tool, "InvalidSpec", "builtIn and image are mutually exclusive")
	}

	// For built-in tools: verify the name exists in the catalogue.
	if tool.Spec.BuiltIn != "" {
		if _, ok := r.Catalogue.Get(string(tool.Spec.BuiltIn)); !ok {
			return r.setNotReady(ctx, &tool, "BuiltInNotFound",
				fmt.Sprintf("built-in tool %q not found in catalogue", tool.Spec.BuiltIn))
		}
	}

	// For custom image tools: just ensure the image reference is non-empty (already checked above).

	logger.Info("Tool ready", "name", tool.Name, "builtIn", tool.Spec.BuiltIn)
	return r.setReady(ctx, &tool)
}

func (r *ToolReconciler) setReady(ctx context.Context, t *agentv1alpha1.Tool) (ctrl.Result, error) {
	t.Status.Phase = "Ready"
	meta.SetStatusCondition(&t.Status.Conditions, metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionTrue,
		Reason:             "Validated",
		Message:            "Tool spec validated successfully",
		ObservedGeneration: t.Generation,
	})
	return ctrl.Result{}, r.Status().Update(ctx, t)
}

func (r *ToolReconciler) setNotReady(ctx context.Context, t *agentv1alpha1.Tool, reason, message string) (ctrl.Result, error) {
	t.Status.Phase = "NotReady"
	meta.SetStatusCondition(&t.Status.Conditions, metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionFalse,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: t.Generation,
	})
	_ = r.Status().Update(ctx, t)
	return ctrl.Result{}, fmt.Errorf("%s: %s", reason, message)
}

func (r *ToolReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&agentv1alpha1.Tool{}).
		Complete(r)
}
