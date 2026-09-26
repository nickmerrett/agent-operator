# Agent Operator

A production-grade, Kubernetes-native agentic runtime framework written in Go.

Define LLM providers, memory backends, tools, and agents as Kubernetes Custom Resources.
Trigger agents via HTTP — ephemeral pods spin up, drive the ReAct reasoning loop, and terminate when done.

## Architecture

```
HTTP Gateway (external)
  POST /v1/agents/{ns}/{agent}/run  →  creates AgentRun CR
  POST /v1/runs/{ns}/{run-id}/message  →  sends follow-up to in-flight run

Operator (controller-runtime)
  LLMProviderReconciler    — validates LLM credentials
  MemoryProviderReconciler — pings Redis backend
  ToolReconciler           — validates tool specs
  MCPServerReconciler      — discovers MCP tools
  AgentReconciler          — validates dependency graph
  AgentRunReconciler       — spawns ephemeral pod, tracks lifecycle

AgentRun Pod (agent-runtime binary)
  Loads Agent config from Kubernetes API
  Drives ReAct loop: Think → Act (tool call) → Observe → repeat
  Writes result to Redis
  POSTs to callbackURL if set
```

## CRD Taxonomy

| CRD | Purpose |
|-----|---------|
| `LLMProvider` | LLM backend (watsonx, openai, ollama, anthropic) |
| `MemoryProvider` | Memory backend (Redis) — working + episodic memory |
| `Tool` | A callable tool (built-in or custom image) |
| `MCPServer` | External MCP-compatible tool server |
| `Agent` | An agent — LLM + memory + tools + system prompt |
| `AgentRun` | A single ephemeral execution of an agent |

## Built-in Tools

| Tool | Description |
|------|-------------|
| `tool-finder` | Search the tool catalogue by keyword |
| `http-request` | Make HTTP requests (GET/POST/PUT/DELETE/PATCH) |
| `filesystem-read` | Read a file |
| `filesystem-write` | Write a file |
| `filesystem-list` | List a directory |
| `shell-exec` | Execute a shell command (allow-listed) |
| `kubectl-get` | Get a Kubernetes resource |
| `kubectl-apply` | Apply a Kubernetes manifest |

## Redis Key Schema

| Key | Purpose |
|-----|---------|
| `agentrun:{name}:messages` | Working memory — full conversation history |
| `agentrun:{name}:result` | Final result written by agent-runtime |
| `agentrun:{name}:inbound` | Pub/sub channel for interaction messages |
| `agent:{name}:episodes` | Episodic memory sorted set |

## Quick Start

### Prerequisites

- Kubernetes cluster (1.26+) or OpenShift 4.12+
- Go 1.22+
- `kubectl` configured
- Redis instance accessible from the cluster

### 1. Install CRDs

```bash
make install
```

### 2. Create the operator namespace and deploy

```bash
kubectl create namespace agent-operator-system
make deploy
```

### 3. Create a namespace for your team

```bash
kubectl create namespace team-alpha
```

### 4. Apply RBAC for AgentRun pods

```bash
# Edit namespace field in agentrun_pod_role.yaml first
kubectl apply -f config/rbac/agentrun_pod_role.yaml -n team-alpha
```

### 5. Create credentials secrets

```bash
kubectl create secret generic watsonx-creds \
  --from-literal=api_key=<your-api-key> \
  --from-literal=project_id=<your-project-id> \
  -n team-alpha

kubectl create secret generic redis-creds \
  --from-literal=host=<redis-host> \
  --from-literal=port=6379 \
  --from-literal=password=<redis-password> \
  -n team-alpha

kubectl create secret generic agent-gateway-token \
  --from-literal=token=<your-gateway-token> \
  -n team-alpha
```

### 6. Apply sample CRs

```bash
kubectl apply -f config/samples/sample_llmprovider.yaml
kubectl apply -f config/samples/sample_memoryprovider.yaml
kubectl apply -f config/samples/sample_tools.yaml
kubectl apply -f config/samples/sample_agent.yaml
```

Check everything is Ready:

```bash
kubectl get llmprovider,memoryprovider,tool,agent -n team-alpha
```

### 7. Trigger an agent run via HTTP

```bash
# Get the gateway URL (adjust for your Ingress/Route)
GATEWAY=http://agent-gateway.example.com

curl -s -X POST $GATEWAY/v1/agents/team-alpha/research-agent/run \
  -H "Authorization: Bearer <your-gateway-token>" \
  -H "Content-Type: application/json" \
  -d '{
    "prompt": "What is the capital of France?",
    "callbackURL": "https://my-app.example.com/webhook"
  }'
```

Response:

```json
{
  "runId": "research-agent-abc12345",
  "statusURL": "/v1/runs/team-alpha/research-agent-abc12345",
  "resultKey": "agentrun:research-agent-abc12345:result"
}
```

### 8. Check run status

```bash
curl -s $GATEWAY/v1/runs/team-alpha/research-agent-abc12345 \
  -H "Authorization: Bearer <your-gateway-token>"
```

### 9. Read result from Redis

```bash
redis-cli GET "agentrun:research-agent-abc12345:result"
```

### 10. Send a follow-up to an in-flight run

```bash
curl -s -X POST $GATEWAY/v1/runs/team-alpha/research-agent-abc12345/message \
  -H "Authorization: Bearer <your-gateway-token>" \
  -H "Content-Type: application/json" \
  -d '{"message": "Also include the population."}'
```

## Building

```bash
# Build both binaries
make build

# Build Docker images
make docker-build

# Push images
make docker-push
```

## Development

```bash
# Install code generation tools
make controller-gen

# Generate DeepCopy methods
make generate

# Generate CRD manifests
make manifests

# Run locally (requires kubeconfig)
make run
```

## Multi-tenancy

Each namespace is isolated:
- Credentials (LLMProvider, MemoryProvider secrets) are namespace-scoped
- AgentRun pods use a namespace-scoped ServiceAccount (`agent-runtime`)
- Gateway auth token is per-namespace (`agent-gateway-token` Secret)
- Existing cluster policies (NetworkPolicy, Kyverno, OPA) can target agent pods via labels:
  - `agent-operator/managed=true`
  - `agent-operator/agent=<agent-name>`
  - `agent-operator/run=<run-id>`

## Roadmap

- WASM/Spin tool execution (`runtimeClassName: wasmtime`)
- Sub-agent spawning (planner → executor pattern)
- Pipeline CRD (multi-agent sequencing)
- SSE streaming results
- OpenTelemetry observability
- EgressPolicy CRD (fine-grained outbound enforcement)
- LLM routing policy
- Vector store episodic memory (pgvector, Qdrant)
