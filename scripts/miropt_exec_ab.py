#!/usr/bin/env python3
"""Execution A/B for the in-compiler MIR optimiser: NOLANG_MIR_OPT off vs on.

WHY THIS EXISTS
---------------
`scripts/miropt_vs_llvm.py` measures IR size and proves the optimiser does not
break the build. It does NOT prove the optimiser preserves BEHAVIOUR — a pass
can emit valid IR that computes the wrong answer. This harness closes that gap:
it builds each program twice, runs both, and compares (exit code, sha256 of
stdout).

WHY IT IS A SAME-BINARY A/B
---------------------------
The optimiser sits behind an environment switch, so both arms run the SAME
executable and nothing is rebuilt. That removes two confounds at once: no build
non-determinism, and no risk of accidentally comparing against another
work-in-progress change in the tree.

HOW IT REFUSES TO LIE
---------------------
A harness like this fails silently in a predictable way: if nothing runs, every
comparison trivially "matches" and the result looks like a clean pass. So:

  * a control run that does not complete normally (build failure, timeout, or an
    output that is not a native executable) is reported as `skipped`, NEVER as
    "same";
  * the number of control runs that actually exited 0 is printed as a
    false-pass guard, and a zero is called out loudly;
  * the count of control runs with EMPTY stdout is printed too, because two
    empty outputs also compare equal.

A program that runs but legitimately produces no output is fine; the point is
that you must be able to see how many of the "same" verdicts are vacuous.

USAGE
-----
    # whole corpus (skips the network/async files that would block)
    python3 scripts/miropt_exec_ab.py --no ./bin/no --level 2

    # an explicit list, more parallelism, a shorter per-program run timeout
    python3 scripts/miropt_exec_ab.py --no ./bin/no --list /tmp/compute.txt \
        --jobs 8 --timeout 20

Exit status is non-zero if any program behaved differently.
"""
import argparse
import concurrent.futures as cf
import hashlib
import os
import re
import subprocess
import sys
import tempfile
from collections import Counter

EMPTY = hashlib.sha256(b"").hexdigest()
DEFAULT_ROOTS = ['tests', 'test', 'example', 'bench']

# Programs that bind a socket, wait on a child, or talk to a service cannot be
# compared by stdout: they block or need infrastructure that is not there.
SKIP = re.compile(
    r'server|http|tls|socket|dns|udp|tcp|mysql|sqlite|database|'
    r'process|watch|serve|listen|async|spawn|thread|signal|'
    r'stdin|repl|playground',
    re.I,
)

# Verdicts that mean "this side produced nothing comparable".
NOT_RUNNABLE = ('BUILDFAIL', 'BUILD-TIMEOUT', 'RUN-TIMEOUT',
                'NOT-EXECUTABLE', 'RUN-OSERROR')


def discover(roots):
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


def run_one(job):
    no_bin, src, level, run_timeout = job
    d = tempfile.mkdtemp(prefix='miropt_ab_')
    res = {}
    for side, lvl in (('ctl', ''), ('opt', str(level))):
        env = dict(os.environ)
        env.pop('NOLANG_MIR_OPT', None)
        if lvl:
            env['NOLANG_MIR_OPT'] = lvl
        out = os.path.join(d, side + '.bin')
        try:
            p = subprocess.run([no_bin, 'build', '-o', out, src],
                               stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                               env=env, timeout=600)
        except subprocess.TimeoutExpired:
            res[side] = ('BUILD-TIMEOUT',)
            continue
        if p.returncode != 0 or not os.path.exists(out):
            res[side] = ('BUILDFAIL', p.returncode)
            continue
        if not os.access(out, os.X_OK):
            # Some inputs target a non-native back end (the JS backend writes a
            # .js file, not a binary), so there is nothing to execute.
            res[side] = ('NOT-EXECUTABLE',)
            continue
        try:
            r = subprocess.run([out], stdout=subprocess.PIPE,
                               stderr=subprocess.PIPE, stdin=subprocess.DEVNULL,
                               timeout=run_timeout)
        except subprocess.TimeoutExpired:
            res[side] = ('RUN-TIMEOUT',)
            continue
        except OSError as e:
            res[side] = ('RUN-OSERROR', type(e).__name__)
            continue
        res[side] = (r.returncode, hashlib.sha256(r.stdout).hexdigest()[:16],
                     len(r.stdout))
    return src, res


def main():
    ap = argparse.ArgumentParser(
        description=__doc__,
        formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument('--no', default='./bin/no', help='path to the `no` compiler')
    ap.add_argument('--level', default='2', help='NOLANG_MIR_OPT level to test')
    ap.add_argument('--roots', nargs='+', default=DEFAULT_ROOTS)
    ap.add_argument('--list', help='file with one .no path per line (overrides --roots)')
    ap.add_argument('--jobs', type=int, default=4)
    ap.add_argument('--timeout', type=int, default=20,
                    help='per-program RUN timeout in seconds')
    ap.add_argument('--all', action='store_true',
                    help='do not skip network/async programs (expect timeouts)')
    a = ap.parse_args()

    srcs = [l.strip() for l in open(a.list) if l.strip()] if a.list else discover(a.roots)
    if not a.all:
        srcs = [s for s in srcs if not SKIP.search(s)]
    if not srcs:
        print('no inputs found', file=sys.stderr)
        return 2

    jobs = [(a.no, s, a.level, a.timeout) for s in srcs]
    rows = []
    with cf.ThreadPoolExecutor(max_workers=a.jobs) as ex:
        for src, res in ex.map(run_one, jobs):
            rows.append((src, res))

    same = diff = skip = 0
    ctl_ok = ctl_empty = 0
    diffs, skips = [], []
    for src, res in rows:
        c, o = res.get('ctl'), res.get('opt')
        if not c or c[0] in NOT_RUNNABLE:
            skip += 1
            skips.append((src, c, o))
            continue
        if not o or o[0] in NOT_RUNNABLE:
            # The control ran; the optimised build did not. That IS a difference.
            diff += 1
            diffs.append((src, c, o))
            continue
        if c[0] == 0:
            ctl_ok += 1
        if c[1] == EMPTY:
            ctl_empty += 1
        if c[:2] == o[:2]:
            same += 1
        else:
            diff += 1
            diffs.append((src, c, o))

    print(f'files={len(rows)} same={same} diff={diff} skipped={skip} '
          f'(MIR opt level {a.level})')
    print(f'FALSE-PASS GUARD: control exited 0 on {ctl_ok}/{len(rows)}; '
          f'control stdout empty on {ctl_empty}/{len(rows)}')
    if ctl_ok == 0:
        print('!! HARNESS BROKEN: nothing ran successfully on the control side, '
              'so every "same" above is vacuous')
    for src, c, o in diffs:
        print(f'  DIFF {src}\n       ctl={c}\n       opt={o}')
    if skips:
        why = Counter((s[1][0] if s[1] else 'None') for s in skips)
        print(f'  skipped: {dict(why)}')
        for src, c, o in skips[:10]:
            print(f'    {src}: ctl={c} opt={o}')
    return 1 if diffs else 0


if __name__ == '__main__':
    sys.exit(main())
