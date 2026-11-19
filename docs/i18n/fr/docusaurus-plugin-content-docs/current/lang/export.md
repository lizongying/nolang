---
sidebar_position: 5
---

# Export

Nolang utilise le mot-clé `@` dans le fichier `lib.no` à la racine du paquet pour déclarer les éléments exportés. Les paquets externes ne peuvent accéder qu'à ces symboles exportés lorsqu'ils importent via `#`.

> **Note** : `@` ne gère que l'exportation des symboles entre paquets Nolang et ne produit pas de symboles ABI C. Pour permettre à C / C++ / Rust / Go / Java / Python / C# d'appeler Nolang, utilisez plutôt l'annotation `#{cabi='symbol'}` avec `no build --shared`. Voir [Appeler Nolang depuis les langages hôtes](./host-interop).

## Syntaxe

```no
@ path.func
[alias]
```

- `path` — Chemin du module (relatif à la racine du paquet, commence par `/`, sans l'extension `.no`)
- `func` — Nom de la fonction/constante/énumération à exporter
- `alias` — Alias optionnel utilisé lors de l'import externe

## Règles

- Les instructions d'export ne peuvent **être écrites que** dans le fichier `lib.no` à la racine du paquet (la seule exception est la section suivante, « Aliases appelables globaux »)
- Un élément exporté par ligne
- Les éléments exportés ne peuvent être que des symboles finaux tels que fonctions, constantes, énumérations
- **Le nom du symbole doit être fourni** : `@ std/txt` (chemin seul, sans `func` ni `alias`) est une erreur (`lgxbfk9a`).
  Une telle écriture ne déclare rien — tous les consommateurs partent du principe qu'il y a un nom de symbole — elle n'est donc plus ignorée silencieusement.
  Elle ne peut pas non plus servir à « lier un type à un module » : les types n'ont pas de nom global, et l'alias de `@` n'a de sens que pour les symboles appelables
- Les structures, énumérations et autres types référencés par une fonction exportée sont **automatiquement exportés**, sans déclaration manuelle
- Si une fonction exportée n'existe pas dans le module, le LSP signale une erreur

## Exemple

```no
; lib.no - fichier d'export à la racine du paquet
@ /src/utils.greet a
@ /src/utils.hello b
@ /src/math.pi
```

```no
; src/utils.no
; Définition des fonctions exportées
greet = (name str) {
    print('Hello, ' - name)
}

hello = () {
    print('Hi')
}
```

## Aliases appelables globaux

La même déclaration, écrite dans **`std/global.no`** plutôt que dans le `lib.no` d'un paquet, a un sens différent :
elle ne filtre pas les exports, mais déclare un **second nom global appelable**. Le nom nu `<alias>` est simplement
la `func` du module `path` — exactement comme si l'on avait écrit `<path>.<func>(...)`.

```no
; std/global.no — deux écritures, même sens
@ std/err.new err; style export
err = err.new; style affectation
```

```no
e = err('disk full', code.io); identique à e = err.new('disk full', code.io)
m = e.msg(); 'disk full'
```

Règles :

- Seul `std/global.no` a ce sens ; `@` dans un `lib.no` reste un filtre d'export
- Les deux écritures sont **équivalentes** — le compilateur les normalise en une seule déclaration, et les écrire toutes deux ne crée pas de conflit
- L'écriture par affectation n'est valable **que dans `global.no`**. Dans un programme ordinaire, `x = mod.fn` est une
  variable contenant une valeur de fonction (utilisable comme appel indirect), et non un alias global
- Le côté droit de l'affectation doit être une référence nue `<module>.<fonction>` : ni un appel, ni un littéral, ni
  une lecture de champ, ni un liaison typée. Lorsque la cible n'est pas une fonction, la déclaration n'a simplement aucun effet
  (les déclarations de constantes comme `PI = math.PI` ne sont pas affectées)
- L'alias ne s'applique qu'aux **appels par nom nu**. Les appels qualifiés ne sont pas affectés : `io.err(...)` reste le
  writer stderr du module `io` et n'est pas capturé par l'alias `err`
- Un module appelant sa propre fonction de même nom n'est pas affecté (exclusion du module lui-même)
- La **liste de paramètres** de l'alias provient de la cible ; le **type de résultat** est déterminé au point d'appel par la fonction cible
- L'alias n'entre pas en conflit avec la variante `err` de l'option intégrée : là où un type option est attendu
  (`x = err('m')` après `x ?i64`, une branche de match `err ->`, une fonction retournant `?T`),
  `err(...)` reste le constructeur de la variante err de l'option (tag 2)

## Importer des symboles exportés

Les paquets externes ne peuvent accéder qu'aux éléments exportés déclarés dans `lib.no` lorsqu'ils importent via `#` :

```no
; Import de l'alias a (correspond à package-name.utils.greet)
# package-name.utils.greet a
; Ou utilisez directement le nom de la fonction
# package-name.utils.greet
```

## Prise en charge LSP

- **Aller à la définition** : cliquez sur un nom de fonction exportée ou un alias dans `lib.no` pour sauter à sa définition dans le fichier de module correspondant
- **Auto-complétion** : suggère automatiquement les chemins de fichiers et les noms de fonctions lors de la saisie de `@` et d'un chemin
- **Diagnostics d'erreur** : affiche une erreur lorsqu'une fonction exportée n'existe pas dans le module
