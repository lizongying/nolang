---
sidebar_position: 4
---

# Operadores

## Símbolos especiales

- `#` ; Importar módulo
- `@` ; Exportar módulo
- `->` ; Rama de match y de if/else (p. ej. `cond -> body`)
- `:` ; Expresión match (p. ej. `x: { ... }`)
- `{ } (true)` ; Bucle infinito (forma sufija, ortografía antigua); la forma prefija `!! { }` / `true { }` es equivalente y es la nueva forma por defecto (emitida por `no fmt`)
- `<-` ; Iteración por rango (p. ej. `i <- [a..b]: { }`)
- `..` ; Clase padre / operador de rango (p. ej. `[a..b)`)
- `.` ; La propia clase (⚠️ en los límites superiores de un intervalo, use `self.method` en lugar de `.method` para evitar la ambigüedad visual de tres puntos consecutivos)
- `?` ; Prefijo de tipo option (p. ej. `?i64`, `?str`)
- `(cond) { }` ; Bucle condicional (forma prefija, nueva forma por defecto, equivalente a la forma sufija `{ } (cond)`)
- `N * { }` ; Bucle contado (forma prefija, nueva forma por defecto, equivalente a la forma sufija `{ } * N`; si N ≤ 0 se omite)
- `!` ; Falso (false); también el prefijo de bucle «nunca ejecutar» (`! { }` nunca se ejecuta)
- `!!` ; Verdadero (true); también el prefijo de bucle «siempre ejecutar» (`!! { }` bucle infinito)
- `{ } ()` ; No ejecutar (paréntesis vacíos significan false; forma sufija, ortografía antigua); las formas prefijas `! { }` / `false { }` / `() { }` son equivalentes y son la nueva forma por defecto
- `*` ; Omitir la iteración actual, sustituye a continue (el analizador lo acepta, pero aún no se recomienda; use la palabra clave)
- `**` ; Interrumpir, sustituye a break (el analizador lo acepta, pero aún no se recomienda; use la palabra clave)
- `***` ; Instrucción de retorno, termina la función, sustituye a return (planificado, aún no se recomienda; use la palabra clave)
- `awy { }` ; Grupo de corrutinas; al final del grupo espera a todas las tareas `co`
- `co` ; Generación async sin color, solo válido dentro de un grupo `awy { }`

## Operadores aritméticos

- `+` ; Suma
- `-` ; Resta (también se usa para concatenar cadenas)
- `*` ; Multiplicación (también se usa para repetir cadenas)
- `/` ; División

## Operadores de comparación

- `==` ; Igual a (`str` tiene comparación de igualdad real de cadenas; se permiten varios caracteres)
- `!=` ; Distinto de (ídem)
- <code>&lt;</code> ; Menor que
- `>` ; Mayor que
- <code>&lt;=</code> ; Menor o igual que
- `>=` ; Mayor o igual que

:::warning Los operadores de orden (<code>&lt;</code> `>` <code>&lt;=</code> `>=`) no tienen semántica de orden lexicográfico para cadenas

`str` solo implementa la comparación de igualdad (`==` / `!=`). Comparar cadenas con operadores de orden antes **devolvía false silenciosamente**; ahora es un error de compilación.

- Permitido: un literal de cadena de **un solo carácter** `'a'`, implícitamente un `char` (punto de código), comparable con otro literal de un solo carácter o con un `char` (`"a"`). Los tipos numéricos no se ven afectados.
- Rechazado: cadenas de varios caracteres: el literal `'ab'`, una variable `str` o una llamada que devuelve `str`.
- Para comparaciones lexicográficas use `str.compare(b)` (devuelve -1 / 0 / 1).

```nolang
'a' <= 'z'; true  — la cadena de un carácter es implícitamente char
ch >= 'a'; ch es char, OK
'a' > "b"; cadena de un carácter vs char, OK
'abc' < 'abd'; error de compilación: cadena de varios caracteres
s1 < s2; error de compilación: variables str
s1.compare(s2) > 0; forma correcta
```
:::

## Operadores lógicos

- `&&` ; Y lógico
- `||` ; O lógico (también se usa para combinar ramas de match, p. ej. `nil || err -> body`)
- `!` ; NO lógico

## Operadores a nivel de bits

- `&` ; Y a nivel de bits
- `|` ; O a nivel de bits
- `^` ; XOR a nivel de bits
- `~` ; Complemento a nivel de bits (NOT)
- <code>&lt;&lt;</code> ; Desplazamiento a la izquierda
- `>>` ; Desplazamiento a la derecha

## Operadores de asignación

- `=` ; Asignación
- `+=` ; Asignación con suma
- `-=` ; Asignación con resta
- `*=` ; Asignación con multiplicación
- `/=` ; Asignación con división
- `%=` ; Asignación con módulo
- `&=` ; Asignación con Y a nivel de bits
- `|=` ; Asignación con O a nivel de bits
- `^=` ; Asignación con XOR a nivel de bits
- <code>&lt;&lt;=</code> ; Asignación con desplazamiento a la izquierda
- `>>=` ; Asignación con desplazamiento a la derecha

## Otros

- `?` ; Operador ternario (p. ej. `c = flag ? 1 : 2`)
- `as` ; Conversión de tipos de puntero FFI (p. ej. `y = x as *byte`)
- `..` ; Rango de segmento (p. ej. `arr[1..3]`, `arr[1..]`, `arr[..3]`)
