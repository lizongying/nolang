# noui 组件目录 —— gpui-kit 86 模块 → Nolang API 映射

> 来源：gpui-kit（原 gpui-component）`crates/component/src/lib.rs` 的 `pub mod` 清单，2026-11-20 抓取。
> 本文件是 noui 组件实现的权威清单；每个模块映射到 noui 的一个组件 kind 常量 + 构造函数 + 一组属性设置器。
> 实现文件：`src/components.no`（全目录统一 API 层，单文件规避跨模块 vec.push 限制）。

## 设计约束（已实测，勿违反）

- 全部组件实例存于**大写全局 SoA 池**；小写顶层数据被函数引用 → 四档命名硬错误。
- 不支持 vec-of-struct → 每个字段一个平行数组。
- 计数自增用 `n + 1: { ok -> it  -> n }` 解包，循环内 `#{overflow}` 不稳。
- guard 用 `(cond) -> { }`；`(cond) { }` 是 while，误用即死循环。
- 跨 `#` 模块的函数内 vec.push 会失败 → 组件库收敛在单文件。
- 复杂组件早期只做 API 骨架 + 最小可用状态；**核心交互组件的命令式侧已补全**：`C-TABLE`（阶段35）、
  `C-TREE`（阶段33）、`C-SCROLL`（阶段34 `cmp-editor-*`）、`C-DOCK`（阶段36）、
  `C-COMMAND`（阶段37）、`C-VIRTUAL-LIST`（`cmp-virtual-list = (len i64) (h i64)`）、
  `C-INPUT`（阶段38：光标 + 定点编辑 + 语法高亮）、`C-NUMBER-INPUT` / `C-STEPPER` / `C-SEARCH`（阶段40）、
  `C-OTP-INPUT`（阶段41）、`C-COMBOBOX`（阶段42）、`C-RATING` / `C-PAGINATION` / `C-EMPTY`（阶段43）。
  **仍未补的**：5 个 kind 只有构造函数、
  没有任何专属 API（shimmer /
  sizing / styled / text-node / window-border，均为容器 / 面板类，
  走通用容器绘制）。文末统计的是「仍在通用 `rect + text` 兜底里画的**叶子** kind」，
  那一项已清零。

## 模块 → 组件映射（86 项）

### A. 基础与通用（12）
| # | gpui-kit 模块 | noui 组件 kind | 构造函数 | 核心属性/能力 |
|---|---|---|---|---|
| 1 | `label` | `C-LABEL` | `cmp-label(text)` | text, color, size |
| 2 | `button` | `C-BUTTON` | `cmp-button(text)` | variant(primary/danger/ghost), disabled, click |
| 3 | `badge` | `C-BADGE` | `cmp-badge(text)` | color, dot |
| 4 | `tag` | `C-TAG` | `cmp-tag(text)` | closable, color |
| 5 | `icon` | `C-ICON` | `cmp-icon(name)` | name, size, color |
| 6 | `kbd` | `C-KBD` | `cmp-kbd(key)` | key text |
| 7 | `link` | `C-LINK` | `cmp-link(text)` | href, underline |
| 8 | `separator` | `C-SEPARATOR` | `cmp-separator()` | orientation |
| 9 | `avatar` | `C-AVATAR` | `cmp-avatar(name)` | initials, color |
| 10 | `empty` | `C-EMPTY` | `cmp-empty(text)` | title, description（阶段43：描述走 `CMP-TEXT2`，`cs-paint-empty` 两行都画） |
| 11 | `spinner` | `C-SPINNER` | `cmp-spinner()` | speed |
| 12 | `marker` | `C-MARKER` | `cmp-marker(text)` | color |

### B. 表单 / 输入（16）
| # | 模块 | kind | 构造函数 | 能力 |
|---|---|---|---|---|
| 13 | `checkbox` | `C-CHECKBOX` | `cmp-checkbox(text)` | checked, toggle |
| 14 | `radio` | `C-RADIO` | `cmp-radio(text)` | selected, group |
| 15 | `switch` | `C-SWITCH` | `cmp-switch()` | on, toggle |
| 16 | `input` | `C-INPUT` | `cmp-input(placeholder)` | value, focus, caret, text buffer, 语法高亮（阶段38） |
| 17 | `input/textarea` | `C-TEXTAREA` | `cmp-textarea(placeholder)` | multiline value, 行几何 / 上下移光标, 视口（阶段39） |
| 18 | `input/number_input` | `C-NUMBER-INPUT` | `cmp-number-input()` | value, min, max, step, 吸附 / 提交（阶段40） |
| 19 | `input/otp_input` | `C-OTP-INPUT` | `cmp-otp-input(len)` | digits, slots, 活动槽 / 退格 / 灌入（阶段41） |
| 20 | `input/search` | `C-SEARCH` | `cmp-search()` | query, clear, 右端清除区（阶段40） |
| 21 | `select` | `C-SELECT` | `cmp-select(placeholder)` | options, selected, open |
| 22 | `combobox` | `C-COMBOBOX` | `cmp-combobox()` | query, options, selected, 匹配表 / 高亮 / 展开面板（阶段42） |
| 23 | `slider` | `C-SLIDER` | `cmp-slider(min,max)` | value, step（阶段43：构造函数补写 `CMP-VALUE3` 下限 + `slider-min` / `slider-max` 读取器） |
| 24 | `stepper` | `C-STEPPER` | `cmp-stepper()` | value, min, max, step, [−]/[+] 命中（阶段40） |
| 25 | `color_picker` | `C-COLOR-PICKER` | `cmp-color-picker()` | rgba, hue（阶段47：`CMP-COLOR` = `0xRRGGBBAA`、`CMP-VALUE` = hue `0..360`，`cp-color` / `cp-red` / `cp-green` / `cp-blue` / `cp-alpha` + 四个 `cp-set-*`（写通道重算 hue）+ `cp-hue` / `cp-set-hue`（HSV 正算，S=191/255、V=235/255）+ `cp-hue-of`（RGB 反算）+ `cp-hex`（`#RRGGBB`）+ `cs-paint-color-picker` 色板 + hex + hue 数字，**不新增节点种类**） |
| 26 | `time` (calendar/date_picker/time_field) | `C-DATE-PICKER` | `cmp-date-picker()` | year, month, day（阶段47：`CMP-VALUE` = 年、`VALUE2` = 月（1..12）、`VALUE3` = 日，构造默认 2026-01-01，`dp-year` / `dp-month` / `dp-day` + 四个 `dp-set-*` + `dp-set-date`（写月 / 年重夹日）+ `dp-leap` / `dp-days` / `dp-next-month` + `dp-label`（`YYYY-MM-DD`）+ `cs-paint-date-picker` 字段底 + 日期文案，**不新增节点种类**） |
| 27 | `form` | `C-FORM` | `cmp-form()` | fields container |
| 28 | `questionnaire` | `C-QUESTIONNAIRE` | `cmp-questionnaire()` | questions（阶段65：题池 `QA-PROMPT []str` / `QA-ANS []str` / `QA-REQ []i64` 三个池严格同步增长（扁平只增）+ 每句柄 `QA-SBASE` / `QA-N`（**必须在 `cmp-alloc` 里 `.push(0)`**），`qa-pad` / `qa-gap` / `qa-min-w` 三个几何常量 + `qa-count` / `qa-prompt` / `qa-answer` / `qa-required` / `qa-answered` / `qa-progress`（百分比，无题 0）/ `qa-complete`（必答题是否都作答）+ `qa-add` / `qa-set-prompt` / `qa-set-answer` / `qa-set-required` / `qa-clear`（写操作先查 `F-DISABLED`）+ 从 `cs-is-container` 白名单里拿掉 + `cs-measure` 分支（宽 = `2*CS-PAD + max(题干,答案)宽`，下限 6400；高 = `2*CS-PAD + N*2*CS-LINE-H + (N-1)*qa-gap`，下限 `CS-BTN-H`）+ `cs-paint-questionnaire`（FIELD 底 + 每题「必答 TRACK 小方块标记 + 题干 FG」一行、「答案 ACCENT」一行，题间留 `qa-gap`），**不新增节点种类**） |

