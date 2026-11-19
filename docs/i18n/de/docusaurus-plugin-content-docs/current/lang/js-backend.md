---
sidebar_position: 8
---

# JS-Backend

Nolang kann `.no`-Quellcode direkt zu JavaScript kompilieren, ohne LLVM-Toolchain. Das JS-Backend verfolgt eine **Type-Erasure**-Strategie: Alle Nolang-Typannotationen werden in der JS-Ausgabe nicht beibehalten, es wird nur das Laufzeitverhalten erzeugt.

## Schnellstart

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

## Designprinzipien

Das KernDesign des JS-Backends:

- **Type Erasure**: JS ist dynamisch typisiert, Nolang-Typannotationen wie `int`/`str`/`bool`/`vec[T]`/`[N]T`/`?T` werden in der JS-Ausgabe vollständig entfernt
- **Kein LLVM nötig**: JavaScript-Code wird direkt aus der AST emittiert, ohne Abhängigkeit von clang/LLVM
- **Zwei Umgebungen**: Unterstützt Node.js (Standard) und Browser (`--browser`) als Zielumgebungen
- **Plattform-Annotationen**: `#{js}` und `#{js-browser}` steuern die Plattform-Sichtbarkeit von Code

Die Implementierung liegt im Verzeichnis `src/build/js/`:

| Datei | Aufgabe |
| --- | --- |
| `generator.go` | Hauptlogik der Codegenerierung AST → JavaScript |
| `expr.go` | Erzeugung von Ausdrücken |
| `stmt.go` | Erzeugung von Anweisungen |
| `builtin.go` | Abbildung integrierter Funktionen (print → console.log usw.) |
| `html_wrapper.go` | HTML-Vorlage für den Browser-Modus |

## Kommandozeilenparameter

| Parameter | Beschreibung |
| --- | --- |
| `--js` | JS-Backend verwenden (JavaScript emittieren, LLVM umgehen) |
| `--browser` | Browser-orientierte Ausgabe erzeugen (HTML + JS, muss mit `--js` kombiniert werden) |
| `--esm-cdn <url>` | CDN-Präfix der Browser-Import-Map überschreiben (Standard `https://esm.sh`) |
| `--js-runtime <cmd>` | Die von `no run --js` verwendete JS-Laufzeit (Standard `node`; z. B. `bun`, `deno` möglich, mit Parametern erlaubt) |
| `-o <path>` | Ausgabepfad festlegen |

### Bauen

```bash
no build --js main.no                    # 輸出 dist/main.js
no build --js -o app.js main.no          # 指定輸出路徑
no build --js --browser main.no          # 輸出 dist/main.js + dist/main.html
```

### Ausführen

```bash
no run --js main.no                      # 編譯為 JS 後以 node 執行
no run --js --browser main.no            # 編譯為瀏覽器 JS + HTML 並打開瀏覽器
```

#### JS-Laufzeit auswählen

`--js-runtime` legt die Laufzeit für die erzeugten Artefakte fest, **Standard ist `node`** (Node.js). Jede Laufzeit, die ES-Module ausführen kann, ist geeignet:

```bash
no run --js --js-runtime bun main.no             # Bun
no run --js --js-runtime deno run main.no        # Deno（首個 token 是可執行檔，其餘是腳本前的參數）
no run --js --js-runtime "deno run --allow-read" main.no
no run --js --js-runtime "node --enable-source-maps" main.no
no run --js --js-runtime /opt/js/bin/node main.no   # 指定絕對路徑
```

