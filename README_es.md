# Nolang

[繁體中文](README.md) · [简体中文](README_zh-CN.md) · [한국어](README_ko.md) · [日本語](README_ja.md) · [English](README_en.md) · [Русский](README_ru.md) · [Français](README_fr.md) · [Español](README_es.md) · [Deutsch](README_de.md) · [Português](README_pt.md)

Nolang es un lenguaje de programación de sistemas seguro en memoria, de alto rendimiento y con una sintaxis minimalista.

## Características principales

- **Seguridad de memoria**: gestión automática de la memoria
- **Alto rendimiento**: optimizaciones en tiempo de compilación, sin GC
- **Sintaxis minimalista**: una sintaxis más simple
- **Guiado por la intención**: respeta plenamente la intención del desarrollador, con una semántica acorde a la intuición humana
- **Nunca se bloquea**: elimina las rutas de fallo implícitas

## Instalación

Instalación con un solo comando (macOS / Linux / Windows Git Bash): descarga automáticamente la última versión de `no` en `$HOME/.local/bin`:

```bash
curl -fsSL https://raw.githubusercontent.com/lizongying/nolang/main/install.sh | sh
```

Por defecto se instala la versión **con LLVM integrado** (`no-llvm_*`, sin necesidad de instalar LLVM / opt / llc por separado). Para la versión pura en Go, más pequeña, use `NO_FLAVOR=plain` junto con `no build -cc zig`:

```bash
curl -fsSL https://raw.githubusercontent.com/lizongying/nolang/main/install.sh | NO_FLAVOR=plain sh
```

Para fijar una versión o un directorio personalizado (las variables deben llegar a `sh`, así que guarde primero el script):

```bash
curl -fsSL https://raw.githubusercontent.com/lizongying/nolang/main/install.sh -o install.sh
NO_VERSION=0.4.28 NO_INSTALL_DIR="$HOME/bin" sh install.sh
```

| Variable          | Valor por defecto   | Descripción                                                              |
| ----------------- | ------------------- | ------------------------------------------------------------------------ |
| `NO_VERSION`      | `latest`            | Versión, p. ej. `0.4.28` o `v0.4.28`                                    |
| `NO_FLAVOR`       | `embedded`          | `embedded`: LLVM integrado; `plain`: Go puro (requiere LLVM del sistema) |
| `NO_INSTALL_DIR`  | `$HOME/.local/bin`  | Directorio de instalación                                                |
| `NO_REPO`         | `lizongying/nolang` | Repositorio de origen                                                    |

Verifique con `no version`. Si `$HOME/.local/bin` no está en `PATH`, añádalo:

```bash
export PATH="$HOME/.local/bin:$PATH"
```

> Compilar programas `.no` también requiere un enlazador C: se recomienda `no build -cc zig` (zig incluye clang / lld / libc) o instalar clang por su cuenta.

## Uso

doc

