---
sidebar_position: 2.5
---

# Annotation and Comment Scope

A `#{...}` annotation carries no information about "how much it covers" — its scope is decided by
**the node it is attached to**. This page pins that down: one position rule, one set of decisions,
four levels, plus one refinement ("a key may support only some of those levels" — `overflow` is the
one that only accepts L3, see §4.1), so that the author, `no fmt`, `no vet`, codegen and the LSP all
give the same answer for the same line.

> Why this must be pinned down: if `no vet` thinks one `#{overflow=wrap}` covers the whole function
> while codegen thinks it covers only the next line, you get silent semantic drift — "vet is green,
> runtime still returns `err`". Scope is not a property of the annotation; it is **the readers'
> shared understanding**, and an unwritten shared understanding drifts.

---

## 1. One principle: the host decides the scope

An annotation has exactly two legal positions (the position rules themselves live in
[`syntax.md`](syntax.md)〈Annotation positions〉):

```no
; (1) above: on its own line, placed above the target
#{overflow=wrap}
x i8 = a + b

; (2) trailing: on the same line, after the target
x i8 = a + b #{overflow=wrap}

; for multi-line targets (functions, blocks) the trailing form goes after the closing `}`
greet = () {
    print('hi')
} #{mac-arm64}
```

**The two are fully equivalent** — same key, same host, same scope, and `no fmt` never rewrites one
into the other (trailing stays trailing, above stays above). Which one to pick is purely a
readability call: use the trailing form to save a line on short statements, use the above form when
you are annotating a whole expression or the statement is long.

**Writing it on the same line *before* the target is a compile error**; there is no "prefix applies to
what follows" path:

```no
#{overflow = wrap} x i8 = a + b   ; ✗ prefix form → error
```

The decision takes three steps, each with exactly one answer:

| Step | Question | Where the answer comes from |
| --- | --- | --- |
| 1 | Who is the host? | Position: the first non-comment statement above, or the preceding statement on the same line |
| 2 | Which level is the host, and does the key support that level? | the table in §2 plus the matrix in §4 (unsupported ⇒ compile error) |
| 3 | How far does it reach? | the host's subtree, minus inner hard boundaries (§3) |

---

## 2. The four levels

| Level | Host | Effective range | How to write it |
| --- | --- | --- | --- |
| **L0 module** | File head | Every declaration and statement in this file | At the very top of the file, with a blank line between it and what follows |
| **L1 declaration** | Constants/globals, structs, interfaces, tagged enums, functions, methods | The whole definition of that declaration (for functions: the body and all nested blocks) | Directly above the declaration, or trailing on the declaration line |
| **L2 block** | Coroutine group, if-group, `cond -> body`, match, `for`/count/range loops, bare blocks | Every statement in that block body (nested blocks included, stopping at inner L1 boundaries) | Directly above the block header statement, or after the block's closing `}` |
| **L3 single statement** | A single statement (`let`, assignment, `?=`, `return`, call, `cond ->` arm) | That statement and its expressions | Directly above the statement, or trailing on the same line |

### L0 module level

The position is **the very beginning of the file** (nothing before it, not even an import), and there
must be **at least one blank line** between the annotation group and the first statement — the blank
line is the "scope terminator" here: it says this group does not belong to the next statement but to
the whole file.

```no
#{win-amd64}

; ↑ blank line → module level: this file only compiles on Windows x86_64
; every declaration and statement below is covered
helper = () {
    print('windows only')
}

label = 'win'
```

Without the blank line you get an ordinary L1 (attached to the first declaration), and this boundary
means existing code needs no migration:

```no
#{buildin}
bool.to-str = () (out str) {
}
```

Module level is only useful for keys that make sense for a whole file (currently only the platform
keys); declaration keys (`buildin`, `generic`, `embed`, …) at L0 are an error, not "a wider scope".
`overflow` does not accept L0 either — it only accepts L3.

Cross-file configuration is not an annotation; it goes in `package.noml`.

### L1 declaration level

