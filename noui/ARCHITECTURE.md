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
- **模块级 `[]str` **字面量**在函数体里读出来是空的（`len()` = 0）**：
  `T []str = ['a','b','c']` 这种绑定，函数里 `T.len()` 是 0、按元素取也全空，而**编译与
  `vet` 都干净**（没有 hint、没有 error）。`[]i64` 字面量不受影响（`T-K0 []i64 = [1,5,8]`
  在函数里读得出来），`str` 模块绑定也不受影响。根因在 `src/mir/hir2mir.go` 的
  `foldConstText`（详见 §5.25）；绕法是模块级 `[]str` 一律 `with-len(n)` + 逐项下标写。

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
- **空间也是状态（阶段 23：声明式滚动）**：前面所有「会变的量」都是应用写进
  状态的标量（开关 / 选中 / 触发帧），几何全是布局算出来的、应用碰不到。滚动
  是第一个把**空间**写进状态的原语，但偏移不能由应用拍脑袋写 —— 它取决于
  「内容多高」，而内容高只有布局知道。分工因此是：布局算出「能滚多少」
  （`CMP-SCROLL-MAX` = 内容高 − 视口高），状态槽存「滚了多少」；
  `dv-scroll(S, key, offslot, step, parent)` 每帧把 `S[offslot]` 读成组件
  `value`，布局据此把内容整体上移（子节点不拉伸），`step` 放 `value2`。
  布局只夹紧 `off` 到 `[0, max]` 后排版，**不写回状态**（真源是槽）；
  写回在事件侧：新增滚轮事件 `type = 5`（光标走 x/y 槽、有符号增量走
  bit56..63），装配层 `dva-wheel` 拿命中顺 `cmp-parent-of` 父链找最近的
  `C-SCROLL`，夹紧后 `dva-set` 回同一个槽（与滑杆同一模式）。
  绘制层给滚动子树的每条 Scene 指令带裁剪矩形（`cs-emit-clip` →
  `noui_clip`），视口外不画；命中层视口外直接不命中、视口内按全局坐标走
  `cs-hit-inner`（阶段 24 修正：子节点矩形本身已带 `-off` 偏移，原来多换算
  了一次 `py + off`，大偏移时视口内点不中）。于是页头回显的 `scrl=N` 也是
  `UI = f(state)`。
- **看不见的行就不存在（阶段 24：声明式虚拟列表）**：滚动只解决「内容比视口
  高」，内容本身仍全挂在活树上 —— 一万行就是一万个活节点。虚拟列表把
  「内容多高」变成**声明数据**而不是实测结果：`dv-scroll-content(h, ch)` 说
  内容高、`dv-scroll-start(h, i0)` 说「挂上来的第一个儿子是第 i0 行」。
  布局在 `decl > 0` 时不再累加子节点，直接用 `ch` 算可滚范围，并把第 j 个
  儿子排在 `y + pad - off + (i0 + j) * 行距` 上 —— **绝对行号**是关键，
  否则窗口滑到底时那几行会被当成第 0..4 行堆在视口顶。视图侧一行式原语
  `dv-list-view(S, key, offslot, step, viewh, rowh, n, texts, vals, acts,
  pays, key0, parent)` 内部算可见窗口
  （`i0 = (off - pad - rowh) / 行距 + 1` 夹到 `[0, n-1]`，
  `i1 = ceil((off - pad + viewh) / 行距)` 夹到 `n`），再走 `dv-scroll` +
  `dv-size` + `dv-scroll-content` + `dv-scroll-start` + `dv-repeat-at`。
  `dv-repeat-at` 是 `dv-repeat-items` 的切片版，行 key = `key0 + 绝对行号`，
  所以窗口滑动时 `dv-sync` 复用同一批组件、只改文案（零结构操作）；
  `rowh > 0` 时给每行 `dv-size` 定高，让实测行距与窗口公式的行距一致。
  落点：`DV-SCROLL-CONTENT` / `DV-SCROLL-START` 视图池（`dv-apply-props`
  无条件同步，0 = 没声明，普通滚动不受影响）+ `CMP-SCROLL-CONTENT` /
  `CMP-SCROLL-START` 活池。
- **显示哪些也是状态（阶段 25：声明式过滤列表）**：虚拟列表让「挂多少」变成
  偏移的函数，但「挂哪些」仍是应用手写 if。搜索框是最典型的场景 —— 输入一个
  词，列表只剩包含它的行。做法是把查询串也当成状态（动态字符串状态
  `DV-BIND-T[qslot]`，`dv-bound` 读），列表内容就是它的纯函数：
  `dv-filter-map(qslot, n, texts, idxs)` 每帧算出命中行号表（两边
  `str.to-lower` 再 `str.index` —— 大小写不敏感；`''.index('') == 0` 所以
  空查询天然命中全部），`dv-filter-list` 用 `dv-repeat-mapped` 按下标表挂行。
  行 key = `key0 + 源行号`（不是窗口位置），所以查询变化 / 窗口滑动都按源行号
  复用组件；`dv-repeat-mapped` 的越界 / 负数下标夹到 `[0, m-1]`，`count`
  超出表长时只挂到表尾（否则会建出一堆同 key 的行，`dv-sync` 按 `(key, kind)`
  复用会互相覆盖）。窗口计算抽成 `dv-win-range(off, viewh, rowh, n, out)`，
  `dv-list-view` 也改用它 —— 两套窗口公式只有一个真源。内容高
  `= 命中数 * rowh + (命中数 - 1) * CS-GAP + 2 * CS-PAD`。
  落点：`src/dv.no` 的 `dv-win-range` / `dv-repeat-mapped` / `dv-filter-map` /
  `dv-filter-count` / `dv-filter-list` + `src/cmp-scene.no` 的空滚动容器修正
  （见 §5.19）。
- **顺序也是状态（阶段 26：声明式排序 / 分组）**：阶段 24/25 把「挂多少」「挂哪些」
  都变成了状态的函数，「按什么顺序显示」却还要手写 if 挑行重排。现在一个 mode 槽
  （0 源序 / 1 键升 / 2 键降 / 3 分组 / 4 分组降）驱动整条纯函数链：
  `keys`/`grps` → `dv-sort-map`（**稳定**插入排序，出下标表；`n` 夹到 `keys.len()`、
  负数当 0；mode 只认 1/2，其它返回源序）→ `dv-group-plan`（下标表 → 混合行表：
  组号变化时写 token `-(g)-1`（负 = 标题），**组序跟着排好的表走**）→
  `dv-win-range`（混合行表里取可见窗口）→ `dv-repeat-plan`（token < 0 挂标题行
  （key = `hkey0 + 组号`、`dv-disabled()` + `hcolor`），token ≥ 0 挂数据行
  （key = `key0 + 源行号`），所以换模式仍按源行号复用组件）。内容高按**混合行数**
  `m` 算，分组后「能滚多远」也跟着变大。行数按 `plan.len()` 夹（不是 `texts.len()`）。
  落点：`src/dv.no` 的 `dv-sort-run` / `dv-sort-map` / `dv-group-plan` /
  `dv-repeat-plan` / `dv-order-list`（见 §5.20）。
- **合不合规矩也是状态（阶段 27：声明式表单校验）**：前面所有「会变的量」都是
  应用写进状态的标量，「这个字段合不合法」却还要手写 if-else 链。现在校验规则也是
  数据：一张**扁平规则表**（`kinds` / `pars` 平行数组，按 `base + n` 切片），
  每个字段「怎样算不合法」就是表里的一段，值来自受控输入的字符串槽
  （`DV-BIND-T`）。`dv-rule-check(v, spars, kinds, pars, base, n)` 返回**第一条**
  违反的规则号（1 基，而且是**全局规则下标 + 1**，不是切片内序号 —— `msgs` 是
  逐条规则的扁平表，字段只是切片，返回全局号才能直接查措辞），全过 0；
  `dv-rule-msg(code, msgs)` 把规则号换成文案（文案挂在**规则**上，不是字段上）；
  `dv-form-bad(slots, bases, ns, ...)` 汇总成「坏了几项」。`dv-field(...)` 一行建出
  「标签 + 输入（绑字符串槽）+ 错误行」，错误行只在（有违反 && 揭示槽 ≠ 0）时
  出现在视图里 —— **「什么时候显示错误」是另一根轴**，提交失败时应用层把揭示槽
  写成 1，首次进页面只禁用提交、不显示红字。规则种类：必填 / 最小长度 /
  最大长度 / 最小值 / 最大值（不可解析算违反）/ 与另一槽相等 / 包含子串。
  落点：`src/dv.no` 的 `DV-R-*` + `dv-rule-check` / `dv-rule-msg` /
  `dv-form-bad` / `dv-field` + `tests/dva-valid.no`（46 项断言，见 §5.21）；
  demo 左栏换成 `dv-scroll` 滚动容器 + 四字段校验表单 + 显式提交命令。
- **一行 N 列也是状态（阶段 28：声明式虚拟表格）**：阶段 24/25/26 的列表一行只有
  一个标签，真实的表格（一行 N 列 + 表头）还手写不出来。现在 `dv-table-rows` 把
  窗口内的混合行表挂成 `C-GRID` 行（每行 `ncols` 个 `C-LABEL` 单元格，行宽 = 表宽、
  行高 = `rowh`），`dv-table-list` 在它之上补一条**不滚的表头**（`parent` 的前一个
  孩子）+ 滚动容器 + 可见窗口的行。它是 `dv-order-list` 的**严格超集**：吃同一套
  `plan`（token ≥ 0 = 源行号 / token < 0 = 组标题），所以组标题在表格里是**通栏行**
  （占满表宽、`dv-disabled()` 的单标签），数据行才是 N 列；`ncols = 1` 且不分组时
  行为与 `dv-order-list` 等价。**列宽是声明的**（`colws` 逐列给，表宽 =
  `2*CS-PAD + 列宽和 + (ncols-1)*CS-GAP` 由 `dv-table-list` 自己算）—— 可见窗口
  只有几行，若按内容实测，列宽会随「当前窗口恰好是哪些行」浮动，表格一边滚一边抖。
  落点：`src/dv.no` 的 `dv-table-rows` / `dv-table-list` + `tests/dva-table.no`
  （32 项断言，见 §5.22）；demo 右栏那条列表换成两列表格（`name` / `key`）。
- **分栏位置也是状态（阶段 29：声明式可拆面板）**：`dv-dock-split(key, slot, minw,
  maxw, gw, parent)` = 一台通了电的 `C-DOCK` 横向容器 —— `L-H` 提示、`gap = gw`
  （拖杆命中区）、动作 `DV-A-CHANGE` + 载荷 = 状态槽、`value` = 左屏最小宽、
  `value2` = 最大宽。左屏 `dv-size(a, dv-arr-get(S, slot), …)`（宽就是状态），
  右屏 `dv-lay(b, dv-lay-grow(), 0, 0)` 吃掉剩余；竖拆只要 `dv-lay(d, 0, gw,
  CS-PAD)` 抹掉 `L-H`、状态槽改存上屏高。**拖杆不需要新节点种类、也不需要布局
  引擎分支**：两屏之间的间隙不属于任何子节点，`cs-hit` 的「自身矩形兜底」让间隙
  命中 `C-DOCK` 自己，事件侧（`dva-drag-down`）再用光标是否落在「左屏右缘 ..
  左屏右缘 + gap」把它和普通容器点区分开。事件侧三段：`t == 2` 在间隙里按下 ->
  `DVA-DRAG[0] = dock`（否则照常走点击归约）、`t == 3` 移动 -> `nv = px − 左屏.x`
  （竖向 `py − 上屏.y`）夹进 `[value, value2]` 写回状态槽、`t == 6` 抬起 ->
  清 `DVA-DRAG`。为此 shim 新增 `SDL_EVENT_MOUSE_BUTTON_UP` -> 事件类型 **6**
  （坐标槽与按下一致；`app-poll` 的 `t = e & 0xFF` 不用改）。
  落点：`src/dv.no` 的 `dv-dock-split` + `src/dv-app.no` 的 `DVA-DRAG` /
  `dva-drag-down` / `dva-drag-move` / `dva-drag-up` / `dva-drag-of` +
  `src/shim/shim.c` 的 mouse-up 分支 + `tests/dva-dock.no`（31 项断言，见 §5.23）；
  demo 两栏行从 `dv-row` 换成 `dv-dock-split`（节点数不变，`ops` 仍 83）。
- **多行文本也是状态（阶段 30：声明式代码编辑器）**：命令式内核只有单行 `C-INPUT`，
  真实的编辑器要四样东西 —— 多行编辑、行号、语法高亮、光标。这四样**没有一样需要
  新组件种类**：`dv-code-editor(S, key, tslot, offslot, step, viewh, rowh, gutter,
  key0, parent)` 把文本按 `\n` 切行（`dv-edit-split` 保留结尾空行），每行挂一个
  `C-FORM`，行内 token 挂着色的 `C-LABEL`（关键字 = `CS-TH-ACCENT` / 字符串 =
  `CS-TH-OK` / 注释 = `CS-TH-MUTED` / 数字 = 琥珀 `DV-C-NUM`），行号挂右对齐的
  `C-LABEL`（`dv-align(g, A-RIGHT)`），光标挂定宽 2px 的 `C-SEPARATOR`，外圈套
  现成的 `dv-scroll`。**文本 / 光标 / 滚动是三份状态**：文本 = 字符串状态
  `DV-BIND-T[tslot]`（真源），光标 = dv 侧的 `DV-EDIT-L` / `DV-EDIT-C[tslot]`
  （**按文本槽存，不按视图节点**），滚动偏移 = 应用状态槽 `S[offslot]`。内容高 =
  `n*行高 + (n-1)*间隙 + 2*内边距`，经 `dv-scroll-content` 声明给布局 —— 能滚多远
  不依赖所有行都在场；行 key = `key0 + 绝对行号`，窗口滑动复用同一批组件。
  `dv-edit-follow` 用 `DV-ED-SEEN[slot]` 记住上次光标：**光标没动就不重排窗口**
  （滚轮推走的窗口不会被弹回去），光标动了才重算 `i0` 并写回偏移槽。键盘喂字在
  dv 侧（`dv-edit-key`），装配层只负责「命中 -> 焦点 = 编辑器 key」：`dva-focus`
  命中后顺 `cmp-parent-of` 上溯，用 `dv-node-by-key` 反查视图节点、`dv-edit-of`
  认它是不是 `dv-edit` 声明处，是就把**声明式 key** 记进 `DVA-EDIT-FOCUS[0]`
  （活句柄每帧随调和变，key 不变）并释放输入焦点；主循环 `t == 4` 的顺序是
  输入焦点 -> **编辑器焦点** -> 全局快捷键，所以 'q' 在编辑器里打得出来、不打字时
  仍然退出。落点：`src/dv.no` 的 `dv-code-editor` / `dv-edit-line` / `dv-edit-*` /
  `dv-lex-*` / `dv-run-push` + `src/dv-app.no` 的 `DVA-EDIT-FOCUS` /
  `dva-edit-find` / `dva-edit-keyboard` + `tests/dva-editor.no`（61 项断言，见 §5.24）；
  demo 左栏底部就是这个编辑器，点进去直接敲（`ops` 83 -> 105）。
- **模态命令面板也是状态（阶段 32）**：命令式内核没有模态；`dv-command-palette(S, key,
  openslot, qslot, offslot, cmdslot, n, texts, acts, key0, parent)` 把「有没有这个叠加层」
  也变成状态 —— `S[openslot] != 0` 时挂一个居中的 `C-FORM`（`dv-on-overlay()`），
  里面是查询输入（`dv-bind` 到 `qslot`）+ `dv-scroll` 虚拟列表（偏移真源 = `offslot`），
  行 = `C-FORM` + `C-LABEL`，挂 `DV-A-RUN = 12`（`value` = 命令码写进 `cmdslot`，
  `value2` = 关面板槽，归约器一次写两个槽）。键盘全归面板：`dva-pal-keyboard` 吃
  ↑/↓ 导航、回车执行、ESC 关闭、退格 / 可打印字符改查询串；面板自己那本跨帧的账
  （命中数 / 游标 / 命中源行表 / 上次开关）在 dv 侧 `DV-PAL-*`。落点：`src/dv.no` 的
  `dv-command-palette` / `dv-filter-map` / `dv-pal-*` + `src/dv-app.no` 的
  `DVA-PAL-FOCUS` / `dva-pal-find` / `dva-pal-keyboard` + `tests/dva-palette.no`
  （34 项断言，见 §5.26）；demo 树下方多一个 `palette` 按钮（快捷键 `p`），
  `ops` 124 -> 125。
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
- 声明式滚动（阶段 23，`tests/dva-scroll.no`）：
  `dv-scroll(S, key, offslot, step, parent)`（`C-SCROLL`，`value = S[offslot]`、
  `value2 = step`、`DV-A-CHANGE` 载荷 = 偏移槽）+ 布局 `C-SCROLL` 分支
  （两遍：累加内容高 → 夹紧 `off` 写 `CMP-SCROLL-MAX` → 按 `off` 排版）+
  绘制裁剪（`cs-emit-clip` + `CS-CLIP` + `noui_clip`）+ 命中包装
  （视口外不命中 / 视口内按全局坐标直接命中，见阶段 24 修正）+ 事件侧 `dva-wheel`（顺父链找容器、
  夹紧写回状态槽）。
  实测 dva-scroll `M..W` 全 true（33 项断言）：标记（kind / `value` = 槽 /
  `value2` = 步长 / 动作载荷 / flags；改状态重建 `ops == 0` 且 `value` 跟随）；
  布局（`off = 0` 基线、`off` 上移排版且行距不变、`cmp-scroll-max` =
  内容高 − 视口高、超界 / 负数夹紧、**夹紧不写回状态**）；
  绘制（滚动子孙指令带裁剪标记且裁剪矩形 = 视口，滚动外的兄弟不带）；
  命中（视口内换算命中正确的行、视口外不命中、从整页根顺父链命中、
  偏移变则命中目标变）；
  滚轮（滚一格 = `step`、上下界夹紧、到顶 / 到底不动、落在滚动外不动、
  容器自身空白也认、改偏移 `ops == 0` 且布局位置真的变）。
  demo 侧：右栏顶部 10 行 / 视口 160px 的滚动列表（内容约 336px，
  `cmp-scroll-max == 17600`），新增 `D-SCROLL` 槽 + `DEMO-SCROLL*` 数据，
  页头回显 `scrl=`，reset 多写 `D-SCROLL`，`dv-app-click` 的 CHANGE 分支
  收紧为只认 `C-SLIDER`（滚动的 CHANGE 走 `dva-wheel`，否则点滚动空白会把
  滑杆值写坏）；首帧 `ops` 53 → 64（容器 + 10 行），`bye … scrl=0`、`exit 0`。
- 声明式虚拟列表（阶段 24，`tests/dva-virtual.no`）：
  `dv-scroll-content` / `dv-scroll-start` 视图声明（`DV-SCROLL-CONTENT` /
  `DV-SCROLL-START`，`dv-apply-props` 无条件同步到 `CMP-SCROLL-CONTENT` /
  `CMP-SCROLL-START`）+ 布局声明分支（内容高不再实测、按 `(i0 + j) * 行距`
  绝对行号排版）+ `dv-repeat-at`（切片重复原语，绝对行 key）+ `dv-list-view`
  一行式原语 + `cs-hit` 命中修正。
  实测 dva-virtual `M..R` 全 true（36 项断言）：标记（`C-SCROLL` 的 kind /
  `value` = 槽 / `value2` = 步长 / 动作载荷 / flags；声明内容高与起始行号
  同时出现在视图节点和活节点；改状态重建 `ops == 0`）；
  布局（`cmp-scroll-max` = 声明内容高 − 视口高 = 97600、`off = 0` 与
  `off = max` 时只挂可见的 4 行（越界夹紧后剩 1 行）、行文案正确、行距 =
  行高 + `CS-GAP`、滚到底窗口是第 16..19 行且首行 `y == -2400`、超界夹紧后
  首行 `y == 15000` 且**不写回状态**）；
  绘制（滚动子孙指令仍带裁剪 = 视口）；
  命中（`off = 0` 与 `off = max` 时视口内的点都命中正确的行）；
  滚轮（滚 20 格 = 8000、窗口滑到第 1..4 行且首行 `y == 200`、滑动复用同一批
  key、两端夹紧、改偏移 `ops == 0`）；
  切片（根下直接子 = 5 个 = 5 行（无容器直接挂根）、文案 `v18` / `v19` / `''`
  越界补空、行 key = 718..722 = `key0(700) + 绝对行号 18..22`）。
  demo 侧：右栏那条列表换成 20 行 / 视口 160px 的虚拟列表
  （`dv-list-view(DEMO, 55, D-SCROLL, …, 93, rc)`，内容高 736px，任意时刻
  只挂 5 行），首帧 `ops` 64 → 59，`bye … scrl=0`、`exit 0`（连跑两次一致）。
- 声明式过滤列表（阶段 25，`tests/dva-filter.no`）：
  `dv-filter-map` / `dv-filter-count`（查询 → 命中行号表 / 命中数，大小写不敏感，
  空查询命中全部）+ `dv-repeat-mapped`（`dv-repeat-at` 的「按下标表取行」版，
  行 key = 源行号）+ `dv-filter-list` 一行式原语 + `dv-win-range`（窗口计算
  抽成公共原语，`dv-list-view` 也改用它）+ `cs-layout` 空滚动容器修正。
  实测 dva-filter `M..R` 全 true（46 项断言）：标记（`C-SCROLL` 的 kind /
  `value` = 槽 / `value2` = 步长 / 动作载荷 / flags；声明内容高同时出现在视图
  节点和活节点）；过滤（空查询 = 全量 20 行；`alpha` 与 `ALPHA` 都命中 5 行、
  `eta` 命中 `beta 5..9`、`1` 命中 11 行（`alpha 1` + `gamma 10..14` +
  `delta 15..19`）、`zzz` 命中 0 行 → 挂 0 行 / 内容高 0 / `maxoff` 0、
  `gamma 1` 时窗口第 0 行是源第 10 行且 `value == 10`（值按**源下标**取，
  不是窗口位置））；布局（内容高随命中数变 = 30600 / `maxoff` = 10600、行位 =
  窗口序号 × 行距（首行 `y == 2400`、次行 `2400 + 5800`）、滚到底窗口滑到源
  11..14 且首行 `y == -2400` / 末行 `y == 15000`、超界夹紧后首行 `y == 15000`
  且**不写回状态**）；绘制（可见行带裁剪 = 视口）；命中（`off = 0` 时点源第 6 行、
  `off = max` 时点源第 8 行，都命中正确的行；视口外不命中）；滚轮（滚 20 格 =
  8000、窗口滑到源 6..9 且首行 `y == 200`、同一批行复用（`ops == 0`）、两端夹紧）；
  映射（`dv-repeat-mapped` 按下标表 `[3,1,0]` → key 703/701/700、文案
  `alpha 3` / `alpha 1` / `alpha 0`；表长 2 而 `count` 5 → 只挂 2 行、key
  819/818）。
  demo 侧：页头加搜索框（`dv-input(13, …)` + `dv-bind(sinp, D-QUERY, 0)`，
  新增字符串槽 `D-QUERY = 1`、`dv-bind-t-ensure(2)`），右栏列表换成
  `dv-filter-list(…, D-QUERY, 93, rc)`，页头回显 `match=N`，reset 多写
  `dv-bound-set(D-QUERY, '')`，首帧 `ops` 59 → 60，`bye … scrl=0`、`exit 0`。
- 声明式排序 / 分组（阶段 26，`tests/dva-order.no`）：
  `dv-sort-run` / `dv-sort-map`（键 → 稳定排序的下标表）+ `dv-group-plan`
  （下标表 → 混合行表，token < 0 = 组标题）+ `dv-repeat-plan`（按混合行表挂行 /
  挂标题）+ `dv-order-list` 一行式原语（mode 0/1/2/3/4）。
  实测 dva-order `S..W` 全 true（43 项断言）：排序（源序 / 键升
  `[0,7,4,1,8,5,2,9,6,3]` / 键降；**稳定** —— 同键保持源序；`n` 夹到
  `keys.len()`；负数 `n` → 空表；非法 mode 7 → 源序）；分组（`on = 0` 复制、
  分组 token `[-1,5,6,7,8,9,-2,0,1,2,3,4]`、**组序跟着 plan 走**（源序 plan →
  组 2 在前）、空 plan、`n = 0`、plan 短于 `n`）；布局（源序内容高 37600 /
  `maxoff` 21600，分组 44800 / 28800；mode 1/2/3/4/9 的窗口与文案；标题文案 /
  key / 禁用 + `CS-TH-MUTED`、数据行 `CS-TH-FG`；行位 `y == 2400` / `6000`；
  **换模式按源行号复用组件**（`ops` 不涨）；分组滚到近底 → 6 行
  `[组1标题, 行0..4]` 且行 9 不挂；超界夹紧到 `maxoff` 且不写回，窗口只剩混合表
  最后一行）；绘制（可见行带裁剪 = 视口）；命中（点数据行 → `DV-A-CLICK`
  载荷 = 源行号、点标题 → `(0,0)`、视口外不命中 —— 命中根用滚动容器，表单本身是
  个大矩形）；滚轮（滚 20 格 = 8000、窗口 `[6,7,8,9,-2]`、两端夹到分组 `maxoff`、
  切回键升后上限缩到 21600）。
  demo 侧：右栏列表换成 `dv-order-list(…, D-SORT, 93, 9000, rc)`，上方加
  「order」下拉 `dv-select-from(DEMO, rc, 56, D-SORT, D-SOPEN2, SORT-OPTS, 'order', 57)`
  （键乱序 `(i*7)%20`、组号 `i/10`，排序 / 分组都真的改变行序），新增
  `D-SORT = 18` / `D-SOPEN2 = 19`（`with-len(20)`）与 `SORT-OPTS`，页头回显
  `sort=`，reset 多写两个槽；搜索框与 `match=` 撤掉（过滤与排序是两根轴，demo
  一次只演一根），首帧 `ops` 60 → 65，`bye … sort=0`、`exit 0`。
