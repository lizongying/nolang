# noui 架构实测明细（原 ARCHITECTURE.md §5.3–§5.78）

> 工程化收尾时从 ARCHITECTURE.md 外移：本文件按**原 §5.x 编号**保留阶段
> 9–85 的逐阶段实测坑与决策，旧链接与交叉引用不受影响。地基约束仍在
> [ARCHITECTURE.md](ARCHITECTURE.md) §5.1–§5.2，分组导航见其 §0；
> 阶段 × 组件 × 测试跳转见 [README.md](README.md) 的「目录索引」。

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

### §5.41 阶段 46 命令式元数据族（C-ATTACHMENT / C-CLIPBOARD / C-INDEX-PATH）新坑（2026-11-22 实测）

`src/components-catalog.md` 第 58 / 73 / 86 行分别写着 `C-ATTACHMENT` 的能力是
「name, size」、`C-CLIPBOARD` 是「content」、`C-INDEX-PATH` 是「path segments」，
但命令式侧三者都只有一个构造函数——name / size / content / 段池都没有 API，渲染
还落在 `cs-paint-leaf` 的通用 `rect + text` 兜底里。**目录承诺了但代码没做**。

- **复用而不是新造**：attachment 用 `CMP-TEXT` = 文件名、`CMP-VALUE` = 字节数；
  clipboard 用 `CMP-TEXT` = 内容；index-path 新增扁平段池 `IP-SEG` + 每句柄
  基址 `IP-SBASE` / 段数 `IP-SN`（同 `INP-SG` / `CB-MLIST` 段池思路，随
  `cmp-alloc` 一起长）。几何常量一律抄字面量（内边距 1200、间隙 800、图标 3200、
  复制方块 2400）。**不新增节点种类**：三者都仍是叶子。
- **API**（`src/components.no`）：`att-name` / `att-set-name` / `att-size` /
  `att-set-size`（负数夹 0）/ `att-size-text`（1024 进位）+ `clip-content` /
  `clip-set-content` / `clip-len` + `ip-count` / `ip-seg` / `ip-add-seg` /
  `ip-clear` / `ip-sep`。写操作一律先查 `F-DISABLED`。
- **渲染层**（`src/cmp-scene.no`）：`cs-paint-attachment`（左图标方块 32px +
  文件名 + 右端尺寸文案 muted）、`cs-paint-clipboard`（字段底 `CS-TH-FIELD` +
  内容 + 右端 24px 复制方块）、`cs-paint-index-path`（各段 + 「 / 」分隔符
  muted）；`cs-leaf-size` 给 `C-CLIPBOARD` 加 intrinsic，`cs-measure` 的兜底
  分支给 `C-ATTACHMENT` / `C-INDEX-PATH` 加（前者要读 `att-size-text`、后者要
  读段池——同 `C-ALERT` / `C-SPEECH` 的理由：`cs-leaf-size` 只收
  `(kind, tlen)`，没有句柄）；`cs-paint-leaf` 加三行路由。
- 测试：`tests/cmp-meta.no` 46 项断言全绿（attachment 16 / clipboard 11 /
  index-path 16 + 汇总）；`tests/main-meta.no` 窗口 demo（点附件循环尺寸、点
  剪贴板开关内容、点路径条加段 / 清空，`a` `c` `i` `x` 同效、`q` 退出），900 帧
  `bye frames=900 keys=0 clicks=0 att-size=2048 clip-len=20 ip-segs=3`
  + `meta render OK`、exit 0。
- 坑一条：**模块级变量的类型按首次赋值定死** —— `clen = clip-content(cl)` 之后再
  `clen = clen.len()` 不会变成整数，`print('...{clen}...')` 打出 19 个 NUL 字节
  （长度值被当成字符串内容）。要么换个新名字，要么直接 `clip-len(cl)`。
- 回归：见下方 §7 阶段 46 条目。

### §5.42 阶段 47 命令式输入族（C-COLOR-PICKER / C-DATE-PICKER）新坑（2026-11-22 实测）

`src/components-catalog.md` 第 25 / 26 行分别写着 `C-COLOR-PICKER` 的能力是
「hue, hex」、`C-DATE-PICKER` 是「year, month, day」，但命令式侧两者都只有一个
构造函数——hue / 十六进制 / 年月日都没有 API，渲染还落在 `cs-paint-leaf` 的通用
`rect + text` 兜底里。**目录承诺了但代码没做**，和阶段44 的 `C-ALERT` /
阶段45 的 `C-SPEECH` / 阶段46 的 `C-ATTACHMENT` 是同一类欠账。

- **复用而不是新造**：color-picker 用 `CMP-COLOR` = 打包色（0xRRGGBBAA，和
  `ARCHITECTURE.md` 的 float4 拆包约定一致）、`CMP-VALUE` = hue；date-picker 用
  `CMP-VALUE` / `VALUE2` / `VALUE3` = 年 / 月 / 日。几何常量一律抄字面量
  （色板 3200、内边距 1200、间隙 800）。**不新增节点种类**：两者都仍是叶子。
- **API**（`src/components.no`）：`cp-color` / `cp-red` / `cp-green` / `cp-blue` /
  `cp-alpha` + 四个 `cp-set-*`（写任一通道都按新 RGB 重算 hue）+ `cp-hue` /
  `cp-set-hue`（固定 S=191/255、V=235/255 正算 RGB，hue 夹 0..360）+ `cp-hue-of`
  （RGB 反算 hue）+ `cp-hex` + `dp-year` / `dp-month` / `dp-day` + 四个 `dp-set-*`
  + `dp-set-date`（写月 / 年按当月天数重夹日）+ `dp-leap` / `dp-days` /
  `dp-next-month` + `dp-label`。写操作一律先查 `F-DISABLED`。
- **整数色彩数学的截断方向**：`ct-sub` 的除法向零截断，所以反算 hue 时负差
  （`g - b < 0`）不能写 `60 * q / d` 再补符号，改成 `base - 60*|q|/d`
  （`|q|` 用 `(q < 0) -> { q = 0 - q }` 取）；正算 RGB 用六路互斥 if-group 按
  `seg = hue / 60` 分段，避免任何跨段插值。
- **渲染层**（`src/cmp-scene.no`）：`cs-paint-color-picker`（左色板方块 32px +
  内边距 12px + 间隙 8px + 「#RRGGBB」+ 间隙 + hue 数字 muted）、
  `cs-paint-date-picker`（字段底 `CS-TH-FIELD` + 「YYYY-MM-DD」）；
  `cs-measure` 的兜底分支给两者加 intrinsic（前者 = 内边距*2 + 色板 + 间隙 +
  hex 宽 + 间隙 + hue 数字宽，后者 = 文本宽 + 内边距*2，都夹到
  `CS-MIN-LEAF-W`——同 `C-ALERT` / `C-SPEECH` / `C-ATTACHMENT` 的理由：
  `cs-leaf-size` 只收 `(kind, tlen)`，没有句柄）；`cs-paint-leaf` 加两行路由。
- 测试：`tests/cmp-picker.no` 50 项断言全绿（color-picker 28 / date-picker 22）；
  `tests/main-picker.no` 窗口 demo（点取色器循环 hue、点日期翻月，`o` / `d` /
  `y` 同效、`q` 退出），900 帧
  `bye frames=900 keys=0 clicks=0 hex=#3BD9EB hue=213 date=2026-11-21`
  + `picker render OK`、exit 0。
- 坑两条：① **`;` 是行注释不是语句分隔符** —— `cp-hsv` 的六分支 if-group 一开始
  写成 `-> { r = ct-add(c, m); g = ct-add(x, m) }`，闭合 `}` 被注释掉，后面所有
  定义都被解析成它的嵌套块，一次报 130 条 `<name> is not defined`；
  ② **`str[i]` 读出来是 `char`**，`s.push-str(CP-HEX-DIGITS[i])` 在 codegen 报
  `builtin str-push-bytes: bad source type i32`，要改 `s.push(...)`。
- 回归：见下方 §7 阶段 47 条目。

### §5.43 阶段 48 命令式代码 / 主题（C-HIGHLIGHTER / C-THEME）新坑（2026-11-22 实测）

`src/components-catalog.md` 第 72 / 80 行分别写着 `C-HIGHLIGHTER` 的能力是
「code, language」、`C-THEME` 是「palette, size, mode」，但命令式侧两者都只有一个
构造函数——代码 / 语言 / 缩进、主题名 / 模式 / 密度 / 强调色都没有 API，渲染还
落在 `cs-paint-leaf` 的通用 `rect + text` 兜底里。**目录承诺了但代码没做**，和
阶段44–47 是同一类欠账。

- **复用而不是新造**：highlighter 的行几何照抄阶段39 `ta-line-*` 的「`'\n'` 分行」
  算法，着色直接调阶段38 的 `ed-lex-line`（关键字 accent / 字符串 OK / 注释
  muted / 数字 `ED-C-NUM`），**没有新造 tokenizer**。槽位：highlighter 用
  `CMP-TEXT` = 代码、`TEXT2` = 语言、`VALUE` = 缩进（每级空格数，默认 4）；
  theme 用 `CMP-TEXT` = 名字、`VALUE` = 模式（0 亮 / 1 暗）、`VALUE2` = 密度
  （0/1/2）、`CMP-COLOR` = 强调色。**不新增节点种类**：两者都仍是叶子。
- **API**（`src/components.no`）：`hl-code` / `hl-set-code` / `hl-lang` /
  `hl-set-lang` / `hl-tab` / `hl-set-tab`（夹 0..8）+ `hl-lines` /
  `hl-line-start` / `hl-line-len-b` / `hl-line`（越界行号夹到最后一行）/
  `hl-expand-len`（tab 折算成 `hl-tab` 个空格）/ `hl-max-len`；theme 的
  `th-name` / `th-set-name` / `th-mode` / `th-set-mode`（夹 0..1）/
  `th-density` / `th-set-density`（夹 0..2）/ `th-accent` / `th-set-accent` /
  `th-swatch`（按密度 3600 / 4800 / 6000）/ `th-mode-name` / `th-bg` / `th-fg` /
  `th-muted` / `th-swatch-color` / `th-swatch-label`。写操作一律先查
  `F-DISABLED`。
- **渲染层**（`src/cmp-scene.no`）：`cs-paint-highlighter`（字段底
  `CS-TH-FIELD` + 右对齐行号槽 + 逐 token 着色 + 右端语言列）、
  `cs-paint-theme`（字段底 + 名字 / 右对齐模式 + 四块色板 + 标签）；
  `cs-measure` 的兜底分支给两者加 intrinsic（前者 = 左右内边距 + 行号槽 +
  最长行（tab 折算）+ 可选语言列，高 = 上下内边距 + 行数 × 行高；后者 = 左右
  内边距 + 4 块色板 + 3 个间隙，高 = 上下内边距 + 标题行 + 间隙 + 色板 + 间隙
  + 标签行，都夹到 `CS-MIN-LEAF-W`——同 `C-ALERT` / `C-SPEECH` / `C-ATTACHMENT`
  的理由：`cs-leaf-size` 只收 `(kind, tlen)`，没有句柄）；`cs-paint-leaf` 加两行
  路由。
- 测试：`tests/cmp-code.no` 72 项断言全绿（highlighter 32 / theme 40）；
  `tests/main-code.no` 窗口 demo（点代码块循环缩进、点主题卡切亮暗，`c` / `m` /
  `d` 同效、`q` 退出），900 帧
  `bye frames=900 keys=0 clicks=0 lines=4 max=23 tab=4 theme="midnight"
  mode=light den=1` + `code render OK`、exit 0。
- 坑两条：① **`slice-bytes(start, end)` 的第二个参数是结束下标、不是长度**——
  `hl-line` 一开始写成 `slice-bytes(b, hl-line-len-b(h, l))`，会把行多切掉一截；
  按 `ta-line-text` 的用法改成 `slice-bytes(b, ct-add(b, hl-line-len-b(h, l)))`
  才对；② **插值里不能放函数调用**——探针里 `print('kind={cmp-kind(HC)}')` 直接
  报 `unsupported construct in MIR backend: interp: unresolved format field
  <call>`，必须先赋值给临时变量再插值。
- 回归：见下方 §7 阶段 48 条目。

### §5.44 阶段 49 命令式差异对照（C-DIFF）新坑（2026-11-22 实测）

`src/components-catalog.md` 第 71 行写着 `C-DIFF` 的能力是「old, new, hunks」，
但命令式侧只有一个 `cmp-diff()` 构造函数——old / new 都没有 API，hunks 无从读出，
渲染还落在 `cs-paint-leaf` 的通用 `rect + text` 兜底里。**目录承诺了但代码没做**，
和阶段44–48 是同一类欠账。

- **是真的 diff，不是摆样子**：`df-split` 按 `'\n'` 切行（复用阶段39 的 `'\n'`
  算法）→ `df-lcs` 动态规划求最长公共子序列（`dp[(n+1)*(m+1)]`）→
  `df-backtrack` 从 `(n, m)` 回溯成行（0 上下文 / 1 新增 / 2 删除），同时累计
  旧 / 新行号与 hunk 起点。结果缓存在模块级 `DF-CACHE-*`，**句柄 + old + new
  三者一致才复用**，否则重算。槽位沿用 `CMP-TEXT` = old、`CMP-TEXT2` = new。
  **不新增节点种类**：`C-DIFF` 仍是叶子。
- **API**（`src/components.no`）：`df-old` / `df-set-old` / `df-new` /
  `df-set-new`（写操作先查 `F-DISABLED`）+ 行级 `df-rows` / `df-row-op` /
  `df-row-text` / `df-row-old-no` / `df-row-new-no` + 统计 `df-hunks` /
  `df-hunk-start` / `df-count-add` / `df-count-del` / `df-max-len` /
  `df-summary`（`'+a -d'`）。
- **渲染层**（`src/cmp-scene.no`）：`cs-paint-diff`（字段底 `CS-TH-FIELD` +
  双列行号槽（旧号右对齐到 `x+pad+gutter-char-w`、新号右对齐到
  `x+pad+gutter`）+ 行级 glyph（上下文空格 / `'+'` / `'-'`）+ 文本，颜色按行
  级别：上下文 `CS-TH-FG` / 新增 `CS-TH-OK` / 删除 `CS-TH-DANGER`）；
  `cs-measure` 的兜底分支给 intrinsic（宽 = 左右内边距 + 行号槽 + 最长行 × 字宽，
  夹到 `CS-MIN-LEAF-W`；高 = 上下内边距 + 行数 × 行高——同 `C-HIGHLIGHTER` 的
  理由：那里才有句柄 `h`，才读得到 `df-rows` / `df-max-len`）；`cs-paint-leaf`
  加一行路由。
- 测试：`tests/cmp-diff.no` 67 项断言全绿（构造默认 12 / 典型替换 18 / 纯新增 ·
  纯删除 · 全等 · 双 hunk · 空 old 19 / disabled 4 / 测算 3 / 渲染 11）；
  `tests/main-diff.no` 窗口 demo（点 diff 块循环三组样例，`0` / `1` / `2` 选样例、
  `r` 新旧互换、`q` 退出），900 帧
  `bye frames=900 keys=0 clicks=0 rows=4 hunks=1 +1 -1` + `diff render OK`、
  exit 0。
- 坑三条：① **`CMP-TEXT[h]` / `CMP-TEXT2[h]` 返回 `?str`**，不能直接赋给 `str`
  变量（`cannot assign ?str value to str variable 'DF-CACHE-OLD'`），要用
  `cmp-text(h)` / `cmp-text2(h)`；② **逗号不是语句分隔符**——
  `df-build(h), n = DF-CACHE-ROWS` 能编译但 `df-build` 根本没跑（`df-rows` 恒为
  0），必须拆成两行（同阶段39 的 `ed-lex-line` 老坑）；③ **局部 `str` 在 `!!` 里
  被 guard 反复改写后 `push` 进 `[]str` 会 SIGTRAP**（`out.push(cur)` 直接
  rc=133）——改成不累积字符串、直接 `s.slice-bytes(start, end)` 取切片 push 就
  正常；同类的还有 `dp[idx] = v` 直写局部数组，要用 `ct-set`。