```no
; a whole struct: the field layout follows the type tag
#{inline}
holder {
    inl point
    out point
}

; a whole function: entry/exit logging automatically carries the caller
#{track-caller}
log.write = (msg str) {
    ...
}

; a whole constant declaration: an embedded directory (path relative to package.noml)
#{embed='assets'}
ASSETS fs.embed

; compile out an entire subtree (function bodies included)
#{mac-arm64, linux-arm64}
greet = () {
    print('hello from unix')
}
```

L1 also has a **member granularity**: annotations on struct fields, tagged-enum variants and
C-style enum members (`a pt #{inline}`, `ok(v i64) #{inline}`) cover that member, not the whole type.
Member annotations likewise have only two positions: directly above the member, or after the member
on the same line (the trailing form is idiomatic).

### L2 block level

When the host is a "block header statement", the scope is the whole block body:

```no
; the whole block is excluded (no IR generated, no type checking either)
#{win-amd64}
2 * {
    print('windows only')
}

; an if-group is likewise treated as one unit
x = 1
#{win-amd64}
{
    x == 1 -> print('arm-1')
    -> print('arm-other')
}
```

All four block kinds apply: coroutine groups (a bare block in statement position), if-groups
(`{ cond -> … }`), single-condition statements (`cond -> body`), match, and `for` / count / range
loops.

If the body of `cond -> body` is a single statement, L2 and L3 give the same result; the difference
only shows up when the body is a block.

**Not every key fills all four levels.** A key may support only some of them, and writing it at a
level it does not support is a **compile error**, not a harmless redundant line. The only restricted
key today is `overflow`: it only accepts L3, for the reasons in §4.1.

### L3 single-statement level

```no
a i8 = 100
b i8 = 100
; covers only the next statement (the trailing form `c = a + b #{overflow=wrap}` is equivalent)
#{overflow=wrap}
c = a + b
c: {
    ok -> print('wrap:' - (it == -56).to-str())
    nil -> print('nil')
    err -> print('err')
}
```

Inside a block, a standalone annotation line covers only "the one statement immediately after it". An
unrelated annotation in between (say another `#{...}` after `#{overflow=wrap}`) does not break the
chain; the first real statement is still found. But if the next annotation carries the same key, then
the former covers nothing (the latter wins).

---

## 3. Inheritance, override, and hard boundaries

**Inherited from outside in, overridden by the nearest one.** For keys that support multiple levels
(the platform keys), L0 → L1 → L2 → L3 apply in order; if the same key appears twice on the same
node, the latter overrides the former (`#{mac-arm64} #{mac-arm64, win-amd64}` means the latter).

**A function body is a hard boundary.** The scope of an L2 block stops at the **syntactic subtree**:
it does not spread along call relations, and it does not enter the definition body of any function /
method / anonymous function. The mode used inside a function body is decided only by its own L1
annotation or by L3 annotations inside it, and has nothing to do with the annotation on the block that
calls it.

```no
; L1: the whole function is removed together with its body (no IR on macOS)
#{win-amd64}
windows-only = () {
    print('never compiled on mac')
}

; no L1 annotation of its own → compiles everywhere, and being called from anywhere changes nothing
always = () {
    print('always compiled')
}
```

The reason: if an outer annotation could reach into a function body, one word on a call site would
silently change the semantics of the callee — invisible while debugging. An L1 annotation is the
opposite: it sits on the function's own head, as part of the declaration.

**Hard boundaries** (inheritance stops here): function bodies, method bodies, anonymous
function/lambda bodies, `#{c}` foreign declarations.
**Not boundaries** (inheritance continues as usual): `{}` bare blocks, if-groups, match arms, loop
bodies.

---

## 4. Key × level matrix

`✓` applies ／ `✗` illegal (must be a compile error) ／ `—` meaningless at that level (also an error,
not silently dropped)

