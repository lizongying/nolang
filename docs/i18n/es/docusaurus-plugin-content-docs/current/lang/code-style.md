---
sidebar_position: 4
---

# Guía de estilo de código

## Salto de línea final (Trailing Newline / EOF)

Todo archivo fuente `.no` **no vacío** **debe terminar con exactamente un salto de línea final** (es decir, el archivo termina con un único salto de línea).

- El archivo **no tiene** salto final → añádalo.
- El archivo termina con **varios** líneas en blanco → colápselas en un único salto final.
- **Archivo vacío** (0 bytes) → déjelo sin cambios.

Directorios excluidos: `dist/`, `vscode-nolang/`, `node_modules/`.

Motivo: un único salto EOF coherente evita que `git diff` muestre "no newline at end of file", mantiene limpias las diferencias y hace predecible la concatenación y el uso de herramientas.

### Aplicación automática (integrada en la cadena de herramientas)

Esta norma se aplica **automáticamente** mediante la cadena de herramientas; no existe un script independiente:

- **`no fmt`**: el subcomando `fmt` de `src/cmd/no/main.go` formatea los archivos en el lugar y llama a `fmt.FormatFile`, que garantiza exactamente un salto final.
- **Formato al guardar del LSP / `textDocument/formatting`**: `formatNolangCode` en `src/lsp/server.go` → `fmt.FormatFile`, que añade o colapsa el salto EOF automáticamente al guardar o formatear un archivo `.no` en el editor.

La implementación está en `src/fmt/formatter.go`:

- `FormatFile(code)` formatea un archivo completo y llama a `ensureTrailingNewline`, que elimina todos los `\r` / `\n` finales (compatible con CRLF y con varios saltos finales) y añade un único `\n`. La entrada vacía o que no se puede analizar se devuelve tal cual, para evitar dañar un archivo que el formateador no entiende.
- `Format(code)` es la función pura de formateo de fragmentos (sin salto final), usada sobre todo por pruebas unitarias; al escribir un archivo fuente real, use `FormatFile`.

## Limpieza automática de importaciones (deduplicar / eliminar `# std/fmt` / ordenar)

`no fmt` (y el formato al guardar del LSP) normaliza automáticamente el **bloque contiguo de importaciones en la cabecera del archivo** (`# path[.fn] [alias]`); la implementación está en `src/fmt/imports.go`:

1. **Deduplicar**: las importaciones byte a byte idénticas (misma ruta, nombre de función y alias) se reducen a la primera aparición.
2. **Eliminación automática de `# std/fmt`**: `print` / `eprint` / `format` / `sprintf` son intrínsecos del compilador, y los módulos de la biblioteca estándar se pueden cargar implícitamente de todos modos (véase [Prefijos de llamada entre módulos](./module)), así que `# std/fmt` nunca es necesario y el formateador lo elimina. Dos excepciones lo conservan:
   - se escribió un **alias** o una **importación de función** (p. ej. `# std/fmt.sprintf fm`) — un enlace de nombre deliberado;
   - la línea de importación lleva un **comentario** o una anotación `#{...}` adjunta — el contenido escrito por el usuario nunca se descarta silenciosamente.
3. **Ordenación automática**: las importaciones se ordenan léxicamente por ruta → función → alias (ordenación estable).

Límite conservador: si alguna importación del bloque lleva un comentario (en su propia línea superior o al final de la misma línea) o una anotación `#{...}` adjunta (p. ej. la directiva de enlace nativo `#{bind=ffi}`), todo el bloque **conserva el orden de origen y no se elimina nada**: ordenar rompería la correspondencia entre comentario y sentencia, y eliminar haría desaparecer también el comentario. En ese caso, los duplicados exactos sin contenido adjunto siguen deduplicándose.

Los bloques de importación que aparecen fuera de la cabecera del archivo (después del código) se dejan intactos.

```no
#{fmt-skip}
; antes                             ; después
# std/vec                           # std/io
# std/fmt                           # std/vec
# std/io
# std/fmt        ;→  print/format son intrínsecos, se elimina # std/fmt; el resto se ordena léxicamente
```

## Marcadores de comentario

Nolang admite dos marcadores de **comentario de una sola línea** y uno de **comentario multilínea (de bloque)**, con la semántica siguiente:

- `//` — marcador tradicional de una sola línea (comenta hasta el final de la línea).
- `;` — marcador alternativo de una sola línea (implementado el 2026-07-17, comenta hasta el final de la línea).
- `;; ... ;;` — comentario multilínea (de bloque) (implementado el 2026-07-18, delimitadores simétricos; si no se cierra, el comentario llega hasta el final del archivo).

