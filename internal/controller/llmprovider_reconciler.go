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

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	agentv1alpha1 "github.com/nickmerrett/agent-operator/api/v1alpha1"
	"github.com/nickmerrett/agent-operator/internal/llm"
)

// LLMProviderReconciler reconciles LLMProvider resources.
//
// +kubebuilder:rbac:groups=agent.runtime.io,resources=llmproviders,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=agent.runtime.io,resources=llmproviders/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
type LLMProviderReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

func (r *LLMProviderReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var provider agentv1alpha1.LLMProvider
	if err := r.Get(ctx, req.NamespacedName, &provider); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Fetch credentials secret.
	var secret corev1.Secret
	if err := r.Get(ctx, types.NamespacedName{
		Name:      provider.Spec.CredentialsSecret,
		Namespace: provider.Namespace,
	}, &secret); err != nil {
		return r.setNotReady(ctx, &provider, "CredentialsSecretNotFound",
			fmt.Sprintf("secret %q not found: %v", provider.Spec.CredentialsSecret, err))
	}

	creds := secretToMap(secret.Data)

	// Validate credentials by building the client. We do a lightweight check —
	// just construct the client (no actual LLM call) to avoid unnecessary API costs.
	_, err := llm.NewClientFromSpec(provider.Spec, creds)
	if err != nil {
		return r.setNotReady(ctx, &provider, "InvalidConfiguration",
			fmt.Sprintf("failed to build LLM client: %v", err))
	}

	logger.Info("LLMProvider ready", "name", provider.Name)
	return r.setReady(ctx, &provider)
}

func (r *LLMProviderReconciler) setReady(ctx context.Context, p *agentv1alpha1.LLMProvider) (ctrl.Result, error) {
	p.Status.Phase = "Ready"
	meta.SetStatusCondition(&p.Status.Conditions, metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionTrue,
		Reason:             "CredentialsValid",
		Message:            "LLM provider credentials validated successfully",
		ObservedGeneration: p.Generation,
	})
	return ctrl.Result{}, r.Status().Update(ctx, p)
}

func (r *LLMProviderReconciler) setNotReady(ctx context.Context, p *agentv1alpha1.LLMProvider, reason, message string) (ctrl.Result, error) {
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

func (r *LLMProviderReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&agentv1alpha1.LLMProvider{}).
		Complete(r)
}

// secretToMap converts a secret's Data map from []byte values to string values.
func secretToMap(data map[string][]byte) map[string]string {
	m := make(map[string]string, len(data))
	for k, v := range data {
		m[k] = string(v)
	}
	return m
}
