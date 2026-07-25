# Contributing to Nolang

Nolang is an open source project.

We appreciate your help!

## Filing issues

Sensitive security-related issues should be reported to [lizongying@msn.com](mailto:lizongying@msn.com).

Otherwise, when filing an issue, make sure to answer these five questions:

1. What version of Nolang are you using (`nolang version`)?
2. What operating system and processor architecture are you using?
3. What did you do?
4. What did you expect to see?
5. What did you see instead?

## Playground 開發

Nolang Playground 是一個基於 Docusaurus 的在線 IDE，可在瀏覽器中編輯、編譯並執行 Nolang 代碼。Playground 使用 WebAssembly 版本的 `no` 編譯器與 LSP 服務器。

### 目錄結構

| 路徑 | 說明 |
| --- | --- |
| `docs/src/pages/playground/` | Playground 主頁面（React 組件） |
| `docs/src/playground/` | Playground 共用模組（`nolang-cm.ts` CodeMirror 集成、`lsp-bridge.ts` LSP 橋接、`examples.ts` 範例集） |
| `docs/static/wasm/` | 編譯產出的 `no.wasm`、`lsp.wasm`（由 Makefile 構建後落入此目錄） |
| `src/build/wasm/` | Direct WASM 後端（`no build -target wasm32-wasi` 使用） |
| `docs/docusaurus.config.ts` | Docusaurus 站點配置（`baseUrl: /nolang/`） |

### 構建 WASM 產物

Playground 依賴兩個 WASM 模組，均通過 Go 的 `wasip1` 目標交叉編譯：

```bash
make no-wasm    # 產出 docs/static/wasm/no.wasm
make lsp-wasm   # 產出 docs/static/wasm/lsp.wasm
```

也可一次性構建站點：

```bash
make playground  # 構建 no.wasm + lsp.wasm + Docusaurus 站點
```

### 本地開發（Hot-Reload）

啟動 Docusaurus 開發服務器，修改 Playground 源碼後自動熱更新：

```bash
cd docs && bun install    # 首次需安裝依賴
cd docs && bun run start  # 默認 http://localhost:3000/nolang/
```

開發服務器啟動後，訪問 http://localhost:3000/nolang/playground 即可使用 Playground。

> 注意：若修改了 Nolang 編譯器或 LSP 源碼（`src/`），需重新執行 `make no-wasm` 或 `make lsp-wasm` 後刷新頁面。

### Smoke Test

修改 Playground 後，可執行 smoke test 驗證 dev server、Playground 路由與 WASM 資源均可正常訪問：

```bash
make playground-smoke
```

該目標會：

1. 構建 `no.wasm` 與 `lsp.wasm`（若已存在且源碼未變則跳過）
2. 在後台啟動 Docusaurus dev server（默認端口 3000）
3. 等待服務器就緒後驗證以下路由返回 HTTP 200：
   - `/nolang/playground`
   - `/nolang/wasm/no.wasm`
   - `/nolang/wasm/lsp.wasm`
4. 自動停止 dev server

若端口 3000 被佔用，可通過 `PLAYGROUND_PORT` 環境變量指定其他端口：

```bash
make playground-smoke PLAYGROUND_PORT=3001
```

### CI

GitHub Actions（`.github/workflows/build.yml`）中的 `wasm-smoke` job 會在每個 PR 和非 tag push 時：

- 構建 `no.wasm` 與 `lsp.wasm`（`wasip1` 目標）
- 以 `wasmtime` 驗證 `no.wasm version` 與 `lsp.wasm -version` 輸出版本號
- 構建並運行 `tests/test-wasi-hello.no` 與 `tests/test-wasi-fib.no`（`wasm32-wasi` 目標，使用 Zig）