- 回归：见下方 §7 阶段 49 条目。

### §5.45 阶段 50 命令式图表 / 绘图（C-CHART / C-PLOT）新坑（2026-11-22 实测）

`src/components-catalog.md` 第 65 / 66 行写着 `C-CHART` 的能力是「series,
axes」、`C-PLOT` 是「points, scale」，但命令式侧只有一个 `cmp-chart()` /
`cmp-plot()` 构造函数——序列数 / 点数 / 序列名 / 值轴上限 / 数值 / 量程 /
点坐标全都没有 API，渲染还落在 `cs-paint-leaf` 的通用 `rect + text` 兜底里。
**目录承诺了但代码没做**，和阶段44–49 是同一类欠账。

- **每句柄定步长的数据池**：chart 用 `CH-VAL`（步长 `CH-STRIDE =
  CH-SERIES-MAX × CH-PTS-MAX = 3 × 8 = 24`）、`CH-NAME`（步长 3）、
  `CH-SN` / `CH-PN`（各一格），`ch-ensure(h)` 按需补格；plot 用 `PL-X` /
  `PL-Y`（每句柄 `PL-PTS-MAX = 16` 格）+ `PL-CN` / `PL-SC`，`pl-ensure(h)`
  补格。**不新增节点种类**：两者都仍是叶子。
- **API**（`src/components.no`）：`ch-series` / `ch-set-series`（夹 0..3）/
  `ch-points` / `ch-set-points`（夹 0..8）/ `ch-name` / `ch-set-name` /
  `ch-max` / `ch-set-max`（`CMP-VALUE2`）/ `ch-value` / `ch-set-value`（夹
  0..max）/ `ch-total` / `ch-peak`；`pl-count` / `pl-set-count`（夹 0..16）/
  `pl-scale` / `pl-set-scale`（≥1）/ `pl-x` / `pl-y` / `pl-set-x` / `pl-set-y`
  （夹 ±scale）/ `pl-min-x` / `pl-max-x` / `pl-min-y` / `pl-max-y`。写操作一律
  先查 `F-DISABLED`，再 `ch-ensure` / `pl-ensure`。
- **渲染层**（`src/cmp-scene.no`）：`cs-paint-chart`（字段底 + 左轴 / 下轴 +
  值轴上限文案 + 分组柱（序列序映射 ACCENT / OK / DANGER）+ x 轴刻度 +
  右对齐图例）与 `cs-paint-plot`（字段底 + 过原点两轴 + 上 `'+scale'` /
  下 `'-scale'` 文案 + 方块数据点 + 相邻点 L 形连线）；`cs-measure` 的兜底
  分支给 intrinsic（宽 = 左右内边距 + 点数 × 点槽宽，夹 `CS-MIN-LEAF-W`；
  chart 高 = 上下内边距 + 标题行 + 绘图区 + x 轴标签行，plot 高 = 上下内边距
  + 标题行 + 绘图区——同 `C-HIGHLIGHTER` / `C-DIFF` 的理由：那里才有句柄
  `h`）；`cs-paint-leaf` 加两行路由。**没有线 / 圆图元**，所以数据点画成方块、
  连线用细矩形拼（先水平后垂直的 L 形）；`cs-neg-str` 是组件模块不能
  import 渲染层，抄的一份最小负数文案实现。
- 测试：`tests/cmp-chart.no` 89 项断言全绿（chart 默认 6 / 写入 + 夹紧 18 /
  越界拒绝 3 / disabled 5 / plot 默认 6 / 写入 + 夹紧 14 / 越界 + disabled 5 /
  测算 6 / 渲染 chart 13 / 渲染 plot 13）；`tests/main-chart.no` 窗口 demo
  （点图表循环值轴上限、点绘图循环点数，`c` / `r` / `-` / `+` 同效、`q` 退出），
  900 帧 `bye frames=900 keys=0 clicks=0 series=3 points=6 max=100 peak=99
  plot-n=5 scale=100` + `chart render OK`、exit 0。
- 坑三条：① **新建句柄上第一次写会静默 no-op**——setter 只 `ct-set` 到池里，
  而池是 `ch-ensure` / `pl-ensure` 按需长的，新句柄的池还是空的，写进去读回来
  仍是 0（探针里 `pl-set-count(P, 5)` 后 `pl-count` 还是 0，第二次写才生效）。
  已在每个 setter 里先 `ch-ensure(h)` / `pl-ensure(h)`；② **`ch-max` 默认 1 的
  陷阱**——`CMP-VALUE2` 构造给的是 0，而 `ch-set-value` 要按 `ch-max` 夹紧，
  若不把 `<= 0` 映射成默认 100，所有值都被夹到 1；③ **`cs-layout` 不拉伸叶子
  高度**——挂进根以后叶子拿的是 intrinsic 高（chart 39200、plot 36800），不是
  根内容高 47600，断言里别按内容高算；④ **顶层块里的变量名是全程序共享的**：
  `tests/main-table.no` 在顶层 `!!` 里写 `nm = NAMES[i]`（`NAMES` 是 `[]str`，
  所以那个 `nm` 是 `?str`），于是 `src/cmp-scene.no` 里同名的局部 `nm` 也被
  vet 判成 `?str`，`nm.len()` 直接报 `nolang-option`（换名 `leg` 即消）。
  顶层 `!!` / guard 里赋的值都是模块级变量，取名字要避开这种全局占用。
- 回归：见下方 §7 阶段 50 条目。

### §5.46 阶段 51 命令式下拉菜单（C-NATIVE-MENU）新坑（2026-11-22 实测）

`src/components-catalog.md` 第 57 行写着 `C-NATIVE-MENU` 的能力是「items」，
但命令式侧只有一个 `cmp-native-menu()` 构造函数——项数 / 项文案 / 选中项 /
禁用项全都没有 API，渲染还落在 `cs-paint-leaf` 的通用 `rect + text` 兜底里。
**目录承诺了但代码没做**，和阶段44–50 是同一类欠账。

- **「原生」是自绘的**：框架里没有平台菜单图元，所以菜单按钮 / 弹层都用
  rect + text 画（同 `C-CHART` / `C-PLOT` 的做法）。**不新增节点种类**：它仍是
  叶子，不走 `cs-layout` 的子节点特殊分支（这点和 `C-SELECT` / `C-COMBOBOX`
  不同——那两个的选项是子节点，布局时要单独摆）。
- **槽位**：`CMP-TEXT` = 按钮文案、`CMP-VALUE` = 项数、`CMP-VALUE2` = 选中项 + 1
  （`0` = 未选）、`CMP-COLOR` = 强调色；展开态走 `F-OPEN`。
- **每句柄定步长的数据池**（`src/components.no`）：`NM-LABEL`（项文案）/
  `NM-SEL`（项禁用标记）按句柄 × `NM-MAX = 8` 定步长，`NM-CN`（项数）每句柄
  一格，`nm-ensure(h)` 按需补格。
- **API**：`nm-count` / `nm-set-count`（夹 0..8，同步写 `CMP-VALUE`，缩小时把
  越界的选中项清成未选）/ `nm-label` / `nm-set-label` / `nm-index` /
  `nm-set-index`（夹 −1..count−1；空菜单选不中）/ `nm-item-off` /
  `nm-set-item-off` / `nm-open` / `nm-close` / `nm-toggle`。写操作一律先查
  `F-DISABLED`，再 `nm-ensure`。
- **渲染层**（`src/cmp-scene.no`）：`cs-measure` 兜底分支加 `C-NATIVE-MENU`
  ——宽 = `2*CS-PAD + max(按钮文案宽, 各项文案宽) + 2400`（箭头槽），夹到
  `CS-MIN-LEAF-W`；高 = `CS-BTN-H`，展开时再加 `CS-GAP + count*CS-LINE-H`。
  `cs-paint-menu` 画字段底按钮 + 聚焦四边 `ACCENT` 描边 + 左对齐文案 + 右端
  `'v'` / `'^'` 箭头，展开时下方画面板（项高 `CS-LINE-H`，选中项铺 `ACCENT`、
  禁用项铺 `TRACK` 且文案 `MUTED`）；`cs-paint-leaf` 加一行路由。命中测试
  `cs-menu-hit(h,px,py)` 返回弹层命中项，未展开 / 越界 / 禁用项返回 −1
  （组件模块拿不到 `cs-rect`，所以命中放渲染层，同 `cs-dock-*` 的分工）。
- 测试：`tests/cmp-menu.no` 69 项断言全绿（默认 8 / 写入 + 夹紧 14 / disabled 5 /
  选中语义 + 开关 9 / 单项禁用 4 / 测算 6 / 渲染收起 3 / 渲染展开 9 / 命中 10）；
  `tests/main-menu.no` 窗口 demo（两个菜单，点击开 / 关 / 选中 / 收起，
  `m` / `o` / `n` / `p` / `d` / `x` 同效、`q` 退出），900 帧
  `bye frames=900 keys=0 clicks=0 m1=n4 sel-1 label= m2=n3` + `menu render OK`、
  exit 0。
- 坑三条：① **`ct-get` 返回的是 `i64` 不是 `?i64`**——最初给 `nm-count` /
  `nm-raw-sel` / `nm-item-off` 套了 `x = ct-get(...): { ok -> it ; -> 0 }`，
  match 永远走 `-> 0` 兜底臂，`nm-count` 恒返回 0（`ct-get` 自己已经把越界映射
  成 0 了）。改成直接 `v = ct-get(...)` 即消；② **`->` 链不互斥**——`nm-toggle`
  一开始写成 `(cmp-has(h, F-OPEN)) -> { nm-close(h) }` 紧跟
  `(!cmp-has(h, F-OPEN)) -> { nm-open(h) }`，第一条把 `F-OPEN` 清掉后第二条的
  条件跟着变真，两次 toggle 之后还是开着的。先取到局部 `was` 再分支；
  ③ **`cs-emit-text-left` 的 y 会按行高垂直居中**——`ty = y + (ht -
  CS-LINE-H)/2`，按钮文案（`ht = CS-BTN-H = 4000`）落在 `y + 800` 不是 `y`；
  断言按 `y` 找一个都找不到（项行 `ht = CS-LINE-H`，居中后正好等于 `y`）。
- 回归：见下方 §7 阶段 51 条目。

### §5.47 阶段 52 命令式窗口选项（C-WINDOW-EXT）新坑（2026-11-22 实测）

`src/components-catalog.md` 第 84 行写着 `C-WINDOW-EXT` 的能力是「window
options」，是命令式侧最后一个只有构造函数的叶子——阶段 43–51 把通用兜底里的
叶子 kind 一个一个补完，这是最后一个，也是唯一**真的改窗口本身**的一个。

- **层依赖是这个阶段的真难点**：`components.no` **不能** `import app.no`——
  `app.no` import `cmp-scene`，`cmp-scene` 又 import `components`，再让
  `components` import `app.no` 就成环。所以拆成两半：组件层只留**纯状态**
  （`we-*` 池），推窗口那一步整个放 `app.no`（同 `cs-dock-*` 的分工思路）。
- **槽位**：`CMP-TEXT` = 标题、`CMP-VALUE` = flags（`WE-F-RES = 1` 可缩放 /
  `WE-F-FS = 2` 全屏 / `WE-F-TOP = 4` 置顶 / `WE-F-BRD = 8` 边框）、
  `CMP-VALUE2` = 总开关（`1` = 接管真窗口，`0` = 只存状态）、`CMP-COLOR` =
  强调色。
- **每句柄定步长的数据池**（`src/components.no`）：`WE-TITLE` / `WE-FLAGS` /
  `WE-OP` 每句柄一格、`WE-MIN` / `WE-MAX` / `WE-SIZE` 每句柄两格，
  `we-ensure(h)` 按需补格（新格默认 flags = `WE-F-RES | WE-F-BRD`、不透明度
  100、其余 0）。
- **API**：`we-title` / `we-set-title`（同步 `TEXT`）/ `we-flags` /
  `we-set-flags`（同步 `VALUE`）/ `we-resizable` / `we-set-resizable` /
  `we-fullscreen` / `we-set-fullscreen` / `we-always-on-top` /
  `we-set-always-on-top` / `we-bordered` / `we-set-bordered` / `we-opacity` /
  `we-set-opacity`（夹 0..100）/ `we-min-x` / `we-min-y` / `we-set-min-size` /
  `we-max-x` / `we-max-y` / `we-set-max-size` / `we-w` / `we-h` /
  `we-set-size`（夹 ≥0，`0` = 不覆盖）/ `we-on` / `we-set-on`。写操作一律先查
  `F-DISABLED`。
- **装配层**（`src/app.no`）：12 个 `#{c}` FFI + `app-window-apply(h)`
  （`h < 0` / 非 `C-WINDOW-EXT` / `we-on(h) == 0` 三种静默跳过）+
  `app-window-apply-all()`（遍历 `cmp-count()` 个句柄挑 `C-WINDOW-EXT`）+
  读回 `app-window-title()` / `app-window-size()`（`get_size` 返回
  `(w << 32) | h` 打包值）/ `app-window-flags()`，加 `APP-WF-*` 四个 SDL
  标志位常量。每帧 `app-frame()` 开头调 `app-window-apply-all()`。
  **注意 `app-window-apply-all` 是 O(句柄数) 每帧**，目前句柄数在几十到几百
  量级，可接受；真要优化就维护一个「已挂载的 window-ext 句柄列表」。
- **shim**（`src/shim/shim.h` / `shim.c`）：新增 `noui_window_set_title` /
  `set_resizable` / `set_min_size` / `set_max_size` / `set_fullscreen` /
  `set_always_on_top` / `set_bordered` / `set_opacity` / `set_size` /
  `get_size` / `get_flags` / `get_title`，全部 `if (g_window ...)`
  保护。**SDL3 删了 `SDL_TRUE` / `SDL_FALSE`**，一律写 `true` / `false`。
- **渲染层**（`src/cmp-scene.no`）：`cs-measure` 加分支——宽 =
  `2*ch-pad() + 16000` = 18400，高 = `2*ch-pad() + ch-title-h() +
  WE-ROWS*CS-LINE-H` = 2400 + 2400 + 19200 = **24000**。
  `cs-paint-window-ext` 画字段底 + 标题（`ch-pad()` 处 `FG`）+ 标题下 `TRACK`
  分隔线 + 8 行状态（`WE-ROWS = 8`：on / title / resizable / fullscreen /
  always on top / bordered / opacity / size），标签列宽 12000 `MUTED` 左对齐、
  数值列宽 2400 `FG` 右对齐（`cs-emit-text-align(..., A-RIGHT)`），`size` 行
  在宽或高为 0 时显示 `auto`；聚焦时四边 `ACCENT` 描边。`cs-paint-leaf` 加
  一行路由。**不新增节点种类**。
- 测试：`tests/cmp-window-ext.no` 90 项断言全绿（默认 17 / 写入 + 夹紧 32 /
  disabled 7 / 测算 8 / 渲染 26）；`tests/main-window-ext.no` 窗口 demo
  （`t` / `r` / `f` / `a` / `b` / `o` / `=` / `-` / `m` 逐项改真窗口，
  真窗口读回与面板状态双显示，`q` 退出），900 帧
  `bye frames=900 keys=0 applied=900 live=final check 800x600 res=true` +
  `win-ext render OK` + `win-ext live OK`、exit 0。
