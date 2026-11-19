---
sidebar_position: 4
---

# Estilo de código

## Nova linha no fim do ficheiro (Trailing Newline / EOF)

Todo o ficheiro de origem `.no` **não vazio** **deve terminar com exatamente uma nova linha no fim** (ou seja, o ficheiro termina com uma única quebra de linha).

- O ficheiro **não tem** nova linha no fim → adiciona uma.
- O ficheiro termina com **várias** linhas em branco → reduz a uma única nova linha no fim.
- **Ficheiro vazio** (0 bytes) → mantém-se inalterado.

Diretórios excluídos: `dist/`, `vscode-nolang/`, `node_modules/`.

Justificação: uma única nova linha EOF consistente evita que o `git diff` mostre "no newline at end of file", torna as diferenças mais limpas e torna previsível a concatenação e o encadeamento de ferramentas.

### Aplicação automática (incorporada no conjunto de ferramentas)

Esta regra é aplicada **automaticamente** pelas ferramentas — não existe um script separado:

- **`no fmt`**: o subcomando `fmt` de `src/cmd/no/main.go` formata os ficheiros no local e chama `fmt.FormatFile`, garantindo exatamente uma nova linha no fim.
- **Formatação ao guardar no LSP / `textDocument/formatting`**: `formatNolangCode` em `src/lsp/server.go` → `fmt.FormatFile` adiciona ou reduz automaticamente a nova linha EOF ao guardar ou formatar um ficheiro `.no` no editor.

A implementação está em `src/fmt/formatter.go`:

- `FormatFile(code)` formata um ficheiro completo e chama `ensureTrailingNewline`, que remove todos os `\r` / `\n` finais (compatível com CRLF e múltiplas linhas em branco no fim) e adiciona um único `\n`. Entradas vazias ou que não podem ser analisadas são devolvidas tal qual, para evitar estragar o ficheiro.
- `Format(code)` é a função de formatação de fragmentos puros (sem nova linha no fim), usada sobretudo por testes unitários; ao escrever um ficheiro de origem real, usa antes `FormatFile`.

## Arranjo automático de instruções de importação (deduplicação / remoção de `# std/fmt` / ordenação)

O `no fmt` (e a formatação ao guardar no LSP) normaliza automaticamente o **bloco contíguo de importações no topo de um ficheiro** (`# path[.fn] [alias]`); a implementação está em `src/fmt/imports.go`:

1. **Deduplicação**: importações exatamente iguais (caminho, nome de função e alcunha idênticos byte a byte) são reduzidas à primeira ocorrência.
2. **Remoção automática de `# std/fmt`**: `print` / `eprint` / `format` / `sprintf` são funções incorporadas do compilador e os módulos da biblioteca padrão podem ser carregados implicitamente (ver [Prefixos de chamada entre módulos](./module)); portanto `# std/fmt` nunca é necessário e é removido pelo formatador. Há duas exceções que o preservam:
   - foi escrita uma **alcunha** ou uma **importação de função** (por exemplo `# std/fmt.sprintf fm`) — uma ligação de nome deliberada;
   - a linha de importação tem um **comentário** ou uma anotação `#{...}` anexa — o conteúdo escrito pelo utilizador nunca é descartado silenciosamente.
3. **Ordenação automática**: as importações são ordenadas de forma estável por ordem lexicográfica caminho → nome de função → alcunha.

Limite conservador: se qualquer importação do bloco tiver um comentário (linha própria acima ou no fim da mesma linha) ou uma anotação `#{...}` anexa (por exemplo a política de ligação nativa `#{bind=ffi}`), o bloco inteiro **mantém a ordem original e nada é removido** — ordenar separaria a correspondência entre comentário e instrução, e remover levaria o comentário consigo. Nesse caso, os duplicados exatos sem conteúdo anexo continuam a ser deduplicados.

Blocos de importação que aparecem fora do topo do ficheiro (depois de código) são mantidos tal qual.

```no
#{fmt-skip}
; antes                              ; depois
# std/vec                            # std/io
# std/fmt                            # std/vec
# std/io
# std/fmt        ;→  print/format são incorporadas, # std/fmt removido; o resto ordenado lexicograficamente
```

## Marcadores de comentário (Comment Markers)

O Nolang suporta dois marcadores de **comentário de linha única** e um marcador de **comentário multilinha (de bloco)**, com a seguinte semântica:

- `//` — marcador tradicional de linha única (comenta até ao fim da linha)
- `;` — marcador alternativo de linha única (implementado a 2026-07-17, comenta até ao fim da linha)
- `;; ... ;;` — comentário multilinha (de bloco) (implementado a 2026-07-18, delimitadores simétricos; se não for fechado, o comentário vai até ao fim do ficheiro)

```no
#{fmt-skip}
// Isto é um comentário
; Isto também é um comentário, com o mesmo significado
x = 1 ; comentário inline, até ao fim da linha

;;
   Isto é um comentário multilinha (de bloco)
   Pode abranger várias linhas
   Terminando com ;; ;;
y = 2 ;; comentário de bloco inline ;;
```

**Delimitação de bloco**: `;;` abre e `;;` fecha; tudo o que estiver entre eles (incluindo novas linhas) é conteúdo do comentário. Um único `;` no interior não fecha o bloco — só `;;` fecha. Se não for encontrado um `;;` de fecho, o comentário vai do `;;` de abertura até ao fim do ficheiro. O `;;` de abertura só entra em modo multilinha quando **não há mais nada além de espaços em branco depois dele nessa linha** — havendo texto depois do `;;` na mesma linha, é um comentário comum de linha única (por exemplo `;; nota` comenta apenas essa linha, e a linha seguinte continua a ser código). O delimitador de fecho funciona da mesma forma: só podem seguir-se espaços em branco ao `;;`.

**O formatador preserva o marcador original**: o `no fmt` nunca altera nenhum marcador de comentário — `;` continua `;`, `//` continua `//`, `;; ... ;;` continua `;; ... ;;`. O formatador regista o marcador original através de `Comment.Marker` e emite-o tal qual; o conteúdo do comentário de bloco (incluindo novas linhas internas) também é preservado palavra por palavra, pelo que a formatação é idempotente.

**Âmbito seguro**: quando `;` / `;;` aparecem dentro de literais de string (por exemplo `'text/plain; charset=utf-8'`, `index-from(';', pos)`) ou dentro de comentários `//`, os analisadores de string e de comentário do lexer consomem-nos primeiro, pelo que nunca são tratados como marcadores de comentário.

**Atenção a `cond -> X; Y`**: como `;` é agora um comentário, `cond -> X; Y` é analisado como "executar `cond -> X` (avalia X e descarta)" mais um comentário inline `; Y` — **X nunca é atribuído**. A forma correta é `cond -> X = Y` (padrão já estabelecido na stdlib; ver `arr.no`, `uuid.no`, `path.no`, `err.no`, `assert.no`). Se encontrares `cond -> X; Y` no código, muda-o para `cond -> X = Y`.

**É proibido escrever várias instruções numa só linha**: `;` é apenas um marcador de comentário e não pode juntar várias instruções na mesma linha; a vírgula `,` também não pode juntar instruções. Cada instrução dentro de um bloco `{ ... }` deve ocupar a sua própria linha. Compactar um bloco numa única linha é o erro mais perigoso:

```no
#{fmt-skip}
; ❌ Errado: `; return }` passa tudo a comentário, pelo que o `}` do bloco desaparece
cond -> { print('X'); return }
```

```no
; ✅ Correto: cada instrução ocupa a sua própria linha
cond -> {
    print('X')
    return
}
```

`cond -> { print('X'); return }` deixa na prática apenas `cond -> { print('X')`: o bloco nunca fecha, e todas as instruções depois do `}` perdido (normalmente o resto do corpo da função, por vezes até as funções seguintes ao nível do módulo) são engolidas. O compilador pode nem chegar a reportar erro, e o diagnóstico pode apontar muito longe do erro real.

## Literais booleanos e simplificação de `== true` / `== false` (implementado a 2026-09-24)

Além das palavras-chave `true` / `false`, o Nolang aceita duas **abreviaturas de literal booleano**:

- `!!` — um literal autónomo equivalente a `true`.
- `!` — um literal autónomo equivalente a `false` (apenas quando o token a seguir a `!` não pode iniciar um operando; se `!` for seguido de `nova linha` / `;` / fim de ficheiro / `)` / `}` / `]` / `->`, é analisado como `false`).

> Nota: aqui falamos de `!!` / `!` em **posição de expressão**, o que é diferente das formas em posição de loop `!! { }` (sempre executar) e `! { }` (nunca executar).

Ambas as abreviaturas se tornam um `BooleanLiteral` comum **no momento da análise**, pelo que as quatro formas `f == true`, `f == !!`, `f == false`, `f == !` são sintaticamente equivalentes e percorrem a mesma lógica.

O tratamento de booleanos pelo `no fmt` (implementado em `src/fmt/expr.go`):

