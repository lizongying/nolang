---
sidebar_position: 3.1
---

## Tipos básicos

### types — Definiciones de tipos

Correspondencia entre los tipos de Nolang y LLVM:

| Nolang           | LLVM                                               |
| ---------------- | -------------------------------------------------- |
| `bool`           | `i1`                                               |
| `byte`           | `i8`                                               |
| `char`           | `i32`                                              |
| `i8/i16/i32/i64/i128` | `i8/i16/i32/i64/i128`                             |
| `u8/u16/u32/u64/u128` | `i8/i16/i32/i64/i128`                          |
| `f32`            | `float`                                            |
| `f64`            | `double`                                           |
| `str`            | `%str-long = { i32, i32, i8* }` (u32 len, u32 cap, data; 16 bytes en 64 bits, asignado en el montón) |
| `[]t`            | `%vec = { i32, i32, i64 }` (u32 len, u32 cap, ptrtoint data; 16 bytes en 64 bits, asignado en el montón) |
| `[n]t`           | `[n x t]` (agregado simple, elementos en línea; **sin descriptor len / cap**) |
| `txt`            | `{ [255 x i8], i8 }` (fijo de 256 bytes) |

> **Tanto `str` como `[]t` guardan len / cap como `u32` internamente pero los exponen como `i64`.** En el límite del descriptor, las lecturas se amplían (`zext i32 -> i64`) y las escrituras se estrechan (`trunc i64 -> i32`); las firmas de `s.len-bytes()`, `s.len()`, `s.cap()` y de los privados `_len` / `_cap` / `_set-len` /
> `_set-cap` siguen siendo `i64`, y lo mismo ocurre con `v.len()`. Consulte [Cadenas: diseño interno y API pública](../lang/str.md)
> y [Modelo de memoria: diseño de tipos](../lang/memory.md#型別佈局).
> El campo `data` de `%vec` se mantiene deliberadamente como `i64` (codificado con `ptrtoint`), a diferencia del `i8*` de `%str-long`, por lo que ambos
> **ya no comparten el mismo diseño** y no se puede acceder a ellos con `getelementptr` como si fueran el mismo descriptor.
> `txt` no tiene campo `cap`, su longitud se guarda en un `i8` (0-255) y **no adopta u32**.

**Tipos compuestos:**

- **Array de longitud variable `[]t`**: subyacentemente `%vec = { u32 len, u32 cap, i64 data }` (16 bytes; `cap == 0` indica una vista prestada que no posee el búfer)
- **Array de longitud fija `[n]t`**: array de tamaño fijo de LLVM `[n x t]`, elementos en línea, sin descriptor en tiempo de ejecución; `.len` es la constante de compilación `n` y no hay `cap`
- **Cadena `str`**: secuencia de bytes asignada en el montón con descriptor `{ u32 len, u32 cap, i8* data }` (16 bytes en 64 bits); admite `s[i]`, `s[i..j]` y `s + t`. La longitud y la capacidad se exponen como `i64` (`s.len-bytes()` / `s.cap()`) y se almacenan como `u32` internamente
- **Cadena fija `txt`**: estructura fija de 256 bytes `{ [255]byte data, byte len }`; los primeros 255 bytes almacenan los datos y el último byte guarda la longitud en **bytes** (0-255), sin necesidad de asignación en el montón; requiere anotación de tipo (`t txt = 'abc'`). `t.len()` devuelve el número de puntos de código (alineado con `str`, equivalente a `t.count()`) y `t.len-bytes()` devuelve el número de bytes; la indexación `t[i]` **lee** en unidades de **punto de código** (devuelve el `char` de ese punto de código; si está fuera de rango devuelve `err`, `code.index-out`). **Escribir `t[i] = c` está prohibido globalmente desde el 2026-11-12** (error de compilación `idxwrite`): vuelve a codificar `c` en UTF-8 y desplaza los bytes finales, de modo que la longitud cambia implícitamente con la escritura (se añade cuando `i` es igual al número de puntos de código y se descartan silenciosamente los bytes que superan 255). Para hacerlo crecer use `t.push(c char)` / `t.push-str(s)` / `t.push-txt(u)`; para modificar un valor in situ use place-match `t[i]: { ok -> it = c }`; para lecturas a nivel de byte use `t.byte(i)` (devuelve `?byte`, nil si está fuera de rango); para ensamblar por bytes, simplemente acumule un `[]byte` y cópielo de una vez con `txt.from-bytes(b)` (se trunca a partir de 255 bytes y la longitud se fija según el número real de bytes). El escritor a nivel de byte `txt._set-byte` es **privado de std** desde el 2026-11-11 y el código de usuario no puede llamarlo. `s[i] = v` de `str` se **prohibió globalmente** el 2026-11-11 (error de compilación `idxwrite`; las bases `with-cap` tampoco quedan exentas), por lo que en `str` solo quedan `s.fill(val byte)`, el place-match `s[i]: { ok -> it = v }` y los métodos de crecimiento `s.push(c)` / `s.push-str(s)`
- **Enumeración/Union**: enumeración etiquetada `option` (`ok t` / `nil` / `err str`)
- **Estructura**: debe definirse en varias líneas; los campos no se separan con comas
- **Mapa**: subyacentemente linked-hash-map
- **Iterador**: `for iter.next() {}` (método de interfaz `next() (ok bool)`)

### option — Tipo opción

Enumeración etiquetada `option<t>` (tag=0=val, 1=nil, 2=err):

```no
#{fmt-skip}
x ?t                ; Declarar option<t>
x = 42              ; Establecer con valor
x = nil             ; Establecer vacío
x = err('msg')      ; Establecer error

; match
x: {
    val -> f(it)
    nil ->
    err -> g(it.msg())
}  
```

**Guía de estilo:** cuando una función puede fallar o devolver un valor vacío, use la opción `?t` en lugar de `(val, ok bool)`. `?t` tiene tres estados: `ok` (con valor), `nil` (vacío/ausencia normal) y `err` (error). El valor normal se vincula implícitamente. Por ejemplo, `pop()` devuelve `?i64` (`nil` = vacío), `read-line()` devuelve `?str` (`nil` = EOF, `err` = error) y `lookup()` devuelve `?str` (`nil` = no encontrado). Consulte la documentación de sintaxis para más detalles.

---
