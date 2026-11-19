---
sidebar_position: 3.3
---

## Système d'exploitation et fichiers

### os — Interface du système d'exploitation

Fournit les variables d'environnement, les opérations sur les répertoires, la gestion des processus, les informations système, l'heure, etc. Pour les fonctions de lecture/écriture de fichiers, voir le module `fs`.

```no
; Variables d'environnement
val = os.get-env(key); renvoie ?str (nil = non défini)
os.set-env(key, 'value')

; Répertoires
dir = os.get-wd()
os.ch-dir(dir)
os.mkdir(path, mode)

; Processus
exit(code); termine le processus (élevé au rang de fonction intégrée globale, pas de préfixe os requis, voir global)
pid = os.get-pid()

; Informations système
name = os.host-name()
arch = os.get-arch()
msg = os.strerror(errnum)
errno = os.get-errno()

; Attributs de fichier et utilisateur (l'obtention des permissions est dans os, pas fs)
mode = os.stat-mode(path); obtient le mode du fichier (st_mode, renvoie ?i64, nil = échec)
uid = os.stat-uid(path); obtient le uid du propriétaire du fichier (renvoie ?i64, nil = échec)
gid = os.stat-gid(path); obtient le gid du groupe du fichier (renvoie ?i64, nil = échec)
mtime = os.stat-mtime(path); obtient l'heure de modification (secondes Unix, renvoie ?i64, nil = échec)
self-uid = os.getuid(); ID de l'utilisateur courant
self-gid = os.getgid(); ID du groupe courant

; Heure
sec = os.now()
ms = os.now-ms()
us = os.now-us()
ns = os.now-ns()
os.sleep(sec)
os.sleep-us(us)
os.sleep-ns(ns)

; Arguments de ligne de commande
argv = os.args(); tous les arguments de ligne de commande ([]str, inclut le nom du programme)
val = os.arg(idx); obtient le idx-ième argument (nil hors limites)
```

### fs — Utilitaires du système de fichiers

Encapsule un fichier ouvert dans la structure `file` et un chemin dans la structure `path`. Définit le newtype `fd` (`fd = i64`) pour éviter de confondre les descripteurs de fichier avec des valeurs `i64` arbitraires. Fournit aussi une structure `embed` pour les systèmes de fichiers intégrés à la compilation (via `#{embed='dir/path'}`).

