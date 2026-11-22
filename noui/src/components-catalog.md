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
  长期项缺口已清零。**仍未补的**：约 37 个 kind 只有构造函数（详见覆盖统计末尾），
  其中 12 个叶子 kind 仍落在通用 `rect + text` 兜底绘制里。

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
| 25 | `color_picker` | `C-COLOR-PICKER` | `cmp-color-picker()` | rgba, hue |
| 26 | `time` (calendar/date_picker/time_field) | `C-DATE-PICKER` | `cmp-date-picker()` | year, month, day |
| 27 | `form` | `C-FORM` | `cmp-form()` | fields container |
| 28 | `questionnaire` | `C-QUESTIONNAIRE` | `cmp-questionnaire()` | questions container |

### C. 导航（12）
| # | 模块 | kind | 构造函数 | 能力 |
|---|---|---|---|---|
| 29 | `tab` | `C-TAB` | `cmp-tab()` | tabs, active |
| 30 | `breadcrumb` | `C-BREADCRUMB` | `cmp-breadcrumb()` | items, separator |
| 31 | `sidebar` | `C-SIDEBAR` | `cmp-sidebar()` | groups, collapsed |
| 32 | `menu` | `C-MENU` | `cmp-menu()` | items, open |
| 33 | `pagination` | `C-PAGINATION` | `cmp-pagination(total)` | page, total（阶段43：`pagination-total` / `-page` / `-clamp` / `-set` / `-prev` / `-next` / `-btn-w` / `-label` / `-hit` + `cs-paint-pagination`；页码按钮是数据不是视觉子节点，已从 `cs-is-container` 白名单拿掉） |
| 34 | `toolbar` | `C-TOOLBAR` | `cmp-toolbar()` | items |
| 35 | `title_bar` | `C-TITLE-BAR` | `cmp-title-bar(title)` | title, buttons |
| 36 | `status_bar` | `C-STATUS-BAR` | `cmp-status-bar()` | segments |
| 37 | `command` | `C-COMMAND` | `cmp-command()` | palette, query（阶段37 补 `cmp-command-*`：输入框 + 滚动体 + 两段式重建可见行，游标真源 `CP-CUR`，命中表抄本 `CP-SRC`/`CP-SB`） |
| 38 | `history` | `C-HISTORY` | `cmp-history()` | entries, index |
| 39 | `link`（导航向）已计 A7 | — | — | — |
| 40 | `sidebar/group` 归入 C31 | — | — | — |

### D. 覆盖层 / 反馈（18）
| # | 模块 | kind | 构造函数 | 能力 |
|---|---|---|---|---|
| 41 | `dialog` | `C-DIALOG` | `cmp-dialog(title)` | open, title, actions |
| 42 | `popover` | `C-POPOVER` | `cmp-popover()` | open, anchor, content |
| 43 | `tooltip` | `C-TOOLTIP` | `cmp-tooltip(text)` | visible, text |
| 44 | `hover_card` | `C-HOVER-CARD` | `cmp-hover-card()` | visible, content |
| 45 | `notification` | `C-NOTIFICATION` | `cmp-notification(text)` | level, timeout（阶段44：`LV-*` + `lv-slot` + `msg-level` / `msg-set-level` + `ntf-timeout` / `ntf-remain` / `ntf-set-timeout` / `ntf-set-remain` / `ntf-ratio` + `cs-paint-notification` 文本一行 + 剩余时间条，**不新增节点种类**） |
| 46 | `alert` | `C-ALERT` | `cmp-alert(text)` | level, closable（阶段44：共用 `msg-level` / `msg-set-level` + `alert-closable` / `-set-closable` / `alert-close-w` / `alert-close-x` / `alert-hit`（`-1` / `0` 主体 / `1` 关闭钮）+ `cs-paint-alert` 左色条 + 字形 + 文本 + 右端 `'x'`，**不新增节点种类**） |
| 47 | `message` | `C-MESSAGE` | `cmp-message(text)` | level（阶段44：共用 `LV-*` + `lv-slot` + `msg-level` / `msg-set-level` + `cs-level-color` / `cs-level-glyph` + `cs-paint-message` 左色条 + 字形 + 文本，**不新增节点种类**） |
| 48 | `message_scroller` | `C-MESSAGE-SCROLLER` | `cmp-message-scroller()` | messages, scroll |
| 49 | `sheet` | `C-SHEET` | `cmp-sheet(title)` | open, side, title |
| 50 | `progress` | `C-PROGRESS` | `cmp-progress()` | value, max, indeterminate |
| 51 | `skeleton` | `C-SKELETON` | `cmp-skeleton()` | lines, animated |
| 52 | `shimmer` | `C-SHIMMER` | `cmp-shimmer()` | duration |
| 53 | `bubble` | `C-BUBBLE` | `cmp-bubble(text)` | text, position（阶段45：`CMP-VALUE` 存 position `0` 左 / `1` 右、`CMP-COLOR` 存底色，`bubble-pos` / `bubble-set-pos`（夹紧 `0..1`）+ `cs-paint-bubble` 底色方块 + 按 position 对齐的文本 + 底边尾巴方块，**不新增节点种类**） |
| 54 | `speech` | `C-SPEECH` | `cmp-speech(text)` | text, lang（阶段45：`CMP-TEXT2` 存语言标签（空 = 不画标签行），`speech-lang` / `speech-set-lang` + `cs-paint-speech` 文本行 + 有标签时的细分隔线 + 标签行，**不新增节点种类**） |
| 55 | `inspector` | `C-INSPECTOR` | `cmp-inspector()` | target, fields |
| 56 | `window_border` | `C-WINDOW-BORDER` | `cmp-window-border()` | radii, width |
| 57 | `native_menu` | `C-NATIVE-MENU` | `cmp-native-menu()` | items |
| 58 | `attachment` | `C-ATTACHMENT` | `cmp-attachment(name)` | name, size |