```no
#{fmt-skip}
// esto es un comentario
; esto también es un comentario, con el mismo significado
x = 1 ; comentario en línea, hasta el final de la línea

;;
   este es un comentario multilínea (de bloque)
   puede abarcar varias líneas
   termina con ;; ;;
y = 2 ;; comentario de bloque en línea ;;
```

**Delimitación del bloque**: `;;` abre y `;;` cierra; todo lo que hay entre ambos (incluidos los saltos de línea) es contenido del comentario. Un `;` suelto en el interior no cierra el bloque — solo lo hace `;;`. Si no se encuentra un `;;` de cierre, el comentario va desde el `;;` de apertura hasta el final del archivo. El delimitador de apertura `;;` solo entra en modo multilínea cuando **no hay nada más que espacios en blanco después en esa línea** — si hay texto detrás del `;;` en la misma línea, se trata de un comentario normal de una sola línea (p. ej. `;; texto de nota` comenta solo esa línea, y la siguiente sigue siendo código). El delimitador de cierre funciona igual: solo pueden seguir espacios en blanco después del `;;`.

**El formateador conserva el marcador original**: `no fmt` nunca cambia ningún marcador de comentario — `;` sigue siendo `;`, `//` sigue siendo `//`, y `;; ... ;;` sigue siendo `;; ... ;;`. El formateador recuerda el marcador original mediante `Comment.Marker` y lo emite literalmente; el contenido del comentario de bloque (incluidos los saltos internos) también se conserva tal cual, de modo que el formateo es idempotente.

**Ámbito seguro**: cuando `;` / `;;` aparecen dentro de literales de cadena (p. ej. `'text/plain; charset=utf-8'`, `index-from(';', pos)`) o dentro de comentarios `//`, los analizadores de cadenas y de comentarios del lexer los consumen primero, por lo que nunca se tratan como marcadores de comentario.

**Atención con `cond -> X; Y`**: como `;` es ahora un comentario, `cond -> X; Y` se analiza como «ejecutar `cond -> X` (evaluar X y descartarla)» más un comentario en línea `; Y` — **X nunca se asigna**. La forma correcta es `cond -> X = Y` (convención consolidada en la biblioteca estándar; véase `arr.no`, `uuid.no`, `path.no`, `err.no`, `assert.no`). Si encuentra `cond -> X; Y` en el código, cámbiela preferentemente por `cond -> X = Y`.

**Prohibido escribir varias sentencias en una sola línea**: `;` es solo un marcador de comentario y no sirve para unir varias sentencias en una línea; la coma `,` tampoco une sentencias. Cada sentencia dentro de un bloque `{ ... }` debe ocupar su propia línea. Comprimir un bloque en una línea es el error más peligroso:

```no
#{fmt-skip}
; ❌ Error: `; return }` se convierte todo en comentario y la `}` del bloque desaparece
cond -> { print('X'); return }
```

```no
; ✅ Correcto: cada sentencia ocupa su propia línea
cond -> {
    print('X')
    return
}
```

`cond -> { print('X'); return }` en realidad deja solo `cond -> { print('X')`: el bloque nunca se cierra y todas las sentencias posteriores a la `}` huérfana (normalmente el resto del cuerpo de la función, a veces incluso las funciones siguientes a nivel de módulo) quedan engullidas. El compilador puede no dar error, y cualquier diagnóstico puede apuntar muy lejos del fallo real.

## Literales booleanos y simplificación de `== true` / `== false` (implementado el 2026-09-24)

Además de las palabras clave `true` / `false`, Nolang acepta dos **formas abreviadas de literal booleano**:

- `!!` — literal independiente equivalente a `true`.
- `!` — literal independiente equivalente a `false` (solo cuando el token que sigue a `!` no puede iniciar un operando; si `!` va seguido de `salto de línea` / `;` / fin de archivo / `)` / `}` / `]` / `->`, se analiza como `false`).

> Nota: aquí hablamos de `!!` / `!` en **posición de expresión**, distinto de las formas en posición de bucle `!! { }` (ejecutar siempre) y `! { }` (no ejecutar).

Ambas abreviaturas se convierten en un `BooleanLiteral` ordinario **durante el análisis**, de modo que las cuatro formas `f == true`, `f == !!`, `f == false` y `f == !` son sintácticamente equivalentes y recorren la misma lógica.

