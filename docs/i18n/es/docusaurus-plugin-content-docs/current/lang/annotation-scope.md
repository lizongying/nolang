---
sidebar_position: 2.5
---

# Alcance de anotaciones y comentarios

Una anotación `#{...}` no contiene información sobre «cuánto cubre»: su alcance lo decide **el nodo al que se adhiere**.
Esta página lo fija: una posición, un conjunto de decisiones, cuatro niveles y una precisión adicional («una clave puede admitir solo algunos de esos niveles»:
`overflow` es la que solo acepta L3, véase §4.1), de modo que el autor, `no fmt`, `no vet`, codegen y el LSP den
la misma respuesta para una misma línea de anotación.

> Por qué hay que fijarlo: si `no vet` cree que un `#{overflow=wrap}` cubre toda la función
> mientras codegen cree que solo cubre la línea siguiente, aparece una deriva semántica silenciosa: «vet en verde, pero en tiempo de ejecución sigue devolviendo `err`».
> El alcance no es una propiedad de la anotación, sino **el entendimiento compartido de los lectores**, y un entendimiento no escrito deriva.

---

## 1. Un principio: el host decide el alcance

Una anotación tiene exactamente dos posiciones legales (las reglas de posición en sí están en [`syntax.md`](syntax.md)〈Posiciones de anotaciones〉):

```no
; ① arriba: en su propia línea, colocada sobre el objetivo
#{overflow=wrap}
x i8 = a + b

; ② final: escrita en la misma línea, detrás del objetivo
x i8 = a + b #{overflow=wrap}

; para objetivos de varias líneas (funciones, bloques) la forma final va tras la `}` de cierre
greet = () {
    print('hi')
} #{mac-arm64}
```

**Ambas son completamente equivalentes**: misma clave, mismo host, mismo alcance, y `no fmt` nunca reescribe una en
la otra (la final sigue siendo final, la de arriba sigue siendo de arriba). Cuál elegir es solo una cuestión de legibilidad: la forma final ahorra una línea en sentencias cortas;
la de arriba se usa cuando se anota toda una expresión o la sentencia es larga.

**Escribirla en la misma línea *antes* del objetivo es un error de compilación**; no existe la vía de «prefijo que se aplica a lo que sigue»:

```no
#{overflow = wrap} x i8 = a + b   ; ✗ prefijo → error
```

La decisión se toma en tres pasos, cada uno con una sola respuesta:

| Paso | Pregunta | Fuente de la respuesta |
| --- | --- | --- |
| 1 | ¿Quién es el host? | Posición: la primera sentencia no comentario superior, o la sentencia anterior en la misma línea |
| 2 | ¿A qué nivel pertenece el host? ¿Admite la clave ese nivel? | la tabla de §2 y la matriz de §4 (no admitirlo es error de compilación) |
| 3 | ¿Hasta dónde llega? | el subárbol del host, menos las fronteras duras internas (§3) |

---

## 2. Los cuatro niveles

| Nivel | Host | Alcance efectivo | Ejemplo de escritura |
| --- | --- | --- | --- |
| **L0 módulo** | Cabecera del archivo | Todas las declaraciones y sentencias de este archivo | En lo más alto del archivo, con una línea en blanco entre ella y lo que sigue |
| **L1 declaración** | Constantes/variables globales, estructuras, interfaces, enumeraciones etiquetadas, funciones, métodos | Toda la definición de esa declaración (en funciones: el cuerpo y todos los bloques anidados) | Justo encima de la declaración, o al final de la línea de declaración |
| **L2 bloque** | Grupo de corrutinas, grupo if, `cond -> body`, match, bucles `for`/contados/de rango, bloques desnudos | Todas las sentencias del cuerpo de ese bloque ( bloques anidados incluidos, hasta las fronteras L1 internas) | Justo encima de la sentencia de cabecera del bloque, o tras la `}` de cierre del bloque |
| **L3 línea única** | Una sola sentencia (`let`, asignación, `?=`, `return`, llamada, rama `cond ->`) | Esa sentencia y sus expresiones | Justo encima de la sentencia, o al final en la misma línea |

### L0 nivel de módulo

La posición es **el comienzo mismo del archivo** (nada delante, ni siquiera un import) y debe haber
**al menos una línea en blanco** entre el grupo de anotaciones y la primera sentencia: la línea en blanco actúa aquí como «terminador de alcance»:
dice que ese grupo no pertenece a la siguiente sentencia, sino a todo el archivo.

```no
#{win-amd64}

; ↑ línea en blanco → nivel de módulo: este archivo solo compila en Windows x86_64
; todas las declaraciones y sentencias de abajo están bajo su jurisdicción
helper = () {
    print('windows only')
}

label = 'win'
```

