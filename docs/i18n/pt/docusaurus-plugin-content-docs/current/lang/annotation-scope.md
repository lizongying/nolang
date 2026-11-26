---
sidebar_position: 2.5
---

# Âmbito de anotações e comentários

Uma anotação `#{...}` não contém informação sobre "quão grande é o seu alcance" — o âmbito é determinado pelo **nó ao qual está anexada**.
Esta página fixa isso: uma posição, um conjunto de decisões, quatro níveis, mais uma ressalva ("uma chave pode suportar apenas alguns desses níveis" — `overflow` é a que só aceita L3, ver §4.1), de modo que o autor, o `no fmt`, o `no vet`, o codegen e o LSP
dêem todos a mesma resposta para a mesma linha de anotação.

> Porquê que isto tem de ser fixado: se o `no vet` considerar que um `#{overflow=wrap}` cobre a função inteira e o codegen considerar que cobre apenas a linha seguinte, surge uma deriva semântica silenciosa — "vet tudo verde, runtime continua a devolver `err`".
> O âmbito não é uma propriedade da anotação; é **o entendimento partilhado dos leitores**, e um entendimento partilhado que não é escrito deriva.

---

## 1. Um princípio: o anfitrião decide o âmbito

Uma anotação tem exatamente duas posições legais (as regras de posição em si estão em [`syntax.md`](syntax.md)〈Posições de anotação〉):

```no
; ① acima：numa linha própria, colocada acima do alvo
#{overflow=wrap}
x i8 = a + b

; ② à cauda：escrita na mesma linha, depois do alvo
x i8 = a + b #{overflow=wrap}

; Em alvos multilinha (funções, blocos), a forma de cauda vai depois do `}` de fecho
greet = () {
    print('hi')
} #{mac-arm64}
```

**As duas são completamente equivalentes** — a mesma chave, o mesmo anfitrião, o mesmo âmbito, e o `no fmt` nunca reescreve uma na
outra (a cauda fica na cauda, a de cima fica em cima). A escolha depende apenas da legibilidade: em instruções curtas, a forma de cauda poupa uma linha;
quando se pretende anotar uma expressão inteira ou a instrução é longa, usa-se a forma de cima.

**Escrevê-la na mesma linha *antes* do alvo é erro de compilação**, não existe o caminho "prefixo aplica-se ao que vem a seguir":

```no
#{overflow = wrap} x i8 = a + b   ; ✗ prefixo → erro
```

A decisão dá-se em três passos, cada um com uma única resposta:

| Passo | Pergunta | Origem da resposta |
| --- | --- | --- |
| 1 | Quem é o anfitrião? | Posição: a primeira instrução não-comentário/não-anotação acima, ou a instrução precedente na mesma linha |
| 2 | A que nível pertence o anfitrião? A chave suporta esse nível? | A tabela de §2 + a matriz de §4 (não suportado = erro de compilação) |
| 3 | Até onde chega? | A subárvore do anfitrião, menos as fronteiras rígidas internas (§3) |

---

## 2. Os quatro níveis

| Nível | Anfitrião | Âmbito efetivo | Exemplo de grafia |
| --- | --- | --- | --- |
| **L0 nível de módulo** | Cabeçalho do ficheiro | Todas as declarações e instruções deste ficheiro | No topo absoluto do ficheiro, com uma linha em branco entre ele e o conteúdo seguinte |
| **L1 nível de declaração** | Constante/variável global, struct, interface, enum tagged, função, método | Toda a definição dessa declaração (a função inclui o corpo e todos os blocos aninhados) | Imediatamente acima da declaração, ou à cauda da linha da declaração |
| **L2 nível de bloco** | Grupo de corrotinas, grupo if, `cond -> body`, match, loops `for`/contado/intervalo, bloco nu | Todas as instruções dentro desse bloco (incluindo blocos aninhados, até ao limite L1 interno) | Imediatamente acima da instrução de cabeçalho do bloco, ou depois do `}` de fecho do bloco |
| **L3 nível de linha única** | Uma única instrução (`let`, atribuição, `?=`, `return`, chamada, braço `cond ->`) | A própria instrução e as suas expressões | Imediatamente acima da instrução, ou à cauda da mesma linha |

### L0 nível de módulo

A posição é **no início absoluto do ficheiro** (sem nenhuma instrução ou import antes) e o grupo de anotações deve ter
**pelo menos uma linha em branco** entre ele e a primeira instrução — a linha em branco funciona aqui como "terminador de âmbito", indicando que este grupo de anotações não pertence à instrução seguinte,
mas a todo o ficheiro.

