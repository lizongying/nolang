---
sidebar_position: 3.2
---

## Bibliothèque centrale

### fmt — Sortie formatée

Nolang utilise des **chaînes de format nommées** `{name[:spec]}`, qui référencent directement les variables de la portée, sans arguments positionnels. La sortie passe par les appels système `io.out`/`io.err`, sans dépendre du `printf` de libc.

```no
print('x={x}'); format nommé, retour à la ligne automatique (stdout)
print('result={val}', 42, 'result={val}'); plusieurs arguments : séparés par des espaces ; chaque littéral est son propre modèle
eprint('err {x}'); format nommé, retour à la ligne automatique (stderr)
print('編號 {id:06} 金額 {money:.2f}'); prend en charge alignement/remplissage/largeur/précision
s = format('x={x}'); retourne la chaîne formatée (remplace sprintf)
io.out('no-newline-here'); commande de bas niveau, sortie sans retour à la ligne (stdout)
io.err('err-no-newline'); commande de bas niveau, sortie sans retour à la ligne (stderr)
; printf/eprintf ont été retirés (les appeler est une erreur [printf-depr]) : printf→print/io.out, eprintf→eprint/io.err
; sprintf fonctionne encore mais est abandonné : sprintf→format
; io.err porte explicitement son préfixe de module, il n'entre pas en conflit avec le constructeur d'option err()
```

### math — Fonctions mathématiques (API sous forme de fonctions)

Toutes les API numériques de std en « forme de fonction » sont regroupées dans le module math (std/math.no) ; les API en « forme de méthode » (`*.to-str`, f64/f32 sqrt/sin/exp…, sqrt/is-prime des entiers) se trouvent dans le module number (voir la section number ci-dessous). Types unions et méthodes membres : num → std/num.no, int → std/int.no, float → std/float.no.