- 声明式表单校验（阶段 27，`tests/dva-valid.no`）：
  `DV-R-REQ/MINL/MAXL/MINV/MAXV/EQ/HAS`（= 1..7）+ `dv-rule-check`（单字段单
  规则表 → 第一条违反的规则号，**全局下标 + 1**）+ `dv-rule-msg`（规则号 → 文案，
  文案挂规则不挂字段）+ `dv-form-bad`（多字段汇总坏项数）+ `dv-field`（一行 =
  标签 + 输入绑字符串槽 + 条件错误行，`show` 是揭示槽）。
  实测 dva-valid `V..H` 全 true（46 项断言）：规则（必填 / 最短 / 最长 / 最小值 /
  最大值含**不可解析算违反** / 与另一槽相等 / 含子串；`trim` 生效；多规则取第一条；
  `base + n` 切片；`n = 0`；越界 `base` 吞掉；未知种类忽略）；汇总（全过 / 坏 1 /
  坏 2 / 全坏；`nf = 0`；`nf` 超表长按表长；空规则字段不算坏）；视图（揭示槽 0 →
  错误行**不存在**；揭示后出现且文案正确 + 危险变体；合法时不建；输入确实绑到
  字符串槽；提交按钮 flags = f(坏项数)；改值重建同一输入 key 复用同一组件对象；
  同状态重复 render 零操作）；绘制（揭示后错误文案真的画出来；未揭示时场景里
  没有它）；命中（禁用提交被命中也不出动作；合法时提交 = `DELTA(D-SUB)`）。
  demo 侧：左栏换成 `dv-scroll(DEMO, 91, D-LSCROLL, 400, cols)` +
  `dv-size(lc, 30000, 0)` 滚动容器 + `dv-form(310, lc)` + 4 × `dv-field(...)`
  + 状态标签 + 主色提交按钮（`DEMO-CMD-SUBMIT`：不合格写 `D-VSHOW = 1`、
  合格 `D-VSUB + 1` + 通知），新增 `D-VSHOW/D-VSUB/D-LSCROLL` 槽与四个字符串槽，
  页头回显 `vsub=` / `lsc=`，reset 多写三个槽 + 清空四个字符串，
  首帧 `ops` 65 → 76，`bye … vsub=0 lsc=0`、`exit 0`。
- 声明式虚拟表格（阶段 28，`tests/dva-table.no`）：
  `dv-table-rows(parent, key0, hkey0, ckey0, start, count, ncols, colws, tw, plan,
  names, cells, acts, pays, hcolor, rowh)`（混合行表 → 窗口内的表格行：`token < 0`
  挂通栏组标题（`key = hkey0 + 组号`、`names[组号]`、禁用 + `hcolor`、宽 = `tw`），
  `token >= 0` 挂 `C-GRID` 行 + `ncols` 个单元格（`key = ckey0 + 源行号*ncols +
  列号`），行动作 / 载荷按源行号取；`start` 夹进 `[0, plan.len()]`、`count` 再按
  剩余可挂行数夹，两端越界一行不挂）+
  `dv-table-list(S, key, hkey, offslot, step, viewh, rowh, plan, ncols, colws,
  headers, names, cells, acts, pays, key0, ckey0, hkey0, hcolor, parent)`
  （表头 `C-GRID` 不滚 + `dv-scroll` + 可见窗口的行；表宽自己算；表头行高声明为
  `CS-PAD + CS-LINE-H`）。
  实测 dva-table `R..H` 全 true（32 项断言）：行（3 行 2 列：行 kind = `C-GRID`、
  每行 2 个单元格、文案 = `cells[源行号*列数 + 列号]`、行宽 = 声明表宽、行高 =
  `rowh`、列宽 = `colws`；乱序 `plan` 的行号映射；通栏组标题文案 / 禁用 / 占满
  表宽；`count` 夹到混合表长；`start` 越界 → 一行不挂）；列表（表头两个单元格 +
  滚动容器；声明内容高 20 行 = 73600、`maxoff` = 63600，分组 22 行 = 80800 /
  70800；窗口切片；状态驱动偏移；换偏移复用同一批行；同状态重复 render 零操作；
  空 plan → 内容高 0 / `maxoff` 0 / 0 行）；绘制（行指令带裁剪矩形 = 滚动视口）；
  命中（点单元格命中该行；派发 = `DV-A-SET` + 载荷 = 7）。
  demo 侧：右栏列表换成 `dv-table-list(DEMO, 55, 920, D-SCROLL, …, tplan, 2,
  DEMO-TABLE-COLW, DEMO-TABLE-HEADERS, DEMO-SCROLL-GNAME, DEMO-TABLE-CELLS, …, 93,
  900, 9000, CS-TH-MUTED, rc)`（`name` / `key` 两列，行主序单元格表），顺序链抽成
  `dv-demo-order-plan(S)` 纯函数复用阶段 26 的五档模式；左栏让宽 300px → 220px、
  右栏视口 160px → 112px（给表头 36px 与「order」下拉 40px 让位，见 §5.22），
  首帧 `ops` 76 → 83，`bye … sort=0`、`exit 0`。
- 声明式可拆面板（阶段 29，`tests/dva-dock.no`）：
  `dv-dock-split(key, slot, minw, maxw, gw, parent)` = `dv-node(C-DOCK, …)` +
  `dv-lay(h, dv-lay-h(), gw, CS-PAD)` + `dv-act(h, DV-A-CHANGE, slot)`，并把
  `minw` / `maxw` 落到 `value` / `value2`（后者要顺手在 `dv-apply-props` 里补一条
  `(kd == C-DOCK) -> { cmp-set-value2(L, dv-value2(v)) }`，否则事件侧读到 0）。
  事件侧 `dva-drag-down(S, root, px, py)`：命中是 `C-DOCK` / `C-RESIZABLE` 且其
  动作是 `DV-A-CHANGE` 时，取第 0 个流子节点矩形，横向判 `px ∈ [x0+w0, x0+w0+gap]`
  （竖向判 `py ∈ [y0+h0, y0+h0+gap]`）——**间隙就是拖杆**；命中成立写
  `DVA-DRAG[0] = dock`。`dva-drag-move(S, px, py)`：`nv = px − x0`（竖向
  `py − y0`）→ `dv-clamp(nv, cmp-value(d), cmp-value2(d))` → 与当前槽值不同才
  `dva-set`。`dva-drag-up()` 清 `DVA-DRAG[0] = -1`。主循环：`t == 2` 先试抓住，
  抓住则**整个点击归约块跳过**；`t == 3` 末尾追加 `dva-drag-move`；新增 `t == 6`
  （mouse-up）分支；退出前也 `dva-drag-up()` 防悬挂。
  实测 dva-dock `M..R` 全 true（31 项断言）：标记 8 项（kind = `C-DOCK`、`L-H`
  在位、`gap` = 声明值、`value`/`value2` = 上下限、动作与载荷、节点可见可用、
  改状态重建零操作、重建后上下限不漂移）；布局 6 项（左屏 x/宽/高、右屏 x =
  左屏右缘 + `gap`、右屏右缘 = 容器右缘 − 内边距、顶对齐同高）；命中 4 项
  （点左屏 → 左屏；点间隙 → dock；点内边距 → dock；点容器外 → 非 dock）；
  拖拽 7 项（抓住且 `dva-drag-of` = dock；`px = 20000` → 17600；下限夹 8000；
  上限夹 28000；松手后不动；面板内不抓；内边距不抓）；竖拆 4 项（上屏 y/高、
  抓住、`py` 换算 17600、只动 `px` 值不变）；重建 2 项（零结构操作、新宽 =
  新状态）。
  demo 侧：两栏行 `dv-row` → `dv-dock-split(DEMO-DOCK-KEY, D-DOCK, 12000, 42000,
  800, page)`，左栏 `dv-scroll` 宽 = 状态槽（初值 22000），右栏 `dv-fill` grow，
  状态标签加 `dock={dk}`，`bye … dock=22000`、`exit 0`（节点数不变，`ops` 仍 83；
  `gap` 800 比原来的 1600 省 8px 给右栏）。
- 声明式代码编辑器（阶段 30，`tests/dva-editor.no`）：
  `dv-lex-line(line)`（扫描一行 -> 平行的 `DV-RUN-T` / `DV-RUN-C`：`;` 到行尾是注释、
  配对引号（含未闭合）是字符串、连续数字、连续标识符（查 `,kw,` 关键字表）、连续
  空白，其余单字符；`dv-run-push` 把**相邻同色**并成一段）+ `dv-edit-split` /
  `dv-edit-join`（`\n` 分段 / 拼回，**保留结尾空行**）+ `dv-edit-set` /
  `dv-edit-insert` / `dv-edit-backspace` / `dv-edit-enter` / `dv-edit-del-line` /
  `dv-edit-ins-line` / `dv-edit-move`（左右 / 上下 / **跨行**，跨行时按「上一行末尾」
  而不是「行首」落光标）+ `dv-edit-key`（键码 -> 字符：可打印 ASCII 查 `ED-PRINT`
  表；左右 / 上下 / 回车 / 退格 / Tab / 普通字符）+ `dv-edit-follow`（`DV-ED-SEEN`
  记住上次光标，**光标没动不重排窗口**）+ `dv-edit-line`（一行 = `C-FORM` + 右对齐
  行号 `C-LABEL` + 逐 token 的 `C-LABEL`；光标落在某个 token 里时，那个 token 拆成
  `前缀 + 2px 的 C-SEPARATOR + 后缀`）+ `dv-code-editor(S, key, tslot, offslot, step,
  viewh, rowh, gutter, key0, parent)`（`dv-scroll` + `dv-size(h, 0, viewh)` +
  `dv-scroll-content`（内容高 = `n*行高 + (n-1)*间隙 + 2*内边距`）+ `dv-scroll-start`
  + `dv-edit`（节点 -> 文本槽映射）+ 内容 `C-FORM` + 可见窗口内的行）。
  状态：文本 = `DV-BIND-T[tslot]`；光标 = `DV-EDIT-L` / `DV-EDIT-C[tslot]`（**按文本槽
  存**，`dv-clear` 不清）；窗口 = `S[offslot]` + `DV-ED-SEEN[slot]`。
  事件侧：`DVA-EDIT-FOCUS`（1 槽，存**声明式 key**）+ `dva-edit-find(hit)`（顺
  `cmp-parent-of` 上溯，`cmp-key` -> `dv-node-by-key` -> `dv-edit-of` 认声明处）+
  `dva-edit-keyboard(kk)`；`dva-focus` 里「命中到编辑器」这条支路要**释放输入焦点**
  并清 `DVA-FOCUS`；主循环 `t == 4` 的优先级是 输入焦点 -> 编辑器焦点 -> 全局快捷键
  （否则 'q' 打不出来）。
  实测 dva-editor `T..A` 全 true（61 项断言）：着色 18 项（空行 / 注释 / 字符串 /
  数字 / 关键字 / 标识符 / 空白 / **相邻同色合并** / 引号配对与未闭合 / 字节越界）；
  行模型 6 项（切拼往返，含 `'a\nb\n'` 的 3 行与 `''` 的 1 行）；编辑 13 项（左右 /
  插入 / 退格 / 回车 / 上下 / **Tab 光标前进 4** / 跨行左移落到上一行末尾 / 跨行退格
  并入上行 / 行首退格到顶不吞字符 / 空文本）；窗口 3 项；跟随 4 项（不动不重排、
  动了才重排、重复稳定、强制偏移不被覆盖）；装配 17 项（结构 / 内容高 / 行号 /
  命中上溯 / 焦点交接 / 喂字 / 释放焦点 / 空文本 / 重建零操作）。
  demo 侧：左栏底部 `dv-code-editor(DEMO, 960, D-ED-TEXT, D-ED-OFF, 3200, 14400,
  2400, 4800, 9700, lc)` + `dv-size(ed, dk, 14400)` + `editor` 标题标签，初始文本
  `'let name = "noui"\n; a comment\nlet n = 42\nlet ok = 1'`，新增 `D-ED-OFF = 24`
  与字符串槽 `D-ED-TEXT = 6`，页头回显 `edoff=`，首帧 `ops` 83 -> 105、`bye …
  edoff=0`、`exit 0`（见 §5.24）。
- 声明式树（阶段 31，`tests/dva-tree.no`）：
  `dv-tree-flat(S, openslot, n, kids0, kids, parents, out, deps)`（**显式栈的迭代式
  先序** —— 语言没有可用递归，深度也不该由数据决定；把展开态作用到树上，产出
  **可见行表** + 每行层级；数据形状 = 「先序序列 + 兄弟块相邻」，节点 `i` 的孩子
  是下标 `kids0[i] .. kids0[i]+kids[i]-1`，和 DOM 的扁平序列化同构；栈用
  `with-len(n)` + `sp` 下标 + `dv-arr-set` / `dv-arr-get` 模拟，避开 `vec.pop()`
  的 `%option_i64`）+ `dv-tree-row(parent, key0, v, d, texts, kids, S, openslot,
  selslot, rowh)`（一行 = `C-FORM` 行（`value` = 节点号、`DV-A-SET(selslot)`）+
  右对齐「缩进 + 箭头」`C-LABEL`（宽 = `层级*缩进 + 箭头宽`，有孩子才给 `v` / `>`，
  叶子留空但**仍占位**好让文本起点对齐，挂 `DV-A-TOGGLE(openslot + 节点号)`）+
  文本 `C-LABEL`；选中态三处一起变 = `dv-on-sel()` / 行底色 `CS-TH-TRACK` /
  文本色 `CS-TH-ACCENT`）+ `dv-tree-view(S, key, offslot, step, viewh, rowh, n,
  texts, kids0, kids, parents, openslot, selslot, key0, parent)`（一行式树视图 =
  `dv-scroll` + `dv-size` + `dv-scroll-content`（内容高 = `m*行高 + (m-1)*间隙 +
  2*内边距`）+ `dv-scroll-start` + `dv-win-range` + 逐行 `dv-tree-row`，偏移真源
  是状态槽 `offslot`，行 key = `key0 + 节点号*10000000` 按**源节点号**）。
  动作码 `DV-A-FILL = 11`：一次把 `S[payload .. payload+value2)` 全写成 `value`
  （`value2 <= 0` 不写、回执 0）—— 「全部展开 / 全部收起」只是同一动作的两个操作数。
  实测 dva-tree `Z..E` 全 true（42 项断言）：标记 16 项（可见行表 `[0,1,5,6,7,2,3,4]`
  / 层级 / 行 + 箭头 + 文本结构 / 箭头宽度与文案 / 叶子无动作仍占位 / 内容高 27200 /
  选中态三处一致）；调和 5 项（首帧 `1+8*3` / 换选中**零结构操作**且颜色跟随 /
  收起子树才增删 / 再展开回来）；派发归约 10 项（箭头 `TOGGLE(openslot+节点)` /
  行 `SET(selslot)` / 叶子箭头 `dispatch = 0` / `FILL` 全展开 13 槽 / 全收起只剩根 /
  `value2 <= 0` 不写）；端到端 11 项（`cs-hit` 命中行与箭头 -> `dv-dispatch` ->
  `dv-apply` -> 重建 -> 活树跟随 / 偏移 100px 只挂可见 5 行 / 全展开 13 行且能滚 /
  选中行文本色）。
  demo 侧：左栏顶部 `dv-label(89, 'tree', lc)` + `dv-tree-view(DEMO, 980, D-TOFF,
  3200, 16800, 2400, 13, DEMO-TREE-TEXTS, DEMO-TREE-K0, DEMO-TREE-KN, DEMO-TREE-PAR,
  D-TOPEN, D-TSEL, 9800, lc)` + `dv-size(tr, dk, 16800)`，下方 `expand all` /
  `collapse all` 两个幽灵按钮（同一个 `DV-A-FILL`，`value` 一个 1 一个 0），
  13 个节点就是 noui 自己的目录；新增 `D-TOPEN = 25` / `D-TSEL = 38` /
  `D-TOFF = 39`（`with-len(40)`）与 `DEMO-TREE-*`，文案数组 `DEMO-TREE-TEXTS`
  用 `with-len(13)` + 逐项写（**模块级 `[]str` 字面量在函数体里是空的**，见 §5.25），
  页头回显 `tsel=` / `toff=`，reset 多写展开槽 + `D-TSEL = -1` + `D-TOFF = 0`，
  启动处显式写 `DEMO[D-TOPEN] = 1` / `DEMO[D-TOPEN+1] = 1` / `DEMO[D-TSEL] = -1`
  （demo 不走 reset 命令）；首帧 `ops` 105 -> 124、`bye … tsel=-1 toff=0`、`exit 0`
  （见 §5.25）。
- 声明式命令面板（阶段 32，`tests/dva-palette.no`）：
  `dv-filter-map(qslot, n, texts, idxs)`（查询串 -> 命中的**源行号**表；空查询命中全部，
  大小写不敏感，`S` 里的查询是 `dv-bound(qslot)` 读的）+ `dv-pal-keys-ensure` /
  `dv-pal-of` / `dv-pal-open-key` / `dv-pal-move` / `dv-pal-cur-key` / `dv-pal-reset` /
  `dv-pal-close` / `dv-pal-type` / `dv-pal-backspace` + `dv-command-palette(S, key,
  openslot, qslot, offslot, cmdslot, n, texts, acts, key0, parent)`（`S[openslot]` 为 0 时
  只登记 key / 槽映射并做**开关翻转检测**（`DV-PAL-WAS`，翻转就把游标归 0、偏移写回 0），
  不为 0 时建面板：`dv-on-overlay()` 的 `C-FORM`（内边距 `dv-lay(box, 0, 0, 2*CS-PAD)`、
  高 = `2*CS-PAD + 行高 + CS-GAP + 视口高`）+ 查询 `dv-input`（`dv-bind(qslot)`）+
  `dv-scroll`（`dv-lay-grow()`）+ `dv-scroll-content`（内容高 = `m*行高 + (m-1)*间隙 +
  2*内边距`）+ `dv-scroll-start` + 游标夹紧 + 逐行 `C-FORM`（`value` = 命令码、
  `value2` = `openslot`、`dv-act(DV-A-RUN, cmdslot)`）+ 文本 `C-LABEL`（同一个动作））。
  动作码 `DV-A-RUN = 12`：`dv-apply` 先写 `S[payload] = value`，`value2 >= 0` 时再写
  `S[value2] = 0` —— 「记住命令 + 关面板」两个槽一次写完。面板侧跨帧账本：
  `DV-PAL-CUR[面板]` / `DV-PAL-M[面板]` / `DV-PAL-SRC`（命中源行号的**只增扁平副本**）+
  `DV-PAL-SB[面板]`（该面板在副本里的基址）。
  事件侧：`DVA-PAL-FOCUS`（1 槽，存**声明式 key**，`-1` = 没有面板）+ `dva-pal-find(hit)`
  （活父链上溯到 `dv-command-palette` 声明处）+ `dva-pal-keyboard(S, kk)`（↑/↓ 走
  `dv-pal-move`；回车 = `dv-pal-cur-key` -> `dv-dispatch` -> `dv-app-reduce` -> 命中则
  `DVA-CLICKS + 1` -> `dv-app-click`；ESC = `dv-pal-close`；退格 / 32..126 = 改查询串，
  字符查 `ED-PRINT.slice(kk-32, kk-32+1)`）；`dva-focus` 里「命中到面板」这条支路**释放
  输入焦点 / 编辑器焦点**并清两个 `DVA-FOCUS`；主循环 `t == 4` 的优先级是
  **面板焦点 -> 输入焦点 -> 编辑器焦点 -> 全局快捷键**；每帧建完视图后
  `DVA-PAL-FOCUS[0] = dv-pal-open-key(S)`，面板开着时顺手释放输入 / 编辑器焦点
  （点面板里的输入框也不会抢走焦点）。
  实测 dva-palette `Z..E` 全 true（34 项断言）：标记 14 项（登记 / 开关槽映射 / 面板结构 /
  内容高 30400 / 命中数 / 游标 / 输入框占位 / 关闭后子树消失）；调和 6 项（首帧
  `3 + 8*2` / 过滤成 2 行有增删 / **游标移动零结构操作** / 退格回空查询 8 行全回来 /
  关闭有结构操作）；派发归约 7 项（`RUN` 写命令码 + 清开关槽 / 过滤计数 / 空命中游标 -1 /
  查询串追加退格）；端到端 7 项（`cs-hit` 命中行 -> 派发 -> 归约 / 面板焦点 -> 下移 ->
  回车执行 / ESC 关闭）。
  demo 侧：树下方 `palette` 幽灵按钮（key=83，`dv-act(DV-A-SET, D-PAL-OPEN)` +
  `dv-hotkey(112, ...)`）+ `dv-command-palette(DEMO, 99, D-PAL-OPEN, D-PAL-Q, D-PAL-OFF,
  D-PAL-CMD, 8, DEMO-PAL-TEXTS, DEMO-PAL-ACTS, 2000, page)`（父 = 根表单，所以是居中
  模态；文案 `new file` / `open file` / `save` / `save as` / `close tab` /
  `find in files` / `toggle terminal` / `quit`，命令码 101..108），页头回显 `pal=`、
  `bye` 也带 `pal=`，reset 多写「关面板 + 清命令码 + `dv-bound-set(D-PAL-Q, '')` +
  `dv-pal-reset`」，显式命令分支在 `D-PAL-CMD` 被写时补一个 toast（归约器表达不了
  「顺带弹个通知」）；`DEMO` 池 40 -> 44（`D-PAL-OPEN = 40` / `D-PAL-OFF = 41` /
  `D-PAL-CMD = 42`）、`dv-bind-t-ensure(7)` -> 8（`D-PAL-Q = 7`），行 key 基数 2000
  （行 key = 2000 + 源行号*100，避开编辑器 960 / 9700+ 与树 980 / 9800+），
  首帧 `ops` 124 -> 125（多的那 1 个是 `palette` 触发按钮；面板关着时只登记 key、
  不建子树）、`bye … toff=0 pal=0`、`exit 0`（见 §5.26）。
- 命令式树（阶段 33，`tests/cmp-tree.no`）：`src/components.no` 补 `cmp-tree-*` 与
  一组 `ct-*`（components 内部的定长小工具：`ct-get` / `ct-set` / `ct-add` /
  `ct-sub` / `ct-inc` / `ct-mul`，全部走 `?T` 解包或 `#{overflow}` 显式处理）。
  模型池 `TR-PAR` / `TR-FIRST` / `TR-NEXT` / `TR-OPEN` / `TR-ROW-NODE` /
  `TR-ARROW` / `TR-TSTART` / `TR-TCOUNT` 与既有的 `TR-ALL`（节点句柄扁平表）一起
  随 `cmp-alloc` 逐槽 push（**新下标 = 句柄**），树自己的节点表按段存在 `TR-ALL`，
  段界记在 `TR-TSTART[树]` / `TR-TCOUNT[树]`。
  `cmp-tree()` 的 `value` = -1（= 没选中任何节点），选中存 `CMP-VALUE[树]`。
  `cmp-tree-add(h, parent, text)` 建节点 + 挂兄弟链 + 追加进本树段；
  `cmp-tree-flat(h, out, deps)` 用**显式栈的迭代式先序**把展开态作用到模型上，
  产出可见行表 + 每行层级（收起 = 子树整块不出现）。
  `cmp-tree-row-head` / `cmp-tree-row-body` / `cmp-tree-row` 把一行建成
  `C-FORM`（`L-H`、行高 2400、无内边距无间隙）+ 右对齐「缩进 + 箭头」`C-LABEL`
  （宽 = `层级*2400 + 1600`）+ 文本 `C-LABEL`；选中态三处一起变（行 flags
  `F-SELECTED` / 行底色 / 文本色）。
  `cmp-tree-sync` 两段式重建（先挂全部行壳、再逐行填内容），
  `cmp-tree-row-of` 从 `cs-hit` 的最深命中沿 `CMP-PARENT` 上溯到行。
  实测 36 项断言全绿（`ctor` 3 / `model` 4 / `flat` 8 / `sync` 8 / `select` 3 /
  `resync` 3 / `hit` 3 / `isolate` 4）；窗口 demo `tests/main-tree.no`
  900 帧 `bye frames=900 clicks=0`、exit 0，另用一次性探针在真实布局坐标下
  验证 4 行 + 箭头槽的 `cs-hit` 全部能上溯回行（`checks=5 bad=0`，见 §5.27）。
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

### §5.17 阶段 23 滚动新坑（2026-11-20 实测）

- **`->` 匹配链不是互斥的，后跑的赢**：`cs-layout` 里 `C-DIALOG` 分支跑完，
  紧接着的通用容器分支会再跑一遍，把 dialog 的子节点按「无标题偏移」重排，
  通用结果覆盖前面 —— 这是既有行为（`cs-paint` 的通用分支早就显式写了
  `kind != C-DIALOG`，说明作者知道）。阶段 23 给 `C-SCROLL` 加完分支后，
  滚动偏移被通用分支当成 0 重排一次，表现是「布局完全不理 `off`」。
  修法：通用分支显式加 `&& (kind != C-SCROLL)`。**不要**顺手把 dialog 也排除，
  那会改动既有行为。
- **几何的「真源」只能是状态槽**：布局夹紧 `off` 后只用于本次排版，
  **不能写回状态**。写回发生在事件侧（`dva-wheel`），否则布局会变成隐藏的
  状态写者：测试里「状态写 99999 → 布局夹紧 → 状态还是 99999」就是钉这条的。
- **事件编码装不下负数**：`app-poll` 的 x/y 槽各 24 位掩码，直接放滚轮增量
  会把负数（上滚）吃掉。绕法：x/y 放**光标位置**（命中要用），有符号增量
  夹到 int8 放 bit56..63，Nolang 侧 `(e >> 56) & 0xFF` 再减 256 还原补码。
  SDL3 用 `SDL_MouseWheelEvent.integer_y`（整格 tick，≥ 3.2.12），避开
  `float y` 的截断。
- **裁剪矩形要「每条指令自带」**：`cs-emit-rect` / `cs-emit-fill` /
  `cs-emit-text` 三个发射器统一调 `cs-emit-clip`，把当前裁剪记进
  `CS-CF/CX/CY/CW/CH` 池；`app-draw-scene` 按指令逐个 `noui_clip` /
  `noui_clip_clear`。滚动容器自己只发一条背景矩形，然后推 `CS-CLIP` 画子树，
  画完恢复 —— 这样嵌套裁剪是栈式的，不需要额外状态。
- **命中要换算坐标，不能只判断矩形**：视口外的点直接不命中（与裁剪一致），
  视口内的点再走 `cs-hit-inner`。**注意别换算第二次**：子节点的矩形本来就
  是带 `-off` 的全局坐标（布局已经减过 `off`），所以视口内直接按全局坐标
  命中即可 —— 阶段 23 写成 `py + off` 导致大偏移时视口内怎么点都不命中，
  阶段 24 修掉（`tests/dva-scroll.no` 的命中断言点随之从 `py = 2200` 改成
  `py = 7000`）。嵌套滚动靠 `cs-hit-inner` 里递归调 `cs-hit`（而不是
  `cs-hit-inner`）天然各自判视口矩形。
- **`C-SCROLL` 必须在 `cs-is-container` 白名单里**，否则子节点不参与流布局。
- **滚动容器也会挂 `DV-A-CHANGE`**（载荷是偏移槽，`dv-scroll` 的语义完整），
  但 `dv-app-click` 的 CHANGE 分支是给滑杆写的几何换算，必须收紧成
  `cmp-kind(live) == C-SLIDER` —— 否则点一下滚动空白就会把滑杆值写坏。
  滚动的几何归约走装配层的 `dva-wheel`（与 `dva-run` 的 `t == 5` 分支配套）。
