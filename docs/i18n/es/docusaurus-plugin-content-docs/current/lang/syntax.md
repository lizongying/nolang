\---  
sidebar_position: 2  
\---

# Sintaxis


## Comentarios

Nolang admite marcadores de **comentario de una sola línea** y de **comentario multilínea (de bloque)**:

- `;` — marcador de una sola línea (comenta hasta el final de la línea).
- `;; <contenido>` — marcador de una sola línea (cuando después de `;;` **queda contenido en la misma línea**, comenta hasta el final de la línea, con la misma semántica que `;`).
- `;;\n` — comentario multilínea (de bloque): se activa cuando a `;;` **le sigue inmediatamente un salto de línea** (solo se permiten espacios en blanco), y termina al encontrar otro `;;` seguido también de salto de línea o EOF.

```no
; esto es un comentario
; esto también es un comentario, con la misma semántica
;; esto sigue siendo un comentario de una sola línea (no hay salto de línea tras ;;)
x = 1; comentario en línea, hasta el final de la línea
x = 2;; comentario de una sola línea en línea, con la misma semántica

;;
este es un comentario multilínea (de bloque)
puede abarcar varias líneas
hasta encontrar un ';;' aislado
;;

y = 3
;;
la comprobación de cierre debe ir seguida de salto de línea o fin de archivo
para considerarse delimitador de cierre
;;
```

> **Regla de activación multilínea:** después de `;;` **solo puede haber espacios en blanco** (espacios/tabuladores) hasta el salto de línea o el EOF para entrar en modo multilínea. Si detrás de `;;` hay cualquier carácter no blanco en la misma línea, se trata de un comentario de una sola línea (hasta el final de la línea).
>
> **Regla de cierre multilínea:** el `;;` de cierre también debe ir seguido de un salto de línea o del EOF (con solo espacios en blanco entremedias). Un comentario multilínea sin cerrar sigue comentando hasta el final del archivo.

> **Regla: una sentencia por línea, prohibido escribir varias en una sola línea.**
> El punto y coma `;` es un **marcador de comentario**, no un separador de sentencias; la coma `,` tampoco puede unir sentencias. Cada sentencia dentro de un mismo bloque `{ ... }` debe ocupar su propia línea.
> Esta regla también se aplica a los ejemplos de código dentro de comentarios: incluso allí, no use `;` ni `,` para poner varias sentencias en una línea, para no inducir a error al lector.

```no
#{fmt-skip}
; ❌ Error: usar `;` para separar varias sentencias — todo lo que sigue a `;` es comentario,
; incluso la `}` del bloque queda comentada, el bloque no se cierra y el código posterior se descarta silenciosamente
; cond -> { print('X'); return }

; ❌ Error: usar comas para unir sentencias
; h0 = 1732584193, h1 = 4023233417
; out = from-i64(v), out = from-u64(v)
; debug(msg), info(msg), warn(msg)
```

```no
; ✅ Correcto: cada sentencia ocupa su propia línea
cond -> {
    print('X')
    return
}

; ✅ Correcto: cada sentencia ocupa su propia línea
; h0 = 1732584193
; h1 = 4023233417
; out = from-i64(v)
; out = from-u64(v)
; debug(msg)
; info(msg)
; warn(msg)
```

> **Por qué `;` es especialmente peligroso:** en `cond -> { print('X'); return }`, todo `; return }` se considera comentario y el código real que queda es solo `cond -> { print('X')`. El bloque nunca se cierra y todas las sentencias posteriores a la `}` (normalmente todo el resto del cuerpo de la función) quedan engullidas; además, el error puede aparecer muy lejos del fallo real y resultar dificilísimo de localizar.

## Tipos de datos

Tipos básicos

- byte
- bool ; solo se permiten minúsculas
- char ; tipo carácter: un único punto de código escalar Unicode (rune), almacenado internamente como i32. Se escriben comillas dobles con un solo carácter, p. ej. "中"
- str ; tipo cadena, con comillas simples 'hello', o cadena cruda entre comillas invertidas `contenido-multilínea`
- txt ; tipo de texto de longitud fija, 256 bytes fijos por debajo (máximo 255 bytes de datos), requiere anotación de tipo explícita, p. ej. t txt = 'abc'
- i8
- i16
- i32
- i64 ; tipo numérico por defecto, independiente de la arquitectura
- i128 ; entero con signo de 128 bits
- u8
- u16
- u32
- u64
- u128 ; entero sin signo de 128 bits
- f32
- f64

### Tipo char (punto de código Unicode)

`char` representa un **único punto de código escalar Unicode** (scalar value). Su tipo de almacenamiento subyacente es **`i32`**, no `i64`:

- **Rango válido**: `0 ..= 0x10FFFF` (es decir `0 ..= 1114111`). El valor máximo del punto de código solo necesita 21 bits, así que `i32` lo contiene sin problema.
- **Error fuera de rango**: un literal que exceda ese rango produce **error directo** en compilación (incluido `no vet`). Por ejemplo `c char = 0x110000` ✗, `c char = 1114112` ✗; `c char = 1114111` ✓.
- **Operaciones numéricas permitidas**: `char` **puede participar directamente en operaciones enteras**; p. ej. `z = a + 25` y `u = ch - 32` (donde `ch` es la variable de iteración de `for ch <- s`) son **formas normales**, que suman o restan el valor del punto de código. Las operaciones se hacen siempre con **anchura i32** y el resultado se trata como un valor entero ordinario; por eso, las operaciones `+ - * /` de `char` **no devuelven por defecto `option<int>`** como la familia entera y no disparan el error de compilación «desbordamiento entero no gestionado». Lo único que hay que vigilar es el rango: el punto de código máximo es `0x10FFFF`; en contextos fuera de la semántica de `i32`/punto de código (como desplazamientos o acumuladores) conviene convertir explícitamente a `i32`/`i64` antes de operar, para que la intención sea clara.
- **Relación con `str`**: `s[i]` devuelve un `char`; un `char` se convierte implícitamente a `str` (codificación UTF-8); véase [Cadenas — conversión implícita](str.md).

> **«Tipo subyacente» y «conversión en la llamada» son dos cosas distintas.** `char` es en sí mismo un `i32`. La **firma en código fuente** del intrínseco `str_from_cp(cp char) -> ?str` tiene un parámetro `char`; solo al **llamar al runtime externo** el compilador **extiende con ceros** (`zext i32 → i64`) ese parámetro para coincidir con los argumentos IR de `@str_from_cp` — se trata meramente de una conversión temporal en la preparación de argumentos, **no cambia el tipo de `char`**. Es como en C `char c='a'; f((long)c);`: `c` sigue siendo de 1 byte y se promueve solo momentáneamente en la llamada.

Tipos contenedor

- obj ; objeto
- map ; mapa
- arr ; arreglo de longitud fija
- vec ; arreglo de longitud variable
- slice ; rebanada (vista), no tiene estructura de datos propia, debe apoyarse en arr/vec/str/txt

- \* ; puntero, limitado a declaraciones FFI `#{c}` y a la biblioteca estándar
- usize ; solo para FFI
- any ; cualquier tipo, limitado a la biblioteca estándar

Tipos avanzados

- bigint
- err

## Alias de tipo y tipos unión

Un alias de tipo crea un nombre nuevo para un tipo existente. Se usa la sintaxis con signo igual `name = type` y admite tanto alias de un solo tipo como uniones de varios tipos.

### Sintaxis

```no
; tipo unión: varios tipos separados por |
int = i8 | i16 | i32 | i64 | i128 | u8 | u16 | u32 | u64 | u128
float = f32 | f64
num = int | float
string = str | txt

; alias de un solo tipo
bytes = []byte
buf = [16]u8
```

> `int`, `float` y `num` están definidos en `std/number`, y `string` en `std/str`: ninguno es una palabra clave integrada del compilador, sino tipos unión escritos en la biblioteca estándar con la misma sintaxis de alias.

### Referencias encadenadas de tipos unión

Un tipo unión puede referenciar a otros tipos unión, formando una jerarquía:

```no
int = i8 | i16 | i32 | i64 | i128 | u8 | u16 | u32 | u64 | u128
float = f32 | f64
num = int | float; num es la unión de int y float
string = str | txt; string une directamente dos tipos de cadena concretos
```

### Uso en funciones

Los tipos unión se pueden usar en parámetros y valores de retorno de funciones; el compilador realiza automáticamente la monomorfización, generando una versión independiente de la función para cada tipo miembro:

```no
; tipo de parámetro num (unión)
max = (a ..num) (r num) {
    r = a[0]
    n = len(a)
    i <- [1..n): {
        a[i] > r -> r = a[i]
    }
}

; método definido sobre el tipo unión
num.sign = () (r num) {
    {
        . > 0 -> r = 1

        . < 0 -> r = -1

        -> r = 0
    }
}
```

`string` (= `str | txt`) también se puede usar en parámetros y valores de retorno:

```no
; se pueden pasar argumentos str y txt, cada uno monomorfiza su propia función concreta
take = (s string) (n i64) {
    n = len(s)
}

a str = 'hello'
b txt = 'hi'
n1 = take(a); take__str
n2 = take(b); take__txt
```

> Limitaciones actuales del cuerpo de funciones con tipos unión (igual para `num` y `string`): todavía no se admite llamar a métodos miembro (como `s.len()`) sobre un parámetro union dentro del cuerpo; hay que usar intrínsecos globales (`len(s)`, número de bytes) o dividir en funciones de tipos concretos. Tampoco se admiten declaraciones de variables de tipos unión (`s string = 'x'`). Además, evite definir alias de método con el mismo nombre que un método miembro (p. ej. `string.len`): se apropiaría de la llamada `s.len()` y en el cuerpo del método se redirigiría recursivamente a sí mismo.

### Reglas de detección

La sintaxis con signo igual se reconoce como alias de tipo (y no como asignación a variable) en los casos siguientes:

- `name = type | type | ...`: tipo unión (contiene `|`);
- `name = []type`: tipo rebanada;
- `name = [N]type`: tipo arreglo;
- `name = ?type`: tipo opcional;
- `name = known-type`: alias de un solo tipo, donde `known-type` es un nombre de tipo integrado (como `i64`, `f64`, `bool`, `str`, etc.) o el nombre de un alias de tipo definido anteriormente.

## Declaración de variables

```no
#{fmt-skip}
; las variables no tienen palabra clave
; i64、f64、byte、bool、byte、str pueden omitir la anotación de tipo
i = 1

; f64 lleva un . en medio
f = 1.0

; byte
b = 0x00

; i8: si el nombre de la variable coincide con el tipo, se puede omitir la anotación
i8 = 3

; una anotación explícita igual al tipo inferido es redundante (no vet avisa tcpoxtfd; no fmt --fix=redundant la elimina automáticamente)
x str = ''
x = ''; equivalente, la anotación es redundante

; valor cero por defecto
; las variables no necesitan declaración previa
u16 u16

; str entre comillas simples
name = 'nolang'

; bool: true/false todo en minúsculas
flag = true
flag = false

; abreviaturas bool: !! equivale a true, ! equivale a false (literales independientes)
flag = true; true
flag = false; false

; asignación a variables
; no se permite el mismo nombre; si coincide se considera modificación de la variable
name = 'hello'
name = 'world'

; concatenación de cadenas
greeting = 'hello, ' - name

; cadena cruda (entre comillas invertidas, multilínea, sin escapes)
sql = 'SELECT id,name
FROM user
WHERE id > 100'

; anotación de tipo explícita
a u64 = 10

; inferencia de tipo de literales hexadecimales
; - literales enteros decimales (como 771) se infieren como i64 (tipo entero por defecto)
; - literales hexadecimales (como 0x0303) se infieren como byte (u8)
; - si el valor hex supera el rango de byte (> 255), hay que añadir una anotación de tipo explícita:
PORT i64 = 0x0303; correcto: i64 explícito, valor = 771

; PORT = 0x0303          ; error: se infiere byte, ¡el valor se trunca!
; PORT = 771             ; correcto: el decimal por defecto es i64
; - en literales hexadecimales se recomiendan letras minúsculas (0x00ff, no 0x00FF)
; - recomendación: use decimal para constantes enteras generales; use hex con anotación i64 explícita para constantes de protocolo/máscaras de bits

; carácter (comillas dobles = char/rune, un solo carácter)
c = "中"

; tipo byte
b = 0x00

; arr arreglo de longitud fija
arr [3] = [1, 2, 3]

; vec arreglo dinámico (rebanada)
vec = [4, 5, 6]

; tipo explícito (rebanada)
typed []u8 = [1, 2, 3]

; arreglo
typed [3]u16 = [1, 2, 3]

; inferencia automática de longitud (i64)
a[nil] = [1, 2, 3]
```
```

## Cadenas crudas (Raw String)

Se declara una cadena cruda con un par de comillas invertidas `` ` `` ` ` `` ` ``; su tipo es `str`.

### Sintaxis

```
sql = `
SELECT id,name
FROM user
WHERE id > 100
`
```

### Restricciones de formato obligatorias

1. La comilla invertida de **apertura** `` ` `` debe ir **seguida inmediatamente de un salto de línea en el código fuente** (LF; también se acepta CRLF en archivos de Windows);
2. la comilla invertida de **cierre** `` ` `` debe **ocupar una línea por sí sola**, y en esa línea solo se permiten espacios en blanco + la comilla de cierre;
3. **las líneas de las comillas inicial y final no cuentan como contenido** de la cadena, solo sirven como delimitadores;
4. **reglas de escape**: todos los `\`, `\n`, `\t`, `\'`, `\"` del interior se conservan **literalmente**, sin aplicar ninguna interpretación de escape;
5. **los saltos de línea y los espacios de indentación del código fuente se conservan íntegramente** en los bytes de la cadena; excepción: el CR (`\r`) se **descarta siempre**, de modo que un archivo CRLF y uno LF producen un valor byte a byte idéntico;
6. **no se puede incrustar directamente una comilla invertida**: si hace falta incluir `` ` ``, hay que concatenar una cadena normal entre comillas simples.

### Ejemplo

```
; el contenido es "SELECT id,name\nFROM user\nWHERE id > 100\n"
; los caracteres de escape \n \t se conservan tal cual, no se interpretan
raw = `
line1\nline2
\ttabbed
`
```

## Literales regex (Regex Literal)

Nolang admite la sintaxis de literales regex estilo JavaScript `/pattern/flags` para crear instancias `regexp` ya compiladas.

### Sintaxis

```no
; literal regex básico
re = /\d+/

; con banderas (flags)
re = /hello/gi

; clases de caracteres, anclas, cuantificadores
re = /[a-z]+/
re = /^hello.*world$/

; barra escapada
re = /a\/b/
```

### Banderas

| Bandera | Significado |
| --- | --- |
| `g` | coincidencia global |
| `i` | sin distinción de mayúsculas/minúsculas |
| `m` | modo multilínea |
| `s` | `.` coincide también con saltos de línea |

Las banderas son opcionales, van después de la `/` de cierre y se componen de letras ASCII.

### Análisis léxico sensible al contexto

`/` es a la vez operador de división y apertura de literal regex; Nolang usa el mismo análisis léxico **sensible al contexto** que JavaScript para distinguirlos:

- **posición de inicio de expresión** (comienzo de sentencia, o después de `=` / `(` / `[` / `{` / `,` / `:` / `;`, etc.) → `/` abre un literal regex;
- **posición de producción de valor** (después de un identificador, un literal, `)` / `]` / `}`, etc.) → `/` es operador de división;
- `//` siempre es comentario de línea (máxima prioridad).

```no
; literal regex (después de = hay posición de inicio de expresión)
re = /\d+/
result = match-text(/[a-z]+/, text)

; operador de división (después de un identificador hay posición de producción de valor)
ratio = 100 / 4
x = a / b
```

### Desazucarado

Los literales regex se desazucaran en la fase de generación de código a una llamada a la función de biblioteca estándar `regexp-compile`:

```no
; código fuente
re = /\d+/

; tras desazucarar, equivale a
re = regexp-compile('\\d+')
```

`regexp-compile` está definido en `std/regexp.no`; crea el struct `regexp` y llama a `.compile()` para compilar el patrón.

### Ejemplos de uso

```no
; crear una regex y comprobar coincidencia
re = /\d+/
matched = re.matches('hello 123 world')
print(matched); true

; buscar coincidencias
re = /[a-z]+/g
result = re.find('hello 42 world')
print(result); "hello"

; como parámetro de función
result = match-text(/\d+/, text)
```

> **Atención:** el patrón vacío `//` entra en conflicto con el comentario de línea (igual que en JavaScript). Si necesita una coincidencia vacía, use `/(?:)/`.

## Reglas de nomenclatura

Los nombres de variables, funciones, structs, etc. pueden empezar por guion bajo; a continuación pueden contener guiones medios, letras y dígitos; no pueden empezar por dígito, ni terminar en guion medio, ni tener varios guiones medios consecutivos.

**Reglas de mayúsculas/minúsculas (obligatorias):**

- **constantes globales, variables globales**: **deben** empezar por letra mayúscula (p. ej. `NOLANG`, `MAX-SIZE`, `HEX-CHARS`). Las variables globales privadas empiezan por guion bajo y después deben seguir en mayúscula (p. ej. `_NOLANG`, `_PRIVATE-CONST`).
- **variables locales, parámetros de función**: usan letras minúsculas (p. ej. `hex-chars`, `data-len`). **No** use el prefijo `_`: las variables locales ya son privadas dentro de su ámbito y no necesitan marca de visibilidad. El prefijo `_` se reserva para variables globales privadas y declaraciones FFI privadas.
- **nombres de funciones y structs**: usan letras minúsculas (p. ej. `sha1-block`, `db-mysql`).

**Convenciones de nombres de funciones (encarecidamente recomendadas):**

- **No añada el prefijo del nombre del módulo al nombre de la función.** Las funciones dentro de un módulo necesitan solo nombres breves e intuitivos; el prefijo del módulo lo aporta automáticamente el `ShortName.` en las llamadas entre módulos. Por ejemplo, en `tail.no` defina la función de entrada `tail` y no `tail-run`, y la auxiliar `atoi` y no `tail-atoi`. Así el código es más conciso y la llamada `tail.tail()` resulta más intuitiva.
- La **función de entrada** debería usar directamente el nombre del módulo (p. ej. `ping.no` → `ping`, `cat.no` → `cat`). La importación entre módulos adopta la forma `# /src/tail.tail`.
- **Evite las palabras clave**: `awy` (palabra clave de grupo de corrutinas), `match` (palabra clave de correspondencia condicional), etc. no pueden usarse como nombres de función. `run` se ha eliminado y puede usarse como identificador normal.
- **`main` es un nombre reservado y no puede usarse como nombre de función**: el punto de entrada de un programa Nolang se **sintetiza automáticamente a partir de las sentencias de nivel superior** (véase `hir2mir.synthesizeMainForTopLevel`), así que no hace falta —y **no se permite**— definir `main` explícitamente. En cuanto aparece `main = (...) { }` —incluidos el método `recv.main` y un `main` anidado dentro de otra función—, el compilador informa directamente del error (`'main' is a reserved name and cannot be used as a function name`). Para servir de entrada, escriba las sentencias directamente a nivel superior.

> **Las variables globales deben empezar por mayúscula**: es una regla obligatoria, no una convención. Una variable de nivel superior que empiece por minúscula será tratada por el compilador como variable local, lo que puede provocar errores de referencia no definida, entre otros.

```no
; ✅ Correcto: los datos globales usan mayúsculas
NOLANG = 'nolang'
MAX-SIZE = 1024
HEX-CHARS = '0123456789abcdef'

; ✅ Variable global privada: empieza por guion bajo y después sigue en mayúscula
_NOLANG = 'nolang'
_PRIVATE-CONST = 42
```

```no
#{fmt-skip}
; ❌ Error: las variables globales no pueden empezar por minúscula
; x1 = 10
; x = 10
; foo-bar = 42
; hello-world = 'Hello World'
```
```

```no
#{fmt-skip}
; ✅ Las variables locales (dentro de funciones) usan minúsculas, sin prefijo _
; fn-example = () {
;     x1 = 10
;     x = 10
;     foo-bar = 42
;     hello-world = 'Hello World'
;     ; ❌ Error: las variables locales no necesitan prefijo _
;     ; _x = 10
; }
```
```

### Evitar variables globales en los módulos

**Encarecidamente recomendable: salvo que sea imprescindible, no use variables globales en un módulo (archivo `.no`).** Las variables globales plantean estos problemas:

- **Riesgo de bug del compilador**: el compilador de Nolang tiene limitaciones conocidas en el tratamiento de direcciones de memoria entre funciones para variables globales de tipo struct; funciones distintas pueden ver direcciones diferentes y producir estados incoherentes.
- **Seguridad concurrente**: el estado global mutable es difícil de rastrear en escenarios de fork o asíncronos y puede provocar condiciones de carrera.
- **Testabilidad**: el estado global crea dependencias implícitas en las funciones, dificultando el aislamiento de pruebas.
- **Legibilidad**: las variables globales oscurecen el flujo de datos; el lector debe recorrer todo el módulo para entender el comportamiento de una función.

**Práctica recomendada:**

1. **Prefiera variables locales**: mantenga el estado en variables locales dentro de la función y páselo mediante parámetros y valores de retorno.
2. **Use structs para encapsular el estado**: organice el estado relacionado en un struct y manipúlelo mediante métodos (el receptor del método es una variable local, con dirección coherente).
3. **Use variables globales solo cuando sea necesario**: p. ej. constantes de nivel de módulo (inmutables), recursos singleton (como un búfer de registro global), etc.
4. **Las variables globales deben ir en mayúsculas**: es obligatorio (véase arriba); una variable de nivel superior en minúscula será tratada como local por el compilador.

```no
#{fmt-skip}
; ❌ Evitar: usar variables globales mutables en un módulo
; g-conn = tls.conn {}
; g-buf = ' '
;
; fn-a = () {
;     g-conn.send(g-buf)   ; la dirección de las variables globales puede diferir entre funciones
; }
```
```

```no
; ✅ Recomendado: usar variables locales, con el estado pasado mediante parámetros/valores de retorno
fn-a = () {
    conn = tls.conn {}; variable local, dirección coherente
    buf = ' '
    conn.send(buf)
}

; ✅ Recomendado: encapsular el flujo completo en una sola función, evitando pasar estado entre funciones
serve-once = (listen-fd fd, body str) (ok bool) {
    ok = false
    client-fd = net.net-accept(listen-fd)
    conn = tls.server-init(client-fd); variable local
    conn: {
        ok -> {
            c = it
            c.handshake()
            c.send(body)
            c.close()
            ok = true
        }

        -> fs.close(client-fd)
    }
}
```

> **Caso práctico**: la función `https-serve-once` de `std/net/tls.no` encapsula el ciclo completo petición-respuesta HTTPS (accept + handshake + recv + send + close) en una sola función, manteniendo todo el estado TLS en variables locales y esquivando con éxito el bug del compilador de direcciones incoherentes entre funciones para variables globales.

## Normas de documentación de API

Los comentarios de documentación de una función deben incluir los nombres y tipos de todos los parámetros, y los nombres y tipos de los parámetros de resultado.

**Reglas:**

- los comentarios de documentación sobre la definición de una función deben enumerar el nombre y el tipo de cada parámetro, así como el nombre y el tipo de los parámetros de retorno;
- el resumen de API en lo alto del módulo también debe usar firmas completas (con nombres de parámetro, tipos, nombres de retorno y tipos), sin formas abreviadas.

```no
#{fmt-skip}
; ❌ Error: faltan tipos y falta el nombre del parámetro de retorno
; sha1(data) (hash)
; sha1-block(s, h0..h4)
```
```

```no
; ✅ Correcto: incluye nombres de parámetro, tipos, nombre y tipo del retorno
; sha1(data []byte) (hash [20]byte) — hash completo
; sha1-block(s []u32, h0 u32, h1 u32, h2 u32, h3 u32, h4 u32) — procesa un solo bloque

; los comentarios de documentación sobre la definición también deben seguir la misma norma:
; sha1: calcula el hash SHA-1
; data []byte: arreglo de bytes de entrada
; retorna hash [20]byte: valor hash de 20 bytes
sha1 = (data []byte) (hash [20]byte) {
    ...
}
```

### Comentarios de documentación traducibles (enlaces Markdown)

Un comentario de documentación puede terminar con un **enlace Markdown en su propia línea**, que apunta toda la anotación a una entrada del directorio de traducciones:

```no
; get: obtiene el argumento i-ésimo
; delega en el intrínseco os.arg(i) (lee argv[i] de main); fuera de límites devuelve nil
; [get](#args.get)
get = (i i64) (val ?str) {
    val = os.arg(i)
}
```

**Reglas:**

- el enlace debe ser la **última línea** del grupo de comentarios y ocupar una línea por sí sola; todas las líneas anteriores forman un todo que se traduce como un único mensaje;
- el destino debe ser un ancla que empiece por `#` (una entrada del mismo directorio). Enlaces externos como `[explicación](https://example.com)` son enlaces normales para personas, **no** se tratan como marcadores de traducción;
- el ancla es la clave (`args.get`). La especifica el autor y la ve el traductor en el directorio; no se deriva del texto, así que reformular la redacción no invalida traducciones existentes. El ancla admite letras, dígitos, `.`, `_` y `-`, es global y se recomienda prefijarla con el nombre del módulo;
- el prefijo usa el **nombre del módulo aplanado**, no el nombre del directorio. El compilador aplana los archivos std a su nombre de archivo: para `std/collection/linked-hash-map.no` el módulo es `linked-hash-map`, y `use std/collection` no añade el prefijo `collection.` a los símbolos. Por tanto, el ancla de `linked-hash-map.clear` es `linked-hash-map.linked-hash-map.clear` (nombre del módulo + nombre tal cual), **no** `collection.linked-hash-map.linked-hash-map.clear`. Los segmentos de directorio `collection`／`net`／`crypto`／`archive`／`encoding`／`database` no pertenecen a ningún símbolo y no deben aparecer en las anclas;
- el texto entre corchetes (`[get]`) es solo una pista para quien lee el código fuente; al mostrarlo, la línea completa se elimina; también es válido `[](#args.get)`;
- solo los comentarios `;` de una sola línea pueden llevar enlace (no cuentan `;;` ni `;;block`).

Las traducciones se guardan en `locales/<idioma>-doc.noml`, con el ancla como clave. Si falta una traducción, se recurre al texto original del comentario, nunca queda vacío. El nombre de archivo sigue las reglas de nomenclatura de `.no` (letras minúsculas, dígitos, un solo guion medio); la etiqueta ya tiene ese formato: `--lang=zh-tw` lee `zh-tw-doc.noml`.

En la biblioteca estándar, cada función y método con comentarios ya lleva un ancla (unas 2000), así que en cuanto se añade una traducción, el LSP puede usarla de inmediato. `make i18n` solo escribe en el directorio las entradas **ya traducidas**, sin insertar el texto original como si fuera una traducción (de lo contrario, el directorio en inglés volvería a contener chino). `go run ./i18n/cmd/genen -doc-missing` lista las anclas aún sin traducir.

**Consejo LSP:** al pasar el ratón por el nombre de una función se muestra la descripción completa traducida; al pasar por la línea del enlace se muestra el contenido al que apunta el ancla (también en el idioma actual), para comprobar la traducción in situ.

## Priorizar la biblioteca estándar

La biblioteca estándar de Nolang ofrece abundantes funciones de uso común: operaciones con cadenas, conversiones de bytes, cálculo de hashes, comunicaciones de red, etc.