- **Literais autónomos são normalizados para a grafia de palavra-chave**: `c = !!` → `c = true`, `c = !` → `c = false`.
- **O conhecido simplifica, o desconhecido não**: quando um dos lados de `==` / `!=` é um literal booleano e o outro
  operando **pode ser provado como valor bool**, o formatador emite a forma simplificada. Os quatro pipelines partilham a mesma
  **regra de recolha de assinaturas** (`collectModuleSignatures`: tabelas std incorporadas, módulos locais apontados por
  `use` / `#`, EnumVariants) e o mesmo contrato de injeção `SemRebuildConfig.Apply`; a diferença está apenas no fornecedor:
  `no fmt` / LSP usam `build.FmtSigLoader`, enquanto `no build` / `no vet` ainda passam por
  `preloadModuleSignatures` + `astParseConfig` (os dois loaders ainda não foram fundidos).
  O `no fmt` injeta antes da análise fresca `SkipLowering`; o LSP injeta em `ParseDocument`
  e publica um `parseSnapshot`, reaproveitando na formatação ao guardar uma cópia profunda dessa AST sintática
  (`fmt.CloneProgramForFormat`), sem análise extra nas definições predefinidas. Há três fontes que contam como prova:

  1. Anotação de tipo explícita: `b bool = true`, parâmetro `(b bool)`;
  2. Atribuição de literal booleano: `flag = true` / `flag = false`;
  3. Chamada a uma função cujo resultado declarado é `bool`: **definida neste ficheiro** (por exemplo `bytes-eq = ... (eq bool)`),
     ou função std / de módulo local que conste da tabela de assinaturas (por exemplo `args.has-flag`, ou
     `helper.is-ok` de um módulo local). Uma chamada de método num recetor variável (`s.starts-with('a')`) é
     resolvida através do tipo do recetor já inferido por Sem (`s: str`) consultando `str.starts-with`; se o tipo
     não tiver sido inferido, ou se o mesmo nome de variável tiver sido inferido como tipos diferentes em scopes
     diferentes, continua desconhecido (deliberadamente não se adivinha "nome de método globalmente único", para não
     confundir `?bool` com bool puro). Se a tabela de assinaturas não resolver o tipo de resultado, continua desconhecido.

  | Grafia original (o `b` abaixo é uma variável "conhecida") | Após formatação |
  | ---------------------------------------------------------- | --------------- |
  | `b == true` / `b == !!`                                    | `b`             |
  | `b == false` / `b == !`                                    | `! b`           |
  | `b != true` / `b != !!`                                    | `! b`           |
  | `b != false` / `b != !`                                    | `b`             |
  | `true == b` (literal à esquerda)                           | `b`             |

  `!=` apenas inverte a polaridade; `true == false` (ambos os lados são literais) mantém-se tal qual.
  Um nome nu sem prova de bool, como `f == true`, também se mantém tal qual.
- **`?T` e todos os operandos desconhecidos mantêm a comparação**: opções (`?T`), chamadas entre módulos cujo tipo de
  resultado a tabela de assinaturas não resolve, nomes nus sem atribuição, acesso por ponto com tipo de recetor não
  inferido, expressões de chamada e resultados de operações mantêm todos `== true` / `== false`. Reduzir um deles a
  `x` / `! x` nu como condição de controlo de fluxo faz o MIR bater num crash de codegen "option-as-i1" — a v0.3.3
  revertou a regra inteira exatamente por isso (`8a8ecb59`); a 2026-11-15 voltou a ativá-la sob a regra de "prova
  positiva apenas", deixando de usar a inferência inversa de "se não se vê `?T`, então é um valor". A partir do mesmo
  dia, o `no fmt` / LSP também injetam assinaturas entre módulos, pelo que resultados option como
  `fs.write-str(...)` (`?bool`) ficam **visíveis** e são travados pelo bloqueio rígido de option, em vez de depender
  de "não se ver o tipo". Uma option é um **bloqueio rígido independente**
  (`boolOperandIsOptionTyped`), verificado antes da prova de valor bool, pelo que nunca é simplificada quer a anotação
  de tipo se mantenha ou não no ficheiro: tanto `ok ?bool = fs.write-str(...)` como
  `ok = fs.write-str(...)` mantêm a comparação. O `no fmt` remover anotações de tipo redundantes segundo a regra
  existente (`fixRedundantTypeSource`) é **comportamento correto** — não desatives essa remoção para preservar a
  deteção de option. Testes de regressão: `src/fmt/bool_comparison_test.go`,
  `src/fmt/signatures_test.go`, `src/lsp/fmt_gate_test.go`.