- **`dv-render(live, view)` 挂的是 view 的**子节点**，不是 view 本身**：
  页面节点（`dv-form(1, -1)`）从不进活树。探针里 `dv-live-deep(root, 1)`
  返回 −1 不是 bug；查页面内容的 key 才能查到。
- **`dv-apply-props` 的 `VALUE2` 同步要显式加 kind 分支**：`C-SLIDER` /
  `C-GRID` / `C-TABLE` / `C-PAGINATION` / `C-RATING` 之后必须补
  `C-SCROLL`，否则 `dv-scroll` 的 `step`（放 `value2`）永远是 0，
  表现是「滚轮每格走 0px，像没反应」。
- **赋值给函数形参会被静默忽略**：`dva-wheel` 里要改 `sh` 必须用局部拷贝，
  不能指望形参回写。

### §5.18 阶段 24 虚拟列表新坑（2026-11-20 实测）

- **未解析的模块级全局不会让 vet / build 报错**：`dv-repeat-at` 里把
  `DV-EMPTY-F` 写成 `DV-EMPTY_F`（下划线），编译照样过，运行时那个全局被
  静默当成坏的 `str`，`.len()` / 下标直接段错误（而且是**偶发**的 —— 同一
  份二进制连跑两次，一次过一次挂）。定位靠逐段注释 + 二分。教训：新引用一个
  全局前先 `grep` 确认拼写；`vet` 干净 ≠ 运行时干净。
- **布局要按绝对行号排，不能按「第几个儿子」排**：第一版声明模式里
  `cy = y + pad - off` 之后顺序累加，窗口滑到底时第 16..19 行被当成
  第 0..4 行排到视口顶部（探针里 `y16 == -95200`，应该是 `-2400`）。
  修法是加「首个挂载子的绝对行号」（`DV-SCROLL-START` / `CMP-SCROLL-START`），
  起点改成 `cy = y + pad - off + i0 * 行距`。
- **两套行距会打架**：窗口公式用 `rowh + CS-GAP`，而布局实测子节点用的是
  子节点自身高度。所以 `dv-repeat-at` 在 `rowh > 0` 时给每行
  `dv-size(nh, 0, rowh)`，让实测行距 == 声明行距；否则窗口边界和实际位置
  错开半行，滚到底时最后一行露不出来。
- **行 key 必须用绝对行号**：`dv-repeat-at` 的行 key = `key0 + 绝对行号`。
  窗口从 0..4 滑到 1..5 时，`dv-sync` 按 `(key, kind)` 复用同一批组件，
  只改文案 —— 如果 key 用「窗口内序号」，每次滑动都是 5 删 5 增的结构操作。
- **`dv-render` 只挂子节点**（阶段 23 那条在这里又踩一次）：`R` 场景里
  想验证「根下直接子 = 5 个」，用 `dv-live-deep(root, 1)` 查页面节点会得到
  −1（页面 `dv-form(1, -1)` 从不进活树）。改成用 `cs-child-flow` 数根的孩子。
- **声明同步要无条件、幂等**：`dv-apply-props` 末尾对
  `CMP-SCROLL-CONTENT` / `CMP-SCROLL-START` 的同步不带 kind 判断（0 = 没
  声明），所以普通 `dv-scroll` 每帧被写成 0 也安全；`dv-clear` / `dv-node`
  要同步补 0，否则数组长度和节点数错位。

### §5.19 阶段 25 过滤列表新坑（2026-11-20 实测）

- **空滚动容器会跳过整个布局分支**：`cs-layout` 的 `C-SCROLL` 分支原来要求
  `n > 0`（有子节点）才进去。过滤列表命中 0 行时 `decl == 0 && n == 0`，
  分支被整个跳过 —— `cmp-scroll-max` 留着上一次的旧值（实测 10600），
  于是滚轮按旧上限夹紧，清空查询后列表还停在旧偏移上。修法：去掉 `n > 0`
  条件（内容高 = 上下 padding，`maxoff` 夹到 0），实测子节点那段仍带
  `n > 0`（没有子节点就不该减末尾 gap），第二遍排版改成
  `(decl > 0) && (n > 0)`（`n == 0` 时 `cs-child-flow(h, 0)` 是 −1）。
- **下标表比 `count` 短会建出一堆同 key 的行**：`dv-repeat-mapped` 原来
  对越界的 `j` 一律兜底成 `idx = 0`，`count = 5` 而表长 2 时后 3 行都是
  `key0 + 0` —— `dv-sync` 按 `(key, kind)` 复用，互相覆盖，活树里只剩 3 个
  孩子。修法：`lim = min(count, idxs.len())`，表不够就只挂到表尾。
- **函数里引用「顶层运行时句柄」会拿到错的值**：把 `root = cmp-root()` 存成
  顶层常量后，在**函数体内**直接引用它，`dv-live-deep` 恒返回 −1（实测
  `cm-noarg()` = 0，而 `cm3(root)` 传参 = 4）；同一份代码把它当**参数**传
  进去就正常。所以测试 / 应用里凡是要在函数里用的活树句柄，一律走参数，
  别在函数体里直接读顶层常量（`VL-*` 这类纯字面量常量不受影响）。
- **`str.index` 的空串语义正好可用**：`''.index('') == 0`，所以「空查询命中
  全部」不需要特判；两边都 `to-lower` 就是大小写不敏感。注意 `str.index`
  找不到返回 −1，判断要写 `>= 0`。
- **`print` 的格式字段只能是简单标识符**：`print('x={cmp-text(h)}')` 报
  `unresolved format field`。要么先赋给变量，要么改字段名（`key` 是保留字，
  `str.format` 那版也一样）。

### §5.20 阶段 26 排序 / 分组新坑（2026-11-21 实测）

- 🔴 **两套句柄 / 标志位空间**（本阶段最容易踩）：`dv-live-deep(root, key)` 返回的
  是 **CMP 活组件句柄**，而 `dv-flags(h)` 读的是**声明式视图池**（`DV-*`）—— 两套
  下标互不相干。拿 `dv-flags(cmp句柄)` 会**静默返回 0**（看着像「节点不可见」，
  实际读到了一个无关的视图节点）。活节点的 flags 一律用 `cmp-flags(h)` +
  `F-VISIBLE`(1) / `F-ENABLED`(2) / `F-DISABLED`(64)；视图节点才用 `dv-flags` +
  `DV-F-VIS`(1) / `DV-F-EN`(2) / `DV-F-CHK`(4) / `DV-F-SEL`(8) / `DV-F-ACT`(16) /
  `DV-F-OPEN`(32) / `DV-F-OVL`(64) / `DV-F-PASS`(128)。映射在
  `dv-apply-props` → `dv-apply-flags(L, dv-flags(v))`。
- **分组时「窗口行数」会超过「数据行数」**：混合行表里 `m = 数据行 + 组标题`，
  可见窗口可能整个落在标题上。`dv-repeat-plan` 原来按 `texts.len()`（数据行数）
  夹 `lim`，分组模式下会把标题行**静默切掉**。修法：按**混合行表长度** `plan.len()` 夹。
- **顺序必须稳定**：`dv-sort-run` 用插入排序（同键不交换），所以「键升」在键全同时
  就是源序。写成选择排序 / 交换排序会让「源序」断言莫名失败（同键行的相对顺序变了）。
- **超界偏移时窗口只剩最后一行**：窗口由**未夹紧的** `off` 算（`dv-win-range` 只把
  `i0` 夹到 `n-1`），布局再把位置夹到 `maxoff`。所以状态写 999999 时，混合表只剩
  最后一个 token（分组模式 = 最后一行，不是「最后 5 行」）。这与阶段 25 过滤列表的
  钉法一致（`tests/dva-filter.no` r27/r28），不是回归。
- **组序跟着 plan 走，不是源序**：`dv-group-plan` 在**排好的**下标表上检测组号变化，
  所以「键升 + 分组」的先出现的组是键最小的组。写成对源序扫描会得到另一个顺序。
- **顶层 `!!` 循环计数器必须大写**：顶层循环体被提升成函数，小写模块级绑定从函数
  引用是硬错误（`top-level binding 'i' ... lowercase name`）。测试里 20 组数据直接
  写字面量数组，别写循环。
- **`/`、`%` 是 fallible**：`(i * 7) % 20` 这类表达式裸写要解包 option。库里的
  `dv-sort-run` / `dv-group-plan` 全部避开除法，测试数据用字面量。

### §5.21 阶段 27 表单校验新坑（2026-11-21 实测）

- 🔴 **`EQ` 的 `par` 是「另一个字符串槽号」，不是常量 0**：`dv-rule-check` 里
  `DV-R-EQ` 走 `dv-bound(p)`，`p = pars[bi]`。写 `0` 是**页头那个 `type here`
  输入框**（demo 的 0 号字符串槽），不是「空」—— 断言会返回「3 个坏项」而
  预期是 0。demo 里必须写 `V-PARS[7] = D-VN`。这类「par 是下标而非值」的语义
  在 `HAS`（`spars` 下标）上同理，文档里要写明。
- **规则号必须返回全局下标 +1，不是切片内序号**：`msgs` 是**逐条规则**的扁平
  表（一条规则一句措辞），字段只是 `base + n` 的一个切片。若返回切片内序号，
  `dv-field` 拿到号后要再 `+base` 才能查文案，而 `dv-form-bad` 的「坏了几项」
  也会因为不同字段的局部号重复而失去意义。实现里 `code = bi + 1`。
- **文案挂规则、不挂字段**：同一种规则在不同字段可以写不同措辞（「age must be
  >= 18」vs「must match name」），所以 `msgs` 与 `kinds` 等长、逐条对应；字段侧
  只给 `base` / `n`。
- **「揭示」必须是独立的一根轴**：错误行只在（违反 && `show != 0`）时建。
  首次进页面 `show = 0` → 视图里根本没有错误节点（不是「建了但藏起来」），
  所以结构操作数也不同；提交失败才由应用层写 `show = 1`。测试要同时钉住
  「揭示前错误行不存在」与「揭示后文案正确」。
- **字段三个 key 不能撞**：`dv-field` 建 `label(key)` + `input(key+1)` +
  `error(errkey)`。撞了会被 `dv-sync` 按 `(key, kind)` 互相覆盖（label 与 error
  同为 `C-LABEL` 时尤其隐蔽）。demo 用 200/210/220/230（label）、400..430
  （error），避开虚拟列表的 93..112。
- **窗口装不下就别塞**：`APP-WIN-H = 420`（`tests/app-assemble.no` 钉住），
  四字段表单 intrinsic 高 41600 已经顶到窗口，加错误行 / 对话框必然溢出。
  demo 因此把左栏改成 `dv-scroll` 滚动容器（复用阶段 23 原语，不需要新代码），
  视口 228px、内容 100000；副作用是提交按钮要滚一下才看得见，页头回显
  `lsc=` 提示「这栏能滚」。**别去改 `APP-WIN-W/H` 默认值**（会同时改掉
  `app-win-wf()` 与装配层断言）。
- **`dv-bound(slot)` 读的是字符串状态池**，`dv-rule-check` 只认 `str`；传 i64
  槽会在编译期类型错误。数值规则先 `to-i64()`，**解析失败算违反**（否则
  `abc` 会静默通过 `>= 18`）。

### §5.22 阶段 28 虚拟表格新坑（2026-11-21 实测）

- 🔴 **列宽必须声明，不能实测**：可见窗口只挂几行，若让 `C-GRID` 按内容实测，
  列宽会随「当前窗口里恰好是哪些行」浮动（长文案行撑宽、短文案行缩回），表格会
  一边滚一边抖。`dv-table-list` 因此收 `colws` 逐列声明，自己算
  `tw = 2*CS-PAD + 列宽和 + (ncols-1)*CS-GAP`。**行宽也必须声明**（`dv-size(row,
  tw, rowh)`），否则行宽会随窗口内容变。
- **表头挂在滚动容器外面**：表头是 `parent` 的前一个孩子（与 `C-SCROLL` 同级），
  不是滚动容器的孩子 —— 否则会跟着滚。这也是「表头不滚」唯一的实现方式：
  没有 position: sticky 之类的东西，只有树的位置。
- **表头行高 = `CS-PAD + CS-LINE-H`，不是自动量**：`C-GRID` 自动量高是
  `2*pad + 行高`（48px）。在 420px 高的窗口里，右栏内容预算只有 204px，
  48px 的表头会把「order」下拉顶出屏外（实测：表头自动量高时下拉落在
  y = 40400，内容底是 39600）。声明成 `pad + 行高`（36px）后，交叉轴顶对齐的
  标签正好落在 `y+pad .. y+pad+行高`，既不溢出也不浪费。
- **`start` 越界不要兜底成第 0 行**：早期实现在 `j` 越界时保留 `tok = 0`，于是
  `start = 99` 会挂出 3 行 key 相同的行（`dv-sync` 按 `(key, kind)` 复用，互相
  覆盖）。正确做法是 `st` 先夹进 `[0, plan.len()]`，再 `av = plan.len() - st`、
  `lim = min(count, av)` —— 两端都越界时一行不挂，空表就是空表。
- **行主序单元格表**：`cells[源行号*ncols + 列号]`，不是「每行一个数组」——
  Nolang 没有二维数组，平行数组是唯一选择。demo 用 `dv-demo-fill-table()` 把
  「源行号 → 两列文案」摊平成 40 个槽（`i64 → str` 用 `to-str()`）。
- **`ncols = 1` 且不分组 ≡ `dv-order-list`**：两者吃同一套 `plan`，所以阶段 26
  的五档排序 / 分组原样复用，demo 主循环一行没改。分组时 `token < 0` 的行是
  **通栏**的（一个禁用标签），不是 1 列数据行 —— 这是表格与列表唯一的层次差别。
- **key 空间**：行 `key0 + 源行号`、单元格 `ckey0 + 源行号*ncols + 列号`、
  组标题 `hkey0 + 组号`。demo 用 93（行）/ 900（单元格）/ 920（表头行）/
  9000（组标题），与既有 90/91/92/55/56/57 不撞。

### §5.23 阶段 29 可拆面板新坑（2026-11-21 实测）

- **拖杆不用新节点种类，也不用动布局引擎**：两块面板之间的间隙不属于任何子节点，
  `cs-hit` 的「自身矩形兜底」让间隙命中 `C-DOCK` 容器自己。所以 `C-DOCK` 只是
  一台「通了电的横向容器」——`cmp-scene.no` 里它只出现在 `cs-is-container` 的白名单，
  布局 / 绘制 / 命中都没有专属分支。**前提是要实测**：我一开始按惯例以为容器点
  不到，写完才用探针测出「间隙 → 容器」，省掉了新增 kind 的一整摊改动。
- **间隙判定要连内边距一起判**：命中容器不等于命中拖杆。容器自己的内边距
  （`CS-PAD`）也落在「容器矩形 − 子节点」里，同样会命中容器。所以事件侧不是
  「命中 dock 就抓」，而是**再用光标坐标判一次**是否在 `[左屏右缘, 左屏右缘+gap]`
  这条带里；内边距点得到容器、但判不进带里，自然不抓（测试 `r25`）。
- **鼠标抬起要新开一个事件类型**：shim 原来只导出 down(2) / motion(3)，拖拽只能
  「再按一下」才结束。新增 `case SDL_EVENT_MOUSE_BUTTON_UP` -> `type = 6`（只收
  左键，坐标槽与 down 一致）。`app-poll` 的 `t = e & 0xFF` 是通用解码，不用改；
  改完跑 `./build-shims.sh`（约 4 s，纯增量）。
- **`value2` 要显式同步**：`dv-apply-props` 只对特定 kind 抄 `VALUE2`，`C-DOCK`
  不在其列 → 事件侧 `cmp-value2(dock)` 读到 0，夹紧上限失效。补一条
  `(kd == C-DOCK) -> { cmp-set-value2(L, dv-value2(v)) }`。
- **换算用「容器内第一个流子节点的原点」而不是 dock 自身原点**：横向
  `nv = px − paneA.x`，竖向 `nv = py − paneA.y`。dock 自身原点还含内边距，
  直接拿会差一个 `CS-PAD`。左屏宽 = 状态槽，所以这个差值就是新的左屏宽。
- **竖拆 = 抹掉 `L-H`，不是换 kind**：`dv-lay(d, 0, gap, CS-PAD)` 清掉横向提示，
  `cs-is-h` 随之返回 0，布局 / 命中 / 拖拽全部走纵向分支，状态槽改存**上屏高**。
  一套原语两个方向，不需要 `dv-vdock`。
- **名字只能叫 `dv-dock-split`**：2 参数的 `dv-dock(key, parent)` 已经是「83 个
  kind 构造器」家族的一员（`tests/dv.no` 覆盖），同名重载会撞车。带 `-split`
  后缀既避开冲突，也说明它是「可拆」的那一支。
- **拖拽期间不能走点击归约**：`t == 2` 先试 `dva-drag-down`，抓住就**跳过整个
  点击块**（否则按一下拖杆会被当成点容器触发 `DV-A-CHANGE` 的点击语义）；
  `t == 3` 只在抓住时才做几何换算，没抓住仍按 hover 处理。

### §5.24 阶段 30 代码编辑器新坑（2026-11-21 实测）

- **光标池的索引空间必须是「文本槽」，不是「视图节点」**：`DV-EDIT-L` / `DV-EDIT-C`
  原先在 `dv-node` 里按**节点**各 push 一格，而 `dv-edit-set` / `dv-edit-insert` /
  `dv-edit-follow` / `dv-code-editor` 全按**文本槽**读写 —— 两个下标空间对不上，
  读出来恒是 `dv-ensure` 填的 -1，光标永远不存在。修法有两处：`dv-node` 不再为
  它们 push；`dv-clear` 只截断 `DV-EDIT`（节点 -> 槽的映射，本来就该按帧清），
  **不截断光标池** —— 否则每帧都把光标弹回行首。另加一道防御：`dv-edit-key` 入口
  把负光标归零（第一次按键时池里可能还是 -1），`dv-code-editor` 读光标时也夹到
  `>= 0`。这个坑单测里表现为「渲染后 L=-1 C=-1」，光看结构是看不出来的。
- **`**` 不能当「本次迭代结束」用**：`-> {}` 臂里的 `**` 会打断**外层** `!!`。
  我最初把词法分支链写成「每个分支末尾一个 `**`」，编译与 vet 都干净，但语义是
  「整个 `!!` 只跑一次」—— 每行只出一个 token（探针实测 `'"a" "b"'` 出 1 段而不是
  3 段）。正确写法是**纯臂 if-group**：`{ (c1) -> {} ... -> {} }`，兜底臂是裸 `->`。
- **裸 `->` 兜底臂必须待在「只含臂」的 if-group 里**：`{ 前置语句 \n (c) -> {} ...
  -> {} }` 会报 `bare '->' else arm outside an if-group`。所以词法循环里
  `b = dv-byte-at(line, i)` 必须放在 group **外面**（`!! { (i >= len) -> **; b = ...
  { ... } }`），group 里只有臂。
- **相邻同色 token 会被并成一段**：`dv-run-push` 故意合并同色的前后 token（少挂
  节点、跨帧 key 更稳）。所以 `'let x'` 是 2 段（`let` / ` x`）而不是 3 段，
  `'let name = "noui"'` 是 3 段（`let` / ` name = ` / `"noui"`）。**数 token 要
  按「颜色段」数，不是按「词法单元」数** —— 测试的断言得跟着这条写，否则会以为
  词法器漏了 token。
- **`str.lines()` 会丢掉结尾空行**：`'a\nb\n'` 得到 2 行，于是「在最后一行末尾按
  回车」永远进不去。`dv-edit-split` 自己数 `\n` 分段（`n = 段数`，空串也算 1 段），
  `dv-edit-join` 用 `\n` 拼回。
- **光标前进量 = 插入串的长度，不是 1**：Tab 插的是 4 个空格，`dv-edit-insert` 里
  写 `c + 1` 会让光标停在缩进中间（实测 C1 而不是 C4）。改成 `cs-add(c,
  ch.len-bytes())`。
- **跨行移动必须在改行号之前判「是不是已经在首/末行」**：`(nc < 0)` 分支里先
  `l = l - 1` 再 `(l <= 0) -> { nc = 0 }`，会让「从第 1 行行首左移」被误判成
  「已经在第一行」而吞掉（实测 L0 C0 而不是 L0 C1）。用一个 `back` / `adv` 标志
  把夹取和跨行拆开：跨成功就不夹，没跨才夹。右移对称分支同理（`l` 自增后
  `l >= n-1` 会拿**旧行**的长度去夹）。
- **`dv-node-by-key` 查的是视图描述池，不是活树**：它扫 `DV-KIND`（`dv-node` 写的
  那批视图节点），返回的是**视图节点 id**。所以 `dva-edit-find` 才能用它把「活句柄
  的 key」翻译成「声明处」，再 `dv-edit-of` 拿文本槽 —— 别拿它当 `dv-live-deep` 用。
- **命中点要落在行内**：`cs-hit` 有「自身矩形兜底」，所以点在滚动容器的内边距上
  命中的是**容器自己**。测「命中上溯」时取 `(dx + 3000, dy + 2400)`（第 0 行的行号
  标签上），取 `(dx + 600, dy + 600)` 只会命中编辑器根 —— 虽然结果一样，但那就
  测不到「顺父链上溯」这条路径了。
- **长行不需要横向滚动，`cs-layout` 会夹**：纵向容器把子宽夹到 `w - 2*pad`，
  `cs-paint` 的 `C-SCROLL` 分支又带裁剪矩形，所以超长的代码行是被裁掉而不是把
  布局撑破。编辑器因此只要声明宽度（`dv-size(ed, dk, viewh)`，一次写全宽高 ——
  分两次调 `dv-size` 第二次会把第一次的高覆盖成 0）。
- **文本槽要 `dv-bind-t-ensure` 到足够大**：`dv-edit` 里会 `dv-bind-t-ensure(slot+1)`，
  所以忘了先 ensure 也不会越界，但 `dv-bound` / `dv-bound-set` 在别处直接下标读写
  时得自己保证池够长（demo 里 `dv-bind-t-ensure(7)` 配 `D-ED-TEXT = 6`）。

### §5.25 阶段 31 树新坑（2026-11-21 实测）

- **模块级 `[]str` **字面量**在函数体里是空的**（这是 §5.2 那条的完整版）：
  `T-TEXTS []str = ['noui','src','tests',...]` 在 `dv-tree-row` 里 `texts.len()` 读到 0、
  `dv-arr-get-s(texts, v)` 全空 —— 整棵树的文案消失，而结构断言（行数 / key / 宽度）
  全过，非常隐蔽。
  - **根因**（`src/mir/hir2mir.go`）：`foldConstText` 的 `KSliceLit` 分支只对**定长数组**
    （`ty.IsContainerRaw`）做常量折叠；通用分支又把元素按 `foldConstText(child, "i64")`
    折一遍，而它对 `KStrLit` 返回 `""`。于是 `fixedArrayConstGlobalType` 拒绝
    `[]str` 字面量，这个全局就停在 zeroinitializer（长度 0）。
  - **绕法（本仓库现行写法）**：模块级 `[]str` 一律 `with-len(n)` + 逐项下标写。
    这样建出来的是真存储，函数里读得到，**当参数往下传也保得住**（实测经两层函数
    传递后 `len()` 仍是 13）。`[]i64` / `[]f64` 这类字面量不用改。
  - **不修编译器**：改 `foldConstText` 会动到所有模块级常量折叠路径，回归面大，
    而目前只有树这一个「模块级字符串表 + 函数内消费」的用例。先把写法统一到
    `with-len`，编译器留待单独评估。
- **`vec.pop()` 在 MIR 层返回 `%option_i64`**，不能当普通 `i64` 用（与 §5.2 的
  safe-index 迁移同源）。`dv-tree-flat` 的显式栈因此用 `with-len(n)` + `sp` 下标 +
  `dv-arr-set` / `dv-arr-get` 自己模拟，不碰 `pop`。
- **`CS-TH-*` 是主题**槽号**，不是颜色**：`CS-TH-ACCENT = 2`、`CS-TH-TRACK = 4`。
  直接当颜色用会得到 `0x00000002` 这种近黑值。所有取色都走 `cs-theme(slot)`。
  树的选中态有**三处**（行 flags / 行底色 / 文本色），漏一处就花。
- **行 key 必须按「源节点号」而不是「可见行号」**：`key0 + 节点号*10000000`。
  按可见行号的话，滚动一下同一批 key 全换，`dv-sync` 会整棵重建而不是复用。
  同理 `dv-tree-flat` 返回的是**可见**行表，`m` 随展开态变（全收起时 `m = 1`），
  内容高与滚动上限都跟着 `m` 走。
- **`dv-win-range` 的窗口是 `[i0, i1)`，`i1` 是「第一个不可见行」**：`viewh` 给小了
  会**静默少挂行**（实测 `viewh = 14400` 只挂 5 行，树看着像「只有 5 个节点」）。
  树测试里把视口抬到 24000 让 8 行全挂，才测得到可见行表的全貌；demo 里视口
  16800 / 行高 2400 / 行距 3200 → `i1 = (−1200 + 16800 + 3199)/3200 = 5`，
  首帧正好挂 5 行。
- **demo 的初始态要自己写全**：`tests/main-dv.no` 启动**不走** reset 命令，只写
  `DEMO[D-COUNT]` / `D-LIST` / `D-DOCK`。所以树的 `D-TOPEN` / `D-TSEL` 得在启动处
  显式写，否则读到的是 zero-init —— `D-TSEL = 0` 看着像「选中了第 0 行」，
  其实只是没初始化。

### §5.26 阶段 32 命令面板新坑（2026-11-21 实测）

- **`dv-pad` 不存在**：面板内边距只能走 `dv-lay(box, 0, 0, 2*CS-PAD)` 的第 4 参
  （`CMP-PAD` 池），没有 `dv-pad` 这个 helper（那是 `cmp-pad` 的组件侧名字）。
- **查询输入框不能 `dv-lay-grow()`**：面板是纵向容器，输入框声明成 grow 会把它撑满
  整个高度、把列表挤没。输入框只 `dv-size(inp, 0, PAL-ROWH)`，grow 留给列表
  （`dv-lay(sc, dv-lay-grow(), 0, 0)`）。
- **行 key 段要避开编辑器与树**：`dv-sync` 按 `(key, kind)` 复用组件，key 撞了会串组件。
  demo 里把面板根定在 99（输入 100 / 列表 101）、行 key 基数 2000（行 key =
  2000 + 源行号*100），避开编辑器 960 / 9700+ 与树 980 / 9800+、表格 900+ / 9000+。
  这个撞车**不会崩**，只会静默画错，所以是写之前扫一遍所有 builder 的 key 段。
- **`cs-hit` 返回最深节点**：点在命令名字上命中的是文本 `C-LABEL` 而不是行 `C-FORM`，
  所以行和文本**两个都挂** `DV-A-RUN`，否则「点命令名字」什么也不发生。
