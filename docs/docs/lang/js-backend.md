---
sidebar_position: 8
---

# JS 後端

Nolang 支援將 `.no` 原始碼直接編譯為 JavaScript，無需 LLVM 工具鏈。JS 後端採用**型別擦除**（type erasure）策略：所有 Nolang 型別標註在 JS 輸出中不保留，僅生成執行時行為。

## 快速開始

```bash
# 編譯為 JS（輸出至 dist/<name>.js）
no build --js main.no

# 瀏覽器模式：生成 JS + HTML wrapper
no build --js --browser main.no

# 運行：編譯為 JS 並以 node 執行
no run --js main.no

# 瀏覽器模式：編譯並在預設瀏覽器中打開
no run --js --browser main.no
```

## 設計原理

JS 後端的核心設計：

- **型別擦除**：JS 為動態型別，Nolang 的 `int`/`str`/`bool`/`vec[T]`/`[N]T`/`?T` 等型別標註在 JS 輸出中完全不保留
- **無需 LLVM**：直接從 AST 發射 JavaScript 原始碼，不依賴 clang/LLVM 工具鏈
- **雙環境**：支援 Node.js（預設）和瀏覽器（`--browser`）兩種目標環境
- **平台標註**：`#{js}` 和 `#{js-browser}` 控制代碼的平台可見性

實作位於 `src/build/js/` 目錄：

| 檔案 | 職責 |
| --- | --- |
| `generator.go` | AST → JavaScript codegen 主邏輯 |
| `expr.go` | 表達式生成 |
| `stmt.go` | 語句生成 |
| `builtin.go` | 內建函數映射（print→console.log 等） |
| `html_wrapper.go` | 瀏覽器模式 HTML 模板 |

## 命令列參數

| 參數 | 說明 |
| --- | --- |
| `--js` | 使用 JS 後端（發射 JavaScript，繞過 LLVM） |
| `--browser` | 生成瀏覽器導向輸出（HTML + JS，需搭配 `--js`） |
| `--esm-cdn <url>` | 覆蓋瀏覽器 ESM import map 的 CDN 前綴（預設 `https://esm.sh`） |
| `--js-runtime <cmd>` | `no run --js` 使用的 JS 執行期（預設 `node`；可換 `bun`、`deno` 等，允許附帶參數） |
| `-o <path>` | 指定輸出路徑 |

### 構建

```bash
no build --js main.no                    # 輸出 dist/main.js
no build --js -o app.js main.no          # 指定輸出路徑
no build --js --browser main.no          # 輸出 dist/main.js + dist/main.html
```

### 運行

```bash
no run --js main.no                      # 編譯為 JS 後以 node 執行
no run --js --browser main.no            # 編譯為瀏覽器 JS + HTML 並打開瀏覽器
```

#### 選擇 JS 執行期

`--js-runtime` 指定執行產物的執行期，**預設 `node`**（Node.js）。任何能跑 ES module 的執行期都可以：

```bash
no run --js --js-runtime bun main.no             # Bun
no run --js --js-runtime deno run main.no        # Deno（首個 token 是可執行檔，其餘是腳本前的參數）
no run --js --js-runtime "deno run --allow-read" main.no
no run --js --js-runtime "node --enable-source-maps" main.no
no run --js --js-runtime /opt/js/bin/node main.no   # 指定絕對路徑
```

- 規格的第一個空白分隔 token 是可執行檔，其餘 token 會排在腳本路徑之前；程式本身的參數照舊寫在 `.no` 檔名之後。
- 預設（未指定 `--js-runtime`）使用 `node`；若系統只有 Debian/Ubuntu 風格的 `nodejs` 二進位，會自動回退到它。顯式指定的執行期（`bun`、絕對路徑、`node --flags`）永不回退，找不到就報錯。
- 執行期不在 `PATH` 中時直接報錯，並提示用 `--js-runtime` 換一個（不會靜默退回 node）。
- ESM 導入（`# npm/…`）對這些執行期同樣有效：`no run --js` 會把專案的 `node_modules` 鏈接進暫存目錄，node/bun/deno 都按相同規則解析。

## 平台標註

JS 後端引入兩個額外的平台標註鍵：

| 鍵 | 匹配場景 |
| --- | --- |
| `#{js}` | JS 後端（Node.js 和瀏覽器） |
| `#{js-browser}` | 瀏覽器模式（搭配 `--browser`） |

