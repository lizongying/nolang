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
| `no init`                                                    | 定義工作區（生成 workspace.jsonc，不含 package.jsonc） |
| `no new <name>`                                              | 在工作區內新建包（子目錄 + package.jsonc，並註冊到 workspace.jsonc） |
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

Nolang 采用**單倉（項目）多包**架構：倉庫根目錄是工作區（只有 `workspace.jsonc`），每個包是一個子目錄（有自己的 `package.jsonc`）。`no init` 與 `no new` 是兩件獨立的事：

- `no init` —— 在當前目錄定義工作區（只生成 `workspace.jsonc`）。
- `no new <name>` —— 在當前工作區下新建一個包（生成子目錄 `./<name>/` 及其 `package.jsonc`），並把該包註冊進 `workspace.jsonc`。

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
  "foo": "/foo",
  "bar": "/bar",
  "baz": "/baz"
}
```

> **路徑格式**：`workspace.jsonc` 中的映射值使用 `/` 前綴表示工作區相對路徑（如 `/foo` → `workspaceRoot/foo`）。也支援 `./`、`../` 相對跳轉和作業系統絕對路徑（如 `/home/user/code/fork`），允許指向工作區外部目錄。

`no build` 在根目錄無參數運行時，會並行構建工作區內所有包。

### 只初始化工作區（不創建包）

```bash
# 在當前目錄定義工作區
no init
```

`no init` 只生成 `workspace.jsonc`（初始為空 `{}`），**不會生成 `package.jsonc` 或 `main.no`**。若 `workspace.jsonc` 已存在則不會被覆蓋。包需要通過 `no new <name>` 創建。

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

項目根目錄下的 `package.jsonc` 文件描述項目信息：

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

### 依賴類型與版本號規則

`package.jsonc` 中的依賴分為**本地包**和**線上包**兩類。編譯器會根據以下規則自動判定依賴類型，並在本地包未使用 `"*"` 版本時發出警告。

#### 判定規則

```
依賴鍵 → 查找 workspace.jsonc（短名稱或完整鍵匹配）
  ├─ 找到 → 本地包（應使用 "*"）
  └─ 未找到 → 檢查路徑前綴（/ 開頭）
      ├─ 是 → 本地包（應使用 "*"）
      └─ 否 → 線上包（使用版本號，不警告）