- **游标移动不重建视图**：`dv-pal-move` 只改 `DV-PAL-CUR[面板]`，`dv-render` 因此
  `ops == 0`（实测），此时游标行的 key 不在活树里、`dv-live-deep` 拿不到。
  `dv-pal-cur-key` 必须**现算**：`DV-PAL-SRC[DV-PAL-SB[面板] + 游标]` -> 源行号 ->
  `key0 + 源行号*PAL-STRIDE`。`DV-PAL-SRC` 是每帧重抄的只增扁平副本（不清），
  基址记在 `DV-PAL-SB[面板]`。
- **`WAS` 沿 flip 检测必须放在「关着也跑」的那半段**：`dv-command-palette` 一进来
  就 `dv-pal-keys-ensure` + 登记四个槽映射 + `DV-PAL-WAS` 翻转检测（翻转 -> 游标归 0 +
  偏移写回 0），**然后**才 `(now != 0)` 建子树。放进 `(now != 0)` 里的话，关着的帧
  不更新 `WAS`，重开时游标还停在上次的位置。
- **字符串状态槽的写入要能「推回」组件**：查询串的真源是 `S[qslot]`（`dv-bound` 读），
  `dv-pal-type` / `dv-pal-backspace` 改的是**状态**，下一帧 `dva-sync-binds` 才把文本
  推回输入框 —— 和输入框自身收键盘走的是同一个「状态 -> 组件」方向，不冲突。
- **demo 的初始态要自己写全**（同 §5.25）：启动处补 `dv-bound-set(D-PAL-Q, '')`，
  否则查询串槽是 zero-init 的空串（行为一样，但回显与「reset 后清空」的语义要一致）。

### §5.27 阶段 33 命令式树新坑（2026-11-21 实测）

- **`->` 链不互斥**（最贵的一坑）：`(c >= 0) -> { A }` 后面跟 `(ct-get(...) >= 0) -> { B }`，
  **两个分支都会跑**，因为第一条分支改的正是第二条条件读的值。`cmp-tree-add` 挂兄弟链时
  写成「先 `ct-set(TR-FIRST, parent, node)`，再判 `TR-FIRST[parent] >= 0` 走 else」，
  结果 else 也执行，`ct-set(TR-NEXT, s, node)` 把 `NEXT[node]` 写成了 `node` 自己 ——
  单向链表成自环，拍平直接死循环（实测 `no run` 卡到超时）。`cmp-tree-toggle` 里是同一个
  形状（第一个分支把值改成 0，第二个分支的条件 `== 0` 又变真，值被改回 1，toggle 永不生效）。
  **写法**：条件先取到局部（`has = 0` / `(ct-get(...) >= 0) -> { has = 1 }`），再按局部分支。
- **`CHILD-IDX` 是全局扁平数组，所以「一个父的孩子必须连续挂载」**：`cmp-child(parent, i)`
  读的是 `CHILD-IDX[CMP-HEAD[parent] + i]`，`cs-child-flow` 也走它。`cmp-tree-sync` 要是
  「边建行边填内容」，行里的箭头 / 文本标签就会插进**树的子链**里（实测树的 5 个子节点
  变成 `C-FORM / C-LABEL / C-LABEL / C-FORM / ...`，`cmp-tree-node-of(row2)` = -1）。
  **写法**：两段式 —— 先一口气 `cmp-tree-row-head` 挂完全部行壳，再逐行
  `cmp-tree-row-body` 填内容（行的子链因此连续）。`dv-render` 早就是这个约束（step 5
  先全摘再按视图顺序重挂），命令式侧得自己守住。
- **单向兄弟链表不能倒着走**：`TR-NEXT` 只有 next 指针。`cmp-tree-flat` 要正序输出，
  所以先把孩子正序收进临时 `ks []i64`，再 `j = ks.len()-1 .. 0` **倒着压栈**，
  弹出顺序才是正序（声明式 `dv-tree-flat` 是「兄弟块相邻」所以天然正序，命令式这侧
  必须自己翻一次）。根也一样：倒着入栈。
- **`cs-hit` 返回最深节点，不是行**：点树行的文本命中的是文本 `C-LABEL`，点箭头命中的是
  箭头 `C-LABEL`。所以点击处理要 `cmp-tree-row-of(TREE, hit)` 沿 `CMP-PARENT` 上溯到
  `TR-ROW-NODE >= 0` 的那一层；从树本身（或树外的东西）上溯则返回 -1。
- **新池要随 `cmp-alloc` 一起长**：`TR-*` 全是按句柄索引的平行数组，`cmp-alloc` 每 push
  一个组件槽就得同步 push 一行（8 个 push），否则 `ct-get(TR-PAR, h)` 读到界外。
  反过来说，**句柄即下标**这个约定让「选中存 `CMP-VALUE[树]`」很自然，但树自己也是
  一个普通组件，别把树句柄和节点句柄混用。
- **`print` / `format` 字段只收简单标识符**（再犯一次）：`print('rows={cmp-tree-nodes(t1)}')`
  与 `print('w={2 * CT-INDENT + CT-ARROW-W}')` 都是 `unresolved format field`（MIR
  codegen 直接报），必须先赋给局部再插值。
- **`cmp-tree` 的 `value` 语义变了**：阶段 33 之前是 0，现在是 -1 = 「没选中任何节点」。
  选中节点句柄存 `CMP-VALUE[树]`，`cmp-tree-selected` 读它。测试里 `v1 = cmp-value(t1) == -1`
  就是这条。
- **`cs-div` 没有 `cs-div2`**：探针里写 `cs-div2(w)` 直接 vet 不过，一半宽要 `cs-div(w, 2)`。
- **命中链要在真实坐标下验一次**：纯逻辑测试只验「句柄上溯」，验不了「点下去真的落在行里」。
  一次性探针做法：`cs-layout` 后对每行取 `cs-rect` 中心点 `cs-hit`，再
  `cmp-tree-row-of` 看是否解析回该行 —— 4 行 + 箭头槽共 5 个点全部命中（`bad=0`）。

### §5.28 阶段 34 命令式代码编辑器新坑（2026-11-21 实测）

- **参数叫 `ok` 会误编译**（本轮最贵的一坑，和 §5.24 的 `co` 同类）：`cmp-editor-row`
  一族原本把「字符串 token 颜色」参数命名为 `ok`，函数体里又有 option 模式
  `x = f(): { ok -> it \n -> d }`。编译器把形参 `ok` 和模式绑定 `ok` 混成一个东西，
  结果是**返回值是对的、组件树是错的**：`cmp-editor-sync` 返回 `m = 7`，
  但 `cmp-children(ED)` 只有 5（两段式的第一段只挂了一半就停），紧接着
  `cs-layout` 段错误（lldb 停在 `cmp_add_child + 56`）。改名 `okc` 之后
  同一份调用连跑三次都是 `kids=7 content=24000`，`tests/cmp-editor.no` 仍全绿，
  `no vet` 计数不变。**结论**：`ok` 在 nolang 里事实上是保留名，形参 / 局部一律别用。
- **函数里调 `cmp-editor-sync` 曾经拿到 `undef` 句柄**（编译器缺陷，
  2026-11-22 已修）：同一次调用，写在 init 的顶层块里 `kids=7`；原样包进一个函数
  （`ed-refresh()`）再调，`kids=0`，右栏 `rows=0`、编辑区整块空白，但
  `n=7` / `l=0` / `c=0` 全对。逐项排除过：实参用字面量还是局部、先调 `ed-viewh()`
  还是直接给值、函数返回 `()` 还是 `(m i64)`、有没有右栏 detail 面板、有没有
  `cmp-set-text` —— 全都不是。唯一决定因素是**这次调用是否发生在函数里**
  （`ed-min` = 只调一次 sync 的最小函数，当时给出 `m=7 kids=5` 然后 139）。
  根因在 `hir2mir` 的**模块存储判定**：`ED` 是大写标量，初值 `ED = cmp-editor()`
  写在 init 块里（嵌套写），于是它没被登记成模块全局，而是降级成合成 main 的局部；
  函数读 `ED` 解析成 `undef`，`cmp-editor-sync` 把行挂到句柄 `undef` 上，
  真编辑器 `kids=0`。纯标量最小复现：`ED = 0` + 块内 `ED = 7` + `f` 读 `ED`
  → 打印 `r=0 ED=7`（错）。
  修法：`globalRefInit` 去掉 `isUnsafeInlineType` 限制（标量的 call 初值同样是这个
  缺陷）、登记条件与 `synthesizeMainForTopLevel` 的两处跳过条件统一改成
  「有嵌套写**或**名字是大写」（`isGlobalStyleName`）—— 块内赋值是顶层绑定唯一
  合法的重赋形式（顶层重复 `let` 是 duplicate-variable 错误），不能成为降级理由。
  回归钉在 `src/mir/global_scalar_ref_test.go`（旧代码上两条 FAIL、新代码上 PASS）；
  A/B 实测：旧编译器跑 `tests/main-editor.no` 0.33s 静默退出且无输出，
  新编译器跑满 900 帧 `rows=7`。**库侧不再需要绕**：重建段平铺在主循环里无害，
  抽成函数也正确；`ed-hit-in` / `ed-click` 这类只读函数本来就对。
- **`components.no` 看不到 `CS-PAD` / `CS-GAP`**：这两个常量只定义在 `cmp-scene.no`，
  而 `components.no` 是基础模块、没有 `#` import。所以 `cmp-editor()` 的
  `pad = 1200` / `gap = 800` 是**字面量**（代码里写了注释说明），
  `cmp-editor-sync` 的内容高公式 `n*rowh + (n-1)*800 + 2*1200` 同理。
  声明式侧 `dv-code-editor` 能写 `CS-PAD` 是因为 `dv.no` import 了 `cmp-scene`。
- **两段式重建**（§5.27 那条约束的第二次现身）：`CHILD-IDX` 是全局扁平数组，
  一个父的孩子必须连续挂载。`cmp-editor-sync` 先一口气 `cmp-editor-row-head` 挂完
  可见行壳，再逐行 `cmp-editor-row-body` 填内容；边建行边填会把光标 / 着色 token
  插进编辑器的子链里。
- **窗口公式与声明式对齐**：`st = rowh + 800`，`i0 = (off - 1200 - rowh)/st + 1`
  夹到 `[0, n-1]`，`i1 = (off - 1200 + viewh + st - 1)/st` 夹到 `n`；
  内容高 `n*rowh + (n-1)*800 + 2*1200`（`n = 0` 时是上下 padding）。
  命令式侧另有一个 `ED-SEEN` 池存「上次跟随时的光标行」，**光标没动就不重排窗口**
  —— 否则滚轮推走的窗口会被下一次 sync 弹回去（声明式 `dv-edit-follow` 同理）。
- **`ED-LINES` 保留末尾空行**：`'a\nb\n'` 是 3 行，`''` 是 1 行空串。
  这是「在最后一行末尾按回车能进去」的前提，`cmp-editor-del-line(0)` 因此
  真的少一行（测试最初按「删行后仍 3 行」写，是测试错了不是实现错了）。
- **光标列是字节列**（`ED-C` 存 byte offset，着色按 `ed-byte-at` 取），
  不是显示列 —— 等宽字体下两者一致，非等宽时会偏。
- **`cmp-editor-row-of` 上溯到 `ED-ROW > 0`**：`cs-hit` 返回最深节点，点行号命中
  行号标签、点代码命中 token 标签，都要沿 `CMP-PARENT` 上溯到行。
  点击落点取「行首」（`cmp-editor-set-cur(ED, ln, 0)`），没做列级定位。
- **`print` format 字段只收简单标识符**（第三次犯）：`print('in go: ED={ED}')`
  报 `unresolved format field ED` —— 函数体内的 format 字段连模块级大写都解析不到，
  必须先赋给局部。

### §5.29 阶段 35 命令式虚拟表格新坑（2026-11-21 实测）

- **`;` 是行注释，不是语句分隔符**（本轮最贵的一坑，`tests/main-table.no` 死循环的
  真因）：`(k == 49) -> { cmp-table-sort(0); tb-mode = 1; tb-keys = ... }` 里第一个
  `;` 之后**整行都是注释**，包括那个收尾 `}`。于是这个分支的块没有在本行闭合，
  解析器继续往下吃，直到撞上 `(t == 4)` 的 `}` 才收口 —— 后面整个主循环被重新
  嵌套，`tb-frames` 永远不自增，窗口活着但一帧都不推进（headless 下表现为
  `loop f=0 / poll t=0` 无限刷屏，`timeout` 杀掉，进程不退出）。
  **诊断路径**：先按「四个事件分支逐个单独留全 body」二分（hover / 点击 / 滚轮三个
  都过，只有键盘分支挂），再在键盘分支内部逐语句二分 —— 单句全过，三句合体挂，
  最后定位到「句间用 `;` 分隔」这一条。**绕法**：分支体里多句一律**换行**写。
  （`cmp-table-sort(0)` 那些调用本身没问题，`cmp-editor-sync` 一类的顶层重建要求
  也与此无关。）
- **`CHILD-IDX` 的扁平性比 §5.27/§5.28 记的更强**：不只是「一个父的孩子们要
  连续挂载」，而是**一个父的所有直接子必须连续挂载完，才能去填任何一棵子树**。
  `cmp-table-sync` 最初按「建表头 `hg` → `cmp-add-child(h, hg)` → 填 3 个表头标签
  → 建滚动体 `scr` → `cmp-add-child(h, scr)`」写，结果 3 个标签插进了 `hg` 和
  `scr` 之间，`cmp-child(h,1)` 拿到的是 `C-LABEL` 而不是滚动体。
  一次探针（打印 `k0` / `k1` / `kidsB` / `scr` / `start` / `content`）直接给出
  `k0=92 k1=1 kidsB=0`。**正确顺序**：先把 `hg` 和 `scr` 连续挂进 `h`，
  再回头填 `hg` 的标签。
- **`cmp-win-range` 在 `n = 0` 时会给出 `i0=0, i1=1`**（末尾那句
  `i1 = max(i1, i0+1)`），所以每个「按窗口挂行」的调用点都必须先判 `m <= 0`。
  表格空数据时少了这个守卫，会挂出一个源行号取自 `ct-get(TB-ORDER,0)` 的幽灵行。
- **`cmp-table-sync` 不写回偏移**：`cs-layout` 的 `C-SCROLL` 分支只把夹紧后的
  `off` 存在局部，`CMP-SCROLL-MAX` 会写、`CMP-VALUE` 不写。所以命令式侧的
  偏移真源是 `TB-OFF[句柄]`，`cmp-table-set-off` 写它、建滚动体时读它当
  `CMP-VALUE`，滚轮也写它 —— 和阶段 34 的 `ED-OFF` 同一个路子。
- **数据模型是全局的**（`TB-HEAD` / `TB-COLW` / `TB-CELL` / `TB-KEY` / `TB-ORDER`），
  多表靠**句柄 -> 状态池**（`TB-OFF` / `TB-SEL` / `TB-HEAD-H` / `TB-SCR-H`）隔离，
  行 / 单元格 -> 源行号的映射靠 `TB-ROW-SRC` / `TB-CELL-SRC` / `TB-CELL-COL`
  沿命中链上溯。`TB-ORDER` 是显示顺序（源行号表），排序是**稳定插入排序**，
  所以选中态跟着源行号走，排序后仍指同一行。
- **内容高声明**：`m*rowh + (m-1)*800 + 2*1200`（`m = 0` 时是上下 padding），
  走 `cmp-set-scroll-content`，于是 `cmp-scroll-max` 不依赖「所有行都挂出来」，
  和声明式 `dv-table-list` 完全一致。
- **`tb-arr-set` 是新增的唯一下标工具**：`[]i64` 出参 / 状态池只能靠
  `push` + 下标写，`ed-arr-set-s` 只管 `[]str`。`arr[i]` 读出来是 `?i64`，
  统一用既有的 `ct-get` 读（越界给 0）。

### §5.30 阶段 36 命令式可拆面板新坑（2026-11-21 实测）

- **`CMP-VALUE` 是夹紧下界，不是当前位置**（本轮最贵的一坑）：`cmp-dock-split`
  按声明式 `dv-dock-split` 的语义把 `minw` / `maxw` 写进 `value` / `value2`，
  于是 `cs-dock-move` 里那句 `nv = ct-clamp(nv, cmp-value(d), cmp-value2(d))`
  之后再 `cmp-set-value(d, nv)` 就把下界写掉了 —— 第一次拖完 `value` 变成
  光标换算值，第二次再拖 `clamp(nv, 17600, 28000)` 永远 ≥17600，下界夹紧
  永久失效（`tests/cmp-dock.no` 的 `dg3` 直接钉住这条）。**绕法**：位置真源
  另开一个随 `cmp-alloc` 逐槽长的池 `DK-SPLIT-V`，`cmp-dock-split` 初始化成
  `minw`，`cs-dock-move` 写它，应用每帧 `cmp-set-sw(首子, cmp-dock-split-val(d))`
  同步 —— 和 `TB-OFF` / `ED-OFF` 同一个路子。
- **`print` 的 `{field}` 只接受「调用之前已绑定」的简单标识符**：为了探查而把
  `print('A l1={l1} ...')` 写在赋值之前，直接编译错
  `unresolved format field l1`（不是运行期空值）。排查阶段要打印就先把值算进
  变量、再打印。
- **嵌套分支体里的 `->` else 臂必须自己包 `{ ... }`**：`(t == 2) -> { ... (g == 1) -> {...} -> {...} }`
  报 `an else arm ('->' on its own line) must live inside an if-group`。
  写成 `{ (g == 1) -> {...} -> {...} }` 才可以。顶层（函数体一级）的 `->` 不用包。
- **标识符命名 lint 是硬的**：`paneA` / `paneB` 直接 ERROR（`should use only
  lowercase letters and hyphens`），要写 `pane-a` / `pane-b`。`[]str` 这类
  类型标注会被 hint 成「可以省略」（`NAMES []str = [...]` → `NAMES = [...]`）。
- **`NAMES[i]` 读出来是 `?str`**，不能直接赋给 `str` 变量，要两步解包
  （`q = NAMES[i]: { ok -> it  -> '-' }`）。
- **断言变量别用两字符短名**：`tests/cmp-dock.no` 初版用 `l1`..`l12`，
  同一份布局 rect、同一份逻辑，往文件里加三行无关的探针赋值后 `l3` / `l4`
  的结果就翻转了（两次构建一次 `l3=false,l4=true`、一次 `l3=true,l4=false`）。
  改成 `lay1`..`lay12` 后连续两次构建、两次运行完全一致。MIR 后端对这种短名
  的槽位分配敏感，断言变量一律用可读的长名。
- **`cs-dock-down` 的命中判定复用 `cs-hit-inner` 的「自身矩形兜底」**：间隙
  不属于任何子节点，点进去没人接住就兜到容器头上，所以不需要新增节点种类，
  也不用给布局引擎加分支。只有**落在 gap 里**才算抓住（容器内边距不算，
  否则点空白就开始拖）。

### §5.31 阶段 37 命令式命令面板新坑（2026-11-21 实测）

- **`CHILD-IDX` 是全局扁平数组，一个父的所有直接子必须占一段连续槽位**（本轮最贵
  的一坑）：`cmp-add-child` 只在 `CHILD-IDX.len()` 追加，且**只在
  `CMP-COUNT[parent] == 0` 时**才重设 `CMP-HEAD[parent]`。所以「把滚动体摘掉再挂
  一个新的」里，新滚动体会落在父既有子链之外 —— 面板每 sync 一次 `cmp-children`
  就涨一个（探针实测 `k1=2 ok=true k2=1 k3=2 ok2=false k4=2`）。绕法两条：
  (a) 输入框 + 滚动体**第一次建好就复用**，之后只重建滚动体里的行（顺带解决了
  「每次重建输入框就丢焦点」）；(b) 重建行时必须**全摘** —— 全摘后 `COUNT` 归零，
  下一轮 `cmp-add-child` 会重设 `HEAD`，新挂的行才是连续的一段；只摘一半再挂
  会静默错位。和 `cmp-tree-sync` / `cmp-table-sync` 是同一个约束。
- **同一个标识符在一个函数里带两种类型会误编译**：`cmp-command-sync` 里 `r`
  既是 `i64` 循环变量、后面又 `r []i64 = with-len(2)`，MIR→LLVM 直接报
  `argument 2 of '@ct_set': parameter 'i64' expects a scalar (LLVM i64) but the
  argument has aggregate type '[]i64' (LLVM %vec)`（`@cmp_detach` 同样报错）；
  但**有的构建不报错，而是运行期静默退出**（exit 0，只打印前几个探针）。改成
  `rw` 后一切正常。排查「程序跑一半没了」先查这个。
- **`**` 写在顶层 / 循环外是干净退出**（exit 0，之后没有任何输出），所以上面那种
  误编译的表象是「静默停住」而不是崩溃 —— 两者要一起记。
- **`ct-get(arr, -1)` 不返回 -1**（越界读是未定义值），别拿负下标当哨兵探针；
  要探「是不是面板行」就用 `cmp-command-row-src(x) >= 0`。
- **`cs-hit` 对模态 overlay 点在外面返回 `-2`**（屏障：什么都没命中），命中内部
  ≥ 0，且 `-2` 会一路向上传播、不会落穿到下面的节点 —— 「点面板外就关面板」
  靠的就是这个，不是自己再算一遍几何。
- **游标是命中表下标，不是源行号**：查询一变命中表就变，拿源行号当游标会指错行。
  命中表抄一份只增的扁平副本（`CP-SRC` + 每面板基址 `CP-SB`），于是
  `cmp-command-move` 不重建视图也能立刻算出「游标 → 源行号」（和 `dv-pal-cur-key`
  同一个理由）。代价：`CP-SRC` 只增不减，随每次 sync 变长 —— 和 `DV-PAL-SRC`
  一样有界。
- `components.no` 没有 `#` import，看不到 `CS-PAD` / `CS-GAP`，所以面板和滚动体
  的内边距 / 间距一律写字面量 1200 / 800（和 `cmp-editor` 一样）；行高 / 步长由
  调用方按帧传（demo 用和声明式一致的 2800 / 3200，窗口算式才对得上）。
- **`tests/dva-editor.no` 的 `trace/BPT trap` 是环境漂移，不是阶段37 引入的**：
  陷阱在 `cmp_alloc` 末尾（`bl _str_push` 之后一条 `brk #0x1`，lldb 实测
  `cmp_alloc + 5144`）。判定依据：把 `components.no` 里整个阶段37 段落（`cmp-alloc`
  的 6 行 `CP-*.push` + `12c` 全节，3218 -> 2968 行）删掉再编译，照样 3/3 陷阱；
  同一编译器下另外 7 个声明式测试（`dva-palette` / `dva-tree` / `dva-dock` /
  `dva-table` / `dva-scroll` / `dva-tabs` / `dv`）全过；而同一份源码在 14:04 那轮
  回归里又 PASS。`md5` 相同的两次构建行为一致，说明它随**外部文件**（并发 session
  正在改写仓库根 `src/std/dom.no` / `bigint.no` / `bin/no`）漂移，不随本阶段改动。
  排查这类「某个测试突然崩在无关函数里」的红，先做**删代码复现**：删掉怀疑的改动
  仍然崩，就往工具链/环境查，别急着改自己的代码。

### §5.32 收尾：5 个早期遗留草稿转绿（2026-11-22 实测）

把上一轮记为「与本阶段无关」的 5 个红测试全部修掉后，全量回归 **PASS=45 FAIL=0**。
三个新坑，前两个是语言/编译器的，第三个是老坑再犯：

- **option 不能隐式解包**：`v = G-i0[h]`（下标读是 `?T`）直接赋给 `i64` / `str`
  变量，编译期报 `cannot assign ?i64 value to i64 variable 'v'`。库里的标准写法是
  **match 直接挂在表达式上**：`v = G-i0[h]: { ok -> it \n -> 0 }`（`->` 臂给默认值；
  臂之间必须空一行）。`src/entity.no` 的 `entity-i0` / `entity-i1` / `entity-s0`
  三处都这么改。
- **i64 字面量写进 f64 结构体字段 → 非法 IR**（2026-11-22 已修）：`r rect; r.x = 10`
  曾经生成 `store double %lv.N, double* %gp.M`，而 `%lv.N` 是 i64 局部，clang 在**链接期**
  报 `defined with type 'i64' but expected 'double'`。冻结工具链（`tmp/s148/wt/bin/no`）
  上同样复现 → 是编译器缺 `sitofp`。绕法曾是**字面量带小数点**（`r.x = 10.0`），
  函数实参同理（`rect-hit(r, 50.0, 30.0)`）；现在 `emitSetField` 会补 int→float 的
  `sitofp`，新代码不必带 `.0`，回归见 `src/mir/f64_field_store_test.go`。
- **(`cond`) `{` 是 while，又是死循环**：`src/entity.no` 的 `entity-emit` 把
  `(G-sub-active[i] == 1 && ...) {` 写成了条件循环 —— 条件恒真，函数永不返回；
  `tests/test-l1.no` 跑到 `entity-emit` 就挂，只有 `timeout` 才拿得住。
  `tests/test-kernel.no` 的 `el-add` 同样一处（`scene-dump` 里还有两处，但是死代码
  没触发）。全部改成 `-> {`，与 `tests/kernel.no` 的正确版本对齐。
  **判据**：一个 `(cond) {` 里如果只改局部状态、没有修改(cond 用到的量)，
  十有八九是要写 `->`。
- **`tests/main-app.no` / `main-layered.no` 无头下不退出不是缺陷**：它们是没有
  帧数上限的交互 demo，dummy 视频驱动不投递 `SDL_EVENT_QUIT`，主循环一直空转
  （`main-command` / `main-dock` / `main-tree` 有 `*-MAX-FRAMES` 故能 `bye`）。
  用 `timeout` 观察时，到点发的 `SIGTERM` 被 SDL 的信号处理器转成 quit 事件 ——
  于是先打印 `bye` 再停在退出路径上，看着像「打印 bye 后卡死」，其实信号已被消费。
- 🔴 **跨模块同名全局会真遮蔽，且结果随 `#` 导入顺序变**：`src/dv.no` 的树形箭头
  常量 `TR-OPEN = 'v'` 与 `src/components.no` 阶段33 的模型池 `TR-OPEN []i64`
  同名。同时 `#` 导入两者的程序里，`TR-OPEN` 指向谁**取决于导入顺序** —— 探针实测
  `# dv` 后 `# components` 打印 `TR-OPEN=[]`（组件池），反过来打印 `TR-OPEN=[118]`
  （`'v'`）。`tests/dva-tree.no` 的 `dv-text(ar0) == TR-OPEN` 只是**碰巧**靠传递
  导入顺序拿到了 dv 那份。修法：把 dv 侧改成 `TR-ARROW-OPEN` / `TR-ARROW-SHUT`
  （与 `TR-ARROW-W` 同族），`components.no` 的池名不动（那是阶段33 的公开 API）。
  两类名字**同前缀不同义**（`TR-ARROW` 是池、`TR-ARROW-W` 是宽度）也容易看错，
  新增 `TR-*` 前先 `grep -rn "TR-" src/`。

