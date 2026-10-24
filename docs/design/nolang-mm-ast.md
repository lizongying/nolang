# MetaMessage（mm）承载 Nolang AST

状态：**编码侧已实现并通过验证（2026-10-15），消费侧已接入编译管线**（见 §4.1）。
仓库：`metamessage/metamessage` v0.2.10（Go 1.25.4），模块路径 `github.com/metamessage/metamessage`。
约束实测基线已升至 mm v0.2.10（2026-10-15）：C3 的 65535 off-by-one 与 C4 的 JSONC 深度 bug 均已修
（见 `metamessage` 仓库 `fix(core)` / `fix(jsonc)` 两个 commit），但 C1/C2/C5/C6/C8 依旧。本日另实测
确认「递归具体树」不可行（见 §3.1），扁平节点表为 mm 唯一可编码形态。

已落地：
- `src/parser/astmm.go`：AST ↔ 节点表（含三張 node-keyed 表 `anns` / `typeFacts` / `nodeFacts`，按节点索引存储；Stage 7 前的 `nodeSem` 已刪）。
- `src/mmast/mmast.go`：节点表 → mm 分片（每片 ≤64KB）→ `<tool>.ast.zip`；反向解回 AST。
- `no pub`：产出 `<name>.ast.zip`；`no add`：写 `tool@host/owner/repo = <version>`。
- 验证：std 全量 118 个模块节点表往返 dump 文本逐字节一致（含注解）；
  `net/tls.no` 53500 节点 → 234 分片 → 926KB zip → 解回一致。

## 1. 目标

- `no add nogit:v0.1.33@github.com/lizongying/notools` —— 依赖 spec 与 `no install` 同形。
- 依赖产物从「源码 tarball」改成「预编 AST」：
  `https://github.com/lizongying/notools/releases/download/v0.1.33/nogit.ast.zip`
- package.jsonc 依赖键：`"<tool>@<host>/<owner>/<repo>"`，值为版本（`nogit@github.com/lizongying/notools: v0.1.33`）。
- 编译器侧：AST → mm（发布）、mm → AST（消费）。

## 2. 实测约束（基线 mm v0.2.10，全部来自本机探针实测，不是 README 推断）

> v0.2.10 相对 v0.2.9 的变化：C3（65535 off-by-one）与 C4（JSONC 深度 bug）已修；其余约束不变。
> 新增 C8（自引用递归栈溢出）为本日实测发现，是比 C1–C5 更根本的硬限制。

| # | 约束 | 实测证据 |
|---|---|---|
| C1 | **反射路径拒绝 `interface{}`** | `valueToNode`: `unsupported type: interface{} (cannot reconstruct concrete value)`。我们的 AST 全是接口字段（`Expression`×39、`Type`×14、`Statement`、`Node`、`AnnotationValue`）⇒ 不能 `EncodeFromValue(program)` |
| C2 | **零值/空容器一律拒绝** | `zero.i: not allow empty`；空 slice/map/nil 指针同样；`allow_empty` 标签对 `bool` 不支持（`type bool not support 'allow_empty' tag`）⇒ 只能编码非零标量、非空集合；缺省即零值 |
| C3 | **每个容器/字符串编码后 ≤ 65535 字节** | `array too long, max length: 65535, actual: 158612`；`string too long, max length: 65535`。**这是真正的 64KB 限制**（不是整个文档；`maxCap` 编码器上限其实是 1GB） |
| C4 | **JSONC 路径有深度 bug**：数组元素累加深度 | 5000 个元素的数组在**第 7 个元素**就 `max depth: 32`（`ValueToNode` 的 `maxDepth = 32`）。Go 值路径无此问题（1000 元素正常）⇒ **只用 `EncodeFromValue` / `DecodeToValue`，不用 JSONC 路径** |
| C5 | 键名被 `CamelToSnake` 改写 | `Kind`→`kind`、`lineNo`→`line_no`、`A`→`a`（`Kind` 与 `kind` 会撞）⇒ 影子字段名一律用全小写 snake_case |
| C6 | `DecodeToJsonc` 输出带尾逗号（非法 JSON） | 仅调试用，产物链路不依赖 |
| C7 | 官方自述「开发测试阶段，不建议生产使用」 | 接口与行为可能调整 ⇒ 必须 pin 版本 + 版本化我们的 wire 格式 |
| C8 | **自引用递归结构会 stack overflow** | mm 反射编码器遇 nil 指针/空容器会调 `createExampleValue` 造「示例值」（`internal/core/value_to_node.go` :1397/:1565/:1647）；示例值本身是完整节点 ⇒ 含 `[]*Node`/`map[...]*Node`/`*Node` 的自引用型別在编码首个叶子（children 恒空）时无限递归。与 C1–C5 无关，是 mm「从示例值反推 schema」设计的固有限制，外部无法用标签绕开 ⇒ **节点结构绝不能自引用，扁平索引表是唯一 mm 可编码形态** |

