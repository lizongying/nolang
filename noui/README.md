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
- **声明式滚动（阶段23）**：滚动条不见了 —— 滚到哪了也是状态。新增
  `dv-scroll(S, key, offslot, step, parent)`：每帧把 `S[offslot]` 读出来当组件
  `value`，布局据此把内容整体上移（子节点保持自身高度、不拉伸），`step` 放
  `value2` 当滚轮每格步长。偏移的**真源是状态槽**：布局只负责「夹紧到
  内容高 − 视口高」后排版，不写回状态；写回发生在事件侧 —— 新增滚轮事件
  （`type = 5`，光标位置走 x/y 槽、有符号增量走 bit56..63，因为 24 位掩码装不下
  负数），装配层的 `dva-wheel` 拿命中顺着父链找到滚动容器，用布局算出的
  `cmp-scroll-max` 夹紧后 `dva-set` 回同一个槽（与滑杆同一模式）。绘制层给
  滚动容器的每条指令带上裁剪矩形（`cs-emit-clip` / `noui_clip`），视口外的
  内容根本不画；命中层把视口外的点直接判不命中、视口内的点按全局坐标直接走
  `cs-hit-inner`（子节点矩形本身已带 `-off` 偏移，不用再换算一次；阶段24
  修掉了这里多换一次 `py + off` 导致大偏移点不中的 bug），嵌套滚动靠递归各自
  判视口矩形。于是页头那行 `scrl=N`
  也是 `UI = f(state)`。落点：`dv-scroll` + `dva-wheel` + `CMP-SCROLL-MAX` /
  `cmp-parent-of` + `cs-emit-clip` + `noui_clip` + `tests/dva-scroll.no`
  （33 项断言，含状态驱动重建、布局偏移 + 夹紧 + 不写回、裁剪指令、视口
  命中换算、滚轮归约两端夹紧）；demo 右栏顶部加一条 10 行 / 视口 160px 的
  滚动列表（内容约 336px），首帧 `ops` 53 → 64（滚动容器 + 10 行）
- **声明式虚拟列表（阶段24）**：一万行也只挂可见的那几行。滚动容器新增两个
  声明数据：`dv-scroll-content(h, ch)` 说「内容高是 ch」（不写 = 0 = 照老规矩
  实测子节点），`dv-scroll-start(h, i0)` 说「挂上来的第一个儿子是第 i0 行」。
  布局在声明模式下不再累加子节点高度，直接用 `ch` 算可滚范围，并把第 j 个
  儿子排到 `(i0 + j) * 行距` 上（所以窗口滑到底时行不会全堆在顶部）；命中层
  顺手修了一个阶段23 留下的 bug —— `cs-hit` 之前把 `off` 又加了一遍到 `py`
  上，而子节点的矩形本来就带 `-off` 的全局偏移，导致大偏移时视口内怎么点都
  不命中，现在改成「视口内的点按全局坐标直接命中」。一行式原语
  `dv-list-view(S, key, offslot, step, viewh, rowh, n, texts, ...)` 内部走
  `dv-scroll` + `dv-size` + `dv-scroll-content` + `dv-scroll-start` +
  `dv-repeat-at`（切片版重复原语，行 key 用**绝对行号**，所以窗口滑动时
  `dv-sync` 复用同一批组件，零结构操作）。内容高公式
  `n * rowh + (n - 1) * CS-GAP + 2 * CS-PAD`，可见窗口
  `i0 = (off - pad - rowh) / 行距 + 1` 夹到 `[0, n-1]`，
  `i1 = ceil((off - pad + viewh) / 行距)` 夹到 `n`。落点：`dv-list-view` /
  `dv-repeat-at` / `dv-scroll-content` / `dv-scroll-start` + `DV-SCROLL-CONTENT`
  / `DV-SCROLL-START` + `CMP-SCROLL-CONTENT` / `CMP-SCROLL-START` +
  `tests/dva-virtual.no`（36 项断言，含窗口滑动前后的挂载数 / 行距 / 绝对
  key 复用 / 两端夹紧 / 命中 / 滚轮归约）；demo 右栏那条列表换成 20 行 /
  视口 160px 的虚拟列表，任意时刻只挂 5 行，首帧 `ops` 64 → 59
- **声明式过滤列表（阶段25）**：搜索框是最典型的「显示哪些由输入决定」——
  之前要手写 if 把命中的行挑出来再挂。现在列表内容是**查询串的纯函数**：
  查询来自动态字符串状态 `DV-BIND-T[qslot]`（`dv-bound` 读），
  `dv-filter-map` 每帧算出命中行号表（`str.to-lower` + `str.index`，大小写
  不敏感，空查询命中全部），`dv-filter-list` 只挂可见窗口那几行。行 key 用
  **源行号**而不是窗口位置，所以查询变化 / 窗口滑动都按源行号复用组件。
  顺手把窗口计算抽成 `dv-win-range`（`dv-list-view` 也改用它，单一真源），
  并补 `dv-repeat-mapped`（`dv-repeat-at` 的「按下标表取行」版，越界下标
  夹到表尾、避免建出一堆同 key 的行）。落点：`dv-filter-list` /
  `dv-filter-map` / `dv-filter-count` / `dv-repeat-mapped` / `dv-win-range` +
  `tests/dva-filter.no`（46 项断言，含大小写不敏感 / 子串命中 / 无命中 /
  值按源下标取 / 命中数驱动内容高与 maxoff）；demo 页头加搜索框，右栏列表
  改成 `dv-filter-list`，页头回显 `match=N`，首帧 `ops` 59 → 60
- **声明式排序与分组（阶段26）**：列表的「**按什么顺序显示**」也应该是状态 ——
  之前换顺序要手写 if 把行挑出来重排。现在一个 mode 槽（0 源序 / 1 键升 /
  2 键降 / 3 分组 / 4 分组降）驱动整条纯函数链：`keys`/`grps` → `dv-sort-map`
  （**稳定**插入排序，出下标表；`n` 夹到 `keys.len()`、负数当 0）→
  `dv-group-plan`（下标表 → **混合行表**：token < 0 = 组标题，名字取
  `names[-token-1]`；分组顺序跟着排好的表走，不是源序）→ `dv-win-range`
  （混合行表里取可见窗口）→ `dv-repeat-plan`（按混合行表挂行 / 挂标题，
  行 key = `key0 + 源行号`、标题 key = `hkey0 + 组号`，所以换模式仍按源行号
  复用组件）。内容高按**混合行数**算（分组时 = 行数 + 组数），所以「能滚多远」
  跟着模式变。落点：`dv-sort-run` / `dv-sort-map` / `dv-group-plan` /
  `dv-repeat-plan` / `dv-order-list` + `tests/dva-order.no`（43 项断言，含
  稳定排序 / 越界与非法 mode / 分组 token 与组序 / 内容高与 maxoff 随模式变 /
  标题行禁用且用主题色 / 行位 / 超界偏移夹紧不写回 / 命中与滚轮）；demo 右栏
  那条列表换成 `dv-order-list`，上方加「order」下拉（`dv-select-from` 第二次
  出场），页头回显 `sort=`，首帧 `ops` 60 → 65
- **声明式表单校验（阶段27）**：「这个字段合不合法」也不该是手写 if-else 链 ——
  校验规则同样应该是**数据**：一张扁平规则表（`kinds` / `pars` 平行数组，按
  `base + n` 切片）描述每个字段「怎样算不合法」，值来自受控输入的字符串槽
  （`DV-BIND-T`）。`dv-rule-check` 返回**第一条**违反的规则号（1 基，而且是
  全局规则下标 +1 而不是切片内序号 —— 文案表因此能按规则号直接查），
  `dv-rule-msg` 把规则号换成文案（文案挂在**规则**上，不是字段上，同一种规则
  在不同字段可以写不同措辞），`dv-form-bad` 把多字段汇总成「坏了几项」。
  `dv-field` 一行建出「标签 + 输入（绑字符串槽）+ 错误行」，错误行只在
  （有违反 && 揭示槽为 1）时才出现在视图里 —— **「什么时候显示错误」是另一根
  轴**，首次进页面不该一片红。规则种类：必填 / 最小长度 / 最大长度 / 最小值 /
  最大值（不可解析算违反）/ 与另一槽相等 / 包含子串。落点：`DV-R-*` +
  `dv-rule-check` / `dv-rule-msg` / `dv-form-bad` / `dv-field` +
  `tests/dva-valid.no`（46 项断言，含逐条规则 / trim / 多规则取第一条 /
  切片与越界吞掉 / 汇总 / 揭示前后视图差量 / 组件复用与零操作 / 绘制与命中）；
  demo 左栏改成 `dv-scroll` 滚动容器 + 四字段校验表单（新增提交命令：不合格
  写揭示槽、合格计数 + 通知），页头回显 `vsub=` / `lsc=`，首帧 `ops` 65 → 76
- **声明式虚拟表格（阶段28）**：列表的「一行一个标签」升级成「一行 N 列」——
  表头、可见窗口的行、能滚多远，全都是声明出来的。`dv-table-rows` 把窗口内的
  混合行表挂成 `C-GRID` 行（每行 `ncols` 个 `C-LABEL` 单元格），`dv-table-list`
  在它之上补一条**不滚的表头**与滚动容器；它是 `dv-order-list` 的严格超集
  （`ncols = 1` 且不分组时行为等价），吃同一套 `plan`（token ≥ 0 = 源行号 /
  token < 0 = 组标题）—— 所以组标题在表格里是**通栏行**（占满表宽、禁用的
  标签），数据行才是 N 列。宽度是**声明**的（`colws` 逐列给，表宽 =
  `2*pad + 列宽和 + (ncols-1)*gap`），不靠实测 —— 否则「可见窗口的行」只有
  几行，实测宽会随窗口内容浮动。落点：`dv-table-rows` / `dv-table-list` +
  `tests/dva-table.no`（32 项断言，含行结构 / 列宽 / plan 映射 / 通栏组标题 /
  `count` 与 `start` 两端夹紧 / 表头 + 声明内容高 → `maxoff` / 状态驱动偏移 /
  组件复用与零操作重渲染 / 绘制 / 命中与派发）；demo 右栏那条列表换成两列表格
  （`name` / `key`），表头不滚、行滚动，分组模式多出来的组标题变成通栏行，
  左栏让宽（300px → 220px）、右栏视口 160px → 112px 给表头与「order」下拉让位，
  首帧 `ops` 76 → 83
- **声明式可拆面板（阶段29）**：分栏位置也是状态 —— 两栏之间那条 8px 的间隙**就是
  拖杆**。`dv-dock-split(key, slot, minw, maxw, gw, parent)` 建一台 `C-DOCK` 横向
  容器：`gap = gw` 是拖杆命中区，动作 `DV-A-CHANGE` + 载荷 = `slot`（左屏宽的真源），
  `value` / `value2` = 夹紧界。两块面板之间的 gap 不属于任何子节点，命中测试落到
  容器自己身上（`cs-hit` 的自身矩形兜底），所以**不需要新增节点种类、也不需要给布局
  引擎加分支** —— `C-DOCK` 只是一台通了电的横向容器。配合 `dv-size(paneA, S[slot], 0)`
  + `dv-lay(paneB, dv-lay-grow(), 0, 0)`，拖栏和滚动是同一个模式（空间写进状态），
  区别只是滚动沿内容轴、拖栏沿分栏轴。shim 补了左键抬起（type 6），拖拽才是完整的
  「按下抓住 / 移动 / 松开放手」三段；事件侧 `dva-drag-down` / `-move` / `-up` 做
  间隙判定 + 光标换算 + 夹紧 + 写回，主循环 `t == 2` 先试拖杆，抓住了就不走点击路径。
  落点：
  `dv-dock-split` + `dva-drag-*` + `tests/dva-dock.no`（31 项断言，含标记 / 布局 /
  命中 / 拖拽换算与夹紧 / 竖拆 / 拖完重建零操作）；demo 左右两栏就是这条缝拖开的
- **声明式代码编辑器（阶段30）**：多行编辑 + 行号 + 语法高亮 + 光标，仍然
  **不新增任何组件种类**。`dv-code-editor(S, key, tslot, offslot, step, viewh, rowh,
  gutter, key0, parent)` 一次建出「滚动容器 + 可见窗口的行」：文本按 `\n` 切行
  （`dv-edit-split` 保留结尾空行），每行一个 `C-FORM`，行内 token 是着色的
  `C-LABEL`（关键字 = 主色 / 字符串 = `CS-TH-OK` / 注释 = `CS-TH-MUTED` /
  数字 = 琥珀），行号是右对齐的 `C-LABEL`，光标是定宽 2px 的 `C-SEPARATOR`。
  内容高 = `n*行高 + (n-1)*间隙 + 2*内边距`，经 `dv-scroll-content` 声明给布局，
  所以「能滚多远」不依赖所有行都在场；行 key = `key0 + 绝对行号`，滚动时同一行
  复用同一批组件 —— 与虚拟列表 / 虚拟表格同一套路。**文本 / 光标 / 滚动是三份
  状态**：文本是字符串状态 `DV-BIND-T[tslot]`（真源），光标是 dv 侧的
  `DV-EDIT-L/C[tslot]`（按**文本槽**存，不按视图节点 —— 否则每帧都被 `dv-clear`
  清回行首），滚动偏移是应用状态槽 `S[offslot]`；`dv-edit-follow` 只在光标**动了**
  时重排窗口，所以滚轮把窗口推走时不会被弹回去。**键盘喂字在 dv 侧**：装配层
  只负责「命中 → 焦点 = 编辑器 key」（`dva-edit-find` 顺活父链上溯到 `dv-edit`
  声明处），之后每个按键走 `dv-edit-key` 改文本状态，下一帧视图重建出行 / token /
  光标 —— 所以 'q' 在编辑器里打得出来（编辑器也算「正在敲字」）。落点：
  `dv-code-editor` + `dv-edit-*` + `dv-lex-*` + `dva-edit-*` +
  `tests/dva-editor.no`（61 项断言，含着色 / 行模型 / 编辑原语 / 窗口 / 跟随 /
  装配与焦点交接 / 重建）；demo 左栏底部就是这个编辑器，点进去直接敲
- **声明式树（阶段31）**：内核有 `C-TREE` 这个容器 kind，但**没有绘制分支**，
  声明式层也只有一个空壳 `dv-tree(key, parent)` —— 层级 / 缩进 / 拍平 / 虚拟窗口
  全得应用自己手写递归渲染。阶段 31 把「展开态」变成一根状态轴：
  `dv-tree-flat(S, openslot, n, kids0, kids, parents, out, deps)` 用**迭代式先序**
  （显式栈 —— 语言没有可用递归，深度也不该由数据决定）把展开态作用到树上，
  产出**可见行表**（先序）+ 每行层级；数据形状是「先序序列 + 兄弟块相邻」
  （每个节点的孩子落在连续下标块 `kids0 .. kids0+kids-1`），和 DOM 的扁平序列化同构。
  `dv-tree-row` 一行 = `C-FORM` + 「缩进 + 箭头」右对齐 `C-LABEL` + 文本 `C-LABEL`，
  选中态三处一起变（flags / 行底色 / 文本色），箭头只在有孩子时挂
  `DV-A-TOGGLE(openslot + 节点)`、行挂 `DV-A-SET(selslot)`。
  `dv-tree-view` 一行式：`dv-scroll` + `dv-scroll-content` + `dv-win-range` + 逐行
  `dv-tree-row`，偏移真源是状态槽 `offslot`，行 key = `key0 + 节点号*10000000`。
  新动作码 `DV-A-FILL = 11` 一次把 `S[payload .. payload+value2)` 全写成 `value` ——
  「全部展开 / 全部收起」只是同一动作的两个操作数，应用层不写循环。
  落点：`dv-tree-flat` / `dv-tree-row` / `dv-tree-view` + `DV-A-FILL` +
  `tests/dva-tree.no`（42 项断言，含可见行表 / 行结构 / 缩进 / 箭头 / 选中态 /
  调和 / 派发归约 / 命中到重建端到端）；demo 左栏顶部就是这棵树
- **声明式命令面板（阶段32）**：内核的 `C-COMMAND` 只是个空壳（一个节点、无绘制
  分支），声明式层也只有一个占位 `dv-command(key, parent)` —— 「搜索 → 过滤 →
  键盘导航 → 执行」整套全缺。阶段 32 补 `dv-command-palette(S, key, openslot,
  qslot, offslot, cmdslot, n, texts, acts, key0, parent)` 一行式面板 = 模态叠加层
  （`dv-on-overlay()`）+ 受控查询输入 + `dv-scroll` 虚拟列表 + 逐行（`C-FORM` 行 +
  文本 `C-LABEL`）。查询串来自动态字符串状态 `DV-BIND-T[qslot]`，命中表由既有的
  `dv-filter-map` 给（大小写不敏感，空查询命中全部），行 key = `key0 + 源行号*100`
  —— **按源行号**，所以查询一变，同一行复用同一批组件。新动作码 `DV-A-RUN = 12`：
  `S[payload] = value`（命令码）+ `S[value2] = 0`（关面板），点行 = 执行 + 收起，
  两个槽一次写完。**面板自己收键盘**：装配层每帧用 `dv-pal-open-key(S)` 问出哪个
  面板开着并写进 `DVA-PAL-FOCUS`，键盘优先级 面板 > 输入 > 编辑器 > 全局 ——
  所以「按 p 打开、直接敲字过滤、↑↓ 选行、回车执行、ESC 关掉」不需要先点输入框；
  `dva-focus` 也不再让焦点落进面板内的输入框（面板内点击照样命中行）。
  面板的游标 / 命中数 / 命中表抄本按**面板 key** 存在 `DV-PAL-*` 池里（跨帧存活，
  因为 `dv-clear()` 只清视图描述），`dv-pal-cur-key` 现算游标行的 key ——
  游标移动不重建视图，回车必须立刻知道当前指的是哪一行。落点：
  `dv-command-palette` + `dv-pal-*` + `DV-A-RUN` + `dva-pal-*` +
  `tests/dva-palette.no`（34 项断言，含标记 / 调和 / 派发归约 / 命中到执行端到端）；
  demo 左栏树下面那个 `palette` 按钮（快捷键 `p`）就是它
- **命令式树（阶段33）**：命令式侧的 `C-TREE` 一直只是个空壳（`cmp-tree()` 返回一个
  空容器），所以阶段 31 的树完全长在声明式层。阶段 33 把同一套能力补回命令式层，
  **不新增组件 kind、不改渲染层**：树就是普通纵向容器，可见行是应用调出来的真组件树。
  模型是一组按句柄索引的平行池 `TR-PAR` / `TR-FIRST` / `TR-NEXT` / `TR-OPEN`
  （first-child / next-sibling 链 + 展开标记），节点表按树分段存在 `TR-ALL`，
  段界记在 `TR-TSTART` / `TR-TCOUNT`（`cmp-tree-add` 追加，两棵树互不干扰）。
  `cmp-tree-flat(h, out, deps)` 用**迭代式先序**（显式栈）把「展开态」这根轴作用到
  模型上，产出可见行表 + 每行层级 —— 收起 = 子树整块不出现，兄弟是单向链表，
  所以先正序收进临时数组再**倒着压栈**，弹出顺序才是正序。
  `cmp-tree-sync` 把可见行重建成 `C-FORM` 横向行 + 右对齐「缩进 + 箭头」`C-LABEL` +
  文本 `C-LABEL`，选中态三处一起变（flags / 行底色 / 文本色），箭头只在有孩子时给
  真箭头（叶子留空定宽占位，文本起点因此对齐）。选中存 `CMP-VALUE[树]`（-1 = 没选中）。
  因为 `CHILD-IDX` 是**全局扁平数组**、`cmp-child` 按 `HEAD + i` 取，一个父的孩子必须
  连续挂载，所以 `cmp-tree-sync` 是**两段式**：先把所有行壳一口气挂到树上，再逐行填内容。
  落点：`src/components.no` 的 `cmp-tree-*` / `ct-*` + `tests/cmp-tree.no`
  （36 项断言，含模型 / 拍平 / 两段式重建 / 选中 / 二次 sync / 命中上溯 / 两树隔离）+
  `tests/main-tree.no` 窗口 demo（点行选中 / 点箭头展开 / ↑↓ 移动 / ←→ 收起展开 /
  Enter 切换 / 滚轮 / `e` `c` 全展开全收起 / `r` 重置）
- **命令式代码编辑器（阶段34）**：命令式侧的编辑器同样只是 API + 最小状态。
  阶段 34 补 `cmp-editor()`（根就是 `C-SCROLL`）+ 文本模型池 `ED-LINES` / `ED-KEEP`
  与光标 / 跟随 / 行标记池 + 一整套编辑原语（insert / backspace / enter / move /
  key / del-line / ins-line / clamp）+ `ed-lex-quote` / `ed-lex-line` 六类着色
  （关键字 / 字符串 / 注释 / 数字 / 标识符 / 空白，相邻同色合并）+
  `cmp-win-range`（与声明式 `dv-win-range` 同一套窗口公式）+ `cmp-editor-follow`
  （光标没动就不重排窗口，`ED-SEEN` 记上次跟随时的光标行）+ `cmp-editor-sync`
  两段式重建可见行 + 声明内容高。落点：`src/components.no` 的 `cmp-editor-*` /
  `ed-lex-*` + `tests/cmp-editor.no`（65 项断言）+ `tests/main-editor.no`
  窗口 demo（打字 / 退格 / 回车 / Tab / 方向键 / 点行 / 滚轮 / `r` 重置）。
  坑：形参别叫 `ok`（见 `ARCHITECTURE.md §5.28`）。另有一条「`cmp-editor-sync` 必须
  平铺在顶层作用域」的旧限制（包进函数会拿到 `undef` 句柄），2026-11-22 随编译器
  模块存储判定的修复作废。
- **命令式虚拟表格（阶段35）**：命令式侧的 `C-TABLE` 也只是 API + 最小状态。
  阶段 35 补 `cmp-table()`（根是普通 `C-TABLE` 容器，`value` = 列数 /
  `value2` = 行数）+ 全局数据模型 `TB-HEAD` / `TB-COLW` / `TB-CELL` / `TB-KEY`
  / `TB-ORDER`（显示顺序 = 源行号表）+ 随 `cmp-alloc` 逐槽长的句柄状态池
  `TB-OFF` / `TB-SEL` / `TB-HEAD-H` / `TB-SCR-H` / `TB-ROW-SRC` / `TB-CELL-SRC`
  / `TB-CELL-COL` + `tb-arr-set`（`[]i64` 下标写）+ 一整套
  `cmp-table-set-head` / `-set-col-w` / `-set-cell` / `-set-key` / `-sort`
  （按 `TB-KEY` 对 `TB-ORDER` 做**稳定**插入排序，选中态跟着源行号走）/
  `-reset-order` / `-select` / `-scroll` / `-off` / `-set-off` /
  `-row-of` / `-cell-of` / `-cell-rc`（沿 `cmp-parent-of` 上溯）+
  `cmp-table-sync`（表头 `C-GRID` + 滚动体 `C-SCROLL` + 可见行，声明内容高
  `m*rowh + (m-1)*800 + 2*1200`，于是 `cmp-scroll-max` 不依赖「所有行都挂出来」，
  和声明式 `dv-table-list` 一致）。落点：`src/components.no` 的 `cmp-table-*` /
  `tb-arr-set` + `tests/cmp-table.no`（57 项断言，含排序稳定性 / 降序 / 空表 /
  两表隔离 / 重定义表头）+ `tests/main-table.no` 窗口 demo（3 列 20 行，
  点行选中 / 点单元格报源行:列 / `1` `2` `0` 升序降序源序 / 滚轮 / `r` 重置）。
  坑两条（`ARCHITECTURE.md §5.29`）：`CHILD-IDX` 要求**一个父的所有直接子
  先连续挂完，再去填任何一棵子树**；`;` 是**行注释**不是语句分隔符，
  分支体里写 `{ a; b; c }` 会把 `}` 一起注释掉，整段错版死循环
- **命令式可拆面板（阶段36）**：命令式侧的 `C-DOCK` 只是一台通了电的横向容器
  （`cs-is-container` 认它，但没有分栏语义）。阶段 36 补 `cmp-dock-split` /
  `cmp-dock-split-v`（把间隙声明成拖杆命中区，`minw` / `maxw` 写进 `value` /
  `value2` 当夹紧界，位置写进新池 `DK-SPLIT-V` 并初始化成 `minw`）+
  `cmp-dock-splittable` / `-split-val` / `-set-split` / `-grab` / `-up`
  + `ct-clamp`（`hi <= 0` = 不夹上限，与 `dv-clamp` 一致）+ `cs-dock-down`
  （命中 -> 间隙判定 -> 抓住，返回 1 让主循环跳过点击路径）+ `cs-dock-move`
  （光标换算成左屏宽 / 上屏高，夹紧后写回 `DK-SPLIT-V`）。**不新增节点种类**：
  复用 `cs-hit-inner` 的「自身矩形兜底」，间隙自己就是拖杆。落点：
  `src/components.no` 的 `cmp-dock-*` / `DK-SPLIT` / `DK-SPLIT-V` / `DK-GRAB`
  + `src/cmp-scene.no` 的 `cs-dock-down` / `cs-dock-move` +
  `tests/cmp-dock.no`（58 项断言，含夹紧 / 命中 / 拖拽 / 竖拆 / 双 dock 隔离 /
  普通 dock 不抓）+ `tests/main-dock.no` 窗口 demo。
  坑三条（`ARCHITECTURE.md §5.30`）：`value` 是**夹紧下界**不能拿来存当前位置
  （写掉就再也夹不到 `minw`）；`print` 的 `{field}` 必须是调用前已绑定的标识符；
  嵌套分支体里的 `->` else 臂要自己包 `{ ... }`
- **命令式命令面板（阶段37）**：内核的 `C-COMMAND` 同样只是个空壳（一个节点、
  无绘制分支）。阶段 37 补 `cmp-command()`（根就是 `C-COMMAND` 普通竖排容器）+
  命令表 `CP-TEXT` / `CP-ACT`（应用写）+ 随 `cmp-alloc` 逐槽长的 `CP-Q`（每面板
  查询串）/ `CP-CUR`（游标 = **命中表下标**，不是源行号）/ `CP-OFF`（滚动偏移
  真源）/ `CP-SB` + 只增的 `CP-SRC`（命中源行号扁平抄本，和 `DV-PAL-SRC` 同构）/
  `CP-INP-H` / `CP-SCR-H` / `CP-ROW-SRC` + `cmp-command-count` / `-add` /
  `-query` / `-set-query` / `-type` / `-backspace` / `-off` / `-set-off` /
  `-scroll` / `-input` / `-sync`（输入框 + 滚动体第一次建好就复用，命中表抄副本，
  **全摘**旧行 -> 两段式重挂 -> 填内容；声明内容高 `m*rowh + (m-1)*800 + 2*1200`）+
  `-cur` / `-set-cur` / `-move`（夹紧在命中区间内）/ `-cur-src` / `-run`
  （取 `CP-ACT[源行号]`）/ `-row-of` / `-row-src`。**不新增节点种类**：面板完全由
  应用调出来的真组件拼（行是 `C-FORM` + `C-LABEL`），渲染层只走通用容器路径；
  模态标记由应用自己 `cmp-flag-set(panel, F-OVERLAY)`。落点：
  `src/components.no` 的 `cmp-command-*` / `CP-*` +
  `tests/cmp-command.no`（90+ 项断言，含两段式重建结构 / 命中上溯 / 导航夹紧 /
  过滤 / 空命中 / 滚动 / 重复 sync 不累积 / 双面板隔离）+
  `tests/main-command.no` 窗口 demo。五个功能对等长期项到此全部补齐。
  坑五条（`ARCHITECTURE.md §5.31`）：`CHILD-IDX` 是全局扁平数组，父的直接子必须
  占一段连续槽位（摘一半再挂会静默错位，重建的行必须**全摘**）；同一标识符在
  一个函数里带两种类型会**误编译**（甚至静默 exit 0）；`**` 在循环外是干净退出；
  `ct-get(arr, -1)` 不返回 -1；`cs-hit` 对模态 overlay 点在外面返回 `-2`
- **命令式输入框富文本 / 语法高亮（阶段38）**：`C-INPUT` 此前只有纯文本
  `CMP-TEXT` + placeholder + focus，命令式侧最后一条「只有 API + 最小状态」的
  组件。阶段 38 补 `input-caret` / `-set-caret` / `-move`（**字节**下标，和
  `slice-bytes` 一致）/ `input-insert`（在光标处插入）/ `-backspace` /
  `-delete` / `input-syntax` / `-syntax-mode` / `input-lex-build` /
  `-lex-count` / `-lex-seg` / `-lex-color` + 随 `cmp-alloc` 逐槽长的
  `INP-CAR` / `INP-SY` / `INP-SGO` / `INP-SGN` + **只增**的段池
  `INP-SG` / `INP-SGC`（每句柄一段，基址 + 段数索引，和 `CP-SRC` 同构），
  词法沿用阶段 34 的 `ed-lex-line`（关键字 / 字符串 / 注释 / 默认色）；
  渲染层新增 `cs-paint-input` / `cs-paint-input-lex`（高亮模式下按段画，
  光标落进段内就把该段拆成前后两截、光标画在中间）。**不新增节点种类**：
  `C-INPUT` 仍是叶子，布局 / 测量 / 命中路径不动；`input-append` 保留旧语义
  （追加到末尾并把光标推过去），`input-set` / `input-clear` 同步重置光标，
  于是 `main-form` / `dv-app` 行为逐位不变。落点：`src/components.no` 的
  `INP-*` / `input-*` + `src/cmp-scene.no` 的 `cs-paint-input*` +
  `tests/cmp-input.no`（26 项断言，含定点编辑 / 夹紧 / disabled 写无效 /
  词法段数与配色 / 渲染段数与光标位置）+ `tests/main-input.no` 窗口 demo
  （两个字段，一个开高亮一个纯文本；打字 / 退格 / 前删 / ←→ / `p` 切高亮 /
  `r` 重置 / `q` 退出）。坑两条（`ARCHITECTURE.md §5.33`）：嵌套分支体里的
  `->` 兜底臂必须再包一层裸 `{ ... }`，否则**整个模块静默不导出**，症状是
  用户文件满屏 `<name> is not defined`，只有 `no fmt -d` 能看到真错；
  光标统一用字节下标，`ct-get` 越界返回 0（不是 -1）
- **命令式 OTP 验证码输入（阶段41）**：组件目录写着 `C-OTP-INPUT`「digits, slots」，
  但命令式侧它只有 `cmp-otp-input(len)` 一个空壳构造（`CMP-VALUE` 存槽数）——没有逐槽
  数字、没有活动槽、没有退格 / 前删 / 灌入，渲染还落在单行 `cs-paint-field`。阶段 41
  补 `OT-CODE` 定长码池（**空格 = 空槽**）+ `OT-CUR` 活动槽池（都随 `cmp-alloc` 逐槽
  长）+ `otp-slots` / `otp-cur` / `otp-cur-set` / `otp-code`（短了补空格、长了截断）/
  `otp-digit` / `otp-set-digit`（`d` 不在 `0..9` = 清空）/ `otp-clear-slot` /
  `otp-clear` / `otp-filled` / `otp-digits` / `otp-value` / `otp-complete` /
  `otp-put`（写活动槽后跳到**下一个空槽**，满了停末槽）/ `otp-backspace`（活动槽有数字
  就清它，否则回退清上一格）/ `otp-delete` / `otp-left` / `otp-right` /
  `otp-set`（只取数字字符灌入，超长截断）+ 槽格几何 `otp-slot-w`（格宽 = 控件高，格距
  800，宽不够就缩、下限 1200）/ `otp-slot-x` / `otp-hit`。渲染层新增 `cs-paint-otp`：
  整块字段底 + 每槽一个暗色格 + 数字居中 + 聚焦时活动槽四条强调描边 + 空槽竖光标；
  `cs-measure` 给 OTP 算 `槽数 * 格宽 + (槽数-1) * 格距` 的宽（否则 6 位验证码只有
  一个 24 宽的输入框样子）。**不新增节点种类**：仍是叶子，布局 / 命中路径不动。
  落点：`src/components.no` 的 `OT-CODE` / `OT-CUR` / `otp-*` + `src/cmp-scene.no`
  的 `cs-paint-otp` + `tests/cmp-otp.no`（48 项断言，含构造 / 写槽 / put 跳空槽 /
  聚合 / 退格回退 / 前删 / 左右夹紧 / 灌入截断 / disabled / 槽格几何 / 命中空隙 /
  渲染槽格与居中的落点 / 聚焦描边颜色）+ `tests/main-otp.no` 窗口 demo（6 槽 + 4 槽
  两个，点槽移活动槽、打字、退格 / 前删 / ←→、`c` 清空、`r` 重置、`q` 退出，900 帧
  `otp=123456 cur=0 otp2=2468 cur2=0` + `otp render OK`）。坑两条
  （`ARCHITECTURE.md §5.36`）：**插值 `{x}` 只在该字符串作为实参（如 `print` /
  `cmp-set-text`）时展开，写在赋值号右边就是字面量**——拼字符串必须用
  `s.push-str(t)` 逐段追加（`input-insert` 就是这么做）；`i = i + 1` 在 nolang 里是
  `?i64`（溢出检查），必须走 `ct-inc` / `ct-add`，否则编译期报
  `cannot assign ?i64 value to i64 variable`
- **命令式 combobox 可输入下拉（阶段42）**：组件目录写着 `C-COMBOBOX`「query,
  options, selected」，但命令式侧它只有 `cmp-combobox()` 一个空壳构造——没有选项
  API、没有查询过滤、没有选中，渲染还落在单行 `cs-paint-field`。阶段 42 补：查询串
  沿用 `CMP-TEXT`（复用 `INP-CAR` 光标池，编辑走 `input-insert` / `-backspace` /
  `-delete`），选项与 select 同构存 `CHILD-IDX`（`cmp-child` 可读，**不新增节点
  种类**），另存一份**匹配表** `CB-MBASE` / `CB-MN` / `CB-MLIST`（扁平只增，同
  `INP-SG` 段池思路）+ `CB-MCUR` 高亮，都随 `cmp-alloc` 长。API：`combobox-refresh`
  （查询空 = 全选；否则选项 `to-lower` 后 `contains` 查询 `to-lower`，大小写不敏感；
  顺手把高亮夹紧到 `[0, mn)`）/ `-query` / `-set-query` / `-add-option` /
  `-option-count` / `-option` / `-selected` / `-choose` / `-match-count` /
  `-match` / `-match-label` / `-match-index` / `-match-index-of` / `-highlight` /
  `-highlight-set` / `-highlight-move` / `-choose-match` / `-apply` / `-insert` /
  `-backspace` / `-delete` / `-clear`（写操作全查 `F-DISABLED`）。渲染层新增
  `cs-paint-combobox`：字段底 + 查询文本（空则 placeholder，`CS-TH-MUTED`）+ 聚焦
  竖光标 + 右端 `'v'` 箭头；展开时**只画匹配项**，高亮行用 `CS-TH-ACCENT` 底。
  `cs-measure` 在展开时按**匹配项**里最宽的算宽（收起床宽 6400，展开 9600），
  `cs-layout` 只排匹配项，`cs-hit` 经新的 `cs-hit-child` 把「第 i 个（虚拟）子节点」
  在 combobox 展开时翻译到匹配表——**画的 / 点的 / 量的三者同源**。落点：
  `src/components.no` 的 `CB-M*` / `combobox-*` + `src/cmp-scene.no` 的
  `cs-paint-combobox` / `cs-hit-child` + `tests/cmp-combobox.no`（41 项断言，含
  构造 / 加选项 / 大小写不敏感过滤 / 匹配反查 / 高亮夹紧与移动 / 选中与
  choose-match / apply / 编辑查询 / disabled / 测算与布局 / 渲染与命中）+
  `tests/main-combobox.no` 窗口 demo（水果 + 颜色两个，点字段开关、打字过滤、
  ↑↓ 移高亮、Enter 选中、Esc 收起、`c` 清空、`r` 重置、`q` 退出，900 帧
  `cb1=Banana sel1=1 match1=1 open1=true cb2=Blue sel2=2 open2=false` +
  `combobox render OK`）。坑三条（`ARCHITECTURE.md §5.37`）：**`-> { a; b }` 里的
  分号是注释不是分隔符**——`{ ct-set(...); return }` 会把 `return` 和收尾的 `}` 一起
  注释掉，于是这个 `{` 再不闭合，**整个模块从该行起静默截断**（症状：后半截所有
  顶层名字 `is not defined`，`no fmt` 却不报错），必须拆成两行；nolang **没有前向
  引用**，函数定义必须在使用之前，所以 `combobox-refresh` 要放在整块的最前面；
  插值里不能调函数，`print('n={f(x)}')` 报 `unresolved format field`
- **值域型展示控件补全（阶段43）**：组件目录第 10 / 33 / 67 行分别写着 `C-EMPTY`
  「title, description」、`C-PAGINATION`「page, total」、`C-RATING`「value, max」，
  但命令式侧三者都只有一个构造函数——rating / pagination 连值都没有 API，empty 的
  描述（`CMP-TEXT2`）更是没人画，渲染还落在通用 `rect + text` 兜底里。阶段 43 补：
  `rating-max` / `-value` / `-set`（夹紧 `0..max`，`F-DISABLED` 不动）/ `-cell`
  （星格宽 = 行高 2400）/ `-hit`（命中第几颗星，0 起）；`pagination-total` /
  `-page` / `-clamp` / `-set` / `-prev` / `-next` / `-btn-w` / `-label`（「页/总数」）/
  `-hit`（`-1` 没命中 / `0` 上一页 / `1` 标签 / `2` 下一页）；empty 的描述走
  `CMP-TEXT2`（`cmp-empty` 只收标题，描述由应用 `cmp-set-text2` 给）。槽位沿用既有
  约定：`VALUE` = 当前值，`VALUE2` = 上限 / 满分 / 总页数。渲染层新增
  `cs-paint-rating`（填满的星用组件色、没填的用 `CS-TH-MUTED`，星形用 ASCII `*`——
  同 select 的 `v` / search 的 `x` 一路，不依赖非 ASCII 字形的字体覆盖）、
  `cs-paint-pagination`（左 `<` / 中间「页/总数」/ 右 `>`，到顶 / 到底时对应钮画
  `CS-TH-MUTED`）、`cs-paint-empty`（标题一行 + 描述一行，各在半个矩形里居中），
  `cs-measure` 给三者各自的 intrinsic 尺寸。**`C-PAGINATION` 同时从
  `cs-is-container` 白名单里拿掉**——它的页码按钮是**数据**不是视觉子节点
  （同 `C-SELECT` / `C-COMBOBOX` / `C-TAB` 一路），混进白名单会让「给分页挂真子
  节点」和「虚拟页码」两套语义打架。落点：`src/components.no` 的新段 +
  `src/cmp-scene.no` 的三个 `cs-paint-*` + `tests/cmp-rating.no`（54 项断言：slider
  下限回归 / rating 读写夹紧 / rating 命中 / rating 测算与渲染 / pagination 读写
  夹紧与 label / pagination 命中 / pagination 测算与渲染 / empty 测算与渲染）+
  `tests/main-rating.no` 窗口 demo（点星条设星数、点页码钮翻页、点滑条设值，
  `a` `d` 星数、`z` `x` 翻页、`s` `w` 滑条、`e` 空态描述开关、`r` 重置、`q` 退出，
  900 帧 `slider=40 rating=3/5 page=1/12 label=1/12` + `rating render OK`）。
  顺带修掉一条**真 bug**：`cmp-slider(min,max)` 只把 `min` 写进 `CMP-VALUE`（初值），
  没写 `CMP-VALUE3`（下限真源），而 `slider-value-at` / `slider-set` /
  `cs-paint-leaf` 都读 `VALUE3` 当下限——`min != 0` 时左端点返回 0 而不是 `min`；
  既有测试全拿 `min == 0` 构造，所以一直没暴露。已补 `CMP-VALUE3[h] = min`，
  并把渲染层改走对称读取器 `slider-min` / `slider-max`（`tests/cmp-rating.no`
  的 `t1`–`t8` 就是这条回归）。坑两条：`cs-emit-text-center` 存进 Scene 的是
  **居中后的落点**（`x + (w - tw)/2`）而不是矩形左上角，按矩形坐标去断言颜色会
  全落空；`rating-hit` 的右边界是 `x + max * cell`（星格总宽），不是控件矩形宽 `w`
