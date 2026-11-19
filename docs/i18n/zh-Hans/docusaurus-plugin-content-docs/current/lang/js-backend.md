---
sidebar_position: 8
---

# JS 后端

Nolang 支援将 `.no` 原始码直接编译为 JavaScript，无需 LLVM 工具链。JS 后端采用**型别擦除**（type erasure）策略：所有 Nolang 型别标注在 JS 输出中不保留，仅生成执行时行为。

## 快速开始

```bash
# 编译为 JS（输出至 dist/<name>.js）
no build --js main.no

# 浏览器模式：生成 JS + HTML wrapper
no build --js --browser main.no

# 运行：编译为 JS 并以 node 执行
no run --js main.no

# 浏览器模式：编译并在预设浏览器中打开
no run --js --browser main.no
```

## 设计原理

JS 后端的核心设计：

- **型别擦除**：JS 为动态型别，Nolang 的 `int`/`str`/`bool`/`vec[T]`/`[N]T`/`?T` 等型别标注在 JS 输出中完全不保留
- **无需 LLVM**：直接从 AST 发射 JavaScript 原始码，不依赖 clang/LLVM 工具链
- **双环境**：支援 Node.js（预设）和浏览器（`--browser`）两种目标环境
- **平台标注**：`#{js}` 和 `#{js-browser}` 控制代码的平台可见性

实作位于 `src/build/js/` 目录：

| 档案 | 职责 |
| --- | --- |
| `generator.go` | AST → JavaScript codegen 主逻辑 |
| `expr.go` | 表达式生成 |
| `stmt.go` | 语句生成 |
| `builtin.go` | 内建函数映射（print→console.log 等） |
| `html_wrapper.go` | 浏览器模式 HTML 模板 |

## 命令列参数

| 参数 | 说明 |
| --- | --- |
| `--js` | 使用 JS 后端（发射 JavaScript，绕过 LLVM） |
| `--browser` | 生成浏览器导向输出（HTML + JS，需搭配 `--js`） |
| `--esm-cdn <url>` | 覆盖浏览器 ESM import map 的 CDN 前缀（预设 `https://esm.sh`） |
| `--js-runtime <cmd>` | `no run --js` 使用的 JS 执行期（预设 `node`；可换 `bun`、`deno` 等，允许附带参数） |
| `-o <path>` | 指定输出路径 |

### 构建

```bash
no build --js main.no                    # 输出 dist/main.js
no build --js -o app.js main.no          # 指定输出路径
no build --js --browser main.no          # 输出 dist/main.js + dist/main.html
```

### 运行

```bash
no run --js main.no                      # 编译为 JS 后以 node 执行
no run --js --browser main.no            # 编译为浏览器 JS + HTML 并打开浏览器
```

#### 选择 JS 执行期

`--js-runtime` 指定执行产物的执行期，**预设 `node`**（Node.js）。任何能跑 ES module 的执行期都可以：

```bash
no run --js --js-runtime bun main.no             # Bun
no run --js --js-runtime deno run main.no        # Deno（首个 token 是可执行档，其余是脚本前的参数）
no run --js --js-runtime "deno run --allow-read" main.no
no run --js --js-runtime "node --enable-source-maps" main.no
no run --js --js-runtime /opt/js/bin/node main.no   # 指定绝对路径
```

- 规格的第一个空白分隔 token 是可执行档，其余 token 会排在脚本路径之前；程式本身的参数照旧写在 `.no` 档名之后。
- 预设（未指定 `--js-runtime`）使用 `node`；若系统只有 Debian/Ubuntu 风格的 `nodejs` 二进位，会自动回退到它。显式指定的执行期（`bun`、绝对路径、`node --flags`）永不回退，找不到就报错。
- 执行期不在 `PATH` 中时直接报错，并提示用 `--js-runtime` 换一个（不会静默退回 node）。
- ESM 导入（`# npm/…`）对这些执行期同样有效：`no run --js` 会把专案的 `node_modules` 链接进暂存目录，node/bun/deno 都按相同规则解析。

## 平台标注

JS 后端引入两个额外的平台标注键：

| 键 | 匹配场景 |
| --- | --- |
| `#{js}` | JS 后端（Node.js 和浏览器） |
| `#{js-browser}` | 浏览器模式（搭配 `--browser`） |

```no
; 仅在 JS 后端编译时保留
#{js}
js-helper = () {
    print('JS only code')
}

; 仅在浏览器模式保留
#{js-browser}
print('running in browser mode')

; 仅在原生后端保留（JS 编译时排除）
#{mac-arm64}
print('running on macOS ARM64')
```

## 内建函数映射

JS 后端将 Nolang 内建函数映射为 JavaScript 等价物：