另：**「一个模块就是一个文件，不会超过 64K」不成立**——std 159 个模块里 8 个 gob+gzip 后仍 >64KB
（`net/tls.no` 181KB、`crypto/x509.no` 110KB、`yaml.no` 98KB、`str.no` 73KB…）。所以分片是必需的，
不是可选优化。

## 3. 设计决策

- **D1 走 Go 值路径**：`EncodeFromValue` / `DecodeToValue`，绕开 C1/C4 的 JSONC 路径。
- **D2 AST 具体化为「扁平节点表」**（C8 下**强制**，非可选）：每个 AST 节点 → 一个 `NTNode{K, C []NTCell}`，
  整棵树摊平成 `NodeTable{Nodes []NTNode, Root int32}`，子节点用 1-based 索引边引用（`NTCell{T:"n"/"l"}`）。
  索引 1-based，节点按 DFS 前序编号。
  - 好处：深度恆定 ~5（绕开 C4）、字段具体（绕开 C1）、缺省即零值（绕开 C2）；
    **且 `NTNode` 只含 `NTCell{N,T,V}`（全 string，零指向 NTNode 的指标），从源头切断自引用**，
    使 mm 的 `createExampleValue` 永远碰不到自引用型別（这是绕开 C8 的关键）。
  - 嵌套具体结构（`lexer.Token`×72、`lexer.Position`×7）按点号路径摊平成标量（`token.literal` 等），
    不需要为每种类型写映射代码。
- **D3 分片**：单块编码后逼近 C3 上限即切块（自适应：先试编，超过阈值/报错就回退半量）。
  超过 60KB 的单个字符串切成 `big:<field>` 分片。
  zip 条目：`manifest.mm` + `chunk-0000.mm`…（每片一个独立 mm 文档，互不拼接）。
- **D4 打包**：`nogit.ast.zip`（用户指定）。zip 的 deflate 负责压缩，mm 只管结构化。
- **D5 版本与失效**：manifest 带 `wire_ver`（codec 版本）、`ast_ver`（`parser.ASTVersion`）、
  `sig`（`parser.ExternSignatureFingerprint`）、`src_sha256`。任一不匹配即拒绝加载（硬错，不静默回退），
  与 incremental 的 hard-error 口径一致。
- **D6 依赖键**：`"nogit@github.com/lizongying/notools": "v0.1.33"`。

## 3.1 为什么不能改用「递归具体树」（2026-10-15 实测）

曾尝试把扁平索引表改成递归树（`MMNode` 含 `[]*MMNode` / `map[string]*MMNode` / `*MMNode` 子节点，
再走 `mm.EncodeFromValue`），以去除手写索引与字串 cell 分型。按用户确认的约束处理（C1 具体化、
C2 非 bool 加 `allow_empty`、C3 原生容器、C4 值路径、C5 两端一致）后，首个 `go test` 即 **stack overflow**，
栈帧全是 `internal/core.createExampleValue` 在自引用型別上无限递归。

**根因（C8）**：mm 反射编码器对 nil 指针/空容器的处理是「造一个示例值反推型别」；示例值本身是完整
节点，其 nil/空栏位又触发下一轮示例 → 任何自引用型別在编码首个叶子（children 必空）时死循环。
`allow_empty` 救不了（空容器分支不查它）。这与 C1–C5 无关，是 mm 设计层面的硬限制。

**对比其他序列化工具**（为何它们没这道坎）：

| 工具 | 递归树 | 机制 |
|---|---|---|
| JSON / MessagePack / CBOR | ✅ | 只编实际资料，nil→null、空容器→空，绝不造假示例 |
| Go gob | ✅ | 型别登记表，型别定义只送一次，沿实际资料递归（nolang 早期即 gob） |
| Protobuf / FlatBuffers / Cap'n Proto | ✅ | schema 先宣告递归型别一次，编码器只跟实际资料走 |
| **mm** | ❌ | schema-less 从「示例值」反推型别，遇 nil/空就造假示例 → 自引用栈溢出 |

**结论**：递归树在 mm 路径下不可编码。三条出路——（a）维持扁平 NodeTable（mm 唯一可编码形态，现况）；
（b）改 mm 核心让 nil/空直接省略而非造假示例（动 mm 核心，工作量大）；（c）AST blob 脱离 mm 改用原生
支援递归的工具（gob/msgpack/protobuf）。当前选 **(a)**。