- **会话族补全（阶段45）**：组件目录第 53 / 54 行分别写着 `C-BUBBLE`「text,
  position」、`C-SPEECH`「text, lang」，但命令式侧两者都只有一个构造函数——
  `position` / `lang` 都没有 API，渲染还落在通用 `rect + text` 兜底里。阶段 45
  补：bubble 的 `CMP-VALUE` = position（`0` 左 / `1` 右）、`CMP-TEXT` = 文本、
  `CMP-COLOR` = 气泡底色，`bubble-pos` / `bubble-set-pos`（夹紧 `0/1`，disabled
  写无效）；speech 的 `CMP-TEXT` = 文本、`CMP-TEXT2` = 语言标签（空 = 不画标签行），
  `speech-lang` / `speech-set-lang`。渲染层新增 `cs-paint-bubble`（底色方块 +
  内边距里的文本，按 position 左对齐 / 右对齐 + 底边上的小尾巴方块，尾巴朝
  position 那一边）与 `cs-paint-speech`（文本行 + 有标签时多一条 `CS-TH-BG` 细分隔线
  + 标签行），`cs-measure` 给 bubble 加 intrinsic（宽 = 文本宽 + 左右内边距、
  高 = 行高 + 上下内边距），speech 因要读 `speech-lang` 而在 `cs-measure` 的兜底
  分支里算（同阶段44 的 `C-ALERT` 理由：`cs-leaf-size` 只收 `(kind, tlen)`，
  没有句柄）。文本用 `CS-TH-BG`——气泡底色是组件色（默认深蓝 / 灰），配背景色
  比配 `FG` 对比度高。落点：`src/components.no` 的新段 + `src/cmp-scene.no` 的两个
  `cs-paint-*` + `tests/cmp-bubble.no`（28 项断言）+ `tests/main-bubble.no` 窗口
  demo（点气泡翻面、点语音泡开关语言标签行，`l` `r` 翻面、`g` `h` 开关标签、
  `q` 退出，900 帧 `b1-pos=0 b2-pos=1 sp-lang=zh-CN sp2-lang=` + `bubble render OK`）
- **提示族补全（阶段44）**：组件目录第 45 / 46 / 47 行分别写着 `C-NOTIFICATION`
  「level, timeout」、`C-ALERT`「level, closable」、`C-MESSAGE`「level」，但命令式
  侧三者都只有一个构造函数——连 `level` 都没有 API，渲染还落在通用 `rect + text`
  兜底里。阶段 44 补一套共用的 level 语义：`LV-INFO` / `LV-OK` / `LV-WARN` /
  `LV-ERR` + `lv-slot`（level -> 主题槽下标，表驱动，`-1` = 没有对应槽）+
  `msg-level` / `msg-set-level`（`F-DISABLED` 不动）；alert 的 `alert-closable` /
  `-set-closable` / `alert-close-w` / `alert-close-x` / `alert-hit`（`-1` 没命中 /
  `0` 主体 / `1` 关闭钮）；notification 的 `ntf-timeout` / `ntf-remain` /
  `ntf-set-timeout` / `ntf-set-remain` / `ntf-ratio`（`0..10000` 定点百分数，
  `remain` 夹进 `[0,total]`，`total <= 0` -> `0`）。槽位沿用既有约定：`VALUE` =
  level，`VALUE2` = 总时长，`VALUE3` = 剩余 / closable。渲染层新增
  `cs-level-color`（`0`->`ACCENT`、`1`->`OK`、`2`->琥珀常量、`3`->`DANGER`，
  未知值按 `info` 画；warning 没有主题槽，用常量是同 `dv.no` 的 `DV-C-NUM` 先例）、
  `cs-level-glyph`（`i` / `v` / `!` / `x`，ASCII 字形同 select 的 `v` 一路）、
  `cs-paint-message`（左色条 + 字形格 + 文本一行）、`cs-paint-alert`（message +
  右端 `'x'` 关闭钮，`closable` 时才画）、`cs-paint-notification`（文本一行 +
  剩余时间条一行，条长 = `ntf-ratio`），`cs-measure` 给三者各自的 intrinsic 尺寸。
  落点：`src/components.no` 的新段 + `src/cmp-scene.no` 的三个 `cs-paint-*` +
  `tests/cmp-alert.no`（62 项断言：level 读写与 `lv-slot` 映射 / alert closable
  与命中 / notification 时长与比值 / 三者测算 / 三者渲染）+ `tests/main-alert.no`
  窗口 demo（点 alert 右端 `x` 关掉 closable，`l` `t` `n` 循环三者 level、`c`
  开关 closable、`,` 扣剩余时间、`.` 重置、`r` 全部重置、`q` 退出，900 帧
  `msg-level=0 alert-level=3 closable=1 ntf-level=2 ntf-ratio=10000` +
  `alert render OK`）。实现中踩到两条坑：`cs-leaf-size` 只收 `(kind, tlen)`，
  **没有句柄**——在里面写 `cmp-text(h)` 不会报错但取不到文本（`h` 不是参数），
  提示族的文本宽必须用 `tlen`；alert 的关闭钮宽度必须加在最小宽度夹紧**之前**，
  否则短文本会先被夹到 `CS-MIN-LEAF-W` 再长回去，把关闭钮挤出固有宽——因此
  `C-ALERT` 整个不走 `cs-leaf-size`，改在 `cs-measure` 的兜底分支里算（那里才有
  句柄可读 `alert-closable`）
- **命令式输入族补全（阶段40）**：组件目录写着 `C-NUMBER-INPUT`「value, min, max,
  step」、`C-STEPPER`「value, min, max」、`C-SEARCH`「query, clear」，但命令式侧三者
  都只走单行 `cs-paint-field`——没有值域 / 步长 / 吸附，没有 `[−]` / `[+]` 按钮，
  也没有清除钮。阶段 40 补值域与步长 `num-range` / `num-step-set` / `num-step` /
  `num-min` / `num-max` / `num-clamp`（`hi<=0` = 不夹上限，与 `ct-clamp` 同语义）/
  `num-snap`（先夹紧，再四舍五入到 `lo + k*step`，最后再夹一次）/ `num-value` /
  `num-set`（吸附 + 写 `VALUE` + 同步 `CMP-TEXT`）/ `num-bump`（`dir * step`，
  disabled 不动）/ `num-commit`（把 `CMP-TEXT` 解析回值，失败不动值；`to-i64` 不裁
  空白，带空格的文本按非法处理）+ 搜索 `search-query` / `search-clear` /
  `search-hit`（右端 `ht` 宽的正方形清除区）+ 步进器 `stepper-hit`（`-1` 左 /
  `0` 中 / `1` 右，控件太窄时按钮对半分）。槽位沿用 slider 的约定：`VALUE` =
  当前值、`VALUE3` = 下限、`VALUE2` = 上限，新增 `CMP-VALUE4` = 步长（`<=0` 按 1）
  随 `cmp-alloc` 逐槽长。渲染层新增 `cs-paint-number-input`（右对齐 + 光标贴右端，
  按「右内边距 − 光标后缀宽」定位）/ `cs-paint-stepper`（`[−]` 值 `[+]`，杠是纯
  轴对齐矩形）/ `cs-paint-search`（文本 + 光标 + 右端清除钮；清除钮用 ASCII `x`
  字形——和 `cs-paint-select` 拿 `v` 当箭头同一路子，不依赖非 ASCII 字形的字体
  覆盖，且只在有文本时画）。**不新增节点种类**：三者仍是叶子，`cs-measure` 早就给
  它们 `CS-BTN-H` 的高，布局 / 命中路径不动。落点：`src/components.no` 的
  `CMP-VALUE4` / `num-*` / `search-*` / `stepper-hit` + `src/cmp-scene.no` 的
  `cs-paint-number-input` / `cs-paint-stepper` / `cs-paint-search` +
  `tests/cmp-number.no`（42 项断言，含值域 / 吸附 / 夹紧 / bump / commit 失败不动值 /
  命中 / 渲染右对齐与居中落点 / 减号与加号杠的几何）+ `tests/main-number.no`
  窗口 demo（点步进器左右钮增减、点搜索右端清除、数字输入打字 + 回车提交、`r` 重置、
  `q` 退出）。坑两条（`ARCHITECTURE.md §5.35`）：`to-i64` 的 `ok` 臂标签本身是个
  **bool** 变量，返回值不能也叫 `ok`（会撞名），且 bool 只能赋 `true` / `false`
  不能赋 `1` / `0`；`cs-paint-*` 的落点断言要按「右内边距 / 居中」反算，
  只断言「有没有这段文本」是测不出对齐的
- **命令式多行文本域（阶段39）**：组件目录写着 `C-TEXTAREA`「multiline value」，
  但命令式侧它只有 `cmp-textarea` 一个空壳构造——`input-set` 塞多行也只按**一行**
  画（走单行 `cs-paint-field`），没有行概念、没有上下移光标、没有视口。阶段 39 补
  行几何 `ta-line-count` / `-line-start` / `-line-len` / `-line-text` /
  `ta-caret-line` / `ta-caret-col` / `ta-move-up` / `ta-move-down`（保持列、夹到目标
  行长度，首 / 末行不动）+ 视口 `TA-OFF` 池（每句柄首行行号）+ `ta-off` /
  `ta-set-off` / `ta-follow`（夹紧到「最后 cap 行」，`cap<=0` 按 1）。文本与光标仍走
  `CMP-TEXT` + `INP-CAR`（阶段 38 的字节下标），所以**换行 / 退格 / 前删直接复用**
  `input-insert` / `input-backspace` / `input-delete`——行首退格合并上一行、行尾前删
  合并下一行、行中插 `\n` 多一行，都是这三条原语的推论，不另写编辑函数。渲染层新增
  `cs-paint-textarea`：字段底 + 焦点描边 + **只画视口内的行**（`l` 从 `off` 到
  `min(lines, off+cap)`，多行文本不再靠 `CS-CLIP` 事后裁剪，滚出视口的行根本不进
  Scene）+ 行内光标；`C-TEXTAREA` 从 `cs-paint-field` 改走它。**不新增节点种类**：
  `cs-measure` 早就给它 `2 * CS-BTN-H` 的高，布局 / 命中路径不动。落点：
  `src/components.no` 的 `TA-OFF` / `ta-*` + `src/cmp-scene.no` 的
  `cs-paint-textarea` + `tests/cmp-textarea.no`（34 项断言，含行几何 / 光标行列 /
  上下移夹紧 / 换行与合并 / disabled / 视口夹紧 / 渲染视口内外）+
  `tests/main-textarea.no` 窗口 demo（打字 / 换行 / 退格 / 前删 / ↑↓←→ / `r` 重置 /
  `q` 退出，900 帧 `caret=26 line=2 col=6 off=0 lines=3`）。坑一条
  （`ARCHITECTURE.md §5.34`）：`cs-paint-textarea` 的视口循环上界必须同时受
  `lines` 与 `off + cap` 约束，只判 `lines` 会把滚出视口的行也发进 Scene
  （`CS-CLIP` 只影响绘制，不影响 Scene 内容，断言一眼看不出）
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

> 本轮是「全目录 API + 核心扎实」档：**核心交互组件的功能对等长期项已全部补齐**——
> 注意这句只覆盖「有明确交互语义、此前只有 API + 最小状态」的那批；全目录 86 项里
> 仍有约 40 个 kind 只有一个构造函数，其中 17 个叶子 kind 还落在通用 `rect + text`
> 兜底绘制里（清单见 `src/components-catalog.md` 覆盖统计末尾）。
> DataTable 虚拟滚动 / 代码编辑器 / 树 / Dock 四样的
> 命令式侧已由阶段 35 / 34 / 33 / 36 补上（声明式超集分别是 阶段 28 `dv-table-list`、
> 阶段 30 `dv-code-editor`、阶段 31 `dv-tree-view`、阶段 29 `dv-dock-split`）；
> 命令面板 / `C-VIRTUAL-LIST` 由阶段 37 补上；`C-INPUT` 的富文本 / 语法高亮由
> 阶段 38 补上（`INP-*` 光标池 + `input-insert` / `-delete` / `-syntax` / `-lex-*`
> + `src/cmp-scene.no` 的 `cs-paint-input`，着色复用阶段 34 的 `ed-lex-line`；
> 仍是叶子节点，不新增 kind）；`C-TEXTAREA` 的多行值 / 行几何 / 视口由阶段 39
> 补上（`TA-OFF` + `ta-*` + `cs-paint-textarea`，编辑复用阶段 38 的三条原语；
> 仍是叶子节点，不新增 kind）；`C-NUMBER-INPUT` / `C-STEPPER` / `C-SEARCH` 的
> 值域 / 步长 / 吸附 / 按钮 / 清除由阶段 40 补上（`CMP-VALUE4` + `num-*` +
> `search-*` / `stepper-hit` + `cs-paint-number-input` / `-stepper` / `-search`，
> 槽位沿用 slider 的 `VALUE` / `VALUE3` / `VALUE2`；仍是叶子节点，不新增 kind）；
> `C-OTP-INPUT` 的逐槽数字 / 活动槽 / 退格 / 灌入与每槽一格的渲染由阶段 41 补上
> （`OT-CODE` / `OT-CUR` + `otp-*` + `cs-paint-otp`，槽数沿用 `CMP-VALUE`；仍是叶子
> 节点，不新增 kind）；`C-COMBOBOX` 的查询过滤 / 匹配表 / 高亮 / 选中与展开面板的
> 渲染由阶段 42 补上（`CB-MBASE` / `CB-MN` / `CB-MLIST` / `CB-MCUR` + `combobox-*`
> + `cs-paint-combobox` / `cs-hit-child`，查询串沿用 `CMP-TEXT` + `INP-CAR`，选项
> 沿用 `CHILD-IDX`；仍是叶子节点，不新增 kind）；`C-RATING` / `C-PAGINATION` /
> `C-EMPTY` 的读数 / 写值（夹紧）/ 命中与各自的渲染由阶段 43 补上（`rating-*` /
> `pagination-*` + `slider-min` / `slider-max` + `cs-paint-rating` /
> `-pagination` / `-empty`，槽位沿用 `VALUE` / `VALUE2`；仍是叶子节点，不新增
> kind；`C-PAGINATION` 从 `cs-is-container` 白名单拿掉，页码按钮是数据不是视觉
> 子节点）。阶段 43 另修掉 `cmp-slider` 没写下限真源 `CMP-VALUE3` 的真 bug。
> `C-MESSAGE` / `C-ALERT` / `C-NOTIFICATION` 的 level 读写、alert 的关闭钮、
> notification 的剩余时间条与各自的渲染由阶段 44 补上（`LV-*` + `lv-slot` +
> `msg-level` / `msg-set-level` + `alert-closable` / `-set-closable` /
> `alert-close-w` / `alert-close-x` / `alert-hit` + `ntf-timeout` / `ntf-remain` /
> `ntf-set-timeout` / `ntf-set-remain` / `ntf-ratio` + `cs-level-color` /
> `cs-level-glyph` + `cs-paint-message` / `-alert` / `-notification`，槽位沿用
> `VALUE` / `VALUE2` / `VALUE3`；仍是叶子节点，不新增 kind）。
> `C-BUBBLE` / `C-SPEECH` 的 position / lang 读写与各自的渲染由阶段 45 补上
> （`bubble-pos` / `bubble-set-pos` + `speech-lang` / `speech-set-lang` +
> `cs-paint-bubble` / `-speech` + `cs-bubble-tail-x`，槽位沿用 `VALUE` / `TEXT` /
> `TEXT2`；仍是叶子节点，不新增 kind）。
> 实现中踩到的两个编译器坑记在 `ARCHITECTURE.md §5.2`；命令式复合组件的
> 实测坑记在 `§5.27`–`§5.37`。

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
DEMO []i64 = with-len(18)   ; 阶段23 起多一个 D-SCROLL 槽
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

## 声明式滚动（阶段23）

前面所有声明式几何都是「应用算好写进状态」：滑杆的值、标签页的页码。滚动
不太一样 —— 偏移是**视口和内容**的函数，而内容高只有布局才知道。做法是让
布局把「能滚多少」算出来（`cmp-scroll-max`），把「滚了多少」留给状态槽：

```nolang
DEMO []i64 = with-len(18)
D-SCROLL = 17          ; 滚动偏移：真源。0 .. 内容高 - 视口高
DEMO-SCROLL-STEP = 400 ; 滚轮每格 4px
DEMO-SCROLL-VIEW = 16000
...
; 视口高是布局数据（dv-size），偏移是状态数据（dv-scroll 读 S[offslot]）
dsc = dv-scroll(DEMO, 55, D-SCROLL, DEMO-SCROLL-STEP, rc)
dv-size(dsc, 0, DEMO-SCROLL-VIEW)
dv-repeat-items(dsc, 93, DEMO-SCROLL-N, C-LABEL, DEMO-SCROLL, ...)
```

- `dv-scroll(S, key, offslot, step, parent)` 建出 `C-SCROLL`：`value` =
  `S[offslot]`（布局据此把内容整体上移），`value2` = `step`（滚轮每格步长），
  并挂 `DV-A-CHANGE(offslot)` —— 动作码复用，但**几何不走 `dv-app-click`**
  （那里只认 `C-SLIDER`），而是走装配层的 `dva-wheel`。
- 布局（`cs-layout` 的 `C-SCROLL` 分支）跑两遍：先按子节点自身高度累加出
  内容高，夹紧 `off` 到 `[0, 内容高 - 视口高]` 并写进 `CMP-SCROLL-MAX`；
  再按夹紧后的 `off` 排版（子节点不拉伸，保持 intrinsic 高）。**夹紧不写回
  状态** —— 偏移的真源是状态槽，布局只是「按当前偏移排版」。
- 滚轮事件是新的事件类型 `type = 5`：x/y 槽放**光标位置**（命中要用），
  有符号增量放 bit56..63（`integer_y`，SDL3 ≥ 3.2.12 的整格 tick）。增量不能
  走 24 位坐标槽 —— 负数会被掩码吃掉。
- `dva-wheel(S, root)`：`cs-hit` 拿命中，顺着父链（`cmp-parent-of`）向外找
  最近的 `C-SCROLL`，读它的 `DV-A-CHANGE` 载荷当偏移槽，`nv = clamp(cur +
  dy * step, 0, cmp-scroll-max)`，变了才 `dva-set`。嵌套滚动天然只有最内层
  响应。
- 绘制层：`C-SCROLL` 自己发一条背景矩形，然后把裁剪矩形推进 `CS-CLIP`，
  子节点（含叠加层）逐条发指令时都带上裁剪（`cs-emit-clip` →
  `_noui-clip`），出视口的内容根本不画；画完恢复。
- 命中层：`cs-hit` 对 `C-SCROLL` 做包装 —— 视口外的点直接不命中（与裁剪
  一致），视口内的点按全局坐标直接走 `cs-hit-inner`（阶段24 修正：子节点
  矩形本身已带 `-off` 偏移，原来多换算了一次 `py + off`）。嵌套滚动靠递归
  各自判视口矩形。
- 于是「滚到哪了」也进了 `UI = f(state)`：demo 页头那行 `scrl=N` 就是当前
  偏移，reset 会把它写回 0。

纯逻辑测试（33 项断言，无窗口）：
```sh
../bin/no run -cc ./ccwrap.sh tests/dva-scroll.no   # ALL DVA-SCROLL TESTS PASSED
# M 标记（kind = C-SCROLL；value = S[offslot]；value2 = step；
#    DV-A-CHANGE 载荷 = 偏移槽；flags 带 VIS|EN；改状态重建 ops = 0
#    且 value 跟着变成 2500 —— 声明式闭环）
# L 布局（off = 0 时第一行在 ROW-Y0；off 生效后行按 -off 上移、行距不变；
#    cmp-scroll-max = 内容高 - 视口高；状态写超界 -> 布局夹紧到 max 但
#    **不写回状态**（槽里仍是 99999）；负数同样夹紧回 0 且不写回）
# P 绘制（滚动子孙的 Scene 指令带裁剪标记，裁剪矩形 = 滚动视口；
#    滚动外面的兄弟节点指令不带裁剪）
# H 命中（视口内命中换算到内容坐标 -> 命中正确的行；视口下 / 左外不命中；
#    从整页根命中也顺父链走到同一行；偏移改变后命中目标跟着变）
# W 滚轮（滚一格 -> 槽 = step；上界夹紧到 max；已到底不动；
#    下界夹紧到 0；已在顶不动；落在滚动外的兄弟上不动；
#    命中落在滚动容器自身的空白处也认（父链第一站就是它）；
#    改偏移是纯状态变更：重建 ops = 0，布局后行位置真的变了）
```

## 声明式虚拟列表（阶段24）

滚动解决了「内容比视口高」，但内容本身还是全挂在活树上的：10 行没问题，
10000 行就有 10000 个活节点 + 10000 次布局 + 10000 条绘制指令。虚拟列表要
求「只有看得见的行才存在」，而可见窗口是**偏移的函数** —— 所以声明式的做法
是：应用把内容高和行距**声明**出来，布局负责算窗口，视图只建窗口那几行。

```nolang
; 声明内容高（0 = 不声明 -> 布局照老规矩实测子节点高度）
dsc = dv-scroll(ST, 500, D-OFF, 400, page)
dv-size(dsc, 0, 20000)          ; 视口高 200px
dv-scroll-content(dsc, 117600)  ; 声明内容高 = 20 行 * 50px + 19 个间隙 + 上下 padding
dv-scroll-start(dsc, 0)         ; 首个挂载子的绝对行号（0 = 顺序排）
dv-repeat-at(dsc, 520, 0, 20, C-LABEL, TXTS, VALS, ACTS, PAYS, DV-EMPTY-F, 5000)
```

- **声明内容高**：`dv-scroll-content(h, ch)` / `dv-scroll-start(h, i0)` 写在
  视图节点上（`DV-SCROLL-CONTENT` / `DV-SCROLL-START` 池），`dv-apply-props`
  调和时同步到活节点的 `CMP-SCROLL-CONTENT` / `CMP-SCROLL-START`（0 表示
  没声明，两条同步都是幂等的，普通滚动不受影响）。布局的 `C-SCROLL` 分支
  因此在 `decl > 0` 时走第二套：内容高直接用 `ch`（不再累加子节点），
  `off` 夹到 `[0, ch - 视口高]`；排版时第 j 个儿子排在
  `y + pad - off + (i0 + j) * (行高 + gap)` 上 —— **绝对行号**是关键，
  否则滚到底时窗口里那几行会被当成第 0..4 行排在视口顶。
- **切片重复原语** `dv-repeat-at(parent, key0, start, count, ...)`：
  `dv-repeat-items` 的切片版，只建 `start .. start+count-1` 这 `count` 项，
  行 key = `key0 + 绝对行号`。因为 key 是绝对行号，窗口从 0..4 滑到 1..5 时
  复用的是同一批组件（`dv-sync` 按 `(key, kind)` 复用），只改文案 —— 零结构
  操作。`rowh > 0` 时给每行 `dv-size(h, 0, rowh)`，让布局实测的行距与窗口
  公式用的行距一致（否则两套行距会打架）。
- **一行式** `dv-list-view(S, key, offslot, step, viewh, rowh, n, texts, vals,
  acts, pays, key0, parent)`：把窗口计算（`i0` / `i1` 两个夹紧公式）和上面
  四步打包成一个原语，应用只需要给数据数组和行高。
- **命中修正**：`cs-hit` 对 `C-SCROLL` 的包装原来在视口判断之后把 `off` 又加
  到 `py` 上，而子节点矩形本身已经是带 `-off` 的全局坐标 —— 大偏移时视口内
  无论点哪都不命中。现在改成：视口外的点不命中，视口内的点按全局坐标直接
  走 `cs-hit-inner`（嵌套滚动各自判自己的视口矩形）。

纯逻辑测试（36 项断言，无窗口）：
```sh
../bin/no run -cc ./ccwrap.sh tests/dva-virtual.no   # ALL DVA-VIRTUAL TESTS PASSED
# M 标记（C-SCROLL 的 kind / value = S[offslot] / value2 = step /
#    DV-A-CHANGE 载荷 / flags；声明内容高同时出现在视图节点和活节点上；
#    改状态重建 ops = 0）
# L 布局（cmp-scroll-max = 声明内容高 - 视口高 = 97600；off = 0 与 off = max
#    时只挂可见的 4 行（越界夹紧后剩 1 行）；行文案正确、行距 = 行高 + CS-GAP；
#    滚到底窗口是最后 4 行（16..19）、首行 y = -2400；
#    状态写超界 -> 夹紧，首行 y = 15000 且不写回状态）
# P 绘制（滚动子孙的指令仍带裁剪矩形 = 滚动视口）
# H 命中（off = 0 与 off = max 时视口内的点都命中正确的行）
# W 滚轮（滚 20 格 -> 槽 = 8000，窗口滑到第 1..4 行、首行 y = 200；
#    窗口滑动复用同一批 key；两端夹紧；改偏移重建 ops = 0）
# R 切片（根下直接子 = 5 个 = 5 行（无容器，直接挂根）；文案 v18 / v19 / ''
#    （越界补空）；行 key = 718..722 = key0(700) + 绝对行号 18..22）
```

## 声明式过滤列表（阶段25）

虚拟列表让「挂多少」变成偏移的函数，但「挂哪些」还是应用手写 if。搜索框是
最典型的场景：输入一个词，列表里只剩包含它的行。声明式的做法是**把查询串也
当成状态**（动态字符串状态 `DV-BIND-T[qslot]`，`dv-bound` 读），列表内容
就是它的函数 —— 没有一行 if。

```nolang
; 查询来自动态字符串状态（dv-input + dv-bind 维护，见阶段13）
dv-bind(sinp, D-QUERY, 0)

; 命中行号表：每帧由查询算出（大小写不敏感，空查询命中全部）
n = dv-filter-count(D-QUERY, 20, TEXTS)

; 一行式：过滤 + 虚拟窗口
dsc = dv-filter-list(ST, 55, D-SCROLL, 400, 16000, 2800,
    20, TEXTS, VALS, ACTS, PAYS, D-QUERY, 93, rc)
```

- **过滤原语**：`dv-filter-map(qslot, n, texts, idxs)` 把命中行号写进 `idxs`
  并返回命中数（`str.to-lower` 两边都转小写再 `str.index`；`''.index('') == 0`
  所以空查询天然命中全部）；`dv-filter-count` 是只要数量不要表的便捷版。
- **按下标表取行**：`dv-repeat-mapped(parent, key0, start, count, kind, idxs,
  texts, ...)` 是 `dv-repeat-at` 的映射版 —— 挂的是 `idxs[start .. start+count)`
  这些**源行**，行 key = `key0 + 源行号`。因为 key 是源行号，查询变化（换一批
  源行）和窗口滑动（换一段下标）都按源行号复用组件；越界 / 负数下标夹到
  `[0, m-1]`，`count` 超出表长时只挂到表尾（否则会建出一堆 key 相同的行，
  `dv-sync` 按 `(key, kind)` 复用会互相覆盖）。
- **一行式** `dv-filter-list(...)`：`dv-list-view` 的过滤版，多一个 `qslot`
  参数；内容高 = `命中数 * rowh + (命中数 - 1) * CS-GAP + 2 * CS-PAD`，
  命中 0 行时内容高就是 0。窗口计算抽成了 `dv-win-range(off, viewh, rowh, n,
  out)`（结果写进 `out[0]` / `out[1]`，因为形参不能回写），`dv-list-view`
  也改用它 —— 两套窗口公式只有一个真源。
- **空列表的布局修正**：`cs-layout` 的 `C-SCROLL` 分支原来要求 `n > 0`
  （有子节点）才进去，命中 0 行时整个分支被跳过，`cmp-scroll-max` 留着上一
  次的旧值 —— 滚轮会按旧上限夹紧，清空查询后列表还会停在旧偏移上。现在
  `n == 0` 也进来（内容高 = 上下 padding，`maxoff` 夹到 0），第二遍排版仍在
  `decl > 0 && n > 0` 时跳过。

纯逻辑测试（46 项断言，无窗口）：
```sh
../bin/no run -cc ./ccwrap.sh tests/dva-filter.no   # ALL DVA-FILTER TESTS PASSED
# M 标记（dv-filter-list 建出 C-SCROLL 的 kind / value = S[offslot] / value2 = step /
#    DV-A-CHANGE 载荷 / flags；声明内容高同时出现在视图节点和活节点上）
# F 过滤（空查询 = 全量 20 行；'alpha' 命中 5 行、'ALPHA' 同样 5 行（大小写不敏感）；
#    'eta' 命中 beta 5..9；'1' 命中 11 行（alpha 1 + gamma 10..14 + delta 15..19）；
#    'zzz' 命中 0 行 -> 挂 0 行、内容高 0、maxoff 0；
#    'gamma 1' 时窗口第 0 行是源第 10 行，value = 10（值按源下标取，不是窗口位置））
# L 布局（内容高随命中数变 = 5*5000 + 4*800 + 2*1200 = 30600、maxoff = 10600；
#    行位 = 窗口序号 * 行距（首行 y = 2400，次行 2400 + 5800）；
#    滚到底窗口滑到源 11..14、首行 y = -2400、末行 y = 15000；
#    状态写超界 -> 夹紧，首行 y = 15000 且不写回状态）
# P 绘制（可见行带裁剪矩形 = 滚动视口）
# H 命中（off = 0 时点源第 6 行、off = max 时点源第 8 行，都命中正确的行；
#    视口外不命中）
# W 滚轮（滚 20 格 -> 槽 = 8000，窗口滑到源 6..9、首行 y = 200；
#    同一批行 -> 组件对象复用（ops = 0）；两端夹紧）
# R 映射（dv-repeat-mapped 按下标表 [3,1,0] 取行 -> key 703/701/700、
#    文案 alpha 3 / alpha 1 / alpha 0；表长 2 而 count 5 -> 只挂 2 行、
#    key 819/818）
```

## 声明式排序与分组（阶段26）

阶段 24/25 把「挂多少」「挂哪些」都变成了状态（偏移、查询）的函数，但「**按什么
顺序显示**」还是应用手写 if 挑行重排。现在顺序也是状态：一个 mode 槽
（0 源序 / 1 键升 / 2 键降 / 3 分组 / 4 分组降）驱动整条纯函数链。

```nolang
; 排序键 / 组号 / 组名（组名只在分组模式下用）
dv-order-list(ST, 55, D-SCROLL, 400, 16000, 2800,
    20, KEYS, GRPS, GNAMES, TEXTS, VALS, ACTS, PAYS,
    D-SORT, 93, 9000, rc)
;                 ↑ 行 key 基数 93 + 源行号；标题 key 9000 + 组号
```

- **稳定排序**：`dv-sort-map(mode, n, keys)` 对 `keys` 做**稳定**插入排序，
  返回下标表（不是把数组原地打乱）—— 同键的行保持源序，所以「键升」在键相同
  时就是源序。`n` 夹到 `keys.len()`、负数当 0；mode 只认 1/2，其它值（含 0）
  返回源序下标表。`dv-sort-run` 是它下面的原地插入排序（复用给分组）。
- **混合行表**：`dv-group-plan(on, n, grps, plan, out)` 把排好的下标表变成
  「行 + 组标题」的混合表：组号变化时写一个 token `-(g) - 1`（负数 = 标题），
  否则写行下标。所以**组顺序跟着排好的表走**（键升时先出现哪组，哪组就在
  前面），而不是源序。`on == 0` 直接复制 plan（源序模式不产生标题）。
- **按混合行表挂行**：`dv-repeat-plan(parent, key0, hkey0, start, count, kind,
  plan, texts, vals, acts, pays, flags, names, icolor, hcolor, rowh)` —— token
  < 0 挂一个标题行（key = `hkey0 + 组号`，文案 `names[组号]`，禁用且用
  `hcolor`），token ≥ 0 挂数据行（key = `key0 + 源行号`）。行数按**混合行表
  长度**夹（分组时窗口行数会超过数据行数，用 `texts` 长度夹会把标题切掉）。
- **一行式** `dv-order-list(...)`：`dv-list-view` / `dv-filter-list` 的顺序版。
  内容高按**混合行数** `m` 算（`m * rowh + (m - 1) * CS-GAP + 2 * CS-PAD`），
  所以分组后「能滚多远」也跟着变大；窗口仍由 `dv-win-range` 在混合行表里取。
  偏移的真源仍是状态槽（写回在 `dva-wheel`），超界偏移由布局夹紧但**不写回**。

纯逻辑测试（43 项断言，无窗口）：
```sh
../bin/no run -cc ./ccwrap.sh tests/dva-order.no   # ALL DVA-ORDER TESTS PASSED
# S 排序（dv-sort-map：源序 / 键升 [0,7,4,1,8,5,2,9,6,3] / 键降；稳定 —— 同键保持
#    源序；n 夹到 keys.len；负数 n -> 空表；非法 mode 7 -> 源序）
# G 分组（dv-group-plan：on=0 复制；分组 token [-1,5,6,7,8,9,-2,0,1,2,3,4]；
#    组序跟着 plan 走；空 plan；n=0；plan 短于 n）
# L 布局（行数 / 文案 / key；内容高与 maxoff 随模式变（源序 37600/21600，
#    分组 44800/28800）；mode 1/2/3/4/9 的窗口；标题文案与 key；标题禁用 +
#    主题色、数据行可用；行位 y = 2400 / 6000；换模式按源行号复用组件；
#    分组滚到近底 -> 6 行 [组1标题, 行0..4]；状态写超界 -> 夹紧到 maxoff 且不写回，
#    窗口只剩混合表最后一行）
# P 绘制（行带裁剪矩形）
# H 命中（点数据行 -> DV-A-CLICK 载荷 = 源行号；点标题 -> (0,0)；视口外不命中）
# W 滚轮（滚 20 格 -> 偏移 8000、窗口滑到 [6,7,8,9,-2]；两端夹到 maxoff；
#    换模式后上限跟着变）
```

## 声明式表单校验（阶段27）

阶段 24/25/26 把「挂多少」「挂哪些」「按什么顺序」都变成了状态的函数，但
「**这个字段合不合法**」仍是应用手写的 if-else 链，错误文案也硬编码在事件回调
里。现在校验规则也是数据：一张**扁平规则表**（`kinds` / `pars` 两个平行数组，
按 `base + n` 切片），每个字段「怎样算不合法」就是表里的一段。

```nolang
; 四个字段的规则表：kinds / pars 平行，按 base + n 切片取一段
;   name    槽2  必填 + 最短 2        base 0 n 2
;   age     槽3  必填 + 18..99        base 2 n 3
;   code    槽4  必填 + 含 'X'        base 5 n 2
;   confirm 槽5  必须等于 name 槽     base 7 n 1
vf = dv-form(310, lc)
dv-field(200, 400, D-VN, 'name', 'your name', vsh, V-SPARS, V-KINDS, V-PARS, 0, 2, V-MSGS, vf)
dv-field(210, 410, D-VA, 'age', '18-99', vsh, V-SPARS, V-KINDS, V-PARS, 2, 3, V-MSGS, vf)
dv-field(220, 420, D-VC, 'code', 'contains X', vsh, V-SPARS, V-KINDS, V-PARS, 5, 2, V-MSGS, vf)
dv-field(230, 430, D-VF, 'confirm', 'same as name', vsh, V-SPARS, V-KINDS, V-PARS, 7, 1, V-MSGS, vf)
;                 ↑ label key / error key / 字符串槽 / 文案表 / 规则切片(base, n) / 父
```

- **规则种类**（`DV-R-*`）：`1` 必填（trim 后非空）、`2` 最小长度、`3` 最大长度、
  `4` 最小值、`5` 最大值、`6` 与另一槽相等、`7` 包含子串。长度类先看
  `v.trim()`；数值类先 `to-i64()`，**解析失败算违反**（年龄填 `abc` 不会静默通过）；
  `EQ` 的 `par` 是**另一个字符串槽号**（不是常量），所以「确认密码」这类规则是
  纯粹的数据；`HAS` 的 `par` 是 `spars` 下标，取子串常量。
- **单字段检查**：`dv-rule-check(v, spars, kinds, pars, base, n)` 返回**第一条**
  违反的规则号（1 基），全过返回 0。`base`/`n` 越界一律按「没有这条规则」吞掉
  （下标解包失败 → `k = 0` → 不匹配任何种类）。
- **规则号 → 文案**：`dv-rule-msg(code, msgs)`（0 / 越界返回空串）。文案挂在
  **规则**上，不是字段上 —— 同一种规则在不同字段可以写不同措辞。
- **关键设计：返回全局规则下标 +1**，不是切片内序号。`msgs` 是一张逐条规则的
  扁平表，字段只是 `base + n` 的一个切片；返回全局下标后 `dv-field` 拿到号
  直接 `dv-rule-msg(c, msgs)` 就能查到那条规则的措辞，不必再换算回切片内序号。
