---
sidebar_position: 4
---

# 編碼規範

## 檔案末尾換行（Trailing Newline / EOF）

每個**非空**的 `.no` 原始檔，**必須以恰好一個末尾換行符結尾**（也就是檔案最後要有一個空行）。

- 檔案**沒有**末尾換行 → 補上一個。
- 檔案末尾有**多個**空行 → 合併為單一末尾換行。
- **空檔案**（0 位元組）→ 保持不動。

排除目錄：`dist/`、`vscode-nolang/`、`node_modules/`。

理由：統一的 EOF 單一換行可避免 `git diff` 出現 "no newline at end of file"，讓差異更乾淨，也讓工具串接與拼接行為可預測。

### 自動化執行（工具鏈內建）

此規範由工具鏈**自動執行**，沒有獨立腳本：

- **`no fmt`**：`src/cmd/no/main.go` 的 `fmt` 子命令會原地格式化檔案，並呼叫 `fmt.FormatFile`，保證恰好一個末尾換行。
- **LSP 存檔格式化 / `textDocument/formatting`**：`src/lsp/server.go` 中的 `formatNolangCode` → `fmt.FormatFile`，在編輯器存檔或手動格式化 `.no` 檔案時，自動補上或合併 EOF 換行。

實作位於 `src/fmt/formatter.go`：

- `FormatFile(code)` 格式化整個檔案，並呼叫 `ensureTrailingNewline`：去掉末尾所有 `\r` / `\n`（相容 CRLF 與多個末尾空行），再補上一個 `\n`。空檔案或無法解析的內容會原樣返回，避免破壞檔案。
- `Format(code)` 是純片段格式化函式（不加末尾換行），主要供單元測試使用；寫入真實原始檔時請改用 `FormatFile`。

## 註釋標記（Comment Markers）

Nolang 支援兩種**單行註釋**標記與一種**多行（塊）註釋**標記，語義如下：

- `//` —— 傳統單行標記（註釋到行尾）
- `;` —— 另類單行標記（2026-07-17 實作，註釋到行尾）
- `;; ... ;;` —— 多行（塊）註釋（2026-07-18 實作，對稱定界，未閉合則註釋到文件尾）

```no
// 這是註釋
; 這也是註釋，語義相同
x = 1 ; 行內註釋，註釋到行尾

;; 這是多行（塊）註釋
   可以跨多行
   以 ;; 結尾 ;;
y = 2 ;; 行內塊註釋 ;;
```

**塊註釋定界**：`;;` 開始、`;;` 結束，兩者之間（含換行）都是註釋內容；內容裡的單一 `;` 不會結束塊，只有 `;;` 才會。若找不到結尾 `;;`，則從 `;;` 開始一直註釋到文件結尾。

**格式化保留原始標記**：`no fmt` 不會改變任何註釋的標記——`;` 仍是 `;`、`//` 仍是 `//`、`;; ... ;;` 仍是 `;; ... ;;`。formatter 透過 `Comment.Marker` 記住原始標記並原樣輸出，塊註釋內容（含內部換行）也會逐字保留，因此格式化是冪等的。

**安全範圍**：`;` / `;;` 出現在字串字面量（例如 `'text/plain; charset=utf-8'`、`index-from(';', pos)`）與 `//` 註釋內部時，由 lexer 的字串掃描器與註釋掃描器先行消費，不會被當成註釋標記。

**注意 `cond -> X; Y`**：`;` 現在是註釋，所以 `cond -> X; Y` 解析為「執行 `cond -> X`（求值 X 後丟棄）」加上行內註釋 `; Y`——`X` **不會被賦值**。正確寫法是 `cond -> X = Y`（stdlib 既有慣例，見 `arr.no`、`uuid.no`、`path.no`、`err.no`、`assert.no`）。若在原始碼看到 `cond -> X; Y`，優先改為 `cond -> X = Y`。

## 返回值變數延遲零值（Deferred Zero-Init for Return Values）

Nolang 函式的具名結果參數（out 參數）採用**延遲零值**策略：函式 prologue **不會**對 out 參數做任何零值初始化，編譯器改以一個 bitmap `%__ret_init_bitmap` 追蹤每個 out 參數是否在函式體內被顯式賦值過。當函式返回時，任何**未被顯式賦值**的 out 參數會自動補上對應型別的零值 store（整數 → `0`、str-long → `zeroinitializer`、struct → `zeroinitializer`、option → `nil`）。此機制與既有的 `%__move_bitmap` 延遲釋放機制對稱。

### 為什麼這樣設計

- **效能**：避免 prologue 對每個 out 參數都做一次 `store zeroinitializer`，再被函式體內的賦值覆蓋掉——這是純粹的冗餘 store。延遲策略只在「真的沒賦值」時才補一次零。
- **可讀性**：不需要在函式開頭寫樣板 `found = false` / `result = nil` 等提前賦零值的程式碼，讀者可以直接看到「成功路徑才會賦值」的意圖。
- **一致性**：與 `%__move_bitmap` 的延遲釋放機制採用相同的 bitmap 追蹤模式，編譯器內部實作對稱。

### 推薦寫法

不要在函式開頭對 out 參數做提前賦零值——編譯器會在返回時自動補零。只寫「成功路徑」的賦值即可：

```no
; Good：不寫提前賦零值，編譯器自動處理
hashmap-str-tmpl.contains = (key str) (found bool) {
    val ?v = .get(key)
    val: {
        ok -> found = true
        err -> {}
        nil -> {}
    }
}
```

### 反模式（避免）

不要寫冗餘的提前賦零值——這只是被後續賦值覆蓋掉的無效 store：

```no
; Bad：冗餘的提前賦零值
hashmap-str-tmpl.contains = (key str) (found bool) {
    found = false              ; 冗餘——編譯器會在返回時自動補零
    val ?v = .get(key)
    val: {
        ok -> found = true
        err -> {}
        nil -> {}
    }
}
```

### Option 預設為 nil

對 `?T` option 型別的 out 參數，編譯器在補零時直接寫入 `nil`（tag=1, data=0），等同於 `result = nil`。因此「找不到 / 空集合 / EOF」這類路徑只需裸 `return`，不需要在原始碼寫 `result = nil`：

```no
; Good：未命中路徑用裸 return，編譯器自動補 result = nil
hashmap-str-tmpl.get = (key str) (result ?v) {
    .size == 0 -> return        ; 編譯器補 result = nil
    ...
    ; fall-through：編譯器補 result = nil
}
```

### 除錯提示

若函式返回了非預期的零值（例如 `found` 應為 `true` 卻得到 `false`，或 `result` 應有值卻是 `nil`），檢查函式體內**所有應返回非零值的路徑**是否都對 out 參數做了顯式賦值。編譯器不會猜測意圖——它只會對未被賦值的 out 參數補零。詳見 `.agents/skills/nolang-debug/SKILL.md` 的除錯提示。