**Regla: si la biblioteca estándar ya tiene la funcionalidad correspondiente, no se recomienda reimplementarla.** Los desarrolladores deben consultar atentamente la documentación de la biblioteca estándar (`docs/docs/std/overview.md`) para evitar reinventar la rueda.

```no
#{fmt-skip}
; ❌ Error: implementar por cuenta propia la conversión str → []byte
str-to-bytes = (s str) (out []byte) {
    n = s.len-bytes()
    i = 0
    (i < n) {
        out[i] = s[i]
        i = i + 1
    }
}
```
```

```no
; ✅ Correcto: usar el método de biblioteca estándar str.to-bytes()
data []byte = s.to-bytes()
```

Sustituciones habituales de la biblioteca estándar:

- `str.to-bytes()` — cadena a arreglo de bytes (sustituye a un `str-to-bytes` escrito a mano)
- `[]byte.to-str()` — arreglo de bytes a cadena (sustituye a un `bytes-to-str` escrito a mano)
- `[n]t.to-vec()` — arreglo de longitud fija a rebanada (`[20]byte` → `[]byte`)
- `[]byte.to-hex()` / `[]byte.to-hex-lower()` — arreglo de bytes a cadena hexadecimal
- `str.to-i64()` / `str.to-f64()` — analizar una cadena como número
- `int.to-str()` / `float.to-str()` — número a cadena
- `std/crypto/sha1`、`std/crypto/sha256`、`std/crypto/sha512` — cálculo de hashes

### Archivos de configuración TOML (`toml`)

La biblioteca estándar `toml` ofrece análisis y serialización de archivos de configuración TOML 1.0. El analizador admite comentarios, claves bare/quoted/dotted, `[table]`, `[[array-of-tables]]`, cadenas basic/literal/multiline, enteros (decimal/hexadecimal/octal/binario), coma flotante, booleanos, fechas y horas, arreglos y tablas inline. Los valores se conservan como literales TOML crudos, de modo que no se pierde información como arreglos o fechas.

```no
cfg = toml.parse('title = "Nolang"\n[server]\nport = 8080\n[[server.backends]]\nname = "local"')

doc = toml.new()
doc.put('name', '"nolang"')
doc.put('server.port', '8080')
doc.put('enabled', 'true')
text = toml.stringify(doc)
```

API habitual:

- `toml.new()` — crea un `toml` vacío
- `toml.parse(text)` — analiza TOML, devuelve `?toml`
- `doc.put(key, raw-value)` — añade o actualiza un valor TOML crudo; rechaza valores inválidos; las claves duplicadas las informa el analizador
- `toml.get(doc, key)` — obtiene el valor crudo (`?str`)
- `toml.get-str(doc, key)`、`toml.get-i64(doc, key)`、`toml.get-f64(doc, key)`、`toml.get-bool(doc, key)` — obtiene escalares ya convertidos
- `toml.stringify(doc)` — emite TOML; los array-of-tables se escriben con cabeceras `[[...]]`

El modelo de documento actual limita cada documento a 4 claves, 2 tablas normales y 2 array-of-tables como máximo; una clave mide como máximo 32 bytes y un valor 128 bytes; si se supera, hay que dividir el archivo de configuración o dejarlo al llamador.

### Documentos YAML (`yaml`)

La biblioteca estándar `yaml` analiza y genera documentos YAML según el esquema core de YAML 1.2: admite asignaciones de bloque, secuencias de bloque, colecciones de flujo, tres tipos de escalares entre comillas, escalares de bloque `|` / `>`, comentarios, multidocumento, anclas y alias, claves de fusión `<<`, claves explícitas e inferencia de tipos (null / bool / int / float / `.inf` / `.nan`).

```no
doc ?yaml = yaml.parse('name: Alice\nserver:\n  port: 8080\ntags:\n  - a\n  - b\n')
doc: {
    nil -> print('nil')

    err -> print(it.msg())

    -> {
        doc = it
        name = doc.find-str('name'); ?str
        port = doc.find-i64('server.port'); ?i64
        name: {
            ok -> print(it)

            -> print('')
        }
        print(doc.to-json())
    }
}
```

API habitual:

- `yaml.parse(text)` / `yaml.parse-all(text)` — analiza, devuelve `?yaml`
- `yaml.valid-of(text)` / `yaml.error-of(text)` — solo valida
- `it.find(path)` / `it.get(key)` — obtiene un nodo hijo, devuelve `?yaml`
- `it.find-str(path)`、`find-i64`、`find-f64`、`find-bool` — acceso conveniencia, devuelve `?valor`
- `it.to-json()` / `it.to-yaml()` — serialización

> Atención: los nodos hijo se devuelven como `?yaml` y las funciones auxiliares de acceso como `?valor`; desempaquételos con match (`child = it.find(path)`, cuyo brazo `ok` enlaza `it` al nodo hijo y el brazo por defecto trata el valor vacío, cada brazo en su propia línea).

## Nomenclatura de archivos

Los nombres de archivo `.no` (incluidos los nombres de carpeta) usan siempre el guion medio `-` para unir palabras, **nunca el guion bajo `_`**. Esto coincide con el estilo de nomenclatura de los identificadores Nolang (variables, funciones, structs, etc.).

El compilador **comprueba obligatoriamente** esta norma en los comandos `run` / `build` / `test` / `vet` / `fmt` / `ast`: cualquier segmento de la ruta (nombre de carpeta o cuerpo del nombre de archivo, sin contar la extensión `.no`) que la incumpla produce **error directo**:

- caracteres permitidos: letras minúsculas `a-z`, dígitos `0-9` y guion medio `-`;
- caracteres prohibidos: guion bajo `_`, mayúsculas y cualquier otro símbolo especial;
- el guion medio no puede aparecer al principio ni al final de un segmento, ni repetirse (`--`).

Por ejemplo, `tmp/zz_stridx/r_index.no` se rechaza por los guiones bajos en `zz_stridx` y `r_index`; cambiándolo a `tmp/zz-stridx/r-index.no` cumple la norma. En rutas absolutas del SO (p. ej. `/Users/me/proj/a.no`) solo se valida la parte que cae bajo el directorio de trabajo actual; los nombres de directorios anfitriones (como `Users`) no generan falsos positivos.

```shell
✅ Recomendado:
utils/
├── string-helper.no
├── hash-table.no
└── http-client.no
```

```shell
❌ Evitar:
utils/
├── string_helper.no
├── hash_table.no
└── http_client.no
```

## Definición de funciones

Las funciones pueden devolver resultados mediante **parámetros de resultado con nombre** (named result parameters), que son en esencia  
**azúcar sintáctico de parámetros de salida (out-parameter)**; `return` (y su abreviatura `***`, desaconsejada por ahora) solo sirve para terminar anticipadamente y no puede llevar resultados.

La forma de definición de funciones en Nolang es `name = (in-params) (out-params) { body }`: el segundo  
grupo de paréntesis `(out-params)` declara los parámetros de resultado con nombre (azúcar sintáctico). En el cuerpo de la función se les asigna  
(al igual que los parámetros de entrada normales, todos son de tipo referencia); **la variable que recibe realmente el resultado debe definirse en el punto de llamada**:  
el nombre del parámetro de resultado en la definición es solo un marcador de posición; el LHS del llamador (o el argumento final) decide en qué  
variable acaba el resultado:

```no
parse-line = (s str, max-fields ?i64) (fields []str) {
    ...; en el cuerpo se asigna fields
}

; el punto de llamada define la variable receptora (enlace LHS, tipo inferido de la firma)
fields = parse-line(line, nil)

; varios resultados se enlazan en orden
a, b = swap(x, y)

; o en forma de argumento final: res se pasa como argumento de salida adicional
add1(5, 3, res)
```

Las funciones de Nolang tienen estas características:

- **Los parámetros de resultado con nombre son azúcar sintáctico**: por debajo se sigue usando un parámetro de entrada (out-parameter), sin crear objetos de retorno nuevos, así que internamente es seguro.
- **Los parámetros de entrada son de solo lectura**: los escalares se pasan por valor y los tipos compuestos como referencia de solo lectura; está prohibido escribir en los parámetros de entrada ni en sus subcampos dentro del cuerpo.
- **Los parámetros de salida se pueden escribir**: el llamador puede enlazar una variable existente a la ranura de salida y la función modifica directamente esa memoria; también puede no enlazarla y que la función genere un valor nuevo para entregarlo.
- Las variables dentro de la función se destruyen automáticamente al salir de ella.
- El punto de llamada debe proporcionar una variable receptora (LHS o argumento final); decir «la función no devuelve nada» es impreciso —  
  lo exacto es: el resultado se transmite mediante parámetros de resultado con nombre (azúcar de out-parameter) y lo enlaza el llamador.
- **Regla de alias**: se permite que una referencia de entrada de solo lectura y una ranura de salida apunten al mismo objeto. Siempre que las escrituras ocurran solo en la zona de salida y las lecturas solo en la entrada, es válido; el compilador no hace comprobación estática de alias.

### Enlazar la ranura de salida directamente a una variable existente (evitar intermediarias)

La ranura de salida del lado llamador se puede **enlazar directamente a una variable ya existente**; la función reescribe su memoria in situ (véase «Los parámetros de salida se pueden escribir» más arriba). Por tanto, cuando un resultado solo sirve para seguir actualizando un estado existente, no hace falta recogerlo primero en una variable intermedia de un solo uso y luego volcarlo:

```no
; ✅ Recomendado: el resto se escribe directamente en r (r es además el receptor del método)
qsq, r = r.div-mod(modulus)
```

```no
; ❌ Evitar: msq solo se usa una vez, es puro tránsito
qsq, msq = r.div-mod(modulus)
r = msq
```

- **El receptor puede ser a la vez destino de salida**: en `r.div-mod(modulus)`, el receptor `r` es una entrada de solo lectura (`self` implícito) y, a la vez, `r` se enlaza a la segunda ranura de salida `r bigint`; según la ⟨regla de alias⟩, entrada solo leída y salida solo escrita es válido.
  Medido (`no build` / `no vet` / `no fmt`): `q, a = a.div-mod(bigint.from-i64(7))` tras `a = bigint.from-i64(100)` da `q = 14` y `a = 2`, exactamente igual que «recoger primero `msq` y luego `r = msq`»;
  también vale aunque `r` sea un parámetro de resultado de una función externa (`mod-pow` en `src/std/bigint.no` tiene precisamente esa forma).
- **Descarte de salidas no usadas con `_`**: si el cociente no se usa en absoluto, `_, r = r.div-mod(modulus)` ni siquiera introduce una variable nueva y es la forma más concisa (véase más adelante ⟨Asignación múltiple⟩ y el marcador `_`).
- **Vale también con una sola salida**: `r = r._mul(r)`, `s = s.trim()` y `out = out.replace-str(...)` son actualizaciones in situ ya presentes en la biblioteca estándar.
- **El código válido existente no debe reescribirse por esto**: es una recomendación para código nuevo, no una orden de refactorización; siga la ⟨regla de oro: no modificar código válido⟩ y no toque programas ya correctos por estilo.

### Valores por defecto de parámetros

**No se admiten.** `name type = expr` en la lista de parámetros (p. ej. `foo = (a i64, b ?i64 = 10)`) es un **error de sintaxis** (`E_DEFAULT_PARAM`): el compilador no analiza expresiones por defecto ni rellena valores por defecto en ningún punto de llamada. Esto elimina la ambigüedad de «una declaración, varias aridades válidas» y evita la explosión de monomorfizaciones que supondría reducir la expresión por defecto en cada llamada.

Cuando se necesite semántica opcional, declare el parámetro como tipo option `?T` y que el llamador pase **explícitamente** `nil` / `ok(...)`; el cuerpo de la función lo gestiona con `match`:

```no
parse-line = (s str, max-fields ?i64) (fields []str) {
    limit = 1024
    max-fields: {
        ok(v) -> limit = v

        nil ->

        err ->
    }
    ...; limit es el límite superior
}

; el llamador debe pasar los argumentos explícitamente
fields = csv.parse-line(line, nil); recurre a 1024
fields = csv.parse-line(line, ok(256)); límite 256
```

```no
add = (a i64, b i64) (result i64) {
    result = a + b; devuelve el resultado mediante el parámetro
    ...; sentencia de marcador de posición: no hace nada, en build se descarta por completo (véase ⟨Sentencia de marcador de posición⟩)
}

; parámetros variádicos
add3 = (a ..i64) {

}

; llamada a función
sum = add(1, 2); sum == 3

; función anónima y for? ¿con argumentos?
(a i64) {
    print(a)
}(10)

; llamada a función
add(a, b)

; también puede haber varios valores de retorno
a, b = swap(5, 3)
```

## Control de flujo

> **Sintaxis obsoleta (se eliminará tras la versión n)**: `for { }` / `for cond { }` / `for init, cond, update { }` / `for i in [..) { }` / `match x { }` / `if/elif/else { }` todavía se analizan, pero emiten una advertencia de obsolescencia. Use la «**nueva sintaxis**» que se indica abajo.

### Comparación entre forma antigua y nueva

| Forma antigua / sufija                    | Forma prefija (prefijo, por defecto en format) |
| ----------------------------------------- | ---------------------------------------------- |
| `{ } (true)` bucle infinito               | `!! { }` o `true { }`                          |
| `{ } (cond)` bucle condicional            | `(cond) { }` (`()` vacío se considera false, no ejecuta) |
| `{ } ()` no ejecuta                       | `! { }` o `false { }` o `() { }`               |
| `{ } * n` conteo constante                | `n * { }` (si N ≤ 0 el cuerpo no se ejecuta)   |
| `for i=0, i<n, i++ { }` contador          | `n * { }` (conteo constante) o `i <- [0..n): { }` (variable) |
| `for i <- [a..b] { }` rango               | `i <- [a..b]: { }`                             |
| `for i in [a..b) { }` rango               | `i <- [a..b): { }`                             |
| `match x { ... }` correspondencia         | `x: { ... }`                                   |
| `if/elif/else { }` ramas                  | `{ cond -> body }` (cortocircuito, debe envolverse en `{}`) |
| `continue`                                | `*` (abreviatura desaconsejada)                |
| `break`                                   | `**` (abreviatura desaconsejada)               |
| `return`                                  | `***` (abreviatura desaconsejada)              |

### Loop / While / for-in

> **Dos formas equivalentes**: un mismo bucle admite la «forma prefija `(cond) { }`» y la «forma sufija `{ } (cond)`»;  
> la semántica es idéntica, solo cambia el orden de condición y cuerpo. `no fmt` emite por defecto la **forma prefija**;  
> puede volver a la sufija con `no fmt -loop-style=suffix` (ambas formas garantizan formato idempotente).

```no
#{fmt-skip}
; === forma prefija (prefijo, condición antes del cuerpo) ===

; bucle infinito (condición siempre true) — las dos formas siguientes son equivalentes
!! {
    ...
}
!! {
    ...
}

; no ejecutar (condición siempre false) — las tres formas siguientes son equivalentes
! {
    ...
}
! {
    ...
}
! {
    ...
}

; bucle condicional (comprueba cond y ejecuta el cuerpo si es verdadera)
(x == 1) {
    do-something()
}

; número limitado de ejecuciones (conteo prefijo)
10 * {
    do-something()
}

; si N ≤ 0 el cuerpo no se ejecuta (0 o negativo se omiten)
0 * {
    print('no se ejecutará')
}
-3 * {
    print('tampoco se ejecutará')
}

; se puede añadir una etiqueta (label antes de la condición/conteo)
#1 (x == 1) {
    do-something()
}

; === forma sufija (sufijo, condición después del cuerpo, forma antigua) ===
!! {
    ...
}

; no ejecutar (paréntesis vacío = false)
! {
    ...
}

; bucle condicional
(x == 1) {
    do-something()
}

; número limitado de ejecuciones
10 * {
}

; si N ≤ 0 el cuerpo no se ejecuta (0 o negativo se omiten)
0 * {
    print('no se ejecutará')
}

-3 * {
    print('tampoco se ejecutará')
}

; sintaxis de intervalo (admite tipos numéricos como i64/u8, arr, vec, str)
; 16 combinaciones en total: '[' incluye el extremo izquierdo, '(' lo excluye; ']' incluye el derecho, ')' lo excluye.
; acotado (4 tipos): ambos extremos tienen valor
i <- [a..b]: {
}
i <- (a..b]: {
}
i <- [a..b): {
}
i <- (a..b): {
}

; sin límite superior (4 tipos): se omite el extremo derecho, itera hasta el valor máximo del tipo
i <- [a..]: {
}
i <- [a..): {
}
i <- (a..]: {
}
i <- (a..): {
}

; sin límite inferior (4 tipos): se omite el extremo izquierdo, itera desde el valor mínimo del tipo
i <- [..b]: {
}
i <- [..b): {
}
i <- (..b]: {
}
i <- (..b): {
}

; totalmente ilimitado (4 tipos): se omiten ambos extremos, según mínimo/máximo del tipo
i <- [..]: {
}
i <- [..): {
}
i <- (..]: {
}
i <- (..): {
}
i <- [5..0]: {
}
i <- abc: {
}

; detección de dirección en tiempo de ejecución: cuando start > end, la iteración decrece automáticamente (paso -1).
; las cuatro combinaciones de paréntesis admiten decrecimiento:
;   [5..1]  → 5 4 3 2 1   izquierda cerrada, derecha cerrada, decreciente
;   (5..1]  → 4 3 2 1     izquierda abierta, derecha cerrada, decreciente
;   [5..1)  → 5 4 3 2     izquierda cerrada, derecha abierta, decreciente
;   (5..1)  → 4 3 2       izquierda abierta, derecha abierta, decreciente
;   (3..0]  → 2 1 0       izquierda abierta, derecha cerrada, decrece hasta 0
; cuando start <= end, itera en dirección creciente (paso +1).
;
; los extremos de intervalos ilimitados los decide el tipo estático de la variable:
;   i64 con [..]  va de i64.MIN (-9223372036854775808) a i64.MAX (9223372036854775807)
;   u8  con [..]  va de 0 a 255
;   i64 con [..)  va de i64.MIN a i64.MAX - 1 (excluye el máximo)
;   i64 con (..]  va de i64.MIN + 1 a i64.MAX (excluye el mínimo)
;   i64 con (..)  va de i64.MIN + 1 a i64.MAX - 1 (excluye ambos extremos)
; regla: '[' incluye el mínimo del tipo, '(' empieza en mínimo+1;
;       ']' incluye el máximo del tipo, ')' termina en máximo-1.

; ❌ rechazo explícito
;   los límites del intervalo deben ser enteros; no se admiten expresiones anidadas
;   for i <- [1.5..5.5] { }   ; error de compilación
;   for i <- [0..[1..5][0]] { } ; error de sintaxis

; ⚠️ evitar la ambigüedad de ...
;   el operador de rango es .. (dos puntos), la llamada a método self es .len().
;   al escribirlos juntos [0.. .len()) → [0...len()), tres puntos que parecen un único operador
;   (y la abreviatura de return ya está fijada como ***, desaconsejada; pero tres puntos seguidos siguen dificultando la lectura).
;   use self.len() en lugar de .len() para desambiguar: i <- [0..self.len()): { }
;   (dentro del cuerpo del método, self y . son semanticamente equivalentes)

; bucle condicional (nueva forma prefija (cond) { }, por defecto en `no fmt`; sustituye a la antigua for cond { })
; la forma sufija { } (cond) es totalmente equivalente (forma antigua)
; en la mayoría de los casos se puede usar range-for: i <- [0..n): { }
(x == 1) {
    do-something()
}
```
```

### Salir / omitir / retorno anticipado

> ⚠️ **El grupo de abreviaturas `*`（omitir esta iteración）/ `**`（salir del bucle）/ `***`（retorno de terminación) está desaconsejado por ahora**: escriba
> las palabras clave inglesas `continue` / `break` / `return`; `no fmt` normaliza todas las abreviaturas admitidas de vuelta a la forma de palabra clave.

```no
i <- [0..10): {

    ; continue (omite esta iteración, desaconsejado)
    continue

    ; break (sale del bucle, desaconsejado)
    break

    ; return/terminate (retorno anticipado, solo termina la función; planificado, desaconsejado)
    break
    continue
}

; recomendado: usar palabras clave inglesas (coherente con C/Rust, fácil de portar y compila y ejecuta con normalidad):
i <- [0..10): {

    ; equivalente a * (desaconsejado)
    continue

    ; equivalente a ** (desaconsejado)
    break

    ; equivalente a *** (desaconsejado), solo termina la función anticipadamente
    return
}

; alias de palabras clave (el lexer las admite todas, totalmente equivalentes a las originales):
;   ctn ≡ continue、brk ≡ break、rtn ≡ return; `no fmt` conserva la grafía del alias
i <- [0..10): {

    ; equivalente a continue
    ctn

    ; equivalente a break
    brk
}

; equivalente a return, solo termina la función anticipadamente
rtn
```

> ⚠️ **`return` (y su abreviatura `***`, desaconsejada) solo se puede usar desnudo, sin valor de retorno.**
> Las funciones de Nolang no tienen mecanismo de «valor de retorno»: el resultado siempre se envía asignando un
> **parámetro de resultado con nombre (out-param)** dentro del cuerpo (véase «Definición de funciones»).
> Por tanto, `return <valor>`, `return(expr)` y `*** <valor>` están **prohibidos**; el compilador, el formateador y el LSP informan del error:
>
> - compilador (`no build`): `Error: compilation error: parser errors: línea L, columna C: 'return' no puede ir seguido de un valor de retorno; … [E_GENERAL]`
> - `no vet`: `<archivo>:L:C: [ERROR] nolang-compile: 'return' no puede ir seguido de un valor de retorno; … [E_GENERAL]` (mismo formato que otros diagnósticos, uno por línea)
> - `no fmt`: `format error: …` y termina con estado distinto de cero
> - LSP: muestra un diagnóstico de error en rojo en la línea correspondiente
>
> Forma incorrecta:
>
> ```no
> has = (n i64) (r i64) {
>     n == 0 -> return 0        ; ❌ error en compilación / formateador / LSP
>     r = n
> }
> ```
>
> Forma correcta (primero se asigna el parámetro de resultado y luego se usa un `return` desnudo para terminar anticipadamente; las varias sentencias del cuerpo de un brazo deben ocupar cada una su línea):
>
> ```no
> has = (n i64) (r i64) {
>     n == 0 -> {
>         r = 0
>         return
>     }
>     r = n
> }
> ```
>
> ⚠️ Comprimirlo en una línea escribiendo `n == 0 -> { r = 0; return }` no es una abreviatura: `; return }` es todo comentario,
> así que el bloque nunca se cierra y todas las sentencias posteriores (normalmente el resto del cuerpo de la función) quedan engullidas silenciosamente en ese brazo —
> medido: `no vet` no da error y la función nunca llega a llamarse.

## Canales (`chan[T]`)

Un canal es una cola de valores **acotada, extensible, con envío no bloqueante y recepción en espera**; el extremo receptor consume los valores uno a uno mediante el `?T` de tres estados.
Conserva un solo símbolo sintáctico: `<-` (los demás usos, véase el `i <- range:` de «Control de flujo»).

```no
ch chan[i64] = with-cap(8)   ; capacidad inicial explícita
buf chan[i64]                 ; declaración desnuda: equivale a with-cap(0), crece según se envía

ch <- 1                       ; enviar (solo cuenta como envío si el tipo de la izquierda es chan[T])
n = ch.len()                  ; número actual de valores pendientes
c = ch.cap()                  ; capacidad inicial
ch.close()                    ; cerrar: no acepta más envíos, pero los valores existentes se pueden seguir tomando
```

### Recepción: bucle de tres estados

`v <- ch: { ... }` es un **bucle** que en cada vuelta enlaza el `?T` de tres estados a `v`; `break` puede salir anticipadamente.
`ok` significa que se obtuvo un valor, `nil` que el canal está cerrado y vacío, y `err` un **error real** (un handle cero no inicializado: no hay nada que esperar) — **estos dos últimos terminan el bucle** (los brazos de esa misma vuelta se ejecutan con normalidad).

Lo más importante después de `ok`: **una lectura vacía es «esperar un valor», no «no hay valor»**. Si el canal está abierto pero vacío, el lector
se suspende hasta que alguien haga `send` o `close`:

- en una corrutina (el clon `-async` creado por `co`) realmente se aparca: el número de estado se escribe en un frame del heap, la tarea se registra en la cadena de espera del
  canal y devuelve el control al planificador (véase la sección «Corrutinas»);
- en una función normal no se puede aparcar, así que **se bloquea**: impulsa repetidamente la cola de listos hasta que haya un valor disponible.

Por tanto, «quedar vacío» debe terminar el bucle mediante `close()`, con una forma idéntica a `range` sobre channel en Go.

```no
ch chan[i64] = with-cap(2)
ch <- 10
ch.close()

<- ch: {
    ok -> print('got {it}')      ; it es el elemento de esta vuelta
    nil -> print('closed')       ; aquí se cae al quedar vacío
    err -> print('bad channel')  ; aquí solo cae un handle cero
}
```

Para descartar la variable de bucle se escribe `<- ch: { ... }`; si la primera sentencia del body no es un match sobre `v`, entonces `v` se enlaza directamente al **elemento mismo** (`T`, no `?T`) y el body solo se ejecuta en las vueltas `ok`:

```no
i <- ch: {
    print(i)                     ; i es i64, no ?i64
}
```

Si quiere distinguir los tres estados por sí mismo y conservar la variable de bucle, basta con hacer match de ella al principio del body; escribir solo los brazos (omitiendo `v:`) es una abreviatura sinónima: el criterio recae en los **nombres de brazo**, y la presencia de `ok` / `nil` / `err` indica que se trata de los tres estados:

```no
i <- ch: {
    ok -> print('got {it}')      ; it es el elemento de esta vuelta, i es el ?i64 de esta vuelta
    nil -> print('closed')
    err -> print('empty')
}
```

El criterio es sintáctico, no de tipos (véase «Determinación de forma» más abajo). `no fmt` completa la abreviatura anterior al sujeto explícito `i: { ... }`; ambas formas se pueden volver a analizar y el formateo es idempotente.

### Determinación de forma

| Inicio del body | Forma | Tipo de `v` |
| --- | --- | --- |
| `co bar(i)`, `print(i)` u otra sentencia normal | elemento | `T` |
| `v: { ... }` | tres estados | `?T` |
| `ok -> ...` / `nil -> ...` / `err -> ...` | tres estados (abreviado) | `?T` |
| otro match desnudo (p. ej. `i > 5 -> ...`) | elemento | `T` |

La determinación debe hacerse **antes de analizar el body**: si el comienzo del bloque es un match, el parser lo desazucara de inmediato en una cadena `if` y después ya no se puede reconocer la intención original. Esta casilla llegó a fallar por completo: `i <- ch:` se trataba siempre como forma de elemento, los brazos de tres estados degeneraban en guards independientes entre sí (todos se ejecutaban en cada vuelta) y `it` se enlazaba al valor vacío.

### Semántica

| Aspecto | Regla |
| --- | --- |
| Capacidad | el `n` de `with-cap(n)` es la capacidad **inicial**; al llenarse se duplica, y `send` **nunca bloquea ni pierde valores** |
| Lectura vacía | **espera**: la corrutina se aparca, la función normal bloquea impulsando la cola de listos; `err` se reserva para errores reales como un handle cero |
| Cierre | tras `close()`, un `send` aborta el proceso; los valores ya encolados se siguen tomando en orden y al agotarse se informa `nil`; el cierre despierta también a todos los esperadores |
| Carga útil | en el envío se hace **copia profunda** (`str` / `%vec` usan clone), el receptor obtiene una copia independiente; no hay que preocuparse por que la variable de origen se modifique o se libere antes |
| Concurrencia | el canal no usa cerraduras (planificación cooperativa de un solo hilo): los esperadores cuelgan de la cadena `waiters` del bloque de control, y `send` / `close` descolgán toda la cadena y reencolan uno a uno; **despertar no equivale a obtener el valor**, así que al reanudarse hay que volver a comprobar la legibilidad; por eso el punto de reanudación de la corrutina es el propio bloque de park |
| Interbloqueo | si la cola de listos está vacía y la tarea objetivo sigue sin terminar ⇒ no se puede avanzar: el runtime imprime `nolang: deadlock: …` en stderr y aborta, en lugar de devolver un valor que nadie envió o girar en vacío |