- **多字段汇总**：`dv-form-bad(slots, bases, ns, spars, kinds, pars, nf)` 返回
  「坏了几项」。`nf` 超出表长按表长算（越界解包 → 空规则 → 不算坏）。
- **一行字段** `dv-field(...)`：`dv-label(key, label)` + `dv-input(key+1, ph)` +
  `dv-bind(输入, slot, 0)`，然后**条件建错误行** —— 只有「本字段有违反 &&
  `show != 0`」才 `dv-label(errkey, 文案, parent)` + `dv-variant(el, dv-var-danger())`
  （变体 → 主题 DANGER 色）。`show` 是**揭示槽**：提交失败时应用层把它写成 1，
  于是「显示哪些错误」也是状态的函数；首次进页面 `show = 0`，只禁用提交、
  不显示红字。key 三者（label / input / error）必须互不冲突，撞了会被
  `dv-sync` 按 `(key, kind)` 互相覆盖。

纯逻辑测试（46 项断言，无窗口）：
```sh
../bin/no run -cc ./ccwrap.sh tests/dva-valid.no   # ALL DVA-VALID TESTS PASSED
# V 规则（必填 / 最短 / 最长 / 最小值 / 最大值含不可解析 / 与另一槽相等 / 含子串；
#    trim 生效；多规则取第一条；base+n 切片；n=0；越界 base 吞掉；未知种类忽略）
# F 汇总（全过 / 坏 1 / 坏 2 / 全坏；nf=0；nf 超表长按表长；空规则字段不算坏）
# L 视图（揭示槽 0 -> 错误行不存在；揭示后出现且文案正确 + 危险变体；合法时不建；
#    输入确实绑到字符串槽；提交按钮 flags = f(坏项数)；改值重建同一输入 key 复用
#    同一组件对象；同状态重复 render 零操作）
# P 绘制（揭示后错误文案真的画出来；未揭示时场景里没有它）
# H 命中（禁用提交被命中也不出动作；合法时提交 = DELTA(D-SUB)）
```

demo 侧：`tests/main-dv.no` 左栏原本是普通 `C-FORM`，装不下新增的四个字段，
所以改成 `dv-scroll` 滚动容器（`lc = dv-scroll(DEMO, 91, D-LSCROLL, 400, cols)`，
`dv-size(lc, 30000, 0)`），校验表单挂在里面；「提交」是显式命令
（`DEMO-CMD-SUBMIT`，和 `reset` 同套路 —— 归约器表达不了「一次做完两件事」）：
不合格写 `D-VSHOW = 1`，合格则 `D-VSUB + 1` + 触发通知。页头回显
`vsub=` / `lsc=`（后者顺带提示左栏现在能滚）。首帧 `ops` 65 → 76
（1 个表单容器 + 4 标签 + 4 输入 + 1 状态标签 + 1 提交按钮 + 1 个滚动容器
− 原左栏表单容器）。

## 声明式虚拟表格（阶段28）

阶段 24/25/26 把「挂多少 / 挂哪些 / 按什么顺序」都变成了状态的函数，但一行仍
只有一个标签 —— 真实的表格（一行 N 列 + 表头）还手写不出来。现在它也是数据：
**列宽逐列声明**、**表宽自己算**、**表头挂在滚动容器之外**（所以不滚）、
**容器里只挂可见窗口那几行**。

```nolang
; 两列表格：表头 name/key 不滚，容器里只挂可见窗口的行，每行 2 个单元格
tplan = dv-demo-order-plan(DEMO)          ; mode 槽 -> 混合行表（阶段26 那条链）
dsc = dv-table-list(DEMO, 55, 920, D-SCROLL, DEMO-SCROLL-STEP, DEMO-SCROLL-VIEW,
    DEMO-SCROLL-ROWH, tplan, 2, DEMO-TABLE-COLW, DEMO-TABLE-HEADERS, DEMO-SCROLL-GNAME,
    DEMO-TABLE-CELLS, DEMO-TABLE-ACTS, DEMO-TABLE-PAYS, 93, 900, 9000, CS-TH-MUTED, rc)
;        ↑ 滚动 key / 表头行 key / 偏移槽 / 步长 / 视口高 / 行高 / 行表 / 列数
;          列宽表 / 表头文案 / 组名 / 行主序单元格 / 动作 / 载荷
;          行 key 基数 93 / 单元格 key 基数 900 / 组标题 key 基数 9000 / 父
```

- **行 = `C-GRID`**：`dv-table-rows` 把窗口内的混合行表逐行挂成 `C-GRID`
  （宽 = 表宽 `tw`、高 = `rowh`），每行 `ncols` 个 `C-LABEL` 单元格
  （`key = ckey0 + 源行号*ncols + 列号`），`colws[c] > 0` 时给该列定宽；
  行的动作 / 载荷按**源行号**取（`acts[idx]` / `pays[idx]`）。
- **通栏组标题**：`token < 0` 时挂的是**一个**占满表宽、`dv-disabled()` +
  `hcolor` 的标签（`key = hkey0 + 组号`、文案 `names[组号]`），不是 N 列 ——
  所以分组表格里「组标题 + 组内数据行」的层次是声明出来的，不是画出来的。
- **宽度是声明的，不是实测的**：`tw = 2*CS-PAD + 列宽和 + (ncols-1)*CS-GAP`
  由 `dv-table-list` 自己算。这一点很关键 —— 可见窗口只有几行，若让 C-GRID
  按内容实测，列宽会随「当前窗口里恰好是哪些行」浮动（长文案行撑宽、短文案行
  缩回），表格就会一边滚一边抖。
- **表头不滚**：表头是 `parent` 的**前一个孩子**（`C-GRID`，与滚动容器同级），
  行高声明为 `CS-PAD + CS-LINE-H`（C-GRID 交叉轴默认顶对齐，标签正好落在
  `y+pad .. y+pad+行高`）—— 比让它自动量（`2*pad + 行高`）省一行边距，
  在 420px 高的窗口里这点预算就是「order 下拉还在不在屏上」的差别。
- **越界不兜底**：`start` 先夹进 `[0, plan.len()]`，`count` 再按「剩余可挂行数」
  夹。两端都越界时**一行都不挂**（不退回第 0 行、不建假行）—— 空表就是空表。
- **`ncols = 1` 且不分组 ≡ `dv-order-list`**：同一个 `plan` 语义、同一套
  `dv-sort-map` / `dv-group-plan`，所以阶段 26 的五档排序 / 分组原样复用，
  demo 主循环一行没改。

纯逻辑测试（32 项断言，无窗口）：
```sh
../bin/no run -cc ./ccwrap.sh tests/dva-table.no   # ALL DVA-TABLE TESTS PASSED
# R 行（3 行 2 列：行 kind = C-GRID、每行 2 个单元格、文案 = cells[源行号*列数+列号]、
#    行宽 = 表宽、行高 = rowh、列宽 = colws；乱序 plan 的行号映射；通栏组标题
#    （文案 / 禁用 / 占满表宽）；count 夹到混合表长；start 越界 -> 一行不挂）
# L 列表（表头两个单元格 + 滚动容器；声明内容高 20 行 -> 73600、maxoff 63600；
#    分组 22 行 -> 80800 / 70800；窗口切片；状态驱动偏移；换偏移复用同一批行；
#    同状态重复 render 零操作；空 plan -> 内容高 0 / maxoff 0 / 0 行）
# P 绘制（行带裁剪矩形 = 滚动视口）
# H 命中（点单元格 -> 命中该行动作；派发 = DV-A-SET + 载荷）
```

demo 侧：`tests/main-dv.no` 右栏那条 20 行列表升级成两列表格（`name` 列是原来的
行文案、`key` 列是键的文本形式），上方「order」下拉原样驱动五档模式，分组模式下
多出来的组标题变成**通栏行**。左栏让宽（`dv-size(lc, 30000, 0)` → `22000`），
右栏视口 160px → 112px —— 给表头（36px）与「order」下拉（40px）让位，
否则窗口 420px 高装不下，下拉会被顶出屏外。首帧 `ops` 76 → 83。

## 声明式可拆面板（阶段29）

阶段 23 的滚动把「空间」写进了状态，但只能沿一个轴滚；面板的**分栏位置**（左屏
多宽）同样是空间，同样该由状态说了算。命令式内核里 `C-DOCK` 已经有了 kind 与构造
器，但 `cs-is-container` 之外它没有任何分栏语义 —— 哪条缝能拖、拖到哪、夹在哪，
全都没声明。现在这些也是数据。

```nolang
; 两栏：中间那条 8px 的缝就是拖杆。左屏宽 = 状态槽，右屏吃掉剩余宽。
cols = dv-dock-split(95, D-DOCK, 12000, 42000, 800, page)
dv-lay(cols, dv-lay-h-grow(), 800, 0)        ; 横向 + 吃掉页头以下剩余高；间隙 8px
lc = dv-scroll(DEMO, 91, D-LSCROLL, 400, cols)
dv-size(lc, dk, 0)                           ; dk = DEMO[D-DOCK]，左屏宽的真源
rc = dv-fill(92, cols)                       ; 右屏（dv-fill 自带 L-GROW）
```

- **间隙就是拖杆**：两块面板之间的 `gap` 不属于任何子节点，`cs-hit` 的「自身矩形
  兜底」会把它算到容器头上 —— 所以拖杆的命中区就是那条缝，**不需要新增节点种类、
  也不需要给布局引擎加分支**。`C-DOCK` 只是一台通了电的横向容器。
- **命中要落在缝里才算**：`dva-drag-down` 命中的是容器之后，还要按「第一块面板的
  右边界 .. +gap」判一次（横向比 `px`、竖向比 `py`）。容器的内边距虽然也命中容器，
  但不算拖杆 —— 否则点一下空白就开始拖。
- **换算在事件侧，语义在视图上**：抓住时只记容器句柄；移动时 `nv = px - 第一块
  面板的左边`（竖向 `py - 上边`），夹到视图声明的 `[value, value2]`，写回状态槽。
  与 `dva-wheel` 完全同一个模式。
- **要鼠标抬起事件**：拖拽是「按下抓住 / 移动 / 松开放手」三段，缺了抬起就只能靠
  再次按下结束。`src/shim/shim.c` 补了 `SDL_EVENT_MOUSE_BUTTON_UP` → type 6
  （坐标槽与按下一致），`app-poll` 的 `t = e & 0xFF` 不用改；主循环 `t == 6` 调
  `dva-drag-up()`，`t == 2` 先试拖杆、抓住了就跳过点击路径。
- **`value2` 同步补 `C-DOCK`**：`dv-apply-props` 原来只对 `C-SCROLL` 等少数 kind
  同步 `value2`，`C-DOCK` 不加进去的话夹紧上限永远读到 0。
- **竖着拆**：`dv-lay(dock, 0, gap, pad)` 把 `L-H` 抹掉即可（`cs-is-h` 读 `CMP-LAY`
  的 `L-H` 位），状态槽改存上屏高、左屏那句 `dv-size` 改成定高。事件侧按 `cs-is-h`
  自动选轴，不用另写一份。

纯逻辑测试（31 项断言，无窗口）：
```sh
../bin/no run -cc ./ccwrap.sh tests/dva-dock.no   # ALL DVA-DOCK TESTS PASSED
# M 标记（kind = C-DOCK / L-H 位 / gap / value = min / value2 = max /
#    派发 = DV-A-CHANGE + 载荷 = slot / flags；改状态重建零操作且夹紧界不变）
# L 布局（左屏宽 = 状态 / 右屏吃掉剩余 / 间隙 = 声明值 / 两块面板同高同顶）
# H 命中（间隙命中容器 / 面板内部命中面板 / 容器内边距命中容器 / 容器外不命中）
# D 拖拽（间隙正中间抓住 / px=20000 -> 左屏宽 17600 / 下界夹到 8000 /
#    上界夹到 28000 / 松手后再移动不动 / 面板里不抓 / 容器内边距不抓）
# V 竖拆（上屏高 = 状态 / 按 py 换算 / px 甩出去不影响）
# R 重建（拖完改状态 -> 重建零结构操作 -> 左屏宽跟着变）
```

demo 侧：`tests/main-dv.no` 左右两栏从 `dv-row` 换成 `dv-dock-split`（节点数不变，
首帧 `ops` 仍是 83），页头多回显一个 `dock=`。拖那条 8px 的缝 → 写 `D-DOCK` →
下一帧 `dv-size(lc, dk, 0)` 跟着变，和 `scrl=` / `lsc=` 一样是 UI = f(state)。

## 声明式代码编辑器（阶段30）

阶段 28/29 把「列表 / 表格 / 分栏」变成了声明，但**多行文本**还只有命令式内核的
`C-INPUT`（单行）。真实的代码编辑器要四样东西：多行编辑、行号、语法高亮、光标。
这四样没有一样需要新的组件种类 —— 行是 `C-FORM`，token 是 `C-LABEL`，行号是
`C-LABEL`，光标是 `C-SEPARATOR`，外圈是现成的 `dv-scroll`。缺的只是**文本模型**
和**着色器**，而它们都是纯函数。

```nolang
; 一行 = 整个编辑器。文本槽是字符串状态（真源），偏移槽是应用状态。
ed = dv-code-editor(DEMO, DEMO-ED-KEY, D-ED-TEXT, D-ED-OFF, DEMO-ED-STEP,
    DEMO-ED-VIEW, DEMO-ED-ROW, DEMO-ED-GUTTER, DEMO-ED-KEY0, lc)
dv-size(ed, dk, DEMO-ED-VIEW)
```

- **文本模型在 dv 侧，按文本槽存**：`DV-ED-LINES`（`[]str`）是切好的行，
  `DV-EDIT-L` / `DV-EDIT-C` 是光标（行 / 列）。**索引是文本槽，不是视图节点** ——
  视图节点每帧随调和变，按节点存光标等于每帧把光标弹回行首；`dv-clear` 也只清
  `DV-EDIT`（节点 → 槽的映射），不清光标。这是实测踩出来的（见 `ARCHITECTURE.md
  §5.24`）。
- **切行要保留结尾空行**：`str.lines()` 会把 `'a\nb\n'` 切成 2 行，于是「在最后一行
  末尾按回车」进不去。`dv-edit-split` 自己数 `\n` 分段，`n = 段数`，末尾空行保留；
  `dv-edit-join` 用 `\n` 拼回去。
- **着色器是纯函数**：`dv-lex-line(line)` 扫一遍写出平行的 `DV-RUN-T` / `DV-RUN-C`
  （文案 / 颜色），`dv-run-push` 把**相邻同色**的 token 并进去（少挂节点，且跨帧
  key 更稳）。关键字查 `,kw,` 夹逼表，字符串扫到配对引号（含未闭合），注释 `;` 到
  行尾，数字连续，其余单字符。列号一律是**字节下标**（与光标同一坐标系）。
- **光标是声明出来的，不是画出来的**：光标落在哪个 token 里，那个 token 就拆成
  `前缀 + 2px 的 C-SEPARATOR + 后缀` 三个节点 —— 所以光标移动只会增删这两个标签
  （`dv-sync` 按 `(key, kind)` 复用），不重建整行。空行挂不到 token，就只挂光标。
- **窗口 / 跟随 / 内容高**：`dv-win-range` 切可见行（与虚拟列表同一公式），行 key =
  `key0 + 绝对行号`；内容高 = `n*行高 + (n-1)*间隙 + 2*内边距` 经 `dv-scroll-content`
  声明，所以滚动上限不依赖所有行都在场。`dv-edit-follow` 用 `DV-ED-SEEN[slot]`
  记住上次光标：**光标没动就不重排窗口**，滚轮把窗口推走时不会被弹回去；光标动了
  才按 `i0 = (行号-1)*行距/行距` 重算窗口并写回偏移槽。
- **键盘喂字在 dv 侧，焦点在装配层**：`dva-focus(hit)` 命中后顺 `cmp-parent-of`
  上溯，用 `dv-node-by-key` 反查视图节点、`dv-edit-of` 看它是不是 `dv-edit` 声明
  的编辑器根 —— 是就把**声明式 key** 记进 `DVA-EDIT-FOCUS[0]`（活句柄每帧会变，
  key 不变），并释放输入焦点。主循环 `t == 4` 的顺序是：输入焦点 → **编辑器焦点**
  → 全局快捷键。所以 'q' 在编辑器里打得出来，不打字时 'q' 仍然退出。

纯逻辑测试（61 项断言，无窗口）：
```sh
../bin/no run -cc ./ccwrap.sh tests/dva-editor.no   # ALL DVA-EDITOR TESTS PASSED
# T lex（18 项：空行 / 注释 / 字符串 / 数字 / 关键字 / 普通标识符 / 空白 /
#    相邻同色合并 / 引号扫描（含未闭合）/ 字节读取）
# E lines（6 项：切 / 拼往返，含 'a\nb\n' 的结尾空行、'' 与单行）
# K edit（13 项：左右 / 插字符 / 退格 / 回车 / 上下 / Tab 缩进 / 跨行退格并入上行 /
#    行首退格到顶不再吞字符 / 空文本）
# W window（3 项：dv-win-range 的 i0 / i1 / 空表）
# F follow（4 项：光标不动不重排 / 动了才重排 / 重复调用稳定 / 强制偏移不被覆盖）
# A assemble（17 项：挂载结构 / 声明内容高 / 命中上溯 / 焦点交接 / 喂字 /
#    点空白释放焦点 / 空文本不崩 / 重建零操作）
```

demo 侧：`tests/main-dv.no` 左栏底部加了这个编辑器（初始文本就是一段 noui 代码，
着色看得最清楚），新增 `D-ED-OFF = 24` 与字符串槽 `D-ED-TEXT = 6`，页头回显
`edoff=`。首帧 `ops` 83 → 105（1 个标题标签 + 编辑器 21 个节点：滚动容器 + 内容
容器 + 4 行，其中光标所在行多挂「前缀 / 光标 / 后缀」），之后 899 帧零结构操作。
点进去直接敲 —— 文本状态、光标、窗口三者都是 UI = f(state)。

## 声明式树（阶段31）

阶段 24/25 的虚拟列表 / 虚拟表格、阶段 30 的编辑器，都是「一个数组 + 可见窗口」。
树多一根轴：**层级**。层级不该由递归渲染出来（语言没有可用的递归），而该由
**数据形状**保证 —— 先序序列 + 兄弟块相邻：节点 `i` 的孩子就是下标 `kids0[i] ..
kids0[i]+kids[i]-1` 这一段。这样「拍平成可见行表」是纯迭代。

```nolang
; 一行 = 整棵树。展开槽 / 选中槽 / 偏移槽都是应用状态。
tr = dv-tree-view(ST, DEMO-TREE-KEY, D-TOFF, DEMO-TREE-STEP, DEMO-TREE-VIEW,
    DEMO-TREE-ROW, DEMO-TREE-N, DEMO-TREE-TEXTS, DEMO-TREE-K0, DEMO-TREE-KN,
    DEMO-TREE-PAR, D-TOPEN, D-TSEL, DEMO-TREE-KEY0, lc)
```

- **拍平是显式栈的先序**：`dv-tree-flat` 用 `with-len(n)` 的索引栈模拟递归 ——
  压入节点时把它的兄弟块按「逆序」入栈，展开的节点才继续下钻；顺手把每行层级
  写进 `deps`。`vec.pop()` 在 MIR 层返回 `%option_i64`，用不得，所以栈顶是
  一个 `sp` 下标。可见行表长度 `m` 可能远小于 `n`（收起时只剩根）。
- **一行三个节点**：`C-FORM`（行，`value` = 节点号，挂 `DV-A-SET(selslot)`）+
  「缩进 + 箭头」右对齐 `C-LABEL`（宽度 = `层级*缩进 + 箭头宽`，有孩子才给文案
  `v` / `>`，叶子留空但仍占位 —— 这样所有行的文本起点对齐）+ 文本 `C-LABEL`。
  选中态是**三处一起变**：行 flags 换 `dv-on-sel()`、行底色换 `CS-TH-TRACK`、
  文本色换 `CS-TH-ACCENT`。
- **箭头就是展开开关**：有孩子的箭头挂 `DV-A-TOGGLE(openslot + 节点号)`，
  点它只翻一个槽，子树整块消失 / 回来（有结构操作）；点行只换选中（零结构操作，
  活树颜色跟着状态变）。叶子箭头没有动作，`dv-dispatch` 只认自己那个 key，
  所以命中它拿到 0。
- **窗口 / 内容高 / 滚动**：和虚拟列表同一套 —— `dv-win-range(off, viewh, rowh, m)`
  切可见行，行 key = `key0 + 节点号*10000000`（**按源节点号**，不是按可见行号，
  所以滚动时同一节点复用同一批组件）；内容高 = `m*行高 + (m-1)*间隙 + 2*内边距`
  经 `dv-scroll-content` 声明，滚动上限跟着 `m` 变 —— 全部展开后 13 行全在、
  内容高变大、仍能滚。
- **FILL 是批量写**：`DV-A-FILL = 11` 把 `S[payload .. payload+value2)` 全写成
  `value`（`value2 <= 0` 不写、回执 0）。「全部展开」= `value=1, value2=13`，
  「全部收起」= `value=0, value2=13` —— 同一个动作码、两个操作数，应用层不写循环。
- **坑：模块级 `[]str` 字面量在函数体里读出来是空的**。`A []str = ['a','b']` 这种
  绑定只在被当作常量折叠时才生成固定数组常量，而树视图是函数、又要把它往下游传，
  于是 `texts.len()` 在 `dv-tree-row` 里是 0、文案全空。绕法：模块级 `[]str` 一律
  `with-len(n)` + 逐项写（`[]i64` 字面量没这个问题）。详见 `ARCHITECTURE.md §5.25`。

纯逻辑测试（42 项断言，无窗口）：
```sh
../bin/no run -cc ./ccwrap.sh tests/dva-tree.no    # ALL DVA-TREE TESTS PASSED
# Z mark（16 项：可见行表顺序 / 层级 / 行 + 箭头 + 文本结构 / 箭头宽度与文案 /
#    叶子箭头无动作仍占位 / 行动作 = SET 选中槽 / 内容高 / 起始行 /
#    选中态三处一致，含换回未选中）
# Y live（5 项：首帧 1+8*3 / 活树结构 / 换选中零结构操作且颜色跟随 /
#    收起子树才增删 / 再展开节点回来）
# D disp（10 项：箭头 TOGGLE(openslot+节点) / 行 SET(selslot) / 归约回执 /
#    叶子箭头 dispatch=0 / FILL 全展开 13 槽 / 全收起只剩根 / value2<=0 不写）
# E e2e（11 项：布局 -> cs-hit 命中行 / 箭头 -> 派发 -> 归约 -> 重建 ->
#    活树跟随 / 偏移 100px 只挂可见 5 行且首尾行号正确 / 全展开 13 行且能滚 /
#    选中行文本色）
```

demo 侧：`tests/main-dv.no` 左栏顶部加这棵树（`DEMO-TREE-*` 一组常量 + 13 个
节点的文案 / 孩子块 / 父链），下方挂 `expand all` / `collapse all` 两个幽灵按钮
（同一个 `DV-A-FILL`，`value` 一个 1 一个 0），页头回显 `tsel=` / `toff=`。
首帧 `ops` 105 → 124（1 个标题标签 + 树 8 行 × 3 + 滚动容器 + 2 个按钮），
之后 899 帧零结构操作。点箭头 / 点行 / 拖滚动条都只是改状态，下一帧视图重建。

## 声明式命令面板（阶段32）

内核的 `C-COMMAND` 是个空壳（只有节点、没有绘制分支），声明式层原本也只有一个
占位 `dv-command(key, parent)` —— 「搜索 → 过滤 → 键盘导航 → 执行」整套都不存在，
命令式用法要应用自己维护查询串 / 命中表 / 游标 / 滚动偏移，再手写键盘分流。
阶段 32 把它补成一行：

```nolang
; 一行 = 整个面板。开关槽 / 查询槽 / 偏移槽 / 命令码槽都是应用状态。
dv-command-palette(DEMO, DEMO-PAL-KEY, D-PAL-OPEN, D-PAL-Q, D-PAL-OFF, D-PAL-CMD,
    DEMO-PAL-N, DEMO-PAL-TEXTS, DEMO-PAL-ACTS, DEMO-PAL-KEY0, page)
```

- **结构**：`C-FORM` 面板根（`dv-on-overlay()` —— 不参与流布局、在窗口矩形内居中、
  命中先于其它子节点、绘制最后画）+ `C-INPUT` 查询行（`dv-bind` 到 `qslot`，
  **只显示不收键盘**）+ `C-SCROLL` 列表（`dv-lay-grow()` 吃掉面板剩余高度）。
  列表内容高按命中数 `m` 算（`m*行高 + (m-1)*间隙 + 2*内边距`），所以「能滚多远」
  跟着查询变；行 key = `key0 + 源行号*100`，**按源行号**，查询过滤后同一行复用
  同一批组件。每行 = `C-FORM` 行 + 文本 `C-LABEL`，两者都挂同一个 `DV-A-RUN`
  （`cs-hit` 返回的是最深的节点，只挂行的话「点命令名字」什么也不会发生）。
- **新动作码 `DV-A-RUN = 12`**：`S[payload] = value`（命令码）+ `S[value2] = 0`
  （关面板）。点行 = 执行 + 收起，归约器一次写两个槽，应用层不写分支。
- **面板自己收键盘**：装配层每帧在建视图**之后**调 `dv-pal-open-key(S)` 问出哪个
  面板开着，写进 `DVA-PAL-FOCUS[0]`（必须在建视图之后 —— 面板 key 是构建时才登记
  的）。键盘优先级 **面板 > 输入 > 编辑器 > 全局**，于是按 `p` 打开后直接敲字就在
  过滤，↑↓ 移动游标，回车执行游标行，ESC 关掉，退格改查询 —— 不需要先点输入框。
  `dva-pal-keyboard` 是这条链上唯一的分流处，主循环里没有 key 的 if-else。
- **焦点互斥**：`dva-focus` 先顺活父链问 `dva-pal-find(hit)` —— 命中在面板里就
  把输入 / 编辑器焦点都放掉，也不把焦点交给面板内的输入框（否则两处都在改文本）。
  面板内点击照样命中行（`dv-dispatch` 认的是行 / 文本自己的 key）。
- **跨帧的面板账**：游标 / 命中数 / 命中表抄本 / 开关沿flip 都按**面板 key** 存在
  `DV-PAL-*` 池里（`dv-clear()` 只清视图描述，这些不是视图的一部分）。
  `dv-pal-cur-key` 用「只增不减的扁平命中表 `DV-PAL-SRC` + 每面板基址 `DV-PAL-SB`」
  现算游标行的 key —— 游标移动**不重建视图**，回车必须立刻知道当前指的是哪一行。
  `WAS` 沿 flip 检测也放在「关着也跑」的那半段里，否则重开时游标还停在上次的位置。
- **坑：`dv-pad` 不存在**。内边距走 `dv-lay(box, 0, 0, cs-mul(CS-PAD, 2))`
  （`dv-lay` 的第 4 参），不是某个 `dv-pad`。
- **坑：输入框不能 `dv-lay-grow()`**。面板是竖排，输入框一 grow 就把列表挤没；
  只有滚动容器拿 `dv-lay-grow()`。
- **坑：行 key 段要避开编辑器 / 树**。demo 里编辑器占 `960` / `9700+`、树占
  `980` / `9800+`、表格占 `900+` / `9000+`，面板行 key 选 `2000 + 源行号*100`
  才不撞（撞了 `dv-sync` 会按 `(key, kind)` 复用错组件）。

纯逻辑测试（34 项断言，无窗口）：
```sh
../bin/no run -cc ./ccwrap.sh tests/dva-palette.no   # ALL DVA-PALETTE TESTS PASSED
# Z mark（14 项：关着不建子树且 open-key=-1 / 开着是模态叠加层 / 输入绑定查询槽 /
#    列表 8 行 / 行 value=命令码 + RUN(cmdslot) / value2=开关槽 / 文本 /
#    行 key 按源行号 / 游标行高亮 / 内容高 / 命中数 + 游标 + 游标行 key /
#    open-key / 关面板整棵消失）
# Y live（6 项：首帧 3+8*2 / 活树文本 / 过滤成 4 行且增删 / 游标移动零结构操作 /
#    退格回空查询 8 行全回来 / 关面板活树根消失）
# D disp（7 项：行 dispatch=RUN(cmdslot) / apply 写命令码 + 清开关槽回执 2 /
#    过滤 'sa' 2 行 / 过滤 'z' 0 行且游标 -1 / 空命中移动不动 / 追加 / 退格）
# E e2e（7 项：布局 -> cs-hit 命中行（或其父）/ 派发 -> 归约 -> 活树 /
#    面板焦点下 ↓ -> 回车执行游标行 -> 命令码 + 关面板 + 计数 /
#    可打印字符进查询串 / ESC 关面板 / 命中面板内输入框也算在面板里）
```

demo 侧：`tests/main-dv.no` 左栏树下面加一个主色 `palette` 按钮
（`DV-A-SET(D-PAL-OPEN)` + 快捷键 `p`），页头回显 `pal=`，reset 多写
`D-PAL-OPEN = 0` / `D-PAL-CMD = 0` + 清空 `D-PAL-Q` + `dv-pal-reset`。执行是
显式命令（`pay == D-PAL-CMD`）：归约表达不了「顺带弹个通知」，所以补一条
`D-TOAST = dv_frame()`。新增 `D-PAL-OPEN = 40` / `D-PAL-OFF = 41` /
`D-PAL-CMD = 42`（`with-len(44)`）与字符串槽 `D-PAL-Q = 7`（`dv-bind-t-ensure(8)`）。
首帧 `ops` 124 → 125（多的那 1 个是 `palette` 触发按钮；面板关着时只登记 key、
不建子树），把面板打开跑一遍是 144（再多 19 个节点 = 面板根 + 输入 + 列表 +
8 行 × 2）。

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
- `with-len` 类 call-init 只有大写名才注册为 module global：小写名即使在纯 main 内 `push` 也会失败（`vec.push: needs receiver and element`）。共享数据一律大写。
  2026-11-22 补：这条判定以前只看「初值是不是 call」和「容器类型」，**标量**（`ED = cmp-editor()`）
  和**块内重赋**（`ED = 0` + init 块里 `ED = ...`）都会被降级成合成 main 的局部，
  函数读到 `undef`（`cmp-editor-sync` 拿到 undef 句柄 → `kids=0`）。已在
  `src/mir/hir2mir.go` 统一成「有嵌套写**或**名字大写即模块存储」，
  回归见 `src/mir/global_scalar_ref_test.go`。
- 不支持 vec-of-struct → 用 SoA 平行数组。
- 字符串用单引号；插值里的复杂表达式先算成局部变量。
- `i = i + 1` 的溢出注解在循环内不稳 → 用 match 解包 option 或区间迭代。
- ✅ **i64 字面量写进 f64 字段已修**（2026-11-22）：`r rect; r.x = 10` 以前生成非法 IR
  （`store double %i64局部, double* %全局`），clang 报
  `defined with type 'i64' but expected 'double'`，链接期才炸，所以库侧一直被迫
  **写字面量就带小数点**（`r.x = 10.0`；函数实参 `rect-hit(r, 50.0, 30.0)` 同理）。
  根因是 `emitSetField` 按字段声明推存储类型、值却保留 RHS 类型，整数分支只在两边都是
  整数时才触发。现已补 int→float 的 `sitofp`，回归见 `src/mir/f64_field_store_test.go`；
  **新代码不必再带 `.0`**（老写法继续合法）。
- 🔴 **跨模块同名全局会真遮蔽，且随 `#` 导入顺序变**：`src/dv.no` 的 `TR-OPEN = 'v'`
  与 `src/components.no` 的池 `TR-OPEN []i64` 曾同名 —— 同时导入两者时，`TR-OPEN`
  指向谁取决于写 `#` 的顺序（实测 `[]` vs `[118]`）。新增 `TR-*` / `CP-*` 这类
  前缀名之前先 `grep -rn "前缀-" src/`。现已把 dv 侧改成 `TR-ARROW-OPEN`。
- 🔴 `(cond) { }` 是条件循环（while），`(cond) -> { }` 才是一次性 guard。旧内核的
  「死循环」根因是 3 处 guard 误写成循环（`el-add` + `scene-dump` 两处），
  已全部改为 `-> {}`，大写版 `tests/kernel2.no` 复活并输出 `elements: 2`。
  收尾修遗留草稿时又栽两次：`src/entity.no` 的 `entity-emit` 与
  `tests/test-kernel.no` 的 `el-add`（共 3 处），都是条件恒真 → 函数永不返回，
  `timeout` 才拿得住。
- 🔴 **两套句柄 / 标志位空间别混**：声明式视图池（`DV-*`，`dv-kind` / `dv-flags` …）
  与活组件池（`CMP-*`，`cmp-kind` / `cmp-flags` …）是**两套下标**。`dv-live-deep`
  返回的是 **CMP** 句柄，所以活节点的 flags 必须读 `cmp-flags(h)` + `F-VISIBLE`(1) /
  `F-ENABLED`(2) / `F-DISABLED`(64)；拿 `dv-flags(cmp句柄)` 会**静默读到无关的
  视图节点**（返回 0，看着像「节点不可见」）。视图 flags 用 `DV-F-VIS`(1) /
  `DV-F-EN`(2) / `DV-F-CHK`(4) / `DV-F-SEL`(8) / `DV-F-ACT`(16) / `DV-F-OPEN`(32) /
  `DV-F-OVL`(64) / `DV-F-PASS`(128)，经 `dv-apply-flags` 映射到活节点。
- `print` / `format` 的格式字段只接受**简单标识符**：`{dv-arr-get(a,0)}`、
  `{cmp-text(h)}`、`{cs-rect(h).1}` 都是编译错误（`unresolved format field`）。
  先把结果赋给局部变量再插值。
- `->` 链有三种语义，别凭直觉写：`{}` 里全是 `cond -> body` 才是**短路 if-elif-else**；
  函数 / 循环体里**裸写**的 `cond -> body` 是**独立 if**（每个都会检查）；`{}` 之外
  另起一行的 `->` else 臂会折到最近的裸 if 上（编译错误）。
