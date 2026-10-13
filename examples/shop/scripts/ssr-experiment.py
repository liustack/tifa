#!/usr/bin/env python3
"""Run VM lifetime policies in separate processes, retaining raw JSON evidence."""

import argparse
import json
import os
from pathlib import Path
import random
import subprocess
import tempfile
import time


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--suite", choices=["perf", "soak", "control", "all"], default="all")
    parser.add_argument("--rounds", type=int, default=3)
    parser.add_argument("--requests", type=int, default=512)
    parser.add_argument("--soak-requests", type=int, default=10000)
    parser.add_argument("--output", default=".issues/2026-10-13-prototype/ssr-results")
    args = parser.parse_args()
    root = Path(__file__).resolve().parents[1]
    output = root / args.output
    output.mkdir(parents=True, exist_ok=True)
    runs = []
    if args.suite in ("perf", "all"):
        for round_number in range(1, args.rounds + 1):
            modes = ["source", "bytecode", "reuse", "sync", "async", "single"]
            random.Random(round_number).shuffle(modes)
            for mode in modes:
                runs.append((f"perf-{mode}-{round_number}", {
                    "MODE": mode, "REQUESTS": args.requests, "CLIENTS": 2,
                    "WORKERS": 2, "SPARES": 1, "MAX_USES": 64, "SAMPLE_EVERY": 0,
                }))
    if args.suite in ("soak", "all"):
        for mode, requests in [("reuse", args.soak_requests), ("async", args.soak_requests), ("bytecode", 1000)]:
            runs.append((f"soak-{mode}", {
                "MODE": mode, "REQUESTS": requests, "CLIENTS": 2,
                "WORKERS": 2, "SPARES": 1, "MAX_USES": 64, "SAMPLE_EVERY": min(1000, requests),
            }))
    if args.suite in ("control", "all"):
        for mode in ["reuse", "async"]:
            runs.append((f"control-{mode}", {
                "MODE": mode, "REQUESTS": 192, "CLIENTS": 1, "WORKERS": 1,
                "SPARES": 1, "MAX_USES": 32, "SAMPLE_EVERY": 32, "SCENARIO": "retained",
            }))
    summaries = []
    with tempfile.TemporaryDirectory(prefix="go-quick-ssr-") as temporary:
        binary = Path(temporary) / "ssrbench.test"
        subprocess.run(["go", "test", "-c", "-tags", "memory.counters", "-o", str(binary), "./internal/ssr"], cwd=root, check=True)
        for name, config in runs:
            env = {key: value for key, value in os.environ.items() if not key.startswith("SSR_")}
            env.update({"SSR_" + key: str(value) for key, value in config.items()})
            print(f"START {name} {config}", flush=True)
            start = time.monotonic()
            result = subprocess.run([str(binary), "-test.run", "^TestSSRExperiment$", "-test.v", "-test.timeout", "10m"], cwd=root, env=env, capture_output=True, text=True)
            (output / f"{name}.log").write_text(result.stdout + result.stderr)
            events = [json.loads(line) for line in result.stdout.splitlines() if line.startswith("{")]
            (output / f"{name}.jsonl").write_text("".join(json.dumps(event, ensure_ascii=False) + "\n" for event in events))
            summary = next((event for event in events if event["event"] == "summary"), {})
            summaries.append({"run": name, "config": config, "exit_code": result.returncode, "process_wall_s": time.monotonic()-start, **summary})
            (output / f"{args.suite}-summary.json").write_text(json.dumps(summaries, ensure_ascii=False, indent=2) + "\n")
            if result.returncode:
                print(result.stdout + result.stderr, flush=True)
                raise SystemExit(f"FAILED {name}, evidence in {output}")
            print(f"DONE {name}: p50={summary['p50_ms']:.2f}ms p95={summary['p95_ms']:.2f}ms "
                  f"rate={summary['requests_per_s']:.1f}/s CPU={summary['cpu_ms_per_request']:.2f}ms/request "
                  f"native delta after close={summary['native_allocation_delta_after_close']}", flush=True)


if __name__ == "__main__":
    main()
