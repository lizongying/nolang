---
sidebar_position: 8
---

# JS バックエンド

Nolang は `.no` ソースコードを LLVM ツールチェーンなしで JavaScript へ直接コンパイルできます。JS バックエンドは**型消去**（type erasure）の戦略を採用します：すべての Nolang の型注釈は JS 出力に残らず、実行時の振る舞いのみが生成されます。

## クイックスタート

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

JS バックエンドの核となる設計：

- **型消去**：JS は動的型付けであり、Nolang の `int`/`str`/`bool`/`vec[T]`/`[N]T`/`?T` などの型注釈は JS 出力に一切残りません
- **LLVM 不要**：AST から直接 JavaScript ソースコードを発行し、clang/LLVM ツールチェーンに依存しません
- **二つの環境**：Node.js（デフォルト）とブラウザ（`--browser`）の二つのターゲット環境をサポート
- **プラットフォーム注釈**：`#{js}` と `#{js-browser}` がコードのプラットフォーム可視性を制御

実装は `src/build/js/` ディレクトリにあります：

| ファイル | 役割 |
| --- | --- |
| `generator.go` | AST → JavaScript codegen の主ロジック |
| `expr.go` | 式の生成 |
| `stmt.go` | 文の生成 |
| `builtin.go` | 組み込み関数のマッピング（print→console.log など） |
| `html_wrapper.go` | ブラウザモードの HTML テンプレート |

## コマンドライン引数

| 引数 | 説明 |
| --- | --- |
| `--js` | JS バックエンドを使用（JavaScript を発行し、LLVM をバイパス） |
| `--browser` | ブラウザ向けの出力を生成（HTML + JS、`--js` と併用が必要） |
| `--esm-cdn <url>` | ブラウザの ESM import map の CDN プレフィックスを上書き（デフォルト `https://esm.sh`） |
| `--js-runtime <cmd>` | `no run --js` が使用する JS ランタイム（デフォルト `node`；`bun`、`deno` などに変更可、引数の付与も可） |
| `-o <path>` | 出力パスを指定 |

### ビルド

```bash
no build --js main.no                    # 輸出 dist/main.js
no build --js -o app.js main.no          # 指定輸出路徑
no build --js --browser main.no          # 輸出 dist/main.js + dist/main.html
```

### 実行

```bash
no run --js main.no                      # 編譯為 JS 後以 node 執行
no run --js --browser main.no            # 編譯為瀏覽器 JS + HTML 並打開瀏覽器
```

#### JS ランタイムの選択

`--js-runtime` は生成物を実行するランタイムを指定し、**デフォルトは `node`**（Node.js）です。ES module を実行できるランタイムであれば何でも構いません：

```bash
no run --js --js-runtime bun main.no             # Bun
no run --js --js-runtime deno run main.no        # Deno（首個 token 是可執行檔，其餘是腳本前的參數）
no run --js --js-runtime "deno run --allow-read" main.no
no run --js --js-runtime "node --enable-source-maps" main.no
no run --js --js-runtime /opt/js/bin/node main.no   # 指定絕對路徑
```

- 仕様上、最初の空白区切りトークンが実行可能ファイルで、残りのトークンはスクリプトパスの前に置かれます；プログラム自身の引数はこれまでどおり `.no` ファイル名の後に記述します。
- デフォルト（`--js-runtime` 未指定）では `node` を使用します；システムに Debian/Ubuntu 形式の `nodejs` バイナリしかない場合は、自動的にそちらへフォールバックします。明示的に指定したランタイム（`bun`、絶対パス、`node --flags`）は決してフォールバックせず、見つからなければエラーになります。
- ランタイムが `PATH` にない場合は即座にエラーとなり、`--js-runtime` で別のものを指定するよう通知します（黙って node に戻ることはありません）。
- ESM インポート（`# npm/…`）もこれらのランタイムで同様に機能します：`no run --js` はプロジェクトの `node_modules` を一時ディレクトリへリンクし、node/bun/deno はいずれも同じ規則で解決します。

## プラットフォーム注釈

JS バックエンドは二つの追加のプラットフォーム注釈キーを導入します：

| キー | 一致するシナリオ |
| --- | --- |
| `#{js}` | JS バックエンド（Node.js とブラウザ） |
| `#{js-browser}` | ブラウザモード（`--browser` と併用） |

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

