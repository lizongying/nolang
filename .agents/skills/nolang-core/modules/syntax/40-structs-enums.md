# Structs, enums, methods, slices, indexing, interfaces, generics

> 视图切分自原 `nolang-syntax/SKILL.md`，正文未改写；索引与加载判据见 [`../m10-syntax.md`](../m10-syntax.md)。

### Structs & Methods

Struct definitions and literals must both use multi-line form, with each field on its own line, fields not separated by commas, and no trailing comma.

A struct can implement one or more interfaces by listing them after the struct name. When implementing interfaces from **other modules**, the interface name must include the module prefix (e.g. `sql.db`, not `db`). See [Cross-Module Type References](#cross-module-type-references).

```no
; Same-module interface: no prefix needed
user json {
    name str
    age i64
}

; Multiple interfaces
file enter, leave {
    path str
}

; Cross-module interface: prefix required
; db, rows, stmt are defined in the sql module
db-mysql sql.db {
    fd i64
}

u = user {
    name: 'Alice'
    age: 30
}
u.name = 'Bob'
u.age = 25
print(u.name)

user.greet = () {
    print('Hello, ' - .name)
}
```

### Struct field inline tags

`#{inline}` on a **struct field** decides whether the field is stored **by value inside its host**
(`%pt`) or as a **pointer** (`ptr`). The annotation goes **above** the field on its own line or
**trailing** on the field's line — the same-line prefix form is a parse error, exactly as for every
other annotation target:

```no
pt {
    x i64
    y i64
}

holder {
    inl pt #{inline=true}; by value: the struct lives inside holder
    out pt #{inline=false}; pointer: the field holds a pointer to pt
    bare pt #{inline}; shorthand for inline=true
}
```

`#{inline}` is a **boolean**; see [`#{inline}` — the three spellings](#inline--the-three-spellings).
The full truth table:

| Spelling | Field layout |
| --- | --- |
| `#{inline}` / `#{inline=true}` / `#{inline=1}` | **by value**, inlined in the host (`%pt`) |
| `#{inline=false}` / `#{inline=0}` | **pointer** (`ptr`) |
| absent | depends on `NOLANG_FIELD_PTR` (see below) |
| `#{inline=foo}`, `#{inline='true'}` | **compile error** — not a boolean |

**The default when there is no annotation is decided by `NOLANG_FIELD_PTR`.** `mir.FieldPtrLayout`
is `os.Getenv("NOLANG_FIELD_PTR") != ""` and is **off by default**; turning it on enables the
"Phase 1 layout flip", which changes a struct field's default from by-value to pointer.

| Field | Flag off (default) | Flag on (`NOLANG_FIELD_PTR=1`) |
| --- | --- | --- |
| no annotation | `%pt` (by value) | `ptr` (pointer) |
| `#{inline}` / `#{inline=true}` | `%pt` | `%pt` |
| `#{inline=false}` | `ptr` | `ptr` |

In other words **the annotation is always honoured, regardless of that environment variable** — the
flag only changes the default when no annotation is written. So under the default configuration
`#{inline=false}` is "actively ask for a pointer" while `#{inline=true}` merely restates the layout
that already applies; once the flip is enabled their roles swap.

Two checker rules (TraceID `fieldtag1`, `ValidateFieldTags`):

1. **Struct-typed fields only.** `#{inline}` on a scalar / `str` / `vec` / array / slice / map /
   option / pointer field is rejected, because those are *always* stored inline — the annotation
   would assert nothing. `#{inline=false}` on such a field is rejected too, since it would claim
   the opposite of what the compiler does.
2. **No by-value cycles.** An inlined struct is stored *inside* its host, so `a { #{inline} b b }`
   together with `b { #{inline} a a }` has no finite size and is rejected (message: `inline fields
   form a cycle: a -> b -> a`). Direct self-reference is the degenerate case. Only `=true` creates a
   by-value edge, so mutually recursive structs are legal when **both sides write
   `#{inline=false}`**.

   > ⚠️ **"No annotation" does NOT mean "not inline".** Under the default configuration an
   > unannotated struct-typed field *is* by-value, so `a { x b }` with `b { y a }` (neither
   > annotated) is **not** legal by default — it fails with
   > `identified structure type 'b' is recursive`. Omitting the annotation only means "pointer"
   > when `NOLANG_FIELD_PTR=1`. See [Recursive types need `inline=false`](#recursive-types-need-inlinefalse)
   > below for the full picture.
   >
   > And note the checker does **not** catch this: it only adds an edge for an *annotated* field, so
   > the unannotated cycle reaches LLVM as an opaque `opt:` error. See the compiler-change skill.

#### Recursive types need `inline=false`

A struct that inlines itself has infinite size, so a self-reference cannot be by value. Since a
struct field is by value under the default configuration, **`#{inline=false}` is the only way to
write a self-referential type without flipping `NOLANG_FIELD_PTR` for the whole program**:

```no
node {
    v i64
    next node #{inline=false}; pointer: gives node a finite size
}

n node
n.next.v = 2
print(n.next.v); 2
```

Without it (or with `#{inline=true}`) LLVM rejects the type:
`identified structure type 'node' is recursive`.

Ownership of a forced-pointer field is handled by the compiler, not the author:

- the pointee is allocated **lazily on first write** (`@__nolang_get_<T>`: malloc + zero);
- the host's drop (`@__nolang_drop_<T>`) **null-checks and frees** it at scope exit;
- a by-value copy (`b = a`, pass by value, container element write) **deep-copies** the pointee,
  so two hosts never share one allocation and it is never freed twice.

End-to-end coverage: `tests/field-inline-annotation.no` (its recursive case is the
discriminator — if `inline=false` stopped forcing a pointer, that file would fail to compile).

> Implementation note: layout lives on `FieldInfo.Layout` (`FieldLayoutInline` /
> `FieldLayoutPointer` / `FieldLayoutDefault`) and is read by `Module.FieldIsPointer`. It is a
> SEPARATE axis from `FieldInfo.Tag`, which still decides ownership — a forced pointer is
> `FieldTagOwned` and is dropped like any other owning field. Never gate a decision on
> `mir.FieldPtrLayout` when the real question is "is this field a pointer"; ask
> `FieldIsPointer` / `StructHasPtrFields`, or `#{inline=false}` fields leak.

### Enums

Enum definitions use the same syntax as structs, but with commas between values. Values auto-increment from 0.

```no
; red=0, green=1, blue=2
color {
    red,
    green,
    blue,
}

; This is a special enum, can have types, commas, and aliases
enum-name {
    a t,
    b u,
    c v,
}

; Note this is a regular struct, multiple fields without commas
struct-name {
    a t
    b u
    c v
}
```

**Tagged enums (payload variants).** A variant may carry named payload fields using the
parenthesized form `variant(field type)`; a payload-less variant is a bare name. A variant
may carry **multiple** fields (multi-field payloads are boxed in a synthesized heap struct):

```no
; tagged enum — tags are 0,1,2... in declaration order
result {
    ok(v t),
    nil,
    err(e str),
}

; payload field v of type t
; no payload
; payload field e of type str (user-defined enum — unrelated to option)

shape {
    circle(r f64),
    rect(w f64, h f64),
    dot,
}
; single field
; multiple fields
; no payload
```

Match on the variant name; payload-bearing variants support destructuring binding (multiple
fields bind positionally):

```no
r result = ok(42)
r: {
    ok(v) -> print(v); binds ok's payload to v

    nil -> print('empty')

    err(e) -> print(e)
}

s shape = rect(3.0, 4.0)
s: {
    circle(r) -> print(r); binds the single field

    rect(w, h) -> print(w * h); binds fields positionally

    dot -> print('dot')
}
```

Variants can be constructed directly in **expression position** — as a function argument, or
as another variant's payload:

```no
x f64 = perimeter(rect(3.0, 4.0)); construct inline and pass
o outer = wrap(a(5)); payload is itself an enum
```

**Namespacing and bare-name resolution.** Internally the compiler registers variants under
their fully-qualified names (`module.enum.variant`, e.g. `option.option.ok`,
`some-mod.my-result.ok`), so identically-named variants in different enums never collide. At
the source level you write the bare name (`ok`, `rect`); the compiler resolves it to the
full name **from the static type of the matched/constructed variable**. Two enums can
therefore both have an `ok` variant with different tag orders and still be distinguished
correctly.

The old space-separated form `ok t` is equivalent to `ok(v t)` (field name omitted).
Tagged-enum variant names are registered in the compiler's enum-variant table, which
drives match lowering and exhaustiveness checking.

**Builtin tagged enums (`#{buildin}`).** Prefixing a tagged enum with `#{buildin}` marks it
as a *builtin* enum: its variants (names, order, payload types) drive matching and
exhaustiveness, but the underlying representation and construction come from the builtin
runtime — no user-visible struct/union is generated. The `?t` option type is declared this
way in `src/std/global.no`:

```no
#{buildin}
option {
    ok(v t),
    nil,
    err(e err.err),
}
; tag 0
; tag 1
; tag 2 — std structured error, not str
```

`#{buildin}` takes no value (`#{buildin}`, not `#{buildin=NAME}`); it applies to builtin
function stubs and builtin enums alike.

#### Enum annotations and memory layout

Enums accept `#{...}` annotations under the same placement rule as every other target (see
[Annotation placement](#annotation-placement-only-two-legal-positions)): **above**, on its own
line, or **trailing**, on the target's line after it. The same-line **prefix** spelling is a
compile error.

There are **two** legal places an enum annotation may sit:

1. **Above the whole enum** — attaches to the enum definition itself (e.g. `#{buildin}`,
   `#{inline=true}`);
2. **On an individual member** — above it on its own line, or trailing on its line. This works
   for the values of a C-style enum and the variants of a tagged enum alike.

```no
; 1. whole enum: own line above
#{inline=true}
box {
    full(v i64) #{doc='has value'},
    empty #{doc='empty'},
}

; 2. one variant: own line above

; 2. one variant: trailing on its line

; `#{inline=false}` states the default explicitly
#{inline=false}
plain {
    a,
    b,
}

color {
    red #{deprecated},
    green #{deprecated},
    blue,
}
; C-style enum values take the same two spellings
```

**An annotation never changes the enum's structure.** Variant names, declaration order (tags),
payload fields and their types are untouched, and matching / exhaustiveness checking are
unaffected — an annotation is metadata on the definition.

**Memory layout (the stack form).** A tagged enum is stored as a small by-value object rather
than a heap-scattered payload:

```llvm
%tenum_<name> = type { i64 tag, [N x i64] payload }
```

- `tag` is the discriminant (`i64`), `0, 1, 2...` in declaration order;
- `payload` is a slot array **shared by all variants** (union semantics — every variant writes
  its fields into the same storage, so field access bitcasts at a slot offset);
- `N` is the slot count of the **widest** variant, and is **at least 1** (never zero-width).

| Case | `N` | Size |
| --- | --- | --- |
| every variant payload-less (a pure tag enum) | 1 | **16 bytes** |
| widest variant is `i64` / `f64` / a pointer | 1 | 16 bytes |
| widest variant is `?T` | 2 | 24 bytes |
| widest variant is `str` / `vec` / `[]T` | 3 | 32 bytes |
| widest variant is a struct `T` | sum of `T`'s fields' slots (expanded recursively) | depends on `T` |

So the layout **starts at 16 bytes by default** and grows only when some variant's payload is
actually wider — "fixed 16 bytes by default" and "size varies per variant" describe the same rule.

#### `#{inline}` — the three spellings

`inline` is a **boolean** annotation with three spellings, and the value is honoured:

| Spelling | Meaning |
| --- | --- |
| `#{inline}` | shorthand for `inline=true` (a bare key is true) |
| `#{inline=true}` | explicit true (`#{inline=1}` also works) |
| `#{inline=false}` | explicit false (`#{inline=0}` also works) |

A non-boolean value is a **compile error** (`#{inline=foo}`, `#{inline='true'}`), not a silent
"true" — `ValidateFieldTags` (TraceID `fieldtag1`) reports it for both struct fields and enum
definitions.

Writing `#{inline=true}` **above the whole enum** is the explicit marker for the stack layout
("this enum uses the stack form"); the compiler records it in `TaggedEnumInfo.Inline`. The
default layout is already the stack form, so on an enum the annotation is byte-identical in
codegen whichever value you write — `#{inline=false}` merely restates the default. The marker
makes the intent explicit and is the stable switch should the default ever change.

On a **struct field**, however, `#{inline}` is load-bearing — see
[Struct field inline tags](#struct-field-inline-tags):

- `#{inline}` / `#{inline=true}` → the field is stored **by value** inside its host (`%pt`);
- `#{inline=false}` → the field is a **pointer** (`ptr`);
- absent → whichever the `NOLANG_FIELD_PTR` flag selects (off by default: by value).

Writing `#{inline=false}` on both sides is also what lets mutually recursive structs be legal
with the choice spelled out.

> `#{inline}` on an **individual variant** does **not** change layout today; it is retained only
> as that variant's metadata.

**Rule: enum values must always be referenced using qualified form `enum-type.value`, never as bare names.** This prevents naming conflicts and ensures external packages cannot use values directly without qualification.

```no
#{fmt-skip}
; ❌ Wrong: bare enum value
kind = null
yes = e.is(io)
```

```no
; ✅ Correct: qualified form
kind = json-kind.null
yes = e.is(code.io)
```

> Enum types can be used as struct field types, function parameter types, and return value types. Both inside and outside the module that defines the enum, enum values should be referenced using the `enum-type.value` form.

### Method Conventions

Methods are defined on types, using `.` to reference the receiver. The receiver does not need to be explicitly declared as a parameter; it is referenced via `.` inside the method body.

**Rules:**
1. Method names use the `type.method` format; type must be a previously defined type
2. Receiver is accessed via `.` inside the method body
3. Call with `receiver.method(args)` syntax
4. Return values go in the second set of parentheses
5. Boolean returns must use `bool` type, not `i64`
6. Avoid reserved words as method names (e.g. use `matches` not `match`)

**Examples:**

```no
; str method — byte-wise build via a []byte buffer (str index writes are banned)
str.to-upper = () (out str) {
    n = .len-bytes()
    b []byte = with-cap-len(n, n)
    i <- [0..n): {
        c ?byte = .byte(i)
        v = c: {
            ok -> it

            nil -> 0

            err -> 0
        }
        {
            v >= 97 && v <= 122 -> b[i] = v - 32

            -> b[i] = v
        }
    }
    out = b.to-bytes-str()
}

; char method
char.is-digit = () (result bool) {
    result = false
    . >= 48 && . <= 57 -> result = true
}

; struct method
user {
    name str
    age i64
}

user.greet = () {
    print('Hello, ' - .name)
}

; Calling methods
s = 'hello'
u = s.to-upper(); receiver.method()
c char = 5
d = c.is-digit(); receiver.method()
u = user {
    name: 'Alice'
    age: 30
}
u.greet()
```

### Slices (Views, Not New Types)

Slicing (`arr[1..3]`, `vec[1..3]`, `str[1..3]`) produces a **view** into the original data — it does **not** copy data or create a new independent type. The slice is a lightweight descriptor (pointer + length + capacity) that shares the original buffer:

- Modifications through a slice affect the original data, and vice versa
- The slice does not own the data; it becomes invalid when the original is released
- Methods of the original type are directly available — no "inheritance" mechanism needed

| Original type | Slice view type | Available methods |
| ------------- | --------------- | ----------------- |
| `arr` (`[n]t`) | `[]t` (`vec`) | All `[]t` methods (`len`, `push`, `pop`, `contains`, `reverse`, `clone`, `fill`, `to-arr`, etc.) |
| `vec` (`[]t`) | `[]t` (`vec`) | Same as above |
| `str` | `str` | All `str` methods (`to-upper`, `to-lower`, `index`, `contains`, `slice`, `copy`, `fill`, etc.) |

```no
; arr slice → vec view, shares arr's memory
a [5]u8 = [0, 1, 2, 3, 4]
s = a[1..4]; s is []u8 view into a's buffer
n = s.len(); vec.len()

; vec slice → vec view, shares vec's memory
v = [10, 20, 30, 40, 50]
s = v[2..]; s is []i64 view
s.reverse(s.len()); vec.reverse

; str slice → str view, shares str's memory
s = 'Hello World'
sub = s[6..]; sub is 'World' view
upper = sub.to-upper(); str.to-upper

; Modifying through a slice affects the original
data = [10, 20, 30, 40, 50]
view = data[1..4]; view = [20, 30, 40]
view[0] = 99; modifies data[1] too — shared memory
```

### Indexing

```no
; str[i] -> char (Unicode code point, NOT a byte)
str[i]

; Get element from arr, vec
arr[i]
vec[i]

; Get value from map
map[str]
```

> `str[i]` returns `char`; `str[a..b]` returns a `str` view; `char` implicitly converts to `str`. See [Indexing & Slicing](#indexing--slicing).

### Safe Indexing (安全索引)

`arr` / `vec` / `slice` / `str` / `txt` 的 `v[i]` **恒返回 `?elem`**（越界为 `nil`）。
`#{index-out=DEF}` 注解**已废弃并移除（D11）**：残留注解是编译错误，不存在「越界自动取默认零值」的捷径。
必须显式消费，只有三种写法：

**READ — 值 match 或简写 `?=`**

```no
; ✅ 完整写法：值 match，`-> _` 是元素型别的零值臂
get-elem = (arr []i64, i i64) (out i64) {
    out = arr[i]: {
        ok -> it

        -> _
    }
}

; ✅ 完整写法：陈述 match
read-statement = (arr []i64, i i64) (out i64) {
    arr[i]: {
        ok -> out = it

        -> _
    }
}

; ✅ 简写：上抛（函数有 ?T 结果参数，或包在块 match 主体里）
safe-get = (arr []i64, i i64) (res ?i64) {
    x ?= arr[i]; OOB → res = None (no crash)
}
```

**WRITE — place-match 或简写 `?=`**

```no
arr[i]: {
    ok -> it = x

    -> _
}

arr[i] ?= x; 上抛简写
```

**MAP 例外**：`m['k'] = 1` 直接写、不需要解包（下标是键，写入是 put/insert，恒有定义）；
但 **map 读取 `m['k']` 仍返回 `?V`，必须解包**。`m[k] ?= v` 是编译错误（get 的副本写不回 map）。

**嵌套读取**：内层 match 会重绑 `it`，所以内层要用具名绑定 `ok(it1) -> it1`。
自 2026-11-06 起，把值 match 直接写在 place-match 的 `it = ...` 右侧**会正确写回容器**
（lowering 会把该值 match 提升为合成暂存再重绑 `it`，回归：`tests/match.no` 的
`place-write-value-match` 段）。**首选直接写法**：值 match 的结果直接绑到最终要用的那个名字，
不要先塞进一次性暂存再拷一次。

```no
#{fmt-skip}
; ✅ 直接绑定目标变量
at = argt[i]: {
    ok -> it

    -> _
}
s.farg.push(at)

; ❌ 不要为单次使用引入一次性暂存
ixout1 = argt[i]: { ok -> it  -> _ }
at = ixout1
s.farg.push(at)
```

「把值 match 提到臂外的具名暂存、臂内再 `it = ixout1`」**与直接写法并不等价**，不能当成
可自由互换的排版选择：

- **求值时机**：直接写法只在外层 `ok` 臂命中时求值；提到臂外后，外层 `nil` / `err` /
  catch-all 也会先读一次，下标表达式带副作用时行为不同。
- **绑定与所有权**：`it` 只在 match 臂内有效且指向元素位置，拷回必须留在臂内；具名暂存是
  一个新的绑定，有自己的 move / 所有权与作用域退出销毁，不只是换个名字。
- **内层绑定形态**：嵌套时内层必须用 `ok(it1) -> it1`；臂外暂存用的 `ok -> it` 不能原样
  搬回嵌套位置。

只有确实要在多处复用同一个值时才提升，且要用有意义的变量名，不要留 `ixout…` 这类
迁移工具生成的占位名；单次使用一律直接绑定。

#### 用 `?=` 与块 match 收掉剩余的一次性暂存

上面说的是「单次使用就直接绑定」。再往前一步：安全索引返回的是 `?T`，**所以当函式的
结果参数本身就是 `?T` 时，连值 match 都不必写，`?=` 一行直接上抛**：

```no
#{fmt-skip}
; ✅ 函式有 ?T 结果参数：无暂存、无 match
at ?= argt[i]
s.farg.push(at)
```

**函式结果不是 option 时**（最常见：无返回值的辅助函式，或只是往容器里写回），用这种
「类似 try」的写法把该陈述包起来：

```no
#{fmt-skip}
handle = (x i64) {
    {
        y ?= x + 1
        print(y)
    }: {
        err -> _

        -> _
    }
    print('continues afterwards')
}
```

⚠️ **这不是新的 `try` 语法，语言里也没有 `try` 关键字。** 它就是一个**陈述层级的块
match**（`{ ... }: { ... }`），主语是三态 `?T`；`?=` 把失败上抛给**最近的**块 match
（详见 *Error Propagation with `?=`* 与 *Block Match*）。失败时块主体剩下的陈述——包括
后续循环迭代——全部跳过，臂只执行一次，然后**继续执行块之后的代码**，不是函式
`return`。`err -> _` 丢弃错误，`-> _` 兜底丢弃成功结果；要处理错误可写
`err -> print(it.msg())`。

⚠️ **语义差异要讲清楚**（实测，别凭直觉）：`?=` 失败时**目标照样被写成零值**——`i64` 得
`0`、`str` 得 `''`、`out[1]` 得 `0`，这点和旧的 catch-all `-> _` 完全一致。真正的差异只有
一处：`?=` 会**跳过块主体中它之后的陈述**（在 `?T` 函式里是提前 `return`，在块 match 里
是直接转到臂）。

所以：**块主体里只有这一条陈述时，新旧写法行为完全等价**（这也正是本仓库批量改写的
形态）；只有当同一块主体里 `?=` 后面还有别的陈述（循环计数、后续赋值）时才会分岔，那些
站点不能机械替换。

另一个容易踩的点：**成功时臂也会执行一次**，catch-all 收到成功值（`-> print('x')` 会打印）。
所以臂里只该放丢弃或纯处理；`err -> _` / `-> _` 这种形态没有副作用。

由此得到清理 `ixout…` 一次性暂存的顺序：

1. 值只在同一处用一次 → 直接绑到最终目标名（见上文）。
2. 函式有 `?T` 结果参数 → `TARGET ?= EXPR`，无需暂存。
3. 否则 → 把该陈述放进块 match 再写 `TARGET ?= EXPR`；place-match 写回
   （`SUBJ: { ok -> it = temp  -> _ }`）也直接缩成 `SUBJ ?= EXPR`。
4. 只有确实要在多处复用同一个值时才保留具名暂存，且用有意义的变量名。

两个边界：`?=` 在既没有 `?T` 结果参数、也不在块 match 主体内时是编译错误
（`l86f91me`）；**带型别宣告的 LHS 不能配 `?=`**（`ai i64 ?= a[i]` 是语法错误
`f5ikdmuc`）。需要 `byte → i64` 这类显式提升时，改成带型别宣告的直接值 match：

```no
#{fmt-skip}
ai i64 = a[i]: {
    ok -> it

    -> _
}
```

> **Consume or fail:** 把 `?T` 当普通值用（例如 `crc = crc ^ s[i]`、`f(s[i])`、`v i64 = a[i]`）
> 是**编译硬错误**，编译器会提示改用 match 或 `?=`（无 `?T` 结果参数时用块 match 包住）。
> **Guarantee:** 越界永不静默崩溃 —— 不是被 match 处理、就是被 `?=` 上抛。

### Standard Library Struct Pattern

The standard library uses a consistent pattern for data structures and I/O abstractions: define a struct, then attach methods to it. The receiver is accessed via `.` inside the method body, and nested fields via `self.field` (or `.field` for single-level).

```no
; Data structure: stack (LIFO)
stack {
    data []i64
    n i64
}

stack.push = (val i64) {
    .data[.n] = val
    .n = .n + 1
}

stack.pop = () (val ?i64) {
    .n == 0 -> {
        val = nil
        return
    }
    .n = .n - 1
    val = .data[.n]
}

; Usage
buf [128]i64
s = stack {
    data: buf
    n: 0
}
s.push(42)
val = s.pop()
```

The same pattern applies to `heap`, `deque`, `path`, `regexp`, `file`, `io-reader`, `io-writer`, `sse-client`. See the [standard library reference](file://../nolang-std/SKILL.md) for the full API.

### Networking Modules

The standard library includes comprehensive networking modules under `std/net/`:

- `std/net/http` — HTTP/1.1 client (GET, POST, PUT, DELETE, PATCH), supports TLS
- `std/net/http2` — HTTP/2.0 client (RFC 7540, h2c prior knowledge mode)
- `std/net/http3` — HTTP/3.0 client (RFC 9114, over QUIC)
- `std/net/ws` — WebSocket client and server (RFC 6455)
- `std/net/quic` — QUIC protocol (RFC 9000)
- `std/net/tls` — TLS 1.2/1.3 client connection (pure Nolang)
- `std/net/sse` — Server-Sent Events client (W3C EventSource), supports TLS and auto-reconnect
- `std/net/client` — High-level TCP client with reconnect support
- `std/net/server` — HTTP server
- `std/net/dns` — DNS resolution
- `std/net/url` — URL parsing
- `std/net/cookie` — HTTP Cookie handling
- `std/net/multipart` — Multipart form data
- `std/net/hpack` — HPACK header compression (for HTTP/2)
- `std/net/proxy` — Proxy support
- `std/net/pool` — Connection pool
- `std/net/unix` — Unix domain sockets
- `std/net/ip` — IPv4 address parsing and classification

```no
; SSE client usage
client = sse.sse-connect('http://localhost:3000/events'); returns ?sse-client
client: {
    nil -> print('connection failed')

    -> {
        ! {
            ev = client.next-event(); returns ?sse-event
            ev: {
                nil -> break; EOF

                err -> print(it.msg()); error

                -> print(ev.data); event data
            }
        }
        client.close()
    }
}
```

### Struct Field Method Calls

Method calls on struct fields via `self.field` (abbreviated `.field`) are fully supported. The type checker resolves the field type from the struct definition, so return types are correctly inferred:

```no
; .recv-buf is a str field → .recv-buf.slice() returns str
data = .recv-buf.slice(0, .recv-buf-len); correctly inferred as str

; .tls-c is a tls.conn field → .tls-c.send() works directly
written = .tls-c.send(req, req.len())
```

### Interfaces

```no
; Define interface
json {
    to-json()
}

; Interface default implementation
json.to-json = () {
}

; Interface implementation
user json {
    name str
    age i64
}

; Override + call parent implementation
user.to-json = () {

    ; Parent implementation
    ..to-json()
}

user.other = () {

    ; Current implementation
    .to-json()

    ; Parent implementation
    ..to-json()
}
```

#### Special Interfaces: enter / leave

Types that implement the `enter` / `leave` interfaces are automatically called when entering and leaving a scope:

```no
file enter, leave {
    path str
}

file.enter = () {
    .open()
}

file.leave = () {
    .close()
}

read-file = () {

    ; Auto f.enter()
    f = file {
        path: 'data.txt'
    }

    ; Use f
    ; Auto f.leave()
    read(f)
}
```

### Generics

```no
arr_to_vec = (arr [n]t) (out []t) {
    i <- [0..n): {
        out[i] = arr[i]
    }
}
```
