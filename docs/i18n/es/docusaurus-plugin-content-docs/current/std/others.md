---
sidebar_position: 4.4
---

## Otros

### unicode — Compatibilidad con Unicode

La funcionalidad relacionada con Unicode se ha distribuido entre los módulos `char` y `str`:

- Clasificación de caracteres (`is-letter`, `is-digit`, `is-upper`, etc.) → véase el módulo `char`
- Codificación/decodificación UTF-8 (`char.to-bytes`, `char.to-str`) → véase el módulo `char`
- Conteo de runas de una cadena (`str.count`) → véase el módulo `str`

### uuid — Generación y análisis de UUID v4

```no
out = uuid.new-v4(state); Generar UUID v4
out-n = uuid.to-str(out); Convertir a cadena en minúsculas (método)
out-n = uuid.to-str-upper(out); Convertir a cadena en mayúsculas (método)
ok = uuid.from-str(s, sn, out); Analizar desde cadena (admite con/sin guiones)
ok = uuid.parse-with-dashes(s, pos, out); Análisis con guiones
ok = uuid.parse-no-dashes(s, pos, out); Análisis sin guiones
ok = uuid.validate(); Validar formato de UUID (método)
v = uuid.version(); Obtener la versión (método)
v = uuid.variant(); Obtener la variante (método)
yes = uuid.is-nil(); Si es nil (método)
yes = uuid.eq(b); Comparación de igualdad (método)
r = uuid.cmp(b); Comparar (método)
uuid.nil-uuid(out); Devolver el UUID nil
```

### bigint — Entero de precisión arbitraria

```no
; Tipo
bigint {
    sign i64
    limbs []i64
    len i64
}

; Construcción
out = bigint.from-i64(v)
out = bigint.from-u64(v)
out = bigint.zero()
out = bigint.one()
out = bigint.copy(a)

; Comparación
r = bigint.cmp(a, b)
r = bigint.eq(a, b)
r = bigint.is-zero(a)
r = bigint.is-neg(a)
r = bigint.is-pos(a)

; Operaciones (+ - * / % están sobrecargados para bigint, véase «Sintaxis > Sobrecarga de operadores»)
c = a + b; equivalente a a._add(b)
c = a - b; equivalente a a._sub(b)
c = a * b; equivalente a a._mul(b)
q = a / b; equivalente a a._div(b), dividir por cero devuelve cero según div-mod
r = a % b; equivalente a a._mod(b)

; Operandos mixtos: los operadores sobrecargados toman una unión `bigint | i64` como parámetro;
; el compilador monomorfiza y elige la rama en tiempo de compilación según el tipo del operando derecho,
; por lo que `a + 1` / `a * 256` funcionan de forma nativa (sin tag en tiempo de ejecución).
; Solo se admite «el operando izquierdo es bigint»; `1 + a` no está soportado.
c = a + 1; bigint ⊕ i64
c = a * 256; bigint ⊕ i64
q, r = a.div-mod(b); Obtener a la vez el cociente y el resto
c = a.pow(n)
r = base.mod-pow(exp, mod)

; Teoría de números
g = a.gcd(b)
l = a.lcm(b)

; Desplazamientos
bigint.shl(a, n, c)
bigint.shr(a, n, c)

; Conversión a cadena
n = bigint.to-str(a, out)
out = bigint.from-str(s, sn)
n = bigint.to-hex(a, out)
out = bigint.from-hex(s, sn)
; Las rutas rápidas para enteros pequeños (_add-i64 / _mul-i64 / _div-i64 / _mod-i64) son privadas
; del módulo bigint; para operandos mixtos use siempre los operadores sobrecargados + - * / % de arriba
```

### err — Manejo de errores

Tipo de error estructurado y funciones de utilidad:

```no
; Enumeración de códigos de error
code {
    ok,
    not-found,
    permission,
    io,
    timeout,
    parse,
    invalid,
    overflow,
    unknown,
}

; Estructura
err {
    code code
    msg str
    file str
    line i64
    col i64
}

; Funciones
e = err.new(msg, code.io); Crear error
e = err.from-errno(errno); Crear a partir de un errno de C
yes = e.is(code.io); Comprobar el código de error
msg = e.msg(); Obtener el mensaje de error
c = e.code(); Obtener el código de error
s = e.format(); Formatear como cadena
```

Esta estructura `err` es la carga de la variante `err` de la opción integrada (`option { ok(v t), nil, err(e err.err) }`):
al hacer match sobre `?t`, el `it` enlazado implícitamente en la rama `err ->` es ella, y no un `str`.

```no
x = f(); f devuelve ?i64
x: {
    ok -> n = it

    nil -> n = 0

    err -> {
        log.error(it.msg()); Solo el mensaje
        print(it.format()); 'code.unknown: ... at file:line:col'
        it.is(code.io) -> print('io error')
    }
}
```

### bool — Tipo booleano

```no
#{fmt-skip}
bool.to-str() (out str)     ; true→"true", false→"false" (método)
```

### enter / leave — Hooks de ciclo de vida

```no
; Se ejecuta al iniciar
enter {
    enter()
}

; Se ejecuta al salir
leave {
    leave()
}
```

### magic — Detección de tipos de archivo

Determina el tipo de archivo a partir de la extensión y de los números mágicos (magic bytes), sin depender de libmagic:

```no
kind = magic.detect-type(path); Detectar el tipo de archivo
; Devuelve una cadena descriptiva del tipo, como 'PNG image', 'ELF executable', 'ASCII text', 'directory', 'unknown' o 'data'
```

---
