import argparse
import asyncio
import csv
import os
import random
import statistics
import time
from dataclasses import dataclass

import aiohttp

DEFAULT_URL = "http://localhost:9000/v1/chat/completions"
DEFAULT_MODEL = "meta-llama/Llama-3.1-8B-Instruct"

VOCAB = [
    "latency",
    "throughput",
    "context",
    "token",
    "router",
    "replica",
    "queue",
    "batch",
    "scheduler",
    "decode",
    "prefill",
    "attention",
]


def sample_prompt_tokens() -> int:
    # Mixture distribution: mostly short prompts, with medium/long tail.
    p = random.random()
    if p < 0.20:
        return random.randint(8, 64)
    if 0.20 < p < 0.32:
        return random.randint(65, 512)
    return random.randint(513, 2048)


def sample_max_tokens() -> int:
    # Shorter generations dominate, but allow occasional longer responses.
    p = random.random()
    if p < 0.70:
        return random.randint(16, 128)
    if 0.70 < p < 0.85:
        return random.randint(129, 384)
    return random.randint(385, 1024)


def generate_prompt(token_count: int) -> str:
    words = [random.choice(VOCAB) for _ in range(token_count)]
    return " ".join(words)


@dataclass
class RequestResult:
    req_id: int
    prompt_tokens: int
    max_tokens: int
    status: int
    latency_s: float
    ttfb_s: float
    ok: bool
    error: str


class Recorder:
    def __init__(self) -> None:
        self.rows: list[RequestResult] = []

    def add(self, row: RequestResult) -> None:
        self.rows.append(row)

    def _quantile(self, values: list[float], q: float) -> float:
        if not values:
            return 0.0
        values_sorted = sorted(values)
        idx = min(len(values_sorted) - 1, int(round((len(values_sorted) - 1) * q)))
        return values_sorted[idx]

    def print_summary(self) -> None:
        total = len(self.rows)
        ok = sum(1 for r in self.rows if r.ok)
        err = total - ok
        if total == 0:
            print("No requests recorded.")
            return

        latencies = [r.latency_s for r in self.rows if r.ok]
        ttfbs = [r.ttfb_s for r in self.rows if r.ok and r.ttfb_s >= 0]
        print("\n=== Load Test Summary ===")
        print(f"requests_total={total} success={ok} error={err} success_rate={100.0*ok/total:.2f}%")
        if latencies:
            print(
                "latency_s "
                f"p50={self._quantile(latencies, 0.50):.3f} "
                f"p95={self._quantile(latencies, 0.95):.3f} "
                f"p99={self._quantile(latencies, 0.99):.3f} "
                f"avg={statistics.mean(latencies):.3f}"
            )
        if ttfbs:
            print(
                "ttfb_s "
                f"p50={self._quantile(ttfbs, 0.50):.3f} "
                f"p95={self._quantile(ttfbs, 0.95):.3f} "
                f"p99={self._quantile(ttfbs, 0.99):.3f} "
                f"avg={statistics.mean(ttfbs):.3f}"
            )

    def write_csv(self, path: str) -> None:
        with open(path, "w", newline="", encoding="utf-8") as f:
            writer = csv.writer(f)
            writer.writerow(
                [
                    "req_id",
                    "prompt_tokens",
                    "max_tokens",
                    "status",
                    "latency_s",
                    "ttfb_s",
                    "ok",
                    "error",
                ]
            )
            for r in self.rows:
                writer.writerow(
                    [
                        r.req_id,
                        r.prompt_tokens,
                        r.max_tokens,
                        r.status,
                        f"{r.latency_s:.6f}",
                        f"{r.ttfb_s:.6f}",
                        int(r.ok),
                        r.error,
                    ]
                )


@dataclass
class RuntimeStats:
    started: int = 0
    completed: int = 0
    success: int = 0
    errors: int = 0


