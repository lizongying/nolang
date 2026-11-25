---
sidebar_position: 3
---

# Chaînes

Les chaînes Nolang (`str`) sont une séquence d'octets allouée sur le tas, prenant en charge divers opérateurs et méthodes.

## Structure interne et API publique

Le descripteur `str` (`%str-long`) fait **16 octets** sur les plates-formes 64 bits :

```llvm
%str-long = type { i32, i32, i8* }   ; u32 len, u32 cap, data
```

| Champ | Type interne | Type public | Signification |
|---|---|---|---|
| `len` | `u32` (LLVM `i32`) | `i64` | longueur logique en octets ; `s.len-bytes()`, `len(s)`, `s._len()` |
| `cap` | `u32` (LLVM `i32`) | `i64` | capacité physique (valable uniquement pour str-long) ; `s.cap()`, `s._cap()` |
| `data` | `i8*` | `[]byte` / `*byte` | pointeur vers le tampon sur le tas |

Interne, `len` / `cap` sont réduits à `u32` afin de passer le descripteur de 24 à 16 octets (`i32` + `i32` + un pointeur aligné sur 8 octets). **L'API publique reste partout en `i64`** : à la lecture on fait `zext i32 -> i64`, à l'écriture `trunc i64 -> i32`. La bibliothèque standard et les programmes utilisateurs n'ont pas (et ne doivent pas) toucher à cette conversion ; les signatures de `s.len-bytes()`, `s.len()`, `s.cap()`, `s._len()` / `s._cap()` / `s._set-len()` / `s._set-cap()` restent en `i64`.

> ⚠️ Les compteurs internes étant des `u32`, la longueur et la capacité maximales d'une seule `str` sont de 4 Gio (`4294967295`). Un `i64` supérieur à cette valeur est tronqué à la frontière ; la manipulation courante de chaînes n'est pas affectée, mais vérifiez avant de préallouer d'immenses tampons.
>
> `txt` **n'est pas concerné par ce changement** : il reste le bloc fixe de 256 octets `{ [255 x i8] data, i8 len }`, sans champ `cap`, avec une longueur sur 1 octet.

## Opérateurs sur les chaînes

### Concaténation (`-`)

Utilisez l'opérateur `-` pour concaténer des chaînes :

```no
; Concaténation de littéraux
s = 'Hello' - ' ' - 'World'

; Concaténation avec une variable
greeting = 'Hello, ' - name
```

### Répétition (`*`)

Utilisez l'opérateur `*` pour répéter une chaîne :

```no
s = 'Hello' * 3
```

### Comparaison (`==` / `!=` / `str.compare`)

`str` n'implémente **que la comparaison d'égalité** : `==` / `!=` passent par le `@str_eq` du runtime, et les chaînes de plusieurs caractères sont parfaitement valides.

Les opérateurs de relation (<code>&lt;</code> `>` <code>&lt;=</code> `>=`) **n'ont pas de sémantique lexicographique pour les chaînes** ; les utiliser sur une chaîne est une erreur de compilation :

- **Autorisé** : un littéral de chaîne d'**un seul caractère** `'a'`, implicitement traité comme un `char` (son point de code) ; comparable à un autre littéral d'un seul caractère ou à un `char` (`"a"`).
- **Erreur** : les chaînes de plusieurs caractères — `'ab'`, une variable `str`, ou un appel qui retourne `str` (ces cas **renvoyaient silencieusement false** par le passé).

Pour un ordre lexicographique, utilisez `str.compare(b)` :

```no
s = 'abc'
b = 'abd'
c = s.compare(b); -1（s < b）/ 0（égal）/ 1（s > b）

'a' <= 'z'; true — chaîne d'un seul caractère implicitement un char
ch >= 'a'; ch est un char, OK
s < b; erreur de compilation : chaîne de plusieurs caractères
s.compare(b) < 0; la forme correcte
```

## Indexation et découpage

**Indexation** : le type inféré de `s[i]` est **`?char`** (un point de code Unicode), et non `byte` — hors limites, il retourne `err` (`code.index-out`), et dans les limites `ok(point de code)` (le même jeu de règles unifiées que `+ - * / %` sur les entiers). Pour une chaîne contenant des caractères multioctets, `s[i]` décode l'UTF-8 depuis le début de la chaîne jusqu'à compter le i-ème point de code (voir les notes de performance ci-dessous).