```no
#{win-amd64}

; ↑ linha em branco → nível de módulo：este ficheiro só compila em Windows x86_64
; Todas as declarações e instruções seguintes estão sob jurisdição
helper = () {
    print('windows only')
}

label = 'win'
```

Sem linha em branco, é um L1 normal (anexado à primeira declaração); esta fronteira permite que o código existente não precise de migração:

```no
#{buildin}
bool.to-str = () (out str) {
}
```

O nível de módulo só é útil para chaves que "fazem sentido para o ficheiro inteiro" (atualmente apenas chaves de plataforma); chaves de declaração (`buildin`, `generic`,
`embed`…) em L0 são erro, não "ampliação de âmbito". `overflow` também não aceita L0 — só reconhece L3.

Configurações entre ficheiros não pertencem às anotações, escrevem-se em `package.noml`.

### L1 nível de declaração

```no
; Estrutura inteira：o layout dos campos é decidido pela marcação de tipo
#{inline}
holder {
    inl point
    out point
}

; Função inteira：o registo de entrada/saída inclui automaticamente o chamador
#{track-caller}
log.write = (msg str) {
    ...
}

; Declaração de constante inteira：diretório incorporado (caminho relativo a package.noml)
#{embed='assets'}
ASSETS fs.embed

; Eliminação em tempo de compilação de toda a subárvore (incluindo o corpo da função)
#{mac-arm64, linux-arm64}
greet = () {
    print('hello from unix')
}
```

Dentro de L1 há ainda **granularidade de membro**: anotações em campos de struct, variantes de enum tagged e membros de enum ao estilo C
(`a pt #{inline}`, `ok(v i64) #{inline}`) governam esse membro, não o tipo inteiro. As anotações de membro
também só têm duas posições: imediatamente acima do membro, ou na mesma linha dele (a cauda é a forma usual).

### L2 nível de bloco

Quando o anfitrião é a "instrução de cabeçalho de bloco", o âmbito é o corpo inteiro do bloco:

```no
; Bloco inteiro excluído (não gera IR, nem faz verificação de tipos)
#{win-amd64}
2 * {
    print('windows only')
}

; Um grupo if também é tratado como um todo
x = 1
#{win-amd64}
{
    x == 1 -> print('arm-1')
    -> print('arm-other')
}
```

Os quatro tipos de cabeçalho de bloco aplicam-se todos: grupo de corrotinas (`awy { ... }`), grupo if (`{ cond -> … }`), instrução de condição única
(`cond -> body`), match e loops `for` / contado / intervalo.

Se o corpo de `cond -> body` for apenas uma instrução, L2 e L3 dão o mesmo resultado; só com a forma de bloco é que há diferença.

**Nem todas as chaves consomem os quatro níveis.** Uma chave pode suportar apenas alguns deles e escrevê-la num nível que não suporta é **erro de compilação**,
não "redundância inofensiva de uma linha a mais". Atualmente a única chave restrita é `overflow`: só reconhece L3, pelo motivo em §4.1.

### L3 nível de linha única

```no
a i8 = 100
b i8 = 100
; Governa apenas a instrução seguinte imediata (a forma de cauda `c = a + b #{overflow=wrap}` é equivalente)
#{overflow=wrap}
c = a + b
c: {
    ok -> print('wrap:' - (it == -56).to-str())
    nil -> print('nil')
    err -> print('err')
}
```

Dentro de um bloco, uma anotação isolada numa linha reconhece apenas "a instrução que vem imediatamente a seguir". Linhas de comentário alheias pelo meio
(por exemplo, outro `#{...}` depois de `#{overflow=wrap}`) não interrompem — continua a encontrar a primeira instrução real;
mas se a anotação seguinte também tiver a mesma chave, a primeira não governa nenhuma instrução (a segunda prevalece).

⚠️ **Um block match `{ … }: { … }` é "uma instrução" e não é atravessado**: quando a anotação é escrita acima do seu `{`,
o anfitrião é a própria instrução de bloco e **não** desce até à expressão `?=` dentro do bloco (medido: com `#{overflow=wrap}` fora do bloco,
`np ?= np + 1` dentro do bloco continua a transbordar → `err` e o alvo é zerado). Para aplicar `overflow` a uma expressão dentro do bloco, escreve-se
imediatamente acima daquela instrução `?=` dentro do bloco. A diferença entre as duas formas de consumo está em [syntax.md](syntax.md)
〈Escolha entre block match e value-position match〉.

---

## 3. Herança, substituição e fronteiras rígidas

