---
sidebar_position: 8
---

# JS 백엔드

Nolang는 `.no` 소스 코드를 LLVM 도구 모음 없이 JavaScript로 직접 컴파일할 수 있습니다. JS 백엔드는 **타입 지우기**(type erasure) 방식을 사용합니다. 모든 Nolang 타입 표기는 JS 출력에 남지 않고 런타임 동작만 생성됩니다.

## 빠른 시작

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

## 설계 원리

JS 백엔드의 핵심 설계는 다음과 같습니다.

- **타입 지우기**: JS는 동적 타입 언어이므로 Nolang의 `int`/`str`/`bool`/`vec[T]`/`[N]T`/`?T` 같은 타입 표기가 JS 출력에 전혀 남지 않습니다
- **LLVM 불필요**: AST에서 JavaScript 소스 코드를 직접 생성하며 clang/LLVM 도구 모음에 의존하지 않습니다
- **두 가지 환경**: Node.js(기본값)와 브라우저(`--browser`) 두 대상 환경을 지원합니다
- **플랫폼 표기**: `#{js}`와 `#{js-browser}`가 코드의 플랫폼 가시성을 제어합니다

구현은 `src/build/js/` 디렉터리에 있습니다.

| 파일 | 역할 |
| --- | --- |
| `generator.go` | AST → JavaScript codegen 주요 로직 |
| `expr.go` | 표현식 생성 |
| `stmt.go` | 문장 생성 |
| `builtin.go` | 내장 함수 매핑(print→console.log 등) |
| `html_wrapper.go` | 브라우저 모드 HTML 템플릿 |

## 명령줄 매개변수

| 매개변수 | 설명 |
| --- | --- |
| `--js` | JS 백엔드를 사용합니다(JavaScript를 생성하고 LLVM을 우회합니다) |
| `--browser` | 브라우저용 출력을 생성합니다(HTML + JS, `--js`와 함께 사용해야 함) |
| `--esm-cdn <url>` | 브라우저 ESM import map의 CDN 접두사를 덮어씁니다(기본값 `https://esm.sh`) |
| `--js-runtime <cmd>` | `no run --js`가 사용할 JS 런타임(기본값 `node`. `bun`, `deno` 등으로 바꿀 수 있으며 추가 인자를 붙일 수 있음) |
| `-o <path>` | 출력 경로를 지정합니다 |

### 빌드

```bash
no build --js main.no                    # 輸出 dist/main.js
no build --js -o app.js main.no          # 指定輸出路徑
no build --js --browser main.no          # 輸出 dist/main.js + dist/main.html
```

### 실행

```bash
no run --js main.no                      # 編譯為 JS 後以 node 執行
no run --js --browser main.no            # 編譯為瀏覽器 JS + HTML 並打開瀏覽器
```

#### JS 런타임 선택

`--js-runtime`은 실행 산출물의 런타임을 지정하며, **기본값은 `node`**(Node.js)입니다. ES module을 실행할 수 있는 런타임이면 모두 가능합니다.

```bash
no run --js --js-runtime bun main.no             # Bun
no run --js --js-runtime deno run main.no        # Deno（首個 token 是可執行檔，其餘是腳本前的參數）
no run --js --js-runtime "deno run --allow-read" main.no
no run --js --js-runtime "node --enable-source-maps" main.no
no run --js --js-runtime /opt/js/bin/node main.no   # 指定絕對路徑
```

