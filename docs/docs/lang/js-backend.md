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