**Herança de fora para dentro, substituição pelo mais próximo.** Para chaves que suportam vários níveis (chaves de plataforma), L0 → L1 → L2 → L3 aplicam-se nível a nível;
se a mesma chave aparecer no mesmo nó, a última substitui a primeira (`#{mac-arm64} #{mac-arm64, win-amd64}`
prevalece a última).

**O corpo da função é uma fronteira rígida.** O âmbito de um bloco L2 termina na **subárvore sintática**: não se propaga por relações de chamada nem entra
no corpo de qualquer função / método / função anónima. O padrão usado dentro do corpo de uma função é decidido apenas pela sua própria anotação L1 ou por
anotações L3 internas, independentemente da anotação no bloco que a chama.

```no
; L1：a função inteira, com o corpo, é excluída (não gera IR no macOS)
#{win-amd64}
windows-only = () {
    print('never compiled on mac')
}

; Sem anotação L1 própria → compila em qualquer plataforma, e não muda por quem a chama
always = () {
    print('always compiled')
}
```

Motivo: se uma anotação externa pudesse penetrar no corpo da função, uma linha no local de chamada mudaria silenciosamente a semântica da função chamada —
algo completamente invisível ao depurar. Já a anotação L1 é diferente: está escrita no cabeçalho dessa função e faz parte da declaração.

**Lista de fronteiras rígidas** (a herança para aqui): corpo de função, corpo de método, corpo de função anónima/lambda, declaração externa `#{c}`.
**Não fronteiras** (herdam normalmente): bloco nu `{}`, grupo if, braço de match, corpo de loop.

---

## 4. Matriz chave × nível

`✓` efetivo ／ `✗` ilegal (deve reportar erro de compilação) ／ `—` sem sentido para esse nível (também deve dar erro, em vez de ser silenciosamente descartado)

| Chave | L0 módulo | L1 declaração | L2 bloco | L3 linha |
| --- | --- | --- | --- | --- |
| `overflow` | ✗ | ✗ | ✗ | ✓ |
| Chaves de plataforma (`mac-arm64` etc.) | ✓ | ✓ | ✓ | ✓ |
| `buildin` | ✗ | ✓ função / enum tagged / struct de layout | ✗ | ✗ |
| `inline` | ✗ | ✓ struct / campo / variante / função | ✗ | ✓ cauda de membro |
| `generic` | ✗ | ✓ função / struct | ✗ | ✗ |
| `track-caller` | ✗ | ✓ função | ✗ | ✗ |
| `embed` | ✗ | ✓ `let` de nível superior | ✗ | ✗ |
| `bind` | ✗ | ✓ linha de import | ✗ | ✗ |
| `c` / `cpp` / `rust` | ✗ | ✓ declaração de função seguinte | ✗ | ✗ |
| `fmt-skip` | ✗ | ✗ | ✓ bloco cercado markdown | ✗ |
| Metadados (`derive` / `range` / `max` / `debug`…) | ✗ | ✓ declaração / membro | ✗ | ✗ |
| `index-out` | — | — | — | — removido no D11, a presença dá erro |

Tanto "ilegal" como "sem sentido" têm de **reportar erro**, não podem falhar silenciosamente: um `#{buildin}` escrito acima de um loop for que fosse
ignorado discretamente levaria o autor a acreditar que a declaração incorporada estava ativa.

### 4.0 Os três suportes de `#{buildin}` e a especificidade do "struct de layout"

`#{buildin}` é marcado numa declaração, mas o seu significado é completamente diferente para três tipos de suporte:

| Suporte | Significado | Exemplo |
| --- | --- | --- |
| Função | Stub de corpo vazio: o compilador / FFI fornece a implementação, o lado Nolang não gera corpo de função | `#{buildin}`<br/>`fs.remove = (p str) (ok bool) { }` |
| Enum tagged | As variantes servem para matching e verificação de exaustividade, a representação subjacente e a construção são fornecidas pelo runtime incorporado | `#{buildin}`<br/>`option { ok(v t), nil, err(e err.err), }` |
| **Struct de layout** | **É apenas documentação de layout**: este `struct` não existe semanticamente, o único produto são os tipos dos campos (`t.len` é `u8`) | `#{buildin}`<br/>`txt { data [255]byte; len u8 }` |

O terceiro caso merece explicação à parte, porque vai no sentido oposto aos dois primeiros: o stub de função e o enum tagged são "o compilador **usa** realmente esta declaração",
enquanto o struct de layout é "o compilador **não usa** esta declaração; ela é apenas a fonte dos tipos dos campos e uma descrição legível do layout". O layout real e a ligação à documentação estão noutro sítio:

