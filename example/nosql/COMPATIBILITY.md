# nosql Compatibility Manifest

## Status

`nosql` is a self-contained Nolang SQLite-compatible engine under development.
This manifest is intentionally explicit: an item is complete only when the listed
evidence exists. The package does not currently claim full SQLite compatibility.

## Canonical oracle

- SQLite version: `3.54.0`
- Canonical executable for fixture generation: `/usr/bin/sqlite3`
- Verified version output:

  ```text
  3.54.0 2026-04-09 12:25:13 8fa8248e303219400c646a885e36dfc52eae33d83f4412e3f369b2be5373aapl (64-bit)
  ```

- `SELECT sqlite_version()` returns `3.54.0`.
- Other SQLite libraries, including the copy bundled with Python, are not the
  oracle for this project.

## Stage 1 foundations

| Area | Current contract | Evidence |
| --- | --- | --- |
| Package | Nolang-only `nosql` package with no dependencies or SQLite link metadata | `package.noml`, `lib.no` |
| Values | Explicit NULL, INTEGER, FLOAT, TEXT, and BLOB variants; typed accessors | `src/value.no`, `tests/value.no` |
| Affinity | Declared-type affinity classification and basic affinity application | `src/value.no`, `tests/value.no` |
| Comparison | SQLite storage-class ordering and BINARY text/blob comparison | `src/value.no`, `tests/value.no` |
| Truth | SQL false, true, and unknown truth values | `src/value.no`, `tests/value.no` |
| Errors | All 105 public SQLite 3.54.0 primary/extended result codes and name lookup | `src/error.no`, `tests/error.no` |
| Structured results | Primary code, extended code, message, and SQL offset | `src/error.no`, `tests/error.no` |
| Strict tests | Counted assertions and nonzero process exit on failure | `src/test-support.no`, `tests/harness.no` |

The result-code lookup accepts names as written in `sqlite3.h` after removing the
`SQLITE_` prefix and replacing `_` with `-`. Unknown, empty, and incorrectly
cased names return `SQLITE_ERROR`. Extended primary codes use `code & 255`;
`SQLITE_OK_LOAD_PERMANENTLY` remains successful.

## Not yet implemented

The following remain incomplete and must not be reported as supported:

- SQL tokenizer, parser, analyzer, planner, VM, and executor.
- Persistent open/query/exec APIs, prepared statements, rows, transactions, and
  metadata APIs.
- Record/page codecs, pager, B-trees, cache, freelist, and overflow pages.
- Rollback journal, savepoints, recovery, locking, and crash safety.
- WAL, shared memory, checkpoints, and multi-process concurrency.
- Schema objects, indexes, views, triggers, generated columns, foreign keys,
  attached databases, and conflict handling.
- Built-in scalar, aggregate, date/time, math, JSON, FTS, and RTree features.
- Durable browser/WASM host VFS and legacy facade cutover.

## Canonical Stage 1 fixture

`tests/fixtures/stage1.sql` exercises the pinned oracle's version reporting,
storage classes, NULL, BLOB bytes, 64-bit integer preservation, ordering, and
declared-type affinity. Its frozen output is `tests/fixtures/stage1.expected`.

Regenerate and compare it from the repository root:

```bash
mkdir -p tmp/nosql-fixtures
/usr/bin/sqlite3 --version
/usr/bin/sqlite3 :memory: \
  '.read example/nosql/tests/fixtures/stage1.sql' \
  > tmp/nosql-fixtures/stage1.actual
diff -u example/nosql/tests/fixtures/stage1.expected \
  tmp/nosql-fixtures/stage1.actual
```

The same check is available as:

```bash
make -C example/nosql fixture-check
```

That target rejects any oracle version other than `3.54.0`.

## Stage 1 exit gates

Run from the repository root:

```bash
make test-nosql
```

This checks the public package build, vets the package, and runs every
package-local test with a 120-second per-test timeout. A failed assertion must
terminate the test process nonzero; printing a failure without exiting nonzero
is not a passing gate.

Stage 1 is complete only when these gates pass and the canonical fixture record
matches SQLite 3.54.0. Later stages add raw VFS, codecs/SQL, pager/B-tree,
journal/schema, WAL/concurrency, planner/built-ins, public API/facade, and
browser durability evidence as separate milestones.
