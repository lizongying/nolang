---
sidebar_position: 2
---

# Instalação e utilização

## Instalação

### Instalar a CLI

Transfira o executável da plataforma correspondente a partir de [GitHub Releases](https://github.com/lizongying/nolang/releases/latest) ou instale através do seguinte método:

```bash
# macOS / Linux
# 1. Transferir o binário
# 2. Colocá-lo no PATH
sudo mv nolang /usr/local/bin/no
```

### Instalar a extensão do VS Code

Instale a extensão Nolang a partir do [VS Code Marketplace](https://marketplace.visualstudio.com/items?itemName=lizongying.vscode-nolang), que oferece realce de sintaxe, diagnósticos LSP, navegação para a definição, conclusão automática e outros recursos.

## Comandos CLI

| Comando                                                         | Descrição                    |
| ------------------------------------------------------------ | ----------------------- |
| `no version`                                                 | Imprime a informação da versão            |
| `no init`                                                    | Define o espaço de trabalho (gera workspace.noml, sem package.noml) |
| `no new <name>`                                              | Cria um novo pacote no espaço de trabalho (subdiretório + package.noml e registo em workspace.noml) |
| `no fmt [-w] [-d] <file\|dir>`                               | Formata o código-fonte (inclui deduplicação/ordenação de importações e remoção automática de `# std/fmt`); os ficheiros de configuração `.noml` seguem um formato que preserva o layout (indentação/espaços/linhas em branco, sem alterar comentários nem a ordem das chaves) |
| `no build [-o <file>] [-cc <s>] [-target <s>] [-opt[=N]] [<file\|dir>]` | Compila (produz um executável) |
| `no run [-cc <s>] [-target <s>] [<package\|dir\|file>]`        | Compila e executa (nome do pacote/diretório/ficheiro) |
| `no test [-cc <s>] [-target <s>] [<file>]`                   | Executa os testes                |
| `no add <tool>[:<version>]@<host>/<owner>/<repo>`            | Adiciona uma dependência (módulo AST pré-compilado)|
| `no remove <pkg>`                                            | Remove uma dependência                |
| `no update <pkg>`                                            | Atualiza uma dependência                |
| `no update-all`                                              | Atualiza todas as dependências            |
| `no list`                                                    | Lista as dependências                |
| `no sync`                                                    | Sincroniza as dependências                |
| `no install [-u] [<tool>[:<version>]@<host>/<owner>/<repo>]`  | Instala um binário             |
| `no uninstall <name>`                                        | Remove um binário             |
| `no pack [-o <file>] [<dir\|file.no>]`                       | Empacota o AST (`<tool>-ast.zip`)|
| `no pub [-o <file>]`                                         | Igual a `no pack` (envio para o registry ainda não implementado)|
| `no cache <stat\|clear>`                                     | Consulta / limpa a cache de AST (normalmente não é necessário limpar)|

## Início rápido

O Nolang adota uma arquitetura de **um único repositório (projeto) com vários pacotes**: a raiz do repositório é o espaço de trabalho (apenas `workspace.noml`) e cada pacote é um subdiretório (com o seu próprio `package.noml`). `no init` e `no new` são duas operações distintas:

- `no init` — define o espaço de trabalho no diretório atual (gera apenas `workspace.noml`).
- `no new <name>` — cria um novo pacote no espaço de trabalho atual (gera o subdiretório `./<name>/` e o seu `package.noml`) e regista esse pacote em `workspace.noml`.

### Inicializar o espaço de trabalho e criar um pacote

```bash
# 1) Definir o espaço de trabalho na raiz do repositório
no init

# 2) Criar um pacote dentro do espaço de trabalho (registado automaticamente em workspace.noml)
no new foo

# 3) Entrar no diretório do pacote
cd foo

# Executar diretamente (compila e executa main.no automaticamente)
no run
```

Pode usar `no new` repetidamente no espaço de trabalho para adicionar mais pacotes:

```bash
no new bar
no new baz
```

Nesse momento, `workspace.noml` tem o seguinte aspeto:

```noml
{
  foo: '/foo'
  bar: '/bar'
  baz: '/baz'
}
```

> **Formato dos caminhos**: os valores de mapeamento em `workspace.noml` usam o prefixo `/` para indicar caminhos relativos ao espaço de trabalho (por exemplo, `/foo` → `workspaceRoot/foo`). Também são suportados saltos relativos `./` e `../` e caminhos absolutos do sistema operativo (como `/home/user/code/fork`), permitindo apontar para diretórios fora do espaço de trabalho.

Quando executado sem argumentos na raiz, `no build` compila em paralelo todos os pacotes do espaço de trabalho.

### Apenas inicializar o espaço de trabalho (sem criar pacotes)

```bash
# Definir o espaço de trabalho no diretório atual
no init
```

`no init` gera apenas `workspace.noml` (inicialmente vazio, `{}`) e **não gera `package.noml` nem `main.no`**. Se `workspace.noml` já existir, não será sobrescrito. Os pacotes têm de ser criados com `no new <name>`.

### Compilar e executar

```bash
# Compilar (procura main.no por predefinição)
no build                    # compila o diretório atual
no build main.no            # compila um ficheiro específico
no build -o output          # especifica o caminho de saída
no build -cc zig            # usa o compilador Zig
no build -target x86_64-linux-gnu  # compilação cruzada (especifica a plataforma de destino)

# Executar (compilar + executar)
no run                      # compila e executa o main.no do diretório atual
no run foo                  # executa o pacote chamado foo no espaço de trabalho (resolve workspace.noml)
no run ./foo                # executa o main.no do diretório ./foo
no run main.no              # executa um ficheiro .no específico
no run -cc zig
no run -target aarch64-macos-gnu
```

> `no build -v` escreve também o LLVM IR original junto ao caminho de saída (`<caminho de saída>.ll`). Por sua vez, `no build --shared -o libdemo.dylib main.no` exporta as funções de top-level marcadas com `#{cabi='symbol'}` como símbolos ABI C estáveis, liga-as como biblioteca dinâmica com `-shared -fPIC` e gera automaticamente um ficheiro de cabeçalho `.h` com o mesmo nome; pode usar `--header` para substituir o caminho do cabeçalho. Atualmente não existe a opção `-c` (compilar sem ligar). Para o procedimento completo de chamada ao Nolang a partir de C / C++ / Rust / Go / Java / Python / C#, consulte [Chamar o Nolang a partir de linguagens anfitriãs](./lang/host-interop).

O argumento posicional de `no run` é resolvido pela seguinte ordem:

1. Ficheiro `.no` existente → executa diretamente esse ficheiro
2. Diretório existente → executa o `main.no` que contém
3. Nome de pacote registado no espaço de trabalho (o `workspace.noml` mais próximo) → executa o `main.no` desse pacote

Sem argumentos, executa por predefinição o `main.no` do diretório atual. Se o diretório atual for a raiz do espaço de trabalho (contém `workspace.noml` mas não tem `main.no`), será apresentada uma sugestão para usar `no run <package>` e especificar um pacote.

### Destinos de compilação cruzada

O formato do parâmetro `-target` é `<arch>-<os>-<abi>` e são suportados os seguintes destinos:

| Tripla de destino           | Descrição           |
| -------------------- | -------------- |
| `x86_64-linux-gnu`   | Linux x86_64   |
| `aarch64-linux-gnu`  | Linux ARM64    |
| `x86_64-macos-gnu`   | macOS x86_64   |
| `aarch64-macos-gnu`  | macOS ARM64    |
| `x86_64-windows-gnu` | Windows x86_64 |

**Deteção automática da plataforma atual**: `no build`, `no run` e `no test`, quando não se especifica `-target`, detetam automaticamente a plataforma anfitriã atual e compilam para código nativo. No dia a dia não é necessário indicar o target manualmente:

```bash
no run hello.no          # executa diretamente em nativo
no test                  # executa os testes em nativo
no build -target aarch64-linux-gnu   # especificar explicitamente apenas quando for preciso compilação cruzada
```

### Seleção do compilador

O parâmetro `-cc` especifica o backend do compilador C:

- `clang` (predefinição) — requer LLVM instalado
- `zig` — requer Zig instalado, adequado para compilação cruzada

### Layout dos tipos embutidos (limite de inline de `?T`)

O layout de memória de `?T` é **dimensionado conforme o payload**: cada tipo de payload `T` tem o seu próprio `%option_<key> = { i8 tag, [K x i64] }` (1 byte de tag, payload fixo no deslocamento de bytes 8, `K = roundUp8(sizeof(T))/8`). Se o payload não couber no limite, passa a ser empacotado no heap e a primeira word da área de payload armazena um ponteiro.

`--option-payload-inline-threshold=N` especifica esse limite (contado em bytes por **toda a estrutura da opção**, ou seja `1 + 7 + sizeof(T)`):

```bash
no build --option-payload-inline-threshold=1032 main.no   # permite inline de payloads maiores
no build --option-payload-inline-threshold=16 main.no     # valor mínimo configurável
```

| Valor | Efeito |
| --- | --- |
| Predefinição **264** (= 1 + 7 + 256) | `?i64` 16 B, `?str` 24 B, `?txt` (256 B) 264 B inline; payloads maiores são empacotados |
| Mínimo **16** | Só `?i64` continua inline; a partir de `?str` (24 B), tudo é empacotado |
| < 16 | Erro de compilação |

Ordem de precedência: linha de comando > variável de ambiente `NOLANG_OPTION_PAYLOAD_INLINE_THRESHOLD` > `compiler.option-payload-inline-threshold` de `package.noml` > valor predefinido. A única fonte do layout é `src/mir/layout.go`; ao nível semântico (quais os valores considerados `ok`／`nil`／`err`) **nada é afetado**, apenas a ocupação de memória e a existência ou não de empacotamento mudam.

### Otimizador embutido do compilador (MIR opt)

O compilador inclui um otimizador que atua ao **nível MIR** (após o lowering do HIR e antes da geração de código LLVM). É distinto do `opt` do LLVM já existente no backend: o primeiro reescreve o MIR, o segundo reescreve o LLVM IR já gerado.

Ativa-se com `-opt[=N]` e está **desligado por predefinição**:

```bash
no build main.no          # predefinição: desativado (comportamento histórico)
no build -opt main.no     # equivalente a -opt=1
no build -opt=2 main.no   # dobra de constantes + eliminação de código morto + limpeza de fluxo de controlo
no build -opt=0 main.no   # desligado explicitamente
```

| Nível | Significado |
| ---- | ---- |
| `0` / `off` / `false` / `no` / `none` | Desligado (predefinição) |
| `1` / `on` / `true` / `yes` / `-opt` | Dobro de constantes + reescrita de identidades inteiras + propagação de cópias + eliminação de escalares mortos |
| `2` | Nível 1 + limpeza de fluxo de controlo (dobra de ramos constantes, eliminação de blocos inalcançáveis, encadeamento de saltos em blocos vazios, fusão de blocos com um único predecessor) + poda do prelúdio |

`-opt` é uma opção **global** (processada em `main`, tal como `-v`), pelo que é suportada por `build`, `run` e `test`.

A mesma opção também pode ser especificada pela variável de ambiente `NOLANG_MIR_OPT`; a flag de linha de comando apenas escreve nessa variável, pelo que não podem ocorrer inconsistências entre ambas. Para ver o que a otimização fez:

```bash
NOLANG_MIR_OPT=2 NOLANG_MIR_OPT_STATS=1 no build -o out main.no
# [miropt] level=2 funcs=3 folded=5 (arith=2 cmp=3) ident=1 copies=4 dead=6 cfg{condbr=3 unreachable=4(5 insts) threaded=1 merged=5}
# [miropt] prelude: dropped 27 unreferenced helpers
```

| Campo | Significado |
| ---- | ---- |
| `folded` (`arith`/`cmp`) | Número de instruções aritméticas／de comparação dobradas em constantes |
| `ident` | Identidades inteiras simplificadas para um dos operandos (`x+0`, `x*1`, `x&x`, `x^0`…) |
| `copies` | Cópias de escalares removidas por propagação de cópias |
| `dead` | Instruções escalares mortas removidas |
| `cfg{condbr, unreachable, threaded, merged}` | Dobra de ramos constantes, eliminação de blocos inalcançáveis (com o número de instruções), encadeamento de saltos em blocos vazios, fusão de blocos com um único predecessor |
| `prelude: dropped N` | Número de helpers de runtime no prelúdio do codegen provados como não referenciados (ver abaixo) |

### Poda do prelúdio (nível 2)

`emitPrelude` escreve o runtime inteiro (`str_*`, `print_*`, `eprint_*`, `vec_*`, `nolang_*`) em **todos os** módulos, independentemente de o programa os usar — um programa `print('hi')` gera 60 definições de funções, das quais só 8 são alcançáveis a partir da entrada e **52 nunca são chamadas**.

**Isto é algo que `opt -O2` não consegue fazer**: o GlobalDCE do LLVM só remove definições `internal`, e o prelúdio é emitido com external linkage (os módulos `no` podem ser ligados com C, pelo que os helpers de que o linker possa precisar não podem ser internal). Medido nesse programa, exatamente 7 das 60 definições eram `define internal`, `opt -O2` removeu exatamente essas 7, e o `.ll` chegou mesmo a crescer de 61,812 para 69,874 bytes (+8,062).

Por isso, o nível 2 efetua, após a geração do IR, uma poda de alcance sobre o **texto já completo**, mantendo apenas os helpers realmente referenciados (e, recursivamente, os que eles próprios chamam). No mesmo `print('hi')`: número de definições 60 → **16**, `.ll` 61,812 → **14,548** bytes (**−76,5%**), executável 36,616 → 34,648 (−5,4%). A poda reconhece apenas referências reais — **nomes que aparecem em comentários não contam**, e é essa a razão pela qual consegue remover 44 e não 33; além disso, ao eliminar uma função, **o respetivo comentário descritivo também é eliminado** (`optTrailingComment`), caso contrário ficariam 12,609 bytes a descrever 44 funções que já não existem.

Remover os comentários **não reduz o executável** — os comentários são descartados ao analisar com `opt`/`llc`, e o executável é de 34,648 bytes em ambos os casos. Esses 12,609 bytes são um ganho no texto do IR e na quantidade analisada em tempo de compilação.

As aproximações da poda tendem sempre a preservar — blocos dos quais não é possível extrair um nome único são mantidos, se um bloco não tiver chaveta de fecho a poda para aí, e `declare`／globais／definições de tipos nunca são removidas. Perder uma otimização são apenas mais alguns bytes; remover algo errado é um erro de ligação.

**O prelúdio pode ser removido por completo? Não.** A região do prelúdio não contém apenas funções: contém também os **literais de string do próprio programa** (`@.mir.str.N`, ou seja `"hi"`), tipos como `%str-long`／`%option`／`%task` e declarações externas como `@malloc`／`@write`／`@llvm.memcpy` — estes dados representam cerca de 30% do módulo e, sem eles, nem `llc` passa (`error: use of undefined value '@.mir.str.1'`). O que se pode remover é apenas a metade das **funções**: ao trocar 48 `define` por `declare` com o mesmo nome, os símbolos indefinidos reportados pelo linker são exatamente quatro — `print_nl`, `print_str`, `str_free`, `str_from_const`. Portanto, a pergunta correta é «podemos emitir sob demanda?» e não «podemos removê-lo?».

Esse desperdício é um **imposto fixo, independente do tamanho do programa**: o código e os comentários do prelúdio são idênticos módulo a módulo (48 definições, 36,043 bytes de código ＋ 17,516 de comentários) e, numa amostragem de 40 ficheiros, a retenção foi de **0–19, com mediana 9**. `tests/dup.no`／`tests/q.no`／`tests/u128-types.no` retêm **0** — o prelúdio inteiro é código morto para eles. Assim, a poupança é determinada inteiramente pelo tamanho do módulo: −76,5% para um programa de uma linha, apenas −4,1% para `test-std/src/pbkdf2.no` de 3,0 MB.

O prelúdio pode ser removido por completo? Não — a região contém ainda os literais de string do próprio programa (`@.mir.str.N`), tipos e declarações externas (1,151 bytes), e sem eles nem `llc` passa. Além disso, `extraFuncs`／`extraFuncsBody` em `codegen.go:75-83` já são, por natureza, um emissor sob demanda (os helpers de option／struct／enum／ptr passam todos por ele); o prelúdio é a única parte que não foi convertida — mas convertê-lo traria sobretudo comentários, não havendo já quase nada a ganhar do lado do código.

Este otimizador dobra apenas **escalares** (`i64`/`u64`/`i16`/`i32`/`char`/`bool`/`f32`/`f64`). `byte`/`u8`/`i8`, `i128`/`u128`, `option`, fatias, estruturas, etc. são sempre ignorados: os primeiros não têm ramo correspondente em `emitConst` (degenerariam em 0) e a aritmética dos segundos passa pelo caminho de empacotamento por overflow de `option`, pelo que a dobra alteraria a semântica. Os valores de vírgula flutuante também estão excluídos de todas as identidades — `x * 0.0` não é 0 para `inf`/`NaN` e `x + 0.0` não é `x` para `-0.0`.

A conclusão central dessa comparação merece ser apresentada desde já: **a limpeza local (dobra de constantes, eliminação de código morto, limpeza de fluxo de controlo) tem valor marginal quase nulo depois de `opt -O2` ter passado**（−0,06%），porque o LLVM faz o mesmo tipo de trabalho de forma mais completa. O que verdadeiramente compensa é o que o LLVM **não pode fazer por causa do linkage** — ou seja, a poda do prelúdio descrita acima. Com a poda do prelúdio incluída (com poda sensível a comentários), o valor marginal global de `mir+llvm_O2` → `mir_opt+llvm_O2` é de **−19,05% em instruções / −15,83% em texto**; e a redução de texto de `mir` → `mir+mir_opt` passa de −14,29% para **−17,75%** (corpus completo de 631 ficheiros).

## Backend JavaScript

O backend JS (`no build --js` / `no run --js`) compila código-fonte `.no` em JavaScript (com erasure de tipos),
suportando os destinos Node.js e browser, as anotações de plataforma `#{js}` / `#{js-browser}` e importações ESM de terceiros
(`# npm/<pkg>`).

A documentação completa sobre JS (parâmetros de linha de comando, mapeamento de funções embutidas, biblioteca padrão `js/*`, import map do browser, etc.)
está concentrada no documento autónomo [Backend JavaScript](./lang/js-backend) e não é repetida nesta página.

## Regras de ponto de entrada

- **main.no** — ponto de entrada do programa
- **lib.no** — ponto de entrada da biblioteca, exporta funções (consulte a [documentação de exportação](lang/export))
- **Todos os ficheiros .no no diretório tests/** — contêm asserções de teste

## Testes

```bash
# Testar todos os ficheiros .no no diretório tests/
no test

# Executar um único ficheiro de teste
no test my-test.no

# Usar um compilador ou destino específico
no test -cc zig
no test -target x86_64-windows-gnu
```

Notas sobre os testes:

- Os ficheiros de teste são colocados de forma uniforme no diretório tests/
- Cada ficheiro de teste é compilado de forma independente
- Se algum teste falhar, é devolvido um código de saída diferente de zero

## Cache de AST

`no build` / `no run` / `no test` gravam em disco a AST resultante da análise de cada ficheiro de origem do utilizador
(predefinição `~/.cache/nolang/ast`, podendo o caminho ser alterado com `$NOLANG_AST_CACHE_DIR`); da próxima vez, se o conteúdo não mudar, a AST é reutilizada diretamente,
evitando o lex e o parse. A biblioteca padrão usa um blob embutido no compilador e não ocupa esta cache.

**Não é necessário limpar manualmente.** A chave de cache é

```
sha256( ASTVersion ‖ salt de análise ‖ conteúdo do ficheiro de origem )
```

em que um dos componentes do salt é `checker.StdSigKey` — a assinatura de conteúdo de todos os ficheiros listados em `src/hash.noml`
(biblioteca padrão + o próprio compilador). Assim, qualquer alteração no compilador ou na biblioteca padrão faz com que o `no` reconstruído com `make` use uma chave nova; as entradas antigas naturalmente falham e não são reutilizadas.

| Comando | Descrição |
| --- | --- |
| `no cache stat` | Mostra o diretório da cache, o número de entradas, o tamanho total e o `ASTVersion` atual |
| `no cache clear` | Limpa todas as entradas (apenas em caso de emergência: quando se suspeita que estão desatualizadas) |

Variáveis de ambiente:

| Variável | Função |
| --- | --- |
| `NOLANG_AST_CACHE_OFF=1` | Desliga a cache (útil para comparações A/B do compilador e para investigar «erros corrigidos que voltam a aparecer») |
| `NOLANG_AST_CACHE_DIR` | Personaliza o diretório da cache. **Ao mesmo tempo**, afeta a cache dos ficheiros `-ast.zip` das dependências AST pré-compiladas (essa cache fica, por predefinição, em `~/no/cache/ast`, ver «Dependências AST pré-compiladas» abaixo) |
| `NOLANG_AST_CACHE_MAX` | Limite do número de entradas na cache (predefinição 4096) |

> As entradas antigas simplesmente deixam de ser encontradas e são eliminadas à medida que novas entradas são escritas, de acordo com o limite — pelo que **não é preciso** apagar `~/.cache/nolang/ast`.
> Atenção: o `no` tem de ser reconstruído com `make gen` / `make no`; um binário compilado diretamente com `cd src && go build` não tem o `StdSigKey` injetado e termina com erro em vez de reutilizar a cache silenciosamente.

## Instalar e desinstalar binários

### Instalação

```bash
# Instalar o pacote no diretório atual (compilação local)
no install
no install -u                                   # forçar reconstrução (atualização)

# Instalar um binário pré-compilado a partir de um GitHub Release (recomendado): <tool>[:<version>]@<host>/<owner>/<repo>
no install nogit@github.com/lizongying/notools            # versão mais recente
no install nogit:v0.1.33@github.com/lizongying/notools    # versão específica
no install nogit@lizongying/notools                       # omite o host, predefinição github.com
no install -u nogit@github.com/lizongying/notools         # forçar nova transferência
```

A instalação remota obtém um **binário pré-compilado**, e não código-fonte: o nome do recurso é `<tool>-<os>-<arch>-<version>` (com `.exe` no Windows), em que `<os>-<arch>` provém da plataforma na qual o próprio `no` está a executar (por exemplo `darwin-arm64`, `linux-amd64`). Por isso não é necessário um toolchain LLVM local e a ferramenta instalada não precisa de estar escrita em Nolang. Quando a versão é omitida, consulta-se `releases/latest`; definir `$GITHUB_TOKEN` (ou `$GH_TOKEN`) evita o limite de taxa da API do GitHub.

Processo de instalação:

1. Remoto: analisar a spec → localizar o recurso do release → transferir; local: usar a compilação do diretório atual
2. Escrever o binário em `~/no/bin/`
3. Criar uma ligação simbólica em `/usr/local/bin/`

### Desinstalação

```bash
no uninstall pkg-name
```

A desinstalação remove a ligação simbólica em `/usr/local/bin/` e o binário em `~/no/bin/`.

## Configuração do projeto

### O formato de configuração noml

Todos os ficheiros de configuração do Nolang (`package.noml`, `workspace.noml`, `.workspace.noml`, `nolang.lock.noml`, `sum.noml`) usam o formato **noml** (Nolang Markup Language) desenvolvido internamente, que substitui o antigo JSONC. As regras de sintaxe estão alinhadas com a própria linguagem Nolang:

| Regra | Descrição |
| --- | --- |
| Objeto de topo | É um objeto: `{` e `}` ocupam cada um a sua própria linha, um membro por linha, **sem vírgulas separadoras** (a quebra de linha serve de separador) |
| Chaves | Por predefinição identificadores nus, com as mesmas regras dos nomes de variáveis Nolang: letras minúsculas/dígitos/hífen (ao meio)/start por underscore, não podendo começar por um dígito (como `link-libs`, `anonymous-fn-type`, `dev-dependencies`); chaves que não satisfazem as regras (caminhos, URL, `@scope/pkg`) são escritas como chaves de cadeia entre aspas simples |
| Valores de cadeia | Aspas simples, com escape `\`: `\' \\ \n \t` |
| Booleanos | `true` / `false` (tudo em minúsculas, como os literais bool do Nolang) |
| Números | Literais nus (reservados para ficheiros de bloqueio, etc.) |
| Arrays | `[`, elementos, `,` e `]` podem ocupar várias linhas; os elementos são separados por **vírgulas** e é **suportada a vírgula final**; um array vazio escreve-se `[]` |
| Comentários | `;` até ao fim da linha (exceto dentro de cadeias) |
| Indentação | Sem significado (afeta apenas a legibilidade, sem garantias de formatação) |

`dependencies` / `dev-dependencies` são **arrays de cadeias**, cujos elementos têm a forma `'chave:versão'` (a versão pode ser omitida, com predefinição `*`), por exemplo:

```noml
{
  dependencies: [
    '/example/test2',                          ; pacote local, qualquer versão por predefinição
    'github.com/lizongying/nolang/test1:v0.1.0', ; pacote online, versão especificada,
  ]
}
```

O ficheiro `package.noml` na raiz do projeto descreve a informação do projeto:

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

`compiler.lang` fixa o idioma das mensagens deste projeto (`'zh-tw'`, `'ja'` etc., com a mesma notação de `--lang`): tem precedência sobre o locale do sistema, mas é inferior a `--lang` e `NO_LANG`; se omitido, segue o sistema.

### Gestão de dependências

```bash
# Adicionar dependência: <tool>[:<version>]@<host>/<owner>/<repo>
# O número de versão pode ser omitido (usa o último release); a chave gravada em package.noml é <tool>@<host>/<owner>/<repo>
no add nogit@github.com/lizongying/notools
no add nogit:v0.1.33@github.com/lizongying/notools

# Remover dependência
no remove pkg-name

# Atualizar dependência
no update pkg-name

# Atualizar todas as dependências
no update-all

# Listar dependências
no list

# Sincronizar dependências (transferir e gerar o ficheiro de bloqueio)
no sync
```

### Dependências AST pré-compiladas

Dependências como `no add <tool>@<host>/<owner>/<repo>` **têm apenas AST, sem código-fonte**: em tempo de compilação obtêm `<tool>-ast.zip` (zip with one .gob entry per module) a partir do release e, após descodificação, são injetadas diretamente no pipeline de compilação. Uso:

```nolang
# nogit                      # alias do pacote; módulos são nogit.<module>
# nogit/remote                  # importar diretamente o módulo remote (ou nogit.remote)
# nogit.fn                      # nogit é apenas o nome do pacote; localizar o módulo que define fn
# <host>/<owner>/<repo>/nogit/remote
```

Cada zip é um pacote, que pode conter vários módulos, cada um como entrada `<modulePath>.gob`. Não há módulo de entrada: `# nogit` cria apenas um alias do pacote (módulos em `nogit.<module>`, ex.: `nogit.remote.greet(...)`); `# nogit/remote` importa um módulo diretamente; `# nogit.fn` localiza um módulo por função. O `no fmt` remove imports `# nogit` / `# nogit/remote` não usados. A versão tem de ser um valor concreto (`no sync` / a compilação reportam erro perante `""` ou `"*"`). O zip é colocado em cache em `~/no/cache/ast/` e pode ser substituído com `$NOLANG_AST_CACHE_DIR`.

Os recursos de saída são produzidos com `no pack` (basta o lexer e o parser, sem toolchain LLVM, ideal para CI):

```bash
no pack -o nogit-ast.zip nogit.no      # um único ficheiro de origem
no pack -o nogit-ast.zip ./nogit       # um diretório de pacote
```

### Tipos de dependência e regras de versão

As dependências em `package.noml` dividem-se em **pacotes locais** e **pacotes online**. O compilador determina automaticamente o tipo de dependência segundo as regras seguintes e emite um aviso quando um pacote local não usa a versão `"*"`.

#### Regras de classificação

```
chave de dependência → procura em workspace.noml (correspondência por nome curto ou chave completa)
  ├─ encontrada → pacote local (deve usar "*")
  └─ não encontrada → verificar prefixo do caminho (começa com /)
      ├─ sim → pacote local (deve usar "*")
      └─ não → pacote online (usar número de versão, sem aviso)
```

1. **Procurar em `workspace.noml`**: primeiro, procura-se a chave de dependência em `workspace.noml` como nome curto ou chave completa. Se encontrada, é um pacote local.
2. **Verificar o prefixo do caminho**: se não for encontrada em `workspace.noml` e a chave começar por `/` (caminho relativo ao espaço de trabalho), também é um pacote local.
3. **Todo o resto é pacote online**: chaves que não satisfazem as condições acima são tratadas como pacotes online e devem especificar um número de versão.

> **Restrição de fronteira do espaço de trabalho**: os caminhos locais em `package.noml` só podem começar por `/` (relativos ao espaço de trabalho); são proibidos `./`, `../` e caminhos absolutos do sistema operativo. A única forma de ultrapassar a fronteira do espaço de trabalho são os mapeamentos em `workspace.noml` / `.workspace.noml`. Consulte [Restrições de fronteira do espaço de trabalho](#restrições-de-fronteira-do-espaço-de-trabalho).

#### Formas de referência a pacotes locais

Os pacotes locais suportam as três formas de referência seguintes; todas devem usar `"*"` como versão:

```noml
dependencies: [
  ; 1. Nome curto: uma chave registada em workspace.noml
  'test2',
  ; 2. Caminho relativo ao espaço de trabalho: começa com /, relativo à raiz do espaço de trabalho
  '/example/test2',
  ; 3. URL completa: se existir um mapeamento correspondente em workspace.noml, é um pacote local
  'github.com/lizongying/nolang/test2',,
]
```

> **Atenção**:
> - A 3.ª forma só é um pacote local se existir um mapeamento correspondente em `workspace.noml`. Se `workspace.noml` mapear `github.com/lizongying/nolang/test2` para um caminho local, essa dependência é local (deve usar `"*"`); caso contrário é online (deve usar um número de versão).
> - Os prefixos `./` e `../` deixaram de ser permitidos em `package.noml`. Todos os caminhos locais têm de começar por `/` (relativos ao espaço de trabalho).

#### Uso avançado: converter um pacote online em local

`workspace.noml` não se limita a registar nomes curtos: também pode mapear o nome de um pacote online para um caminho local, permitindo o modo de desenvolvimento «online para local»:

```noml
; workspace.noml
{
  test2: '/example/test2'
  'github.com/lizongying/nolang/test2': '/example/test2'
}
```

Nesse caso, uma dependência referida em `package.noml` como `github.com/lizongying/nolang/test2` é resolvida como o pacote local `/example/test2` e a versão deve usar `"*"`. Isto é muito conveniente ao desenvolver localmente um pacote online — basta adicionar um mapeamento em `workspace.noml` para trocar uma dependência remota por código-fonte local e depurar em conjunto.

#### Avisos de versão

Se, em tempo de compilação, for detetado que um pacote local usa um número de versão diferente de `"*"` (como `"v0.1.0"`), é emitido um aviso a sugerir `"*"`. Os pacotes online não estão sujeitos a esta restrição.

#### Mapeamento recursivo do espaço de trabalho (cadeias de mapeamento entre pacotes)

O Nolang suporta **mapeamento recursivo do espaço de trabalho**: um pacote dependente pode trazer o seu próprio `workspace.noml`, com novas regras de mapeamento no seu interior, formando uma cadeia natural de resolução entre pacotes. Esta é a capacidade diferenciadora central do Nolang — os mecanismos `replace`/`patch` do Go/Cargo dominantes só têm efeito no projeto atual e não se propagam para dentro dos pacotes dependentes.

**Cenários aplicáveis:**
- Unificar aliases internos numa biblioteca base
- Padronizar caminhos de importação
- Migrações de compatibilidade entre várias camadas de bibliotecas

**Como funciona:**

Ao resolver uma chave de dependência, o compilador:
1. Procura a chave no `workspace.noml` atual
2. Se encontrar um mapeamento, verifica se o diretório de destino tem o seu próprio `workspace.noml`
3. Em caso afirmativo, continua a procurar com base na mesma chave, resolvendo nível a nível
4. Até não existirem mais mapeamentos profundos

**Exemplo:**

```
workspace/                  ← espaço de trabalho de nível superior
├── workspace.noml         ← {"testkey": "/pkgA"}
├── pkgA/
│   ├── workspace.noml     ← {"testkey": "/pkgB"}
│   └── pkgB/
│       ├── workspace.noml ← {"testkey": "/pkgC"}
│       └── pkgC/           ← destino final (sem workspace.noml)
```

Ao resolver `testkey`:
1. Mapeamento do `workspace.noml` superior → `/pkgA` (relativo ao espaço de trabalho)
2. Mapeamento de `pkgA/workspace.noml` → `/pkgB` (relativo ao espaço de trabalho de pkgA)
3. Mapeamento de `pkgB/workspace.noml` → `/pkgC` (relativo ao espaço de trabalho de pkgB)
4. `pkgC` não tem `workspace.noml` → resolução final em `pkgC`

**Deteção de ciclos:**

O compilador mantém uma pilha de visitas de resolução que regista as raízes de espaço de trabalho já visitadas. Logo que deteta um mapeamento circular (por exemplo A→B, B→A), reporta imediatamente um erro com a cadeia completa:

```
Error: circular workspace mapping detected: /path/to/A → /path/to/B → /path/to/A
```

> **Nota**: esta capacidade suporta apenas o mapeamento recursivo natural entre pacotes (o pacote dependente traz o seu próprio `workspace.noml`); não suporta abreviaturas manuais de cadeia num único ficheiro.

### Configuração de espelhos

Configure endereços de espelhos no array `mirrors` de `package.noml` para acelerar a transferência de pacotes remotos:

```noml
mirrors: [
  'https://mirror.example.com/',,
]
```

### Configuração do espaço de trabalho workspace.noml

`workspace.noml` fica na raiz do repositório e descreve um espaço de trabalho de **um único repositório com vários pacotes**, sendo um mapeamento «nome do pacote → caminho relativo». Quando executado sem argumentos e com `workspace.noml` presente, `no build` compila em paralelo todos os pacotes nele listados; `no run` executa um único pacote do espaço de trabalho através do seu nome (como `no run foo`).

```noml
{
  foo: '/foo'
  bar: '/bar'
}
```

`no init` é responsável por definir o espaço de trabalho: se `workspace.noml` não existir, gera um objeto vazio `{}` e **não gera qualquer `package.noml`**; se já existir, mantém-se inalterado e não é sobrescrito. Quando se cria um pacote com `no new <name>`, o mapeamento `"<name>": "./<name>"` é registado automaticamente em `workspace.noml`.

#### Configuração privada local .workspace.noml

Além do `workspace.noml` público, o Nolang suporta também um ficheiro de configuração privado local `.workspace.noml` (note o `.` inicial). Recomenda-se adicionar este ficheiro ao `.gitignore`, destinando-se a substituições temporárias de dependências próprias de cada programador.

**Lógica de carregamento:**

1. Primeiro carrega a configuração pública `workspace.noml`
2. Depois carrega a configuração privada `.workspace.noml`
3. **Mesma chave**: a configuração privada substitui a pública
4. **Chave nova**: são acrescentadas mutuamente

```
workspace/
├── workspace.noml       ← configuração partilhada do projeto (sob controlo de versão)
├── .workspace.noml      ← configuração privada local (recomenda-se .gitignore)
├── foo/
└── bar/
```

**Cenários de utilização:**

- Aliases e regras de reencaminhamento de dependências comuns à equipa são escritos em `workspace.noml`, sob controlo de versão
- Quando um programador precisa, ao depurar localmente, de apontar temporariamente uma dependência para um fork local, basta substituí-la em `.workspace.noml`
- Evita que mapeamentos temporários de forks poluam a configuração pública do projeto, conciliando a colaboração em equipas grandes com a flexibilidade do desenvolvimento local

**Exemplo:**

```noml
; workspace.noml (público, sob controlo de versão)
{
  foo: '/foo'
  bar: '/bar'
}

; .workspace.noml (privado, depuração local)
{
  ; Substituir a configuração pública: foo aponta para um fork local (dentro do espaço de trabalho)
  foo: '/my-fork-foo'
  ; Novo mapeamento privado: aponta para um fork fora do espaço de trabalho (caminho absoluto do SO, única saída de escape)
  'github.com/lizongying/nolang/core': '/home/lzy/code/fork/core'
  ; Novo mapeamento privado (dentro do espaço de trabalho)
  experimental: '/experimental'
}
```

Os mapeamentos finais efetivos são: `foo` → `/my-fork-foo` (substituição privada), `bar` → `/bar` (preservado da configuração pública), `github.com/lizongying/nolang/core` → `/home/lzy/code/fork/core` (acrescentado privadamente, escapa do espaço de trabalho) e `experimental` → `/experimental` (acrescentado privadamente).

### Restrições de fronteira do espaço de trabalho

O Nolang aplica um controlo rigoroso da fronteira do espaço de trabalho às referências de caminhos, garantindo a portabilidade e a segurança do projeto.

#### Visão geral das regras

| Nível de configuração | `/` relativo ao espaço de trabalho | Caminho absoluto do SO | Saltos relativos `./` `../` |
| --- | --- | --- | --- |
| Chaves de dependência em `package.noml` | ✅ permitido | ❌ proibido | ❌ proibido |
| Valores de mapeamento em `workspace.noml` | ✅ permitido | ✅ permitido (saída de escape) | ✅ permitido (saída de escape) |
| Valores de mapeamento em `.workspace.noml` | ✅ permitido | ✅ permitido (saída de escape) | ✅ permitido (saída de escape) |
| Caminhos de importação `#` no código-fonte | ✅ permitido | ❌ proibido | ❌ proibido |

#### 1. package.noml (estritamente limitado, impede a saída do espaço de trabalho)

`package.noml` é a configuração de base padrão para publicação externa, CI e colaboração entre várias pessoas, e é submetida ao repositório de código.

```noml
#{fmt-skip}
dependencies: [
  ; ✅ válido: começa com /, relativo à raiz do workspace
  '/vendor/test',
  ; ❌ proibido: caminho absoluto do sistema operativo /home/xxx/...
  ; ❌ proibido: saltos relativos ./ ../
]
```

```
**Restrições:**
- Os caminhos locais só podem ser escritos como `/xxx`, com a raiz do workspace como base
- Não são aceites caminhos absolutos do sistema operativo
- Não são suportados `./` nem `../`
- Não é possível referenciar diretamente qualquer diretório fora do repositório de trabalho

**Objetivo:** se fossem permitidos caminhos externos, seria muito fácil chegar a «compila na minha máquina, falha na dos outros», além do risco de segurança de carregar acidentalmente código estranho externo.

#### 2. workspace.noml + .workspace.noml (a única saída de escape)

Os destinos de mapeamento suportam três tipos de caminho local:
- `/xxx` → diretório dentro da raiz do workspace (mesma semântica de package.noml)
- `./xxx`, `../xxx` → resolvidos relativamente à raiz do workspace, permitindo sair da fronteira do espaço de trabalho
- Caminho absoluto do sistema operativo → permite apontar para fora do espaço de trabalho (forks, componentes externos)

```noml
; .workspace.noml configuração privada, para desenvolvimento local
{
  ; Dentro do espaço de trabalho
  foo: '/vendor/foo'
  ; Salto relativo: aponta para um fork fora do espaço de trabalho
  'github.com/lizongying/nolang/core': '../fork/core'
  ; Caminho absoluto do SO: aponta para qualquer diretório externo
  'github.com/lizongying/nolang/utils': '/home/lzy/code/utils'
}
```

**Lógica fundamental:** `package.noml` não tem capacidade para referenciar código externo diretamente; se o utilizador quiser montar código-fonte fora do repositório, tem de declarar explicitamente o mapeamento na configuração do workspace. Isto equivale a acrescentar um interruptor explícito e visível ao «carregamento entre diretórios», com todas as dependências externas centralizadas num único local.

#### 3. Restrições correspondentes ao nível de importação no código-fonte

Não é permitido escrever diretamente caminhos absolutos do sistema operativo em importações no código-fonte; todas as referências a código externo só podem ser introduzidas indiretamente através de aliases do workspace ou de identificadores de pacotes remotos. Os saltos relativos `./` e `../` também são proibidos.

```no
; ✅ válido: caminho relativo ao espaço de trabalho
# /vendor/utils.helper
; ✅ válido: identificador de pacote remoto
# github.com/lizongying/nolang/core.process
; ✅ válido: biblioteca padrão
# std/fs
```

```no
#{fmt-skip}
; ❌ proibido: saltos relativos
; # ./utils.helper
; # ../lib/helper.process

; ❌ proibido: caminho absoluto do sistema operativo
; # /home/user/code/core.process
```

#### Demonstração do fluxo de dados

Cenário: a raiz do espaço de trabalho do projeto de negócio está em `/code/project` e pretende-se usar `/code/fork/core`, externo.

❌ Não se pode fazer isto (proibido em package.noml):

```noml
; package.noml
dependencies: [
  '/code/fork/core', ; ilegal: não é possível escapar do espaço de trabalho,
]
```

✅ Procedimento padrão (ultrapassar a fronteira através do mapeamento do workspace):

```noml
; .workspace.noml
{
  'github.com/lizongying/nolang/core': '/code/fork/core'
}
```

```no
; importação normal no código-fonte
# github.com/lizongying/nolang/core
```

#### Fluxo de trabalho do espaço de trabalho

Os diretórios de entrada para compilação e execução são sempre o **diretório do espaço de trabalho** (onde se encontra `workspace.noml`). O fluxo global é o seguinte:

1. O utilizador executa `no build` ou `no run <nome do pacote>` no diretório do espaço de trabalho
2. O compilador lê `workspace.noml` e procura o caminho do subdiretório correspondente pelo nome do pacote
3. Carrega o `package.noml` nesse subdiretório (ou seja, a raiz do pacote) e compila/executa tomando a raiz do pacote como base
4. Todos os caminhos de importação (`use /path/to/module`) são **resolvidos relativamente à raiz do espaço de trabalho** (consulte [Convenção de resolução de caminhos](path-resolution.md)), ou seja, têm como única base o diretório que contém `workspace.noml`, deixando de depender do pacote onde a importação ocorre.

```
workspace/               ← diretório do espaço de trabalho (workspace.noml fica aqui)
├── workspace.noml      ← mapeamento nome do pacote → caminho
├── foo/                 ← pacote foo
│   ├── package.noml        ← raiz do pacote foo
│   ├── main.no
│   └── lib.no
└── bar/                 ← pacote bar
    ├── package.noml        ← raiz do pacote bar
    └── main.no
```

> **Nota**: os caminhos de importação têm como base a **raiz do espaço de trabalho** (o diretório que contém `workspace.noml`), e não o diretório `package.noml` do pacote. Se existir um `package.noml` aninhado num subdiretório do pacote, `LoadPackage` procura para cima e encontra o `package.noml` mais próximo, usando-o como raiz do pacote, mas a resolução de importações é sempre feita com base na raiz do espaço de trabalho.

---

## Projetos

- [notools](https://github.com/lizongying/notools) — um conjunto de utilitários de linha de comando Unix implementados na linguagem Nolang, cobrindo ferramentas clássicas como cat, ls, grep, wc, head e tail, e demonstrando a aplicação prática do Nolang na programação de sistemas.
