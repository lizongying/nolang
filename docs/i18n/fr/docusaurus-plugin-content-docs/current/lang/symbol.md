---
sidebar_position: 4
---

# Opérateurs

## Symboles spéciaux

- `#` ; Importer un module
- `@` ; Exporter un module
- `->` ; Branche d'un match et branche if/else (par ex. `cond -> body`)
- `:` ; Expression match (par ex. `x: { ... }`)
- `{ } (true)` ; Boucle infinie (forme suffixe, ancienne écriture) ; la forme prefix `!! { }` / `true { }` est équivalente et constitue la nouvelle écriture par défaut (produite par `no fmt`)
- `<-` ; Parcours d'intervalle (par ex. `i <- [a..b]: { }`)
- `..` ; Classe parente / opérateur d'intervalle (par ex. `[a..b)`)
- `.` ; Soi-même (⚠️ dans les bornes d'un intervalle, utiliser `self.method` et non `.method`, pour éviter l'ambiguïté visuelle de trois points consécutifs)
- `?` ; Préfixe du type option (par ex. `?i64`, `?str`)
- `(cond) { }` ; Boucle conditionnelle (forme prefix, nouvelle écriture par défaut, équivalente à la forme suffixe `{ } (cond)`)
- `N * { }` ; Boucle comptée (forme prefix, nouvelle écriture par défaut, équivalente à la forme suffixe `{ } * N` ; N ≤ 0 est ignoré)
- `!` ; Faux (false) ; également le préfixe de boucle « ne jamais exécuter » (`! { }` ne s'exécute jamais)
- `!!` ; Vrai (true) ; également le préfixe de boucle « toujours exécuter » (`!! { }` boucle indéfiniment)
- `{ } ()` ; Non exécuté (parenthèses vides = false ; forme suffixe, ancienne écriture) ; les formes prefix `! { }` / `false { }` / `() { }` sont équivalentes et constituent la nouvelle écriture par défaut
- `*` ; Ignorer l'itération courante — remplace continue (accepté par le parser, mais déconseillé pour l'instant — utilisez le mot-clé)
- `**` ; Interrompre — remplace break (accepté par le parser, mais déconseillé pour l'instant — utilisez le mot-clé)
- `***` ; Instruction de retour, termine la fonction — remplace return (prévu, déconseillé pour l'instant — utilisez le mot-clé)
- `awy { }` ; Groupe de coroutines ; attend toutes les tâches `co` à la fin du groupe
- `co` ; Spawning async sans couleur, légal uniquement dans un groupe `awy { }`

## Opérateurs arithmétiques

- `+` ; Addition
- `-` ; Soustraction (également utilisé pour la concaténation de chaînes)
- `*` ; Multiplication (également utilisé pour la répétition de chaînes)
- `/` ; Division

## Opérateurs de comparaison

- `==` ; Égal à (égalité réelle des chaînes pour `str`, plusieurs caractères autorisés)
- `!=` ; Différent de (idem)
- <code>&lt;</code> ; Inférieur à
- `>` ; Supérieur à
- <code>&lt;=</code> ; Inférieur ou égal à
- `>=` ; Supérieur ou égal à

:::warning Les opérateurs de relation (<code>&lt;</code> `>` <code>&lt;=</code> `>=`) n'ont pas de sémantique lexicographique pour les chaînes

`str` n'implémente que l'égalité (`==` / `!=`). Comparer l'ordre de chaînes **renvoyait silencieusement false** par le passé ; c'est désormais une erreur de compilation.

- Autorisé : un littéral de chaîne d'**un seul caractère** `'a'`, implicitement un `char` (son point de code), comparable à un autre littéral d'un seul caractère ou à un `char` (`"a"`). Les types numériques ne sont pas affectés.
- Refusé : les chaînes de plusieurs caractères — le littéral `'ab'`, une variable `str`, ou un appel retournant `str`.
- Pour un ordre lexicographique, utiliser `str.compare(b)` (retourne -1 / 0 / 1).

```nolang
'a' <= 'z'; true — chaîne d'un seul caractère implicitement un char
ch >= 'a'; ch est un char, OK
'a' > "b"; chaîne d'un seul caractère vs char, OK
'abc' < 'abd'; erreur de compilation : chaîne de plusieurs caractères
s1 < s2; erreur de compilation : variables str
s1.compare(s2) > 0; la forme correcte
```
:::

## Opérateurs logiques

- `&&` ; ET logique
- `||` ; OU logique (également utilisé pour la combinaison de branches de match, par ex. `nil || err -> body`)
- `!` ; NON logique

## Opérateurs bit à bit

- `&` ; ET bit à bit
- `|` ; OU bit à bit
- `^` ; OU exclusif bit à bit
- `~` ; Complément bit à bit
- <code>&lt;&lt;</code> ; Décalage à gauche
- `>>` ; Décalage à droite

## Opérateurs d'affectation

- `=` ; Affectation
- `+=` ; Affectation par addition
- `-=` ; Affectation par soustraction
- `*=` ; Affectation par multiplication
- `/=` ; Affectation par division
- `%=` ; Affectation par modulo
- `&=` ; Affectation par ET bit à bit
- `|=` ; Affectation par OU bit à bit
- `^=` ; Affectation par OU exclusif bit à bit
- <code>&lt;&lt;=</code> ; Affectation par décalage à gauche
- `>>=` ; Affectation par décalage à droite

## Divers

- `?` ; Opérateur ternaire (par ex. `c = flag ? 1 : 2`)
- `as` ; Conversion de type de pointeur FFI (par ex. `y = x as *byte`)
- `..` ; Intervalle de tranche (par ex. `arr[1..3]`, `arr[1..]`, `arr[..3]`)
