# json, toml, yaml, dom

> 视图切分自原 `nolang-std/SKILL.md`，正文未改写；索引与加载判据见 [`../m20-std.md`](../m20-std.md)。

### Data Exchange

#### json — JSON Parsing and Generation

`json` stores a JSON tree in a **heap-allocated node pool** (`json-pool`), supporting nested arrays/objects, string/number/bool/null scalars, escape sequences and exponent numbers. The pool is built from dynamic `[]vec` buffers, so there is **no fixed capacity cap** — total node count and children-per-node are bounded only by available memory. The high-level `json` struct wraps the pool plus a root index; its `pool` field is annotated `#{inline=false}` (a pointer field), which lets `json` be safely returned and copied **by value**.

```no
; Parse: returns ?json (ok = value, err = failure with a specific reason)
j = json.parse('{"name":"Alice","age":30,"items":[1,2,3],"active":true}')
j: {
    ok -> {
        name = it.get-str('name'); scalars: get-str / get-num / get-bool / get-i64 (?value)
        age = it.get-i64('age'); ?i64
        items = it.get('items'); child node, returns ?json
        s = it.stringify(); serialize back to string
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

```no
j = json.new()                              ; empty (null) json
j = json.parse(s)                           ; ?json; err reasons: empty input / parse error / trailing characters
s = j.stringify()                           ; serialize

; read scalars by key (returns ?val)
val = j.get-str(key)                    ; ?str
val = j.get-num(key)                    ; ?f64
val = j.get-bool(key)                   ; ?bool
val = j.get-i64(key)                    ; ?i64, truncated integer
child = j.get(key)                          ; child node, ?json

; direct root scalar access (when root is a scalar)
val = j.str()
val = j.num()
val = j.bool()
val = j.i64()                             ; each ?val
val = j.str-val()

; write
ok = j.set-str(key, val)
ok = j.set-num(key, val)
ok = j.set-i64(key, val)
ok = j.set-bool(key, val)
ok = j.set-null(key)
ok = j.set-key(key, val json)               ; attach another json node (deep-copied)

; array
child = j.arr-get(i)
n = j.arr-len()
ok = j.arr-push(val json)
ok = j.arr-push-str(val)
ok = j.arr-push-num(val)
ok = j.arr-push-bool(val)

; object enumeration
n = j.obj-len()
key = j.obj-key(i)
keys = j.obj-keys-str()
ok = j.delete-key(key)

; type checks
k = j.kind()                                ; JSON-KIND-* constant
yes = j.is-null()
yes = j.is-obj()
yes = j.is-arr()
yes = j.is-str()
yes = j.is-num()
yes = j.is-bool()
```

**Pool capacity (no fixed cap).** The pool uses heap `[]vec`, so it grows automatically:

- Total nodes and children-per-node have **no hard limit** (only memory-bound). The old `JSON-MAX-NODES = 128` / `JSON-MAX-CHILDREN = 32` caps are gone.
- `json.parse` returns `err` only on a genuine syntax error — there is no longer a "pool exhausted" error branch.
- `j.set*` / `j.arr-push*` return `false` only on a kind mismatch (e.g. `set` on a non-object, `arr-push` on a non-array). `j.overflowed()` is kept for backward compatibility and now always returns `false`.

Low-level API (`json-pool` / `json-value`) — operate by node index; the pool is a single-level struct so it is safe to return/copy by value:

```no
p = json.new-pool()
p.init(); init() sets nodes/strs/ec/ek/en to with-len(0)
idx = p.alloc(); allocate a node (heap vec auto-grows)
p.add-child(node-idx, child-idx, key); append a child via the ec/ek/en edge linked-list
node-idx, next-pos, ok = p.parse(s, pos); parse one value from pos, returns node index
val-idx = p.get-key(obj-idx, key); ?i64
val-idx = p.arr-get(arr-idx, i); ?i64
n = p.arr-len(arr-idx)
kind = p.get-kind(idx)
s = p.get-str(idx); ?str
n = p.get-num(idx); ?f64
b = p.get-bool(idx); ?bool
ok = p.set-key(obj-idx, key, val-idx)
dst = p.copy-tree(src, idx); copy-tree recurses
s = p.stringify(node-idx); serialize a specific node
```

> **`json` copy semantics — `#{inline=false}`.** `pool` is a pointer field, so a child handle returned by `j.get` / `j.arr-get` **deep-copies** the whole pool: the handle is an independent snapshot of the parent, and writes through it do **not** propagate back to the parent node. Also, matching a `?json` option **consumes** its value — calling `match` on the *same* option a second time falls into the `nil` branch. To reuse one parsed result several times, either finish all work inside a single `match` (use `it`), or bind it once into a plain `json` local. This is why the pool was migrated to heap `vec` + `#{inline=false}`: the earlier fixed inline array existed only because a two-level `json { pool json-pool }` **by-value** return silently corrupted nested heap vecs; making `pool` a pointer field removes that restriction.


#### toml — TOML Parsing and Generation

