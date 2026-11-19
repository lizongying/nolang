---
sidebar_position: 8
---

# Backend JS

Nolang prend en charge la compilation directe du code source `.no` en JavaScript, sans chaîne d'outils LLVM. Le backend JS applique une stratégie d'**effacement des types** (*type erasure*) : toutes les annotations de type Nolang sont absentes de la sortie JS ; seul le comportement à l'exécution est généré.

## Démarrage rapide

```bash
# 編譯為 JS（輸出至 dist/<name>.js）
no build --js main.no

# 瀏覽器模式：生成 JS + HTML wrapper
no build --js --browser main.no

# 運行：編譯為 JS 並以 node 執行
no run --js main.no

# 瀏覽器模式：編譯並在預設瀏覽器中打開
no run --js --browser main.no
```

## Principes de conception

Conception centrale du backend JS :

- **Effacement des types** : JS est dynamiquement typé ; les annotations de type `int`/`str`/`bool`/`vec[T]`/`[N]T`/`?T` de Nolang sont totalement absentes de la sortie JS
- **Aucun LLVM requis** : le code JavaScript est émis directement depuis l'AST, sans dépendre de la chaîne d'outils clang/LLVM
- **Double cible** : prend en charge Node.js (par défaut) et le navigateur (`--browser`) comme environnements cibles
- **Annotations de plateforme** : `#{js}` et `#{js-browser}` contrôlent la visibilité du code selon la plateforme

L'implémentation se trouve dans le répertoire `src/build/js/` :

| Fichier | Responsabilité |
| --- | --- |
| `generator.go` | Logique principale de codegen AST → JavaScript |
| `expr.go` | Génération des expressions |
| `stmt.go` | Génération des instructions |
| `builtin.go` | Mappage des fonctions intégrées (print→console.log, etc.) |
| `html_wrapper.go` | Modèle HTML du mode navigateur |

## Paramètres de la ligne de commande

| Paramètre | Description |
| --- | --- |
| `--js` | Utilise le backend JS (émet du JavaScript, contourne LLVM) |
| `--browser` | Génère une sortie orientée navigateur (HTML + JS, nécessite `--js`) |
| `--esm-cdn <url>` | Remplace le préfixe CDN de l'import map ESM du navigateur (par défaut `https://esm.sh`) |
| `--js-runtime <cmd>` | Exécutable JS utilisé par `no run --js` (par défaut `node` ; `bun`, `deno`, etc. ; arguments supplémentaires autorisés) |
| `-o <path>` | Spécifie le chemin de sortie |

### Compilation

```bash
no build --js main.no                    # 輸出 dist/main.js
no build --js -o app.js main.no          # 指定輸出路徑
no build --js --browser main.no          # 輸出 dist/main.js + dist/main.html
```

### Exécution

```bash
no run --js main.no                      # 編譯為 JS 後以 node 執行
no run --js --browser main.no            # 編譯為瀏覽器 JS + HTML 並打開瀏覽器
```

#### Choisir l'exécutable JS

`--js-runtime` désigne l'exécutable qui lance l'artefact ; la valeur par défaut est **`node`** (Node.js). Tout exécutable capable de lancer des modules ES convient :

```bash
no run --js --js-runtime bun main.no             # Bun
no run --js --js-runtime deno run main.no        # Deno（首個 token 是可執行檔，其餘是腳本前的參數）
no run --js --js-runtime "deno run --allow-read" main.no
no run --js --js-runtime "node --enable-source-maps" main.no
no run --js --js-runtime /opt/js/bin/node main.no   # 指定絕對路徑
```

- Le premier token séparé par des espaces est l'exécutable ; les tokens suivants sont placés avant le chemin du script. Les arguments du programme s'écrivent toujours après le nom du fichier `.no`, comme auparavant.
- Par défaut (sans `--js-runtime`), c'est `node` qui est utilisé ; si le système ne dispose que du binaire `nodejs` de style Debian/Ubuntu, il sert de repli automatique. Un exécutable explicitement choisi (`bun`, un chemin absolu, `node --flags`) ne fait jamais l'objet d'un repli : s'il est introuvable, une erreur est signalée.
- Un exécutable absent de `PATH` provoque une erreur immédiate, avec une invite à utiliser `--js-runtime` pour en choisir un autre (pas de repli silencieux vers node).
- Les imports ESM (`# npm/…`) fonctionnent avec tous ces exécutables : `no run --js` lie le `node_modules` du projet dans le répertoire temporaire ; node, bun et deno résolvent les paquets selon les mêmes règles.