### C. 导航（12）
| # | 模块 | kind | 构造函数 | 能力 |
|---|---|---|---|---|
| 29 | `tab` | `C-TAB` | `cmp-tab()` | tabs, active |
| 30 | `breadcrumb` | `C-BREADCRUMB` | `cmp-breadcrumb()` | items, separator（阶段53：`BC-ITEM` 扁平池 + 每句柄 `BC-SBASE` / `BC-N` / `BC-SEP`，`bc-count` / `bc-item` / `bc-add-item` / `bc-clear` / `bc-sep` / `bc-set-sep` / `bc-last` / `bc-join` + `cs-measure` 分支 + `cs-paint-breadcrumb`：祖先 MUTED、当前页 FG、分隔符 TRACK，**不新增节点种类**） |
| 31 | `sidebar` | `C-SIDEBAR` | `cmp-sidebar()` | groups, collapsed（阶段69：组池 `SDB-GROUP []str` + 每句柄 `SDB-GSBASE` / `SDB-GNC`，条目池 `SDB-ITEM []str` 配平行「所属组」`SDB-ITOG []i64` + 每句柄 `SDB-ISBASE` / `SDB-IN`，折叠位 `SDB-COLL`（**五个 per-handle 池都必须在 `cmp-alloc` 里 `.push(0)`**），`sdb-pad=1200` / `sdb-gap=800` / `sdb-indent=1600` / `sdb-line-h=2400` 四个几何常量 + `sdb-group-count` / `sdb-group` / `sdb-add-group`（返回插入下标）/ `sdb-set-group` / `sdb-clear-groups` / `sdb-item-count` / `sdb-item` / `sdb-item-group`（越界 −1）/ `sdb-add-item`（组号越界返回 −1 且不追加）/ `sdb-set-item` / `sdb-clear-items` / `sdb-clear` / `sdb-collapsed` / `sdb-set-collapsed` / `sdb-group-size`（写操作先查 `F-DISABLED`）+ 从 `cs-is-container` 白名单里拿掉 + `cs-measure` 分支（宽 = `2*sdb-pad + max(组头宽, 未折叠时条目宽+sdb-indent)`，下限 6400；高 = `2*sdb-pad + Σ_g(sdb-line-h + 展开时该组条目数*sdb-line-h) + (N-1)*sdb-gap`，下限 `CS-BTN-H`）+ `cs-paint-sidebar`（FIELD 底 + 组头 MUTED + 展开时属于该组的条目 FG 并缩进 `sdb-indent`，组间留 `sdb-gap`），**不新增节点种类**） |
| 32 | `menu` | `C-MENU` | `cmp-menu()` | items, open |
| 33 | `pagination` | `C-PAGINATION` | `cmp-pagination(total)` | page, total（阶段43：`pagination-total` / `-page` / `-clamp` / `-set` / `-prev` / `-next` / `-btn-w` / `-label` / `-hit` + `cs-paint-pagination`；页码按钮是数据不是视觉子节点，已从 `cs-is-container` 白名单拿掉） |
| 34 | `toolbar` | `C-TOOLBAR` | `cmp-toolbar()` | items（阶段68：条目池 `TLB-ITEM`（`[]str`，扁平只增）+ 每句柄 `TLB-SBASE` / `TLB-N` / `TLB-CUR`（随 `cmp-alloc` 长），`tlb-pad=1200` / `tlb-gap=800` / `tlb-chip-pad=800` / `tlb-bar-h=4000` 四个几何常量 + `tlb-count` / `tlb-item` / `tlb-add`（返回插入下标，且**首次有条目时自动高亮第 0 项**）/ `tlb-set-item` / `tlb-clear`（同时把选中复位 −1）/ `tlb-selected` / `tlb-select`（越界夹紧到最近一端）/ `tlb-select-next` / `tlb-select-prev`（两端回绕）（写操作先查 `F-DISABLED`）+ 从 `cs-is-container` 白名单里拿掉 + `cs-measure` 分支（宽 = `2*tlb-pad + Σ(文案宽+2*tlb-chip-pad) + (N-1)*tlb-gap`，下限 6400；高 = `2*tlb-pad + tlb-bar-h`，下限 `CS-BTN-H`）+ `cs-paint-toolbar`（FIELD 底 + 一条横排 chip：未选中 TRACK 盒、当前项 ACCENT 盒，文案一律 FG 垂直居中），**不新增节点种类**） |
| 35 | `title_bar` | `C-TITLE-BAR` | `cmp-title-bar(title)` | title, buttons（阶段67：标题走 `CMP-TEXT`（`tb-title` / `tb-set-title`），按钮池 `TTB-BTN`（`[]str`，扁平只增）+ 每句柄 `TTB-SBASE` / `TTB-N`（随 `cmp-alloc` 长），`tb-pad` / `tb-gap` / `tb-btn-pad` / `tb-head-h` 四个几何常量 + `tb-count` / `tb-button` / `tb-add`（返回插入下标）/ `tb-set-button` / `tb-clear`（写操作先查 `F-DISABLED`）+ 从 `cs-is-container` 白名单里拿掉 + `cs-measure` 分支（宽 = `2*tb-pad + 标题宽 + (有按钮 ? tb-gap + Σ(按钮文案宽+2*tb-btn-pad) + (N-1)*tb-gap : 0)`，下限 6400；高 = `2*tb-pad + tb-head-h`，下限 `CS-BTN-H`）+ `cs-paint-title-bar`（FIELD 底 + 左标题 FG + 右端一排 TRACK 按钮盒 + FG 文案，文字垂直居中），**不新增节点种类**） |
| 36 | `status_bar` | `C-STATUS-BAR` | `cmp-status-bar()` | segments（阶段63：分段池 `SB-SEG`（`[]str`，扁平只增）+ 每句柄 `SB-SBASE` / `SB-N`（随 `cmp-alloc` 长），`sb-pad` / `sb-div-w` 两个几何常量 + `sb-count` / `sb-seg` / `sb-add` / `sb-set-seg` / `sb-clear`（写操作先查 `F-DISABLED`）+ 从 `cs-is-container` 白名单里拿掉 + `cs-measure` 分支（宽 = `2*CS-PAD + Σ 段文案宽 + (N-1)*CS-SEP-H`，下限 6400；高 = `CS-BTN-H`）+ `cs-paint-status-bar`（FIELD 底 + 段 FG 文案 + 段间 TRACK 竖分隔条，文字垂直居中），**不新增节点种类**） |
| 37 | `command` | `C-COMMAND` | `cmp-command()` | palette, query（阶段37 补 `cmp-command-*`：输入框 + 滚动体 + 两段式重建可见行，游标真源 `CP-CUR`，命中表抄本 `CP-SRC`/`CP-SB`） |
| 38 | `history` | `C-HISTORY` | `cmp-history()` | entries, index（阶段60：`HST-ENTRY` 扁平只增条目池（`[]str`）+ 每句柄 `HST-SBASE` / `HST-N`（随 `cmp-alloc` 长），当前下标走 `CMP-VALUE`，`hst-count` / `hst-entry` / `hst-add`（返回新下标，disabled 时 -1）/ `hst-set-entry` / `hst-index`（越界回落 0）/ `hst-set-index` / `hst-jump` / `hst-back` / `hst-forward`（夹紧在 `[0, n-1]`，不环绕）/ `hst-clear`（写操作先查 `F-DISABLED`）+ 从 `cs-is-container` 白名单里拿掉 + `cs-measure` 分支（宽 = `2*CS-PAD + 标记条 800 + 间隙 800 + 最长条目文案`，下限 6400；高 = `2*CS-PAD + N*CS-LINE-H + (N-1)*CS-SEP-H`，下限 `CS-BTN-H`）+ `cs-paint-history`（FIELD 底 + 当前行 ACCENT 标记条 + FG 文本，其余行 TRACK 标记条 + MUTED 文本，行间 TRACK 分隔线），**不新增节点种类**） |
| 39 | `link`（导航向）已计 A7 | — | — | — |
| 40 | `sidebar/group` 归入 C31 | — | — | — |

