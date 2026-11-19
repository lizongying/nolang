---
sidebar_position: 3.6
---

## Estructuras de datos

### set — Conjunto (basado en array)

```no
new-n = set.add(s, n, val); Añadir elemento
new-n = set.set-remove(s, n, val); Eliminar elemento
ok = set.contains(s, n, val); Si contiene
new-an = set.union(a, an, b, bn); Unión
out, n = set.intersection(a, an, b, bn); Intersección
out, n = set.difference(a, an, b, bn); Diferencia
v = set.to-vec(s, n); Convertir a segmento
sz = set.set-size(s, n); Número de elementos
yes = set.set-empty(s, n); Si está vacío
```

### deque — Cola de doble extremo

Una cola de doble extremo implementada con un búfer circular, encapsulada en la estructura `deque`:

```no
; Estructura
deque {
    buf []i64
    cap i64
    head i64
    tail i64
}

; Inicialización
d = deque {
    buf: buf
    cap: 128
    head: 0
    tail: 0
}

; Métodos
d.push-front(val); Insertar por el frente
d.push-back(val); Insertar por el final
val = d.pop-front(); Extraer por el frente
val = d.pop-back(); Extraer por el final
val = d.peek-front(); Ver el elemento del frente (?i64, nil=vacío)
val = d.peek-back(); Ver el elemento del final (?i64, nil=vacío)
sz = d.size(); Tamaño
yes = d.empty(); Si está vacío
d.clear(); Vaciar
```

### heap — Montón mínimo (min heap)

Un montón mínimo binario encapsulado en la estructura `heap`:

```no
; Estructura
heap {
    data []i64
    n i64
}

; Inicialización
h = heap.init(data); Crear el montón

; Métodos
h.push(val); Insertar elemento
val = h.pop(); Extraer el elemento mínimo (?i64, nil=vacío)
val = h.peek(); Ver el elemento mínimo (?i64, nil=vacío)
sz = h.size(); Tamaño
yes = h.empty(); Si está vacío
```

### stack — Pila

Estructura de datos de último en entrar, primero en salir (LIFO), encapsulada en la estructura `stack`:

```no
; Estructura
stack {
    data []i64
    n i64
}

; Inicialización
buf [128]i64
s = stack {
    data: buf
    n: 0
}

; Métodos
s.push(val); Insertar elemento
val = s.pop(); Extraer el elemento superior (?i64, nil=vacío)
val = s.peek(); Ver el elemento superior (?i64, nil=vacío)
sz = s.size(); Tamaño
yes = s.empty(); Si está vacío
s.clear(); Vaciar
```

### map/linked-hash-map — Tabla hash ordenada

Capacidad fija 64 (i64→i64), sondeo lineal; una lista doblemente enlazada mantiene el orden de inserción:

```no
m = linked-hash-map {}
m.init()
m.put(key, val)
result = m.get(key); ?i64, nil=no encontrado
found = m.contains(key)
removed = m.remove(key)
m.clear()
n = m.len()
empty = m.is-empty()
m.for-each(key, val)
```

### map/hash-set — Conjunto hash de i64

Capacidad fija 64, sondeo lineal, búsqueda/inserción/eliminación O(1):

```no
s = hash-set {}
s.init()
is-new = s.add(val)
found = s.contains(val)
removed = s.remove(val)
s.clear()
n = s.len()
empty = s.is-empty()
s.for-each(val)
```

### map/str-map — Tabla hash str→str

Capacidad fija 256, hash FNV-1a, sondeo lineal:

```no
m = str-map {}
m.init()
m.put('key', 'val')
result = m.get('key'); ?str, nil=no encontrado
found = m.contains('key')
removed = m.remove('key')
m.clear()
n = m.len()
empty = m.is-empty()
m.for-each(k, v)
```

### map/str-set — Conjunto hash de str

Capacidad fija 256, hash FNV-1a, deduplicación de cadenas:

```no
s = str-set {}
s.init()
is-new = s.add('hello')
found = s.contains('hello')
removed = s.remove('hello')
s.clear()
n = s.len()
empty = s.is-empty()
s.for-each(val)
```

### map/tree-map — Mapa ordenado (árbol AVL)

Mapa ordenado (i64→i64) implementado sobre un árbol binario de búsqueda AVL autobalanceado, capacidad 64:

```no
m = tree-map {}
m.clear(); Inicializar
ok = m.put(key, val); Insertar o actualizar
val = m.get(key); Buscar (?i64, nil=no encontrado)
yes = m.contains(key); Comprobar si existe la clave
ok = m.remove(key); Eliminar clave
key = m.first(); Clave mínima (?i64)
key = m.last(); Clave máxima (?i64)
key = m.lower-bound(target); Primera clave ≥ target (?i64)
key = m.upper-bound(target); Primera clave > target (?i64)
m.for-each(k, v); Recorrer en orden ascendente de clave
sz = m.size()
yes = m.empty()
yes = m.full()
```

### map/tree-set — Conjunto ordenado (árbol AVL)

Conjunto ordenado (i64) implementado sobre un árbol binario de búsqueda AVL autobalanceado, capacidad 64:

```no
s = tree-set {}
s.clear(); Inicializar
ok = s.add(key); Añadir elemento
yes = s.contains(key); Comprobar si existe
ok = s.remove(key); Eliminar elemento
val = s.first(); Valor mínimo (?i64)
val = s.last(); Valor máximo (?i64)
val = s.lower-bound(target); Primer elemento ≥ target (?i64)
val = s.upper-bound(target); Primer elemento > target (?i64)
s.for-each(val); Recorrer en orden ascendente
sz = s.size()
yes = s.empty()
yes = s.full()
```

### collection/queue — Cola genérica (búfer circular)

Implementado con un búfer circular sobre un array de longitud fija; el búfer lo proporciona el receptor `[n]t`:

```no
buf [128]i64
q = buf.queue-init()
ok = buf.queue-push(q, val); Insertar al final
val = buf.queue-pop(q); Extraer por el frente (?t)
val = buf.queue-peek(q); Ver el frente de la cola (?t)
sz = q.size()
yes = q.empty()
yes = q.full()
q.clear()
```

### collection/arr-stack — Pila genérica (basada en array de longitud fija)

Implementación de pila basada en un array de longitud fija; el búfer lo proporciona el receptor `[n]t`:

```no
buf [128]i64
s = buf.arr-stack-init()
ok = buf.arr-stack-push(s, val); Insertar
val = buf.arr-stack-pop(s); Extraer (?t)
val = buf.arr-stack-peek(s); Ver la cima (?t)
sz = s.size()
yes = s.empty()
yes = s.full()
s.clear()
```

### collection/link — Lista doblemente enlazada genérica

Lista doblemente enlazada basada en un pool de nodos de array de longitud fija; los valores los proporciona el receptor `[n]t`:

```no
buf [128]i64
nxt [128]i64
prv [128]i64
l = buf.link-init(nxt, prv)
ok = buf.link-push-front(l, val); Insertar en la cabeza
ok = buf.link-push-back(l, val); Insertar en la cola
val = buf.link-pop-front(l); Extraer de la cabeza (?t)
val = buf.link-pop-back(l); Extraer de la cola (?t)
val = buf.link-peek-front(l); Ver la cabeza (?t)
val = buf.link-peek-back(l); Ver la cola (?t)
sz = l.size()
yes = l.empty()
yes = l.full()
```

### collection/map — Tabla hash dinámica genérica

Tabla hash genérica de capacidad dinámica; cuando el factor de carga supera 0.75 se hace un rehash automático para ampliarla (se duplica la capacidad). Tres plantillas especializadas por tipo de clave:

```no
; Mapa con clave str (V genérico)
m = hashmap-str-tmpl {}
m.init()
m.put('key', val)
result = m.get('key'); ?V, nil=no encontrado
found = m.contains('key')
m.remove('key')
n = m.len()
yes = m.is-empty()
m.clear()

; Mapa con clave int (K y V genéricos)
m2 = hashmap-int-tmpl {}
m2.init()
m2.put(k, v)

; Mapa con clave bool (V genérico)
m3 = hashmap-bool-tmpl {}
m3.init()
m3.put(flag, v)
```

### collection/static-hashmap — Tabla hash genérica de capacidad fija

Tabla hash genérica de capacidad fija (256 ranuras) con sondeo lineal. Tres plantillas especializadas por tipo de clave:

```no
; Mapa estático con clave str (V genérico)
m = static-hashmap-str-tmpl {}
m.init()
m.put('key', val)
result = m.get('key'); ?V, nil=no encontrado
found = m.contains('key')
m.remove('key')
n = m.len()

; Mapa estático con clave int (K y V genéricos)
m2 = static-hashmap-int-tmpl {}

; Mapa estático con clave bool (V genérico, 2 ranuras)
m3 = static-hashmap-bool-tmpl {}
```

---