## Annotations de plateforme

Le backend JS introduit deux clés d'annotation de plateforme supplémentaires :

| Clé | Scénario correspondant |
| --- | --- |
| `#{js}` | Backend JS (Node.js et navigateur) |
| `#{js-browser}` | Mode navigateur (avec `--browser`) |

```no
; Conservé uniquement lors d'une compilation par le backend JS
#{js}
js-helper = () {
    print('JS only code')
}

; Conservé uniquement en mode navigateur
#{js-browser}
print('running in browser mode')

; Conservé uniquement sur les backends natifs (exclu lors de la compilation JS)
#{mac-arm64}
print('running on macOS ARM64')
```

## Mappage des fonctions intégrées

Le backend JS mappe les intégrés Nolang vers leurs équivalents JavaScript :

| Nolang | JavaScript | Remarque |
| --- | --- | --- |
| `print(x)` | `console.log(x)` | Retour à la ligne automatique |
| `eprint(x)` | `console.error(x)` | Sortie vers stderr |
| `format(...)` | Concaténation de chaînes | v1 simplifiée : concaténation avec `"" +` |
| `len(x)` | `x.length` | Convient aux chaînes et aux tableaux |
| `with-len(n)` | `new Array(n)` | Crée un tableau de longueur donnée |

## Bibliothèque standard du backend JS

Le répertoire `src/js/` fournit des modules exclusifs au backend JS, tous portant l'annotation `#{js}` :

### API navigateur

#### `js/dom` — Manipulation du DOM

```no
# js/dom

; Créer des éléments
el = dom.create-element('div')
heading = dom.create-element('h2')

; Rechercher des éléments
el = dom.get-element-by-id('my-id')
el = dom.query-selector('.my-class')

; Récupérer le body
body = dom.body()

; Méthodes d'élément (mappées par builtin.go)
el.set-text('Hello')
el.set-style('color', 'red')
el.set-attr('data-id', '42')
el.append-child(child)
```

#### `js/canvas` — Dessin Canvas 2D

```no
# js/canvas

canvasEl = dom.create-element('canvas')
canvasEl.set-attr('width', '240')
canvasEl.set-attr('height', '140')
body.append-child(canvasEl)

ctx = canvas.get-context-2d(canvasEl)
ctx.set-fill('red')
ctx.fill-rect(10, 10, 60, 60)
ctx.set-stroke('orange')
ctx.begin-path()
ctx.move-to(120, 80)
ctx.line-to(80, 130)
ctx.line-to(160, 130)
ctx.fill()
ctx.stroke()
```

#### `js/events` — Gestion des événements

```no
# js/events

btn = dom.create-element('button')
btn.set-text('Click me')
body.append-child(btn)

; Rappel anonyme
events.on-click(btn, () {
    print('button was clicked!')
})

; Chargement de la page
events.on-load(() {
    print('page loaded')
})
```

#### `js/storage` — localStorage

```no
# js/storage

storage.set-item('key', 'value')
val = storage.get-item('key')
storage.remove-item('key')
storage.clear()
```

#### `js/location` — Location API

```no
# js/location

href = location.href()
search = location.search()
path = location.path()
location.redirect('https://example.com')
```

#### `js/history` — History API

```no
# js/history

history.back()
history.forward()
history.push('/page2')
n = history.length()
```

#### `js/animation` — Images d'animation

```no
# js/animation

id = animation.request-frame(() {

    ; Rappel à chaque image
    print('frame')
})
animation.cancel-frame(id)
```

### API Node.js

#### `js/fs-read-file` / `js/fs-write-file` — Lecture/écriture de fichiers

```no
# js/fs-read-file
# js/fs-write-file

data = fs-read-file('input.txt')
fs-write-file('output.txt', data)
```

#### `js/http-fetch` — Requête HTTP

```no
# js/http-fetch

data = http-fetch('https://api.example.com/data')
print(data)
```

#### `js/process-exit` — Sortie de processus

