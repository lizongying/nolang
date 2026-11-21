# noui — 用 Nolang 重写 GPUI 精神内核

本目录（仓库根 `noui/`）是「用 Nolang 重写 GPUI」的**可运行纵切面**交付物。
2026-11-20 从 `tmp/noui/` 迁入仓库根目录，纳入版本管理。

## 已交付

- **两个可运行版本**：
  - `main.no`：原始纵切面（顶层主循环 + 标量全局）
  - `tests/main-layered.no`：**分层内核版**（Entity / Element / Scene 大写全局池 + 函数封装 + Scene 遍历绘制），窗口跑通
- **组件 + 场景 + 窗口端到端**：
  - `tests/main-app.no`：组件树（`src/components.no`）→ `cs-layout` / `cs-paint`（`src/cmp-scene.no`）→ Scene 指令池 → SDL 绘制，窗口跑通
  - 交互：mousemove 命中测试置 hover、点击 `-`/`+` 改计数并联动 progress、勾选 checkbox / switch、ESC 退出
- **应用装配层（阶段9）**：
  - `src/app.no`：窗口 + 组件树 + 场景 + 事件泵的装配底座（`app-init` / `app-frame` / `app-draw-scene` / `app-poll` / `app-hover`）
  - `tests/app-assemble.no`：装配层纯逻辑 29 项断言（尺寸 / 坐标 / 颜色 / 底色 / hover / 布局 / Scene）
  - `tests/main-form.no`：**窗口端到端表单**——输入框键盘输入、下拉展开选中、滑杆拖拽、勾选 / 开关、提交弹窗，全部经 `src/app.no` 装配
- **声明式视图层（阶段10）**：
  - `src/dv.no`：**声明式写法**——`UI = f(state)`。每帧用 `dv-*` 构造器把状态描述成一棵纯数据视图树，
    再 `dv-render` 按 `(key, kind)` 调和进命令式组件池：同 key 同 kind 的组件对象跨帧复用
    （输入框文本 / 焦点不丢），只对变化部分做 props 更新，新增 / 删除 / 重排走最小操作
  - `tests/dv.no`：声明式层纯逻辑 65 项断言（新建 / 复用 / 改稿 / 删除 / 重排 / 嵌套 / 条件 / flags / 动作 / 重复 / 事件回路 / 83 kind 全可构造 / 命名构造器 / 状态归约器）
  - `tests/dv-layout.no`：**声明式布局纯逻辑 41 项断言**（提示同步 / 横向 / gap-pad 覆盖 / 主轴 grow / 交叉轴对齐 / C-GRID 回归 / 真实两栏骨架 / 固定尺寸 / 文本对齐）
  - `tests/main-dv.no`：**窗口端到端声明式 demo**——事件经 `dv-dispatch` 取视图自带动作只改状态，
    视图每帧重建（含 `dv-repeat-items` 列表、两栏布局与组件目录速写），900 帧仅 40 次结构操作
  - 与命令式层的边界：dv **不替代** `components.no` / `cmp-scene.no`，只加一层「视图描述 → 组件池」的映射
- **声明式应用装配层（阶段11）**：
  - `src/dv-app.no`：`dva-run(S, title, max-frames)` 一行接管整个主循环——轮询 / hover /
    命中 / 焦点交接 / 键盘喂输入框 / 按名调三个回调 / 重建视图 / `dv-render` 调和 /
    `cs-layout` / `cs-paint` / 上屏 / 帧数上限收尾。应用只按名提供
    `dv-app-view()` / `dv-app-reduce(S, k)` / `dv-app-click(S, k, act, pay, nw)`
    （语言没有可用的 fn 值，只能按名约定）
  - `tests/dva.no`：装配层纯逻辑 18 项断言（输入类 kind 判定 / 焦点交接 / 键盘喂字 / 计数器）
  - `tests/main-dv.no` 的主循环从 ~70 行命令式降到「状态初值 + `dva-run(...)` + 打印」三行，
    输出逐字节不变（`bye frames=900 ops=40 clicks=0 rows=2`）
- **声明式样式的落点（阶段12）**：
  - `src/cmp-scene.no`：`CS-TH` 九槽主题色板 + `cs-theme` / `cs-theme-set` / `cs-theme-reset`，
    绘制路径所有分支改读主题色（默认值 = 旧常量，**不写主题时渲染逐位不变**）
  - `src/components.no` + `src/dv.no`：`CMP-VAR` 变体池 / `dv-variant` / `dv-var-primary|secondary|ghost|danger`，
    把「主色 / 次要 / 幽灵 / 危险」写成视图数据，绘制时映射到主题语义色
  - `tests/dv-style.no`：主题 / 变体 / 绘制层实际发色的纯逻辑 29 项断言
  - `tests/main-dv.no` 应用了一套偏紫深色主题 + 6 处变体，结构不变（仍是 `ops=40`）
- **声明式受控输入与快捷键（阶段13）**：
  - `src/dv.no`：`DV-BIND` / `DV-BIND-MAX` / `DV-BIND-T` 池 + `dv-bind` / `dv-bind-of` /
    `dv-bind-max-of` / `dv-bound` / `dv-bound-set` / `dv-bind-t-ensure` / `dv-node-count`；
    快捷键 `DV-HOT-*` 池 + `dv-hotkey` / `dv-hot-dispatch`
  - `src/dv-app.no`：`dva-sync-binds(root)` 每帧双向同步（shadow `DVA-BIND-LAST` 分辨
    「谁改了谁」），`dva-run` 键盘分支改成「有焦点喂字 / 无焦点先试快捷键」；签名不变
  - `tests/dva-bind.no`：绑定 / 快捷键 / 双向同步纯逻辑 18 项断言
  - `tests/main-dv.no`：输入框绑到 `DV-BIND-T[0]` 并在页头回显，`r` 键重置（含清文本），
    结构不变（仍是 `ops=40`）
- **声明式列表项（阶段14）**：
  - `src/dv.no`：`dv-repeat-items(parent, key0, n, kind, texts, values, acts, pays)`
    把「文案 / value / 动作 / 载荷」四个平行数组映射成 n 个同 kind 节点
    （短数组补默认，n 超长按长度、负数按 0）；`dv-rows-del(texts, n, i)` 删第 i 项返回新长度；
    动作码加 `DV-A-ITEM`（列表项被点，载荷 = 行号）
  - `tests/dva-repeat.no`：构造 / 钳制 / 事件回路 / 删除差量 / 数组删除纯逻辑 23 项断言
  - `tests/main-dv.no`：三行列表每行挂 `(DV-A-ITEM, 行号)`，点任意一行删掉它，
    结构不变（仍是 `ops=40`）
- **声明式模态弹层（阶段15）**：
  - `src/components.no`：新 flag 位 `F-OVERLAY`（512）；`src/dv.no` 新视图位 `DV-F-OVL`（64）
    与 `dv-overlay(node)` / `dv-on-overlay()`（DV-F-OVL 经 `dv-apply-flags` 落到活节点）
  - `src/cmp-scene.no`：`cs-is-overlay` / `cs-child-flow`（流布局视图，跳过叠加层）；
    `cs-measure` / `cs-layout` 不把叠加层算进流，布局末尾改为在父矩形内居中；
    `cs-paint` 叠加层最后画；`cs-hit` 叠加层先命中且**点不穿**（返回 `-2` 向上传播）
  - `tests/dva-overlay.no`：标记 / 测量 / 居中 / 命中阻隔 / 绘制顺序纯逻辑 17 项断言
  - `tests/main-dv.no`：`dialog` 按钮把 `D-DIALOG` 写 1，视图里随之出现一个居中的模态
    对话框（ok / cancel 写回 0），页头回显 `dlg=`；新增一个按钮后 `ops` 38 → 39
- **声明式锚定浮层（阶段16）**：
  - `src/components.no`：新活池 `CMP-ANCHOR`（锚点 key，-1 = 无）+ `cmp-anchor` /
    `cmp-set-anchor`；`src/dv.no` 新视图池 `DV-ANCHOR` + `dv-anchor-of` /
    `dv-overlay-anchor(node, anchor-key)`（= `dv-overlay` + 记下锚点 key，调和时经
    `dv-apply-props` 落到活节点）
  - `src/cmp-scene.no`：`cs-find-key(k)` 按声明式 key 线性反查活组件句柄；
    `cs-layout` 的叠加层分支改成**锚定优先**：把浮层放在锚点矩形下方（`x` 对齐锚点、
    `y = 锚点底 + CS-GAP`），下方放不下就翻到锚点上方，最后两个轴各夹紧在父矩形内；
    锚点 key 找不到（或没挂锚点）则保持原来的父矩形内居中
  - `tests/dva-anchor.no`：标记 / 调和 / 下方优先 / 贴边翻转 + 夹紧 / 命中阻隔 /
    绘制顺序纯逻辑 18 项断言
  - `tests/main-dv.no`：`popover` 按钮写 `D-POP=1`，视图里出现锚在触发按钮下的菜单
    （`close` 写回 0），页头回显 `pop=`；新增一个按钮后 `ops` 39 → 40
- **声明式 hover + 非模态叠加层（阶段17）**：`dv-hover(node, slot)` 让 hover 也进状态 ——
  装配层每帧按最近一次布局的命中 key 归约（`dva-sync-hover`），视图据此决定是否建 tooltip。
  叠加层因此分两档：**模态**（`dv-on-overlay()`，对话框 / 菜单，点不穿）与
  **非模态**（`dv-on-tip()` = `VIS|EN|PASS|OVL`，tooltip / hover-card / 通知）：
  非模态仍不参与流布局、仍最后画、自身仍可被命中，但没接住点击就继续找下面的兄弟
  （`cs-hit` 不再竖 `-2` 屏障），所以点得穿。落点：
  - `src/components.no`：新位 `F-PASS`（1024）；`src/dv.no`：新视图位 `DV-F-PASS`（128）、
    新池 `DV-HOVER`（视图节点 → 状态槽 + 1）、`dv-hover` / `dv-hover-of` / `dv-pass` /
    `dv-on-tip`，`dv-apply-flags` 覆盖式同步 `F-PASS`
  - `src/dv-app.no`：`dva-sync-hover(S)`（每帧把 hover 写进状态，值没变不写）+
    `DVA-HIT` / `DVA-MOUSE` 缓存；`src/cmp-scene.no`：`cs-hit` 分档处理叠加层
  - `tests/dva-hover.no`：标记 / 调和（含「第二帧降级要清掉 F-PASS」）/ 状态同步 /
    命中穿透 / 绘制顺序 / hover→tooltip 端到端 23 项断言
  - `tests/main-dv.no`：鼠标停在 `dialog` 按钮上时它下面浮出说明文字（`tip=` 回显）；
    没 hover 时那个节点根本不存在，所以结构操作数不变（仍 `ops=40`）
- **声明式禁用态（阶段18）**：禁用就是 flags 少一个 `DV-F-EN` —— `dv-disabled()` = `VIS`
  （保留可见），`dv-enabled()` = `VIS|EN`，`dv-disabled-of(node)` 读回。
  视图每帧读状态重建，所以「行数到上限 → `add row` 禁用」这类派生不需要额外的状态槽；
  与 `dv-cond` 的分工是「不存在」对「在原地但点不动」。绘制层早就消费 `F-DISABLED`
  （按钮 / 开关 / 输入框分支画成灰色），阶段 18 补的是**行为语义**：
  `dv-dispatch` 对禁用节点不出动作、`dv-hot-dispatch` 目标禁用不触发、
  `dva-focus` 不把焦点交给禁用节点；但**命中仍有效** —— hover / tooltip 照常，
  禁用原因往往就要靠 tooltip 解释。落点：`dv-disabled` / `dv-enabled` /
  `dv-disabled-of` + 三处派发闸门 + `tests/dva-disabled.no`（23 项断言，
  含「按状态派生 flags」的视图写法本身）
- **声明式标签页（阶段19）**：`dv-tabs(parent, active, slot, labels, base)` 一次建出
  「`C-TAB` 标题条 + n 个标题 + 内容容器」并返回内容容器。当前页就是状态槽
  （视图每帧读出来当 `active`），点第 i 个标题 = `SET slot = i` —— 复用现有归约，
  主循环里没有一行 key 的 if-else。选中态用 flags（`dv-on-sel`）表达；调和**末尾**
  再把 `active` 同步到 `C-TAB` 自己的 `CMP-VALUE`，因为 `cs-paint-tab` 画高亮读的是
  `tab-active(h)`。页面内容按 `active` 条件挂进返回的容器 —— 切页 = 换子树，
  容器同 key 复用。落点：`DV-TAB-ACT` 池 + `dv-tabs` / `dv-tab-active-of` +
  `dv-sync` 末尾的 `tab-select` 同步 + `tests/dva-tabs.no`（27 项断言，含真实
  `cs-hit` → `cmp-key` → `dv-dispatch` 命中路径）；demo 右栏底部一条三页标签，
  首帧 `ops` 40 → 47（标题条 + 3 标题 + 容器 + 2 个页面内容节点）
- **声明式多选列表（阶段20）**：`dv-repeat-items` 原来把每项的 flags 硬编码成
  `dv-on()`，所以「哪些行选中 / 哪些行禁用」没法由状态推导 —— 声明式多选缺地基。
  补 `dv-repeat-items-f(parent, key0, n, kind, texts, values, acts, pays, flags)`
  （多一个 flags 平行数组，短数组 / 空数组补 `dv-on()`）与
  `dv-sel-flags(S, slot0, n)`（状态槽非 0 → `dv-on-sel()`，否则 `dv-on()`）。
  于是多选列表 = `dv-repeat-items-f(..., dv-sel-flags(DEMO, D-SEL0, 3))`，每项挂
  `(DV-A-TOGGLE, 自己的槽)` —— 点一项翻自己的槽，下一帧视图读状态重建 flags，
  活树选中态跟着走（`C-RADIO` 画 `F-SELECTED` 时用 `CS-TH-OK`，选中真的看得见）。
  全选 / 清空是「一次写 N 个槽」，归约器表达不了 → 走 `DV-A-CLICK` + 显式命令 id
  （和 reset 同一套路），主循环里仍然没有一行 if-else。落点：`DV-EMPTY-F` +
  `dv-repeat-items-f` / `dv-sel-flags` + `tests/dva-sel.no`（28 项断言）；
  demo 右栏顶部一条三行多选 + `all` / `none` 两个幽灵按钮，页头回显 `sel=N`，
  首帧 `ops` 47 → 52（3 个列表项 + 2 个按钮）
