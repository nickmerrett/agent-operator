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

// Package gateway implements the HTTP gateway for the agent operator.
// It provides two externally-exposed entry points:
//
//  1. POST /v1/agents/{namespace}/{agent-name}/run
//     Triggers a new AgentRun by creating an AgentRun CR.
//
//  2. POST /v1/runs/{namespace}/{run-id}/message
//     Sends a follow-up message to an in-flight AgentRun via Redis pub/sub.
//
// Additional read endpoints:
//
//	GET /v1/runs/{namespace}/{run-id}         - returns AgentRun status
//	GET /v1/runs/{namespace}/{run-id}/logs    - streams pod logs
package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"

	agentv1alpha1 "github.com/nickmerrett/agent-operator/api/v1alpha1"
	"github.com/nickmerrett/agent-operator/internal/memory"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Server is the HTTP gateway server.
type Server struct {
	addr      string
	k8s       client.Client
	clientset kubernetes.Interface
	// memStores caches memory stores per namespace (keyed by namespace).
	// In practice the gateway resolves the store lazily on each interact call.
}

// New creates a new gateway Server.
func New(addr string, k8sClient client.Client, clientset kubernetes.Interface) *Server {
	return &Server{
		addr:      addr,
		k8s:       k8sClient,
		clientset: clientset,
	}
}

// Start begins listening. This is a blocking call; run it in a goroutine.
func (s *Server) Start(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/agents/", s.routeAgents)
	mux.HandleFunc("/v1/runs/", s.routeRuns)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	srv := &http.Server{
		Addr:         s.addr,
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
	}()

	return srv.ListenAndServe()
}

// ── Routing ───────────────────────────────────────────────────────────────────

// routeAgents handles: POST /v1/agents/{namespace}/{agent-name}/run
func (s *Server) routeAgents(w http.ResponseWriter, r *http.Request) {
	// Expected path: /v1/agents/{namespace}/{agent-name}/run
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/v1/agents/"), "/")
	if len(parts) != 3 || parts[2] != "run" {
		http.NotFound(w, r)
		return
	}
	ns, agentName := parts[0], parts[1]

	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if err := s.authMiddleware(r, ns); err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	s.handleTrigger(w, r, ns, agentName)
}

// routeRuns handles:
//
//	POST /v1/runs/{namespace}/{run-id}/message
//	GET  /v1/runs/{namespace}/{run-id}
//	GET  /v1/runs/{namespace}/{run-id}/logs
func (s *Server) routeRuns(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/v1/runs/"), "/")
	if len(parts) < 2 {
		http.NotFound(w, r)
		return
	}
	ns, runID := parts[0], parts[1]

	if err := s.authMiddleware(r, ns); err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	if len(parts) == 3 && parts[2] == "message" && r.Method == http.MethodPost {
		s.handleInteract(w, r, ns, runID)
		return
	}
	if len(parts) == 3 && parts[2] == "logs" && r.Method == http.MethodGet {
		s.handleLogs(w, r, ns, runID)
		return
	}
	if len(parts) == 2 && r.Method == http.MethodGet {
		s.handleStatus(w, r, ns, runID)
		return
	}
	http.NotFound(w, r)
}

// ── Handlers ──────────────────────────────────────────────────────────────────

type triggerRequest struct {
	Prompt      string            `json:"prompt"`
	Context     map[string]string `json:"context"`
	CallbackURL string            `json:"callbackURL"`
}

type triggerResponse struct {
	RunID          string `json:"runId"`
	StatusURL      string `json:"statusURL"`
	RedisResultKey string `json:"resultKey"`
}

func (s *Server) handleTrigger(w http.ResponseWriter, r *http.Request, ns, agentName string) {
	var req triggerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid request body: %v", err), http.StatusBadRequest)
		return
	}
	if req.Prompt == "" {
		http.Error(w, "prompt is required", http.StatusBadRequest)
		return
	}

	ctx := r.Context()

	// Verify the Agent CR exists and is Ready.
	var agent agentv1alpha1.Agent
	if err := s.k8s.Get(ctx, types.NamespacedName{Name: agentName, Namespace: ns}, &agent); err != nil {
		http.Error(w, fmt.Sprintf("agent %q not found", agentName), http.StatusNotFound)
		return
	}
	if agent.Status.Phase != agentv1alpha1.AgentPhaseReady {
		http.Error(w, fmt.Sprintf("agent %q is not Ready (phase: %s)", agentName, agent.Status.Phase), http.StatusConflict)
		return
	}

	runID := agentName + "-" + randomSuffix()
	resultKey := fmt.Sprintf("agentrun:%s:result", runID)

	agentRun := &agentv1alpha1.AgentRun{
		ObjectMeta: metav1.ObjectMeta{
			Name:      runID,
			Namespace: ns,
		},
		Spec: agentv1alpha1.AgentRunSpec{
			AgentRef: agentName,
			Input: agentv1alpha1.AgentRunInput{
				Prompt:  req.Prompt,
				Context: req.Context,
			},
			CallbackURL: req.CallbackURL,
		},
	}

	if err := s.k8s.Create(ctx, agentRun); err != nil {
		http.Error(w, fmt.Sprintf("failed to create AgentRun: %v", err), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusAccepted, triggerResponse{
		RunID:          runID,
		StatusURL:      fmt.Sprintf("/v1/runs/%s/%s", ns, runID),
		RedisResultKey: resultKey,
	})
}

type interactRequest struct {
	Message string `json:"message"`
}

