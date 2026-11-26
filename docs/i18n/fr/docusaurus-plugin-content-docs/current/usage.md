---
sidebar_position: 2
---

# Installation et utilisation

## Installation

### Installer l'interface CLI

Téléchargez l'exécutable correspondant à votre plateforme depuis [GitHub Releases](https://github.com/lizongying/nolang/releases/latest), ou installez-le de la manière suivante :

```bash
# macOS / Linux
# 1. 下載二進制文件
# 2. 放到 PATH 中
sudo mv nolang /usr/local/bin/no
```

### Installer l'extension VS Code

Installez l'extension Nolang depuis le [VS Code Marketplace](https://marketplace.visualstudio.com/items?itemName=lizongying.vscode-nolang) : coloration syntaxique, diagnostics LSP, aller-à-la-définition, auto-complétion, etc.

## Commandes CLI

| Commande                                                         | Description                    |
| ------------------------------------------------------------ | ----------------------- |
| `no version`                                                 | Affiche les informations de version            |
| `no init`                                                    | Définit l'espace de travail (génère workspace.noml, sans package.noml) |
| `no new <name>`                                              | Crée un nouveau paquet dans l'espace de travail (sous-répertoire + package.noml, enregistré dans workspace.noml) |
| `no fmt [-w] [-d] <file\|dir>`                               | Formate le code source (déduplication/tri des imports, suppression automatique des imports de module std sans alias) ; les fichiers de configuration `.noml` suivent une règle de mise en forme préservant la disposition (indentation/espaces/lignes vides, commentaires et ordre des clés inchangés) |
| `no build [-o <file>] [-cc <s>] [-target <s>] [-opt[=N]] [<file\|dir>]` | Compile (produit un exécutable) |
| `no run [-cc <s>] [-target <s>] [<package\|dir\|file>]`        | Compile et exécute (nom de paquet/répertoire/fichier) |
| `no test [-cc <s>] [-target <s>] [<file>]`                   | Exécute les tests                |
| `no add <tool>[:<version>]@<host>/<owner>/<repo>`            | Ajoute une dépendance (module AST précompilé)|
| `no remove <pkg>`                                            | Supprime une dépendance                |
| `no update <pkg>`                                            | Met à jour une dépendance                |
| `no update-all`                                              | Met à jour toutes les dépendances            |
| `no list`                                                    | Liste les dépendances                |
| `no sync`                                                    | Synchronise les dépendances                |
| `no install [-u] [<tool>[:<version>]@<host>/<owner>/<repo>]`  | Installe un binaire             |
| `no uninstall <name>`                                        | Supprime un binaire             |
| `no pack [-o <file>] [<dir\|file.no>]`                       | Empaquette l'AST (`<tool>-ast.zip`)|
| `no pub [-o <file>]`                                         | Identique à `no pack` (envoi vers le registre non implémenté)|
| `no cache <stat\|clear>`                                     | Affiche / vide le cache AST (normalement inutile de le vider)|

## Démarrage rapide

Nolang adopte une architecture **mono-dépôt (projet) multi-paquets** : la racine du dépôt est l'espace de travail (il n'y a que `workspace.noml`), et chaque paquet est un sous-répertoire (avec son propre `package.noml`). `no init` et `no new` sont deux opérations distinctes :

- `no init` —— définit l'espace de travail dans le répertoire courant (génère uniquement `workspace.noml`).
- `no new <name>` —— crée un nouveau paquet dans l'espace de travail courant (génère le sous-répertoire `./<name>/` et son `package.noml`) et enregistre ce paquet dans `workspace.noml`.

### Initialiser l'espace de travail et créer un paquet

```bash
# 1) 在倉庫根目錄定義工作區
no init

# 2) 在工作區內新建包（會自動註冊到 workspace.noml）
no new foo

# 3) 進入包目錄
cd foo

# 直接運行（自動構建並執行 main.no）
no run
```

Vous pouvez utiliser `no new` à plusieurs reprises dans l'espace de travail pour ajouter d'autres paquets :

```bash
no new bar
no new baz
```

À ce stade, `workspace.noml` ressemble à :

```noml
{
  foo: '/foo'
  bar: '/bar'
  baz: '/baz'
}
```

> **Format des chemins** : dans `workspace.noml`, les valeurs de mappage utilisent le préfixe `/` pour les chemins relatifs à l'espace de travail (p. ex. `/foo` → `workspaceRoot/foo`). Les sauts relatifs `./`, `../` et les chemins absolus du système d'exploitation (p. ex. `/home/user/code/fork`) sont également pris en charge, ce qui permet de pointer vers des répertoires hors de l'espace de travail.

Lorsque `no build` est exécuté à la racine sans arguments, il compile en parallèle tous les paquets de l'espace de travail.

### Initialiser uniquement l'espace de travail (sans créer de paquet)

```bash
# 在當前目錄定義工作區
no init
```

`no init` ne génère que `workspace.noml` (initialement vide `{}`) ; il **ne génère ni `package.noml` ni `main.no`**. Si `workspace.noml` existe déjà, il n'est pas écrasé. Les paquets doivent être créés avec `no new <name>`.

### Compilation et exécution

```bash
# 構建（默認尋找 main.no）
no build                    # 構建當前目錄
no build main.no            # 構建指定文件
no build -o output          # 指定輸出路徑
no build -cc zig            # 使用 Zig 編譯器
no build -target x86_64-linux-gnu  # 交叉編譯（指定目標平台）

# 運行（構建 + 執行）
no run                      # 構建並執行當前目錄的 main.no
no run foo                  # 運行工作區中名為 foo 的包（解析 workspace.noml）
no run ./foo                # 運行 ./foo 目錄的 main.no
no run main.no              # 運行指定 .no 文件
no run -cc zig
no run -target aarch64-macos-gnu
```