> Una consecuencia directa de suspender la lectura vacía: **el bucle de vaciado debe terminarse con `close()`**. Las versiones antiguas informaban `err` para «abierto pero vacío», haciendo que el bucle saliera naturalmente y no hiciera falta cerrar el canal en la forma `for`; ahora esa ruta es una espera, y un programa que olvide `close()` se queda en la última vuelta (suspendido en una corrutina, declarado interbloqueo en una función normal).

### Limitaciones

- **Solo está implementado en el backend LLVM.** `no build --js` / `--wasm` dan un error explícito (`[chbkn001]`) al encontrar cualquier nodo de canal, en lugar de generar silenciosamente código incompleto.
- Los canales son **tipos valor** (un handle de una palabra de máquina) y se pueden pasar como parámetro, resultado o elemento; `chan[chan[T]]` y `?chan[T]` se pueden declarar (aún no cubiertos por `tests/chan.no`).

## Sentencia de marcador de posición (`...`)

`...` es una **sentencia de marcador de posición** del lenguaje: sintácticamente válida y semánticamente **no hace nada**. Se usa sobre todo en ejemplos de documentación, esqueletos de código y huecos `TODO`. En compilación, la sentencia completa se descarta en la fase AST→HIR y el artefacto es **idéntico** a «no haber escrito nunca esa línea» (medido: el binario `sha256` compilado a partir del mismo código con y sin `...` es el mismo).

```no
parse-line = (s str) (fields []str) {

    ; TODO: aquí faltan los caracteres de escape
    ...

    fields = csv.parse-line(s)
}

add = (a i64, b i64) (result i64) {
    result = a + b
    ...
}
```

Reglas:

- **solo puede ocupar una línea por sí sola** (puede llevar un comentario `;` al final). Escrituras como `fail -> ...`, `x = ...` o `[0...n)` son siempre error de sintaxis: `...` no tiene valor y no puede aparecer en posición de expresión ni en el resto de una sentencia.
- **no es un alias de `return`**, ni un **operador de rango**: el rango es `..` (dos puntos) y el retorno anticipado se escribe `return`.
- `no fmt` la conserva tal cual, sin borrarla ni moverla.

Todas las formas siguientes son inválidas:

```no
#{fmt-skip}
add = (a i64, b i64) (result i64) {
    result = a + b
    ...          ; ✅ única forma válida: ocupa una línea por sí sola
}

ok -> ...        ; ❌ error de sintaxis: `...` no puede aparecer en posición de expresión
x = ...          ; ❌ error de sintaxis: ídem
i <- [0...n): {  ; ❌ error de sintaxis: `...` no es operador de rango, escriba [0..self.n)
}
```

### Match

```no
; forma sencilla, it sirve para tomar el parámetro
x: {
    err -> log(it.msg()); it es el error estructurado err.err

    nil -> log('nil')

    -> do-right-thing(it)
}

; forma destructurada
x: {
    err(e) -> log(e.msg())

    nil -> log('nil')

    ok(v) -> do-right-thing(v)
}

; nota: el patrón de struct (`user: { User{id=1} -> ... }`) y la vinculación de campos aún no se admiten, por eso no se muestran

score: {
    [0..59] -> print('suspenso')

    [60..89] -> print('bien')

    [90..100] -> print('excelente')

    -> print('nota no válida')
}

; la sintaxis de intervalo tiene 16 combinaciones; regla: '[' incluye el extremo izquierdo, '(' lo excluye;
;                     ']' incluye el extremo derecho, ')' lo excluye.
;
; 【intervalos acotados】（4 tipos）— ambos extremos tienen valor concreto
;   [a..b]  → x >= a && x <= b   ambos extremos incluidos
;   [a..b)  → x >= a && x <  b   izquierdo incluido, derecho excluido
;   (a..b]  → x >  a && x <= b   izquierdo excluido, derecho incluido
;   (a..b)  → x >  a && x <  b   ambos extremos excluidos
;
; 【intervalos sin límite superior】（4 tipos）— se omite el extremo derecho (End=nil), solo se comprueba la cota inferior
;   [a..)   → x >= a              izquierdo incluido, sin límite superior
;   [a..]   → x >= a              izquierdo incluido, sin límite superior
;   (a..)   → x >  a              izquierdo excluido, sin límite superior
;   (a..]   → x >  a              izquierdo excluido, sin límite superior
;
; 【intervalos sin límite inferior】（4 tipos）— se omite el extremo izquierdo (Start=nil), solo se comprueba la cota superior
;   [..b]   → x <= b              sin límite inferior, derecho incluido
;   [..b)   → x <  b              sin límite inferior, derecho excluido
;   (..b]   → x <= b              sin límite inferior, derecho incluido
;   (..b)   → x <  b              sin límite inferior, derecho excluido
;
; 【intervalos totalmente ilimitados】（4 tipos）— se omiten ambos extremos, según mínimo/máximo del tipo
;   [..]    → true                mínimo del tipo ≤ x ≤ máximo del tipo (ambos incluidos)
;   [..)    → true                mínimo del tipo ≤ x < máximo del tipo
;   (..]    → true                mínimo del tipo < x ≤ máximo del tipo
;   (..)    → true                mínimo del tipo < x < máximo del tipo
;
; en match, un intervalo totalmente ilimitado se basa en el rango de valores del tipo de la variable comparada (siempre verdadero, equivale a catch-all).
; en range-for, se basa en el rango del tipo de la variable de iteración (p. ej. [..] de i64 va de i64.MIN a i64.MAX).
;
; ejemplo: clasificación completa con intervalos ilimitados
v = 85
result = v: {
    [..0) -> 'negative'

    [0..60) -> 'fail'

    [60..80) -> 'pass'

    [80..90) -> 'good'

    [90..100] -> 'excellent'

    (100..) -> 'extraordinary'
}
print(result)

num: {
    1 || 3 || 5 || 7 -> print('número impar pequeño')

    2 || 4 || 6 -> print('número par pequeño')

    -> print('número mayor')
}

; con valor de retorno, la última sentencia/valor
result = x: {
    1 -> 1

    2 -> 2 + 1

    -> a + b
}

; el body de un brazo de varias líneas debe usar llaves -> { ... }
x: {
    nil -> {
        log('nil')
        do-cleanup()
        return
    }

    err -> {
        log(it.msg())
        do-cleanup()
        return
    }

    ok -> print(it)
}
```

> **Regla de body de brazo multilínea**: cuando el body de un brazo contiene varias sentencias, hay que envolverlo con llaves `-> { ... }`. Un body de una sola línea se puede escribir directamente tras `->`. El motivo es que, sin llaves, no se puede insertar correctamente la vinculación `it` del match de option y se produce un error de compilación.

> **Semántica de match dentro de un for-in**: `i <- (a..b]: { 1 -> ... 2 -> ... }` ejecuta una vez el cuerpo del match para cada variable de iteración `i` (`1 ->` equivale a `i == 1 ->`, etc.). Es azúcar sintáctico de «un match por vuelta de iteración».

#### Las tres formas del sujeto de match

El sujeto de un match puede ser un **valor**, una **expresión** o un **bloque**:

| Forma | Escritura | Cómo se evalúa |
| --- | --- | --- |
| valor | `x: { ... }` | compara directamente `x` |
| expresión | `x + 1: { ... }` | primero calcula el valor de la expresión y luego compara |
| bloque | `{ ... }: { ... }` | ejecuta un cuerpo de sentencias y compara el resultado de los `?=` intermedios |

Las dos primeras son **expresiones**: pueden usarse como sentencia y también tomar un valor en contexto de valor (`v = x: { ok -> it, -> 0 }`).
La tercera **es solo una sentencia**: no es expresión, no se puede asignar, no tiene valor de fin de bloque; `-> _` sirve para descartar el resultado.
`no fmt` hace idempotente `}: {` y las reglas de líneas en blanco entre brazos son las mismas que en un match normal.

##### Sujeto de expresión (`expr: { ... }`)

El `: {` de `expr: { ... }` tiene prioridad **mayor** que `=`: a la derecha de una asignación constituye toda la RHS, no «primero se completa `r = <expr>` y luego viene un match independiente». Por eso la aritmética de option se puede desempaquetar en una sola forma:

```no
#{overflow=wrap}
rem = seconds % 3600: {
    ok -> it

    -> 0
}

#{overflow=wrap}
sec = rem % 60: {
    ok -> it

    -> 0
}
```

Esto equivale a la forma anterior de tres tramos que había que separar: `rem-opt = seconds % 3600` / `rem = 0` / `rem = rem-opt: { ok -> it  -> 0 }`. La primera asignación puede llevar tipo explícito (`rem i64 = seconds % 3600: { ... }`) y la reasignación se escribe directamente `rem = ...: { ... }`; ambas son un único match de posición de valor, cuyo tipo de resultado lo decide la expresión final del brazo.

Semántica y limitaciones:

- Cuando el sujeto es aritmética de option D6 (`+ - * / %`), el resultado es `?T`. El match lo normaliza a un temporal sintético `__match_subj_L_C ?T`: el sujeto se **evalúa exactamente una vez**, `ok -> it` se enlaza a la carga útil, la condición se resuelve comparando el tag de option y `-> 0` cubre `nil` / `err`. El nombre sintético no aparece en diagnósticos ni en la salida de `no fmt`.
- El resultado del match es una **T plana** y se registra en la tabla semántica auxiliar; por eso se puede seguir escribiendo `sec = rem % 60: { ... }`: varios matches consecutivos de posición de valor no pierden la carga útil.
- El match de posición de valor solo se adjunta en **contexto de valor** (lado derecho de asignación／`?=`／asignación compuesta) y `: { ... }` debe ser realmente un bloque match. Condiciones de bucle a nivel de sentencia como `cond: { body }` y los literales de struct siguen su ruta de análisis habitual y no se ven afectados.
- **La asignación con destino campo también es contexto de valor** (desde 2026-11-09): en `d.month = mp - 9: { ... }` y `b.n ?= b.n + 1: { ... }`, el `: {` se asocia al **valor derecho**, no a toda la asignación. Antes, estas dos ramas omitían propagar `CTX_EXPR` al analizar el valor derecho (los destinos identificador con `let` sí lo propagaban; `obj.f =` / `a[i] =` no), de modo que `: {` lo recogía la capa de sentencia y se aplicaba envolviendo toda la asignación, analizándose como `match(sujeto = (d.month = mp - 9))`: la asignación se ejecutaba igualmente y escribía el `?T` en el campo tras desempaquetarlo implícitamente, mientras que los brazos `ok -> it` / `-> 0` quedaban como código muerto. Ahora el valor del brazo sí vuelve a caer en el campo.
- Los destinos indexados (`a[i] = …: { ... }`) tienen la misma forma, pero D9 ya prohíbe la escritura directa por subíndice, así que el checker sigue rechazándolos con `idxwrite`; hay que reescribirlos con place-match (`a[i]: { ok -> it = v }`) o con `?=`.
- `no fmt` hace idempotente `expr: { ... }` y no lo separa en dos sentencias (asignación + match independiente).
- **Límite de visibilidad de tipos**: para normalizar un sujeto de aritmética de option a `?T`, los tipos declarados de ambos operandos deben ser visibles estáticamente para el parser. Si un operando viene de una cadena de métodos profunda (cuyo tipo de receptor ni la reproducción del AST logra inferir), añada un tipo explícito a su declaración, p. ej. `e i64 = flr.to-i64()`; si no, la normalización se omite conservadoramente, `ok -> it` no se vincula, MIR rellena con cero y el valor observado es siempre 0. En `src/std/number.no`, `f64.to-str-general` es precisamente ese caso: sin esa anotación, `fmt.fmt-f64(2.5, '')` degenera de `'2.5'` a `'3'`. La regresión está fijada en
  `src/parser/value_match_expr_subject_test.go::TestValueMatchExprSubjectASTAnnotatedOperand`
  (si se quita la anotación, la prueba falla).

`src/std` migró el 2026-11-09 **122 puntos en 22 módulos** a esta forma de un solo tramo para el desempaquetado de option: además de la forma triple inicial (`x-opt = expr` / `x = 0` / `x = x-opt: { ... }`), cubre la forma doble (sin inicialización a cero), la primera asignación con tipo explícito (`x i64 = expr: { ... }`) y las formas con la variable intermedia declarada por pares varias veces en la misma función. Tras plegar, el sujeto puede ser una llamada a método, una indexación o aritmética de option;
`ok -> it`／`ok(v) -> v`／brazos constantes／`-> _` están todos verificados por medición como equivalentes, y `no fmt` es idempotente en estos puntos.
La inferencia de tipos del compilador atiende también al caso de «la tabla semántica auxiliar encuentra un `?T`, pero la declaración es en realidad `i64`» con valores residuales del mismo nombre a nivel global; `ok -> it` no pierde su vinculación por eso.

Los casos conservados (9 puntos): ① el match interno escribe en la propia variable `-opt` (que es el sujeto de un match de posición de valor posterior); ② dos capas de match de option encadenadas; ③ match de posición de valor que despacha por valor (p. ej. errno → mensaje, no desempaquetado de option); ④ entre la declaración y el match hay otras sentencias, o la variable intermedia es el resultado acumulado entre ramas (plegar cambiaría el momento de evaluación).

##### Sujeto de bloque (`}: {`)

El sujeto de bloque envuelve un grupo de sentencias para que también una **función sin parámetro de resultado `?T`** pueda gestionar in situ la propagación de `?=`:

```no
; d i64: divisor, si es 0 entonces 6/d da err
f = (d i64) {
    {
        v ?= 6 / d; el fallo ya no exige que la función tenga un parámetro de resultado ?T
        print(v.to-str()); si falla, no se ejecuta
    }: {
        err -> print(it.msg())

        nil -> print('nil')

        -> _; descarta el resultado
    }
    print('después se sigue ejecutando'); el flujo continúa tras los brazos
}
```

Semántica:

- los `?=` del cuerpo los captura el **match de bloque más cercano** (en caso de anidamiento, gana el más interno; incluso si la función contenedora tiene un parámetro de resultado `?T`, gana el match de bloque). La captura es recursiva: cuentan los `?=` dentro de bloques desnudos, cuerpos if, cuerpos de bucle y brazos de match; la definición de función es una frontera, pero un literal de función no lo es.
- lo que se compara es el `?T` de tres estados, con la **comprobación de exhaustividad** estándar de los match de option: sin brazo de respaldo, hay que completar las tres ramas `nil` / `err` / `ok` aunque `ok ->` sea inalcanzable en tiempo de ejecución.
- si el cuerpo termina bien (cero `?=` o todos con éxito) ⇒ coincide con `nil`; si algún `?=` falla ⇒ coincide con el `nil` o `err` de esa vez (el `it` del brazo `err` es el `err.err` completo, véase ⟨El tipo de `it`⟩).
- **un fallo detiene todo el cuerpo**: no se ejecutan las sentencias posteriores ni las siguientes iteraciones del bucle; los brazos se ejecutan una sola vez. Es un «bloque de parada», no un `return` de función; sus `return` / `break` / `continue` propios no se ven afectados.
- la expansión se compone enteramente de nodos AST básicos ya existentes (variable local `?T` sintética + cadena de guardias `if` + match normal), sin nodos HIR/MIR nuevos. Los nombres sintéticos no aparecen en diagnósticos ni en la salida de `no fmt`.

> Diferencia con la captura in situ de `=`: `=` **guarda el fallo en la variable destino** y el flujo continúa; `}: {` **reúne el fallo en un match** y detiene el resto del cuerpo. Cuando hace falta «seguir tras el fallo», use `=` + un match posterior; cuando hace falta «saltar a un brazo de limpieza en cuanto falla», use `}: {`.

##### Elección entre match de bloque y match de posición de valor

Una misma cosa —devolver a una variable plana el resultado de una aritmética de option— tiene dos formas. **Cuando el cuerpo del bloque consta solo de esa sentencia, ambas son equivalentes; en cuanto hay otras sentencias en el cuerpo, o el valor derecho puede fallar por otra vía, la semántica diverge.**

```no
; A：match de bloque + `?=`（«try» a nivel de bloque）
{
    #{overflow=wrap}
    np ?= np + 1
}: {
    err -> _

    -> _
}

; B：match de posición de valor vincula directamente el destino (a nivel de sentencia)
#{overflow=wrap}
np = np + 1: {
    ok -> it

    -> _
}
```

Bajo `#{overflow=wrap}`, el resultado de `np + 1` **siempre es `ok` (valor con wrap)**; ambas generan una sola instrucción `add i64` sin rama de comprobación de desbordamiento, y el brazo de fallo es código muerto para «la suma en sí» — por eso en la mayoría de los puntos parecen «totalmente equivalentes». La diferencia real está en el **ámbito** y la **ruta de fallo**:

| Dimensión | A：match de bloque + `?=` | B：match de posición de valor |
| --- | --- | --- |
| Posición sintáctica | **sentencia**: sin valor final, no se puede asignar | **expresión**: se puede poner a la derecha de una asignación, como destino campo o a la derecha de `?=` |
| qué se escribe en el destino al fallar | **valor cero** (`i64` → `0`, `str` → `''`) | lo decide el brazo de respaldo: `-> _` **no escribe** (conserva el valor anterior), `-> 0` escribe 0, `-> sum` reescribe el valor antiguo |
| resto de sentencias del cuerpo | **se omiten todas** (incluidas las siguientes iteraciones del bucle) | se ejecutan con normalidad |
| qué cubre un brazo `err` | **todos** los `?=` del bloque | solo esa sentencia |
| destino con declaración de tipo | ❌ `q i64 ?= …` es error de análisis (`yh8904fm`) | ✅ `q i64 = …: { … }` |
| valor derecho plegable en compilación | ❌ `s8gh6opt` (`4 / 2` se pliega a plain, deja de ser option) | ✅ el sujeto se sigue normalizando a `?T` |
| requisito previo | la función tiene un parámetro de resultado `?T`, o está dentro de un match de bloque (si no, `l86f91me`) | ninguno |
| brazo cuando hay éxito | también entra una vez en el brazo (el cuerpo termina y coincide con `nil`); en el brazo solo conviene descartar o procesar | solo se ejecuta el brazo `ok` |

Orden de elección:

1. **sentencia única, el fallo se trata como 0 y no hay nada más que ejecutar** → equivalentes, prefiera B: un nivel menos de indentación y un store de valor cero menos; `no fmt` es idempotente en ambas formas.
2. **varias sentencias fallibles que comparten un mismo tratamiento de error** (leer archivo → analizar → volver a escribir) → A: un `?=` por línea, un mismo brazo `err ->` compartido, y el primer fallo para en el brazo de limpieza.
3. **el fallo debe conservar el valor anterior / sustituirlo por un respaldo concreto / dejar que las sentencias posteriores sigan** → B (`-> sum`, `-> 0`, `-> it`).
4. **el valor todavía debe participar en una operación** (lado derecho de asignación, destino campo, lado derecho de `?=`) → B: A es una sentencia y no tiene valor.
5. **procesar elemento por elemento y que un fallo individual no arruine el lote** → B; **que todo el lote falle en bloque y se aborte** → A (detiene incluso las iteraciones del bucle).

Dos errores frecuentes:

- 🔴 **Posición de la anotación**: `#{overflow=wrap}` debe ir **dentro del bloque**, justo encima de la línea con `?=`. Escribirla fuera del match de bloque (sobre `{`) **no surte efecto**: se midió que el desbordamiento superior sigue devolviendo `err` y `?=` pone `np` a cero.
- 🔴 **No copie el patrón A para un acumulador**: `sum ?= 1 / (i - 1)` en la vuelta que falla **borra también el `sum` acumulado, poniéndolo a `0`** (escritura del valor cero propia de `?=`). Para conservar el valor acumulado use la forma B con `-> sum`.

#### Guía de estilo para match

```no
#{fmt-skip}
; ❌ evitar repetir el cuerpo de las ramas
w = tls-c.send(req)
w: {
    nil -> {
        tls-c.close()
        return
    }

    err -> {
        tls-c.close()
        return
    }

    ok -> n = it
}
```
```

```no
; ✅ lógica común en el catch-all ->
w = tls-c.send(req)
w: {
    ok -> n = it

    -> {
        tls-c.close()
        return
    }
}

; ✅ y al revés: ramas simples con nombre, lógica compleja en ->
val: {
    nil -> return

    err -> log(it.msg())

    -> {
        n = it
        total = total + n
        process(n)
    }
}
```

```no
; una sentencia — sin llaves
val: {
    ok -> print(it)

    -> print('empty or error')
}

; varias sentencias — con llaves obligatorias
val: {
    ok -> {
        n = it
        total = total + n
    }

    -> {
        log('failed')
        return
    }
}
```

```no
; vinculación implícita de it
val: {
    ok -> process(it); it = el valor desempaquetado

    err -> log(it.msg()); it = el error estructurado err.err (no un str)

    -> log('empty'); catch-all, aquí nil
}
```

```no
#{fmt-skip}
; ámbito de it: los match anidados lo restauran por niveles
; (el nombre de función de relleno es keep — `use` es palabra clave de importación y no puede ser nombre de función;
;  además, un brazo `->` desnudo que lea it cuando nil/err no se han reclamado a la vez es error de compilación,
;  por eso en todos los niveles se escribe ok ->)
outer: {
    ok -> {
        keep(it)            ; it externo
        inner ?T = f(it)
        inner: {
            ok -> keep(it)  ; it interno = valor desempaquetado de inner
        }
        keep(it)            ; ✅ al terminar el brazo interno, it se ha restaurado al valor externo
    }
}
```

> **Atención**: la restauración solo ocurre en match **anidados**. Tras terminar el match más externo, `it` conserva el valor del último brazo; no use `it` fuera del match. Para usarlo en varios niveles, guárdelo en una variable con nombre (`doc = it`) o use una vinculación destructurada `ok(v) -> ...` para enlazar la carga útil a su propio nombre.

> **Mejor práctica (prefiera `it` para desempaquetar option)**: cuando el valor comparado es una **option `?t`**, escriba preferentemente `ok -> ...(it)...`  
> en lugar de `ok(v) -> ...(v)...`: no introduce un nombre de vinculación adicional y esquiva naturalmente el problema de «variables con el mismo nombre en ramas distintas que hacen que codegen mezcle tipos».  
> Solo dos casos exigen conservar la **vinculación con nombre** `ok(v) ->`: (1) el valor comparado es una **variante de enumeración definida por el usuario** (p. ej.  
> `res { ok(v str) }`, `shape { rect(w, h) }`) — allí `it` es **toda la enumeración**, no la carga útil, y para tomar esta hay que darle nombre;  
> (2) hace falta seguir usando el valor **después de un match anidado** — el match interno revincula `it`; use una vinculación con nombre o cópielo antes a una local con nombre.

### La escritura por subíndice exige desempaquetado explícito

La lectura indexada devuelve una option (fuera de límites `err`, `code.index-out`); **asignar directamente al subíndice convertiría un acceso fuera de límites en una escritura sin comprobación**, así que no se permite y hay que elegir una de estas dos formas:

```no
#{fmt-skip}
; ❌ no permitido: a[1] = 2
```
```

```no
; ✅ place-match: gestiona usted el brazo fuera de límites (it = posición del elemento, legible y escribible)
a[1]: {
    ok -> it = 2

    -> _
}

; ✅ propagación del error: cuando la función contenedora tiene un parámetro de resultado option (fuera de límites → propagar)
a[1] ?= 2
```

- Cuando el compilador puede demostrar que el subíndice está dentro de límites (arreglo de longitud fija + subíndice constante, variable de bucle demostrablemente dentro, etc.), **elimina internamente la comprobación y asigna directamente, sin coste en tiempo de ejecución**; pero sintácticamente hay que seguir escribiendo la forma desempaquetada: no hay ninguna exención del lado usuario.
- Los map (`[str]i64`, etc.) **no están sujetos a esta regla en absoluto**: `m['b'] = c` se escribe directamente, sin place-match ni `?=`: el subíndice de un map es una **clave**, no una posición; escribir es put/insert (si falta la clave se inserta, si existe se sobrescribe), siempre está definido y no existe la «escritura fuera de límites»; las bases de campo de struct (`self.keys[i] = v`) tampoco están sujetas por ahora.
- El lado de **lectura** de un map sigue el mismo esquema option (exactamente la misma regla que `a[i]` para arreglos/vec/slice): dentro de una función, `x = m[k]` da `?V` y, si la clave no existe, `nil`. `m[k]` equivale a `m.get(k)`, y `m[k] = v` equivale a `m.put(k, v)`; ambas formas se pueden mezclar libremente.
- `m[k] ?= v` es **error de compilación** (no solo redundante): el brazo `ok` de `?=` es «leer el elemento y volver a escribirlo en su **posición**», pero el `?V` que devuelve la lectura de un map es solo una copia de `get`, y volver a escribir en ella no llega al map. Escriba directamente `m[k] = v`. La forma place-match `m[k]: { ok -> it = v }` sigue siendo válida (equivale a put).

### La variable de iteración es también la «posición» del elemento

`x <- a` (donde `a` es `arr` / `vec` / `slice` / arreglo de longitud fija) enlaza `x` con la **posición del elemento mismo**, no con una copia cargada en cada vuelta:

- **leer** `x` vuelve a leer `a[__i]` — dentro de una misma vuelta, escribir y después leer permite ver el valor nuevo; `x = x + 1` es leer-modificar-escribir;
- **escribir** `x = v` cae de vuelta en `a[__i]`;
- el `it` del brazo match es un **alias** de esa misma posición (válido solo dentro del brazo), así que `x: { ok -> it = v }` equivale exactamente a `a[i]: { ok -> it = v }`.

```no
; los dos bloques siguientes dan el mismo resultado: cada elemento de a pasa a valer 5
a []i64 = [0, 0, 0]
i <- [0..3): {
    a[i]: {
        ok -> it = 5

        -> _
    }
}

b []i64 = [0, 0, 0]
x <- b: {
    x: {
        ok -> it = 5

        -> _
    }
}
```

- Cuando el elemento mismo es una option (`[]?T`), `it` es la **carga útil** y al volver a escribir se envuelve automáticamente en `ok`.
- Al terminar el brazo, el alias deja de valer: escribir `it = v` después del match no vuelve al contenedor.

**Casos en que no es una posición** (la escritura se queda en local):

- `c <- s` (`str`): el elemento es el **punto de código** decodificado; en la cadena no hay una casilla de almacenamiento correspondiente.
- `k <- m` (map): el subíndice es una **clave**, no una posición; la escritura no vuelve a la tabla.

### `it` solo se puede usar en brazos «demostrablemente de un solo caso»

El brazo catch-all `->` recoge todos los casos no reclamados explícitamente por los brazos anteriores. `it` solo tiene un significado único y claro cuando **queda exactamente un caso**:

```no
; ✅ -> es ok (nil / err ya están reclamados)
v: {
    nil -> log('empty')

    err -> log(it.msg())

    -> process(it)
}

; ✅ -> es err (ok / nil ya están reclamados)
v: {
    ok -> process(it)

    nil -> log('empty')

    -> log(it.msg())
}
```

