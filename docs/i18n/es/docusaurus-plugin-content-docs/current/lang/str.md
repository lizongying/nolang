---
sidebar_position: 3
---

# Cadenas

Las cadenas de Nolang (`str`) son secuencias de bytes asignadas en el heap, con diversos operadores y métodos.

## Layout interno y API pública

El descriptor de `str` (`%str-long`) ocupa **16 bytes** en plataformas de 64 bits:

```llvm
%str-long = type { i32, i32, i8* }   ; u32 len, u32 cap, data
```

| Campo | Tipo interno | Tipo público | Descripción |
|---|---|---|---|
| `len` | `u32` (LLVM `i32`) | `i64` | longitud lógica en bytes; `s.len-bytes()`, `len(s)`, `s._len()` |
| `cap` | `u32` (LLVM `i32`) | `i64` | capacidad física (solo válido en str-long); `s.cap()`, `s._cap()` |
| `data` | `i8*` | `[]byte` / `*byte` | puntero al búfer del heap |

Internamente `len` / `cap` se estrechan a `u32` para reducir el descriptor de 24 a 16 bytes (`i32` + `i32` + un puntero alineado a 8 bytes). **La API pública sigue siendo siempre `i64`**: en lectura se aplica `zext i32 -> i64` en la frontera, y en escritura `trunc i64 -> i32`. La biblioteca estándar y el código de usuario no necesitan (ni deben) tocar esta conversión; las firmas de `s.len-bytes()`, `s.len()`, `s.cap()`, `s._len()` / `s._cap()` / `s._set-len()` / `s._set-cap()` siguen siendo `i64`.

> ⚠️ El contador interno es `u32`, por lo que la longitud / capacidad máxima de una sola `str` es 4 GiB (`4294967295`). Un `i64` mayor se trunca en la frontera; el procesamiento normal de cadenas no se ve afectado, pero conviene comprobarlo antes de preasignar búfers gigantes.
>
> `txt` **no se ve afectado por este cambio**: sigue siendo una estructura fija de 256 bytes `{ [255 x i8] data, i8 len }`, sin campo `cap` y con una longitud de 1 byte.

## Operadores de cadena

### Concatenación (`-`)

Use el operador `-` para concatenar cadenas:

```no
; concatenación de literales
s = 'Hello' - ' ' - 'World'

; concatenación con una variable
greeting = 'Hello, ' - name
```

### Repetición (`*`)

Use el operador `*` para repetir una cadena:

```no
s = 'Hello' * 3
```

### Comparación (`==` / `!=` / `str.compare`)

`str` **solo implementa la comparación de igualdad**: `==` / `!=` usan `@str_eq` del runtime, y las cadenas de varios caracteres son perfectamente válidas.

Los operadores relacionales (<code>&lt;</code> `>` <code>&lt;=</code> `>=`) **no tienen semántica de orden lexicográfico para cadenas**; usarlos con cadenas es un error de compilación:

- **Válido**: literales de cadena de **un solo carácter** `'a'`, tratados implícitamente como `char` (punto de código); se pueden comparar con otro literal de un carácter o con un `char` (`"a"`).
- **Error**: cadenas de varios caracteres — `'ab'`, variables `str` o llamadas que devuelven `str` (antes **devolvían `false` silenciosamente**).

Cuando necesite orden lexicográfico, use `str.compare(b)`:

```no
s = 'abc'
b = 'abd'
c = s.compare(b); -1（s < b）/ 0（iguales）/ 1（s > b）

'a' <= 'z'; true — literal de un carácter implícitamente char
ch >= 'a'; ch es char, correcto
s < b; error de compilación: cadena de varios caracteres
s.compare(b) < 0; forma correcta
```

## Indexación y rebanadas

**Indexación**: el tipo inferido de `s[i]` es **`?char`** (punto de código Unicode), no `byte` — fuera de límites devuelve `err` (`code.index-out`) y dentro devuelve `ok(punto-de-código)` (el mismo esquema unificado que `+ - * / %` para enteros). Para cadenas con caracteres multibyte, `s[i]` decodifica UTF-8 desde el inicio hasta contar el i-ésimo punto de código (véase la nota de rendimiento más abajo).