The `toml` module supports the TOML 1.0 value grammar: comments, bare/quoted/dotted keys, tables, array-of-tables, basic/literal/multiline strings, decimal/hex/octal/binary integers, floats, booleans, date-time literals, arrays, and inline tables. Values are stored as bounded raw TOML literals, preserving syntax that does not have a dedicated Nolang runtime type.

```no
cfg = toml.parse('title = "Nolang"\n[server]\nport = 8080\n[[server.backends]]\nname = "local"')

doc = toml.new()
doc.put('name', '"nolang"')
doc.put('server.port', '8080')
doc.put('enabled', 'true')
text = toml.stringify(doc)
```

API:

```no
doc = toml.new(); Create empty toml-doc
cfg = toml.parse(text); ?toml-doc
doc.put(key, raw-value); Insert/update a validated TOML value
raw = toml.get(doc, key); ?str raw literal
s = toml.get-str(doc, key); ?str Basic/literal string
n = toml.get-i64(doc, key); ?i64 Decimal/hex/octal/binary integer
f = toml.get-f64(doc, key); ?f64 Float
b = toml.get-bool(doc, key); ?bool Boolean
text = toml.stringify(doc); Serialize TOML
```

The current bounded document model supports up to 4 keys, 2 ordinary tables, and 2 array-of-tables. Keys are limited to 32 bytes and values to 128 bytes.

#### yaml — YAML 1.2 Parsing and Generation

`yaml` keeps a YAML 1.2 core-schema document tree in a node pool (`yaml-pool`). Supported: block mappings, block sequences, flow collections (`[...]` / `{...}`), single-quoted / double-quoted / plain scalars, block scalars (`|` / `>` with `-` / `+` chomping), comments, document markers (`---` / `...`) and multi-document streams, anchors (`&`) / aliases (`*`), merge keys `<<`, explicit keys (`? k`), core-schema type inference (null / bool / int / float / `.inf` / `.nan`), dotted-path lookup, and JSON / YAML serialization.

```no
doc ?yaml = yaml.parse('name: Alice\nserver:\n  port: 8080\ntags:\n  - a\n  - b\n')
doc: {
    nil -> print('nil')

    err -> print(it.msg())

    -> {
        doc = it
        name = doc.find-str('name'); ?str
        port = doc.find-i64('server.port'); ?i64
        name: {
            ok -> print(it)

            -> print('')
        }
        port: {
            ok -> print(it.to-str())

            -> print('')
        }
        print(doc.to-json())
        print(doc.to-yaml())
    }
}
```

API:

```no
; Entry points
d = yaml.parse(text); ?yaml — first document
all = yaml.parse-all(text); ?yaml — every document
yes = yaml.valid-of(text); Validate without building a document
msg = yaml.error-of(text); '' when valid, else "line L, column C: ..."
s = yaml.strip-bom(text); Strip a UTF-8 BOM

; Multi-document
n = all.doc-count()
d = all.doc(i); ?yaml — i-th document view
j = all.to-json-all()

; Node information (methods on a yaml value)
k = d.kind(); YAML-KIND-* constant
d.is-null()
d.is-bool()
d.is-int()
d.is-float()
d.is-nan()
d.is-num()
d.is-str()
d.is-seq()
d.is-map()
n = d.len(); Entries (map) or items (seq)
t = d.text(); Raw scalar text
s = d.tag(); Explicit tag, '' when absent
a = d.anchor(); Anchor name, '' when absent
ln = d.line-of(); 1-based source line
st = d.style(); YAML-STYLE-* constant

; Value access — all return ?value
s = d.str(); ?str
n = d.i64(); ?i64
f = d.f64(); ?f64
b = d.bool(); ?bool
s = d.scalar-text(); ?str, none for seq/map

; Children — all return ?yaml (unwrap with a match)
child = d.at(i); ?yaml — seq item / i-th map value
child = d.value(i); ?yaml — alias of at(i)
child = d.key-node(i); ?yaml — i-th map key node
s = d.key(i); ?str — i-th map key text
child = d.get(key); ?yaml — map value by scalar key
node = d.find(path); ?yaml — dotted path; seq indices are numeric ('a.0.b')

; Typed shortcuts — all return ?value
s = d.get-str(key); ?str
n = d.get-i64(key); ?i64
f = d.get-f64(key); ?f64
b = d.get-bool(key); ?bool
s = d.find-str(path); ?str
n = d.find-i64(path); ?i64
f = d.find-f64(path); ?f64
b = d.find-bool(path); ?bool

; Serialization
j = d.to-json()
y = d.to-yaml(); Same as stringify()
y = d.stringify()
src = d.source-of()

; Constants
YAML-KIND-NULL = 0
BOOL = 1
INT = 2
FLOAT = 3
STR = 4
SEQ = 5
MAP = 6
YAML-STYLE-PLAIN = 0
SINGLE = 1
DOUBLE = 2
LITERAL = 3
FOLDED = 4
ALIAS = 5
YAML-STYLE-BLOCK = 6
FLOW = 7
```

