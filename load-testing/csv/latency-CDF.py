import pandas as pd
import numpy as np
import matplotlib.pyplot as plt

rr = pd.read_csv("results-rr-rps6.csv")
qa = pd.read_csv("results-qa-rps6.csv")

fig, ax = plt.subplots(figsize=(8, 4))
for df, label, color in [(rr, "Round Robin", "#378ADD"), (qa, "Queue Aware", "#1D9E75")]:
    ok = df[df["ok"] == 1]["latency_s"].sort_values()
    ax.plot(ok, np.linspace(0, 1, len(ok)), label=label, color=color, lw=2)

ax.set_xlabel("Latency (s)")
ax.set_ylabel("CDF")
ax.set_title("Latency CDF — RR vs QA")
ax.legend()
ax.grid(True, alpha=0.3)
plt.tight_layout()
plt.savefig("cdf.png", dpi=150)

plt.show()

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

fig, ax = plt.subplots(figsize=(8, 4))
bins = np.linspace(0, 70, 50)

ax.hist(rr_ok, bins=bins, alpha=0.6, label="Round Robin", color="#378ADD")
ax.hist(qa_ok, bins=bins, alpha=0.6, label="Queue Aware",  color="#1D9E75")
ax.set_xlabel("Latency (s)")
ax.set_ylabel("Count")
ax.set_title("Latency distribution — RR vs QA")
ax.legend()
ax.grid(True, axis="y", alpha=0.3)
plt.tight_layout()
plt.savefig("histogram.png", dpi=150)



plt.show()