```no
#{fmt-skip}
; ❌ -> puede ser nil o err —— error de compilación
v: {
    ok -> process(it)

    -> log(it)
}
```
```

Añada `nil ->` / `err ->`, o cambie a `ok ->`, y pasará. Los enum y el match desnudo (`{ cond -> ... }`)  
no están sujetos a esto: `it` se refiere al valor comparado mismo, sin posibilidad de nil / err.

> El `it` del brazo `err ->` **siempre es el valor de error (`err.err`)**, con independencia del tipo de elemento de la option —  
> `option { ok(v t), nil, err(e err.err) }`. Así, incluso si se compara una option escalar como `?i64` / `?bool`,  
> el `it` del brazo `err ->` es un error estructurado: para leer el mensaje use `msg = it.msg()`,  
> para comparar el código `it.is(code.io)`, y la ubicación está en `it.file` / `it.line` / `it.col`  
> (que el compilador inyecta automáticamente en el punto de construcción de `err(...)`).

```no
; ✅ combinar patrones de option: nil || err -> body
; cuando la option es nil o err se comparte la misma rama
val: {
    nil || err -> {
        cleanup()
        return
    }

    ok -> process(it)
}

; ✅ también se puede mezclar con el catch-all ->
val: {
    nil || err -> log('failed')

    ok -> process(it)
}
```

### Canalización de cortocircuito (`->`)

`->` es un **operador de canalización de cortocircuito asociativo por la izquierda**, con semántica fija, **sin que influya si hay una asignación fuera ni si estamos dentro de un brazo match**. Las tres situaciones —sentencia desnuda, body de una línea de un brazo match y lado derecho de una asignación— comparten la misma reducción (lowering).

Los nodos de la canalización se dividen en tres clases:

| Nodo | Efecto sobre el estado de la canalización |
| --- | --- |
| llamada con efecto secundario (sin retorno, como `print('A')`) | se ejecuta, **no cambia el estado**; si antes era Ok, sigue el siguiente nodo |
| llamada que devuelve `?T` (puede fallar, como `might-fail()`) | se ejecuta, **sobrescribe el estado**; en cuanto pasa a nil/err, **se omiten todos los nodos posteriores** |
| expresión de valor normal al final | solo se evalúa si el estado sigue siendo Ok |

**Solo un nodo que devuelve `?T` tiene capacidad de llevar la canalización al fallo**; una llamada como `print` nunca lo hace. Por eso «una cadena formada solo por `print`» parece no cortocircuitar: simplemente no hay ningún nodo que pueda generar un fallo.

```no
; sin nodos que puedan fallar → se ejecuta todo
ok -> print('A') -> print('B')

; might-fail() devuelve ?T → si falla, print('B') no se ejecuta
ok -> print('A') -> might-fail(x) -> print('B')

; lado derecho de una asignación: misma semántica
r = print('E') -> might-fail(x) -> 42
```

**Aclaración de límites:**

1. `->` tiene una semántica única: la sentencia desnuda y la expresión de asignación comparten la misma reducción de canalización, sin cambiar de modo según haya asignación exterior o no.
2. En un match, **`->` dentro del body de una línea es una continuación de la canalización, no un brazo nuevo**; los brazos se separan por **saltos de línea**:

```no
; ✅ un brazo, con una canalización en el body → se imprimen las dos líneas
ok -> print('A') -> print('B')

; ✅ dos brazos (separados por salto de línea) → si da ok no se imprime 'miss'
v: {
    ok -> print('hit')

    -> print('miss')
}
```

> ⚠️ Los brazos se separan por saltos de línea, así que `pat -> A -> B` escrito en una sola línea es **una canalización de un brazo**, no «si A, si no B». Si necesita if/else, escriba los brazos en líneas separadas o use un grupo de cortocircuito envuelto en `{}`.

1. **La canalización puede producir un valor**: al escribirla a la derecha de una asignación, el resultado de la canalización es el **nodo de valor final** (solo se evalúa si el estado sigue siendo Ok).

```no
n = 7
x = 1 > 2 -> 42; condición falsa → x conserva su valor 7
y = 2 > 1 -> 42; y == 42
s str = 'old'
s = 1 > 2 -> 'new'; cortocircuito → s sigue siendo 'old'
r = print('E') -> might-fail(bad) -> 99; fallo en medio → r conserva su valor
```

Si hay fallo (condición falsa o cortocircuito por un nodo `?T`), **la asignación no surte efecto**: la variable conserva su valor anterior; si es una declaración inicial, toma el valor cero de su tipo. Esto coincide con la forma de sentencia: `{ 1 > 2 -> x = 42 }` tampoco modifica `x`.

El tipo del valor se infiere de la expresión final del brazo (`str` / `i64` / `?T` todos valen) y comparte el mismo esquema de inferencia con la forma ya existente `x = subject: { arms }`.

### If / Else

El grupo if-else (de cortocircuito) **debe** envolverse en `{}`. Gana la primera condición que coincide y las posteriores no se comprueban.

```no
; múltiples ramas (nueva forma recomendada) — cadena if-else de cortocircuito
{
    a == 1 -> {
        a = 1
        b = 2
    }

    a == 2 || a == 3 -> do-something()

    -> {
        c = 0
    }
}

; if individual (se conserva) — if autónomo, sin cortocircuito
x == 1 -> do-something()

; expresión ternaria condition ? true-value : false-value
c = flag ? 1 : 2
max = sum > 10 ? sum : 10
```

**Reglas importantes:**

1. **Grupo de cortocircuito**: cuando varios `cond -> body` se envuelven en `{}`, forman una cadena if-elif-else y solo se ejecuta la primera rama que coincide.
2. **If independiente**: escribir directamente `cond -> body` en el cuerpo de una función o de un bucle (sin envolver en `{}`) es un if independiente, que no cortocircuita con los if-then vecinos.
   **En cuanto aparece un else hay que formar un grupo**: un brazo de respaldo `->` en una línea nueva, escrito fuera de `{}`, es **error de compilación**: un `cond ->` aislado con `->` da `k3r7mbqd`, y si encima tiene brazos `cond ->` hermanos da `m7vq3xrt` (el mensaje indica cuántos brazos se han descartado). El parser solo pliega el `->` de nueva línea en el **if desnudo más cercano** y los demás brazos quedan inactivos silenciosamente; lo que se lee literalmente es totalmente distinto de lo que se compila, por eso es una regla y no una advertencia. Envuelva todos los brazos en `{ ... }`; **un `cond -> body` aislado sin else sigue admitiéndose desnudo**.
3. **Prohibido mezclar**: los hijos directos de un grupo de cortocircuito `{}` deben ser todos ramas `cond -> body`, sin sentencias normales entremezcladas.

```no
#{fmt-skip}
; ❌ no cortocircuita — estos son if independientes, se comprueban todas las condiciones
git-dispatch = (cmd str) {
    cmd == 'a' -> {
        fa()
    }
    cmd == 'b' -> {
        fb()
    }
}
```
```

```no
; ✅ cortocircuito — envolver en {}, gana el primero que coincide
git-dispatch = (cmd str) {
    {
        cmd == 'a' -> {
            fa()
        }

        cmd == 'b' -> {
            fb()
        }

        true -> {
        }
    }
}
```

```no
#{fmt-skip}
; ❌ if/else escrito fuera del grupo de cortocircuito — error de compilación
; el `-> break` de nueva línea solo se cuelga de `c == 43`; aunque se dé `c == 45`, el break lo corta
;   c == 45 -> {
;       exp-sign = -1
;       i = i + 1
;   }
;   c == 43 -> i = i + 1
;   -> break
```
```

```no
; ✅ el mismo código escrito como grupo if — los brazos son mutuamente excluyentes, `->` es el verdadero respaldo
{
    c == 45 -> {
        exp-sign = -1
        i = i + 1
    }

    c == 43 -> i = i + 1

    -> break
}
```

```no
#{fmt-skip}
; ❌ match-block mezclado con sentencias normales → error de compilación
{
    cmd == 'a' -> {
        fa()
    }
    print(cmd)
    cmd == 'b' -> {
        fb()
    }
}
```
```

```no
; ✅ las sentencias normales van dentro del body de una rama
{
    cmd == 'a' -> {
        print(cmd)
        fa()
    }

    cmd == 'b' -> {
        fb()
    }

    true -> {
    }
}
```

### Programación asíncrona: grupo de corrutinas `awy { ... }` + `co`

Nolang tiene una sola sintaxis superficial de concurrencia: el grupo de corrutinas `awy { ... }`, dentro del cual se derivan tareas con `co`.
La palabra clave `run` se ha eliminado (`run` es ahora un identificador normal); el prefijo de espera `awy <handle>` y el grupo de corrutinas de bloque desnudo en posición de sentencia están obsoletos. **Escribir `co` fuera de un grupo es error de compilación** (`E_CO_OUTSIDE_GROUP`).

`co` tiene dos formas; ambas inician solo un **proceso** y ninguna produce un valor:

```no
; proceso: solo escribe en el canal, sin declarar valores de retorno
produce = (ch chan[i64], v i64) {
    ch <- v
    ch.close()
}

ch chan[i64] = with-cap(2)
awy {
    co produce(ch, 1)          ; ① spawn de proceso: co f(args)
    co {                       ; ② spawn de closure: co { ... }
        ch <- 2
        ch.close()
    }
}
sum = 0
i <- ch: {
    sum = sum + i
}
print(sum.to-str())            ; => 3
```

#### ① Spawn de proceso: `co f(args)`

`f` **no debe declarar valores de retorno**. El resultado del spawn no tiene destino (`co` no es un valor), así que una función con valor de retorno llamada por `co` produce `c0retval`:

```no
worker = (n i64) (r i64) { r = n }   ; tiene valor de retorno declarado
awy { co worker(1) }                  ; ✗ c0retval
```

El `self` que devuelve implícitamente un método **no cuenta** como valor de retorno: es el parámetro de salida receptor que lleva todo método, cuya razón de ser es permitir encadenar llamadas; por eso `co obj.flush()` es perfectamente normal:

```no
queue {
    n i64
}
queue.flush = () {          ; implícito (self queue), sin otros valores de retorno
    self.n = 0
}
awy { co q.flush() }        ; ✓
```

Cuando necesite un resultado, use un canal: el proceso envía el valor al canal y hace `close()`, y el llamador lo vacía fuera del grupo. No es una recomendación de estilo, sino una regla: el resultado de una llamada spawnada solo se puede leer después de que el grupo se haya unido (join), y el punto de lectura no coincide con el punto de join; ese es precisamente el escenario para el que existe `chan`.

#### ② Spawn de closure: `co { ... }`

`co { ... }` entrega in situ un proceso a una corrutina para que lo ejecute. Las **variables libres** del bloque se capturan **por valor** (instantánea en el momento del spawn, con la misma semántica que la copia profunda de los argumentos en un spawn normal):

```no
seed = 33
ch chan[i64] = with-cap(1)
awy {
    co {
        ch <- seed
        ch.close()
    }
}
r = 0
<- ch: { ok -> r = it }
print(r.to-str())          ; => 33
```

#### `co` no es un valor: solo puede formar una sentencia por sí mismo

`co` produce void, así que debe ocupar una sentencia completa. En lado derecho de asignación, posición de argumento o posición de retorno produce siempre `E_CO_NOT_STATEMENT` (`c0nostmt`):

```no
r = co produce(ch, 1)      ; ✗ c0nostmt
print(co produce(ch, 1))   ; ✗ c0nostmt
```

Además, después de `co` solo puede venir una «llamada» o un «bloque»; cualquier otra forma da `E_CO_NOT_CALL` (`c0notcal`).

#### Async incoloro: monomorfización automática

`co` es sintaxis incolora (colorless): usted escribe el nombre de función sin teñir `produce` y el compilador monomorfiza automáticamente la variante `produce-async` y la ejecuta internamente como una corrutina teñida sin pila. No escriba el sufijo `-async` a mano en código de aplicación.

Reglas:

- **Monomorfización**: `co produce(ch, 1)` genera automáticamente `produce-async` (copia el cuerpo de `produce` y lo renombra). Si `produce` también se llama de forma síncrona, ambas versiones conviven.
- **Transitividad**: si un proceso contiene a su vez `awy { co child(n) }`, ese proceso también se monomorfiza a `-async` y el teñido asíncrono se propaga hacia arriba por la cadena de llamadas.
- **Barrera**: dentro del grupo, las sentencias que no son `co` (asignaciones normales, `print`, bucles, `if`, **lectura de canal**…) esperan primero a todas las tareas en vuelo. Por eso las dependencias de datos dentro del grupo se escriben como «primero se lee el canal y luego se usa el valor leído para spawn»: la lectura del canal ya es una barrera y se lee mejor que una dependencia implícita entre variables.
- **El cuerpo de la función no es un grupo de corrutinas**: solo `awy { ... }` introduce un grupo. Un `co` en el cuerpo de una función, en una rama `if` o en un cuerpo de bucle se rechaza siempre.
- **Descartar el resultado también espera**: `awy { co side(5) }` sigue haciendo spawn + await, sin dejar tareas sin esperar.

#### Modelo de planificación: suspensión real

El clon `-async` no es algo que «se ejecuta entero y luego informa», sino una **máquina de estados reanudable**: las variables locales se trasladan a un frame en el heap colgado de `%task.frame`, cada punto de suspensión se convierte en el terminador del bloque (escribe el número de estado en el frame y devuelve el control), y la entrada de la función pasa a ser una cadena de comparaciones de estado (`__state == k` ⇒ salto directo al k-ésimo punto de reanudación).

Por tanto, hay tres puntos que **suspenden** en lugar de bloquear:

| Punto de suspensión | Condición de reanudación |
| --- | --- |
| `async-yield()` | se reencola a sí mismo al final de la cola y espera a llegar a la cabeza |
| `awy` de una subtarea aún incompleta | se despierta cuando termina la subtarea (registrado en la cadena `%task.waiters`; un despertar avisa a toda la cadena) |
| recepción vacía de canal (`v <- ch:` / `<- ch:`) | `send` / `close` descolgán toda la cadena de espera y reencolan uno a uno |

La planificación es cooperativa de un solo hilo, con cola de listos FIFO, así que **el orden es totalmente predecible** y no hace falta sleep: dos tareas que ceden una vez cada una dan necesariamente `a1 b1 a2 b2` (`tests/coro-yield.no` fija los códigos de evento). **Que se despierte un esperador no significa que ya tenga el valor**: un `send` añade un solo elemento pero puede despertar a varios, por eso el punto de reanudación del canal es el propio bloque de park, que vuelve a evaluar la legibilidad antes de decidir si toma el valor o sigue esperando.

> El interbloqueo es detectable: cola de listos vacía y tarea objetivo aún incompleta ⇒ el runtime imprime `nolang: deadlock: …` en stderr y aborta. Eso es mucho más honesto que «devolver un valor cero que nadie envió».

> **Reducción de bajo nivel** (no es sintaxis de usuario): cada `co` se convierte en `OpRun` y al final del grupo se inserta un `OpAwait` por tarea; `KRun` / `KAwait` / `OpRun` / `OpAwait` solo existen en HIR/MIR. La transformación de corrutinas la hace `PlanCoroutines` de MIR después de `Analyze` (`src/mir/coro.go`).

Cancelación: `cancelled()` permite una comprobación cooperativa de aut.cancelación dentro de una función corrutina; `cancel(h)` es una operación de handle de bajo nivel que la nueva sintaxis no expone y que el código de aplicación no usa.

### Asignación múltiple

Las funciones pueden devolver varios valores y la llamada se recibe con una asignación múltiple:

```no
; la función define varios parámetros de resultado
swap = (a i64, b i64) (x i64, y i64) {
    x = b
    y = a
}

; asignación múltiple
a, b = swap(5, 3)

; usar _ para ignorar retornos que no se necesitan (variable de marcador de posición)
_, b = swap(5, 3); solo se toma el segundo valor, se ignora el primero
a, _ = swap(5, 3); solo se toma el primero, se ignora el segundo
_, _ = swap(5, 3); se ignoran todos (la llamada es solo por efecto secundario)

; también se admite como body de un brazo match
val: {
    ok -> {
        a, b = parse-pair(it)
    }

    -> return
}
```

## Sobrecarga de operadores

Cuando el tipo estático del operando izquierdo define un método `_op` concreto, el compilador reescribe automáticamente en la fase de reducción la operación infija `a op b` como la llamada a método `a._op(b)`. El conjunto de sobrecarga es cerrado: 5 operadores aritméticos binarios simétricos:

| Operador | Método de sobrecarga |
|-------- | -------------------- |
| `+` | `_add` |
| `-` | `_sub` |
| `*` | `_mul` |
| `/` | `_div` |
| `%` | `_mod` |

```no
big {
    v i64
}

; el tipo de retorno se escribe ?big, el compilador lo considera payload puro big
big._add = (b big) (c ?big) {
    c = big {
        v: .v + b.v
    }
}

f = (a big, b big) (out big) {
    r big = a + b; el compilador lo reescribe como a._add(b)
    out = r
}
```

**Convención de tipos**: el parámetro de resultado de un método de sobrecarga **se escribe `?T`, pero su tipo efectivo es el payload puro `T`** — el compilador no hace envuelto/desenvuelto de option: en el cuerpo se asigna directamente un valor `T` a `c` y el llamador obtiene también `T`. Errores como la división por cero hacen trap en tiempo de ejecución igual que los tipos numéricos integrados, sin producir option.

**Puntos clave de semántica**:

- la reescritura solo se hace si el tipo estático del operando izquierdo define el método `_op` correspondiente; los tipos numéricos integrados (i64, f64, etc.) siguen usando aritmética integrada;
- la llamada explícita `a._add(b)` es totalmente equivalente a `a + b`;
- las cadenas anidadas por la izquierda se reescriben nivel a nivel: `a + b + c` → `(a._add(b))._add(c)`; el resultado intermedio sigue siendo de tipo `T` y puede seguir participando en operaciones o llamadas a método;
- la biblioteca estándar `bigint` usa este mecanismo para admitir `+ - * / %` (véase la subsección de bigint en «Biblioteca estándar > Otros módulos»).

## Arreglos y rebanadas

Los contenedores guardan copias de los datos; la variable original y el contenedor son independientes, lo que elimina referencias colgantes.

**Arreglo de longitud fija arr:**

```no
; usar un arreglo de longitud fija
a [3] = [1, 2, 3]; arreglo i64 de longitud 3
a [3]u16 = [1, 2, 3]; arreglo de longitud fija con tipo especificado

a[nil]
u16 = [1, 2, 3]; longitud inferida automáticamente (tipo especificado)
a[nil] = [1, 2, 3]; longitud inferida automáticamente (i64)
```

**Arreglo de longitud variable vec:**

```no
v = [1, 2, 3]; arreglo de longitud variable i64
bs = [0x11, 0x22, 0x33]
v []u8 = [1, 2, 3]; arreglo de longitud variable con tipo especificado

; preasignación (evita expansiones repetidas)
v = with-cap(100); len=0, cap=100 (se puede indexar tras push)
v = with-len(100); len=100, cap=100 (se puede indexar directamente)
v = with-cap-len(200, 100); len=100, cap=200 (reserva espacio para crecer)
```

**Rebanada slice (vista, no un tipo independiente):**

Una rebanada es una **vista** de los datos originales: no copia los datos ni crea un tipo nuevo e independiente.  
Internamente solo guarda un puntero al búfer original, una longitud y una capacidad, por tanto:

- modificar elementos a través de la rebanada afecta a los datos originales y viceversa;
- la rebanada no posee los datos; en cuanto se libera la variable original, la rebanada deja de ser válida;
- el tipo de la rebanada lo decide el tipo original y sus métodos se aplican naturalmente, sin mecanismo de «herencia».

```no
; admite arr/vec/str
; admite rangos, con la misma notación que for <- 
nums [5]u8 = [0, 1, 2, 3, 4]

nums[..]; [0 1 2 3 4]   totalmente ilimitada, equivale a toda la rebanada
nums[1..]; [1 2 3 4]     sin límite superior, del índice 1 al final
nums[..4]; [0 1 2 3 4]   sin límite inferior, del principio al índice 4
nums[2..3]; [2 3]        acotada, ambos extremos incluidos
nums[1..3]; [1 2 3]      acotada, ambos extremos incluidos
nums[1..3); [1 2]        izquierda cerrada, derecha abierta
nums(1..3); [2]          ambos extremos abiertos

; cadena (el resultado es una vista str, comparte memoria subyacente; los índices son posiciones de punto de código, como en s[i])
s = 'abc'
s[1..]; 'bc'
s[1..s.len()); 'bc'
b str = s[0..1]; el resultado ya es str, se puede asignar directamente a una variable str
```

> La sintaxis de rebanada admite las mismas 16 combinaciones de intervalo que match/range-for. En contexto de rebanada, los intervalos ilimitados se basan en la longitud de la rebanada: `[..]` es toda ella, `[a..]` del índice a al final y `[..b]` del principio al índice b.

**Tipo y métodos de la rebanada:**

La rebanada no genera un tipo nuevo e independiente, sino que es una vista del tipo original (con puntero de inicio y longitud ajustados).  
Por eso los métodos del tipo original se pueden usar directamente:

| Tipo original | Tipo de la vista | Métodos disponibles |
| --- | --- | --- |
| `arr` (`[n]t`) | `[]t<range>` | todos los métodos de `[]t` (como `len`, `push`, `pop`, `contains`, `reverse`, `clone`, `fill`, `to-arr`, etc.) |
| `vec` (`[]t`) | `[]<range>` | ídem |
| `str` | `str<range>` | todos los métodos de `str` (como `to-upper`, `to-lower`, `index`, `contains`, `slice`, `copy`, `fill`, etc.) |

```no
; rebanada de arr → vista vec, comparte la memoria del arr
a [5]u8 = [0, 1, 2, 3, 4]
s = a[1..4]; s es una vista []u8 que apunta a la memoria de a
n = s.len(); slice.len()

; rebanada de vec → vista vec, comparte la memoria del vec
v = [10, 20, 30, 40, 50]
s = v[2..]; s es una vista []i64
s.reverse(s.len()); slice.reverse

; rebanada de str → vista str, comparte la memoria de str
s = 'Hello World'
sub = s[6..]; sub es una vista de 'World'
upper = sub.to-upper(); str.to-upper

; modificar elementos a través de la rebanada afecta a los datos originales
data = [10, 20, 30, 40, 50]
view = data[1..4]; view = [20, 30, 40]
view[0] = 99; modifica el elemento de view
; data[1] también vale 99 ahora, porque view comparte la memoria de data
```

### Indexación

```no
; indexar una cadena → devuelve ?char (punto de código Unicode, no un byte)
c char = s[i]; tipo plain explícito para traer el resultado a char (véase ⟨Indexación segura⟩)

; arr, vec: obtener elemento
arr[i]; → ?elem
vec[i]; → ?elem

; map: obtener value
map[str]
```

> **El resultado de una lectura indexada siempre es `?T`**: `arr[i]` / `vec[i]` / arreglo de longitud fija / `slice` → `?elem`,  
> y la indexación `s[i]` de `str` / `txt` → `?char` (D6, la misma regla unificada que la aritmética entera).  
> Por tanto, el **tipo inferido de `str[i]` es `?char`**; para recuperar un `char` plain hay dos caminos:  
> escribir un tipo plain explícito en la vinculación (`c char = s[i]`, que se cortocircuita en la reducción y lee un valor plain),  
> o desempaquetar con match / `?=`.  
> `str[a..b]` sigue devolviendo `str` (vista con semántica de puntos de código, que comparte la memoria subyacente).  
> Un `char` se **convierte implícitamente a `str`** (codificado como cadena de un carácter), así que todas las formas siguientes son válidas sin llamar manualmente a `char.to-str()`:

```no
s = 'héllo'
c char = s[1]; 'é' (char, cuyo punto de código es 233)
a str = s[1]; 'é' (conversión implícita char → str)
b str = s[0..1]; 'hé' (el resultado de la rebanada ya es str)
msg = 'first: ' - s[0]; char se convierte implícitamente a str en la concatenación (-)
ok = s[0] == 'h'; char se convierte implícitamente a str en la comparación
```

### Indexación segura (Safe Indexing)

Al indexar `v[i]` sobre `arr` / `vec` / `slice` (`[]T`) / arreglos de longitud fija / `str` / `txt`, **el resultado es siempre `?T`**: fuera de límites devuelve `err` (`err('index out of range', code.index-out)`, con la ubicación del subíndice en el código fuente) y dentro devuelve `ok(valor)`; nunca aborta (`runtime error: index out of bounds`). Es la misma regla que el desbordamiento no anotado de `+ - * / %` (véase ⟨Desbordamiento en aritmética entera⟩).

Para consumir un `?T` solo hay tres caminos (**la anotación `#{index-out=DEF}` quedó obsoleta y se eliminó en D11**; los comentarios residuales son error de compilación;
y tampoco existe el atajo «de un plain explícito a un plain»: `v i64 = a[i]` también debe consumirse explícitamente):

**1. Lectura — desempaquetado con match**

Un match de valor toma el valor directamente, o un match de sentencia asigna dentro del brazo `ok`. `-> _` es el brazo de «descarte» y en contexto de valor produce el **valor cero del tipo del elemento** (i64→0, str→'', byte→0, contenedor→contenedor vacío).

```no
; match de valor
x = state[5]: {
    ok -> it

    -> _
}

; match de sentencia
state[5]: {
    ok -> x = it

    -> _
}
```

**2. Escritura — place-match**

En el brazo `ok`, `it` es la **posición del elemento** (legible y escribible), no una copia del valor.

```no
state[5]: {
    ok -> it = x

    -> _
}
```

> **Excepción map:** escribir `m['k'] = 1` **no necesita desempaquetado**: el subíndice de un map es una clave y escribir es put/insert (si falta la clave se inserta, si existe se sobrescribe); siempre está definido y no hay «escritura fuera de límites» que vigilar. Pero **leer de un map `m['k']` sí devuelve `?V` y hay que desempaquetarlo**. `m[k] ?= v` es **error de compilación** (el `?V` leído por `get` es solo una copia y volver a escribir en ella no llega al map): escriba directamente `m[k] = v`.

**3. Abreviatura `?=` — propagación del error**

`x ?= state[5]` (leer) y `state[5] ?= x` (escribir) son abreviaturas de los dos match anteriores: al fallar propagan toda la option hacia arriba. Solo son válidas en **uno de estos dos casos**:

- la función contenedora tiene un parámetro de resultado `?T` — al fallar escriben en el parámetro de resultado y hacen un `return` anticipado;
- la sentencia está dentro del cuerpo de un **match de bloque a nivel de sentencia** `{ ... }: { ... }` — lo captura el match de bloque más cercano, el cuerpo se detiene y los brazos se ejecutan una sola vez; la función **no necesita** un parámetro de resultado `?T`.

Si no se da ninguno de los dos, es error de compilación. Por eso el respaldo estándar cuando «el valor de retorno no es option pero quiero usar la abreviatura» es envolver el cuerpo de la función en un match de bloque:

```no
handle = (x i64) {
    {
        y ?= x + 1
        print(1)
    }: {
        err -> _

        -> _
    }
}
```

**Lectura anidada**: el match interno revincula `it`, así que dentro se usa la vinculación con nombre `ok(it1) -> it1`. La forma exacta que da el usuario (un match de valor escrito directamente a la derecha del `it = ...` de un place-match) **vuelve a escribir correctamente en el contenedor desde 2026-11-06**: el compilador, al reducir, eleva ese match de valor a un temporal sintético y revincula `it`; la regresión está en el bloque `place-write-value-match` de `tests/match.no`. **Forma preferida**: enlazar el resultado del match de valor directamente al nombre final que se va a usar, sin meterlo antes en un temporal de un solo uso y copiarlo después.

