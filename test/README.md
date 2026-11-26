# test

`test` is a lightweight testing library for Nolang projects. It provides two complementary modules:

- `runner`: assertions with shared pass/fail counters, named test callbacks, and summary output.
- `assert`: assertions that return `ok bool`, so callers can choose how to handle failures.

The library does not discover test files or set process exit codes by itself. Use `no test` to build and execute test files, and make each test file exit nonzero when an assertion fails.

## Package layout

```text
test/
├── assert.no       ; bool-returning assertions
├── runner.no       ; counter-based assertions and test callbacks
├── package.noml
└── README.md
```

## Import

When working inside this repository, the package is already registered in `workspace.noml`:

```no
# test/runner
```

```no
# test/assert
```

In another workspace, add the source package to `package.noml`:

```noml
dependencies: [
  'github.com/lizongying/nolang/test',
]
```

Then import the modules by their full package path:

```no
# github.com/lizongying/nolang/test/assert
# github.com/lizongying/nolang/test/runner
```

Import only the modules used by the test file.

## Quick start with runner

`runner` keeps process-wide counters. Every runner assertion records either one pass or one failure and continues execution.

```no
# test/runner

t-basic = () {
    runner.eq(2, 2, 'integer equality')
    runner.ne(2, 3, 'integer inequality')
    runner.gt(3, 2, 'greater than')
    runner.lt(1, 2, 'less than')
    runner.is-true(true, 'true value')
    runner.is-false(false, 'false value')
    runner.str-eq('nolang', 'nolang', 'string equality')
    runner.f64-eq(1.5, 1.5, 0.0001, 'float equality')
}

runner.test('basic assertions', t-basic)
runner.summary()

failed = runner.failed-count()
{
    failed > 0 -> os.exit(1)

    -> os.exit(0)
}
```

Save an executable test file as `tests/basic.no` in your package, then run it from the package root (the directory containing `package.noml`):

```bash
no test tests/basic.no
```

Run every test file in `tests/` from the package root:

```bash
no test tests/
```

A successful run ends with:

```text
'TEST:' 'basic assertions'
'--- Test Summary ---'
'passed:' 8
'failed:' 0
'total:' 8
'ALL TESTS PASSED'
```

The final exit-code check is required: a failed runner assertion prints `FAIL:` but does not terminate the process.

### runner API

| Function | Signature | Behavior |
|---|---|---|
| `runner.eq` | `(a i64, b i64, msg str)` | Passes when `a == b`. |
| `runner.ne` | `(a i64, b i64, msg str)` | Passes when `a != b`. |
| `runner.gt` | `(a i64, b i64, msg str)` | Passes when `a > b`. |
| `runner.lt` | `(a i64, b i64, msg str)` | Passes when `a < b`. |
| `runner.is-true` | `(val bool, msg str)` | Passes when `val` is true. |
| `runner.is-false` | `(val bool, msg str)` | Passes when `val` is false. |
| `runner.str-eq` | `(a str, b str, msg str)` | Compares byte lengths and then the complete strings. |
| `runner.f64-eq` | `(a f64, b f64, eps f64, msg str)` | Passes when the absolute difference is less than `eps`. |
| `runner.test` | `(name str, cb callback) (passed i64, failed i64)` | Prints the case name, invokes a no-argument callback, and returns the case's pass/fail deltas. |
| `runner.failed-count` | `() (n i64)` | Returns the total failure count. |
| `runner.summary` | `() (total i64)` | Prints passed, failed, and total counts; returns the total. |

`callback` is a named function type with no parameters and no results. Define a named function rather than passing an anonymous function:

```no
t-example = () {
    runner.eq(1, 1, 'named callback')
}

passed, failed = runner.test('example', t-example)
```

Counters are shared by all imports in the same executable. Separate test files are separate executables and therefore have separate counters.

## Quick start with assert

`assert` returns `ok bool` from every assertion. A false result means the assertion failed; the function prints the failure and its message but does not exit.

```no
# test/assert

check-equality = () (ok bool) {
    ok = assert.eq(1, 1, 'one equals one')
}

ok = check-equality()
{
    ! ok -> os.exit(1)

    -> os.exit(0)
}
```

This style is useful inside helper functions that need to return a status:

```no
# test/assert

check-positive = (value i64) (ok bool) {
    ok = assert.gt(value, 0, 'value is positive')
}

ok = check-positive(1)
{
    ! ok -> os.exit(1)

    -> os.exit(0)
}
```

### assert API

| Function | Signature | Behavior |
|---|---|---|
| `assert.eq` | `(a t, b t, msg str) (ok bool)` | Generic equality assertion. |
| `assert.ne` | `(a t, b t, msg str) (ok bool)` | Generic inequality assertion. |
| `assert.gt` | `(a t, b t, msg str) (ok bool)` | Passes when `a > b`. |
| `assert.ge` | `(a t, b t, msg str) (ok bool)` | Passes when `a >= b`. |
| `assert.lt` | `(a t, b t, msg str) (ok bool)` | Passes when `a < b`. |
| `assert.le` | `(a t, b t, msg str) (ok bool)` | Passes when `a <= b`. |
| `assert.is-true` | `(val bool, msg str) (ok bool)` | Passes when `val` is true. |
| `assert.is-false` | `(val bool, msg str) (ok bool)` | Passes when `val` is false. |
| `assert.f64-eq` | `(a f64, b f64, eps f64, msg str) (ok bool)` | Passes when the absolute difference is less than `eps`. |
| `assert.f64-ne` | `(a f64, b f64, eps f64, msg str) (ok bool)` | Passes when the absolute difference is at least `eps`. |
| `assert.str-eq` | `(a str, b str, msg str) (ok bool)` | Compares byte lengths and then the complete strings. |
| `assert.str-ne` | `(a str, b str, n i64, msg str) (ok bool)` | Compares the first `n` bytes; `n` must not exceed either string's byte length. |
| `assert.str-empty` | `(s str, msg str) (ok bool)` | Passes when the string is empty. |
| `assert.str-not-empty` | `(s str, msg str) (ok bool)` | Passes when the string is nonempty. |
| `assert.is-nil` | `(p i64, msg str) (ok bool)` | Passes when the pointer value represented by `p` is zero. |
| `assert.not-nil` | `(p i64, msg str) (ok bool)` | Passes when the pointer value represented by `p` is nonzero. |

Generic assertions require both operands to have the same type and support the corresponding comparison operator.

## Combining the modules

Use `assert` for a detailed check and feed its result into `runner` when the check belongs in a counted test case:

```no
# test/assert
# test/runner

t-strings = () {
    ok = assert.str-not-empty('nolang', 'string is not empty')
    runner.is-true(ok, 'assert result is true')
}

runner.test('strings', t-strings)
runner.summary()

failed = runner.failed-count()
{
    failed > 0 -> os.exit(1)

    -> os.exit(0)
}
```

An `assert` call alone does not update runner counters.

## Test-file conventions

- Put executable test files under `tests/`; keep imported libraries outside that directory.
- Use one statement per line. In Nolang, `;` starts a comment and is not a statement separator.
- Give each test case a descriptive name and assertion message.
- Check `runner.failed-count()` or the relevant `assert` result before exiting.
- Use exit code `0` for success and `1` for assertion failure.
- Handle fallible values explicitly with `?=` or `match`; do not ignore an `option` result.
- Integer arithmetic can return an option on overflow. Apply `#{overflow=wrap}` only when wrapping is the intended behavior, and unwrap the result before passing it to an assertion.

## Related package

The Nolang standard-library test suite is maintained separately in [`test-std`](../test-std/). This package contains only the reusable testing utilities documented above.