- 已声明的局部可以**重新赋值函数返回值**（`a = dv-sort-map(...)` 合法）；但
  `/`、`%` 是 fallible（返回 option），裸写要解包 —— 库里的排序 / 分组绕开了除法。

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
| `src/shim/shim.c` `.h` | SDL_Render + SDL_ttf(HarfBuzz) 字体后端；阶段23 加 `noui_clip` / `noui_clip_clear`（SDL_RenderSetClipRect，`w<=0` / `h<=0` 视为清除）+ 滚轮事件 `type = 5`（x/y 槽 = 光标位置，`integer_y` 夹到 int8 放 bit56..63）；阶段29 加鼠标抬起事件 `type = 6`（`SDL_EVENT_MOUSE_BUTTON_UP` 的左键，坐标槽与按下一致 —— 拖分栏需要「按下抓住 / 移动 / 松开放手」三段，缺了抬起只能靠再次按下结束拖拽） |
| `src/shim/gpu_shim.c` `.h` | SDL_GPU 后端（MSL shader / Metal） |
| `src/entity.no` `src/geom.no` … | 早期库模块（被 `#` 导入）；两者均可用 —— `entity` 的遗留小写全局已改大写（收尾轮，见 §验收记录「收尾：5 个早期遗留草稿全部转绿」） |
| `src/components.no` | **全目录组件库**：83 个组件 kind + 核心 10 状态机（gpui-kit 86 模块映射）；阶段15 加 flag 位 `F-OVERLAY`（512）；阶段16 加活池 `CMP-ANCHOR`（锚点 key，-1 = 无）+ `cmp-anchor` / `cmp-set-anchor`；阶段17 加 flag 位 `F-PASS`（1024，非模态叠加层）；阶段23 加活池 `CMP-SCROLL-MAX`（布局算好的「内容高 − 视口高」，`cmp-scroll-max` / `cmp-set-scroll-max`）+ `cmp-parent-of`（父句柄，滚轮命中顺父链向外找滚动容器用；−1 = 没有父）；阶段24 加活池 `CMP-SCROLL-CONTENT`（声明的内容高，0 = 没声明 -> 实测子节点）+ `CMP-SCROLL-START`（虚拟列表首个挂载子的**绝对行号**，0 = 顺序排）+ `cmp-scroll-content` / `cmp-set-scroll-content` / `cmp-scroll-start` / `cmp-set-scroll-start`（`cmp-alloc` 各 push 一个 0）；阶段33 加 `cmp-tree-*` 与 `ct-*` 工具（`ct-get` / `ct-set` / `ct-add` / `ct-sub` / `ct-inc` / `ct-mul`）+ 模型池 `TR-PAR` / `TR-FIRST` / `TR-NEXT` / `TR-OPEN` / `TR-ROW-NODE` / `TR-ARROW` / `TR-TSTART` / `TR-TCOUNT`（随 `cmp-alloc` 逐槽 push）+ `cmp-tree-add` / `-flat`（显式栈迭代先序）/ `-row-head` / `-row-body` / `-row` / `-sync`（两段式重建）/ `-selected` / `-select` / `-row-of` / `-node-of` / `-arrow-of` / `-open-all` / `-depth` / `-kids` / `-has-kids` / `-toggle`（树是普通 `C-TREE` 容器，行是应用调出来的真组件）；阶段34 加 `ed-lex-quote` / `ed-lex-line`（六类着色，相邻同色合并）+ `cmp-editor()`（根就是 `C-SCROLL`，`value2` = 滚轮步长，`pad` / `gap` 写字面量 1200 / 800，因为 `components.no` 看不到 `CS-PAD` / `CS-GAP`）+ 文本模型池 `ED-LINES` / `ED-KEEP` / `ED-L` / `ED-C` / `ED-SEEN` / `ED-ROW` + `cmp-editor-clamp` / `-set-text` / `-text` / `-lines` / `-line` / `-set-line` / `-line-of` / `-col-of` / `-set-cur` / `-del-line` / `-ins-line` / `-insert` / `-backspace` / `-enter` / `-move` / `-key` + `cmp-win-range` + `cmp-editor-follow`（光标没动不重排窗口）/ `-off` / `-set-off` / `-row-head` / `-row-body` / `-row` / `-row-of` / `-sync`；阶段35 加 `cmp-table()`（根是普通 `C-TABLE` 容器，`value` = 列数 / `value2` = 行数）+ 全局数据模型 `TB-HEAD` / `TB-COLW` / `TB-CELL` / `TB-KEY` / `TB-ORDER` + 随 `cmp-alloc` 逐槽长的句柄状态池 `TB-OFF` / `TB-SEL` / `TB-HEAD-H` / `TB-SCR-H` / `TB-ROW-SRC` / `TB-CELL-SRC` / `TB-CELL-COL` + `tb-arr-set`（`[]i64` 下标写，长不到补 0）+ `cmp-table-set-head` / `-head` / `-set-col-w` / `-col-w` / `-set-cell` / `-cell` / `-set-key` / `-key` / `-cols` / `-rows` / `-reset-order` / `-sort`（按 `TB-KEY` 对 `TB-ORDER` 稳定插入排序）/ `-select` / `-selected` / `-scroll` / `-off` / `-set-off` / `-row-head` / `-row-body` / `-row-of` / `-cell-of` / `-cell-rc` / `-sync`（表头 + 滚动体 + 可见行，声明内容高 `m*rowh + (m-1)*800 + 2*1200`；一个父的所有直接子要先连续挂完再填子树）+ `tb-clear-marks`；阶段36 加 `DK-SPLIT`（可拆标记）/ `DK-SPLIT-V`（分栏位置真源）/ `DK-GRAB`（抓握句柄）三个池（前两个随 `cmp-alloc` 逐槽长）+ `ct-clamp`（`hi <= 0` = 不夹上限）+ `cmp-dock-split` / `cmp-dock-split-v`（间隙 = 拖杆命中区，`minw` / `maxw` 写进 `value` / `value2`，位置初始化成 `minw`）+ `cmp-dock-splittable` / `-split-val` / `-set-split` / `-grab` / `-up`（**不新增节点种类**，几何在 `cmp-scene` 的 `cs-dock-down` / `cs-dock-move`） ；阶段37 加 `CP-TEXT` / `CP-ACT`（命令表，应用写）+ 随 `cmp-alloc` 逐槽长的 `CP-Q`（每面板查询串）/ `CP-CUR`（游标 = 命中表下标，-1 = 无）/ `CP-OFF`（滚动偏移真源）/ `CP-SB`（命中表在 `CP-SRC` 里的基址）+ 只增的 `CP-SRC`（命中源行号扁平抄本）/ `CP-INP-H` / `CP-SCR-H`（输入框 / 滚动体句柄，-1 = 没有）/ `CP-ROW-SRC`（行句柄 -> 源行号，-1 = 不是面板行）八个池 + `cmp-command-count` / `-add` / `-query` / `-set-query` / `-type` / `-backspace` / `-off` / `-set-off` / `-scroll` / `-input` / `cmp-command-sync`（根就是 `C-COMMAND` 普通竖排容器：输入框 + 滚动体第一次建好就复用，命中表抄副本，**全摘**旧行 -> 两段式重挂 -> 逐行填内容，声明内容高 `m*rowh + (m-1)*800 + 2*1200`）+ `cmp-command-cur` / `-set-cur` / `-move`（夹紧在命中区间内）/ `-cur-src` / `-run`（取 `CP-ACT[源行号]`）/ `-row-of` / `-row-src`（**不新增节点种类**，面板由应用调出来的真组件拼，行是 `C-FORM` + `C-LABEL`） |
| `src/cmp-scene.no` | **组件渲染层（阶段8）+ 主题层（阶段12）**：`cs-layout` / `cs-measure` / `cs-paint` / `cs-hit` / `cs-is-container` + Scene 指令池（rect/fill/text）；容器布局消费 `L-H` / `L-GROW` / `L-AE` / `L-AC` 提示与 `gap` / `pad` 覆盖；`cs-measure` / `cs-layout` 消费 `CMP-SW` / `CMP-SH` 固定尺寸，`cs-paint-leaf` 经 `cs-align-of` / `cs-text-origin-x` 消费 `CMP-ALIGN`；阶段12 加 `CS-TH` 色板 + `cs-theme` / `cs-theme-set` / `cs-theme-reset` / `cs-var-color`（绘制路径所有分支改读主题色，默认值 = 旧常量）；阶段15 加 `cs-is-overlay` / `cs-child-flow`（叠加层不参与流布局，布局末尾在父矩形内居中，绘制最后画，命中先于其它子节点且点不穿 -> `-2`）；阶段16 加 `cs-find-key`（按声明式 key 反查活句柄）+ 叠加层分支改为锚定优先（锚点下方 -> 贴边翻上方 -> 两轴夹紧到父矩形，无锚点 / 锚点找不到则居中）；阶段17 把 `cs-hit` 的叠加层分支分档：模态竖 `-2` 屏障，非模态（`F-PASS`）照常命中、没接住就继续走流子节点；阶段23 加 `C-SCROLL` 三件套 —— 布局两遍（先累加内容高、夹紧 `off` 写 `CMP-SCROLL-MAX`，再按夹紧后的 `off` 排版，子节点不拉伸；**不写回状态**），绘制推 `CS-CLIP` 裁剪矩形后画子树（含叠加层）再恢复，命中包装成「视口外不命中、视口内按全局坐标直接命中」（阶段 24 修正：子节点矩形本身已带 `-off` 偏移，原来多换算了一次 `py + off`）（`cs-hit-inner` / `cs-hit`）；同时给三条指令发射器（`cs-emit-rect` / `cs-emit-fill` / `cs-emit-text`）统一挂上裁剪记录（`cs-emit-clip` + `CS-CF/CX/CY/CW/CH` 池 + `cs-scene-cf/cx/cy/cw/ch` 读取器），`cs-clear` 一并清；注意通用布局分支必须显式排除 `C-SCROLL`（`->` 链不互斥，后跑的赢）；阶段24 把 `C-SCROLL` 布局拆成实测 / 声明两遍：`decl > 0` 时内容高直接用 `CMP-SCROLL-CONTENT`（不再累加子节点），第 j 个儿子排在 `y + pad - off + (CMP-SCROLL-START + j) * (行高 + gap)`（绝对行号，窗口滑到底不会堆在视口顶），`decl <= 0` 保持原顺序排版；`cs-hit` 的包装改成「视口外的点不命中、视口内的点按全局坐标直接 `cs-hit-inner`」（子节点矩形本身已带 `-off` 偏移，原来多换算了一次 `py + off`，大偏移时视口内点不中）；阶段25 让 `C-SCROLL` 布局分支不再要求 `n > 0` —— 过滤列表命中 0 行时内容高就是上下 padding（`maxoff` 夹到 0），否则整个分支被跳过、`cmp-scroll-max` 留着上一次的旧值（滚轮会按旧上限夹紧，清空查询后列表还停在旧偏移），第二遍排版仍在 `decl > 0 && n > 0` 时才做（`n == 0` 时没有子节点要排） |
| `src/app.no` | **应用装配层（阶段9）**：`app-init` / `app-frame` / `app-draw-scene` / `app-poll` / `app-hover` + 窗口尺寸 / 底色 / 坐标换算；阶段23 加 `_noui-clip` / `_noui-clip-clear` 声明 + `APP-WHEEL` 池 + `app-wheel()`（最近一次滚轮有符号增量）+ `app-draw-scene` 按指令逐个设裁剪（`pxi` 定点→像素）+ `app-poll` 解 bit56..63 的 int8 补码增量 |
| `src/dv.no` | **声明式视图层（阶段10）**：`dv-*` 构造器（覆盖全部 83 kind，视图节点 SoA 池）+ `dv-render` / `dv-sync` 调和器（按 `(key, kind)` 复用组件、最小增删改排）+ `dv-act` / `dv-dispatch` 动作数据 + `dv-apply` 状态归约器（SET / TOGGLE / DELTA + clamp）+ `dv-repeat` 重复原语 + `dv-cond` 条件节点 + `dv-lay` 布局提示（`dv-row` / `dv-fill` / `dv-lay-*` 组合子）+ `dv-size` / `dv-align` 尺寸与对齐提示（`DV-SW` / `DV-SH` / `DV-ALIGN`）+ `dv-live-by-key` / `dv-live-deep` / `dv-parent-of-key` + `dv-on*` flags 组合子；阶段12 加 `DV-VAR` 变体池 + `dv-variant` / `dv-var-primary|secondary|ghost|danger` + `dv-theme-set` / `dv-theme-reset`（主题是全局的，不随 `dv-clear()` 清空）；阶段13 加 `DV-BIND` / `DV-BIND-MAX` / `DV-BIND-T`（字符串状态池）+ `dv-bind` / `dv-bind-of` / `dv-bind-max-of` / `dv-bound` / `dv-bound-set` / `dv-bind-t-ensure` / `dv-node-count` + `DV-HOT-*` + `dv-hotkey` / `dv-hot-dispatch` / `dv-hot-count`；阶段14 加 `dv-repeat-items`（文案 / value / 动作 / 载荷四个平行数组 → n 个列表项）/ `dv-rows-del`（删第 i 项返回新长度）+ `DV-A-ITEM`（列表项被点，载荷 = 行号）；阶段15 加 `DV-F-OVL`（64）+ `dv-overlay` / `dv-on-overlay`（叠加层标记，经 `dv-apply-flags` 落到活节点 `F-OVERLAY`）；阶段16 加 `DV-ANCHOR` + `dv-anchor-of` / `dv-overlay-anchor`（叠加层 + 锚点 key，经 `dv-apply-props` 落到活节点 `CMP-ANCHOR`）；阶段17 加 `DV-F-PASS`（128）+ `DV-HOVER` 池 + `dv-hover` / `dv-hover-of` / `dv-pass` / `dv-on-tip`（`dv-apply-flags` 覆盖式同步 `F-PASS`）；阶段18 加 `dv-disabled` / `dv-enabled` / `dv-disabled-of`，`dv-dispatch` / `dv-hot-dispatch` 对禁用节点不出动作（`DV-HOT-VKEY` 存的是声明式 key，先 `dv-node-by-key` 反查）；阶段19 加 `DV-TAB-ACT` 池 + `dv-tabs` / `dv-tab-active-of`（标题条 + n 标题 + 内容容器，标题挂 `DV-A-SET`），`dv-sync` **末尾**按 `DV-TAB-ACT` 调 `tab-select` 覆盖回 `CMP-VALUE`（`cs-paint-tab` 读它画高亮；放末尾是因为 `dv-apply-props` 跑在递归之前，那时容器可能还没孩子）；阶段20 加 `DV-EMPTY-F` + `dv-repeat-items-f`（阶段14 原语多一个 flags 平行数组，短数组 / 空数组补 `dv-on()`）+ `dv-sel-flags(S, slot0, n)`（状态槽非 0 → `dv-on-sel()`，否则 `dv-on()`），`dv-repeat-items` 签名不变、内部转调 f 版；阶段21 加 `DV-A-CHOOSE`(10)（选中并收起：`dv-apply` 一次写两个槽 —— `S[payload] = value`，若 `value2 >= 0` 再 `S[value2] = 0`）+ `dv-select-from(S, parent, key, selslot, openslot, labels, ph, base)`（一行建出 `C-SELECT` 字段 + n 个常驻 `C-LABEL` 选项：字段文案 = `labels[S[selslot]]`（未选 / 越界为空 → 占位符 `ph`），字段挂 `DV-A-TOGGLE(openslot)`、选项挂 `DV-A-CHOOSE(selslot)` 且 `value` = 下标 / `value2` = 展开槽）；阶段22 加 `dv-toast-alive(S, slot, ttl)`（槽存**触发帧**，`t > 0 && dv-frame() < t + ttl` 才活，0 = 没有通知）+ `dv-toast(S, parent, key, slot, text, ttl, base)`（活着才建 `C-POPOVER` + `dv-on-tip()` + 文案 + 关闭按钮，关闭 = `DV-A-SET` 写回 0），并把 `DV-FRAME[0]` 初值设为 1，让 0 成为干净的「从未触发」哨兵；阶段23 加 `dv-scroll(S, key, offslot, step, parent)`（`C-SCROLL`：`value` = `S[offslot]`、`value2` = 步长、`DV-A-CHANGE(offslot)`）+ `dv-apply-props` 的 `VALUE2` 同步补 `C-SCROLL` 分支；阶段24 加 `DV-SCROLL-CONTENT` / `DV-SCROLL-START` 视图池（`dv-clear` 清、`dv-node` 补 0）+ `dv-scroll-content` / `dv-scroll-content-of` / `dv-scroll-start` / `dv-scroll-start-of` 构造器与读取器，`dv-apply-props` 末尾无条件同步到活节点的 `CMP-SCROLL-CONTENT` / `CMP-SCROLL-START`（0 = 没声明，普通滚动不受影响）+ `dv-repeat-at(parent, key0, start, count, kind, texts, values, acts, pays, flags, rowh)`（`dv-repeat-items` 的切片版：只建 `start .. start+count-1`，行 key = `key0 + 绝对行号`（窗口滑动复用同一批组件），`rowh > 0` 时给每行 `dv-size` 定高）+ `dv-list-view(S, key, offslot, step, viewh, rowh, n, texts, vals, acts, pays, key0, parent)`（一行式虚拟列表：可见窗口 `i0 = (off - pad - rowh)/行距 + 1` 夹到 `[0, n-1]`、`i1 = ceil((off - pad + viewh)/行距)` 夹到 `n`，声明内容高 `n*rowh + (n-1)*CS-GAP + 2*CS-PAD`，再走 `dv-scroll` + `dv-size` + `dv-scroll-content` + `dv-scroll-start` + `dv-repeat-at`）；阶段25 加 `dv-win-range(off, viewh, rowh, n, out)`（可见窗口 `[i0, i1)`，结果写进 `out[0]` / `out[1]` —— 形参不能回写所以走数组；`dv-list-view` 也改用它，两套窗口公式只有一个真源）+ `dv-repeat-mapped(parent, key0, start, count, kind, idxs, texts, values, acts, pays, flags, rowh)`（`dv-repeat-at` 的「按下标表取行」版：挂 `idxs[start .. start+count)` 这些**源行**，行 key = `key0 + 源行号`（查询变化 / 窗口滑动都按源行号复用组件），越界 / 负数下标夹到 `[0, m-1]`，`count` 超出表长时只挂到表尾（否则会建出一堆 key 相同的行，`dv-sync` 按 `(key, kind)` 复用会互相覆盖））+ `dv-filter-map(qslot, n, texts, idxs)`（查询来自动态字符串状态 `DV-BIND-T[qslot]`，两边 `to-lower` 后 `str.index` 判命中 —— 大小写不敏感，空查询命中全部；命中行号写进 `idxs`，返回命中数）+ `dv-filter-count`（只要数量不要表的便捷版）+ `dv-filter-list(S, key, offslot, step, viewh, rowh, n, texts, vals, acts, pays, qslot, key0, parent)`（`dv-list-view` 的过滤版：多一个 `qslot`，内容高 = `命中数*rowh + (命中数-1)*CS-GAP + 2*CS-PAD`）；阶段26 加 `dv-sort-run(idxs, n, keys, desc)`（稳定原地插入排序）/ `dv-sort-map(mode, n, keys)`（建 `0..lim-1` 再排序，返回下标表；`lim` 夹到 `keys.len()` 且 ≥ 0；mode 1 升 / 2 降 / 其它源序）/ `dv-group-plan(on, n, grps, plan, out)`（`{}` 短路的 `->` 链：`on == 0` 复制 plan，否则组号变化时写 token `-(g)-1`（负 = 标题），返回混合行数 `m`；`lim` 夹到 `plan.len()`）/ `dv-repeat-plan(parent, key0, hkey0, start, count, kind, plan, texts, vals, acts, pays, flags, names, icolor, hcolor, rowh)`（混合行渲染器：token < 0 挂标题行（key = `hkey0 + 组号`、文案 `names[组号]`、`dv-disabled()` + `hcolor`），token ≥ 0 挂数据行（key = `key0 + 源行号`），行数按**混合行表长度**夹 —— 分组时窗口行数会超过数据行数，用 `texts` 长度夹会把标题切掉）/ `dv-order-list(S, key, offslot, step, viewh, rowh, n, keys, grps, names, texts, vals, acts, pays, modeslot, key0, hkey0, parent)`（`dv-list-view` 的顺序版：mode 0 源序 / 1 键升 / 2 键降 / 3 分组 / 4 分组降；内容高按**混合行数** `m` 算，所以分组后 `maxoff` 也跟着变大）；阶段27 加 `DV-R-REQ/MINL/MAXL/MINV/MAXV/EQ/HAS`（= 1..7，表单校验规则种类）+ `dv-rule-check(v str, spars []str, kinds []i64, pars []i64, base i64, n i64)`（单字段单规则表 → 第一条违反的规则号（**全局规则下标 + 1**，不是切片内序号 —— `msgs` 是逐条规则的扁平表，按字段切片后仍要能直接查到措辞），全过 0；长度类先 `trim`，数值类先 `to-i64`（解析失败算违反），`EQ` 的 `par` = 另一字符串槽号、`HAS` 的 `par` = `spars` 下标；`base`/`n` 越界按「没这条规则」吞掉）/ `dv-rule-msg(code, msgs)`（规则号 → 文案，0 / 越界空串）/ `dv-form-bad(slots, bases, ns, spars, kinds, pars, nf)`（多字段汇总「坏了几项」，`nf` 超表长按表长）/ `dv-field(key, errkey, slot, label, ph, show, spars, kinds, pars, base, n, msgs, parent)`（一行 = `dv-label(key)` + `dv-input(key+1)` + `dv-bind(输入, slot, 0)`，且仅当「有违反 && `show != 0`」时 `dv-label(errkey, 文案)` + `dv-var-danger()`；返回输入句柄 —— **「什么时候显示错误」是另一根轴**，`show` 是揭示槽）；阶段28 加 `dv-table-rows(parent, key0, hkey0, ckey0, start, count, ncols, colws, tw, plan, names, cells, acts, pays, hcolor, rowh)`（混合行表 → 窗口内的表格行：`token < 0` 挂**通栏**组标题（`key = hkey0 + 组号`、文案 `names[组号]`、`dv-disabled()` + `hcolor`、宽 = `tw`），`token >= 0` 挂 `C-GRID` 行（`key = key0 + 源行号`、宽 = `tw`、高 = `rowh`）+ `ncols` 个 `C-LABEL` 单元格（`key = ckey0 + 源行号*ncols + 列号`、文案 `cells[...]`、`colws[c] > 0` 时定宽），行动作 = `acts[源行号]` / `pays[源行号]`；`start` 先夹进 `[0, plan.len()]`、`count` 再按剩余可挂行数夹 —— 两端越界时一行不挂，不兜底成第 0 行）/ `dv-table-list(S, key, hkey, offslot, step, viewh, rowh, plan, ncols, colws, headers, names, cells, acts, pays, key0, ckey0, hkey0, hcolor, parent)`（表头 + 滚动容器 + 可见窗口的行：表头是 `parent` 的**前一个孩子** `C-GRID`（不滚），行高声明为 `CS-PAD + CS-LINE-H`（交叉轴顶对齐，标签正好落在 `y+pad .. y+pad+行高`，比自动量的 `2*pad+行高` 省一行边距）；表宽 `tw = 2*CS-PAD + 列宽和 + (ncols-1)*CS-GAP` 由它自己算；内容高按混合行数 `m` 算，`dv-win-range` 切窗口，`dv-table-rows` 只挂可见行 —— `ncols = 1` 且不分组时与 `dv-order-list` 行为等价）；阶段29 加 `dv-dock-split(key, slot, minw, maxw, gw, parent)`（声明式可拆面板：`C-DOCK` 横向容器 + `gap = gw`（拖杆命中区）+ `DV-A-CHANGE(slot)`（左屏宽的真源）+ `value` / `value2` = 夹紧界；两块面板之间的 gap 不属于任何子节点，`cs-hit` 的自身矩形兜底会把它算到容器头上 —— 所以拖杆就是那条缝，不需要新增节点种类或布局分支；名字带 `-split` 是因为 2 参数的 `dv-dock(key, parent)` 已作为「83 kind 构造器」之一被 `tests/dv.no` 覆盖）+ `dv-apply-props` 的 `VALUE2` 同步补 `C-DOCK` 分支（否则夹紧上限永远读到 0）；阶段31 加 `dv-tree-flat(S, openslot, n, kids0, kids, parents, out, deps)`（**显式栈的迭代式先序**把展开态作用到树上 → 可见行表 + 每行层级；数据形状 = 「先序序列 + 兄弟块相邻」，节点 `i` 的孩子是下标 `kids0[i] .. kids0[i]+kids[i]-1`；用 `with-len(n)` + `sp` 下标模拟栈，避开 `vec.pop()` 的 `%option_i64`）+ `dv-tree-row(parent, key0, v, d, texts, kids, S, openslot, selslot, rowh)`（一行 = `C-FORM` 行（`value` = 节点号、`DV-A-SET(selslot)`）+ 右对齐「缩进 + 箭头」`C-LABEL`（宽 = `层级*缩进 + 箭头宽`，有孩子才给 `v` / `>`，叶子留空但占位，挂 `DV-A-TOGGLE(openslot + 节点号)`）+ 文本 `C-LABEL`；选中态三处一起变 —— `dv-on-sel()` / 行底色 `CS-TH-TRACK` / 文本色 `CS-TH-ACCENT`）+ `dv-tree-view(S, key, offslot, step, viewh, rowh, n, texts, kids0, kids, parents, openslot, selslot, key0, parent)`（一行式树视图 = `dv-scroll` + `dv-scroll-content` + `dv-win-range` + 逐行 `dv-tree-row`，偏移真源是状态槽 `offslot`，行 key = `key0 + 节点号*10000000`（按源节点号，滚动复用同一批组件），内容高 = `m*行高 + (m-1)*间隙 + 2*内边距`）+ `DV-A-FILL = 11`（一次把 `S[payload .. payload+value2)` 全写成 `value`，`value2 <= 0` 不写、回执 0 —— 「全部展开 / 全部收起」只是同一动作的两个操作数）；阶段32 加 `DV-A-RUN = 12`（`S[payload] = value` 写命令码，`S[value2] = 0` 关面板 —— 一个动作写两个槽）+ `dv-command-palette(S, key, openslot, qslot, offslot, cmdslot, n, texts, acts, key0, parent)`（一行式命令面板：`dv-on-overlay()` 的 `C-FORM` 面板根 + `dv-bind` 到查询槽的 `C-INPUT`（只显示不收键盘）+ `dv-lay-grow()` 的 `C-SCROLL` 列表（内容高按命中数算）+ 逐行 `C-FORM` 行 + 文本 `C-LABEL`，行与文本都挂 `DV-A-RUN(cmdslot)`（`value` = 命令码、`value2` = 开关槽），行 key = `key0 + 源行号*100`，游标行 `dv-on-sel()`；登记 / 开关沿 flip / 游标夹紧都放在「关着也跑」的那半段）+ `dv-pal-keys-ensure` / `dv-pal-of`（视图节点 → 面板 key）/ `dv-pal-open-key(S)`（哪个面板开着）/ `dv-pal-move` / `dv-pal-cur-key`（走 `DV-PAL-SRC` 扁平抄本 + 每面板基址 `DV-PAL-SB` 现算，游标移动不重建视图也能立刻回车）/ `dv-pal-reset` / `dv-pal-close` / `dv-pal-type` / `dv-pal-backspace` + `DV-PAL-KEYS` / `-ROOT` / `-OPEN` / `-Q` / `-CMD` / `-CUR` / `-M` / `-WAS` / `-KEY0` / `-SB` / `-SRC` 池（按**面板 key** 存，跨帧存活，`dv-clear()` 不清） |
| `src/dv-app.no` | **声明式应用装配层（阶段11）**：`dva-run(S, title, max-frames)` 主循环（`app-poll` / hover / 命中 / `dva-focus` 焦点交接 / `dva-keyboard` / 按名调 `dv-app-view`·`dv-app-reduce`·`dv-app-click` / 重建视图 / `dv-render` 调和 / `cs-layout` / `cs-paint` / 上屏）+ `dva-frames` / `dva-ops` / `dva-clicks` / `dva-set` / `dva-root` / `dva-px` / `dva-py`；阶段13 加 `dva-sync-binds(root)`（受控输入双向同步）/ `dva-str-get` / `dva-str-set` / `dva-last-get` / `dva-last-ensure`（shadow）；阶段17 加 `dva-sync-hover(S)`（每帧把 hover 写进状态槽）+ `DVA-HIT` / `DVA-MOUSE` 命中缓存（`cs-layout` 之后重算）；阶段18 的 `dva-focus(hit)` 尊重禁用（带 `F-DISABLED` 的节点视同空白，禁用输入框不接焦点）；阶段23 加 `dva-wheel(S, root)`（`app-wheel` 拿增量 → `cs-hit` → 顺 `cmp-parent-of` 父链找最近的 `C-SCROLL` → 读它的 `DV-A-CHANGE` 载荷当偏移槽 → `clamp(cur + dy*step, 0, cmp-scroll-max)` → 变了才 `dva-set`）+ 主循环 `t == 5` 分支（把光标写进 `DVA-PX/PY` 后调 `dva-wheel`）；阶段29 加 `DVA-DRAG` 抓握池（1 槽，存容器句柄，-1 = 没在拖）+ `dva-drag-down(S, root, px, py)`（命中 `C-DOCK` / `C-RESIZABLE` 后先派发认动作、再按「第一块面板的右/下边界 .. +gap」判是不是落在间隙里，是则抓住；返回 1 = 抓住了）/ `dva-drag-move(S, px, py)`（横向 `px - 第一块面板左边`、竖向 `py - 上边`，`dv-clamp` 到视图的 `[CMP-VALUE, CMP-VALUE2]`，变了才 `dva-set`）/ `dva-drag-up()` / `dva-drag-of()`，主循环 `t == 2` 先试拖杆（抓住了就跳过点击路径）、`t == 3` 追加一次 `dva-drag-move`、`t == 6` 调 `dva-drag-up`，退出收尾也松手；阶段30 把 `dva-focus` 拆出 `dva-focus-raw`（点在编辑器里就交出输入焦点，反之点进输入框就交出编辑器焦点，两者互斥）；阶段32 加 `DVA-PAL-FOCUS`（1 槽，存当前接管键盘的面板 key，-1 = 没有）+ `dva-pal-find(hit)`（顺活父链上溯，用 `cmp-key` → `dv-node-by-key` → `dv-pal-of` 找最近声明过面板根的祖先，返回面板 key）+ `dva-pal-keyboard(S, kk)`（`↑` / `↓` 走 `dv-pal-move`，回车取 `dv-pal-cur-key` 后 `dv-dispatch` → `dv-app-reduce` → `dv-app-click`，ESC 走 `dv-pal-close`，退格走 `dv-pal-backspace`，32..126 的可打印字符经 `ED-PRINT` 切片走 `dv-pal-type`），`dva-focus` 开头先问 `dva-pal-find` —— 命中在面板里就把输入 / 编辑器焦点都放掉且**不**把焦点交给面板内的输入框；主循环 `t == 4` 的键盘优先级改成 **面板 > 输入 > 编辑器 > 全局**（`pk >= 0` 时整段归面板）；每帧建视图**之后**调 `dv-pal-open-key(S)` 写 `DVA-PAL-FOCUS`（必须在建视图之后 —— 面板 key 是构建时才登记的）并清输入 / 编辑器焦点；退出收尾也清 `DVA-PAL-FOCUS` |
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
| `tests/dva-scroll.no` | 阶段23 滚动纯逻辑：标记（`C-SCROLL` 的 kind / `value` = `S[offslot]` / `value2` = 步长 / `DV-A-CHANGE` 载荷 = 偏移槽 / flags；改状态重建 ops = 0 且 value 跟随）/ 布局（`off=0` 基线；`off` 生效后行按 −off 上移且行距不变；`cmp-scroll-max` = 内容高 − 视口高；状态写超界 → 夹紧到 max 但**不写回状态**；负数夹紧回 0 且不写回）/ 绘制（滚动子孙的 Scene 指令带裁剪标记，裁剪矩形 = 滚动视口；滚动外的兄弟指令不带）/ 命中（视口内命中正确的行（按全局坐标直接命中 —— 阶段24 修掉了多换算一次 `py + off` 的 bug，断言点从 `py=2200` 改成 `py=7000`）；视口下 / 左外不命中；从整页根命中顺父链到同一行；偏移改变后命中目标跟着变）/ 滚轮（滚一格 → 槽 = step；上界夹紧到 max；已到底不动；下界夹紧到 0；已在顶不动；落在滚动外的兄弟上不动；命中落在滚动容器自身空白处也认；改偏移是纯状态变更，重建 ops = 0 且布局位置真的变了）（33 项断言） |
| `tests/dva-virtual.no` | 阶段24 声明式虚拟列表纯逻辑：标记（`dv-list-view` 建出 `C-SCROLL` 的 kind / `value` = `S[offslot]` / `value2` = 步长 / `DV-A-CHANGE` 载荷 / flags；声明内容高与首个挂载子行号同时出现在视图节点和活节点上；改状态重建 ops = 0）/ 布局（`cmp-scroll-max` = 声明内容高 − 视口高 = 97600；任意时刻只挂可见的 4~5 行；行文案正确；行距 = 行高 + `CS-GAP`；滚到底窗口是第 16..19 行、首行 y = −2400；状态写超界 → 夹紧，首行 y = 15000 且**不写回状态**）/ 绘制（滚动子孙的指令仍带裁剪矩形 = 滚动视口）/ 命中（`off=0` 与 `off=max` 时视口内的点都命中正确的行）/ 滚轮（滚 20 格 → 槽 = 8000，窗口滑到第 1..4 行、首行 y = 200；窗口滑动复用同一批 key；两端夹紧；改偏移重建 ops = 0）/ 复用（根下直接子 = 5 个 = 容器 + 4 行；文案 `v18` / `v19` / `''`；行 key = 718..722 绝对行号）（36 项断言） |
| `tests/dva-filter.no` | 阶段25 声明式过滤列表纯逻辑：标记（`dv-filter-list` 建出 `C-SCROLL` 的 kind / `value` = `S[offslot]` / `value2` = 步长 / `DV-A-CHANGE` 载荷 / flags；声明内容高同时出现在视图节点和活节点上）/ 过滤（空查询 = 全量 20 行；`alpha` 命中 5 行、`ALPHA` 同样 5 行（大小写不敏感）；`eta` 命中 `beta 5..9`；`1` 命中 11 行（`alpha 1` + `gamma 10..14` + `delta 15..19`）；`zzz` 命中 0 行 → 挂 0 行、内容高 0、`maxoff` 0；`gamma 1` 时窗口第 0 行是源第 10 行、`value` = 10（值按**源下标**取，不是窗口位置））/ 布局（内容高随命中数变 = 30600、`maxoff` = 10600；行位 = 窗口序号 × 行距，首行 y = 2400、次行 2400 + 5800；滚到底窗口滑到源 11..14、首行 y = −2400、末行 y = 15000；状态写超界 → 夹紧，首行 y = 15000 且**不写回状态**）/ 绘制（可见行带裁剪矩形 = 滚动视口）/ 命中（`off = 0` 时点源第 6 行、`off = max` 时点源第 8 行，都命中正确的行；视口外不命中）/ 滚轮（滚 20 格 → 槽 = 8000，窗口滑到源 6..9、首行 y = 200；同一批行 → 组件对象复用 `ops = 0`；两端夹紧）/ 映射（`dv-repeat-mapped` 按下标表 `[3,1,0]` 取行 → key 703/701/700、文案 `alpha 3` / `alpha 1` / `alpha 0`；表长 2 而 `count` 5 → 只挂 2 行、key 819/818）（46 项断言） |
| `tests/dva-order.no` | 阶段26 声明式排序 / 分组纯逻辑：标记（`dv-order-list` 建出 `C-SCROLL`：mode 0/1/2 无标题行、mode 3/4 多两行标题；声明内容高按混合行数）/ 排序（`dv-sort-map` 源序 / 键升 `[0,7,4,1,8,5,2,9,6,3]` / 键降；**稳定** —— 同键保持源序；`n` 夹到 `keys.len()`；负数 `n` → 空表；非法 mode 7 → 源序）/ 分组（`dv-group-plan`：`on = 0` 复制；分组 token `[-1,5,6,7,8,9,-2,0,1,2,3,4]`；**组序跟着 plan 走**（源序 plan → 组 2 在前）；空 plan；`n = 0`；plan 短于 `n`）/ 布局（源序内容高 37600 / `maxoff` 21600，分组 44800 / 28800；mode 1/2/3/4/9 的窗口与文案；标题文案 / key / 禁用 + `CS-TH-MUTED`、数据行 `CS-TH-FG`；行位 y = 2400 / 6000；**换模式按源行号复用组件**（`ops` 不涨）；分组滚到近底 → 6 行 `[组1标题, 行0..4]`、行 9 不挂；状态写超界 → 夹紧到 `maxoff` 且不写回，窗口只剩混合表最后一行）/ 绘制（可见行带裁剪矩形）/ 命中（点数据行 → `DV-A-CLICK` 载荷 = 源行号；点标题 → `(0,0)`；视口外不命中 —— 命中根用滚动容器，表单本身是个大矩形）/ 滚轮（滚 20 格 → 偏移 8000、窗口 `[6,7,8,9,-2]`；两端夹到分组 `maxoff`；切回键升后上限缩到 21600）（43 项断言） |
| `tests/dva-valid.no` | 阶段27 声明式表单校验纯逻辑：规则表（4 字段 = 扁平 `kinds`/`pars` 的 `base + n` 切片：必填 + 最短 2 / 必填 + 18..99 / 必填 + 含 `X` / 必须等于槽 0）/ 规则（逐条：必填 / 最短 / 最长 / 最小值 / 最大值含**不可解析算违反** / 与另一槽相等 / 含子串；`trim` 生效；多规则取第一条；`base + n` 切片；`n = 0`；越界 `base` 吞掉；未知种类忽略）/ 汇总（全过 / 坏 1 / 坏 2 / 全坏；`nf = 0`；`nf` 超表长按表长；空规则字段不算坏）/ 视图（揭示槽 0 → 错误行**不存在**；揭示后出现且文案正确 + 危险变体；合法时不建；输入确实绑到字符串槽；提交按钮 flags = f(坏项数)；改值重建同一输入 key 复用同一组件对象；同状态重复 render 零操作）/ 绘制（揭示后错误文案真的画出来；未揭示时场景里没有它）/ 命中（禁用提交被命中也不出动作；合法时提交 = `DELTA(D-SUB)`）（46 项断言） |
| `tests/dva-table.no` | 阶段28 声明式虚拟表格纯逻辑：行（`dv-table-rows` 挂 3 行 2 列：行 kind = `C-GRID` / 每行 `ncols` 个单元格 / 文案 = `cells[源行号*列数 + 列号]` / 行宽 = 声明表宽 / 行高 = `rowh` / 列宽 = `colws`；乱序 `plan` 的行号映射；通栏组标题 = 单个禁用标签、占满表宽、文案 = `names[组号]`；`count` 夹到混合行表长度；`start` 越界 -> 一行不挂）/ 列表（`dv-table-list` 表头两个单元格 + 滚动容器；声明内容高 20 行 = 73600、`maxoff` = 63600；分组 22 行 = 80800 / 70800；窗口切片；状态驱动偏移；换偏移复用同一批行、`ops` = 0；同状态重复 render 零操作；空 plan -> 内容高 0 / `maxoff` 0 / 0 行）/ 绘制（行指令带裁剪矩形 = 滚动视口）/ 命中（点单元格命中该行；派发 = `DV-A-SET` + 载荷 = 7）（32 项断言） |
| `tests/dva-dock.no` | 阶段29 声明式可拆面板纯逻辑：标记（`dv-dock-split` 的 kind = `C-DOCK` / `CMP-LAY` 带 `L-H` / `CMP-GAP` = 声明的拖杆宽 / `CMP-VALUE` = 下限 / `CMP-VALUE2` = 上限 / 派发 = `DV-A-CHANGE` + 载荷 = 状态槽 / flags 可见可用；改状态重建零操作且夹紧界不变）/ 布局（左屏宽 = 状态槽 / 右屏从「左屏右边界 + 间隙」起、吃到 dock 内容右边界 / 两块同高同顶）/ 命中（间隙中点命中容器 / 面板内部命中面板 / 容器内边距命中容器 / 容器外不命中）/ 拖拽（间隙正中间抓住且 `dva-drag-of()` = 容器 / `px = 20000` → 左屏宽 `20000 - 2400 = 17600` / 拖进左屏夹到下限 8000 / 拖出右界夹到上限 28000 / `dva-drag-up()` 后再移动不动 / 面板内部不抓 / 容器内边距不抓）/ 竖拆（`dv-lay(dock, 0, gap, pad)` 抹掉 `L-H` → 上屏高 = 状态槽 / 按 `py` 换算 / `px` 甩出去值不变）/ 重建（拖完改状态 → 重建零结构操作 → 左屏宽跟着变，声明式闭环）（31 项断言） |
| `tests/dva-editor.no` | 阶段30 声明式代码编辑器纯逻辑：着色（`dv-lex-line` 对 `''` / `;abc` / `"hi"` / `42` / `let` / `x` / 空白 / `let x` / `42 43` / `a;b` / `"a" "b"` 的 token 数 / 文案 / 颜色，含**相邻同色合并**、关键字表、`dv-lex-quote` 配对与未闭合、`dv-byte-at` 越界）/ 行模型（`dv-edit-split` + `dv-edit-join` 往返，含 `'a\nb\n'` 的结尾空行保留、`''` 与单行）/ 编辑原语（右移 / 插字符 / 退格 / 回车 / 上下 / **Tab 插入 4 空格且光标前进 4** / 跨行退格并入上行且光标落到拼接处 / 行首退格到顶不再吞字符 / 空文本）/ 窗口（`dv-win-range` 的 `i0` / `i1` / 空表）/ 跟随（光标不动不重排窗口且偏移槽不变 / 光标动了才重排 / 重复调用稳定 / 强制偏移槽不被覆盖）/ 装配（挂载出 `C-SCROLL` + 声明内容高 + 可见行 + 行号文案 / `dv-edit-of(dv-node-by-key(key))` = 文本槽 / 命中编辑器内部任意节点 → `dva-edit-find` 上溯到编辑器 key，根与 -1 不是 / `dva-focus(hit)` → `DVA-EDIT-FOCUS` = 编辑器 key / `dva-edit-keyboard` 喂一个字先进文本状态 / 点空白释放焦点 / 空文本仍有光标 / 重建零结构操作）（61 项断言） |
| `tests/dva-tree.no` | 阶段31 声明式树纯逻辑：拍平（`dv-tree-flat` 的显式栈先序 —— 展开态作用到树上得可见行表 + 每行层级；13 节点 / 根 + src 展开 → 8 行 `[0,1,5,6,7,2,3,4]`、层级 `[0,1,2,2,2,1,1,1]`）/ 行结构（行 `C-FORM` 的 `value` = 节点号 + 挂 `DV-A-SET(selslot)` / 箭头 `C-LABEL` 右对齐、宽度 = `层级*缩进 + 箭头宽`、有孩子才给 `v` / `>` / 叶子箭头空且无动作但仍占位 / 文本 `C-LABEL`）/ 内容高（`m*行高 + (m-1)*间隙 + 2*内边距` = 27200）/ 选中态（行 flags 换 `dv-on-sel()` + 行底色 `CS-TH-TRACK` + 文本色 `CS-TH-ACCENT` 三处一致，换回未选中复原）/ 调和（首帧 1 + 8*3；只换选中 = **零结构操作**且活树颜色跟随；收起子树才增删节点；再展开节点回来）/ 派发归约（点箭头 = `DV-A-TOGGLE(openslot + 节点号)`；点行 = `DV-A-SET(selslot)` 写节点号；叶子箭头 `dispatch` = 0；`DV-A-FILL` 全展开一次写 13 槽、全收起只剩根、`value2 <= 0` 不写且回执 0）/ 端到端（`cs-layout` → `cs-hit` 命中行 / 箭头 → `dv-dispatch` → `dv-apply` → 重建 → 活树跟随；偏移 100px 只挂可见 5 行且首行 = 节点 6、末行 = 节点 4；全部展开 13 行全在且 `cmp-scroll-max > 0`；选中行文本色）（42 项断言） |
| `tests/dva-palette.no` | 阶段32 声明式命令面板纯逻辑：标记（关着不建子树且 `dv-pal-open-key` = -1 / 开着是 `dv-on-overlay()` 的 `C-FORM` / 输入是 `C-INPUT` 且 `dv-bind-of` = 查询槽 / 列表是 `C-SCROLL` 且 8 行 / 行 `value` = 命令码 + `DV-A-RUN(cmdslot)` / `value2` = 开关槽 / 文本 / 行 key 按**源行号** / 游标行 `dv-on-sel()` / 内容高 30400 / 命中数 + 游标 + `dv-pal-cur-key` / `dv-pal-open-key` / 关面板整棵消失）/ 调和（首帧 3 + 8×2 / 活树文本 / 过滤成 4 行（`s` 还命中 `close`）且增删 / 游标移动**零结构操作** / 退格回空查询 8 行全回来 / 关面板活树根消失）/ 派发归约（行 `dispatch` = `RUN(cmdslot)`；`dv-apply` 回执 2 且写命令码 + 清开关槽；过滤 `sa` 2 行 / 过滤 `z` 0 行且游标 -1 且 `cur-key` -1 / 空命中移动不动 / `dv-pal-type` 追加 / `dv-pal-backspace` 退格）/ 端到端（`cs-layout` → `cs-hit` 命中行（最深节点是文本，故断言行或其父）→ `dv-dispatch` → `dv-apply` → 活树；面板焦点下 `↓` → 游标 1 → 回车执行游标行 → 命令码 + 关面板 + 计数；可打印字符进查询串；ESC 关面板；命中面板内输入框 `dva-pal-find` 仍 = 面板 key）（34 项断言） |
| `tests/dva-select.no` | 阶段21 下拉纯逻辑：标记（字段 `C-SELECT` 的 kind / key / 文案 / 占位符 / value / flags，选项 key 递增 / 文案 / `value` = 下标 / `value2` = 展开槽 / `act = DV-A-CHOOSE` 载荷 = 选中槽，字段 `act = DV-A-TOGGLE` 载荷 = 展开槽，子节点数，未选 / 越界时字段文案空）/ 调和（ops = 4；活节点 kind / value / 文案 / 占位符；`select-option-count` / `select-selected` / 选项文案；展开后 ops = 0 + `F-OPEN`；换选中 + 收起后 ops = 0 且文案 / value / selected 跟随）/ 派发归约（字段 `TOGGLE` + 展开槽、选项 `CHOOSE` + 选中槽；`dv-apply` 写 2 个槽；`value2 < 0` 只写 1 个槽）/ `cs-hit`→`cmp-key`→`dispatch`→`apply`→重建端到端（展开后可命中选项、选中并收起、`CS-TH-ACCENT` 计数更高、收起后同一坐标不再命中选项）（32 项断言） |
| `tests/dva-tabs.no` | 阶段19 标签页纯逻辑：标记 / 调和（换页 ops=0、越界收敛）/ 派发归约 / 布局（标题同行 x 递增、容器在条下方）/ 绘制（选中标题独占 `CS-TH-ACCENT`）/ `cs-hit`→`cmp-key`→`dispatch`→`apply`→重建端到端（27 项断言） |
| `tests/main-dv.no` | 阶段10+11+12+13+14+15+16+17+18+19+20+21+22+23+24+25+26+27 **窗口端到端声明式 demo**：`dva-run` 接管主循环，按名提供 `dv-app-view` / `dv-app-reduce` / `dv-app-click`；视图含居中页头 + `dv-row`/`dv-size`/`dv-fill` 定宽两栏 + 重复行 + 组件目录速写；阶段12 在 `dva-run` 前设 9 槽主题、视图里挂 6 处变体；阶段13 把输入框绑到 `DV-BIND-T[0]` 并回显、`r` 键重置；阶段14 三行各挂 `(DV-A-ITEM, 行号)`，点任意一行删掉它；阶段15 `dialog` 按钮写 `D-DIALOG=1`，视图里出现居中的模态对话框（ok / cancel 写回 0）；阶段16 `popover` 按钮写 `D-POP=1`，视图里出现锚在触发按钮下的菜单（`close` 写回 0）；阶段17 `dv-hover(b19, D-TIP)` 让鼠标停在 `dialog` 按钮上时锚在它下面浮出 tooltip（非模态，点得穿），页头回显 `tip=`；阶段18 按状态派生禁用（`nl >= DEMO-MAX-ROWS` 时 `add row` 用 `dv-disabled()`、`nl <= 0` 时 `del row` 禁用），并给禁用的 `add row` 挂 `dv-hover(b16, D-TIP2)` —— 禁用节点照样能被 hover，浮出「at the row cap」解释原因；阶段19 右栏底部加一条三页标签（`dv-tabs(rc, tab, D-TAB, DEMO-TABS, 40)`，页头回显 `tab=`，reset 回到第 0 页），点标题切页时容器与标题条同 key 复用、只换内容子树；阶段20 右栏顶部加一条三行多选（`dv-repeat-items-f(rc, 53, 3, C-RADIO, DEMO-SEL, DEMO-SEL-VALS, DEMO-SEL-ACTS, DEMO-SEL-PAYS, dv-sel-flags(DEMO, D-SEL0, 3))`，每项 `(DV-A-TOGGLE, D-SEL0+i)`，页头回显 `sel=N`）+ `all` / `none` 两个幽灵按钮（`DV-A-CLICK` + `DEMO-CMD-ALL` / `DEMO-CMD-NONE`，命令分支一次写 3 个槽），reset 一并清空选中位；阶段21 左栏下拉换成 `dv-select-from(DEMO, lc, 4, D-CHOICE, D-SOPEN, DEMO-OPTS, 'pick', 4)`（原来是手写 `C-SELECT` + 3 个选项 + `DV-A-OPEN` / `DV-A-SET`，事件侧那两段 select 分支一并删掉），右栏单选组文案改从 `DEMO-OPTS` 读（`dv-demo-opt(i)`），reset 多写一个 `D-SOPEN`，页头回显 `pick=`；阶段22 左栏加 `notify` 按钮（`value = dv-frame()` + `DV-A-SET(D-TOAST)`），视图里 `dv-toast(DEMO, page, 78, D-TOAST, 'saved', DEMO-TOAST-TTL, 79)` 非模态叠加出「saved」，页头回显 `tst=`，reset 多写一个 `D-TOAST`（`src/app.no` + `src/dv.no` + `src/dv-app.no` + `src/cmp-scene.no`）；阶段23 右栏顶部加一条 10 行 / 视口 160px 的滚动列表（`dsc = dv-scroll(DEMO, 55, D-SCROLL, DEMO-SCROLL-STEP, rc)` + `dv-size(dsc, 0, DEMO-SCROLL-VIEW)` + `dv-repeat-items(dsc, 93, ...)`，内容约 336px），新增 `D-SCROLL` 槽（`with-len(18)`）与 `DEMO-SCROLL*` 数据数组，页头回显 `scrl=`，reset 多写一个 `D-SCROLL`，并把 `dv-app-click` 的 CHANGE 分支收紧为只认 `C-SLIDER`（滚动容器的 `DV-A-CHANGE` 走装配层 `dva-wheel`，否则点一下滚动空白会把滑杆值写坏）；阶段24 把右栏顶部那条列表换成 20 行 / 视口 160px 的**虚拟列表**：`dsc = dv-list-view(DEMO, 55, D-SCROLL, DEMO-SCROLL-STEP, DEMO-SCROLL-VIEW, DEMO-SCROLL-ROWH, DEMO-SCROLL-N, DEMO-SCROLL, DEMO-SCROLL-VALS, DEMO-SCROLL-VALS, DEMO-SCROLL-VALS, 93, rc)`（原来是 `dv-scroll` + `dv-size` + `dv-repeat-items(dsc, 93, 20, ...)` 全挂 20 行），内容高 736px，任意时刻只挂可见的 5 行 —— 首帧 `ops` 64 → 59；阶段25 页头加搜索框（`sinp = dv-input(13, 'search list', page)` + `dv-bind(sinp, D-QUERY, 0)`，新增字符串槽 `D-QUERY = 1`，`dv-bind-t-ensure(2)`），右栏那条列表换成 `dv-filter-list(..., D-QUERY, 93, rc)`，页头回显 `match=N`（`nm = dv-filter-count(D-QUERY, DEMO-SCROLL-N, DEMO-SCROLL)`），reset 多写一个 `dv-bound-set(D-QUERY, '')`，首帧 `ops` 59 → 60；阶段26 把「顺序」也变成状态：右栏那条列表换成 `dv-order-list(DEMO, 55, D-SCROLL, DEMO-SCROLL-STEP, DEMO-SCROLL-VIEW, DEMO-SCROLL-ROWH, DEMO-SCROLL-N, DEMO-SCROLL-KEY, DEMO-SCROLL-GRP, DEMO-SCROLL-GNAME, DEMO-SCROLL, DEMO-SCROLL-VALS, DEMO-SCROLL-VALS, DEMO-SCROLL-VALS, D-SORT, 93, 9000, rc)`（键故意乱序 `(i*7)%20`、组号 `i/10`，所以排序 / 分组都真的改变行序），上方加「order」下拉 `dv-select-from(DEMO, rc, 56, D-SORT, D-SOPEN2, SORT-OPTS, 'order', 57)`（`dv-select-from` 第二次出场：同一个原语既做左栏 pick 也做排序），新增 `D-SORT = 18` / `D-SOPEN2 = 19`（`with-len(20)`）与 `SORT-OPTS`（`source` / `key asc` / `key desc` / `group` / `group desc`），页头回显 `sort=`，reset 多写 `D-SORT = 0` / `D-SOPEN2 = 0`；搜索框与 `match=` 回显撤掉（过滤与排序是两根轴，demo 一次只演一根），`D-QUERY` 槽保留，首帧 `ops` 60 → 65；阶段27 把「这个字段合不合法」也变成数据：左栏原本是普通 `C-FORM`，装不下新增的四个字段，改成 `dv-scroll` 滚动容器（`lc = dv-scroll(DEMO, 91, D-LSCROLL, 400, cols)` + `dv-size(lc, 30000, 0)`），上面挂四字段校验表单（`vf = dv-form(310, lc)` + 4 × `dv-field(200/210/220/230, 400/410/420/430, D-VN/D-VA/D-VC/D-VF, ...)`，规则表 `V-KINDS`/`V-PARS`/`V-SPARS`/`V-MSGS`，`V-PARS[7] = D-VN` —— `EQ` 比的是**另一个字符串槽**，0 号槽是页头那个 `type here` 输入框）+ 状态标签（`bad={} submits={}`）+ 主色提交按钮（`DV-A-CLICK` + `DEMO-CMD-SUBMIT`）；提交是显式命令（和 `reset` 同套路，归约器表达不了）：`bad > 0` → 写 `D-VSHOW = 1`（揭示错误行），`bad == 0` → `D-VSUB + 1` + `D-TOAST = dv-frame()`；新增 `D-VSHOW = 20` / `D-VSUB = 21` / `D-LSCROLL = 22`（`with-len(24)`）与字符串槽 `D-VN/D-VA/D-VC/D-VF = 2..5`（`dv-bind-t-ensure(6)`），页头回显 `vsub=` / `lsc=`（后者顺带提示左栏能滚了），reset 多写三个槽 + 清空四个字符串；首帧 `ops` 65 → 76（1 表单容器 + 4 标签 + 4 输入 + 1 状态标签 + 1 提交按钮 + 1 滚动容器 − 原左栏表单容器），`bye … vsub=0 lsc=0`；阶段28 右栏那条 20 行列表升级成两列表格（`dv-table-list(DEMO, 55, 920, D-SCROLL, DEMO-SCROLL-STEP, DEMO-SCROLL-VIEW, DEMO-SCROLL-ROWH, tplan, 2, DEMO-TABLE-COLW, DEMO-TABLE-HEADERS, DEMO-SCROLL-GNAME, DEMO-TABLE-CELLS, DEMO-TABLE-ACTS, DEMO-TABLE-PAYS, 93, 900, 9000, CS-TH-MUTED, rc)`，`tplan = dv-demo-order-plan(DEMO)` 是阶段26 那条纯函数链的 demo 侧抽取），左栏让宽 30000 → 22000、右栏视口 16000 → 11200，首帧 `ops` 76 → 83；阶段29 把左右两栏这一行从 `dv-row` 换成 `dv-dock-split(95, D-DOCK, 12000, 42000, 800, page)` + `dv-lay(cols, dv-lay-h-grow(), 800, 0)`（中间那条 8px 的缝就是拖杆，`dv-size(lc, dk, 0)` 的 `dk = DEMO[D-DOCK]` 是左屏宽的真源），新增 `D-DOCK = 23`（`with-len(24)` 刚好用满）与 `DEMO-DOCK-KEY/GAP/MIN/MAX/DEF`，页头回显 `dock=`、`bye` 也带 `dock=`，节点数不变所以首帧 `ops` 仍是 83；阶段30 左栏底部加声明式代码编辑器（`dv-code-editor(DEMO, DEMO-ED-KEY, D-ED-TEXT, D-ED-OFF, DEMO-ED-STEP, DEMO-ED-VIEW, DEMO-ED-ROW, DEMO-ED-GUTTER, DEMO-ED-KEY0, lc)` + `dv-size(ed, dk, DEMO-ED-VIEW)`，上面挂一个 `editor` 标题标签，初始文本 = 一段 noui 代码 `DEMO-ED-CODE`，行高 24px / 视口 144px / 行号槽 48px / 滚轮步长 32px），新增 `D-ED-OFF = 24`（`with-len(25)`）与字符串槽 `D-ED-TEXT = 6`（`dv-bind-t-ensure(7)`），页头回显 `edoff=`、`bye` 也带 `edoff=`，reset 多写 `D-ED-OFF = 0` + 把 `D-ED-TEXT` 设回初始文本；首帧 `ops` 83 → 105（1 个标题标签 + 编辑器 21 个节点：滚动容器 + 内容容器 + 4 行，光标所在行把一个 token 拆成「前缀 / 光标 / 后缀」所以多 2 个）；阶段31 左栏顶部加声明式树（`dv-label(89, 'tree', lc)` + `dv-tree-view(DEMO, 980, D-TOFF, 3200, 16800, 2400, 13, DEMO-TREE-TEXTS, DEMO-TREE-K0, DEMO-TREE-KN, DEMO-TREE-PAR, D-TOPEN, D-TSEL, 9800, lc)` + `dv-size(tr, dk, 16800)`，下方两个幽灵按钮 `expand all` / `collapse all`（同一个 `DV-A-FILL`，`value` 一个 1 一个 0，载荷 = `D-TOPEN`）），新增 `D-TOPEN = 25` / `D-TSEL = 38` / `D-TOFF = 39`（`with-len(40)`）与 `DEMO-TREE-*`（`DEMO-TREE-N = 13`、`DEMO-TREE-KEY = 980`、`DEMO-TREE-KEY0 = 9800`、`DEMO-TREE-ROW = 2400`、`DEMO-TREE-VIEW = 16800`、`DEMO-TREE-STEP = 3200` + `DEMO-TREE-TEXTS` / `-K0` / `-KN` / `-PAR` 三组数据 —— 文案数组用 `with-len` + 逐项写，因为模块级 `[]str` **字面量**在函数体里读出来是空的，见 `ARCHITECTURE.md §5.25`），页头回显 `tsel=` / `toff=`、`bye` 也带，reset 多写展开槽（根 + src 展开、其余收起）+ `D-TSEL = -1` + `D-TOFF = 0`，启动处显式写 `DEMO[D-TOPEN] = 1` / `DEMO[D-TOPEN+1] = 1` / `DEMO[D-TSEL] = -1`；首帧 `ops` 105 → 124（1 个标题标签 + 树 8 行 × 3 + 滚动容器 + 2 个按钮 = 1 + 24 + 1 + 2），之后 899 帧零结构操作，exit 0（连跑两次一致）；阶段32 树下面加主色 `palette` 按钮（`DV-A-SET(D-PAL-OPEN)` + 快捷键 `p`）与 `dv-command-palette(DEMO, 99, D-PAL-OPEN, D-PAL-Q, D-PAL-OFF, D-PAL-CMD, 8, DEMO-PAL-TEXTS, DEMO-PAL-ACTS, 2000, page)`（面板根 99 / 输入 100 / 列表 101，行 key `2000 + 源行号*100` —— 避开编辑器 960 / 9700+ 与树 980 / 9800+ 的 key 段），新增 `D-PAL-OPEN = 40` / `D-PAL-OFF = 41` / `D-PAL-CMD = 42`（`with-len(44)`）与字符串槽 `D-PAL-Q = 7`（`dv-bind-t-ensure(8)`），页头回显 `pal=`、`bye` 也带 `pal=`，reset 多写 `D-PAL-OPEN = 0` / `D-PAL-CMD = 0` + `dv-bound-set(D-PAL-Q, '')` + `dv-pal-reset(99)`；执行走显式命令 `pay == D-PAL-CMD`（归约表达不了「顺带弹个通知」→ `D-TOAST = dv_frame()`）；首帧 `ops` 124 → 125（多的 1 个是触发按钮，面板关着时只登记 key 不建子树），把面板打开跑一遍是 144（再多 19 个节点 = 面板根 + 输入 + 列表 + 8 行 × 2），`bye … pal=0`，exit 0（连跑两次输出逐字节一致，只有 window handle 这个指针不同） |
| `tests/cmp-tree.no` | 阶段33 命令式树纯逻辑：构造（kind = `C-TREE` / `value` = -1）/ 模型（`cmp-tree-add` 的 first-child + next-sibling 链与 `cmp-tree-kids`）/ 拍平（显式栈先序，全收起 2 行 → 展开 `r0` 4 行 → 再展开 `a0` 5 行）/ 两段式重建（行数 = 可见节点数、行 → 节点映射、箭头标签、叶子箭头留空占位）/ 选中 / 二次 sync（旧行摘掉不翻倍）/ 命中上溯 / 两树隔离（36 项断言） |
| `tests/main-tree.no` | 阶段33 **窗口端到端命令式树 demo**：左 `C-SCROLL` 包 `cmp-tree()`（19 节点全展开 45600 > 视口 39600 所以滚轮真能用）+ 右详情面板；点行选中 / 点箭头切换 / ↑↓ 移动 / ←→ 收起展开 / Enter / 滚轮（命中顺父链找到 `C-SCROLL` 才滚，夹紧到 `cmp-scroll-max`）/ `e` `c` 全展开全收起 / `r` 重置 / ESC `q` 退出（900 帧 `bye frames=900 clicks=0`） |
| `tests/cmp-editor.no` | 阶段34 命令式代码编辑器纯逻辑：构造 / 文本模型（`set-text` 保留末尾空行、`del-line` 真少一行、`ins-line` 插一行）/ 夹紧（行阵列 / 光标行列 / 空文本）/ 编辑原语（插字符 / 退格含跨行并入 / 回车 / Tab 4 空格）/ 移动（左右 / 上下 / 跨行 / 行首行尾夹紧）/ 按键分派（`ED-K-*` / `ED-PRINT` / 8 / 13 / 9）/ 窗口（`cmp-win-range` 的 `i0` / `i1` / 空表）/ 跟随（光标没动不重排、`ED-SEEN`）/ 两段式重建（行数 = 可见行数、行号文案、光标拆段、内容高 / 起始行号）/ 着色（关键字 / 字符串 / 注释 / 数字 / 标识符 / 空白）/ 多编辑器隔离（65 项断言） |
| `tests/main-editor.no` | 阶段34 **窗口端到端命令式编辑器 demo**：左 `cmp-editor()`（`L-GROW`，`sh` 跟窗口高）+ 右状态面板（lines / cursor / offset / rows / keys+clicks）；打字 / 退格 / 回车 / Tab / 方向键 / 点行跳光标 / 滚轮 / `r` 重置 / ESC `q` 退出。重建段平铺在主循环**顶层**（阶段34 时 `cmp-editor-sync` 包进函数会拿到 `undef` 句柄，2026-11-22 已修，见 ARCHITECTURE §5.28），900 帧 `bye frames=900 keys=0 clicks=0 rows=7` + `editor render OK` |
| `tests/cmp-table.no` | 阶段35 命令式虚拟表格纯逻辑：构造（kind = `C-TABLE` / `value` = 列数 / `value2` = 行数）/ 模型（表头定义列数、单元格行主序、列宽 0 = 自适应）/ 键 / 两段式重建（表头 + 滚动体 + 可见行数、内容高 33600、空表头 0 行）/ 选中 / 命中上溯（行 / 列 / 行列出参）/ 偏移 / 两表隔离（句柄状态池各自独立）/ 排序 / 稳定性 / 降序 / 空表 / 重定义表头（57 项断言） |
| `tests/cmp-dock.no` | 阶段36 命令式可拆面板纯逻辑：标记（kind / 可拆标记 / `L-H` / gap / pad / `value` / `value2` / 位置初值 / 改位置不动夹紧界）/ `ct-clamp`（上下界 + `hi <= 0` 不夹上限）/ 布局（dock 与两屏的 rect、间隙宽度、改位置后重排）/ 命中（间隙 -> 容器、面板内部 -> 面板、容器内边距 -> 容器、容器外 -> 不是）/ 拖拽（抓住 / px 换算 / 上下界夹紧 / 松手后不动 / 面板与内边距不抓 / 容器外不抓 / 夹紧界没被写掉）/ 竖拆（按 `py` 换算、`px` 不参与）/ 双 dock 隔离 / 普通 `C-DOCK` 不抓（58 项断言） |
| `tests/main-table.no` | 阶段35 **窗口端到端命令式表格 demo**：左 `cmp-table(3, 20)`（`L-GROW`，`sh` 跟窗口高）+ 右状态面板（sel / cell / off / max / rows / mode / keys+clicks）；点行选中 / 点单元格报源行:列 / `1` `2` `0` 升序降序源序 / 滚轮（命中链上有表格根才滚，夹紧到 `cmp-scroll-max`）/ `r` 重置 / ESC `q` 退出，900 帧 `bye frames=900 keys=0 clicks=0 rows=11 sel=-1` + `table render OK`。分支体多句一律**换行**写（`;` 是行注释，见 ARCHITECTURE §5.29） |
| `tests/main-dock.no` | 阶段36 **窗口端到端命令式可拆面板 demo**：`cmp-dock()` + `cmp-dock-split(d, 12000, 60000, 1200)`（`L-GROW`，`sh` 跟窗口高），左 `explorer` 12 行标签、右 `detail` 5 个状态标签；左键按下先试 `cs-dock-down`（抓住就置 `dk-drag`，不走点击路径），移动时 `cs-dock-move` 换算光标，抬起 `cmp-dock-up` 松手；每帧把 `DK-SPLIT-V` 同步到左屏 `cmp-set-sw`；点行选中 / `r` 重置 / ESC `q` 退出，900 帧 `bye frames=900 split=12000 sel=-1 clicks=0 moved=0` + `dock render OK`，两次运行逐字节一致 |
| `tests/cmp-command.no` | 阶段37 命令式命令面板纯逻辑：构造（kind = `C-COMMAND` / 无子）/ 命令表（`add` 后 `count`）/ 查询编辑（`type` / `backspace` / 越界退格不动）/ 两段式重建结构（直接子 == 2、`C-INPUT` + `C-SCROLL`、`sh` = 行高、滚动体 `L-GROW` / pad 1200 / gap 800、内容高 37600、起始 0、8 行、行是 `C-FORM` + 1 个子、选中行 `F-SELECTED` + 底色 + 文本色 = accent、行源行号 0/1/7）/ 命中上溯 / 游标与执行 / 导航夹紧（`move` 上下界）/ 过滤（`file` 4 命中、`FILE` 大小写不敏感、`save` 2 命中且内容高 16000）/ 空命中（`cur` = -1 / `cur-src` = -1 / `run` = -1 / 0 行 / 内容高 2400）/ 偏移（`off` 0 / 4000 / 999999 三档的起始行号与行数）/ 重复 sync（句柄复用、子链不累积）/ 双面板隔离（共享命令表、查询 / 游标 / 偏移各自独立）（90+ 项断言） |
| `tests/main-command.no` | 阶段37 **窗口端到端命令式命令面板 demo**：12 条命令的模态面板（`cmp-command()` + `F-OVERLAY` + `sw` 480000 / `sh` 34000）+ 主界面 5 个状态标签；打字过滤（32..126 经 `ED-PRINT` 切片）/ ↑↓ 导航 / 回车执行游标行 / 退格 / 点行执行 / 点面板外（`cs-hit` = -2）关闭 / `p` 开 / ESC `q` 退出；面板打开时每帧在主循环**顶层** `cmp-command-sync`（经函数中转会拿到错位的子链），900 帧 `bye frames=900 rows=8 keys=0 clicks=0 runs=0` + `command render OK` |
| `tests/cmp-input.no` | 阶段38 命令式输入框富文本 / 语法高亮纯逻辑：构造 / placeholder / 初始光标 / `append` 旧语义 / `set-caret` 夹紧 / 中段 `insert` / 退格（`caret`=0 无操作）/ 前删 / `move` 夹紧 / `set` & `clear` 重置光标 / disabled 写操作全无效 / 词法（段数 == 4、`'a = '`/`'1'`/`' '`/`'; hi'` 配色、拼接 == 原文、开关）/ 渲染（纯文本 3 条 vs 高亮 6 条、注释色与数字色都在 Scene 里、光标落 token 内与末尾）（26 项断言） |
| `tests/main-input.no` | 阶段38 **窗口端到端输入框 demo**：两个字段（一个开高亮、一个纯文本，同一段样例文本），点击聚焦 / 失焦、打字在光标处插入 / 退格 / 前删 / ←→ 移光标、`p` 切高亮、`r` 重置、`q` / ESC 退出，900 帧 `bye frames=900 keys=0 clicks=0 value=let x = 42 ; hi caret=15 lex=1 tokens=5` + `input render OK` |
| `tests/cmp-textarea.no` | 阶段39 命令式多行文本域纯逻辑：构造 / placeholder / 空文本算 1 行 / 行几何（`line-count` 2、`line-start` 0 与 3、`line-len` 2+2、`line-text`、行号越界夹到最后一行）/ 光标行列（0、2、3、4 四个位置）/ 上下移保持列 + 列夹紧（`abcdef`↑`xy` 夹到 2）+ 首尾行不动 / 行中插 `\n` 多一行且光标落新行行首 / 行首退格合并上一行 / 行尾前删合并下一行 / disabled 写无效 / 视口（`set-off` 夹 0、`follow` 上滚 / 不动 / 下滚、`cap<=0` 按 1）/ 渲染（视口内 3 行、视口外两行不进 Scene、光标滚出后视口跟随到 `off=2`、placeholder）（34 项断言） |
| `tests/main-textarea.no` | 阶段39 **窗口端到端多行文本域 demo**：一个大字段（48000 × 16000，视口 6 行）+ 状态标签；点击聚焦 / 失焦、打字 / 回车换行 / 退格 / 前删 / ↑↓←→（上下走 `ta-move-up` / `-down`）、`r` 重置、`q` / ESC 退出，900 帧 `bye frames=900 keys=0 clicks=0 value=let x = 1¶let y = 2¶; done caret=26 line=2 col=6 off=0 lines=3` + `textarea render OK` |
| `tests/cmp-number.no` | 阶段40 命令式数字输入 / 步进器 / 搜索框纯逻辑：构造（三个 kind、初始值 0、步长默认 1）/ 值域与步长读写（`step-set` 非正数按 1）/ 吸附（23→20、25→30、27→30、-5→0、200→100）/ `hi<=0` 不夹上限 / `num-set` 同步 `CMP-TEXT` / `num-bump`（`dir*step`、disabled 不动）/ `num-commit`（合法写回、`' 77 '` 因 `to-i64` 不裁空白而失败、`'abc'` 与空串失败且不动值、`'500'` 夹到 100）/ `search-query` / `search-clear` / `search-hit`（命中 1、中间 0、越上边界 0）/ `stepper-hit`（-1 / 1 / 0 / 越界 0、窄控件按钮对半分）/ 渲染（数字与步进器值都进 Scene、搜索有文本时画清除钮、清空后不画）/ 几何（数值右对齐落点 = 右边界 − 文本宽、步进器值居中、减号横杠与加号竖杠落在左右钮上）（42 项断言） |
| `tests/main-number.no` | 阶段40 **窗口端到端输入族 demo**：数字输入（20000 宽，值域 0–100 / 步长 5）+ 步进器（24000 宽，值域 0–100 / 步长 10）+ 搜索框（32000 宽）+ 状态标签；点击聚焦 / 失焦、点步进器左 / 右钮 `stepper-hit` 命中后 `num-bump`、点搜索右端 `search-hit` 命中后 `search-clear`；键盘按焦点分流（数字输入：数字键入 + 回车 `num-commit` + ↑↓ `num-bump`；步进器：↑↓←→ `num-bump` + 回车 `num-commit`；搜索：文本 + 退格 + 前删 + ←→）、`r` 重置、`q` / ESC 退出，900 帧 `bye frames=900 keys=0 clicks=0 num=40 stepper=50 search=` + `number render OK` |
| `tests/cmp-otp.no` | 阶段41 命令式 OTP 验证码输入纯逻辑：构造（`cmp-otp-input(len)` 槽数入 `CMP-VALUE`、`len <= 0` 按 1 槽、空码）/ 写数字（`otp-set-digit` 定长补空格、非 0..9 清空、写活动槽）/ `otp-put` 跳**下一个空槽**（连写、满槽停末槽、从空槽起跳）/ 聚合（`otp-filled` / `otp-complete` / `otp-digits` 去空格 / `otp-value`）/ `otp-clear` / `otp-backspace`（活动槽有数字清它、否则回退清上一格、第 0 槽无数字不动）/ `otp-delete` 只清不移动 / 左右夹紧 / `otp-set` 灌入（只取数字、忽略多余、活动槽回 0）/ disabled 全写无效 / 槽格几何（`otp-slot-w` 按可用宽均分且不低于 1200、`otp-slot-x` 累加格宽 + 格距 800）/ `otp-hit`（命中第几槽、格距不算、上下越界 -1）/ 渲染（`cs-measure` 宽度 = 槽数 × 格宽 + (槽数-1) × 800、`cs-paint-otp` 每槽一格 + 数字居中 + 聚焦活动槽四条强调描边、空槽竖光标、未聚焦控件不描边）（48 项断言） |
| `tests/main-otp.no` | 阶段41 **窗口端到端 OTP demo**：6 槽（预置 `'123456'`）+ 4 槽（预置 `'2468'`）+ 状态标签；点击按 `otp-hit` 选中槽、键盘按焦点分流（数字 `otp-put` / 退格 / 前删 / ←→↑↓ / `c` 清空 / `r` 重置 / `q` / ESC 退出）、状态栏回显 `6-slot: {v} (cur {c}, filled {f}/{n}) | 4-slot: …`，900 帧 `bye frames=900 keys=0 clicks=0 otp=123456 cur=0 otp2=2468 cur2=0` + `otp render OK` |
| `tests/cmp-combobox.no` | 阶段42 命令式 combobox 可输入下拉纯逻辑：构造（`cmp-combobox()` 空查询 / 0 选项 / 高亮 -1 / 选中 0）/ 加选项（空查询 = 全选，返回下标）/ 查询过滤（`'ap'`→Apple、`'AN'`→Banana、`'z'`→0 且高亮 -1、清空→3 且高亮回 0、越界匹配下标 -1，大小写不敏感）/ 匹配反查（`match-index-of` 命中 1、自身 -1）/ 高亮夹紧与移动（`set(-5)`→-1、`set(99)`→2、`move` 到顶 / 底都夹紧）/ 选中（`choose` 写查询并收起、匹配表跟着变；`choose-match` / `apply`）/ 编辑查询（`insert` 追加 + 光标推进 + 重算匹配；`backspace` / `delete` 复用输入框光标语义；`clear` 清空 + 光标归零）/ disabled 全写无效 / 测算与布局（收起 6400、展开按最宽匹配项 9600，面板行高 2400、行距 800）/ 渲染（`'v'` 箭头、三个选项文本、高亮行 `CS-TH-ACCENT` 底、收起后选项文本消失）/ 命中（展开点选项 0 / 1 得选项句柄、点字段得 combobox 自身、收起后 -1）（41 项断言） |
| `tests/main-combobox.no` | 阶段42 **窗口端到端 combobox demo**：水果（Apple / Banana / Cherry / Durian，预置选中 Banana）+ 颜色（Red / Green / Blue，预置选中 Blue）两个 + 状态标签，默认展开水果那个；点击展开面板按 `combobox-match-index-of` 反查后 `combobox-choose`、点字段开关 `F-OPEN`；键盘按焦点分流（字母 `combobox-insert` 过滤并自动展开、退格 / 前删、↑↓ `combobox-highlight-move`、回车 `combobox-apply`、`c` 清空、`r` 重置）、Esc 先收起再退出、`q` 退出，900 帧 `bye frames=900 keys=0 clicks=0 cb1=Banana sel1=1 match1=1 open1=true cb2=Blue sel2=2 open2=false` + `combobox render OK` |
| `tests/cmp-rating.no` | 阶段43 值域型展示控件纯逻辑：**slider 下限回归**（`cmp-slider(20,100)` 的 `slider-min`/`-max` 与 `slider-value-at` 在 0 / 5000 / 10000 三点的采样 = 20 / 60 / 100、`slider-set` 夹紧到 `[20,100]`、`min == 0` 的旧行为不变）/ rating（构造、`rating-max`/`-value`/`-set` 夹紧 `0..max`、disabled 写无效）/ rating 命中（第 0 / 1 / 4 颗星、右边界 `x + max*cell` 外、下方、上方）/ rating 测算与渲染（`cs-measure` = `5*2400` × 行高、5 个 `*` 前 4 个组件色、最后 1 个 `CS-TH-MUTED`）/ pagination（构造、`pagination-page`/`-total`/`-clamp`/`-set`/`-prev`/`-next`、只有 1 页时夹在 1、disabled 写无效、`pagination-label` = `5/12`）/ pagination 命中（0 上一页 / 1 标签 / 2 下一页 / 上下越界 -1 / 控件太窄一律 -1）/ pagination 测算与渲染（intrinsic `2*4000 + 标签宽` × `CS-BTN-H`、`<` `>` 与标签、到顶时 `<` 转 MUTED、翻到最后一页 `>` 转 MUTED 且 `<` 恢复）/ empty（标题 + `cmp-set-text2` 描述、有描述两行高、无描述单行高、`cs-measure` 取标题 / 描述里更宽的、两行都进 Scene）（54 项断言） |
| `tests/main-rating.no` | 阶段43 **窗口端到端值域型展示控件 demo**：`cmp-slider(20,100)`（初值 40）+ `cmp-rating(5)`（3 星）+ `cmp-pagination(12)`（第 1 页）+ `cmp-empty`（标题 + 描述）+ 三条状态标签；点击按 `rating-hit` / `pagination-hit` / `slider-value-at` 分别设星数 / 翻页 / 设值，键盘 `a` `d` 星数、`z` `x` 翻页、`s` `w` 滑条、`e` 空态描述开关、`r` 重置、`q` / ESC 退出；状态栏实时回显 `slider at-left/at-mid/at-right` 三个采样点（20 / 60 / 100），900 帧 `bye frames=900 keys=0 clicks=0 slider=40 rating=3/5 page=1/12 label=1/12` + `rating render OK` |
| `tests/cmp-alert.no` | 阶段44 提示族纯逻辑：level 语义（`LV-*` 四档、`lv-slot` 映射 `0->2` / `1->7` / `2->-1` / `3->8`、`msg-level` / `msg-set-level`、未知 level 按 info 画、disabled 写无效）/ `cs-level-color` 四档 + 未知值 / `cs-level-glyph` 四个 ASCII 字形 / alert（`alert-closable` / `-set-closable` / `alert-close-w` / `alert-close-x` / `alert-hit` 的 `-1` 没命中 / `0` 主体 / `1` 关闭钮、关闭钮右缘是主体、不可关闭时整块都是主体、disabled 写无效）/ notification（`ntf-timeout` / `ntf-remain` / `ntf-set-timeout` / `ntf-set-remain` / `ntf-ratio` 的夹紧与 `total <= 0` -> 0、disabled 写无效）/ 三者测算（message `文本宽 + 1200 + 2400 + 400` × 行高、短文本夹到 `CS-MIN-LEAF-W`；alert 关闭钮加在夹紧前所以 `'hi'` 可关闭 = 8800；notification 多一行 `lv-progress-h`）/ 三者渲染（左色条 = `cs-level-color`、字形落在居中格、文本 `CS-TH-FG`、alert 的 `'x'` 只在 closable 时出现、notification 的轨道是 `CS-RECT` + 进度条长度 = `ratio`）（62 项断言） |
| `tests/main-alert.no` | 阶段44 **窗口端到端提示族 demo**：`cmp-message`（info）+ `cmp-alert`（danger，可关闭）+ `cmp-notification`（warning，5s）+ 三条状态标签；点击 alert 右端 `'x'` 把 `closable` 关掉（文案同步改成 closed），键盘 `l` `t` `n` 循环三者 level、`c` 开关 closable、`,` 扣剩余时间、`.` 重置剩余时间、`r` 全部重置、`q` / ESC 退出，900 帧 `bye frames=900 keys=0 clicks=0 msg-level=0 alert-level=3 closable=1 ntf-level=2 ntf-ratio=10000` + `alert render OK` |
| `tests/main-bubble.no` | 阶段45 **窗口端到端会话族 demo**：`cmp-bubble('left bubble')`（pos 0）+ `cmp-bubble('right bubble')`（pos 1）+ `cmp-speech('speech with lang')`（`zh-CN`）+ `cmp-speech('speech no lang')`；点击左气泡翻面、点击带标签的语音泡开关标签行，键盘 `l` 翻 b1、`r` 翻 b2、`g` 开关 sp 标签、`h` 开关 sp2 标签、`q` / ESC 退出，900 帧 `bye frames=900 keys=0 clicks=0 b1-pos=0 b2-pos=1 sp-lang=zh-CN sp2-lang=` + `bubble render OK` |
| `tests/cmp-bubble.no` | 阶段45 会话族纯逻辑：bubble API（`C-BUBBLE`、默认 pos 0、`CMP-TEXT`、`bubble-pos` / `bubble-set-pos` 的夹紧 `-5->0` / `9->1`、disabled 写无效、`bb-pad` / `bb-tail` = 1200）/ 测算（`'hello'` 与 `'hi'` 都因 `CS-MIN-LEAF-W` 夹到 6400 × 4800，`'hello world'` = 11200 × 4800）/ 渲染（底色方块、pos 0 文本在 `+1200`、pos 1 文本在 `右缘-1200-文本宽`、尾巴方块 1200 × 1200 落在底边 `bh-600`、pos 1 尾巴在 `右缘-1200-1200`）/ speech API（`C-SPEECH`、`speech-lang` 默认 `''`、`speech-set-lang`、disabled 写无效）/ 测算（有标签多 `CS-SEP-H` + `CS-GAP` + 一行，`'hi'` + `'zh-Hans-CN'` 取更宽的标签 = 10400 × 8100）/ 渲染（文本在 `+1200`、分隔线 100 高、标签在 `分隔线 + 800`、无标签时没有分隔线也没有标签文本）（28 项断言） |
| `tests/core-v4.no` | 分层内核 v4（Entity → Element → Scene） |
| `tests/app-layer.no` | 阶段3：Ctx / Action / 焦点系统 |
| `tests/widget.no` | 阶段4：widget 构造器 + 主题 |
| `tests/interaction.no` | 交互逻辑单元测试 |
| `tests/logic/` | `no test` L1–L3 纯逻辑（独立 package，无 shim 依赖） |
| `tests/kernel.no` `tests/kernel2.no` `tests/test-kernel.no` | 旧 470 行完整分层原型 / 大写修复版 / gen-test.sh 生成物；三份已统一为大写全局 + `->` guard（收尾轮，`test-kernel` 输出 `elements: 2`） |
| `tests/probe.no` `tests/test-l1.no` `tests/sv.no` `tests/t2.no` `tests/t3.no` | 早期探针；`test-l1`（L1 几何 + 实体注册表集成）/ `t2` / `t3` 已在收尾轮转绿 |

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
bye frames=900 ops=125 clicks=0 rows=2 sel=0 pick=0 tst=0 scrl=0 sort=0 vsub=0 lsc=0 dock=22000 edoff=0 tsel=-1 toff=0 pal=0   # 首帧 125 个节点（阶段19 三页标签 +7、阶段20 多选 +5、阶段21 下拉仍是 4 个节点、阶段22 触发按钮 +1、阶段23 滚动列表 +11（容器 + 10 行）；阶段24 把它换成 20 行虚拟列表 -> 容器 + 可见的 5 行 = +6，所以 64 -> 59；阶段25 加搜索框 +1（输入框本身），所以 59 -> 60；阶段26 换 dv-order-list + order 下拉，60 -> 65；阶段27 左栏换 dv-scroll 滚动容器 + 四字段校验表单（1 表单容器 + 4 标签 + 4 输入 + 1 状态标签 + 1 提交按钮 + 1 滚动容器 − 原左栏表单容器 = +11），65 -> 76；阶段28 右栏换 dv-table-list 两列表格（表头 1 + 表头单元格 2 + 滚动容器 1 + 可见 3 行 × 3 节点 = 13，原阶段26 的 1 容器 + 可见 5 行 = 6 → +7），76 -> 83；阶段29 两栏行从 dv-row 换成 dv-dock-split —— 都是 1 个节点，节点数不变；阶段30 左栏底部加声明式代码编辑器 -> 1 个标题标签 + 21 个节点（滚动容器 + 内容容器 + 4 行 ×（行 + 行号 + token），光标所在行把一个 token 拆成「前缀 / 光标 / 后缀」所以多 2 个），83 -> 105；阶段31 左栏顶部加声明式树 -> 1 个标题标签 + 树 8 行 × 3（行 + 箭头 + 文本）+ 滚动容器 + 2 个按钮 = +19，105 -> 124；阶段32 树下面加 palette 触发按钮 +1（面板关着时只登记 key、不建子树），124 -> 125，把面板打开跑一遍是 144（再多 19 个节点 = 面板根 + 输入 + 列表 + 8 行 × 2）；通知默认不活，toast 子树不计），之后 899 帧零结构操作，exit 0（连跑两次一致）

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

