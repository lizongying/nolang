---
sidebar_position: 3.6
---

## 자료 구조

### set — 집합 (배열 기반)

```no
new-n = set.add(s, n, val); 新增元素
new-n = set.set-remove(s, n, val); 移除元素
ok = set.contains(s, n, val); 是否包含
new-an = set.union(a, an, b, bn); 聯集
out, n = set.intersection(a, an, b, bn); 交集
out, n = set.difference(a, an, b, bn); 差集
v = set.to-vec(s, n); 轉切片
sz = set.set-size(s, n); 元素個數
yes = set.set-empty(s, n); 是否為空
```

### deque — 양방향 큐

순환 버퍼로 구현한 양방향 큐를 `deque` 구조체로 캡슐화했습니다:

```no
; 結構體
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
d.push-front(val); 從前端推入
d.push-back(val); 從後端推入
val = d.pop-front(); 從前端彈出
val = d.pop-back(); 從後端彈出
val = d.peek-front(); 查看前端元素（?i64, nil=空）
val = d.peek-back(); 查看後端元素（?i64, nil=空）
sz = d.size(); 大小
yes = d.empty(); 是否為空
d.clear(); 清空
```

### heap — 최소 힙

`heap` 구조체로 캡슐화한 이진 최소 힙입니다:

```no
; 結構體
heap {
    data []i64
    n i64
}

; 初始化
h = heap.init(data); 建立堆積

; 方法
h.push(val); 推入元素
val = h.pop(); 彈出最小元素（?i64, nil=空）
val = h.peek(); 查看最小元素（?i64, nil=空）
sz = h.size(); 大小
yes = h.empty(); 是否為空
```

### stack — 스택

후입선출(LIFO) 자료 구조로, `stack` 구조체로 캡슐화했습니다:

```no
; 結構體
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
val = s.pop(); 彈出頂端元素（?i64, nil=空）
val = s.peek(); 查看頂端元素（?i64, nil=空）
sz = s.size(); 大小
yes = s.empty(); 是否為空
s.clear(); 清空
```

### map/linked-hash-map — 정렬된 해시 테이블

고정 용량 64(i64→i64), 선형 탐사, 양방향 연결 리스트로 삽입 순서를 유지합니다:

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

### map/hash-set — i64 해시 집합

고정 용량 64, 선형 탐사, O(1) 조회/삽입/삭제:

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

### map/str-map — str→str 해시 맵

고정 용량 256, FNV-1a 해시, 선형 탐사:

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

### map/str-set — str 해시 집합

고정 용량 256, FNV-1a 해시, 문자열 중복 제거:

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

### map/tree-map — 정렬된 맵 (AVL 트리)

AVL 자기 균형 이진 탐색 트리로 구현한 정렬된 맵(i64→i64)이며, 용량은 64입니다:

```no
m = tree-map {}
m.clear(); 初始化
ok = m.put(key, val); 插入或更新
val = m.get(key); 查找（?i64, nil=未找到）
yes = m.contains(key); 檢查鍵是否存在
ok = m.remove(key); 刪除鍵
key = m.first(); 最小鍵（?i64）
key = m.last(); 最大鍵（?i64）
key = m.lower-bound(target); 第一個 ≥ target 的鍵（?i64）
key = m.upper-bound(target); 第一個 > target 的鍵（?i64）
m.for-each(k, v); 按鍵升序遍歷
sz = m.size()
yes = m.empty()
yes = m.full()
```

### map/tree-set — 정렬된 집합 (AVL 트리)

AVL 자기 균형 이진 탐색 트리로 구현한 정렬된 집합(i64)이며, 용량은 64입니다:

```no
s = tree-set {}
s.clear(); 初始化
ok = s.add(key); 加入元素
yes = s.contains(key); 檢查是否存在
ok = s.remove(key); 刪除元素
val = s.first(); 最小值（?i64）
val = s.last(); 最大值（?i64）
val = s.lower-bound(target); 第一個 ≥ target 的元素（?i64）
val = s.upper-bound(target); 第一個 > target 的元素（?i64）
s.for-each(val); 按升序遍歷
sz = s.size()
yes = s.empty()
yes = s.full()
```

### collection/queue — 범용 큐 (순환 버퍼)

고정 길이 배열 기반 순환 버퍼로 구현하며, 버퍼는 `[n]t` 수신자가 제공합니다:

```no
buf [128]i64
q = buf.queue-init()
ok = buf.queue-push(q, val); 推入尾端
val = buf.queue-pop(q); 從前端彈出（?t）
val = buf.queue-peek(q); 查看隊首（?t）
sz = q.size()
yes = q.empty()
yes = q.full()
q.clear()
```

### collection/arr-stack — 범용 스택 (고정 길이 배열 기반)

고정 길이 배열 기반 스택으로 구현하며, 버퍼는 `[n]t` 수신자가 제공합니다:

```no
buf [128]i64
s = buf.arr-stack-init()
ok = buf.arr-stack-push(s, val); 推入
val = buf.arr-stack-pop(s); 彈出（?t）
val = buf.arr-stack-peek(s); 查看頂端（?t）
sz = s.size()
yes = s.empty()
yes = s.full()
s.clear()
```

### collection/link — 범용 양방향 연결 리스트

고정 길이 배열 노드 풀 기반 양방향 연결 리스트이며, 값은 `[n]t` 수신자가 제공합니다:

```no
buf [128]i64
nxt [128]i64
prv [128]i64
l = buf.link-init(nxt, prv)
ok = buf.link-push-front(l, val); 插入頭部
ok = buf.link-push-back(l, val); 插入尾部
val = buf.link-pop-front(l); 彈出頭部（?t）
val = buf.link-pop-back(l); 彈出尾部（?t）
val = buf.link-peek-front(l); 查看頭部（?t）
val = buf.link-peek-back(l); 查看尾部（?t）
sz = l.size()
yes = l.empty()
yes = l.full()
```

### collection/map — 범용 동적 해시 맵

동적 용량의 범용 해시 맵으로, 적재 계수가 > 0.75가 되면 자동으로 rehash하여 확장합니다(용량이 두 배가 됩니다). 세 가지 템플릿이 키 타입별로 특화되어 있습니다:

```no
; str 鍵映射表（V 泛型）
m = hashmap-str-tmpl {}
m.init()
m.put('key', val)
result = m.get('key'); ?V，nil=未找到
found = m.contains('key')
m.remove('key')
n = m.len()
yes = m.is-empty()
m.clear()

; int 鍵映射表（K, V 均泛型）
m2 = hashmap-int-tmpl {}
m2.init()
m2.put(k, v)

; bool 鍵映射表（V 泛型）
m3 = hashmap-bool-tmpl {}
m3.init()
m3.put(flag, v)
```

### collection/static-hashmap — 범용 고정 용량 해시 맵

고정 용량(256 슬롯) 범용 해시 맵이며 선형 탐사를 사용합니다. 세 가지 템플릿이 키 타입별로 특화되어 있습니다:

```no
; str 鍵靜態映射表（V 泛型）
m = static-hashmap-str-tmpl {}
m.init()
m.put('key', val)
result = m.get('key'); ?V，nil=未找到
found = m.contains('key')
m.remove('key')
n = m.len()

; int 鍵靜態映射表（K, V 均泛型）
m2 = static-hashmap-int-tmpl {}

; bool 鍵靜態映射表（V 泛型，2 槽）
m3 = static-hashmap-bool-tmpl {}
```

---
