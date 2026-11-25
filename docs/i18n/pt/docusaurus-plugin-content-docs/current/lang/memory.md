---
sidebar_position: 6
---

# Gestão de memória

O Nolang é uma linguagem **sem GC**, e a segurança de memória é garantida pelo compilador através da inserção automática de `free`. Este artigo descreve o design de memória implementado e a semântica de propriedade.

## Princípios fundamentais

### Propriedade única
Cada buffer `data` no heap tem **um único proprietário**. A propriedade pode ser transferida através de move; após a transferência, o proprietário original deixa de ser responsável pelo free. O `=` entre variáveis locais utiliza um clone profundo, de modo que cada variável possui os seus próprios dados.

### Semântica de atribuição
`b = a` escolhe uma das seguintes semânticas, dependendo do contexto:

| Semântica | Condição de disparo | Comportamento |
|------|---------|------|
| **Cópia de valor** | Tipos básicos (i64/f64/bool, etc.) que não satisfazem can_slot_rebind | Copia diretamente o valor, sem dados no heap |
| **Rebind de slot de pilha** | `b = a` entre variáveis locais, em que a é do tipo de pilha (i64/u64/i128/u128/txt) e satisfaz can_slot_rebind | `g.varAlias[b] = a`, b e a partilham o mesmo slot de pilha, 0 cópias (melhor que cópia de valor); caso contrário, degrada para cópia de valor |
| **Clone profundo** | `b = a` entre variáveis locais, em que a é um tipo proprietário de heap (vec/arr/str/estrutura clonável/**`?str`, `?[]T`**) e **continua a ser referenciado** após a atribuição | malloc de novos dados + memcpy + clone recursivo dos elementos; a e b possuem cada um os seus próprios dados e fazem free no final da função |
| **move (origem morta)** | `b = a` entre variáveis locais, em que a é um tipo proprietário de heap mas **já não é referenciado** após a atribuição | Transferência de propriedade sem cópia; a origem ignora o free. **Observacionalmente equivalente** ao clone profundo, apenas poupa uma cópia |
| **move** | Parâmetro de saída `out = x` | Cópia superficial da estrutura + marca a origem como moved; a origem ignora o free |
| **Clone profundo** | `vec.push(x)` (x é um tipo proprietário de heap) | malloc de novos dados + memcpy + clone recursivo dos elementos; a origem continua a possuir dados independentes e cada um faz free no final da função |

> **A atribuição valor-a-valor é sempre uma cópia profunda; aliases sem cópia são proibidos.** `b = a` (em que o RHS é uma variável inteira com nome) tem apenas duas reduções —
> **origem ainda viva ⇒ clone profundo**, **origem morta ⇒ move**; **não é permitido** que «dois nomes partilhem um buffer e apenas um faça free»
> (essa redução existiu na otimização de encaminhamento só de leitura e foi removida). As **leituras de elementos/campos** (`b = a[i]`, `b = a.f`) mantêm a semântica de
> **alias de vista** e não estão sujeitas a esta regra.

## move de tipos de pilha (rebind de slot de pilha / slot-rebind)

`i64/u64/i128/u128/txt` são todos **tipos de pilha** (o valor é armazenado diretamente num slot de pilha alloca, sem dados no heap), mas `b = a` ainda segue, por predefinição, a semântica de «cópia de valor» (fazer memcpy do valor de pilha de a para o slot de pilha de b). Para reduzir cópias desnecessárias, o compilador executa o **rebind de slot de pilha** em `b = a` de tipos de pilha que satisfazem a restrição **can_slot_rebind**: define `g.varAlias[b] = a`, de modo que b e a partilhem o mesmo slot de pilha (0 cópias, melhor que cópia de valor).

### Restrição can_slot_rebind (correção conservadora)
Após o rebind, b e a apontam para o mesmo slot de pilha, pelo que qualquer leitura posterior de b por parte de a lerá o «armazenamento de a». Para evitar erros semânticos, o rebind só é permitido quando a origem a **não tem qualquer referência posterior (leitura ou escrita)**; caso contrário, **degrada para uma cópia de valor completa** (a semântica mantém-se, apenas com um memcpy adicional).

- A segurança é resolvida estaticamente por `computeSlotRebindSafety` (função principal) / `computeMoveEligibility` (funções do utilizador) através de `stmtContainsVarRefAny` / `exprContainsVarRefAny`: percorre todas as instruções após o move (incluindo ramos e arestas de retorno de ciclos); se a variável de origem ainda for referenciada de alguma forma, `slotRebindSafe[stmt] = false` → segue para cópia.
- A varredura de referências deve esgotar **todos os nós da AST que podem referenciar variáveis**; caso contrário, uma referência de escrita não coberta pode causar um rebind inseguro (slot de alias poluído → saída errada ou até ciclo infinito). Cobertos: ao nível de instrução `LetStatement` / `ExpressionStatement` / `ForStatement` (incluindo `Init`/`Update`/`Condition`/`CountExpr`/`Body` e a **coleção de iteração `IterRange`**) / `ReturnStatement` / `MultiAssignStatement` / **`UnwrapAssignStatement` (atribuição de desembrulho `?=`)** / **`BlockStatement` (bloco nu)**; ao nível de expressão `Identifier` / `AssignExpression` / `Infix` / `Prefix` / `Call` / `Dot` / `Index` / `IfExpression` (incluindo os corpos dos ramos) / `Slice` / `Conditional` / `Grouped` / **`CoExpression` (spawn de coroutine `co <call>`, de forma defensiva; a desativação para toda a função continua a ser da responsabilidade de `curHasUnsafeConstruct`)** / **`CastExpression`** / **`RangeExpression`**. Qualquer novo constructo de referência a variáveis deve ser adicionado a ambas as funções de varredura.
- `match` já foi desugared para `IfExpression`, e as referências nos seus ramos são cobertas pela análise acima, pelo que **match não dispara a desativação** (verificado com `tests/match.no` / `tests/option.no` como consistente com a linha de base).

### Cenários de desativação (sempre degradam para cópia de valor)
| Condição de desativação | Motivo |
|---------|------|
| O destino é um parâmetro de saída / variável global / variável de tipo de heap | Os parâmetros de saída são passados por ponteiro, as globais são visíveis entre funções e os tipos de heap requerem free profundo; o rebind quebra a propriedade |
| A origem é um parâmetro / variável global | Os parâmetros são passados por referência; o rebind corrompe o frame de pilha do chamador |
| **Funções da stdlib** (`curIsStdLib`, determinado por `SetStdModules`) | Os aliases internos da stdlib (como `it` da fmt com alias para a variável local `n`) são difíceis de modelar completamente com a análise de referências |
| **Corpo de função do utilizador com closure (`FunctionLiteral`) ou spawn de coroutine (`RunExpression`)** (`curHasUnsafeConstruct`, verificado recursivamente por `bodyHasUnsafeConstruct`) | As variáveis capturadas por closures são avaliadas num «contexto de função independente» e as coroutines executam entre threads; em ambos os casos o tempo de vida das variáveis ultrapassa o âmbito de alias de função única de `g.varAlias` da função atual, pelo que o rebind corromperia o slot de pilha partilhado |

> Compromisso de design: closures/coroutines usam «desativação para toda a função» em vez de «cálculo variável a variável», porque a análise de alias de função única não consegue, por natureza, modelar a partilha de armazenamento entre funções/threads. A desativação para toda a função apenas perde otimização (degrada para cópia) e nunca introduz erros, em conformidade com o princípio de «correção conservadora».

### Invalidação de alias
- Quando o destino é reatribuído, primeiro `delete(g.varAlias, name)` limpa os aliases antigos residuais; caso contrário, o caminho de atribuição genérico `varAddr(name)` continuaria a apontar para o slot de pilha da origem antiga, poluindo-a.
- O rebind executa resolução transitiva: percorre a cadeia `g.varAlias` para encontrar o slot de pilha da origem final (`a → c → …`), evitando desalinhamento em aliases de vários saltos; e sincroniza `g.varTypes[name]` para manter consistentes as consultas de tipo posteriores.

### Ortogonalidade com o move de tipos de heap
O rebind de slot de pilha atua apenas sobre a semântica de **cópia de valor** de `b = a` em tipos de pilha e é totalmente ortogonal ao «clone profundo / move (parâmetro de saída)» dos tipos proprietários de heap; o caminho dos tipos de heap não é afetado por `slotRebindSafe`.

### Referências de testes
- `tests/slot-rebind.no`: rebind de i64/u64, comparação de i128/u128 via `==`, txt via `.len-bytes()`, cópia degradada (`da` reutilizado → cópias `db`/`dc`), após reatribuição (`ra`=10), move de parâmetro (`pm_fn` com origem como parâmetro → cópia), reatribuição após rebind (`rr_fn` → `rr`=99), consumo (`consume(m)` → `cm`=84). Saída esperada `42 7 1 1 17 5 5 10 100 99 84`.
- `tests/slot-rebind-unsafe.no`: uma coroutine (`awy { co ... }`) captura uma variável de pilha; verifica que, com o rebind desativado por `curHasUnsafeConstruct`, a saída permanece consistente com a linha de base.

### free inserido pelo compilador
- No final da função: liberta todas as variáveis locais de heap que não foram moved
- Antes de reatribuir: liberta o valor antigo
- Campos de estrutura: liberta recursivamente os campos com dados no heap

## Layout de tipos

| Tipo Nolang | Estrutura de memória | Campos | Estratégia de alocação |
|------------|-----------|------|---------|
| `[]T` (fatia) | **16 bytes** | **u32 len, u32 cap, i64 data (ptrtoint)** | malloc (heap) |
| `[N]T` (array fixo) | N × sizeof(T) | Elementos inline, **sem descritores len/cap** | alloca (pilha) ou malloc |
| `str` (string longa) | **16 bytes** | **u32 len, u32 cap, i8\* data** | malloc (heap) |
| `txt` (string fixa) | 256 bytes | `[255 x i8]` data, i8 len | alloca (pilha) |
| `?T` (anulável) | **Dimensionado conforme o payload**: `{ i8 tag, [K x i64] }` | 1 byte de tag + 7 bytes de preenchimento + payload (múltiplo de 8) | alloca (pilha); quando o payload excede o limite, passa a armazenar um ponteiro para o heap |
| Enum com tag | `{ iN tag, [K x i64] }` | A largura da tag varia com o número de variantes (começa em `i8`); o payload é a união da variante mais larga | alloca (pilha) |
| Estrutura | Soma de todos os campos | Cada campo | alloca (pilha) |

**O tamanho de `?T` é calculado, não fixo**: `?i64` = 16 bytes, `?str` = 24, `?txt` = 264; um payload que exceda `option-payload-inline-threshold` (predefinição 264 = 1 + 7 + 256, mínimo 16) passa a ser empacotado no heap e a opção inteira volta a 16 bytes. Cada tipo de payload tem o seu próprio tipo `%option_<key>` (`src/mir/layout.go`), pelo que «tipo igual ⇔ layout igual». O payload de `err` é sempre empacotado e não conta para o orçamento de payload. Ver `docs/docs/lang/syntax.md` «Tipos anuláveis (option) · Layout de memória».

A largura da tag de um **enum com tag** varia com o número de variantes (`<=256` → `i8`, `<=65536` → `i16`, `<2^32` → `i32`, caso contrário `i64`); a largura da união de payload é «a variante mais larga» e não a soma de todas, os campos são dispostos segundo o alinhamento natural e endereçados por **deslocação em bytes**. Quando todas as variantes não têm payload, o layout degrada para `{ iN }` (a partir de 1 byte).

**O header u32 de `str`**: `%str-long = type { i32, i32, i8* }` (16 bytes em plataformas de 64 bits). `len` / `cap` são internamente `u32` (LLVM `i32`), mas **todas as APIs públicas continuam a ser `i64`**: na leitura `zext i32 -> i64`, na escrita `trunc i64 -> i32`. As assinaturas de `s.len-bytes()`, `s.len()`, `s.cap()`, `s._len()` / `s._cap()` / `s._set-len()` / `s._set-cap()` e `len(s)` não mudaram. Esta camada de fronteira é tratada de forma uniforme pelo codegen através de `strLenI64` / `strCapI64` / `strHeaderLoadI64` / `strHeaderStoreI64` / `strLongFromParts`:
**Não é permitido** emitir `load i64` / `store i64` diretamente nos campos 0 / 1 do descritor; com ponteiros opacos o verificador LLVM não o deteta, mas ler/escrever 8 bytes de uma vez altera também o campo de header adjacente (mediu-se `s.len-bytes()` a devolver `len | cap<<32`).

As constantes de tamanho correlatas também têm de acompanhar os 16 bytes: o stride dos elementos de `[]str`, o armazenamento de apoio de argv de `os.args()` e os literais `{ i32 len, i32 cap, i8* data }` das constantes de string globais. O payload de `?str` tem exatamente 16 bytes (2 words i64, em que o primeiro word empacota len e cap), pelo que `?str` é uma opção inline de 24 bytes; o payload da variante `str` num enum com tag também é 2 words. `txt` mantém-se inalterado e não passa a u32 como `str`.

**O header u32 de `[]T`**: `%vec = type { i32, i32, i64 }` (16 bytes). `len` / `cap` são isomórficos a `str`, internamente `u32` e com APIs públicas ainda `i64`; a diferença está em que **o campo data continua a ser `i64`** (um ponteiro codificado com `ptrtoint`, em vez de `i8*`), para manter inalterados os pontos de chamada existentes de `inttoptr` / `ptrtoint`. Esta camada de fronteira é tratada de forma uniforme por `vecLenI64` / `vecCapI64` / `vecHeaderLoadI64` / `vecHeaderStoreI64` / `vecFromParts` (assim como os genéricos `hdrFieldI64` / `hdrTrunc` / `hdrWiden`); `emitLenCap` / `emitGetField` / `emitSetField` / `ensureVecBuffer` passam todos por eles. A mesma proibição de `str` aplica-se: **não é permitido** emitir `load i64` / `store i64` diretamente nos campos 0 / 1 de `%vec`.

`cap == 0` é um marcador de **vista de empréstimo** para `%vec` (`s.bytes()`, empréstimo `[N]T` → `[]T`, conversão forçada de str → `[]byte`): não possui heap e `@vec_free` / `@vec_retain` ignoram-no; só `cap > 0` possui o bloco apontado por `data`.

`[N]T` não tem qualquer descritor em tempo de execução: ao nível LLVM é uma agregação nua `[N x T]`, e `.len` é substituído pelo compilador diretamente pela constante de compilação `N`, sem `cap`.

## Free superficial vs. free profundo

### Free superficial
Liberta apenas o buffer de dados do contentor, sem percorrer os elementos. Aplica-se a:
- `%str-long` (os dados da string são um buffer de caracteres, sem elementos aninhados proprietários de heap)
- vec/arr cujos elementos são tipos básicos (i64, double, etc.)

### Free profundo
Primeiro percorre cada elemento e liberta recursivamente os seus dados no heap; depois faz free do buffer de dados do contentor. Aplica-se a vec/arr cujos elementos são tipos proprietários de heap:
- `[]str` (elementos do tipo %str-long)
- `[][]i64` (elementos do tipo %vec)
- `[]MyType` (elementos do tipo estrutura do utilizador, campos libertados recursivamente)

### Verificação de NULL
Antes de cada free, verifica-se `icmp eq i8* %ptr, null` para evitar free(NULL) ou o free de um ponteiro não inicializado.

## Transferência de propriedade (move)

### move de retorno único
```no
get-slice = () (out []i64) {
    local = [1, 2, 3]
    out = local; local marcado como moved, sem free no final da função; out gerido pelo chamador
}

v = get-slice(); v possui os dados, free no final da função
```

### move de retorno múltiplo (por ordem de posição dos parâmetros)
```no
get-pair = () (a []i64, b []i64) {
    x = [1, 2]
    y = [3, 4]
    a = x; primeiro parâmetro de saída, x marcado como moved
    b = y; segundo parâmetro de saída, y marcado como moved
}

a, b = get-pair(); a possui os dados de x, b possui os dados de y
```

**Ordem de processamento**: processa um a um pela **ordem de declaração** dos parâmetros de saída na assinatura da função. Cada atribuição `out = src` marca a variável de origem como moved de forma independente.

**Múltiplas atribuições da mesma variável de origem**: se `a` e `b` referenciarem a mesma variável de origem (como `a = x; b = x`), o compilador decide automaticamente entre clone e move através da **pré-análise de Liveness** (`moveEligible`):

- `a = x`: x é referenciado posteriormente por `b = x` → **clone profundo**, a possui dados independentes
- `b = x`: x não é referenciado posteriormente → **move**, b assume os dados de x, x é marcado como moved e ignora o free

Isto evita double-free. Regra: **a última referência segue move; as anteriores seguem clone**.

Cenários com ramos condicionais (como `if cond { out = x }`) acompanham o estado moved através de um bitmap em tempo de execução; ver a secção seguinte.

### Clone profundo em vec.push
```no
inner = [1, 2, 3]
outer.push(inner)
; os dados de inner são clonados profundamente para a posição do novo elemento em outer
; inner continua a possuir dados independentes; no final da função, inner e outer fazem free cada um
```

push executa clone profundo em tipos de elementos proprietários de heap (`%str-long`/`%vec`/`%arr`/estrutura do utilizador): malloc de novos dados + memcpy + clone recursivo dos elementos. A variável de origem e o vec externo possuem cada um dados independentes, **sem necessidade de marcação move**, evitando double-free. Elementos de tipos básicos (i64/f64, etc.) têm o valor armazenado diretamente.

### Rastreio de move em tempo de execução (bitmap indexado pelo slot da variável de heap)

O move sob ramos condicionais apresenta um desafio: em tempo de compilação não é possível determinar se um move chegou realmente a ocorrer.

```no
cond-move = (flag i64) (out []i64) {
    x = [1, 2, 3]
    if flag == 1 {
        out = x; move ocorre apenas quando flag==1
    }
    ; quando flag==0, x continua a possuir dados e precisa de free no final da função
    ; quando flag==1, a propriedade de x foi transferida e o free deve ser ignorado no final da função
}
```

O Nolang resolve este problema com um **bitmap indexado pelo slot da variável de heap**. Em tempo de compilação, é atribuído um `varIdx` único a cada variável local de heap; cada bit do bitmap em tempo de execução corresponde a uma variável de heap (e não a um parâmetro de saída).

#### Estado do compilador

| Campo | Tipo | Finalidade |
|------|------|------|
| `heapVarIndex` | `map[string]int` | Nome da variável de heap → `varIdx` (apenas variáveis locais de heap) |
| `outBindState` | `[]int` | Slot da variável de heap atualmente vinculada a cada parâmetro de saída (-1 = sem vínculo, -2 = incerto) |
| `movedVarBitset` | `[]uint64` | Bitmap moved em tempo de compilação (usado quando não há bitmap em tempo de execução) |
| `movedBitmapBase` | `string` | Prefixo do nome da variável do bitmap em tempo de execução (como `%__mb`; vazio = não alocado) |
| `bitmapCount` | `int` | Número de blocos u64 do bitmap (= maxVarIdx/64 + 1) |

#### Regra de mapeamento de slots

Um bloco `u64` armazena 64 bits de marcação; o slot da variável de heap é `varIdx`:

- Número do bloco = `varIdx / 64`
- Deslocação dentro do bloco = `varIdx % 64`
- Máscara = `1u64 << deslocação`

Em tempo de compilação, calculam-se constantes diretamente, sem custo de computação em tempo de execução. Vários blocos `u64` suportam qualquer número de variáveis de heap, **sem limite para o número de parâmetros/valores de retorno**.

#### Processamento da atribuição move (a substituição limpa o bit antigo)

Sempre que uma variável de heap é movida para um parâmetro de saída:

1. Se esse parâmetro de saída já estava vinculado a outra variável (`outBindState[outIdx] >= 0`), primeiro limpa o bit correspondente à variável antiga
2. Depois coloca o bit da variável atual a 1
3. Atualiza o slot da variável vinculada a esse parâmetro de saída (`outBindState[outIdx] = srcVarIdx`)

#### Libertação no final da função

Percorre todas as variáveis de heap e gera `if` independentes e «achatados»: se o bit correspondente for 0, faz free; se for 1, a propriedade foi transferida e a libertação é ignorada.

#### Alocação do bitmap sob demanda

A variável do bitmap só é alocada **quando necessário**, evitando custos de desempenho em cenários sem ramos:

| Cenário | Alocação do bitmap | Comportamento do free |
|------|---------|----------|
| Sem move | Sem alocação | Tudo é libertado |
| move fora de ramos (move determinístico) | Sem alocação | `movedVarBitset` em tempo de compilação ignora diretamente o free |
| move dentro de ramos (move condicional) | Com alocação | Verificação do bitmap em tempo de execução: bit=1 ignora, bit=0 faz free |

Antes de gerar o corpo da função, o compilador pré-verifica a AST (`detectBranchMoveToOut`) para detetar atribuições move a parâmetros de saída dentro de ramos `IfExpression`/`ForStatement`/`ConditionalExpression`; só aloca a variável do bitmap em tempo de execução quando esse padrão existe. O `alloca` do bitmap é inserido após a geração do corpo da função (quando `nextHeapVarIdx` já tem o valor final) e escrito no bloco de entrada.

Este mecanismo aplica-se a todos os tipos de heap (`vec`/`str-long`/`arr`/estrutura do utilizador).

## Clone profundo (atribuição entre variáveis locais)

```no
a []i64 = [10, 20, 30]
b = a; clone profundo: malloc de novos dados + memcpy + clone recursivo dos elementos
b[0] = 99
; a[0] == 10 (a não é afetado)
; b[0] == 99 (b é modificado de forma independente)
```

### Fluxo do clone profundo
1. Liberta o valor antigo da variável de destino (se já tiver dados no heap)
2. Faz `malloc` de um novo buffer de dados e `memcpy` dos dados de origem para os novos dados
3. Clona recursivamente cada elemento proprietário de heap:
   - Elemento `%str-long`: malloc + memcpy dos dados da string
   - Elemento do tipo estrutura do utilizador: memcpy da estrutura + clone recursivo dos campos com dados no heap
4. Escreve o novo ponteiro de dados, len e cap na variável de destino
5. Regista o destino como variável de heap (free no final da função)

### Tipos clonáveis
| Tipo | Clone profundo possível | Observações |
|------|-------------|------|
| `%vec` / `%arr` (elementos de tipos básicos) | ✅ | Basta memcpy dos dados |
| `%vec` / `%arr` (elementos do tipo %str-long) | ✅ | malloc+memcpy dos dados da string, elemento a elemento |
| `%vec` / `%arr` (elementos do tipo estrutura clonável) | ✅ | Clone recursivo dos campos da estrutura, elemento a elemento |
| `%vec` / `%arr` (elementos do tipo %vec / %arr) | ✅ | Clone recursivo dos elementos do contentor interno através do mecanismo `elemElemType` |
| `%str-long` | ✅ | malloc + memcpy dos dados da string |
| Estrutura do utilizador (sem campos de contentor aninhados) | ✅ | memcpy da estrutura + clone recursivo dos campos de heap |
| Estrutura do utilizador (com campos de contentor aninhados) | ✅ | memcpy da estrutura + clone recursivo dos campos com contentores aninhados (via `elemElemType`) |
| `?str` | ✅ | Opção com payload inline: clona a própria opção e os seus dados de string |
| `?[]T` | ✅ | Opção com payload inline: clona a própria opção e os dados da fatia interna (clone elemento a elemento, recursivo quando os elementos possuem heap, como em `?[]str`) |

> **`?str` / `?[]T` são payloads inline, mas possuem heap.** Os seus payloads são armazenados diretamente no próprio `%option_<key>` da opção (a estrutura de `?str` é `{ i8 tag, [2 x i64] }`, com 16 bytes de payload para `{ u32 len, u32 cap, i8* data }`), mas os dados `str` / o armazenamento de apoio da fatia são alocados no heap
> ⇒ **no clone é preciso copiar profundamente também o payload e no free é preciso libertar também o payload**, os dois lados têm de andar em par.
> A libertação de `?[]T` usa o mesmo helper de free profundo de `[]T` (decide se é recursivo conforme o tipo de elemento), pelo que a dualidade «clone profundo com free profundo» também se verifica aqui.
> ⚠️ Nas opções normais (não `?str`/`?[]T`), o payload é um empréstimo ou um valor puro e **não possui** heap; nem o clone nem o free lhe tocam.
> Quando o payload é grande ao ponto de exceder `option-payload-inline-threshold`, a opção passa a armazenar um ponteiro para o heap e o free tem de libertar também a caixa
> (determinado por `Boxed`, ver `src/mir/layout.go`).

### Diferença em relação ao move
- **Clone profundo**: a origem e o destino possuem cada um dados independentes e fazem free cada um no final da função
- **move**: a origem abdica da propriedade (marcada como moved), o destino assume os dados e a origem ignora o free

Regras de decisão para `b = a`:
1. Se a for a origem de um parâmetro de saída → move
2. Caso contrário, se a for um tipo proprietário de heap mas **não for referenciado após a atribuição** → move (sem cópia, observacionalmente equivalente ao clone profundo)
3. Caso contrário, se a for um tipo proprietário de heap e permitir clone profundo → clone profundo
4. Caso contrário → cópia de valor

`vec.push(x)` não segue estas regras: push é uma chamada de método e, independentemente de x ser ou não um tipo proprietário de heap, executa clone profundo nos elementos proprietários de heap (ver secção anterior).

## move de tipos de pilha (rebind de slot de pilha / slot-rebind)

`i64`/`u64`/`i128`/`u128`/`txt` são tipos de pilha (não proprietários de heap), mas `b = a` (com uma variável como RHS) deixa de fazer cópia de valor quando a origem `a` não tem referências posteriores e passa a executar o **rebind de slot de pilha** (stack-slot rebind): define `g.varAlias[b] = a`, de modo que `b` e `a` partilhem o mesmo slot de pilha (0 cópias). Isto é melhor que a cópia de valor (que ainda tem de emitir load+store), especialmente vantajoso para tipos de pilha grandes (como `%txt`, de 256 bytes).

### Restrição can_slot_rebind (correção conservadora)

O rebind de slot de pilha é **mais rigoroso** que o `moveEligible` do move de heap:

- `moveEligible`: permite move quando a origem `a` **não é lida** posteriormente (aplica-se a tipos de heap; após o move, a origem ignora o free, sem afetar o destino).
- `can_slot_rebind` (`slotRebindSafe`): só permite o rebind quando a origem `a` **não tem qualquer referência posterior (leitura ou escrita)**. Como, após o rebind, `b` e `a` partilham o mesmo slot de pilha, uma escrita posterior em `a` corromperia o valor de `b`.

Cálculo: `generateFunctionDefinition` chama `computeMoveEligibility` (que preenche simultaneamente `moveEligible` e `slotRebindSafe`); `generateMainFunction` chama `computeSlotRebindSafety` (**apenas** preenche `slotRebindSafe`, sem tocar em `moveEligible` — a função principal do HEAD não ativa o move de heap, mantido desligado para evitar SEGFAULT). Ambos fazem uma varredura de referências sensível a ramos/ciclos via `stmtContainsVarRefAny` (incluindo o valor lvalue de `AssignExpression` e as arestas de retorno de ciclos).

Quando `can_slot_rebind` não se verifica (a origem ainda tem referências posteriores) → **degrada para uma cópia de valor completa**, nunca degenerando para um comportamento inseguro.

### Cenários de desativação (devem seguir o caminho de atribuição normal)

| Cenário | Motivo |
|------|------|
| Funções da stdlib (`curIsStdLib`) | A stdlib contém constructos como match/closure/coroutine que a análise de referências não consegue modelar totalmente; o rebind corromperia o slot de pilha partilhado (como `it` da fmt com alias para a variável local `n`). Deteção: `g.stdModules` (injetado por `transpiler.go` `SetStdModules` a partir de `checker.KnownStdModules()`), com retorno a `g.funcOwner[fd.Name]` |
| O destino é um parâmetro de saída | O parâmetro de saída recebe um ponteiro do chamador; o rebind faz com que aponte para o slot de pilha local da origem e o chamador não consegue lê-lo |
| O destino é uma variável global | O rebind faz com que o nome global seja resolvido para o slot de pilha local da origem, quebrando a semântica global |
| O destino é uma variável de tipo de heap | O rebind de slot de pilha só se aplica a tipos de pilha |
| A origem é um parâmetro | Os parâmetros são passados por referência; o rebind corrompe o frame de pilha do chamador |
| A origem é uma variável global | O endereço global é alocado por alias à origem local, erro semântico |

### Invalidação de alias após reatribuição

Quando uma variável é reatribuída (`b = ...`), primeiro `delete(g.varAlias, name)` limpa os aliases antigos que possam restar, evitando que o caminho de atribuição genérico chegue a `varAddr(name)` e ainda aponte para o slot de pilha da origem antiga, poluindo-a. Ao definir um novo alias, faz-se **resolução transitiva** ao longo da cadeia de aliases (`src := ident.Value; for { if n2,ok := g.varAlias[src]; ok { src = n2 } else break }`) para encontrar o slot de pilha da origem final e evitar desalinhamento em aliases de vários saltos.

### Ortogonalidade com o move de heap

O rebind de slot de pilha atua apenas sobre tipos de pilha, não envolve transferência de propriedade nem `free` e é totalmente independente do mecanismo de clone/move dos tipos de heap.

**Testes**: `tests/slot-rebind.no` (cobre rebind de i64/u64/i128/u128/txt, degradação para copy, invalidação de alias após reatribuição do destino, degradação de move de parâmetro, passagem de argumentos a consume; saída esperada `42 7 1 1 17 5 5 10 100 99 84`).

## Valores de retorno str de FFI extern

As funções FFI extern (marcadas com `#{c}`) devolvem ponteiros C para strings (`i8*`) que podem apontar para memória estática (como `getenv`, `strerror`) ou para um buffer externo (como o ponteiro devolvido por `strchr`, que aponta para o interior do parâmetro). Envolver diretamente num `%str-long` faria com que `emitHeapFree` chamasse `free()` em memória não-heap → UB.

O compilador insere uma cópia segura no caminho de retorno `str` de FFI extern:

1. **Verificação de NULL**: se C devolver NULL, constrói um `%str-long` vazio (data=0, len=0). Atenção: `str` não é uma opção, pelo que **`s == nil` é um erro de compilação** (`q4nilcmp`) — para distinguir NULL de string vazia, declare o extern como devolvendo `?str` (NULL ⇒ `nil`) ou teste `s.len == 0`
2. **Não NULL**: `strlen` + `malloc` + `memcpy` + terminação null, copiando para um buffer de heap independente
3. **Fusão PHI**: as duas vias são fundidas e constrói-se um `%str-long` para retorno

```no
#{c}
strchr = (s str, c i64) (r str)

find = () (r str) {
    r = strchr('hello', 108); C devolve um ponteiro para o interior de 'hello'
    ; o compilador copia automaticamente com malloc+memcpy; r possui dados independentes
    ; no final da função, emitHeapFree liberta r.data em segurança
}
```

Este mecanismo é logicamente idêntico ao caminho `RetCStrToStr` da clib (usado por built-ins como `get-env` e `get-wd`), garantindo que todos os valores de retorno de strings C possuem propriedade independente.

## Libertação de variáveis ao nível de módulo

As variáveis de heap ao nível de módulo (`vec`/`str`/`arr`/estrutura) são compiladas como globais LLVM (`@name`), e os seus buffers `data` são inicializados com malloc nas instruções de top-level na entrada de `main`.

O compilador chama, antes do `ret i32 0` no `main` de entrada C:
1. `emitHeapFree` — liberta as variáveis locais de heap de top-level (não globalVars)
2. `emitGlobalHeapFree` — percorre `moduleVarTypes` e liberta todos os tipos proprietários de heap em `globalVars`

```no
GLOBAL-STR = 'hello'; LLVM @GLOBAL-STR = global %str-long zeroinitializer
GLOBAL-VEC = [1, 2, 3]; LLVM @GLOBAL-VEC = global %vec zeroinitializer
; as instruções de top-level fazem malloc de data e armazenam-no na global
; antes do ret de main, emitGlobalHeapFree liberta data
```

Isto evita a acumulação de fugas de memória em serviços de longa duração (como daemons com ciclos). Não tem impacto em ferramentas CLI de execução única (o sistema operativo recupera a memória ao terminar o processo).

## Vistas de fatias

A expressão de fatia `arr[1..3]` produz uma vista (sem cópia) que partilha os dados do array original. Os três destinos possíveis da vista:

| Destino | Comportamento | Propriedade |
|------|------|--------|
| Variável local `v = arr[1..3]` | Vista sem cópia | Partilha os dados do array original |
| Parâmetro de saída `out = arr[1..3]` | clone (malloc+memcpy) | Propriedade independente |
| Tipo explícito `[]T`, `v []i64 = arr[1..3]` | clone | Propriedade independente |

**Motivo**: o parâmetro de saída escapa para o chamador e o array original pode ser libertado antes do final da função, pelo que a vista tem de ser clonada para dados independentes.

## Reatribuição e libertação do valor antigo

```no
s = 'hello'; malloc do buffer de dados
s = 'world'; liberta os dados de 'hello' e faz malloc de novos dados
```

Ao reatribuir um tipo proprietário de heap, o compilador liberta automaticamente os dados do valor antigo antes da atribuição, evitando fugas.

## Libertação de campos de estrutura

```no
Node {
    name str
    items []i64
}

n = Node {
    name: 'hello'
    items: [1, 2, 3]
}
; no final da função, liberta recursivamente:
;   - n.name.data (campo %str-long)
;   - n.items.data (campo %vec)
```

Ao libertar uma estrutura, percorrem-se todos os campos e libertam-se recursivamente os dados dos campos de tipos proprietários de heap.

## Reatribuição de array fixo a fatia

```no
local [4]i64 = [100, 200, 300, 400]; local é um array fixo (16 bytes)
local = [100, 200, 300]; reatribuído como fatia (16 bytes)
```

Os layouts de memória de um array fixo (`%arr`, 2 campos) e de uma fatia (`%vec`, 3 campos) são diferentes. Na reatribuição, o compilador aloca automaticamente uma nova variável `%vec` e redireciona todos os acessos posteriores, evitando acessos fora dos limites do buffer.

## Casos de teste verificados

Os testes encontram-se em `tests/mem-safety/`:

| Teste | O que é verificado |
|------|---------|
| `deep-clone.no` | Independência do clone profundo em `b = a` ([]i64/[]str/str/estrutura) |
| `deep-free-str.no` | Free profundo de `[]str` |
| `deep-free-nested-vec.no` | Free profundo de `[][]i64` + clone profundo em push |
| `deep-free-struct-vec.no` | Free profundo de `[]MyType` (estrutura recursiva) |
| `struct-field-leak.no` | Libertação dos dados de heap dos campos de estrutura |
| `slice-view-escape.no` | Clone da vista de fatia atribuída a um parâmetro de saída |
| `reassign-leak.no` | Libertação do valor antigo na reatribuição |
| `vec-push-leak.no` | Libertação do buffer antigo no crescimento de vec.push + clone profundo de elementos proprietários de heap |
| `ffi-str-return.no` | Cópia segura do valor de retorno str de FFI extern |
| `global-heap-free.no` | Libertação das variáveis de heap ao nível de módulo na saída de main |
| `double-move-same-source.no` | `a=x; b=x`: múltiplas atribuições da mesma origem, primeiro clone + último move |
| `move-clone-liveness.no` | A pré-análise de Liveness decide clone/move (inclui ramos condicionais e três atribuições) |
| `rule1-binding.no` | Regra um: independência de escrita, alias só de leitura e move de origem morta para `str`／`[]i64`／struct／`?str`／`?[]i64` em `b = a` |
| `nested-container-clone.no` | Dualidade clone profundo／free profundo em contentores aninhados (`[][]str`, vec dentro de struct, vec como valor de map, etc., 12 casos) |

## Limitações conhecidas

### Contentor map
O hashmap ainda não implementa o free profundo de key/value, pelo que os dados de heap do contentor map sofrem fugas.

### Variáveis temporárias em ciclos
```no
#{fmt-skip}
loop {
    s = 'temp'   ; a cada iteração, malloc de novos dados; os dados antigos não são libertados
}
```

### Vista de fatia + move do array original
```no
view = arr[1..3]; view partilha arr.data
arr = [9, 8, 7]; liberta o arr.data antigo → view fica pendurada
```

### Dados partilhados em async (corrigido a 2026-09-30)

A fronteira de spawn de `awy { r = co f(x) }` passou a **copiar profundamente** os argumentos proprietários de heap; as tarefas deixaram de partilhar buffers com o chamador.

As coroutines são cooperativas e de thread única, pelo que «partilhar dados de heap mutáveis entre coroutines» não se verifica ao nível da linguagem; a única coisa que podia ser partilhada era o valor que o **chamador continua a manter após o spawn**. Antes da correção, o argbuf era apenas uma **cópia bit a bit** do descritor de argumentos, com `data` partilhado com o chamador, e a tarefa só executava no ponto de espera — se o chamador reatribuisse ou saísse do âmbito antes disso, libertava o buffer e a tarefa lia memória já libertada (mediu-se: str imprimia 40 NUL, `[]i64` imprimia 0, ambos com rc=0 e sem qualquer diagnóstico).

Agora a cópia profunda é feita no momento do spawn e o wrapper liberta a cópia após a chamada:

| Tipo do parâmetro | Fronteira de spawn | Libertação pelo wrapper |
|---|---|---|
| `str` | `@str_clone` | `@str_free` |
| `[]T` (T tem helper de deep-clone) | `vecDeepClone` | `@vec_free` |
| struct proprietário de heap (folhas `str` inline / campos ponteiro) | Clone de folhas no local + cópia do pointee | Destrutor de estrutura recursivo |
| Outros (tipos de valor puro) | Cópia bit a bit (correta) | Liberta apenas o contentor |

Regressão: `tests/async-ownership.no`.

### Fuga na reatribuição de struct (corrigido a 2026-09-30)

```no
holder {
    s str
}
h holder = holder {
    s: 'first'
}
h = holder {
    s: 'second'
}
; o buffer antigo de 'first' sofria fuga
```

Ao reencaminhar uma variável local, o valor antigo só era libertado se o seu tipo fosse `Type.Owned` (`str`/`vec`/`[]T`/`map`/`?owned`). Uma struct **não conta, deliberadamente, como** `Type.Owned` (essa marca é também a base utilizada pelo lowerer para decidir «se uma vinculação é um alias»), pelo que `h = holder { … }` sobrescrevia diretamente a estrutura inteira e o buffer antigo de `s` nunca era libertado. O mecanismo de drop, porém, reconhece structs (`dropOwnsHeap` → `typeOwnsHeap`, e `emitDrop` liberta com um destrutor recursivo); apenas o portão do lowerer era demasiado estreito. Após a correção, os campos antigos são libertados antes do reencaminhamento.

Medição com 2 000 000 de reatribuições: RSS de pico 66,1 MB → 33,8 MB, saída byte a byte idêntica.

### Valor do primeiro campo de struct literal é uma chamada de método (corrigido a 2026-09-30)

```no
holder {
    s str
}
h = holder {
    s: n.to-str()
}
; Antes da correção: 'holder' is not defined
```

A categorização de `{ … }` é decidida com base numa antevisão de alguns tokens. O **separador de braços do match também é `:`**, pelo que `s: n.to-str()` se parece tanto com «um braço de match com padrão `s`» como com «um campo de struct literal». O único desambiguador entre os dois é o token **a seguir** a `ident.ident` — uma chamada de método só pode ser um valor de campo. O ramo antigo omitia `(`, pelo que o bloco inteiro era classificado como match, o nome da struct não era encontrado no contexto de match e era reportado como indefinido (quando o valor do campo é uma chamada de função normal, funciona).

Após a correção, `h = holder { s: recv.method(args) }` é analisado corretamente. Um braço de match em nolang não pode começar com `name : X(` (o separador de braços é `->`, um braço que começa com `:` é obrigatoriamente wildcard/default e o corpo não contém chamadas com parênteses), pelo que aceitar `(` não faz com que nenhum match válido seja confundido com um struct literal. Regressão: `src/parser/struct_literal_field_value_test.go`.

### Tipo do resultado de await (simplificação de sintaxe a 2026-11-17)

A sintaxe antiga expunha o handle da tarefa como uma variável `i64` e era necessário transportar o tipo do resultado através de um «rebind do slot do handle» (`t i64 = 0; t = run f(...); v = awy t`). Com a remoção de `run` e `awy <handle>`, o tipo do resultado da expressão `co` é determinado diretamente pelo callee, pelo que este caminho deixou de existir. A regressão passou a ser fixada pelo MIR subjacente em `src/mir/async_boundary_ownership_test.go`.

### Contagem de referências de tarefas (simplificação de sintaxe a 2026-11-17)

O `%task` interno continua a ser o único objeto com contagem de referências: é criado com `rc = 1` no spawn e sofre `release` quando a espera termina. A sintaxe antiga expunha handles escritos à mão, e caminhos de cópia como `h2 = h` / `run h` por vezes omitiam o retain (sobretudo em tarefas com resultado void, com SIGSEGV medido), sendo detetados pelo verificador `checkRefBalance`. O novo `awy { ... }` não expõe handles e espera exatamente uma vez por cada `co` no fim do grupo, pelo que a sintaxe visível não pode produzir referências fora da lista. Os testes `.no` e Go de alias de handle / RC foram removidos juntamente com `run`; o emparelhamento `OpTaskRetain` / `OpAwait` ao nível MIR continua coberto pelos fixtures MIR em `src/mir/opt_test.go`.

### Movimento dos argumentos de spawn: sem cópia quando a origem está morta (2026-10-01)

`co f(x)` faz uma cópia profunda de cada argumento **proprietário de heap** na fronteira de spawn, porque a tarefa pode só executar no fim do grupo e o chamador pode reatribuir ou sair do âmbito antes disso. Quando x **não volta a ser usado com certeza** após o spawn, essa cópia é redundante: a propriedade é **transferida** diretamente para a tarefa.

```no
s str = 'hello'
awy {
    v = co echo(s)      ; s não é lido depois -> transferência sem cópia
}
print(v)                ; hello

s str = 'hello'
awy {
    v = co echo(s)
}
print(s)                ; s ainda é lido -> mantém-se cópia profunda (cada lado possui o seu)
print(v)                ; hello
```

Três critérios, todos necessários:

1. O chamador **já iria**, por natureza, libertar x — empréstimos (vistas de fatia, borrow read) e parâmetros não são libertados pelo chamador;
2. x está morto após o spawn (**sem qualquer leitura posterior**);
3. O parâmetro do callee nessa posição é `str` / `[]T` / `struct` proprietário de heap — o wrapper liberta o seu payload.

Para estas três categorias, a libertação feita pelo wrapper da tarefa e o drop original do chamador **reduzem-se à mesma helper**, pelo que a transferência apenas desloca **a mesma libertação** do chamador para a tarefa, sem libertar a mais nem a menos.

**Semântica do utilizador inalterada**: uma tarefa que não é esperada tem o buffer dos seus argumentos libertado pelo wrapper no momento da espera; o movimento e a cópia são idênticos a este respeito.

Implementação: `insertDrops` regista em `Module.spawnArgMoves` e `emitAsyncRun` salta a cópia profunda com base nisso. Regressão: `tests/async-arg-move.no`, `src/mir/spawn_arg_move_test.go`.

### Alias na tabela de esperadores de coroutines (corrigido a 2026-09-30)

O escalonador usava originalmente `@nolang_waiters = global [256 x i8*]`, indexado por `ptrtoint(task) & 255`. As tarefas são alocadas por `@malloc` e **alinhadas a 16 bytes**, pelo que os bits baixos do ponteiro só têm 4 bits válidos — os 256 slots colapsavam, na prática, em 16 (mediu-se que só 8 eram usados), e duas tarefas em espera concorrente conseguiam facilmente sobrescrever-se mutuamente e acordar a tarefa errada.

A tabela inteira foi removida e os esperadores passaram a ser guardados no 5.° campo do próprio `%task` (`%task` passou de 24 para 32 bytes): `@nolang_async_wait` escreve a tarefa atual no campo do **esperado** e `@nolang_async_done` lê esse campo e, se não estiver vazio, coloca-o na fila e limpa-o. A consulta passa a ser exata, sem aliases de endereço.

> Nota: o backend MIR atualmente acciona await de forma síncrona e nunca chama `@nolang_async_run`／`@nolang_async_wait`, pelo que esta é hoje uma **falha latente** e não uma avaria observável. Regressão:
> `src/mir/async_boundary_ownership_test.go`.

### `awy` era consumido quando não era a primeira instrução de um bloco (corrigido a 2026-09-30; reescrito a 2026-11-17)

`skipToStatementEnd()` avança até um token que possa **começar** a instrução seguinte e só aí para, e a lista branca `isStatementBoundary()` (`src/parser/stmt.go`) omitia as palavras-chave de prefixo. Assim, o `run` / `awy t` da sintaxe antiga podia ser consumido juntamente com a quebra de linha quando não era a primeira instrução, causando uma degradação semântica com `rc=0` e sem diagnóstico.

Na nova sintaxe, `awy` tem o seu próprio caso em `parseStatement` (grupo de coroutines) e está incluído, juntamente com `co`, em `isStatementBoundary()`; `run` deixou de ser palavra-chave e foi removido da lista branca. A regra é: **todos os tokens que o `switch` de `parseStatement` consegue despachar têm de estar também listados em `isStatementBoundary()`**.

Regressão: `tests/stmt-boundary.no`, `src/parser/stmt_boundary_block_test.go`.

### Um `{ ... }` nu em posição de instrução era tratado como declaração (corrigido a 2026-09-30)

O ramo `LBRACE` de `parseStatement` chama `classifyBlockAtCurrent()` (neste momento, `currentToken` já é `{` e não há nome para consumir). Os valores `blockEnum` / `blockIface` / `blockTaggedEnum` que devolve **não têm qualquer sentido em posição de instrução** — as declarações reais de enum/interface/enum com tag requerem todas um nome à frente e seguem o ramo anterior `classifyBlock()`. Mas o despacho de instruções só encaminhava `blockUnknown` para um bloco de instruções, pelo que:

- `{ x = 2 }` era classificado como `blockEnum` e `{ print(2) }` como `blockIface`;
- ambos eram enviados para `parseExpressionStatement`, e o ramo `LBRACE` de `expr.go` só reconhecia `blockStruct` / `blockMatch`, seguindo os restantes por `p.nextToken(); return nil`;
- o `{` era consumido, as instruções do bloco passavam a ser **irmãs** do corpo externo e o `}` do bloco acabava por fechar o bloco **externo**.

O resultado não era «consumir», mas **fuga de instruções um nível acima**: `f = () { print(1); { print(2) }; print(3) }` imprimia `3,1,2`; `f = () { x = 1; { x = 2 }; print(x) }` não imprimia nada. Também com `rc=0` e sem diagnóstico.

A correção consistiu em encaminhar também `blockEnum` / `blockIface` / `blockTaggedEnum` para o bloco de instruções no despacho de instruções (`blockStruct` tem de **permanecer** no caminho de expressão — `{ field: value }` é uma instrução literal de struct anónima válida).
⚠️ **Não** altere o ramo `ASSIGN` de `classifyBlockAtCurrent` para «corrigir» isto: isso cobriria apenas uma das três classificações erradas e afastaria a decisão do único local que sabe «estou em posição de instrução».

⚠️ **Efeito colateral**: esta correção fez com que `{ ... }` em posição de instrução sobrevivesse, pela primeira vez, como `*parser.BlockStatement`, e a lista branca de `programUsesPrint` (`src/build/transpiler.go`, o walker manual da AST que decide se é preciso carregar `fmt`/`io`/`str`/`byte`) não inclui `BlockStatement` ⇒ programas que escrevem `print` apenas dentro de um bloco nu deixam de ser detetados.
**Ao mexer ao nível de instrução, é preciso voltar a executar `go test ./build/ -run TestProgramUsesPrint`** e acrescentar os novos tipos de contentor a `walkStmt`. Regressão: `src/parser/stmt_boundary_block_test.go`, `tests/stmt-boundary.no`.

### O critério de ignorar o free na primeira atribuição de uma variável global não é suficientemente preciso

O compilador usa o map de compilação `globalFirstAssigned` para acompanhar se uma variável global já sofreu a primeira atribuição: a primeira atribuição ignora a libertação do valor antigo (que é `zeroinitializer`, não dados de heap) e só as reatribuições seguintes libertam o valor de heap antigo.

Este map é inicializado uma única vez ao longo de toda a compilação, não é reiniciado por função e não distingue caminhos de ramos condicionais. Se uma variável global receber a primeira atribuição dentro de um ramo condicional, o compilador processa segundo a ordem da AST: a primeira instrução de atribuição é marcada como «primeira» (ignora free) e a segunda (mesmo noutro ramo) segue o caminho de reatribuição (tenta libertar o valor antigo). Se, em tempo de execução, o segundo ramo executar primeiro, a global continua a ser `zeroinitializer` (data=NULL, len=0) e o compilador tenta libertar um valor antigo não inicializado.

**Mitigação atual (eficaz)**:
- Contentores rasos (`%str-long`): `emitNullCheckFree` gera uma verificação em tempo de execução `icmp eq i8* dataPtr, null` e ignora `call @free` quando é NULL
- Contentores profundos (`%vec/%arr`): `emitDeepContainerFree` tem ainda um atalho de `len == 0`; o len de um zeroinitializer é 0, pelo que se ignora diretamente todo o ciclo de libertação

Estas duas camadas de proteção em tempo de execução fazem com que, mesmo com uma decisão de compilação pouco precisa, não ocorra uma falha. Mas, logicamente, depende-se da verificação NULL em tempo de execução como rede de segurança, em vez de uma decisão precisa em tempo de compilação.

**Direção de melhoria potencial**: transformar `globalFirstAssigned` de um map de compilação num mecanismo de rastreio em tempo de execução (semelhante ao bitmap de `movedVarBitset`), mas isso acrescentaria custo em tempo de execução e a mitigação atual já é suficientemente eficaz.

## Compromissos de design

1. **O custo do clone profundo.** `b = a` aloca e copia uma cópia dos dados; `vec.push(x)` também faz um clone profundo dos elementos proprietários de heap.
   Isto é deliberado: **troca-se uma cópia por uma categoria inteira de análise de aliases** — entre valores mantém-se apenas «propriedade» e «movimento», sem partilha,
   pelo que «quem possui, quem liberta e se uma escrita é observável» não precisa de um conjunto de regras separado para cada caso de alias.
2. **A cópia profunda na fronteira de spawn (＝instantâneo).** `co f(x)` faz uma cópia profunda dos argumentos proprietários de heap no momento do spawn, pelo que a tarefa vê
   **o valor no momento do spawn**, independentemente das escritas posteriores do chamador. **Quando a origem está morta, passa a ser um move sem cópia.**
   ⚠️ Não altere isto para «partilhar o mesmo buffer com contagem de referências» só para poupar a cópia: isso faria com que a tarefa visse as **escritas no local** do chamador
   entre o spawn e a espera no fim do grupo (partilha), violando a regra 4. A forma correta de poupar a cópia é **copiar apenas na escrita** (copy-on-write).
3. **A contagem de referências de tarefas não fecha ciclos.** O `%task` interno é o único objeto com contagem de referências; o novo `awy { ... }` não expõe handles
   e cada `co` espera exatamente uma vez no fim do grupo, pelo que não se forma um ciclo de «tarefas com handles umas das outras».
4. **Não se partilham dados de heap mutáveis entre coroutines.** As coroutines são agendadas de forma **cooperativa e de thread única**; partilhar dados de heap mutáveis entre coroutines não se verifica ao nível da linguagem;
   permitir isso só traria condições de corrida e ponteiros pendurados. Por isso, a fronteira de spawn transmite sempre **cópias independentes** (ou transfere a propriedade).