### D. 覆盖层 / 反馈（18）
| # | 模块 | kind | 构造函数 | 能力 |
|---|---|---|---|---|
| 41 | `dialog` | `C-DIALOG` | `cmp-dialog(title)` | open, title, actions |
| 42 | `popover` | `C-POPOVER` | `cmp-popover()` | open, side, title, body（阶段64：正文行池 `PV-LINE []str`（扁平只增）+ 每句柄 `PV-SBASE` / `PV-N`（随 `cmp-alloc` 长），开合走 `CMP-VALUE`、锚定边 0..3 走 `CMP-VALUE2`；`pv-pad=1200` / `pv-arrow-w=800` / `pv-gap=800` + `pv-open` / `pv-set-open`（负数归 0）/ `pv-side` / `pv-set-side`（夹到 0..3）/ `pv-title` / `pv-set-title` / `pv-count` / `pv-line` / `pv-add` / `pv-set-line` / `pv-clear`（写操作先查 `F-DISABLED`）+ 从 `cs-is-container` 白名单里拿掉 + `cs-measure` 分支（关闭 → `CS-MIN-LEAF-W` × `CS-BTN-H`；展开 → 宽 = `2*CS-PAD + 箭头 800 + 间隙 800 + max(标题宽, 最长正文行)`，下限 6400，高 = `2*CS-PAD + 标题行 +（有标题且有正文 ? CS-SEP-H : 0）+ N*CS-LINE-H`，下限 `CS-BTN-H`）+ `cs-paint-popover`（FIELD 底 + 按 `side` 贴边的 800×800 ACCENT 箭头方块 + 标题 FG + 条件 TRACK 分隔线 + N 行正文 MUTED；关闭时一笔画不出），**不新增节点种类**） |
| 43 | `tooltip` | `C-TOOLTIP` | `cmp-tooltip(text)` | visible, text |
| 44 | `hover_card` | `C-HOVER-CARD` | `cmp-hover-card()` | visible, content |
| 45 | `notification` | `C-NOTIFICATION` | `cmp-notification(text)` | level, timeout（阶段44：`LV-*` + `lv-slot` + `msg-level` / `msg-set-level` + `ntf-timeout` / `ntf-remain` / `ntf-set-timeout` / `ntf-set-remain` / `ntf-ratio` + `cs-paint-notification` 文本一行 + 剩余时间条，**不新增节点种类**） |
| 46 | `alert` | `C-ALERT` | `cmp-alert(text)` | level, closable（阶段44：共用 `msg-level` / `msg-set-level` + `alert-closable` / `-set-closable` / `alert-close-w` / `alert-close-x` / `alert-hit`（`-1` / `0` 主体 / `1` 关闭钮）+ `cs-paint-alert` 左色条 + 字形 + 文本 + 右端 `'x'`，**不新增节点种类**） |
| 47 | `message` | `C-MESSAGE` | `cmp-message(text)` | level（阶段44：共用 `LV-*` + `lv-slot` + `msg-level` / `msg-set-level` + `cs-level-color` / `cs-level-glyph` + `cs-paint-message` 左色条 + 字形 + 文本，**不新增节点种类**） |
| 48 | `message_scroller` | `C-MESSAGE-SCROLLER` | `cmp-message-scroller()` | messages, scroll（阶段62：消息池 `MS-MSG` / `MS-AUTH`（`[]str`，扁平只增）+ 每句柄 `MS-SBASE` / `MS-N`（随 `cmp-alloc` 长），「输入中」走 `CMP-VALUE`（0 / 1，负数归 0）；`ms-pad` / `ms-gap` / `ms-count` / `ms-text` / `ms-author` / `ms-add` / `ms-set-text` / `ms-set-author` / `ms-clear`（连输入中一起清）/ `ms-typing` / `ms-set-typing`（写操作先查 `F-DISABLED`）+ 从 `cs-is-container` 白名单里拿掉 + `cs-measure` 分支（宽 = `2*CS-PAD + max(最长作者, 最长正文)`，下限 6400；高 = `2*CS-PAD + N*2*CS-LINE-H + (N-1)*CS-SEP-H +（输入中 ? CS-SEP-H + CS-LINE-H : 0）`，下限 `CS-BTN-H`）+ `cs-paint-message-scroller`（FIELD 底 + 作者 MUTED + 正文 FG + 条间 TRACK 分隔线，输入中时末尾再补一条分隔线 + 「typing...」ACCENT），**不新增节点种类**） |
| 49 | `sheet` | `C-SHEET` | `cmp-sheet(title)` | open, side, title（阶段70：展开位 `SHT-OPEN []i64` + 停靠边 `SHT-SIDE []i64`（**两个 per-handle 池都随 `cmp-alloc` 长并 `.push(0)`**，side: 0=左 1=右 2=上 3=下），标题走 `CMP-TEXT[h]`；`sht-pad=1200` / `sht-gap=800` / `sht-bar-w=1200` / `sht-min-w=2400` / `sht-body-h=8000` 五个几何常量 + `sht-title` / `sht-set-title` / `sht-open` / `sht-set-open` / `sht-show` / `sht-hide` / `sht-toggle` / `sht-side` / `sht-set-side`（越界夹到 [0,3]），写操作先查 `F-DISABLED`）+ 从 `cs-is-container` 白名单里拿掉 + `cs-measure` 分支（宽 = `2*sht-pad + max(标题宽, sht-min-w)`，下限 6400；高 = `2*sht-pad + CS-LINE-H`，展开再加 `sht-body-h`，下限 `CS-BTN-H`）+ `cs-paint-sheet`（FIELD 底 + 停靠边一条 ACCENT 抓杆 + 标题 FG/收起 MUTED，停靠左侧时给抓杆让位），**不新增节点种类**） |
| 50 | `progress` | `C-PROGRESS` | `cmp-progress()` | value, max, indeterminate |
| 51 | `skeleton` | `C-SKELETON` | `cmp-skeleton()` | lines, animated（阶段54：`CMP-VALUE` = 扫光相位 0..100、`CMP-VALUE2` = 行数（夹 0..8）、`CMP-VALUE3` = 行距（下限 800），`sk-lines` / `sk-set-lines` / `sk-gap` / `sk-set-gap` / `sk-phase` / `sk-set-phase` + `cs-measure` 分支（宽 = `2*CS-PAD + 12000`；高 = `2*CS-PAD + N*CS-LINE-H + (N-1)*gap`，下限 `CS-BTN-H`）+ `cs-paint-skeleton`（TRACK 底 + N 条 MUTED 占位条 + 相位扫光 FG 带），**不新增节点种类**） |
| 52 | `shimmer` | `C-SHIMMER` | `cmp-shimmer()` | duration |
| 53 | `bubble` | `C-BUBBLE` | `cmp-bubble(text)` | text, position（阶段45：`CMP-VALUE` 存 position `0` 左 / `1` 右、`CMP-COLOR` 存底色，`bubble-pos` / `bubble-set-pos`（夹紧 `0..1`）+ `cs-paint-bubble` 底色方块 + 按 position 对齐的文本 + 底边尾巴方块，**不新增节点种类**） |
| 54 | `speech` | `C-SPEECH` | `cmp-speech(text)` | text, lang（阶段45：`CMP-TEXT2` 存语言标签（空 = 不画标签行），`speech-lang` / `speech-set-lang` + `cs-paint-speech` 文本行 + 有标签时的细分隔线 + 标签行，**不新增节点种类**） |
| 55 | `inspector` | `C-INSPECTOR` | `cmp-inspector()` | target, fields（阶段61：目标名走 `CMP-TEXT`，字段池 `IN-LAB` / `IN-VAL`（扁平只增）+ 每句柄 `IN-SBASE` / `IN-N`（随 `cmp-alloc` 长），`in-pad` / `in-marker-w` / `in-gap` 三个几何常量 + `in-target` / `in-set-target` / `in-count` / `in-label` / `in-value` / `in-add` / `in-set-label` / `in-set-value` / `in-clear`（写操作先查 `F-DISABLED`）+ 从 `cs-is-container` 白名单里拿掉 + `cs-measure` 分支（宽 = `2*CS-PAD + 标记条 800 + 间隙 800 + max(目标名宽, 最长标签 + 间隙 + 最长值)`，下限 6400；高 = `2*CS-PAD + 目标行 CS-LINE-H + N*CS-LINE-H + (N-1)*CS-SEP-H`，下限 `CS-BTN-H`）+ `cs-paint-inspector`（FIELD 底 + 目标行 ACCENT 标记条 + FG 目标名，字段行 MUTED 标签 + FG 值，行间 TRACK 分隔线），**不新增节点种类**） |
| 56 | `window_border` | `C-WINDOW-BORDER` | `cmp-window-border()` | radii, width |
| 57 | `native_menu` | `C-NATIVE-MENU` | `cmp-native-menu()` | items（阶段51：`CMP-TEXT` = 按钮文案、`VALUE` = 项数、`VALUE2` = 选中项 + 1（0 = 未选）、`COLOR` = 强调色，展开用 `F-OPEN`；`nm-count` / `nm-set-count`（夹 0..8）/ `nm-label` / `nm-set-label` / `nm-index` / `nm-set-index`（夹 −1..count−1）/ `nm-item-off` / `nm-set-item-off` / `nm-open` / `nm-close` / `nm-toggle`；数据池 `NM-LABEL` / `NM-SEL` 按句柄 × `NM-MAX` 定步长、`NM-CN` 每句柄一格，均随 `nm-ensure` 按需长；`cs-measure` 给 intrinsic 宽（`2*CS-PAD + max(按钮文案, 各项文案) + 2400`，下限 `CS-MIN-LEAF-W`）/ 高（`CS-BTN-H`，展开再加 `CS-GAP + count*CS-LINE-H`），`cs-paint-menu` 画字段底按钮 + 文案 + `'v'` / `'^'` 箭头 + 展开时下方弹层（选中项铺 `ACCENT`、禁用项 `TRACK` 底 + `MUTED` 文案），`cs-menu-hit(h,px,py)` 返回弹层命中项（禁用项 / 未展开 / 越界返回 −1）；**不新增节点种类**） |
| 58 | `attachment` | `C-ATTACHMENT` | `cmp-attachment(name)` | name, size（阶段46：`CMP-TEXT` = 文件名、`CMP-VALUE` = 字节数，`att-name` / `att-set-name` / `att-size` / `att-set-size`（负数夹 0）/ `att-size-text`（1024 进位）+ `cs-paint-attachment` 左图标方块 + 文件名 + 右端尺寸文案，**不新增节点种类**） |