| Tipo | Origem do layout real | Origem da documentação / ligação |
| `str` | Descritor escrito à mão no MIR `%str-long = { i32 len, i32 cap, i8* data }` | `parser.BuiltinTypes()` |
| `vec` | Descritor escrito à mão no MIR `%vec = { i32 len, i32 cap, i64 data }` | `parser.BuiltinTypes()` |
| `txt` | Descritor escrito à mão no MIR `%txt = { [255 x i8] data, i8 len }` | `parser.BuiltinTypes()` |
| `arr` | Sem descritor: `[n]t` é uma agregação nua `[N x T]`, e `.len` é a constante de compilação `n` | `parser.BuiltinTypes()` |

`parser.BuiltinTypes()` (`src/parser/builtintype.go`) é o único registo "nome → módulo std → âncora de documentação do módulo → layout"; o hover / ir para definição / preenchimento do LSP e `IsBuiltinLayoutStructName` leem-no. É por isso que o struct de layout **já não assume a ligação**: eliminando-o, hover e F12 continuam a funcionar;
apenas o checker perderia o tipo do campo `t.len`, pelo que ele se mantém.

Daí resultam duas regras rígidas, qualquer uma que seja violada produz de imediato um "struct sombra" que contradiz o layout real:

1. **Um struct com `#{buildin}` tem de usar um nome de layout incorporado** (`str` / `vec` / `txt` / `arr`).
   Colá-lo noutro struct é erro: essa declaração seria ignorada pelo MIR como "layout detido pelo compilador",
   todos os campos deixariam de ter efeito e o autor ficaria com uma série de `unknown field`.
2. **Declarar um struct com um nome de layout incorporado exige `#{buildin}`**. Se o utilizador escrever um
   `str { … }` nu, este é tratado como um tipo comum do módulo, com prefixo (`m.str`), e o codegen gera
   então um `%m_str` que conflita com `%str-long`.

Ambas são reportadas por `checker.ValidateBuiltinStructDecls` (`1a8m445h` / `3lu6n4np`) e são detetadas tanto por `no build` como por `no vet`. Por outro lado, **só** a combinação `#{buildin}` + nome de layout é ignorada pela fase de prefixo de módulo e pelo `collectStructFields` do MIR (ver `src/build/module_prefix.go`
Fase 1 e `src/mir/hir2mir.go` `isBuiltinLayoutStruct`) — o critério é a combinação "anotação + nome", não o nome por si só.

### 4.1 Porquê que `overflow` só vale para L3

`#{overflow}` é um interruptor de semântica **por operação**, não metadados de uma declaração. Se fosse permitido escrevê-lo numa função ou num bloco,
"que modo usa realmente uma certa operação" passaria a depender da distância até à anotação, de haver ou não outras anotações pelo meio e de quantas camadas de bloco existem —
ler uma linha de código exigiria rastrear para cima para responder, e manter a mesma resposta entre `no fmt` / `no vet` / codegen
seria extremamente difícil (já se verificou uma deriva, ver §7).

Além disso, **não altera o tipo**: o resultado é sempre `?T`. Um `wrap` a nível de bloco ou de função não transforma `?T` em `T`, apenas troca
"transbordo = `err`" silenciosamente por "retorno circular silencioso" — o erro passa de "falha explícita" para "valor errado que parece sucesso",
exatamente o que menos deveria ser decidido por uma anotação longínqua.

Por isso fica fixado como uma anotação por linha:

- Reconhece apenas a instrução imediatamente seguinte; a forma de cauda (`x = a + b #{overflow=wrap}`) é equivalente.
- Para abranger uma área inteira, escreve-se instrução a instrução — tanto `no fmt --fix=overflow` como o quickfix do LSP geram essa forma.
- Escrito no cabeçalho da função, no cabeçalho do bloco ou no cabeçalho do ficheiro → **erro de compilação**, não "ineficaz mas inofensivo".

Benefício acessório: a anotação fica sempre no mesmo ecrã que a instrução que governa, não se separam ao mover código; e `wrap` não acaba
por cobrir acidentalmente uma operação vizinha devido a um ajuste de indentação.

---

## 5. Âmbito dos comentários (`;` / `//` / `;;`)

Os comentários não participam na semântica da compilação; a sua atribuição só afeta a formatação do `no fmt` e a fixação da documentação da API. A sintaxe dos marcadores está em
[`code-style.md`](code-style.md)〈Marcadores de comentário〉.

