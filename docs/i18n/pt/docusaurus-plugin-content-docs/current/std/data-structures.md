---
sidebar_position: 3.6
---

## Estruturas de dados

### set — conjunto (baseado em array)

```no
new-n = set.add(s, n, val); adiciona elemento
new-n = set.set-remove(s, n, val); remove elemento
ok = set.contains(s, n, val); contém o elemento
new-an = set.union(a, an, b, bn); união
out, n = set.intersection(a, an, b, bn); interseção
out, n = set.difference(a, an, b, bn); diferença
v = set.to-vec(s, n); converte em slice
sz = set.set-size(s, n); número de elementos
yes = set.set-empty(s, n); está vazio
```

### deque — fila de duas extremidades

Uma fila de duas extremidades implementada com um buffer circular, encapsulada na struct `deque`:

```no
; Estrutura
deque {
    buf []i64
    cap i64
    head i64
    tail i64
}

; Inicialização
d = deque {
    buf: buf
    cap: 128
    head: 0
    tail: 0
}

; Métodos
d.push-front(val); inserir pela frente
d.push-back(val); inserir por trás
val = d.pop-front(); retirar pela frente
val = d.pop-back(); retirar por trás
val = d.peek-front(); ver o elemento da frente（?i64, nil=vazio）
val = d.peek-back(); ver o elemento de trás（?i64, nil=vazio）
sz = d.size(); tamanho
yes = d.empty(); está vazio
d.clear(); limpar
```

### heap — heap mínimo

Um heap mínimo binário encapsulado na struct `heap`:

```no
; Estrutura
heap {
    data []i64
    n i64
}

; Inicialização
h = heap.init(data); cria o heap

; Métodos
h.push(val); inserir elemento
val = h.pop(); retirar o elemento mínimo（?i64, nil=vazio）
val = h.peek(); ver o elemento mínimo（?i64, nil=vazio）
sz = h.size(); tamanho
yes = h.empty(); está vazio
```

### stack — pilha

Estrutura do tipo último a entrar, primeiro a sair (LIFO), encapsulada na struct `stack`:

```no
; Estrutura
stack {
    data []i64
    n i64
}

; Inicialização
buf [128]i64
s = stack {
    data: buf
    n: 0
}

; Métodos
s.push(val); inserir elemento
val = s.pop(); retirar o elemento do topo（?i64, nil=vazio）
val = s.peek(); ver o elemento do topo（?i64, nil=vazio）
sz = s.size(); tamanho
yes = s.empty(); está vazio
s.clear(); limpar
```

### map/linked-hash-map — mapa hash ordenado

Capacidade fixa 64 (i64→i64), sondagem linear, lista duplamente ligada preserva a ordem de inserção:

```no
m = linked-hash-map {}
m.init()
m.put(key, val)
result = m.get(key); ?i64, nil=não encontrado
found = m.contains(key)
removed = m.remove(key)
m.clear()
n = m.len()
empty = m.is-empty()
m.for-each(key, val)
```

### map/hash-set — conjunto hash de i64

Capacidade fixa 64, sondagem linear, pesquisa/inserção/remoção em O(1):

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

### map/str-map — mapa hash str→str

Capacidade fixa 256, hash FNV-1a, sondagem linear:

```no
m = str-map {}
m.init()
m.put('key', 'val')
result = m.get('key'); ?str, nil=não encontrado
found = m.contains('key')
removed = m.remove('key')
m.clear()
n = m.len()
empty = m.is-empty()
m.for-each(k, v)
```

### map/str-set — conjunto hash de str

Capacidade fixa 256, hash FNV-1a, deduplicação de strings:

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

### map/tree-map — mapa ordenado (árvore AVL)

Mapa ordenado (i64→i64) implementado sobre uma árvore de pesquisa binária AVL autoequilibrada, capacidade 64:

```no
m = tree-map {}
m.clear(); inicializar
ok = m.put(key, val); inserir ou atualizar
val = m.get(key); pesquisar（?i64, nil=não encontrado）
yes = m.contains(key); verificar se a chave existe
ok = m.remove(key); eliminar chave
key = m.first(); chave mínima（?i64）
key = m.last(); chave máxima（?i64）
key = m.lower-bound(target); primeira chave ≥ target（?i64）
key = m.upper-bound(target); primeira chave > target（?i64）
m.for-each(k, v); percorrer por ordem ascendente de chave
sz = m.size()
yes = m.empty()
yes = m.full()
```

