#!/usr/bin/env python3
"""MD5 benchmark with trimmed mean (15 runs, trim 3 from each end)"""
import time, subprocess, os

ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__)))))
BINS = {
    'Nolang': os.path.join(ROOT, 'test', 'dist', 'md5-bench'),
    'Go':     os.path.join(ROOT, 'test', 'bench', 'md5', 'go', 'md5-bench_go'),
    'Rust':   os.path.join(ROOT, 'test', 'bench', 'md5', 'rust', 'target', 'release', 'md5-bench'),
}

def bench(path, runs=15):
    times = []
    for _ in range(runs):
        s = time.perf_counter()
        subprocess.run([path], capture_output=True)
        elapsed = (time.perf_counter() - s) * 1000
        times.append(elapsed)
    times.sort()
    trimmed = times[3:-3]
    avg = sum(trimmed) / len(trimmed)
    return avg, min(times), max(times)

print(f"{'Language':<10} {'Avg(ms)':<10} {'Min(ms)':<10} {'Max(ms)':<10}")
print('-' * 45)
results = {}
for name, path in BINS.items():
    avg, mn, mx = bench(path)
    results[name] = avg
    print(f"{name:<10} {avg:<10.1f} {mn:<10.1f} {mx:<10.1f}")

rust = results.get('Rust', 1)
nolang = results.get('Nolang', 1)
go = results.get('Go', 1)
print()
print(f"Nolang / Rust  = {nolang/rust:.2f}x")
print(f"Go / Rust      = {go/rust:.2f}x")
print(f"Nolang / Go    = {nolang/go:.2f}x")