### §5.33 阶段 38 命令式输入框富文本 / 语法高亮新坑（2026-11-22 实测）

组件目录里最后一条「功能对等长期项」——`C-INPUT` 的富文本 / 语法高亮 —— 补齐。
在此之前 `cmp-input()` 只是 `cmp-alloc(C-INPUT, '', placeholder, 色, 0)`：没有光标
（`input-append` 追加到末尾、`input-backspace` 删末字符），渲染层走通用
`cs-paint-field`（整条文本一个颜色）。

- **光标按句柄存**：`INP-CAR []i64`（字节下标，和 `slice-bytes` 同一套索引），
  随 `cmp-alloc` 一起长。`input-caret` / `input-set-caret`（夹紧到 `[0, len-bytes]`）
  / `input-move`（相对移动后夹紧）。
- **定点编辑**：`input-insert(h, ch)` 在光标处插入并把光标推进 `ch.len-bytes()`；
  `input-backspace` 改为「删光标前一个字节，光标后退」，`caret == 0` 时无操作；
  新增 `input-delete`（向前删）。`input-append` **保持旧语义**（追加到末尾），
  实现改成「先把光标推到末尾再走 `input-insert`」；`input-set` 整段替换后光标落末尾，
  `input-clear` 清零 —— 这样老调用方（`main-form` / `dv-app`）行为逐位不变。
- **语法高亮**：`input-syntax(h, on)` / `input-syntax-mode(h)` 开关。段**不在渲染层
  token 化**：`input-lex-build(h)` 在组件模块内调 `ed-lex-line`（与阶段34 编辑器
  同一个词法器），把结果摊平进只增池 `INP-SG` / `INP-SGC`，每句柄一段
  （`INP-SGO` 基址 + `INP-SGN` 段数）；渲染层只读 `input-lex-count` /
  `input-lex-seg` / `input-lex-color`。这么做是躲开「跨模块 `vec.push`」那条老坑 ——
  段池和 push 都留在 `components.no`。颜色取 `INP-C-KW` / `INP-C-STR` / `INP-C-COM`，
  默认色沿用组件自己的 `CMP-COLOR`（数字仍走 `ED-C-NUM`）。
- **渲染层**：`cs-paint-input` 取代 `cs-paint-field` 成为 `C-INPUT` 的落点（其余
  输入类 kind 仍走 `cs-paint-field`，行为不变）。纯文本路径按 `INP-CAR` 取前缀宽
  画光标；高亮路径 `cs-paint-input-lex` 逐段上色，光标落在段边界上时拆成
  前缀 / 光标 / 后缀（与 `cmp-editor-row-body` 同构），并补一条「光标在所有段之后」
  的兜底。
- **不新增节点种类**：`C-INPUT` 仍是叶子，富文本完全靠 Scene 的多条 text 指令表达，
  布局 / 测量 / 命中路径不动。
- 落点：`src/components.no` 的 `INP-*` 池 + `input-caret` / `-set-caret` / `-move` /
  `-insert` / `-delete` / `-syntax` / `-syntax-mode` / `-lex-build` / `-lex-count` /
  `-lex-seg` / `-lex-color`，`src/cmp-scene.no` 的 `cs-paint-input` /
  `cs-paint-input-lex`；`tests/cmp-input.no` 26 项断言全绿（编辑 13 项 / 词法 8 项 /
  渲染 5 项，含「段拼回原文 == 原文」与「高亮比纯文本多 3 条 text 指令」）；
  `tests/main-input.no` 窗口 demo（两个字段：一个开高亮一个纯文本，点击聚焦、
  打字在光标处插入 / 退格 / 前删 / ←→ 移光标、`p` 切高亮、`r` 重置、`q` / ESC 退出），
  900 帧 `bye frames=900 keys=0 clicks=0 value=let x = 42 ; hi caret=15 lex=1
  tokens=5` + `input render OK`、exit 0。
- 坑两条：**`->` 兜底臂必须待在 if-group 里** —— 嵌套在 guard 块内的
  「`cond -> {…}` + `-> {…}`」要再包一层裸 `{ … }`（`cs-paint-input` 与
  `cs-paint-input-lex` 各踩一次，症状是 `cmp-scene` 整个模块静默不导出，用户文件里
  满屏「`xxx` is not defined」，用 `no fmt -d src/cmp-scene.no` 才看得到真错）；
  **光标用字节下标**，`len()`（字符）与 `len-bytes()`（字节）混用会在非 ASCII 时偏位，
  库里统一按字节算，宽度近似仍走 `cs-text-w`（每字符 8.0）。

### §5.34 阶段 39 命令式多行文本域（C-TEXTAREA）新坑（2026-11-22 实测）

`src/components-catalog.md` 写着 `C-TEXTAREA` 的能力是「multiline value」，但命令式侧
它只有 `cmp-textarea(placeholder)` 一个空壳构造：`input-set` 塞多行文本进去，渲染仍走
单行的 `cs-paint-field`（整串画一行、光标按整串宽度定位），既没有行概念也没有视口。
**文档承诺了但代码没做**，所以这是「最强的一种缺口」。

- **文本与光标不另起炉灶**：仍走 `CMP-TEXT` + `INP-CAR`（阶段38 的字节下标）。于是
  换行 / 退格 / 前删**直接复用** `input-insert` / `input-backspace` / `input-delete`——
  行首退格合并上一行、行尾前删合并下一行、行中插 `'\n'` 多一行，全是这三条原语的
  推论，不写新的编辑函数（也就自动继承 disabled 写无效语义）。
- **行几何**（`src/components.no`）：行 = `'\n'` 分隔、换行符归上一行行尾，空文本算
  1 行。`ta-line-count` / `ta-line-start`（行号越界夹到最后一行）/ `ta-line-len`
  （**不含**结尾 `'\n'`，按字节）/ `ta-line-text` / `ta-caret-line` / `ta-caret-col`。
  `ta-move-up` / `ta-move-down` 保持列、夹到目标行长度，首 / 末行不动。
- **视口**：`TA-OFF []i64` 每句柄首行行号，随 `cmp-alloc` 逐槽长；`ta-off` /
  `ta-set-off`（夹 0）/ `ta-follow(h, cap)`——让光标行落进 `[off, off+cap)`，且 `off`
  不越过「最后 cap 行」（`lines - cap`，负则 0）；`cap <= 0` 按 1。
- **渲染层**：新增 `cs-paint-textarea`，`C-TEXTAREA` 从 `cs-paint-field` 改走它。
  `cap = ht / CS-LINE-H`（至少 1），`ta-follow` 夹好视口，`l` 从 `off` 画到
  `min(lines, off + cap)`：每行一条 `cs-emit-text-left`（文本由 `ta-line-text` 给），
  光标行再画一条行内 `fill`（x = 行首 + `cs-text-w(行内前缀)`，夹到右内边距）。
  同时把 `CS-CLIP` 设到字段矩形再恢复（嵌套滚动靠这次存 / 取）。
- **不新增节点种类**：`C-TEXTAREA` 仍是叶子，`cs-measure` 早就给它 `2 * CS-BTN-H`
  的高（视口 3 行），布局 / 命中路径不动。
- 落点：`src/components.no` 的 `TA-OFF` + `ta-*`，`src/cmp-scene.no` 的
  `cs-paint-textarea`；`tests/cmp-textarea.no` 34 项断言全绿（几何 8 / 光标 9 /
  编辑 5 / 视口 7 / 渲染 5，含「视口外两行**不进 Scene**」与「光标滚出后视口跟随到
  `off=2`」）；`tests/main-textarea.no` 窗口 demo（48000 × 16000 字段 = 视口 6 行，
  点击聚焦、打字 / 回车 / 退格 / 前删 / ↑↓←→、`r` 重置、`q` / ESC 退出），900 帧
  `bye frames=900 keys=0 clicks=0 value=let x = 1¶let y = 2¶; done caret=26 line=2
  col=6 off=0 lines=3` + `textarea render OK`、exit 0。
- 坑一条：**视口循环的上界必须同时受 `lines` 与 `off + cap` 约束**。只判 `lines` 时，
  滚出视口的行照样被发进 Scene——`CS-CLIP` 只影响**绘制**、不影响 Scene 内容，截图上
  看不出来，只有断言「Scene 里有没有那一行」才抓得到（`t31` 就是这么发现的）。
- 回归：`tests/*.no`（排除 `main-*`，共 47 个）**PASS=46 FAIL=1**，唯一红项
  `tests/dva-editor.no` 的 `trace/BPT trap` 是既有环境漂移（把阶段39 的改动整个剥掉
  再编译照样 133，见 README 阶段37 验收记录）；11 个 `tests/main-*.no` 窗口 demo
  **全部 `rc=0`**（dummy 驱动），`main-dv` 输出与阶段38 基线逐字节相同（`ops=125`）。
  仓库根 `GOFLAGS=-buildvcs=false make test` 另有 1 个与 noui 无关的既有红项
  （`src/parser/dump` 的 `TestS1RawCondFixturesAreExercised`：未提交的
  `tests/match-2.no` 改写掉了 `ok(cond)` 臂，钉 P4 覆盖的测试没跟着更新）。

### §5.35 阶段 40 命令式输入族（C-NUMBER-INPUT / C-STEPPER / C-SEARCH）新坑（2026-11-22 实测）

`src/components-catalog.md` 写着 `C-NUMBER-INPUT`「value, min, max, step」、`C-STEPPER`
「value, min, max」、`C-SEARCH`「query, clear」，但命令式侧三者都只走单行
`cs-paint-field`：没有值域 / 步长 / 吸附，没有 `[−]` / `[+]` 按钮，也没有清除钮。和
阶段39 的 `C-TEXTAREA` 一样，**目录承诺了但代码没做**。

- **槽位沿用 slider 的约定**：`CMP-VALUE` = 当前值、`CMP-VALUE3` = 下限、
  `CMP-VALUE2` = 上限；新增 `CMP-VALUE4 []i64` = 步长（`<= 0` 一律按 1），随
  `cmp-alloc` 逐槽长。**不新增节点种类**：三者仍是叶子，`cs-measure` 早就给
  `CS-BTN-H` 的高，布局 / 命中路径不动。
- **值域与吸附**（`src/components.no`）：`num-range` / `num-step-set` / `num-step` /
  `num-min` / `num-max` / `num-clamp`（`hi <= 0` = 不夹上限，与 `ct-clamp` 同语义）/
  `num-snap`（先夹紧，再 `q = (v - lo + step/2) / step` 四舍五入到 `lo + k*step`，
  最后再夹一次——夹两次是因为 `lo` 非 0 时吸附可能越过上限）/ `num-value` /
  `num-set`（吸附 + 写 `VALUE` + 同步 `CMP-TEXT`）/ `num-bump`（`dir * step`，
  disabled 直接 return）/ `num-commit`（把 `CMP-TEXT` 解析回值，失败不动值）。
- **命中**：`search-hit(h, px, py, x, y, w, ht)` = 右端 `ht` 宽的正方形清除区；
  `stepper-hit` = `-1` 左 / `0` 中 / `1` 右，按钮宽 `bw = ht`，控件太窄
  （`w < 2*bw`）时 `bw = w / 2`。
- **渲染层**：`cs-paint-number-input`（右对齐 + 光标贴右端，x = 右边界 − 右内边距 −
  **光标后缀**宽，比 `cs-paint-field` 的「整串宽」更准）/ `cs-paint-stepper`
  （`[−]` 值 `[+]`，杠是纯轴对齐矩形：横杠 1200 × 200、加号竖杠 200 × 1200）/
  `cs-paint-search`（文本 + 光标 + 右端清除钮）。
- 清除钮用 **ASCII `'x'`** 字形（`cs-emit-text-center`），与 `cs-paint-select` 拿 `'v'`
  当箭头是同一路子：`shim.c` 写死 `/System/Library/Fonts/Supplemental/Arial.ttf`，
  非 ASCII 字形（`×` / `▼`）能不能画取决于字体覆盖，而 `src/*.no` 里此前没有任何
  非 ASCII 字形依赖；纯轴对齐矩形又拼不出对角线。且**只在有文本时画**清除钮。
- 落点：`src/components.no` 的 `CMP-VALUE4` + `num-*` / `search-*` / `stepper-hit`，
  `src/cmp-scene.no` 的 `cs-paint-number-input` / `-stepper` / `-search`；
  `tests/cmp-number.no` 42 项断言全绿（构造 4 / 值域 3 / 吸附 6 / set 2 / bump 4 /
  commit 5 / search 6 / stepper 5 / 渲染 3 / 几何 4）；`tests/main-number.no`
  窗口 demo（点步进器左右钮、点搜索右端、数字输入打字 + 回车提交、`r` 重置、
  `q` / ESC 退出），900 帧
  `bye frames=900 keys=0 clicks=0 num=40 stepper=50 search=` + `number render OK`、exit 0。
- 坑两条：① **`to-i64` 的 `ok` 臂标签本身是个 bool 变量**，函数返回值不能也叫 `ok`
  （`cannot assign i64 value to bool variable 'ok'`）；且 bool 只能赋 `true` / `false`，
  写 `done = 1` 同样报错。② **`cs-paint-*` 的落点断言要按「右内边距 / 居中」反算**：
  只断言「Scene 里有没有这段文本」测不出对齐，`t39` / `t40` 是直接拿
  `cs-text-at` 钉 `x = 右边界 − 文本宽` 与居中落点才钉住的。
- 回归：`tests/*.no`（排除 `main-*`，共 48 个）**PASS=47 FAIL=1**，唯一红项仍是
  `tests/dva-editor.no` 的 `trace/BPT trap`（既有环境漂移，与本阶段无关）；12 个
  `tests/main-*.no` 窗口 demo **全部 `rc=0`**（dummy 驱动），`main-dv` 输出与阶段38 /
  39 基线逐字节相同（`ops=125`）。`no vet src/components.no` 0 error / 6 warning /
  139 hint，与阶段39 基线逐项相同（新增代码 0 warning、0 hint）；`no vet
  src/cmp-scene.no` 的 hint 行号全部落在 68–192，本阶段新增的 1030–1121 行
  **零 hint**。

### §5.36 阶段 41 命令式 OTP 验证码输入（C-OTP-INPUT）新坑（2026-11-22 实测）

`src/components-catalog.md` 第 19 行写着 `C-OTP-INPUT` 的能力是「digits, slots,
活动槽 / 退格 / 灌入」，但命令式侧它只走单行 `cs-paint-field`——和阶段38 / 39 / 40
的 `C-INPUT` / `C-TEXTAREA` / 输入族一样，**目录承诺了但代码没做**。这是输入族里
最后一个仍落到单行字段画法的 kind。

- **槽数沿用 `CMP-VALUE`**（构造参数 `cmp-otp-input(len)`，`len <= 0` 按 1 槽）；新增
  两个池随 `cmp-alloc` 逐槽长：`OT-CODE []str`（**定长**数字串，空格 = 空槽）+
  `OT-CUR []i64`（活动槽号）。**不新增节点种类**：仍是叶子，`cs-measure` 的兜底分支
  加一条 OTP 宽度公式，布局 / 命中路径不动。
- **API**（`src/components.no`）：`otp-slots` / `otp-cur` / `otp-cur-set` / `otp-code`
  （短了补空格、长了截断）/ `otp-digit` / `otp-set-digit` / `otp-clear-slot` /
  `otp-clear` / `otp-filled` / `otp-digits`（去空格的已输入码）/ `otp-value` /
  `otp-complete` / `otp-put`（写一位后跳**下一个空槽**，没有空槽停末槽）/
  `otp-backspace`（活动槽有数字清它，否则回退清上一格）/ `otp-delete`（只清不移动）/
  `otp-left` / `otp-right` / `otp-set`（灌入字符串，只取数字、忽略多余）+
  几何与命中 `otp-slot-w` / `otp-slot-x` / `otp-hit`。所有写操作先查 `F-DISABLED`。
- **渲染层**：`cs-paint-otp`（整块字段底 + 每槽一格；数字居中；聚焦时活动槽四条
  `CS-TH-ACCENT` 描边，空槽再画一条竖光标）；`cs-measure` 宽度 = `n * ht + (n-1)*800`。
  槽格几何**渲染与命中同源**：`cs-paint-otp` 与 `otp-hit` 都调 `otp-slot-w` /
  `otp-slot-x`；`components.no` 看不到 `CS-GAP`，只能写字面量 `800` 并注明同值
  （与 §5.31 的写法一致）。
- 测试：`tests/cmp-otp.no` 48 项断言全绿（构造 5 / 写数字 4 / put 6 / 聚合 5 / 退格 5 /
  前删 1 / 移动 3 / 灌入 3 / disabled 1 / 几何 4 / 命中 5 / 渲染 6）；
  `tests/main-otp.no` 窗口 demo（6 槽 + 4 槽，点击选槽、数字 `otp-put` / 退格 /
  前删 / ←→↑↓ / `c` 清空 / `r` 重置 / `q` / ESC 退出），900 帧
  `bye frames=900 keys=0 clicks=0 otp=123456 cur=0 otp2=2468 cur2=0` + `otp render OK`、
  exit 0。
- 坑两条：① **插值 `{x}` 只在字符串字面量处于实参位时展开**——`s = '{s} '`（赋值
  RHS）存的是**字面量** `{s} `，拼接一律走 `s.push-str(t)`（`input-insert` 的写法）。
  `components.no` 开头「追加用插值 `s = '{s}{x}'`」的注释是错的 / 至少会误导，阶段41
  的 `otp-code` / `otp-set-digit` / `otp-clear-slot` / `otp-clear` / `otp-digits`
  全部按 `push-str` 写。② **`+` / `-` 产生 `?i64`**（溢出检查），自增一律
  `ct-inc` / `ct-add` / `ct-sub`——`i = i + 1` 会报 `cannot assign ?i64 value to i64
  variable 'i'`。
- 回归：见下方 §7 阶段 41 条目。

### §5.37 阶段 42 命令式 combobox 可输入下拉（C-COMBOBOX）新坑（2026-11-22 实测）

`src/components-catalog.md` 第 22 行写着 `C-COMBOBOX` 的能力是「query, options,
selected」，但命令式侧它只有 `cmp-combobox()` 一个空壳构造——**目录承诺了但代码
没做**，和阶段38 / 39 / 40 / 41 的 `C-INPUT` / `C-TEXTAREA` / 输入族 / `C-OTP-INPUT`
是同一类欠账。这是输入族里最后一个仍落到单行 `cs-paint-field` 的 kind。

- **复用而不是新造**：查询串沿用 `CMP-TEXT`（光标沿用 `INP-CAR`，编辑直接调
  `input-insert` / `input-backspace` / `input-delete`）；选项与 `select` 同构存
  `CHILD-IDX`（`cmp-child` 可读，`cs-child-flow` / `cs-hit-inner` 不用改）。另存
  一份**匹配表**：`CB-MBASE` / `CB-MN` / `CB-MLIST`（扁平只增的匹配下标池，同
  `INP-SG` 段池思路）+ `CB-MCUR` 高亮，四个池都随 `cmp-alloc` 长。**不新增节点
  种类**：仍是叶子，布局 / 命中路径只在「combobox 展开」这一态多走一层翻译。
- **API**（`src/components.no`）：`combobox-refresh`（空查询 = 全选；否则选项
  `to-lower` 后 `contains` 查询 `to-lower`，**大小写不敏感**；顺手把高亮夹紧到
  `[0, mn)`，没匹配置 -1）/ `-query` / `-set-query` / `-add-option` /
  `-option-count` / `-option` / `-selected` / `-choose`（写查询 + 收起 + 重算）/
  `-match-count` / `-match` / `-match-label` / `-match-index` / `-match-index-of` /
  `-highlight` / `-highlight-set` / `-highlight-move` / `-choose-match` / `-apply` /
  `-insert` / `-backspace` / `-delete` / `-clear`。所有写操作先查 `F-DISABLED`。
- **渲染层**：新增 `cs-paint-combobox`（字段底 + 查询文本 / 空则 placeholder +
  聚焦竖光标 + 右端 `'v'` 箭头；展开时**只画匹配项**，高亮行 `CS-TH-ACCENT` 底）；
  `cs-measure` 加展开分支（宽取**匹配项**里最宽的，收起床宽 `CS-MIN-LEAF-W`，
  展开 9600）；`cs-layout` 加展开分支只排匹配项；命中新增 `cs-hit-child`，把
  「第 i 个（虚拟）子节点」在 `C-COMBOBOX` 展开时翻译到匹配表，`cs-hit-inner`
  的两个循环统一改调它。**画的 / 点的 / 量的三者同源**（都读 `CB-MLIST`）。
  `components.no` 看不到 `CS-*` 常量，`combobox-*` 里的几何字面量（800 / 1200 /
  9600）与 `cmp-scene.no` 保持一致并注明同值。
- 测试：`tests/cmp-combobox.no` 41 项断言全绿（构造 3 / 加选项 4 / 大小写不敏感
  过滤 5 / 匹配反查 1 / 高亮夹紧与移动 5 / 选中与 choose-match 4 / 编辑查询 5 /
  disabled 1 / 测算与布局 5 / 渲染 4 / 命中 4）；`tests/main-combobox.no` 窗口 demo
  （水果 + 颜色两个，点字段开关、打字过滤、↑↓ 移高亮、回车选中、Esc 收起、
  `c` 清空、`r` 重置、`q` 退出），900 帧
  `bye frames=900 keys=0 clicks=0 cb1=Banana sel1=1 match1=1 open1=true cb2=Blue sel2=2 open2=false`
  + `combobox render OK`、exit 0。
- 坑三条：① **`-> { a; b }` 里的分号是注释不是分隔符**——`{ ct-set(...); return }`
  会把 `return` **和收尾的 `}` 一起**注释掉，于是这个 `{` 再不闭合，**整个模块从
  该行起静默截断**：后半截所有顶层名字报 `is not defined`（`cp-filter` 这种早已
  存在、且被别的模块正常调用的函数也会中招），而 `no fmt` 只报空行差异、不报真错。
  定位办法是「按行扫大括号深度」，看到 depth 从某行起卡在 1 就到顶了；修法是把
  同行多语句拆成两行（`ct-set(...)` 一行、`return` 一行）。② **nolang 没有前向
  引用**：函数定义必须在使用之前，所以 `combobox-refresh` 要放在整个 combobox 块
  的**最前面**（`add-option` / `set-query` / `insert` / `backspace` / `delete` /
  `clear` 都调它），而 `select-*` 系列在文件更后面——整块 combobox API 必须挪到
  `select-*` 之后（阶段42 就是把原 1736–1881 行整块搬到 `tab-title` 之后）。
  ③ **插值里不能调函数**：`print('n={f(x)}')` 报 `unresolved format field`，要先
  取局部再插值。
- 回归：见下方 §7 阶段 42 条目。

### §5.38 阶段 43 值域型展示控件（C-RATING / C-PAGINATION / C-EMPTY）+ slider 下限新坑（2026-11-22 实测）

`src/components-catalog.md` 第 67 / 33 / 10 行分别写着 `C-RATING` 的能力是
「value, max」、`C-PAGINATION` 是「page, total」、`C-EMPTY` 是「title,
description」，但命令式侧三者都只有一个构造函数——rating / pagination 连值都
没有 API，empty 的描述（`CMP-TEXT2`）更是没人画，渲染还落在 `cs-paint-leaf`
的通用 `rect + text` 兜底里。**目录承诺了但代码没做**，和阶段38–42 是同一类欠账。
审计这一批时还顺带挖出一条**真 bug**（见下面坑①）。

- **复用而不是新造**：槽位沿用既有约定——`CMP-VALUE` = 当前值，`CMP-VALUE2` =
  上限 / 满分 / 总页数，`CMP-VALUE3` = 下限。星格宽 = 行高（2400 = `CS-LINE-H`）、
  页码钮宽 = `CS-BTN-H`（4000）、内边距 1200 一律抄字面量（`components.no` 不能
  import 渲染层，同 `otp-slot-w` 的先例）。**不新增节点种类**：三者都仍是叶子。
- **API**（`src/components.no`）：`rating-max` / `-value` / `-set`（夹紧 `0..max`，
  `F-DISABLED` 时直接 return）/ `-cell` / `-hit`（命中第几颗星，0 起，没命中 -1）；
  `pagination-total` / `-page` / `-clamp`（页码夹到 `[1,total]`，`total <= 0` 按 1 页）/
  `-set` / `-prev` / `-next` / `-btn-w` / `-label`（「页/总数」）/ `-hit`（`-1` 没命中 /
  `0` 上一页 / `1` 标签 / `2` 下一页）；`slider-min` / `slider-max`（对称读取器，
  渲染层改走它们，不再直读 `CMP-VALUE3` / `CMP-VALUE2`）。
- **渲染层**（`src/cmp-scene.no`）：新增 `cs-paint-rating`（填满的星用组件色、没填的
  用 `CS-TH-MUTED`，星形用 ASCII `*`——同 select 的 `v` / search 的 `x` 一路，不依赖
  非 ASCII 字形的字体覆盖）、`cs-paint-pagination`（左 `<` / 中间「页/总数」/ 右 `>`，
  到顶 / 到底时对应钮画 `CS-TH-MUTED`——语义提示，不是禁用，禁用仍走 `F-DISABLED`）、
  `cs-paint-empty`（标题一行 + 描述一行，各在半个矩形里居中；没描述就只画标题），
  `cs-measure` 加三条 intrinsic 分支（rating 宽 = `max * 2400`、高 = 行高；pagination
  宽 = `2*4000 + 标签宽`、高 = `CS-BTN-H`；empty 宽取标题 / 描述里更宽的、有描述
  两行高、无描述单行高）。`cs-paint-leaf` 的 `C-SLIDER` 分支改读 `slider-min` /
  `slider-max`，并新增三行路由。
- **`C-PAGINATION` 从 `cs-is-container` 白名单里拿掉**：它的页码按钮是**数据**不是
  视觉子节点（同 `C-SELECT` / `C-COMBOBOX` / `C-TAB` 一路），混进白名单会让「给
  分页挂真子节点」和「虚拟页码」两套语义打架。
- 测试：`tests/cmp-rating.no` 54 项断言全绿（slider 下限回归 8 / rating 读写夹紧 6 /
  rating 命中 6 / rating 测算与渲染 7 / pagination 读写夹紧 10 / pagination 命中 6 /
  pagination 测算与渲染 6 / empty 5）；`tests/main-rating.no` 窗口 demo（滑条 + 星条 +
  分页 + 空态，点星条设星数、点页码钮翻页、点滑条设值，`a` `d` 星数、`z` `x` 翻页、
  `s` `w` 滑条、`e` 空态描述开关、`r` 重置、`q` 退出），900 帧
  `bye frames=900 keys=0 clicks=0 slider=40 rating=3/5 page=1/12 label=1/12`
  + `rating render OK`、exit 0。