| Comentário | Atribuição |
| --- | --- |
| Inline (`x = 1 ; nota`) | Essa linha |
| Linhas de comentário consecutivas acima | A instrução/declaração seguinte imediata (L1 ou L3, mesma decisão de anfitrião que para as anotações) |
| Linha em branco entre o comentário e o alvo | Não atribuído a nenhum nó, funciona como nota separadora de bloco/ficheiro (exceto no cabeçalho do ficheiro, ver L0) |
| Comentário de bloco `;; … ;;` | Apenas até ao seu próprio delimitador de fecho, não atravessa âmbitos |
| Comentário entre uma anotação e o alvo | Acompanha o alvo, com ordem de saída fixa "comentário de documentação → anotação → instrução" |

`;` e `//` têm o mesmo significado; o `no fmt` preserva o marcador original sem o reescrever. A âncora de tradução de documentação
(`[get](#args.get)`) tem de ser a **última linha** do grupo de comentários, pertence ao mesmo anfitrião e não pode atravessar nós.

---

## 6. Os quatro leitores têm de concordar

Perante a mesma anotação, os quatro leitores têm de obter o mesmo âmbito:

| Leitor | O que precisa de saber |
| --- | --- |
| parser / codegen | Que modo usar neste nó e na sua subárvore |
| checker / `no vet` | O alcance da decisão "já anotado" |
| `no fmt` | A anotação é reimpressa no seu lugar, não pode ser movida para outro anfitrião |
| LSP | A posição onde o hover/quickfix insere é igual à posição onde ele realmente tem efeito |

Daqui resultam quatro invariantes:

1. **Decisão de anotação coerente**: o alcance que o codegen considera "anotado" é obrigatoriamente o mesmo que o `no vet` considera processado, e vice-versa.
2. **fmt não move**: o `no fmt` só pode ajustar posições dentro do mesmo anfitrião (acima ↔ cauda); mover para outro anfitrião equivale a alterar a semântica.
3. **Ineficácia dá erro**: quando uma anotação cai num nível sem semântica correspondente (ver `✗` / `—` em §4), reporta-se erro, não se ignora silenciosamente.
4. **Nível restrito**: a chave só tem efeito nos níveis permitidos pela matriz. Quando `overflow` é escrito em L0/L1/L2, os quatro leitores têm todos de o tratar como
   **erro** — não pode haver uma bifurcação do tipo "vet aceita, codegen não aceita" (situação atual em §7).

---

## 7. Situação atual e lacunas (medido a 2026-11-11)

Verificação item a item com `bin/no`, com a forma uniforme "somar dois `i8 = 100` e ver se o resultado é `err` ou `-56`
(wrap)"; as chaves de plataforma são verificadas com `#{win-amd64}` em macOS para ver se são excluídas.

| Nível | `overflow` (desenho: apenas L3) | Chaves de plataforma | Chaves de declaração |
| --- | --- | --- | --- |
| L0 módulo | ✗ proibido por desenho; atualmente anexa-se à primeira instrução (= L1) | Não implementado | Não implementado |
| L1 declaração | ✗ proibido por desenho; **a situação atual é uma falha**: `no vet` aceita (salta a função inteira), codegen não aceita (continua `err`) | ✓ exclui toda a subárvore | ✓ |
| L2 bloco | ✗ proibido por desenho; codegen não aceita. A cabeça de `for` / grupo if / braço de match consegue fazer o `no vet` calar-se ao longo do bloco; **um `{` nu em posição de instrução nem isso consegue** (ver abaixo) | ✗ continua a participar na compilação dentro do bloco | — |
| L3 linha | ✓ | ✓ | — |

Evidência medida:

```text
a-none.no    (sem anotação)                     → err
b-line.no    (#{overflow=wrap} acima da instrução)   → result:true  (-56，efetivo)
c-fn.no      (#{overflow=wrap} acima da função)   → err           (vet: No issues found)
d-block.no   (#{overflow=wrap} acima do for)   → err ×2        (vet: 2 hints)

p1-block.no  (#{win-amd64} + 2 * {…})      → corpo do bloco ainda executa    (não filtrado)
p2-stmt.no   (#{win-amd64} + print único)   → essa linha é excluída    (efetivo)
p4-fn.no     (#{win-amd64} + declaração de função)     → função excluída    (efetivo)
p6-ifgroup.no(#{win-amd64} + grupo if)        → braços ainda executam      (não filtrado)

; Comparação em tempo de compilação：com anotação na cabeça do for vs sem, 15w45dqk é idêntico palavra por palavra
k2.no (cabeça do for #{overflow=wrap} + corpo `x i8 = a + a`) → cannot assign ?i8 to i8
k3.no (remover essa linha, resto idêntico)                       → cannot assign ?i8 to i8
```

