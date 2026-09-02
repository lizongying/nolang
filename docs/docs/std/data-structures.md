---
sidebar_position: 3.6
---

## 資料結構

### set — 集合（基於陣列）

```no
new-n = set.add(s, n, val)           ; 新增元素
new-n = set.set-remove(s, n, val)        ; 移除元素
ok = set.contains(s, n, val)         ; 是否包含
new-an = set.union(a, an, b, bn)     ; 聯集
out, n = set.intersection(a, an, b, bn); 交集
out, n = set.difference(a, an, b, bn)  ; 差集
v = set.to-vec(s, n)                 ; 轉切片
sz = set.set-size(s, n)                   ; 元素個數
yes = set.set-empty(s, n)                    ; 是否為空
```

### deque — 雙端佇列

使用循環緩衝區實作的雙端佇列，以 `deque` 結構體封裝：

```no
; 結構體
deque {
    buf []i64
    cap i64
    head i64
    tail i64
}

; 初始化
d = deque{
    buf: buf
    cap: 128
    head: 0
    tail: 0
}

; 方法
d.push-front(val)              ; 從前端推入
d.push-back(val)               ; 從後端推入
val = d.pop-front()             ; 從前端彈出
val = d.pop-back()              ; 從後端彈出
val = d.peek-front()            ; 查看前端元素（?i64, nil=空）
val = d.peek-back()             ; 查看後端元素（?i64, nil=空）
sz = d.size()                   ; 大小
yes = d.empty()                 ; 是否為空
d.clear()                      ; 清空
```

### heap — 最小堆

以 `heap` 結構體封裝的二元最小堆積：

```no
; 結構體
heap {
    data []i64
    n i64
}

; 初始化
h = heap.init(data)            ; 建立堆積

; 方法
h.push(val)                    ; 推入元素
val = h.pop()                  ; 彈出最小元素（?i64, nil=空）
val = h.peek()                 ; 查看最小元素（?i64, nil=空）
sz = h.size()                  ; 大小
yes = h.empty()                ; 是否為空
```

### stack — 堆疊

後進先出（LIFO）資料結構，以 `stack` 結構體封裝：

```no
; 結構體
stack {
    data []i64
    n i64
}

; 初始化
buf [128]i64 = [0:128]
s = stack{
    data: buf
    n: 0
}

; 方法
s.push(val)                    ; 推入元素
val = s.pop()                  ; 彈出頂端元素（?i64, nil=空）
val = s.peek()                 ; 查看頂端元素（?i64, nil=空）
sz = s.size()                  ; 大小
yes = s.empty()                ; 是否為空
s.clear()                      ; 清空
```

### map/linked-hash-map — 有序哈希表

固定容量 64（i64→i64），線性探測，雙向鏈表保持插入順序：

```no
m = linked-hash-map{}
m.init()
m.put(key, val)
result = m.get(key)   ; ?i64, nil=未找到
found = m.contains(key)
removed = m.remove(key)
m.clear()
n = m.len()
empty = m.is-empty()
m.for-each(key, val)
```

### map/hash-set — i64 哈希集合

固定容量 64，線性探測，O(1) 查找/插入/刪除：

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

### map/str-map — str→str 哈希映射表

固定容量 256，FNV-1a 雜湊，線性探測：

```no
m = str-map{}
m.init()
m.put('key', 'val')
result = m.get('key')   ; ?str, nil=未找到
found = m.contains('key')
removed = m.remove('key')
m.clear()
n = m.len()
empty = m.is-empty()
m.for-each(k, v)
```

### map/str-set — str 哈希集合

固定容量 256，FNV-1a 雜湊，字串去重：

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

### map/tree-map — 有序映射表（AVL 樹）

基於 AVL 自平衡二元搜尋樹實現的有序映射表（i64→i64），容量 64：

```no
m = tree-map{}
m.clear()                           ; 初始化
ok = m.put(key, val)                ; 插入或更新
val = m.get(key)                    ; 查找（?i64, nil=未找到）
yes = m.contains(key)               ; 檢查鍵是否存在
ok = m.remove(key)                  ; 刪除鍵
key = m.first()                     ; 最小鍵（?i64）
key = m.last()                      ; 最大鍵（?i64）
key = m.lower-bound(target)         ; 第一個 ≥ target 的鍵（?i64）
key = m.upper-bound(target)         ; 第一個 > target 的鍵（?i64）
m.for-each(k, v)                    ; 按鍵升序遍歷
sz = m.size()
yes = m.empty()
yes = m.full()
```

