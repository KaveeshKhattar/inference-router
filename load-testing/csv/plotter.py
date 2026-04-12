import pandas as pd
import matplotlib.pyplot as plt
import numpy as np

# qa = pd.read_csv("load-testing/results-qa.csv")
# rr = pd.read_csv("load-testing/results-rr.csv")

# qa_lat = qa[qa["ok"] == 1]["latency_s"]
# rr_lat = rr[rr["ok"] == 1]["latency_s"]

# def cdf(data):
#     x = np.sort(data)
#     y = np.arange(len(x)) / len(x)
#     return x, y

# x1, y1 = cdf(qa_lat)
# x2, y2 = cdf(rr_lat)

# plt.plot(x1, y1, label="Queue Aware")
# plt.plot(x2, y2, label="Round Robin")

# plt.xlabel("Latency (seconds)")
# plt.ylabel("CDF")
# plt.title("Latency Distribution")
# plt.legend()
# plt.grid()
# plt.show()


# qa = pd.read_csv("load-testing/results-qa.csv")
# rr = pd.read_csv("load-testing/results-rr.csv")

# qa_success = qa["ok"].mean() * 100
# rr_success = rr["ok"].mean() * 100

# plt.bar(["Queue Aware", "Round Robin"], [qa_success, rr_success])

# plt.ylabel("Success Rate (%)")
# plt.title("Request Success Rate")
# plt.show()


# qa = pd.read_csv("load-testing/results-qa.csv")

# qa["time_index"] = range(len(qa))

# plt.scatter(qa["time_index"], qa["latency_s"], s=3)

# plt.xlabel("Request Order")
# plt.ylabel("Latency (seconds)")
# plt.title("Latency Growth Over Time")
# plt.show()

qa = pd.read_csv("load-testing/results-qa.csv")

plt.scatter(qa["prompt_tokens"], qa["latency_s"], s=5)

plt.xlabel("Prompt Tokens")
plt.ylabel("Latency (seconds)")
plt.title("Latency vs Prompt Size")
plt.show()