| Key | L0 module | L1 declaration | L2 block | L3 single statement |
| --- | --- | --- | --- | --- |
| `overflow` | ✗ | ✗ | ✗ | ✓ |
| Platform keys (`mac-arm64` etc.) | ✓ | ✓ | ✓ | ✓ |
| `buildin` | ✗ | ✓ function / tagged enum / layout struct | ✗ | ✗ |
| `inline` | ✗ | ✓ struct / field / variant / function | ✗ | ✓ trailing member |
| `generic` | ✗ | ✓ function / struct | ✗ | ✗ |
| `track-caller` | ✗ | ✓ function | ✗ | ✗ |
| `embed` | ✗ | ✓ top-level `let` | ✗ | ✗ |
| `bind` | ✗ | ✓ import line | ✗ | ✗ |
| `c` / `cpp` / `rust` | ✗ | ✓ the function declaration it precedes | ✗ | ✗ |
| `fmt-skip` | ✗ | ✗ | ✓ markdown fenced block | ✗ |
| Metadata (`derive` / `range` / `max` / `debug`…) | ✗ | ✓ declaration / member | ✗ | ✗ |
| `index-out` | — | — | — | — removed in D11; its presence is an error |

Both "illegal" and "meaningless" **must be reported as errors**, never silently ignored: a
`#{buildin}` written above a `for` loop that is quietly dropped makes the author believe the builtin
declaration took effect.

### 4.0 The three carriers of `#{buildin}`, and what makes the "layout struct" special

`#{buildin}` sits on a declaration, but its meaning differs completely across the three carriers:

| Carrier | Meaning | Example |
| --- | --- | --- |
| Function | Empty-body stub: the compiler / FFI provides the implementation, no body is generated on the Nolang side | `#{buildin}`<br/>`fs.remove = (p str) (ok bool) { }` |
| Tagged enum | Variants exist for matching and exhaustiveness checking; the representation and construction come from the builtin runtime | `#{buildin}`<br/>`option { ok(v t), nil, err(e err.err), }` |
| **Layout struct** | **Documentation only**: this `struct` does not exist semantically; its sole output is the field types (`t.len` is `u8`) | `#{buildin}`<br/>`txt { data [255]byte; len u8 }` |

The third one needs its own explanation, because it points the opposite way from the first two: the
function stub and the tagged enum are both "the compiler really uses this declaration", while the
layout struct is "the compiler **does not** use this declaration — it is only the source of field
types and a readable note about the layout". The real layout and the documentation links live
elsewhere:

| Type | Source of the real layout | Source of docs / links |
| --- | --- | --- |
| `str` | MIR hand-written descriptor `%str-long = { i32 len, i32 cap, i8* data }` | `parser.BuiltinTypes()` |
| `vec` | MIR hand-written descriptor `%vec = { i32 len, i32 cap, i64 data }` | `parser.BuiltinTypes()` |
| `txt` | MIR hand-written descriptor `%txt = { [255 x i8] data, i8 len }` | `parser.BuiltinTypes()` |
| `arr` | no descriptor: `[n]t` is the bare aggregate `[N x T]`, and `.len` is the compile-time constant `n` | `parser.BuiltinTypes()` |

`parser.BuiltinTypes()` (`src/parser/builtintype.go`) is the single registry mapping "name → std
module → module doc anchor → layout"; the LSP's hover / go-to-definition / completion and
`IsBuiltinLayoutStructName` all read it. That is exactly why the layout struct **no longer carries the
links**: delete it and hover and F12 still work; only the checker would lose the field type of
`t.len`, which is why it is still there.

Two hard rules follow, and breaking either one immediately produces a "shadow struct" that
contradicts the real layout:

1. **A struct carrying `#{buildin}` must use a builtin layout name** (`str` / `vec` / `txt` / `arr`).
   Putting it on any other struct is an error: MIR treats that declaration as "the layout is owned by
   the compiler" and skips it, every field is ignored, and what is left for the author is a pile of
   `unknown field`.
2. **Declaring a struct with a builtin layout name must carry `#{buildin}`.** If a user writes a bare
   `str { … }`, it is treated as an ordinary type of this module and prefixed (`m.str`), so codegen
   emits a `%m_str` that clashes with `%str-long`.