```no
# js/process-exit

process-exit(0)
```

#### `js/fetch` — Fetch API (async)

```no
# js/fetch

; Récupération asynchrone de données URL
data = fetch.async('https://api.example.com/data')
json-data = fetch.json-async('https://api.example.com/data')
```

## Mode navigateur

Lorsque `--browser` est utilisé, le compilateur génère :

1. **Un fichier JS** : le code JavaScript compilé
2. **Un fichier HTML** : un wrapper HTML qui référence le JS, contenant un div `#nolang-output`

Caractéristiques du wrapper HTML :

- La sortie de `print()` est redirigée vers `<div id="nolang-output">`
- La page inclut des styles de base (police, bordure, zone de sortie en monospace)
- L'artefact compilé est référencé via `<script src="name.js">`

Le chemin de sortie vaut par défaut le répertoire `dist/`.

## Import de modules ESM tiers

Le backend JS peut consommer des paquets de l'écosystème npm (l'équivalent de l'`import` de TypeScript), **sans nouvelle instruction `import`** : on réutilise la directive `#` avec le préfixe réservé `npm/` :

```no
# npm/lodash; objet module entier : lodash.get(x, 'a.b')
# npm/lodash.get as get; un seul export nommé : get(x, 'a.b')
# npm/lodash.default as d; l'export par défaut du paquet : d(props)
# npm/@scope/pkg; paquets scopés
# npm/lodash/fp.get as fpg; import de sous-chemin
```

Règles :

- **Backend JS obligatoire.** Les backends LLVM et Direct-WASM rejettent `# npm/...` avec une indication d'utiliser `no build --js`.
- **Le paquet doit être déclaré** dans les `dependencies` (ou `devDependencies`) de `package.noml`, sinon la compilation échoue. Les imports de sous-chemin (`lodash/fp`) nécessitent seulement la présence du paquet parent (`lodash`) dans la liste.
- **La qualification par nom court reste inchangée** : après `# npm/lodash`, on écrit `lodash.get(...)` ; un import nommé (`as get`) s'appelle directement `get(...)`.
- **v1 sans transitivité de type** : les liaisons importées ne portent aucun type Nolang (aucune modification du checker), donc aucune vérification de signature n'a lieu ; en cas de collision avec une fonction locale de même nom, le côté import est renommé automatiquement (suffixe `$esm`).
- **En cas de collision avec un module intégré, l'import prévaut** : après `# npm/math`, `math.sin(x)` vise le paquet npm `math`, et non `Math.sin`.

### Exécution sur Node.js

L'artefact est un module ES standard (les instructions `import` ne peuvent apparaître qu'au niveau module). `no build --js` dépose un `package.json` contenant `{"type": "module"}` à côté de l'artefact (convention npm) afin que node l'exécute en ESM ; si le répertoire contient déjà un `package.json` non-ESM, la compilation suggère d'utiliser plutôt `-o out.mjs`.

```bash
npm install lodash                    # 先安裝套件
no build --js main.no                 # 產出 dist/main.js（+ dist/package.json）
node dist/main.js                     # 以 ESM 執行
no run --js main.no                   # 建置到暫存目錄後自動以 node 執行
```

`no run --js` lie le `node_modules` du projet dans le répertoire temporaire (node résout les paquets en remontant depuis le répertoire du script) et échoue avec une invite d'installation si un paquet manque ; `no build --js` n'émet qu'un avertissement pour un paquet manquant (l'artefact reste valide — les paquets ne sont nécessaires qu'à l'exécution).

### Exécution dans le navigateur

Avec `--browser`, le HTML devient un `<script type="module">` accompagné d'une import map qui résout les spécificateurs nus via un CDN (par défaut `https://esm.sh`) :

```html
<script type="importmap">
{"imports": {"lodash": "https://esm.sh/lodash"}}
</script>
<script type="module" src="main.js"></script>
```

Pour changer de CDN (priorité décroissante) :

1. `no build --js --browser --esm-cdn https://cdn.jsdelivr.net/npm main.no`
2. `compiler: { esm-cdn: 'https://cdn.jsdelivr.net/npm' }` dans `package.noml`

