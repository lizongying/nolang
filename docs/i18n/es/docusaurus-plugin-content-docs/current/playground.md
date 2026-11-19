---
sidebar_position: 5
---

# Playground

Nolang Playground es un entorno en línea donde puede editar, compilar y ejecutar programas Nolang directamente en el navegador. Compila el compilador de Nolang (`no.wasm`) y el servidor de lenguaje (`lsp.wasm`) a WebAssembly y ejecuta el código del usuario dentro del entorno aislado del navegador mediante WASI, sin necesidad de instalar ninguna cadena de herramientas localmente.

Ir ahora: [**/playground**](/playground)

## Uso

La página del Playground se divide en dos paneles:

- **Editor izquierdo**: un editor de código basado en [CodeMirror 6](https://codemirror.net/), con resaltado de sintaxis de Nolang, autoindentación e historial.
- **Output / stderr / Diagnostics derecho**: resultados de ejecución, salida de error estándar y mensajes de diagnóstico.

La interfaz cambia automáticamente entre chino simplificado e inglés según el idioma de Docusaurus, y los paneles de salida admiten tanto el tema claro como el oscuro.

### Barra de herramientas

| Botón        | Función                                                            |
| ------------- | ------------------------------------------------------------------- |
| **▶ Run**     | Compilar el código actual a WASM y ejecutarlo mediante WASI; stdout se muestra en el panel Output de la derecha. |
| **Format**    | Llamar a `no fmt` a través del LSP para formatear el código actual. |
| **Examples**  | Cargar un ejemplo integrado agrupado por categoría, reemplazando el contenido actual del editor. |

El lado derecho de la barra de herramientas muestra el estado actual (inactivo / compilando / ejecutando / completado / error); debajo del menú desplegable se muestra la descripción bilingüe (chino e inglés) del ejemplo. El último ejemplo seleccionado se guarda en el almacenamiento local del navegador.

### Ejemplos

El menú desplegable de Examples incluye **24 ejemplos** agrupados por tema, todos validados en CI mediante exactamente la misma ruta de compilación `--wasm-direct` que usa el navegador:

| Categoría | Cantidad | Contenido |
| -------- | ----- | ------ |
| Básicos | 7 | Hello World, inferencia de tipos, funciones y recursión, Fibonacci, aritmética y operaciones a nivel de bits, salida formateada, cadenas |
| Control de flujo | 6 | Guardas de cortocircuito, coincidencia de patrones, FizzBuzz, cuatro formas de bucle por rango, bucle while condicional, break / continue |
| Estructuras de datos | 4 | Arrays de longitud fija, segmentos dinámicos, estructuras, métodos |
| Tipos escalares | 4 | char, bool, f64, byte |
| Biblioteca estándar | 2 | math.max / math.min / sqrt, rand.rand |
| Manejo de errores | 1 | option y nil con match de tres ramas |

Cada ejemplo lleva una subcadena de salida esperada y estable; CI compila cada ejemplo, lo ejecuta con `wasmtime` y comprueba que esa subcadena aparece en stdout. Así, los ejemplos no quedan obsoletos silenciosamente cuando evoluciona el lenguaje o el backend Direct-WASM.

Al seleccionar cualquier ejemplo, el Playground limpia el Output, stderr y Diagnostics actuales y reemplaza el editor con el contenido de ese ejemplo.

### Diagnósticos

Cuando el LSP detecta errores de sintaxis o de tipos, aparecen en el área de Diagnostics, abajo a la derecha. Al hacer clic en cualquier elemento de diagnóstico se navega a la línea y columna correspondientes. Los mensajes `file:line:col` del stderr del compilador se analizan además como marcadores de **Run Errors**, eliminando automáticamente los TraceID finales y los prefijos de gravedad localizados.

### Errores y tiempos de espera

Antes, los fallos de carga de WASM solo se escribían en `console.warn`, y el usuario se encontraba con un botón Run que no hacía nada. El comportamiento actual es el siguiente:

- Si no se pueden descargar o compilar `no.wasm` o `lsp.wasm`, aparece en la parte superior de la página una bandera de error que se puede cerrar y que indica qué recurso falló y el motivo.
- Un fallo de inicialización del servidor de lenguaje también muestra una bandera; el editor sigue funcionando, pero no están disponibles la finalización automática, la información al pasar el cursor ni el formateo.
- Cada ejecución es cronometrada por un vigilante (watchdog) en el hilo principal (8 segundos por defecto). `wasi.start()` es una llamada síncrona dentro del worker, por lo que un bucle infinito impide que se dispare cualquier temporizador de ese worker: la única forma de detenerlo realmente es terminar el worker desde el hilo principal. Tras un tiempo de espera agotado, el worker se termina, se recrea automáticamente en la siguiente ejecución y el precalentamiento de WASM se reinicia de inmediato.
- Los clics repetidos en Run se rechazan mediante una protección de concurrencia, y los temporizadores de anti-rebote de ediciones pendientes se cancelan al cambiar de ejemplo o al formatear, para evitar que una entrada obsoleta sobrescriba el contenido nuevo.

`no.wasm` y `lsp.wasm` se descargan y se compilan en flujo durante los periodos de inactividad del navegador, de modo que la primera pulsación de Run ya no tiene que esperar a que se descarguen y compilen los 27 MB completos.

## Limitaciones

Como el Playground se ejecuta dentro de un entorno aislado WASM del navegador, algunas funciones de Nolang no están disponibles:

### Sin compatibilidad con FFI

El navegador no puede enlazar bibliotecas C nativas, por lo que las declaraciones FFI `#{c}`, `#{cpp}`, `#{rust}` no se pueden usar en el Playground. El código que implica FFI (como los controladores `sqlite` o `mysql`) no se puede ejecutar.

### Sin soporte para fork / exec / pipe

WASI preview1 no ofrece API de gestión de procesos, por lo que la biblioteca estándar `process` (`process.start`, `process.wait`, etc.) no está disponible en el Playground.

### Sockets de red no disponibles

WASI preview1 no ofrece API de sockets, por lo que las siguientes bibliotecas estándar no están disponibles en el Playground:

- `net` (listener / conn TCP)
- `http` / `http2` / `http3` (cliente HTTP)
- `ws` (WebSocket)
- `tls` (conexión TLS)
- `quic`, `sse`, `dns`

**Una restricción más estricta para `no` / `lsp` en sí**: los artefactos wasm ni siquiera pueden *importar* una función de socket. El navegador los instancia mediante `@wasmer/wasi`, que no implementa `sock_accept` / `sock_shutdown`, así que un artefacto que arrastra `net` / `net/http` a la enlazación wasip1 lanza:

```
Failed to instantiate WASI: RuntimeError: JsValue(LinkError: WebAssembly.Instance():
Import #28 "wasi_snapshot_preview1" "sock_accept": function import requires a callable)
```

Como consecuencia, el programa ni siquiera llega a `_start`: el servicio de lenguaje informa de un «fallo de inicialización» (sin finalización automática, información al cursor ni formateo), y el botón de ejecución falla igualmente. Por ello, todo código de red que solo tiene sentido en un entorno nativo se coloca en archivos con `!wasip1`, y en el lado wasip1 solo quedan los stub de error: `cmd/lsp/pprof_wasip1.go`, `cmd/no/serve_esm_wasip1.go`,
`package/download_net_wasip1.go`, `package/release_net_wasip1.go`.
`bun scripts/wasm_imports_check.ts` (`make wasm-imports-check`) protege esta invariante usando la propia tabla de importaciones del motor.

### `poll_oneoff` lo suministra el front end

Una tabla de importaciones limpia solo significa que las funciones existen, no que funcionen. El cuerpo de `poll_oneoff` de `@wasmer/wasi` es un `unimplemented!()` de Rust: en cuanto se llama —incluso con cero suscripciones— ejecuta `unreachable` y hace que toda la instancia falle (trap), con un error similar a este:

```
Error while running start function: RuntimeError: JsValue(RuntimeError: unreachable)
```

El runtime de Go convierte tanto `time.Sleep` como `netpoll(delay)` en una única suscripción a un reloj monotónico, por lo que **todos los programas Go se topan con esto**: `no fmt` / `no vet` / `no build` morían la primera vez que se dormían, mientras la comprobación de importaciones seguía en verde.

La solución está en `docs/src/playground/wasm-loader.ts`: `instantiateWasi()` construye él mismo el objeto de importaciones a partir de `wasi.getImports()` y sustituye `poll_oneoff` por una implementación en JS (las suscripciones a reloj duermen hasta el vencimiento más cercano y notifican el evento de reloj; las suscripciones solo a fd notifican «listo» de inmediato para que una espera nunca se bloquee indefinidamente). Tenga en cuenta que la sobrescritura **no se puede** pasar a `wasi.instantiate(module, overrides)`: las funciones propias del glue prevalecen, la sobrescritura se ignora silenciosamente y el programa sigue fallando. Como el objeto de importaciones se construye aquí, hay que pasar explícitamente la instancia a `wasi.start(instance)`.

### Tiempo de compilación de la primera ejecución

Run utiliza `no build --wasm-direct` para generar el wasm. En la práctica, el hello world tarda unos **0,5 segundos** dentro de wasm (el `no` nativo con el mismo comando, unos 0,6 segundos; ambos después de la precompilación del índice std de 2026-11; antes eran de unos 55 y 11 segundos respectivamente).

El cuello de botella nunca estuvo en codegen, sino en **analizar toda la biblioteca estándar dos veces en cada arranque en frío**: `ValidateUndefinedVars` recopila los símbolos exportados de los 119 módulos std para reconocer llamadas cualificadas como `math.max(...)`; `CollectStdUnionAliases` volvía a analizar independientemente el mismo lote de módulos para encontrar alias de unión. Ahora `genstdsig` hornea juntas «los símbolos exportados de cada módulo» y «qué módulos tienen alias de unión» en `stdsig_gen.go`, y en tiempo de ejecución se consulta directamente la tabla; además hay dos guardas (`hir.TestModuleExportsMatchChecker` compara módulo por módulo y campo por campo el resultado calculado en HIR, y `TestUnionAliasNarrowingMatchesFullScan` compara el análisis estrecho con el análisis completo), de modo que una tabla obsoleta hace fallar los tests en lugar de cambiar la semántica silenciosamente.

El vigilante conserva sus dos presupuestos («carga + compilación: 180 segundos / programa del usuario: 5 segundos»): el primero tiene ahora muchísimo margen y se mantiene para absorber arranques en frío, programas grandes y dispositivos lentos.

### Sistema de archivos virtual

WASI preview1 solo proporciona los tres descriptores de archivo stdin / stdout / stderr; no hay un sistema de archivos real. Por ello, `fs`, `path`, `os.get-env`, `os.set-env` y otras bibliotecas estándar relacionadas con archivos o variables de entorno pueden no funcionar como se espera en el Playground.

### Límite de memoria

El WASM de navegador suele estar limitado por un espacio de direcciones de 32 bits; el techo de memoria utilizable es de aproximadamente **2 GB a 4 GB** (según el navegador y el sistema operativo). Los programas que requieren mucha memoria (como la recursión profunda o los arrays grandes) pueden fallar por falta de memoria.

## Compatibilidad con navegadores

El Playground requiere un navegador moderno con compatibilidad con WebAssembly. Se recomienda usar las siguientes versiones o posteriores:

| Navegador   | Versión mínima |
| --------- | --------------- |
| Chrome    | 100+            |
| Firefox   | 100+            |
| Safari    | 16+             |
| Edge      | 100+            |

Si el navegador no admite WebAssembly o tiene JavaScript desactivado, el Playground no podrá cargar el editor ni ejecutar código.

## Desarrollo y validación

Tras modificar el Playground, puede ejecutar las siguientes comprobaciones desde un árbol de compilación limpio:

```bash
make playground-demos                       # 編譯 + 在行程內用 playground 自己的 WASI loader 執行全部範例
make playground-demos DEMO_RUN=wasmtime     # 改用外部 wasmtime 執行（交叉比對用）
make playground-demos DEMO_RUN=none         # 只編譯、不驗輸出（不可當發佈門檻）
make wasm-imports-check                     # 重建兩個 wasm 並驗證導入表無 socket 函式
make wasm-runtime-smoke                     # 用 worker 的同一條載入路徑實跑 version/fmt/vet
RUN_WASM_LSP=1 RUN_WASM_BUILD=1 make wasm-runtime-smoke   # 追加語言服務與完整編譯+執行
bun scripts/playground_ui_check.ts          # 診斷解析、看門狗、worker 恢復、i18n
make playground-smoke                       # dev server 與 WASM 路由 HTTP 200
cd docs && bun run typecheck                # TypeScript 型別檢查
```

El compilador de `playground-demos` es el `no` **nativo**, pero los artefactos siguen la ruta del navegador `--wasm-direct -target wasm32-wasi` y se ejecutan mediante `instantiateWasi()` de `wasi-glue` + `wasm-loader`, exactamente el mismo código que ejecuta `runner.worker`. Por eso no hace falta instalar wasmtime y CI puede ejecutarlo directamente. Comprueba tres cosas a la vez: que el snippet compila, que el artefacto pasa la validación de WebAssembly (los errores de pila vacía del tipo `local.set` con coincidencia de valor de campo aparecen aquí) y que la salida impresa coincide con `expect` (las desviaciones del backend Direct-WASM, como silenciar un `?=` devolviendo `0`, también aparecen aquí).

Lo único que no puede detectar es la tabla de importaciones de `no.wasm` / `lsp.wasm` en sí (esos dos artefactos no se generan a partir de snippets); `wasm-imports-check` cubre ese hueco: cuando `@wasmer/wasi` no tiene `sock_*`, `WebAssembly.Instance` lanza `LinkError` y todo el playground (incluido el servicio de lenguaje) no logra arrancar. El worker añade otra capa: `instantiateWasi()` de `wasm-loader.ts` sustituye cualquier función WASI que `@wasmer/wasi` no proporcione por un stub que devuelve `ENOSYS` / `NOTSUP`, de modo que una importación inesperada degrade a una llamada al sistema fallida en lugar de derribar todo el servicio.

`wasm-imports-check` tampoco detecta «la función existe pero su cuerpo hace trap» (la clase de `poll_oneoff` descrita arriba), así que `make wasm-runtime-smoke` cierra el último hueco: recorre la ruta real de `instantiateWasi()` para ejecutar `no version` / `no fmt` / `no vet`, y puede verificar además el servicio de lenguaje y una compilación + ejecución completas.

`docs/static/wasm/*.wasm` está excluido por `.gitignore` y los artefactos locales nunca se deben confirmar. CI reconstruye `no.wasm` y `lsp.wasm` a partir del mismo árbol de código fuente y con el mismo `StdSigKey`, para garantizar que el compilador del navegador, su blob de AST std incrustado y el backend Direct-WASM proceden de una única compilación. El desajuste de versiones entre estos tres era la causa habitual de los antiguos traps `unreachable` y errores de validación WASM.

CI (`pr-verify` en `.github/workflows/build.yml` y la compilación del sitio en `static.yml`) ejecuta en orden los tests de Go, la comprobación de tipos, las comprobaciones de UI y la verificación de ejemplos, y solo entonces reconstruye el WASM desde un árbol limpio y despliega el sitio.
