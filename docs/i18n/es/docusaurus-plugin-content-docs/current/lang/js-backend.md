---
sidebar_position: 8
---

# Backend JS

Nolang admite compilar código fuente `.no` directamente a JavaScript, sin necesidad de la cadena de herramientas LLVM. El backend JS emplea una estrategia de **borrado de tipos** (type erasure): todas las anotaciones de tipo de Nolang se omiten en la salida JS; solo se genera el comportamiento en tiempo de ejecución.

## Inicio rápido

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

## Principios de diseño

Diseño básico del backend JS:

- **Borrado de tipos**: JS es de tipado dinámico; las anotaciones de tipo de Nolang (`int`/`str`/`bool`/`vec[T]`/`[N]T`/`?T`, etc.) desaparecen por completo en la salida JS.
- **Sin LLVM**: emite código JavaScript directamente desde el AST, sin depender de clang/LLVM.
- **Doble entorno**: admite Node.js (por defecto) y navegador (`--browser`) como entornos de destino.
- **Anotaciones de plataforma**: `#{js}` y `#{js-browser}` controlan la visibilidad del código según la plataforma.

La implementación está en el directorio `src/build/js/`:

| Archivo | Responsabilidad |
| --- | --- |
| `generator.go` | lógica principal de generación de código AST → JavaScript |
| `expr.go` | generación de expresiones |
| `stmt.go` | generación de sentencias |
| `builtin.go` | mapeo de funciones integradas (print→console.log, etc.) |
| `html_wrapper.go` | plantilla HTML del modo navegador |

## Parámetros de línea de comandos

| Parámetro | Descripción |
| --- | --- |
| `--js` | usar el backend JS (emitir JavaScript, omitiendo LLVM) |
| `--browser` | generar salida orientada al navegador (HTML + JS, requiere `--js`) |
| `--esm-cdn <url>` | sobrescribe el prefijo CDN del import map ESM del navegador (por defecto `https://esm.sh`) |
| `--js-runtime <cmd>` | entorno de ejecución JS usado por `no run --js` (por defecto `node`; se puede cambiar a `bun`, `deno`, etc., con argumentos adicionales permitidos) |
| `-o <path>` | especificar la ruta de salida |

### Compilación

```bash
no build --js main.no                    # 輸出 dist/main.js
no build --js -o app.js main.no          # 指定輸出路徑
no build --js --browser main.no          # 輸出 dist/main.js + dist/main.html
```

### Ejecución

```bash
no run --js main.no                      # 編譯為 JS 後以 node 執行
no run --js --browser main.no            # 編譯為瀏覽器 JS + HTML 並打開瀏覽器
```

#### Elegir el entorno de ejecución JS

`--js-runtime` especifica el entorno que ejecutará el artefacto; **por defecto es `node`** (Node.js). Sirve cualquier entorno capaz de ejecutar módulos ES:

```bash
no run --js --js-runtime bun main.no             # Bun
no run --js --js-runtime deno run main.no        # Deno（el primer token es el ejecutable, el resto son argumentos antes del script）
no run --js --js-runtime "deno run --allow-read" main.no
no run --js --js-runtime "node --enable-source-maps" main.no
no run --js --js-runtime /opt/js/bin/node main.no   # 指定絕對路徑
```

- El primer token separado por espacios de la especificación es el ejecutable; los demás tokens se colocan antes de la ruta del script. Los argumentos del programa se escriben como siempre detrás del nombre del archivo `.no`.
- Por defecto (sin `--js-runtime`) se usa `node`; si el sistema solo tiene el binario `nodejs` de estilo Debian/Ubuntu, se recurre automáticamente a él. Un entorno especificado explícitamente (`bun`, ruta absoluta, `node --flags`) nunca tiene fallback: si no se encuentra, se informa del error.
- Si el entorno no está en `PATH`, se informa directamente del error y se sugiere cambiarlo con `--js-runtime` (no se recurre silenciosamente a node).
- Las importaciones ESM (`# npm/…`) funcionan igual con estos entornos: `no run --js` enlaza el `node_modules` del proyecto en un directorio temporal, y node/bun/deno resuelven las dependencias con las mismas reglas.

## Anotaciones de plataforma

El backend JS introduce dos claves adicionales de anotación de plataforma:

| Clave | Escenario en que coincide |
| --- | --- |
| `#{js}` | backend JS (Node.js y navegador) |
| `#{js-browser}` | modo navegador (con `--browser`) |

```no
; solo se conserva al compilar con el backend JS
#{js}
js-helper = () {
    print('JS only code')
}

; solo se conserva en modo navegador
#{js-browser}
print('running in browser mode')

; solo se conserva con el backend nativo (se excluye al compilar a JS)
#{mac-arm64}
print('running on macOS ARM64')
```