- Das erste durch Leerzeichen getrennte Token der Spezifikation ist die ausführbare Datei, die übrigen Tokens werden vor dem Skriptpfad angeordnet; Argumente des Programms werden wie gewohnt nach dem `.no`-Dateinamen geschrieben.
- Ohne Angabe von `--js-runtime` wird `node` verwendet; ist nur ein `nodejs`-Binary im Debian-/Ubuntu-Stil vorhanden, fällt das System automatisch darauf zurück. Explizit angegebene Laufzeiten (`bun`, absoluter Pfad, `node --flags`) fallen nie zurück und melden bei Fehlschlag einen Fehler.
- Ist die Laufzeit nicht im `PATH`, wird direkt ein Fehler gemeldet und ein Wechsel über `--js-runtime` vorgeschlagen (kein stiller Rückfall auf node).
- ESM-Imports (`# npm/…`) funktionieren mit diesen Laufzeiten ebenfalls: `no run --js` verlinkt die `node_modules` des Projekts in ein temporäres Verzeichnis; node, bun und deno lösen nach denselben Regeln auf.

## Plattform-Annotationen

Das JS-Backend führt zwei zusätzliche Plattform-Annotationsschlüssel ein:

| Schlüssel | Passendes Szenario |
| --- | --- |
| `#{js}` | JS-Backend (Node.js und Browser) |
| `#{js-browser}` | Browser-Modus (mit `--browser`) |

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

## Abbildung integrierter Funktionen

Das JS-Backend bildet Nolang-Builtins auf JavaScript-Äquivalente ab:

| Nolang | JavaScript | Beschreibung |
| --- | --- | --- |
| `print(x)` | `console.log(x)` | Automatischer Zeilenumbruch |
| `eprint(x)` | `console.error(x)` | Ausgabe nach stderr |
| `format(...)` | String-Konkatenation | v1 vereinfacht: Verkettung mit `"" +` |
| `len(x)` | `x.length` | Für Strings und Arrays |
| `with-len(n)` | `new Array(n)` | Array mit angegebener Länge erzeugen |

## JS-Backend-Standardbibliothek

Das Verzeichnis `src/js/` enthält JS-Backend-spezifische Module, alle mit der Plattform-Annotation `#{js}` versehen:

### Browser-API

#### `js/dom` — DOM-Operationen

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

#### `js/canvas` — Canvas-2D-Zeichnung

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

#### `js/events` — Ereignisbehandlung

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

#### `js/location` — Location-API

```no
# js/location

href = location.href()
search = location.search()
path = location.path()
location.redirect('https://example.com')
```

#### `js/history` — History-API

```no
# js/history

history.back()
history.forward()
history.push('/page2')
n = history.length()
```

#### `js/animation` — Animationsframes

```no
# js/animation

id = animation.request-frame(() {

    ; 每幀回調
    print('frame')
})
animation.cancel-frame(id)
```

### Node.js-API

#### `js/fs-read-file` / `js/fs-write-file` — Datei-Lese-/Schreibzugriff

```no
# js/fs-read-file
# js/fs-write-file

data = fs-read-file('input.txt')
fs-write-file('output.txt', data)
```

#### `js/http-fetch` — HTTP-Abruf

```no
# js/http-fetch

data = http-fetch('https://api.example.com/data')
print(data)
```

#### `js/process-exit` — Prozessbeendigung

```no
# js/process-exit

process-exit(0)
```

#### `js/fetch` — Fetch-API (async)

```no
# js/fetch

; async 獲取 URL 數據
data = fetch.async('https://api.example.com/data')
json-data = fetch.json-async('https://api.example.com/data')
```

## Browser-Modus

Bei Verwendung von `--browser` erzeugt der Compiler:

1. **JS-Datei**: der kompilierte JavaScript-Quellcode
2. **HTML-Datei**: ein HTML-Wrapper, der das JS einbindet und ein `<div id="nolang-output">` enthält

Eigenschaften des HTML-Wrappers:

- Die Ausgabe von `print()` wird zu `<div id="nolang-output">` umgeleitet
- Die Seite enthält grundlegende Styles (Schriftart, Rahmen, Ausgabebereich mit Monospace-Schrift)
- Das kompilierte Artefakt wird über `<script src="name.js">` eingebunden

