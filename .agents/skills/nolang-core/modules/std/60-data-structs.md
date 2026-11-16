# set, map/* 与 collection/*（哈希/树/链表/静态表/队列/栈）, heap, deque

> 视图切分自原 `nolang-std/SKILL.md`，正文未改写；索引与加载判据见 [`../m20-std.md`](../m20-std.md)。

### Data Structures

#### set — Generic Set (Slice-field)

`set` is a **generic** set. The `set` struct owns its element buffer as a `[]t` field plus state (`cap`/`size`); the element type `t` is inferred from the concrete `[N]E` / `[]E` buffer passed to the factory `set.init(data)`, and the compiler monomorphizes `set-E` (via `monomorphizeSliceStructs`). So `set` works for any element type with `==` semantics (`i64`/`u8`/`byte`/`str` …). Capacity is the buffer length.

```no
# std/set

buf [128]i64 = [0, 0, 0, /* ... n elements ... */]
s = set.init(buf); s : set-i64, cap = n, size = 0
new-n = s.add(val); Add element (if absent); returns new size
new-n = s.remove(val); Remove element (swap-last); returns new size
yes = s.contains(val); Membership
new-n = s.union(other, osize); Union: add other's elements into s (other []t, osize i64)
cnt = s.intersection(other, osize, out); Common elements written to out []t; returns count
cnt = s.difference(other, osize, out); In s but not other, written to out []t; returns count
v = s.to-vec(); -> []t of size s.size
sz = s.size(); Element count
yes = s.is-empty(); Whether empty

; Generic str set — just supply a [n]str buffer
sbuf [16]str = ['', '', /* ... */]
ss = set.init(sbuf)
ss.add('apple')
```

#### deque — Double-ended Queue

Double-ended queue implemented with a circular buffer, wrapped in the `deque` struct:

```no
; Struct
deque {
    buf []i64
    cap i64
    head i64
    tail i64
}

; Initialization
d = deque {
    buf: buf
    cap: 128
    head: 0
    tail: 0
}

; Methods
d.push-front(val); Push from front
d.push-back(val); Push from back
val = d.pop-front(); Pop from front
val = d.pop-back(); Pop from back
val = d.peek-front(); Peek front element (?i64, nil=empty)
val = d.peek-back(); Peek back element (?i64, nil=empty)
sz = d.size(); Size
yes = d.empty(); Whether empty
d.clear(); Clear
```

#### heap — Generic Min Heap (Slice-field)

`heap` is a **generic** binary min heap / priority queue. The `heap` struct owns its element buffer as a `[]t` field plus state (`cap`/`n`); the element type `t` is inferred from the concrete `[N]E` buffer passed to the factory `heap.init(data)`, and the compiler monomorphizes `heap-E` (via `monomorphizeSliceStructs`). So `heap` works for any element type with `<`/`>=` comparison semantics (`i64`/`u8`/`f64` …). Capacity is the buffer length. Minimum element pops first.

```no
# std/heap

buf [128]i64 = [0, 0, 0, /* ... n elements ... */]
h = heap.init(buf); h : heap-i64, cap = n, n = 0
h.push(val); Push element (ignored if full)
val = h.pop(); Pop minimum (?t, nil = empty)
val = h.peek(); Peek minimum without removing (?t, nil = empty)
sz = h.size(); Element count
yes = h.empty(); Whether empty

; Generic str is NOT supported (needs < / >= ordering); use i64/u8/f64 etc.
; f64 heap — just supply an [n]f64 buffer
fbuf [8]f64 = [0.0, 0.0, /* ... */]
fh = heap.init(fbuf)
fh.push(3.0)
fh.push(1.5)
smallest = fh.pop(); -> ok(1.5)
```

> Codegen caveat: writing into a **top-level global** `[n]f64` array via a generic method triggers a compiler bug (global emitted as `[N x i64]`). Reads are fine; keep f64 buffers **function-local** (stack alloca) until fixed.


#### stack — Stack

Last-in-first-out (LIFO) data structure, wrapped in the `stack` struct:

```no
; Struct
stack {
    data []i64
    n i64
}

; Initialization
buf [128]i64
s = stack {
    data: buf
    n: 0
}

; Methods
s.push(val); Push element
val = s.pop(); Pop top element (?i64, nil=empty)
val = s.peek(); Peek top element (?i64, nil=empty)
sz = s.size(); Size
yes = s.empty(); Whether empty
s.clear(); Clear
```

#### map/linked-hash-map — Ordered Hash Map

Fixed capacity 64 (i64→i64), linear probing, doubly-linked list preserves insertion order:

```no
m = linked-hash-map {}
m.init()
m.put(key, val)
result = m.get(key); ?i64, nil=not found
found = m.contains(key)
removed = m.remove(key)
m.clear()
n = m.len()
empty = m.is-empty()
m.for-each(key, val)
```

