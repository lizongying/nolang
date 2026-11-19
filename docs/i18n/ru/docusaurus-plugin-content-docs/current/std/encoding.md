---
sidebar_position: 3.8
---

## Кодирование

### encoding/hex — шестнадцатеричный формат

```no
; Кодирование (определено в модуле byte)
out = data.to-hex(); []byte → hex str в верхнем регистре
out = data.to-hex-lower(); []byte → hex str в нижнем регистре

; Декодирование (определено в модуле str)
out = s.from-hex(); hex str → ?[]byte (nil=пусто, err=недопустимый символ)
```

### encoding/base64 — Base64 (RFC 4648)

```no
BASE64-STD = 'ABC...+/'
BASE64-URL = 'ABC...-_'
PAD = 61; '='

out-n = base64.encode(data, n, table, out); кодирование Base64
out-n = base64.encode-std(data, n, out); стандартное кодирование
out-n = base64.encode-url(data, n, out); кодирование, безопасное для URL
out-n = base64.decode(s, n, table, out); декодирование Base64 (?i64, nil=недопустимые входные данные)
```

### encoding/csv — разбор CSV (RFC 4180)

```no
fn, new-pos = csv.parse-field(s, sn, pos, field); разбор одного поля
fields = csv.parse-line(s, nil); разбор одной строки (nil → предел 1024)
out-n = csv.encode-field(field, fn, out); кодирование поля
```

### encoding/pem — кодирование/декодирование PEM (RFC 7468)

Формат PEM широко используется для сертификатов X.509, ключей RSA/ECDSA и т. д.

```no
; Структура
block {
    label str
    data []byte
}

; Кодирование
out = pem.encode(label, data); кодирование исходных байтов в строку PEM

; Декодирование
result = pem.decode(pem-str); разбор строки PEM (?block, nil=ошибка разбора)
; при успехе доступны result.label и result.data
```

---