## 4. 产物与流程

发布侧（`no pub`，当前是 stub）：
```
package 目录 → 逐 .no 解析 → AST → 节点表 → 分片 → mm → <tool>.ast.zip
```
消费侧（`no add` → `no sync` → 编译）：
```
<tool>.ast.zip → 校验 manifest → 逐片 mm → 节点表 → AST → 交给编译管线
```

### 4.1 消费侧接线（已实现）

依赖键 `mymod@host/owner/repo` 与源码里的 `use mymod` 不是前缀关系，所以三层各补一处：

| 层 | 位置 | 作用 |
|---|---|---|
| 键识别 | `pkg.IsASTDependencyKey`（`src/package/pkg.go`） | `@` 之后至少两段路径 ⇒ 预编 AST 依赖 |
| 键匹配 | `pkg.MatchDependency` → `dependencyPrefixes` | 把 `tool@host/owner/repo` 展开成 `tool` 与 `host/owner/repo` 两个可比前缀，`use mymod` 与 `use host/owner/repo/mymod` 都能命中 |
| 取 AST | `build.Transpiler.resolveASTDependency`（`src/build/transpiler.go`） | 在 `resolveUse` 的两处 `MatchDependency` 之后短路：调 `mmast.EnsureModuleAST`，**不走** `ResolveDependencyModule → DownloadPackage`（没有源码可下载） |
| 注册表 | `mmast.RegisterModuleAST` / `EnsureModuleAST`（`src/mmast/registry.go`） | 进程内 `sync.Map` 缓存；未命中才 `FetchASTZip`（含磁盘缓存） |

`EnsureModuleAST` 要求版本是具体值——`""` / `"*"` 直接报错，不做「猜最新版」。
一个 zip = 一个模块：`use host/owner/repo/deep` 会明确报 `single prebuilt module`，不静默退化成「文件找不到」。

`no sync` 额外预取：AST 依赖不进依赖图（`Package.resolveFromScratch` 跳过 `IsASTDependencyKey`），
由 `syncDependencies` 单独抓并快取到 `~/no/cache/ast/`，让后续编译离线可用。
快取根目录可用 `$NOLANG_AST_CACHE_DIR` 覆写（CI 想把快取放工作区内时用）。

端到端实测（本地 httptest / `python3 -m http.server` 冒充 GitHub Releases）：

```
no pack -o mymod.ast.zip mymod.no                 # 生产侧：1786 B / 30 nodes / 1 chunk
<serve> /o/r/releases/download/v1.0.0/mymod.ast.zip
no sync                                           # → ast  mymod@127.0.0.1:8123/o/r v1.0.0
no run main.no                                    # use mymod.greet → 输出 hi bob
```

版本不对时 `.../releases/download/v9.9.9/mymod.ast.zip (status 404)`——HTTP 状态错误不重试，
传输层错误（EOF / connection refused）才重试 3 次。

## 5. `no pack`（CI 工具）与 `no pub`（未来）

两者共用同一个入口 `packAST(dir|file.no, out)`（`src/cmd/no/main.go`），pub 只多一步上传：

```bash
# 打包当前包（读 package.jsonc 的 name / main）→ dist/<name>.ast.zip
no pack

# CI 常用：显式输出路径，之后上传为 release 资产
no pack -o nogit.ast.zip nogit.no

# 打包某个包目录
no pack -o nogit.ast.zip ./nogit
```

CI 示例（GitHub Actions）：

```yaml
- run: no pack -o nogit.ast.zip nogit.no
- run: gh release upload "$TAG" nogit.ast.zip
```

`no pack` 只需要 lexer + parser（与 `build/builder.go` 的解析入口一致：`lexer.NewCached` +
`p.Filename`），**不需要 LLVM 工具链**，所以能在任意 runner 上产出资产。
打包会打印 `bytes / nodes / chunks` 便于 CI 日志核对。

`no pub` 目前只做打包（registry 上传未实现），行为与 `no pack` 相同并复用同一代码路径。

## 6. 未完成 / 待定

- 大字符串（>64KB 字面量）分片后的拼接与校验。
- `no pub` 的 registry 上传尚未设计（当前只产出本地 zip）。
- 真实 `.ast.zip` 资产尚未发布：notools 的 release 里还没有 `nogit.ast.zip`，
  消费侧目前只有本地/单测级别的验证，没有跑过真实 GitHub 下载。
- 一个 zip 只装一个模块：仓库里多个工具（nogit / noimg / …）要各自打包。
  目前不支持「一个 zip 装多个模块 + `use repo/submod`」。
