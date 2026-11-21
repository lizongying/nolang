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

### 阶段 8 组件渲染层（`src/cmp-scene.no`，已验证）

把 `src/components.no` 的组件树摊平成后端无关的 Scene 指令池，补齐
「组件 → 布局 → 绘制 → 命中」闭环。组件树**跨帧保留**（不再每帧重建），
布局结果写入 `CS-LX/LY/LW/LH` 池并同步 `cmp-set-bounds`。

- 布局常量：定点 i64（1px = 100），`CS-PAD=1200` / `CS-GAP=800` / `CS-CHAR-W=800` / `CS-LINE-H=2400`。
- `cs-measure(h)`：自底向上。容器纵向取最大宽、求和高；`C-GRID` 横向求和宽、取最大高。
- `cs-layout(h,x,y,w,ht)`：自顶向下。容器子元素拉伸到内容宽 `w - 2*CS-PAD`，按测算高度依次堆叠。
- `cs-paint(h)` / `cs-paint-leaf(...)`：容器发 rect 后递归；叶按 `cmp-kind` 发射 rect / fill / text，
  hover 按钮（`F-HOVERED`）变 `CS-HOVER`，progress 发 track + fill，checkbox/switch 发选中块。
- `cs-hit(h,px,py)`：后序遍历，返回最深层命中句柄，无命中返回 -1。
- 算术统一走 `cs-add` / `cs-sub` / `cs-inc` / `cs-mul` / `cs-div`：索引读取与四则运算都产出 `?i64`，
  在 helper 内 match 解包成裸 i64，避免调用点重复解包。
- 测试：`no run -cc ccwrap.sh tests/cmp-scene.no` → 40 项断言全绿（布局/测算/Scene 发射/命中/复合组件）。
- 端到端：`tests/main-app.no` —— 组件树 → `cs-layout` → `cs-clear` + `cs-paint` → 逐条 Scene 送
  `_noui-draw-rect` / `_noui-font-draw`，事件经 `cs-hit` 置 hover / 改状态。窗口持续存活，ESC 退出。
- 复合组件（阶段8 后半）：`cs-is-container(kind)` 把「真容器」与「子项存在 `CHILD-IDX` 的复合组件」
  分开——后者（select / tab）若按普通容器纵向堆叠会把选项画成一屏列表。`cs-measure` / `cs-layout` /
  `cs-paint` / `cs-hit` 为 select 下拉、tab 条、dialog 面板、input 焦点描边、tooltip、
  skeleton / shimmer / spinner 各加了专门分支；**收起的 select 不参与布局与命中**（`cs-hit` 置
  `walk = 0`），避免残留边界被误点。

### 阶段 9 应用装配层（`src/app.no`，已验证）

把窗口 / 组件树 / 场景 / 事件收成一个底座，入口文件不再需要自己写 `#{c}` FFI 与绘制循环：

- 状态：`APP-BGC` / `APP-FONT` / `APP-ROOT` / `APP-QUIT` / `APP-HOVER`（一律 `[]i64` 承载可变状态），
  窗口尺寸 `APP-WIN-W/H = 560/420` 与定点 `APP-WIN-WF/HF = 56000/42000`。
- `app-init(title)`：`_noui-init` → `_noui-window-create` → `_noui-font-init`（Arial 16）→ `cmp-root()`。
- `app-frame()`：`begin_frame(底色)` → `app-draw-scene()`（Scene 逐条送 SDL，字体可用走
  `_noui-font-draw`，否则回退 `_noui-set-color` + `_noui-draw-text`）→ `end_frame`。
- `app-poll()`：取一个事件解码为 `(t, x, y)`；`t=1` 置 `APP-QUIT`。事件编码：低 8 位 type，
  中 24 位 x（按键事件为 keycode），高 24 位 y。
- `app-hover(root,px,py)`：`cs-hit` + `F-HOVERED` 切换（旧 hover 清除、新命中置上）。
- `app-should-quit()`：包 `_noui-should-quit`（ESC 置位）。
- 测试：`no run -cc ccwrap.sh tests/app-assemble.no` → 29 项断言全绿（尺寸 / `fx` / 颜色通道 /
  底色读写 / 退出初值 / 布局 / select 展开与反查 / hover / 命中 / slider 取值 / Scene 数量）。
- 端到端：`tests/main-form.no` —— 表单装配（输入框键盘输入、下拉展开选中、滑杆拖拽、checkbox /
  switch、提交弹窗），900 帧后自动收尾 `exit 0`；ESC / `q` / 关窗同样退出。

### 阶段 10 声明式视图层（`src/dv.no`，已验证）

在阶段 8–9 的**命令式内核之上**加一层声明式视图层，而不是换掉它：
组件对象 / 布局 / 绘制 / 命中仍是命令式的，dv 只负责「视图描述 → 组件池」的映射与增量同步。
公式 `UI = f(state)` —— 每帧由状态重建一棵**纯数据**视图树，再调和进活组件树。

- **为什么是数据驱动而不是闭包**：实测 Nolang 的 fn 值不可用 —— 全局 fn 值（`G Unary = double`）
  在 `EmitLLVM` 报 `unknown callee G`；局部 fn 值（`f Unary = double; a = f(5)`）直接
  `signal: trace/BPT trap`；参数表里内联匿名 fn 类型被 `anonymous-fn-type: false` 关掉。
  所以视图树只能是 SoA 数据 + keyed reconciler（这反而更声明式）。
- **视图节点池**：`DV-KIND` / `DV-KEY` / `DV-TEXT` / `DV-TEXT2` / `DV-COLOR` / `DV-VALUE` /
  `DV-VALUE2` / `DV-FLAGS` / `DV-PARENT`；调和状态 `DV-MATCH[view]=活句柄`、
  `DV-USED[活句柄]=帧号`（帧号不同即未认领）、`DV-FRAME`。
- **构造器**：整套命名构造器覆盖全部 83 个 kind —— 基础 `dv-label` / `dv-button` / `dv-badge` /
  `dv-tag` / `dv-icon` / `dv-kbd` / `dv-link` / `dv-separator` / `dv-avatar` / `dv-empty` /
  `dv-spinner` / `dv-marker`；表单 `dv-checkbox` / `dv-radio` / `dv-switch` / `dv-input` /
  `dv-textarea` / `dv-number-input` / `dv-otp-input` / `dv-search` / `dv-select` / `dv-combobox` /
  `dv-slider` / `dv-stepper` / `dv-color-picker` / `dv-date-picker` / `dv-questionnaire`；
  导航 `dv-tab` / `dv-breadcrumb` / `dv-sidebar` / `dv-menu` / `dv-pagination` / `dv-command` /
  `dv-history`；覆盖层与反馈 `dv-dialog` / `dv-popover` / `dv-tooltip` / `dv-hover-card` /
  `dv-notification` / `dv-alert` / `dv-message` / `dv-message-scroller` / `dv-sheet` /
  `dv-progress` / `dv-skeleton` / `dv-shimmer` / `dv-bubble` / `dv-speech` / `dv-inspector` /
  `dv-window-border` / `dv-native-menu` / `dv-attachment`；数据展示 `dv-table` / `dv-list` /
  `dv-virtual-list` / `dv-tree` / `dv-accordion` / `dv-carousel` / `dv-chart` / `dv-plot` /
  `dv-rating` / `dv-description-list` / `dv-collapsible` / `dv-diff` / `dv-highlighter` /
  `dv-clipboard` / `dv-scroll`；布局 `dv-resizable` / `dv-dock` / `dv-grid` / `dv-text-node` /
  `dv-theme` / `dv-sizing` / `dv-styled` / `dv-root` / `dv-window-ext`；其他
  `dv-searchable-list` / `dv-index-path`。另有 `dv-option` / `dv-tab-item`（语义化 label）与
  `dv-cond(key, cond, parent)` 条件节点（cond=0 返回 -1）。
- **视图字段 → 构造参数**：`text` = 标题 / 文案，`text2` = placeholder，`value` = 当前值 /
  列数 / 格数，`value2` = 上限 / 行数 / 总数 / 满分。`dv-apply-props` 只对「构造参数落在
  `VALUE2`」的 kind 同步 `value2`（slider 上限 / grid 行数 / table 行数 / pagination 总数 /
  rating 满分），其余 kind 的 `value2` 保持构造时默认。
- **flags 编码**：dv 自己的紧凑位（`DV-F-VIS/EN/CHK/SEL/ACT/OPEN` = 1/2/4/8/16/32），
  由 `dv-apply-flags` 映射到 CMP 的 `F-VISIBLE/ENABLED/CHECKED/SELECTED/ACTIVE/OPEN`；
  组合子 `dv-on` / `dv-on-chk` / `dv-on-sel` / `dv-on-act` / `dv-on-open`（函数内位或，数值稳定）。
- **`dv-create` 是全函数**：83 个 kind 各走自己的 `cmp-*` 构造器，未列出的 kind 落到
  `cmp-alloc` 兜底 —— 新增组件不必改视图层即可用 `dv-node(kind, key, …)` 表达。
- **调和器 `dv-sync(live, view)` 五遍**：① 按 `(key, kind)` 认领活组件（`dv-find-free`）→
  ② 删除本帧未认领的活子节点（`cmp-detach`）→ ③ 为未匹配的视图节点新建（`dv-create` 按 kind 分派构造）→
  ④ `dv-apply-props` 同步 props 并递归 → ⑤ 重排成视图顺序。`dv-render` 先 `dv-bump-frame` 再 `dv-sync`。
- **props 同步的取舍**：`dv-apply-props` 对 `C-INPUT` / `C-TEXTAREA` **不同步 text**（非受控），
  否则每帧把用户刚敲的字清掉；placeholder 走 `text2`。这正是 keyed 复用的意义 —— 输入缓冲与焦点跨帧保留。
- **行为也是数据（动作池）**：`DV-ACT[view] = 动作 id`、`DV-ACT2[view] = 载荷`。
  `dv-act(node, act, payload)` 把「点了意味着什么」挂到视图节点上，`dv-dispatch(key)` 一次取回
  `(act, payload)`，`dv-node-by-key` 做 key → 视图节点反查。动作 id 分两类：
  **交互类** `DV-A-CLICK=1` / `DV-A-CHANGE=2` / `DV-A-SUBMIT=3` / `DV-A-OPEN=4` /
  `DV-A-CLOSE=5`（载荷语义由应用层自定义）；**归约类** `DV-A-SET=6` / `DV-A-TOGGLE=7` /
  `DV-A-DELTA=8`（载荷 = 状态池下标，`value` / `value2` = 操作数）。
  **应用层因此不再写 `key` 的 if-else 链**：命中 → `cmp-key` → `dv-dispatch` → 按 `(act, payload)` 分派。
- **点击即归约（`dv-apply`）**：归约类动作把点击写成对状态池的一次赋值，
  `dv-apply(S, key)` 就是整个点击处理器，返回实际写入的槽数（`0` = 该节点不是归约动作）：
  `SET` → `S[payload] = value`；`TOGGLE` → `S[payload] = 1 - S[payload]`；
  `DELTA` → `S[payload] = dv-clamp(S[payload] + value, 0, value2)`（`value2 = 0` 表示不设上限）。
  视图的 `value` / `value2` 在动作侧是**操作数**，在 props 侧是**显示值**，两者互不干扰 ——
  这正是「视图节点同时承载外观与行为」的落点。归约器覆盖不了的（一次写多个槽、需要读组件几何）
  仍留命令式：demo 的 `reset` 是 `DV-A-CLICK` + 自定义载荷，滑杆拖动是 `DV-A-CHANGE` + `cs-rect`。
  归约命中后若还需动组件对象（例如 select 选中项后收起弹层），用
  `dv-parent-of-key` / `dv-parent-kind-of-key` 从命中 key 上溯视图父子关系，再 `dv-live-by-key` 取活句柄。
- **重复原语**：`dv-repeat(parent, key0, n, kind, …)`（同形状 n 份）、
  `dv-repeat-labels(parent, key0, texts)`（全量）、`dv-repeat-labels-n(parent, key0, texts, n)`
  （前 n 份；超长按长度、负数按 0）。key 从 `key0` 递增 → 跨帧稳定 → 列表长度变化只发差量。