Comme `<script type="module">` est bloqué par CORS sous `file://`, `no run --js --browser` démarre un serveur statique jetable sur `127.0.0.1` puis ouvre le navigateur (Ctrl+C pour arrêter). Pour un exemple complet, voir `example/esm-imports/`.

## Paquets natifs : WASM et FFI

Outre l'import de paquets JS, le backend JS peut appeler des paquets natifs (bibliothèque partagée à ABI C / WASM). Aucune nouvelle syntaxe n'est ajoutée : le chemin d'appel est choisi au moyen d'une annotation sur les déclarations existantes. Les déclarations de fonction C (`#{c}` + `name = (params) (results)`, voir le chapitre FFI) constituent l'interface native ; le backend JS les ignorait, mais avec une annotation `bind`, il génère une liaison d'appel pour chaque extern `#{c}`.

### Annotations

`bind` est une **stratégie de compilation** : il peut s'écrire au-dessus d'une ligne `# import` (sur sa propre ligne) ou en fin de ligne, ou encore directement dans le groupe d'annotations au-dessus d'un extern `#{c}`.

| Clé | Valeurs | Description |
| --- | --- | --- |
| `bind` | `auto` / `ffi` / `wasm` | Mode d'appel ; une seule stratégie autorisée par compilation JS |
| `wasm` | Chaîne | Spécificateur du module WASM ; obligatoire pour `bind=wasm` et pour `auto` sur navigateur |
| `lib` | Chaîne | Remplace le nom de la bibliothèque FFI (par défaut `compiler.link-libs`) |
| `init` | Identifiant | Nom de l'export d'initialisation WASM, `default` par défaut |

```no
; pow de libm : FFI via koffi ; le navigateur nécessite un specifier wasm=
#{c, bind=auto}
pow = (x f64, y f64) (r f64)
```

### Matrice cible × stratégie

| Cible | `bind=ffi` | `bind=wasm` | `bind=auto` |
| --- | --- | --- | --- |
| node / bun / deno | Liaison koffi des externs | Import WASM + ready | avec `link-libs` / `lib=` → FFI ; sinon nécessite `wasm=` → WASM ; sinon erreur |
| navigateur | **Erreur à la compilation** (koffi ne peut pas fonctionner dans un navigateur) | Import WASM + ready | avec `wasm=` → WASM ; sinon erreur |

Règles supplémentaires : `bind=ffi` nécessite au moins un extern `#{c}`, sinon erreur ; sans `link-libs` ni `lib=`, erreur également. Des stratégies `bind` / `wasm` contradictoires à plusieurs endroits font échouer la compilation.

### FFI (local / node)