- **声明式下拉（阶段21）**：命令式用法要 `cmp-select` + 逐个 `select-add-option` +
  `select-open` / `select-close` + `select-choose` 四步，而声明式层只要
  `dv-select-from(S, parent, key, selslot, openslot, labels, ph, base)` 一行 ——
  一次建出「`C-SELECT` 字段 + n 个常驻选项」：字段挂 `DV-A-TOGGLE(openslot)`
  （点字段 = 展开 / 收起），每个选项挂新动作码 `DV-A-CHOOSE(selslot)`。
  `dv-apply` 的 CHOOSE 分支一次写两个槽（选中下标 + 清展开槽），因为单槽的
  `SET` 表达不了「选中并收起」；选项的 `value` = 下标、`value2` = 展开槽正好
  是操作数。字段文案 = `labels[S[selslot]]`（未选 / 越界 = 空 → 显示占位符），
  所以「显示什么」也是状态的函数。选项常驻（不条件挂载），收起时靠 `cs-hit` 的
  `C-SELECT` 分支不可命中 —— 展开 / 收起零结构操作。落点：`DV-A-CHOOSE`(10) +
  `dv-apply` CHOOSE 分支 + `dv-select-from` + `tests/dva-select.no`（32 项断言，
  含真实 `cs-hit` → `cmp-key` → `dv-dispatch` → `dv-apply` → 重建的全链路、
  以及「收起后同一坐标不再命中选项」）；demo 左栏下拉换成这一行（原来是手写
  `C-SELECT` + 3 个选项 + `DV-A-OPEN` / `DV-A-SET`），右栏单选组的文案也改成从
  `DEMO-OPTS` 数组里读（`dv-demo-opt(i)`），和左栏共用同一份选项数据；
  首帧 `ops` 仍是 52（同样 4 个节点）
- **声明式通知（阶段22）**：前面的状态都是「应用写了什么就是什么」，通知不一样 ——
  它自己会过期。新增 `dv-toast-alive(S, slot, ttl)`（槽里存的是**触发时的帧号**，
  视图每帧拿 `dv-frame()` 和「触发帧 + ttl」比，过期即死；0 = 没有通知，帧计数从
  1 起不会假活）和 `dv-toast(S, parent, key, slot, text, ttl, base)`（活着才建整棵
  子树，`dv-cond` 的思路）：`C-POPOVER` + `dv-on-tip()`（非模态叠加层，仍最后画、
  自身可命中，但点得穿）+ 一个文案 + 一个关闭按钮（`DV-A-SET` 把触发帧写回 0 =
  立即过期）。触发按钮本身也只是视图数据 —— `value = dv-frame()` +
  `DV-A-SET(触发槽)`，所以「何时触发」同样是 `UI = f(state)`，主循环一行没改；
  过期不需要定时器也不需要事件，下一帧视图重建时 `dv-toast-alive` 自己说「没了」，
  `dv-sync` 把那棵子树摘掉（只算被摘掉的直接子节点 = 1 次操作）。落点：
  `dv-toast-alive` / `dv-toast` + `tests/dva-toast.no`（31 项断言，含触发按钮接线、
  真实 `cs-hit` → 点关闭 → 重建消失、ttl 边界逐帧推进、绘制层文案随存活出现 /
  消失）；demo 左栏加 `notify` 按钮，点一下「saved」在窗口中间停一会儿再自己消失，
  首帧 `ops` 52 → 53（多一个触发按钮）
- 一个真正能开的原生窗口（SDL3，底层 Metal/Vulkan），显示计数器 UI：
  - 标题 + 动态计数值 + 两个按钮（`-` / `+`）
  - 鼠标 hover 按钮高亮、点击改数值、ESC 退出
- 平台层 C shim（`src/shim/shim.c`）：把 SDL3 的 C API 摊平成标量函数供 Nolang `#{c}` 调用
- 交互逻辑单元测试（`tests/interaction.no`）：10 项断言全绿
- **全目录组件库**（`src/components.no`）：gpui-kit（原 gpui-component）86 模块的 Nolang API 层，
  83 个组件 kind 全可构造，核心 10 个有完整交互状态机（见下）
- 架构文档（`ARCHITECTURE.md`）：GPUI 概念映射 + 实测约束 + 分步计划

## 组件库（gpui-kit 全目录）

`src/components.no` 按 gpui-kit 的 86 个源码模块建全 Nolang API：83 个唯一组件 kind，
统一 SoA 大写全局池 + 构造函数 + 通用 setter/getter；核心 10 个
（button / input / checkbox / switch / select / tab / dialog / tooltip / progress / slider）
有完整状态机（hover/click/focus/勾选/值域/键盘式切换）。清单与映射见 `src/components-catalog.md`。

```sh
# 全目录 smoke：83 个组件逐一构造 + setter 断言
../bin/no run -cc ./ccwrap.sh tests/components-smoke.no   # ALL COMPONENT SMOKE TESTS PASSED
# 核心 10 组件深度交互
../bin/no run -cc ./ccwrap.sh tests/components-core.no     # ALL CORE COMPONENT TESTS PASSED
```

> 本轮是「全目录 API + 核心扎实」档：DataTable 虚拟滚动、Dock 可拆面板、代码编辑器等
> 功能对等是长期项（当前只做 API + 最小状态）。实现中踩到的两个编译器坑记在
> `ARCHITECTURE.md §5.2`。

## 组件渲染层（阶段8：`src/cmp-scene.no`）

把组件树摊平成后端无关的 Scene 指令池，补齐「组件 → 布局 → 绘制 → 命中」闭环：

- `cs-layout`：自顶向下递归布局。容器纵向堆叠、`C-GRID` 横向排列；子元素拉伸到内容宽（`w - 2*CS-PAD`），叶高由 `cs-measure` 决定
- `cs-measure`：自底向上测算。纵向取最大宽 / 求和高，横向求和宽 / 取最大高，带 `CS-PAD` 内边距与 `CS-GAP` 间距
- `cs-paint` / `cs-paint-leaf`：容器发 rect 后递归；叶按 kind 发射 rect / fill / text，hover 按钮变 `CS-HOVER`
- `cs-hit`：后序遍历返回最深层命中句柄（无命中 -1）
- 坐标用定点 i64（1px = 100），算术统一走 `cs-add` / `cs-sub` / `cs-mul` / `cs-div` 解包 `?i64`

纯逻辑测试（40 项断言，无窗口）：
```sh
../bin/no run -cc ./ccwrap.sh tests/cmp-scene.no     # ALL CMP-SCENE TESTS PASSED
```

阶段8 起补齐了复合组件的渲染闭环：`cs-is-container` 区分「真容器」与「子项存在 CHILD-IDX 的
复合组件」（select / tab），`cs-measure` / `cs-layout` / `cs-paint` / `cs-hit` 对 select 下拉、
tab 条、dialog 面板、input 焦点描边、tooltip、skeleton / shimmer / spinner 各有专门分支；
收起的 select 不参与布局与命中，避免残留边界被误点。

窗口端到端（组件树 → 布局 → Scene → SDL）：
```sh
export DYLD_LIBRARY_PATH=/opt/homebrew/opt/sdl3/lib:/opt/homebrew/opt/sdl3_ttf/lib
../bin/no build -cc ./ccwrap.sh -o dist/main-app tests/main-app.no && ./dist/main-app
```
启动打印 `window handle: ...`，窗口持续存活；点击 `-`/`+` 改计数并联动 progress，ESC 退出打印 `bye`。

## 应用装配层（阶段9：`src/app.no`）

把「窗口 / 组件树 / 场景 / 事件」四件事收成一个可复用底座，入口文件不再需要自己写 FFI 与绘制循环：

- `app-init(title)`：SDL 初始化 + 建窗 + 加载字体 + 建根组件（0 成功，-1 失败）
- `app-frame()`：`begin_frame(底色)` → `app-draw-scene`（Scene 指令逐条送 SDL）→ `end_frame`
- `app-poll()`：取一个事件，解码成 `(t, x, y)`（t=0 无 / 1 退出 / 2 左键 / 3 移动 / 4 按键）
- `app-hover(root, px, py)`：命中测试 + F-HOVERED 高亮切换
- `app-quit()` / `app-should-quit()`：退出事件标志 / ESC 请求
- `app-set-bg` / `app-bg` / `app-win-w()·h()·wf()·hf()` / `app-delay` / `app-shutdown`
- `fx(px)`：像素 f64 → 定点 i64；`col-r/g/b(c)`：颜色通道

纯逻辑测试（29 项断言，无窗口）：
```sh
../bin/no run -cc ./ccwrap.sh tests/app-assemble.no   # ALL APP-ASSEMBLE TESTS PASSED
```

窗口端到端表单（装配层 + 组件状态机全链路）：
```sh
export DYLD_LIBRARY_PATH=/opt/homebrew/opt/sdl3/lib:/opt/homebrew/opt/sdl3_ttf/lib
../bin/no build -cc ./ccwrap.sh -o dist/main-form tests/main-form.no && ./dist/main-form
```
窗口内可点输入框聚焦并键盘输入（字母 / 数字 / 空格 / 退格 / 回车提交）、点开下拉选一项、
拖滑杆、勾选 checkbox / 开关 switch、点提交弹确认框、重置清空；ESC / `q` / 关窗退出，
默认 900 帧后自动收尾打印 `bye`（`FORM-MAX-FRAMES = 0` 可取消上限）。

## 声明式视图层（阶段10：`src/dv.no`）

把「命令式装配」与「声明式写法」合起来：**在既有命令式内核之上加一层声明式视图层**，
不推翻 `components.no` / `cmp-scene.no`。核心公式 `UI = f(state)`：

```nolang
; 事件：命中 -> key -> 视图自带的 (act, payload) -> 归约到状态
hit = cs-hit(root, cxf, cyf)
act, pay = dv-dispatch(cmp-key(hit))
nw = dv-apply(DEMO, cmp-key(hit))               ; 视图说「点什么」，状态池就是归约目标
(nw > 0) -> { dv-demo-bump() }
(pay == DEMO-CMD-RESET) -> { ... }              ; 归约器表达不了的才留显式命令

; 每帧：状态 -> 纯数据视图树（长什么样 + 点了意味着什么 + 怎么排，都是数据）
dv-clear()
page = dv-form(1, -1)
dv-label(2, 'noui declarative', page)
dv-input(3, 'type here', page)
cols = dv-row(90, page)                       ; 两栏：布局提示也是视图数据
dv-lay(cols, dv-lay-h-grow(), 1600, 0)
lc = dv-fill(91, cols)
rc = dv-fill(92, cols)
sel = dv-node(C-SELECT, 4, st, 'pick', 0, ch, 0, dv-on(), lc)   ; st/ch 都是状态的函数
dv-act(sel, DV-A-OPEN, 0)                       ; 点开 / 收起
o0 = dv-node(C-LABEL, 5, DEMO-OPT-A, '', 0, 0, 0, dv-on(), sel)
dv-act(o0, DV-A-SET, D-CHOICE)                  ; 选中第 0 项 = S[D-CHOICE] = 0
sld = dv-node(C-SLIDER, 8, '', '', 0, ct, 100, dv-on(), lc)
dv-act(sld, DV-A-CHANGE, 0)                     ; 拖动
chk = dv-node(C-CHECKBOX, 9, 'agree', '', 0, 0, 0, fa, lc)
dv-act(chk, DV-A-TOGGLE, D-AGREE)               ; S[D-AGREE] = 1 - S[D-AGREE]
extra = dv-cond(13, sh, lc)                     ; 条件节点：sh=0 时整棵子树不进视图
(extra >= 0) -> { dv-label(14, 'extra line', extra) }
dv-repeat-labels-n(lc, 80, DEMO-ROWS, nl)       ; 重复行：行数由状态决定
dv-icon(20, 'star', rc)                         ; 右栏：组件目录速写

; 视图 -> 活组件树（按 (key, kind) 调和）
ops = dv-render(root, page)
```

调和器 `dv-sync` 五遍：① 按 `(key, kind)` 认领活组件 → ② 删除未认领的 →
③ 为未匹配的视图节点新建 → ④ 同步 props 并递归 → ⑤ 重排成视图顺序。
组件对象跨帧复用，因此输入框文本 / 焦点不丢；结构变化只发最小增删。

- 视图节点是 SoA 纯数据（`DV-KIND` / `DV-KEY` / `DV-TEXT` / `DV-FLAGS` / `DV-PARENT` /
  `DV-ACT` / `DV-ACT2` …），无闭包、无回调 —— Nolang 的 fn 值不可用（全局 fn 值 codegen 失败、
  局部 fn 值崩溃），所以声明式层走**数据驱动**路线
- **行为也是数据**：`dv-act(node, act, payload)` 把「点了意味着什么」挂在视图节点上，
  `dv-dispatch(key)` 一次取回 `(act, payload)`；应用层不再写 `key` 的 if-else 链。
  注意 `dv-act` 与 `dv-node` 同生命周期：`dv-clear()` 会清空动作池，
  **每帧重建视图时都要重新挂一次**