Sin la línea en blanco se obtiene un L1 normal (adherido a la primera declaración), y esta frontera
permite que el código existente no necesite migración:

```no
#{buildin}
bool.to-str = () (out str) {
}
```

El nivel de módulo solo sirve para claves que tienen sentido para un archivo entero (por ahora solo las claves de plataforma);
las claves de declaración (`buildin`, `generic`, `embed`…) en L0 son un error, no «un alcance más amplio».
`overflow` tampoco admite L0: solo acepta L3.

La configuración entre archivos no es una anotación; va en `package.noml`.

### L1 nivel de declaración

```no
; toda la estructura: el diseño de campos sigue la etiqueta de tipo
#{inline}
holder {
    inl point
    out point
}

; toda la función: el registro de entrada/salida lleva automáticamente el llamador
#{track-caller}
log.write = (msg str) {
    ...
}

; toda una declaración de constante: un directorio embebido (ruta relativa a package.noml)
#{embed='assets'}
ASSETS fs.embed

; compilar fuera toda un subárbol (cuerpos de función incluidos)
#{mac-arm64, linux-arm64}
greet = () {
    print('hello from unix')
}
```

L1 tiene además **granularidad de miembro**: las anotaciones sobre campos de estructura, variantes de enumeración etiquetada
y miembros de enumeración estilo C (`a pt #{inline}`, `ok(v i64) #{inline}`) gobiernan ese miembro, no todo el tipo.
Las anotaciones de miembro tienen igualmente solo dos posiciones: justo encima del miembro, o detrás del miembro
en la misma línea (la forma final es la usual).

### L2 nivel de bloque

Cuando el host es una «sentencia de cabecera de bloque», el alcance es todo el cuerpo del bloque:

```no
; todo el bloque se excluye (no se genera IR, ni se hace comprobación de tipos)
#{win-amd64}
2 * {
    print('windows only')
}

; un grupo if también se trata como una unidad
x = 1
#{win-amd64}
{
    x == 1 -> print('arm-1')
    -> print('arm-other')
}
```

Se aplican los cuatro tipos de bloque: grupos de corrutinas (`awy { ... }`), grupos if
(`{ cond -> … }`), sentencias de condición única (`cond -> body`), match y bucles `for` / contados / de rango.

Si el cuerpo de `cond -> body` es una sola sentencia, L2 y L3 dan el mismo resultado; la diferencia
solo aparece cuando el cuerpo es un bloque.

**No todas las claves llenan los cuatro niveles.** Una clave puede admitir solo algunos, y escribirla en un
nivel que no admite es un **error de compilación**, no una línea redundante inofensiva. La única clave restringida
hoy es `overflow`: solo acepta L3, por las razones de §4.1.

### L3 nivel de línea única

```no
a i8 = 100
b i8 = 100
; solo gobierna la siguiente sentencia (la forma final `c = a + b #{overflow=wrap}` es equivalente)
#{overflow=wrap}
c = a + b
c: {
    ok -> print('wrap:' - (it == -56).to-str())
    nil -> print('nil')
    err -> print('err')
}
```

Dentro de un bloque, una anotación aislada en su propia línea solo reconoce «la sentencia que va justo detrás». Una
anotación sin relación interpuesta (por ejemplo otra `#{...}` después de `#{overflow=wrap}`) no interrumpe la
cadena y se sigue encontrando la primera sentencia real; pero si la siguiente anotación lleva ella misma la misma clave, la anterior no gobierna ninguna sentencia (gana la posterior).

⚠️ **El match de bloque `{ … }: { … }` es «una sentencia» y no se atraviesa**: cuando la anotación se escribe sobre su `{`,
el host es esa sentencia de bloque en sí y **no** baja a la expresión `?=` dentro del bloque (medido: si se pone `#{overflow=wrap}`
fuera del bloque,

el `np ?= np + 1` de dentro sigue desbordando → `err` y el objetivo se pone a cero). Para aplicar `overflow` a una expresión dentro del bloque, escríbala justo encima de esa `?=` en el interior. La diferencia entre las dos formas de consumo véase en [syntax.md](syntax.md),
〈Compromiso entre match de bloque y match de valor-posición〉.

---

## 3. Herencia, sobrescritura y fronteras duras

**Se hereda de fuera hacia dentro y gana la más cercana.** Para las claves que admiten varios niveles (las claves de plataforma), L0 → L1 → L2 → L3 se aplican en orden;
si la misma clave aparece dos veces en el mismo nodo, la posterior sobrescribe la anterior (`#{mac-arm64} #{mac-arm64, win-amd64}`
se queda con la posterior).