[docs](https://lizongying.github.io/nolang/)

Extensión de VSCode

[vscode](https://marketplace.visualstudio.com/items?itemName=lizongying.vscode-nolang)

CLI de Nolang

[nolang](https://github.com/lizongying/nolang/releases/latest)

nohub — índice de paquetes (paquetes de la comunidad)

[nohub](https://lizongying.github.io/nohub/)

### Búsqueda de paquetes de la comunidad (nohub)

`no` lee directamente el índice de paquetes de nohub y ofrece tres comandos de solo lectura:

```bash
no search <query>    # buscar por nombre / palabra clave / categoría
no info <pkg>        # mostrar metadatos y comandos de instalación/importación
no docs <pkg>        # imprimir la URL de documentación (docs → homepage → repository)
```

Por defecto se usa el índice público. Para combinar varias instancias de nohub, configura una lista de fuentes en `~/no/sources.noml` (el orden del array define la prioridad):

```
{
  sources: [
    'https://lizongying.github.io/nohub',
  ]
}
```

`NOHUB_SOURCES` (separado por comas) la sobrescribe temporalmente. Los tres comandos aceptan `-source <name>`; `no search` muestra una columna `SOURCE`, y un paquete con el mismo nombre y versión se resuelve con la primera fuente. Una fuente que falle solo genera una advertencia. `no` solo descarga el `index.noml` de cada fuente (`<base>/index.noml` — el mismo archivo que la página de búsqueda carga primero, una entrada por paquete en su versión más reciente); ni tarball ni `data.js`, con un mapeo de campos idéntico a `web/app.js`. Los mismos datos se pueden explorar en la [página de búsqueda de nohub](https://lizongying.github.io/nohub/).


### Comandos de la CLI

| Comando                                                      | Descripción                                                                                                                |
| ------------------------------------------------------------ | -------------------------------------------------------------------------------------------------------------------------- |
| `no init`                                                    | Define el espacio de trabajo (genera workspace.jsonc, sin package.jsonc)                                                    |
| `no new <name>`                                              | Crea un nuevo paquete en el espacio de trabajo (subdirectorio + package.jsonc, y lo registra en workspace.jsonc)              |
| `no fmt [-w] [-d] <file\|dir>`                               | Formatea el código fuente                                                                                                  |
| `no build [-o <file>] [-cc <s>] [-target <s>] [<file\|dir>]` | Compila (genera un ejecutable)                                                                                              |
| `no run [-cc <s>] [-target <s>] [<package\|dir\|file>]`        | Compila y ejecuta (nombre de paquete/directorio/archivo)                                                                   |
| `no test [-cc <s>] [-target <s>] [<file\|dir>]`              | Ejecuta las pruebas                                                                                                        |
| `no add <pkg>`                                               | Añade una dependencia                                                                                                      |
| `no remove <pkg>`                                            | Elimina una dependencia                                                                                                    |
| `no update <pkg>`                                            | Actualiza una dependencia                                                                                                  |
| `no update-all`                                              | Actualiza todas las dependencias                                                                                           |
| `no list`                                                    | Lista las dependencias                                                                                                     |
| `no search <query>`                                            | Buscar paquetes de la comunidad por nombre / palabra clave / categoría |
| `no info <pkg>`                                                | Mostrar metadatos y comandos de instalación/importación |
| `no docs <pkg>`                                                | Imprimir la URL de documentación del paquete |
| `no sync`                                                    | Sincroniza las dependencias                                                                                                |
| `no install [-u] [<tool>[:<version>]@<host>/<owner>/<repo>]` | Instala un binario (~/no/bin/ + enlace simbólico)                                                                           |
| `no uninstall <name>`                                        | Elimina el binario y el enlace simbólico                                                                                    |
| `no pub --token <token> [--registry <url>]`                  | Publica en el registro                                                                                                     |

```bash
# compilación (por defecto desde main.no)
no build                    # compila el directorio actual
no build main.no            # compila el archivo especificado
no build -o output          # especifica la ruta de salida
no build -cc zig            # usa el compilador Zig
no build -target x86_64-linux-gnu  # compilación cruzada (especifica la plataforma de destino)

# ejecución (compilación + lanzamiento)
no run                    # compila y ejecuta main.no del directorio actual
no run foo                # ejecuta el paquete llamado foo en el espacio de trabajo
no run ./foo              # ejecuta main.no del directorio ./foo
no run -cc zig
no run -target aarch64-macos-gnu

# pruebas (compilación + ejecución)
no test                   # prueba todos los archivos .no del directorio `test`
no test test/my-test.no        # ejecuta un único archivo de prueba
no test -cc zig
no test -target x86_64-windows-gnu
```

### Crear un nuevo proyecto

```shell
no new test1

cd test1

no run
```

### Hello World

Tras crear el proyecto, edita `main.no`:

```no
print('Hello, World!')
```

Ejecución:

```bash
no run
```

Salida:

```
Hello, World!
```

### Reglas de entrada

- **main.no** — punto de entrada del programa
- **lib.no** — punto de entrada de la biblioteca, exporta funciones
- **todos los archivos .no del directorio tests/** — contienen aserciones de prueba

### Notas sobre las pruebas

- Los archivos de prueba se colocan todos en el directorio tests/
- Cada archivo de prueba se compila de forma independiente
- Si alguna prueba falla, se devuelve un código de salida distinto de cero

```bash
no vet src/std                     # comprueba la biblioteca estándar con Nolang vet
```

## Compilación desde el código fuente

Nolang debe compilarse con `make`:

```bash
make            # compila todos los objetivos (bin/no y vscode-nolang/server/lsp)
make no         # compila bin/no
make lsp        # compila el servidor LSP
make package    # compila el LSP y empaqueta la extensión de VSCode
make no-wasm    # transpila no a WebAssembly
make lsp-wasm   # transpila el LSP a WebAssembly
make playground # compila no.wasm + lsp.wasm + el sitio Docusaurus
make clean      # limpia los artefactos de compilación
make help       # muestra todos los objetivos de compilación
```

> ⚠️ **La compilación de Nolang debe usar `make`; nunca compiles directamente con `cd src && go build`.**
>
> `make` ejecuta automáticamente `make gen` antes de compilar, para regenerar `src/checker/stdsig_gen.go`
> — una tabla de firmas de la biblioteca estándar **integrada en el binario** (`funcSigs` / `methodSigs` /
> `funcParams` / `methodParams` / `structFields` / `aliases` / `structMod` /
> `enumVariants` / `embeddedStdModules`). Gracias a ella, cada `no build` omite la pasada PASS1
> (« análisis completo de todo std ») y el arranque es notablemente más rápido.
>
> Si has modificado `src/std` y ejecutas `go build` **sin** haber ejecutado antes `make gen`, el compilador
> detectará una incoherencia entre la tabla de firmas integrada y el contenido de `src/std`, y
> **rechazará la compilación con un error** (indicándote que ejecutes
> `make no`). Esto es deliberado: antes, una degradación silenciosa al análisis completo ralentizaba
> cada compilación y dificultaba detectarlo. Tras modificar `src/std`, ejecuta siempre `make no`
> (que primero ejecuta `make gen` y luego compila).
>
> Ejecuta las pruebas también con `make test`: **un `go test` directo fallará porque no se inyecta la clave de firma (`StdSigKey` está vacía)**. `make test` inyecta la misma clave al compilar el binario de prueba, de modo que siga la ruta rápida de la tabla de firmas integrada.

---

## Base de términos (glossary)

`glossary/` es una **base de conocimientos de Nolang organizada por entradas**: cada término tiene un registro, con su tipo, definición, sintaxis,
ejemplos, referencias relacionadas y un resumen de una frase. En total **244 entradas / 10 dominios semánticos** (tipos, sintaxis, anotaciones, memoria,
errores, módulos, internos del compilador, cadena de herramientas, todos los módulos std, trampas conocidas), y los datos están en formato `.noml`.

La interfaz está escrita en Nolang y ofrece tanto una CLI como MCP (stdio / HTTP), consultables tanto por personas como por IA:

```bash
no build glossary                        # producto: glossary/dist/glossary

./glossary/dist/glossary get option      # obtiene el texto completo de una entrada
./glossary/dist/glossary search 三態      # búsqueda por palabra clave
./glossary/dist/glossary list --domain=traps   # lista de trampas conocidas
./glossary/dist/glossary check           # comprobación de coherencia (para CI)

./glossary/dist/glossary mcp             # servidor MCP (stdio)
./glossary/dist/glossary mcp --http=127.0.0.1:8765
```

La salida se puede cambiar con `--format=text|md|json`. Consulta [`glossary/README.md`](glossary/README.md) para los detalles de integración y el formato de datos.

También puedes ver el paquete glossary en nohub: [glossary on nohub](https://lizongying.github.io/nohub/?package=glossary:latest@github.com/lizongying/nolang)

---

## Proyectos relacionados

- [nohub — búsqueda del índice de paquetes comunitarios del ecosistema Nolang](https://lizongying.github.io/nohub/)

  Busque paquetes comunitarios por nombre, palabra clave o categoría; consulte información del paquete, instantáneas de versión y entradas al código fuente. El índice se genera con `web/build.mjs` a partir de las instantáneas de `packages/`.

- [noui — un conjunto de herramientas de interfaz que reescribe el espíritu de GPUI en Nolang (paquete nohub)](https://lizongying.github.io/nohub/?package=noui:latest@github.com/lizongying/nolang)

- [Benchmarks Game — informe de comparación de rendimiento Nolang vs C vs Rust](https://github.com/lizongying/no-benchmarks/results/report.md)

- [notools — conjunto de utilidades de línea de comandos Unix implementadas con Nolang](https://github.com/lizongying/notools)

  Caso de uso de Nolang: un conjunto de utilidades de línea de comandos Unix habituales implementadas en el lenguaje Nolang, que cubre herramientas clásicas como cat, ls, grep, wc, head, tail, etc., y muestra la capacidad real de Nolang en el ámbito de la programación de sistemas.

---

## TODO

## Licencia

Apache-2.0