- 지정값에서 공백으로 구분된 첫 번째 token이 실행 파일이고, 나머지 token은 스크립트 경로 앞에 배치됩니다. 프로그램 자체의 인자는 그대로 `.no` 파일 이름 뒤에 씁니다.
- 기본값(`--js-runtime` 미지정)은 `node`입니다. 시스템에 Debian/Ubuntu 스타일의 `nodejs` 바이너리만 있으면 자동으로 그것으로 대체됩니다. 명시적으로 지정한 런타임(`bun`, 절대 경로, `node --flags`)은 절대 대체되지 않으며, 찾을 수 없으면 오류가 발생합니다.
- 런타임이 `PATH`에 없으면 바로 오류를 내고 `--js-runtime`으로 다른 것을 지정하라고 안내합니다(조용히 node로 되돌아가지 않습니다).
- ESM 가져오기(`# npm/…`)도 이러한 런타임에서 동일하게 동작합니다. `no run --js`가 프로젝트의 `node_modules`를 임시 디렉터리에 링크하며, node/bun/deno 모두 같은 규칙으로 해석합니다.

## 플랫폼 표기

JS 백엔드는 두 가지 추가 플랫폼 표기 키를 도입합니다.

| 키 | 매칭 상황 |
| --- | --- |
| `#{js}` | JS 백엔드(Node.js와 브라우저) |
| `#{js-browser}` | 브라우저 모드(`--browser`와 함께 사용) |

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

## 내장 함수 매핑

JS 백엔드는 Nolang 내장 함수를 JavaScript의 대응 기능으로 매핑합니다.

| Nolang | JavaScript | 설명 |
| --- | --- | --- |
| `print(x)` | `console.log(x)` | 자동으로 줄바꿈 |
| `eprint(x)` | `console.error(x)` | stderr로 출력 |
| `format(...)` | 문자열 연결 | v1 단순화: `"" +`로 연결 |
| `len(x)` | `x.length` | 문자열과 배열에 적용 |
| `with-len(n)` | `new Array(n)` | 지정한 길이의 배열 생성 |

## JS 백엔드 표준 라이브러리

`src/js/` 디렉터리는 JS 백엔드 전용 모듈을 제공하며, 모두 `#{js}` 플랫폼 표기가 붙어 있습니다.

### 브라우저 API

#### `js/dom` — DOM 조작

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

#### `js/canvas` — Canvas 2D 그리기

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

#### `js/events` — 이벤트 처리

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

#### `js/animation` — 애니메이션 프레임

```no
# js/animation

id = animation.request-frame(() {

    ; 每幀回調
    print('frame')
})
animation.cancel-frame(id)
```

### Node.js API

#### `js/fs-read-file` / `js/fs-write-file` — 파일 읽기/쓰기

```no
# js/fs-read-file
# js/fs-write-file

data = fs-read-file('input.txt')
fs-write-file('output.txt', data)
```

#### `js/http-fetch` — HTTP 가져오기

```no
# js/http-fetch

data = http-fetch('https://api.example.com/data')
print(data)
```

#### `js/process-exit` — 프로세스 종료

```no
# js/process-exit

process-exit(0)
```

#### `js/fetch` — Fetch API(async)

```no
# js/fetch

; async 獲取 URL 數據
data = fetch.async('https://api.example.com/data')
json-data = fetch.json-async('https://api.example.com/data')
```

## 브라우저 모드

`--browser`를 사용하면 컴파일러가 다음을 생성합니다.

1. **JS 파일**: 컴파일된 JavaScript 소스 코드
2. **HTML 파일**: JS를 참조하는 HTML wrapper로, `#nolang-output` div를 포함합니다

HTML wrapper의 특징:

- `print()` 출력을 `<div id="nolang-output">`로 리다이렉트합니다
- 페이지에 기본 스타일(글꼴, 테두리, 고정폭 글꼴 출력 영역)이 포함됩니다
- `<script src="name.js">`로 컴파일 산출물을 참조합니다

출력 경로의 기본값은 `dist/` 디렉터리입니다.

## 서드파티 ESM 모듈 가져오기

JS 백엔드는 npm 생태계의 패키지를 직접 가져올 수 있습니다(TypeScript의 `import`와 동일한 기능). **새로운 `import` 문이 필요하지 않습니다.** 기존 `#` use 지시문에 예약 접두사 `npm/`을 덧붙여 사용합니다.