**El cuerpo de una función es una frontera dura.** El alcance de un bloque L2 se detiene en el **subárbol sintáctico**:
no se propaga por relaciones de llamada ni entra en el cuerpo de definición de ninguna función / método / función anónima. El modo que se usa dentro de un cuerpo de función lo deciden solo su propia anotación L1 o las anotaciones L3 internas, y no tiene nada que ver con la anotación del bloque que la llama.

```no
; L1: toda la función junto con su cuerpo se compila fuera (no se genera IR en macOS)
#{win-amd64}
windows-only = () {
    print('never compiled on mac')
}

; sin anotación L1 propia → se compila en cualquier plataforma y no cambia por quién la llame
always = () {
    print('always compiled')
}
```

El motivo: si una anotación externa pudiera atravesar el cuerpo de una función, una palabra en un punto de llamada cambiaría silenciosamente la semántica de quien recibe la llamada, algo invisible al depurar. Una anotación L1 es lo contrario: está escrita en la cabecera misma de esa función, como parte de la declaración.

**Lista de fronteras duras** (la herencia se detiene aquí): cuerpos de función, cuerpos de método, cuerpos de función anónima/lambda, declaraciones externas `#{c}`.
**No fronteras** (la herencia continúa como siempre): bloques desnudos `{}`, grupos if, ramas de match, cuerpos de bucle.

---

## 4. Matriz clave × nivel

`✓` se aplica ／ `✗` ilegal (debe dar error de compilación) ／ `—` sin sentido en ese nivel (también debe dar error, no descartarse silenciosamente)

| Clave | L0 módulo | L1 declaración | L2 bloque | L3 línea única |
| --- | --- | --- | --- | --- |
| `overflow` | ✗ | ✗ | ✗ | ✓ |
| Claves de plataforma (`mac-arm64` etc.) | ✓ | ✓ | ✓ | ✓ |
| `buildin` | ✗ | ✓ función / enumeración etiquetada / estructura de diseño | ✗ | ✗ |
| `inline` | ✗ | ✓ estructura / campo / variante / función | ✗ | ✓ final de miembro |
| `generic` | ✗ | ✓ función / estructura | ✗ | ✗ |
| `track-caller` | ✗ | ✓ función | ✗ | ✗ |
| `embed` | ✗ | ✓ `let` de nivel superior | ✗ | ✗ |
| `bind` | ✗ | ✓ línea de importación | ✗ | ✗ |
| `c` / `cpp` / `rust` | ✗ | ✓ la declaración de función que sigue | ✗ | ✗ |
| `fmt-skip` | ✗ | ✗ | ✓ bloque vallado de markdown | ✗ |
| Metadatos (`derive` / `range` / `max` / `debug`…) | ✗ | ✓ declaración / miembro | ✗ | ✗ |
| `index-out` | — | — | — | — eliminada en D11; su presencia da error |

Tanto «ilegal» como «sin sentido» deben **informarse como error**, no fallar silenciosamente: un
`#{buildin}` escrito sobre un bucle `for` que se ignore sin más haría creer al autor que la declaración integrada surtió efecto.

### 4.0 Los tres soportes de `#{buildin}` y qué hace especial la «estructura de diseño»

`#{buildin}` se coloca sobre una declaración, pero su significado difiere por completo entre tres soportes:

| Soporte | Significado | Ejemplo |
| --- | --- | --- |
| Función | Stub de cuerpo vacío: el compilador / FFI aporta la implementación; en el lado Nolang no se genera cuerpo | `#{buildin}`<br/>`fs.remove = (p str) (ok bool) { }` |
| Enumeración etiquetada | Las variantes existen para coincidencia y comprobación de exhaustividad; la representación y construcción vienen del runtime integrado | `#{buildin}`<br/>`option { ok(v t), nil, err(e err.err), }` |
| **Estructura de diseño** | **Solo documentación**: este `struct` no existe semánticamente; su único producto son los tipos de campo (`t.len` es `u8`) | `#{buildin}`<br/>`txt { data [255]byte; len u8 }` |

El tercero merece una explicación aparte, porque apunta en dirección contraria a los dos primeros: el stub de función y la enumeración etiquetada son «el compilador sí usa esta declaración», mientras que la estructura de diseño es «el compilador **no** usa esta declaración: solo es la fuente de los tipos de campo y una nota legible sobre el diseño». El diseño real y los enlaces de documentación viven en otro sitio:

| Tipo | Fuente del diseño real | Fuente de documentación / enlaces |
| --- | --- | --- |
| `str` | Descriptor escrito a mano en MIR `%str-long = { i32 len, i32 cap, i8* data }` | `parser.BuiltinTypes()` |
| `vec` | Descriptor escrito a mano en MIR `%vec = { i32 len, i32 cap, i64 data }` | `parser.BuiltinTypes()` |
| `txt` | Descriptor escrito a mano en MIR `%txt = { [255 x i8] data, i8 len }` | `parser.BuiltinTypes()` |
| `arr` | Sin descriptor: `[n]t` es el agregado desnudo `[N x T]` y `.len` es la constante de compilación `n` | `parser.BuiltinTypes()` |

`parser.BuiltinTypes()` (`src/parser/builtintype.go`) es el único registro que mapea «nombre → módulo std → ancla de documentación del módulo → diseño»; el hover / ir a definición / finalización del LSP y `IsBuiltinLayoutStructName` lo leen. Por eso mismo la estructura de diseño **ya no lleva los enlaces**: elimínela y el hover y F12 siguen funcionando; solo el checker perdería el tipo de campo de `t.len`, razón por la que se conserva.

De aquí se deducen dos reglas duras; romper cualquiera produce de inmediato una «estructura sombra» que contradice el diseño real:

1. **Una struct con `#{buildin}` debe usar un nombre de diseño integrado** (`str` / `vec` / `txt` / `arr`).
   Pegarla en otra struct es un error: MIR tratará esa declaración como «diseño en manos del compilador» y la omitirá; todos los campos se ignoran y lo que queda para el autor es un montón de `unknown field`.
2. **Declarar una struct con un nombre de diseño integrado debe llevar `#{buildin}`.** Si un usuario escribe un `str { … }` desnudo, se tratará como un tipo normal de este módulo y se prefijará (`m.str`), de modo que codegen genera un `%m_str` que choca con `%str-long`.

Ambas las informa `checker.ValidateBuiltinStructDecls` (`1a8m445h` / `3lu6n4np`) y tanto `no build` como `no vet` las detectan. En sentido inverso, **solo** la combinación de `#{buildin}` + un nombre de diseño es omitida por la fase de prefijo de módulo y por `collectStructFields` de MIR (véase Fase 1 de `src/build/module_prefix.go` y `isBuiltinLayoutStruct` en `src/mir/hir2mir.go`): el criterio es la combinación «anotación + nombre», no el nombre por sí solo.

### 4.1 Por qué `overflow` solo recibe L3

`#{overflow}` es un conmutador semántico **por operación**, no metadatos de declaración. Si se permitiera escribirlo en una función o un bloque, «qué modo usa realmente tal operación» dependería de lo lejos que esté la anotación, de si hay otras anotaciones interpuestas y de cuántos bloques se apilen: leer una línea de código exigiría rastrear hacia arriba para responder, y mantener la misma respuesta entre `no fmt` / `no vet` / codegen sería muy difícil (ya se ha comprobado que derivó, véase §7).

Además **no cambia el tipo**: el resultado siempre es `?T`. Un `wrap` de nivel bloque o función no convierte `?T` en `T`, solo sustituye silenciosamente «desbordar ⇒ `err`» por «retroceso silencioso»: el error pasa de «fallo explícito» a «valor erróneo que parece un éxito», justo lo que menos debería decidir una anotación lejana.

Por eso se fija en una anotación por línea:

- Solo se reconoce la sentencia inmediatamente posterior; la forma final (`x = a + b #{overflow=wrap}`) es equivalente.
- Para que surta efecto en todo un bloque hay que escribirla una por una: tanto `no fmt --fix=overflow` como el quickfix del LSP producen esa forma.
- Escrita en la cabecera de una función, de un bloque o en la cabecera del archivo → **error de compilación**, no «inactiva pero inofensiva».

Ventaja añadida: la anotación siempre está en la misma pantalla que la sentencia que gobierna, así que al mover código no se separan; y `wrap` no acaba cubriendo por accidente una operación vecina tras un reajuste de indentación.

---

## 5. Alcance de los comentarios (`;` / `//` / `;;`)

Los comentarios no participan en la semántica de compilación; su atribución solo afecta al maquetado de `no fmt` y al enganche de la documentación de API. La sintaxis de marcado está en [`code-style.md`](code-style.md)〈Marcado de comentarios〉.

| Comentario | Atribución |
| --- | --- |
| En línea (`x = 1 ; explicación`) | Esa línea |
| Líneas de comentario consecutivas sobre la línea | La siguiente sentencia/declaración (L1 o L3, con la misma determinación de host que los comentarios) |
| Línea en blanco entre el comentario y el objetivo | No se atribuye a ningún nodo; sirve como separador de bloque/archivo (salvo en la cabecera de archivo, véase L0) |
| Comentario de bloque `;; … ;;` | Solo hasta su propio delimitador de cierre; no cruza ámbitos |
| Comentario entre la anotación y el objetivo | Sigue al objetivo; el orden de salida es fijo: «comentario de documento → anotación → sentencia» |

