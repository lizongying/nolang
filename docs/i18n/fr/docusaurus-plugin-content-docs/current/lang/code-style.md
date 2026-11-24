---
sidebar_position: 4
---

# Style de code

## Retour à la ligne final (Trailing Newline / EOF)

Tout fichier source `.no` **non vide** **doit se terminer par exactement un retour à la ligne final** (c'est-à-dire que le fichier se termine par un saut de ligne unique).

- Le fichier n'a **pas** de retour à la ligne final → ajoutez-en un.
- Le fichier se termine par **plusieurs** lignes vides → réduisez-les à un seul retour à la ligne final.
- **Fichier vide** (0 octet) → laissez-le inchangé.

Répertoires exclus : `dist/`, `vscode-nolang/`, `node_modules/`.

Justification : un retour à la ligne EOF unique et uniforme évite les avertissements « no newline at end of file » dans `git diff`, rend les différences plus propres et garantit un comportement prévisible de la concaténation et des outils.

### Application automatique (intégrée à la chaîne d'outils)

Cette règle est appliquée **automatiquement** par la chaîne d'outils ; il n'existe pas de script distinct :

- **`no fmt`** : la sous-commande `fmt` de `src/cmd/no/main.go` formate les fichiers sur place et appelle `fmt.FormatFile`, qui garantit exactement un retour à la ligne final.
- **Formatage à la sauvegarde du LSP / `textDocument/formatting`** : `formatNolangCode` dans `src/lsp/server.go` → `fmt.FormatFile` ajoute ou réduit automatiquement le retour à la ligne EOF lors de la sauvegarde ou du formatage d'un fichier `.no` dans l'éditeur.

L'implémentation se trouve dans `src/fmt/formatter.go` :

- `FormatFile(code)` formate un fichier complet et appelle `ensureTrailingNewline`, qui supprime tous les `\r` / `\n` finaux (compatible CRLF et lignes vides multiples), puis ajoute un seul `\n`. Une entrée vide ou non analysable est retournée telle quelle, afin d'éviter d'endommager un fichier que le formateur ne comprend pas.
- `Format(code)` est la fonction de formatage de fragments pure (sans retour à la ligne final), utilisée principalement par les tests unitaires ; pour écrire un vrai fichier source, préférez `FormatFile`.

## Nettoyage des instructions d'import (déduplication / suppression des imports std sans alias / tri)

`no fmt` (et le formatage à la sauvegarde du LSP) normalise automatiquement le **bloc d'imports contigu en haut d'un fichier** (`# path[.fn] [alias]`) ; l'implémentation se trouve dans `src/fmt/imports.go` :

1. **Déduplication** : les imports strictement identiques (même chemin, même nom de fonction, même alias) ne conservent que la première occurrence.
2. **Suppression automatique de tout import de module std pur sans alias** : les modules de la bibliothèque standard sont chargeables implicitement (voir [Préfixes d'appel inter-modules](./module)) — un import `# std/<module>` sans alias ni `.fonction` (par ex. `# std/noml`, `# std/math`, `# std/database/sql`) ne lie aucun nom et est redondant ; le formateur le supprime. Trois exceptions le conservent :
   - un **alias** (`# std/noml n`) ou un **import de fonction** (`# std/fmt.sprintf fm`) a été écrit — c'est une liaison de nom délibérée ;
   - la ligne d'import porte un **commentaire** ou une annotation `#{...}` attachée — le contenu écrit par l'utilisateur n'est jamais supprimé silencieusement.
3. **Tri automatique** : les imports sont triés lexicographiquement et de façon stable selon chemin → nom de fonction → alias.

Limite conservatrice : dès qu'un import du bloc porte un commentaire (ligne propre au-dessus ou en fin de ligne) ou une annotation `#{...}` attachée (par ex. la stratégie de liaison native `#{bind=ffi}`), tout le bloc **conserve son ordre source et rien n'est supprimé** — le tri briserait la correspondance entre commentaires et instructions, et la suppression emporterait le commentaire avec elle. Les doublons exacts sans contenu attaché sont tout de même dédupliqués dans ce cas.

Les blocs d'imports qui apparaissent ailleurs qu'en haut du fichier (après du code) sont laissés tels quels.

```no
#{fmt-skip}
; avant                             ; après
# std/vec                           # aaa/lib
# std/noml                          # std/io
# std/io                            # std/vec
# std/noml n
# aaa/lib        ;→  les imports std sans alias (# std/noml, # std/vec, # std/io) sont supprimés ;
                    ;   # std/noml n avec alias est conservé ; le reste trié par ordre lexicographique
```

## Marqueurs de commentaire

Nolang prend en charge deux marqueurs de **commentaire sur une ligne** et un marqueur de **commentaire multiligne (par bloc)**, avec la sémantique suivante :

- `//` — marqueur monoligne traditionnel (le commentaire va jusqu'à la fin de la ligne)
- `;` — marqueur monoligne alternatif (implémenté le 2026-07-17, commentaire jusqu'à la fin de la ligne)
- `;; ... ;;` — commentaire multiligne (par bloc) (implémenté le 2026-07-18, délimiteurs symétriques ; s'il n'est pas fermé, le commentaire court jusqu'à la fin du fichier)

```no
#{fmt-skip}
// ceci est un commentaire
; ceci est aussi un commentaire, de même sens
x = 1 ; commentaire en ligne, jusqu'à la fin de la ligne

;;
   ceci est un commentaire multiligne (par bloc)
   il peut s'étendre sur plusieurs lignes
   et se termine par ;; ;;
y = 2 ;; commentaire de bloc en ligne ;;
```

**Délimitation des blocs** : `;;` ouvre et `;;` ferme ; tout ce qui se trouve entre les deux (y compris les retours à la ligne) est le contenu du commentaire. Un `;` simple à l'intérieur ne ferme pas le bloc — seul `;;` le fait. Si aucun `;;` de fermeture n'est trouvé, le commentaire court depuis le `;;` d'ouverture jusqu'à la fin du fichier. Le `;;` d'ouverture n'entre en mode multiligne **que si rien d'autre que des espaces ne le suit sur cette ligne** — s'il reste du texte après le `;;` sur la même ligne, c'est un commentaire monoligne ordinaire (ex. `;; texte explicatif` ne commente que cette ligne, la suivante reste du code). Le délimiteur de fermeture fonctionne de la même manière : seuls des espaces peuvent suivre le `;;`.

**Le formateur préserve le marqueur d'origine** : `no fmt` ne modifie jamais un marqueur de commentaire — `;` reste `;`, `//` reste `//`, `;; ... ;;` reste `;; ... ;;`. Le formateur mémorise le marqueur d'origine via `Comment.Marker` et le restitue tel quel ; le contenu du commentaire de bloc (y compris ses retours à la ligne internes) est également préservé mot pour mot, si bien que le formatage est idempotent.

**Domaine sûr** : lorsque `;` / `;;` apparaissent dans des littéraux de chaîne (par ex. `'text/plain; charset=utf-8'`, `index-from(';', pos)`) ou à l'intérieur de commentaires `//`, les analyseurs de chaînes et de commentaires du lexer les consomment en premier ; ils ne sont donc jamais traités comme des marqueurs de commentaire.

**Attention à `cond -> X; Y`** : comme `;` est désormais un commentaire, `cond -> X; Y` s'analyse comme « exécuter `cond -> X` (évaluer X puis jeter le résultat) » plus un commentaire en ligne `; Y` — **`X` n'est jamais assigné**. La forme correcte est `cond -> X = Y` (une convention établie de la stdlib ; voir `arr.no`, `uuid.no`, `path.no`, `err.no`, `assert.no`). Si vous trouvez `cond -> X; Y` dans le code, remplacez-le par `cond -> X = Y`.

**Écriture de plusieurs instructions sur une seule ligne interdite** : `;` n'est qu'un marqueur de commentaire et ne peut pas joindre plusieurs instructions sur une même ligne ; la virgule `,` ne peut pas joindre d'instructions non plus. Chaque instruction dans un bloc `{ ... }` doit occuper sa propre ligne. Compacter un bloc sur une seule ligne est l'erreur la plus dangereuse :

```no
#{fmt-skip}
; ❌ Erreur : `; return }` devient tout un commentaire, le `}` du bloc disparaît
cond -> { print('X'); return }
```

```no
; ✅ Correct : chaque instruction occupe sa propre ligne
cond -> {
    print('X')
    return
}
```

`cond -> { print('X'); return }` ne laisse en réalité que `cond -> { print('X')` : le bloc ne se ferme jamais, et toutes les instructions après le `}` orphelin (souvent la totalité du reste du corps de la fonction, parfois même les fonctions suivantes du module) sont avalées. Le compilateur ne signale pas forcément d'erreur, et un éventuel diagnostic peut pointer très loin de la vraie faute.

## Littéraux booléens et simplification de `== true` / `== false` (implémenté le 2026-09-24)

Outre les mots-clés `true` / `false`, Nolang accepte deux **formes abrégées de littéraux booléens** :

- `!!` — un littéral autonome équivalent à `true`.
- `!` — un littéral autonome équivalent à `false` (uniquement lorsque le jeton après `!` ne peut pas amorcer un opérande ; si `!` est suivi d'un `retour à la ligne` / `;` / fin de fichier / `)` / `}` / `]` / `->`, il est analysé comme `false`).

> Attention : il s'agit ici de `!!` / `!` en **position d'expression**, ce qui est différent des formes en position de boucle `!! { }` (toujours exécuter) et `! { }` (ne jamais exécuter).

Ces deux abréviations deviennent un `BooleanLiteral` ordinaire dès l'**analyse syntaxique** ; les quatre formes `f == true`, `f == !!`, `f == false`, `f == !` sont donc entièrement équivalentes syntaxiquement et suivent la même logique.

Règles de `no fmt` pour les booléens (implémentées dans `src/fmt/expr.go`) :

- **Les littéraux autonomes sont normalisés vers la forme mot-clé** : `c = !!` → `c = true`, `c = !` → `c = false`.
- **Ce qui est connu est simplifié, ce qui est inconnu ne l'est pas** : quand l'un des côtés de `==` / `!=`
  est un littéral booléen et que l'autre opérande **peut être prouvé comme une valeur bool**,
  le formateur produit la forme simplifiée. Les quatre pipelines partagent les mêmes règles de
  collecte de signatures (`collectModuleSignatures` : tables std intégrées, modules locaux atteints via
  `use` / `#`, et EnumVariants) et le même contrat d'injection `SemRebuildConfig.Apply` ; seul le fournisseur diffère :
  `no fmt` / LSP utilisent `build.FmtSigLoader`, tandis que `no build` / `no vet` passent encore par
  `preloadModuleSignatures` + `astParseConfig` (les deux loaders ne sont pas encore fusionnés).
  `no fmt` injecte avant le parse `SkipLowering` à neuf ; le LSP injecte dans `ParseDocument`
  puis publie un `parseSnapshot`, si bien que le formatage à la sauvegarde réutilise une copie profonde
  de cet AST sucré (`fmt.CloneProgramForFormat`) et, avec les réglages par défaut, ne reparsed plus. Trois sources comptent comme preuve :

  1. Une annotation de type explicite : `b bool = true`, paramètre `(b bool)` ;
  2. Une affectation de littéral booléen : `flag = true` / `flag = false` ;
  3. Un appel à une fonction dont le résultat déclaré est `bool` : **définie dans ce fichier** (par ex. `bytes-eq = ... (eq bool)`),
     ou visible dans la table de signatures (par ex. `args.has-flag` du std, un `helper.is-ok` local).
     Un appel sur un récepteur variable (`s.starts-with('a')`) est résolu via le type déjà inféré du récepteur
     dans `Sem` (`s: str`) contre `str.starts-with` ; un récepteur non inféré, ou un même nom de variable
     inféré comme des types différents dans des portées distinctes, reste inconnu (aucune supposition de
     « nom de méthode globalement unique », afin de ne pas prendre `?bool` pour un `bool` pur). Un appel
     dont la table ne peut pas résoudre le type de résultat reste inconnu.

  | Forme source (`b` ci-dessous est une variable « connue ») | Après formatage |
  | ---------------------------------------------------------- | --------------- |
  | `b == true` / `b == !!`                                    | `b`             |
  | `b == false` / `b == !`                                    | `! b`           |
  | `b != true` / `b != !!`                                    | `! b`           |
  | `b != false` / `b != !`                                    | `b`             |
  | `true == b`（littéral à gauche）                           | `b`             |

  `!=` inverse simplement la polarité ; `true == false` (les deux côtés sont des littéraux) reste inchangé.
  Un nom nu sans preuve booléenne, comme `f == true`, reste également inchangé.
- **`?T` et tout opérande inconnu conservent la comparaison** : les options (`?T`),
  les appels inter-modules dont la table de signatures ne peut résoudre le type de résultat,
  les noms nus sans affectation, les accès par point dont le type du récepteur n'est pas inféré,
  les expressions d'appel et les résultats d'opération conservent tous `== true` / `== false`. Réduire l'un d'eux à un
  `x` / `! x` nu comme condition de flot de contrôle déclenche un crash codegen MIR « option-as-i1 » —
  la v0.3.3 avait précisément annulé toute la règle pour cette raison
  (`8a8ecb59`) ; le 2026-11-15, elle a été rétablie sous une règle de « preuve positive » plutôt que
  l'ancienne inférence inverse « pas de `?T` visible, donc c'est une valeur ». Le même jour, `no fmt` / LSP ont
  également reçu les signatures inter-modules : les résultats option tels que `fs.write-str(...)` (`?bool`) sont désormais
  **visibles** et sont arrêtés par le garde-fou strict de l'option, et non par leur invisibilité. Une option est un **garde-fou
  indépendant** (`boolOperandIsOptionTyped`), vérifié avant la preuve de valeur bool, donc jamais simplifiée, que
  l'annotation subsiste ou non dans la source : `ok ?bool = fs.write-str(...)` et
  `ok = fs.write-str(...)` conservent toutes deux la comparaison. Supprimer les annotations de type redondantes est un
  comportement voulu et correct de `no fmt` (`fixRedundantTypeSource`) ; ne désactivez pas cette suppression pour
  préserver la détection des options. Tests de régression : `src/fmt/bool_comparison_test.go`,
  `src/fmt/signatures_test.go`, `src/lsp/fmt_gate_test.go`.
- **S'applique aussi dans les branches `->` / les conditions if-then autonomes** (pour les variables connues) :
  `b == ! -> return` → `! b -> return`.
- **Sûr vis-à-vis de la priorité des opérateurs** : la négation s'écrit `! ` (avec un espace) pour respecter la convention
  existante des opérateurs préfixés, ce qui garantit l'idempotence de `no fmt`. Aujourd'hui, tous les opérandes prouvables
  sont des identifiants, donc `! ` ne peut pas se réassocier ; l'implémentation conserve néanmoins la protection par
  parenthèses pour les opérandes non atomiques (`! (a + b)` et non `! a + b`), au cas où la règle de preuve serait élargie plus tard.

## Initialisation à zéro différée des valeurs de retour (Deferred Zero-Init)

Les fonctions Nolang appliquent une stratégie d'**initialisation à zéro différée** à leurs paramètres de résultat nommés (paramètres out) : le prologue de la fonction ne fait **aucune** initialisation à zéro des paramètres out. Le compilateur suit plutôt, via un bitmap `%__ret_init_bitmap`, si chaque paramètre out a été explicitement assigné quelque part dans le corps. Lorsque la fonction retourne, tout paramètre out **non explicitement assigné** reçoit automatiquement un store de valeur zéro correspondant à son type (entier → `0`, str-long → `zeroinitializer`, struct → `zeroinitializer`, option → `nil`). Ce mécanisme est symétrique avec le mécanisme de libération différée `%__move_bitmap` existant.

### Pourquoi cette conception

- **Performance** : évite d'émettre un `store zeroinitializer` pour chaque paramètre out dans le prologue, seulement pour qu'il soit écrasé par une affectation ultérieure — c'est un store purement redondant. La stratégie différée n'écrit le zéro que lorsque « rien n'a réellement été assigné ».
- **Lisibilité** : pas besoin d'écrire en tête de fonction un code récurrent d'affectation anticipée à zéro comme `found = false` / `result = nil` ; le lecteur voit directement que « seul le chemin de succès assigne ».
- **Cohérence** : utilise le même modèle de suivi par bitmap que le mécanisme de libération différée `%__move_bitmap`, gardant les internes du compilateur symétriques.

### Style recommandé

N'initialisez pas à zéro les paramètres out au début d'une fonction — le compilateur complète automatiquement au retour. N'écrivez que les affectations du « chemin de succès » :

```no
; Good : pas d'initialisation anticipée à zéro, le compilateur s'en charge
hashmap-str-tmpl.contains = (key str) (found bool) {
    val ?v = .get(key)
    val: {
        ok -> found = true

        err -> {
        }

        nil -> {
        }
    }
}
```

### Anti-modèle (à éviter)

N'écrivez pas d'affectations anticipées à zéro redondantes — ce sont des stores morts écrasés par une affectation ultérieure :

```no
#{fmt-skip}
; Bad : initialisation anticipée à zéro redondante
hashmap-str-tmpl.contains = (key str) (found bool) {
    found = false              ; redondant — le compilateur complètera à zéro au retour
    val ?v = .get(key)
    val: {
        ok -> found = true
        err -> {}
        nil -> {}
    }
}
```

### Option par défaut à nil

Pour un paramètre out de type option `?T`, le complément à zéro écrit directement `nil` (tag=1, data=0), équivalent à `result = nil`. Ainsi, les chemins du type « introuvable / ensemble vide / EOF » n'ont besoin que d'un `return` nu, sans écrire `result = nil` dans la source :

```no
#{fmt-skip}
; Good : le chemin d'échec utilise un return nu, le compilateur complète result = nil
hashmap-str-tmpl.get = (key str) (result ?v) {
    .size == 0 -> return        ; le compilateur complète result = nil
    ...
    ; fall-through : le compilateur complète result = nil
}
```

### Astuce de débogage

Si une fonction retourne une valeur zéro inattendue (par ex. `found` devrait être `true` mais vaut `false`, ou `result` devrait avoir une valeur mais vaut `nil`), vérifiez que **tous les chemins qui devraient retourner une valeur non nulle** assignent explicitement le paramètre out dans le corps. Le compilateur ne devine pas l'intention — il ne fait que compléter à zéro les paramètres out non assignés. Voir l'astuce de débogage dans `.agents/skills/nolang-debug/SKILL.md`.

## Liaison directe de l'emplacement de sortie à une variable existante (éviter les variables intermédiaires)

**Style recommandé** : lorsque vous écrivez du nouveau code et que le résultat d'un appel sert uniquement à mettre à jour une variable existante, **liez directement l'emplacement de sortie à cette variable**, sans introduire une variable de transfert utilisée une seule fois. C'est particulièrement courant lorsque la sortie est multiple et que le récepteur est lui-même l'état à mettre à jour.

```no
; ✅ Recommandé : le reste est écrit directement dans r（r est aussi le récepteur de la méthode）
qsq, r = r.div-mod(modulus)
```

```no
; ❌ À éviter : msq n'est écrit qu'une fois, sans autre usage
qsq, msq = r.div-mod(modulus)
r = msq
```

- Utilisez `_` comme espace réservé pour les sorties inutilisées : `_, r = r.div-mod(modulus)` n'introduit même pas de nouvelle variable.
- Une référence en lecture seule à l'entrée et un emplacement de sortie pointant sur le même objet est légal (voir la〈Règle d'aliasing〉) ; `r = r._mul(r)` dans `src/std/bigint.no` est le même type de mise à jour sur place, valable aussi pour une sortie unique.
- C'est une recommandation pour le **code nouveau**, pas un ordre de refactorisation : ne réécrivez pas du code existant valide pour des raisons de style (voir la〈Règle d'or〉).
- Pour la sémantique complète, les résultats mesurés et le champ d'application, voir [Référence du langage : Définition de fonction](./syntax).

## Blocs de code Markdown (`no fmt -md`)

`no fmt -md` ne met en forme que le contenu **intérieur** des délimiteurs ```` ```no ```` / ```` ```nolang ```` ; les lignes de délimitation et tout le reste du Markdown sont préservés octet pour octet. Le contenu d'un bloc est analysé comme du code source Nolang ; s'il ne peut pas être analysé, le bloc est laissé tel quel et son numéro de ligne est signalé — le formatage ne jamais endommager un fichier qu'il ne comprend pas. (`-md` et `-fix` sont mutuellement exclusifs — `-md` ne fait que mettre en forme.)

### Garder les exemples corrects et erronés dans des blocs distincts

Mettre « la forme correcte » et « la forme erronée » dans un même bloc amène l'analyseur à traiter les deux comme un seul programme : la moitié correcte perd sa validation `no fmt`, et la moitié erronée peut être silencieusement « corrigée » par le formateur jusqu'à ce que la faute ne soit plus visible. Par conséquent :

- un **exemple correct** occupe son propre bloc, sans annotation, afin que `no fmt` le mette en forme et le valide ;
- un **exemple erroné** occupe son propre bloc dont la première ligne est `#{fmt-skip}`, afin qu'il ne soit ni validé ni modifié.

```no
; ✅ Correct : chaque instruction occupe sa propre ligne
cond -> {
    print('X')
    return
}
```

```no
#{fmt-skip}
; ❌ Erreur : tout ce qui suit `;` est un commentaire, le `}` du bloc disparaît
cond -> { print('X'); return }
```

### Les trois usages légitimes de `#{fmt-skip}`

1. **Exemples erronés / contre-exemples** — ce ne sont pas des programmes valides pour commencer ; le formatage ne fait que nuire.
2. **Pseudo-code et résumés de signatures** — fragments qui n'ont que la forme, sans implémentation (par ex. le squelette de signature `f = (a i64) (r i64) { ... }`).
3. **Aide-mémoire avec colonnes de commentaires alignées** — `no fmt` réécrit `code      ; note` en `code; note`, alors que l'alignement des colonnes est tout l'intérêt de ce type de tableau ; il faut donc le filtrer.

### Ne pas ajouter d'annotation juste pour empêcher le dépliage

`no fmt` déplie un bloc vide `{ }` et une branche à une seule ligne `-> { x() }` sur plusieurs lignes :

```no
ok = (b bool) (yes bool) {
}

dispatch = (cmd str) {
    cmd == 'a' -> {
        fa()
    }
}
```

C'est une forme canonique qu'il faut **accepter** — ce n'est qu'après dépliage que le bloc dispose d'une ligne propre pour insérer les instructions suivantes ; sinon, chaque ajout d'une ligne exige de déplier manuellement le bloc. De même, `true -> {}` qui devient `true -> {\n}` ne justifie pas un retour en arrière. Seuls les trois cas ci-dessus (contre-exemples, pseudo-code, tableaux alignés) ont réellement besoin de `#{fmt-skip}`.

### Les blocs composés uniquement de commentaires n'ont pas besoin d'annotation

Un bloc qui ne contient que des commentaires (la façon habituelle de « commenter » la forme correcte) s'analyse en **zéro instruction**. `no fmt` le traite comme « rien à mettre en forme », le laisse tel quel et ne signale rien — ce n'est pas une erreur de syntaxe, et ajouter `#{fmt-skip}` ne ferait que masquer ce fait.

### Ordre : annoter d'abord, formater ensuite

`no fmt -md -w` réécrit sur place tous les blocs qui ne sont pas filtrés. Dès qu'un exemple erroné a été « corrigé » par le formateur, le contre-exemple ne démontre plus rien et la forme d'origine est perdue à jamais. **Ajoutez `#{fmt-skip}` d'abord, lancez `-w` ensuite.**
