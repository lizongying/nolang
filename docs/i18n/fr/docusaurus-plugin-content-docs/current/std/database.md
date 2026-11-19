---
sidebar_position: 3.7
---

## Base de données

### database/sql — Interface d'accès à la base de données

Définit les interfaces standard pour les connexions, les requêtes et les instructions précompilées de base de données ; elles sont implémentées par les pilotes concrets :

```no
; Résultat d'exécution
result {
    last-id i64
    affected i64
}

; Interface de connexion (enter/leave gérés automatiquement)
db {
    enter
    leave
    close() (ok bool)
    exec(sql str) (r result)
    query(sql str) (rs rows)
    prepare(sql str) (s stmt)
}

; Interface de jeu de résultats
rows {
    enter
    leave
    next() (ok bool)
    scan-int(col i64) (v i64)
    scan-str(col i64) (v str)
    scan-float(col i64) (v f64)
    close() (ok bool)
}

; Itérer vers la ligne suivante
; Lire un entier
; Lire une chaîne
; Lire un nombre flottant

; Interface d'instruction précompilée
stmt {
    enter
    leave
    bind-int(idx i64, v i64) (ok bool)
    bind-str(idx i64, v str) (ok bool)
    bind-bool(idx i64, v bool) (ok bool)
    exec() (r result)
    query() (rs rows)
    close() (ok bool)
}
```

---
