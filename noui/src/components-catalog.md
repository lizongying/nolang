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
- 复杂组件（DataTable / 虚拟列表 / 代码编辑器）只做 API 骨架 + 最小可用状态，功能对等是长期项。

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
| 10 | `empty` | `C-EMPTY` | `cmp-empty(text)` | title, description |
| 11 | `spinner` | `C-SPINNER` | `cmp-spinner()` | speed |
| 12 | `marker` | `C-MARKER` | `cmp-marker(text)` | color |

### B. 表单 / 输入（16）
| # | 模块 | kind | 构造函数 | 能力 |
|---|---|---|---|---|
| 13 | `checkbox` | `C-CHECKBOX` | `cmp-checkbox(text)` | checked, toggle |
| 14 | `radio` | `C-RADIO` | `cmp-radio(text)` | selected, group |
| 15 | `switch` | `C-SWITCH` | `cmp-switch()` | on, toggle |
| 16 | `input` | `C-INPUT` | `cmp-input(placeholder)` | value, focus, caret, text buffer |
| 17 | `input/textarea` | `C-TEXTAREA` | `cmp-textarea(placeholder)` | multiline value |
| 18 | `input/number_input` | `C-NUMBER-INPUT` | `cmp-number-input()` | value, min, max, step |
| 19 | `input/otp_input` | `C-OTP-INPUT` | `cmp-otp-input(len)` | digits, slots |
| 20 | `input/search` | `C-SEARCH` | `cmp-search()` | query, clear |
| 21 | `select` | `C-SELECT` | `cmp-select(placeholder)` | options, selected, open |
| 22 | `combobox` | `C-COMBOBOX` | `cmp-combobox()` | query, options, selected |
| 23 | `slider` | `C-SLIDER` | `cmp-slider(min,max)` | value, step |
| 24 | `stepper` | `C-STEPPER` | `cmp-stepper()` | value, min, max |
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
| 33 | `pagination` | `C-PAGINATION` | `cmp-pagination(total)` | page, total |
| 34 | `toolbar` | `C-TOOLBAR` | `cmp-toolbar()` | items |
| 35 | `title_bar` | `C-TITLE-BAR` | `cmp-title-bar(title)` | title, buttons |
| 36 | `status_bar` | `C-STATUS-BAR` | `cmp-status-bar()` | segments |
| 37 | `command` | `C-COMMAND` | `cmp-command()` | palette, query |
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
| 45 | `notification` | `C-NOTIFICATION` | `cmp-notification(text)` | level, timeout |
| 46 | `alert` | `C-ALERT` | `cmp-alert(text)` | level, closable |
| 47 | `message` | `C-MESSAGE` | `cmp-message(text)` | level |
| 48 | `message_scroller` | `C-MESSAGE-SCROLLER` | `cmp-message-scroller()` | messages, scroll |
| 49 | `sheet` | `C-SHEET` | `cmp-sheet(title)` | open, side, title |
| 50 | `progress` | `C-PROGRESS` | `cmp-progress()` | value, max, indeterminate |
| 51 | `skeleton` | `C-SKELETON` | `cmp-skeleton()` | lines, animated |
| 52 | `shimmer` | `C-SHIMMER` | `cmp-shimmer()` | duration |
| 53 | `bubble` | `C-BUBBLE` | `cmp-bubble(text)` | text, position |
| 54 | `speech` | `C-SPEECH` | `cmp-speech(text)` | text, lang |
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
| 67 | `rating` | `C-RATING` | `cmp-rating(max)` | value, max |
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
- 功能对等长期项：`C-TABLE`（虚拟滚动/排序）、`C-DOCK`（可拆面板）、`C-VIRTUAL-LIST`、`C-INPUT`（富文本/语法高亮）——本轮只做 API + 最小状态。