- **点击即归约（`dv-apply`）**：三类归约动作把「点击」写成对状态池的赋值 ——
  `DV-A-SET`（`S[payload] = value`）、`DV-A-TOGGLE`（`S[payload] = 1 - S[payload]`）、
  `DV-A-DELTA`（`S[payload] = clamp(S[payload] + value, 0, value2)`）。
  于是 `dv-apply(S, key)` 一次调用就是整个点击处理器：返回实际写入的槽数，
  `0` 表示这个节点不是归约动作（应用层可另作处理，例如 `DV-A-CLICK` + 自定义 payload）。
  视图的 `value` / `value2` 在这里是**操作数**而不是显示值，这是它与 `dv-apply-props`
  的分工：props 管「长什么样」，`dv-act` 管「点了算什么」。
  归约器表达不了的（一次写多个槽、需要读组件几何）仍留命令式，例如 demo 的 `reset`
  与滑杆拖动的几何换算
- **重复原语**：`dv-repeat`（同形状 n 份）/ `dv-repeat-labels`（全量）/ `dv-repeat-labels-n`（前 n 份，
  超长按长度、负数按 0）。key 从 `key0` 递增，跨帧稳定 → 列表增删只发差量；
  阶段14 另加 `dv-repeat-items`（文案 / value / 动作 / 载荷四个平行数组 → n 个列表项，
  第 i 项的动作与载荷也按索引取）
- `dv-apply-props` 对 `C-INPUT` / `C-TEXTAREA` **不同步 text**（非受控）：否则每帧会把用户刚敲的字清掉；
  placeholder 走 `text2`
- `dv-on` / `dv-on-chk` / `dv-on-sel` / `dv-on-act` / `dv-on-open` / `dv-on-overlay` 提供 flags 组合；
  叠加层另有一个 `dv-overlay(node)` 事后标记（视图位 `DV-F-OVL`，调和时落到活节点 `F-OVERLAY`），
  阶段16 再加 `dv-overlay-anchor(node, anchor-key)`（叠加层 + 锚点 key，落到活节点 `CMP-ANCHOR`）；
  `dv-live-by-key` 做「视图 key → 活组件句柄」反查（事件命中后要拿组件对象改状态时用）
- 焦点管理不属于动作：输入框的 `input-focus` 由应用层在点击时直接处理

**布局也是数据**：`dv-lay(node, bits, gap, pad)` 把「怎么排」挂到视图节点上，
随 props 一起同步进组件（`CMP-LAY` / `CMP-GAP` / `CMP-PAD`），由 `cmp-scene` 的
`cs-measure` / `cs-layout` 消费。`bits` 复用 `components.no` 的 `L-*` 位，
`gap` / `pad` 为 `0` 表示用 `CS-GAP` / `CS-PAD` 默认。

```nolang
cols = dv-row(90, page)                      ; 横向容器（等价 C-GRID）
dv-lay(cols, dv-lay-h-grow(), 1600, 0)       ; 吃掉主轴剩余 + 兄弟间距 1600
lc = dv-fill(91, cols)                       ; 纵向 + 吃掉行内剩余宽度（左栏）
rc = dv-fill(92, cols)                       ; 右栏
dv-lay(some-node, dv-lay-end(), 0, 0)        ; 交叉轴末端对齐（纵向容器=靠右）
dv-lay(some-node, dv-lay-center(), 0, 0)     ; 交叉轴居中
```

**多大、字怎么对齐也是数据**：`dv-size(node, w, h)` 与 `dv-align(node, al)` 把尺寸 /
对齐提示挂到视图节点上，随 props 同步进组件（`CMP-SW` / `CMP-SH` / `CMP-ALIGN`），
由 `cs-measure` / `cs-layout`（尺寸）与 `cs-paint-leaf`（对齐）消费。
`0` 一律表示「自动 / 按 kind 默认」，所以不写提示的节点行为完全不变。

```nolang
lc = dv-form(91, cols)                       ; 不定 grow 就不会参与宽度分配
dv-size(lc, 30000, 0)                        ; 定宽侧栏：宽 30000，高 0 = 自动
rc = dv-fill(92, cols)                       ; 右栏吃掉剩余宽度
t = dv-label(2, 'noui declarative', page)
dv-align(t, dv-align-center())               ; 标题居中（默认是左对齐）
```

- `dv-size(node, w, h)`：`w` / `h` 为 `0` 时走 `cs-leaf-size` 的 intrinsic 测算；
  非 `0` 直接覆盖。**容器同样生效** —— 「定宽侧栏 / 定高工具条」也是纯数据
- `dv-align(node, al)`：`al` 取 `A-LEFT` / `A-CENTER` / `A-RIGHT`（1/2/3），
  `0` = 按 kind 默认（`label` / `text` / `link` / `icon` 左对齐，
  `button` / `badge` / `tag` / `kbd` / `marker` / `avatar` 居中）；
  组合子 `dv-align-left` / `dv-align-center` / `dv-align-right`
- 落点由 `cs-align-of` + `cs-text-origin-x` 统一计算，`link` 的下划线跟随实际字起点
- 与 `dv-lay` 同生命周期：`dv-clear()` 会清空 `DV-SW` / `DV-SH` / `DV-ALIGN`，
  **每帧重建视图时都要重新挂一次**

- 组合子：`dv-lay-h` / `dv-lay-grow` / `dv-lay-end` / `dv-lay-center` /
  `dv-lay-h-grow` / `dv-lay-h-end` / `dv-lay-grow-end`（位或写在函数里，避开顶层位或缺陷）
- 两个便捷构造器：`dv-row(key, parent)` = `C-GRID`；`dv-fill(key, parent)` = `C-FORM` + `L-GROW`
- **无提示时行为完全不变**：三个池默认 `0`，`cs-is-h` 等价于原来的 `kind == C-GRID` 判断，
  `cs-gap-of` / `cs-pad-of` 回落到 `CS-GAP` / `CS-PAD`，纵向子节点仍保持拉伸宽、
  横向子节点仍保持 intrinsic 高并顶对齐 —— 老测试（`tests/cmp-scene.no` 等）零改动通过
- `dv-lay` 与 `dv-act` 同生命周期：`dv-clear()` 会清空 `DV-LAY` / `DV-GAP` / `DV-PAD`，
  **每帧重建视图时都要重新挂一次**
- 嵌套容器之后，事件侧的 key 不再是 root 的直接子节点，用 `dv-live-deep(root, key)`
  跨层 BFS 回查活组件（显式队列实现，不用递归）

**全组件目录可声明式表达**：`dv-create` 对 83 个 kind 各走自己的 `cmp-*` 构造器，
未列出的 kind 落到 `cmp-alloc` 兜底，因此 `dv-node(kind, key, ...)` 对目录是「全函数」，
新增组件不必改视图层。另有整套命名构造器（`dv-icon` / `dv-kbd` / `dv-avatar` /
`dv-radio` / `dv-search` / `dv-table` / `dv-tree` / `dv-accordion` / `dv-collapsible` /
`dv-tooltip` / `dv-alert` / `dv-pagination` / `dv-rating` / `dv-text-node` …），
把构造参数映射到视图字段：`text`（标题/文案）、`text2`（placeholder）、
`value`（当前值 / 列数 / 格数）、`value2`（上限 / 行数 / 总数 / 满分）。
视图 flags 是独立紧凑编码（`DV-F-VIS/EN/CHK/SEL/ACT/OPEN` = 1/2/4/8/16/32），
由 `dv-apply-flags` 映射到 CMP 的 `F-VISIBLE/ENABLED/CHECKED/SELECTED/ACTIVE/OPEN`。

纯逻辑测试（65 项断言，无窗口）：
```sh
../bin/no run -cc ./ccwrap.sh tests/dv.no     # ALL DV TESTS PASSED
# A 新建/复用/改稿/删除/重排  B 嵌套  C 条件  D select  E flags
# F 动作 + dispatch  G 重复（复用/增长/收缩/限量）  H 事件回路
# I 83 kind 全可构造 + SEL/ACT/OPEN 位 + cmp-alloc 兜底
# J 命名构造器全目录 + VALUE/VALUE2 语义 + flags 组合子
# K 状态归约器 SET/TOGGLE/DELTA（含上下限截断、非归约节点跳过、改稿后视图复用）
```

布局提示纯逻辑测试（41 项断言，无窗口）：
```sh
../bin/no run -cc ./ccwrap.sh tests/dv-layout.no   # ALL DV-LAYOUT TESTS PASSED
# L 提示随 props 同步（bits/gap/pad）+ 只改提示时结构零操作
# M C-FORM + L-H 变横向（对照普通 C-FORM 的纵向换行）
# N gap / pad 覆盖后的坐标
# O L-GROW 吃掉主轴剩余（对照无提示时只拿 intrinsic 高）
# P 交叉轴 L-AE / L-AC（纵向容器靠右 / 居中）
# Q 回归：C-GRID 仍横向、默认 gap / pad 读回 0
# R 真实两栏骨架：页头全宽 + 一行两列等宽 + 子节点不串栏
# S 固定尺寸：叶子 30000x5000 / 容器 20000x4000 / 未提示叶子仍 intrinsic
# T 文本对齐：右 / 左 / 中覆盖 + 默认值复刻旧渲染 + 绘制层真实落点
```

窗口端到端声明式 demo：
```sh
export DYLD_LIBRARY_PATH=/opt/homebrew/opt/sdl3/lib:/opt/homebrew/opt/sdl3_ttf/lib
../bin/no build -cc ./ccwrap.sh -o dist/main-dv tests/main-dv.no && ./dist/main-dv
# bye frames=900 ops=40 clicks=0 rows=2   —— 首帧建 40 个节点，之后 899 帧零结构操作
```
demo 的主循环只有三行：状态初值 + `dva-run(DEMO, title, 900)` + 打印；轮询 / 命中 /
焦点 / 键盘 / 重建 / 调和 / 上屏全在 `dva-run` 里，应用按名提供 `dv-app-view` /
`dv-app-reduce` / `dv-app-click` 三个回调（点击段只剩 `dv-apply` 归约 + 归约命中后的
结构相关动作：命中节点的父节点是 select 时收起弹层，用 `dv-live-deep` 跨两栏回查）。
页面骨架是声明式两栏：页头（居中标题 / 输入框 / 两行状态摘要）全宽，
下面 `dv-row` + `dv-size` 定宽左栏 + `dv-fill` 右栏吃满剩余宽度
（左栏交互控件、右栏组件目录速写）。
demo 里的右栏是一组「目录速写」：单选组的选中项由状态经 `dv-on-sel` 决定，点击是
`DV-A-SET D-CHOICE`（操作数就是各自的 `value`），另有图标 / 徽标 / 标签 / 键帽 /
分隔线 / 进度条 / 分页 / 评分 / 折叠面板 / 提示 / 提醒 / 表格 / 纯文本等命名构造器，
`dv-on-open` / `dv-on-act` 各出现一次。

## 声明式应用装配层（阶段11：`src/dv-app.no`）

阶段 10 把「长什么样 / 点了意味着什么 / 怎么排 / 多大 / 字怎么对齐」都变成了数据，
但主循环仍是每个应用各写一遍的 ~70 行命令式。阶段 11 把这段也收成一个入口：

```nolang
DEMO[D-COUNT] = 40
DEMO[D-LIST] = 2
dva-run(DEMO, 'noui declarative', 900)     ; 整个主循环
```

`dva-run(S, title, max-frames)` 每帧做：`app-poll` → hover 高亮 → 左键命中后
`dva-focus` 交接焦点（输入类组件获得键盘）→ `dv-dispatch` 取视图动作 →
按名调 `dv-app-reduce(S, k)` 归约状态 → 按名调 `dv-app-click(...)` 补组件侧动作 →
按名调 `dv-app-view()` 重建视图 → `dv-render` 调和 → `cs-layout` / `cs-paint` /
`app-frame`。退出条件：`q` / ESC / 关窗 / 帧数上限。

应用必须按名提供三个回调（语言没有可用的 fn 值，只能按名调用）：

```nolang
dv-app-view   = () (page i64) { page = dv-demo-view() }
dv-app-reduce = (S []i64, k i64) (nw i64) { nw = dv-apply(S, k) }
dv-app-click  = (S []i64, k i64, act i64, pay i64, nw i64) () { ... }
```

- `dva-focus` 按 kind 判定输入类组件（`C-INPUT` / `C-TEXTAREA` / `C-SEARCH` /
  `C-NUMBER-INPUT` / `C-OTP-INPUT`），点击时接管焦点、点别处释放；
  `dva-keyboard` 把退格 / 空格 / 数字 / 小写字母喂给当前焦点组件
- 计数器 `dva-frames()` / `dva-ops()` / `dva-clicks()` 由装配层维护，不再占应用状态槽
- 状态池写入只能 place-match（`dva-set`），因为 `S` 是数组参数
- 回调名是编译期契约：漏定义直接编译错误（`tests/dva.no` 用一组哑回调验证）

纯逻辑测试（18 项断言，无窗口）：
```sh
../bin/no run -cc ./ccwrap.sh tests/dva.no     # ALL DVA TESTS PASSED
# U 输入类 kind 判定  V 焦点交接（输入接管 / 标签释放 / 按钮释放 / 显式 -1）
# W 键盘喂字（字母+数字+空格 -> 退格 / 无焦点忽略 / ESC 忽略）  X 计数器
```

## 声明式样式的落点（阶段12：主题色板 + 变体）

阶段 10/11 把结构、行为、布局、尺寸、对齐都变成了数据，只剩「颜色」还散在绘制层的
硬编码常量里。阶段 12 把调色板也变成数据，并让整条绘制路径跟着它走：

```nolang
dv-theme-set(CS-TH-ACCENT, 0x7C5CFFFF)        ; 调色板：9 个槽位，dva-run 之前设一次
dv-variant(btn, dv-var-danger())              ; 变体：主色 / 次要 / 幽灵 / 危险
```

