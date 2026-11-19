---
sidebar_position: 2.5
---

# Portée des annotations et des commentaires

Une annotation `#{...}` ne porte en elle-même aucune information sur « la portée qu'elle couvre » — cette portée est déterminée par **le nœud auquel elle est attachée**.
Cette page fixe ce point : une règle de position, un ensemble de décisions, quatre niveaux, ainsi qu'une précision (« une clé ne peut prendre en charge que certains de ces niveaux » — `overflow` est celle qui n'accepte que L3, voir §4.1), afin que l'auteur, `no fmt`, `no vet`, le codegen et le LSP donnent tous la même réponse pour la même ligne d'annotation.

> Pourquoi cela doit être fixé : si `no vet` considère qu'une même `#{overflow=wrap}` couvre toute la fonction, tandis que le codegen estime qu'elle ne couvre que la ligne suivante, on obtient une dérive sémantique silencieuse — « vet est tout vert, mais l'exécution renvoie quand même `err` ». La portée n'est pas une propriété de l'annotation, c'est **une convention partagée entre les lecteurs** ; une convention non écrite dérive de chaque côté.

---

## 1. Un principe : c'est l'hôte qui décide de la portée

Une annotation n'a que deux positions légales (les règles de position elles-mêmes sont décrites dans [`syntax.md`](syntax.md)〈Positions des annotations〉) :

```no
; ① au-dessus : sur sa propre ligne, placée au-dessus de la cible
#{overflow=wrap}
x i8 = a + b

; ② en fin de ligne : écrite sur la même ligne, après la cible
x i8 = a + b #{overflow=wrap}

; pour une cible sur plusieurs lignes (fonction, bloc), la forme en fin de ligne va après le `}` fermant
greet = () {
    print('hi')
} #{mac-arm64}
```

**Les deux sont parfaitement équivalentes** — même clé, même hôte, même portée, et `no fmt` ne réécrit jamais l'une en l'autre (la forme en fin de ligne reste en fin de ligne, celle au-dessus reste au-dessus). Le choix ne dépend que de la lisibilité : la forme en fin de ligne économise une ligne sur les instructions courtes, tandis que la forme au-dessus convient lorsqu'il faut annoter toute une expression ou lorsque l'instruction est longue.

**Écrire l'annotation sur la même ligne, avant la cible, est une erreur de compilation** ; il n'existe pas de chemin « préfixe s'appliquant à ce qui suit » :

```no
#{overflow = wrap} x i8 = a + b   ; ✗ préfixe → erreur
```

La décision se prend en trois étapes, chacune avec une seule réponse :

| Étape | Question | Source de la réponse |
| --- | --- | --- |
| 1 | Quel est l'hôte ? | Position : la première instruction non-commentaire au-dessus, ou l'instruction précédente sur la même ligne |
| 2 | À quel niveau appartient l'hôte ? La clé prend-elle en charge ce niveau ? | Le tableau de §2 + la matrice de §4 (non pris en charge ⇒ erreur de compilation) |
| 3 | Jusqu'où s'étend la portée ? | Le sous-arbre de l'hôte, moins les frontières dures internes (§3) |

---

## 2. Les quatre niveaux

