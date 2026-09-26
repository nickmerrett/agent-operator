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
	"encoding/base64"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	agentv1alpha1 "github.com/nickmerrett/agent-operator/api/v1alpha1"
)

const (
	// podRetentionAfterCompletion is how long to keep the pod around after it finishes.
	podRetentionAfterCompletion = 10 * time.Minute
	// agentRunLabel is the label key used to identify AgentRun pods.
	agentRunLabel = "agent-operator/run"
	// agentLabel identifies which Agent spawned this pod.
	agentLabel = "agent-operator/agent"
	// managedLabel marks all pods as agent-operator managed (for policy engine targeting).
	managedLabel = "agent-operator/managed"
)

// AgentRunReconciler reconciles AgentRun resources.
//
// +kubebuilder:rbac:groups=agent.runtime.io,resources=agentruns,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=agent.runtime.io,resources=agentruns/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;create;delete
// +kubebuilder:rbac:groups="",resources=pods/log,verbs=get
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
type AgentRunReconciler struct {
	client.Client
	Scheme       *runtime.Scheme
	RuntimeImage string // agent-runtime container image
}

func (r *AgentRunReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var agentRun agentv1alpha1.AgentRun
	if err := r.Get(ctx, req.NamespacedName, &agentRun); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Terminal states — nothing more to do.
	if agentRun.Status.Phase == agentv1alpha1.AgentRunPhaseSucceeded ||
		agentRun.Status.Phase == agentv1alpha1.AgentRunPhaseFailed ||
		agentRun.Status.Phase == agentv1alpha1.AgentRunPhaseTimedout {
		return ctrl.Result{}, nil
	}

	// Fetch the referenced Agent.
	var agent agentv1alpha1.Agent
	if err := r.Get(ctx, types.NamespacedName{Name: agentRun.Spec.AgentRef, Namespace: agentRun.Namespace}, &agent); err != nil {
		return r.setFailed(ctx, &agentRun, "AgentNotFound",
			fmt.Sprintf("Agent %q not found: %v", agentRun.Spec.AgentRef, err))
	}
	if agent.Status.Phase != agentv1alpha1.AgentPhaseReady {
		return r.setFailed(ctx, &agentRun, "AgentNotReady",
			fmt.Sprintf("Agent %q is not Ready (phase: %s)", agentRun.Spec.AgentRef, agent.Status.Phase))
	}

	// Fetch LLMProvider and MemoryProvider to get their credential secret names.
	var llmProvider agentv1alpha1.LLMProvider
	if err := r.Get(ctx, types.NamespacedName{Name: agent.Spec.LLMProvider, Namespace: agentRun.Namespace}, &llmProvider); err != nil {
		return r.setFailed(ctx, &agentRun, "LLMProviderNotFound", err.Error())
	}
	var memProvider agentv1alpha1.MemoryProvider
	if err := r.Get(ctx, types.NamespacedName{Name: agent.Spec.MemoryProvider, Namespace: agentRun.Namespace}, &memProvider); err != nil {
		return r.setFailed(ctx, &agentRun, "MemoryProviderNotFound", err.Error())
	}

	podName := agentRun.Name + "-pod"
	resultKey := fmt.Sprintf("agentrun:%s:result", agentRun.Name)

	// ── Check if the pod already exists ─────────────────────────────────────
	var pod corev1.Pod
	podErr := r.Get(ctx, types.NamespacedName{Name: podName, Namespace: agentRun.Namespace}, &pod)

	if apierrors.IsNotFound(podErr) {
		// Pod does not exist yet — create it.
		newPod := r.buildPod(agentRun, agent, llmProvider, memProvider, podName, resultKey)
		if err := r.Create(ctx, newPod); err != nil {
			return ctrl.Result{}, fmt.Errorf("create pod: %w", err)
		}
		logger.Info("Created AgentRun pod", "pod", podName)

		agentRun.Status.Phase = agentv1alpha1.AgentRunPhaseRunning
		agentRun.Status.PodName = podName
		agentRun.Status.RedisResultKey = resultKey
		return ctrl.Result{RequeueAfter: 5 * time.Second}, r.Status().Update(ctx, &agentRun)
	}

	if podErr != nil {
		return ctrl.Result{}, fmt.Errorf("get pod: %w", podErr)
	}

	// ── Pod exists — sync phase ──────────────────────────────────────────────
	switch pod.Status.Phase {
	case corev1.PodRunning:
		if agentRun.Status.StartTime == nil {
			now := metav1.NewTime(time.Now())
			agentRun.Status.StartTime = &now
			_ = r.Status().Update(ctx, &agentRun)
		}
		// Check timeout.
		if agentRun.Status.StartTime != nil {
			timeout := time.Duration(agent.Spec.TimeoutSeconds) * time.Second
			if time.Since(agentRun.Status.StartTime.Time) > timeout {
				_ = r.Delete(ctx, &pod)
				return r.setTimedout(ctx, &agentRun)
			}
		}
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil

	case corev1.PodSucceeded:
		now := metav1.NewTime(time.Now())
		agentRun.Status.CompletionTime = &now
		agentRun.Status.Phase = agentv1alpha1.AgentRunPhaseSucceeded
		meta.SetStatusCondition(&agentRun.Status.Conditions, metav1.Condition{
			Type:               "Completed",
			Status:             metav1.ConditionTrue,
			Reason:             "PodSucceeded",
			Message:            fmt.Sprintf("AgentRun pod %q completed successfully. Result in Redis key: %s", podName, resultKey),
			ObservedGeneration: agentRun.Generation,
		})
		if err := r.Status().Update(ctx, &agentRun); err != nil {
			return ctrl.Result{}, err
		}
		// Schedule pod cleanup after retention window.
		return ctrl.Result{RequeueAfter: podRetentionAfterCompletion}, nil

	case corev1.PodFailed:
		message := "pod failed"
		if len(pod.Status.ContainerStatuses) > 0 {
			cs := pod.Status.ContainerStatuses[0]
			if cs.State.Terminated != nil {
				message = fmt.Sprintf("container exited with code %d: %s",
					cs.State.Terminated.ExitCode, cs.State.Terminated.Message)
			}
		}
		return r.setFailed(ctx, &agentRun, "PodFailed", message)
	}

	// Pending or unknown — recheck.
	return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
}