| Nolang | JavaScript | 说明 |
| --- | --- | --- |
| `print(x)` | `console.log(x)` | 自动换行 |
| `eprint(x)` | `console.error(x)` | 输出到 stderr |
| `format(...)` | 字串拼接 | v1 简化：以 `"" +` 拼接 |
| `len(x)` | `x.length` | 字串和阵列适用 |
| `with-len(n)` | `new Array(n)` | 建立指定长度阵列 |

## JS 后端标准库

`src/js/` 目录提供 JS 后端专用模组，均带有 `#{js}` 平台标注：

### 浏览器 API

#### `js/dom` — DOM 操作

```no
# js/dom

; 建立元素
el = dom.create-element('div')
heading = dom.create-element('h2')

; 查询元素
el = dom.get-element-by-id('my-id')
el = dom.query-selector('.my-class')

; 取得 body
body = dom.body()

; 元素方法（由 builtin.go 映射）
el.set-text('Hello')
el.set-style('color', 'red')
el.set-attr('data-id', '42')
el.append-child(child)
```

#### `js/canvas` — Canvas 2D 绘图

```no
# js/canvas

canvasEl = dom.create-element('canvas')
canvasEl.set-attr('width', '240')
canvasEl.set-attr('height', '140')
body.append-child(canvasEl)

ctx = canvas.get-context-2d(canvasEl)
ctx.set-fill('red')
ctx.fill-rect(10, 10, 60, 60)
ctx.set-stroke('orange')
ctx.begin-path()
ctx.move-to(120, 80)
ctx.line-to(80, 130)
ctx.line-to(160, 130)
ctx.fill()
ctx.stroke()
```

#### `js/events` — 事件处理

```no
# js/events

btn = dom.create-element('button')
btn.set-text('Click me')
body.append-child(btn)

; 匿名回调
events.on-click(btn, () {
    print('button was clicked!')
})

; 页面载入
events.on-load(() {
    print('page loaded')
})
```

#### `js/storage` — localStorage

```no
# js/storage

storage.set-item('key', 'value')
val = storage.get-item('key')
storage.remove-item('key')
storage.clear()
```

#### `js/location` — Location API

```no
# js/location

href = location.href()
search = location.search()
path = location.path()
location.redirect('https://example.com')
```

#### `js/history` — History API

```no
# js/history

history.back()
history.forward()
history.push('/page2')
n = history.length()
```

#### `js/animation` — 动画帧

```no
# js/animation

id = animation.request-frame(() {

    ; 每帧回调
    print('frame')
})
animation.cancel-frame(id)
```

### Node.js API

#### `js/fs-read-file` / `js/fs-write-file` — 档案读写

```no
# js/fs-read-file
# js/fs-write-file

data = fs-read-file('input.txt')
fs-write-file('output.txt', data)
```

#### `js/http-fetch` — HTTP 获取

```no
# js/http-fetch

data = http-fetch('https://api.example.com/data')
print(data)
```

#### `js/process-exit` — 进程退出

```no
# js/process-exit

process-exit(0)
```

#### `js/fetch` — Fetch API（async）

```no
# js/fetch

; async 获取 URL 数据
data = fetch.async('https://api.example.com/data')
json-data = fetch.json-async('https://api.example.com/data')
```

## 浏览器模式

使用 `--browser` 时，编译器生成：

1. **JS 档案**：编译后的 JavaScript 原始码
2. **HTML 档案**：引用 JS 的 HTML wrapper，包含 `#nolang-output` div

HTML wrapper 特性：

- `print()` 输出重导向至 `<div id="nolang-output">`
- 页面包含基本样式（字型、边框、等宽字型输出区）
- 透过 `<script src="name.js">` 引用编译产物

输出路径预设为 `dist/` 目录。

## 第三方 ESM 模组导入

JS 后端可以直接引入 npm 生态的套件（TypeScript 的 `import` 等价物），**不需要新的 `import` 语句**——复用 `#` use 指令加上保留前缀 `npm/`：

```no
# npm/lodash; 整个模组物件：lodash.get(x, 'a.b')
# npm/lodash.get as get; 单个命名导出：get(x, 'a.b')
# npm/lodash.default as d; 套件的预设导出：d(props)
# npm/@scope/pkg; scoped 套件
# npm/lodash/fp.get as fpg; 子路径导入
```

规则：

- **必须是 JS 后端**。LLVM 与 Direct-WASM 后端遇到 `# npm/...` 直接报错并提示改用 `no build --js`。
- **套件必须宣告在 `package.noml`** 的 `dependencies`（或 `devDependencies`）里，否则建置失败。子路径导入（`lodash/fp`）只要父套件（`lodash`）在清单中即可。
- **短名限定的用法保持不变**：`# npm/lodash` 之后即可写 `lodash.get(...)`；具名导入（`as get`）则直接呼叫 `get(...)`。
- **v1 无型别直通**：导入的绑定不携带 Nolang 型别（checker 零改动），因此不做签名检查；导入项与本地同名函式碰撞时，导入侧自动改名（尾码 `$esm`）。
- **与内建模组重名时以导入为准**：`# npm/math` 之后 `math.sin(x)` 指向 npm 的 `math` 套件而非 `Math.sin`。

