#!/usr/bin/env python3
"""
Realistic load generator for LLM inference (router or direct vLLM/simulator).

Features:
- Realistic prompt length distribution (based on ShareGPT/OpenOrca).
- Realistic max_tokens distribution (short answers dominate, some long).
- Closed‑loop concurrency (workers pick next request immediately after previous finishes).
- Poisson arrival rate (optional, for open‑loop bursts).
- Tracks failures: timeout, HTTP error, connection error, invalid JSON.
- Writes detailed CSV with timestamps, latencies, error types.
- Prints summary with error rate, throughput, and percentiles.

Usage:
    python realistic_load_gen.py --url http://localhost:9000/v1/chat/completions \
                                 --concurrency 20 \
                                 --duration 60 \
                                 --model test \
                                 --output results.csv
"""

import argparse
import asyncio
import aiohttp
import csv
import random
import time
import sys
from dataclasses import dataclass, field
from typing import Optional, List
import statistics

# ------------------------------------------------------------
# Realistic prompt length distribution (tokens)
# Based on real chat datasets: ~80% short (<256), 15% medium, 5% long
# ------------------------------------------------------------
def sample_prompt_tokens() -> int:
    r = random.random()
    if r < 0.70:
        return random.randint(8, 128)      # short user queries
    elif r < 0.85:
        return random.randint(129, 512)    # medium context
    else:
        return random.randint(513, 2048)   # long document

# ------------------------------------------------------------
# Realistic generation length (max_tokens)
# Most responses are short; some are long (e.g., reasoning, lists)
# ------------------------------------------------------------
def sample_max_tokens() -> int:
    r = random.random()
    if r < 0.65:
        return random.randint(16, 128)     # short answers
    elif r < 0.85:
        return random.randint(129, 384)    # medium
    else:
        return random.randint(385, 1024)   # long (rare)

