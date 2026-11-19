---
sidebar_position: 3.8
---

## Codificação

### encoding/hex — Hexadecimal

```no
; Codificação (definida no módulo byte)
out = data.to-hex(); []byte → str hex em maiúsculas
out = data.to-hex-lower(); []byte → str hex em minúsculas

; Descodificação (definida no módulo str)
out = s.from-hex(); str hex → ?[]byte（nil=vazio, err=carácter inválido）
```

### encoding/base64 — Base64 (RFC 4648)

```no
BASE64-STD = 'ABC...+/'
BASE64-URL = 'ABC...-_'
PAD = 61; '='

out-n = base64.encode(data, n, table, out); codificação Base64
out-n = base64.encode-std(data, n, out); codificação padrão
out-n = base64.encode-url(data, n, out); codificação segura para URL
out-n = base64.decode(s, n, table, out); descodificação Base64（?i64, nil=entrada inválida）
```

### encoding/csv — Análise de CSV (RFC 4180)

```no
fn, new-pos = csv.parse-field(s, sn, pos, field); analisa um único campo
fields = csv.parse-line(s, nil); analisa uma linha（nil → limite 1024）
out-n = csv.encode-field(field, fn, out); codifica um campo
```

### encoding/pem — Codificação/descodificação PEM (RFC 7468)

O formato PEM é amplamente usado em certificados X.509, chaves RSA/ECDSA, etc.

```no
; Estrutura
block {
    label str
    data []byte
}

; Codificação
out = pem.encode(label, data); codifica bytes crus numa string PEM

; Descodificação
result = pem.decode(pem-str); analisa uma string PEM（?block，nil=falha na análise）
; Em caso de sucesso, é possível aceder a result.label e result.data
```

---
