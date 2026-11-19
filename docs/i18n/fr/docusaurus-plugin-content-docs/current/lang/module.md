---
sidebar_position: 6
---

# Préfixe d'appel inter-modules

Nolang impose une convention de namespace de modules obligatoire : lorsqu'on appelle, depuis un fichier `.no`, des fonctions ou constantes définies dans **d'autres modules** (d'autres fichiers `.no`), il faut utiliser le préfixe `ShortName.`. Cela évite les conflits de noms entre modules.

Les modules de la bibliothèque standard sont chargés automatiquement par le compilateur — **aucun import explicite n'est nécessaire** (pas besoin d'annotation `# std/...`) ; il suffit d'utiliser le préfixe `ShortName.` pour les appeler. `# std/fmt` est particulièrement superflu — `print` / `eprint` / `format` sont des intégrés du compilateur, et `no fmt` supprime automatiquement un `# std/fmt` sans alias (voir [Style de code · Nettoyage des instructions d'import](./code-style#導入語句自動整理去重--移除-stdfmt--排序)).

> **Dépendances et résolution des chemins** : la clé de dépendance d'un paquet local peut être mappée, via `workspace.noml`, vers un nom court ou une URL complète. Voir [Types de dépendances et règles de numérotation des versions](../usage#依賴類型與版本號規則).

## Définition du ShortName

Le ShortName est le dernier segment du chemin du module ; il sert de préfixe pour les appels inter-modules.

| Chemin de fichier | FullPath | ShortName | Description |
| --- | --- | --- | --- |
| `std/math.no` | `math` | `math` | Fichier de premier niveau |
| `std/fs.no` | `fs` | `fs` | Fichier de premier niveau |
| `std/net/net.no` | `net/net` | `net` | Dernier segment du chemin |
| `std/net/client.no` | `net/client` | `client` | Dernier segment du chemin |
| `std/crypto/sha256.no` | `crypto/sha256` | `sha256` | Dernier segment du chemin |
| `std/archive/gzip.no` | `archive/gzip` | `gzip` | Dernier segment du chemin |

Le ShortName est le dernier segment du FullPath une fois séparé par des `/` (par ex. `crypto/sha256` → `sha256`).

## Quand le préfixe est requis

Lors de l'appel de fonctions ou de constantes de niveau module définies dans d'autres modules, il faut utiliser le préfixe `ShortName.`.

```no
; Fonctions de niveau module
sha256.sha256(data)
sha256.sha256-hex(data)
fs.open(path, opts)
gzip.gzip-decompress(data)
math.pow(x, y)

; Constantes de module
net.NET-BUF-SIZE
math.PI
```

### Convention de nommage des fonctions

**Ne préfixez pas les noms de fonctions par le nom du module.** Les fonctions à l'intérieur d'un module doivent simplement porter des noms courts et intuitifs ; le préfixe `ShortName.` sur les appels inter-modules véhicule déjà l'information du module.

```no
; ✅ Correct : noms de fonctions concis, sans préfixe de module
; tail.no
; la fonction d'entrée utilise directement le nom du module
tail = () {
    ...
}

; les fonctions auxiliaires utilisent des noms courts
atoi = (s str) (v i64) {
    ...
}
```

```no
#{fmt-skip}
; ❌ À éviter : noms de fonctions redondamment préfixés par le nom du module
; tail-run = () { }
; tail-atoi = (s str) (v i64) { }
```

La même convention s'applique lors des imports inter-modules :

```no
; ✅ Concis et intuitif
# /src/tail.tail
# /src/mktemp.mktemp
```

```no
#{fmt-skip}
; ❌ Redondant
; # /src/tail.tail-run
; # /src/mktemp.mktemp-run
```

> **Évitez les mots-clés** : `awy` (mot-clé de groupe de coroutines), `match` (mot-clé de filtrage conditionnel), etc. ne peuvent pas servir de noms de fonctions. `run` a été retiré et peut être utilisé comme identifiant ordinaire. Il est recommandé que la fonction d'entrée porte directement le nom du module lui-même (par ex. `ping.no` → `ping`).

## Quand le préfixe n'est pas requis

Les cas suivants ne nécessitent pas de préfixe :

### 1. Fonctions globales (`with-cap` / `with-len` / `with-cap-len` / `print` / `eprint` / `format` / `exit`)

Ces 7 fonctions sont des intégrés globaux au niveau du langage ; elles s'utilisent directement, sans préfixe de module. Leurs déclarations sous forme de commentaires sont centralisées dans `std/global.no`, pour faciliter la consultation.

**Constructeurs de capacité/longueur :**

- `with-cap(cap)` — Crée une chaîne ou une tranche avec la capacité indiquée (len=0), le type étant inféré depuis le côté gauche de l'affectation
- `with-len(len)` — Crée une chaîne ou une tranche avec la longueur indiquée
- `with-cap-len(cap, len)` — Crée une chaîne ou une tranche avec la capacité et la longueur indiquées

**Sortie/formatage :**

Nolang utilise une syntaxe de **chaînes de format nommées** `{name[:spec]}`, qui référence directement les variables de la portée, sans arguments positionnels. Une validation à la compilation est prise en charge. La sortie est écrite directement via les appels système `io.out`/`io.err`, sans dépendre du `printf` de libc.

- `print(s)` / `print(s0, s1, ...)` — écrit sur la sortie standard ; les arguments multiples sont séparés par des espaces, **avec retour à la ligne automatique**
- `eprint(s)` / `eprint(s0, s1, ...)` — écrit sur la sortie d'erreur ; les arguments multiples sont séparés par des espaces, **avec retour à la ligne automatique**
- `format(s)` — retourne la chaîne formatée (remplace `sprintf`), sans retour à la ligne automatique

**Contrôle de processus :**

- `exit(code)` — termine le processus avec un code d'état (anciennement `os.exit`, promu en intégré global ; aucun préfixe `os.` nécessaire)

**Avec plusieurs arguments, chaque littéral de chaîne est un modèle — et non une chaîne de format de style C.** Dans `print('result={val}', 42, 'result={val}')`, chaque littéral de chaîne est un modèle **en soi** : son `{val}` est résolu depuis la portée au **point d'appel**, indépendamment des autres arguments du même appel. Un littéral **ne décrit ni ne consomme** jamais les arguments qui le suivent — ce n'est pas une substitution de style `printf('%d', 42)`. Les arguments non littéraux (variables, expressions) sont traités comme des arguments variadiques ordinaires, séparés par des espaces, avec un unique retour à la ligne à la fin de l'appel.

```no
val = 42
print('result={val}', 42, 'result={val}'); result=42 42 result=42
tmpl = '{val}'
print(tmpl); {val} —— une variable est du texte brut, sans substitution
print('a={val}', val); a=42 42
```

`format(s)` ainsi que le `sprintf(s)` abandonné n'acceptent toujours qu'**une seule chaîne de format** ; il n'existe pas de forme à plusieurs arguments.

> `printf` et `eprintf` ont été **retirés** : les noms restent dans la table des symboles (un appel se résout toujours et l'éditeur propose toujours la complétion),
> mais **tout appel est une erreur de compilation bloquante** (`[printf-depr]`), dont le message indique directement la migration. `sprintf` fonctionne encore mais est **abandonné**.
> Les correspondances sont les suivantes :
> - `printf(s)` → `print(s)` (retour à la ligne automatique) ou `io.out(s)` (sans retour à la ligne, sortie standard)
> - `eprintf(s)` → `eprint(s)` (retour à la ligne automatique) ou `io.err(s)` (sans retour à la ligne, sortie d'erreur)
> - `sprintf(s)` → `format(s)` (retourne la chaîne formatée)
>
> `io.out`/`io.err` sont des commandes de bas niveau dont la sortie **ne comporte pas de retour à la ligne**. Comme les appels de module doivent comporter le préfixe du module, `io.err` porte explicitement son préfixe de module et n'entre pas en conflit avec le constructeur d'option `err()` ; même en cas de collision de noms, le préfixe de module permet de les distinguer.

```no
; Constructeurs de capacité/longueur (sans préfixe)
s str = with-cap(256); ✅ str préalloué de 256 octets
v []i64 = with-cap(100); ✅ tranche préallouée de 100 éléments
v []i64 = with-cap-len(200, 100); ✅ capacité 200, longueur 100
v []i64 = with-len(100); ✅ tranche de longueur 100

; Sortie/formatage (sans préfixe)
print('hello {n}'); ✅ retour à la ligne automatique
print(a, b, c); ✅ plusieurs arguments, séparés par des espaces, retour à la ligne automatique
print('x={n}', n); ✅ le littéral est un modèle, la variable une valeur
print('result={val}', 42, 'result={val}'); ✅ chaque littéral se substitue lui-même
print(); ✅ aucun argument, seul le retour à la ligne est produit
s = format('x={x}'); ✅ retourne une chaîne formatée (chaîne de format unique)
eprint('err: {n}'); ✅ écrit sur stderr avec retour à la ligne
eprint('err:', a, b); ✅ plusieurs arguments, séparés par des espaces sur stderr
print('編號 {id:06} 金額 {money:.2f}'); ✅ prend en charge alignement, remplissage, largeur, précision

; Commandes de bas niveau (préfixe de module requis)
io.out('no-newline-here'); ✅ sortie sans retour à la ligne (remplace printf)
io.err('err-no-newline'); ✅ stderr sans retour à la ligne (remplace eprintf)

; Tous les autres appels inter-modules nécessitent un préfixe
fs.open(path, opts); ✅ avec préfixe (les intégrés aussi)
```

#### Spécificateurs de format

Dans `{name[:spec]}`, le champ `spec` prend en charge les éléments suivants (ordre fixe, tous facultatifs) :

```
[[fill]align][sign][#][0][width][.precision][type]
```

- `fill` — Caractère de remplissage (espace par défaut), doit être utilisé avec `align`
- `align` — `<` alignement à gauche, `>` à droite, `^` centré
- `sign` — `+` affiche un plus pour les positifs, `-` n'affiche que les négatifs (par défaut)
- `#` — Préfixe de base (`0x`/`0o`/`0b`)
- `0` — Préfixe les nombres de zéros jusqu'à la largeur indiquée
- `width` — Largeur minimale du champ
- `.precision` — Nombre de décimales pour les flottants / longueur maximale pour les chaînes
- `type` — `d`(entier), `x`/`X`(hexadécimal), `o`(octal), `b`(binaire), `c`(caractère), `f`(point fixe), `e`/`E`(notation scientifique), `g`/`G`(général), `s`(chaîne, par défaut), `t`(nom du type de données, déterminé à la compilation), `v`(valeur littérale : chaînes entre guillemets simples, vec appelle to-str)

```no
x i64 = 42
u u64 = 255
pi f64 = 3.14159
s str = 'hello'
print('{x:06}'); 000042
print('{x:>10}'); aligné à droite, largeur 10
print('{u:#x}'); 0xff
print('{pi:.2f}'); 3.14
print('{pi:8.3e}'); 3.142e+00
print('{s:<10}'); hello     （aligné à gauche）
print('{s:.3}'); hel（tronqué à 3 caractères）
print('{x:t}'); i64（affiche le nom du type de la variable x）
print('{s:v}'); 'hello'（chaîne entourée de guillemets simples）
```

`{{` et `}}` servent à produire les accolades littérales `{` et `}`, mais **uniquement si la même chaîne contient au moins un vrai champ `{name}`**. Un littéral composé seulement d'échappements est produit tel quel : `print('{{n}}')` affiche `{{n}}` (et non `{n}`), et `print('{{')` affiche `{{`. Pour produire une accolade isolée, utilisez simplement `print('{')` / `print('}')`.

### 2. Définitions dans le même fichier

Les fonctions, constantes et méthodes définies dans le même fichier `.no` s'utilisent directement, sans préfixe.

```no
; Dans sha256.no :
sha256(data); sha256 est défini dans ce fichier

; Constante définie dans ce fichier
HMAC-BLOCK-SIZE
```

### 3. Méthodes des types intégrés

Les méthodes des types intégrés (`str`, `i64`, `vec`, `arr`, `byte`, `char`, `bool`, etc.) ne nécessitent pas de préfixe. Les méthodes sont intégrées au type et résolues via le type du récepteur.

```no
'hello'.starts-with('he'); méthode str
n.to-str(); méthode int
v.push(42); méthode vec
a.contains(3); méthode arr
c.is-digit(); méthode char
```

### 4. Méthodes d'instance de structure

L'appel de méthodes sur une instance de structure déjà créée ne nécessite pas de préfixe de module. Les méthodes sont résolues via le type de l'instance, et le compilateur trouve automatiquement la définition `struct.method` correspondante.

```no
f = fs.open(path, opts); fs.open est une fonction de niveau module, préfixe requis
f.read(buf, n); file.read est une méthode de structure, aucun préfixe requis
f.close(); file.close est une méthode de structure, aucun préfixe requis

p = path {
    p: '/tmp'
}
p.exists(); path.exists est une méthode de structure, aucun préfixe requis
```

## Appels de méthodes vs appels de fonctions de module

La nécessité d'un préfixe pour un appel de méthode dépend du **propriétaire de la méthode** :

- **Méthodes de types intégrés** (`str.starts-with`, `i64.to-str`, etc.) — aucun préfixe requis
- **Méthodes d'instance de structure** (`f.read`, `p.exists`, etc.) — aucun préfixe requis
- **Fonctions de niveau module** (`fs.open`, `sha256.sha256`, etc.) — **préfixe requis**

Dans `fs.fil()`, `fs` est le ShortName du module et `fil` est le nom de la fonction de niveau module. Le préfixe `fs.` ne peut pas être omis, car `fs` n'est pas ici un nom de variable mais un chemin de module.

### Les deux sens de `Name.Function`

`process.cmd(...)` et `p.start(...)` s'écrivent tous deux `xxx.yyy()`, mais ont des sens complètement différents :

| Forme | `xxx` | `yyy` | Sens |
| --- | --- | --- | --- |
| `process.cmd(...)` | ShortName du module | Fonction de niveau module | `xxx` est un chemin de module, `yyy` est une fonction autonome définie dans ce module |
| `p.start(...)` | Variable d'instance | Méthode de structure | `xxx` est une variable de type `process`, `yyy` est une méthode définie par `process.start = ...` |

**Différence à la définition** :
- Les fonctions de niveau module se définissent **sans préfixe** : à l'intérieur du module, on écrit directement `cmd = (program str, ...) { }`
- Les méthodes de structure se définissent **avec le préfixe `struct.`** : `process.start = (program str, ...) { }`

**Différence à l'appel** :
- Les fonctions de niveau module s'appellent de l'extérieur via `ModuleName.function()` : `process.cmd(...)`
- Les méthodes de structure s'appellent via une instance : `p = process.new()` → `p.start(...)`

> **Attention** : même à l'intérieur d'un module, l'appel d'une fonction de niveau module du même module se fait sans préfixe (`cmd(...)`), tandis que les méthodes de structure sont invoquées via `self` implicite ou la syntaxe `.method()`.

## Références de types inter-modules

Lorsqu'on référence des types (structures, interfaces, énumérations, etc.) définis dans **d'autres modules**, le préfixe `ShortName.` est requis. Cela s'applique aux cas suivants :

### Structure implémentant une interface

Lorsqu'une structure implémente une interface définie dans un autre module, le nom de l'interface doit porter le préfixe du module :

```no
#{fmt-skip}
; ❌ Erreur : db, rows, stmt sont des interfaces définies par le module sql, le préfixe ne peut pas être omis
db-mysql db {
    fd i64
}
```

```no
; ✅ Correct : utiliser sql.db, sql.rows, sql.stmt
db-mysql sql.db {
    fd i64
}

rows-mysql sql.rows {
    fd i64
}

stmt-mysql sql.stmt {
    fd i64
}
```

### Paramètres et types de retour de fonction

Les types inter-modules dans les signatures de fonctions nécessitent également un préfixe :

```no
; ✅ Correct : le type de retour utilise sql.result
db-mysql.exec = (sql str) (r sql.result) {
    ...
}
```

### Types de champs de structure

```no
; ✅ Correct : le type du champ utilise sql.connection
conn-mysql sql.db {
    handle sql.connection
}
```

### Cas ne nécessitant pas de préfixe

- **Types définis dans le même module** : structures, interfaces et énumérations définies dans le même fichier `.no` s'utilisent directement par leur nom de type
- **Types intégrés** : `str`, `i64`, `bool`, `byte` etc. ne nécessitent pas de préfixe
- **Interfaces intégrées** : `enter`, `leave` etc., interfaces intégrées au langage, ne nécessitent pas de préfixe

```no
; Types définis dans le même fichier, aucun préfixe requis
result {
    last-id i64
    affected i64
}

; enter/leave sont des interfaces intégrées, aucun préfixe requis
; result est une structure définie dans le même fichier, aucun préfixe requis
db {
    enter
    leave
    close() (ok bool)
    exec(sql str) (r result)
}
```

## Exemple complet

```no
; Les modules de la bibliothèque standard sont chargés automatiquement, aucun import explicite requis

; ─── Aucun préfixe requis ───

; Fonctions globales (déclarées dans global.no)
s str = with-cap(256)
v []i64 = with-len(100)
print('hello {n}')
s = format('x={x}')

; Fonction du même fichier
sha256(data)

; Méthode de type intégré
'hello'.starts-with('he')
n.to-str()
v.push(42)

; Méthode d'instance de structure
f = fs.open(path, opts)
f.read(buf, n)
f.close()

; ─── Préfixe requis ───

; Fonction de niveau module
sha256.sha256(data)
sha256.sha256-hex(data)
fs.open(path, opts)
gzip.gzip-decompress(data)
math.pow(x, y)

; Constante de module
net.NET-BUF-SIZE
math.PI

; Références de types inter-modules (implémentation d'interface, types de paramètre/retour, types de champ)
db-mysql sql.db {
    fd i64
}

r sql.result = d.exec('CREATE TABLE ...')
```