```

1. **查找 `workspace.jsonc`**：先以依賴鍵作為短名稱或完整鍵在 `workspace.jsonc` 中查找。找到即為本地包。
2. **檢查路徑前綴**：未在 `workspace.jsonc` 中找到時，若依賴鍵以 `/`（工作區相對路徑）開頭，亦為本地包。
3. **其餘為線上包**：不滿足上述條件的依賴鍵視為線上包，應指定版本號。

> **工作區邊界約束**：`package.jsonc` 中的本地路徑只能以 `/` 開頭（工作區相對），禁止 `./`、`../` 和作業系統絕對路徑。唯一允許突破工作區邊界的入口是 `workspace.jsonc` / `.workspace.jsonc` 映射。詳見 [工作區邊界約束](#工作區邊界約束)。

#### 本地包的多種引用形式

本地包支持以下三種引用方式，版本號均應使用 `"*"`：

```jsonc
"dependencies": {
  // 1. 短名稱：workspace.jsonc 中註冊的鍵
  "test2": "*",

  // 2. 工作區相對路徑：以 / 開頭，相對工作區根目錄
  "/example/test2": "*",

  // 3. 完整 URL：若 workspace.jsonc 中有對應映射則為本地包
  "github.com/lizongying/nolang/test2": "*",
}
```

> **注意**：
> - 第 3 種形式是否為本地包取決於 `workspace.jsonc` 中是否存在對應映射。若 `workspace.jsonc` 中將 `github.com/lizongying/nolang/test2` 映射到本地路徑，則該依賴為本地包（應使用 `"*"`）；否則為線上包（應使用版本號）。
> - `./` 和 `../` 前綴已不再允許在 `package.jsonc` 中使用。所有本地路徑必須以 `/` 開頭（工作區相對）。

#### 高級用法：線上包轉本地包

`workspace.jsonc` 不僅可以註冊短名稱，還可以將一個線上包名映射到本地路徑，實現「線上包轉本地包」的開發模式：

```jsonc
// workspace.jsonc
{
  "test2": "/example/test2",
  "github.com/lizongying/nolang/test2": "/example/test2"
}
```

此時 `package.jsonc` 中以 `github.com/lizongying/nolang/test2` 引用的依賴會被解析為本地包 `/example/test2`，版本號應使用 `"*"`。這在本地開發線上包時非常方便——只需在 `workspace.jsonc` 中添加映射，即可將遠端依賴切換為本地源碼進行聯調。

#### 版本號警告

編譯時若檢測到本地包使用了非 `"*"` 的版本號（如 `"v0.1.0"`），會輸出警告提示使用 `"*"`。線上包則不受此限制。

#### 遞迴工作區映射（跨包映射鏈）

Nolang 支援**遞迴工作區映射**：被依賴的包可以自帶 `workspace.jsonc`，內部繼續存在映射規則，形成自然的跨包解析鏈。這是 Nolang 的核心差異化能力——主流 Go/Cargo 的 `replace`/`patch` 僅生效於當前項目，不會傳遞到依賴包內部。

**適用場景：**
- 基礎庫統一內部別名
- 標準化導入路徑
- 多層庫兼容遷移

**工作原理：**

當解析依賴鍵時，編譯器會：
1. 在當前 `workspace.jsonc` 中查找鍵
2. 若找到映射，檢查目標目錄是否自帶 `workspace.jsonc`
3. 若有，繼續在同一鍵的基礎上查找，逐層解析
4. 直到沒有更深的映射為止

**範例：**

```
workspace/                  ← 頂層工作區
├── workspace.jsonc         ← {"testkey": "/pkgA"}
├── pkgA/
│   ├── workspace.jsonc     ← {"testkey": "/pkgB"}
│   └── pkgB/
│       ├── workspace.jsonc ← {"testkey": "/pkgC"}
│       └── pkgC/           ← 最終目標（無 workspace.jsonc）
```

解析 `testkey` 時：
1. 頂層 `workspace.jsonc` 映射 → `/pkgA`（工作區相對）
2. `pkgA/workspace.jsonc` 映射 → `/pkgB`（pkgA 工作區相對）
3. `pkgB/workspace.jsonc` 映射 → `/pkgC`（pkgB 工作區相對）
4. `pkgC` 無 `workspace.jsonc` → 最終解析到 `pkgC`

**循環檢測：**

編譯器維護一條解析訪問棧，跟蹤已訪問的工作區根目錄。一旦檢測到循環映射（如 A→B, B→A），立刻報錯並輸出完整鏈路：

```
Error: circular workspace mapping detected: /path/to/A → /path/to/B → /path/to/A
```

> **注意**：此能力僅支援跨包的天然遞迴映射（被依賴包自帶 `workspace.jsonc`），不支援單文件手動鏈式簡寫。

### 鏡像配置

在 `package.jsonc` 的 `mirrors` 數組中配置鏡像地址，用於加速遠端包下載：

```jsonc
"mirrors": [
  "https://mirror.example.com/"
]
```

### 工作區配置 workspace.jsonc

`workspace.jsonc` 位於倉庫根目錄，描述一個**單倉多包**工作區，是一個「包名 → 相對路徑」的映射。`no build` 在無參數且存在 `workspace.jsonc` 時，會並行編譯其中列出的所有包；`no run` 則透過包名（如 `no run foo`）執行工作區中的單個包。

```jsonc
{
  "foo": "/foo",
  "bar": "/bar"
}
```

`no init` 負責定義工作區：若 `workspace.jsonc` 不存在則生成一個空對象 `{}`，**不生成任何 `package.jsonc`**；已存在時則保持不變，不會被覆蓋。隨後用 `no new <name>` 創建包時，會自動把 `"<name>": "./<name>"` 註冊進 `workspace.jsonc`。

#### 本機私有配置 .workspace.jsonc

除公共的 `workspace.jsonc` 外，Nolang 還支援本機私有配置檔案 `.workspace.jsonc`（注意前導 `.`）。該檔案建議加入 `.gitignore`，用於開發者個人的臨時依賴替換。

**加載邏輯：**

1. 先加載公共配置 `workspace.jsonc`
2. 再加載私有配置 `.workspace.jsonc`
3. **相同 key**：私有配置覆蓋公共配置
4. **新 key**：相互疊加

```
workspace/
├── workspace.jsonc       ← 工程共享配置（納入版本管理）
├── .workspace.jsonc      ← 本機私有配置（建議加入 .gitignore）
├── foo/
└── bar/
```

**使用場景：**

- 團隊統一別名、依賴轉發規則寫在 `workspace.jsonc` 中，納入版本管理
- 開發者本地調試時需要臨時將某個依賴指向本地 fork，在 `.workspace.jsonc` 中覆蓋即可
- 避免臨時 fork 映射污染項目公共配置，兼顧大型團隊協作與開發者本地開發靈活性

**範例：**

```jsonc
// workspace.jsonc（公共，版本管理）
{
  "foo": "/foo",
  "bar": "/bar"
}