- 坑两条：① **`cmp-slider(min,max)` 只写 `CMP-VALUE`（初值）不写 `CMP-VALUE3`
  （下限真源）**——`slider-value-at` / `slider-set` / `cs-paint-leaf` 都按 `VALUE3`
  当下限，于是 `min != 0` 时左端点返回 0。既有测试全拿 `min == 0` 构造，所以一直
  没暴露；`tests/cmp-rating.no` 的 `t1`–`t8` 用 `cmp-slider(20,100)` 把这条钉死
  （左 / 中 / 右采样 = 20 / 60 / 100），并额外用 `min == 0` 的滑条确认旧行为不变。
  ② **`cs-emit-text-center` 存进 Scene 的是居中后的落点**（`x + (w - tw)/2`、
  `y + (ht - 行高)/2`），**不是矩形左上角**——拿矩形坐标去断言文字颜色会全落空；
  另外 `rating-hit` 的右边界是 `x + max * cell`（星格总宽）而不是控件矩形宽 `w`，
  按矩形宽取点会误判成「没命中」。
- 回归：见下方 §7 阶段 43 条目。

### §5.39 阶段 44 命令式提示族（C-MESSAGE / C-ALERT / C-NOTIFICATION）新坑（2026-11-22 实测）

`src/components-catalog.md` 第 45 / 46 / 47 行分别写着 `C-NOTIFICATION` 的能力是
「level, timeout」、`C-ALERT` 是「level, closable」、`C-MESSAGE` 是「level」，
但命令式侧三者都只有一个构造函数——连 `level` 都没有 API，渲染还落在
`cs-paint-leaf` 的通用 `rect + text` 兜底里。**目录承诺了但代码没做**，和阶段38–43
是同一类欠账。

- **复用而不是新造**：level 语义三者共用一套——`LV-INFO` / `LV-OK` / `LV-WARN` /
  `LV-ERR` + `lv-slot`（level -> 主题槽下标，**表驱动**，`-1` = 没有对应槽）+
  `msg-level` / `msg-set-level`。槽位沿用既有约定：`CMP-VALUE` = level、
  `CMP-VALUE2` = 总时长、`CMP-VALUE3` = 剩余 / closable。几何常量一律抄字面量
  （`components.no` 不能 import 渲染层，同 `otp-slot-w` 先例）：色条 400、
  字形格 2400（= `CS-LINE-H`）、时间条 800（= `CS-PROGRESS-H`）、关闭钮 2400。
  **不新增节点种类**：三者都仍是叶子。
- **API**（`src/components.no`）：`lv-slot` / `msg-level` / `msg-set-level`
  （`F-DISABLED` 时直接 return）；`alert-closable` / `alert-set-closable` /
  `alert-close-w` / `alert-close-x`（矩形右缘往内 1200 起、宽 2400）/ `alert-hit`
  （`-1` 没命中 / `0` 主体 / `1` 关闭钮）；`ntf-timeout` / `ntf-remain` /
  `ntf-set-timeout`（同时把 remain 重置成 total）/ `ntf-set-remain` / `ntf-ratio`
  （`0..10000` 定点百分数，remain 夹进 `[0,total]`，`total <= 0` -> 0）。
- **渲染层**（`src/cmp-scene.no`）：`cs-level-warn`（琥珀常量——主题里没有琥珀
  语义槽，同 `dv.no` 的 `DV-C-NUM` 先例，是有意的折中）、`cs-level-color`
  （**消费 `lv-slot`**，未知 level 按 info 画，不夹紧写入）、`cs-level-glyph`
  （`i` / `v` / `!` / `x`，ASCII 字形同 select 的 `v` / search 的 `x` 一路，不依赖
  非 ASCII 字形的字体覆盖）、`cs-paint-message`（左色条 4px + 字形格 + 文本一行）、
  `cs-paint-alert`（message + 右端 `'x'` 关闭钮，`closable` 时才画）、
  `cs-paint-notification`（文本一行 + 剩余时间条一行：`CS-TH-TRACK` 轨道 +
  按 `ntf-ratio` 缩短的填充）。`cs-measure` 给 message / notification 加 intrinsic
  分支；`cs-paint-leaf` 加三行路由。
- 测试：`tests/cmp-alert.no` 62 项断言全绿（level 语义 12 / alert 读写与几何 5 /
  alert 命中 8 / notification 时长与比值 13 / 三者测算 7 / 三者渲染 17）；
  `tests/main-alert.no` 窗口 demo（message + alert + notification + 三条状态标签，
  点 alert 右端 `'x'` 关掉 closable，`l` `t` `n` 循环三者 level、`c` 开关 closable、
  `,` 扣剩余时间、`.` 重置、`r` 全部重置、`q` 退出），900 帧
  `bye frames=900 keys=0 clicks=0 msg-level=0 alert-level=3 closable=1 ntf-level=2
  ntf-ratio=10000` + `alert render OK`、exit 0。
- 坑三条：① **`cs-leaf-size` 只收 `(kind, tlen)`，没有句柄**——在它的分支里写
  `cmp-text(h).len()` **不会报错**（`h` 不是参数，解析到别处去了）但取不到文本，
  宽会算成 `CS-MIN-LEAF-W`；提示族的文本宽必须用 `tlen`。这类「静默解析到别处」
  的坑和 `cs-measure` 里 `h` 的作用域是同一类陷阱。② **alert 的关闭钮宽度必须加在
  最小宽度夹紧之前**，否则 `'hi'` 这种短文本会先被夹到 6400 再长回 9600，把关闭钮
  挤出固有宽；因此 `C-ALERT` 整个不走 `cs-leaf-size`，改在 `cs-measure` 的兜底分支
  里算（那里才有句柄可读 `alert-closable`）。③ **`cs-emit-rect` 存的是 `CS-RECT`
  不是 `CS-FILL`**——notification 的剩余时间轨道是 `CS-RECT`，拿只匹配 `CS-FILL` 的
  探针去断言它的颜色会恒落空，得单独写一个 `CS-RECT` 探针（`cs-rect-color-at`）。
- 回归：见下方 §7 阶段 44 条目。

### §5.40 阶段 45 命令式会话族（C-BUBBLE / C-SPEECH）新坑（2026-11-22 实测）

`src/components-catalog.md` 第 53 / 54 行分别写着 `C-BUBBLE` 的能力是
「text, position」、`C-SPEECH` 是「text, lang」，但命令式侧两者都只有一个构造函数
——`position` / `lang` 都没有 API，渲染还落在 `cs-paint-leaf` 的通用
`rect + text` 兜底里。**目录承诺了但代码没做**，和阶段38–44 是同一类欠账。

- **复用而不是新造**：几何常量一律抄字面量（`components.no` 不能 import 渲染层，
  同 `otp-slot-w` / 阶段44 先例）：内边距 1200（= `CS-PAD`）、尾巴方块 1200。
  槽位沿用既有约定：bubble 用 `CMP-VALUE` = position、`CMP-TEXT` = 文本、
  `CMP-COLOR` = 底色；speech 用 `CMP-TEXT` = 文本、`CMP-TEXT2` = 语言标签。
  **不新增节点种类**：两者都仍是叶子。
- **API**（`src/components.no`）：`bb-pad` / `bb-tail`（两个常量）+ `bubble-pos` /
  `bubble-set-pos`（夹紧 `0..1`，`F-DISABLED` 时直接 return）+ `speech-lang` /
  `speech-set-lang`（标签空 = 不画标签行）。
- **渲染层**（`src/cmp-scene.no`）：`cs-bubble-tail-x`（尾巴左缘，朝 position 那一边）
  + `cs-paint-bubble`（底色方块 + 内边距里的文本，pos 0 左对齐 / pos 1 右对齐，
  底边上的尾巴方块）+ `cs-paint-speech`（文本行 + 有标签时多一条 `CS-TH-BG` 细分隔线
  + 标签行）。`cs-leaf-size` 给 `C-BUBBLE` 加 intrinsic（宽 = 文本宽 + 左右内边距，
  高 = 行高 + 上下内边距）；`cs-measure` 的兜底分支给 `C-SPEECH` 加（宽取
  「文本宽 / 标签宽」更宽者，高在标签非空时多 `CS-SEP-H` + `CS-GAP` + 一行）；
  `cs-paint-leaf` 加两行路由。文本统一用 `CS-TH-BG`——气泡底色是组件色（默认深蓝 /
  灰），配背景色比配 `FG` 对比度高。
- 测试：`tests/cmp-bubble.no` 28 项断言全绿（bubble API 8 / 测算 3 / 渲染 pos 0 三项 /
  渲染 pos 1 两项 / speech API 4 / 测算 3 / 渲染 4）；`tests/main-bubble.no` 窗口 demo
  （两个 bubble + 两个 speech，点气泡翻面、点语音泡开关标签行，`l` `r` `g` `h` 分别
  翻 b1 / 翻 b2 / 开关 sp 标签 / 开关 sp2 标签、`q` 退出），900 帧
  `bye frames=900 keys=0 clicks=0 b1-pos=0 b2-pos=1 sp-lang=zh-CN sp2-lang=` +
  `bubble render OK`、exit 0。
- 坑两条：① **option match 的单行写法 `{ ok -> it -> 0 }` 会被类型检查拒**——
  `non-exhaustive match on option '?': missing arm(s): nil, err`；必须像
  `msg-level` / `ntf-timeout` 那样写成多行（`ok -> it` + 空行 + `-> 0`）。
  ② **`C-SPEECH` 的宽度取决于句柄上的 `speech-lang`，所以不能走 `cs-leaf-size`**
  （那里只有 `(kind, tlen)`），只能落在 `cs-measure` 的兜底分支里——和阶段44 的
  `C-ALERT` 完全同一个理由。
- 回归：见下方 §7 阶段 45 条目。

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
      功能对等长期项：DataTable 虚拟滚动 / Dock / 代码编辑器 / 树 / 命令面板（声明式层
      已补全五样的超集 —— 阶段 28 `dv-table-list`、阶段 29 `dv-dock-split`、
      阶段 30 `dv-code-editor`、阶段 31 `dv-tree-view`、阶段 32 `dv-command-palette`；
      命令式侧原本只有 API + 最小状态，其中**树**已由阶段 33 补上
      （`src/components.no` 的 `cmp-tree-*`，仍然只是普通 `C-TREE` 容器 +
      应用调出来的真组件行），**代码编辑器**已由阶段 34 补上
      （`src/components.no` 的 `cmp-editor-*` + `ed-lex-*`，根是 `C-SCROLL`，
      可见行由 `cmp-editor-sync` 两段式重建），**DataTable** 已由阶段 35 补上
      （`src/components.no` 的 `cmp-table-*`，根是普通 `C-TABLE` 容器，
      表头 + 滚动体 + 可见行由 `cmp-table-sync` 重建），**Dock** 已由阶段 36 补上
      （`src/components.no` 的 `cmp-dock-split*` / `cmp-dock-splittable` /
      `cmp-dock-split-val` / `cmp-dock-set-split` / `cmp-dock-grab` / `cmp-dock-up`
      + `src/cmp-scene.no` 的 `cs-dock-down` / `cs-dock-move`：间隙就是拖杆，
      位置真源 `DK-SPLIT-V`），**命令面板**已由阶段 37 补上
      （`src/components.no` 的 `cmp-command-*`：根就是 `C-COMMAND` 普通竖排容器，
      输入框 + 滚动体两个直接子，可见行由 `cmp-command-sync` 两段式重建，
      游标真源 `CP-CUR`，命中表抄本 `CP-SRC` / `CP-SB`）—— 五个功能对等长期项
      到此全部补齐；**输入框的富文本 / 语法高亮**（组件目录里最后一条长期项）
      已由阶段 38 补上（`INP-*` 光标池 + `input-insert` / `-delete` / `-syntax` /
      `-lex-*` + `src/cmp-scene.no` 的 `cs-paint-input` / `cs-paint-input-lex`，
      见 §5.33），命令式侧不再有「只有 API + 最小状态」的组件）。
- [x] 阶段 39：命令式多行文本域 — `src/components.no` 的新池 `TA-OFF`（每句柄视口首行
      行号，随 `cmp-alloc` 逐槽长）+ `ta-line-count` / `-line-start` / `-line-len` /
      `-line-text` / `ta-caret-line` / `ta-caret-col` / `ta-move-up` / `ta-move-down` /
      `ta-off` / `ta-set-off` / `ta-follow`；`src/cmp-scene.no` 的新 `cs-paint-textarea`
      （`C-TEXTAREA` 从 `cs-paint-field` 改走它，逐行画 + 视口裁剪 + 行内光标）。
      文本 / 光标仍走 `CMP-TEXT` + `INP-CAR`，**编辑复用阶段38 的 `input-insert` /
      `-backspace` / `-delete`**（行首退格并上行、行尾前删并下行都是推论），
      不新增节点种类。`tests/cmp-textarea.no` 34 项断言全绿、
      `tests/main-textarea.no` 窗口 demo 900 帧 `caret=26 line=2 col=6 off=0 lines=3`
      + `textarea render OK`（见 §5.34）。
- [x] 阶段 40：命令式输入族（数字输入 / 步进器 / 搜索框）— `src/components.no` 的新池
      `CMP-VALUE4`（步长，随 `cmp-alloc` 逐槽长）+ `num-range` / `num-step-set` /
      `num-step` / `num-min` / `num-max` / `num-clamp` / `num-snap` / `num-value` /
      `num-set` / `num-bump` / `num-commit` + `search-query` / `search-clear` /
      `search-hit` / `stepper-hit`；`src/cmp-scene.no` 的新 `cs-paint-number-input`
      （右对齐 + 贴右端光标）/ `cs-paint-stepper`（`[−]` 值 `[+]`）/ `cs-paint-search`
      （文本 + 光标 + 右端清除钮）（三者从 `cs-paint-field` 改走各自分支）。槽位沿用
      slider 的 `VALUE` / `VALUE3` / `VALUE2`，**不新增节点种类**。
      `tests/cmp-number.no` 42 项断言全绿、`tests/main-number.no` 窗口 demo 900 帧
      `num=40 stepper=50` + `number render OK`（见 §5.35）。
- [x] 阶段 41：命令式 OTP 验证码输入 — `src/components.no` 的新池 `OT-CODE []str`
      （定长码，空格 = 空槽）+ `OT-CUR []i64`（活动槽，均随 `cmp-alloc` 逐槽长）+
      `otp-slots` / `otp-cur(-set)` / `otp-code` / `otp-digit` / `otp-set-digit` /
      `otp-clear-slot` / `otp-clear` / `otp-filled` / `otp-digits` / `otp-value` /
      `otp-complete` / `otp-put`（跳下一个空槽）/ `otp-backspace` / `otp-delete` /
      `otp-left` / `otp-right` / `otp-set` / `otp-slot-w` / `otp-slot-x` /
      `otp-hit`；`src/cmp-scene.no` 的新 `cs-paint-otp` + `cs-measure` 的 OTP 宽度
      分支（`C-OTP-INPUT` 从 `cs-paint-field` 改走它）。槽数沿用 `CMP-VALUE`，
      **不新增节点种类**；槽格几何渲染与命中同源。`tests/cmp-otp.no` 48 项断言
      全绿、`tests/main-otp.no` 窗口 demo 900 帧
      `otp=123456 cur=0 otp2=2468 cur2=0` + `otp render OK`（见 §5.36）。
      回归：`tests/*.no` 排除 `main-*` 共 49 项 PASS=48 FAIL=1（唯一红项仍是
      `tests/dva-editor.no` 的既有工具链漂移）；13 个 `tests/main-*.no` 窗口 demo
      全部 `rc=0`，`main-dv` 输出与阶段38 / 39 / 40 基线逐字节相同（`ops=125`）；
      `no vet` `components.no` 0/6/139、`cmp-scene.no` 0/0（hint 行号 68–192）；
      `tests/logic` ALL TESTS PASSED；`make test` 仍只有 `parser/dump` 的
      `TestS1RawCondFixturesAreExercised` 一个既有红项。
- [x] 阶段 42：命令式 combobox 可输入下拉 — `src/components.no` 的新池
      `CB-MBASE []i64` / `CB-MN []i64` / `CB-MLIST []i64`（扁平只增的匹配下标池）
      + `CB-MCUR []i64`（高亮，均随 `cmp-alloc` 长）+ `combobox-refresh` /
      `-query` / `-set-query` / `-add-option` / `-option-count` / `-option` /
      `-selected` / `-choose` / `-match-count` / `-match` / `-match-label` /
      `-match-index` / `-match-index-of` / `-highlight` / `-highlight-set` /
      `-highlight-move` / `-choose-match` / `-apply` / `-insert` / `-backspace` /
      `-delete` / `-clear`；`src/cmp-scene.no` 的新 `cs-paint-combobox` +
      `cs-hit-child` + `cs-measure` / `cs-layout` 的展开分支（`C-COMBOBOX` 从
      `cs-paint-field` 改走它）。查询串沿用 `CMP-TEXT` + `INP-CAR`，选项与 select
      同构存 `CHILD-IDX`，**不新增节点种类**；画的 / 点的 / 量的都读 `CB-MLIST`。
      `tests/cmp-combobox.no` 41 项断言全绿、`tests/main-combobox.no` 窗口 demo
      900 帧 `cb1=Banana sel1=1 match1=1 open1=true cb2=Blue sel2=2 open2=false`
      + `combobox render OK`（见 §5.37）。回归：`tests/*.no` 排除 `main-*` 共 50 项
      PASS=49 FAIL=1（唯一红项仍是 `tests/dva-editor.no` 的既有工具链漂移）；
      14 个 `tests/main-*.no` 窗口 demo 全部 `rc=0`，`main-dv` 输出与阶段38 / 39 /
      40 / 41 基线逐字节相同（`ops=125`）；`no vet` `components.no` 0/6/139、
      `cmp-scene.no` 0/0（hint 行号 68–192）；`tests/logic` ALL TESTS PASSED；
      `make test` 仍只有 `parser/dump` 的 `TestS1RawCondFixturesAreExercised`
      一个既有红项。
- [x] 阶段 45：命令式会话族（bubble / speech）—
      `src/components.no` 的新段：`bb-pad` / `bb-tail`（两个几何常量）+
      `bubble-pos` / `bubble-set-pos`（`CMP-VALUE` 存 position，夹紧 `0..1`）+
      `speech-lang` / `speech-set-lang`（`CMP-TEXT2` 存语言标签）；
      `src/cmp-scene.no` 的新 `cs-bubble-tail-x` / `cs-paint-bubble` /
      `cs-paint-speech` + `cs-leaf-size` 一条 `C-BUBBLE` intrinsic（宽 = 文本宽 +
      左右内边距）+ `cs-measure` 兜底分支的 `C-SPEECH`（宽取文本 / 标签更宽者，
      高在标签非空时多一条分隔线 + 一行）+ `cs-paint-leaf` 两行路由。槽位沿用
      `VALUE` / `TEXT` / `TEXT2`，**不新增节点种类**。
      `tests/cmp-bubble.no` 28 项断言全绿、`tests/main-bubble.no` 窗口 demo
      900 帧 `b1-pos=0 b2-pos=1 sp-lang=zh-CN sp2-lang=` + `bubble render OK`
      （见 §5.40）。回归：见下方阶段 45 汇总。
- [x] 阶段 44：命令式提示族（message / alert / notification）—
      `src/components.no` 的新段：共用的 `LV-INFO` / `LV-OK` / `LV-WARN` /
      `LV-ERR` + `lv-slot`（level -> 主题槽下标，表驱动）+ `msg-level` /
      `msg-set-level`；alert 的 `alert-closable` / `-set-closable` /
      `alert-close-w` / `alert-close-x` / `alert-hit`；notification 的
      `ntf-timeout` / `ntf-remain` / `ntf-set-timeout` / `ntf-set-remain` /
      `ntf-ratio`；`src/cmp-scene.no` 的新 `cs-level-warn` / `cs-level-color` /
      `cs-level-glyph` / `cs-paint-message` / `cs-paint-alert` /
      `cs-paint-notification` + `cs-measure` 两条 intrinsic 分支（message /
      notification；`C-ALERT` 因关闭钮宽度必须在最小宽夹紧之前，整个改在
      `cs-measure` 的兜底分支里算）+ `cs-paint-leaf` 三行路由。槽位沿用
      `VALUE` / `VALUE2` / `VALUE3`，**不新增节点种类**。
      `tests/cmp-alert.no` 62 项断言全绿、`tests/main-alert.no` 窗口 demo
      900 帧 `msg-level=0 alert-level=3 closable=1 ntf-level=2 ntf-ratio=10000`
      + `alert render OK`（见 §5.39）。回归：`tests/*.no` 排除 `main-*` 共 52 项
      PASS=51 FAIL=1（唯一红项仍是 `tests/dva-editor.no` 的既有工具链漂移）；
      16 个 `tests/main-*.no` 窗口 demo 全部 `rc=0`，`main-dv` 输出与阶段38–43
      基线逐字节相同（`ops=125`）；`no vet` `components.no` 0/6/139、
      `cmp-scene.no` 0/0（hint 162）；`tests/logic` ALL TESTS PASSED；
      `make test` 仍只有 `parser/dump` 的 `TestS1RawCondFixturesAreExercised`
      一个既有红项。
- [x] 阶段 43：命令式值域型展示控件（rating / pagination / empty）+ slider 下限修复 —
      `src/components.no` 的新段：`slider-min` / `slider-max`（对称读取器）、
      `rating-max` / `-value` / `-set` / `-cell` / `-hit`、`pagination-total` /
      `-page` / `-clamp` / `-set` / `-prev` / `-next` / `-btn-w` / `-label` /
      `-hit`；`src/cmp-scene.no` 的新 `cs-paint-rating` / `cs-paint-pagination` /
      `cs-paint-empty` + `cs-measure` 三条 intrinsic 分支 + `cs-paint-leaf` 的
      `C-SLIDER` 改读 `slider-min` / `slider-max` 并路由三个新 kind；
      `cs-is-container` 拿掉 `C-PAGINATION`（页码按钮是数据不是视觉子节点）。
      槽位沿用 `VALUE` / `VALUE2` / `VALUE3`，**不新增节点种类**。
      **顺带修一条真 bug**：`cmp-slider(min,max)` 没写下限真源 `CMP-VALUE3`，
      `min != 0` 时 `slider-value-at` / `slider-set` / `cs-paint-leaf` 的左端点
      返回 0（既有测试全拿 `min == 0` 构造，故一直未暴露）。
      `tests/cmp-rating.no` 54 项断言全绿、`tests/main-rating.no` 窗口 demo
      900 帧 `slider=40 rating=3/5 page=1/12 label=1/12` + `rating render OK`
      （见 §5.38）。回归：`tests/*.no` 排除 `main-*` 共 51 项 PASS=50 FAIL=1
      （唯一红项仍是 `tests/dva-editor.no` 的既有工具链漂移）；15 个
      `tests/main-*.no` 窗口 demo 全部 `rc=0`，`main-dv` 输出与阶段38–42 基线
      逐字节相同（`ops=125`）；`no vet` `components.no` 0/6/139、`cmp-scene.no`
      0/0（hint 162，行号 68–192）；`tests/logic` ALL TESTS PASSED；
      `make test` 仍只有 `parser/dump` 的 `TestS1RawCondFixturesAreExercised`
      一个既有红项。
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
- [x] 阶段 23：声明式滚动 — `src/dv.no` 新原语 `dv-scroll(S, key, offslot, step,
      parent)`（`C-SCROLL`：`value = S[offslot]`、`value2 = step`、
      `DV-A-CHANGE` 载荷 = 偏移槽）+ `dv-apply-props` 的 `VALUE2` 同步补
      `C-SCROLL` 分支；`src/components.no` 加 `CMP-SCROLL-MAX` 池 +
      `cmp-scroll-max` / `cmp-set-scroll-max` + `cmp-parent-of`；
      `src/cmp-scene.no` 加 `C-SCROLL` 的布局两遍（累加内容高 → 夹紧 `off`
      写 `CMP-SCROLL-MAX` → 按 `off` 排版，**不写回状态**）、绘制推 `CS-CLIP`
      裁剪后画子树再恢复、命中包装（视口外不命中 / 视口内按全局坐标直接命中，
      阶段 24 修掉了多换算一次 `py + off`），
      通用布局分支显式加 `kind != C-SCROLL`（`->` 链不互斥），三条指令发射器
      统一 `cs-emit-clip`（`CS-CF/CX/CY/CW/CH` 池 + `cs-scene-*` 读取器）；
      `src/app.no` 加 `_noui-clip` / `_noui_clip_clear` + `APP-WHEEL` +
      `app-wheel()` + `app-draw-scene` 逐指令设裁剪 + `app-poll` 解 bit56..63
      的有符号增量；`src/shim/shim.c` `.h` 加 `noui_clip` / `noui_clip_clear`
      + 滚轮事件 `type = 5`（x/y = 光标，`integer_y` 夹 int8 放 bit56..63）；
      `src/dv-app.no` 加 `dva-wheel(S, root)`（顺父链找容器 → 夹紧 →
      `dva-set` 回状态槽）+ 主循环 `t == 5` 分支；
      `tests/dva-scroll.no` 33 项断言全绿（标记 / 布局偏移 + 夹紧 + 不写回 /
      裁剪指令 / 视口命中换算 / 滚轮两端夹紧与父链查找）；
      `tests/main-dv.no` 右栏顶部加 10 行 · 视口 160px 的滚动列表
      （`dsc = dv-scroll(DEMO, 55, D-SCROLL, DEMO-SCROLL-STEP, rc)`，
      内容约 336px、`cmp-scroll-max == 17600`），新增 `D-SCROLL` 槽 +
      `DEMO-SCROLL*` 数据，页头回显 `scrl=`，reset 多写 `D-SCROLL`，
      `dv-app-click` 的 CHANGE 分支收紧为只认 `C-SLIDER`，
      首帧 `ops` 53 → 64，`bye … scrl=0`、`exit 0`。
