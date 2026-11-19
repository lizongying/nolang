---
sidebar_position: 3.6
---

## 资料结构

### set — 集合（基于阵列）

```no
new-n = set.add(s, n, val); 新增元素
new-n = set.set-remove(s, n, val); 移除元素
ok = set.contains(s, n, val); 是否包含
new-an = set.union(a, an, b, bn); 联集
out, n = set.intersection(a, an, b, bn); 交集
out, n = set.difference(a, an, b, bn); 差集
v = set.to-vec(s, n); 转切片
sz = set.set-size(s, n); 元素个数
yes = set.set-empty(s, n); 是否为空
```

### deque — 双端伫列

使用循环缓冲区实作的双端伫列，以 `deque` 结构体封装：

```no
; 结构体
deque {
    buf []i64
    cap i64
    head i64
    tail i64
}

; 初始化
d = deque {
    buf: buf
    cap: 128
    head: 0
    tail: 0
}

; 方法
d.push-front(val); 从前端推入
d.push-back(val); 从后端推入
val = d.pop-front(); 从前端弹出
val = d.pop-back(); 从后端弹出
val = d.peek-front(); 查看前端元素（?i64, nil=空）
val = d.peek-back(); 查看后端元素（?i64, nil=空）
sz = d.size(); 大小
yes = d.empty(); 是否为空
d.clear(); 清空
```

### heap — 最小堆

以 `heap` 结构体封装的二元最小堆积：

```no
; 结构体
heap {
    data []i64
    n i64
}

; 初始化
h = heap.init(data); 建立堆积

; 方法
h.push(val); 推入元素
val = h.pop(); 弹出最小元素（?i64, nil=空）
val = h.peek(); 查看最小元素（?i64, nil=空）
sz = h.size(); 大小
yes = h.empty(); 是否为空
```

### stack — 堆叠

后进先出（LIFO）资料结构，以 `stack` 结构体封装：

```no
; 结构体
stack {
    data []i64
    n i64
}

; 初始化
buf [128]i64
s = stack {
    data: buf
    n: 0
}

; 方法
s.push(val); 推入元素
val = s.pop(); 弹出顶端元素（?i64, nil=空）
val = s.peek(); 查看顶端元素（?i64, nil=空）
sz = s.size(); 大小
yes = s.empty(); 是否为空
s.clear(); 清空
```

### map/linked-hash-map — 有序哈希表

固定容量 64（i64→i64），线性探测，双向链表保持插入顺序：

```no
m = linked-hash-map {}
m.init()
m.put(key, val)
result = m.get(key); ?i64, nil=未找到
found = m.contains(key)
removed = m.remove(key)
m.clear()
n = m.len()
empty = m.is-empty()
m.for-each(key, val)
```

### map/hash-set — i64 哈希集合

固定容量 64，线性探测，O(1) 查找/插入/删除：

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

### map/str-map — str→str 哈希映射表

固定容量 256，FNV-1a 杂凑，线性探测：

```no
m = str-map {}
m.init()
m.put('key', 'val')
result = m.get('key'); ?str, nil=未找到
found = m.contains('key')
removed = m.remove('key')
m.clear()
n = m.len()
empty = m.is-empty()
m.for-each(k, v)
```

### map/str-set — str 哈希集合

固定容量 256，FNV-1a 杂凑，字串去重：

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

### map/tree-map — 有序映射表（AVL 树）

基于 AVL 自平衡二元搜寻树实现的有序映射表（i64→i64），容量 64：

```no
m = tree-map {}
m.clear(); 初始化
ok = m.put(key, val); 插入或更新
val = m.get(key); 查找（?i64, nil=未找到）
yes = m.contains(key); 检查键是否存在
ok = m.remove(key); 删除键
key = m.first(); 最小键（?i64）
key = m.last(); 最大键（?i64）
key = m.lower-bound(target); 第一个 ≥ target 的键（?i64）
key = m.upper-bound(target); 第一个 > target 的键（?i64）
m.for-each(k, v); 按键升序遍历
sz = m.size()
yes = m.empty()
yes = m.full()
```