```no
; newtype fd (i64 sous-jacent, mutuellement exclusif avec i64 dans le système de types)
fd = i64

; Structure de fichier
file {
    fd fd
    path str
}

; Fichiers standard (les littéraux entiers peuvent servir à initialiser fd)
stdin = file {
    fd: 0
    path: '<stdin>'
}
stdout = file {
    fd: 1
    path: '<stdout>'
}
stderr = file {
    fd: 2
    path: '<stderr>'
}

; Descripteurs de fichier standard
STDIN-FD fd = 0
STDOUT-FD fd = 1
STDERR-FD fd = 2

; Ouvrir un fichier (avec options)
file-mode {
    read,
    write,
    append,
    read-write,
}
file-perm {
    perm-600,
    perm-644,
    perm-664,
    perm-666,
    perm-755,
    perm-777,
}
file-opts {
    mode file-mode
    perm file-perm
    excl bool
    truncate bool
    append bool
}
f = fs.open(path, opts); ouvre un fichier, renvoie ?file en cas de succès, err en cas d'échec

; Méthodes de file
read-n = f.read(buf, n); lit jusqu'à n octets dans buf
line = f.read-line(); lit une ligne (?str, nil = EOF ou erreur)
content = f.read-bytes(); lit tout le fichier comme ?[]byte (nil = vide, err = échec)
content = f.read-str(); lit tout le fichier comme ?str (nil = vide, err = échec)
written = f.write(data, n); écrit n octets
ok = f.write-str(data); écrit une chaîne (écrase), renvoie ?bool
ok = f.write-bytes(data); écrit un []byte (écrase), renvoie ?bool
ok = f.append(data, n); ajoute des données
ok = f.copy-to(dst-path); copie vers le chemin cible
ok = f.close(); ferme (les fichiers standard stdin/stdout/stderr ne sont pas fermés)
yes = f.is-open(); vérifie si le fichier est ouvert (fd >= 0)
sz = f.size(); taille du fichier (via fstat(fd), élimine TOCTOU)

; Fonctions intégrées (enregistrées dans src/builtin/os.go, documentées sous forme de commentaires dans fs.no)
; Ce sont des fonctions intégrées ForwardFunc/CLibCall — aucune implémentation .no nécessaire.

; Lecture/écriture de fichiers (obsolète, préférer read-bytes/write-bytes pour la gestion d'erreurs)
; [deprecated] read-file : utiliser read-bytes (gestion d'erreurs, sûr pour TOCTOU, lecture en boucle)
;   un slice vide signifie échec, impossible de distinguer un fichier vide d'une erreur
; [deprecated] write-file : utiliser write-bytes (gestion d'erreurs, écriture en boucle)
;   impossible de distinguer une écriture partielle d'un échec total
data = fs.read-file(path); [deprecated] lit tout le fichier comme []byte (slice vide = échec)
ok = fs.write-file(path, data); [deprecated] écrit un []byte dans le fichier (écrase). Renvoie true en cas de succès
fd = fs.open-read(path); ouverture en lecture seule (O_RDONLY)
fd = fs.open-write(path); ouverture en écriture (O_WRONLY|O_CREAT|O_TRUNC, 0644)
fd = fs.open-file(path, flags, mode); ouverture avec indicateurs et permissions personnalisés
n = fs.read(fd, buf, n); lecture de bas niveau
written = fs.write(fd, data, n); écriture de bas niveau
ok = fs.close(fd); fermeture de bas niveau

; Gestion de fichiers
ok = fs.remove(path); supprime un fichier (unlink)
ok = fs.rename(old, new); renomme
ok = fs.symlink(target, linkpath); crée un lien symbolique
ok = fs.link(oldpath, newpath); crée un lien dur
ok = fs.copy-file(src, dst); copie le contenu d'un fichier

; Informations sur les fichiers
ok = fs.exists(path); vérifie si le chemin existe (suit les liens symboliques)
ok = fs.is-file(path); vérifie s'il s'agit d'un fichier ordinaire
ok = fs.is-dir(path); vérifie s'il s'agit d'un répertoire
sz = fs.stat-size(path); obtient la taille du fichier (renvoie ?i64)
sz = fs.file-size(path); identique à stat-size (renvoie ?i64)
sz = fs.fstat-size(fd); obtient la taille via fstat(fd), élimine TOCTOU (renvoie ?i64)

; Remarque : les fonctions d'obtention du mode/propriétaire/heure de modification du fichier sont dans le module `os`
;     (os.stat-mode / os.stat-uid / os.stat-gid / os.stat-mtime)
ok = fs.lstat(path); obtient les informations du lien symbolique (ne suit pas la cible du lien)

; Opérations sur les répertoires
dirp = fs.open-dir(path); ouvre un répertoire (renvoie un descripteur, 0 = échec)
name = fs.read-dir(dirp); lit l'entrée suivante du répertoire (renvoie ?str)
ok = fs.close-dir(dirp); ferme le descripteur de répertoire
names = fs.list-dir(path); liste tous les noms d'entrées (inclut . et ..)
names = fs.dir-entries(path); liste les entrées (exclut . et ..)
paths = fs.walk(root); parcourt récursivement l'arborescence

; Résolution de chemin
abs = fs.realpath(path); résout en chemin canonique absolu
target = fs.readlink(path); lit la cible d'un lien symbolique (renvoie ?str)

; Fichiers/répertoires temporaires
name, fd = fs.mkstemp(tmpl); crée un fichier temporaire (le modèle se termine par XXXXXX)
name = fs.mkdtemp(tmpl); crée un répertoire temporaire (le modèle se termine par XXXXXX, renvoie ?str)

; Fichiers spéciaux
ok = fs.mkfifo(path, mode); crée un tube nommé (FIFO)
ok = fs.mknod(path, mode, dev); crée un fichier spécial (nœud de périphérique)
ok = fs.truncate(path, length); tronque ou étend le fichier à la taille donnée
fs.sync(); vide les tampons du système de fichiers vers le disque
ok = fs.touch-file(path); met à jour les horodatages du fichier à l'heure courante
ok = fs.utime(path, atime, mtime); définit les heures d'accès et de modification
ok = fs.rmdir(path); supprime un répertoire vide

; Entrée standard
line = fs.get-line(); lit une ligne depuis l'entrée standard (?str, nil = EOF)

; Fonctions d'enveloppe pratiques (implémentées en Nolang dans fs.no, appellent les fonctions intégrées en interne)
content = fs.read-str(path); lit tout le fichier comme ?str (nil = vide, err = échec)
content = fs.read-bytes(path); lit tout le fichier comme ?[]byte (nil = vide, err = échec)
ok = fs.write-str(path, data); écrit une chaîne dans le fichier (écrase), renvoie ?bool
ok = fs.write-bytes(path, data); écrit un []byte dans le fichier (écrase), renvoie ?bool

; embed : système de fichiers intégré à la compilation (initialisé par #{embed='dir/path'})
;   #{embed='../frontend/dist'}
;   DIST fs.embed
;   data = DIST.read('index.html')   ; lit un fichier intégré, renvoie ([]byte, bool)
;   ok = DIST.exists('css/style.css') ; vérifie si un fichier intégré existe
embed {
    count i64
    paths str; tous les chemins concaténés (séparés par \0)
    pathStarts []i64; décalage de départ de chaque chemin dans paths
    pathLens []i64; longueur de chaque chemin
    blob []byte; contenu de tous les fichiers concaténé
    dataStarts []i64; décalage de départ de chaque fichier dans blob
    dataLens []i64; longueur de chaque fichier
}

; Spécifique à Windows (uniquement disponible sur win-amd64/win-arm64)
bufptr = os.win-find-first-file(path); FindFirstFileA, renvoie un descripteur (0 = échec)
name = os.win-find-next-file(bufptr); FindNextFileA, renvoie ?str (nil = plus d'entrées)
ok = os.win-find-close(bufptr); FindClose

; Constantes d'indicateurs de open() (spécifiques à la plateforme, valeurs macOS affichées)
O-RDONLY = 0
O-WRONLY = 1
O-RDWR = 2
O-CREAT = 512; macOS=512, Linux=64, Windows=256
O-TRUNC = 1024; macOS=1024, Linux=512, Windows=512
O-APPEND = 8; macOS=8, Linux=1024, Windows=8
O-EXCL = 2048; macOS=2048, Linux=128, Windows=1024
```