Both are reported by `checker.ValidateBuiltinStructDecls` (`1a8m445h` / `3lu6n4np`), and both
`no build` and `no vet` hit them. Conversely, **only** the combination of `#{buildin}` + a layout name
is skipped by the module-prefix phase and by MIR's `collectStructFields` (see Phase 1 of
`src/build/module_prefix.go` and `isBuiltinLayoutStruct` in `src/mir/hir2mir.go`) — the criterion is
the "annotation + name" combination, not the name alone.

### 4.1 Why `overflow` only gets L3

`#{overflow}` is a **per-operation** semantic switch, not declaration metadata. As soon as it were
allowed on a function or a block, "which mode does this operation actually use" would depend on how
far away the annotation is, whether other annotations sit in between, and how many blocks are
stacked — answering it for one line of code means walking back up, and keeping `no fmt` / `no vet` /
codegen on the same answer becomes extremely hard (they have measurably drifted, see §7).

It also **does not change the type**: the result is always `?T`. A block-level or function-level
`wrap` would not turn `?T` into `T`; it would only silently replace "overflow is an `err`" with
"silent wraparound" — an error turning from an "explicit failure" into a "wrong value that looks
successful", which is exactly the thing that should never be decided by a distant annotation.

So it is pinned to one line, one annotation:

- It covers only the statement immediately after it; the trailing form
  (`x = a + b #{overflow=wrap}`) is equivalent.
- To make a whole region apply, annotate statement by statement — that is what both
  `no fmt --fix=overflow` and the LSP quickfix produce.
- Writing it on a function head, block head or file head → **compile error**, not "ineffective but
  harmless".

A side benefit: the annotation is always on screen together with the statement it governs, so moving
code around never separates them; and a `wrap` never accidentally covers the neighbouring operation
just because of an indentation change.

---

## 5. The scope of comments (`;` / `//` / `;;`)

Comments take no part in compilation semantics; their attachment only affects `no fmt` layout and
where API docs are mounted. The marker syntax lives in [`code-style.md`](code-style.md)〈Comment markers〉.

| Comment | Attachment |
| --- | --- |
| Inline (`x = 1 ; note`) | that line |
| Consecutive comment lines above a line | the next statement/declaration (L1 or L3, same host rule as annotations) |
| A blank line between it and the target | attached to no node; acts as a block/file separator (except at the file head, see L0) |
| `;; … ;;` block comment | stops at its own terminator, never crosses a scope |
| Comments between an annotation and the target | travel with the target; output order is fixed as "doc comment → annotation → statement" |

`;` and `//` are semantically identical; `no fmt` keeps the original marker and never rewrites it.
The translation anchor of a doc comment (`[get](#args.get)`) must be the **last line** of the comment
group; it belongs to the same host and cannot cross nodes.

---

## 6. The four readers must agree

For one annotation, four readers must derive the same scope:

| Reader | What it needs to know |
| --- | --- |
| parser / codegen | which mode this node and its subtree use |
| checker / `no vet` | the range considered "already annotated" |
| `no fmt` | the annotation prints back in place and must not move across hosts |
| LSP | the position where hover/quickfix inserts equals where it actually takes effect |

Four invariants follow:

1. **Same annotation verdict**: the range codegen considers "annotated" is necessarily the range
   `no vet` considers handled, and vice versa.
2. **fmt does not move**: `no fmt` may only adjust the position within the same host (above ↔
   trailing); moving across hosts is a semantic change.
3. **Failure must be reported**: an annotation at a level with no matching meaning (the `✗` / `—`
   cells in §4) is an error, not a silent no-op.
4. **Level restricted**: a key only applies at the levels the matrix allows. When `overflow` is
   written at L0/L1/L2, all four readers must treat it as an **error** — no "vet accepts, codegen
   does not" split (current state in §7).

---

## 7. Current state and gaps (measured 2026-11-11)

Verified item by item with `bin/no`, in a uniform shape: "add two `i8 = 100` values and see whether
the result is `err` or `-56` (wrapped)"; platform keys were verified with `#{win-amd64}` on macOS by
checking whether the code was excluded.

