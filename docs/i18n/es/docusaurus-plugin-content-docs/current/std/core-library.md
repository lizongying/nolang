---
sidebar_position: 3.2
---

## Biblioteca central

### fmt — salida formateada

Nolang usa **cadenas de formato con nombre** `{name[:spec]}`, que referencian directamente variables del ámbito sin argumentos posicionales. La salida se escribe mediante las llamadas al sistema `io.out`/`io.err`, sin depender del `printf` de libc.

```no
print('x={x}'); formato con nombre, añade salto de línea (stdout)
print('result={val}', 42, 'result={val}'); varios argumentos: separados por espacios; cada literal es su propia plantilla
eprint('err {x}'); formato con nombre, añade salto de línea (stderr)
print('número {id:06} importe {money:.2f}'); admite alineación/relleno/ancho/precisión
s = format('x={x}'); devuelve la cadena formateada (sustituye a sprintf)
io.out('no-newline-here'); comando de bajo nivel, salida sin salto de línea (stdout)
io.err('err-no-newline'); comando de bajo nivel, salida sin salto de línea (stderr)
; printf/eprintf se han eliminado (llamarlos es error de compilación [printf-depr]): printf→print/io.out, eprintf→eprint/io.err
; sprintf sigue disponible pero está obsoleto: sprintf→format
; io.err lleva prefijo de módulo explícito, no choca con el constructor de Option err()
```

### math — funciones matemáticas (API en forma de función)

Todas las API en «forma de función» de la familia numérica de std están en el módulo math (std/math.no); las API en «forma de método» (`*.to-str`, sqrt/sin/exp de f64/f32, sqrt/is-prime de enteros) están en el módulo number (véase la sección number más abajo). Los tipos unión y sus métodos: num → std/num.no, int → std/int.no, float → std/float.no.

**Constantes:** `math.PI`, `math.E`, `math.LN10` (ln 10, complemento de la implementación de f64-to-str)

**Comparación (función variádica genérica num):** `math.max`, `math.min` (`a ..num`; enteros y flotantes siguen la misma ruta de monomorfización)

**Potenciación / raíz (funciones):** `math.pow` (potenciación f64 a^b, implementada en `.no` puro: `exp(y*ln|x|)` + tratamiento del signo de base negativa (exponente entero impar → negativo, par → positivo; exponente no entero → NaN), **sin depender de libm**), `math.hypot` (sqrt(x*x+y*y))

**Otras funciones:** `math.atan2`, `math.fmod` (resto de coma flotante), `math.clamp` (limitación saturada de i64)

**Matemática de enteros (funciones genéricas int):** `math.even`, `math.odd`, `math.gcd`, `math.lcm`, `math.div` (cociente), `math.mod` (módulo)

**Conversiones de tipo (las antiguas funciones `math.i64-to-f64` / `math.f64-to-i64` / `math.f32-to-f64` / `math.f64-to-f32` se han eliminado; use la forma de método):** `v.to-f64()`, `v.to-i64()`, `f32.to-f64()` / `f64.to-f32()` (módulo number; todos los anchos enteros tienen `.to-f64()`, y i128 / u128 también ofrecen `.to-f64()`)

**Número a cadena de bajo nivel:** solo queda `math.char-to-str` (obsoleto, se recomienda `c.to-str()`); `math.i64-to-str` / `math.u64-to-str` / `math.f64-to-str` se han eliminado; use la forma de método `v.to-str()`

**Operaciones a nivel de bits (funciones):** `math.swap`, `math.arr-zero`, `math.rotate-left`, `math.rotate-right`, `math.store-le-u32`

> Potenciación entera: `math.pow-int` (potenciación rápida genérica int, en un módulo distinto de `math.pow` de f64); `number.pow` conserva una firma i64 explícita para cumplir las aserciones históricas.

### number — operaciones numéricas (API en forma de método)

Las API en «forma de método» de la familia numérica de std están en el módulo number (std/number.no); las API en forma de función están en la sección math de arriba. Los tipos unión int / float / num están en sus respectivas secciones.

**Constantes de rango:** i8.MIN / MAX … i128.MIN / MAX, u8.MIN / MAX … u128.MAX; `number.INF`

**Potenciación entera (función):** `r = number.pow(base, n)` (base^n, n ≥ 0, potenciación rápida O(log n), firma i64, desbordamiento con wrap; la versión genérica int está arriba como `math.pow-int`)

