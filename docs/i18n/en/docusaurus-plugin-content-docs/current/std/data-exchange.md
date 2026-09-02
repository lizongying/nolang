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

---