- **布局也是数据（布局提示）**：`dv-lay(node, bits, gap, pad)` 把「怎么排」挂到视图节点上，
  随 props 同步进组件（`CMP-LAY` / `CMP-GAP` / `CMP-PAD`），由 `cs-measure` / `cs-layout` 消费。
  `bits` 复用 `components.no` 的 `L-H=1`（横向）/ `L-GROW=2`（吃掉主轴剩余）/
  `L-AE=4`（交叉轴末端）/ `L-AC=8`（交叉轴居中）；`gap` / `pad` 为 `0` 表示用默认
  `CS-GAP` / `CS-PAD`。组合子 `dv-lay-h` / `dv-lay-grow` / `dv-lay-end` / `dv-lay-center` /
  `dv-lay-h-grow` / `dv-lay-h-end` / `dv-lay-grow-end`（位或写在函数里 —— 顶层位或初始化有缺陷），
  便捷构造器 `dv-row(key, parent)`（= `C-GRID`）/ `dv-fill(key, parent)`（= `C-FORM` + `L-GROW`）。
  `cs-measure` / `cs-layout` 的容器分支据此分派：横向宽求和高取最大，纵向宽取最大高求和；
  `L-GROW` 子节点均分主轴剩余（`avail = 容器内容长 - Σ(非 grow 子节点长 + gap)`，
  `gw = avail / ngrow`）；`L-AE` / `L-AC` 只改交叉轴起点，不改主轴进度。
  **无提示时行为逐位不变**（三池默认 `0`、`cs-is-h` 等价于原来的 `kind == C-GRID`、
  `cs-gap-of` / `cs-pad-of` 回落默认、横向子节点保持 intrinsic 高并顶对齐），
  所以 `tests/cmp-scene.no` / `components-*.no` / `app-assemble.no` 零改动通过。
- **多大、字怎么对齐也是数据（尺寸 / 对齐提示）**：`dv-size(node, w, h)` 与
  `dv-align(node, al)` 把提示挂到视图节点上，随 props 同步进组件
  （`CMP-SW` / `CMP-SH` / `CMP-ALIGN`）。`cs-measure` 尾部与 `cs-layout` 头部消费尺寸：
  `0` = 自动（走 `cs-leaf-size` intrinsic），非 `0` 直接覆盖，**容器同样生效**
  （定宽侧栏 / 定高工具条）。`cs-paint-leaf` 消费对齐：`cs-align-of(h, kind)` 先读
  `CMP-ALIGN`，为 `0` 时按 kind 复刻旧默认（`label` / `text` / `link` / `icon` 左对齐，
  `button` / `badge` / `tag` / `kbd` / `marker` / `avatar` 居中），
  `cs-text-origin-x` 统一算落点（`link` 的下划线跟随实际字起点），
  文本类叶子一律走 `cs-emit-text-align`。**不写提示的节点渲染逐位不变**，
  所以老测试零改动通过。
- **跨层回查**：嵌套容器之后事件命中的 key 不再是 root 的直接子节点，
  `dv-live-deep(root, key)` 用显式队列做 BFS 反查活组件（语言没有可用的递归 fn 值，
  就不冒递归的险）；`dv-live-by-key` 仍用于同层查找。
- **主循环也声明式（阶段 11）**：`src/dv-app.no` 的 `dva-run(S, title, max-frames)` 把
  「轮询 → hover → 命中 → 焦点交接 → 键盘 → 归约 → 重建视图 → 调和 → 布局 → 上屏」
  整条链收成一个入口；应用只按名提供 `dv-app-view` / `dv-app-reduce` / `dv-app-click`
  三个回调（语言没有可用的 fn 值，只能按名约定）。计数器 `dva-frames` / `dva-ops` /
  `dva-clicks` 由装配层维护，不再占应用状态槽；焦点交接（`dva-focus`）与键盘喂字
  （`dva-keyboard`）也搬进了装配层。
- **样式也是数据（阶段 12：主题色板 + 变体）**：`src/cmp-scene.no` 的 `CS-TH` 九槽色板
  （`BG/FG/ACCENT/FIELD/TRACK/MUTED/TIP/OK/DANGER`）+ `cs-theme` / `cs-theme-set` /
  `cs-theme-reset`，`cs-paint` 的所有分支改读主题色；**默认值逐一复刻旧 `CS-*` 常量，
  不写主题时渲染逐位不变**。变体走 `CMP-VAR` 池（`cmp-var` / `cmp-set-var`）→
  `dv-variant(node, v)` 挂到视图节点 → `dv-apply-props` 每帧同步 → `cs-var-color`
  映射到主题语义色；优先级 **变体 > `cmp-color` > 主题 TRACK**。
  主题是全局的，不随每帧 `dv-clear()` 清空，`dva-run` 之前设一次即可；
  `dva-run` 启动时把 `CS-TH-BG` 接到 `app-set-bg`，所以窗口底色也是主题的一部分。
  命名注意：`dv-theme(key, parent)` 已被 `C-THEME` 组件构造器占用，设置器叫 `dv-theme-set`。
- **输入内容与快捷键也是数据（阶段 13：受控输入 + 全局键）**：阶段 10~12 之后只剩
  「输入框里写了什么」不是状态（组件自持文本缓冲，视图读不到、reset 清不掉）。
  `dv-bind(node, slot, max)` 把输入类组件绑到字符串状态池 `DV-BIND-T[slot]`，
  装配层每帧 `dva-sync-binds(root)` 双向同步：**组件文本变了 → 写回状态**（用户敲字），
  **状态被归约器 / reset 改了 → 推回组件**。靠 shadow `DVA-BIND-LAST` 分辨「谁改了谁」，
  否则两个方向会互相覆盖；比较必须用**未截断的原始文本**，否则「超长被截断后」的变化
  会和上次同步值相同而丢事件。`max` 是两个方向都生效的容量约束，截断结果写回状态。
  同步点在 `dv-render` 之后、`cs-layout` 之前（文本影响输入框宽度）。
  视图用 `dv-bound(slot)` 读、归约器用 `dv-bound-set(slot, s)` 写，
  所以 `UI = f(state)` 对文本也成立。**`dva-run` 签名不变** —— 字符串池住在 dv 侧，
  与主题一样是全局的，不随 `dv-clear()` 清空（状态不是视图的一部分）。
  `dv-hotkey(node, keycode, act, pay)` 声明全局键：命中后走与点击**完全相同**的
  归约路径（`dv-app-reduce` → `dv-app-click` → `dva-clicks()` 计数）。
  语义取「**只在没有输入焦点时生效**」：否则在输入框里打 'r' 会把页面重置。
  快捷键与 `dv-act` 同生命周期（每帧重挂）。
- **列表项也是数据（阶段 14：平行数组 → 列表项）**：`dv-repeat-labels-n` 只能重复同一种标签、
  `dv-repeat` 只能重复同一个节点，真实列表要的是「第 i 行的文案 / 动作 / 载荷都由状态数组决定」。
  `dv-repeat-items(parent, key0, n, kind, texts, values, acts, pays)` 把四个平行数组映射成
  n 个同 kind 节点：第 i 项文案 `texts[i]`、`value = values[i]`、动作 `acts[i]`（0 = 不挂）、
  载荷 `pays[i]`；数组短于 n 补默认，n 超长按 `texts` 长度、负数按 0。
  key 从 key0 递增 → 跨帧稳定，删一行 = 改一行文案 + 删一个尾节点（差量）。
  新增动作码 `DV-A-ITEM`（9，载荷 = 行号）是为了和 `reset` 占用的
  `DV-A-CLICK` + 自定义 payload 区分开 —— 同一个 `(act, pay)` 组合会撞车。
  配套的 `dv-rows-del(texts, n, i)` 是纯数组删除（返回新长度，越界原样返回），
  应用侧只需比较返回值决定要不要 `dva-set`。
- **模态弹层也是数据（阶段 15：叠加层节点 = 开关）**：命令式的 `main-form` 把对话框做成
  **另一棵根**（单独画、单独命中、`dialog-is-open` 开关）。声明式不复制这套 ——
  弹层就是同一棵树上的一个节点，`dv-on-overlay()` 标成叠加层，装配层 `dva-run` 一行不用改。
  视图位 `DV-F-OVL`（64）经 `dv-apply-flags` 落到活节点 `F-OVERLAY`（512）；
  场景侧 `cs-measure` / `cs-layout` 经 `cs-child-flow` 把叠加层排除出流布局，
  布局末尾改为在父矩形内**居中**；`cs-paint` 最后画（盖在最上面）；
  `cs-hit` 先命中叠加层，命中不了就返回 `-2` 向上传播（**点不穿**），且不再做自身矩形兜底。
  「开关」就是状态槽的 1/0：为 1 时视图建这个节点，为 0 时节点根本不存在
  （`dv-render` 会把它移出活树）—— 与 `dv-cond` 条件节点同一个思路。
  新增 `dv-overlay(node)` 事后标记 + `dv-on-overlay()` 组合子（`DV-F-VIS|EN|OPEN|OVL`，
  带 `OPEN` 是因为 `C-DIALOG` 的绘制分支认 `F-OPEN`）。
- **锚定浮层也是数据（阶段 16：叠加层 + 一个锚点 key）**：居中只够模态对话框，
  菜单 / popover / tooltip 要锚在触发节点旁边。`dv-overlay-anchor(node, anchor-key)`
  在叠加层之上多带一个 key（视图池 `DV-ANCHOR`，活池 `CMP-ANCHOR`），
  `cs-layout` 的叠加层分支用 `cs-find-key` 反查锚点矩形后定位：`x` 对齐锚点、
  `y = 锚点底 + CS-GAP`，下方超出父矩形就翻到锚点上方，最后两个轴夹紧在父矩形内；
  命中 / 绘制语义与阶段 15 完全一致（同样模态、点不穿）。
- **hover 也是状态，叠加层分两档（阶段 17）**：tooltip / hover-card / 通知不能像对话框
  那样吃掉点击。`dv-hover(node, slot)` 只声明「我被 hover 时写哪个状态槽」，
  装配层 `dva-sync-hover(S)` 每帧按最近一次布局的命中 key（`DVA-HIT`，在 `cs-layout`
  之后重算，所以布局变了也准）写 1/0，值没变不写；必须在建视图**之前**跑，否则慢一帧。
  叠加层因此有了模态 / 非模态两档：`F-PASS`（1024）/ `DV-F-PASS`（128）+
  `dv-pass` / `dv-on-tip()`，`cs-hit` 对非模态层照常命中但不竖 `-2` 屏障 ——
  没接住点击就继续走流子节点（点得穿）。tooltip 直接复用阶段 16 的 `dv-overlay-anchor`
  锚在触发节点上，所以「hover → 状态 → 条件建节点 → 锚定浮层」是一条完整的声明式链。
- **禁用态是派生的（阶段 18：少一个 `DV-F-EN`）**：`dv-disabled()` = `VIS`（保留可见），
  与 `dv-cond` 的分工是「在原地但点不动」对「根本不存在」。视图每帧读状态重建，
  所以「到上限 → 按钮禁用」这类派生不需要新机制、也不需要新状态槽。
  绘制层本来就消费 `F-DISABLED`（按钮 / 开关 / 输入框分支画灰）；阶段 18 补的是
  行为语义三处闸门：`dv-dispatch` 禁用节点不出动作、`dv-hot-dispatch` 目标禁用不触发、
  `dva-focus` 不把焦点交给禁用节点。**命中故意不拦** —— 禁用节点仍被 `cs-hit` 命中，
  hover / tooltip 照常，所以「禁用原因靠 tooltip 解释」能成立。
- **标签页是「当前页进状态」（阶段 19）**：`dv-tabs(parent, active, slot, labels, base)`
  把命令式的 `cmp-alloc(C-TAB)` + 逐个建标题 + `tab-select` 收敛成一个入口：
  建标题条（key = `base`）+ n 个标题（key = `base+1+i`，`value` = 页号，
  动作 `DV-A-SET` 载荷 = `slot`）+ 内容容器（key = `base+1+n`，返回它）。
  「当前页」就是状态槽，点标题走的还是 `dv-apply` 那条归约，主循环没有 key 的 if-else；
  选中态用 `dv-on-sel()` 表达（命中 / 绘制都认），另记 `DV-TAB-ACT[标题条]`，
  调和**末尾**再 `tab-select` 覆盖回 `CMP-VALUE`（`cs-paint-tab` 画高亮读它）。
  页面内容按 `active` 条件挂进返回的容器 —— 切页 = 换子树，容器与标题条同 key 复用。
- **列表项的 flags 也是数据（阶段 20）**：`dv-repeat-items`（阶段 14）把每项的
  flags 写死成 `dv-on()`，所以「哪几行选中 / 哪几行禁用」进不了 `UI = f(state)`。
  补 `dv-repeat-items-f(parent, key0, n, kind, texts, values, acts, pays, flags)`
  —— 多一个 flags 平行数组，第 i 项取 `flags[i]`，下标越界补 `dv-on()`；
  `dv-repeat-items(...)` 签名一字不改，内部转调 f 版。再补
  `dv-sel-flags(S, slot0, n)`：槽非 0 → `dv-on-sel()`，否则 `dv-on()`。
  于是声明式多选列表 = `dv-repeat-items-f(..., dv-sel-flags(DEMO, D-SEL0, 3))`，
  每项挂 `(DV-A-TOGGLE, 自己的槽)`，点一项翻自己的槽，下一帧视图重建 flags、
  活树选中态跟着走（`C-RADIO` 画 `F-SELECTED` 时用 `CS-TH-OK`）。
  全选 / 清空是「一次写 N 个槽」，归约器表达不了 → 走 `DV-A-CLICK` + 显式命令 id
  （和 reset 同一套路），主循环里仍然没有一行 key 的 if-else。
