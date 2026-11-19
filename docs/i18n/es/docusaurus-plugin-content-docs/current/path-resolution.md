---
sidebar_position: 99
---

# Convención de resolución de rutas: relativa a la raíz del espacio de trabajo (Workspace-Root-Relative)

## Convención (adoptada)

Todas las **rutas de embed** y las **rutas de importación** (directivas `use` / `#`, recursos embebidos) se resuelven como
**rutas absolutas canónicas con base en la raíz del espacio de trabajo (workspace root)**; **ya no se resuelven relativas al «directorio del archivo de código fuente actual»**.

- La «raíz del espacio de trabajo» es el directorio que contiene `workspace.noml`, es decir, `pkg.WorkspaceRoot()`.
  Nota: el directorio que contiene `package.noml` es la **raíz del paquete** (`pkg.RootDir`); no están en el mismo nivel: la raíz del espacio de trabajo está arriba (contiene `workspace.noml` y puede contener varios paquetes), mientras que la raíz del paquete es el directorio de un **paquete** concreto (contiene `package.noml`), que a su vez contiene varios **módulos** (archivos fuente `.no`). No los confunda.
- Tras la resolución se aplica la canonicalización (`filepath.Clean`, eliminar `.`/`..`, unificar separadores), obteniendo una **ruta canónica única a nivel global** que sirve como identificador único para la carga de módulos, las claves de caché y la pertenencia AST/semántica.
- Esta ruta canónica es el **componente de ruta** de las claves de caché de `lexer.tokenCache` y `checker.parseProgramFileCache`. En realidad la clave de caché es una clave compuesta `(ruta canónica, hash del contenido)` (`cache.Key`, véase `src/cache/lru.go`): misma ruta pero contenido cambiado → hash distinto → invalidación automática; ya no se devuelven tokens/AST obsoletos. Además, la caché es una LRU con límite de capacidad, por lo que los procesos de larga duración (LSP / fmt) no presentan fugas de memoria (elemento ③ de la revisión original de la arquitectura de dos pasadas).

## Por qué

1. **Eliminar desde la raíz las colisiones de nombres en la caché**. La antigua clave de caché de tokens/AST era la «cadena de ruta»; cuando distintos archivos fuente importaban/embebían con la misma cadena relativa (p. ej. `utils/no` o el nombre de embed `data.json`), accedían a la misma entrada de caché → contaminación cruzada, tokens/AST erróneos. Al pasar a rutas absolutas canónicas relativas a la raíz del espacio de trabajo, **cada archivo lógico tiene una clave única; el mismo archivo siempre da la misma clave**, y las colisiones desaparecen por construcción (elemento ④ de la revisión original de la arquitectura de dos pasadas).
2. **Simplificación y robustez enormes de la lógica del compilador**. La resolución de rutas pasa de estar «dispersa en parser / transpiler / checker resolviendo respecto al directorio del archivo actual» a un **único paso de canonicalización**; las referencias adelantadas, la carga de módulos, las claves de caché y las rutas en los diagnósticos comparten todas una misma base y ya no dependen de la ubicación del archivo que importa.
3. **Preparar el terreno para caché entre archivos y compilaciones paralelas**. Las rutas absolutas canónicas hacen que las claves de caché sean estables y comparables globalmente, lo que facilita la reutilización mediante deep-clone en el futuro (evitando `ParseProgram` duplicados) y optimizaciones de análisis perezoso.

## Límites de aplicación

- **Rutas absolutas** (módulos locales que empiezan con `/`): ya están resueltas respecto a la raíz del espacio de trabajo; se mantienen.
- **Módulos `std/`**: provienen del `StdFS` integrado, con clave `std/<rel>.no`; se mantienen y no se confunden con rutas locales.
- **Recursos embebidos**: con independencia del nombre relativo con que se haga referencia a ellos, internamente se normalizan a una clave de embed canónica relativa a la raíz del espacio de trabajo.

## Implementado (2026-08-02)

