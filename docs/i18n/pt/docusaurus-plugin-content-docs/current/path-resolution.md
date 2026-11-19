---
sidebar_position: 99
---

# Convenção de Resolução de Caminhos: Relativa à Raiz da Área de Trabalho (Workspace-Root-Relative)

## Convenção (Adotada)

Todos os **caminhos de embed** e **caminhos de importação** (diretivas `use` / `#`, recursos embed) são resolvidos como
**caminhos absolutos canónicos relativos à raiz da área de trabalho (workspace root)** — **deixando de ser resolvidos relativamente ao "diretório do ficheiro de origem atual"**.

- A "raiz da área de trabalho" = o diretório que contém `workspace.noml`, ou seja, `pkg.WorkspaceRoot()`.
  Atenção: o diretório que contém `package.noml` é a **raiz do pacote** (`pkg.RootDir`); não são o mesmo nível — a raiz da área de trabalho está acima (contém `workspace.noml`, pode conter vários pacotes), enquanto a raiz do pacote é o diretório de um **pacote** específico (contém `package.noml`), que por sua vez contém vários **módulos** (ficheiros de origem `.no`). Não confundir.
- Após a resolução, aplica-se a canonicalização (`filepath.Clean`, remoção de `.`/`..`, normalização de separadores), obtendo um **caminho canónico globalmente único** que serve como identificador exclusivo para o carregamento de módulos, chaves de cache e propriedade AST/semântica.
- Esse caminho canónico é o **componente de caminho** das chaves de cache de `lexer.tokenCache` e `checker.parseProgramFileCache`. A chave de cache é, na verdade, uma chave composta `(caminho canónico, hash do conteúdo)` (`cache.Key`, ver `src/cache/lru.go`): mesmo caminho com conteúdo alterado → hash diferente → invalidação automática; deixam de ser devolvidos tokens/AST desatualizados. Além disso, o cache é uma LRU com limite de capacidade, pelo que processos de longa duração (LSP / fmt) não sofrem fugas de memória (item ③ da auditoria de arquitetura original em duas passagens).

## Porquê

1. **Eliminar colisões de chaves de cache pela raiz**. A antiga chave de cache de tokens/AST era a "string do caminho"; quando diferentes ficheiros de origem importavam/incorporavam com a mesma string relativa (por exemplo `utils/no` ou o nome de embed `data.json`), atingiam a mesma entrada de cache → contaminação cruzada, tokens/AST errados. Após a mudança para caminhos absolutos canónicos relativos à raiz da área de trabalho, **cada ficheiro lógico tem uma chave única; o mesmo ficheiro corresponde sempre à mesma chave**, e as colisões desaparecem por construção (item ④ da auditoria de arquitetura original em duas passagens).
2. **Simplificação e robustez significativas da lógica do compilador**. A resolução de caminhos converge de "resolução relativa ao diretório do ficheiro atual espalhada por parser / transpiler / checker" para um **único passo de canonicalização**; referências antecipadas, carregamento de módulos, chaves de cache e caminhos em diagnósticos partilham todos a mesma base, deixando de depender da localização do ficheiro importador.
3. **Preparar o caminho para cache entre ficheiros / compilações paralelas**. Os caminhos absolutos canónicos tornam as chaves de cache estáveis e comparáveis globalmente, facilitando reutilização futura por deep-clone (evitando `ParseProgram` duplicados) e otimizações de resolução preguiçosa.

## Limites de aplicabilidade

- **Caminhos absolutos** (módulos locais que começam com `/`): já relativos à raiz da área de trabalho, permanecem inalterados.
- **Módulos `std/`**: provêm do `StdFS` incorporado, com chave `std/<rel>.no`; permanecem inalterados e nunca são confundidos com caminhos locais.
- **Recursos embed**: independentemente do nome relativo usado para os referenciar, são normalizados internamente para uma chave embed canónica relativa à raiz da área de trabalho.

## Implementado (2026-08-02)

