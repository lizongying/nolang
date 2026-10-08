#!/usr/bin/env python3
"""Compare the in-compiler MIR optimiser against LLVM's `opt -O2`.

WHAT IS BEING COMPARED
----------------------
Two different optimisers that can act on the same program:

  * the compiler's own MIR optimiser (src/mir/opt.go), switched on with
    NOLANG_MIR_OPT / `no build -opt[=N]`, which rewrites the MIR *before*
    codegen;
  * LLVM's `opt`, which the compiler already runs on the emitted IR
    (`opt -O2` by default, NOLANG_OPT_LEVEL to change it).

Both are controlled by the environment, so ONE compiler binary produces every
arm. That is deliberate: the repository carries uncommitted work from other
sessions, so there is no clean baseline to diff against. Holding the binary
fixed and varying only the two switches removes that confound entirely.

ARMS
----
The default set is the 2x2 factorial of (MIR opt off/on) x (LLVM off/-O2):

  mir                raw emitted IR, no MIR opt, no LLVM opt. THE BASELINE.
  mir+mir_opt        raw emitted IR with the MIR optimiser on.
  mir+llvm_O2        opt -O2 applied to `mir`.
  mir_opt+llvm_O2    opt -O2 applied to `mir+mir_opt`.

`mir` vs `mir+mir_opt` isolates the new pass. `mir` vs `mir+llvm_O2` is the
comparison the task named explicitly. `mir+llvm_O2` vs `mir_opt+llvm_O2` is the
pair that actually matters: the marginal value of the MIR pass once a mature
optimiser has already run.

`--extended` adds `mir+llvm_O1`, `mir+llvm_O3` and `mir_opt+llvm_O3`.

METRICS -- THE COMPLETE SET
---------------------------
  IR instruction count   lines that start an LLVM instruction (SSA defs, plus
                         bare terminators/stores/calls). Declarations, globals,
                         type definitions, labels, comments and blank lines are
                         excluded, so the number tracks executable work rather
                         than textual overhead.
  IR size                bytes of the emitted .ll.
  compile time           wall-clock seconds for the whole `no build` (front end
                         + MIR + LLVM opt/llc/cc), so the delta between the two
                         MIR arms is what the pass itself costs.
  binary size            bytes of the linked executable.
  execution time         median of --run N runs of the built program.

WHY INSTRUCTION COUNT AND BYTE SIZE DISAGREE
--------------------------------------------
They measure different things and the arms move them in opposite directions.
`opt -O2` inlines aggressively: the instruction count falls (callee bodies fold
into constants and then die) while the byte size can RISE, because inlined
copies carry their mangled symbol names, attribute groups and debug metadata
into the caller. A single-metric table would hide this, so both are reported
side by side -- which is also why "complete metric set" does not mean "one
number": instruction count alone is a weak proxy once inlining is on.

EXECUTION TIME IS ALSO A CORRECTNESS CHECK
------------------------------------------
Every arm's program is run and its stdout compared against the baseline's. A
runtime number is only reported for programs where all arms AGREED, so a
"faster" arm that computes something different can never be quoted as a win.
Programs that do not finish, do not build, are not executables (the JS backend
emits .js), or that disagree are reported as skipped, never as equal.

FAIRNESS LIMIT -- READ BEFORE QUOTING NUMBERS
---------------------------------------------
This compares two optimisers at very different levels of maturity over a
pipeline where they are NOT peers:

  * `mir` is the compiler's own unoptimised SSA IR, freshly lowered from HIR;
  * `opt -O2` is a mature mid-level optimiser with ~200 passes (inlining, GVN,
    LICM, loop unrolling, SROA, ...) running after codegen has already done its
    own lowering.

So the table answers "does the new pass catch the same class of waste, and how
far does the gap close", NOT "is the new pass better than LLVM". A large
`mir` -> `mir+llvm_O2` gap is expected and is not a defect of the new pass. The
honest reading of the `mir_opt+llvm_O*` rows is the marginal value: how much
IR the MIR pass removes that LLVM's pipeline had not already removed on its own.

USAGE
-----
    python3 scripts/miropt_vs_llvm.py                    # whole corpus, level 2
    python3 scripts/miropt_vs_llvm.py --extended         # add the O1/O3 arms
    python3 scripts/miropt_vs_llvm.py --no-run           # IR + compile time only
    python3 scripts/miropt_vs_llvm.py --list files.txt   # a subset
    python3 scripts/miropt_vs_llvm.py --json out.json    # machine-readable dump

Requires `no` (the compiler) and the LLVM tools (opt/llc/cc) on PATH.
"""
import argparse
import concurrent.futures as cf
import json
import os
import re
import statistics
import subprocess
import sys
import tempfile
import time