| Level | `overflow` (design: L3 only) | Platform keys | Declaration keys |
| --- | --- | --- | --- |
| L0 module | ✗ forbidden by design; currently attaches to the first statement (= L1) | not implemented | not implemented |
| L1 declaration | ✗ forbidden by design; **currently a hole**: `no vet` accepts it (whole function skipped), codegen does not (still `err`) | ✓ whole subtree excluded | ✓ |
| L2 block | ✗ forbidden by design; codegen does not accept it. `for` / if-group / match-arm heads can make `no vet` quiet along the block; **a bare block `{` in statement position cannot even do that** (see below) | ✗ the block still compiles | — |
| L3 single statement | ✓ | ✓ | — |

Measured evidence:

```text
a-none.no    (no annotation)                   → err
b-line.no    (#{overflow=wrap} above a stmt)   → result:true  (-56, applied)
c-fn.no      (#{overflow=wrap} above a fn)     → err           (vet: No issues found)
d-block.no   (#{overflow=wrap} above a for)    → err ×2        (vet: 2 hints)

p1-block.no  (#{win-amd64} + 2 * {…})          → block still runs   (not filtered)
p2-stmt.no   (#{win-amd64} + a single print)   → line excluded      (applied)
p4-fn.no     (#{win-amd64} + fn declaration)   → function excluded  (applied)
p6-ifgroup.no(#{win-amd64} + if-group)         → arm still runs     (not filtered)

; compile-time comparison: a comment on a `for` head vs none, 15w45dqk byte-identical
k2.no (for head #{overflow=wrap} + body `x i8 = a + a`) → cannot assign ?i8 to i8
k3.no (same file with that line deleted)                → cannot assign ?i8 to i8
```

Why a bare block cannot even make `no vet` quiet: the attachable targets of
`parseAnnotationStatement` are IDENT / USE / IN / RARROW / array-type methods only, and `{` is not
among them. So in the form below the annotation is attached neither to the block nor to a statement
inside it — it degenerates into a standalone `AnnotationStatement`, a dead line for the parser,
codegen and `no vet` alike:

```no
#{overflow=wrap}
{
    ...
}
```

The heads of `for` / if-groups / match arms start with an identifier or `in`, which is why they
happen to fall inside the attachable targets.

Three holes to tighten (the "L3 only" rule for `overflow` is not enforced yet):

1. **`no vet` skips the whole function once it sees a function-level `overflow`** (the
   `fn.OverflowMode == ""` short-circuit in `walkStmtForOverflow`) → checks are green while the
   runtime still returns `err`.
2. **`no vet` inherits downwards along blocks** (`enclosingOverflow`) → one annotation on a block head
   quiets the whole block, even though codegen does not accept it at all.
3. **parser / codegen silently ignore L1/L2 `overflow`** instead of reporting it → the author has no
   way to learn it did not take effect.

Inventory and handling of existing code (`src/std`, after three rounds of `no fmt --fix=overflow`
down-pushing):

| Category | Before migration | After round 1 | After round 2 | After round 3 |
| --- | --- | --- | --- | --- |
| `overflow` annotation lines in total | 4580 | 4083 | 3861 | 4080 |
| of which dead lines that "attach to no node" | 330 (by shape) | 87 | **0** | **0** |
| reported by `no vet` but unattachable | — | ~270 | 270 | **51** |

Round 1 (2026-11-11) net change: 816 scope-level annotation lines deleted, 325 per-statement
annotations inserted. Split as A/B, about 218 of the 325 are sites that were already missing an
annotation and have nothing to do with down-pushing (running insert-only without stripping the
shielding would add them too), and about 97 are block-level intents materialised per statement.

Round 2 (2026-11-12) was a **pure sweep**, caused by `findFixTargetNode` not descending into arm
bodies and there being no "check after insert" step: 381 lines deleted, 159 lines added, **all of
them `#{...}` annotation lines, 0 lines of code** (verified line by line with `diff`). 159 of the
additions are relocations "from a block head / arm head to above a statement", and the other 222 are
dead lines and redundant scope-level annotations deleted outright — those positions were not in
effect anyway, so deleting them changes neither compilation nor checking.

Round 2 also fixed three tool defects:

