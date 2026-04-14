import requests
import time
from rich.console import Console
from rich.live import Live
from rich.table import Table

# Configuration
PROMETHEUS_URL = "http://localhost:9090/api/v1/query"
QUERY = "{__name__=~'vllm:.*(_sum|_count|total|perc|running|waiting|info)'}"
REFRESH_RATE = 0.1

CATEGORIES = {
    "🚀 Traffic & Throughput": [
        "vllm:request_success_total",
        "vllm:prompt_tokens_total",
        "vllm:generation_tokens_total",
    ],
    "⏱️ Latency Metrics (Total)": [
        "vllm:e2e_request_latency_seconds_sum",
        "vllm:time_to_first_token_seconds_sum",
        "vllm:inter_token_latency_seconds_sum",
    ],
    "🏗️ Scheduler & Memory": [
        "vllm:num_requests_running",
        "vllm:num_requests_waiting",
        "vllm:kv_cache_usage_perc",
    ]
}

console = Console()

def fetch_metrics():
    try:
        response = requests.get(PROMETHEUS_URL, params={'query': QUERY}, timeout=2)
        response.raise_for_status()
        results = response.json().get('data', {}).get('result', [])
        
        # Structure: metrics_map[metric_name][replica_id] = [(value, other_labels)]
        metrics_map = {}
        for item in results:
            name = item['metric'].get('__name__')
            val = item['value'][1]
            replica = item['metric'].get('replica', 'unknown-pod')
            
            # Keep specific context labels (like finish_reason)
            other_labels = {k: v for k, v in item['metric'].items() 
                           if k not in ['__name__', 'replica', 'instance', 'job', 'model_name']}
            
            if name not in metrics_map:
                metrics_map[name] = {}
            if replica not in metrics_map[name]:
                metrics_map[name][replica] = []
                
            metrics_map[name][replica].append((val, other_labels))
        return metrics_map
    except Exception:
        return None

def generate_dashboard():
    metrics_map = fetch_metrics()
    
    table = Table(show_header=True, header_style="bold cyan", expand=True, border_style="dim")
    table.add_column("Category", style="bold white", width=25)
    table.add_column("Replica ID", style="cyan", width=25)
    table.add_column("Metric", style="magenta")
    table.add_column("Value", justify="right", style="green")
    table.add_column("Extra", style="italic dim")

    if not metrics_map:
        table.add_row("⚠️ ERROR", "", "[red]No Data Found[/]", "0", "Check Prometheus targets")
        return table

    # Get a unique list of all replicas present in the data
    all_replicas = sorted(list(set(rep for m in metrics_map.values() for rep in m.keys())))

    for category, metric_list in CATEGORIES.items():
        first_cat_row = True
        
        for replica in all_replicas:
            first_rep_row = True
            
            for m_name in metric_list:
                if m_name in metrics_map and replica in metrics_map[m_name]:
                    for val, labels in metrics_map[m_name][replica]:
                        display_name = m_name.replace("vllm:", "").replace("_seconds", "s")
                        label_str = ", ".join([f"{k}={v}" for k, v in labels.items()])
                        
                        table.add_row(
                            category if first_cat_row else "",
                            f"🆔 {replica[:]}" if first_rep_row else "",
                            display_name,
                            str(val),
                            label_str
                        )
                        first_cat_row = False
                        first_rep_row = False
            
            # Add a subtle divider between pods within the same category
            table.add_row("", "", "", "", "") 
            
        table.add_section() # Heavy divider between categories

    return table

def main():
    with Live(generate_dashboard(), refresh_per_second=2, screen=True) as live:
        try:
            while True:
                time.sleep(REFRESH_RATE)
                live.update(generate_dashboard())
        except KeyboardInterrupt:
            pass

if __name__ == "__main__":
    main()