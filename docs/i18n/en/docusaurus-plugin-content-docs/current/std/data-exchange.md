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

---