```no
#{fmt-skip}
s = 'Hello World'
c = s[0]: { ok -> it  -> _ }   ; point de code de 'H'（?char doit être déballé explicitement, la branche vide prend '\0'）
```

> Il n'existe que trois façons de consommer un `?char` (**`#{index-out=DEF}` a été abandonné et supprimé en D11** ; les commentaires résiduels sont une erreur de compilation ; il n'existe pas non plus de raccourci « retirer explicitement le plain du plain », `c char = s[i]` doit également être consommé explicitement) :
> **déballage par match** (`x = s[i]: { ok -> it, -> _ }`, la branche vide prenant la valeur zéro de `char`, `'\0'`),
> **écriture par place-match** (`s[i]: { ok -> it = x, -> _ }`), ou **raccourci `?=` qui propage l'erreur vers le haut** (la fonction a un paramètre de résultat `?T`, ou l'instruction se trouve dans le corps d'un match de bloc). Voir les détails et exemples dans〈syntax · Indexation sûre〉.

**Découpage** : `s[a..b]` retourne une **vue** de la chaîne sous-jacente, dont le type reste **`str`** (mémoire partagée, sans copie). Les indices de découpage sont des **positions de points de code**, cohérentes avec `s[i]` :

```no
sub = s[6..]; 'World'
sub = s[6..11]; 'World'
sub = s[0..5); 'Hello'（borne gauche incluse, droite exclue）
```

