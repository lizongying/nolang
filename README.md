# Nolang

[繁體中文](README.md) · [简体中文](README_zh-CN.md) · [한국어](README_ko.md) · [日本語](README_ja.md) · [English](README_en.md) · [Русский](README_ru.md) · [Français](README_fr.md) · [Español](README_es.md) · [Deutsch](README_de.md) · [Português](README_pt.md)

Nolang 是一種內存安全、性能強勁、語法極簡的系統編程語言。

## 核心特性

- **內存安全**: 自动內存管理
- **性能強勁**: 編譯期優化，無 GC
- **語法極簡**: 語法更簡單
- **意圖驅動**: 充分尊重開發者意圖，語義符合人類直覺
- **永不崩溃**: 消除隱式崩潰路徑

## 安裝

一鍵安裝（macOS / Linux / Windows Git Bash），會自動下載最新的 `no` 到 `$HOME/.local/bin`：

```bash
curl -fsSL https://raw.githubusercontent.com/lizongying/nolang/main/install.sh | sh
```

默認裝**內置 LLVM** 的版本（`no-llvm_*`，不必另裝 LLVM / opt / llc）。若想要體積較小的純 Go 版，用 `NO_FLAVOR=plain` 並搭配 `no build -cc zig`：

```bash
curl -fsSL https://raw.githubusercontent.com/lizongying/nolang/main/install.sh | NO_FLAVOR=plain sh
```

需要固定版本或自訂目錄時（環境變數要傳給 `sh`，所以先存檔再執行）：

```bash
curl -fsSL https://raw.githubusercontent.com/lizongying/nolang/main/install.sh -o install.sh
NO_VERSION=0.4.28 NO_INSTALL_DIR="$HOME/bin" sh install.sh
```

| 環境變數         | 預設                   | 說明                                                |
| ---------------- | ---------------------- | --------------------------------------------------- |
| `NO_VERSION`    | `latest`               | 版本號，可寫 `0.4.28` 或 `v0.4.28`                 |
| `NO_FLAVOR`     | `embedded`             | `embedded` 內置 LLVM；`plain` 純 Go（需系統 LLVM） |
| `NO_INSTALL_DIR` | `$HOME/.local/bin`     | 安裝目錄                                            |
| `NO_REPO`       | `lizongying/nolang`    | 來源倉庫                                            |

安裝後執行 `no version` 驗證。若 `$HOME/.local/bin` 不在 `PATH`，請自行加入：

```bash
export PATH="$HOME/.local/bin:$PATH"
```

> 編譯 `.no` 程式還需要一個 C 連結器：推薦 `no build -cc zig`（zig 自帶 clang / lld / libc），或自行安裝 clang。

## Usage

doc