### E. 数据展示（16）
| # | 模块 | kind | 构造函数 | 能力 |
|---|---|---|---|---|
| 59 | `table` | `C-TABLE` | `cmp-table(cols,rows)` | cells, sort, selection |
| 60 | `list` | `C-LIST` | `cmp-list()` | items, selected |
| 61 | `virtual_list` | `C-VIRTUAL-LIST` | `cmp-virtual-list(len)` | visible range, item height |
| 62 | `tree` | `C-TREE` | `cmp-tree()` | nodes, expanded, selected |
| 63 | `accordion` | `C-ACCORDION` | `cmp-accordion()` | sections, expanded（阶段56：`AC-TITLE` / `AC-BODY` 扁平只增组池（`[]str`）+ `AC-EXP` / `AC-BBASE` / `AC-BN`（`[]i64`，与 `AC-TITLE` 同下标 / 同组号）+ 每句柄 `AC-SBASE` / `AC-N`，`ac-count` / `ac-title` / `ac-body-count` / `ac-body-line` / `ac-expanded` / `ac-open-count` / `ac-add` / `ac-add-open` / `ac-add-body` / `ac-set-title` / `ac-set-expanded` / `ac-expand` / `ac-collapse` / `ac-toggle` / `ac-clear`（写操作先查 `F-DISABLED`）+ 从 `cs-is-container` 白名单里拿掉 + `cs-measure` 分支（宽 = `2*CS-PAD + 800 + 800 + 标题列`，标题列取最长「组标题 / 展开组正文行」、下限 6400；高 = `2*CS-PAD + Σ组头 4000 + 展开组正文行*CS-LINE-H + 组间分隔线 CS-SEP-H`，下限 `CS-BTN-H`）+ `cs-paint-accordion`（FIELD 底 + 展开符 `'-'` / `'>'` ACCENT + 标题 FG + 展开组正文 MUTED + 组间 TRACK 分隔线），**不新增节点种类**） |
| 64 | `carousel` | `C-CAROUSEL` | `cmp-carousel()` | slides, current（阶段57：`CR-SLIDE` 扁平只增幻灯片池（`[]str`）+ 每句柄 `CR-SBASE` / `CR-N`，当前张走 `CMP-VALUE`，`cr-count` / `cr-slide` / `cr-add`（返回新下标，disabled 时 -1）/ `cr-set-slide` / `cr-current` / `cr-set-current` / `cr-select` / `cr-next` / `cr-prev` / `cr-clear`（写操作先查 `F-DISABLED`）+ 从 `cs-is-container` 白名单里拿掉 + `cs-measure` 分支（宽 = `2*CS-PAD + 2*箭头 800 + 2*间隙 800 + 最长幻灯片文案`，下限 6400；高 = `2*CS-PAD + CS-BTN-H + CS-SEP-H + CS-LINE-H` = 8900）+ `cs-paint-carousel`（FIELD 底 + 上带「`<` 当前幻灯片 `>`」，箭头到头 / 到尾转 MUTED + TRACK 分隔线 + 下带一行圆点，当前点 ACCENT），**不新增节点种类**） |
| 65 | `chart` | `C-CHART` | `cmp-chart()` | series, axes（阶段50：`ch-series` / `ch-points` / `ch-name` / `ch-max` / `ch-value` / `ch-total` / `ch-peak` + 对应 `ch-set-*`；`cs-paint-chart` 画字段底 + 左/下轴 + 值轴上限 + 分组柱（ACCENT/OK/DANGER 按序列序）+ x 刻度 + 右对齐图例，**不新增节点种类**） |
| 66 | `plot` | `C-PLOT` | `cmp-plot()` | points, scale（阶段50：`pl-count` / `pl-scale` / `pl-x` / `pl-y` / `pl-min-x` / `pl-max-x` / `pl-min-y` / `pl-max-y` + 对应 `pl-set-*`；`cs-paint-plot` 画字段底 + 过原点两轴 + 上下量程 + 方块点 + L 形连线，**不新增节点种类**） |
| 67 | `rating` | `C-RATING` | `cmp-rating(max)` | value, max（阶段43：`rating-max` / `-value` / `-set` / `-cell` / `-hit` + `cs-paint-rating`） |
| 68 | `description_list` | `C-DESCRIPTION-LIST` | `cmp-description-list()` | pairs（阶段55：`DL-LAB` / `DL-VAL` 扁平只增行池 + 每句柄 `DL-SBASE` / `DL-N`，`dl-count` / `dl-label` / `dl-value` / `dl-add` / `dl-set-label` / `dl-set-value` / `dl-clear`（写操作先查 `F-DISABLED`）+ 从 `cs-is-container` 白名单里拿掉 + `cs-measure` 分支（宽 = `2*CS-PAD + 12000 + 800 + 值列`，值列取最长值、下限 6400；高 = `2*CS-PAD + N*CS-LINE-H + (N-1)*CS-SEP-H`，下限 `CS-BTN-H`）+ `cs-paint-description-list`（FIELD 底 + 左标签 MUTED / 右值 FG 右对齐 + 行间 TRACK 分隔线），**不新增节点种类**） |
| 69 | `group_box` | `C-GROUP-BOX` | `cmp-group-box(title)` | title, children（阶段59：`gb-title` / `gb-set-title`（写先查 `F-DISABLED`）+ `gb-title-h()`（标题行高 4000，与 `CS-BTN-H` 同高）；目录标「title, children」所以它是**真容器**——标题走 `CMP-TEXT`，子节点是视觉节点，`cs-is-container` 白名单**保留**。`cs-measure` 新增分支（宽 = `2*CS-PAD + max(标题宽, 内容区宽)`，下限 6400；高 = `2*CS-PAD + 标题行 4000 + (N>0 ? 间隙 800 + 内容区高 : 0)`，下限 `CS-BTN-H`），`cs-layout` 把「内容区」当矩形重排一次（后跑的赢），`cs-paint` 容器守卫放开到「没子节点也画面板」并叠标题（FG）+ TRACK 分隔线，**不新增节点种类**） |
| 70 | `collapsible` | `C-COLLAPSIBLE` | `cmp-collapsible(title)` | open, title（阶段58：`CL-LINE` 扁平只增正文行池（`[]str`）+ 每句柄 `CL-SBASE` / `CL-N`，标题走 `CMP-TEXT`，展开态复用既有 `F-OPEN` 位（与声明式 `dv-on-open` 一致），`cl-title` / `cl-set-title` / `cl-open` / `cl-set-open` / `cl-expand` / `cl-collapse` / `cl-toggle` / `cl-count` / `cl-body-line` / `cl-add-body` / `cl-set-body-line` / `cl-clear`（写操作先查 `F-DISABLED`）+ 从 `cs-is-container` 白名单里拿掉 + `cs-measure` 分支（宽 = `2*CS-PAD + max(展开符 800 + 间隙 800 + 标题, 最长正文行)`，下限 6400；高 = `2*CS-PAD + 头高 4000`，展开再加 `N*CS-LINE-H`，下限 `CS-BTN-H`）+ `cs-paint-collapsible`（FIELD 底 + 头部展开符 `v` / `>` 均 ACCENT + 标题 FG + 展开时 TRACK 分隔线 + N 行正文 MUTED），**不新增节点种类**） |
| 71 | `diff` | `C-DIFF` | `cmp-diff()` | old, new, hunks（阶段49：`CMP-TEXT` = old、`TEXT2` = new，`df-old` / `df-set-old` / `df-new` / `df-set-new`（写入口尊重 `F-DISABLED`）+ `df-rows` / `df-row-op`（0 上下文 / 1 新增 / 2 删除）/ `df-row-text` / `df-row-old-no` / `df-row-new-no` + `df-hunks` / `df-hunk-start` / `df-count-add` / `df-count-del` / `df-max-len` / `df-summary`（`'+a -d'`）；内部 `df-split` 按 `'\n'` 切行、`df-lcs` 动态规划 + `df-backtrack` 回溯成行，结果缓存在 `DF-CACHE-*`（句柄 + old + new 三者一致才复用）；`cs-measure` 兜底分支给 intrinsic 宽（`2*pad + gutter + maxlen*char-w`，下限 `CS-MIN-LEAF-W`）/ 高（`2*pad + rows*line-h`），`cs-paint-diff` 字段底 + 双列行号槽（旧号右对齐到 `x+pad+gutter-char-w`、新号右对齐到 `x+pad+gutter`）+ 行级 glyph（上下文空格 / `'+'` OK / `'-'` DANGER）+ 文本按行级别着色，**不新增节点种类**） |
| 72 | `highlighter` | `C-HIGHLIGHTER` | `cmp-highlighter(code)` | code, language（阶段48：`CMP-TEXT` = 代码、`TEXT2` = 语言、`VALUE` = 缩进（每级空格数，默认 4），`hl-code` / `hl-set-code` / `hl-lang` / `hl-set-lang` / `hl-tab` / `hl-set-tab` + `hl-lines` / `hl-line-start` / `hl-line-len-b` / `hl-line` / `hl-expand-len` / `hl-max-len` + `cs-paint-highlighter` 字段底 + 右对齐行号 + 逐 token 着色（复用 `ed-lex-line`）+ 右端语言列，**不新增节点种类**） |
| 73 | `clipboard` | `C-CLIPBOARD` | `cmp-clipboard()` | content（阶段46：`CMP-TEXT` = 内容，`clip-content` / `clip-set-content` / `clip-len` + `cs-paint-clipboard` 字段底 + 内容 + 右端复制方块，**不新增节点种类**） |
| 74 | `scroll` | `C-SCROLL` | `cmp-scroll()` | offset, content size |

