---
sidebar_position: 3.6
---

## Data Structures

### set — Set (Array-based)

```no
new-n = set.add(s, n, val)           ; Add element
new-n = set.set-remove(s, n, val)        ; Remove element
ok = set.contains(s, n, val)         ; Whether it contains
new-an = set.union(a, an, b, bn)     ; Union
out, n = set.intersection(a, an, b, bn); Intersection
out, n = set.difference(a, an, b, bn)  ; Difference
v = set.to-vec(s, n)                 ; Convert to slice
sz = set.set-size(s, n)                   ; Number of elements
yes = set.set-empty(s, n)                    ; Whether it is empty
```

### deque — Double-Ended Queue

A double-ended queue implemented with a circular buffer, wrapped in the `deque` struct:

```no
; Struct
deque {
    buf []i64
    cap i64
    head i64
    tail i64
}

; Initialization
d = deque{
    buf: buf
    cap: 128
    head: 0
    tail: 0
}

; Methods
d.push-front(val)              ; Push from front
d.push-back(val)               ; Push from back
val = d.pop-front()             ; Pop from front
val = d.pop-back()              ; Pop from back
val = d.peek-front()            ; Peek front element (?i64, nil=empty)
val = d.peek-back()             ; Peek back element (?i64, nil=empty)
sz = d.size()                   ; Size
yes = d.empty()                 ; Whether it is empty
d.clear()                      ; Clear
```

### heap — Min Heap

A binary min heap wrapped in the `heap` struct:

```no
; Struct
heap {
    data []i64
    n i64
}

; Initialization
h = heap.init(data)            ; Create heap

; Methods
h.push(val)                    ; Push element
val = h.pop()                  ; Pop minimum element (?i64, nil=empty)
val = h.peek()                 ; Peek minimum element (?i64, nil=empty)
sz = h.size()                  ; Size
yes = h.empty()                ; Whether it is empty
```

### stack — Stack

A last-in-first-out (LIFO) data structure, wrapped in the `stack` struct:

```no
; Struct
stack {
    data []i64
    n i64
}

; Initialization
buf [128]i64 = [0:128]
s = stack{
    data: buf
    n: 0
}

; Methods
s.push(val)                    ; Push element
val = s.pop()                  ; Pop top element (?i64, nil=empty)
val = s.peek()                 ; Peek top element (?i64, nil=empty)
sz = s.size()                  ; Size
yes = s.empty()                ; Whether it is empty
s.clear()                      ; Clear
```

### map/linked-hash-map — Ordered Hash Map

Fixed capacity 64 (i64→i64), linear probing, doubly-linked list preserves insertion order:

```no
m = linked-hash-map{}
m.init()
m.put(key, val)
result = m.get(key)   ; ?i64, nil=not found
found = m.contains(key)
removed = m.remove(key)
m.clear()
n = m.len()
empty = m.is-empty()
m.for-each(key, val)
```

### map/hash-set — i64 Hash Set

Fixed capacity 64, linear probing, O(1) lookup/insert/delete:

```no
s = hash-set{}
s.init()
is-new = s.add(val)
found = s.contains(val)
removed = s.remove(val)
s.clear()
n = s.len()
empty = s.is-empty()
s.for-each(val)
```

### map/str-map — str→str Hash Map

Fixed capacity 256, FNV-1a hash, linear probing:

```no
m = str-map{}
m.init()
m.put('key', 'val')
result = m.get('key')   ; ?str, nil=not found
found = m.contains('key')
removed = m.remove('key')
m.clear()
n = m.len()
empty = m.is-empty()
m.for-each(k, v)
```

### map/str-set — str Hash Set

Fixed capacity 256, FNV-1a hash, string deduplication:

```no
s = str-set{}
s.init()
is-new = s.add('hello')
found = s.contains('hello')
removed = s.remove('hello')
s.clear()
n = s.len()
empty = s.is-empty()
s.for-each(val)
```

### map/tree-map — Ordered Map (AVL Tree)

An ordered map (i64→i64) implemented on a self-balancing AVL binary search tree, capacity 64:

```no
m = tree-map{}
m.clear()                           ; Initialize
ok = m.put(key, val)                ; Insert or update
val = m.get(key)                    ; Lookup (?i64, nil=not found)
yes = m.contains(key)               ; Check whether key exists
ok = m.remove(key)                  ; Delete key
key = m.first()                     ; Minimum key (?i64)
key = m.last()                      ; Maximum key (?i64)
key = m.lower-bound(target)         ; First key >= target (?i64)
key = m.upper-bound(target)         ; First key > target (?i64)
m.for-each(k, v)                    ; Traverse in ascending key order
sz = m.size()
yes = m.empty()
yes = m.full()
```

### map/tree-set — Ordered Set (AVL Tree)

An ordered set (i64) implemented on a self-balancing AVL binary search tree, capacity 64:

```no
s = tree-set{}
s.clear()                           ; Initialize
ok = s.add(key)                     ; Add element
yes = s.contains(key)               ; Check whether it exists
ok = s.remove(key)                  ; Delete element
val = s.first()                     ; Minimum value (?i64)
val = s.last()                      ; Maximum value (?i64)
val = s.lower-bound(target)         ; First element >= target (?i64)
val = s.upper-bound(target)         ; First element > target (?i64)
s.for-each(val)                     ; Traverse in ascending order
sz = s.size()
yes = s.empty()
yes = s.full()
```

### collection/queue — Generic Queue (Ring Buffer)

Implemented on a fixed-length array ring buffer; the buffer is provided by the `[n]t` receiver:

```no
buf [128]i64 = [0:128]
q = buf.queue-init()
ok = buf.queue-push(q, val)         ; Push to tail
val = buf.queue-pop(q)              ; Pop from front (?t)
val = buf.queue-peek(q)             ; Peek front (?t)
sz = q.size()
yes = q.empty()
yes = q.full()
q.clear()
```

### collection/arr-stack — Generic Stack (Fixed-Length Array Based)

A stack implementation based on a fixed-length array; the buffer is provided by the `[n]t` receiver:

```no
buf [128]i64 = [0:128]
s = buf.arr-stack-init()
ok = buf.arr-stack-push(s, val)     ; Push
val = buf.arr-stack-pop(s)          ; Pop (?t)
val = buf.arr-stack-peek(s)         ; Peek top (?t)
sz = s.size()
yes = s.empty()
yes = s.full()
s.clear()
```

### collection/link — Generic Doubly Linked List

A doubly linked list based on a fixed-length array node pool; values are provided by the `[n]t` receiver:

```no
buf [128]i64 = [0:128]
nxt [128]i64 = [0:128]
prv [128]i64 = [0:128]
l = buf.link-init(nxt, prv)
ok = buf.link-push-front(l, val)    ; Insert at head
ok = buf.link-push-back(l, val)     ; Insert at tail
val = buf.link-pop-front(l)         ; Pop head (?t)
val = buf.link-pop-back(l)          ; Pop tail (?t)
val = buf.link-peek-front(l)        ; Peek head (?t)
val = buf.link-peek-back(l)         ; Peek tail (?t)
sz = l.size()
yes = l.empty()
yes = l.full()
```

### collection/map — Generic Dynamic Hash Map

Generic dynamic-capacity hash map, using vec fields for keys/vals/occ. Supports str keys (hashmap-str-tmpl), int keys (hashmap-int-tmpl), and bool keys (hashmap-bool-tmpl). Initial capacity 16, auto-rehash when load factor > 0.75:

```no
m = hashmap-str-tmpl{}
m.init()
m.put('key', val)                   ; Insert or update
v = m.get('key')                    ; Lookup (?v, nil=not found)
yes = m.contains('key')             ; Check whether key exists
m.remove('key')                     ; Delete key
m.clear()
n = m.len()
yes = m.is-empty()
m.for-each(key, val)                ; Traverse all entries
```

### collection/static-hashmap — Generic Fixed-capacity Hash Map

Generic fixed-capacity hash map (256 slots for str/int, 2 slots for bool), using linear probing and FNV-1a hashing. Recommended ≤ 192 entries (load factor ≤ 0.75):

```no
m = static-hashmap-str-tmpl{}
m.init()
m.put('key', val)                   ; Insert or update
v = m.get('key')                    ; Lookup (?v, nil=not found)
yes = m.contains('key')             ; Check whether key exists
m.remove('key')                     ; Delete key
m.clear()
n = m.len()
yes = m.is-empty()
m.for-each(key, val)                ; Traverse all entries
```

---