- 坑三条：① **`we-title` 是 `[]str`，不能用 `ct-get`**——`ct-get` 是 `[]i64`
  专用的，`v = ct-get(WE-TITLE, h)` 直接报 `cannot assign i64 value to str
  variable 'v'`；`[]str` 走 `ed-arr-get-s` / `ed-arr-set-s`。② **位运算
  helper 原本不存在**——`ct-or` / `ct-and` / `ct-not` 都得新加，「清掉某一位」
  写 `ct-and(f, ct-not(bit))`（`ct-not` 用 `ct-sub(-1, a)` 实现按位取反）。
  ③ **`ch-title-h()` 是 2400 不是 3200**（`ch-plot-h()` 才是 32000）——按 3200
  算 intrinsic 高得到 24800，渲染断言整段漂移 800，面板实际 24000 高；
  另外 `cs-text-w` 是 `len * CS-CHAR-W`，`'640x480'` 是 **7** 个字不是 6，
  右对齐落点 12800 不是 13600。
- 回归：见下方 §7 阶段 52 条目。

### §5.48 阶段 53 命令式面包屑（C-BREADCRUMB）新坑（2026-11-22 实测）

- 立项：`src/components-catalog.md` 第 30 行的 `C-BREADCRUMB`「items, separator」
  在命令式侧只有一个 `cmp-breadcrumb()` 构造函数，渲染落在通用
  `rect + text` 兜底里。它是阶段 46 `C-INDEX-PATH` 的孪生，照抄
  「扁平条目池 + 每句柄基址 / 计数」结构，换配色语义。
- 组件层（`src/components.no`）新 `bc-*` 段：数据池 `BC-ITEM`（`[]str` 扁平只增）
  + `BC-SBASE` / `BC-N` / `BC-SEP` 每句柄一格，全部随 `cmp-alloc` 一起长
  （`BC-SEP` 新格直接填默认 `' / '`，省掉「空串 = 未设置」的二义）。公开
  `bc-count` / `bc-item`（越界返回空串）/ `bc-add-item`（空池时记一次基址）/
  `bc-clear`（基址与计数归零，扁平池只增不复用）/ `bc-sep` / `bc-set-sep` /
  `bc-last`（末项下标，空 = −1）/ `bc-join`；写操作先查 `F-DISABLED`。
  **不新增节点种类**。
- 渲染层（`src/cmp-scene.no`）：`cs-measure` 分支宽 =
  `2*CS-PAD + Σ条目宽 + (n-1)*分隔符宽`（下限 `CS-MIN-LEAF-W`）、高 = `CS-BTN-H`；
  `cs-paint-breadcrumb` 从 `x + CS-PAD` 起逐段画、垂直居中
  （`y + (ht - CS-LINE-H)/2`），配色分两档 —— 除最后一项外画 `MUTED`（可回退的
  祖先）、最后一项画 `FG`（当前页）、段间分隔符画 `TRACK`；`cs-paint-leaf`
  加一行路由。
- 验证：`tests/cmp-breadcrumb.no` 60 项断言全绿（默认 7 / 条目池 16 / 多句柄隔离 9 /
  分隔符 4 / disabled 3 / 测算 10 / 渲染 11）；`tests/main-breadcrumb.no` 窗口
  demo（`a` 追加 / `c` 清空 / `b` 重建 / `s` 换分隔符 / `d` 切换 disabled，
  收尾按 `cs-rect(bc)` 反推基址逐段核对坐标与配色），900 帧
  `bye frames=900 keys=0 clicks=0 path=home / docs / noui w=53600` +
  `breadcrumb render OK` + `scene OK`、exit 0。
- 坑两条：① **`->` 链不是互斥的** —— `bc-last` 一开始写成「先 `i = count - 1`，
  再 `(i <= 0) -> { i = -1 }`」，第一条分支把 `i` 改成 0 之后第二条立刻又命中，
  末项下标恒为 −1（`count == 3` 时也返回 −1）；必须先把 `n = bc-count(h)` 存进
  局部，再从 −1 起只让一条分支写。② **`[]str` 不能走 `ct-get` / `ct-set`** ——
  `BC-ITEM` / `BC-SEP` 的读写都得用 `ed-arr-get-s` / `ed-arr-set-s`，否则报
  `cannot assign i64 value to str variable`。
- 回归：见下方 §7 阶段 53 条目。

### §5.49 阶段 54 命令式骨架屏（C-SKELETON）新坑（2026-11-22 实测）

- `skeleton` 是叶子（不在 `cs-is-container` 白名单里），补之前**连 `cs-measure`
  分支都没有**：`w` / `ht` 是函数局部、初值 0，落进 `cs-paint-leaf` 后画出一个
  0×0 的 TRACK 矩形，等于完全不显示。补法是「先补测算、再补绘制」：组件层
  `CMP-VALUE` = 扫光相位 0..100 / `CMP-VALUE2` = 行数（夹 0..8）/
  `CMP-VALUE3` = 行距（下限 800），公开 `sk-lines` / `sk-set-lines` / `sk-gap` /
  `sk-set-gap` / `sk-phase` / `sk-set-phase`；`cs-measure` 给
  `2*CS-PAD + 12000` × `2*CS-PAD + N*CS-LINE-H + (N-1)*gap`（下限 `CS-BTN-H`）；
  `cs-paint-skeleton` 画 TRACK 底 + N 条 MUTED 占位条 + 相位扫光 FG 带
  （带宽 = 轨道宽 / 3，位置 = 轨道左缘 + 相位 * (轨道宽 − 带宽) / 100）。
  `tests/cmp-skeleton.no` 43 项断言全绿、`tests/main-skeleton.no` 900 帧
  `bye frames=900 keys=0 clicks=0 lines=4 gap=800 phase=50 w=53600 h=14400` +
  `skeleton render OK` + `scene OK`。
- 坑三条：① **改完属性必须重新 `cs-layout`** —— `cs-paint` 读的是布局阶段算好的
  `cs-rect`，`sk-set-lines(sk, 0)` 之后直接 `cs-paint` 还是旧几何，「行数 0 只剩
  底色」那条断言一开始就是因此挂的。② **叶子横向会被拉到父内容宽，只有高度是
  intrinsic** —— 测算值 14400，实际画出来 18400（父内容宽），渲染断言整段按
  14400 写会全部漂移，dump `cs-rect` 才看出来（阶段 53 的面包屑也是这条）。
  ③ **相位落点别用裸算术** —— `50 * ((bw - band) / 100)` 这类整数除法会截断，
  一律走 `cs-mul` / `cs-div`，断言里也写成同样的表达式而不是硬编码截断后的数。
- 回归：见下方 §7 阶段 54 条目。

### §5.50 阶段 55 命令式描述列表（C-DESCRIPTION-LIST）新坑（2026-11-23 实测）

- 源码：`src/components.no` 新增 `DL-LAB` / `DL-VAL`（`[]str`，扁平只增、同下标）
  + 每句柄 `DL-SBASE` / `DL-N`（随 `cmp-alloc` 长）+ `dl-pad` / `dl-lab-w` /
  `dl-gap` / `dl-val-min` / `dl-count` / `dl-label` / `dl-value` / `dl-add` /
  `dl-set-label` / `dl-set-value` / `dl-clear`（写操作先查 `F-DISABLED`）
- 渲染：`src/cmp-scene.no` 的 `cs-measure` 新增 `C-DESCRIPTION-LIST` 分支
  （宽 = `2*CS-PAD + 12000 + 800 + 值列`，值列取最长值、下限 `CS-MIN-LEAF-W`；
  高 = `2*CS-PAD + N*CS-LINE-H + (N-1)*CS-SEP-H`，下限 `CS-BTN-H`）+
  `cs-paint-description-list`（FIELD 底 + 左标签 MUTED / 右值 FG 右对齐 +
  行间 TRACK 分隔线）+ `cs-paint-leaf` 一行路由
- 验收：`tests/cmp-description-list.no` 64 项断言全绿；
  `tests/main-description-list.no` 窗口 demo 900 帧（`bye frames=900 keys=0
  clicks=0 rows=3 w=53600 h=9800` + `description-list render OK` + `scene OK`）
- 回归（2026-11-23 实测）：非 main 63 个文件 → 62 通过 / 1 失败
  （`tests/dva-editor.no` rc=133，既有）；main 27/27；`tests/logic` ALL TESTS
  PASSED；`no vet` 17 错全在未触碰文件；`dist/` 仅四个 shim 产物
- 坑两条：① **行基址必须「先读后补种」** —— `dl-add` 先
  `b = ct-get(DL-SBASE, h)`，再 `(n == 0) -> { b = DL-LAB.len() }` 把空句柄的
  基址补种到池尾，最后才 `ct-set(DL-SBASE, h, b)`。漏掉补种这一条，`b` 就是
  上一个句柄留下的基址，新行会写进别的句柄的行区间（`dl-count` 看着对、
  `dl-label` 读出别的句柄的文本）；`dl-clear` 只把 `DL-SBASE` 归零、不清池，
  所以清空后再加必须重新补种。
  ② **`[]str` 行池不能走 `ct-get` / `ct-set`** —— `DL-LAB` / `DL-VAL` 必须用
  `ed-arr-get-s` / `ed-arr-set-s`；基址与行数是 `[]i64`，才走 `ct-get` /
  `ct-set`。两类池混用会直接报 `cannot assign i64 value to str variable`。
- 设计选择：阶段 55 是**白名单减法**而不是加法 —— `C-DESCRIPTION-LIST` 原本
  在 `cs-is-container` 白名单里，但它的行是 `DL-LAB` / `DL-VAL` **数据**、不是
  视觉子节点（同阶段 43 的 `C-PAGINATION` 一路），没挂子节点时走通用兜底会
  画成一个 0×0 的 FIELD 矩形。留在白名单里会让「给描述列表挂真子节点」和
  「虚拟行数据」两套语义打架，所以先拿掉、再补 measure / paint。

### §5.51 阶段 56 命令式手风琴（C-ACCORDION）新坑（2026-11-23 实测）

- 源码：`src/components.no` 新增 `AC-TITLE` / `AC-BODY`（`[]str`，扁平只增）+
  `AC-EXP` / `AC-BBASE` / `AC-BN`（`[]i64`，与 `AC-TITLE` 同下标 / 同组号）+
  每句柄 `AC-SBASE` / `AC-N`（随 `cmp-alloc` 长）+ `ac-pad` / `ac-head-h` /
  `ac-glyph-w` / `ac-gap` / `ac-count` / `ac-title` / `ac-body-count` /
  `ac-body-line` / `ac-expanded` / `ac-open-count` / `ac-add` / `ac-add-open` /
  `ac-add-body` / `ac-set-title` / `ac-set-expanded` / `ac-expand` /
  `ac-collapse` / `ac-toggle` / `ac-clear`（写操作先查 `F-DISABLED`）
- 渲染：`src/cmp-scene.no` 把 `C-ACCORDION` 从 `cs-is-container` 白名单里拿掉，
  `cs-measure` 新增分支（宽 = `2*CS-PAD + 800 + 800 + 标题列`，标题列取最长
  「组标题 / 展开组正文行」、下限 `CS-MIN-LEAF-W`；高 = `2*CS-PAD + Σ组头 4000
  + 展开组正文行*CS-LINE-H + 组间分隔线 CS-SEP-H`，下限 `CS-BTN-H`）+
  `cs-paint-accordion`（FIELD 底 + 展开符 `'-'` / `'>'` ACCENT + 标题 FG +
  展开组正文 MUTED + 组间 TRACK 分隔线）+ `cs-paint-leaf` 一行路由
- 验收：`tests/cmp-accordion.no` 74 项断言全绿；`tests/main-accordion.no`
  窗口 demo 900 帧（`bye frames=900 keys=0 clicks=0 sections=3 open=1
  w=53600 h=17000` + `accordion render OK` + `scene OK`）
- 回归：见下方 §7 阶段 56 条目
- 坑三条：① **演示里的辅助函数必须放模块级** —— `ac-build-default` 写在
  `(ok == 0) -> { ... }` 块里会编译期报 `'ac-build-default' is not defined`
  （nolang 不允许块内定义函数），提到模块级并改成 `(h i64)` 入参后通过；
  ② **构建失败时旧二进制会骗人** —— 5 错构建失败后 `dist/_probe` 仍是上一轮
  逻辑测试的产物，运行照样 `rc=0` + `ALL ACCORDION TESTS PASSED`；判断 demo
  是否真过必须先看 `Build successful`；③ **正文池扁平跨组连续** —— `AC-BODY`
  是所有组共享的一条池，`AC-BBASE[i]` / `AC-BN[i]` 记录第 i 组的起始与行数，
  `ac-body-line` 用 `AC-BBASE[si] + j` 取行，与 `dl-*` 的「同下标」模型不同。
- 设计选择：阶段 56 同样是**白名单减法**而不是加法 —— `C-ACCORDION` 原本在
  `cs-is-container` 白名单里，但它的组是 `AC-TITLE` / `AC-BODY` / `AC-EXP`
  **数据**、不是视觉子节点（同阶段 43 / 55 一路），没挂子节点时走通用兜底会
  画成一个 0×0 的 FIELD 矩形。先拿掉、再补 measure / paint。

### §5.52 阶段 57 命令式走马灯（C-CAROUSEL）新坑（2026-11-23 实测）

- 源码：`src/components.no` 新增 `CR-SLIDE`（`[]str`，扁平只增）+ 每句柄
  `CR-SBASE` / `CR-N`（随 `cmp-alloc` 长）+ 当前张走 `CMP-VALUE` +
  `cr-pad` / `cr-arrow-w` / `cr-gap` / `cr-dot-w` / `cr-dot-gap` / `cr-count` /
  `cr-slide` / `cr-add`（返回新下标，disabled 时 -1）/ `cr-set-slide` /
  `cr-current` / `cr-set-current` / `cr-select` / `cr-next` / `cr-prev` /
  `cr-clear`（写操作先查 `F-DISABLED`）
- 渲染：`src/cmp-scene.no` 把 `C-CAROUSEL` 从 `cs-is-container` 白名单里拿掉，
  `cs-measure` 新增分支（宽 = `2*CS-PAD + 2*箭头 800 + 2*间隙 800 + 最长幻灯片
  文案`，下限 `CS-MIN-LEAF-W`；高 = `2*CS-PAD + CS-BTN-H + CS-SEP-H +
  CS-LINE-H` = 8900）+ `cs-paint-carousel`（FIELD 底 + 上带「`<` 当前幻灯片
  `>`」，箭头到头 / 到尾转 MUTED + TRACK 分隔线 + 下带一行圆点，当前点
  ACCENT）+ `cs-paint-leaf` 一行路由
- 验收：`tests/cmp-carousel.no` 70 项断言全绿；`tests/main-carousel.no`
  窗口 demo 900 帧（`bye frames=900 keys=0 clicks=0 slides=3 current=2
  w=53600 h=8900` + `carousel render OK` + `scene OK`）
- 回归：见下方 §7 阶段 57 条目
- 坑三条：① **`->` 链不是互斥的** —— `cr-prev` 写成 `(i <= 0) -> { i = n-1 }`
  后接 `(i > 0) -> { i = i-1 }`，前一支改了 `i` 后后一支重新求值并再改一次；
  改成 `i = cr-current(h) - 1` 再单独一支 `(i < 0) -> { i = n-1 }` 才对；
  ② **`cs-paint` 不清场景** —— 同一棵树连续 `cs-layout` + `cs-paint` 两次，
  场景是**累加**的，第二次断言会命中第一次的旧指令，翻状态后必须先
  `cs-clear()`；③ **空池别画箭头** —— 无条件画 `<` / `>` 会让空走马灯也有
  两条文本，加 `(n > 0)`  guard 后空池只剩底色。
