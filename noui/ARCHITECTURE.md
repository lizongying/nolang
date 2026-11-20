# noui — 用 Nolang 重写 GPUI 精神内核

> 目标：一套 Nolang 原生 UI 框架，覆盖 GPUI 的核心能力面（状态 / 布局 / 渲染 / 输入 / 事件 / widget / 主题），
> API 贴合 Nolang 习惯，不逐字兼容 Rust。后端走 SDL3 的 `SDL_GPU`（底层 Metal / Vulkan / DX12）。

> 工程位置：仓库根目录 `noui/`（原 `tmp/noui/`，2026-11-20 迁入根目录以便纳入版本管理）。
> 探针仍在 gitignored 的 `tmp/noui-probe/`（一次性实验，不随工程迁移）。

## 1. 已验证的地基（探针 tmp/noui-probe）

- `no build -cc <wrapper>` 的 `-cc` 是自由字符串，直接作为链接器命令执行；wrapper 注入 `-I/-L` 即可链接 SDL3。
- Nolang `#{c}` FFI：支持 str 回传（自动复制为拥有所有权 str）、i32、i64 承载指针、out 参数 `**byte`。
- 不支持结构体按值传参 → 用 C shim（`shim.c`）把 SDL 大写符号与结构体 API 摊平成小写标量函数。
- 实测：`SDL 3.4.18`、窗口句柄有效、事件轮询/延迟/清理可用。
- 运行需 `DYLD_LIBRARY_PATH=/opt/homebrew/opt/sdl3/lib`。

## 2. 分层架构（从底到顶）

```
┌─────────────────────────────────────────────┐
│ L5  examples/     应用示例（计数器 + 按钮）        │
├─────────────────────────────────────────────┤
│ L4  widget.no     div / text / button 元素构造器   │
│                   + 主题样式（Tailwind 风格 API）    │
├─────────────────────────────────────────────┤
│ L3  app.no        App / Context / 事件分发 / Action │
│                   View(Render) 契约                 │
├─────────────────────────────────────────────┤
│ L2  layout.no     flex 布局子集（行/列、间距、对齐）  │
│     element.no    Element 树 + 三阶段渲染管线         │
│     scene.no      Scene 绘制指令（quad / text）      │
├─────────────────────────────────────────────┤
│ L1  entity.no     Entity registry（i64 句柄 + 池）   │
│     geom.no       Vec2 / Rect / Size / Color         │
├─────────────────────────────────────────────┤
│ L0  shim.c        SDL3 C ABI 摊平层                  │
│     platform.no   #{c} 声明（窗口/事件/GPU）          │
└─────────────────────────────────────────────┘
```

## 3. 核心抽象映射（GPUI → Nolang）

| GPUI | noui | 落地方式 |
|---|---|---|
| `Application` (singleton) | `App` 模块单例 | 模块级 `app-state` 全局，进程生命周期 |
| `Entity<T>` (RC + observe) | Entity 句柄 `i64` | i64 索引 + 全局池（`dom.no` 范式），显式 `notify/observe/emit` |
| 单前台线程不变量 | 天然满足 | Nolang 单线程协作协程，禁跨协程共享可变堆 |
| `Context` / `WindowContext` | `Ctx` | 每帧传递，持有 scene 累加器 + 事件队列 + 窗口句柄 |
| `View: Render` | `render` 函数契约 | `render(cx, entity) -> Element`，View 是带 render 的 Entity |
| `Element` 三阶段 | `layout / prepaint / paint` | 纯 Nolang，Element 树递归三遍 |
| `Scene` (push_quad/push_text) | `Scene` 指令列表 | `[]DrawCmd`，后端无关，可 textual dump 做测试 |
| Taffy flexbox | `layout.no` | 布局子集：row/column、gap、padding、justify/align |
| `Action` / keymap | `Action` 枚举 + keymap 表 | 键盘事件 → action 分发 |
| `FocusHandle` | focus 句柄 i64 | 全局焦点实体 id |
| 平台 / Metal/Vulkan/DX12 | SDL3 `SDL_GPU` | C shim → `#{c}` |