### F. 布局 / 容器（10）
| # | 模块 | kind | 构造函数 | 能力 |
|---|---|---|---|---|
| 75 | `resizable` | `C-RESIZABLE` | `cmp-resizable()` | panels, sizes, direction |
| 76 | `dock` | `C-DOCK` | `cmp-dock()` | panels, tabs, splits |
| 77 | `grid_layout` | `C-GRID` | `cmp-grid(cols,rows)` | cells, gaps |
| 78 | `form`（容器向）已计 B27 | — | — | — |
| 79 | `text` | `C-TEXT` | `cmp-text-node(text)` | styled text runs |
| 80 | `theme` | `C-THEME` | `cmp-theme()` | palette, size, mode（阶段48：`CMP-TEXT` = 名字、`VALUE` = 模式（0 亮 / 1 暗）、`VALUE2` = 密度（0 紧凑 / 1 标准 / 2 宽松）、`CMP-COLOR` = 强调色，`th-name` / `th-set-name` / `th-mode` / `th-set-mode` / `th-density` / `th-set-density` / `th-accent` / `th-set-accent` / `th-swatch` / `th-mode-name` / `th-bg` / `th-fg` / `th-muted` / `th-swatch-color` / `th-swatch-label` + `cs-paint-theme` 字段底 + 名字 / 模式 + 四块色板（底 / 前景 / 强调 / 次要），**不新增节点种类**） |
| 81 | `sizing` | `C-SIZING` | `cmp-sizing()` | width, height, min/max |
| 82 | `styled` | `C-STYLED` | `cmp-styled()` | base style container |
| 83 | `root` | `C-ROOT` | `cmp-root()` | root container |
| 84 | `window_ext` | `C-WINDOW-EXT` | `cmp-window-ext()` | window options（阶段52：`CMP-TEXT` = 标题、`VALUE` = flags（1 可缩放 / 2 全屏 / 4 置顶 / 8 边框）、`VALUE2` = 总开关（1 = 接管真窗口，0 = 只存状态）、`COLOR` = 强调色；`we-title` / `we-set-title`（同步 `TEXT`）/ `we-flags` / `we-set-flags`（同步 `VALUE`）/ `we-resizable` / `we-set-resizable` / `we-fullscreen` / `we-set-fullscreen` / `we-always-on-top` / `we-set-always-on-top` / `we-bordered` / `we-set-bordered` / `we-opacity` / `we-set-opacity`（夹 0..100）/ `we-min-x` / `we-min-y` / `we-set-min-size`（夹 ≥0）/ `we-max-x` / `we-max-y` / `we-set-max-size`（夹 ≥0）/ `we-w` / `we-h` / `we-set-size`（夹 ≥0，0 = 不覆盖）/ `we-on` / `we-set-on`；数据池 `WE-TITLE` / `WE-FLAGS` / `WE-OP` 每句柄一格、`WE-MIN` / `WE-MAX` / `WE-SIZE` 每句柄两格，均随 `we-ensure` 按需长；真窗口调用不在组件层 —— `components.no` 不能 `import app.no`（`app -> cmp-scene -> components` 成环），改由 `app.no` 的 `app-window-apply(h)` / `app-window-apply-all()`（每帧 `app-frame()` 调）经 12 个 `#{c}` FFI 推给 SDL，读回走 `app-window-title()` / `app-window-size()` / `app-window-flags()`；`cs-measure` 给 intrinsic 宽（`2*ch-pad() + 16000`）/ 高（`2*ch-pad() + ch-title-h() + 8*CS-LINE-H` = 24000），`cs-paint-window-ext` 画字段底 + 标题 + 分隔线 + 8 行状态（标签列 `MUTED` 左对齐、数值列 `FG` 右对齐），聚焦时四边 `ACCENT` 描边，**不新增节点种类**） |

### G. 搜索 / 其他（2）
| # | 模块 | kind | 构造函数 | 能力 |
|---|---|---|---|---|
| 85 | `searchable_list` | `C-SEARCHABLE-LIST` | `cmp-searchable-list()` | query + items + filtered（阶段66：条目池 `SL-ITEM []str`（扁平只增）+ 每句柄基址 `SL-SBASE` / 条数 `SL-N`；过滤结果另开一张扁平池 `SL-MATCH []i64`（存条目下标）+ 每句柄 `SL-MBASE` / `SL-MN`，高亮 `SL-CUR`（**以上六个 per-handle 池都必须在 `cmp-alloc` 里 `.push(0)`，`SL-CUR` 推 -1**）。查询串直接复用 `CMP-TEXT[h]`（同 combobox）。公开 `sl-pad=1200` / `sl-gap=800` / `sl-min-w=6400` + `sl-count` / `sl-item` / `sl-add` / `sl-set-item` / `sl-clear` / `sl-query` / `sl-set-query` / `sl-refresh` / `sl-match-count` / `sl-match` / `sl-highlight` / `sl-selected` / `sl-select` / `sl-select-next` / `sl-select-prev`（写操作先查 `F-DISABLED`），从 `cs-is-container` 白名单里拿掉 + `cs-measure` 分支（宽 = `2*CS-PAD + max(查询串宽, 最长匹配标签)` 下限 6400；高 = `2*CS-PAD + CS-LINE-H + (有匹配 ? CS-SEP-H + N*CS-LINE-H : 0)`）+ `cs-paint-searchable-list`（FIELD 底 + 查询行 FG + TRACK 分隔线 + N 行标签 FG，高亮行铺一条 TRACK 整行底），**不新增节点种类**） |
| 86 | `index_path` | `C-INDEX-PATH` | `cmp-index-path()` | path segments（阶段46：扁平段池 `IP-SEG` + 每句柄基址 / 段数，`ip-count` / `ip-seg` / `ip-add-seg` / `ip-clear` / `ip-sep` + `cs-paint-index-path` 各段 + 「 / 」分隔符，**不新增节点种类**） |

## 覆盖统计

