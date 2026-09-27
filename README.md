# agent-operator

A production-grade, Kubernetes-native agentic runtime framework written in Go.

Define LLM providers, memory backends, tools, and agents as Kubernetes Custom Resources.
Trigger agents via HTTP — ephemeral pods spin up, drive a ReAct reasoning loop, write their
result to Redis, and terminate. The operator manages lifecycle. Nothing else runs permanently.

---

## Table of Contents

- [Architecture](#architecture)
- [CRD Taxonomy](#crd-taxonomy)
- [Policy System](#policy-system)
- [Built-in Tools](#built-in-tools)
- [Redis Key Schema](#redis-key-schema)
- [Quick Start — Kubernetes](#quick-start--kubernetes)
- [Quick Start — OpenShift](#quick-start--openshift)
- [agentctl CLI](#agentctl-cli)
- [Go Client SDK](#go-client-sdk)
- [Building Images](#building-images)
- [Development](#development)
- [Multi-tenancy](#multi-tenancy)
- [Security Model](#security-model)
- [Roadmap](#roadmap)

---

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│  External caller (curl / agentctl / SDK)                        │
│    POST /v1/agents/{ns}/{agent}/run        ← trigger            │
│    POST /v1/runs/{ns}/{run-id}/message     ← interact (mid-run) │
│    GET  /v1/runs/{ns}/{run-id}             ← status             │
│    GET  /v1/runs/{ns}/{run-id}/logs        ← logs               │
└───────────────────────┬─────────────────────────────────────────┘
                        │ creates AgentRun CR
                        ▼
┌─────────────────────────────────────────────────────────────────┐
│  agent-operator  (controller-manager)                           │
│                                                                 │
│  Reconcilers:                                                   │
│    LLMProviderReconciler      validates LLM credentials         │
│    MemoryProviderReconciler   pings Redis                       │
│    ToolReconciler             validates tool specs              │
│    MCPServerReconciler        discovers MCP tool list           │
│    AgentReconciler            validates dependency graph        │
│    AgentRunReconciler         spawns pod, tracks lifecycle      │
│    PromptTemplateReconciler   validates Go template syntax      │
│    AgentPolicyReconciler      validates namespace policy        │
│    ClusterPolicyReconciler    validates cluster policy          │
└───────────────────────┬─────────────────────────────────────────┘
                        │ creates ephemeral Pod
                        ▼
┌─────────────────────────────────────────────────────────────────┐
│  agent-runtime  (ephemeral pod — one per AgentRun)              │
│                                                                 │
│  1. Loads Agent, LLMProvider, MemoryProvider from K8s API       │
│  2. Loads AgentPolicy + ClusterPolicy → EffectivePolicy         │
│  3. Renders 7-layer system prompt (ComposeSystemPrompt)         │
│  4. ReAct loop:                                                 │
│       Think  → LLM call                                        │
│       Act    → parallel tool execution (with retry/backoff)    │
│       Observe→ tool results injected as messages               │
│       repeat until final answer or policy limit hit            │
│  5. On limit → GracefulStop (wrap-up LLM call) or HardStop     │
│  6. Writes result to Redis                                      │
│  7. POSTs to callbackURL if set                                 │
│  8. Exits (pod terminates, operator marks AgentRun Succeeded)  │
└─────────────────────────────────────────────────────────────────┘
                        │ result
                        ▼
                      Redis
```

---

## CRD Taxonomy

All CRDs live under `agent.runtime.io/v1alpha1`.

### Core CRDs (6)

| CRD | Scope | Purpose |
|-----|-------|---------|
| `LLMProvider` | Namespace | LLM backend — watsonx, OpenAI-compatible, Ollama |
| `MemoryProvider` | Namespace | Memory backend — Redis (working + episodic) |
| `Tool` | Namespace | A callable tool — built-in or custom container image |
| `MCPServer` | Namespace | External MCP-compatible tool server (HTTP or SSE transport) |
| `Agent` | Namespace | An agent — wires together LLM + memory + tools + prompt |
| `AgentRun` | Namespace | One ephemeral execution of an agent |

### Policy & Prompt CRDs (3)

| CRD | Scope | Purpose |
|-----|-------|---------|
| `PromptTemplate` | Namespace | Reusable Go-template system prompt with typed variables |
| `AgentPolicy` | Namespace | Per-namespace guardrails — token budgets, tool rules, egress, audit |
| `ClusterPolicy` | Cluster | Platform-wide ceilings that namespace policies cannot exceed |

### Example CRs

```yaml
# LLMProvider — watsonx.ai
apiVersion: agent.runtime.io/v1alpha1
kind: LLMProvider
metadata:
  name: granite
  namespace: team-alpha
spec:
  type: watsonx
  endpoint: https://us-south.ml.cloud.ibm.com
  model: ibm/granite-3-3-8b-instruct
  credentialsSecret: watsonx-creds   # Secret with api_key + project_id
  parameters:
    temperature: "0.7"
    maxTokens: "4096"
---
# Agent
apiVersion: agent.runtime.io/v1alpha1
kind: Agent
metadata:
  name: research-agent
  namespace: team-alpha
spec:
  llmProvider: granite
  memoryProvider: redis-memory
  tools:
    - http-tool
    - filesystem-tool
  systemPrompt: |
    You are a research assistant. Use the tools available to answer questions
    thoroughly and cite your sources.
  maxIterations: 20
  timeoutSeconds: 300
```

---

## Policy System

Policy is two-tier: `ClusterPolicy` (admin sets ceilings), `AgentPolicy` (teams configure within those ceilings). The runtime resolves them at pod startup — the loop never reads CRs directly.

### Token & loop controls

```yaml
apiVersion: agent.runtime.io/v1alpha1
kind: AgentPolicy
metadata:
  name: team-alpha-policy
  namespace: team-alpha
spec:
  run:
    maxTokensPerRun: 20000          # hard cap on LLM tokens per run
    warningThresholdPercent: 80     # injects a soft warning at 80%
    maxIterations: 15               # loop iteration ceiling
    maxToolCallsPerRun: 50          # total tool call ceiling
    maxCallsPerTool: 10             # per-tool call ceiling
    onLimitExceeded: GracefulStop   # GracefulStop | HardStop
    loopDetectionEnabled: true      # detect identical repeated tool calls
  tools:
    defaultPolicy: Allow
    rules:
      - tool: shell-exec
        allow: false                # block shell on this namespace
  egress:
    blockedDomains:
      - internal.corp.example.com
    requireTLS: true
  audit:
    conversationLog: true
    toolCallLog: true
```

### 7-layer system prompt composition

Every run assembles the system prompt from these layers in order:

1. **ClusterPolicy preamble** — platform admin injection (e.g. compliance statements)
2. **AgentPolicy preamble** — namespace admin injection
3. **Agent system prompt** — inline string or rendered `PromptTemplate`
4. **AgentPolicy safety rules** — namespace-level safety bullets
5. **ClusterPolicy safety rules** — platform-level safety bullets
6. **Episodic memory summary** — last 5 relevant past interactions from Redis
7. **AgentPolicy epilogue** — final namespace instruction (e.g. response format)

### PromptTemplate

```yaml
apiVersion: agent.runtime.io/v1alpha1
kind: PromptTemplate
metadata:
  name: research-prompt
  namespace: team-alpha
spec:
  template: |
    You are a {{.role}} specialising in {{.domain}}.
    Always respond in {{.language}}.
  variables:
    - name: role
      required: true
    - name: domain
      required: true
    - name: language
      default: English
```

Reference it from an Agent:

```yaml
spec:
  systemPromptRef: research-prompt
  systemPromptVars:
    role: "financial analyst"
    domain: "macroeconomics"
```

---

## Built-in Tools

| Tool | Description |
|------|-------------|
| `tool-finder` | Search the tool catalogue by keyword — helps the agent discover available tools |
| `http-request` | HTTP requests (GET / POST / PUT / DELETE / PATCH) with headers and body |
| `filesystem-read` | Read a file from the pod's filesystem |
| `filesystem-write` | Write a file to the pod's filesystem |
| `filesystem-list` | List directory contents |
| `shell-exec` | Execute a shell command (uses an allow-list; off by default in policy) |
| `kubectl-get` | `kubectl get` a Kubernetes resource by kind/name/namespace |
| `kubectl-apply` | Apply a Kubernetes manifest string |

All tools implement the MCP JSON schema. Custom tools can be added by:
- Pointing a `Tool` CR at a container image that exposes an MCP HTTP server
- Deploying an `MCPServer` CR pointing at any external MCP-compatible server

---

## Redis Key Schema

| Key | Type | Purpose |
|-----|------|---------|
| `agentrun:{name}:messages` | List | Working memory — full conversation history (JSON messages) |
| `agentrun:{name}:result` | String | Final result JSON written by agent-runtime |
| `agentrun:{name}:inbound` | Pub/Sub | Channel for gateway → runtime interaction messages |
| `agentrun:{name}:trace` | List | Structured trace events (iteration, type, timestamp, data) |
| `agent:{name}:episodes` | Sorted Set | Episodic memory — score = unix timestamp |

---

## Quick Start — Kubernetes

### Prerequisites

- Kubernetes 1.26+
- Go 1.25+
- `kubectl` configured against your cluster
- Redis accessible from within the cluster
- Container registry (GHCR, Docker Hub, etc.)

### 1. Clone and build

```bash
git clone https://github.com/nickmerrett/agent-operator.git
cd agent-operator

# Regenerate code if needed (CRD YAMLs are pre-generated in config/crd/)
make generate
make manifests
```

### 2. Build and push images

```bash
# Override the image names if needed
export OPERATOR_IMG=ghcr.io/youruser/agent-operator:latest
export RUNTIME_IMG=ghcr.io/youruser/agent-runtime:latest

make docker-build docker-push
```

### 3. Install CRDs

```bash
make install
# or: kubectl apply -f config/crd/
```

Verify:

```bash
kubectl get crds | grep agent.runtime.io
```

### 4. Create the operator namespace and deploy

```bash
kubectl create namespace agent-operator-system
make deploy
# or: kubectl apply -f config/rbac/ && kubectl apply -f config/manager/

kubectl get pods -n agent-operator-system -w
# agent-operator-<hash>   1/1   Running
```

### 5. Create a tenant namespace

```bash
# Apply the sample namespace (adds PodSecurity restricted labels)
kubectl apply -f config/samples/sample_namespace.yaml
# or manually:
kubectl create namespace agent-system
```

### 6. Apply per-namespace RBAC for AgentRun pods

```bash
kubectl apply -f config/rbac/agentrun_pod_role.yaml -n agent-system
```

### 7. Deploy Redis

```bash
# Quickest option — bitnami helm chart
helm repo add bitnami https://charts.bitnami.com/bitnami
helm install redis bitnami/redis -n agent-system \
  --set auth.password=changeme \
  --set architecture=standalone
```

### 8. Create secrets

```bash
# LLM credentials (watsonx example)
kubectl create secret generic watsonx-creds \
  --from-literal=api_key=YOUR_WATSONX_API_KEY \
  --from-literal=project_id=YOUR_PROJECT_ID \
  -n agent-system

# Redis connection
kubectl create secret generic redis-creds \
  --from-literal=host=redis-master.agent-system.svc.cluster.local \
  --from-literal=port=6379 \
  --from-literal=password=changeme \
  -n agent-system

# Gateway auth token (one per namespace)
kubectl create secret generic agent-gateway-token \
  --from-literal=token=my-secret-token \
  -n agent-system
```

### 9. Apply sample CRs

```bash
# Edit the samples to point at agent-system (default namespace is team-alpha)
sed 's/team-alpha/agent-system/g' config/samples/sample_llmprovider.yaml | kubectl apply -f -
sed 's/team-alpha/agent-system/g' config/samples/sample_memoryprovider.yaml | kubectl apply -f -
sed 's/team-alpha/agent-system/g' config/samples/sample_tools.yaml | kubectl apply -f -
sed 's/team-alpha/agent-system/g' config/samples/sample_agent.yaml | kubectl apply -f -

# Check everything reconciled to Ready
kubectl get llmprovider,memoryprovider,tool,agent -n agent-system
```

### 10. Trigger a run

```bash
# Get the gateway address (adjust for your Ingress setup)
GATEWAY=http://$(kubectl get svc agent-operator-gateway -n agent-operator-system \
  -o jsonpath='{.status.loadBalancer.ingress[0].ip}')

curl -s -X POST $GATEWAY/v1/agents/agent-system/research-agent/run \
  -H "Authorization: Bearer my-secret-token" \
  -H "Content-Type: application/json" \
  -d '{"prompt": "Summarise the latest Go release notes."}' | jq .
```

Response:

```json
{
  "runId": "research-agent-a1b2c3d4",
  "statusURL": "/v1/runs/agent-system/research-agent-a1b2c3d4",
  "resultKey": "agentrun:research-agent-a1b2c3d4:result"
}
```

### 11. Monitor and retrieve results

```bash
# Poll status
curl -s $GATEWAY/v1/runs/agent-system/research-agent-a1b2c3d4 \
  -H "Authorization: Bearer my-secret-token" | jq .

# Stream pod logs
curl -s $GATEWAY/v1/runs/agent-system/research-agent-a1b2c3d4/logs \
  -H "Authorization: Bearer my-secret-token"

# Send a follow-up while the run is in-flight
curl -s -X POST $GATEWAY/v1/runs/agent-system/research-agent-a1b2c3d4/message \
  -H "Authorization: Bearer my-secret-token" \
  -H "Content-Type: application/json" \
  -d '{"message": "Also include the release date."}'
```

---

## Quick Start — OpenShift

OpenShift replaces Kubernetes PodSecurity admission with its SCC (SecurityContextConstraints) system. The steps differ in a few places.

### Prerequisites

- OpenShift 4.12+ (or CRC for local dev)
- `oc` CLI, logged in as `cluster-admin`
- Go 1.25+
- Container registry (GHCR, Quay.io, or the cluster's internal registry)

### CRC (local OpenShift)

```bash
# Install from https://console.redhat.com/openshift/create/local
crc setup
crc start          # ~8 min, requires ~16 GB RAM

eval $(crc oc-env)
oc login -u kubeadmin -p $(crc console --credentials | grep kubeadmin | awk '{print $NF}')
```

### Option: use OpenShift's internal registry (no external registry needed)

```bash
# Expose the internal registry route (CRC — already exposed on real clusters)
oc patch configs.imageregistry.operator.openshift.io/cluster \
  --type merge --patch '{"spec":{"defaultRoute":true}}'

REGISTRY=$(oc get route default-route -n openshift-image-registry -o jsonpath='{.spec.host}')
docker login $REGISTRY -u $(oc whoami) -p $(oc whoami -t)

# Build and push
docker build -f Dockerfile.operator -t $REGISTRY/agent-operator-system/agent-operator:dev .
docker build -f Dockerfile.runtime  -t $REGISTRY/agent-operator-system/agent-runtime:dev  .
docker push $REGISTRY/agent-operator-system/agent-operator:dev
docker push $REGISTRY/agent-operator-system/agent-runtime:dev

# Patch the deployment to use internal registry images
sed -i "s|ghcr.io/nickmerrett/agent-operator:latest|$REGISTRY/agent-operator-system/agent-operator:dev|" \
  config/manager/deployment.yaml
sed -i "s|ghcr.io/nickmerrett/agent-runtime:latest|$REGISTRY/agent-operator-system/agent-runtime:dev|" \
  config/manager/deployment.yaml
```

### 1. Create the operator namespace

```bash
oc new-project agent-operator-system
```

> Do **not** apply `sample_namespace.yaml` to this namespace — that file is for tenant namespaces and adds Kubernetes PodSecurity labels which OpenShift ignores anyway.

### 2. Install CRDs

```bash
oc apply -f config/crd/

# Verify all 9 CRDs are installed
oc get crds | grep agent.runtime.io
```

### 3. Install the custom SCC (cluster-admin required)

This is the key OpenShift-specific step. The SCC is equivalent to `restricted-v2` and grants no extra privileges.

```bash
oc apply -f config/rbac/agent-runtime-scc.yaml

# Verify
oc get scc agent-runtime-restricted
```

### 4. Deploy the operator

```bash
oc apply -f config/rbac/operator_clusterrole.yaml
oc apply -f config/rbac/operator_clusterrolebinding.yaml
oc apply -f config/manager/

# Watch it start
oc get pods -n agent-operator-system -w
oc logs -n agent-operator-system deploy/agent-operator -f
# Look for: "starting HTTP gateway" and "starting manager"
```

### 5. Create a tenant namespace

```bash
oc new-project agent-system
```

### 6. Apply per-namespace RBAC for AgentRun pods

```bash
oc apply -f config/rbac/agentrun_pod_role.yaml -n agent-system
```

### 7. Deploy Redis

```bash
# OpenShift's built-in redis template
oc new-app redis:7 --name=redis -n agent-system \
  -e REDIS_PASSWORD=changeme

# Or bitnami helm chart
helm repo add bitnami https://charts.bitnami.com/bitnami
helm install redis bitnami/redis -n agent-system \
  --set auth.password=changeme \
  --set architecture=standalone
```

### 8. Create secrets

```bash
oc create secret generic watsonx-creds \
  --from-literal=api_key=YOUR_WATSONX_API_KEY \
  --from-literal=project_id=YOUR_PROJECT_ID \
  -n agent-system

oc create secret generic redis-creds \
  --from-literal=host=redis.agent-system.svc.cluster.local \
  --from-literal=port=6379 \
  --from-literal=password=changeme \
  -n agent-system

oc create secret generic agent-gateway-token \
  --from-literal=token=my-secret-token \
  -n agent-system
```

### 9. Apply sample CRs

```bash
sed 's/team-alpha/agent-system/g' config/samples/sample_llmprovider.yaml | oc apply -f -
sed 's/team-alpha/agent-system/g' config/samples/sample_memoryprovider.yaml | oc apply -f -
sed 's/team-alpha/agent-system/g' config/samples/sample_tools.yaml | oc apply -f -
sed 's/team-alpha/agent-system/g' config/samples/sample_agent.yaml | oc apply -f -

oc get llmprovider,memoryprovider,tool,agent -n agent-system
```

### 10. Expose the gateway and trigger a run

```bash
# The Route is pre-configured in config/manager/route.yaml (edge TLS termination)
oc apply -f config/manager/route.yaml
GATEWAY=https://$(oc get route agent-operator-gateway -n agent-operator-system \
  -o jsonpath='{.spec.host}')

curl -s -X POST $GATEWAY/v1/agents/agent-system/research-agent/run \
  -H "Authorization: Bearer my-secret-token" \
  -H "Content-Type: application/json" \
  -d '{"prompt": "What is 2 + 2? Show your working."}' | jq .
```

### 11. Monitor

```bash
# Watch the AgentRun pod appear and complete
oc get pods -n agent-system -w

# Status via gateway
curl -s $GATEWAY/v1/runs/agent-system/research-agent-a1b2c3d4 \
  -H "Authorization: Bearer my-secret-token" | jq .

# Pod logs via gateway
curl -s $GATEWAY/v1/runs/agent-system/research-agent-a1b2c3d4/logs \
  -H "Authorization: Bearer my-secret-token"

# Or directly
oc logs -n agent-system research-agent-a1b2c3d4-pod -f
```

### OpenShift troubleshooting

| Symptom | Likely cause | Fix |
|---------|-------------|-----|
| Pod `CreateContainerConfigError` | SCC rejected the pod | `oc describe pod <name>` — check Events; verify SCC applied: `oc adm policy who-can use scc agent-runtime-restricted` |
| Pod `CrashLoopBackOff` — permission denied | `/tmp` not writable (read-only root FS) | Should not occur — emptyDir `/tmp` is mounted. If it does, check SecurityContext is set on the pod |
| `ImagePullBackOff` | Registry not accessible | Use internal registry or `oc create secret docker-registry` and add to SA |
| Operator pod `403` when creating pods | Missing RBAC | `oc auth can-i create pods --as=system:serviceaccount:agent-operator-system:agent-operator -n agent-system` |
| Gateway returns `401` | Token secret in wrong namespace | The `agent-gateway-token` Secret must be in the **agent's namespace**, not the operator namespace |
| Route returns `503` | Operator not ready yet | `oc logs deploy/agent-operator -n agent-operator-system` — wait for `starting HTTP gateway` |
| `RunAsUser` denied | SCC not bound to SA | `oc adm policy add-scc-to-user agent-runtime-restricted -z agent-runtime -n agent-system` |

---

## agentctl CLI

```bash
# Install
make install-agentctl   # installs to $GOPATH/bin

# Configure
export AGENT_GATEWAY_URL=https://your-gateway.example.com
export AGENT_TOKEN=my-secret-token

# Trigger a run (blocks until complete by default)
agentctl trigger agent-system research-agent "Summarise the Go 1.25 release notes."

# Non-blocking trigger
agentctl trigger --no-wait agent-system research-agent "What time is it?"

# Check status
agentctl status agent-system research-agent-a1b2c3d4

# Stream logs
agentctl logs agent-system research-agent-a1b2c3d4

# Send a follow-up to an in-flight run
agentctl send agent-system research-agent-a1b2c3d4 "Also include the release date."
```

---

## Go Client SDK

```go
import "github.com/nickmerrett/agent-operator/sdk/go/client"

c := client.New("https://your-gateway.example.com", "my-secret-token")

// Trigger and wait for the result
run, err := c.Trigger(ctx, "agent-system", "research-agent", client.TriggerRequest{
    Prompt: "What is the capital of France?",
})
if err != nil {
    log.Fatal(err)
}

result, err := run.Wait(ctx)
fmt.Println(result.Answer)

// Send a follow-up mid-run
if err := run.Send(ctx, "Also give me the population."); err != nil {
    log.Fatal(err)
}

// Stream logs
logs, err := run.Logs(ctx)
fmt.Print(logs)
```

### Writing a custom MCP tool server

```go
import "github.com/nickmerrett/agent-operator/sdk/go/tools"

srv := tools.NewServer()

srv.Register(tools.Tool{
    Name:        "word-count",
    Description: "Count the words in a string",
    InputSchema: tools.Schema{
        Properties: map[string]tools.Property{
            "text": {Type: "string", Description: "The text to count"},
        },
        Required: []string{"text"},
    },
    Handler: func(ctx context.Context, input map[string]interface{}) (map[string]interface{}, error) {
        text, _ := input["text"].(string)
        return map[string]interface{}{"count": len(strings.Fields(text))}, nil
    },
})

log.Fatal(srv.ListenAndServe(":8090"))
```

Then register it as an `MCPServer` CR:

```yaml
apiVersion: agent.runtime.io/v1alpha1
kind: MCPServer
metadata:
  name: my-tools
  namespace: agent-system
spec:
  endpoint: http://my-tool-server.agent-system.svc.cluster.local:8090
  transport: http
```

---

## Building Images

```bash
# Build binaries only
make build

# Build Docker images (uses OPERATOR_IMG and RUNTIME_IMG from Makefile)
make docker-build

# Push
make docker-push

# Override image names
make docker-build docker-push \
  OPERATOR_IMG=quay.io/youruser/agent-operator:v0.1.0 \
  RUNTIME_IMG=quay.io/youruser/agent-runtime:v0.1.0
```

Both images build from a two-stage Dockerfile:
- **Build stage**: `golang:1.22-alpine` — compiles a static binary
- **Runtime stage (operator)**: `gcr.io/distroless/static:nonroot` — ~3 MB, UID 65532
- **Runtime stage (agent-runtime)**: `alpine:3.19` — ~12 MB, UID 1000 (shell tools available)

---

## Development

### Code generation

```bash
# After modifying api/v1alpha1/*_types.go:
make generate   # regenerates zz_generated.deepcopy.go
make manifests  # regenerates config/crd/*.yaml

# Tool version (Go 1.25 compatible)
# CONTROLLER_TOOLS_VERSION = v0.20.1  (set in Makefile)
```

### Run locally against a cluster

```bash
# Run the operator out-of-cluster (uses your local kubeconfig)
make run

# Or directly:
go run ./cmd/operator/ \
  --runtime-image=ghcr.io/nickmerrett/agent-runtime:latest \
  --gateway-bind-address=:8080
```

### Format and vet

```bash
make fmt
make vet
```

### Project layout

```
agent-operator/
├── api/v1alpha1/                  # CRD Go types (one file per CRD)
│   ├── groupversion_info.go
│   ├── llmprovider_types.go
│   ├── memoryprovider_types.go
│   ├── tool_types.go
│   ├── mcpserver_types.go
│   ├── agent_types.go
│   ├── agentrun_types.go
│   ├── prompttemplate_types.go
│   ├── agentpolicy_types.go
│   ├── clusterpolicy_types.go
│   └── zz_generated.deepcopy.go
├── cmd/
│   ├── operator/main.go           # operator entry point
│   ├── agent-runtime/main.go      # ephemeral pod brain
│   └── agentctl/main.go           # CLI
├── internal/
│   ├── controller/                # one reconciler per CRD
│   ├── gateway/server.go          # HTTP gateway
│   ├── llm/                       # LLM adapters (watsonx, openai, ollama)
│   ├── memory/                    # Redis memory store
│   ├── mcp/                       # MCP client (HTTP + SSE)
│   ├── runtime/                   # ReAct loop, policy resolver, prompt composer
│   └── tools/                     # Tool catalogue + built-in tools
├── sdk/go/
│   ├── client/                    # Go client SDK
│   └── tools/                     # MCP tool server SDK
├── config/
│   ├── crd/                       # Generated CRD YAMLs (9 files)
│   ├── rbac/                      # ClusterRole, RoleBinding, SCC
│   ├── manager/                   # Deployment, Service, Ingress, Route
│   └── samples/                   # Example CRs for all types
├── Dockerfile.operator
├── Dockerfile.runtime
└── Makefile
```

---

## Multi-tenancy

Each namespace is fully isolated:

- LLMProvider, MemoryProvider, Tool, MCPServer, Agent, AgentRun, PromptTemplate, AgentPolicy are all **namespace-scoped**
- Credential secrets stay in the namespace — never copied across
- The `agent-runtime` ServiceAccount is namespace-scoped (deploy `agentrun_pod_role.yaml` per namespace)
- Gateway auth token (`agent-gateway-token` Secret) is namespace-scoped — different teams get different tokens
- `ClusterPolicy` sets hard ceilings; teams configure `AgentPolicy` within those ceilings

AgentRun pods are labelled for easy targeting by existing cluster policy engines:

| Label | Value |
|-------|-------|
| `agent-operator/managed` | `true` |
| `agent-operator/agent` | `<agent-name>` |
| `agent-operator/run` | `<agentrun-name>` |

Use these labels in NetworkPolicy, Kyverno ClusterPolicy, OPA Gatekeeper, or Istio AuthorizationPolicy rules without any changes to this operator.

---

## Security Model

### Pod security

All AgentRun pods are launched with the Kubernetes `restricted` PodSecurity profile:

| Setting | Value |
|---------|-------|
| `runAsNonRoot` | `true` |
| `runAsUser` / `runAsGroup` | `1000` |
| `allowPrivilegeEscalation` | `false` |
| `readOnlyRootFilesystem` | `true` |
| `capabilities.drop` | `["ALL"]` |
| `seccompProfile` | `RuntimeDefault` |
| `/tmp` volume | `emptyDir` (writable scratch) |

On OpenShift: the `agent-runtime-restricted` SCC (installed via `config/rbac/agent-runtime-scc.yaml`) enforces the same constraints using the SCC system.

### Gateway authentication

Every HTTP request to the gateway must carry a `Bearer` token that matches the `agent-gateway-token` Secret in the **target namespace**. There is no cluster-wide master token — each namespace authenticates independently.

### Egress defaults

The platform default egress blocked domain list includes `169.254.169.254` (cloud metadata endpoint). Additional blocked domains can be added via `ClusterPolicy` or `AgentPolicy`.

---

## Roadmap

- **Sub-agent spawning** — planner → executor pattern; architecture is ready, no Pipeline CRD yet
- **Pipeline CRD** — multi-agent sequencing (DAG of AgentRun steps)
- **WASM/Spin tool execution** — `runtimeClassName: wasmtime`; Tool CR has `image` field ready
- **SSE streaming** — stream trace events to the caller in real time
- **OpenTelemetry** — emit spans and metrics from the runtime loop
- **EgressPolicy CRD** — generate NetworkPolicy / Istio AuthorizationPolicy from declarative egress rules
- **Vector store episodic memory** — pgvector, Qdrant, Weaviate backends
- **LLM routing policy** — cost-aware model selection per run
- **Anthropic + Gemini adapters** — additional LLM providers