// .workspace.jsonc（私有，本地調試）
{
  // 覆蓋公共配置：foo 指向本地 fork（工作區內）
  "foo": "/my-fork-foo",
  // 新增私有映射：指向工作區外部的 fork（OS 絕對路徑，唯一逃逸出口）
  "github.com/lizongying/nolang/core": "/home/lzy/code/fork/core",
  // 新增私有映射（工作區內）
  "experimental": "/experimental"
}
```

最終生效的映射：`foo` → `/my-fork-foo`（私有覆蓋），`bar` → `/bar`（公共保留），`github.com/lizongying/nolang/core` → `/home/lzy/code/fork/core`（私有新增，逃逸工作區），`experimental` → `/experimental`（私有新增）。

### 工作區邊界約束

Nolang 對路徑引用實施嚴格的工作區邊界控制，確保項目的可移植性和安全性。

#### 規則總覽

| 配置層級 | `/` 工作區相對 | OS 絕對路徑 | `./` `../` 相對跳轉 |
| --- | --- | --- | --- |
| `package.jsonc` 依賴鍵 | ✅ 允許 | ❌ 禁止 | ❌ 禁止 |
| `workspace.jsonc` 映射值 | ✅ 允許 | ✅ 允許（逃逸出口） | ✅ 允許（逃逸出口） |
| `.workspace.jsonc` 映射值 | ✅ 允許 | ✅ 允許（逃逸出口） | ✅ 允許（逃逸出口） |
| 源碼 `#` 導入路徑 | ✅ 允許 | ❌ 禁止 | ❌ 禁止 |

#### 1. package.jsonc（嚴格受限，禁止逃逸工作區）

`package.jsonc` 是項目對外發布、CI、多人協作的標準基線配置，提交到代碼倉庫。

```jsonc
"dependencies": {
    // ✅ 合法：以 / 開頭，相對於 workspace 根
    "/vendor/test": "*",
    // ❌ 禁止：作業系統絕對路徑 /home/xxx/...
    // ❌ 禁止：./ ../ 相對跳轉
}
```

**約束：**
- 本地路徑只能寫 `/xxx`，基準 = workspace 根目錄
- 不接受作業系統絕對路徑
- 不支援 `./`、`../`
- 無法直接引用工作倉庫外部任何目錄

**目的：** 如果允許外部路徑，極易出現「本機能編譯，別人編譯失敗」，也存在意外加載外部陌生代碼的安全隱患。

#### 2. workspace.jsonc + .workspace.jsonc（唯一逃逸出口）