$ ../bin/no run -cc ./ccwrap.sh tests/dva-scroll.no
=== dva-scroll tests ===
M mark=truetruetruetruetruetruetrue
L layout=truetruetruetruetruetruetruetruetruetruetruetrue
P paint=truetruetruetruetrue
H hit=truetruetruetruetrue
W wheel=truetruetruetruetruetruetruetruetrue
all-ok=true
ALL DVA-SCROLL TESTS PASSED

$ ../bin/no run -cc ./ccwrap.sh tests/dva-virtual.no
=== dva-virtual tests ===
M mark=truetruetruetruetruetrue
L layout=truetruetruetruetruetruetruetruetruetruetruetruetruetruetruetruetrue
P paint=true
H hit=truetruetrue
W wheel=truetruetruetruetruetruetruetruetruetrue
R slice=truetruetrue
all-ok=true
ALL DVA-VIRTUAL TESTS PASSED

$ ../bin/no run -cc ./ccwrap.sh tests/dva-filter.no
=== dva-filter tests ===
M mark=truetruetruetruetruetrue
F filter=truetruetruetruetruetruetruetruetruetruetruetruetrue
L layout=truetruetruetruetruetruetruetruetruetruetruetruetrue
P paint=true
H hit=truetruetrue
W wheel=truetruetruetruetruetruetruetruetruetrue
R mapped=truetruetruetrue
all-ok=true
ALL DVA-FILTER TESTS PASSED