- 设计选择：阶段 57 同样是**白名单减法**而不是加法 —— `C-CAROUSEL` 原本在
  `cs-is-container` 白名单里，但它的幻灯片是 `CR-SLIDE` **数据**、不是视觉
  子节点（同阶段 43 / 55 / 56 一路），没挂子节点时走通用兜底会画成一个 0×0
  的 FIELD 矩形。先拿掉、再补 measure / paint。

### §5.53 阶段 58 命令式折叠面板（C-COLLAPSIBLE）新坑（2026-11-23 实测）

- 源码：`src/components.no` 新增 `CL-LINE`（`[]str`，扁平只增）+ 每句柄
  `CL-SBASE` / `CL-N`（随 `cmp-alloc` 长）+ 标题走 `CMP-TEXT` + 展开态复用既有
  `F-OPEN` 位 + `cl-pad` / `cl-head-h` / `cl-glyph-w` / `cl-gap` / `cl-title` /
  `cl-set-title` / `cl-open` / `cl-set-open` / `cl-expand` / `cl-collapse` /
  `cl-toggle` / `cl-count` / `cl-body-line` / `cl-add-body` /
  `cl-set-body-line` / `cl-clear`（写操作先查 `F-DISABLED`）
- 渲染：`src/cmp-scene.no` 把 `C-COLLAPSIBLE` 从 `cs-is-container` 白名单里拿掉，
  `cs-measure` 新增分支（宽 = `2*CS-PAD + max(展开符 800 + 间隙 800 + 标题,
  最长正文行)`，下限 `CS-MIN-LEAF-W`；高 = `2*CS-PAD + 头高 4000`，展开再加
  `N*CS-LINE-H`）+ `cs-paint-collapsible`（FIELD 底 + 头部「展开符 `v` / `>`
  均 ACCENT + 标题 FG」+ 展开时 TRACK 分隔线 + N 行正文 MUTED）+
  `cs-paint-leaf` 一行路由
- 验收：`tests/cmp-collapsible.no` 65 项断言全绿；`tests/main-collapsible.no`
  窗口 demo 900 帧（`bye frames=900 keys=0 clicks=0 lines=2 open=0 w=53600
  h=6400` + `collapsible render OK` + `scene OK`）
- 回归：见下方 §7 阶段 58 条目
- 坑三条：① **`cmp-has` 返回 bool 不能赋给 i64** —— `cl-open` 写
  `v = cmp-has(h, F-OPEN)` 报 `cannot assign bool value to i64 variable 'v'`，
  改成 `v = 0` 后 `(cmp-has(h, F-OPEN)) -> { v = 1 }`；② **`->` 链不是互斥的**
  —— `cl-toggle` 写 `(cl-open(h) != 0) -> { cl-collapse(h) }
  (cl-open(h) == 0) -> { cl-expand(h) }`，收起后第二支重新求值再展开一次，
  改成先 `v = cl-open(h)` 快照再判 `v` 才对；③ **`cs-paint` 不清场景**（同
  阶段 57）—— 翻状态重画前必须先 `cs_clear()`。
- 设计选择：阶段 58 同样是**白名单减法**而不是加法 —— `C-COLLAPSIBLE` 原本在
  `cs-is-container` 白名单里，但目录把 `group_box` 标成「title, children」而
  collapsible 只标「open, title」，它的正文是 `CL-LINE` **数据**、不是视觉
  子节点（同阶段 43 / 55 / 56 / 57 一路），没挂子节点时走通用兜底会画成一个
  0×0 的 FIELD 矩形。先拿掉、再补 measure / paint。

### §5.54 阶段 59 命令式标题容器（C-GROUP-BOX）新坑（2026-11-23 实测）

- 源码：`src/components.no` 新增 `gb-title` / `gb-set-title`（写先查
  `F-DISABLED`）+ `gb-title-h()`（标题行高 4000，与 `CS-BTN-H` 同高）。
  **不新增数据池**——标题走既有 `CMP-TEXT`，子节点是视觉节点，
  `cs-is-container` 白名单**保留**（与阶段 58 的减法相反，这是加法）
- 渲染：`src/cmp-scene.no` 先抽 `cs-measure-flow` / `cs-layout-flow` /
  `cs-layout-overlay` 三个辅助函数复用通用容器逻辑（通用分支改为调用它们，
  避免 120 行重复），再补 `cs-measure` group-box 分支（宽 = `2*CS-PAD +
  max(标题宽, 内容区宽)`，下限 `CS-MIN-LEAF-W`；高 = `2*CS-PAD + 标题行
  4000`，有子节点再加 `间隙 800 + 内容区高`，下限 `CS-BTN-H`）+
  `cs-layout` 把「内容区」当矩形重排一次（后跑的赢）+ `cs-paint` 容器守卫
  放开到「没子节点也画面板」并叠标题（FG）+ TRACK 分隔线
- 验收：`tests/cmp-group-box.no` 38 项断言全绿；`tests/main-group-box.no`
  窗口 demo 900 帧（`bye frames=900 keys=0 clicks=0 kids=3 w=53600 h=16000`
  + `group-box render OK` + `scene OK`）
- 回归：见下方 §7 阶段 59 条目
- 坑三条：① **`{}` 块里有 wildcard 兜底臂就是 elif 链（先命中先赢）** ——
  `cs-measure` 的大块末臂是 `-> { t = cmp-text(h); cs-leaf-size(...) }`，
  所以 `((cont == 1) && (n > 0))` 通用容器分支会先命中 group-box，新加的
  group-box 分支排在它后面**永远不执行**（实测：把 `gb-title-h()` 改成
  `12345` 测算值不变；分支里加 `print` 也无输出）。修法是把 group-box 分支
  移到通用容器分支**之前**。注意 standalone `->`（不在 `{}` 块里）是另一套
  语义：**全真全执行、后跑的赢**，所以 `gb-cand(i)` / `gb-kid-name(i)` 里
  `(i == 0)` 必须排在最前，否则 i=0 时它最后执行会盖掉默认值。
  ② **`print` 插值里不能调函数** —— 报 `unresolved format field
  cs-child-count(GB)`，必须先赋给临时变量再插值。③ **`CHILD-IDX` 是全局
  扁平数组，父的子链必须连续** —— 往「已经挂到 root 的 GB」后面直接
  `cmp-add-child` 追加子节点，会让 `cmp-child(GB, i)` 读到 root 的那个槽
  （GB 自己），`cs-measure` 无限递归 → `EXC_BAD_ACCESS`（lldb 回溯是
  `cs_measure` ↔ `cs_measure_flow` 交替 20+ 层）。demo 的 `a` / `r` 必须
  先 `cmp-detach` 清空再重挂
- 设计选择：阶段 59 是**白名单加法**而不是减法 —— 目录标「title,
  children」的 `group_box` 是真容器，标题走 `CMP-TEXT`、子节点是视觉节点，
  所以保留白名单，只补「标题占顶行 + 内容区在其下」的排版；顺带修掉真实
  缺陷：无子节点的 group-box 原先掉进 `cs-paint-leaf` 兜底，画成整块矩形 +
  居中文本（标题行没有底）。

### §5.55 阶段 60 命令式历史条目（C-HISTORY）新坑（2026-11-23 实测）

- 源码：`src/components.no` 新增 `hst-*` 段。扁平池 `HST-ENTRY []str`（条目
  文案，只增）+ 每句柄 `HST-SBASE []i64` / `HST-N []i64`，当前下标走
  `CMP-VALUE`（`hst-index` 越界回落 0）。公开 `hst-pad` / `hst-marker-w` /
  `hst-gap` / `hst-count` / `hst-entry` / `hst-add`（返回新下标，disabled 时
  -1）/ `hst-set-entry` / `hst-index` / `hst-set-index` / `hst-jump` /
  `hst-back` / `hst-forward` / `hst-clear`，写操作先查 `F-DISABLED`。
  与 carousel 的**唯一语义差别**：`hst-back` / `hst-forward` 夹紧在
  `[0, n-1]`，不环绕——历史不是循环的
- 渲染：`src/cmp-scene.no` 把它从 `cs-is-container` 白名单里拿掉（条目是
  `HST-ENTRY` 数据、不是视觉子节点，同 `C-PAGINATION` / `C-TAB` /
  `C-DESCRIPTION-LIST` / `C-ACCORDION` / `C-CAROUSEL` 一路），`cs-measure`
  新增分支（宽 = `2*CS-PAD + 标记条 800 + 间隙 800 + 最长条目文案`，下限
  `CS-MIN-LEAF-W`；高 = `2*CS-PAD + N*CS-LINE-H + (N-1)*CS-SEP-H`，下限
  `CS-BTN-H`）+ `cs-paint-history`（FIELD 底 + 当前行 ACCENT 标记条 + FG 文本，
  其余行 TRACK 标记条 + MUTED 文本，行间一条 TRACK 分隔线）+ `cs-paint-leaf`
  一行路由
- 验收：`tests/cmp-history.no` 72 项断言全绿；`tests/main-history.no` 窗口
  demo 900 帧（`bye frames=900 keys=0 clicks=0 entries=3 index=2 w=53600
  h=9800` + `history render OK` + `scene OK`）
- 回归：见下方 §7 阶段 60 条目
- 坑一条：**`ct-set` 只写 `i < a.len()` 的槽** —— `HST-SBASE` / `HST-N` 用
  `with-len(0)` 声明后必须在 `cmp-alloc` 里 `.push(0)`，否则 `ct-set` 静默
  不写、`hst-count` 永远 0、`hst-add` 的返回值也错（实测头一轮 72 项里 40 项
  挂，全是这一条）。`CR-SBASE` / `CR-N` / `CL-SBASE` / `CL-N` 等既有池都在
  `cmp-alloc` 里同步增长，新池照抄即可
- 设计选择：同 carousel 的减法——目录标「entries, index」的是数据池，
  不是视觉子节点，所以拿掉白名单并补 measure / paint；`cs-measure` 的
  history 分支放在 wildcard 兜底臂**之前**（`{}` 块里有 wildcard 就是
  elif 链，先命中先赢，见 §5.54 坑①）

### §5.56 阶段 61 命令式对象检查器（C-INSPECTOR）新坑（2026-11-23 实测）

- 源码：`src/components.no` 新增 `in-*` 段。目标名走 `CMP-TEXT`（同 group-box
  的标题），字段池 `IN-LAB []str` / `IN-VAL []str`（扁平只增）+ 每句柄
  `IN-SBASE []i64` / `IN-N []i64`（**必须在 `cmp-alloc` 里 `.push(0)`**，见
  §5.55 那条静默失效坑）。公开 `in-pad` / `in-marker-w` / `in-gap` /
  `in-target` / `in-set-target` / `in-count` / `in-label` / `in-value` /
  `in-add` / `in-set-label` / `in-set-value` / `in-clear`，写操作先查
  `F-DISABLED`
- 渲染：`src/cmp-scene.no` 把它从 `cs-is-container` 白名单里拿掉（字段是
  `IN-LAB` / `IN-VAL` 数据、不是视觉子节点，同 `C-PAGINATION` / `C-TAB` /
  `C-DESCRIPTION-LIST` / `C-ACCORDION` / `C-CAROUSEL` / `C-HISTORY` 一路），
  `cs-measure` 新增分支（宽 = `2*CS-PAD + 标记条 800 + 间隙 800 +
  max(目标名宽, 最长标签 + 间隙 + 最长值)`，下限 `CS-MIN-LEAF-W`；高 =
  `2*CS-PAD + 目标行 CS-LINE-H + N*CS-LINE-H + (N-1)*CS-SEP-H`，下限
  `CS-BTN-H`）+ `cs-paint-inspector`（FIELD 底 + 目标行「ACCENT 标记条 +
  FG 目标名」+ 字段行「MUTED 标签 + FG 值」+ 行间 TRACK 分隔线）+
  `cs-paint-leaf` 一行路由
- 验收：`tests/cmp-inspector.no` 67 项断言全绿；`tests/main-inspector.no`
  窗口 demo 900 帧（`bye frames=900 keys=0 clicks=0 target=abcdefghij
  fields=0 w=53600 h=12200` + `inspector render OK` + `scene OK`）
- 回归：见下方 §7 阶段 61 条目
- 坑一条：**demo 里的「场景文本总数」要把 demo 自己的标签算进去**。
  `cs-text-count()` 数的是整棵场景树，`main-inspector.no` 除了检查器的
  7 条文本还有 title / hint / stat / stat2 四条标签，所以是 11；清空字段
  后是 5。`cmp-inspector.no` 里没有额外标签才是 7 / 1（实测两处不一致时
  先怀疑这个，别去改渲染）

### §5.57 阶段 62 命令式消息流（C-MESSAGE-SCROLLER）新坑（2026-11-23 实测）

- 源码：`src/components.no` 新增 `ms-*` 段。消息池 `MS-MSG []str` /
  `MS-AUTH []str`（扁平只增）+ 每句柄 `MS-SBASE []i64` / `MS-N []i64`
  （**必须在 `cmp-alloc` 里 `.push(0)`**，见 §5.55 那条静默失效坑），
  「输入中」走 `CMP-VALUE`（0 / 1）。公开 `ms-pad` / `ms-gap` /
  `ms-count` / `ms-text` / `ms-author` / `ms-add` / `ms-set-text` /
  `ms-set-author` / `ms-clear` / `ms-typing` / `ms-set-typing`，写操作先查
  `F-DISABLED`
- 渲染：`src/cmp-scene.no` 把它从 `cs-is-container` 白名单里拿掉（消息是
  `MS-MSG` / `MS-AUTH` 数据、不是视觉子节点，同 `C-PAGINATION` / `C-TAB` /
  `C-DESCRIPTION-LIST` / `C-ACCORDION` / `C-CAROUSEL` / `C-HISTORY` /
  `C-INSPECTOR` 一路），`cs-measure` 新增分支（宽 = `2*CS-PAD +
  max(最长作者, 最长正文)`，下限 `CS-MIN-LEAF-W`；高 = `2*CS-PAD +
  N*2*CS-LINE-H + (N-1)*CS-SEP-H +（输入中 ? CS-SEP-H + CS-LINE-H : 0）`，
  下限 `CS-BTN-H`）+ `cs-paint-message-scroller`（FIELD 底 + 作者 MUTED +
  正文 FG + 条间 TRACK 分隔线；输入中时末尾再补一条分隔线 + 「typing...」
  ACCENT）+ `cs-paint-leaf` 一行路由
- 验收：`tests/cmp-message-scroller.no` 63 项断言全绿；
  `tests/main-message-scroller.no` 窗口 demo 900 帧
  （`bye frames=900 keys=0 clicks=0 messages=0 typing=0 w=53600 h=12100` +
  `message-scroller render OK` + `scene OK`）
- 回归：见下方 §7 阶段 62 条目
- 坑一条：**`ms-clear` 要连「输入中」一起清**。输入中走 `CMP-VALUE`，
  而 `ms-clear` 只清消息池的话，清空后渲染层仍会画一行「typing...」，
  面板高度也仍是「输入中」那一版（实测清空后文本数 1 / 高度 4900，
  不是 0 / 4000）。同 `hst-clear` 要一起把 `CMP-VALUE` 的当前下标清零
- 坑二条：**`ms-set-typing` 的夹紧方向**。`(v != 0) -> 1` 会把负数也夹成
  1；应当 `(v > 0) -> 1` / `(v <= 0) -> 0`，否则「关掉」的负数路径
  反而打开

### §5.58 阶段 63 命令式状态条（C-STATUS-BAR）新坑（2026-11-23 实测）

- 源码：`src/components.no` 新增 `sb-*` 段。分段池 `SB-SEG []str`（扁平
  只增）+ 每句柄 `SB-SBASE []i64` / `SB-N []i64`（**必须在 `cmp-alloc`
  里 `.push(0)`**，见 §5.55 那条静默失效坑）。公开 `sb-pad` /
  `sb-div-w` / `sb-count` / `sb-seg` / `sb-add` / `sb-set-seg` /
  `sb-clear`，写操作先查 `F-DISABLED`