```no
#{fmt-skip}
s = 'Hello World'
c = s[0]: { ok -> it  -> _ }   ; punto de código de 'H' (?char debe desempaquetarse explícitamente; brazo vacío toma '\0')
```

> Solo hay tres formas de consumir un `?char` (**`#{index-out=DEF}` quedó obsoleto y se eliminó en D11**; los comentarios residuales son error de compilación;
> tampoco existe el atajo de «extraer un plain explícitamente»: `c char = s[i]` también debe consumirse explícitamente):
> **desempaquetado con match** (`x = s[i]: { ok -> it, -> _ }`, brazo vacío toma el valor cero de `char`, `'\0'`),
> **escritura place-match** (`s[i]: { ok -> it = x, -> _ }`) o **propagación abreviada con `?=`** (la función tiene un parámetro de resultado `?T`, o la sentencia está dentro del cuerpo de un match de bloque). Véase ⟨syntax · Indexación segura⟩ para detalles y ejemplos.

**Rebanadas**: `s[a..b]` devuelve una **vista** de la cadena subyacente, cuyo tipo sigue siendo **`str`** (memoria compartida, sin copia). Los índices de la rebanada son **posiciones de punto de código**, igual que en `s[i]`:

```no
sub = s[6..]; 'World'
sub = s[6..11]; 'World'
sub = s[0..5); 'Hello'（izquierda cerrada, derecha abierta）
```

> Los índices de la rebanada son **posiciones de punto de código**, no desplazamientos en bytes. Si necesita una rebanada por desplazamiento de bytes (p. ej. para iterar con `s.byte(i)`), use `s.slice-bytes(start, end)`.

```no
; longitud
n = s.len(); número de puntos de código (caracteres Unicode)
n = s.count(); ídem (nombre de método antiguo, aún disponible)
n = s.len-bytes(); longitud en bytes (número de bytes UTF-8)
; atención: el campo suelto s.len ya no se admite, leerlo o escribirlo da error; use s.len() / s.len-bytes()
```

### Conversión implícita (char → str)

Un `char` se puede **convertir implícitamente** a `str`: el punto de código se codifica en UTF-8 como una nueva cadena. Por tanto, todas las formas siguientes son válidas **sin** llamar manualmente a `char.to-str()`:

```no
s = 'héllo'; 'h' 'é' 'l' 'l' 'o'（é es multibyte）

; 1) el char de s[i] se asigna directamente a una variable str → se convierte en cadena de un carácter
a str = s[0]; 'h'

; 2) el resultado de la rebanada ya es de tipo str, se asigna directamente
b str = s[0..1]; 'hé'（índices 0..1, ambos extremos incluidos）

; 3) un char en una concatenación de cadenas (-) se convierte implícitamente a str
msg = 'first: ' - s[0]; 'first: h'

; 4) un char comparado con una str se convierte implícitamente a str
ok = s[0] == 'h'; true
```

> **`'...'` es un literal `str` y `"..."` es un literal `char`** — aunque `'x'` tenga un solo punto de código, sigue siendo `str`. Por eso, cuando **ambos lados** de `+` / `-` son literales (`'...'` con `"..."`), se aplica la conversión implícita anterior y el resultado es **concatenación**, no una operación entera:
>
> ```no
> 'x' - "a"                ; "xa"（concatenación; no 120-97 = 23）
> "a" + 'x'                ; "ax"
> 'xy' + "a"               ; "xya"
> ```
>
> En cuanto un lado **no es literal** (variable char, `s[i]`, entero), el `'...'` de un carácter participa como **byte** en la operación entera — esto es deliberado y es lo que permite la idiomática extracción de dígitos numéricos:
>
> ```no
> c char = "5"
> n = c - '0'              ; 5（resta de bytes, no "50"）
> m = 'A' + 1              ; 66
> "z" - "a"                ; 25（ambos literales char, resta de chars）
> 'x' - c                  ; 23（c es variable → operación de bytes）
> ```
>
> Dicho esto, la degradación a byte **solo ocurre cuando el otro lado es entero o carácter**. Si el otro lado es un valor **sin semántica de byte**, como `bool`, `f64` o un struct, `'...'` sigue siendo cadena y toda la expresión es una **concatenación**, con el otro lado renderizado mediante `to-str`:
>
> ```no
> b = true
> 'x' - b                  ; "xtrue"（no 120 - 1 = 119）
> 'x' - 1.5                ; "x1.5"
> 'x' - 'y'                ; "xy"（ambos literales de cadena）
> ```
>
> Así, `'x' - b` se puede asignar a `str`: `s str = 'x' - b` da `"xtrue"`; en cambio `s str = 'x' - 1` se rechaza, porque es una operación de bytes (el resultado es numérico).