Reglas de `no fmt` para los booleanos (implementadas en `src/fmt/expr.go`):

- **Los literales independientes se normalizan a la forma de palabra clave**: `c = !!` → `c = true`, `c = !` → `c = false`.
- **Lo conocido se simplifica; lo desconocido no**: cuando un lado de `==` / `!=` es un literal booleano y el otro operando **puede demostrarse que es un valor bool**, se emite la forma más simple. Las cuatro canalizaciones comparten las mismas reglas de **recopilación de firmas** (`collectModuleSignatures`: tablas std integradas, módulos locales alcanzados mediante `use` / `#` y EnumVariants) y el mismo contrato de inyección `SemRebuildConfig.Apply`; la única diferencia es el proveedor de tablas: `no fmt` / LSP usan `build.FmtSigLoader`, mientras que `no build` / `no vet` siguen usando `preloadModuleSignatures` + `astParseConfig` (ambos cargadores aún no se han unificado). `no fmt` inyecta antes del análisis fresco con `SkipLowering`; LSP inyecta en `ParseDocument` y publica un `parseSnapshot`, de modo que el formato al guardar reutiliza una copia profunda de ese AST azucarado (`fmt.CloneProgramForFormat`) y, con la configuración predeterminada, ya no vuelve a analizar. Hay tres fuentes que cuentan como prueba:

  1. Anotación de tipo explícita: `b bool = true`, parámetro `(b bool)`.
  2. Asignación de un literal booleano: `flag = true` / `flag = false`.
  3. Llamada a una función cuyo resultado declarado es `bool`: **definida en este archivo** (p. ej. `bytes-eq = ... (eq bool)`), o función std / de módulo local que aparece en la tabla de firmas (p. ej. `args.has-flag`, el local `helper.is-ok`). Una llamada sobre un receptor variable (`s.starts-with('a')`) se resuelve usando el tipo del receptor ya inferido en `Sem` (`s: str`) contra `str.starts-with`; si el receptor no se ha inferido, o el mismo nombre de variable se infiere como tipos distintos en ámbitos diferentes, sigue siendo desconocido (se evita deliberadamente adivinar «un nombre de método único globalmente», para no confundir `?bool` con un bool puro). Si la tabla de firmas no resuelve el tipo de resultado, sigue siendo desconocido.

  | Forma original (`b` abajo es una variable «conocida») | Tras formatear |
  | ----------------------------------------------------- | -------------- |
  | `b == true` / `b == !!`                               | `b`            |
  | `b == false` / `b == !`                               | `! b`          |
  | `b != true` / `b != !!`                               | `! b`          |
  | `b != false` / `b != !`                               | `b`            |
  | `true == b` (literal a la izquierda)                  | `b`            |

  `!=` solo invierte la polaridad; `true == false` (ambos lados literales) se deja intacto. Un nombre suelto sin evidencia bool, como `f == true`, también se deja intacto.
- **`?T` y todo operando desconocido conservan la comparación**: las opciones (`?T`), las llamadas entre módulos cuyo tipo de resultado no resuelve la tabla de firmas, los nombres sueltos sin asignación, los accesos con punto cuyo receptor no se ha inferido, las expresiones de llamada y los resultados de operaciones conservan todas `== true` / `== false`. Reducir uno de ellos a una guardia de control de flujo `x` / `! x` desencadena un fallo de codegen MIR «option-as-i1» — la v0.3.3 revirtió toda la regla precisamente por esto (`8a8ecb59`); el 2026-11-15 se restauró bajo una regla de **prueba positiva**, en lugar de la antigua inferencia «no se ve `?T`, así que debe ser un valor». Ese mismo día `no fmt` y LSP recibieron también las firmas entre módulos, de modo que los resultados option como `fs.write-str(...)` (`?bool`) son **visibles** y los detiene la compuerta dura de option, no el hecho de no ver el tipo. Una option es una **compuerta dura independiente** (`boolOperandIsOptionTyped`), que se comprueba antes de la prueba de valor bool, por lo que nunca se simplifica tanto si la anotación sobrevive en el código como si no: tanto `ok ?bool = fs.write-str(...)` como `ok = fs.write-str(...)` conservan la comparación. Eliminar anotaciones de tipo redundantes es un comportamiento **correcto y previsto** de `no fmt` (`fixRedundantTypeSource`); no desactive esa eliminación para preservar la detección de option. Pruebas de regresión: `src/fmt/bool_comparison_test.go`, `src/fmt/signatures_test.go`, `src/lsp/fmt_gate_test.go`.
- **También se aplica dentro de brazos `->` / condiciones if-then independientes** (para variables conocidas): `b == ! -> return` → `! b -> return`.
- **Seguridad de precedencia**: la negación se escribe como `! ` (con un espacio) para seguir la convención existente de operadores prefijos, lo que mantiene `no fmt` idempotente. Hoy todos los operandos demostrables son identificadores, así que `! ` no puede reenlazarse; la implementación conserva de todos modos la protección con paréntesis para operandos no atómicos (`! (a + b)` en lugar de `! a + b`), por si la regla de prueba se ampliara en el futuro.