Porquê que um bloco nu nem consegue calar o `no vet`: os alvos anexáveis de `parseAnnotationStatement` são apenas IDENT / USE /
IN / RARROW / método de tipo array; `{` não está entre eles. Por isso, na seguinte grafia, a anotação não fica anexada ao bloco nem
à instrução dentro dele — degrada-se numa `AnnotationStatement` independente, linha morta para o parser, o codegen e
o `no vet`:

```no
#{overflow=wrap}
{
    ...
}
```

As cabeças de `for` / grupo if / braço de match começam por um identificador ou por `in`, e por isso caem exatamente nos alvos anexáveis.

Três buracos a fechar (o "só para L3" de `overflow` ainda não é imposto):

1. **`no vet` salta a função inteira ao ver `overflow` a nível de função** (curto-circuito `fn.OverflowMode == ""` de
   `walkStmtForOverflow`) → verificação toda verde, execução continua `err`.
2. **`no vet` herda para baixo ao longo do bloco** (`enclosingOverflow`) → uma linha de anotação na cabeça do bloco faz o bloco inteiro calar-se,
   mesmo que o codegen não a reconheça.
3. **parser / codegen ignoram silenciosamente `overflow` em L1/L2**, sem reportar erro → o autor não tem como saber que não teve efeito.

Inventário e destino do legado (`src/std`, `no fmt --fix=overflow` executado em três rondas de descida):

| Categoria | Antes da migração | Após 1.ª ronda | Após 2.ª ronda | Após 3.ª ronda |
| --- | --- | --- | --- | --- |
| Total de linhas de anotação `overflow` | 4580 | 4083 | 3861 | 4080 |
| Das quais são linhas mortas "não anexáveis a nenhum nó" | 330 (contadas por forma) | 87 | **0** | **0** |
| Reportadas por `no vet` mas não marcáveis | — | ~270 | 270 | **51** |

Primeira ronda (2026-11-11), variação líquida: 816 linhas de anotações de âmbito eliminadas, 325 linhas instrução-a-instrução inseridas. Analisando por A/B,
cerca de 218 das 325 linhas eram sítios já sem marcação e sem relação com a descida (seriam acrescentadas mesmo apenas inserindo, sem retirar as que faziam sombra),
e cerca de 97 linhas eram intenções de nível de bloco materializadas instrução a instrução.

A segunda ronda (2026-11-12) foi **limpeza pura**, causada por `findFixTargetNode` não descer ao corpo dos braços e não fazer "autoverificação após inserção":
381 linhas eliminadas, 159 acrescentadas, **todas linhas de anotação `#{...}`, 0 linhas de código** (`diff` verificado linha a linha). Das 159,
são deslocações "da cabeça do bloco / cabeça do braço para acima da instrução", e as restantes 222 são a eliminação direta de linhas mortas e de anotações de âmbito supérfluas —
essas posições nunca tiveram efeito, pelo que eliminá-las não altera qualquer resultado de compilação/verificação.

Três defeitos de ferramenta corrigidos em simultâneo na segunda ronda:

1. **`checker` reportava números de linha colapsados no contentor**: o `emit` de `walkExprForIntOverflow` misturava a
   `stmt.Pos().Line` do contentor com a coluna do operador, pelo que cada reporte dentro de `{ ... }` / grupo if / braço de match
   apontava para a linha do cabeçalho. O diagnóstico apontar para o sítio errado é apenas o sintoma; o problema real é que o `findFixTargetNode` do `--fix`
   apanhava "a instrução mais interna que contém essa linha", só conseguindo recuperar o contentor, e a anotação era inserida de volta na cabeça do bloco. Passou a tomar **a linha do próprio operador**
   (`x.Token.Line`), com recurso à linha do contentor quando o token não tem número de linha.
2. **Não descer ao corpo dos braços**: `findFixTargetNode` parava deliberadamente acima de toda a instrução match. Agora desce ao
   **corpo de braço em forma de bloco** (o `{ ... }` de `cond -> {`, incluindo `BlockMatchStatement.Body` de `{ ... }: { ... }`);
   o corpo de braço inline continua sem descida (`w i64 -> w + 1`: a instrução e o padrão do braço estão na mesma linha, e inserir acima dela seria inserir dentro do match,
   antes da linha do padrão do braço).