> La conversión implícita codifica solo **un único punto de código** como `str`. Si lo que quiere es el valor numérico, use el `char` directamente (p. ej. `print(c)` imprime el valor decimal del punto de código); si quiere una conversión explícita, sigue disponible `c.to-str()` (equivalente a la implícita).
>
> `char` representa un único punto de código escalar Unicode y su tipo de almacenamiento subyacente es **`i32`** (rango válido `0 ..= 0x10FFFF`).
>
> `char` **puede participar normalmente en operaciones enteras** (p. ej. `z = a + 25`, `u = ch - 32`), calculadas con el valor del punto de código en anchura i32; a diferencia de la familia entera, no devuelve por defecto `option<int>`, así que no dispara el error de compilación «desbordamiento entero no gestionado».
>
> **Renderizado**: al convertir un `char` a texto siempre se emite el **valor decimal del punto de código** (`"A"` → `65`, `"é"` → `233`), y `print(c)` y la interpolación `print('{c}')` **deben dar el mismo resultado**. La aritmética de char se calcula como **i32 con signo**, así que `"x" - "y"` es `-1` y ambas vías emiten `-1` (nunca una emite `4294967295`). Los puntos de código válidos están siempre en `0 ..= 0x10FFFF` (el bit 31 es 0), donde la extensión con y sin signo son equivalentes, así que esta regla solo afecta a valores negativos obtenidos por desbordamiento en aritmética de char. Si quiere el **carácter en sí** y no el número, use `c.to-str()` (`"A".to-str()` → `"A"`), que es otra operación distinta.

## Optimización ASCII y consejos de rendimiento

El subíndice `s[i]` de `str`/`txt` toma el **i-ésimo punto de código (carácter Unicode)**, no el i-ésimo byte. Por tanto, para cadenas que puedan contener caracteres UTF-8 multibyte (como chino, emoji o `é`), cada `s[i]` debe iterar UTF-8 desde el inicio de la cadena hasta contar el i-ésimo punto de código — complejidad **O(n)**.

Cuando el compilador puede **demostrar** que la cadena se compone solo de caracteres ASCII (puntos de código 0–127), cada byte es exactamente un carácter y `s[i]` indexa directamente el byte subyacente, degradándose a **O(1)**. El compilador demuestra que una cadena es ASCII puro mediante estas reglas:

- anotación explícita: `#{ascii}` en la declaración o asignación;
- asignación de un literal de cadena ASCII (p. ej. `'hello'`);
- propagación por identificador: `b = a`; si se ha demostrado que `a` es ASCII, `b` también lo es;
- ambos lados de una concatenación (`-`) ya se han demostrado ASCII.

```no
; literal ASCII → el compilador demuestra ASCII puro, s[i] es O(1)
name = 'hello'
c = name[1]; O(1), toma el byte 'e'

; contiene caracteres multibyte → no se puede demostrar ASCII, s[i] es iteración O(n) de puntos de código
s = 'héllo'
c = s[1]; O(n), toma el primer punto de código 'é'（no el byte）
```

### Advertencia de rendimiento (LSP / `no vet`)

