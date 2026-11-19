---
sidebar_position: 4.1
---

## Intercambio de datos

### json — Análisis y generación de JSON

`json` guarda un árbol JSON en un **pool de nodos asignado en el montón** (`json-pool`); admite arrays y objetos anidados, escalares de cadena/número/booleano/null, secuencias de escape y números en formato exponencial. El pool usa `vec` dinámicos, **sin límite fijo de capacidad**: el número total de nodos y el número de hijos por nodo solo están limitados por la memoria disponible. La interfaz de alto nivel encapsula el pool y el índice del nodo raíz en la estructura `json`; su campo `pool` está anotado con `#{inline=false}` (campo puntero), lo que permite devolver y copiar `json` por valor de forma segura.

```no
; Analizar: devuelve ?json (ok=éxito, err=fallo con el motivo)
j = json.parse('{"name":"Alice","age":30,"items":[1,2,3],"active":true}')
j: {
    ok -> {
        name = it.get-str('name'); Escalares: get-str / get-num / get-bool / get-i64 (?valor)
        age = it.get-i64('age'); Obtener entero (?i64)
        items = it.get('items'); Obtener nodo hijo, devuelve ?json
        s = it.stringify(); Serializar de vuelta a cadena
    }

    err(e) -> print(e)
}

; Generar: empezar con un json vacío e ir usando set / arr-push
j2 = json.new()
j2.set-str('key', 'value')
j2.set-i64('count', 3)
j2.arr-push-str('a')
print(j2.stringify())
```

**API de alto nivel (estructura `json`)**

- `json.new()` — crear un `json` vacío (null)
- `json.parse(s)` — analizar y devolver `?json`; la rama `err` incluye el motivo concreto (`empty input` / `parse error` / `trailing characters`)
- `j.stringify()` — serializar a cadena
- Lectura: `j.get-str(key)` / `j.get-num(key)` / `j.get-bool(key)` / `j.get-i64(key)` (cada uno devuelve `?valor`); `j.get(key)` devuelve el nodo hijo `?json`
- Acceso directo al escalar raíz (cuando la raíz es un escalar): `j.str()` / `j.num()` / `j.bool()` / `j.i64()` / `j.str-val()`
- Escritura: `j.set-str(key,val)` / `j.set-num` / `j.set-i64` / `j.set-bool` / `j.set-null` / `j.set-key(key, val json)`
- Array: `j.arr-get(i)` / `j.arr-len()` / `j.arr-push(val json)` / `j.arr-push-str` / `j.arr-push-num` / `j.arr-push-bool`
- Enumeración de objetos: `j.obj-len()` / `j.obj-key(i)` / `j.obj-keys-str()` / `j.delete-key(key)`
- Comprobación de tipos: `j.kind()` y `j.is-null()` / `j.is-obj()` / `j.is-arr()` / `j.is-str()` / `j.is-num()` / `j.is-bool()`

**Capacidad del pool (sin límite fijo)**

Las versiones anteriores usaban un array fijo como pool, con el límite estricto `JSON-MAX-NODES = 128` / `JSON-MAX-CHILDREN = 32`, y fallaban silenciosamente al superarlo. Ahora se ha pasado por completo a `vec` en el montón:

- El número total de nodos y el número de hijos por nodo **no tienen límite**, solo el de la memoria disponible.
- `json.parse` devuelve `err` solo ante un error de sintaxis real; ya no existe la rama de error «pool agotado».
- `j.set*` / `j.arr-push*` solo devuelven `false` cuando el tipo del objetivo de construcción no coincide (p. ej. hacer set en un nodo que no es objeto o push en un nodo que no es array). `j.overflowed()` se conserva por compatibilidad hacia atrás y ahora devuelve siempre `false`.

> **Semántica de copia de `json` (`#{inline=false}`)**: `pool` es un campo puntero; los nodos hijos `json` devueltos por `j.get` / `j.arr-get` **copian profundamente** todo el pool y son una instantánea independiente del pool padre: las escrituras en la instantánea no se reflejan en el nodo padre. Además, como `?json` (option) consume su valor al hacer `match`, **repetir el `match`** sobre la misma option entra en la rama `nil`; si necesita operar varias veces sobre el mismo resultado de análisis, hágalo todo dentro de un único `match` o reciba antes `it` en una variable local `json`.

**API de bajo nivel (`json-pool` / `json-value`)**

Se opera directamente con índices de nodo y el pool se usa como variable local (las estructuras de un solo nivel se pueden devolver/copiar por valor sin problema):

- `p = json.new-pool()`; o `p = json-pool {}` y luego `p.init()` (inicializa cada vec `nodes`/`strs`/`ec`/`ek`/`en` con `with-len(0)`)
- `idx = p.alloc()` — asignar un nodo nuevo y devolver su índice (el vec del montón crece automáticamente)
- `p.add-child(node-idx, child-idx, key)` — añadir un hijo mediante la lista enlazada de aristas (`ec`/`ek`/`en`), sin límite por nodo
- `node-idx, next-pos, ok = p.parse(s, pos)` — analizar un valor desde `pos` y devolver el índice del nodo
- `val-idx = p.get-key(obj-idx, key)` (`?i64`); `p.arr-get(arr-idx, i)` (`?i64`); `p.arr-len(arr-idx)`
- `p.get-kind(idx)` / `p.get-str(idx)` / `p.get-num(idx)` / `p.get-bool(idx)`
- `p.set-key(obj-idx, key, val-idx)`; `p.copy-tree(src, idx)` (copia profunda recursiva del subárbol)
- `p.stringify(node-idx)` — serializar el nodo indicado

### toml — Análisis y generación de TOML 1.0

