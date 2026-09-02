---
sidebar_position: 3.8
---

## 編碼

### encoding/hex — 十六進制

```no
; 編碼（定義於 byte 模組）
out = data.to-hex()                  ; []byte → 大寫 hex str
out = data.to-hex-lower()            ; []byte → 小寫 hex str

; 解碼（定義於 str 模組）
out = s.from-hex()                   ; hex str → ?[]byte（nil=空, err=無效字元）
```

### encoding/base64 — Base64（RFC 4648）

```no
BASE64-STD = 'ABC...+/'
BASE64-URL = 'ABC...-_'
PAD = 61  ; '='

out-n = base64.encode(data, n, table, out)    ; Base64 編碼
out-n = base64.encode-std(data, n, out)       ; 標準編碼
out-n = base64.encode-url(data, n, out)       ; URL 安全編碼
out-n = base64.decode(s, n, table, out)   ; Base64 解碼（?i64, nil=無效輸入）
```

### encoding/csv — CSV 解析（RFC 4180）

```no
fn, new-pos = csv.parse-field(s, sn, pos, field)  ; 解析單個欄位
n = csv.parse-line(s, sn, fields, max)             ; 解析一行
out-n = csv.encode-field(field, fn, out)           ; 編碼欄位
```

### encoding/pem — PEM 編解碼（RFC 7468）

PEM 格式廣泛用於 X.509 憑證、RSA/ECDSA 金鑰等。

```no
; 結構體
pem-block {
    label str
    data []byte
}

; 編碼
out = pem.pem-encode(label, data)                  ; 將原始位元組編碼為 PEM 字串

; 解碼
result = pem.pem-decode(pem-str)                    ; 解析 PEM 字串（?pem-block，nil=解析失敗）
; 成功時可存取 result.label 和 result.data
```

---