**Métodos de enteros:** `n.sqrt()` (raíz cuadrada entera floor(√n), negativos devuelven 0), `n.is-prime()` (test de primalidad) — con versiones para i8 / i16 / i32 / i64 / u8 / u16 / u32 / u64 y también para i128 / u128 (la aritmética de 128 bits usa la vía LLVM i128 y funciona; hay que declarar explícitamente variables locales con tipo, como `n i128 = .`, y colocar correctamente las anotaciones `#{overflow=wrap}` según la signitud)

**to-str de tipos concretos (método):** `v.to-str()` — para i8 / i16 / i32 / i64 / u8 / u16 / u32 / u64 / byte / f32 / f64 en todos los anchos; i128 / u128 también (mediante `@str_from_i128` / `@str_from_u128` nativos)

**Métodos de coma flotante (f64 y sus homónimos f32):** trigonométricos `x.sin()`, `x.cos()`, `x.tan()`, `x.asin()`, `x.acos()`, `x.atan()`; hiperbólicos `x.sinh()`, `x.cosh()`, `x.tanh()`; redondeo `x.ceil()`, `x.floor()`, `x.round()`, `x.trunc()`; exponenciales/logarítmicos `x.exp()`, `x.log()`, `x.log10()`, `x.log2()`; potenciación/raíz `x.sqrt()`, `x.cbrt()`; grados/radianes `x.degrees()`, `x.radians()`. f32 se calcula internamente con precisión double y luego se estrecha a f32.

### char — operaciones con caracteres

char es esencialmente un i32 (punto de código Unicode); todas las operaciones se ofrecen en forma de método:

```no
c char = 'A'
c.is-digit(); si es un dígito (0-9)（método）
c.is-letter(); si es una letra (a-z, A-Z)（método）
c.is-alpha(); alias de is-letter（método）
c.is-alnum(); si es letra o dígito（método）
c.is-space(); si es un carácter de espacio（método）
c.is-upper(); si es mayúscula（método）
c.is-lower(); si es minúscula（método）
c.to-upper(); a mayúsculas (ASCII)（método）
c.to-lower(); a minúsculas (ASCII)（método）
c.to-bytes(); Unicode → bytes UTF-8（método）
c.to-str(); Unicode → cadena (UTF-8, método)
```

### str — operaciones con cadenas

El descriptor de `str` es `{ u32 len, u32 cap, i8* data }` (16 bytes en 64 bits), pero los parámetros y valores devueltos de `s.len()` / `s.len-bytes()` / `s.cap()` y de `with-cap` / `with-len` / `with-cap-len` **siguen siendo todos `i64`**: el compilador hace la conversión u32 ↔ i64 en la frontera del descriptor. Véase [Cadenas: layout interno y API pública](../lang/str.md). `txt` no se ve afectado por este cambio.

El descriptor de la rebanada `[]t` es isomorfo: `%vec = { u32 len, u32 cap, i64 data }` (16 bytes, `data` es un puntero codificado con `ptrtoint`), y `v.len()` también sigue siendo `i64`. `[n]t` no tiene descriptor en tiempo de ejecución (los elementos están en línea en `[n x t]` y `.len` es una constante en tiempo de compilación).

```no
#{fmt-skip}
ok = a.eq(b, n)               ; comparación de igualdad (método)
dst = s.copy()                ; copia de cadena (método)
s.fill(val byte)              ; rellena con un valor byte (método)
pos = s.index(sub)            ; posición de la subcadena
ok = s.contains(sub)          ; si la contiene
ok = s.starts-with(sub)       ; comprobación de prefijo
ok = s.ends-with(sub)         ; comprobación de sufijo
s.to-upper()                  ; a mayúsculas
s.to-lower()                  ; a minúsculas
out = s.trim()                ; quita espacios al inicio y al final
out = s.repeat(n)             ; repetición
out = s.slice(start, end)     ; rebanada (índices de punto de código)
b = s.to-bytes()              ; a []byte
s = b.to-str()                ; []byte a str (método)
v = s.to-i64()                ; cadena a i64 (devuelve ?i64)
v = s.to-i8()                 ; cadena a i8 (devuelve ?i8)
v = s.to-i16()                ; cadena a i16 (devuelve ?i16)
v = s.to-i32()                ; cadena a i32 (devuelve ?i32)
v = s.to-u8()                 ; cadena a u8 (devuelve ?u8)
v = s.to-u16()                ; cadena a u16 (devuelve ?u16)
v = s.to-u32()                ; cadena a u32 (devuelve ?u32)
v = s.to-u64()                ; cadena a u64 (devuelve ?u64)
v = s.to-byte()               ; cadena a byte (devuelve ?byte)
v = s.to-f64()                ; cadena a f64 (devuelve ?f64)
v = s.to-bool()               ; cadena "true"/"false" a bool (devuelve ?bool)
s = v.to-str()                ; i64 a cadena (método)
out = s.reverse()             ; inversión
c = s.compare(b)              ; comparación lexicográfica
n = s.count()                 ; total de puntos de código
val = s.replace-char(old, new) ; sustituye un carácter (devuelve la cadena resultante)
out = s.trim-char(c)          ; quita el carácter indicado
ok = s.empty()                ; si está vacía
s.clear()                     ; vacía (len=0, modificación in situ)
s = with-cap(cap)            ; sintaxis integrada: crea una cadena nueva con la capacidad indicada (len=0)
s = with-len(len)            ; sintaxis integrada: crea una cadena con la longitud indicada (len=cap)
s = with-cap-len(cap, len)   ; sintaxis integrada: crea una cadena con capacidad y longitud indicadas
parts = s.split(sep)          ; divide por el separador (devuelve []str, método)
out = ss.join(sep)            ; une []str con el separador (método)
```