Der Ausgabepfad ist standardmäßig das Verzeichnis `dist/`.

## Import von Drittanbieter-ESM-Modulen

Das JS-Backend kann Pakete aus dem npm-Ökosystem direkt importieren (Äquivalent zu TypeScripts `import`) — **ohne neue `import`-Anweisung**: Die `#`-use-Direktive wird mit dem reservierten Präfix `npm/` wiederverwendet:

```no
# npm/lodash; 整個模組物件：lodash.get(x, 'a.b')
# npm/lodash.get as get; 單個命名導出：get(x, 'a.b')
# npm/lodash.default as d; 套件的預設導出：d(props)
# npm/@scope/pkg; scoped 套件
# npm/lodash/fp.get as fpg; 子路徑導入
```

Regeln:

- **JS-Backend erforderlich**: Die LLVM- und Direct-WASM-Backends melden bei `# npm/...` direkt einen Fehler und verweisen auf `no build --js`.
- **Paket muss in `package.noml` deklariert sein**: Es muss in `dependencies` (oder `devDependencies`) stehen, sonst schlägt der Build fehl. Subpfad-Imports (`lodash/fp`) genügen, wenn das Elternpaket (`lodash`) in der Liste steht.
- **Kurznamensqualifizierung bleibt unverändert**: Nach `# npm/lodash` kann man `lodash.get(...)` schreiben; bei benanntem Import (`as get`) ruft man direkt `get(...)` auf.
- **v1 ohne Typweitergabe**: Importierte Bindungen tragen keine Nolang-Typen (der Checker bleibt unverändert), daher findet keine Signaturprüfung statt; kollidiert ein Import mit einer lokalen Funktion gleichen Namens, wird die Importseite automatisch umbenannt (Suffix `$esm`).
- **Bei Namenskonflikt mit eingebauten Modulen gilt der Import**: Nach `# npm/math` zeigt `math.sin(x)` auf das npm-Paket `math`, nicht auf `Math.sin`.

### Ausführung unter Node.js

Das Artefakt ist ein standardmäßiges ES-Modul (Import-Anweisungen + `import` darf nur im Modul-Geltungsbereich stehen). `no build --js` legt neben dem Artefakt eine `package.json` an (`{"type": "module"}`, npm-Konvention), damit node das Modul als ESM ausführt; existiert im Verzeichnis bereits eine `package.json` ohne ESM, wird ein Wechsel zu `-o out.mjs` vorgeschlagen.

```bash
npm install lodash                    # 先安裝套件
no build --js main.no                 # 產出 dist/main.js（+ dist/package.json）
node dist/main.js                     # 以 ESM 執行
no run --js main.no                   # 建置到暫存目錄後自動以 node 執行
```

`no run --js` verlinkt die `node_modules` des Projekts in ein temporäres Verzeichnis (node löst Pakete ab dem Skriptverzeichnis nach oben auf); fehlt ein Paket, wird direkt ein Fehler gemeldet und `npm install lodash` vorgeschlagen; `no build --js` gibt bei fehlenden Paketen nur eine Warnung aus (das Artefakt ist gültig, das Paket wird erst zur Laufzeit benötigt).

### Ausführung im Browser

Unter `--browser` wird das HTML-Artefakt zu `<script type="module">` mit einer Import-Map, die nackte Specifier zu einem CDN auflöst (Standard `https://esm.sh`):

```html
<script type="importmap">
{"imports": {"lodash": "https://esm.sh/lodash"}}
</script>
<script type="module" src="main.js"></script>
```

So wird das CDN gewechselt (Priorität von hoch nach niedrig):

1. `no build --js --browser --esm-cdn https://cdn.jsdelivr.net/npm main.no`
2. `package.noml` mit `compiler: { esm-cdn: 'https://cdn.jsdelivr.net/npm' }`