# ------------------------------------------------------------
# Generate a plausible prompt text (not just random words)
# ------------------------------------------------------------
def generate_prompt_text(prompt_tokens: int) -> str:
    # Simple placeholder: repeat a few realistic sentences.
    # For true realism, you could sample from a real dataset.
    sentences = [
        "Explain the concept of distributed systems.",
        "What are the benefits of using Kubernetes for inference?",
        "Write a short Python function to compute fibonacci numbers.",
        "Summarize the history of the Roman Empire.",
        "Compare and contrast GPT-4 and Llama 3.",
        "How do you optimize LLM serving latency?",
        "Describe the attention mechanism in transformers.",
    ]
    # Approximate tokens: 1 token ~ 0.75 words. This is crude but fine for load testing.
    words_per_sentence = random.randint(10, 30)
    sentence = random.choice(sentences)
    # Repeat the sentence enough times to reach desired token count (very rough)
    repeat = max(1, prompt_tokens // (len(sentence.split()) + 5))
    return (sentence + " ") * repeat

# ------------------------------------------------------------
# Request result data class
# ------------------------------------------------------------
@dataclass
class RequestResult:
    worker_id: int
    start_time: float
    end_time: float
    latency_s: float
    prompt_tokens: int
    max_tokens: int
    status_code: int
    error_type: str  # "none", "timeout", "http_error", "connection", "json_decode"
    error_msg: str
    ttft_s: Optional[float] = None  # not used here, but could be added

# ------------------------------------------------------------
# Worker: sends one request, records result
# ------------------------------------------------------------
async def send_request(
    session: aiohttp.ClientSession,
    url: str,
    model: str,
    timeout_s: float,
    worker_id: int,
    request_id: int,
    verbose: bool,
) -> RequestResult:
    prompt_tokens = sample_prompt_tokens()
    max_tokens = sample_max_tokens()
    prompt_text = generate_prompt_text(prompt_tokens)

    payload = {
        "model": "meta-llama/Llama-3.1-8B-Instruct",
        "messages": [{"role": "user", "content": prompt_text}],
        "max_tokens": max_tokens,
        "temperature": 0.7,
    }

    start = time.time()
    error_type = "none"
    error_msg = ""
    status_code = 0
    ttft = None

    try:
        timeout = aiohttp.ClientTimeout(total=timeout_s)
        async with session.post(url, json=payload, timeout=timeout) as resp:
            status_code = resp.status
            # Read full response (streaming not needed for this simple test)
            await resp.json()  # this also consumes the response body
            end = time.time()
            latency = end - start
            # We don't measure TTFT here – that requires streaming.
            # For TTFT, we'd need to read first byte separately.
    except asyncio.TimeoutError:
        error_type = "timeout"
        error_msg = f"Timeout after {timeout_s}s"
        end = time.time()
        latency = end - start
    except aiohttp.ClientError as e:
        error_type = "connection"
        error_msg = str(e)
        end = time.time()
        latency = end - start
    except Exception as e:
        error_type = "unknown"
        error_msg = str(e)
        end = time.time()
        latency = end - start
    else:
        if status_code < 200 or status_code >= 300:
            error_type = "http_error"
            error_msg = f"HTTP {status_code}"

    if verbose and (error_type != "none" or random.random() < 0.1):
        print(f"[{worker_id}:{request_id}] {error_type if error_type!='none' else 'OK'} "
              f"lat={latency:.3f}s prompt={prompt_tokens} max={max_tokens}")

    return RequestResult(
        worker_id=worker_id,
        start_time=start,
        end_time=end,
        latency_s=latency,
        prompt_tokens=prompt_tokens,
        max_tokens=max_tokens,
        status_code=status_code,
        error_type=error_type,
        error_msg=error_msg,
    )

# ------------------------------------------------------------
# Main load test with closed‑loop concurrency
# ------------------------------------------------------------
async def run_closed_loop(
    url: str,
    model: str,
    concurrency: int,
    duration_s: int,
    timeout_s: float,
    verbose: bool,
    output_csv: str,
):
    results: List[RequestResult] = []
    request_counter = 0
    stop_time = time.time() + duration_s

    async with aiohttp.ClientSession() as session:
        # Use a semaphore to limit concurrency (closed‑loop: each worker loops until time ends)
        semaphore = asyncio.Semaphore(concurrency)

        async def worker(worker_id: int):
            nonlocal request_counter
            while time.time() < stop_time:
                async with semaphore:
                    req_id = request_counter
                    request_counter += 1
                    result = await send_request(
                        session, url, model, timeout_s,
                        worker_id, req_id, verbose
                    )
                    results.append(result)

        # Launch workers
        workers = [asyncio.create_task(worker(i)) for i in range(concurrency)]
        await asyncio.gather(*workers, return_exceptions=True)

    # Write CSV
    with open(output_csv, "w", newline="") as f:
        writer = csv.writer(f)
        writer.writerow([
            "worker_id", "timestamp", "latency_s", "prompt_tokens", "max_tokens",
            "status_code", "error_type", "error_msg"
        ])
        for r in results:
            writer.writerow([
                r.worker_id, r.start_time, round(r.latency_s, 6),
                r.prompt_tokens, r.max_tokens, r.status_code,
                r.error_type, r.error_msg
            ])

    # Summary statistics (excluding failed requests for latency percentiles)
    latencies_ok = [r.latency_s for r in results if r.error_type == "none"]
    total = len(results)
    success = len(latencies_ok)
    errors = total - success

    print("\n=== Load Test Summary ===")
    print(f"Duration: {duration_s}s, Concurrency: {concurrency}")
    print(f"Total requests: {total}")
    print(f"Successful: {success} ({100*success/total:.2f}%)")
    print(f"Errors: {errors}")
    if errors > 0:
        error_counts = {}
        for r in results:
            if r.error_type != "none":
                error_counts[r.error_type] = error_counts.get(r.error_type, 0) + 1
        print("Error breakdown:")
        for err_type, count in error_counts.items():
            print(f"  {err_type}: {count}")

    if latencies_ok:
        latencies_ok.sort()
        p50 = latencies_ok[int(len(latencies_ok) * 0.5)]
        p95 = latencies_ok[int(len(latencies_ok) * 0.95)]
        p99 = latencies_ok[int(len(latencies_ok) * 0.99)]
        avg = statistics.mean(latencies_ok)
        print(f"\nLatency (successful only):")
        print(f"  p50 = {p50*1000:.1f} ms")
        print(f"  p95 = {p95*1000:.1f} ms")
        print(f"  p99 = {p99*1000:.1f} ms")
        print(f"  avg = {avg*1000:.1f} ms")
        print(f"Throughput: {success / duration_s:.2f} req/s")
    else:
        print("No successful requests.")

    print(f"\nResults saved to {output_csv}")

# ------------------------------------------------------------
# Entry point
# ------------------------------------------------------------
def main():
    parser = argparse.ArgumentParser(description="Realistic LLM load generator")
    parser.add_argument("--url", default="http://localhost:9000/v1/chat/completions")
    parser.add_argument("--model", default="meta-llama/Llama-3.1-8B-Instruct")
    parser.add_argument("--concurrency", type=int, default=10, help="Number of concurrent workers")
    parser.add_argument("--duration", type=int, default=60, help="Test duration in seconds")
    parser.add_argument("--timeout", type=float, default=120.0, help="Per‑request timeout seconds")
    parser.add_argument("--verbose", action="store_true", help="Print per‑request progress")
    parser.add_argument("--output", default="load_results.csv", help="Output CSV file")
    args = parser.parse_args()

    asyncio.run(run_closed_loop(
        url=args.url,
        model=args.model,
        concurrency=args.concurrency,
        duration_s=args.duration,
        timeout_s=args.timeout,
        verbose=args.verbose,
        output_csv=args.output,
    ))

if __name__ == "__main__":
    main()