`toml` guarda un documento TOML en `toml`; admite comentarios, claves bare/quoted/dotted, tablas normales, array-of-tables, cadenas basic/literal/multiline, números enteros en distintas bases, coma flotante, booleanos, fechas y horas, arrays e inline tables. Los valores sin un tipo Nolang dedicado conservan su valor literal original.

```no
cfg = toml.parse('title = "Nolang"\n[server]\nport = 8080\n[[server.backends]]\nname = "local"')

doc = toml.new()
doc.put('name', '"nolang"')
doc.put('server.port', '8080')
doc.put('enabled', 'true')
raw = toml.get(doc, 'server.port')
text = toml.stringify(doc)
```

Funciones comunes:

- `toml.new()` — crear un documento vacío
- `toml.parse(text)` — analizar y devolver `?toml`
- `doc.put(key, raw-value)` — validar y añadir/actualizar un valor TOML
- `get(doc, key)` — obtener el valor original
- `get-str`, `get-i64`, `get-f64`, `get-bool` — obtener valores escalares
- `toml.stringify(doc)` — serializar el documento

Los límites del documento son 4 claves, 2 tablas normales y 2 array-of-tables; la longitud máxima de una clave es 32 bytes y la de un valor, 128 bytes.

### yaml — Análisis y generación de YAML 1.2

`yaml` guarda el árbol de un documento YAML 1.2 (core schema) en un pool de nodos (`pool`). Admite mapeos de bloque, secuencias de bloque, colecciones de flujo (`[...]` / `{...}`), escalares entre comillas simples/dobles y escalares plain, escalares de bloque (`|` / `>` con chomping `-` / `+`), comentarios, marcadores de documento (`---` / `...`) y multidocumento, anclas (`&`) y alias (`*`), claves de fusión `<<`, claves explícitas (`? k`), inferencia de tipos del core schema (null / bool / int / float / `.inf` / `.nan`), búsqueda por ruta con puntos y serialización JSON / YAML.

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
        port: {
            ok -> print(it.to-str())

            -> print('')
        }
        print(doc.to-json())
        print(doc.to-yaml())
    }
}
```

Funciones y métodos comunes:

- `yaml.parse(text)` / `yaml.parse-all(text)` — analizar y devolver `?yaml` (la rama `err` incluye la posición `line L, column C`)
- `yaml.valid-of(text)` / `yaml.error-of(text)` — solo validar, sin construir el documento
- `yaml.strip-bom(text)` — quitar la BOM UTF-8
- `doc-count()` / `doc(i)` — acceso a múltiples documentos
- `kind()`, `is-null()`, `is-bool()`, `is-int()`, `is-float()`, `is-nan()`, `is-str()`, `is-seq()`, `is-map()`, `len()`, `text()`, `tag()`, `anchor()`, `line-of()` — información del nodo
- `str()`, `i64()`, `f64()`, `bool()`, `scalar-text()` — acceso a valores; todos devuelven `?valor`
- `at(i)`, `value(i)`, `key(i)`, `key-node(i)`, `get(key)`, `find(path)` — nodos hijos y claves
- `get-str` / `get-i64` / `get-f64` / `get-bool`, `find-str` / `find-i64` / `find-f64` / `find-bool` — accesos convenientes
- `to-json()`、`to-json-all()`、`to-yaml()`、`stringify()`、`source-of()` — serialización

Constantes de tipo de nodo: `YAML-KIND-NULL` / `BOOL` / `INT` / `FLOAT` / `STR` / `SEQ` / `MAP` (0–6); constantes de estilo escalar `YAML-STYLE-PLAIN` / `SINGLE` / `DOUBLE` / `LITERAL` / `FOLDED` / `ALIAS` (0–5) y `YAML-STYLE-BLOCK` / `FLOW` (6–7).

> **Nota uno**: las vistas de nodos hijos se devuelven como `?yaml` y las funciones auxiliares de obtención de valores devuelven `?valor`; desenvuélvalas con un match (`child = d.at(i)`, cuya rama `ok` vincula `it` al nodo hijo y cuya rama por defecto trata el caso vacío, una rama por línea). El nodo hijo comparte el pool con el documento padre, por lo que se recomienda completar la obtención dentro del mismo match, o enlazar primero una copia, y no mantener identificadores obsoletos a través de cambios sucesivos del pool. Para obtener valores se recomienda usar siempre funciones de ruta convenientes como `find-str` / `find-i64`.
>
> **Nota dos**: en los match anidados, `it` se **restaura por nivel**. La rama de un match interno vuelve a vincular `it` a su propio sujeto (el cuerpo de la rama debe ver el valor interno), pero en cuanto termina esa rama, `it` vuelve al sujeto de la rama externa; por eso la siguiente forma es segura:
>
> ```no
> d ?yaml = yaml.parse(src)
> d: {
>     nil -> print('nil')
>
>     err -> print(it.msg())
>
>     -> {
>         print(it.find-str('a'))        ; it externo = el documento
>         v ?yaml = it.at(0)
>         v: {
>             nil -> print('none')
>             err -> print('err')
>             -> print(it.find-str('b')) ; it interno = el nodo hijo
>         }
>         print(it.find-str('c'))        ; ✅ it se ha restaurado al documento
>     }
> }
> ```
>
> La única excepción es el match más externo: cuando termina, `it` conserva el valor de la última rama (no se restaura), así que no confíe en `it` fuera de un match. Si necesita usar el valor entre niveles, guárdelo explícitamente en una variable local con nombre (`doc = it`) o use un enlace destructurado `ok(v) -> ...`.
>
> **Nota tres**: `.nan` se analiza como un NaN real (`f64()` devuelve NaN y `x != x` es verdadero). JSON no tiene NaN, por lo que `to-json()` emite `null` para él; `to-yaml()` emite `.nan`.

---
