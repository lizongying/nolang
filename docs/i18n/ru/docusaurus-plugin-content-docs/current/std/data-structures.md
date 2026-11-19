---
sidebar_position: 3.6
---

## Структуры данных

### set — множество (на основе массива)

```no
new-n = set.add(s, n, val); добавить элемент
new-n = set.set-remove(s, n, val); удалить элемент
ok = set.contains(s, n, val); содержит ли
new-an = set.union(a, an, b, bn); объединение
out, n = set.intersection(a, an, b, bn); пересечение
out, n = set.difference(a, an, b, bn); разность
v = set.to-vec(s, n); преобразовать в срез
sz = set.set-size(s, n); число элементов
yes = set.set-empty(s, n); пусто ли
```

### deque — двусторонняя очередь

Двусторонняя очередь, реализованная на кольцевом буфере, инкапсулированная в структуре `deque`:

```no
; Структура
deque {
    buf []i64
    cap i64
    head i64
    tail i64
}

; Инициализация
d = deque {
    buf: buf
    cap: 128
    head: 0
    tail: 0
}

; Методы
d.push-front(val); добавить в начало
d.push-back(val); добавить в конец
val = d.pop-front(); взять из начала
val = d.pop-back(); взять из конца
val = d.peek-front(); просмотреть элемент в начале (?i64, nil=пусто)
val = d.peek-back(); просмотреть элемент в конце (?i64, nil=пусто)
sz = d.size(); размер
yes = d.empty(); пусто ли
d.clear(); очистить
```

### heap — минимальная куча

Двоичная минимальная куча, инкапсулированная в структуре `heap`:

```no
; Структура
heap {
    data []i64
    n i64
}

; Инициализация
h = heap.init(data); создать кучу

; Методы
h.push(val); добавить элемент
val = h.pop(); взять минимальный элемент (?i64, nil=пусто)
val = h.peek(); просмотреть минимальный элемент (?i64, nil=пусто)
sz = h.size(); размер
yes = h.empty(); пусто ли
```

### stack — стек

Структура данных «последним пришёл — первым вышел» (LIFO), инкапсулированная в структуре `stack`:

```no
; Структура
stack {
    data []i64
    n i64
}

; Инициализация
buf [128]i64
s = stack {
    data: buf
    n: 0
}

; Методы
s.push(val); добавить элемент
val = s.pop(); взять верхний элемент (?i64, nil=пусто)
val = s.peek(); просмотреть верхний элемент (?i64, nil=пусто)
sz = s.size(); размер
yes = s.empty(); пусто ли
s.clear(); очистить
```

### map/linked-hash-map — упорядоченная хэш-таблица

Фиксированная ёмкость 64 (i64→i64), линейное зондирование, двусвязный список сохраняет порядок вставки:

```no
m = linked-hash-map {}
m.init()
m.put(key, val)
result = m.get(key); ?i64, nil=не найдено
found = m.contains(key)
removed = m.remove(key)
m.clear()
n = m.len()
empty = m.is-empty()
m.for-each(key, val)
```

### map/hash-set — хэш-множество i64

Фиксированная ёмкость 64, линейное зондирование, O(1) поиск/вставка/удаление:

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

### map/str-map — хэш-отображение str→str

Фиксированная ёмкость 256, хэш FNV-1a, линейное зондирование:

```no
m = str-map {}
m.init()
m.put('key', 'val')
result = m.get('key'); ?str, nil=не найдено
found = m.contains('key')
removed = m.remove('key')
m.clear()
n = m.len()
empty = m.is-empty()
m.for-each(k, v)
```

### map/str-set — хэш-множество str

Фиксированная ёмкость 256, хэш FNV-1a, дедупликация строк:

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

### map/tree-map — упорядоченное отображение (AVL-дерево)

Упорядоченное отображение (i64→i64) на основе самобалансирующегося AVL-дерева поиска, ёмкость 64:

```no
m = tree-map {}
m.clear(); инициализация
ok = m.put(key, val); вставить или обновить
val = m.get(key); поиск (?i64, nil=не найдено)
yes = m.contains(key); проверить наличие ключа
ok = m.remove(key); удалить ключ
key = m.first(); минимальный ключ (?i64)
key = m.last(); максимальный ключ (?i64)
key = m.lower-bound(target); первый ключ ≥ target (?i64)
key = m.upper-bound(target); первый ключ > target (?i64)
m.for-each(k, v); обход в порядке возрастания ключей
sz = m.size()
yes = m.empty()
yes = m.full()
```

