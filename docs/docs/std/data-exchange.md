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

### yaml — YAML 1.2 解析與產生

`yaml` 以節點池（`yaml-pool`）保存 YAML 1.2 core schema 的文件樹，支援塊映射、塊序列、流式集合（`[...]` / `{...}`）、單引號/雙引號/plain 標量、塊標量（`|` / `>` 與 `-` / `+` chomping）、註解、文件標記（`---` / `...`）與多文檔、錨點（`&`）與別名（`*`）、合併鍵 `<<`、顯式鍵（`? k`）、型別推斷（null / bool / int / float / `.inf` / `.nan`）、點號路徑查詢，以及 JSON / YAML 序列化。

```no
doc ?yaml = yaml.parse('name: Alice\nserver:\n  port: 8080\ntags:\n  - a\n  - b\n')
doc: {
    nil -> print('nil')

    err -> print(it)

    -> {
        name, ok = it.find-str('name')
        port, ok2 = it.find-i64('server.port')
        print(name)
        print(port.to-str())
        print(it.to-json())
        print(it.to-yaml())
    }
}
```

常用函式與方法：

- `yaml.parse(text)` / `yaml.parse-all(text)` — 解析，回傳 `?yaml`（`err` 分支帶 `line L, column C` 位置）
- `yaml.valid-of(text)` / `yaml.error-of(text)` — 只做校驗，不構造文件
- `yaml.strip-bom(text)` — 去掉 UTF-8 BOM
- `doc-count()` / `doc(i)` — 多文檔訪問
- `kind()`、`is-null()`、`is-bool()`、`is-int()`、`is-float()`、`is-nan()`、`is-str()`、`is-seq()`、`is-map()`、`len()`、`text()`、`tag()`、`anchor()`、`line-of()` — 節點資訊
- `str()`、`i64()`、`f64()`、`bool()`、`scalar-text()` — 取值，一律回傳 `(值, ok)`
- `at(i)`、`value(i)`、`key(i)`、`key-node(i)`、`get(key)`、`find(path)` — 取子節點 / 鍵
- `get-str` / `get-i64` / `get-f64` / `get-bool`、`find-str` / `find-i64` / `find-f64` / `find-bool` — 便捷取值
- `to-json()`、`to-json-all()`、`to-yaml()`、`stringify()`、`source-of()` — 序列化

節點類型常量：`YAML-KIND-NULL` / `BOOL` / `INT` / `FLOAT` / `STR` / `SEQ` / `MAP`（0–6）；標量風格常量 `YAML-STYLE-PLAIN` / `SINGLE` / `DOUBLE` / `LITERAL` / `FOLDED` / `ALIAS`（0–5）與 `YAML-STYLE-BLOCK` / `FLOW`（6–7）。

> **注意一**：子節點視圖以 `(out yaml, ok bool)` 回傳，而不是 `?yaml`。MIR 後端目前無法安全地共享節點池——把含切片的結構體裝進 option 交還呼叫方後，呼叫方釋放該 option 會連帶釋放父文檔的節點緩衝區（表現為父文檔再次使用時 `len()` 變成 0）。
>
> **注意二**：後端以 fast-math 生成浮點運算，運行期無法產生真正的 NaN（`0.0 / 0.0` 會被折成 `0`），因此 `.nan` 由 `is-nan()` 依節點原文判定，`f64()` 對它回傳 `0.0`；`to-json()` 輸出 `null`，`to-yaml()` 輸出 `.nan`。

---