1. **`checker` reported line numbers collapsing to the container**: `walkExprForIntOverflow`'s
   `emit` mixed the container's `stmt.Pos().Line` with the operator's column, so every report inside
   `{ ... }` / an if-group / a match arm carried the header line. The wrong diagnostic position was
   only the symptom; the real problem was that `--fix`'s `findFixTargetNode` took "the innermost
   statement containing that line" and could only get the container back, so the annotation was
   inserted at the block head again. It now uses **the operator's own line** (`x.Token.Line`), falling
   back to the container line when the token line is missing.
2. **No descent into arm bodies**: `findFixTargetNode` deliberately stopped above the whole match
   statement. It now descends into **block-form arm bodies** (the `{ ... }` of `cond -> {`, including
   `BlockMatchStatement.Body` of the `{ ... }: { ... }` form); inline arm bodies are still not
   descended into (the statement of `w i64 -> w + 1` is on the same line as the arm pattern, so
   inserting above it would land inside the match, before the arm pattern line).
3. **Inserting at an unattachable position anyway**: `#{overflow}` now **re-parses to self-check**
   after inserting, and withdraws annotations that "were inserted but degenerated into a standalone
   `AnnotationStatement`" (no more dead lines). After a withdrawal the source seen by a second
   `--fix` pass is unchanged, so idempotence holds.

Round 3 (2026-11-12) touched the parser: `parseAnnotationStatement` gained **DOT** as an attachable
target. Statements that start with the receiver, like `.n = .n + 1` / `.cnt == 0 -> …`, are the same
class as `x = …` (single statement, L3), yet they were not among the attachable targets ⇒ the
annotation degenerated into a standalone `AnnotationStatement` (dead line), the site was reported
forever, and `--fix` was withdrawn by the self-check even when it did insert. After the relaxation,
219 sites received an L3 annotation (annotation lines 3861 → 4080) and the unattachable count dropped
from 270 to 51. The AST shape changed accordingly (one fewer AnnotationStatement, one extra side-table
record and `OverflowMode` on the target statement), `parser.ASTVersion` was bumped to `2026-11-12.1`,
and `make gen no` is required afterwards.

**`{` and `(` were deliberately not relaxed**: `{` (bare block in statement position / parenthesised
match) and `(` (the `(cond) { }` loop head) are L2 block hosts, and `overflow` is designed to accept
L3 only. Letting them in would make `overflowAnnotatedNode` return early for the whole block, which
amounts to legalising L2 semantics — in conflict with the "tighten first, relax later" order in §8.
Most of the remaining 51 are these two kinds, and belong to step 4b of §8.

Acceptance criteria (isolated trees A/B, same source and same compiler, differing only in `src/std`,
`no vet` per file): all 73 affected files had **0 errors** (no compilation breakage), 25 files had
fewer diagnostics and 19 had more — and every increase is an `ovfhndld` "bound to `?T` but no overflow
value declared" exposed after removing the shielding, which is the expected outcome of step 1 in §8,
not a new defect. Another 9 `nolang-overflow-ineffective` warnings are the tool's pre-existing
"fill the gap" behaviour landing on bigint / string overloaded operators, unrelated to down-pushing.
After round 2 a full re-run of `bin/no run test/std/*.no` (zstd / tls / str / json / set / base32 …)
**all PASSED**.

Round 3's re-run criteria: the 3 red `parser` tests (`TestAnnotationsLiveOnAST` /
`TestCachedASTLowersToIdenticalHIR` / `TestBuildSemViewsMatchesFreshParse`) were **byte-identical
before and after the relaxation** — `only 288 files parsed cleanly` in both runs, which shows the
relaxation introduced no new parse failures; they are the in-flight state of the concurrent
parser/annotation refactor. The output of `no fmt -d src/std` was **byte-identical** before and after
the relaxation, i.e. the formatter prints annotations on `.` back in place with no format drift.
`test/std`'s heap / zstd / tls / json / set / map / regexp were all green.

**76 remaining arm-head annotations** (`#{overflow=wrap}` immediately above `cond -> {`): these are
**not** dead lines — the parser copies arm-level annotations onto every statement in the arm body, so
they take effect for the body (measured: one annotation on the arm head quiets `r = i * 2` in the
body, leaving only the header condition reported). They have no effect on the **arm condition**
itself (a condition is not a statement). These 76 are left as they are: per-statement insertion
cannot reach them (the body sites are already quiet and no longer reported), and stripping them just
to re-add them only churns the header line.

