# noui 实现叙事存档（原 README「已交付」正文）

> README 在工程化收尾时把阶段 9–32 的逐阶段实现叙事整体移入本文件；
> 现状、目录索引与验收记录见 [README.md](README.md)，架构决策见
> [ARCHITECTURE.md](ARCHITECTURE.md)。按阶段跳转用 README 的「目录索引」。

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

