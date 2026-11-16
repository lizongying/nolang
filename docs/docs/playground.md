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

介面會跟隨 Docusaurus 的語系自動切換簡體中文與英文，輸出窗格也同時支援亮色與暗色主題。

### 工具列

| 按鈕      | 功能                                                              |
| --------- | ----------------------------------------------------------------- |
| **▶ Run** | 編譯當前程式碼為 WASM 並透過 WASI 執行；stdout 顯示於右側 Output。 |
| **Format**| 透過 LSP 呼叫 `no fmt` 格式化當前程式碼。                          |
| **Examples** | 依分類載入內建範例，覆蓋編輯器當前內容。                        |

工具列右側顯示目前狀態（空閒／編譯中／運行中／完成／錯誤）；下拉選單下方會顯示該範例的中英雙語說明。上次選擇的範例會保存在瀏覽器本機儲存中。

### 範例

Examples 下拉選單預先收錄了 **24 個**依主題分組的範例，全部透過與瀏覽器完全相同的 `--wasm-direct` 編譯路徑在 CI 中驗證：

| 分類 | 數量 | 內容 |
| ---- | ---- | ---- |
| 基礎 | 7 | Hello World、變數推斷、函數與遞迴、Fibonacci、算術與位運算、格式化輸出、字串 |
| 流程控制 | 6 | 短路守衛、模式匹配、FizzBuzz、四種區間迴圈、條件 while 迴圈、break / continue |
| 資料結構 | 4 | 定長陣列、動態切片、結構體、方法 |
| 標量類型 | 4 | char、bool、f64、byte |
| 標準庫 | 2 | math.max / math.min / sqrt、rand.rand |
| 錯誤處理 | 1 | option 與 nil 的三臂 match |

每個範例都附帶一段穩定的預期輸出字串；CI 會編譯每個範例、用 `wasmtime` 執行，並斷言該字串出現在 stdout 中。因此語言或 Direct-WASM 後端演進後，範例不會悄悄失效。

選擇任一範例後，Playground 會清空當前 Output、stderr 與 Diagnostics，並以該範例內容取代編輯器。

### 診斷

當 LSP 偵測到語法或型別錯誤時，會在右下角的 Diagnostics 區列出。點擊任一診斷項目即可跳轉到對應的行與欄。編譯器 stderr 中的 `file:line:col` 訊息會另外解析成 **Run Errors** 標記，並自動移除尾端的 TraceID 與本地化的嚴重性前綴。

### 錯誤與逾時

過去 WASM 載入失敗只會寫入 `console.warn`，使用者看到的是一個毫無反應的 Run 按鈕。現在的行為是：

- `no.wasm` 或 `lsp.wasm` 下載／編譯失敗時，頁面頂端會顯示可關閉的錯誤橫幅，說明是哪個資源與原因。
- 語言伺服器初始化失敗同樣會顯示橫幅；編輯器仍可繼續使用，只是補全、懸停與格式化不可用。
- 每次執行由主執行緒看門狗計時（預設 8 秒）。`wasi.start()` 在 worker 中是同步呼叫，無限迴圈會讓 worker 內的任何計時器都無法觸發，因此唯一有效的中斷方式是從主執行緒終止 worker。逾時後 worker 會被終止並在下次執行時自動重建，同時立刻重新預熱 WASM。
- 重複點擊 Run 會被並行防護擋下， pending 編輯的防抖計時器在切換範例或格式化時會被取消，避免舊輸入覆蓋新內容。

`no.wasm` 與 `lsp.wasm` 會在頁面空閒時預先下載並以串流方式編譯，首次按 Run 不必再等待完整的 27 MB 下載與編譯。

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

**對 `no` / `lsp` 本身的約束（比標準庫更嚴格）**：wasm 產物連「導入」socket 函式都不行。
瀏覽器端用 `@wasmer/wasi` 實例化，它沒有實作 `sock_accept` / `sock_shutdown`，
所以產物一旦因 `net` / `net/http` 被鏈進來而多出這兩個 `wasi_snapshot_preview1` 導入，
`WebAssembly.Instance` 會直接擲出：

```
Failed to instantiate WASI: RuntimeError: JsValue(LinkError: WebAssembly.Instance():
Import #28 "wasi_snapshot_preview1" "sock_accept": function import requires a callable)
```

