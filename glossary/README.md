# glossary — Nolang 词条化知识库

面向人和 AI 的 **Nolang 名词解释库**：每个名词独立一条记录，带类型、定义、语法、
示例、关联引用与一句话摘要。数据是 `.noml`，前端是一个不依赖第三方库的
CLI + MCP 服务器（本仓库用 Nolang 自身写成）。

当前规模：**244 条词条 / 10 个语义域**，覆盖语言类型、语法构造、注解、内存模型、
错误处理、模块系统、编译器内部、工具链、std 全部模块，以及一批实测踩过的坑。

---

## 快速开始

```bash
# 在仓库根目录构建（产物落在 glossary/dist/glossary）
no build glossary

# 或者只重建编译器、再构建本包
make no && ./bin/no build glossary

./glossary/dist/glossary version
```

`glossary` 默认**向上逐级探测** `glossary/data/index.noml` 来定位词库，
所以从仓库任意子目录运行都能工作；也可以用 `--data=<dir>` 或环境变量
`GLOSSARY_DATA` 显式指定。

```bash
./glossary/dist/glossary get option
./glossary/dist/glossary search 三态
./glossary/dist/glossary related traps --depth=2
./glossary/dist/glossary list --domain=traps
./glossary/dist/glossary domains
./glossary/dist/glossary check
```

---

## CLI

| 子命令 | 说明 |
| --- | --- |
| `get <id>` | 取单条词条全文 |
| `search <关键词>` | 关键词检索（大小写不敏感），默认 10 条 |
| `related <id>` | 沿 `related` 引用展开，1–3 层 |
| `list [--domain=X] [--kind=X] [--status=X]` | 列出词条 |
| `domains` | 列出语义域与条数 |
| `check` | 一致性体检（CI 用） |
| `mcp` | 启动 MCP 服务器（stdio） |
| `mcp --http=<host:port>` | 启动 MCP 服务器（HTTP） |
| `version` | 版本与数据统计 |
| `help` | 用法 |

选项：

| 选项 | 说明 |
| --- | --- |
| `--data=<dir>` | 词库数据目录（默认自动探测） |
| `--format=text\|md\|json` | 输出形态，默认 `text` |
| `--limit=N` | `search` 条数上限 |
| `--depth=N` | `related` 展开层数（1–3，默认 1） |

三种输出形态都实现了：`text` 供人读、`md` 可直接拼进文档、`json` 供程序消费。
空结果在 `text`/`md` 给一句人话（并区分「检索无匹配」「无关联词条」「该域为空」），
在 `json` 恒为 `[]`。

退出码：`0` 成功；`1` 数据缺失 / 词条未找到 / `check` 有 error；`2` 用法错误或未知子命令。

---

## MCP 接入

四个工具：`glossary_get` / `glossary_search` / `glossary_list` / `glossary_related`。
`glossary_get` 的工具描述里明确写了「查任何 Nolang 名词先用它」，便于模型自选。

### stdio（推荐）

```json
{
  "mcpServers": {
    "nolang-glossary": {
      "command": "/绝对路径/glossary/dist/glossary",
      "args": ["mcp", "--data=/绝对路径/glossary/data"]
    }
  }
}
```

### HTTP

```bash
./glossary/dist/glossary mcp --http=127.0.0.1:8765
```

- 协议版本 `2025-06-18`，实现 Streamable HTTP 的子集：
  `POST /mcp` → `application/json`（单次响应，不开 SSE）；
  `GET` → `405`；`OPTIONS` → `204`。
- 绑定失败返回 1，成功监听时在 **stderr** 打一行 `listening on ...`（stdout 保持纯净）。

传输细节：stdio 用换行分帧，单行上限 8 MiB；通知（无 `id`）静默处理，
HTTP 侧回 `202` 空体；`id` 原样回显（支持数字、字符串、`null`）。

---

## 数据组织

```
glossary/data/
  index.noml        域清单（数组顺序 = CLI/渲染顺序）
  types.noml        类型系统            21
  syntax.noml       语法                22
  annotations.noml  注解                 5
  memory.noml       内存管理            12
  errors.noml       错误处理            10
  modules.noml      模块与符号           7
  compiler.noml     编译器内部          13
  toolchain.noml    工具链              17
  std.noml          标准库模块索引     119
  traps.noml        已知坑              18
```

`index.noml`：

```noml
{
  version: 1
  domains: [
    'types.noml',
    'syntax.noml',
  ]
}
```

域文件：

```noml
{
  version: 1
  domain: 'types'          ; 必须与文件名 stem 一致，check 会核对
  title: '类型系统'
  terms: {
    'option': {
      title: 'option (?T)'
      kind: 'type'
      status: 'stable'
      summary: '一句话摘要，AI 优先读这个。'
      aliases: ['?T', '三态']
      definition: `...`    ; 反引号原始块，多行 Markdown 友好
      syntax: `...`
      example: `...`
      related: ['syntax-match', 'nil']
      refs: ['src/parser/type.go']
    }
  }
}
```

### 词条字段

