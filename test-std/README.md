# test-std — Nolang Standard Library Tests

This directory contains functional tests for the standard library in `src/std/`.
Each active test file is a standalone Nolang program that imports the assertion
helpers in `/test/runner`. Assertion failures are counted and reported in the
program's final summary.

Compiler regressions belong in `tests/`, not here. See the
[compiler test guide](../test/README.md) for that suite.

## Layout

```text
test-std/
└── src/                       # 138 .no files: 130 active tests + 8 ignored repros
    ├── arr.no
    ├── float.no
    ├── vec.no
    └── ...

test/
├── runner.no                  # shared assertion helpers and global counters
├── assert.no                  # legacy bool-returning assertions
└── package.noml               # package name: test

src/std/                       # standard-library sources under test
```

The shared runner remains in `test/`; only the standard-library cases live in
`test-std/src/`. Tests resolve `# test/runner` through the `test` package mapping
in [`workspace.noml`](../workspace.noml), so run commands from the repository
root.

Test filenames normally match the module alias in `src/std/`. For example,
`archive/zip` is covered by `test-std/src/zip.no`, and `collection/arr-stack`
by `test-std/src/arr-stack.no`.

Files whose names begin with `_` are ignored by directory scans. They are retained
as repros or internal experiments and are not part of the default gate.

## Running Tests

Build the repository compiler through `make` before testing. Tests use the
standard library embedded in `bin/no`, not a live view of `src/std/`.

```bash
make no

# LLVM-backed native tests
export PATH="/opt/homebrew/opt/llvm/bin:/Library/Developer/CommandLineTools/usr/bin:$PATH"
./bin/no test -timeout 120 test-std/src/

# One test file
./bin/no test -timeout 120 test-std/src/vec.no
```

`no test` accepts one file or directory argument; passing multiple file paths in
one invocation is unsupported. Run multiple files with separate invocations:

```bash
./bin/no test -timeout 120 test-std/src/vec.no
./bin/no test -timeout 120 test-std/src/str.no
```

The default target when no path is supplied is `tests/`, so always pass the
standard-library path explicitly.

Directory mode recursively discovers `.no` files, excluding `main.no`, `lib.no`,
ignored directories, and `_*.no` files. Each test is compiled and run sequentially.
A failure does not stop later tests, but makes the overall command exit nonzero.
`-timeout` limits each test program's runtime in seconds; a timeout is reported as
a failed file.

## Writing a Test

Start with the structured file header used by the existing tests:

```no
; std-vec.no — vec（切片）模組測試
;
; 執行：./bin/no test -timeout 120 test-std/src/vec.no
; 覆蓋：len / push / pop / contains / index-of
; 未測：known compiler limitation, with a link or repro when available

# test/runner

t-len = () {
    v = [1, 2, 3]
    runner.eq(v.len(), 3, 'vec.len == 3')
}

runner.test('vec.len', t-len)

total = runner.summary()
f = runner.failed-count()
{
    f > 0 -> exit(1)

    -> exit(0)
}
```

The runner already tracks global passed/failed counts. There is no need to
maintain separate `passed` and `failed` accumulators in a new test. End each test
with `runner.summary()` and a nonzero exit when the global failure count is
positive, as shown above.

### Assertion API

| Function | Signature | Purpose |
|---|---|---|
| `runner.eq` | `(a i64, b i64, msg str)` | Integer equality |
| `runner.ne` | `(a i64, b i64, msg str)` | Integer inequality |
| `runner.gt` | `(a i64, b i64, msg str)` | `a > b` |
| `runner.lt` | `(a i64, b i64, msg str)` | `a < b` |
| `runner.is-true` | `(val bool, msg str)` | Boolean value is true |
| `runner.is-false` | `(val bool, msg str)` | Boolean value is false |
| `runner.str-eq` | `(a str, b str, msg str)` | Length-checked string equality |
| `runner.f64-eq` | `(a f64, b f64, eps f64, msg str)` | Absolute tolerance comparison |
| `runner.test` | `(name str, cb callback) (passed i64, failed i64)` | Run a named no-argument callback |
| `runner.failed-count` | `() (n i64)` | Return the global failure count |
| `runner.summary` | `() (total i64)` | Print the summary and return the assertion total |

`callback` is a named function type for `()`. Pass a defined no-argument
function to `runner.test`.

The authoritative runner implementation is
[`test/runner.no`](../test/runner.no). `test/assert.no` is retained for legacy
bool-returning assertions; new tests should use `runner`.

### Language Rules to Keep in Mind

- `;` starts a line comment. It is not a statement separator.
- Put each statement on its own line.
- An index read returns an option. Consume it explicitly with `?=`, `match`, or
  an expression-subject match.
- Arithmetic on ordinary integers can return an option on overflow. When a
  wrapping counter is intended, apply `#{overflow=wrap}` to the statement and
  unwrap the resulting option.
- Do not add a disabled or commented-out test without recording why the case is
  blocked. Preserve a minimal repro when reporting a compiler defect.

For a value-position match, prefer the compact form. The catch-all arm handles
both `nil` and `err` in this example:

```no
mx = a.max(): {
    ok -> it

    -> 0
}
```

### Checklist for a New Module Test

1. Inspect the module source and list its public functions and methods.
2. Copy a small existing test as a structural starting point.
3. Cover normal inputs and meaningful boundaries: empty containers, zero,
   negative values, invalid input, and size limits where relevant.
4. Add an execution command and an explicit coverage/limitations header.
5. Run the file and require `failed: 0`, `ALL TESTS PASSED`, and exit code `0`.
6. Confirm the assertion can fail by temporarily changing an expected value,
   then restore it.
7. Run `./bin/no vet test-std/src/<module>.no` and the full suite before handoff.

For crypto and protocol tests, use authoritative RFC, NIST, FIPS, OpenSSL, or
other independently verified golden values rather than expected values produced
only by the implementation under test.

## Coverage Audit

The inventory script is
[`scripts/std_test_coverage.py`](../scripts/std_test_coverage.py):

```bash
python3 scripts/std_test_coverage.py
python3 scripts/std_test_coverage.py --missing
python3 scripts/std_test_coverage.py --module archive
python3 scripts/std_test_coverage.py --markdown
python3 scripts/std_test_coverage.py --strict
```

The audit compares public declarations in `src/std/**/*.no` with calls in the
corresponding test files. It excludes private symbols, declarations unavailable
on the current host, and a small documented list of structurally unreachable
APIs. Method matching is approximate when several types in one module share a
method name, so the percentage is an inventory signal, not a substitute for
behavioral coverage.

## CI and Acceptance Gates

Before reporting a code change as verified, record the actual results of the
relevant gates rather than copying older snapshots:

```bash
make no
./bin/no vet src/std
./bin/no test -timeout 120 test-std/src/
make test
```

CI configuration is not fully synchronized yet: the PR verification job in
[`build.yml`](../.github/workflows/build.yml) uses `test-std/src/`, while the
`std-test` job in [`test.yml`](../.github/workflows/test.yml) still names
`test-std/src`. Treat a green result from the stale path as inconclusive.

## Known Limitations

Known defects and their minimal repros are tracked in the
[standard-library test notes](../test/README.md). Do not delete failing coverage,
change valid Nolang code to hide a suspected compiler bug, or silently add files
to an ignore list. Report the defect and keep the smallest useful regression
case.
