---
sidebar_position: 3
---

# Bibliothèque standard

La bibliothèque standard de Nolang (`src/std/`) contient plus de 80 modules, couvrant le formatage, les mathématiques, les chaînes, les structures de données, l'encodage/décodage, le chiffrement, la compression, les opérations sur fichiers, les abstractions E/S, les coroutines asynchrones, la détection de type de fichier, etc.

Utilisation : `# std/xxx` (les modules centraux ne nécessitent aucun import).

> **L'ancienne syntaxe `use std/xxx` fonctionne encore, mais est abandonnée (*deprecated*) ; la nouvelle syntaxe `# std/xxx` est recommandée.**

> **Attention : tous les exemples de code de ce document respectent la règle « une instruction par ligne » — il est interdit d'utiliser le point-virgule `;` ou la virgule `,` pour écrire plusieurs instructions sur une même ligne.** Par exemple, `out = from-i64(v), out = from-u64(v)` est une écriture incorrecte ; il faut la répartir sur plusieurs lignes.

---

## Catégories de modules

- [Types de base](basic-types)
- [Bibliothèque centrale](core-library)
- [Système d'exploitation et fichiers](os-and-files)
- [Heure et date](time-and-date)
- [Journalisation](logging)
- [Structures de données](data-structures)
- [Base de données](database)
- [Encodage](encoding)
- [Archives](archives)
- [Cryptographie et hachage](crypto)
- [Échange de données](data-exchange)
- [Autres](others)
- [Aperçu des modules](module-overview)
