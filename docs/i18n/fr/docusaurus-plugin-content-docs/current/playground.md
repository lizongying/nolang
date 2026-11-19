---
sidebar_position: 5
---

# Playground

Le Nolang Playground est un environnement en ligne qui permet d'éditer, de compiler et d'exécuter des programmes Nolang directement dans le navigateur. Il compile le compilateur Nolang (`no.wasm`) et le serveur de langage (`lsp.wasm`) en WebAssembly et exécute le code utilisateur dans le bac à sable du navigateur via WASI — aucune installation d'outillage local n'est nécessaire.

Accédez-y immédiatement : [**/playground**](/playground)

## Utilisation

La page du Playground se divise en deux volets :

- **Éditeur de gauche** : un éditeur de code basé sur [CodeMirror 6](https://codemirror.net/), avec coloration syntaxique de Nolang, auto-indentation et historique.
- **Output / stderr / Diagnostics de droite** : résultats d'exécution, sortie d'erreur standard et messages de diagnostic.

L'interface suit automatiquement la langue de Docusaurus (chinois simplifié / anglais), et le volet de sortie prend en charge les thèmes clair et sombre.

### Barre d'outils

| Bouton        | Fonction                                                            |
| ------------- | ------------------------------------------------------------------- |
| **▶ Run**     | Compile le code courant en WASM et l'exécute via WASI ; stdout affiché dans le volet Output de droite. |
| **Format**    | Appelle `no fmt` via le LSP pour formater le code courant.           |
| **Examples**  | Charge un exemple intégré groupé par catégorie, en remplaçant le contenu de l'éditeur. |

À droite de la barre d'outils s'affiche l'état courant (inactif / compilation / exécution / terminé / erreur), et une description bilingue de l'exemple sélectionné apparaît sous le menu déroulant. Le dernier exemple choisi est mémorisé dans le stockage local du navigateur.

### Exemples

Le menu déroulant Examples contient **24 exemples** regroupés par thème, tous validés en CI via exactement le même chemin de compilation `--wasm-direct` que celui utilisé par le navigateur :

| Catégorie | Nombre | Contenu |
| --------- | ------ | ------- |
| Bases | 7 | Hello World, inférence de type, fonctions et récursion, Fibonacci, arithmétique et opérations bit à bit, sortie formatée, chaînes |
| Contrôle de flux | 6 | Gardes de court-circuit, filtrage par motif (match), FizzBuzz, quatre formes de boucles sur intervalle, while conditionnel, break / continue |
| Structures de données | 4 | Tableaux de taille fixe, tranches dynamiques, structures, méthodes |
| Types scalaires | 4 | char, bool, f64, byte |
| Bibliothèque standard | 2 | math.max / math.min / sqrt, rand.rand |
| Gestion des erreurs | 1 | option et nil avec match à trois branches |

Chaque exemple est accompagné d'une chaîne de sortie attendue stable ; la CI compile chaque exemple, l'exécute avec `wasmtime` et vérifie que cette chaîne apparaît dans stdout. Ainsi, les exemples ne peuvent pas devenir silencieusement obsolètes quand le langage ou le backend Direct-WASM évolue.

Après la sélection d'un exemple, le Playground efface l'Output, stderr et Diagnostics courants, puis remplace l'éditeur par le contenu de cet exemple.

### Diagnostics

Lorsque le LSP détecte une erreur de syntaxe ou de type, celle-ci apparaît dans la zone Diagnostics en bas à droite. Un clic sur un élément de diagnostic navigue vers la ligne et la colonne correspondantes. Les messages `file:line:col` présents dans stderr du compilateur sont en outre analysés en marqueurs **Run Errors**, les TraceIDs finaux et les préfixes de gravité localisés étant automatiquement supprimés.

### Erreurs et dépassements de délai

Auparavant, les échecs de chargement WASM n'allaient que dans `console.warn`, laissant l'utilisateur avec un bouton Run sans effet. Le comportement actuel est le suivant :

- Si `no.wasm` ou `lsp.wasm` ne peut être téléchargé ou compilé, une bannière d'erreur refermable apparaît en haut de la page, en indiquant la ressource et la cause.
- Un échec d'initialisation du serveur de langage affiche également une bannière ; l'éditeur reste utilisable, mais la complétion, le survol et le formatage sont indisponibles.
- Chaque exécution est chronométrée par un chien de garde sur le thread principal (8 secondes par défaut). `wasi.start()` est un appel synchrone dans le worker, donc une boucle infinie empêche tout minuteur du worker de se déclencher — la seule façon de l'arrêter réellement est de terminer le worker depuis le thread principal. Après un dépassement de délai, le worker est terminé, recréé paresseusement à la prochaine exécution, et le préchauffage WASM redémarre immédiatement.
- Les clics répétés sur Run sont bloqués par une protection contre la concurrence, et les anti-rebonds d'édition en attente sont annulés lors d'un changement d'exemple ou d'un formatage, afin qu'une saisie obsolète ne puisse écraser un contenu récent.

`no.wasm` et `lsp.wasm` sont téléchargés et compilés en flux pendant les périodes d'inactivité du navigateur ; le premier clic sur Run n'a donc plus à attendre le téléchargement et la compilation complets des 27 Mo.

## Limitations

Comme le Playground s'exécute dans un bac à sable WASM du navigateur, certaines fonctionnalités de Nolang sont indisponibles :

### Pas de FFI

Le navigateur ne peut pas lier de bibliothèques C natives ; les déclarations FFI `#{c}`, `#{cpp}`, `#{rust}` etc. ne peuvent donc pas être utilisées dans le Playground. Le code qui implique de la FFI (comme les pilotes `sqlite` ou `mysql`) ne peut pas s'exécuter.

### Pas de fork / exec / pipe

WASI preview1 ne fournit pas d'API de gestion des processus ; la bibliothèque standard `process` (`process.start`, `process.wait`, etc.) n'est donc pas disponible dans le Playground.

### Sockets réseau indisponibles

WASI preview1 ne fournit pas d'API de sockets ; les bibliothèques standard suivantes sont donc indisponibles dans le Playground :

- `net` (écouteur / connexion TCP)
- `http` / `http2` / `http3` (client HTTP)
- `ws` (WebSocket)
- `tls` (connexion TLS)
- `quic`, `sse`, `dns`

**Contrainte plus stricte sur `no` / `lsp` eux-mêmes** : les artefacts wasm ne peuvent même pas *importer* une fonction socket. Le navigateur les instancie via `@wasmer/wasi`, qui n'implémente pas `sock_accept` / `sock_shutdown` ; dès qu'un artefact lie `net` / `net/http` et ajoute ces deux imports `wasi_snapshot_preview1`, `WebAssembly.Instance` lève directement :

```
Failed to instantiate WASI: RuntimeError: JsValue(LinkError: WebAssembly.Instance():
Import #28 "wasi_snapshot_preview1" "sock_accept": function import requires a callable)
```

Le programme n'atteint alors jamais `_start` — le service de langage signale un « échec d'initialisation » (plus de complétion, survol ni formatage), et le bouton d'exécution échoue de la même manière. C'est pourquoi tout code réseau qui n'a de sens que dans un environnement natif est placé dans des fichiers `!wasip1`, laissant côté wasip1 uniquement des stubs d'erreur : `cmd/lsp/pprof_wasip1.go`, `cmd/no/serve_esm_wasip1.go`, `package/download_net_wasip1.go`, `package/release_net_wasip1.go`.
`bun scripts/wasm_imports_check.ts` (`make wasm-imports-check`) protège cette invariant à l'aide de la table d'imports du moteur lui-même.

### `poll_oneoff` fourni par le front-end

Une table d'imports propre signifie seulement que les fonctions existent, pas qu'elles fonctionnent. Le corps de `poll_oneoff` de `@wasmer/wasi` est le `unimplemented!()` de Rust : dès qu'il est appelé — même avec zéro abonnement — il exécute `unreachable` et fait trapper toute l'instance, avec une erreur de la forme :

```
Error while running start function: RuntimeError: JsValue(RuntimeError: unreachable)
```

Le runtime de Go transforme aussi bien `time.Sleep` que `netpoll(delay)` en un unique abonnement à l'horloge monotone ; **chaque programme Go est donc concerné** — `no fmt` / `no vet` / `no build` mouraient au premier endormissement, alors que la vérification des imports restait verte.

Le correctif se trouve dans `docs/src/playground/wasm-loader.ts` : `instantiateWasi()` construit lui-même l'objet d'imports à partir de `wasi.getImports()` et remplace `poll_oneoff` par une implémentation JS (abonnements d'horloge → on dort jusqu'à l'échéance la plus proche puis on signale l'événement d'horloge ; abonnements uniquement sur des fd → signal immédiat de disponibilité, pour éviter un blocage indéfini). Attention : on **ne peut pas** passer la substitution à `wasi.instantiate(module, overrides)` — les fonctions propres du glue ont la priorité, la substitution est silencieusement ignorée et le programme trappe quand même. Du fait du passage à un objet d'imports construit ici, `wasi.start(instance)` doit recevoir explicitement l'instance.

### Temps de compilation du premier lancement

Run utilise `no build --wasm-direct` pour produire le wasm. En mesure, le hello world prend désormais environ **0,5 s** dans le wasm (la commande native `no` équivalente : environ 0,6 s ; les deux après le précuisson de l'index std de 2026-11, auparavant respectivement environ 55 s et 11 s).

Le goulet d'étranglement n'était pas le codegen, mais le fait de **reparser tout le std deux fois à chaque démarrage à froid** : `ValidateUndefinedVars` de `validateProgram` collecte les symboles exportés des 119 modules std afin de reconnaître les appels qualifiés par module comme `math.max(...)` ; `CollectStdUnionAliases` reparsait indépendamment le même lot de modules pour trouver les alias d'union. Désormais `genstdsig` cuit ensemble « les symboles exportés de chaque module » et « quels modules ont des alias d'union » dans `stdsig_gen.go`, et le runtime consulte directement la table ; deux gardes supplémentaires (`hir.TestModuleExportsMatchChecker` compare module par module et champ par champ le résultat HIR fraîchement calculé, `TestUnionAliasNarrowingMatchesFullScan` compare l'analyse étroite à l'analyse complète) font échouer les tests en cas de table périmée plutôt que de modifier silencieusement la sémantique.

Le chien de garde conserve ses deux budgets (« chargement + compilation 180 s / programme utilisateur 5 s ») : le premier dispose désormais d'une très large marge, mais est maintenu pour absorber les démarrages à froid, les grands programmes et les longues traînes des appareils lents.

### Système de fichiers virtuel

WASI preview1 ne fournit que les descripteurs de fichiers stdin / stdout / stderr ; il n'existe pas de vrai système de fichiers. Les bibliothèques standard qui impliquent des fichiers ou l'environnement, telles que `fs`, `path`, `os.get-env`, `os.set-env`, peuvent donc ne pas fonctionner comme prévu dans le Playground.

### Limite de mémoire

Le WASM dans le navigateur est généralement contraint par un espace d'adressage 32 bits ; le plafond de mémoire utilisable est d'environ **2 Go à 4 Go** (selon le navigateur et le système d'exploitation). Les programmes nécessitant beaucoup de mémoire (récursion profonde, grands tableaux) peuvent échouer par manque de mémoire.

## Compatibilité des navigateurs

Le Playground nécessite un navigateur moderne prenant en charge WebAssembly. Les versions suivantes ou ultérieures sont recommandées :

| Navigateur | Version minimale |
| ---------- | ---------------- |
| Chrome     | 100+             |
| Firefox    | 100+             |
| Safari     | 16+              |
| Edge       | 100+             |

Si le navigateur ne prend pas en charge WebAssembly ou si JavaScript est désactivé, le Playground ne pourra charger l'éditeur ni exécuter de code.

## Développement et validation

Après une modification du Playground, exécutez les vérifications suivantes sur un arbre de compilation propre :

```bash
make playground-demos                       # 編譯 + 在行程內用 playground 自己的 WASI loader 執行全部範例
make playground-demos DEMO_RUN=wasmtime     # 改用外部 wasmtime 執行（交叉比對用）
make playground-demos DEMO_RUN=none         # 只編譯、不驗輸出（不可當發佈門檻）
make wasm-imports-check                     # 重建兩個 wasm 並驗證導入表無 socket 函式
make wasm-runtime-smoke                     # 用 worker 的同一條載入路徑實跑 version/fmt/vet
RUN_WASM_LSP=1 RUN_WASM_BUILD=1 make wasm-runtime-smoke   # 追加語言服務與完整編譯+執行
bun scripts/playground_ui_check.ts          # 診斷解析、看門狗、worker 恢復、i18n
make playground-smoke                       # dev server 與 WASM 路由 HTTP 200
cd docs && bun run typecheck                # TypeScript 型別檢查
```

Le compilateur de `playground-demos` est le `no` **natif**, mais les artefacts passent par le chemin `--wasm-direct -target wasm32-wasi` du navigateur et sont exécutés via `instantiateWasi()` de `wasi-glue` + `wasm-loader` — exactement le même code que `runner.worker`. Il n'est donc pas nécessaire d'installer wasmtime : la CI l'exécute directement. Il vérifie trois choses à la fois : le snippet se compile, l'artefact passe la validation WebAssembly (l'erreur de pile vide `local.set` d'une valeur de filtrage par champ apparaît ici), et la sortie imprimée correspond à `expect` (un backend Direct-WASM qui renvoie silencieusement `0` pour `?=` apparaît ici aussi).

La seule chose qu'il ne peut pas voir est la table d'imports de `no.wasm` / `lsp.wasm` eux-mêmes (ces artefacts ne proviennent pas de snippets) : `wasm-imports-check` comble cette lacune — quand `@wasmer/wasi` n'a pas `sock_*`, `WebAssembly.Instance` lève une `LinkError` et tout le Playground (service de langage inclus) ne parvient pas à démarrer. Le worker ajoute une couche de sécurité : l'`instantiateWasi()` de `wasm-loader.ts` remplace toute fonction WASI absente de `@wasmer/wasi` par un stub renvoyant `ENOSYS` / `NOTSUP`, afin qu'un import inattendu dégénère en simple appel système échoué plutôt que de faire tomber tout le service.

`wasm-imports-check` ne peut pas non plus détecter « la fonction existe mais son corps trappe » (la classe `poll_oneoff` ci-dessus) ; `make wasm-runtime-smoke` comble la dernière lacune : il exécute `no version` / `no fmt` / `no vet` via le vrai chemin `instantiateWasi()` et peut en plus vérifier le service de langage et une compilation + exécution complètes.

`docs/static/wasm/*.wasm` est exclu par `.gitignore` et les artefacts locaux ne doivent jamais être commités. `no.wasm` et `lsp.wasm` sont reconstruits par la CI à partir de la même arborescence source et du même `StdSigKey`, afin que le compilateur dans le navigateur, son blob AST std intégré et le backend Direct-WASM proviennent d'une seule et même compilation. Un désalignement de version entre ces trois éléments était la cause habituelle des anciens traps `unreachable` et erreurs de validation WASM.

La CI (`pr-verify` dans `.github/workflows/build.yml` et la compilation du site dans `static.yml`) exécute successivement les tests Go, la vérification des types, les contrôles UI et la validation des exemples, puis seulement reconstruit le WASM à partir d'un arbre propre et déploie le site.