- `src/cmp-scene.no`：`CS-TH` 色板（`CS-TH-BG/FG/ACCENT/FIELD/TRACK/MUTED/TIP/OK/DANGER`）
  + `cs-theme` / `cs-theme-set` / `cs-theme-reset`。`cs-paint` 的所有分支
  （hover 按钮 / 焦点描边 / 输入底色 / 勾选 / 轨道·骨架 / 浮层 / 次要文本）改读主题色。
  **默认值逐一复刻旧常量，不写主题时渲染逐位不变**（`tests/dv.no` / `tests/cmp-scene.no` 保持全绿）
- `src/components.no`：`CMP-VAR` 变体池 + `cmp-var` / `cmp-set-var`
- `src/dv.no`：`dv-variant(node, v)` 把变体挂到视图节点（与 `dv-act` 同生命周期，
  每帧重建视图时重挂，`dv-apply-props` 负责同步进活组件池）；`dv-var-primary/secondary/ghost/danger`
  四个常量构造器；`dv-theme-set` / `dv-theme-reset` 转发到 cmp-scene
- `src/dv-app.no`：`dva-run` 启动时把 `CS-TH-BG` 接到窗口底色——声明式主题连窗口背景也管
- 变体优先级：**变体 > `cmp-color` > 主题 TRACK 默认色**（`cs-var-color`）

纯逻辑测试（29 项断言，无窗口）：
```sh
../bin/no run -cc ./ccwrap.sh tests/dv-style.no   # ALL DV-STYLE TESTS PASSED
# Y 默认值 = 旧常量  Z 设色 / 越界忽略 / 复位  AA 变体 -> 主题语义色（含改主题后跟随）
# AB 真实绘制树发色（hover / 危险按钮 / 次要徽章 / 勾选 / 焦点输入 / 浮层 / 自定色不被变体顶掉）
```

> 命名冲突：`dv-theme(key, parent)` 已被 `C-THEME` 组件构造器占用，
> 所以主题设置器叫 `dv-theme-set`。

## 声明式受控输入与快捷键（阶段13）

阶段 10~12 之后，只剩「输入框里写了什么」还不是状态：组件自己持有文本缓冲，
视图每帧重建也读不到它，reset 也清不掉它。阶段 13 补上两种声明式写法：

```nolang
inp = dv-input(3, 'type here', page)
dv-bind(inp, D-TEXT, 0)                       ; 受控输入：文本 = DV-BIND-T[D-TEXT]
dv-label(18, format('typed={tx}'), page)      ; tx = dv-bound(D-TEXT)，视图读得到
dv-hotkey(btn, 114, DV-A-CLICK, DEMO-CMD-RESET)   ; 快捷键 r = 重置
```

- **受控输入（`dv-bind(node, slot, max)`）**：把输入类组件（`C-INPUT` / `C-TEXTAREA` /
  `C-SEARCH` / `C-NUMBER-INPUT` / `C-OTP-INPUT`）的文本与字符串状态池 `DV-BIND-T[slot]`
  连起来。装配层每帧 `dva-sync-binds(root)` 做一次双向同步：组件文本变了 → 写回状态；
  状态被归约器 / reset 改了 → 推回组件。`max` 是槽位允许的最大字符数（0 = 不限），
  两个方向都截断，并且截断结果会写回状态，两边永远一致。
  视图用 `dv-bound(slot)` 读、归约器用 `dv-bound-set(slot, s)` 写；
  池子用 `dv-bind-t-ensure(n)` 声明槽数。**`dva-run` 签名不变**（字符串池住在 dv 侧，
  像主题一样是全局的，不随 `dv-clear()` 清空）。
  同步在 `dv-render` 之后、`cs-layout` 之前做 —— 文本影响输入框宽度。
- **快捷键（`dv-hotkey(node, keycode, act, pay)`）**：视图声明「按下这个键 = 点了这个节点」，
  命中后走与点击**完全相同**的归约路径（`dv-app-reduce` → `dv-app-click`），
  所以 `dva-clicks()` 也照常计数。语义：**只在没有输入焦点时生效** ——
  否则正在输入框里打 'r' 会把页面重置了。与 `dv-act` 同生命周期，每帧重挂。

纯逻辑测试（18 项断言，无窗口）：
```sh
../bin/no run -cc ./ccwrap.sh tests/dva-bind.no   # ALL DVA-BIND TESTS PASSED
# Y 绑定元数据（挂得上 / 读得回 / 未绑定 -1 / 节点计数）
# Z 快捷键分派（命中 -> act/pay/vkey / 未命中 act=0 / 计数 / 命中后 dv-apply 等价于点击）
# AA 双向同步 9 步：组件->状态 / 敲字->状态 / 幂等 / 状态->组件 / 两侧超长按 max 截断 /
#    非输入类节点被忽略 / input-append 回流 / dv-clear 后绑定消失
```

## 声明式列表项（阶段14）

阶段 10 的 `dv-repeat-labels-n` 只能重复「同一种标签」，`dv-repeat` 只能重复「同一个节点」。
真实列表要的是「第 i 行的文案、动作、载荷都不一样，而且都由状态数组决定」。阶段 14 补上
**平行数组 → 列表项** 的映射：

```nolang
; 四个平行数组：文案 / value / 动作 / 载荷。短的补默认（'' / 0 / 不挂动作 / 0）
dv-repeat-items(lc, 80, nl, C-LABEL, DEMO-ROWS, DEMO-ROW-VALS, DEMO-ROW-ACTS, DEMO-ROW-PAYS)

; 视图侧只声明「第 i 行挂了 (acts[i], pays[i])」，点第 i 行 -> dv-dispatch 取回它
act, pay = dv-dispatch(cmp-key(hit))     ; act = DV-A-ITEM, pay = 行号
```

- **`dv-repeat-items`**：第 i 项的文案取 `texts[i]`、`value` 取 `values[i]`、
  动作取 `acts[i]`（`0` = 不挂）、载荷取 `pays[i]`，key 从 `key0` 递增 —— 跨帧稳定，
  所以列表增删只发差量（删一行 = 改一行文案 + 删一个尾节点）。
  `n` 超出 `texts` 长度按长度算、负数按 0 算，与 `dv-repeat-labels-n` 同语义。
  数组短于 `n` 的部分补默认，所以「只有文案」的列表只要传空数组即可。
- **`DV-A-ITEM`**：新增动作码（9），语义是「列表项被点，载荷 = 行号」。
  之所以不复用 `DV-A-CLICK`，是因为 `reset` 按钮已经占用了
  `DV-A-CLICK` + 自定义 payload —— 同一个 `(act, pay)` 组合会撞车。
- **`dv-rows-del(texts, n, i)`**：删除列表第 i 项（后面的前移），返回新长度；
  越界 / 负数 / `n <= 0` 原样返回 `n`。删除是纯数组操作，应用侧只需比较返回值。

纯逻辑测试（23 项断言，无窗口）：
```sh
../bin/no run -cc ./ccwrap.sh tests/dva-repeat.no   # ALL DVA-REPEAT TESTS PASSED
# P 构造（key 递增 / 逐项取数组 / value / 动作与载荷逐项 / 返回首个 handle / 父与 kind）
# Q 钳制（n 超长按长度 / 短数组补默认 / acts 空数组不挂动作 / n 负返回 -1）
# R 回路（render 后 dv-dispatch 取回 (DV-A-ITEM, 行号) / 组件文案与数组一致）
# S 差量（dv-rows-del 删第 1 行后重建：key 稳定 -> 改一行文案 + 删尾节点）
# T 数组删除（删中间 / 删首部 / 越界与负数原样返回）
```

## 声明式模态弹层（阶段15）

命令式的 `tests/main-form.no` 里，对话框是**另一棵根**：单独 `cmp-dialog()`、单独画、
单独命中，靠 `dialog-is-open` 开关。声明式做法不复制这套——弹层就是**同一棵树上的一个
叠加层节点**，装配层一行不用改：

```nolang
; 开 = 状态槽为 1：视图里存在这个节点；关 = 为 0：节点根本不建（dv-render 会把它移出活树）
(dl != 0) -> {
    dlg = dv-node(C-DIALOG, 60, 'modal', '', 0, 0, 0, dv-on-overlay(), page)
    dv-node(C-LABEL, 61, 'click outside is blocked', '', 0, 0, 0, dv-on(), dlg)
    dok = dv-node(C-BUTTON, 62, 'ok', '', 0, 0, 0, dv-on(), dlg)
    dv-act(dok, DV-A-SET, D-DIALOG)          ; value = 0 -> 写回 0 = 关
    dcan = dv-node(C-BUTTON, 63, 'cancel', '', 0, 0, 0, dv-on(), dlg)
    dv-act(dcan, DV-A-SET, D-DIALOG)
}
```

- **`dv-on-overlay()` / `dv-overlay(node)`**：视图位 `DV-F-OVL`（64），调和时由
  `dv-apply-flags` 落到活节点的 `F-OVERLAY`（512）。**注意视图池（`DV-*`）与活组件池
  （`CMP-*`）是两套下标**，所以标记写 `DV-FLAGS` 而不是 `cmp-flag-set`（后者写活池，
  视图句柄会被误解成组件下标）。
- **`cs-measure` / `cs-layout`**：叠加层不参与父级的流布局（`cs-child-flow` 返回
  「第 j 个参与流布局的子节点」，叠加层被跳过），布局末尾改为**在父矩形内居中**
  （先夹到父尺寸，再两个轴各取一半剩余空间）。
- **`cs-paint`**：先画流子节点，叠加层最后画 —— 永远盖在最上面。
- **`cs-hit`**：叠加层先命中；命中则直接返回；**一个都没命中则返回 `-2` 向上传播**
  （`-2` = 被模态挡住），且不再做自身矩形兜底 —— 点空白不会掉到下面的内容。
  装配层无需特判：`cmp-key(-2)` = `-1`，`dv-dispatch(-1)` = 无动作。
- **开关语义**：因为「关」= 节点不在视图里，所以 `dv-on-overlay()` 里除了
  `DV-F-OVL` 还带上 `DV-F-OPEN`，让 `C-DIALOG` 的绘制分支认识它。

纯逻辑测试（17 项断言，无窗口）：
```sh
../bin/no run -cc ./ccwrap.sh tests/dva-overlay.no   # ALL DVA-OVERLAY TESTS PASSED
# U 标记（视图位 -> 活节点 F-OVERLAY / F-OPEN；普通节点不受影响；dv-on-overlay 位组合）
# V 测量（有 / 无叠加层的容器同尺寸；cs-child-flow 看不到叠加层）
# W 布局（叠加层在父矩形内居中；流子节点位置逐位不变）
# X 命中（弹层内最深节点 / 弹层面板 / 弹层外返回 -2 / 无弹层时照常命中）
# Y 绘制（叠加层场景下标更靠后 = 盖在最上面；关闭后活树里没有它）
```

## 声明式锚定浮层（阶段16）

阶段 15 的叠加层只会「在父矩形内居中」，真实 UI 里的菜单 / popover / tooltip 需要
**锚在触发节点旁边**。阶段 16 给叠加层加一个可选的锚点：视图层多带一个 key，
布局层按 key 反查锚点活节点的矩形来定位。

```nolang
; 视图：popover 按钮（key=70）+ 锚在它下面的菜单
b70 = dv-node(C-BUTTON, 70, 'popover', '', 0, 1, 0, dv-on(), lc)
dv-act(b70, DV-A-SET, D-POP)
(pp != 0) -> {
    pop = dv-node(C-POPOVER, 71, 'menu', '', 0, 0, 0, dv-on-overlay(), page)
    dv-overlay-anchor(pop, 70)               ; 叠加层 + 锚点 key = 70
    dv-node(C-LABEL, 72, 'item one', '', 0, 0, 0, dv-on(), pop)
    pclose = dv-node(C-BUTTON, 74, 'close', '', 0, 0, 0, dv-on(), pop)
    dv-act(pclose, DV-A-SET, D-POP)
}
```

- **`dv-overlay-anchor(node, anchor-key)`**：`dv-overlay` + 把锚点 key 写进 `DV-ANCHOR`；
  调和时 `dv-apply-props` 把它落到活节点的 `CMP-ANCHOR`（-1 = 无锚点，每帧无条件同步）。
- **`cs-find-key(k)`**：线性扫 `CMP-KEY` 反查活组件句柄（视图层只带 key，矩形只在活池里）。
- **`cs-layout`**：叠加层分支先算「居中」兜底，再在有锚点且锚点能找到时改成锚定 ——
  `x = 锚点 x`，`y = 锚点底 + CS-GAP`；下方超出父矩形就翻到 `锚点顶 - 浮层高 - CS-GAP`；
  最后两个轴各夹紧在父矩形内。流子节点仍然逐位不变。
- **命中 / 绘制**：与阶段 15 完全一致（先命中、点不穿返回 `-2`；最后画）。
  也就是说锚定浮层同样是**模态**的 —— 点空白不会落到下面的内容，关浮层要靠浮层里的按钮。
- **锚点找不到**（例如触发节点还没进活树）：保持居中，不会出现 0,0 之类的退化位置。

纯逻辑测试（18 项断言，无窗口）：
```sh
../bin/no run -cc ./ccwrap.sh tests/dva-anchor.no   # ALL DVA-ANCHOR TESTS PASSED
# Z 标记（dv-overlay-anchor 加 DV-F-OVL + DV-ANCHOR；默认 -1；dv-clear 清掉）
# Y 调和（活节点带 F-OVERLAY + CMP-ANCHOR；无锚点叠加层是 -1；cs-find-key 正反查）
# L 布局（下方优先 x 对齐 / y = 锚点底 + CS-GAP；下方放不下 -> 翻转到父矩形上沿；夹紧）
# H 命中（浮层内最深节点；浮层外返回 -2；同构无浮层树照常命中）
# P 绘制（锚定浮层场景下标更靠后 = 盖在最上面）
```

