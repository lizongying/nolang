---
sidebar_position: 3.8
---

## 编码

### encoding/hex — 十六进制

```no
; 编码（定义于 byte 模组）
out = data.to-hex(); []byte → 大写 hex str
out = data.to-hex-lower(); []byte → 小写 hex str

; 解码（定义于 str 模组）
out = s.from-hex(); hex str → ?[]byte（nil=空, err=无效字元）
```

### encoding/base64 — Base64（RFC 4648）

```no
BASE64-STD = 'ABC...+/'
BASE64-URL = 'ABC...-_'
PAD = 61; '='

out-n = base64.encode(data, n, table, out); Base64 编码
out-n = base64.encode-std(data, n, out); 标准编码
out-n = base64.encode-url(data, n, out); URL 安全编码
out-n = base64.decode(s, n, table, out); Base64 解码（?i64, nil=无效输入）
```

### encoding/csv — CSV 解析（RFC 4180）

```no
fn, new-pos = csv.parse-field(s, sn, pos, field); 解析单个栏位
fields = csv.parse-line(s, nil); 解析一行（nil → 上限 1024）
out-n = csv.encode-field(field, fn, out); 编码栏位
```

### encoding/pem — PEM 编解码（RFC 7468）

PEM 格式广泛用于 X.509 凭证、RSA/ECDSA 金钥等。

```no
; 结构体
block {
    label str
    data []byte
}

; 编码
out = pem.encode(label, data); 将原始位元组编码为 PEM 字串

; 解码
result = pem.decode(pem-str); 解析 PEM 字串（?block，nil=解析失败）
; 成功时可存取 result.label 和 result.data
```

---