### map/tree-set — 有序集合（AVL 树）

基于 AVL 自平衡二元搜寻树实现的有序集合（i64），容量 64：

```no
s = tree-set {}
s.clear(); 初始化
ok = s.add(key); 加入元素
yes = s.contains(key); 检查是否存在
ok = s.remove(key); 删除元素
val = s.first(); 最小值（?i64）
val = s.last(); 最大值（?i64）
val = s.lower-bound(target); 第一个 ≥ target 的元素（?i64）
val = s.upper-bound(target); 第一个 > target 的元素（?i64）
s.for-each(val); 按升序遍历
sz = s.size()
yes = s.empty()
yes = s.full()
```

### collection/queue — 泛型伫列（环形缓冲区）

基于定长阵列的环形缓冲区实现，缓冲区由 `[n]t` 接收者提供：

```no
buf [128]i64
q = buf.queue-init()
ok = buf.queue-push(q, val); 推入尾端
val = buf.queue-pop(q); 从前端弹出（?t）
val = buf.queue-peek(q); 查看队首（?t）
sz = q.size()
yes = q.empty()
yes = q.full()
q.clear()
```

### collection/arr-stack — 泛型堆叠（基于定长阵列）

基于定长阵列的堆叠实现，缓冲区由 `[n]t` 接收者提供：

```no
buf [128]i64
s = buf.arr-stack-init()
ok = buf.arr-stack-push(s, val); 推入
val = buf.arr-stack-pop(s); 弹出（?t）
val = buf.arr-stack-peek(s); 查看顶端（?t）
sz = s.size()
yes = s.empty()
yes = s.full()
s.clear()
```

### collection/link — 泛型双向链结串列

基于定长阵列节点池的双向链结串列，值由 `[n]t` 接收者提供：

```no
buf [128]i64
nxt [128]i64
prv [128]i64
l = buf.link-init(nxt, prv)
ok = buf.link-push-front(l, val); 插入头部
ok = buf.link-push-back(l, val); 插入尾部
val = buf.link-pop-front(l); 弹出头部（?t）
val = buf.link-pop-back(l); 弹出尾部（?t）
val = buf.link-peek-front(l); 查看头部（?t）
val = buf.link-peek-back(l); 查看尾部（?t）
sz = l.size()
yes = l.empty()
yes = l.full()
```

### collection/map — 泛型动态杂凑映射表

动态容量泛型杂凑映射表，装载因子 > 0.75 时自动 rehash 扩容（容量加倍）。三个模板按键型别特化：

```no
; str 键映射表（V 泛型）
m = hashmap-str-tmpl {}
m.init()
m.put('key', val)
result = m.get('key'); ?V，nil=未找到
found = m.contains('key')
m.remove('key')
n = m.len()
yes = m.is-empty()
m.clear()

; int 键映射表（K, V 均泛型）
m2 = hashmap-int-tmpl {}
m2.init()
m2.put(k, v)

; bool 键映射表（V 泛型）
m3 = hashmap-bool-tmpl {}
m3.init()
m3.put(flag, v)
```

### collection/static-hashmap — 泛型固定容量杂凑映射表

固定容量（256 槽）泛型杂凑映射表，线性探测。三个模板按键型别特化：

```no
; str 键静态映射表（V 泛型）
m = static-hashmap-str-tmpl {}
m.init()
m.put('key', val)
result = m.get('key'); ?V，nil=未找到
found = m.contains('key')
m.remove('key')
n = m.len()

; int 键静态映射表（K, V 均泛型）
m2 = static-hashmap-int-tmpl {}

; bool 键静态映射表（V 泛型，2 槽）
m3 = static-hashmap-bool-tmpl {}
```

---
