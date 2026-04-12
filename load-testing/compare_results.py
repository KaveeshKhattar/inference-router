import argparse
import csv
import math
from dataclasses import dataclass


@dataclass
class Summary:
    name: str
    total: int
    success: int
    errors: int
    success_rate: float
    latency_values: list[float]
    ttfb_values: list[float]


def quantile(values: list[float], q: float) -> float:
    if not values:
        return 0.0
    values_sorted = sorted(values)
    idx = min(len(values_sorted) - 1, int(round((len(values_sorted) - 1) * q)))
    return values_sorted[idx]


def mean(values: list[float]) -> float:
    if not values:
        return 0.0
    return sum(values) / len(values)


def load_csv(path: str, name: str) -> Summary:
    total = 0
    success = 0
    errors = 0
    latency_values: list[float] = []
    ttfb_values: list[float] = []

    with open(path, newline="", encoding="utf-8") as f:
        reader = csv.DictReader(f)
        for row in reader:
            total += 1
            ok = row.get("ok", "0").strip()
            is_ok = ok in ("1", "true", "True")
            if is_ok:
                success += 1
                latency_values.append(float(row["latency_s"]))
                ttfb = float(row["ttfb_s"])
                if ttfb >= 0:
                    ttfb_values.append(ttfb)
            else:
                errors += 1

    success_rate = (100.0 * success / total) if total > 0 else 0.0
    return Summary(
        name=name,
        total=total,
        success=success,
        errors=errors,
        success_rate=success_rate,
        latency_values=latency_values,
        ttfb_values=ttfb_values,
    )


def fmt_seconds(x: float) -> str:
    return f"{x:.3f}s"


def fmt_percent(x: float) -> str:
    return f"{x:.2f}%"


def pct_change(old: float, new: float) -> float:
    if math.isclose(old, 0.0):
        return 0.0
    return ((new - old) / old) * 100.0


def print_summary(s: Summary) -> None:
    lp50 = quantile(s.latency_values, 0.50)
    lp95 = quantile(s.latency_values, 0.95)
    lp99 = quantile(s.latency_values, 0.99)
    lavg = mean(s.latency_values)

    tp50 = quantile(s.ttfb_values, 0.50)
    tp95 = quantile(s.ttfb_values, 0.95)
    tp99 = quantile(s.ttfb_values, 0.99)
    tavg = mean(s.ttfb_values)

    print(f"\n=== {s.name} ===")
    print(
        f"requests_total={s.total} success={s.success} error={s.errors} "
        f"success_rate={fmt_percent(s.success_rate)}"
    )
    print(
        f"latency_s p50={fmt_seconds(lp50)} p95={fmt_seconds(lp95)} "
        f"p99={fmt_seconds(lp99)} avg={fmt_seconds(lavg)}"
    )
    print(
        f"ttfb_s    p50={fmt_seconds(tp50)} p95={fmt_seconds(tp95)} "
        f"p99={fmt_seconds(tp99)} avg={fmt_seconds(tavg)}"
    )


def print_delta(baseline: Summary, candidate: Summary) -> None:
    b_lp95 = quantile(baseline.latency_values, 0.95)
    c_lp95 = quantile(candidate.latency_values, 0.95)
    b_lp99 = quantile(baseline.latency_values, 0.99)
    c_lp99 = quantile(candidate.latency_values, 0.99)
    b_lavg = mean(baseline.latency_values)
    c_lavg = mean(candidate.latency_values)
    b_s = baseline.success_rate
    c_s = candidate.success_rate

    p95_delta = c_lp95 - b_lp95
    p99_delta = c_lp99 - b_lp99
    avg_delta = c_lavg - b_lavg
    success_delta = c_s - b_s

    print("\n=== Candidate vs Baseline ===")
    print(
        f"latency_p95_delta={fmt_seconds(p95_delta)} "
        f"({pct_change(b_lp95, c_lp95):+.2f}%)"
    )
    print(
        f"latency_p99_delta={fmt_seconds(p99_delta)} "
        f"({pct_change(b_lp99, c_lp99):+.2f}%)"
    )
    print(
        f"latency_avg_delta={fmt_seconds(avg_delta)} "
        f"({pct_change(b_lavg, c_lavg):+.2f}%)"
    )
    print(f"success_rate_delta={success_delta:+.2f}pp")
    print("\nInterpretation: negative latency deltas are better.")


def main() -> None:
    parser = argparse.ArgumentParser(description="Compare two load-test CSV outputs.")
    parser.add_argument("--baseline", required=True, help="CSV from baseline run")
    parser.add_argument("--candidate", required=True, help="CSV from candidate run")
    parser.add_argument("--baseline-name", default="baseline")
    parser.add_argument("--candidate-name", default="candidate")
    args = parser.parse_args()

    baseline = load_csv(args.baseline, args.baseline_name)
    candidate = load_csv(args.candidate, args.candidate_name)

    print_summary(baseline)
    print_summary(candidate)
    print_delta(baseline, candidate)


if __name__ == "__main__":
    main()