## 組み込み関数のマッピング

JS バックエンドは Nolang の組み込み関数を JavaScript の等価物へマッピングします：

| Nolang | JavaScript | 説明 |
| --- | --- | --- |
| `print(x)` | `console.log(x)` | 自動的に改行 |
| `eprint(x)` | `console.error(x)` | stderr へ出力 |
| `format(...)` | 文字列連結 | v1 では簡略化：`"" +` で連結 |
| `len(x)` | `x.length` | 文字列と配列に適用 |
| `with-len(n)` | `new Array(n)` | 指定長の配列を作成 |

## JS バックエンド標準ライブラリ

`src/js/` ディレクトリは JS バックエンド専用のモジュールを提供し、いずれも `#{js}` のプラットフォーム注釈を持ちます：

### ブラウザ API

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

#### `js/canvas` — Canvas 2D 描画

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

#### `js/events` — イベント処理

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

#### `js/animation` — アニメーションフレーム

```no
# js/animation

id = animation.request-frame(() {

    ; 每幀回調
    print('frame')
})
animation.cancel-frame(id)
```

### Node.js API

#### `js/fs-read-file` / `js/fs-write-file` — ファイル読み書き

```no
# js/fs-read-file
# js/fs-write-file

data = fs-read-file('input.txt')
fs-write-file('output.txt', data)
```

#### `js/http-fetch` — HTTP 取得

```no
# js/http-fetch

data = http-fetch('https://api.example.com/data')
print(data)
```

#### `js/process-exit` — プロセス終了

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

## ブラウザモード

`--browser` を使用すると、コンパイラは次を生成します：

1. **JS ファイル**：コンパイル後の JavaScript ソースコード
2. **HTML ファイル**：JS を参照する HTML wrapper で、`#nolang-output` の div を含む

HTML wrapper の特徴：

- `print()` の出力は `<div id="nolang-output">` へリダイレクト
- ページには基本スタイル（フォント、枠線、等幅フォントの出力領域）が含まれる
- `<script src="name.js">` でコンパイル生成物を参照

出力パスのデフォルトは `dist/` ディレクトリです。

## サードパーティ ESM モジュールのインポート

JS バックエンドは npm エコシステムのパッケージを直接取り込めます（TypeScript の `import` に相当します）。**新しい `import` 文は不要**です——既存の `#` use ディレクティブに予約済みプレフィックス `npm/` を付けて再利用します：

```no
# npm/lodash; 整個模組物件：lodash.get(x, 'a.b')
# npm/lodash.get as get; 單個命名導出：get(x, 'a.b')
# npm/lodash.default as d; 套件的預設導出：d(props)
# npm/@scope/pkg; scoped 套件
# npm/lodash/fp.get as fpg; 子路徑導入
```

ルール：

- **JS バックエンドが必須**です。LLVM バックエンドと Direct-WASM バックエンドは `# npm/...` に遭遇すると即座にエラーとなり、`no build --js` の使用を提案します。
- パッケージは `package.noml` の `dependencies`（または `devDependencies`）に**宣言する必要があり**、そうでなければビルドは失敗します。サブパスインポート（`lodash/fp`）は親パッケージ（`lodash`）が一覧にあれば構いません。
- **短い名前による修飾の用法は変わりません**：`# npm/lodash` の後は `lodash.get(...)` と書けます；名前付きインポート（`as get`）の場合は直接 `get(...)` を呼び出します。
- **v1 では型の透過なし**：インポートされたバインディングは Nolang の型を保持しないため（checker は無変更）、シグネチャ検査は行われません；インポート項目とローカルの同名関数が衝突した場合、インポート側が自動的に名前を変更します（接尾辞 `$esm`）。
- **組み込みモジュールと同名の場合はインポートが優先**：`# npm/math` の後、`math.sin(x)` は `Math.sin` ではなく npm の `math` パッケージを指します。

### Node.js での実行

生成物は標準の ES module です（import 文 + `import` は module スコープにのみ出現できます）。`no build --js` は生成物と同じ場所に `package.json`（`{"type": "module"}`、npm の慣例）を補い、node が ESM として実行できるようにします；そのディレクトリに既に ESM ではない `package.json` がある場合は、`-o out.mjs` の使用を提案します。