> **Gotcha 1 — child views return `?yaml`; unwrap them with a match.** Write `child = d.at(i)` then match it — an `ok` arm that binds `it` to the child node, and a default arm for the none case (one arm per line, no `;` separators). The value helpers (`str`/`i64`/`f64`/`bool`/`scalar-text`) likewise return `?value`. Child views share the parent document's node pool, so finish work with a node inside the same `match` (or bind it once) rather than holding a stale handle across further pool mutations.
>
> **Gotcha 2 — `.nan` has no f64 representation.** The backend emits fast-math FP, so a real NaN cannot be produced (`0.0 / 0.0` folds to `0`). Detect it with `d.is-nan()` (based on the node's source text); `d.f64()` returns `0.0` for it, `to-json()` emits `null`, and `to-yaml()` emits `.nan`.

#### dom — XML / HTML DOM Parsing and Manipulation

`dom` (`# std/dom`) keeps a full DOM tree in a node pool (`dom-pool`, heap vecs, no fixed capacity cap). Nodes are addressed by **i64 indices** (0 = document root) valid for the lifetime of the `dom` value; the `pool` field is `#{inline=false}`, so `dom` copies safely by value.

- **XML (strict)**: `dom.parse-xml` — elements/attributes/text/comments/CDATA/PI/DOCTYPE, predefined + numeric character entities, tag matching, single-root check; malformed input returns `err` with the failing byte offset. Max nesting depth `DOM-MAX-DEPTH = 256`.
- **HTML (lenient)**: `dom.parse-html` — never fails; case-insensitive tag/attribute names, void elements (`br/hr/img/input/...`), raw-text (`script`/`style`) and RCDATA (`title`/`textarea`) elements, implicit close rules (`p`/`li`/`dd`/`dt`/`option`/`optgroup`/`td`/`th`/`tr`/table sections), unmatched close tags auto-closed or ignored, HTML4 named entity table, unquoted attribute values.

```no
# std/dom

d = dom.parse-xml('<root a="1">text<!--c--></root>')
d: {
    ok -> {
        doc = it
        root ?i64 = doc.document-element()
        print(doc.name(root)); 'root'
        print(doc.get-attribute(root, 'a')); option<str>
        print(doc.to-string()); serialize whole document
    }

    err(e) -> print(e)
}

h = dom.parse-html('<P ID="x">hi<br><li>a<li>b</P>')
h: {
    ok -> {
        doc = it
        ps = doc.get-elements-by-tag-name('p'); case-insensitive lookup
        print(ps.len()); 1
    }

    ->
}
```

API summary:

```no
; Parsing / construction
d = dom.parse-xml(source)          ; ?dom — err('... at byte N') on invalid XML
d = dom.parse-html(source)         ; ?dom — always ok
d = dom.new-document(html bool)    ; empty document (html=true selects HTML serialization)

; Node access (idx i64 handles)
k = d.kind(idx)                    ; DOM-KIND-* constant, -1 = invalid index
nm = d.name(idx)                   ; element tag / PI target / doctype name
v = d.value(idx)                             ; text/CDATA/comment data, PI data
d.set-node-value(idx, v)
doc-idx = d.root                             ; document root node (index 0)
p = d.parent(idx)
c = d.first-child(idx)
c = d.last-child(idx)
c = d.next-sibling(idx)                      ; -1 when absent
c = d.prev-sibling(idx)                      ; -1 when absent
n = d.child-count(idx)
c ?i64 = d.child-at(idx, i)
root ?i64 = d.document-element()

; Attributes (HTML mode: names case-insensitive / lowercased)
val ?str = d.get-attribute(idx, name)
yes = d.has-attribute(idx, name)
d.set-attribute(idx, name, val)
yes = d.remove-attribute(idx, name)
n = d.attribute-count(idx)
nm ?str = d.attribute-name(idx, i)
val ?str = d.attribute-value(idx, i)

; Query
idx ?i64 = d.get-element-by-id(id)
list []i64 = d.get-elements-by-tag-name(name)       ; '*' = all elements
list []i64 = d.get-elements-by-class-name(cls)
text = d.text-content(idx)

; Mutation
idx = d.create-element(name)
idx = d.create-text-node(data)
idx = d.create-comment(data)
d.append-child(parent, child)                ; moves an attached node
d.insert-child-before(parent, child, ref)
d.remove-child(parent, child)
d.replace-child(parent, new, old)
d.set-text-content(idx, text)                        ; clears children, sets one text node

; Serialization
yes = d.is-html()
s = d.to-string()                                    ; whole document
s = d.outer-html(idx)
s = d.inner-html(idx)                        ; HTML mode: void elements as <br>, empty as <x></x>

; Constants
DOM-KIND-DOCUMENT = 0  ELEMENT = 1  TEXT = 2  CDATA = 3  COMMENT = 4  PI = 5  DOCTYPE = 6
DOM-MAX-DEPTH = 256
```

> **Gotcha — keep node indices inside one `dom`.** Indices reference that document's pool only; passing an index to a different `dom` is invalid (`kind()` returns -1 and accessors no-op). Query results are plain `[]i64`; iterate with `for`/index and re-enter the same `dom`.

---
