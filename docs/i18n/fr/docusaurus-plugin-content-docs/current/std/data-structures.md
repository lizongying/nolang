---
sidebar_position: 3.6
---

## Structures de données

### set — Ensemble (basé sur un tableau)

```no
new-n = set.add(s, n, val); ajoute un élément
new-n = set.set-remove(s, n, val); retire un élément
ok = set.contains(s, n, val); contient l'élément
new-an = set.union(a, an, b, bn); union
out, n = set.intersection(a, an, b, bn); intersection
out, n = set.difference(a, an, b, bn); différence
v = set.to-vec(s, n); convertir en tranche
sz = set.set-size(s, n); nombre d'éléments
yes = set.set-empty(s, n); est vide
```

### deque — File à deux extrémités

Une file à deux extrémités implémentée avec un tampon circulaire, encapsulée dans la structure `deque` :

```no
; Structure
deque {
    buf []i64
    cap i64
    head i64
    tail i64
}

; Initialisation
d = deque {
    buf: buf
    cap: 128
    head: 0
    tail: 0
}

; Méthodes
d.push-front(val); pousse par l'avant
d.push-back(val); pousse par l'arrière
val = d.pop-front(); dépile par l'avant
val = d.pop-back(); dépile par l'arrière
val = d.peek-front(); consulte l'élément de tête (?i64, nil=vide)
val = d.peek-back(); consulte l'élément de queue (?i64, nil=vide)
sz = d.size(); taille
yes = d.empty(); est vide
d.clear(); vide
```

### heap — Tas minimal

Un tas binaire minimal encapsulé dans la structure `heap` :

```no
; Structure
heap {
    data []i64
    n i64
}

; Initialisation
h = heap.init(data); crée le tas

; Méthodes
h.push(val); pousse un élément
val = h.pop(); dépile l'élément minimal (?i64, nil=vide)
val = h.peek(); consulte l'élément minimal (?i64, nil=vide)
sz = h.size(); taille
yes = h.empty(); est vide
```

### stack — Pile

Une structure de données last-in-first-out (LIFO), encapsulée dans la structure `stack` :

```no
; Structure
stack {
    data []i64
    n i64
}

; Initialisation
buf [128]i64
s = stack {
    data: buf
    n: 0
}

; Méthodes
s.push(val); pousse un élément
val = s.pop(); dépile l'élément du sommet (?i64, nil=vide)
val = s.peek(); consulte l'élément du sommet (?i64, nil=vide)
sz = s.size(); taille
yes = s.empty(); est vide
s.clear(); vide
```

### map/linked-hash-map — Table de hachage ordonnée

Capacité fixe 64 (i64→i64), sondage linéaire ; une liste doublement chaînée conserve l'ordre d'insertion :

```no
m = linked-hash-map {}
m.init()
m.put(key, val)
result = m.get(key); ?i64, nil=introuvable
found = m.contains(key)
removed = m.remove(key)
m.clear()
n = m.len()
empty = m.is-empty()
m.for-each(key, val)
```

### map/hash-set — Ensemble de hachage i64

Capacité fixe 64, sondage linéaire, recherche/insertion/suppression en O(1) :

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

### map/str-map — Table de hachage str→str

Capacité fixe 256, hachage FNV-1a, sondage linéaire :

```no
m = str-map {}
m.init()
m.put('key', 'val')
result = m.get('key'); ?str, nil=introuvable
found = m.contains('key')
removed = m.remove('key')
m.clear()
n = m.len()
empty = m.is-empty()
m.for-each(k, v)
```

### map/str-set — Ensemble de hachage str

Capacité fixe 256, hachage FNV-1a, déduplication des chaînes :

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

### map/tree-map — Table ordonnée (arbre AVL)

Une table ordonnée (i64→i64) implémentée sur un arbre binaire de recherche AVL auto-équilibré, de capacité 64 :

```no
m = tree-map {}
m.clear(); initialise
ok = m.put(key, val); insère ou met à jour
val = m.get(key); recherche (?i64, nil=introuvable)
yes = m.contains(key); vérifie l'existence de la clé
ok = m.remove(key); supprime la clé
key = m.first(); clé minimale (?i64)
key = m.last(); clé maximale (?i64)
key = m.lower-bound(target); première clé ≥ target (?i64)
key = m.upper-bound(target); première clé > target (?i64)
m.for-each(k, v); parcourt par clés croissantes
sz = m.size()
yes = m.empty()
yes = m.full()
```