```no
# npm/lodash; 整個模組物件：lodash.get(x, 'a.b')
# npm/lodash.get as get; 單個命名導出：get(x, 'a.b')
# npm/lodash.default as d; 套件的預設導出：d(props)
# npm/@scope/pkg; scoped 套件
# npm/lodash/fp.get as fpg; 子路徑導入
```

규칙:

- **반드시 JS 백엔드여야 합니다.** LLVM과 Direct-WASM 백엔드는 `# npm/...`을 만나면 바로 오류를 내고 `no build --js`를 사용하라고 안내합니다.
- **패키지는 `package.noml`의 `dependencies`(또는 `devDependencies`)에 선언해야 합니다.** 그렇지 않으면 빌드가 실패합니다. 하위 경로 가져오기(`lodash/fp`)는 상위 패키지(`lodash`)가 목록에 있으면 됩니다.
- **짧은 이름으로 한정하는 사용법은 그대로입니다.** `# npm/lodash` 다음에 `lodash.get(...)`을 쓸 수 있고, 이름을 지정해 가져오기(`as get`)하면 `get(...)`을 바로 호출합니다.
- **v1에는 타입 통과가 없습니다.** 가져온 바인딩은 Nolang 타입을 지니지 않으므로(checker 변경 없음) 시그니처 검사를 하지 않습니다. 가져온 항목이 로컬 동명 함수와 충돌하면 가져온 쪽이 자동으로 이름을 바꿉니다(접미사 `$esm`).
- **내장 모듈과 이름이 겹치면 가져온 것이 우선합니다.** `# npm/math` 다음의 `math.sin(x)`은 `Math.sin`이 아니라 npm의 `math` 패키지를 가리킵니다.

### Node.js 실행

산출물은 표준 ES module입니다(import 문 + `import`는 module 범위에만 나타날 수 있음). `no build --js`가 산출물 옆에 `package.json`(`{"type": "module"}`, npm 관례)을 추가해 node가 ESM으로 실행하도록 합니다. 해당 디렉터리에 이미 ESM이 아닌 `package.json`이 있으면 `-o out.mjs`를 사용하라고 안내합니다.

```bash
npm install lodash                    # 先安裝套件
no build --js main.no                 # 產出 dist/main.js（+ dist/package.json）
node dist/main.js                     # 以 ESM 執行
no run --js main.no                   # 建置到暫存目錄後自動以 node 執行
```

`no run --js`는 프로젝트의 `node_modules`를 임시 디렉터리에 연결합니다(node는 스크립트가 있는 디렉터리에서 위로 올라가며 패키지를 해석합니다). 패키지가 없으면 바로 오류를 내고 `npm install lodash`를 안내합니다. 반면 `no build --js`는 패키지가 없어도 경고만 출력합니다(산출물 자체는 유효하며 런타임에만 패키지가 필요합니다).

### 브라우저 실행

`--browser`에서는 산출물 HTML이 `<script type="module">`가 되고 import map을 포함하여, 베어 지정자를 CDN(기본값 `https://esm.sh`)으로 해석합니다.

```html
<script type="importmap">
{"imports": {"lodash": "https://esm.sh/lodash"}}
</script>
<script type="module" src="main.js"></script>
```

CDN을 바꾸는 방법(우선순위가 높은 것부터):

1. `no build --js --browser --esm-cdn https://cdn.jsdelivr.net/npm main.no`
2. `package.noml`의 `compiler: { esm-cdn: 'https://cdn.jsdelivr.net/npm' }`

`<script type="module">`은 `file://`에서 CORS에 가로막히므로, `no run --js --browser`는 `127.0.0.1`의 임시 포트에 일회용 정적 서버를 띄운 뒤 브라우저를 엽니다(Ctrl+C로 중지). 전체 예제는 `example/esm-imports/`를 참고하세요.

## 네이티브 패키지: WASM과 FFI