Cuando **itera manualmente por índices de punto de código dentro de un bucle** sobre un `str`/`txt` que no se ha demostrado ASCII (es decir, escrituras como `for i <- [0..s.count()): { s[i] }` que toman caracteres uno a uno por índice de punto de código), `no vet` y el LSP del editor emiten una alerta estática de nivel **WARNING** (no prohíben la sintaxis, solo avisan). Esta forma hace que cada `s[i]` tenga que iterar UTF-8 desde el inicio para obtener el i-ésimo punto de código, por lo que el bucle completo se degrada a **O(n²)**.

Un `s[i]` suelto (p. ej. `s[5]`, solo O(n) una vez) **no** genera alerta; iterar directamente `for c <- s` tampoco **la** genera (véase abajo).

Se recomienda usar formas más rápidas:

- Si necesita recorrer cada carácter, use directamente `for c <- s` hacia adelante: es la forma preferida de un único barrido O(n), que evita el O(n²) de `s[i]` repetido. El compilador avanza por **puntos de código UTF-8 (caracteres Unicode)** y guarda en la variable de bucle `c` el valor del punto de código decodificado, semanticamente equivalente a `for c <- s.to-chars()`:
  ```no
  c <- s: {
      ; c es el punto de código actual, de tipo char (decodificado UTF-8, no un byte)
  }
  ```
- Si solo necesita acceso por bytes (p. ej. codificación/decodificación, hash, memcmp), use la escotilla de escape `s.byte(i)`: siempre indexa directamente el i-ésimo byte y es **O(1)** constante, sin entrar en el cuerpo de funciones std:
  ```no
  b = s.byte(0); toma el byte 0, de tipo ?byte, O(1)
  out = 0
  b: {
      ok -> {
          out = it; dentro de límites: ok(byte)
      }

      -> {
          out = 0; fuera de límites: nil (brazo vacío sintetiza el valor cero)
      }
  }
  ```
  > El resultado de `s.byte(i)` es **`?byte`** (desde 2026-10-21): antes la lectura de bytes suelta no hacía comprobación de límites y un índice fuera de rango era comportamiento indefinido. Ahora fuera de límites devuelve `nil` — atención, esto **difiere** de la indexación de contenedores `arr[i]` / `s[i]`, que fuera de límites devuelve `err` (`code.index-out`): `s.byte(i)` es una escotilla de escape a nivel de byte, y «no hay tal byte» no es un error.
  > (Véase design/option-unified.md §3.1, punto 5). Por tanto `b i64 = s.byte(0)` da
  > `15w45dqk`: una anotación plain explícita junto a una llamada option es desempaquetado silencioso y constituye error duro de compilación;
  > use `?=`, un match de dos tramos (`b = s.byte(0): { ok -> it, -> _ }`) o `_ =` para consumirlo.
  > `txt.byte(i)` tiene la misma semántica (el field 1 de `txt` es la longitud en bytes).

> El acceso a bytes dentro de la biblioteca estándar (src/std) ya se ha migrado por completo a `s.byte(i)`: codificación/decodificación UTF-8 (`decode-cp`) en `str.no`/`txt.no`, comparación (`compare`/`starts-with`/`ends-with`), hash, copia de rebanadas (`slice`/`repeat`/`trim`/`copy`), `to-bytes`/`to-upper`/`to-lower`/`reverse`/`replace-char`/`count`/`index`, etc. Por tanto `no vet` ya no necesita omitir los archivos de la biblioteca estándar; el acceso a elementos `x[i]` de los demás tipos `[]byte`/rebanadas/arreglos ya indexa por elemento (byte), con la misma semántica y sin generar alerta.

| Escritura | Semántica | Complejidad | Descripción |
|---|---|---|---|
| `s[i]` (ASCII demostrado) | el i-ésimo byte = el i-ésimo carácter | O(1) | demostración automática, sin cambios |
| `s[i]` (ASCII no demostrado, dentro de bucle) | el i-ésimo punto de código | O(n²) antipatrón | dispara alerta del LSP |
| `s[i]` (ASCII no demostrado, fuera de bucle) | el i-ésimo punto de código | O(n) una vez | sin alerta |
| `s.byte(i)` | el i-ésimo byte (`?byte`, `nil` fuera de límites) | O(1) | escotilla de escape de acceso a bytes |
| `for c <- s` | punto de código por punto de código | O(n) un único barrido | forma preferida, sin alerta |

