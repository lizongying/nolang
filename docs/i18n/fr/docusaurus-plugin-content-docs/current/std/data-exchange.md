---
sidebar_position: 4.1
---

## Échange de données

### json — Analyse et génération JSON

`json` conserve un arbre JSON dans un **pool de nœuds alloué sur le tas** (`json-pool`) ; il prend en charge les tableaux et objets imbriqués, les scalaires chaîne/nombre/booléen/null, les caractères d'échappement et les nombres en notation exponentielle. Le pool utilise désormais des `vec` dynamiques et **n'a aucune limite de capacité fixe** — le nombre total de nœuds et le nombre d'enfants par nœud ne sont limités que par la mémoire disponible. L'interface de haut niveau encapsule le pool et l'index du nœud racine dans une structure `json` ; son champ `pool` porte l'annotation `#{inline=false}` (champ pointeur), ce qui permet de retourner et de copier `json` par valeur en toute sécurité.

```no
; Analyse : retourne ?json (ok=succès, err=échec avec la raison)
j = json.parse('{"name":"Alice","age":30,"items":[1,2,3],"active":true}')
j: {
    ok -> {
        name = it.get-str('name'); lit un scalaire : get-str / get-num / get-bool / get-i64 (?valeur)
        age = it.get-i64('age'); lecture d'un entier (?i64)
        items = it.get('items'); lit un nœud enfant, retourne ?json
        s = it.stringify(); sérialise en chaîne
    }

    err(e) -> print(e)
}

; Génération : partir d'un json vide, puis set / arr-push progressifs
j2 = json.new()
j2.set-str('key', 'value')
j2.set-i64('count', 3)
j2.arr-push-str('a')
print(j2.stringify())
```

**API de haut niveau (structure `json`)**

- `json.new()` — crée un `json` vide (null)
- `json.parse(s)` — analyse, retourne `?json` ; en cas d'échec, la branche `err` porte la raison précise (`empty input` / `parse error` / `trailing characters`)
- `j.stringify()` — sérialise en chaîne
- Lecture : `j.get-str(key)` / `j.get-num(key)` / `j.get-bool(key)` / `j.get-i64(key)` (retournent chacun `?valeur`) ; `j.get(key)` retourne un nœud enfant `?json`
- Accès direct au scalaire racine (lorsque la racine est un scalaire) : `j.str()` / `j.num()` / `j.bool()` / `j.i64()` / `j.str-val()`
- Écriture : `j.set-str(key,val)` / `j.set-num` / `j.set-i64` / `j.set-bool` / `j.set-null` / `j.set-key(key, val json)`
- Tableau : `j.arr-get(i)` / `j.arr-len()` / `j.arr-push(val json)` / `j.arr-push-str` / `j.arr-push-num` / `j.arr-push-bool`
- Énumération d'objet : `j.obj-len()` / `j.obj-key(i)` / `j.obj-keys-str()` / `j.delete-key(key)`
- Test de type : `j.kind()` et `j.is-null()` / `j.is-obj()` / `j.is-arr()` / `j.is-str()` / `j.is-num()` / `j.is-bool()`

**Capacité du pool (aucune limite fixe)**

Les premières versions utilisaient un tableau de taille fixe comme pool, avec une limite stricte `JSON-MAX-NODES = 128` / `JSON-MAX-CHILDREN = 32`, au-delà de laquelle l'opération échouait silencieusement. Tout est désormais passé à un `vec` sur le tas :

- Le nombre total de nœuds comme le nombre d'enfants par nœud **n'ont aucune limite**, si ce n'est la mémoire disponible.
- `json.parse` ne retourne `err` que sur une véritable erreur de syntaxe ; il n'y a plus de branche d'erreur « pool épuisé ».
- `j.set*` / `j.arr-push*` ne retournent `false` que lorsque le type de la cible ne correspond pas (par ex. un set sur un nœud non-objet, un push sur un nœud non-tableau). `j.overflowed()` est conservé pour la compatibilité ascendante et retourne désormais toujours `false`.

