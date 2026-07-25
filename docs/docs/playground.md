---
sidebar_position: 5
---

# Playground

Nolang Playground 是一個在瀏覽器中直接編輯、編譯並執行 Nolang 程式的線上環境。它將 Nolang 編譯器（`no.wasm`）與語言伺服器（`lsp.wasm`）編譯為 WebAssembly，並透過 WASI 在瀏覽器沙箱中執行使用者程式碼，無需本地安裝任何工具鏈。

立即前往：[**/playground**](/playground)

## 用法

Playground 頁面分為左右兩個窗格：

- **左側 Editor**：基於 [CodeMirror 6](https://codemirror.net/) 的程式碼編輯器，支援 Nolang 語法高亮、自動縮排與歷史紀錄。
- **右側 Output / stderr / Diagnostics**：執行結果、標準錯誤輸出與診斷訊息。

### 工具列

| 按鈕      | 功能                                                              |
| --------- | ----------------------------------------------------------------- |
| **▶ Run** | 編譯當前程式碼為 WASM 並透過 WASI 執行；stdout 顯示於右側 Output。 |
| **Format**| 透過 LSP 呼叫 `no fmt` 格式化當前程式碼。                          |
| **Examples** | 從內建範例清單中載入程式碼，覆蓋編輯器當前內容。                |

### 範例

Examples 下拉選單預先收錄了 6 個常見範例：

- **Hello World** — 最基本的 `print` 與字串拼接
- **Fibonacci** — 迴圈版與遞迴版 Fibonacci
- **Variables & Functions** — 變數宣告、型別、函數定義與多重返回值
- **Structs & Methods** — 結構體定義、實例化、方法
- **Match Expression** — `x: { ... }` 模式匹配
- **math/rand** — 使用 `rand` 標準庫生成隨機數

選擇任一範例後，Playground 會清空當前 Output、stderr 與 Diagnostics，並以該範例內容取代編輯器。

### 診斷

當 LSP 偵測到語法或型別錯誤時，會在右下角的 Diagnostics 區列出。點擊任一診斷項目即可跳轉到對應的行與欄。

## 限制

由於 Playground 在瀏覽器 WASM 沙箱中執行，部分 Nolang 功能無法使用：

### 不支援 FFI

瀏覽器無法連結原生 C 函式庫，因此 `#{c}`、`#{cpp}`、`#{rust}` 等 FFI 宣告在 Playground 中無法使用。包含 FFI 的程式碼（如 `sqlite`、`mysql` driver）無法執行。

### 不支援 fork / exec / pipe

WASI preview1 不提供行程管理 API，因此 `process` 標準庫（`process.start`、`process.wait` 等）在 Playground 中不可用。

### 網路 socket 不可用

WASI preview1 不提供 socket API，因此以下標準庫在 Playground 中無法使用：

- `net`（TCP listener / conn）
- `http` / `http2` / `http3`（HTTP client）
- `ws`（WebSocket）
- `tls`（TLS 連線）
- `quic`、`sse`、`dns`

### 檔案系統為虛擬

WASI preview1 僅提供 stdin / stdout / stderr 三個檔案描述符，沒有真實檔案系統。因此 `fs`、`path`、`os.get-env`、`os.set-env` 等涉及檔案或環境的標準庫在 Playground 中可能無法如預期運作。

### 記憶體上限

瀏覽器 WASM 通常受 32-bit 定址空間限制，可用記憶體上限約為 **2 GB ~ 4 GB**（視瀏覽器與作業系統而定）。需要大量記憶體的程式（如深度遞迴、大型陣列）可能因 out-of-memory 而失敗。

## 瀏覽器相容性

Playground 需要支援 WebAssembly 的現代瀏覽器。建議使用以下版本或更新：

| 瀏覽器    | 最低版本 |
| --------- | -------- |
| Chrome    | 100+     |
| Firefox   | 100+     |
| Safari    | 16+      |
| Edge      | 100+     |

若瀏覽器不支援 WebAssembly 或未啟用 JavaScript，Playground 將無法載入編輯器或執行程式碼。
