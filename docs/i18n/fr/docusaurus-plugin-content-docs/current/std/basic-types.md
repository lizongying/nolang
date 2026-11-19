---
sidebar_position: 3.1
---

## Types de base

### types — Définitions des types

Correspondance des types Nolang vers LLVM :

| Nolang           | LLVM                                               |
| ---------------- | -------------------------------------------------- |
| `bool`           | `i1`                                               |
| `byte`           | `i8`                                               |
| `char`           | `i32`                                              |
| `i8/i16/i32/i64/i128` | `i8/i16/i32/i64/i128`                             |
| `u8/u16/u32/u64/u128` | `i8/i16/i32/i64/i128`                          |
| `f32`            | `float`                                            |
| `f64`            | `double`                                           |
| `str`            | `%str-long = { i32, i32, i8* }`（u32 len, u32 cap, data ; 16 octets sur 64 bits, alloué sur le tas） |
| `[]t`            | `%vec = { i32, i32, i64 }`（u32 len, u32 cap, ptrtoint data ; 16 octets sur 64 bits, alloué sur le tas） |
| `[n]t`           | `[n x t]`（agrégat nu, éléments en ligne ; **aucun descripteur len / cap**） |
| `txt`            | `{ [255 x i8], i8 }`（256 octets fixes） |

> **Pour `str` comme pour `[]t`, len / cap sont des `u32` en interne mais toujours des `i64` vers l'extérieur.** À la frontière du descripteur, les lectures sont élargies (`zext i32 -> i64`) et les écritures réduites (`trunc i64 -> i32`) ; les signatures de `s.len-bytes()`, `s.len()`, `s.cap()` et des privés `_len` / `_cap` / `_set-len` / `_set-cap` restent toutes en `i64`, ainsi que celle de `v.len()`. Voir [Chaînes : structure interne et API publique](../lang/str.md)
> et [Modèle mémoire : disposition des types](../lang/memory.md#型別佈局).
> Le champ `data` de `%vec` reste délibérément en `i64` (encodé par `ptrtoint`), contrairement au `i8*` de `%str-long` : les deux descripteurs
> **n'ont donc plus la même disposition** et l'on ne peut plus les traiter indifféremment avec `getelementptr` comme s'ils étaient identiques.
> `txt` n'a pas de champ `cap`, stocke sa longueur sur un seul `i8` (0-255) et **ne passe pas au u32** comme `str`.

**Types composites :**

- **Tableau de longueur variable `[]t`** : sous-jacent `%vec = { u32 len, u32 cap, i64 data }` (16 octets ; `cap == 0` signale une vue empruntée qui ne possède pas le tampon)
- **Tableau de longueur fixe `[n]t`** : tableau LLVM de taille fixe `[n x t]`, éléments en ligne, sans descripteur à l'exécution ; `.len` est la constante de compilation `n`, et il n'y a pas de `cap`
- **Chaîne `str`** : séquence d'octets allouée sur le tas, avec descripteur `{ u32 len, u32 cap, i8* data }` (16 octets sur 64 bits) ; prend en charge `s[i]`, `s[i..j]`, `s + t`. Longueur et capacité sont exposées en `i64` (`s.len-bytes()` / `s.cap()`) et stockées en `u32` en interne
- **Chaîne fixe `txt`** : structure de 256 octets fixes `{ [255]byte data, byte len }`, 255 octets de données et 1 octet stockant la **longueur en octets** (0-255), sans allocation sur le tas, nécessitant une annotation de type (`t txt = 'abc'`) ; `t.len()` retourne le nombre de points de code (aligné sur `str`, équivalent à `t.count()`), `t.len-bytes()` retourne le nombre d'octets ; l'indexation `t[i]` **lit** par **point de code** (retourne le `char` de ce point de code, et `err` / `code.index-out` hors bornes) ; **l'écriture `t[i] = c` est interdite globalement depuis le 2026-11-12** (erreur de compilation `idxwrite`) — elle réencode `c` en UTF-8 et décale les octets suivants, la longueur changeant ainsi comme effet de bord (ajout lorsque `i` égale le nombre de points de code, perte silencieuse des octets au-delà de 255) ; pour changer la longueur, utilisez `t.push(c char)` / `t.push-str(s)` / `t.push-txt(u)`, et pour modifier en place, le place-match `t[i]: { ok -> it = c }` ; pour une lecture brute par octets, utilisez `t.byte(i)` (retourne `?byte`, `nil` hors bornes) ; pour assembler un `txt` octet par octet, rassemblez directement un `[]byte`, puis copiez-le en une seule fois avec `txt.from-bytes(b)` (troncature au-delà de 255 octets, la longueur étant fixée au nombre réel d'octets). L'écrivain brut par octets `txt._set-byte` est **privé à std** depuis le 2026-11-11 et n'est pas accessible depuis le code utilisateur. Le `s[i] = v` de `str` a été **interdit globalement** le 2026-11-11 (erreur de compilation `idxwrite`, les bases `with-cap` n'étant plus exemptées) : côté `str`, il ne reste que `s.fill(val byte)`, le place-match `s[i]: { ok -> it = v }` et, pour la croissance, `s.push(c)` / `s.push-str(s)`
- **Énumération/Union** : enum taguée `option` (`ok t` / `nil` / `err str`)
- **Structure** : doit être définie sur plusieurs lignes ; les champs ne sont pas séparés par des virgules
- **Tableau associatif (Map)** : sous-jacent linked-hash-map
- **Itérateur** : `for iter.next() {}` (méthode d'interface `next() (ok bool)`)

### option — Type option

Enum taguée `option<t>` (tag=0=val, 1=nil, 2=err) :

```no
#{fmt-skip}
x ?t                ; déclare option<t>
x = 42              ; lui donne une valeur
x = nil             ; le met à vide
x = err('msg')      ; le met en erreur

; match
x: {
    val -> f(it)
    nil ->
    err -> g(it.msg())
}  
```

**Guide de style :** lorsqu'une fonction peut échouer ou retourner une valeur vide, utilisez l'option `?t` plutôt que `(val, ok bool)`. `?t` a trois états : `ok` (avec valeur), `nil` (valeur vide / absence normale) et `err` (erreur). La valeur normale est liée implicitement. Par exemple, `pop()` retourne `?i64` (`nil` = vide), `read-line()` retourne `?str` (`nil` = EOF, `err` = erreur), `lookup()` retourne `?str` (`nil` = introuvable). Voir la documentation de syntaxe pour les détails.

---