Da `<script type="module">` unter `file://` von CORS blockiert wird, startet `no run --js --browser` einen einmaligen statischen Server auf einem temporären Port unter `127.0.0.1` und öffnet dann den Browser (mit Strg+C beenden). Ein vollständiges Beispiel liegt in `example/esm-imports/`.

## Native Pakete: WASM und FFI

Neben dem Import von JS-Paketen kann das JS-Backend auch native Pakete aufrufen (C-ABI-Bibliotheken / WASM). Dazu wird keine neue Syntax eingeführt, sondern eine bestehende Deklaration per Annotation versehen, um den Aufrufpfad zu wählen. Die vorhandenen C-Funktionsdeklarationen (`#{c}` + `name = (params) (results)`, siehe FFI-Kapitel) sind die native Schnittstelle; das JS-Backend übersprang sie bisher — mit der `bind`-Annotation wird für jedes `#{c}`-extern eine Aufrufbindung erzeugt.

### Annotation

`bind` ist eine **Build-Strategie**: Sie kann über einer `# import`-Zeile (eigenständig) oder am Zeilenende stehen oder direkt in der Annotationsgruppe über einem `#{c}`-extern.

| Schlüssel | Werte | Beschreibung |
| --- | --- | --- |
| `bind` | `auto` / `ffi` / `wasm` | Aufrufart; pro JS-Build ist nur eine Strategie erlaubt |
| `wasm` | String | WASM-Modul-Specifier; bei `bind=wasm` oder Browser-`auto` Pflicht |
| `lib` | String | FFI-Bibliotheksname überschreiben (Standard aus `compiler.link-libs`) |
| `init` | Bezeichner | Exportname zur WASM-Initialisierung, Standard `default` |

```no
; libm 的 pow：FFI 走 koffi，瀏覽器需另給 wasm= 的 specifier
#{c, bind=auto}
pow = (x f64, y f64) (r f64)
```

### Ziel × Strategie-Matrix

| Ziel | `bind=ffi` | `bind=wasm` | `bind=auto` |
| --- | --- | --- | --- |
| node / bun / deno | koffi-Bindung für extern | WASM-Import + ready | Mit `link-libs` / `lib=` → FFI; sonst `wasm=` nötig → WASM; sonst Fehler |
| browser | **Buildzeitfehler** (koffi kann im Browser nicht laufen) | WASM-Import + ready | Mit `wasm=` → WASM; sonst Fehler |

Zusätzliche Regeln: `bind=ffi` benötigt mindestens ein `#{c}`-extern, sonst Fehler; fehlen sowohl `link-libs` als auch `lib=`, ebenfalls Fehler. Bei widersprüchlichen `bind`-/`wasm`-Strategien an mehreren Stellen schlägt der Build fehl.

### FFI (lokal / node)

