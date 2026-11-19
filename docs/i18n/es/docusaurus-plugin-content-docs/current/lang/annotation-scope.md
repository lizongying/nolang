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