### map/tree-set — упорядоченное множество (AVL-дерево)

Упорядоченное множество (i64) на основе самобалансирующегося AVL-дерева поиска, ёмкость 64:

```no
s = tree-set {}
s.clear(); инициализация
ok = s.add(key); добавить элемент
yes = s.contains(key); проверить наличие
ok = s.remove(key); удалить элемент
val = s.first(); минимальное значение (?i64)
val = s.last(); максимальное значение (?i64)
val = s.lower-bound(target); первый элемент ≥ target (?i64)
val = s.upper-bound(target); первый элемент > target (?i64)
s.for-each(val); обход в порядке возрастания
sz = s.size()
yes = s.empty()
yes = s.full()
```

### collection/queue — универсальная очередь (кольцевой буфер)

Реализация на кольцевом буфере на основе массива фиксированной длины; буфер предоставляется получателем `[n]t`:

```no
buf [128]i64
q = buf.queue-init()
ok = buf.queue-push(q, val); добавить в конец
val = buf.queue-pop(q); взять из начала (?t)
val = buf.queue-peek(q); просмотреть начало очереди (?t)
sz = q.size()
yes = q.empty()
yes = q.full()
q.clear()
```

### collection/arr-stack — универсальный стек (на основе массива фиксированной длины)

Реализация стека на основе массива фиксированной длины; буфер предоставляется получателем `[n]t`:

```no
buf [128]i64
s = buf.arr-stack-init()
ok = buf.arr-stack-push(s, val); добавить
val = buf.arr-stack-pop(s); взять (?t)
val = buf.arr-stack-peek(s); просмотреть верх (?t)
sz = s.size()
yes = s.empty()
yes = s.full()
s.clear()
```

### collection/link — универсальный двусвязный список

Двусвязный список на основе пула узлов массива фиксированной длины; значения предоставляются получателем `[n]t`:

```no
buf [128]i64
nxt [128]i64
prv [128]i64
l = buf.link-init(nxt, prv)
ok = buf.link-push-front(l, val); вставить в начало
ok = buf.link-push-back(l, val); вставить в конец
val = buf.link-pop-front(l); взять из начала (?t)
val = buf.link-pop-back(l); взять из конца (?t)
val = buf.link-peek-front(l); просмотреть начало (?t)
val = buf.link-peek-back(l); просмотреть конец (?t)
sz = l.size()
yes = l.empty()
yes = l.full()
```

### collection/map — универсальное динамическое хэш-отображение

Универсальное хэш-отображение с динамической ёмкостью; при коэффициенте заполнения > 0,75 автоматически выполняется rehash с удвоением ёмкости. Три шаблона специализированы по типу ключа:

```no
; отображение со строковыми ключами (универсальное V)
m = hashmap-str-tmpl {}
m.init()
m.put('key', val)
result = m.get('key'); ?V, nil=не найдено
found = m.contains('key')
m.remove('key')
n = m.len()
yes = m.is-empty()
m.clear()

; отображение с целочисленными ключами (универсальные K и V)
m2 = hashmap-int-tmpl {}
m2.init()
m2.put(k, v)

; отображение с логическими ключами (универсальное V)
m3 = hashmap-bool-tmpl {}
m3.init()
m3.put(flag, v)
```

### collection/static-hashmap — универсальное хэш-отображение фиксированной ёмкости

Универсальное хэш-отображение фиксированной ёмкости (256 слотов), линейное зондирование. Три шаблона специализированы по типу ключа:

```no
; статическое отображение со строковыми ключами (универсальное V)
m = static-hashmap-str-tmpl {}
m.init()
m.put('key', val)
result = m.get('key'); ?V, nil=не найдено
found = m.contains('key')
m.remove('key')
n = m.len()

; статическое отображение с целочисленными ключами (универсальные K и V)
m2 = static-hashmap-int-tmpl {}

; статическое отображение с логическими ключами (универсальное V, 2 слота)
m3 = static-hashmap-bool-tmpl {}
```

---
