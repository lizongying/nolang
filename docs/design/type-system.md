# Type system: the structure behind a type spelling

Status: implemented (this document ships with the code).
Package: `src/ty` — imports the standard library only.
Consumers: `parser`, `hir`, `checker`, `mir`, `build`, `fmt`, `lsp`.

## 1. The rule

A type is a **structure**. The text a program writes (`?[str]i64`, `ptr byte`,
`u128`) is a *spelling* of that structure, not the type itself.

Before this layer existed every stage re-derived the type from the text:
five independent parsers (`parser.buildType`, `parser.parseMapTypeString`,
`mir.KindOfRaw`, `mir.parseArray`/`parseSliceElem`/`parseOptionElem`,
`mir.parseFuncType`) plus roughly two hundred prefix checks spread over
codegen. They disagreed (`[fd]i64` was a map in two of them and an array in
the third), and because promotion flattened every narrow integer to `i64` the
*original* type was gone by the time codegen needed it, so the backend guessed
from the remaining text.

Now:

* `ty.Parse(raw) *ty.Type` is the only classifier.
* `ty.Type` is immutable, interned per raw spelling, and `String()` returns the
  spelling it was parsed from — so AST caches, diagnostics and LLVM symbol
  names are unchanged.
* Promotion rules live in `ty.Promote` / `ty.Convert`, not at each use site.
* When a promotion erases a type, the erased one is preserved as
  **provenance** (`mir.Value.Origin`), not reconstructed from text.

## 2. `ty.Type`

```go
type Kind uint8
// Invalid Void Bool Int Float Char Str Txt Slice Array Map Option View Ptr
// Func Union Named

type Type struct {
    Kind    Kind
    Bits    int16   // Int / Float: 8..128
    Signed  bool    // Int: false for u*, byte, char
    Elem    *Type   // Slice Array Option View Ptr
    Key     *Type   // Map
    Len     int64   // Array: literal length, -1 when symbolic
    LenText string  // Array: the written size, e.g. "n" or "?"
    Params, Results, Members []*Type
    Name    string  // Named (may be module-qualified)
    Under   *Type   // Named: what a Resolver said this alias means
    raw     string  // the spelling; identity key and diagnostic output
}
```

`Kind` describes **syntax**. It deliberately does not describe layout:
whether `fd` is an integer alias or a struct, whether an enum is tagged —
those are registry facts that belong to the stage holding the registry.
`ty` exposes a `Resolver` interface for exactly that and nothing more:

```go
type Resolver interface {
    Alias(name string) (target string, ok bool) // fd = i64, code = i32
    IsTypeName(name string) bool                // a registered struct/enum
}
```

A pool without a resolver is the common case (`ty.NewPool(nil)`): every
unresolved name simply stays `Named` and keeps its spelling, which is all a
parser or the HIR layer can honestly know at that point.

## 3. The double truth, and the one place they meet

| | `ty.Kind` | `mir.Type.Kind` (`TypeKind`) |
|---|---|---|
| answers | what the spelling says | how the value is laid out |
| who decides | `ty.Parse` | the module registry |
| examples | `Named("fd")` | `KindInt` (no layout registered) |

`mir.internType` is the **only** place a registry fact overrides the syntax:
tagged enums become `KindEnum`, owned structs set the ownership bit, a bare
name that names nothing registered falls back to `KindInt` (nolang models
scalar newtypes as integer aliases). `Builder.TypeExplicit` asserts the two
views agree — a caller claiming `KindStruct` for a spelling already interned as
`KindInt` is a bug, reported as a warning, or as a panic under
`NOLANG_TY_STRICT=1`.

## 4. Parsing rules (total)

`Parse` never fails and never panics; unrecognised text becomes a `Named` with
that spelling, so `Parse(s).String() == s` for every input.

Handled: `i8..i128`, `u8..u128`, `byte`, `char`, `bool`, `f32/f64`, `str`,
`txt`, `void`, the aliases `int`/`uint`/`float` (Int64 signed / Int64 unsigned
/ Float64), `num` and `string` (reserved unions), `[]T`, `[N]T`, `[K]V`,
`?T` (including `?&T`), `&T`, `*T` and `ptr T`, `fn(p...)(r...)`, `a|b`,
module-qualified and monomorphised names.