Os helpers unificados encontram-se em `src/package/paths.go` (pacote `pkg`, apenas biblioteca padrão, sem dependências circulares):

- `FindWorkspaceRoot(start)`: procura para cima a partir de `start` para encontrar o diretório que contém `workspace.noml`.
- `FindPackageRoot(start)`: procura para cima a partir de `start` para encontrar o diretório que contém `package.noml` (alternativa quando não existe workspace).
- `ResolveToWorkspaceRoot(wsRoot, rel)`: normaliza qualquer caminho de importação / embed num caminho absoluto relativo à raiz da área de trabalho (remove o `/` inicial; caminhos absolutos são devolvidos tal qual; recorre ao diretório atual quando `wsRoot` está vazio).
- `ResolveEmbedBase(sourcePath)`: base de resolução para caminhos de embed relativos — raiz da área de trabalho preferencialmente, com alternativa para raiz do pacote / diretório do ficheiro de origem.

Pontos de entrada convergidos:

- `src/build/transpiler.go` `resolveUse`: o ramo A (prefixo `/`) e o ramo E (alias) foram unificados em `pkg.ResolveToWorkspaceRoot(t.workspaceRoot(), path)`, com um novo método `t.workspaceRoot()` (prefere `pkg.WorkspaceRoot()`, caso contrário procura para cima a partir de `sourcePath` até encontrar `workspace.noml`). A antiga lógica redundante `if t.pkg != nil { baseDir = pkg.RootDir; if wsRoot... }` foi eliminada; a alternativa relativa ao cwd passou a usar `FindWorkspaceRoot`.
- `src/build/transpiler.go` `processEmbeds`: os caminhos de embed relativos passaram a usar `pkg.ResolveEmbedBase(sourcePath)` (raiz da área de trabalho preferencialmente).
- `src/checker/checker.go` `ValidateEmbedAnnotations`: os caminhos de validação de embed usam igualmente `pkg.ResolveEmbedBase`.

As chaves de cache (`tokenCache` / `parseProgramFileCache`) obtêm o caminho absoluto canónico relativo à raiz da área de trabalho como componente de caminho através de `resolveFile(filePath)`; combinado com o hash do conteúdo do ficheiro de origem numa chave composta (`cache.Key`), as colisões desaparecem por construção, as edições no mesmo caminho invalidam-se automaticamente e a LRU com limite evita fugas de memória.

Os módulos incorporados `std/` e `js/` continuam a usar `StdFS`/`JsFS` (com chave `std/<rel>.no` / `js/<rel>.no`), sem alterações; `processEmbeds` aplica-se apenas ao programa principal (código de origem do utilizador) — os embeds dos próprios módulos std continuam a ser resolvidos relativamente à sua própria localização.

> Estado: **Implementado (2026-08-02)**. O código novo deve usar diretamente `pkg.ResolveToWorkspaceRoot` /
> `pkg.ResolveEmbedBase`; não usar o "diretório do ficheiro atual" (`filepath.Dir(t.sourcePath)` etc.)
> para resolução relativa.

## Glossário (Unificado, 2026-08-02)

- **workspace (área de trabalho)** — corresponde geralmente a um repositório. O diretório raiz contém `workspace.noml` e pode conter vários pacotes. A raiz da área de trabalho é a única base para a resolução de todos os caminhos de embed/importação.
- **package (pacote)** — uma grande unidade de compilação: uma biblioteca ou um executável. O diretório raiz contém `package.noml` (a **raiz do pacote**). Declara dependências, backend de emissão, etc. Um pacote contém vários módulos.
- **module (módulo)** — geralmente um ficheiro de origem (`.no`). Convenção atual: **um ficheiro = um módulo**. Carregar um módulo = carregar um ficheiro `.no`.

A hierarquia é: ** área de trabalho ⊃ pacote (um ou mais) ⊃ módulo (um ou mais)**. Não usar "module" para designar "package" (unidade de compilação) — essa é a função de "package".
