---
sidebar_position: 3.8
---

## Codificación

### encoding/hex — Hexadecimal

```no
; Codificación (definida en el módulo byte)
out = data.to-hex(); []byte → cadena hex en mayúsculas
out = data.to-hex-lower(); []byte → cadena hex en minúsculas

; Decodificación (definida en el módulo str)
out = s.from-hex(); hex str → ?[]byte (nil=vacío, err=carácter inválido)
```

### encoding/base64 — Base64 (RFC 4648)

```no
BASE64-STD = 'ABC...+/'
BASE64-URL = 'ABC...-_'
PAD = 61; '='

out-n = base64.encode(data, n, table, out); Codificación Base64
out-n = base64.encode-std(data, n, out); Codificación estándar
out-n = base64.encode-url(data, n, out); Codificación segura para URL
out-n = base64.decode(s, n, table, out); Decodificación Base64 (?i64, nil=entrada inválida)
```

### encoding/csv — Análisis de CSV (RFC 4180)

```no
fn, new-pos = csv.parse-field(s, sn, pos, field); Analizar un único campo
fields = csv.parse-line(s, nil); Analizar una línea (nil → límite 1024)
out-n = csv.encode-field(field, fn, out); Codificar campo
```

### encoding/pem — Codificación y decodificación PEM (RFC 7468)

El formato PEM se usa ampliamente para certificados X.509, claves RSA/ECDSA, etc.

```no
; Estructura
block {
    label str
    data []byte
}

; Codificación
out = pem.encode(label, data); Codificar bytes sin procesar como cadena PEM

; Decodificación
result = pem.decode(pem-str); Analizar cadena PEM (?block, nil=error de análisis)
; Si tiene éxito, se puede acceder a result.label y result.data
```

---
