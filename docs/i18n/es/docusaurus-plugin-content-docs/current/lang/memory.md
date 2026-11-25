---
sidebar_position: 6
---

# Gestión de memoria

Nolang es un lenguaje **sin GC**: la seguridad de memoria se garantiza mediante llamadas a `free` insertadas por el compilador. Este documento describe el diseño de memoria implementado y la semántica de propiedad.

## Principios fundamentales

### Propiedad única

Cada búfer `data` en el montón tiene **un único propietario**. La propiedad se puede transferir mediante move; tras la transferencia, el propietario original deja de ser responsable de liberarlo. En las asignaciones `=` entre variables locales, un clon profundo hace que ambas variables posean sus datos de forma independiente.

### Semántica de asignación

`b = a` selecciona una de las siguientes semánticas según el contexto:

| Semántica | Condición de activación | Comportamiento |
|------|---------|------|
| **Copia de valor** | Tipos primitivos (i64/f64/bool, etc.) y no se cumple can_slot_rebind | Copia directa del valor, sin datos en el montón |
| **Religado de ranura de pila** | `b = a` entre locales; a es un tipo de pila (i64/u64/i128/u128/txt) y se cumple can_slot_rebind | `g.varAlias[b] = a`; b y a comparten la misma ranura de pila, 0 copias (mejor que la copia de valor); si no, degrada a copia de valor |
| **Clon profundo** | `b = a` entre locales; a es un tipo con propiedad en el montón (vec/arr/str/estructura clonable/**`?str`, `?[]T`**) y **sigue referenciada** tras la asignación | malloc de nuevos datos + memcpy + clon recursivo de elementos; a y b poseen sus datos por separado y cada uno se libera al salir de la función |
| **move (fuente muerta)** | `b = a` entre locales; a tiene propiedad en el montón pero **ya no se referencia** tras la asignación | Transferencia de propiedad sin copias; la fuente omite free. **Observacionalmente equivalente** a un clon profundo, menos una copia |
| **move** | Parámetro de salida `out = x` | Copia superficial de la estructura + marca la fuente como moved; la fuente omite free |
| **Clon profundo** | `vec.push(x)` (x es un tipo con propiedad en el montón) | malloc de nuevos datos + memcpy + clon recursivo de elementos; la fuente sigue poseyendo datos independientes y cada uno se libera al salir de la función |

> **La asignación de valor a valor siempre hace clon profundo; se prohíbe el alias sin copias.** `b = a` (el RHS es una variable con nombre completa) solo tiene dos reducciones:
> **fuente viva ⇒ clon profundo**, **fuente muerta ⇒ move**; **no se permite** «dos nombres que comparten un búfer con un solo free»
> (esa reducción existió en la optimización forward de solo lectura y se eliminó). Las **lecturas de elemento／campo** (`b = a[i]`, `b = a.f`) mantienen
> la semántica de **alias de vista** y no están sujetas a esta restricción.

## Move de tipos de pila (religado de ranura de pila / slot-rebind)

Aunque `i64/u64/i128/u128/txt` son todos **tipos de pila** (los valores viven directamente en la ranura de pila de alloca, sin datos en el montón), `b = a` sigue adoptando por defecto la **copia de valor** (memcpy del valor de pila de a a la ranura de pila de b). Para evitar copias innecesarias, el compilador realiza un **religado de ranura de pila** en los `b = a` de tipos de pila que satisfacen la restricción **can_slot_rebind**: establece `g.varAlias[b] = a`, de modo que b y a comparten la misma ranura de pila (0 copias, mejor que la copia de valor).

### Restricción can_slot_rebind (corrección conservadora)

Tras el religado, b y a apuntan a la misma ranura de pila, por lo que cualquier lectura posterior de b observará «el almacenamiento de a». Por ello, el religado solo se permite cuando la fuente a **no tiene ninguna referencia posterior (lectura o escritura)**; de lo contrario **degrada a una copia de valor completa** (la semántica no cambia, solo hay un memcpy adicional).

- La seguridad se resuelve estáticamente mediante `computeSlotRebindSafety` (función principal) / `computeMoveEligibility` (funciones de usuario) a través de `stmtContainsVarRefAny` / `exprContainsVarRefAny`: se exploran todas las sentencias posteriores al move (incluidas las ramas y los retornos de bucle); si la fuente se referencia de cualquier forma, `slotRebindSafe[stmt] = false` → se toma la vía de la copia.
- La exploración de referencias debe enumerar **todos los nodos AST que pueden referenciar una variable**; una referencia de escritura no cubierta provocaría un religado inseguro (ranura de alias corrompida → salida errónea o incluso un bucle infinito). Cubiertos: a nivel de sentencia `LetStatement` / `ExpressionStatement` / `ForStatement` (incl. `Init`/`Update`/`Condition`/`CountExpr`/`Body` y la **colección de iteración `IterRange`**) / `ReturnStatement` / `MultiAssignStatement` / **`UnwrapAssignStatement` (asignación de desempaquetado `?=`)** / **`BlockStatement` (bloque desnudo)**; a nivel de expresión `Identifier` / `AssignExpression` / `Infix` / `Prefix` / `Call` / `Dot` / `Index` / `IfExpression` (cuerpos de rama) / `Slice` / `Conditional` / `Grouped` / **`CoExpression` (generación de corrutina `co <call>`, de forma defensiva; la desactivación de toda la función la sigue gestionando `curHasUnsafeConstruct`)** / **`CastExpression`** / **`RangeExpression`**. Todo constructo nuevo que referencie variables debe añadirse a ambas funciones de exploración.
- `match` se desugara a `IfExpression` y las referencias dentro de los cuerpos de sus ramas quedan cubiertas por el análisis anterior, por lo que **match no activa la desactivación** (verificado frente a la línea base en `tests/match.no` / `tests/option.no`).

### Escenarios de desactivación (siempre degradan a copia de valor)

| Condición de desactivación | Motivo |
|---------|------|
| El destino es un parámetro de salida / variable global / variable de tipo montón | Los parámetros de salida se pasan por puntero, las globales son visibles entre funciones y los tipos de montón requieren free profundo; el religado rompe la propiedad |
| La fuente es un parámetro / variable global | Los parámetros se pasan por referencia; el religado corrompería el marco de pila de quien llama |
| **Función de stdlib** (`curIsStdLib`, determinada por `SetStdModules`) | Los alias internos de stdlib (p. ej. el `it` de fmt alias de la variable local `n`) son difíciles de modelar por completo en el análisis de referencias |
| **El cuerpo de la función de usuario contiene una clausura (`FunctionLiteral`) o una generación de corrutina (`RunExpression`)** (`curHasUnsafeConstruct`, explorado recursivamente por `bodyHasUnsafeConstruct`) | Una clausura evalúa las variables capturadas en un *contexto de función separado*; una corrutina se ejecuta en otro hilo. En ambos casos la vida de las variables excede el ámbito de alias `g.varAlias` de una sola función de la función actual, de modo que el religado corrompería la ranura de pila compartida |

> Compromiso de diseño: las clausuras/corrutinas usan una **desactivación de toda la función** en lugar de un cálculo por variable, porque el análisis de alias de una sola función no puede modelar por naturaleza el uso compartido de almacenamiento entre funciones/hilos. Desactivar toda la función solo pierde la optimización (degrada a copia) y nunca introduce un error, en línea con el principio de «corrección conservadora».

### Invalidación de alias

- Cuando se reasigna el destino, primero se hace `delete(g.varAlias, name)` para limpiar cualquier alias residual; si no, la vía genérica de asignación `varAddr(name)` seguiría apuntando a la ranura antigua de la fuente y contaminaría a la fuente antigua.
- El religado realiza una resolución transitiva a lo largo de la cadena `g.varAlias` para encontrar la ranura final de la fuente (`a → c → …`), evitando desalineamientos de alias de varios saltos; además se sincroniza `g.varTypes[name]` para que las consultas de tipo posteriores sigan siendo coherentes.

### Ortogonalidad con el move de tipos de montón

El religado de ranura de pila solo actúa sobre la semántica de **copia de valor** de los `b = a` de tipos de pila; es completamente ortogonal a las vías de «clon profundo / move (parámetro de salida)» de los tipos con propiedad en el montón, que no se ven afectadas por `slotRebindSafe`.

### Referencias de pruebas

- `tests/slot-rebind.no`: religado de i64/u64, i128/u128 comparados con `==`, txt con `.len-bytes()`, degradación a copia (`da` reutilizado → copias `db`/`dc`), reasignación posterior (`ra`=10), move de parámetro (`pm_fn` con fuente parámetro → copia), religado y reasignación (`rr_fn` → `rr`=99), consumo (`consume(m)` → `cm`=84). Salida esperada `42 7 1 1 17 5 5 10 100 99 84`.
- `tests/slot-rebind-unsafe.no`: una corrutina (`awy { co ... }`) captura una variable de pila; verifica que `curHasUnsafeConstruct` desactiva el religado y que la salida coincide con la línea base.

### Free insertado por el compilador

- Al salir de la función: libera todas las variables locales de montón que no se han movido
- Antes de reasignar: libera el valor antiguo
- Campos de estructura: libera recursivamente los campos que contienen datos en el montón

## Diseño de tipos

| Tipo Nolang | Estructura en memoria | Campos | Estrategia de asignación |
|------------|-----------|------|---------|
| `[]T` (segmento) | **16 bytes** | **u32 len, u32 cap, i64 data (ptrtoint)** | malloc (montón) |
| `[N]T` (array fijo) | N × sizeof(T) | elementos en línea, **sin descriptor len/cap** | alloca (pila) o malloc |
| `str` (cadena larga) | **16 bytes** | **u32 len, u32 cap, i8\* data** | malloc (montón) |
| `txt` (cadena fija) | 256 bytes | `[255 x i8]` data, i8 len | alloca (pila) |
| `?T` (anulable) | **tipado según su carga**: `{ i8 tag, [K x i64] }` | etiqueta de 1 byte + 7 bytes de relleno + carga (múltiplo de 8) | alloca (pila); si la carga supera el umbral se guarda un puntero al montón |
| enumeración etiquetada | `{ iN tag, [K x i64] }` | el ancho de la etiqueta sigue el número de variantes (`i8` en adelante); la carga es la unión de la variante más ancha | alloca (pila) |
| estructura | suma de los campos | cada campo | alloca (pila) |

**El tamaño de `?T` se calcula, no es fijo**: `?i64` = 16 bytes, `?str` = 24, `?txt` = 264; una carga que supera
`option-payload-inline-threshold` (predeterminado 264 = 1 + 7 + 256, mínimo 16) se encapsula en el montón y la option
completa vuelve a 16 bytes. Cada tipo de carga tiene su propio tipo `%option_<key>` (`src/mir/layout.go`), de modo que
«mismo tipo ⇔ mismo diseño». La carga de `err` siempre se encapsula y no compite por el presupuesto de carga. Véase
`docs/docs/lang/syntax.md`〈Tipos anulables (option) · Diseño de memoria〉.

El ancho de la etiqueta de una **enumeración etiquetada** sigue el número de variantes (`<=256` → `i8`, `<=65536` → `i16`, `<2^32` →
`i32`, en caso contrario `i64`); la unión de carga tiene el ancho de la **variante más ancha**, no la suma de todas, y sus campos se disponen con alineación natural y
se direccionan por **desplazamiento de bytes**. Cuando todas las variantes carecen de carga, el diseño degenera a `{ iN }` (a partir de 1 byte).

**La cabecera u32 de `str`**: `%str-long = type { i32, i32, i8* }` (16 bytes en plataformas de 64 bits).
`len` / `cap` son `u32` internamente (LLVM `i32`), pero **todas las API públicas siguen siendo `i64`**: las lecturas
hacen `zext i32 -> i64` y las escrituras `trunc i64 -> i32`. Las firmas de `s.len-bytes()`, `s.len()`,
`s.cap()`, `s._len()` / `s._cap()` / `s._set-len()` / `s._set-cap()` y `len(s)` no han cambiado. Esta capa frontera se gestiona de forma centralizada en
`strLenI64` / `strCapI64` / `strHeaderLoadI64` / `strHeaderStoreI64` / `strLongFromParts` del codegen:
**no se debe** emitir un `load i64` / `store i64` directo sobre los campos 0 / 1 del descriptor; con punteros opacos
el verificador de LLVM no lo detecta, pero se leen／escriben 8 bytes y se modifica también el campo de cabecera adyacente (se midió que
`s.len-bytes()` llegó a devolver `len | cap<<32`).

Las constantes de tamaño asociadas también deben seguir los 16 bytes: el paso de elemento (stride) de `[]str`, el
almacenamiento de respaldo de argv de `os.args()` y los literales de constantes de cadena globales `{ i32 len, i32 cap, i8* data }`. La carga de `?str` son exactamente 16
bytes (dos palabras i64, la primera empaqueta len y cap), por lo que `?str` es una option en línea de 24 bytes y
la carga de la variante `str` en una enumeración etiquetada ocupa igualmente dos palabras. `txt` se mantiene igual y no pasa a u32 como `str`.

**La cabecera u32 de `[]T`**: `%vec = type { i32, i32, i64 }` (16 bytes). `len` / `cap` son
isomórficos a los de `str` (u32 por dentro, `i64` en todas las API públicas); la diferencia es que **el campo data sigue siendo `i64`**
(un puntero codificado con `ptrtoint`, no un `i8*`), para mantener sin cambios los puntos de llamada existentes de `inttoptr` /
`ptrtoint`. Esta capa frontera se gestiona de forma centralizada en
`vecLenI64` / `vecCapI64` / `vecHeaderLoadI64` / `vecHeaderStoreI64` / `vecFromParts` (y los genéricos `hdrFieldI64` / `hdrTrunc` / `hdrWiden`);
`emitLenCap` / `emitGetField` / `emitSetField` / `ensureVecBuffer` pasan todos por ellos. Se aplica la misma prohibición que para `str`: **no se debe**
emitir un `load i64` / `store i64` directo sobre los campos 0 / 1 de `%vec`.

`cap == 0` en `%vec` es un marcador de **vista prestada** (`s.bytes()`, préstamos `[N]T` → `[]T`, coercionstr → `[]byte`):
no posee montón y `@vec_free` / `@vec_retain` lo omiten; solo `cap > 0` posee el bloque apuntado por `data`.

`[N]T` no tiene ningún descriptor en tiempo de ejecución: en la capa LLVM es simplemente un agregado desnudo `[N x T]`; `.len` se sustituye directamente por la
constante de compilación `N` y no hay `cap`.

## Free superficial y free profundo

### Free superficial

Solo libera el búfer data del contenedor, sin recorrer los elementos. Se aplica a:
- `%str-long` (los datos de la cadena son un búfer de caracteres, sin elementos anidados con propiedad en el montón)
- vec/arr cuyos elementos son tipos primitivos (i64, double, etc.)

### Free profundo

Primero recorre cada elemento y libera recursivamente sus datos en el montón, y después libera el búfer data del contenedor. Se aplica a vec/arr cuyos elementos tienen propiedad en el montón:
- `[]str` (elementos %str-long)
- `[][]i64` (elementos %vec)
- `[]MyType` (elementos de estructuras de usuario; libera campos recursivamente)

### Comprobación de NULL

Antes de cada free se comprueba `icmp eq i8* %ptr, null`, para evitar free(NULL) o liberar un puntero no inicializado.

## Transferencia de propiedad (move)

### Move con un único valor de retorno

```no
get-slice = () (out []i64) {
    local = [1, 2, 3]
    out = local; local se marca como moved, no se libera al salir de la función; out lo gestiona quien llama
}

v = get-slice(); v posee data y se libera al salir de la función
```

### Move con varios valores de retorno (por orden de posición de los parámetros)

```no
get-pair = () (a []i64, b []i64) {
    x = [1, 2]
    y = [3, 4]
    a = x; primer parámetro de salida, x se marca como moved
    b = y; segundo parámetro de salida, y se marca como moved
}

a, b = get-pair(); a posee los datos de x, b posee los datos de y
```

**Orden de tratamiento**: se procesa uno por uno según el **orden de declaración** de los parámetros de salida en la firma de la función. Cada asignación `out = src` marca de forma independiente la variable fuente como moved.

**Múltiples asignaciones de la misma variable fuente**: si `a` y `b` referencian la misma variable fuente (p. ej. `a = x; b = x`), el compilador decide automáticamente entre clon y move mediante un **preanálisis de Liveness** (`moveEligible`):

- `a = x`: x se referencia después en `b = x` → **clon profundo**, a posee sus datos por separado
- `b = x`: x no se referencia más adelante → **move**, b toma los datos de x, x se marca como moved y omite free

Esto evita un double-free. Regla: **la última referencia usa move; las anteriores usan clon**.

Los escenarios con ramas condicionales (p. ej. `if cond { out = x }`) siguen el estado moved mediante un mapa de bits en tiempo de ejecución; véase la sección siguiente.

### Clon profundo en vec.push

```no
inner = [1, 2, 3]
outer.push(inner)
; los datos de inner se clonan profundamente en la nueva posición de elemento de outer
; inner sigue poseyendo datos independientes; al salir de la función inner y outer se liberan por separado
```

push realiza un clon profundo en los tipos de elemento con propiedad en el montón (`%str-long`/`%vec`/`%arr`/estructuras de usuario): malloc de nuevos datos + memcpy + clon recursivo de elementos. La variable fuente y el vec exterior poseen datos independientes y **no necesitan marca de move**, lo que evita el double-free. Los elementos de tipos primitivos (i64/f64, etc.) simplemente almacenan el valor.

### Seguimiento de move en tiempo de ejecución (mapa de bits indexado por subíndice de variable de montón)

Un move bajo una rama condicional plantea un desafío: en tiempo de compilación no se puede determinar si un move llega a ocurrir realmente.

```no
cond-move = (flag i64) (out []i64) {
    x = [1, 2, 3]
    if flag == 1 {
        out = x; move solo ocurre si flag==1
    }
    ; si flag==0, x sigue poseyendo datos y hay que liberarlos al salir de la función
    ; si flag==1, la propiedad de x se ha transferido y hay que omitir el free al salir
}
```

Nolang resuelve este problema con un **mapa de bits indexado por subíndice de variable de montón**. En tiempo de compilación se asigna un `varIdx` único a cada variable local de montón; cada bit del mapa en tiempo de ejecución corresponde a una variable de montón (no a un parámetro de salida).

#### Estado del compilador

| Campo | Tipo | Uso |
|------|------|------|
| `heapVarIndex` | `map[string]int` | nombre de variable de montón → `varIdx` (solo variables locales de montón) |
| `outBindState` | `[]int` | subíndice de la variable de montón actualmente enlazada a cada parámetro de salida (-1=sin enlace, -2=indeterminado) |
| `movedVarBitset` | `[]uint64` | mapa de bits moved en tiempo de compilación (cuando no hay mapa en tiempo de ejecución) |
| `movedBitmapBase` | `string` | prefijo del nombre de la variable del mapa en tiempo de ejecución (p. ej. `%__mb`; vacío=sin asignar) |
| `bitmapCount` | `int` | número de bloques u64 del mapa (= maxVarIdx/64 + 1) |

#### Reglas de mapeo de subíndices

Un bloque `u64` guarda 64 bits de marca; para el subíndice de variable de montón `varIdx`:

- número de bloque = `varIdx / 64`
- desplazamiento dentro del bloque = `varIdx % 64`
- máscara = `1u64 << desplazamiento`

En tiempo de compilación se calculan constantes directas; en tiempo de ejecución no hay coste de cálculo. Varios bloques `u64` pueden admitir cualquier número de variables de montón, **sin límite en el número de parámetros o valores de retorno**.

#### Tratamiento de la asignación move (limpia el bit anterior)

Cada vez que se mueve una variable de montón a un parámetro de salida:

1. Si ese parámetro de salida ya estaba enlazado a otra variable (`outBindState[outIdx] >= 0`), primero se limpia el bit correspondiente a la variable antigua
2. Después se pone a 1 el bit correspondiente a la variable actual
3. Se actualiza el subíndice de variable enlazada a ese parámetro de salida (`outBindState[outIdx] = srcVarIdx`)

#### Liberación al final de la función

Se recorren todas las variables de montón y se emiten `if` independientes y aplanados: si el bit correspondiente es 0, se libera; si es 1, significa que la propiedad se ha transferido y se omite la liberación.

#### Asignación del mapa de bits bajo demanda

La variable del mapa de bits solo se asigna cuando es **necesario**, para evitar el coste de rendimiento en escenarios sin ramas:


| Escenario | Asignación del mapa de bits | Comportamiento de free |
|------|---------|----------|
| Sin move | No se asigna | Todo se libera |
| Move fuera de una rama (move determinista) | No se asigna | El `movedVarBitset` de compilación omite directamente el free |
| Move dentro de una rama (move condicional) | Se asigna | Comprobación del mapa en tiempo de ejecución: bit=1 omite, bit=0 libera |

El compilador preexplora el AST antes de generar el cuerpo de la función (`detectBranchMoveToOut`) para detectar si existe una asignación move a un parámetro de salida dentro de una rama `IfExpression`/`ForStatement`/`ConditionalExpression`, y solo asigna la variable del mapa de bits en tiempo de ejecución cuando existe ese patrón. El `alloca` del mapa se inserta después de generar el cuerpo de la función (cuando `nextHeapVarIdx` ya tiene su valor final) y se escribe en el bloque de entrada.

Este mecanismo se aplica a todos los tipos de montón (`vec`/`str-long`/`arr`/estructuras de usuario).

## Clon profundo (asignación entre variables locales)

```no
a []i64 = [10, 20, 30]
b = a; Clon profundo: malloc nuevos datos + memcpy + clon recursivo de elementos
b[0] = 99
; a[0] == 10 (a no se ve afectado)
; b[0] == 99 (b se modifica de forma independiente)
```

### Proceso de clon profundo

1. Liberar el valor anterior de la variable de destino (si ya tenía datos en el montón)
2. `malloc` de un nuevo búfer de datos y `memcpy` de los datos de la fuente a los nuevos datos
3. Clonar recursivamente cada elemento con propiedad en el montón:
   - elemento `%str-long`: malloc + memcpy de los datos de la cadena
   - elemento de estructura de usuario: memcpy de la estructura + clon recursivo de los campos con datos en el montón
4. Escribir el nuevo puntero de datos, len y cap en la variable de destino
5. Registrar el destino como variable de montón (se libera al salir de la función)

### Tipos clonables

| Tipo | Se puede clonar en profundidad | Descripción |
|------|-------------|------|
| `%vec` / `%arr` (elementos de tipos primitivos) | ✅ | Basta con memcpy de los datos |
| `%vec` / `%arr` (elementos %str-long) | ✅ | malloc+memcpy de los datos de la cadena elemento por elemento |
| `%vec` / `%arr` (elementos de estructuras clonables) | ✅ | Clon recursivo de los campos de la estructura elemento por elemento |
| `%vec` / `%arr` (elementos %vec / %arr) | ✅ | Clon recursivo de los elementos del contenedor interior mediante el mecanismo `elemElemType` |
| `%str-long` | ✅ | malloc + memcpy de los datos de la cadena |
| estructura de usuario (sin campos de contenedor anidados) | ✅ | memcpy de la estructura + clon recursivo de campos de montón |
| estructura de usuario (con campos de contenedor anidados) | ✅ | memcpy de la estructura + clon recursivo de los campos con contenedores anidados (mediante `elemElemType`) |
| `?str` | ✅ | option con payload en línea: clonar la opción en sí y sus datos de cadena |
| `?[]T` | ✅ | option con payload en línea: clonar la opción en sí y los datos del segmento interior (clon elemento por elemento, recursivo si los elementos poseen montón, como `?[]str`) |

> **`?str` / `?[]T` tienen payload en línea, pero poseen montón.** Sus payload se guardan directamente en el
> `%option_<key>` de la propia opción (la estructura de `?str` es `{ i8 tag, [2 x i64] }`, con una carga de 16 bytes que guarda
> `{ u32 len, u32 cap, i8* data }`), pero los datos `str` / el almacenamiento de respaldo del segmento están asignados en el montón
> ⇒ **al clonar hay que copiar profundamente también el payload y al liberar hay que liberar también el payload**; ambos lados deben ir en pareja.
> La liberación de `?[]T` usa el mismo helper de free profundo que `[]T` (decide si recurre según el tipo de elemento), de modo que
> aquí también se cumple la dualidad «clon profundo con free profundo».
> ⚠️ Las options generales (no `?str`/`?[]T`) tienen un payload prestado o de puro valor y **no poseen** montón; clon y free no lo tocan.
> Cuando la carga es demasiado grande y supera `option-payload-inline-threshold`, la opción pasa a guardar un puntero al montón y entonces el free debe liberar también la caja
> (determinado por `Boxed`, véase `src/mir/layout.go`).

### Diferencia con move

- **Clon profundo**: la fuente y el destino poseen datos independientes y cada uno se libera al salir de la función
- **Move**: la fuente renuncia a la propiedad (se marca moved), el destino toma los datos y la fuente omite free

Reglas de decisión para `b = a`:

1. Si a es la fuente de un parámetro de salida → move
2. Si no, si a tiene propiedad en el montón pero **ya no se referencia tras la asignación** → move (sin copias, observacionalmente equivalente al clon profundo)
3. Si no, si a tiene propiedad en el montón y se puede clonar en profundidad → clon profundo
4. Si no, copia de valor

`vec.push(x)` no entra en estas reglas: push es una llamada a método y, tanto si x tiene propiedad en el montón como si no, realiza un clon profundo de los elementos con propiedad en el montón (véase la sección anterior).

## Move de tipos de pila (religado de ranura de pila / slot-rebind)

Aunque `i64`/`u64`/`i128`/`u128`/`txt` son tipos de pila (no poseen montón), en `b = a` (con RHS una variable), cuando la fuente `a` no tiene referencias posteriores, ya no se hace copia de valor, sino un **religado de ranura de pila** (stack-slot rebind): se establece `g.varAlias[b] = a` para que `b` y `a` compartan la misma ranura de pila (0 copias). Esto es mejor que la copia de valor (que todavía tiene que emitir load+store) y resulta especialmente rentable para tipos de pila grandes (como `%txt`, de 256 bytes).

### Restricción can_slot_rebind (corrección conservadora)

El religado de ranura de pila es **más estricto** que el `moveEligible` del move de montón:

- `moveEligible`: permite el move si la fuente `a` **no se lee** después (válido para tipos de montón; tras el move la fuente omite free y no afecta al destino).
- `can_slot_rebind` (`slotRebindSafe`): permite el religado solo si la fuente `a` **no tiene ninguna referencia posterior (lectura ni escritura)**. Como tras el religado `b` y `a` comparten la misma ranura de pila, una escritura posterior en `a` destruiría el valor de `b`.

Cálculo: `generateFunctionDefinition` llama a `computeMoveEligibility` (que rellena a la vez `moveEligible` y `slotRebindSafe`); `generateMainFunction` llama a `computeSlotRebindSafety` (**solo** rellena `slotRebindSafe` y no toca `moveEligible`: la función principal de HEAD no habilita el move de montón y se mantiene desactivado para evitar SEGFAULT). Ambos usan `stmtContainsVarRefAny` para una exploración de referencias consciente de ramas/bucles (incluye los valores izquierdos de `AssignExpression` y los retornos de bucle).

Cuando no se cumple `can_slot_rebind` (la fuente sigue referenciada) → **degrada a copia de valor completa**, nunca a un comportamiento inseguro.

### Escenarios de desactivación (deben seguir la vía de asignación normal)

| Escenario | Motivo |
|------|------|
| Función de stdlib (`curIsStdLib`) | stdlib contiene construcciones (match/closure/coroutine) que el análisis de referencias no puede modelar por completo; el religado corrompería la ranura compartida (p. ej. el `it` interno de fmt alias de la variable local `n`). Detección: `g.stdModules` (inyectado desde `checker.KnownStdModules()` por `SetStdModules` de `transpiler.go`), con retroceso a `g.funcOwner[fd.Name]` |
| El destino es un parámetro de salida | Los parámetros de salida reciben un puntero de quien llama; el religado lo haría apuntar a la ranura local de la fuente y quien llama no podría leerlo |
| El destino es una variable global | El religado haría que el nombre global resolviera a la ranura local de la fuente, rompiendo la semántica global |
| El destino es una variable de tipo montón | El religado de ranura de pila solo se aplica a tipos de pila |
| La fuente es un parámetro | Los parámetros se pasan por referencia; el religado corrompería el marco de pila de quien llama |
| La fuente es una variable global | La dirección global quedaría aliasada a la fuente local, con error semántico |

### Invalidación de alias tras reasignación

Cuando se reasigna una variable (`b = ...`), primero se hace `delete(g.varAlias, name)` para eliminar cualquier alias residual que pudiera quedar y evitar que la vía genérica de asignación siga apuntando, mediante `varAddr(name)`, a la ranura antigua de la fuente y la contamine. Al establecer el alias nuevo se hace una **resolución transitiva** a lo largo de la cadena de alias (`src := ident.Value; for { if n2,ok := g.varAlias[src]; ok { src = n2 } else break }`) para encontrar la ranura final de la fuente y evitar desalineamientos de alias de varios saltos.

### Ortogonalidad con el move de montón

El religado de ranura de pila solo actúa sobre tipos de pila y no implica transferencia de propiedad ni `free`; es totalmente independiente del mecanismo de clon/move de los tipos de montón.

**Prueba**: `tests/slot-rebind.no` (cubre religado de i64/u64/i128/u128/txt, degradación a copy, invalidación de alias tras reasignar el destino, degradación de move de parámetro, paso de consume; salida esperada `42 7 1 1 17 5 5 10 100 99 84`).

## Valor de retorno str de FFI extern

El puntero de cadena C (`i8*`) devuelto por una función extern FFI (marcada `#{c}`) puede apuntar a memoria estática (como `getenv`, `strerror`) o a un búfer externo (como el puntero devuelto por `strchr`, que apunta al interior del parámetro). Envolverlo directamente en un `%str-long` provocaría un `free()` de memoria no perteneciente al montón durante `emitHeapFree` → UB.

El compilador inserta una copia segura en la vía de retorno `str` de FFI extern:

1. **Comprobación de NULL**: si C devuelve NULL, se construye un `%str-long` vacío (data=0, len=0). Nótese que `str` no es una opción, por lo que **`s == nil` es un error de compilación** (`q4nilcmp`); para distinguir NULL de una cadena vacía, declare el extern con retorno `?str` (NULL ⇒ `nil`) o compruebe `s.len == 0`
2. **No NULL**: `strlen` + `malloc` + `memcpy` + terminador nulo, copiando a un búfer de montón independiente
3. **Fusión PHI**: se unen las dos vías y se construye el `%str-long` de retorno

```no
#{c}
strchr = (s str, c i64) (r str)

find = () (r str) {
    r = strchr('hello', 108); C devuelve un puntero al interior de 'hello'
    ; el compilador hace automáticamente malloc+memcpy para copiar; r posee data de forma independiente
    ; al salir de la función emitHeapFree libera r.data de forma segura
}
```

Este mecanismo es coherente con la vía `RetCStrToStr` de clib (usada por integradas como `get-env`, `get-wd`) y garantiza que todos los valores de cadena C devueltos tengan propiedad independiente.

## Liberación de variables de nivel de módulo

Las variables de montón de nivel de módulo (`vec`/`str`/`arr`/estructuras) se compilan como globales LLVM (`@name`) y sus búferes `data` se inicializan con malloc en las sentencias de nivel superior de la entrada `main`.

El compilador llama, antes del `ret i32 0` del `main` de entrada en C:

1. `emitHeapFree`: libera las variables locales de montón de nivel superior (no globalVars)
2. `emitGlobalHeapFree`: recorre `moduleVarTypes` y libera todos los tipos con propiedad en el montón de `globalVars`

```no
GLOBAL-STR = 'hello'; LLVM @GLOBAL-STR = global %str-long zeroinitializer
GLOBAL-VEC = [1, 2, 3]; LLVM @GLOBAL-VEC = global %vec zeroinitializer
; las sentencias de nivel superior hacen malloc de data y lo guardan en el global
; antes del ret de main, emitGlobalHeapFree libera data
```

Esto evita la acumulación de fugas de memoria en servicios de larga duración (como un daemon con bucle). No afecta a las herramientas CLI de un solo uso (el SO recupera la memoria al salir el proceso).

## Vistas de segmento

La expresión de segmento `arr[1..3]` produce una vista (sin copias) que comparte los datos del array original. Tres destinos posibles para la vista:

| Destino | Comportamiento | Propiedad |
|------|------|--------|
| Variable local `v = arr[1..3]` | Vista sin copias | Comparte los datos del array original |
| Parámetro de salida `out = arr[1..3]` | Clon (malloc+memcpy) | Propiedad independiente |
| Tipo `[]T` explícito `v []i64 = arr[1..3]` | Clon | Propiedad independiente |

**Motivo**: el parámetro de salida escapa hacia quien llama; el array original podría liberarse antes de que termine la función, de modo que la vista debe clonarse en datos independientes.

## Reasignación y liberación del valor anterior

```no
s = 'hello'; malloc del búfer de datos
s = 'world'; libera los datos de 'hello', malloc de nuevos datos
```

Al reasignar un tipo con propiedad en el montón, el compilador libera automáticamente los datos del valor anterior antes de la asignación, para evitar fugas.

## Liberación de campos de estructura

```no
Node {
    name str
    items []i64
}

n = Node {
    name: 'hello'
    items: [1, 2, 3]
}
; al salir de la función se libera recursivamente:
;   - n.name.data (campo %str-long)
;   - n.items.data (campo %vec)
```

Al liberar una estructura se recorren todos los campos y, para los campos con propiedad en el montón, se liberan sus datos recursivamente.

## Reasignar un array fijo a un segmento

```no
local [4]i64 = [100, 200, 300, 400]; local es un array fijo (16 bytes)
local = [100, 200, 300]; reasignación a un segmento (16 bytes)
```

El diseño en memoria de un array fijo (`%arr`, 2 campos) y un segmento (`%vec`, 3 campos) es distinto. Al reasignar, el compilador asigna automáticamente una nueva variable `%vec` y redirige todos los accesos posteriores, para evitar desbordamientos de búfer.

## Casos de prueba verificados

Las pruebas están en `tests/mem-safety/`:

| Prueba | Qué verifica |
|------|---------|
| `deep-clone.no` | Independencia del clon profundo de `b = a` ([]i64/[]str/str/estructura) |
| `deep-free-str.no` | Free profundo de `[]str` |
| `deep-free-nested-vec.no` | Free profundo de `[][]i64` + clon profundo en push |
| `deep-free-struct-vec.no` | Free profundo de `[]MyType` (estructura recursiva) |
| `struct-field-leak.no` | Liberación de datos de montón en campos de estructura |
| `slice-view-escape.no` | Clon al asignar una vista de segmento a un parámetro de salida |
| `reassign-leak.no` | Liberación del valor anterior al reasignar |
| `vec-push-leak.no` | Liberación del búfer anterior y clon profundo de elementos con propiedad en el montón al crecer vec.push |
| `ffi-str-return.no` | Copia segura del valor de retorno str de FFI extern |
| `global-heap-free.no` | Liberación de variables de montón de nivel de módulo al salir de main |
| `double-move-same-source.no` | `a=x; b=x` con múltiples asignaciones de la misma fuente: primer clon + último move |
| `move-clone-liveness.no` | Preanálisis de Liveness decide clon/move (con ramas condicionales y tres asignaciones) |
| `rule1-binding.no` | Regla uno: independencia de escritura de `str`／`[]i64`／struct／`?str`／`?[]i64` en `b = a`, alias de solo lectura, move de fuente muerta |
| `nested-container-clone.no` | Dualidad clon profundo／free profundo en contenedores anidados (`[][]str`, vec dentro de struct, valores de map que son vec, etc., 12 casos) |

## Limitaciones conocidas

### Contenedores map

hashmap no implementa el free profundo de key/value, por lo que se filtran los datos de montón del contenedor map.

### Variables temporales en bucle

```no
#{fmt-skip}
loop {
    s = 'temp'   ; cada iteración hace malloc de nuevos datos, los datos anteriores no se liberan
}
```

### Vista de segmento + move del array original

```no
view = arr[1..3]; view comparte arr.data
arr = [9, 8, 7]; libera el arr.data anterior → view queda colgando
```

### Datos compartidos en async (corregido el 2026-09-30)

El límite spawn de `awy { r = co f(x) }` ahora **copia profundamente** los argumentos que poseen montón; la tarea ya no comparte búfer con quien llama.

Las corrutinas son cooperativas y de un solo hilo, así que «compartir datos mutables en el montón entre corrutinas» no existe en el lenguaje; lo único que se compartía era el valor que **quien llama seguía reteniendo tras el spawn**. Antes de la corrección, argbuf era solo una **copia de bits** del descriptor del argumento, con `data` compartido con quien llama, y la tarea no se ejecutaba hasta el punto de espera: si quien llamaba reasignaba o salía del ámbito antes, liberaba el búfer y la tarea leía memoria ya liberada (medido: str imprimía 40 NUL, `[]i64` imprimía 0, ambos con rc 0 y sin ningún diagnóstico).

Ahora se copia profundamente en el momento del spawn y el wrapper libera la copia al terminar la llamada:

| Tipo de parámetro | Límite spawn | Liberación en el wrapper |
|---|---|---|
| `str` | `@str_clone` | `@str_free` |
| `[]T` (T con helper deep-clone) | `vecDeepClone` | `@vec_free` |
| struct que posee montón (hojas `str` en línea／campos puntero) | clon de hojas in situ + copia del pointee | destructor recursivo de la estructura |
| Otros (tipos de puro valor) | copia de bits (correcta) | solo libera el contenedor |

Regresión: `tests/async-ownership.no`.

### Fuga en reasignación de struct (corregido el 2026-09-30)

```no
holder {
    s str
}
h holder = holder {
    s: 'first'
}
h = holder {
    s: 'second'
}
; el búfer antiguo de 'first' antes se filtraba
```

Al religar una variable local, el valor anterior solo se libera si su tipo es `Type.Owned` (`str`/`vec`/`[]T`/`map`/`?owned`). Un struct **no cuenta deliberadamente** como `Type.Owned` (esa marca también la usa el lowerer para decidir si «un enlace es un alias»), de modo que `h = holder { … }` sobrescribe toda la estructura y el búfer antiguo de `s` nunca se libera. Pero el mecanismo drop sí reconoce los structs (`dropOwnsHeap` → `typeOwnsHeap`, y `emitDrop` libera con un destructor recursivo): solo la compuerta del lowerer era demasiado estrecha. Tras la corrección se liberan los campos antiguos antes de religar.

Medido con 2.000.000 de reasignaciones: RSS pico de 66,1 MB → 33,8 MB, salida byte a byte idéntica.

### Primera longitud de campo de un struct literal es una llamada a método (corregido el 2026-09-30)

```no
holder {
    s str
}
h = holder {
    s: n.to-str()
}
; antes de la corrección: 'holder' is not defined
```

La clasificación de `{ … }` se decide mirando unos tokens por delante. El **separador de rama** de match también es `:`, así que `s: n.to-str()` se parece tanto a «una rama de match con patrón `s`» como a «un campo de struct literal». El único desambiguador es el token **después** de `ident.ident`: una llamada a método solo puede ser un valor de campo. La rama antigua omitía `(`, de modo que todo el bloque se clasificaba como match, el nombre del struct no se encontraba en contexto de match y se informaba de que estaba sin definir (si el valor del campo era una llamada a función normal, funcionaba).

Tras la corrección, `h = holder { s: recv.method(args) }` se analiza correctamente. Una rama de match de nolang no puede empezar con `name : X(` (el separador de rama es `->`; una rama que empieza con `:` es obligatoriamente wildcard/default y su cuerpo no lleva llamada entre paréntesis), de modo que permitir `(` no hace que ningún match válido se confunda con un struct literal. Regresión: `src/parser/struct_literal_field_value_test.go`.

### Tipo de resultado de await (simplificación de sintaxis del 2026-11-17)

La sintaxis antigua exponía el identificador de tarea como una variable `i64` y era necesario transportar el tipo de resultado mediante «religado de ranura de handle» (`t i64 = 0; t = run f(...); v = awy t`). Con la eliminación de `run` y `awy <handle>`, el tipo de resultado de la expresión `co` lo determina directamente quien recibe la llamada, y esa vía ya no existe. La regresión ahora la fija `src/mir/async_boundary_ownership_test.go`, que comprueba el tipo de resultado del MIR subyacente.

### Recuento de referencias de tareas (simplificación de sintaxis del 2026-11-17)

El `%task` interno sigue siendo el único objeto con recuento de referencias: al crear el spawn, `rc = 1`, y se `release` cuando termina la espera. La sintaxis antigua exponía identificadores manuales y las vías de copia como `h2 = h` / `run h` olvidaban el retain (especialmente grave en tareas con resultado void; se midió SIGSEGV), y el verificador `checkRefBalance` lo detectaba. El nuevo `awy { ... }` no expone identificadores y el grupo espera exactamente una vez por cada `co`, de modo que la sintaxis de superficie no puede producir referencias fuera de la lista. Las pruebas `.no` y de Go de alias de handle / RC se eliminaron junto con `run`; el emparejamiento `OpTaskRetain` / `OpAwait` en la capa MIR sigue cubierto por los fixtures MIR de `src/mir/opt_test.go`.

### Mover los argumentos de spawn: sin copias cuando la fuente está muerta (2026-10-01)

`co f(x)` hace una copia profunda en el límite spawn de cada argumento que **posee montón**, porque la tarea podría no ejecutarse hasta el final del grupo y quien llama podría reasignarlo o salir del ámbito antes. Cuando se sabe que x **no se usará más** tras el spawn, esa copia es redundante: la propiedad se **transfiere** directamente a la tarea.

```no
s str = 'hello'
awy {
    v = co echo(s)      ; s no se lee después -> transferencia sin copias
}
print(v)                ; hello

s str = 'hello'
awy {
    v = co echo(s)
}
print(s)                ; s se sigue leyendo -> se mantiene el clon profundo (cada lado posee el suyo)
print(v)                ; hello
```

Tres criterios, todos necesarios:

1. Quien llama **ya iba a** liberar x: los préstamos (vistas de segmento, borrow read) y los parámetros no los libera quien llama;
2. x está muerto tras el spawn (**no hay ninguna lectura posterior**);
3. el parámetro de ese lugar en quien recibe la llamada es `str` / `[]T` / un `struct` que posee montón: el wrapper libera su payload.

Para esas tres clases, la liberación del wrapper de la tarea y el drop original de quien llama **se degradan a la misma helper**, de modo que la transferencia solo mueve **la misma liberación** de quien llama a la tarea: ni se libera de más ni de menos.

**Semántica de usuario invariable**: los parámetros de una tarea que no se espera ya los libera el wrapper al esperar; mover y copiar son idénticos a este respecto.

Implementación: `insertDrops` lo registra en `Module.spawnArgMoves` y `emitAsyncRun` omite la copia profunda en consecuencia. Regresiones: `tests/async-arg-move.no`, `src/mir/spawn_arg_move_test.go`.

### Alias en la tabla de esperadores de corrutinas (corregido el 2026-09-30)

El planificador usaba `@nolang_waiters = global [256 x i8*]` con `ptrtoint(task) & 255` como índice. Las tareas las asigna `@malloc` y están **alineadas a 16 bytes**, así que los bits bajos del puntero solo tienen 4 bits significativos: las 256 ranuras se colapsaban en 16 (medido: solo se usaban 8) y dos tareas esperando simultáneamente sobrescribían fácilmente el esperador de la otra y despertaban la tarea equivocada.

Ahora se elimina toda la tabla y el esperador se guarda en el quinto campo del propio `%task` (`%task` pasa de 24 a 32 bytes): `@nolang_async_wait` escribe la tarea actual en el campo de **quien se espera**, y `@nolang_async_done` lee ese campo y, si no es nulo, lo encola y lo limpia. La consulta es ahora exacta, sin alias de dirección.

> Nota: el backend MIR actualmente impulsa await de forma síncrona y nunca llama a `@nolang_async_run`／`@nolang_async_wait`, así que hoy se trata de un defecto **latente** y no de un fallo observable. Regresión: `src/mir/async_boundary_ownership_test.go`.

### `awy` se consume cuando no es la primera sentencia del bloque (corregido el 2026-09-30; reescrito el 2026-11-17)

`skipToStatementEnd()` avanza hasta un token que pueda **empezar** la siguiente sentencia, y la lista blanca `isStatementBoundary()` (`src/parser/stmt.go`) antes omitía las palabras clave de prefijo. El `run` / `awy t` de la sintaxis antigua podía por tanto tragarse, junto con el salto de línea, cuando no era la primera sentencia, causando una degradación semántica con `rc=0` y sin diagnóstico.

En la sintaxis nueva, `awy` tiene su propio caso en `parseStatement` (grupo de corrutinas) y figura junto a `co` en `isStatementBoundary()`; `run` ya no es palabra clave y se ha retirado de la lista. La regla es: **todo token que `parseStatement` pueda despachar en su `switch` debe figurar también en `isStatementBoundary()`**.

Regresión: `tests/stmt-boundary.no`, `src/parser/stmt_boundary_block_test.go`.

### Un `{ ... }` desnudo en posición de sentencia se toma por una declaración (corregido el 2026-09-30)

La rama `LBRACE` de `parseStatement` llama a `classifyBlockAtCurrent()` (cuando `currentToken` ya es `{` y no hay nombre que consumir). Los valores `blockEnum` / `blockIface` / `blockTaggedEnum` que devuelve **no tienen sentido en posición de sentencia**: las verdaderas declaraciones de enumeración／interfaz／enumeración etiquetada necesitan un nombre previo y van por la rama anterior `classifyBlock()`. Pero el despacho de sentencias solo dirigía `blockUnknown` al bloque de sentencias, de modo que:

- `{ x = 2 }` se clasificaba como `blockEnum` y `{ print(2) }` como `blockIface`;
- ambos se enviaban a `parseExpressionStatement`, y la rama `LBRACE` de `expr.go` solo reconoce `blockStruct` / `blockMatch`; los demás hacen `p.nextToken(); return nil`;
- se tragaba el `{`, las sentencias del bloque se convertían en **hermanas** del cuerpo exterior y el `}` del bloque cerraba en cambio el bloque **exterior**.

El resultado no era «tragar» sino **filtrar una sentencia un nivel hacia fuera**: `f = () { print(1); { print(2) }; print(3) }` imprimía `3,1,2`; `f = () { x = 1; { x = 2 }; print(x) }` no imprimía nada. Igualmente `rc=0`, sin diagnóstico.

La corrección consiste en dirigir también `blockEnum` / `blockIface` / `blockTaggedEnum` al bloque de sentencias en el despacho (`blockStruct` debe **permanecer** en la vía de expresión: `{ field: value }` es una sentencia de literal de estructura anónima válida).
⚠️ **No** cambie la rama `ASSIGN` de `classifyBlockAtCurrent` para «arreglar» esto: solo cubre una de las tres clasificaciones erróneas y además traslada la decisión fuera del único lugar que sabe «estoy en posición de sentencia».

⚠️ **Efecto colateral**: esta corrección hace que `{ ... }` en posición de sentencia sobreviva por primera vez como un `*parser.BlockStatement` real, y la lista blanca de `programUsesPrint` (`src/build/transpiler.go`, que decide si hay que cargar `fmt`/`io`/`str`/`byte` mediante un walker AST escrito a mano) no incluye `BlockStatement`, de modo que un programa que solo usa `print` dentro de un bloque desnudo deja de detectarse.
**Al tocar la capa de sentencias hay que volver a ejecutar `go test ./build/ -run TestProgramUsesPrint`** y añadir el nuevo tipo contenedor a `walkStmt`. Regresiones: `src/parser/stmt_boundary_block_test.go`, `tests/stmt-boundary.no`.

### La comprobación de omisión de free en la primera asignación de variables globales no es lo bastante precisa

El compilador usa el mapa de compilación `globalFirstAssigned` para registrar si ya se ha hecho la primera asignación de una variable global: la primera asignación omite la liberación del valor anterior (que es `zeroinitializer`, no datos de montón) y solo las reasignaciones posteriores liberan el valor de montón anterior.

Este mapa se inicializa una sola vez durante toda la compilación, no se reinicia por función y no distingue las rutas de ramas condicionales. Si una variable global se asigna por primera vez dentro de una rama condicional, el compilador la procesa en orden AST: la primera sentencia de asignación se marca como «primera» (omite free) y la segunda (aunque esté en otra rama) sigue la vía de reasignación (intenta liberar el valor anterior). Si en tiempo de ejecución se ejecuta primero la segunda rama, la global sigue siendo `zeroinitializer` (data=NULL, len=0) e intentará liberar un valor anterior no inicializado.

**Mitigación actual (eficaz)**:
- Contenedores superficiales (`%str-long`): `emitNullCheckFree` genera una comprobación en tiempo de ejecución `icmp eq i8* dataPtr, null` y omite `call @free` si es NULL
- Contenedores profundos (`%vec/%arr`): `emitDeepContainerFree` añade además un cortocircuito `len == 0`; el len de zeroinitializer es 0, así que se omite directamente todo el bucle de liberación

Estas dos capas de protección en tiempo de ejecución hacen que, aunque la decisión en tiempo de compilación no sea lo bastante precisa, en la práctica no haya fallos. Pero lógicamente se depende de la comprobación NULL en tiempo de ejecución como red de seguridad, en lugar de una decisión precisa en tiempo de compilación.

**Posible mejora**: convertir `globalFirstAssigned` de un mapa de compilación en un mecanismo de seguimiento en tiempo de ejecución (similar al mapa de bits `movedVarBitset`), pero eso añadiría coste en tiempo de ejecución y la mitigación actual ya es suficientemente eficaz.

## Compromisos de diseño

1. **El coste del clon profundo.** `b = a` asigna y copia una copia de los datos; `vec.push(x)` también hace un clon profundo de los elementos con propiedad en el montón.
   Es deliberado: **se cambia una copia por eliminar toda una categoría de análisis de alias**: entre valores solo se mantienen «propiedad» y «traslado», no uso compartido,
   de modo que «quién posee, quién libera y si una escritura es observable» no necesita un conjunto de reglas distinto para cada caso de alias.
2. **La copia profunda en el límite spawn (= instantánea).** `co f(x)` copia profundamente en el momento del spawn los argumentos que poseen montón, de modo que la tarea ve
   el valor **en el instante del spawn**, independientemente de lo que escriba quien llama después. **Cuando la fuente está muerta se pasa a move sin copias**.
   ⚠️ No cambie esto por «compartir el mismo búfer más recuento de referencias» para ahorrar la copia: eso haría que la tarea viera las **escrituras in situ** de quien llama entre el spawn y la espera al final del grupo (uso compartido), violando la regla 4. La forma correcta de ahorrar la copia es **copiar solo al escribir** (copy-on-write).
3. **El recuento de referencias de tareas no forma ciclos.** El `%task` interno es el único objeto con recuento de referencias; el nuevo `awy { ... }` no expone identificadores
   y cada `co` espera exactamente una vez al final del grupo, de modo que no se forma un ciclo de «tareas que se tienen handle mutuamente».
4. **No se comparten datos mutables en el montón entre corrutinas.** Las corrutinas se planifican de forma **cooperativa y de un solo hilo**; compartir datos mutables en el montón entre corrutinas no existe en el lenguaje;
   permitirlo solo traería carreras de datos y punteros colgantes. Por ello, el límite spawn siempre transmite **copias independientes** (o transfiere la propiedad).