func (s *Server) handleInteract(w http.ResponseWriter, r *http.Request, ns, runID string) {
	var req interactRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid request body: %v", err), http.StatusBadRequest)
		return
	}
	if req.Message == "" {
		http.Error(w, "message is required", http.StatusBadRequest)
		return
	}

	ctx := r.Context()

	// Verify the AgentRun is Running.
	var agentRun agentv1alpha1.AgentRun
	if err := s.k8s.Get(ctx, types.NamespacedName{Name: runID, Namespace: ns}, &agentRun); err != nil {
		http.Error(w, fmt.Sprintf("agentrun %q not found", runID), http.StatusNotFound)
		return
	}
	if agentRun.Status.Phase != agentv1alpha1.AgentRunPhaseRunning {
		http.Error(w, fmt.Sprintf("agentrun %q is not Running (phase: %s)", runID, agentRun.Status.Phase), http.StatusConflict)
		return
	}

	// Resolve the MemoryProvider for this agent to get Redis connection details.
	var agent agentv1alpha1.Agent
	if err := s.k8s.Get(ctx, types.NamespacedName{Name: agentRun.Spec.AgentRef, Namespace: ns}, &agent); err != nil {
		http.Error(w, "agent not found", http.StatusInternalServerError)
		return
	}
	var memProvider agentv1alpha1.MemoryProvider
	if err := s.k8s.Get(ctx, types.NamespacedName{Name: agent.Spec.MemoryProvider, Namespace: ns}, &memProvider); err != nil {
		http.Error(w, "memory provider not found", http.StatusInternalServerError)
		return
	}
	var memSecret corev1.Secret
	if err := s.k8s.Get(ctx, types.NamespacedName{Name: memProvider.Spec.ConnectionSecret, Namespace: ns}, &memSecret); err != nil {
		http.Error(w, "memory secret not found", http.StatusInternalServerError)
		return
	}
	creds := secretDataToMap(memSecret.Data)
	memStore, err := memory.NewStoreFromSpec(memProvider.Spec, creds)
	if err != nil {
		http.Error(w, "failed to connect to memory backend", http.StatusInternalServerError)
		return
	}

	if err := memStore.PublishInbound(ctx, runID, req.Message); err != nil {
		http.Error(w, fmt.Sprintf("failed to publish message: %v", err), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusAccepted)
}

type statusResponse struct {
	RunID          string `json:"runId"`
	Phase          string `json:"phase"`
	PodName        string `json:"podName,omitempty"`
	RedisResultKey string `json:"resultKey,omitempty"`
	StartTime      string `json:"startTime,omitempty"`
	CompletionTime string `json:"completionTime,omitempty"`
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request, ns, runID string) {
	ctx := r.Context()
	var agentRun agentv1alpha1.AgentRun
	if err := s.k8s.Get(ctx, types.NamespacedName{Name: runID, Namespace: ns}, &agentRun); err != nil {
		http.Error(w, fmt.Sprintf("agentrun %q not found", runID), http.StatusNotFound)
		return
	}

	resp := statusResponse{
		RunID:          runID,
		Phase:          string(agentRun.Status.Phase),
		PodName:        agentRun.Status.PodName,
		RedisResultKey: agentRun.Status.RedisResultKey,
	}
	if agentRun.Status.StartTime != nil {
		resp.StartTime = agentRun.Status.StartTime.Format(time.RFC3339)
	}
	if agentRun.Status.CompletionTime != nil {
		resp.CompletionTime = agentRun.Status.CompletionTime.Format(time.RFC3339)
	}

	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request, ns, runID string) {
	ctx := r.Context()
	var agentRun agentv1alpha1.AgentRun
	if err := s.k8s.Get(ctx, types.NamespacedName{Name: runID, Namespace: ns}, &agentRun); err != nil {
		http.Error(w, fmt.Sprintf("agentrun %q not found", runID), http.StatusNotFound)
		return
	}
	if agentRun.Status.PodName == "" {
		http.Error(w, "no pod associated with this AgentRun yet", http.StatusNotFound)
		return
	}

	req := s.clientset.CoreV1().Pods(ns).GetLogs(agentRun.Status.PodName, &corev1.PodLogOptions{
		Container: "agent-runtime",
		Follow:    false,
	})
	stream, err := req.Stream(ctx)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to get logs: %v", err), http.StatusInternalServerError)
		return
	}
	defer stream.Close()

	w.Header().Set("Content-Type", "text/plain")
	_, _ = io.Copy(w, stream)
}

// ── Auth ──────────────────────────────────────────────────────────────────────

// authMiddleware validates the Bearer token in the Authorization header against
// the "agent-gateway-token" Secret in the target namespace.
func (s *Server) authMiddleware(r *http.Request, namespace string) error {
	authHeader := r.Header.Get("Authorization")
	if !strings.HasPrefix(authHeader, "Bearer ") {
		return fmt.Errorf("missing or malformed Authorization header")
	}
	token := strings.TrimPrefix(authHeader, "Bearer ")

	ctx := r.Context()
	var secret corev1.Secret
	if err := s.k8s.Get(ctx, types.NamespacedName{
		Name:      "agent-gateway-token",
		Namespace: namespace,
	}, &secret); err != nil {
		return fmt.Errorf("gateway token secret not found: %w", err)
	}

	storedToken := string(secret.Data["token"])
	if storedToken == "" || token != storedToken {
		return fmt.Errorf("invalid token")
	}
	return nil
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func randomSuffix() string {
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 8)
	for i := range b {
		b[i] = chars[rand.Intn(len(chars))]
	}
	return string(b)
}

func secretDataToMap(data map[string][]byte) map[string]string {
	m := make(map[string]string, len(data))
	for k, v := range data {
		m[k] = string(v)
	}
	return m
}