## 声明式 hover + 非模态叠加层（阶段17）

阶段 15/16 的叠加层是**模态**的：居中 / 锚定、命中时先于其它子节点、绘制最后画，
点不穿（`cs-hit` 返回 `-2` 向上传播）。这一档对对话框 / 菜单是对的，但 tooltip /
hover-card / 通知需要相反的行为 —— 它们挡在内容上面，却**不该吃掉点击**。

阶段 17 把叠加层分成两档，并让 hover 也变成状态：

```nolang
DEMO []i64 = with-len(10)
D-TIP = 9
...
b19 = dv-node(C-BUTTON, 19, 'dialog', '', 0, 1, 0, dv-on(), lc)
dv-act(b19, DV-A-SET, D-DIALOG)
dv-hover(b19, D-TIP)                     ; 鼠标停在我身上 -> 装配层把 D-TIP 写成 1
...
tp = DEMO[D-TIP]: { ok -> it \n\n -> 0 }
(tp != 0) -> {
    tip = dv-node(C-LABEL, 75, 'modal: click outside is blocked', '', 0, 0, 0, dv-on-tip(), page)
    dv-overlay-anchor(tip, 19)           ; 复用阶段 16：锚在触发节点下沿
}
```

- `dv-hover(node, slot)`：只记「本节点关心哪个状态槽」（视图池 `DV-HOVER`，存 `slot+1`，
  0 = 未声明）。装配层 `dva-sync-hover(S)` 每帧按**最近一次布局**算出的命中 key
  （`DVA-HIT`，在 `cs-layout` 之后重算，所以布局变了也准）把 `1`/`0` 写进那个槽 ——
  写之前比对当前值，没变就不写（写状态 = 通知应用状态变了）。
  必须在建视图**之前**跑，否则 tooltip 慢一帧。
- `dv-on-tip()` = `VIS|EN|PASS|OVL`；`dv-pass(node)` 只加 `DV-F-PASS`。
  调和时 `dv-apply-flags` **覆盖式**同步（第二帧降级成普通叠加层会把 `F-PASS` 清掉）。
- `cs-hit` 分档：非模态叠加层照常参与命中，但不竖 `-2` 屏障；它没接住点击时，
  继续走流子节点那趟（点得穿），也不挡父矩形兜底。
- 命中缓存 `DVA-HIT` 只在收到过鼠标事件（`DVA-MOUSE`）后才算，
  否则开局会在 `(0,0)` 误报一次 hover。

纯逻辑测试（23 项断言，无窗口）：
```sh
../bin/no run -cc ./ccwrap.sh tests/dva-hover.no     # ALL DVA-HOVER TESTS PASSED
# Z 标记（dv-hover 只动 DV-HOVER；dv-hover-of 读回；默认 -1；dv-pass / dv-on-tip）
# Y 调和（活节点带 F-OVERLAY + F-PASS；普通叠加层没有；同 key 第二帧降级后 F-PASS 被清）
# S 同步（命中声明节点 -> 1；命中别的 / -1 / -2 -> 0；未声明的槽不动）
# H 命中（浮层自身可命中；点穿到下面的兄弟；对照模态树同点是 -2；对照无浮层树命中盒子）
# P 绘制（非模态浮层场景下标更靠后 = 仍最后画）
# T 端到端（hover 槽 1 -> 视图里出现锚在触发节点上的 tooltip；槽 0 -> 消失）
```

## 声明式禁用态（阶段18）

「禁用」在声明式里不需要新机制 —— 视图每帧读状态重建，所以只要能在建节点时按状态选
flags 就够了。缺的只是一个可读的组合子和几处行为闸门。

```nolang
DEMO []i64 = with-len(11)
DEMO-MAX-ROWS = 3
...
nl = DEMO[D-LIST]: { ok -> it \n\n -> 0 }
fa = dv-on()
(nl >= DEMO-MAX-ROWS) -> { fa = dv-disabled() }   ; 到上限 -> 禁用
b16 = dv-node(C-BUTTON, 16, 'add row', '', 0, 1, 3, fa, lc)
dv-act(b16, DV-A-DELTA, D-LIST)
dv-hover(b16, D-TIP2)                             ; 禁用节点照样能被 hover
```

- `dv-disabled()` = `DV-F-VIS`（**没有** `DV-F-EN`）；`dv-enabled()` = `VIS|EN`；
  `dv-disabled-of(v)` 返回 1/0。与 `dv-off()` 的区别：`dv-off` 把 flags 清零，
  连可见一起没了 = **隐藏**；`dv-disabled` 保留可见，控件还在原地。
- 调和照常把 `F-ENABLED` 的反面写成 `F-DISABLED`，所以绘制层（按钮 / 开关 / 输入框
  分支）自动画成灰色 —— 这部分阶段 18 之前就有，不用改。
- 阶段 18 补的三处闸门：`dv-dispatch(k)` 对禁用节点返回 `act=0`（点击不产生归约）；
  `dv-hot-dispatch(keycode)` 目标禁用返回 `act=0`（注意 `DV-HOT-VKEY` 存的是
  **声明式 key**，要先 `dv-node-by-key` 反查视图节点再看 flags）；
  `dva-focus(hit)` 对带 `F-DISABLED` 的节点视同空白（禁用输入框点了不开始编辑）。
- **命中故意不拦**：禁用节点仍被 `cs-hit` 命中，hover / tooltip 照常工作 ——
  这是有意的，禁用原因往往就要靠 tooltip 解释（demo 里停在禁用的 `add row` 上
  会浮出「at the row cap」）。

纯逻辑测试（23 项断言，无窗口）：
```sh
../bin/no run -cc ./ccwrap.sh tests/dva-disabled.no   # ALL DVA-DISABLED TESTS PASSED
# Z 标记（dv-disabled = VIS 无 EN；dv-enabled = VIS|EN；dv-disabled-of；dv-clear 不残留）
# Y 调和（活节点 F-DISABLED 有 / F-ENABLED 无；同 key 第二帧改回启用要清掉 F-DISABLED）
# D 派发（禁用节点 act=0；启用节点照常；没挂动作的仍是 0）
# K 快捷键（挂禁用节点不触发；挂启用节点正常，vkey 是声明式 key）
# F 焦点（禁用输入框不接焦点；启用输入框正常；交回禁用节点释放焦点）
# H 命中（禁用节点仍可被命中 —— hover / tooltip 要用）
# V 视图（把 demo 的「按状态选 flags」写法本身钉住：0/2 -> VIS|EN，3/9 -> VIS）
```

## 声明式标签页（阶段19）

命令式内核里 `C-TAB` 早就能画（`cs-paint-tab`）能命中，但用法是命令式的：
手动 `cmp-alloc(C-TAB)` + 逐个建标题子节点 + `tab-select` 改当前页。
声明式层只补一个入口，把「当前页」重新接回状态：

```nolang
DEMO []i64 = with-len(15)
D-TAB = 11
DEMO-TABS []str = ['alpha', 'beta', 'gamma']
...
tab = DEMO[D-TAB]: { ok -> it \n\n -> 0 }
tb = dv-tabs(rc, tab, D-TAB, DEMO-TABS, 40)      ; 标题条 key=40，标题 41..43，容器 44
(tab == 0) -> { dv-label(45, 'page alpha', tb) }
(tab == 1) -> { dv-label(46, 'page beta', tb) }
```

- 结构是数据：`dv-tabs` 建 `C-TAB`（key = `base`）+ n 个 `C-LABEL` 标题
  （key = `base+1+i`）+ 一个 `C-FORM` 内容容器（key = `base+1+n`，函数返回它）。
  标题的 `value` 就是页号，`dv-act(hi, DV-A-SET, slot)` —— 点标题走的还是
  `dv-apply` 那条归约路径，和点按钮完全同构。
- 选中态是 flags：`active` 那一项用 `dv-on-sel()`（`VIS|EN|SEL`），
  命中与绘制都认；另外把 `active` 记进 `DV-TAB-ACT[标题条]`。
- **调和末尾**再 `tab-select(live, active)`：`cs-paint-tab` 画高亮读的是
  `tab-active(h)`（= `CMP-VALUE`），而 `cmp-set-value` 会先把 `dv-value`
  （标题条是 0）写进去。必须放在 `dv-sync` 的最后 —— `dv-apply-props` 跑在递归
  之前，那时容器可能还没孩子，`tab-select` 会因越界被吞掉。
- 越界的 `active` 在 `dv-tabs` 里收敛到最后一页（`n-1`）：否则 `cmp-set-value`
  打成 0、`tab-select` 又因越界忽略，标题条会悄悄跳回第 0 页。
- 切页 = 换子树：内容节点 key 不同 → 旧页删、新页建（各 1 次结构操作），
  标题条与容器同 key 复用，`ops` 不涨。

纯逻辑测试（27 项断言，无窗口）：
```sh
../bin/no run -cc ./ccwrap.sh tests/dva-tabs.no   # ALL DVA-TABS TESTS PASSED
# Z 标记（结构 / key 分配 / 标题文案与 value / SET 动作 / 选中 flags / 非标签节点 -1）
# Y 调和（tab-active / tab-count / tab-title；换页 ops=0；越界 active 保持最后一页）
# D 派发（dv-dispatch 拿 SET + slot；dv-apply 写页号）
# L 布局（标题同一行、x 递增；容器在标题条下方；标题可命中）
# P 绘制（选中标题用 CS-TH-ACCENT，全场景仅此一处；切页后强调色位置跟着走）
# E 端到端（cs-hit -> cmp-key -> dispatch -> apply -> 重建：当前页变、内容子树切换）
```

## 声明式多选列表（阶段20）

`dv-repeat-items`（阶段 14）把四个平行数组摊成 n 个列表项，但每项的 flags 是
写死的 `dv-on()`、`value2` 写死 0。也就是说「这一行选中了吗 / 这一行禁用了吗」
只能靠应用层在命令式侧另建节点 —— 列表项的**状态**进不了 `UI = f(state)`。
阶段 20 把 flags 也变成平行数组：

```nolang
DEMO []i64 = with-len(15)
D-SEL0 = 12                        ; 选中位：DEMO[D-SEL0+i] 非 0 = 第 i 项选中
DEMO-SEL []str = ['apple', 'banana', 'cherry']
DEMO-SEL-ACTS []i64 = [DV-A-TOGGLE, DV-A-TOGGLE, DV-A-TOGGLE]
DEMO-SEL-PAYS []i64 = [12, 13, 14] ; 每项翻自己的槽
...
; 每项的 flags 不是手写 if，而是状态的函数：
dv-repeat-items-f(rc, 53, 3, C-RADIO, DEMO-SEL, DEMO-SEL-VALS,
                  DEMO-SEL-ACTS, DEMO-SEL-PAYS,
                  dv-sel-flags(DEMO, D-SEL0, 3))
ball = dv-node(C-BUTTON, 51, 'all', '', 0, 0, 0, dv-on(), rc)
dv-variant(ball, dv-var-ghost())
dv-act(ball, DV-A-CLICK, DEMO-CMD-ALL)     ; 全选：一次写 3 个槽
```

- `dv-repeat-items-f(parent, key0, n, kind, texts, values, acts, pays, flags)`
  就是阶段 14 的原语多一个 `flags` 平行数组：第 i 项取 `flags[i]`，
  下标越界（数组短、或传 `DV-EMPTY-F`）补 `dv-on()`。
  `dv-repeat-items(...)` **签名一字未改**，内部转调 f 版 —— 既有调用方零改动。
- `dv-sel-flags(S, slot0, n)` 是「状态 → flags」的常用形态：槽非 0 → `dv-on-sel()`，
  否则 `dv-on()`。于是选中行是数据的函数，不需要每帧手写 if。
- 点第 i 项 = `(DV-A-TOGGLE, D-SEL0+i)`，走的还是 `dv-dispatch` → `dv-apply`
  那条归约路径，和点按钮完全同构；`dv-apply` 只翻一个槽。
- 禁用也是同一套：把某项的 flags 换成 `dv-disabled()`（少一个 `DV-F-EN` 位），
  活节点就带 `F-DISABLED`，派发不出动作（阶段 18 的语义）—— 阶段 20 只是让
  「哪几项禁用」也变成状态而已。
- 全选 / 清空：归约器只会「按一个槽做一种运算」，表达不了「一次写 N 个槽」，
  所以走 `DV-A-CLICK` + 显式命令 id（`DEMO-CMD-ALL` / `DEMO-CMD-NONE`），
  和 `reset` 同一套路。这是声明式层里**故意保留的一小段命令式**：
  应用层命令分支仍是一行 `if (pay == ...)`，但主循环里没有。
- 选中态看得见：`C-RADIO` 画 `F-SELECTED` 时用 `CS-TH-OK`，
  所以「选中」在绘制层有真实反馈，不是只存在于 flags 里。

纯逻辑测试（28 项断言，无窗口）：
```sh
../bin/no run -cc ./ccwrap.sh tests/dva-sel.no   # ALL DVA-SEL TESTS PASSED
# Z 标记（结构 / key 递增 / 文案 / 每项 flags / 每项动作载荷 / 短数组补默认 /
#    旧签名回归 / n 收敛到 texts 长度 / 负数 n = 0）
# F 推导（dv-sel-flags 长度 / 0 与非 0 的取值 / 负数长度空数组 / 越界槽补 dv-on()）
# Y 调和（活节点 F-SELECTED / F-DISABLED；换 flags 零结构操作；禁用项派发不出动作）
# D 派发（点第 i 项 = TOGGLE + 自己的槽；dv-apply 翻回来；容器无动作）
# E 端到端（cs-hit -> cmp-key -> dispatch -> apply -> 用 dv-sel-flags 重建：
#    选中态跟着状态走、全选 / 清空、绘制层 CS-TH-OK 计数变化）
```