### map/tree-set — 有序集合（AVL 樹）

基於 AVL 自平衡二元搜尋樹實現的有序集合（i64），容量 64：

```no
s = tree-set{}
s.clear()                           ; 初始化
ok = s.add(key)                     ; 加入元素
yes = s.contains(key)               ; 檢查是否存在
ok = s.remove(key)                  ; 刪除元素
val = s.first()                     ; 最小值（?i64）
val = s.last()                      ; 最大值（?i64）
val = s.lower-bound(target)         ; 第一個 ≥ target 的元素（?i64）
val = s.upper-bound(target)         ; 第一個 > target 的元素（?i64）
s.for-each(val)                     ; 按升序遍歷
sz = s.size()
yes = s.empty()
yes = s.full()
```

### collection/queue — 泛型佇列（環形緩衝區）

基於定長陣列的環形緩衝區實現，緩衝區由 `[n]t` 接收者提供：

```no
buf [128]i64 = [0:128]
q = buf.queue-init()
ok = buf.queue-push(q, val)         ; 推入尾端
val = buf.queue-pop(q)              ; 從前端彈出（?t）
val = buf.queue-peek(q)             ; 查看隊首（?t）
sz = q.size()
yes = q.empty()
yes = q.full()
q.clear()
```

### collection/arr-stack — 泛型堆疊（基於定長陣列）

基於定長陣列的堆疊實現，緩衝區由 `[n]t` 接收者提供：

```no
buf [128]i64 = [0:128]
s = buf.arr-stack-init()
ok = buf.arr-stack-push(s, val)     ; 推入
val = buf.arr-stack-pop(s)          ; 彈出（?t）
val = buf.arr-stack-peek(s)         ; 查看頂端（?t）
sz = s.size()
yes = s.empty()
yes = s.full()
s.clear()
```

### collection/link — 泛型雙向鏈結串列

基於定長陣列節點池的雙向鏈結串列，值由 `[n]t` 接收者提供：

```no
buf [128]i64 = [0:128]
nxt [128]i64 = [0:128]
prv [128]i64 = [0:128]
l = buf.link-init(nxt, prv)
ok = buf.link-push-front(l, val)    ; 插入頭部
ok = buf.link-push-back(l, val)     ; 插入尾部
val = buf.link-pop-front(l)         ; 彈出頭部（?t）
val = buf.link-pop-back(l)          ; 彈出尾部（?t）
val = buf.link-peek-front(l)        ; 查看頭部（?t）
val = buf.link-peek-back(l)         ; 查看尾部（?t）
sz = l.size()
yes = l.empty()
yes = l.full()
```

### collection/map — 泛型動態雜湊映射表

動態容量泛型雜湊映射表，裝載因子 > 0.75 時自動 rehash 擴容（容量加倍）。三個模板按鍵型別特化：

```no
; str 鍵映射表（V 泛型）
m = hashmap-str-tmpl{}
m.init()
m.put('key', val)
result = m.get('key')   ; ?V，nil=未找到
found = m.contains('key')
m.remove('key')
n = m.size()
yes = m.empty()
m.clear()

; int 鍵映射表（K, V 均泛型）
m2 = hashmap-int-tmpl{}
m2.init()
m2.put(k, v)

; bool 鍵映射表（V 泛型）
m3 = hashmap-bool-tmpl{}
m3.init()
m3.put(flag, v)
```

### collection/static-hashmap — 泛型固定容量雜湊映射表

固定容量（256 槽）泛型雜湊映射表，線性探測。三個模板按鍵型別特化：

```no
; str 鍵靜態映射表（V 泛型）
m = static-hashmap-str-tmpl{}
m.init()
m.put('key', val)
result = m.get('key')   ; ?V，nil=未找到
found = m.contains('key')
m.remove('key')
n = m.size()

; int 鍵靜態映射表（K, V 均泛型）
m2 = static-hashmap-int-tmpl{}

; bool 鍵靜態映射表（V 泛型，2 槽）
m3 = static-hashmap-bool-tmpl{}
```

---
