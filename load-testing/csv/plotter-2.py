import pandas as pd
import numpy as np
import matplotlib.pyplot as plt

rr = pd.read_csv("load-testing/results-rr-long-prompts-rps6.csv")
qa = pd.read_csv("load-testing/results-qa-long-prompts-rps6.csv")

RR_COLOR = "#378ADD"
QA_COLOR = "#1D9E75"


fig, ax = plt.subplots(figsize=(5, 4))
strategies = ["Round Robin", "Queue Aware"]
rates = [
    rr["ok"].sum() / len(rr) * 100,
    qa["ok"].sum() / len(qa) * 100,
]
bars = ax.bar(strategies, rates, color=[RR_COLOR, QA_COLOR], width=0.4)
ax.set_ylabel("Success rate (%)")
ax.set_ylim(0, 100)
ax.set_title("Success rate — RR vs QA")
for bar, rate in zip(bars, rates):
    ax.text(bar.get_x() + bar.get_width()/2, bar.get_height() + 1,
            f"{rate:.1f}%", ha="center", fontsize=11, fontweight="bold")
ax.grid(True, axis="y", alpha=0.3)
plt.tight_layout()
plt.savefig("load-testing/success-rate.png", dpi=150)
plt.show()



fig, ax = plt.subplots(figsize=(8, 4))
for df, label, color in [(rr, "Round Robin", RR_COLOR), (qa, "Queue Aware", QA_COLOR)]:
    latencies = df["latency_s"].sort_values()
    cdf = np.linspace(0, 1, len(latencies))
    ax.plot(latencies, cdf, label=label, color=color, lw=2)

ax.axvline(120, color="red", linestyle="--", alpha=0.5, label="120s timeout")
ax.set_xlabel("Latency (s)")
ax.set_ylabel("CDF")
ax.set_title("Latency CDF — RR vs QA (long prompts, 6 RPS)")
ax.legend()
ax.grid(True, alpha=0.3)
plt.tight_layout()
plt.savefig("load-testing/cdf.png", dpi=150)
plt.show()



fig, ax = plt.subplots(figsize=(8, 4))
for df, label, color in [(rr, "Round Robin", RR_COLOR), (qa, "Queue Aware", QA_COLOR)]:
    df_sorted = df.sort_values("req_id").reset_index(drop=True)
    cumulative_errors = (df_sorted["ok"] == 0).cumsum()
    ax.plot(df_sorted["req_id"], cumulative_errors, label=label, color=color, lw=2)

ax.set_xlabel("Request ID (proxy for arrival order)")
ax.set_ylabel("Cumulative errors")
ax.set_title("Error accumulation — RR vs QA")
ax.legend()
ax.grid(True, alpha=0.3)
plt.tight_layout()
plt.savefig("load-testing/cumulative-errors.png", dpi=150)
plt.show()


fig, axes = plt.subplots(1, 2, figsize=(10, 4), sharey=True)
bins = np.linspace(0, 125, 50)

for ax, df, label, color in [
    (axes[0], rr, "Round Robin", RR_COLOR),
    (axes[1], qa, "Queue Aware", QA_COLOR),
]:
    ax.hist(df["latency_s"], bins=bins, color=color, alpha=0.85, edgecolor="none")
    ax.axvline(120, color="red", linestyle="--", alpha=0.6, label="120s timeout")
    ax.set_title(label)
    ax.set_xlabel("Latency (s)")
    ax.legend()
    ax.grid(True, axis="y", alpha=0.3)

axes[0].set_ylabel("Count")
fig.suptitle("Latency distribution — long prompts, 6 RPS", y=1.02)
plt.tight_layout()
plt.savefig("load-testing/histogram.png", dpi=150)
plt.show()