```no
; 僅在 JS 後端編譯時保留
#{js}
js-helper = () {
    print('JS only code')
}

; 僅在瀏覽器模式保留
#{js-browser}
print('running in browser mode')

; 僅在原生後端保留（JS 編譯時排除）
#{mac-arm64}
print('running on macOS ARM64')
```

## 內建函數映射

JS 後端將 Nolang 內建函數映射為 JavaScript 等價物：

| Nolang | JavaScript | 說明 |
| --- | --- | --- |
| `print(x)` | `console.log(x)` | 自動換行 |
| `eprint(x)` | `console.error(x)` | 輸出到 stderr |
| `format(...)` | 字串拼接 | v1 簡化：以 `"" +` 拼接 |
| `len(x)` | `x.length` | 字串和陣列適用 |
| `with-len(n)` | `new Array(n)` | 建立指定長度陣列 |

## JS 後端標準庫

`src/js/` 目錄提供 JS 後端專用模組，均帶有 `#{js}` 平台標註：

### 瀏覽器 API

#### `js/dom` — DOM 操作

```no
# js/dom

; 建立元素
el = dom.create-element('div')
heading = dom.create-element('h2')

; 查詢元素
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

#### `js/canvas` — Canvas 2D 繪圖

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

#### `js/events` — 事件處理

```no
# js/events

btn = dom.create-element('button')
btn.set-text('Click me')
body.append-child(btn)

; 匿名回調
events.on-click(btn, () {
    print('button was clicked!')
})

; 頁面載入
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

#### `js/animation` — 動畫幀

```no
# js/animation

id = animation.request-frame(() {
    ; 每幀回調
    print('frame')
})
animation.cancel-frame(id)
```

### Node.js API

#### `js/fs-read-file` / `js/fs-write-file` — 檔案讀寫

```no
# js/fs-read-file
# js/fs-write-file

data = fs-read-file('input.txt')
fs-write-file('output.txt', data)
```

#### `js/http-fetch` — HTTP 獲取

```no
# js/http-fetch

data = http-fetch('https://api.example.com/data')
print(data)
```

#### `js/process-exit` — 進程退出

```no
# js/process-exit

process-exit(0)
```

#### `js/fetch` — Fetch API（async）

```no
# js/fetch

; async 獲取 URL 數據
data = fetch.async('https://api.example.com/data')
json-data = fetch.json-async('https://api.example.com/data')
```

## 瀏覽器模式

使用 `--browser` 時，編譯器生成：

1. **JS 檔案**：編譯後的 JavaScript 原始碼
2. **HTML 檔案**：引用 JS 的 HTML wrapper，包含 `#nolang-output` div

HTML wrapper 特性：

- `print()` 輸出重導向至 `<div id="nolang-output">`
- 頁面包含基本樣式（字型、邊框、等寬字型輸出區）
- 透過 `<script src="name.js">` 引用編譯產物

輸出路徑預設為 `dist/` 目錄。

## 第三方 ESM 模組導入

JS 後端可以直接引入 npm 生態的套件（TypeScript 的 `import` 等價物），**不需要新的 `import` 語句**——複用 `#` use 指令加上保留前綴 `npm/`：

```no
# npm/lodash                ; 整個模組物件：lodash.get(x, 'a.b')
# npm/lodash.get as get     ; 單個命名導出：get(x, 'a.b')
# npm/lodash.default as d   ; 套件的預設導出：d(props)
# npm/@scope/pkg            ; scoped 套件
# npm/lodash/fp.get as fpg  ; 子路徑導入
```

規則：

- **必須是 JS 後端**。LLVM 與 Direct-WASM 後端遇到 `# npm/...` 直接報錯並提示改用 `no build --js`。
- **套件必須宣告在 `package.jsonc`** 的 `dependencies`（或 `devDependencies`）裡，否則建置失敗。子路徑導入（`lodash/fp`）只要父套件（`lodash`）在清單中即可。
- **短名限定的用法保持不變**：`# npm/lodash` 之後即可寫 `lodash.get(...)`；具名導入（`as get`）則直接呼叫 `get(...)`。
- **v1 無型別直通**：導入的綁定不攜帶 Nolang 型別（checker 零改動），因此不做簽名檢查；導入項與本地同名函式碰撞時，導入側自動改名（尾碼 `$esm`）。
- **與內建模組重名時以導入為準**：`# npm/math` 之後 `math.sin(x)` 指向 npm 的 `math` 套件而非 `Math.sin`。

### Node.js 運行

