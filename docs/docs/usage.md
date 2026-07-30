---
sidebar_position: 2
---

# 安裝與使用

## 安裝

### 安裝 CLI

從 [GitHub Releases](https://github.com/lizongying/nolang/releases/latest) 下載對應平台的可執行文件，或使用以下方式安裝：

```bash
# macOS / Linux
# 1. 下載二進制文件
# 2. 放到 PATH 中
sudo mv nolang /usr/local/bin/no
```

### 安裝 VS Code 插件

從 [VS Code Marketplace](https://marketplace.visualstudio.com/items?itemName=lizongying.vscode-nolang) 安裝 Nolang 擴展，提供語法高亮、LSP 診斷、跳轉定義、自動補全等支持。

## CLI 命令

| 命令                                                         | 說明                    |
| ------------------------------------------------------------ | ----------------------- |
| `no version`                                                 | 打印版本信息            |
| `no init`                                                    | 定義工作區（生成 workspace.jsonc，不含 mod.jsonc） |
| `no new <name>`                                              | 在工作區內新建包（子目錄 + mod.jsonc，並註冊到 workspace.jsonc） |
| `no fmt [-w] [-d] <file\|dir>`                               | 格式化源代碼            |
| `no build [-o <file>] [-cc <s>] [-target <s>] [<file\|dir>]` | 構建（輸出 executable） |
| `no run [-cc <s>] [-target <s>] [<package\|dir\|file>]`        | 構建並執行（包名/目錄/文件） |
| `no test [-cc <s>] [-target <s>] [<file>]`                   | 執行測試                |
| `no add <pkg>`                                               | 添加依賴                |
| `no remove <pkg>`                                            | 移除依賴                |
| `no update <pkg>`                                            | 更新依賴                |
| `no update-all`                                              | 更新所有依賴            |
| `no list`                                                    | 列出依賴                |
| `no sync`                                                    | 同步依賴                |
| `no install [-u] [<pkg>@<version>]`                          | 安裝 binary             |
| `no uninstall <name>`                                        | 移除 binary             |
| `no pub --token <token> [--registry <url>]`                  | 發布至 registry         |

## 快速開始

Nolang 采用**單倉（項目）多包**架構：倉庫根目錄是工作區（只有 `workspace.jsonc`），每個包是一個子目錄（有自己的 `mod.jsonc`）。`no init` 與 `no new` 是兩件獨立的事：

- `no init` —— 在當前目錄定義工作區（只生成 `workspace.jsonc`）。
- `no new <name>` —— 在當前工作區下新建一個包（生成子目錄 `./<name>/` 及其 `mod.jsonc`），並把該包註冊進 `workspace.jsonc`。

### 初始化工作區並新建包

```bash
# 1) 在倉庫根目錄定義工作區
no init

# 2) 在工作區內新建包（會自動註冊到 workspace.jsonc）
no new foo

# 3) 進入包目錄
cd foo

# 直接運行（自動構建並執行 main.no）
no run
```

可在工作區內反覆使用 `no new` 添加更多包：

```bash
no new bar
no new baz
```

此時 `workspace.jsonc` 形如：

```jsonc
{
  "foo": "./foo",
  "bar": "./bar",
  "baz": "./baz"
}
```

`no build` 在根目錄無參數運行時，會並行構建工作區內所有包。

### 只初始化工作區（不創建包）

```bash
# 在當前目錄定義工作區
no init
```

`no init` 只生成 `workspace.jsonc`（初始為空 `{}`），**不會生成 `mod.jsonc` 或 `main.no`**。若 `workspace.jsonc` 已存在則不會被覆蓋。包需要通過 `no new <name>` 創建。

### 構建與運行

```bash
# 構建（默認尋找 main.no）
no build                    # 構建當前目錄
no build main.no            # 構建指定文件
no build -o output          # 指定輸出路徑
no build -cc zig            # 使用 Zig 編譯器
no build -target x86_64-linux-gnu  # 交叉編譯（指定目標平台）

# 運行（構建 + 執行）
no run                      # 構建並執行當前目錄的 main.no
no run foo                  # 運行工作區中名為 foo 的包（解析 workspace.jsonc）
no run ./foo                # 運行 ./foo 目錄的 main.no
no run main.no              # 運行指定 .no 文件
no run -cc zig
no run -target aarch64-macos-gnu
```

`no run` 的位置參數按以下順序解析：

1. 已存在的 `.no` 文件 → 直接運行該文件
2. 已存在的目錄 → 運行其中的 `main.no`
3. 工作區（最近的 `workspace.jsonc`）中註冊的包名 → 運行該包的 `main.no`

無參數時，默認運行當前目錄的 `main.no`。若當前目錄是工作區根（含 `workspace.jsonc` 但沒有 `main.no`），會提示用 `no run <package>` 指定包。

### 交叉編譯目標

`-target` 參數格式為 `<arch>-<os>-<abi>`，支持以下目標：

| 目標三元組           | 說明           |
| -------------------- | -------------- |
| `x86_64-linux-gnu`   | Linux x86_64   |
| `aarch64-linux-gnu`  | Linux ARM64    |
| `x86_64-macos-gnu`   | macOS x86_64   |
| `aarch64-macos-gnu`  | macOS ARM64    |
| `x86_64-windows-gnu` | Windows x86_64 |

**自動檢測當前平台**：`no build`、`no run`、`no test` 在未指定 `-target` 時，會自動檢測當前宿主平台並編譯為本機代碼。日常開發無需手動指定 target：

```bash
no run hello.no          # 本機直接跑
no test                  # 本機跑測試
no build -target aarch64-linux-gnu   # 需要交叉編譯時才顯式指定
```

### 編譯器選擇

`-cc` 參數指定 C 編譯器後端：

- `clang`（預設）— 需要安裝 LLVM
- `zig` — 需要安裝 Zig，適合交叉編譯

## JS 後端（JavaScript Backend）

Nolang 支援將 `.no` 原始碼直接編譯為 JavaScript，無需 LLVM 工具鏈。JS 後端採用**型別擦除**（type erasure）策略：所有 Nolang 型別標註（`int`/`str`/`bool`/`vec[T]`/`[N]T`/`?T` 等）在 JS 輸出中不保留，僅生成執行時行為。

### 構建為 JavaScript

```bash
# 編譯為 JS（輸出至 dist/<name>.js）
no build --js main.no

# 指定輸出路徑
no build --js -o app.js main.no

# 瀏覽器模式：生成 JS + HTML wrapper
no build --js --browser main.no
# 輸出: dist/<name>.js 和 dist/<name>.html
```

### 運行 JavaScript

```bash
# 編譯為 JS 並以 node 執行
no run --js main.no

# 編譯為瀏覽器 JS + HTML 並在預設瀏覽器中打開
no run --js --browser main.no
```

### JS 後端特性

| 特性 | 說明 |
| --- | --- |
| **無需 LLVM** | 直接從 AST 發射 JS 原始碼，不依賴 clang/LLVM 工具鏈 |
| **型別擦除** | JS 為動態型別，Nolang 型別標註不保留 |
| **Node.js 模式** | 預設目標，可用 `require('fs')` 等 Node API |
| **瀏覽器模式** | `--browser` 生成 HTML wrapper，`print()` 輸出重導向至 `#nolang-output` div |
| **平台標註** | `#{js}` 標記 JS 後端專用宣告；`#{js-browser}` 標記瀏覽器專用代碼 |

### JS 後端標準庫

`src/js/` 目錄提供 JS 後端專用模組，均帶有 `#{js}` 平台標註，僅在 JS 後端編譯時生效：

| 模組 | 說明 |
| --- | --- |
| `js/dom` | DOM 操作（create-element、query-selector、set-text、set-style 等） |
| `js/canvas` | Canvas 2D 繪圖（fill-rect、stroke、begin-path 等） |
| `js/events` | 事件處理（on-click、on-load） |
| `js/storage` | localStorage（set-item、get-item） |
| `js/fetch` | Fetch API（async 獲取 URL 數據） |
| `js/console-log` | console.log 封裝 |
| `js/fs-read-file` | Node.js fs.readFileSync 封裝 |
| `js/fs-write-file` | Node.js fs.writeFileSync 封裝 |
| `js/http-fetch` | fetch API 封裝（Node 18+ / 瀏覽器） |
| `js/process-exit` | process.exit 封裝 |
| `js/location` | Location API（href、search、redirect） |
| `js/history` | History API（back、forward、push） |
| `js/animation` | 動畫幀（request-frame、cancel-frame） |

### 瀏覽器應用示例

```no
; main.no — 瀏覽器應用
# js/dom
# js/canvas
# js/events
# js/storage

; DOM: 創建元素並附加到 body
heading = dom.create-element('h2')
heading.set-text('Hello from Nolang!')
body = dom.body()
body.append-child(heading)

; 事件: 按鈕點擊
btn = dom.create-element('button')
btn.set-text('Click me')
body.append-child(btn)
events.on-click(btn, () {
    print('button was clicked!')
})

; localStorage: 保存和讀取
storage.set-item('greeting', 'Hello from localStorage')
g = storage.get-item('greeting')
print('stored:', g)
```

構建並打開：

```bash
no build --js --browser main.no
# 輸出: dist/main.js 和 dist/main.html
# 在瀏覽器中打開 dist/main.html
```

### 內建函數映射

JS 後端將 Nolang 內建函數映射為 JavaScript 等價物：

| Nolang | JavaScript |
| --- | --- |
| `print(x)` | `console.log(x)` |
| `eprint(x)` | `console.error(x)` |
| `format(...)` | 字串拼接 |
| `len(x)` | `x.length` |
| `with-len(n)` | `new Array(n)` |

### 平台標註

使用 `#{js}` 和 `#{js-browser}` 標註控制代碼的平台可見性：

```no
; 僅在 JS 後端編譯時保留
#{js}
js-helper = () {
    print('JS only code')
}

; 僅在瀏覽器模式保留
#{js-browser}
print('running in browser mode')

; 僅在原生後端保留
#{mac-arm64}
print('running on macOS ARM64')
```

## 入口規則

- **main.no** — 程序入口
- **lib.no** — 庫入口，導出函數（詳見[導出文檔](lang/export)）
- **test/ 目錄下所有 .no 文件** — 包含測試斷言

## 測試

```bash
# 測試test目錄下所有 .no 文件
no test

# 執行單個測試文件
no test my-test.no

# 使用指定編譯器或目標
no test -cc zig
no test -target x86_64-windows-gnu
```

測試說明：

- 测试文件统一放在 test/ 目录下
- 每個測試文件獨立構建
- 若任一測試失敗，返回非零退出碼

## 安裝與卸載 Binary

### 安裝

```bash
# 安裝當前目錄的包
no install

# 強制重構（更新）
no install -u

# 從遠端倉庫安裝指定版本
no install pkg-name@1.0

# 更新已安裝的包
no install -u pkg-name@1.0
```

安裝流程：

1. 下載包源碼（遠端包）或使用當前目錄（本地包）
2. 自動執行構建
3. 將 binary 複製到 `~/no/bin/`
4. 在 `/usr/local/bin/` 建立軟鏈接

### 卸載

```bash
no uninstall pkg-name
```

卸載會移除 `/usr/local/bin/` 中的軟鏈接和 `~/no/bin/` 中的 binary。

## 項目配置

項目根目錄下的 `mod.jsonc` 文件描述項目信息：

```jsonc
{
  "name": "my-project",
  "version": "0.1.0",
  "description": "A new Nolang project",
  "keywords": [],
  "author": "",
  "email": "",
  "organization": "",
  "repository": "",
  "homepage": "",
  "license": "MIT",
  "workspace": "",
  "mirrors": [],
  "dependencies": {
    "fmt": "*",
  },
  "compiler": {
    "version": "0.1.0",
  },
  "output": "./dist",
  "ignore": [],
}
```

### 依賴管理

```bash
# 添加依賴（版本號可省略，倉庫中不寫版本號）
no add pkg-name

# 移除依賴
no remove pkg-name

# 更新依賴
no update pkg-name

# 更新所有依賴
no update-all

# 列出依賴
no list

# 同步依賴（下載並生成鎖文件）
no sync
```

### 鏡像配置

在 `mod.jsonc` 的 `mirrors` 數組中配置鏡像地址，用於加速遠端包下載：

```jsonc
"mirrors": [
  "https://mirror.example.com/"
]
```

### 工作區配置 workspace.jsonc

`workspace.jsonc` 位於倉庫根目錄，描述一個**單倉多包**工作區，是一個「包名 → 相對路徑」的映射。`no build` 在無參數且存在 `workspace.jsonc` 時，會並行編譯其中列出的所有包；`no run` 則透過包名（如 `no run foo`）執行工作區中的單個包。

```jsonc
{
  "foo": "./foo",
  "bar": "./bar"
}
```

`no init` 負責定義工作區：若 `workspace.jsonc` 不存在則生成一個空對象 `{}`，**不生成任何 `mod.jsonc`**；已存在時則保持不變，不會被覆蓋。隨後用 `no new <name>` 創建包時，會自動把 `"<name>": "./<name>"` 註冊進 `workspace.jsonc`。