# A line starts an LLVM instruction if it begins with an SSA definition
# (`%x = ...`) or with one of the bare mnemonics that take no result.
INSTR = re.compile(
    r'^\s*(?:'
    r'%[-\w.$"\']+\s*=\s*'
    r'|ret\b|br\b|store\b|call\b|switch\b|unreachable\b|invoke\b|resume\b'
    r'|fence\b|atomicrmw\b|cmpxchg\b|indirectbr\b|tail\b|catchswitch\b'
    r'|cleanupret\b|catchret\b|landingpad\b|freeze\b'
    r')'
)
DEFINE = re.compile(r'^define\b')
DECLARE = re.compile(r'^declare\b')

# The pass reports what it did on stderr when NOLANG_MIR_OPT_STATS is set:
#   [miropt] level=2 funcs=3 folded=5 (arith=2 cmp=3) ident=1 copies=4 dead=6
#            cfg{condbr=3 unreachable=4(5 insts) threaded=1 merged=5}
STATS = re.compile(
    r'\[miropt\]\s+level=(\d+)\s+funcs=(\d+)\s+'
    r'folded=(\d+)\s+\(arith=(\d+)\s+cmp=(\d+)\)\s+'
    r'ident=(\d+)\s+copies=(\d+)\s+dead=(\d+)\s+'
    r'cfg\{condbr=(\d+)\s+unreachable=(\d+)\((\d+)\s+insts\)\s+'
    r'threaded=(\d+)\s+merged=(\d+)\}'
)
STATS_KEYS = ['level', 'funcs', 'folded', 'arith', 'cmp', 'ident', 'copies',
              'dead', 'condbr', 'unreachable', 'unreachable_insts', 'threaded',
              'merged']

DEFAULT_ROOTS = ['tests', 'test', 'example', 'bench']

# (name, uses MIR opt?, NOLANG_OPT_LEVEL, which .ll to measure)
ARM_FAST = [
    ('mir',              False, '-O0', 'raw'),
    ('mir+mir_opt',      True,  '-O0', 'raw'),
    ('mir+llvm_O2',      False, '-O2', 'opt'),
    ('mir_opt+llvm_O2',  True,  '-O2', 'opt'),
]
ARM_EXTENDED_EXTRA = [
    ('mir+llvm_O1',      False, '-O1', 'opt'),
    ('mir+llvm_O3',      False, '-O3', 'opt'),
    ('mir_opt+llvm_O3',  True,  '-O3', 'opt'),
]
# Report order: baseline, the MIR pass alone, LLVM alone, then the combination.
ARM_ORDER = ['mir', 'mir+mir_opt',
             'mir+llvm_O1', 'mir+llvm_O2', 'mir+llvm_O3',
             'mir_opt+llvm_O2', 'mir_opt+llvm_O3']


def count_ir(path):
    """Return (instructions, functions, declared, bytes), or None if unreadable."""
    n = fn = decl = 0
    try:
        with open(path, errors='replace') as fh:
            for line in fh:
                if not line.strip() or line.lstrip().startswith(';'):
                    continue
                if DEFINE.match(line):
                    fn += 1
                elif DECLARE.match(line):
                    decl += 1
                if INSTR.match(line):
                    n += 1
    except OSError:
        return None
    return n, fn, decl, os.path.getsize(path)


def build_arm(no_bin, src, out, mir_level, llvm_level):
    """Run one `no build` with the two switches set; return (proc, seconds)."""
    env = dict(os.environ)
    for k in ('NOLANG_MIR_OPT', 'NOLANG_MIR_OPT_STATS', 'NOLANG_OPT_LEVEL'):
        env.pop(k, None)
    if mir_level is not None:
        env['NOLANG_MIR_OPT'] = str(mir_level)
        env['NOLANG_MIR_OPT_STATS'] = '1'
    env['NOLANG_OPT_LEVEL'] = llvm_level
    t0 = time.perf_counter()
    try:
        p = subprocess.run([no_bin, 'build', '-v', '-o', out, src],
                           stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                           env=env, timeout=900)
    except subprocess.TimeoutExpired:
        return None, time.perf_counter() - t0
    return p, time.perf_counter() - t0