### num — tipo unión num y métodos de num

`num = int | float` (std/num.no). Los métodos del tipo num están en el módulo num (las funciones genéricas max/min están arriba, en math):

```no
r = num.abs(); valor absoluto (método)
r = num.clamp(lo, hi); limitación de rango (método)
r = num.sign(); signo (-1/0/1, método)
```

### int — tipo unión int y métodos de int

`int = i8 | i16 | i32 | i64 | i128 | u8 | u16 | u32 | u64 | u128` (std/int.no). Los métodos del tipo int están en el módulo int:

```no
s = v.to-str(); entero a cadena (método)
q = v.div(b); división, cociente (devuelve option)
r = v.mod(b); módulo (devuelve option)
```

### float — tipo unión float y métodos de float

`float = f32 | f64` (std/float.no). Los métodos del tipo float están en el módulo float:

```no
q = v.div(b); división de coma flotante
yes = v.is-nan(); comprobación de NaN (método)
yes = v.is-inf(); comprobación de Inf (método)
```

(No se define el método unión float.to-str; las variables f64/f32 usan directamente el método de su tipo concreto `v.to-str()`.)

### number — ejemplo de uso de métodos

(La API completa está arriba en «number — operaciones numéricas (API en forma de método)»; las formas de función están en la sección «math».)

```no
; forma de método (módulo number)
s = v.to-str(); entero/flotante a cadena (todos los anchos)
r = n.sqrt(); raíz cuadrada entera floor(√n)
yes = n.is-prime(); test de primalidad (int/u8…)
q = x.sqrt(); raíz cuadrada de coma flotante (método f64/f32)
q = x.floor(); redondeo (método f64/f32)
r = number.pow(a, n); potenciación entera (firma i64; la versión genérica int es math.pow-int)

; forma de función (módulo math)
m = math.max(1, 2, 3); variádico genérico num
g = math.gcd(a, b); máximo común divisor
l = math.lcm(a, b); mínimo común múltiplo
pi = math.pow-int(a, n); potenciación entera (genérico int)
sw = math.swap(a, b); intercambio

; conversiones numéricas (forma de método; las antiguas funciones math.*-to-* se han eliminado)
f = v.to-f64(); entero → f64
n = x.to-i64(); f64 → entero (truncamiento)
w = f32v.to-f64(); f32 → f64; la inversa es f64.to-f32()

; constantes de rango (módulo number)
i8.MIN / MAX; -128 / 127
i16.MIN / MAX; -32768 / 32767
i32.MIN / MAX; -2147483648 / 2147483647
i64.MIN / MAX; -2^63 / 2^63-1
i128.MIN / MAX; -2^127 / 2^127-1
u8.MIN / MAX; 0 / 255
u16.MIN / MAX; 0 / 65535
u32.MIN / MAX; 0 / 4294967295
u64.MIN / MAX; 0 / 2^64-1
u128.MIN / MAX; 0 / 2^128-1
number.INF; infinito de coma flotante
```

### byte — operaciones con bytes

