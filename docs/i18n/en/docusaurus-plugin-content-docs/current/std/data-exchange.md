---
sidebar_position: 4.1
---

## Data Exchange

### json — JSON Parsing and Generation

```no
; Type enum
json-kind {
    null,
    bool,
    num,
    str,
    arr,
    obj,
}

; Parsing
v = json.parse(s, n)          ; Full parse
v = json.parse-str(s, n)                 ; Parse string value
v = json.parse-num(s, n)                 ; Parse numeric value

; Generation
n = json.stringify(v, out)    ; Serialize

; Access
val = json.get-key(v, key)    ; Get object property
json.set-key(v json-value, key, val)    ; Set object property
```

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