**51 sites still reported by `no vet` that the current language cannot annotate** (270 in round 2,
only these left after round 3 relaxed DOT): the target statement starts with `{` (bare block in
statement position / parenthesised match) or `(` (the `(cond) { }` loop head), plus a few arm header
conditions. They are reported **today as well** (the original annotation never took effect), so
withdrawing them is not a regression; zeroing them out requires steps 1–2 of §8 first (making L2
`overflow` an error) and then a decision on whether L2 should really be supported — see step 4b of §8.

Spot-checking positions like `archive/bzip2.no:315`, where both the block head and the block interior
carry `#{overflow=wrap}`: the block-head layer is currently ineffective and the per-statement layer
inside the block is what actually works (`--fix=overflow` has already filled it in). So deleting the
block-level layer changes no semantics; but after deleting it, a re-run of `no vet` exposes the
positions that were "quiet only because of the outer annotation" — that is the point of tightening,
not a side effect. Measured: the `ovfhndld` hints in `crypto/rsa.no` went from 19 to 22, all on
statements "bound to `?T` without an overflow value declared", with no semantic change.

L0's impact on existing code: 21 files under `src/**/*.no` have an annotation as the first effective
line — 17 are `#{js}`, the rest are `#{buildin}` etc. directly followed by a declaration — and **none**
of them has a blank line, so under the §2 L0 criterion they are still L1 and neither `no fmt` nor
compilation changes. `str.no` has 2 cases of "annotation + blank line + cond arm" inside a function
body, which do not trigger the L0 criterion.

---

## 8. Order of tightening

1. **First stop `no vet` from accepting L1/L2 `overflow`**: delete the "skip the whole function" short
   circuit, and restrict `enclosingOverflow` to the subtree of the same statement (no downward
   propagation across blocks).
2. **Then make the parser report it**: `#{overflow}` at L0/L1/L2 → compile error, with a message
   pointing at "annotate per statement, or run `no fmt --fix=overflow`". This step exposes the whole
   existing corpus at once.
3. **Clean up the existing corpus**: `no fmt --fix=overflow` already has the down-pushing ability
   (strip the scope-level layer, then fill in L3 per statement following the checker's reports);
   `src/std` has been through three rounds (see the inventory table in §7). After the change,
   `make gen no` is mandatory (re-bake the blob and StdSigKey), and `no fmt -w` must not be run over
   `src/std` — the migration only touches annotation lines, done per file with
   `no fmt -fix=overflow -w <f>`, with `diff` guaranteeing no other changes.
4. **Relax the parser's attachable targets** (round 3 completed half of it):
   - **4a done**: `parseAnnotationStatement` accepts statements starting with **DOT**. `.n = .n + 1` /
     `.cnt == 0 -> …` are single statements (L3) just like `x = …`, but historically could not take an
     annotation ⇒ 219 `#{overflow=wrap}` in `src/std` never took effect. After the relaxation the
     unattachable count dropped from 270 to 51, and `parser.ASTVersion` was bumped to `2026-11-12.1`.
   - **4b pending decision**: `{` (bare block in statement position / parenthesised match) and `(`
     (the `(cond) { }` loop head) are **L2 block hosts**. Relaxing them legalises L2 semantics for
     `overflow` (`overflowAnnotatedNode` would return early for the whole block), which runs against
     the tightening direction of steps 1–2. The correct order is to do steps 1–2 first (make L2
     `overflow` an error), then decide whether the remaining 51 should be solved by "rewriting them
     as statements without a block" or "filling in the arm body per statement", rather than opening
     `{` / `(` unconditionally.
5. **Tighten the other keys gradually**: turn declaration keys (`buildin` / `generic` / `embed`…)
   written at L2/L3 from "ineffective" into "an error".
6. Every step must keep `no fmt` idempotent and keep all four readers agreeing on the same input (§6).