> **Sémantique de copie de `json` (`#{inline=false}`)** : `pool` est un champ pointeur ; les nœuds enfants retournés par `j.get` / `j.arr-get` **copient en profondeur** tout le pool, constituant un instantané indépendant du pool parent — les écritures dans l'instantané ne se répercutent pas sur le nœud parent. En outre, comme `?json` (option) consomme sa valeur lors d'un `match`, **refaire un `match`** sur la même option fait basculer dans la branche `nil` ; si vous devez effectuer plusieurs opérations sur le même résultat d'analyse, faites-les dans un seul `match`, ou récupérez d'abord `it` dans une variable locale `json`.

**API de bas niveau (`json-pool` / `json-value`)**

On opère directement par index de nœud, le pool servant de variable locale (une structure à un seul niveau peut être retournée/copiée par valeur en toute sécurité) :

- `p = json.new-pool()` ; ou `p = json-pool {}` puis `p.init()` (initialise chacun des vec `nodes`/`strs`/`ec`/`ek`/`en` avec `with-len(0)`)
- `idx = p.alloc()` — alloue un nouveau nœud et retourne son index (le vec sur le tas s'agrandit automatiquement)
- `p.add-child(node-idx, child-idx, key)` — ajoute un enfant via une liste d'arêtes (`ec`/`ek`/`en` listes chaînées à sens unique), sans limite par nœud
- `node-idx, next-pos, ok = p.parse(s, pos)` — analyse une valeur à partir de `pos`, retourne l'index du nœud
- `val-idx = p.get-key(obj-idx, key)` (`?i64`) ; `p.arr-get(arr-idx, i)` (`?i64`) ; `p.arr-len(arr-idx)`
- `p.get-kind(idx)` / `p.get-str(idx)` / `p.get-num(idx)` / `p.get-bool(idx)`
- `p.set-key(obj-idx, key, val-idx)` ; `p.copy-tree(src, idx)` (copie profonde récursive d'un sous-arbre)
- `p.stringify(node-idx)` — sérialise le nœud indiqué

### toml — Analyse et génération TOML 1.0

`toml` conserve un document TOML dans une structure `toml` ; il prend en charge les commentaires, les clés bare/quoted/dotted, les tables ordinaires, les array-of-tables, les chaînes basic/literal/multiline, les diverses bases d'entiers, les nombres à virgule flottante, les booléens, les dates-heures, les tableaux et les tables en ligne. Les valeurs sans type Nolang dédié conservent leur littéral d'origine.

```no
cfg = toml.parse('title = "Nolang"\n[server]\nport = 8080\n[[server.backends]]\nname = "local"')

doc = toml.new()
doc.put('name', '"nolang"')
doc.put('server.port', '8080')
doc.put('enabled', 'true')
raw = toml.get(doc, 'server.port')
text = toml.stringify(doc)
```

Fonctions courantes :

- `toml.new()` — crée un document vide
- `toml.parse(text)` — analyse et retourne `?toml`
- `doc.put(key, raw-value)` — valide puis ajoute/met à jour une valeur TOML
- `get(doc, key)` — obtient la valeur brute
- `get-str`、`get-i64`、`get-f64`、`get-bool` — obtient une valeur scalaire
- `toml.stringify(doc)` — sérialise le document

Les limites du document sont de 4 clés, 2 tables ordinaires et 2 array-of-tables ; une clé fait au plus 32 octets et une valeur au plus 128 octets.

### yaml — Analyse et génération YAML 1.2

`yaml` conserve un arbre de document au schema YAML 1.2 core dans un pool de nœuds (`pool`) ; il prend en charge les mappings en bloc, les séquences en bloc, les collections de flux (`[...]` / `{...}`), les scalaires entre guillemets simples/doubles ou plain, les scalaires de bloc (`|` / `>` avec chomping `-` / `+`), les commentaires, les marqueurs de document (`---` / `...`) et les documents multiples, les ancres (`&`) et les alias (`*`), la clé de fusion `<<`, les clés explicites (`? k`), l'inférence de type (null / bool / int / float / `.inf` / `.nan`), la recherche par chemin pointé, ainsi que la sérialisation JSON / YAML.

```no
doc ?yaml = yaml.parse('name: Alice\nserver:\n  port: 8080\ntags:\n  - a\n  - b\n')
doc: {
    nil -> print('nil')

    err -> print(it.msg())

    -> {
        doc = it
        name = doc.find-str('name'); ?str
        port = doc.find-i64('server.port'); ?i64
        name: {
            ok -> print(it)

            -> print('')
        }
        port: {
            ok -> print(it.to-str())

            -> print('')
        }
        print(doc.to-json())
        print(doc.to-yaml())
    }
}
```

Fonctions et méthodes courantes :

- `yaml.parse(text)` / `yaml.parse-all(text)` — analyse, retourne `?yaml` (la branche `err` porte une position `line L, column C`)
- `yaml.valid-of(text)` / `yaml.error-of(text)` — validation seule, sans construire le document
- `yaml.strip-bom(text)` — supprime le BOM UTF-8
- `doc-count()` / `doc(i)` — accès aux documents multiples
- `kind()`、`is-null()`、`is-bool()`、`is-int()`、`is-float()`、`is-nan()`、`is-str()`、`is-seq()`、`is-map()`、`len()`、`text()`、`tag()`、`anchor()`、`line-of()` — informations sur le nœud
- `str()`、`i64()`、`f64()`、`bool()`、`scalar-text()` — accès aux valeurs, retournent toujours `?valeur`
- `at(i)`、`value(i)`、`key(i)`、`key-node(i)`、`get(key)`、`find(path)` — nœuds enfants / clés
- `get-str` / `get-i64` / `get-f64` / `get-bool`、`find-str` / `find-i64` / `find-f64` / `find-bool` — raccourcis de lecture
- `to-json()`、`to-json-all()`、`to-yaml()`、`stringify()`、`source-of()` — sérialisation

Constantes de type de nœud : `YAML-KIND-NULL` / `BOOL` / `INT` / `FLOAT` / `STR` / `SEQ` / `MAP` (0–6) ; constantes de style scalaire `YAML-STYLE-PLAIN` / `SINGLE` / `DOUBLE` / `LITERAL` / `FOLDED` / `ALIAS` (0–5) et `YAML-STYLE-BLOCK` / `FLOW` (6–7).

> **Attention 1** : les vues de nœuds enfants sont retournées en `?yaml` et les fonctions d'aide à la lecture en `?valeur` ; déballez-les par un match (`child = d.at(i)`, dont la branche `ok` lie `it` au nœud enfant et la branche par défaut traite l'absence, chaque branche sur sa propre ligne). L'enfant partage le pool de nœuds du document parent ; il est conseillé d'effectuer la lecture dans le même match, ou de lier une copie d'abord, plutôt que de conserver un handle périmé à travers plusieurs mutations du pool. Pour la lecture, privilégiez toujours les fonctions de chemin comme `find-str` / `find-i64`.
>
> **Attention 2** : dans les match imbriqués, `it` est **restauré niveau par niveau**. La branche d'un match interne relie `it` à son propre sujet (le corps de la branche doit voir la valeur interne), mais dès que cette branche se termine, `it` reprend le sujet de la branche englobante ; l'écriture suivante est donc sûre :
>
> ```no
> d ?yaml = yaml.parse(src)
> d: {
>     nil -> print('nil')
>
>     err -> print(it.msg())
>
>     -> {
>         print(it.find-str('a'))        ; it externe = le document
>         v ?yaml = it.at(0)
>         v: {
>             nil -> print('none')
>             err -> print('err')
>             -> print(it.find-str('b')) ; it interne = le nœud enfant
>         }
>         print(it.find-str('c'))        ; ✅ it est de nouveau le document
>     }
> }
> ```
>
> La seule exception est le match le plus externe : une fois terminé, `it` conserve la valeur de la dernière branche (il n'est pas restauré). Ne vous fiez donc jamais à `it` hors d'un match. Pour l'utiliser à travers plusieurs niveaux, stockez-le explicitement dans une variable locale nommée (`doc = it`), ou utilisez une liaison par décomposition `ok(v) -> ...`.
>
> **Attention 3** : `.nan` est analysé en un vrai NaN (`f64()` retourne NaN et `x != x` est vrai). JSON n'a pas de NaN, donc `to-json()` produit `null` pour lui, tandis que `to-yaml()` produit `.nan`.

---
