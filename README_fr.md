# Nolang

[繁體中文](README.md) · [简体中文](README_zh-CN.md) · [한국어](README_ko.md) · [日本語](README_ja.md) · [English](README_en.md) · [Русский](README_ru.md) · [Français](README_fr.md) · [Español](README_es.md) · [Deutsch](README_de.md) · [Português](README_pt.md)

Nolang est un langage de programmation système sûr en mémoire, très performant et d'une syntaxe minimaliste.

## Fonctionnalités principales

- **Sécurité mémoire** : gestion automatique de la mémoire
- **Performances élevées** : optimisations à la compilation, sans GC
- **Syntaxe minimaliste** : une syntaxe plus simple
- **Guidé par l'intention** : respecte pleinement l'intention du développeur, avec une sémantique conforme à l'intuition humaine
- **Ne plante jamais** : élimine les chemins de plantage implicites

## Installation

Installation en une commande (macOS / Linux / Windows Git Bash) : le script télécharge la dernière version de `no` dans `$HOME/.local/bin` :

```bash
curl -fsSL https://raw.githubusercontent.com/lizongying/nolang/main/install.sh | sh
```

Par défaut, c'est la version **LLVM intégré** qui est installée (`no-llvm_*`, aucun LLVM / opt / llc séparé n'est nécessaire). Pour la version Go pur, plus légère, utilisez `NO_FLAVOR=plain` avec `no build -cc zig` :

```bash
curl -fsSL https://raw.githubusercontent.com/lizongying/nolang/main/install.sh | NO_FLAVOR=plain sh
```