- 渲染：`src/cmp-scene.no` 把它从 `cs-is-container` 白名单里拿掉（分段是
  `SB-SEG` 数据、不是视觉子节点，同 `C-PAGINATION` / `C-TAB` /
  `C-DESCRIPTION-LIST` / `C-ACCORDION` / `C-CAROUSEL` / `C-HISTORY` /
  `C-INSPECTOR` / `C-MESSAGE-SCROLLER` 一路），`cs-measure` 新增分支
  （宽 = `2*CS-PAD + Σ cs-text-w(sb-seg) + (N-1)*sb-div-w()`，下限
  `CS-MIN-LEAF-W`；**高固定 `CS-BTN-H`**）+ `cs-paint-status-bar`
  （FIELD 底 + 段 FG 文案 + 段间 TRACK 竖分隔条）+ `cs-paint-leaf`
  一行路由
- 验收：`tests/cmp-status-bar.no` 48 项断言全绿；
  `tests/main-status-bar.no` 窗口 demo 900 帧
  （`bye frames=900 keys=0 clicks=0 segments=0 w=53600 h=4000` +
  `status-bar render OK` + `scene OK`）
- 回归：见下方 §7 阶段 63 条目
- 坑一条：**细条的高度是常量，不是「内边距 + 行高」**。状态条只有
  `CS-BTN-H = 4000` 高，`cs-paint-status-bar` 画满 `ht`，文字走
  `cs-emit-text-left` 的垂直居中，所以文字 y = `y + (4000-2400)/2 =
  y + 800`——不是行顶。写窗口 demo 时按行顶算会整条错位
- 坑二条：**竖分隔条是 `sb-div-w()` 宽的全高条**，画在段与段之间；
  段宽变化会带动其后所有段与竖条整体平移（实测改第 0 段 'ready'(4000)
  为 'longer'(4800) 后竖条从 `x+5200` 移到 `x+6000`），收尾自检要按
  实测矩形重算坐标，别写死

### §5.59 阶段 64 命令式锚定弹层（C-POPOVER）新坑（2026-11-23 实测）

- 源码：`src/components.no` 新增 `pv-*` 段。正文行池 `PV-LINE []str`
  （扁平只增）+ 每句柄 `PV-SBASE []i64` / `PV-N []i64`（**必须在
  `cmp-alloc` 里 `.push(0)`**，见 §5.55 那条静默失效坑）。开合走
  `CMP-VALUE`（`pv-set-open` 把负数归 0、正数归 1），锚定边 0..3 走
  `CMP-VALUE2`（`pv-set-side` 夹到 0..3）。公开 `pv-pad=1200` /
  `pv-arrow-w=800` / `pv-gap=800` + `pv-open` / `pv-set-open` /
  `pv-side` / `pv-set-side` / `pv-title` / `pv-set-title` / `pv-count` /
  `pv-line` / `pv-add` / `pv-set-line` / `pv-clear`，写操作先查
  `F-DISABLED`
- 渲染：`src/cmp-scene.no` 把它从 `cs-is-container` 白名单里拿掉
  （正文行是 `PV-LINE` 数据、不是视觉子节点，同 `C-MESSAGE-SCROLLER` /
  `C-STATUS-BAR` 一路），`cs-measure` 新增分支（关闭 → `CS-MIN-LEAF-W` ×
  `CS-BTN-H`；展开 → 宽 = `2*CS-PAD + pv-arrow-w() + pv-gap() +
  max(cs-text-w(标题), 最长正文行)`，下限 `CS-MIN-LEAF-W`；高 =
  `2*CS-PAD + 标题行 +（有标题且有正文 ? CS-SEP-H : 0）+ N*CS-LINE_H`，
  下限 `CS-BTN_H`）+ `cs-paint-popover`（FIELD 底 + 按 `side` 贴边的
  800×800 ACCENT 箭头方块 + 标题 FG + 条件 TRACK 分隔线 + N 行正文
  MUTED；**关闭时 `(pv-open(h) == 0) -> return` 一笔画不出**）+
  `cs-paint-leaf` 一行路由
- 验收：`tests/cmp-popover.no` 62 项断言全绿；
  `tests/main-popover.no` 窗口 demo 900 帧
  （`bye frames=900 keys=0 clicks=0 open=0 side=3 w=53600 h=9700` +
  `popover render OK` + `scene OK`）
- 回归：见下方 §7 阶段 64 条目
- 坑一条：**读可选值不要用「先赋值给临时变量再匹配」的两步写法**。
  `pv-open` 一开始写成
  `k = CMP-VALUE[h]` 然后 `v = k: { ok -> it … }`，结果写进去的值
  读不回来：`pv-set-open(pv,1)` 之后 `ct-get(CMP-VALUE,h)` 已经是 1，
  而 `pv-open` 恒返回 0（`pv-set-side` / `pv-side` 同款全废）。仓库里
  既有的 `cmp-value` / `cmp-value2` 用的是**单表达式内联匹配**
  `x = CMP-VALUE[h]: { ok -> it`（空行）`-> 0 }`，改成同款后立刻恢复
- 坑二条：**箭头是固定 800×800 的方块，位置只由 `side` 决定，与内容无关**。
  side 0（下）= `(x+1600, y)`；side 1（上）= `(x+1600, y+h-800)`；
  side 2（左）= `(x, y+1600)`；side 3（右）= `(x+w-800, y+1600)`。
  写测试时容易顺手写成 `y+h` 或 `x+w`，差一个箭头边长就查不到节点
- 坑三条：**分隔线是条件件**，只在「有标题 **且** 有正文」时画；
  清空正文后分隔线要消失，只剩标题 + 面板

### §5.60 阶段 65 命令式问卷（C-QUESTIONNAIRE）新坑（2026-11-23 实测）

- 源码：`src/components.no` 新增 `qa-*` 段。三个严格同步增长的池
  `QA-PROMPT []str` / `QA-ANS []str` / `QA-REQ []i64`（扁平只增；**每次
  `qa-add` 三个池各 push 一条**，所以基址只记一个 `QA-SBASE []i64`
  就够，另加每句柄 `QA-N []i64`；两者**必须在 `cmp-alloc` 里 `.push(0)`**）。
  公开 `qa-pad=1200` / `qa-gap=800` / `qa-min-w=6400` + `qa-count` /
  `qa-prompt` / `qa-answer` / `qa-required` / `qa-answered`（答案非空题数）/
  `qa-progress`（`已作答*100/题数`，无题 0）/ `qa-complete`（必答题是否
  都作答）+ `qa-add` / `qa-set-prompt` / `qa-set-answer` /
  `qa-set-required` / `qa-clear`，写操作先查 `F-DISABLED`
- 渲染：`src/cmp-scene.no` 把它从 `cs-is-container` 白名单里拿掉
  （题目是数据不是视觉子节点），`cs-measure` 新增分支（宽 =
  `2*CS-PAD + max(各题题干宽, 各题答案宽)`，下限 `CS-MIN-LEAF-W`；高 =
  `2*CS-PAD + N*2*CS-LINE-H + (N-1)*qa-gap()`，下限 `CS-BTN_H`）+
  `cs-paint-questionnaire`（FIELD 底 + 每题「必答 TRACK 小方块标记 +
  题干 FG」一行、「答案 ACCENT」一行；标记 800×800 落在题干行左侧，
  题干随之右移 1600；题间留 `qa-gap()`）+ `cs-paint-leaf` 一行路由
- 验收：`tests/cmp-questionnaire.no` 63 项断言全绿；
  `tests/main-questionnaire.no` 窗口 demo 900 帧
  （`bye frames=900 keys=0 clicks=0 count=0 w=53600 h=18400` +
  `questionnaire render OK` + `scene OK`）
- 回归：见下方 §7 阶段 65 条目
- 坑一条：**components.no 里不能调 `cs-*`**（反向依赖渲染层）。
  一开始在 `qa-progress` 里写了 `cs-mul` / `cs-sub`，改为普通算术即可
- 坑二条：**普通算术（`+` `*` `/`）返回 `?i64`**，直接赋给 `i64` 变量会报
  `cannot assign ?i64 value to i64 variable`。按仓库既有写法
  （`select-option-index`，`components.no:5643`）改成单表达式内联匹配解包：
  `ni = i + 1: { ok -> it`（空行）`-> i }` 然后 `i = ni`；乘除要两级解包
- 坑三条：**变量类型是粘的**。`tests/main-questionnaire.no` 里先在按键
  分支写 `p0 = qa-prompt(qa, 0)`（`str`），收尾又写 `p0 = 1200`（`i64`），
  语法能过、语义却在 codegen 炸成
  `argument 2 of '@cs_add': parameter 'i64' expects a scalar but the
  argument has aggregate type 'str'`（两处 `cs-add(bx,p0)` / `cs-add(by,p0)`
  各报一次）。同一作用域里同名变量跨类型复用要避开，改个名即可
- 坑四条：**高度别只算 N 行**。每题是「题干 + 答案」两行，`cs-measure`
  一开始写 `N*CS-LINE-H`，单题得 4800（应 7200）、两题得 8000（应 12800）；
  改成 `N*2*CS-LINE-H` 再加题间距才对

### §5.61 阶段 66 命令式可搜索列表（C-SEARCHABLE-LIST）新坑（2026-11-23 实测）

- 源码：`src/components.no` 新增 `sl-*` 段。条目走扁平池
  `SL-ITEM []str` + 每句柄基址 `SL-SBASE` / 条数 `SL-N`；过滤结果另开一张
  扁平池 `SL-MATCH []i64`（存的是条目下标，不是标签本身）+ 每句柄
  `SL-MBASE` / `SL-MN`；高亮 `SL-CUR`（-1 = 没有）。**六个 per-handle 池都
  必须在 `cmp-alloc` 里 `.push(0)`，其中 `SL-CUR` 推 -1**（见 §5.55）。
  查询串直接复用 `CMP-TEXT[h]`（同 combobox / history），匹配规则用
  `lab.to-lower().contains(q.to-lower())`——`||` 与 `.to-lower()` /
  `.contains()` 都是既有能力（`select-match-index`，`components.no`）。
  公开 `sl-pad=1200` / `sl-gap=800` / `sl-min-w=6400` + `sl-count` /
  `sl-item` / `sl-add`（返回条目下标）/ `sl-set-item` / `sl-clear` /
  `sl-query` / `sl-set-query` / `sl-refresh` / `sl-match-count` /
  `sl-match` / `sl-highlight` / `sl-selected` / `sl-select` /
  `sl-select-next` / `sl-select-prev`，写操作先查 `F-DISABLED`
- 渲染：`src/cmp-scene.no` 把它从 `cs-is-container` 白名单里拿掉，
  `cs-measure` 新增分支（宽 = `2*CS-PAD + max(查询串宽, 最长匹配标签)`，
  下限 `CS-MIN-LEAF-W`；高 = `2*CS-PAD + CS-LINE-H + (有匹配 ?
  CS-SEP-H + N*CS-LINE-H : 0)`）+ `cs-paint-searchable-list`（FIELD 底 +
  查询行 FG + TRACK 分隔线 + N 行标签 FG；**高亮行先铺一条 `(x, ry, w,
  CS-LINE-H)` 的 TRACK 整行底**再画标签）+ `cs-paint-leaf` 一行路由
- 验收：`tests/cmp-searchable-list.no` 84 项断言全绿；
  `tests/main-searchable-list.no` 窗口 demo 900 帧
  （`bye frames=900 keys=0 clicks=0 items=0 match=0 w=53600 h=16900` +
  `searchable-list render OK` + `scene OK`）
- 回归：见下方 §7 阶段 66 条目
- 坑一条：**空查询也要画查询行**。`cs-measure` 的高不能写成「没匹配就
  只剩内边距」——查询行永远在，空列表的高度是 `2*CS-PAD + CS-LINE-H`
  （4800），不是 `CS-BTN_H`（4000）
- 坑二条：**空字符串也会发文本指令**。收尾想断言「清空后文本只剩四条
  标签」时，`cs-text-count()` 仍是 5（多一条空查询文本），改成断言某个
  条目标签查不到更稳
- 坑三条：**`print` 插值不能调函数**（老坑重现）：调试版里写
  `'hl={sl-highlight(sl)}'` 直接报 `unresolved format field`，必须先赋
  临时变量

### §5.62 阶段 67 命令式标题栏（C-TITLE-BAR）新坑（2026-11-23 实测）

- 源码：`src/components.no` 新增 `tb-*` 段。标题**复用 `CMP-TEXT[h]`**
  （构造函数已经把 `title` 存进去，读写走 `cmp-text` / `cmp-set-text`），
  按钮走扁平池 `TTB-BTN []str` + 每句柄基址 `TTB-SBASE` / 按钮数 `TTB-N`
  ——**这两个 per-handle 池都必须在 `cmp-alloc` 里 `.push(0)`**（见 §5.55）。
  公开 `tb-pad=1200` / `tb-gap=800` / `tb-btn-pad=800` / `tb-head-h=4000`
  四个几何常量 + `tb-count` / `tb-title` / `tb-set-title` / `tb-button` /
  `tb-add`（返回插入下标）/ `tb-set-button` / `tb-clear`，写操作先查
  `F-DISABLED`
- 渲染：`src/cmp-scene.no` 把它从 `cs-is-container` 白名单里拿掉，
  `cs-measure` 新增分支（宽 = `2*tb-pad + 标题宽 + (有按钮 ? tb-gap +
  Σ(按钮文案宽+2*tb-btn-pad) + (N-1)*tb-gap : 0)`，下限 `CS-MIN-LEAF-W`；
  高 = `2*tb-pad + tb-head-h`，下限 `CS-BTN-H`）+ `cs-paint-title-bar`
  （FIELD 底 + 左标题 FG + **右端一排** TRACK 按钮盒 + FG 文案，文字垂直
  居中）+ `cs-paint-leaf` 一行路由
- 验收：`tests/cmp-title-bar.no` 69 项断言全绿；
  `tests/main-title-bar.no` 窗口 demo 900 帧
  （`bye frames=900 keys=0 clicks=0 count=2 w=53600 h=6400` +
  `title-bar render OK` + `scene OK`）
- 回归：见下方 §7 阶段 67 条目
- 坑一条：**`cs-emit-text-left` 是垂直居中的**，文本顶 = 槽顶 +
  `(ht - CS-LINE-H)/2`。给标题栏传 `tb-head-h()`（4000）当槽高，文本顶就
  比槽顶低 `(4000-2400)/2 = 800`；收尾断言若还按「文本顶 = 槽顶」写就会
  三条全错（按钮盒的 rect 断言用槽顶、文本断言用居中后的顶，两者不能共用
  一个变量）
- 坑二条：**`tb-add` 一开始写成无返回值**，测试里 `b0 = tb-add(...)` 拿到
  的恒为 0，`u12`（`b1 == 1`）直接挂。要么学 `sl-add` 声明 `(idx i64)`
  返回插入下标，要么别在测试里依赖返回值
- 坑三条：**新增模块级名字前必须全仓库 grep**。最初池名取 `TB-BTN` /
  `TB-SBASE` / `TB-N`（跟着 kind 名 title-bar 走），编译能过，但
  `tests/main-table.no` 第 64 行有一个**自己的模块级标量 `TB-N = 20`**
  （表格 demo 的行数），被库里的同名 `[]i64` 池顶掉，main-table 直接
  `rc=133`（打印完 font init 就崩）。改名 `TTB-*`（title-bar 双写 t）后
  main-table 立刻恢复 `bye frames=900 keys=0 clicks=0 rows=11 sel=-1`
  + `table render OK`。教训：demo 文件里的顶层标量是**自由命名**的，
  库里加全局池 / 全局函数都要先 `grep -rn --include='*.no' '<名字>'`
  确认全仓库唯一