| Niveau | Hôte | Portée effective | Exemple de notation |
| --- | --- | --- | --- |
| **L0 module** | En-tête de fichier | Toutes les déclarations et instructions de ce fichier | Tout en haut du fichier, avec une ligne vide entre elle et la suite |
| **L1 déclaration** | Constantes/variables globales, structures, interfaces, énumérations taguées, fonctions, méthodes | Toute la définition de cette déclaration (pour une fonction : le corps et tous les blocs imbriqués) | Juste au-dessus de la déclaration, ou en fin de ligne de déclaration |
| **L2 bloc** | Groupe de coroutines, groupe if, `cond -> body`, match, boucles `for`/à compteur/sur intervalle, blocs nus | Toutes les instructions du corps du bloc (blocs imbriqués compris, jusqu'aux frontières L1 internes) | Juste au-dessus de l'instruction d'en-tête du bloc, ou après le `}` fermant du bloc |
| **L3 instruction unique** | Une seule instruction (`let`, affectation, `?=`, `return`, appel, branche `cond ->`) | Cette instruction elle-même et ses expressions | Juste au-dessus de l'instruction, ou en fin de ligne |

### Niveau L0 (module)

La position est **le tout début du fichier** (aucune instruction ni import avant), et il doit y avoir **au moins une ligne vide** entre le groupe d'annotations et la première instruction — la ligne vide joue ici le rôle de « terminateur de portée » : elle signifie que ce groupe n'appartient pas à l'instruction suivante, mais au fichier entier.

```no
#{win-amd64}

; ↑ ligne vide → niveau module : ce fichier ne compile que sur Windows x86_64
; toutes les déclarations et instructions ci-dessous sont couvertes
helper = () {
    print('windows only')
}

label = 'win'
```

Sans ligne vide, on obtient un L1 ordinaire (attaché à la première déclaration), et cette frontière évite toute migration du code existant :

```no
#{buildin}
bool.to-str = () (out str) {
}
```

Le niveau module n'est utile que pour les clés qui ont un sens « pour tout le fichier » (actuellement les clés de plateforme uniquement) ; une clé de déclaration (`buildin`, `generic`, `embed`, …) placée en L0 est une erreur, et non un « élargissement de la portée ». `overflow` n'accepte pas non plus L0 — il n'accepte que L3.

La configuration inter-fichiers n'est pas une annotation ; elle se écrit dans `package.noml`.

### Niveau L1 (déclaration)

```no
; toute la structure : la disposition des champs est déterminée par le tag de type
#{inline}
holder {
    inl point
    out point
}

; toute la fonction : la journalisation d'entrée/sortie inclut automatiquement l'appelant
#{track-caller}
log.write = (msg str) {
    ...
}

; toute la déclaration de constante : un répertoire intégré (chemin relatif à package.noml)
#{embed='assets'}
ASSETS fs.embed

; élimination de tout un sous-arbre à la compilation (corps de fonction compris)
#{mac-arm64, linux-arm64}
greet = () {
    print('hello from unix')
}
```

L1 possède également une **granularité de membre** : les annotations placées sur les champs d'une structure, les variantes d'une énumération taguée ou les membres d'une énumération de style C (`a pt #{inline}`, `ok(v i64) #{inline}`) concernent ce membre, et non le type entier. Les annotations de membre n'ont elles aussi que deux positions : juste au-dessus du membre, ou après le membre sur la même ligne (la forme en fin de ligne est idiomatique).

### Niveau L2 (bloc)

Lorsque l'hôte est une « instruction d'en-tête de bloc », la portée est tout le corps du bloc :

```no
; tout le bloc est exclu (aucun IR généré, aucune vérification de type non plus)
#{win-amd64}
2 * {
    print('windows only')
}

; un groupe if est lui aussi traité comme un tout
x = 1
#{win-amd64}
{
    x == 1 -> print('arm-1')
    -> print('arm-other')
}
```

Les quatre sortes d'en-têtes de bloc s'appliquent toutes : les groupes de coroutines (`awy { ... }`), les groupes if (`{ cond -> … }`), les instructions à condition unique (`cond -> body`), le match, et les boucles `for` / à compteur / sur intervalle.

Si le corps de `cond -> body` n'est qu'une seule instruction, L2 et L3 donnent le même résultat ; la différence n'apparaît que lorsque le corps est un bloc.

**Toutes les clés ne couvrent pas les quatre niveaux.** Une clé ne peut prendre en charge que certains d'entre eux, et l'écrire à un niveau qu'elle ne prend pas en charge est une **erreur de compilation**, et non une « ligne redondante inoffensive ». La seule clé restreinte aujourd'hui est `overflow` : il n'accepte que L3, pour les raisons exposées en §4.1.

### Niveau L3 (instruction unique)

```no
a i8 = 100
b i8 = 100
; ne couvre que l'instruction immédiatement suivante (la forme en fin de ligne `c = a + b #{overflow=wrap}` est équivalente)
#{overflow=wrap}
c = a + b
c: {
    ok -> print('wrap:' - (it == -56).to-str())
    nil -> print('nil')
    err -> print('err')
}
```

Dans un bloc, une annotation sur sa propre ligne ne couvre que « l'unique instruction qui la suit immédiatement ». Une annotation sans rapport insérée entre les deux (par exemple un autre `#{...}` après `#{overflow=wrap}`) n'interrompt pas la chaîne : la première vraie instruction est toujours trouvée. Mais si l'annotation suivante porte elle-même la même clé, alors la première ne couvre aucune instruction (la seconde l'emporte).

⚠️ **Un match de bloc `{ … }: { … }` est « une instruction » et n'est pas traversé** : lorsqu'une annotation est écrite au-dessus de son `{`, l'hôte est cette instruction de bloc elle-même, et l'annotation **ne** descend **pas** vers l'expression `?=` à l'intérieur du bloc (mesuré : avec `#{overflow=wrap}` placé hors du bloc, `np ?= np + 1` à l'intérieur déborde tout de même → `err`, la cible est remise à zéro). Pour appliquer `overflow` à une expression interne, écrivez-le juste au-dessus de l'instruction `?=` concernée, à l'intérieur du bloc. La différence entre les deux formes de consommation est décrite dans [syntax.md](syntax.md) sous 〈Arbitrage entre match de bloc et match de valeur〉.

---

## 3. Héritage, substitution et frontières dures

**Héritage de l'extérieur vers l'intérieur, substitution par la plus proche.** Pour les clés qui prennent en charge plusieurs niveaux (les clés de plateforme), L0 → L1 → L2 → L3 s'appliquent successivement ; si la même clé apparaît deux fois sur un même nœud, la seconde substitue la première (`#{mac-arm64} #{mac-arm64, win-amd64}` suit la seconde).

**Le corps d'une fonction est une frontière dure.** La portée d'un bloc L2 s'arrête au **sous-arbre syntaxique** : elle ne se propage pas le long des relations d'appel et ne pénètre dans le corps de définition d'aucune fonction / méthode / fonction anonyme. Le mode utilisé dans un corps de fonction est décidé uniquement par sa propre annotation L1 ou par des annotations L3 internes, et n'a rien à voir avec l'annotation placée sur le bloc qui l'appelle.

```no
; L1 : toute la fonction est éliminée avec son corps (aucun IR généré sur macOS)
#{win-amd64}
windows-only = () {
    print('never compiled on mac')
}

; pas d'annotation L1 propre → compile sur toute plateforme, sans changer selon l'appelant
always = () {
    print('always compiled')
}
```

La raison : si une annotation externe pouvait pénétrer dans un corps de fonction, une seule ligne sur le site d'appel modifierait silencieusement la sémantique de la fonction appelée — invisiblement pendant le débogage. Une annotation L1 fait l'inverse : elle est écrite sur la tête de la fonction elle-même, comme partie intégrante de la déclaration.

**Liste des frontières dures** (l'héritage s'y arrête) : corps de fonction, corps de méthode, corps de fonction anonyme/lambda, déclarations étrangères `#{c}`.
**Non-frontières** (l'héritage continue normalement) : blocs nus `{}`, groupes if, branches match, corps de boucle.

---

## 4. Matrice clé × niveau

`✓` s'applique ／ `✗` illégal (doit être une erreur de compilation) ／ `—` sans signification à ce niveau (doit également être une erreur, et non une suppression silencieuse)

| Clé | L0 module | L1 déclaration | L2 bloc | L3 instruction |
| --- | --- | --- | --- | --- |
| `overflow` | ✗ | ✗ | ✗ | ✓ |
| Clés de plateforme (`mac-arm64`, etc.) | ✓ | ✓ | ✓ | ✓ |
| `buildin` | ✗ | ✓ fonction / énumération taguée / structure de disposition | ✗ | ✗ |
| `inline` | ✗ | ✓ structure / champ / variante / fonction | ✗ | ✓ en fin de membre |
| `generic` | ✗ | ✓ fonction / structure | ✗ | ✗ |
| `track-caller` | ✗ | ✓ fonction | ✗ | ✗ |
| `embed` | ✗ | ✓ `let` de premier niveau | ✗ | ✗ |
| `bind` | ✗ | ✓ ligne d'import | ✗ | ✗ |
| `c` / `cpp` / `rust` | ✗ | ✓ déclaration de fonction suivante | ✗ | ✗ |
| `fmt-skip` | ✗ | ✗ | ✓ bloc délimité markdown | ✗ |
| Métadonnées (`derive` / `range` / `max` / `debug`…) | ✗ | ✓ déclaration / membre | ✗ | ✗ |
| `index-out` | — | — | — | — supprimé en D11, signalé dès qu'il apparaît |

« Illégal » et « sans signification » doivent tous deux **être signalés comme erreur**, jamais être ignorés silencieusement : si un `#{buildin}` écrit au-dessus d'une boucle `for` était discrètement écarté, l'auteur croirait que la déclaration intégrée a pris effet.

### 4.0 Les trois supports de `#{buildin}`, et la particularité de la « structure de disposition »

`#{buildin}` est placé sur une déclaration, mais sa signification diffère entièrement selon les trois supports :

| Support | Signification | Exemple |
| --- | --- | --- |
| Fonction | Stub à corps vide : le compilateur / la FFI fournit l'implémentation, aucun corps n'est généré côté Nolang | `#{buildin}`<br/>`fs.remove = (p str) (ok bool) { }` |
| Énumération taguée | Les variantes servent au filtrage et à la vérification d'exhaustivité ; la représentation sous-jacente et la construction viennent du runtime intégré | `#{buildin}`<br/>`option { ok(v t), nil, err(e err.err), }` |
| **Structure de disposition** | **Simple documentation de la disposition** : cette `struct` n'existe pas sémantiquement ; son seul produit est le type des champs (`t.len` est `u8`) | `#{buildin}`<br/>`txt { data [255]byte; len u8 }` |

Le troisième cas mérite une explication à part, car il va dans le sens inverse des deux premiers : le stub de fonction et l'énumération taguée sont tous deux « le compilateur utilise réellement cette déclaration », tandis que la structure de disposition est « le compilateur **n'utilise pas** cette déclaration — elle n'est que la source des types de champs et une notice lisible sur la disposition ». La vraie disposition et les liens de documentation se trouvent ailleurs :

| Type | Source de la vraie disposition | Source des docs / des liens |
| --- | --- | --- |
| `str` | Descripteur MIR écrit à la main `%str-long = { i32 len, i32 cap, i8* data }` | `parser.BuiltinTypes()` |
| `vec` | Descripteur MIR écrit à la main `%vec = { i32 len, i32 cap, i64 data }` | `parser.BuiltinTypes()` |
| `txt` | Descripteur MIR écrit à la main `%txt = { [255 x i8] data, i8 len }` | `parser.BuiltinTypes()` |
| `arr` | Aucun descripteur : `[n]t` est l'agrégat brut `[N x T]`, et `.len` est la constante de compilation `n` | `parser.BuiltinTypes()` |

`parser.BuiltinTypes()` (`src/parser/builtintype.go`) est le registre unique « nom → module std → ancre de doc du module → disposition » ; le survol / l'aller-à-la-définition / la complétion du LSP et `IsBuiltinLayoutStructName` le lisent tous. C'est précisément pourquoi la structure de disposition **ne porte plus les liens** : supprimez-la, le survol et F12 fonctionnent toujours ; seul le checker perdrait le type du champ `t.len`, ce qui explique qu'elle est conservée pour l'instant.

Deux règles strictes en découlent ; enfoncer l'une ou l'autre produit immédiatement une « structure fantôme » qui contredit la disposition réelle :

1. **Une struct portant `#{buildin}` doit utiliser un nom de disposition intégré** (`str` / `vec` / `txt` / `arr`). La coller sur une autre struct est une erreur : MIR traite cette déclaration comme « disposition détenue par le compilateur » et la saute, tous les champs sont ignorés, et il ne reste à l'auteur qu'une pile de `unknown field`.
2. **Déclarer une struct avec un nom de disposition intégré doit comporter `#{buildin}`.** Si un utilisateur écrit une `str { … }` nue, elle est traitée comme un type ordinaire du module et préfixée (`m.str`), si bien que le codegen émet un `%m_str` qui entre en conflit avec `%str-long`.

Les deux sont signalées par `checker.ValidateBuiltinStructDecls` (`1a8m445h` / `3lu6n4np`), et `no build` comme `no vet` les détectent. Inversement, **seule** la combinaison `#{buildin}` + nom de disposition est sautée par la phase de préfixage des modules et par le `collectStructFields` du MIR (voir la Phase 1 de `src/build/module_prefix.go` et `isBuiltinLayoutStruct` dans `src/mir/hir2mir.go`) — le critère est la combinaison « annotation + nom », et non le nom seul.

### 4.1 Pourquoi `overflow` n'est donné qu'en L3

`#{overflow}` est un interrupteur sémantique **par opération**, et non des métadonnées de déclaration. Dès qu'il serait autorisé sur une fonction ou un bloc, « quel mode telle opération utilise réellement » dépendrait de sa distance à l'annotation, d'éventuelles autres annotations intercalées et du nombre de blocs empilés — répondre pour une seule ligne de code demanderait de remonter en arrière, et maintenir la même réponse entre `no fmt` / `no vet` / codegen deviendrait extrêmement difficile (une dérive a été mesurée, voir §7).

Et il **ne change pas le type** : le résultat est toujours `?T`. Un `wrap` au niveau bloc ou fonction ne transformerait pas `?T` en `T` ; il ne ferait que remplacer silencieusement « débordement ⇒ `err` » par « retour en arrière silencieux » — une erreur passant d'un « échec explicite » à une « valeur erronée qui paraît réussie », ce qui est précisément la chose qui ne devrait jamais être décidée par une annotation lointaine.

C'est pourquoi il est figé à une ligne, une annotation :

- Il ne couvre que l'instruction immédiatement suivante ; la forme en fin de ligne (`x = a + b #{overflow=wrap}`) est équivalente.
- Pour couvrir toute une région, annotez instruction par instruction — c'est ce que produisent `no fmt --fix=overflow` et le correctif rapide du LSP.
- Écrit sur une tête de fonction, de bloc ou de fichier → **erreur de compilation**, et non « inefficace mais inoffensif ».

Avantage secondaire : l'annotation reste toujours à l'écran avec l'instruction qu'elle régit, et déplacer le code ne les sépare jamais ; un `wrap` ne couvre pas non plus accidentellement l'opération voisine à la suite d'un simple réajustement d'indentation.

---

## 5. Portée des commentaires (`;` / `//` / `;;`)

Les commentaires ne participent pas à la sémantique de compilation ; leur rattachement n'influe que sur la mise en forme de `no fmt` et sur le montage de la documentation API. La syntaxe des marqueurs est décrite dans [`code-style.md`](code-style.md)〈Marqueurs de commentaires〉.

| Commentaire | Rattachement |
| --- | --- |
| En ligne (`x = 1 ; note`) | cette ligne |
| Lignes de commentaires consécutives au-dessus | la prochaine instruction/déclaration (L1 ou L3, même règle de détermination de l'hôte que les annotations) |
| Ligne vide entre le commentaire et la cible | rattaché à aucun nœud ; sert de séparateur de bloc/fichier (sauf en tête de fichier, voir L0) |
| Commentaire de bloc `;; … ;;` | s'arrête à son propre délimiteur final, ne traverse pas de portée |
| Commentaire entre une annotation et la cible | suit la cible ; l'ordre de sortie est fixé à « commentaire de doc → annotation → instruction » |

`;` et `//` ont la même signification ; `no fmt` conserve le marqueur d'origine et ne le réécrit pas. L'ancre de traduction d'un commentaire de documentation (`[get](#args.get)`) doit être la **dernière ligne** du groupe de commentaires ; elle appartient au même hôte et ne peut pas traverser de nœud.

---

## 6. Les quatre lecteurs doivent être d'accord

Pour une même annotation, quatre lecteurs doivent aboutir à la même portée :

| Lecteur | Ce qu'il doit savoir |
| --- | --- |
| parser / codegen | quel mode ce nœud et son sous-arbre utilisent |
| checker / `no vet` | la portée considérée comme « déjà annotée » |
| `no fmt` | l'annotation est réimprimée à sa place et ne doit pas être déplacée d'un hôte à l'autre |
| LSP | la position où le survol/le correctif rapide insère est égale à celle où il prend réellement effet |

Quatre invariants en découlent :

1. **Verdict d'annotation cohérent** : la portée que le codegen considère « annotée » est nécessairement aussi celle que `no vet` considère traitée, et inversement.
2. **fmt ne déplace pas** : `no fmt` ne peut ajuster la position qu'au sein du même hôte (au-dessus ↔ en fin de ligne) ; déplacer d'un hôte à l'autre revient à modifier la sémantique.
3. **Échec signalé** : une annotation tombant sur un niveau sans signification correspondante (les cases `✗` / `—` de §4) est une erreur, et non une absence silencieuse d'effet.
4. **Niveau restreint** : une clé ne s'applique qu'aux niveaux autorisés par la matrice. Lorsque `overflow` est écrit en L0/L1/L2, les quatre lecteurs doivent tous le traiter comme une **erreur** — aucune divergence de type « vet accepte, codegen refuse » (état actuel en §7).

---

## 7. État actuel et écarts (mesurés le 2026-11-11)

Vérifié point par point avec `bin/no`, sous une forme uniforme : « additionner deux `i8 = 100` et voir si le résultat est `err` ou `-56` (wrap) » ; les clés de plateforme ont été vérifiées avec `#{win-amd64}` sur macOS pour voir si le code était exclu.

| Niveau | `overflow` (conception : L3 uniquement) | Clés de plateforme | Clés de déclaration |
| --- | --- | --- | --- |
| L0 module | ✗ interdit par conception ; s'attache actuellement à la première instruction (= L1) | non implémenté | non implémenté |
| L1 déclaration | ✗ interdit par conception ; **état actuel : une faille** : `no vet` l'accepte (fonction entière sautée), le codegen non (toujours `err`) | ✓ sous-arbre entier exclu | ✓ |
| L2 bloc | ✗ interdit par conception ; le codegen ne l'accepte pas. Les têtes de `for` / groupe if / branche match peuvent faire taire `no vet` le long du bloc ; **un bloc nu `{` en position d'instruction ne parvient même pas à le faire taire** (voir ci-dessous) | ✗ le bloc compile quand même | — |
| L3 instruction | ✓ | ✓ | — |

Preuves mesurées :

```text
a-none.no    (aucune annotation)                     → err
b-line.no    (#{overflow=wrap} au-dessus d'une instr) → result:true  (-56, appliqué)
c-fn.no      (#{overflow=wrap} au-dessus d'une fn)     → err           (vet : No issues found)
d-block.no   (#{overflow=wrap} au-dessus d'un for)     → err ×2        (vet : 2 hints)

p1-block.no  (#{win-amd64} + 2 * {…})                 → le bloc s'exécute encore  (non filtré)
p2-stmt.no   (#{win-amd64} + un print isolé)          → ligne exclue             (appliqué)
p4-fn.no     (#{win-amd64} + déclaration de fn)       → fonction exclue          (appliqué)
p6-ifgroup.no(#{win-amd64} + groupe if)               → la branche s'exécute     (non filtré)

; comparaison à la compilation : commentaire sur une tête de `for` vs aucun, 15w45dqk octet pour octet identique
k2.no (tête de for #{overflow=wrap} + corps `x i8 = a + a`) → cannot assign ?i8 to i8
k3.no (même fichier, cette ligne supprimée)                  → cannot assign ?i8 to i8
```

Pourquoi un bloc nu ne parvient-il même pas à faire taire `no vet` : les cibles attachables de `parseAnnotationStatement` sont uniquement IDENT / USE / IN / RARROW / les méthodes de type tableau, et `{` n'en fait pas partie. Ainsi, dans la forme ci-dessous, l'annotation n'est attachée ni au bloc ni à une instruction interne — elle dégénère en une `AnnotationStatement` autonome, une ligne morte pour le parser, le codegen et `no vet` :

```no
#{overflow=wrap}
{
    ...
}
```

Les têtes de `for` / des groupes if / des branches match commencent par un identifiant ou par `in`, ce qui les fait tomber justement dans les cibles attachables.

Trois failles à resserrer (la règle « L3 uniquement » pour `overflow` n'est pas encore imposée) :

1. **`no vet` saute toute la fonction dès qu'il voit un `overflow` de niveau fonction** (le court-circuit `fn.OverflowMode == ""` dans `walkStmtForOverflow`) → les vérifications sont toutes vertes, tandis que l'exécution renvoie toujours `err`.
2. **`no vet` hérite vers le bas le long des blocs** (`enclosingOverflow`) → une seule annotation sur la tête d'un bloc fait taire tout le bloc, même si le codegen ne l'accepte pas du tout.
3. **parser / codegen ignorent silencieusement un `overflow` en L1/L2** sans signaler d'erreur → l'auteur n'a aucun moyen de savoir qu'il n'a pas pris effet.

Inventaire et traitement du code existant (`src/std`, après trois passages de descente avec `no fmt --fix=overflow`) :

| Catégorie | Avant migration | Après le 1er passage | Après le 2e passage | Après le 3e passage |
| --- | --- | --- | --- | --- |
| Lignes d'annotation `overflow` au total | 4580 | 4083 | 3861 | 4080 |
| dont lignes mortes « n'attachant à aucun nœud » | 330 (par forme) | 87 | **0** | **0** |
| signalées par `no vet` mais non annotables | — | ~270 | 270 | **51** |

Premier passage (2026-11-11), variation nette : 816 lignes d'annotations de portée supprimées, 325 annotations par instruction insérées. En les répartissant en A/B, environ 218 des 325 sont des sites qui manquaient déjà d'annotation et n'ont rien à voir avec la descente (les insérer sans retirer le masquage les ajouterait aussi), et environ 97 sont des intentions de niveau bloc matérialisées instruction par instruction.

Le deuxième passage (2026-11-12) a été un **nettoyage pur**, dû à ce que `findFixTargetNode` ne descendait pas dans les corps de branche et qu'il n'existait pas d'« auto-vérification après insertion » : 381 lignes supprimées, 159 ajoutées, **toutes des lignes d'annotation `#{...}`, 0 ligne de code** (vérifié ligne par ligne avec `diff`). Sur ces ajouts, 159 sont des déplacements « de la tête de bloc / de branche vers le-dessus d'une instruction », et les 222 autres sont des lignes mortes et des annotations de portée redondantes supprimées purement et simplement — ces positions n'étaient de toute façon pas efficaces, et leur suppression ne change aucun résultat de compilation ou de vérification.

Trois défauts d'outillage corrigés en même temps lors du deuxième passage :

1. **Les numéros de ligne signalés par le `checker` s'effondraient sur le conteneur** : l'`emit` de `walkExprForIntOverflow` mélangeait le `stmt.Pos().Line` du conteneur avec la colonne de l'opérateur, si bien que chaque signalement dans `{ ... }` / un groupe if / une branche match portait la ligne de l'en-tête. Le mauvais emplacement du diagnostic n'était que le symptôme ; le vrai problème était que le `findFixTargetNode` de `--fix` prenait « l'instruction la plus interne contenant cette ligne » et ne récupérait que le conteneur, si bien que l'annotation était réinsérée à la tête du bloc. Il utilise désormais **la ligne propre de l'opérateur** (`x.Token.Line`), avec repli sur la ligne du conteneur lorsque la ligne du token est absente.
2. **Pas de descente dans les corps de branche** : `findFixTargetNode` s'arrêtait délibérément au-dessus de toute l'instruction match. Il descend désormais dans les **corps de branche en forme de bloc** (le `{ ... }` de `cond -> {`, y compris le `BlockMatchStatement.Body` de la forme `{ ... }: { ... }`) ; les corps de branche en ligne ne sont toujours pas parcourus (l'instruction de `w i64 -> w + 1` est sur la même ligne que le motif de branche, l'insérer au-dessus reviendrait à l'insérer dans le match, avant la ligne du motif).
3. **Insertion même à une position non attachable** : `#{overflow}` **re-parse désormais pour s'auto-vérifier** après insertion, et retire les annotations « insérées mais dégénérées en une `AnnotationStatement` autonome » (plus de lignes mortes). Après un retrait, le source vu par un second passage de `--fix` est inchangé, si bien que l'idempotence est préservée.

Le troisième passage (2026-11-12) a touché au parser : la cible attachable **DOT** a été ajoutée à `parseAnnotationStatement`. Les instructions commençant par le récepteur, comme `.n = .n + 1` / `.cnt == 0 -> …`, sont de la même classe que `x = …` (instruction unique, L3), mais ne faisaient pas partie des cibles attachables ⇒ l'annotation dégénérait en une `AnnotationStatement` autonome (ligne morte), le site était signalé éternellement et `--fix` était retiré par l'auto-vérification même lorsqu'il insérait. Après l'assouplissement, 219 sites ont reçu une annotation L3 (lignes d'annotation 3861 → 4080) et le nombre de sites non annotables est passé de 270 à 51. La forme de l'AST en a changé (une AnnotationStatement de moins, une entrée de table auxiliaire et un `OverflowMode` de plus sur l'instruction cible), `parser.ASTVersion` a été porté à `2026-11-12.1`, et `make gen no` est obligatoire après la modification.

**`{` et `(` n'ont pas été assouplis volontairement** : `{` (bloc nu en position d'instruction / match entre parenthèses) et `(` (tête de boucle `(cond) { }`) sont des hôtes de bloc L2, alors que `overflow` est conçu pour n'accepter que L3. Les autoriser ferait retourner `overflowAnnotatedNode` en avance pour tout le bloc, ce qui légaliserait la sémantique L2, en conflit avec l'ordre « d'abord resserrer, puis assouplir » de §8. La grande majorité des 51 sites restants appartient à ces deux catégories et relève de l'étape 4b de §8.

Critères d'acceptation (arbres isolés A/B, même source et même compilateur, ne différant que par `src/std`, `no vet` fichier par fichier) : les 73 fichiers concernés ont **tous 0 erreur** (aucune rupture de compilation), 25 fichiers ont moins de diagnostics et 19 en ont davantage — et chaque augmentation est un `ovfhndld` « lié à `?T` sans valeur de débordement déclarée » exposé après le retrait du masquage, ce qui est le résultat attendu de l'étape 1 de §8, et non un nouveau défaut. Neuf autres avertissements `nolang-overflow-ineffective` sont un vieux problème du comportement préexistant de l'outil « combler le vide » sur les opérateurs surchargés de bigint / de chaîne, sans lien avec la descente. Après le deuxième passage, une nouvelle exécution complète de `bin/no run test/std/*.no` (zstd / tls / str / json / set / base32 …) a **tout PASSED**.

Critères de rejeu du troisième passage : les 3 tests `parser` en échec (`TestAnnotationsLiveOnAST` / `TestCachedASTLowersToIdenticalHIR` / `TestBuildSemViewsMatchesFreshParse`) étaient **octet pour octet identiques avant et après l'assouplissement** — `only 288 files parsed cleanly` dans les deux cas, ce qui montre que l'assouplissement n'a introduit aucun nouvel échec d'analyse ; ils reflètent l'état en cours de la refonte parallèle parser/annotation. La sortie de `no fmt -d src/std` était **octet pour octet identique** avant et après l'assouplissement, c'est-à-dire que le formateur réimprime telles quelles les annotations sur `.`, sans dérive de formatage. Les heap / zstd / tls / json / set / map / regexp de `test/std` étaient tous au vert.

**76 annotations de tête de branche subsistent** (`#{overflow=wrap}` juste au-dessus de `cond -> {`) : elles **ne sont pas des lignes mortes** — le parser copie les annotations de niveau branche sur chaque instruction du corps de branche, si bien qu'elles sont efficaces pour le corps (mesuré : une seule annotation sur la tête de branche fait taire `r = i * 2` dans le corps, ne laissant que la condition de l'en-tête signalée). Elles n'ont en revanche aucun effet sur la **condition de la branche** elle-même (une condition n'est pas une instruction). Ces 76 sont laissées telles quelles : l'insertion par instruction ne peut pas les atteindre (les sites du corps sont déjà silencieux et ne sont plus signalés), et les retirer pour les réajouter ne ferait que tourner en rond sur la ligne d'en-tête.

**51 sites encore signalés par `no vet` et que la langue actuelle ne peut pas annoter** (270 au deuxième passage, il ne reste que ceux-ci depuis que le troisième passage a assoupli DOT) : l'instruction cible commence par `{` (bloc nu en position d'instruction / match entre parenthèses) ou `(` (tête de boucle `(cond) { }`), auxquels s'ajoutent quelques conditions d'en-tête de branche. Ils sont signalés **aujourd'hui aussi** (l'annotation d'origine n'était de toute façon pas efficace), donc le retrait ne constitue pas une régression ; pour les ramener à zéro, il faut d'abord appliquer les étapes 1–2 de §8 (déclarer l'`overflow` en L2 comme erreur), puis décider s'il faut réellement prendre en charge L2 — voir l'étape 4b de §8.

En vérifiant par sondage des positions comme `archive/bzip2.no:315`, où la tête du bloc et l'intérieur du bloc portent **simultanément** `#{overflow=wrap}` : la couche de tête de bloc n'est pas efficace actuellement, c'est la couche par instruction à l'intérieur du bloc qui fonctionne réellement (`--fix=overflow` l'a déjà comblée). Supprimer la couche de niveau bloc ne change donc pas la sémantique ; mais après cette suppression, relancer `no vet` exposera les positions qui n'étaient « silencieuses que grâce à l'annotation externe » — c'est précisément l'objectif du resserrement, et non un effet secondaire. Mesuré : les hints `ovfhndld` de `crypto/rsa.no` sont passés de 19 à 22, tous sur des instructions « liées à `?T` sans valeur de débordement déclarée », sans changement sémantique.

Impact du L0 sur le code existant : 21 fichiers sous `src/**/*.no` ont une annotation comme première ligne effective — 17 sont `#{js}`, les autres sont `#{buildin}` etc. directement suivis d'une déclaration — et **aucun** n'a de ligne vide ; selon le critère L0 de §2, ils restent donc en L1, et le comportement de `no fmt` comme celui de la compilation est inchangé. `str.no` compte 2 cas de « annotation + ligne vide + branche cond » à l'intérieur d'un corps de fonction, qui ne déclenchent pas le critère L0.

---

## 8. Ordre de resserrement

1. **D'abord faire en sorte que `no vet` n'accepte plus l'`overflow` en L1/L2** : supprimer le court-circuit « sauter toute la fonction au niveau fonction » et limiter `enclosingOverflow` au sous-arbre de la même instruction (sans propagation vers le bas à travers les blocs).
2. **Puis faire signaler une erreur par le parser** : `#{overflow}` tombant en L0/L1/L2 → erreur de compilation, avec un message orientant vers « annotez instruction par instruction, ou lancez `no fmt --fix=overflow` ». Cette étape exposera d'un seul coup tout le code existant.
3. **Nettoyer le code existant** : `no fmt --fix=overflow` possède déjà la capacité de descente (retirer la couche de niveau portée, puis combler L3 instruction par instruction selon les signalements du checker) ; `src/std` a subi trois passages (voir le tableau d'inventaire de §7). Après la modification, `make gen no` est obligatoire (recuisson du blob et de StdSigKey), et `no fmt -w` ne doit pas être lancé sur `src/std` — la migration ne touche que les lignes d'annotation, fichier par fichier avec `no fmt -fix=overflow -w <f>`, `diff` garantissant l'absence d'autres modifications.
4. **Assouplir les cibles attachables du parser** (le troisième passage en a fait la moitié) :
   - **4a terminé** : `parseAnnotationStatement` accepte les instructions commençant par **DOT**. `.n = .n + 1` / `.cnt == 0 -> …` sont des instructions uniques (L3) comme `x = …`, mais ne pouvaient pas porter d'annotation auparavant ⇒ 219 `#{overflow=wrap}` dans `src/std` n'ont jamais pris effet. Après l'assouplissement, le nombre de sites non annotables est passé de 270 à 51, et `parser.ASTVersion` a été porté à `2026-11-12.1`.
   - **4b à décider** : `{` (bloc nu en position d'instruction / match entre parenthèses) et `(` (tête de boucle `(cond) { }`) sont des **hôtes de bloc L2**. Les assouplir reviendrait à légaliser la sémantique L2 pour `overflow` (`overflowAnnotatedNode` retournerait en avance pour tout le bloc), ce qui va à l'encontre de la direction de resserrement des étapes 1–2. L'ordre correct est de faire d'abord les étapes 1–2 (déclarer l'`overflow` en L2 comme erreur), puis de décider si les 51 sites restants doivent être résolus en « les réécrivant comme instructions sans bloc » ou « en comblant le corps de branche instruction par instruction », plutôt que d'ouvrir `{` / `(` sans condition.
5. **Resserrer progressivement les autres clés** : faire passer les clés de déclaration (`buildin` / `generic` / `embed`…) écrites en L2/L3 de « inefficaces » à « erreur signalée ».
6. Chaque étape doit garantir l'idempotence de `no fmt` et l'accord des quatre lecteurs sur la même entrée (§6).