### env — Variables d'environnement (enveloppe simplifiée)

```no
val = env.get(key); renvoie ?str (nil = non trouvé)
val = env.lookup(key); renvoie ?str (nil = non trouvé)
env.set(key, val)
env.unset(key)
val = env.get-with-default(key)
ok = env.is-set(key)
```

### args — Arguments de ligne de commande

```no
n = args.count()
arg = args.get(i)
name = args.program()
ok = args.has-flag(name)
val = args.get-option(name)
arg = args.get-positional(i)
```

### path — Opérations sur les chemins

Encapsule une chaîne de chemin dans la structure `path` ; toutes les opérations sont fournies sous forme de méthodes :

```no
#{fmt-skip}
SEP = 47     ; '/' (ASCII)
DOT = 46     ; '.'

; Structure
path {
    p str
}

; Concaténation et décomposition de chemins (modifie .p sur place)
p = path{
    p: '/a/b/c.txt'
}
p.join(b str)           ; concatène deux chemins (modifie sur place)
p.base() (out)           ; prend le nom de fichier
p.dir()                  ; prend le répertoire (modifie .p sur place)
p.ext() (out)            ; prend l'extension
p.clean()                ; normalise (modifie .p sur place)
p.split() (f str)        ; divise en répertoire + nom de fichier (.p devient le répertoire, renvoie le nom de fichier)

; Tests sur les chemins
p.is-abs() (yes bool)    ; s'agit-il d'un chemin absolu

; Opérations sur le système de fichiers (délègue aux fonctions intégrées fs)
p.exists() (yes bool)        ; existe-t-il
p.is-dir() (yes bool)        ; s'agit-il d'un répertoire
p.is-file() (yes bool)       ; s'agit-il d'un fichier
p.size() (sz ?i64)           ; taille du fichier (nil = inexistant ou échec de stat)
p.make-dir() (ok bool)       ; crée un répertoire
p.remove() (ok bool)         ; supprime
p.rename(new-p str) (ok bool)    ; renomme
p.change-dir() (ok bool)     ; change le répertoire de travail

; Méthodes constructrices
path.current() (out path)    ; obtient le répertoire de travail courant
```