### §5.63 阶段 68 命令式工具栏（C-TOOLBAR）新坑（2026-11-23 实测）

- 源码：`src/components.no` 新增 `tlb-*` 段。条目走扁平池
  `TLB-ITEM []str` + 每句柄基址 `TLB-SBASE` / 条目数 `TLB-N` /
  当前选中 `TLB-CUR`（**三个 per-handle 池都必须在 `cmp-alloc` 里
  `.push`**，`TLB-CUR` 推 `-1`）。公开 `tlb-pad=1200` / `tlb-gap=800` /
  `tlb-chip-pad=800` / `tlb-bar-h=4000` / `tlb-count` / `tlb-item` /
  `tlb-add`（返回插入下标，**首次有条目时自动高亮第 0 项**）/
  `tlb-set-item` / `tlb-clear`（同时把选中复位 −1）/ `tlb-selected` /
  `tlb-select`（越界夹紧到最近一端）/ `tlb-select-next` /
  `tlb-select-prev`（两端回绕），写操作先查 `F-DISABLED`
- 渲染：`src/cmp-scene.no` 把它从 `cs-is-container` 白名单里拿掉，
  `cs-measure` 新增分支（宽 = `2*tlb-pad + Σ(文案宽+2*tlb-chip-pad) +
  (N-1)*tlb-gap`，下限 `CS-MIN-LEAF-W`；高 = `2*tlb-pad + tlb-bar-h`，
  下限 `CS-BTN-H`）+ `cs-paint-toolbar`（FIELD 底 + 一条横排 chip：
  未选中 TRACK 盒、当前项 ACCENT 盒，文案一律 FG 垂直居中）+
  `cs-paint-leaf` 一行路由
- 验收：`tests/cmp-toolbar.no` 74 项断言全绿；
  `tests/main-toolbar.no` 窗口 demo 900 帧
  （`bye frames=900 keys=0 clicks=0 count=3 w=53600 h=6400` +
  `toolbar render OK` + `scene OK`）
- 回归：见下方 §7 阶段 68 条目
- 坑一条：**chip 盒宽随文案变**。收尾断言里若沿用上一个状态的常量宽度
  （这里 `cw = 3200` 是按 'aa' 算的，最后重加 'solo' 时还拿 3200 去比），
  必然挂——按当前文案现算 `cs-add(cs-text-w(lab), cs-mul(cpad, 2))`
- 坑二条：**`?i64` 的两种处理方式都验证可用**：`ct-set(TLB-CUR, h,
  ct-sub(n, 1))` 这种把 `ct-sub`（返回 `?i64`）直接塞进实参的写法实测正常
  （`cmp-toolbar.no` 的 `u26` 走的就是它）；而裸算术 `cur - 1` 仍必须
  内联解包（见 §5.60 坑二）

### §5.64 阶段 69 命令式侧栏（C-SIDEBAR）新坑（2026-11-23 实测）

- 源码：`src/components.no` 新增 `sdb-*` 段。组走扁平池 `SDB-GROUP []str`
  + 每句柄基址 `SDB-GSBASE` / 组数 `SDB-GNC`；条目走扁平池
  `SDB-ITEM []str` 配一条平行的「所属组」`SDB-ITOG []i64` + 每句柄基址
  `SDB-ISBASE` / 条目数 `SDB-IN`（往任意组追加条目都不用搬移数组）；
  折叠位每句柄 `SDB-COLL`（**五个 per-handle 池都必须在 `cmp-alloc` 里
  `.push(0)`**）。公开 `sdb-pad=1200` / `sdb-gap=800` / `sdb-indent=1600` /
  `sdb-line-h=2400` / `sdb-group-count` / `sdb-group` / `sdb-add-group`
  （返回插入下标）/ `sdb-set-group` / `sdb-clear-groups` /
  `sdb-item-count` / `sdb-item` / `sdb-item-group`（越界 −1）/
  `sdb-add-item`（**组号越界返回 −1 且不追加**）/ `sdb-set-item` /
  `sdb-clear-items` / `sdb-clear` / `sdb-collapsed` /
  `sdb-set-collapsed` / `sdb-group-size`，写操作先查 `F-DISABLED`
- 渲染：`src/cmp-scene.no` 把它从 `cs-is-container` 白名单里拿掉，
  `cs-measure` 新增分支（宽 = `2*sdb-pad + max(组头宽, 未折叠时条目宽+
  sdb-indent)`，下限 `CS-MIN-LEAF-W`；高 = `2*sdb-pad + Σ_g(sdb-line-h +
  展开时该组条目数*sdb-line-h) + (N-1)*sdb-gap`，下限 `CS-BTN-H`）+
  `cs-paint-sidebar`（FIELD 底 + 组头 MUTED + 展开时属于该组的条目 FG，
  缩进 `sdb-indent`，组间留 `sdb-gap`）+ `cs-paint-leaf` 一行路由
- 验收：`tests/cmp-sidebar.no` 94 项断言全绿；
  `tests/main-sidebar.no` 窗口 demo 900 帧
  （`bye frames=900 keys=0 clicks=0 groups=2 w=53600 h=17600` +
  `sidebar render OK` + `scene OK`）
- 回归：见下方 §7 阶段 69 条目
- 坑一条：**折叠高度别心算漏一个组头**。两组折叠后是
  `2*pad + 2*line-h + gap = 2400+4800+800 = 8000`，我一开始按
  `7600` 断言挂了——`(N-1)` 个间距、`N` 个组头要分开数
- 坑二条：**收尾断言里的纵向坐标链要逐行推**。默认两组四条时组头 / 条目
  依次是 `ay`、`ay+lh`、`ay+2*lh`、`ay+3*lh`、`ay+3*lh+lh+gap`、
  `ay+3*lh+lh+gap+lh`，漏掉中间某一行会让后续所有断言一起挂
- 坑三条：**helper 套 helper 可用但容易写错名字**。`sdb-clear` 体内调
  `sdb-clear-groups(h)` / `sdb-clear-items(h)` 没问题，但手写这段时曾把
  减法写成 `cs_sub_(n,1)`、自增写成 `cs_inc_(i)`——本仓库组件层是
  `ct-sub` / `ct-inc`，渲染层是 `cs-sub` / `cs-inc`，没有下划线版本
- 坑四条：**demo 文件里的函数名被 sed 批量改名时要连自造的下划线版一起
  覆盖**。这轮收尾段一度整段写成 `find_text_color` / `sdb_add_group`
  这类下划线命名（编译器一律不认），批量替换时按「先长后短」的顺序做，
  并回头 grep 一遍 `_`

### §5.65 阶段 70 命令式抽屉面板（C-SHEET）新坑（2026-11-23 实测）

- 源码：`src/components.no` 新增 `sht-*` 段。每句柄两个池 `SHT-OPEN []i64`
  （展开位）/ `SHT-SIDE []i64`（停靠边 0=左 1=右 2=上 3=下），**都随
  `cmp-alloc` 长并 `.push(0)`**；标题仍走 `CMP-TEXT[h]`，另加
  `sht-title` / `sht-set-title`（写操作先查 `F-DISABLED`）。公开
  `sht-pad=1200` / `sht-gap=800` / `sht-bar-w=1200` / `sht-min-w=2400` /
  `sht-body-h=8000` + `sht-open` / `sht-set-open` / `sht-show` /
  `sht-hide` / `sht-toggle` / `sht-side` / `sht-set-side`（越界夹到
  [0,3]）
- 渲染：`src/cmp-scene.no` 把它从 `cs-is-container` 白名单里拿掉，
  `cs-measure` 新增分支（宽 = `2*sht-pad + max(标题宽, sht-min-w)`，下限
  `CS-MIN-LEAF-W`；高 = `2*sht-pad + CS-LINE-H`，展开再加 `sht-body-h`，
  下限 `CS-BTN-H`）+ `cs-paint-sheet`（FIELD 底 + 四条边各一种 ACCENT
  抓杆 + 标题 FG / 收起 MUTED，停靠左侧时给抓杆让位）+ `cs-paint-leaf`
  一行路由
- 验收：`tests/cmp-sheet.no` 66 项断言全绿；`tests/main-sheet.no` 窗口
  demo 900 帧（`bye frames=900 keys=0 clicks=0 open=1 w=53600 h=12800`
  + `sheet render OK` + `scene OK`）
- 回归：见下方 §7 阶段 70 条目
- 坑一条：**全局 `cmp-set-text` 不看 `F-DISABLED`**。组件专属 setter 才看
  （`tb-set-title` / `sdb-*` / `tlb-*` 都先查 flag）。只补一个
  `cmp-set-text` 调用点的话，「disabled 时改标题」会静默写进去——所以
  sheet 要有自己的 `sht-set-title`
- 坑二条：**空串也发文本指令**（老坑重现）：清空标题后 `cs-text-count()`
  不是 0 而是 1；demo 里还有四条标签 → 5
- 坑三条：`app-hover` 的纵坐标也是 `fx(...)`，没有 `fy`

### §5.66 阶段 71 命令式样式文本节点（C-TEXT）新坑（2026-11-23 实测）

- 源码：`src/components.no` 新增 `txn-*` 段。片段池两条扁平只增：文案
  `TXN-RUN []str` + 平行颜色 `TXN-COL []i64`（`-1` = 继承组件自身
  `CMP-COLOR[h]`；另有关键字强调色常量 `INP-C-KW = 0x4A90E2FF`），每句柄
  基址 `TXN-SBASE` / 段数 `TXN-N`（**四个 per-handle 池都随 `cmp-alloc`
  长并 `.push(0)`**）。公开 `txn-count` / `txn-run-text`（越界空串）/
  `txn-run-color`（越界 −1）/ `txn-add`（返回插入下标）/
  `txn-set-run`（越界写不影响）/ `txn-clear`（写操作先查 `F-DISABLED`）
- 渲染：`src/cmp-scene.no` 的 `cs-measure` 新增 C-TEXT 分支（宽 =
  `Σ cs-text-w(片段) + 2*CS-PAD`，无片段退回标题宽，下限 `CS-MIN-LEAF-W`；
  高 = `CS-LINE-H`）；新增 `cs-paint-text-node`（**无片段时一条
  `cmp-text(h)` 走 `cs-emit-text-align`，与补全前行为完全一致**；有片段按
  `A-LEFT/A-CENTER/A-RIGHT` 求起点后逐段发文本指令，片段色 `-1` 回落到
  组件色，文字垂直居中）+ `cs-paint-leaf` 一行路由
- 验收：`tests/cmp-text-node.no` 60 项断言全绿；`tests/main-text-node.no`
  窗口 demo 900 帧（`bye frames=900 keys=0 clicks=0 runs=2 w=53600 h=2400`
  + `text-node render OK` + `scene OK`）
- 回归：见下方 §7 阶段 71 条目
- 坑一条：**`cmp-set-align` 的对齐常量是 1/2/3，不是 0/1/2**——`0` 表示
  「未指定」，由 `cs-align-of` 按 kind 兜底（C-TEXT 落到 `A-LEFT`）。demo
  里写 `cmp-set-align(h, 1)` 以为居中，实际是左对齐，整排坐标全错
- 坑二条：`cs-measure` 返回的是 **clamp 后**的宽，想验「公式算出来是
  4000」是看不到的——只能验最终值 ≥ `CS-MIN-LEAF-W`
- 坑三条：调试打印不能插值函数调用（老坑重现），`print('x={cs-scene-x(i)}')`
  直接报 `unresolved format field`，得先把值存进局部变量

### §5.67 阶段 72 命令式尺寸约束盒（C-SIZING）新坑（2026-11-23 实测）

- 源码：`src/components.no` 新增 `szg-*` 段。六个 per-handle 池
  `SZG-W` / `SZG-H`（目标，0 = 跟随内容）/ `SZG-MINW` / `SZG-MINH`
  （下限，0 = 无）/ `SZG-MAXW` / `SZG-MAXH`（上限，0 = 不限），**都随
  `cmp-alloc` 长并 `.push(0)`**。公开六个 getter + `szg-set-size` /
  `szg-set-min` / `szg-set-max` / `szg-reset`（写操作先查 `F-DISABLED`）+
  纯夹紧 `szg-clamp`（**先上限后下限，冲突时下限优先**）。`sizing` 是容器
  （留在 `cs-is-container` 白名单里），内容只是没指定目标时的默认值
- 渲染：`src/cmp-scene.no` 三处——`cs-measure` 新增 C-SIZING 分支（**必须排在
  通用容器分支之前**，匹配链先命中先赢；先算内容盒再套目标 / min / max，最后
  叶子下限兜底）；`cs-layout` 同步夹紧，并把 `cmp-sw/sh` 从最前挪到**最后**
  覆盖（与 measure 末尾一致）；`cs-paint` 把 `C-SIZING` 折进通用容器臂
  （`(n > 0) || kind == C-GROUP-BOX || kind == C-SIZING`，空盒也画面板）+
  独立后置块补四边 `CS-SEP-H` 厚 TRACK 细框
- 验收：`tests/cmp-sizing.no` 64 项断言全绿；`tests/main-sizing.no`
  窗口 demo 900 帧（`bye frames=900 keys=0 clicks=0 w=12000 h=8000`
  + `sizing render OK` + `scene OK`）
- 回归：见下方 §7 阶段 72 条目
- 坑一条：**`cs-measure` 的 C-SIZING 分支被通用容器分支抢先命中**——
  一开始把新分支加在通用容器分支之后，目标 / min / max 全部不生效；
  匹配链 `{ cond -> {...} -> {...} }` 先命中先赢，特殊 kind 必须排前面
- 坑二条：**`cs-layout` 原本只覆盖目标、不夹紧**，且 `cmp-sw/sh` 写在目标
  之前（目标反压 sw，与 measure 相反）；demo 收尾按 `cs-rect(box)` 核对时
  上限 / 下限分支全错才暴露。修法是 layout 里也走 `szg-clamp` + 叶子下限，
  再把 `cmp-sw/sh` 挪到最后
- 坑三条：**顶层单开 `(kind == C-SIZING)` 绘制分支会造成二次绘制**
  （面板矩形出现两遍 + 一条空白 FG 文本）；正解是末臂保持无条件、把
  `C-SIZING` 折进通用容器臂的条件里，装饰画独立后置块

### §5.68 阶段 73 基础样式容器（C-STYLED）新坑（2026-11-23 实测）

- 源码：`src/components.no` 新增 `sty-*` 段。三个 per-handle 池
  `STY-BG`（底色，-1 = 继承组件自身 `CMP-COLOR[h]`）/ `STY-BC`（边框色，
  -1 = 不画）/ `STY-BW`（边框厚度，0 = 不画；负数夹到 0），**都随
  `cmp-alloc` 长并 `.push(-1/-1/0)`**。公开 `sty-bg` / `sty-border` /
  `sty-border-w` 三个 getter + `sty-set-bg` / `sty-set-border` /
  `sty-set-border-w` / `sty-reset`（写操作先查 `F-DISABLED`）。它是**容器**
  （`cs-is-container` 白名单保留），布局仍走通用容器分支
- 渲染：`src/cmp-scene.no` 的 `cs-paint` **不加新节点种类**——通用容器臂把
  面板色换成 `STY-BG`（-1 不动）、空盒条件加 `|| kind == C-STYLED`，装饰位
  补四条 `STY-BW` 厚 `STY-BC` 边框（色或厚任一为「不画」就整组跳过）
- 验收：`tests/cmp-styled.no` 51 项断言全绿；`tests/main-styled.no`
  窗口 demo 900 帧（`bye frames=900 keys=0 clicks=0 bw=53600 bh=4800`
  + `styled render OK` + `scene OK`）
