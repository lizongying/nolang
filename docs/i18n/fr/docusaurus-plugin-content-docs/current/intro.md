---
sidebar_position: 1
---

# Introduction à Nolang

Nolang est un langage de programmation système expérimental : sûr en mémoire, sans GC, avec une sémantique intuitive et une syntaxe minimale.

## Fonctionnalités clés

- **Sécurité mémoire** : sans ramasse-miettes, gestion automatique de la mémoire.
- **Performances puissantes** : optimisation à la compilation, pas de GC.
- **Syntaxe minimale** : moins de mots-clés, pas de pointeurs, de propriété (*ownership*) ni de durées de vie (*lifetimes*) comme charge mentale implicite ; une syntaxe plus simple.
- **Piloté par l'intention** : respecte pleinement l'intention du développeur ; la sémantique correspond à l'intuition humaine.
- **Sans plantage** : élimine les chemins de plantage implicites.

## Démarrage rapide

```no
; Bonjour, monde !
; Pas besoin de point d'entrée main
print('Hello, Nolang!')

; Déclaration de variable
count i64

; Définition de fonction
add = (a i64, b i64) (result i64) {
    result = a + b
}

; Méthode de la bibliothèque standard, appelable directement
c = math.max(a, b)

; Structure
user {
    name str
    age i64
}

; Méthode
user.greet = () {
    print('Hello, {.name}')
}

u = user {
    name: 'Alice'
    age: 30
}

u.greet()
```

## Projets

- [notools](https://github.com/lizongying/notools) — Une collection d'utilitaires en ligne de commande Unix courants implémentés en Nolang, couvrant des outils classiques tels que cat, ls, grep, wc, head, tail, etc. Elle illustre les capacités réelles de Nolang dans le domaine de la programmation système.