### bufio — Lecture tamponnée

```no
r = reader.init(fd, buf); initialise un lecteur tamponné (renvoie reader)
ok = reader.fill(); remplit le tampon
b = reader.read-byte(); lit un octet (?byte, nil = EOF)
line = reader.read-line(); lit une ligne (?str, nil = EOF ou erreur)
reader.close(); ferme
```

### io — Abstraction des entrées/sorties

Fournit les structures `io-reader` et `io-writer`, unifiant les opérations de lecture/écriture sur les fichiers, l'entrée/sortie standard, etc. :

```no
; Descripteurs de fichier standard
STDIN-FD = 0
STDOUT-FD = 1
STDERR-FD = 2

; Structure io-reader
io-reader {
    fd i64
}
r = io-reader.from-fd(fd); crée depuis fd
r = io-reader.from-stdin(); crée depuis l'entrée standard
read-n = r.read(buf, n); lit n octets
b = r.read-byte(); lit un octet (?byte, nil = EOF)
line = r.read-line(); lit une ligne (?str, nil = EOF)
total = r.read-all(buf, size); lit tout

; Structure io-writer
io-writer {
    fd i64
}
w = io-writer.from-fd(fd); crée depuis fd
w = io-writer.from-stdout(); crée depuis la sortie standard
w = io-writer.from-stderr(); crée depuis la sortie d'erreur
written = w.write(data, n); écrit n octets
written = w.write-str(s); écrit toute la chaîne
written = w.write-byte(b); écrit un octet
written = w.write-line(s); écrit une chaîne + un saut de ligne

; Fonctions pratiques
n = io.out(s); écrit sur stdout (sans saut de ligne)
n = io.outln(s); écrit sur stdout (avec saut de ligne)
n = io.err(s); écrit sur stderr (sans saut de ligne)
n = io.errln(s); écrit sur stderr (avec saut de ligne)
line = io.read-line(); lit une ligne depuis stdin (?str, nil = EOF)
```

### regexp — Expressions régulières

Encapsule un pattern dans la structure `regexp` ; le moteur d'expressions régulières est entièrement implémenté en Nolang (VM impératif + correspondance par retour en arrière), sans dépendre de regex.h de la bibliothèque C standard :

```no
; Structure
regexp {
    pattern str
}

; Méthodes
re = regexp {
    pattern: '^hello'
}
matched = re.matches(text); vérifie la correspondance
result = re.find(text); cherche la première sous-chaîne correspondante
```

Prend en charge la **syntaxe des littéraux regex** `/pattern/flags` (style JavaScript), désugarée en un appel à la fonction `regexp-compile` lors de la génération de code :

```no
; Littéral regex (écriture recommandée)
re = /\d+/
matched = re.matches('hello 123 world'); true

; Avec indicateurs
re = /[a-z]+/gi

; Appel explicite équivalent
re = regexp-compile('\\d+')
```

### process — Opérations sur les processus

Fournit la création de processus, l'obtention des flux standard, l'attente de processus, l'interrogation d'informations sur les processus, etc. Utilise en interne POSIX fork/exec/pipe/waitpid :