## 4. 三阶段渲染管线

每帧：
1. **`layout`**：根 Element 递归，按 flex 规则计算每个节点的 `Rect`，写入 Element 的 `frame` 字段。
2. **`prepaint`**：注册 hitbox（矩形 → 事件路由表），处理 hover/focus 决策。
3. **`paint`**：递归发出绘制指令到 `Scene`（矩形 → quad，文字 → text glyph）。
4. **呈现**：`backend` 把 `Scene` 的指令翻译成 SDL_GPU 绘制调用，提交帧。

Element 树不跨帧保留（GPUI 的 immediate 哲学）：每帧由 View 重新 `render` 出树。需要跨帧的状态放 Entity。

## 5. 数据模型要点

- **Element**：`kind`（rect/text/container）、`style`（Rect/颜色/圆角/flex 参数）、`children []i64`（池索引）、`frame Rect`。
  存在 `ElementPool`（`[]element-node` + i64 句柄），避免按值深拷贝整棵树。
- **Scene / DrawCmd**：`kind`（quad/text）、`rect`、`color`、`text`。 quad 直接映射 SDL_GPU 的彩色三角形。
- **Entity**：`[]entity-slot`，每槽 `{kind, payload}`；payload 先用有限的标量槽（i64/str），
  足够表达计数器、按钮状态。observe/emit 用订阅表（`[]sub`）。
- **Color**：`rgba` 四字节打包进 i64（0xRRGGBBAA），shim 侧拆成 float4。

## 5.1 实测约束与编译器修复（2026-11-20 更新）

### 旧结论（已推翻）
早期实测把下列现象归为「MIR 能力边界」，并据此把内核降级为顶层主循环：
- 函数内 push 全局 vec 报 `vec.push: needs receiver and element`
- vec-of-struct push 报 `unsupported value type`
- 函数内读/写全局 vec 得 void slot

### 真正根因（用户指出：全局变量必须大写）
按语言规范 `docs/docs/lang/syntax.md`：**module 级全局必须用大写名开头，小写顶层变量是 main 的局部变量**。
旧探针全用小写 `g-*`/`e-*`，所以函数引用到的是另一份存储。这不是 MIR 无能，是命名违规。

但即便改成大写，仍有一个真实编译器缺陷：`BUF []i64 = with-len(0)` 这类**带 call 初始化器的大写全局**，
在 `hir2mir.go` 的 registration loop 里因 `foldConstText(with-len)==""` 被 `continue` 跳过，没注册成 module
global；synthesizeMain 又把它 inline 成 main 局部 → 函数侧读到空 global。这是编译器 bug。

### 编译器修复（已落地，make test 全绿）
`src/mir/hir2mir.go` 三处改动 + 回归测试（`src/mir/global_call_init_test.go`、`compiler/tests/probe-glob-call-init.no`）：
1. `globalRefInit`：**大写名 + 被函数引用 + 容器类型 + call 初始化器** 纳入 module global 注册白名单。
2. synthesizeMain 的 declOnly 守卫：带 call 初始化器的 global 不再跳过 KLet（否则 with-len 不执行）。
3. lowerStmtInner 的 write-back：允许顶层 call 初始化的 owned %vec 写入 global slot（全局生命周期=程序，无需 scope drop）。

修复后：`BUF []i64 = with-len(3)` 被函数读写，长度与内容都正确（回归测试锁定）。

### 已澄清：旧 kernel 的「死循环」是语法误用，不是 MIR 缺陷

旧 `tests/kernel.no` 卡死曾被认为是「20+ 混合类型全局池 + 嵌套条件里的全局索引读写」触发的独立 MIR 缺陷。
实测定位（采样运行中进程栈 → `el_add` 100% CPU → 对比 LLVM IR 控制流）后更正：