#### map/hash-set — i64 Hash Set

Fixed capacity 64, linear probing, O(1) lookup/insert/delete:

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

#### map/str-map — str→str Hash Map

Fixed capacity 256, FNV-1a hash, linear probing:

```no
m = str-map {}
m.init()
m.put('key', 'val')
result = m.get('key'); ?str, nil=not found
found = m.contains('key')
removed = m.remove('key')
m.clear()
n = m.len()
empty = m.is-empty()
m.for-each(k, v)
```

#### map/str-set — str Hash Set

Fixed capacity 256, FNV-1a hash, string deduplication:

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

#### map/tree-map — Ordered Map (AVL Tree)

Ordered map (i64→i64) implemented based on AVL self-balancing binary search tree, capacity 64:

```no
m = tree-map {}
m.clear(); Initialize
ok = m.put(key, val); Insert or update
val = m.get(key); Lookup (?i64, nil=not found)
yes = m.contains(key); Check whether key exists
ok = m.remove(key); Delete key
key = m.first(); Minimum key (?i64)
key = m.last(); Maximum key (?i64)
key = m.lower-bound(target); First key ≥ target (?i64)
key = m.upper-bound(target); First key > target (?i64)
m.for-each(k, v); Traverse in ascending key order
sz = m.size()
yes = m.empty()
yes = m.full()
```

#### map/tree-set — Ordered Set (AVL Tree)

Ordered set (i64) implemented based on AVL self-balancing binary search tree, capacity 64:

```no
s = tree-set {}
s.clear(); Initialize
ok = s.add(key); Add element
yes = s.contains(key); Check whether it exists
ok = s.remove(key); Delete element
val = s.first(); Minimum value (?i64)
val = s.last(); Maximum value (?i64)
val = s.lower-bound(target); First element ≥ target (?i64)
val = s.upper-bound(target); First element > target (?i64)
s.for-each(val); Traverse in ascending order
sz = s.size()
yes = s.empty()
yes = s.full()
```

#### collection/queue — Generic Queue (Ring Buffer)

Ring buffer implementation based on fixed-length array, buffer provided by the `[n]t` receiver:

```no
buf [128]i64
q = buf.queue-init()
ok = buf.queue-push(q, val); Push to tail
val = buf.queue-pop(q); Pop from head (?t)
val = buf.queue-peek(q); Peek queue head (?t)
sz = q.size()
yes = q.empty()
yes = q.full()
q.clear()
```

#### collection/arr-stack — Generic Stack (Array-based)

Stack implementation based on fixed-length array, buffer provided by the `[n]t` receiver:

```no
buf [128]i64
s = buf.arr-stack-init()
ok = buf.arr-stack-push(s, val); Push
val = buf.arr-stack-pop(s); Pop (?t)
val = buf.arr-stack-peek(s); Peek top (?t)
sz = s.size()
yes = s.empty()
yes = s.full()
s.clear()
```

#### collection/link — Generic Doubly-linked List

Doubly-linked list based on fixed-length array node pool, values provided by the `[n]t` receiver:

```no
buf [128]i64
nxt [128]i64
prv [128]i64
l = buf.link-init(nxt, prv)
ok = buf.link-push-front(l, val); Insert at head
ok = buf.link-push-back(l, val); Insert at tail
val = buf.link-pop-front(l); Pop head (?t)
val = buf.link-pop-back(l); Pop tail (?t)
val = buf.link-peek-front(l); Peek head (?t)
val = buf.link-peek-back(l); Peek tail (?t)
sz = l.size()
yes = l.empty()
yes = l.full()
```

#### collection/map — Generic Dynamic Hash Map

Dynamic-capacity generic hash maps with automatic rehashing (load factor > 0.75 triggers capacity doubling). Three templates specialized by key type:

```no
; str-key map (V is generic)
m = hashmap-str-tmpl {}
m.init()
m.put('key', val)
result = m.get('key'); ?V, nil=not found
found = m.contains('key')
m.remove('key')
n = m.len()
yes = m.is-empty()
m.clear()

; int-key map (K, V both generic)
m2 = hashmap-int-tmpl {}
m2.init()
m2.put(k, v)

; bool-key map (V is generic)
m3 = hashmap-bool-tmpl {}
m3.init()
m3.put(flag, v)
```

#### collection/static-hashmap — Generic Fixed-capacity Hash Map

Fixed-capacity (256 slots) generic hash maps with linear probing. Three templates specialized by key type:

```no
; str-key static map (V is generic)
m = static-hashmap-str-tmpl {}
m.init()
m.put('key', val)
result = m.get('key'); ?V, nil=not found
found = m.contains('key')
m.remove('key')
n = m.len()

; int-key static map (K, V both generic)
m2 = static-hashmap-int-tmpl {}

; bool-key static map (V is generic, 2 slots)
m3 = static-hashmap-bool-tmpl {}
```

---