- **下拉的选项 / 选中 / 展开都是数据（阶段 21）**：命令式用法要 `cmp-select` +
  逐个 `select-add-option` + `select-open` / `select-close` + `select-choose` 四步；
  声明式层补 `dv-select-from(S, parent, key, selslot, openslot, labels, ph, base)`
  一行建出「`C-SELECT` 字段 + n 个常驻 `C-LABEL` 选项」：字段文案 =
  `labels[S[selslot]]`（未选 / 越界为空 → 显示占位符），字段挂
  `DV-A-TOGGLE(openslot)`，选项挂 `DV-A-CHOOSE(selslot)`。新开动作码
  `DV-A-CHOOSE = 10`，因为「选中并收起」要写两个槽（选中下标 + 清展开槽），
  单槽的 `SET` 表达不了；选项节点的 `value` / `value2` 正好是这两个操作数，
  所以视图里的动作数据就是全部语义，归约器不需要知道「这是下拉」。
  选项常驻（不条件挂载），收起时靠 `cs-hit` 的 `C-SELECT` 分支不可命中 ——
  展开 / 收起零结构操作，只改 flags 与文案。
- **时间也是状态（阶段 22：声明式通知）**：前面所有状态都是「应用写了什么就是什么」，
  通知是第一个**自己会变**的状态。`dv-toast-alive(S, slot, ttl)` 的槽里存的是
  **触发时的帧号**（不是布尔 —— 布尔只能表达「现在有没有」，帧号才能表达
  「还有多久」）：`t > 0 && dv-frame() < t + ttl` 才活着，0 = 没有通知，
  所以 `DV-FRAME[0]` 初值设为 1，让 0 成为干净的「从未触发」哨兵。
  `dv-toast(S, parent, key, slot, text, ttl, base)` 活着才建整棵子树
  （`dv-cond` 的思路）：`C-POPOVER` + `dv-on-tip()`（非模态叠加层，点得穿）
  + 文案 + 关闭按钮（`DV-A-SET` 把触发帧写回 0 = 立即过期）。
  触发按钮本身只是视图数据 —— `value = dv-frame()` + `DV-A-SET(触发槽)`，
  「何时触发」也是 `UI = f(state)`；过期不需要定时器也不需要事件，
  下一帧视图重建时判活原语自己说「没了」。本体用 `C-POPOVER` 而不是
  `C-NOTIFICATION`，因为后者不在 `cs-is-container` 白名单里。
- **组件侧新增支撑 API**（`src/components.no`）：`CMP-KEY` 池 + `cmp-key` / `cmp-set-key`（跨帧复用凭据）、
  `CMP-LAY` / `CMP-GAP` / `CMP-PAD` 池 + `cmp-lay` / `cmp-gap` / `cmp-pad` 与对应 setter（布局提示落点）、
  `CMP-SW` / `CMP-SH` / `CMP-ALIGN` 池 + `cmp-sw` / `cmp-sh` / `cmp-align` 与对应 setter（尺寸 / 对齐提示落点）、
  `CMP-VAR` 池 + `cmp-var` / `cmp-set-var`（变体落点）、
  `cmp-head`（子链表起始槽）、`cmp-detach`（摘除子组件）。
- **事件闭环**：`cs-hit` → `cmp-key` → `dv-dispatch` 取动作 → 改状态；要拿活组件对象时用
  `dv-live-by-key(live, key)`（视图 key → 活句柄，如滑杆拖动的几何换算、select 收起）。
  焦点管理不算动作：输入框的 `input-focus` 由应用层在点击时直接处理。
- 测试：`no run -cc ccwrap.sh tests/dv.no` → **65 项断言全绿**：
  A 新建 / 复用 / 改稿 / 删除 / 新增带序 / 纯重排；B 嵌套 group-box；C 条件节点开关；
  D select 选项子节点；E flags（checked / disabled）；F 动作挂载 + `dv-dispatch` + 改动作；
  G 重复（同长度零操作 / 增长只加差量 / 收缩只删差量 / `n=0` / 限量截断）；
  H 事件回路（dispatch → live-by-key → 改状态 → 重渲染 → 句柄不变且零结构操作）；
  I 83 个 kind 全部可声明式构造（逐个核对活树 kind / key）+ `DV-F-SEL/ACT/OPEN` 位映射 +
  未知 kind 走 `cmp-alloc` 兜底；J 命名构造器全目录（57 个）+ `VALUE`/`VALUE2` 语义抽查 +
  flags 组合子数值；K 归约器：SET / TOGGLE / DELTA 各一次写入、DELTA 到顶与到底截断、
  非归约节点与不存在的 key 返回 0、无关槽不被误写、TOGGLE 可逆，以及「改稿后旧视图节点被
  删掉、勾选框复用且未勾选」的零结构操作断言 + `dv-clamp` 边界。
  实测操作数 `A=2,0,1,1,0 B=3,0,1 C=1,3,2 D=3,0 E=1,0,0 F=2,0 G=3,0,2,4,0,2,5 H=2,0,0`；
  `I catalog=true flags-on=true flags-off=true fallback=true`、`J builders=true props=true flag-fns=true`、
  `K reduce=truetruetrue skip=true back=true view=true clamp=true`。
- 测试：`no run -cc ccwrap.sh tests/dv-layout.no` → **41 项断言全绿**：
  L 提示随 props 同步（`cmp-lay/gap/pad` 读回）+ 只改提示时结构零操作；
  M `C-FORM` + `L-H` 变横向（对照普通 `C-FORM` 的纵向换行）；N `gap` / `pad` 覆盖后的坐标；
  O `L-GROW` 吃掉主轴剩余（对照无提示时只拿 intrinsic 高，底边 = 容器底 - pad）；
  P 交叉轴 `L-AE` / `L-AC`（纵向容器靠右 / 居中）；Q 回归（`C-GRID` 仍横向、
  默认 `gap` / `pad` 读回 `0`）；R 真实两栏骨架（页头全宽 + 一行两列等宽 + 子节点不串栏）；
  S 固定尺寸（叶子 `30000x5000` / 容器 `20000x4000` / 未提示叶子仍 intrinsic /
  定宽容器内部不溢出）；T 文本对齐（右 / 左 / 中覆盖 + 未提示时复刻旧默认 +
  绘制层真实字起点）。
  实测 `R twocol=truetrue inside=truetrue header=true`，两栏各 `37600` 宽、间距 `1600`；
  `S size=truetruetrue rect=truetruetruetrue inside=true`、`T align=truetruetruetrue paint=truetrue`。
- 端到端：`tests/main-dv.no` —— 主循环只有「状态初值 + `dva-run(DEMO, title, 900)` + 打印」
  三行；点击段收缩进 `dv-app-click` 回调：`dv-apply` 归约状态 → 命中后做一次结构相关的
  组件侧动作（命中节点父节点是 `C-SELECT` 时收起弹层，用 `dv-live-deep` 跨两栏回查活句柄）。
  `dv-app-view()` 每帧重建视图（页头全宽：`dv-align` 居中的标题 / 输入框 / 两行状态摘要；
  下面 `dv-row` + `dv-size` 定宽左栏 + `dv-fill` 右栏吃满剩余宽度，左栏交互控件、
  右栏「组件目录速写」；含 `dv-repeat-items` 列表与全部 `dv-act`），`dva-run` 内
  `dv-render` 调和；900 帧打印 `bye frames=900 ops=40 clicks=0 rows=2`
  （首帧建 40 个节点，之后零结构操作），`exit 0`，连跑两次输出一致。
  demo 里绝大部分交互已是归约动作，只剩 `reset`（一次写 6 个槽）与滑杆几何换算留在命令式。
  视图声明的动作数据（`tests/dv.no` 场景 K 覆盖其语义）：
  「add row」`key=16 act=DV-A-DELTA(8) pay=D-LIST(8) value=1 value2=3`、
  勾选框 `key=9 act=DV-A-TOGGLE(7) pay=D-AGREE(0)`、
  下拉选项 `key=5/6/7 act=DV-A-SET(6) pay=D-CHOICE(3) value=0/1/2`。
- 声明式弹层（阶段 15 + 16，`tests/dva-overlay.no` / `tests/dva-anchor.no`）：
  `dv-on-overlay()` 把节点标成叠加层（`DV-F-OVL` → 活池 `F-OVERLAY`），
  不参与流布局、在父矩形内居中、命中先于其它子节点、绘制最后画；
  `dv-overlay-anchor(node, key)` 再带一个锚点 key（`DV-ANCHOR` → `CMP-ANCHOR`），
  `cs-layout` 叠加层分支用 `cs-find-key` 反查锚点矩形：`x` 对齐锚点、`y = 锚点底 + CS-GAP`，
  下方放不下就翻到锚点上方，两轴最后夹紧在父矩形内，锚点找不到就保持居中。
  实测 dva-overlay `U..Y` 全 true（含 `cs-hit` 点不穿、`dv-live-deep` 查不到已关节点）、
  dva-anchor `Z..P` 全 true（18 项断言，含贴边翻转与左右夹紧、对照组无叠加层时同点能命中盒子）。
- 声明式 hover + 非模态叠加层（阶段 17，`tests/dva-hover.no`）：
  `dv-hover(node, slot)` 让 hover 进状态，`dv-on-tip()` 给非模态叠加层
  （仍最后画、自身可命中、没接住点击就继续找下面的兄弟 = 点得穿），
  tooltip 复用 `dv-overlay-anchor` 锚在触发节点上。
  实测 dva-hover `Z..T` 全 true（23 项断言，含「同 key 第二帧降级要清掉 F-PASS」、
  `-1` / `-2` 都算没命中、对照模态树同点 `-2`、对照无浮层树命中盒子、
  hover 槽 1 → tooltip 出现且锚点正确 / 槽 0 → 消失）。
  装配层侧另做了探针验证：把 `D-TIP` 初值写 1 后 `ops` 40 → 42
  （首帧多建 tooltip = +1，第二帧 `dva-sync-hover` 因无鼠标事件把槽写回 0 删掉它 = +1），
  证明「hover 槽 -> 视图分支 -> 节点增删」整条链在真实主循环里成立。
- 声明式禁用态（阶段 18，`tests/dva-disabled.no`）：
  `dv-disabled()` = `VIS`（少一个 `DV-F-EN`），`dv-enabled()` = `VIS|EN`，
  `dv-disabled-of(v)` 读回；`dv-dispatch` / `dv-hot-dispatch` / `dva-focus`
  三处闸门尊重禁用，`cs-hit` 故意不拦（hover / tooltip 照常）。
  实测 dva-disabled `Z..V` 全 true（23 项断言，含「同 key 第二帧改回启用要清掉
  `F-DISABLED`」、`-1` 命中仍有效、以及 demo 那个「按状态选 flags」写法本身）。
  demo 侧：`nl >= DEMO-MAX-ROWS` 时 `add row` 禁用、`nl <= 0` 时 `del row` 禁用，
  并给禁用的 `add row` 挂 `dv-hover(b16, D-TIP2)` —— 鼠标停在禁用的按钮上仍会
  浮出「at the row cap」，`ops` 仍 40（禁用不改变节点数）。
- 声明式标签页（阶段 19，`tests/dva-tabs.no`）：
  `dv-tabs(parent, active, slot, labels, base)` 建「标题条 + n 标题 + 内容容器」，
  标题挂 `DV-A-SET`（载荷 = `slot`，值 = 页号），选中态用 `dv-on-sel()`，
  当前页另记 `DV-TAB-ACT[标题条]` 并在调和末尾 `tab-select` 回写 `CMP-VALUE`。
  实测 dva-tabs `Z..E` 全 true（27 项断言，含换页 `ops == 0`、越界 `active` 收敛到
  最后一页、`CS-TH-ACCENT` 全场景只此一处、以及 `cs-hit` → `cmp-key` →
  `dv-dispatch` → `dv-apply` → 重建的真实命中链路）。
  demo 侧：右栏底部一条三页标签（`tab=` 回显），首帧 `ops` 40 → 47
  （标题条 + 3 标题 + 容器 + 2 个页面内容节点），reset 回到第 0 页。
- 声明式多选列表（阶段 20，`tests/dva-sel.no`）：
  `dv-repeat-items-f`（阶段 14 原语 + flags 平行数组）+ `dv-sel-flags`
  （状态槽非 0 → `dv-on-sel()`）把「哪些行选中 / 禁用」变成状态的函数。
  实测 dva-sel `Z..E` 全 true（28 项断言）：短数组 / 空数组补 `dv-on()`、
  `dv-repeat-items` 旧签名回归、n 收敛；活节点 `F-SELECTED` / `F-DISABLED`
  随 flags 走且换帧 `ops == 0`；禁用项派发不出动作；点第 i 项 = `TOGGLE` + 自己的槽；
  真实命中链路 `cs-hit` → `cmp-key` → `dv-dispatch` → `dv-apply` → 重建后
  活树选中态跟着状态走，且绘制层 `CS-TH-OK` 计数随选中数变化。
  demo 侧：右栏顶部三行多选（`sel=` 回显）+ `all` / `none` 幽灵按钮
  （`DV-A-CLICK` + `DEMO-CMD-ALL` / `DEMO-CMD-NONE`，命令分支一次写 3 个槽），
  首帧 `ops` 47 → 52（3 个列表项 + 2 个按钮），reset 一并清空选中位。
