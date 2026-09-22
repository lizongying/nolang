---
sidebar_position: 6
---

# Memory Management

Nolang is a **GC-free** language. Memory safety is guaranteed by compiler-inserted `free` calls. This document describes the implemented memory design and ownership semantics.

## Core Principles

### Single Ownership
Each heap `data` buffer has **exactly one owner**. Ownership can be transferred via move; after transfer, the original owner relinquishes free responsibility. For `=` between local variables, a deep clone makes both variables independently own their data.

### Three Assignment Semantics
`b = a` selects one of three semantics based on context:

| Semantic | Trigger | Behavior |
|----------|---------|----------|
| **Value copy** | Primitive types (i64/f64/bool, etc.) and `can_slot_rebind` not satisfied | Direct value copy, no heap data |
| **Stack-slot rebind** | local `b = a`, a is a stack type (i64/u64/i128/u128/txt) and `can_slot_rebind` satisfied | `g.varAlias[b] = a`, b and a share the same stack slot, 0-copy (better than value copy); otherwise degrades to value copy |
| **Deep clone** | `b = a` between locals, a is heap-owning (vec/arr/str/cloneable struct) | malloc new data + memcpy + recursively clone elements; a and b independently own data, each freed at function exit |
| **move** | Output param `out = x`, `vec.push(x)` | Shallow copy struct + mark source as moved; source skips free |

## Stack-type move (stack-slot rebind / slot-rebind)

`i64/u64/i128/u128/txt` are all **stack types** (values live directly in the alloca stack slot, no heap `data`), yet `b = a` still defaults to **value copy** (memcpy a's stack value into b's stack slot). To avoid pointless copies, the compiler performs a **stack-slot rebind** for stack-type `b = a` that satisfies the **can_slot_rebind** constraint: set `g.varAlias[b] = a` so b and a share the same stack slot (0-copy, better than value copy).

### can_slot_rebind constraint (conservative correctness)
After rebind, b and a point to the same stack slot, so any later read of b observes "a's storage". The rebind is therefore only allowed when the source `a` has **no subsequent reference (read or write) at all**; otherwise it **degrades to a full value copy** (semantics unchanged, just one extra memcpy).

- Safety is solved statically by `computeSlotRebindSafety` (main) / `computeMoveEligibility` (user functions) via `stmtContainsVarRefAny` / `exprContainsVarRefAny`: scan every statement after the move (including branch / loop back-edges); if the source is referenced in any way, `slotRebindSafe[stmt] = false` → take the copy path.
- The reference scan must enumerate **every AST node that can reference a variable**; an uncovered write reference would cause an unsafe rebind (corrupted alias slot → wrong output or even an infinite loop). Covered: statement-level `LetStatement` / `ExpressionStatement` / `ForStatement` (incl. `Init`/`Update`/`Condition`/`CountExpr`/`Body` and the **`IterRange` iteration collection**) / `ReturnStatement` / `MultiAssignStatement` / **`UnwrapAssignStatement` (`?=` unwrap)** / **`BlockStatement` (bare block)**; expression-level `Identifier` / `AssignExpression` / `Infix` / `Prefix` / `Call` / `Dot` / `Index` / `IfExpression` (arm bodies) / `Slice` / `Conditional` / `Grouped` / **`AwaitExpression`** / **`CastExpression`** / **`RangeExpression`** / **`RunExpression` (coroutine spawn, defensive; whole-function disable still handled by `curHasUnsafeConstruct`)**. Any new variable-referencing construct must be added to both scanners.
- `match` is desugared into `IfExpression`; references inside its arm bodies are covered by the analysis above, so **match does not trigger a disable** (verified against baseline on `tests/match.no` / `tests/option.no`).