```no
#{fmt-skip}
; ✅ vinculación directa a la variable destino
at = argt[i]: {
    ok -> it

    -> _
}
s.farg.push(at)

; ❌ no introducir un temporal de un solo uso para algo que se usa una vez
ixout1 = argt[i]: { ok -> it  -> _ }
at = ixout1
s.farg.push(at)
```

«Elevar el match de valor a un temporal con nombre fuera del brazo y luego hacer `it = ixout1` dentro» **no es equivalente a la forma directa** y no es una opción de formato libremente intercambiable: el momento de evaluación es distinto (una vez elevado fuera, los brazos `nil` / `err` / catch-all externos también leen una vez); `it` solo vale dentro del brazo y apunta a la posición del elemento, así que la copia debe quedarse dentro; el temporal con nombre es una vinculación nueva, con su propio move／propiedad y destrucción al salir del ámbito; y en caso de anidamiento el nivel interno debe usar la vinculación con nombre `ok(it1) -> it1`, no copiar el `ok -> it` externo. Solo eleve cuando realmente vaya a reutilizar el mismo valor en varios puntos, y use un nombre con significado; no deje nombres de marcador generados por herramientas de migración como `ixout…`.

#### Recoger los temporales de un solo uso restantes con `?=` y match de bloque

Lo anterior trataba de «vincular directamente cuando se usa una vez». Un paso más: la indexación segura devuelve `?T`, **así que cuando el parámetro de resultado de una función es él mismo `?T`, ni siquiera hace falta escribir el match de valor: `?=` lo propaga directamente en una línea**:

```no
#{fmt-skip}
; ✅ la función tiene un parámetro de resultado ?T: sin temporal, sin match
at ?= argt[i]
s.farg.push(at)
```

**Cuando el resultado de la función no es una opción** (lo más común: una función auxiliar sin valor de retorno, o que solo escribe de vuelta en un contenedor), envuelva la sentencia con esta forma «parecida a try»:

```no
#{fmt-skip}
handle = (x i64) {
    {
        y ?= x + 1
        print(y)
    }: {
        err -> _

        -> _
    }
    print('continues afterwards')
}
```

⚠️ **Esta no es una sintaxis `try` nueva, y el lenguaje no tiene la palabra clave `try`.** Es simplemente un **match de bloque a nivel de sentencia** (`{ ... }: { ... }`), cuyo sujeto es el `?T` de tres estados; `?=` propaga el fallo al match de bloque **más cercano** (véase «Propagación de errores con `?=`» y «Las tres formas del sujeto de match»). Cuando falla, se omiten todas las sentencias restantes del cuerpo del bloque —incluidas las iteraciones posteriores del bucle—, el brazo se ejecuta una sola vez y luego **continúa ejecutando el código posterior al bloque**, no es un `return` de la función. `err -> _` descarta el error y `-> _` descarta el resultado correcto como respaldo; para manejar el error se puede escribir `err -> print(it.msg())`.

⚠️ **La diferencia semántica hay que dejarla clara** (medido en la práctica, no por intuición): cuando `?=` falla, **el destino se escribe con el valor cero de todos modos**: `i64` obtiene `0`, `str` obtiene `''`, `out[1]` obtiene `0`. Pero el brazo de respaldo `-> _` del match de valor **no escribe en el destino**: cuando el destino es una **declaración nueva** ya está inicializado a cero, así que parece igual; cuando el destino **ya tiene un valor**, ambos divergen (`?=` lo pone a cero, `-> _` conserva el valor original; para ponerlo a cero hay que escribir `-> 0`). Otra diferencia es el flujo de control: `?=` **omite las sentencias posteriores a él en el cuerpo del bloque** (en una función `?T` es un `return` anticipado, en un match de bloque es un salto directo al brazo).

Así que: **cuando el cuerpo del bloque solo contiene esta sentencia y el valor anterior al fallo no tiene significado**, escribir el brazo de respaldo como `-> 0` es totalmente equivalente (y este es justamente el patrón de la reescritura por lotes en este repositorio, donde los temporales `ixout…` suelen ser declaraciones nuevas); pero cuando después de `?=` hay otras sentencias en el cuerpo (contadores de bucle, asignaciones posteriores), o el valor anterior del destino es significativo, hay divergencia y esos sitios no se pueden reemplazar de forma mecánica. La tabla completa de compensaciones está en «Compensaciones entre match de bloque y match de valor».

Otro punto fácil de pasar por alto: **el brazo también se ejecuta una vez en caso de éxito**, el catch-all recibe el valor correcto (`-> print('x')` imprime). Por eso en el brazo solo se debe poner descarte o procesamiento puro; las formas `err -> _` / `-> _` no tienen efectos secundarios.

De aquí se obtiene el orden para limpiar los temporales de un solo uso `ixout…`:

1. El valor se usa una sola vez en un mismo lugar → vincularlo directamente al nombre del destino final (véase arriba).
2. La función tiene un parámetro de resultado `?T` → `TARGET ?= EXPR`, sin necesidad de temporal.
3. En caso contrario → poner la sentencia dentro de un match de bloque y escribir `TARGET ?= EXPR`; la escritura de vuelta con place-match (`SUBJ: { ok -> it = temp  -> _ }`) también se reduce directamente a `SUBJ ?= EXPR`.
4. Conservar un temporal con nombre solo cuando de verdad se vaya a reutilizar el mismo valor en varios lugares, y usar un nombre de variable con significado.

Dos límites: `?=` es un error de compilación cuando no hay ni un parámetro de resultado `?T` ni se está dentro del cuerpo de un match de bloque (`l86f91me`); **un LHS con declaración de tipo no puede combinarse con `?=`** (`ai i64 ?= a[i]` es un error de análisis `yh8904fm`, `expected assignment operator`). Cuando se necesite una promoción explícita como `byte → i64`, usar en su lugar un match de valor directo con declaración de tipo:

```no
#{fmt-skip}
ai i64 = a[i]: {
    ok -> it

    -> _
}
```

> **Ámbito de aplicación:** válido para `arr` / `vec` / `slice` / arreglos de longitud fija / `str` / `txt` por igual; solo la indexación con **base de variable directa** (`v[i]`) se reescribe a la forma segura; `receiver.field[i]` sigue la ruta original de comprobación de límites. La lectura indexada dentro de operadores de comparación compara el payload (`a[i] == 5`), no todo `{tag, payload}`.

> ⛔ **Error fatal:** usar `?T` como un valor ordinario (como en `crc = crc ^ s[i]`, `f(s[i])`, `v i64 = a[i]`) bloquea `no build`; el compilador sugiere usar match o `?=` (envuelto en un match de bloque cuando no hay parámetro de resultado `?T`). El corpus existente ya se migró de una sola vez con `no fmt --fix=index-out`, y esa herramienta se retiró con D11.

> **Garantía fundamental:** un desbordamiento de límites nunca falla silenciosamente: o lo maneja un match o lo propaga `?=`.

> **La lectura indexada dentro de operadores de comparación** también aplica la indexación segura: `a[i] == 5` compara el payload (misma regla que `?T == 5`), no compara todo `{tag, payload}`. Al desbordarse, se lee `err` (`code.index-out`), cuya comparación con cualquier valor es **falsa** (`a[i] == 0` y `a[i] == 5` son ambos falsos), y **no** se lee memoria fuera de límites. Lo mismo vale para `s[0] == 'a'`: al comparar un punto de código con una cadena de un solo carácter se aplica la conversión implícita `char → str`.

> **Subíndice anidado** `a[i][j]`: **ambos niveles son indexación segura**. El nivel externo `a[i]` comprueba primero los límites y determina si existe; solo si existe se le toma `[j]`; si cualquier nivel se desborda, toda la expresión es `err`. Por eso `a[oob][0] == 'c'` es falso y no aborta.

> **La comparación de contenedores** `[]T == []T` (y `[N]T == [N]T`) es una **comparación estructural**: primero se comparan las longitudes y luego los elementos uno por uno. Cuando los elementos son contenedores (`[][]i64`) se recorre nivel por nivel; cuando son `str` se compara el **contenido**, no el puntero data. Las comparaciones de orden (`<` / `<=` / `>` / `>=`) no están definidas para contenedores; solo se admiten `==` / `!=`.

> **`nil` solo puede compararse con opciones**: `?T == nil` / `?T != nil` son válidos; `T == nil` con un tipo que no es opción (`str`, `[]i64`, `[N]T`, `txt`, `i64`, estructuras…) es un **error de compilación** (`q4nilcmp`). El motivo no es un capricho: `str == nil` siempre es falso (se compara una estructura `%str-long` con 0), y `[]i64 == nil` cae en la ruta de comparación de enteros y genera LLVM inválido (`icmp eq i64 %vec, i64`); ambos dan silenciosamente una respuesta equivocada. Para expresar «puede no haber valor», declarar el tipo como `?T` y compararlo con `nil`.

## Estructuras

Las definiciones y los literales de estructura deben usar obligatoriamente el formato multilínea: cada campo ocupa su propia línea, los campos no se separan con comas y no hay coma al final.

```no
user {
    name str
    age i64
}

u = user {
    name: 'Alice'
    age: 30
}
u.name = 'Bob'
u.age = 25
print(u.name)
```

### Estructuras que implementan interfaces

Una estructura puede implementar una o varias interfaces; los nombres de las interfaces se escriben después del nombre de la estructura, separados por comas.

```no
; implementar una sola interfaz
user json {
    name str
    age i64
}

; implementar varias interfaces
file enter, leave {
    path str
    fd i64
}
```

#### Implementación de interfaces entre módulos

Al implementar una interfaz definida en **otro módulo**, el nombre de la interfaz debe llevar el prefijo del módulo (`ShortName.`). Véase [Prefijo de llamada entre módulos](module.md).

```no
#{fmt-skip}
; ❌ error: db, rows y stmt son interfaces definidas en el módulo sql; no se puede omitir el prefijo
db-mysql db {
    fd i64
}
```
```

```no
; ✅ correcto: usar sql.db, sql.rows, sql.stmt
db-mysql sql.db {
    fd i64
}

rows-mysql sql.rows {
    fd i64
}

stmt-mysql sql.stmt {
    fd i64
}
```

> Las interfaces integradas (`enter`, `leave`) y las interfaces definidas en el mismo archivo no necesitan prefijo de módulo.

## Métodos

Los métodos se definen sobre un tipo y usan `.` para referirse al receptor (receiver).

### Sintaxis

```no
#{fmt-skip}
type.method-name = (params) (results) {
    ; . es el receptor
}
```

### Reglas

1. El nombre del método usa el formato `type.method`, donde type debe ser un tipo ya definido.
2. El receptor no necesita declararse explícitamente como parámetro; dentro del cuerpo del método se hace referencia a él con `.`.
3. Para llamarlo se usa la sintaxis `receiver.method(args)`.
4. Los valores de retorno van en el segundo grupo de paréntesis, igual que en las funciones ordinarias.
5. **Desugarización del azúcar sintáctico**: `type.method = (inputs) (rest-outputs...) {}` es equivalente a la función ordinaria  
   `method = (inputs) (self type, rest-outputs...) {}`. Al llamar `instance.method(args)`,  
   instance se vincula al primer parámetro de la lista de salida, `self`; dentro del cuerpo de la función,  
   `.` designa la zona de salida `self`; las escrituras que la función hace en `self`  
   modifican directamente la instancia original. El resto de salidas de la lista, aparte de `self`,  
   admiten recepción destructurada opcional o descarte directo.

### Ejemplo

```no
; método str
str.to-upper = () (out str) {
    n = .len-bytes()
    b []byte = with-cap-len(n, n)
    i <- [0..n): {
        c ?byte = .byte(i)
        v = c: {
            ok -> it

            nil -> 0

            err -> 0
        }
        {
            v >= 97 && v <= 122 -> b[i] = v - 32

            -> b[i] = v
        }
    }
    out = b.to-bytes-str()
}

; método char
char.is-digit = () (result bool) {
    result = false
    . >= 48 && . <= 57 -> result = true
}

; método struct
user {
    name str
    age i64
}

user.greet = () {
    print('Hello, ' - .name)
}

; llamada
s = 'hello'
u = s.to-upper(); receiver.method()
c char = 5
d = c.is-digit(); receiver.method()
u = user {
    name: 'Alice'
    age: 30
}
u.greet()
```

## Interfaces

```no
; definir una interfaz
json {
    to-json()
}

; implementación por defecto de la interfaz
json.to-json = () {
}

noml {
    to-noml()
}

serialize {
    json
    noml
}

serialize = json | noml
serialize = json & noml

; implementación de la interfaz
user json {
    name str
    age i64
}

; sobrescribir + llamar a la implementación padre
user.to-json = () {

    ; implementación padre
    ..to-json()
}

user.other = () {

    ; implementación actual
    .to-json()

    ; implementación padre
    ..to-json()
}
```

### Fusión de interfaces

Una interfaz puede «fusionar» los métodos de otras interfaces: los nombres de las interfaces padre se escriben dentro del cuerpo de la interfaz, **uno por línea** (pueden llevar prefijo de módulo), sin comas ni paréntesis:
```no
json {
    to-json()
}
noml {
    to-noml()
}

serialize {
    json
    noml
}

; fusión transitiva: roundtrip obtiene todos los métodos de serialize, json y noml
roundtrip {
    serialize
    roundtrip-json()
}
```

Reglas:

- **Fusión recursiva**: la fusión es transitiva: `roundtrip` fusiona `serialize`, y `serialize` a su vez fusiona `json` / `noml`, de modo que `roundtrip` posee los métodos de las tres interfaces.
- **Uno por línea, sin comas**: `serialize { json, noml }` tiene la misma forma que un enum de estilo C (`color { red, green }`), por lo que se sigue analizando como enum, no como fusión de interfaces.
- **Métodos con el mismo nombre**: firmas idénticas (tipos de parámetros y de retorno coincidentes; el nombre del receptor puede diferir) ⇒ se deduplican y se conserva uno; firmas distintas ⇒ se emite un diagnóstico de error.
- **Lado de la implementación**: el tipo solo necesita escribir la declaración de implementación (lista inicial `user serialize { name str }`); los métodos de las interfaces padre también cuentan como implementados, y la comprobación de firmas atraviesa las interfaces padre.
- **`no fmt`**: la antigua forma de «lista inicial» (`serialize json, noml { }`) todavía se analiza, pero `no fmt` la reescribe a la forma interna y `no vet` emite un WARNING al respecto.

Diagnósticos (`no vet` y el editor comparten la misma fuente, TraceID `ifmrg001`):

| Caso | Mensaje |
| --- | --- |
| Interfaz padre no declarada | `interface 'serialize': unknown parent interface 'noml'` |
| Fusión circular | `interface 'a': merge cycle a -> b -> a` |
| Mismo nombre, firma distinta | `interface 'serialize': merged method 'to-str' has conflicting signatures ('json.to-str', 'noml.to-str')` |

> Formas como `serialize = json \| noml` son sintaxis de **tipo unión** (union type) y no tienen nada que ver con la fusión de interfaces; la fusión de interfaces solo afecta a «qué métodos debe tener un tipo».

### Interfaces especiales

```no
file {
    enter
    leave
}
```

## Enumeraciones

```no
; red=0, green=1, blue=2
color {
    red,
    green,
    blue,
}

; en los métodos ordinarios, a, b, c se definen realmente como a=0, b=1, c=2... Este es un punto en el que el lenguaje difiere de otros.
; por eso normalmente no se pueden definir varias variables con comas

; esta es una enumeración especial: puede tener tipos, comas y alias
enum-name {
    a t,
    b u,
    c v,
}

; nótese que esto es un struct ordinario: los campos múltiples no llevan comas
struct-name {
    a t
    b u
    c v
}
```

### Enumeraciones etiquetadas (con payload)

Una variante puede llevar campos de payload con nombre, escritos en forma de paréntesis `variante(campo tipo)`; una variante sin payload se escribe directamente como nombre simple.  
Una variante puede llevar **varios** campos (un payload de varios campos se representa en el montón como un struct sintetizado):

```no
#{fmt-skip}
; enumeración etiquetada: las variantes pueden llevar payload; el tag sigue el orden de declaración 0,1,2...
result {
    ok(v t),
    nil,
    err(e str),
}

; con valor: campo de payload v, de tipo t
; valor nulo: sin payload
; error: campo de payload e, de tipo str (enumeración definida por el usuario, sin relación con la opción integrada)

shape {
    circle(r f64),
    rect(w f64, h f64),
    dot,
}
; un solo campo
; varios campos
; sin payload
```
```

Al hacer match se usa el nombre de la variante como patrón de brazo; una variante con payload puede desestructurar y vincular su payload (los varios campos se vinculan posicionalmente, uno a uno):

```no
r result = ok(42)
r: {
    ok(v) -> print(v); vincular el payload de ok a v

    nil -> print('empty')

    err(e) -> print(e)
}

s shape = rect(3.0, 4.0)
s: {
    circle(r) -> print(r); vincular el campo único

    rect(w, h) -> print(w * h); vincular varios campos por posición

    dot -> print('dot')
}
```

Una variante también se puede construir directamente en **posición de expresión**, como argumento de función o como payload de otra variante:

```no
x f64 = perimeter(rect(3.0, 4.0)); construido en línea y pasado como argumento
o outer = wrap(a(5)); un payload de enumeración que es a su vez otra enumeración
```

> **Espacios de nombres y resolución de nombres simples**: internamente el compilador registra las variantes con un nombre totalmente cualificado (`módulo.enum.variante`, como `option.option.ok`, `some-mod.my-result.ok`), de modo que las variantes con el mismo nombre en distintas enumeraciones no colisionan. El usuario escribe solo el nombre simple (`ok`, `rect`) y el compilador **completa automáticamente el nombre completo a partir del tipo estático de la variable que se está comparando o construyendo**. Por eso dos enumeraciones pueden tener ambas una variante `ok` con distinto orden de tag y aun así distinguirse correctamente.
>
> También se puede escribir la forma antigua con el tipo separado por un espacio `ok t`, equivalente a `ok(v t)` (con el nombre de campo omitido). Los nombres de las variantes de una enumeración etiquetada se registran en la tabla de variantes de enumeración del compilador, para su uso en la comparación y en la comprobación de exhaustividad.

### Enumeraciones etiquetadas integradas (`#{buildin}`)

Anteponer `#{buildin}` a una enumeración etiquetada la marca como **enumeración integrada**: sus variantes (nombres, orden, tipos de payload) se usan para la comparación y la comprobación de exhaustividad, pero la representación subyacente y la construcción las proporciona el runtime/builtin, y **no se genera ningún struct/union visible para el usuario**. La opción `?t` se declara de esta manera:

```no
; src/std/global.no
#{buildin}
option {
    ok(v t),
    nil,
    err(e err.err),
}
```

Los tag de `option` son, en orden, `0=ok`, `1=nil`, `2=err`, acordes con la representación integrada de option. El usuario no necesita declarar option por su cuenta; el `#{buildin}` de aquí unifica la fuente de las variantes de la opción integrada (véase «Tipos anulables (option)»).

### Anotaciones de enumeración y disposición en memoria

Las enumeraciones admiten anotaciones `#{...}`, con las mismas reglas de colocación que cualquier otro destino (véase «Colocación de anotaciones»): **arriba** (en su propia línea) o **después** (al final de la misma línea); escribir la anotación **delante del destino en la misma línea** es un error de compilación.

La anotación se puede colocar en **dos lugares**:

1. **Encima de todo el enum** — afecta a la definición del enum en sí (como `#{buildin}`, `#{inline=true}`);
2. **Encima de un miembro individual o al final de su línea** — afecta a ese miembro (tanto los «valores» de un enum de estilo C como las «variantes» de una enumeración etiquetada).

```no
; ① todo el enum: en su propia línea, encima del enum
#{inline=true}
box {
    full(v i64) #{doc='con valor'},
    empty #{doc='vacío'},
}

; ② una sola variante: en su propia línea, encima de la variante

; ② una sola variante: al final de la misma línea

; `#{inline=false}`: escribir explícitamente «no usar disposición inline»
#{inline=false}
plain {
    a,
    b,
}

color {
    red #{deprecated},
    green #{deprecated},
    blue,
}
; los valores individuales de un enum de estilo C admiten estas mismas dos formas
```

**Las anotaciones no cambian la estructura del enum**: los nombres de las variantes, el orden de declaración (tag), los campos de payload y sus tipos no se ven afectados por las anotaciones, y la comparación y la comprobación de exhaustividad tampoco cambian en absoluto. Una anotación es solo un **metadato** adjunto a la definición.

#### Disposición en memoria (forma de pila)

Una enumeración etiquetada se almacena en memoria con la **forma de pila inline** (inline stack form): un pequeño objeto por valor, cuyo payload no se reparte por el montón:

```llvm
%tenum_<name> = type { iN tag, [K x i64] payload }   ; forma habitual
%tenum_<name> = type { iN tag }                      ; todas las variantes carecen de payload
```

- El ancho de `tag` depende del **número de variantes** y es un `u8/u16/u32/u64` completo: `<= 256` → `i8`, `<= 65536` → `i16`, `< 2^32` → `i32`, de lo contrario `i64`. Los discriminantes toman los valores `0, 1, 2...` según el orden de declaración.
- `payload` es una unión **compartida por todas las variantes** (cada variante escribe sus campos en el mismo almacenamiento); `K` es el número de palabras de 8 bytes que necesita la **variante más ancha**, **no la suma de todas las variantes**, y es como mínimo 1.
- Dentro de una variante, los campos se colocan con **alineación natural**, de modo que el desplazamiento de un campo no es necesariamente múltiplo de 8 (`(a char, b char, c char)` tiene desplazamientos 0 / 4 / 8). Las lecturas y escrituras se direccionan siempre por **desplazamiento de bytes** (`bitcast` a `i8*` y luego `getelementptr`), así que el desplazamiento con el que se escribe y el que se lee los determina la misma tabla de disposición.

El tamaño es `8 + 8K` (el `tag` se rellena hasta una palabra completa por la alineación de 8 bytes del arreglo); solo una enumeración sin payload en ninguna variante tiene el tamaño del tag puro:

| Caso | `K` | Tamaño |
| ---------------------------------------------- | ----------------- | ---------- |
| Todas las variantes carecen de payload | — (sin campo payload) | **1 byte** (`{ i8 }`; 2 / 4 / 8 si hay > 256 variantes) |
| La variante más ancha es `i64` / `i32` / `f64` / puntero | 1 | 16 bytes |
| La variante más ancha es `char` (4 bytes) / `bool` (1 byte) | 1 | 16 bytes |
| La variante más ancha es `(a char, b char, c char)` (12 bytes) | 2 | 24 bytes |
| La variante más ancha es `str` / `vec` / `[]T` (descriptor de 16 bytes) | 2 | 24 bytes |
| La variante más ancha es la estructura `T` | `ceil(sizeof(T) / 8)` | `8 + 8K` |

Es decir: **por defecto se empieza en 16 bytes** y solo crece cuando el payload de alguna variante es efectivamente más ancho: «16 bytes fijos por defecto» y «el tamaño varía con las variantes» son dos caras de la misma moneda. Los payload pequeños como `char` / `bool` solo determinan la alineación, no agrandan la unión: el ancho de `payload` es «la variante más ancha», no la suma de todas.

> La única fuente de la disposición es `src/mir/layout.go`: `tagWidth` determina `iN`, `enumVariantLayout` determina el desplazamiento y el tamaño de los campos de cada variante, y `enumUnionLayout` toma el máximo para obtener `K`. `hir2mir.fillEnumLayout` registra estos números en `TaggedEnumInfo` al construir la tabla, y el emisor solo los lee sin calcularlos: ambos no pueden llegar a respuestas distintas.

#### Las tres formas de `#{inline}`

`inline` es una anotación **booleana** con tres formas de escritura, y **su valor se respeta de verdad**:

| Forma | Significado |
| ----------------- | --------------------------- |
| `#{inline}` | Abreviatura, equivale a `inline=true` (una clave suelta se considera verdadera) |
| `#{inline=true}` | Explícitamente verdadero (también vale `#{inline=1}`) |
| `#{inline=false}` | Explícitamente falso (también vale `#{inline=0}`) |

Un valor no booleano es un **error de compilación** (`#{inline=foo}`, `#{inline='true'}`) y no se interpreta silenciosamente como verdadero: `ValidateFieldTags` (TraceID `fieldtag1`) lo informa tanto para los campos de estructura como para las definiciones de enum.

Escribir `#{inline=true}` encima de todo el enum es una **marca explícita** de la disposición de pila, que indica «esta enumeración usa disposición de pila»; el compilador la registra en `TaggedEnumInfo.Inline`. **La disposición por defecto actual ya es de pila**, así que añadir o no la anotación, y escribir `true` o `false`, da exactamente la misma disposición: la marca sirve para hacer explícita la intención y, si en el futuro cambia la disposición por defecto, como interruptor estable de elección.

> `#{inline}` escrito **encima de todo el enum** significa «esta enumeración usa disposición de pila»; escrito en **una sola variante** no cambia actualmente la disposición y solo se conserva como metadato de esa variante (a diferencia de los campos de estructura, véase la sección siguiente).

#### `#{inline}` en campos de estructura

A diferencia de los enum, `#{inline}` en **campos de estructura** **sí tiene efecto real**: decide directamente si el campo se **in-linea por valor** dentro de la estructura anfitriona (`%pt`) o se almacena como **puntero** (`ptr`).

```no
pt {
    x i64
    y i64
}

holder {
    inl pt #{inline=true}; por valor: la estructura crece directamente dentro de holder
    out pt #{inline=false}; puntero: el campo guarda un puntero a pt
    bare pt #{inline}; abreviatura, equivale a inline=true
}
```

| Forma | Disposición del campo |
| ---------------------------------------------- | --------------------------- |
| `#{inline}` / `#{inline=true}` / `#{inline=1}` | **Por valor**, in-lineado en el anfitrión (`%pt`) |
| `#{inline=false}` / `#{inline=0}` | **Puntero** (`ptr`) |
| Sin anotación | Depende de `NOLANG_FIELD_PTR` (véase la tabla siguiente) |
| `#{inline=foo}` / `#{inline='true'}` | **Error de compilación**: no es booleano |

**La disposición por defecto sin anotación la determina `NOLANG_FIELD_PTR`.** `mir.FieldPtrLayout` equivale a `os.Getenv("NOLANG_FIELD_PTR") != ""`, **desactivado por defecto**; activarlo inicia el «volteo de disposición de la Fase 1», que cambia el valor por defecto de los campos de estructura de por valor a puntero.

| Campo | Bandera desactivada (por defecto) | Bandera activada (`NOLANG_FIELD_PTR=1`) |
| ------------------------------ | --------- | ------------------------- |
| Sin anotación | `%pt` (por valor) | `ptr` (puntero) |
| `#{inline}` / `#{inline=true}` | `%pt` | `%pt` |
| `#{inline=false}` | `ptr` | `ptr` |

Es decir: **la anotación siempre surte efecto, sin importar la variable de entorno**; la bandera solo cambia el valor por defecto «cuando no se escribe anotación». Por eso, con la configuración por defecto, `#{inline=false}` es «pedir explícitamente un puntero», mientras que `#{inline=true}` solo vuelve a escribir una disposición que ya era así; una vez activado el volteo, los papeles se invierten.

El verificador tiene dos reglas (TraceID `fieldtag1`, `ValidateFieldTags`):