後果是整支程式連 `_start` 都跑不到——語言服務會報「初始化失敗」（補全、懸停、格式化全廢），
執行按鈕也一樣。因此凡是只有原生環境才有意義的網路程式碼，一律放進 `!wasip1` 的檔案，
wasip1 側只留報錯樁：`cmd/lsp/pprof_wasip1.go`、`cmd/no/serve_esm_wasip1.go`、
`package/download_net_wasip1.go`、`package/release_net_wasip1.go`。
`bun scripts/wasm_imports_check.ts`（`make wasm-imports-check`）用引擎自己的導入表守這條線。

### `poll_oneoff` 由前端補上

導入表乾淨只代表「函式存在」，不代表「函式不炸」。`@wasmer/wasi` 的 `poll_oneoff` 本體是
Rust 的 `unimplemented!()`：只要被呼叫（連 0 個訂閱都會），就執行 `unreachable` 讓整支實例
trap，錯誤長這樣：

```
Error while running start function: RuntimeError: JsValue(RuntimeError: unreachable)
```

Go 的 `runtime` 會把 `time.Sleep` 與 `netpoll(delay)` 都變成一個 monotonic clock 訂閱，
所以 **每一個 Go 程式都會踩到**——`no fmt` / `no vet` / `no build` 全都會在第一次睡著時死掉，
而導入表檢查完全看不出來。

修法在 `docs/src/playground/wasm-loader.ts`：`instantiateWasi()` 自己用 `wasi.getImports()`
組導入物件，把 `poll_oneoff` 換成 JS 實作（時鐘訂閱 → 睡到最近到期時間後回報 clock 事件；
只有 fd 訂閱 → 立即回報就緒，避免永久阻塞）。注意**不能**把覆寫丟給
`wasi.instantiate(module, overrides)`：glue 自己的函式優先，覆寫會被無聲忽略，程式照樣 trap。
也因為改成自建導入物件，`wasi.start(instance)` 必須顯式傳入實例。

### 首次執行的編譯時間

Run 會用 `no build --wasm-direct` 把整個 std 重新下降成 wasm，在單執行緒的 wasm 直譯環境裡
約需 **一分鐘**（原生 `no` 約 11 秒）。這是既有效能特性，不是逾時判定：
runner 的看門狗因此分成兩段預算——載入 + 編譯 180 秒，使用者程式仍只給 5 秒。

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

## 開發與驗證

修改 Playground 後，可在乾淨的建置樹上執行下列檢查：

```bash
make playground-demos                       # 編譯 + wasmtime 執行全部 24 個範例
make wasm-imports-check                     # 重建兩個 wasm 並驗證導入表無 socket 函式
make wasm-runtime-smoke                     # 用 worker 的同一條載入路徑實跑 version/fmt/vet
RUN_WASM_LSP=1 RUN_WASM_BUILD=1 make wasm-runtime-smoke   # 追加語言服務與完整編譯+執行
bun scripts/playground_ui_check.ts          # 診斷解析、看門狗、worker 恢復、i18n
make playground-smoke                       # dev server 與 WASM 路由 HTTP 200
cd docs && bun run typecheck                # TypeScript 型別檢查
```

注意 `playground-demos` 走的是**原生** `no`，它攔不到「wasm 產物帶了瀏覽器不支援的導入」這類問題；
`wasm-imports-check` 補的就是這一格（以 `WebAssembly.Module.imports()` 判定，與實例化時的檢查同源）。
worker 端另有一層兜底：`wasm-loader.ts` 的 `instantiateWasi()` 會把 `@wasmer/wasi` 沒提供的
WASI 函式補成回傳 `ENOSYS` / `NOTSUP` 的樁，讓意外的導入退化成一次失敗的系統呼叫，
而不是整片服務掛掉。

`wasm-imports-check` 也攔不到「函式存在但本體會 trap」（上面 `poll_oneoff` 那一類），
所以 `make wasm-runtime-smoke` 補上最後一格：它走 `instantiateWasi()` 的真實路徑實跑
`no version` / `no fmt` / `no vet`，並可加驗語言服務與完整編譯 + 執行。

`docs/static/wasm/*.wasm` 被 `.gitignore` 排除，永遠不應提交本機產物。`no.wasm` 與 `lsp.wasm` 由 CI 從同一份原始碼與同一個 `StdSigKey` 一起重建，確保瀏覽器裡的編譯器、內嵌 std AST blob 與 Direct-WASM 後端來自同一次建置；三者版本錯位正是過去 `unreachable` 與驗證錯誤的常見來源。

CI（`.github/workflows/build.yml` 的 `pr-verify`、`static.yml` 的站台建置）會依序執行 Go 測試、型別檢查、UI 檢查與範例驗證，然後才從乾淨樹重建 WASM 並部署站台。