## 声明式下拉（阶段21）

命令式用法是四步：`cmp-select` 建字段、逐个 `select-add-option` 加选项、
`select-open` / `select-close` 控制展开、`select-choose` 写选中。声明式层把它
压成一行 —— 选项数组 / 选中 / 展开都是数据：

```nolang
DEMO []i64 = with-len(16)
D-CHOICE = 3      ; 选中下标
D-SOPEN = 15      ; 展开槽：非 0 = 字段是展开的
DEMO-OPTS []str = with-len(3)
DEMO-OPTS[0] = 'alpha'
DEMO-OPTS[1] = 'beta'
DEMO-OPTS[2] = 'gamma'
...
; 一行 = 字段 + 3 个常驻选项
dv-select-from(DEMO, lc, 4, D-CHOICE, D-SOPEN, DEMO-OPTS, 'pick', 4)
```

- `dv-select-from(S, parent, key, selslot, openslot, labels, ph, base)` 建出
  `C-SELECT` 字段（key = `key`）和 n 个 `C-LABEL` 选项（key = `base+i+1`，
  `value` = 下标，`value2` = 展开槽）。字段文案取 `labels[S[selslot]]`，
  未选 / 越界就是空串 → 显示占位符 `ph`。
- 字段挂 `(DV-A-TOGGLE, openslot)`：点字段 = 翻展开槽。选项挂
  `(DV-A-CHOOSE, selslot)`：点选项 = 选中并收起。
- `DV-A-CHOOSE = 10` 是阶段 21 新开的动作码，因为「选中并收起」要写两个槽，
  单槽的 `SET` 表达不了。`dv-apply` 的 CHOOSE 分支：`S[payload] = value`
  （选中下标），若 `value2 >= 0` 再 `S[value2] = 0`（清展开槽）。
  选项节点的 `value` / `value2` 正好是这两个操作数，所以视图里的动作数据
  就是全部语义，归约器不需要知道「这是下拉」。
- 选项**常驻**在视图里（不像 dialog / popover 那样条件挂载）：收起时
  `cs-hit` 的 `C-SELECT` 分支直接 `walk = 0`，选项布局不出来也点不中，
  所以展开 / 收起是零结构操作，只改 flags 与文案。
- 选中态看得见：`cs-paint-select` 展开时给 `select-selected(h)` 那一项用
  `CS-TH-ACCENT` 上色。

纯逻辑测试（32 项断言，无窗口）：
```sh
../bin/no run -cc ./ccwrap.sh tests/dva-select.no   # ALL DVA-SELECT TESTS PASSED
# Z 标记（字段 kind / key / 文案 / 占位符 / value / flags；选项 key 递增、文案、
#    value = 下标、value2 = 展开槽、act = DV-A-CHOOSE 且载荷 = 选中槽；
#    字段 act = DV-A-TOGGLE 且载荷 = 展开槽；子节点数；展开 flags；
#    未选 / 越界时字段文案为空、value 原样）
# Y 调和（ops = 4；活节点 kind / value / 文案 / 占位符；select-option-count；
#    select-selected；选项文案；展开后 ops = 0 + F-OPEN；换选中 + 收起后
#    ops = 0 且文案 / value / selected 都跟着状态走）
# D 派发（字段 TOGGLE + 展开槽；选项 CHOOSE + 选中槽；dv-apply 写 2 个槽；
#    字段只写 1 个槽；value2 < 0 的 CHOOSE 只写 1 个槽）
# E 端到端（ops = 4；字段可命中；dispatch + apply 展开；选项可命中；
#    dispatch + apply 选中并收起；重建 ops = 0 且文案 / value / selected 跟随；
#    展开时 CS-TH-ACCENT 计数更高；收起后同一坐标不再命中选项）
```

## 声明式通知（阶段22）

前面所有声明式状态都是「应用写了什么就是什么」（开关 / 选中 / 展开），没有
「自己会变」的状态。通知是典型的时序状态：点一下弹出来，过几帧自己没。
做法是把**触发帧**存进状态槽，让视图每帧自己算存活：

```nolang
DEMO []i64 = with-len(17)
D-TOAST = 16      ; 触发帧：0 = 没有通知；非 0 = dv-frame() 触发于哪一帧
DEMO-TOAST-TTL = 180
...
; 触发按钮：value = dv-frame()，点一下 = 把当前帧写进 D-TOAST
b77 = dv-node(C-BUTTON, 77, 'notify', '', 0, dv-frame(), 0, dv-on(), lc)
dv-act(b77, DV-A-SET, D-TOAST)
...
; 通知本体：活着才建（C-POPOVER + dv-on-tip + 文案 + 关闭按钮）
dv-toast(DEMO, page, 78, D-TOAST, 'saved', DEMO-TOAST-TTL, 79)
```

- `dv-toast-alive(S, slot, ttl)` 是判活原语：`t > 0 && dv-frame() < t + ttl`。
  槽里存的是**触发时的帧号**而不是布尔 —— 布尔只能表达「现在有没有」，
  帧号才能表达「还有多久」。0 是「没有通知」，所以帧计数从 1 起
  （`DV-FRAME[0] = 1`），不会和哨兵撞。
- `dv-toast(S, parent, key, slot, text, ttl, base)` 活着才建整棵子树
  （`dv-cond` 的思路）：`C-POPOVER` + `dv-on-tip()`（`VIS|EN|PASS|OVL`，
  非模态叠加层 —— 仍最后画、自身可被命中，但没接住点击就继续找下面的内容）。
  里面一个 `C-LABEL`（key = `base`）+ 一个关闭按钮（key = `base+1`，
  `DV-A-SET` 载荷 = 触发槽，`value` = 0 → 立即过期）。
- 触发按钮只是视图数据：`value = dv-frame()` + `DV-A-SET(触发槽)`。
  「什么时候触发」也是 `UI = f(state)` 的一部分 —— 视图算好帧号再交给归约器，
  归约器仍然只写一个槽，主循环一行没改。
- 过期不需要定时器、不需要事件：下一帧视图重建时 `dv-toast-alive` 自己说
  「没了」，`dv-sync` 把那棵子树摘掉。注意**删除只算被摘掉的直接子节点**
  （整棵子树随本体一起走），所以关闭 = 1 次操作；而已经建好的活着节点再渲染
  是 0 次操作，只有「活 → 死」那一次摘除算 1。
- 本体用 `C-POPOVER` 而不是 `C-NOTIFICATION`：后者不在 `cs-is-container`
  的白名单里，布局阶段会被当成叶子。

纯逻辑测试（31 项断言，无窗口）：
```sh
../bin/no run -cc ./ccwrap.sh tests/dva-toast.no   # ALL DVA-TOAST TESTS PASSED
# Z 标记（0 槽 / 界内 / 界外 / ttl=0 / 越界槽的 alive 边界；死时不建节点；
#    活时 kind = C-POPOVER、key、flags 带 PASS + OVL、子节点数 = 2、
#    parent、文案、关闭按钮 DV-A-SET 载荷 = 触发槽且 value = 0）
# Y 调和（活 -> ops 3；活节点带 F-OVERLAY / F-PASS；cmp-children = 2；
#    关闭 -> ops 1 且活树里没了）
# D 派发（点关闭 = DV-A-SET + 触发槽；dv-apply 写 1 个槽并清零；toast 本体无动作）
# E 端到端（ops 3；cs-hit 命中 toast / 文案 / 按钮；点关闭 -> dispatch + apply
#    -> 重建 ops 1 且消失；帧推进 ttl 边界逐帧：400/401/402 活（ops 0）、
#    403 起 ops 1 且消失；绘制层文案随存活出现 / 消失；
#    触发按钮接线（value = dv-frame() + DV-A-SET）与 main-dv 一致）
```

## 编译器迁移（safe-index downgrade）

`bin/no` 更新后，**数组索引读取返回带类型 option**（`ARR[i]` → `?i64` / `?str`），
且 `+` / `-` / `*` / `/` 返回溢出检查后的 `?i64`；直接赋给 i64/str 变量是硬编译错误。
库与测试已统一改为两步 match 解包：

```nolang
x = ARR[i]: {
    ok -> it

    -> 0
}
v = x
```

match 的 else 分支写作 `-> `（不是 `err -> `），且各分支必须换行分隔。
`src/components.no`、`tests/app-layer.no`、`tests/main-layered.no`、`tests/core-v4.no`、
`tests/logic/l1-l3.no` 已按此迁移，`no vet` 零错误、`no run` 全绿。

## 编译器修复（2026-11-20）

用户指出「全局变量必须大写」后定位到一个真实编译器缺陷并修复：
`BUF []i64 = with-len(0)` 这类**带 call 初始化的大写全局**未注册为 module global，
导致函数内 push/index 读到空存储（`vec.push: needs receiver and element`）。

修复在 `src/mir/hir2mir.go`（3 处）+ 回归测试（`src/mir/global_call_init_test.go`,
`compiler/tests/probe-glob-call-init.no`），`make test` 全绿。
这让分层内核（函数封装全局池）成为可能 —— `tests/main-layered.no` 即基于修复后的编译器。

一键运行分层版：
```sh
export DYLD_LIBRARY_PATH=/opt/homebrew/opt/sdl3/lib:/opt/homebrew/opt/sdl3_ttf/lib
../bin/no build -cc ./ccwrap.sh -o dist/app-layered tests/main-layered.no
./dist/app-layered
```
启动打印 `scene=8 elements=3 entities=3`，窗口持续存活。

阶段6 —— SDL_GPU 后端（Metal，真 GPU 渲染）+ SDL_ttf 字体 shaping：
```sh
./build-shims.sh                                   # 编译 libnoui-shim.a + libnoui-gpu-shim.a
cd ../example/noui-gpu-demo
../../bin/no build -cc ../../noui/ccwrap.sh -o gpu-demo . && ./gpu-demo
```
GPU demo 源码已移入 `example/noui-gpu-demo/`（仓库 example 区）；noui 提供 shim 库与 `-cc` wrapper。
实测打印 `Metal API Validation Enabled` + 窗口句柄有效（macOS 走 Metal）。

阶段3/4 纯逻辑测试（无窗口）：
```sh
../bin/no run -cc ./ccwrap.sh tests/app-layer.no     # ALL APP-LAYER TESTS PASSED
../bin/no run -cc ./ccwrap.sh tests/widget.no        # ALL WIDGET TESTS PASSED
```

`no test` 覆盖 L1–L3 纯逻辑（实体注册 / flex 布局 / scene dump，11 项断言）：
```sh
cd tests/logic && ../../../bin/no test .       # ALL TESTS PASSED
```
该目录有独立 `package.noml`（无 link-libs），不依赖 SDL/shim，可在无图形环境跑。

## 一键运行

```sh
chmod +x run.sh && ./run.sh
```

前置：`brew install sdl3 sdl3_ttf`（阶段6字体依赖，已装 sdl3 3.4.18 / sdl3_ttf 3.2.2）；Nolang 编译器 `bin/no`。

## 技术路线（已按你的选择：原生 Vulkan/Metal/DX12）

GPUI 的核心是 Application/Entity/Context/View/Element 状态模型 + 三阶段渲染 +
flex 布局 + SDF 绘制 + 平台后端。本纵切面用 SDL3 作为统一的 C ABI 后端
（底层在 macOS 走 Metal、Linux 走 Vulkan、Windows 走 DX12），
Nolang 通过 `#{c}` FFI 驱动窗口、绘制与事件。

## 实测约束（诚实说明，详见 ARCHITECTURE.md §5.1）

早期把「函数内无法访问全局 vec」归为 MIR 限制，实际是**命名违规**：语言规范要求 module 级
全局用大写名开头，旧探针用小写所以函数读到另一份存储。修正命名 + 修复编译器的
with-len 全局注册缺陷后，**分层内核已可落地**（`tests/main-layered.no`）。

当前仍成立的约束：
- **頂層數據綁定四檔命名**（`build/global_case.go` 管同模組、`build/module_check.go` 管跨模組）：`BUF` 大寫=全局（跨函式/跨模組合法）；`buf` 小寫=局部（被函式引用→硬錯誤）；`_BUF`=私有全局（**同模組跨函式讀寫合法**，「_」是模組私有標記；只有跨模組 `mod._BUF` 訪問→硬錯誤）；`_buf`=私有局部（不開放，無條件硬錯誤）。單獨 `_` 通配放行。但**函數名、方法名、接口名、結構體名可小寫 / `_` 開頭**（`foo`、`_helper`），純 main 局部也合法。
- `with-len` 类 call-init 容器只有大写名才注册为 module global：小写名即使在纯 main 内 `push` 也会失败（`vec.push: needs receiver and element`）。共享数据一律大写。
- 不支持 vec-of-struct → 用 SoA 平行数组。
- 字符串用单引号；插值里的复杂表达式先算成局部变量。
- `i = i + 1` 的溢出注解在循环内不稳 → 用 match 解包 option 或区间迭代。
- 🔴 `(cond) { }` 是条件循环（while），`(cond) -> { }` 才是一次性 guard。旧内核的
  「死循环」根因是 3 处 guard 误写成循环（`el-add` + `scene-dump` 两处），
  已全部改为 `-> {}`，大写版 `tests/kernel2.no` 复活并输出 `elements: 2`。

## 文件清单

入口与配置（包根）：
| 文件 | 作用 |
|---|---|
| `main.no` | 唯一入口（`no build .` 约定）：FFI 声明 + 顶层主循环 + 命中测试 + 绘制 |
| `package.noml` | 包配置（link-libs）。工作区用仓库根 `workspace.noml`（`noui: '/noui'`），本目录不再单独放 `workspace.noml` |
| `run.sh` / `build-shims.sh` / `ccwrap.sh` | 一键运行 / 编译 shim 静态库 / `-cc` 链接包装器（注入 sdl3/sdl3_ttf 与 shim 路径） |
| `ARCHITECTURE.md` | 架构设计与实测约束全文 |

