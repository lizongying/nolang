---
sidebar_position: 2
---

# Instalación y uso

## Instalación

### Instalar la CLI

Descargue el ejecutable para su plataforma desde [GitHub Releases](https://github.com/lizongying/nolang/releases/latest), o instálelo con el siguiente método:

```bash
# macOS / Linux
# 1. 下載二進制文件
# 2. 放到 PATH 中
sudo mv nolang /usr/local/bin/no
```

### Instalar la extensión de VS Code

Instale la extensión de Nolang desde [VS Code Marketplace](https://marketplace.visualstudio.com/items?itemName=lizongying.vscode-nolang) para obtener resaltado de sintaxis, diagnósticos LSP, ir a la definición, autocompletado y más.

## Comandos de la CLI

| Comando                                                         | Descripción             |
| ------------------------------------------------------------ | ----------------------- |
| `no version`                                                 | Imprimir información de versión |
| `no init`                                                    | Definir el espacio de trabajo (genera workspace.noml, sin package.noml) |
| `no new <name>`                                              | Crear un paquete nuevo dentro del espacio de trabajo (subdirectorio + package.noml, registrado en workspace.noml) |
| `no fmt [-w] [-d] <file\|dir>`                               | Dar formato al código fuente (dedup/ordena las importaciones, elimina automáticamente las importaciones de módulo std sin alias); los archivos de configuración `.noml` siguen la norma de maquetación que preserva el diseño (indentación/espacios/líneas en blanco, sin tocar los comentarios ni el orden de las claves) |
| `no build [-o <file>] [-cc <s>] [-target <s>] [-opt[=N]] [<file\|dir>]` | Compilar (genera un ejecutable) |
| `no run [-cc <s>] [-target <s>] [<package\|dir\|file>]`        | Compilar y ejecutar (nombre de paquete/directorio/archivo) |
| `no test [-cc <s>] [-target <s>] [<file>]`                   | Ejecutar pruebas                |
| `no add <tool>[:<version>]@<host>/<owner>/<repo>`            | Añadir una dependencia (módulo AST precompilado) |
| `no remove <pkg>`                                            | Eliminar una dependencia                |
| `no update <pkg>`                                            | Actualizar una dependencia                |
| `no update-all`                                              | Actualizar todas las dependencias            |
| `no list`                                                    | Listar dependencias                |
| `no sync`                                                    | Sincronizar dependencias                |
| `no install [-u] [<tool>[:<version>]@<host>/<owner>/<repo>]`  | Instalar un binario             |
| `no uninstall <name>`                                        | Eliminar un binario             |
| `no pack [-o <file>] [<dir\|file.no>]`                       | Empaquetar el AST (`<tool>-ast.zip`) |
| `no pub [-o <file>]`                                         | Igual que `no pack` (carga al registro no implementada) |
| `no cache <stat\|clear>`                                     | Ver / vaciar la caché de AST (normalmente no hace falta vaciarla) |

## Inicio rápido

Nolang usa una estructura de **un único repositorio (proyecto) y varios paquetes**: la raíz del repositorio es el espacio de trabajo (solo tiene `workspace.noml`) y cada paquete es un subdirectorio (con su propio `package.noml`). `no init` y `no new` son dos acciones independientes:

- `no init` — define el espacio de trabajo en el directorio actual (solo genera `workspace.noml`).
- `no new <name>` — crea un paquete nuevo bajo el espacio de trabajo actual (genera el subdirectorio `./<name>/` y su `package.noml`) y registra ese paquete en `workspace.noml`.

### Inicializar el espacio de trabajo y crear un paquete

```bash
# 1) 在倉庫根目錄定義工作區
no init

# 2) 在工作區內新建包（會自動註冊到 workspace.noml）
no new foo

# 3) 進入包目錄
cd foo

# 直接運行（自動構建並執行 main.no）
no run
```

Puede usar `no new` repetidamente dentro del espacio de trabajo para añadir más paquetes:

```bash
no new bar
no new baz
```

En este momento, `workspace.noml` tiene esta forma:

```noml
{
  foo: '/foo'
  bar: '/bar'
  baz: '/baz'
}
```

> **Formato de rutas**: los valores de asignación en `workspace.noml` usan el prefijo `/` para indicar rutas relativas al espacio de trabajo (p. ej. `/foo` → `workspaceRoot/foo`). También se admiten saltos relativos `./`, `../` y rutas absolutas del sistema operativo (p. ej. `/home/user/code/fork`) para apuntar a directorios fuera del espacio de trabajo.

Cuando se ejecuta `no build` sin argumentos desde el directorio raíz, compila en paralelo todos los paquetes del espacio de trabajo.

### Solo inicializar el espacio de trabajo (sin crear un paquete)

```bash
# 在當前目錄定義工作區
no init
```

`no init` solo genera `workspace.noml` (inicialmente un `{}` vacío); **no genera `package.noml` ni `main.no`**. Si `workspace.noml` ya existe, no se sobrescribe. Los paquetes se crean mediante `no new <name>`.

### Compilación y ejecución

```bash
# 構建（默認尋找 main.no）
no build                    # 構建當前目錄
no build main.no            # 構建指定文件
no build -o output          # 指定輸出路徑
no build -cc zig            # 使用 Zig 編譯器
no build -target x86_64-linux-gnu  # 交叉編譯（指定目標平台）

# 運行（構建 + 執行）
no run                      # 構建並執行當前目錄的 main.no
no run foo                  # 運行工作區中名為 foo 的包（解析 workspace.noml）
no run ./foo                # 運行 ./foo 目錄的 main.no
no run main.no              # 運行指定 .no 文件
no run -cc zig
no run -target aarch64-macos-gnu
```

> `no build -v` también escribe el LLVM IR sin procesar junto a la ruta de salida (`<ruta de salida>.ll`). `no build --shared -o libdemo.dylib main.no`, por su parte, exporta como símbolos ABI de C estables las funciones de nivel superior marcadas con `#{cabi='symbol'}`, enlaza una biblioteca dinámica con `-shared -fPIC` y genera automáticamente un archivo de encabezado `.h` del mismo nombre; se puede sobrescribir la ruta del encabezado con `--header`. Por ahora no existe la opción `-c` (solo compilar, sin enlazar). Para que C / C++ / Rust / Go / Java / Python / C# llamen a Nolang, consulte [Llamar a Nolang desde lenguajes anfitriones](./lang/host-interop).

El argumento posicional de `no run` se resuelve en el siguiente orden:

1. Un archivo `.no` existente → ejecutar directamente ese archivo
2. Un directorio existente → ejecutar su `main.no`
3. Un nombre de paquete registrado en el `workspace.noml` más cercano → ejecutar el `main.no` de ese paquete

Sin argumentos, ejecuta el `main.no` del directorio actual. Si el directorio actual es la raíz de un espacio de trabajo (tiene `workspace.noml` pero no `main.no`), se le pedirá que especifique un paquete con `no run <package>`.

### Objetivos de compilación cruzada

El formato del parámetro `-target` es `<arch>-<os>-<abi>` y admite los siguientes objetivos:

| Tripleta de destino  | Descripción    |
| -------------------- | -------------- |
| `x86_64-linux-gnu`   | Linux x86_64   |
| `aarch64-linux-gnu`  | Linux ARM64    |
| `x86_64-macos-gnu`   | macOS x86_64   |
| `aarch64-macos-gnu`  | macOS ARM64    |
| `x86_64-windows-gnu` | Windows x86_64 |

**Detección automática de la plataforma actual**: `no build`, `no run` y `no test` detectan automáticamente la plataforma anfitriona actual y compilan para el destino nativo cuando no se especifica `-target`. En el desarrollo cotidiano no hace falta indicar el objetivo manualmente:

```bash
no run hello.no          # 本機直接跑
no test                  # 本機跑測試
no build -target aarch64-linux-gnu   # 需要交叉編譯時才顯式指定
```

### Selección del compilador

El parámetro `-cc` especifica el backend del compilador C:

- `clang` (predeterminado) — requiere LLVM instalado
- `zig` — requiere Zig instalado; adecuado para compilación cruzada

### Diseño de los tipos integrados (umbral de inlining de `?T`)

La disposición en memoria de `?T` se define **según el tipo de la carga útil**: cada tipo de carga `T` tiene su propio
`%option_<key> = { i8 tag, [K x i64] }` (una etiqueta de 1 byte, carga fija en el desplazamiento de byte 8,
`K = roundUp8(sizeof(T))/8`). Si la carga no cabe en el umbral, se encapsula en el montón y la primera palabra de la zona de carga guarda el puntero.

`--option-payload-inline-threshold=N` especifica ese umbral (medido sobre **toda la estructura option**,
es decir, `1 + 7 + sizeof(T)`):

```bash
no build --option-payload-inline-threshold=1032 main.no   # 讓更大的載荷內聯
no build --option-payload-inline-threshold=16 main.no     # 最小可配置值
```

| Valor | Efecto |
| --- | --- |
| predeterminado **264** (= 1 + 7 + 256) | `?i64` 16 B, `?str` 24 B, `?txt` (256 B) 264 B en línea; las cargas más grandes se encapsulan |
| mínimo **16** | solo las cargas del tamaño de `?i64` siguen en línea; `?str` (24 B) y superiores se encapsulan |
| < 16 | error de compilación |

Prioridad: línea de comandos > variable de entorno `NOLANG_OPTION_PAYLOAD_INLINE_THRESHOLD` > `compiler.option-payload-inline-threshold` de `package.noml` > valor predeterminado. La única fuente de verdad del diseño es `src/mir/layout.go`; la semántica (qué valores cuentan como `ok` / `nil` / `err`) **no se ve afectada**: solo cambia el consumo de memoria y si la carga se encapsula o no.

### Optimizador integrado del compilador (MIR opt)

El compilador incluye un optimizador que actúa en la **capa MIR** (después del descenso desde HIR y antes de la generación de código LLVM). Es algo distinto del `opt` de LLVM del backend: el primero reescribe MIR y el segundo reescribe el IR LLVM ya generado.

Se activa con `-opt[=N]`, **desactivado por defecto**:

```bash
no build main.no          # 預設：不啟用（與歷史行為一致）
no build -opt main.no     # 等同 -opt=1
no build -opt=2 main.no   # 常數折疊 + 死碼消除 + 控制流清理
no build -opt=0 main.no   # 顯式關閉
```

| Nivel | Significado |
| ---- | ---- |
| `0` / `off` / `false` / `no` / `none` | Desactivado (predeterminado) |
| `1` / `on` / `true` / `yes` / `-opt` | Plegado de constantes + reescritura de identidades enteras + propagación de copias + eliminación de escalares muertos |
| `2` | Nivel 1 + limpieza del flujo de control (plegado de ramas constantes, eliminación de bloques inalcanzables, concatenación de saltos de bloques vacíos, fusión de bloques con un único predecesor) + recorte de epílogos |

`-opt` es un interruptor **global** (se procesa en `main`, igual que `-v`), por lo que lo admiten `build`, `run` y `test`.

El mismo interruptor también se puede especificar con la variable de entorno `NOLANG_MIR_OPT`; el flag de línea de comandos solo escribe en esa variable, por lo que no puede haber discrepancia entre ambos. Para ver qué hizo la optimización en esta ejecución:

```bash
NOLANG_MIR_OPT=2 NOLANG_MIR_OPT_STATS=1 no build -o out main.no
# [miropt] level=2 funcs=3 folded=5 (arith=2 cmp=3) ident=1 copies=4 dead=6 cfg{condbr=3 unreachable=4(5 insts) threaded=1 merged=5}
# [miropt] prelude: dropped 27 unreferenced helpers
```

| Campo | Significado |
| ---- | ---- |
| `folded` (`arith`/`cmp`) | Número de instrucciones aritméticas/de comparación plegadas a constantes |
| `ident` | Identidades enteras simplificadas a alguno de sus operandos (`x+0`, `x*1`, `x&x`, `x^0`…) |
| `copies` | Copias escalares eliminadas mediante propagación de copias |
| `dead` | Instrucciones escalares muertas eliminadas |
| `cfg{condbr, unreachable, threaded, merged}` | Plegado de ramas constantes, eliminación de bloques inalcanzables (con el número de instrucciones), concatenación de saltos de bloques vacíos, fusión de bloques con un único predecesor |
| `prelude: dropped N` | Número de helpers de runtime del preludio de codegen que se demostró que no se usan (véase más abajo) |

### Recorte del preludio (nivel 2)

`emitPrelude` escribe todo el runtime (`str_*`, `print_*`, `eprint_*`, `vec_*`, `nolang_*`) en **cada** módulo, tanto si el programa lo usa como si no: un programa con `print('hi')` genera 60 definiciones de función, de las cuales solo 8 son alcanzables desde la entrada, por lo que **52 nunca se llaman**.

**Esto `opt -O2` no puede hacerlo**: el GlobalDCE de LLVM solo elimina definiciones `internal`, mientras que el preludio se emite con enlazado externo (los módulos `no` pueden enlazarse con C y los helpers que el enlazador podría necesitar no pueden ser internal). Medido en ese programa, exactamente 7 de las 60 definiciones son `define internal`, `opt -O2` elimina justo esas 7 y, además, el `.ll` crece de 61.812 a 69.874 bytes (+8.062).

Por ello, el nivel 2 realiza, una vez generado el IR, un recorte de alcanzabilidad sobre el **texto ya terminado**, conservando solo los helpers realmente referenciados (y, recursivamente, los que ellos llaman). El mismo `print('hi')`: definiciones de 60 a **16**, `.ll` de 61.812 a **14.548** bytes (**−76,5 %**), ejecutable de 36.616 a 34.648 (−5,4 %). El recorte solo reconoce referencias reales: **los nombres que aparecen en comentarios no cuentan**, y esa es la clave de que pueda eliminar 44 y no 33; además, al descartar una función **se descarta también su propio bloque de comentarios descriptivo** (`optTrailingComment`), pues si no quedarían 12.609 bytes describiendo 44 funciones que ya no existen.

Eliminar comentarios **no reduce el ejecutable**: los comentarios se descartan al analizar con `opt`/`llc`, y ambos ejecutables son de 34.648 bytes. Esos 12.609 bytes son una ganancia en texto IR y en cantidad analizada durante la compilación.

La aproximación del recorte siempre se inclina a conservar: se conservan los bloques cuyo nombre único no se puede resolver, se detiene el recorte desde un bloque que no tiene llave de cierre, y `declare`/globales/definiciones de tipos nunca se eliminan. Pasar por alto una optimización solo cuesta unos bytes más; borrar algo incorrecto provoca un error de enlazado.

**¿Se puede eliminar el preludio por completo? No.** En la región del preludio no solo hay funciones, sino también **los literales de cadena del propio programa** (`@.mir.str.N`, es decir, `"hi"` en sí), tipos como `%str-long` / `%option` / `%task` y declaraciones externas como `@malloc` / `@write` / `@llvm.memcpy`: esos datos representan cerca del 30 % del módulo y recortarlos ni siquiera deja pasar `llc` (`error: use of undefined value '@.mir.str.1'`). Lo único que se puede eliminar es la mitad correspondiente a **funciones**: al sustituir los 48 `define` por `declare` del mismo nombre, los símbolos no definidos que reporta el enlazador son exactamente cuatro: `print_nl`, `print_str`, `str_free` y `str_from_const`. Así que la pregunta correcta es «¿se puede emitir bajo demanda?», no «¿se puede eliminar?».

Este despilfarro es un **impuesto fijo, independiente del tamaño del programa**: el código y los comentarios del preludio son idénticos módulo a módulo (48 definiciones, 36.043 bytes de código + 17.516 de comentarios) y, en una muestra de 40 archivos del corpus, la retención es de **0 a 19, con mediana 9**. `tests/dup.no` / `tests/q.no` / `tests/u128-types.no` retienen **0**: todo el preludio es código muerto para ellos. Por tanto, el porcentaje de ahorro lo decide por completo el tamaño del módulo: −76,5 % para un programa de una línea y solo −4,1 % para `test-std/src/pbkdf2.no`, de 3,0 MB.

¿Se puede eliminar el preludio por completo? No: en la región también están los literales de cadena del propio programa (`@.mir.str.N`), los tipos y las declaraciones externas (1.151 bytes), y recortarlos ni siquiera deja pasar `llc`. Además, `extraFuncs` / `extraFuncsBody` de `codegen.go:75-83` ya son un emisor bajo demanda (los helpers de option／struct／enum／ptr pasan por ahí); el preludio es la única parte no convertida, pero hacerlo bajo demanda solo aportaría casi exclusivamente comentarios: en la parte de código ya no queda nada que ganar.

El optimizador solo pliega **escalares** (`i64`/`u64`/`i16`/`i32`/`char`/`bool`/`f32`/`f64`). Se omiten sin excepción `byte`/`u8`/`i8`, `i128`/`u128`, `option`, segmentos, estructuras, etc.: los primeros no tienen rama correspondiente en `emitConst` (degenerarían a 0) y la aritmética de los segundos usa la ruta de envoltura por desbordamiento de `option`, de modo que plegarla cambiaría la semántica. Los números de coma flotante también quedan excluidos de todas las identidades: `x * 0.0` no es 0 para `inf`/`NaN` y `x + 0.0` no es `x` para `-0.0`.

La conclusión central de esa comparación merece exponerse aquí antes: **la limpieza local (plegado de constantes, eliminación de código muerto, limpieza del flujo de control) tiene un valor marginal casi nulo después de ejecutar `opt -O2`** (−0,06 %), porque LLVM hace el mismo trabajo de forma más exhaustiva. Lo que realmente renta es aquello que LLVM **no puede hacer por el linkage**: justamente el recorte del preludio descrito arriba. Con el recorte del preludio incluido (con recorte sensible a comentarios), el valor marginal global de `mir+llvm_O2` → `mir_opt+llvm_O2` es de **−19,05 % en instrucciones / −15,83 % en texto**; y la reducción de texto de `mir` → `mir+mir_opt` pasa de −14,29 % a **−17,75 %** (corpus completo de 631 archivos).

## Backend JS

El backend JS (`no build --js` / `no run --js`) compila el código fuente `.no` a JavaScript (con borrado de tipos),
y admite tanto destinos Node.js como de navegador, las anotaciones de plataforma `#{js}` / `#{js-browser}` y las importaciones ESM de terceros
(`# npm/<pkg>`).

La documentación completa de JS (flags de línea de comandos, mapeos de funciones integradas, la biblioteca estándar `js/*`, los import maps del navegador, etc.)
está concentrada en la página específica [Backend JS](./lang/js-backend); no se repite aquí.

## Reglas de punto de entrada

- **main.no** — punto de entrada del programa
- **lib.no** — punto de entrada de la biblioteca, exporta funciones (véase la [documentación de exportación](lang/export))
- **Todos los archivos .no bajo el directorio tests/** — contienen aserciones de prueba

## Pruebas

```bash
# 測試tests目錄下所有 .no 文件
no test

# 執行單個測試文件
no test my-test.no

# 使用指定編譯器或目標
no test -cc zig
no test -target x86_64-windows-gnu
```

Notas sobre las pruebas:

- Los archivos de prueba se colocan todos en el directorio tests/
- Cada archivo de prueba se compila de forma independiente
- Si alguna prueba falla, se devuelve un código de salida distinto de cero

## Caché de AST

`no build` / `no run` / `no test` guardan en disco el AST obtenido al analizar cada archivo fuente del usuario
(predeterminadamente `~/.cache/nolang/ast`, ruta que se puede cambiar con `$NOLANG_AST_CACHE_DIR`); si el contenido no ha cambiado, se reutiliza directamente,
ahorrando lex + parse. La biblioteca estándar usa el blob incrustado en el compilador y no ocupa esta caché.

**No hace falta limpiarla manualmente.** La clave de caché es

```
sha256( ASTVersion ‖ salt de fase de análisis ‖ contenido del archivo fuente )
```

donde uno de los componentes del salt es `checker.StdSigKey`, es decir, la firma de contenido de todos los archivos fuente enumerados en `src/hash.noml`
(biblioteca estándar + el propio compilador). Por tanto, cualquier cambio en el compilador o en la biblioteca estándar hace que el `no` reconstruido con `make`
use una clave nueva; las entradas antiguas simplemente no aciertan y no se reutilizan.

| Comando | Descripción |
| --- | --- |
| `no cache stat` | Muestra el directorio de caché, el número de entradas, el tamaño total y el `ASTVersion` actual |
| `no cache clear` | Vacía todas las entradas (solo para emergencias: cuando se sospecha que una entrada está obsoleta) |

Variables de entorno:

| Variable | Función |
| --- | --- |
| `NOLANG_AST_CACHE_OFF=1` | Desactiva la caché (útil para comparaciones A/B del compilador o para investigar un «error corregido que reaparece») |
| `NOLANG_AST_CACHE_DIR` | Directorio de caché personalizado. **También** afecta a la caché de `-ast.zip` de las dependencias AST precompiladas (esa caché está predeterminadamente en `~/no/cache/ast`, véase «Dependencias AST precompiladas» más abajo) |
| `NOLANG_AST_CACHE_MAX` | Límite de entradas de la caché (predeterminado 4096) |

> Las entradas antiguas simplemente dejan de acertar y se van descartando, por límite, a medida que se escriben entradas nuevas; por eso **no hace falta** borrar `~/.cache/nolang/ast`.
> Tenga en cuenta que `no` debe reconstruirse con `make gen` / `make no`: un binario compilado por su cuenta con `cd src && go build` no tiene inyectado `StdSigKey` y abandonará con un error en lugar de reutilizar la caché silenciosamente.

## Instalar y desinstalar binarios

### Instalación

```bash
# 安裝當前目錄的包（本地構建）
no install
no install -u                                   # 強制重構（更新）

# 從 GitHub Release 安裝預編 binary（推薦）：<tool>[:<version>]@<host>/<owner>/<repo>
no install nogit@github.com/lizongying/notools            # 最新版
no install nogit:v0.1.33@github.com/lizongying/notools    # 指定版本
no install nogit@lizongying/notools                       # 省略 host，默認 github.com
no install -u nogit@github.com/lizongying/notools         # 強制重新下載
```

La instalación remota descarga un **binario precompilado**, no código fuente: el nombre del recurso es `<tool>-<os>-<arch>-<version>` (con `.exe` en Windows), donde `<os>-<arch>` se toma de la plataforma en la que se ejecuta el propio `no` (p. ej. `darwin-arm64`, `linux-amd64`); por eso no se necesita una cadena de herramientas LLVM local y la herramienta instalada no tiene por qué estar escrita en Nolang. Si se omite la versión, se consulta `releases/latest`; definir `$GITHUB_TOKEN` (o `$GH_TOKEN`) evita el límite de la API de GitHub.

Proceso de instalación:

1. Remoto: resolver la spec → localizar el recurso del release → descargar; local: usar la compilación del directorio actual
2. Escribir el binario en `~/no/bin/`
3. Crear un enlace simbólico en `/usr/local/bin/`

### Desinstalación

```bash
no uninstall pkg-name
```

Al desinstalar se eliminan el enlace simbólico en `/usr/local/bin/` y el binario en `~/no/bin/`.

## Configuración del proyecto

### Formato de configuración noml

Todos los archivos de configuración de Nolang (`package.noml`, `workspace.noml`, `.workspace.noml`, `nolang.lock.noml`, `sum.noml`) usan el formato **noml** (Nolang Markup Language) propio, que sustituye al antiguo JSONC. Las reglas de sintaxis están alineadas con el propio lenguaje Nolang:

| Regla | Descripción |
| --- | --- |
| Objeto de nivel superior | Es un objeto: `{` y `}` ocupan cada uno su propia línea, un miembro por línea, **sin comas separadoras** (el salto de línea ya separa) |
| Clave | Por defecto un identificador desnudo, con las mismas reglas que un nombre de variable Nolang: minúsculas/dígitos/guion medio (interior)/guion bajo al inicio, no puede empezar por dígito (p. ej. `link-libs`, `anonymous-fn-type`, `dev-dependencies`); las claves que no cumplen la regla (rutas, URL, `@scope/pkg`) se escriben como claves de cadena entre comillas simples |
| Valor de cadena | Comillas simples, con escape `\`: `\' \\ \n \t` |
| Booleano | `true` / `false` (todo en minúsculas, igual que los literales bool de Nolang) |
| Número | Literal desnudo (reservado para archivos de bloqueo, etc.) |
| Array | `[`, elementos, `,` y `]` pueden ocupar varias líneas; los elementos se separan con **comas** y se **admiten comas finales**; un array vacío se escribe `[]` |
| Comentario | `;` hasta el final de línea (salvo dentro de cadenas) |
| Indentación | Sin significado (solo afecta a la legibilidad; no se garantiza el formato) |

`dependencies` / `dev-dependencies` son **arrays de cadenas** cuyos elementos tienen la forma `'clave:versión'` (la versión se puede omitir y entonces vale `*`), por ejemplo:

```noml
{
  dependencies: [
    '/example/test2',                          ; 本地包，默認任意版本
    'github.com/lizongying/nolang/test1:v0.1.0', ; 線上包，指定版本,
  ]
}
```

El archivo `package.noml` en la raíz del proyecto describe la información del proyecto:

```noml
{
  name: 'my-project'
  version: 'v0.1.0'
  description: 'A new Nolang project'
  keywords: []
  author: ''
  email: ''
  organization: ''
  repository: ''
  homepage: ''
  license: 'MIT'
  dependencies: [
    'fmt',,
  ]
  compiler: {
    version: 'v0.1.0'
  }
  ignore: []
}
```

`compiler.lang` fija el idioma de los mensajes de este proyecto (`'zh-tw'`, `'ja'`, etc.; misma notación que `--lang`): tiene prioridad sobre el locale del sistema, pero está por debajo de `--lang` y `NO_LANG`; si no se escribe, se sigue el sistema.

### Gestión de dependencias

```bash
# 添加依賴：<tool>[:<version>]@<host>/<owner>/<repo>
# 版本號可省略（取最新 release）；寫入 package.noml 的鍵是 <tool>@<host>/<owner>/<repo>
no add nogit@github.com/lizongying/notools
no add nogit:v0.1.33@github.com/lizongying/notools

# 移除依賴
no remove pkg-name

# 更新依賴
no update pkg-name

# 更新所有依賴
no update-all

# 列出依賴
no list

# 同步依賴（下載並生成鎖文件）
no sync
```

### Dependencias AST precompiladas

Las dependencias de la forma `no add <tool>@<host>/<owner>/<repo>` **solo tienen AST, no código fuente**: durante la compilación se descarga `<tool>-ast.zip` del release (zip with one .gob entry per module) y, una vez decodificado, se inyecta directamente en la tubería de compilación. Uso:

```nolang
# nogit                      # alias del paquete; los módulos son nogit.<module>
# nogit/remote                  # importar directamente el módulo remote (también nogit.remote)
# nogit.fn                      # nogit es solo el nombre del paquete; localizar el módulo que define fn
# <host>/<owner>/<repo>/nogit/remote
```

Un zip equivale a un paquete, que puede contener varios módulos, cada uno como entrada `<modulePath>.gob`. No hay módulo de entrada: `# nogit` solo crea un alias del paquete (los módulos se acceden como `nogit.<module>`, p. ej. `nogit.remote.greet(...)`); `# nogit/remote` importa un módulo directamente; `# nogit.fn` localiza un módulo por función. `no fmt` elimina los imports `# nogit` / `# nogit/remote` sin usar. La versión debe ser un valor concreto (`no sync` / la compilación dan error si encuentran `""` o `"*"`). El zip se guarda en caché en `~/no/cache/ast/` y se puede sobrescribir con `$NOLANG_AST_CACHE_DIR`.

El recurso de salida se produce con `no pack` (solo necesita lexer + parser, sin cadena de herramientas LLVM; ideal para CI):

```bash
no pack -o nogit-ast.zip nogit.no      # 單個源文件
no pack -o nogit-ast.zip ./nogit       # 一個包目錄
```

### Tipos de dependencia y reglas de número de versión

Las dependencias en `package.noml` se dividen en **paquetes locales** y **paquetes en línea**. El compilador determina automáticamente el tipo de dependencia según las reglas siguientes y emite una advertencia cuando un paquete local no usa la versión `"*"`.

#### Reglas de determinación

```
依賴鍵 → 查找 workspace.noml（短名稱或完整鍵匹配）
  ├─ 找到 → 本地包（應使用 "*"）
  └─ 未找到 → 檢查路徑前綴（/ 開頭）
      ├─ 是 → 本地包（應使用 "*"）
      └─ 否 → 線上包（使用版本號，不警告）
```

1. **Buscar en `workspace.noml`**: primero se busca la clave de dependencia en `workspace.noml` como nombre corto o clave completa. Si se encuentra, es un paquete local.
2. **Comprobar el prefijo de ruta**: si no se encuentra en `workspace.noml` y la clave de dependencia empieza por `/` (ruta relativa al espacio de trabajo), también es un paquete local.
3. **El resto son paquetes en línea**: una clave de dependencia que no cumple las condiciones anteriores se considera un paquete en línea y debe especificar un número de versión.

> **Restricción de frontera del espacio de trabajo**: las rutas locales en `package.noml` solo pueden empezar por `/` (relativas al espacio de trabajo); se prohíben `./`, `../` y las rutas absolutas del sistema operativo. La única entrada autorizada para cruzar la frontera del espacio de trabajo es el mapeo en `workspace.noml` / `.workspace.noml`. Véase [Restricción de frontera del espacio de trabajo](#工作區邊界約束).

#### Diversas formas de referenciar paquetes locales

Los paquetes locales admiten las tres formas de referencia siguientes; en todas ellas la versión debe ser `"*"`:

```noml
dependencies: [
  ; 1. 短名稱：workspace.noml 中註冊的鍵
  'test2',
  ; 2. 工作區相對路徑：以 / 開頭，相對工作區根目錄
  '/example/test2',
  ; 3. 完整 URL：若 workspace.noml 中有對應映射則為本地包
  'github.com/lizongying/nolang/test2',,
]
```

> **Nota**:
> - Si la tercera forma es un paquete local depende de si existe una asignación correspondiente en `workspace.noml`. Si `workspace.noml` asigna `github.com/lizongying/nolang/test2` a una ruta local, esa dependencia es un paquete local (debe usar `"*"`); de lo contrario es un paquete en línea (debe usar un número de versión).
> - Los prefijos `./` y `../` ya no se permiten en `package.noml`. Todas las rutas locales deben empezar por `/` (relativas al espacio de trabajo).

#### Uso avanzado: convertir un paquete en línea en local

`workspace.noml` no solo puede registrar nombres cortos, sino también asignar el nombre de un paquete en línea a una ruta local, lo que permite el modo de desarrollo «paquete en línea convertido en local»:

```noml
; workspace.noml
{
  test2: '/example/test2'
  'github.com/lizongying/nolang/test2': '/example/test2'
}
```

En este momento, una dependencia referenciada como `github.com/lizongying/nolang/test2` en `package.noml` se resuelve como el paquete local `/example/test2`, y la versión debe ser `"*"`. Esto resulta muy práctico al desarrollar localmente un paquete en línea: basta con añadir una asignación en `workspace.noml` para cambiar una dependencia remota por código fuente local y depurarla conjuntamente.

#### Advertencia de número de versión

Si durante la compilación se detecta que un paquete local usa un número de versión distinto de `"*"` (como `"v0.1.0"`), se emite una advertencia indicando que se use `"*"`. Los paquetes en línea no están sujetos a esta restricción.

#### Asignaciones recursivas de workspace (cadenas de mapeo entre paquetes)

Nolang admite **asignaciones recursivas de workspace**: un paquete del que se depende puede traer su propio `workspace.noml`, con más reglas de asignación en su interior, formando una cadena de resolución natural entre paquetes. Esta es una capacidad diferencial clave de Nolang: los `replace`/`patch` de Go/Cargo mayoritarios solo afectan al proyecto actual y no se propagan al interior de los paquetes dependientes.

**Escenarios de aplicación:**
- Unificar alias internos en bibliotecas base
- Estandarizar rutas de importación
- Migraciones de compatibilidad entre bibliotecas de varias capas

**Cómo funciona:**

Al resolver una clave de dependencia, el compilador:
1. Busca la clave en el `workspace.noml` actual
2. Si encuentra una asignación, comprueba si el directorio de destino trae su propio `workspace.noml`
3. Si lo trae, continúa buscando sobre la misma clave y resuelve nivel por nivel
4. Hasta que no haya asignaciones más profundas

**Ejemplo:**

```
workspace/                  ← espacio de trabajo superior
├── workspace.noml         ← {"testkey": "/pkgA"}
├── pkgA/
│   ├── workspace.noml     ← {"testkey": "/pkgB"}
│   └── pkgB/
│       ├── workspace.noml ← {"testkey": "/pkgC"}
│       └── pkgC/           ← destino final (sin workspace.noml)
```

Al resolver `testkey`:
1. Asignación del `workspace.noml` superior → `/pkgA` (relativo al workspace)
2. Asignación de `pkgA/workspace.noml` → `/pkgB` (relativo al workspace de pkgA)
3. Asignación de `pkgB/workspace.noml` → `/pkgC` (relativo al workspace de pkgB)
4. `pkgC` no tiene `workspace.noml` → resolución final en `pkgC`

**Detección de ciclos:**

El compilador mantiene una pila de visitas de resolución que registra las raíces de workspace ya visitadas. En cuanto detecta una asignación circular (p. ej. A→B, B→A), aborta de inmediato y muestra la cadena completa:

```
Error: circular workspace mapping detected: /path/to/A → /path/to/B → /path/to/A
```

> **Nota**: esta capacidad solo admite asignaciones recursivas naturales entre paquetes (el paquete dependiente trae su propio `workspace.noml`); no admite abreviaturas manuales encadenadas en un único archivo.

### Configuración de espejos

Configure direcciones de espejo en el array `mirrors` de `package.noml` para acelerar la descarga de paquetes remotos:

```noml
mirrors: [
  'https://mirror.example.com/',,
]
```

### Configuración del workspace workspace.noml

`workspace.noml` está en la raíz del repositorio y describe un workspace de **un único repositorio y varios paquetes**: es un mapeo de «nombre de paquete → ruta relativa». Cuando no recibe argumentos y existe `workspace.noml`, `no build` compila en paralelo todos los paquetes enumerados; `no run`, por su parte, ejecuta un paquete concreto del workspace mediante su nombre (p. ej. `no run foo`).

```noml
{
  foo: '/foo'
  bar: '/bar'
}
```

`no init` se encarga de definir el workspace: si `workspace.noml` no existe, genera un objeto vacío `{}` y **no genera ningún `package.noml`**; si ya existe, se deja intacto y no se sobrescribe. Cuando después se crea un paquete con `no new <name>`, se registra automáticamente `"<name>": "./<name>"` en `workspace.noml`.

#### Configuración privada local .workspace.noml

Además del `workspace.noml` público, Nolang admite un archivo de configuración privado local `.workspace.noml` (nótese el punto inicial). Se recomienda añadirlo a `.gitignore` y sirve para sustituciones de dependencias temporales y personales del desarrollador.

**Lógica de carga:**

1. Primero se carga la configuración pública `workspace.noml`
2. Después se carga la configuración privada `.workspace.noml`
3. **Misma clave**: la configuración privada sobrescribe a la pública
4. **Clave nueva**: se suman entre sí

```
workspace/
├── workspace.noml       ← configuración compartida del proyecto (bajo control de versiones)
├── .workspace.noml      ← configuración privada local (se recomienda añadir a .gitignore)
├── foo/
└── bar/
```

**Casos de uso:**

- Los alias y las reglas de reenvío de dependencias comunes del equipo se escriben en `workspace.noml` y se incluyen en el control de versiones
- Cuando un desarrollador necesita depurar localmente apuntando temporalmente una dependencia a un fork local, basta con sobrescribirla en `.workspace.noml`
- Evita que los mapeos temporales a forks contaminen la configuración pública del proyecto, compatibilizando la colaboración en equipos grandes con la flexibilidad del desarrollo local

**Ejemplo:**

```noml
; workspace.noml（公共，版本管理）
{
  foo: '/foo'
  bar: '/bar'
}

; .workspace.noml（私有，本地調試）
{
  ; 覆蓋公共配置：foo 指向本地 fork（工作區內）
  foo: '/my-fork-foo'
  ; 新增私有映射：指向工作區外部的 fork（OS 絕對路徑，唯一逃逸出口）
  'github.com/lizongying/nolang/core': '/home/lzy/code/fork/core'
  ; 新增私有映射（工作區內）
  experimental: '/experimental'
}
```

Los mapeos finalmente vigentes son: `foo` → `/my-fork-foo` (privado sobrescribe), `bar` → `/bar` (público conservado), `github.com/lizongying/nolang/core` → `/home/lzy/code/fork/core` (privado nuevo, escapa del workspace) y `experimental` → `/experimental` (privado nuevo).

### Restricción de frontera del workspace

Nolang aplica un control estricto de la frontera del workspace sobre las referencias a rutas, para garantizar la portabilidad y la seguridad del proyecto.

#### Resumen de reglas

| Nivel de configuración | `/` relativo al workspace | Ruta absoluta del SO | Saltos relativos `./` `../` |
| --- | --- | --- | --- |
| Clave de dependencia en `package.noml` | ✅ permitido | ❌ prohibido | ❌ prohibido |
| Valor de asignación en `workspace.noml` | ✅ permitido | ✅ permitido (salida de escape) | ✅ permitido (salida de escape) |
| Valor de asignación en `.workspace.noml` | ✅ permitido | ✅ permitido (salida de escape) | ✅ permitido (salida de escape) |
| Ruta de importación `#` en el código fuente | ✅ permitido | ❌ prohibido | ❌ prohibido |

#### 1. package.noml (estrictamente limitado, prohibido escapar del workspace)

`package.noml` es la configuración base estándar que el proyecto publica hacia fuera, usa CI y emplea la colaboración entre varias personas; se confirma en el repositorio de código.

```noml
#{fmt-skip}
dependencies: [
  ; ✅ 合法：以 / 開頭，相對於 workspace 根
  '/vendor/test',
  ; ❌ 禁止：作業系統絕對路徑 /home/xxx/...
  ; ❌ 禁止：./ ../ 相對跳轉
]
```
```

**Restricciones:**
- Las rutas locales solo se pueden escribir como `/xxx`, con base en la raíz del workspace
- No se aceptan rutas absolutas del sistema operativo
- No se admite `./` ni `../`
- No se puede referenciar directamente ningún directorio fuera del repositorio de trabajo

**Propósito:** si se permitieran rutas externas, sería muy frecuente que «en mi máquina compila, pero a los demás les falla», además del riesgo de seguridad de cargar por accidente código externo desconocido.

#### 2. workspace.noml + .workspace.noml (la única salida de escape)

Los destinos de asignación admiten tres tipos de ruta local:
- `/xxx` → directorio interior de la raíz del workspace (misma semántica que package.noml)
- `./xxx`, `../xxx` → se resuelven relativas a la raíz del workspace; permiten escapar de la frontera del workspace
- ruta absoluta del sistema operativo → permite apuntar a ubicaciones fuera del workspace (forks, componentes externos)

```noml
; .workspace.noml 私有配置，用於本地開發
{
  ; 工作區內部
  foo: '/vendor/foo'
  ; 相對跳轉：指向工作區外部的 fork
  'github.com/lizongying/nolang/core': '../fork/core'
  ; OS 絕對路徑：指向任意外部目錄
  'github.com/lizongying/nolang/utils': '/home/lzy/code/utils'
}
```

**Lógica clave:** `package.noml` no tiene capacidad de referenciar código externo directamente; si el usuario quiere montar código fuente fuera del repositorio, debe asignarlo explícitamente en la configuración del workspace. Esto equivale a añadir un interruptor claro y visible a la «carga entre directorios», de modo que todas las dependencias externas se gestionen en un único lugar.

#### 3. Restricciones complementarias en la capa de importación del código fuente

En el código fuente no se permite escribir directamente importaciones con rutas absolutas del sistema operativo; todas las referencias a código externo se han de introducir de forma indirecta, mediante alias del workspace o identificadores de paquetes remotos. Los saltos relativos `./` y `../` también están prohibidos.

```no
; ✅ 合法：工作區相對路徑
# /vendor/utils.helper
; ✅ 合法：遠端包標識
# github.com/lizongying/nolang/core.process
; ✅ 合法：標準庫
# std/fs
```

```no
#{fmt-skip}
; ❌ 禁止：相對跳轉
; # ./utils.helper
; # ../lib/helper.process

; ❌ 禁止：作業系統絕對路徑
; # /home/user/code/core.process
```

#### Demostración del flujo de datos

Escenario: la raíz del workspace del proyecto de negocio está en `/code/project` y se quiere usar `/code/fork/core`, externo.

❌ No se puede hacer esto (package.noml lo prohíbe):

```noml
; package.noml
dependencies: [
  '/code/fork/core', ; 非法：無法逃逸工作區,
]
```

✅ Forma estándar (romper la frontera mediante el mapeo del workspace):

```noml
; .workspace.noml
{
  'github.com/lizongying/nolang/core': '/code/fork/core'
}
```

```no
; 源碼正常導入
# github.com/lizongying/nolang/core
```

#### Flujo del workspace

El directorio de entrada para compilar y ejecutar siempre es el **directorio del workspace** (el que contiene `workspace.noml`). El flujo completo es el siguiente:

1. El usuario ejecuta `no build` o `no run <nombre de paquete>` en el directorio del workspace
2. El compilador lee `workspace.noml` y busca la ruta del subdirectorio correspondiente por nombre de paquete
3. Carga el `package.noml` de ese subdirectorio (es decir, la raíz del paquete) y compila/ejecuta tomando como base la raíz del paquete
4. Todas las rutas de importación (`use /path/to/module`) **se resuelven relativas a la raíz del workspace** (véase [Convención de resolución de rutas](path-resolution.md)), es decir, toman como única base el directorio que contiene `workspace.noml`, y ya no dependen del paquete en que se encuentra quien importa.

```
workspace/               ← directorio del workspace (aquí está workspace.noml)
├── workspace.noml      ← mapeo nombre de paquete → ruta
├── foo/                 ← paquete foo
│   ├── package.noml        ← raíz del paquete foo
│   ├── main.no
│   └── lib.no
└── bar/                 ← paquete bar
    ├── package.noml        ← raíz del paquete bar
    └── main.no
```

> **Nota**: las rutas de importación se basan en la **raíz del workspace** (el directorio que contiene `workspace.noml`), no en el directorio `package.noml` del paquete. Si en un subdirectorio del paquete existe un `package.noml` anidado, `LoadPackage` busca hacia arriba el `package.noml` más cercano y lo toma como raíz del paquete, pero la resolución de importaciones se basa uniformemente en la raíz del workspace.

---

## Casos de uso

- [notools](https://github.com/lizongying/notools) — Una colección de herramientas de línea de comandos comunes de Unix implementadas en Nolang, que incluye cat, ls, grep, wc, head, tail y más. Demuestra las capacidades reales de programación de sistemas de Nolang.