1. **Solo tiene sentido en campos de tipo estructura.** Los campos escalares / `str` / `vec` / arreglo / rebanada / map / option / puntero ya se almacenan siempre por valor, así que escribir `#{inline}` es como no decir nada y se informa como error; escribir `#{inline=false}` también es error, porque equivale a declarar una disposición contraria al comportamiento del compilador.
2. **La in-lineación por valor no puede formar ciclos.** La estructura in-lineada se almacena **dentro** del anfitrión, así que si coexisten `a { #{inline} b b }` y `b { #{inline} a a }` el tamaño es infinito y se debe informar como error (la autorreferencia directa es un caso degenerado). Solo `=true` crea un arista por valor, de modo que las estructuras mutuamente recursivas son válidas cuando **ambos lados escriben `#{inline=false}`**.
   > ⚠️ **«No escribir la anotación» no equivale a «no usar inline».** Con la configuración por defecto, no escribir la anotación significa in-lineación por valor, por lo que `a { x b }` junto con `b { y a }` (sin anotación en ninguno) **no es válido con la configuración por defecto** y falla con `opt: error: identified structure type 'b' is recursive` —y este error actualmente **no** lo informa el verificador, sino que estalla en la fase de verificación de LLVM. Para escribir tipos recursivos con la configuración por defecto **hay que** escribir explícitamente `#{inline=false}`. Solo con `NOLANG_FIELD_PTR=1` «no escribir» equivale a puntero.

#### Tipos recursivos: por qué se necesita `inline=false`

Una estructura que se in-linea a sí misma por valor tendría tamaño infinito, así que la **autorreferencia** no se puede almacenar por valor. Con la configuración por defecto (`NOLANG_FIELD_PTR` desactivado), los campos de estructura son por valor por defecto, de modo que la única forma de que `node` pueda referirse a sí misma es pedir explícitamente un puntero:

```no
node {
    v i64
    next node #{inline=false}; puntero: así node tiene un tamaño finito
}

n node
n.v = 1
n.next.v = 2
print(n.next.v); 2
```

Si se quita el `#{inline=false}` de arriba (o se escribe `#{inline=true}`), LLVM rechazará directamente el tipo: `identified structure type 'node' is recursive`.

Un campo puntero **posee** su pointee, y el compilador gestiona su ciclo de vida:

- el pointee se asigna de forma **perezoza en la primera escritura** (`@__nolang_get_<T>`, que incluye `malloc` + puesta a cero), no se asigna de una vez al construir el anfitrión;
- cuando el anfitrión sale del ámbito, `@__nolang_drop_<T>` lo **libera tras comprobar que no es nulo**;
- la copia por valor (`b = a`, pasar como argumento, escribir en un contenedor) **copia en profundidad** el pointee, de modo que dos anfitriones no comparten la misma memoria ni se liberan dos veces.

La prueba de extremo a extremo está en `tests/field-inline-annotation.no`.

### Referencia a valores de enumeración

**Regla: los valores de enumeración deben referenciarse con la forma cualificada `TipoEnum.valor`; no se pueden usar directamente valores simples.**  
Así se evitan colisiones de nombres y se impide que los paquetes externos utilicen directamente los valores concretos.

```no
#{fmt-skip}
; ❌ error: usar directamente un valor simple
kind = null
yes = e.is(io)
```
```

```no
; ✅ correcto: usar la forma cualificada
kind = json-kind.null
yes = e.is(code.io)
```

> Un tipo de enumeración se puede usar como tipo de campo de estructura, tipo de parámetro de función y tipo de valor de retorno.  
> Tanto dentro como fuera del módulo que define la enumeración, los valores se deben referenciar con la forma `TipoEnum.valor`.

## enter/leave

Los tipos que implementan las interfaces `enter` / `leave` se invocan automáticamente al entrar y salir del ámbito:

```no
file enter, leave {
    path str
}

file.enter = () {
    .open()
}

file.leave = () {
    .close()
}

read-file = () {

    ; f.enter() automático
    f = file {
        path: 'data.txt'
    }

    ; usar f
    ; f.leave() automático
    read(f)
}
```

### Tipos anulables (option)

Anteponer `?` a un tipo indica un tipo anulable:

Una variable de tipo anulable puede contener legalmente un valor nulo o un valor de error, y el compilador realiza las comprobaciones de nulidad correspondientes.

`?t` es una enumeración etiquetada integrada, cuyas variantes se definen en `src/std/global.no`:

```no
#{buildin}
option {
    ok(v t),
    nil,
    err(e err.err),
}
; tag 0: con valor
; tag 1: valor nulo
; tag 2: error (err.err, error estructurado de std)
```

El payload de la variante `err` **no es `str`**, sino la estructura `err` de `std/err` (`code` / `msg` / `file` / `line` / `col`). `err('msg', code)` es un constructor alias de `err.new('msg', code)`: el código de error `code` es un parámetro **obligatorio** (los valores por defecto de parámetros se eliminaron del lenguaje); cuando no se tenga un código concreto, pasar explícitamente `code.unknown`. Los campos de posición los inyecta automáticamente el compilador en el punto de construcción. `err('msg')`, con un solo argumento, no es este alias sino el constructor de la variante `err` de option (véase más abajo).

`#{buildin}` indica que la representación subyacente y la construcción las proporciona el runtime integrado (el tipo `%option` y los constructores `ok(...)`/`nil`/`err(...)`); la declaración de enumeración de `option` sirve solo para la comparación y la comprobación de exhaustividad. Véase «Enumeraciones etiquetadas» y «Enumeraciones etiquetadas integradas».

#### Disposición en memoria

`?T` **se tipa según el payload**: cada tipo de payload tiene su propio tipo LLVM, cuyo nombre se deriva del tipo de payload (aplicación inyectiva), de modo que «mismo tipo de payload ⇒ misma disposición» y `?i32` / `?i64` / `?bool` comparten un mismo tipo:

```llvm
%option_<key> = type { i8 tag, [K x i64] payload }   ; in-lineado
%option_<key> = type { i8 tag, [1 x i64] payload }   ; empaquetado (el payload guarda un puntero i8*)
```

- `tag` siempre ocupa **1 byte**: `option` solo tiene 3 variantes (`0=ok`, `1=nil`, `2=err`), que caen en el tramo «`<= 256` variantes → `i8`». La regla de anchura del tag de las enumeraciones está en «Disposición en memoria (forma de pila)».
- Después del tag viene el **relleno de alineación** (un tag de 1 byte → 7 bytes de relleno) y solo después el payload.
- El tamaño del payload `K` lo determina el tamaño real de `T` y se redondea hacia arriba a múltiplo de 8, **no es una ranura fija**: `?i64` ocupa 16 bytes (8 + 8), `?str` 24 bytes (8 + 16) y `?txt` 264 bytes (8 + 256).
- Un payload que supera el umbral pasa a **empaquetarse en el montón**: `payload` guarda solo un puntero de 8 bytes y toda la opción ocupa 16 bytes.

El umbral `option-inline-threshold` se mide en bytes de **toda la estructura option** (por defecto **264** = 1 byte de tag + 7 bytes de relleno + 256 bytes de payload; mínimo **16**): la condición para in-linear el payload es `8 + roundUp8(sizeof(T)) <= umbral`. Por eso, por defecto, «se in-linea si el payload no supera los 256 bytes», y `?txt` (búfer inline de 256 bytes) cae justo dentro del límite.

| `T` de `?T` | ¿In-lineado? | Tamaño de `%option_<key>` |
| --- | --- | --- |
| `i64` / `i32` / `bool` / `char` / `f64` / puntero | Sí | 16 bytes |
| `str` / `[]T` / `vec` (descriptor de 16 bytes) | Sí | 24 bytes |
| `txt` (búfer inline de 256 bytes) | Sí | 264 bytes (justo en el límite por defecto) |
| Estructura / arreglo de tamaño > 256 bytes | No | 16 bytes (payload en el montón) |
| Payload autorreferencial (`node { next ?node }`) | No | 16 bytes (la autorreferencia solo se rompe con un puntero) |

> **El payload de `err` siempre se empaqueta**: la variante `err` almacena un puntero a la estructura `err` de `std/err` y no ocupa el presupuesto de payload de arriba. Por eso `?str` ocupa 24 bytes con una cadena in-lineada en `ok`, y también la misma estructura de 24 bytes en `err`: solo que la palabra de `payload` guarda un puntero al objeto de error. Esta regla evita que «un mismo tipo `?T`, dos variantes y dos payloads» necesiten espacio adicional.

> **In-linear y empaquetado solo cambian «dónde se guarda», no la propiedad**: en ambas rutas, cuando ocurre una **copia** se crea una copia nueva de la parte en el montón del payload estructurado (los campos puntero se recursan, los búferes `str` / `[]T` propiedad in-lineada se clonan), así que la semántica de valor de `?T` no depende de si está in-lineada: el mismo programa debe producir exactamente la misma salida con cualquier `option-inline-threshold`. Cambiar de comportamiento por un byte de diferencia en el umbral sería un defecto del compilador, no una característica. (Las asignaciones de movimiento puro no clonan; la opción toma el montón de la fuente.)

> La única fuente de la disposición es `src/mir/layout.go`: `optLayoutOfPayload` determina `K` y si se empaqueta, y `optionPayloadOffset` (8) es el desplazamiento fijo en bytes del payload. Tanto `hir2mir` al plegar constantes `?T` de nivel de módulo como codegen al emitir lecturas y escrituras consultan la misma función.

#### Propagación de `?=` entre payloads de distinto tipo

`?=` copia primero la opción que está en la **raíz** de la cadena del valor derecho en el parámetro de resultado de la opción, de modo que `nil` / `err` se propagan directamente y la ruta de éxito se sobrescribe después con el brazo `ok`:

```no
pick = (p pool) (out ?i64) {
    out ?= p.nodes[0].kind; la raíz es ?node, la ranura de resultado es ?i64
}
```

Por tanto, los tipos de payload de ambos lados **pueden ser distintos**, y esto es una frontera ABI real (`?node` ocupa 112 bytes, `?i64` 16 bytes), no dos formas de escribir la misma disposición. Entre tipos solo se pueden llevar dos cosas:

- `tag`: el byte 0 de cada opción;
- `err`: la variante `err` es una propiedad de la opción, independiente del payload, y siempre se empaqueta, de modo que en ambos lados la primera palabra de la zona de payload es un puntero de 8 bytes a la caja (la caja se **copia**, no se comparte, porque ambos lados la liberan).

El payload `ok` **no se transporta entre tipos**: la zona de payload del destino se pone primero a cero y el valor de éxito lo vuelve a escribir siempre el brazo `ok`. La implementación está en `@__nolang_opt_prop_<dst>__<src>` (`optPropagateState` en `src/mir/codegen.go`) y aparece solo en asignaciones de propagación generadas por el compilador como `?=`.

```no
o ?i64
o = nil; poner a vacío
o = 42; poner con valor
o = err('msg'); poner a error

nullableValue ?[]str
nullableString ?str

; modificar el tipo anulable
nullableString = 'test'

; establecer un error
nullableString = err('some error')

; se puede comprobar con match
x: {
    err -> log(it.msg()); it es err.err, el mensaje se obtiene con .msg()

    nil -> log('nil')

    -> do-right-thing(it)
}

; desempaquetado forzado
; cancelar la implementación
; !x.say()
```

### Guía de estilo: usar la opción ?t en lugar de (val, ok)

Cuando una función puede fallar o devolver un valor vacío, **se debe preferir el tipo de opción `?t`** en lugar del patrón de doble retorno `(val t, ok bool)`.

`?t` es una enumeración etiquetada con tres estados: `ok` (con valor), `nil` (valor vacío) y `err` (error). Los valores normales se vinculan implícitamente; se usa `nil` cuando la operación simplemente no encuentra un valor y `err(...)` cuando encuentra un error real.

```no
#{fmt-skip}
; ❌ no recomendado: patrón de doble retorno
stack.pop = () (val i64, ok bool) {
    .n == 0 -> return
    val = .data[.n]
    ok = true
}
```
```

```no
; ✅ recomendado: tipo opción (nil significa vacío, err significa error)
stack.pop = () (val ?i64) {
    .n == 0 -> {
        val = nil
        return
    }
    val = .data[.n]
}

; ✅ devolver un error
file.read = () (data ?str) {
    .fd < 0 -> {
        data = err('file not open')
        return
    }

    ; ... leer datos
    data = buf
}
```

Usar match para desempaquetar la opción:

```no
val = s.pop()
val: {
    nil -> print('empty')

    err -> print(it.msg()); it = el error estructurado err.err

    -> print(it); it = el valor extraído
}
```

## Desbordamiento de operaciones enteras (anotación `#{overflow}`)

Nolang no admite pánico: cualquier operación que pueda desbordarse debe manejarse con `option` o con una anotación explícita, y nunca lanzará una excepción de desbordamiento entero en tiempo de ejecución. El comportamiento ante el desbordamiento de las siguientes **operaciones aritméticas enteras** lo controla la anotación `#{overflow = ...}`:

- **`+` `-` `*`** con signo y sin signo: se aplican cuando ambos operandos son enteros (incluidos literales `int`);
- **`/`** con signo: solo desborda en el caso `INT_MIN / -1` (la división sin signo `a/b ≤ a` nunca desborda y no se aplica).

> Los enteros sin signo (`u8`/`u16`/`u32`/`u64`/`u128`) en `+ - *` **siguen exactamente las mismas reglas** que los enteros con signo: sin anotación, también devuelven por defecto `option<int>` y el desbordamiento da `err`. Esta es una decisión de diseño deliberada: «si no se puede demostrar en tiempo de compilación que el desbordamiento es imposible, se aplica la misma regla».

### Caso especial: aritmética de `byte` y `char`

`byte` y `char` son un **caso especial** en la semántica de desbordamiento: su aritmética **no produce opciones** y no se rige por la anotación `#{overflow}`.

- **`byte` (u8)**: al desbordarse `+ - *` **se envuelve silenciosamente en complemento a dos** (`250 + 10` → `4`, `0 - 1` → `255`). `byte` está pensado para escenarios que «admiten naturalmente el envoltorio», como búferes de bytes y codificación/decodificación hex, con la misma semántica que `#{overflow = wrap}` para hash/contadores, pero integrado y sin necesidad de anotación.
- **`char` (punto de código Unicode)**: `+ -` es **aritmética de puntos de código** y, cuando el resultado sale del rango válido (`< 0` o `> 0x10FFFF`), da **`-1`** (centinela de punto de código inválido): no da `err` ni se envuelve.

**Motivo de diseño**: `i8`–`i128` / `u8`–`u128` se usan normalmente para **valores de negocio reales** (importes, conteos, capacidades), donde el envoltorio silencioso ocultaría errores de desbordamiento; por eso el valor por defecto es `err` y se exige una elección explícita. En cambio, los usos típicos de `byte` / `char` (flujos de bytes, recorrido de puntos de código) admiten naturalmente (e incluso esperan) el envoltorio, y usar opciones solo añadiría ruido de desempaquetado. Si se necesita detectar el desbordamiento en operaciones con `byte` / `char`, convertir primero a un tipo más ancho (p. ej. `b.to-i64()`) y luego calcular.

| Modo | Anotación | Al desbordarse | Tipo de resultado | Escenarios de aplicación |
| ----------- | ------------------------ | --------------------------- | ------ | ------------------ |
| **Por defecto (sin anotar)** | — | desbordamiento → `err`, normal → `ok(value)` | `?int` | operaciones en las que hay que detectar el desbordamiento |
| **Envoltorio** | `#{overflow = wrap}` | envoltorio silencioso en complemento a dos | `?int` | hash, criptografía, contadores, etc., donde el envoltorio es semánticamente aceptable |
| **Puesta a cero** | `#{overflow = clamp0}` | el desbordamiento (por exceso o por defecto) da `0` | `?int` | semánticas en las que la diferencia no debe ser negativa (p. ej. cantidades restantes) |
| **Fijación al mínimo** | `#{overflow = min}` | se fija al valor mínimo del tipo | `?int` | límite inferior de contadores, protección de índices |
| **Fijación al máximo** | `#{overflow = max}` | se fija al valor máximo del tipo | `?int` | límite superior de capacidad, acumulación saturada |
| **Saturación** | `#{overflow = saturate}` | exceso → máximo, defecto → mínimo | `?int` | saturación de señales/colores, etc. |

> ⚠️ **La anotación no desempaqueta** (D1): `#{overflow = …}` **solo decide «cómo se obtiene el valor en caso de fallo»**; el tipo de resultado siempre es `?T`. Para llevar el resultado de vuelta a un tipo simple solo se puede consumir con match o con `?=`; las diferencias y la elección entre las dos formas de consumo (`?=` + match de bloque / match de valor) están en «Compensaciones entre match de bloque y match de valor». Anotar explícitamente un tipo simple (`w i64 = a - b  #{overflow=wrap}`) **no es** una salida: en la práctica sigue dando `15w45dqk` (cannot assign `?i8` value to `i8` variable).

Además, todos los modos admiten la **forma con prefijo de tipo**, que especifica con precisión el límite de saturación de un tipo estrecho concreto, p. ej. `#{overflow = u8-max}`, `#{overflow = i8-min}`, `#{overflow = u16-saturate}`.

> **Granularidad de la anotación**: `#{overflow = ...}` es una **anotación de línea**: solo afecta a **la sentencia inmediatamente posterior**. Esta es la **única forma permitida por diseño** (para evitar abusos): tanto la semántica de ejecución (codegen) como el error fatal `ovfhndld` solo la reconocen a ella, y solo cambia «cómo se obtiene el valor al desbordarse», no el tipo; ampliarla a nivel de bloque o de función haría que la semántica de todo un bloque de operaciones dependiera de una anotación situada a muchas líneas de distancia. El motivo se explica en [«Ámbito de anotaciones y comentarios»](annotation-scope.md) §4.1.
>
> ⛔ **No es válido colocarla encima de una definición de función, al inicio de un bloque o al inicio de un archivo**. La única laguna actual es que una anotación a nivel de función hace que el lint `ovf-int-default` omita toda la función (la lista parece «limpia») mientras las operaciones sin anotar dentro del cuerpo siguen disparando `ovfhndld`; una anotación a nivel de bloque hace que el lint la herede hacia abajo y calle, mientras que codegen no la reconoce en absoluto. Anote sentencia por sentencia; `no fmt --fix=overflow` genera justamente esta forma por sentencia.
>
> El modelo completo (L0 módulo / L1 declaración / L2 bloque / L3 línea individual, y qué claves admiten qué niveles) está en [«Ámbito de anotaciones y comentarios»](annotation-scope.md); el §7 de esa página enumera el código existente y las lagunas, y el §8 el orden de endurecimiento.

> **Corrección rápida LSP**: el editor (nolang-lsp) emite una **pista** para las operaciones enteras sin anotar (`nolang-overflow`, trace id `ovfhndld`) y ofrece cinco correcciones rápidas: **Add `#{overflow = wrap}`** / **`clamp0`** / **`min`** / **`max`** / **`saturate`**; insertan automáticamente la anotación correspondiente encima de la sentencia de la operación, con la misma indentación que esa línea, para especificar el comportamiento ante el desbordamiento (el resultado sigue siendo `?T` y hay que consumirlo con match / `?=`). El equivalente en línea de comandos es `no fmt --fix=overflow` (véase más abajo).

### Por defecto: devolver `option<int>`

Sin anotación, el tipo de resultado de `a - b` es `option<int>`. El receptor debe ser `?T` (un tipo anulable) y hay que usar match para desestructurar las tres ramas `err` / `nil` / `ok`:

```nolang
main = () {
    x i64 = 100
    d ?i64 = x - 1; por defecto: devuelve option<i64>
    d: {
        err -> print(-1); desbordamiento (p. ej. subdesbordamiento de x - 2^63)

        nil -> print(0)

        -> print(1); ok(value): resultado normal
    }
}
main()
```

> El campo `data` de `%option` es `i64`; los resultados de tipos estrechos (`i8`/`i16`/`i32`) se extienden primero con signo (sext) antes de guardarse y se truncan de nuevo a su anchura original al desempaquetar.

### wrap: envoltorio silencioso

```nolang
#{overflow=wrap}
sub-wrap = (a i64, b i64) (r i64) {
    r = a - b; al desbordarse, envoltorio silencioso en complemento a dos; r es i64 simple (tipo explícito)
}
```

A nivel de sentencia:

```nolang
x i64 = -9223372036854775807

#{overflow=wrap}
w i64 = x - 2; subdesbordamiento con envoltorio → 9223372036854775807
print(w)
```

### clamp0: poner el desbordamiento a cero

```nolang
x i64 = -9223372036854775807

#{overflow=clamp0}
c i64 = x - 2; subdesbordamiento → 0 (el desbordamiento por exceso también da cero)
print(c)
```

### min / max / saturate: fijación con saturación

Los tres modos «fijan» el resultado del desbordamiento dentro del intervalo representable del tipo, en lugar de envolverlo o ponerlo a cero:

```nolang
#{overflow=min}
floor = (a i64, b i64) (r i64) {
    r = a - b; subdesbordamiento → valor mínimo de i64; desbordamiento por exceso → valor máximo de i64
}

#{overflow=max}
cap = (a i64, b i64) (r i64) {
    r = a * b; desbordamiento por exceso → valor máximo de i64; subdesbordamiento → valor mínimo de i64
}

#{overflow=saturate}
sat = (a i64, b i64) (r i64) {
    r = a + b; exceso→máximo, defecto→mínimo
}
```

La forma con prefijo de tipo permite controlar con precisión el límite de saturación de tipos estrechos (una anotación a nivel de función se aplica a todas las operaciones del cuerpo con el tipo correspondiente):

```nolang
#{overflow=max}
inc = (x u8) (r u8) {
    r = x + 1; si x = 255, desbordamiento por exceso → 255 (en lugar de envolver a 0)
}

#{overflow=min}
dec = (x i8) (r i8) {
    r = x - 1; si x = -128, subdesbordamiento → -128 (en lugar de envolver a 127)
}
```

### Aplicación por el compilador

Las operaciones enteras sin anotar devuelven `option<int>` por defecto, de modo que «no manejarlas» se detecta automáticamente. **La condición de «no manejado» se evalúa de forma paralela**: si se cumple cualquiera de las siguientes, no hay error:

- **Propagación con `?=`** (requiere que la función tenga un parámetro de resultado de opción);
- **Captura en el lugar**: vincular con `=` a una variable **nueva** (la variable se infiere automáticamente como `?T`) o declarar explícitamente `?T` (p. ej. `x ?i64 = a + b`);
- **Descarte explícito `_ = expr`** (la evaluación sigue la ruta segura y el error se ignora también);
- **Añadir una anotación** `#{overflow = wrap}` / `clamp0` / `min` / `max` / `saturate` (o una forma con prefijo de tipo como `u8-max`, `i8-min`), declarando que se acepta la semántica de envoltorio/saturación.

Solo si no se cumple ninguna de las cuatro se informa el error (`ovfhndld`).

> **La aritmética pura no es una fuente de error**: `+ - * <<` y la negación no disparan la captura por sí solas (si no, toda la aritmética se convertiría en opciones y la biblioteca estándar estallaría). Por eso `d = x - 1` (sin `/` `%`, sin indexación segura y sin operandos de opción) sigue dando error: añada una anotación o escriba `d ?i64 = x - 1`.
>
> **Las variables existentes no cambian de tipo**: la captura solo afecta a destinos «declarados por primera vez en esta sentencia». Si la variable ya existe (p. ej. primero `d = 0` y después `d = x - 1`, o primero `v i64 = 0` y después `v = arr[i]`), el compilador no la convierte en opción en el lugar, sino que informa del error y le pide que elija explícitamente la semántica.

Además, hay dos errores fatales que no se ven afectados por las exenciones anteriores:

- Si una operación entera sin anotar se asigna a una variable **`int` simple explícita**, el compilador informa del error (`cannot assign ?i64 value to i64 variable`) y le obliga a añadir `#{overflow = wrap}` / `#{overflow = clamp0}` / `#{overflow = min}` / `#{overflow = max}` / `#{overflow = saturate}` (o una forma con prefijo de tipo) o a usar un `?T` con nombre.
- Si una operación entera sin anotar se asigna a una variable **ya existente** cuyo tipo no es opción (`d = x - 1` después de `d = 0`), el compilador informa del error y sugiere añadir la anotación o declarar `?i64`, para evitar generar IR inválido.

Reglas adicionales:

- Las operaciones con enteros sin signo (`u8`/`u16`/`u32`/`u64`/`u128`) **se rigen por el mismo mecanismo**: sin anotación, `+ - *` devuelve por defecto `option<int>` y el desbordamiento da `err`, igual que con los enteros con signo.
- Las operaciones con `i128` no admiten la ruta `option` (el campo data solo es `i64`) y se degradan uniformemente a envoltorio silencioso para preservar la corrección.

> **¿Por qué por defecto option y no wrap?** El envoltorio silencioso ocultaría errores de desbordamiento; devolver `option` por defecto obliga a quien llama a manejar el desbordamiento explícitamente (o a anotar `wrap` por iniciativa propia para indicar «acepto la semántica de envoltorio»), convirtiendo «si se permite el desbordamiento» en una decisión de diseño visible.

### División por cero (division by zero)

Un divisor `0` en `/` y `%` enteros es **comportamiento no definido (UB)** y es **algo independiente del desbordamiento**: ninguna anotación puede eximirlo:

- **Determinable en tiempo de compilación**: el divisor es una **constante literal 0** (`10 / 0`, `a % 0`, `x / 0`) ⇒ **error fatal** (trace id `div-by-zero`). `#{overflow = wrap}` no lo exime (esa anotación rige el desbordamiento, no la división por cero).
- **Solo conocido en tiempo de ejecución**: el divisor proviene de una variable/entrada externa ⇒ el compilador inserta una comprobación de cero **en ambas rutas**; la única diferencia es «cómo se informa»:

| Tipo de resultado | Condición de disparo | Cuando el divisor es 0 |
| ----------- | ----------------------- | ------------------------------------------------------------------------------ |
| `?int` (por defecto) | sentencia sin anotar | tag de opción = `err`, manejado con `?=` / match |
| `int` simple | sentencia con `#{overflow = ...}` | el resultado se fuerza a `0` y se imprime `nolang: integer division or remainder by zero ...` en **stderr** |

```nolang
safe = (a i64, b i64) (r ?i64) {
    r = a / b; b == 0 → err (quien llama debe manejarlo)
}

#{overflow=wrap}
fast = (a i64, b i64) (r i64) {
    r = a / b; b == 0 → 0 e imprime una línea de diagnóstico en stderr (ya no es UB de sdiv directo)
}
```

> **Por qué `#{overflow = wrap}` no puede eximir la guarda de divisor cero**: lo que cambia es el **comportamiento ante el desbordamiento** (el tipo de resultado sigue siendo siempre `?int`), no la seguridad del divisor. Si se le permitiera eximir, el compilador emitiría un `sdiv` directo y el optimizador tendría derecho a suponer que el divisor nunca es 0 (plegando `a / 0` a cualquier valor), mientras que `idiv` de x86-64 se bloquea con #DE: UB y sin ningún diagnóstico. Por eso la guarda de divisor cero existe **independientemente de la anotación**; la ruta simple imprime una línea extra en stderr justamente porque no hay ninguna opción que quien llama pueda comprobar.
>
> **La misma guarda cubre también `sdiv (INT_MIN, -1)`** (resultado no representable, también UB; en x86-64 provoca SIGFPE). La ruta de opción lo incorpora a `err`; la ruta simple toma el valor de envoltorio en complemento a dos (`INT_MIN / -1 → INT_MIN`, `INT_MIN % -1 → 0`).

