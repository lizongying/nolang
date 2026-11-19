---
sidebar_position: 99
---

# Convention de résolution des chemins : relatif à la racine de l'espace de travail (Workspace-Root-Relative)

## Convention (adoptée)

Tous les **chemins d'intégration (embed)** et les **chemins d'import** (directives `use` / `#`, ressources embed) sont résolus en un **chemin absolu canonique relatif à la racine de l'espace de travail (workspace root)** — ils ne sont **plus résolus relativement au « répertoire du fichier source courant »**.

- La « racine de l'espace de travail » = le répertoire contenant `workspace.noml`, c'est-à-dire `pkg.WorkspaceRoot()`.
  Attention : le répertoire contenant `package.noml` est la **racine du paquet** (`pkg.RootDir`) ; les deux ne sont pas au même niveau — la racine de l'espace de travail est au-dessus (elle contient `workspace.noml` et peut contenir plusieurs paquets), tandis que la racine du paquet est le répertoire d'un **paquet** précis (contenant `package.noml`), lequel renferme plusieurs **modules** (fichiers source `.no`). Ne pas confondre.
- Après la résolution, une canonicalisation est appliquée (`filepath.Clean`, suppression de `.`/`..`, normalisation des séparateurs), produisant un **chemin canonique globalement unique** qui sert d'identifiant unique pour le chargement des modules, les clés de cache et l'appartenance AST/sémantique.
- Ce chemin canonique est la **composante de chemin** des clés de cache de `lexer.tokenCache` et `checker.parseProgramFileCache`. La clé de cache est en réalité une clé composite `(chemin canonique, hachage du contenu)` (`cache.Key`, voir `src/cache/lru.go`) : même chemin mais contenu modifié → hachage différent → invalidation automatique ; les tokens/AST périmés ne sont plus renvoyés. Le cache est un LRU à capacité limitée, aussi les processus de longue durée (LSP / fmt) ne fuient-ils pas de mémoire (point ③ de l'audit initial de l'architecture en deux passes).

## Pourquoi

1. **Éliminer les collisions de clés de cache à la racine**. L'ancienne clé de cache des tokens/AST était la « chaîne de chemin » ; lorsque différents fichiers source étaient importés/intégrés avec la même chaîne relative (par ex. `utils/no` ou le nom d'intégration `data.json`), ils atteignaient la même entrée de cache → contamination croisée, tokens/AST erronés. Avec le passage aux chemins absolus canoniques relatifs à la racine de l'espace de travail, **chaque fichier logique possède une clé unique ; un même fichier correspond toujours à la même clé**, et les collisions disparaissent par construction (point ④ de l'audit initial de l'architecture en deux passes).
2. **Simplification et robustesse considérables de la logique du compilateur**. La résolution des chemins passe d'un « relatif au fichier courant éparpillé dans le parser / transpiler / checker » à une **étape de canonicalisation unique** ; les références anticipées, le chargement des modules, les clés de cache et les chemins dans les diagnostics reposent tous sur la même base, sans plus dépendre de l'emplacement du fichier importateur.
3. **Préparer la mise en cache inter-fichiers / parallèle**. Les chemins absolus canoniques rendent les clés de cache stables et comparables globalement, ce qui facilitera la réutilisation par deep-clone (évitant les `ParseProgram` redondants) et les optimisations de résolution paresseuse.

## Limites d'applicabilité

- **Chemins absolus** (modules locaux commençant par `/`) : déjà relatifs à la racine de l'espace de travail, inchangés.
- **Modules `std/`** : proviennent du `StdFS` intégré, avec pour clé `std/<rel>.no` ; inchangés, jamais confondus avec les chemins locaux.
- **Ressources intégrées (embed)** : quelle que soit le nom relatif utilisé pour les référencer, elles sont normalisées en interne en une clé d'embed canonique relative à la racine de l'espace de travail.

## Implémenté (2026-08-02)