**The single `[` rule.** `[X]T` is an array when `X` is a number, `?`, or a
single ASCII letter (an implicit generic size parameter — see
`parser.addImplicitGeneric`); it is a map otherwise. One rule, one place, so
`[fd]i64` is a map everywhere. (It used to be a map in two parsers and an
array in `parser.buildType`; nothing depended on the disagreement.)

**Pointers.** `*T` and `ptr T` are both `KindPtr`. The spelling is preserved
(`String()` round-trips), so `parser.PointerType.String()` may render `*T`
while a written `ptr T` stays `ptr T`.

## 5. Promotion, conversion, provenance

```go
func (p *Pool) Promote(l, r *Type) *Type  // same kind → wider; int+float → float;
                                          // char+int → Int32 unsigned; bool+bool → u1
func ConvertExact(from, to *Type) Conv    // Trunc SExt ZExt SIToFP UIToFP FPToSI
                                          // FPToUI SatFPToI FPExt FPTrunc Bitcast Clone
```

The extension direction comes from `from.Signed` — `byte`, `u8`, `bool`, `char`
zero-extend, `i8..i128` sign-extend — **not** from the spelling of the storage
slot. This is what fixes `[]i8` element reads: `-9` used to come back as `247`
because the MIR type was already `i64` and the old code extended whatever it
guessed the source was.

MIR keeps computing every integer in `i64`. The source type survives in:

```go
type Value struct {
    Type   TypeID // what the backend materialises (i64)
    Origin TypeID // what the SOURCE declared, when promotion erased it
}
```

`Origin` is set at exactly three kinds of site: a narrow container-element read
(`KIndex`), an infix result that had to widen, and a char/unsigned mixed
operation. Codegen reads `sourceType(v) = Origin ?? Type` and decides udiv vs
div, sext vs zext, `@str_from_cp` vs `@str_from_i64` from **structure**, never
from the leading letter of a name.

## 6. Stage wiring

| Boundary | Before | Now |
|---|---|---|
| parser type node → structure | string round-trip | `parser.TyOf(Type) *ty.Type` (recursive, no text) |
| `parser.buildType` | hand-rolled prefix chain | classify with `ty.Parse`, rebuild the AST node |
| `parser.parseMapTypeString` | "not a number ⇒ map" | `ty.Parse(s).Kind == ty.Map` |
| HIR `Node.Type` | index into `Strings` | unchanged, plus `TypeRef(id)` / `SetTypeRef` over `Package.Types []*ty.Type` |
| HIR `Inferred` | `map[int32]string` | `map[int32]*ty.Type`; `InferredType` returns the spelling |
| THIR `exprTypes` | `map[parser.Node]string` | `map[parser.Node]*ty.Type`; `TypeRefOf` is the structured `TypeOf` |
| THIR `declTypeOf` | string | `*ty.Type` (precedence decided once) |
| MIR | five local parsers | one `Module.tyOf` → `ty.Parse`, registry overrides in `internType` |
| MIR declared-type table | `lowerer.localRaw map[string]string` | `localTypes map[string]*ty.Type`; `Value.Origin` decides first, the name table only answers "is the initializer of THIS declaration signed?" |
| parser `SemanticContext` | 4 maps of spelling strings | `VarTypes` / `FuncVarTypes` / `IdxLocalTypes` / `EnumVariantPayload` hold `*ty.Type`; `VarType`/`SetVarType` stay as spelling projections, `*Ref` is the structured API |
| checker alias resolution | ad-hoc name lists | `checker.ty.go`: a `ty.Resolver` over the published alias registry + `checkerType`/`checkerCanonicalRaw` |
| codegen | 41 prefix parses | `Value.Origin` / `Type` structure |