- [x] 阶段 24：声明式虚拟列表 — `src/dv.no` 新池 `DV-SCROLL-CONTENT` /
      `DV-SCROLL-START`（`dv-clear` 清、`dv-node` 补 0）+ `dv-scroll-content` /
      `dv-scroll-content-of` / `dv-scroll-start` / `dv-scroll-start-of`，
      `dv-apply-props` 末尾**无条件**同步到活节点（0 = 没声明，普通滚动不受
      影响）+ `dv-repeat-at`（`dv-repeat-items` 的切片版：只建
      `start .. start+count-1`，行 key = `key0 + 绝对行号`（窗口滑动复用同一批
      组件），`rowh > 0` 时给每行 `dv-size` 定高）+ `dv-list-view`（一行式：
      窗口 `i0 = (off - pad - rowh)/行距 + 1` 夹到 `[0, n-1]`、
      `i1 = ceil((off - pad + viewh)/行距)` 夹到 `n`，声明内容高
      `n*rowh + (n-1)*CS-GAP + 2*CS-PAD`）；`src/components.no` 加
      `CMP-SCROLL-CONTENT` / `CMP-SCROLL-START` 池与四个读写函数
      （`cmp-alloc` 各 push 0）；`src/cmp-scene.no` 的 `C-SCROLL` 布局拆成
      实测 / 声明两遍（`decl > 0` 时内容高直接用声明值，第 j 个儿子排在
      `y + pad - off + (i0 + j) * 行距`），`cs-hit` 命中包装修正为「视口外
      不命中、视口内按全局坐标直接 `cs-hit-inner`」；
      `tests/dva-virtual.no` 36 项断言全绿（标记 / 布局含 `cmp-scroll-max`、
      只挂可见行、绝对行号定位、两端夹紧不写回 / 裁剪 / 命中 / 滚轮含窗口
      滑动复用 / 根下直接子与绝对行 key）；`tests/dva-scroll.no` 的命中断言点
      随修正从 `py = 2200` 改成 `py = 7000`；
      `tests/main-dv.no` 右栏那条列表换成 20 行 · 视口 160px 的虚拟列表
      （`dv-list-view(DEMO, 55, D-SCROLL, …, 93, rc)`，内容高 736px，任意时刻
      只挂 5 行），首帧 `ops` 64 → 59，`bye … scrl=0`、`exit 0`（连跑两次一致）。
- [x] 阶段 25：声明式过滤列表 — `src/dv.no` 加 `dv-win-range`（可见窗口
      `[i0, i1)`，结果写进 `out[0]` / `out[1]`，`dv-list-view` 也改用它）+
      `dv-repeat-mapped`（`dv-repeat-at` 的「按下标表取行」版：挂
      `idxs[start .. start+count)`，行 key = 源行号，越界 / 负数下标夹到
      `[0, m-1]`，`count` 超出表长只挂到表尾）+ `dv-filter-map` /
      `dv-filter-count`（查询来自动态字符串状态 `DV-BIND-T[qslot]`，两边
      `to-lower` 后 `str.index` —— 大小写不敏感，空查询命中全部）+
      `dv-filter-list`（`dv-list-view` 的过滤版，内容高
      `命中数*rowh + (命中数-1)*CS-GAP + 2*CS-PAD`）；`src/cmp-scene.no` 的
      `C-SCROLL` 布局分支去掉 `n > 0` 条件（命中 0 行时 `maxoff` 夹到 0，
      否则 `cmp-scroll-max` 留旧值），第二遍排版改成
      `(decl > 0) && (n > 0)`；`tests/dva-filter.no` 46 项断言全绿（标记 /
      过滤含大小写不敏感与无命中 / 布局含命中数驱动内容高 / 裁剪 / 命中 /
      滚轮含组件复用 / 映射含表长截断）；`tests/main-dv.no` 页头加搜索框、
      右栏列表换成 `dv-filter-list`、页头回显 `match=N`，首帧 `ops` 59 → 60，
      `bye … scrl=0`、`exit 0`。
- [x] 阶段 26：声明式排序 / 分组 — `src/dv.no` 加 `dv-sort-run`（稳定原地插入
      排序）/ `dv-sort-map`（键 → 下标表，`n` 夹到 `keys.len()`、负数当 0，
      mode 1 升 / 2 降 / 其它源序）/ `dv-group-plan`（下标表 → 混合行表，组号
      变化写 token `-(g)-1`（负 = 标题），**组序跟着 plan 走**）/
      `dv-repeat-plan`（token < 0 挂标题行（key = `hkey0 + 组号`、
      `dv-disabled()` + `hcolor`），token ≥ 0 挂数据行（key = `key0 + 源行号`），
      行数按**混合行表长度**夹 —— 分组时窗口行数会超过数据行数）/
      `dv-order-list`（`dv-list-view` 的顺序版，mode 0/1/2/3/4，内容高按混合
      行数 `m` 算，所以分组后 `maxoff` 也跟着变大）；
      `tests/dva-order.no` 43 项断言全绿（排序含稳定性与越界 / 分组含 token 与
      组序 / 布局含内容高与 maxoff 随模式变、标题禁用 + 主题色、换模式按源行号
      复用组件、超界夹紧不写回 / 裁剪 / 命中 / 滚轮）；
      `tests/main-dv.no` 右栏列表换成 `dv-order-list`、上方加「order」下拉
      （`dv-select-from` 第二次出场）、页头回显 `sort=`，reset 多写 `D-SORT` /
      `D-SOPEN2`，撤掉搜索框与 `match=`（过滤与排序是两根轴，demo 一次只演
      一根），首帧 `ops` 60 → 65，`bye … sort=0`、`exit 0`。
- [x] 阶段 27：声明式表单校验 — `src/dv.no` 加 `DV-R-REQ/MINL/MAXL/MINV/MAXV/EQ/HAS`
      （= 1..7）+ `dv-rule-check(v str, spars []str, kinds []i64, pars []i64,
      base i64, n i64)`（单字段单规则表 → 第一条违反的规则号，**全局规则下标 + 1**
      不是切片内序号（`msgs` 是逐条规则的扁平表，字段只是切片，返回全局号才查得到
      措辞）；长度类先 `trim`，数值类先 `to-i64`（解析失败算违反），`EQ` 的 `par`
      = 另一字符串槽号、`HAS` 的 `par` = `spars` 下标；`base`/`n` 越界按「没这条
      规则」吞掉）+ `dv-rule-msg(code, msgs)`（规则号 → 文案，0 / 越界空串）+
      `dv-form-bad(slots, bases, ns, spars, kinds, pars, nf)`（多字段汇总「坏了
      几项」，`nf` 超表长按表长）+ `dv-field(key, errkey, slot, label, ph, show,
      spars, kinds, pars, base, n, msgs, parent)`（一行 = `dv-label(key)` +
      `dv-input(key+1)` + `dv-bind(输入, slot, 0)`，且仅当「有违反 && `show != 0`」
      时 `dv-label(errkey, 文案)` + `dv-var-danger()`；返回输入句柄 —— 「什么时候
      显示错误」是另一根轴）；
      `tests/dva-valid.no` 46 项断言全绿（规则逐条 / `trim` / 多规则取第一条 /
      切片与越界吞掉 / 未知种类忽略 / 汇总 / 揭示前后视图差量 / 输入绑定 /
      提交 flags = f(坏项) / 组件复用与零操作 / 绘制 / 命中）；
      `tests/main-dv.no` 左栏换成 `dv-scroll` 滚动容器 + 四字段校验表单 +
      显式提交命令（`bad > 0` → 写 `D-VSHOW`，`bad == 0` → `D-VSUB + 1` + 通知），
      新增 `D-VSHOW/D-VSUB/D-LSCROLL` 槽与四个字符串槽，页头回显 `vsub=` / `lsc=`，
      首帧 `ops` 65 → 76，`bye … vsub=0 lsc=0`、`exit 0`（见 §5.21）。
- [x] 阶段 28：声明式虚拟表格 — `src/dv.no` 加
      `dv-table-rows(parent, key0, hkey0, ckey0, start, count, ncols, colws, tw,
      plan, names, cells, acts, pays, hcolor, rowh)`（混合行表 → 窗口内的表格行：
      `token < 0` 挂通栏组标题（`key = hkey0 + 组号`、`names[组号]`、
      `dv-disabled()` + `hcolor`、宽 = `tw`），`token >= 0` 挂 `C-GRID` 行
      （`key = key0 + 源行号`、宽 = `tw`、高 = `rowh`）+ `ncols` 个 `C-LABEL`
      单元格（`key = ckey0 + 源行号*ncols + 列号`、文案 `cells[...]`、
      `colws[c] > 0` 时定宽），行动作 = `acts[源行号]` / `pays[源行号]`；
      `start` 先夹进 `[0, plan.len()]`、`count` 再按剩余可挂行数夹 —— 两端
      越界时一行不挂）+
      `dv-table-list(S, key, hkey, offslot, step, viewh, rowh, plan, ncols,
      colws, headers, names, cells, acts, pays, key0, ckey0, hkey0, hcolor,
      parent)`（表头 `C-GRID`（不滚，行高 `CS-PAD + CS-LINE-H`）+ `dv-scroll`
      + 可见窗口的行；表宽 `tw = 2*CS-PAD + 列宽和 + (ncols-1)*CS-GAP` 自己算；
      内容高按混合行数 `m` 算 —— `ncols = 1` 且不分组时与 `dv-order-list`
      行为等价）；
      `tests/dva-table.no` 32 项断言全绿（行结构 / 列宽 / plan 映射 / 通栏组标题 /
      `count` 与 `start` 两端夹紧 / 表头 + 声明内容高 → `maxoff` / 窗口切片 /
      状态驱动偏移 / 组件复用与零操作 / 空 plan / 裁剪 / 命中与派发）；
      `tests/main-dv.no` 右栏列表换成两列表格（`name` / `key`），顺序链抽成
      `dv-demo-order-plan(S)` 复用五档模式，左栏让宽 300px → 220px、右栏视口
      160px → 112px，首帧 `ops` 76 → 83，`bye … sort=0`、`exit 0`（见 §5.22）。
- [x] 阶段 29：声明式可拆面板 — `src/dv.no` 加
      `dv-dock-split(key, slot, minw, maxw, gw, parent)`（`C-DOCK` 横向容器 +
      `L-H` 提示 + `gap = gw` + 动作 `DV-A-CHANGE` / 载荷 = 状态槽 + `value` =
      最小宽 / `value2` = 最大宽；`dv-apply-props` 补 `C-DOCK` 的 `value2` 同步）；
      `src/dv-app.no` 加 `DVA-DRAG` + `dva-drag-down/move/up/of`（间隙即拖杆：
      命中 `C-DOCK` / `C-RESIZABLE` 且动作是 `DV-A-CHANGE` 时按光标是否落在
      「首子节点右缘 .. +gap」判抓住；移动按 `px − 左屏.x`（竖向 `py − 上屏.y`）
      夹进 `[value, value2]` 写回状态槽），主循环 `t == 2` 抓住则跳过点击归约、
      `t == 3` 追加 `dva-drag-move`、新增 `t == 6` 抬手分支；
      `src/shim/shim.c` 加 `SDL_EVENT_MOUSE_BUTTON_UP` -> 事件类型 6 并重建
      `dist/*.a`；
      `tests/dva-dock.no` 31 项断言全绿（标记 / 布局 / 命中 / 拖拽 / 竖拆 / 重建
      六组，含「点间隙命中容器」「内边距不抓」「上下限夹紧」「松手后不动」
      「只动 `px` 值不变」）；
      `tests/main-dv.no` 两栏行 `dv-row` → `dv-dock-split`（左屏宽 = 状态槽，
      初值 22000，最小 12000 / 最大 42000 / 拖杆 800），节点数不变、首帧
      `ops` 仍 83，`bye … dock=22000`、`exit 0`（见 §5.23）。
- [x] 阶段 30：声明式代码编辑器 — `src/dv.no` 加
      `dv-code-editor(S, key, tslot, offslot, step, viewh, rowh, gutter, key0,
      parent)`（`dv-scroll` + 定高 + `dv-scroll-content`（内容高 = `n*行高 +
      (n-1)*间隙 + 2*内边距`）+ `dv-scroll-start` + `dv-edit` + 内容 `C-FORM` +
      `dv-win-range` 切出的可见行）+ `dv-edit-line`（行 `C-FORM` + 右对齐行号
      `C-LABEL` + 逐 token 的 `C-LABEL`；光标 token 拆成「前缀 + 2px 的
      `C-SEPARATOR` + 后缀」；空行只挂光标）+ `dv-edit-split` / `dv-edit-join`
      （保留结尾空行）+ `dv-edit-set` / `dv-edit-insert`（光标前进**插入串长度**）/
      `dv-edit-backspace` / `dv-edit-enter` / `dv-edit-del-line` /
      `dv-edit-ins-line` / `dv-edit-move`（跨行落上一行末尾，夹取与跨行用标志拆开）/
      `dv-edit-key`（键码 -> 字符查 `ED-PRINT` 表；负光标入口归零）/
      `dv-edit-follow`（`DV-ED-SEEN` 去重，光标没动不重排窗口）+ 着色器
      `dv-lex-line` / `dv-run-push`（**相邻同色合并**）/ `dv-lex-quote` /
      `dv-is-kw` / `dv-word-byte` / `dv-byte-at` / `dv-c-*`（五种 token 色）+
      状态池 `DV-EDIT-L` / `DV-EDIT-C` / `DV-ED-LINES` / `DV-ED-KEEP` /
      `DV-ED-SEEN` / `DV-RUN-T` / `DV-RUN-C`，`dv-clear` 改为**只清 `DV-EDIT`**、
      `dv-node` 不再为光标池 push；
      `src/dv-app.no` 加 `DVA-EDIT-FOCUS` + `dva-edit-find`（活父链上溯到
      `dv-edit` 声明处，认的是**声明式 key**）+ `dva-edit-keyboard`，`dva-focus`
      加「命中到编辑器 -> 释放输入焦点 + 记编辑器 key」支路，主循环 `t == 4`
      改成 输入焦点 -> 编辑器焦点 -> 全局快捷键；
      `tests/dva-editor.no` 61 项断言全绿（着色 / 行模型 / 编辑原语 / 窗口 /
      跟随 / 装配与焦点交接 / 重建七组）；
      `tests/main-dv.no` 左栏底部接 `dv-code-editor`（初始文本 = 一段 noui 代码，
      行高 24px / 视口 144px / 行号槽 48px / 步长 32px），新增 `D-ED-OFF = 24`
      与字符串槽 `D-ED-TEXT = 6`，页头回显 `edoff=`、`bye` 也带 `edoff=`，
      reset 多写 `D-ED-OFF = 0` + 文本回初始值，首帧 `ops` 83 -> 105、
      `bye … edoff=0`、`exit 0`（见 §5.24）。
- [x] 阶段 31：声明式树 — `src/dv.no` 加
      `dv-tree-flat(S, openslot, n, kids0, kids, parents, out, deps)`（**显式栈的
      迭代式先序**，数据形状 = 「先序序列 + 兄弟块相邻」，产出可见行表 + 每行层级；
      栈自己用 `with-len(n)` + `sp` 下标模拟，避开 `vec.pop()` 的 `%option_i64`）+
      `dv-tree-row(...)`（一行 = `C-FORM` + 右对齐「缩进 + 箭头」`C-LABEL` + 文本
      `C-LABEL`；选中态三处一起变）+ `dv-tree-view(...)`（`dv-scroll` +
      `dv-scroll-content` + `dv-scroll-start` + `dv-win-range` + 逐行 `dv-tree-row`，
      行 key = `key0 + 节点号*10000000` 按源节点号）+ `DV-A-FILL = 11`（一次把
      `S[payload .. payload+value2)` 全写成 `value`）+ `TR-STRIDE` / `TR-INDENT` /
      `TR-ARROW-W` / `TR-ARROW-OPEN` / `TR-ARROW-SHUT` 常量；
      `tests/dva-tree.no` 42 项断言全绿（标记 / 调和 / 派发归约 / 端到端四组）；
      `tests/main-dv.no` 左栏顶部接 `dv-tree-view`（13 节点 = noui 自己的目录）+
      `expand all` / `collapse all` 两个幽灵按钮，新增 `D-TOPEN = 25` /
      `D-TSEL = 38` / `D-TOFF = 39` 与 `DEMO-TREE-*`，页头与 `bye` 加 `tsel=` /
      `toff=`，首帧 `ops` 105 -> 124、`bye … tsel=-1 toff=0`、`exit 0`
      （见 §5.25）。
- [x] 阶段 32：声明式命令面板 — `src/dv.no` 加
      `dv-command-palette(S, key, openslot, qslot, offslot, cmdslot, n, texts, acts,
      key0, parent)`（关着时只登记 key / 槽映射 + `DV-PAL-WAS` 翻转检测；开着时建
      `dv-on-overlay()` 的居中 `C-FORM` + `dv-input`（`dv-bind(qslot)`）+ `dv-scroll`
      （`dv-lay-grow()`）+ `dv-scroll-content` + `dv-scroll-start` + 游标夹紧 + 逐行
      `C-FORM` / `C-LABEL`，两者同挂 `DV-A-RUN`）+ `dv-filter-map`（查询串 -> 命中的
      **源行号**表，空查询命中全部）+ `dv-pal-*`（keys-ensure / of / open-key / move /
      cur-key / reset / close / type / backspace）+ `DV-A-RUN = 12`（`dv-apply` 先写
      `S[payload] = value`，`value2 >= 0` 再写 `S[value2] = 0`）+ 状态池
      `DV-PAL-OPEN/Q/CMD/KEY0/WAS/CUR/M/ROOT/SRC/SB` 与 `PAL-ROWH/VIEW/STEP/STRIDE`；
      `src/dv-app.no` 加 `DVA-PAL-FOCUS`（存声明式 key，-1 = 无面板）+
      `dva-pal-find`（活父链上溯到声明处）+ `dva-pal-keyboard`（↑/↓ 移动、回车
      `dv-pal-cur-key` -> 派发 -> 归约 -> `DVA-CLICKS + 1` -> 点击钩子、ESC 关闭、
      退格 / 可打印字符改查询串），`dva-focus` 加「命中到面板 -> 释放输入 / 编辑器焦点」
      支路，主循环 `t == 4` 改成 面板 -> 输入 -> 编辑器 -> 全局快捷键，每帧建完视图后
      `DVA-PAL-FOCUS[0] = dv-pal-open-key(S)` 并在面板开着时释放输入 / 编辑器焦点；
      `tests/dva-palette.no` 34 项断言全绿（标记 / 调和 / 派发归约 / 端到端四组）；
      `tests/main-dv.no` 树下方接 `palette` 按钮（key=83 + 快捷键 `p`）+
      `dv-command-palette(DEMO, 99, D-PAL-OPEN, D-PAL-Q, D-PAL-OFF, D-PAL-CMD, 8,
      DEMO-PAL-TEXTS, DEMO-PAL-ACTS, 2000, page)`（8 条命令、命令码 101..108、
      行 key 基数 2000），新增 `D-PAL-OPEN = 40` / `D-PAL-OFF = 41` /
      `D-PAL-CMD = 42`（`with-len(44)`）与字符串槽 `D-PAL-Q = 7`
      （`dv-bind-t-ensure(8)`），页头与 `bye` 加 `pal=`，reset 多写「关面板 + 清命令码
      + 清查询串 + `dv-pal-reset`」，显式命令分支在 `D-PAL-CMD` 被写时补 toast，
      首帧 `ops` 124 -> 125（多的 1 个是触发按钮）、`bye … pal=0`、`exit 0`
      （见 §5.26）。
- [x] 阶段 33：命令式树 — `src/components.no` 补 `cmp-tree-*` 与 `ct-*` 工具
      （`ct-get` / `ct-set` / `ct-add` / `ct-sub` / `ct-inc` / `ct-mul`）+ 模型池
      `TR-PAR` / `TR-FIRST` / `TR-NEXT` / `TR-OPEN` / `TR-ROW-NODE` / `TR-ARROW` /
      `TR-TSTART` / `TR-TCOUNT`（随 `cmp-alloc` 逐槽 push，句柄即下标）+
      `cmp-tree-add`（first-child / next-sibling 链 + 按树分段追加进 `TR-ALL`）+
      `cmp-tree-flat`（**显式栈的迭代式先序**，把展开态作用到模型上，兄弟单向链表
      先正序收进临时数组再倒着压栈）+ `cmp-tree-row-head` / `-row-body` /
      `-row`（一行 = `L-H` 的 `C-FORM` + 右对齐「缩进 + 箭头」`C-LABEL` + 文本
      `C-LABEL`，选中态三处一起变）+ `cmp-tree-sync`（**两段式**重建：先挂全部
      行壳再逐行填内容 —— `CHILD-IDX` 是全局扁平数组，一个父的孩子必须连续挂载）+
      `cmp-tree-selected` / `-select`（选中存 `CMP-VALUE[树]`，-1 = 没选中）+
      `cmp-tree-row-of` / `-node-of` / `-arrow-of`（`cs-hit` 返回最深节点，沿
      `CMP-PARENT` 上溯到行）+ `cmp-tree-open-all` / `-depth` / `-kids` /
      `-has-kids` / `-toggle`；`cmp-tree()` 的 `value` 由 0 改成 -1；
      `tests/cmp-tree.no` 36 项断言全绿（构造 / 模型 / 拍平 / 两段式重建 / 选中 /
      二次 sync / 命中上溯 / 两树隔离）；`tests/main-tree.no` 窗口 demo
      （19 节点全展开 45600 > 视口 39600 所以滚轮可用，点行选中 / 点箭头展开 /
      ↑↓ 移动 / ←→ 收起展开 / Enter 切换 / `e` `c` 全展开全收起 / `r` 重置），
      900 帧 `bye frames=900 clicks=0`、exit 0，另用一次性探针在真实布局坐标下
      验证 4 行 + 箭头槽的 `cs-hit` 全部上溯回行（`checks=5 bad=0`，见 §5.27）。
- [x] 阶段 34：命令式代码编辑器 — `src/components.no` 补 `ed-lex-quote` /
      `ed-lex-line`（关键字 / 字符串 / 注释 / 数字 / 标识符 / 空白六类着色，
      相邻同色合并）+ `cmp-editor()`（根就是 `C-SCROLL`，`value2` = 滚轮步长 3200，
      `pad` / `gap` 写字面量 1200 / 800）+ 文本模型池 `ED-LINES` / `ED-KEEP` /
      `ED-L` / `ED-C` / `ED-SEEN` / `ED-ROW` + `cmp-editor-clamp` / `-set-text` /
      `-text` / `-lines` / `-line` / `-set-line` / `-line-of` / `-col-of` /
      `-set-cur` / `-del-line` / `-ins-line` / `-insert` / `-backspace` /
      `-enter` / `-move` / `-key` + `cmp-win-range`（与声明式 `dv-win-range`
      同一套窗口公式，结果写进 `[]i64` 出参）+ `cmp-editor-follow`（光标没动
      就不重排窗口，`ED-SEEN` 记上次跟随时的光标行）+ `cmp-editor-off` /
      `-set-off` + `cmp-editor-row-head` / `-row-body` / `-row`（行 = `L-H` 的
      `C-FORM` + 右对齐行号 `C-LABEL` + 逐段着色 token `C-LABEL`，光标段 2px）+
      `cmp-editor-row-of`（沿 `CMP-PARENT` 上溯到 `ED-ROW > 0`）+
      `cmp-editor-sync`（**两段式**重建可见行 + 声明内容高 / 起始行号）；
      顺带把三个编辑器函数的形参 `ok` 改名 `okc` —— `ok` 撞上 option 模式的
      `ok -> it` 绑定会误编译（返回值对、子链只挂一半、随后段错误，见 §5.28）；
      `tests/cmp-editor.no` 65 项断言全绿（构造 / 文本模型 / 夹紧 / 编辑原语 /
      移动 / 按键 / 窗口 / 跟随 / 两段式重建 / 着色 / 多编辑器隔离十一组）；
      `tests/main-editor.no` 窗口 demo（左 `cmp-editor()` + 右状态面板，
      打字 / 退格 / 回车 / Tab / 方向键 / 点行 / 滚轮 / `r` 重置 / ESC `q` 退出），
      900 帧 `bye frames=900 keys=0 clicks=0 rows=7` + `editor render OK`、exit 0。
      坑：`cmp-editor-sync` 曾在函数里拿到 `undef` 句柄（阶段 34 的库侧绕法，
      2026-11-22 随编译器修复作废，见 §5.28）。
- [x] 阶段 35：命令式虚拟表格 — `src/components.no` 补 `cmp-table()`（根是普通
      `C-TABLE` 容器，`value` = 列数 / `value2` = 行数）+ 全局数据模型
      `TB-HEAD` / `TB-COLW` / `TB-CELL` / `TB-KEY` / `TB-ORDER` + 随 `cmp-alloc`
      逐槽长的句柄状态池 `TB-OFF` / `TB-SEL` / `TB-HEAD-H` / `TB-SCR-H` /
      `TB-ROW-SRC` / `TB-CELL-SRC` / `TB-CELL-COL` + `tb-arr-set`（`[]i64` 下标写，
      长不到就补 0）+ `cmp-table-set-head` / `-head` / `-set-col-w` / `-col-w` /
      `-set-cell` / `-cell` / `-set-key` / `-key` / `-cols` / `-rows` /
      `-reset-order` / `-sort`（按 `TB-KEY` 对 `TB-ORDER` 做稳定插入排序）/
      `-select` / `-selected` / `-scroll` / `-off` / `-set-off` /
      `-row-head` / `-row-body` / `-row-of` / `-cell-of` / `-cell-rc`（沿
      `cmp-parent-of` 上溯到根 `h`）+ `tb-clear-marks`（摘旧滚动体前清掉旧标记）+
      `cmp-table-sync`（表头 `C-GRID` + 滚动体 `C-SCROLL` + 可见行，声明内容高
      `m*rowh + (m-1)*800 + 2*1200`）；`tests/cmp-table.no` 57 项断言全绿
      （构造 / 模型 / 键 / 两段式重建 / 选中 / 命中上溯 / 偏移 / 两表隔离 /
      排序 / 稳定性 / 降序 / 空表 / 重定义表头十三组）；
      `tests/main-table.no` 窗口 demo（3 列 20 行，左表格 + 右状态面板；
      点行选中 / 点单元格报源行:列 / `1` `2` `0` 升序降序源序 / 滚轮 /
      `r` 重置 / ESC `q` 退出），900 帧
      `bye frames=900 keys=0 clicks=0 rows=11 sel=-1` + `table render OK`、exit 0。
      坑两条，见 §5.29：`CHILD-IDX` 要求**一个父的所有直接子先连续挂完再填子树**
      （表头标签插进了表头和滚动体之间）；`;` 是**行注释**不是语句分隔符，
      分支体里 `{ a; b; c }` 会把 `}` 一起注释掉、整段错版死循环。
- [x] 阶段 36：命令式可拆面板（Dock）— `src/components.no` 补 `DK-SPLIT` /
      `DK-SPLIT-V`（分栏位置真源，随 `cmp-alloc` 逐槽长）/ `DK-GRAB` 三个池 +
      `ct-clamp`（`hi <= 0` = 不夹上限，与 `dv-clamp` 一致）+
      `cmp-dock-split` / `cmp-dock-split-v`（把间隙声明成拖杆命中区，`minw` /
      `maxw` 写进 `value` / `value2`，位置初始化成 `minw`）+
      `cmp-dock-splittable` / `cmp-dock-split-val` / `cmp-dock-set-split` /
      `cmp-dock-grab` / `cmp-dock-up`；`src/cmp-scene.no` 补 `cs-dock-down`
      （命中 -> 间隙判定 -> 抓住，返回 1 让调用方跳过点击路径）+
      `cs-dock-move`（光标换算成左屏宽 / 上屏高，夹紧到 `value` / `value2`，
      写回 `DK-SPLIT-V`）；**不新增节点种类** —— 复用 `cs-hit-inner` 的自身
      矩形兜底，间隙自己就是拖杆。`tests/cmp-dock.no` 58 项断言全绿
      （标记 / 夹紧 / 布局 / 命中 / 拖拽 / 竖拆 / 双 dock 隔离 / 普通 dock 不抓
      八组）；`tests/main-dock.no` 窗口 demo（左 explorer + 右 detail 两块，
      拖缝改分栏，点行选中，`r` 重置），900 帧
      `bye frames=900 split=12000 sel=-1 clicks=0 moved=0` + `dock render OK`、
      exit 0，两次运行逐字节一致。坑见 §5.30：`value` 是夹紧下界不能写；
      `print` 字段必须先绑定；嵌套分支的 `->` else 臂要包 `{ ... }`；
      断言变量别用两字符短名。