> Les indices de découpage sont des **positions de points de code**, et non des décalages en octets. Si vous avez besoin d'un découpage par décalage d'octets (par exemple pour l'associer à un parcours `s.byte(i)`), utilisez `s.slice-bytes(start, end)`.

```no
; Longueur
n = s.len(); nombre de points de code（nombre de caractères Unicode）
n = s.count(); idem（ancien nom de méthode, toujours utilisable）
n = s.len-bytes(); longueur en octets（nombre d'octets UTF-8）
; Attention : s.len nu（champ de structure）n'est plus pris en charge — lecture et écriture provoquent une erreur ; utilisez s.len() / s.len-bytes()
```

### Conversion implicite (char → str)

Un `char` peut être **converti implicitement** en `str` : le point de code est encodé en UTF-8 dans une nouvelle chaîne. Toutes les écritures suivantes sont donc valides, **sans qu'il soit besoin** d'appeler manuellement `char.to-str()` :

```no
s = 'héllo'; 'h' 'é' 'l' 'l' 'o'（é est multioctet）

; 1) le char de s[i] assigné directement à une variable str → implicitement une chaîne d'un seul caractère
a str = s[0]; 'h'

; 2) le résultat du découpage est déjà de type str, affectation directe
b str = s[0..1]; 'hé'（indices 0..1, deux bornes incluses）

; 3) un char dans une concaténation de chaînes (-) est implicitement converti en str
msg = 'first: ' - s[0]; 'first: h'

; 4) un char comparé à un str est implicitement converti en str
ok = s[0] == 'h'; true
```

> **`'...'` est un littéral `str`, `"..."` est un littéral `char`** — même si `'x'` ne contient qu'un seul point de code, c'est tout de même un `str`. Ainsi, quand les **deux** côtés de `+` / `-` sont des littéraux (`'...'` avec `"..."`), la conversion implicite ci-dessus s'applique et le résultat est une **concaténation**, et non une opération arithmétique sur entiers :
>
> ```no
> 'x' - "a"                ; "xa"（concaténation ; et non 120-97 = 23）
> "a" + 'x'                ; "ax"
> 'xy' + "a"               ; "xya"
> ```
>
> Dès qu'**un seul** côté n'est pas un littéral (variable char, `s[i]`, entier), un `'...'` d'un seul caractère participe à l'arithmétique entière en tant qu'**octet** — c'est volontaire, et c'est ce qui permet à l'idiome d'extraction de la valeur d'un chiffre de fonctionner :
>
> ```no
> c char = "5"
> n = c - '0'              ; 5（soustraction d'octets, et non "50"）
> m = 'A' + 1              ; 66
> "z" - "a"                ; 25（les deux côtés sont des littéraux char, soustraction de chars）
> 'x' - c                  ; 23（c est une variable → opération sur octets）
> ```
>
> Réciproquement, la dégradation en octets ne se produit **que lorsque l'autre côté est un entier ou un char**. Si l'autre côté est un `bool`, un `f64`, une structure ou toute valeur **sans signification d'octet**, le `'...'` reste une chaîne et toute l'expression est une **concaténation**, l'autre côté étant rendu via `to-str` :
>
> ```no
> b = true
> 'x' - b                  ; "xtrue"（et non 120 - 1 = 119）
> 'x' - 1.5                ; "x1.5"
> 'x' - 'y'                ; "xy"（les deux côtés sont des littéraux de chaîne）
> ```
>
> Ainsi `'x' - b` peut être affecté à un `str` : `s str = 'x' - b` donne `"xtrue"` ; tandis que `s str = 'x' - 1` reste refusé, car c'est une opération sur octets (le résultat est un nombre).

> La conversion implicite n'encode qu'un **seul point de code** en `str`. Si vous voulez la valeur numérique, utilisez le `char` lui-même (par ex. `print(c)` affiche la valeur décimale de ce point de code) ; si vous voulez une conversion explicite, `c.to-str()` reste disponible (équivalent à la conversion implicite).
>
> Un `char` représente un unique point de code scalaire Unicode ; son type de stockage sous-jacent est **`i32`** (plage valide `0 ..= 0x10FFFF`).
>
> Un `char` **participe normalement aux opérations arithmétiques sur entiers** (par ex. `z = a + 25`, `u = ch - 32`), calculées sur la valeur du point de code sur une largeur de i32 ; contrairement à la famille des entiers, il ne retourne pas par défaut `option<int>`, et ne déclenche donc jamais d'erreur de compilation « débordement entier non traité ».
>
> **Rendu** : un `char` converti en texte affiche toujours la **valeur décimale de son point de code** (`"A"` → `65`, `"é"` → `233`), et `print(c)` et l'interpolation `print('{c}')` **doivent donner le même résultat**. L'arithmétique sur char est effectuée en **i32 signé**, donc `"x" - "y"` vaut `-1`, et les deux chemins affichent `-1` (jamais `4294967295` de l'un des côtés). Tout point de code valide se situe dans `0 ..= 0x10FFFF` (bit 31 à 0), où les extensions signée et non signée coïncident ; cette règle ne concerne donc que les valeurs négatives issues d'une arithmétique sur char hors limites. Si vous voulez le **caractère lui-même** plutôt que le nombre, utilisez `c.to-str()` (`"A".to-str()` → `"A"`) : c'est une autre opération.

## Optimisation ASCII et conseils de performance

L'indexation `s[i]` sur `str`/`txt` récupère le **i-ème point de code (caractère Unicode)**, et non le i-ème octet. Pour une chaîne pouvant contenir des caractères UTF-8 multioctets (chinois, emoji, `é`, etc.), chaque `s[i]` doit parcourir l'UTF-8 depuis le début de la chaîne jusqu'à atteindre le i-ème point de code — la complexité temporelle est donc **O(n)**.

Lorsque le compilateur peut **prouver** qu'une chaîne est entièrement composée de caractères ASCII (points de code 0–127), chaque octet correspond exactement à un caractère : `s[i]` adresse directement l'octet sous-jacent et retombe à **O(1)**. Le compilateur prouve l'ASCII pur grâce aux règles suivantes :

- Annotation explicite : ajouter une annotation `#{ascii}` à la déclaration ou à l'affectation ;
- Affectation d'un littéral de chaîne ASCII (par ex. `'hello'`) ;
- Propagation par identifiant : `b = a`, si `a` est déjà prouvé ASCII, alors `b` l'est aussi ;
- Concaténation (`-`) dont les deux côtés sont prouvés ASCII.

```no
; Littéral ASCII → le compilateur prouve l'ASCII pur, s[i] est en O(1)
name = 'hello'
c = name[1]; O(1), récupère l'octet 'e'

; Contient des caractères multioctets → ASCII non prouvé, s[i] est un parcours de points de code en O(n)
s = 'héllo'
c = s[1]; O(n), récupère le 1er point de code 'é'（et non un octet）
```

### Avertissement de performance (LSP / `no vet`)

Lorsque vous **parcourez manuellement les points de code par index dans une boucle** sur une `str`/`txt` **non prouvée ASCII** — c'est-à-dire le motif `for i <- [0..s.count()): { s[i] }` qui récupère les caractères un par un par index de point de code — `no vet` et le LSP de l'éditeur émettent un diagnostic statique de niveau **WARNING** (la syntaxe n'est pas interdite, c'est seulement un avertissement). Chaque `s[i]` doit parcourir l'UTF-8 depuis le début de la chaîne pour atteindre le i-ème point de code : la boucle complète retombe à **O(n²)**.

Un `s[i]` isolé (par ex. `s[5]`, une seule recherche en O(n)) **ne déclenche pas** d'avertissement ; un parcours direct `for c <- s` des caractères **n'en déclenche pas non plus** (voir ci-dessous).

Préférez des méthodes plus rapides :

- Si vous devez parcourir chaque caractère, utilisez directement `for c <- s` — le parcours avant en O(n) à passe unique privilégié, qui évite le O(n²) des `s[i]` répétés. Le compilateur avance par **point de code UTF-8 (caractère Unicode)**, stockant à chaque étape la valeur du point de code décodé dans la variable de boucle `c` ; sémantiquement, cela équivaut à `for c <- s.to-chars()` :
  ```no
  c <- s: {
      ; c est le point de code courant, de type char（déjà décodé en UTF-8, et non un octet）
  }
  ```
- Si vous n'avez besoin que d'un accès par octets (encodage/décodage, hachage, memcmp), utilisez la trappe d'évasion `s.byte(i)` : elle adresse toujours directement le i-ème octet, est **toujours en O(1)**, et n'entre pas dans le corps d'une fonction std :
  ```no
  b = s.byte(0); récupère le 0e octet, de type ?byte, O(1)
  out = 0
  b: {
      ok -> {
          out = it; dans les limites : ok(byte)
      }

      -> {
          out = 0; hors limites : nil（la branche vide synthétise la valeur zéro）
      }
  }
  ```
  > `s.byte(i)` retourne un **`?byte`** (depuis le 2026-10-21) : auparavant, la lecture d'octet brute ne faisait aucune vérification de limites et un index hors bornes était un comportement indéfini. Désormais, hors bornes, elle retourne `nil` — attention, cela diffère de l'indexation de conteneur `arr[i]` / `s[i]` qui retourne `err` (`code.index-out`) **différemment** : `s.byte(i)` est une trappe d'évasion au niveau octet, et « hors bornes » signifie seulement « il n'y a pas cet octet », ce n'est pas une erreur.
  > (Voir design/option-unified.md §3.1, point 5.) Ainsi `b i64 = s.byte(0)` provoque
  > `15w45dqk` — une annotation plain explicite associée à un appel option est un déballage silencieux, une erreur de compilation bloquante ;
  > utilisez `?=`, un match en deux temps (`b = s.byte(0): { ok -> it, -> _ }`) ou `_ =` pour consommer la valeur.
  > `txt.byte(i)` a la même sémantique (`txt` stocke sa longueur en octets dans le champ 1).

> La bibliothèque standard (src/std) a entièrement migré ses accès par octets vers `s.byte(i)` : cela couvre l'encodage/décodage UTF-8 (`decode-cp`), la comparaison (`compare`/`starts-with`/`ends-with`), le hachage, la copie de découpage (`slice`/`repeat`/`trim`/`copy`), `to-bytes`/`to-upper`/`to-lower`/`reverse`/`replace-char`/`count`/`index`, etc. dans `str.no`/`txt.no`. `no vet` n'a donc plus besoin d'ignorer les fichiers de la bibliothèque standard ; l'accès aux éléments `x[i]` des types `[]byte`/tranche/tableau est de toute façon indexé par élément (octet), sa sémantique est inchangée et n'entraîne pas d'avertissement.

| Écriture | Sémantique | Complexité | Remarque |
|---|---|---|---|
| `s[i]`（ASCII prouvé） | le i-ème octet = le i-ème caractère | O(1) | prouvé automatiquement, aucune modification |
| `s[i]`（ASCII non prouvé, dans une boucle） | le i-ème point de code | O(n²) anti-motif | déclenche un avertissement LSP |
| `s[i]`（ASCII non prouvé, hors boucle） | le i-ème point de code | O(n) une seule fois | aucun avertissement |
| `s.byte(i)` | le i-ème octet（`?byte`, `nil` hors bornes） | O(1) | trappe d'évasion d'accès octet |
| `for c <- s` | point de code par point de code | O(n) passe unique | parcours privilégié, aucun avertissement |

## Méthodes de chaîne

```no
#{fmt-skip}
; Comparaison
ok = a.eq(b, n)               ; Comparaison d'égalité（méthode）
c = s.compare(b)              ; Comparaison lexicographique

; Recherche（index/rindex retournent une position de POINT DE CODE, cohérente avec s[i]）
pos = s.index(sub)             ; Premier index de point de code de sub, -1 si non trouvé
pos = s.index-from(sub, k)     ; Recherche à partir du k-ième point de code
pos = s.rindex(sub)            ; Dernier index de point de code de sub
pos = s.rindex-from(sub, k)    ; Recherche inverse à partir du k-ième point de code
pos = s.last-index(sub)        ; Alias de rindex
ok  = s.contains(sub)          ; Contient la sous-chaîne
ok  = s.starts-with(sub)       ; Test de préfixe
ok  = s.ends-with(sub)         ; Test de suffixe
ok  = s.empty()                ; Est vide

; Attention : la famille index retourne une POSITION DE POINT DE CODE, donc s[s.index(sub)] donne le premier
;       point de code de sub, conformément à la sémantique d'indexation par point de code de s[i]. Par ex. '日本語'.index('語') == 2,
;       et s[s.index('語')] vaut le point de code de '語'.
;       Pour un découpage par POSITION DE POINT DE CODE, s.slice(start, end) a une sémantique de points de code（cohérente avec s[i]） ;
;       pour un découpage par DÉCALAGE D'OCTETS（par ex. avec un parcours s.byte）, utilisez s.slice-bytes(start, end).
;       str fournit en interne un chercheur au niveau octet str.find-byte-from（réutilisé par split, etc.）.
;       Attention : slice/at du type txt ont encore une sémantique d'octets（txt est un tampon d'octets brut）, divergence avec str.

; Conversion
out = s.to-upper()             ; Convertir en majuscules
out = s.to-lower()             ; Convertir en minuscules
out = s.trim()                 ; Supprimer les espaces en début/fin
out = s.trim-char(c)           ; Supprimer un caractère donné
out = s.repeat(n)              ; Répéter
out = s.reverse()              ; Inverser
out = s.slice(start, end)      ; Découper（indices en positions de points de code, cohérent avec s[i]）
out = s.slice-bytes(b, e)      ; Découper（indices en décalages d'octets, usage interne）
val = s.replace-char(old, new) ; Remplacer un caractère

; Conversion vers d'autres types
b = s.to-bytes()               ; Convertir en []byte
v = s.to-i64()                 ; Convertir en ?i64
v = s.to-f64()                 ; Convertir en ?f64
v = s.to-bool()                ; Convertir en ?bool

; Fractionnement et jointure
parts = s.split(sep)           ; Fractionner（retourne []str）
out = ss.join(sep)             ; Joindre un []str

; Copie et remplissage
dst = s.copy()                 ; Copie de chaîne
s.fill(val byte)               ; Remplir avec une valeur byte
```

## Conversion chaîne et nombre

```no
; Nombre vers chaîne（méthode）
s = i64.to-str(); i64 vers chaîne
s = f64.to-str(); f64 vers chaîne
s = bool.to-str(); bool vers "true"/"false"
s = byte.to-str(); byte vers chaîne
s = char.to-str(); char vers chaîne

; Chaîne vers nombre（retourne option）
v = s.to-i64(); retourne ?i64
v = s.to-i32(); retourne ?i32
v = s.to-u64(); retourne ?u64
v = s.to-f64(); retourne ?f64
```

## Écriture par index interdite (`str` 2026-11-11 / `txt` 2026-11-12)

> **`s[i] = v` est l'erreur de compilation `idxwrite`** — aucune exception pour les bases `with-cap` / `with-len` / `with-cap-len`. **Le `t[i] = c` de `txt` est également interdit** (raison ci-dessous).
>
> Raison pour `str` : c'était la seule source d'« écritures au-delà de la fin d'un tampon alloué » — un tampon de len=0 croissait par écritures directes, et écrire à `i >= cap` sortait de la zone allouée. Le garde-fou de croissance `ensureStrLongBuffer` du codegen existait précisément à cette fin ; après l'interdiction, ce garde-fou n'est plus atteignable depuis le code utilisateur et n'est conservé qu'à titre défensif.
>
> La raison de `txt` est différente mais tout aussi rédhibitoire : `t[i] = c` **réencode un point de code** et le réécrit, si bien que la LONGUEUR change comme effet de bord (ajout lorsque `i` égale le nombre de points de code, et perte silencieuse de ce qui ne tient pas dans 255 octets). `txt` n'a ni tas ni croissance, mais « une écriture par index a secrètement changé la longueur » est le même danger.

Quatre écritures restent légales :

```no
; ① CONSTRUCTION octet par octet : réception dans un tampon []byte, conversion globale en str à la fin
b []byte = with-cap-len(n, n)
b[0] = 48
b[i] = src.byte(i): {
    ok -> it

    -> 0
}
s = b.slice(0, k).to-str(); ou b.to-str()（tampon entier）

; ② ÉCRASURE sur place du contenu existant : str.fill remplit les len octets avec la même valeur, longueur inchangée
s = 'hello'
s.fill(72); 'HHHHH'

; ③ La seule écriture par index restante est le place-match (conditionné par une LECTURE : si i >= len, l'opération est silencieusement ignorée, sans croissance ni dépassement)
s[i]: {
    ok -> it = v
    -> _
}

; ④ Pour changer la longueur : la famille push (un appel explicite et auditable)
s.push("中");      ajout d'un point de code（UTF-8）
s.push-str('de');  ajout d'octets（croissance sur place）
```

> ⚠️ **Le place-match est conditionné par les POINTS DE CODE, et non par les octets.** Son conditionnement vient de la **lecture** `s[i]` — or la lecture par index sur `str` compte les points de code. Le cas ③ ne convient donc qu'aux traitements ASCII « un point de code par emplacement » (tirets d'un uuid, chiffres d'une heure, tables hexadécimales). Pour assembler du contenu **octet par octet** (tout octet `>= 0x80`, ou toute chaîne source pouvant être non ASCII), vous devez utiliser le `[]byte` du cas ① :
> en pratique, avec `f = with-len(8)`, écrire 224..231 via place-match ne dépose que les 4 premières cases,
> les autres étant silencieusement ignorées par le conditionnement — c'est exactement ainsi que `path.split` a tronqué des noms de fichiers non ASCII.

> **Troncature de longueur** : `s.len = n` nu n'est plus pris en charge ; utilisez le découpage — `s.slice(0, n)` est un découpage par **points de code**,
> `s.slice-bytes(0, n)` est un découpage par octets. (`b.slice(0, k)` sur un tampon `[]byte` est naturellement un découpage par octets.)
>
> **Croissance d'un `str` construit avec `with-cap(n)`** : `s str = with-cap(256)` est un tampon de len=0 qui possède déjà une capacité ; le premier `push` y écrit directement sans déclencher de réallocation (`push-str` ne réalloue que lorsque `len + longueur ajoutée` dépasse `cap`, et croît alors par doublage). L'écriture octet par octet `buf[i] = b` reste interdite pour `str`.

### Croissance : la famille `push` (2026-11-12)

Après l'interdiction des écritures directes, **la croissance doit passer par un appel explicite unique** :

| Méthode | Sémantique |
|---|---|
| `s.push(c char)` | Ajoute un point de code, encodé en UTF-8 (délègue à `push-str`) |
| `s.push-str(t str)` | Ajoute une chaîne, copie **par octets**, croissance sur place |

```no
s = 'ab'
s.push("中");      'ab中', len-bytes 5, len 3
s.push-str('de');  'ab中de', len-bytes 7

t = 'zz'
t.push-str(t);     'zzzz'（ajout à soi-même sûr : la source est d'abord retenue, puis copiée par memmove）

u = ''
u.push-str('');    ''（une source vide ne change rien et n'alloue rien）
```

> `push-str` est un builtin du compilateur (`#{buildin}`, abaissé en MIR en « croissance sur place + memmove + mise à jour de la longueur »),
> et non une boucle std : un ajout coûte O(longueur ajoutée), et des ajouts répétés sont amortis en O(1) (doublage).
> `push` passe par l'encodage UTF-8, donc l'incrément de `len-bytes` est égal à la longueur encodée du point de code, tandis que `len` augmente toujours de 1.

> **Les écritures sur `txt` sont elles aussi interdites (2026-11-12)** : `t[i] = c` **réencode un point de code** et le réécrit, si bien que la longueur change comme effet de bord — ajout lorsque `i` égale le nombre de points de code, et perte silencieuse de ce qui dépasse 255 octets.
> « Une écriture par index a secrètement changé la longueur » est précisément ce que l'interdiction vise à éliminer ; `txt` est traité comme `str`. Le point d'entrée de croissance de txt est
> `t.push(c)` / `t.push-str(s)` / `t.push-txt(u)` ; une modification en place passe par le place-match.
> La lecture `t[i]` retourne toujours le point de code `char` : cela n'a pas changé.
>
> Les écrivains bruts au niveau octet `str._set-byte` / `txt._set-byte` sont **privés à std** depuis le 2026-11-11
> (seuls `str.fill` / `txt.push` etc. à l'intérieur de std les appellent) ; le code utilisateur ne peut pas y accéder.
> Pour assembler un `txt` octet par octet, rassemblez d'abord un `[]byte`, puis copiez-le en une seule fois avec `txt.from-bytes(b)`
> (au-delà de 255 octets, troncature automatique, la longueur étant fixée au nombre réel d'octets).

## Préallocation (syntaxe intégrée)

Les trois syntaxes intégrées `with-cap`, `with-len` et `with-cap-len` permettent de préallouer de la mémoire sur le tas, afin d'éviter les réallocations répétées lors des `push` ultérieurs :

```no
; with-cap(cap) : alloue une capacité cap, len=0（il faut faire push avant de pouvoir indexer）
s = with-cap(256); str, len=0, cap=256

; with-len(len) : alloue une capacité len, len=cap（lecture par index directe autorisée）
s = with-len(128); str, len=128, cap=128

; with-cap-len(cap, len) : spécifie à la fois capacité et longueur, idéal pour « réserver de la place pour la croissance »
s = with-cap-len(512, 64); str, len=64, cap=512
```

| Syntaxe intégrée | Paramètres | len | cap | Cas d'usage |
|---|---|---|---|---|
| `with-cap(cap)` | 1 | 0 | cap | Seule la limite supérieure est connue ; la longueur croît par push |
| `with-len(len)` | 1 | len | len | Longueur fixe, lecture par index directe |
| `with-cap-len(cap, len)` | 2 | len | cap | Longueur initiale connue + réserve pour la croissance |

> Les paramètres et les valeurs retournées par `s.cap()` / `s.len-bytes()` sont tous des **`i64`** (l'API publique ne change pas), mais le descripteur stocke len / cap en interne sous forme de **`u32`**. L'appelant voit toujours des valeurs 64 bits, la conversion étant effectuée par le compilateur à la frontière du descripteur ;
> voir〈Structure interne et API publique〉. Les écritures de champ telles que `s.len = n` / `s.cap = n` passent également par cette frontière,
> et n'écrasent donc pas les champs d'en-tête voisins.

> ⚠️ **Un tampon `str` n'autorise jamais d'écriture directe par index** (depuis le 2026-11-11, les bases `with-cap` ne sont plus exemptées non plus).
> L'exemption with-cap de `idxwrite` ne sert désormais plus qu'aux **`[]byte` / `[]T` / tableaux de taille fixe** : leurs tampons de len=0 croissent encore leur longueur via `buf[i] = b`, sans écriture équivalente de remplacement. Pour `str`, passez par un tampon `[]byte` +
> `to-str()` (voir la section〈Écriture par index interdite〉).
> Pour `with-len(n)` / `with-cap-len(cap, len)`, la longueur est déjà en place : **les écritures doivent passer par le place-match `buf[i]: { ok -> it = v / -> _ }` ou `buf[i] ?= v`** — comme pour tout autre conteneur, une écriture directe est l'erreur de compilation `idxwrite`. (`with-len` a cessé d'être exempté le 2026-11-10.)

> **Inférence de type** : le type de résultat des trois syntaxes intégrées est inféré depuis le côté gauche de l'affectation ; elles peuvent servir pour `str` ou pour des tranches `[]T`.