> `no build -v` écrit également le LLVM IR brut à côté du chemin de sortie (`<chemin de sortie>.ll`). `no build --shared -o libdemo.dylib main.no` exporte les fonctions de premier niveau marquées `#{cabi='symbol'}` comme symboles ABI C stables, lie une bibliothèque dynamique avec `-shared -fPIC` et génère automatiquement un fichier d'en-tête `.h` du même nom ; l'option `--header` permet de remplacer le chemin de l'en-tête. Il n'existe pas encore d'option `-c` (compilation sans édition de liens). Pour la procédure complète permettant à C / C++ / Rust / Go / Java / Python / C# d'appeler Nolang, voir [Appeler Nolang depuis les langages hôtes](./lang/host-interop).

L'argument positionnel de `no run` est résolu dans l'ordre suivant :

1. un fichier `.no` existant → exécute directement ce fichier
2. un répertoire existant → exécute son `main.no`
3. un nom de paquet enregistré dans l'espace de travail (le `workspace.noml` le plus proche) → exécute le `main.no` de ce paquet

Sans argument, il exécute par défaut le `main.no` du répertoire courant. Si le répertoire courant est la racine de l'espace de travail (contient `workspace.noml` mais pas de `main.no`), un message invite à préciser le paquet avec `no run <package>`.

### Cibles de compilation croisée

Le paramètre `-target` suit le format `<arch>-<os>-<abi>` et prend en charge les cibles suivantes :

| Triplet cible        | Description    |
| -------------------- | -------------- |
| `x86_64-linux-gnu`   | Linux x86_64   |
| `aarch64-linux-gnu`  | Linux ARM64    |
| `x86_64-macos-gnu`   | macOS x86_64   |
| `aarch64-macos-gnu`  | macOS ARM64    |
| `x86_64-windows-gnu` | Windows x86_64 |

**Détection automatique de la plateforme** : lorsque `-target` n'est pas précisé, `no build`, `no run` et `no test` détectent automatiquement la plateforme hôte courante et compilent pour la cible native. Aucune spécification manuelle de la cible n'est nécessaire au quotidien :

```bash
no run hello.no          # 本機直接跑
no test                  # 本機跑測試
no build -target aarch64-linux-gnu   # 需要交叉編譯時才顯式指定
```

### Sélection du compilateur

Le paramètre `-cc` spécifie le backend du compilateur C :

- `clang` (par défaut) — nécessite LLVM installé
- `zig` — nécessite Zig installé, adapté à la compilation croisée

### Disposition des types intégrés (seuil d'inlining de `?T`)