### Node.js 运行

产物是标准 ES module（import 陈述式 + `import` 只能出现在 module 范畴）。`no build --js` 会在产物旁补一个 `package.json`（`{"type": "module"}`，npm 惯例）让 node 以 ESM 执行；若该目录已有不是 ESM 的 `package.json`，会提示改用 `-o out.mjs`。

```bash
npm install lodash                    # 先安装套件
no build --js main.no                 # 产出 dist/main.js（+ dist/package.json）
node dist/main.js                     # 以 ESM 执行
no run --js main.no                   # 建置到暂存目录后自动以 node 执行
```

`no run --js` 会把专案的 `node_modules` 连结进暂存目录（node 从脚本所在目录向上解析套件），缺少套件时直接报错并提示 `npm install lodash`；`no build --js` 对缺包只给 warning（产物本身仍然合法，只在执行期才需要套件）。

### 浏览器运行

`--browser` 下，产物 HTML 变成 `<script type="module">` 并带一张 import map，把裸限定符解析到 CDN（预设 `https://esm.sh`）：

```html
<script type="importmap">
{"imports": {"lodash": "https://esm.sh/lodash"}}
</script>
<script type="module" src="main.js"></script>
```

换 CDN 的方式（优先级由高到低）：

1. `no build --js --browser --esm-cdn https://cdn.jsdelivr.net/npm main.no`
2. `package.noml` 的 `compiler: { esm-cdn: 'https://cdn.jsdelivr.net/npm' }`

因为 `<script type="module">` 在 `file://` 下会被 CORS 拦截，`no run --js --browser` 会在 `127.0.0.1` 的临时埠起一个一次性静态服务再打开浏览器（Ctrl+C 停止）。完整范例见 `example/esm-imports/`。

## 原生包：WASM 与 FFI

JS 后端除了汇入 JS 套件，还可呼叫原生包（C ABI 共享函式库／WASM）。方式不新增文法，而是在既有宣告上加注解选择呼叫路径。现有的 C 函式宣告（`#{c}` + `name = (params) (results)`，见 FFI 章）就是原生介面，JS 后端原本直接跳过；加上 `bind` 注解后，会为每个 `#{c}` extern 产生呼叫绑定。

### 注解

`bind` 是一条**建置策略**：可写在 `# import` 行上方（独立成行）或行尾，也可直接写在 `#{c}` extern 上方的注解群组内。

| 键 | 取值 | 说明 |
| --- | --- | --- |
| `bind` | `auto` / `ffi` / `wasm` | 呼叫方式；一个 JS 建置只允许一种策略 |
| `wasm` | 字串 | WASM 模组 specifier；`bind=wasm` 或浏览器 `auto` 必填 |
| `lib` | 字串 | 覆盖 FFI 库名（预设取 `compiler.link-libs`） |
| `init` | 识别字 | WASM 初始化导出名，预设 `default` |

```no
; libm 的 pow：FFI 走 koffi，浏览器需另给 wasm= 的 specifier
#{c, bind=auto}
pow = (x f64, y f64) (r f64)
```

### 目标 × 策略矩阵

| 目标 | `bind=ffi` | `bind=wasm` | `bind=auto` |
| --- | --- | --- | --- |
| node / bun / deno | koffi 绑定 extern | WASM 导入 + ready | 有 `link-libs` / `lib=` → FFI；否则需 `wasm=` → WASM；否则报错 |
| browser | **建置期报错**（koffi 无法在浏览器运行） | WASM 导入 + ready | 有 `wasm=` → WASM；否则报错 |

附加规则：`bind=ffi` 需要至少一个 `#{c}` extern，否则报错；既无 `link-libs` 又无 `lib=` 也报错。多处出现互相冲突的 `bind` / `wasm` 策略时建置失败。

### FFI（本地 / node）