`;` y `//` tienen el mismo significado; `no fmt` conserva el marcado original sin reescribirlo. El ancla de traducción de un comentario de documento
(`[get](#args.get)`) debe ser la **última línea** del grupo de comentarios, pertenecer al mismo host y no cruzar nodos.

---

## 6. Los cuatro lectores deben coincidir

Una misma anotación debe dar el mismo alcance a los cuatro lectores:

| Lector | Lo que debe saber |
| --- | --- |
| parser / codegen | Qué modo usa este nodo y su subárbol |
| checker / `no vet` | El ámbito de la determinación «ya anotado» |
| `no fmt` | La anotación se vuelve a imprimir en su sitio, sin moverse entre hosts |
| LSP | La posición donde inserta hover/quickfix es igual a donde surte efecto realmente |

De aquí se deducen cuatro invariantes:

1. **Determinación de anotado coherente**: el ámbito que codegen considera «ya anotado» necesariamente es también el que `no vet` considera procesado, y viceversa.
2. **fmt no mueve**: `no fmt` solo puede ajustar la posición dentro del mismo host (arriba ↔ final); moverla entre hosts equivale a cambiar la semántica.
3. **Si no surge efecto, se informa**: una anotación que cae en un nivel sin semántica correspondiente (véanse `✗` / `—` en §4) da error, no se ignora silenciosamente.
4. **Nivel restringido**: una clave solo surge efecto en los niveles permitidos por la matriz. Cuando `overflow` se escribe en L0/L1/L2, los cuatro lectores deben
   tratarlo como un **error**: no puede haber una bifurcación de «vet lo acepta, codegen no» (situación actual en §7).

---

## 7. Situación actual y diferencias (medido el 2026-11-11)

Se verificó punto por punto con `bin/no`, con una forma uniforme: «sumar dos `i8 = 100` y ver si el resultado es `err` o `-56`
(wrap)»; las claves de plataforma se verificaron con `#{win-amd64}` en macOS para comprobar si se excluían.

| Nivel | `overflow` (diseño: solo L3) | Claves de plataforma | Claves de declaración |
| --- | --- | --- | --- |
| L0 módulo | ✗ prohibido por diseño; actualmente se adhiere a la primera sentencia (equivale a L1) | No implementado | No implementado |
| L1 declaración | ✗ prohibido por diseño; **la situación actual es un agujero**: `no vet` lo acepta (salta toda la función), codegen no (sigue `err`) | ✓ excluye todo el subárbol | ✓ |
| L2 bloque | ✗ prohibido por diseño; codegen no lo acepta. La cabecera de `for` / grupo if / rama de match puede hacer que `no vet` calle en todo el bloque; **un bloque desnudo `{` en posición de sentencia ni siquiera logra callar** (véase abajo) | ✗ el bloque sigue participando en la compilación | — |
| L3 línea única | ✓ | ✓ | — |

Evidencia medida:

```text
a-none.no    (sin anotación)                     → err
b-line.no    (#{overflow=wrap} sobre la sentencia)   → result:true  (-56, surte efecto)
c-fn.no      (#{overflow=wrap} sobre la función)     → err           (vet: No issues found)
d-block.no   (#{overflow=wrap} sobre for)            → err ×2        (vet: 2 hints)

p1-block.no  (#{win-amd64} + 2 * {…})      → el cuerpo sigue ejecutándose    (no filtrado)
p2-stmt.no   (#{win-amd64} + print único)   → esa línea se excluye    (surte efecto)
p4-fn.no     (#{win-amd64} + declaración de función)     → la función se excluye    (surte efecto)
p6-ifgroup.no(#{win-amd64} + grupo if)        → la rama sigue ejecutándose      (no filtrado)

; comparación en tiempo de compilación: con y sin anotación en la cabecera de `for`, 15w45dqk es idéntico byte a byte
k2.no (cabecera de for #{overflow=wrap} + `x i8 = a + a` en el cuerpo) → cannot assign ?i8 to i8
k3.no (se borra esa línea, todo lo demás idéntico byte a byte)                       → cannot assign ?i8 to i8
```

Por qué un bloque desnudo ni siquiera logra silenciar a `no vet`: los objetivos a los que
`parseAnnotationStatement` puede adjuntar una anotación son solo IDENT / USE / IN / RARROW /
métodos de tipo arreglo, y `{` no está entre ellos. Así, en la forma siguiente la anotación no se
adjunta ni al bloque ni a una sentencia dentro de él — degenera en un `AnnotationStatement`
independiente, una línea muerta para el parser, codegen y `no vet` por igual:

```no
#{overflow=wrap}
{
    ...
}
```

Las cabeceras de `for` / grupos if / brazos de match empiezan con un identificador o con `in`,
por lo que casualmente caen dentro de los objetivos adjuntables.

Tres agujeros que hay que cerrar (la regla «solo L3» para `overflow` aún no se aplica):

1. **`no vet` se salta toda la función en cuanto ve un `overflow` a nivel de función**
   (el atajo `fn.OverflowMode == ""` en `walkStmtForOverflow`) → las comprobaciones salen en verde
   mientras en tiempo de ejecución sigue devolviendo `err`.
2. **`no vet` hereda hacia abajo por los bloques** (`enclosingOverflow`) → una anotación en la
   cabecera de un bloque silencia todo el bloque, aunque codegen no la acepte en absoluto.
3. **parser / codegen ignoran silenciosamente el `overflow` en L1/L2** en lugar de informar del
   error → el autor no tiene forma de saber que no surtió efecto.

Inventario y tratamiento del código existente (`src/std`, tras tres rondas de empujar hacia abajo
con `no fmt --fix=overflow`):

| Categoría | Antes de la migración | Tras la ronda 1 | Tras la ronda 2 | Tras la ronda 3 |
| --- | --- | --- | --- | --- |
| Líneas de anotación `overflow` en total | 4580 | 4083 | 3861 | 4080 |
| de las cuales, líneas muertas que «no se adjuntan a ningún nodo» | 330 (por forma) | 87 | **0** | **0** |
| informadas por `no vet` pero no adjuntables | — | ~270 | 270 | **51** |

Ronda 1 (2026-11-11), cambio neto: 816 líneas de anotación de nivel de ámbito eliminadas,
325 anotaciones por sentencia insertadas. Visto como A/B, unas 218 de las 325 son sitios a los que
ya faltaba una anotación y no tienen nada que ver con el empujado hacia abajo (ejecutar solo la
inserción, sin retirar la cobertura, también las habría añadido), y unas 97 son intenciones de
nivel de bloque materializadas por sentencia.

Ronda 2 (2026-11-12) fue una **limpieza pura**, motivada por que `findFixTargetNode` no descendía
a los cuerpos de los brazos y no había un paso de «verificar tras insertar»: 381 líneas eliminadas,
159 añadidas, **todas ellas líneas de anotación `#{...}`, 0 líneas de código** (verificado línea a
línea con `diff`). 159 de las adiciones son reubicaciones «de una cabecera de bloque / de brazo a
sobre una sentencia», y las otras 222 son líneas muertas y anotaciones redundantes de nivel de
ámbito eliminadas directamente — esas posiciones no surtían efecto de todos modos, así que
eliminarlas no cambia ni la compilación ni la comprobación.

La ronda 2 también corrigió tres defectos de la herramienta:

1. **`checker` informaba números de línea que colapsaban en el contenedor**: el `emit` de
   `walkExprForIntOverflow` mezclaba el `stmt.Pos().Line` del contenedor con la columna del
   operador, de modo que cada informe dentro de `{ ... }` / un grupo if / un brazo de match llevaba
   la línea de la cabecera. La posición errónea del diagnóstico era solo el síntoma; el problema
   real era que el `findFixTargetNode` de `--fix` tomaba «la sentencia más interna que contiene esa
   línea» y solo podía recuperar el contenedor, por lo que la anotación se insertaba de nuevo en la
   cabecera del bloque. Ahora toma **la línea propia del operador** (`x.Token.Line`), con retroceso
   a la línea del contenedor cuando falta la línea del token.
2. **Sin descenso a los cuerpos de los brazos**: `findFixTargetNode` se detenía deliberadamente
   por encima de toda la sentencia match. Ahora desciende a **cuerpos de brazo en forma de bloque**
   (el `{ ... }` de `cond -> {`, incluido el `BlockMatchStatement.Body` de la forma
   `{ ... }: { ... }`); los cuerpos de brazo en línea siguen sin descenderse (la sentencia de
   `w i64 -> w + 1` está en la misma línea que el patrón del brazo, así que insertar por encima de
   ella caería dentro del match, antes de la línea del patrón del brazo).
3. **Insertar también en posiciones donde no se puede adjuntar**: `#{overflow}` ahora **vuelve a
   analizar el código para verificar** después de insertar, y retira las anotaciones que «se
   insertaron pero degeneraron en un `AnnotationStatement` independiente» (no quedan más líneas
   muertas). Tras una retirada, el código que ve una segunda pasada de `--fix` no cambia, así que
   se mantiene la idempotencia.

