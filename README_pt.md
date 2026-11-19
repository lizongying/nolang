# Nolang

[繁體中文](README.md) · [简体中文](README_zh-CN.md) · [한국어](README_ko.md) · [日本語](README_ja.md) · [English](README_en.md) · [Русский](README_ru.md) · [Français](README_fr.md) · [Español](README_es.md) · [Deutsch](README_de.md) · [Português](README_pt.md)

Nolang é uma linguagem de programação de sistemas com segurança de memória, alto desempenho e sintaxe mínima.

## Funcionalidades principais

- **Segurança de memória**: Gerenciamento automático de memória
- **Alto desempenho**: Otimização em tempo de compilação, sem GC
- **Sintaxe mínima**: Sintaxe mais simples
- **Orientada à intenção**: Respeita plenamente a intenção do desenvolvedor, com semântica conforme a intuição humana
- **À prova de falhas**: Elimina caminhos implícitos de falha

## Uso

doc

[docs](https://lizongying.github.io/nolang/)

Plugin do VSCode

[vscode](https://marketplace.visualstudio.com/items?itemName=lizongying.vscode-nolang)

nolang cli

[nolang](https://github.com/lizongying/nolang/releases/latest)

### Comandos CLI

| Comando                                                      | Descrição                                                                       |
| ------------------------------------------------------------ | ------------------------------------------------------------------------------- |
| `no init`                                                    | Define o espaço de trabalho (gera workspace.jsonc, sem package.jsonc)            |
| `no new <name>`                                              | Cria um novo pacote no espaço de trabalho (subdiretório + package.jsonc, registrado em workspace.jsonc) |
| `no fmt [-w] [-d] <file\|dir>`                               | Formata o código-fonte                                                          |
| `no build [-o <file>] [-cc <s>] [-target <s>] [<file\|dir>]` | Compila o projeto (gera o executável)                                           |
| `no run [-cc <s>] [-target <s>] [<package\|dir\|file>]`        | Compila e executa (nome do pacote/diretório/arquivo)                            |
| `no test [-cc <s>] [-target <s>] [<file\|dir>]`              | Executa testes                                                                  |
| `no add <pkg>`                                               | Adiciona uma dependência                                                        |
| `no remove <pkg>`                                            | Remove uma dependência                                                          |
| `no update <pkg>`                                            | Atualiza uma dependência                                                        |
| `no update-all`                                              | Atualiza todas as dependências                                                  |
| `no list`                                                    | Lista as dependências                                                           |
| `no sync`                                                    | Sincroniza as dependências                                                      |
| `no install [-u] [<tool>[:<version>]@<host>/<owner>/<repo>]` | Instala o binary (~/no/bin/ + link simbólico)                                   |
| `no uninstall <name>`                                        | Remove o binary e o link simbólico                                              |
| `no pub --token <token> [--registry <url>]`                  | Publica na registry                                                             |

```bash
# Compilar (começa por main.no por padrão)
no build                    # compilar o diretório atual
no build main.no            # compilar o arquivo especificado
no build -o output          # especificar o caminho de saída
no build -cc zig            # usar o compilador Zig
no build -target x86_64-linux-gnu  # compilação cruzada (especificar a plataforma de destino)

# Executar (compilar + executar)
no run                    # compila e executa o main.no do diretório atual
no run foo                # executa o pacote chamado foo no espaço de trabalho
no run ./foo              # executa o main.no do diretório ./foo
no run -cc zig
no run -target aarch64-macos-gnu

# Testar (compilar + executar)
no test                   # testa todos os arquivos .no no diretório `test`
no test test/my-test.no        # executa um único arquivo de teste
no test -cc zig
no test -target x86_64-windows-gnu
```

### Criar um novo projeto

```shell
no new test1

cd test1

no run
```

### Hello World

Após criar o projeto, edite `main.no`:

```no
print('Hello, World!')
```

Executar:

```bash
no run
```

Saída:

```
Hello, World!
```

### Regras de entrada

- **main.no** — ponto de entrada do programa
- **lib.no** — ponto de entrada da biblioteca, exporta funções
- **Todos os arquivos .no no diretório tests/** — contêm asserções de teste

### Notas sobre testes

- Os arquivos de teste são colocados uniformemente no diretório tests/
- Cada arquivo de teste é compilado independentemente
- Se qualquer teste falhar, um código de saída diferente de zero é retornado

```bash
no vet src/std                     # Nolang vet verifica a biblioteca padrão
```

## Compilar a partir do código-fonte

O Nolang deve ser compilado com `make`:

```bash
make            # compila todos os alvos (bin/no e vscode-nolang/server/lsp)
make no         # compila bin/no
make lsp        # compila o servidor LSP
make package    # compila o LSP e empacota a extensão VSCode
make no-wasm    # transpila no para WebAssembly
make lsp-wasm   # transpila o LSP para WebAssembly
make playground # compila no.wasm + lsp.wasm + o site Docusaurus
make clean      # limpa os artefatos de compilação
make help       # exibe todos os alvos de compilação
```

> ⚠️ **O Nolang deve ser compilado com `make`; nunca compile diretamente com `cd src && go build`.**
>
> `make` executa automaticamente `make gen` antes da compilação, regenerando `src/checker/stdsig_gen.go`
> — uma tabela de assinaturas da biblioteca padrão **incorporada no binário** (`funcSigs` / `methodSigs` /
> `funcParams` / `methodParams` / `structFields` / `aliases` / `structMod` /
> `enumVariants` / `embeddedStdModules`). Com ela, cada `no build` ignora o
> PASS1 de "analisar todos os std", tornando a inicialização bem mais rápida.
>
> Se você modificou `src/std` mas executou `go build` diretamente **sem** antes rodar `make gen`, o compilador
> detectará uma inconsistência entre a tabela de assinaturas incorporada e o conteúdo de `src/std` e
> **rejeitará a compilação com um erro imediato** (pedindo que você execute `make no`). Isso é intencional: antes, uma
> degradação silenciosa para análise completa tornava cada compilação mais lenta e difícil de perceber. Após modificar `src/std`, execute sempre `make no` (que roda `make gen` primeiro e depois compila).
>
> Execute os testes também com `make test`: **o `go test` direto falha e é rejeitado porque a chave de assinatura não foi injetada (`StdSigKey` está vazia)**. O `make test` injeta a mesma chave ao compilar o binário de teste, fazendo com que ele use o caminho rápido da tabela de assinaturas incorporada.

---

## Glossário (glossary)

`glossary/` é uma **base de conhecimento do Nolang organizada em verbetes**: cada termo é um registro com tipo, definição, sintaxe, exemplo, referências relacionadas e um resumo de uma linha. São **244 verbetes / 10 domínios semânticos** (tipos, sintaxe, anotações, memória, erros, módulos, internals do compilador, toolchain, todos os módulos std, armadilhas conhecidas); os dados em si estão no formato `.noml`.

O frontend é escrito em Nolang e oferece tanto CLI quanto MCP (stdio / HTTP), para que humanos e IA possam consultar:

```bash
no build glossary                        # saída: glossary/dist/glossary

./glossary/dist/glossary get option      # obtém o texto completo de um único verbete
./glossary/dist/glossary search 三態      # busca por palavra-chave
./glossary/dist/glossary list --domain=traps   # lista de armadilhas conhecidas
./glossary/dist/glossary check           # verificação de consistência (para CI)

./glossary/dist/glossary mcp             # servidor MCP (stdio)
./glossary/dist/glossary mcp --http=127.0.0.1:8765
```

A saída pode ser alternada com `--format=text|md|json`. Veja [`glossary/README.md`](glossary/README.md) para instruções de integração e formato dos dados.

---

## Projetos relacionados

- [Benchmarks Game — relatório de comparação de desempenho Nolang vs C vs Rust](https://github.com/lizongying/no-benchmarks/results/report.md)

- [notools — conjunto de ferramentas de linha de comando Unix implementado em Nolang](https://github.com/lizongying/notools)

  Caso de uso do Nolang: um conjunto de ferramentas de linha de comando Unix comuns implementado na linguagem Nolang, incluindo ferramentas clássicas como cat, ls, grep, wc, head e tail, demonstrando a capacidade prática do Nolang na área de programação de sistemas.

---

## TODO

## Licença

Apache-2.0