```no
; Constantes de signal
SIG-TERM = 15
SIG-KILL = 9
SIG-INT = 2
SIG-STOP = 19
SIG-CONT = 18
SIG-CHLD = 17
WNOHANG = 1

; Structure
process {
    pid i64
    stdin-fd i64
    stdout-fd i64
    stderr-fd i64
    exit-code i64
    running i64
}

; Création de processus
p = process.new()
ok = p.start-with-opts(program, args, dir, input, merge-err); démarre un sous-processus (sans attendre)
ok = p.start(program, arg); méthode pratique : fork + exec, capture stdout

; Attente de processus
status = p.wait(); attend bloquamment la fin du sous-processus
status = p.wait-nohang(); interrogation non bloquante ; nil = encore en cours d'exécution

; Contrôle de processus
ok = p.kill(sig); envoie un signal
ok = p.terminate(); SIG-TERM
ok = p.force-kill(); SIG-KILL

; Opérations sur les flux standard
read-n = p.read(buf, n); lit depuis stdout
line = p.read-line(); lit une ligne (?str, nil = EOF)
content, n = p.read-all(); lit tout le stdout
written = p.write(data, n); écrit sur stdin
p.close-stdin(); ferme le tube stdin
p.close-stdout(); ferme le tube stdout
p.close-stderr(); ferme le tube stderr

; Informations sur le processus
pid = p.pid-of(); ID du sous-processus
code = p.exit-code-of(); code de sortie
yes = p.is-running(); est-il encore en cours d'exécution
pid = process.parent-pid(); ID du processus parent

; Cycle de vie
p.close(); ferme tous les tubes et attend

; Exécution ponctuelle multiplateforme (recommandée, paramètres plats) : fork/exec + capture stdout/stderr, avec délai et environnement
;   paramètres : program = chemin de l'exécutable, args = slice d'arguments, dir = répertoire de travail, input = contenu écrit sur stdin du sous-processus,
;         env = slice de variables d'environnement sous forme K=V, timeout = millisecondes (<=0 signifie sans limite), merge-err = true fusionne stderr dans out
;   sorties : out = stdout capturé (stderr fusionné selon merge-err), stderr = sortie stderr capturée, code = code de sortie (-2 = tué pour dépassement de délai, -1 = échec de démarrage), err = message d'erreur
out, stderr, code, err = process.cmd('echo', ['hello'], '', '', [], 0, false)

; Forme à structure d'options (recommandée, plus lisible) : démonstration de transmission de structure cmdopts entre modules
;   cmdopts est défini dans ce module ; l'appelant construit process.cmdopts{...} dans un autre module et le passe par référence,
;   illustrant la capacité de Nolang à transmettre des définitions de structure entre modules.
cmdopts {
    dir str; répertoire de travail ; chaîne vide = hérite du répertoire du processus parent
    stdin str; contenu écrit sur stdin du sous-processus ; chaîne vide = aucun stdin fourni
    env []str; variables d'environnement (["K=V", ...]) ; tableau vide = hérite de l'environnement du parent
    timeout i64; délai d'attente (millisecondes), <=0 = attente infinie
    merge-err bool; true = fusionne stderr dans le out capturé
}
o = process.cmdopts {
    dir: ''
    stdin: 'piped-data'
    env: ['K=V']
    timeout: 200
    merge-err: false
}

; exec-result { stdout str, stderr str, code i64 }
res = process.exec('echo', ['hello'], o); renvoie exec-result (res.stdout / res.code ...)

; Fonctions pratiques
code = process.shell(cmd); exécute une commande via le shell, renvoie le code de sortie
pid = process.spawn(cmd); génère un sous-processus, renvoie le pid
status = process.wait(pid); attend la fin du pid donné
content, code = process.new().output(program, arg); exécute et capture la sortie
```

### net — Opérations réseau

Fournit les capacités de programmation réseau TCP, notamment l'écoute serveur, la connexion client, l'envoi/réception de données, etc. Utilise en interne l'API socket POSIX :

```no
; Constantes réseau
AF-INET = 2
SOCK-STREAM = 1
SOL-SOCKET = 65535
SO-REUSEADDR = 4
BACKLOG = 128

; Structure listener
listener {
    fd i64
}

; Opérations d'écoute
l = listener {}
ok = l.listen(host, port); crée une écoute TCP (socket+setsockopt+bind+listen)
c = l.accept(); accepte une connexion (?conn, nil = aucune connexion)
l.close(); ferme le socket d'écoute
fd = l.fd-of(); obtient le fd

; Structure conn
conn {
    fd i64
}

; Opérations de connexion
c = conn {}
ok = c.dial(host, port); crée une connexion TCP (socket+connect)
written = c.send(data); envoie une chaîne
read-n = c.recv(buf, n); reçoit des données dans buf
line = c.recv-line(); reçoit une ligne (?str, nil = EOF, 4096 octets maximum)
content, total = c.recv-all(); reçoit tout jusqu'à la fermeture de la connexion
c.close(); ferme la connexion
fd = c.fd-of(); obtient le fd

; Fonctions pratiques
l = net.listen-on(host, port); crée un écouteur et commence à écouter (?listener)
c = net.dial-to(host, port); crée une connexion et compose le numéro (?conn)
```