- 唯一组件 kind：**约 84 个**（86 模块中 link/sidebar-group/form 各复用/归并 2 项）
- 核心扎实 10：`C-BUTTON` `C-INPUT` `C-CHECKBOX` `C-SWITCH` `C-SELECT` `C-TAB` `C-DIALOG` `C-TOOLTIP` `C-PROGRESS` `C-SLIDER`
- 功能对等长期项：**已全部补齐**（上一条所述的「核心交互组件」）。**注意别把这句读成全目录**：
  下表 86 项里仍有约 35 个 kind 只有一个构造函数、没有任何属性 API；其中 4 个叶子 kind
  至今仍落在 `cs-paint-leaf` 的通用 `rect + text` 兜底分支里，画不出该有的样子。
  已补上命令式侧的：`C-TABLE`（阶段 35 `cmp-table-*`）、`C-TREE`（阶段 33 `cmp-tree-*`）、`C-SCROLL`（阶段 34 `cmp-editor-*`）、`C-DOCK`（阶段 36 `cmp-dock-*` + `cs-dock-*`，间隙即拖杆，位置真源 `DK-SPLIT-V`）、`C-COMMAND`（阶段 37 `cmp-command-*`，根就是 `C-COMMAND` 普通竖排容器，**不新增节点种类**）、`C-VIRTUAL-LIST`（`cmp-virtual-list = (len i64) (h i64)`，`value` = 条目数，命令式侧的窗口滚动由 `cmp-command-sync` / `cmp-table-sync` / `cmp-tree-sync` 复用）、`C-INPUT`（阶段 38：`INP-*` 光标池 + `input-insert` / `-delete` / `-syntax` / `-lex-*` + `cs-paint-input` 富文本渲染，**不新增节点种类**；着色复用阶段 34 的 `ed-lex-line`）、`C-TEXTAREA`（阶段 39：`TA-OFF` 视口池 + `ta-line-*` / `ta-caret-*` / `ta-move-*` / `ta-follow` + `cs-paint-textarea` 逐行渲染，编辑复用阶段 38 的三条原语，**不新增节点种类**）、`C-NUMBER-INPUT` / `C-STEPPER` / `C-SEARCH`（阶段 40：`CMP-VALUE4` 步长池 + `num-range` / `num-step-set` / `num-step` / `num-min` / `num-max` / `num-clamp` / `num-snap` / `num-value` / `num-set` / `num-bump` / `num-commit` + `search-query` / `search-clear` / `search-hit` / `stepper-hit` + `cs-paint-number-input` / `cs-paint-stepper` / `cs-paint-search`，值域槽位沿用 slider 的 `VALUE` / `VALUE3` / `VALUE2`，**不新增节点种类**）、`C-OTP-INPUT`（阶段 41：`OT-CODE` 定长码池 + `OT-CUR` 活动槽池 + `otp-slots` / `otp-cur(-set)` / `otp-code` / `otp-digit` / `otp-set-digit` / `otp-clear-slot` / `otp-clear` / `otp-filled` / `otp-digits` / `otp-value` / `otp-complete` / `otp-put` / `otp-backspace` / `otp-delete` / `otp-left` / `otp-right` / `otp-set` / `otp-slot-w` / `otp-slot-x` / `otp-hit` + `cs-paint-otp`，槽数沿用 `CMP-VALUE`，**不新增节点种类**）、`C-COMBOBOX`（阶段 42：`CB-MBASE` / `CB-MN` / `CB-MLIST` 匹配表 + `CB-MCUR` 高亮（均随 `cmp-alloc` 长）+ `combobox-refresh` / `-query` / `-set-query` / `-add-option` / `-option-count` / `-option` / `-selected` / `-choose` / `-match-count` / `-match` / `-match-label` / `-match-index` / `-match-index-of` / `-highlight` / `-highlight-set` / `-highlight-move` / `-choose-match` / `-apply` / `-insert` / `-backspace` / `-delete` / `-clear` + `cs-paint-combobox` + `cs-measure` / `cs-layout` / `cs-hit` 都走匹配表（`cs-hit-child`）；查询串沿用 `CMP-TEXT` + `INP-CAR` 光标池，选项与 select 同构存 `CHILD-IDX`，**不新增节点种类**）、`C-RATING` / `C-PAGINATION` / `C-EMPTY`（阶段 43：`rating-max` / `-value` / `-set`（夹紧 0..max、`F-DISABLED` 不动）/ `-cell` / `-hit`（命中第几颗星）+ `pagination-total` / `-page` / `-clamp` / `-set` / `-prev` / `-next` / `-btn-w` / `-label` / `-hit`（-1 / 0 上一页 / 1 标签 / 2 下一页）+ `slider-min` / `slider-max` + `cs-paint-rating` / `cs-paint-pagination` / `cs-paint-empty` + `cs-measure` 三条 intrinsic 分支；`C-PAGINATION` 从 `cs-is-container` 白名单拿掉 —— 页码按钮是**数据**不是视觉子节点；empty 的描述走 `CMP-TEXT2`，**不新增节点种类**）、`C-MESSAGE` / `C-ALERT` /
  `C-NOTIFICATION`（阶段 44：共用 `LV-*` + `lv-slot` + `msg-level` / `msg-set-level` +
  `alert-closable` / `-set-closable` / `alert-close-w` / `alert-close-x` / `alert-hit` +
  `ntf-timeout` / `ntf-remain` / `ntf-set-timeout` / `ntf-set-remain` / `ntf-ratio` +
  `cs-level-color` / `cs-level-glyph` + `cs-paint-message` / `-alert` /
  `-notification`，槽位沿用 `VALUE` / `VALUE2` / `VALUE3`，**不新增节点种类**）、
  `C-BUBBLE` / `C-SPEECH`（阶段 45：`bb-pad` / `bb-tail` + `bubble-pos` /
  `bubble-set-pos` + `speech-lang` / `speech-set-lang` + `cs-bubble-tail-x` +
  `cs-paint-bubble` / `cs-paint-speech`，槽位沿用 `VALUE` / `TEXT` / `TEXT2`，
  **不新增节点种类**）、`C-ATTACHMENT` / `C-CLIPBOARD` / `C-INDEX-PATH`（阶段 46：
  `att-name` / `-set-name` / `att-size` / `-set-size`（负数夹 0）/ `att-size-text`
  （1024 进位）+ `clip-content` / `-set-content` / `clip-len` + 扁平段池 `IP-SEG` /
  `IP-SBASE` / `IP-SN`（随 `cmp-alloc` 长）与 `ip-count` / `ip-seg` / `ip-add-seg` /
  `ip-clear` / `ip-sep` + `cs-paint-attachment` / `-clipboard` / `-index-path`，
  槽位沿用 `TEXT` / `VALUE` + 扁平段池，**不新增节点种类**）、
  `C-COLOR-PICKER` / `C-DATE-PICKER`（阶段 47：`cp-color` / `cp-red` / `cp-green` /
  `cp-blue` / `cp-alpha` + 四个 `cp-set-*`（写通道重算 hue）+ `cp-hue` /
  `cp-set-hue`（HSV 正算，S=191/255、V=235/255）+ `cp-hue-of`（RGB 反算）+
  `cp-hex` + `dp-year` / `dp-month` / `dp-day` + 四个 `dp-set-*` + `dp-set-date`
  （写月 / 年重夹日）+ `dp-leap` / `dp-days` / `dp-next-month` + `dp-label` +
  `cs-paint-color-picker` / `cs-paint-date-picker`，槽位沿用 `COLOR` / `VALUE` /
  `VALUE2` / `VALUE3`，**不新增节点种类**）、
  `C-HIGHLIGHTER` / `C-THEME`（阶段 48：`hl-code` / `hl-set-code` / `hl-lang` /
  `hl-set-lang` / `hl-tab` / `hl-set-tab` + `hl-lines` / `hl-line-start` /
  `hl-line-len-b` / `hl-line` / `hl-expand-len` / `hl-max-len` +
  `cs-paint-highlighter`（行几何复用阶段39 的 `ta-line-*` 算法、着色复用阶段38 的
  `ed-lex-line`）；`th-name` / `th-set-name` / `th-mode` / `th-set-mode` /
  `th-density` / `th-set-density` / `th-accent` / `th-set-accent` / `th-swatch` /
  `th-mode-name` / `th-bg` / `th-fg` / `th-muted` / `th-swatch-color` /
  `th-swatch-label` + `cs-paint-theme`，槽位沿用 `TEXT` / `TEXT2` / `VALUE` /
  `VALUE2` / `COLOR`，**不新增节点种类**）、
  `C-DIFF`（阶段 49：`df-old` / `df-set-old` / `df-new` / `df-set-new` +
  `df-rows` / `df-row-op` / `df-row-text` / `df-row-old-no` / `df-row-new-no` +
  `df-hunks` / `df-hunk-start` / `df-count-add` / `df-count-del` / `df-max-len` /
  `df-summary` + `cs-paint-diff`，槽位沿用 `TEXT`（old）/ `TEXT2`（new），
  **不新增节点种类**）、
  `C-CHART` / `C-PLOT`（阶段 50：`ch-series` / `ch-set-series`（夹 0..3）/
  `ch-points` / `ch-set-points`（夹 0..8）/ `ch-name` / `ch-set-name` /
  `ch-max` / `ch-set-max`（`VALUE2`，未设过默认 100）/ `ch-value` / `ch-set-value`
  （夹 0..max）/ `ch-total` / `ch-peak` + `pl-count` / `pl-set-count`（夹 0..16）/
  `pl-scale` / `pl-set-scale`（≥1）/ `pl-x` / `pl-y` / `pl-set-x` / `pl-set-y`
  （夹 ±scale）/ `pl-min-x` / `pl-max-x` / `pl-min-y` / `pl-max-y` +
  `cs-paint-chart`（字段底 + 左/下轴 + 值轴上限 + 分组柱 + x 刻度 + 右对齐图例）/
  `cs-paint-plot`（字段底 + 过原点两轴 + 上下量程 + 方块点 + L 形连线，细矩形拼）；
  数据池 `CH-*` / `PL-*` 按句柄定步长，**不新增节点种类**）、
  `C-NATIVE-MENU`（阶段 51：`nm-count` / `nm-set-count`（夹 0..8，同步 `VALUE`）/
  `nm-label` / `nm-set-label` / `nm-index` / `nm-set-index`（夹 −1..count−1，
  存 `VALUE2` = 选中 + 1）/ `nm-item-off` / `nm-set-item-off` / `nm-open` /
  `nm-close` / `nm-toggle` + `cs-measure` 分支 + `cs-paint-menu` +
  `cs-menu-hit`，槽位沿用 `TEXT` / `VALUE` / `VALUE2` / `COLOR`，
  数据池 `NM-LABEL` / `NM-SEL`（句柄 × `NM-MAX` 定步长）/ `NM-CN`（每句柄一格），
  **不新增节点种类**）、
  `C-WINDOW-EXT`（阶段 52：`we-title` / `we-set-title`（同步 `TEXT`）/
  `we-flags` / `we-set-flags`（同步 `VALUE`）/ `we-resizable` / `we-set-resizable` /
  `we-fullscreen` / `we-set-fullscreen` / `we-always-on-top` / `we-set-always-on-top` /
  `we-bordered` / `we-set-bordered` / `we-opacity` / `we-set-opacity`（夹 0..100）/
  `we-min-x` / `we-min-y` / `we-set-min-size` / `we-max-x` / `we-max-y` /
  `we-set-max-size` / `we-w` / `we-h` / `we-set-size`（夹 ≥0）/ `we-on` /
  `we-set-on`（`VALUE2` 总开关）+ `cs-measure` 分支 + `cs-paint-window-ext`；
  数据池 `WE-TITLE` / `WE-FLAGS` / `WE-OP`（每句柄一格）/ `WE-MIN` / `WE-MAX` /
  `WE-SIZE`（每句柄两格），**不新增节点种类**）、
  `C-BREADCRUMB`（阶段 53：`bc-count` / `bc-item` / `bc-add-item` / `bc-clear` /
  `bc-sep` / `bc-set-sep` / `bc-last` / `bc-join` + `cs-measure` 分支 +
  `cs-paint-breadcrumb`；数据池 `BC-ITEM`（扁平只增）+ `BC-SBASE` / `BC-N` /
  `BC-SEP`（每句柄一格，`BC-SEP` 随 `cmp-alloc` 填默认「 / 」），
  **不新增节点种类**）。
  声明式层的超集分别是阶段 28–32。