## Inicialización a cero diferida de los valores de retorno

Las funciones de Nolang usan una estrategia de **inicialización a cero diferida** para sus parámetros de resultado con nombre (parámetros out): el prólogo de la función **no** inicializa a cero los parámetros out. En su lugar, el compilador lleva el seguimiento, mediante un bitmap `%__ret_init_bitmap`, de si cada parámetro out fue asignado explícitamente en algún punto del cuerpo. Cuando la función retorna, cualquier parámetro out **no asignado explícitamente** recibe automáticamente un store del valor cero de su tipo (entero → `0`, str-long → `zeroinitializer`, struct → `zeroinitializer`, option → `nil`). Este mecanismo es simétrico con el mecanismo existente de liberación diferida `%__move_bitmap`.

### Por qué este diseño

- **Rendimiento**: evita emitir un `store zeroinitializer` para cada parámetro out en el prólogo solo para que luego lo sobrescriba una asignación — un store puramente redundante. La estrategia diferida escribe el cero solo cuando «realmente no se asignó».
- **Legibilidad**: no hace falta escribir al inicio de la función código repetitivo de puesta a cero anticipada como `found = false` / `result = nil`; el lector ve directamente la intención de «solo la ruta de éxito asigna».
- **Coherencia**: usa el mismo patrón de seguimiento con bitmap que el mecanismo de liberación diferida `%__move_bitmap`, manteniendo simétrica la implementación interna del compilador.

### Estilo recomendado

No ponga a cero anticipadamente los parámetros out al inicio de la función — el compilador completa el cero al retornar. Escriba solo las asignaciones de la «ruta de éxito»:

```no
; Good: sin puesta a cero anticipada, el compilador se encarga
hashmap-str-tmpl.contains = (key str) (found bool) {
    val ?v = .get(key)
    val: {
        ok -> found = true

        err -> {
        }

        nil -> {
        }
    }
}
```

### Antipatrón (evitar)

No escriba puestas a cero anticipadas redundantes — son stores muertos que una asignación posterior sobrescribe:

```no
#{fmt-skip}
; Bad: puesta a cero anticipada redundante
hashmap-str-tmpl.contains = (key str) (found bool) {
    found = false              ; redundante: el compilador completa el cero al retornar
    val ?v = .get(key)
    val: {
        ok -> found = true
        err -> {}
        nil -> {}
    }
}
```

### Option tiene nil por defecto

Para parámetros out de tipo option `?T`, al completar el cero el compilador escribe directamente `nil` (tag=1, data=0), equivalente a `result = nil`. Por tanto, las rutas de «no encontrado / conjunto vacío / EOF» solo necesitan un `return` desnudo, sin escribir `result = nil` en el código:

```no
#{fmt-skip}
; Good: la ruta de fallo usa un return desnudo, el compilador completa result = nil
hashmap-str-tmpl.get = (key str) (result ?v) {
    .size == 0 -> return        ; el compilador completa result = nil
    ...
    ; flujo directo: el compilador completa result = nil
}
```

### Pista de depuración

Si una función devuelve un valor cero inesperado (p. ej. `found` debería ser `true` pero es `false`, o `result` debería tener un valor pero es `nil`), compruebe que **todas las rutas que deberían devolver un valor distinto de cero** asignan explícitamente el parámetro out dentro del cuerpo. El compilador no adivina la intención — solo completa con cero los parámetros out no asignados. Véase la pista de depuración en `.agents/skills/nolang-debug/SKILL.md`.

## Enlazar directamente la ranura de salida a una variable existente (evitar variables intermedias)

**Estilo recomendado**: al escribir código nuevo, si el resultado de una llamada solo sirve para seguir actualizando una variable ya existente, enlace la ranura de salida **directamente a esa variable**, sin crear una variable de tránsito que se use una sola vez. Esto es especialmente habitual con salidas múltiples y cuando el receptor es precisamente el estado que hay que actualizar.