### Disable scenarios (always degrade to value copy)
| Disable condition | Reason |
|-------------------|--------|
| Target is output param / global / heap-type variable | Output params are passed by pointer, globals are cross-function visible, heap types need deep free — rebind breaks ownership |
| Source is a param / global | Params are references to the caller's data; rebind would corrupt the caller's stack frame |
| **stdlib function** (`curIsStdLib`, set via `SetStdModules`) | stdlib internal alias bindings (e.g. fmt's `it` aliased to local `n`) are hard for the reference analysis to fully model |
| **User function body contains a closure (`FunctionLiteral`) or coroutine spawn (`RunExpression`)** (`curHasUnsafeConstruct`, scanned recursively by `bodyHasUnsafeConstruct`) | A closure evaluates captured variables in a *separate function context*; a coroutine runs on another thread. Both have variable lifetimes beyond the current function's single-function `g.varAlias` alias scope, so rebind would corrupt the shared stack slot |

> Design trade-off: closures/coroutines use a **whole-function disable** rather than per-variable precision, because single-function alias analysis fundamentally cannot model cross-function / cross-thread storage sharing. A whole-function disable only forfeits the optimization (degrades to copy) and never introduces a bug — consistent with the "conservative correctness" principle.

### Alias invalidation
- When the target is reassigned, `delete(g.varAlias, name)` first clears any stale alias, otherwise the generic assignment path `varAddr(name)` still points at the old source slot and pollutes the old source.
- The rebind performs transitive resolution along the `g.varAlias` chain to find the final source slot (`a → c → …`), avoiding multi-hop alias misalignment; `g.varTypes[name]` is synced so later type queries stay consistent.

### Orthogonality with heap-type move
Stack-slot rebind only affects the **value copy** semantics of stack-type `b = a`; it is fully orthogonal to the deep-clone / move (output param) paths of heap-owning types, which are unaffected by `slotRebindSafe`.

### Test references
- `tests/slot-rebind.no`: i64/u64 rebind, i128/u128 via `==`, txt via `.len-bytes()`, degrade-to-copy (`da` reused → `db`/`dc` copy), reassign-after (`ra`=10), param-move (`pm_fn` source is a param → copy), rebind-then-reassign (`rr_fn` → `rr`=99), consume (`consume(m)` → `cm`=84). Expected output: `42 7 1 1 17 5 5 10 100 99 84`.
- `tests/slot-rebind-unsafe.no`: coroutine (`run`/`awy`) capturing a stack variable, verifying `curHasUnsafeConstruct` disables rebind and output matches baseline.

### Compiler-Inserted Free
- Function exit: free all non-moved local heap variables
- Before reassignment: free the old value
- Struct fields: recursively free fields containing heap data

## Type Layout

| Nolang Type | Memory Layout | Fields | Allocation |
|------------|---------------|--------|-----------|
| `[]T` (slice) | 24 bytes | len, cap, data | malloc (heap) |
| `[N]T` (fixed array) | 16 bytes | len, data | alloca (stack) or malloc |
| `str` (long string) | 24 bytes | len, cap, data | malloc (heap) |
| struct | sum of fields | each field | alloca (stack) |

## Shallow Free vs Deep Free

### Shallow Free
Only frees the container's data buffer without iterating elements. Applies to:
- `%str-long` (string data is a character buffer, no nested heap-owning elements)
- vec/arr whose elements are primitive types (i64, double, etc.)

### Deep Free
Iterates each element to recursively free its heap data, then frees the container's data buffer. Applies to vec/arr whose elements are heap-owning types:
- `[]str` (elements are %str-long)
- `[][]i64` (elements are %vec)
- `[]MyType` (elements are user structs; recursively free fields)

### NULL Check
All frees are preceded by `icmp eq i8* %ptr, null` to avoid free(NULL) or freeing uninitialized pointers.

## Ownership Transfer (move)

### Single Return Value move
```no
get-slice = () (out []i64) {
    local = [1, 2, 3]
    out = local   ; local marked as moved, not freed at function exit; out managed by caller
}

v = get-slice()  ; v owns data, freed at function exit
```

### Multi-Return Value move (by parameter position order)
```no
get-pair = () (a []i64, b []i64) {
    x = [1, 2]
    y = [3, 4]
    a = x   ; first output param, x marked as moved
    b = y   ; second output param, y marked as moved
}

a, b = get-pair()  ; a owns x's data, b owns y's data
```

**Processing order**: Output parameters are processed in their **declaration order** in the function signature. Each `out = src` assignment independently marks the source variable as moved.

**Note**: If `a` and `b` reference the same source variable (e.g., `a = x; b = x`), within the callee only one move occurs (x marked moved); both a and b receive a shallow copy of x (sharing the same data pointer). But in the caller, a and b are independent local variables, each tracked as a heap variable, and both will be freed at function exit → **double-free**. Nolang currently has no reference/borrow semantics; b does not automatically become an alias of a. **Avoid this pattern**.

### Implicit move in vec.push
```no
inner = [1, 2, 3]
outer.push(inner)
; inner marked as moved, data ownership transferred to outer
; inner skips free at function exit, outer deep-frees inner's data
```

push only shallow-copies inner's struct into outer's element slot **without cloning data**. Thus the source variable and the outer vec share the same data pointer; the source must be marked as moved to avoid double-free.

### Runtime move tracking (function-level u64 bitmap variable)

Move under conditional branches poses a challenge: the compiler cannot statically determine whether a move actually occurs.

```no
cond-move = (flag i64) (out []i64) {
    x = [1, 2, 3]
    if flag == 1 {
        out = x   ; move only happens when flag==1
    }
    ; when flag==0, x still owns data and must be freed at function exit
    ; when flag==1, x's ownership has been transferred and must skip free
}
```

Nolang uses **dual checking** to solve this:

1. **Compile-time marking**: `movedVars[source]=true` indicates a move code path exists
2. **Runtime bitmap**: each function with output parameters allocates a `u64` bitmap variable `%__move_bitmap` on the stack; each bit corresponds to one output parameter position
3. **When move occurs**: set bitmap bit=1 (`or i64 %old, (1<<idx)`)
4. **At function-exit free**: check the bitmap — `bit=1` means move occurred, ownership transferred, skip free; `bit=0` means move did not occur (branch not taken), still owns data, must free

This mechanism applies to all heap types (`vec`/`str-long`/`arr`/user structs).

### Parameter and result count limit

Because the `u64` bitmap variable tracks at most 64 output parameters, the **parameter and result count limit of a function is 64**. When exceeded, the compiler reports an error:

```
Error: compilation error: line 2, column 1: function foo has 65 parameters,
exceeding the 64-parameter limit; use a container type (vec/arr/struct) to
bundle multiple values
```

To pass many values, use a container type to bundle them:
- `[]i64` (slice) — multiple values of the same type
- `[N]T` (fixed array) — fixed-length values of the same type
- struct — heterogeneous multiple values

## Deep Clone (Assignment Between Locals)

```no
a []i64 = [10, 20, 30]
b = a          ; deep clone: malloc new data + memcpy + recursively clone elements
b[0] = 99
; a[0] == 10 (a unaffected)
; b[0] == 99 (b modified independently)
```

### Deep Clone Flow
1. Free the target variable's old value (if it already has heap data)
2. `malloc` a new data buffer, `memcpy` source data to new data
3. Recursively clone each heap-owning element:
   - `%str-long` element: malloc + memcpy string data
   - User struct element: memcpy struct + recursively clone heap-owning fields
4. Write new data pointer, len, cap into the target variable
5. Track target as a heap variable (freed at function exit)

### Cloneable Types
| Type | Deep cloneable | Notes |
|------|----------------|-------|
| `%vec` / `%arr` (primitive elements) | Yes | memcpy data suffices |
| `%vec` / `%arr` (elements are %str-long) | Yes | per-element malloc+memcpy of string data |
| `%vec` / `%arr` (elements are cloneable structs) | Yes | per-element recursive clone of struct fields |
| `%vec` / `%arr` (elements are %vec / %arr) | No | nested container element type unknown, falls back to move |
| `%str-long` | Yes | malloc + memcpy string data |
| User struct (no nested container fields) | Yes | memcpy struct + recursive clone of heap fields |
| User struct (with nested container fields) | No | falls back to move |

### Difference from move
- **Deep clone**: source and target each independently own data; each freed at function exit
- **move**: source relinquishes ownership (marked moved), target takes over data, source skips free

Decision rules for `b = a`:
1. If a is the source of an output param → move
2. If a is the source of vec.push → move
3. Otherwise, if a is a heap-owning type and deep-cloneable → deep clone
4. Otherwise value copy

## Stack-type move (stack-slot rebind / slot-rebind)

`i64`/`u64`/`i128`/`u128`/`txt` are stack types (non-heap-owning). When `b = a` (RHS is a variable) and the source `a` is not referenced afterward, instead of a value copy the compiler performs a **stack-slot rebind**: `g.varAlias[b] = a` makes `b` and `a` share the same stack slot (0-copy). This beats a value copy (which still emits load+store), and is especially beneficial for large stack types such as the 256-byte `%txt`.

### can_slot_rebind constraint (conservative correctness)

Stack-slot rebind is **stricter** than the heap-move `moveEligible`:
- `moveEligible`: source `a` not **read** afterward → move allowed (heap types; after move the source skips free, target unaffected).
- `can_slot_rebind` (`slotRebindSafe`): source `a` has **no subsequent reference (read or write)** → rebind allowed. After rebind, `b` and `a` share the same slot, so a later write to `a` would corrupt `b`'s value.

Computation: `generateFunctionDefinition` calls `computeMoveEligibility` (fills both `moveEligible` and `slotRebindSafe`); `generateMainFunction` calls `computeSlotRebindSafety` (**only** fills `slotRebindSafe`, never touches `moveEligible` — HEAD's main does not enable heap moves, kept disabled to avoid SEGFAULT). Both scan via `stmtContainsVarRefAny` (branch/loop-aware, including `AssignExpression` LHS and loop back-edges).

When `can_slot_rebind` is not satisfied (source referenced later) → **degrade to a full value copy**; never unsafe.

### Disabled scenarios (must use the normal assignment path)

| Scenario | Reason |
|----------|-------|
| stdlib function (`curIsStdLib`) | stdlib contains match/closure/coroutine constructs the reference analysis cannot fully model; rebinding would corrupt shared stack slots (e.g. fmt's internal `it` aliased to a local `n`). Detected via `g.stdModules` (`SetStdModules` from `checker.KnownStdModules()` in `transpiler.go`), falling back to `g.funcOwner[fd.Name]` |
| target is an output param | output param is a caller-passed pointer; rebinding would point it at the local source slot, caller can't read it |
| target is a global var | rebinding makes the global name resolve to a local source slot, breaking global semantics |
| target is a heap-type var | stack-slot rebind only applies to stack types |
| source is a param | params are references to the caller's data; rebinding corrupts the caller's stack frame |
| source is a global var | global address aliased to a local source, semantically wrong |

### Alias invalidation on reassignment

When a variable is reassigned (`b = ...`), `delete(g.varAlias, name)` clears any stale alias first, so the generic assignment path does not write through the old alias into the source's slot via `varAddr(name)`. When setting a new alias, follow the `varAlias` chain for **transitive resolution** (`src := ident.Value; for { if n2,ok := g.varAlias[src]; ok { src = n2 } else break }`) to the ultimate source slot, avoiding multi-hop misalignment.

### Orthogonality with heap move

Stack-slot rebind only affects stack types; it does not involve ownership transfer or `free`, and is fully independent of the heap clone/move machinery.

**Test**: `tests/slot-rebind.no` (covers i64/u64/i128/u128/txt rebind, degrade-to-copy, alias invalidation after target reassignment, param-move degrade, consume passthrough; expected output `42 7 1 1 17 5 5 10 100 99 84`).

## FFI extern str Return Values

FFI extern functions (marked with `#{c}`) return C string pointers (`i8*`) that may point to static memory (e.g. `getenv`, `strerror`) or external buffers (e.g. `strchr` returns a pointer into its argument). Wrapping them directly into `%str-long` would cause `emitHeapFree` to `free()` non-heap memory → UB.

The compiler inserts a safe copy on the FFI extern `str` return path:

1. **NULL check**: if C returns NULL, construct a nil `%str-long` (data=0), making `s == nil` true
2. **Non-NULL**: `strlen` + `malloc` + `memcpy` + null-terminate, copying into an independent heap buffer
3. **PHI merge**: merge both paths and construct the `%str-long` return value

```no
#{c}
strchr = (s str, c i64) (r str)

find = () (r str) {
    r = strchr('hello', 108)   ; C returns a pointer into 'hello'
    ; compiler auto malloc+memcpy copies, r independently owns data
    ; emitHeapFree safely frees r.data at function exit
}
```

This mechanism is consistent with the clib `RetCStrToStr` path (used by built-in functions like `get-env`, `get-wd`), ensuring all C string return values have independent ownership.

## Module-Level Variable Free

Module-level heap variables (`vec`/`str`/`arr`/structs) are compiled as LLVM globals (`@name`); their `data` buffers are malloc-initialized by top-level statements in the `main` entry.

The compiler calls the following before `ret i32 0` in the C entry `main`:
1. `emitHeapFree` — frees top-level local heap variables (not in globalVars)
2. `emitGlobalHeapFree` — iterates `moduleVarTypes`, frees all heap-owning types in `globalVars`

```no
GLOBAL-STR = 'hello'      ; LLVM @GLOBAL-STR = global %str-long zeroinitializer
GLOBAL-VEC = [1, 2, 3]    ; LLVM @GLOBAL-VEC = global %vec zeroinitializer
; top-level statements malloc data and store into global
; emitGlobalHeapFree frees data before main ret
```

This prevents memory accumulation leaks in long-running services (e.g. daemons with loops). No impact on one-shot CLI tools (process exit reclaims via OS).

## Slice Views

A slice expression `arr[1..3]` produces a view (zero-copy) that shares the original array's data. Three fates of a view:

| Target | Behavior | Ownership |
|--------|----------|-----------|
| Local var `v = arr[1..3]` | zero-copy view | shares original data |
| Output param `out = arr[1..3]` | clone (malloc+memcpy) | independent |
| Explicit `[]T` type `v []i64 = arr[1..3]` | clone | independent |

**Reason**: Output params escape to the caller; the original array may be freed before the function exits, so the view must clone to independent data.

## Reassignment and Old Value Free

```no
s = 'hello'     ; malloc data buffer
s = 'world'     ; free 'hello's data, malloc new data
```

When reassigning a heap-owning type, the compiler automatically frees the old value's data before the assignment to prevent leaks.

## Struct Field Free

```no
Node {
    name str
    items []i64
}

n = Node{
    name: 'hello'
    items: [1, 2, 3]
}
; At function exit, recursively free:
;   - n.name.data (%str-long field)
;   - n.items.data (%vec field)
```

When freeing a struct, all fields are traversed; heap-owning type fields are recursively freed.

## Fixed Array Reassigned to Slice

```no
local [4]i64 = [100, 200, 300, 400]   ; local is fixed array (16 bytes)
local = [100, 200, 300]                ; reassigned as slice (24 bytes)
```

Fixed arrays (`%arr`, 2 fields) and slices (`%vec`, 3 fields) have different memory layouts. On reassignment the compiler automatically allocates a new `%vec` variable and redirects all subsequent accesses to avoid buffer overflow.

## Verified Test Cases

Tests are in `tests/mem-safety/`:

| Test | Verifies |
|------|----------|
| `deep-clone.no` | `b = a` deep clone ([]i64/[]str/str/struct) independence |
| `deep-free-str.no` | `[]str` deep free |
| `deep-free-nested-vec.no` | `[][]i64` deep free + push moved |
| `deep-free-struct-vec.no` | `[]MyType` deep free (recursive struct) |
| `struct-field-leak.no` | struct field heap data free |
| `slice-view-escape.no` | slice view assigned to output param clone |
| `reassign-leak.no` | reassignment old value free |
| `vec-push-leak.no` | vec.push moved marking |
| `ffi-str-return.no` | FFI extern str return value safe copy |
| `global-heap-free.no` | module-level heap variables freed at main exit |

## Known Limitations

### map Container
hashmap does not implement deep free of key/value; map container heap data leaks.

### Loop Temporary Variables
```no
loop {
    s = 'temp'   ; each iteration mallocs new data, old data not freed
}
```

### Slice View + Original Array move
```no
view = arr[1..3]   ; view shares arr.data
arr = [9, 8, 7]    ; free old arr.data → view dangling
```

### async Shared Data
When async threads share heap data with the main thread, free order is nondeterministic.
