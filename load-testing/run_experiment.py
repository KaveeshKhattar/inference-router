#!/usr/bin/env python3
"""Run a single Layer 14 experiment from experiments.yaml."""

from __future__ import annotations

import argparse
import json
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
EXPERIMENTS_FILE = ROOT / "load-testing" / "experiments.json"


def load_config() -> dict:
    with open(EXPERIMENTS_FILE, encoding="utf-8") as f:
        return json.load(f)


def merge_experiment(cfg: dict, exp_id: str) -> dict:
    defaults = cfg.get("defaults", {})
    for exp in cfg.get("experiments", []):
        if exp["id"] == exp_id:
            merged = {**defaults, **{k: v for k, v in exp.items() if k not in ("router_env",)}}
            merged["router_env"] = exp.get("router_env", {})
            merged["id"] = exp_id
            merged["name"] = exp.get("name", exp_id)
            merged["talk"] = exp.get("talk", "")
            merged["baseline"] = exp.get("baseline", False)
            merged["csv"] = exp.get("csv", f"exp-{exp_id}.csv")
            return merged
    raise SystemExit(f"unknown experiment id: {exp_id}")


def print_router_env(env: dict[str, str]) -> None:
    print("\n# Set these env vars on the router before running load:")
    for k, v in sorted(env.items()):
        print(f"export {k}={v!r}")
    print()


def run_load(exp: dict, dry_run: bool) -> Path:
    results_dir = ROOT / exp["results_dir"]
    results_dir.mkdir(parents=True, exist_ok=True)
    csv_path = results_dir / exp["csv"]

    cmd = [
        sys.executable,
        str(ROOT / "load-testing" / "load-generator.py"),
        "--url",
        str(exp["url"]),
        "--model",
        str(exp["model"]),
        "--rps",
        str(exp["rps"]),
        "--duration-seconds",
        str(exp["duration_seconds"]),
        "--warmup-seconds",
        str(exp["warmup_seconds"]),
        "--timeout-seconds",
        str(exp["timeout_seconds"]),
        "--max-in-flight",
        str(exp["max_in_flight"]),
        "--min-prompt-tokens",
        str(exp["min_prompt_tokens"]),
        "--priority-high-fraction",
        str(exp["priority_high_fraction"]),
        "--csv-out",
        str(csv_path),
    ]
    if exp.get("shared_prefix"):
        cmd.append("--shared-prefix")

    meta_path = csv_path.with_suffix(".meta.json")
    meta = {
        "id": exp["id"],
        "name": exp["name"],
        "talk": exp["talk"],
        "router_env": exp.get("router_env", {}),
        "rps": exp["rps"],
        "shared_prefix": exp.get("shared_prefix", False),
        "min_prompt_tokens": exp["min_prompt_tokens"],
        "priority_high_fraction": exp["priority_high_fraction"],
        "csv": str(csv_path.relative_to(ROOT)),
    }

    print(f"Experiment: {exp['name']} ({exp['id']})")
    print(f"Talk: {exp['talk']}")
    print_router_env(exp.get("router_env", {}))
    print("Load command:")
    print(" ", " ".join(cmd))

    if dry_run:
        return csv_path

    subprocess.run(cmd, check=True, cwd=str(ROOT))
    with open(meta_path, "w", encoding="utf-8") as f:
        json.dump(meta, f, indent=2)
    print(f"\nWrote {csv_path}")
    print(f"Wrote {meta_path}")
    return csv_path


def main() -> None:
    parser = argparse.ArgumentParser(description="Run one benchmark experiment.")
    parser.add_argument("--id", default="", help="Experiment id from experiments.json")
    parser.add_argument("--list", action="store_true", help="List experiment ids")
    parser.add_argument("--dry-run", action="store_true", help="Print command without running")
    args = parser.parse_args()

    cfg = load_config()
    if args.list:
        for exp in cfg.get("experiments", []):
            print(f"{exp['id']:20} {exp.get('name', '')}")
        return

    if not args.id:
        parser.error("--id is required unless --list is set")

    exp = merge_experiment(cfg, args.id)
    run_load(exp, dry_run=args.dry_run)


if __name__ == "__main__":
    main()
