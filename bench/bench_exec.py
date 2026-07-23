#!/usr/bin/env python3
"""Benchmark all Nolang benchmarks: compare no execution time vs C.
Pre-compiles both, then measures only execution time."""
import time, subprocess, os, sys

NO_BIN = '/Users/lizongying/IdeaProjects/no/bin/no'
BENCH_ROOT = '/Users/lizongying/IdeaProjects/benchmarks-no/benchmarks'
TMP_DIR = '/tmp/no-bench'

os.makedirs(TMP_DIR, exist_ok=True)

# (name, c_binary_path, no_source_path, input_path_or_None, args_or_None)
BENCHMARKS = [
    ('binary-trees',  'binary-trees/c/binary-trees',  'binary-trees/nolang/main.no',  None, '10'),
    ('chameneos-redux','chameneos-redux/c/chameneos-redux','chameneos-redux/nolang/main.no', None, '1000'),
    ('fannkuch-redux','fannkuch-redux/c/fannkuch-redux','fannkuch-redux/nolang/main.no', None, '8'),
    ('fasta',         'fasta/c/fasta',                'fasta/nolang/main.no',         None, '1000'),
    ('k-nucleotide',  'k-nucleotide/c/k-nucleotide',  'k-nucleotide/nolang/main.no',  'k-nucleotide/inputs/knucleotide.txt', None),
    ('mandelbrot',    'mandelbrot/c/mandelbrot',      'mandelbrot/nolang/main.no',    None, '1000'),
    ('meteor-contest', 'meteor-contest/c/meteor-contest','meteor-contest/nolang/main.no', None, None),
    ('n-body',        'n-body/c/n-body',              'n-body/nolang/main.no',        None, '1000'),
    ('pidigits',      'pidigits/c/pidigits',          'pidigits/nolang/main.no',      None, '50'),
    ('regex-redux',   'regex-redux/c/regex-redux',    'regex-redux/nolang/main.no',   'regex-redux/inputs/regex.txt', None),
    ('reverse-complement','reverse-complement/c/reverse-complement','reverse-complement/nolang/main.no','reverse-complement/inputs/revcomp.txt', None),
    ('spectral-norm', 'spectral-norm/c/spectral-norm','spectral-norm/nolang/main.no', None, '1000'),
    ('thread-ring',   'thread-ring/c/thread-ring',    'thread-ring/nolang/main.no',   None, '1000'),
]


def run_cmd(cmd, input_data=None, cwd=None):
    try:
        stdin = subprocess.PIPE if input_data is not None else None
        p = subprocess.run(cmd, input=input_data, stdout=subprocess.PIPE, stderr=subprocess.PIPE, cwd=cwd)
        return p.returncode, p.stdout, p.stderr
    except Exception as e:
        return -1, b'', str(e).encode()


def bench_one(cmd, input_data=None, cwd=None, runs=30):
    times = []
    last_out = b''
    for _ in range(runs):
        s = time.perf_counter()
        rc, out, err = run_cmd(cmd, input_data, cwd)
        elapsed = (time.perf_counter() - s) * 1000
        times.append(elapsed)
        last_out = out
        if rc != 0:
            return None, None, rc, out, err
    times.sort()
    trimmed = times[5:-5] if len(times) > 10 else times[1:-1]
    avg = sum(trimmed) / len(trimmed) if trimmed else 0
    mn = min(times)
    return avg, mn, 0, last_out, b''


# Phase 1: Pre-compile all no binaries
print("=== Phase 1: Pre-compiling ===")
no_bins = {}
for name, c_rel, no_rel, inp_rel, args in BENCHMARKS:
    no_src = os.path.join(BENCH_ROOT, no_rel)
    no_cwd = os.path.dirname(no_src)
    out_bin = os.path.join(TMP_DIR, name)
    print(f"  Compiling {name}...", end=' ', flush=True)
    rc, out, err = run_cmd([NO_BIN, 'build', '-o', out_bin, 'main.no'], cwd=no_cwd)
    if rc == 0:
        no_bins[name] = out_bin
        print("OK")
    else:
        print(f"FAIL: {err.decode(errors='replace')[:100]}")

# Phase 2: Benchmark execution
print(f"\n{'Benchmark':<20} {'C avg(ms)':<12} {'no avg(ms)':<14} {'C/no':<8} {'match':<8}")
print('-' * 70)

results = []
for name, c_rel, no_rel, inp_rel, args in BENCHMARKS:
    c_path = os.path.join(BENCH_ROOT, c_rel)
    input_data = None
    if inp_rel:
        with open(os.path.join(BENCH_ROOT, inp_rel), 'rb') as f:
            input_data = f.read()
    arg_list = args.split() if args else []

    if name not in no_bins:
        print(f"{name:<20} {'SKIP':<12} {'(compile failed)':<14}")
        continue

    # Bench C
    c_cmd = [c_path] + arg_list
    c_avg, c_min, c_rc, c_out, c_err = bench_one(c_cmd, input_data)
    if c_avg is None:
        print(f"{name:<20} C FAIL rc={c_rc}")
        continue

    # Bench no
    no_cmd = [no_bins[name]] + arg_list
    no_avg, no_min, no_rc, no_out, no_err = bench_one(no_cmd, input_data)
    if no_avg is None:
        print(f"{name:<20} {c_avg:<12.1f} {'FAIL rc='+str(no_rc):<14}")
        continue

    match = 'OK' if c_out == no_out else 'DIFF'
    ratio = c_avg / no_avg if no_avg > 0 else float('inf')
    print(f"{name:<20} {c_avg:<12.1f} {no_avg:<14.1f} {ratio:<8.3f} {match:<8}")
    results.append((name, c_avg, no_avg, ratio, match))

print()
print("Summary:")
slow = [(n, c, no, r, m) for n, c, no, r, m in results if r < 1.0 and m == 'OK']
fast = [(n, c, no, r, m) for n, c, no, r, m in results if r >= 1.0 and m == 'OK']
print(f"  no slower than C: {len(slow)} benchmarks")
print(f"  no faster than C: {len(fast)} benchmarks")
if slow:
    print("Slowest (highest optimization opportunity):")
    for n, c, no, r, m in sorted(slow, key=lambda x: x[3])[:5]:
        print(f"  {n}: C={c:.1f}ms no={no:.1f}ms ratio={r:.3f}")