- **Também se aplica em braços `->` / condições if-then autónomas** (para variáveis conhecidas):
  `b == ! -> return` → `! b -> return`.
- **Segurança de precedência**: a negação escreve-se `! ` (com um espaço) para seguir a convenção existente de
  operadores de prefixo, garantindo que o `no fmt` é idempotente. Atualmente todos os operandos prováveis são
  identificadores e `!` não se reassocia; a implementação mantém ainda a proteção de parênteses para operandos
  não atómicos (`! (a + b)` em vez de `! a + b`), para o caso de a regra de prova vir a ser alargada.

## Inicialização a zero diferida de valores de retorno (Deferred Zero-Init for Return Values)

As funções do Nolang usam uma estratégia de **inicialização a zero diferida** para os seus parâmetros de resultado com nome (parâmetros out): o prólogo da função **não** faz qualquer inicialização a zero dos parâmetros out. Em vez disso, o compilador usa um bitmap `%__ret_init_bitmap` para acompanhar se cada parâmetro out foi explicitamente atribuído em algum ponto do corpo da função. Quando a função retorna, qualquer parâmetro out **que não tenha sido explicitamente atribuído** recebe automaticamente um store do valor zero correspondente ao seu tipo (inteiro → `0`, str-long → `zeroinitializer`, struct → `zeroinitializer`, option → `nil`). Este mecanismo é simétrico com o mecanismo existente de liberação diferida `%__move_bitmap`.

### Porquê este desenho

- **Desempenho**: evita que o prólogo emita um `store zeroinitializer` para cada parâmetro out apenas para este ser depois substituído por uma atribuição no corpo — um store puramente redundante. A estratégia diferida só escreve zero quando "realmente não houve atribuição".
- **Legibilidade**: não é preciso escrever no início da função código repetitivo de atribuição antecipada a zero como `found = false` / `result = nil`; o leitor vê diretamente a intenção de "só o caminho de sucesso atribui".
- **Consistência**: usa o mesmo padrão de acompanhamento por bitmap do mecanismo de liberação diferida `%__move_bitmap`, mantendo a implementação interna do compilador simétrica.

### Estilo recomendado

Não faças atribuição antecipada a zero dos parâmetros out no início da função — o compilador preenche o zero no retorno. Escreve apenas as atribuições do "caminho de sucesso":

```no
; Good：sem atribuição antecipada a zero, o compilador trata disso
hashmap-str-tmpl.contains = (key str) (found bool) {
    val ?v = .get(key)
    val: {
        ok -> found = true

        err -> {
        }

        nil -> {
        }
    }
}
```

### Anti-padrão (evitar)

Não escrevas atribuições antecipadas a zero redundantes — são stores mortos substituídos por uma atribuição posterior:

```no
#{fmt-skip}
; Bad：atribuição antecipada a zero redundante
hashmap-str-tmpl.contains = (key str) (found bool) {
    found = false              ; redundante — o compilador preenche zero no retorno
    val ?v = .get(key)
    val: {
        ok -> found = true
        err -> {}
        nil -> {}
    }
}
```

### Option predefine nil

Para parâmetros out do tipo option `?T`, o compilador escreve diretamente `nil` (tag=1, data=0) ao preencher o zero, equivalente a `result = nil`. Assim, caminhos como "não encontrado / conjunto vazio / EOF" precisam apenas de um `return` nu, sem escrever `result = nil` no código:

```no
#{fmt-skip}
; Good：o caminho de falha usa return nu, o compilador preenche result = nil
hashmap-str-tmpl.get = (key str) (result ?v) {
    .size == 0 -> return        ; o compilador preenche result = nil
    ...
    ; fall-through：o compilador preenche result = nil
}
```

### Dica de depuração

Se uma função retornar um valor zero inesperado (por exemplo `found` devia ser `true` mas dá `false`, ou `result` devia ter um valor mas é `nil`), verifica se **todos os caminhos que deviam retornar um valor não nulo** fazem uma atribuição explícita ao parâmetro out no corpo da função. O compilador não adivinha intenções — apenas preenche com zero os parâmetros out não atribuídos. Consulta a dica de depuração em `.agents/skills/nolang-debug/SKILL.md`.

## Ligação direta de slot de saída a variável existente (evitar variável intermédia)

**Estilo recomendado**: ao escrever código novo, se o resultado de uma chamada serve apenas para atualizar uma variável já existente, liga o slot de saída **diretamente a essa variável**,
sem criar uma variável temporária usada uma única vez. Isto é especialmente comum quando há várias saídas e o recetor é ele próprio o estado a atualizar.