`src/` 源码：
| 文件 | 作用 |
|---|---|
| `src/shim/shim.c` `.h` | SDL_Render + SDL_ttf(HarfBuzz) 字体后端 |
| `src/shim/gpu_shim.c` `.h` | SDL_GPU 后端（MSL shader / Metal） |
| `src/entity.no` `src/geom.no` … | 早期库模块（被 `#` 导入）；`geom` 可用，`entity` 因遗留小写全局被四档命名诊断拒绝 |
| `src/components.no` | **全目录组件库**：83 个组件 kind + 核心 10 状态机（gpui-kit 86 模块映射）；阶段15 加 flag 位 `F-OVERLAY`（512）；阶段16 加活池 `CMP-ANCHOR`（锚点 key，-1 = 无）+ `cmp-anchor` / `cmp-set-anchor`；阶段17 加 flag 位 `F-PASS`（1024，非模态叠加层） |
| `src/cmp-scene.no` | **组件渲染层（阶段8）+ 主题层（阶段12）**：`cs-layout` / `cs-measure` / `cs-paint` / `cs-hit` / `cs-is-container` + Scene 指令池（rect/fill/text）；容器布局消费 `L-H` / `L-GROW` / `L-AE` / `L-AC` 提示与 `gap` / `pad` 覆盖；`cs-measure` / `cs-layout` 消费 `CMP-SW` / `CMP-SH` 固定尺寸，`cs-paint-leaf` 经 `cs-align-of` / `cs-text-origin-x` 消费 `CMP-ALIGN`；阶段12 加 `CS-TH` 色板 + `cs-theme` / `cs-theme-set` / `cs-theme-reset` / `cs-var-color`（绘制路径所有分支改读主题色，默认值 = 旧常量）；阶段15 加 `cs-is-overlay` / `cs-child-flow`（叠加层不参与流布局，布局末尾在父矩形内居中，绘制最后画，命中先于其它子节点且点不穿 -> `-2`）；阶段16 加 `cs-find-key`（按声明式 key 反查活句柄）+ 叠加层分支改为锚定优先（锚点下方 -> 贴边翻上方 -> 两轴夹紧到父矩形，无锚点 / 锚点找不到则居中）；阶段17 把 `cs-hit` 的叠加层分支分档：模态竖 `-2` 屏障，非模态（`F-PASS`）照常命中、没接住就继续走流子节点 |
| `src/app.no` | **应用装配层（阶段9）**：`app-init` / `app-frame` / `app-draw-scene` / `app-poll` / `app-hover` + 窗口尺寸 / 底色 / 坐标换算 |
| `src/dv.no` | **声明式视图层（阶段10）**：`dv-*` 构造器（覆盖全部 83 kind，视图节点 SoA 池）+ `dv-render` / `dv-sync` 调和器（按 `(key, kind)` 复用组件、最小增删改排）+ `dv-act` / `dv-dispatch` 动作数据 + `dv-apply` 状态归约器（SET / TOGGLE / DELTA + clamp）+ `dv-repeat` 重复原语 + `dv-cond` 条件节点 + `dv-lay` 布局提示（`dv-row` / `dv-fill` / `dv-lay-*` 组合子）+ `dv-size` / `dv-align` 尺寸与对齐提示（`DV-SW` / `DV-SH` / `DV-ALIGN`）+ `dv-live-by-key` / `dv-live-deep` / `dv-parent-of-key` + `dv-on*` flags 组合子；阶段12 加 `DV-VAR` 变体池 + `dv-variant` / `dv-var-primary|secondary|ghost|danger` + `dv-theme-set` / `dv-theme-reset`（主题是全局的，不随 `dv-clear()` 清空）；阶段13 加 `DV-BIND` / `DV-BIND-MAX` / `DV-BIND-T`（字符串状态池）+ `dv-bind` / `dv-bind-of` / `dv-bind-max-of` / `dv-bound` / `dv-bound-set` / `dv-bind-t-ensure` / `dv-node-count` + `DV-HOT-*` + `dv-hotkey` / `dv-hot-dispatch` / `dv-hot-count`；阶段14 加 `dv-repeat-items`（文案 / value / 动作 / 载荷四个平行数组 → n 个列表项）/ `dv-rows-del`（删第 i 项返回新长度）+ `DV-A-ITEM`（列表项被点，载荷 = 行号）；阶段15 加 `DV-F-OVL`（64）+ `dv-overlay` / `dv-on-overlay`（叠加层标记，经 `dv-apply-flags` 落到活节点 `F-OVERLAY`）；阶段16 加 `DV-ANCHOR` + `dv-anchor-of` / `dv-overlay-anchor`（叠加层 + 锚点 key，经 `dv-apply-props` 落到活节点 `CMP-ANCHOR`）；阶段17 加 `DV-F-PASS`（128）+ `DV-HOVER` 池 + `dv-hover` / `dv-hover-of` / `dv-pass` / `dv-on-tip`（`dv-apply-flags` 覆盖式同步 `F-PASS`）；阶段18 加 `dv-disabled` / `dv-enabled` / `dv-disabled-of`，`dv-dispatch` / `dv-hot-dispatch` 对禁用节点不出动作（`DV-HOT-VKEY` 存的是声明式 key，先 `dv-node-by-key` 反查）；阶段19 加 `DV-TAB-ACT` 池 + `dv-tabs` / `dv-tab-active-of`（标题条 + n 标题 + 内容容器，标题挂 `DV-A-SET`），`dv-sync` **末尾**按 `DV-TAB-ACT` 调 `tab-select` 覆盖回 `CMP-VALUE`（`cs-paint-tab` 读它画高亮；放末尾是因为 `dv-apply-props` 跑在递归之前，那时容器可能还没孩子）；阶段20 加 `DV-EMPTY-F` + `dv-repeat-items-f`（阶段14 原语多一个 flags 平行数组，短数组 / 空数组补 `dv-on()`）+ `dv-sel-flags(S, slot0, n)`（状态槽非 0 → `dv-on-sel()`，否则 `dv-on()`），`dv-repeat-items` 签名不变、内部转调 f 版；阶段21 加 `DV-A-CHOOSE`(10)（选中并收起：`dv-apply` 一次写两个槽 —— `S[payload] = value`，若 `value2 >= 0` 再 `S[value2] = 0`）+ `dv-select-from(S, parent, key, selslot, openslot, labels, ph, base)`（一行建出 `C-SELECT` 字段 + n 个常驻 `C-LABEL` 选项：字段文案 = `labels[S[selslot]]`（未选 / 越界为空 → 占位符 `ph`），字段挂 `DV-A-TOGGLE(openslot)`、选项挂 `DV-A-CHOOSE(selslot)` 且 `value` = 下标 / `value2` = 展开槽）；阶段22 加 `dv-toast-alive(S, slot, ttl)`（槽存**触发帧**，`t > 0 && dv-frame() < t + ttl` 才活，0 = 没有通知）+ `dv-toast(S, parent, key, slot, text, ttl, base)`（活着才建 `C-POPOVER` + `dv-on-tip()` + 文案 + 关闭按钮，关闭 = `DV-A-SET` 写回 0），并把 `DV-FRAME[0]` 初值设为 1，让 0 成为干净的「从未触发」哨兵 |
| `src/dv-app.no` | **声明式应用装配层（阶段11）**：`dva-run(S, title, max-frames)` 主循环（`app-poll` / hover / 命中 / `dva-focus` 焦点交接 / `dva-keyboard` / 按名调 `dv-app-view`·`dv-app-reduce`·`dv-app-click` / 重建视图 / `dv-render` 调和 / `cs-layout` / `cs-paint` / 上屏）+ `dva-frames` / `dva-ops` / `dva-clicks` / `dva-set` / `dva-root` / `dva-px` / `dva-py`；阶段13 加 `dva-sync-binds(root)`（受控输入双向同步）/ `dva-str-get` / `dva-str-set` / `dva-last-get` / `dva-last-ensure`（shadow）；阶段17 加 `dva-sync-hover(S)`（每帧把 hover 写进状态槽）+ `DVA-HIT` / `DVA-MOUSE` 命中缓存（`cs-layout` 之后重算）；阶段18 的 `dva-focus(hit)` 尊重禁用（带 `F-DISABLED` 的节点视同空白，禁用输入框不接焦点）|
| `src/components-catalog.md` | 组件清单：86 模块 → Nolang API 映射 + 设计约束 |

