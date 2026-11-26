\---  
sidebar_position: 2  
\---

# Syntaxe


## Commentaires

Nolang prend en charge les marqueurs de **commentaires d'une seule ligne** et de **commentaires multilignes (de bloc)** :

- `;` — marqueur monoligne (le commentaire court jusqu'à la fin de la ligne)
- `;; <contenu>` — marqueur monoligne (lorsque `;;` est suivi d'un contenu sur la **même ligne**, le commentaire court jusqu'à la fin de la ligne, même sémantique que `;`)
- `;;\n` — commentaire multiligne (de bloc) : lorsque `;;` est **immédiatement suivi d'un retour à la ligne** (seuls des espaces blancs sont autorisés entre les deux), le mode multiligne est activé, jusqu'à ce qu'un autre `;;` suivi d'un retour à la ligne/EOF soit rencontré

```no
; Ceci est un commentaire
; Ceci est aussi un commentaire, de même sémantique
;; Ceci est toujours un commentaire d'une seule ligne (pas de retour à la ligne après ;;)
x = 1; commentaire en fin de ligne, court jusqu'à la fin de la ligne
x = 2;; commentaire monoligne en fin de ligne, même sémantique

;;
Ceci est un commentaire multiligne (de bloc)
Il peut s'étendre sur plusieurs lignes
Jusqu'à ce qu'on rencontre un ';;' isolé
;;

y = 3
;;
Le ;; final doit être suivi d'un retour à la ligne ou de la fin du fichier
pour être reconnu comme délimiteur de fin
;;
```

> **Règle de déclenchement multiligne :** `;;` ne doit être suivi que d'**espaces blancs** (espaces/tabulations) jusqu'à un retour à la ligne ou EOF pour activer le mode multiligne. Si `;;` est suivi d'un caractère non blanc quelconque sur la même ligne, il est traité comme un commentaire monoligne (jusqu'à la fin de la ligne).
>
> **Règle de fin multiligne :** Le `;;` final doit lui aussi être suivi d'un retour à la ligne ou de EOF (seuls des espaces blancs sont autorisés entre les deux). Un commentaire multiligne non refermé commente jusqu'à la fin du fichier.

> **Règle : une instruction par ligne — l'écriture de plusieurs instructions sur une seule ligne est interdite.**
> Le point-virgule `;` est un **marqueur de commentaire**, jamais un séparateur d'instructions ; la virgule `,` ne peut pas non plus joindre plusieurs instructions. Chaque instruction d'un même bloc `{ ... }` doit occuper sa propre ligne.
> Cette règle s'applique aussi aux exemples de code dans les commentaires : même là, n'utilisez pas `;` ou `,` pour placer plusieurs instructions sur une même ligne, afin de ne pas induire le lecteur en erreur.

```no
#{fmt-skip}
; ❌ Erreur : `;` utilisé comme séparateur d'instructions — tout ce qui suit `;` est un commentaire,
; y compris le `}` fermant du bloc. Le bloc ne se referme jamais et le code suivant est silencieusement écarté
; cond -> { print('X'); return }

; ❌ Erreur : virgule joignant plusieurs instructions
; h0 = 1732584193, h1 = 4023233417
; out = from-i64(v), out = from-u64(v)
; debug(msg), info(msg), warn(msg)
```

```no
; ✅ Correct : chaque instruction occupe sa propre ligne
cond -> {
    print('X')
    return
}

; ✅ Correct : chaque instruction occupe sa propre ligne
; h0 = 1732584193
; h1 = 4023233417
; out = from-i64(v)
; out = from-u64(v)
; debug(msg)
; info(msg)
; warn(msg)
```

> **Pourquoi `;` est-il particulièrement dangereux :** dans `cond -> { print('X'); return }`, la queue `; return }` est entièrement un commentaire ; le code réel ne reste que `cond -> { print('X')`. Le bloc ne se referme jamais, et toutes les instructions après le `}` orphelin (souvent tout le reste du corps de la fonction) sont avalées ; les diagnostics peuvent en plus pointer très loin de l'erreur réelle, ce qui rend le débogage très difficile.

## Types de données

Types de base

- byte
- bool ; minuscules uniquement
- char ; type caractère : un seul point de code scalaire Unicode (rune), stocké en i32. Entouré de guillemets doubles, ex. "中"
- str ; type chaîne, entouré de guillemets simples 'hello', ou chaîne brute entre backticks `contenu multiligne`
- txt ; type texte de longueur fixe, 256 octets fixes en interne (255 octets de données au plus), nécessite une annotation de type explicite, ex. t txt = 'abc'
- i8
- i16
- i32
- i64 ; type numérique par défaut, indépendant de l'architecture
- i128 ; entier signé 128 bits
- u8
- u16
- u32
- u64
- u128 ; entier non signé 128 bits
- f32
- f64

### Type char (point de code Unicode)

`char` représente **un seul point de code scalaire Unicode** (scalar value). Son type de stockage sous-jacent est **`i32`**, et non `i64` :

- **Plage valide** : `0 ..= 0x10FFFF` (soit `0 ..= 1114111`). La valeur maximale d'un point de code ne nécessite que 21 bits, que `i32` contient largement.
- **Erreur hors limites** : lorsqu'un littéral dépasse cette plage, une **erreur est émise directement** à la compilation (y compris `no vet`). Par exemple `c char = 0x110000` ✗, `c char = 1114112` ✗ ; `c char = 1114111` ✓.
- **Opérations numériques autorisées** : `char` **peut participer directement aux opérations entières**, par exemple `z = a + 25`, `u = ch - 32` (`ch` étant la variable d'itération de `for ch <- s`) sont des **écritures normales**, avec addition/soustraction sur la valeur du point de code. Les opérations se font toujours sur **une largeur de i32**, et le résultat est traité comme une valeur entière ordinaire ; pour cette raison, les `+ - * /` de `char` **ne renvoient pas par défaut `option<int>` comme la famille des entiers**, et ne déclenchent pas d'erreur de compilation « débordement entier non traité ». La seule chose à surveiller est la plage : le point de code maximal est `0x10FFFF` ; dans les contextes qui dépassent la sémantique `i32`/point de code (comme les décalages ou les compteurs accumulés), il est conseillé de convertir d'abord explicitement en `i32`/`i64` avant de calculer, pour rendre l'intention plus claire.
- **Relation avec `str`** : `s[i]` renvoie un `char` ; un `char` peut être converti implicitement en `str` (encodage UTF-8), voir [Chaînes — Conversion implicite](str.md).

> **Le « type sous-jacent » et la « conversion à l'appel » sont deux choses distinctes.** `char` est lui-même un `i32`. La **signature source** de la fonction intégrée `str_from_cp(cp char) -> ?str` a un paramètre `char` ; ce n'est que lors de l'**appel au runtime externe** que le compilateur **étend avec zéro** (`zext i32 → i64`) ce paramètre i32 pour correspondre aux paramètres IR de `@str_from_cp` — ce n'est qu'une conversion temporaire lors de la préparation des arguments, elle **ne change pas le type de `char`**. Analogie en C : `char c='a'; f((long)c);` : `c` fait lui-même 1 octet et est temporairement promu à l'appel.

Types conteneurs

- obj ; objet
- map ; table de correspondance
- arr ; tableau de longueur fixe
- vec ; tableau de longueur variable
- slice ; tranche (vue) — structure de données non autonome, doit être rattachée à arr/vec/str/txt

- \* ; pointeur — uniquement les déclarations FFI `#{c}` et la bibliothèque standard
- usize ; uniquement pour le FFI
- any ; type quelconque — uniquement la bibliothèque standard

Types avancés

- bigint
- err

## Alias de type et types union

Un alias de type crée un nouveau nom pour un type existant. La syntaxe avec le signe égal `name = type` est utilisée ; les alias simples comme les unions de plusieurs types sont pris en charge.

### Syntaxe

```no
; Type union : plusieurs types séparés par |
int = i8 | i16 | i32 | i64 | i128 | u8 | u16 | u32 | u64 | u128
float = f32 | f64
num = int | float
string = str | txt

; Alias de type simple
bytes = []byte
buf = [16]u8
```

> `int`, `float` et `num` sont définis dans `std/number`, `string` dans `std/str` — ce ne sont pas des mots-clés intégrés au compilateur, mais des types union écrits avec la même syntaxe d'alias par la bibliothèque standard.

### Référence chaînée des types union

Un type union peut référencer d'autres types union, formant une hiérarchie :

```no
int = i8 | i16 | i32 | i64 | i128 | u8 | u16 | u32 | u64 | u128
float = f32 | f64
num = int | float; num est l'union de int et float
string = str | txt; string unit directement deux types chaîne concrets
```

### Utilisation dans les fonctions

Les types union peuvent servir de paramètres et de valeurs de retour de fonction ; le compilateur effectue automatiquement la monomorphisation (monomorphization), générant une version distincte de la fonction pour chaque type membre :

```no
; Type de paramètre : union num
max = (a ..num) (r num) {
    r = a[0]
    n = len(a)
    i <- [1..n): {
        a[i] > r -> r = a[i]
    }
}

; Méthode définie sur le type union
num.sign = () (r num) {
    {
        . > 0 -> r = 1

        . < 0 -> r = -1

        -> r = 0
    }
}
```

`string` (= `str | txt`) peut également servir de paramètre et de valeur de retour :

```no
; Les arguments str comme txt peuvent être passés, chacun ayant sa propre version monomorphisée
take = (s string) (n i64) {
    n = len(s)
}

a str = 'hello'
b txt = 'hi'
n1 = take(a); take__str
n2 = take(b); take__txt
```

> Limites actuelles du corps des fonctions à type union (identiques pour `num` et `string`) : appeler une méthode membre (comme `s.len()`) sur un paramètre union dans le corps n'est pas encore pris en charge ; utilisez une fonction intégrée globale (`len(s)`, nombre d'octets) ou séparez en fonctions à types concrets. La déclaration de variables de type union (`s string = 'x'`) n'est pas non plus prise en charge. Attention aussi à ne pas définir un alias de méthode portant le même nom qu'une méthode membre (comme `string.len`) — il intercepterait l'appel `s.len()` et se redistribuerait récursivement vers lui-même dans son corps.

### Règles de détection

La syntaxe avec le signe égal est reconnue comme alias de type (et non comme affectation de variable) dans les cas suivants :

- `name = type | type | ...` : type union (contenant `|`)
- `name = []type` : type tranche
- `name = [N]type` : type tableau
- `name = ?type` : type optionnel
- `name = known-type` : alias de type simple, où `known-type` est un nom de type intégré (comme `i64`, `f64`, `bool`, `str`, etc.) ou le nom d'un alias de type déjà défini

## Déclaration de variables

```no
#{fmt-skip}
; Les variables n'ont pas de mot-clé
; i64、f64、byte、bool、byte、str peuvent omettre l'annotation de type
i = 1

; f64 contient un point au milieu
f = 1.0

; byte
b = 0x00

; i8 si le nom de variable et le type coïncident, l'annotation de type peut être omise
i8 = 3

; Une annotation explicite identique au type inféré est redondante (no vet signale tcpoxtfd, no fmt --fix=redundant peut la retirer automatiquement)
x str = ''
x = ''; équivalent, annotation redondante

; Valeur par défaut 0
; Une variable n'a pas besoin d'être déclarée à l'avance
u16 u16

; str entouré de guillemets simples
name = 'nolang'

; bool true/false entièrement en minuscules
flag = true
flag = false

; bool abrégé : !! équivaut à true, ! équivaut à false (littéraux autonomes)
flag = true; true
flag = false; false

; Affectation de variable
; Noms identiques interdits ; si le nom est identique, il est traité comme une modification de la variable
name = 'hello'
name = 'world'

; Concaténation de chaînes
greeting = 'hello, ' - name

; Chaîne brute (entourée de backticks, multiligne, sans échappement)
sql = 'SELECT id,name
FROM user
WHERE id > 100'

; Annotation de type explicite
a u64 = 10

; Inférence de type des littéraux hexadécimaux
; - Littéral entier décimal (comme 771) inféré comme i64 (type entier par défaut)
; - Littéral hexadécimal (comme 0x0303) inféré comme byte (u8)
; - Si la valeur hex dépasse la plage de byte (> 255), une annotation de type explicite est obligatoire :
PORT i64 = 0x0303; correct : i64 explicite, valeur = 771

; PORT = 0x0303          ; erreur : inféré comme byte, valeur tronquée !
; PORT = 771             ; correct : le décimal vaut i64 par défaut
; - Les littéraux hexadécimaux peuvent utiliser des minuscules (0x00ff plutôt que 0x00FF)
; - Conseil : utilisez le décimal pour les constantes entières générales ; le hex avec annotation i64 explicite pour les constantes de protocole/masque de bits

; Caractère (guillemets doubles = char/rune, caractère unique)
c = "中"

; Type byte
b = 0x00

; arr tableau de longueur fixe
arr [3] = [1, 2, 3]

; vec tableau dynamique (tranche)
vec = [4, 5, 6]

; Type explicite (tranche)
typed []u8 = [1, 2, 3]

; Tableau
typed [3]u16 = [1, 2, 3]

; Inférence automatique de la longueur (i64)
a[nil] = [1, 2, 3]
```
```

## Chaîne brute (Raw String)

Une paire de backticks `` ` `` déclare une chaîne brute, de type `str`.

### Syntaxe

```
sql = `
SELECT id,name
FROM user
WHERE id > 100
`
```

### Contraintes de format strictes

1. Le backtick **d'ouverture** `` ` `` doit être **immédiatement suivi d'un retour à la ligne source** (LF ; les CRLF des fichiers Windows sont également acceptés) ;
2. Le backtick **de fermeture** `` ` `` doit **occuper une ligne à lui seul**, cette ligne n'autorisant que des espaces blancs + le backtick final ;
3. Les lignes des backticks de début et de fin **ne comptent pas dans le contenu de la chaîne**, elles servent uniquement de délimiteurs ;
4. **Règle d'échappement** : tous les `\`, `\n`, `\t`, `\'`, `\"` internes sont **conservés tels quels**, sans aucune interprétation d'échappement ;
5. Les **retours à la ligne et les espaces d'indentation du code source sont conservés intégralement** dans les octets de la chaîne ; exception : le CR (`\r`) est **toujours écarté**, si bien qu'un fichier CRLF et un fichier LF donnent une valeur octet pour octet identique ;
6. **Impossible d'intégrer directement un backtick** ; pour en inclure un, utilisez une concaténation de chaînes ordinaires entre guillemets simples.

### Exemple

```
; Le contenu est "SELECT id,name\nFROM user\nWHERE id > 100\n"
; Les caractères d'échappement \n \t sont conservés tels quels, non interprétés
raw = `
line1\nline2
\ttabbed
`
```

## Littéral regex (Regex Literal)

Nolang prend en charge la syntaxe des littéraux regex de style JavaScript `/pattern/flags`, utilisée pour créer une instance `regexp` déjà compilée.

### Syntaxe

```no
; Littéral regex de base
re = /\d+/

; Avec indicateurs (flags)
re = /hello/gi

; Classes de caractères, ancres, quantificateurs
re = /[a-z]+/
re = /^hello.*world$/

; Barre oblique échappée
re = /a\/b/
```

### Indicateurs

| Indicateur | Signification |
| --- | --------- |
| `g` | correspondance globale |
| `i` | insensible à la casse |
| `m` | mode multiligne |
| `s` | `.` correspond aussi aux retours à la ligne |

Les indicateurs sont optionnels ; ils suivent le `/` final et sont composés de lettres ASCII.

### Analyse lexicale sensible au contexte

`/` est à la fois l'opérateur de division et le début d'un littéral regex ; Nolang utilise, comme JavaScript, une analyse lexicale **sensible au contexte** pour les distinguer :

- **Position de début d'expression** (début d'instruction, après `=` / `(` / `[` / `{` / `,` / `:` / `;`, etc.) → `/` commence un littéral regex
- **Position de production de valeur** (après un identifiant, un littéral, `)` / `]` / `}`, etc.) → `/` est l'opérateur de division
- `//` est toujours un commentaire de ligne (priorité maximale)

```no
; Littéral regex (après =, position de début d'expression)
re = /\d+/
result = match-text(/[a-z]+/, text)

; Opérateur de division (après un identifiant, position de production de valeur)
ratio = 100 / 4
x = a / b
```

### Désucrement

Les littéraux regex sont désugarés, lors de la génération de code, en un appel à la fonction `regexp-compile` de la bibliothèque standard :

```no
; Code source
re = /\d+/

; Équivalent après désucrement
re = regexp-compile('\\d+')
```

`regexp-compile` est défini dans `std/regexp.no` ; il crée la structure `regexp` et appelle `.compile()` pour compiler le motif.

### Exemples d'utilisation

```no
; Créer une regex et tester la correspondance
re = /\d+/
matched = re.matches('hello 123 world')
print(matched); true

; Chercher une correspondance
re = /[a-z]+/g
result = re.find('hello 42 world')
print(result); "hello"

; Comme paramètre de fonction
result = match-text(/\d+/, text)
```

> **Attention :** un pattern vide `//` entre en conflit avec le commentaire de ligne (comportement identique à JavaScript). Pour une correspondance vide, utilisez `/(?:)/`.

## Règles de nommage

Les noms de variables, de fonctions, de structures, etc. peuvent commencer par un tiret bas `_`, suivi de tirets, de lettres et de chiffres ; ils ne peuvent pas commencer par un chiffre, ni se terminer par un tiret, ni contenir plusieurs tirets consécutifs.

**Règles de casse (à respecter obligatoirement) :**

- **Constantes globales, variables globales** : **doivent** commencer par une lettre majuscule (ex. `NOLANG`, `MAX-SIZE`, `HEX-CHARS`). Les variables globales privées commencent par un tiret bas, suivi là aussi d'une majuscule (ex. `_NOLANG`, `_PRIVATE-CONST`).
- **Variables locales, paramètres de fonction** : utilisent des minuscules (ex. `hex-chars`, `data-len`). **N'utilisez pas** de préfixe `_` — une variable locale est déjà privée par sa portée, aucun marqueur de visibilité n'est nécessaire. Le préfixe `_` est réservé aux variables globales privées et aux déclarations FFI privées.
- **Noms de fonctions, noms de structures** : utilisent des minuscules (ex. `sha1-block`, `db-mysql`)

**Conventions de nommage des fonctions (fortement recommandées) :**

- **Ne préfixez pas le nom de fonction par le nom du module.** Les fonctions d'un module n'ont besoin que d'un nom court et intuitif ; le préfixe de module est ajouté automatiquement par `ShortName.` lors des appels inter-modules. Par exemple, définissez la fonction d'entrée `tail` dans `tail.no` plutôt que `tail-run`, et la fonction auxiliaire `atoi` plutôt que `tail-atoi`. Le code est ainsi plus concis, et un appel inter-module comme `tail.tail()` plus intuitif.
- **La fonction d'entrée** devrait utiliser directement le nom du module lui-même (ex. `ping.no` → `ping`, `cat.no` → `cat`). Un import inter-module prend la forme `# /src/tail.tail`.
- **Évitez les mots-clés** : `awy` (mot-clé de groupe de coroutines), `match` (mot-clé de correspondance conditionnelle), etc. ne peuvent pas servir de nom de fonction. `run` a été supprimé et peut servir d'identifiant ordinaire.
- **`main` est un nom réservé, impossible à utiliser comme nom de fonction** : le point d'entrée du programme Nolang est **synthétisé automatiquement par les instructions de premier niveau** (voir `hir2mir.synthesizeMainForTopLevel`) ; il est donc inutile et **interdit** de définir explicitement `main`. Dès qu'apparaît `main = (...) { }` — y compris la méthode `recv.main` ou un `main` imbriqué dans une autre fonction — le compilateur signale directement une erreur (`'main' is a reserved name and cannot be used as a function name`). Pour servir d'entrée, écrivez simplement les instructions au premier niveau.

> **Les variables globales doivent commencer par une majuscule** : c'est une règle impérative, pas une convention. Une variable de premier niveau commençant par une minuscule est traitée par le compilateur comme une variable locale, ce qui peut provoquer des erreurs de référence non définie, etc.

```no
; ✅ Correct : les données globales utilisent des majuscules
NOLANG = 'nolang'
MAX-SIZE = 1024
HEX-CHARS = '0123456789abcdef'

; ✅ Variable globale privée : tiret bas initial, puis majuscule obligatoire
_NOLANG = 'nolang'
_PRIVATE-CONST = 42
```

```no
#{fmt-skip}
; ❌ Erreur : une variable globale ne peut pas commencer par une minuscule
; x1 = 10
; x = 10
; foo-bar = 42
; hello-world = 'Hello World'
```
```

```no
#{fmt-skip}
; ✅ Variables locales (dans une fonction) en minuscules, sans préfixe _
; fn-example = () {
;     x1 = 10
;     x = 10
;     foo-bar = 42
;     hello-world = 'Hello World'
;     ; ❌ Erreur : une variable locale n'a pas besoin de préfixe _
;     ; _x = 10
; }
```
```

### Éviter les variables globales dans les modules

**Fortement recommandé : sauf nécessité, n'utilisez pas de variables globales dans un module (fichier `.no`).** Les variables globales posent les problèmes suivants :

- **Risque de bug du compilateur** : le compilateur Nolang présente des limites connues dans la gestion des adresses mémoire inter-fonctions des variables globales de type structure ; différentes fonctions peuvent voir des adresses différentes, entraînant des états incohérents.
- **Sûreté concurrency** : un état global mutable est difficile à suivre dans les scénarios de fork ou asynchrones, et provoque facilement des accès concurrents.
- **Testabilité** : un état global crée des dépendances implicites, rendant l'isolation des tests difficile.
- **Lisibilité** : les variables globales rendent le flux de données obscur ; le lecteur doit parcourir tout le module pour comprendre le comportement d'une fonction.

**Approche recommandée :**

1. **Privilégier les variables locales** : conservez l'état dans des variables locales à l'intérieur des fonctions, transmises via paramètres et valeurs de retour.
2. **Encapsuler l'état dans une structure** : organisez les états liés dans une structure, manipulée via des méthodes (le récepteur de méthode est une variable locale, à l'adresse cohérente).
3. **N'utiliser des variables globales qu'en cas de nécessité** : constantes au niveau module (immuables), ressources singleton (comme un tampon de journal global), etc.
4. **Les variables globales doivent être en majuscules** : règle impérative (voir ci-dessus) ; une variable de premier niveau commençant par une minuscule est traitée comme locale par le compilateur.

```no
#{fmt-skip}
; ❌ À éviter : utiliser une variable globale mutable dans un module
; g-conn = tls.conn {}
; g-buf = ' '
;
; fn-a = () {
;     g-conn.send(g-buf)   ; adresse d'une globale potentiellement incohérente entre fonctions
; }
```
```

```no
; ✅ Recommandé : utiliser des variables locales, état transmis via paramètres/valeurs de retour
fn-a = () {
    conn = tls.conn {}; variable locale, adresse cohérente
    buf = ' '
    conn.send(buf)
}

; ✅ Recommandé : encapsuler le flux complet dans une seule fonction, éviter la transmission d'état entre fonctions
serve-once = (listen-fd fd, body str) (ok bool) {
    ok = false
    client-fd = net.net-accept(listen-fd)
    conn = tls.server-init(client-fd); variable locale
    conn: {
        ok -> {
            c = it
            c.handshake()
            c.send(body)
            c.close()
            ok = true
        }

        -> fs.close(client-fd)
    }
}
```

> **Cas pratique** : la fonction `https-serve-once` de `std/net/tls.no` encapsule le cycle complet requête-réponse HTTPS (accept + handshake + recv + send + close) dans une seule fonction, conservant tout l'état TLS dans des variables locales, évitant ainsi le bug du compilateur lié aux adresses incohérentes des globales entre fonctions.

## Spécification de la documentation API

Le commentaire de documentation d'une fonction doit contenir les noms et types de tous les paramètres, ainsi que les noms et types des paramètres de retour.

**Règles :**

- Le commentaire de documentation au-dessus d'une définition de fonction doit énumérer le nom et le type de chaque paramètre, ainsi que le nom et le type des paramètres de retour.
- Le résumé API en tête de module doit lui aussi utiliser la signature complète (avec noms de paramètres, types, noms de retour, types) ; n'utilisez pas de forme abrégée.

```no
#{fmt-skip}
; ❌ Erreur : types manquants, nom du paramètre de retour manquant
; sha1(data) (hash)
; sha1-block(s, h0..h4)
```
```

```no
; ✅ Correct : noms des paramètres, types, nom et type du paramètre de retour inclus
; sha1(data []byte) (hash [20]byte) — empreinte complète
; sha1-block(s []u32, h0 u32, h1 u32, h2 u32, h3 u32, h4 u32) — traite un seul bloc

; Les commentaires de documentation au-dessus d'une définition doivent suivre la même règle :
; sha1 : calcule l'empreinte SHA-1
; data []byte : tableau d'octets d'entrée
; retour hash [20]byte : valeur d'empreinte de 20 octets
sha1 = (data []byte) (hash [20]byte) {
    ...
}
```

### Commentaires de documentation traduisibles (liens Markdown)

Un commentaire de documentation peut se terminer par un **lien Markdown autonome**, qui fait pointer tout le commentaire vers une entrée du répertoire de traduction :

```no
; get : obtient le i-ème paramètre
; délègue au builtin os.arg(i) (lit argv[i] de main) ; nil hors limites
; [get](#args.get)
get = (i i64) (val ?str) {
    val = os.arg(i)
}
```

**Règles :**

- Le lien doit être la **dernière ligne** du groupe de commentaires, sur une ligne à lui seul ; toutes les lignes au-dessus forment un tout, traduit comme un seul message.
- La cible doit être une ancre commençant par `#` (une entrée du même répertoire). Les liens externes comme `[explication](https://example.com)` sont des liens ordinaires à destination humaine, ils ne sont **pas** traités comme des marqueurs de traduction.
- L'ancre est la clé (`args.get`). Elle est spécifiée par l'auteur et vue par le traducteur dans le répertoire ; elle n'est pas dérivée du texte — ainsi, reformuler le libellé n'invalide pas une traduction existante. L'ancre autorise lettres, chiffres, `.`, `_`, `-`, et est globale ; il est conseillé de la préfixer par le nom du module.
- Le préfixe utilise le **nom de module aplati**, pas le nom du répertoire. Le compilateur aplatit un fichier std vers son nom de fichier : pour `std/collection/linked-hash-map.no`, le module est `linked-hash-map`, et `use std/collection` n'ajoute pas de préfixe `collection.` aux symboles. L'ancre de `linked-hash-map.clear` est donc `linked-hash-map.linked-hash-map.clear` (nom de module + nom tel quel), et **non** `collection.linked-hash-map.linked-hash-map.clear`. Les segments de répertoire `collection` / `net` / `crypto` / `archive` / `encoding` / `database` n'appartiennent à aucun symbole et ne doivent pas figurer dans l'ancre.
- Le texte entre crochets (`[get]`) n'est qu'une indication pour qui lit le code source ; la ligne entière est retirée à l'affichage ; `[](#args.get)` est également valide.
- Seuls les commentaires `;` monolignes peuvent porter un lien (`;;` et `;;block` ne comptent pas).

Les traductions sont stockées dans `locales/<langue>-doc.noml`, avec l'ancre comme clé. En l'absence de traduction, on retombe sur le texte original du commentaire, jamais sur du vide. Le nom de fichier suit les règles de nommage `.no` (lettres minuscules, chiffres, tiret simple) ; l'étiquette elle-même suit ce format : `--lang=zh-tw` lit `zh-tw-doc.noml`.

Dans la bibliothèque standard, chaque fonction et méthode commentée porte déjà une ancre (environ 2000 entrées) ; dès qu'une traduction est déposée, le LSP peut l'utiliser immédiatement. `make i18n` n'écrit dans le répertoire que les entrées **déjà traduites**, sans y insérer le texte original comme traduction — sinon le répertoire anglais contiendrait à nouveau du chinois. `go run ./i18n/cmd/genen -doc-missing` liste les ancres non encore traduites.

**Astuce LSP :** survolez un nom de fonction pour voir l'explication complète traduite ; survolez la ligne du lien pour voir le contenu pointé par l'ancre (lui aussi dans la langue courante), pratique pour vérifier la traduction sur place.

## Privilégier la bibliothèque standard

La bibliothèque standard Nolang offre de nombreuses fonctionnalités courantes : manipulation de chaînes, conversions d'octets, calcul d'empreintes, communications réseau, etc.

