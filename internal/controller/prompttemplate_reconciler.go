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
	"bytes"
	"context"
	"fmt"
	"text/template"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	agentv1alpha1 "github.com/nickmerrett/agent-operator/api/v1alpha1"
)

// PromptTemplateReconciler reconciles PromptTemplate resources.
//
// +kubebuilder:rbac:groups=agent.runtime.io,resources=prompttemplates,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=agent.runtime.io,resources=prompttemplates/status,verbs=get;update;patch
type PromptTemplateReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

func (r *PromptTemplateReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var pt agentv1alpha1.PromptTemplate
	if err := r.Get(ctx, req.NamespacedName, &pt); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Validate the template parses as valid Go template syntax.
	if _, err := template.New("validate").Parse(pt.Spec.Template); err != nil {
		return r.setNotReady(ctx, &pt, "InvalidTemplate",
			fmt.Sprintf("template syntax error: %v", err))
	}

	// Validate required variables have no default (they must be supplied at render time).
	for _, v := range pt.Spec.Variables {
		if v.Required && v.Default != "" {
			return r.setNotReady(ctx, &pt, "InvalidVariable",
				fmt.Sprintf("variable %q is required but also has a default value — remove the default or mark as optional", v.Name))
		}
	}

	logger.Info("PromptTemplate ready", "name", pt.Name)
	return r.setReady(ctx, &pt)
}

func (r *PromptTemplateReconciler) setReady(ctx context.Context, pt *agentv1alpha1.PromptTemplate) (ctrl.Result, error) {
	pt.Status.Phase = "Ready"
	meta.SetStatusCondition(&pt.Status.Conditions, metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionTrue,
		Reason:             "TemplateValid",
		Message:            "PromptTemplate parsed and validated successfully",
		ObservedGeneration: pt.Generation,
	})
	return ctrl.Result{}, r.Status().Update(ctx, pt)
}

func (r *PromptTemplateReconciler) setNotReady(ctx context.Context, pt *agentv1alpha1.PromptTemplate, reason, message string) (ctrl.Result, error) {
	pt.Status.Phase = "NotReady"
	meta.SetStatusCondition(&pt.Status.Conditions, metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionFalse,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: pt.Generation,
	})
	_ = r.Status().Update(ctx, pt)
	return ctrl.Result{}, fmt.Errorf("%s: %s", reason, message)
}

func (r *PromptTemplateReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&agentv1alpha1.PromptTemplate{}).
		Complete(r)
}