- 声明式下拉（阶段 21，`tests/dva-select.no`）：
  `dv-select-from(S, parent, key, selslot, openslot, labels, ph, base)` 建
  「字段 + n 个常驻选项」，字段挂 `DV-A-TOGGLE(openslot)`、选项挂
  `DV-A-CHOOSE(selslot)`；`dv-apply` 的 CHOOSE 分支一次写两个槽。
  实测 dva-select `Z..E` 全 true（32 项断言）：字段 / 选项的 key、文案、
  `value` = 下标、`value2` = 展开槽、动作码与载荷；`ops == 4`（根节点不计）
  且换选中 / 展开只改 flags 与文案（`ops == 0`）；`select-option-count` /
  `select-selected` / 选项文案跟随；`dv-apply` 写 2 个槽、`value2 < 0` 时写 1 个；
  真实命中链路 `cs-hit` → `cmp-key` → `dv-dispatch` → `dv-apply` → 重建后
  展开时可命中选项、选中并收起、绘制层 `CS-TH-ACCENT` 计数更高、
  收起后同一坐标不再命中选项。
  demo 侧：左栏下拉换成这一行（原手写 `C-SELECT` + 3 选项 + `DV-A-OPEN` /
  `DV-A-SET`，事件侧两段 select 分支一并删掉），右栏单选组文案改从
  `DEMO-OPTS` 读（`dv-demo-opt(i)`），reset 多写 `D-SOPEN`，页头回显 `pick=`；
  首帧 `ops` 仍是 52（同样 4 个节点），`bye … pick=0`、`exit 0`（连跑两次一致）。
- 声明式通知（阶段 22，`tests/dva-toast.no`）：
  `dv-toast-alive(S, slot, ttl)`（槽存触发帧，0 = 没有通知）+ `dv-toast(…)`
  （活着才建 `C-POPOVER` + `dv-on-tip()` + 文案 + 关闭按钮）。
  实测 dva-toast `Z..E` 全 true（31 项断言）：alive 边界（0 槽 / 界内 / 界外 /
  `ttl=0` / 越界槽）、死时不建节点、活时 kind / key / flags（`DV-F-PASS` +
  `DV-F-OVL`）/ 子节点数 / 关闭按钮动作；活 `ops == 3`、关闭 `ops == 1`
  （`dv-sync` 只数被摘掉的**直接**子节点，整棵子树随本体一起走）；
  活节点 `F-OVERLAY` / `F-PASS`、`cmp-children == 2`；点关闭 = `DV-A-SET` +
  触发槽、`dv-apply` 写 1 个槽并清零、toast 本体无动作；真实命中链路
  `cs-hit` → `cmp-key` → `dv-dispatch` → `dv-apply` → 重建后消失；
  ttl 边界逐帧（400/401/402 活 `ops == 0`、403 起 `ops == 1` 且消失）；
  绘制层文案随存活出现 / 消失；触发按钮 `value = dv-frame()` + `DV-A-SET`
  接线与 main-dv 一致（含触发后同一帧视图里就有通知）。
  demo 侧：左栏加 `notify` 按钮、视图里 `dv-toast(DEMO, page, 78, D-TOAST,
  'saved', DEMO-TOAST-TTL, 79)`，reset 多写 `D-TOAST`，页头回显 `tst=`；
  首帧 `ops` 52 → 53（多一个触发按钮；通知默认不活所以 toast 子树不计），
  `bye … tst=0`、`exit 0`。

### §5.3 阶段 8–9 新坑（2026-11-20 实测）

- **`f64.to-i64()` 不是 option**：它是 `#{buildin}`（`std/number.no`），**直接返回 i64**。
  按 option 套 match（`r = (p*100.0).to-i64(): { ok -> it  -> 0 }`）会恒走 else 分支，
  `fx()` 曾因此恒返回 0（鼠标坐标全 0）。正确写法：`m = p * 100.0` 然后 `v = m.to-i64()`。
- **小写顶层 str 全局不能进字符串插值**：`print('{bar}')`（`bar` 为小写顶层 str）报
  `unresolved format field`；小写顶层标量被函数读取则报四档命名错。**函数要引用的数据一律大写**
  （`APP-*` / `FORM-*` / `CS-*`），只有纯 main 局部才用小写。
- **顶层绑定一次定型**：名字首次绑定后类型就固定。把已绑定为 str 的名字再赋 i64 会静默保持 str，
  直到 MIR 报 `aggregate type 'str' passed to i64 param`。改名最省事（`t1` → `tb0`）。
- **互斥分支必须写成 if-group**：`(c1) -> {}` 与 `(c2) -> {}` 是两条独立 guard，会各自触发
  （曾导致容器被画两次、Scene 数量翻倍）。互斥语义写成 `{ c1 -> {} c2 -> {} -> {} }`。
- **`return` 在函数中间不生效**：实测 `return` 之后的语句照常执行。提前退出用 guard 包裹整个主体，
  或循环里用 `**` 打断 `!!`。
- **`select-add-option` / `tab-add` 返回下标不是句柄**：句柄要用 `select-option(h, i)`；
  反查用 `select-index-of(h, handle)`（把命中测试拿到的选项句柄翻译回 `select-choose` 的下标）。
- **`select-choose` 会清 `F-OPEN`**：选完即收起；要维持展开需再 `select-open`。
- **`cmp-alloc` 默认 `F-VISIBLE | F-ENABLED`**：tooltip / 弹窗默认是「可见」的，隐藏要显式
  `tooltip-hide` / `dialog-close`。
- **顶层 `+` / `-` 赋给已定型变量是硬错误**：`form-frames = form-frames + 1` 报 15w45dqk，
  改用 `cs-inc(form-frames)` / `cs-sub(k, 48)` 等已解包的 helper。

### 编译器语义迁移：safe-index downgrade（2026-11-20）

`bin/no` 更新后索引读取返回**带类型 option**（`ARR[i]` → `?i64` / `?str`），
`+` / `-` / `*` / `/` 也返回溢出检查后的 `?i64`，裸赋给 i64/str 变量是硬错误（15w45dqk）。
统一写法是**两步 match 解包**，且 else 分支写作 `-> `（不是 `err -> `）、分支必须换行：

```nolang
x = ARR[i]: {
    ok -> it

    -> 0
}
v = x
```

注意 §5.2 的「match default arm 里再读数组下标 ⇒ 结果值算错」仍然成立：
默认臂里需要旧值时先落局部变量，不要把下标读取写进 match 表达式或 arms。
`src/components.no` / `src/cmp-scene.no` 与全部维护中测试已按此迁移。

### §5.4 阶段 10 声明式层新坑（2026-11-20 实测）

- **🔴 顶层（main 作用域）变量用位或表达式初始化会取到错值，且不确定**：
  `f-on = DV-F-VIS | DV-F-EN | DV-F-CHK` 实测同一份二进制多次运行分别得到 `1` / `5` / `7`
  （`pw(1|2|4)` 内联调用始终为 7，函数内 `z = DV-F-VIS | DV-F-EN | DV-F-CHK` 也始终为 7）。
  这是 `tests/dv.no` 之前**时好时坏**的根因（表现是 `E flags=truetruefalsetrue`）。
  规避：**flags 位组合直接写字面量**（`f-on = 7`），或放进函数里算（`dv-on` / `dv-on-chk`）。
  多个「顶层位或变量」并存时更会直接 codegen 失败（`or void undef, undef`）。
- **`CHILD-IDX` 是全局扁平数组，删子节点必须整体压实**：每个父组件只有
  `CMP-HEAD`（起始槽）+ `CMP-COUNT`。`cmp-detach` 删掉一槽后，**该槽之后的所有元素**
  （包括别的父的子链）都要左移一格并 `truncate`，再把所有 `head > 被删槽` 的父组件 `head--`。
  只移父组件自己那段会串位（实测：从 root 删掉一个子后，第二个容器的子节点读成了 `d,f`）。
- **数组下标写入只能用 place-match**：`a[i] = v` 对**数组参数**是硬错误
  （`cannot assign through an index directly`）。写法是 `a[i]: { ok -> it = v \n -> _ }`。
  模块全局数组的直接写入（`CMP-FLAGS[h] = ...`）合法。
- **哨兵值必须与「真句柄 0」区分**：`DV-MATCH` 用 `-1` 表示未匹配。`dv-arr-get` 默认返回 `0`
  会把「空洞」读成「匹配到句柄 0」，于是跳过新建、把 root 认成自己的子节点。
  因此另给 `dv-arr-getm`（默认 `-1`），且 `dv-ensure` 补位也补 `-1` 而不是 `0`。
- **局部变量名必须小写**：`L` / `N` / `Ldel` 会触发 lint `2dhoris2`（'should use only lowercase
  letters and hyphens'）。Nolang 的局部是函数作用域，同一函数内重复声明同名（哪怕在不同循环里）
  也是 `duplicate variable`。
- **`**` 只在 `!!` 循环内合法**（break）；循环外的一次性分支用 `(cond) -> { }` 嵌套，
  或 `{ a -> {} b -> {} -> {} }` if-group。
- **`return` 在函数中间不生效**，`dv-create` 之类的分派用 if-group 而非提前 return。
- **`dv-act` 与 `dv-node` 同生命周期，必须每帧重挂**：`dv-clear()` 会 `truncate` 动作池，
  而视图是每帧重建的。只在第一帧挂过一次动作的写法，第二帧 `dv-dispatch` 就返回 `(0,0)`
  （`tests/dv.no` 场景 H 的注释里固定了这条）。动作载荷因此可以携带状态 —— 挂载就是「视图描述」的一部分。
- **循环体内赋值的局部进不了字符串插值**：`!!` 循环里 `fr = ...` 之后 `print('fr={fr}')` 报
  `unresolved format field fr`。要么把打印放进独立函数（函数局部可插值），要么先落 main 顶层局部。
- **`cs-hit` 未命中返回 -1，`cmp-key(-1)` 安全**（内部对越界下标走 match else 返回 -1），
  所以 `k = cmp-key(hit)` 不必先判 `hit >= 0`；`dv-dispatch(-1)` 也随之返回 `(0,0)`。
- **DV flags 与 CMP flags 是两套编号，别混用**：dv 侧是紧凑位
  （`DV-F-VIS/EN/CHK/SEL/ACT/OPEN` = 1/2/4/8/16/32），CMP 侧是
  `F-VISIBLE=1 / F-ENABLED=2 / F-FOCUSED=4 / F-HOVERED=8 / F-CHECKED=16 / F-OPEN=32 / …`。
  两套只在前两位重合，`DV-F-CHK=4` 直接喂给 `cmp-flag-set` 会置成 `F-FOCUSED`。
  必须经 `dv-apply-flags` 转译。
- **`dv-apply-props` 只同步「构造参数落在 VALUE2」的 kind 的 value2**：slider 上限 /
  grid 行数 / table 行数 / pagination 总数 / rating 满分。给其余 kind 写 `value2` 会被丢弃
  （视图字段冗余但无害）；要改这些组件的该属性请走事件里的 `cmp-set-value2`。
- **命名构造器与 getter 重名会撞车**：`dv-text` 已被 getter 占用，纯文本节点的构造器叫
  `dv-text-node`。新增构造器前先 `grep "^dv-"`。
- **归约动作的 `payload` 是状态池下标，不是命令 id**：`DV-A-SET/TOGGLE/DELTA` 的 `payload`
  指向 `S[payload]`，`value` / `value2` 是操作数。把它当命令 id 用会写到无关槽（场景 K 专门
  留了一个「无关槽」验证不误写）。`dv-apply` 对「非归约动作」和「不存在的 key」都返回 `0`，
  应用层用返回值区分「归约命中」与「需要另作处理」，不要靠 `payload` 是否为 0 判断。
- **`dv-apply` 写的是数组参数，只能 place-match**：状态池一律以 `S []i64` 参数传入，
  函数内 `S[i] = v` 是硬错误，必须 `S[i]: { ok -> it = v \n -> _ }`（同 §5.4 的索引写入坑）。
  读旧值同理走 match，默认臂给 `0`。
- **改稿后视图节点可能整棵消失，断言要按「差量」写**：场景 K 里第二次视图只有 1 个节点
  （第一次 5 个），调和器正确发出 4 次删除。写测试时别假设「只改属性 ⇒ 零操作」，
  先想清楚这一帧的视图树与上一帧差几个节点。
