---
sidebar_position: 2
---

# 安装与使用

## 安装

### 安装 CLI

从 [GitHub Releases](https://github.com/lizongying/nolang/releases/latest) 下载对应平台的可执行文件，或使用以下方式安装：

```bash
# macOS / Linux
# 1. 下载二进制文件
# 2. 放到 PATH 中
sudo mv nolang /usr/local/bin/no
```

### 安装 VS Code 插件

从 [VS Code Marketplace](https://marketplace.visualstudio.com/items?itemName=lizongying.vscode-nolang) 安装 Nolang 扩展，提供语法高亮、LSP 诊断、跳转定义、自动补全等支持。

## CLI 命令

| 命令                                                         | 说明                    |
| ------------------------------------------------------------ | ----------------------- |
| `no version`                                                 | 打印版本信息            |
| `no init`                                                    | 定义工作区（生成 workspace.noml，不含 package.noml） |
| `no new <name>`                                              | 在工作区内新建包（子目录 + package.noml，并注册到 workspace.noml） |
| `no fmt [-w] [-d] <file\|dir>`                               | 格式化源代码（含导入去重/排序，自动移除无别名 std 模组导入）；`.noml` 配置档走保留布局的排版规范（缩进/空白/空行，注释与键序不动） |
| `no build [-o <file>] [-cc <s>] [-target <s>] [-opt[=N]] [<file\|dir>]` | 构建（输出 executable） |
| `no run [-cc <s>] [-target <s>] [<package\|dir\|file>]`        | 构建并执行（包名/目录/文件） |
| `no test [-cc <s>] [-target <s>] [<file>]`                   | 执行测试                |
| `no add <tool>[:<version>]@<host>/<owner>/<repo>`            | 添加依赖（预编 AST 模组）|
| `no remove <pkg>`                                            | 移除依赖                |
| `no update <pkg>`                                            | 更新依赖                |
| `no update-all`                                              | 更新所有依赖            |
| `no list`                                                    | 列出依赖                |
| `no sync`                                                    | 同步依赖                |
| `no install [-u] [<tool>[:<version>]@<host>/<owner>/<repo>]`  | 安装 binary             |
| `no uninstall <name>`                                        | 移除 binary             |
| `no pack [-o <file>] [<dir\|file.no>]`                       | 打包 AST（`<tool>-ast.zip`）|
| `no pub [-o <file>]`                                         | 同 `no pack`（registry 上传未实现）|
| `no cache <stat\|clear>`                                     | 检视 / 清空 AST 快取（正常情况下不需要清）|

## 快速开始

Nolang 采用**单仓（项目）多包**架构：仓库根目录是工作区（只有 `workspace.noml`），每个包是一个子目录（有自己的 `package.noml`）。`no init` 与 `no new` 是两件独立的事：

- `no init` —— 在当前目录定义工作区（只生成 `workspace.noml`）。
- `no new <name>` —— 在当前工作区下新建一个包（生成子目录 `./<name>/` 及其 `package.noml`），并把该包注册进 `workspace.noml`。

### 初始化工作区并新建包

```bash
# 1) 在仓库根目录定义工作区
no init

# 2) 在工作区内新建包（会自动注册到 workspace.noml）
no new foo

# 3) 进入包目录
cd foo

# 直接运行（自动构建并执行 main.no）
no run
```

可在工作区内反复使用 `no new` 添加更多包：

```bash
no new bar
no new baz
```

此时 `workspace.noml` 形如：

```noml
{
  foo: '/foo'
  bar: '/bar'
  baz: '/baz'
}
```

> **路径格式**：`workspace.noml` 中的映射值使用 `/` 前缀表示工作区相对路径（如 `/foo` → `workspaceRoot/foo`）。也支援 `./`、`../` 相对跳转和作业系统绝对路径（如 `/home/user/code/fork`），允许指向工作区外部目录。

`no build` 在根目录无参数运行时，会并行构建工作区内所有包。

### 只初始化工作区（不创建包）

```bash
# 在当前目录定义工作区
no init
```

`no init` 只生成 `workspace.noml`（初始为空 `{}`），**不会生成 `package.noml` 或 `main.no`**。若 `workspace.noml` 已存在则不会被覆盖。包需要通过 `no new <name>` 创建。

### 构建与运行

```bash
# 构建（默认寻找 main.no）
no build                    # 构建当前目录
no build main.no            # 构建指定文件
no build -o output          # 指定输出路径
no build -cc zig            # 使用 Zig 编译器
no build -target x86_64-linux-gnu  # 交叉编译（指定目标平台）

# 运行（构建 + 执行）
no run                      # 构建并执行当前目录的 main.no
no run foo                  # 运行工作区中名为 foo 的包（解析 workspace.noml）
no run ./foo                # 运行 ./foo 目录的 main.no
no run main.no              # 运行指定 .no 文件
no run -cc zig
no run -target aarch64-macos-gnu
```

> `no build -v` 会在输出路径旁另外写出原始 LLVM IR（`<输出路径>.ll`）。`no build --shared -o libdemo.dylib main.no` 则会把标记 `#{cabi='symbol'}` 的顶层函数导出为稳定的 C ABI 符号，以 `-shared -fPIC` 链成动态库，并自动生成同名 `.h` 标头档；可用 `--header` 覆写标头路径。目前没有 `-c`（只编译不链接）选项。让 C / C++ / Rust / Go / Java / Python / C# 呼叫 Nolang 的完整做法见 [从宿主语言调用 Nolang](./lang/host-interop)。

`no run` 的位置参数按以下顺序解析：

1. 已存在的 `.no` 文件 → 直接运行该文件
2. 已存在的目录 → 运行其中的 `main.no`
3. 工作区（最近的 `workspace.noml`）中注册的包名 → 运行该包的 `main.no`

无参数时，默认运行当前目录的 `main.no`。若当前目录是工作区根（含 `workspace.noml` 但没有 `main.no`），会提示用 `no run <package>` 指定包。

### 交叉编译目标

`-target` 参数格式为 `<arch>-<os>-<abi>`，支持以下目标：

| 目标三元组           | 说明           |
| -------------------- | -------------- |
| `x86_64-linux-gnu`   | Linux x86_64   |
| `aarch64-linux-gnu`  | Linux ARM64    |
| `x86_64-macos-gnu`   | macOS x86_64   |
| `aarch64-macos-gnu`  | macOS ARM64    |
| `x86_64-windows-gnu` | Windows x86_64 |

**自动检测当前平台**：`no build`、`no run`、`no test` 在未指定 `-target` 时，会自动检测当前宿主平台并编译为本机代码。日常开发无需手动指定 target：

```bash
no run hello.no          # 本机直接跑
no test                  # 本机跑测试
no build -target aarch64-linux-gnu   # 需要交叉编译时才显式指定
```

### 编译器选择

`-cc` 参数指定 C 编译器后端：

- `clang`（预设）— 需要安装 LLVM
- `zig` — 需要安装 Zig，适合交叉编译

### 内建型别布局（`?T` 内联阈值）

`?T` 的记忆体布局**按载荷定型**：每个载荷型别 `T` 各自一个
`%option_<key> = { i8 tag, [K x i64] }`（1 位元组标签、载荷固定在位元组偏移 8、
`K = roundUp8(sizeof(T))/8`）。载荷放不进阈值就改成堆装箱、载荷区首字存指标。

`--option-payload-inline-threshold=N` 指定这个阈值（按**整个 option 结构**的位元组计，
即 `1 + 7 + sizeof(T)`）：

```bash
no build --option-payload-inline-threshold=1032 main.no   # 让更大的载荷内联
no build --option-payload-inline-threshold=16 main.no     # 最小可配置值
```

| 值 | 效果 |
| --- | --- |
| 预设 **264**（= 1 + 7 + 256） | `?i64` 16 B、`?str` 24 B、`?txt`（256 B）264 B 内联；更大的载荷装箱 |
| 最小 **16** | 只有 `?i64` 一类还内联；`?str`（24 B）起全部装箱 |
| < 16 | 编译错误 |

优先序：命令行 > 环境变量 `NOLANG_OPTION_PAYLOAD_INLINE_THRESHOLD` > `package.noml` 的
`compiler.option-payload-inline-threshold` > 预设值。布局的唯一来源是 `src/mir/layout.go`，
语义层面（哪些值算 `ok`／`nil`／`err`）**不受影响**，只有记忆体占用与是否装箱会变。

### 编译器内置优化器（MIR opt）

编译器自带一个作用在 **MIR 层**（HIR 降级之后、LLVM 代码生成之前）的优化器。它与后端已有的 LLVM `opt` 是两件事：前者改写 MIR，后者改写已生成的 LLVM IR。

用 `-opt[=N]` 开启，**预设关闭**：

```bash
no build main.no          # 预设：不启用（与历史行为一致）
no build -opt main.no     # 等同 -opt=1
no build -opt=2 main.no   # 常数折叠 + 死码消除 + 控制流清理
no build -opt=0 main.no   # 显式关闭
```

| 级别 | 含义 |
| ---- | ---- |
| `0` / `off` / `false` / `no` / `none` | 关闭（预设） |
| `1` / `on` / `true` / `yes` / `-opt` | 常数折叠 + 整数恒等式改写 + 复制传播 + 死纯量消除 |
| `2` | 级别 1 + 控制流清理（常数分支折叠、不可达块删除、空块跳转串接、单前驱块合并）+ 前导裁剪 |

`-opt` 是**全局**开关（与 `-v` 一样在 `main` 中处理），因此 `build`、`run`、`test` 都支持。

同一开关也可用环境变量 `NOLANG_MIR_OPT` 指定；命令行 flag 只是把它写进该变量，因此两者不会出现不一致。查看本次优化做了什么：

```bash
NOLANG_MIR_OPT=2 NOLANG_MIR_OPT_STATS=1 no build -o out main.no
# [miropt] level=2 funcs=3 folded=5 (arith=2 cmp=3) ident=1 copies=4 dead=6 cfg{condbr=3 unreachable=4(5 insts) threaded=1 merged=5}
# [miropt] prelude: dropped 27 unreferenced helpers
```

| 栏位 | 含义 |
| ---- | ---- |
| `folded`（`arith`/`cmp`） | 折叠成常数的算术／比较指令数 |
| `ident` | 化简为某个运算元的整数恒等式（`x+0`、`x*1`、`x&x`、`x^0`…） |
| `copies` | 被复制传播删除的纯量复制 |
| `dead` | 被删除的死纯量指令 |
| `cfg{condbr, unreachable, threaded, merged}` | 常数分支折叠、不可达块删除（连带指令数）、空块跳转串接、单前驱块合并 |
| `prelude: dropped N` | codegen 前导中被证明无人引用的 runtime helper 数量（见下） |

### 前导裁剪（级别 2）

`emitPrelude` 把整个 runtime（`str_*`、`print_*`、`eprint_*`、`vec_*`、`nolang_*`）写进**每一个**模组，不管程式有没有用到——一个 `print('hi')` 程式会产生 60 个函式定义，其中只有 8 个从入口可达，**52 个从未被呼叫**。

**这件事 `opt -O2` 做不到**：LLVM 的 GlobalDCE 只删 `internal` 定义，而前导是以 external linkage 发出的（`no` 模组可以跟 C 连结，连结器可能需要的 helper 不能是 internal）。在该程式上实测，60 个定义中恰好 7 个是 `define internal`，`opt -O2` 恰好只删掉这 7 个，而且 `.ll` 反而从 61,812 长到 69,874 bytes（+8,062）。

因此级别 2 会在 IR 生成完成后，对**已完成的文本**做一次可达性裁剪，只保留真正被引用到的 helper（连带递回保留它们自己呼叫的）。同一份 `print('hi')`：定义数 60 → **16**，`.ll` 61,812 → **14,548** bytes（**−76.5%**），执行档 36,616 → 34,648（−5.4%）。裁剪只认真正的引用——**注解里出现的名字不算**，这是它能删掉 44 个而不是 33 个的关键；而丢掉一个函式时，**它自己那段说明注解也一起丢**（`optTrailingComment`），否则会留下 12,609 bytes 在描述 44 个已经不存在的函式。

删注解**不会缩小执行档**——注解在 `opt`/`llc` 解析时就被丢弃，两者的执行档都是 34,648 bytes。那 12,609 bytes 是 IR 文本与编译期解析量的收益。

裁剪的近似一律偏向保留——无法解析出唯一名字的区块保留、区块没有闭合大括号就从该处起停止裁剪、`declare`／global／型别定义永不删。漏掉一个优化只是多几个位元组，删错一个就是连结错误。

**前导能不能整个移除？不能。** 前导区间里不只有函式，还有**程式自己的字串字面值**（`@.mir.str.N`，就是 `"hi"` 本身）、`%str-long`／`%option`／`%task` 等型别、`@malloc`／`@write`／`@llvm.memcpy` 等外部宣告——这些资料面占模组约 30%，砍掉连 `llc` 都过不了（`error: use of undefined value '@.mir.str.1'`）。能删的只有**函式**那一半：把 48 个 `define` 换成同名 `declare` 后，连结器报出的未定义符号恰好是 `print_nl`、`print_str`、`str_free`、`str_from_const` 四个。所以正确的问法是「能不能按需发射」，而不是「能不能移除」。

这笔浪费是一笔**与程式大小无关的固定税**：前导的程式码与注解逐模组相同（48 个定义、程式码 36,043 ＋ 注解 17,516 bytes），语料抽样 40 档的保留量是 **0–19 个、中位数 9**。`tests/dup.no`／`tests/q.no`／`tests/u128-types.no` 保留 **0 个**——整段前导对它们全是死的。因此节省比例完全由模组大小决定：一行程式 −76.5%，3.0 MB 的 `test/std/pbkdf2.no` 只有 −4.1%。

前导能不能整个移除？不能——区间里还有程式自己的字串字面值（`@.mir.str.N`）、型别与外部宣告（1,151 bytes），砍掉连 `llc` 都过不了。而且 `codegen.go:75-83` 的 `extraFuncs`／`extraFuncsBody` 本来就是一个按需发射器（option／struct／enum／ptr 的 helper 都走它），前导是唯一没被转换的部分；但把它改成按需，能多拿的几乎全是注解，程式码面已经没有东西可拿。

该优化器只折叠 **纯量**（`i64`/`u64`/`i16`/`i32`/`char`/`bool`/`f32`/`f64`）。`byte`/`u8`/`i8`、`i128`/`u128`、`option`、切片、结构体等一律跳过：前者在 `emitConst` 中没有对应分支（会退化成 0），后者的算术走 `option` 溢出包装路径，折叠会改变语义。浮点同样被排除在所有恒等式之外——`x * 0.0` 对 `inf`/`NaN` 不是 0，`x + 0.0` 对 `-0.0` 不是 `x`。

那份对比的核心结论值得在这里先讲：**局部清理（常数折叠、死码消除、控制流清理）在 `opt -O2` 跑过之后边际价值几乎为零**（−0.06%），因为 LLVM 把同一类工作做得更彻底。真正有回报的是 LLVM **因为 linkage 而不能做**的事——也就是上面的前导裁剪。加上前导裁剪（含注解感知裁剪）后，`mir+llvm_O2` → `mir_opt+llvm_O2` 的整体边际值是 **−19.05% 指令 / −15.83% 文本**；`mir` → `mir+mir_opt` 的文本降幅则由 −14.29% 变成 **−17.75%**（631 档全语料）。

## JS 后端

JS 后端（`no build --js` / `no run --js`）将 `.no` 原始码编译为 JavaScript（型别擦除），
支援 Node.js 与浏览器两种目标、`#{js}` / `#{js-browser}` 平台标注，以及第三方 ESM 导入
（`# npm/<pkg>`）。

JS 相关的完整文档（命令列参数、内建函数映射、`js/*` 标准库、浏览器 import map 等）
集中在独立的 [JS 后端](./lang/js-backend) 文档，本页不再重复。

## 入口规则

- **main.no** — 程序入口
- **lib.no** — 库入口，导出函数（详见[导出文档](lang/export)）
- **tests/ 目录下所有 .no 文件** — 包含测试断言

## 测试

```bash
# 测试tests目录下所有 .no 文件
no test

# 执行单个测试文件
no test my-test.no

# 使用指定编译器或目标
no test -cc zig
no test -target x86_64-windows-gnu
```

测试说明：

- 测试文件统一放在 tests/ 目录下
- 每个测试文件独立构建
- 若任一测试失败，返回非零退出码

## AST 快取

`no build` / `no run` / `no test` 会把每个用户源文件解析出的 AST 落到磁碟
（预设 `~/.cache/nolang/ast`，可用 `$NOLANG_AST_CACHE_DIR` 改路径），下次内容没变就直接复用，
省掉 lex + parse。标准库走的是编译器内嵌的 blob，不占这份快取。

**不需要手动清理。** 快取键是

```
sha256( ASTVersion ‖ 解析期 salt ‖ 源文件内容 )
```

其中 salt 的一项就是 `checker.StdSigKey` —— 即 `src/hash.noml` 所列全部源文件
（标准库 + 编译器自身）的内容签名。所以编译器或标准库的任何改动，都会让用 `make` 重建出的
`no` 换一把新键，旧条目自然 miss、不会被覆用。

| 命令 | 说明 |
| --- | --- |
| `no cache stat` | 显示快取目录、条目数、总大小与目前 `ASTVersion` |
| `no cache clear` | 清空全部条目（仅应急：怀疑条目陈旧时） |

环境变量：

| 变量 | 作用 |
| --- | --- |
| `NOLANG_AST_CACHE_OFF=1` | 关闭快取（做编译器 A/B 对比、排查「修好的错误又出现」时可用） |
| `NOLANG_AST_CACHE_DIR` | 自订快取目录。**同时**作用于预编 AST 依赖的 `-ast.zip` 快取（该快取预设在 `~/no/cache/ast`，见下文「预编 AST 依赖」） |
| `NOLANG_AST_CACHE_MAX` | 快取条目数上限（预设 4096） |

> 旧条目只是不再被命中，会随新条目写入按上限淘汰，因此**不必**删 `~/.cache/nolang/ast`。
> 注意 `no` 必须用 `make gen` / `make no` 重建：用 `cd src && go build` 自行编译出来的二进位
> 没有注入 `StdSigKey`，它会直接报错退出而不是静默复用快取。

## 安装与卸载 Binary

### 安装

```bash
# 安装当前目录的包（本地构建）
no install
no install -u                                   # 强制重构（更新）

# 从 GitHub Release 安装预编 binary（推荐）：<tool>[:<version>]@<host>/<owner>/<repo>
no install nogit@github.com/lizongying/notools            # 最新版
no install nogit:v0.1.33@github.com/lizongying/notools    # 指定版本
no install nogit@lizongying/notools                       # 省略 host，默认 github.com
no install -u nogit@github.com/lizongying/notools         # 强制重新下载
```

远端安装抓的是**预编二进位**，不是源码：资产名为 `<tool>-<os>-<arch>-<version>`（Windows 带
`.exe`），其中 `<os>-<arch>` 取自 `no` 自身运行的平台（如 `darwin-arm64`、
`linux-amd64`），因此不需要本地 LLVM 工具链，被安装的工具也不必是 Nolang 写的。
版本省略时查 `releases/latest`；设 `$GITHUB_TOKEN`（或 `$GH_TOKEN`）可避免 GitHub
API 限流。

安装流程：

1. 远端：解析 spec → 定位 release 资产 → 下载；本地：使用当前目录构建
2. 将 binary 写入 `~/no/bin/`
3. 在 `/usr/local/bin/` 建立软链接

### 卸载

```bash
no uninstall pkg-name
```

卸载会移除 `/usr/local/bin/` 中的软链接和 `~/no/bin/` 中的 binary。

## 项目配置

### noml 配置格式

Nolang 的所有配置档（`package.noml`、`workspace.noml`、`.workspace.noml`、`nolang.lock.noml`、`sum.noml`）采用自研的 **noml**（Nolang Markup Language）格式，取代过去的 JSONC。语法规则与 Nolang 语言本身对齐：

| 规则 | 说明 |
| --- | --- |
| 顶层物件 | 是一个物件：`{` 与 `}` 各独占一行，成员一行一个，**无逗号分隔**（换行即分隔） |
| 键 | 默认裸识别符，规则同 Nolang 变数名：小写字母/数字/中划线（中间）/下划线开头，不能以数字开头（如 `link-libs`、`anonymous-fn-type`、`dev-dependencies`）；不满足规则的键（路径、URL、`@scope/pkg`）写成单引号字串键 |
| 字串值 | 单引号，`\` 转义：`\' \\ \n \t` |
| 布林 | `true` / `false`（全小写，同 Nolang bool 字面量） |
| 数字 | 裸字面量（保留给锁档等） |
| 阵列 | `[`、元素、`,`、`]` 均可跨行；元素用**逗号**分隔，**支援尾随逗号**；空阵列写 `[]` |
| 注解 | `;` 到行尾（字串内除外） |
| 缩排 | 无意义（仅影响可读性，不做格式化保证） |

`dependencies` / `dev-dependencies` 是**字串阵列**，元素形如 `'键:版本'`（版本可省略，默认 `*`），例如：

```noml
{
  dependencies: [
    '/example/test2',                          ; 本地包，默认任意版本
    'github.com/lizongying/nolang/test1:v0.1.0', ; 线上包，指定版本,
  ]
}
```

项目根目录下的 `package.noml` 文件描述项目信息：

```noml
{
  name: 'my-project'
  version: 'v0.1.0'
  description: 'A new Nolang project'
  keywords: []
  author: ''
  email: ''
  organization: ''
  repository: ''
  homepage: ''
  license: 'MIT'
  dependencies: [
    'fmt',,
  ]
  compiler: {
    version: 'v0.1.0'
  }
  ignore: []
}
```

`compiler.lang` 把本专案的消息语言固定下来（`'zh-tw'`、`'ja'` 等，写法同 `--lang`）：它优先于系统 locale，
但低于 `--lang` 与 `NO_LANG`；不写则跟随系统。

### 依赖管理

```bash
# 添加依赖：<tool>[:<version>]@<host>/<owner>/<repo>
# 版本号可省略（取最新 release）；写入 package.noml 的键是 <tool>@<host>/<owner>/<repo>
no add nogit@github.com/lizongying/notools
no add nogit:v0.1.33@github.com/lizongying/notools

# 移除依赖
no remove pkg-name

# 更新依赖
no update pkg-name

# 更新所有依赖
no update-all

# 列出依赖
no list

# 同步依赖（下载并生成锁文件）
no sync
```

### 预编 AST 依赖

`no add <tool>@<host>/<owner>/<repo>` 这类依赖**只有 AST、没有源码**：编译时从 release 抓
`<tool>-ast.zip`（zip，每个模组一个 .gob 条目），解码后直接注入编译管线。用法：

```nolang
# nogit                         ; 包别名，模组以 nogit.<module> 取用
# nogit/remote                  ; 直接引入模组 remote（也可用 nogit.remote）
# nogit.fn                      ; nogit 只是包名，按 fn 在包内定位模组
# <host>/<owner>/<repo>/nogit/remote
```

一个 zip 就是一个包，包内可含多个模组，每个模组是一个 `<modulePath>.gob` 条目；没有入口模组：`# nogit` 只建立包别名，模组以 `nogit.<module>` 取用（如 `nogit.remote.greet(...)`）；`# nogit/remote` 直接取模组，`# nogit.fn` 按函式定位模组。`no fmt` 会清理正文未使用的 `# nogit` / `# nogit/remote`。版本必须是具体值
（`no sync` / 编译时遇到 `""` 或 `"*"` 会报错）。zip 会快取到 `~/no/cache/ast/`，
可用 `$NOLANG_AST_CACHE_DIR` 覆写。

产出资产用 `no pack`（只需 lexer + parser，不需要 LLVM 工具链，适合 CI）：

```bash
no pack -o nogit-ast.zip nogit.no      # 单个源文件
no pack -o nogit-ast.zip ./nogit       # 一个包目录
```

### 依赖类型与版本号规则

`package.noml` 中的依赖分为**本地包**和**线上包**两类。编译器会根据以下规则自动判定依赖类型，并在本地包未使用 `"*"` 版本时发出警告。

#### 判定规则

```
依赖键 → 查找 workspace.noml（短名称或完整键匹配）
  ├─ 找到 → 本地包（应使用 "*"）
  └─ 未找到 → 检查路径前缀（/ 开头）
      ├─ 是 → 本地包（应使用 "*"）
      └─ 否 → 线上包（使用版本号，不警告）
```

1. **查找 `workspace.noml`**：先以依赖键作为短名称或完整键在 `workspace.noml` 中查找。找到即为本地包。
2. **检查路径前缀**：未在 `workspace.noml` 中找到时，若依赖键以 `/`（工作区相对路径）开头，亦为本地包。
3. **其余为线上包**：不满足上述条件的依赖键视为线上包，应指定版本号。

> **工作区边界约束**：`package.noml` 中的本地路径只能以 `/` 开头（工作区相对），禁止 `./`、`../` 和作业系统绝对路径。唯一允许突破工作区边界的入口是 `workspace.noml` / `.workspace.noml` 映射。详见 [工作区边界约束](#工作区边界约束)。

#### 本地包的多种引用形式

本地包支持以下三种引用方式，版本号均应使用 `"*"`：

```noml
dependencies: [
  ; 1. 短名称：workspace.noml 中注册的键
  'test2',
  ; 2. 工作区相对路径：以 / 开头，相对工作区根目录
  '/example/test2',
  ; 3. 完整 URL：若 workspace.noml 中有对应映射则为本地包
  'github.com/lizongying/nolang/test2',,
]
```

> **注意**：
> - 第 3 种形式是否为本地包取决于 `workspace.noml` 中是否存在对应映射。若 `workspace.noml` 中将 `github.com/lizongying/nolang/test2` 映射到本地路径，则该依赖为本地包（应使用 `"*"`）；否则为线上包（应使用版本号）。
> - `./` 和 `../` 前缀已不再允许在 `package.noml` 中使用。所有本地路径必须以 `/` 开头（工作区相对）。

#### 高级用法：线上包转本地包

`workspace.noml` 不仅可以注册短名称，还可以将一个线上包名映射到本地路径，实现「线上包转本地包」的开发模式：

```noml
; workspace.noml
{
  test2: '/example/test2'
  'github.com/lizongying/nolang/test2': '/example/test2'
}
```

此时 `package.noml` 中以 `github.com/lizongying/nolang/test2` 引用的依赖会被解析为本地包 `/example/test2`，版本号应使用 `"*"`。这在本地开发线上包时非常方便——只需在 `workspace.noml` 中添加映射，即可将远端依赖切换为本地源码进行联调。

#### 版本号警告

编译时若检测到本地包使用了非 `"*"` 的版本号（如 `"v0.1.0"`），会输出警告提示使用 `"*"`。线上包则不受此限制。

#### 递回工作区映射（跨包映射链）

Nolang 支援**递回工作区映射**：被依赖的包可以自带 `workspace.noml`，内部继续存在映射规则，形成自然的跨包解析链。这是 Nolang 的核心差异化能力——主流 Go/Cargo 的 `replace`/`patch` 仅生效于当前项目，不会传递到依赖包内部。

**适用场景：**
- 基础库统一内部别名
- 标准化导入路径
- 多层库兼容迁移

**工作原理：**

当解析依赖键时，编译器会：
1. 在当前 `workspace.noml` 中查找键
2. 若找到映射，检查目标目录是否自带 `workspace.noml`
3. 若有，继续在同一键的基础上查找，逐层解析
4. 直到没有更深的映射为止

**范例：**

```
workspace/                  ← 顶层工作区
├── workspace.noml         ← {"testkey": "/pkgA"}
├── pkgA/
│   ├── workspace.noml     ← {"testkey": "/pkgB"}
│   └── pkgB/
│       ├── workspace.noml ← {"testkey": "/pkgC"}
│       └── pkgC/           ← 最终目标（无 workspace.noml）
```

解析 `testkey` 时：
1. 顶层 `workspace.noml` 映射 → `/pkgA`（工作区相对）
2. `pkgA/workspace.noml` 映射 → `/pkgB`（pkgA 工作区相对）
3. `pkgB/workspace.noml` 映射 → `/pkgC`（pkgB 工作区相对）
4. `pkgC` 无 `workspace.noml` → 最终解析到 `pkgC`

**循环检测：**

编译器维护一条解析访问栈，跟踪已访问的工作区根目录。一旦检测到循环映射（如 A→B, B→A），立刻报错并输出完整链路：

```
Error: circular workspace mapping detected: /path/to/A → /path/to/B → /path/to/A
```

> **注意**：此能力仅支援跨包的天然递回映射（被依赖包自带 `workspace.noml`），不支援单文件手动链式简写。

### 镜像配置

在 `package.noml` 的 `mirrors` 数组中配置镜像地址，用于加速远端包下载：

```noml
mirrors: [
  'https://mirror.example.com/',,
]
```

### 工作区配置 workspace.noml

`workspace.noml` 位于仓库根目录，描述一个**单仓多包**工作区，是一个「包名 → 相对路径」的映射。`no build` 在无参数且存在 `workspace.noml` 时，会并行编译其中列出的所有包；`no run` 则透过包名（如 `no run foo`）执行工作区中的单个包。

```noml
{
  foo: '/foo'
  bar: '/bar'
}
```

`no init` 负责定义工作区：若 `workspace.noml` 不存在则生成一个空对象 `{}`，**不生成任何 `package.noml`**；已存在时则保持不变，不会被覆盖。随后用 `no new <name>` 创建包时，会自动把 `"<name>": "./<name>"` 注册进 `workspace.noml`。

#### 本机私有配置 .workspace.noml

除公共的 `workspace.noml` 外，Nolang 还支援本机私有配置档案 `.workspace.noml`（注意前导 `.`）。该档案建议加入 `.gitignore`，用于开发者个人的临时依赖替换。

**加载逻辑：**

1. 先加载公共配置 `workspace.noml`
2. 再加载私有配置 `.workspace.noml`
3. **相同 key**：私有配置覆盖公共配置
4. **新 key**：相互叠加

```
workspace/
├── workspace.noml       ← 工程共享配置（纳入版本管理）
├── .workspace.noml      ← 本机私有配置（建议加入 .gitignore）
├── foo/
└── bar/
```

**使用场景：**

- 团队统一别名、依赖转发规则写在 `workspace.noml` 中，纳入版本管理
- 开发者本地调试时需要临时将某个依赖指向本地 fork，在 `.workspace.noml` 中覆盖即可
- 避免临时 fork 映射污染项目公共配置，兼顾大型团队协作与开发者本地开发灵活性

**范例：**

```noml
; workspace.noml（公共，版本管理）
{
  foo: '/foo'
  bar: '/bar'
}

; .workspace.noml（私有，本地调试）
{
  ; 覆盖公共配置：foo 指向本地 fork（工作区内）
  foo: '/my-fork-foo'
  ; 新增私有映射：指向工作区外部的 fork（OS 绝对路径，唯一逃逸出口）
  'github.com/lizongying/nolang/core': '/home/lzy/code/fork/core'
  ; 新增私有映射（工作区内）
  experimental: '/experimental'
}
```

最终生效的映射：`foo` → `/my-fork-foo`（私有覆盖），`bar` → `/bar`（公共保留），`github.com/lizongying/nolang/core` → `/home/lzy/code/fork/core`（私有新增，逃逸工作区），`experimental` → `/experimental`（私有新增）。

### 工作区边界约束

Nolang 对路径引用实施严格的工作区边界控制，确保项目的可移植性和安全性。

#### 规则总览

| 配置层级 | `/` 工作区相对 | OS 绝对路径 | `./` `../` 相对跳转 |
| --- | --- | --- | --- |
| `package.noml` 依赖键 | ✅ 允许 | ❌ 禁止 | ❌ 禁止 |
| `workspace.noml` 映射值 | ✅ 允许 | ✅ 允许（逃逸出口） | ✅ 允许（逃逸出口） |
| `.workspace.noml` 映射值 | ✅ 允许 | ✅ 允许（逃逸出口） | ✅ 允许（逃逸出口） |
| 源码 `#` 导入路径 | ✅ 允许 | ❌ 禁止 | ❌ 禁止 |

#### 1. package.noml（严格受限，禁止逃逸工作区）

`package.noml` 是项目对外发布、CI、多人协作的标准基线配置，提交到代码仓库。

```noml
#{fmt-skip}
dependencies: [
  ; ✅ 合法：以 / 开头，相对于 workspace 根
  '/vendor/test',
  ; ❌ 禁止：作业系统绝对路径 /home/xxx/...
  ; ❌ 禁止：./ ../ 相对跳转
]
```
```

**约束：**
- 本地路径只能写 `/xxx`，基准 = workspace 根目录
- 不接受作业系统绝对路径
- 不支援 `./`、`../`
- 无法直接引用工作仓库外部任何目录

**目的：** 如果允许外部路径，极易出现「本机能编译，别人编译失败」，也存在意外加载外部陌生代码的安全隐患。

#### 2. workspace.noml + .workspace.noml（唯一逃逸出口）

映射目标支援三种本地路径：
- `/xxx` → workspace 根内部目录（和 package.noml 语义一致）
- `./xxx`、`../xxx` → 相对于 workspace 根目录解析，允许逃逸工作区边界
- 作业系统绝对路径 → 允许指向工作区外部（fork、外部组件）

```noml
; .workspace.noml 私有配置，用于本地开发
{
  ; 工作区内部
  foo: '/vendor/foo'
  ; 相对跳转：指向工作区外部的 fork
  'github.com/lizongying/nolang/core': '../fork/core'
  ; OS 绝对路径：指向任意外部目录
  'github.com/lizongying/nolang/utils': '/home/lzy/code/utils'
}
```

**关键逻辑：** `package.noml` 没有能力直接引用外部代码；用户想要挂载仓库外源码，必须显式在 workspace 配置映射。相当于给「跨目录加载」增加一道明确、可见的开关，所有外部依赖集中一处管控。

#### 3. 源码导入层面配套约束

源码内不允许直接书写作业系统绝对路径导入；所有外部代码引用，只能通过 workspace 别名或远端包标识间接引入。`./` 和 `../` 相对跳转同样被禁止。

```no
; ✅ 合法：工作区相对路径
# /vendor/utils.helper
; ✅ 合法：远端包标识
# github.com/lizongying/nolang/core.process
; ✅ 合法：标准库
# std/fs
```

```no
#{fmt-skip}
; ❌ 禁止：相对跳转
; # ./utils.helper
; # ../lib/helper.process

; ❌ 禁止：作业系统绝对路径
; # /home/user/code/core.process
```

#### 数据流链路演示

场景：业务项目 workspace 根在 `/code/project`，想要使用外部 `/code/fork/core`

❌ 不能这么干（package.noml 禁止）：

```noml
; package.noml
dependencies: [
  '/code/fork/core', ; 非法：无法逃逸工作区,
]
```

✅ 标准做法（通过 workspace 映射突破边界）：

```noml
; .workspace.noml
{
  'github.com/lizongying/nolang/core': '/code/fork/core'
}
```

```no
; 源码正常导入
# github.com/lizongying/nolang/core
```

#### 工作区流程

编译和执行的入口目录始终是**工作区目录**（`workspace.noml` 所在目录）。整体流程如下：

1. 用户在工作区目录中执行 `no build` 或 `no run <包名>`
2. 编译器读取 `workspace.noml`，按包名查找对应的子目录路径
3. 载入该子目录中的 `package.noml`（即包根目录），以包根目录为基准进行构建/运行
4. 所有导入路径（`use /path/to/module`）**相对于工作区根目录解析**（见[路径解析约定](path-resolution.md)），即以 `workspace.noml` 所在目录为唯一基准，不再依赖导入方所在的包。

```
workspace/               ← 工作区目录（workspace.noml 所在此处）
├── workspace.noml      ← 包名 → 路径映射
├── foo/                 ← 包 foo
│   ├── package.noml        ← foo 的包根目录
│   ├── main.no
│   └── lib.no
└── bar/                 ← 包 bar
    ├── package.noml        ← bar 的包根目录
    └── main.no
```

> **注意**：导入路径以**工作区根目录**（`workspace.noml` 所在目录）为准，而非包的 `package.noml` 目录。包子目录中若存在嵌套的 `package.noml`，`LoadPackage` 会向上搜索找到最近的 `package.noml` 并将其作为包根目录，但导入解析统一以工作区根为基准。

---

## 应用案例

- [notools](https://github.com/lizongying/notools) — 使用 Nolang 语言实现的 Unix 常用命令行工具集，涵盖 cat、ls、grep、wc、head、tail 等经典工具，展示了 Nolang 在系统编程领域的实际应用能力。