```no
#{fmt-skip}
out = i64.to-bytes-be()         ; i64 → [8]byte big-endian
out = i64.to-bytes-le()         ; i64 → [8]byte little-endian
v = []byte.to-i64-be()          ; []byte big-endian → i64（1~8 bytes）
v = []byte.to-i64-le()          ; []byte little-endian → i64（1~8 bytes）
s = []byte.to-str()             ; []byte a str (método)
s = []byte.to-hex()             ; []byte → cadena hexadecimal en mayúsculas
s = []byte.to-hex-lower()       ; []byte → cadena hexadecimal en minúsculas
s = byte.to-str()               ; byte a str (método)
```

### txt — tipo de texto de longitud fija

`txt` es un tipo de texto fijo de 256 bytes, adecuado para textos cortos:
- los primeros 255 bytes guardan los datos (`data [255]byte`);
- el último byte guarda la longitud (`len byte`, en «bytes», rango 0-255);
- no requiere asignación en el heap, todo está en la pila;
- debe llevar anotación de tipo.

Semántica de longitud (alineada con `str`):
- `t.len()` → número de caracteres (puntos de código) (equivalente a `t.count()`);
- `t.len-bytes()` → número de bytes (longitud real del búfer UTF-8 subyacente);
- las operaciones de bytes (indexación / rebanadas / añadidos) se rigen por `len-bytes()`; en ASCII ambos valores coinciden, y con UTF-8 multibyte `len() < len-bytes()`.

```no
#{fmt-skip}
t txt = 'hello'              ; debe llevar anotación de tipo
n = t.len()                  ; número de caracteres (puntos de código) (i64)
b = t.len-bytes()            ; número de bytes (i64)
c = t[0]                     ; acceso por índice (byte)
ok = t.eq(b txt)             ; comparación de igualdad
dst = t.copy()               ; copia
s = t.to-str()              ; a str
out = t.to-bytes()           ; a []byte
pos = t.index(sub txt)       ; buscar subcadena
ok = t.contains(sub txt)     ; si la contiene
ok = t.starts-with(sub txt)  ; comprobación de prefijo
ok = t.ends-with(sub txt)    ; comprobación de sufijo
t.push(c char)              ; añade un punto de código (si el carácter entero no cabe en el límite de 255, se ignora por completo)
t.push-str(s str)            ; añade str (truncado a 255 por bytes)
t.push-txt(t2 txt)           ; añade txt (truncado a 255 por bytes)
out = t.reverse()            ; inversión
out = t.slice(start, end)   ; extrae [start, end)
r = t.compare(b txt)        ; comparación lexicográfica (-1/0/1)
```

### vec — operaciones con rebanadas

```no
#{fmt-skip}
v = vec.vec-create(n, val)         ; crea una rebanada de longitud n rellena con val
ok = []t.eq(a, b, n)           ; comparación de igualdad
n = []t.len()                  ; longitud
[]t.push(val)                   ; añadir (expansión automática)
[]t.clear()                     ; vaciar (len=0, cap/data sin cambios)
v = with-cap(cap)             ; sintaxis integrada: crea una rebanada nueva con la capacidad indicada (len=0)
v = with-len(len)             ; sintaxis integrada: crea una rebanada con la longitud indicada (len=cap)
v = with-cap-len(cap, len)    ; sintaxis integrada: crea una rebanada con capacidad y longitud indicadas
val, new-n = []t.pop()         ; extraer el último
found = []t.contains(n, val)   ; si lo contiene (n es la longitud)
[]t.reverse(n)                  ; invierte los primeros n elementos
[]t.clone(dst)                  ; copia a dst
[]t.fill(n, val)                ; rellena los primeros n elementos
arr = []t.to-arr()             ; a arreglo
[]t.sort-asc()                  ; ordenación ascendente (método)
[]t.sort-desc()                 ; ordenación descendente (método)
```

### arr — operaciones con arreglos

```no
out = [n]
t.clone(); copia
ok = [n]
t.eq(b); comparación de igualdad
[n]
t.fill(val); relleno
[n]
t.reverse(); inversión
ok = [n]
t.contains(val); si lo contiene
v = [n]
t.to-vec(); a rebanada
v = [n]
t.max(); valor máximo
v = [n]
t.min(); valor mínimo
v = [n]
t.sum(); suma
i = [n]
t.index-of(val); índice
v = [n]
t.last(); último elemento
v = [n]
t.first(); primer elemento
[n]
t.sort-asc(); ordenación ascendente
[n]
t.sort-desc(); ordenación descendente
```

### sort — constantes de ordenación

```no
sort.ast; ascendente
sort.desc; descendente
```

---
