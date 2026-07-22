#!/usr/bin/env python3
"""MD5 基準測試 — 毫秒精度對比 Nolang / Go / Rust"""
import time
import subprocess
import os

ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__)))))

BINS = {
    "Nolang": os.path.join(ROOT, "test", "dist", "md5-bench"),
    "Go":     os.path.join(ROOT, "test", "bench", "md5", "go", "md5-bench_go"),
    "Rust":   os.path.join(ROOT, "test", "bench", "md5", "rust", "target", "release", "md5-bench"),
}

def bench(path, runs=10):
    times = []
    for _ in range(runs):
        s = time.perf_counter()
        subprocess.run([path], capture_output=True)
        elapsed = (time.perf_counter() - s) * 1000
        times.append(elapsed)
    times.sort()
    trimmed = times[1:-1]
    avg = sum(trimmed) / len(trimmed)
    return avg, min(times), times

for name, path in BINS.items():
    avg, mn, all_t = bench(path)
    print(f"{name:8s}: avg={avg:.1f}ms  min={mn:.1f}ms  all={['%.1f' % t for t in all_t]}")