```no
; ✅ Recomendado：o resto escrito diretamente em r (r é também o recetor do método)
qsq, r = r.div-mod(modulus)
```

```no
; ❌ Evitar：msq só é escrito uma vez, sem outra utilidade
qsq, msq = r.div-mod(modulus)
r = msq
```

- Usa `_` como marcador de posição para saídas não usadas: `_, r = r.div-mod(modulus)` não introduce sequer uma nova variável.
- É legal uma referência de entrada só de leitura e um slot de saída apontarem para o mesmo objeto (regra de aliasing); o `r = r._mul(r)` em `src/std/bigint.no` é exatamente essa atualização in-place, aplicando-se também a saída única.
- Esta é uma recomendação para **código novo**, não uma ordem de refatoração: código válido existente não deve ser reescrito por estilo (ver a〈Regra de ouro〉).
- Para a semântica completa, resultados medidos e âmbito de aplicação, consulta [Referência da linguagem: definição de funções](./syntax).

## Blocos de código Markdown (`no fmt -md`)

O `no fmt -md` formata apenas o **interior** das cercas ```` ```no ```` / ```` ```nolang ````; as linhas de cerca e todo o restante Markdown são preservados byte a byte. O conteúdo de um bloco é analisado como código-fonte Nolang; se não puder ser analisado, o bloco fica tal qual e o número da linha é reportado, pelo que a formatação nunca estraga um ficheiro que não consegue compreender. (`-md` e `-fix` são mutuamente exclusivos — `-md` apenas formata.)

### Exemplos corretos e errados devem estar em blocos separados

Colocar "a forma correta" e "a forma errada" no mesmo bloco faz o analisador tratar ambos como um único programa: a metade correta perde a verificação do `no fmt`, e a metade errada pode ser "corrigida" silenciosamente pelo formatador até o erro ficar invisível. Portanto:

- um **exemplo correto** fica sozinho num bloco sem anotação, para que o `no fmt` o formate e valide;
- um **exemplo errado** fica sozinho num bloco cuja primeira linha é `#{fmt-skip}`, pelo que não é nem validado nem modificado.

```no
; ✅ Correto：cada instrução ocupa a sua própria linha
cond -> {
    print('X')
    return
}
```

```no
#{fmt-skip}
; ❌ Errado：tudo depois de `;` é um comentário, pelo que o `}` do bloco desaparece
cond -> { print('X'); return }
```

### As três utilizações legítimas de `#{fmt-skip}`

1. **Exemplos errados / contra-exemplos** — não são programas válidos para começar; formatar só atrapalha.
2. **Pseudocódigo e resumos de assinaturas** — fragmentos apenas com a forma, sem implementação (por exemplo o esqueleto de assinatura `f = (a i64) (r i64) { ... }`).
3. **Folhas de consulta com colunas de comentário alinhadas** — o `no fmt` reescreve `code      ; nota` como `code; nota`, e o alinhamento das colunas é exatamente o que dá legibilidade a esse tipo de tabela.

### Nunca anotes apenas para impedir a expansão

O `no fmt` expande um bloco vazio `{ }` e um braço de uma linha `-> { x() }` para várias linhas:

```no
ok = (b bool) (yes bool) {
}

dispatch = (cmd str) {
    cmd == 'a' -> {
        fa()
    }
}
```

Esta é a forma **canónica** que deve ser aceite — só depois da expansão é que o bloco tem uma linha própria onde inserir instruções seguintes; caso contrário, cada instrução adicionada exige primeiro dividir o bloco manualmente. Da mesma forma, `true -> {}` passar a `true -> {\n}` não é motivo para reverter. Só os três casos acima (contra-exemplos, pseudocódigo, tabelas alinhadas) necessitam realmente de `#{fmt-skip}`.

### Blocos só de comentário não precisam de anotação

Um bloco que contém apenas comentários (a forma habitual de "comentar" a grafia correta) é analisado como **zero instruções**. O `no fmt` trata isso como "nada a formatar", deixa-o tal qual e não reporta nada — não é um erro de sintaxe, e acrescentar `#{fmt-skip}` apenas esconderia esse facto.

### Ordem: anota primeiro, formata depois

O `no fmt -md -w` reescreve no local todos os blocos que não foram filtrados. Uma vez que o formatador "corrija" um exemplo errado, o contra-exemplo deixa de demonstrar alguma coisa e a grafia original perde-se para sempre. **Acrescenta `#{fmt-skip}` primeiro, executa `-w` depois.**
