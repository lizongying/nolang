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

Nolang 支援兩種**單行註釋**標記，語義完全相同——都註釋到行尾：

- `//` —— 傳統標記
- `;` —— 另類標記（2026-07-17 實作）

```nolang
// 這是註釋
; 這也是註釋，語義相同
x = 1 ; 行內註釋，註釋到行尾
```

**格式化保留原始標記**：`no fmt` 不會把 `;` 改成 `//`，也不會把 `//` 改成 `;`。formatter 透過 `Comment.Marker` 記住原始標記並原樣輸出，因此 `;` 註釋經過格式化後仍然是 `;`。

**安全範圍**：`;` 出現在字串字面量（例如 `'text/plain; charset=utf-8'`、`index-from(';', pos)`）與 `//` 註釋內部時，由 lexer 的字串掃描器與註釋掃描器先行消費，不會被當成註釋標記。

**注意 `cond -> X; Y`**：`;` 現在是註釋，所以 `cond -> X; Y` 解析為「執行 `cond -> X`（求值 X 後丟棄）」加上行內註釋 `; Y`——`X` **不會被賦值**。正確寫法是 `cond -> X = Y`（stdlib 既有慣例，見 `arr.no`、`uuid.no`、`path.no`、`err.no`、`assert.no`）。若在原始碼看到 `cond -> X; Y`，優先改為 `cond -> X = Y`。
