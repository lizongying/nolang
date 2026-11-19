---
sidebar_position: 4.2
---

## Asynchrone (Async)

Nolang ne dispose que d'une seule syntaxe de surface pour la concurrence : le groupe de coroutines `awy { ... }`, dans lequel on spawn des tâches avec `co`.
Le mot-clé `run` a été supprimé (c'est désormais un identifiant ordinaire) ; `awy <handle>` et les groupes de coroutines à bloc nu ont été abandonnés ;
**`co` hors d'un groupe est une erreur de compilation** (`E_CO_OUTSIDE_GROUP`).

`co` ne lance qu'une **procédure** : l'appelé ne doit pas déclarer de résultat (le `self` implicite d'une méthode ne compte pas),
si bien que les résultats passent toujours par un canal : la procédure envoie la valeur dans le canal puis appelle `close()`, et l'appelant vide le canal hors du groupe.

### Groupes de coroutines

```no
; Procédure : n'écrit que dans le canal, sans résultat déclaré
produce = (ch chan[i64], v i64) {
    ch <- v
    ch.close()
}

ch1 chan[i64] = with-cap(1)
ch2 chan[i64] = with-cap(1)

; Concurrence : deux tâches s'exécutent ensemble, le groupe attend à la fin
awy {
    co produce(ch1, 1)
    co produce(ch2, 2)
}

r1 = 0
<- ch1: { ok -> r1 = it }
r2 = 0
<- ch2: { ok -> r2 = it }

; Séquence : le spawn suivant utilise le résultat du précédent comme argument, la « lecture du canal » servant de barrière
awy {
    co produce(ch1, r1)
}
```

Ce que l'on obtient dans `ch1` / `ch2` sont des valeurs de résultat, et non des handles de tâche. Voir `lang/syntax.md`, section « Programmation asynchrone », pour les détails.

### Async sans couleur : `co`

`co` est une syntaxe sans couleur (*colorless*) : vous écrivez le nom non coloré `produce`, et le compilateur génère automatiquement par monomorphisation une variante
`produce-async` qu'il exécute en profondeur comme une coroutine colorée sans pile. N'écrivez pas le suffixe `-async` à la main dans le code applicatif.

Deux formes :

- `co f(args)` — spawn d'une procédure ; `f` ne doit pas déclarer de résultat (sauf le `self` implicite d'une méthode).
- `co { ... }` — spawn d'une closure ; les variables libres du bloc sont capturées par valeur (instantané au moment du spawn).

`co` n'est pas une valeur et doit former une instruction autonome : `r = co f()` et `print(co f())` provoquent tous deux `c0nostmt`.

Règles :

- **Monomorphisation** : `co produce(ch, 1)` génère automatiquement `produce-async` (copie du corps de `produce`, renommée). Si `produce` est aussi appelé de façon synchrone, les deux variantes coexistent.
- **Transitivité** : si une procédure contient elle-même `awy { co child(n) }`, cette procédure est également monomorphisée en `-async`.
- **Lowering sous-jacent** (pas une syntaxe utilisateur) : chaque `co` devient un `OpRun`, et un `OpAwait` est inséré à la fin du groupe.

### Annulation

`cancelled()` permet une auto-vérification coopérative à l'intérieur d'une fonction coroutine. `cancel(h)` est une opération de handle de bas niveau ;
la nouvelle syntaxe n'expose pas de handle de tâche, et le code applicatif ne l'utilise pas.

> **Attention :** l'annulation est coopérative et non préemptive. Un appel bloquant de longue durée (comme une requête réseau) ne peut pas être interrompu de force ;
> la tâche ne s'arrête réellement qu'au prochain point de contrôle coopératif (appel à `cancelled()` ou prochaine ordonnancement de la boucle d'événements).

#### Faire soi-même l'annulation avec un canal (recommandé)

La nouvelle syntaxe n'a pas de `ch.cancel()`, et les canaux n'ont pas de variante `cancel`. Lorsqu'il faut « signaler de l'extérieur à un worker qu'il doit se terminer »,
**entretenez vous-même un canal et utilisez `close()` comme signal d'annulation** — c'est la seule sémantique d'annulation de `chan[T]`.
Parmi les trois états de la boucle de réception, `nil` signifie « fermé et vidé », ce qui est précisément la branche d'annulation ; `err` ne signale qu'un handle non initialisé,
et ne doit pas servir à recevoir une annulation.

```no
; Canal de contrôle : le fermer signifie annuler
stop chan[i64] = with-cap(0)

worker = (stop chan[i64]) {
    <- stop: {
        ok  -> { }              ; message reçu (inutilisé ici)
        nil -> print('cancel')  ; on arrive ici après stop.close()
        err -> { }              ; handle non initialisé
    }
}

stop.close()
awy {
    co worker(stop)
}
; affiche cancel
```

Pour déclencher l'annulation en cours d'exécution depuis une autre tâche, placez également l'action de fermeture dans le groupe (on ne peut pas faire `close()` après l'`awy` —
la fin du groupe attendant tout le monde, un worker déjà mis en attente sur un canal vide provoquerait un blocage). Attention, `<- ch: { ... }` est une **boucle de réception** :
après avoir reçu une valeur, elle continue d'attendre la suivante ; l'émetteur doit donc appeler `close()` après son envoi pour que l'annulateur puisse traverser la boucle et exécuter
`stop.close()` :

```no
stop chan[i64] = with-cap(0)
awake chan[i64] = with-cap(1)

worker = (stop chan[i64]) {
    <- stop: { ok -> { }  nil -> print('cancel')  err -> { } }
}

canceller = (awake chan[i64], stop chan[i64]) {
    <- awake: { ok -> { }  nil -> { }  err -> { } }   ; attend la notification, puis attend encore le close pour sortir de la boucle
    stop.close()                                       ; c'est cette instruction qui est le signal d'annulation
}

notify = (awake chan[i64]) {
    awake <- 1
    awake.close()                                      ; fait arriver la boucle de réception de canceller à nil
}

awy {
    co worker(stop)
    co canceller(awake, stop)
    co notify(awake)
}
; affiche cancel
```

Si vous voulez simplement vérifier au **début de chaque étape du worker** qu'une arrêt a été demandé, `cancelled()` est plus direct ;
mais il ne permet qu'une auto-vérification, sans handle externe pour prévenir — pour le scénario « signalé par autrui », utilisez la convention de canal ci-dessus.

---