func (r *AgentRunReconciler) buildPod(
	agentRun agentv1alpha1.AgentRun,
	agent agentv1alpha1.Agent,
	llmProvider agentv1alpha1.LLMProvider,
	memProvider agentv1alpha1.MemoryProvider,
	podName, resultKey string,
) *corev1.Pod {
	promptB64 := base64.StdEncoding.EncodeToString([]byte(agentRun.Spec.Input.Prompt))

	ownerRef := metav1.OwnerReference{
		APIVersion: agentRun.APIVersion,
		Kind:       agentRun.Kind,
		Name:       agentRun.Name,
		UID:        agentRun.UID,
	}

	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: agentRun.Namespace,
			Labels: map[string]string{
				agentRunLabel: agentRun.Name,
				agentLabel:    agent.Name,
				managedLabel:  "true",
			},
			OwnerReferences: []metav1.OwnerReference{ownerRef},
		},
		Spec: corev1.PodSpec{
			RestartPolicy:      corev1.RestartPolicyNever,
			ServiceAccountName: "agent-runtime",
			Containers: []corev1.Container{
				{
					Name:  "agent-runtime",
					Image: r.RuntimeImage,
					Env: []corev1.EnvVar{
						{Name: "AGENT_NAME", Value: agent.Name},
						{Name: "AGENT_NAMESPACE", Value: agentRun.Namespace},
						{Name: "AGENTRUN_NAME", Value: agentRun.Name},
						{Name: "INPUT_PROMPT", Value: promptB64},
						{Name: "CALLBACK_URL", Value: agentRun.Spec.CallbackURL},
						{Name: "REDIS_RESULT_KEY", Value: resultKey},
						{Name: "LLM_PROVIDER_NAME", Value: agent.Spec.LLMProvider},
						{Name: "MEMORY_PROVIDER_NAME", Value: agent.Spec.MemoryProvider},
					},
					EnvFrom: []corev1.EnvFromSource{
						{
							SecretRef: &corev1.SecretEnvSource{
								LocalObjectReference: corev1.LocalObjectReference{Name: llmProvider.Spec.CredentialsSecret},
							},
						},
						{
							SecretRef: &corev1.SecretEnvSource{
								LocalObjectReference: corev1.LocalObjectReference{Name: memProvider.Spec.ConnectionSecret},
							},
						},
					},
				},
			},
		},
	}
}

func (r *AgentRunReconciler) setFailed(ctx context.Context, ar *agentv1alpha1.AgentRun, reason, message string) (ctrl.Result, error) {
	now := metav1.NewTime(time.Now())
	ar.Status.Phase = agentv1alpha1.AgentRunPhaseFailed
	ar.Status.CompletionTime = &now
	meta.SetStatusCondition(&ar.Status.Conditions, metav1.Condition{
		Type:               "Failed",
		Status:             metav1.ConditionTrue,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: ar.Generation,
	})
	return ctrl.Result{}, r.Status().Update(ctx, ar)
}

func (r *AgentRunReconciler) setTimedout(ctx context.Context, ar *agentv1alpha1.AgentRun) (ctrl.Result, error) {
	now := metav1.NewTime(time.Now())
	ar.Status.Phase = agentv1alpha1.AgentRunPhaseTimedout
	ar.Status.CompletionTime = &now
	meta.SetStatusCondition(&ar.Status.Conditions, metav1.Condition{
		Type:               "Timedout",
		Status:             metav1.ConditionTrue,
		Reason:             "DeadlineExceeded",
		Message:            "AgentRun exceeded its configured timeoutSeconds",
		ObservedGeneration: ar.Generation,
	})
	return ctrl.Result{}, r.Status().Update(ctx, ar)
}

func (r *AgentRunReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&agentv1alpha1.AgentRun{}).
		Owns(&corev1.Pod{}).
		Complete(r)
}