FFI 走 [`koffi`](https://koffi.dev) 真 FFI：`koffi.load(<库>)` + `lib.func(symbol, ret, args)`。库来源是 `package.noml` 的 `compiler.link-libs`（`lib=` 可覆盖）；多个库依序尝试，命中即用。每个 `#{c}` extern 产生一个绑定，本地名为 `jsIdent(name)`（`_sqlite3-open` → `_sqlite3Open`），C 符号名为去前导 `_`、`-` 换 `_`（`sqlite3_open`）。型别对映：

| Nolang | koffi | Nolang | koffi |
| --- | --- | --- | --- |
| `i8` / `u8` / `byte` | `int8` / `uint8` | `i32` / `u32` | `int` / `uint` |
| `i16` / `u16` | `int16` / `uint16` | `i64` / `u64` | `int64` / `uint64` |
| `f32` / `f64` | `float` / `double` | `str` | `str` |
| `bool` | `bool` | `void` / 无回传 | `void` |
| `*T` | `koffi.pointer(<T>)` | `**T` | `koffi.pointer(koffi.pointer(<T>))` |

其它（切片／阵列／结构体）v1 一律 `"void *"` 兜底。`koffi` 以合成 ESM 依赖加入：`no build` 缺包只 warning，`no run --js` 缺包直接报错并提示 `npm install koffi`（与 ESM 策略一致）。koffi 是 CJS，走 `import * as` + `.default ??` 互操作层，不用裸 named import。

### WASM（浏览器 / `bind=wasm`）

WASM 模式导入指定的 `.wasm` 模组，对每个 `#{c}` extern 产生**同名转发**（`_sqlite3-open` → wasm 导出 `sqlite3_open`）。初始化产生一张 ready Promise（**不 await**）：同步初始化的模组立即就绪，非同步的会在就绪时翻转 state，并把 ready Promise 暴露在 `globalThis.__nolang_wasm_ready[<短名>]` 供外层 JS await。呼叫早于就绪会抛明确错误。

WASM specifier 有两种形态，其余规则依形态而异：

| 形态 | 例 | `dependencies` 宣告 | 浏览器 import map | `no run --js` |
| --- | --- | --- | --- | --- |
| bare（npm 套件） | `@sqlite.org/sqlite-wasm` | **必填** | 经 `--esm-cdn`（预设 `https://esm.sh`）解析 | 需 `npm install` |
| 相对（本地档案） | `./math-wasm.mjs` | **不需要** | **不进 map**（浏览器本就不重映射相对 specifier） | 自动把该档（及其自身的相对导入）链接进暂存目录 |

相对形态适用「模组就在专案里」：`no build --js` 的产物与模组同目录即可执行，`no run --js` 会帮你把模组连结进暂存 bundle 目录；浏览器目标则要把模组一并复制到 `dist/`（`import` 相对文件位置解析）。

### 完整范例

- `example/native-libm/`：`#{bind = auto, c}` 宣告 libm 的 `pow`，`no run --js main.no` 印出 `1024`（`no run` 需先在该目录 `npm install` 装 `koffi`）。
- `example/native-wasm/`：本地 WASM 模组（`math.wasm` + `math-wasm.mjs`，零 npm 依赖），`no run --js main.no` 印出 `42.25` 与 `42`；`no build --js --browser main.no` 的 import map 不含相对 specifier。

### 已知限制（v1）
- WASM 只做**同名导出转发**，不产生 API 适配层（例如 `better-sqlite3` → `sql.js` 这类语意差异需自行提供 nolang wrapper 模组）。
- 策略是**程式级**的：一个 JS 建置只允许一种 `bind` 策略；多原生库的按模组关联留待后续。
- FFI 只绑定 C ABI 共享库；Node 原生 addon（`.node`）请用 `bind=wasm` 或一般 ESM 导入。

## 完整示例

```no
; main.no — 浏览器应用示范
# js/dom
# js/canvas
# js/events
# js/storage

print('=== Nolang Browser Demo ===')

; DOM: 建立标题并附加到 body
heading = dom.create-element('h2')
heading.set-text('Hello from Nolang!')
body = dom.body()
body.append-child(heading)

; DOM: 建立按钮
btn = dom.create-element('button')
btn.set-text('Click me')
btn.set-style('margin', '8px')
body.append-child(btn)

; 事件: 按钮点击
events.on-click(btn, () {
    print('button was clicked!')
})

; Canvas: 绘制矩形
canvasEl = dom.create-element('canvas')
canvasEl.set-attr('width', '240')
canvasEl.set-attr('height', '140')
body.append-child(canvasEl)

ctx = canvas.get-context-2d(canvasEl)
ctx.set-fill('red')
ctx.fill-rect(10, 10, 60, 60)
ctx.set-fill('blue')
ctx.fill-rect(80, 10, 60, 60)

; localStorage: 保存和读取
storage.set-item('greeting', 'Hello from localStorage')
g = storage.get-item('greeting')
print('stored:', g)

; 迭代
nums = [10, 20, 30]
i <- nums: {
    print('elem:', i)
}

; 平台分支
#{js-browser}
print('running in browser mode')

print('=== done ===')
```

构建：

```bash
no build --js --browser main.no
# 输出: dist/main.js, dist/main.html
# 在浏览器中打开 dist/main.html
```