產物是標準 ES module（import 陳述式 + `import` 只能出現在 module 範疇）。`no build --js` 會在產物旁補一個 `package.json`（`{"type": "module"}`，npm 慣例）讓 node 以 ESM 執行；若該目錄已有不是 ESM 的 `package.json`，會提示改用 `-o out.mjs`。

```bash
npm install lodash                    # 先安裝套件
no build --js main.no                 # 產出 dist/main.js（+ dist/package.json）
node dist/main.js                     # 以 ESM 執行
no run --js main.no                   # 建置到暫存目錄後自動以 node 執行
```

`no run --js` 會把專案的 `node_modules` 連結進暫存目錄（node 從腳本所在目錄向上解析套件），缺少套件時直接報錯並提示 `npm install lodash`；`no build --js` 對缺包只給 warning（產物本身仍然合法，只在執行期才需要套件）。

### 瀏覽器運行

`--browser` 下，產物 HTML 變成 `<script type="module">` 並帶一張 import map，把裸限定符解析到 CDN（預設 `https://esm.sh`）：

```html
<script type="importmap">
{"imports": {"lodash": "https://esm.sh/lodash"}}
</script>
<script type="module" src="main.js"></script>
```

換 CDN 的方式（優先級由高到低）：

1. `no build --js --browser --esm-cdn https://cdn.jsdelivr.net/npm main.no`
2. `package.jsonc` 的 `"compiler": { "esm-cdn": "https://cdn.jsdelivr.net/npm" }`

因為 `<script type="module">` 在 `file://` 下會被 CORS 攔截，`no run --js --browser` 會在 `127.0.0.1` 的臨時埠起一個一次性靜態服務再打開瀏覽器（Ctrl+C 停止）。完整範例見 `example/esm-imports/`。

## 原生包：WASM 與 FFI

JS 後端除了匯入 JS 套件，還可呼叫原生包（C ABI 共享函式庫／WASM）。方式不新增文法，而是在既有宣告上加註解選擇呼叫路徑。現有的 C 函式宣告（`#{c}` + `name = (params) (results)`，見 FFI 章）就是原生介面，JS 後端原本直接跳過；加上 `bind` 註解後，會為每個 `#{c}` extern 產生呼叫綁定。

### 註解

`bind` 是一條**建置策略**：可寫在 `# import` 行上方（獨立成行）或行尾，也可直接寫在 `#{c}` extern 上方的註解群組內。

| 鍵 | 取值 | 說明 |
| --- | --- | --- |
| `bind` | `auto` / `ffi` / `wasm` | 呼叫方式；一個 JS 建置只允許一種策略 |
| `wasm` | 字串 | WASM 模組 specifier；`bind=wasm` 或瀏覽器 `auto` 必填 |
| `lib` | 字串 | 覆蓋 FFI 庫名（預設取 `compiler.link-libs`） |
| `init` | 識別字 | WASM 初始化導出名，預設 `default` |

```no
; libm 的 pow：FFI 走 koffi，瀏覽器需另給 wasm= 的 specifier
#{bind = auto, c}
pow = (x f64, y f64) (r f64)
```

### 目標 × 策略矩陣

| 目標 | `bind=ffi` | `bind=wasm` | `bind=auto` |
| --- | --- | --- | --- |
| node / bun / deno | koffi 綁定 extern | WASM 導入 + ready | 有 `link-libs` / `lib=` → FFI；否則需 `wasm=` → WASM；否則報錯 |
| browser | **建置期報錯**（koffi 無法在瀏覽器運行） | WASM 導入 + ready | 有 `wasm=` → WASM；否則報錯 |

附加規則：`bind=ffi` 需要至少一個 `#{c}` extern，否則報錯；既無 `link-libs` 又無 `lib=` 也報錯。多處出現互相衝突的 `bind` / `wasm` 策略時建置失敗。

### FFI（本地 / node）