- **真正原因**：`(cond) { }` 在 Nolang 里是**条件循环（while）**，`(cond) -> { }` 才是一次性守卫（guard）。
  旧内核把 3 处 guard 写成了 `(cond) { }`：`el-add` 的 `E-CHILDREN-COUNT[parent] == 0`，
  以及 `scene-dump` 的 `D-KIND[i] == CMD-RECT` / `CMD-TEXT`。body 不改条件 ⇒ `for.header/for.body/for.update`
  回边恒成立 ⇒ 死循环。与全局池规模、索引读写的 MIR lowering 无关。
- **修复**：3 处统一改成 `(cond) -> { }`。大写版 `tests/kernel2.no` 复活，`elements: 2` 正常退出。
- **诊断规则（2026-11-20 最终）**：頂層數據綁定按首字符四檔（`build/global_case.go` 管同模組、`build/module_check.go` 管跨模組，診斷含原因 + 改名建議）：
  - `BUF` 大寫 = **全局**：跨函式 / 跨模組都合法。
  - `buf` 小寫 = **局部**：被函式引用 → 硬錯誤（改 `Buf`）；跨模組 `mod.buf` 也硬錯誤。純 main 局部合法。
  - `_BUF` = **私有全局**：**同模組跨函式讀寫合法**（`_` 是「模組私有」標記）；只有**跨模組** `mod._BUF` 訪問 → 硬錯誤（去底線改 `BUF`）。
  - `_buf` = **私有局部**：Nolang 不開放，無條件硬錯誤。單獨 `_` 通配放行。函数/方法/接口/结构体名可小寫 / `_` 開頭（`foo`、`_helper`），不被函式引用的純 main 局部也合法。
  - MIR 側無需改：`isGlobalStyleName` 已把 `_大寫` 註冊為 module global，`globalVisible` 按 owner 作用域（同模組可見）本就支援。歷經多次搞錯門檻，現已統一。

分层内核 v4（`tests/core-v4.no` / `tests/main-layered.no`）的写法仍然有效且推荐：
- 全局池字段控制在必要范围（11–12 个），SoA 平行数组；
- 条件里的全局索引先存局部变量（`cnt = EL-COUNT[p]` 再判断 `cnt == 0`）；
- 计数自增用 match 解包 option（`cnt + 1: { ok -> it }`）而非溢出注解。
但「条件里先存局部」只是可读性/保险写法，不是绕过编译器缺陷的必需规避。

### 仍然成立的约束
- 字符串用单引号 `'text'`（双引号是 char）；字符串插值里放复杂表达式易触发 MIR 问题，先算成局部变量。
- vec-of-struct push 不支持 → 一律 SoA 平行数组。
- 参数/返回走标量；`with-len(N)` 全局需显式类型标注。
- **全局写回缺陷（2026-11-20 阶段3实测）**：函式内连续写回多个**独立标量全局**时，第 4 个及以后的赋值会静默丢失（f64/i64 混合尤甚）；负值初始化的 f64 全局赋值也不可靠。规避：把一帧状态集中进一个 `CTX []i64` 数组池，用 `CTX[slot] = v` 索引写入（实测可靠），坐标用定点 i64（值*100）。见 `tests/app-layer.no`。
- **函式嵌套调用缺陷**：辅助函式（如 `keymap-resolve` 读全局数组）被另一函式调用后，其调用者的全局写回会丢失甚至静默崩溃。规避：这类辅助函式只在顶层调用，结果以参数传入。

### §5.2 组件库实现新坑（2026-11-20，`src/components.no` 实测）

- **option + 字符串插值 = optunuse 假错**：`txt = ARR[h]: { ok -> it  -> '' }` 解包出的 `txt` 若用在字符串插值
  `'{txt}{ch}'` 里，会报 "option bound but never consumed"（`txt` 被当成未消费的 `?str`）；同一 `txt`
  赋给普通变量或 `print` 则正常。**绕法**：字符串追加改用 `txt.push-str(ch)` 原地方法再写回数组，`io` / 插值
  全部避开 option 值。实测 `append-probe` 复现（插值版报 optunuse，push-str 版得 `[xab]`）。