Las funciones auxiliares unificadas están en `src/package/paths.go` (paquete `pkg`, solo biblioteca estándar, sin dependencias circulares):

- `FindWorkspaceRoot(start)`: busca hacia arriba desde `start` el directorio que contiene `workspace.noml`.
- `FindPackageRoot(start)`: busca hacia arriba desde `start` el directorio que contiene `package.noml` (recurso cuando no hay workspace).
- `ResolveToWorkspaceRoot(wsRoot, rel)`: normaliza cualquier ruta original de importación/embed a una ruta absoluta relativa a la raíz del espacio de trabajo (quita la `/` inicial; las rutas absolutas se devuelven tal cual; si `wsRoot` está vacío, recurre al directorio actual).
- `ResolveEmbedBase(sourcePath)`: base de resolución para rutas de embed relativas: se prefiere la raíz del espacio de trabajo, con recurso a la raíz del paquete / al directorio del archivo fuente.

Puntos de entrada convergidos:

- `src/build/transpiler.go` `resolveUse`: la rama A (prefijo `/`) y la rama E (alias) se unificaron en `pkg.ResolveToWorkspaceRoot(t.workspaceRoot(), path)`, y se añadió el método `t.workspaceRoot()` (prefiere `pkg.WorkspaceRoot()`; si no, busca `workspace.noml` hacia arriba desde `sourcePath`). Se eliminó la lógica redundante anterior `if t.pkg != nil { baseDir = pkg.RootDir; if wsRoot... }`; el recurso relativo al cwd ahora usa `FindWorkspaceRoot`.
- `src/build/transpiler.go` `processEmbeds`: las rutas de embed relativas ahora usan `pkg.ResolveEmbedBase(sourcePath)` (se prefiere la raíz del espacio de trabajo).
- `src/checker/checker.go` `ValidateEmbedAnnotations`: las rutas de validación de embed usan igualmente `pkg.ResolveEmbedBase`.

Las claves de caché (`tokenCache` / `parseProgramFileCache`) obtienen la ruta absoluta canónica relativa a la raíz del espacio de trabajo como componente de ruta mediante `resolveFile(filePath)`; al combinarla con el hash del contenido del archivo fuente se forma una clave compuesta (`cache.Key`), por lo que las colisiones desaparecen por construcción, las ediciones en la misma ruta se invalidan automáticamente y la LRU con límite evita fugas de memoria.

Los módulos integrados en `std/` y `js/` siguen usando `StdFS`/`JsFS` (con clave `std/<rel>.no` / `js/<rel>.no`), sin cambios; `processEmbeds` solo actúa sobre el programa principal (código fuente del usuario): los embeds propios de los módulos std se siguen resolviendo respecto a su propia ubicación.

> Estado: **implementado (2026-08-02)**. El código nuevo debe usar directamente `pkg.ResolveToWorkspaceRoot` / `pkg.ResolveEmbedBase`; no utilice el «directorio del archivo actual» (`filepath.Dir(t.sourcePath)` etc.) para resoluciones relativas.

## Glosario (unificado, 2026-08-02)

- **workspace (espacio de trabajo)** — normalmente corresponde a un repositorio. El directorio raíz contiene `workspace.noml` y puede incluir varios paquetes. La raíz del espacio de trabajo es la única base para resolver todas las rutas de embed/importación.
- **package (paquete)** — una unidad de compilación grande: una biblioteca o un ejecutable. El directorio raíz contiene `package.noml` (la **raíz del paquete**). Declara dependencias, backend de emisión, etc. Un paquete contiene varios módulos.
- **module (módulo)** — normalmente un archivo fuente (`.no`). La convención actual es **un archivo = un módulo**. Cargar un módulo = cargar un archivo `.no`.

La jerarquía entre los tres es: **workspace ⊃ package (uno o más) ⊃ module (uno o más)**. No use «module» para referirse a un «paquete» (unidad de compilación): esa es la función de «package».