FFI 走 [`koffi`](https://koffi.dev) 真 FFI：`koffi.load(<庫>)` + `lib.func(symbol, ret, args)`。庫來源是 `package.jsonc` 的 `compiler.link-libs`（`lib=` 可覆蓋）；多個庫依序嘗試，命中即用。每個 `#{c}` extern 產生一個綁定，本地名為 `jsIdent(name)`（`_sqlite3-open` → `_sqlite3Open`），C 符號名為去前導 `_`、`-` 換 `_`（`sqlite3_open`）。型別對映：

| Nolang | koffi | Nolang | koffi |
| --- | --- | --- | --- |
| `i8` / `u8` / `byte` | `int8` / `uint8` | `i32` / `u32` | `int` / `uint` |
| `i16` / `u16` | `int16` / `uint16` | `i64` / `u64` | `int64` / `uint64` |
| `f32` / `f64` | `float` / `double` | `str` | `str` |
| `bool` | `bool` | `void` / 無回傳 | `void` |
| `*T` | `koffi.pointer(<T>)` | `**T` | `koffi.pointer(koffi.pointer(<T>))` |

其它（切片／陣列／結構體）v1 一律 `"void *"` 兜底。`koffi` 以合成 ESM 依賴加入：`no build` 缺包只 warning，`no run --js` 缺包直接報錯並提示 `npm install koffi`（與 ESM 策略一致）。koffi 是 CJS，走 `import * as` + `.default ??` 互操作層，不用裸 named import。

### WASM（瀏覽器 / `bind=wasm`）

WASM 模式導入指定的 `.wasm` 模組，對每個 `#{c}` extern 產生**同名轉發**（`_sqlite3-open` → wasm 導出 `sqlite3_open`）。初始化產生一張 ready Promise（**不 await**）：同步初始化的模組立即就緒，非同步的會在就緒時翻轉 state，並把 ready Promise 暴露在 `globalThis.__nolang_wasm_ready[<短名>]` 供外層 JS await。呼叫早於就緒會拋明確錯誤。

WASM specifier 有兩種形態，其餘規則依形態而異：

| 形態 | 例 | `dependencies` 宣告 | 瀏覽器 import map | `no run --js` |
| --- | --- | --- | --- | --- |
| bare（npm 套件） | `@sqlite.org/sqlite-wasm` | **必填** | 經 `--esm-cdn`（預設 `https://esm.sh`）解析 | 需 `npm install` |
| 相對（本地檔案） | `./math-wasm.mjs` | **不需要** | **不進 map**（瀏覽器本就不重映射相對 specifier） | 自動把該檔（及其自身的相對導入）鏈接進暫存目錄 |

相對形態適用「模組就在專案裡」：`no build --js` 的產物與模組同目錄即可執行，`no run --js` 會幫你把模組連結進暫存 bundle 目錄；瀏覽器目標則要把模組一併複製到 `dist/`（`import` 相對文件位置解析）。

### 完整範例

- `example/native-libm/`：`#{bind = auto, c}` 宣告 libm 的 `pow`，`no run --js main.no` 印出 `1024`（`no run` 需先在該目錄 `npm install` 裝 `koffi`）。
- `example/native-wasm/`：本地 WASM 模組（`math.wasm` + `math-wasm.mjs`，零 npm 依賴），`no run --js main.no` 印出 `42.25` 與 `42`；`no build --js --browser main.no` 的 import map 不含相對 specifier。

### 已知限制（v1）
- WASM 只做**同名導出轉發**，不產生 API 適配層（例如 `better-sqlite3` → `sql.js` 這類語意差異需自行提供 nolang wrapper 模組）。
- 策略是**程式級**的：一個 JS 建置只允許一種 `bind` 策略；多原生庫的按模組關聯留待後續。
- FFI 只綁定 C ABI 共享庫；Node 原生 addon（`.node`）請用 `bind=wasm` 或一般 ESM 導入。

## 完整示例

```no
; main.no — 瀏覽器應用示範
# js/dom
# js/canvas
# js/events
# js/storage

print('=== Nolang Browser Demo ===')

; DOM: 建立標題並附加到 body
heading = dom.create-element('h2')
heading.set-text('Hello from Nolang!')
body = dom.body()
body.append-child(heading)

; DOM: 建立按鈕
btn = dom.create-element('button')
btn.set-text('Click me')
btn.set-style('margin', '8px')
body.append-child(btn)

; 事件: 按鈕點擊
events.on-click(btn, () {
    print('button was clicked!')
})

; Canvas: 繪製矩形
canvasEl = dom.create-element('canvas')
canvasEl.set-attr('width', '240')
canvasEl.set-attr('height', '140')
body.append-child(canvasEl)

ctx = canvas.get-context-2d(canvasEl)
ctx.set-fill('red')
ctx.fill-rect(10, 10, 60, 60)
ctx.set-fill('blue')
ctx.fill-rect(80, 10, 60, 60)

; localStorage: 保存和讀取
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

構建：

```bash
no build --js --browser main.no
# 輸出: dist/main.js, dist/main.html
# 在瀏覽器中打開 dist/main.html
```