- **match default arm 里再读数组下标 ⇒ match 结果值算错**（下游表现为「写回失效」）：
  `n = ARR[h] + 1: { ok -> it  -> ARR[h] }` 得到的 `n` 恒为旧值 0；先存局部 `cnt = ARR[h]`
  再用 `cnt + 1: { ok -> it  -> cnt }` 则正确 1。core-v4.no 的 `el-add` 一直是后一种写法，
  组件库初版 button-click 用了前一种故点击计数丢失。
  - **最小复现**：`tmp/checker-probe/match-idx-legal.no`（`ARR []i64 = [0]`，同一档内
    `bad()` / `good()` 两写法对比；写回走合法的返回变量，**不用非法的 `ARR[0] = n`**），
    `no run` 输出 `bad=0 good=1`；两写法都无 checker error。这说明缺陷在 match 结果值
    本身，与写回方式无关。
  - **IR 机制**（`no build -v` 对比 `@bad` / `@good`）：good 版为 match 结果分配
    `%option_i64` 槽、生成 overflow 检查与 err 构造，按 tag 正确分支；bad 版所有槽都是
    `alloca i64`，**option 槽与 overflow 检查整体消失**，直接用 `icmp eq <裸加法结果>, 0`
    当 tag 判断——`0+1=1` 非零被判成失败分支，n 取到 default arm 的 `ARR[0]`（旧值 0），
    后续任何写回或使用都拿到错值。
  - **规则**：match 表达式及其 arms 内不得再读数组下标，先落到局部变量（下标读取本身产出
    option，会污染 MIR 对 match 结果载荷类型的推导）。另：`ARR[h] = v` 属禁用写法，
    写回请走 `push` 家族 / `fill` / place-match。
- **i64 位运算 `&` 产生 option**：`(FLAGS[h] & MASK) != 0` 直接比较会把 option 挂到下一行报 optunuse。
  **绕法**：`#{overflow=wrap}` + 中间变量 `disabled = FLAGS[h] & MASK`，再 `disabled != 0`。
- **str 数组下标读只能一步直跟 match**：`cur = ARR[h]`（裸下标当表达式）会产生未消费 `?str`；
  必须是 `v = ARR[h]: { ok -> it  -> '' }`（下标直跟 match 块）。裸下标仅在 match 表达式位置合法。
- **option 与字符串的三种口径（2026-11-20 校正）**：
  - `print('{a}')` / `format('{a}')` 里的 `{field}` 是真插值（MIR 才展开），**算消费 option**，不报 optunuse；
  - 普通字符串字面量 `s = 'v={a}'` **不展开**（输出字面量 `v={a}`），a 仍算未消费 → 报 optunuse；
  - 字符串拼接 `s = 'v=' - a`（以及错误的 `+` 写法）会把 option 的 tag+payload 位模式直接拼进去
    （实测输出 `12884901891`），是沉默泄漏 → 现在也报 optunuse。数值减法 `a - 1` 不受影响。
  修法在 checker 的读取收集：拼接站点只跳过**直接操作数标识符**，方法调用等子树仍算读取。