Pour épingler une version ou un répertoire personnalisé (les variables doivent atteindre `sh`, enregistrez d'abord le script) :

```bash
curl -fsSL https://raw.githubusercontent.com/lizongying/nolang/main/install.sh -o install.sh
NO_VERSION=0.4.28 NO_INSTALL_DIR="$HOME/bin" sh install.sh
```

| Variable           | Valeur par défaut   | Description                                                              |
| ------------------ | ------------------- | ------------------------------------------------------------------------ |
| `NO_VERSION`       | `latest`            | Version, par ex. `0.4.28` ou `v0.4.28`                                  |
| `NO_FLAVOR`        | `embedded`          | `embedded` : LLVM intégré ; `plain` : Go pur (LLVM système requis)       |
| `NO_INSTALL_DIR`   | `$HOME/.local/bin`  | Répertoire d'installation                                                |
| `NO_REPO`          | `lizongying/nolang` | Dépôt source                                                             |

Vérifiez avec `no version`. Si `$HOME/.local/bin` n'est pas dans le `PATH`, ajoutez-le :

```bash
export PATH="$HOME/.local/bin:$PATH"
```

> La compilation de programmes `.no` nécessite également un éditeur de liens C : `no build -cc zig` est recommandé (zig fournit clang / lld / libc), ou installez clang vous-même.

## Usage

doc

[docs](https://lizongying.github.io/nolang/)

Extension VSCode

[vscode](https://marketplace.visualstudio.com/items?itemName=lizongying.vscode-nolang)

CLI Nolang

[nolang](https://github.com/lizongying/nolang/releases/latest)

nohub — index des paquets (paquets communautaires)

[nohub](https://lizongying.github.io/nohub/)

### Recherche de paquets communautaires (nohub)

`no` lit directement l’index de paquets de nohub et propose trois commandes en lecture seule :

```bash
no search <query>    # rechercher par nom / mot-clé / catégorie
no info <pkg>        # afficher les métadonnées et les commandes d’installation/import
no docs <pkg>        # afficher l’URL de documentation (docs → homepage → repository)
```

L’index public est utilisé par défaut. Pour combiner plusieurs instances nohub, configurez une liste de sources dans `~/no/sources.noml` (l’ordre du tableau définit la priorité) :

```
{
  sources: [
    'https://lizongying.github.io/nohub',
  ]
}
```

`NOHUB_SOURCES` (séparé par des virgules) la remplace temporairement. Les trois commandes acceptent `-source <name>` ; `no search` affiche une colonne `SOURCE`, et un paquet de même nom et même version est résolu par la première source. Une source inaccessible ne produit qu’un avertissement. `no` ne récupère que le `index.noml` de chaque source (`<base>/index.noml` — le même fichier chargé en premier par la page de recherche, une entrée par paquet à sa dernière version) : ni tarball ni `data.js`, avec un mappage des champs identique à `web/app.js`. Les mêmes données sont consultables sur la [page de recherche nohub](https://lizongying.github.io/nohub/).


### Commandes CLI

| Commande                                                     | Description                                                                                |
| ------------------------------------------------------------ | ------------------------------------------------------------------------------------------ |
| `no init`                                                    | Définit l'espace de travail (génère workspace.jsonc, sans package.jsonc)                   |
| `no new <name>`                                              | Crée un nouveau paquet dans l'espace de travail (sous-répertoire + package.jsonc, et l'enregistre dans workspace.jsonc) |
| `no fmt [-w] [-d] <file\|dir>`                               | Formate le code source                                                                     |
| `no build [-o <file>] [-cc <s>] [-target <s>] [<file\|dir>]` | Compile (produit un exécutable)                                                             |
| `no run [-cc <s>] [-target <s>] [<package\|dir\|file>]`        | Compile et exécute (nom de paquet/répertoire/fichier)                                      |
| `no test [-cc <s>] [-target <s>] [<file\|dir>]`              | Exécute les tests                                                                          |
| `no add <pkg>`                                               | Ajoute une dépendance                                                                      |
| `no remove <pkg>`                                            | Supprime une dépendance                                                                    |
| `no update <pkg>`                                            | Met à jour une dépendance                                                                  |
| `no update-all`                                              | Met à jour toutes les dépendances                                                          |
| `no list`                                                    | Liste les dépendances                                                                      |
| `no search <query>`                                            | Rechercher des paquets communautaires par nom / mot-clé / catégorie |
| `no info <pkg>`                                                | Afficher les métadonnées et les commandes d’installation/import |
| `no docs <pkg>`                                                | Afficher l’URL de documentation du paquet |
| `no sync`                                                    | Synchronise les dépendances                                                                |
| `no install [-u] [<tool>[:<version>]@<host>/<owner>/<repo>]` | Installe un binaire (~/no/bin/ + lien symbolique)                                          |
| `no uninstall <name>`                                        | Supprime le binaire et le lien symbolique                                                  |
| `no pub --token <token> [--registry <url>]`                  | Publie sur le registre                                                                     |

```bash
# compilation (à partir de main.no par défaut)
no build                    # compile le répertoire courant
no build main.no            # compile le fichier spécifié
no build -o output          # spécifie le chemin de sortie
no build -cc zig            # utilise le compilateur Zig
no build -target x86_64-linux-gnu  # compilation croisée (spécifie la plateforme cible)

# exécution (compilation + lancement)
no run                    # compile et exécute main.no du répertoire courant
no run foo                # exécute le paquet nommé foo dans l'espace de travail
no run ./foo              # exécute main.no du répertoire ./foo
no run -cc zig
no run -target aarch64-macos-gnu

# tests (compilation + exécution)
no test                   # teste tous les fichiers .no du répertoire `test`
no test test/my-test.no        # exécute un seul fichier de test
no test -cc zig
no test -target x86_64-windows-gnu
```

### Créer un nouveau projet

```shell
no new test1

cd test1

no run
```

### Hello World

Après avoir créé le projet, modifiez `main.no` :

```no
print('Hello, World!')
```

Exécution :

```bash
no run
```

Sortie :

```
Hello, World!
```

### Règles d'entrée

- **main.no** — point d'entrée du programme
- **lib.no** — point d'entrée de la bibliothèque, exporte des fonctions
- **tous les fichiers .no du répertoire tests/** — contiennent des assertions de test

### Notes sur les tests

- Les fichiers de test sont tous placés dans le répertoire tests/
- Chaque fichier de test est compilé indépendamment
- Si un test échoue, un code de sortie non nul est retourné

```bash
no vet src/std                     # vérifie la bibliothèque standard avec Nolang vet
```

## Compilation depuis les sources

Nolang doit être compilé avec `make` :

```bash
make            # compile toutes les cibles (bin/no et vscode-nolang/server/lsp)
make no         # compile bin/no
make lsp        # compile le serveur LSP
make package    # compile le LSP et empaquette l'extension VSCode
make no-wasm    # transpile no en WebAssembly
make lsp-wasm   # transpile le LSP en WebAssembly
make playground # compile no.wasm + lsp.wasm + le site Docusaurus
make clean      # nettoie les artefacts de compilation
make help       # affiche toutes les cibles de compilation
```

> ⚠️ **La compilation de Nolang doit utiliser `make` ; ne jamais compiler directement avec `cd src && go build`.**
>
> `make` exécute automatiquement `make gen` avant la compilation, pour régénérer `src/checker/stdsig_gen.go`
> — une table de signatures de la bibliothèque standard **intégrée au binaire** (`funcSigs` / `methodSigs` /
> `funcParams` / `methodParams` / `structFields` / `aliases` / `structMod` /
> `enumVariants` / `embeddedStdModules`). Grâce à elle, chaque `no build` ignore la passe PASS1
> (« analyse complète de tout std »), ce qui accélère nettement le démarrage.
>
> Si vous avez modifié `src/std` et lancez `go build` **sans** avoir d'abord exécuté `make gen`, le compilateur
> détectera une incohérence entre la table de signatures intégrée et le contenu de `src/std`, et
> **refusera la compilation avec une erreur** (en vous invitant à exécuter
> `make no`). C'est un choix délibéré : par le passé, une dégradation silencieuse vers l'analyse complète
> ralentissait chaque compilation et rendait le problème difficile à détecter. Après avoir modifié
> `src/std`, exécutez toujours `make no` (qui lance d'abord `make gen` puis compile).
>
> Utilisez également `make test` pour exécuter les tests : **un `go test` direct échouera car la clé de signature n'est pas injectée (`StdSigKey` est vide)**. `make test` injecte la même clé lors de la compilation du binaire de test, ce qui lui permet d'emprunter le chemin rapide de la table de signatures intégrée.

---

## Glossaire (glossary)

`glossary/` est une **base de connaissances de Nolang structurée en entrées** : chaque terme dispose d'une fiche, avec son type, sa définition, sa syntaxe,
ses exemples, ses références associées et un résumé en une phrase. Au total **244 entrées / 10 domaines sémantiques** (types, syntaxe, annotations, mémoire,
erreurs, modules, internals du compilateur, chaîne d'outils, tous les modules std, pièges connus), les données elles-mêmes étant au format `.noml`.

L'interface est écrite en Nolang et propose à la fois une CLI et un MCP (stdio / HTTP), consultables par les humains comme par l'IA :

```bash
no build glossary                        # produit : glossary/dist/glossary

./glossary/dist/glossary get option      # récupère l'intégralité d'une entrée
./glossary/dist/glossary search 三態      # recherche par mot-clé
./glossary/dist/glossary list --domain=traps   # liste des pièges connus
./glossary/dist/glossary check           # contrôle de cohérence (pour la CI)

./glossary/dist/glossary mcp             # serveur MCP (stdio)
./glossary/dist/glossary mcp --http=127.0.0.1:8765
```

La sortie peut être choisie avec `--format=text|md|json`. Voir [`glossary/README.md`](glossary/README.md) pour les instructions d'intégration et le format des données.

Vous pouvez aussi consulter le paquet glossary sur nohub : [glossary on nohub](https://lizongying.github.io/nohub/?package=glossary:latest@github.com/lizongying/nolang)

---

## Projets liés

- [nohub — recherche de l'index des paquets communautaires de l'écosystème Nolang](https://lizongying.github.io/nohub/)

  Recherche des paquets communautaires par nom, mot-clé ou catégorie ; consultation des informations sur le paquet, des instantanés de version et des points d'entrée vers le code source. L'index est généré par `web/build.mjs` à partir des instantanés de `packages/`.

- [noui — une boîte à outils d'interface réécrivant l'esprit de GPUI en Nolang (paquet nohub)](https://lizongying.github.io/nohub/?package=noui:latest@github.com/lizongying/nolang)

- [Benchmarks Game — rapport de comparaison des performances Nolang vs C vs Rust](https://github.com/lizongying/no-benchmarks/results/report.md)

- [notools — ensemble d'utilitaires en ligne de commande Unix implémentés avec Nolang](https://github.com/lizongying/notools)

  Étude de cas Nolang : un ensemble d'utilitaires en ligne de commande Unix courants implémentés en Nolang, couvrant des outils classiques tels que cat, ls, grep, wc, head, tail, etc., démontrant la capacité réelle de Nolang dans le domaine de la programmation système.

---

## TODO

## Licence

Apache-2.0