Ronda 3 (2026-11-12) tocó el parser: `parseAnnotationStatement` ganó **DOT** como objetivo
adjuntable. Las sentencias que empiezan por el receptor, como `.n = .n + 1` / `.cnt == 0 -> …`,
son de la misma clase que `x = …` (sentencia única, L3), pero no estaban entre los objetivos
adjuntables ⇒ la anotación degeneraba en un `AnnotationStatement` independiente (línea muerta), el
sitio se informaba eternamente y `--fix` se retiraba por la verificación incluso cuando sí
insertaba. Tras la flexibilización, 219 sitios recibieron una anotación L3 (líneas de anotación
3861 → 4080) y el número de no adjuntables bajó de 270 a 51. La forma del AST cambió en
consecuencia (una sentencia AnnotationStatement menos, un registro de tabla lateral y
`OverflowMode` más en la sentencia objetivo), `parser.ASTVersion` se incrementó a
`2026-11-12.1`, y después hay que ejecutar `make gen no`.

**`{` y `(` se dejaron fuera deliberadamente**: `{` (bloque desnudo en posición de sentencia /
match entre paréntesis) y `(` (la cabecera de bucle `(cond) { }`) son hosts de bloque L2, y
`overflow` está diseñado para aceptar solo L3. Permitirlos haría que `overflowAnnotatedNode`
retornara anticipadamente para todo el bloque, lo que equivale a legalizar la semántica L2 — en
conflicto con el orden «primero cerrar, luego flexibilizar» de §8. La mayoría de las 51 restantes
son de estos dos tipos y pertenecen al paso 4b de §8.

Criterios de aceptación (árboles aislados A/B, misma fuente y mismo compilador, difiriendo solo en
`src/std`, con `no vet` por archivo): los 73 archivos afectados tuvieron **0 errores** (sin rotura
de compilación), 25 archivos tuvieron menos diagnósticos y 19 más — y cada aumento es un
`ovfhndld` «vinculado a `?T` pero sin valor de desbordamiento declarado» expuesto al retirar la
cobertura, que es el resultado esperado del paso 1 de §8, no un defecto nuevo. Otras 9 advertencias
`nolang-overflow-ineffective` son el comportamiento preexistente de la herramienta de «rellenar el
hueco» cayendo sobre operadores sobrecargados de bigint / cadena, sin relación con el empujado
hacia abajo. Tras la ronda 2, una repetición completa de `bin/no run test/std/*.no` (zstd / tls /
str / json / set / base32 …) dio **todo PASSED**.

Criterios de repetición de la ronda 3: las 3 pruebas `parser` en rojo
(`TestAnnotationsLiveOnAST` / `TestCachedASTLowersToIdenticalHIR` /
`TestBuildSemViewsMatchesFreshParse`) fueron **idénticas byte a byte antes y después de la
flexibilización** — `only 288 files parsed cleanly` en ambas ejecuciones, lo que muestra que la
flexibilización no introdujo ningún fallo de análisis nuevo; son el estado en curso de la
refactorización concurrente de parser/anotaciones. La salida de `no fmt -d src/std` fue **idéntica
byte a byte** antes y después de la flexibilización, es decir, el formateador imprime las
anotaciones sobre `.` en su sitio original, sin deriva de formato. El heap / zstd / tls / json /
set / map / regexp de `test/std` estaban todos en verde.

**76 anotaciones de cabecera de brazo restantes** (`#{overflow=wrap}` justo encima de
`cond -> {`): **no son** líneas muertas — el parser copia las anotaciones de nivel de brazo a cada
sentencia del cuerpo del brazo, así que surten efecto para el cuerpo (medido: una anotación en la
cabecera del brazo silencia `r = i * 2` en el cuerpo, dejando solo la condición de la cabecera
informada). No tienen efecto sobre la **condición del brazo** en sí (una condición no es una
sentencia). Estas 76 se dejan como están: la inserción por sentencia no puede alcanzarlas (los
sitios del cuerpo ya están silenciados y ya no se informan), y retirarlas para volver a añadirlas
solo agita la línea de la cabecera.

**51 sitios aún informados por `no vet` que el lenguaje actual no puede anotar** (270 en la ronda
2, solo estos quedan tras la ronda 3 con DOT flexibilizado): la sentencia objetivo empieza por `{`
(bloque desnudo en posición de sentencia / match entre paréntesis) o `(` (la cabecera de bucle
`(cond) { }`), además de unas pocas condiciones de cabecera de brazo. Hoy **también** se informan
(la anotación original nunca surtió efecto), así que retirarlas no constituye una regresión;
llevarlos a cero exige primero los pasos 1–2 de §8 (convertir `overflow` en L2 en un error) y luego
decidir si L2 debe soportarse realmente — véase el paso 4b de §8.

