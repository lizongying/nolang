---
sidebar_position: 4.1
---

## Data Exchange

### json — JSON Parsing and Generation

`json` stores a JSON tree in a **heap-allocated node pool** (`json-pool`), supporting nested arrays/objects, string/number/bool/null scalars, escape sequences and exponent-format numbers. The pool is built from dynamic `[]vec` buffers, so there is **no fixed capacity cap** — total node count and children-per-node are bounded only by available memory. The high-level `json` struct wraps the pool plus a root index; its `pool` field is annotated `#{inline=false}` (a pointer field), which lets `json` be safely returned and copied by value.

```no
; Parse: returns ?json (ok = value, err = failure with a specific reason)
j = json.parse('{"name":"Alice","age":30,"items":[1,2,3],"active":true}')
j: {
    ok -> {
        name, found = it.get-str('name')   ; scalars: get-str / get-num / get-bool / get-i64
        age, ok2 = it.get-i64('age')
        items = it.get('items')            ; child node, returns ?json
        s = it.stringify()                 ; serialize back to string
    }
    err(e) -> print(e)
}

; Build: start from an empty (null) json, then set / arr-push
j2 = json.new()
j2.set-str('key', 'value')
j2.set-i64('count', 3)
j2.arr-push-str('a')
print(j2.stringify())
```

High-level API (methods on `json`):

- `json.new()` — create an empty (null) `json`
- `json.parse(s)` — parse, returns `?json`; the `err` branch carries the reason (`empty input` / `parse error` / `trailing characters`)
- `j.stringify()` — serialize to a string
- Read by key: `j.get-str(key)` / `j.get-num(key)` / `j.get-bool(key)` / `j.get-i64(key)` (each returns `(val, ok)`); `j.get(key)` returns a child `?json`
- Direct root scalar access (when root is a scalar): `j.str()` / `j.num()` / `j.bool()` / `j.i64()` / `j.str-val()`
- Write: `j.set-str(key,val)` / `j.set-num` / `j.set-i64` / `j.set-bool` / `j.set-null` / `j.set-key(key, val json)`
- Array: `j.arr-get(i)` / `j.arr-len()` / `j.arr-push(val json)` / `j.arr-push-str` / `j.arr-push-num` / `j.arr-push-bool`
- Object enumeration: `j.obj-len()` / `j.obj-key(i)` / `j.obj-keys-str()` / `j.delete-key(key)`
- Type checks: `j.kind()` and `j.is-null()` / `j.is-obj()` / `j.is-arr()` / `j.is-str()` / `j.is-num()` / `j.is-bool()`

**Pool capacity (no fixed cap).** The pool grows automatically on heap `[]vec`:

- Total nodes and children-per-node have no hard limit (only memory-bound). The old `JSON-MAX-NODES = 128` / `JSON-MAX-CHILDREN = 32` caps are gone.
- `json.parse` returns `err` only on a genuine syntax error — there is no longer a "pool exhausted" branch.
- `j.set*` / `j.arr-push*` return `false` only on a kind mismatch. `j.overflowed()` is kept for backward compatibility and now always returns `false`.

> **`json` copy semantics — `#{inline=false}`.** Because `pool` is a pointer field, a child handle returned by `j.get` / `j.arr-get` deep-copies the whole pool: the handle is an independent snapshot of the parent, and writes through it do not propagate back. Also, matching a `?json` option consumes its value — matching the same option a second time falls into the `nil` branch. To reuse one parsed result, finish the work inside a single `match` (use `it`) or bind it once into a plain `json` local.

Low-level API (`json-pool` / `json-value`) — operate by node index; the pool is a single-level struct so it is safe to return/copy by value:

- `p = json.new-pool()`, or `p = json-pool {}` then `p.init()` (sets `nodes`/`strs`/`ec`/`ek`/`en` to `with-len(0)`)
- `idx = p.alloc()` — allocate a node (heap vec auto-grows); `p.add-child(node-idx, child-idx, key)` — append a child via the `ec`/`ek`/`en` edge linked-list
- `node-idx, next-pos, ok = p.parse(s, pos)` — parse one value from `pos`
- `p.get-key(obj-idx, key)`; `p.arr-get(arr-idx, i)`; `p.arr-len(arr-idx)`
- `p.get-kind(idx)` / `p.get-str(idx)` / `p.get-num(idx)` / `p.get-bool(idx)`
- `p.set-key(obj-idx, key, val-idx)`; `p.copy-tree(src, idx)` (recursive deep copy)
- `p.stringify(node-idx)` — serialize a specific node

### toml — TOML 1.0 Parsing and Generation

`toml` stores a document in `toml-doc` and supports comments, bare/quoted/dotted keys, tables, array-of-tables, basic/literal/multiline strings, decimal/hex/octal/binary integers, floats, booleans, date-time literals, arrays, and inline tables. Values without a dedicated Nolang type remain available as their original TOML literals.