## Mapeo de funciones integradas

El backend JS mapea las funciones integradas de Nolang a sus equivalentes en JavaScript:

| Nolang | JavaScript | Descripción |
| --- | --- | --- |
| `print(x)` | `console.log(x)` | añade salto de línea automáticamente |
| `eprint(x)` | `console.error(x)` | escribe en stderr |
| `format(...)` | concatenación de cadenas | simplificación v1: concatena con `"" +` |
| `len(x)` | `x.length` | válido para cadenas y arreglos |
| `with-len(n)` | `new Array(n)` | crea un arreglo de la longitud indicada |

## Biblioteca estándar del backend JS

El directorio `src/js/` ofrece módulos específicos del backend JS, todos con la anotación de plataforma `#{js}`:

### API del navegador

#### `js/dom` — manipulación del DOM

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

#### `js/canvas` — dibujo Canvas 2D

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

#### `js/events` — manejo de eventos

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

#### `js/animation` — fotogramas de animación

```no
# js/animation

id = animation.request-frame(() {

    ; 每幀回調
    print('frame')
})
animation.cancel-frame(id)
```

### API de Node.js

#### `js/fs-read-file` / `js/fs-write-file` — lectura y escritura de archivos

```no
# js/fs-read-file
# js/fs-write-file

data = fs-read-file('input.txt')
fs-write-file('output.txt', data)
```

#### `js/http-fetch` — obtención HTTP

```no
# js/http-fetch

data = http-fetch('https://api.example.com/data')
print(data)
```

#### `js/process-exit` — salida del proceso

```no
# js/process-exit

process-exit(0)
```

#### `js/fetch` — Fetch API (async)

```no
# js/fetch

; async 獲取 URL 數據
data = fetch.async('https://api.example.com/data')
json-data = fetch.json-async('https://api.example.com/data')
```

## Modo navegador

Al usar `--browser`, el compilador genera:

1. **Archivo JS**: el código JavaScript compilado.
2. **Archivo HTML**: un wrapper HTML que referencia el JS y contiene un div `#nolang-output`.

Características del wrapper HTML:

- la salida de `print()` se redirige a `<div id="nolang-output">`;
- la página incluye estilos básicos (fuente, borde y área de salida monoespaciada);
- el artefacto compilado se referencia mediante `<script src="name.js">`.

La ruta de salida por defecto es el directorio `dist/`.

## Importar módulos ESM de terceros

El backend JS puede importar directamente paquetes del ecosistema npm (el equivalente a `import` de TypeScript), **sin necesidad de una nueva sentencia `import`**: reutiliza la directiva `#` con el prefijo reservado `npm/`:

```no
# npm/lodash; objetoCompleto: lodash.get(x, 'a.b')
# npm/lodash.get as get; exportación con nombre individual: get(x, 'a.b')
# npm/lodash.default as d; exportación por defecto del paquete: d(props)
# npm/@scope/pkg; paquete con scope
# npm/lodash/fp.get as fpg; importación de subruta
```

Reglas:

- **Debe ser el backend JS**. Los backends LLVM y Direct-WASM producen un error directo al encontrar `# npm/...` y sugieren usar `no build --js`.
- **El paquete debe declararse** en `dependencies` (o `devDependencies`) de `package.noml`; si no, la compilación falla. Las importaciones de subruta (`lodash/fp`) bastan con que el paquete padre (`lodash`) esté en la lista.
- **El uso con nombre corto no cambia**: después de `# npm/lodash` se puede escribir `lodash.get(...)`; con una importación con nombre (`as get`) se llama directamente a `get(...)`.
- **v1 sin tipos**: los enlaces importados no llevan tipos Nolang (el checker no se modifica), así que no hay comprobación de firmas; cuando un importado choca con una función local del mismo nombre, el lado importado se renombra automáticamente (sufijo `$esm`).
- **En caso de colisión con un módulo integrado, prevalece la importación**: después de `# npm/math`, `math.sin(x)` apunta al paquete npm `math`, no a `Math.sin`.

### Ejecución en Node.js

El artefacto es un módulo ES estándar (sentencias import; `import` solo puede aparecer en ámbito de módulo). `no build --js` añade junto al artefacto un `package.json` (`{"type": "module"}`, convención npm) para que node lo ejecute como ESM; si el directorio ya tiene un `package.json` que no es ESM, se sugiere usar `-o out.mjs`.

```bash
npm install lodash                    # 先安裝套件
no build --js main.no                 # 產出 dist/main.js（+ dist/package.json）
node dist/main.js                     # 以 ESM 執行
no run --js main.no                   # 建置到暫存目錄後自動以 node 執行
```