3. **Inserir em posições onde não há onde anexar também inseria**: `#{overflow}` agora, após inserir, **volta a analisar para autoverificação**, retirando as anotações que
   "foram inseridas mas degeneraram numa `AnnotationStatement` independente" (deixam de ficar linhas mortas). Após a retirada, a segunda passagem de `--fix`
   recebe o código inalterado, mantendo a idempotência.

A terceira ronda (2026-11-12) mexeu no parser: os alvos de anexação de `parseAnnotationStatement` ganharam **DOT**.
Instruções que começam pelo recetor, como `.n = .n + 1` / `.cnt == 0 -> …`, são da mesma classe de `x = …` (instrução única, L3),
mas antes não estavam entre os alvos anexáveis ⇒ a anotação degradava-se numa `AnnotationStatement` independente (linha morta), o sítio era sempre reportado e
o `--fix` inseria e era retirado pela autoverificação. Com o alargamento, 219 sítios passaram a ter anotação L3 (linhas de anotação 3861 → 4080),
e os não marcáveis desceram de 270 para 51. A forma da AST mudou em consequência (uma `AnnotationStatement` a menos, e a instrução alvo passa a ter
um registo side-table e `OverflowMode`), `parser.ASTVersion` passou a `2026-11-12.1`, e após a alteração é obrigatório `make gen no`.

**`{` e `(` não foram alargados de propósito**: `{` (bloco nu em posição de instrução / match com parênteses) e `(` (cabeçalho de loop `(cond) { }`)
são anfitriões de bloco L2, e o desenho de `overflow` é reconhecer apenas L3. Deixá-los entrar faria `overflowAnnotatedNode`
retornar antecipadamente para o bloco inteiro, o que equivaleria a legalizar a semântica L2, em conflito com a ordem "primeiro restringir, depois alargar" de §8. As 51 posições restantes
são na sua maioria destes dois tipos e pertencem ao passo 4b de §8.

Critério de aceitação (árvore isolada A/B, mesma fonte e mesmo compilador, só `src/std` diferente, `no vet` ficheiro a ficheiro): os 73 ficheiros afetados
**todos com 0 error** (sem quebra de compilação); 25 ficheiros passaram a ter menos diagnósticos e 19 mais — os que aumentaram são todos `ovfhndld`
"vinculado a `?T` sem declarar captação de transbordo" expostos após retirar as anotações que faziam sombra, resultado esperado do passo 1 de §8, não um novo defeito.
Há ainda 9 avisos `nolang-overflow-ineffective` que são um problema antigo do comportamento "preencher lacuna" da ferramenta incidir sobre operadores sobrecarregados de bigint /
strings, sem relação com a descida. Após a segunda ronda, a repetição integral de `bin/no run test-std/src/*.no`
(zstd / tls / str / json / set / base32 …) **deu tudo PASSED**.

Critério da terceira ronda: os 3 testes vermelhos de `parser` (`TestAnnotationsLiveOnAST` /
`TestCachedASTLowersToIdenticalHIR` / `TestBuildSemViewsMatchesFreshParse`) **são idênticos palavra por palavra antes e depois do alargamento** —
`only 288 files parsed cleanly` é o mesmo nas duas vezes, indicando que o alargamento não trouxe nenhuma nova falha de análise;
são o estado em voo da refatoração parser/annotation em trabalho paralelo. A saída de `no fmt -d src/std` é
**byte a byte idêntica** antes e depois do alargamento, ou seja, o formatador imprime as anotações sobre `.` exatamente no seu lugar, sem deriva de formatação. `test-std/src`
está tudo verde para heap / zstd / tls / json / set / map / regexp.

**Restam 76 anotações em cabeça de braço** (`#{overflow=wrap}` imediatamente acima de `cond -> {`): **não são linhas mortas** —
o parser copia a anotação de nível de braço para cada instrução dentro do corpo do braço, pelo que tem efeito no corpo (medido: uma anotação na cabeça do braço faz
`r = i * 2` no corpo calar-se, restando apenas a reportar a condição do cabeçalho). Mas **não tem efeito na condição do braço** em si (a condição não é uma instrução).
Estas 76 mantêm-se como estão: a inserção instrução a instrução não as apanha (os sítios do corpo do braço já estão calados e deixaram de ser reportados), e retirá-las e voltar a acrescentar
só daria voltas no mesmo sítio da linha do cabeçalho.

