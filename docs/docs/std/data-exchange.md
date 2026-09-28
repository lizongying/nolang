---
sidebar_position: 4.1
---

## 資料交換

### json — JSON 解析與產生

`json` 以**堆配置的節點池**（`json-pool`）保存 JSON 樹，支援巢狀陣列與物件、字串/數值/布林/null 標量、轉義字元與指數格式數字。節點池改用動態 `vec`，**沒有固定容量上限**——節點總數與單一節點的子元素數僅受可用記憶體限制。高級介面以 `json` 結構體封裝節點池與根節點索引；其 `pool` 欄位標註 `#{inline=false}`（指標欄位），使 `json` 可按值安全回傳與複製。

```no
; 解析：回傳 ?json（ok=成功，err=失敗並附原因）
j = json.parse('{"name":"Alice","age":30,"items":[1,2,3],"active":true}')
j: {
    ok -> {
        name, found = it.get-str('name')   ; 取標量：get-str / get-num / get-bool / get-i64
        age, ok2 = it.get-i64('age')       ; 整數取值
        items = it.get('items')            ; 取子節點，回傳 ?json
        s = it.stringify()                 ; 序列化回字串
    }
    err(e) -> print(e)
}

; 產生：從空 json 開始，逐步 set / arr-push
j2 = json.new()
j2.set-str('key', 'value')
j2.set-i64('count', 3)
j2.arr-push-str('a')
print(j2.stringify())
```

**高級 API（`json` 結構體）**

- `json.new()` — 建立空（null）`json`
- `json.parse(s)` — 解析，回傳 `?json`；失敗時 `err` 分支帶具體原因（`empty input` / `parse error` / `trailing characters`）
- `j.stringify()` — 序列化為字串
- 取值：`j.get-str(key)` / `j.get-num(key)` / `j.get-bool(key)` / `j.get-i64(key)`（回傳 `(val, ok)`）；`j.get(key)` 回傳子節點 `?json`
- 標量直取（root 為標量時）：`j.str()` / `j.num()` / `j.bool()` / `j.i64()` / `j.str-val()`
- 寫入：`j.set-str(key,val)` / `j.set-num` / `j.set-i64` / `j.set-bool` / `j.set-null` / `j.set-key(key, val json)`
- 陣列：`j.arr-get(i)` / `j.arr-len()` / `j.arr-push(val json)` / `j.arr-push-str` / `j.arr-push-num` / `j.arr-push-bool`
- 物件列舉：`j.obj-len()` / `j.obj-key(i)` / `j.obj-keys-str()` / `j.delete-key(key)`
- 型別判定：`j.kind()` 及 `j.is-null()` / `j.is-obj()` / `j.is-arr()` / `j.is-str()` / `j.is-num()` / `j.is-bool()`

**池容量（無固定上限）**

早期版本以固定陣列作池，硬限制 `JSON-MAX-NODES = 128` / `JSON-MAX-CHILDREN = 32`，超出即靜默失敗。現已全面改為堆 `vec`：

- 節點總數、每節點子元素數**皆無上限**，僅受可用記憶體限制。
- `json.parse` 只在真正的語法錯誤時回傳 `err`，不再有「池耗盡」錯誤分支。
- `j.set*` / `j.arr-push*` 僅在建構目標型別不符時回傳 `false`（例如對非物件節點 set、對非陣列節點 push）。`j.overflowed()` 為向後相容而保留，現恆回傳 `false`。

> **`json` 的複製語意（`#{inline=false}`）**：`pool` 是指標欄位，`j.get` / `j.arr-get` 回傳的子節點 `json` 會**深拷貝**整個池，是父池的獨立快照——對快照的寫入不會回寫父節點。另因 `?json`（option）在 `match` 時會消費其值，對同一個 option **重複 `match`** 會進入 `nil` 分支；若需對同一解析結果多次操作，請在單一 `match` 內完成，或先以一個 `json` 區域變數承接 `it`。

**低階 API（`json-pool` / `json-value`）**

直接以節點索引操作，池作為區域變數使用（單層結構體按值回傳/複製安全）：

- `p = json.new-pool()`；或 `p = json-pool {}` 後 `p.init()`（把 `nodes`/`strs`/`ec`/`ek`/`en` 各 vec 初始化為 `with-len(0)`）
- `idx = p.alloc()` — 分配新節點並回傳索引（堆 vec 自動擴容）
- `p.add-child(node-idx, child-idx, key)` — 以邊表（`ec`/`ek`/`en` 單向鏈表）附加子元素，無每節點上限
- `node-idx, next-pos, ok = p.parse(s, pos)` — 從 `pos` 解析一個值，回傳節點索引
- `val-idx, ok = p.get-key(obj-idx, key)`；`p.arr-get(arr-idx, i)`；`p.arr-len(arr-idx)`
- `p.get-kind(idx)` / `p.get-str(idx)` / `p.get-num(idx)` / `p.get-bool(idx)`
- `p.set-key(obj-idx, key, val-idx)`；`p.copy-tree(src, idx)`（遞迴深拷貝子樹）
- `p.stringify(node-idx)` — 序列化指定節點

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

> **注意一**：子節點視圖以 `(out yaml, ok bool)` 回傳，而不是 `?yaml`。這是 API 風格的選擇，不是所有權限制——子節點與父文檔共享節點池本來就是安全的。取值一律建議用 `find-str` / `find-i64` 這類路徑便捷函式。
>
> **注意二**：巢狀 match 裡 `it` 是**按層還原**的。內層 match 的臂會把 `it` 重新綁到自己的主體（臂體必須看到內層值），但該臂一結束 `it` 就還原成外層臂的主體，所以下面這種寫法是安全的：
>
> ```no
> d ?yaml = yaml.parse(src)
> d: {
>     nil -> print('nil')
>
>     err -> print(it)
>
>     -> {
>         print(it.find-str('a'))        ; 外層 it = 文件
>         v ?yaml = it.at(0)
>         v: {
>             nil -> print('none')
>             err -> print('err')
>             -> print(it.find-str('b')) ; 內層 it = 子節點
>         }
>         print(it.find-str('c'))        ; ✅ it 已還原成文件
>     }
> }
> ```
>
> 唯一的例外是最外層的 match：它結束後 `it` 仍保留最後一個臂的值（不會還原），所以不要在 match 之外依賴 `it`。需要跨層使用時，仍可顯式存到具名區域變數（`doc = it`），或用析構綁定 `ok(v) -> ...`。
>
> **注意三**：`.nan` 會被解析成真正的 NaN（`f64()` 回傳 NaN，`x != x` 為真）。JSON 沒有 NaN，所以 `to-json()` 對它輸出 `null`；`to-yaml()` 輸出 `.nan`。

---