```no
; ✅ Recomendado: el resto se escribe directamente en r (r es además el receptor del método)
qsq, r = r.div-mod(modulus)
```

```no
; ❌ Evitar: msq solo se escribe una vez y no tiene otro uso
qsq, msq = r.div-mod(modulus)
r = msq
```

- Use `_` como marcador de posición para las salidas que no se usan: `_, r = r.div-mod(modulus)` no introduce ni una variable nueva.
- La referencia de entrada de solo lectura y la ranura de salida que apuntan al mismo objeto son legítimas (regla de alias); `r = r._mul(r)` en `src/std/bigint.no` es el mismo tipo de actualización in situ, válida también con una sola salida.
- Es un estilo recomendado para **código nuevo**, no una orden de refactorización: el código válido existente no se reescribe por estilo (véase la regla de oro).
- La semántica completa, los resultados medidos y el ámbito de aplicación están en [Referencia del lenguaje: definición de funciones](./syntax).

## Bloques de código Markdown (`no fmt -md`)

`no fmt -md` da formato solo al **interior** de las vallas ```` ```no ```` / ```` ```nolang ````; las líneas de valla y el resto del Markdown se conservan byte a byte. El contenido del bloque se analiza como código fuente Nolang; si no se puede analizar, el bloque se deja intacto y se informa de su número de línea, de modo que el formateo nunca daña un archivo que no entiende. (`-md` y `-fix` son mutuamente excluyentes: `-md` solo da formato.)

### Los ejemplos correctos y los incorrectos van en bloques separados

Poner «la forma correcta» y «la forma incorrecta» en un mismo bloque hace que el analizador trate ambos como un único programa: la mitad correcta pierde su verificación con `no fmt`, y la mitad incorrecta puede ser «corregida» silenciosamente por el formateador hasta que el error deje de verse. Por tanto:

- un **ejemplo correcto** va en su propio bloque, sin anotación, para que `no fmt` lo formatee y valide;
- un **ejemplo incorrecto** va en su propio bloque, con `#{fmt-skip}` como primera línea, de modo que no se valide ni se modifique.

```no
; ✅ Correcto: cada sentencia ocupa su propia línea
cond -> {
    print('X')
    return
}
```

```no
#{fmt-skip}
; ❌ Error: todo lo que sigue a `;` es un comentario y la `}` del bloque desaparece
cond -> { print('X'); return }
```

### Tres usos legítimos de `#{fmt-skip}`

1. **Ejemplos incorrectos / contraejemplos**: no son programas válidos para empezar; formatearlos solo estorba.
2. **Pseudocódigo y resúmenes de firmas**: fragmentos solo de forma, sin implementación (p. ej. el esqueleto de firma `f = (a i64) (r i64) { ... }`).
3. **Hojas de referencia rápida con columnas de comentarios alineadas**: `no fmt` reescribe `code      ; nota` como `code; nota`, y la alineación de columnas es precisamente lo que hace legible este tipo de tabla.

### No añada anotaciones solo para impedir la expansión

`no fmt` expande un bloque vacío `{ }` y un brazo de una sola línea `-> { x() }` a varias líneas:

```no
ok = (b bool) (yes bool) {
}

dispatch = (cmd str) {
    cmd == 'a' -> {
        fa()
    }
}
```

Esta es la forma **canónica que debe aceptarse**: solo una vez expandido, el bloque tiene una línea propia donde insertar sentencias posteriores; de lo contrario, cada sentencia añadida obliga a dividir el bloque manualmente. Del mismo modo, que `true -> {}` se convierta en `true -> {\n}` no es motivo para revertirlo. Solo los tres casos anteriores (contraejemplos, pseudocódigo y tablas alineadas) necesitan realmente `#{fmt-skip}`.

### Los bloques solo de comentarios no necesitan anotación

Un bloque que contiene solo comentarios (la forma habitual de «comentar» la forma correcta) se analiza como **cero sentencias**. `no fmt` lo considera «nada que formatear», lo deja literalmente igual y no informa de nada — no es un error de sintaxis, y añadir `#{fmt-skip}` solo ocultaría ese hecho.

### Orden: primero anotar, luego formatear

`no fmt -md -w` reescribe en el lugar todos los bloques no filtrados. Una vez que el formateador ha «corregido» un ejemplo incorrecto, el contraejemplo ya no demuestra nada y la forma original se pierde sin remedio. **Añada `#{fmt-skip}` primero y ejecute `-w` después.**