**Ainda reportadas por `no vet` e não anotáveis pela linguagem atual: 51 posições** (na segunda ronda eram 270; a terceira ronda, ao alargar DOT,
deixou apenas estas): a instrução alvo começa por `{` (bloco nu em posição de instrução / match com parênteses) ou `(` (cabeçalho de loop `(cond) { }`),
e algumas condições de cabeçalho de braço. Hoje **também** são reportadas (a anotação original nunca teve efeito), pelo que a retirada não constitui
regressão; para chegar a zero é preciso primeiro fazer os passos 1–2 de §8 (declarar `overflow` em L2 como erro) e só depois decidir se se deve mesmo suportar
L2 — ver passo 4b de §8.

Verificando posições como `archive/bzip2.no:315`, há `#{overflow=wrap}` **simultaneamente** na cabeça do bloco e dentro dele:
a camada da cabeça do bloco não tem efeito atualmente, e a que realmente funciona é a camada instrução-a-instrução dentro do bloco (já acrescentada por `--fix=overflow`). Por isso eliminar
a camada de nível de bloco não altera a semântica; mas, após a eliminação, voltar a correr `no vet` vai expor as posições que "só se calavam por causa da anotação externa" — esse é exatamente
o objetivo do endurecimento, não um efeito colateral. Medido: as hints `ovfhndld` de `crypto/rsa.no` passaram de 19 para 22,
todas em instruções "vinculadas a `?T` sem declarar captação de transbordo", sem alteração semântica.

Impacto no legado de L0: em `src/**/*.no`, há 21 ficheiros em que uma anotação é a primeira linha efetiva do ficheiro, dos quais 17 são
`#{js}` e os restantes `#{buildin}` etc. imediatamente seguidos de declaração — todos **sem** linha em branco, pelo que, pelo critério L0 de §2,
continuam a ser L1, e o comportamento de `no fmt` e da compilação não muda. `str.no` tem 2 casos de "anotação + linha em branco + braço cond",
dentro do corpo de uma função, que não disparam o critério L0.

---

## 8. Ordem de endurecimento

1. **Primeiro fazer o `no vet` deixar de aceitar `overflow` em L1/L2**: eliminar o curto-circuito "saltar a função inteira",
   e limitar `enclosingOverflow` à subárvore da mesma instrução (sem propagação para baixo entre blocos).
2. **Depois fazer o parser reportar erro**: `#{overflow}` em L0/L1/L2 → erro de compilação, com mensagem a apontar para "marca instrução a instrução,
   ou corre `no fmt --fix=overflow`". Este passo expõe de uma vez todo o legado.
3. **Limpar o legado**: `no fmt --fix=overflow` já tem capacidade de descida (retira a camada de âmbito e depois acrescenta L3 instrução a instrução
   segundo os reportes do checker); `src/std` passou por três rondas (ver tabela de legado em §7). Após a alteração é obrigatório `make gen no` (recozer
   os blobs e o StdSigKey), e não se deve correr `no fmt -w` em `src/std` — a migração só mexe em linhas de anotação, executando-se ficheiro a ficheiro com
   `no fmt -fix=overflow -w <f>`, e o `diff` garante que não há outras alterações.
4. **Alargar os alvos de anexação do parser** (terceira ronda concluiu metade):
   - **4a concluído**: `parseAnnotationStatement` aceita instruções que começam por **DOT**. `.n = .n + 1` /
     `.cnt == 0 -> …` pertencem à mesma classe de `x = …` (instrução única, L3), mas antes não admitiam anotação ⇒ `src/std` tinha 219 sítios com
     `#{overflow=wrap}` que nunca tiveram efeito. Após o alargamento, os não marcáveis desceram de 270 para 51 e `parser.ASTVersion` passou a
     `2026-11-12.1`.
   - **4b por decidir**: `{` (bloco nu em posição de instrução / match com parênteses) e `(` (cabeçalho de loop `(cond) { }`) são **anfitriões de bloco L2**.
     Alargá-los equivaleria a legalizar a semântica L2 para `overflow` (`overflowAnnotatedNode` retornaria antecipadamente para o bloco inteiro),
     indo contra a direção de endurecimento dos passos 1–2. A ordem correta é fazer primeiro os passos 1–2 (declarar `overflow` em L2 como erro) e só depois ver se as restantes 51 posições se resolvem
     "reescrevendo-as como instruções sem bloco" ou "acrescentando L3 ao corpo do braço", em vez de abrir `{` / `(` sem condição.
5. **Restringir gradualmente as restantes chaves**: passar as chaves de declaração (`buildin` / `generic` / `embed`…) escritas em L2/L3 de
   "ineficaz" para "erro".
6. Cada passo tem de garantir que `no fmt` é idempotente e que os quatro leitores chegam à mesma conclusão para a mesma entrada (§6).
