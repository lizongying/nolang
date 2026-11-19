---
sidebar_position: 3.6
---

## データ構造

### set — 集合（配列ベース）

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

### deque — 両端キュー

循環バッファで実装した両端キューを `deque` 構造体でカプセル化したものです：

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

### heap — 最小ヒープ

`heap` 構造体でカプセル化した二分最小ヒープ：

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

### stack — スタック

後入れ先出し（LIFO）のデータ構造で、`stack` 構造体でカプセル化したものです：

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

### map/linked-hash-map — 順序付きハッシュテーブル

固定容量 64（i64→i64）、線形 probing、雙方向連結リストで挿入順序を保持します：

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

### map/hash-set — i64 ハッシュ集合

固定容量 64、線形 probing、O(1) の検索/挿入/削除：

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

### map/str-map — str→str ハッシュマップ

固定容量 256、FNV-1a ハッシュ、線形 probing：

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

### map/str-set — str ハッシュ集合

固定容量 256、FNV-1a ハッシュ、文字列の重複排除：

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

### map/tree-map — 順序付きマップ（AVL 木）

AVL 自己平衡二分探索木に基づく順序付きマップ（i64→i64）で、容量は 64 です：

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

### map/tree-set — 順序付き集合（AVL 木）

AVL 自己平衡二分探索木に基づく順序付き集合（i64）で、容量は 64 です：

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

### collection/queue — ジェネリックキュー（循環バッファ）

固定長配列に基づく循環バッファで実装され、バッファは `[n]t` レシーバーによって提供されます：

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

### collection/arr-stack — ジェネリックスタック（固定長配列ベース）

固定長配列に基づくスタックで実装され、バッファは `[n]t` レシーバーによって提供されます：

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

### collection/link — ジェネリック雙方向連結リスト

固定長配列のノードプールに基づく雙方向連結リストで、値は `[n]t` レシーバーによって提供されます：

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

### collection/map — ジェネリック動的ハッシュマップ

動的容量のジェネリックハッシュマップで、負荷係数が > 0.75 になると自動的に rehash して拡張します（容量を倍増）。三つのテンプレートがキー型別ごとに特化されています：

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

### collection/static-hashmap — ジェネリック固定容量ハッシュマップ

固定容量（256 スロット）のジェネリックハッシュマップで、線形 probing を使用します。三つのテンプレートがキー型別ごとに特化されています：

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