### map/tree-set — conjunto ordenado (árvore AVL)

Conjunto ordenado (i64) implementado sobre uma árvore de pesquisa binária AVL autoequilibrada, capacidade 64:

```no
s = tree-set {}
s.clear(); inicializar
ok = s.add(key); adicionar elemento
yes = s.contains(key); verificar se existe
ok = s.remove(key); eliminar elemento
val = s.first(); valor mínimo（?i64）
val = s.last(); valor máximo（?i64）
val = s.lower-bound(target); primeiro elemento ≥ target（?i64）
val = s.upper-bound(target); primeiro elemento > target（?i64）
s.for-each(val); percorrer por ordem ascendente
sz = s.size()
yes = s.empty()
yes = s.full()
```

### collection/queue — fila genérica (buffer circular)

Implementada sobre um buffer circular de array de comprimento fixo; o buffer é fornecido pelo recetor `[n]t`:

```no
buf [128]i64
q = buf.queue-init()
ok = buf.queue-push(q, val); inserir na cauda
val = buf.queue-pop(q); retirar pela frente（?t）
val = buf.queue-peek(q); ver a cabeça da fila（?t）
sz = q.size()
yes = q.empty()
yes = q.full()
q.clear()
```

### collection/arr-stack — pilha genérica (baseada em array de comprimento fixo)

Implementação de pilha baseada num array de comprimento fixo; o buffer é fornecido pelo recetor `[n]t`:

```no
buf [128]i64
s = buf.arr-stack-init()
ok = buf.arr-stack-push(s, val); inserir
val = buf.arr-stack-pop(s); retirar（?t）
val = buf.arr-stack-peek(s); ver o topo（?t）
sz = s.size()
yes = s.empty()
yes = s.full()
s.clear()
```

### collection/link — lista duplamente ligada genérica

Lista duplamente ligada baseada num pool de nós de array de comprimento fixo; os valores são fornecidos pelo recetor `[n]t`:

```no
buf [128]i64
nxt [128]i64
prv [128]i64
l = buf.link-init(nxt, prv)
ok = buf.link-push-front(l, val); inserir na cabeça
ok = buf.link-push-back(l, val); inserir na cauda
val = buf.link-pop-front(l); retirar da cabeça（?t）
val = buf.link-pop-back(l); retirar da cauda（?t）
val = buf.link-peek-front(l); ver a cabeça（?t）
val = buf.link-peek-back(l); ver a cauda（?t）
sz = l.size()
yes = l.empty()
yes = l.full()
```

### collection/map — mapa hash dinâmico genérico

Mapa hash genérico de capacidade dinâmica; faz rehash automático (duplicando a capacidade) quando o fator de carga ultrapassa 0,75. Três modelos especializados pelo tipo da chave:

```no
; Mapa com chave str（V genérico）
m = hashmap-str-tmpl {}
m.init()
m.put('key', val)
result = m.get('key'); ?V, nil=não encontrado
found = m.contains('key')
m.remove('key')
n = m.len()
yes = m.is-empty()
m.clear()

; Mapa com chave int（K e V ambos genéricos）
m2 = hashmap-int-tmpl {}
m2.init()
m2.put(k, v)

; Mapa com chave bool（V genérico）
m3 = hashmap-bool-tmpl {}
m3.init()
m3.put(flag, v)
```

### collection/static-hashmap — mapa hash de capacidade fixa genérico

Mapa hash genérico de capacidade fixa (256 slots), com sondagem linear. Três modelos especializados pelo tipo da chave:

```no
; Mapa estático com chave str（V genérico）
m = static-hashmap-str-tmpl {}
m.init()
m.put('key', val)
result = m.get('key'); ?V, nil=não encontrado
found = m.contains('key')
m.remove('key')
n = m.len()

; Mapa estático com chave int（K e V ambos genéricos）
m2 = static-hashmap-int-tmpl {}

; Mapa estático com chave bool（V genérico, 2 slots）
m3 = static-hashmap-bool-tmpl {}
```

---
