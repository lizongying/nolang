#!/usr/bin/env python3
"""Benchmark all Nolang benchmarks: compare no vs C performance."""
import time, subprocess, os, sys

NO_BIN = '/Users/lizongying/IdeaProjects/no/bin/no'
BENCH_ROOT = '/Users/lizongying/IdeaProjects/benchmarks-no/benchmarks'

# (name, c_binary_path, no_source_path, input_path_or_None, args_or_None, expected_output_file_or_None)
BENCHMARKS = [
    ('binary-trees',  'binary-trees/c/binary-trees',  'binary-trees/nolang/main.no',  None, '10', None),
    ('chameneos-redux','chameneos-redux/c/chameneos-redux','chameneos-redux/nolang/main.no', None, '1000', None),
    ('fannkuch-redux','fannkuch-redux/c/fannkuch-redux','fannkuch-redux/nolang/main.no', None, '8', None),
    ('fasta',         'fasta/c/fasta',                'fasta/nolang/main.no',         None, '1000', None),
    ('k-nucleotide',  'k-nucleotide/c/k-nucleotide',  'k-nucleotide/nolang/main.no',  'k-nucleotide/inputs/knucleotide.txt', None, None),
    ('mandelbrot',    'mandelbrot/c/mandelbrot',      'mandelbrot/nolang/main.no',    None, '1000', None),
    ('meteor-contest', 'meteor-contest/c/meteor-contest','meteor-contest/nolang/main.no', None, None, None),
    ('n-body',        'n-body/c/n-body',              'n-body/nolang/main.no',        None, '1000', None),
    ('pidigits',      'pidigits/c/pidigits',          'pidigits/nolang/main.no',      None, '50', None),
    ('regex-redux',   'regex-redux/c/regex-redux',    'regex-redux/nolang/main.no',   'regex-redux/inputs/regex.txt', None, None),
    ('reverse-complement','reverse-complement/c/reverse-complement','reverse-complement/nolang/main.no','reverse-complement/inputs/revcomp.txt', None, None),
    ('spectral-norm', 'spectral-norm/c/spectral-norm','spectral-norm/nolang/main.no', None, '1000', None),
    ('thread-ring',   'thread-ring/c/thread-ring',    'thread-ring/nolang/main.no',   None, '1000', None),
]


def run_cmd(cmd, input_data=None, cwd=None):
    try:
        stdin = subprocess.PIPE if input_data is not None else None
        p = subprocess.run(cmd, input=input_data, stdout=subprocess.PIPE, stderr=subprocess.PIPE, cwd=cwd)
        return p.returncode, p.stdout, p.stderr
    except Exception as e:
        return -1, b'', str(e).encode()


def bench_one(cmd, input_data=None, cwd=None, runs=5):
    times = []
    for _ in range(runs):
        s = time.perf_counter()
        rc, out, err = run_cmd(cmd, input_data, cwd)
        elapsed = (time.perf_counter() - s) * 1000
        times.append(elapsed)
        if rc != 0:
            return None, None, rc, out, err
    times.sort()
    trimmed = times[1:-1] if len(times) > 2 else times
    avg = sum(trimmed) / len(trimmed) if trimmed else 0
    mn = min(times)
    return avg, mn, 0, out, err


print(f"{'Benchmark':<20} {'C avg(ms)':<12} {'no avg(ms)':<14} {'C/no':<8} {'match':<8}")
print('-' * 70)

results = []
for name, c_rel, no_rel, inp_rel, args, _ in BENCHMARKS:
    c_path = os.path.join(BENCH_ROOT, c_rel)
    no_src = os.path.join(BENCH_ROOT, no_rel)
    no_cwd = os.path.dirname(no_src)

    input_data = None
    if inp_rel:
        with open(os.path.join(BENCH_ROOT, inp_rel), 'rb') as f:
            input_data = f.read()

    arg_list = args.split() if args else []

    # Build C binary if not exists
    if not os.path.exists(c_path):
        print(f"{name:<20} {'SKIP':<12} {'(C bin not built)':<14}")
        continue

    # Bench C
    c_cmd = [c_path] + arg_list
    c_avg, c_min, c_rc, c_out, c_err = bench_one(c_cmd, input_data)
    if c_avg is None:
        print(f"{name:<20} C FAIL rc={c_rc} {c_err[:80]}")
        continue

    # Bench no
    no_cmd = [NO_BIN, 'run', 'main.no'] + arg_list
    no_avg, no_min, no_rc, no_out, no_err = bench_one(no_cmd, input_data, cwd=no_cwd)
    if no_avg is None:
        print(f"{name:<20} {c_avg:<12.1f} {'FAIL rc='+str(no_rc):<14}")
        if no_err:
            print(f"  err: {no_err.decode(errors='replace')[:200]}")
        continue

    # Compare output
    match = 'OK' if c_out == no_out else 'DIFF'
    ratio = c_avg / no_avg if no_avg > 0 else float('inf')
    print(f"{name:<20} {c_avg:<12.1f} {no_avg:<14.1f} {ratio:<8.3f} {match:<8}")
    if match == 'DIFF':
        # Show first diff lines
        c_lines = c_out.decode(errors='replace').splitlines()
        no_lines = no_out.decode(errors='replace').splitlines()
        print(f"  C lines: {len(c_lines)}, no lines: {len(no_lines)}")
        for i in range(min(3, max(len(c_lines), len(no_lines)))):
            cl = c_lines[i] if i < len(c_lines) else '<none>'
            nl = no_lines[i] if i < len(no_lines) else '<none>'
            if cl != nl:
                print(f"  line {i}: C={cl[:60]}")
                print(f"  line {i}: no={nl[:60]}")
                break
    results.append((name, c_avg, no_avg, ratio, match))

print()
print("Summary (no faster means ratio > 1.0):")
slow = [(n, c, no, r, m) for n, c, no, r, m in results if r < 1.0 and m == 'OK']
fast = [(n, c, no, r, m) for n, c, no, r, m in results if r >= 1.0 and m == 'OK']
print(f"  no slower than C: {len(slow)} benchmarks")
print(f"  no faster than C: {len(fast)} benchmarks")
if slow:
    print("Slowest (highest optimization opportunity):")
    for n, c, no, r, m in sorted(slow, key=lambda x: x[3])[:5]:
        print(f"  {n}: C={c:.1f}ms no={no:.1f}ms ratio={r:.3f}")