La disposition en mémoire de `?T` est **déterminée par le type de la charge utile** : chaque type de charge `T` dispose de son propre `%option_<key> = { i8 tag, [K x i64] }` (étiquette d'un octet, charge à l'offset d'octet 8, `K = roundUp8(sizeof(T))/8`). Si la charge ne tient pas dans le seuil, elle est placée dans une boîte sur le tas et le premier mot de la zone de charge contient le pointeur.

`--option-payload-inline-threshold=N` définit ce seuil (mesuré en octets sur **toute la structure option**, soit `1 + 7 + sizeof(T)`) :

```bash
no build --option-payload-inline-threshold=1032 main.no   # 讓更大的載荷內聯
no build --option-payload-inline-threshold=16 main.no     # 最小可配置值
```

| Valeur | Effet |
| --- | --- |
| par défaut **264** (= 1 + 7 + 256) | `?i64` 16 o, `?str` 24 o, `?txt` (256 o) 264 o en ligne ; les charges plus grandes sont placées dans une boîte |
| minimum **16** | seules les charges de la taille de `?i64` restent en ligne ; à partir de `?str` (24 o), tout est placé dans une boîte |
| < 16 | erreur de compilation |

Ordre de priorité : ligne de commande > variable d'environnement `NOLANG_OPTION_PAYLOAD_INLINE_THRESHOLD` > `compiler.option-payload-inline-threshold` de `package.noml` > valeur par défaut. La source unique de la disposition est `src/mir/layout.go` ; la sémantique (quelles valeurs comptent comme `ok` / `nil` / `err`) **n'est pas affectée** — seules l'occupation mémoire et la mise en boîte changent.

### Optimiseur intégré du compilateur (MIR opt)

Le compilateur intègre un optimiseur qui agit au **niveau MIR** (après l'abaissement HIR, avant la génération de code LLVM). Il est distinct de l'`opt` LLVM du backend : le premier réécrit le MIR, le second réécrit le LLVM IR déjà généré.

Activez-le avec `-opt[=N]`, **désactivé par défaut** :

```bash
no build main.no          # 預設：不啟用（與歷史行為一致）
no build -opt main.no     # 等同 -opt=1
no build -opt=2 main.no   # 常數折疊 + 死碼消除 + 控制流清理
no build -opt=0 main.no   # 顯式關閉
```

| Niveau | Signification |
| ---- | ---- |
| `0` / `off` / `false` / `no` / `none` | désactivé (par défaut) |
| `1` / `on` / `true` / `yes` / `-opt` | repli de constantes + réécriture d'identités entières + propagation de copies + élimination des scalaires morts |
| `2` | niveau 1 + nettoyage du flot de contrôle (repli de branches constantes, suppression de blocs inaccessibles, fusion des sauts de blocs vides, fusion de blocs à prédécesseur unique) + élagage du prélude |

`-opt` est un interrupteur **global** (traité dans `main`, comme `-v`) ; `build`, `run` et `test` le prennent donc tous en charge.

Le même interrupteur peut aussi être défini par la variable d'environnement `NOLANG_MIR_OPT` ; l'option de ligne de commande ne fait qu'écrire dans cette variable, il n'y a donc pas d'incohérence possible. Pour voir ce que l'optimisation a fait :

```bash
NOLANG_MIR_OPT=2 NOLANG_MIR_OPT_STATS=1 no build -o out main.no
# [miropt] level=2 funcs=3 folded=5 (arith=2 cmp=3) ident=1 copies=4 dead=6 cfg{condbr=3 unreachable=4(5 insts) threaded=1 merged=5}
# [miropt] prelude: dropped 27 unreferenced helpers
```

| Champ | Signification |
| ---- | ---- |
| `folded`（`arith`/`cmp`） | nombre d'instructions arithmétiques/comparaison repliées en constantes |
| `ident` | identités entières simplifiées en un opérande (`x+0`, `x*1`, `x&x`, `x^0`, …) |
| `copies` | copies scalaires supprimées par propagation de copies |
| `dead` | instructions scalaires mortes supprimées |
| `cfg{condbr, unreachable, threaded, merged}` | repli de branches constantes, suppression de blocs inaccessibles (avec le nombre d'instructions), fusion des sauts de blocs vides, fusion de blocs à prédécesseur unique |
| `prelude: dropped N` | nombre de helpers du runtime du prélude du codegen prouvés non référencés (voir ci-dessous) |

### Élagage du prélude (niveau 2)

`emitPrelude` écrit tout le runtime (`str_*`, `print_*`, `eprint_*`, `vec_*`, `nolang_*`) dans **chaque** module, que le programme l'utilise ou non — un programme `print('hi')` génère 60 définitions de fonctions, dont seulement 8 sont accessibles depuis le point d'entrée et **52 ne sont jamais appelées**.

**Cela, `opt -O2` ne peut pas le faire** : le GlobalDCE de LLVM ne supprime que les définitions `internal`, alors que le prélude est émis avec une liaison externe (les modules `no` peuvent être liés à du C, et les helpers dont l'éditeur de liens pourrait avoir besoin ne peuvent pas être `internal`). Sur ce programme, exactement 7 des 60 définitions sont `define internal`, et `opt -O2` ne supprime précisément que ces 7-là ; le `.ll` passe au contraire de 61 812 à 69 874 octets (+8 062).

Le niveau 2 effectue donc, une fois l'IR générée, un élagage par accessibilité sur le **texte achevé**, ne conservant que les helpers réellement référencés (et, récursivement, ceux qu'ils appellent eux-mêmes). Pour le même `print('hi')` : nombre de définitions 60 → **16**, `.ll` 61 812 → **14 548** octets (**−76,5 %**), exécutable 36 616 → 34 648 (−5,4 %). L'élagage ne reconnaît que les références réelles — **les noms apparaissant dans les commentaires ne comptent pas**, ce qui est la clé de sa capacité à supprimer 44 helpers au lieu de 33 ; et lorsqu'une fonction est supprimée, **son propre commentaire descriptif l'est aussi** (`optTrailingComment`), sinon 12 609 octets resteraient à décrire 44 fonctions qui n'existent plus.

Supprimer les commentaires **ne réduit pas l'exécutable** — les commentaires sont écartés dès l'analyse par `opt`/`llc`, et les deux exécutables font 34 648 octets. Les 12 609 octets sont un gain sur le texte IR et sur la charge d'analyse à la compilation.

Les approximations de l'élagage penchent toujours vers la conservation : un bloc dont on ne peut résoudre un nom unique est conservé, si un bloc n'a pas d'accolade fermante l'élagage s'arrête à cet endroit, et les `declare` / globals / définitions de types ne sont jamais supprimés. Manquer une optimisation ne coûte que quelques octets ; en supprimer une à tort provoque une erreur d'édition de liens.

**Peut-on supprimer entièrement le prélude ? Non.** La zone du prélude ne contient pas que des fonctions : elle contient aussi **les littéraux de chaîne du programme lui-même** (`@.mir.str.N`, c'est-à-dire `"hi"`), des types comme `%str-long` / `%option` / `%task`, et des déclarations externes comme `@malloc` / `@write` / `@llvm.memcpy` — ces données représentent environ 30 % du module, et les supprimer empêcherait même `llc` de passer (`error: use of undefined value '@.mir.str.1'`). Seule la moitié **fonctions** peut être supprimée : après avoir remplacé les 48 `define` par des `declare` de même nom, les symboles non définis signalés par l'éditeur de liens sont précisément `print_nl`, `print_str`, `str_free` et `str_from_const`. La bonne question est donc « peut-on émettre à la demande », et non « peut-on supprimer ».

Ce gaspillage est un **impôt fixe, indépendant de la taille du programme** : le code et les commentaires du prélude sont identiques d'un module à l'autre (48 définitions, 36 043 octets de code + 17 516 octets de commentaires), et sur un échantillon de 40 fichiers le nombre conservé va de **0 à 19, avec une médiane de 9**. `tests/dup.no` / `tests/q.no` / `tests/u128-types.no` en conservent **0** — tout le prélude est mort pour eux. Le taux d'économie dépend donc entièrement de la taille du module : −76,5 % pour un programme d'une ligne, seulement −4,1 % pour `test-std/src/pbkdf2.no` (3,0 Mo).

Le prélude peut-il être supprimé entièrement ? Non — la zone contient encore les littéraux de chaîne du programme (`@.mir.str.N`), des types et des déclarations externes (1 151 octets) ; le supprimer empêcherait `llc` de passer. De plus, `extraFuncs` / `extraFuncsBody` dans `codegen.go:75-83` est déjà un émetteur à la demande (les helpers option / struct / enum / ptr passent tous par lui) ; le prélude est la seule partie qui n'a pas été convertie, mais le rendre à la demande ne rapporterait presque que des commentaires — il n'y a plus rien à gagner du côté du code.

L'optimiseur ne replie que les **scalaires** (`i64`/`u64`/`i16`/`i32`/`char`/`bool`/`f32`/`f64`). `byte`/`u8`/`i8`, `i128`/`u128`, `option`, les tranches, les structures, etc. sont tous ignorés : les premiers n'ont pas de branche correspondante dans `emitConst` (ils dégénéreraient en 0) ; pour les seconds, l'arithmétique passe par le chemin de débordement encapsulé de `option`, et le repli changerait la sémantique. Les flottants sont également exclus de toutes les identités — `x * 0.0` n'est pas 0 pour `inf`/`NaN`, et `x + 0.0` n'est pas `x` pour `-0.0`.

La conclusion centrale de cette comparaison mérite d'être énoncée ici : **le nettoyage local (repli de constantes, élimination de code mort, nettoyage du flot de contrôle) a une valeur marginale presque nulle après `opt -O2`** (−0,06 %), car LLVM fait le même travail de façon plus approfondie. Ce qui rapporte vraiment, c'est ce que LLVM **ne peut pas faire à cause de la liaison** — à savoir l'élagage du prélude décrit ci-dessus. Avec l'élagage du prélude (y compris l'élagage sensible aux commentaires), la valeur marginale globale de `mir+llvm_O2` → `mir_opt+llvm_O2` est de **−19,05 % instructions / −15,83 % texte** ; la réduction de texte de `mir` → `mir+mir_opt` passe de −14,29 % à **−17,75 %** (corpus complet de 631 fichiers).

## Backend JavaScript

Le backend JS (`no build --js` / `no run --js`) compile le code source `.no` en JavaScript (effacement des types) ; il cible Node.js comme le navigateur, prend en charge les annotations de plateforme `#{js}` / `#{js-browser}` et les imports ESM tiers (`# npm/<pkg>`).

Toute la documentation relative à JS (options de ligne de commande, correspondance des fonctions intégrées, bibliothèque standard `js/*`, import maps du navigateur, etc.) est regroupée dans la page dédiée [Backend JS](./lang/js-backend) ; elle n'est pas répétée ici.

## Règles du point d'entrée

- **main.no** — point d'entrée du programme
- **lib.no** — point d'entrée de la bibliothèque, exporte des fonctions (voir [Documentation sur l'export](lang/export))
- **Tous les fichiers .no du répertoire tests/** — contiennent des assertions de test

## Tests

```bash
# 測試tests目錄下所有 .no 文件
no test

# 執行單個測試文件
no test my-test.no

# 使用指定編譯器或目標
no test -cc zig
no test -target x86_64-windows-gnu
```

Notes sur les tests :

- Les fichiers de test sont tous placés dans le répertoire tests/
- Chaque fichier de test est compilé indépendamment
- Si un test échoue, un code de sortie non nul est renvoyé

## Cache AST

`no build` / `no run` / `no test` écrivent sur disque l'AST issu de l'analyse de chaque fichier source utilisateur (par défaut `~/.cache/nolang/ast`, chemin modifiable avec `$NOLANG_AST_CACHE_DIR`) ; si le contenu n'a pas changé la fois suivante, il est réutilisé directement, économisant l'analyse lexicale et syntaxique. La bibliothèque standard passe par le blob intégré au compilateur et n'utilise pas ce cache.

**Aucun nettoyage manuel n'est nécessaire.** La clé de cache est :

```
sha256( ASTVersion ‖ 解析期 salt ‖ 源文件內容 )
```

L'un des éléments du salt est `checker.StdSigKey` — la signature de contenu de tous les fichiers sources listés dans `src/hash.noml` (bibliothèque standard + compilateur lui-même). Ainsi, toute modification du compilateur ou de la bibliothèque standard donne une nouvelle clé au `no` reconstruit avec `make` ; les anciennes entrées ne sont plus trouvées et ne sont pas réutilisées.

| Commande | Description |
| --- | --- |
| `no cache stat` | Affiche le répertoire du cache, le nombre d'entrées, la taille totale et l'`ASTVersion` actuelle |
| `no cache clear` | Vide toutes les entrées (uniquement en dépannage : lorsque des entrées semblent obsolètes) |

Variables d'environnement :

| Variable | Rôle |
| --- | --- |
| `NOLANG_AST_CACHE_OFF=1` | Désactive le cache (utile pour les comparaisons A/B de compilateurs et pour diagnostiquer un « bug corrigé qui réapparaît ») |
| `NOLANG_AST_CACHE_DIR` | Répertoire de cache personnalisé. S'applique **également** au cache `-ast.zip` des dépendances AST précompilées (ce cache est par défaut dans `~/no/cache/ast`, voir « Dépendances AST précompilées » ci-dessous) |
| `NOLANG_AST_CACHE_MAX` | Nombre maximal d'entrées du cache (4096 par défaut) |

> Les anciennes entrées ne sont simplement plus trouvées et sont évincées au fur et à mesure de l'écriture de nouvelles entrées, selon la limite ; il est donc **inutile** de supprimer `~/.cache/nolang/ast`.
> Attention : `no` doit être reconstruit avec `make gen` / `make no` ; un binaire compilé manuellement avec `cd src && go build` n'a pas de `StdSigKey` injecté et quittera directement avec une erreur plutôt que de réutiliser silencieusement le cache.

## Installation et désinstallation de binaires

### Installation

```bash
# 安裝當前目錄的包（本地構建）
no install
no install -u                                   # 強制重構（更新）

# 從 GitHub Release 安裝預編 binary（推薦）：<tool>[:<version>]@<host>/<owner>/<repo>
no install nogit@github.com/lizongying/notools            # 最新版
no install nogit:v0.1.33@github.com/lizongying/notools    # 指定版本
no install nogit@lizongying/notools                       # 省略 host，默認 github.com
no install -u nogit@github.com/lizongying/notools         # 強制重新下載
```

L'installation distante récupère un **binaire précompilé**, pas le code source : le nom de l'actif est `<tool>-<os>-<arch>-<version>` (avec `.exe` sous Windows), où `<os>-<arch>` provient de la plateforme sur laquelle `no` s'exécute lui-même (p. ex. `darwin-arm64`, `linux-amd64`). Aucune chaîne d'outils LLVM locale n'est nécessaire, et l'outil installé n'a pas besoin d'être écrit en Nolang. Si la version est omise, `releases/latest` est consulté ; définir `$GITHUB_TOKEN` (ou `$GH_TOKEN`) évite la limitation de débit de l'API GitHub.

Procédure d'installation :

1. distant : analyse de la spécification → localisation de l'actif de la release → téléchargement ; local : compilation à partir du répertoire courant
2. écriture du binaire dans `~/no/bin/`
3. création d'un lien symbolique dans `/usr/local/bin/`

### Désinstallation

```bash
no uninstall pkg-name
```

La désinstallation supprime le lien symbolique dans `/usr/local/bin/` et le binaire dans `~/no/bin/`.

## Configuration du projet

### Format de configuration noml

Tous les fichiers de configuration de Nolang (`package.noml`, `workspace.noml`, `.workspace.noml`, `nolang.lock.noml`, `sum.noml`) utilisent le format **noml** (Nolang Markup Language) maison, en remplacement de l'ancien JSONC. Les règles de syntaxe s'alignent sur le langage Nolang lui-même :

| Règle | Description |
| --- | --- |
| Objet de premier niveau | un objet : `{` et `}` chacun sur leur propre ligne, un membre par ligne, **aucun séparateur par virgule** (le retour à la ligne sépare les membres) |
| Clés | identifiants nus par défaut, suivant les mêmes règles que les noms de variables Nolang : commencent par une lettre minuscule/un chiffre/un tiret/underscore, ne peuvent pas commencer par un chiffre (p. ex. `link-libs`, `anonymous-fn-type`, `dev-dependencies`) ; les clés qui ne respectent pas ces règles (chemins, URL, `@scope/pkg`) s'écrivent comme des clés chaîne entre guillemets simples |
| Valeurs chaîne | entre guillemets simples, échappement `\` : `\' \\ \n \t` |
| Booléens | `true` / `false` (tout en minuscules, comme les littéraux bool de Nolang) |
| Nombres | littéraux nus (réservés aux fichiers de verrouillage, etc.) |
| Tableaux | `[`, éléments, `,`, `]` peuvent s'étaler sur plusieurs lignes ; les éléments sont séparés par des **virgules**, **virgule finale autorisée** ; un tableau vide s'écrit `[]` |
| Commentaires | `;` jusqu'à la fin de ligne (sauf dans les chaînes) |
| Indentation | sans significance (uniquement pour la lisibilité, aucune garantie de formatage) |

`dependencies` / `dev-dependencies` sont des **tableaux de chaînes** dont les éléments ont la forme `'clé:version'` (la version peut être omise, par défaut `*`), par exemple :

```noml
{
  dependencies: [
    '/example/test2',                          ; 本地包，默認任意版本
    'github.com/lizongying/nolang/test1:v0.1.0', ; 線上包，指定版本,
  ]
}
```

Le fichier `package.noml` à la racine du projet décrit les informations du projet :

```noml
{
  name: 'my-project'
  version: 'v0.1.0'
  description: 'A new Nolang project'
  keywords: []
  author: ''
  email: ''
  organization: ''
  repository: ''
  homepage: ''
  license: 'MIT'
  dependencies: [
    'fmt',,
  ]
  compiler: {
    version: 'v0.1.0'
  }
  ignore: []
}
```

`compiler.lang` fixe la langue des messages du projet (`'zh-tw'`, `'ja'`, etc. — même orthographe que `--lang`) : elle est prioritaire sur la locale système, mais subordonnée à `--lang` et `NO_LANG` ; si elle est absente, la langue du système est utilisée.

### Gestion des dépendances

```bash
# 添加依賴：<tool>[:<version>]@<host>/<owner>/<repo>
# 版本號可省略（取最新 release）；寫入 package.noml 的鍵是 <tool>@<host>/<owner>/<repo>
no add nogit@github.com/lizongying/notools
no add nogit:v0.1.33@github.com/lizongying/notools

# 移除依賴
no remove pkg-name

# 更新依賴
no update pkg-name

# 更新所有依賴
no update-all

# 列出依賴
no list

# 同步依賴（下載並生成鎖文件）
no sync
```

### Dépendances AST précompilées

Les dépendances de la forme `no add <tool>@<host>/<owner>/<repo>` ne contiennent **que l'AST, pas le code source** : à la compilation, `<tool>-ast.zip` (zip with one .gob entry per module) est récupéré depuis la release, décodé puis injecté directement dans la chaîne de compilation. Utilisation :

```nolang
# nogit                      # alias du paquet ; les modules sont nogit.<module>
# nogit/remote                  # importer directement le module remote (ou nogit.remote)
# nogit.fn                      # nogit est seulement le nom du paquet ; trouver le module définissant fn
# <host>/<owner>/<repo>/nogit/remote
```

Un zip correspond à un paquet, qui peut contenir plusieurs modules, chacun sous forme d'entrée `<modulePath>.gob`. Il n'y a pas de module d'entrée : `# nogit` crée seulement un alias du paquet (les modules s'utilisent comme `nogit.<module>`, ex. `nogit.remote.greet(...)`) ; `# nogit/remote` importe directement un module ; `# nogit.fn` trouve un module par fonction. `no fmt` supprime les imports `# nogit` / `# nogit/remote` inutilisés. La version doit être une valeur précise (`no sync` / la compilation signalent une erreur sur `""` ou `"*"`). Le zip est mis en cache dans `~/no/cache/ast/`, chemin modifiable avec `$NOLANG_AST_CACHE_DIR`.

L'actif est produit avec `no pack` (analyseur lexical + syntaxique suffisent, aucune chaîne d'outils LLVM nécessaire, idéal pour la CI) :

```bash
no pack -o nogit-ast.zip nogit.no      # 單個源文件
no pack -o nogit-ast.zip ./nogit       # 一個包目錄
```

### Types de dépendances et règles de numérotation des versions

Les dépendances dans `package.noml` se divisent en deux catégories : **paquets locaux** et **paquets distants**. Le compilateur détermine automatiquement le type de dépendance selon les règles suivantes et émet un avertissement lorsqu'un paquet local n'utilise pas la version `"*"`.

#### Règles de détermination

```
依賴鍵 → 查找 workspace.noml（短名稱或完整鍵匹配）
  ├─ 找到 → 本地包（應使用 "*"）
  └─ 未找到 → 檢查路徑前綴（/ 開頭）
      ├─ 是 → 本地包（應使用 "*"）
      └─ 否 → 線上包（使用版本號，不警告）
```

1. **Recherche dans `workspace.noml`** : la clé de dépendance est d'abord recherchée dans `workspace.noml` comme nom court ou clé complète. Si elle est trouvée, c'est un paquet local.
2. **Vérification du préfixe de chemin** : si la clé n'est pas trouvée dans `workspace.noml` et commence par `/` (chemin relatif à l'espace de travail), c'est également un paquet local.
3. **Les autres sont des paquets distants** : toute clé ne remplissant pas les conditions ci-dessus est un paquet distant et doit préciser un numéro de version.

> **Contrainte de frontière de l'espace de travail** : dans `package.noml`, les chemins locaux ne peuvent commencer que par `/` (relatif à l'espace de travail) ; `./`, `../` et les chemins absolus du système d'exploitation sont interdits. La seule porte autorisée pour franchir la frontière de l'espace de travail est le mappage `workspace.noml` / `.workspace.noml`. Voir [Contrainte de frontière de l'espace de travail](#工作區邊界約束).

#### Les différentes formes de référence aux paquets locaux

Les paquets locaux prennent en charge les trois formes de référence suivantes, qui doivent toutes utiliser la version `"*"` :

```noml
dependencies: [
  ; 1. 短名稱：workspace.noml 中註冊的鍵
  'test2',
  ; 2. 工作區相對路徑：以 / 開頭，相對工作區根目錄
  '/example/test2',
  ; 3. 完整 URL：若 workspace.noml 中有對應映射則為本地包
  'github.com/lizongying/nolang/test2',,
]
```

> **Attention** :
> - La troisième forme est un paquet local ou non selon qu'un mappage correspondant existe dans `workspace.noml`. Si `workspace.noml` mappe `github.com/lizongying/nolang/test2` vers un chemin local, la dépendance est un paquet local (utiliser `"*"`) ; sinon, c'est un paquet distant (utiliser un numéro de version).
> - Les préfixes `./` et `../` ne sont plus autorisés dans `package.noml`. Tous les chemins locaux doivent commencer par `/` (relatif à l'espace de travail).

#### Utilisation avancée : transformer un paquet distant en paquet local

`workspace.noml` permet non seulement d'enregistrer des noms courts, mais aussi de mapper un nom de paquet distant vers un chemin local, réalisant ainsi le mode de développement « paquet distant transformé en paquet local » :

```noml
; workspace.noml
{
  test2: '/example/test2'
  'github.com/lizongying/nolang/test2': '/example/test2'
}
```

Dans ce cas, la dépendance référencée dans `package.noml` sous `github.com/lizongying/nolang/test2` est résolue comme le paquet local `/example/test2`, et la version doit être `"*"`. C'est très pratique pour développer localement un paquet distant : il suffit d'ajouter un mappage dans `workspace.noml` pour basculer une dépendance distante vers le code source local et déboguer conjointement.

#### Avertissement sur les numéros de version

Si la compilation détecte qu'un paquet local utilise un numéro de version autre que `"*"` (p. ex. `"v0.1.0"`), elle émet un avertissement invitant à utiliser `"*"`. Les paquets distants ne sont pas soumis à cette restriction.

#### Mappage récursif de l'espace de travail (chaîne de mappage inter-paquets)

Nolang prend en charge le **mappage récursif de l'espace de travail** : un paquet dépendant peut contenir son propre `workspace.noml` avec des règles de mappage, formant une chaîne de résolution inter-paquets naturelle. C'est la capacité différentiatrice centrale de Nolang — les `replace`/`patch` de Go/Cargo ne s'appliquent qu'au projet courant et ne se propagent pas dans les paquets dépendants.

**Cas d'usage :**
- Uniformisation des alias internes d'une bibliothèque de base
- Normalisation des chemins d'import
- Migration de compatibilité sur des bibliothèques multiniveaux

**Principe de fonctionnement :**

Lors de la résolution d'une clé de dépendance, le compilateur :
1. recherche la clé dans le `workspace.noml` courant
2. si un mappage est trouvé, vérifie si le répertoire cible contient son propre `workspace.noml`
3. le cas échéant, poursuit la recherche sur la même clé, niveau par niveau
4. jusqu'à ce qu'il n'y ait plus de mappage plus profond

**Exemple :**

```
workspace/                  ← 頂層工作區
├── workspace.noml         ← {"testkey": "/pkgA"}
├── pkgA/
│   ├── workspace.noml     ← {"testkey": "/pkgB"}
│   └── pkgB/
│       ├── workspace.noml ← {"testkey": "/pkgC"}
│       └── pkgC/           ← 最終目標（無 workspace.noml）
```

Lors de la résolution de `testkey` :
1. mappage du `workspace.noml` de premier niveau → `/pkgA` (relatif à l'espace de travail)
2. mappage de `pkgA/workspace.noml` → `/pkgB` (relatif à l'espace de travail de pkgA)
3. mappage de `pkgB/workspace.noml` → `/pkgC` (relatif à l'espace de travail de pkgB)
4. `pkgC` n'a pas de `workspace.noml` → résolution finale vers `pkgC`

**Détection de cycles :**

Le compilateur maintient une pile de visite de résolution, qui suit les racines d'espace de travail déjà visitées. Dès qu'un mappage cyclique est détecté (p. ex. A→B, B→A), une erreur est immédiatement signalée avec la chaîne complète :

```
Error: circular workspace mapping detected: /path/to/A → /path/to/B → /path/to/A
```

> **Attention** : cette capacité ne prend en charge que le mappage récursif naturel inter-paquets (le paquet dépendant contient son propre `workspace.noml`) ; elle ne prend pas en charge les raccourcis chaînés manuels sur un seul fichier.

### Configuration des miroirs

Configurez les adresses des miroirs dans le tableau `mirrors` de `package.noml` pour accélérer le téléchargement des paquets distants :

```noml
mirrors: [
  'https://mirror.example.com/',,
]
```

### Configuration de l'espace de travail : workspace.noml

`workspace.noml` se trouve à la racine du dépôt ; il décrit un espace de travail **mono-dépôt multi-paquets** sous forme d'un mappage « nom de paquet → chemin relatif ». Sans arguments et en présence de `workspace.noml`, `no build` compile en parallèle tous les paquets qui y sont listés ; `no run` exécute un paquet unique de l'espace de travail via son nom (p. ex. `no run foo`).

```noml
{
  foo: '/foo'
  bar: '/bar'
}
```

`no init` définit l'espace de travail : si `workspace.noml` n'existe pas, il génère un objet vide `{}`, **sans créer aucun `package.noml`** ; s'il existe déjà, il reste inchangé et n'est pas écrasé. Lors de la création ultérieure d'un paquet avec `no new <name>`, `"<name>": "./<name>"` est automatiquement enregistré dans `workspace.noml`.

#### Configuration privée locale : .workspace.noml

Outre le `workspace.noml` public, Nolang prend en charge un fichier de configuration privé local `.workspace.noml` (notez le `.` initial). Il est recommandé de l'ajouter à `.gitignore` ; il sert aux remplacements temporaires de dépendances propres à chaque développeur.

**Logique de chargement :**

1. charge d'abord la configuration publique `workspace.noml`
2. puis charge la configuration privée `.workspace.noml`
3. **clé identique** : la configuration privée écrase la configuration publique
4. **nouvelle clé** : les configurations se cumulent

```
workspace/
├── workspace.noml       ← 工程共享配置（納入版本管理）
├── .workspace.noml      ← 本機私有配置（建議加入 .gitignore）
├── foo/
└── bar/
```

**Cas d'usage :**

- Les alias et règles de redirection de dépendances communs à l'équipe s'écrivent dans `workspace.noml` et sont versionnés
- Pour un débogage local, un développeur peut pointer temporairement une dépendance vers un fork local en la remplaçant dans `.workspace.noml`
- Évite qu'un mappage temporaire vers un fork ne pollue la configuration publique du projet, conciliant collaboration à grande échelle et souplesse du développement local

**Exemple :**

```noml
; workspace.noml（公共，版本管理）
{
  foo: '/foo'
  bar: '/bar'
}

; .workspace.noml（私有，本地調試）
{
  ; 覆蓋公共配置：foo 指向本地 fork（工作區內）
  foo: '/my-fork-foo'
  ; 新增私有映射：指向工作區外部的 fork（OS 絕對路徑，唯一逃逸出口）
  'github.com/lizongying/nolang/core': '/home/lzy/code/fork/core'
  ; 新增私有映射（工作區內）
  experimental: '/experimental'
}
```

Les mappages effectifs finaux : `foo` → `/my-fork-foo` (remplacement privé), `bar` → `/bar` (conservé du public), `github.com/lizongying/nolang/core` → `/home/lzy/code/fork/core` (ajout privé, hors de l'espace de travail), `experimental` → `/experimental` (ajout privé).

### Contrainte de frontière de l'espace de travail

Nolang applique un contrôle strict de la frontière de l'espace de travail sur les références de chemins, afin de garantir la portabilité et la sécurité du projet.

#### Vue d'ensemble des règles

| Niveau de configuration | `/` relatif à l'espace de travail | Chemin absolu OS | Saut relatif `./` `../` |
| --- | --- | --- | --- |
| Clé de dépendance `package.noml` | ✅ autorisé | ❌ interdit | ❌ interdit |
| Valeur de mappage `workspace.noml` | ✅ autorisé | ✅ autorisé (porte de sortie) | ✅ autorisé (porte de sortie) |
| Valeur de mappage `.workspace.noml` | ✅ autorisé | ✅ autorisé (porte de sortie) | ✅ autorisé (porte de sortie) |
| Chemin d'import `#` dans le code source | ✅ autorisé | ❌ interdit | ❌ interdit |

#### 1. package.noml (strictement limité, interdiction de sortir de l'espace de travail)

`package.noml` est la configuration de référence standard pour la publication, la CI et la collaboration multi-développeurs ; elle est versionnée dans le dépôt.

```noml
#{fmt-skip}
dependencies: [
  ; ✅ 合法：以 / 開頭，相對於 workspace 根
  '/vendor/test',
  ; ❌ 禁止：作業系統絕對路徑 /home/xxx/...
  ; ❌ 禁止：./ ../ 相對跳轉
]
```
```

**Contraintes :**
- Les chemins locaux ne peuvent s'écrire que `/xxx`, avec comme base la racine de l'espace de travail
- Les chemins absolus du système d'exploitation ne sont pas acceptés
- `./` et `../` ne sont pas pris en charge
- Impossible de référencer directement un répertoire hors du dépôt de travail

**Objectif :** autoriser les chemins externes exposerait facilement à des situations où « la compilation réussit sur ma machine mais échoue chez les autres », et créerait un risque de sécurité lié au chargement accidentel de code externe inconnu.

#### 2. workspace.noml + .workspace.noml (la seule porte de sortie)

Les cibles de mappage prennent en charge trois types de chemins locaux :
- `/xxx` → répertoire interne à la racine de l'espace de travail (même sémantique que package.noml)
- `./xxx`, `../xxx` → résolus relativement à la racine de l'espace de travail, permettent de franchir la frontière de l'espace de travail
- chemin absolu du système d'exploitation → permet de pointer hors de l'espace de travail (forks, composants externes)

```noml
; .workspace.noml 私有配置，用於本地開發
{
  ; 工作區內部
  foo: '/vendor/foo'
  ; 相對跳轉：指向工作區外部的 fork
  'github.com/lizongying/nolang/core': '../fork/core'
  ; OS 絕對路徑：指向任意外部目錄
  'github.com/lizongying/nolang/utils': '/home/lzy/code/utils'
}
```

**Logique clé :** `package.noml` ne peut pas référencer directement du code externe ; pour monter du code source hors du dépôt, l'utilisateur doit explicitement le mapper dans la configuration de l'espace de travail. C'est comme ajouter un interrupteur explicite et visible au « chargement inter-répertoires » : toutes les dépendances externes sont gérées en un seul endroit.

#### 3. Contraintes associées au niveau des imports dans le code source

Dans le code source, il n'est pas permis d'écrire directement un import avec un chemin absolu du système d'exploitation ; toute référence à du code externe doit passer indirectement par un alias de l'espace de travail ou un identifiant de paquet distant. Les sauts relatifs `./` et `../` sont également interdits.

```no
; ✅ autorisé : chemin relatif à l'espace de travail
# /vendor/utils.helper
; ✅ autorisé : identifiant de paquet distant
# github.com/lizongying/nolang/core.process
; ✅ autorisé : bibliothèque standard
# std/fs
```

```no
#{fmt-skip}
; ❌ interdit : saut relatif
; # ./utils.helper
; # ../lib/helper.process

; ❌ interdit : chemin absolu du système d'exploitation
; # /home/user/code/core.process
```

#### Démonstration du flux de données

Scénario : la racine de l'espace de travail d'un projet métier se trouve dans `/code/project`, et l'on souhaite utiliser `/code/fork/core` externe.

❌ Cela n'est pas possible (interdit par package.noml) :

```noml
; package.noml
dependencies: [
  '/code/fork/core', ; 非法：無法逃逸工作區,
]
```

✅ Procédure standard (franchir la frontière via un mappage de l'espace de travail) :

```noml
; .workspace.noml
{
  'github.com/lizongying/nolang/core': '/code/fork/core'
}
```

```no
; import normal dans le code source
# github.com/lizongying/nolang/core
```

#### Flux de l'espace de travail

Le répertoire d'entrée de la compilation et de l'exécution est toujours le **répertoire de l'espace de travail** (celui qui contient `workspace.noml`). Le flux global est le suivant :

1. l'utilisateur exécute `no build` ou `no run <nom de paquet>` dans le répertoire de l'espace de travail
2. le compilateur lit `workspace.noml` et recherche le chemin du sous-répertoire correspondant au nom du paquet
3. il charge le `package.noml` de ce sous-répertoire (la racine du paquet) et effectue la compilation/l'exécution en prenant la racine du paquet comme référence
4. tous les chemins d'import (`use /path/to/module`) sont **résolus relativement à la racine de l'espace de travail** (voir [Convention de résolution des chemins](path-resolution.md)) : le répertoire contenant `workspace.noml` est la seule référence, indépendamment du paquet dans lequel se trouve l'import.

```
workspace/               ← 工作區目錄（workspace.noml 所在此處）
├── workspace.noml      ← 包名 → 路徑映射
├── foo/                 ← 包 foo
│   ├── package.noml        ← foo 的包根目錄
│   ├── main.no
│   └── lib.no
└── bar/                 ← 包 bar
    ├── package.noml        ← bar 的包根目錄
    └── main.no
```

> **Attention** : les chemins d'import sont basés sur la **racine de l'espace de travail** (le répertoire contenant `workspace.noml`), et non sur le répertoire `package.noml` du paquet. Si un sous-répertoire de paquet contient un `package.noml` imbriqué, `LoadPackage` remonte l'arborescence pour trouver le `package.noml` le plus proche et l'utilise comme racine du paquet, mais la résolution des imports est uniformément basée sur la racine de l'espace de travail.

---

## Cas d'utilisation

- [notools](https://github.com/lizongying/notools) — un ensemble d'utilitaires en ligne de commande Unix courants écrits en Nolang (cat, ls, grep, wc, head, tail, etc.), qui illustre la capacité réelle de Nolang dans le domaine de la programmation système.