- 回归：见下方 §7 阶段 73 条目
- 坑一条：**父节点的直接子节点必须在扁平 `CHILD-IDX` 数组里连续**——
  正确顺序是先把某容器的子树建完、挂到父节点上，最后挂其余兄弟叶子；
  一开始先挂 `r` 再建 `st` 的子树，root 的第 2 个子节点会指向 `inner`
  （框架既有约束，测试写法问题，不是本阶段引入的回归）
- 坑二条：`cs-is-container` 的参数是 **kind 常量**不是组件句柄，
  测试里传 handle 会恒为 0
- 坑三条：`print` 插值不能调函数（`'bg={sty-bg(st)}'` →
  `unresolved format field`），先把值存局部变量

### §5.69 阶段 74 扫光占位（C-SHIMMER）新坑（2026-11-23 实测）

- 源码：`src/components.no` 新增 `shm-*` 段。三个 per-handle 池
  `SHM-DUR`（周期 ms，0 = 默认 1200）/ `SHM-BAND`（高光带宽百分比，
  夹 0..100，0 = 默认 30）/ `SHM-COL`（高光色，−1 = 回落主题
  `CS-TH-MUTED`），**都随 `cmp-alloc` 长并 `.push(0/0/-1)`**；相位沿用既有
  槽位 `CMP-VALUE[h]`（0..100，0 = 不扫）。公开 `shm-duration` /
  `shm-set-duration`（负数归 0）/ `shm-band` / `shm-set-band` / `shm-color`
  / `shm-set-color` / `shm-phase` / `shm-set-phase` / `shm-advance(h, dt)`
  （步进 `dt*100/duration`，duration≤0 取 1200，累加到 ≥100 绕回；写操作
  先查 `F-DISABLED`）/ `shm-reset`。它是**叶子**（非容器，`cs-is-container`
  白名单不含它）
- 渲染：`src/cmp-scene.no` 的 `cs-paint-leaf` 里把旧 `C-SHIMMER` 分支
  （半宽 MUTED 块）换成 TRACK 底 + 相位 > 0 时一条高亮带：
  `band = w*bpct/100`、`span = w - band`、`off = ph*span/100`
  （相位 100 恰好贴右缘；相位 0 只画底）。**不新增节点种类**
- 验收：`tests/cmp-shimmer.no` 51 项断言全绿；`tests/main-shimmer.no`
  窗口 demo 900 帧（`bye frames=900 keys=0 clicks=0 bx=1200 by=1200
  bw=53600 bh=2400` + `shimmer render OK` + `scene OK`）
- 回归：见下方 §7 阶段 74 条目
- 坑一条：**窗口 demo 不要硬编码内容宽**——dummy 驱动下实测 `bw=53600`
  （不是 root 18000×20000 时的 15600），底 / 高光带坐标一律按
  `cs-rect(shm)` 量出来的宽反推
- 坑二条：匹配链里 `{ cond -> {...} -> {...} }` 的两臂必须成组包起来，
  裸写第二臂报 "an else arm must live inside an if-group"
- 坑三条：收尾断言的顺序会改状态——先跑 `advance` 把相位改成 10，后面
  再按「相位 100 贴右缘」查带子就假失败；要么重设相位，要么改期望值

### §5.70 阶段 75 窗口描边容器（C-WINDOW-BORDER）新坑（2026-11-23 实测）

- 源码：`src/components.no` 新增 `wb-*` 段。两个 per-handle 池
  `WB-RAD`（四角统一圆角半径 px*100，0 = 直角）/ `WB-W`（边框厚度 px*100，
  0 = 不画框），**都随 `cmp-alloc` 长并 `.push(0)`**。公开 `wb-radius` /
  `wb-set-radius`（负数归 0）/ `wb-width` / `wb-set-width`（负数归 0）/
  `wb-reset`（写操作先查 `F-DISABLED`）。它是**真容器**（在
  `cs-is-container` 白名单里），此前一直只有构造函数
- 渲染：`src/cmp-scene.no` 三处——① `cs-measure` 加分支，让空盒也量出
  `CS-MIN-LEAF-W × CS-BTN-H`（否则退化成 2400×2400 叶子盒）；② 通用容器臂
  的空盒条件加 `|| kind == C-WINDOW-BORDER`，面板底固定主题 `CS-TH-FIELD`
  （组件色留给描边）；③ 四边描边块：`WB-W` 厚、组件色（`CMP-COLOR[h]`），
  每条边在角部各让出 `WB-RAD`（半径夹在 w/h 的一半以内），**不新增节点
  种类**
- 验收：`tests/cmp-window-border.no` 49 项断言全绿；
  `tests/main-window-border.no` 窗口 demo 900 帧
  （`bye frames=900 keys=0 clicks=0 bx=1200 by=1200 bw=53600 bh=4800`
  + `window-border render OK` + `scene OK`）
- 回归：见下方 §7 阶段 75 条目
- 坑一条：**单测别照抄 `components-smoke.no` 的全目录断言**
  （`cmp-count() == 56`）——那是「整份目录的组件总数」，放单测里必然假失败
- 坑二条：**收尾自检的顺序会改状态**——demo 恢复内部标签后
  `text-count()` 是 5（标题 / 提示 / 两行状态 + inner panel），不是 4
- 坑三条：demo 辅助函数名别写成下划线版（`cs_rect` / `app_win_wf`），
  编译器只认点号 / 连字符版（`cs-rect` / `app-win-wf`）

### §5.71 阶段 76 平台能力：窗口 resize 回写 + 文本输入 + 系统剪贴板（2026-11-25 实测）

审查（P0–P3）里三类「非组件」缺口一次补齐，组件目录零改动：

- **窗口 resize 回写**：`src/app.no` 新增 `app-sync-window-size()`——读
  `_noui-window-get-size`，只有宽高都 >0 才写 `APP-WIN-W/H/WF/HF`。
  `app-frame()` 每帧开头调用，位置在 `app-window-apply-all()` **之前**：
  拖边框 / 全屏切换后布局常量跟着真窗口走；显式声明尺寸的
  `C-WINDOW-EXT` 每帧仍覆盖回去（显式优先），未声明的窗口跟随真实尺寸。
- **文本输入**：shim 侧 `noui_poll` 接 `SDL_EVENT_TEXT_INPUT`（事件类型
  **7**），UTF-8 串存内部 256B 缓冲，`noui_text_take()` 读完即空；`app.no`
  加 FFI `_noui-text-take` 与 `app-text()`。`dv-app.no` 的 `dva-type(s)` 按
  **码点**切片（`str.slice` 是码点索引，中文 / emoji 不会被切成半个字节）
  喂给三级焦点：命令面板 `DVA-PAL-FOCUS`（`dv-pal-type`）> 输入框
  `DVA-FOCUS`（`input-append`）> 代码编辑器 `DVA-EDIT-FOCUS`
  （`dv-node-by-key` → `dv-edit-of` → `dv-edit-insert`）。主循环
  `(t == 7)` 分支放在按键 / 点击派发之前。
- **系统剪贴板**：shim `noui_clipboard_copy` / `noui_clipboard_paste`
  （SDL clipboard，paste 有 4096B 上限）；`app.no` 暴露
  `app-clipboard-copy/-paste`。`dv-app.no` 的
  `dva-clipboard-hit(h, px, py)` 按 `cs-paint-clipboard` **同款几何**
  （右内边距 1200、方块 2400、垂直居中）检测复制方块，命中就把
  `clip-content(h)` 写进系统剪贴板并返回 1，调用方据此跳过普通点击派发。
- 验收：`tests/app-resize.no` 8 项断言全绿（560×420 → resize 700×500 →
  四个常量跟随、亚像素 = 100×、同尺寸再同步幂等、拖回原尺寸恢复）；
  `tests/dva-input.no` 9 项断言全绿（无焦点 no-op、`'a0 中😀'` 逐码点
  进入输入框、复制方块中心命中外不命中、非剪贴板组件不命中、句柄 -1
  不崩）。`no vet src/` 回到 7 error（全是下方 §5.5 记载的 by-design
  host-callback 未定义）/ 0 warning。
- 测试运行器：`scripts/run-noui-tests.sh` 无头全量跑 `tests/*.no`——
  dummy 视频/音频驱动 + `NOUI_MAX_FRAMES` 帧闸 + 退出码判定 + 并行 +
  单测超时；2026-11-25 实测 **131/131 通过**（其后阶段 77–80 扩到 136，
  见 §5.73）。noui 现已接入 CI：`.github/workflows/test.yml` 的
  `noui-test` job（macos runner，无头全量），详见 §5.73。
- 坑三条：① `i = i + 1` 在 nolang 里得到 `?i64`，不能赋回 `i64`
  变量（继续用 `cs-inc`）；② 剪贴板组件的 `CMP-X/Y/W/H` 要等
  `cs-layout` 才有值，单测别在 `dv-render` 后立刻读几何；③
  `str.slice` 是**码点**索引，正是切 UTF-8 字符想要的语义。

### §5.72 阶段 77 键盘焦点遍历 + 阶段 78 图片 / 精灵（2026-11-25 实测）

- **阶段 77 焦点遍历**：`src/shim/shim.c` 的键码打包此前只有 24 位 x 槽，
  方向键（`0x40000000` 段）被截断成 `0x50` 而失效。现在键码拆 `x(低24) +
  y(高8)`，Shift 标记占 y 的 bit23；`app.no` 的 `app-poll()` 合回完整键码放
  `x`、标记放 `APP-SHIFT[0]`（FFI 声明处显式标注 `i64`——掩码表达式会被推成
  `byte`，而 `byte` 没有 `.to-f64()`）。`src/dv-app.no` 新增
  `dva-focus-step(root, dir)`：候选 = 视图创建序中「输入类 + 已挂载 + 未禁用」
  的节点，环形步进（正向无焦点取第一个、反向取最后一个），交接只点亮新焦点
  （复用 `F-FOCUSED`）；编辑器 / 命令面板聚焦时遍历让位，保持既有语义。
  验收：`tests/dva-focus-tab.no` 9 项断言全绿（含禁用节点跳过、环形回绕、
  反向回绕、让位规则）。
- **阶段 78 图片 / 精灵**：新增最小 PNG 解码器 `src/shim/img_png.c`
  （zlib `uncompress` + 反滤波 + Paeth + 调色板；ct 0/2/3/4/6、bit depth 8、
  非隔行，输出 RGBA8），`shim.c` 注册表把解码结果存为 **SDL_Surface**
  （纹理由各窗口按需创建，见 §5.73）；`noui_img_load/-size/-draw_image`
  导出标量 FFI。Nolang 侧 `C-SPRITE = 101` 组件 + `CMP-IMG` 池
  （`components.no`），`cs-paint` 发 `CS-IMG` 指令（`cmp-scene.no`），
  `app-draw-scene` 拉伸绘制。验收：`tests/cmp-sprite.no` 8 项断言全绿
  （加载 / 尺寸 / 坏路径返回 0 / CS-IMG 指令 / 几何=布局盒 / 无纹理不发指令 /
  端到端冒烟），夹具 `tests/fixtures/dot.png`（8×6 RGBA）。
- 语言坑（实测）：`print('... {f()}')` 的格式串里**不能内嵌函数调用**，
  会报 `unresolved format field`；先赋值给变量再插值。

### §5.73 阶段 79 GPU 后端接入主循环 + 阶段 80 多窗口（2026-11-25 实测）

- **阶段 79 GPU 接线**：`gpu_shim.c` 此前是孤立 demo。现在
  `package.noml` 的 `link-libs` 增加 `noui-gpu-shim`，`app.no` 加开关
  `app-use-gpu(on)` / `app-gpu-mode()` / `app-gpu-active()` 与一组
  `_noui-gpu-*` FFI。`app-init` 在 GPU 模式走 `_noui-gpu-init` +
  `_noui-gpu-window-create`（失败清理半初始化设备并返回 -1）；`app-frame`
  改用 `_noui-gpu-set-clear` + 绘制 + `_noui-gpu-frame`；`app-delay` /
  `app-shutdown` 同步分流。**GPU 路径只画矩形**：裁剪是 SDL_Render 概念，
  文本 / 图片指令静默跳过（无纹理采样管线），`C-WINDOW-EXT` 在 GPU 模式
  空转。shim 侧同时修掉两个真 bug：`noui_gpu_draw_rect` 归一化写死
  200×150（改为按真实窗口尺寸），形参 `float` 改 `double`（与 nolang `f64`
  实参 ABI 对齐，见 §6 FFI 陷阱）。验收：`tests/app-gpu.no`——开关 / 状态
  查询任何环境可跑；有 GPU 的机器上 init 应成功并连跑两帧，dummy 驱动下
  只要求优雅返回 -1（本机即后者）。
- **阶段 80 多窗口**：单窗口假设全链路拆除。shim 增窗口注册表
  （`NOUI_WIN_MAX 8`），全局 `g_window/g_renderer` 改为指向「当前 attach」
  的槽位——既有 setter/getter/绘制函数因此**逐窗复用、签名不变**。新 API：
  `noui_window_create` 返回 1 基 id（新窗自动成为当前窗）、
  `noui_window_attach/count/close`、`noui_window_event`（最近一次事件所属
  窗口，供 `app-event-window()`）。事件编码布局不变（i64 槽位已满），
  窗口 id 走独立标量查询。图片注册表改为 surface + **每窗口纹理缓存**
  （SDL_Texture 属于创建它的 renderer，跨窗复用会坏），关窗 / 退出按槽位
  清理。`app-frame()` 逐窗「attach → 对齐该窗真实尺寸 → 清屏 → 绘制 →
  present」，最后回到主窗：所有窗口**镜像同一份 Scene**（每窗尺寸自适应，
  见下方限制）。GPU 后端目前单窗，`app-window-create-extra` 在 GPU 模式
  返回 0。验收：`tests/app-windows.no` 14 项断言全绿（开副窗 id=2、副窗
  尺寸独立、切回主窗恢复、双窗各跑两帧、无效 id 温和失败、GPU 拒绝副窗）。
- **限制（如实记录）**：镜像窗口按各自真实尺寸重排布局常量，但 Scene 指令
  由调用方在帧首生成，因此非主窗尺寸下坐标不重新排版（小窗裁切、大窗留白）；
  每窗独立 UI 需要 Scene 按窗过滤，属后续架构演进。`NOUI_MAX_FRAMES` 帧闸
  按 present 次数计数，多窗时会提前触发（测试语义，不是错误）。
- **CI**：`.github/workflows/test.yml` 的 `noui-test` job 已落地——
  macos runner + `brew install llvm sdl3 sdl3_ttf coreutils` + `make no` +
  `./build-shims.sh` + `./scripts/run-noui-tests.sh`（dummy 驱动无头全量）。
  运行器实测 136/136 通过（含新增 app-gpu / app-windows）。注意 job 未显式
  装 zlib：macOS SDK 自带 libz，`-lz` 可直接链接。
- **同批修掉两个编译器隐患**（都在 `src/`，非 noui 自身）：跨模块同名顶层
  可变绑定从「静默别名」（MIR globals 以裸名为键，导入顺序决定谁赢）改为
  合并后硬错误 `dup-global-xmod`；match arm 体内手写 let 不再穿透全局槽位
  （hir2mir 写回闸门加 `matchDepth == 0`，arm 内建臂局部阴影、arm 末还原）。
  永久回归测试：`src/build/cross_module_dup_global_test.go`（含大写常量与
  std 内部先赢的放行边界）、`src/mir/arm_shadow_global_test.go`（MIR 层断言
  arm 内对全域槽位 0 次写入 + 非 arm 赋值仍写回的非空洞对照）。Go 套件
  `./...` 全绿（手动注入 StdSigKey 时 `TestStdASTBlobCarriesParseConfig`
  需使用 blob 内嵌 key，属构建方式差异；`i18n/catalog` 的
  `TestEnCatalogFresh` 红是既有问题，与本批无关）。