```no
cfg = toml.parse('title = "Nolang"\n[server]\nport = 8080\n[[server.backends]]\nname = "local"')

doc = toml.new()
doc.put('name', '"nolang"')
doc.put('server.port', '8080')
doc.put('enabled', 'true')
raw = toml.get(doc, 'server.port')
text = toml.stringify(doc)
```

Common functions:

- `toml.new()` — create an empty document
- `toml.parse(text)` — parse and return `?toml-doc`
- `doc.put(key, raw-value)` — validate and insert/update a TOML value
- `toml.get(doc, key)` — retrieve the raw value
- `toml.get-str`, `toml.get-i64`, `toml.get-f64`, `toml.get-bool` — retrieve scalar values
- `toml.stringify(doc)` — serialize the document

The document limits are 4 keys, 2 ordinary tables, and 2 array-of-tables; keys are limited to 32 bytes and values to 128 bytes.

### yaml — YAML 1.2 Parsing and Generation

`yaml` keeps a YAML 1.2 core-schema document tree in a node pool (`yaml-pool`). It supports block mappings, block sequences, flow collections (`[...]` / `{...}`), single-quoted / double-quoted / plain scalars, block scalars (`|` / `>` with `-` / `+` chomping), comments, document markers (`---` / `...`) and multi-document streams, anchors (`&`) and aliases (`*`), merge keys `<<`, explicit keys (`? k`), core-schema type inference (null / bool / int / float / `.inf` / `.nan`), dotted-path lookup, and JSON / YAML serialization.

```no
doc ?yaml = yaml.parse('name: Alice\nserver:\n  port: 8080\ntags:\n  - a\n  - b\n')
doc: {
    nil -> print('nil')

    err -> print(it)

    -> {
        name, ok = it.find-str('name')
        port, ok2 = it.find-i64('server.port')
        print(name)
        print(port.to-str())
        print(it.to-json())
        print(it.to-yaml())
    }
}
```

Common functions and methods:

- `yaml.parse(text)` / `yaml.parse-all(text)` — parse, returning `?yaml` (the `err` branch carries a `line L, column C` position)
- `yaml.valid-of(text)` / `yaml.error-of(text)` — validate only, without building a document
- `yaml.strip-bom(text)` — strip a UTF-8 BOM
- `doc-count()` / `doc(i)` — multi-document access
- `kind()`, `is-null()`, `is-bool()`, `is-int()`, `is-float()`, `is-nan()`, `is-str()`, `is-seq()`, `is-map()`, `len()`, `text()`, `tag()`, `anchor()`, `line-of()` — node information
- `str()`, `i64()`, `f64()`, `bool()`, `scalar-text()` — value access; all return `(value, ok)`
- `at(i)`, `value(i)`, `key(i)`, `key-node(i)`, `get(key)`, `find(path)` — child nodes and keys
- `get-str` / `get-i64` / `get-f64` / `get-bool`, `find-str` / `find-i64` / `find-f64` / `find-bool` — typed shortcuts
- `to-json()`, `to-json-all()`, `to-yaml()`, `stringify()`, `source-of()` — serialization

Kind constants: `YAML-KIND-NULL` / `BOOL` / `INT` / `FLOAT` / `STR` / `SEQ` / `MAP` (0–6). Scalar-style constants: `YAML-STYLE-PLAIN` / `SINGLE` / `DOUBLE` / `LITERAL` / `FOLDED` / `ALIAS` (0–5), plus `YAML-STYLE-BLOCK` / `FLOW` (6–7).

> **Caveat 1**: child views are returned as `(out yaml, ok bool)`, not `?yaml`. This is an API-style choice, not an ownership limitation — a child sharing the parent's node pool is perfectly safe. Prefer the path helpers (`find-str` / `find-i64` / ...) for lookups.
>
> **Caveat 2**: inside nested matches `it` is restored **per level**. An inner match's arm rebinds `it` to its own subject (the arm body must see the inner value), but as soon as that arm ends `it` reverts to the enclosing arm's subject — so this is safe:
>
> ```no
> d ?yaml = yaml.parse(src)
> d: {
>     nil -> print('nil')
>
>     err -> print(it)
>
>     -> {
>         print(it.find-str('a'))        ; outer it = the document
>         v ?yaml = it.at(0)
>         v: {
>             nil -> print('none')
>             err -> print('err')
>             -> print(it.find-str('b')) ; inner it = the child
>         }
>         print(it.find-str('c'))        ; ✅ it is the document again
>     }
> }
> ```
>
> The one exception is the outermost match: after it ends `it` still holds the last arm's value (it is not restored), so never rely on `it` outside a match. To use the value across levels, still copy it into a named local (`doc = it`) or use a destructuring binding `ok(v) -> ...`.
>
> **Caveat 3**: `.nan` parses to a real NaN — `f64()` returns NaN and `x != x` is true. JSON has no NaN, so `to-json()` emits `null` for it; `to-yaml()` emits `.nan`.

---
