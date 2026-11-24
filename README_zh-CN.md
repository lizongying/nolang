# Nolang

[繁體中文](README.md) · [简体中文](README_zh-CN.md) · [한국어](README_ko.md) · [日本語](README_ja.md) · [English](README_en.md) · [Русский](README_ru.md) · [Français](README_fr.md) · [Español](README_es.md) · [Deutsch](README_de.md) · [Português](README_pt.md)

Nolang 是一种内存安全、性能强劲、语法极简的系统编程语言。

## 核心特性

- **内存安全**：自动内存管理
- **性能强劲**：编译期优化，无 GC
- **语法极简**：语法更简单
- **意图驱动**：充分尊重开发者意图，语义符合人类直觉
- **永不崩溃**：消除隐式崩溃路径

## 安装

一键安装（macOS / Linux / Windows Git Bash），会自动下载最新的 `no` 到 `$HOME/.local/bin`：

```bash
curl -fsSL https://raw.githubusercontent.com/lizongying/nolang/main/install.sh | sh
```

默认装**内置 LLVM** 的版本（`no-llvm_*`，不必另装 LLVM / opt / llc）。若想要体积较小的纯 Go 版，用 `NO_FLAVOR=plain` 并搭配 `no build -cc zig`：

```bash
curl -fsSL https://raw.githubusercontent.com/lizongying/nolang/main/install.sh | NO_FLAVOR=plain sh
```

需要固定版本或自定义目录时（环境变量要传给 `sh`，所以先存档再执行）：

```bash
curl -fsSL https://raw.githubusercontent.com/lizongying/nolang/main/install.sh -o install.sh
NO_VERSION=0.4.28 NO_INSTALL_DIR="$HOME/bin" sh install.sh
```

| 环境变量          | 默认值               | 说明                                                |
| ----------------- | -------------------- | --------------------------------------------------- |
| `NO_VERSION`      | `latest`             | 版本号，可写 `0.4.28` 或 `v0.4.28`                 |
| `NO_FLAVOR`       | `embedded`           | `embedded` 内置 LLVM；`plain` 纯 Go（需系统 LLVM） |
| `NO_INSTALL_DIR`  | `$HOME/.local/bin`   | 安装目录                                            |
| `NO_REPO`         | `lizongying/nolang`  | 来源仓库                                            |

安装后执行 `no version` 验证。若 `$HOME/.local/bin` 不在 `PATH`，请自行加入：

```bash
export PATH="$HOME/.local/bin:$PATH"
```

> 编译 `.no` 程序还需要一个 C 链接器：推荐 `no build -cc zig`（zig 自带 clang / lld / libc），或自行安装 clang。

## 用法

文档