### §5.74 阶段 81：事件面与绘制原语补齐（2026-11-25 实测）

审查 P2「平台能力缺口」里尚未落地的项一次补齐；P3 工程化收尾同步完成。

- **事件码表（shim.c 顶部常量，i64 打包不变）**：1 QUIT、2/8/10 左/右/中按下、
  3 MOVE、4 KEYDOWN、5 WHEEL、6/9/11 左/右/中抬起、7 TEXT、12 KEYUP、
  13/14/15 FINGER_DOWN/UP/MOTION、16/17/18 PEN_DOWN/UP/MOTION、
  19 DROP_FILE、20 DROP_POS、21 DROP_BEGIN、22 DROP_COMPLETE、
  23 PEN_AXIS、24 DROP_TEXT。打包仍走 `type | x<<8 | y<<32 | aux<<56`
  （x/y 各 24 位），辅助函数 `ev_pack` 统一夹紧。
- **aux 语义分流**：鼠标与键盘事件的 aux 字节是**修饰键位图**
  （bit0 Shift / bit1 Ctrl / bit2 Alt / bit3 Meta，`mods_byte()` 读
  `SDL_GetModState`）；触摸 / 笔事件的 aux 是**压力量化**（`quant_unit()`
  把 0..1 映射到 0..255）。阶段77 起 Shift 不再挤占键码的 y bit23，
  key-up 因此可以复用同一键码拆分（低 24 + 高 8）。
- **坐标换算**：触摸是归一化坐标，按 attach 窗口像素尺寸换算（越界窗口退回
  1.0 防止除零）；笔坐标本就是像素。拖放内容不进 i64，走独立静态缓冲 +
  `noui_drop_take()`（读完即空，与 `noui_text_take()` 同构，务必同一轮取）。
- **Nolang 侧解码**：`app-event-decode(e)` 纯函数（不碰平台），app-poll 调它；
  合成 i64 即可单测整个事件面。新增 `app-mods()/app-pressure()/app-drop()/
  app-display-scale()`。
- **绘制原语**：Scene 指令 `CS-ROUND`(5) / `CS-SHADOW`(6) / `CS-IMGR`(7)，
  附加参数池 `CS-SR`（半径）/ `CS-SD`（阴影偏移）/ `CS-SA`（角度）/
  `CS-SF`（翻转位）。C 侧：`noui_draw_round_rect`——SDL_Renderer 无圆角 API，
  用「中央矩形 + 上下逐行 sqrt 收窄」近似（半径夹到 min(w,h)/2）；
  `noui_draw_shadow`——6 层外扩半透明圆角矩形叠加；`noui_draw_image_rot`——
  `SDL_RenderTextureRotated` + `SDL_FlipMode`（图片纹理惰性创建抽成
  `win_img_tex()` 供普通/旋转两条路径共用）。GPU 后端无逐行/纹理管线：
  圆角降级为直角矩形，阴影与旋转图片跳过（同阶段79 口径，已如实记录）。
- **透明度打通**：`app-draw-scene` 所有绘制出口的 alpha 从硬编码 255 改为
  `col-a(c)`（矩形 / 填充 / 文本 / GPU 矩形）；窗口清屏底色仍固定 255（窗口
  自身不应半透明）。
- **字体 fallback / DPI**：`app-init` 字号乘 `noui_window_display_scale1000`
  缩放；候选路径 Arial → Helvetica → PingFang → Library/Arial → Menlo 逐个
  尝试，全失败才回退 DebugText（此前单一硬编码路径，换机器静默丢字体）。
- **默认按钮 / Enter 提交**：`dva-default-submit(S,root)`——焦点组件带
  dv-act 时 Enter 等价点击，走同一 派发 → 归约 → 回调 路径；输入框等
  无 dv-act 组件不受影响；主循环「无面板焦点」分支接入。
- **P3 收尾**：新增 `NEXT.md`（现状 / 下一步 / 约定）；删除早期拼接脚本
  `gen-test.sh`（产出 kernel.no / test-kernel.no 保留为历史回归）。
  另修掉 `tests/dv.no` 的行首 `&&` 续行——该写法当前解析器不支持
  （报 `m0db1nfx`），此前一直靠编译缓存命中旧产物；cmp-scene 变更强制重编后
  暴露，已改单行。全量套件 **139/139**（新增 app-event-decode /
  cmp-scene-prims / dva-default-submit 三份）。CI job 同路复跑。

### §5.75 阶段 82：GPU 后端的纹理管线（2026-11-26 实测）

阶段79 起 GPU 路径只画矩形，文本 / 图片 / 圆角 / 阴影 / 旋转图静默降级或跳过。
本阶段把 `gpu_shim.c` 补成完整纹理管线，`app-draw-scene` 按 Scene 指令分流；
dummy 驱动下 GPU init 失败仍优雅返回 -1（不算失败）。

- **两条 MSL 管线**：纯色管线顶点 `{vec2 pos, vec4 color}`（24B）；纹理管线顶点
  `{vec2 pos, vec4 color, vec2 uv, vec2 cxy, float rad}`（`QVert`），片段着色器
  `texture.sample(smp, uv)` 乘顶点色（`TVert` 无角点/半径字段）。旋转在 CPU
  端算好四角：先平移 `(x,y)`/`(x+w,y+h)` 减中心再绕中心转 `angle`；flip 位
  （bit0 水平 / bit1 垂直）翻转 UV 而非几何。
- **纹理注册表**：`tex_push` 统一入口，`id` 从 1 起；PNG 走 SDL_image 解码后
  转 surface→GPU 纹理，文字走 TTF 渲染成白底 alpha 蒙版（缓存按字符串复用）。
  文字纹理**必须转 `SDL_PIXELFORMAT_ABGR8888`**——SDL 的 RGBA8888 内存序是
  A,B,G,R（小端），而 Metal 的 R8G8B8A8_UNORM 期望 R,G,B,A，直接用会让文字
  变青。
- **sampler 绑定**：`SDL_GPUShaderCreateInfo.num_samplers` 必须由调用方声明
  （纹理管线传 1），否则 Metal 校验层报 `missing Sampler binding at index 0`。
- **合并绘制**：连续 `OP_QUAD` 合并成一次 `SDL_DrawGPUPrimitives`（6 顶点 /
  批）；`OP_TEX` 逐条 draw。帧末上传 transfer buffer、跑 render pass、
  submit 后释放 buffer 并清零 op 队列。
- **app.no 分流**：CS-TEXT → `_noui-gpu-text-load` + `_noui-gpu-draw-tex`
  （顶点色染色蒙版）；CS-IMG → `_noui-gpu-img-load` + `_noui-gpu-draw-tex`；
  CS-IMGR → `_noui-gpu-draw-tex-rot`（flip 直接传 i32，**不要过 pxi**，
  flip 是枚举起量，除以 100 会归零导致永远不翻转）；CS-ROUND / CS-SHADOW →
  `_noui-gpu-draw-round/_draw-shadow`（圆角走平滑边界着色器近似）。
  字体初始化：`app-font-open` 成功后调 `_noui-gpu-font-init(path, size)`，
  GPU 模式用独立字体句柄。
- **测试**：`tests/app-gpu.no` 扩到 13 项（开关状态 + 颜色通道 + 真机 /
  dummy 分支；live 分支断言 img-load 打包尺寸 8×6、文字缓存命中、混排冒烟、
  shutdown 后 inactive；无 GPU 分支断言各 FFI 返回 0 且 draw 不崩）；
  `tests/app-draw-prims.no` 软件后端混排断言 CS-IMGR/CS-SHADOW getter。
  两路均绿：dummy → `ALL APP-GPU TESTS PASSED`、`ALL APP-DRAW-PRIMS TESTS
  PASSED`；真机 Metal → 同输出 + `Metal API Validation Enabled`。
- **像素级回归**（临时脚本已删）：BMP 截帧 + Python 解析确认背景精确
  0x2A2A38、文字纯白、plain bbox (20,20)-(59,59)、45° 旋转菱形顶点符合
  预期、flip=1 后 half.png 左绿右红。

### §5.76 阶段 83：触摸 / 压感笔的事件侧语义（2026-11-26 实测）

阶段81 把事件码铺到了 `app-poll`，但组件侧还没有消费触摸 / 笔。本阶段做三件
事：统一主指针语义、修正 aux 含义、让测试能端到端驱动主循环。

- **aux 按类型分流**：此前 `APP-MODS[0] = aux` 无条件生效，滚轮事件的
  delta、笔压都被当成修饰键；反过来触摸 / 笔的压力也读不到。新增纯函数
  `app-is-mods(t)`（2..4 / 6..12）与 `app-is-contact(t)`（13..18 / 23），
  `app-poll` 只把 aux 写进对应的槽。
- **dva-press 单一入口**：鼠标左键、手指按下（13）、笔按下（16）都走
  `dva-press(S, root, px, py)`——拖分栏优先，否则命中 → 焦点 → `dv-act`
  归约。抬起（6/14/17）清拖拽与 `DVA-TOUCH`；移动（15/18）与鼠标移动同语义；
  笔轴（23）只更新悬停位置，不产生点击（笔尖悬空也算轴事件）。接触方式不
  改变语义，只通过 `dva-touch()` / `app-pressure()` 暴露给视图。
- **事件注入**：dummy 驱动不产生真实触摸 / 笔事件，没有注入就无法端到端
  验证。`shim.c` 加 16 深度的 FIFO `noui_event_push`（打包 i64 与
  `noui_poll` 同格式），经 `app-event-push` 暴露。刻意不在 `noui_init` 清零
  ——测试通常先 push 再 `dva-run`（内部会重新 init），清零会冲掉待发事件。
- **手指坐标窗口修正**：`SDL_EVENT_FINGER_*` 的 x/y 是归一化的，此前统一用
  「当前 attach 的窗口」换算，多窗口时会算错；改为按 `ev_window_id` 找到
  事件所属窗口再换算。
- **顺带修掉的绘制包装 bug**：`app-draw-round-rect` / `-shadow` /
  `-image-rot` 曾把颜色和 flip 一起过 `pxi`（除以 100），圆角会变全黑、
  翻转永远为 0；现原样传 C。
- **语言陷阱记录**：数组元素直接和字面量比较会被推断成 bool 而恒假
  （`S[3] == 1` 为 false，`S[3] == ONE` 才真）；测试里统一先落到命名常量。
- **测试**：`tests/dva-touch.no` 11 项——aux 分类纯断言、`dva-press` 归约、
  注入手指 / 笔序列端到端命中按钮（状态池置位、点击计数 +1、`dva-touch()`
  置位 / 抬起清零）。dummy 驱动下 `ALL DVA-TOUCH TESTS PASSED`。

### §5.77 阶段 84：拖放落点反馈（2026-11-26 实测）

阶段81 通了 `DROP_*` 事件码，但组件侧没有「放置区」语义。本阶段补上标记、
会话与派发，并且**不新增主循环回调**（避免 26 个 demo / 测试都要改签名）。

- **放置区是 flag**：`F-DROP = 2048`；`cmp-drop-zone(h, on)` /
  `cmp-is-drop(h)` 只是 `CMP-FLAGS` 的语义包装。任何组件都能当放置区，
  不需要新组件类型。
- **主循环会话**：`t==21 BEGIN` 置 `DVA-DROP-ACTIVE` 并清落点；`t==20 POS`
  走 `cs-hit`，命中带 `F-DROP` 的组件就记 `DVA-DROP-TARGET`（同时累加
  `DVA-DROP-HITS`），否则清 -1；`t==22 COMPLETE` 取 `app-drop()` 载荷，
  以 `DV-A-DROP = 13` 复用 `dv-app-click(S, k, act, 0, nw)` 出口并归约，
  然后清会话。`t==19/24 FILE/TEXT` 只更新 shim 缓冲，不单独派发（载荷在
  COMPLETE 那一轮取，保证「同轮取」语义）。
- **反馈路径**：视图渲染时直接查 `dva-drop-active()` / `dva-drop-target()`
  就能画高亮；计数器 `dva-drop-hits()` / `dva-drops()` 给测试与调试用。
- **测试**：`tests/dva-drop.no` 13 项——标记可查可清、默认状态、注入
  `BEGIN→POS×2→FILE→COMPLETE` 端到端（回调收到 `DV-A-DROP`、落点 key=8、
  状态池 TOGGLE 生效、会话清空）、以及没有 BEGIN 的裸 COMPLETE 不派发。
  dummy 驱动下 `ALL DVA-DROP TESTS PASSED`。

### §5.78 阶段 85：字体回退列表产品化（2026-11-26 实测）

`app-init` 里原本硬编码 5 条 macOS 字体路径，主题无法配置，DPI 变化也无法重开。
本阶段把候选清单、内置默认、最近成功路径与字号收进 C shim，Nolang 只做「按序
尝试」的循环——**语言约束决定了存储位置**：Nolang 存不住多字符串状态
（模块级 str 数组不能下标写，普通 str 全局在函数内赋值不持久，str 元素 `.len()`
还会打崩 codegen），所以「清单类状态」只能落在 C，与既有 `noui_event_push`
注入队列同一个理由。

- **C 侧（两个后端各一份）**：软件 `shim.c` 加候选表 `g_fcands`——条目数组
  `realloc` 翻倍扩容（初始 8 条），**每条路径单独 `strdup`**，所以条数与单条
  长度都没有上限；`clear` 逐条 `free`。+ `noui_font_cands_{clear,add,count,at}`、
  `FONT_DEFAULTS[5]` +
  `noui_font_defaults_{count,at}`、`g_font_path/g_font_pts` + `noui_font_path/_size`；
  成功打开才记录路径（`font_open_one` 统一出口）。GPU `gpu_shim.c` 对称提供
  `noui_gpu_*` 同名函数；GPU 重开还会遍历 `g_tex_src[]` 释放文字纹理（§5.75），
  图片纹理不受影响。
- **运行期重开**：`noui_font_reopen(scale1000)` / `noui_gpu_font_reopen` 用同一
  路径、`16pt × scale`（内部夹到 ≥1）重开；没有已知路径时返回 0，调用方保持现状，
  不会因为换屏失败把现有字体搞没。
- **Nolang API**（`src/app.no`，全部后端无关分发）：`app-font-add(path)`
  （返回是否收下：空串拒收；清单动态增长、无条数上限）/ `clear-cands` /
  `cand-count` / `cand(i)` / `defaults-count` / `default-at(i)` /
  `try-all(fs)` / `path()` / `size()` / `reopen()`。`try-all` 先候选后默认，
  `path/size` 读的是 C 记录。`app-init` 改用 `fv = app-font-try-all(fs)`；
  空清单时自动回退内置 5 条。
- **测试**：`tests/app-font-list.no`——内置默认可读且非空、候选增/查/清、
  坏候选 try-all=0 且不记路径、无路径 reopen=0、**扩容**（连加 32 条全部收下、
  可回读、空串被拒）、**超长单条**（1200+ 字符照收，旧定长表会截断）、
  app-init 集成后「有路径 ⇔ 可用」一致且 reopen 保持可用。
  dummy 驱动下全绿（本机无那些字体，走的是不可用双分支）。详见 README 验收记录。
- **语言坑备忘**：本阶段实测到的 Nolang 限制（都已绕开并留在代码注释里）：
  模块级 str 数组 `.push` 报 `needs receiver and element`、下标写报
  `index-store of value with no slot`；模块级标量 str 在函数内赋值不持久（顶层
  赋值才持久）；str 元素 `.len()` 触发 clang codegen `integer constant must have
  integer type`；字符串插值不能内嵌函数调用。