La división en coma flotante no se rige por esto: `1.0 / 0.0` está definida por IEEE-754 como `inf` / `nan`, y no es UB.

### Las operaciones sin anotar se informan como error (`ovf-int-default`)

`no vet` informa las operaciones enteras **sin anotar** como una **pista** (trace id `ovfhndld`, HINT). El motivo: el `?T` por defecto no es visible para el usuario, que debe estar informado: puede añadir `#{overflow = ...}` para especificar el comportamiento ante el desbordamiento, o consumirlo con `?T` + `?=` / match / un tipo simple explícito. Lo que de verdad debería bloquear la compilación es `optunuse` (una opción que no se propaga ni se desempaqueta).

### Corrección automática: `no fmt --fix=overflow`

No hace falta escribir a mano las anotaciones en lote; use el parámetro de categoría de corrección de `no fmt`:

```bash
no fmt --fix=overflow -w src/std          # 就地修復整個目錄
no fmt --fix=overflow -d src/std/str.no   # 只印 diff，不寫檔
no fmt --fix=overflow src/std/str.no      # 印出修復後的內容
```

La corrección es **precisa y no contaminante**: inserta `#{overflow=wrap}` solo encima de la sentencia de la operación que el lint ha informado realmente, sin modificar otras sentencias, sin reordenar el formato y sin tocar anotaciones existentes, y es idempotente al repetirla (lo ya anotado no cambia). Esto difiere de ejecutar directamente `no fmt -w`, que es un reformateo completo.

> Después de corregir, vuelva a ejecutar `no vet` para confirmar que se ha puesto a cero. En la gran mayoría de los casos se vacía de una sola vez (`src/std` pasó de 4728 a 0). La única forma excepcional conocida: el informe recae sobre la **condición** de un brazo de match y el cuerpo de ese brazo no tiene ninguna sentencia que se pueda anotar: una anotación de línea solo afecta a la primera sentencia posterior y no cubre la operación de la condición del brazo, por lo que `no vet` lo vuelve a informar. En ese caso, extraiga primero la operación a una sentencia independiente y luego anótela.

### Corrección automática: anotaciones de tipo redundantes

Cuando la **anotación de tipo de una declaración de variable es exactamente igual al tipo inferido del valor de la derecha**, la anotación es redundante (`no vet` lo informa como pista `tcpoxtfd`, `type annotation '<T>' can be omitted (inferred from value)`).

- **`no fmt` ya las elimina por defecto**: `no fmt -w` elimina las anotaciones redundantes al formatear (además de hacer el reformateo normal), `no fmt -d` previsualiza este cambio y `-w` lo escribe.

```bash
no fmt -w main.no          # 格式化並去掉冗餘標註
no fmt -d main.no          # 只印 diff（含標註移除）
no fmt -w src/             # 整個目錄
```

- **`--fix=redundant` (quirúrgico, solo toca anotaciones)**: si solo se quieren eliminar las anotaciones redundantes sin un reformateo completo (p. ej. en `src/std`, porque `no fmt` perdería `#{index-out}`), use esta categoría: solo borra anotaciones, no reordena el formato:

```bash
no fmt --fix=redundant -w src/std          # 就地移除整個目錄的冗餘標註（不重排版）
no fmt --fix=redundant -d src/std/str.no   # 只印 diff，不寫檔
no fmt --fix=redundant src/std/str.no      # 印出修復後的內容
```

```nolang
; antes de corregir
x str = ''
b i64 = 5
m [str]i64 = make-map()

; después de corregir (anotación == tipo inferido, eliminada)
x = ''
b = 5
m = make-map()
```

Puntos clave de las reglas:

- Solo se elimina cuando **el tipo anotado == el tipo inferido** (p. ej. `str`/`i64`/`[str]i64` coinciden con su valor inferido).
- Los casos **convertibles implícitamente pero no iguales** **no** se eliminan: conservar la anotación es lo que da sentido a la conversión. Por ejemplo `c i16 = 5` (`5` se infiere como `i64`, distinto de `i16`), `p ?i64 = nil` y `last txt = ''` (`txt` y `str` se pueden convertir implícitamente entre sí, pero son tipos distintos).
- Las formas **sin anotación de tipo** (como `d = 'x'`) no se informan y, por tanto, no se tocan.
- **Las anotaciones de arreglo de longitud fija `[N]T` / `[?]T` se conservan siempre** (nunca se consideran redundantes). `[N]` no es una simple pista de tipo, sino un interruptor que decide la **forma de representación**: solo si se escribe, el literal `[...]` de la derecha se analiza como un arreglo de pila de longitud fija, y solo entonces el resultado inferido «resulta igual» a la anotación: es un razonamiento circular creado por la propia anotación. Al borrarla, la misma línea se reanaliza como una rebanada `[]T` crecible en el montón y la semántica cambia: en `fixed [3]char = ["x", "y", "z"]`, `.push()` no es válido (`.len()` siempre es 3), mientras que al degradarse a `fixed = ["x", "y", "z"]`, `.push()` hace que `.len()` sea 4. La anotación de rebanada `[]T` no está incluida: al borrarla sigue siendo una rebanada, así que sí es realmente redundante.
- **Los valores con literales hexadecimales conservan siempre la anotación** (excepción conservadora). La inferencia de tipos de `no vet` usa la heurística de «inferir `byte`» para `0xNN`, distinta del comportamiento real del compilador («los literales enteros sin anotar son `i64` por defecto»), por lo que `IP-TBL [64]byte = [0x3a, ...]` y `s []byte = [0x50, 0x4b]` se juzgan erróneamente redundantes; al eliminarlas, los literales se degradan a elementos `i64` y producen errores de tipo como `expected '[]byte', got '[]i64'`. Por eso, si el texto de la expresión del valor contiene `0x`/`0X`, no se emite la pista ni se produce la eliminación.
- **Cuando el valor derecho es una lectura de campo se conserva siempre la anotación** (excepción conservadora). En escrituras como `i i64 = .pos`, la anotación no es una simple pista de tipo, sino el interruptor que decide si la aritmética entera posterior produce `?T`: si se infiere el tipo del campo, `i + N` deja de ser una «operación con signo» y ya no devuelve `?i64`, el brazo `ok` del desempaquetado en dos fases nunca se alcanza y el contador del bucle no avanza. El caso real `parser.looks-like-map-entry` de `src/std/yaml.no`: borrar `i64` hace que `yaml.parse('abc')` entre en un bucle infinito. Por eso las anotaciones en `x = .field` y `x = obj.field` (incluso con paréntesis o prefijos) no emiten `tcpoxtfd` ni se eliminan con `no fmt` por defecto ni con `--fix=redundant`.

Tanto con `no fmt` por defecto como con `--fix=redundant`, la eliminación solo borra la anotación de tipo informada y su espacio previo, y es idempotente al repetirla (lo ya eliminado no cambia). Después de corregir se recomienda ejecutar de nuevo `no vet` para confirmar que `tcpoxtfd` se ha puesto a cero.

### Corrección automática: consumir opciones (`--fix=unwrap`)

Bajo la regla unificada, «generar una opción y nunca consumirla» es el único diagnóstico que debería bloquear la compilación (trace `optunuse`). Para convertirlo en forma de consumo explícito en lote, use `--fix=unwrap`:

```bash
no fmt --fix=unwrap -w src/std            # 就地修復整個目錄
no fmt --fix=unwrap -d src/std/str.no     # 只印 diff，不寫檔
no fmt --fix=unwrap src/std/str.no        # 印出修復後的內容
```

Forma de la corrección (véase «Indexación segura»): **si la función que contiene la sentencia ya tiene un parámetro de resultado `?T`** → propagar (`x ?= <expr>`); en caso contrario → renombrar + desempaquetar con match:

```nolang
; antes de corregir
x = a + b

; después de corregir (la función contenedora tiene un parámetro de resultado ?T)
x ?= a + b

; después de corregir (en caso contrario, forma estándar de 4 líneas; brazo de respaldo vacío = valor cero)
x-opt = a + b
x = x-opt: {
    ok -> it

    ->
}
```

Como las demás categorías de corrección: sustitución textual quirúrgica, sin cambiar el formato, sin tocar las anotaciones `#{overflow=…}` / `#{index-out=…}` situadas encima de la sentencia (siguen encargándose de «cómo se obtiene el valor en caso de fallo»), e idempotente al repetirla. Después de corregir, vuelva a ejecutar `no vet` para confirmar que `optunuse` se ha puesto a cero.

### Vinculación de anotaciones y fusión en una sola línea

Las anotaciones `#{...}` **se vinculan al nodo**, de modo que una misma sentencia puede llevar varias claves a la vez; varias anotaciones siempre se **fusionan en una sola línea**:

```nolang
#{fmt-skip}
i <- [0..16): {

    #{index-out=0, overflow=wrap}
    buf[base + i] = data[i]
}
```

La sentencia de arriba necesita a la vez dos semánticas: «tomar el valor por defecto si se sale de límites» (`index-out`) y «envolver al desbordarse» (`overflow`). Escribirlas en dos líneas da el mismo resultado de análisis, pero `no fmt --fix=overflow` produce la forma fusionada de una sola línea.

> **Ámbito de la anotación de línea**: `#{overflow = ...}` solo afecta a **la sentencia inmediatamente posterior**. Si entre ambas hay una anotación sin relación, sigue aplicándose correctamente a esa sentencia. Las escrituras con indexación segura (`arr[base + i] = v`, `.buf[.pos + i] = v`) se desugarean a forma match y `#{overflow = wrap}` se lleva también a los nodos posteriores al desugar, de modo que la expresión de índice queda igualmente bajo la anotación y no se informa erróneamente como sin anotar.

### Propagación de errores (operador `?=`)

`?=` es una **forma abreviada** de desempaquetar una opción y propagar el error hacia arriba. **No es necesario que la función devuelva una opción**: basta con que se dé una de las dos situaciones siguientes, así que una función cuyo valor de retorno no es una opción también puede escribir `y ?= x + 1` si envuelve el cuerpo con un match de bloque.

**Sintaxis:**

```no
v ?= expr
```

**Semántica:**

- Si `expr` devuelve `ok(value)`, `v` recibe el valor interno desempaquetado (desempaquetado automático).
- Si `expr` devuelve `nil` o `err`:
  - si la función tiene un parámetro de resultado `?T` → se escribe esa opción en el parámetro de resultado y se hace un `return` anticipado (propagación automática);
  - si la sentencia está dentro del cuerpo de un **match de bloque a nivel de sentencia** `{ ... }: { ... }` → lo captura el match de bloque **más cercano**, **se detiene todo el cuerpo** y los brazos se ejecutan una sola vez (**no** es un `return`), sin que la función necesite un parámetro de resultado `?T`.

**Condiciones de uso (basta una de las dos):**

1. La función contenedora tiene un parámetro de resultado de tipo opción;
2. la sentencia está dentro del cuerpo de un match de bloque a nivel de sentencia `{ ... }: { ... }` (véase «Las tres formas del sujeto de match»).

Si no se da ninguna de las dos, el compilador informa del error. Los dos brazos `err -> _` / `-> _` pueden tragarse el error propagado en el lugar y son el respaldo estándar para «querer usar la forma abreviada sin un parámetro de resultado `?T`»:
```no
handle = (x i64) {
    {
        y ?= x + 1
        print(1)
    }: {
        err -> _

        -> _
    }
}
```

**No hace falta asignar antes el parámetro de resultado**: el prólogo de la función no hace inicialización a cero, pero al hacer return los parámetros de salida no asignados explícitamente se rellenan automáticamente con cero (enteros → `0`, `str` / estructuras → valor cero, opción → `nil`), así que en el ejemplo no se escribe la plantilla `result = nil` y basta con escribir la asignación de la ruta de éxito.

**Ejemplo:**

```no
; leer y procesar un archivo línea por línea: usar ?= para simplificar la propagación de errores
process-file = (path str) (result ?str) {
    f ?= open(path); si falla, automáticamente result = f; return
    line ?= f.read-line(); EOF o error se propaga automáticamente
    result = line
}
```

Forma expandida equivalente (cadena de match generada automáticamente por el compilador):

```no
#{fmt-skip}
process-file = (path str) (result ?str) {
    result = nil
    __tmp = open(path)
    __tmp: {
        nil || err -> {
            result = __tmp
            return
        }
        -> f = it
    }
    __tmp2 = f.read-line()
    __tmp2: {
        nil || err -> {
            result = __tmp2
            return
        }
        -> line = it
    }
    result = line
}
```

**Uso encadenado:** se pueden encadenar varios `?=` para lograr una propagación de errores en forma de tubería:

```no
pipeline = (input str) (result ?str) {
    a ?= step1(input); si falla, propagar
    b ?= step2(a); si falla, propagar
    result = b
}
```

**Aritmética directa**: el lado derecho de `?=` no se limita a llamadas de función: cualquier operación entera que devuelva `option` (como `a - b`, `a + b`, `a * b` sin anotar) se puede usar directamente con `?=` para propagar el error de desbordamiento, sin necesidad de vincularla antes a una variable intermedia:

```no
; al desbordarse, automáticamente result = err; return; en caso normal, v se desempaqueta al valor interno
sub-safe = (a i64, b i64) (result ?i64) {
    v ?= a - b; subdesbordamiento/desbordamiento → err propagado automáticamente; si no, v = i64 interno
    result = v
}

add-safe = (a i64, b i64) (result ?i64) {
    v ?= a + b; igual que arriba, aplica a + * y a / con signo
    result = v
}
```

Expansión equivalente (generada automáticamente por el compilador):

```no
#{fmt-skip}
sub-safe = (a i64, b i64) (result ?i64) {
    result = nil
    __unwrap = a - b            ; option<i64>
    __unwrap: {
        nil -> {
            result = __unwrap
            return
        }
        err -> {
            result = __unwrap
            return
        }
        -> v = it               ; ok(value): desempaquetar el valor interno
    }
    result = v
}
```

**Lado derecho compuesto (compound RHS)**: el lado derecho de `?=` puede ser una expresión compuesta con varios operandos de opción. Cada operando de opción se desempaqueta automáticamente y se le inyecta una guarda de propagación: en cuanto cualquier operando es `nil` / `err`, el parámetro de resultado de opción de la función actual se establece a ese valor y se hace `return` (propagación hacia arriba); solo si todos tienen éxito se asigna el valor interno al lado izquierdo:

```no
sum3 = () (result ?i64) {
    a ?i64 = ok(10)
    b ?i64 = ok(20)
    c ?i64 = ok(30)
    total ?= a + b + c; cada operando de opción se desempaqueta y propaga automáticamente
    result = total
}

comp = () (result ?i64) {
    a ?i64 = ok(4)
    b ?i64 = ok(6)
    v ?= dbl(a + b); a + b puede ser una opción, también se desempaqueta y propaga automáticamente
    result = v
}
```

> Solo se desempaquetan automáticamente las opciones que están en posición de operando (como en `a + b + c` o `f(b + c)`); una variable de opción pasada directamente como argumento de función no entra aquí: sigue la exención existente de `?=` para argumentos de opción directos (`a ?= f(v)`, donde `v` es `?i64` y `f` recibe un `i64` simple).

**Error si el lado derecho no es una opción**: para evitar confundir `=` con `?=` a gran escala, cuando el lado derecho se puede determinar en tiempo de compilación como «claramente no una opción», el compilador informa del error y sugiere usar `=`:

- Literales: `v ?= 42`
- Operaciones enteras plegables como constantes: `v ?= 10 + 20`
- Variables locales de tipo conocido que no son opciones: `v ?= x` (donde `x i64 = 5`)

El mensaje de error tiene la forma: `` `?=` RHS `42` is not an option — use `=` instead ``.

> En una operación aritmética, si cualquier operando es una opción (o su tipo es desconocido / es una llamada de función que podría devolver una opción), se considera «posiblemente opción» y se permite, sin informar erróneamente: por ejemplo `v ?= a + b + c` (a/b/c son `?i64`) y `v ?= b + c` (b/c son de tipo desconocido) compilan normalmente.

**Escenarios de aplicación:**

- Operaciones de contenedor que pueden estar vacías, como `pop` / `peek` → `?t` (`nil` = vacío)
- Operaciones de E/S como `read-line` / `read-byte` → `?str` / `?i64` (`nil` = EOF, `err` = error)
- Operaciones de búsqueda como `lookup` / `get` → `?t` (`nil` = no encontrado)
- Operaciones de análisis como `parse` / `from-str` → `?t` (`nil` = vacío, `err` = entrada inválida)
- Conexiones de red como `accept` / `dial` → `?conn` (`nil` = sin conexión, `err` = error)

**nil frente a err**: use `nil` cuando la ausencia es un resultado normal/esperado (pila vacía, clave inexistente, EOF); use `err('msg')` cuando la ausencia representa un estado de error real (fallo de E/S, entrada inválida, conexión rechazada).

**Excepción**: cuando una función necesita devolver varios valores independientes (como `(name str, value str, ok bool)`), se puede conservar el patrón de retorno múltiple.

**Los retornos múltiples pueden tener como máximo un `?T` (error fatal)**: entre los parámetros de retorno declarados de una función, `?T` aparece como máximo una vez. `?T` es un **único** canal de error: cuando `?=` en el cuerpo de la función provoca un retorno anticipado, solo puede colocar el fallo en ese único parámetro de retorno de opción; si se declaran dos `?T`, el compilador solo puede rellenar el primero y quien llama recibe una tupla a medio llenar (`(err, nil)`), sin forma de saber qué valor representa el fallo:

```no
#{fmt-skip}
pair = () (a ?i64, b ?i64) {; error: dos parámetros de retorno ?T
    a = ok(1)
    v ?= boom(); boom() devuelve err('boom')
    a = v
    b = ok(2); nunca se ejecuta: quien llama ve (err, nil)
}
```
```

La forma correcta es declarar los valores independientes como tipos simples y dejar solo un `?T` como canal de error:

```no
lookup = (k str) (value str, found bool, err ?str) {
    value = ''
    found = false
    err = nil
}
```

El mensaje de error tiene la forma: `` function 'pair' declares 2 option (?T) results ('a', 'b'); only one result may be ?T … [moptres1] ``. El receptor `self` implícito en la definición de un método es un parámetro de salida con alias de quien llama y no ocupa este cupo; las declaraciones extern `#{c}` están sujetas a la misma limitación.

### Captura de errores mediante asignación (`=`, captura en el lugar)

La semántica de `?=` es «propagar el error hacia arriba»: exige que la función contenedora tenga un parámetro de resultado de opción y hace un `return` anticipado al fallar. Cuando no se quiere retornar anticipadamente sino manejar el error en el lugar, se usa `=` normal: siempre que el lado derecho contenga una **fuente de error**, la variable destino se infiere automáticamente como `?T` y `nil` / `err` se **guardan en esa misma variable**, continuando el flujo.

```no
handle = (b i64, c i64, d i64) (r i64) {
    a = b + c / d; a se infiere como ?i64; división por cero → a = err
    a: {
        err -> {
            r = -1; manejar el error en el lugar
        }

        nil -> {
            r = -1
        }

        -> {
            r = it; éxito: r = b + c/d
        }
    }
}
```

**Fuentes de error (disparadores de la captura):**

| Fuente | Ejemplo | Resultado de la captura |
| --------------------- | ----------------------------------- | ----------------------------- |
| División / módulo enteros | `a = b + c / d`, `a = b % c` | división por cero, `MIN / -1` → `err` |
| Indexación segura | `a = v[i]` (`arr` / `vec` / `slice`) | fuera de límites → `err` (`code.index-out`) |
| Operando de tipo opción | `a = x + 1` (`x ?i64`) | si `x` es `nil` / `err`, se guarda tal cual en `a` |
| Llamada que devuelve opción (como operando aritmético) | `a = f(x) + 1` (`f` devuelve `?i64`) | igual que arriba |

> Las opciones pasadas **directamente** como argumentos de función no entran aquí: eso se deja a la función llamada (coherente con la exención de argumentos de opción directos de `?=`).

**La aritmética pura no es una fuente de error**: `+ - * <<` y la negación no disparan la captura por sí solas (si no, toda la aritmética se convertiría en opciones). Pero una vez que se entra en la ruta de opción (p. ej. porque el lado derecho tiene además un `/`, o porque el destino se declara explícitamente como `?T`), estas operaciones llevan una **comprobación de desbordamiento en tiempo de ejecución** y el desbordamiento da `err`:

```no
ovf = (x i64) (r i64) {
    a ?i64 = x + 1; ?T explícito → sigue la ruta de empaquetado en opción
    a: {
        err -> {
            r = 0; si x = i64-max, desbordamiento → err
        }

        nil -> {
            r = 0
        }

        -> {
            r = it
        }
    }
}
```

**Captura frente a propagación:**

| Forma | Al fallar | ¿Requiere parámetro de resultado de opción? | Cómo se obtiene el valor después |
| ------------- | ----------------------------------- | -------------- | ------------------ |
| `a ?= expr` | pone el parámetro de resultado a `nil`/`err` y hace `return` (retorno anticipado) | Sí | No aplica (ya se retornó) |
| `a = expr` | `a` guarda `nil`/`err` en el lugar y el flujo continúa | No | `a: { ok -> ... }` |
| `a ?T = expr` | igual que `=` (anotación explícita, incluye subexpresiones internas con error) | No | Igual que arriba |
| `_ = expr` | evalúa pero descarta el resultado y el error (sin error, sin disparar el lint de unused) | No | No aplica |

**Ámbito de aplicación**: la captura de indexación segura, al igual que `?=`, **solo surte efecto dentro de funciones que devuelven un resultado `?T`** (véase la tercera forma en «Indexación segura»). La captura de `/` `%` y de operandos de opción se aplica a todas las funciones.

**Las variables existentes no cambian de tipo**: la captura solo afecta a destinos «declarados por primera vez en esta sentencia». Si el destino ya existe (`v = arr[i]` después de `v i64 = 0`, o `d = x - 1` después de `d = 0`), el compilador no cambia su tipo en el lugar, sino que informa del error y exige elegir explícitamente la semántica (añadir anotaciones `#{overflow=...}` / `#{index-out=...}`, usar `?=` o declarar `?T`).

### Genéricos

```no
arr_to_vec = (arr [n]t) (out []t) {
    i <- [0..n): {
        out[i] = arr[i]
    }
}
```

### Conversiones de tipo

```no
; devolver una cadena con el nombre del tipo
a = typeof(x)

; `as` solo se permite para conversiones de tipos puntero FFI (como *byte, **byte, *i64)
; los enteros son i64 internamente, no hace falta conversión explícita
y = x as *byte
```

### Comprobación de tipos en asignaciones enteras

El compilador comprueba los tipos en las asignaciones enteras para evitar una conversión estrecha (narrowing) insegura que cause pérdida de datos. Estas son las reglas:

#### Ensanchamiento implícito (seguro, permitido automáticamente)

Un valor de un tipo entero estrecho se puede asignar automáticamente a un tipo más ancho, porque el rango del tipo destino contiene completamente al del tipo de origen:

```no
b byte = 200
i i64 = b; ✓ rango de byte [0,255] ⊆ rango de i64
u u32 = b; ✓ rango de byte ⊆ rango de u32
```

#### Asignación de literales enteros

Un literal entero (inferido como `i64` por defecto) se puede asignar a cualquier tipo entero cuyo rango contenga ese valor:

```no
#{fmt-skip}
n u8 = 200; ✓ 200 ∈ [0,255]
m u8 = 300; ✗ 300 > 255, error de compilación
big u64 = 18446744073709551615; ✓ 2^64-1, valor máximo de u64
```
```

#### Conversión estrecha insegura (error de compilación)

Asignar directamente una variable de tipo ancho a un tipo estrecho produce un error de compilación porque puede causar pérdida de datos. El mensaje de error incluye una **sugerencia de corrección accionable** que indica cómo estrechar de forma segura con operaciones a nivel de bits:

```no
#{fmt-skip}
d u64 = 42
h u32 = d; ✗ cannot assign u64 value to u32 variable 'h'; hint: narrow safely with a bitwise mask (e.g. `& 4294967295`) or right shift (e.g. `>> 32`)
h u16 = d; ✗ cannot assign u64 value to u16 variable 'h'; hint: narrow safely with a bitwise mask (e.g. `& 65535`) or right shift (e.g. `>> 48`)
h u8 = d; ✗ cannot assign u64 value to u8 variable 'h'; hint: narrow safely with a bitwise mask (e.g. `& 255`) or right shift (e.g. `>> 56`)
x u32 = d + 1; ✗ el resultado de la suma sigue siendo u64, inseguro
y u32 = foo(); ✗ el tipo del resultado de la llamada no coincide
```
```

> **Sugerencia de corrección**: el compilador calcula automáticamente el valor exacto de la máscara y el desplazamiento según el tipo destino. Al aplicar la máscara o el desplazamiento indicados se puede estrechar de forma segura (véase la sección siguiente).
>
> **Tipos destino con signo**: para enteros con signo como `i8`/`i16`/`i32`/`i64`, la sugerencia indica que no se aplica el estrechamiento a nivel de bits (la truncación del bit de signo tiene semántica ambigua) y recomienda usar en su lugar una comprobación de rango explícita.

#### Estrechamiento seguro mediante operaciones a nivel de bits (permitido automáticamente)

Cuando el lado derecho de la asignación es una **expresión a nivel de bits** (`&`, `|`, `^`, `<<`, `>>`) y el tipo destino es un **entero sin signo** (`u8`/`u16`/`u32`/`u64`/`byte`), el compilador permite el estrechamiento implícito: truncar los bits altos es la semántica estándar de las operaciones a nivel de bits y no causa una pérdida de datos inesperada:

```no
d u64 = 42

; ✓ operación de máscara: el resultado siempre es ≤ al valor de la máscara, cabe seguro en u32
h u32 = d & 67108863; máscara = 2^26-1 < 2^32
h u32 = d & 4294967295; máscara = 2^32-1, justo el rango de u32

; ✓ operación de desplazamiento: tras desplazar a la derecha, los bits altos son 0, seguro
hi u32 = d >> 32; u64 >> 32 deja 32 bits

; ✓ combinación XOR / OR
c u32 = a ^ b; resultado de la operación a nivel de bits
b byte = v & 255; máscara al rango de byte

; ✓ operación a nivel de bits compuesta (habitual en criptografía/codificación)
s u32 = (key[0] & 255) | ((key[1] & 255) << 8) | ((key[2] & 255) << 16) | ((key[3] & 255) << 24)
```

> **¿Por qué se permite?** La semántica de las operaciones a nivel de bits (máscara, desplazamiento, XOR, OR) es construir un patrón de bits. Al asignar a un tipo sin signo estrecho, truncar los bits altos es una operación deliberada: quien desarrolla ha garantizado el rango del resultado mediante una máscara o un desplazamiento, o ha descartado los bits altos a propósito. Es el patrón estándar en criptografía (como ChaCha20, Poly1305, Blake2) y en código de codificación/decodificación.

> **Solo para tipos destino sin signo.** Para destinos enteros con signo (`i8`/`i16`/`i32`/`i64`), se informa del error incluso si el lado derecho es una operación a nivel de bits, porque la truncación del bit de signo tiene semántica ambigua:
>
> ```no
> d u64 = 42
> h i32 = d & 4294967295   ; ✗ sigue dando error: el estrechamiento seguro no se aplica a destinos con signo
> ```

