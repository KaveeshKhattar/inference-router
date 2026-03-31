import asyncio
import aiohttp
import random

ROUTER_URL = "http://localhost:9000/v1/chat/completions"

def generate_prompt(length):
    return " ".join(["hello"] * length)

async def send_request(session, prompt_len, req_id):
    payload = {
        "model": "meta-llama/Llama-3.1-8B-Instruct",
        "messages": [
            {"role": "user", "content": generate_prompt(prompt_len)}
        ]
    }
    try:
        async with session.post(ROUTER_URL, json=payload) as resp:
            await resp.text()
            print(f"req={req_id} prompt_len={prompt_len} status={resp.status}")
    except Exception as e:
        print(f"req={req_id} error={e}")

async def main():
    concurrency = 20
    duration_seconds = 120  # run for 2 minutes so grafana captures it

    async with aiohttp.ClientSession() as session:
        req_id = 0
        start = asyncio.get_event_loop().time()

        while asyncio.get_event_loop().time() - start < duration_seconds:
            tasks = []
            for _ in range(concurrency):
                # 80% short, 20% long — skewed workload
                prompt_len = random.choice(
                    [5, 10, 20, 50, 50, 50, 100, 100, 500, 1000]
                )
                # round-robin across replicas — this is the baseline (blind)
                # replica = random.choice(REPLICAS)
                tasks.append(send_request(session, prompt_len, req_id))
                req_id += 1

            await asyncio.gather(*tasks)
            await asyncio.sleep(0.5)  # brief pause between waves

if __name__ == "__main__":
    asyncio.run(main())