**Règle : si la bibliothèque standard fournit déjà la fonctionnalité, il n'est pas conseillé de la réimplémenter.** Le développeur doit consulter attentivement la documentation de la bibliothèque standard (`docs/docs/std/overview.md`) pour éviter de réinventer la roue.

```no
#{fmt-skip}
; ❌ Erreur : implémenter soi-même la conversion str → []byte
str-to-bytes = (s str) (out []byte) {
    n = s.len-bytes()
    i = 0
    (i < n) {
        out[i] = s[i]
        i = i + 1
    }
}
```
```

```no
; ✅ Correct : utiliser la méthode str.to-bytes() de la bibliothèque standard
data []byte = s.to-bytes()
```

Substitutions courantes de la bibliothèque standard :

- `str.to-bytes()` — chaîne vers tableau d'octets (remplace `str-to-bytes` écrit à la main)
- `[]byte.to-str()` — tableau d'octets vers chaîne (remplace `bytes-to-str` écrit à la main)
- `[n]t.to-vec()` — tableau de longueur fixe vers tranche (`[20]byte` → `[]byte`)
- `[]byte.to-hex()` / `[]byte.to-hex-lower()` — tableau d'octets vers chaîne hexadécimale
- `str.to-i64()` / `str.to-f64()` — analyse d'une chaîne en nombre
- `int.to-str()` / `float.to-str()` — nombre vers chaîne
- `std/crypto/sha1`, `std/crypto/sha256`, `std/crypto/sha512` — calcul d'empreintes

### Fichiers de configuration TOML (`toml`)

La bibliothèque standard `toml` assure l'analyse et la sérialisation de fichiers de configuration TOML 1.0. L'analyseur prend en charge les commentaires, les clés bare/quoted/dotted, `[table]`, `[[array-of-tables]]`, les chaînes basic/literal/multiline, les entiers (décimal/h exadécimal/octal/binaire), les nombres à virgule flottante, les booléens, les dates-heures, les tableaux et les inline tables. Les valeurs sont conservées comme littéraux TOML bruts, si bien qu'aucune information (tableaux, dates-heures, etc.) n'est perdue.

```no
cfg = toml.parse('title = "Nolang"\n[server]\nport = 8080\n[[server.backends]]\nname = "local"')

doc = toml.new()
doc.put('name', '"nolang"')
doc.put('server.port', '8080')
doc.put('enabled', 'true')
text = toml.stringify(doc)
```

API courantes :

- `toml.new()` — crée un `toml` vide
- `toml.parse(text)` — analyse du TOML, renvoie `?toml`
- `doc.put(key, raw-value)` — ajoute ou met à jour une valeur TOML brute ; les valeurs illégales sont refusées, les clés en doublon sont signalées par l'analyseur
- `toml.get(doc, key)` — obtient la valeur brute (`?str`)
- `toml.get-str(doc, key)`, `toml.get-i64(doc, key)`, `toml.get-f64(doc, key)`, `toml.get-bool(doc, key)` — obtiennent un scalar converti
- `toml.stringify(doc)` — sort le TOML ; les array-of-tables produisent un en-tête `[[...]]`

Le modèle de document actuel limite un fichier à 4 clés, 2 tables ordinaires et 2 array-of-tables ; une clé fait au plus 32 octets et une valeur au plus 128 octets ; au-delà, il faut fractionner le fichier de configuration ou laisser l'appelant gérer.

### Documents YAML (`yaml`)

La bibliothèque standard `yaml` analyse et produit des documents YAML selon le core schema 1.2, prenant en charge les mappings en bloc, les séquences en bloc, les collections de flux, les trois styles de guillemets pour les scalaires, les scalaires en bloc `|` / `>`, les commentaires, les multi-documents, les ancres et alias, la clé de fusion `<<`, les clés explicites, ainsi que l'inférence de type (null / bool / int / float / `.inf` / `.nan`).

```no
doc ?yaml = yaml.parse('name: Alice\nserver:\n  port: 8080\ntags:\n  - a\n  - b\n')
doc: {
    nil -> print('nil')

    err -> print(it.msg())

    -> {
        doc = it
        name = doc.find-str('name'); ?str
        port = doc.find-i64('server.port'); ?i64
        name: {
            ok -> print(it)

            -> print('')
        }
        print(doc.to-json())
    }
}
```

API courantes :

- `yaml.parse(text)` / `yaml.parse-all(text)` — analyse, renvoie `?yaml`
- `yaml.valid-of(text)` / `yaml.error-of(text)` — validation seule
- `it.find(path)` / `it.get(key)` — obtient un nœud enfant, renvoie `?yaml`
- `it.find-str(path)`, `find-i64`, `find-f64`, `find-bool` — obtiennent une valeur pratique, renvoient `?valeur`
- `it.to-json()` / `it.to-yaml()` — sérialisation

> Attention : les nœuds enfants sont renvoyés en `?yaml` et les fonctions d'aide à la valeur en `?valeur` ; déballez-les avec un match (`child = it.find(path)`, dont la branche `ok` lie `it` au nœud enfant et la branche par défaut traite le vide, chaque branche sur sa propre ligne).

## Nommage des fichiers

Les noms de fichiers `.no` (y compris les noms de dossiers) utilisent toujours un tiret `-` pour joindre les mots, **jamais le tiret bas `_`**. Cela s'aligne sur le style de nommage des identifiants Nolang (variables, fonctions, structures).

Le compilateur **vérifie strictement** cette règle dans les commandes `run` / `build` / `test` / `vet` / `fmt` / `ast` ; tout segment du chemin (nom de dossier ou nom de fichier sans l'extension `.no`) en infraction provoque une **erreur directe** :

- Caractères autorisés : lettres minuscules `a-z`, chiffres `0-9`, tiret `-`.
- Caractères interdits : tiret bas `_`, majuscules, et tout autre symbole spécial.
- Le tiret ne peut apparaître ni en début ni en fin de segment, ni être répété (`--`).

Par exemple `tmp/zz_stridx/r_index.no` est refusé à cause des tirets bas dans `zz_stridx` et `r_index` ; `tmp/zz-stridx/r-index.no` est conforme. Pour un chemin OS absolu (comme `/Users/me/proj/a.no`), seule la partie située sous le répertoire de travail courant est vérifiée ; les noms de dossiers parents (comme `Users`) ne sont pas signalés à tort.

```shell
✅ Recommandé :
utils/
├── string-helper.no
├── hash-table.no
└── http-client.no
```

```shell
❌ À éviter :
utils/
├── string_helper.no
├── hash_table.no
└── http_client.no
```

## Définition de fonctions

Une fonction peut &#x8F38;mettre un résultat via des **paramètres de résultat nommés** (named result parameters) — essentiellement  
le **sucre syntaxique des paramètres de sortie** (out-parameter) ; `return` (ainsi que son abrégé `***`, déconseillé pour l'instant) ne sert qu'à terminer anticipément, et ne peut pas être suivi d'une valeur.

La forme d'une définition de fonction Nolang est `name = (in-params) (out-params) { body }` : le  
deuxième groupe de parenthèses `(out-params)` déclare les paramètres de résultat nommés (sucre syntaxique). Le corps de la fonction leur  
assigne des valeurs (comme pour les paramètres d'entrée ordinaires, ce sont des types référence) ; **la variable qui reçoit réellement le résultat doit être définie à l'appel** —  
le nom du paramètre de résultat dans la définition n'est qu'un espace réservé ; le LHS de l'appelant (ou l'argument en fin de liste) détermine dans quelle  
variable le résultat atterrit :

```no
parse-line = (s str, max-fields ?i64) (fields []str) {
    ...; assigne des valeurs à fields dans le corps
}

; À l'appel, on définit la variable réceptrice (liaison LHS, type inféré de la signature)
fields = parse-line(line, nil)

; Plusieurs résultats sont liés dans l'ordre
a, b = swap(x, y)

; Ou forme à argument en fin de liste : res est passé comme paramètre de sortie supplémentaire
add1(5, 3, res)
```

Les fonctions Nolang présentent les caractéristiques suivantes :

- **Les paramètres de résultat nommés sont un sucre syntaxique** : sous-jacent, tout passe par des paramètres d'entrée (out-parameter) ; aucun nouvel objet de valeur de retour n'est créé, c'est donc sûr en interne.
- **Les paramètres d'entrée sont en lecture seule** : les scalaires sont passés par valeur, les types composites par référence en lecture seule ; il est interdit d'écrire dans un paramètre d'entrée ou ses sous-champs dans le corps.
- **Les paramètres de sortie sont accessibles en écriture** : l'appelant peut lier une variable existante à un emplacement de sortie, que la fonction modifie directement en mémoire ; il peut aussi ne pas la lier, et la fonction génère alors une nouvelle valeur à livrer.
- Les variables d'une fonction sont détruites automatiquement à sa sortie.
- L'appel doit fournir une variable réceptrice (LHS ou argument en fin de liste) ; dire « la fonction n'a pas de valeur de retour » est imprécis —  
  la formulation exacte est : le résultat est transmis via des paramètres de résultat nommés (sucre syntaxique de l'out-parameter), et lié par l'appelant.
- **Règle d'alias** : une référence d'entrée en lecture seule et un emplacement de sortie peuvent pointer vers le même objet. Tant que les écritures ont lieu uniquement dans la zone de sortie et que l'entrée est seulement lue, c'est légal ; le compilateur n'effectue pas de vérification statique d'alias.

### Lier directement un emplacement de sortie à une variable existante (éviter les variables intermédiaires)

L'emplacement de sortie chez l'appelant peut être **lié directement à une variable déjà existante** ; la fonction réécrit sa mémoire sur place (voir « paramètres de sortie accessibles en écriture » ci-dessus).  
Ainsi, lorsqu'une sortie ne sert qu'à mettre à jour un état existant, inutile de la recevoir d'abord dans une variable intermédiaire jetable avant de la réécrire :

```no
; ✅ Recommandé : le reste est réécrit directement dans r (r est aussi le récepteur de la méthode)
qsq, r = r.div-mod(modulus)
```

```no
; ❌ À éviter : msq n'est utilisé qu'une fois, purement transitoire
qsq, msq = r.div-mod(modulus)
r = msq
```

- **Le récepteur peut en même temps être la cible de sortie** : le récepteur `r` de `r.div-mod(modulus)` est une entrée en lecture seule (`self` implicite),  
  et `r` est lié au second emplacement de sortie `r bigint` ; selon la « règle d'alias », entrée en lecture seule + sortie en écriture est légal.  
  Mesuré (`no build` / `no vet` / `no fmt`) : `q, a = a.div-mod(bigint.from-i64(7))` après `a = bigint.from-i64(100)` donne `q = 14`, `a = 2`, exactement comme « recevoir d'abord `msq` puis `r = msq` » ;  
  cela vaut même si `r` est un paramètre de résultat d'une fonction externe (`mod-pow` de `src/std/bigint.no` a précisément cette forme).
- **Utiliser `_` pour écarter une sortie inutilisée** : si le quotient n'est jamais utilisé, `_, r = r.div-mod(modulus)` n'a même pas besoin d'introduire une nouvelle variable,  
  c'est la forme la plus concise (voir le placeholder `_` dans « Affectation multiple » plus loin).
- **Valable aussi pour une sortie unique** : `r = r._mul(r)`, `s = s.trim()`, `out = out.replace-str(...)` sont des mises à jour sur place déjà présentes dans la stdlib.
- **Le code existant légal n'a pas besoin d'être réécrit pour cela** : c'est une écriture recommandée pour le nouveau code, pas un ordre de refactorisation ; respectez la « règle d'or : ne modifiez pas le code légal »,  
  ne touchez pas à du code déjà correct pour des raisons de style.

### Valeurs par défaut des paramètres

**Non prises en charge.** `name type = expr` dans une liste de paramètres (ex. `foo = (a i64, b ?i64 = 10)`) est une **erreur de syntaxe** (`E_DEFAULT_PARAM`) : le compilateur n'analyse pas l'expression par défaut et ne comble aucune valeur par défaut à l'appel. Cela élimine l'ambiguïté d'« une déclaration, plusieurs arités légales » et évite l'explosion monomorphique due à la réduction répétée de l'expression par défaut à chaque appel.

Pour une sémantique optionnelle, déclarez le paramètre avec le type option `?T` ; l'appelant passe **explicitement** `nil` / `ok(...)`, et le corps traite le cas avec `match` :

```no
parse-line = (s str, max-fields ?i64) (fields []str) {
    limit = 1024
    max-fields: {
        ok(v) -> limit = v

        nil ->

        err ->
    }
    ...; limit est la borne
}

; L'appelant doit passer explicitement l'argument
fields = csv.parse-line(line, nil); repli 1024
fields = csv.parse-line(line, ok(256)); borne 256
```

```no
add = (a i64, b i64) (result i64) {
    result = a + b; retourne le résultat via le paramètre
    ...; instruction placeholder : ne fait rien, écartée entièrement au build (voir « Instruction placeholder »)
}

; Paramètres variables
add3 = (a ..i64) {
}

; Appel de fonction
sum = add(1, 2); sum == 3

; Fonction anonyme — et for ? avec paramètres ?
(a i64) {
    print(a)
}(10)

; Appel de fonction
add(a, b)

; Il peut aussi y avoir plusieurs valeurs de retour
a, b = swap(5, 3)
```

## Contrôle du flux

> **Syntaxe dépréciée (supprimée après la version n)** : `for { }` / `for cond { }` / `for init, cond, update { }` / `for i in [..) { }` / `match x { }` / `if/elif/else { }` peuvent encore être analysés mais émettent un avertissement de dépréciation. Veuillez utiliser la « **nouvelle syntaxe** » ci-dessous.

### Ancien / nouveau : comparaison

| Ancienne forme / forme suffixe | Forme préfixe (par défaut pour `format`) |
| -------------------------- | --------------------------------------- |
| `{ } (true)` boucle infinie | `!! { }` ou `true { }` |
| `{ } (cond)` boucle conditionnelle | `(cond) { }` (`()` vide est considéré comme false, non exécuté) |
| `{ } ()` non exécuté | `! { }` ou `false { }` ou `() { }` |
| `{ } * n` comptage constant | `n * { }` (le corps ne s'exécute pas quand N ≤ 0) |
| `for i=0, i<n, i++ { }` comptage | `n * { }` (comptage constant) ou `i <- [0..n): { }` (variable) |
| `for i <- [a..b] { }` plage | `i <- [a..b]: { }` |
| `for i in [a..b) { }` plage | `i <- [a..b): { }` |
| `match x { ... }` correspondance | `x: { ... }` |
| `if/elif/else { }` branche | `{ cond -> body }` (court-circuit, doit être enveloppé dans `{}`) |
| `continue` | `*` (abréviation temporairement déconseillée) |
| `break` | `**` (abréviation temporairement déconseillée) |
| `return` | `***` (abréviation temporairement déconseillée) |

### Boucle / While / for-in

> **Deux écritures équivalentes** : une même boucle prend en charge la forme préfixe `(cond) { }` et la forme suffixe `{ } (cond)`,  
> avec exactement la même sémantique ; seul l'ordre de la condition et du corps diffère. `no fmt` produit par défaut la **forme préfixe** ;  
> vous pouvez revenir à la forme suffixe avec `no fmt -loop-style=suffix` (les deux écritures garantissent l'idempotence du formatage).

```no
#{fmt-skip}
; === Forme préfixe (prefix, condition écrite avant le corps) ===

; Boucle infinie (condition toujours vraie) — les deux formes suivantes sont équivalentes
!! {
    ...
}
!! {
    ...
}

; Non exécuté (condition toujours fausse) — les trois formes suivantes sont équivalentes
! {
    ...
}
! {
    ...
}
! {
    ...
}

; Boucle conditionnelle (vérifie cond, exécute le corps si vrai)
(x == 1) {
    do-something()
}

; Nombre d'exécutions limité (comptage en forme préfixe)
10 * {
    do-something()
}

; Quand N ≤ 0, le corps ne s'exécute pas (0 fois ou un nombre négatif sont ignorés)
0 * {
    print('不會執行')
}
-3 * {
    print('也不會執行')
}

; On peut ajouter une étiquette (label écrit avant la condition/le comptage)
#1 (x == 1) {
    do-something()
}

; === Forme suffixe (suffix, condition écrite après le corps, ancienne forme) ===
!! {
    ...
}

; Non exécuté (des parenthèses vides représentent false)
! {
    ...
}

; Boucle conditionnelle
(x == 1) {
    do-something()
}

; Nombre d'exécutions limité
10 * {
}

; Quand N ≤ 0, le corps ne s'exécute pas (0 fois ou un nombre négatif sont ignorés)
0 * {
    print('不會執行')
}

-3 * {
    print('也不會執行')
}

; Syntaxe des intervalles (prend en charge les types numériques tels que i64/u8, arr, vec, str)
; 16 combinaisons au total : '[' inclut la borne gauche, '(' ne l'inclut pas ; ']' inclut la borne droite, ')' ne l'inclut pas.
; Borné (4 variantes) : les deux extrémités ont une valeur
i <- [a..b]: {
}
i <- (a..b]: {
}
i <- [a..b): {
}
i <- (a..b): {
}

; Sans borne supérieure (4 variantes) : borne droite omise, itération jusqu'à la valeur maximale du type
i <- [a..]: {
}
i <- [a..): {
}
i <- (a..]: {
}
i <- (a..): {
}

; Sans borne inférieure (4 variantes) : borne gauche omise, itération à partir de la valeur minimale du type
i <- [..b]: {
}
i <- [..b): {
}
i <- (..b]: {
}
i <- (..b): {
}

; Totalement non borné (4 variantes) : les deux extrémités omises, basé sur les valeurs min/max du type
i <- [..]: {
}
i <- [..): {
}
i <- (..]: {
}
i <- (..): {
}
i <- [5..0]: {
}
i <- abc: {
}

; Détection du sens à l'exécution : quand start > end, l'itération décroît automatiquement (pas de -1).
; Les quatre combinaisons de parenthèses prennent toutes en charge la décroissance :
;   [5..1]  → 5 4 3 2 1   fermé à gauche et à droite, décroissant
;   (5..1]  → 4 3 2 1     ouvert à gauche, fermé à droite, décroissant
;   [5..1)  → 5 4 3 2     fermé à gauche, ouvert à droite, décroissant
;   (5..1)  → 4 3 2       ouvert à gauche et à droite, décroissant
;   (3..0]  → 2 1 0       ouvert à gauche, fermé à droite, décroissance jusqu'à 0
; Quand start <= end, l'itération se fait en sens croissant (pas de +1).
;
; Les extrémités d'un intervalle non borné sont déterminées par le type statique de la variable :
;   [..] d'un i64 : de i64.MIN (-9223372036854775808) à i64.MAX (9223372036854775807)
;   [..] d'un u8  : de 0 à 255
;   [..) d'un i64 : de i64.MIN à i64.MAX - 1 (valeur maximale exclue)
;   (..] d'un i64 : de i64.MIN + 1 à i64.MAX (valeur minimale exclue)
;   (..) d'un i64 : de i64.MIN + 1 à i64.MAX - 1 (les deux extrémités exclues)
; Règle : '[' inclut la valeur minimale du type, '(' commence à la valeur minimale + 1 ;
;         ']' inclut la valeur maximale du type, ')' se termine à la valeur maximale - 1.

; ❌ Refus explicite
;   Les bornes de l'intervalle doivent être des entiers ; les expressions imbriquées ne sont pas prises en charge
;   for i <- [1.5..5.5] { }   ; erreur de compilation
;   for i <- [0..[1..5][0]] { } ; erreur de syntaxe