FFI nutzt echtes FFI über [`koffi`](https://koffi.dev): `koffi.load(<lib>)` + `lib.func(symbol, ret, args)`. Die Bibliothek stammt aus `compiler.link-libs` in `package.noml` (`lib=` kann sie überschreiben); mehrere Bibliotheken werden der Reihe nach versucht, die erste passende wird verwendet. Für jedes `#{c}`-extern entsteht eine Bindung mit dem lokalen Namen `jsIdent(name)` (`_sqlite3-open` → `_sqlite3Open`) und dem C-Symbolnamen ohne führendes `_` und mit `-` → `_` (`sqlite3_open`). Typzuordnung:

| Nolang | koffi | Nolang | koffi |
| --- | --- | --- | --- |
| `i8` / `u8` / `byte` | `int8` / `uint8` | `i32` / `u32` | `int` / `uint` |
| `i16` / `u16` | `int16` / `uint16` | `i64` / `u64` | `int64` / `uint64` |
| `f32` / `f64` | `float` / `double` | `str` | `str` |
| `bool` | `bool` | `void` / keine Rückgabe | `void` |
| `*T` | `koffi.pointer(<T>)` | `**T` | `koffi.pointer(koffi.pointer(<T>))` |

Alle anderen (Slices / Arrays / Strukturen) fallen in v1 auf `"void *"` zurück. `koffi` wird als synthetisierte ESM-Abhängigkeit hinzugefügt: `no build` meldet bei fehlendem Paket nur eine Warnung, `no run --js` meldet direkt einen Fehler und verweist auf `npm install koffi` (konsistent mit der ESM-Strategie). koffi ist CJS und läuft über `import * as` + `.default ??` als Interop-Schicht, kein nackter Named Import.

### WASM (Browser / `bind=wasm`)

Im WASM-Modus wird das angegebene `.wasm`-Modul importiert und für jedes `#{c}`-extern eine **Weiterleitung mit gleichem Namen** erzeugt (`_sqlite3-open` → WASM-Export `sqlite3_open`). Die Initialisierung erzeugt ein Ready-Promise (**ohne await**): Synchron initialisierte Module sind sofort bereit, asynchrone ändern den Zustand bei Fertigstellung; das Ready-Promise wird unter `globalThis.__nolang_wasm_ready[<kurzname>]` bereitgestellt, damit äußerer JS-Code es abwarten kann. Ein Aufruf vor der Bereitschaft wirft einen klaren Fehler.

WASM-Specifier gibt es in zwei Formen, die übrigen Regeln unterscheiden sich je nach Form:

| Form | Beispiel | `dependencies`-Deklaration | Browser-Import-Map | `no run --js` |
| --- | --- | --- | --- | --- |
| bare (npm-Paket) | `@sqlite.org/sqlite-wasm` | **Pflicht** | Über `--esm-cdn` aufgelöst (Standard `https://esm.sh`) | `npm install` nötig |
| relativ (lokale Datei) | `./math-wasm.mjs` | **nicht nötig** | **Nicht in der Map** (Browser mapped relative Specifier ohnehin nicht neu) | Verlinkt die Datei (und ihre eigenen relativen Imports) automatisch in ein temporäres Verzeichnis |

Die relative Form gilt für den Fall „Modul liegt im Projekt“: Das Artefakt von `no build --js` kann im selben Verzeichnis wie das Modul ausgeführt werden, `no run --js` verlinkt das Modul in das temporäre Bundle-Verzeichnis; für Browser-Ziele muss das Modul zusätzlich nach `dist/` kopiert werden (`import`-Auflösung ist relativ zur Dateiposition).

### Vollständige Beispiele

- `example/native-libm/`: `#{bind = auto, c}` deklariert `pow` aus libm, `no run --js main.no` gibt `1024` aus (`no run` erfordert zuvor `npm install` von `koffi` in diesem Verzeichnis).
- `example/native-wasm/`: lokales WASM-Modul (`math.wasm` + `math-wasm.mjs`, keine npm-Abhängigkeit), `no run --js main.no` gibt `42.25` und `42` aus; die Import-Map von `no build --js --browser main.no` enthält keine relativen Specifier.

### Bekannte Einschränkungen (v1)
- WASM unterstützt nur **Weiterleitung mit gleichem Namen**, keine API-Anpassungsschicht (semantische Unterschiede wie `better-sqlite3` → `sql.js` erfordern ein selbst bereitgestelltes Nolang-Wrapper-Modul).
- Die Strategie ist **programmweit**: Ein JS-Build erlaubt nur eine `bind`-Strategie; die modulweise Zuordnung mehrerer nativer Bibliotheken bleibt späteren Versionen vorbehalten.
- FFI bindet nur C-ABI-Bibliotheken; Node-Native-Addons (`.node`) bitte mit `bind=wasm` oder als gewöhnlichen ESM-Import verwenden.

## Vollständiges Beispiel

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

Bauen:

```bash
no build --js --browser main.no
# 輸出: dist/main.js, dist/main.html
# 在瀏覽器中打開 dist/main.html
```
