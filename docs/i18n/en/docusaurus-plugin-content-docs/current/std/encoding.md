---
sidebar_position: 3.8
---

## Encoding

### encoding/hex — Hexadecimal

```no
#{fmt-skip}
; Encoding (defined in the byte module)
out = data.to-hex()                  ; []byte -> uppercase hex str
out = data.to-hex-lower()            ; []byte -> lowercase hex str

; Decoding (defined in the str module)
out = s.from-hex()                   ; hex str -> ?[]byte (nil=empty, err=invalid character)
```

### encoding/base64 — Base64 (RFC 4648)

```no
#{fmt-skip}
BASE64-STD = 'ABC...+/'
BASE64-URL = 'ABC...-_'
PAD = 61  ; '='

out-n = base64.encode(data, n, table, out)    ; Base64 encoding
out-n = base64.encode-std(data, n, out)       ; Standard encoding
out-n = base64.encode-url(data, n, out)       ; URL-safe encoding
out-n = base64.decode(s, n, table, out)   ; Base64 decoding (?i64, nil=invalid input)
```

### encoding/csv — CSV Parsing (RFC 4180)

```no
#{fmt-skip}
fn, new-pos = csv.parse-field(s, sn, pos, field)  ; Parse a single field
fields = csv.parse-line(s, nil)                    ; Parse one line (nil -> cap 1024)
out-n = csv.encode-field(field, fn, out)           ; Encode field
```

### encoding/pem — PEM Encoding/Decoding (RFC 7468)

PEM (Privacy-Enhanced Mail) format encoding/decoding, widely used for X.509 certificates, RSA/ECDSA keys, etc.:

```no
#{fmt-skip}
; Constants
PEM-LINE-LEN = 64                    ; Max Base64 characters per line

; Struct
block {
    label str                         ; PEM label (e.g. 'CERTIFICATE', 'RSA PRIVATE KEY')
    data []byte                       ; Raw binary data
}

; Encoding
pem-str = pem.encode(label, data)              ; Encode raw bytes to PEM string

; Decoding
result = pem.decode(pem-str)                    ; Decode PEM string (?block)
;   ok -> result.label, result.data
;   nil -> invalid format
```

---
