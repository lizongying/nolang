---
sidebar_position: 3.5
---

## Journalisation

### log — Journalisation par niveaux

```no
LEVEL-DEBUG = 0
LEVEL-INFO = 1
LEVEL-WARN = 2
LEVEL-ERROR = 3
LEVEL-FATAL = 4

log.set-level(lvl); définit le niveau minimal de journalisation
log.debug(msg)
log.info(msg)
log.warn(msg)
log.error(msg)
log.fatal(msg, code); journalise et quitte avec le code d'état donné
```

### Format de sortie

Combinez les bits de format avec `set-format` (par défaut, seul `F-LEVEL`, pour rester proche de l'ancienne sortie) :

```no
F-TIME = 1; horodatage AAAA-MM-JJ HH:MM:SS (UTC)
F-LEVEL = 2; étiquette de niveau [INFO] / [ERROR]
F-LOC = 4; point d'appel [fichier:ligne:colonne]

log.set-format(log.F-TIME | log.F-LEVEL | log.F-LOC)
log.info('server started'); 2026-.. ..:.. [INFO] app.no:7:5 server started
```

Le « fichier:ligne:colonne » de `F-LOC` est injecté automatiquement par le compilateur au point d'appel via `#{track-caller}` ; aucun passage manuel de paramètre n'est nécessaire. Chacune des fonctions `debug / info / warn / error / fatal` comporte en dernière position un emplacement `loc str` (sans valeur par défaut ; l'appelant peut l'omettre, et une valeur explicitement passée prévaut, afin qu'un wrapper puisse transmettre sa propre position).

### Cible de sortie (stderr / fichier)

Par défaut, les journaux sont écrits sur la sortie d'erreur standard (fd=2). On peut basculer vers un fichier ou vers n'importe quel `io.writer` :

```no
#{fmt-skip}
log.open-file(path)                    ; ouvre le fichier en mode ajout et en fait la cible, retourne ?bool
log.set-output-fd(fd)                  ; bascule vers un fd personnalisé (l'appelant ouvre/ferme)
log.set-writer(w io.writer)            ; bascule vers un io.writer (utilise son fd)
cur = log.get-output-fd()              ; retourne le fd de la cible de sortie courante
w = log.get-writer()                   ; retourne l'io.writer correspondant à la cible courante
```

```no
log.set-format(log.F-TIME | log.F-LEVEL | log.F-LOC)
log.open-file('app.log'); les journaux suivants sont ajoutés à app.log
log.error('disk full'); [ERROR] [main.no:12:5] disk full → app.log
```

---