- 阶段 52 的 `C-WINDOW-EXT` 是唯一「真的改窗口」的组件，也是最容易踩层依赖的一个：
  `components.no` **不能** `import app.no`（`app.no -> cmp-scene -> components` 会成环），
  所以组件层只留纯状态（`we-*` 池），把 12 个 `#{c}` FFI 与 `app-window-apply` /
  `app-window-apply-all` / `app-window-title` / `app-window-size` /
  `app-window-flags` 全放在 `app.no`，由每帧 `app-frame()` 统一扫已挂载的面板。
  坑三条：① `we-title` 是 `[]str`，`ct-get` 是 `[]i64` 专用的，必须走
  `ed-arr-get-s` / `ed-arr-set-s`，否则报 `cannot assign i64 value to str variable`；
  ② `ct-or` / `ct-and` / `ct-not` 原本不存在，位运算只能写 `a | b`，而
  「清掉某一位」需要 `ct-and(f, ct-not(bit))`（`ct-not` 用 `ct-sub(-1, a)` 实现）；
  ③ `cs-measure` 的 intrinsic 高是 `2*ch-pad() + ch-title-h() + 8*CS-LINE-H`
  = 2400 + 2400 + 19200 = **24000**（`ch-title-h()` 是 2400，不是 3200；
  `ch-plot-h()` 才是 32000），写测试时按错记的常量算会整段渲染断言漂移。
- 阶段 53 的 `C-BREADCRUMB` 是阶段 46 `C-INDEX-PATH` 的孪生（同样是「条目池 + 基址」
  的扁平只增结构），但配色分「祖先 / 当前页」两档：除最后一项外画 `MUTED`，
  最后一项画 `FG`，分隔符画 `TRACK`。坑两条：① `->` 链**不是互斥的** ——
  `bc-last` 一开始写成「先 `i = count - 1`，再 `(i <= 0) -> { i = -1 }`」，
  第一条把 `i` 改成 0 之后第二条立刻又命中，末项下标恒为 −1；必须先把
  `n = bc-count(h)` 存进局部，再从 −1 起只让一条分支写。② 条目池是 `[]str`，
  读写都得走 `ed-arr-get-s` / `ed-arr-set-s`（`ct-get` / `ct-set` 只吃 `[]i64`），
  分隔符默认值在 `cmp-alloc` 里就填好，避免「空串 = 未设置」的二义。
- 阶段 54 的 `C-SKELETON` 是**叶子**（不在 `cs-is-container` 白名单里），
  补之前它连 `cs-measure` 分支都没有 —— `w` / `ht` 是函数局部、初值 0，
  落进 `cs-paint-leaf` 后画出一个 0×0 的 TRACK 矩形，等于完全不显示。
  补法是「先补测算、再补绘制」：`cs-measure` 分支给出
  `2*CS-PAD + 12000` × `2*CS-PAD + N*CS-LINE-H + (N-1)*gap`（下限
  `CS-BTN-H`），`cs-paint-skeleton` 再按真实矩形画 TRACK 底 + N 条
  MUTED 占位条 + 相位扫光 FG 带。坑一条：**改完属性必须重新
  `cs-layout`** —— 绘制的矩形是布局阶段算好的，`sk-set-lines` 之后直接
  `cs-paint` 还是旧几何（测试里 `lines=0` 那条断言一开始就是因此挂的）。
  另外叶子横向会被拉到父内容宽，只有高度是 intrinsic，写渲染断言时
  别把宽度当成测算值。
- 阶段 55 的 `C-DESCRIPTION-LIST` 反过来：它**原本在 `cs-is-container` 白名单
  里**，但行是 `DL-LAB` / `DL-VAL` 数据、不是视觉子节点（同 `C-PAGINATION` /
  `C-SELECT` / `C-TAB` 一路），没挂子节点时走通用兜底会画成一个 0×0 的 FIELD
  矩形。补法是**先把它从白名单里拿掉**，再补 `cs-measure` 分支与
  `cs-paint-description-list`；留在白名单里会让「给描述列表挂真子节点」和
  「虚拟行数据」两套语义打架。
- 阶段 43 顺带修掉一条**真 bug**：`cmp-slider(min,max)` 只把 `min` 写进 `CMP-VALUE`（初值），
  没写 `CMP-VALUE3`（下限真源），而 `slider-value-at` / `slider-set` / `cs-paint-leaf` 都读
  `VALUE3` 当下限 —— `min != 0` 时左端点返回 0。既有测试全拿 `min == 0` 构造，所以一直没暴露。
  已补 `CMP-VALUE3[h] = min`，并把渲染层改走对称读取器 `slider-min` / `slider-max`。