### map/tree-set — Ensemble ordonné (arbre AVL)

Un ensemble ordonné (i64) implémenté sur un arbre binaire de recherche AVL auto-équilibré, de capacité 64 :

```no
s = tree-set {}
s.clear(); initialise
ok = s.add(key); ajoute un élément
yes = s.contains(key); vérifie l'existence
ok = s.remove(key); supprime un élément
val = s.first(); valeur minimale (?i64)
val = s.last(); valeur maximale (?i64)
val = s.lower-bound(target); premier élément ≥ target (?i64)
val = s.upper-bound(target); premier élément > target (?i64)
s.for-each(val); parcourt en ordre croissant
sz = s.size()
yes = s.empty()
yes = s.full()
```

### collection/queue — File générique (tampon circulaire)

Implémentée sur un tampon circulaire de tableau de longueur fixe ; le tampon est fourni par le récepteur `[n]t` :

```no
buf [128]i64
q = buf.queue-init()
ok = buf.queue-push(q, val); pousse à la queue
val = buf.queue-pop(q); dépile par l'avant (?t)
val = buf.queue-peek(q); consulte la tête (?t)
sz = q.size()
yes = q.empty()
yes = q.full()
q.clear()
```

### collection/arr-stack — Pile générique (basée sur un tableau de longueur fixe)

Une pile implémentée sur un tableau de longueur fixe ; le tampon est fourni par le récepteur `[n]t` :

```no
buf [128]i64
s = buf.arr-stack-init()
ok = buf.arr-stack-push(s, val); pousse
val = buf.arr-stack-pop(s); dépile (?t)
val = buf.arr-stack-peek(s); consulte le sommet (?t)
sz = s.size()
yes = s.empty()
yes = s.full()
s.clear()
```

### collection/link — Liste doublement chaînée générique

Une liste doublement chaînée basée sur un pool de nœuds en tableau de longueur fixe ; les valeurs sont fournies par le récepteur `[n]t` :

```no
buf [128]i64
nxt [128]i64
prv [128]i64
l = buf.link-init(nxt, prv)
ok = buf.link-push-front(l, val); insère en tête
ok = buf.link-push-back(l, val); insère en queue
val = buf.link-pop-front(l); dépile la tête (?t)
val = buf.link-pop-back(l); dépile la queue (?t)
val = buf.link-peek-front(l); consulte la tête (?t)
val = buf.link-peek-back(l); consulte la queue (?t)
sz = l.size()
yes = l.empty()
yes = l.full()
```

### collection/map — Table de hachage dynamique générique

Table de hachage générique à capacité dynamique, avec rehash automatique (doubling de la capacité) lorsque le facteur de charge dépasse 0,75. Trois modèles spécialisés par type de clé :

```no
; Table à clés str (V générique)
m = hashmap-str-tmpl {}
m.init()
m.put('key', val)
result = m.get('key'); ?V, nil=introuvable
found = m.contains('key')
m.remove('key')
n = m.len()
yes = m.is-empty()
m.clear()

; Table à clés int (K et V génériques)
m2 = hashmap-int-tmpl {}
m2.init()
m2.put(k, v)

; Table à clés bool (V générique)
m3 = hashmap-bool-tmpl {}
m3.init()
m3.put(flag, v)
```

### collection/static-hashmap — Table de hachage générique à capacité fixe

Table de hachage générique à capacité fixe (256 emplacements), sondage linéaire. Trois modèles spécialisés par type de clé :

```no
; Table statique à clés str (V générique)
m = static-hashmap-str-tmpl {}
m.init()
m.put('key', val)
result = m.get('key'); ?V, nil=introuvable
found = m.contains('key')
m.remove('key')
n = m.len()

; Table statique à clés int (K et V génériques)
m2 = static-hashmap-int-tmpl {}

; Table statique à clés bool (V générique, 2 emplacements)
m3 = static-hashmap-bool-tmpl {}
```

---