> **La operación de nivel superior debe ser a nivel de bits.** Solo se permite cuando el operador de nivel superior de la expresión es `&`/`|`/`^`/`<<`/`>>`. La suma, la resta, las llamadas de función, las referencias directas a variables, etc., no entran aquí:
>
> ```no
> d u64 = 42
> h u32 = d              ; ✗ el nivel superior es un Identifier, no una operación a nivel de bits
> h u32 = d + 1          ; ✗ el nivel superior es +, no una operación a nivel de bits
> ```

### Sistema de módulos

- Cada archivo es un módulo
- Los nombres de archivos y carpetas usan guiones medios

```shell
utils/
└── helper.no    ; nombre del módulo: utils/helper
```

### Importar módulos

> **La sintaxis nueva usa `#` para importar. La palabra clave `use` de la sintaxis antigua todavía funciona, pero está obsoleta (deprecated); se recomienda usar `#`.**

```no
; biblioteca estándar (sintaxis nueva, recomendada)
# std/math.add
; módulo remoto (no empieza por std/)
# github.com/utils/math.add
; módulo local, debe empezar por /
# /utils/math.add
; alias
# std/math.add a

; ── sintaxis antigua (deprecated, todavía funciona pero no se recomienda) ──
; use std/math.add
; use github.com/utils/math.add
; use /utils/math.add
; use std/math.add a
```

#### Importar directamente archivos de configuración `.noml`

Los archivos `.noml` se pueden importar directamente con `#`: el orden de búsqueda es primero el `.no` del mismo nombre y, si no se encuentra, el `.noml`. Tras importar, **el nombre de la estructura es por defecto el del archivo** (sin la extensión, y se puede reescribir con una anotación `#{name = '...'}` en la parte superior del archivo), y cada miembro se convierte en un campo de esa estructura; al mismo tiempo se genera un valor global con el mismo nombre que contiene el contenido del archivo, de modo que se lee la configuración con `server.port`.

```noml
; cfg/server.noml
{
  host: 'localhost'
  port: 8080
  tags: [] #{type = '[]str'}
  compiler: {
    version: 'v0.1.0'
  }
}
```

```no
# /cfg/server

print(server.port); 8080
print(server.compiler.version); v0.1.0
```

- El tipo del campo se toma preferentemente de la anotación `#{type = '...'}` del miembro (el valor es una expresión de tipo nolang completa, como `'[]str'`, `'[]i64'`, `'?i64'`); sin anotación se infiere del valor: cadena → `str`, entero → `i64`, decimal → `f64`, booleano → `bool`, objeto → estructura anidada generada automáticamente (`server-compiler`), arreglo → `[]tipo-elemento`.
- Los contenedores vacíos (`[]`, `{}`) **deben** llevar `#{type = '...'}`: los valores noml no tienen tipo por sí mismos y un contenedor vacío no se puede inferir. Cuando un miembro de objeto lleva `#{type = 'x'}`, se usa directamente `x` (hay que definirlo) y no se genera la estructura anidada.
- El nombre de la estructura de un objeto anidado lleva como prefijo el nombre del archivo (`server-compiler`); el orden de definición es primero el anidado y luego el anfitrión.
- **Una anotación superior (a nivel de documento) puede reescribir el nombre de la estructura**: `#{name = '...'}` (forma equivalente `#{type = '...'}`, con prioridad para `name`) se escribe en una línea propia encima de la `{` de nivel superior (o al final de la misma línea que el valor superior). Tras reescribirlo, el nombre de vinculación del valor y el prefijo de las estructuras anidadas cambian en consecuencia, pero **la ruta de importación se sigue resolviendo por el nombre del archivo**:

```noml
; cfg/db.noml
#{name = 'db-options'}
{
  host: 'localhost'
}
```

```no
# /cfg/db
# /cfg/db as db2

print(db-options.host); localhost
print(db2.host); localhost
```

- Si dos archivos distintos generan el mismo nombre de tipo (p. ej. archivos con el mismo nombre en directorios distintos), se informa del error; basta con distinguirlos con `#{name = '...'}` en la parte superior. Si el mismo archivo se importa varias veces (p. ej. añadiendo otro alias `as`), solo se fusiona una definición de tipo.
- Los nombres de clave se convierten según las reglas de nolang a minúsculas con guiones medios (`anonymous-fn-type`): si empiezan por un dígito se les añade `f-` y si coinciden con una palabra clave se les añade `x-`.
- Se permite que un alias `as` reescriba el nombre de vinculación del valor, sin cambiar el nombre de la estructura: `# /cfg/server as cfg`.
- Los tipos `.noml` no participan en el prefijo de módulo: incluso si el nombre del archivo coincide con un módulo de la biblioteca estándar (como `server`), el tipo sigue siendo un nombre simple.
- `package.noml` / `workspace.noml` también admiten anotaciones `#{...}` (como `#{type = '[]str'}`): se ignoran al analizar y solo sirven para que herramientas externas reconstruyan estructuras fuertemente tipadas; la sintaxis es exactamente la misma que `#{...}` de nolang.

### Exportar módulos

Solo se aplica a lib.no

```no
@ std/math.add a
```
### FFI (anotación `#{c}`)

Las funciones C externas se declaran mediante la anotación `#{c}` para implementar FFI (Foreign Function Interface).

**Sintaxis**: `#{c}` va en su propia línea y marca la siguiente como declaración FFI. `#{c}` es la clave de lenguaje FFI del sistema de anotaciones, que también admite otros lenguajes como `#{cpp}` y `#{rust}`. La sintaxis antigua `#c` sigue siendo retrocompatible.

**Declaraciones privadas**: un nombre que empieza por `_` indica privado (no se exporta); el símbolo C ABI pierde automáticamente el prefijo `_` y los guiones medios se convierten en guiones bajos.

**Ya no hace falta separar archivos**: las declaraciones FFI y el código ordinario se pueden escribir en el mismo archivo `.no`.

**Sintaxis de tipos puntero**: en FFI se usan formas de estilo C `*T`, `**T`, `***T` para los punteros, y debe haber un tipo concreto `T`. El código ordinario no puede usar esta sintaxis.

| Sintaxis | Significado | LLVM IR | Uso |
| --------- | ----------- | ------- | ------------------- |
| `*byte` | puntero a byte | `i8*` | puntero opaco (p. ej. handle de db) |
| `**byte` | doble puntero | `i8**` | parámetro de salida (p. ej. `sqlite3**`) |
| `***byte` | triple puntero | `i8***` | triple indirección, muy rara |

```no
; sqlite.no — enlaces FFI y envoltorio seguro en el mismo archivo
; el compilador convierte automáticamente los guiones medios (-) en guiones bajos (_) para coincidir con los símbolos C ABI
; los nombres que empiezan por _ son privados; el símbolo C ABI pierde automáticamente el prefijo _

; parámetros de tipo básico
#{c}
c-strlen = (s str) (n i64)

; parámetro puntero (*byte = puntero opaco), declaración privada
#{c}
_sqlite3-close = (db *byte) (rc i32)

; doble puntero (**byte = parámetro de salida, el valor se guarda de vuelta en la variable tras la llamada), declaración privada
#{c}
_sqlite3-open = (filename str, db **byte) (rc i32)

; varios parámetros puntero, declaración privada
#{c}
_sqlite3-exec = (db *byte, sql str, callback *byte, arg *byte, errmsg *byte) (rc i32)
```

```no
; envoltorio seguro en el mismo archivo

open = (dsn str) (d db-sqlite) {
    handle i64 = 0
    rc i32 = _sqlite3-open(dsn, handle)
    rc != SQLITE-OK -> {
        return
    }
    d.handle = handle
}
```

**Reglas:**

1. `#{c}` va en su propia línea y marca la siguiente como declaración FFI (la sintaxis antigua `#c` sigue siendo retrocompatible).
2. FFI es solo una declaración, sin cuerpo de función.
3. Los punteros deben tener un tipo concreto (como `*byte`); no se permite `ptr` desnudo.
4. `**byte` se usa para parámetros de salida: tras la llamada, el valor puntero escrito por la función C se convierte automáticamente a `i64` y se guarda en la variable de quien llama.
5. Todos los punteros se almacenan en el lado Nolang como `i64` (`ptrtoint`).
6. Los parámetros de tipo `str` se convierten automáticamente a `i8*` terminado en nulo.
7. Un nombre que empieza por `_` indica privado (no se exporta); el símbolo C ABI pierde el prefijo `_`.
8. Las declaraciones FFI y el código ordinario se pueden escribir en el mismo archivo `.no`.

> La dirección inversa (permitir que C / C++ / Rust / Go / Java / Python / C# llamen a Nolang) usa `#{cabi='symbol'}` y `no build --shared`; véase [Llamar a Nolang desde lenguajes anfitriones](./host-interop).

### Sistema de anotaciones (`#{...}`)

`#{...}` es el sistema de anotaciones general, una lista de pares clave-valor separados por comas. Admite los siguientes tipos de valor:

| Sintaxis | Tipo | Ejemplo |
| --- | --- | ------------------------------------ |
| Clave suelta | Booleano | `#{debug}` |
| Valor booleano | Booleano | `#{inline=true}` / `#{inline=false}` |
| Numérico | Entero | `#{max=100}` |
| Texto | Cadena | `#{name='hello'}` |
| Identificador | Identificador | `#{mode=fast}` |
| Arreglo | Arreglo | `#{derive=[Serialize, Deserialize]}` |
| Rango | Rango | `#{range=[0..256)}` |

Varios pares clave-valor se separan con comas:

```no
#{derive=[Serialize, Deserialize], range=[0..256), max=100, debug}
```

#### Colocación de anotaciones

`#{...}` solo admite dos formas de escritura:

- **Arriba**: en su propia línea, encima del destino (sentencia, campo de estructura, miembro de enumeración, declaración, brazo de match);
- **Después**: al final de la misma línea del destino (`res = arr[i] #{index-out=zero}`, `a pt #{inline}`,  
  `green #{deprecated}`).

Escribirla **delante del destino en la misma línea** (como prefijo, p. ej. `#{index-out=zero} res = arr[i]`) es un **error de compilación**, y el compilador y el LSP informan el mismo diagnóstico. El criterio: después de la `}` del grupo de anotaciones no puede haber más código en la misma línea (un salto de línea, un comentario de línea `;` o directamente otro grupo de anotaciones no cuentan como código).

```no
; ✓ arriba: en su propia línea
#{overflow=wrap}
x i8 = a + 100

; ✓ después: al final de la misma línea
x i8 = a + 100 #{overflow=wrap}
```

```no
#{fmt-skip}
; ✗ delante: prefijo en la misma línea → error
#{overflow = wrap} x i8 = a + 100
```

Las anotaciones en campos de estructura y miembros de enumeración (valores de un enum de estilo C y variantes de una enumeración etiquetada) tampoco admiten más que arriba o después (`a pt #{inline}`, `a pt #{inline=true}`, `green #{deprecated}`, `ok(v i64) #{inline}`). Nótese que el **valor** de `#{inline}` también se admite: `#{inline=false}` es válido (véase más abajo «`#{inline}` en campos de estructura»).

En la forma `if <cond> { ... }`, una anotación situada entre la condición y `{` (`if x > 0 #{overflow=wrap} {`) también es una posición «delante» y da el mismo error. El análisis en ese punto es laxo y antes se tragaba el grupo entero de anotaciones: no surtía efecto ni informaba del error, siendo la forma de fallo más difícil de detectar.

La sintaxis de rangos admite 16 combinaciones de paréntesis (regla: `[` incluye el extremo izquierdo y `(` no; `]` incluye el extremo derecho y `)` no):

**Intervalos acotados (4 tipos)**: ambos extremos tienen valor:

- `[a..b]` — intervalo cerrado en ambos extremos (`x >= a && x <= b`)
- `[a..b)` — cerrado a la izquierda, abierto a la derecha (`x >= a && x < b`)
- `(a..b]` — abierto a la izquierda, cerrado a la derecha (`x > a && x <= b`)
- `(a..b)` — intervalo abierto en ambos extremos (`x > a && x < b`)

**Intervalos sin límite superior (4 tipos)**: se omite el extremo derecho:

- `[a..]`, `[a..)` — extremo izquierdo incluido, sin límite superior (`x >= a`)
- `(a..]`, `(a..)` — extremo izquierdo excluido, sin límite superior (`x > a`)

**Intervalos sin límite inferior (4 tipos)**: se omite el extremo izquierdo:

- `[..b]`, `(..b]` — sin límite inferior, extremo derecho incluido (`x <= b`)
- `[..b)`, `(..b)` — sin límite inferior, extremo derecho excluido (`x < b`)

**Intervalos totalmente ilimitados (4 tipos)**: ambos extremos se omiten y se basan en los valores mínimo/máximo del tipo:

- `[..]` — mínimo del tipo ≤ x ≤ máximo del tipo (ambos extremos incluidos)
- `[..)` — mínimo del tipo ≤ x < máximo del tipo
- `(..]` — mínimo del tipo < x ≤ máximo del tipo
- `(..)` — mínimo del tipo < x < máximo del tipo

#### Ámbito de las anotaciones

La **posición** de la anotación determina el anfitrión y el **tipo** de anfitrión determina cuánto abarca. Hay cuatro niveles que surten efecto de fuera hacia dentro, prevaleciendo el más cercano; el cuerpo de la función es un límite duro para la herencia:

| Nivel | Anfitrión | Ámbito efectivo |
| --- | --- | --- |
| **L0 nivel de módulo** | cabecera del archivo | todas las declaraciones y sentencias del archivo (anotación de cabecera + una línea en blanco) |
| **L1 nivel de declaración** | constantes/variables globales, estructuras, interfaces, enumeraciones etiquetadas, funciones, métodos | toda la definición de esa declaración (la función incluye el cuerpo y todos los bloques anidados) |
| **L2 nivel de bloque** | grupos de corutinas, grupos if, `cond -> body`, match, bucles `for`/contador/rango | todas las sentencias dentro del bloque (incluidos los bloques anidados, hasta el límite L1 interno) |
| **L3 nivel de línea individual** | una sola sentencia (`let`, asignación, `?=`, `return`, llamada, brazo `cond ->`) | la propia sentencia y sus expresiones |

```no
; L1: toda la función
#{overflow=wrap}
sum = (xs []i64) (total i64) {
    ; L3: solo afecta a la siguiente sentencia
    #{overflow=wrap}
    total = xs.len() - 1
    ; L2: todo el cuerpo del bucle
    #{overflow=wrap}
    i <- [0..xs.len()): {
        total = total + xs[i]
    }
}
```

Las reglas completas de decisión, la matriz clave × nivel y la diferencia con la implementación actual están en [«Ámbito de anotaciones y comentarios»](annotation-scope.md).

La anotación FFI `#{c}` es una forma especial del sistema de anotaciones: cuando la anotación contiene una clave de lenguaje FFI (`c`, `cpp`, `rust`, etc.) y a continuación viene una declaración de función, el compilador la reconoce como un enlace FFI:

```no
; #{c} con anotaciones adicionales
#{c, debug}
_sqlite3-open = (filename str, db **byte) (rc i32)
```

#### Anotaciones adjuntas a declaraciones

Las anotaciones que no son FFI se adjuntan automáticamente a la declaración inmediatamente posterior (declaración de variable, definición de estructura) y se pueden usar para marcar metadatos como límites de rango en tipos numéricos (como `num`, `i64`, etc.):

```no
; declaración de variable con anotación range
#{range=[0..256)}
x num = 42

; definición de estructura con anotación
#{derive=[Serialize, Deserialize]}
point {
    x i64
    y i64
}

; campo de estructura con anotación range (se puede usar en tipos numéricos como num)
person {
    age num #{range=[0..150]}
    score i64 #{range=[0..256)}
    name str
}
```

La anotación `range` es especialmente útil para el tipo `num` (`num = int | float`), para marcar el rango válido de un valor numérico. El valor del rango puede ser un entero o un identificador:

```no
; usar identificadores constantes como límites del rango
#{range=['i8.MIN'..'i8.MAX']}
val i8 = 100
```

#### Incrustación de archivos (`#{embed=...}`)

La anotación `#{embed=...}` incrusta el contenido de un archivo externo en tiempo de compilación como una constante `[]byte` de solo lectura, similar a `//go:embed` de Go. Los datos incrustados se compilan directamente en el ejecutable, permitiendo una distribución de un único binario sin necesidad de leer archivos en tiempo de ejecución.

**Sintaxis**:

```no
#{embed='path/to/file'}
ICON []byte

; o usar una ruta simple (sin comillas)
#{embed='assets/icon.ico'}
ICON []byte
```

La anotación `embed` se adjunta a la declaración de variable `[]byte` inmediatamente posterior. La declaración de variable **no puede** tener un valor inicial explícito: el valor lo proporciona el contenido del archivo incrustado.

**Resolución de rutas**:

- Las rutas relativas se resuelven respecto al directorio raíz del paquete (donde está `package.noml`)
- Se admiten rutas absolutas
- Las rutas pueden contener los caracteres `/`, `.`, `-`

**Ejemplo**:

```no
; incrustar un icono de Windows
#{embed='assets/win-icon.ico'}
WIN-ICON []byte

; incrustar un certificado SSL
#{embed='certs/server.pem'}
SERVER-CERT []byte

; incrustar un archivo de configuración
#{embed='config/default.json'}
CONFIG []byte

; usar los datos incrustados
print('icon size: ', len(WIN-ICON))
print('first byte: ', WIN-ICON[0])
```

**Consideraciones**:

1. Los datos incrustados son constantes de **solo lectura** y apuntan a una región de memoria de solo lectura. Escribir en datos incrustados es comportamiento no definido (coherente con la semántica de `//go:embed` de Go).
2. Las variables incrustadas no participan en la liberación del montón: al salir el programa no se libera el puntero de los datos incrustados.
3. Solo se admiten declaraciones de tipo `[]byte` (también vale el arreglo fijo `[N]byte`).
4. El archivo debe existir en tiempo de compilación; si no, se informa del error.
5. No puede coexistir con un valor inicial explícito (p. ej. `ICON []byte = [1, 2, 3]` da error).

#### Incrustación de directorios (`#{embed='dir'}`)

`#{embed=...}` también admite incrustar un directorio entero. Cuando la ruta apunta a un directorio, el compilador lee recursivamente todos los archivos que contiene y los incrusta como un sistema de archivos de solo lectura de tipo `fs.embed`. En tiempo de ejecución se accede a los archivos incrustados por ruta mediante los métodos `read()` y `exists()`, sin necesidad de ninguna función C: la lógica de búsqueda está implementada íntegramente en Nolang.

**Sintaxis**:

```no
; incrustar todo el directorio del frontend
#{embed='../frontend/dist'}
DIST fs.embed

; leer un archivo del sistema de archivos incrustado
data = DIST.read('index.html')

; comprobar si un archivo existe
ok = DIST.exists('css/style.css')
```

**Tipo `fs.embed`**:

`fs.embed` es una estructura definida en el módulo `fs` de la biblioteca estándar de Nolang que contiene los metadatos de los archivos incrustados (ruta, desplazamiento, longitud). El compilador lee el directorio en tiempo de compilación y concatena el contenido de todos los archivos en una variable global constante; en tiempo de ejecución, el método `read`, implementado en Nolang puro, recorre y busca los archivos.

| Método | Firma | Descripción |
| -------- | ----------------------------------- | ----------------- |
| `read` | `(path str) (data ?[]byte)` | lee el archivo incrustado y devuelve su contenido (nil si no se encuentra) |
| `exists` | `(path str) (yes bool)` | comprueba si el archivo incrustado existe |

**Ejemplo**:

```no
; servidor HTTP de archivos estáticos: incrustar los archivos del frontend en un único binario
#{embed='../frontend/dist'}
DIST fs.embed

serve = (conn server-conn) {
    conn.parse()
    path = conn.path
    path == '/' -> path = '/index.html'

    ; quitar la / inicial para obtener la ruta relativa
    rel = path.slice(1, path.len-bytes())

    ; leer desde el sistema de archivos incrustado
    data []byte = DIST.read(rel)
    {
        ok -> it

        -> {
            conn.write-html(404, 'Not Found')
            conn.close()
            return
        }
    }
    conn.write-status(200)
    conn.write-header('Content-Type', get-content-type(path))
    conn.write-body(data)
    conn.close()
}
```

**Consideraciones**:

1. Todos los archivos del directorio (incluidos los subdirectorios) se incrustan recursivamente y las rutas se almacenan relativas al directorio incrustado (p. ej. `css/style.css`).
2. El separador de rutas se unifica como barra inclinada `/` (coherente entre plataformas).
3. Las variables de tipo `fs.embed` son de solo lectura y no participan en la liberación del montón.
4. Resulta útil para escenarios de distribución de un único binario (como servidores HTTP de archivos estáticos o herramientas CLI que incrustan recursos).

#### Anotaciones de plataforma

Las anotaciones de plataforma son filtros en tiempo de compilación que deciden si se incluye código según la plataforma de destino. Usan **claves aplanadas** (como `#{mac-arm64}`) que especifican a la vez el sistema operativo y la arquitectura sin ambigüedad, y se adjuntan a la declaración inmediatamente posterior. El código que no coincide no participa en la compilación en absoluto: no se genera LLVM IR ni se realiza comprobación de tipos.

**Claves de plataforma admitidas (6 combinaciones aplanadas):**

| Clave | Coincidencia |
| ---------------- | -------------------------- |
| `#{linux-amd64}` | Linux x86_64 |
| `#{linux-arm64}` | Linux ARM64 |
| `#{win-amd64}` | Windows x86_64 |
| `#{win-arm64}` | Windows ARM64 |
| `#{mac-amd64}` | macOS x86_64 (Intel) |
| `#{mac-arm64}` | macOS ARM64 (Apple Silicon) |

```no
#{mac-arm64}
print('running on macOS ARM64')

#{linux-amd64}
print('running on Linux x86_64')

#{win-amd64}
print('running on Windows x86_64')

; variables específicas de plataforma
#{mac-amd64, mac-arm64}
sep = '/'

#{win-amd64, win-arm64}
sep = '\\'

; funciones específicas de plataforma
#{mac-arm64, mac-amd64}
greet = () {
    print('hello from mac')
}

#{linux-amd64, linux-arm64}
greet = () {
    print('hello from linux')
}

greet()
```

Varias claves en una misma declaración tienen relación **OR**: basta una coincidencia para incluirla. Como cada clave ya especifica a la vez el sistema operativo y la arquitectura, no se necesita lógica AND.

| Anotación | Significado |
| --------------------------- | --------------------------------- |
| `#{mac-arm64}` | solo macOS ARM64 |
| `#{mac-amd64, mac-arm64}` | cualquier arquitectura de macOS |
| `#{linux-amd64, win-amd64}` | Linux x86_64 **o** Windows x86_64 |
| `#{mac-arm64, linux-arm64}` | macOS ARM64 **o** Linux ARM64 |

```no
; se ejecuta en macOS y Linux (todas las arquitecturas)
#{mac-amd64, mac-arm64, linux-amd64, linux-arm64}
shared = () {
    print('unix-like')
}

; solo Windows x86_64
#{win-amd64}
reg-key = () {
    print('reading registry on win/x64')
}

; solo macOS ARM64 (Apple Silicon)
#{mac-arm64}
neural = () {
    print('Apple Neural Engine available')
}
```

`os.get-arch()` permite obtener la arquitectura actual en tiempo de ejecución, mientras que las anotaciones de plataforma incluyen o excluyen código en tiempo de compilación.

## Configuración del compilador en package.noml

El bloque `compiler` de `package.noml` controla el comportamiento del compilador. Por convención, el valor de `version` lleva una `v` inicial (como `"v0.1.0"`); internamente el compilador no es sensible al prefijo al analizarlo, y `"v0.1.0"` y `"0.1.0"` se tratan como la misma versión.

```noml
{
  compiler: {
    version: 'v0.1.0'
    anonymous-fn-type: false
    emit: 'js'
  }
}
```

### `emit`

Controla el backend de salida:

| Valor | Descripción |
| ----------- | --------------------------------------- |
| `""` (vacío, por defecto) | usa el backend LLVM para generar un ejecutable nativo |
| `"js"` | usa el backend JS para generar JavaScript (con borrado de tipos), sin necesidad de la cadena de herramientas LLVM |

Al establecerlo a `"js"`, `no build` usa automáticamente el backend JS, equivalente a la opción de línea de comandos `--js`. Las banderas de línea de comandos tienen prioridad sobre el archivo de configuración.

```noml
; generar JavaScript automáticamente
compiler: {
  emit: 'js'
}
```

### `anonymous-fn-type`

Controla si se permite la sintaxis de tipos de función anónima (como `cb ()()`). Por defecto es `false` (desactivado) y hay que usar un alias de tipo de función con nombre.

### `link-libs`

Especifica la lista de bibliotecas C a enlazar:

```noml
compiler: {
  link-libs: ['crypto', 'ssl']
}
```

## Inyección de variables en tiempo de compilación (-ld)

`no build`, `no run` y `no test` admiten inyectar constantes globales en tiempo de compilación mediante la bandera `-ld-KEY=VALUE`. La variable inyectada equivale a declarar `KEY = VALUE` al inicio del código fuente y se puede usar directamente en el programa.

### Sintaxis

```bash
no build -ld-KEY=VALUE [más -ld...] <file>
```

- Tras el prefijo `-ld` van directamente el nombre y el valor de la variable, separados por `=`.
- Se pueden usar varias banderas `-ld` a la vez.
- Forma abreviada booleana: `-ld-DEBUG` (sin `=VALUE`) equivale a `-ld-DEBUG=true`.

### Inferencia de tipo del valor

| Forma del valor | Tipo inferido | Ejemplo |
| ---------------- | --------------- | ------------------- |
| Entero | `i64` | `-ld-COUNT=42` |
| Decimal | `f64` | `-ld-PI=3.14` |
| `true` / `false` | `bool` | `-ld-DEBUG=true` |
| Otro | `str` (literal de cadena entre comillas simples) | `-ld-VERSION=0.1.2` |

### Ejemplo

```no
; suponer compilación con -ld-VERSION=0.1.2 -ld-COUNT=42 -ld-DEBUG=true
print('VERSION:', VERSION); salida: VERSION: 0.1.2
print('COUNT:', COUNT); salida: COUNT: 42
print('DEBUG:', DEBUG); salida: DEBUG: 1 (bool se imprime como 1 en el backend nativo)
```

```bash
; inyectar varias variables
no build -ld-VERSION=0.1.2 -ld-COUNT=42 -ld-DEBUG=true main.no

; forma abreviada booleana
no run -ld-RELEASE main.no

; el backend JS también lo admite
no build --js -ld-VERSION=0.1.2 main.no
```

### Relación con las declaraciones del código fuente

Reglas de interacción entre las variables inyectadas con `-ld` y las declaraciones del código fuente:

- **El código fuente ya tiene una declaración con el mismo nombre**: el valor inyectado **sustituye en el lugar** el valor de esa variable en el código fuente. El patrón habitual es declarar una variable de marcador de posición en el código (como `VERSION = ''`) y asignarle el valor en la compilación con `-ld-VERSION=0.1.2`.
- **No existe en el código fuente**: la variable inyectada se antepone al principio del AST como una nueva declaración de constante global y participa en todos los pases de compilación posteriores (inferencia de tipos, verificación, fusión de módulos, codegen).
- **Sin inyección**: la declaración del código fuente permanece intacta y no se ve afectada.

```no
; declarar variables de marcador de posición en el código fuente
VERSION = ''
COUNT = 0

print('VERSION:', VERSION)
print('COUNT:', COUNT)

; no build -ld-VERSION=0.1.2 main.no
; salida: VERSION: 0.1.2  (el valor inyectado sobrescribió el '' del código fuente)
;       COUNT: 0        (no inyectado, conserva el valor del código fuente)
```
