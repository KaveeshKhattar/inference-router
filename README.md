# inference-router

A queue-depth-aware inference request router for Kubernetes-hosted vLLM replicas.

Built to demonstrate — and fix — a fundamental problem with naive load balancing in LLM inference: round-robin is blind to queue state, causing latency divergence under skewed prompt-length distributions. This project implements the same scheduling signals used by [llm-d's Inference Scheduler](https://github.com/llm-d/llm-d) and [GIE's Endpoint Picker](https://github.com/kubernetes-sigs/gateway-api-inference-extension) in a from-scratch Go router.

---

## The Problem

Standard Kubernetes Services distribute requests using round-robin. For stateless HTTP services this is fine. For LLM inference it breaks down:

- Long prompts hold GPU slots open for seconds
- Round-robin sends the next request to whichever replica is "next", regardless of how many requests are already queued there
- Result: one replica saturates while the other sits idle

Under a realistic skewed prompt distribution (80% short, 20% long), this produces **5x queue depth divergence** between replicas — measured and reproduced in this project using the llm-d vLLM simulator.

```
19:06:45  replica-1: running=8  waiting=5   ← saturated, clients waiting
          replica-2: running=7  waiting=0   ← free slots, round-robin ignored this
```

The router fixes this by reading `vllm:num_requests_waiting` and `vllm:num_requests_running` from each replica's `/metrics` endpoint before every routing decision.

---

## Architecture

```
load-generator (Python)
        │
        ▼
  inference-router (Go)          ← this project
        │
        ├──── scrapes /metrics from each replica
        ├──── scores replicas: f(queue_depth, running, kv_cache)
        │
        ├──▶  vllm-sim replica-1 (pod)
        └──▶  vllm-sim replica-2 (pod)
                    │
                    ▼
             Prometheus → Grafana
```

Inference traffic flows through the router. The router makes a routing decision per request based on real-time replica health. Prometheus scrapes both the simulator pods and the router itself.

---

## Simulator Configuration

The [llm-d vLLM simulator](https://github.com/llm-d/llm-d-inference-sim) is used in place of a real vLLM instance. It exposes an OpenAI-compatible API and the full vLLM Prometheus metrics surface — no GPU required.

Simulator parameters are tuned to model a **T4-class inference node** running Llama-3-8B in fp16:

| Flag | Value | Rationale |
|---|---|---|
| `--max-num-seqs` | 8 | T4 has 16GB VRAM; ~8GB for model weights, ~8GB for KV cache at 2048 context |
| `--time-to-first-token` | 400ms | T4 TTFT at light load, from vLLM published benchmarks |
| `--time-to-first-token-std-dev` | 80ms | ~20% jitter, models thermal and memory bandwidth variability |
| `--prefill-time-per-token` | 2ms | Linear prefill cost; 500-token prompt adds ~1s to TTFT |
| `--inter-token-latency` | 30ms | T4 decode throughput for Llama-3-8B |
| `--inter-token-latency-std-dev` | 8ms | ~25% decode jitter, realistic for memory-bandwidth-bound ops |
| `--time-factor-under-load` | 3.0 | Latency 3x under full concurrency; models GPU memory bandwidth saturation |
| `--kv-cache-size` | 512 | 512 blocks × 16 tokens = 8192 token slots, fits T4 KV budget |
| `--max-model-len` | 2048 | Context window under T4 memory constraints |

`--prefill-time-per-token` combined with `--time-factor-under-load` is what makes routing decisions matter: long prompts hold slots open, and a saturated replica degrades for all subsequent requests — the exact condition a queue-aware router exploits.

---

## Layers

This project is built in observable layers. Each layer is independently runnable and produces a measurable output.

### Layer 0 — Simulator running in Docker

Get the vLLM simulator running locally. Send a prompt via curl. Confirm an OpenAI-compatible response comes back. No Kubernetes yet.

```bash
docker run --rm -p 8000:8000 ghcr.io/llm-d/llm-d-inference-sim:v0.8.0 \
  --model dummy --port 8000 \
  --time-to-first-token 400ms \
  --inter-token-latency 30ms \
  --max-num-seqs 8 \
  --time-factor-under-load 3.0
```

```bash
curl http://localhost:8000/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model":"dummy","messages":[{"role":"user","content":"hello"}]}'
```

**Output:** OpenAI-compatible JSON response with `prompt_tokens`, `completion_tokens`, `usage`.

---

### Layer 1 — Reading the metrics surface

The simulator exposes vLLM-compatible Prometheus metrics at `/metrics`. The key signals for routing decisions:

| Metric | Type | Routing relevance |
|---|---|---|
| `vllm:num_requests_waiting` | Gauge | Primary queue pressure signal |
| `vllm:num_requests_running` | Gauge | Current slot utilization |
| `vllm:kv_cache_usage_perc` | Gauge | Memory pressure |
| `vllm:time_to_first_token_seconds` | Histogram | Outcome SLO metric |
| `vllm:request_queue_time_seconds` | Histogram | Time requests spent waiting |

A Python scraper polls `/metrics` every 5 seconds and prints per-replica health. This is the foundation of the router's scoring function.

```bash
python3 load-generator/scrape.py
# waiting=0 running=0 kv_cache=0.00 avg_ttft=0.0000s
# waiting=3 running=8 kv_cache=0.12 avg_ttft=0.6240s  ← under load
```

---

### Layer 2 — Two replicas on Kubernetes

The simulator is deployed as a 2-replica Deployment in a local kind cluster. A NodePort Service exposes inference traffic. Each pod is independently port-forwarded for per-replica metrics scraping.

```bash
kind create cluster --name llm-cluster
kubectl apply -f k8s/vllm-deployment.yaml
kubectl apply -f k8s/vllm-service.yaml
```

Kubernetes now round-robins inference traffic across both replicas — blindly.

---

### Layer 3 — The imbalance, made visible

A Python load generator fires 20 concurrent requests with skewed prompt lengths (`[5, 10, 50, 100, 500, 1000]` tokens) in continuous waves. Prometheus scrapes both replicas at 1s resolution. Grafana renders per-replica queue depth and running requests side by side.

**Observed under load (round-robin baseline):**

```
19:06:45  replica-1: running=8  waiting=5
          replica-2: running=7  waiting=0
```

Replica 2 had free slots. Replica 1 had 5 requests queued. Round-robin sent work to replica 1 anyway. Those 5 requests waited unnecessarily — the latency cost of routing blindness.

This is the problem. Layers 4–7 build the solution.

---

## Load Test Results
 
All tests use Poisson arrival process, long prompts (80% of requests), `meta-llama/Llama-3.1-8B-Instruct`.
 
### Round-Robin Saturation Curve (long prompts (20% of requests))
 
| RPS | p50 | p95 | p99 | avg |
|-----|-----|-----|-----|-----|
| 6 | 0.81s | 8.0s | 19.1s | 1.84s |
| 8 | 0.91s | 9.8s | 20.2s | 2.01s |
| 9 | 0.91s | 9.3s | 21.3s | 2.10s |
| 10 | 0.84s | 6.1s | 12.5s | 1.56s |
| 12 | 1.29s | 7.2s | 23.8s | 2.50s |
| 20 | 9.80s | 32.3s | 42.6s | 12.7s |
| 25 | 10.0s | 24.4s | 32.8s | 11.1s |
| 45 | 14.9s | 30.0s | 38.7s | 16.6s |
| 75 | 17.9s | 28.5s | 43.5s | 18.2s |
 
**Saturation cliff: ~15 RPS.** Below this, p50 stays under 1.3s. Above it, p50 jumps to 9.8s+.
 
### Queue-Aware vs Round-Robin (v1 scoring, 15 RPS, long prompts (20% of requests))
 
| Metric | Round-Robin | Queue-Aware | Delta |
|--------|-------------|-------------|-------|
| p50 | ~1.3s | 2.9s | +1.6s |
| p95 | ~10s | 12.3s | +2.3s |
| p99 | ~22s | 25.6s | +3.5s |
| avg | ~2.5s | 4.2s | +1.7s |
 
QA underperforms with v1 scoring — broken `estimatedLoad` formula means routing decisions are effectively random with added per-request scrape overhead.
 
### Queue-Aware vs Round-Robin (v2 scoring, 5 RPS, long prompts (80% of requests))
 
| Metric | Round-Robin | Queue-Aware | Delta |
|--------|-------------|-------------|-------|
| p50 | 1.29s | 1.41s | +0.12s _(expected)_ |
| p95 | 11.22s | 4.82s | **−57%** |
| p99 | 21.30s | 17.25s | −19% |
| avg | 2.50s | 2.12s | −15% |
 
p50 slightly worse at low load — no queue pressure to optimize, scoring overhead adds a few ms. p95/p99 improvement is the real signal.
 
### Queue-Aware vs Round-Robin (v2 scoring, 15 RPS, long prompts (80% of requests))
 
| Metric | Round-Robin | Queue-Aware | Delta |
|--------|-------------|-------------|-------|
| p50 | 6.54s | 3.53s | **−46%** |
| p95 | 25.30s | 14.23s | **−44%** |
| p99 | 35.61s | 26.45s | **−26%** |
| avg | 8.87s | 4.87s | **−45%** |
 
### Queue-Aware vs Round-Robin (v2 scoring, 25 RPS, long prompts (80% of requests))
 
Both routers degrade equally above saturation. At 25 RPS the system is overwhelmed regardless of routing strategy — per-request scrape overhead makes QA slightly worse.
 
| Metric | Round-Robin | Queue-Aware |
|--------|-------------|-------------|
| p50 | 18.6s | 21.7s |
| p95 | 27.1s | 33.0s |
| avg | 16.6s | 19.5s |


---

## Stack

| Component | Purpose |
|---|---|
| [llm-d-inference-sim](https://github.com/llm-d/llm-d-inference-sim) | vLLM-compatible inference simulator, no GPU required |
| Go | Router implementation |
| Python / asyncio | Load generator |
| Kubernetes / kind | Local cluster |
| Helm | Router deployment packaging |
| Prometheus | Metrics scraping (1s resolution) |
| Grafana | Per-replica dashboard |
| OpenTelemetry | Distributed tracing across router and replicas |

---

## Relationship to llm-d and GIE

This project reimplements the core scheduling logic of production inference routing systems:

- **llm-d Inference Scheduler** uses `num_requests_waiting`, `kv_cache_usage_perc`, and `prefix_cache_hits` to score replicas via Envoy's ext-proc interface
- **GIE Endpoint Picker (EPP)** implements the same signals as a Kubernetes-native ext-proc server sitting behind any Gateway API-compatible proxy

This router is a from-scratch Go implementation of that same scoring idea — without the Envoy dependency — designed to make the algorithm legible and the experiment reproducible using llm-d's own simulator tooling.

---

## Status

- [x] Layer 0 — Simulator running
- [x] Layer 1 — Metrics scraper
- [x] Layer 2 — Kubernetes deployment
- [x] Layer 3 — Load generator + Grafana baseline
- [x] Layer 4 — Go metrics client
- [x] Layer 5 — Router with scoring function
- [x] Layer 6 — Helm chart + router metrics
- [ ] Layer 7 — Controlled benchmark, p99 TTFT comparison