$ ../bin/no run -cc ./ccwrap.sh tests/dva-order.no
=== dva-order tests ===
S sort=truetruetruetruetruetruetrue
G group=truetruetruetruetruetrue
L layout=truetruetruetruetruetruetruetruetruetruetruetruetruetruetruetruetruetruetruetruetruetrue
P paint=true
H hit=truetruetruetrue
W wheel=truetruetruetrue
all-ok=true
ALL DVA-ORDER TESTS PASSED

$ ../bin/no run -cc ./ccwrap.sh tests/dva-valid.no
=== dva-valid tests ===
V rule=truetruetruetruetruetruetruetruetruetruetruetruetruetruetruetruetruetruetruetruetruetruetrue
F form=truetruetruetruetruetruetrue
L view=truetruetruetruetruetruetruetruetruetrue
P paint=truetrue
H hit=truetruetruetrue
all-ok=true
ALL DVA-VALID TESTS PASSED

$ ./build-shims.sh && ../bin/no build -cc ./ccwrap.sh -o dist/main-dv tests/main-dv.no
[shim] 完成：libnoui-shim.a / libnoui-gpu-shim.a
Build successful: dist/main-dv

$ ./dist/main-dv
window handle: 512097566720
bye frames=900 ops=76 clicks=0 rows=2 sel=0 pick=0 tst=0 scrl=0 sort=0 vsub=0 lsc=0

$ cd tests/logic && ../../../bin/no test .
all-ok=true
ALL TESTS PASSED

