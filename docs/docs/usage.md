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
| `no init`                                                    | 在當前目錄初始化倉庫    |
| `no new <name>`                                              | 建立新倉庫              |
| `no fmt [-w] [-d] <file\|dir>`                               | 格式化源代碼            |
| `no build [-o <file>] [-cc <s>] [-target <s>] [<file\|dir>]` | 構建（輸出 executable） |
| `no run [-cc <s>] [-target <s>] [<file\|dir>]`               | 構建並執行 main.no      |
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

### 創建新項目

```bash
# 創建新倉庫
no new test1

# 進入目錄
cd test1

# 直接運行（自動構建並執行 main.no）
no run
```

### 初始化現有目錄

```bash
# 在當前目錄初始化
no init
```

### 構建與運行

```bash
# 構建（默認尋找 main.no）
no build                    # 構建當前目錄
no build main.no            # 構建指定文件
no build -o output          # 指定輸出路徑
no build -cc zig            # 使用 Zig 編譯器
no build -target x86_64-linux-gnu  # 交叉編譯（指定目標平台）

# 運行（構建 + 執行）
no run                      # 構建並執行 main.no（必須有 main.no）
no run -cc zig
no run -target aarch64-macos-gnu
```

### 交叉編譯目標

`-target` 參數格式為 `<arch>-<os>-<abi>`，支持以下目標：

| 目標三元組           | 說明           |
| -------------------- | -------------- |
| `x86_64-linux-gnu`   | Linux x86_64   |
| `aarch64-linux-gnu`  | Linux ARM64    |
| `x86_64-macos-gnu`   | macOS x86_64   |
| `aarch64-macos-gnu`  | macOS ARM64    |
| `x86_64-windows-gnu` | Windows x86_64 |

### 編譯器選擇

`-cc` 參數指定 C 編譯器後端：

- `clang`（預設）— 需要安裝 LLVM
- `zig` — 需要安裝 Zig，適合交叉編譯

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
3. 將 binary 複製到 `~/.nolang/bin/`
4. 在 `/usr/local/bin/` 建立軟鏈接

### 卸載

```bash
no uninstall pkg-name
```

卸載會移除 `/usr/local/bin/` 中的軟鏈接和 `~/.nolang/bin/` 中的 binary。

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