`no run --js` enlaza el `node_modules` del proyecto en el directorio temporal (node resuelve los paquetes subiendo desde el directorio del script); si falta un paquete, informa del error y sugiere `npm install lodash`. `no build --js` solo avisa con una advertencia si falta un paquete (el artefacto sigue siendo válido; el paquete solo se necesita en tiempo de ejecución).

### Ejecución en el navegador

Con `--browser`, el HTML generado usa `<script type="module">` e incluye un import map que resuelve los especificadores pelados a un CDN (por defecto `https://esm.sh`):

```html
<script type="importmap">
{"imports": {"lodash": "https://esm.sh/lodash"}}
</script>
<script type="module" src="main.js"></script>
```

Formas de cambiar el CDN (de mayor a menor prioridad):

1. `no build --js --browser --esm-cdn https://cdn.jsdelivr.net/npm main.no`
2. `compiler: { esm-cdn: 'https://cdn.jsdelivr.net/npm' }` en `package.noml`

Como `<script type="module">` queda bloqueado por CORS bajo `file://`, `no run --js --browser` levanta un servidor estático desechable en un puerto temporal de `127.0.0.1` y abre el navegador (Ctrl+C para detenerlo). Véase el ejemplo completo en `example/esm-imports/`.

## Paquetes nativos: WASM y FFI

Además de importar paquetes JS, el backend JS puede llamar a paquetes nativos (bibliotecas compartidas con ABI C / WASM). No se añade sintaxis nueva: se anota una declaración ya existente para elegir la vía de llamada. La declaración C existente (`#{c}` + `name = (params) (results)`, véase el capítulo FFI) es la interfaz nativa, que el backend JS antes omitía directamente; al añadir la anotación `bind`, se genera un enlace de llamada para cada `extern` `#{c}`.

### Anotaciones

`bind` es una **estrategia de compilación**: se puede escribir sobre una línea `# import` (en su propia línea) o al final de ella, o directamente dentro del grupo de anotaciones sobre un `extern` `#{c}`.

| Clave | Valores | Descripción |
| --- | --- | --- |
| `bind` | `auto` / `ffi` / `wasm` | forma de llamada; una compilación JS solo permite una estrategia |
| `wasm` | cadena | specifier del módulo WASM; obligatorio con `bind=wasm` o con `auto` en navegador |
| `lib` | cadena | sobrescribe el nombre de la biblioteca FFI (por defecto se toma de `compiler.link-libs`) |
| `init` | identificador | nombre de exportación de inicialización WASM, por defecto `default` |

```no
; pow de libm: FFI usa koffi; el navegador necesita además un specifier wasm=
#{c, bind=auto}
pow = (x f64, y f64) (r f64)
```

### Matriz destino × estrategia

| Destino | `bind=ffi` | `bind=wasm` | `bind=auto` |
| --- | --- | --- | --- |
| node / bun / deno | enlace koffi del extern | importación WASM + ready | con `link-libs` / `lib=` → FFI; si no, requiere `wasm=` → WASM; si no, error |
| navegador | **error en compilación** (koffi no funciona en el navegador) | importación WASM + ready | con `wasm=` → WASM; si no, error |

Reglas adicionales: `bind=ffi` requiere al menos un `extern` `#{c}`, si no, error; tampoco se permiten ausencias simultáneas de `link-libs` y `lib=`. Si aparecen estrategias `bind` / `wasm` contradictorias en varios puntos, la compilación falla.

### FFI (nativo / node)