- **`#` 导入模块的型别检查（2026-11-20 已在编译器修复）**：此前入口 `vet`/`build` 会完全吞掉导入模块函数体的型别错误，
  build 成功但运行时静默降级（返回变量取默认值 `0`）。现在编译器在模块 AST 刚解析、注解侧表完整时补跑
  `ValidateTypesTHIR`：`vet` 把诊断追加到 lint 串流并正确归因到模块文件，`build` 直接硬失败。
  最小复现与修复前后命令见 `tmp/checker-probe/README.md`，回归测试在
  `src/build/imported_module_type_error_test.go`。
  - **不能在 merged program 上补跑**：合併會丟棄模組 Sem 注解側表，`#{overflow=wrap}` 查不到，
    合法的 `i = i + 1` 會被誤報成 15w45dqk（`lib_export_sem_test` 的失效形态）。
  - **optunuse 也已纳入 build 硬错误**：入口自身本来就是硬错误；導入模組內的 optunuse
    現在同樣讓 `no build` 失敗（vet 路徑不重複報告，RunAllLints 已報）。
  - std 模組由 `no vet src/std` 單獨守護；組件庫仍建議單獨 `no vet src/components.no`
    看全 hint/overflow 診斷。
  - **最小复现**（`tmp/checker-probe/`，workspace.noml = `{ probe: '.' }`）：
    - 模块 `bad.no`：`bad-fn = () (r i64) { r = 'not-an-int' }`
    - 入口 `main.no`：`# /bad.bad-fn` + `r = bad-fn()` + `print('r={r}')`
    - 修复前：`no vet main.no` → `No issues found.`；`no build main.no` → 成功；运行打印 `r=0`
    - 修复后：两者均报 `bad.no:4:9: [ERROR] ... 15w45dqk`，build rc=1，不再产出降级二进制
    - 对照 optunuse 模块 `optbad.no`：入口 vet 仍报（恰好 1 条，不重复），
      `no build` 现在也 rc=1 并归因到 `optbad.no:4:5`
- **同名函数静默覆盖**：getter `cmp-text`（取文本）与 text 组件构造函数同名，后者覆盖前者导致
  "expected str, got i64"。全库定义须 `grep -E '^[a-z].*=' | uniq -d` 查重，构造函数改名 `cmp-text-node`。

### 阶段 3 应用层（`tests/app-layer.no`，已验证）
- Action 枚举 + keymap（`KM-KEY`/`KM-ACTION` 平行数组，`keymap-resolve` 线性查表）。
- 焦点系统：`FOCUS-ID` + `FOCUS-RING`，`focus-cycle(dir)` 环形前后移动。
- Ctx 帧上下文：`CTX []i64` 池集中承载 view/mouse/hover/action，命中测试 `ctx-hit-entity`。
- 测试：`no run -cc ccwrap.sh tests/app-layer.no` → `ALL APP-LAYER TESTS PASSED`。

## 6. 后端（两套 shim，均已落地验证）

**回退后端 `src/shim/shim.c` + `shim.h`**（SDL_Render，导出 `noui_*`）：
- 窗口/事件：`noui_init`、`noui_window_create`、`noui_quit`、`noui_poll`、`noui_delay`、`noui_version`
- 帧：`noui_begin_frame`（清屏）、`noui_set_color`、`noui_draw_rect`、`noui_end_frame`（present）
- 文字（阶段6升级）：`noui_font_init(path, ptsize)`、`noui_font_draw(x,y,r,g,b,a,text)` 用 **SDL_ttf 3.x（内部 HarfBuzz shaping）** 渲染到 surface→texture；未加载字体时 `noui_draw_text` 回退 DebugText。
- 实测：app 打印 `font shaping: 1`，窗口句柄有效，ESC/超时正常退出。

**GPU 后端 `src/shim/gpu_shim.c` + `gpu_shim.h`**（SDL_GPU，导出 `noui_gpu_*`）：
- `noui_gpu_init` 用 `SDL_CreateGPUDevice(SDL_GPU_SHADERFORMAT_MSL)`；macOS 走 **Metal**（实测 `Metal API Validation Enabled`），Linux Vulkan / Windows DX12 由 SDL 按平台选择，调用方不变。
- 顶点格式 `{vec2 pos, vec4 color}`（24B）；MSL vertex/fragment shader 以源码交 `SDL_CreateGPUShader`。
- `noui_gpu_draw_rect` 追加 6 顶点（2 三角形），`noui_gpu_frame` 经 transfer buffer 上传 + render pass + `SDL_DrawGPUPrimitives` 一次绘制。
- 演示：`example/noui-gpu-demo/`（`../../bin/no build -cc ../../noui/ccwrap.sh -o gpu-demo .`），编译链接通过、窗口创建成功、Metal 驱动启用。