$ cd ../example/noui-gpu-demo && ./gpu-demo
Metal API Validation Enabled      # SDL_GPU 在 macOS 走 Metal
window handle: 528093741056
```

## 验收记录（阶段28：声明式虚拟表格）

纯逻辑测试（32 项断言，无窗口）：
```sh
$ ../bin/no run -cc ./ccwrap.sh tests/dva-table.no
=== dva-table tests ===
R rows=truetruetrue…(11 true)
L list=truetrue…(16 true)
P paint=truetrue
H hit=truetrue…(4 true)
all-ok=true
ALL DVA-TABLE TESTS PASSED
```
R 行结构 / 列宽 / plan 映射 / 通栏组标题 / `count` 与 `start` 两端夹紧；
L 表头 + 声明内容高 → `maxoff`（20 行 73600 / 63600，分组 22 行 80800 / 70800）、
窗口切片、状态驱动偏移、组件复用、零操作重渲染、空 plan；
P 行指令带裁剪矩形；H 命中单元格 → `DV-A-SET` + 载荷。

窗口端到端：
```sh
$ ../bin/no build -cc ./ccwrap.sh -o dist/main-dv tests/main-dv.no && ./dist/main-dv
noui on SDL 3.4.18
window handle: 512227049984
bye frames=900 ops=83 clicks=0 rows=2 sel=0 pick=0 tst=0 scrl=0 sort=0 vsub=0 lsc=0
```
右栏那条 20 行列表换成两列表格（表头 `name` / `key` 不滚，容器里只挂可见的
3 行，每行 2 个单元格），上方「order」下拉原样驱动五档模式，分组模式下多出来的
组标题变成通栏行。左栏让宽 300px → 220px、右栏视口 160px → 112px，
给表头（36px）与「order」下拉（40px）让位 —— 否则 420px 高的窗口装不下，
下拉会被顶出屏外（这是实测出来的：表头自动量高 48px 时会把下拉推到 y=40400，
已经超出内容底 39600）。首帧 `ops` 76 → 83，之后 899 帧零结构操作，exit 0。

全量回归（无窗口测试全跑，`tests/main-*.no` 是交互 demo 不计）：
```sh
$ for f in tests/*.no; do case "$f" in *main-*|*tmp-*) continue;; esac; ../bin/no run -cc ./ccwrap.sh "$f"; done
# app-assemble / app-layer / cmp-scene / components-core / components-smoke / core-v4 /
# dv-layout / dv-style / dv / dva-anchor / dva-bind / dva-disabled / dva-dock /
# dva-filter / dva-hover / dva-order / dva-overlay / dva-repeat / dva-scroll /
# dva-sel / dva-select / dva-table / dva-tabs / dva-toast / dva-valid /
# dva-virtual / dva / interaction / kernel2 / probe / sv / widget  —— 全部 PASSED
# kernel / t2 / t3 / test-kernel / test-l1 是早期遗留草稿（小写全局绑定，被现行
# 编译器的命名规则拒绝），与本阶段无关，阶段27 之前就是红的

$ cd tests/logic && ../../../bin/no test .
all-ok=true
ALL TESTS PASSED
```

## 验收记录（阶段29：声明式可拆面板）

`tests/dva-dock.no`（31 项断言，全过）：

```sh
$ ../bin/no run -cc ./ccwrap.sh tests/dva-dock.no
=== dva-dock tests ===
M mark=truetruetruetruetruetruetruetrue
L layout=truetruetruetruetruetrue
H hit=truetruetruetrue
D drag=truetruetruetruetruetruetrue
V vertical=truetruetruetruetrue
R rebuild=truetrue
all-ok=true
ALL DVA-DOCK TESTS PASSED
```

`M` 八项（r1..r8）：`dv-dock-split` 造出的是 `C-DOCK`；`L-H` 横向提示在位；
`gap` 就是声明的拖杆命中区；`value` = 左屏最小宽、`value2` = 最大宽；
动作是 `DV-A-CHANGE` 且载荷是状态槽；视图节点可见且可用；改状态重建零结构操作；
重建后上下限声明原样保留（夹紧界是**声明**的，不随拖拽漂移）。
`L` 六项：左屏 x = 容器内边距后的位置；左屏宽 = 状态槽；左屏高 = 声明值；
右屏 x = 左屏 x + 左屏宽 + `gap`；右屏右缘 = 容器右缘 − 内边距（grow 语义）；
右屏与左屏顶对齐、同高。`H` 四项：点左屏 -> 命中左屏；**点两屏之间那条间隙
-> 命中 `C-DOCK` 容器**（`cs-hit` 的自身矩形兜底）；点容器内边距 -> 也命中
容器；点容器外 -> 不命中。拖杆的全部依据就是这两条：间隙命中容器，而事件侧
还能凭光标是否落在「左屏右缘 .. 左屏右缘 + gap」区间把它和普通容器点区分开。
`D` 七项：按下抓住（`dva-drag-of` = dock）；移动按 `nv = px − 左屏.x` 换算并
写回状态槽（20000 − 2400 = 17600）；拖进左屏以内按下限 8000 夹紧；拖出右界按
上限 28000 夹紧；松手后再移动不动；点在面板内部不抓（命中的是面板）；点在容器
内边距不抓（命中的是 dock 但不在间隙）。`V` 四项：竖拆时上屏 y = 容器内边距后
位置、上屏高 = 状态槽；按间隙抓住；按 `py` 换算（20000 − 2400 = 17600）；
只甩 `px` 而 `py` 不动 -> 值不变（竖向换算只认 `py`）。`R` 两项：拖完重建视图
零结构操作；新布局里左屏宽 = 新状态（17600）。

demo：`tests/main-dv.no` 的两栏行从 `dv-row` 换成 `dv-dock-split` —— 都是
1 个节点，首帧 `ops` 仍是 83，之后 899 帧零结构操作：

```sh
$ ../bin/no build -cc ./ccwrap.sh -o dist/main-dv tests/main-dv.no
$ DYLD_LIBRARY_PATH=/opt/homebrew/opt/sdl3/lib:/opt/homebrew/opt/sdl3_ttf/lib ./dist/main-dv
bye frames=900 ops=83 clicks=0 rows=2 sel=0 pick=0 tst=0 scrl=0 sort=0 vsub=0 lsc=0 dock=22000
```

左屏初始 220px、最小 120px、最大 420px（夹紧上限），拖杆命中区 8px
（= `CS-GAP`，与容器默认间隙一致）。全量回归 32 个无窗口测试全部 PASSED
（列表见上，新增 `dva-dock`），`tests/logic` 仍 `all-ok=true`。

## 验收记录（阶段30：声明式代码编辑器）

`tests/dva-editor.no`（61 项断言，全过）：

```sh
$ ../bin/no run -cc ./ccwrap.sh tests/dva-editor.no
=== dva-editor tests ===
T lex=truetruetruetruetruetruetruetruetruetruetruetruetruetruetruetruetrue
E lines=truetruetrue…(6 true)
K edit=truetruetruetruetruetruetruetruetruetruetruetruetrue
W window=truetrue…(3 true)
F follow=truetrue…(4 true)
A assemble=truetruetruetruetruetruetruetruetrue
all-ok=true
ALL DVA-EDITOR TESTS PASSED
```

`T` 十八项（r1..r18）：`dv-lex-line` 的 token 数 / 文案 / 颜色 —— `''` → 0；
`';abc'` → 1 段注释（`CS-TH-MUTED`）；`'"hi"'` → 1 段字符串（`CS-TH-OK`）；
`'42'` → 数字（`DV-C-NUM`）；`'let'` → 关键字（`CS-TH-ACCENT`）；`'x'` → 普通
标识符（`CS-TH-FG`）；`'  '` → 一段空白；`'let x'` → 2 段（`let` / ` x`，**相邻
同色被 `dv-run-push` 并成一段**）；`'42 43'` → 3 段（`42` / ` ` / `43`）；
`'a;b'` → 2 段（`a` / `;b`，注释吃到行尾）；`'"a" "b"'` → 3 段；关键字表命中 /
不命中；`dv-lex-quote('"ab"cd', 0, '"') == 4`（配对引号）与 `dv-lex-quote('"ab',
0, '"') == 3`（未闭合吃到行尾）；`dv-byte-at` 越界给 0。
`E` 六项（r19..r24）：`dv-edit-split` / `dv-edit-join` 往返 —— `'a\nb'` → 2 行；
`'a\nb\n'` → **3 行**（结尾空行保留，否则「在最后一行末尾按回车」进不去）；
`''` → 1 行空串；`'x'` → 1 行。
`K` 十三项（r25..r37）：右移列 +1；插 `'Z'` → `'lZet x = 1'` 且列 +1；退格 →
`'let x = 1'`；回车 → `'l\net x = 1'`（光标 L1 C0）；上移回 L0 C0；下移回 L1 C0；
**Tab 插入 4 个空格且光标前进 4**（`'l\n    et x = 1'`，C4）；4 次左移到 L1 C0；
再左移**跨到上一行末尾** L0 C1；退格把两行并成 `'\n    et x = 1'` 且光标落到拼接处
C0；`'q'` 打得出来；跨行退格 `'ab\ncd'` + 下移 + 退格 → `'abcd'` L0 C2；行首退格到顶
不再吞字符（先右移两次再退格三次 → `''` C0）。
`W` 三项：`dv-win-range(0, 14400, 2400, 10)` → `[0, 5)`；`n = 0` → `[0, 0)`；
`off = 100000` → `[9, 10)`。
`F` 四项：光标在 L0 → 偏移 0 且状态槽不变；光标跳到 L8 → 窗口重排到 15600 且写回
状态槽；再调一次稳定在 15600（**光标没动就不重排**）；强制把偏移槽写回 0 而光标
不动 → 返回 0（**滚轮推走的窗口不会被弹回去**）。
`A` 十七项（r45..r61）：`dv-render` + `cs-layout` 后 `dv-live-deep(root, 900)` 是
`C-SCROLL`；`dv-edit-of(dv-node-by-key(900))` = 文本槽；声明内容高 = 8000（2 行）；
`dv-scroll-start` = 0；行节点 1001 在、光标节点 10001001 在；行号文案 = `'0'`；
命中编辑器内部 → `dva-edit-find` = 900，命中编辑器根本身也是 900，命中窗口根 /
`-1` 是 -1；`dva-focus(hit)` → `DVA-EDIT-FOCUS[0]` = 900；`dva-edit-keyboard(122)`
→ 文本变成 `'zlet x = 1\nlet y = 2'`；重建一次结构不变、**再重建一次 `ops` = 0**；
点空白 → 焦点释放；空文本仍有光标（编辑器根还在）。

窗口端到端：
```sh
$ ../bin/no build -cc ./ccwrap.sh -o dist/main-dv tests/main-dv.no
$ DYLD_LIBRARY_PATH=/opt/homebrew/opt/sdl3/lib:/opt/homebrew/opt/sdl3_ttf/lib ./dist/main-dv
noui on SDL 3.4.18
window handle: 512227049984
bye frames=900 ops=105 clicks=0 rows=2 sel=0 pick=0 tst=0 scrl=0 sort=0 vsub=0 lsc=0 dock=22000 edoff=0
```

左栏底部多了一个编辑器：`dv-code-editor(DEMO, 960, D-ED-TEXT, D-ED-OFF, 3200,
14400, 2400, 4800, 9700, lc)` + `dv-size(ed, dk, 14400)`，初始文本是
`'let name = "noui"\n; a comment\nlet n = 42\nlet ok = 1'`（关键字 / 字符串 / 注释 /
数字四样着色都齐了）。首帧 `ops` 83 → 105：1 个 `editor` 标题标签 + 编辑器 21 个
节点（滚动容器 + 内容容器 + 4 行；第 0 行 `let` 上带着光标，被拆成「空前缀 +
2px 光标 + `let`」所以比别的行多 2 个）。行号右对齐 0..3，行高 24px，视口 144px，
行号槽 48px，滚轮步长 32px。新增 `D-ED-OFF = 24`（`with-len(25)`）与字符串槽
`D-ED-TEXT = 6`（`dv-bind-t-ensure(7)`），页头多回显一个 `edoff=`，reset 多写
`D-ED-OFF = 0` + 把文本设回初始值。之后 899 帧零结构操作，exit 0。

## 验收记录（阶段31：声明式树）

`tests/dva-tree.no`（42 项断言，全过）：

```sh
$ ../bin/no run -cc ./ccwrap.sh tests/dva-tree.no
=== dva-tree tests ===
Z mark=truetruetruetruetruetruetruetruetruetruetruetruetruetruetrue
Y live=truetruetruetruetrue
D disp=truetruetruetruetruetruetruetruetruetrue
E e2e=truetruetruetruetruetruetruetruetruetrue
all-ok=true
ALL DVA-TREE TESTS PASSED
```

`Z` 十六项（r1..r16）：拍平 —— 13 节点 / 根 + src 展开 → `m = 8`，可见行表
`[0,1,5,6,7,2,3,4]`、层级 `[0,1,2,2,2,1,1,1]`；挂载出 `C-SCROLL`（key 50）带 8 个
孩子；行 0 是 `C-FORM`、`value = 0`、2 个孩子、动作 `DV-A-SET(D-SEL)`；箭头 0 =
`TR-ARROW-OPEN`（`v`）且宽 `TR-ARROW-W`；文本 = `'noui'`；src 行层级 1 → 箭头宽 =
`TR-INDENT + TR-ARROW-W`、仍 `v`；tests 行收起 → `>` 且箭头挂
`DV-A-TOGGLE(D-OPEN + 2)`；叶子（Makefile）箭头空、无动作、**宽度仍按层级给**；
内容高 = 27200（`8*2400 + 7*800 + 2*1200`）；起始行 0；选中态 —— 行 flags =
`dv-on-sel()`、行底色 = `cs-theme(CS-TH-TRACK)`、文本色 = `cs-theme(CS-TH-ACCENT)`
三处一致，换回未选中复原成 `dv-on()` / 底色 0。
`Y` 五项（r17..r21）：首帧 `ops = 1 + 8*3`；活树里行是 `C-FORM`、文本 `'noui'` 在；
只换选中 = **零结构操作**且活树行底色变成 `CS-TH-TRACK`；收起 src = 有结构操作且
节点 5 的活节点消失；再展开 = 有结构操作且节点 5 回来。
`D` 十项（r22..r31）：`dv-dispatch` 命中 src 箭头 = `DV-A-TOGGLE` + 载荷
`D-OPEN + 1`；命中行 = `DV-A-SET` + 载荷 `D-SEL`；归约后 src 收起；归约后选中 = 0；
叶子箭头 `dispatch` = 0；`DV-A-FILL`（`value = 1`，`value2 = 13`）一次写 13 个槽、
回执 13、拍平后 `m = 13`；`value = 0` 全收起 → 只剩根（`m = 1`，行 0 = 节点 0）；
`value2 = 0` 不写、回执 0、状态不变。
`E` 十一项（r32..r42）：首帧 `ops = 1 + 8*3`；`cs-layout` 后 `cs-hit` 命中 src 行
正中 = 该行的活句柄；派发 + 归约 → `D-SEL = 1`；重建 = **零结构操作**且活树行底色
变 `CS-TH-TRACK`；命中箭头正中 = 箭头活句柄；派发 = `DV-A-TOGGLE`、归约后
`D-OPEN + 1 = 0`；重建后节点 5 消失；偏移 100px → 滚动容器只剩 5 个孩子，首行 =
节点 6、末行 = 节点 4；全部展开 → 8 个孩子且 `cmp-scroll-max > 0`；选中节点 8 的
文本色 = `cs-theme(CS-TH-ACCENT)`。

窗口端到端：
```sh
$ ../bin/no build -cc ./ccwrap.sh -o dist/main-dv tests/main-dv.no
$ DYLD_LIBRARY_PATH=/opt/homebrew/opt/sdl3/lib:/opt/homebrew/opt/sdl3_ttf/lib ./dist/main-dv
noui on SDL 3.4.18
window handle: 509566524416
bye frames=900 ops=124 clicks=0 rows=2 sel=0 pick=0 tst=0 scrl=0 sort=0 vsub=0 lsc=0 dock=22000 edoff=0 tsel=-1 toff=0
```

左栏顶部多了一棵树：`dv-label(89, 'tree', lc)` + `dv-tree-view(DEMO, 980, D-TOFF,
3200, 16800, 2400, 13, DEMO-TREE-TEXTS, DEMO-TREE-K0, DEMO-TREE-KN, DEMO-TREE-PAR,
D-TOPEN, D-TSEL, 9800, lc)` + `dv-size(tr, dk, 16800)`，下方两个幽灵按钮
`expand all` / `collapse all`（同一个 `DV-A-FILL`，`value` 一个 1 一个 0，载荷 =
`D-TOPEN`）。13 个节点就是 noui 自己的目录（`noui` / `src` / `tests` / `docs` +
各级文件），初始根与 `src` 展开 → 可见 8 行。新增 `D-TOPEN = 25` / `D-TSEL = 38` /
`D-TOFF = 39`（`with-len(40)`）与 `DEMO-TREE-*`；文案数组 `DEMO-TREE-TEXTS` 用
`with-len(13)` + 逐项写（模块级 `[]str` **字面量**在函数体里读出来是空的，见
`ARCHITECTURE.md §5.25`），另两组 `[]i64` 字面量不受影响。页头多回显 `tsel=` /
`toff=`，reset 多写展开槽（根 + src 展开、其余收起）+ `D-TSEL = -1` + `D-TOFF = 0`；
启动处显式写 `DEMO[D-TOPEN] = 1` / `DEMO[D-TOPEN+1] = 1` / `DEMO[D-TSEL] = -1`
（demo 不走 reset 命令，所以初始态要自己写全）。首帧 `ops` 105 → 124（1 个标题标签 +
树 8 行 × 3 + 滚动容器 + 2 个按钮 = 19），之后 899 帧零结构操作，exit 0（连跑两次
输出逐字节一致）。

全量回归（无窗口测试全跑，`tests/main-*.no` 是交互 demo 不计）：**PASS=34 FAIL=5**
—— 阶段30 的 33 个 PASSED 加 `dva-tree`；红的仍是 `kernel` / `t2` / `t3` /
`test-kernel` / `test-l1` 这 5 个早期遗留草稿（小写全局绑定，被现行编译器的命名规则
拒绝），与本阶段无关。`src/*.no` 全量 `no vet`：`dv.no` 零错误 155 hints（与阶段30
基线一致），`dv-app.no`（经 demo 构建）/ `cmp-scene.no` / `app.no` / `geom.no` 零错误，
`dv-app.no` 单文件 vet 的 7 处「未定义」（`dv-app-reduce` / `-click` / `-view` 三个
装配层名字，2026-11-22 实测）是装配层固有（要由 import 它的程序提供），
`entity.no` / `pm-*.no` / `pushmod*.no` 是内核
草稿，`d-struct.no` 是空文件。

## 验收记录（阶段32：声明式命令面板）

`tests/dva-palette.no`（34 项断言，全过）：

```sh
$ ../bin/no run -cc ./ccwrap.sh tests/dva-palette.no
=== dva-palette tests ===
Z mark=truetruetruetruetruetruetruetruetruetruetruetruetruetrue
Y live=truetruetruetruetruetrue
D disp=truetruetruetruetruetruetrue
E e2e=truetruetruetruetruetruetrue
all-ok=true
ALL DVA-PALETTE TESTS PASSED
```

`Z` 十四项（r1..r14）：关着 → `dv-node-by-key(P-KEY) < 0` 且 `dv-pal-open-key(ST)`
= -1；开着 → 面板根是 `C-FORM` 且 flags = `dv-on-overlay()`；输入是 `C-INPUT`
且 `dv-bind-of` = 查询槽；列表是 `C-SCROLL` 且 8 个孩子；行 0 = `C-FORM`、
`value` = `P-ACTS[0]`、动作 `DV-A-RUN`、载荷 = 命令码槽；`value2` = 开关槽；
文本 = `'new file'`；游标在 0 → 行 0 flags = `dv-on-sel()`、行 1 = `dv-on()`；
行 key 按**源行号**（源行 5 = `'find in files'`、`value` = `P-ACTS[5]`）；
内容高 = 30400（`8*2800 + 7*800 + 2*1200`）；命中数 = 8 / 游标 = 0 /
`dv-pal-cur-key` = `P-KEY0`；`dv-pal-open-key` = `P-KEY`；`dv-pal-of(面板根)` =
`P-KEY`；关面板 → 整棵子树消失且 `open-key` 回 -1。
`Y` 六项（r15..r20）：首帧 `ops = 3 + 8*2`；活树里行是 `C-FORM`、文本 `'new file'`
在；查询改成 `'s'` → 有结构操作且命中数 = 4（`save` / `save as` / `close` /
`find in files` 都含 s）；`dv-pal-move` 下移 → **零结构操作**且游标 = 1；
退格回空查询 → 8 行全回来且 `dv-bound(D-Q)` = `''`；关面板 → 有结构操作且活树
里的面板根消失。
`D` 七项（r21..r27）：`dv-dispatch(P-KEY0)` = `DV-A-RUN` + 载荷 = 命令码槽；
`dv-apply` 回执 2 且 `ST[D-CMD]` = `P-ACTS[0]` / `ST[D-OPEN]` = 0；过滤 `'sa'`
→ 命中 2 行、游标 0；过滤 `'z'` → 命中 0、游标 -1、`dv-pal-cur-key` = -1；
空命中时移动游标不动；`dv-pal-type(pk,'f')` → `'zf'`；`dv-pal-backspace` → `'z'`。
`E` 七项（r28..r34）：`cs-layout` 后 `cs-hit` 命中行正中 = 行自己**或其父**
（`cs-hit` 返回最深节点，行内那个文本标签也会被命中）；派发 + 归约 = `DV-A-RUN` +
回执 2 + 写命令码 + 清开关槽；面板焦点下 `↓` → 游标 1 且 `dv-pal-cur-key` =
`P-KEY0 + 100`；回车 → `ST[D-CMD]` = `P-ACTS[1]` / `ST[D-OPEN]` = 0 / 归约命中
计数 1；可打印字符（`'q'` = 113）直接进查询串 → `dv-bound` = `'q'` 且命中 1 行
（面板自己收键盘，不需要先点输入框）；ESC → `ST[D-OPEN]` = 0；命中面板里的输入
框时 `dva-pal-find` 仍 = 面板 key（装配层据此不把焦点交给它）。

窗口端到端：
```sh
$ ../bin/no build -cc ./ccwrap.sh -o dist/main-dv tests/main-dv.no
$ DYLD_LIBRARY_PATH=/opt/homebrew/opt/sdl3/lib:/opt/homebrew/opt/sdl3_ttf/lib ./dist/main-dv
noui on SDL 3.4.18
window handle: 4351350688
bye frames=900 ops=125 clicks=0 rows=2 sel=0 pick=0 tst=0 scrl=0 sort=0 vsub=0 lsc=0 dock=22000 edoff=0 tsel=-1 toff=0 pal=0
```

左栏树下面多了一个主色 `palette` 按钮（`DV-A-SET(D-PAL-OPEN)` + 快捷键 `p` = 112）
和一行 `dv-command-palette(DEMO, 99, D-PAL-OPEN, D-PAL-Q, D-PAL-OFF, D-PAL-CMD, 8,
DEMO-PAL-TEXTS, DEMO-PAL-ACTS, 2000, page)`。8 条命令（`new file` / `open file` /
`save` / `save as` / `close tab` / `find in files` / `toggle terminal` / `quit`）
的文案数组同样用 `with-len(8)` + 逐项写。新增 `D-PAL-OPEN = 40` /
`D-PAL-OFF = 41` / `D-PAL-CMD = 42`（`with-len(44)`）与字符串槽 `D-PAL-Q = 7`
（`dv-bind-t-ensure(8)`）；页头多回显 `pal=`，reset 多写 `D-PAL-OPEN = 0` /
`D-PAL-CMD = 0` + `dv-bound-set(D-PAL-Q, '')` + `dv-pal-reset(99)`。执行走显式
命令（`pay == D-PAL-CMD`）：归约表达不了「顺带弹个通知」，所以补一条
`D-TOAST = dv_frame()`。行 key 段选 `2000 + 源行号*100`，避开编辑器的
`960` / `9700+` 与树的 `980` / `9800+`（撞了 `dv-sync` 会按 `(key, kind)`
复用错组件）。首帧 `ops` 124 → 125（多的 1 个是触发按钮；面板关着时只登记 key、
不建子树），把面板打开跑一遍是 144（再多 19 个节点 = 面板根 + 输入 + 列表 +
8 行 × 2），`bye … pal=0`，exit 0（连跑两次输出逐字节一致，只有 window handle
这个指针不同）。

全量回归（无窗口测试全跑，`tests/main-*.no` 是交互 demo 不计）：**PASS=35 FAIL=5**
—— 阶段31 的 34 个 PASSED 加 `dva-palette`；红的仍是 `kernel` / `t2` / `t3` /
`test-kernel` / `test-l1` 这 5 个早期遗留草稿（小写全局绑定，被现行编译器的命名规则
拒绝），与本阶段无关。`src/*.no` 全量 `no vet`：`dv.no` 零错误 155 hints（与阶段30/31
基线一致），`dv-app.no`（经 demo 构建）/ `cmp-scene.no` / `app.no` / `geom.no` 零错误，
`dv-app.no` 单文件 vet 的 7 个「未定义」是装配层固有（要由 import 它的程序提供
`dv-app-reduce` / `-click` / `-view`），`entity.no` / `pm-*.no` / `pushmod*.no` 是内核
草稿，`d-struct.no` 是空文件。

## 验收记录（阶段33：命令式树）

`tests/cmp-tree.no`（36 项断言，全过）：

```sh
$ DYLD_LIBRARY_PATH=/opt/homebrew/opt/sdl3/lib:/opt/homebrew/opt/sdl3_ttf/lib \
  SDL_VIDEODRIVER=dummy SDL_AUDIODRIVER=dummy ./dist/cmp-tree
ctor=truetruetrue model=truetruetruetrue
flat=truetruetruetruetruetruetruetrue
sync=truetruetruetruetruetruetruetrue
select=truetruetrue resync=truetruetrue
hit=truetruetrue isolate=truetruetruetrue
all-ok=true
ALL TREE TESTS PASSED
```

八组：`ctor` 三项（`cmp-kind` = `C-TREE` / `cmp-value` = -1 / 节点数 0）；`model`
四项（5 个节点的文本、`cmp-tree-kids` = 2/1/0、`cmp-tree-has-kids`）；`flat` 八项
（全收起 → 2 行 `[r0,b0]` 层级 `[0,0]`；展开 `r0` → 4 行 `[r0,a0,a2,b0]` 层级
`[0,1,1,0]`；再展开 `a0` → 5 行且 `f3[2]` = `a1` / 层级 2；`cmp-tree-toggle` 翻回
2 行）；`sync` 八项（行数 = 5 = `cmp-children(t1)`；`cmp-tree-node-of` 把行映回
`r0`/`a1`/`b0`；行是 `L-H` 的 `C-FORM` 且 2 个孩子；箭头标签是 `C-LABEL` /
`A-RIGHT` / 文本 `CT-OPEN` / `cmp-sw` = `CT-ARROW-W`；文本标签文本 `'r0'`；叶子箭头
文本 `''` 且 `cmp-sw` = `2*CT-INDENT + CT-ARROW-W`；未选中行底色 0 / 文本色 FG）；
`select` 三项（`cmp-tree-select` + 重 sync → 行底色 = SELB、带 `F-SELECTED`、文本色
= ACC）；`resync` 三项（旧行 `cmp-parent-of` = -1、`TR-ROW-NODE` / `TR-ARROW` 已清、
仍是 5 行不翻倍）；`hit` 三项（从文本标签 / 箭头 / 行自身上溯都得到行，从树本身上溯
得 -1，`cmp-tree-arrow-of` = 箭头）；`isolate` 四项（第二棵树 2 节点、`t1` 仍 5 节点、
`t1` 拍平仍 5 行、`cmp-tree-sync(t2)` 后 `cmp-children(t1)` 仍 5）。

窗口端到端（真实布局坐标下的命中链探针，跑完即删）：4 行树 `cs-layout` 后逐行取
`cs-rect` 中心点 `cs-hit`，`cmp-tree-row-of` 全部解析回该行自己（`node=2/3/4/5`），
箭头槽中心点也命中行 0，`checks=5 bad=0`，证明 demo 的点击路径（`cs-hit` →
`cmp-tree-row-of` → `cmp-tree-node-of` / `cmp-tree-arrow-of`）真的通。

```sh
$ ../bin/no build -cc ./ccwrap.sh -o dist/main-tree tests/main-tree.no
$ DYLD_LIBRARY_PATH=/opt/homebrew/opt/sdl3/lib:/opt/homebrew/opt/sdl3_ttf/lib \
  SDL_VIDEODRIVER=dummy SDL_AUDIODRIVER=dummy ./dist/main-tree
noui on SDL 3.4.18
window handle: 4390043696
bye frames=900 clicks=0
```

`tests/main-tree.no` 走的是和 `tests/main-form.no` 同一条纯命令式路子（顶层标量存状态、
主循环直接读写，没有声明式视图层）：左栏 `C-SCROLL`（定宽 22000 / 高 39600）包着
`cmp-tree()`，19 个节点（src 6 / tests 4 / docs 3 / tmp 2 + 4 个根）全展开 → 19 行 ×
2400 = 45600 > 视口 39600，滚轮因此真的能用；右栏详情面板回显选中节点的
`node` / `depth` / `kids` / `open` 与 `rows / nodes`。交互：点行选中、点箭头切换展开
（叶子没箭头，点了只选中）、↑↓ 在可见行里移动选中、←→ 收起/展开选中节点、Enter 切换、
滚轮滚动（命中顺着父链找到 `C-SCROLL` 才滚，夹紧到 `cmp-scroll-max`）、`e` / `c` 全展开
/ 全收起、`r` 重置、ESC / `q` 退出。900 帧后 `bye frames=900 clicks=0`，exit 0。

全量回归（无窗口测试全跑，`tests/main-*.no` 是交互 demo 不计）：**PASS=36 FAIL=5**
—— 阶段32 的 35 个 PASSED 加 `cmp-tree`；红的仍是 `kernel` / `t2` / `t3` /
`test-kernel` / `test-l1` 这 5 个早期遗留草稿（小写全局绑定，被现行编译器的命名规则
拒绝），与本阶段无关。`src/*.no` 全量 `no vet`：`components.no` 零错误 4 warnings
134 hints（警告数与阶段30–32 基线一致），`dv.no` 零错误 159 hints，`cmp-scene.no` /
`app.no` / `geom.no` 零错误，`dv-app.no` 单文件 vet 的 7 个「未定义」是装配层固有，
`entity.no` / `pm-*.no` / `pushmod*.no` 是内核草稿，`d-struct.no` 是空文件。
`dist/main-dv` 已按新 `components.no` 重跑，输出与阶段32 逐字节一致（`ops=125`）。

## 验收记录（阶段34：命令式代码编辑器）

`tests/cmp-editor.no`（65 项断言，11 组全过）：

```sh
$ DYLD_LIBRARY_PATH=/opt/homebrew/opt/sdl3/lib:/opt/homebrew/opt/sdl3_ttf/lib \
  SDL_VIDEODRIVER=dummy SDL_AUDIODRIVER=dummy ./dist/cmp-editor
ctor=truetruetruetruetrue text=truetruetruetruetruetrue
clamp=truetruetruetrue edit=truetruetruetruetruetruetruetrue
move=truetruetruetruetruetrue key=truetruetruetruetruetrue
win=truetruetruetrue follow=truetruetruetruetrue
sync=truetruetruetruetruetrue
color=truetruetruetruetrue isolate=truetrue
all-ok=true
ALL EDITOR TESTS PASSED
```

`ctor` 五项（`cmp-kind` = `C-SCROLL` / `value` = 0 / `value2` = 3200 / `pad` = 1200 /
`gap` = 800）；`text` 六项（`set-text` 后 7 行、`text()` 往返一致、`del-line(0)` **真的
少一行**、`ins-line` 插一行、末行空行保留所以「最后一行末尾按回车」进得去）；
`clamp` 四项（空文本仍 1 行、光标行列夹紧、`set-cur` 越界收敛、删行后光标跟着夹）；
`edit` 九项（插字符 / 退格 / **跨行退格并入上行且光标落到拼接处** / 回车拆分 /
Tab 插 4 空格且列 +4 / `''` 空文本）；`move` 七项（左右 / 上下 / 跨行 / 行首行尾 /
空行）；`key` 六项（四个 `ED-K-*` / 退格 8 / 回车 13 / Tab 9 / `ED-PRINT` 可打印 /
其它键忽略）；`win` 四项（`cmp-win-range` 的 `i0` / `i1` / 负偏移 / `n = 0`）；
`follow` 六项（光标没动不重排窗口、动了才重排、重复调用稳定、**偏移槽不被弹回**、
`ED-SEEN` 生效）；`sync` 八项（行数 = 可见行数、行号文案 0..6、光标行被拆成
「前缀 + 2px 光标 + 后缀」、`ED-ROW` 标记、声明内容高 / 起始行号、旧行摘掉不翻倍）；
`color` 七项（关键字 `CS-TH-ACCENT` / 字符串 `CS-TH-OK` / 注释 `CS-TH-MUTED` /
数字 `ED-C-NUM` / 标识符与普通文本 `CS-TH-FG` / 空白 / **相邻同色合并**）；
`isolate` 三项（两个编辑器互不影响、光标各自独立、各挂各的行）。

窗口端到端：

```sh
$ ../bin/no build -cc ./ccwrap.sh -o dist/main-editor tests/main-editor.no
$ DYLD_LIBRARY_PATH=/opt/homebrew/opt/sdl3/lib:/opt/homebrew/opt/sdl3_ttf/lib \
  SDL_VIDEODRIVER=dummy SDL_AUDIODRIVER=dummy ./dist/main-editor
noui on SDL 3.4.18
window handle: 4323387664
bye frames=900 keys=0 clicks=0 rows=7
editor render OK
```

`tests/main-editor.no` 走的是和 `tests/main-tree.no` 同一条纯命令式路子（顶层标量存状态、
主循环直接读写，没有声明式视图层）：左栏就是 `cmp-editor()`（`L-GROW`，`sh` 跟
窗口高），右栏状态面板回显 `lines` / `cursor:l:c` / `offset` / `rows` / `keys+clicks`。
交互：打字 / 退格 / 回车 / Tab / 方向键（`cmp-editor-key`）、点行把光标跳到该行行首
（`cs-hit` → `cmp-editor-row-of` → 行在滚动容器子链里的下标 + `cmp-scroll-start`
算绝对行号）、滚轮（命中顺父链找到 `C-SCROLL` 才滚，夹紧到 `cmp-scroll-max`）、
`r` 重置、ESC / `q` 退出。重建段平铺在主循环顶层（没抽 `ed-refresh()`）：阶段34 时
`cmp-editor-sync` 包进函数再调会拿到 `undef` 句柄（`kids=0` 甚至段错误），根因是
编译器没把 `ED` 登记成模块存储，2026-11-22 已修，详见 `ARCHITECTURE.md §5.28`。
900 帧 `bye frames=900 keys=0 clicks=0 rows=7`，
exit 0；命中 / 点击上溯那两个小函数走函数路径实测正确（`hit=1 click=1 line=0`）。

全量回归（无窗口测试全跑，`tests/main-*.no` 是交互 demo 不计）：**PASS=37 FAIL=5**
—— 阶段33 的 36 个 PASSED 加 `cmp-editor`；红的仍是 `kernel` / `t2` / `t3` /
`test-kernel` / `test-l1` 这 5 个早期遗留草稿（小写全局绑定，被现行编译器的命名规则
拒绝），与本阶段无关。`src/*.no` 全量 `no vet`：`components.no` 零错误 4 warnings
137 hints（警告数与阶段30–33 基线一致，hints 比阶段33 多 3 条 = 新增编辑器代码的
溢出注解提示），`cmp-scene.no` / `app.no` / `dv.no` 零错误，`tests/cmp-editor.no` /
`tests/main-editor.no` 零错误；`dv-app.no` 单文件 vet 的 7 个「未定义」是装配层固有。
`dist/main-dv` 已按新 `components.no` 重跑，输出与阶段33 逐字节一致（`ops=125`）。

## 验收记录（阶段35：命令式虚拟表格）

`tests/cmp-table.no`（57 项断言，13 组全过）：

```sh
$ DYLD_LIBRARY_PATH=/opt/homebrew/opt/sdl3/lib:/opt/homebrew/opt/sdl3_ttf/lib \
  SDL_VIDEODRIVER=dummy SDL_AUDIODRIVER=dummy ./dist/cmp-table
ctor=truetruetrue model=truetruetruetruetrue key=truetrue
sync=truetruetruetruetruetruetruetruetruetruetruetrue
sel=truetruetruetrue hit=truetruetruetruetruetrue
off=truetruetrue two=truetruetruetrue
order=truetruetrue stable=truetruetruetruetrue desc=truetruetrue
empty=truetruetruetruetrue redef=truetruetruetruetrue
all-ok=true
ALL TABLE TESTS PASSED
```

窗口 demo（`tests/main-table.no`）：

```sh
$ DYLD_LIBRARY_PATH=/opt/homebrew/opt/sdl3/lib:/opt/homebrew/opt/sdl3_ttf/lib \
  SDL_VIDEODRIVER=dummy SDL_AUDIODRIVER=dummy ./dist/main-table
noui on SDL 3.4.18
window handle: 4321866160
bye frames=900 keys=0 clicks=0 rows=11 sel=-1
table render OK
```

`tests/main-table.no` 走的是和 `tests/main-editor.no` / `tests/main-tree.no` 同一条
纯命令式路子：3 列 20 行，左栏 `cmp-table(3, 20)`（`L-GROW`，`sh` 跟窗口高），
右栏状态面板回显 `sel` / `cell:行:列` / `off` / `max` / `rows` / `mode` /
`keys+clicks`。交互：点行选中（选中存源行号，排序后仍指同一行）、点单元格把
`cmp-table-cell-rc` 的结果（`ct-get` 读 `[]i64` 出参）回显到右栏、
`1` / `2` / `0` = 升序 / 降序 / 源顺序（`cmp-table-sort` 是对 `TB-ORDER` 的
**稳定**插入排序）、滚轮（`tb-hit-in` 顺 `cmp-parent-of` 上溯到表格根才滚，
夹紧到 `cmp-scroll-max`）、`r` 重置、ESC / `q` 退出。

两条坑（详见 `ARCHITECTURE.md §5.29`）：

1. `CHILD-IDX` 是全局扁平数组，**一个父的所有直接子必须先连续挂完，才能去填
   任何一棵子树**。`cmp-table-sync` 最初建完表头就填标签，3 个标签插进了表头和
   滚动体之间，`cmp-child(h,1)` 拿到 `C-LABEL` 而不是滚动体（探针实测
   `k0=92 k1=1 kidsB=0`）。改成「先挂 `hg` 和 `scr`，再回头填表头」后
   `k1=85 kidsB=8 content=33600`。
2. `;` 在 nolang 里是**行注释**，不是语句分隔符。键盘分支体写成
   `{ cmp-table-sort(0); tb-mode = 1; ... }` 会把收尾 `}` 一起注释掉，块一路
   吃到后面的 `}`，主循环被重新嵌套 —— 窗口活着但 `tb-frames` 永远不自增，
   headless 下 `loop f=0 / poll t=0` 无限刷屏。诊断靠两层二分：先按四个事件
   分支逐个单独留全 body（只有键盘分支挂），再在分支内部逐语句二分（单句全过、
   三句合体挂），最后定位到句间分隔符。绕法：分支体里多句一律换行写。

全量回归（无窗口测试全跑，`tests/main-*.no` 是交互 demo 不计）：**PASS=38 FAIL=5**
—— 阶段34 的 37 个 PASSED 加 `cmp-table`；红的仍是 `kernel` / `t2` / `t3` /
`test-kernel` / `test-l1` 这 5 个早期遗留草稿（小写全局绑定，被现行编译器的命名规则
拒绝），与本阶段无关。`tests/logic/l1-l3.no` 11 项断言全绿。
`src/*.no` 全量 `no vet`：`components.no` 零错误 4 warnings 137 hints
（三项计数与阶段34 基线完全一致），`tests/cmp-table.no` / `tests/main-table.no`
零错误。`dist/main-dv` 按新 `components.no` 重跑，900 帧
`bye frames=900 ops=125 ... pal=0`，连跑两次除窗口句柄外逐字节一致 —— 声明式层
读的是同一份 `cmp-alloc`，没有任何结构变化。

## 验收记录（阶段36：命令式可拆面板）

`tests/cmp-dock.no`（58 项断言，8 组全过）：

```
$ ../bin/no build -cc ./ccwrap.sh -o dist/cmp-dock tests/cmp-dock.no
$ DYLD_LIBRARY_PATH=/opt/homebrew/opt/sdl3/lib:/opt/homebrew/opt/sdl3_ttf/lib \
  SDL_VIDEODRIVER=dummy SDL_AUDIODRIVER=dummy ./dist/cmp-dock
=== cmp-dock tests ===
mark=truetruetruetruetruetruetruetruetruetruetruetruetruetruetrue
clamp=truetruetruetruetruetruetrue
layout=truetruetruetruetruetruetruetruetruetruetrue
hit=truetruetruetrue
drag=truetruetruetruetruetruetruetruetruetrue
vert=truetruetruetruetruetruetruetrue
iso=truetrue plain=truetrue
all-ok=true
ALL DOCK TESTS PASSED
```

窗口 demo（`tests/main-dock.no`）：

```
$ ../bin/no build -cc ./ccwrap.sh -o dist/main-dock tests/main-dock.no
$ DYLD_LIBRARY_PATH=/opt/homebrew/opt/sdl3/lib:/opt/homebrew/opt/sdl3_ttf/lib \
  SDL_VIDEODRIVER=dummy SDL_AUDIODRIVER=dummy ./dist/main-dock
noui on SDL 3.4.18
window handle: 4380790064
init-done split=12000
bye frames=900 split=12000 sel=-1 clicks=0 moved=0
dock render OK
```

`tests/main-dock.no` 走的是和 `tests/main-table.no` / `main-editor.no` / `main-tree.no`
同一条纯命令式路子：`cmp-dock()` 造容器，`cmp-dock-split(d, 12000, 60000, 1200)`
把它声明成可拆（间隙 = 拖杆命中区），左 `explorer` 12 行标签、右 `detail`
5 个状态标签。主循环 `t == 2` 先试 `cs-dock-down`，抓住就置 `dk-drag` 并跳过
点击路径；`t == 3` 拖着时 `cs-dock-move` 换算光标；`t == 6`（左键抬起）
`cmp-dock-up` 松手。每帧把 `DK-SPLIT-V` 同步到左屏的 `cmp-set-sw` ——
和 `cmp-table-sync` 同一模式：状态在应用手里，组件树只负责摆。

坑三条（详见 `ARCHITECTURE.md §5.30`）：

1. `cmp-dock-split` 把 `minw` / `maxw` 写进 `value` / `value2` 当夹紧界，
   所以 `value` **不能**拿来存当前位置。`cs-dock-move` 最初写的是
   `cmp-set-value(d, nv)`，第一次拖完下界就变成光标换算值，之后
   `clamp(nv, 17600, 28000)` 永远 ≥17600，下界夹紧永久失效。绕法：位置真源
   另开一个随 `cmp-alloc` 逐槽长的池 `DK-SPLIT-V`，和 `TB-OFF` / `ED-OFF`
   同一个路子。`tests/cmp-dock.no` 的 `dg3` / `dg5` 直接钉住这条。
2. `print` 的 `{field}` 只接受**调用之前已绑定**的简单标识符。排查时把
   `print('A l1={l1} ...')` 写在赋值之前，直接编译错
   `unresolved format field l1`（不是运行期空值）。
3. 嵌套在另一个分支体里的 `->` else 臂必须自己包 `{ ... }`，否则报
   `an else arm ('->' on its own line) must live inside an if-group`。
   顺带两条 lint：标识符只能小写字母 + 连字符（`paneA` → `pane-a`），
   `[]str` 这类类型标注会被 hint 成可省略；`NAMES[i]` 读出来是 `?str`，
   要两步解包。断言变量别用 `l3` / `l4` 这种两字符短名 —— 加三行无关探针
   就能让结果翻转，改成 `lay3` / `lay4` 后两次构建两次运行完全一致。

全量回归（无窗口测试全跑，`tests/main-*.no` 是交互 demo 不计）：**PASS=39 FAIL=5**
—— 阶段35 的 38 个 PASSED 加 `cmp-dock`；红的仍是 `kernel` / `t2` / `t3` /
`test-kernel` / `test-l1` 这 5 个早期遗留草稿（小写全局绑定，被现行编译器的命名规则
拒绝），与本阶段无关。`tests/logic/l1-l3.no` 11 项断言全绿。
`src/*.no` 全量 `no vet`：`components.no` 零错误 4 warnings 137 hints
（三项计数与阶段34/35 基线完全一致），`cmp-scene.no` 零错误 0 warnings 160 hints，
`tests/cmp-dock.no` / `tests/main-dock.no` 零错误。
`dist/main-dv` 按新 `components.no` 重跑，900 帧
`bye frames=900 ops=125 ... dock=22000 ...`，连跑两次除窗口句柄外逐字节一致 ——
声明式层读的是同一份 `cmp-alloc`，没有任何结构变化。

## 验收记录（阶段37：命令式命令面板）

`tests/cmp-command.no`（90+ 项断言，12 组全过）：

```
$ ../bin/no build -cc ./ccwrap.sh -o dist/cmp-command tests/cmp-command.no
$ DYLD_LIBRARY_PATH=/opt/homebrew/opt/sdl3/lib:/opt/homebrew/opt/sdl3_ttf/lib \
  SDL_VIDEODRIVER=dummy SDL_AUDIODRIVER=dummy ./dist/cmp-command
ctor=truetruetruetrue table=true query=truetruetruetrue
sync=truetruetruetruetruetruetruetruetruetruetruetruetruetruetruetrue
hit=truetruetruetrue cur=true move=truetruetruetruetrue
filter=truetruetruetruetruetruetruetruetruetruetruetruetruetruetrue
empty=truetruetruetruetruetruetrue clamp=truetrue
off=truetruetruetruetruetruetrue resync=truetruetruetruetrue
two=truetruetruetruetruetruetruetrue
ALL=true
```

窗口 demo（`tests/main-command.no`）：

```
$ ../bin/no build -cc ./ccwrap.sh -o dist/main-command tests/main-command.no
$ DYLD_LIBRARY_PATH=/opt/homebrew/opt/sdl3/lib:/opt/homebrew/opt/sdl3_ttf/lib \
  SDL_VIDEODRIVER=dummy SDL_AUDIODRIVER=dummy ./dist/main-command
noui on SDL 3.4.18
window handle: 4322458896
init-done cmds=12
bye frames=900 rows=8 keys=0 clicks=0 runs=0
command render OK
```

面板是**应用调出来的**，不是新节点种类：`cmp-command()` 的根就是 `C-COMMAND`
（内核早就认它是容器，但没有任何专用绘制分支），`cmp-command-sync` 在上面挂
两个直接子 —— `C-INPUT` 查询框 + `L-GROW` 的 `C-SCROLL` 列表 —— 再往滚动体里
两段式重建可见行（先挂全部 `C-FORM` 行壳，再逐行填 `C-LABEL` 文本）。声明式
`dv-command-palette` 的四件事在命令式侧的落点：查询过滤 = `cp-filter`（大小写
不敏感子串，空查询命中全部）；虚拟列表 = `cmp-win-range` +
`CMP-SCROLL-CONTENT` / `CMP-SCROLL-START`；键盘导航 = `cmp-command-move` /
`-run`（游标存 `CP-CUR`）；执行 = 行挂源行号（`CP-ROW-SRC`），点行 / 回车都取
`CP-ACT[源行号]`。模态标记由应用自己 `cmp-flag-set(panel, F-OVERLAY)`，
`components.no` 不写死模态行为。

游标是**命中表下标**而不是源行号 —— 查询一变命中表就变，拿源行号当游标会指错
行。命中表抄一份只增的扁平副本（`CP-SRC` + 每面板基址 `CP-SB`），于是
`cmp-command-move` 不重建视图也能立刻把「游标 -> 源行号」算出来（和
`dv-pal-cur-key` 同一个理由）。代价：`CP-SRC` 只增不减，随每次 sync 变长 ——
和 `DV-PAL-SRC` 一样有界。

坑五条（详见 `ARCHITECTURE.md §5.31`）：

1. **`CHILD-IDX` 是全局扁平数组，一个父的所有直接子必须占一段连续槽位**：
   `cmp-add-child` 只在末尾追加，且只在 `CMP-COUNT[parent] == 0` 时重设
   `CMP-HEAD[parent]`。最初版每轮 sync 都「摘掉滚动体 -> 挂新滚动体」，结果
   新滚动体落在面板既有子链之外，`cmp-children` 从 2 一路涨到 3、4…
   （探针实测 `k1=2 ok=true k2=1 k3=2 ok2=false k4=2`）。绕法：输入框 +
   滚动体**第一次建好就复用**（顺带解决每次重建输入框丢焦点），只重建滚动体
   里的行；而重建行时必须**全摘** —— 全摘后 `COUNT` 归零，下一轮
   `cmp-add-child` 会重设 `HEAD`，新挂的行才是连续的一段。和 `cmp-tree-sync` /
   `cmp-table-sync` 同一个约束。
2. **同一个标识符在一个函数里带两种类型会误编译**（本轮最贵一坑）：
   `cmp-command-sync` 里 `r` 既是 `i64` 循环变量、后面又
   `r []i64 = with-len(2)`，MIR→LLVM 报
   `argument 2 of '@ct_set': parameter 'i64' expects a scalar (LLVM i64) but the
   argument has aggregate type '[]i64' (LLVM %vec)`（`@cmp_detach` 同样报错）；
   但有的构建不报错，而是运行期**静默退出**（exit 0，只打印前几个探针）。
   改成 `rw` 后一切正常。排查「程序跑一半没了」先查这个。
3. **`**` 写在顶层 / 循环外是干净退出**（exit 0，之后无输出），所以第 2 条那种
   误编译的表象是「静默停住」而不是崩溃 —— 两个要一起记。
4. **`ct-get(arr, -1)` 不返回 -1**（越界读是未定义值），别拿负下标当哨兵探针；
   探「是不是面板行」用 `cmp-command-row-src(x) >= 0`。
5. **`cs-hit` 对模态 overlay 点在外面返回 `-2`**（屏障：什么都没命中），命中
   内部 ≥ 0，`-2` 会一路向上传播、不落穿到下面的节点 —— 「点面板外就关面板」
   靠这个，不用自己再算一遍几何。

全量回归（无窗口测试全跑，`tests/main-*.no` 是交互 demo 不计）：**PASS=40 FAIL=5**
—— 阶段36 的 39 个 PASSED 加 `cmp-command`；红的仍是 `kernel` / `t2` / `t3` /
`test-kernel` / `test-l1` 这 5 个早期遗留草稿（小写全局绑定，被现行编译器的命名规则
拒绝），与本阶段无关。`tests/logic/l1-l3.no` 11 项断言全绿（`ALL TESTS PASSED`）。

**一处环境扰动（已排查，与阶段37 无关）**：`tests/dva-editor.no` 在这轮里出现
`Error: signal: trace/BPT trap`（exit 133），陷阱在 `cmp_alloc` 末尾（`bl _str_push`
之后一条 `brk #0x1`）。三条证据把它钉在环境上而不是本阶段代码上：① 把 `components.no`
里整个阶段37 段落（`cmp-alloc` 的 6 行 `CP-*.push` + `12c` 全节，3218 -> 2968 行）
删掉再编译，**照样 3/3 陷阱**；② 同一份源码、同一个编译器，`dva-palette` / `dva-tree` /
`dva-dock` / `dva-table` / `dva-scroll` / `dva-tabs` / `dv` 七个声明式测试全过，只有
`dva-editor` 中；③ 同一次回归里 `dva-editor` 又能 PASS（14:04 那轮就是），而
`md5` 相同的两次构建结果一致 —— 说明它随外部文件漂移，不随本阶段改动。同期有并发
session 正在改写仓库根 `src/std/dom.no`（13:59）、`src/std/bigint.no`（14:00）和
`bin/no`（14:00）。**结论：`dva-editor` 的红是工具链/环境漂移，不是阶段37 引入的回归**；
阶段37 的两个测试（`cmp-command` / `main-command`）在冻结工具链下稳定全绿。
`src/*.no` 全量 `no vet`：`components.no` 零错误 4 warnings 137 hints（三项计数
与阶段34/35/36 基线完全一致，4 条 warning 是阶段34 之前就有的
`nolang-overflow-ineffective`），`tests/cmp-command.no` / `tests/main-command.no`
零错误。至此**命令式侧五个功能对等长期项（DataTable / Dock / 代码编辑器 / 树 /
命令面板）全部补齐**，`components.no` 里不再有「只有 API + 最小状态」的组件。

## 验收记录（阶段38：命令式输入框富文本 / 语法高亮）

立项理由：组件目录里 `C-INPUT` 是**最后一条**「只有 API + 最小状态」的功能对等
长期项 —— 有 `CMP-TEXT` 纯文本 + placeholder + focus，但没有光标、没有定点编辑、
没有语法高亮（阶段 34 的 `ed-lex-*` 只给代码编辑器着色，输入框本身仍是纯文本）。

改动（`src/components.no`）：新池 `INP-CAR`（光标，**字节**下标）/ `INP-SY`
（高亮开关）/ `INP-SGO` / `INP-SGN`（每句柄段基址与段数）随 `cmp-alloc` 逐槽长；
高亮段摊平进**只增**池 `INP-SG` / `INP-SGC`（和 `CP-SRC` 同构，规避「跨 `#`
模块函数内 `vec.push` 会失败」的老坑 —— 词法化只在组件模块内做，渲染层只读）。
新 API：`input-caret` / `-set-caret` / `-move` / `input-insert` / `-backspace` /
`-delete` / `input-syntax` / `-syntax-mode` / `input-lex-build` / `-lex-count` /
`-lex-seg` / `-lex-color`；配色常量 `INP-C-KW` / `INP-C-STR` / `INP-C-COM`。
渲染层（`src/cmp-scene.no`）：`cs-paint-input` 取代 `cs-paint-field`，
`input-syntax-mode(h) != 0` 时走新的 `cs-paint-input-lex`（按段画，光标落进段内
就拆成前后两截，把光标画在中间），否则走原纯文本路径 + 光标。

**不新增节点种类**：`C-INPUT` 仍是叶子，`cs-layout` / `cs-measure` / `cs-hit`
路径一律不动，布局与命中零风险。**向后兼容**：`input-append` 保留旧语义（追加到
末尾并把光标推过去），`input-set` / `input-clear` 同步重置光标，`main-form` /
`dv-app` 行为逐位不变（已实测 exit 0）。

`tests/cmp-input.no`（26 项断言，12 组全过）：构造 / placeholder / 初始光标 /
append 语义 / `set-caret` 上下界夹紧 / 中段 insert / 退格（caret=0 无操作）/
前删 / move 夹紧 / `set` & `clear` 重置光标 / disabled 时写操作全无效 / 词法
（`'a = 1 ; hi'` 段数 == 4、`'a = '`(默认色) / `'1'`(`ED-C-NUM`) / `' '`(默认色) /
`'; hi'`(`INP-C-COM`)、拼接 == 原文、开关切换）/ 渲染（纯文本 3 条 vs 高亮 6 条、
注释色与数字色都真的进了 Scene、光标落在 token 内与末尾两种位置）。

窗口 demo（`tests/main-input.no`）：两个字段同一段样例 `'let x = 42 ; hi'`
（一个开高亮、一个纯文本），点击聚焦 / 失焦，打字 / 退格 / 前删 / ←→ 移光标
（都走 `input-insert` / `input-backspace` / `input-delete` / `input-move`），
`p` 切换高亮、`r` 重置、`q` / ESC 退出；900 帧
`bye frames=900 keys=0 clicks=0 value=let x = 42 ; hi caret=15 lex=1 tokens=5`
+ `input render OK`，`exit 0`。

全量回归（`tests/*.no`，排除 `main-*` / `tmp-*`）：**PASS=46 FAIL=0** —— 阶段37 的
45 项全绿加 `cmp-input`，5 个早期遗留草稿（`kernel` / `t2` / `t3` / `test-kernel` /
`test-l1`）在收尾轮转绿后本阶段不再有红项。`tests/logic` `ALL TESTS PASSED`；
`GOFLAGS=-buildvcs=false make test` 全绿（含 `cmd/no` / `fmt` / `lsp` / `parser` 等）。

坑两条（详见 `ARCHITECTURE.md §5.33`）：① **嵌套分支体里的 `->` 兜底臂必须再包
一层裸 `{ ... }`** —— 否则 `no build` 可能不报错却让**整个模块静默不导出**，症状是
用户文件满屏 `<name> is not defined`（`components` / `cmp-scene` 各报上百条），
只有 `no fmt -d src/<file>.no` 能看到真正的 parse error；② 光标统一用**字节**下标
（和 `slice-bytes` / `cs-text-w` 一致），别混字符下标；`ct-get` 越界返回 0 不是 -1，
`input-set-caret` 自己夹紧。

## 验收记录（阶段45：命令式会话族 bubble / speech）

立项理由：`src/components-catalog.md` 第 53 / 54 行分别写着 `C-BUBBLE` 的能力是
「text, position」、`C-SPEECH` 是「text, lang」，但命令式侧两者都只有一个构造函数
——`position` / `lang` 都没有 API，渲染还落在 `cs-paint-leaf` 的通用
`rect + text` 兜底里。和阶段38–44 的输入族 / 提示族是同一类欠账。

改动（`src/components.no`）：新增一段会话族。`bb-pad`（1200，气泡内边距）/
`bb-tail`（1200，尾巴方块边长）两个常量 + bubble 的 `bubble-pos` / `bubble-set-pos`
（`CMP-VALUE` 存 position，夹紧 `0..1`，`F-DISABLED` 时直接 `return`）+
speech 的 `speech-lang` / `speech-set-lang`（`CMP-TEXT2` 存语言标签，空 = 不画
标签行）。槽位沿用既有约定：bubble 用 `VALUE` = position、`TEXT` = 文本、
`COLOR` = 底色；speech 用 `TEXT` = 文本、`TEXT2` = 语言标签。

渲染层（`src/cmp-scene.no`）：`cs-bubble-tail-x`（尾巴左缘，朝 position 那一边）、
`cs-paint-bubble`（底色方块 + 内边距里的文本，pos 0 左对齐 / pos 1 右对齐，
底边上的 1200 × 1200 尾巴方块）、`cs-paint-speech`（文本行 + 有标签时多一条
`CS-TH-BG` 细分隔线 + 标签行）；`cs-leaf-size` 给 `C-BUBBLE` 加 intrinsic（宽 =
文本宽 + 左右内边距，高 = 行高 + 上下内边距，两者都夹到 `CS-MIN-LEAF-W`），
`cs-measure` 的兜底分支给 `C-SPEECH` 加（宽取「文本宽 / 标签宽」更宽者 +
左右内边距，高在标签非空时多 `CS-SEP-H` + `CS-GAP` + 一行）；`cs-paint-leaf`
加两行路由。文本统一用 `CS-TH-BG`——气泡底色是组件色（默认深蓝 / 灰），
配背景色比配 `FG` 对比度高。**不新增节点种类**：两者都仍是叶子。

`tests/cmp-bubble.no`（28 项断言全绿）：bubble API 8 项（`cmp-kind` 是
`C-BUBBLE`、默认 pos 0、`cmp-text`、`bubble-set-pos` 读写、越界 `-5 -> 0` /
`9 -> 1`、disabled 写无效、`bb-pad` / `bb-tail` = 1200）/ 测算 3 项（`'hello'`
与 `'hi'` 都夹到 6400 × 4800、`'hello world'` = 11200 × 4800）/ 渲染 pos 0 三项
（底色方块、文本在 `+1200`、尾巴在底边 `bh-600`）/ 渲染 pos 1 两项（文本在
`右缘-1200-文本宽`、尾巴在 `右缘-1200-1200`）/ speech API 4 项 / 测算 3 项
（`'hello'` + `'en'` = 6400 × 8100、`'hi'` + `'zh-Hans-CN'` 取更宽的标签 =
10400 × 8100、无标签 = 6400 × 4800）/ 渲染 4 项（底色方块、文本、100 高
`CS-TH-BG` 分隔线、标签在分隔线 + 800；无标签时既无分隔线也无标签文本）。

窗口 demo（`tests/main-bubble.no`）：`cmp-bubble('left bubble')`（pos 0）+
`cmp-bubble('right bubble')`（pos 1）+ `cmp-speech('speech with lang')`（`zh-CN`）+
`cmp-speech('speech no lang')`；点击左气泡翻面、点击带标签的语音泡开关标签行，
键盘 `l` 翻 b1、`r` 翻 b2、`g` 开关 sp 标签、`h` 开关 sp2 标签、`q` / ESC 退出；
900 帧 `bye frames=900 keys=0 clicks=0 b1-pos=0 b2-pos=1 sp-lang=zh-CN sp2-lang=`
+ `bubble render OK`、exit 0。

## 验收记录（阶段44：命令式提示族 message / alert / notification）

立项理由：`src/components-catalog.md` 第 45 / 46 / 47 行分别写着 `C-NOTIFICATION`
的能力是「level, timeout」、`C-ALERT` 是「level, closable」、`C-MESSAGE` 是
「level」，但命令式侧三者都只有一个构造函数——连 `level` 都没有 API，渲染还落在
`cs-paint-leaf` 的通用 `rect + text` 兜底里。和阶段38–43 的 `C-INPUT` /
`C-TEXTAREA` / 输入族 / `C-OTP-INPUT` / `C-COMBOBOX` / 值域型展示控件是同一类欠账。

改动（`src/components.no`）：新增一段提示族。共用的 level 语义：`LV-INFO` /
`LV-OK` / `LV-WARN` / `LV-ERR` 四档 + `lv-slot`（level -> 主题槽下标，表驱动：
`0 -> CS-TH-ACCENT`、`1 -> CS-TH-OK`、`2 -> -1`（没有对应槽，渲染层用琥珀常量）、
`3 -> CS-TH-DANGER`）+ `msg-level` / `msg-set-level`（`F-DISABLED` 时直接
`return`）。alert：`alert-closable` / `alert-set-closable` / `alert-close-w`
（2400）/ `alert-close-x`（矩形右缘往内 1200 起、宽 2400）/ `alert-hit`
（`-1` 没命中 / `0` 主体 / `1` 关闭钮）。notification：`ntf-timeout` /
`ntf-remain` / `ntf-set-timeout`（同时把 remain 重置成 total）/ `ntf-set-remain` /
`ntf-ratio`（`0..10000` 定点百分数，remain 夹进 `[0,total]`，`total <= 0` -> 0）。
槽位沿用既有约定：`VALUE` = level，`VALUE2` = 总时长，`VALUE3` = 剩余 / closable。

渲染层（`src/cmp-scene.no`）：`cs-level-warn`（琥珀常量，同 `dv.no` 的 `DV-C-NUM`
先例——主题里没有琥珀语义槽，这是有意的折中）、`cs-level-color`（消费 `lv-slot`，
未知值按 info 画，不夹紧写入）、`cs-level-glyph`（`i` / `v` / `!` / `x`，ASCII
字形同 select 的 `v` / search 的 `x` 一路，不依赖非 ASCII 字形的字体覆盖）、
`cs-paint-message`（左色条 4px + 字形格 + 文本一行）、`cs-paint-alert`
（message + 右端 `'x'` 关闭钮，`closable` 时才画）、`cs-paint-notification`
（文本一行 + 剩余时间条一行：`CS-TH-TRACK` 轨道 + 按 `ntf-ratio` 缩短的填充）；
`cs-measure` 给 message / notification 加 intrinsic 分支，`cs-paint-leaf` 加三行
路由。**不新增节点种类**：三者都仍是叶子。**向后兼容**：`main-dv` 里若出现这三个
kind 会真的画出来（此前是通用兜底的空色块），但 `main-dv` 输出与阶段38–43 基线
逐字节相同（`ops=125`，已实测）。

`tests/cmp-alert.no`（62 项断言全绿）：level 语义 12 项（构造、默认 info、
`msg-set-level` 读写、未知 level 原样存、`lv-slot` 四档映射、`cs-level-color`
四档 + 未知值退回 ACCENT、`cs-level-glyph` 四个字形、disabled 写无效）/ alert
读写与几何 5 项（构造、`closable` 默认 0 与开关、`alert-close-w`、`alert-close-x`）/
alert 命中 8 项（主体、关闭钮左缘、关闭钮右缘、关闭钮右缘之外仍是主体、矩形右缘
之外 -1、下方、上方、不可关闭时整块都是主体、disabled 写无效）/ notification
时长与比值 13 项（默认 0、`set-timeout` 同时重置 remain、ratio 满格、`set-remain`
后 2500、归零、超上限夹到满格、负值归零、`total <= 0` -> 0、disabled 写无效）/
三者测算 7 项（`'hello'` = `5*800 + 1200 + 2400 + 400` = 8000 × 行高、`'hi'`
夹到 `CS-MIN-LEAF-W` = 6400、alert 可关闭 = 11200 / 8800、notification 多一行
`lv-progress-h` = 3200）/ 三者渲染 17 项（左色条 = `cs-level-color`、字形落在
居中格、文本 `CS-TH-FG`、alert 的 `'x'` 只在 closable 时出现、notification 的
轨道是 `CS-RECT` + 进度条长度 = `ratio`）。

窗口 demo（`tests/main-alert.no`）：`cmp-message`（info）+ `cmp-alert`（danger，
可关闭）+ `cmp-notification`（warning，5s）+ 三条状态标签；点击 alert 右端 `'x'`
把 `closable` 关掉（文案同步改成 closed），键盘 `l` `t` `n` 循环三者 level、`c`
开关 closable、`,` 扣剩余时间、`.` 重置剩余时间、`r` 全部重置、`q` / ESC 退出；
900 帧 `bye frames=900 keys=0 clicks=0 msg-level=0 alert-level=3 closable=1
ntf-level=2 ntf-ratio=10000` + `alert render OK`、exit 0。

全量回归（`tests/*.no`，排除 `main-*`，共 52 个）：**PASS=51 FAIL=1** —— 唯一红项
仍是 `tests/dva-editor.no` 的 `trace/BPT trap`（exit 133），**已排查为工具链 / 环境
漂移**（阶段39 已验证：把改动整个剥掉再编译照样 133）。其余 51 项全绿（含新增
`cmp-alert`）。16 个 `tests/main-*.no` 窗口 demo **全部 `rc=0`**（dummy 驱动），
`main-dv` 输出与阶段38–43 基线逐字节相同（`ops=125`）。

`no vet`：`src/components.no` 0 error / 6 warning / 139 hint（与阶段42/43 基线
逐项相同，新增代码零新增 warning / hint）；`src/cmp-scene.no` 0 error / 0 warning /
162 hint（同基线）。`tests/logic` `ALL TESTS PASSED`。

`GOFLAGS=-buildvcs=false make test`（仓库根）的红项与本阶段无关（既有夹具不一致，
阶段40 已记录），除此之外全绿。

坑三条（详见 `ARCHITECTURE.md §5.39`）：① **`cs-leaf-size` 只收 `(kind, tlen)`，
没有句柄**——在它的分支里写 `cmp-text(h).len()` 不会报错（`h` 不是参数，解析到别处
去了）但取不到文本，宽会算成 6400；提示族的文本宽必须用 `tlen`。② **alert 的
关闭钮宽度必须加在最小宽度夹紧之前**，否则 `'hi'` 这种短文本会先被夹到
`CS-MIN-LEAF-W` 再长回 9600，把关闭钮挤出固有宽；因此 `C-ALERT` 整个不走
`cs-leaf-size`，改在 `cs-measure` 的兜底分支里算（那里才有句柄可读
`alert-closable`）。③ **`cs-emit-rect` 存的是 `CS-RECT` 不是 `CS-FILL`**——
notification 的剩余时间轨道是 `CS-RECT`，拿只匹配 `CS-FILL` 的探针去断言它的颜色
会恒落空，得单独写一个 `CS-RECT` 探针。

## 验收记录（阶段43：命令式值域型展示控件 + slider 下限修复）

立项理由：`src/components-catalog.md` 第 67 / 33 / 10 行分别写着 `C-RATING` 的
能力是「value, max」、`C-PAGINATION` 是「page, total」、`C-EMPTY` 是「title,
description」，但命令式侧三者都只有一个构造函数——rating / pagination 连值都
没有 API，empty 的描述（`CMP-TEXT2`）没人画，三者渲染还落在 `cs-paint-leaf` 的
通用 `rect + text` 兜底里。和阶段38 / 39 / 40 / 41 / 42 的 `C-INPUT` /
`C-TEXTAREA` / 输入族 / `C-OTP-INPUT` / `C-COMBOBOX` 是同一类欠账。审计这批时
还顺带挖出一条**真 bug**：`cmp-slider(min,max)` 只把 `min` 写进 `CMP-VALUE`
（初值），没写 `CMP-VALUE3`（下限真源），而 `slider-value-at` / `slider-set` /
`cs-paint-leaf` 都读 `VALUE3` 当下限——`min != 0` 时左端点返回 0 而不是 `min`。
既有测试全拿 `min == 0` 构造，所以一直没暴露。

改动（`src/components.no`）：新增一段「值域型展示控件」。`slider-min` /
`slider-max` 是对称读取器（渲染层改走它们，不再直读 `CMP-VALUE3` / `CMP-VALUE2`）；
`cmp-slider` 补写 `CMP-VALUE3[h] = min`。`rating-max` / `rating-value` /
`rating-set`（夹紧 `0..max`，`F-DISABLED` 时直接 `return`）/ `rating-cell`
（星格宽 = 行高 2400）/ `rating-hit`（命中第几颗星，0 起，没命中 -1）。
`pagination-total` / `pagination-page` / `pagination-clamp`（页码夹到 `[1,total]`，
`total <= 0` 按 1 页）/ `pagination-set` / `pagination-prev` / `pagination-next` /
`pagination-btn-w`（4000 = `CS-BTN-H`）/ `pagination-label`（「页/总数」）/
`pagination-hit`（`-1` 没命中 / `0` 上一页 / `1` 标签 / `2` 下一页）。empty 的
描述走 `CMP-TEXT2`——`cmp-empty` 只收标题，描述由应用 `cmp-set-text2` 给。
槽位沿用既有约定：`VALUE` = 当前值，`VALUE2` = 上限 / 满分 / 总页数，
`VALUE3` = 下限。

渲染层（`src/cmp-scene.no`）：新增 `cs-paint-rating`（填满的星用组件色、没填的
用 `CS-TH-MUTED`；星形用 ASCII `*`——同 select 的 `v` / search 的 `x` 一路，
不依赖非 ASCII 字形的字体覆盖）、`cs-paint-pagination`（左 `<` / 中间「页/总数」/
右 `>`；到顶 / 到底时对应钮画 `CS-TH-MUTED`——语义提示不是禁用，禁用仍走
`F-DISABLED`）、`cs-paint-empty`（标题一行 + 描述一行，各在半个矩形里居中；
没描述就只画标题）；`cs-measure` 加三条 intrinsic 分支（rating 宽 = `max * 2400`、
高 = 行高；pagination 宽 = `2*4000 + 标签宽`、高 = `CS-BTN-H`；empty 宽取标题 /
描述里更宽的，有描述两行高、无描述单行高）；`cs-paint-leaf` 的 `C-SLIDER` 分支
改读 `slider-min` / `slider-max`，并新增三行路由。**`C-PAGINATION` 同时从
`cs-is-container` 白名单里拿掉**——它的页码按钮是**数据**不是视觉子节点
（同 `C-SELECT` / `C-COMBOBOX` / `C-TAB` 一路），混进白名单会让「给分页挂真子
节点」和「虚拟页码」两套语义打架。

**不新增节点种类**：三者都仍是叶子。**向后兼容**：`main-dv` 里那两个裸节点
`dv-pagination(29, 12, rc)` / `dv-rating(30, 5, rc)` 现在会真的画出来（此前是
通用兜底的空色块），但 `main-dv` 的输出与阶段38–42 基线逐字节相同（`ops=125`，
`ops` 是 `dva-ops()` 的调和操作数，与 Scene 指令数无关，已实测）。

`tests/cmp-rating.no`（54 项断言全绿）：slider 下限回归 8 项（`cmp-slider(20,100)`
的 `slider-min` / `-max`、`slider-value-at` 在 0 / 5000 / 10000 三点的采样 =
20 / 60 / 100、`slider-set(500)` → 100、`slider-set(-5)` → 20，外加 `min == 0`
的旧行为不变）/ rating 读写夹紧 6 项（构造、`rating-max` / `-value`、`set(3)`、
`set(99)` → 5、`set(-1)` → 0、disabled 写无效）/ rating 命中 6 项（第 0 / 1 / 4
颗星、右边界 `x + max*cell` = 13200 之外、下方、上方）/ rating 测算与渲染 7 项
（`cs-measure` = `5*2400` × 行高、5 个 `*` 里前 4 个组件色 `0x50C878FF`、最后
1 个 `CS-TH-MUTED`）/ pagination 读写夹紧 10 项（构造、`pagination-page` / `-total`、
`set(5)` → `5/12`、`set(99)` → 12、`set(0)` → 1、`next` / `prev`、只有 1 页时夹在 1、
disabled 写无效）/ pagination 命中 6 项（0 上一页 / 1 标签 / 2 下一页、上下越界 -1、
控件太窄时一律 -1）/ pagination 测算与渲染 6 项（intrinsic `2*4000 + 标签宽` ×
`CS-BTN-H`、`<` `>` 与标签文本、到顶时 `<` 转 MUTED、翻到最后一页 `>` 转 MUTED
且 `<` 恢复组件色）/ empty 5 项（标题 + `cmp-set-text2` 描述、有描述两行高 4800、
无描述单行高 2400、`cs-measure` 取标题 / 描述里更宽的、两行都进 Scene）。

窗口 demo（`tests/main-rating.no`）：滑条（20..100，初值 40）+ 星条（5 星，3 星）+
分页（12 页，第 1 页）+ 空态（标题 + 描述）+ 三条状态标签；点击按 `rating-hit` /
`pagination-hit` / `slider-value-at` 分别设星数 / 翻页 / 设值，键盘 `a` `d` 星数、
`z` `x` 翻页、`s` `w` 滑条、`e` 空态描述开关、`r` 重置、`q` / ESC 退出；状态栏
实时回显滑条左 / 中 / 右三个采样点（20 / 60 / 100，即那条回归的现场）；900 帧
`bye frames=900 keys=0 clicks=0 slider=40 rating=3/5 page=1/12 label=1/12`
+ `rating render OK`、exit 0。

全量回归（`tests/*.no`，排除 `main-*`，共 51 个）：**PASS=50 FAIL=1** —— 唯一红项
仍是 `tests/dva-editor.no` 的 `trace/BPT trap`（exit 133），**已排查为工具链 / 环境
漂移**（阶段39 已验证：把改动整个剥掉再编译照样 133）。其余 50 项全绿（含新增
`cmp-rating`）。15 个 `tests/main-*.no` 窗口 demo **全部 `rc=0`**（dummy 驱动），
`main-dv` 输出与阶段38–42 基线逐字节相同（`ops=125`）。

`no vet`：`src/components.no` 0 error / 6 warning / 139 hint（与阶段42 基线逐项
相同，新增代码零新增 warning / hint）；`src/cmp-scene.no` 0 error / 0 warning /
162 hint（与阶段42 基线相同）。`tests/logic` `ALL TESTS PASSED`。

`GOFLAGS=-buildvcs=false make test`（仓库根）的红项与本阶段无关（既有夹具不一致，
阶段40 已记录），除此之外全绿。

坑两条（详见 `ARCHITECTURE.md §5.38`）：① **`cmp-slider` 的下限真源是 `CMP-VALUE3`
而不是 `CMP-VALUE`**——构造函数以前只写后者，于是 `min != 0` 时所有按 `VALUE3`
取下限的读取器（`slider-value-at` / `slider-set` / `cs-paint-leaf`）都退回 0；
修法是把 `min` 同时写进 `VALUE3`，并补一对对称读取器 `slider-min` / `slider-max`
让渲染层不再直读槽位。② **`cs-emit-text-center` 存进 Scene 的是居中后的落点**
（`x + (w - tw)/2`、`y + (ht - 行高)/2`）而不是矩形左上角，拿矩形坐标去断言文字
颜色会全落空；`rating-hit` 的右边界也是 `x + max * cell`（星格总宽）而不是控件
矩形宽 `w`，按矩形宽取点会把第 4 颗星误判成「没命中」。

## 验收记录（阶段42：命令式 combobox 可输入下拉）

立项理由：`src/components-catalog.md` 第 22 行写着 `C-COMBOBOX` 的能力是「query,
options, selected」，但命令式侧它只有 `cmp-combobox()` 一个空壳构造——没有加选项、
没有查询过滤、没有选中，渲染还落在单行 `cs-paint-field`。**目录承诺的能力没有
实现**，和阶段38 / 39 / 40 / 41 的 `C-INPUT` / `C-TEXTAREA` / 输入族 / `C-OTP-INPUT`
是同一种缺口，也是输入族里最后一个仍落到单行字段画法的 kind。

改动（`src/components.no`）：查询串沿用 `CMP-TEXT`（光标沿用 `INP-CAR`，编辑直接调
`input-insert` / `input-backspace` / `input-delete`），选项与 `select` 同构存
`CHILD-IDX`（`cmp-child` 可读）。新增四个池，都随 `cmp-alloc` 长：`CB-MBASE`
（匹配表在 `CB-MLIST` 里的基址）/ `CB-MN`（匹配数）/ `CB-MCUR`（高亮匹配下标，
-1 = 无）/ `CB-MLIST`（扁平只增的匹配下标池，同 `INP-SG` 段池思路）。新 API
`combobox-refresh`（空查询 = 全选；否则选项 `to-lower` 后 `contains` 查询
`to-lower`，**大小写不敏感**；顺手把高亮夹紧到 `[0, mn)`，没匹配置 -1）/ `-query` /
`-set-query` / `-add-option`（内部就是 `select-add-option` + `refresh`）/
`-option-count` / `-option` / `-selected`（都转发 `select-*`）/ `-choose`（写查询 +
收起 + 重算匹配表）/ `-match-count` / `-match` / `-match-label` / `-match-index` /
`-match-index-of` / `-highlight` / `-highlight-set` / `-highlight-move` /
`-choose-match` / `-apply` / `-insert` / `-backspace` / `-delete` / `-clear`。所有
写操作先查 `F-DISABLED`。

渲染层（`src/cmp-scene.no`）：新增 `cs-paint-combobox`（字段底 + 查询文本，空则
placeholder 走 `CS-TH-MUTED` + 聚焦竖光标 + 右端 `'v'` 箭头；展开时**只画匹配项**，
高亮行 `CS-TH-ACCENT` 底）；`cs-measure` 加展开分支（宽取**匹配项**里最宽的 +
左右内边距，收起床宽 `CS-MIN-LEAF-W` = 6400，展开 9600）；`cs-layout` 加展开分支
只排匹配项（行高 2400、行距 800，与 `components.no` 的几何字面量同值）；命中新增
`cs-hit-child`，把「第 i 个（虚拟）子节点」在 `C-COMBOBOX` 展开时翻译到匹配表，
`cs-hit-inner` 的两个循环统一改调它（收起时 `walk = 0`，匹配表根本不参与）。
**画的 / 点的 / 量的三者同源**（都读 `CB-MLIST`）。

**不新增节点种类**：仍是叶子，布局 / 命中路径只在「combobox 展开」这一态多走一层
翻译。**向后兼容**：`main-form` / `dv-app` 不使用这个 kind，`main-dv` 输出与阶段38 /
39 / 40 / 41 基线逐字节相同（`ops=125`，已实测）。

`tests/cmp-combobox.no`（41 项断言，11 组全过）：构造（空查询 / 0 选项 / 高亮 -1 /
选中 0）/ 加选项（空查询 = 全选，返回下标）/ 查询过滤（`'ap'`→Apple、`'AN'`→Banana、
`'z'`→0 且高亮 -1、清空→3 且高亮回 0、越界匹配下标 -1，大小写不敏感）/ 匹配反查
（`match-index-of` 命中 1、自身 -1）/ 高亮夹紧与移动（`set(-5)`→-1、`set(99)`→2、
`move` 到顶 / 底都夹紧）/ 选中（`choose` 写查询并收起、匹配表跟着变；
`choose-match` / `apply`）/ 编辑查询（`insert` 追加 + 光标推进 + 重算匹配；
`backspace` / `delete` 复用输入框光标语义；`clear` 清空 + 光标归零）/ disabled 全写
无效 / 测算与布局（收起 6400、展开按最宽匹配项 9600，面板行高 2400、行距 800）/
渲染（`'v'` 箭头、三个选项文本、高亮行 `CS-TH-ACCENT` 底、收起后选项文本消失）/
命中（展开点选项 0 / 1 得选项句柄、点字段得 combobox 自身、收起后 -1）。

窗口 demo（`tests/main-combobox.no`）：水果（Apple / Banana / Cherry / Durian，预置
选中 Banana）+ 颜色（Red / Green / Blue，预置选中 Blue）两个 + 状态标签，默认展开
水果那个；点击展开面板按 `combobox-match-index-of` 反查后 `combobox-choose`、点字段
开关 `F-OPEN`；键盘按焦点分流（字母 `combobox-insert` 过滤并自动展开、退格 / 前删、
↑↓ `combobox-highlight-move`、回车 `combobox-apply`、`c` 清空、`r` 重置）、Esc 先
收起再退出、`q` 退出；900 帧
`bye frames=900 keys=0 clicks=0 cb1=Banana sel1=1 match1=1 open1=true cb2=Blue sel2=2 open2=false`
+ `combobox render OK`、exit 0。

全量回归（`tests/*.no`，排除 `main-*`，共 50 个）：**PASS=49 FAIL=1** —— 唯一红项
仍是 `tests/dva-editor.no` 的 `trace/BPT trap`（exit 133），**已排查为工具链 / 环境
漂移**（阶段39 已验证：把改动整个剥掉再编译照样 133）。其余 49 项全绿（含新增
`cmp-combobox`）。14 个 `tests/main-*.no` 窗口 demo **全部 `rc=0`**（dummy 驱动），
`main-dv` 输出与阶段38 / 39 / 40 / 41 基线逐字节相同（`ops=125`）。

`no vet`：`src/components.no` 0 error / 6 warning / 139 hint（与阶段41 基线逐项
相同，新增代码零新增 warning / hint）；`src/cmp-scene.no` 0 error / 0 warning，hint
行号全部落在 68–192，本阶段新增的 `cs-paint-combobox` / `cs-hit-child` 段零 hint。

`GOFLAGS=-buildvcs=false make test`（仓库根）有 1 个红项，与本阶段无关：
`src/parser/dump` 的 `TestS1RawCondFixturesAreExercised`（既有夹具不一致，阶段40
已记录）。除此之外全绿。

坑三条（详见 `ARCHITECTURE.md §5.37`）：① **`-> { a; b }` 里的分号是注释不是
分隔符**——`{ ct-set(...); return }` 会把 `return` **和收尾的 `}` 一起**注释掉，
于是这个 `{` 再不闭合，**整个模块从该行起静默截断**：后半截所有顶层名字报
`is not defined`（连 `cp-filter` 这种早已存在、且被别的模块正常调用的函数也中招），
而 `no fmt -d` 只报空行差异、不报真错；定位办法是按行扫大括号深度（depth 卡在 1
就是它），修法是把同行多语句拆成两行。② **nolang 没有前向引用**：函数定义必须在
使用之前，所以 `combobox-refresh` 要放在整个 combobox 块最前面，而 `select-*` 系列
在文件更后面——整块 combobox API 必须挪到 `select-*` 之后（阶段42 把原 1736–1881
行整块搬到了 `tab-title` 之后）。③ **插值里不能调函数**：`print('n={f(x)}')` 报
`unresolved format field`，要先取局部再插值。

## 验收记录（阶段41：命令式 OTP 验证码输入）

立项理由：`src/components-catalog.md` 第 19 行写着 `C-OTP-INPUT` 的能力是「digits,
slots, 活动槽 / 退格 / 灌入」，但命令式侧它只走单行 `cs-paint-field`——输入族里
最后一个仍落到「底色 + 单行字」画法的 kind，**目录承诺的能力没有实现**。

改动（`src/components.no`）：新池两个，随 `cmp-alloc` 逐槽长——`OT-CODE []str`
（**定长**数字串，空格 = 空槽）+ `OT-CUR []i64`（活动槽号）；槽数沿用 `CMP-VALUE`
（构造参数 `cmp-otp-input(len)`，`len <= 0` 按 1 槽）。新 API：`otp-slots` /
`otp-cur` / `otp-cur-set`（夹到 `[0, n)`）/ `otp-code`（短了补空格、长了截断）/
`otp-digit` / `otp-set-digit`（非 0..9 视为清空）/ `otp-clear-slot` / `otp-clear` /
`otp-filled` / `otp-digits`（去空格的已输入码）/ `otp-value` / `otp-complete` /
`otp-put`（写一位后跳**下一个空槽**，没有空槽就停在末槽）/ `otp-backspace`
（活动槽有数字清它，否则回退一格清上一格）/ `otp-delete`（只清不移动）/
`otp-left` / `otp-right` / `otp-set`（从字符串灌入，只取数字、多余的忽略、活动槽
回 0）+ 槽格几何与命中 `otp-slot-w`（格宽 = 控件高；控件太窄时按可用宽均分且不低于
1200）/ `otp-slot-x`（累加格宽 + 格距 800）/ `otp-hit`（命中第几槽，格距不算，
上下越界 -1）。所有写操作先查 `F-DISABLED`。

渲染层（`src/cmp-scene.no`）：新增 `cs-paint-otp`（整块字段底 + 每槽一格；数字居中；
聚焦时活动槽四条 `CS-TH-ACCENT` 描边，空槽再画一条竖光标；未聚焦控件不描边），
`cs-measure` 的兜底分支加 OTP 宽度 = `n * ht + (n-1) * 800`（否则 6 位验证码只有
一个 24 宽的「输入框」样子）。**槽格几何渲染与命中同源**：`cs-paint-otp` 与
`otp-hit` 都调 `otp-slot-w` / `otp-slot-x`；`components.no` 看不到 `CS-GAP`，只能
写字面量 `800` 并注明同值。

**不新增节点种类**：仍是叶子，`cs-measure` 只加宽度公式，布局 / 命中路径不动。
**向后兼容**：`main-form` / `dv-app` 不使用这个 kind，`main-dv` 输出与阶段38 /
39 / 40 基线逐字节相同（`ops=125`，已实测）。

`tests/cmp-otp.no`（48 项断言，13 组全过）：构造（槽数入 `CMP-VALUE`、`len <= 0`
按 1 槽、空码）/ 写数字（定长补空格、非 0..9 清空）/ `otp-put` 跳下一个空槽
（连写、满槽停末槽）/ 聚合（`filled` / `complete` / `digits` / `value`）/
`otp-clear` / `otp-backspace`（活动槽有数字清它、否则回退、第 0 槽无数字不动）/
`otp-delete` / 左右夹紧 / `otp-set` 灌入 / disabled 全写无效 / 槽格几何（均分与
下限 1200）/ `otp-hit` / 渲染（宽度公式、每槽一格、数字居中落点、聚焦描边四条、
未聚焦不描边）。

窗口 demo（`tests/main-otp.no`）：6 槽（预置 `'123456'`）+ 4 槽（预置 `'2468'`）+
状态标签；点击按 `otp-hit` 选中槽、键盘按焦点分流（数字 `otp-put` / 退格 / 前删 /
←→↑↓ / `c` 清空 / `r` 重置 / `q` / ESC 退出）、状态栏回显
`6-slot: {v} (cur {c}, filled {f}/{n}) | 4-slot: …`；900 帧
`bye frames=900 keys=0 clicks=0 otp=123456 cur=0 otp2=2468 cur2=0` + `otp render OK`、
exit 0。

全量回归（`tests/*.no`，排除 `main-*`，共 49 个）：**PASS=48 FAIL=1** —— 唯一红项
仍是 `tests/dva-editor.no` 的 `trace/BPT trap`（exit 133），**已排查为工具链 / 环境
漂移**（阶段39 已验证：把改动整个剥掉再编译照样 133）。其余 48 项全绿（含新增
`cmp-otp`）。13 个 `tests/main-*.no` 窗口 demo **全部 `rc=0`**（dummy 驱动），
`main-dv` 输出与阶段38 / 39 / 40 基线逐字节相同（`ops=125`）。

`no vet`：`src/components.no` 0 error / 6 warning / 139 hint（新增代码零新增
warning / hint）；`src/cmp-scene.no` 0 error / 0 warning，hint 行号全部落在 68–192，
本阶段新增的 `cs-paint-otp` 段零 hint。

`GOFLAGS=-buildvcs=false make test`（仓库根）有 1 个红项，与本阶段无关：
`src/parser/dump` 的 `TestS1RawCondFixturesAreExercised`（既有夹具不一致，阶段40
已记录）。除此之外全绿。

坑两条（详见 `ARCHITECTURE.md §5.36`）：① **插值 `{x}` 只在字符串字面量处于
**实参位**时展开**——`s = '{s} '`（赋值 RHS）存的是**字面量** `{s} `，所以拼接
一律 `s.push-str(t)`（`input-insert` 的写法）；`components.no` 开头「追加用插值
`s = '{s}{x}'`」的注释会误导。② **`+` / `-` 产生 `?i64`**（溢出检查），自增
一律 `ct-inc` / `ct-add` / `ct-sub`，`i = i + 1` 会报 `cannot assign ?i64 value
to i64 variable 'i'`。

## 验收记录（阶段40：命令式输入族补全）

立项理由：`src/components-catalog.md` 写着 `C-NUMBER-INPUT` 的能力是「value, min,
max, step」、`C-STEPPER` 是「value, min, max」、`C-SEARCH` 是「query, clear」，但命令式
侧三者都只走单行 `cs-paint-field`——没有值域 / 步长 / 吸附，没有 `[−]` / `[+]` 按钮，
也没有清除钮。**目录承诺的能力没有实现**，和阶段39 的 `C-TEXTAREA` 是同一种缺口。

改动（`src/components.no`）：新池 `CMP-VALUE4`（步长，`<= 0` 一律按 1）随 `cmp-alloc`
逐槽长；槽位沿用 slider 的约定 —— `VALUE` = 当前值、`VALUE3` = 下限、`VALUE2` = 上限。
新 API `num-range` / `num-step-set` / `num-step` / `num-min` / `num-max` /
`num-clamp`（`hi <= 0` = 不夹上限，与 `ct-clamp` 同语义）/ `num-snap`（先夹紧，再
`q = (v - lo + step/2) / step` 四舍五入到 `lo + k*step`，最后再夹一次——`lo` 非 0 时
吸附可能越过上限）/ `num-value` / `num-set`（吸附 + 写 `VALUE` + 同步 `CMP-TEXT`）/
`num-bump`（`dir * step`，disabled 直接 return）/ `num-commit`（把 `CMP-TEXT` 解析
回值，失败不动值；`to-i64` **不裁空白**，`' 77 '` 这类带空格的文本按非法处理）；
`search-query` / `search-clear` / `search-hit`（右端 `ht` 宽的正方形清除区）；
`stepper-hit`（`-1` 左 / `0` 中 / `1` 右，按钮宽 `bw = ht`，`w < 2*bw` 时对半分）。

渲染层（`src/cmp-scene.no`）：新增 `cs-paint-number-input`（右对齐 + 光标贴右端，
x = 右边界 − 右内边距 − **光标后缀**宽，比 `cs-paint-field` 的「整串宽」更准）/
`cs-paint-stepper`（`[−]` 值 `[+]`，杠是纯轴对齐矩形：横杠 1200 × 200、加号竖杠
200 × 1200）/ `cs-paint-search`（文本 + 光标 + 右端清除钮），三者从 `cs-paint-field`
改走各自分支。清除钮用 **ASCII `'x'`** 字形（`cs-emit-text-center`）——和
`cs-paint-select` 拿 `'v'` 当箭头是同一路子：`shim.c` 写死
`/System/Library/Fonts/Supplemental/Arial.ttf`，非 ASCII 字形（`×` / `▼`）能不能画
取决于字体覆盖，而 `src/*.no` 里此前没有任何非 ASCII 字形依赖；纯轴对齐矩形又拼不
出对角线。且**只在有文本时画**清除钮（空搜索框不画）。

**不新增节点种类**：三者仍是叶子，`cs-measure` 早就给它们 `CS-BTN-H` 的高，布局 /
命中路径不动。**向后兼容**：`main-form` / `dv-app` 不使用这三个 kind 的数值语义，
`main-dv` 输出与阶段38 / 39 基线逐字节相同（`ops=125`，已实测）。

`tests/cmp-number.no`（42 项断言，10 组全过）：构造（三个 kind / 初始值 0 / 步长默认 1）/
值域与步长读写（`step-set` 非正数按 1）/ 吸附（23→20、25→30、27→30、-5→0、200→100）/
`hi <= 0` 不夹上限 / `num-set` 同步 `CMP-TEXT` / `num-bump`（`dir*step`、disabled 不动）/
`num-commit`（合法写回、`' 77 '` 因不裁空白而失败、`'abc'` 与空串失败且不动值、
`'500'` 夹到 100）/ `search-hit`（命中 1、中间 0、越上边界 0）/ `stepper-hit`
（-1 / 1 / 0 / 越界 0、窄控件按钮对半分）/ 渲染（数值与步进器值都进 Scene、搜索有
文本时画清除钮、清空后不画）/ 几何（数值右对齐落点 = 右边界 − 文本宽、步进器值居中、
减号横杠与加号竖杠落在左右钮上）。

窗口 demo（`tests/main-number.no`）：数字输入（20000 宽，0–100 / 步长 5）+ 步进器
（24000 宽，0–100 / 步长 10）+ 搜索框（32000 宽）+ 状态标签；点击聚焦 / 失焦、点
步进器左 / 右钮 `stepper-hit` 命中后 `num-bump`、点搜索右端 `search-hit` 命中后
`search-clear`；键盘按焦点分流（数字输入：数字键入 + 回车 `num-commit` + ↑↓
`num-bump`；步进器：↑↓←→ `num-bump` + 回车 `num-commit`；搜索：文本 + 退格 + 前删 +
←→）、`r` 重置、`q` / ESC 退出；900 帧
`bye frames=900 keys=0 clicks=0 num=40 stepper=50 search=` + `number render OK`、exit 0。

全量回归（`tests/*.no`，排除 `main-*`，共 48 个）：**PASS=47 FAIL=1** —— 唯一红项仍是
`tests/dva-editor.no` 的 `trace/BPT trap`（exit 133），**已排查为工具链 / 环境漂移**
（阶段39 已验证：把改动整个剥掉再编译照样 133）。其余 47 项全绿（含新增
`cmp-number`）。`tests/logic` `ALL TESTS PASSED`（`cd noui && ../bin/no test
tests/logic`）。12 个 `tests/main-*.no` 窗口 demo **全部 `rc=0`**（dummy 驱动），
`main-dv` 输出与阶段38 / 39 基线逐字节相同（`ops=125`）。

`no vet`：`src/components.no` **0 error / 6 warning / 139 hint**，与阶段39 基线逐项
相同（新增代码 0 warning、0 hint）；`src/cmp-scene.no` 0 error / 0 warning，hint 行号
全部落在 68–192，本阶段新增的 1030–1121 行**零 hint**。

`GOFLAGS=-buildvcs=false make test`（仓库根）**有 1 个红项，与本阶段无关**：
`src/parser/dump` 的 `TestS1RawCondFixturesAreExercised` 报
`match-2.no is no longer classified as a raw-cond match (P4) ... forms=[P1 P2 P6]`。
原因是那份未提交的工作把 `tests/match-2.no` 的 `ok(cond)` 臂改写成了 `ok -> { ... }`
块（不再算 raw-cond 臂），而钉住 P4 覆盖的测试没跟着更新——**属于 nolang 核心 /
测试夹具的既有不一致，不在 noui 阶段40 的范围内**，本阶段不动它（也不回退别人的未提交
改动）。除此之外 `make test` 全绿（`cmd/no` / `fmt` / `lsp` / `parser` / `checker` …）。

坑两条（详见 `ARCHITECTURE.md §5.35`）：① **`to-i64` 的 `ok` 臂标签本身是个 bool
变量**，函数返回值不能也叫 `ok`（报 `cannot assign i64 value to bool variable 'ok'`），
且 bool 只能赋 `true` / `false`，写 `done = 1` 同样报错；② `cs-paint-*` 的落点断言
要按「右内边距 / 居中」反算——只断言「Scene 里有没有这段文本」测不出对齐，`t39` /
`t40` 是用 `cs-text-at` 直接钉 `x` 才钉住的。

## 验收记录（阶段39：命令式多行文本域）

立项理由：`src/components-catalog.md` 写着 `C-TEXTAREA` 的能力是「multiline value」，
但命令式侧它只有 `cmp-textarea(placeholder)` 一个空壳构造——`input-set` 塞多行文本
进去，渲染仍走单行的 `cs-paint-field`（整串画在一行、光标按整串宽度定位），既没有
行概念也没有视口。**目录承诺的能力没有实现**，这是最强的一种缺口（不是「没做」，
是「文档说了但代码没有」）。

改动（`src/components.no`）：新池 `TA-OFF`（每句柄视口首行行号）随 `cmp-alloc` 逐槽长；
新 API `ta-line-count` / `ta-line-start` / `ta-line-len` / `ta-line-text` /
`ta-caret-line` / `ta-caret-col` / `ta-move-up` / `ta-move-down` / `ta-off` /
`ta-set-off` / `ta-follow`。行 = `'\n'` 分隔、换行符归上一行行尾（空文本算 1 行，
行号越界夹到最后一行）；`ta-move-up` / `-down` 保持列并夹到目标行长度，首 / 末行不动；
`ta-follow(h, cap)` 让光标行落进 `[off, off+cap)`，且 `off` 不越过「最后 cap 行」
（`cap <= 0` 按 1）。**编辑不另写**：文本与光标仍在 `CMP-TEXT` + `INP-CAR`（阶段 38
的字节下标），换行 / 退格 / 前删直接复用 `input-insert` / `input-backspace` /
`input-delete`——行首退格合并上一行、行尾前删合并下一行、行中插 `\n` 多一行，都是
这三条原语的推论。

渲染层（`src/cmp-scene.no`）：新增 `cs-paint-textarea`，`C-TEXTAREA` 从
`cs-paint-field` 改走它。字段底 + 焦点描边后，`cap = ht / CS-LINE-H`（至少 1），
`ta-follow` 夹好视口，然后 `l` 从 `off` 画到 `min(lines, off + cap)`：每行一条
`cs-emit-text-left`（行文本由 `ta-line-text` 给，不含换行符），光标行再画一条行内
`fill`（x = 行首 + `cs-text-w(行内前缀)`，夹到右内边距）。

**不新增节点种类**：`C-TEXTAREA` 仍是叶子，`cs-measure` 早就给它 `2 * CS-BTN-H`
的高（视口 3 行），`cs-layout` / `cs-hit` / 命中路径一律不动。**向后兼容**：
`main-form` / `dv-app` 不用 `C-TEXTAREA`，行为逐位不变（已实测 exit 0）。

`tests/cmp-textarea.no`（34 项断言，5 组全过）：构造 / placeholder / 空文本算 1 行 /
行几何（2 行、起始 0 与 3、长度 2+2、`line-text`、行号越界夹到最后一行）/ 光标行列
（0、2、3、4 四个位置）/ 上下移保持列 + 列夹紧（`abcdef` ↑ `xy` 夹到 2）+ 首尾行不动 /
行中插 `\n` 多一行且光标落新行行首 / 行首退格合并上一行 / 行尾前删合并下一行 /
disabled 写无效 / 视口（`set-off` 夹 0、`follow` 上滚 / 不动 / 下滚、`cap<=0` 按 1）/
渲染（视口内 3 行、视口外两行**不进 Scene**、光标滚出后视口跟随到 `off=2`、
placeholder 走 MUTED）。

窗口 demo（`tests/main-textarea.no`）：一个大字段（48000 × 16000，视口 6 行）+ 状态
标签，点击聚焦 / 失焦，打字 / 回车换行 / 退格 / 前删 / ↑↓←→（上下走 `ta-move-up` /
`-down`），`r` 重置、`q` / ESC 退出；900 帧
`bye frames=900 keys=0 clicks=0 value=let x = 1¶let y = 2¶; done caret=26 line=2 col=6 off=0 lines=3`
+ `textarea render OK`，`exit 0`。

全量回归（`tests/*.no`，排除 `main-*`）：**PASS=46 FAIL=1** —— 唯一红项是
`tests/dva-editor.no` 的 `trace/BPT trap`（exit 133），**已排查为工具链 / 环境漂移**：
把阶段39 的全部改动从 `src/components.no` / `src/cmp-scene.no` 里剥掉再编译，
`dva-editor` 照样 133（README 阶段37 验收记录已记同源现象，陷阱在 `cmp_alloc` 末尾，
与本阶段无关）。其余 46 项全绿（含新增 `cmp-textarea`）。`tests/logic`
`ALL TESTS PASSED`。

`GOFLAGS=-buildvcs=false make test`（仓库根）**有 1 个红项，与本阶段无关**：
`src/parser/dump` 的 `TestS1RawCondFixturesAreExercised` 报
`match-2.no is no longer classified as a raw-cond match (P4) ... forms=[P1 P2 P6]`。
证据三条：`src/parser/` 相对 HEAD **零改动**（`git status` 空）；`tests/match-2.no`
的 mtime（17:22）早于本阶段在 `src/components.no` / `src/cmp-scene.no` 的改动
（17:41 / 17:47）；本阶段 diff 只碰 `noui/` 下的文件。原因是那份未提交的工作把
`tests/match-2.no` 的 `ok(cond)` 臂改写成了 `ok -> { ... }` 块（不再算 raw-cond 臂），
而钉住 P4 覆盖的测试没跟着更新——**属于 nolang 核心 / 测试夹具的既有不一致，不在
noui 阶段39 的范围内**，本阶段不动它（也不回退别人的未提交改动）。

窗口 demo 全量：11 个 `tests/main-*.no` 逐个构建 + 运行（dummy 视频 / 音频驱动）
**全部 `rc=0`**——`main-app` / `main-layered` / `main-form` / `main-tree` 退出 `bye`，
`main-editor` / `main-table` / `main-dock` / `main-command` / `main-input` /
`main-textarea` 各自 `... render OK`，`main-dv` 仍是
`bye frames=900 ops=125 clicks=0 rows=2 sel=0 pick=0 tst=0 scrl=0 sort=0 vsub=0 lsc=0
dock=22000 edoff=0 tsel=-1 toff=0 pal=0`（与阶段38 基线逐字节相同）。

坑一条（详见 `ARCHITECTURE.md §5.34`）：`cs-paint-textarea` 的视口循环上界必须同时受
`lines` 与 `off + cap` 约束。只判 `lines` 时，滚出视口的行照样被发进 Scene——
`CS-CLIP` 只影响**绘制**、不影响 Scene 内容，肉眼在截图上看不出差别，只有去断言
Scene 里有没有那一行才会发现（本阶段的 `t31` 就是这么抓到的）。

## 验收记录（收尾：5 个早期遗留草稿全部转绿）

把上一轮记为「与本阶段无关」的 5 个红测试（`kernel` / `t2` / `t3` / `test-kernel` /
`test-l1`）逐个修掉，全量回归 **PASS=45 FAIL=0**（`tests/main-*.no` 仍是交互 demo，
不计入这 45），`tests/logic/l1-l3.no` 11 项断言全绿（`ALL TESTS PASSED`）。

修的过程中踩到几个**新坑**，都已写进上面的「实测约束」：

1. **`src/entity.no` 的 9 个小写全局**（`g-kind` / `g-i0` / `g-i1` / `g-s0` /
   `g-dirty` / `g-sub-*`）被四档命名诊断拒绝 —— 改名即可，函数名仍可小写。
2. **option 隐式解包**：`entity-i0` / `entity-i1` / `entity-s0` 里 `v = G-i0[h]` 直接
   把 `?i64` 赋给 `i64`，编译期报 `cannot assign ?i64 value to i64 variable 'v'`。
   改成库里的标准两段式（`v = G-i0[h]: { ok -> it \n -> 0 }`）。
3. **`(cond) { }` 是 while、`(cond) -> { }` 才是 guard**，这次又栽两次：
   - `entity-emit` 里 `(G-sub-active[i] == 1 && ...) {` 是**死循环** —— 条件一直成立，
     函数永不返回（`tests/test-l1.no` 跑到 `entity-emit` 就挂，`timeout` 才拿得住）。
   - `tests/test-kernel.no` 的 `el-add` 里 `(E-children-count[parent] == 0) {` 同样是
     while（`scene-dump` 里两处也是，只是那段是死代码没触发）。三处都改成 `->`，与
     `tests/kernel.no` 的正确版本对齐；`test-kernel.no` 随即输出 `elements: 2`。

另外 `tests/test-l1.no` 还暴露一个**编译器代码生成缺陷**（冻结工具链上同样复现，
非本轮引入，**2026-11-22 已修**）：`r rect; r.x = 10` 这种「i64 字面量写进 f64
结构体字段」会生成非法 IR —— `store double %lv.N, double* %gp.M`（`%lv.N` 是 i64
局部），clang 直接报 `defined with type 'i64' but expected 'double'`。原先绕法是
**写字面量就带小数点**（`r.x = 10.0`，函数实参同理 `rect-hit(r, 50.0, 30.0)`）；
现在 `emitSetField` 补上了 int→float 的 `sitofp`，新代码不必带 `.0`，回归见
`src/mir/f64_field_store_test.go`。

`tests/main-app.no` / `tests/main-layered.no` 在无头（dummy 视频驱动）下**不会自己
退出**，这不是缺陷：它们是**没有帧数上限的交互 demo**，而 dummy 驱动不会投递
`SDL_EVENT_QUIT`，所以主循环一直空转（`main-command` / `main-dock` / `main-tree`
都写了 `*-MAX-FRAMES` 上限，故能正常 `bye`）。用 `timeout` 观察时，到点发的
`SIGTERM` 会被 SDL 的信号处理器转成 quit 事件 —— 于是先打印 `bye` 再停在退出路径上，
看起来像「打印 bye 之后卡死」，实际是信号已被消费。

**顺带修掉一个跨模块同名全局的真遮蔽**：`src/dv.no` 的树箭头常量
`TR-OPEN = 'v'` 和 `src/components.no` 阶段33 的模型池 `TR-OPEN []i64` 同名。同时
`#` 导入两者的程序里，`TR-OPEN` 指向谁**随导入顺序变**（探针实测：`# dv` 后
`# components` → `TR-OPEN=[]`；反过来 → `TR-OPEN=[118]`，即 `'v'`）。
`tests/dva-tree.no` 的 `dv-text(ar0) == TR-OPEN` 只是碰巧靠传递导入顺序拿到了 dv
那份。已把 dv 侧改成 `TR-ARROW-OPEN` / `TR-ARROW-SHUT`（与 `TR-ARROW-W` 同族），
`components.no` 的池名不动。`src/*.no` 单文件 `no vet` 现在除 `dv-app.no` 的
7 个「装配层固有未定义」和 `pm-*.no` / `pushmod*.no` 这些内核草稿外全绿。
