---
sidebar_position: 4.1
---

## 資料交換

### json — JSON 解析與產生

```no
; 型別枚舉
json-kind {
    null,
    bool,
    num,
    str,
    arr,
    obj,
}

; 解析
v = json.parse(s, n)          ; 完整解析
v = json.parse-str(s, n)                 ; 解析字串值
v = json.parse-num(s, n)                 ; 解析數值值

; 產生
n = json.stringify(v, out)    ; 序列化

; 存取
val = json.get-key(v, key)    ; 取得物件屬性
json.set-key(v json-value, key, val)    ; 設定物件屬性
```

### toml — TOML 1.0 解析與產生

`toml` 以 `toml-doc` 保存 TOML 文件，支援註解、bare/quoted/dotted keys、普通表格、array-of-tables、basic/literal/multiline strings、各種整數進位、浮點數、布林值、日期時間、陣列與 inline table。未提供專用 Nolang 型別的值會保留原始字面值。

```no
cfg = toml.parse('title = "Nolang"\n[server]\nport = 8080\n[[server.backends]]\nname = "local"')

doc = toml.new()
doc.put('name', '"nolang"')
doc.put('server.port', '8080')
doc.put('enabled', 'true')
raw = toml.get(doc, 'server.port')
text = toml.stringify(doc)
```

常用函式：

- `toml.new()` — 建立空文件
- `toml.parse(text)` — 解析並回傳 `?toml-doc`
- `doc.put(key, raw-value)` — 驗證並新增/更新 TOML 值
- `toml.get(doc, key)` — 取得原始值
- `toml.get-str`、`toml.get-i64`、`toml.get-f64`、`toml.get-bool` — 取得 scalar 值
- `toml.stringify(doc)` — 序列化文件

文件上限為 4 個 key、2 個普通表格、2 個 array-of-tables；key 最長 32 bytes，value 最長 128 bytes。

---