Muestreo de posiciones como `archive/bzip2.no:315`, donde la cabecera del bloque y su interior
llevan **simultáneamente** `#{overflow=wrap}`: la capa de la cabecera del bloque no surte efecto
actualmente y la que realmente funciona es la capa por sentencia dentro del bloque
(`--fix=overflow` ya la ha rellenado). Así, eliminar la capa de nivel de bloque no cambia la
semántica; pero, tras eliminarla, una nueva ejecución de `no vet` expone las posiciones que «solo
estaban silenciadas por la anotación externa» — ese es precisamente el objetivo del
endurecimiento, no un efecto secundario. Medido: las sugerencias `ovfhndld` en `crypto/rsa.no`
pasaron de 19 a 22, todas en sentencias «vinculadas a `?T` sin valor de desbordamiento
declarado», sin cambio semántico.

Impacto de L0 en el código existente: 21 archivos bajo `src/**/*.no` tienen una anotación como
primera línea efectiva — 17 son `#{js}` y el resto son `#{buildin}` etc. inmediatamente seguidos
de una declaración — y **ninguno** de ellos tiene una línea en blanco, así que según el criterio
L0 de §2 siguen siendo L1, y ni `no fmt` ni la compilación cambian. `str.no` tiene 2 casos de
«anotación + línea en blanco + brazo cond» dentro del cuerpo de una función, que no activan el
criterio L0.

---

## 8. Orden de endurecimiento

1. **Primero impedir que `no vet` acepte `overflow` en L1/L2**: eliminar el atajo de «saltar toda
   la función» y restringir `enclosingOverflow` al subárbol de la misma sentencia (sin propagación
   hacia abajo entre bloques).
2. **Luego hacer que el parser informe del error**: `#{overflow}` en L0/L1/L2 → error de
   compilación, con un mensaje que apunte a «anote por sentencia, o ejecute
   `no fmt --fix=overflow`». Este paso expone de una vez todo el corpus existente.
3. **Limpiar el corpus existente**: `no fmt --fix=overflow` ya tiene la capacidad de empujar hacia
   abajo (retirar la capa de nivel de ámbito y luego rellenar L3 por sentencia siguiendo los
   informes del checker); `src/std` ha pasado por tres rondas (véase la tabla de inventario en
   §7). Tras el cambio, `make gen no` es obligatorio (regenerar el blob y StdSigKey), y no debe
   ejecutarse `no fmt -w` sobre `src/std` — la migración solo toca líneas de anotación, se realiza
   por archivo con `no fmt -fix=overflow -w <f>`, y `diff` garantiza que no hay otros cambios.
4. **Flexibilizar los objetivos adjuntables del parser** (la ronda 3 completó la mitad):
   - **4a hecha**: `parseAnnotationStatement` acepta sentencias que empiezan por **DOT**.
     `.n = .n + 1` / `.cnt == 0 -> …` son sentencias únicas (L3) al igual que `x = …`, pero
     históricamente no podían llevar una anotación ⇒ 219 `#{overflow=wrap}` en `src/std` nunca
     surtieron efecto. Tras la flexibilización, el número de no adjuntables bajó de 270 a 51 y
     `parser.ASTVersion` se incrementó a `2026-11-12.1`.
   - **4b pendiente de decisión**: `{` (bloque desnudo en posición de sentencia / match entre
     paréntesis) y `(` (la cabecera de bucle `(cond) { }`) son **hosts de bloque L2**.
     Flexibilizarlos equivale a legalizar la semántica L2 para `overflow`
     (`overflowAnnotatedNode` retornaría anticipadamente para todo el bloque), lo que va en contra
     de la dirección de endurecimiento de los pasos 1–2. El orden correcto es hacer primero los
     pasos 1–2 (convertir `overflow` en L2 en error) y luego decidir si las 51 restantes se
     resuelven «reescribiéndolas como sentencias sin bloque» o «rellenando el cuerpo del brazo por
     sentencia», en lugar de abrir `{` / `(` sin condiciones.
5. **Endurecer las demás claves gradualmente**: convertir las claves de declaración
   (`buildin` / `generic` / `embed`…) escritas en L2/L3 de «ineficaces» a «error».
6. Cada paso debe mantener `no fmt` idempotente y mantener a los cuatro lectores de acuerdo sobre
   la misma entrada (§6).