## Métodos de cadena

```no
#{fmt-skip}
; comparación
ok = a.eq(b, n)               ; comparación de igualdad (método)
c = s.compare(b)              ; comparación lexicográfica

; búsqueda (index/rindex devuelven «posición de punto de código (carácter)», igual que s[i])
pos = s.index(sub)             ; índice de punto de código de la primera aparición, -1 si no se encuentra
pos = s.index-from(sub, k)     ; busca a partir del punto de código k
pos = s.rindex(sub)            ; índice de punto de código de la última aparición
pos = s.rindex-from(sub, k)    ; búsqueda inversa a partir del punto de código k
pos = s.last-index(sub)        ; alias de rindex
ok  = s.contains(sub)          ; si lo contiene
ok  = s.starts-with(sub)       ; comprobación de prefijo
ok  = s.ends-with(sub)         ; comprobación de sufijo
ok  = s.empty()                ; si está vacía

; atención: la serie index devuelve «posición de punto de código», así que s[s.index(sub)] toma el primer carácter de sub,
;       coherente con la semántica de índice por punto de código de s[i]. Por ejemplo '日本語'.index('語') == 2, y s[s.index('语')] es el punto de código '語'.
;       para rebanadas por «posición de punto de código», s.slice(start, end) tiene semántica de puntos de código (igual que s[i]);
;       para rebanadas por «desplazamiento en bytes» (p. ej. iterando con s.byte), use s.slice-bytes(start, end).
;       internamente str ofrece la búsqueda a nivel de byte str.find-byte-from (reutilizada por split, etc.).
;       atención: slice/at del tipo txt siguen teniendo semántica de bytes (txt es un búfer de bytes crudo), divergiendo de str.

; conversión
out = s.to-upper()             ; a mayúsculas
out = s.to-lower()             ; a minúsculas
out = s.trim()                 ; quita espacios al inicio y al final
out = s.trim-char(c)           ; quita el carácter indicado
out = s.repeat(n)              ; repetición
out = s.reverse()              ; inversión
out = s.slice(start, end)      ; rebanada (índices de punto de código, igual que s[i])
out = s.slice-bytes(b, e)      ; rebanada (índices de desplazamiento en bytes, uso interno)
val = s.replace-char(old, new) ; sustituye un carácter

; conversión a otros tipos
b = s.to-bytes()               ; a []byte
v = s.to-i64()                 ; a ?i64
v = s.to-f64()                 ; a ?f64
v = s.to-bool()                ; a ?bool

; división y unión
parts = s.split(sep)           ; divide (devuelve []str)
out = ss.join(sep)             ; une []str

; copia y relleno
dst = s.copy()                 ; copia de la cadena
s.fill(val byte)               ; rellena con un valor byte
```

## Conversión entre cadenas y números

```no
; número a cadena (métodos)
s = i64.to-str(); i64 a cadena
s = f64.to-str(); f64 a cadena
s = bool.to-str(); bool a "true"/"false"
s = byte.to-str(); byte a cadena
s = char.to-str(); char a cadena

; cadena a número (devuelve option)
v = s.to-i64(); devuelve ?i64
v = s.to-i32(); devuelve ?i32
v = s.to-u64(); devuelve ?u64
v = s.to-f64(); devuelve ?f64
```

## Escritura por subíndice deshabilitada (`str` 2026-11-11 / `txt` 2026-11-12)

> **`s[i] = v` es un error de compilación `idxwrite`**; las tres bases `with-cap` / `with-len` / `with-cap-len` no hacen excepción. **`t[i] = c` en `txt` también está deshabilitado** (el motivo se explica abajo).
>
> En `str`, el motivo es que era la única fuente de «escritura fuera del búfer asignado»: un búfer con len=0 crece mediante escritura directa, y escribir en `i >= cap` sale de la región asignada. La barrera de crecimiento `ensureStrLongBuffer` de codegen se creó precisamente para esto; al deshabilitar la escritura directa, la barrera deja de ser alcanzable por código de usuario y se conserva solo como código defensivo.
>
> En `txt` el motivo es distinto pero igual de grave: `t[i] = c` **recodifica por punto de código y vuelve a escribir**, por lo que la longitud cambia implícitamente con la escritura (`i` igual al número de puntos de código equivale a añadir, y lo que supera 255 se descarta silenciosamente). txt no tiene heap ni expansión, pero «una escritura por subíndice cambia la longitud a escondidas» es el mismo tipo de peligro.