La FFI passe par [`koffi`](https://koffi.dev), une vraie FFI : `koffi.load(<bibliothèque>)` + `lib.func(symbol, ret, args)`. La bibliothèque provient de `compiler.link-libs` dans `package.noml` (`lib=` permet de la remplacer) ; plusieurs bibliothèques sont essayées dans l'ordre, la première qui convient étant retenue. Chaque extern `#{c}` produit une liaison dont le nom local est `jsIdent(name)` (`_sqlite3-open` → `_sqlite3Open`) et dont le nom de symbole C retire le `_` initial et remplace `-` par `_` (`sqlite3_open`). Mappage des types :

| Nolang | koffi | Nolang | koffi |
| --- | --- | --- | --- |
| `i8` / `u8` / `byte` | `int8` / `uint8` | `i32` / `u32` | `int` / `uint` |
| `i16` / `u16` | `int16` / `uint16` | `i64` / `u64` | `int64` / `uint64` |
| `f32` / `f64` | `float` / `double` | `str` | `str` |
| `bool` | `bool` | `void` / aucun retour | `void` |
| `*T` | `koffi.pointer(<T>)` | `**T` | `koffi.pointer(koffi.pointer(<T>))` |

Tout le reste (tranches / tableaux / structures) retombe sur `"void *"` en v1. `koffi` est ajouté comme dépendance ESM synthétique : `no build` n'émet qu'un avertissement s'il manque, tandis que `no run --js` échoue et suggère `npm install koffi` (même politique que pour ESM). koffi étant du CJS, il passe par la couche d'interopérabilité `import * as` + `.default ??`, plutôt que par un import nommé nu.

### WASM (navigateur / `bind=wasm`)

Le mode WASM importe le module `.wasm` indiqué et transfère chaque extern `#{c}` vers son export de même nom (`_sqlite3-open` → export wasm `sqlite3_open`). L'initialisation produit une Promise de disponibilité (**non attendue**) : un module initialisé de façon synchrone est immédiatement prêt ; un module asynchrone bascule son état lorsqu'il est prêt et expose la Promise de disponibilité sur `globalThis.__nolang_wasm_ready[<nom-court>]` pour que le JS externe l'attende. Un appel avant la disponibilité lève une erreur explicite.

Le spécificateur WASM se présente sous deux formes, avec des règles différentes selon la forme :

| Forme | Exemple | Entrée `dependencies` | Import map navigateur | `no run --js` |
| --- | --- | --- | --- | --- |
| nue (paquet npm) | `@sqlite.org/sqlite-wasm` | **Obligatoire** | Résolue via `--esm-cdn` (par défaut `https://esm.sh`) | Nécessite `npm install` |
| relative (fichier local) | `./math-wasm.mjs` | **Inutile** | **Absente de la map** (les navigateurs ne remappent jamais les spécificateurs relatifs) | Le fichier (et ses propres imports relatifs) est lié dans le répertoire temporaire du bundle |

La forme relative couvre le cas « le module se trouve dans le projet » : un artefact `no build --js` placé à côté du module fonctionne directement, `no run --js` lie le module dans son répertoire temporaire, et pour une cible navigateur il faut copier le module à côté de la sortie `dist/` (l'import se résout relativement au document).

### Exemple complet

- `example/native-libm/` : `#{bind = auto, c}` déclare le `pow` de libm, et `no run --js main.no` affiche `1024` (`no run` nécessite d'abord un `npm install` dans ce répertoire pour installer `koffi`).
- `example/native-wasm/` : un module WASM local (`math.wasm` + `math-wasm.mjs`, zéro dépendance npm) ; `no run --js main.no` affiche `42.25` et `42`, et l'import map de `no build --js --browser main.no` n'inclut pas le spécificateur relatif.

### Limites connues (v1)

- Le WASM ne fait que **transférer des exports de même nom** ; aucune couche d'adaptation d'API n'est générée (faire correspondre une API à un autre paquet WASM, par ex. `better-sqlite3` → `sql.js`, nécessite votre propre module wrapper Nolang).
- La stratégie est **au niveau du programme** : une compilation JS n'autorise qu'une stratégie `bind` pour tous les externs `#{c}` ; l'association par module reste à faire.
- La FFI ne lie que les bibliothèques partagées à ABI C ; pour les addons natifs Node (`.node`), utilisez `bind=wasm` ou un import ESM ordinaire.

## Exemple complet

```no
; main.no — démonstration d'application navigateur
# js/dom
# js/canvas
# js/events
# js/storage

print('=== Nolang Browser Demo ===')

; DOM : créer un titre et l'ajouter au body
heading = dom.create-element('h2')
heading.set-text('Hello from Nolang!')
body = dom.body()
body.append-child(heading)

; DOM : créer un bouton
btn = dom.create-element('button')
btn.set-text('Click me')
btn.set-style('margin', '8px')
body.append-child(btn)

; Événements : clic sur le bouton
events.on-click(btn, () {
    print('button was clicked!')
})

; Canvas : dessiner des rectangles
canvasEl = dom.create-element('canvas')
canvasEl.set-attr('width', '240')
canvasEl.set-attr('height', '140')
body.append-child(canvasEl)

ctx = canvas.get-context-2d(canvasEl)
ctx.set-fill('red')
ctx.fill-rect(10, 10, 60, 60)
ctx.set-fill('blue')
ctx.fill-rect(80, 10, 60, 60)

; localStorage : sauvegarder et lire
storage.set-item('greeting', 'Hello from localStorage')
g = storage.get-item('greeting')
print('stored:', g)

; Itération
nums = [10, 20, 30]
i <- nums: {
    print('elem:', i)
}

; Branche de plateforme
#{js-browser}
print('running in browser mode')

print('=== done ===')
```

Compilation :

```bash
no build --js --browser main.no
# 輸出: dist/main.js, dist/main.html
# 在瀏覽器中打開 dist/main.html
```