def run_binary(path, runs, timeout):
    """Run a built program `runs` times; return (rc, stdout, median_seconds).

    None when the program is not something we may run at all.
    """
    if not os.path.exists(path) or not os.access(path, os.X_OK):
        return None
    times = []
    first = None
    for _ in range(runs):
        t0 = time.perf_counter()
        try:
            p = subprocess.run([path], stdin=subprocess.DEVNULL,
                               stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                               timeout=timeout)
        except (subprocess.TimeoutExpired, OSError):
            return None
        times.append(time.perf_counter() - t0)
        if first is None:
            first = (p.returncode, p.stdout)
        elif (p.returncode, p.stdout) != first:
            # Non-deterministic output: the timing is still meaningful but the
            # equality oracle is not, so mark it and let the caller decide.
            return ('NONDET', first[1], statistics.median(times))
    return (first[0], first[1], statistics.median(times))


def measure_one(job):
    """Build one program once per arm and collect every metric."""
    idx, no_bin, src, level, arms, workdir, runs, timeout = job
    key = re.sub(r'[^\w.-]', '_', src).replace('.no', '')
    res = {'src': src, 'arms': {}}

    # Rotate the arm order per program. The first build of a program pays the
    # cold page-cache cost of reading its source and every header it includes;
    # with a fixed order that penalty would land on the same arm every time and
    # show up as a fake compile-time advantage for the others.
    if arms:
        k = idx % len(arms)
        arms = arms[k:] + arms[:k]

    for name, use_mir, llvm_level, which in arms:
        out = os.path.join(workdir, f'{key}.{name}.bin')
        proc, secs = build_arm(no_bin, src, out,
                               level if use_mir else None, llvm_level)
        entry = {'rc': None if proc is None else proc.returncode,
                 'compile_s': secs}
        if proc is not None and proc.returncode == 0:
            raw, opt = out + '.ll', out + '_opt.ll'
            chosen = raw if which == 'raw' else opt
            c = count_ir(chosen) if os.path.exists(chosen) else None
            if c:
                entry['instr'], entry['funcs'], entry['decls'], entry['bytes'] = c
            if os.path.exists(out):
                entry['bin_bytes'] = os.path.getsize(out)
            if use_mir:
                m = STATS.search(proc.stderr.decode(errors='replace'))
                if m:
                    entry['stats'] = dict(zip(STATS_KEYS,
                                              (int(x) for x in m.groups())))
            if runs > 0:
                r = run_binary(out, runs, timeout)
                if r is None:
                    entry['run'] = 'skipped'
                elif r[0] == 'NONDET':
                    entry['run'] = 'nondeterministic'
                    entry['run_s'] = r[2]
                    entry['stdout_sha'] = hash_bytes(r[1])
                else:
                    entry['run'] = 'ok' if r[0] == 0 else f'rc={r[0]}'
                    entry['run_s'] = r[2]
                    entry['stdout_sha'] = hash_bytes(r[1])
        elif proc is not None:
            entry['error'] = (proc.stderr.decode(errors='replace').strip()
                              .splitlines() or ['?'])[-1][:200]
        res['arms'][name] = entry
    return res


def hash_bytes(b):
    import hashlib
    return hashlib.sha256(b).hexdigest()[:16]


def human(n):
    for unit in ('B', 'KB', 'MB', 'GB'):
        if abs(n) < 1024 or unit == 'GB':
            return f'{n:.2f}{unit}' if unit != 'B' else f'{n}B'
        n /= 1024.0


def secs(n):
    if n >= 60:
        return f'{n / 60:.2f}m'
    if n >= 1:
        return f'{n:.2f}s'
    return f'{n * 1000:.0f}ms'


