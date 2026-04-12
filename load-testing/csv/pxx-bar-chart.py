import pandas as pd
import numpy as np
import matplotlib.pyplot as plt

fig, ax = plt.subplots(figsize=(7, 4))
quantiles = [0.50, 0.95, 0.99]
labels = ["p50", "p95", "p99"]
x = np.arange(len(labels))
w = 0.35

rr_ok = rr[rr["ok"] == 1]["latency_s"]
qa_ok = qa[qa["ok"] == 1]["latency_s"]

rr_vals = [rr_ok.quantile(q) for q in quantiles]
qa_vals = [qa_ok.quantile(q) for q in quantiles]

ax.bar(x - w/2, rr_vals, w, label="Round Robin", color="#378ADD")
ax.bar(x + w/2, qa_vals, w, label="Queue Aware",  color="#1D9E75")
ax.set_xticks(x); ax.set_xticklabels(labels)
ax.set_ylabel("Latency (s)")
ax.set_title("p50 / p95 / p99 — RR vs QA")
ax.legend()
ax.grid(True, axis="y", alpha=0.3)
plt.tight_layout()
plt.savefig("percentiles.png", dpi=150)
plt.show()