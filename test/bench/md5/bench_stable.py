#!/usr/bin/env python3
import time, subprocess, os
ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__)))))
BINS = {'Nolang': ROOT+'/test/dist/md5-bench', 'Rust': ROOT+'/test/bench/md5/rust/target/release/md5-bench', 'Go': ROOT+'/test/bench/md5/go/md5-bench_go', 'C': ROOT+'/test/bench/md5/c/md5-bench_c'}
def bench(path, runs=30):
    times = []
    for _ in range(runs):
        s = time.perf_counter()
        subprocess.run([path], capture_output=True)
        times.append((time.perf_counter()-s)*1000)
    times.sort()
    trimmed = times[5:-5]
    return sum(trimmed)/len(trimmed), min(times)
print("Lang       Avg        Min")
for name, path in BINS.items():
    avg, mn = bench(path)
    print(f"{name:<10} {avg:<10.1f} {mn:<10.1f}")