**FFI 参数陷阱（阶段6实测）**：Nolang `f64` 实参传给 C `float` 形参，与 `str` 混合时值会变成 0（`ptsize=0.0` 导致字体加载失败）。规避：shim 侧浮点形参一律声明为 `double`（对应 Nolang `f64`），内部再转 float。

构建：`build-shims.sh` 一次编译两个 `.a`（libnoui-shim.a / libnoui-gpu-shim.a）；`ccwrap.sh` 注入 sdl3/sdl3_ttf 的 -I/-L。运行需 `DYLD_LIBRARY_PATH=/opt/homebrew/opt/sdl3/lib:/opt/homebrew/opt/sdl3_ttf/lib`。依赖：`brew install sdl3 sdl3_ttf`。

## 7. 分步执行计划

- [x] 阶段 0：地基验证（FFI + SDL3 + shim + -cc wrapper）— 已完成
- [x] 阶段 1：Entity registry + Element pool（大写全局 SoA + 函数封装）— `tests/core-v4.no`，已验证
- [x] 阶段 2：Scene 指令池 + 绘制遍历 — `tests/core-v4.no` / `tests/main-layered.no`，已验证
- [x] 阶段 5：C shim SDL 后端 + 端到端分层示例 — `tests/main-layered.no`，窗口跑通
- [x] 阶段 3：Context / Action / 焦点系统 — `tests/app-layer.no`，已验证（ALL APP-LAYER TESTS PASSED）
- [x] 阶段 4：widget 库 + 主题 — `tests/widget.no`，已验证（ALL WIDGET TESTS PASSED）
- [x] 阶段 6：SDL_GPU / 字体 shaping — GPU shim（MSL shader，Metal 驱动实测启用）+ SDL_ttf(HarfBuzz) 字体，均已验证
      —— `src/shim/gpu_shim.c`（SDL_GPU 矩形）、`src/shim/shim.c`（TTF shaping）、`example/noui-gpu-demo/`（GPU 演示）、`main.no`（字体）。
      旧 kernel 的「死循环」是 `(cond){}` 语法误用，3 处已改为 `-> {}`（与 GPU 无关）。
- [x] 阶段 7：全目录组件库 — `src/components.no`（gpui-kit 86 模块 / 83 kind），核心 10 状态机；
      smoke（83 构造）+ core（交互）两套测试全绿。映射清单 `src/components-catalog.md`。
      功能对等长期项：DataTable 虚拟滚动 / Dock / 代码编辑器（当前仅 API + 最小状态）。

### 当前可运行产物
- `main.no`（包根唯一入口）：原始纵切面（顶层主循环 + 标量全局），已验证
- `tests/main-layered.no`：**分层内核版**（Entity/Element/Scene 大写全局池 + 函数封装 + Scene 遍历绘制），已验证
  运行：`../bin/no build -cc ./ccwrap.sh -o dist/app-layered tests/main-layered.no`
  启动打印 `scene=8 elements=3 entities=3`，窗口持续存活，ESC/超时退出。

## 8. 验收目标

- [x] `no test` 覆盖 L1–L3 纯逻辑（实体注册、布局计算、scene dump）— `tests/logic/l1-l3.no`，
      11 项断言全绿；该目录用独立 `package.noml`（无 link-libs）避开 shim 链接，
      直接 `cd tests/logic && ../../../bin/no test .` 即可。
- [x] 端到端示例：一个窗口显示按钮，点击后计数器 +1，窗口标题/内容随之变化，ESC 退出。
- [x] 全部构建走 `no build -cc wrapper`，运行带 `DYLD_LIBRARY_PATH`。