JS 백엔드는 JS 패키지를 가져오는 것 외에도 네이티브 패키지(C ABI 공유 라이브러리／WASM)를 호출할 수 있습니다. 새로운 문법을 추가하는 대신 기존 선언에 애노테이션을 붙여 호출 경로를 선택합니다. 기존 C 함수 선언(`#{c}` + `name = (params) (results)`, FFI 장 참조)이 곧 네이티브 인터페이스이며, JS 백엔드는 원래 이를 그대로 건너뛰었습니다. `bind` 애노테이션을 붙이면 각 `#{c}` extern에 대해 호출 바인딩을 생성합니다.

### 애노테이션

`bind`는 **빌드 전략**입니다. `# import` 줄 위(독립된 줄)나 줄 끝에 쓸 수 있고, `#{c}` extern 위의 애노테이션 그룹 안에 바로 쓸 수도 있습니다.

| 키 | 값 | 설명 |
| --- | --- | --- |
| `bind` | `auto` / `ffi` / `wasm` | 호출 방식이며, 하나의 JS 빌드에는 한 가지 전략만 허용됩니다 |
| `wasm` | 문자열 | WASM 모듈 specifier이며, `bind=wasm` 또는 브라우저 `auto`일 때 필수입니다 |
| `lib` | 문자열 | FFI 라이브러리 이름을 덮어씁니다(기본값은 `compiler.link-libs`에서 가져옴) |
| `init` | 식별자 | WASM 초기화 내보내기 이름이며, 기본값은 `default`입니다 |

```no
; libm 的 pow：FFI 走 koffi，瀏覽器需另給 wasm= 的 specifier
#{c, bind=auto}
pow = (x f64, y f64) (r f64)
```

### 대상 × 전략 행렬

| 대상 | `bind=ffi` | `bind=wasm` | `bind=auto` |
| --- | --- | --- | --- |
| node / bun / deno | extern에 koffi 바인딩 | WASM 가져오기 + ready | `link-libs` / `lib=`가 있으면 → FFI, 없으면 `wasm=` 필요 → WASM, 그것도 없으면 오류 |
| browser | **빌드 시 오류**(koffi는 브라우저에서 실행할 수 없음) | WASM 가져오기 + ready | `wasm=`가 있으면 → WASM, 없으면 오류 |

추가 규칙: `bind=ffi`는 `#{c}` extern이 적어도 하나 필요하며, 없으면 오류입니다. `link-libs`도 `lib=`도 없으면 역시 오류입니다. 여러 곳에서 서로 충돌하는 `bind` / `wasm` 전략이 나타나면 빌드가 실패합니다.

### FFI(로컬 / node)