| 字段 | 必填 | 说明 |
| --- | --- | --- |
| 键名（`'option':`） | 是 | 词条 id，同时是 `glossary get` 的参数；`#overflow` 这类以 `#` 开头的要加引号 |
| `title` | 是 | 显示名 |
| `kind` | 是 | `type`/`annotation`/`keyword`/`operator`/`concept`/`builtin`/`attr`/`macro`/`flag`/`module`/`trap` |
| `status` | 是 | `stable`/`unstable`/`removed`/`unused`/`deprecated`/`trap`/`experimental` |
| `summary` | 是 | 一句话摘要，检索结果与 MCP 默认返回 |
| `aliases` | 否 | 别名 / 报错码（如 `15w45dqk`、`m7vq3xrt`），检索会命中 |
| `definition` | 是 | 多行定义 |
| `syntax` | 否 | 语法模板 |
| `example` | 否 | 可编译示例 |
| `related` | 否 | 关联词条 id，`related` 子命令沿它展开 |
| `refs` | 否 | 源码/文档路径，相对**工作区根** |

检索打分：`id` / `title` / `aliases` 命中权重高于 `summary` 与 `definition`；
大小写折叠后才比较。中文可直接查（内部按字节切片，不假设 UTF-16）。

---

## 一致性体检

```bash
./glossary/dist/glossary check      # 0 error / 0 warning，rc=0
```

- **ERROR（rc=1）**：缺必填字段、`kind`/`status` 不在允许集、id 与域内键不一致、
  跨域重复 id、`domain` 字段与文件名不符。
- **WARN（rc=0）**：`related` 指向不存在的词条、`refs` 指向的文件不存在。

之所以把引用类问题降为 WARN：词库是逐步补的，「先写引用、目标稍后补」是正常过程；
但结构错了就是错，必须挡住。当前基线是 **0 error / 0 warning**，新增词条后请保持。

---

## 新增一条词条

1. 选定语义域；没有合适的域就新建 `xxx.noml`（`domain` = 文件名 stem），
   并在 `index.noml` 的 `domains` 里加一行。
2. 在 `terms` 里加一个条目，`kind`/`status` 取允许值。
3. 跑 `./glossary/dist/glossary check`，直到 0 error / 0 warning。
4. 跑 `./glossary/dist/glossary get <新id>` 确认渲染正常（尤其是反引号块里的缩进）。

`.noml` 的三条硬约束（否则解析失败）：

- **成员之间必须换行**，`{ a: 1  b: 2 }` 这种单行多成员写法一定失败；
- 数组元素用逗号分隔，可以写在一行；
- 反引号原始块内**不能出现反引号**；重复键是解析错误；没有 `null`，只有 `nil`。

---

## 目录结构

```
glossary/
  main.no            入口：逐个 `# /glossary/src/xxx` 导入后调 drv.dispatch()
  package.noml       包元信息（output: ./dist）
  src/
    nav.no           noml 索引级访问薄封装（child/at/key-at/len-of/...）
    db.no            Arena + 载入 + 查询（gl-db 单例）
    jsono.no         单行 JSON 输出（手写转义，保证无裸换行）
    jsonp.no         极简 JSON 读取器（自带，不用 std/json）
    render.no        text / md / json 三形态渲染
    schema.no        check 的实现
    mcp.no           MCP stdio 传输 + JSON-RPC 分派
    mcph.no          MCP HTTP 传输（自实现，不用 std 的 http server）
    drv.no           CLI 参数解析与子命令分派
  data/              词库数据
  dist/              构建产物
  tests/
    probe.no         Phase 0 探针（noml pool 跨模块访问 / []T / 文件 embed）
    probe-eof-free-bug.no  编译器误编译最小复现（见下）
```

### 为什么自己写 JSON 读写

`std/json` 处理非 ASCII 时表现异常（中文经 `.get().str()` 后只剩部分有效字节，
其余为 NUL；`.stringify()` 会重复末字符并输出 U+FFFD），因此 `jsonp.no` / `jsono.no`
自带读写；同一时期 `src/std/json.no` 正被其他改动牵动，也不适合作为依赖。

`probe-eof-free-bug.no` 是一个与词库无关的**编译器误编译复现**：
`[]byte` 缓冲区里 push 过元素后再调 `.len()` 作孤立分支条件、并给模块级结构体字段赋值，
会生成重复释放，运行时 `malloc: pointer being freed was not allocated`（rc=134）。
保留在 `tests/` 供后续修编译器时回归；`probe.no` 则用来在换机器/换编译器版本后
快速确认本项目依赖的三个前提仍然成立。

---

## 已知限制

- 词库是**手写数据 + 机械校验**，不是从编译器源码派生，所以和源码之间没有强一致约束；
  `check` 只保证结构与引用不悬空，不保证内容与最新实现同步。
- `refs` 路径相对工作区根解析，脱离本仓库单独分发二进制时这些路径仅供人看。
- `search` 是子串匹配 + 权重排序，没有分词与同义词扩展；中文靠别名与摘要原文命中。