- **`dv-lay` 与 `dv-act` 同生命周期，必须每帧重挂**：`dv-clear()` 会 `truncate`
  `DV-LAY` / `DV-GAP` / `DV-PAD`（连同 `dv-ensure` 一起清空）。只在第一帧挂过提示的写法，
  第二帧起布局退回默认（纵向堆叠），而且**不会报错** —— 只是界面悄悄变回一栏。
- **`dv-lay` 的 `gap` / `pad` 是「0 = 用默认」的覆盖值，不是绝对值**：写 `0` 时
  `cs-gap-of` / `cs-pad-of` 回落到 `CS-GAP` / `CS-PAD`。所以想「无间距」必须写一个真实小值
  （如 `1`），写 `0` 只会拿到默认的 `800` / `1200`。`bits` 的 `0` 同理 = 无提示。
- **布局位组合要在函数里算**：`dv-lay-h-end()` 之类的组合子内部做位或（数值稳定）；
  别在 main 顶层写 `dv-lay(x, L-H | L-AE, 0, 0)` —— 顶层位或初始化有 §5.4 记录的缺陷。
- **两栏 / 嵌套容器之后，`dv-live-by-key(root, key)` 会返回 -1**：它只查 `root` 的直接子节点。
  事件侧要拿活组件请用 `dv-live-deep(root, key)`（BFS）。忘了换会让「select 选中后收起」
  「滑杆几何换算」这类分支静默失效（有 `(live >= 0)` 守卫，不崩但也不动）。
- **`L-GROW` 只在容器有主轴剩余时才拉伸**：`cs-measure` 仍按 intrinsic 尺寸累加子节点，
  `L-GROW` 分的是「容器内容长 − Σ(非 grow 子节点 + gap)」。容器本身是按内容高度给定时
  （例如窗口高度经页头消耗后剩多少就是多少），grow 子节点不会把容器撑高。
  想验证 grow，要么给容器显式高度（测试里 `cs-layout(root, …, 100000)`），
  要么让它的父容器带 `L-GROW`。
- **`dv-size` / `dv-align` 与 `dv-lay` 同生命周期，也必须每帧重挂**：`dv-clear()` 会
  `truncate` `DV-SW` / `DV-SH` / `DV-ALIGN`。漏挂同样不报错 —— 只是尺寸退回 intrinsic、
  字退回 kind 默认对齐。
- **`dv-size` 是「覆盖」不是「最小值」**：非 `0` 直接采用，`cs-measure` 与 `cs-layout` 两处
  都生效，所以父容器分配到的宽 / 高会被强制改掉，**父容器的进度仍按覆盖后的值推进**
  （横向：`cw` 用 `cs-measure` 的返回值，所以定宽侧栏的兄弟会紧挨着它排）。
  想「最小宽度」得自己在视图侧算。
- **`dv-size` 与 `L-GROW` 同时挂在一个横向子节点上会打架**：grow 先按剩余均分算出 `cw`，
  `cs-layout` 再强制成固定尺寸，但兄弟的起点已经按均分结果推进了 —— 中间会留一段空白。
  定宽那一侧请用 `dv-form`（不带 `L-GROW`），只让另一侧 `dv-fill`。
- **`dv-align` 只影响文本类叶子**：`checkbox` / `radio` / `switch` 的文案前面有图形块、
  输入类组件走 `cs-paint-field`，都不经过 `cs-emit-text-align`，给它们写 `dv-align` 无效果。
  `cs-align-of` 的默认值表必须与 `cs-paint-leaf` 里各 kind 的既有对齐方式一致，
  改动默认表等于改渲染行为。

### §5.5 阶段 11 装配层新坑（2026-11-20 实测）

- **回调只能按名调用，且定义在「使用方」文件里**：Nolang 没有可用的 fn 值（全局 fn 值
  codegen 失败、局部 fn 值崩溃），所以 `dva-run` 通过约定名 `dv-app-view` /
  `dv-app-reduce` / `dv-app-click` 回调。定义必须写在 `import` 了 `dv-app` 的那个文件里
  （跨文件向前解析已实测通过：`src/dv-app.no` 调用 `tests/main-dv.no` 里的同名函数）。
  漏定义是编译错误，不是运行时静默。
- **状态池写入必须 place-match**：`dva-run` 把状态池当 `S []i64` 参数传给回调，数组参数的
  下标写入是硬错误，统一走 `dva-set(S, i, v)`（内部 `S[i]: { ok -> it = v \n -> _ }`）。
  同理 `dv-apply` 内部也是 place-match。
- **回调里的 `dva-root()` / `dva-px()` / `dva-py()` 是装配层状态**：root 由 `app-init` 建好，
  指针由 `dva-run` 在每次 move / click 分支更新。回调里读到的坐标就是本次命中的坐标，
  不要在回调里再换算一遍窗口坐标。
- **`dva-focus` 只交接输入类组件**：点非输入类会释放上一个输入焦点（「点别处失焦」），
  但组件自身的 hover / pressed 态仍由 `cs-hit` 链负责，不要在回调里重复实现。
- **`dva-keyboard` 只认退格 / 空格 / 数字 / 小写字母**：其它键（含 ESC）在装配层被忽略，
  `q` 是退出键。要扩展字符集改 `DVA-AZ` / `DVA-DIG` 与 `dva-keyboard` 的区间。
- **帧数上限是「防挂死」不是「退出事件」**：`max-frames` 到点后 `dva-run` 直接收尾
  （释放焦点 + `app-shutdown`），不产生退出事件。要「跑够 N 帧后做点事」得在回调里读
  `dva-frames()`。
- **`dva-ops()` / `dva-clicks()` 是累计值，不是本帧值**：每帧把 `dv-render` 的返回值累加进
  `DVA-OPS`。要本帧差量得在回调里取前后相减。

### §5.6 阶段 12 主题 / 变体层新坑（2026-11-20 实测）

- **🔴 被导入模块的「顶层下标写入」不会执行**：`src/cmp-scene.no` 里写
  `CS-TH[0] = 0x1E1E28FF`（顶层、模块作用域）时，只有 `with-len(9)` 生效，
  9 行赋值**全部被跳过**（读回来全是 0；对照实验：`cs-theme-set` 的
  `slot < CS-TH.len()` 守卫通过，证明 `len()` 那行跑了）。main 作用域的下标写入才执行。
  规避：**延迟初始化** —— `CS-TH-INIT []i64 = [0]` + `cs-theme-init()`，
  在 `cs-theme` / `cs-theme-set` / `cs-theme-reset` 开头检查一次标志位再铺默认值。
  这条对任何「导入模块里想预置全局池内容」的写法都成立。
- **`dv-theme` 名字已被占用**：`dv-theme(key, parent)` 是 `C-THEME` 组件构造器，
  再定义 `dv-theme(slot, color)` 直接 `duplicate function definition`。
  主题设置器改名 `dv-theme-set`，复位叫 `dv-theme-reset`。
- **主题色板默认值必须逐位复刻旧常量**：`cs-paint` 的每个分支都改成读主题后，
  「不写主题」的渲染结果必须和改动前逐位相同，否则老测试（`tests/dv.no` /
  `tests/cmp-scene.no` / `components-*.no`）会集体变红。
  做法是让 9 个槽位的默认值与旧 `CS-*` 常量一一对应，再用 `tests/dv-style.no`
  的 Y 场景把「默认值 == 旧常量」钉住。
- **变体与 `dv-act` 同生命周期**：`DV-VAR` 被 `dv-clear()` 截断，视图每帧重建，
  所以 `dv-variant` 必须和 `dv-act` 一样每帧重挂；`dv-apply-props` 负责同步进活组件池。
- **主题是全局的，不在 `dv-clear()` 生命周期内**：`CS-TH` 不随视图重建清空，
  在 `dva-run` 之前设一次即可。要「每帧换主题」也可以（`cs-paint` 每帧读），
  但没有必要。
- **优先级链条**：`cs-var-color`（变体）> `cmp-color`（显式颜色）> 主题 TRACK（默认）。
  写反了会出现「自定色被变体顶掉」或「变体不生效」两类难查的色差。

### §5.7 阶段 13 受控输入 / 快捷键新坑（2026-11-20 实测）

- **字符串状态池放哪一层：放 dv 侧，不要加进 `dva-run` 签名**。最自然的设计是
  `dva-run-s(S, T, title, max)`，但这会让 `dva-run` 变成封装、老应用也要改；
  而且 Nolang 的数组参数无法稳妥地存进模块全局（聚合类型不能传给标量参数，
  实测 `dva_str_get(T, slot)` 直接 `EmitLLVM` 报错）。把 `DV-BIND-T` 放在 dv 侧，
  `dva-run` 签名一字不改，`dva-str-get` / `dva-str-set` 退化成 `dv-bound` /
  `dv-bound-set` 的薄封装。
- **shadow 比较必须用未截断的原始文本**：如果先 `cap` 再和 shadow 比，
  那么「组件文本从 `abc` 变成 `abcd`（max=3）」会截成 `abc` == shadow `abc`，
  变化被吃掉、组件里的超长文本永远回写不了状态。先比原始值，再截断、再写回两边。
- **shadow 与状态是两个数组，别混用**：`DVA-BIND-LAST` 是「上次同步值」，
  `DV-BIND-T` 是「归约目标」。在 `raw == last` 分支里读 `T` 之前要先确认
  用的确实是状态池而不是 shadow（写成 `dva-str-get(DVA-BIND-LAST, slot)`
  会直接编译失败：聚合传给标量参数）。
- **绑定只对输入类 kind 生效**：`dva-is-input-kind` 判定后再同步，
  绑在 label / button 上不会报错也不会生效（`tests/dva-bind.no` 场景 AA 钉住）。
- **`dv-bind` / `dv-hotkey` 与 `dv-act` 同生命周期**：`dv-clear()` 截断 `DV-BIND`，
  视图每帧重建时必须重挂；漏挂的表现是「第一次能用、之后输入框不再回写状态」。
- **快捷键只在无输入焦点时生效**：`dva-run` 的键盘分支先读 `DVA-FOCUS[0]`，
  有焦点走 `dva-keyboard`，无焦点才 `dv-hot-dispatch`。
  忘了这个条件会让输入框打不出快捷键字母（实测 'r' 会把页面重置）。
- **`dv-hot-dispatch` 未命中返回 `act = 0`**，与 `dv-dispatch` 的「无动作」一致；
  调用方用 `act != 0` 判断，不要靠 `vkey` 是否为 -1。

### §5.8 阶段 14 列表项新坑（2026-11-20 实测）

- **数组参数不能直接下标写**：`dv-rows-del` 起初写 `texts[j] = nx`，运行时靠
  safe-index downgrade 跑通了，但 `no vet` 直接报
  `cannot assign through an index directly`（索引读返回 option，直写会把越界变成未检查的越界写）。
  正确写法是 place-match：`texts[j]: { ok -> it = nx \n\n -> _ }`。
  注意这跟「模块全局下标写在函数内合法」不冲突 —— 那是全局，这是**参数**。
- **`DV-A-ITEM` 不能复用 `DV-A-CLICK`**：demo 的 `reset` 按钮是
  `dv-act(b12, DV-A-CLICK, DEMO-CMD-RESET)`（pay=1），如果列表项也用
  `(DV-A-CLICK, 行号)`，点 reset 会顺带删掉第 1 行。语义不同的动作要给它自己的 act 码。
- **删一行只需要「改文案 + 删尾节点」**：key 从 key0 递增且跨帧稳定，所以
  `dv-rows-del` 前移数组后重建视图，`dv-render` 只会更新被移动那几项的 props 并删掉多余尾节点，
  不会整树重建（`tests/dva-repeat.no` 场景 S 钉住 `cmp-children` 从 3 变 2）。
- **平行数组在 main 模块顶层初始化是安全的**：`DEMO-ROW-ACTS[i] = 9` 这类字面量下标写
  在 main 模块顶层会执行（`DEMO-ROWS[i] = '...'` 早就这么用）；但**被导入模块**的顶层下标写
  不会执行（阶段 12 踩过），所以库侧不要这么干。

### §5.9 阶段 15 叠加层新坑（2026-11-20 实测）

- **视图池与活组件池是两套下标**：`dv-overlay(h)` 起初写 `cmp-flag-set(h, F-OVERLAY)`，
  但 `h` 是**视图**句柄（`DV-*` 池），`cmp-flag-set` 写的是 `CMP-FLAGS`（活池）——
  等于把视图下标当成组件下标乱写，而且下一帧就被 props 同步覆盖。
  正确做法是写 `DV-FLAGS`（`dv-arr-set(DV-FLAGS, h, dv-flags(h) | DV-F-OVL)`），
  让 `dv-apply-flags` 在调和时落到活节点。**库侧也不要绕过视图直接
  `cmp-flag-set(live, F-OVERLAY)`**：`dv-render` 不清它，节点复用时会残留。