### E. 数据展示（16）
| # | 模块 | kind | 构造函数 | 能力 |
|---|---|---|---|---|
| 59 | `table` | `C-TABLE` | `cmp-table(cols,rows)` | cells, sort, selection |
| 60 | `list` | `C-LIST` | `cmp-list()` | items, selected |
| 61 | `virtual_list` | `C-VIRTUAL-LIST` | `cmp-virtual-list(len)` | visible range, item height |
| 62 | `tree` | `C-TREE` | `cmp-tree()` | nodes, expanded, selected |
| 63 | `accordion` | `C-ACCORDION` | `cmp-accordion()` | sections, expanded |
| 64 | `carousel` | `C-CAROUSEL` | `cmp-carousel()` | slides, current |
| 65 | `chart` | `C-CHART` | `cmp-chart()` | series, axes |
| 66 | `plot` | `C-PLOT` | `cmp-plot()` | points, scale |
| 67 | `rating` | `C-RATING` | `cmp-rating(max)` | value, max（阶段43：`rating-max` / `-value` / `-set` / `-cell` / `-hit` + `cs-paint-rating`） |
| 68 | `description_list` | `C-DESCRIPTION-LIST` | `cmp-description-list()` | pairs |
| 69 | `group_box` | `C-GROUP-BOX` | `cmp-group-box(title)` | title, children |
| 70 | `collapsible` | `C-COLLAPSIBLE` | `cmp-collapsible(title)` | open, title |
| 71 | `diff` | `C-DIFF` | `cmp-diff()` | old, new, hunks |
| 72 | `highlighter` | `C-HIGHLIGHTER` | `cmp-highlighter(code)` | code, language |
| 73 | `clipboard` | `C-CLIPBOARD` | `cmp-clipboard()` | content |
| 74 | `scroll` | `C-SCROLL` | `cmp-scroll()` | offset, content size |

### F. 布局 / 容器（10）
| # | 模块 | kind | 构造函数 | 能力 |
|---|---|---|---|---|
| 75 | `resizable` | `C-RESIZABLE` | `cmp-resizable()` | panels, sizes, direction |
| 76 | `dock` | `C-DOCK` | `cmp-dock()` | panels, tabs, splits |
| 77 | `grid_layout` | `C-GRID` | `cmp-grid(cols,rows)` | cells, gaps |
| 78 | `form`（容器向）已计 B27 | — | — | — |
| 79 | `text` | `C-TEXT` | `cmp-text-node(text)` | styled text runs |
| 80 | `theme` | `C-THEME` | `cmp-theme()` | palette, size, mode |
| 81 | `sizing` | `C-SIZING` | `cmp-sizing()` | width, height, min/max |
| 82 | `styled` | `C-STYLED` | `cmp-styled()` | base style container |
| 83 | `root` | `C-ROOT` | `cmp-root()` | root container |
| 84 | `window_ext` | `C-WINDOW-EXT` | `cmp-window-ext()` | window options |

### G. 搜索 / 其他（2）
| # | 模块 | kind | 构造函数 | 能力 |
|---|---|---|---|---|
| 85 | `searchable_list` | `C-SEARCHABLE-LIST` | `cmp-searchable-list()` | query, items, filtered |
| 86 | `index_path` | `C-INDEX-PATH` | `cmp-index-path()` | path segments |

## 覆盖统计

- 唯一组件 kind：**约 84 个**（86 模块中 link/sidebar-group/form 各复用/归并 2 项）
- 核心扎实 10：`C-BUTTON` `C-INPUT` `C-CHECKBOX` `C-SWITCH` `C-SELECT` `C-TAB` `C-DIALOG` `C-TOOLTIP` `C-PROGRESS` `C-SLIDER`
- 功能对等长期项：**已全部补齐**（上一条所述的「核心交互组件」）。**注意别把这句读成全目录**：
  下表 86 项里仍有约 37 个 kind 只有一个构造函数、没有任何属性 API；其中 12 个叶子 kind
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
  **不新增节点种类**）。声明式层的超集分别是阶段 28–32。
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
- **仍在通用兜底里画的叶子 kind**（阶段 43 之前是 19 个，`C-EMPTY` / `C-RATING` 已补；
  阶段 44 再补 `C-MESSAGE` / `C-ALERT` / `C-NOTIFICATION`、阶段 45 再补
  `C-BUBBLE` / `C-SPEECH`，剩 12 个）：
  `C-COLOR-PICKER`、`C-DATE-PICKER`、
  `C-NATIVE-MENU`、`C-ATTACHMENT`、`C-CHART`、`C-PLOT`、`C-DIFF`、`C-HIGHLIGHTER`、
  `C-CLIPBOARD`、`C-THEME`、`C-WINDOW-EXT`、`C-INDEX-PATH`。
  其中 `C-COLOR-PICKER` / `C-DATE-PICKER` 这类有明确语义的，是下一阶段的候选。