### net/ip — Opérations sur les adresses IP

Fournit l'analyse, la validation, la conversion et la classification des adresses IPv4. Implémentation pure Nolang :

```no
; Constantes d'adresse par défaut
; 0.0.0.0
IP-ZERO

; 127.0.0.1
IP-LOOPBACK

; 0.0.0.0
IP-ANY

; 255.255.255.255
IP-BROADCAST

; Structure ip-addr
ip-addr {
    a i64
    b i64
    c i64
    d i64
}

; Analyse et conversion
ip = ip-addr {}
ok = ip.parse('192.168.1.1'); analyse depuis une chaîne
s = ip.to-str(); convertit en chaîne '192.168.1.1'
v = ip.to-u32(); convertit en u32 (big-endian)
ip.from-u32(v); crée depuis un u32

; Classification des adresses
yes = ip.is-loopback(); 127.0.0.0/8
yes = ip.is-private(); 10/8, 172.16/12, 192.168/16
yes = ip.is-zero(); 0.0.0.0
yes = ip.is-broadcast(); 255.255.255.255
yes = ip.is-multicast(); 224.0.0.0/4
yes = ip.is-link-local(); 169.254.0.0/16
yes = ip.is-class-a(); classe A (1~126)
yes = ip.is-class-b(); classe B (128~191)
yes = ip.is-class-c(); classe C (192~223)

; Comparaison et sous-réseaux
yes = ip.equal(other); comparaison d'égalité d'adresses
yes = ip.in-subnet(base, prefix-len); vérification d'appartenance au sous-réseau

; Fonctions pratiques
addr = ip.ip-parse(s); analyse rapide (?ip-addr, nil = invalide)
yes = ip.ip-is-loopback(s); test rapide de loopback
yes = ip.ip-is-private(s); test rapide d'adresse privée
```

### net/sse — Client Server-Sent Events

Prend en charge la réception de flux SSE conformes à la spécification W3C EventSource. Utilise en interne une connexion HTTP/1.1 longue durée, prend en charge HTTP en clair et HTTPS (TLS) :

```no
; Structure sse-event
sse-event {
    event str; type d'événement (par défaut 'message')
    data str; données de l'événement (les lignes data multiples sont jointes par \n)
    id str; ID de l'événement
    retry i64; millisecondes d'attente avant reconnexion (-1 = non défini)
}

; Structure sse-client
sse-client {
    fd i64; fd du socket TCP
    tls-c tls.conn; connexion TLS
    use-tls bool; utiliser TLS ou non
    connected bool; état de la connexion
    host str; nom d'hôte du serveur
    port i64; numéro de port
    path str; chemin de la requête
    last-event-id str; ID du dernier événement reçu
    recv-buf str; tampon de réception
    recv-buf-len i64; longueur des données dans le tampon
}

; Connexion et réception d'événements
client = sse.connect('http://host:3000/events'); renvoie ?sse-client
client: {
    nil -> print('connect failed')

    -> {
        ev = client.next-event(); renvoie ?sse-event (nil = EOF, err = erreur)
        ev: {
            nil -> print('connection closed')

            err -> print('error: ' - it.msg())

            -> print(ev.data)
        }
        client.close()
    }
}

; Autres méthodes
yes = client.is-connected(); vérifie l'état de la connexion
ok = client.reconnect(); se reconnecte (en utilisant last-event-id)
```

### net/http — Client HTTP/1.1

Fournit un client pour le protocole HTTP/1.1, prenant en charge les méthodes GET, POST, PUT, DELETE, PATCH, avec TLS optionnel :

