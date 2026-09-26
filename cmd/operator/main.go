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

// Command operator is the agent-operator controller manager.
// It runs all CRD reconcilers and the HTTP gateway in a single process.
package main

import (
	"flag"
	"os"

	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	agentv1alpha1 "github.com/nickmerrett/agent-operator/api/v1alpha1"
	"github.com/nickmerrett/agent-operator/internal/controller"
	"github.com/nickmerrett/agent-operator/internal/gateway"
	"github.com/nickmerrett/agent-operator/internal/tools"
	"github.com/nickmerrett/agent-operator/internal/tools/builtin"
)

var (
	scheme   = k8sruntime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(agentv1alpha1.AddToScheme(scheme))
}

func main() {
	var (
		metricsAddr          string
		probeAddr            string
		gatewayAddr          string
		runtimeImage         string
		enableLeaderElection bool
	)

	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8081", "The address the metrics endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8082", "The address the probe endpoint binds to.")
	flag.StringVar(&gatewayAddr, "gateway-bind-address", ":8080", "The address the HTTP gateway binds to.")
	flag.StringVar(&runtimeImage, "runtime-image", "ghcr.io/nickmerrett/agent-runtime:latest", "The agent-runtime container image to use for AgentRun pods.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false, "Enable leader election for controller manager.")

	opts := zap.Options{Development: true}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		MetricsBindAddress:     metricsAddr,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "agent-operator.runtime.io",
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	// ── Register built-in tools ───────────────────────────────────────────────
	cat := tools.Global()
	builtin.RegisterAll(cat, mgr.GetClient())

	// ── Register reconcilers ──────────────────────────────────────────────────
	if err := (&controller.LLMProviderReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create LLMProviderReconciler")
		os.Exit(1)
	}

	if err := (&controller.MemoryProviderReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create MemoryProviderReconciler")
		os.Exit(1)
	}

	if err := (&controller.ToolReconciler{
		Client:    mgr.GetClient(),
		Scheme:    mgr.GetScheme(),
		Catalogue: cat,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create ToolReconciler")
		os.Exit(1)
	}

	if err := (&controller.MCPServerReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create MCPServerReconciler")
		os.Exit(1)
	}

	if err := (&controller.AgentReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create AgentReconciler")
		os.Exit(1)
	}

	if err := (&controller.AgentRunReconciler{
		Client:       mgr.GetClient(),
		Scheme:       mgr.GetScheme(),
		RuntimeImage: runtimeImage,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create AgentRunReconciler")
		os.Exit(1)
	}

	// ── Start HTTP gateway in background goroutine ────────────────────────────
	cfg := ctrl.GetConfigOrDie()
	clientset, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		setupLog.Error(err, "unable to create clientset for gateway")
		os.Exit(1)
	}

	gw := gateway.New(gatewayAddr, mgr.GetClient(), clientset)
	go func() {
		setupLog.Info("starting HTTP gateway", "address", gatewayAddr)
		if err := gw.Start(ctrl.SetupSignalHandler()); err != nil {
			setupLog.Error(err, "gateway exited")
		}
	}()

	// ── Health checks ─────────────────────────────────────────────────────────
	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