[docs](https://lizongying.github.io/nolang/)

VSCode 插件

[vscode](https://marketplace.visualstudio.com/items?itemName=lizongying.vscode-nolang)

Nolang CLI

[nolang](https://github.com/lizongying/nolang/releases/latest)

nohub 包清单（社区包搜索）

[nohub](https://lizongying.github.io/nohub/)

### 社区包搜索（nohub）

`no` 直接读取 nohub 的包索引，提供三个只读命令：

```bash
no search <query>    # 按包名、关键词、分类搜索社区包
no info <pkg>        # 显示包元数据与安装/导入命令
no docs <pkg>        # 打印包文档地址（docs → homepage → repository）
```

默认读取公共索引。要叠加多个 nohub 实例，在 `~/no/sources.noml` 配置源列表（数组顺序即优先级）：

```
{
  sources: [
    'https://lizongying.github.io/nohub',
  ]
}
```

`NOHUB_SOURCES`（逗号分隔）可临时覆盖。三个命令都接受 `-source <name>` 只看指定源；`no search` 的输出含 `SOURCE` 列，同名同版本由靠前的源优先。单个源拉取失败只会告警，不影响其他源。`no` 只 fetch 每个源的 `index.noml`（`<地址>/index.noml`，与搜索页首屏同一份数据，每包一条最新版本），不下载 tarball、不读取 `data.js`，详情字段映射与 `web/app.js` 一致。也可在 [nohub 搜索页](https://lizongying.github.io/nohub/) 用网页浏览。


### CLI 命令

| 命令                                                         | 说明                                     |
| ------------------------------------------------------------ | ---------------------------------------- |
| `no init`                                                    | 定义工作区（生成 workspace.jsonc，不含 package.jsonc） |
| `no new <name>`                                              | 在工作区内新建包（子目录 + package.jsonc，并注册到 workspace.jsonc） |
| `no fmt [-w] [-d] <file\|dir>`                               | 格式化源代码                             |
| `no build [-o <file>] [-cc <s>] [-target <s>] [<file\|dir>]` | 构建（输出可执行文件）                    |
| `no run [-cc <s>] [-target <s>] [<package\|dir\|file>]`      | 构建并执行（包名/目录/文件）             |
| `no test [-cc <s>] [-target <s>] [<file\|dir>]`              | 执行测试                                 |
| `no add <pkg>`                                               | 添加依赖                                 |
| `no remove <pkg>`                                            | 移除依赖                                 |
| `no update <pkg>`                                            | 更新依赖                                 |
| `no update-all`                                              | 更新所有依赖                             |
| `no list`                                                    | 列出依赖                                 |
| `no search <query>`                                            | 按包名、关键词、分类搜索社区包 |
| `no info <pkg>`                                                | 显示包元数据与安装/导入命令 |
| `no docs <pkg>`                                                | 打印包文档地址 |
| `no sync`                                                    | 同步依赖                                 |
| `no install [-u] [<tool>[:<version>]@<host>/<owner>/<repo>]` | 安装二进制文件（~/no/bin/ + 软链接）     |
| `no uninstall <name>`                                        | 移除二进制文件及软链接                   |
| `no pub --token <token> [--registry <url>]`                  | 发布至 registry                          |

```bash
# 构建（默认从 main.no 开始）
no build                    # 构建当前目录
no build main.no            # 构建指定文件
no build -o output          # 指定输出路径
no build -cc zig            # 使用 Zig 编译器
no build -target x86_64-linux-gnu  # 交叉编译（指定目标平台）

# 运行（构建 + 执行）
no run                    # 构建并执行当前目录的 main.no
no run foo                # 运行工作区中名为 foo 的包
no run ./foo              # 运行 ./foo 目录的 main.no
no run -cc zig
no run -target aarch64-macos-gnu

# 测试（构建 + 执行）
no test                   # 测试 `test` 目录下所有 .no 文件
no test test/my-test.no        # 执行单个测试文件
no test -cc zig
no test -target x86_64-windows-gnu
```

### 创建新项目

```shell
no new test1

cd test1

no run
```

### Hello World

新建项目后，编辑 `main.no`：

```no
print('Hello, World!')
```

运行：

```bash
no run
```

输出：

```
Hello, World!
```

### 入口规则

- **main.no** — 程序入口
- **lib.no** — 库入口，导出函数
- **tests/ 目录下所有 .no 文件** — 包含测试断言

### 测试说明

- 测试文件统一放在 tests/ 目录下
- 每个测试文件独立构建
- 若任一测试失败，返回非零退出码

```bash
no vet src/std                     # Nolang vet 检查标准库
```

## 从源码构建

构建 Nolang 应使用 `make`：

```bash
make            # 构建所有目标（bin/no 和 vscode-nolang/server/lsp）
make no         # 构建 bin/no
make lsp        # 构建 LSP server
make package    # 构建 LSP 并打包 VSCode 扩展
make no-wasm    # 转译 no 为 WebAssembly
make lsp-wasm   # 转译 LSP 为 WebAssembly
make playground # 构建 no.wasm + lsp.wasm + Docusaurus 站点
make clean      # 清理构建产物
make help       # 查看所有构建目标
```

> ⚠️ **构建 Nolang 必须使用 `make`，切勿用 `cd src && go build` 直接构建。**
>
> `make` 在构建前会自动执行 `make gen`，重新生成 `src/checker/stdsig_gen.go`
> —— 一份**内嵌进二进制**的标准库签名表（`funcSigs` / `methodSigs` /
> `funcParams` / `methodParams` / `structFields` / `aliases` / `structMod` /
> `enumVariants` / `embeddedStdModules`）。有了它，每次 `no build` 都跳过「全量解析所有 std」的
> PASS1，启动明显更快。
>
> 如果你修改过 `src/std` 却**没有**先 `make gen` 就直接 `go build`，编译器会
> 检测到内嵌签名表与 `src/std` 内容不一致，并**直接报错拒绝构建**（提示你执行
> `make no`）。这是刻意设计：过去静默退化为全量解析会让每次编译都变慢且不易
> 察觉。修改 `src/std` 后请一律执行 `make no`（它会先 `make gen` 再构建）。
>
> 执行测试也请用 `make test`：**直接 `go test` 会因未注入签名 key（`StdSigKey` 为空）而报错拒绝**。`make test` 在构建测试二进制时注入同样的 key，使其走内嵌签名表快路径。

---

## 词条库（glossary）

`glossary/` 是一份**词条化的 Nolang 知识库**：每个名词一条记录，带类型、定义、语法、
示例、关联引用与一句话摘要。共 **244 条 / 10 个语义域**（类型、语法、注解、内存、
错误、模块、编译器内部、工具链、std 全部模块、已知坑），数据本身是 `.noml`。

前端用 Nolang 写成，同时提供 CLI 与 MCP（stdio / HTTP），人和 AI 都能查：

```bash
no build glossary                        # 产物：glossary/dist/glossary

./glossary/dist/glossary get option      # 取单条词条全文
./glossary/dist/glossary search 三态      # 关键词检索
./glossary/dist/glossary list --domain=traps   # 已知坑一览
./glossary/dist/glossary check           # 一致性体检（CI 用）

./glossary/dist/glossary mcp             # MCP server（stdio）
./glossary/dist/glossary mcp --http=127.0.0.1:8765
```

输出可切 `--format=text|md|json`。接入与数据格式说明见 [`glossary/README.md`](glossary/README.md)。

也可在 nohub 线上查看 glossary 包：[glossary on nohub](https://lizongying.github.io/nohub/?package=glossary:latest@github.com/lizongying/nolang)

---

## 相关项目

- [nohub — Nolang 生态的社区包清单搜索](https://lizongying.github.io/nohub/)

  按包名、关键词、分类检索收录的社区包，查看包信息、版本快照与源码入口；索引由 `web/build.mjs` 从 `packages/` 快照生成。

- [noui — 用 Nolang 重写 GPUI 精神内核的 UI 工具箱（nohub 包）](https://lizongying.github.io/nohub/?package=noui:latest@github.com/lizongying/nolang)

- [Benchmarks Game — Nolang vs C vs Rust 性能对比报告](https://github.com/lizongying/no-benchmarks/results/report.md)

- [notools — 使用 Nolang 实现的 Unix 命令行工具集](https://github.com/lizongying/notools)

  Nolang 应用案例：使用 Nolang 语言实现的 Unix 常用命令行工具集，涵盖 cat、ls、grep、wc、head、tail 等经典工具，展示了 Nolang 在系统编程领域的实际应用能力。

---

## TODO

## 许可证

Apache-2.0