- **「模态挡住」会被自身矩形兜底吃掉**：`cs-hit` 原本以 `(found < 0) -> { 命中自身矩形 }`
  收尾，而根容器（C-ROOT）铺满整个窗口 —— 叠加层没命中时，冒泡到根会被根自己的矩形接住，
  于是「点不穿」失效。引入 `-2` 哨兵（= 被模态挡住）向上传播，并把兜底条件改成
  `(found == -1)`。`-2` 对上层无害：`cmp-key(-2)` = `-1`，`dv-dispatch(-1)` = 无动作，
  `dva-focus(-2)` 走 `hit < 0` 分支清焦点。
- **流布局视图要同时改测量和布局的两遍循环**：`cs-measure` / `cs-layout` 各有横向、纵向
  两条分支，每条分支又分「量尺寸」和「摆位置」两遍 —— 漏掉任何一处，叠加层就会被算进流，
  表现为「弹层把下面的内容挤下去」。抽 `cs-child-flow(h, j)`（第 j 个参与流布局的子节点）
  统一这 6 个循环，比到处加 `if` 不容易漏。
- **居中要用「覆盖固定尺寸之后」的父矩形**：`cs-layout` 一进来就用 `CMP-SW` / `CMP-SH`
  覆盖 `w` / `ht`，叠加层居中发生在这之后，所以拿到的是最终父尺寸。
- **`dv-on-overlay()` 必须带 `DV-F-OPEN`**：`cs-paint` 的 `C-DIALOG` 分支用 `F-OPEN` 把整棵
  子树门控住（关闭时什么都不画）。少了这一位，弹层会参与命中/居中但看不见。

### §5.10 阶段 16 锚定浮层新坑（2026-11-20 实测）

- **锚点只能给 key，矩形只在活池里**：视图层重建后只有 `DV-*` 数据，拿不到任何几何。
  所以视图侧存「锚点 key」（`DV-ANCHOR`），调和时落到活节点（`CMP-ANCHOR`），
  布局时用 `cs-find-key` 线性扫 `CMP-KEY` 反查句柄再取 `cs-rect`。
  没有为了锚定去改 `dva-run`，也没有引入第二棵活树。
- **`cs-find-key` 返回第一个匹配**：同一进程里 key 必须唯一。真实应用每帧视图的 key
  天然唯一，但**测试里跨场景复用同一个 key 会命中上一棵树的旧节点**（实测：锚点解析到
  上一场景的按钮，矩形是 `(0,0,0,0)`，浮层被夹到父矩形左上角）。测试每个场景都用
  不同的锚点 key。
- **锚定是「叠加层 + 一个可选锚点」，不是新 kind**：`cs-layout` 先算「居中」兜底，
  再在 `CMP-ANCHOR >= 0 && cs-find-key >= 0` 时改写成锚定位置 —— 锚点找不到就保持居中，
  不会出现 `(0,0)` 之类的退化位置。翻转与夹紧的顺序是：下方 -> 上方 -> 上夹 -> 下夹 ->
  左夹 -> 右夹（先翻再夹，夹紧用父矩形而非视口）。
- **锚点矩形取的是「本次布局已经算好的」值**：`cs-layout` 的容器分支顺序是
  量尺寸 -> 摆流子节点（递归 `cs-layout`，锚点若在这棵子树里此时已就位）-> 最后才跑叠加层
  分支。所以只要锚点不是叠加层的后代，读到的就是本帧的新位置，不存在「差一拍」。
  这也是锚定浮层不需要拓扑排序的原因。

### §5.11 阶段 17 hover / 非模态叠加层新坑（2026-11-20 实测）

- **`cs-hit` 第二趟流子节点循环会覆盖第一趟的结果**：`cs-hit` 的容器分支是
  「先扫叠加层、再扫流子节点」两趟。原实现只要树里有叠加层就 `modal = 1`，于是第二趟
  被跳过；我改成「非模态不竖屏障」时顺手把 `modal = 1` 改成「没命中才置位」，
  结果叠加层**接住了**点击（`found >= 0`）时 `modal` 仍是 0，第二趟照样跑，
  把一个也包含该点的流兄弟（实测是页首那个通栏 label）写进了 `found`。
  回归立刻变红：`dva-overlay` 的 `X hit` 与 `dva-anchor` 的 `H hit` 从 true 翻 false。
  修法：`modal = 1` 对模态层**无条件**置位（像原来那样），第二趟改成
  `(found == -1)` 才进 —— 叠加层已经接住就不许再改答案。
- **hover 状态必须在建视图之前同步**：`dva-run` 的顺序是
  收事件 -> `dva-sync-hover(S)` -> `dv-app-view()` -> `dv-render` -> `cs-layout` ->
  `cs-paint`。放到建视图之后，tooltip 要晚一帧才出现。
- **命中缓存要每帧布局后重算，不能只在鼠标事件里算**：鼠标不动时没有 motion 事件，
  但布局可能变（tooltip 出现把别的东西挤开）。所以 `cs-layout` 之后统一
  `DVA-HIT[0] = cs-hit(root, px, py)`，事件分支只更新 `DVA-PX/PY`。
- **没收到鼠标事件时不要猜命中**：`DVA-PX/PY` 初值是 0，直接算命中会在开局
  `(0,0)` 误报一次 hover。加了 `DVA-MOUSE` 哨兵（收到 motion / 点击才置 1）。
- **写状态前先比对**：`dva-sync-hover` 里 `cur != v` 才 `dva-set` ——
  写状态等于通知应用「状态变了」，每帧写同值是无谓开销。

### §5.12 阶段 18 禁用态新坑（2026-11-20 实测）

- **给函数参数赋值不生效**：`dva-focus` 里原本想写
  `(hit >= 0) -> { (cmp-has(hit, F-DISABLED)) -> { hit = -1 } }`，
  结果 `DVA-FOCUS` 与 `F-FOCUSED` 一个都没变 —— 函数**参数**被当成只读，赋值静默丢失
  （vet 不报）。改成先 `h = hit` 再用局部 `h` 判断才正常。
  这与「局部标量在块内改写」是两回事：`view-flags` 里
  `f = dv-on() (nl >= 3) -> { f = dv-disabled() }` 实测生效（`tests/dva-disabled.no`
  场景 V 专门钉住它），所以 demo 里按状态选 flags 的写法是可靠的。
- **`DV-HOT-VKEY` 存的是声明式 key，不是视图句柄**：`dv-hotkey` 压的是 `dv-key(h)`，
  所以闸门不能直接 `dv-disabled-of(vk)` —— 那会拿 key 当池下标读到越界的 0，
  把**启用**的快捷键也一起拦掉（实测 `dv-hot-dispatch(115)` 三个返回值全 0）。
  正确写法：`tv = dv-node-by-key(vk)` 再 `dv-disabled-of(tv)`。
- **命中故意不拦**：`cs-hit` 不看 `F-DISABLED`。禁用节点仍可被 hover，
  tooltip 才能解释「为什么点不动」；交互语义由派发 / 焦点两处闸门负责。
  三者职责要分清：命中 = 「鼠标在哪」，派发 = 「动作做不做」，焦点 = 「键盘给谁」。

### §5.13 阶段 19 标签页新坑（2026-11-20 实测）

- **`dv-node` 的第 5 个参数是 `color`，第 6 个才是 `value`**：
  `dv-node(C-LABEL, key, text, text2, color, value, value2, flags, parent)`。
  写 `dv-node(C-LABEL, k, lt, '', i, 0, 0, f, bar)` 会把页号塞进 color，
  `value` 仍是 0 —— `dv-apply` 的 `SET` 就把状态写成 0 了。多传一个 `0` 才对。
- **容器的属性同步跑在它的子节点挂好之前**：`dv-sync` 第 4 步是
  `dv-apply-props(m, v)` 然后 `dv-sync(m, v)`（递归挂孩子）。所以把
  `tab-select(L, active)` 放进 `dv-apply-props` 时，标题条的 `CMP-COUNT` 还是上一帧的值
  —— 首帧是 0，`tab-select` 因越界被吞，`CMP-VALUE` 被 `cmp-set-value` 打成 0，
  高亮直接跳回第 0 页（实测第二帧才「自愈」，因为那时 count 已经是 3）。
  放到 `dv-sync` **末尾**（重排之后）才对。
- **越界 `active` 要自己收敛**：`tab-select` 只在 `[0, CMP-COUNT)` 内写，
  越界是**静默忽略**，而 `cmp-set-value` 已经把 `CMP-VALUE` 写成 0 了 ——
  结果是「状态说第 99 页，标题条画第 0 页」。`dv-tabs` 里把 `active` 夹到
  `[0, n-1]`，让 flags / `DV-TAB-ACT` / 活树三者永远一致。
- **`DV-TAB-ACT` 与 `CMP-VALUE` 是同一语义的两个家**：声明式层用它（每帧从状态算），
  绘制层用 `CMP-VALUE`（`tab-active`）。两者必须每帧对齐，且只能由 `dv-tabs`
  写 `DV-TAB-ACT`（`dv-node` 初值 -1 = 「不是标签条」）。

### §5.14 阶段 20 多选列表新坑（2026-11-20 实测）

- **「flags 平行数组」必须自己处理短数组**：`dv-repeat-items-f` 的 `flags[i]`
  越界时不能直接读（会拿到 option），也不能假设调用方给满 n 项 —— 短数组 / 空数组
  一律补 `dv-on()`。为此给了一个单例 `DV-EMPTY-F`：空数组也是池对象，
  每帧新建一个纯浪费。
- **`dv-repeat-items` 必须保持原签名**：阶段 14 的调用方（`tests/dva-repeat.no`、
  `tests/main-dv.no` 的左栏三行）都在用 8 参形式。加 f 版而不是改原函数，
  让「既有调用零改动」成为可验证的回归项（`tests/dva-sel.no` 场景 Z 的 r7）。
- **`dv-ops` 数的是「非根节点」**：`dv-render(live, view)` 里的 `view` 根本身
  匹配到 `cmp-root()`，不产生结构操作。所以「表单 + 容器 + 3 项」= 4 次，
  不是 5 次。写测试断言前先数清楚「根不算」。
- **「一次写 N 个槽」归约器表达不了**：`dv-apply` 的四个动作都是单槽运算。
  全选 / 清空这种批量写只能走 `DV-A-CLICK` + 应用层命令 id（`DEMO-CMD-ALL` /
  `DEMO-CMD-NONE`），和 `reset` 同一套路。这是声明式层**故意保留的一小段命令式**：
  主循环里没有 if-else，但应用层的命令分支仍是一行 `if (pay == ...)`。
- **选中态要在绘制层真的看得见**：flags 只是数据，得挑一个画 `F-SELECTED` 的
  kind 才有的放矢。`C-RADIO` 画 `F-SELECTED` 时用 `CS-TH-OK`，`C-LABEL` 不画 ——
  所以多选列表用 `C-RADIO` 而不是 `C-LABEL`，并在测试里断言 `CS-TH-OK` 计数
  随选中数变化（只断言 flags 会漏掉「选中了但看不出来」）。
- **禁用与选中是同一套机制的两种取值**：阶段 18 的 `dv-disabled()`（少 `DV-F-EN`）
  放进 flags 数组就能表达「这几行禁用」，派发闸门（`dv-dispatch` 对
  `F-DISABLED` 不出动作）自动生效 —— 阶段 20 不需要再动派发逻辑。

### §5.15 阶段 21 下拉新坑（2026-11-20 实测）

- **「选中并收起」要写两个槽，所以必须新开动作码**：`dv-apply` 的 `SET` 只写
  一个槽。下拉点选项时既要写「选中下标」又要清「展开槽」，一个动作码承不住，
  于是加 `DV-A-CHOOSE = 10`：`S[payload] = value`，若 `value2 >= 0` 再
  `S[value2] = 0`。选项节点的 `value` / `value2` 正好是这两个操作数，
  所以视图里的动作数据就是全部语义 —— 归约器仍然不需要知道「这是下拉」，
  主循环里也没有 key 的 if-else。
- **选项必须常驻，靠命中闸门表达「收起」**：如果像 dialog / popover 那样按
  展开条件挂载，每帧增删节点（结构操作）不说，还会把「展开 / 收起」变成
  视图重建。`cs-hit` 早就有 `C-SELECT` 分支（收起时 `walk = 0`），
  `cs-layout` 也只在 `F-OPEN` 时才把选项排到字段下面 —— 所以选项常驻、
  展开 / 收起只改 flags 与文案，`ops == 0`。测试里专门断言了
  「收起后同一坐标不再命中选项」（收起后选项矩形是 `(0,0,0,0)`，
  点击会落到下层表单上，所以断言的是 `hit != 选项`）。
- **字段文案也是状态的函数**：`labels[S[selslot]]` 越界 / 未选时是空串，
  配合占位符 `ph` 显示。注意 `labels[i]` 是 `?str`，要 match 解包 ——
  这里同时踩了「数组索引返回 option」和「短数组要自己兜底」两条。