```bash
npm install lodash                    # 先安裝套件
no build --js main.no                 # 產出 dist/main.js（+ dist/package.json）
node dist/main.js                     # 以 ESM 執行
no run --js main.no                   # 建置到暫存目錄後自動以 node 執行
```

`no run --js` はプロジェクトの `node_modules` を一時ディレクトリへリンクし（node はスクリプトのあるディレクトリから上方へ向かってパッケージを解決します）、パッケージが不足している場合は即座にエラーとなり `npm install lodash` を提案します；`no build --js` はパッケージ不足に対して warning のみを出し（生成物自体は有効で、パッケージが必要なのは実行時のみです）。

### ブラウザでの実行

`--browser` では、生成物の HTML が `<script type="module">` となり、import map を伴って、ベア指定子を CDN（デフォルト `https://esm.sh`）へ解決します：

```html
<script type="importmap">
{"imports": {"lodash": "https://esm.sh/lodash"}}
</script>
<script type="module" src="main.js"></script>
```

CDN を変更する方法（優先度の高い順）：

1. no build --js --browser --esm-cdn https://cdn.jsdelivr.net/npm main.no
2. `package.noml` の `compiler: { esm-cdn: 'https://cdn.jsdelivr.net/npm' }`

`<script type="module">` は `file://` 下で CORS によってブロックされるため、`no run --js --browser` は `127.0.0.1` の一時ポートに使い捨ての静的サーバーを起動してからブラウザを開きます（Ctrl+C で停止）。完全な例は `example/esm-imports/` を参照してください。

## ネイティブパッケージ：WASM と FFI

JS バックエンドは JS パッケージのインポートに加え、ネイティブパッケージ（C ABI の共有ライブラリ／WASM）も呼び出せます。その方法は新しい文法を追加するのではなく、既存の宣言に注釈を付けて呼び出し経路を選択します。既存の C 関数宣言（`#{c}` + `name = (params) (results)`、FFI 章を参照）がそのままネイティブインターフェースであり、JS バックエンドは元々これを単にスキップしていました；`bind` 注釈を加えると、各 `#{c}` extern に対して呼び出しバインディングが生成されます。

### 注釈

`bind` は**ビルド方針**です：`# import` 行の上（独立した行）または行末に記述でき、`#{c}` extern の上の注釈グループ内に直接書くこともできます。

| キー | 値 | 説明 |
| --- | --- | --- |
| `bind` | `auto` / `ffi` / `wasm` | 呼び出し方法；一つの JS ビルドでは一つの方針のみ許可 |
| `wasm` | 文字列 | WASM モジュールの指定子；`bind=wasm` またはブラウザの `auto` では必須 |
| `lib` | 文字列 | FFI ライブラリ名を上書き（デフォルトは `compiler.link-libs` から取得） |
| `init` | 識別子 | WASM 初期化のエクスポート名、デフォルトは `default` |

```no
; libm 的 pow：FFI 走 koffi，瀏覽器需另給 wasm= 的 specifier
#{c, bind=auto}
pow = (x f64, y f64) (r f64)
```

### ターゲット × 方針マトリクス

| ターゲット | `bind=ffi` | `bind=wasm` | `bind=auto` |
| --- | --- | --- | --- |
| node / bun / deno | koffi が extern をバインド | WASM インポート + ready | `link-libs` / `lib=` があれば → FFI；なければ `wasm=` が必要 → WASM；それもなければエラー |
| browser | **ビルド時にエラー**（koffi はブラウザで動作できません） | WASM インポート + ready | `wasm=` があれば → WASM；なければエラー |

追加ルール：`bind=ffi` には少なくとも一つの `#{c}` extern が必要で、なければエラー；`link-libs` も `lib=` もない場合もエラーです。複数の箇所で互いに矛盾する `bind` / `wasm` 方針が現れるとビルドは失敗します。

### FFI（ローカル / node）