Les helpers unifiés se trouvent dans `src/package/paths.go` (paquet `pkg`, bibliothèque standard pure, sans dépendance circulaire) :

- `FindWorkspaceRoot(start)` : remonte depuis `start` pour trouver le répertoire contenant `workspace.noml`.
- `FindPackageRoot(start)` : remonte depuis `start` pour trouver le répertoire contenant `package.noml` (solution de repli en l'absence d'espace de travail).
- `ResolveToWorkspaceRoot(wsRoot, rel)` : normalise tout chemin d'import / d'embed brut en un chemin absolu relatif à la racine de l'espace de travail (suppression du `/` initial ; les chemins absolus sont renvoyés tels quels ; repli sur le répertoire courant lorsque `wsRoot` est vide).
- `ResolveEmbedBase(sourcePath)` : base de résolution des chemins d'embed relatifs — racine de l'espace de travail prioritaire, repli sur la racine du paquet / le répertoire du fichier source.

Points d'entrée convergés :

- `src/build/transpiler.go` `resolveUse` : la branche A (préfixe `/`) et la branche E (alias) sont unifiées vers `pkg.ResolveToWorkspaceRoot(t.workspaceRoot(), path)`, avec une nouvelle méthode `t.workspaceRoot()` (préférant `pkg.WorkspaceRoot()`, sinon remontant depuis `sourcePath` pour trouver `workspace.noml`). L'ancienne logique redondante `if t.pkg != nil { baseDir = pkg.RootDir; if wsRoot... }` a été supprimée ; le repli relatif au cwd passe désormais par `FindWorkspaceRoot`.
- `src/build/transpiler.go` `processEmbeds` : les chemins d'embed relatifs passent désormais par `pkg.ResolveEmbedBase(sourcePath)` (racine de l'espace de travail prioritaire).
- `src/checker/checker.go` `ValidateEmbedAnnotations` : les chemins de validation des embeds passent également par `pkg.ResolveEmbedBase`.

Les clés de cache (`tokenCache` / `parseProgramFileCache`) obtiennent le chemin absolu canonique relatif à la racine de l'espace de travail comme composante de chemin via `resolveFile(filePath)` ; combiné au hachage du contenu du fichier source en une clé composite (`cache.Key`), les collisions disparaissent par construction, les modifications d'un même chemin provoquent une invalidation automatique, et le LRU à capacité limitée empêche les fuites de mémoire.

Les modules intégrés `std/` et `js/` utilisent toujours `StdFS`/`JsFS` (avec pour clé `std/<rel>.no` / `js/<rel>.no`), sans changement ; `processEmbeds` ne s'applique qu'au programme principal (code source utilisateur) — les embeds propres aux modules std restent relatifs à leur propre emplacement.

> Statut : **Implémenté (2026-08-02)**. Le nouveau code doit utiliser directement `pkg.ResolveToWorkspaceRoot` / `pkg.ResolveEmbedBase` ; n'utilisez **pas** le « répertoire du fichier courant » (`filepath.Dir(t.sourcePath)` etc.) pour une résolution relative.

## Glossaire (unifié, 2026-08-02)

- **workspace (espace de travail)** — correspond généralement à un dépôt (repo). Le répertoire racine contient `workspace.noml` ; il peut contenir plusieurs paquets. La racine de l'espace de travail est l'unique base de résolution de tous les chemins d'embed/import.
- **package (paquet)** — une grande unité de compilation : une bibliothèque ou un exécutable. Son répertoire racine contient `package.noml` (la **racine du paquet**). Il déclare les dépendances, le backend d'émission, etc. Un paquet contient plusieurs modules.
- **module** — généralement un fichier source (`.no`). Convention actuelle : **un fichier = un module**. Charger un module = charger un fichier `.no`.

Hiérarchie des trois : **espace de travail ⊃ paquet (un ou plusieurs) ⊃ module (un ou plusieurs)**. N'utilisez plus « module » pour désigner un « paquet » (unité de compilation) — c'est le rôle de « package ».