FFI는 [`koffi`](https://koffi.dev)를 통한 실제 FFI입니다. `koffi.load(<라이브러리>)` + `lib.func(symbol, ret, args)`를 사용합니다. 라이브러리 출처는 `package.noml`의 `compiler.link-libs`이며(`lib=`로 덮어쓸 수 있음), 여러 라이브러리는 순서대로 시도해 찾으면 바로 사용합니다. 각 `#{c}` extern마다 바인딩을 하나 생성하며, 로컬 이름은 `jsIdent(name)`(`_sqlite3-open` → `_sqlite3Open`)이고 C 심볼 이름은 앞의 `_`를 제거하고 `-`를 `_`로 바꾼 것입니다(`sqlite3_open`). 타입 매핑:

| Nolang | koffi | Nolang | koffi |
| --- | --- | --- | --- |
| `i8` / `u8` / `byte` | `int8` / `uint8` | `i32` / `u32` | `int` / `uint` |
| `i16` / `u16` | `int16` / `uint16` | `i64` / `u64` | `int64` / `uint64` |
| `f32` / `f64` | `float` / `double` | `str` | `str` |
| `bool` | `bool` | `void` / 반환값 없음 | `void` |
| `*T` | `koffi.pointer(<T>)` | `**T` | `koffi.pointer(koffi.pointer(<T>))` |

그 밖의 것(슬라이스／배열／구조체)은 v1에서 항상 `"void *"`로 대체합니다. `koffi`는 합성 ESM 의존성으로 추가됩니다. `no build`는 패키지가 없으면 경고만 출력하지만 `no run --js`는 바로 오류를 내고 `npm install koffi`를 안내합니다(ESM 전략과 동일합니다). koffi는 CJS이므로 `import * as` + `.default ??` 상호 운용 계층을 사용하고, 단순 named import는 사용하지 않습니다.

### WASM(브라우저 / `bind=wasm`)

WASM 모드는 지정된 `.wasm` 모듈을 가져와 각 `#{c}` extern에 대해 **동일한 이름으로 전달**합니다(`_sqlite3-open` → wasm 내보내기 `sqlite3_open`). 초기화 시 ready Promise를 하나 생성하지만 **await하지는 않습니다**. 동기적으로 초기화되는 모듈은 즉시 준비되고, 비동기 모듈은 준비가 되면 state를 전환하며 ready Promise를 `globalThis.__nolang_wasm_ready[<짧은 이름>]`에 노출해 바깥쪽 JS가 await할 수 있게 합니다. 준비되기 전에 호출하면 명확한 오류가 발생합니다.

WASM specifier에는 두 가지 형태가 있으며, 나머지 규칙은 형태에 따라 다릅니다.

| 형태 | 예 | `dependencies` 선언 | 브라우저 import map | `no run --js` |
| --- | --- | --- | --- | --- |
| bare(npm 패키지) | `@sqlite.org/sqlite-wasm` | **필수** | `--esm-cdn`(기본값 `https://esm.sh`)으로 해석 | `npm install` 필요 |
| 상대(로컬 파일) | `./math-wasm.mjs` | **불필요** | **map에 넣지 않음**(브라우저는 원래 상대 specifier를 재매핑하지 않음) | 해당 파일(및 그 파일 자신의 상대 가져오기)을 자동으로 임시 디렉터리에 링크 |

상대 형태는 "모듈이 프로젝트 안에 있는" 경우에 적합합니다. `no build --js`는 산출물과 모듈이 같은 디렉터리에 있으면 실행되고, `no run --js`는 모듈을 임시 bundle 디렉터리에 링크해 줍니다. 브라우저 대상이라면 모듈도 함께 `dist/`로 복사해야 합니다(`import`는 파일 위치를 기준으로 상대 경로를 해석합니다).

### 전체 예제

- `example/native-libm/`: libm의 `pow`를 `#{bind = auto, c}`로 선언하며, `no run --js main.no`가 `1024`를 출력합니다(`no run`은 먼저 해당 디렉터리에서 `npm install`로 `koffi`를 설치해야 함).
- `example/native-wasm/`: 로컬 WASM 모듈(`math.wasm` + `math-wasm.mjs`, npm 의존성 없음)으로, `no run --js main.no`가 `42.25`와 `42`를 출력합니다. `no build --js --browser main.no`의 import map에는 상대 specifier가 포함되지 않습니다.

### 알려진 제한(v1)
- WASM은 **동일한 이름의 내보내기 전달**만 수행하며 API 어댑터 계층을 생성하지 않습니다(예: `better-sqlite3` → `sql.js` 같은 의미 차이는 직접 nolang wrapper 모듈을 제공해야 함).
- 전략은 **프로그램 단위**입니다. 하나의 JS 빌드에는 한 가지 `bind` 전략만 허용되며, 여러 네이티브 라이브러리를 모듈별로 연결하는 기능은 후속 과제로 남겨두었습니다.
- FFI는 C ABI 공유 라이브러리만 바인딩합니다. Node 네이티브 addon(`.node`)은 `bind=wasm`이나 일반 ESM 가져오기를 사용하세요.

## 전체 예제

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

빌드:

```bash
no build --js --browser main.no
# 輸出: dist/main.js, dist/main.html
# 在瀏覽器中打開 dist/main.html
```