; ⚠️ Éviter l'ambiguïté de ...
;   L'opérateur de plage est .. (deux points), l'appel de méthode self est .len().
;   Écrits à la suite : [0.. .len()) → [0...len()), trois points qui ressemblent à un opérateur unique
;   (or l'abréviation de return est fixée à ***, temporairement déconseillée ; mais trois points à la suite perturbent tout de même la lecture).
;   Utilisez self.len() plutôt que .len() pour lever l'ambiguïté : i <- [0..self.len()): { }
;   (dans le corps d'une méthode, self et . sont sémantiquement équivalents)

; Boucle conditionnelle (nouvelle forme : préfixe (cond) { }, par défaut dans `no fmt` ; remplace l'ancienne forme for cond { })
; La forme suffixe { } (cond) lui est strictement équivalente (ancienne écriture)
; Dans la plupart des cas, on peut utiliser une boucle range-for : i <- [0..n): { }
(x == 1) {
    do-something()
}
```
```

### Sortir / passer / retour anticipé

> ⚠️ **Le groupe d'abréviations `*` (passer l'itération) / `**` (sortir de la boucle) / `***` (retour/terminaison) est temporairement déconseillé** — écrivez plutôt les mots-clés anglais
> `continue` / `break` / `return` ; `no fmt` ramènera systématiquement les abréviations prises en charge à la forme des mots-clés.

```no
i <- [0..10): {

    ; continue (passe à l'itération suivante, temporairement déconseillé)
    continue

    ; break (sort de la boucle, temporairement déconseillé)
    break

    ; return/terminate (retour anticipé, met uniquement fin à la fonction ; en cours de planification, temporairement déconseillé)
    break
    continue
}

; Il est recommandé d'utiliser les mots-clés anglais (cohérent avec C/Rust, plus facile à porter, et qui compilent et s'exécutent normalement) :
i <- [0..10): {

    ; équivalent à * (temporairement déconseillé)
    continue

    ; équivalent à ** (temporairement déconseillé)
    break

    ; équivalent à *** (temporairement déconseillé), met uniquement fin à la fonction de façon anticipée
    return
}

; Alias de mots-clés (entièrement pris en charge par le lexer, strictement équivalents aux mots-clés d'origine) :
;   ctn ≡ continue, brk ≡ break, rtn ≡ return ; `no fmt` conserve l'orthographe d'origine des alias
i <- [0..10): {

    ; équivalent à continue
    ctn

    ; équivalent à break
    brk
}

; équivalent à return, met uniquement fin à la fonction de façon anticipée
rtn
```

> ⚠️ **`return` (et sa forme abrégée `***`, temporairement déconseillée) ne peut s'employer que nu, sans valeur de retour.**  
> Les fonctions Nolang ne disposent d'aucun mécanisme de « valeur de retour » — le résultat est toujours transmis par affectation à un **paramètre de résultat nommé (out-param)** dans le corps de la fonction (voir « Définition de fonction »).  
> Par conséquent `return <valeur>`, `return(expr)` et `*** <valeur>` sont tous **interdits** ; le compilateur / le formateur / le LSP signaleront tous une erreur :
>
> - Compilateur (`no build`) : `Error: compilation error: parser errors: line L, column C: 'return' ne peut pas être suivi d'une valeur de retour ; … [E_GENERAL]`
> - `no vet` : `<file>:L:C: [ERROR] nolang-compile: 'return' ne peut pas être suivi d'une valeur de retour ; … [E_GENERAL]` (même format que les autres diagnostics, un par ligne)
> - `no fmt` : `format error: …` et sortie avec un statut non nul
> - LSP : affiche un diagnostic d'erreur rouge sur la ligne correspondante
>
> Écriture incorrecte :
>
> ```no
> has = (n i64) (r i64) {
>     n == 0 -> return 0        ; ❌ erreur du compilateur / formateur / LSP
>     r = n
> }
> ```
>
> Écriture correcte (affectez d'abord le paramètre de résultat, puis utilisez un `return` nu pour terminer de façon anticipée ; dans le corps d'une branche, les instructions multiples doivent chacune occuper leur propre ligne) :
>
> ```no
> has = (n i64) (r i64) {
>     n == 0 -> {
>         r = 0
>         return
>     }
>     r = n
> }
> ```
>
> ⚠️ Écrire sur une seule ligne `n == 0 -> { r = 0; return }` n'est pas une abréviation : `; return }` est entièrement un commentaire,
> le bloc ne se ferme donc jamais, et toutes les instructions suivantes (souvent le reste de tout le corps de la fonction) sont silencieusement absorbées par cette branche —
> en pratique, `no vet` ne signale aucune erreur et la fonction n'est jamais appelée.

## Canaux (`chan[T]`)

Un canal est une file de valeurs **bornée, extensible, dont l'envoi ne bloque pas et dont la réception attend** ; le récepteur consomme les valeurs une par une sous forme de `?T` à trois états.
Il ne conserve qu'un seul symbole syntaxique : `<-` (pour les autres usages, voir la traversée `i <- range:` dans « Contrôle du flux »).

```no
ch chan[i64] = with-cap(8)   ; capacité initiale explicite
buf chan[i64]                 ; déclaration nue : équivalent à with-cap(0), croît selon les besoins lors de l'envoi

ch <- 1                       ; envoi (le type de gauche doit être chan[T] pour qu'il y ait envoi)
n = ch.len()                  ; nombre d'éléments actuellement en attente
c = ch.cap()                  ; capacité initiale
ch.close()                    ; fermeture : n'accepte plus d'envois, les valeurs déjà présentes restent accessibles
```

### Réception : boucle à trois états

`v <- ch: { ... }` est une **boucle** qui lie à chaque tour le `?T` à trois états à `v` ; `break` permet de sortir plus tôt.
`ok` signifie qu'une valeur a été obtenue, `nil` que le canal est fermé et vidé, et `err` une **vraie erreur** (un handle zéro non initialisé — il n'y a rien à attendre) —
**les deux derniers mettent fin à la boucle** (les branches de la même itération s'exécutent normalement).

La règle la plus importante après `ok` : **une lecture à vide est une « attente », pas une « absence de valeur »**. Lorsque le canal est ouvert mais vide, le lecteur
se suspend jusqu'à ce que quelqu'un fasse un `send` ou un `close` :

- dans une coroutine (clone `-async` issu de `co`), il se met réellement en parking : le numéro d'état est écrit dans une frame sur le tas, la tâche s'inscrit sur la chaîne d'attente du canal puis rend la main à l'ordonnanceur (voir la section « Coroutines ») ;
- dans une fonction ordinaire, le parking est impossible ; la lecture **bloque** donc : elle actionne répétitivement la file de tâches prêtes jusqu'à ce qu'une valeur soit disponible.

Ainsi, « vider le canal » doit se terminer par `close()`, comme pour un `range` sur un canal en Go.

```no
ch chan[i64] = with-cap(2)
ch <- 10
ch.close()

<- ch: {
    ok -> print('got {it}')      ; it est l'élément de l'itération en cours
    nil -> print('closed')       ; on arrive ici après avoir vidé le canal
    err -> print('bad channel')  ; seul un handle de valeur zéro arrive ici
}
```

Pour ignorer la variable de boucle, écrivez `<- ch: { ... }` ; si la première instruction du body n'est pas un match sur `v`,
alors `v` est directement lié à **l'élément lui-même** (`T` et non `?T`), et seules les itérations `ok` exécutent le corps :

```no
i <- ch: {
    print(i)                     ; i est un i64, pas un ?i64
}
```

Pour distinguer vous-même les trois états tout en conservant la variable de boucle, faites un match au début du corps ; écrire uniquement les branches
(en omettant `v:`) est une abréviation synonyme — le critère repose sur les **noms de branches** : seules les présences de `ok` / `nil` / `err` comptent comme trois états :

```no
i <- ch: {
    ok -> print('got {it}')      ; it est l'élément de l'itération, i est le ?i64 de l'itération
    nil -> print('closed')
    err -> print('empty')
}
```

Le critère est syntaxique, non typé (voir « Détermination de la forme » ci-dessous). `no fmt` complète les abréviations ci-dessus en
sujet explicite `i: { ... }` ; les deux écritures peuvent être relues, le formatage est idempotent.

### Détermination de la forme

| Début du body | Forme | Type de `v` |
|---|---|---|
| `co bar(i)`, `print(i)` ou autre instruction ordinaire | élément | `T` |
| `v: { ... }` | trois états | `?T` |
| `ok -> ...` / `nil -> ...` / `err -> ...` | trois états (abréviation) | `?T` |
| Autre match nu (par ex. `i > 5 -> ...`) | élément | `T` |

La détermination doit avoir lieu **avant l'analyse du body** : si le début du bloc est un match, l'analyseur le convertit immédiatement en
chaîne de `if`, après quoi l'intention d'origine est méconnaissable. Cette case avait tout particulièrement cessé de fonctionner — `i <- ch:` était
toujours traité comme forme élément, les branches à trois états dégénérant en guards indépendants (tous exécutés à chaque tour), avec `it` lié à une valeur vide.

### Sémantique

| Aspect | Règle |
|---|---|
| Capacité | Le `n` de `with-cap(n)` est la capacité **initiale** ; une fois plein, le canal double de taille, l'envoi **ne bloque jamais et ne perd aucune valeur** |
| Lecture à vide | **Attente** : la coroutine se met en parking, la fonction ordinaire bloque en actionnant la file de tâches prêtes ; `err` est réservé aux vraies erreurs comme le handle zéro |
| Fermeture | Après `close()`, l'envoi interrompt le processus ; les valeurs déjà en file restent accessibles dans l'ordre, puis `nil` est signalé ; la fermeture réveille aussi tous les waiters |
| Charge utile | À l'envoi, **copie profonde** (`str` / `%vec` passent par clone) ; le récepteur obtient une copie indépendante — pas de crainte que la variable source soit modifiée ou libérée trop tôt |
| Concurrence | Le canal lui-même n'est pas verrouillé (ordonnancement coopératif monothread) : les waiters sont accrochés à la chaîne `waiters` du bloc de contrôle ; `send` / `close` détachent toute la chaîne et la remettent en file un par un ; **être réveillé ne signifie pas obtenir une valeur** — après reprise, il faut refaire le test de lisibilité, aussi le point de reprise de la coroutine est le bloc de parking lui-même |
| Blocage | File de tâches prêtes vide et tâche cible toujours inachevée ⇒ aucune progression possible : l'exécution affiche `nolang: deadlock: …` sur stderr et avorte, plutôt que de renvoyer une valeur que personne n'a envoyée ou de tourner à vide |

> Une conséquence directe de la suspension sur lecture à vide : **la boucle de vidage doit se terminer par `close()`**. Les versions antérieures signalaient « ouvert mais vide »
> comme `err`, ce qui faisait sortir naturellement la boucle et dispensait de fermer le canal dans une forme `for` ; désormais ce chemin est une attente,
> et un programme qui oublie `close()` reste bloqué sur la dernière itération (suspendu dans une coroutine, déclaré en blocage dans une fonction ordinaire).

### Limitations

- **Implémenté uniquement dans le backend LLVM.** `no build --js` / `--wasm` produisent une erreur explicite
  (`[chbkn001]`) dès qu'ils rencontrent un nœud de canal, plutôt qu'un code incomplet produit silencieusement.
- Le canal est un **type valeur** (un handle de la largeur d'un mot machine) ; il peut être passé comme paramètre, résultat ou élément ;
  `chan[chan[T]]` et `?chan[T]` peuvent être déclarés (pas encore couvert par `tests/chan.no`).

## Instruction placeholder (`...`)

`...` est une **instruction placeholder** du langage : syntaxiquement valide, mais sémantiquement **sans effet**. Elle sert principalement aux
exemples de documentation, aux squelettes de code et aux emplacements réservés `TODO`. À la compilation, l'instruction entière est supprimée dès la phase
AST→HIR, et le produit est strictement identique à « ne jamais avoir écrit cette ligne » (en pratique, le `sha256` du binaire produit par une même source
avec ou sans `...` est le même).

```no
parse-line = (s str) (fields []str) {

    ; TODO: il reste à gérer les caractères d'échappement ici
    ...

    fields = csv.parse-line(s)
}

add = (a i64, b i64) (result i64) {
    result = a + b
    ...
}
```

Règles :

- **Ne peut occuper qu'une ligne à elle seule** (un commentaire `;` peut suivre en fin de ligne). Les écritures comme `fail -> ...`, `x = ...` ou `[0...n)` 
  sont toutes des erreurs de syntaxe — `...` n'a pas de valeur et ne peut apparaître dans une position d'expression ni dans le reste d'une instruction.
- **Ce n'est ni un alias de `return`, ni un opérateur de plage** : la plage est `..` (deux points), et le retour anticipé s'écrit `return`.
- `no fmt` la conserve telle quelle, sans la supprimer ni la déplacer.

Toutes les écritures suivantes sont invalides :

```no
#{fmt-skip}
add = (a i64, b i64) (result i64) {
    result = a + b
    ...          ; ✅ seule forme valide : occupe une ligne entière
}

ok -> ...        ; ❌ erreur de syntaxe : `...` ne peut pas apparaître dans une position d'expression
x = ...          ; ❌ erreur de syntaxe : idem
i <- [0...n): {  ; ❌ erreur de syntaxe : `...` n'est pas un opérateur de plage, écrivez [0..self.n)
}
```

### Match

```no
; Forme simple, it sert à récupérer l'argument
x: {
    err -> log(it.msg()); it est l'erreur structurée err.err

    nil -> log('nil')

    -> do-right-thing(it)
}

; Forme par déconstruction
x: {
    err(e) -> log(e.msg())

    nil -> log('nil')

    ok(v) -> do-right-thing(v)
}

; Remarque : le filtrage par motif de struct (`user: { User{id=1} -> ... }`) et la liaison de champs ne sont pas encore pris en charge ; nous ne les illustrons donc pas

score: {
    [0..59] -> print('不及格')

    [60..89] -> print('良好')

    [90..100] -> print('優秀')

    -> print('分數非法')
}

; La syntaxe des intervalles compte 16 combinaisons, règle : '[' signifie inclure la borne gauche, '(' ne pas l'inclure ;
;                     ']' signifie inclure la borne droite, ')' ne pas l'inclure.
;
; 【Intervalles bornés】(4 variantes) — les deux extrémités ont une valeur concrète
;   [a..b]  → x >= a && x <= b   les deux extrémités incluses
;   [a..b)  → x >= a && x <  b   gauche incluse, droite exclue
;   (a..b]  → x >  a && x <= b   gauche exclue, droite incluse
;   (a..b)  → x >  a && x <  b   les deux extrémités exclues
;
; 【Intervalles sans borne supérieure】(4 variantes) — borne droite omise (End=nil), seule la borne inférieure est vérifiée
;   [a..)   → x >= a              gauche incluse, sans borne supérieure
;   [a..]   → x >= a              gauche incluse, sans borne supérieure
;   (a..)   → x >  a              gauche exclue, sans borne supérieure
;   (a..]   → x >  a              gauche exclue, sans borne supérieure
;
; 【Intervalles sans borne inférieure】(4 variantes) — borne gauche omise (Start=nil), seule la borne supérieure est vérifiée
;   [..b]   → x <= b              sans borne inférieure, droite incluse
;   [..b)   → x <  b              sans borne inférieure, droite exclue
;   (..b]   → x <= b              sans borne inférieure, droite incluse
;   (..b)   → x <  b              sans borne inférieure, droite exclue
;
; 【Intervalles totalement non bornés】(4 variantes) — les deux extrémités omises, basés sur les valeurs min/max du type
;   [..]    → true                min du type ≤ x ≤ max du type (les deux extrémités incluses)
;   [..)    → true                min du type ≤ x < max du type
;   (..]    → true                min du type < x ≤ max du type
;   (..)    → true                min du type < x < max du type
;
; Dans un match, un intervalle totalement non borné se base sur le domaine de valeurs du type de la variable filtrée (toujours vrai, équivalent à catch-all).
; Dans un range-for, il se base sur la plage de type de la variable d'itération (par ex. [..] d'un i64 va de i64.MIN à i64.MAX).
;
; Exemple : classification complète à l'aide d'intervalles non bornés
v = 85
result = v: {
    [..0) -> 'negative'

    [0..60) -> 'fail'

    [60..80) -> 'pass'

    [80..90) -> 'good'

    [90..100] -> 'excellent'

    (100..) -> 'extraordinary'
}
print(result)

num: {
    1 || 3 || 5 || 7 -> print('奇數小數')

    2 || 4 || 6 -> print('偶數小數')

    -> print('更大數字')
}

; Valeur de retour : la dernière instruction/valeur
result = x: {
    1 -> 1

    2 -> 2 + 1

    -> a + b
}

; Un corps d'arm sur plusieurs lignes doit utiliser des accolades -> { ... }
x: {
    nil -> {
        log('nil')
        do-cleanup()
        return
    }

    err -> {
        log(it.msg())
        do-cleanup()
        return
    }

    ok -> print(it)
}
```

> **Règle du corps d'arm sur plusieurs lignes** : lorsqu'un corps d'arm contient plusieurs instructions, il doit être entouré d'accolades `-> { ... }`. Un corps sur une seule ligne peut suivre directement `->`. En effet, si un corps sur plusieurs lignes n'utilise pas d'accolades, la liaison `it` du option match ne peut pas être insérée correctement, ce qui provoque une erreur de compilation.

> **Sémantique du match dans un corps for-in** : `i <- (a..b]: { 1 -> ... 2 -> ... }` exécute le corps du match une fois pour chaque variable d'itération `i` (`1 ->` équivaut à `i == 1 ->`, et ainsi de suite). C'est un sucre syntaxique qui exécute un match par itération.

#### Trois formes du sujet de match

Le sujet d'un match peut être une **valeur**, une **expression** ou un **bloc** :

| Forme | Écriture | Mode d'évaluation |
| --- | --- | --- |
| Valeur | `x: { ... }` | filtre directement `x` |
| Expression | `x + 1: { ... }` | calcule d'abord la valeur de l'expression puis filtre |
| Bloc | `{ ... }: { ... }` | exécute un corps d'instructions, filtre le résultat des `?=` qui s'y trouvent |

Les deux premières sont des **expressions** : elles peuvent servir d'instruction mais aussi fournir une valeur dans un contexte de valeur (`v = x: { ok -> it, -> 0 }`).
La troisième n'est **qu'une instruction** — ce n'est pas une expression, elle ne peut pas être affectée, n'a pas de valeur de fin de bloc, et `-> _` sert à jeter le résultat.
`no fmt` fait circuler `}: {` tel quel, et la règle des lignes vides entre arms est la même que pour un match ordinaire.

##### Sujet expression (`expr: { ... }`)

Dans `expr: { ... }`, le `: {` a une priorité **supérieure** à `=` : à droite d'une affectation, il constitue tout le RHS, et non
« d'abord terminer `r = <expr>`, puis poursuivre avec un match indépendant ». Le calcul optionnel peut donc être déballé en une seule étape :

```no
#{overflow=wrap}
rem = seconds % 3600: {
    ok -> it

    -> 0
}

#{overflow=wrap}
sec = rem % 60: {
    ok -> it

    -> 0
}
```

Cela équivaut à la forme en trois étapes qu'il fallait auparavant séparer : `rem-opt = seconds % 3600` / `rem = 0` /
`rem = rem-opt: { ok -> it  -> 0 }`. La première affectation peut porter un type explicite
(`rem i64 = seconds % 3600: { ... }`), et la réaffectation s'écrit directement `rem = ...: { ... }` ;
les deux sont des matchs à une seule position de valeur, dont le type de résultat est déterminé par l'expression de fin d'arm.

Sémantique et limitations :

- Lorsque le sujet est un calcul optionnel D6 (`+ - * / %`), le résultat est `?T`. Le match le normalise en
  un temporaire synthétique `__match_subj_L_C ?T` : le sujet est **évalué exactement une fois**, `ok -> it` lie le payload,
  la condition passe par une comparaison de tag option, et `-> 0` sert de repli pour `nil` / `err`. Le nom synthétique
  n'apparaît ni dans les diagnostics ni dans la sortie de `no fmt`.
- Le résultat du match est un **T simple** et est enregistré dans la table sémantique secondaire ; il est donc possible d'écrire
  ensuite `sec = rem % 60: { ... }` : des matchs consécutifs à position de valeur ne perdent pas le payload.
- Le match à position de valeur ne s'attache qu'en **contexte de valeur** (côté droit d'une affectation, de `?=` ou d'une affectation composée), et `: { ... }`
  doit être réellement un bloc de match. Les boucles conditionnelles au niveau instruction comme `cond: { body }` et les littéraux de struct
  empruntent toujours la voie d'analyse existante et ne sont pas concernés.
- **L'affectation à une cible de champ est aussi un contexte de valeur** (depuis le 2026-11-09) : dans `d.month = mp - 9: { ... }` et
  `b.n ?= b.n + 1: { ... }`, le `: {` se lie au **RHS**, pas à toute l'affectation. Auparavant, ces deux
  branches oubliaient de propager `CTX_EXPR` lors de l'analyse du RHS (les cibles identifiant passaient par `let` et le propageaient,
  mais `obj.f =` / `a[i] =` ne le faisaient pas) ; le `: {` était alors récupéré au niveau instruction et englobait toute l'affectation,
  analysée comme `match(sujet = (d.month = mp - 9))` : l'affectation s'exécutait comme avant et écrivait le `?T` implicitement déballé dans le champ,
  tandis que les deux arms `ok -> it` / `-> 0` devenaient du code mort. Désormais, la valeur des arms retombe réellement dans le champ.
- Une cible indexée (`a[i] = …: { ... }`) a la même forme, mais D9 interdit déjà l'écriture directe par indice ; cette écriture
  est donc toujours rejetée par le checker avec `idxwrite`. Il faut la réécrire en place-match (`a[i]: { ok -> it = v }`)
  ou avec `?=`.
- `no fmt` fait circuler `expr: { ... }` tel quel, sans le scinder en deux instructions (affectation + match indépendant).
- **Frontière de visibilité des types** : pour normaliser un sujet de calcul optionnel en `?T`, les types déclarés des deux opérandes doivent
  être visibles statiquement par le parser. Si un opérande provient d'une chaîne de méthodes profonde (dont le type de récepteur est impossible à déduire,
  même par rejeu de l'AST), ajoutez un type explicite à sa déclaration, par exemple `e i64 = flr.to-i64()` ; sinon la normalisation est
  prudemment ignorée, `ok -> it` n'a aucune liaison, le MIR remplit à zéro et la valeur observée reste à 0. C'est le cas de
  `f64.to-str-general` dans `src/std/number.no` : sans cette annotation, `fmt.fmt-f64(2.5, '')` régresserait de
  `'2.5'` à `'3'`. La régression est épinglée dans
  `src/parser/value_match_expr_subject_test.go::TestValueMatchExprSubjectASTAnnotatedOperand`
  (retirer l'annotation fait échouer le test).

`src/std` a converti, le 2026-11-09, **122 sites répartis sur 22 modules** en cette écriture en une seule étape :
outre la forme initiale en trois étapes (`x-opt = expr` / `x = 0` / `x = x-opt: { ... }`), cela couvre aussi la forme en deux étapes
(sans initialisation à zéro), la première affectation avec type explicite (`x i64 = expr: { ... }`) et les cas où la variable
intermédiaire est déclarée plusieurs fois par paires dans la même fonction. Après repli, le sujet peut être un appel de méthode,
un index ou un calcul optionnel ; `ok -> it` / `ok(v) -> v` / les arms constants / `-> _` se sont tous révélés équivalents en pratique, et `no fmt` est idempotent sur ces sites.
L'inférence de types du compilateur gère également les valeurs résiduelles de même nom global comme « `?T` trouvé dans la table sémantique secondaire, mais déclaration réellement `i64` » :
`ok -> it` ne perd pas sa liaison pour autant.

Les cas conservés (9 sites) : ① le match interne réécrit la variable `-opt` elle-même (cette variable est le sujet d'un match à position de valeur ultérieur) ;
② deux niveaux de matchs option chaînés ; ③ un match à position de valeur qui distribue selon la valeur (comme errno → message, et non un déballage d'option) ;
④ il reste d'autres instructions entre la déclaration et le match, ou la variable intermédiaire est un résultat accumulé entre plusieurs branches
(le repli modifierait le moment de l'évaluation).
##### Sujet bloc (`}: {`)

Le sujet bloc enveloppe un ensemble d'instructions afin qu'une **fonction sans paramètre de résultat `?T`** puisse traiter sur place la propagation de `?=` :

```no
; d i64 : diviseur, quand vaut 0, 6/d donne err
f = (d i64) {
    {
        v ?= 6 / d; l'échec n'exige plus que la fonction ait un paramètre de résultat ?T
        print(v.to-str()); en cas d'échec, ceci ne s'exécute pas
    }: {
        err -> print(it.msg())

        nil -> print('nil')

        -> _; on jette le résultat
    }
    print('之後照常執行'); le flot reprend une fois les arms terminés
}
```

Sémantique :

- Les `?=` dans le corps sont capturés par le match de bloc **le plus proche** (le plus interne en cas d'imbrication ; même si la fonction concernée a
  un paramètre de résultat `?T`, c'est le match de bloc qui est prioritaire). La capture est récursive : les blocs nus, les corps de if, les corps de boucle
  et les `?=` dans les arms de match comptent ; une définition de fonction est une frontière, un littéral de fonction ne l'est pas.
- Ce qui est filtré est le `?T` à trois états, avec la **vérification d'exhaustivité** du match option standard : sans arm de repli,
  même si `ok ->` est inatteignable à l'exécution, il faut compléter les trois arms `nil` / `err` / `ok`.
- Le corps se déroule normalement (zéro `?=` ou tous réussis) ⇒ `nil` est filtré ; si un `?=` échoue ⇒ c'est son
  `nil` ou son `err` qui est filtré (le `it` de l'arm `err` est l'`err.err` complet, voir « Type de `it` »).
- **Un seul échec arrête tout le corps** : les instructions suivantes et les itérations de boucle suivantes ne s'exécutent plus, les arms ne tournent qu'une fois.
  C'est un « bloc d'arrêt », pas un `return` de fonction ; vos propres liaisons `return` / `break` / `continue`
  ne sont pas affectées.
- Le dépliage est entièrement composé de l'AST noyau existant (variable locale `?T` synthétique + chaîne de gardes `if` + match ordinaire),
  sans nouveau nœud HIR / MIR. Le nom de la variable synthétique n'apparaît ni dans les diagnostics ni dans la sortie de `no fmt`.

> Division du travail avec la capture sur place par `=` : `=` **stocke l'échec dans la variable cible** et le flot continue ; `}: {`
> **le réduit à un seul match** et arrête le reste du corps. Quand il faut « continuer après un échec », utilisez `=` +
> un match ultérieur ; quand il faut « sauter directement à l'arm de nettoyage en cas d'échec », utilisez `}: {`.

##### Choisir entre block match et value-position match

Une même opération — ramener le résultat d'un calcul optionnel dans une variable simple — s'écrit de deux façons. **Lorsque le corps du bloc ne contient que cette seule instruction,
les deux sont équivalents ; dès que le corps contient d'autres instructions, ou que le RHS peut échouer ailleurs, la sémantique diverge.**

```no
; A : block match + `?=` (« try » au niveau du bloc)
{
    #{overflow=wrap}
    np ?= np + 1
}: {
    err -> _

    -> _
}

; B : value-position match, liaison directe de la cible (niveau instruction)
#{overflow=wrap}
np = np + 1: {
    ok -> it

    -> _
}
```

Sous `#{overflow=wrap}`, le résultat de `np + 1` est **toujours `ok` (valeur de retournement)** ; les deux ne génèrent qu'un seul
`add i64` nu, sans branche de contrôle de débordement, et l'arm d'échec est du code mort pour « l'addition elle-même » — c'est pourquoi ils semblent, sur la plupart des sites,
« parfaitement équivalents ». La vraie différence réside dans la **portée** et le **chemin d'échec** :

| Dimension | A : block match + `?=` | B : value-position match |
| --- | --- | --- |
| Position syntaxique | **Instruction** : pas de valeur de fin, non affectable | **Expression** : peut aller au RHS d'une affectation, comme cible de champ ou au RHS de `?=` |
| Écriture dans la cible en cas d'échec | **Valeur zéro** (`i64` → `0`, `str` → `''`) | Décidée par l'arm de repli : `-> _` **n'écrit pas** (conserve la valeur), `-> 0` écrit 0, `-> sum` réécrit l'ancienne valeur |
| Instructions restantes du corps | **Toutes ignorées** (y compris les itérations de boucle suivantes) | Exécutées normalement |
| Portée d'un arm `err` | **Tous** les `?=` du bloc | Une seule instruction |
| Cible avec déclaration de type | ❌ `q i64 ?= …` est une erreur d'analyse (`yh8904fm`) | ✅ `q i64 = …: { … }` |
| RHS à repli constant | ❌ `s8gh6opt` (`4 / 2` est réduit en simple valeur, plus une option) | ✅ Le sujet est toujours normalisé en `?T` |
| Prérequis d'utilisation | La fonction a un paramètre de résultat `?T`, ou se trouve elle-même dans un match de bloc (sinon `l86f91me`) | Aucun |
| Arm en cas de succès | L'arm est tout de même parcouru une fois (le corps terminé donne `nil`) ; il ne doit contenir que de la suppression ou un traitement pur | Seul l'arm `ok` s'exécute |

Ordre de choix :

1. **Une seule instruction, l'échec valant 0, rien à exécuter ensuite** → équivalence, privilégiez B : un niveau d'indentation en moins, un store de valeur zéro en moins,
   et `no fmt` est idempotent sur les deux formes.
2. **Une suite d'instructions fallibles doit partager un même traitement d'erreur** (lecture → analyse → réécriture) → A : un `?=` par ligne,
   partageant le même arm `err ->` ; le premier échec s'arrête à l'arm de nettoyage.
3. **L'échec doit conserver la valeur d'origine / la remplacer par une valeur de repli explicite / laisser les instructions suivantes s'exécuter** → B (`-> sum`, `-> 0`, `-> it`).
4. **Cette valeur doit encore participer à un calcul** (RHS d'une affectation, cible de champ, RHS de `?=`) → B : A est une instruction, sans valeur.
5. **Traitement élément par élément, un échec isolé ne doit pas ruiner tout le lot** → B ; **tout le lot échoue ensemble, il faut abandonner** → A (les itérations de boucle s'arrêtent aussi).

Deux pièges fréquents :

- 🔴 **Emplacement de l'annotation** : `#{overflow=wrap}` doit être écrit **dans le bloc**, juste au-dessus de la ligne `?=`. Écrite à l'extérieur du bloc
  match (au-dessus de `{`), elle **ne prend pas effet** — en pratique, le débordement renvoie quand même `err` et `np` est remis à zéro par `?=`.
- 🔴 **Ne copiez pas A pour un accumulateur** : `sum ?= 1 / (i - 1)` efface, lors de l'itération en échec, **le `sum` déjà accumulé, le remettant à
  `0`** (écriture de la valeur zéro par `?=`). Pour conserver la valeur accumulée, utilisez `-> sum` de B.

#### Guide de style pour match

```no
#{fmt-skip}
; ❌ Éviter de répéter le corps des branches
w = tls-c.send(req)
w: {
    nil -> {
        tls-c.close()
        return
    }

    err -> {
        tls-c.close()
        return
    }

    ok -> n = it
}
```
```

```no
; ✅ Placer la logique commune dans -> catch-all
w = tls-c.send(req)
w: {
    ok -> n = it

    -> {
        tls-c.close()
        return
    }
}

; ✅ Et inversement : branches simples nommées, logique complexe dans ->
val: {
    nil -> return

    err -> log(it.msg())

    -> {
        n = it
        total = total + n
        process(n)
    }
}
```

```no
; Une seule instruction — pas d'accolades
val: {
    ok -> print(it)

    -> print('empty or error')
}

; Plusieurs instructions — accolades obligatoires
val: {
    ok -> {
        n = it
        total = total + n
    }

    -> {
        log('failed')
        return
    }
}
```

```no
; Liaison implicite de it
val: {
    ok -> process(it); it = la valeur déballée

    err -> log(it.msg()); it = l'erreur structurée err.err (pas un str)

    -> log('empty'); catch-all, ici nil
}
```

```no
#{fmt-skip}
; Portée de it : les matchs imbriqués restaurent it niveau par niveau
; （les noms de fonctions factices s'écrivent keep — `use` est un mot-clé d'import, ne peut pas servir de nom de fonction ;
;   et un arm `->` nu qui lit it sans que nil/err soient tous deux pris en charge est une erreur de compilation, d'où ok -> à chaque niveau）
outer: {
    ok -> {
        keep(it)            ; it externe
        inner ?T = f(it)
        inner: {
            ok -> keep(it)  ; it interne = la valeur déballée de inner
        }
        keep(it)            ; ✅ après la fin de l'arm interne, it est restauré à la valeur externe
    }
}
```

> **Attention** : la restauration n'a lieu que dans les matchs **imbriqués**. Après la fin du match le plus externe, `it` conserve encore la valeur du dernier arm.  
> N'utilisez pas `it` en dehors d'un match. Pour l'exploiter à travers plusieurs niveaux, stockez-le dans une variable nommée (`doc = it`) ou utilisez une liaison par déconstruction  
> `ok(v) -> ...` pour lier le payload à votre propre nom.

> **Bonne pratique (privilégier `it` pour le déballage d'option)** : lorsque la valeur filtrée est une **option `?t`**, préférez `ok -> ...(it)...`  
> plutôt que `ok(v) -> ...(v)...` — cela n'introduit pas de nom de liaison supplémentaire et évite naturellement le piège des « variables de même nom dans des arms différents qui font confluer les types en codegen ».  
> Seuls deux cas imposent une **liaison nommée** `ok(v) ->` : (1) la valeur filtrée est une **variante d'énumération définie par l'utilisateur** (comme  
> `res { ok(v str) }` ou `shape { rect(w, h) }`) — là, `it` désigne **l'énumération entière**, pas le payload, qui n'est accessible que par un nom ;  
> (2) il faut réutiliser la valeur **après un match imbriqué** — le match interne relie à nouveau `it` ; utilisez une liaison nommée ou copiez d'abord dans une locale nommée.

### L'écriture par indice doit être explicitement déballée

Une lecture indexée renvoie une option (dépassement → `err`, `code.index-out`) ; **affecter directement un indice transformerait un dépassement en écriture hors limites non vérifiée**. C'est donc interdit, et il faut choisir l'une de ces deux solutions :

```no
#{fmt-skip}
; ❌ Interdit : a[1] = 2
```
```

```no
; ✅ place-match : gérer soi-même l'arm de dépassement (it = position de l'élément, lisible et modifiable)
a[1]: {
    ok -> it = 2

    -> _
}

; ✅ Propagation de l'erreur : quand la fonction contenante a un paramètre de résultat option (dépassement → propagation)
a[1] ?= 2
```

- Lorsque le compilateur peut prouver que l'indice ne dépasse pas les limites (tableau de longueur fixe + indice constant, variable de boucle prouvée dans les bornes, etc.), il **élimine la vérification en interne et affecte directement, sans coût à l'exécution** ; mais la syntaxe doit tout de même employer une forme de déballage — aucune exemption côté utilisateur.
- Les maps (`[str]i64`, etc.) ne sont **absolument pas soumises** à cette règle : `m['b'] = c` s'écrit directement, sans place-match ni `?=` — l'indice d'une map est une **clé**, pas une position ; l'écriture est un put/insert (clé absente → insertion, clé présente → remplacement), toujours définie, sans « écriture hors limites » ; les bases de champs de struct (`self.keys[i] = v`) ne sont pas non plus concernées pour l'instant.
- Le côté **lecture** d'une map passe tout de même par une option (exactement la même règle que `a[i]` pour les tableaux / vec / slice) : dans une fonction, `x = m[k]` donne `?V`, et `nil` si la clé n'existe pas. `m[k]` et `m.get(k)` sont synonymes, tout comme `m[k] = v` et `m.put(k, v)` ; les deux écritures peuvent être librement mélangées.
- `m[k] ?= v` est une **erreur de compilation** (pas seulement une redondance) : l'arm ok de `?=` est « lire l'élément puis le réécrire à sa **position** », or le `?V` lu depuis une map n'est qu'une copie renvoyée par `get` ; l'y réécrire ne retourne pas dans la map. Écrivez directement `m[k] = v`. La forme place-match `m[k]: { ok -> it = v }` reste utilisable (équivalente à put).

### La variable d'itération est aussi la « position » de l'élément

`x <- a` (où `a` est un `arr` / `vec` / `slice` / tableau de longueur fixe) lie `x` à la **position de l'élément lui-même**, et non à une copie chargée à chaque tour :

- **Lire** `x` relit `a[__i]` — une écriture suivie d'une lecture dans la même itération voit la nouvelle valeur ; `x = x + 1` est un read-modify-write ;
- **Écrire** `x = v` retombe dans `a[__i]` ;
- le `it` dans un arm de match est un **alias** de la même position (valable uniquement dans cet arm) : `x: { ok -> it = v }` est donc parfaitement équivalent à `a[i]: { ok -> it = v }`.

```no
; Les deux segments suivants donnent le même résultat : chaque élément de a devient 5
a []i64 = [0, 0, 0]
i <- [0..3): {
    a[i]: {
        ok -> it = 5

        -> _
    }
}

b []i64 = [0, 0, 0]
x <- b: {
    x: {
        ok -> it = 5

        -> _
    }
}
```

- Lorsque l'élément est lui-même une option (`[]?T`), `it` est le **payload**, qui est automatiquement réemballé dans `ok` à l'écriture.
- L'alias devient invalide après la fin de l'arm : écrire `it = v` après le match ne retourne pas dans le conteneur.

**Cas où ce n'est pas une position** (l'écriture ne reste que locale) :

- `c <- s` (avec `s` une `str`) : les éléments sont des **points de code** décodés, sans case de stockage correspondante dans la chaîne.
- `k <- m` (avec `m` une map) : l'indice est une **clé**, pas une position ; l'écriture ne retombe pas dans la table.

### `it` ne peut être utilisé que dans un arm « dont on peut prouver qu'il couvre un seul cas »

L'arm catch-all `->` recueille tous les cas non explicitement pris en charge par les arms précédents. `it` n'a un sens unique et déterminé **que si le cas restant est exactement unique** :

```no
; ✅ -> est ok (nil / err ont déjà été pris en charge)
v: {
    nil -> log('empty')

    err -> log(it.msg())

    -> process(it)
}

; ✅ -> est err (ok / nil ont déjà été pris en charge)
v: {
    ok -> process(it)

    nil -> log('empty')

    -> log(it.msg())
}
```

```no
#{fmt-skip}
; ❌ -> peut être nil ou err — erreur de compilation
v: {
    ok -> process(it)

    -> log(it)
}
```
```

Ajoutez `nil ->` / `err ->`, ou passez à `ok ->`, et cela compile. Les enum et les matchs nus (`{ cond -> ... }`)  
ne sont pas concernés : `it` y désigne la valeur filtrée elle-même, sans possibilité de nil / err.

> L'`it` de l'arm `err ->` est **toujours la valeur d'erreur (`err.err`)**, indépendamment du type d'élément de l'option —  
> `option { ok(v t), nil, err(e err.err) }`. Ainsi, même pour une option scalaire comme `?i64` / `?bool`,  
> l'`it` de l'arm `err ->` est une erreur structurée : lisez le message avec `msg = it.msg()`,  
> comparez le code avec `it.is(code.io)`, et la position de survenue se trouve dans `it.file` / `it.line` / `it.col`  
> (injectés automatiquement par le compilateur au point de construction `err(...)`).

```no
; ✅ Motif option combiné : nil || err -> body
; partage la même branche quand l'option vaut nil ou err
val: {
    nil || err -> {
        cleanup()
        return
    }

    ok -> process(it)
}

; ✅ Peut aussi être mélangé avec -> catch-all
val: {
    nil || err -> log('failed')

    ok -> process(it)
}
```

### Pipeline de court-circuit (`->`)

`->` est un **opérateur de pipeline à court-circuit associatif à gauche**, à la sémantique fixe, **indépendante de l'affectation environnante ou de l'appartenance à un arm de match**. Instruction nue, corps d'arm sur une ligne et RHS d'affectation partagent le même lowering.

Les nœuds du pipeline sont de trois types :

| Nœud | Effet sur l'état du pipeline |
| --- | --- |
| Appel à effet de bord (sans retour, comme `print('A')`) | Exécuté, **sans modifier l'état** ; si l'état était Ok, on passe au nœud suivant |
| Appel renvoyant `?T` (pouvant échouer, comme `might-fail()`) | Exécuté, **remplace l'état** ; dès qu'il devient nil/err, **tous les nœuds suivants sont ignorés** |
| Expression de valeur terminale | Évaluée seulement si l'état est encore Ok |

**Seuls les nœuds renvoyant `?T` peuvent faire basculer le pipeline en échec** ; `print` et les effets de bord analogues jamais. Une « chaîne entière de `print` » semble donc ne pas court-circuiter, simplement parce qu'aucun nœud ne peut produire d'échec.

```no
; Aucun nœud susceptible d'échouer → tout s'exécute
ok -> print('A') -> print('B')

; might-fail() renvoie ?T → en cas d'échec, print('B') ne s'exécute pas
ok -> print('A') -> might-fail(x) -> print('B')

; RHS d'affectation : même sémantique
r = print('E') -> might-fail(x) -> 42
```

**Précisions sur les limites :**

1. La sémantique de `->` est unique : instruction nue et expression d'affectation partagent le même lowering de pipeline, sans changement de mode selon la présence d'une affectation environnante.
2. Dans un **corps d'arm sur une seule ligne, `->` est la continuation du pipeline, pas un nouvel arm** ; les arms sont séparés par un **saut de ligne** :

```no
; ✅ Un seul arm, le corps est un pipeline → les deux lignes s'affichent
ok -> print('A') -> print('B')

; ✅ Deux arms (séparés par un saut de ligne) → si ok est pris, 'miss' n'est pas affiché
v: {
    ok -> print('hit')

    -> print('miss')
}
```

> ⚠️ Les arms sont séparés par des sauts de ligne : `pat -> A -> B` écrit sur une seule ligne est le **pipeline d'un seul arm**, et non « if A else B ». Pour un if/else, mettez les arms sur des lignes distinctes ou enveloppez un groupe à court-circuit dans `{}`.

1. **Le pipeline peut produire une valeur** : placé à droite d'une affectation, son résultat est le **nœud valeur terminal** (évalué seulement si l'état est encore Ok).

```no
n = 7
x = 1 > 2 -> 42; condition fausse → x conserve sa valeur 7
y = 2 > 1 -> 42; y == 42
s str = 'old'
s = 1 > 2 -> 'new'; court-circuit → s reste 'old'
r = print('E') -> might-fail(bad) -> 99; échec au milieu → r conserve sa valeur
```

En cas d'échec (condition fausse ou court-circuit par un nœud `?T`), **l'affectation n'a pas lieu** : la variable conserve sa valeur d'origine ; s'il s'agit d'une première déclaration, elle prend la valeur zéro du type. C'est cohérent avec la forme instruction — `{ 1 > 2 -> x = 42 }` ne modifie pas `x` non plus.

Le type de la valeur est déduit de l'expression terminale de l'arm (`str` / `i64` / `?T` acceptés), avec la même inférence que la forme de récupération de match existante `x = subject: { arms }`.

### If / Else

Un groupe if-else (à court-circuit) **doit** être entouré de `{}`. La première condition correspondante gagne, les conditions suivantes ne sont plus vérifiées.

```no
; Branches multiples (nouvelle forme recommandée) — chaîne if-else à court-circuit
{
    a == 1 -> {
        a = 1
        b = 2
    }

    a == 2 || a == 3 -> do-something()

    -> {
        c = 0
    }
}

; if simple (conservé) — if indépendant, sans court-circuit
x == 1 -> do-something()

; Expression ternaire condition ? true-value : false-value
c = flag ? 1 : 2
max = sum > 10 ? sum : 10
```

**Règles importantes :**

1. **Groupe à court-circuit** — plusieurs `cond -> body` enveloppés dans `{}` forment une chaîne if-elif-else ; seule la première branche correspondante s'exécute.
2. **if indépendant** — écrire directement `cond -> body` dans un corps de fonction/boucle (sans `{}`) est un if indépendant, qui ne court-circuite pas les if-then adjacents.
   **Dès qu'il y a un else, le regroupement est obligatoire** — un arm de repli `->` sur sa propre ligne écrit hors de `{}` est une **erreur de compilation** : un `cond ->` isolé suivi de `->` donne `k3r7mbqd`, et s'il a des arms `cond ->` frères au-dessus, `m7vq3xrt` (le message précise le nombre d'arms perdus). Le parser ne replie le `->` sur sa propre ligne que vers le **if nu le plus proche** ; les autres arms deviennent silencieusement invalides — le sens lu littéralement diffère entièrement du résultat réellement compilé, d'où une règle plutôt qu'un avertissement. Enveloppez tous les arms dans `{ ... }` ; **un `cond -> body` isolé sans else reste autorisé sous forme nue**.
3. **Interdiction de mélanger** — les enfants directs d'un groupe à court-circuit `{}` doivent tous être des branches `cond -> body` ; aucune instruction ordinaire ne peut s'y mêler.

```no
#{fmt-skip}
; ❌ Pas de court-circuit — ce sont des if indépendants, toutes les conditions sont vérifiées
git-dispatch = (cmd str) {
    cmd == 'a' -> {
        fa()
    }
    cmd == 'b' -> {
        fb()
    }
}
```
```

```no
; ✅ Court-circuit — enveloppé dans {}, le premier cas correspondant gagne
git-dispatch = (cmd str) {
    {
        cmd == 'a' -> {
            fa()
        }

        cmd == 'b' -> {
            fb()
        }

        true -> {
        }
    }
}
```

```no
#{fmt-skip}
; ❌ if/else écrit hors du groupe de court-circuit — erreur de compilation
; le `-> break` sur sa propre ligne ne s'attache qu'à `c == 43` ; même après `c == 45`, il interrompt tout de même
;   c == 45 -> {
;       exp-sign = -1
;       i = i + 1
;   }
;   c == 43 -> i = i + 1
;   -> break
```
```

```no
; ✅ Le même code écrit en groupe if — les arms s'excluent mutuellement et `->` est le vrai arm de repli
{
    c == 45 -> {
        exp-sign = -1
        i = i + 1
    }

    c == 43 -> i = i + 1

    -> break
}
```

```no
#{fmt-skip}
; ❌ Mélange de match-block et d'instructions ordinaires → erreur de compilation
{
    cmd == 'a' -> {
        fa()
    }
    print(cmd)
    cmd == 'b' -> {
        fb()
    }
}
```
```

```no
; ✅ Placer les instructions ordinaires dans le corps d'une branche
{
    cmd == 'a' -> {
        print(cmd)
        fa()
    }

    cmd == 'b' -> {
        fb()
    }

    true -> {
    }
}
```

### Programmation asynchrone : groupe de coroutines `awy { ... }` + `co`

Nolang ne dispose que d'une seule syntaxe de concurrence de surface : le groupe de coroutines `awy { ... }`, dans lequel `co` dérive les tâches.
Le mot-clé `run` a été supprimé (`run` est désormais un identifiant ordinaire) ; le préfixe d'attente `awy <handle>` et le groupe de coroutines à bloc nu en position d'instruction
sont tous deux abandonnés. **Écrire `co` hors d'un groupe est une erreur de compilation** (`E_CO_OUTSIDE_GROUP`).

`co` a deux formes, qui toutes deux ne lancent qu'une **procédure** et ne produisent aucune valeur :

```no
; Procédure : n'écrit que dans le canal, aucun retour déclaré
produce = (ch chan[i64], v i64) {
    ch <- v
    ch.close()
}

ch chan[i64] = with-cap(2)
awy {
    co produce(ch, 1)          ; ① spawn de procédure : co f(args)
    co {                       ; ② spawn de fermeture : co { ... }
        ch <- 2
        ch.close()
    }
}
sum = 0
i <- ch: {
    sum = sum + i
}
print(sum.to-str())            ; => 3
```

#### ① Spawn de procédure : `co f(args)`

`f` **ne doit déclarer aucun retour**. Le résultat du spawn n'a nulle part où aller (`co` n'est pas une valeur) ; aussi une fonction avec valeur de retour
appelée par `co` produit `c0retval` :

```no
worker = (n i64) (r i64) { r = n }   ; retour déclaré
awy { co worker(1) }                  ; ✗ c0retval
```

Le `self` implicitement renvoyé par une méthode **ne compte pas** comme une valeur de retour — c'est le paramètre de sortie récepteur que porte chaque méthode, destiné à permettre le chaînage des appels ; `co obj.flush()` est donc parfaitement normal :

```no
queue {
    n i64
}
queue.flush = () {          ; (self queue) implicite, aucun autre retour
    self.n = 0
}
awy { co q.flush() }        ; ✓
```

Pour obtenir un résultat, écrivez dans un canal : la procédure envoie la valeur dans le canal puis fait `close()`, et l'appelant draine le canal hors du groupe. Ce n'est pas une simple suggestion de style

mais une règle : le résultat d'un appel spawné n'est lisible qu'après le join du groupe, et l'endroit où on le lit n'est pas
le point de join ; c'est précisément le scénario pour lequel `chan` existe.

#### ② Spawn de fermeture : `co { ... }`

`co { ... }` confie sur place une procédure à une coroutine. Les **variables libres du bloc sont capturées par valeur**
(instantané au moment du spawn, même sémantique que la copie profonde des arguments d'un spawn ordinaire) :

```no
seed = 33
ch chan[i64] = with-cap(1)
awy {
    co {
        ch <- seed
        ch.close()
    }
}
r = 0
<- ch: { ok -> r = it }
print(r.to-str())          ; => 33
```

#### `co` n'est pas une valeur : ne peut former une instruction autonome

`co` produit void ; il doit donc occuper une instruction entière. À droite d'une affectation, en position d'argument ou de retour, c'est partout
`E_CO_NOT_STATEMENT` (`c0nostmt`) :

```no
r = co produce(ch, 1)      ; ✗ c0nostmt
print(co produce(ch, 1))   ; ✗ c0nostmt
```

En outre, `co` ne peut être suivi que d'un « appel » ou d'un « bloc » ; toute autre forme donne `E_CO_NOT_CALL` (`c0notcal`).

#### Async incolore : monomorphisation automatique

`co` est une syntaxe **incolore (colorless)** : vous écrivez le nom de fonction non coloré `produce`, et le compilateur monomorphise automatiquement
la variante `produce-async` et l'exécute en profondeur comme une coroutine colorée sans pile. N'écrivez pas le suffixe `-async` à la main dans le code applicatif.

Règles :

- **Monomorphisation** : `co produce(ch, 1)` génère automatiquement `produce-async` (copie du corps de `produce`, renommée). Si `produce` est aussi appelé de façon synchrone, les deux coexistent.
- **Transitivité** : si une procédure contient elle-même `awy { co child(n) }`, cette procédure est également monomorphisée en `-async`, et la coloration asynchrone remonte la chaîne d'appels.
- **Barrière** : les instructions du groupe qui ne sont pas des `co` (affectation ordinaire, `print`, boucle, `if`, **lecture de canal**…) attendent d'abord toutes les tâches en vol. Les dépendances de données dans le groupe s'écrivent donc « lire d'abord le canal, puis utiliser la valeur lue pour spawn » — la lecture de canal est elle-même une barrière, plus lisible qu'une dépendance de variable implicite.
- **Le corps de fonction n'est pas un groupe de coroutines** : seul `awy { ... }` introduit un groupe. Un `co` dans un corps de fonction, une branche `if` ou un corps de boucle est toujours refusé.
- **Jeter le résultat attend quand même** : `awy { co side(5) }` spawn et await tout de même, sans laisser de tâche non attendue.

#### Modèle d'ordonnancement : suspension réelle

Le clone `-async` n'est pas « on exécute tout d'un bloc puis on rapporte » : c'est une **machine à états resumable** : les variables locales sont déplacées dans une frame sur le tas accrochée à `%task.frame`, chaque point de suspension devient le terminateur du bloc (il écrit le numéro d'état dans la frame puis rend la main), et l'entrée de la fonction devient une chaîne de comparaisons d'état (`__state == k` ⇒ saut direct au k-ième point de reprise).

Trois endroits provoquent donc une **suspension** plutôt qu'un blocage :

| Point de suspension | Condition de reprise |
|---|---|
| `async-yield()` | Se remet en file à la queue, attend d'être en tête |
| `awy` sur une tâche enfant inachevée | Réveillé quand la tâche enfant se termine (inscrit sur la chaîne `%task.waiters`, un réveil réveille toute la chaîne) |
| Réception sur canal vide (`v <- ch:` / `<- ch:`) | `send` / `close` détache toute la chaîne d'attente et la remet en file un par un |

L'ordonnancement est coopératif monothread, avec une file de tâches prêtes FIFO ; l'**ordre est donc entièrement déterministe**, sans sleep : deux tâches qui cèdent chacune une fois donnent nécessairement `a1 b1 a2 b2` (`tests/coro-yield.no` épingle les codes d'événement).
Un waiter réveillé **n'obtient pas pour autant une valeur** — un `send` n'ajoute qu'un élément mais peut réveiller plusieurs waiters ; aussi le point de reprise du canal est le bloc de park lui-même, qui refait le test de lisibilité avant de décider de prendre la valeur ou de continuer à attendre.

> Le blocage est détectable : file de tâches prêtes vide et tâche cible toujours inachevée ⇒ l'exécution affiche
> `nolang: deadlock: …` sur stderr et avorte. C'est bien plus honnête que « renvoyer une valeur zéro que personne n'a envoyée ».

> **Lowering sous-jacent** (pas une syntaxe utilisateur) : chaque `co` devient un `OpRun`, et la fin du groupe insère un `OpAwait` pour chaque tâche ;
> `KRun` / `KAwait` / `OpRun` / `OpAwait` n'existent que dans le HIR/MIR. La transformation des coroutines est effectuée par
> `PlanCoroutines` du MIR après `Analyze` (`src/mir/coro.go`).

L'annulation : `cancelled()` permet une auto-vérification coopérative dans une fonction coroutine ; `cancel(h)` est une opération de handle bas niveau,
que la nouvelle syntaxe n'expose pas et que le code applicatif n'utilise pas.

### Affectation multiple

Une fonction peut renvoyer plusieurs valeurs ; à l'appel, on les reçoit par affectation multiple :

```no
; Définition de fonction renvoyant plusieurs paramètres de résultat
swap = (a i64, b i64) (x i64, y i64) {
    x = b
    y = a
}

; Affectation multiple
a, b = swap(5, 3)

; Utiliser _ pour ignorer les retours inutiles (variable factice)
_, b = swap(5, 3); ne prend que la deuxième valeur, ignore la première
a, _ = swap(5, 3); ne prend que la première valeur, ignore la deuxième
_, _ = swap(5, 3); ignore tous les retours (appel uniquement pour effet de bord)

; Peut aussi servir de corps d'arm de match
val: {
    ok -> {
        a, b = parse-pair(it)
    }

    -> return
}
```

## Surcharge d'opérateurs

Lorsque le type statique de l'opérande gauche définit une méthode `_op` particulière, le compilateur réécrit automatiquement, lors du lowering, l'opération infixe `a op b` en appel de méthode `a._op(b)`. L'ensemble des surcharges est fermé et compte 5 opérateurs arithmétiques binaires symétriques :

| Opérateur | Méthode de surcharge |
|--------|----------|
| `+` | `_add` |
| `-` | `_sub` |
| `*` | `_mul` |
| `/` | `_div` |
| `%` | `_mod` |

```no
big {
    v i64
}

; Le type de retour s'écrit ?big, le compilateur le traite comme un payload big pur
big._add = (b big) (c ?big) {
    c = big {
        v: .v + b.v
    }
}

f = (a big, b big) (out big) {
    r big = a + b; le compilateur réécrit en a._add(b)
    out = r
}
```

**Convention de type** : le paramètre de résultat d'une méthode de surcharge **s'écrit `?T`, mais son type effectif est le payload pur `T`** — le compilateur n'effectue ni emballage ni déballage d'option ; le corps de la méthode affecte directement une valeur `T` à `c`, et l'appelant reçoit également un `T`. Les erreurs comme la division par zéro provoquent un trap direct à l'exécution, comme pour les types numériques intégrés, sans produire d'option.

**Points clés de la sémantique :**

- La réécriture n'a lieu que si le type statique de l'opérande gauche définit la méthode `_op` correspondante ; les types numériques intégrés (i64, f64, etc.) continuent d'utiliser l'arithmétique intégrée.
- L'appel de méthode explicite `a._add(b)` et `a + b` sont parfaitement équivalents.
- Les chaînes imbriquées à gauche sont réécrites niveau par niveau : `a + b + c` → `(a._add(b))._add(c)` ; le type du résultat intermédiaire reste `T`, qui peut continuer à participer à des calculs ou à des appels de méthode.
- Le `bigint` de la bibliothèque standard utilise ce mécanisme pour prendre en charge `+ - * / %` (voir la section bigint dans « Bibliothèque standard > Autres modules »).

## Tableaux et tranches

Les conteneurs stockent des copies des données ; la variable d'origine et le conteneur sont indépendants, ce qui élimine les références pendantes.

**Tableau de longueur fixe arr :**

```no
; Utilisation d'un tableau de longueur fixe
a [3] = [1, 2, 3]; tableau i64 de longueur fixe 3
a [3]u16 = [1, 2, 3]; tableau de longueur fixe avec type explicite

a[nil]
u16 = [1, 2, 3]; longueur déduite automatiquement (type spécifié)
a[nil] = [1, 2, 3]; longueur déduite automatiquement (i64)
```

**Tableau de longueur variable vec :**

```no
v = [1, 2, 3]; tableau de longueur variable i64
bs = [0x11, 0x22, 0x33]
v []u8 = [1, 2, 3]; tableau de longueur variable avec type explicite

; Préallocation (évite les réallocations répétées)
v = with-cap(100); len=0, cap=100 (indexation après push)
v = with-len(100); len=100, cap=100 (indexation directe possible)
v = with-cap-len(200, 100); len=100, cap=200 (espace réservé à la croissance)
```

**Tranche slice (vue, et non un type indépendant) :**

Une tranche est une **vue** sur les données d'origine : elle ne copie pas les données et ne crée pas de nouveau type indépendant.  
Internement, elle ne conserve qu'un pointeur vers le tampon d'origine, une longueur et une capacité ; par conséquent :

- Modifier un élément via la tranche affecte les données d'origine, et inversement ;
- La tranche ne possède pas les données : une fois la variable d'origine libérée, la tranche devient invalide ;
- Le type de la tranche est déterminé par le type d'origine, et ses méthodes s'appliquent naturellement, sans mécanisme d'« héritage ».

```no
; prend en charge arr/vec/str
; prend en charge les plages, notation identique à for <-
nums [5]u8 = [0, 1, 2, 3, 4]

nums[..]; [0 1 2 3 4]   totalement non bornée, équivaut à la tranche entière
nums[1..]; [1 2 3 4]     sans borne supérieure, de l'index 1 à la fin
nums[..4]; [0 1 2 3 4]   sans borne inférieure, du début à l'index 4
nums[2..3]; [2 3]        bornée, les deux extrémités incluses
nums[1..3]; [1 2 3]      bornée, les deux extrémités incluses
nums[1..3); [1 2]        fermée à gauche, ouverte à droite
nums(1..3); [2]          intervalle ouvert aux deux extrémités

; Chaîne (le résultat est une vue str, mémoire sous-jacente partagée ; l'indice est une position de point de code, comme s[i])
s = 'abc'
s[1..]; 'bc'
s[1..s.len()); 'bc'
b str = s[0..1]; le résultat est déjà un str, affectable directement à une variable str
```

> La syntaxe de tranche prend en charge les mêmes 16 combinaisons d'intervalles que match/range-for. Dans un contexte de tranche, un intervalle non borné se base sur la longueur de la tranche : `[..]` désigne la tranche entière, `[a..]` va de l'index a à la fin, et `[..b]` du début à l'index b.

**Type et méthodes de la tranche :**

Une tranche ne génère pas de nouveau type indépendant ; elle n'est qu'une vue du type d'origine (avec pointeur de départ et longueur ajustés).  
Les méthodes du type d'origine sont donc directement utilisables :

| Type d'origine | Type de la vue tranche | Méthodes disponibles |
| -------------- | ------------ | -------------------------------------------------------------------------------- |
| `arr` (`[n]t`) | `[]t<range>` | Toutes les méthodes de `[]t` (comme `len`, `push`, `pop`, `contains`, `reverse`, `clone`, `fill`, `to-arr`, etc.) |
| `vec` (`[]t`)  | `[]<range>`  | Idem |
| `str`          | `str<range>` | Toutes les méthodes de `str` (comme `to-upper`, `to-lower`, `index`, `contains`, `slice`, `copy`, `fill`, etc.) |

```no
; Tranche arr → vue vec, mémoire sous-jacente de arr partagée
a [5]u8 = [0, 1, 2, 3, 4]
s = a[1..4]; s est une vue []u8 pointant vers la mémoire de a
n = s.len(); slice.len()

; Tranche vec → vue vec, mémoire sous-jacente de vec partagée
v = [10, 20, 30, 40, 50]
s = v[2..]; s est une vue []i64
s.reverse(s.len()); slice.reverse

; Tranche str → vue str, mémoire sous-jacente de str partagée
s = 'Hello World'
sub = s[6..]; sub est une vue de 'World'
upper = sub.to-upper(); str.to-upper

; Modifier un élément via la tranche affecte les données d'origine
data = [10, 20, 30, 40, 50]
view = data[1..4]; view = [20, 30, 40]
view[0] = 99; modifier l'élément de view
; data[1] vaut désormais 99 aussi, car view partage la mémoire de data
```

### Indexation

```no
; L'indexation d'une chaîne renvoie ?char (point de code Unicode, pas un octet)
c char = s[i]; un type plain explicite ramène le résultat à char (voir « Indexation sûre »)

; Récupération d'un élément depuis arr / vec
arr[i]; → ?elem
vec[i]; → ?elem

; Récupération d'une valeur depuis map
map[str]
```

> **Le résultat d'une lecture indexée est toujours `?T`** : `arr[i]` / `vec[i]` / tableau de longueur fixe / `slice` → `?elem`,  
> `str` / `txt` indexé par `s[i]` → `?char` (D6, la même règle unifiée que l'arithmétique sur les entiers).  
> Ainsi, le **type inféré de `str[i]` est `?char`** ; deux façons de récupérer un `char` simple :  
> écrire un type plain explicite sur la liaison (`c char = s[i]`, court-circuit synthétisé dès le lowering, valeur plain relue),  
> ou déballer avec un match / `?=`.  
> Une tranche `str[a..b]` renvoie toujours `str` (vue à sémantique de points de code, mémoire sous-jacente partagée).  
> `char` peut être **converti implicitement en `str`** (encodé en chaîne d'un seul caractère) ; les écritures suivantes sont donc toutes valides, sans appel manuel à `char.to-str()` :

```no
s = 'héllo'
c char = s[1]; 'é' (char, dont le point de code vaut 233)
a str = s[1]; 'é' (conversion implicite char → str)
b str = s[0..1]; 'hé' (le résultat de la tranche est déjà un str)
msg = 'first: ' - s[0]; char converti implicitement en str lors de la concaténation (-)
ok = s[0] == 'h'; char converti implicitement en str lors de la comparaison avec un str
```

### Indexation sûre (Safe Indexing)

Pour l'indexation `v[i]` d'un `arr` / `vec` / `slice` (`[]T`) / tableau de longueur fixe / `str` / `txt`, **le résultat est toujours `?T`** : hors limites, il renvoie `err` (`err('index out of range', code.index-out)`, avec la position source de l'indice), dans les limites, il renvoie `ok(valeur)`, et jamais d'abort (runtime error: index out of bounds). C'est la même règle que le comportement de débordement non annoté de l'arithmétique entière `+ - * / %` (voir « Débordement des calculs entiers »).

Pour consommer un `?T`, il n'y a que trois voies (**l'annotation `#{index-out=DEF}` a été dépréciée et supprimée en D11** ; une annotation résiduelle est une erreur de compilation ;
pas non plus de raccourci « type plain explicite pour ramener un plain » : `v i64 = a[i]` doit également être consommé explicitement) :

**1. Lecture — déballage par match**

Un match valeur prend directement la valeur, ou un match instruction l'affecte dans l'arm `ok`. `-> _` est l'arm « on jette »,
qui produit en contexte de valeur la **valeur zéro du type d'élément** (i64→0, str→'', byte→0, conteneur→conteneur vide).

```no
; Match valeur
x = state[5]: {
    ok -> it

    -> _
}

; Match instruction
state[5]: {
    ok -> x = it

    -> _
}
```

**2. Écriture — place-match**

`it` dans l'arm `ok` est la **position de l'élément** (lisible et modifiable), et non une copie de la valeur.

```no
state[5]: {
    ok -> it = x

    -> _
}
```

> **Exception map :** écrire `m['k'] = 1` **ne nécessite pas de déballage** — l'indice d'une map est une clé et l'écriture est un put/insert,
> clé absente → insertion, clé présente → remplacement : toujours défini, aucune « écriture hors limites » à garder. Mais **la lecture d'une map `m['k']` renvoie toujours `?V` et doit être déballée**.
> `m[k] ?= v` est une **erreur de compilation** (le `?V` lu par `get` n'est qu'une copie ; l'y réécrire ne retourne pas dans la map) : écrivez directement `m[k] = v`.

**3. Abréviation `?=` — propagation de l'erreur**

`x ?= state[5]` (lecture) et `state[5] ?= x` (écriture) sont des abréviations des deux matchs ci-dessus : en cas d'échec, l'option entière
est propagée vers le haut. C'est légal **dans l'un des deux cas suivants** :

- la fonction contenante a un paramètre de résultat `?T` — en cas d'échec, on écrit dans le paramètre de résultat et on fait un `return` anticipé ;
- l'instruction se trouve dans le corps d'un **match de bloc au niveau instruction** `{ ... }: { ... }` — capturé par le match de bloc le plus proche,
  le corps s'arrête et les arms ne tournent qu'une fois ; la fonction **n'a pas besoin** de paramètre de résultat `?T`.

Si aucun des deux ne s'applique, c'est une erreur de compilation. Le repli standard pour « je veux utiliser l'abréviation même si mon retour n'est pas une option » est donc d'envelopper le corps de la fonction dans un match de bloc :

```no
handle = (x i64) {
    {
        y ?= x + 1
        print(1)
    }: {
        err -> _

        -> _
    }
}
```

**Lectures imbriquées** : le match interne relie à nouveau `it` ; on utilise donc une liaison nommée `ok(it1) -> it1` au niveau interne. La forme précise donnée par l'utilisateur
(un match valeur écrit directement à droite du `it = ...` d'un place-match) **réécrit correctement dans le conteneur depuis le 2026-11-06** : le compilateur élève ce match valeur en temporaire synthétique
lors du lowering puis relie à nouveau `it` ; la régression est dans la section `place-write-value-match` de `tests/match.no`. **Forme directe privilégiée** : liez le résultat du match valeur directement au nom final à utiliser,
sans le passer d'abord dans un temporaire jetable pour le recopier ensuite.

```no
#{fmt-skip}
; ✅ Lier directement la variable cible
at = argt[i]: {
    ok -> it

    -> _
}
s.farg.push(at)

; ❌ N'introduisez pas de temporaire à usage unique pour un emploi isolé
arg-at = argt[i]: { ok -> it  -> _ }
at = arg-at
s.farg.push(at)
```

« Élever le match valeur en temporaire nommé hors de l'arm, puis faire `it = arg-at` dans l'arm » **n'est pas équivalent à l'écriture directe**, et ce n'est pas un simple choix de mise en forme librement interchangeable : le moment de l'évaluation diffère (une fois élevé hors de l'arm, le `nil` / `err` / catch-all externe lit aussi une fois) ;
`it` n'est valable que dans l'arm et désigne la position de l'élément, la recopie doit donc rester dans l'arm ; le temporaire nommé est une nouvelle liaison, avec son propre move /
propriété et destruction à la sortie de portée ; et en cas d'imbrication, le niveau interne doit utiliser une liaison nommée `ok(it1) -> it1`, pas recopier le `ok -> it` externe.
N'élevez que si vous devez réellement réutiliser la même valeur à plusieurs endroits, et donnez un nom significatif qui dise ce que la valeur représente.

#### Éliminer les temporaires à usage unique restants avec `?=` et le match de bloc

Ce qui précède concerne le « lien direct pour un usage unique ». Allons plus loin : l'indexation sûre renvoie un `?T`, **donc lorsque le paramètre de résultat de la fonction est lui-même un `?T`, aucun match valeur n'est nécessaire, `?=` propage directement en une ligne** :

```no
#{fmt-skip}
; ✅ La fonction a un paramètre de résultat ?T : ni temporaire, ni match
at ?= argt[i]
s.farg.push(at)
```

**Lorsque le résultat de la fonction n'est pas une option** (cas le plus courant : fonction auxiliaire sans retour, ou simple écriture dans un conteneur), enveloppez l'instruction dans cette
écriture « de type try » :

```no
#{fmt-skip}
handle = (x i64) {
    {
        y ?= x + 1
        print(y)
    }: {
        err -> _

        -> _
    }
    print('continues afterwards')
}
```

⚠️ **Ce n'est pas une nouvelle syntaxe `try`, et le langage n'a pas de mot-clé `try`.** C'est simplement un **match de bloc au niveau instruction**
(`{ ... }: { ... }`), dont le sujet est le `?T` à trois états ; `?=` propage l'échec vers le **match de bloc le plus proche**
(voir « Propagation d'erreur par `?=` » et « Trois formes du sujet de match »). En cas d'échec, les instructions restantes du corps du bloc — y compris
les itérations de boucle suivantes — sont toutes ignorées, les arms ne s'exécutent qu'une fois, puis **l'exécution reprend après le bloc**, pas dans la fonction

`return`. `err -> _` jette l'erreur, `-> _` sert de repli pour jeter le résultat réussi ; pour traiter l'erreur, écrivez
`err -> print(it.msg())`.

⚠️ **La différence sémantique doit être claire** (vérifié en pratique, pas à l'intuition) : lors d'un échec de `?=`, **la cible est tout de même écrite à zéro** —
`i64` donne `0`, `str` donne `''`, `out[1]` donne `0`. Or l'arm de repli `-> _` d'un match à position de valeur **n'écrit pas dans la cible** : celle-ci,
lorsqu'elle est une **nouvelle déclaration**, est déjà initialisée à zéro, ce qui donne l'illusion d'un accord ; lorsqu'elle a **déjà une valeur**, les deux divergent (`?=` remet à zéro, `-> _`
conserve la valeur d'origine ; pour remettre à zéro, écrivez `-> 0`). L'autre différence concerne le flot de contrôle : `?=` **ignore les instructions suivant la sienne dans le corps du bloc**
(dans une fonction `?T`, c'est un `return` anticipé ; dans un match de bloc, un saut direct vers les arms).

Ainsi : **lorsque le corps du bloc ne contient que cette seule instruction et que l'ancienne valeur avant échec n'a pas de sémantique**, écrire l'arm de repli `-> 0` suffit à rendre
les deux parfaitement équivalents (c'est précisément la forme des réécritures en lot de ce dépôt, où les temporaires sont le plus souvent de nouvelles déclarations) ; mais dès que le corps contient
d'autres instructions après le `?=` (compteur de boucle, affectations suivantes), ou que l'ancienne valeur de la cible a un sens, les deux divergent et ces sites ne peuvent pas être remplacés mécaniquement. Le tableau complet
des choix figure dans « Choisir entre block match et value-position match ».

Un autre piège fréquent : **l'arm de succès s'exécute lui aussi une fois**, le catch-all recevant la valeur réussie (`-> print('x')` affichera quelque chose).
Les arms ne doivent donc contenir qu'une suppression ou un traitement pur ; les formes `err -> _` / `-> _` n'ont pas d'effet de bord.

Il en résulte l'ordre de nettoyage des temporaires jetables :

1. La valeur n'est utilisée qu'une seule fois au même endroit → liez-la directement au nom final (voir plus haut).
2. La fonction a un paramètre de résultat `?T` → `TARGET ?= EXPR`, sans temporaire.
3. Sinon → placez l'instruction dans un match de bloc puis écrivez `TARGET ?= EXPR` ; un place-match qui réécrit
   (`SUBJ: { ok -> it = temp  -> _ }`) se réduit aussi directement en `SUBJ ?= EXPR`.
4. Ne conservez un temporaire nommé que si vous devez réellement réutiliser la même valeur à plusieurs endroits, avec un nom significatif.

Deux limites : `?=` est une erreur de compilation lorsqu'il n'y a ni paramètre de résultat `?T`, ni appartenance au corps d'un match de bloc
(`l86f91me`) ; **un LHS avec déclaration de type ne peut pas être associé à `?=`** (`ai i64 ?= a[i]` est une erreur d'analyse
`yh8904fm`, `expected assignment operator`). Pour une promotion explicite comme `byte → i64`, utilisez plutôt un match valeur direct avec déclaration de type :

```no
#{fmt-skip}
ai i64 = a[i]: {
    ok -> it

    -> _
}
```

> **Portée :** valable sans exception pour `arr` / `vec` / `slice` / tableau de longueur fixe / `str` / `txt` ; seules les indexations à **base de variable directe**
> (`v[i]`) sont réécrites en forme sûre ; `receiver.field[i]` emprunte la voie existante de bounds-check.
> Dans un opérande de comparaison, la lecture indexée compare le payload (`a[i] == 5`), pas le `{tag, payload}` entier.

> ⛔ **Erreur bloquante :** utiliser un `?T` comme valeur ordinaire (comme `crc = crc ^ s[i]`, `f(s[i])` ou `v i64 = a[i]`) bloque
> `no build` ; le compilateur suggère le match ou `?=` (avec un match de bloc enveloppant lorsqu'il n'y a pas de paramètre de résultat `?T`).
> Le corpus existant a déjà été migré en une seule passe par `no fmt --fix=index-out`, outil retiré avec D11.

> **Garantie centrale :** un dépassement de limites ne provoque jamais de plantage silencieux — il est soit traité par un match, soit propagé par `?=`.

> Une **lecture indexée dans un opérande de comparaison** applique elle aussi l'indexation sûre : `a[i] == 5` compare le payload (même règle que `?T == 5`),
> et non le `{tag, payload}` entier. En cas de dépassement, on lit `err` (`code.index-out`) ; toute comparaison avec une valeur est alors **fausse**
> (`a[i] == 0` comme `a[i] == 5`), et aucune mémoire hors limites n'est lue.
> `s[0] == 'a'` fonctionne de même : la comparaison entre un point de code et une chaîne d'un caractère passe par la conversion implicite `char → str`.

> **Indices imbriqués** `a[i][j]` : **les deux niveaux sont des indexations sûres**. Le niveau externe `a[i]` vérifie d'abord les limites et l'existence,
> et ce n'est que si l'élément existe qu'on lui applique `[j]` ; si un niveau quelconque dépasse les limites, toute l'expression vaut `err`.
> Ainsi `a[oob][0] == 'c'` est faux sans provoquer d'abort.

> **Comparaison de conteneurs** `[]T == []T` (et `[N]T == [N]T`) est une **comparaison structurelle** : on compare d'abord les longueurs, puis élément par élément.
> Lorsque les éléments sont eux-mêmes des conteneurs (`[][]i64`), la comparaison descend récursivement ; lorsque l'élément est un `str`, on compare le **contenu** et non le pointeur data.
> Les comparaisons d'ordre (`<` / `<=` / `>` / `>=`) ne sont pas définies pour les conteneurs ; seules `==` / `!=` sont prises en charge.

> **`nil` ne peut être comparé qu'à une option** : `?T == nil` / `?T != nil` sont valides ; pour un non-option,
> `T == nil` (`str`, `[]i64`, `[N]T`, `txt`, `i64`, struct…) est une **erreur de compilation** (`q4nilcmp`).
> Ce n'est pas un caprice : `str == nil` serait toujours faux (on compare une structure `%str-long` à 0),
> et `[]i64 == nil` tomberait dans le chemin de comparaison entière, générant un LLVM illégal (`icmp eq i64 %vec, i64`) ;
> les deux donneraient silencieusement une mauvaise réponse. Pour exprimer « il se peut qu'il n'y ait pas de valeur », déclarez le type en `?T` puis comparez à `nil`.

## Structures

La définition comme le littéral de structure doivent utiliser la forme multiligne : chaque champ occupe sa propre ligne, les champs ne sont pas séparés par des virgules, et aucune virgule ne suit le dernier.

```no
user {
    name str
    age i64
}

u = user {
    name: 'Alice'
    age: 30
}
u.name = 'Bob'
u.age = 25
print(u.name)
```

### Implémentation d'interfaces par une structure

Une structure peut implémenter une ou plusieurs interfaces ; les noms d'interfaces s'écrivent après le nom de la structure, séparés par des virgules.

```no
; Implémentation d'une seule interface
user json {
    name str
    age i64
}

; Implémentation de plusieurs interfaces
file enter, leave {
    path str
    fd i64
}
```

#### Implémentation d'interfaces inter-modules

Lorsqu'on implémente une interface définie dans un **autre module**, le nom de l'interface doit être préfixé du module (`ShortName.`). Voir [Préfixe d'appel inter-modules](module.md).

```no
#{fmt-skip}
; ❌ Erreur : db, rows, stmt sont des interfaces définies par le module sql, le préfixe ne peut pas être omis
db-mysql db {
    fd i64
}
```
```

```no
; ✅ Correct : utiliser sql.db, sql.rows, sql.stmt
db-mysql sql.db {
    fd i64
}

rows-mysql sql.rows {
    fd i64
}

stmt-mysql sql.stmt {
    fd i64
}
```

> Les interfaces intégrées (`enter`, `leave`) et celles définies dans le même fichier n'ont pas besoin de préfixe de module.

## Méthodes

Les méthodes sont définies sur un type et utilisent `.` pour désigner le récepteur (receiver).

### Syntaxe

```no
#{fmt-skip}
type.method-name = (params) (results) {
    ; . est le récepteur
}
```

### Règles

1. Le nom de méthode utilise le format `type.method` ; `type` doit être un type déjà défini.
2. Le récepteur n'a pas besoin d'être déclaré explicitement comme paramètre ; on le désigne par `.` dans le corps de la méthode.
3. À l'appel, on utilise la syntaxe `receiver.method(args)`.
4. Les valeurs de retour vont dans le deuxième groupe de parenthèses, comme pour une fonction ordinaire.
5. **Sucre syntaxique désugaré** : `type.method = (inputs) (rest-outputs...) {}` équivaut à la fonction ordinaire  
   `method = (inputs) (self type, rest-outputs...) {}`. Lors de l'appel `instance.method(args)`,  
   `instance` est lié au premier paramètre de la liste de sortie, `self` ; dans le corps, `.` désigne ce `self` de la zone de sortie ;  
   les écritures sur `self` modifient directement l'instance d'origine. Les autres sorties, hormis `self`,  
   acceptent une déconstruction optionnelle ou peuvent simplement être jetées.

### Exemple

```no
; Méthode str
str.to-upper = () (out str) {
    n = .len-bytes()
    b []byte = with-cap-len(n, n)
    i <- [0..n): {
        c ?byte = .byte(i)
        v = c: {
            ok -> it

            nil -> 0

            err -> 0
        }
        {
            v >= 97 && v <= 122 -> b[i] = v - 32

            -> b[i] = v
        }
    }
    out = b.to-bytes-str()
}

; Méthode char
char.is-digit = () (result bool) {
    result = false
    . >= 48 && . <= 57 -> result = true
}

; Méthode struct
user {
    name str
    age i64
}

user.greet = () {
    print('Hello, ' - .name)
}

; Appel
s = 'hello'
u = s.to-upper(); receiver.method()
c char = 5
d = c.is-digit(); receiver.method()
u = user {
    name: 'Alice'
    age: 30
}
u.greet()
```

## Interfaces

```no
; Définition d'une interface
json {
    to-json()
}

; Implémentation par défaut d'une interface
json.to-json = () {
}

noml {
    to-noml()
}

serialize {
    json
    noml
}

serialize = json | noml
serialize = json & noml

; Implémentation d'interface
user json {
    name str
    age i64
}

; Redéfinition + appel de l'implémentation parente
user.to-json = () {

    ; Implémentation parente
    ..to-json()
}

user.other = () {

    ; Implémentation courante
    .to-json()

    ; Implémentation parente
    ..to-json()
}
```

### Fusion d'interfaces

Une interface peut « fusionner » les méthodes d'autres interfaces : les noms d'interfaces parentes s'écrivent dans le corps de l'interface, **un par ligne** (avec préfixe de module possible), sans virgule ni parenthèses :

```no
json {
    to-json()
}
noml {
    to-noml()
}

serialize {
    json
    noml
}

; Fusion transitive : roundtrip obtient en même temps toutes les méthodes de serialize, json et noml
roundtrip {
    serialize
    roundtrip-json()
}
```

Règles :

- **Fusion récursive** : la fusion est transitive — si `roundtrip` fusionne `serialize`, qui fusionne à son tour `json` / `noml`, alors `roundtrip` possède les méthodes des trois interfaces.
- **Un par ligne, sans virgule** : `serialize { json, noml }` a la même forme qu'une enum de style C (`color { red, green }`) ; il est donc toujours analysé comme une enum, et non comme une fusion d'interfaces.
- **Méthodes de même nom** : signature identique (paramètres et types de retour égaux, le nom du récepteur pouvant différer) ⇒ déduplication, on n'en conserve qu'une ; signature différente ⇒ erreur de diagnostic.
- **Côté implémentation** : le type n'a besoin d'écrire que la déclaration d'implémentation (liste préfixe `user serialize { name str }`) ; les méthodes des interfaces parentes comptent aussi comme implémentées, et la vérification de signature traverse les interfaces parentes.
- **`no fmt`** : l'ancienne écriture en « liste préfixe » (`serialize json, noml { }`) reste analysable, mais `no fmt` la réécrit en forme interne et `no vet` émet un avertissement.

Diagnostics (`no vet` et diagnostics de l'éditeur ont la même source, TraceID `ifmrg001`) :

| Situation | Message |
| --- | --- |
| Interface parente non déclarée | `interface 'serialize': unknown parent interface 'noml'` |
| Cycle de fusion | `interface 'a': merge cycle a -> b -> a` |
| Même nom, signature différente | `interface 'serialize': merged method 'to-str' has conflicting signatures ('json.to-str', 'noml.to-str')` |

> Une écriture comme `serialize = json \| noml` est une syntaxe de **type union**, sans rapport avec la fusion d'interfaces ; la fusion n'influe que sur « quelles méthodes un type doit posséder ».

### Interfaces spéciales

```no
file {
    enter
    leave
}
```

## Énumérations

```no
; red=0, green=1, blue=2
color {
    red,
    green,
    blue,
}

; Dans une méthode ordinaire, a,b,c sont en fait définis comme a=0, b=1, c=2... C'est une différence avec les autres langages.
; On ne peut donc normalement pas définir plusieurs variables avec des virgules.

; Ceci est une énumération spéciale : elle peut avoir des types, des virgules et des alias
enum-name {
    a t,
    b u,
    c v,
}

; Attention, ceci est un struct ordinaire : plusieurs champs sans virgule
struct-name {
    a t
    b u
    c v
}
```

### Énumérations taguées (avec payload)

Une variante peut porter des champs de payload nommés, écrits sous la forme `variante(champ type)` ; une variante sans payload s'écrit directement en nom nu.  
Une variante peut porter **plusieurs** champs (un payload à champs multiples est représenté sur le tas par un struct synthétique) :

```no
#{fmt-skip}
; Énumération taguée : les variantes peuvent porter un payload, tag 0,1,2... selon l'ordre de déclaration
result {
    ok(v t),
    nil,
    err(e str),
}

; Valeur : champ de payload v, type t
; Vide : aucun payload
; Erreur : champ de payload e, type str (énumération définie par l'utilisateur, sans lien avec l'option intégrée)

shape {
    circle(r f64),
    rect(w f64, h f64),
    dot,
}
; Un champ
; Plusieurs champs
; Aucun payload
```
```

Lors du filtrage, on utilise le nom de variante comme motif d'arm ; une variante avec payload peut être déconstruite pour lier son payload (les champs multiples sont liés un à un par position) :

```no
r result = ok(42)
r: {
    ok(v) -> print(v); lie le payload de ok à v

    nil -> print('empty')

    err(e) -> print(e)
}

s shape = rect(3.0, 4.0)
s: {
    circle(r) -> print(r); lie un champ unique

    rect(w, h) -> print(w * h); lie plusieurs champs par position

    dot -> print('dot')
}
```

Une variante peut être construite directement en **position d'expression**, comme argument de fonction ou payload d'une autre variante :

```no
x f64 = perimeter(rect(3.0, 4.0)); construction inline et passage en argument
o outer = wrap(a(5)); le payload d'une énumération est lui-même une autre énumération
```

> **Espace de noms et résolution des noms nus** : le compilateur enregistre les variantes sous leur « nom pleinement qualifié » (`module.enum.variante`,  
> comme `option.option.ok` ou `some-mod.my-result.ok`) ; les variantes de même nom dans des énumérations différentes ne se confondent pas.  
> L'utilisateur n'écrit que le nom nu (`ok`, `rect`) ; le compilateur complète automatiquement le nom complet  
> **d'après le type statique de la variable filtrée ou construite**. Ainsi, deux énumérations ont beau avoir toutes deux une variante `ok` avec un ordre de tag différent, elles sont correctement distinguées.
>
> On peut aussi écrire l'ancienne forme `ok t` séparée par un espace, équivalente à `ok(v t)` (nom de champ omis).  
> Les noms de variantes des énumérations taguées sont enregistrés dans la table des variantes du compilateur, utilisée pour le filtrage et la vérification d'exhaustivité.

### Énumérations taguées intégrées (`#{buildin}`)

Préfixer une énumération taguée par `#{buildin}` indique qu'il s'agit d'une **énumération intégrée** : ses variantes (noms, ordre, types de payload) servent  
au filtrage et à la vérification d'exhaustivité, mais sa représentation sous-jacente et sa construction sont fournies par le runtime/builtin ; **aucun  
struct/union visible par l'utilisateur n'est généré**. L'option `?t` est déclarée de cette manière :

```no
; src/std/global.no
#{buildin}
option {
    ok(v t),
    nil,
    err(e err.err),
}
```

Les tags de `option` sont, dans l'ordre, `0=ok`, `1=nil`, `2=err`, conformes à la représentation intégrée de l'option. L'utilisateur n'a pas besoin  
de déclarer lui-même l'option ; le `#{buildin}` ici unifie la source des variantes de l'option intégrée (voir « Type nullable (option) »).

### Annotations d'énumération et disposition en mémoire

Les énumérations prennent en charge les annotations `#{...}`, aux mêmes positions que les autres cibles (voir « Position des annotations ») : **au-dessus** (sur une ligne distincte) ou  
**derrière** (en fin de ligne) ; les écrire **sur la même ligne, devant** la cible est une erreur de compilation.

Les annotations peuvent se placer à **deux endroits** :

1. **Au-dessus de toute l'enum** — s'applique à la définition elle-même (comme `#{buildin}` ou `#{inline=true}`) ;
2. **Au-dessus d'un membre unique ou en fin de sa ligne** — s'applique à ce membre (valeur des enums de style C comme variante des énumérations taguées).

```no
; ① Enum entière : sur une ligne distincte au-dessus de l'enum
#{inline=true}
box {
    full(v i64) #{doc='有值'},
    empty #{doc='空值'},
}

; ② Variante unique : sur une ligne distincte au-dessus de la variante

; ② Variante unique : en fin de ligne

; `#{inline=false}` : écrit explicitement « n'utilise pas la disposition inline »
#{inline=false}
plain {
    a,
    b,
}

color {
    red #{deprecated},
    green #{deprecated},
    blue,
}
; Les valeurs uniques des enums de style C prennent elles aussi en charge ces deux écritures
```

**Les annotations ne modifient pas la structure de l'enum** : noms de variantes, ordre de déclaration (tag), champs de payload et types ne sont pas affectés,  
et le filtrage comme la vérification d'exhaustivité restent strictement identiques. Les annotations ne sont que des **métadonnées** attachées à la définition.

#### Disposition en mémoire (forme stack)

Les énumérations taguées utilisent une **disposition en stack** (forme inline sur la pile) : un petit objet stocké par valeur, dont le payload n'est pas éparpillé sur le tas.

```llvm
%tenum_<name> = type { iN tag, [K x i64] payload }   ; cas général
%tenum_<name> = type { iN tag }                      ; toutes les variantes sans payload
```

- La largeur de `tag` dépend du **nombre de variantes** et porte sur `u8/u16/u32/u64` complets :
  `<= 256` → `i8`, `<= 65536` → `i16`, `< 2^32` → `i32`, sinon `i64`. La valeur discriminante suit l'ordre de déclaration `0, 1, 2...`.
- `payload` est une union **partagée par toutes les variantes** (chacune écrit ses champs dans le même stockage) ; `K` est le nombre de mots de 8 octets
  nécessaires à la **variante la plus large**, et **non la somme de toutes les variantes**, avec un minimum de 1.
- À l'intérieur d'une variante, les champs sont placés selon leur **alignement naturel** ; les décalages ne sont donc pas forcément des multiples de 8 (pour `(a char, b char, c char)`,
  les décalages sont 0 / 4 / 8). Les lectures et écritures utilisent toujours un **décalage en octets** (`bitcast` vers `i8*` puis `getelementptr`) ;
  le « décalage d'écriture » et le « décalage de lecture » sont donc déterminés par la même table de disposition.

La taille est `8 + 8K` (le `tag` est rempli jusqu'à un mot entier par l'alignement sur 8 octets du tableau) ; seules les énumérations sans payload ont une taille de pur tag :

| Cas | `K` | Taille |
| ---------------------------------------------- | ----------------- | ---------- |
| Toutes les variantes sans payload | — (pas de champ payload) | **1 octet** (`{ i8 }` ; 2 / 4 / 8 si > 256 variantes) |
| Variante la plus large : `i64` / `i32` / `f64` / pointeur | 1 | 16 octets |
| Variante la plus large : `char` (4 octets) / `bool` (1 octet) | 1 | 16 octets |
| Variante la plus large : `(a char, b char, c char)` (12 octets) | 2 | 24 octets |
| Variante la plus large : `str` / `vec` / `[]T` (descripteur de 16 octets) | 2 | 24 octets |
| Variante la plus large : struct `T` | `ceil(sizeof(T) / 8)` | `8 + 8K` |

Autrement dit : **par défaut, la taille démarre à 16 octets**, et ne grandit que si le payload d'une variante est réellement plus large —
« 16 octets fixes par défaut » et « taille variable selon la variante » sont les deux faces d'une même réalité. Les petits payloads comme `char` / `bool`
ne font que déterminer l'alignement, sans élargir l'union : la largeur de `payload` est « la variante la plus large », et non la somme de toutes.

> L'unique source de la disposition est `src/mir/layout.go` : `tagWidth` détermine `iN`, `enumVariantLayout` détermine les décalages de champs et la taille propre de chaque
> variante, et `enumUnionLayout` prend le maximum pour obtenir `K`. `hir2mir.fillEnumLayout` enregistre ces nombres dans `TaggedEnumInfo` au moment de la construction de la table ;
> l'emitter ne fait que lire, sans calculer — les deux côtés ne peuvent donc pas aboutir à des résultats différents.

#### Les trois écritures de `#{inline}`

`inline` est une annotation **booléenne**, avec trois écritures, et **sa valeur est réellement respectée** :

| Écriture | Signification |
| ----------------- | --------------------------- |
| `#{inline}` | Forme courte, équivalente à `inline=true` (une clé seule vaut vrai) |
| `#{inline=true}` | Explicitement vrai (`#{inline=1}` fonctionne aussi) |
| `#{inline=false}` | Explicitement faux (`#{inline=0}` fonctionne aussi) |

Une valeur non booléenne est une **erreur de compilation** (`#{inline=foo}`, `#{inline='true'}`), jamais silencieusement traitée comme vraie :  
`ValidateFieldTags` (TraceID `fieldtag1`) signale à la fois les champs de structure et les définitions d'enum.

Écrire `#{inline=true}` au-dessus d'une enum entière est une **marque explicite** de la disposition stack, indiquant « cette énumération utilise la disposition en stack » ;  
le compilateur l'enregistre dans `TaggedEnumInfo.Inline`. **La disposition par défaut actuelle est déjà la forme stack** ; aussi, avec ou sans annotation,
`true` ou `false`, la disposition est rigoureusement identique — la marque sert à rendre l'intention explicite et à constituer un commutateur stable si la disposition par défaut
venait à changer ultérieurement.

> `#{inline}` écrit **au-dessus de toute l'enum** signifie « cette énumération utilise la disposition en stack » ; écrit sur **une variante unique**, il ne change pas
> actuellement la disposition et reste uniquement une métadonnée de cette variante (contrairement aux champs de structure, voir la section suivante).

#### `#{inline}` sur les champs de structure

Contrairement aux enums, `#{inline}` sur un **champ de structure** a un **effet réel** : il détermine directement si le champ est  
**inliné par valeur** dans la structure hôte (`%pt`) ou stocké comme **pointeur** (`ptr`).

```no
pt {
    x i64
    y i64
}

holder {
    inl pt #{inline=true}; par valeur : la structure se développe directement dans holder
    out pt #{inline=false}; pointeur : le champ stocke un pointeur vers pt
    bare pt #{inline}; forme courte, équivalente à inline=true
}
```

| Écriture | Disposition du champ |
| ---------------------------------------------- | --------------------------- |
| `#{inline}` / `#{inline=true}` / `#{inline=1}` | **Par valeur**, inliné dans l'hôte (`%pt`) |
| `#{inline=false}` / `#{inline=0}` | **Pointeur** (`ptr`) |
| Absente | Dépend de `NOLANG_FIELD_PTR` (voir le tableau ci-dessous) |
| `#{inline=foo}` / `#{inline='true'}` | **Erreur de compilation** — non booléen |

**Sans annotation, la disposition par défaut est déterminée par `NOLANG_FIELD_PTR`.** `mir.FieldPtrLayout` vaut  
`os.Getenv("NOLANG_FIELD_PTR") != ""`, **désactivé par défaut** ; l'activer déclenche le « Phase 1 layout flip »,  
qui fait passer le défaut des champs de structure de la valeur au pointeur.

| Champ | Drapeau désactivé (défaut) | Drapeau activé (`NOLANG_FIELD_PTR=1`) |
| ------------------------------ | --------- | ------------------------- |
| Sans annotation | `%pt` (par valeur) | `ptr` (pointeur) |
| `#{inline}` / `#{inline=true}` | `%pt` | `%pt` |
| `#{inline=false}` | `ptr` | `ptr` |

Autrement dit : **l'annotation s'applique toujours, indépendamment de la variable d'environnement** ; le drapeau ne modifie que le défaut « lorsqu'aucune annotation n'est écrite ». Ainsi,
dans la configuration par défaut, `#{inline=false}` signifie « demander explicitement un pointeur », tandis que `#{inline=true}` ne fait que réécrire une disposition déjà en vigueur ;
une fois le basculement activé, les rôles s'inversent.

Le vérificateur applique deux règles (TraceID `fieldtag1`, `ValidateFieldTags`) :

1. **N'a de sens que pour les champs de type structure.** Les champs scalaire / `str` / `vec` / tableau / tranche / map / option /  
   pointeur sont de toute façon toujours stockés par valeur : écrire `#{inline}` n'apporte rien et provoque une erreur ; écrire  
   `#{inline=false}` provoque également une erreur, car ce serait déclarer une disposition contraire au comportement du compilateur.
2. **L'inlining par valeur ne doit pas former de cycle.** La structure inlinée est stockée **à l'intérieur** de l'hôte, donc  
   `a { #{inline} b b }` et `b { #{inline} a a }` coexistant donneraient une taille infinie et doivent être refusés (l'auto-référence directe est un cas dégénéré). Seul `=true` crée un arc par valeur ; aussi des structures
   mutuellement récursives sont valides si **les deux écrivent `#{inline=false}`**.
   > ⚠️ **« Ne pas écrire d'annotation » ne signifie pas « ne pas utiliser inline ».** Dans la configuration par défaut, l'absence d'annotation signifie inlining par valeur, donc  
   > `a { x b }` avec `b { y a }` (sans annotation) **n'est pas valide en configuration par défaut** et échouera avec  
   > `opt: error: identified structure type 'b' is recursive` — erreur qui, actuellement, **n'est pas** signalée par le vérificateur, mais n'explose qu'à la phase de validation LLVM. Pour écrire des types récursifs en configuration par défaut, il **faut**  
   > écrire explicitement `#{inline=false}`. Ce n'est que lorsque `NOLANG_FIELD_PTR=1` que « ne rien écrire » équivaut à un pointeur.

#### Types récursifs : pourquoi `inline=false` est nécessaire

Une structure qui s'inline par elle-même a une taille infinie : une **auto-référence** ne peut donc pas être stockée par valeur. Dans la configuration par défaut  
(`NOLANG_FIELD_PTR` désactivé), les champs de structure sont par défaut par valeur ; pour que `node` puisse se référencer lui-même,  
la seule écriture possible est de demander explicitement un pointeur :

```no
node {
    v i64
    next node #{inline=false}; pointeur : c'est ainsi que node a une taille finie
}

n node
n.v = 1
n.next.v = 2
print(n.next.v); 2
```

Si l'on retire `#{inline=false}` ci-dessus (ou qu'on l'écrit `#{inline=true}`), LLVM rejette directement ce  
type : `identified structure type 'node' is recursive`.

Un champ pointeur **possède** son pointee, dont le cycle de vie est géré par le compilateur :

- le pointee est **alloué paresseusement à la première écriture** (`@__nolang_get_<T>`, contenant `malloc` + remise à zéro),  
  et non alloué en une fois à la construction de l'hôte ;
- quand l'hôte quitte sa portée, `@__nolang_drop_<T>` le **libère après un contrôle de pointeur nul** ;
- une copie par valeur (`b = a`, passage en paramètre, écriture dans un conteneur) **copie en profondeur** le pointee ; deux hôtes ne partagent donc
  pas la même mémoire et ne sont pas libérés deux fois.

Le test de bout en bout se trouve dans `tests/field-inline-annotation.no`.

### Référence aux valeurs d'énumération

**Règle : une valeur d'énumération doit être référencée de façon qualifiée `type_enum.valeur`, jamais en nom nu direct.**  
Cela évite les conflits de noms et empêche les paquets externes d'utiliser directement une valeur concrète.

```no
#{fmt-skip}
; ❌ Erreur : utilisation directe d'une valeur nue
kind = null
yes = e.is(io)
```
```

```no
; ✅ Correct : utilisation qualifiée
kind = json-kind.null
yes = e.is(code.io)
```

> Un type énumération peut être utilisé comme type de champ de structure, type de paramètre de fonction et type de retour.  
> Que ce soit à l'intérieur ou à l'extérieur du module qui définit l'énumération, les valeurs doivent être référencées sous la forme `type_enum.valeur`.

## enter/leave

Les types qui implémentent les interfaces `enter` / `leave` sont appelés automatiquement à l'entrée et à la sortie de la portée :

```no
file enter, leave {
    path str
}

file.enter = () {
    .open()
}

file.leave = () {
    .close()
}

read-file = () {

    ; f.enter() automatique
    f = file {
        path: 'data.txt'
    }

    ; utilisation de f
    ; f.leave() automatique
    read(f)
}
```

### Type nullable (option)

Préfixer un type par `?` désigne un type nullable :

Une variable de type nullable peut légalement contenir une valeur nulle ou une erreur ; le compilateur effectue les vérifications correspondantes.

`?t` est une énumération taguée intégrée, dont les variantes sont définies dans `src/std/global.no` :

```no
#{buildin}
option {
    ok(v t),
    nil,
    err(e err.err),
}
; tag 0 : une valeur
; tag 1 : valeur nulle
; tag 2 : erreur (struct err.err de std)
```

Le payload de la variante `err` **n'est pas un `str`**, mais la structure `err` de `std/err` (`code` / `msg` / `file` / `line` / `col`).
`err('msg', code)` est un constructeur alias de `err.new('msg', code)` — le code d'erreur `code` est un paramètre **obligatoire**
(les valeurs de paramètre par défaut ont été retirées du langage) ; en l'absence de code précis, passez explicitement `code.unknown` ;
les champs de position sont injectés automatiquement par le compilateur au point de construction. `err('msg')` avec un seul argument n'est pas cet alias,
mais le constructeur de la variante `err` de l'option (voir plus bas).

`#{buildin}` signifie que la représentation sous-jacente et la construction sont fournies par le runtime intégré (type `%option`, constructeurs
`ok(...)`/`nil`/`err(...)`) ; la déclaration d'énumération de `option` ne sert qu'au filtrage et à la vérification d'exhaustivité. Voir « Énumérations taguées » et « Énumérations taguées intégrées ».

#### Disposition en mémoire

`?T` est **typé par payload** : chaque type de payload a son propre type LLVM, dont le nom est dérivé du type de payload (application injective),
si bien que « même type de payload ⇒ même disposition » ; `?i32` / `?i64` / `?bool` partagent le même type :

```llvm
%option_<key> = type { i8 tag, [K x i64] payload }   ; en ligne
%option_<key> = type { i8 tag, [1 x i64] payload }   ; boxé (payload stocke un pointeur i8*)
```

- Le `tag` fait **toujours 1 octet** : `option` n'a que 3 variantes (`0=ok`, `1=nil`, `2=err`), ce qui tombe dans la tranche
  « `<= 256` variantes → `i8` ». La règle de largeur de tag des énumérations est décrite dans « Disposition en mémoire (forme stack) ».
- Après le tag vient un **remplissage d'alignement** (tag de 1 octet → 7 octets de remplissage), puis seulement le payload.
- La taille du payload `K` est déterminée par la taille réelle de `T` et arrondie à un multiple de 8 ; **ce n'est pas un emplacement fixe** :
  `?i64` fait 16 octets (8 + 8), `?str` 24 octets (8 + 16), `?txt` 264 octets (8 + 256).
- Un payload au-delà du seuil passe en **boxing sur le tas** : `payload` ne stocke qu'un pointeur de 8 octets, et l'option entière fait 16 octets.

Le seuil `option-payload-inline-threshold` se compte en octets sur **toute la structure option** (par défaut **264** = 1 octet de tag +
7 octets de remplissage + 256 octets de payload, minimum **16**) : la condition d'inlining du payload est
`8 + roundUp8(sizeof(T)) <= seuil`. Par défaut, « payload ne dépassant pas 256 octets est inliné », et `?txt`
(tampon inline de 256 octets) tombe exactement dans la limite.

| `T` de `?T` | Inliné ? | Taille de `%option_<key>` |
| --- | --- | --- |
| `i64` / `i32` / `bool` / `char` / `f64` / pointeur | oui | 16 octets |
| `str` / `[]T` / `vec` (descripteur de 16 octets) | oui | 24 octets |
| `txt` (tampon inline de 256 octets) | oui | 264 octets (exactement la limite par défaut) |
| Structure / tableau de taille > 256 octets | non | 16 octets (payload sur le tas) |
| Payload auto-référentiel (`node { next ?node }`) | non | 16 octets (l'auto-référence ne peut être rompue que par un pointeur) |

> **Le payload de `err` est toujours boxé** : la variante `err` stocke un pointeur vers la structure `err` de `std/err` ; elle n'utilise pas le
> budget de payload ci-dessus. Ainsi `?str` fait 24 octets avec une chaîne inlinée pour `ok`, et reste le même `?T` à 24 octets pour `err` —
> seul le word `payload` stocke un pointeur vers l'objet erreur. Cette règle permet à « un même `?T`, deux variantes, deux payloads » de n'exiger aucun espace supplémentaire.

> **Inlining et boxing ne changent que « où cela est stocké », pas la propriété** : les deux chemins créent, lors d'une **copie**, une copie distincte de la partie tas du payload de structure
> (champs pointeurs copiés récursivement, buffers `str` / `[]T` possédés en inline clonés) ; la sémantique de valeur de `?T` est donc indépendante
> de l'inlining — un même programme doit produire exactement le même résultat pour n'importe quel `option-payload-inline-threshold`.
> Un comportement qui change à un seuil près serait un défaut du compilateur, pas une fonctionnalité. (Une affectation en pur transfert ne clone pas : l'option reprend
> le tas de la source.)

> L'unique source de la disposition est `src/mir/layout.go` : `optLayoutOfPayload` détermine `K` et le boxing,
> `optionPayloadOffset` (8) est le décalage en octets fixe du payload. `hir2mir` replie les constantes `?T` de niveau module et
> le codegen émet les lectures/écritures : tous interrogent la même fonction.

#### Propagation d'erreur `?=` à payload hétérogène

`?=`Copie d'abord l'option à la **racine de la chaîne RHS** dans le paramètre de résultat option, afin que `nil` / `err` soient directement propagés,
le chemin de succès étant ensuite écrasé par l'arm `ok` :

```no
pick = (p pool) (out ?i64) {
    out ?= p.nodes[0].kind; la racine est ?node, l'emplacement résultat est ?i64
}
```

Les types de payload des deux côtés peuvent donc **différer**, et c'est une véritable frontière ABI (`?node` 112 octets, `?i64` 16 octets),
et non deux orthographes d'une même disposition. Entre types hétérogènes, seules deux choses passent :

- le `tag` — octet 0 de chaque option ;
- `err` — la variante `err` est une propriété de l'option, indépendante du payload, et toujours boxée ; des deux côtés, le premier mot de la zone payload est donc
  un pointeur de boîte de 8 octets (la boîte est **copiée**, non partagée, car les deux côtés font un drop).

Le payload `ok` **n'est pas transporté entre types** : la zone payload de la cible est d'abord remise à zéro, et la valeur de succès est toujours réécrite par l'arm `ok`.
L'implémentation est dans `@__nolang_opt_prop_<dst>__<src>` (`optPropagateState` dans `src/mir/codegen.go`),
qui n'apparaît que sur les affectations de propagation générées par le compilateur, comme `?=`.

```no
o ?i64
o = nil; mis à vide
o = 42; mis à une valeur
o = err('msg'); mis à une erreur

nullableValue ?[]str
nullableString ?str

; Modifier le type nullable
nullableString = 'test'

; Définir une erreur
nullableString = err('some error')

; Peut être testé par match
x: {
    err -> log(it.msg()); it est err.err, le message via .msg()

    nil -> log('nil')

    -> do-right-thing(it)
}

; Déballage forcé
; Implémentation annulée
; !x.say()
```

### Guide de style : préférer l'option `?t` à `(val, ok)`

Lorsqu'une fonction peut échouer ou renvoyer une valeur vide, **privilégiez le type option `?t`** plutôt que le motif de double retour `(val t, ok bool)`.

`?t` est une énumération taguée avec trois états : `ok` (une valeur), `nil` (valeur vide), `err` (erreur). La valeur normale est implicitement liée ; utilisez `nil` lorsque l'opération ne fait pas simplement trouver de valeur, et `err(...)` lorsqu'elle rencontre une véritable erreur.

```no
#{fmt-skip}
; ❌ Déconseillé : motif de double retour
stack.pop = () (val i64, ok bool) {
    .n == 0 -> return
    val = .data[.n]
    ok = true
}
```
```

```no
; ✅ Recommandé : type option (nil pour vide, err pour erreur)
stack.pop = () (val ?i64) {
    .n == 0 -> {
        val = nil
        return
    }
    val = .data[.n]
}

; ✅ Renvoi d'erreur
file.read = () (data ?str) {
    .fd < 0 -> {
        data = err('file not open')
        return
    }

    ; ... lecture des données
    data = buf
}
```

Utilisez un match pour déballer l'option :

```no
val = s.pop()
val: {
    nil -> print('empty')

    err -> print(it.msg()); it = l'erreur structurée err.err

    -> print(it); it = la valeur extraite
}
```

## Débordement des calculs entiers (annotation `#{overflow}`)

Nolang ne prend pas en charge le panic — toute opération susceptible de déborder doit être traitée par une `option` ou une annotation explicite, et ne lève jamais d'exception de débordement à l'exécution. Le comportement en cas de débordement des **quatre opérations arithmétiques entières** suivantes est contrôlé par l'annotation `#{overflow = ...}` :

- **`+` `-` `*`** signés et non signés : s'applique lorsque les deux opérandes sont des entiers (y compris les littéraux `int`) ;
- **`/`** signé : seul le cas `INT_MIN / -1` peut déborder (la division non signée `a/b ≤ a` ne déborde jamais et n'est pas concernée).

> Les entiers non signés (`u8`/`u16`/`u32`/`u64`/`u128`) suivent, pour `+ - *`, **exactement les mêmes règles** que les entiers signés : sans annotation, ils renvoient eux aussi par défaut `option<int>`, débordement → `err`. C'est un choix délibéré — « dès qu'on ne peut pas prouver à la compilation qu'un débordement est impossible, on applique la même règle ».

### Cas particuliers : arithmétique de `byte` et `char`

`byte` et `char` sont des **cas particuliers** de la sémantique de débordement : leur arithmétique **ne produit pas d'option** et n'est pas soumise à l'annotation `#{overflow}`.

- **`byte` (u8)** : `+ - *` déborde par **retour silencieux en complément à 2** (`250 + 10` → `4`, `0 - 1` → `255`). `byte` vise les tampons d'octets, le décodage hex, etc., des scénarios où le retournement est « naturellement admis » ; même sémantique que `#{overflow = wrap}` pour les hashs/compteurs, mais intégrée, sans annotation.
- **`char` (point de code Unicode)** : `+ -` est une **arithmétique de points de code** ; lorsque le résultat sort de la plage valide (`< 0` ou `> 0x10FFFF`), il vaut **`-1`** (sentinelle de point de code invalide) — pas d'`err`, pas de retournement.

**Raison de conception** : `i8`–`i128` / `u8`–`u128` servent en général à des **valeurs métier réelles** (montants, comptages, capacités), où un retournement silencieux masquerait un bug de débordement ; d'où `err` par défaut et un choix explicite obligatoire. Les usages typiques de `byte` / `char` (flux d'octets, parcours de points de code) admettent — voire attendent — naturellement le retournement ; passer par une option n'ajouterait que du bruit de déballage. Pour détecter un débordement sur `byte` / `char`, convertissez d'abord vers un type plus large (comme `b.to-i64()`) puis calculez.

| Mode | Annotation | En cas de débordement | Type de résultat | Scénario d'application |
| ----------- | ------------------------ | --------------------------- | ------ | ------------------ |
| **Défaut (sans annotation)** | — | débordement → `err`, normal → `ok(value)` | `?int` | calculs où le débordement doit être détecté |
| **Retournement** | `#{overflow = wrap}` | retour silencieux en complément à 2 | `?int` | hash, cryptographie, compteurs, où le retournement est sémantiquement admis |
| **Mise à zéro** | `#{overflow = clamp0}` | débordement (haut ou bas) donne `0` | `?int` | sémantique où la différence ne doit pas être négative (reste, etc.) |
| **Borne minimale** | `#{overflow = min}` | débordement ramené au minimum du type | `?int` | plancher de compteur, garde d'index |
| **Borne maximale** | `#{overflow = max}` | débordement ramené au maximum du type | `?int` | plafond de capacité, accumulation saturée |
| **Saturation** | `#{overflow = saturate}` | débordement haut→max, bas→min | `?int` | saturation de signal/couleur, etc. |

> ⚠️ **L'annotation ne déballe pas** (D1) : `#{overflow = …}` **détermine seulement « comment obtenir la valeur en cas d'échec »**,
> le type de résultat reste toujours `?T`. Pour ramener le résultat à un type simple, il faut le consommer par un match ou `?=` ; les deux formes de consommation
> (`?=` + match de bloc / match à position de valeur) et leur choix sont décrits dans « Choisir entre block match et value-position match ».
> Annoter explicitement un type simple (`w i64 = a - b  #{overflow=wrap}`) **n'est pas** une échappatoire :
> en pratique, l'erreur `15w45dqk` est toujours signalée (cannot assign `?i8` value to `i8` variable).

De plus, tous les modes prennent en charge la **forme à préfixe de type**, qui fixe précisément les bornes de saturation d'un type étroit donné, par exemple `#{overflow = u8-max}`, `#{overflow = i8-min}`, `#{overflow = u16-saturate}`.

> **Granularité de l'annotation** : `#{overflow = ...}` est une **annotation de ligne** — elle ne s'applique qu'à **l'instruction suivante immédiate**.
> C'est la **seule forme autorisée par conception** (pour éviter les abus) : la sémantique d'exécution (codegen) et l'erreur bloquante `ovfhndld` ne reconnaissent qu'elle,
> et elle ne modifie que « la valeur en cas de débordement », pas le type ; l'étendre au niveau bloc ou fonction ferait dépendre la sémantique de tout un groupe de calculs d'une annotation
> située à des lignes de distance. Les raisons sont détaillées dans [〈Portée des annotations et des commentaires〉](annotation-scope.md) §4.1.
>
> ⛔ **Il n'est pas valide de la placer au-dessus d'une définition de fonction, en tête de bloc ou en tête de fichier.** La seule faille actuelle : une annotation de niveau fonction fait
> que le lint `ovf-int-default` saute toute la fonction (la liste paraît « propre »), tandis que les calculs non annotés dans le corps déclenchent quand même `ovfhndld` ; une annotation de niveau bloc
> fait que le lint l'hérite vers le bas et se tait, mais le codegen ne la reconnaît jamais. Annotz instruction par instruction ;
> `no fmt --fix=overflow` produit précisément cette forme par instruction.
>
> Le modèle complet (L0 module / L1 déclaration / L2 bloc / L3 ligne unique, et quelles clés sont prises en charge à quels niveaux) figure dans
> [〈Portée des annotations et des commentaires〉](annotation-scope.md) ; sa §7 recense l'existant et les failles, sa §8 l'ordre de resserrement.

> **Correctif rapide LSP** : l'éditeur (nolang-lsp) signale les calculs entiers non annotés par une **note** (`nolang-overflow`, trace id `ovfhndld`) et propose cinq quickfix — **Add `#{overflow = wrap}`** / **`clamp0`** / **`min`** / **`max`** / **`saturate`** — qui insèrent automatiquement l'annotation correspondante au-dessus de l'instruction concernée, avec la même indentation que la ligne, pour fixer le comportement en cas de débordement (le résultat reste `?T`, à consommer par match / `?=`). L'équivalent en ligne de commande est `no fmt --fix=overflow` (voir ci-dessous).

### Défaut : renvoi de `option<int>`

Sans annotation, le résultat de `a - b` est de type `option<int>`. Le récepteur doit être un `?T` (type nullable), et l'on déconstruit les trois branches `err` / `nil` / `ok` par un match :

```nolang
main = () {
    x i64 = 100
    d ?i64 = x - 1; défaut : renvoie option<i64>
    d: {
        err -> print(-1); débordement (par ex. x - 2^63 en sous-flux)

        nil -> print(0)

        -> print(1); ok(value) : résultat normal
    }
}
main()
```

> Le champ `data` de `%option` est un `i64` ; les résultats de types étroits (`i8`/`i16`/`i32`) sont d'abord étendus en signe (sext) puis stockés, et tronqués à la largeur d'origine lors du déballage.

### wrap : retournement silencieux

```nolang
#{overflow=wrap}
sub-wrap = (a i64, b i64) (r i64) {
    r = a - b; retour silencieux en complément à 2 en cas de débordement ; r est un i64 simple (type explicite)
}
```

Au niveau instruction :

```nolang
x i64 = -9223372036854775807

#{overflow=wrap}
w i64 = x - 2; sous-flux avec retournement → 9223372036854775807
print(w)
```

### clamp0 : débordement mis à zéro

```nolang
x i64 = -9223372036854775807

#{overflow=clamp0}
c i64 = x - 2; sous-flux → 0 (le dépassement haut donne aussi 0)
print(c)
```

### min / max / saturate : bornage par saturation

Trois modes « écrasent » le résultat de débordement dans l'intervalle représentable du type, au lieu de le faire tourner ou de le mettre à zéro :

```nolang
#{overflow=min}
floor = (a i64, b i64) (r i64) {
    r = a - b; sous-flux → minimum i64 ; dépassement haut → maximum i64
}

#{overflow=max}
cap = (a i64, b i64) (r i64) {
    r = a * b; dépassement haut → maximum i64 ; sous-flux → minimum i64
}

#{overflow=saturate}
sat = (a i64, b i64) (r i64) {
    r = a + b; dépassement haut→max, sous-flux→min
}
```

La forme à préfixe de type permet de contrôler précisément les bornes de saturation d'un type étroit (une annotation de niveau fonction s'applique à tous les calculs du type correspondant dans le corps) :

```nolang
#{overflow=max}
inc = (x u8) (r u8) {
    r = x + 1; x = 255 déborde vers le haut → 255 (au lieu de tourner à 0)
}

#{overflow=min}
dec = (x i8) (r i8) {
    r = x - 1; x = -128 déborde vers le bas → -128 (au lieu de tourner à 127)
}
```

### Application par le compilateur

Un calcul entier non annoté renvoie par défaut `option<int>` ; « non traité » est donc bloqué. Le caractère « non traité » fait l'objet d'un **jugement parallèle** — l'une des conditions suivantes suffit à ne pas signaler d'erreur :

- **propagation par `?=`** (exige une fonction avec paramètre de résultat option) ;
- **capture sur place** : liaison par `=` à une variable **nouvelle** (la variable est inférée automatiquement en `?T`), ou déclaration explicite `?T` (comme `x ?i64 = a + b`) ;
- **rejet explicite `_ = expr`** (l'évaluation suit elle-même le chemin sûr, l'erreur est aussi ignorée) ;
- **ajout d'une annotation** `#{overflow = wrap}` / `clamp0` / `min` / `max` / `saturate` (ou forme à préfixe de type comme `u8-max`, `i8-min`), déclarant l'acceptation de la sémantique de retournement/saturation.

Ce n'est que lorsque les quatre font défaut que l'erreur est signalée (`ovfhndld`).

> **L'arithmétique pure n'est pas une source d'erreur potentielle** : `+ - * <<` et la négation ne déclenchent pas de capture (sinon toute arithmétique deviendrait option et la bibliothèque standard exploserait). Ainsi `d = x - 1` (sans `/` `%`, sans indexation sûre, sans opérande option) signale toujours l'erreur — ajoutez une annotation, ou écrivez `d ?i64 = x - 1`.
>
> **Une variable existante ne change pas de type** : la capture ne s'applique qu'à une cible « déclarée pour la première fois dans cette instruction ». Si la variable existe déjà (par exemple d'abord `d = 0` puis `d = x - 1`, ou d'abord `v i64 = 0` puis `v = arr[i]`), le compilateur ne la convertit pas sur place en option, mais signale une erreur vous demandant de choisir explicitement la sémantique.

Deux autres erreurs bloquantes ne sont pas concernées par les exemptions ci-dessus :

- un calcul entier non annoté affecté à une variable **`int` plain explicite** provoque une erreur du compilateur (`cannot assign ?i64 value to i64 variable`), vous obligeant à ajouter `#{overflow = wrap}` / `#{overflow = clamp0}` / `#{overflow = min}` / `#{overflow = max}` / `#{overflow = saturate}` (ou une forme à préfixe de type), ou à utiliser un `?T` nommé.
- un calcul entier non annoté affecté à une variable **déjà existante** et de type non option (`d = x - 1` après `d = 0`) provoque une erreur du compilateur vous suggérant d'ajouter une annotation ou de déclarer `?i64`, afin d'éviter un IR illégal.

Les autres règles :

- les calculs sur entiers non signés (`u8`/`u16`/`u32`/`u64`/`u128`) **suivent eux aussi** ce mécanisme : sans annotation, `+ - *` renvoient par défaut `option<int>`, débordement → `err`, comme pour les entiers signés ;
- les calculs `i128` ne prennent pas en charge la voie `option` (le champ `data` ne contient qu'un `i64`) ; ils dégénèrent uniformément en retournement silencieux pour préserver la justesse.

> **Pourquoi option par défaut et non wrap ?** Le retournement silencieux masque les bugs de débordement ; renvoyer `option` par défaut force l'appelant à traiter explicitement le débordement (ou à annoter `wrap` pour indiquer « j'accepte le retournement »), transformant « le débordement est-il autorisé ? » en une décision de conception visible.

### Division par zéro (division by zero)

Un diviseur `0` pour `/` et `%` entiers est un **comportement non défini (UB)**, chose **distincte** du débordement — aucune annotation ne l'exempte :

- **déterminable à la compilation** : le diviseur est une **constante littérale 0** (`10 / 0`, `a % 0`, `x / 0`) ⇒ **erreur bloquante** (trace id `div-by-zero`). `#{overflow = wrap}` ne l'exempte pas (cette annotation concerne le débordement, pas la division par zéro) ;
- **connu seulement à l'exécution** : le diviseur vient d'une variable / d'une entrée externe ⇒ le compilateur insère un contrôle de zéro **sur les deux chemins** ; seule la « façon de le signaler » diffère :

| Type de résultat | Condition de déclenchement | Diviseur à 0 |
| ----------- | ----------------------- | ------------------------------------------------------------------------------ |
| `?int` (défaut) | instruction sans annotation | tag option = `err`, à traiter par `?=` / match |
| `int` plain | instruction avec `#{overflow = ...}` | résultat forcé à `0`, avec `nolang: integer division or remainder by zero ...` imprimé sur **stderr** |

```nolang
safe = (a i64, b i64) (r ?i64) {
    r = a / b; b == 0 → err (l'appelant doit traiter)

}

#{overflow=wrap}
fast = (a i64, b i64) (r i64) {
    r = a / b; b == 0 → 0 et affiche une ligne de diagnostic sur stderr (plus de UB d'un sdiv nu)
}
```

> **Pourquoi `#{overflow = wrap}` ne peut pas exempter la garde de division par zéro** : il modifie le **comportement en cas de débordement** (le type de résultat reste toujours `?int`), pas la sûreté du diviseur. Si on l'autorisait à exempter la garde, le compilateur émettrait un `sdiv` nu et l'optimiseur aurait le droit de supposer que le diviseur n'est jamais 0 (en réduisant `a / 0` à une valeur arbitraire), tandis que l'`idiv` x86-64 planterait directement par #DE — à la fois UB et sans aucun diagnostic. La garde du diviseur zéro existe donc **indépendamment de l'annotation** ; si le chemin plain affiche une ligne supplémentaire sur stderr, c'est précisément parce qu'il n'a pas d'option que l'appelant puisse vérifier.
>
> **La même garde couvre aussi `sdiv (INT_MIN, -1)`** (résultat non représentable, UB lui aussi ; SIGFPE sur x86-64). Le chemin option l'intègre dans `err` ; le chemin plain prend la valeur de retournement en complément à 2 (`INT_MIN / -1 → INT_MIN`, `INT_MIN % -1 → 0`).

La division en virgule flottante n'est pas concernée : `1.0 / 0.0` est défini par IEEE-754 comme `inf` / `nan`, et non comme un UB.

### Calculs non annotés signalés comme erreur (`ovf-int-default`)

`no vet` signale les calculs entiers **non annotés** par une **note** (trace id `ovfhndld`, HINT). Raison : le `?T` par défaut est invisible pour l'utilisateur, qui doit en être informé — il peut ajouter `#{overflow = ...}` pour fixer le comportement en cas de débordement, ou consommer avec `?T` + `?=` / match / type plain explicite. Ce qui doit réellement bloquer la compilation, c'est `optunuse` (option ni propagée ni déballée).

### Correction automatique : `no fmt --fix=overflow`

Pour ajouter les annotations en masse, inutile de le faire à la main : utilisez le paramètre de catégorie de correction de `no fmt` :

```bash
no fmt --fix=overflow -w src/std          # correction sur place de tout un répertoire
no fmt --fix=overflow -d src/std/str.no   # affiche seulement le diff, n'écrit pas
no fmt --fix=overflow src/std/str.no      # affiche le contenu corrigé
```

La correction est **précise et non polluante** : elle insère `#{overflow=wrap}` uniquement au-dessus de l'instruction où le lint a effectivement signalé le calcul, sans toucher aux autres instructions, sans réordonner la mise en page, sans modifier les annotations existantes, et en restant idempotente à chaque nouvelle exécution (ce qui est déjà annoté ne change plus). C'est différent d'un simple `no fmt -w` — ce dernier est un reformatage complet.

> Après correction, relancez `no vet` pour confirmer que tout est revenu à zéro. Dans la grande majorité des cas, une seule passe suffit (`src/std` est passé de 4728 occurrences à 0). Seul cas de figure connu : le signalement tombe sur la **condition** d'un arm de match, alors que le corps de cet arm ne contient aucune instruction annotable — une annotation de ligne ne s'appliquant qu'à la première instruction qui la suit, elle ne peut pas couvrir le calcul de la condition de l'arm lui-même ; `no vet` le signalera alors à nouveau. Dans ce cas, extrayez d'abord ce calcul en instruction distincte avant de l'annoter.

### Correction automatique : annotations de type redondantes

Lorsque le **type déclaré d'une variable est exactement le même que le type inféré de la valeur de droite**, l'annotation est redondante (`no vet` le signale par le hint `tcpoxtfd`, `type annotation '<T>' can be omitted (inferred from value)`).

- **Par défaut, `no fmt` la supprime automatiquement** : `no fmt -w` retire les annotations redondantes lors du formatage (tout en effectuant le reformatage normal), `no fmt -d` prévisualise ce changement, et `-w` l'écrit.

```bash
no fmt -w main.no          # formate et retire les annotations redondantes
no fmt -d main.no          # affiche seulement le diff (y compris les suppressions d'annotation)
no fmt -w src/             # tout un répertoire
```

- **`--fix=redundant` (chirurgical, ne touche que les annotations)** : si vous voulez seulement supprimer les annotations redondantes sans reformatage complet (par exemple sur `src/std`, car `no fmt` ferait perdre `#{index-out}`), utilisez cette catégorie : elle ne supprime que les annotations, sans réorganiser la mise en page.

```bash
no fmt --fix=redundant -w src/std          # supprime sur place les annotations redondantes de tout un répertoire (sans reformatage)
no fmt --fix=redundant -d src/std/str.no   # affiche seulement le diff, n'écrit pas
no fmt --fix=redundant src/std/str.no      # affiche le contenu corrigé
```

```nolang
; Avant correction
x str = ''
b i64 = 5
m [str]i64 = make-map()

; Après correction (annotation == type inféré, supprimée)
x = ''
b = 5
m = make-map()
```

Points clés de la règle :

- L'annotation n'est supprimée que si le **type déclaré est égal au type inféré** (comme `str`/`i64`/`[str]i64` coïncidant avec leur valeur inférée).
- Les cas **convertibles implicitement mais non égaux** ne sont **pas** supprimés — garder l'annotation préserve la sémantique de conversion. Par exemple `c i16 = 5` (`5` est inféré `i64`, différent de `i16`), `p ?i64 = nil`, `last txt = ''` (`txt` et `str` sont convertibles implicitement l'un en l'autre, mais de types différents).
- Les écritures **sans annotation de type** (comme `d = 'x'`) ne sont pas signalées, et restent donc inchangées.
- Les annotations de tableau de longueur fixe `[N]T` / `[?]T` sont **toujours conservées** (jamais jugées redondantes). `[N]` n'est pas un simple indice de type, mais un commutateur qui détermine la **représentation** : ce n'est qu'en l'écrivant que le littéral `[...]` de droite est analysé comme un tableau de pile de longueur fixe ; ce n'est qu'en l'écrivant que le résultat inféré « tombe juste » sur l'annotation — un raisonnement circulaire créé par l'annotation elle-même. Supprimée, la même ligne est réanalysée en tranche extensible sur le tas `[]T`, et la sémantique change : le `.push()` de `fixed [3]char = ["x", "y", "z"]` est invalide (`.len()` vaut toujours 3), alors qu'après réduction en `fixed = ["x", "y", "z"]`, `.push()` ferait passer `.len()` à 4. L'annotation de tranche `[]T` n'est pas concernée — une fois supprimée, elle reste une tranche ; c'est une vraie redondance.
- Les valeurs **contenant des littéraux hexadécimaux conservent toujours l'annotation** (exception conservatrice). L'inférence de types de `no vet` traite `0xNN` avec une heuristique « inférer `byte` », différente de la sémantique réelle du compilateur « littéral entier sans annotation par défaut `i64` » ; aussi `IP-TBL [64]byte = [0x3a, ...]` et `s []byte = [0x50, 0x4b]` seraient à tort jugés redondants ; une fois supprimés, les littéraux dégraderaient en éléments `i64` et produiraient des erreurs de type comme `expected '[]byte', got '[]i64'`. Dès que le texte de l'expression de valeur contient `0x`/`0X`, ni le hint ni la suppression n'ont lieu.
- Les valeurs dont le **RHS est une lecture de champ conservent toujours l'annotation** (exception conservatrice). Dans une écriture comme `i i64 = .pos`, l'annotation n'est pas un simple indice de type, mais un commutateur qui détermine si l'arithmétique entière en aval produit un `?T` : en inférant depuis le type du champ, `i + N` n'est plus une « opération signée », ne renvoie plus `?i64`, l'arm `ok` du déballage en deux étapes n'est jamais atteint, et le compteur de boucle reste sur place. Le cas concret `parser.looks-like-map-entry` dans `src/std/yaml.no` : supprimer `i64` fait boucler `yaml.parse('abc')` à l'infini. Aussi les annotations sur `x = .field`, `x = obj.field` (y compris avec parenthèses ou préfixe enveloppant) ne déclenchent pas `tcpoxtfd` et ne sont supprimées ni par le `no fmt` par défaut ni par `--fix=redundant`.

Que ce soit par le `no fmt` par défaut ou par `--fix=redundant`, la suppression ne retire que l'annotation de type signalée et les espaces qui la précèdent, en restant idempotente (ce qui est déjà supprimé ne change plus). Après correction, il est conseillé de relancer `no vet` pour confirmer que `tcpoxtfd` est revenu à zéro.

### Correction automatique : consommer une option (`--fix=unwrap`)

Sous la règle unifiée, « produire une option sans jamais la consommer » est le seul diagnostic qui doit bloquer la compilation (trace `optunuse`). Pour le convertir en masse en forme de consommation explicite, utilisez `--fix=unwrap` :

```bash
no fmt --fix=unwrap -w src/std            # correction sur place de tout un répertoire
no fmt --fix=unwrap -d src/std/str.no     # affiche seulement le diff, n'écrit pas
no fmt --fix=unwrap src/std/str.no        # affiche le contenu corrigé
```

Forme de la correction (voir « Indexation sûre ») : **la fonction concernée a déjà un paramètre de résultat `?T`** → propagation (`x ?= <expr>`) ; sinon → renommage + déballage par match :

```nolang
; Avant correction
x = a + b

; Après correction (la fonction concernée a un paramètre de résultat ?T)
x ?= a + b

; Après correction (sinon, forme standard en 4 lignes ; arm de repli vide = valeur zéro)
x-opt = a + b
x = x-opt: {
    ok -> it

    ->
}
```

Comme les autres catégories de correction : remplacement textuel chirurgical, sans changer la mise en page, sans toucher aux annotations `#{overflow=…}` / `#{index-out=…}` situées au-dessus de l'instruction (elles restent responsables de « comment obtenir la valeur en cas d'échec »), et idempotent. Après correction, relancez `no vet` pour confirmer que `optunuse` est revenu à zéro.

### Liaison d'annotations et fusion sur une seule ligne

Les annotations `#{...}` sont **liées au nœud** : une même instruction peut donc porter plusieurs clés à la fois ; plusieurs annotations sont toujours **fusionnées en une seule ligne** :

```nolang
#{fmt-skip}
i <- [0..16): {

    #{index-out=0, overflow=wrap}
    buf[base + i] = data[i]
}
```

L'instruction ci-dessus a besoin à la fois de « prendre une valeur par défaut hors limites » (`index-out`) et de « retourner en cas de débordement » (`overflow`). Les écrire sur deux lignes donnerait le même résultat d'analyse, mais `no fmt --fix=overflow` produit la forme fusionnée sur une seule ligne.

> **Portée des annotations de ligne** : `#{overflow = ...}` ne s'applique qu'à **l'instruction suivante immédiate**. Si une annotation sans rapport est intercalée, elle s'applique tout de même correctement à cette instruction. Une écriture d'indexation sûre (`arr[base + i] = v`, `.buf[.pos + i] = v`) est désugarée en forme de match, et `#{overflow = wrap}` est reporté sur le nœud issu du desugar ; le calcul d'index est donc lui aussi soumis à l'annotation et ne sera pas signalé à tort comme non annoté.

### Propagation d'erreur (opérateur `?=`)

`?=` est une **écriture abrégée** qui déballe une option et propage l'erreur vers le haut. **Une fonction qui renvoie une option n'est pas une condition nécessaire** : l'une des deux situations suivantes suffit ; une fonction dont le retour n'est pas une option peut donc tout de même écrire `y ?= x + 1` en enveloppant son corps dans un match de bloc.

**Syntaxe :**

```no
v ?= expr
```

**Sémantique :**

- Si `expr` renvoie `ok(value)`, alors `v` reçoit la valeur interne déballée (déballage automatique) ;
- Si `expr` renvoie `nil` ou `err` :
  - fonction avec paramètre de résultat `?T` → l'option est écrite dans le paramètre de résultat et l'on fait un `return` anticipé (propagation automatique) ;
  - instruction dans le corps d'un **match de bloc au niveau instruction** `{ ... }: { ... }` → capturée par le match de bloc **le plus proche**, **tout le corps s'arrête** et les arms ne tournent qu'une fois (**pas** de `return`) ; la fonction n'a pas besoin de paramètre de résultat `?T`.

**Conditions d'utilisation (l'une des deux suffit) :**

1. La fonction concernée possède un paramètre de résultat de type option ;
2. L'instruction se trouve dans le corps d'un match de bloc au niveau instruction `{ ... }: { ... }` (voir « Trois formes du sujet de match »).

Lorsque aucune des deux n'est remplie, le compilateur signale une erreur. Les deux arms `err -> _` / `-> _` peuvent avaler sur place l'erreur propagée ; c'est le repli standard pour « je veux utiliser l'abréviation sans paramètre de résultat `?T` » :

```no
handle = (x i64) {
    {
        y ?= x + 1
        print(1)
    }: {
        err -> _

        -> _
    }
}
```

**Le paramètre de résultat n'a pas besoin d'être pré-affecté** : le prologue de la fonction ne fait pas d'initialisation à zéro, mais les paramètres de sortie non explicitement affectés au retour sont automatiquement remplis à zéro (entiers → `0`, `str` / structure → valeur zéro, option → `nil`) ; inutile donc d'écrire le modèle `result = nil`, il suffit d'écrire l'affectation du chemin de succès.

**Exemple :**

```no
;Lecture ligne par ligne d'un fichier et traitement — ?= simplifie la propagation d'erreur
process-file = (path str) (result ?str) {
    f ?= open(path); en cas d'échec, result = f automatiquement ; return
    line ?= f.read-line(); EOF ou erreur propagée automatiquement
    result = line
}
```

Forme dépliée équivalente (chaîne de matchs générée automatiquement par le compilateur) :

```no
#{fmt-skip}
process-file = (path str) (result ?str) {
    result = nil
    __tmp = open(path)
    __tmp: {
        nil || err -> {
            result = __tmp
            return
        }
        -> f = it
    }
    __tmp2 = f.read-line()
    __tmp2: {
        nil || err -> {
            result = __tmp2
            return
        }
        -> line = it
    }
    result = line
}
```

**Utilisation en chaîne :** plusieurs `?=` peuvent être enchaînés pour une propagation d'erreur de type pipeline :

```no
pipeline = (input str) (result ?str) {
    a ?= step1(input); échec → propagation
    b ?= step2(a); échec → propagation
    result = b
}
```

**Calcul arithmétique nu :** le RHS de `?=` n'est pas limité aux appels de fonction — tout calcul entier renvoyant une `option` (comme `a - b`, `a + b`, `a * b` non annotés) peut être propagé directement par `?=`, sans liaison préalable à une variable intermédiaire :

```no
; En cas de débordement, result = err automatiquement ; return ; sinon v est déballé en valeur interne
sub-safe = (a i64, b i64) (result ?i64) {
    v ?= a - b; sous-flux/dépassement → err propagé ; sinon v = i64 interne
    result = v
}

add-safe = (a i64, b i64) (result ?i64) {
    v ?= a + b; idem, pour + * et / signé
    result = v
}
```

Dépliage équivalent (généré automatiquement par le compilateur) :

```no
#{fmt-skip}
sub-safe = (a i64, b i64) (result ?i64) {
    result = nil
    __unwrap = a - b            ; option<i64>
    __unwrap: {
        nil -> {
            result = __unwrap
            return
        }
        err -> {
            result = __unwrap
            return
        }
        -> v = it               ; ok(value) : déballe la valeur interne
    }
    result = v
}
```

**RHS composé (compound RHS) :** le RHS de `?=` peut être une expression composée contenant plusieurs opérandes option. Chaque opérande option est automatiquement déballé, avec injection d'une garde de propagation — dès que l'un d'eux est `nil` / `err`, le paramètre de résultat option de la fonction courante reçoit cette valeur et l'on fait `return` (propagation vers le haut) ; seuls des succès partout permettent d'affecter la valeur interne au LHS :

```no
sum3 = () (result ?i64) {
    a ?i64 = ok(10)
    b ?i64 = ok(20)
    c ?i64 = ok(30)
    total ?= a + b + c; chaque opérande option est déballé et propagé automatiquement
    result = total
}

comp = () (result ?i64) {
    a ?i64 = ok(4)
    b ?i64 = ok(6)
    v ?= dbl(a + b); a + b peut être une option, déballée et propagée automatiquement aussi
    result = v
}
```

> Seules les options en **position d'opérande** sont automatiquement déballées (comme `a + b + c`, `f(b + c)`) ; une variable option passée directement comme argument de fonction n'en fait pas partie — elle suit l'exemption existante de `?=` pour les arguments option nus (`a ?= f(v)`, où `v` est un `?i64` et `f` reçoit un `i64` simple).

**Erreur sur RHS non-option :** pour éviter de transformer massivement des `=` en `?=` par erreur, lorsque le RHS peut être déterminé à la compilation comme « clairement non-option », le compilateur signale une erreur et suggère d'utiliser `=` :

- littéral : `v ?= 42`
- calcul entier réductible en constante : `v ?= 10 + 20`
- variable locale de type connu et non option : `v ?= x` (où `x i64 = 5`)

Le message d'erreur est de la forme : `` `?=` RHS `42` is not an option — use `=` instead ``.

> Un calcul arithmétique est considéré « potentiellement option » dès que l'un de ses opérandes est une option (ou de type inconnu / appel de fonction, pouvant renvoyer une option) ; il est alors autorisé, sans signalement indu — par exemple `v ?= a + b + c` (a/b/c sont `?i64`) ou `v ?= b + c` (b/c de type inconnu) compilent normalement.

**Scénarios d'application :**

- opérations sur conteneur pouvant être vides comme `pop` / `peek` → `?t` (`nil` = vide)
- opérations I/O comme `read-line` / `read-byte` → `?str` / `?i64` (`nil` = EOF, `err` = erreur)
- opérations de recherche comme `lookup` / `get` → `?t` (`nil` = non trouvé)
- opérations d'analyse comme `parse` / `from-str` → `?t` (`nil` = vide, `err` = entrée invalide)
- connexions réseau comme `accept` / `dial` → `?conn` (`nil` = pas de connexion, `err` = erreur)

**nil vs err :** utilisez `nil` quand l'absence est un résultat normal/attendu (pile vide, clé absente, EOF) ; utilisez `err('msg')` quand l'absence représente un véritable état d'erreur (échec I/O, entrée invalide, connexion refusée).

**Exception :** lorsqu'une fonction doit renvoyer plusieurs valeurs indépendantes (comme `(name str, value str, ok bool)`), le motif de retour multiple peut être conservé.

**Pas plus d'un `?T` dans les retours multiples (erreur bloquante) :** parmi les paramètres de résultat déclarés d'une fonction, il y a au plus un `?T`. Le `?T` est un **unique** canal d'erreur — lors d'un retour anticipé par `?=` dans le corps, l'échec ne peut être placé que dans ce seul paramètre de résultat option ; si deux `?T` sont déclarés, le compilateur ne peut en remplir que le premier, et l'appelant obtient un tuple à moitié rempli (`(err, nil)`), sans pouvoir déterminer quelle valeur représente l'échec :

```no
#{fmt-skip}
pair = () (a ?i64, b ?i64) {; erreur : deux paramètres de résultat ?T
    a = ok(1)
    v ?= boom(); boom() renvoie err('boom')
    a = v
    b = ok(2); jamais exécuté — l'appelant voit (err, nil)
}
```
```

L'écriture correcte consiste à déclarer les valeurs indépendantes en types simples, en ne gardant qu'un seul `?T` comme canal d'erreur :

```no
lookup = (k str) (value str, found bool, err ?str) {
    value = ''
    found = false
    err = nil
}
```

Le message d'erreur est de la forme : `` function 'pair' declares 2 option (?T) results ('a', 'b'); only one result may be ?T … [moptres1] ``. Le récepteur implicite `self` d'une définition de méthode est un paramètre de sortie dont l'appelant a l'alias ; il n'occupe pas ce quota ; une déclaration `extern` `#{c}` est soumise à la même limite.

### Capture d'erreur par affectation (`=` capture sur place)

La sémantique de `?=` est « propager l'erreur vers le haut » — elle exige un paramètre de résultat option dans la fonction concernée et un `return` anticipé en cas d'échec. Lorsque vous ne voulez pas de retour anticipé, mais traiter l'erreur sur place, utilisez le `=` ordinaire : dès que le RHS contient une **source d'erreur potentielle**, la variable cible est automatiquement inférée en `?T`, et `nil` / `err` sont **stockés sur place dans cette variable**, le flot continuant.

```no
handle = (b i64, c i64, d i64) (r i64) {
    a = b + c / d; a inféré en ?i64 ; division par zéro → a = err
    a: {
        err -> {
            r = -1; traitement de l'erreur sur place
        }

        nil -> {
            r = -1
        }

        -> {
            r = it; succès : r = b + c/d
        }
    }
}
```

**Sources d'erreur potentielles (déclencheurs de capture) :**

| Source | Exemple | Résultat de la capture |
| --------------------- | ----------------------------------- | ----------------------------- |
| Division / modulo entier | `a = b + c / d`, `a = b % c` | division par zéro, `MIN / -1` → `err` |
| Indexation sûre | `a = v[i]` (`arr` / `vec` / `slice`) | dépassement → `err` (`code.index-out`) |
| Opérande de type option | `a = x + 1` (`x ?i64`) | si `x` est `nil` / `err`, stocké tel quel dans `a` |
| Appel renvoyant une option (comme opérande arithmétique) | `a = f(x) + 1` (`f` renvoie `?i64`) | idem |

> Une option passée **directement** comme argument de fonction n'en fait pas partie — c'est au receveur de la traiter (cohérent avec l'exemption d'argument option nu de `?=`).

**L'arithmétique pure n'est pas une source d'erreur potentielle** : `+ - * <<` et la négation ne déclenchent pas de capture (sinon toute arithmétique deviendrait une option). Mais dès qu'on est sur le chemin option (par exemple un `/` ailleurs dans le RHS, ou une cible déclarée explicitement `?T`), ces calculs comportent une **vérification de débordement à l'exécution**, débordement → `err` :

```no
ovf = (x i64) (r i64) {
    a ?i64 = x + 1; ?T explicite → chemin d'emballage option
    a: {
        err -> {
            r = 0; x = i64-max déborde → err
        }

        nil -> {
            r = 0
        }

        -> {
            r = it
        }
    }
}
```

**Capture vs propagation :**

| Écriture | En cas d'échec | Paramètre de résultat option requis | Comment obtenir la valeur ensuite |
| ------------- | ----------------------------------- | -------------- | ------------------ |
| `a ?= expr` | met le paramètre de résultat à `nil`/`err` et `return` (retour anticipé) | oui | sans objet (déjà revenu) |
| `a = expr` | `a` stocke sur place `nil`/`err`, le flot continue | non | `a: { ok -> ... }` |
| `a ?T = expr` | idem à `=` (annotation explicite, avec sous-expressions faillibles internes) | non | idem |
| `_ = expr` | évalue mais jette le résultat et l'erreur (pas d'erreur, ne déclenche pas le lint unused) | non | sans objet |

**Portée :** la capture de l'indexation sûre, comme `?=`, **ne s'applique que dans les fonctions qui renvoient un résultat `?T`** (voir « Indexation sûre », troisième forme). La capture de `/` `%` et des opérandes option s'applique, elle, à toutes les fonctions.

**Une variable existante ne change pas de type** : la capture ne s'applique qu'à une cible « déclarée pour la première fois dans cette instruction ». Si la cible existe déjà (`v = arr[i]` après `v i64 = 0`, ou `d = x - 1` après `d = 0`), le compilateur ne change pas son type sur place mais signale une erreur demandant un choix explicite (ajouter une annotation `#{overflow=...}` / `#{index-out=...}`, utiliser `?=`, ou déclarer `?T`).

### Génériques

```no
arr_to_vec = (arr [n]t) (out []t) {
    i <- [0..n): {
        out[i] = arr[i]
    }
}
```

### Conversion de type forcée

```no
; Renvoie le nom du type sous forme de chaîne
a = typeof(x)

; `as` n'est autorisé que pour les conversions de pointeurs FFI (comme *byte, **byte, *i64)
; les entiers sont tous i64 en interne, aucune conversion explicite nécessaire
y = x as *byte
```

### Vérification de type des affectations entières

Le compilateur effectue une vérification de type sur les affectations entières, afin d'empêcher un rétrécissement (narrowing) non sûr qui entraînerait une perte de données. Voici les règles :

#### Élargissement implicite (sûr, automatiquement autorisé)

Une valeur de type entier étroit peut être affectée automatiquement à un type plus large, car la plage du type cible contient entièrement celle du type source :

```no
b byte = 200
i i64 = b; ✓ la plage de byte [0,255] ⊆ la plage de i64
u u32 = b; ✓ la plage de byte ⊆ la plage de u32
```

#### Affectation d'un littéral entier

Un littéral entier (inféré par défaut comme `i64`) peut être affecté à tout type entier dont la plage contient cette valeur :

```no
#{fmt-skip}
n u8 = 200; ✓ 200 ∈ [0,255]
m u8 = 300; ✗ 300 > 255, erreur de compilation
big u64 = 18446744073709551615; ✓ 2^64-1, valeur maximale de u64
```
```

#### Rétrécissement non sûr (erreur de compilation)

Affecter directement une variable de type large à un type étroit provoque une erreur de compilation, car une perte de données est possible. Le message d'erreur s'accompagne d'un **conseil de correction concret**, suggérant comment rétrécir en toute sécurité par des opérations bit à bit :

```no
#{fmt-skip}
d u64 = 42
h u32 = d; ✗ cannot assign u64 value to u32 variable 'h'; hint: narrow safely with a bitwise mask (e.g. `& 4294967295`) or right shift (e.g. `>> 32`)
h u16 = d; ✗ cannot assign u64 value to u16 variable 'h'; hint: narrow safely with a bitwise mask (e.g. `& 65535`) or right shift (e.g. `>> 48`)
h u8 = d; ✗ cannot assign u64 value to u8 variable 'h'; hint: narrow safely with a bitwise mask (e.g. `& 255`) or right shift (e.g. `>> 56`)
x u32 = d + 1; ✗ le résultat de l'addition reste u64, non sûr
y u32 = foo(); ✗ le type du résultat de l'appel de fonction ne correspond pas
```
```

> **Conseil de correction** : le compilateur calcule automatiquement la valeur de masque et le décalage exacts selon le type cible. En appliquant le masque ou le décalage suggéré, on obtient un rétrécissement sûr (voir la section suivante).
>
> **Type cible signé** : pour les entiers signés comme `i8`/`i16`/`i32`/`i64`, le conseil indique que le rétrécissement bit à bit ne s'applique pas (la sémantique de troncature du bit de signe est ambiguë) et suggère plutôt un contrôle de plage explicite.

#### Rétrécissement sûr par opération bit à bit (automatiquement autorisé)

Lorsque le RHS de l'affectation est une **expression bit à bit** (`&`, `|`, `^`, `<<`, `>>`) et que le type cible est un **entier non signé** (`u8`/`u16`/`u32`/`u64`/`byte`), le compilateur autorise le rétrécissement implicite — car tronquer les bits d'ordre élevé est la sémantique standard des opérations bit à bit et n'entraîne pas de perte de données inattendue :

```no
d u64 = 42

; ✓ Opération de masque : le résultat est nécessairement ≤ au masque, tombe sans risque dans u32
h u32 = d & 67108863; mask = 2^26-1 < 2^32
h u32 = d & 4294967295; mask = 2^32-1, exactement la plage de u32

; ✓ Opération de décalage : après décalage à droite, les bits hauts sont à 0, sûr
hi u32 = d >> 32; u64 >> 32 laisse 32 bits

; ✓ Combinaisons XOR / OR
c u32 = a ^ b; résultat d'une opération bit à bit
b byte = v & 255; masque sur la plage de byte

; ✓ Opérations bit à bit composées (courant en cryptographie/codage)
s u32 = (key[0] & 255) | ((key[1] & 255) << 8) | ((key[2] & 255) << 16) | ((key[3] & 255) << 24)
```

> **Pourquoi est-ce autorisé ?** La sémantique des opérations bit à bit (masque, décalage, XOR, OR) est justement de construire un motif de bits. Lors de l'affectation à un type non signé étroit, tronquer les bits hauts est une opération délibérée — le développeur a garanti la plage du résultat par un masque ou un décalage, ou a délibérément jeté les bits hauts. C'est un motif courant en cryptographie (comme ChaCha20, Poly1305, Blake2) et dans le code de codage/décodage.

> **Uniquement pour les types cibles non signés.** Pour une cible entière signée (`i8`/`i16`/`i32`/`i64`), même avec un RHS bit à bit, une erreur est signalée, car la troncature du bit de signe est ambiguë :
>
> ```no
> d u64 = 42
> h i32 = d & 4294967295   ; ✗ erreur quand même : le rétrécissement sûr ne s'applique pas à une cible signée
> ```

> **Le niveau supérieur doit être une opération bit à bit.** Seules les expressions dont l'opérateur de plus haut niveau est `&`/`|`/`^`/`<<`/`>>` sont autorisées. L'addition, la soustraction, les appels de fonction, les références directes à une variable n'en font pas partie :
>
> ```no
> d u64 = 42
> h u32 = d              ; ✗ le niveau supérieur est un Identifier, pas une opération bit à bit
> h u32 = d + 1          ; ✗ le niveau supérieur est +, pas une opération bit à bit
> ```

### Système de modules

- Chaque fichier est un module.
- Les noms de fichiers et de dossiers utilisent des tirets.

```shell
utils/
└── helper.no    ; le nom du module est utils/helper
```

### Importer un module

> **La nouvelle syntaxe utilise `#` pour importer. L'ancien mot-clé `use` reste utilisable, mais est déprécié ; privilégiez `#`.**

```no
; Bibliothèque standard (nouvelle syntaxe, recommandée)
# std/math.add
; Module distant (ne commence pas par std/)
# github.com/utils/math.add
; Module local, doit commencer par /
# /utils/math.add
; Alias
# std/math.add a

; ── Ci-dessous : ancienne syntaxe (dépréciée, encore utilisable mais déconseillée) ──
; use std/math.add
; use github.com/utils/math.add
; use /utils/math.add
; use std/math.add a
```

#### Importer directement un fichier de configuration `.noml`

Un fichier `.noml` peut être importé directement avec `#` : l'ordre d'analyse cherche d'abord un `.no` de même nom, puis, s'il n'existe pas, un `.noml`. Après import,
**le nom de la structure est par défaut celui du fichier** (sans extension, modifiable par l'annotation `#{name = '...'}` en tête de fichier),
et chaque membre devient un champ de cette structure ; une valeur globale de même nom portant le contenu du fichier est également générée, aussi lit-on la configuration avec
`server.port`.

```noml
; cfg/server.noml
{
  host: 'localhost'
  port: 8080
  tags: [] #{type = '[]str'}
  compiler: {
    version: 'v0.1.0'
  }
}
```

```no
# /cfg/server

print(server.port); 8080
print(server.compiler.version); v0.1.0
```

- Le type d'un champ est pris en priorité dans l'annotation `#{type = '...'}` du membre (la valeur est une expression de type nolang complète, comme
  `'[]str'`, `'[]i64'`, `'?i64'`) ; sans annotation, il est inféré depuis la valeur : chaîne → `str`, entier → `i64`,
  décimal → `f64`, booléen → `bool`, objet → structure imbriquée générée automatiquement (`server-compiler`),
  tableau → `[]type_élément`.
- Un conteneur vide (`[]`, `{}`) **doit** porter `#{type = '...'}` : la valeur noml n'a pas de type en soi, un conteneur vide
  ne peut être inféré. Lorsqu'un membre d'objet est annoté `#{type = 'x'}`, on utilise directement `x` (à définir soi-même), sans générer
  de structure imbriquée.
- Le nom de la structure d'un objet imbriqué est préfixé du nom de fichier (`server-compiler`) ; l'ordre de définition place d'abord les imbriquées, puis l'hôte.
- **Une annotation en tête (niveau document) peut modifier le nom de la structure** : `#{name = '...'}` (forme équivalente `#{type = '...'}`,
  `name` prioritaire) s'écrit sur une ligne distincte au-dessus du `{` de premier niveau (ou en fin de ligne de la valeur de premier niveau). Après modification, le nom de liaison de la valeur et le préfixe des structures imbriquées
  suivent tous deux, mais **le chemin d'import reste résolu d'après le nom de fichier** :

```noml
; cfg/db.noml
#{name = 'db-options'}
{
  host: 'localhost'
}
```

```no
# /cfg/db
# /cfg/db as db2

print(db-options.host); localhost
print(db2.host); localhost
```

- Lorsque deux fichiers différents génèrent le même nom de type (par exemple des fichiers de même nom dans des répertoires différents), une erreur est signalée ; utilisez l'annotation
  `#{name = '...'}` en tête pour les distinguer ; un même fichier importé plusieurs fois (par exemple avec un alias `as`) ne fusionne qu'une seule définition de type.
- Les noms de clés sont convertis selon les règles nolang en tirets minuscules (`anonymous-fn-type`) : un nom commençant par un chiffre reçoit le préfixe `f-`, et un nom identique à un mot-clé reçoit le préfixe `x-`.
- L'alias `as` peut modifier le nom de liaison de la valeur, sans changer le nom de la structure : `# /cfg/server as cfg`.
- Les types `.noml` ne participent pas au préfixe de module : même si le nom de fichier est identique à celui d'un module de la bibliothèque standard (comme `server`), le type reste un nom nu.
- `package.noml` / `workspace.noml` peuvent eux aussi porter des annotations `#{...}` (comme `#{type = '[]str'}`) :
  elles sont ignorées à l'analyse, et servent uniquement aux outils externes pour reconstruire une structure fortement typée ; la syntaxe est strictement identique aux `#{...}` de nolang.

### Exporter un module

Ne s'applique qu'aux fichiers lib.no

```no
@ std/math.add a
```

### FFI (annotation `#{c}`)

On déclare une fonction C externe via l'annotation `#{c}`, pour réaliser une FFI (Foreign Function Interface).

**Syntaxe** : `#{c}` sur une ligne distincte marque la ligne suivante comme déclaration FFI. `#{c}` est la clé de langage FFI du système d'annotations ; `#{cpp}`, `#{rust}` et d'autres langages sont également pris en charge. L'ancienne syntaxe `#c` reste rétrocompatible.

**Déclaration privée** : un nom commençant par `_` est privé (non exporté) ; le symbole ABI C voit automatiquement son préfixe `_` retiré et ses tirets convertis en underscores.

**Plus besoin de fichiers séparés** : déclarations FFI et code ordinaire peuvent coexister dans un même fichier `.no`.

**Syntaxe des types pointeur** : en FFI, `*T`, `**T`, `***T` de style C désignent des pointeurs, avec un type concret `T` obligatoire. Le code ordinaire ne peut pas utiliser cette syntaxe.

| Syntaxe | Signification | LLVM IR | Usage |
| --------- | ----------- | ------- | ------------------- |
| `*byte` | pointeur vers byte | `i8*` | pointeur opaque (comme un handle db) |
| `**byte` | double pointeur | `i8**` | paramètre de sortie (comme `sqlite3**`) |
| `***byte` | triple pointeur | `i8***` | triple indirections, très rares |

```no
; sqlite.no — liaison FFI et wrapper sûr dans le même fichier
; le compilateur convertit automatiquement les tirets (-) en underscores (_) pour correspondre aux symboles ABI C
; les noms commençant par _ sont privés, le préfixe _ est automatiquement retiré du symbole ABI C

; Paramètres de types de base
#{c}
c-strlen = (s str) (n i64)

; Paramètre pointeur (*byte = pointeur opaque), déclaration privée
#{c}
_sqlite3-close = (db *byte) (rc i32)

; Double pointeur (**byte = paramètre de sortie, la valeur est automatiquement écrite dans la variable après l'appel), déclaration privée
#{c}
_sqlite3-open = (filename str, db **byte) (rc i32)

; Plusieurs paramètres pointeur, déclaration privée
#{c}
_sqlite3-exec = (db *byte, sql str, callback *byte, arg *byte, errmsg *byte) (rc i32)
```

```no
; Wrapper sûr dans le même fichier

open = (dsn str) (d db-sqlite) {
    handle i64 = 0
    rc i32 = _sqlite3-open(dsn, handle)
    rc != SQLITE-OK -> {
        return
    }
    d.handle = handle
}
```

**Règles :**

1. `#{c}` sur une ligne distincte marque la ligne suivante comme déclaration FFI (l'ancienne syntaxe `#c` reste rétrocompatible).
2. La FFI n'est qu'une déclaration, sans corps de fonction.
3. Un pointeur doit avoir un type concret (comme `*byte`) ; le `ptr` nu n'est pas autorisé.
4. `**byte` sert aux paramètres de sortie : la valeur de pointeur écrite par la fonction C est automatiquement convertie en `i64` et réécrite dans la variable de l'appelant.
5. Tous les pointeurs sont stockés côté Nolang sous forme de `i64` (`ptrtoint`).
6. Un paramètre de type `str` est automatiquement converti en `i8*` terminé par un nul.
7. Un nom commençant par `_` est privé (non exporté) ; le préfixe `_` est retiré du symbole ABI C.
8. Déclarations FFI et code ordinaire peuvent coexister dans un même fichier `.no`.

> La direction inverse (faire appeler Nolang par C / C++ / Rust / Go / Java / Python / C#) utilise `#{cabi='symbol'}` et `no build --shared` ; voir [Appeler Nolang depuis un langage hôte](./host-interop).

### Système d'annotations (`#{...}`)

`#{...}` est le système d'annotations général, sous forme d'une liste de paires clé-valeur séparées par des virgules. Les types de valeurs suivants sont pris en charge :

| Syntaxe | Type | Exemple |
| --- | --- | ------------------------------------ |
| Clé seule | Booléen | `#{debug}` |
| Valeur booléenne | Booléen | `#{inline=true}` / `#{inline=false}` |
| Nombre | Entier | `#{max=100}` |
| Texte | Chaîne | `#{name='hello'}` |
| Identifiant | Identifiant | `#{mode=fast}` |
| Tableau | Tableau | `#{derive=[Serialize, Deserialize]}` |
| Plage | Plage | `#{range=[0..256)}` |

Plusieurs paires clé-valeur sont séparées par des virgules :

```no
#{derive=[Serialize, Deserialize], range=[0..256), max=100, debug}
```

#### Position des annotations

`#{...}` n'autorise que deux écritures :

- **Au-dessus** : sur une ligne distincte, placée au-dessus de la cible (instruction, champ de structure, membre d'énumération, déclaration, arm de match) ;
- **Derrière** : en fin de ligne de la cible (`res = arr[i] #{index-out=zero}`, `a pt #{inline}`,  
  `green #{deprecated}`).

Écrire l'annotation **devant la cible sur la même ligne** (préfixe, comme `#{index-out=zero} res = arr[i]`) est une **erreur de compilation**, signalée de la même façon par le compilateur et le
LSP. Critère : après le `}` du groupe d'annotations, aucune instruction ne doit figurer sur la même ligne (un saut de ligne,
un commentaire de ligne `;`, ou un autre groupe d'annotations adjacent ne comptent pas comme une instruction).

```no
; ✓ Au-dessus : sur une ligne distincte
#{overflow=wrap}
x i8 = a + 100

; ✓ Derrière : en fin de ligne
x i8 = a + 100 #{overflow=wrap}
```

```no
#{fmt-skip}
; ✗ Devant : préfixe sur la même ligne → erreur
#{overflow = wrap} x i8 = a + 100
```

Les annotations sur les champs de structure et les membres d'énumération (valeurs des enums de style C, variantes des énumérations taguées) n'autorisent elles aussi que le-dessus ou l'après  
(`a pt #{inline}`, `a pt #{inline=true}`, `green #{deprecated}`, `ok(v i64) #{inline}`).  
Notez que la **valeur** de `#{inline}` est elle aussi prise en charge : `#{inline=false}` est valide (voir ci-dessous  
〈`#{inline}` sur les champs de structure〉).

Dans l'écriture `if <cond> { ... }`, une annotation placée entre la condition et `{` (`if x > 0 #{overflow=wrap} {`) est elle aussi
une position « devant » et provoque la même erreur. L'analyse y était permissive et avalait autrefois tout le groupe d'annotations — sans effet et sans erreur,
c'était la forme de défaillance la plus difficile à détecter.

La syntaxe de plage prend en charge 16 combinaisons de parenthèses (règle : `[` inclut la borne gauche, `(` ne l'inclut pas ; `]` inclut la borne droite, `)` ne l'inclut pas) :

**Intervalles bornés (4 variantes)** — les deux extrémités ont une valeur :

- `[a..b]` — intervalle fermé aux deux bouts (`x >= a && x <= b`)
- `[a..b)` — fermé à gauche, ouvert à droite (`x >= a && x < b`)
- `(a..b]` — ouvert à gauche, fermé à droite (`x > a && x <= b`)
- `(a..b)` — intervalle ouvert aux deux bouts (`x > a && x < b`)

**Intervalles sans borne supérieure (4 variantes)** — borne droite omise :

- `[a..]`, `[a..)` — gauche incluse, sans borne supérieure (`x >= a`)
- `(a..]`, `(a..)` — gauche exclue, sans borne supérieure (`x > a`)

**Intervalles sans borne inférieure (4 variantes)** — borne gauche omise :

- `[..b]`, `(..b]` — sans borne inférieure, droite incluse (`x <= b`)
- `[..b)`, `(..b)` — sans borne inférieure, droite exclue (`x < b`)

**Intervalles totalement non bornés (4 variantes)** — les deux extrémités omises, basés sur les valeurs min/max du type :

- `[..]` — min du type ≤ x ≤ max du type (les deux extrémités incluses)
- `[..)` — min du type ≤ x < max du type
- `(..]` — min du type < x ≤ max du type
- `(..)` — min du type < x < max du type

#### Portée des annotations

La **position** d'une annotation détermine son hôte, et la **nature** de l'hôte détermine l'étendue de son effet. Il y a quatre niveaux, appliqués de l'extérieur vers l'intérieur,
avec surcharge par le plus proche ; le corps de fonction est une frontière d'héritage stricte :

| Niveau | Hôte | Étendue d'effet |
| --- | --- | --- |
| **L0 niveau module** | En-tête de fichier | Toutes les déclarations et instructions du fichier (annotation d'en-tête + une ligne vide) |
| **L1 niveau déclaration** | Constante / variable globale, structure, interface, énumération taguée, fonction, méthode | Toute la définition de la déclaration (fonction : corps et tous les blocs imbriqués) |
| **L2 niveau bloc** | Groupe de coroutines, groupe if, `cond -> body`, match, boucle `for`/comptage/plage | Toutes les instructions du corps du bloc (y compris les blocs imbriqués, jusqu'à la frontière L1 interne) |
| **L3 niveau ligne unique** | Une instruction unique (`let`, affectation, `?=`, `return`, appel, arm `cond ->`) | L'instruction elle-même et ses expressions |

```no
; L1 : toute la fonction
#{overflow=wrap}
sum = (xs []i64) (total i64) {
    ; L3 : ne concerne que l'instruction suivante
    #{overflow=wrap}
    total = xs.len() - 1
    ; L2 : tout le corps de la boucle
    #{overflow=wrap}
    i <- [0..xs.len()): {
        total = total + xs[i]
    }
}
```

Les règles complètes de détermination, la matrice clé × niveau et l'écart avec l'implémentation actuelle figurent dans
[〈Portée des annotations et des commentaires〉](annotation-scope.md).

L'annotation FFI `#{c}` est une forme spéciale du système d'annotations : lorsqu'une annotation contient une clé de langage FFI (`c`, `cpp`, `rust`, etc.) et qu'une déclaration de fonction suit, le compilateur la reconnaît comme une liaison FFI :

```no
; #{c} avec annotation supplémentaire
#{c, debug}
_sqlite3-open = (filename str, db **byte) (rc i32)
```

#### Attacher une annotation à une déclaration

Les annotations non-FFI sont automatiquement attachées à la déclaration qui suit immédiatement (déclaration de variable, définition de structure) ; elles peuvent servir à marquer des métadonnées comme des restrictions de plage pour les types numériques (`num`, `i64`, etc.) :

```no
; Déclaration de variable avec annotation range
#{range=[0..256)}
x num = 42

; Définition de structure avec annotation
#{derive=[Serialize, Deserialize]}
point {
    x i64
    y i64
}

; Champ de structure avec annotation range (utilisable pour les types numériques comme num)
person {
    age num #{range=[0..150]}
    score i64 #{range=[0..256)}
    name str
}
```

L'annotation `range` convient particulièrement au type `num` (`num = int | float`) : elle marque la plage valide d'une valeur numérique. La plage peut être un entier ou un identifiant :

```no
; Utilisation d'un identifiant constant comme borne de plage
#{range=['i8.MIN'..'i8.MAX']}
val i8 = 100
```

#### Incorporation de fichiers (`#{embed=...}`)

L'annotation `#{embed=...}` incorpore le contenu d'un fichier externe à la compilation sous forme de constante `[]byte` en lecture seule, à l'image de `//go:embed` de Go. Les données incorporées sont compilées directement dans l'exécutable, permettant une distribution en un seul binaire sans lecture de fichier à l'exécution.

**Syntaxe** :

```no
#{embed='path/to/file'}
ICON []byte

; Ou avec un chemin nu (sans guillemets)
#{embed='assets/icon.ico'}
ICON []byte
```

L'annotation `embed` est attachée à la déclaration de variable `[]byte` qui suit immédiatement. La déclaration de variable **ne peut pas** porter de valeur initiale explicite — la valeur est fournie par le contenu du fichier incorporé.

**Résolution des chemins** :

- Les chemins relatifs sont résolus depuis la racine du paquet (le répertoire contenant `package.noml`).
- Les chemins absolus sont pris en charge.
- Les caractères `/`, `.` et `-` sont autorisés dans les chemins.

**Exemple** :

```no
; Incorporer une icône Windows
#{embed='assets/win-icon.ico'}
WIN-ICON []byte

; Incorporer un certificat SSL
#{embed='certs/server.pem'}
SERVER-CERT []byte

; Incorporer un fichier de configuration
#{embed='config/default.json'}
CONFIG []byte

; Utiliser les données incorporées
print('icon size: ', len(WIN-ICON))
print('first byte: ', WIN-ICON[0])
```

**Points d'attention** :

1. Les données incorporées sont des constantes en **lecture seule**, pointant vers une zone mémoire en lecture seule. Écrire dans ces données est un comportement non défini (cohérent avec la sémantique de `//go:embed` de Go).
2. Les variables incorporées ne participent pas à la libération du tas — le pointeur des données incorporées n'est pas libéré à la sortie du programme.
3. Seules les déclarations de type `[]byte` sont prises en charge (les tableaux fixes `[N]byte` fonctionnent aussi).
4. Le fichier doit exister à la compilation, sinon erreur.
5. Ne peut pas coexister avec une valeur initiale explicite (comme `ICON []byte = [1, 2, 3]`, qui provoque une erreur).

#### Incorporation d'un dossier (`#{embed='dir'}`)

`#{embed=...}` prend également en charge l'incorporation d'un répertoire entier. Lorsque le chemin pointe vers un répertoire, le compilateur lit récursivement tous les fichiers qu'il contient et les incorpore comme un système de fichiers en lecture seule de type `fs.embed`. À l'exécution, les fichiers incorporés sont accessed par chemin via les méthodes `read()` et `exists()`, sans aucune fonction C — toute la logique de recherche est implémentée en Nolang.

**Syntaxe** :

```no
; Incorporer tout un répertoire frontend
#{embed='../frontend/dist'}
DIST fs.embed

; Lire un fichier depuis le système de fichiers incorporé
data = DIST.read('index.html')

; Vérifier si un fichier existe
ok = DIST.exists('css/style.css')
```

**Le type `fs.embed`** :

`fs.embed` est une structure définie dans le module `fs` de la bibliothèque standard Nolang ; elle contient les métadonnées des fichiers incorporés (chemin, décalage, longueur). Le compilateur lit le répertoire à la compilation et concatène le contenu de tous les fichiers en une variable globale constante ; à l'exécution, la méthode `read`, implémentée en Nolang pur, parcourt le contenu pour effectuer la recherche.

| Méthode | Signature | Description |
| -------- | ----------------------------------- | ----------------- |
| `read` | `(path str) (data ?[]byte)` | Lit un fichier incorporé et renvoie son contenu (`nil` s'il n'est pas trouvé) |
| `exists` | `(path str) (yes bool)` | Vérifie si un fichier incorporé existe |

**Exemple** :

```no
; Serveur de fichiers statiques HTTP : incorporation des fichiers frontend dans un seul binaire
#{embed='../frontend/dist'}
DIST fs.embed

serve = (conn server-conn) {
    conn.parse()
    path = conn.path
    path == '/' -> path = '/index.html'

    ; Retirer le / initial pour obtenir le chemin relatif
    rel = path.slice(1, path.len-bytes())

    ; Lire depuis le système de fichiers incorporé
    data []byte = DIST.read(rel)
    {
        ok -> it

        -> {
            conn.write-html(404, 'Not Found')
            conn.close()
            return
        }
    }
    conn.write-status(200)
    conn.write-header('Content-Type', get-content-type(path))
    conn.write-body(data)
    conn.close()
}
```

**Points d'attention** :

1. Tous les fichiers du répertoire (y compris les sous-répertoires) sont incorporés récursivement ; les chemins sont stockés relativement au répertoire incorporé (comme `css/style.css`).
2. Les séparateurs de chemin sont uniformément convertis en slash `/` (cohérent entre plateformes).
3. Les variables de type `fs.embed` sont en lecture seule et ne participent pas à la libération du tas.
4. Convient aux scénarios de distribution en binaire unique (serveur de fichiers statiques HTTP, outils CLI incorporant des ressources, etc.).

#### Annotations de plateforme

Les annotations de plateforme sont des filtres à la compilation qui décident d'inclure ou non du code selon la plateforme cible. Elles utilisent des **clés plates** (comme `#{mac-arm64}`) qui spécifient à la fois le système d'exploitation et l'architecture, sans ambiguïté, et s'attachent à la déclaration qui suit immédiatement. Le code qui ne correspond pas ne participe pas du tout à la compilation — aucun LLVM IR n'est généré, aucune vérification de type n'est effectuée.

**Clés de plateforme prises en charge (6 combinaisons plates) :**

| Clé | Correspondance |
| ---------------- | -------------------------- |
| `#{linux-amd64}` | Linux x86_64 |
| `#{linux-arm64}` | Linux ARM64 |
| `#{win-amd64}` | Windows x86_64 |
| `#{win-arm64}` | Windows ARM64 |
| `#{mac-amd64}` | macOS x86_64 (Intel) |
| `#{mac-arm64}` | macOS ARM64 (Apple Silicon) |

```no
#{mac-arm64}
print('running on macOS ARM64')

#{linux-amd64}
print('running on Linux x86_64')

#{win-amd64}
print('running on Windows x86_64')

; Variables spécifiques à la plateforme
#{mac-amd64, mac-arm64}
sep = '/'

#{win-amd64, win-arm64}
sep = '\\'

; Fonctions spécifiques à la plateforme
#{mac-arm64, mac-amd64}
greet = () {
    print('hello from mac')
}

#{linux-amd64, linux-arm64}
greet = () {
    print('hello from linux')
}

greet()
```

Plusieurs clés sur une même déclaration sont en relation de **OU** — une seule correspondance suffit à inclure le code. Comme chaque clé spécifie déjà le système d'exploitation et l'architecture, aucune logique ET n'est nécessaire.

| Annotation | Signification |
| --------------------------- | --------------------------------- |
| `#{mac-arm64}` | macOS ARM64 uniquement |
| `#{mac-amd64, mac-arm64}` | macOS, toute architecture |
| `#{linux-amd64, win-amd64}` | Linux x86_64 **ou** Windows x86_64 |
| `#{mac-arm64, linux-arm64}` | macOS ARM64 **ou** Linux ARM64 |

```no
; S'exécute sur macOS et Linux (toutes architectures)
#{mac-amd64, mac-arm64, linux-amd64, linux-arm64}
shared = () {
    print('unix-like')
}

; Uniquement Windows x86_64
#{win-amd64}
reg-key = () {
    print('reading registry on win/x64')
}

; Uniquement macOS ARM64 (Apple Silicon)
#{mac-arm64}
neural = () {
    print('Apple Neural Engine available')
}
```

`os.get-arch()` permet d'obtenir l'architecture courante à l'exécution, tandis que les annotations de plateforme incluent ou excluent le code à la compilation.

## Configuration du compilateur package.noml

Le bloc `compiler` de `package.noml` contrôle le comportement du compilateur. La valeur de `version` est par convention préfixée d'un `v` (comme `"v0.1.0"`) ; l'analyse interne du compilateur est insensible au préfixe : `"v0.1.0"` et `"0.1.0"` sont traités comme la même version.

```noml
{
  compiler: {
    version: 'v0.1.0'
    emit: 'js'
  }
}
```

### `emit`

Contrôle le backend de sortie :

| Valeur | Description |
| ----------- | --------------------------------------- |
| `""` (vide, défaut) | Utilise le backend LLVM pour générer un exécutable natif |
| `"js"` | Utilise le backend JS pour générer du JavaScript (types effacés), sans chaîne d'outils LLVM |

Une fois défini à `"js"`, `no build` emprunte automatiquement le backend JS, équivalent à l'option de ligne de commande `--js`. Les indicateurs de ligne de commande ont la priorité sur le fichier de configuration.

```noml
; Générer automatiquement du JavaScript
compiler: {
  emit: 'js'
}
```

### `anonymous-fn-type`

Contrôle si la syntaxe de type de fonction anonyme (comme `cb ()()`) est autorisée. Défaut `false` (désactivée) ; un alias de type de fonction nommé est requis.

### `link-libs`

Spécifie la liste des bibliothèques C à lier :

```noml
compiler: {
  link-libs: ['crypto', 'ssl']
}
```

## Injection de variables à la compilation (-ld)

`no build`, `no run` et `no test` prennent en charge l'injection de constantes globales à la compilation via l'indicateur `-ld-KEY=VALUE`. Les variables injectées équivalent à déclarer `KEY = VALUE` en tête du code source et peuvent être utilisées directement dans le programme.

### Syntaxe

```bash
no build -ld-KEY=VALUE [更多 -ld...] <file>
```

- Le préfixe `-ld` est immédiatement suivi du nom de la variable et de sa valeur, séparés par `=`.
- Plusieurs indicateurs `-ld` peuvent être utilisés en même temps.
- Forme courte booléenne : `-ld-DEBUG` (sans `=VALUE`) équivaut à `-ld-DEBUG=true`.

### Inférence du type des valeurs

| Forme de la valeur | Type inféré | Exemple |
| ---------------- | --------------- | ------------------- |
| Entier | `i64` | `-ld-COUNT=42` |
| Nombre décimal | `f64` | `-ld-PI=3.14` |
| `true` / `false` | `bool` | `-ld-DEBUG=true` |
| Autre | `str` (littéral chaîne entre guillemets simples) | `-ld-VERSION=0.1.2` |

### Exemple

```no
; En supposant une compilation avec -ld-VERSION=0.1.2 -ld-COUNT=42 -ld-DEBUG=true
print('VERSION:', VERSION); sortie : VERSION: 0.1.2
print('COUNT:', COUNT); sortie : COUNT: 42
print('DEBUG:', DEBUG); sortie : DEBUG: 1 (un bool s'affiche comme 1 sur le backend natif)
```

```bash
; Injecter plusieurs variables
no build -ld-VERSION=0.1.2 -ld-COUNT=42 -ld-DEBUG=true main.no

; Forme courte booléenne
no run -ld-RELEASE main.no

; Le backend JS la prend aussi en charge
no build --js -ld-VERSION=0.1.2 main.no
```

### Relation avec les déclarations du code source

Règles d'interaction entre les variables injectées par `-ld` et les déclarations du code source :

- **Déclaration de même nom déjà présente dans le code** : la valeur injectée **remplace sur place** la valeur de cette variable dans le code. Le motif courant consiste à déclarer une variable factice dans le code (comme `VERSION = ''`) et à lui attribuer une valeur à la compilation avec `-ld-VERSION=0.1.2`.
- **Absente du code** : la variable injectée est placée en tête de l'AST comme nouvelle déclaration de constante globale et participe à tous les passes de compilation ultérieurs (inférence de types, validation, fusion de modules, codegen).
- **Sans injection** : les déclarations du code restent inchangées.

```no
; Le code déclare des variables factices
VERSION = ''
COUNT = 0

print('VERSION:', VERSION)
print('COUNT:', COUNT)

; no build -ld-VERSION=0.1.2 main.no
; sortie : VERSION: 0.1.2  (la valeur injectée a remplacé le '' du code source)
;          COUNT: 0        (non injecté, la valeur du code source est conservée)
```
