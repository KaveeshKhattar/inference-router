#!/usr/bin/env python3
"""Generate TTFT comparison bar chart PNG from experiment CSVs."""

from __future__ import annotations

import argparse
import json
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent


def main() -> None:
    parser = argparse.ArgumentParser(description="Plot p95 TTFT comparison chart.")
    parser.add_argument("--ids", nargs="+", required=True, help="Experiment ids to compare")
    parser.add_argument("--metric", choices=["p50", "p95", "p99"], default="p95")
    parser.add_argument("--out", default="load-testing/results/ttft-comparison.png")
    args = parser.parse_args()

    try:
        import matplotlib.pyplot as plt
    except ImportError as exc:
        raise SystemExit("matplotlib required: pip install matplotlib") from exc

    from summarize_results import load_csv, quantile, mean

    with open(ROOT / "load-testing" / "experiments.json", encoding="utf-8") as f:
        cfg = json.load(f)

    defaults = cfg.get("defaults", {})
    results_dir = ROOT / defaults.get("results_dir", "load-testing/results")
    exp_map = {e["id"]: e for e in cfg.get("experiments", [])}

    names: list[str] = []
    values: list[float] = []
    for exp_id in args.ids:
        exp = exp_map.get(exp_id)
        if not exp:
            raise SystemExit(f"unknown experiment: {exp_id}")
        csv_path = results_dir / exp.get("csv", f"exp-{exp_id}.csv")
        s = load_csv(
            csv_path,
            exp.get("name", exp_id),
            exp_id,
            exp.get("talk", ""),
            exp.get("rps", defaults.get("rps", 15)),
        )
        if not s.exists:
            raise SystemExit(f"missing CSV: {csv_path}")
        if args.metric == "avg":
            val = mean(s.ttfb)
        else:
            q = {"p50": 0.5, "p95": 0.95, "p99": 0.99}[args.metric]
            val = quantile(s.ttfb, q)
        names.append(s.name)
        values.append(val)
    bars = ax.bar(names, values, color=["#4C72B0", "#55A868", "#C44E52", "#8172B3"][: len(names)])
    ax.set_ylabel(f"TTFT {args.metric} (seconds)")
    ax.set_title(f"Router strategy comparison — TTFT {args.metric}")
    ax.bar_label(bars, labels=[f"{v:.2f}s" for v in values])
    plt.xticks(rotation=15, ha="right")
    plt.tight_layout()

    out = ROOT / args.out
    out.parent.mkdir(parents=True, exist_ok=True)
    fig.savefig(out, dpi=150)
    print(f"Wrote {out}")


if __name__ == "__main__":
    main()