- **声明式替换命令式时，要连事件侧的死分支一起删**：左栏下拉从
  「手写 `C-SELECT` + 3 个选项 + `DV-A-OPEN` / `DV-A-SET`」换成
  `dv-select-from` 之后，`dv-app-click` 里那两段（`DV-A-OPEN` 的
  `select-open` / `select-close`，以及「归约命中后补一次 `select-close`」）
  就变成了死代码 —— 留着不会错，但会让 demo 的「语义都在视图里」这个
  说法打折扣，所以一并删掉。
- **删掉全局量之前先 grep 引用**：`DEMO-OPT-A/B/C` 三个 str 全局被
  `DEMO-OPTS []str` 取代时，右栏单选组（key 25/26/27）还在引用它们。
  `../bin/no vet` 与 `build` 都报 0 error（未定义的全局量被当成一个
  坏掉的 str），运行首帧 `dv_node` 里 push 文案的 `memmove` 直接
  `EXC_BAD_ACCESS` → `exit 138`。改成 `dv-demo-opt(i)` 从数组里读
  （顺带让右栏和左栏共用同一份选项数据），并靠 lldb 回溯
  （`dv_node` ← `dv_demo_view` ← `dva_run`）定位。

### §5.16 阶段 22 通知新坑（2026-11-20 实测）

- **存活要存「触发帧」而不是布尔**：布尔只能表达「现在有没有」，表达不了
  「还有多久」。槽里存 `dv-frame()`，判活 = `t > 0 && dv-frame() < t + ttl`。
  代价是要让 0 成为干净的「从未触发」哨兵 —— `DV-FRAME[0]` 初值设为 1，
  否则第一帧的 0 会和「没有通知」撞车（`dv-is-used` 比的正是这个 0）。
- **触发值也要视图算**：按钮挂 `value = dv-frame()` + `DV-A-SET(触发槽)`，
  归约器仍然只写一个槽 —— 「何时触发」也是 `UI = f(state)`，
  不需要应用层记时间戳。注意 `dv-frame()` 在建视图时读，而
  `dv-bump-frame()` 在 `dv-render` 开头，所以同一帧里「读到的帧」比
  「标记已用」的帧小 1，判活边界因此是 `触发帧 + ttl`（含触发帧本身）。
- **删除整棵子树只算 1 次操作**：`dv-sync` 第 2 步只对**直接子节点**调
  `cmp-detach`，孙节点随本体一起走。所以「关闭 toast」= 1 次操作，
  不是 3 次；而「已经建好的活着节点再渲染」= 0 次。测试最初按 3 次
  写断言，跑出来 5 个 false —— 这是 `dv-ops` 语义里最容易踩的一条。
- **本体必须是容器 kind**：`C-NOTIFICATION` 不在 `cs-is-container` 白名单里，
  布局阶段会被当叶子（子节点不参与流布局）。所以 toast 本体用
  `C-POPOVER`，配 `dv-on-tip()`（`VIS|EN|PASS|OVL`，非模态：仍最后画、
  自身可被命中，但没接住点击就继续往下找）。
- **判活原语要能被测试直接驱动**：`dv_frame()` 只能读，所以测试里用
  `dv-arr-set(DV-FRAME, 0, f)` 直接写帧号（`DV-FRAME` 是模块级 i64 数组，
  测试文件和 `dv.no` 同属一个编译单元），才能逐帧钉住 ttl 边界。

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
- [x] 阶段 8：组件渲染层 — `src/cmp-scene.no`（`cs-layout` / `cs-measure` / `cs-paint` / `cs-hit`
      + Scene 指令池）；`tests/cmp-scene.no` 40 项断言全绿；`tests/main-app.no` 组件树 → 场景 → SDL 窗口跑通。
- [x] 阶段 9：应用装配层 — `src/app.no`（`app-init` / `app-frame` / `app-draw-scene` / `app-poll` /
      `app-hover` / `app-should-quit`）；`tests/app-assemble.no` 29 项断言全绿；
      `tests/main-form.no` 窗口端到端表单（输入 / 下拉 / 滑杆 / 勾选 / 弹窗）跑通。
- [x] 阶段 10：声明式视图层 — `src/dv.no`（`dv-*` 构造器 + `dv-render`/`dv-sync` keyed 调和器
      + `dv-act`/`dv-dispatch` 动作数据 + `dv-apply` 状态归约器 + `dv-repeat` 重复原语
      + `dv-lay` 布局提示 / `dv-row` / `dv-fill` / `dv-size` / `dv-align` / `dv-live-deep`）
      叠加在阶段 8–9 命令式内核之上；构造器覆盖全部 83 kind（`dv-create` 全函数 +
      `cmp-alloc` 兜底 + 整套命名构造器），flags 扩到 `DV-F-VIS/EN/CHK/SEL/ACT/OPEN`，
      布局提示扩到 `L-H/L-GROW/L-AE/L-AC` + `gap`/`pad` 覆盖，
      尺寸 / 对齐提示扩到 `CMP-SW/CMP-SH/CMP-ALIGN` + `dv-size`/`dv-align`；
      `tests/dv.no` 65 项断言全绿、`tests/dv-layout.no` 41 项断言全绿；
      `tests/main-dv.no` 窗口端到端（定宽两栏 + 居中标题，900 帧 `ops=40`）跑通。
- [x] 阶段 11：声明式应用装配层 — `src/dv-app.no`（`dva-run(S, title, max-frames)` 一行接管
      主循环：轮询 / hover / 命中 / `dva-focus` 焦点交接 / `dva-keyboard` / 按名调
      `dv-app-view`·`dv-app-reduce`·`dv-app-click` / 重建视图 / 调和 / 布局 / 上屏 /
      帧数上限收尾）叠加在阶段 10 之上；`tests/dva.no` 18 项断言全绿；
      `tests/main-dv.no` 主循环从 ~70 行降到三行，输出不变（`ops=40`）。
- [x] 阶段 12：声明式样式的落点 — `src/cmp-scene.no` 的 `CS-TH` 九槽主题色板 +
      `cs-theme`/`cs-theme-set`/`cs-theme-reset`（`cs-paint` 所有分支改读主题色，
      默认值 = 旧常量，不写主题时渲染逐位不变）+ `src/components.no` 的 `CMP-VAR`
      变体池 + `src/dv.no` 的 `dv-variant` / `dv-var-*` / `dv-theme-set`；
      `dva-run` 启动时把 `CS-TH-BG` 接到窗口底色；`tests/dv-style.no` 29 项断言全绿；
      `tests/main-dv.no` 应用 9 槽主题 + 6 处变体，结构不变（仍 `ops=40`）。
- [x] 阶段 13：声明式受控输入 + 快捷键 — `src/dv.no` 的 `DV-BIND` / `DV-BIND-MAX` /
      `DV-BIND-T` 池 + `dv-bind` / `dv-bound` / `dv-bound-set` / `dv-bind-t-ensure` /
      `dv-node-count`，`DV-HOT-*` + `dv-hotkey` / `dv-hot-dispatch`；
      `src/dv-app.no` 的 `dva-sync-binds(root)` 每帧双向同步（`dva-run` 签名不变）+
      键盘分支「有焦点喂字 / 无焦点先试快捷键」；
      `tests/dva-bind.no` 18 项断言全绿；`tests/main-dv.no` 输入框绑 `DV-BIND-T[0]`
      并回显、`r` 键重置（含清文本），结构不变（仍 `ops=40`）。
- [x] 阶段 14：声明式列表项 — `src/dv.no` 的 `dv-repeat-items(parent, key0, n, kind,
      texts, values, acts, pays)`（平行数组 → n 个列表项，短数组补默认、n 超长按长度、
      负数按 0）+ `dv-rows-del(texts, n, i)`（删第 i 项返回新长度，越界原样返回）+
      动作码 `DV-A-ITEM`（载荷 = 行号，与 `reset` 的 `DV-A-CLICK` 区分）；
      `tests/dva-repeat.no` 23 项断言全绿（构造 / 钳制 / 事件回路 / 删除差量 / 数组删除）；
      `tests/main-dv.no` 三行各挂 `(DV-A-ITEM, 行号)`，点任意一行删掉它，结构不变（仍 `ops=40`）。
- [x] 阶段 15：声明式模态弹层 — `src/components.no` 新 flag `F-OVERLAY`（512）；
      `src/dv.no` 新视图位 `DV-F-OVL`（64）+ `dv-overlay` / `dv-on-overlay`（经 `dv-apply-flags`
      落到活节点）；`src/cmp-scene.no` 的 `cs-is-overlay` / `cs-child-flow` +
      测量 / 布局（父矩形内居中）/ 绘制（最后画）/ 命中（先命中，点不穿 -> `-2`）；
      `tests/dva-overlay.no` 17 项断言全绿（标记 / 测量 / 居中 / 命中阻隔 / 绘制顺序）；
      `tests/main-dv.no` 的 `dialog` 按钮写 `D-DIALOG=1`，视图里出现居中模态对话框
      （ok / cancel 写回 0），新增一个按钮后 `ops` 38 → 39。
- [x] 阶段 16：声明式锚定浮层 — `src/components.no` 新活池 `CMP-ANCHOR`（锚点 key，-1 = 无）
      + `cmp-anchor` / `cmp-set-anchor`；`src/dv.no` 新视图池 `DV-ANCHOR` + `dv-anchor-of` /
      `dv-overlay-anchor(node, anchor-key)`（= `dv-overlay` + 锚点 key，经 `dv-apply-props`
      落到活节点）；`src/cmp-scene.no` 的 `cs-find-key`（线性扫 `CMP-KEY` 反查活句柄）+
      叠加层分支改「锚定优先」（锚点下方 -> 贴边翻上方 -> 两轴夹紧到父矩形；无锚点 /
      锚点找不到则保持居中）；`tests/dva-anchor.no` 18 项断言全绿（标记 / 调和 / 下方优先 /
      翻转夹紧 / 命中阻隔 / 绘制顺序）；`tests/main-dv.no` 的 `popover` 按钮写 `D-POP=1`，
      菜单锚在触发按钮下（`close` 写回 0），新增一个按钮后 `ops` 39 → 40。
- [x] 阶段 17：声明式 hover + 非模态叠加层 — `src/components.no` 新位 `F-PASS`（1024）；
      `src/dv.no` 新视图位 `DV-F-PASS`（128）+ 新池 `DV-HOVER` + `dv-hover` / `dv-hover-of` /
      `dv-pass` / `dv-on-tip`（`dv-apply-flags` 覆盖式同步）；
      `src/dv-app.no` 的 `dva-sync-hover(S)` + `DVA-HIT` / `DVA-MOUSE` 命中缓存
      （`cs-layout` 之后重算，建视图之前同步）；`src/cmp-scene.no` 的 `cs-hit` 分档
      （模态竖 `-2` 屏障，非模态没接住就继续走流子节点）；
      `tests/dva-hover.no` 23 项断言全绿（标记 / 调和含降级清 `F-PASS` / 状态同步 /
      命中穿透 / 绘制顺序 / hover→tooltip 端到端）；`tests/main-dv.no` 里
      `dv-hover(b19, D-TIP)` 让鼠标停在 `dialog` 按钮上时浮出 tooltip（`tip=` 回显），
      不 hover 时节点不存在，`ops` 仍 40（探针：`D-TIP` 初值写 1 时 `ops` 40 → 42）。
- [x] 阶段 18：声明式禁用态 — `dv-disabled()` / `dv-enabled()` / `dv-disabled-of(v)`；
      `dv-dispatch` / `dv-hot-dispatch` / `dva-focus` 三处闸门尊重禁用
      （`DV-HOT-VKEY` 存的是声明式 key，先 `dv-node-by-key` 反查）；
      `cs-hit` 故意不拦，禁用节点仍可 hover / 出 tooltip；
      `tests/dva-disabled.no` 23 项断言全绿；`tests/main-dv.no` 里
      `nl >= DEMO-MAX-ROWS` → `add row` 禁用、`nl <= 0` → `del row` 禁用，
      禁用的 `add row` 挂 `dv-hover(b16, D-TIP2)` 浮出「at the row cap」。
- [x] 阶段 19：声明式标签页 — `src/dv.no` 新池 `DV-TAB-ACT` + `dv-tabs` /
      `dv-tab-active-of`（标题条 + n 个标题 + 内容容器，标题挂 `DV-A-SET`，
      选中态 `dv-on-sel`，越界 `active` 收敛到最后一页）；
      `dv-sync` **末尾**按 `DV-TAB-ACT` 调 `tab-select` 回写 `CMP-VALUE`
      （`cs-paint-tab` 画高亮读它；放末尾是因为属性同步跑在挂孩子之前）；
      `tests/dva-tabs.no` 27 项断言全绿（标记 / 调和含换页 `ops == 0` 与越界收敛 /
      派发归约 / 布局 / 绘制 / `cs-hit`→`cmp-key`→`dispatch`→`apply`→重建端到端）；
      `tests/main-dv.no` 右栏底部三页标签（`tab=` 回显，reset 回第 0 页），
      首帧 `ops` 40 → 47。