async def send_request(
    session: aiohttp.ClientSession,
    url: str,
    model: str,
    timeout_s: float,
    req_id: int,
    prompt_tokens: int,
    max_tokens: int,
    recorder: Recorder,
    stats: RuntimeStats,
    verbose: bool,
) -> None:
    stats.started += 1
    payload = {
        "model": model,
        "messages": [{"role": "user", "content": generate_prompt(prompt_tokens)}],
        "max_tokens": max_tokens,
        "stream": True
    }

    started = time.perf_counter()
    try:
        timeout = aiohttp.ClientTimeout(total=timeout_s)
        async with session.post(url, json=payload, timeout=timeout) as resp:
            first_byte_started = time.perf_counter()
            first_chunk = await resp.content.read(1)
            ttfb_s = time.perf_counter() - started
            if first_chunk:
                await resp.read()
            ended = time.perf_counter()

            recorder.add(
                RequestResult(
                    req_id=req_id,
                    prompt_tokens=prompt_tokens,
                    max_tokens=max_tokens,
                    status=resp.status,
                    latency_s=ended - started,
                    ttfb_s=ttfb_s,
                    ok=200 <= resp.status < 300,
                    error="",
                )
            )
            stats.completed += 1
            if 200 <= resp.status < 300:
                stats.success += 1
            else:
                stats.errors += 1
            if verbose:
                print(
                    "request "
                    f"id={req_id} status={resp.status} prompt_tokens={prompt_tokens} "
                    f"max_tokens={max_tokens} latency_s={ended - started:.3f} ttfb_s={ttfb_s:.3f}"
                )
    except Exception as exc:
        ended = time.perf_counter()
        recorder.add(
            RequestResult(
                req_id=req_id,
                prompt_tokens=prompt_tokens,
                max_tokens=max_tokens,
                status=0,
                latency_s=ended - started,
                ttfb_s=-1.0,
                ok=False,
                error=str(exc),
            )
        )
        stats.completed += 1
        stats.errors += 1
        if verbose:
            print(
                "request "
                f"id={req_id} status=error prompt_tokens={prompt_tokens} max_tokens={max_tokens} "
                f"latency_s={ended - started:.3f} error={exc}"
            )


async def progress_reporter(
    phase_name: str,
    stats: RuntimeStats,
    start_time: float,
    interval_s: float,
    stop: asyncio.Event,
) -> None:
    while not stop.is_set():
        await asyncio.sleep(interval_s)
        elapsed = max(time.perf_counter() - start_time, 0.001)
        started_rps = stats.started / elapsed
        completed_rps = stats.completed / elapsed
        print(
            f"[{phase_name}] elapsed={elapsed:.1f}s "
            f"started={stats.started} completed={stats.completed} "
            f"success={stats.success} errors={stats.errors} "
            f"started_rps={started_rps:.2f} completed_rps={completed_rps:.2f}"
        )