def main():
    ap = argparse.ArgumentParser(
        description=__doc__,
        formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument('--no', default='./bin/no', help='path to the `no` compiler')
    ap.add_argument('--level', default='2',
                    help='NOLANG_MIR_OPT level for the mir_opt arms')
    ap.add_argument('--roots', nargs='+', default=DEFAULT_ROOTS,
                    help=f'directories to walk for *.no (default: {" ".join(DEFAULT_ROOTS)})')
    ap.add_argument('--list', help='file with one .no path per line (overrides --roots)')
    ap.add_argument('--extended', action='store_true',
                    help='also build the -O1 / -O3 arms')
    ap.add_argument('--jobs', type=int, default=6)
    ap.add_argument('--run', type=int, default=3, metavar='N',
                    help='run each program N times and take the median (0 = do not run)')
    ap.add_argument('--timeout', type=float, default=10.0,
                    help='per-run timeout in seconds')
    ap.add_argument('--workdir', default=None, help='where to keep the artifacts')
    ap.add_argument('--json', default=None, help='write the raw per-file results here')
    a = ap.parse_args()

    if a.list:
        srcs = [l.strip() for l in open(a.list) if l.strip()]
    else:
        srcs = discover(a.roots)
    if not srcs:
        print('no inputs found', file=sys.stderr)
        return 2

    arms = list(ARM_FAST)
    if a.extended:
        arms += ARM_EXTENDED_EXTRA
    workdir = a.workdir or tempfile.mkdtemp(prefix='miropt_')
    os.makedirs(workdir, exist_ok=True)

    jobs = [(i, a.no, s, a.level, arms, workdir, a.run, a.timeout)
            for i, s in enumerate(srcs)]
    rows = []
    t0 = time.perf_counter()
    with cf.ThreadPoolExecutor(max_workers=a.jobs) as ex:
        for r in ex.map(measure_one, jobs):
            rows.append(r)
    wall = time.perf_counter() - t0

    ok = [r for r in rows if r['arms'].get('mir', {}).get('rc') == 0]
    failed = [r for r in rows if r not in ok]

    present = [x for x in ARM_ORDER if any(x in r['arms'] for r in ok)]

    def total(arm, field):
        return sum(r['arms'][arm].get(field, 0) for r in ok if arm in r['arms'])

    def nfiles(arm, field):
        return sum(1 for r in ok if field in r['arms'].get(arm, {}))

    print()
    print(f'programs: {len(ok)}/{len(rows)} built   MIR opt level {a.level}   '
          f'wall {secs(wall)}')
    if failed:
        print(f'  {len(failed)} did not build; first few:')
        for r in failed[:5]:
            e = r['arms'].get('mir', {}).get('error', '?')
            print(f'    {r["src"]}: {e}')
    print()

    base_i = total('mir', 'instr') or 1
    base_b = total('mir', 'bytes') or 1
    base_c = total('mir', 'compile_s') or 1
    base_z = total('mir', 'bin_bytes') or 1

    hdr = (f'{"arm":<18}{"IR instr":>12}{"vs mir":>9}'
           f'{"IR size":>12}{"vs mir":>9}'
           f'{"compile":>11}{"vs mir":>9}'
           f'{"binary":>11}')
    print(hdr)
    print('-' * len(hdr))
    for arm in present:
        i, b = total(arm, 'instr'), total(arm, 'bytes')
        c, z = total(arm, 'compile_s'), total(arm, 'bin_bytes')
        tag = '  <- baseline' if arm == 'mir' else ''
        print(f'{arm:<18}{i:>12,}{100.0 * (i - base_i) / base_i:>8.2f}%'
              f'{human(b):>12}{100.0 * (b - base_b) / base_b:>8.2f}%'
              f'{secs(c):>11}{100.0 * (c - base_c) / base_c:>8.2f}%'
              f'{human(z):>11}{tag}')
    print()

    # ── marginal value: the number the fairness caveat says to read ──────────
    def marg(a_arm, b_arm):
        if a_arm not in present or b_arm not in present:
            return None
        out = []
        for f in ('instr', 'bytes', 'compile_s'):
            av, bv = total(a_arm, f), total(b_arm, f)
            out.append(100.0 * (bv - av) / (av or 1))
        return out

    print('marginal value of the MIR pass (what it removes that the other side had not):')
    for pair in (('mir', 'mir+mir_opt'),
                 ('mir+llvm_O2', 'mir_opt+llvm_O2'),
                 ('mir+llvm_O3', 'mir_opt+llvm_O3')):
        m = marg(*pair)
        if m:
            print(f'  {pair[0]:<16} -> {pair[1]:<16} '
                  f'instr {m[0]:+.2f}%   size {m[1]:+.2f}%   compile {m[2]:+.2f}%')
    print()

    # ── per-program distribution: a total can be carried by a few huge files ──
    def dist(a_arm, b_arm, field='instr'):
        down = same = up = 0
        for r in ok:
            x, y = r['arms'].get(a_arm, {}), r['arms'].get(b_arm, {})
            if field not in x or field not in y:
                continue
            d = y[field] - x[field]
            if d < 0:
                down += 1
            elif d > 0:
                up += 1
            else:
                same += 1
        return down, same, up

    for a_arm, b_arm in (('mir', 'mir+mir_opt'), ('mir', 'mir+llvm_O2')):
        if b_arm in present:
            d, s, u = dist(a_arm, b_arm)
            print(f'{a_arm} -> {b_arm}, per program (IR instructions): '
                  f'shrunk={d} unchanged={s} grew={u}')
    print()

    # ── what the pass actually did, summed ──────────────────────────────────
    st = [r['arms'][arm]['stats'] for r in ok for arm in ('mir+mir_opt',)
          if 'stats' in r['arms'].get(arm, {})]
    if st:
        print('MIR pass activity, summed over all programs:')
        for k in ('funcs', 'folded', 'arith', 'cmp', 'ident', 'copies', 'dead',
                  'condbr', 'unreachable', 'unreachable_insts', 'threaded', 'merged'):
            print(f'  {k:<20}{sum(s[k] for s in st):>12,}')
        print()

    # ── execution time, and the correctness oracle that guards it ───────────
    if a.run > 0:
        report_execution(ok, present, a)

    print(f'artifacts kept in: {workdir}')
    if a.json:
        json.dump(rows, open(a.json, 'w'), indent=1)
        print(f'raw results: {a.json}')
    return 0


def report_execution(ok, present, a):
    print('execution time (median of %d runs; only programs where every arm '
          'produced identical stdout):' % a.run)
    base = 'mir'
    skipped = disagree = 0
    per_arm = {arm: [] for arm in present}
    detail = []
    for r in ok:
        arms = r['arms']
        if 'run_s' not in arms.get(base, {}):
            skipped += 1
            continue
        # Require EVERY built arm to have run and agreed. Comparing only the
        # subset that happened to run would let an arm that crashed pass as
        # "agreeing by absence".
        shas = {}
        missing = False
        for arm in present:
            if arm not in arms:
                continue
            if 'stdout_sha' not in arms[arm]:
                missing = True
                break
            shas[arm] = arms[arm]['stdout_sha']
        if missing:
            skipped += 1
            continue
        if len(set(shas.values())) != 1:
            disagree += 1
            detail.append((r['src'], shas))
            continue
        for arm in present:
            if 'run_s' in arms.get(arm, {}):
                per_arm[arm].append(arms[arm]['run_s'])

    if disagree:
        print(f'  !! {disagree} program(s) produced DIFFERENT output in different '
              f'arms — excluded from the timing table:')
        for src, shas in detail[:10]:
            print(f'     {src}: {shas}')
    for arm in present:
        ts = per_arm[arm]
        if not ts:
            continue
        tot = sum(ts)
        print(f'  {arm:<18}n={len(ts):<6}total {secs(tot):>10}   '
              f'median {secs(statistics.median(ts)):>9}')
    print()
    print(f'  runnable and agreeing: {len(per_arm[base])}   '
          f'skipped (no binary / timeout / nondeterministic): {skipped}   '
          f'disagreeing: {disagree}')
    if len(per_arm[base]) == 0:
        print('  !! nothing was executed — the timing table above proves nothing')
    print()


def discover(roots):
    """Collect *.no under each root, sorted, relative to the repo root."""
    out = []
    for r in roots:
        if not os.path.isdir(r):
            continue
        for dirpath, dirnames, filenames in os.walk(r):
            dirnames[:] = sorted(d for d in dirnames if d != '.git')
            for f in sorted(filenames):
                if f.endswith('.no'):
                    out.append(os.path.join(dirpath, f))
    return out


if __name__ == '__main__':
    sys.exit(main())