**Constantes :** `math.PI`, `math.E`, `math.LN10` (ln 10, compagnon de l'implémentation f64-to-str)

**Comparaison (fonctions génériques variadiques sur num) :** `math.max`, `math.min` (`a ..num`, entiers et flottants empruntent le même chemin de monomorphisation)

**Puissance/racine (fonctions) :** `math.pow` (puissance f64 a^b, implémentée en .no pur : `exp(y*ln|x|)` + un traitement explicite du signe pour les bases négatives — exposant entier impair conserve le signe, pair le supprime, exposant non entier retourne NaN, **sans dépendance à libm**), `math.hypot` (sqrt(x*x+y*y))

**Autres fonctions :** `math.atan2`, `math.fmod` (reste de division flottante), `math.clamp` (bornage saturé i64)

**Mathématiques entières (fonctions génériques sur int) :** `math.even`, `math.odd`, `math.gcd`, `math.lcm`, `math.div` (quotient), `math.mod` (modulo)

**Conversions de type (les anciennes formes fonction `math.i64-to-f64` / `math.f64-to-i64` / `math.f32-to-f64` / `math.f64-to-f32` ont été retirées, utilisez les formes méthode) :** `v.to-f64()`、`v.to-i64()`、`f32.to-f64()` / `f64.to-f32()` (module number ; toutes les largeurs d'entiers disposent de `.to-f64()`, y compris i128 / u128)

**Conversion nombre en chaîne, couche basse :** ne reste que `math.char-to-str` (abandonné, préférez `c.to-str()`) ; `math.i64-to-str` / `math.u64-to-str` / `math.f64-to-str` ont été retirés, utilisez la forme méthode `v.to-str()`

**Opérations sur bits (fonctions) :** `math.swap`, `math.arr-zero`, `math.rotate-left`, `math.rotate-right`, `math.store-le-u32`

> Puissance entière : `math.pow-int` (exponentiation rapide générique sur int, répartie dans un module distinct de `math.pow` pour f64) ; `number.pow` conserve une signature i64 explicite pour satisfaire les assertions historiques.

### number — Opérations numériques (API sous forme de méthodes)

Les API numériques en « forme de méthode » sont regroupées dans le module number (std/number.no) ; les API sous forme de fonctions sont dans la section math ci-dessus. Les types unions int / float / num sont décrits dans leurs sections respectives.

**Constantes de plage :** i8.MIN / MAX … i128.MIN / MAX、u8.MIN / MAX … u128.MAX ; `number.INF`

**Puissance entière (fonction) :** `r = number.pow(base, n)` (base^n, n ≥ 0, exponentiation rapide O(log n), signature i64, débordement par retournement ; la version générique sur int est `math.pow-int` ci-dessus)

**Méthodes sur entiers :** `n.sqrt()` (racine carrée entière floor(√n), retourne 0 pour les négatifs), `n.is-prime()` (test de primalité) — disponibles pour les largeurs i8 / i16 / i32 / i64 / u8 / u16 / u32 / u64, et désormais aussi pour i128 / u128 (l'arithmétique 128 bits emprunte la voie LLVM i128 ; nécessite des variables locales typées explicitement comme `n i128 = .` et un placement correct de l'annotation `#{overflow=wrap}` selon la signedness)

**to-str par type concret (méthodes) :** `v.to-str()` — largeurs i8 / i16 / i32 / i64 / u8 / u16 / u32 / u64 / byte / f32 / f64 ; i128 / u128 sont également pris en charge (via les natifs `@str_from_i128` / `@str_from_u128`)

**Méthodes flottantes (f64 et f32 de même nom) :** trigonométriques `x.sin()`, `x.cos()`, `x.tan()`, `x.asin()`, `x.acos()`, `x.atan()` ; hyperboliques `x.sinh()`, `x.cosh()`, `x.tanh()` ; arrondi `x.ceil()`, `x.floor()`, `x.round()`, `x.trunc()` ; exponentiel/logarithme `x.exp()`, `x.log()`, `x.log10()`, `x.log2()` ; puissance/racine `x.sqrt()`, `x.cbrt()` ; degrés/radians `x.degrees()`, `x.radians()`. f32 calcule en double précision puis réduit vers f32.

### char — Opérations sur les caractères

char est fondamentalement un i32 (point de code Unicode) ; toutes les opérations sont fournies sous forme de méthodes :

```no
c char = 'A'
c.is-digit(); est-ce un chiffre (0-9)（méthode）
c.is-letter(); est-ce une lettre (a-z, A-Z)（méthode）
c.is-alpha(); alias de is-letter（méthode）
c.is-alnum(); est-ce une lettre ou un chiffre（méthode）
c.is-space(); est-ce un espace（méthode）
c.is-upper(); est-ce une majuscule（méthode）
c.is-lower(); est-ce une minuscule（méthode）
c.to-upper(); convertir en majuscule (ASCII)（méthode）
c.to-lower(); convertir en minuscule (ASCII)（méthode）
c.to-bytes(); Unicode → octets UTF-8（méthode）
c.to-str(); Unicode → chaîne (UTF-8, méthode）
```

### str — Opérations sur les chaînes

Le descripteur `str` est `{ u32 len, u32 cap, i8* data }` (16 octets sur 64 bits), mais
`s.len()` / `s.len-bytes()` / `s.cap()` ainsi que les paramètres et valeurs de retour de `with-cap` / `with-len` / `with-cap-len`
**restent tous en `i64`** : la conversion u32 ↔ i64 est effectuée par le compilateur à la frontière du descripteur.
Voir [Chaînes : structure interne et API publique](../lang/str.md). `txt` n'est pas concerné par ce changement.

Le descripteur de la tranche `[]t` est isomorphe : `%vec = { u32 len, u32 cap, i64 data }` (16 octets, `data` est un
pointeur encodé par `ptrtoint`) ; `v.len()` reste lui aussi en `i64`. `[n]t` n'a pas de descripteur à l'exécution (les éléments sont
en ligne dans `[n x t]`, et `.len` est une constante de compilation).

```no
#{fmt-skip}
ok = a.eq(b, n)               ; comparaison d'égalité（méthode）
dst = s.copy()                ; copie de chaîne（méthode）
s.fill(val byte)              ; remplir avec une valeur byte（méthode）
pos = s.index(sub)            ; position d'une sous-chaîne
ok = s.contains(sub)          ; contient la sous-chaîne
ok = s.starts-with(sub)       ; test de préfixe
ok = s.ends-with(sub)         ; test de suffixe
s.to-upper()                  ; convertir en majuscules
s.to-lower()                  ; convertir en minuscules
out = s.trim()                ; supprimer les espaces en début/fin
out = s.repeat(n)             ; répéter
out = s.slice(start, end)     ; découper (indices en positions de points de code)
b = s.to-bytes()              ; convertir en []byte
s = b.to-str()                ; []byte vers str（méthode）
v = s.to-i64()                ; chaîne vers i64（retourne ?i64）
v = s.to-i8()                 ; chaîne vers i8（retourne ?i8）
v = s.to-i16()                ; chaîne vers i16（retourne ?i16）
v = s.to-i32()                ; chaîne vers i32（retourne ?i32）
v = s.to-u8()                 ; chaîne vers u8（retourne ?u8）
v = s.to-u16()                ; chaîne vers u16（retourne ?u16）
v = s.to-u32()                ; chaîne vers u32（retourne ?u32）
v = s.to-u64()                ; chaîne vers u64（retourne ?u64）
v = s.to-byte()               ; chaîne vers byte（retourne ?byte）
v = s.to-f64()                ; chaîne vers f64（retourne ?f64）
v = s.to-bool()               ; chaîne "true"/"false" vers bool（retourne ?bool）
s = v.to-str()                ; i64 vers chaîne（méthode）
out = s.reverse()             ; inverser
c = s.compare(b)              ; comparaison lexicographique
n = s.count()                 ; nombre total de points de code
val = s.replace-char(old, new) ; remplacer un caractère (retourne la chaîne résultante)
out = s.trim-char(c)          ; supprimer un caractère donné
ok = s.empty()                ; est vide
s.clear()                     ; vider (len=0, modification sur place)
s = with-cap(cap)            ; syntaxe intégrée : crée une nouvelle chaîne de capacité donnée (len=0)
s = with-len(len)            ; syntaxe intégrée : crée une chaîne de longueur donnée (len=cap)
s = with-cap-len(cap, len)   ; syntaxe intégrée : crée une chaîne de capacité et longueur données
parts = s.split(sep)          ; fractionne selon un séparateur (retourne []str, méthode)
out = ss.join(sep)            ; joint un []str avec un séparateur（méthode）
```

### num — Type union num et méthodes num

`num = int | float` (std/num.no). Les méthodes du type num se trouvent dans le module num (les fonctions génériques max/min sont dans math, voir ci-dessus) :

```no
r = num.abs(); valeur absolue（méthode）
r = num.clamp(lo, hi); borne dans un intervalle（méthode）
r = num.sign(); signe (-1/0/1, méthode）
```

### int — Type union int et méthodes int

`int = i8 | i16 | i32 | i64 | i128 | u8 | u16 | u32 | u64 | u128` (std/int.no). Les méthodes du type int se trouvent dans le module int :

```no
s = v.to-str(); entier vers chaîne（méthode）
q = v.div(b); quotient de division（retourne option）
r = v.mod(b); modulo（retourne option）
```

### float — Type union float et méthodes float

`float = f32 | f64` (std/float.no). Les méthodes du type float se trouvent dans le module float :

```no
q = v.div(b); division flottante
yes = v.is-nan(); test NaN（méthode）
yes = v.is-inf(); test Inf（méthode）
```

(Aucune méthode union `float.to-str` n'est définie ; sur les variables f64/f32, utilisez directement la méthode du type concret `v.to-str()`.)

### number — Exemples d'utilisation des méthodes

(L'API complète se trouve dans la section « number — Opérations numériques (API sous forme de méthodes) » ci-dessus ; les fonctions sont dans la section « math ».)

```no
; Forme méthode (module number)
s = v.to-str(); entier/flottant vers chaîne（toutes largeurs）
r = n.sqrt(); racine carrée entière floor(√n)
yes = n.is-prime(); test de primalité (int/u8…)
q = x.sqrt(); racine carrée flottante (méthode f64/f32)
q = x.floor(); arrondi (méthode f64/f32)
r = number.pow(a, n); puissance entière (signature i64 ; la version générique int est math.pow-int)

; Forme fonction (module math)
m = math.max(1, 2, 3); variadique générique sur num
g = math.gcd(a, b); plus grand commun diviseur
l = math.lcm(a, b); plus petit commun multiple
pi = math.pow-int(a, n); puissance entière (générique sur int)
sw = math.swap(a, b); échange

; Conversions numériques (forme méthode, anciennes fonctions math.*-to-* retirées)
f = v.to-f64(); entier → f64
n = x.to-i64(); f64 → entier (troncature)
w = f32v.to-f64(); f32 → f64 ; l'inverse est f64.to-f32()

; Constantes de plage (module number)
i8.MIN / MAX; -128 / 127
i16.MIN / MAX; -32768 / 32767
i32.MIN / MAX; -2147483648 / 2147483647
i64.MIN / MAX; -2^63 / 2^63-1
i128.MIN / MAX; -2^127 / 2^127-1
u8.MIN / MAX; 0 / 255
u16.MIN / MAX; 0 / 65535
u32.MIN / MAX; 0 / 4294967295
u64.MIN / MAX; 0 / 2^64-1
u128.MIN / MAX; 0 / 2^128-1
number.INF; infini flottant
```

### byte — Opérations sur les octets

```no
#{fmt-skip}
out = i64.to-bytes-be()         ; i64 → [8]byte big-endian
out = i64.to-bytes-le()         ; i64 → [8]byte little-endian
v = []byte.to-i64-be()          ; []byte big-endian → i64 (1~8 octets)
v = []byte.to-i64-le()          ; []byte little-endian → i64 (1~8 octets)
s = []byte.to-str()             ; []byte vers str（méthode）
s = []byte.to-hex()             ; []byte → chaîne hexadécimale majuscule
s = []byte.to-hex-lower()       ; []byte → chaîne hexadécimale minuscule
s = byte.to-str()               ; byte vers str（méthode）
```

### txt — Type texte de longueur fixe

`txt` est un type texte fixe de 256 octets, adapté aux chaînes courtes :
- Les 255 premiers octets stockent les données (`data [255]byte`)
- Le dernier octet stocke la longueur (`len byte`, l'unité est l'**octet**, plage 0-255)
- Aucune allocation sur le tas, tout est sur la pile
- Nécessite une annotation de type

Sémantique de longueur (alignée sur `str`) :
- `t.len()` → nombre de caractères (points de code) (équivalent à `t.count()`)
- `t.len-bytes()` → nombre d'octets (longueur réelle du tampon UTF-8 sous-jacent)
- Indexation / découpage / ajout et autres opérations par octets se basent sur `len-bytes()` ; les deux valeurs sont égales en ASCII pur, et `len() < len-bytes()` en présence d'UTF-8 multioctet

```no
#{fmt-skip}
t txt = 'hello'              ; nécessite une annotation de type
n = t.len()                  ; nombre de caractères (points de code) (i64)
b = t.len-bytes()            ; nombre d'octets (i64)
c = t[0]                     ; accès par index (byte)
ok = t.eq(b txt)             ; comparaison d'égalité
dst = t.copy()               ; copie
s = t.to-str()              ; convertir en str
out = t.to-bytes()           ; convertir en []byte
pos = t.index(sub txt)       ; rechercher une sous-chaîne
ok = t.contains(sub txt)     ; contient la sous-chaîne
ok = t.starts-with(sub txt)  ; test de préfixe
ok = t.ends-with(sub txt)    ; test de suffixe
t.push(c char)              ; ajoute un point de code (ignoré entièrement s'il ne tient pas dans la limite de 255)
t.push-str(s str)            ; ajoute un str (tronqué par OCTET à 255)
t.push-txt(t2 txt)           ; ajoute un txt (tronqué par OCTET à 255)
out = t.reverse()            ; inverser
out = t.slice(start, end)   ; extrait [start, end)
r = t.compare(b txt)        ; comparaison lexicographique (-1/0/1)
```

### vec — Opérations sur les tranches

```no
#{fmt-skip}
v = vec.vec-create(n, val)         ; crée une tranche de longueur n, remplie avec val
ok = []t.eq(a, b, n)           ; comparaison d'égalité
n = []t.len()                  ; longueur
[]t.push(val)                   ; ajoute (agrandissement automatique)
[]t.clear()                     ; vide (len=0, cap/data inchangés)
v = with-cap(cap)             ; syntaxe intégrée : crée une nouvelle tranche de capacité donnée (len=0)
v = with-len(len)             ; syntaxe intégrée : crée une tranche de longueur donnée (len=cap)
v = with-cap-len(cap, len)    ; syntaxe intégrée : crée une tranche de capacité et longueur données
val, new-n = []t.pop()         ; dépiler
found = []t.contains(n, val)   ; contient (n est la longueur)
[]t.reverse(n)                  ; inverse les n premiers éléments
[]t.clone(dst)                  ; copie dans dst
[]t.fill(n, val)                ; remplit les n premiers éléments
arr = []t.to-arr()             ; convertir en tableau
[]t.sort-asc()                  ; tri croissant（méthode）
[]t.sort-desc()                 ; tri décroissant（méthode）
```

### arr — Opérations sur les tableaux

```no
out = [n]
t.clone(); copie
ok = [n]
t.eq(b); comparaison d'égalité
[n]
t.fill(val); remplir
[n]
t.reverse(); inverser
ok = [n]
t.contains(val); contient
v = [n]
t.to-vec(); convertir en tranche
v = [n]
t.max(); valeur maximale
v = [n]
t.min(); valeur minimale
v = [n]
t.sum(); somme
i = [n]
t.index-of(val); index
v = [n]
t.last(); dernier élément
v = [n]
t.first(); premier élément
[n]
t.sort-asc(); tri croissant
[n]
t.sort-desc(); tri décroissant
```

### sort — Constantes de tri

```no
sort.ast; croissant
sort.desc; décroissant
```

---