FFI usa FFI real mediante [`koffi`](https://koffi.dev): `koffi.load(<biblioteca>)` + `lib.func(symbol, ret, args)`. La biblioteca se toma de `compiler.link-libs` de `package.noml` (`lib=` puede sobrescribirla); se prueban varias bibliotecas en orden y se usa la primera que funciona. Cada `extern` `#{c}` genera un enlace, con nombre local `jsIdent(name)` (`_sqlite3-open` → `_sqlite3Open`) y nombre de símbolo C consistente en quitar el `_` inicial y cambiar `-` por `_` (`sqlite3_open`). Mapeo de tipos:

| Nolang | koffi | Nolang | koffi |
| --- | --- | --- | --- |
| `i8` / `u8` / `byte` | `int8` / `uint8` | `i32` / `u32` | `int` / `uint` |
| `i16` / `u16` | `int16` / `uint16` | `i64` / `u64` | `int64` / `uint64` |
| `f32` / `f64` | `float` / `double` | `str` | `str` |
| `bool` | `bool` | `void` / sin retorno | `void` |
| `*T` | `koffi.pointer(<T>)` | `**T` | `koffi.pointer(koffi.pointer(<T>))` |

Los demás tipos (rebanadas / arreglos / estructuras) usan `"void *"` como respaldo en v1. `koffi` se añade como dependencia ESM sintética: si falta, `no build` solo avisa, mientras que `no run --js` informa del error y sugiere `npm install koffi` (coherente con la estrategia ESM). koffi es CJS, así que se usa la capa de interoperabilidad `import * as` + `.default ??`, no una importación con nombre directa.

### WASM (navegador / `bind=wasm`)

El modo WASM importa el módulo `.wasm` indicado y genera un **reenvío con el mismo nombre** para cada `extern` `#{c}` (`_sqlite3-open` → exportación wasm `sqlite3_open`). La inicialización produce una Promise de ready (**sin await**): los módulos de inicialización síncrona quedan listos de inmediato; los asíncronos cambian de estado cuando están listos, y la Promise de ready se expone en `globalThis.__nolang_wasm_ready[<nombre-corto>]` para que el JS externo pueda usar `await`. Llamar antes de que esté listo produce un error explícito.

El specifier WASM tiene dos formas, y el resto de reglas cambia según la forma:

| Forma | Ejemplo | Declaración en `dependencies` | Import map del navegador | `no run --js` |
| --- | --- | --- | --- | --- |
| bare (paquete npm) | `@sqlite.org/sqlite-wasm` | **obligatoria** | resuelto vía `--esm-cdn` (por defecto `https://esm.sh`) | requiere `npm install` |
| relativa (archivo local) | `./math-wasm.mjs` | **no hace falta** | **no entra en el map** (el navegador no reasigna specifiers relativos) | enlaza automáticamente ese archivo (y sus propias importaciones relativas) en el directorio temporal |

La forma relativa se usa cuando «el módulo está en el proyecto»: con `no build --js` basta con que el artefacto y el módulo estén en el mismo directorio; `no run --js` enlaza el módulo en el directorio temporal del bundle; para el navegador hay que copiar el módulo también a `dist/` (el `import` se resuelve respecto a la ubicación del archivo).

### Ejemplos completos

- `example/native-libm/`: declara `pow` de libm con `#{bind = auto, c}`; `no run --js main.no` imprime `1024` (`no run` requiere haber ejecutado antes `npm install` en ese directorio para instalar `koffi`).
- `example/native-wasm/`: módulo WASM local (`math.wasm` + `math-wasm.mjs`, cero dependencias npm); `no run --js main.no` imprime `42.25` y `42`; el import map de `no build --js --browser main.no` no incluye specifiers relativos.

### Limitaciones conocidas (v1)
- WASM solo hace **reenvío de exportaciones con el mismo nombre**, sin generar una capa de adaptación de API (diferencias semánticas como `better-sqlite3` → `sql.js` requieren que usted proporcione un módulo wrapper en Nolang).
- La estrategia es **de programa**: una compilación JS solo permite una estrategia `bind`; la asociación por módulo de varias bibliotecas nativas queda para el futuro.
- FFI solo enlaza bibliotecas compartidas con ABI C; para addons nativos de Node (`.node`) use `bind=wasm` o una importación ESM normal.

## Ejemplo completo

```no
; main.no — demostración de aplicación para navegador
# js/dom
# js/canvas
# js/events
# js/storage

print('=== Nolang Browser Demo ===')

; DOM: crear un título y adjuntarlo al body
heading = dom.create-element('h2')
heading.set-text('Hello from Nolang!')
body = dom.body()
body.append-child(heading)

; DOM: crear un botón
btn = dom.create-element('button')
btn.set-text('Click me')
btn.set-style('margin', '8px')
body.append-child(btn)

; eventos: clic del botón
events.on-click(btn, () {
    print('button was clicked!')
})

; Canvas: dibujar rectángulos
canvasEl = dom.create-element('canvas')
canvasEl.set-attr('width', '240')
canvasEl.set-attr('height', '140')
body.append-child(canvasEl)

ctx = canvas.get-context-2d(canvasEl)
ctx.set-fill('red')
ctx.fill-rect(10, 10, 60, 60)
ctx.set-fill('blue')
ctx.fill-rect(80, 10, 60, 60)

; localStorage: guardar y leer
storage.set-item('greeting', 'Hello from localStorage')
g = storage.get-item('greeting')
print('stored:', g)

; iteración
nums = [10, 20, 30]
i <- nums: {
    print('elem:', i)
}

; rama de plataforma
#{js-browser}
print('running in browser mode')

print('=== done ===')
```

Compilación:

```bash
no build --js --browser main.no
# 輸出: dist/main.js, dist/main.html
# 在瀏覽器中打開 dist/main.html
```
