#!/usr/bin/env bash
# Layer 14 benchmark suite — runs all experiments sequentially.
# Restart the router with the printed env vars before each experiment.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

echo "Layer 14 benchmark suite"
echo "========================"
echo ""
echo "Prerequisites:"
echo "  - kind cluster with simulator + router running"
echo "  - port-forward: kubectl port-forward svc/inference-router 9000:9000"
echo "  - Python deps: pip install -r load-testing/requirements.txt"
echo ""

EXPERIMENTS=(
  round-robin
  queue-aware
  cache-aware
  admission-off
  cache-admission
  aggregated-long
  disaggregated-long
)

for id in "${EXPERIMENTS[@]}"; do
  echo ""
  echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
  echo "Experiment: $id"
  echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
  python3 load-testing/run_experiment.py --id "$id" --dry-run
  read -r -p "Apply router env above, then press Enter to run load test (or Ctrl-C to skip)... "
  python3 load-testing/run_experiment.py --id "$id"
done

echo ""
echo "Generating RESULTS.md ..."
python3 load-testing/summarize_results.py --write load-testing/results/RESULTS.md
echo "Done. See load-testing/results/RESULTS.md"
