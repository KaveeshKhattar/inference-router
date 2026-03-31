# scrape.py
import requests
import time

METRICS_URL = "http://localhost:8000/metrics"

METRICS_TO_WATCH = [
    "vllm:num_requests_waiting",
    "vllm:num_requests_running",
    "vllm:kv_cache_usage_perc",
    "vllm:time_to_first_token_seconds_sum",
    "vllm:time_to_first_token_seconds_count",
]

def parse_metrics(text):
    values = {}
    for line in text.splitlines():
        if line.startswith("#"):
            continue
        for metric in METRICS_TO_WATCH:
            if line.startswith(metric):
                parts = line.rsplit(" ", 1)
                values[metric] = float(parts[1])
    return values

def max_ttft_bucket(metrics_text):
    for line in metrics_text.splitlines():
        if "time_to_first_token_seconds_bucket" in line and 'le="+Inf"' in line:
            return float(line.rsplit(" ", 1)[1])
    return 0

def avg_ttft(metrics):
    s = metrics.get("vllm:time_to_first_token_seconds_sum", 0)
    c = metrics.get("vllm:time_to_first_token_seconds_count", 0)
    return (s / c) if c > 0 else 0.0

while True:
    try:
        r = requests.get(METRICS_URL, timeout=2)
        m = parse_metrics(r.text)
        running = int(m.get("vllm:num_requests_running", 0))
        max_running = max(max_running, r)
        print(
            f"waiting={int(m.get('vllm:num_requests_waiting', 0))} "
            f"running={int(m.get('vllm:num_requests_running', 0))} "
            f"kv_cache={m.get('vllm:kv_cache_usage_perc', 0):.2f} "
            f"max_ttft={max_ttft_bucket(m):.4f}s"
            f"avg_ttft={avg_ttft(m):.4f}s"
            f"running={r} max_running={max_running}"
        )
    except Exception as e:
        print(f"error: {e}")
    time.sleep(0.2)