- [x] 阶段 37：命令式命令面板 — `src/components.no` 补 `CP-TEXT` / `CP-ACT`
      （命令表，应用写）+ 随 `cmp-alloc` 逐槽长的 `CP-Q`（每面板查询串）/
      `CP-CUR`（游标 = 命中表下标，-1 = 无）/ `CP-OFF`（滚动偏移真源）/
      `CP-SB`（命中表在 `CP-SRC` 里的基址）+ 只增的 `CP-SRC`（命中源行号扁平
      抄本）/ `CP-INP-H` / `CP-SCR-H`（输入框 / 滚动体句柄，-1 = 没有）/
      `CP-ROW-SRC`（行句柄 -> 源行号，-1 = 不是面板行）八个池 +
      `cmp-command-count` / `-add` / `-query` / `-set-query` / `-type` /
      `-backspace` / `-off` / `-set-off` / `-scroll` / `-input` /
      `cmp-command-sync`（根就是 `C-COMMAND`：输入框 + 滚动体第一次建好就复用，
      命中表抄一份扁平副本，**全摘**旧行 -> 两段式重挂 -> 填内容；声明内容高
      `m*rowh + (m-1)*800 + 2*1200`）+ `cmp-command-cur` / `-set-cur` /
      `-move`（夹紧在命中区间内）/ `-cur-src` / `-run`（取 `CP-ACT[源行号]`）/
      `-row-of`（命中顺父链找行壳）/ `-row-src`；**不新增节点种类** —— 面板
      完全由应用调出来的真组件拼（行是 `C-FORM` + `C-LABEL`），渲染层只走通用
      容器路径，模态标记由应用自己 `cmp-flag-set(panel, F-OVERLAY)`。
      `tests/cmp-command.no` 90+ 项断言全绿（构造 / 命令表 / 查询编辑 / 两段式
      重建结构 / 命中上溯 / 游标与执行 / 导航夹紧 / 过滤 / 空命中 / 滚动 /
      重复 sync 不累积 / 双面板隔离）；`tests/main-command.no` 窗口 demo
      （12 条命令的模态面板 + 主界面 5 个状态标签，打字过滤 / ↑↓ / 回车执行 /
      退格 / 点行执行 / 点面板外关闭 / `p` 开 / ESC `q` 退出），900 帧
      `bye frames=900 rows=8 keys=0 clicks=0 runs=0` + `command render OK`、
      exit 0。坑见 §5.31：`CHILD-IDX` 连续槽位（摘一半再挂会错位，行必须全摘）；
      同一标识符带两种类型会误编译；`**` 在循环外是干净退出；
      `ct-get(arr, -1)` 不是 -1；`cs-hit` 对 overlay 点外返回 `-2`。
- [x] 阶段 38：命令式输入框富文本 / 语法高亮 — `src/components.no` 补
      `INP-CAR`（光标字节下标）/ `INP-SY`（高亮开关）/ `INP-SGO` / `INP-SGN`
      （每句柄段基址 + 段数）+ 只增的 `INP-SG` / `INP-SGC`（扁平段文本 / 颜色）
      五个池（前四个随 `cmp-alloc` 逐槽长）+ `input-caret` / `-set-caret` /
      `-move` / `-insert`（光标处插入）/ `-delete`（向前删，`input-backspace`
      改删光标前一个）/ `-syntax` / `-syntax-mode` / `-lex-build` / `-lex-count` /
      `-lex-seg` / `-lex-color`；`input-append` 保持旧语义（追加到末尾并把光标
      推过去），`input-set` / `input-clear` 同步重置光标。`src/cmp-scene.no` 补
      `cs-paint-input`（纯文本 / 高亮两条路径）+ `cs-paint-input-lex`（逐段上色，
      光标落段边界时拆前缀 / 光标 / 后缀），`C-INPUT` 从 `cs-paint-field` 改走它；
      **不新增节点种类**，富文本全靠多条 Scene text 指令。`tests/cmp-input.no`
      26 项断言全绿（编辑 13 / 词法 8 / 渲染 5）；`tests/main-input.no` 窗口 demo
      900 帧 `value=let x = 42 ; hi caret=15 lex=1 tokens=5` + `input render OK`、
      exit 0。坑见 §5.33：嵌套 guard 里的 `->` 兜底臂要再包一层裸 `{ }`
      （症状是模块静默不导出，`no fmt -d` 才看得见）；光标统一用字节下标。
- [x] 编译器迁移：safe-index downgrade 后的索引/算术 option 解包已铺到库与全部维护中测试。
- [x] 收尾：5 个早期遗留草稿（`kernel` / `t2` / `t3` / `test-kernel` / `test-l1`）
      全部转绿 —— `src/entity.no` 9 个小写全局改大写 + 三处 option 两段式解包 +
      `entity-emit` 的 while/guard 误写改 `->`，`tests/test-kernel.no` 三处
      while/guard 误写改 `->`，`tests/test-l1.no` 的 i64 字面量改带小数点。
      顺带修掉 `src/dv.no` 与 `src/components.no` 的跨模块同名全局 `TR-OPEN`
      （真遮蔽、随 `#` 顺序变），dv 侧改名 `TR-ARROW-OPEN` / `TR-ARROW-SHUT`。
      全量回归 **PASS=45 FAIL=0**，`tests/logic` 11 项全绿。坑见 §5.32。

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
  阶段 23：右栏顶部加一条滚动列表 —— `dsc = dv-scroll(DEMO, 55, D-SCROLL,
  DEMO-SCROLL-STEP, rc)` + `dv-size(dsc, 0, DEMO-SCROLL-VIEW)`（视口 160px）+
  `dv-repeat-items(dsc, 93, 10, C-LABEL, DEMO-SCROLL, ...)`（10 行，内容约
  336px，`cmp-scroll-max == 17600`）；偏移的真源是 `D-SCROLL` 槽，布局只夹紧
  不写回，滚轮在装配层 `dva-wheel` 里顺父链找到容器、夹紧后写回同一个槽；
  页头回显 `scrl=`，reset 多写 `D-SCROLL = 0`；`dv-app-click` 的 CHANGE 分支
  收紧为只认 `C-SLIDER`（滚动的 `DV-A-CHANGE` 走 `dva-wheel`）；
  首帧因此再多建 11 个节点（滚动容器 + 10 行）；
  阶段 24：右栏那条列表换成 20 行 · 视口 160px 的**虚拟列表** ——
  `dsc = dv-list-view(DEMO, 55, D-SCROLL, DEMO-SCROLL-STEP, DEMO-SCROLL-VIEW,
  DEMO-SCROLL-ROWH, DEMO-SCROLL-N, DEMO-SCROLL, DEMO-SCROLL-VALS,
  DEMO-SCROLL-VALS, DEMO-SCROLL-VALS, 93, rc)`（内容高 736px，任意时刻只挂
  可见的 5 行，行 key 用绝对行号所以窗口滑动复用同一批组件）；同时修掉
  `cs-hit` 对滚动容器多换算一次 `py + off` 的命中 bug；首帧因此从 64 降到
  59（容器 + 5 行，比阶段 23 全挂 10 行还少）；
  阶段 25：页头加搜索框 —— `sinp = dv-input(13, 'search list', page)` +
  `dv-bind(sinp, D-QUERY, 0)`（新增字符串槽 `D-QUERY = 1`，
  `dv-bind-t-ensure(2)`），右栏那条列表换成
  `dsc = dv-filter-list(DEMO, 55, D-SCROLL, DEMO-SCROLL-STEP, DEMO-SCROLL-VIEW,
  DEMO-SCROLL-ROWH, DEMO-SCROLL-N, DEMO-SCROLL, DEMO-SCROLL-VALS,
  DEMO-SCROLL-VALS, DEMO-SCROLL-VALS, D-QUERY, 93, rc)` —— 列表内容成了查询
  串的纯函数（`dv-filter-map` 每帧算命中行号表，行 key 用源行号），页头回显
  `match=N`（`nm = dv-filter-count(...)`），reset 多写
  `dv-bound-set(D-QUERY, '')`；首帧因此从 59 升到 60（多一个输入框）；
  阶段 26：右栏那条列表换成 `dv-order-list`（mode 0 源序 / 1 键升 / 2 键降 /
  3 分组 / 4 分组降，键故意乱序 `(i*7)%20`、组号 `i/10`），上方加「order」
  下拉 `dv-select-from(DEMO, rc, 56, D-SORT, D-SOPEN2, SORT-OPTS, 'order', 57)`
  （`dv-select-from` 第二次出场），新增 `D-SORT = 18` / `D-SOPEN2 = 19`
  与 `SORT-OPTS`，页头回显 `sort=`，reset 多写两个槽；搜索框与 `match=` 回显
  撤掉（过滤与排序是两根轴，demo 一次只演一根），`D-QUERY` 槽保留，
  首帧因此从 60 升到 65；
  阶段 27：左栏原本是普通 `C-FORM`，装不下新增的四个字段 —— 改成
  `lc = dv-scroll(DEMO, 91, D-LSCROLL, 400, cols)` + `dv-size(lc, 30000, 0)`
  的滚动容器（复用阶段 23 原语，视口 228px / 内容 100000），上面挂
  `vf = dv-form(310, lc)` + 4 × `dv-field(200/210/220/230, 400/410/420/430,
  D-VN/D-VA/D-VC/D-VF, ...)`（规则表 `V-KINDS`/`V-PARS`/`V-SPARS`/`V-MSGS`，
  `V-PARS[7] = D-VN` —— `EQ` 比的是另一个字符串槽，0 号槽是页头那个
  `type here` 输入框）+ 状态标签 + 主色提交按钮（`DV-A-CLICK` +
  `DEMO-CMD-SUBMIT`）；提交是显式命令（和 `reset` 同套路，归约器表达不了
  「一次做完两件事」）：`bad > 0` → 写 `D-VSHOW = 1`（揭示错误行），
  `bad == 0` → `D-VSUB + 1` + `D-TOAST = dv-frame()`；新增
  `D-VSHOW = 20` / `D-VSUB = 21` / `D-LSCROLL = 22`（`with-len(24)`）与字符串槽
  `D-VN/D-VA/D-VC/D-VF = 2..5`（`dv-bind-t-ensure(6)`），页头回显
  `vsub=` / `lsc=`（后者顺带提示左栏能滚了），reset 多写三个槽 + 清空四个字符串；
  首帧因此从 65 升到 76（1 表单容器 + 4 标签 + 4 输入 + 1 状态标签 + 1 提交按钮
  + 1 滚动容器 − 原左栏表单容器）；
  阶段 28：右栏那条 20 行列表升级成两列表格（表头 `name` / `key` 不滚，容器里
  只挂可见的 3 行，每行 = 1 个 `C-GRID` + 2 个 `C-LABEL`），顺序链抽成
  `dv-demo-order-plan(S)` 纯函数（mode 槽 → `dv-sort-map` → `dv-group-plan` →
  混合行表），「order」下拉原样驱动五档模式，分组模式下多出来的组标题变成
  通栏行；列宽逐列声明（100px / 96px，表宽 = 24 + 100 + 96 + 8 = 228px），
  左栏让宽（`dv-size(lc, 30000, 0)` → `22000`，右栏内容宽 252px）、右栏视口
  160px → 112px（给表头 36px 与「order」下拉 40px 让位 —— 表头若让 `C-GRID`
  自动量高是 48px，会把下拉顶出 420px 的窗口）；新增 `DEMO-TABLE-*` 数据
  （列宽 / 表头 / 40 个行主序单元格 / 动作 / 载荷）与 `dv-demo-fill-table()`，
  页头回显不变，reset 不变；首帧因此从 76 升到 83（表头 1 + 表头单元格 2 +
  滚动容器 1 + 可见 3 行 × 3 节点 = 13，原阶段 26 的 1 容器 + 可见 5 行 = 6）；
  阶段 29：两栏行从 `dv-row` 换成 `dv-dock-split(DEMO-DOCK-KEY, D-DOCK,
  DEMO-DOCK-MIN, DEMO-DOCK-MAX, DEMO-DOCK-GAP, page)`（`C-DOCK` 横向容器，
  拖杆命中区 = `gap` 800，左屏最小 12000 / 最大 42000 / 初值 22000），
  左栏 `dv-scroll` 的宽改成读状态槽 `D-DOCK`（真源），右栏 `dv-fill` 吃剩余，
  新增 `D-DOCK = 23`（`with-len(24)`）与 `DEMO-DOCK-*` 常量，状态标签加
  `dock={dk}`、bye 加 `dock=`，reset 不变；节点数不变（`dv-row` 与
  `dv-dock-split` 都只建 1 个节点），首帧 `ops` 仍 83，`gap` 从 1600 收到 800
  等于给右栏让出 8px；
  阶段 30：左栏底部加声明式代码编辑器 —— 先 `dv-label(88, 'editor', lc)`，再
  `dv-code-editor(DEMO, DEMO-ED-KEY, D-ED-TEXT, D-ED-OFF, DEMO-ED-STEP,
  DEMO-ED-VIEW, DEMO-ED-ROW, DEMO-ED-GUTTER, DEMO-ED-KEY0, lc)` + `dv-size(ed, dk,
  DEMO-ED-VIEW)`（一次写全宽高；分两次调 `dv-size` 会把高覆盖成 0），初始文本
  `DEMO-ED-CODE = 'let name = "noui"\n; a comment\nlet n = 42\nlet ok = 1'`
  （关键字 / 字符串 / 注释 / 数字四样着色都齐），行高 2400 / 视口 14400 /
  行号槽 4800 / 滚轮步长 3200，行 key 基数 9700（行 key = 9700 + 行号*10000000，
  与表格的 9000/900/93 段不撞）；`DEMO` 池 24 -> 25（`D-ED-OFF = 24`）、
  `dv-bind-t-ensure(6)` -> 7（`D-ED-TEXT = 6`），页头回显加 `edoff={edo}`、
  `bye` 加 `edoff={edor}`，reset 多写 `D-ED-OFF = 0` + `dv-bound-set(D-ED-TEXT,
  DEMO-ED-CODE)`；首帧因此从 83 升到 105（1 个标题标签 + 编辑器 21 个节点：
  滚动容器 + 内容容器 + 4 行，其中光标所在的第 0 行把 token `let` 拆成
  「空前缀（不挂）+ 2px 光标 + `let`」所以比别的行多 2 个 —— 顺带实测确认
  `dv-run-push` 的相邻同色合并让 `'let name = "noui"'` 只有 3 段而不是 7 段）。
  900 帧打印 `bye frames=900 ops=105 clicks=0 rows=2 sel=0 pick=0 tst=0 scrl=0 sort=0 vsub=0 lsc=0 dock=22000 edoff=0`（首帧建 105 节点，之后零结构操作），
  `exit 0`，连跑两次输出一致；视图声明的动作数据（`tests/dv.no` 场景 K 覆盖语义）：
  「add row」`key=16 act=DV-A-DELTA pay=D-LIST value=1 value2=3`、
  勾选框 `key=9 act=DV-A-TOGGLE pay=D-AGREE`。
  阶段 31：左栏顶部加声明式树 —— 先 `dv-label(89, 'tree', lc)`，再
  `dv-tree-view(DEMO, 980, D-TOFF, 3200, 16800, 2400, 13, DEMO-TREE-TEXTS,
  DEMO-TREE-K0, DEMO-TREE-KN, DEMO-TREE-PAR, D-TOPEN, D-TSEL, 9800, lc)` +
  `dv-size(tr, dk, 16800)`（行高 2400 / 视口 16800 / 行距 3200 / 行 key 基数 9800，
  行 key = 9800 + 节点号*10000000），下方挂 `expand all`（`value=1`）/
  `collapse all`（`value=0`）两个幽灵按钮，载荷都是 `D-TOPEN`，走新动作码
  `DV-A-FILL`；13 个节点就是 noui 自己的目录（`noui` / `src` / `tests` / `docs`
  + 各级文件），初始根与 `src` 展开 → 可见 8 行；`DEMO` 池 25 -> 40
  （`D-TOPEN = 25` / `D-TSEL = 38` / `D-TOFF = 39`），新增 `DEMO-TREE-N/KEY/
  KEY0/ROW/VIEW/STEP` 与 `DEMO-TREE-TEXTS`（**`with-len(13)` + 逐项写**，
  模块级 `[]str` 字面量在函数体里读出来是空的，见 §5.25）/ `-K0` / `-KN` /
  `-PAR`（`[]i64` 字面量不受影响），页头回显加 `tsel={tsl}` / `toff={tof}`、
  `bye` 加 `tsel={tslr}` / `toff={tofr}`，reset 多写「根 + src 展开、其余收起」
  + `D-TSEL = -1` + `D-TOFF = 0`，启动处显式写 `DEMO[D-TOPEN] = 1` /
  `DEMO[D-TOPEN+1] = 1` / `DEMO[D-TSEL] = -1`（demo 不走 reset 命令，初始态
  得自己写全，否则 `D-TSEL` 读到 zero-init 的 0，看着像选中了第 0 行）；
  首帧因此从 105 升到 124（1 个标题标签 + 树 8 行 × 3 + 滚动容器 + 2 个按钮
  = +19）。900 帧打印 `bye frames=900 ops=124 clicks=0 rows=2 sel=0 pick=0 tst=0
  scrl=0 sort=0 vsub=0 lsc=0 dock=22000 edoff=0 tsel=-1 toff=0`（之后零结构
  操作），`exit 0`，连跑两次输出逐字节一致。
  阶段 32：树下方加声明式命令面板 —— 先一个 `palette` 幽灵按钮
  （`dv-node(C-BUTTON, 83, 'palette', '', 0, 1, 0, dv-on(), lc)` + `dv-variant` 主色 +
  `dv-act(DV-A-SET, D-PAL-OPEN)` + `dv-hotkey(112, DV-A-SET, D-PAL-OPEN)`，112 = p），
  再 `dv-command-palette(DEMO, DEMO-PAL-KEY, D-PAL-OPEN, D-PAL-Q, D-PAL-OFF,
  D-PAL-CMD, DEMO-PAL-N, DEMO-PAL-TEXTS, DEMO-PAL-ACTS, DEMO-PAL-KEY0, page)`
  （父 = 根表单，所以是居中模态；8 条命令 = `new file` / `open file` / `save` /
  `save as` / `close tab` / `find in files` / `toggle terminal` / `quit`，
  命令码 `DEMO-PAL-ACTS = [101..108]`；行高 2800 / 视口 28000 / 步长 3200 /
  行 key 基数 2000，行 key = 2000 + 源行号*100）；`DEMO` 池 40 -> 44
  （`D-PAL-OPEN = 40` / `D-PAL-OFF = 41` / `D-PAL-CMD = 42`）、`dv-bind-t-ensure(7)`
  -> 8（`D-PAL-Q = 7`），新增 `DEMO-PAL-N/KEY/KEY0/TEXTS/ACTS`，文案数组同样
  `with-len(8)` + 逐项写（见 §5.26 / §5.25 的模块级 `[]str` 坑）；页头回显加
  `pal={pc}`、`bye` 加 `pal={pcr}`，reset 多写「`D-PAL-OPEN = 0` + `D-PAL-CMD = 0`
  + `dv-bound-set(D-PAL-Q, '')` + `dv-pal-reset(DEMO-PAL-KEY)`」，显式命令分支在
  `D-PAL-CMD` 被写时补 `D-TOAST = dv_frame()`（归约器表达不了「顺带弹个通知」）；
  首帧因此从 124 升到 125 —— 多的那 1 个是 `palette` 触发按钮（面板关着时只登记
  key、不建子树，见 §5.26；曾误判为 `dv-arr-set` 或 `dv-bind-t-ensure(8)` 的影响，
  两组对照实验都证否了）。900 帧打印 `bye frames=900 ops=125 clicks=0 rows=2 sel=0
  pick=0 tst=0 scrl=0 sort=0 vsub=0 lsc=0 dock=22000 edoff=0 tsel=-1 toff=0 pal=0`，
  `exit 0`；另做两组实验确认面板真的建得出来 —— 强制 `D-PAL-OPEN = 1` 跑 900 帧得
  `ops = 144`（+19 = 面板根 + 输入 + 滚动 + 8 行 × 2），去掉 `dv-command-palette`
  调用则回到 125，连跑两次除窗口句柄外逐字节一致。
- `tests/main-input.no`：**阶段38 命令式输入框富文本 / 语法高亮窗口端到端**
  （`src/app.no` + `src/cmp-scene.no` + `src/components.no` 的 `INP-*` / `input-*`），
  已验证。运行：`../bin/no build -cc ./ccwrap.sh -o dist/main-input tests/main-input.no`
  两个字段同一段样例 `'let x = 42 ; hi'`（一个开高亮、一个纯文本），点击聚焦 / 失焦，
  打字 / 退格 / 前删 / ←→ 移光标（都走 `input-insert` / `input-backspace` /
  `input-delete` / `input-move`），`p` 切换高亮、`r` 重置、`q` / ESC 退出；
  900 帧 `bye frames=900 keys=0 clicks=0 value=let x = 42 ; hi caret=15 lex=1 tokens=5`
  + `input render OK`，`exit 0`。
- `tests/main-textarea.no`：**阶段39 命令式多行文本域窗口端到端**
  （`src/app.no` + `src/cmp-scene.no` + `src/components.no` 的 `TA-OFF` / `ta-*`），
  已验证。运行：`../bin/no build -cc ./ccwrap.sh -o dist/main-textarea tests/main-textarea.no`
  一个大字段（48000 × 16000 = 视口 6 行）+ 状态标签，点击聚焦 / 失焦，打字 / 回车换行 /
  退格 / 前删 / ↑↓←→（上下走 `ta-move-up` / `ta-move-down`），`r` 重置、`q` / ESC 退出；
  900 帧 `bye frames=900 keys=0 clicks=0 value=let x = 1¶let y = 2¶; done caret=26
  line=2 col=6 off=0 lines=3` + `textarea render OK`，`exit 0`。

## 8. 验收目标

- [x] `no test` 覆盖 L1–L3 纯逻辑（实体注册、布局计算、scene dump）— `tests/logic/l1-l3.no`，
      11 项断言全绿；该目录用独立 `package.noml`（无 link-libs）避开 shim 链接，
      直接 `cd noui && ../bin/no test tests/logic` 即可（实测 `ALL TESTS PASSED`；
      从 `tests/logic` 里跑 `no test .` 会报 `tests/ directory not found`）。
- [x] 端到端示例：一个窗口显示按钮，点击后计数器 +1，窗口标题/内容随之变化，ESC 退出。
- [x] 全部构建走 `no build -cc wrapper`，运行带 `DYLD_LIBRARY_PATH`。
- [x] 组件目录 86 模块全部有命令式落点，且**没有「只有 API + 最小状态」的核心交互
      组件**（这句只覆盖有明确交互语义的那批；全目录仍有约 40 个 kind 只有构造函数，
      其中 12 个叶子 kind 还落在通用 `rect + text` 兜底绘制里，清单见
      `src/components-catalog.md` 覆盖统计末尾）：
      阶段 33–37 补树 / 编辑器 / 虚拟表格 / Dock / 命令面板，阶段 38 补
      `C-INPUT` 的富文本 / 语法高亮（`INP-*` + `cs-paint-input`，见 §5.33），
      阶段 39 补 `C-TEXTAREA` 的多行值 / 行几何 / 视口（`TA-OFF` + `ta-*` +
      `cs-paint-textarea`，见 §5.34），阶段 40 补 `C-NUMBER-INPUT` / `C-STEPPER` /
      `C-SEARCH` 的值域 / 步长 / 吸附 / `[−]` `[+]` / 清除（`CMP-VALUE4` + `num-*` +
      `search-*` / `stepper-hit` + `cs-paint-number-input` / `-stepper` / `-search`，
      见 §5.35），阶段 41 补 `C-OTP-INPUT` 的逐槽数字 / 活动槽 / 退格 / 灌入
      （`OT-CODE` / `OT-CUR` + `otp-*` + `cs-paint-otp`，槽数沿用 `CMP-VALUE`，
      见 §5.36），阶段 42 补 `C-COMBOBOX` 的查询过滤 / 匹配表 / 高亮 / 选中
      （`CB-MBASE` / `CB-MN` / `CB-MLIST` / `CB-MCUR` + `combobox-*` +
      `cs-paint-combobox` / `cs-hit-child`，查询串沿用 `CMP-TEXT` + `INP-CAR`，
      选项沿用 `CHILD-IDX`，见 §5.37），阶段 43 补 `C-RATING` / `C-PAGINATION` /
      `C-EMPTY` 的读数 / 写值（夹紧）/ 命中与各自渲染（`rating-*` / `pagination-*` +
      `slider-min` / `slider-max` + `cs-paint-rating` / `-pagination` / `-empty`，
      槽位沿用 `VALUE` / `VALUE2` / `VALUE3`，见 §5.38），阶段 44 补 `C-MESSAGE` /
      `C-ALERT` / `C-NOTIFICATION` 的 level 读写 / 关闭钮 / 剩余时间条与各自渲染
      （`LV-*` + `lv-slot` + `msg-level` / `msg-set-level` + `alert-*` + `ntf-*` +
      `cs-level-color` / `cs-level-glyph` + `cs-paint-message` / `-alert` /
      `-notification`，槽位沿用 `VALUE` / `VALUE2` / `VALUE3`，见 §5.39），阶段 45
      补 `C-BUBBLE` / `C-SPEECH` 的 position / lang 读写与各自渲染（`bubble-*` /
      `speech-*` + `cs-paint-bubble` / `-speech` + `cs-bubble-tail-x`，槽位沿用
      `VALUE` / `TEXT` / `TEXT2`，见 §5.40）——目录里「multiline value」
      「value, min, max, step」「query, clear」「digits, slots」「query, options,
      selected」「value, max」「page, total」「title, description」「level」
      「level, closable」「level, timeout」「text, position」「text, lang」这几句
      承诺此前都是空的。
- [x] 每个命令式复合组件都有「无窗口断言测试 + 窗口 demo」双落点：
      `tests/cmp-{tree,editor,table,dock,command,input,textarea,number,otp,combobox,rating,alert,bubble}.no` + 
      `tests/main-{tree,editor,table,dock,command,input,textarea,number,otp,combobox,rating,alert,bubble}.no`。