FFI は [`koffi`](https://koffi.dev) による真の FFI を使用します：`koffi.load(<ライブラリ>)` + `lib.func(symbol, ret, args)`。ライブラリの出所は `package.noml` の `compiler.link-libs`（`lib=` で上書き可）です；複数のライブラリは順に試され、見つかったものを使用します。各 `#{c}` extern に対して一つのバインディングが生成され、ローカル名は `jsIdent(name)`（`_sqlite3-open` → `_sqlite3Open`）、C シンボル名は先頭の `_` を除き `-` を `_` に置き換えたもの（`sqlite3_open`）です。型の対応：

| Nolang | koffi | Nolang | koffi |
| --- | --- | --- | --- |
| `i8` / `u8` / `byte` | `int8` / `uint8` | `i32` / `u32` | `int` / `uint` |
| `i16` / `u16` | `int16` / `uint16` | `i64` / `u64` | `int64` / `uint64` |
| `f32` / `f64` | `float` / `double` | `str` | `str` |
| `bool` | `bool` | `void` / 戻り値なし | `void` |
| `*T` | `koffi.pointer(<T>)` | `**T` | `koffi.pointer(koffi.pointer(<T>))` |

その他（スライス／配列／構造体）は v1 ではすべて `"void *"` でフォールバックします。`koffi` は合成 ESM 依存として追加されます：`no build` ではパッケージ不足は warning のみ、`no run --js` では即座にエラーとなり `npm install koffi` を提案します（ESM の方針と同様）。koffi は CJS なので、`import * as` + `.default ??` の相互運用層を使用し、素の named import は使いません。

### WASM（ブラウザ / `bind=wasm`）

WASM モードは指定された `.wasm` モジュールをインポートし、各 `#{c}` extern に対して**同名の転送**を生成します（`_sqlite3-open` → wasm エクスポート `sqlite3_open`）。初期化では ready Promise を一つ生成します（**await しません**）：同期初期化のモジュールは即座に準備完了となり、非同期のものは準備が整った時点で state を反転させ、ready Promise を `globalThis.__nolang_wasm_ready[<短い名前>]` に公開して外側の JS が await できるようにします。準備完了より前に呼び出すと明確なエラーがスローされます。

WASM 指定子には二つの形態があり、それ以外のルールは形態によって異なります：

| 形態 | 例 | `dependencies` 宣言 | ブラウザの import map | `no run --js` |
| --- | --- | --- | --- | --- |
| bare（npm パッケージ） | `@sqlite.org/sqlite-wasm` | **必須** | `--esm-cdn`（デフォルト `https://esm.sh`）経由で解決 | `npm install` が必要 |
| 相対（ローカルファイル） | `./math-wasm.mjs` | **不要** | **map に入れない**（ブラウザは元々相対指定子を再マップしません） | そのファイル（および自身の相対インポート）を自動的に一時ディレクトリへリンク |

相対形態は「モジュールがプロジェクト内にある」場合に適します：`no build --js` の生成物とモジュールが同じディレクトリにあれば実行でき、`no run --js` はモジュールを一時 bundle ディレクトリへリンクします；ブラウザ向けの場合はモジュールも `dist/` へ一緒にコピーする必要があります（`import` は相対的なファイル位置で解決されます）。

### 完全な例

- `example/native-libm/`：`#{bind = auto, c}` で libm の `pow` を宣言し、`no run --js main.no` は `1024` を出力します（`no run` には事前にそのディレクトリで `npm install` により `koffi` をインストールする必要があります）。
- `example/native-wasm/`：ローカル WASM モジュール（`math.wasm` + `math-wasm.mjs`、npm 依存ゼロ）で、`no run --js main.no` は `42.25` と `42` を出力します；`no build --js --browser main.no` の import map には相対指定子は含まれません。

### 既知の制限（v1）
- WASM は**同名エクスポートの転送**のみを行い、API アダプタ層は生成しません（例えば `better-sqlite3` → `sql.js` のような意味の違いには、独自の nolang wrapper モジュールを用意する必要があります）。
- 方針は**プログラム単位**です：一つの JS ビルドでは一つの `bind` 方針のみ許可されます；複数ネイティブライブラリのモジュール単位の関連付けは今後の課題です。
- FFI がバインドするのは C ABI の共有ライブラリのみです；Node のネイティブ addon（`.node`）には `bind=wasm` または通常の ESM インポートを使用してください。

## 完全な例

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

ビルド：

```bash
no build --js --browser main.no
# 輸出: dist/main.js, dist/main.html
# 在瀏覽器中打開 dist/main.html
```
