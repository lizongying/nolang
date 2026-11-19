---
sidebar_position: 6
---

# Prefijo de llamada entre módulos

Nolang impone una convención obligatoria de espacios de nombres para módulos: al llamar, desde un archivo `.no`, a funciones o constantes definidas en **otros módulos** (otros archivos `.no`), debe usar el prefijo `ShortName.`. Esto evita conflictos de nombres entre módulos.

Los módulos de la biblioteca estándar los carga automáticamente el compilador — **no hace falta importarlos explícitamente** (no se requiere la anotación `# std/...`); basta con usar el prefijo `ShortName.`. En particular, `# std/fmt` es redundantísimo: `print` / `eprint` / `format` son intrínsecos del compilador, y `no fmt` elimina automáticamente un `# std/fmt` sin alias (véase [Guía de estilo · Limpieza automática de importaciones](./code-style#導入語句自動整理去重--移除-stdfmt--排序)).

> **Dependencias y resolución de rutas**: las claves de dependencia de los paquetes locales se pueden asignar a nombres cortos o a URLs completas mediante `workspace.noml`. Véase [Tipos de dependencia y reglas de número de versión](../usage#依賴類型與版本號規則).

## Definición de ShortName

ShortName es el último segmento de la ruta del módulo; se usa como prefijo en las llamadas entre módulos.

| Ruta del archivo | FullPath | ShortName | Descripción |
| --- | --- | --- | --- |
| `std/math.no` | `math` | `math` | archivo de nivel superior |
| `std/fs.no` | `fs` | `fs` | archivo de nivel superior |
| `std/net/net.no` | `net/net` | `net` | último segmento de la ruta |
| `std/net/client.no` | `net/client` | `client` | último segmento de la ruta |
| `std/crypto/sha256.no` | `crypto/sha256` | `sha256` | último segmento de la ruta |
| `std/archive/gzip.no` | `archive/gzip` | `gzip` | último segmento de la ruta |

ShortName es el último segmento de FullPath al dividirlo por `/` (p. ej. `crypto/sha256` → `sha256`).

## Cuándo se requiere el prefijo

Al llamar a funciones o constantes de nivel de módulo definidas en otros módulos, debe usar el prefijo `ShortName.`.

```no
; funciones de nivel de módulo
sha256.sha256(data)
sha256.sha256-hex(data)
fs.open(path, opts)
gzip.gzip-decompress(data)
math.pow(x, y)

; constantes de módulo
net.NET-BUF-SIZE
math.PI
```

### Convención de nombres de funciones

**No añada el prefijo del nombre del módulo al nombre de la función.** Las funciones dentro de un módulo necesitan solo nombres breves e intuitivos; el prefijo `ShortName.` aporta automáticamente la información del módulo al llamarlas desde fuera.

```no
; ✅ Correcto: nombre de función breve, sin prefijo de módulo
; tail.no
; la función de entrada usa directamente el nombre del módulo
tail = () {
    ...
}

; las funciones auxiliares usan nombres breves
atoi = (s str) (v i64) {
    ...
}
```

```no
#{fmt-skip}
; ❌ Evitar: nombres de función con prefijo de módulo redundante
; tail-run = () { }
; tail-atoi = (s str) (v i64) { }
```

La coherencia se mantiene también en las importaciones entre módulos:

```no
; ✅ breve e intuitivo
# /src/tail.tail
# /src/mktemp.mktemp
```

```no
#{fmt-skip}
; ❌ redundante
; # /src/tail.tail-run
; # /src/mktemp.mktemp-run
```

> **Evite las palabras clave**: `awy` (palabra clave de grupo de corrutinas), `match` (palabra clave de correspondencia condicional), etc. no se pueden usar como nombres de función. `run` ya se ha eliminado y puede usarse como identificador normal. Se recomienda que la función de entrada use directamente el nombre del módulo (p. ej. `ping.no` → `ping`).

## Cuándo no se requiere el prefijo

Los casos siguientes no requieren prefijo:

### 1. Funciones globales (`with-cap` / `with-len` / `with-cap-len` / `print` / `eprint` / `format` / `exit`)

Estas 7 funciones son intrínsecas globales del lenguaje y se pueden usar directamente, sin prefijo de módulo. Sus declaraciones en forma de comentario están recogidas en `std/global.no` para que los desarrolladores puedan consultarlas.

**Constructores de capacidad / longitud:**

- `with-cap(cap)` — crea una cadena o rebanada con la capacidad indicada (len=0); el tipo se infiere del lado izquierdo de la asignación.
- `with-len(len)` — crea una cadena o rebanada con la longitud indicada.
- `with-cap-len(cap, len)` — crea una cadena o rebanada con la capacidad y la longitud indicadas.

**Salida / formato:**

Nolang usa la sintaxis de **cadenas de formato con nombre** `{name[:spec]}`, que referencia directamente las variables del ámbito sin argumentos posicionales. Admite verificación en tiempo de compilación. La salida se escribe directamente mediante las llamadas al sistema `io.out`/`io.err`, sin depender del `printf` de libc.

- `print(s)` / `print(s0, s1, ...)` — escribe en la salida estándar; los argumentos múltiples se separan con espacios y **se añade automáticamente un salto de línea**.
- `eprint(s)` / `eprint(s0, s1, ...)` — escribe en la salida de error; los argumentos múltiples se separan con espacios y **se añade automáticamente un salto de línea**.
- `format(s)` — devuelve la cadena formateada (sustituye a `sprintf`); no añade salto de línea.

**Control de procesos:**

- `exit(code)` — termina el proceso con el código de estado indicado (antes `os.exit`, ahora promovido a intrínseco global, sin prefijo `os.`).

**Con varios argumentos, los literales son plantillas por defecto, no cadenas de formato estilo C.** En `print('result={val}', 42, 'result={val}')`, cada literal de cadena es **individualmente** una plantilla: su `{val}` toma el valor del **ámbito del punto de llamada**, sin relación con los demás argumentos de esa misma llamada. Un literal **no** describe ni consume los argumentos que le siguen — no es una sustitución como `printf('%d', 42)`. Los argumentos no literales (variables, expresiones) se tratan como argumentos variables normales, separados por espacios, con un único salto de línea al final de toda la línea.

```no
val = 42
print('result={val}', 42, 'result={val}'); result=42 42 result=42
tmpl = '{val}'
print(tmpl); {val} —— la variable es texto plano, no se sustituye
print('a={val}', val); a=42 42
```

`format(s)` y el ya obsoleto `sprintf(s)` siguen aceptando **una única cadena de formato**, sin forma multiargumento.

> `printf` y `eprintf` se han **eliminado**: los nombres siguen en la tabla de símbolos (las llamadas se siguen resolviendo y el editor sigue completándolas),
> pero **cualquier llamada es un error de compilación duro** (`[printf-depr]`), con un mensaje que guía directamente la migración. `sprintf` sigue disponible pero está **obsoleto**.
> Las sustituciones son:
> - `printf(s)` → `print(s)` (añade salto de línea) o `io.out(s)` (sin salto, salida estándar)
> - `eprintf(s)` → `eprint(s)` (añade salto de línea) o `io.err(s)` (sin salto, salida de error)
> - `sprintf(s)` → `format(s)` (devuelve la cadena formateada)
>
> `io.out`/`io.err` son comandos de bajo nivel cuya salida **no añade salto de línea**. Como las llamadas a módulo deben llevar el nombre del módulo, `io.err` deja explícito el prefijo y no choca con el constructor de Option `err()`; aunque hubiera colisión de nombres, el prefijo de módulo los distingue.

```no
; constructores de capacidad/longitud (sin prefijo)
s str = with-cap(256); ✅ reserva un str de 256 bytes
v []i64 = with-cap(100); ✅ reserva una rebanada de 100 elementos
v []i64 = with-cap-len(200, 100); ✅ capacidad 200, longitud 100
v []i64 = with-len(100); ✅ rebanada de longitud 100

; salida/formato (sin prefijo)
print('hello {n}'); ✅ añade salto de línea
print(a, b, c); ✅ varios argumentos, separados por espacios, con salto
print('x={n}', n); ✅ el literal es plantilla, la variable es valor
print('result={val}', 42, 'result={val}'); ✅ cada literal se sustituye por separado
print(); ✅ sin argumentos, solo emite un salto de línea
s = format('x={x}'); ✅ devuelve la cadena formateada (una sola cadena de formato)
eprint('err: {n}'); ✅ escribe en stderr y añade salto
eprint('err:', a, b); ✅ varios argumentos en stderr separados por espacios
print('número {id:06} importe {money:.2f}'); ✅ admite alineación, relleno, ancho y precisión

; comandos de bajo nivel (requieren prefijo de módulo)
io.out('no-newline-here'); ✅ salida sin salto de línea (sustituye a printf)
io.err('err-no-newline'); ✅ stderr sin salto de línea (sustituye a eprintf)

; todas las demás llamadas entre módulos requieren prefijo
fs.open(path, opts); ✅ con prefijo (los builtin también lo requieren)
```

#### Especificadores de formato

En `{name[:spec]}`, el campo `spec` admite los siguientes elementos (en orden fijo, todos opcionales):

```
[[fill]align][sign][#][0][width][.precision][type]
```

- `fill` — carácter de relleno (espacio por defecto), debe usarse junto con `align`.
- `align` — `<` alineación a la izquierda, `>` a la derecha, `^` centrado.
- `sign` — `+` muestra el signo en positivos, `-` solo en negativos (por defecto).
- `#` — prefijo de base (`0x`/`0o`/`0b`).
- `0` — rellena con ceros a la izquierda hasta el ancho indicado.
- `width` — ancho mínimo del campo.
- `.precision` — número de decimales de coma flotante / longitud de truncamiento de cadena.
- `type` — `d` (entero), `x`/`X` (hexadecimal), `o` (octal), `b` (binario), `c` (carácter), `f` (punto fijo), `e`/`E` (notación científica), `g`/`G` (general), `s` (cadena, por defecto), `t` (nombre del tipo de dato, determinado en compilación), `v` (valor literal; las cadenas entre comillas simples, los vec llaman a to-str).

```no
x i64 = 42
u u64 = 255
pi f64 = 3.14159
s str = 'hello'
print('{x:06}'); 000042
print('{x:>10}'); alineado a la derecha con ancho 10
print('{u:#x}'); 0xff
print('{pi:.2f}'); 3.14
print('{pi:8.3e}'); 3.142e+00
print('{s:<10}'); hello     （alineado a la izquierda）
print('{s:.3}'); hel（truncado a 3 caracteres）
print('{x:t}'); i64（muestra el nombre del tipo de la variable x）
print('{s:v}'); 'hello'（cadena entre comillas simples）
```

`{{` y `}}` sirven para emitir llaves literales `{` y `}`, pero **solo funcionan si en la misma cadena hay al menos un campo `{name}` auténtico**. Un literal que solo contiene escapes y ningún campo se emite tal cual: `print('{{n}}')` imprime `{{n}}` (no `{n}`) y `print('{{')` imprime `{{`. Para imprimir una llave suelta basta con `print('{')` / `print('}')`.

### 2. Definido en el mismo archivo

Las funciones, constantes y métodos definidos dentro del mismo archivo `.no` se usan directamente, sin prefijo.

```no
; en sha256.no:
sha256(data); sha256 está definida en este archivo

; la constante está definida en este archivo
HMAC-BLOCK-SIZE
```

### 3. Métodos de tipos integrados

Los métodos de los tipos integrados (`str`, `i64`, `vec`, `arr`, `byte`, `char`, `bool`, etc.) no requieren prefijo. Los métodos están incorporados al tipo y se resuelven directamente a través del tipo del receptor.

```no
'hello'.starts-with('he'); método de str
n.to-str(); método de int
v.push(42); método de vec
a.contains(3); método de arr
c.is-digit(); método de char
```

### 4. Métodos de instancia de struct

Llamar a métodos de una instancia de struct ya creada no requiere prefijo de módulo. Los métodos se resuelven a través del tipo de la instancia, y el compilador encuentra automáticamente la definición `struct.method` correspondiente.

```no
f = fs.open(path, opts); fs.open es una función de nivel de módulo, requiere prefijo
f.read(buf, n); file.read es un método de struct, no requiere prefijo
f.close(); file.close es un método de struct, no requiere prefijo

p = path {
    p: '/tmp'
}
p.exists(); path.exists es un método de struct, no requiere prefijo
```

## Llamada a métodos frente a llamada a funciones de módulo

Que una llamada a método requiera o no prefijo depende del **propietario del método**:

- **métodos de tipos integrados** (`str.starts-with`, `i64.to-str`, etc.) — no requieren prefijo.
- **métodos de instancias de struct** (`f.read`, `p.exists`, etc.) — no requieren prefijo.
- **funciones de nivel de módulo** (`fs.open`, `sha256.sha256`, etc.) — **requieren prefijo**.

En `fs.fil()`, `fs` es el ShortName del módulo y `fil` es el nombre de una función de nivel de módulo. El prefijo `fs.` no se puede omitir, porque aquí `fs` no es un nombre de variable, sino una ruta de módulo.

### Las dos semánticas de `Name.Function`

Las escrituras `process.cmd(...)` y `p.start(...)` son ambas `xxx.yyy()`, pero su semántica es totalmente distinta:

| Escritura | `xxx` | `yyy` | Semántica |
| --- | --- | --- | --- |
| `process.cmd(...)` | ShortName del módulo | función de nivel de módulo | `xxx` es la ruta del módulo, `yyy` es una función independiente definida en ese módulo |
| `p.start(...)` | variable de instancia | método de struct | `xxx` es una variable de tipo `process`, `yyy` es un método definido como `process.start = ...` |

**Diferencia al definirlos**:
- las funciones de nivel de módulo se definen **sin prefijo**: dentro del módulo se escribe directamente `cmd = (program str, ...) { }`
- los métodos de struct se definen **con el prefijo `struct.`**: `process.start = (program str, ...) { }`

**Diferencia al llamarlos**:
- las funciones de nivel de módulo se llaman desde fuera con `ModuleName.function()`: `process.cmd(...)`
- los métodos de struct se llaman a través de una instancia: `p = process.new()` → `p.start(...)`

> **Nota**: incluso dentro del módulo, llamar a una función de nivel de módulo del mismo módulo no lleva prefijo (`cmd(...)`), mientras que los métodos de struct se invocan mediante `self` implícito o con la sintaxis `.method()`.

## Referencias a tipos de otros módulos

Al referirse a tipos (structs, interfaces, enumeraciones, etc.) definidos en **otros módulos**, debe usar el prefijo `ShortName.`. Esto se aplica a:

### Un struct implementa una interfaz

Cuando un struct implementa una interfaz definida en otro módulo, el nombre de la interfaz debe llevar el prefijo del módulo:

```no
#{fmt-skip}
; ❌ Error: db, rows y stmt son interfaces definidas en el módulo sql, no se puede omitir el prefijo
db-mysql db {
    fd i64
}
```

```no
; ✅ Correcto: usar sql.db, sql.rows, sql.stmt
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

### Parámetros y tipos de retorno de funciones

Los tipos de otros módulos en la firma de una función también requieren prefijo:

```no
; ✅ Correcto: el tipo de retorno usa sql.result
db-mysql.exec = (sql str) (r sql.result) {
    ...
}
```

### Tipos de campos de struct

```no
; ✅ Correcto: el tipo del campo usa sql.connection
conn-mysql sql.db {
    handle sql.connection
}
```

### Casos que no requieren prefijo

- **Tipos definidos en el mismo módulo**: los structs, interfaces y enumeraciones definidos en el mismo archivo `.no` se usan directamente por su nombre.
- **Tipos integrados**: `str`, `i64`, `bool`, `byte`, etc. no requieren prefijo.
- **Interfaces integradas**: `enter`, `leave`, etc., propias del lenguaje, no requieren prefijo.

```no
; tipos definidos en el mismo archivo, sin prefijo
result {
    last-id i64
    affected i64
}

; enter/leave son interfaces integradas, sin prefijo
; result es un struct definido en el mismo archivo, sin prefijo
db {
    enter
    leave
    close() (ok bool)
    exec(sql str) (r result)
}
```

## Ejemplo completo

```no
; los módulos de la biblioteca estándar se cargan automáticamente, sin importación explícita

; ─── sin prefijo ───

; funciones globales (declaradas en global.no)
s str = with-cap(256)
v []i64 = with-len(100)
print('hello {n}')
s = format('x={x}')

; funciones del mismo archivo
sha256(data)

; métodos de tipos integrados
'hello'.starts-with('he')
n.to-str()
v.push(42)

; métodos de instancia de struct
f = fs.open(path, opts)
f.read(buf, n)
f.close()

; ─── con prefijo ───

; funciones de nivel de módulo
sha256.sha256(data)
sha256.sha256-hex(data)
fs.open(path, opts)
gzip.gzip-decompress(data)
math.pow(x, y)

; constantes de módulo
net.NET-BUF-SIZE
math.PI

; referencias a tipos de otros módulos (implementación de interfaz, tipo de parámetro, tipo de retorno, tipo de campo)
db-mysql sql.db {
    fd i64
}

r sql.result = d.exec('CREATE TABLE ...')
```