映射目標支援三種本地路徑：
- `/xxx` → workspace 根內部目錄（和 package.jsonc 語義一致）
- `./xxx`、`../xxx` → 相對於 workspace 根目錄解析，允許逃逸工作區邊界
- 作業系統絕對路徑 → 允許指向工作區外部（fork、外部組件）

```jsonc
// .workspace.jsonc 私有配置，用於本地開發
{
  // 工作區內部
  "foo": "/vendor/foo",
  // 相對跳轉：指向工作區外部的 fork
  "github.com/lizongying/nolang/core": "../fork/core",
  // OS 絕對路徑：指向任意外部目錄
  "github.com/lizongying/nolang/utils": "/home/lzy/code/utils"
}
```

**關鍵邏輯：** `package.jsonc` 沒有能力直接引用外部代碼；用戶想要掛載倉庫外源碼，必須顯式在 workspace 配置映射。相當於給「跨目錄加載」增加一道明確、可見的開關，所有外部依賴集中一處管控。

#### 3. 源碼導入層面配套約束

源碼內不允許直接書寫作業系統絕對路徑導入；所有外部代碼引用，只能通過 workspace 別名或遠端包標識間接引入。`./` 和 `../` 相對跳轉同樣被禁止。

```no
; ✅ 合法：工作區相對路徑
# /vendor/utils.helper

; ✅ 合法：遠端包標識
# github.com/lizongying/nolang/core.process

; ✅ 合法：標準庫
# std/fs

; ❌ 禁止：相對跳轉
; # ./utils.helper
; # ../lib/helper.process

; ❌ 禁止：作業系統絕對路徑
; # /home/user/code/core.process
```

#### 數據流鏈路演示

場景：業務項目 workspace 根在 `/code/project`，想要使用外部 `/code/fork/core`

❌ 不能這麼幹（package.jsonc 禁止）：

```jsonc
// package.jsonc
"/code/fork/core": "*" // 非法：無法逃逸工作區
```

✅ 標準做法（通過 workspace 映射突破邊界）：

```jsonc
// .workspace.jsonc
"github.com/lizongying/nolang/core": "/code/fork/core"
```

```no
; 源碼正常導入
# github.com/lizongying/nolang/core
```

#### 工作區流程

編譯和執行的入口目錄始終是**工作區目錄**（`workspace.jsonc` 所在目錄）。整體流程如下：

1. 用戶在工作區目錄中執行 `no build` 或 `no run <包名>`
2. 編譯器讀取 `workspace.jsonc`，按包名查找對應的子目錄路徑
3. 載入該子目錄中的 `package.jsonc`（即包根目錄），以包根目錄為基準進行構建/運行
4. 所有導入路徑（`use /path/to/module`）**相對於工作區根目錄解析**（見[路徑解析約定](path-resolution.md)），即以 `workspace.jsonc` 所在目錄為唯一基準，不再依賴導入方所在的包。

```
workspace/               ← 工作區目錄（workspace.jsonc 所在此處）
├── workspace.jsonc      ← 包名 → 路徑映射
├── foo/                 ← 包 foo
│   ├── package.jsonc        ← foo 的包根目錄
│   ├── main.no
│   └── lib.no
└── bar/                 ← 包 bar
    ├── package.jsonc        ← bar 的包根目錄
    └── main.no
```

> **注意**：導入路徑以**工作區根目錄**（`workspace.jsonc` 所在目錄）為準，而非包的 `package.jsonc` 目錄。包子目錄中若存在嵌套的 `package.jsonc`，`LoadPackage` 會向上搜索找到最近的 `package.jsonc` 並將其作為包根目錄，但導入解析統一以工作區根為基準。

---

## 應用案例

- [notools](https://github.com/lizongying/notools) — 使用 Nolang 語言實現的 Unix 常用命令行工具集，涵蓋 cat、ls、grep、wc、head、tail 等經典工具，展示了 Nolang 在系統編程領域的實際應用能力。