- [x] 阶段 20：声明式多选列表 — `src/dv.no` 新全局 `DV-EMPTY-F` +
      `dv-repeat-items-f`（阶段 14 原语 + flags 平行数组，短 / 空数组补 `dv-on()`）+
      `dv-sel-flags(S, slot0, n)`（槽非 0 → `dv-on-sel()`）；
      `dv-repeat-items` 签名不变、内部转调 f 版（既有调用零改动）；
      `tests/dva-sel.no` 28 项断言全绿（标记含旧签名回归与 n 收敛 / 推导 /
      调和含 `F-SELECTED`·`F-DISABLED`·禁用项不出动作 / 派发归约 /
      `cs-hit`→`cmp-key`→`dispatch`→`apply`→用 `dv-sel-flags` 重建端到端，
      含绘制层 `CS-TH-OK` 计数随选中数变化）；
      `tests/main-dv.no` 右栏顶部三行多选（`sel=` 回显）+ `all` / `none` 幽灵按钮
      （`DV-A-CLICK` + `DEMO-CMD-ALL` / `DEMO-CMD-NONE`，命令分支一次写 3 个槽），
      reset 一并清空选中位，首帧 `ops` 47 → 52。
- [x] 阶段 21：声明式下拉 — `src/dv.no` 新动作码 `DV-A-CHOOSE`(10)
      （`dv-apply` 一次写两个槽：选中下标 + 清展开槽）+ `dv-select-from`
      （一行建出「`C-SELECT` 字段 + n 个常驻选项」，字段文案 = `labels[S[selslot]]`，
      字段挂 `DV-A-TOGGLE(openslot)`、选项挂 `DV-A-CHOOSE(selslot)` 且
      `value` = 下标 / `value2` = 展开槽）；选项常驻靠 `cs-hit` 的
      `C-SELECT` 分支表达「收起」，展开 / 收起零结构操作；
      `tests/dva-select.no` 32 项断言全绿（标记含未选 / 越界文案空 /
      调和含 `ops == 4` 与换选中·展开 `ops == 0` / 派发归约含写 2 槽与
      `value2 < 0` 写 1 槽 / `cs-hit`→`cmp-key`→`dispatch`→`apply`→重建端到端，
      含 `CS-TH-ACCENT` 计数与「收起后同一坐标不再命中选项」）；
      `tests/main-dv.no` 左栏下拉换成这一行（事件侧两段 select 分支一并删除），
      右栏单选组文案改从 `DEMO-OPTS` 读，reset 多写 `D-SOPEN`，页头回显 `pick=`，
      首帧 `ops` 仍是 52，`bye … pick=0`、`exit 0`（连跑两次一致）。
- [x] 阶段 22：声明式通知 — `src/dv.no` 新判活原语 `dv-toast-alive(S, slot, ttl)`
      （槽存**触发帧**，`t > 0 && dv-frame() < t + ttl` 才活，0 = 没有通知；
      `DV-FRAME[0]` 初值设为 1 让 0 成为干净的哨兵）+ `dv-toast(S, parent, key,
      slot, text, ttl, base)`（活着才建 `C-POPOVER` + `dv-on-tip()` + 文案 +
      关闭按钮，关闭 = `DV-A-SET` 写回 0）；触发按钮 `value = dv-frame()` +
      `DV-A-SET(触发槽)`，「何时触发」也是 `UI = f(state)`；本体用 `C-POPOVER`
      是因为 `C-NOTIFICATION` 不在 `cs-is-container` 白名单里；
      `tests/dva-toast.no` 31 项断言全绿（alive 边界含 0 槽 / `ttl=0` / 越界槽 /
      调和含活 `ops == 3` 与关闭 `ops == 1`（只摘直接子节点）/ 派发归约 /
      ttl 边界逐帧 400·401·402 活 `ops == 0`、403 起 `ops == 1` 且消失 /
      绘制层文案随存活出现·消失 / 触发按钮接线与 main-dv 一致）；
      `tests/main-dv.no` 左栏加 `notify` 按钮 + 视图里 `dv-toast(...)`,
      reset 多写 `D-TOAST`，页头回显 `tst=`，首帧 `ops` 52 → 53，
      `bye … tst=0`、`exit 0`。
- [x] 编译器迁移：safe-index downgrade 后的索引/算术 option 解包已铺到库与全部维护中测试。

### 当前可运行产物
- `main.no`（包根唯一入口）：原始纵切面（顶层主循环 + 标量全局），已验证
- `tests/main-layered.no`：**分层内核版**（Entity/Element/Scene 大写全局池 + 函数封装 + Scene 遍历绘制），已验证
  运行：`../bin/no build -cc ./ccwrap.sh -o dist/main-layered tests/main-layered.no`
  启动打印 `scene=8 elements=3 entities=3`，窗口持续存活，ESC/超时退出。
- `tests/main-app.no`：**组件 + 场景 + 窗口端到端**（组件树 → `cs-layout`/`cs-paint` → Scene → SDL），已验证
  运行：`../bin/no build -cc ./ccwrap.sh -o dist/main-app tests/main-app.no`
  启动打印 `window handle: ...`，hover / 点击 `-`/`+` / 勾选 / ESC 均生效。
- `tests/main-form.no`：**装配层窗口端到端表单**（`src/app.no` + 组件状态机），已验证
  运行：`../bin/no build -cc ./ccwrap.sh -o dist/main-form tests/main-form.no`
  键盘输入 / 下拉选中 / 滑杆拖拽 / 勾选 / 提交弹窗全链路；900 帧自动收尾 `exit 0`。
- `tests/main-dv.no`：**声明式窗口端到端**（`src/app.no` + `src/dv.no` + `src/dv-app.no` +
  `src/cmp-scene.no` 主题层），已验证
  运行：`../bin/no build -cc ./ccwrap.sh -o dist/main-dv tests/main-dv.no`
  `dva-run(DEMO, title, 900)` 接管主循环：`cs-hit` → `cmp-key` → `dv-dispatch` 取视图动作 →
  按名调 `dv-app-reduce`（`dv-apply` 归约 `DEMO` 状态槽）/ `dv-app-click`（结构相关动作）→
  `dv-app-view()` 重建视图（页头全宽 + `dv-row`/`dv-size`/`dv-fill` 定宽两栏 + `dv-act` +
  `dv-repeat-items`）→ `dv-render` 调和；
  阶段 12：`dva-run` 之前设 9 槽主题（偏紫深色），视图里 6 处 `dv-variant`
  （`toggle extra` 主色 / `add row` 幽灵 / `del row` 次要 / `reset` 危险 /
  徽章主色 / 标签次要），结构不变；
  阶段 13：输入框 `dv-bind(inp, D-TEXT, 0)` 绑到字符串状态 `DV-BIND-T[0]`，
  视图用 `dv-bound(D-TEXT)` 回显、reset 归约里 `dv-bound-set` 清文本，
  `dv-hotkey(b12, 114, DV-A-CLICK, DEMO-CMD-RESET)` 让 `r` 键触发重置
  （无输入焦点时；114 = SDLK_r），结构仍不变；
  阶段 14：三行列表由 `dv-repeat-items(lc, 80, nl, C-LABEL, DEMO-ROWS, DEMO-ROW-VALS,
  DEMO-ROW-ACTS, DEMO-ROW-PAYS)` 生成，每行挂 `(DV-A-ITEM, 行号)`，
  `dv-app-click` 收到后用 `dv-rows-del(DEMO-ROWS, nl, pay)` 删掉该行，结构仍不变；
  阶段 15：页头多一个 `dialog` 按钮（`DV-A-SET` 写 `D-DIALOG=1`），视图里
  `(dl != 0)` 时用 `dv-on-overlay()` 挂一个居中模态对话框（label + ok + cancel，
  两个按钮写回 0）—— 叠加层不占页头流布局、绘制最后、点不穿；
  阶段 16：左栏多一个 `popover` 按钮（key=70，写 `D-POP=1`），视图里 `(pp != 0)` 时
  用 `dv-overlay-anchor(pop, 70)` 挂一个锚在触发按钮下的菜单（两项 + `close` 写回 0）——
  下方放不下会翻到上方并夹紧在窗口矩形内；
  阶段 17：`dv-hover(b19, D-TIP)` 声明 hover 槽，视图里 `(tp != 0)` 时用
  `dv-on-tip()` + `dv-overlay-anchor(tip, 19)` 挂一个**非模态** tooltip（自身可命中、
  点得穿），页头回显 `tip=`；不 hover 时节点不存在，所以结构操作数不变；
  阶段 18：`nl >= DEMO-MAX-ROWS` 时 `add row` 用 `dv-disabled()`（可见但点不动），
  `nl <= 0` 时 `del row` 禁用；给禁用的 `add row` 挂 `dv-hover(b16, D-TIP2)`，
  鼠标停在上面仍浮出「at the row cap」—— 命中不被禁用拦住，所以 tooltip 能解释原因；
  阶段 19：右栏底部 `dv-tabs(rc, tab, D-TAB, DEMO-TABS, 40)` 建三页标签
  （标题 alpha / beta / gamma），页头回显 `tab=`；点标题 = `SET D-TAB = 页号`，
  页面内容按 `active` 条件挂进返回的容器（换页只换子树，标题条与容器同 key 复用），
  reset 把 `D-TAB` 写回 0；首帧因此多建 7 个节点（标题条 + 3 标题 + 容器 + 2 内容）；
  阶段 20：右栏顶部 `dv-repeat-items-f(rc, 53, 3, C-RADIO, DEMO-SEL, DEMO-SEL-VALS,
  DEMO-SEL-ACTS, DEMO-SEL-PAYS, dv-sel-flags(DEMO, D-SEL0, 3))` 建三行多选
  （apple / banana / cherry），每项挂 `(DV-A-TOGGLE, D-SEL0+i)` —— 点一项翻自己的槽，
  下一帧 `dv-sel-flags` 把选中态算回 flags，活树选中态跟着走；
  `all` / `none` 两个幽灵按钮走 `DV-A-CLICK` + `DEMO-CMD-ALL` / `DEMO-CMD-NONE`，
  命令分支一次写 3 个槽；页头回显 `sel=N`，reset 一并清空选中位；
  首帧因此再多建 5 个节点（3 个列表项 + 2 个按钮）；
  阶段 21：左栏下拉从「手写 `C-SELECT` + 3 个选项 + `DV-A-OPEN` / `DV-A-SET`」
  换成 `dv-select-from(DEMO, lc, 4, D-CHOICE, D-SOPEN, DEMO-OPTS, 'pick', 4)`
  （字段 + 3 个常驻选项，字段挂 `DV-A-TOGGLE(D-SOPEN)`、选项挂
  `DV-A-CHOOSE(D-CHOICE)`），节点数不变所以 `ops` 不变；右栏单选组（key 25/26/27）
  文案改从 `DEMO-OPTS` 读（`dv-demo-opt(i)`），和左栏共用一份选项数据；
  事件侧那两段 select 分支（`DV-A-OPEN` 的 `select-open` / `select-close` 与
  「归约命中后补 `select-close`」）一并删除；reset 多写 `D-SOPEN = 0`；
  页头回显 `pick=`；
  阶段 22：左栏加 `notify` 按钮（key=77，`value = dv-frame()`，挂
  `DV-A-SET(D-TOAST)`），视图里 `dv-toast(DEMO, page, 78, D-TOAST, 'saved',
  DEMO-TOAST-TTL, 79)` —— 触发帧非 0 且未过 ttl 时挂出 `C-POPOVER` +
  `dv-on-tip()` 的非模态通知（文案 + 关闭按钮写回 0），过期是下一帧视图重建的
  自然结果，不需要定时器；reset 多写 `D-TOAST = 0`；页头回显 `tst=`；
  通知默认不活，所以首帧只多建触发按钮这 1 个节点；
  900 帧打印 `bye frames=900 ops=53 clicks=0 rows=2 sel=0 pick=0 tst=0`（首帧建 53 节点，之后零结构操作），
  `exit 0`，连跑两次输出一致；视图声明的动作数据（`tests/dv.no` 场景 K 覆盖语义）：
  「add row」`key=16 act=DV-A-DELTA pay=D-LIST value=1 value2=3`、
  勾选框 `key=9 act=DV-A-TOGGLE pay=D-AGREE`。

## 8. 验收目标

- [x] `no test` 覆盖 L1–L3 纯逻辑（实体注册、布局计算、scene dump）— `tests/logic/l1-l3.no`，
      11 项断言全绿；该目录用独立 `package.noml`（无 link-libs）避开 shim 链接，
      直接 `cd tests/logic && ../../../bin/no test .` 即可。
- [x] 端到端示例：一个窗口显示按钮，点击后计数器 +1，窗口标题/内容随之变化，ESC 退出。
- [x] 全部构建走 `no build -cc wrapper`，运行带 `DYLD_LIBRARY_PATH`。