async def run_phase(
    session: aiohttp.ClientSession,
    url: str,
    model: str,
    phase_name: str,
    rps: float,
    duration_s: int,
    timeout_s: float,
    poisson: bool,
    max_in_flight: int,
    recorder: Recorder,
    progress_interval_s: float,
    verbose: bool,
) -> None:
    print(f"Starting phase={phase_name} rps={rps} duration_s={duration_s} poisson={poisson}")
    loop = asyncio.get_running_loop()
    phase_start = loop.time()
    next_dispatch = phase_start
    req_id = 0
    in_flight: set[asyncio.Task] = set()
    semaphore = asyncio.Semaphore(max_in_flight)
    stats = RuntimeStats()
    phase_wall_start = time.perf_counter()
    stop_progress = asyncio.Event()
    reporter_task = asyncio.create_task(
        progress_reporter(
            phase_name=phase_name,
            stats=stats,
            start_time=phase_wall_start,
            interval_s=progress_interval_s,
            stop=stop_progress,
        )
    )

    async def launch_one(local_req_id: int) -> None:
        async with semaphore:
            prompt_tokens = sample_prompt_tokens()
            max_tokens = sample_max_tokens()
            await send_request(
                session=session,
                url=url,
                model=model,
                timeout_s=timeout_s,
                req_id=local_req_id,
                prompt_tokens=prompt_tokens,
                max_tokens=max_tokens,
                recorder=recorder,
                stats=stats,
                verbose=verbose,
            )

    while loop.time() - phase_start < duration_s:
        now = loop.time()
        if now < next_dispatch:
            await asyncio.sleep(next_dispatch - now)

        task = asyncio.create_task(launch_one(req_id))
        in_flight.add(task)
        task.add_done_callback(in_flight.discard)
        req_id += 1

        if poisson:
            next_dispatch += random.expovariate(rps)
        else:
            next_dispatch += 1.0 / rps

    if in_flight:
        await asyncio.gather(*in_flight)

    stop_progress.set()
    await reporter_task
    elapsed = max(time.perf_counter() - phase_wall_start, 0.001)
    print(
        f"Finished phase={phase_name} elapsed={elapsed:.1f}s "
        f"started={stats.started} completed={stats.completed} success={stats.success} errors={stats.errors}"
    )


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Open-loop load generator for inference router.")
    parser.add_argument("--url", default=DEFAULT_URL, help="Router endpoint URL")
    parser.add_argument("--model", default=DEFAULT_MODEL, help="Model name sent in payload")
    parser.add_argument("--rps", type=float, default=1, help="Target requests/sec")
    parser.add_argument("--duration-seconds", type=int, default=180, help="Measurement duration")
    parser.add_argument("--warmup-seconds", type=int, default=30, help="Warmup duration")
    parser.add_argument("--timeout-seconds", type=float, default=120.0, help="Per request timeout")
    parser.add_argument("--max-in-flight", type=int, default=256, help="Backpressure cap on in-flight requests")
    parser.add_argument("--deterministic", action="store_true", help="Use fixed inter-arrival (not Poisson)")
    parser.add_argument("--csv-out", default="load-testing/results.csv", help="Output CSV path")
    parser.add_argument("--progress-interval", type=float, default=5.0, help="Progress print interval seconds")
    parser.add_argument("--verbose", action="store_true", help="Print one line per request")
    return parser.parse_args()


async def main() -> None:
    args = parse_args()
    random.seed(42)
    os.makedirs(os.path.dirname(args.csv_out) or ".", exist_ok=True)
    print(
        "Config "
        f"url={args.url} model={args.model} rps={args.rps} warmup_s={args.warmup_seconds} "
        f"duration_s={args.duration_seconds} max_in_flight={args.max_in_flight} "
        f"poisson={not args.deterministic} progress_interval={args.progress_interval} verbose={args.verbose}"
    )

    warmup_recorder = Recorder()
    measure_recorder = Recorder()

    async with aiohttp.ClientSession() as session:
        if args.warmup_seconds > 0:
            await run_phase(
                session=session,
                url=args.url,
                model=args.model,
                phase_name="warmup",
                rps=args.rps,
                duration_s=args.warmup_seconds,
                timeout_s=args.timeout_seconds,
                poisson=not args.deterministic,
                max_in_flight=args.max_in_flight,
                recorder=warmup_recorder,
                progress_interval_s=args.progress_interval,
                verbose=args.verbose,
            )

        await run_phase(
            session=session,
            url=args.url,
            model=args.model,
            phase_name="measure",
            rps=args.rps,
            duration_s=args.duration_seconds,
            timeout_s=args.timeout_seconds,
            poisson=not args.deterministic,
            max_in_flight=args.max_in_flight,
            recorder=measure_recorder,
            progress_interval_s=args.progress_interval,
            verbose=args.verbose,
        )

    print("\nWarmup summary:")
    warmup_recorder.print_summary()
    print("\nMeasurement summary:")
    measure_recorder.print_summary()
    measure_recorder.write_csv(args.csv_out)
    print(f"Wrote results to {args.csv_out}")


if __name__ == "__main__":
    asyncio.run(main())
