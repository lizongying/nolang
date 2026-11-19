---
sidebar_position: 5
---

# Playground

Nolang Playground 是一个在浏览器中直接编辑、编译并执行 Nolang 程式的线上环境。它将 Nolang 编译器（`no.wasm`）与语言伺服器（`lsp.wasm`）编译为 WebAssembly，并透过 WASI 在浏览器沙箱中执行使用者程式码，无需本地安装任何工具链。

立即前往：[**/playground**](/playground)

## 用法

Playground 页面分为左右两个窗格：

- **左侧 Editor**：基于 [CodeMirror 6](https://codemirror.net/) 的程式码编辑器，支援 Nolang 语法高亮、自动缩排与历史纪录。
- **右侧 Output / stderr / Diagnostics**：执行结果、标准错误输出与诊断讯息。

介面会跟随 Docusaurus 的语系自动切换简体中文与英文，输出窗格也同时支援亮色与暗色主题。

### 工具列

| 按钮      | 功能                                                              |
| --------- | ----------------------------------------------------------------- |
| **▶ Run** | 编译当前程式码为 WASM 并透过 WASI 执行；stdout 显示于右侧 Output。 |
| **Format**| 透过 LSP 呼叫 `no fmt` 格式化当前程式码。                          |
| **Examples** | 依分类载入内建范例，覆盖编辑器当前内容。                        |

工具列右侧显示目前状态（空闲／编译中／运行中／完成／错误）；下拉选单下方会显示该范例的中英双语说明。上次选择的范例会保存在浏览器本机储存中。

### 范例

Examples 下拉选单预先收录了 **24 个**依主题分组的范例，全部透过与浏览器完全相同的 `--wasm-direct` 编译路径在 CI 中验证：

| 分类 | 数量 | 内容 |
| ---- | ---- | ---- |
| 基础 | 7 | Hello World、变数推断、函数与递回、Fibonacci、算术与位运算、格式化输出、字串 |
| 流程控制 | 6 | 短路守卫、模式匹配、FizzBuzz、四种区间回圈、条件 while 回圈、break / continue |
| 资料结构 | 4 | 定长阵列、动态切片、结构体、方法 |
| 标量类型 | 4 | char、bool、f64、byte |
| 标准库 | 2 | math.max / math.min / sqrt、rand.rand |
| 错误处理 | 1 | option 与 nil 的三臂 match |

每个范例都附带一段稳定的预期输出字串；CI 会编译每个范例、用 `wasmtime` 执行，并断言该字串出现在 stdout 中。因此语言或 Direct-WASM 后端演进后，范例不会悄悄失效。

选择任一范例后，Playground 会清空当前 Output、stderr 与 Diagnostics，并以该范例内容取代编辑器。

### 诊断

当 LSP 侦测到语法或型别错误时，会在右下角的 Diagnostics 区列出。点击任一诊断项目即可跳转到对应的行与栏。编译器 stderr 中的 `file:line:col` 讯息会另外解析成 **Run Errors** 标记，并自动移除尾端的 TraceID 与本地化的严重性前缀。

### 错误与逾时

过去 WASM 载入失败只会写入 `console.warn`，使用者看到的是一个毫无反应的 Run 按钮。现在的行为是：

- `no.wasm` 或 `lsp.wasm` 下载／编译失败时，页面顶端会显示可关闭的错误横幅，说明是哪个资源与原因。
- 语言伺服器初始化失败同样会显示横幅；编辑器仍可继续使用，只是补全、悬停与格式化不可用。
- 每次执行由主执行绪看门狗计时（预设 8 秒）。`wasi.start()` 在 worker 中是同步呼叫，无限回圈会让 worker 内的任何计时器都无法触发，因此唯一有效的中断方式是从主执行绪终止 worker。逾时后 worker 会被终止并在下次执行时自动重建，同时立刻重新预热 WASM。
- 重复点击 Run 会被并行防护挡下， pending 编辑的防抖计时器在切换范例或格式化时会被取消，避免旧输入覆盖新内容。

`no.wasm` 与 `lsp.wasm` 会在页面空闲时预先下载并以串流方式编译，首次按 Run 不必再等待完整的 27 MB 下载与编译。

## 限制

由于 Playground 在浏览器 WASM 沙箱中执行，部分 Nolang 功能无法使用：

### 不支援 FFI

浏览器无法连结原生 C 函式库，因此 `#{c}`、`#{cpp}`、`#{rust}` 等 FFI 宣告在 Playground 中无法使用。包含 FFI 的程式码（如 `sqlite`、`mysql` driver）无法执行。

### 不支援 fork / exec / pipe

WASI preview1 不提供行程管理 API，因此 `process` 标准库（`process.start`、`process.wait` 等）在 Playground 中不可用。

### 网路 socket 不可用

WASI preview1 不提供 socket API，因此以下标准库在 Playground 中无法使用：

- `net`（TCP listener / conn）
- `http` / `http2` / `http3`（HTTP client）
- `ws`（WebSocket）
- `tls`（TLS 连线）
- `quic`、`sse`、`dns`

**对 `no` / `lsp` 本身的约束（比标准库更严格）**：wasm 产物连「导入」socket 函式都不行。
浏览器端用 `@wasmer/wasi` 实例化，它没有实作 `sock_accept` / `sock_shutdown`，
所以产物一旦因 `net` / `net/http` 被链进来而多出这两个 `wasi_snapshot_preview1` 导入，
`WebAssembly.Instance` 会直接掷出：

```
Failed to instantiate WASI: RuntimeError: JsValue(LinkError: WebAssembly.Instance():
Import #28 "wasi_snapshot_preview1" "sock_accept": function import requires a callable)
```

后果是整支程式连 `_start` 都跑不到——语言服务会报「初始化失败」（补全、悬停、格式化全废），
执行按钮也一样。因此凡是只有原生环境才有意义的网路程式码，一律放进 `!wasip1` 的档案，
wasip1 侧只留报错桩：`cmd/lsp/pprof_wasip1.go`、`cmd/no/serve_esm_wasip1.go`、
`package/download_net_wasip1.go`、`package/release_net_wasip1.go`。
`bun scripts/wasm_imports_check.ts`（`make wasm-imports-check`）用引擎自己的导入表守这条线。

### `poll_oneoff` 由前端补上

导入表干净只代表「函式存在」，不代表「函式不炸」。`@wasmer/wasi` 的 `poll_oneoff` 本体是
Rust 的 `unimplemented!()`：只要被呼叫（连 0 个订阅都会），就执行 `unreachable` 让整支实例
trap，错误长这样：

```
Error while running start function: RuntimeError: JsValue(RuntimeError: unreachable)
```

Go 的 `runtime` 会把 `time.Sleep` 与 `netpoll(delay)` 都变成一个 monotonic clock 订阅，
所以 **每一个 Go 程式都会踩到**——`no fmt` / `no vet` / `no build` 全都会在第一次睡著时死掉，
而导入表检查完全看不出来。

修法在 `docs/src/playground/wasm-loader.ts`：`instantiateWasi()` 自己用 `wasi.getImports()`
组导入物件，把 `poll_oneoff` 换成 JS 实作（时钟订阅 → 睡到最近到期时间后回报 clock 事件；
只有 fd 订阅 → 立即回报就绪，避免永久阻塞）。注意**不能**把覆写丢给
`wasi.instantiate(module, overrides)`：glue 自己的函式优先，覆写会被无声忽略，程式照样 trap。
也因为改成自建导入物件，`wasi.start(instance)` 必须显式传入实例。

### 首次执行的编译时间

Run 会用 `no build --wasm-direct` 产生 wasm。实测 hello world 在 wasm 里约 **0.5 秒**
（原生 `no` 同命令约 0.6 秒；两者都在 2026-11 的 std 索引预烘焙之后，先前分别是
约 55 秒与 11 秒）。

原本的瓶颈不在 codegen，而在**每次冷启动都把整个 std 解析两遍**：`validateProgram`
的 `ValidateUndefinedVars` 为了认得 `math.max(...)` 这类模组限定呼叫，会为全部 119 个
std 模组收集导出符号；`CollectStdUnionAliases` 又独立解析同一批模组找 union 别名。
现在 `genstdsig` 把「每个模组的导出符号」与「哪几个模组有 union 别名」一起烤进
`stdsig_gen.go`，执行期直接查表；另有两道守卫（`hir.TestModuleExportsMatchChecker`
逐模组逐栏位比对 HIR 现算结果、`TestUnionAliasNarrowingMatchesFullScan` 比对窄扫描与
全量扫描），表过期会测试红而不是静默改语义。

看门狗仍保留「载入 + 编译 180 秒 / 使用者程式 5 秒」的两段预算：前者现在有极大余裕，
留著是为了吸收冷启动、大程式与慢装置的长尾。

### 档案系统为虚拟

WASI preview1 仅提供 stdin / stdout / stderr 三个档案描述符，没有真实档案系统。因此 `fs`、`path`、`os.get-env`、`os.set-env` 等涉及档案或环境的标准库在 Playground 中可能无法如预期运作。

### 记忆体上限

浏览器 WASM 通常受 32-bit 定址空间限制，可用记忆体上限约为 **2 GB ~ 4 GB**（视浏览器与作业系统而定）。需要大量记忆体的程式（如深度递回、大型阵列）可能因 out-of-memory 而失败。

## 浏览器相容性

Playground 需要支援 WebAssembly 的现代浏览器。建议使用以下版本或更新：

| 浏览器    | 最低版本 |
| --------- | -------- |
| Chrome    | 100+     |
| Firefox   | 100+     |
| Safari    | 16+      |
| Edge      | 100+     |

若浏览器不支援 WebAssembly 或未启用 JavaScript，Playground 将无法载入编辑器或执行程式码。

## 开发与验证

修改 Playground 后，可在干净的建置树上执行下列检查：

```bash
make playground-demos                       # 编译 + 在行程内用 playground 自己的 WASI loader 执行全部范例
make playground-demos DEMO_RUN=wasmtime     # 改用外部 wasmtime 执行（交叉比对用）
make playground-demos DEMO_RUN=none         # 只编译、不验输出（不可当发布门槛）
make wasm-imports-check                     # 重建两个 wasm 并验证导入表无 socket 函式
make wasm-runtime-smoke                     # 用 worker 的同一条载入路径实跑 version/fmt/vet
RUN_WASM_LSP=1 RUN_WASM_BUILD=1 make wasm-runtime-smoke   # 追加语言服务与完整编译+执行
bun scripts/playground_ui_check.ts          # 诊断解析、看门狗、worker 恢复、i18n
make playground-smoke                       # dev server 与 WASM 路由 HTTP 200
cd docs && bun run typecheck                # TypeScript 型别检查
```

`playground-demos` 的编译器是**原生** `no`，但产物走的是浏览器那条 `--wasm-direct -target wasm32-wasi`，
执行则用 `wasi-glue` + `wasm-loader` 的 `instantiateWasi()`——与 `runner.worker` 完全同一份程式码，
因此不需要安装 wasmtime，CI 直接就能跑。它同时验三件事：snippet 能编、产物能通过 WebAssembly 验证
（栏位值位 match 那类空堆叠错误在这里现形）、列印输出符合 `expect`（`?=` 静默得 0 这类后端偏差也在这里现形）。

它拦不到的只有一件事——`no.wasm` / `lsp.wasm` 自己的导入表（那两个产物不由 snippet 产生），
由 `wasm-imports-check` 补上：`@wasmer/wasi` 缺 `sock_*` 时 `WebAssembly.Instance` 会直接 `LinkError`，
整个 playground（含语言服务）连启动都做不到。worker 端另有一层兜底：`wasm-loader.ts` 的
`instantiateWasi()` 会把 `@wasmer/wasi` 没提供的 WASI 函式补成回传 `ENOSYS` / `NOTSUP` 的桩，
让意外的导入退化成一次失败的系统呼叫，而不是整片服务挂掉。

`wasm-imports-check` 也拦不到「函式存在但本体会 trap」（上面 `poll_oneoff` 那一类），
所以 `make wasm-runtime-smoke` 补上最后一格：它走 `instantiateWasi()` 的真实路径实跑
`no version` / `no fmt` / `no vet`，并可加验语言服务与完整编译 + 执行。

`docs/static/wasm/*.wasm` 被 `.gitignore` 排除，永远不应提交本机产物。`no.wasm` 与 `lsp.wasm` 由 CI 从同一份原始码与同一个 `StdSigKey` 一起重建，确保浏览器里的编译器、内嵌 std AST blob 与 Direct-WASM 后端来自同一次建置；三者版本错位正是过去 `unreachable` 与验证错误的常见来源。

CI（`.github/workflows/build.yml` 的 `pr-verify`、`static.yml` 的站台建置）会依序执行 Go 测试、型别检查、UI 检查与范例验证，然后才从干净树重建 WASM 并部署站台。
