---
sidebar_position: 6
---

# Gestion de la mémoire

Nolang est un langage **sans GC** : la sûreté mémoire est garantie par des appels à `free` insérés automatiquement par le compilateur. Cet article décrit la conception de la mémoire et la sémantique de propriété telles qu'implémentées.

## Principes fondamentaux

### Propriété unique
Chaque tampon `data` sur le tas a **exactement un propriétaire**. La propriété peut être transférée via move ; après le transfert, le propriétaire d'origine abandonne la responsabilité de libérer. Pour `=` entre variables locales, un clone profond fait que chaque variable possède ses propres données de façon indépendante.

### Sémantique d'affectation
`b = a` choisit l'une des sémantiques suivantes selon le contexte :

| Sémantique | Condition de déclenchement | Comportement |
|------|---------|------|
| **Copie de valeur** | Types primitifs (i64/f64/bool, etc.) et `can_slot_rebind` non satisfait | Copie directe de la valeur, aucune donnée sur le tas |
| **Réaffectation de slot de pile** | `b = a` entre variables locales, a est un type pile (i64/u64/i128/u128/txt) et `can_slot_rebind` satisfait | `g.varAlias[b] = a`, b et a partagent le même slot de pile, 0 copie (mieux qu'une copie de valeur) ; sinon dégradation en copie de valeur |
| **Clone profond** | `b = a` entre locales, a possède des données sur le tas (vec/arr/str/struct clonable/**`?str`, `?[]T`**) et **est encore référencé** après l'affectation | malloc de nouvelles données + memcpy + clone récursif des éléments ; a et b possèdent chacun leurs données indépendamment, chacun libéré à la fin de la fonction |
| **move (source morte)** | `b = a` entre locales, a possède des données sur le tas mais **n'est plus référencé** après l'affectation | Transfert de propriété sans copie ; la source saute le free. **Observablement équivalent** à un clone profond, en économisant une copie |
| **move** | Paramètre de sortie `out = x` | Copie superficielle de la structure + marquage de la source comme moved ; la source saute le free |
| **Clone profond** | `vec.push(x)` (x possède des données sur le tas) | malloc de nouvelles données + memcpy + clone récursif des éléments ; la source possède toujours ses propres données, chacun libéré à la fin de la fonction |

> **L'affectation valeur à valeur fait toujours un clone profond ; l'aliasage sans copie est interdit.** `b = a` (le RHS est une variable nommée entière) n'a que deux abaissements —
> **source encore vivante ⇒ clone profond**, **source morte ⇒ move** ; **aucun** « deux noms partageant un tampon avec un seul free »
> (cet abaissement a existé dans l'optimisation de propagation en lecture seule, il a été supprimé). Les **lectures d'élément/champ** (`b = a[i]`, `b = a.f`) conservent
> la sémantique d'**alias de vue**, non soumise à cette règle.

## move sur type pile (réaffectation de slot / slot-rebind)

Bien que `i64/u64/i128/u128/txt` soient tous des **types pile** (les valeurs résident directement dans le slot de pile alloca, sans `data` sur le tas), `b = a` suit tout de même par défaut une **copie de valeur** (memcpy de la valeur de pile de a vers le slot de b). Pour réduire les copies inutiles, le compilateur effectue une **réaffectation de slot de pile** pour les `b = a` de types pile satisfaisant la contrainte **can_slot_rebind** : il pose `g.varAlias[b] = a`, faisant partager à b et a le même slot de pile (0 copie, mieux qu'une copie de valeur).

### Contrainte can_slot_rebind (correction conservative)
Après réaffectation, b et a pointent vers le même slot de pile, donc toute lecture ultérieure de b lit « le stockage de a ». Pour éviter les erreurs sémantiques, la réaffectation n'est autorisée que si la source a **n'est plus du tout référencée (ni lue ni écrite)** ; sinon elle **dégrade en copie de valeur complète** (sémantique inchangée, juste un memcpy supplémentaire).

- La sûreté est résolue statiquement par `computeSlotRebindSafety` (fonction principale) / `computeMoveEligibility` (fonctions utilisateur) via `stmtContainsVarRefAny` / `exprContainsVarRefAny` : analyse toutes les instructions après le move (y compris les branches et les retours en arrière de boucle) ; si la source est encore référencée de quelque manière que ce soit, `slotRebindSafe[stmt] = false` → copie.
- L'analyse de références doit énumérer **tous les nœuds AST susceptibles de référencer une variable**, sinon une écriture non couverte entraînerait une réaffectation non sûre (slot alias corrompu → sortie erronée, voire boucle infinie). Sont couverts : au niveau instruction `LetStatement` / `ExpressionStatement` / `ForStatement` (y compris `Init`/`Update`/`Condition`/`CountExpr`/`Body` et la **collection d'itération `IterRange`**) / `ReturnStatement` / `MultiAssignStatement` / **`UnwrapAssignStatement` (affectation de déballage `?=`)** / **`BlockStatement` (bloc nu)** ; au niveau expression `Identifier` / `AssignExpression` / `Infix` / `Prefix` / `Call` / `Dot` / `Index` / `IfExpression` (corps de branches) / `Slice` / `Conditional` / `Grouped` / **`CoExpression` (`co <call>` spawn de coroutine, défensif ; la désactivation de toute la fonction est toujours gérée par `curHasUnsafeConstruct`)** / **`CastExpression`** / **`RangeExpression`**. Toute nouvelle construction référençant une variable doit être ajoutée à ces deux fonctions d'analyse.
- `match` est désugaré en `IfExpression` ; les références dans ses corps de branches sont couvertes par l'analyse ci-dessus, donc **match ne déclenche pas de désactivation** (vérifié sur `tests/match.no` / `tests/option.no`, conforme à la référence).

### Scénarios de désactivation (toujours dégradation en copie de valeur)
| Condition de désactivation | Raison |
|---------|------|
| Cible = paramètre de sortie / variable globale / variable de type tas | Les paramètres de sortie sont passés par pointeur, les globales sont visibles entre fonctions, les types tas nécessitent un free profond — la réaffectation brise la propriété |
| Source = paramètre / variable globale | Les paramètres sont passés par référence, la réaffectation corrompre le frame de pile de l'appelant |
| **Fonction stdlib** (`curIsStdLib`, déterminé par `SetStdModules`) | Les liaisons d'alias internes à stdlib (p. ex. `it` de fmt aliasé vers le local `n`) sont difficiles à modéliser entièrement par l'analyse de références |
| **Corps de fonction utilisateur contenant une closure (`FunctionLiteral`) ou un spawn de coroutine (`RunExpression`)** (`curHasUnsafeConstruct`, analysé récursivement par `bodyHasUnsafeConstruct`) | Une closure évalue les variables capturées dans un « contexte de fonction distinct » ; une coroutine s'exécute sur un autre thread. Dans les deux cas, la durée de vie des variables dépasse la portée d'alias monofonction `g.varAlias` de la fonction courante ; la réaffectation corromprait le slot de pile partagé |

> Choix de conception : les closures/coroutines utilisent une « désactivation de toute la fonction » plutôt qu'un calcul variable par variable, car l'analyse d'alias monofonction ne peut fondamentalement pas modéliser le partage de stockage entre fonctions/threads. Une désactivation de toute la fonction ne sacrifie que l'optimisation (dégradation en copie) et n'introduit jamais d'erreur, conformément au principe de « correction conservative ».

### Invalidation d'alias (alias invalidation)
- Lorsque la cible est réaffectée, `delete(g.varAlias, name)` supprime d'abord tout alias résiduel, sinon le chemin d'affectation générique `varAddr(name)` pointerait encore vers l'ancien slot source et polluerait l'ancienne source.
- La réaffectation effectue une résolution transitive : suit la chaîne `g.varAlias` jusqu'au slot source final (`a → c → …`), évitant les désalignements d'alias sur plusieurs sauts ; `g.varTypes[name]` est synchronisé pour que les requêtes de type ultérieures restent cohérentes.

### Orthogonalité avec le move sur types tas
La réaffectation de slot de pile n'agit que sur la sémantique de **copie de valeur** des `b = a` de types pile ; elle est entièrement orthogonale aux chemins « clone profond / move (paramètre de sortie) » des types possédant des données sur le tas, qui ne sont pas affectés par `slotRebindSafe`.

### Références de tests
- `tests/slot-rebind.no` : réaffectation i64/u64, comparaison i128/u128 via `==`, txt via `.len-bytes()`, dégradation en copie (`da` réutilisé → copie pour `db`/`dc`), après réaffectation (`ra`=10), move de paramètre (`pm_fn` source = paramètre → copie), réaffectation après réaffectation de slot (`rr_fn` → `rr`=99), consommation (`consume(m)` → `cm`=84). Sortie attendue `42 7 1 1 17 5 5 10 100 99 84`.
- `tests/slot-rebind-unsafe.no` : coroutine (`awy { co ... }`) capturant une variable de pile, vérifie qu'après désactivation de la réaffectation par `curHasUnsafeConstruct` la sortie reste conforme à la référence.

### free insérés par le compilateur
- À la fin de la fonction : libère toutes les variables locales sur le tas non marquées moved
- Avant réaffectation : libère l'ancienne valeur
- Champs de structure : libère récursivement les champs contenant des données sur le tas

## Disposition des types

| Type Nolang | Structure mémoire | Champs | Stratégie d'allocation |
|------------|-----------|------|---------|
| `[]T` (tranche) | **16 octets** | **u32 len, u32 cap, i64 data (ptrtoint)** | malloc (tas) |
| `[N]T` (tableau fixe) | N × sizeof(T) | éléments en ligne, **aucun descripteur len/cap** | alloca (pile) ou malloc |
| `str` (chaîne longue) | **16 octets** | **u32 len, u32 cap, i8\* data** | malloc (tas) |
| `txt` (chaîne fixe) | 256 octets | `[255 x i8]` data, i8 len | alloca (pile) |
| `?T` (nullable) | **typé par sa charge utile** : `{ i8 tag, [K x i64] }` | étiquette de 1 octet + 7 octets de remplissage + charge (multiple de 8) | alloca (pile) ; pointeur sur le tas lorsque la charge dépasse le seuil |
| énumération taguée | `{ iN tag, [K x i64] }` | largeur du tag selon le nombre de variantes (à partir de `i8`), la charge est l'union de la variante la plus large | alloca (pile) |
| structure | somme des champs | chaque champ | alloca (pile) |

**La taille de `?T` est calculée, non fixe** : `?i64` = 16 octets, `?str` = 24, `?txt` = 264, et une charge dépassant `option-payload-inline-threshold` (par défaut 264 = 1 + 7 + 256, minimum 16) est placée dans une boîte sur le tas, ramenant tout l'option à 16 octets. Chaque type de charge possède son propre type `%option_<key>` (`src/mir/layout.go`) ; par conséquent « type identique ⇔ disposition identique ». La charge de `err` est toujours boîtée et ne consomme pas le budget de charge. Voir `docs/docs/lang/syntax.md`〈Types nullables (option) · Disposition mémoire〉.

La largeur du tag d'une **énumération taguée** varie selon le nombre de variantes (`<=256` → `i8`, `<=65536` → `i16`, `<2^32` → `i32`, sinon `i64`) ; la largeur de l'union de charge est « la variante la plus large », et non la somme de toutes, les champs étant disposés selon l'alignement naturel et adressés par **décalage d'octet**. Lorsque toutes les variantes sont sans charge, la disposition dégénère en `{ iN }` (à partir d'un octet).

**L'en-tête u32 de `str`** : `%str-long = type { i32, i32, i8* }` (16 octets sur les plateformes 64 bits). `len` / `cap` sont `u32` en interne (LLVM `i32`), mais **toutes les API publiques restent `i64`** : lecture via `zext i32 -> i64`, écriture via `trunc i64 -> i32`. Les signatures de `s.len-bytes()`, `s.len()`, `s.cap()`, `s._len()` / `s._cap()` / `s._set-len()` / `s._set-cap()` et `len(s)` n'ont pas changé. Cette frontière est gérée de façon centralisée par `strLenI64` / `strCapI64` / `strHeaderLoadI64` / `strHeaderStoreI64` / `strLongFromParts` du codegen : **il ne faut jamais** émettre un `load i64` / `store i64` direct sur les champs 0/1 du descripteur — avec les pointeurs opaques, le vérificateur LLVM ne l'arrêtera pas, mais l'accès de 8 octets écraserait silencieusement le champ d'en-tête adjacent (mesuré : `s.len-bytes()` a déjà renvoyé `len | cap<<32`).

Les constantes de taille associées doivent elles aussi suivre les 16 octets : le pas d'élément de `[]str`, le stockage de backing argv de `os.args()` et les littéraux de chaîne globaux `{ i32 len, i32 cap, i8* data }`. La charge de `?str` fait exactement 16 octets (2 mots i64, le premier empaquetant len et cap) ; `?str` est donc un option en ligne de 24 octets, et la charge d'une variante `str` dans une énumération taguée occupe elle aussi 2 mots. `txt` reste inchangé et n'adopte pas le u32 de `str`.

**L'en-tête u32 de `[]T`** : `%vec = type { i32, i32, i64 }` (16 octets). `len` / `cap` sont isomorphes à ceux de `str` (u32 en interne, `i64` dans les API publiques) ; la différence est que **le champ data reste `i64`** (un pointeur encodé en `ptrtoint`, et non `i8*`), afin de laisser inchangés les sites d'appel `inttoptr` / `ptrtoint` existants. Cette frontière est gérée de façon centralisée par `vecLenI64` / `vecCapI64` / `vecHeaderLoadI64` / `vecHeaderStoreI64` / `vecFromParts` (ainsi que les génériques `hdrFieldI64` / `hdrTrunc` / `hdrWiden`) ; `emitLenCap` / `emitGetField` / `emitSetField` / `ensureVecBuffer` passent tous par eux. La même interdiction que pour `str` s'applique : **il ne faut jamais** émettre un `load i64` / `store i64` direct sur les champs 0/1 de `%vec`.

Pour `%vec`, `cap == 0` est un marqueur de **vue empruntée** (`s.bytes()`, emprunt `[N]T` → `[]T`, conversion forcée str → `[]byte`) : il ne possède aucun tas et `@vec_free` / `@vec_retain` le sautent ; seul `cap > 0` possède le bloc pointé par `data`.

`[N]T` n'a aucun descripteur à l'exécution : au niveau LLVM c'est l'agrégat brut `[N x T]`, `.len` est remplacé directement par le compilateur par la constante de compilation `N`, et il n'y a pas de `cap`.

## free superficiel et free profond

### free superficiel
Libère uniquement le tampon data du conteneur, sans parcourir les éléments. S'applique à :
- `%str-long` (les données de chaîne sont un tampon de caractères, sans élément imbriqué possédant le tas)
- les vec/arr dont les éléments sont des types primitifs (i64, double, etc.)

### free profond
Parcourt d'abord chaque élément pour libérer récursivement ses données sur le tas, puis libère le tampon data du conteneur. S'applique aux vec/arr dont les éléments possèdent des données sur le tas :
- `[]str` (éléments %str-long)
- `[][]i64` (éléments %vec)
- `[]MyType` (éléments = structures utilisateur, libération récursive des champs)

### Vérification NULL
Avant chaque free, on vérifie `icmp eq i8* %ptr, null`, afin d'éviter free(NULL) ou la libération d'un pointeur non initialisé.

## Transfert de propriété (move)

### move à retour unique
```no
get-slice = () (out []i64) {
    local = [1, 2, 3]
    out = local; local marqué moved, non libéré en fin de fonction ; out géré par l'appelant
}

v = get-slice(); v possède data, libéré en fin de fonction
```

### move à retours multiples (selon l'ordre des paramètres)
```no
get-pair = () (a []i64, b []i64) {
    x = [1, 2]
    y = [3, 4]
    a = x; premier paramètre de sortie, x marqué moved
    b = y; deuxième paramètre de sortie, y marqué moved
}

a, b = get-pair(); a possède les données de x, b possède les données de y
```

**Ordre de traitement** : les paramètres de sortie sont traités un par un selon leur **ordre de déclaration** dans la signature de la fonction. Chaque affectation `out = src` marque la variable source comme moved de façon indépendante.

**Affectations multiples depuis la même source** : si `a` et `b` référencent la même variable source (p. ex. `a = x; b = x`), le compilateur décide automatiquement clone ou move via une **pré-analyse de vivacité** (`moveEligible`) :

- `a = x` : x est référencé plus tard par `b = x` → **clone profond**, a possède ses données indépendamment
- `b = x` : x n'est plus référencé ensuite → **move**, b reprend les données de x, x est marqué moved et saute le free

Cela évite le double-free. Règle : **la dernière référence suit le move, les précédentes suivent le clone**.

Les scénarios à branche conditionnelle (tels que `if cond { out = x }`) suivent l'état moved via une bitmap à l'exécution, voir la section suivante.

### Clone profond dans vec.push
```no
inner = [1, 2, 3]
outer.push(inner)
; les données de inner sont clonées en profondeur vers le nouvel emplacement d'élément de outer
; inner possède toujours ses propres données ; en fin de fonction inner et outer sont libérés séparément
```

Pour les types d'éléments possédant le tas (`%str-long`/`%vec`/`%arr`/structures utilisateur), push effectue un clone profond : malloc de nouvelles données + memcpy + clone récursif des éléments. La variable source et le vec externe possèdent chacun leurs propres données indépendantes ; **aucun marquage move n'est nécessaire**, ce qui évite le double-free. Pour les éléments de types primitifs (i64/f64, etc.), la valeur est simplement stockée.

### Suivi du move à l'exécution (bitmap indexée par le numéro de variable sur le tas)

Un move sous branche conditionnelle pose un défi : le compilateur ne peut pas déterminer statiquement si un move a réellement eu lieu.

```no
cond-move = (flag i64) (out []i64) {
    x = [1, 2, 3]
    if flag == 1 {
        out = x; le move n'a lieu que si flag==1
    }
    ; si flag==0, x possède toujours data et doit être libéré en fin de fonction
    ; si flag==1, la propriété de x a été transférée, il faut sauter le free en fin de fonction
}
```

Nolang résout ce problème avec une **bitmap indexée par le numéro de variable sur le tas**. À la compilation, chaque variable locale sur le tas reçoit un `varIdx` unique, et chaque bit de la bitmap à l'exécution correspond à une variable sur le tas (et non à un paramètre de sortie).

#### État du compilateur

| Champ | Type | Usage |
|------|------|------|
| `heapVarIndex` | `map[string]int` | nom de variable sur le tas → `varIdx` (variables locales sur le tas uniquement) |
| `outBindState` | `[]int` | numéro de la variable sur le tas actuellement liée à chaque paramètre de sortie (-1 = non lié, -2 = indéterminé) |
| `movedVarBitset` | `[]uint64` | bitmap moved à la compilation (utilisée quand il n'y a pas de bitmap à l'exécution) |
| `movedBitmapBase` | `string` | préfixe du nom de la variable bitmap à l'exécution (p. ex. `%__mb`, vide = non allouée) |
| `bitmapCount` | `int` | nombre de blocs bitmap u64 (= maxVarIdx/64 + 1) |

#### Règle de correspondance des numéros

Un bloc `u64` stocke 64 bits de marquage ; pour le numéro de variable sur le tas `varIdx` :

- numéro de bloc = `varIdx / 64`
- décalage dans le bloc = `varIdx % 64`
- masque = `1u64 << décalage`

Ces valeurs sont calculées directement comme constantes à la compilation, sans coût à l'exécution. Plusieurs blocs `u64` permettent de prendre en charge un nombre arbitraire de variables sur le tas, **sans limite sur le nombre de paramètres/retours**.

#### Traitement de l'affectation move (l'écrasure efface l'ancien bit)

À chaque fois qu'une variable sur le tas est déplacée vers un paramètre de sortie :

1. Si ce paramètre de sortie était auparavant lié à une autre variable (`outBindState[outIdx] >= 0`), efface d'abord le bit correspondant à l'ancienne variable
2. Puis met à 1 le bit correspondant à la variable courante
3. Met à jour le numéro de variable lié à ce paramètre de sortie (`outBindState[outIdx] = srcVarIdx`)

#### Libération en fin de fonction

Parcourt toutes les variables sur le tas, en déroulant des `if` indépendants : si le bit correspondant vaut 0, libère ; un bit à 1 signifie que la propriété a été transférée, on saute la libération.

#### Allocation de la bitmap à la demande

La variable bitmap n'est allouée que **lorsque nécessaire**, afin d'éviter le surcoût dans le code sans branche :

| Scénario | Allocation bitmap | Comportement du free |
|------|---------|----------|
| Aucun move | aucune | tout est libéré |
| move hors branche (move déterministe) | aucune | le `movedVarBitset` de compilation saute directement le free |
| move dans une branche (move conditionnel) | allouée | vérification bitmap à l'exécution : bit=1 sauter, bit=0 libérer |

Avant de générer le corps de la fonction, le compilateur pré-analyse l'AST (`detectBranchMoveToOut`) pour détecter l'existence d'une affectation move vers un paramètre de sortie dans une branche `IfExpression`/`ForStatement`/`ConditionalExpression`, et n'alloue la variable bitmap à l'exécution que si un tel motif existe. L'`alloca` de la bitmap est inséré après la génération du corps de la fonction (à ce moment `nextHeapVarIdx` a sa valeur finale) et écrit dans le bloc d'entrée.

Ce mécanisme s'applique à tous les types sur le tas (`vec`/`str-long`/`arr`/structures utilisateur).

## Clone profond (affectation entre variables locales)

```no
a []i64 = [10, 20, 30]
b = a; clone profond : malloc de nouvelles données + memcpy + clone récursif des éléments
b[0] = 99
; a[0] == 10 (a n'est pas affecté)
; b[0] == 99 (b modifié indépendamment)
```

### Processus du clone profond
1. Libère l'ancienne valeur de la variable cible (si elle possède déjà des données sur le tas)
2. `malloc` un nouveau tampon data, `memcpy` des données source vers les nouvelles données
3. Clone récursivement chaque élément possédant le tas :
   - élément `%str-long` : malloc + memcpy des données chaîne
   - élément structure utilisateur : memcpy de la structure + clone récursif des champs possédant des données sur le tas
4. Écrit le nouveau pointeur data, len, cap dans la variable cible
5. Marque la cible comme variable sur le tas (libérée en fin de fonction)

### Types clonables
| Type | Clonable en profondeur | Remarques |
|------|-------------|------|
| `%vec` / `%arr` (éléments primitifs) | ✅ | memcpy des données suffit |
| `%vec` / `%arr` (éléments %str-long) | ✅ | malloc+memcpy des données chaîne, élément par élément |
| `%vec` / `%arr` (éléments = structures clonables) | ✅ | clone récursif des champs de structure, élément par élément |
| `%vec` / `%arr` (éléments = %vec / %arr) | ✅ | clone récursif des éléments du conteneur interne via le mécanisme `elemElemType` |
| `%str-long` | ✅ | malloc + memcpy des données chaîne |
| Structure utilisateur (sans champs conteneur imbriqués) | ✅ | memcpy de la structure + clone récursif des champs sur le tas |
| Structure utilisateur (avec champs conteneur imbriqués) | ✅ | memcpy de la structure + clone récursif des champs contenant des conteneurs imbriqués (via `elemElemType`) |
| `?str` | ✅ | option à charge en ligne : clone de l'option elle-même et de ses données chaîne |
| `?[]T` | ✅ | option à charge en ligne : clone de l'option et des données de la tranche interne (clone élément par élément ; récursif pour les éléments possédant le tas comme `?[]str`) |

> **`?str` / `?[]T` ont une charge en ligne, mais possèdent le tas.** Leur charge réside directement dans le propre `%option_<key>` de l'option (`?str` a la structure `{ i8 tag, [2 x i64] }`, la charge de 16 octets contenant `{ u32 len, u32 cap, i8* data }`), mais les données `str` / le stockage de backing de la tranche qu'elle contient sont alloués sur le tas ⇒ **le clone doit copier en profondeur la charge avec le reste, et le free doit la libérer avec le reste**, les deux côtés doivent rester appariés. La libération de `?[]T` passe par le même helper de free profond que `[]T` (la récursion dépend du type d'élément), donc la dualité « clone profond apparié au free profond » tient ici aussi.
> ⚠️ Pour un option ordinaire (non `?str`/`?[]T`), la charge est un emprunt ou une valeur simple et **ne possède pas** le tas ; clone et free ne la touchent pas.
> Lorsque la charge dépasse `option-payload-inline-threshold`, l'option stocke un pointeur sur le tas ; le free doit alors libérer aussi la boîte (déterminé par `Boxed`, voir `src/mir/layout.go`).

### Différence avec move
- **Clone profond** : la source et la cible possèdent chacune leurs données indépendamment, chacune libérée en fin de fonction
- **move** : la source abandonne la propriété (marquée moved), la cible reprend les données, la source saute le free

Règles de décision pour `b = a` :
1. Si a est la source d'un paramètre de sortie → move
2. Sinon, si a possède des données sur le tas mais **n'est plus référencé après l'affectation** → move (zéro copie, observablement équivalent à un clone profond)
3. Sinon, si a possède des données sur le tas et est clonable en profondeur → clone profond
4. Sinon copie de valeur

`vec.push(x)` n'entre pas dans ces règles : push est un appel de méthode ; que x possède ou non des données sur le tas, il effectue un clone profond des éléments qui possèdent le tas (voir section précédente).

## move sur types pile (réaffectation de slot / slot-rebind)

Bien que `i64`/`u64`/`i128`/`u128`/`txt` soient des types pile (qui ne possèdent pas le tas), pour `b = a` (RHS = variable) lorsque la source `a` n'est plus référencée ensuite, le compilateur ne fait plus de copie de valeur mais effectue une **réaffectation de slot de pile** (stack-slot rebind) : il pose `g.varAlias[b] = a`, faisant partager à `b` et `a` le même slot de pile (0 copie). Cela surpasse la copie de valeur (qui doit encore émettre load+store), avec un gain particulièrement net pour les grands types pile comme `%txt` (256 octets).

### Contrainte can_slot_rebind (correction conservative)

La réaffectation de slot de pile est **plus stricte** que le `moveEligible` du move sur tas :

- `moveEligible` : la source `a` n'est pas **lue** ensuite → move autorisé (types tas ; après le move, la source saute le free, la cible n'est pas affectée).
- `can_slot_rebind` (`slotRebindSafe`) : la source `a` n'a **aucune référence (ni lecture ni écriture)** ensuite → réaffectation autorisée. Après réaffectation, `b` et `a` partagent le même slot, donc une écriture ultérieure sur `a` corromprait la valeur de `b`.

Calcul : `generateFunctionDefinition` appelle `computeMoveEligibility` (remplit à la fois `moveEligible` et `slotRebindSafe`) ; `generateMainFunction` appelle `computeSlotRebindSafety` (**remplit uniquement** `slotRebindSafe`, sans toucher `moveEligible` — la fonction principale de HEAD n'active pas le move sur tas, maintenu désactivé pour éviter un SEGFAULT). Les deux effectuent une analyse de références sensible aux branches/boucles via `stmtContainsVarRefAny` (y compris le membre gauche de `AssignExpression` et les retours en arrière de boucle).

Lorsque `can_slot_rebind` n'est pas satisfait (source encore référencée ensuite) → **dégradation en copie de valeur complète**, jamais de comportement non sûr.

### Scénarios désactivés (doivent emprunter le chemin d'affectation normal)

| Scénario | Raison |
|------|------|
| Fonction stdlib (`curIsStdLib`) | stdlib contient des constructions (match/closure/coroutine) que l'analyse de références ne peut pas modéliser entièrement ; la réaffectation corromprait les slots de pile partagés (p. ex. `it` interne de fmt aliasé vers le local `n`). Détection : `g.stdModules` (injecté par `SetStdModules` de `transpiler.go` depuis `checker.KnownStdModules()`), repli sur `g.funcOwner[fd.Name]` |
| La cible est un paramètre de sortie | Le paramètre de sortie est un pointeur passé par l'appelant ; la réaffectation le ferait pointer vers le slot source local, l'appelant ne pourrait plus le lire |
| La cible est une variable globale | La réaffectation ferait résoudre le nom global vers un slot source local, brisant la sémantique globale |
| La cible est une variable de type tas | La réaffectation de slot de pile ne s'applique qu'aux types pile |
| La source est un paramètre | Les paramètres sont passés par référence ; la réaffectation corrompt le frame de pile de l'appelant |
| La source est une variable globale | L'adresse globale est aliasée vers une source locale, erreur sémantique |

### Invalidation d'alias après réaffectation

Lorsqu'une variable est réaffectée (`b = ...`), `delete(g.varAlias, name)` supprime d'abord tout alias résiduel, afin que le chemin d'affectation générique ne pointe plus vers l'ancien slot source via `varAddr(name)` et ne pollue l'ancienne source. Lors de la définition d'un nouvel alias, on effectue une **résolution transitive** le long de la chaîne d'alias (`src := ident.Value; for { if n2,ok := g.varAlias[src]; ok { src = n2 } else break }`) pour trouver le slot source final et éviter les désalignements sur plusieurs sauts.

### Orthogonalité avec le move sur tas

La réaffectation de slot de pile n'agit que sur les types pile ; elle n'implique ni transfert de propriété ni `free`, et est entièrement indépendante des mécanismes clone/move des types tas.

**Test** : `tests/slot-rebind.no` (couvre la réaffectation i64/u64/i128/u128/txt, la dégradation en copie, l'invalidation d'alias après réaffectation de la cible, la dégradation du move de paramètre, le passage de consume ; sortie attendue `42 7 1 1 17 5 5 10 100 99 84`).

## Valeurs de retour str des fonctions extern FFI

Les fonctions FFI extern (marquées `#{c}`) renvoient des pointeurs de chaîne C (`i8*`) qui peuvent pointer vers de la mémoire statique (p. ex. `getenv`, `strerror`) ou vers un tampon externe (p. ex. `strchr` renvoie un pointeur dans son argument). Les envelopper directement dans `%str-long` ferait que `emitHeapFree` appellerait `free()` sur de la mémoire non-tas → UB.

Le compilateur insère une copie sûre sur le chemin de retour `str` des extern FFI :

1. **Vérification NULL** : si C renvoie NULL, un `%str-long` vide est construit (data=0, len=0). Notez que `str` n'est pas un option : **`s == nil` est une erreur de compilation** (`q4nilcmp`) ; pour distinguer NULL de la chaîne vide, déclarez l'extern comme retournant `?str` (NULL ⇒ `nil`), ou testez plutôt `s.len == 0`
2. **Non-NULL** : `strlen` + `malloc` + `memcpy` + terminaison nulle, copie dans un tampon sur le tas indépendant
3. **Fusion PHI** : les deux chemins sont fusionnés, et la valeur de retour `%str-long` est construite

```no
#{c}
strchr = (s str, c i64) (r str)

find = () (r str) {
    r = strchr('hello', 108); C renvoie un pointeur dans 'hello'
    ; le compilateur copie automatiquement via malloc+memcpy, r possède ses données indépendamment
    ; en fin de fonction emitHeapFree libère r.data en toute sécurité
}
```

Ce mécanisme est logiquement identique au chemin `RetCStrToStr` de clib (utilisé par les fonctions intégrées `get-env`, `get-wd`, etc.) : toutes les valeurs de chaîne C retournées possèdent une propriété indépendante.

## Libération des variables au niveau module

Les variables sur le tas au niveau module (`vec`/`str`/`arr`/structures) sont compilées en globals LLVM (`@name`) ; leur tampon `data` est initialisé par malloc dans les instructions de premier niveau à l'entrée de `main`.

Le compilateur appelle, juste avant le `ret i32 0` du `main` C :
1. `emitHeapFree` — libère les variables locales sur le tas de premier niveau (non `globalVars`)
2. `emitGlobalHeapFree` — parcourt `moduleVarTypes` et libère tous les types possédant le tas dans `globalVars`

```no
GLOBAL-STR = 'hello'; LLVM @GLOBAL-STR = global %str-long zeroinitializer
GLOBAL-VEC = [1, 2, 3]; LLVM @GLOBAL-VEC = global %vec zeroinitializer
; les instructions de premier niveau malloc data et le stockent dans le global
; avant le ret de main, emitGlobalHeapFree libère data
```

Cela évite les fuites de mémoire cumulées des services de longue durée (tels qu'un daemon avec boucle). Aucun effet pour les outils CLI ponctuels (la mémoire est récupérée par le système d'exploitation à la sortie du processus).

## Vues de tranche

L'expression de tranche `arr[1..3]` produit une vue (zéro copie) qui partage les données du tableau d'origine. Trois destins pour la vue :

| Cible | Comportement | Propriété |
|------|------|--------|
| Variable locale `v = arr[1..3]` | vue zéro copie | partage les données du tableau d'origine |
| Paramètre de sortie `out = arr[1..3]` | clone (malloc+memcpy) | propriété indépendante |
| Type explicite `[]T` : `v []i64 = arr[1..3]` | clone | propriété indépendante |

**Raison** : le paramètre de sortie s'échappe vers l'appelant ; le tableau d'origine pourrait être libéré avant la fin de la fonction, donc la vue doit être clonée en données indépendantes.

## Réaffectation et libération de l'ancienne valeur

```no
s = 'hello'; malloc du tampon data
s = 'world'; libère les données de 'hello', malloc de nouvelles données
```

Lors de la réaffectation d'un type possédant le tas, le compilateur libère automatiquement les données de l'ancienne valeur avant l'affectation, afin d'éviter les fuites.

## Libération des champs de structure

```no
Node {
    name str
    items []i64
}

n = Node {
    name: 'hello'
    items: [1, 2, 3]
}
; libération récursive en fin de fonction :
;   - n.name.data (champ %str-long)
;   - n.items.data (champ %vec)
```

Lors de la libération d'une structure, tous les champs sont parcourus et les champs de types possédant le tas voient leurs données libérées récursivement.

## Réaffectation d'un tableau fixe en tranche

```no
local [4]i64 = [100, 200, 300, 400]; local est un tableau fixe (16 octets)
local = [100, 200, 300]; réaffecté en tranche (16 octets)
```

Un tableau fixe (`%arr`, 2 champs) et une tranche (`%vec`, 3 champs) ont des dispositions mémoire différentes. Lors de la réaffectation, le compilateur alloue automatiquement une nouvelle variable `%vec` et redirige tous les accès ultérieurs, afin d'éviter un dépassement de tampon.

## Cas de test vérifiés

Les tests se trouvent dans `tests/mem-safety/` :

| Test | Contenu vérifié |
|------|---------|
| `deep-clone.no` | indépendance du clone profond de `b = a` ([]i64/[]str/str/structure) |
| `deep-free-str.no` | free profond de `[]str` |
| `deep-free-nested-vec.no` | free profond de `[][]i64` + clone profond lors de push |
| `deep-free-struct-vec.no` | free profond de `[]MyType` (structure récursive) |
| `struct-field-leak.no` | libération des données sur le tas des champs de structure |
| `slice-view-escape.no` | clone de l'affectation d'une vue de tranche à un paramètre de sortie |
| `reassign-leak.no` | libération de l'ancienne valeur lors de la réaffectation |
| `vec-push-leak.no` | libération de l'ancien tampon lors de l'extension de vec.push + clone profond des éléments possédant le tas |
| `ffi-str-return.no` | copie sûre de la valeur de retour str d'un extern FFI |
| `global-heap-free.no` | libération des variables sur le tas au niveau module à la sortie de main |
| `double-move-same-source.no` | `a=x; b=x` affectations multiples depuis la même source : premier clone + dernier move |
| `move-clone-liveness.no` | la pré-analyse de vivacité décide clone/move (avec branche conditionnelle, triple affectation) |
| `rule1-binding.no` | règle 1 : indépendance d'écriture de `b = a` pour `str`/`[]i64`/struct/`?str`/`?[]i64`, alias en lecture seule, move source morte |
| `nested-container-clone.no` | dualité clone profond/free profond des conteneurs imbriqués (`[][]str`, vec dans struct, value de map = vec, etc. — 12 cas) |

## Limitations connues

### Conteneur map
Le hashmap n'implémente pas le free profond des key/value ; les données sur le tas du conteneur map fuient.

### Variables temporaires en boucle
```no
#{fmt-skip}
loop {
    s = 'temp'   ; malloc de nouvelles données à chaque itération, anciennes données non libérées
}
```

### Vue de tranche + move du tableau d'origine
```no
view = arr[1..3]; view partage arr.data
arr = [9, 8, 7]; libère l'ancien arr.data → view devient pendante
```

### Données partagées async (corrigé le 2026-09-30)

La frontière de spawn de `awy { r = co f(x) }` **copie en profondeur** désormais les arguments possédant le tas ; la tâche ne partage plus de tampon avec l'appelant.

Les coroutines sont coopératives et monothreadées, donc « partager des données sur le tas mutables entre coroutines » n'a pas de sens au niveau du langage ; la seule valeur qui pouvait être partagée est celle que **l'appelant conserve encore après le spawn**. Avant la correction, argbuf n'était qu'une **copie bit à bit** du descripteur d'argument, `data` étant partagé avec l'appelant ; la tâche ne s'exécutait qu'au point d'attente — si l'appelant réaffectait ou quittait le champ d'application avant cela, il libérait le tampon, et la tâche lisait alors une mémoire déjà libérée (mesuré : str affichait 40 NUL, `[]i64` affichait 0, les deux avec rc=0 et sans aucun diagnostic).

Désormais la copie profonde a lieu au moment du spawn, et le wrapper libère la copie après l'appel :

| Type de paramètre | Frontière de spawn | Libération wrapper |
|---|---|---|
| `str` | `@str_clone` | `@str_free` |
| `[]T` (T possède un helper deep-clone) | `vecDeepClone` | `@vec_free` |
| struct possédant le tas (feuilles `str` en ligne / champs pointeur) | clone des feuilles sur place + copie du pointee | destructeur de structure récursif |
| autres (types valeur simple) | copie bit à bit (correcte) | libère uniquement le conteneur |

Régression : `tests/async-ownership.no`.

### Fuite lors de la réaffectation de struct (corrigé le 2026-09-30)

```no
holder {
    s str
}
h holder = holder {
    s: 'first'
}
h = holder {
    s: 'second'
}
; l'ancien tampon 'first' fuyait auparavant
```

Lors de la réaffectation d'une variable locale, l'ancienne valeur n'est libérée que si son type est `Type.Owned` (`str`/`vec`/`[]T`/`map`/`?owned`). Une struct est **volontairement pas** `Type.Owned` (ce marqueur sert aussi au lowerer pour déterminer si une liaison est un alias) ; ainsi `h = holder { … }` écrase directement toute la structure et l'ancien tampon `s` n'est jamais libéré. Pourtant, le mécanisme drop reconnaît bien les structs (`dropOwnsHeap` → `typeOwnsHeap`, et `emitDrop` libère via un destructeur récursif) : seul le filtre du lowerer était trop étroit. Après correction, les anciens champs sont libérés avant la réaffectation.

Mesuré sur 2 000 000 de réaffectations : RSS maximal 66,1 Mo → 33,8 Mo, sortie octet pour octet identique.

### Valeur du premier champ d'un littéral struct = appel de méthode (corrigé le 2026-09-30)

```no
holder {
    s str
}
h = holder {
    s: n.to-str()
}
; avant correction : 'holder' is not defined
```

La classification de `{ … }` repose sur l'examen de quelques tokens vers l'avant. Le **séparateur de branche du match est aussi `:`**, donc `s: n.to-str()` ressemble à la fois à une « branche match de motif `s` » et à un « champ de littéral struct ». Le seul élément de désambiguïsation est le token **suivant** `ident.ident` — un appel de méthode ne peut être qu'une valeur de champ. L'ancienne branche omettait `(`, si bien que tout le bloc était classé comme match ; le nom de la structure, introuvable dans un contexte match, produisait une erreur « non défini » (une valeur de champ appel de fonction ordinaire fonctionnait, elle).

Après correction, `h = holder { s: recv.method(args) }` est analysé normalement. Une branche match nolang ne peut pas commencer par `name : X(` (le séparateur est `->`, une branche commençant par `:` est nécessairement wildcard/default, et son corps ne comporte pas d'appel entre parenthèses) ; autoriser `(` ne fait donc pas confondre un match valide avec un littéral struct. Régression : `src/parser/struct_literal_field_value_test.go`.

### Type de résultat de await (simplification syntaxique du 2026-11-17)

L'ancienne syntaxe exposait le handle de tâche comme une variable `i64` ; il fallait alors transporter le type de résultat à travers une « réaffectation de slot de handle » (`t i64 = 0; t = run f(...); v = awy t`). Depuis la suppression de `run` et `awy <handle>`, le type de résultat de l'expression `co` est déterminé directement par le callee ; ce chemin n'existe plus. La vérification de non-régression est désormais assurée par `src/mir/async_boundary_ownership_test.go`, qui épingle le type de résultat du MIR sous-jacent.

### Comptage de références des tâches (simplification syntaxique du 2026-11-17)

Le `%task` interne reste le seul objet à comptage de références : `rc = 1` à la création lors du spawn, `release` à la fin de l'attente. L'ancienne syntaxe exposait un handle écrit à la main ; les chemins de copie comme `h2 = h` / `run h` oubliaient parfois de compter le retain (particulièrement grave pour les tâches à résultat void ; SIGSEGV mesuré) et étaient interceptés par le vérificateur `checkRefBalance`. Le nouveau `awy { ... }` n'expose pas de handle et attend exactement une fois chaque `co` à la fin du groupe ; la syntaxe de surface ne produit donc aucune référence hors liste. Les tests `.no` et Go sur les alias de handle / RC ont été supprimés avec `run` ; l'appariement `OpTaskRetain` / `OpAwait` au niveau MIR reste couvert par les fixtures MIR de `src/mir/opt_test.go`.

### Déplacement des arguments de spawn : zéro copie quand la source est morte (2026-10-01)

`co f(x)` effectue une copie profonde de chaque argument **possédant le tas** à la frontière de spawn, car la tâche ne s'exécute peut-être qu'à la fin du groupe, et l'appelant peut réaffecter ou quitter le champ d'application avant cela. Lorsque x **n'est définitivement plus utilisé** après le spawn, cette copie est superflue : la propriété est directement **transférée** dans la tâche.

```no
s str = 'hello'
awy {
    v = co echo(s)      ; s n'est plus lu ensuite -> transfert zéro copie
}
print(v)                ; hello

s str = 'hello'
awy {
    v = co echo(s)
}
print(s)                ; s est encore lu -> copie profonde maintenue (chacun possède sa copie)
print(v)                ; hello
```

Trois critères, tous nécessaires :

1. L'appelant **libérerait de toute façon** x — les emprunts (vues de tranche, lecture borrow) et les paramètres ne sont pas libérés par l'appelant ;
2. x est mort après le spawn (**aucune lecture ultérieure**) ;
3. Le paramètre correspondant chez le callee est `str` / `[]T` / une `struct` possédant le tas — le wrapper libère sa charge.

Pour ces trois catégories, la libération par le wrapper de la tâche et le drop d'origine de l'appelant **se réduisent au même helper** : le transfert ne fait que déplacer **la même libération** de l'appelant vers la tâche, ni plus ni moins libération.

**Sémantique utilisateur inchangée** : le tampon d'argument d'une tâche non attendue est de toute façon libéré par le wrapper au moment de l'attente ; déplacement et copie sont strictement identiques sur ce point.

Implémentation : `insertDrops` enregistre dans `Module.spawnArgMoves`, et `emitAsyncRun` saute la copie profonde en conséquence. Régression : `tests/async-arg-move.no`, `src/mir/spawn_arg_move_test.go`.

### Alias de la table des attendeurs de coroutines (corrigé le 2026-09-30)

L'ordonnanceur utilisait `@nolang_waiters = global [256 x i8*]`, indexé par `ptrtoint(task) & 255`. Les tasks sont allouées par `@malloc` et **alignées sur 16 octets** : seuls 4 bits bas du pointeur sont significatifs — les 256 slots se réduisaient en réalité à 16 (mesuré : 8 seulement étaient utilisés), et deux tâches en attente concurrente écrasaient facilement l'attendeur de l'autre, réveillant la mauvaise tâche.

Désormais toute la table est supprimée ; l'attendeur est stocké dans le 5e champ du `%task` lui-même (`%task` passe de 24 à 32 octets) : `@nolang_async_wait` écrit la tâche courante dans le champ de **la tâche attendue**, et `@nolang_async_done` lit ce champ, le met en file s'il est non nul puis le vide. La recherche est ainsi exacte, sans alias d'adresse.

> Remarque : le backend MIR pilote actuellement l'await de façon synchrone et n'appelle jamais `@nolang_async_run` / `@nolang_async_wait`. Cette correction concerne donc aujourd'hui un défaut **latent** et non une défaillance observable. Régression : `src/mir/async_boundary_ownership_test.go`.

### `awy` avalé lorsqu'il n'est pas la première instruction d'un bloc (corrigé le 2026-09-30 ; réécrit le 2026-11-17)

`skipToStatementEnd()` avance jusqu'à un token pouvant **commencer** l'instruction suivante avant de s'arrêter, or la liste blanche `isStatementBoundary()` (`src/parser/stmt.go`) omettait les mots-clés de préfixe. Ainsi, dans l'ancienne syntaxe, `run` / `awy t` pouvaient être avalés avec le retour à la ligne lorsqu'ils n'étaient pas en première position, entraînant une dégradation sémantique avec `rc=0` et sans diagnostic.

Dans la nouvelle syntaxe, `awy` possède son propre cas `parseStatement` (groupe de coroutines) et figure dans `isStatementBoundary()` avec `co` ; `run` n'est plus un mot-clé et a été retiré de la liste blanche. La règle est : **chaque token que le `switch` de `parseStatement` peut dispatcher doit figurer aussi dans `isStatementBoundary()`**.

Régression : `tests/stmt-boundary.no`, `src/parser/stmt_boundary_block_test.go`.

### Un `{ ... }` nu en position d'instruction traité comme déclaration (corrigé le 2026-09-30)

La branche `LBRACE` de `parseStatement` appelle `classifyBlockAtCurrent()` (à ce moment `currentToken` est déjà `{`, aucun nom à consommer). Les `blockEnum` / `blockIface` / `blockTaggedEnum` qu'elle renvoie n'ont **aucun sens en position d'instruction** — une vraie déclaration d'énumération / interface / énumération taguée nécessite un nom préalable et passe par la branche `classifyBlock()` plus tôt. Mais la dispatch d'instruction ne dirige que `blockUnknown` vers le bloc d'instruction ; par conséquent :

- `{ x = 2 }` est classé `blockEnum`, `{ print(2) }` est classé `blockIface` ;
- les deux sont envoyés dans `parseExpressionStatement`, dont la branche `LBRACE` de `expr.go` ne reconnaît que `blockStruct` / `blockMatch` ; les autres passent par `p.nextToken(); return nil` ;
- `{` est avalé, les instructions du bloc deviennent des **sœurs** du corps externe, et le `}` propre au bloc ferme au contraire le bloc **externe**.

Le résultat n'est pas un « avalement » mais une **fuite d'instruction d'un niveau** : `f = () { print(1); { print(2) }; print(3) }` affiche `3,1,2` ; `f = () { x = 1; { x = 2 }; print(x) }` n'affiche rien. Là encore `rc=0`, sans diagnostic.

La correction consiste à diriger aussi `blockEnum` / `blockIface` / `blockTaggedEnum` vers le bloc d'instruction dans la dispatch (`blockStruct` doit **rester** sur le chemin expression — `{ field: value }` est une affectation de littéral struct anonyme valide).
⚠️ Ne **modifiez pas** la branche `ASSIGN` de `classifyBlockAtCurrent` pour « réparer » ce problème : elle ne couvrirait qu'un des trois cas de classification erronée et déplacerait le jugement hors du seul endroit qui sait « je suis en position d'instruction ».

⚠️ **Effet collatéral** : cette correction fait survivre pour la première fois un `{ ... }` en position d'instruction en tant que `*parser.BlockStatement` ; or la liste blanche de `programUsesPrint` (`src/build/transpiler.go`, un marcheur AST manuel qui décide de charger `fmt`/`io`/`str`/`byte`) ne contient pas `BlockStatement` ⇒ un programme qui n'écrit `print` que dans un bloc nu n'est plus détecté.
**Toute modification au niveau instruction impose de relancer `go test ./build/ -run TestProgramUsesPrint`** et d'ajouter le nouveau type de conteneur dans `walkStmt`. Régression : `src/parser/stmt_boundary_block_test.go`, `tests/stmt-boundary.no`.

### La décision de sauter le free lors de la première affectation d'une globale n'est pas assez précise

Le compilateur utilise une map à la compilation `globalFirstAssigned` pour suivre si une variable globale a déjà subi sa première affectation : la première affectation saute la libération de l'ancienne valeur (l'ancienne valeur est `zeroinitializer`, pas une donnée sur le tas) ; seules les réaffectations ultérieures libèrent l'ancienne valeur sur le tas.

Cette map n'est initialisée qu'une fois pour toute la compilation, n'est pas réinitialisée par fonction et ne distingue pas les chemins de branches conditionnelles. Si une globale est affectée pour la première fois dans une branche conditionnelle, le compilateur traite selon l'ordre AST : la première affectation est marquée « première » (free sauté), la deuxième (même dans une autre branche) suit le chemin de réaffectation (tente de libérer l'ancienne valeur). Si, à l'exécution, la deuxième branche s'exécute en premier, la globale est encore `zeroinitializer` (data=NULL, len=0) et le compilateur tente de libérer une ancienne valeur non initialisée.

**Mesures d'atténuation actuelles (efficaces)** :
- Conteneur superficiel (`%str-long`) : `emitNullCheckFree` génère un test à l'exécution `icmp eq i8* dataPtr, null` et saute `call @free` si le pointeur est NULL
- Conteneur profond (`%vec/%arr`) : `emitDeepContainerFree` comporte en plus un court-circuit `len == 0` ; le len de zeroinitializer étant 0, toute la boucle de libération est sautée

Ces deux protections à l'exécution évitent tout crash réel même si la décision à la compilation manque de précision. Mais logiquement, elles s'appuient sur le test NULL à l'exécution comme filet de sécurité, et non sur une décision précise à la compilation.

**Piste d'amélioration potentielle** : remplacer `globalFirstAssigned` d'une map à la compilation par un mécanisme de suivi à l'exécution (type bitmap `movedVarBitset`), mais cela ajouterait un surcoût à l'exécution, et les mesures d'atténuation actuelles suffisent déjà.

## Choix de conception

1. **Le coût du clone profond.** `b = a` alloue et copie une copie des données ; `vec.push(x)` effectue aussi un clone profond des éléments possédant le tas.
   C'est délibéré : **échanger une copie contre toute une catégorie d'analyse d'alias** — entre valeurs, on ne conserve que « possession » et « déplacement », pas de partage. Ainsi « qui possède, qui libère, si une écriture est observable » n'a pas besoin d'un jeu de règles distinct pour chaque cas d'alias.
2. **La copie profonde à la frontière de spawn (= instantané).** `co f(x)` copie en profondeur au moment du spawn les arguments possédant le tas ; la tâche voit donc **la valeur au moment du spawn**, indépendamment des écritures ultérieures de l'appelant. **Quand la source est morte, on passe au move zéro copie.**
   ⚠️ Ne sacrifiez pas cette copie au profit d'un « tampon partagé + comptage de références » : la tâche verrait alors les **écritures en place** de l'appelant entre le spawn et l'attente de fin de groupe (partage), enfreignant la règle 4. La bonne façon d'économiser la copie est la **copie à l'écriture** (copy-on-write).
3. **Le comptage de références des tâches ne forme pas de cycle.** Le `%task` interne est le seul objet à comptage de références ; le nouveau `awy { ... }` n'expose pas de handle et chaque `co` est attendu exactement une fois à la fin du groupe ; il ne peut donc pas se former de cycle « tâches se détenant mutuellement des handles ».
4. **Pas de partage de données sur le tas mutables entre coroutines.** Les coroutines suivent une ordonnancement **coopératif et monothreadé** ; partager des données sur le tas mutables entre coroutines n'a pas de sens au niveau du langage. Autoriser le partage n'apporterait que des accès concurrents et des pointeurs pendants. La frontière de spawn transmet donc toujours une **copie indépendante** (ou transfère la propriété).