[docs](https://lizongying.github.io/nolang/)

vscode 插件

[vscode](https://marketplace.visualstudio.com/items?itemName=lizongying.vscode-nolang)

nolang cli

[nolang](https://github.com/lizongying/nolang/releases/latest)

nohub 包清單（社群包搜尋）

[nohub](https://lizongying.github.io/nohub/)

### 社群包搜尋（nohub）

`no` 直接讀取 nohub 的套件索引，提供三個唯讀命令：

```bash
no search <query>    # 依包名、關鍵詞、分類搜尋社群包
no info <pkg>        # 顯示套件中繼資料與安裝/導入命令
no docs <pkg>        # 列印套件文件網址（docs → homepage → repository）
```

預設讀取公共索引。要疊加多個 nohub 實例，在 `~/no/sources.noml` 配置源列表（陣列順序即優先順序）：

```
{
  sources: [
    'https://lizongying.github.io/nohub',
    'corp = https://corp.example/nohub',
  ]
}
```

`NOHUB_SOURCES`（逗號分隔）可暫時覆蓋。三個命令都接受 `-source <name>` 只看指定源；`no search` 的輸出含 `SOURCE` 欄，同名同版本由靠前的源優先。單一源拉取失敗只會告警，不影響其他源。`no` 只 fetch 每個源的 `index.noml`（`<地址>/index.noml`，與搜尋頁首屏同一份資料，每包一條最新版本），不下載 tarball、不讀取 `data.js`，詳情欄位映射與 `web/app.js` 一致。也可在 [nohub 搜尋頁](https://lizongying.github.io/nohub/) 用網頁瀏覽。


### CLI 命令

| 命令                                                         | 說明                                   |
| ------------------------------------------------------------ | -------------------------------------- |
| `no init`                                                    | 定義工作區（生成 workspace.jsonc，不含 package.jsonc） |
| `no new <name>`                                              | 在工作區內新建包（子目錄 + package.jsonc，並註冊到 workspace.jsonc） |
| `no fmt [-w] [-d] <file\|dir>`                               | 格式化源代碼                           |
| `no build [-o <file>] [-cc <s>] [-target <s>] [<file\|dir>]` | 構建（輸出 executable）                |
| `no run [-cc <s>] [-target <s>] [<package\|dir\|file>]`        | 構建並執行（包名/目錄/文件）           |
| `no test [-cc <s>] [-target <s>] [<file\|dir>]`              | 執行測試                               |
| `no add <pkg>`                                               | 添加依賴                               |
| `no remove <pkg>`                                            | 移除依賴                               |
| `no update <pkg>`                                            | 更新依賴                               |
| `no update-all`                                              | 更新所有依賴                           |
| `no list`                                                    | 列出依賴                               |
| `no search <query>`                                            | 依包名、關鍵詞、分類搜尋社群包 |
| `no info <pkg>`                                                | 顯示套件中繼資料與安裝/導入命令 |
| `no docs <pkg>`                                                | 列印套件文件網址 |
| `no sync`                                                    | 同步依賴                               |
| `no install [-u] [<tool>[:<version>]@<host>/<owner>/<repo>]` | 安裝 binary（~/no/bin/ + 軟鏈接）     |
| `no uninstall <name>`                                        | 移除 binary 及軟鏈接                   |
| `no pub --token <token> [--registry <url>]`                  | 發布至 registry                        |

```bash
# 構建（默認 main.no 開始）
no build                    # 構建當前目錄
no build main.no            # 構建指定文件
no build -o output          # 指定輸出路徑
no build -cc zig            # 使用 Zig 編譯器
no build -target x86_64-linux-gnu  # 交叉編譯（指定目標平台）

# 運行（構建 + 執行）
no run                    # 構建並執行當前目錄的 main.no
no run foo                # 運行工作區中名為 foo 的包
no run ./foo              # 運行 ./foo 目錄的 main.no
no run -cc zig
no run -target aarch64-macos-gnu

# 測試（構建 + 執行）
no test                   # 測試`test`目錄下所有 .no 文件
no test test/my-test.no        # 執行單個測試文件
no test -cc zig
no test -target x86_64-windows-gnu
```

### 創建新項目

```shell
no new test1

cd test1

no run
```

### Hello World

新建項目後，編輯 `main.no`：

```no
print('Hello, World!')
```

運行：

```bash
no run
```

輸出：

```
Hello, World!
```

### 入口規則

- **main.no** — 程序入口
- **lib.no** — 庫入口，導出函數
- **tests/ 目錄下所有 .no 文件** — 包含測試斷言

### 測試說明

- 测试文件统一放在 tests/ 目录下
- 每个测试文件独立构建
- 若任一测试失败，返回非零退出码

```bash
no vet src/std                     # Nolang vet 檢查標準庫
```

## 從源碼構建

構建 Nolang 應使用 `make`：

```bash
make            # 構建所有目標（bin/no 和 vscode-nolang/server/lsp）
make no         # 構建 bin/no
make lsp        # 構建 LSP server
make package    # 構建 LSP 並打包 VSCode 擴展
make no-wasm    # 跨譯 no 為 WebAssembly
make lsp-wasm   # 跨譯 LSP 為 WebAssembly
make playground # 構建 no.wasm + lsp.wasm + Docusaurus 站點
make clean      # 清理構建產物
make help       # 查看所有構建目標
```

> ⚠️ **構建 Nolang 必須使用 `make`，切勿用 `cd src && go build` 直接構建。**
>
> `make` 在建構前會自動執行 `make gen`，重新產生 `src/checker/stdsig_gen.go`
> —— 一份**內嵌進二進位**的標準庫簽名表（`funcSigs` / `methodSigs` /
> `funcParams` / `methodParams` / `structFields` / `aliases` / `structMod` /
> `enumVariants` / `embeddedStdModules`）。有了它，每次 `no build` 都跳過「全量解析所有 std」的
> PASS1，啟動明顯更快。
>
> 如果你修改過 `src/std` 卻**沒有**先 `make gen` 就直接 `go build`，編譯器會
> 偵測到內嵌簽名表與 `src/std` 內容不一致，並**直接報錯拒絕建構**（提示你執行
> `make no`）。這是刻意設計：過去靜默退化為全量解析會讓每次編譯都變慢且不易
> 察覺。修改 `src/std` 後請一律執行 `make no`（它會先 `make gen` 再建構）。
>
> 執行測試也請用 `make test`：**直接 `go test` 會因未注入簽名 key（`StdSigKey` 為空）而報錯拒絕**。`make test` 在建構測試二進位時注入同樣的 key，使其走內嵌簽名表快路徑。

---

## 詞條庫（glossary）

`glossary/` 是一份**詞條化的 Nolang 知識庫**：每個名詞一條記錄，帶類型、定義、語法、
示例、關聯引用與一句話摘要。共 **244 條 / 10 個語義域**（類型、語法、註解、內存、
錯誤、模組、編譯器內部、工具鏈、std 全部模組、已知坑），資料本身是 `.noml`。

前端用 Nolang 寫成，同時提供 CLI 與 MCP（stdio / HTTP），人和 AI 都能查：

```bash
no build glossary                        # 產物：glossary/dist/glossary

./glossary/dist/glossary get option      # 取單條詞條全文
./glossary/dist/glossary search 三態      # 關鍵詞檢索
./glossary/dist/glossary list --domain=traps   # 已知坑一覽
./glossary/dist/glossary check           # 一致性體檢（CI 用）

./glossary/dist/glossary mcp             # MCP server（stdio）
./glossary/dist/glossary mcp --http=127.0.0.1:8765
```

輸出可切 `--format=text|md|json`。接入與資料格式說明見 [`glossary/README.md`](glossary/README.md)。

---

## 相關項目

- [nohub — Nolang 生態的社群包清單搜尋](https://lizongying.github.io/nohub/)

  按包名、關鍵詞、分類檢索收錄的社群包，查看包資訊、版本快照與原始碼入口；索引由 `web/build.mjs` 從 `packages/` 快照產生。

- [Benchmarks Game — Nolang vs C vs Rust 性能對比報告](https://github.com/lizongying/no-benchmarks/results/report.md)

- [notools — 使用 Nolang 實現的 Unix 命令行工具集](https://github.com/lizongying/notools)

  Nolang 應用案例：使用 Nolang 語言實現的 Unix 常用命令行工具集，涵蓋 cat、ls、grep、wc、head、tail 等經典工具，展示了 Nolang 在系統編程領域的實際應用能力。

---

## TODO

## 许可证

Apache-2.0