The MIR-side parsers are **deleted**, not just bypassed: `KindOfRaw`,
`isMapRaw`, `parseArray`, `parseSliceElem`, `parseOptionElem`, `parseMapTypes`,
`parseFuncType` and the hand-rolled `arrayElemRaw` are gone from `src/mir`. What
remains is `Module.tyOf(raw)` (parse with this module's `ty.Resolver`) and
`kindOfTy(*ty.Type) TypeKind` (syntax kind → **layout** kind, with registry
overrides still applied afterwards in `internType`).
`src/mir/structure_classification_test.go` pins the bracket rule at that
boundary so a second, subtly different classifier cannot come back:
`[fd]i64` / `[str]i64` are maps, `[3]i64` / `[n]t` / `[?]i64` are fixed-size
arrays, `[]i64` is a slice, and `ElemRaw()` is the only element accessor.

AST cache: parser node fields and every `String()` are unchanged, so
`parser.ASTVersion` is untouched. `hir.HIRVersion` was bumped because the
`Inferred` side table changed shape (HIR has no disk cache).

## 7. The gate

```
make verify-ty        # scripts/check_ty_strings.sh
```

Fails when a package outside `src/ty` grows new type-string parsing:
`HasPrefix(x, "?"|"&"|"*"|"[")`, `Split(x, "|")`, `TrimPrefix(x, "?"|"&")`.

`scripts/ty_strings_baseline.txt` is the ratchet. It is **empty on purpose**:
the migration drove the debt to zero, so the only remaining hits are the six
printers in `parser/type.go` (allowlisted below) and the gate fails on the very
first new occurrence instead of tolerating a budget. The format is
`file<TAB>class<TAB>count`; a file may never exceed its number, the gate prints
`down` when one drops, and `--update` reprints the current rows (all unregistered
hits are reported with their location, so registering one is a deliberate
`--update` that shows up in review).

Part of that zero was made honest rather than achieved by widening the patterns:
the scan only counts identifiers whose name says they hold a **nolang spelling**
(`…Type…`, `…Raw…`, `typ`, `tin`, `tout`). Locals holding an LLVM type
(`lt`, `plt`, `alt`, `argT`) use a different grammar and are not debt.

Printers (`parser/ast.go`, `parser/type.go`, `parser/dump`) are allowlisted:
reproducing the source spelling *is* their contract.

Migration rule for new code: if you are about to inspect a type, get a
`*ty.Type` (from `ty.Parse`, `Package.TypeRef`, `THIRView.TypeRefOf`,
`Value.Origin`) and switch on `Kind`.

Behavioural gate for the whole backend, run by hand after a change like this:

```
GOLDEN=tests/golden/mir-baseline.tsv scripts/mir_golden.sh
  # add MIR_GOLDEN_GLOB='arr-*.no' for a seconds-scale subset
```

Its buckets are SAME / DIVERGE / REGRESS / IMPROVED / UNSTABLE, one row per
corpus file. `rc=124` in a row means "no verdict" (the harness alarm fired, e.g.
under parallel load), never "the file is broken" — re-run that file alone before
calling it a regression. The sweep scripts deliberately **prefer the Homebrew
LLVM on `PATH` and otherwise inherit `PATH`** instead of forcing `/usr/bin`
first: `/usr/bin/clang` can be older than the macOS SDK, which turns every row
into a fake failure (`ld: tapi error: malformed file … unknown architecture
arm64e.x1-macos`).

## 8. Layout reference

| Spelling | Kind | Bits | Signed | slot |
|---|---|---|---|---|
| `i8..i128` | Int | 8..128 | yes | `iN` |
| `u8..u128` | Int | 8..128 | no | `iN` |
| `byte` | Int | 8 | no | `i8` |
| `char` | Char | 32 | no | `i32` (zero-extended only at call boundaries) |
| `bool` | Bool | 1 | no | `i1` |
| `f32/f64` | Float | 32/64 | — | `double` |
| `str` / `txt` | Str / Txt | — | — | `%str-long` / `%txt` |
| `int` / `uint` / `float` | Int64 / Int64 / Float64 | 64 | yes/no | `i64` / `i64` / `double` |
| `num` | Union{i64,f64} | — | — | resolved per use |