- 阶段 44 补的是提示族三个：`C-MESSAGE` / `C-ALERT` / `C-NOTIFICATION`（`LV-INFO` /
  `LV-OK` / `LV-WARN` / `LV-ERR` + `lv-slot`（level -> 主题槽下标，表驱动）+
  `msg-level` / `msg-set-level` + `alert-closable` / `-set-closable` /
  `alert-close-w` / `alert-close-x` / `alert-hit` + `ntf-timeout` / `ntf-remain` /
  `ntf-set-timeout` / `ntf-set-remain` / `ntf-ratio` + `cs-level-color` /
  `cs-level-glyph` + `cs-paint-message` / `-alert` / `-notification`；槽位沿用
  `VALUE`（level）/ `VALUE2`（总时长）/ `VALUE3`（剩余 / closable），**不新增节点种类**）。
  实现里两条坑值得记：`cs-leaf-size` 只收 `(kind, tlen)` **没有句柄**，在里面写
  `cmp-text(h)` 不报错但取不到文本；alert 的关闭钮宽度必须加在最小宽度夹紧**之前**，
  否则短文本会先被夹到 `CS-MIN-LEAF-W` 再长回去，把关闭钮挤出固有宽 —— 因此
  `C-ALERT` 整个不走 `cs-leaf-size`，改在 `cs-measure` 的兜底分支里算。
- 阶段 45 补的是会话族两个：`C-BUBBLE` / `C-SPEECH`（`bb-pad` / `bb-tail`（两个
  几何常量 1200 / 1200）+ `bubble-pos` / `bubble-set-pos`（`CMP-VALUE` 存 position
  `0` 左 / `1` 右，夹紧 `0..1`）+ `speech-lang` / `speech-set-lang`（`CMP-TEXT2` 存
  语言标签，空 = 不画标签行）+ `cs-bubble-tail-x` + `cs-paint-bubble`（底色方块 +
  按 position 对齐的文本 + 底边尾巴方块）+ `cs-paint-speech`（文本行 + 有标签时的
  `CS-TH-BG` 细分隔线 + 标签行）；槽位沿用 `VALUE` / `TEXT` / `TEXT2`，
  **不新增节点种类**）。实现里两条坑值得记：① **option match 的单行写法
  `{ ok -> it -> 0 }` 会被类型检查拒**（`non-exhaustive match on option '?'`），
  必须像 `msg-level` 那样写成多行；② `C-SPEECH` 的宽度取决于句柄上的
  `speech-lang`，不能走只收 `(kind, tlen)` 的 `cs-leaf-size`，只能落在 `cs-measure`
  的兜底分支里（同阶段44 的 `C-ALERT` 理由）。
- 阶段 46 补的是元数据族三个：`C-ATTACHMENT` / `C-CLIPBOARD` / `C-INDEX-PATH`
  （`att-name` / `att-set-name` / `att-size` / `att-set-size`（负数夹 0）/
  `att-size-text`（1024 进位）+ `clip-content` / `clip-set-content` / `clip-len`
  + 扁平段池 `IP-SEG` / `IP-SBASE` / `IP-SN` 与 `ip-count` / `ip-seg` /
  `ip-add-seg` / `ip-clear` / `ip-sep` + `cs-paint-attachment`（左图标方块 +
  文件名 + 右端尺寸文案）/ `cs-paint-clipboard`（字段底 + 内容 + 右端复制方块）/
  `cs-paint-index-path`（各段 + 「 / 」分隔符）；槽位沿用 `TEXT` / `VALUE` +
  扁平段池，**不新增节点种类**）。坑一条：**模块级变量的类型按首次赋值定死** ——
  `clen = clip-content(cl)` 之后再 `clen = clen.len()` 不会变成整数，
  `print('...{clen}...')` 会打出 19 个 NUL 字节（长度值被当成字符串内容）。
- 阶段 48 补的是代码 / 主题两个：`C-HIGHLIGHTER` / `C-THEME`
  （`hl-code` / `hl-set-code` / `hl-lang` / `hl-set-lang` / `hl-tab` /
  `hl-set-tab` + `hl-lines` / `hl-line-start` / `hl-line-len-b` / `hl-line` /
  `hl-expand-len` / `hl-max-len` + `cs-paint-highlighter`（字段底 + 右对齐行号 +
  逐 token 着色 + 右端语言列）/ `th-name` / `th-set-name` / `th-mode` /
  `th-set-mode` / `th-density` / `th-set-density` / `th-accent` /
  `th-set-accent` / `th-swatch` / `th-mode-name` / `th-bg` / `th-fg` /
  `th-muted` / `th-swatch-color` / `th-swatch-label` + `cs-paint-theme`
  （字段底 + 名字 / 模式 + 四块色板 + 标签）；槽位沿用 `TEXT` / `TEXT2` /
  `VALUE` / `VALUE2` / `COLOR`，**不新增节点种类**）。两条都是**复用**而不是
  重造：行几何照抄阶段39 的 `ta-line-start` / `ta-line-len` / `ta-line-text`，
  着色直接调阶段38 的 `ed-lex-line`（关键字 accent / 字符串 OK / 注释 muted /
  数字 `ED-C-NUM`）。坑一条：`slice-bytes` 的第二参是**结束下标**不是长度 ——
  写成 `slice-bytes(b, hl-line-len-b(h, l))` 会多切一行。
- 阶段 47 补的是输入族两个：`C-COLOR-PICKER` / `C-DATE-PICKER`
  （`cp-color` / `cp-red` / `cp-green` / `cp-blue` / `cp-alpha` + 四个
  `cp-set-*`（写任一通道都重算 hue）+ `cp-hue` / `cp-set-hue`（固定
  S=191/255、V=235/255 正算 RGB）+ `cp-hue-of`（RGB 反算 hue）+ `cp-hex`
  + `dp-year` / `dp-month` / `dp-day` + 四个 `dp-set-*` + `dp-set-date`
  （写月 / 年按当月天数重夹日）+ `dp-leap` / `dp-days` / `dp-next-month`
  + `dp-label` + `cs-paint-color-picker`（左色板方块 + 「#RRGGBB」+ hue 数字）
  / `cs-paint-date-picker`（字段底 + 「YYYY-MM-DD」）；槽位沿用 `COLOR` /
  `VALUE` / `VALUE2` / `VALUE3`，**不新增节点种类**）。坑两条：① **`;` 是行注释
  不是语句分隔符** —— `cp-hsv` 的六分支 if-group 一开始写成
  `-> { r = ct-add(c, m); g = ct-add(x, m) }`，闭合 `}` 被注释掉，后面所有定义
  都被解析成它的嵌套块，一次报 130 条 "is not defined"；② **`str[i]` 读出来是
  `char`**，`s.push-str(CP-HEX-DIGITS[i])` 在 codegen 报
  `builtin str-push-bytes: bad source type i32`，要改 `s.push(...)`。
- **仍在通用兜底里画的叶子 kind**：**0 个**（阶段 43 之前是 19 个，`C-EMPTY` /
  `C-RATING` 已补；阶段 44 再补 `C-MESSAGE` / `C-ALERT` / `C-NOTIFICATION`、
  阶段 45 再补 `C-BUBBLE` / `C-SPEECH`、阶段 46 再补 `C-ATTACHMENT` /
  `C-CLIPBOARD` / `C-INDEX-PATH`、阶段 47 再补 `C-COLOR-PICKER` /
  `C-DATE-PICKER`、阶段 48 再补 `C-HIGHLIGHTER` / `C-THEME`、阶段 49 再补
  阶段 50 再补 `C-CHART` / `C-PLOT`、阶段 51 再补 `C-NATIVE-MENU`、
  阶段 52 再补 `C-WINDOW-EXT`、阶段 53 再补 `C-BREADCRUMB`、
  阶段 54 再补 `C-SKELETON`、阶段 55 再补 `C-DESCRIPTION-LIST`、
  阶段 56 再补 `C-ACCORDION`、阶段 57 再补 `C-CAROUSEL`、
  阶段 58 再补 `C-COLLAPSIBLE`、阶段 59 再补 `C-GROUP-BOX`、
  阶段 60 再补 `C-HISTORY`、阶段 61 再补 `C-INSPECTOR`、
  阶段 62 再补 `C-MESSAGE-SCROLLER`、阶段 63 再补 `C-STATUS-BAR`、
  阶段 64 再补 `C-POPOVER`、阶段 65 再补 `C-QUESTIONNAIRE`、
  阶段 66 再补 `C-SEARCHABLE-LIST`、阶段 67 再补 `C-TITLE-BAR`、
  阶段 68 再补 `C-TOOLBAR`、阶段 69 再补 `C-SIDEBAR`、阶段 70 再补 `C-SHEET`）。
  `C-NATIVE-MENU` 的「原生」是自绘的（框架没有平台菜单图元，按钮 / 弹层都用
  rect + text 画，命中走 `cs-menu-hit`）；`C-WINDOW-EXT` 真的改窗口本身
  （标题 / 可缩放 / 最小·最大尺寸 / 全屏 / 置顶 / 边框 / 不透明度 / 尺寸），
  组件层只存状态，推窗口那一步在 `app.no`（`components.no` 看不到 `app.no`）。