`tests/` 演示与测试（独立可运行档）：
| 文件 | 作用 |
|---|---|
| `tests/main-layered.no` | **分层内核版**：Entity/Element/Scene 大写全局池 + 函数封装 + Scene 遍历绘制 |
| `tests/main-app.no` | **组件 + 场景 + 窗口端到端**：组件树 → `cs-layout`/`cs-paint` → Scene → SDL；hover / 点击 / ESC |
| `tests/cmp-scene.no` | 阶段8 纯逻辑：布局 / 测算 / Scene 发射 / 命中 / 复合组件（40 项断言） |
| `tests/app-assemble.no` | 阶段9 装配层纯逻辑：尺寸 / `fx` / 颜色 / 底色 / hover / 布局 / Scene（29 项断言） |
| `tests/main-form.no` | 阶段9 **窗口端到端表单**：输入 / 下拉 / 滑杆 / 勾选 / 开关 / 提交弹窗（`src/app.no` 装配） |
| `tests/dv.no` | 阶段10 声明式层纯逻辑：新建 / 复用 / 改稿 / 删除 / 重排 / 嵌套 / 条件 / flags / 动作 / 重复 / 事件回路 / 83 kind 全可构造 / 命名构造器 / 状态归约器（65 项断言） |
| `tests/dv-layout.no` | 阶段10 声明式布局纯逻辑：提示同步 / 横向 / gap-pad 覆盖 / 主轴 grow / 交叉轴对齐 / C-GRID 回归 / 真实两栏骨架 / 固定尺寸 / 文本对齐（41 项断言） |
| `tests/dva.no` | 阶段11 装配层纯逻辑：输入类 kind 判定 / 焦点交接 / 键盘喂字 / 计数器（18 项断言） |
| `tests/dv-style.no` | 阶段12 主题 / 变体纯逻辑：默认值 = 旧常量 / 设色 / 越界忽略 / 复位 / 变体→主题语义色 / 真实绘制树发色（29 项断言） |
| `tests/dva-bind.no` | 阶段13 受控输入 / 快捷键纯逻辑：绑定元数据 / 快捷键分派 / 双向同步 9 步（18 项断言） |
| `tests/dva-repeat.no` | 阶段14 列表项纯逻辑：构造 / 钳制 / 事件回路 / 删除差量 / 数组删除（23 项断言） |
| `tests/dva-overlay.no` | 阶段15 模态弹层纯逻辑：标记 / 测量 / 居中 / 命中阻隔 / 绘制顺序（17 项断言） |
| `tests/dva-anchor.no` | 阶段16 锚定浮层纯逻辑：标记 / 调和 / 下方优先 / 贴边翻转 + 夹紧 / 命中阻隔 / 绘制顺序（18 项断言） |
| `tests/dva-disabled.no` | 阶段18 禁用态纯逻辑：标记 / 调和（含改回启用清 `F-DISABLED`）/ 派发闸门 / 快捷键闸门 / 焦点闸门 / 命中仍有效 / 按状态派生 flags 的视图写法（23 项断言） |
| `tests/dva-hover.no` | 阶段17 hover 状态 + 非模态叠加层纯逻辑：标记 / 调和（含降级清 `F-PASS`）/ 状态同步 / 命中穿透（对照模态树与无浮层树）/ 绘制顺序 / hover→tooltip 端到端（23 项断言） |
| `tests/dva-sel.no` | 阶段20 多选列表纯逻辑：标记（结构 / key 递增 / 文案 / 每项 flags / 每项动作载荷 / 短数组补默认 / 旧签名回归 / n 收敛）/ `dv-sel-flags` 推导（长度 / 取值 / 负数长度 / 越界槽）/ 调和（活节点 `F-SELECTED`、`F-DISABLED`、换 flags ops=0、禁用项派发不出动作）/ 派发归约（点第 i 项 = `TOGGLE` + 自己的槽）/ `cs-hit`→`cmp-key`→`dispatch`→`apply`→用 `dv-sel-flags` 重建端到端（28 项断言） |
| `tests/dva-toast.no` | 阶段22 通知纯逻辑：标记（alive 边界：0 槽 / 界内 / 界外 / `ttl=0` / 越界槽；死时不建节点；活时 kind = `C-POPOVER`、key、flags 带 `DV-F-PASS` + `DV-F-OVL`、子节点数 = 2、parent、文案、关闭按钮 `DV-A-SET` 载荷 = 触发槽且 value = 0）/ 调和（活 ops = 3；活节点 `F-OVERLAY` / `F-PASS`；`cmp-children` = 2；关闭 ops = 1 且活树里没了）/ 派发归约（点关闭 = `DV-A-SET` + 触发槽，值 0，写 1 个槽；toast 本体无动作）/ `cs-hit`→`cmp-key`→`dispatch`→`apply`→重建端到端（命中 toast / 文案 / 按钮；点关闭后重建 ops = 1 且消失；ttl 边界逐帧 400/401/402 活 ops = 0、403 起 ops = 1 且消失；绘制层文案随存活出现 / 消失；触发按钮 `value = dv-frame()` + `DV-A-SET` 接线与 main-dv 一致）（31 项断言） |
| `tests/dva-select.no` | 阶段21 下拉纯逻辑：标记（字段 `C-SELECT` 的 kind / key / 文案 / 占位符 / value / flags，选项 key 递增 / 文案 / `value` = 下标 / `value2` = 展开槽 / `act = DV-A-CHOOSE` 载荷 = 选中槽，字段 `act = DV-A-TOGGLE` 载荷 = 展开槽，子节点数，未选 / 越界时字段文案空）/ 调和（ops = 4；活节点 kind / value / 文案 / 占位符；`select-option-count` / `select-selected` / 选项文案；展开后 ops = 0 + `F-OPEN`；换选中 + 收起后 ops = 0 且文案 / value / selected 跟随）/ 派发归约（字段 `TOGGLE` + 展开槽、选项 `CHOOSE` + 选中槽；`dv-apply` 写 2 个槽；`value2 < 0` 只写 1 个槽）/ `cs-hit`→`cmp-key`→`dispatch`→`apply`→重建端到端（展开后可命中选项、选中并收起、`CS-TH-ACCENT` 计数更高、收起后同一坐标不再命中选项）（32 项断言） |
| `tests/dva-tabs.no` | 阶段19 标签页纯逻辑：标记 / 调和（换页 ops=0、越界收敛）/ 派发归约 / 布局（标题同行 x 递增、容器在条下方）/ 绘制（选中标题独占 `CS-TH-ACCENT`）/ `cs-hit`→`cmp-key`→`dispatch`→`apply`→重建端到端（27 项断言） |
| `tests/main-dv.no` | 阶段10+11+12+13+14+15+16+17+18+19+20+21+22 **窗口端到端声明式 demo**：`dva-run` 接管主循环，按名提供 `dv-app-view` / `dv-app-reduce` / `dv-app-click`；视图含居中页头 + `dv-row`/`dv-size`/`dv-fill` 定宽两栏 + 重复行 + 组件目录速写；阶段12 在 `dva-run` 前设 9 槽主题、视图里挂 6 处变体；阶段13 把输入框绑到 `DV-BIND-T[0]` 并回显、`r` 键重置；阶段14 三行各挂 `(DV-A-ITEM, 行号)`，点任意一行删掉它；阶段15 `dialog` 按钮写 `D-DIALOG=1`，视图里出现居中的模态对话框（ok / cancel 写回 0）；阶段16 `popover` 按钮写 `D-POP=1`，视图里出现锚在触发按钮下的菜单（`close` 写回 0）；阶段17 `dv-hover(b19, D-TIP)` 让鼠标停在 `dialog` 按钮上时锚在它下面浮出 tooltip（非模态，点得穿），页头回显 `tip=`；阶段18 按状态派生禁用（`nl >= DEMO-MAX-ROWS` 时 `add row` 用 `dv-disabled()`、`nl <= 0` 时 `del row` 禁用），并给禁用的 `add row` 挂 `dv-hover(b16, D-TIP2)` —— 禁用节点照样能被 hover，浮出「at the row cap」解释原因；阶段19 右栏底部加一条三页标签（`dv-tabs(rc, tab, D-TAB, DEMO-TABS, 40)`，页头回显 `tab=`，reset 回到第 0 页），点标题切页时容器与标题条同 key 复用、只换内容子树；阶段20 右栏顶部加一条三行多选（`dv-repeat-items-f(rc, 53, 3, C-RADIO, DEMO-SEL, DEMO-SEL-VALS, DEMO-SEL-ACTS, DEMO-SEL-PAYS, dv-sel-flags(DEMO, D-SEL0, 3))`，每项 `(DV-A-TOGGLE, D-SEL0+i)`，页头回显 `sel=N`）+ `all` / `none` 两个幽灵按钮（`DV-A-CLICK` + `DEMO-CMD-ALL` / `DEMO-CMD-NONE`，命令分支一次写 3 个槽），reset 一并清空选中位；阶段21 左栏下拉换成 `dv-select-from(DEMO, lc, 4, D-CHOICE, D-SOPEN, DEMO-OPTS, 'pick', 4)`（原来是手写 `C-SELECT` + 3 个选项 + `DV-A-OPEN` / `DV-A-SET`，事件侧那两段 select 分支一并删掉），右栏单选组文案改从 `DEMO-OPTS` 读（`dv-demo-opt(i)`），reset 多写一个 `D-SOPEN`，页头回显 `pick=`；阶段22 左栏加 `notify` 按钮（`value = dv-frame()` + `DV-A-SET(D-TOAST)`），视图里 `dv-toast(DEMO, page, 78, D-TOAST, 'saved', DEMO-TOAST-TTL, 79)` 非模态叠加出「saved」，页头回显 `tst=`，reset 多写一个 `D-TOAST`（`src/app.no` + `src/dv.no` + `src/dv-app.no` + `src/cmp-scene.no`） |
| `tests/core-v4.no` | 分层内核 v4（Entity → Element → Scene） |
| `tests/app-layer.no` | 阶段3：Ctx / Action / 焦点系统 |
| `tests/widget.no` | 阶段4：widget 构造器 + 主题 |
| `tests/interaction.no` | 交互逻辑单元测试 |
| `tests/logic/` | `no test` L1–L3 纯逻辑（独立 package，无 shim 依赖） |
| `tests/kernel.no` `tests/kernel2.no` `tests/test-kernel.no` | 旧 470 行完整分层原型 / 大写修复版 / gen-test.sh 生成物 |
| `tests/probe.no` `tests/test-l1.no` `tests/sv.no` `tests/t1-3.no` | 早期探针 |

仓库 `example/` 区：
| 文件 | 作用 |
|---|---|
| `example/noui-gpu-demo/` | 阶段6：SDL_GPU 演示（独立 package） |
| `example/noui-pkgtest/` | 早期包结构探针（独立 package） |

## 验收记录

```
$ ./run.sh
noui on SDL 3.4.18
font shaping: 1
window handle: 506987857408
bye                          # ESC / 超时后正常退出

$ ../bin/no run -cc ./ccwrap.sh tests/interaction.no
all-ok=true
ALL INTERACTION TESTS PASSED

$ ../bin/no run -cc ./ccwrap.sh tests/app-layer.no
ALL APP-LAYER TESTS PASSED

$ ../bin/no run -cc ./ccwrap.sh tests/widget.no
ALL WIDGET TESTS PASSED

$ ../bin/no run -cc ./ccwrap.sh tests/cmp-scene.no
scene-count=17 all-ok=true
ALL CMP-SCENE TESTS PASSED

$ ../bin/no run -cc ./ccwrap.sh tests/app-assemble.no
win=truetruetrue fx=true col=truetrue bg=truetrue quit=true
layout=truetruetruetruetrue
select=truetruetruetrue hit-open=true hit-closed=true
hover=truetruetrue slider=truetruetrue
scene root=12 dlg-closed=0 dlg-open=5 -> truetruetrue
all-ok=true
ALL APP-ASSEMBLE TESTS PASSED

$ ../bin/no build -cc ./ccwrap.sh -o dist/main-app tests/main-app.no && ./dist/main-app
noui (app) on SDL 3.4.18
window handle: 531422744064        # 组件树 → 布局 → Scene → SDL，窗口持续存活
bye                          # ESC / 超时后正常退出

$ ../bin/no build -cc ./ccwrap.sh -o dist/main-form tests/main-form.no && ./dist/main-form
noui on SDL 3.4.18
window handle: 528022111232        # 表单装配：输入 / 下拉 / 滑杆 / 弹窗全链路
bye                          # 900 帧后自动收尾，exit 0（约 17s）

$ ../bin/no run -cc ./ccwrap.sh tests/dv.no
=== dv tests ===
A build=truetruetrue reuse=truetruetrue del=true add+order=truetrue reorder=true
B nest=truetruetruetruetrue edit=truetrue nest-del=true
C cond=truetrue on=truetrue off=true
D select=truetrue edit=true
E flags=truetrue
F act=truetruetrue dispatch=truetrue edit=true
G repeat=truetruetrue reuse=true grow=truetrue shrink=true empty=true clamp=truetrue
H loop=true state=true diff=true
ops A=2,0,1,1,0 B=3,0,1 C=1,3,2 D=3,0 E=1,0,0
ops F=2,0 G=3,0,2,4,0,2,5 H=2,0,0
I catalog=true flags-on=true flags-off=true fallback=true
J builders=true props=true flag-fns=true
K reduce=truetruetrue skip=true back=true view=true clamp=true
all-ok=true
ALL DV TESTS PASSED

$ ../bin/no build -cc ./ccwrap.sh -o dist/main-dv tests/main-dv.no && ./dist/main-dv
noui on SDL 3.4.18
window handle: 501383218176        # 声明式：dva-run 接管主循环，按名调三个回调 -> 视图重建 -> 调和进活树
bye frames=900 ops=53 clicks=0 rows=2 sel=0 pick=0 tst=0   # 首帧 53 个节点（阶段19 三页标签 +7、阶段20 多选 +5、阶段21 下拉仍是 4 个节点、阶段22 触发按钮 +1；通知默认不活，所以 toast 子树不计），之后 899 帧零结构操作，exit 0（连跑两次一致）

$ ../bin/no run -cc ./ccwrap.sh tests/dva.no
=== dva tests ===
U input-kind=truetruetruetruetruetrue
V focus=truetruetruetruetruetrue
W keyboard=truetruetruetrue
X counters=truetrue
all-ok=true
ALL DVA TESTS PASSED

$ ../bin/no run -cc ./ccwrap.sh tests/dv-style.no
=== dv-style tests ===
Y theme-def=truetruetruetruetruetruetruetruetruetrue
Z theme-set=truetruetrue
AA var=truetruetruetruetruetruetrue
AB paint=truetruetruetruetruetruetruetruetrue
all-ok=true
ALL DV-STYLE TESTS PASSED

$ ../bin/no run -cc ./ccwrap.sh tests/dva-bind.no
=== dva-bind tests ===
Y bind=truetruetruetrue
Z hot=truetruetruetruetrue
AA sync=truetruetruetruetruetruetruetruetruetrue
all-ok=true
ALL DVA-BIND TESTS PASSED

$ ../bin/no run -cc ./ccwrap.sh tests/dva-repeat.no
=== dva-repeat tests ===
P build=truetruetruetruetruetruetruetruetrue
Q clamp=truetruetruetruetruetruetrue
R loop=truetruetruetruetruetruetruetrue
S diff=truetrue
T del=truetrue
all-ok=true
ALL DVA-REPEAT TESTS PASSED

$ ../bin/no run -cc ./ccwrap.sh tests/dva-overlay.no
=== dva-overlay tests ===
U mark=truetruetruetruetruetrue
V measure=truetrue
W layout=truetruetrue
X hit=truetruetruetrue
Y paint=truetrue
all-ok=true
ALL DVA-OVERLAY TESTS PASSED

$ ../bin/no run -cc ./ccwrap.sh tests/dva-anchor.no
=== dva-anchor tests ===
L1 box=1200,1200,80000,60000 anchor=2400,5600,10000,2000 pop=2400,8400,8800,4800
L2 box=1200,1200,80000,6000 anchor=2400,5600,10000,2000 pop=2400,1200,11200,6000
Z mark=truetruetruetrue
Y live=truetruetruetrue
L layout=truetruetruetruetruetrue
H hit=truetruetrue
P paint=true
all-ok=true
ALL DVA-ANCHOR TESTS PASSED

$ ../bin/no run -cc ./ccwrap.sh tests/dva-hover.no
=== dva-hover tests ===
H box=1200,1200,77600,8000 tip=38000,30000,24000,4800
Z mark=truetruetruetruetrue
Y live=truetruetruetrue
S sync=truetruetruetruetrue
H hit=truetrue
P paint=true
T e2e=truetrue
all-ok=true
ALL DVA-HOVER TESTS PASSED

$ ../bin/no run -cc ./ccwrap.sh tests/dva-disabled.no
=== dva-disabled tests ===
Z mark=truetruetruetruetrue
Y live=truetruetruetrue
D dispatch=truetruetrue
K hotkey=truetrue
F focus=truetruetrue
H hit=true
V view=truetrue
all-ok=true
ALL DVA-DISABLED TESTS PASSED

$ ../bin/no run -cc ./ccwrap.sh tests/dva-tabs.no
=== dva-tabs tests ===
Z mark=truetruetruetruetruetruetruetruetrue
Y live=truetruetruetruetrue
D disp=truetruetrue
L lay=truetruetrue
P paint=truetrue
E e2e=truetruetruetruetruetrue
all-ok=true
ALL DVA-TABS TESTS PASSED

$ ../bin/no run -cc ./ccwrap.sh tests/dva-sel.no
=== dva-sel tests ===
Z mark=truetruetruetruetruetrue
F fill=truetruetrue
Y live=truetruetruetruetrue
D disp=truetruetrue
E e2e=truetruetruetruetruetrue
all-ok=true
ALL DVA-SEL TESTS PASSED

$ ../bin/no run -cc ./ccwrap.sh tests/dva-select.no
=== dva-select tests ===
Z mark=truetruetruetruetruetruetruetruetruetruetruetrue
Y live=truetruetruetruetruetrue
D disp=truetruetruetruetrue
E e2e=truetruetruetruetruetruetruetruetrue
all-ok=true
ALL DVA-SELECT TESTS PASSED

$ ../bin/no run -cc ./ccwrap.sh tests/dva-toast.no
=== dva-toast tests ===
Z mark=truetruetruetruetruetruetruetruetruetruetrue
Y live=truetruetruetrue
D disp=truetruetrue
E e2e=truetruetruetruetruetruetruetruetruetruetruetruetrue
all-ok=true
ALL DVA-TOAST TESTS PASSED

$ cd tests/logic && ../../../bin/no test .
all-ok=true
ALL TESTS PASSED

$ cd ../example/noui-gpu-demo && ./gpu-demo
Metal API Validation Enabled      # SDL_GPU 在 macOS 走 Metal
window handle: 528093741056
```
