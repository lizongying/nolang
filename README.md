# Nolang

Nolang 是一種無 GC、內存安全、語法極簡的系統編程語言。

## 核心特性

- **無 GC**：不依賴垃圾回收，自动安全內存管理
- **內存安全**：延遲move，作用域離開自動釋放，杜絕懸垂引用、內存泄漏
- **語法極簡**：減少關鍵字，無冗余語法
- **類型推斷**: 變量無需过度聲明類型
- **統一引用傳遞**：所有參數預設為引用
- **模块系统** - 每个文件即一个独立模块
- **提前批量堆分配**
- **作用域離開批free**

### 函數作用域變量覆蓋

在函數作用域內，如果重複使用相同的變量名進行賦值，Nolang 將其視為覆蓋重賦值，而非創建新的棧變量。不触发变量遮蔽
如果类型不同，语法不允许

### 統一引用傳遞模型

Nolang 採用統一引用傳遞模型，所有函數參數預設均為引用型別。這意味著：

- 函數內修改 = 外部變量直接改變
- 函數內對參數的任何修改，都會直接作用於調用方的原始數據
- 可修改，但不可销毁

### 內存安全機制

- **延遲move**
- **變數自動銷毀** 函數結束自動銷毀所有內部變數
- **禁止手動釋放** 避免誤刪導致的懸垂引用
- **值複製容器** 數組 / 切片存副本，與原變數分離， 原變量生命周期結束並銷毀時，容器內的數據不受任何影響
- **無 GC、無分配隱藏成本**

[docs](https://lizongying.github.io/nolang/)

## Usage

vscode 插件

[vscode](https://marketplace.visualstudio.com/items?itemName=lizongying.vscode-nolang)

安裝nolang cli

[nolang](https://github.com/lizongying/nolang/releases/latest)

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
| `no sync`                                                    | 同步依賴                               |
| `no install [-u] [<pkg>@<version>]`                          | 安裝 binary（~/no/bin/ + 軟鏈接）     |
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

### 入口規則

- **main.no** — 程序入口
- **lib.no** — 庫入口，導出函數
- **tests/ 目錄下所有 .no 文件** — 包含測試斷言

### 測試說明

- 测试文件统一放在 tests/ 目录下
- 每个测试文件独立构建
- 若任一测试失败，返回非零退出码

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

### 修改代碼

修改代碼前，請先閱讀 [.agents/skills/nolang-syntax/SKILL.md](.agents/skills/nolang-syntax/SKILL.md) 了解 Nolang 語法規範。

> ⚠️ **嚴禁使用 `git checkout` 和 `git reset`**，任何時候都不得使用，以防止覆蓋他人正在修改的代碼。

修改後需執行以下檢查：

```bash
./vscode-nolang/server/lsp vet src/std   # LSP vet 檢查標準庫
./bin/no vet src/std                     # Nolang vet 檢查標準庫
```

---

## 相關項目

- [Benchmarks Game — Nolang vs C vs Rust 性能對比報告](https://github.com/lizongying/no-benchmarks/results/report.md)

- [Unix 常用命令行工具集，使用 Nolang 语言实现。](https://github.com/lizongying/notools)

---

## TODO