```no
; Structures (définitions du module std/http ; les définitions internes au module ne prennent pas de préfixe de module, sinon elles sont lues comme un littéral struct)
request {
    method str
    url str
    body str
    headers [16]str
    header-count i64
}
response {
    status-code i64
    status-text str
    headers str
    header-names [32]str
    header-values [32]str
    header-count i64
    body str
}

; Fonctions pratiques
resp = http.get(url); requête GET (?http.response)
resp = http.post(url, body); requête POST (?http.response)
resp = http.put(url, body); requête PUT (?http.response)
resp = http.delete(url); requête DELETE (?http.response)
resp = http.patch(url, body); requête PATCH (?http.response)
resp = http.do(method, url, body); méthode personnalisée (?http.response)
resp = http.get-auth(url, user, password); GET avec authentification Basic (?http.response)
resp = http.post-auth(url, body, user, password); POST avec authentification Basic (?http.response)

; Utiliser un objet request
req = http.request {}
req.init('POST', url, body)
req.add-header('Content-Type', 'application/json')
ok = http.set-basic-auth(req, user, password); définit l'en-tête Basic auth (bool)
ok = http.set-bearer-auth(req, token); définit l'en-tête Bearer (bool)
ok = http.set-api-key(req, header-name, api-key); définit l'en-tête API-key (bool)
resp = http.do-req(req); envoie la requête (?http.response)

; Analyser et lire les en-têtes de la réponse
resp.parse-headers()
val = resp.get-header(name); interroge la valeur d'un en-tête (?str)
```

### net/http2 — Client HTTP/2.0 (RFC 7540)

Prend en charge l'analyse des trames HTTP/2 et la gestion des connexions, avec prise en charge du mode h2c prior knowledge :

```no
; Structure de trame
http2-frame {
    length i64
    frame-type i64
    flags i64
    stream-id i64
    payload str
}

; Structure de connexion
http2-conn {
    fd i64
    next-stream-id i64
    send-window i64
    recv-window i64
    initialized bool
    use-tls bool
}

; Connexion et requêtes
c = http2.connect(host, port); établit une connexion (?http2-conn)
resp = http2.do(method, url, body); envoie une requête (?http.response)

; Opérations sur les trames
frame = http2-frame {}
pos = frame.parse(data, pos); analyse une trame (?i64)
pos = frame.serialize(buf, pos); sérialise une trame
ok = c.send-frame(frame); envoie une trame
frame = c.recv-frame(); reçoit une trame (?http2-frame)
```

### net/http3 — Client HTTP/3.0 (RFC 9114)

Client HTTP/3 basé sur le protocole QUIC :

```no
; Constantes de méthode
HTTP3-METHOD-GET = 'GET'
HTTP3-METHOD-POST = 'POST'
HTTP3-METHOD-PUT = 'PUT'
HTTP3-METHOD-DELETE = 'DELETE'
HTTP3-METHOD-PATCH = 'PATCH'
HTTP3-METHOD-HEAD = 'HEAD'
HTTP3-METHOD-OPTIONS = 'OPTIONS'

; Fonctions pratiques
c = http3.connect(host, port); établit une connexion QUIC (?http3-conn)
resp = http3.send-request(c, method, path, host, body); envoie une requête (?http.response)
resp = http3.get(url); requête GET (?http.response)
resp = http3.post(url, body); requête POST (?http.response)

; Codage/décodage des en-têtes QPACK
buf, n = http3.encode-header(name, value)
buf, n = http3.encode-headers(names, values, count)
name, value, pos = http3.decode-header(buf, pos)
```

### net/ws — Client et serveur WebSocket (RFC 6455)

Prend en charge la communication full-duplex du protocole WebSocket, comme client ou serveur :

```no
; Structure de message
ws-message {
    opcode i64; 0=continuation, 1=text, 2=binary, 8=close, 9=ping, 10=pong
    data str
    fin bool
}

; Serveur
s = ws.listen-on(host, port); crée une écoute (?ws-server)
c = s.accept(); accepte une connexion (?ws-server-conn)
msg = c.recv(); reçoit un message (?ws-message)
ok = c.send-text(text); envoie du texte
ok = c.send-binary(data); envoie des données binaires
c.close()

; Client
c = ws.connect(url); se connecte au serveur (?ws-client)
msg = c.recv(); reçoit un message (?ws-message)
ok = c.send-text(text); envoie du texte
ok = c.send-binary(data); envoie des données binaires
c.close()
```

### net/tls — Client TLS 1.2/1.3 (implémentation pure Nolang)

Fournit des connexions chiffrées TLS, prenant en charge TLS 1.2 et 1.3 :