Cuatro formas siguen siendo válidas:

```no
; ① «construir» byte a byte: un búfer []byte recoge los datos y al final se convierte todo a str
b []byte = with-cap-len(n, n)
b[0] = 48
b[i] = src.byte(i): {
    ok -> it

    -> 0
}
s = b.slice(0, k).to-bytes-str(); también vale b.to-bytes-str()（todo el trozo）

; ② «sobrescribir» contenido existente in situ: str.fill rellena los len bytes con el mismo valor, sin cambiar la longitud
s = 'hello'
s.fill(72); 'HHHHH'

; ③ la escritura por subíndice solo queda como place-match (usa la «lectura» como compuerta: si i >= len se omite silenciosamente, sin crecer ni salirse)
s[i]: {
    ok -> it = v
    -> _
}

; ④ para cambiar la longitud: la familia push (una sola llamada explícita y auditable)
s.push("中");      añade por punto de código (UTF-8)
s.push-str('de');  añade por bytes (expansión in situ)
```

> ⚠️ **place-match es una compuerta de «punto de código», no de bytes.** Su compuerta viene de la **lectura** `s[i]` — y la lectura por subíndice de `str` cuenta puntos de código. Por eso ③ solo sirve para el caso ASCII de «un punto de código por casilla» (uuid poniendo guiones, time poniendo dígitos, tablas hexadecimales). Para construir contenido **byte a byte** (bytes `>= 0x80`, o cualquier cadena de origen que pueda no ser ASCII) hay que usar el `[]byte` de ①: se midió que, tras `f = with-len(8)`, escribir 224..231 sucesivamente con place-match solo guarda las 4 primeras casillas y el resto se omite silenciosamente; así se truncaba el «nombre de archivo no ASCII» de `path.split`.

> **Truncamiento de longitud**: ya no se admite `s.len = n` suelto; use una rebanada — `s.slice(0, n)` es rebanada de **puntos de código** y `s.slice-bytes(0, n)` es la de bytes. (`b.slice(0, k)` sobre un búfer `[]byte` es naturalmente una rebanada de bytes.)
>
> **Crecer un str con `push` tras `with-cap(n)`**: `s str = with-cap(256)` es un búfer con len=0 pero con capacidad ya reservada; el primer `push` escribe directamente sin disparar expansión (`push-str` solo hace realloc cuando `len + longitud-añadida` supera `cap`, y crece duplicando). La escritura byte a byte `buf[i] = b` sigue prohibida para str.

### Crecimiento: la familia `push` (2026-11-12)

Tras prohibir la escritura directa, **cambiar la longitud debe ser una única llamada explícita**:

| Método | Semántica |
|---|---|
| `s.push(c char)` | añade un punto de código, codificado en UTF-8 (internamente delega en `push-str`) |
| `s.push-str(t str)` | añade una cadena, copiándola **por bytes**, con expansión in situ |

```no
s = 'ab'
s.push("中");      'ab中', len-bytes 5, len 3
s.push-str('de');  'ab中de', len-bytes 7

t = 'zz'
t.push-str(t);     'zzzz'（autoañadido seguro: primero se conserva la fuente y luego se copia con memmove）

u = ''
u.push-str('');    ''（fuente vacía: no cambia nada ni asigna memoria）
```

> `push-str` es intrínseco del compilador (`#{buildin}`, expandido en MIR como «expansión in situ + memmove + cambio de longitud»),
> no un bucle std: una adición cuesta O(longitud-añadida) y las adiciones repetidas son O(1) amortizado (crecimiento duplicando).
> `push` pasa por codificación UTF-8, así que el incremento de `len-bytes` es la longitud de codificación de ese punto de código y `len` siempre aumenta en 1.