```no
; Connexion
c = tls.dial(host, port); établit une connexion TLS (?tls.conn)
n = c.send(data); envoie des données chiffrées (?i64)
n = c.recv(buf, n); reçoit des données déchiffrées (?i64)
c.close()
```

### net/client — Client TCP de haut niveau

Encapsule la structure `conn` et fournit des fonctions comme la reconnexion automatique :

```no
c = client.client-create(host, port); crée un client (?client)
ok = c.connect(host, port); se connecte
ok = c.reconnect(); se reconnecte
written = c.send(data); envoie
read-n = c.recv(buf, n); reçoit
line = c.recv-line(); reçoit une ligne (?str)
written = c.send-line(line); envoie une ligne (i64)
response = c.request(data); modèle requête-réponse (?str)
yes = c.is-connected(); état de la connexion
c.close()
```

### net/quic — Protocole QUIC (RFC 9000)

Fournit une implémentation du protocole de transport QUIC, utilisée comme couche de transport sous-jacente de HTTP/3 :

```no
c = quic.dial(host, port); établit une connexion QUIC (?quic.conn)
n = c.send(data, n); envoie des données
n = c.recv(buf, n); reçoit des données
c.close()
```

### net/server — Serveur HTTP

Fournit les fonctionnalités d'un serveur HTTP :

```no
s = server {}
ok = s.listen(host, port); commence à écouter
c = s.accept(); accepte une connexion (?server-conn)
s.close()
```

### net/dns — Résolution DNS

Fournit les fonctions d'interrogation DNS :

```no
ip = dns.resolve(host); résout un nom d'hôte (?str)
```

### net/url — Analyse d'URL

Fournit l'analyse et la construction d'URL :

```no
u = url.url-parse(url); analyse une URL
s = u.to-str(); convertit en chaîne
```

### net/cookie — HTTP Cookie

Fournit l'analyse et la gestion des cookies HTTP :

```no
c = cookie {}
c.parse(set-cookie-header)
s = c.to-str()
```

### net/multipart — Données de formulaire Multipart

Fournit l'analyse et la construction de multipart/form-data :

```no
; Écriture (construction de multipart/form-data)
w = multipart.multipart-create(); crée un écrivain (?multipart-writer)
w.write-field(name, value); écrit un champ
body = w.body-of(); obtient le body encodé
ct = w.content-type(); obtient le content-type (inclut boundary)

; Analyse
r = multipart.multipart-parse(content-type, body); analyse (?multipart-reader)
```

### net/hpack — Compression des en-têtes HPACK (HTTP/2)

Fournit le codage/décodage de l'algorithme HPACK, utilisé pour la compression des en-têtes HTTP/2 :

```no
; Opérations sur la table d'en-têtes
t = hpack.tables {}
t.init(); initialise les tables statique + dynamique
out = t.encode-header(name, value); code un seul en-tête (str)
names, values, count, ok = t.decode-headers(data); décode les en-têtes ([]str)
t.dyn-add(name, value); ajoute à la table dynamique
idx = t.find(name, value); cherche name/value complet
idx = t.find-name(name); cherche uniquement par name
name, value, ok = t.lookup(idx); cherche par index

; Codage/décodage des primitives entier/chaîne
out = hpack.encode-int(val, prefix-bits, prefix-byte)
val, next-pos = hpack.decode-int(data, pos, prefix-bits)
out = hpack.encode-str(s)
out, next-pos = hpack.decode-str(data, pos)
```

### net/proxy — Prise en charge des proxies

Fournit les connexions via proxy HTTP/SOCKS :

```no
c = proxy.dial(proxy-url, target-host, target-port)
```

### net/pool — Pool de connexions

Fournit la gestion en pool des connexions réseau, réutilisant les connexions pour améliorer les performances :

```no
p = pool {}
p.init(capacity)
c = p.get(); obtient une connexion du pool
p.put(c); rend la connexion au pool
p.close()
```

### net/unix — Sockets de domaine Unix

Fournit la communication par sockets de domaine Unix :

```no
fd = unix.unix-listen(path); écoute (renvoie fd)
fd = unix.unix-dial(path); se connecte (renvoie fd)
l = unix.unix-listen-on(path); crée un écouteur (?unix-listener)
c = l.accept(); accepte une connexion (?unix-conn)
```

---