> **La escritura en `txt` también está deshabilitada (2026-11-12)**: `t[i] = c` es una **recodificación a nivel de punto de código con reescritura**, y la longitud cambia implícitamente con la escritura — `i` igual al número de puntos de código equivale a añadir, y lo que supera 255 bytes se descarta silenciosamente.
> «Una escritura por subíndice cambia la longitud a escondidas» es justo lo que la prohibición quiere eliminar; txt se trata igual que str. Las entradas de crecimiento de txt son `t.push(c)` / `t.push-str(s)` / `t.push-txt(u)`; para cambiar un valor in situ se usa place-match.
> Leer `t[i]` sigue devolviendo el punto de código `char`, eso no ha cambiado.
>
> Los escritores crudos a nivel de byte `str._set-byte` / `txt._set-byte` son **privados de std** desde 2026-11-11
> (solo los llama código interno de std como `str.fill` / `txt.push`); el código de usuario no puede usarlos.
> Para construir un `txt` byte a byte, acumule primero un `[]byte` y luego cópielo de una vez con `txt.from-bytes(b)`
> (por encima de 255 bytes se trunca automáticamente y la longitud se fija al número real de bytes).

## Preasignación (sintaxis integrada)

Los tres intrínsecos `with-cap`, `with-len` y `with-cap-len` permiten preasignar memoria en el heap y evitar que los `push` posteriores disparen expansiones repetidas:

```no
; with-cap(cap): asigna capacidad cap, len=0 (hay que hacer push antes de indexar)
s = with-cap(256); str, len=0, cap=256

; with-len(len): asigna capacidad len, len=cap (se puede indexar para leer directamente)
s = with-len(128); str, len=128, cap=128

; with-cap-len(cap, len): especifica a la vez capacidad y longitud, para «reservar espacio de crecimiento»
s = with-cap-len(512, 64); str, len=64, cap=512
```

| Intrínseco | Parámetros | len | cap | Caso de uso |
|---|---|---|---|---|
| `with-cap(cap)` | 1 | 0 | cap | solo se conoce el límite superior; la longitud crece con push |
| `with-len(len)` | 1 | len | len | longitud fija, se puede indexar para leer |
| `with-cap-len(cap, len)` | 2 | len | cap | se conoce la longitud inicial y se quiere reservar espacio para crecer |

> Los parámetros y los valores devueltos por `s.cap()` / `s.len-bytes()` son **`i64`** (la API pública no cambia), pero el descriptor guarda internamente len / cap como **`u32`**. La parte llamadora sigue viendo valores de 64 bits; la conversión la hace el compilador en la frontera del descriptor; véase ⟨Layout interno y API pública⟩. Las escrituras de campo como `s.len = n` / `s.cap = n` pasan también por esta frontera, por lo que no sobrescriben campos contiguos del header.

> ⚠️ **El búfer de `str` nunca permite escritura directa por subíndice** (desde 2026-11-11, las bases `with-cap` tampoco están exentas).
> La exención de `idxwrite` para with-cap ahora solo sirve a **`[]byte` / `[]T` / arreglos fijos**: sus búferes con len=0 siguen creciendo mediante `buf[i] = b` y no tienen una forma equivalente de hacerlo. Para `str`, use un búfer `[]byte` + `to-bytes-str()` (véase la sección ⟨Escritura por subíndice deshabilitada⟩).
> Con `with-len(n)` / `with-cap-len(cap, len)` el len ya está colocado; **la escritura debe hacerse con place-match `buf[i]: { ok -> it = v / -> _ }` o con `buf[i] ?= v`** — igual que cualquier otro contenedor, la escritura directa es error de compilación `idxwrite`. (Desde 2026-11-10 `with-len` ya no está exento.)

> **Inferencia de tipo**: el tipo de resultado de los tres intrínsecos se infiere del lado izquierdo de la asignación y se puede usar con `str` o con rebanadas `[]T`.
