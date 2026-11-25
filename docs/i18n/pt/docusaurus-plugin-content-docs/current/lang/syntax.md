\---  
sidebar_position: 2  
\---

# Sintaxe


## Comentários

O Nolang suporta marcadores de **comentário de linha única** e de **comentário de várias linhas (de bloco)**:

- `;` — marcador de linha única (comenta até ao fim da linha)
- `;; <conteúdo>` — marcador de linha única (quando há conteúdo na **mesma linha** a seguir a `;;`, comenta até ao fim da linha, com semântica idêntica a `;`)
- `;;\n` — comentário de várias linhas (de bloco): é ativado quando `;;` é **seguido imediatamente por uma quebra de linha** (só são permitidos espaços em branco), até se encontrar um `;;` igualmente seguido de quebra de linha / EOF

```no
; Isto é um comentário
; Isto também é um comentário, com a mesma semântica
;; Continua a ser um comentário de linha única (não há quebra de linha depois de ;;)
x = 1; comentário inline, comenta até ao fim da linha
x = 2;; comentário inline de linha única, semântica idêntica

;;
Isto é um comentário de várias linhas (de bloco)
Pode abranger várias linhas
Até encontrar um ';;' isolado
;;

y = 3
;;
O ;; de fim tem de ser seguido de quebra de linha ou fim de ficheiro
para ser considerado um delimitador de fim
;;
```

> **Regra de ativação de várias linhas:** `;;` só pode ter **espaços em branco** (espaços/tabulações) a seguir, até à quebra de linha ou EOF, para entrar no modo de várias linhas. Se houver qualquer carácter não-espaço na mesma linha a seguir a `;;`, é tratado como comentário de linha única (comenta até ao fim da linha).
>
> **Regra de fim de várias linhas:** o `;;` de fim também tem de ser seguido de quebra de linha ou EOF (só são permitidos espaços em branco pelo meio). Um comentário de várias linhas não fechado continua a comentar até ao fim do ficheiro.

> **Regra: uma instrução por linha, é proibido escrever várias instruções numa única linha.**
> O ponto e vírgula `;` é um **marcador de comentário**, não um separador de instruções; a vírgula `,` também não pode ligar várias instruções. Cada instrução dentro do mesmo bloco `{ ... }` tem de ocupar a sua própria linha.
> Esta regra aplica-se também aos exemplos de código dentro de comentários: mesmo num comentário, não use `;` nem `,` para escrever várias instruções na mesma linha, para não induzir o leitor em erro.

```no
#{fmt-skip}
; ❌ Errado: usar `;` para separar várias instruções — tudo a partir de `;` é comentário,
; incluindo o `}` do bloco, que fica assim comentado; o bloco não fecha e o código seguinte é silenciosamente descartado
; cond -> { print('X'); return }

; ❌ Errado: usar vírgulas para ligar várias instruções
; h0 = 1732584193, h1 = 4023233417
; out = from-i64(v), out = from-u64(v)
; debug(msg), info(msg), warn(msg)
```

```no
; ✅ Correto: cada instrução ocupa a sua própria linha
cond -> {
    print('X')
    return
}

; ✅ Correto: cada instrução ocupa a sua própria linha
; h0 = 1732584193
; h1 = 4023233417
; out = from-i64(v)
; out = from-u64(v)
; debug(msg)
; info(msg)
; warn(msg)
```

> **Por que motivo `;` é especialmente perigoso:** em `cond -> { print('X'); return }`, todo o trecho `; return }` é tratado como comentário, restando apenas `cond -> { print('X')`. O bloco nunca fecha e todas as instruções a seguir ao `}` (muitas vezes o resto da função) são engolidas, podendo a localização do erro ficar muito desviada e ser extremamente difícil de encontrar.

## Tipos de dados

Tipos básicos

- byte
- bool ; só são permitidas minúsculas
- char ; tipo de carácter: um único ponto de código escalar Unicode (rune), armazenado internamente como i32. Aspas duplas envolvem um único carácter, como "中"
- str ; tipo de cadeia, envolvida por aspas simples 'hello', ou cadeia raw entre backticks `conteúdo de várias linhas`
- txt ; tipo de texto de comprimento fixo, internamente fixo em 256 bytes (até 255 bytes de dados), requer anotação de tipo explícita, como t txt = 'abc'
- i8
- i16
- i32
- i64 ; tipo predefinido para números, independente da arquitetura
- i128 ; inteiro de 128 bits com sinal
- u8
- u16
- u32
- u64
- u128 ; inteiro de 128 bits sem sinal
- f32
- f64

### Tipo char (ponto de código Unicode)

`char` representa **um único ponto de código escalar Unicode** (scalar value). O seu tipo de armazenamento subjacente é **`i32`**, e não `i64`:

- **Intervalo válido**: `0 ..= 0x10FFFF` (ou seja `0 ..= 1114111`). O valor máximo do ponto de código precisa apenas de 21 bits, pelo que `i32` o contém facilmente.
- **Erro fora do intervalo**: quando um literal excede esse intervalo, o compilador (incluindo `no vet`) **reporta diretamente um erro**. Por exemplo `c char = 0x110000` ✗, `c char = 1114112` ✗; `c char = 1114111` ✓.
- **Operações numéricas permitidas**: `char` **pode participar diretamente em operações inteiras**, por exemplo `z = a + 25` e `u = ch - 32` (em que `ch` é a variável de iteração de `for ch <- s`) são **formas normais**, somando/subtraindo pelo valor do ponto de código. As operações são sempre feitas com **largura i32** e o resultado é tratado como um valor inteiro normal; por isso, `+ - * /` de `char` **não devolve por predefinição `option<int>` como a família dos inteiros**, não disparando o erro de compilação «overflow de inteiro não tratado». O único cuidado é o intervalo: o ponto de código máximo é `0x10FFFF`; em contextos fora da semântica de `i32`/ponto de código (como deslocamentos ou contagens acumuladas), recomenda-se converter explicitamente para `i32`/`i64` antes de calcular, para tornar a intenção mais clara.
- **Relação com `str`**: `s[i]` devolve um `char`; `char` pode ser convertido implicitamente em `str` (codificação UTF-8), consulte [Cadeias — conversão implícita](str.md).

> **«Tipo subjacente» e «conversão na chamada» são coisas distintas.** `char` é, ele próprio, um `i32`. A **assinatura em código-fonte** da função embutida `str_from_cp(cp char) -> ?str` tem `char` como parâmetro; só ao **chamar o runtime externo** é que o compilador faz **zero-extend** desse parâmetro i32 (`zext i32 → i64`) para corresponder aos argumentos IR de `@str_from_cp` — trata-se apenas de uma conversão temporária na fase de preparação de argumentos, **sem alterar o tipo de `char`**. É semelhante a C: `char c='a'; f((long)c);` — `c` tem 1 byte e é promovido temporariamente na chamada.

Tipos de contentor

- obj ; objeto
- map ; mapa
- arr ; array de comprimento fixo
- vec ; array de comprimento variável
- slice ; fatia (vista), não tem estrutura de dados própria, tem de estar associada a arr/vec/str/txt

- \* ; ponteiro, limitado a declarações FFI `#{c}` e à biblioteca padrão
- usize ; usado apenas em FFI
- any ; qualquer tipo, limitado à biblioteca padrão

Tipos avançados

- bigint
- err

## Aliases de tipo e tipos de união

Um alias de tipo cria um novo nome para um tipo existente. Usa a sintaxe de igualdade `name = type` e suporta aliases de tipo único e uniões de vários tipos.

### Sintaxe

```no
; Tipo de união: vários tipos separados por |
int = i8 | i16 | i32 | i64 | i128 | u8 | u16 | u32 | u64 | u128
float = f32 | f64
num = int | float
string = str | txt

; Alias de tipo único
bytes = []byte
buf = [16]u8
```

> `int`, `float` e `num` são definidos em `std/number` e `string` em `std/str` — nenhum deles é uma palavra-chave embutida do compilador, são tipos de união escritos pela biblioteca padrão com o mesmo conjunto de sintaxe de aliases.

### Referenciação em cadeia de tipos de união

Um tipo de união pode referenciar outros tipos de união, formando uma hierarquia:

```no
int = i8 | i16 | i32 | i64 | i128 | u8 | u16 | u32 | u64 | u128
float = f32 | f64
num = int | float; num é a união de int e float
string = str | txt; string une diretamente dois tipos de cadeia concretos
```

### Uso em funções

Tipos de união podem ser usados em parâmetros e valores de retorno de funções; o compilador efetua automaticamente monomorphization, gerando uma versão independente da função para cada tipo membro:

```no
; O tipo do parâmetro é a união num
max = (a ..num) (r num) {
    r = a[0]
    n = len(a)
    i <- [1..n): {
        a[i] > r -> r = a[i]
    }
}

; O método é definido sobre o tipo de união
num.sign = () (r num) {
    {
        . > 0 -> r = 1

        . < 0 -> r = -1

        -> r = 0
    }
}
```

`string` (= `str | txt`) também pode ser usado em parâmetros e valores de retorno:

```no
; Tanto argumentos str como txt podem ser passados, cada um com a sua monomorfização
take = (s string) (n i64) {
    n = len(s)
}

a str = 'hello'
b txt = 'hi'
n1 = take(a); take__str
n2 = take(b); take__txt
```

> As restrições atuais do corpo de funções com tipos de união (o mesmo para `num` e `string`): chamar um método de membro num parâmetro union dentro do corpo (como `s.len()`) ainda não é suportado; use uma função embutida global (`len(s)`, número de bytes) ou divida em funções de tipos concretos. A declaração de variáveis de tipos de união (`s string = 'x'`) também ainda não é suportada. Atenção ainda para não definir um alias de método com o mesmo nome de um método de membro (como `string.len`) — ele assumiria a chamada `s.len()` e recursivamente se redispatcharia para si próprio no corpo do método.

### Regras de deteção

A sintaxe de igualdade é reconhecida como alias de tipo (e não como atribuição de variável) nos seguintes casos:

- `name = type | type | ...`: tipo de união (contém `|`)
- `name = []type`: tipo de fatia
- `name = [N]type`: tipo de array
- `name = ?type`: tipo opcional
- `name = known-type`: alias de tipo único, em que `known-type` é um nome de tipo embutido (como `i64`, `f64`, `bool`, `str`, etc.) ou o nome de um alias de tipo definido anteriormente

## Declaração de variáveis

```no
#{fmt-skip}
; As variáveis não têm palavra-chave
; i64、f64、byte、bool、str podem omitir a anotação de tipo
i = 1

; f64 tem um . ao meio
f = 1.0

; byte
b = 0x00

; i8: se o nome da variável e o tipo coincidirem, a anotação de tipo pode ser omitida
i8 = 3

; Uma anotação explícita idêntica ao tipo inferido é redundante (no vet reporta tcpoxtfd, no fmt --fix=redundant pode removê-la automaticamente)
x str = ''
x = ''; equivalente, a anotação é redundante

; Valor predefinido 0
; As variáveis não precisam de declaração prévia
u16 u16

; str envolvido por aspas simples
name = 'nolang'

; bool true/false tudo em minúsculas
flag = true
flag = false

; Abreviatura bool: !! equivale a true, ! equivale a false (literais independentes)
flag = true; true
flag = false; false

; Atribuição de variáveis
; Não é permitido o mesmo nome; se o nome se repetir, é tratado como modificação da variável
name = 'hello'
name = 'world'

; Concatenação de cadeias
greeting = 'hello, ' - name

; Cadeia raw (envolvida por backticks, de várias linhas, sem escapes)
sql = 'SELECT id,name
FROM user
WHERE id > 100'

; Anotação de tipo explícita
a u64 = 10

; Inferência de tipo de literais hexadecimais
; - Literais inteiros decimais (como 771) são inferidos como i64 (tipo inteiro predefinido)
; - Literais hexadecimais (como 0x0303) são inferidos como byte (u8)
; - Se o valor hex exceder o intervalo de byte (> 255), tem de ser acrescentada uma anotação de tipo explícita:
PORT i64 = 0x0303; correto: i64 explícito, valor = 771

; PORT = 0x0303          ; errado: inferido como byte, valor truncado!
; PORT = 771             ; correto: decimal por predefinição é i64
; - Recomenda-se usar letras minúsculas em literais hex (0x00ff, e não 0x00FF)
; - Sugestão: usar decimal para constantes inteiras gerais; usar hex com anotação explícita de tipo i64 para constantes de protocolo/máscara de bits

; Carácter (aspas duplas = char/rune, carácter único)
c = "中"

; Tipo byte
b = 0x00

; arr array de comprimento fixo
arr [3] = [1, 2, 3]

; vec array dinâmico (fatia)
vec = [4, 5, 6]

; Tipo explícito (fatia)
typed []u8 = [1, 2, 3]

; Array
typed [3]u16 = [1, 2, 3]

; Inferência automática do comprimento (i64)
a[nil] = [1, 2, 3]
```
```

## Cadeias raw (Raw String)

Declara-se uma cadeia raw com um par de backticks `` ` `` ` ` `` ` ``; o tipo é `str`.

### Sintaxe

```
sql = `
SELECT id,name
FROM user
WHERE id > 100
`
```

### Restrições obrigatórias de formatação

1. O backtick de **abertura** `` ` `` tem de ser **seguido imediatamente por uma quebra de linha no código-fonte** (LF; CRLF de ficheiros Windows também é aceite);
2. O backtick de **fecho** `` ` `` tem de **ocupar uma linha só para si**, e essa linha só pode conter espaços em branco + o backtick de fecho;
3. **As linhas dos backticks inicial e final não contam para o conteúdo da cadeia**, servem apenas como marcadores delimitadores;
4. **Regras de escape**: todos os `\`, `\n`, `\t`, `\'`, `\"` no interior são **preservados tal como estão**, sem qualquer processamento de escape;
5. **As quebras de linha e os espaços de indentação no código-fonte são preservados integralmente** nos bytes da cadeia; exceção: CR (`\r`) — é **sempre descartado**, pelo que ficheiros CRLF e LF produzem valores byte a byte idênticos;
6. **Não é possível incorporar diretamente um carácter backtick**; se for necessário incluí-lo, use concatenação com uma cadeia normal entre aspas simples.

### Exemplo

```
; O conteúdo é "SELECT id,name\nFROM user\nWHERE id > 100\n"
; Os caracteres de escape \n \t são preservados tal como estão, não são interpretados
raw = `
line1\nline2
\ttabbed
`
```

## Literais de expressão regular (Regex Literal)

O Nolang suporta sintaxe de literais de expressão regular ao estilo JavaScript `/pattern/flags`, para criar uma instância `regexp` já compilada.

### Sintaxe

```no
; Literal de regex básico
re = /\d+/

; Com flags (sinalizadores)
re = /hello/gi

; Classes de caracteres, âncoras, quantificadores
re = /[a-z]+/
re = /^hello.*world$/

; Barra de escape
re = /a\/b/
```

### Flags (sinalizadores)

| Flag  | Significado        |
| --- | --------- |
| `g` | Correspondência global      |
| `i` | Sem distinção de maiúsculas/minúsculas    |
| `m` | Modo de várias linhas      |
| `s` | `.` corresponde a quebras de linha |

As flags são opcionais, vêm a seguir à `/` de fecho e são compostas por letras ASCII.

### Análise léxica sensível ao contexto


`/` é simultaneamente o operador de divisão e o iniciador de literais de expressão regular; o Nolang usa a mesma **análise léxica sensível ao contexto** do JavaScript para os distinguir:

- **Posição de início de expressão** (início de instrução, depois de `=` / `(` / `[` / `{` / `,` / `:` / `;`, etc.) → `/` inicia um literal de regex
- **Posição de produção de valor** (depois de identificador, literal, `)` / `]` / `}`, etc.) → `/` é o operador de divisão
- `//` é sempre um comentário de linha (prioridade máxima)

```no
; Literal de regex (= seguido de posição de início de expressão)
re = /\d+/
result = match-text(/[a-z]+/, text)

; Operador de divisão (depois de identificador é posição de produção de valor)
ratio = 100 / 4
x = a / b
```

### Desaçúcar (desugaring)

Os literais de regex são convertidos (desugared) na fase de geração de código numa chamada à função da biblioteca padrão `regexp-compile`:

```no
; Código-fonte
re = /\d+/

; Após desugaring, equivalente a
re = regexp-compile('\\d+')
```

`regexp-compile` está definido em `std/regexp.no`, cria a estrutura `regexp` e chama `.compile()` para compilar o padrão.

### Exemplos de uso

```no
; Criar regex e verificar correspondência
re = /\d+/
matched = re.matches('hello 123 world')
print(matched); true

; Procurar correspondências
re = /[a-z]+/g
result = re.find('hello 42 world')
print(result); "hello"

; Como parâmetro de função
result = match-text(/\d+/, text)
```

> **Nota:** o padrão vazio `//` entra em conflito com o comentário de linha (comportamento idêntico ao JavaScript). Se precisar de uma correspondência vazia, use `/(?:)/`.

## Regras de nomenclatura

Nomes de variáveis, funções, estruturas, etc. podem começar por underscore, a seguir podem conter hífen, letras e dígitos; não podem começar por um dígito, não podem terminar com hífen e não podem ter vários hífenes consecutivos.

**Regras de maiúsculas/minúsculas (obrigatórias):**

- **Constantes globais, variáveis globais**: **devem** começar com letra maiúscula (como `NOLANG`, `MAX-SIZE`, `HEX-CHARS`). Variáveis globais privadas começam com underscore, mas a seguir também devem ter maiúscula (como `_NOLANG`, `_PRIVATE-CONST`).
- **Variáveis locais, parâmetros de função**: usam letras minúsculas (como `hex-chars`, `data-len`). **Não** use o prefixo `_` — as variáveis locais já são privadas no seu âmbito e não precisam de marcador de visibilidade. O prefixo `_` é reservado para variáveis globais privadas e declarações FFI privadas.
- **Nomes de funções, nomes de estruturas**: usam letras minúsculas (como `sha1-block`, `db-mysql`)

**Convenções de nomenclatura de funções (fortemente recomendadas):**

- **Não acrescente o prefixo do módulo ao nome da função**. As funções dentro de um módulo precisam apenas de nomes curtos e intuitivos; o prefixo do módulo é acrescentado automaticamente nas chamadas entre módulos através de `ShortName.`. Por exemplo, defina a função de entrada `tail` em `tail.no` em vez de `tail-run`, e a função auxiliar `atoi` em vez de `tail-atoi`. Assim o código fica mais conciso e as chamadas entre módulos, como `tail.tail()`, ficam mais intuitivas.
- **A função de entrada** deve, de preferência, usar o próprio nome do módulo (como `ping.no` → `ping`, `cat.no` → `cat`). A importação entre módulos tem a forma `# /src/tail.tail`.
- **Evite palavras-chave**: `awy` (palavra-chave de grupo de coroutines), `match` (palavra-chave de correspondência condicional), etc. não podem ser usados como nomes de funções. `run` foi removido e pode ser usado como identificador normal.
- **`main` é um nome reservado e não pode ser usado como nome de função**: a entrada do programa em Nolang é **sintetizada automaticamente a partir das instruções de top-level** (consulte `hir2mir.synthesizeMainForTopLevel`), pelo que não é necessário nem **permitido** definir `main` explicitamente. Assim que aparece `main = (...) { }` — incluindo o método `recv.main` ou um `main` aninhado noutras funções — o compilador reporta diretamente um erro (`'main' is a reserved name and cannot be used as a function name`). Para servir de entrada, escreva as instruções diretamente no top-level.

> **As variáveis globais têm de começar por letra maiúscula**: esta é uma regra obrigatória, não uma convenção. Uma variável de top-level que comece por minúscula é tratada pelo compilador como variável local, o que pode causar erros como referências indefinidas.

```no
; ✅ Correto: dados globais usam letra maiúscula
NOLANG = 'nolang'
MAX-SIZE = 1024
HEX-CHARS = '0123456789abcdef'

; ✅ Variável global privada: começa com underscore, mas a seguir continua maiúscula
_NOLANG = 'nolang'
_PRIVATE-CONST = 42
```

```no
#{fmt-skip}
; ❌ Errado: variáveis globais não podem começar por letra minúscula
; x1 = 10
; x = 10
; foo-bar = 42
; hello-world = 'Hello World'
```
```

```no
#{fmt-skip}
; ✅ Variáveis locais (dentro de funções) usam letra minúscula, sem prefixo _
; fn-example = () {
;     x1 = 10
;     x = 10
;     foo-bar = 42
;     hello-world = 'Hello World'
;     ; ❌ Errado: variáveis locais não precisam de prefixo _
;     ; _x = 10
; }
```
```

### Evitar variáveis globais em módulos

**Recomenda-se vivamente: salvo necessidade, não usar variáveis globais num módulo (ficheiro `.no`).** As variáveis globais trazem os seguintes problemas:

- **Risco de bug do compilador**: o compilador Nolang tem limitações conhecidas no tratamento de endereços de memória entre funções para variáveis globais do tipo struct; funções diferentes podem ver endereços diferentes, causando inconsistência de estado.
- **Segurança em concorrência**: estado global mutável é difícil de acompanhar em cenários de fork ou assíncronos, facilitando condições de corrida.
- **Testabilidade**: estado global cria dependências implícitas nas funções, dificultando testes isolados.
- **Legibilidade do código**: variáveis globais tornam o fluxo de dados obscuro, obrigando o leitor a percorrer o módulo inteiro para compreender o comportamento de uma função.

**Práticas recomendadas:**

1. **Prefira variáveis locais**: mantenha o estado em variáveis locais dentro das funções, transmitindo-o através de parâmetros e valores de retorno.
2. **Use structs para encapsular o estado**: organize o estado relacionado numa struct e opere-o através de métodos (o recetor do método é uma variável local, com endereço consistente).
3. **Use variáveis globais apenas quando necessário**: como constantes ao nível de módulo (imutáveis), recursos singleton (como um buffer de log global), etc.
4. **As variáveis globais têm de ter maiúscula**: é uma regra obrigatória (ver acima); uma variável de top-level que comece por minúscula é tratada pelo compilador como local.

```no
#{fmt-skip}
; ❌ Evitar: usar variáveis globais mutáveis num módulo
; g-conn = tls.conn {}
; g-buf = ' '
;
; fn-a = () {
;     g-conn.send(g-buf)   ; variáveis globais podem ter endereços inconsistentes entre funções
; }
```
```

```no
; ✅ Recomendado: usar variáveis locais, com estado transmitido por parâmetros/valores de retorno
fn-a = () {
    conn = tls.conn {}; variável local, endereço consistente
    buf = ' '
    conn.send(buf)
}

; ✅ Recomendado: encapsular o fluxo completo numa única função, evitando estado entre funções
serve-once = (listen-fd fd, body str) (ok bool) {
    ok = false
    client-fd = net.net-accept(listen-fd)
    conn = tls.server-init(client-fd); variável local
    conn: {
        ok -> {
            c = it
            c.handshake()
            c.send(body)
            c.close()
            ok = true
        }

        -> fs.close(client-fd)
    }
}
```

> **Caso prático**: a função `https-serve-once` de `std/net/tls.no` encapsula o ciclo completo pedido-resposta HTTPS (accept + handshake + recv + send + close) numa única função, mantendo todo o estado TLS em variáveis locais e evitando com sucesso o bug do compilador de endereços inconsistentes entre funções para variáveis globais.

## Especificação de documentação de API

Os comentários de documentação de uma função devem conter os nomes e tipos de todos os parâmetros, bem como os nomes e tipos dos parâmetros de retorno.

**Regras:**

- O comentário de documentação acima da definição da função tem de listar o nome e o tipo de cada parâmetro, assim como o nome e o tipo do parâmetro de retorno
- O resumo de API no topo do módulo também deve usar a assinatura completa (com nome do parâmetro, tipo, nome do retorno, tipo), e não uma forma abreviada

```no
#{fmt-skip}
; ❌ Errado: faltam os tipos e o nome do parâmetro de retorno
; sha1(data) (hash)
; sha1-block(s, h0..h4)
```
```

```no
; ✅ Correto: inclui nomes dos parâmetros, tipos, nome do retorno, tipo
; sha1(data []byte) (hash [20]byte) — hash completo
; sha1-block(s []u32, h0 u32, h1 u32, h2 u32, h3 u32, h4 u32) — processa um único bloco

; O comentário de documentação acima da definição da função também deve seguir a mesma especificação:
; sha1: calcula o hash SHA-1
; data []byte: array de bytes de entrada
; retorna hash [20]byte: valor de hash de 20 bytes
sha1 = (data []byte) (hash [20]byte) {
    ...
}
```

### Comentários de documentação traduzíveis (ligações Markdown)

Um comentário de documentação pode terminar com uma **ligação Markdown autónoma**, apontando todo o comentário para uma entrada num diretório de traduções:

```no
; get: obtém o i-ésimo parâmetro
; Delegado para o builtin os.arg(i) (lê argv[i] de main); fora dos limites devolve nil
; [get](#args.get)
get = (i i64) (val ?str) {
    val = os.arg(i)
}
```

**Regras:**

- A ligação tem de ser a **última linha** do grupo de comentários e ocupar uma linha só para si; todas as linhas acima dela formam um todo, traduzido como uma única mensagem
- O destino tem de ser uma âncora começada por `#` (uma entrada dentro do mesmo diretório). Ligações externas como `[explicação](https://example.com)` são ligações normais para leitura humana e **não** são tratadas como marcadores de tradução
- A âncora é a chave (`args.get`). É atribuída pelo autor e vista pelo tradutor no diretório, não derivada do texto — pelo que reformular a redação não invalida traduções existentes. A âncora permite letras, dígitos, `.`, `_`, `-` e é global; recomenda-se acrescentar o prefixo do módulo
- O prefixo usa o **nome do módulo achatado**, não o nome do diretório. O compilador achata ficheiros std para o seu nome de ficheiro: o módulo de `std/collection/linked-hash-map.no` é `linked-hash-map`, e `use std/collection` não acrescenta o prefixo `collection.` aos símbolos. Assim, a âncora de `linked-hash-map.clear` é `linked-hash-map.linked-hash-map.clear` (nome do módulo + nome original), e **não** `collection.linked-hash-map.linked-hash-map.clear`. Segmentos de diretório como `collection`／`net`／`crypto`／`archive`／`encoding`／`database` não pertencem a nenhum símbolo e não devem aparecer na âncora
- As palavras entre parênteses retos (`[get]`) são apenas uma dica para quem lê o código-fonte; ao apresentar, a linha inteira é removida; `[](#args.get)` também é válido
- Só comentários de linha única `;` podem ter ligação (`;;` e `;;block` não contam)

As traduções ficam em `locales/<idioma>-doc.noml`, com a âncora como chave. Quando falta uma tradução, recorre-se ao texto original do comentário, sem ficar em branco. O nome do ficheiro segue as regras de nomenclatura de `.no` (letras minúsculas, dígitos, um único hífen); a própria tag tem esse formato: `--lang=zh-tw` lê `zh-tw-doc.noml`.

Na biblioteca padrão, todas as funções e métodos com comentários já têm âncora (cerca de 2000 entradas), pelo que qualquer tradução que chegue fica imediatamente utilizável pelo LSP. `make i18n` só escreve no diretório as entradas **já traduzidas**, não colocando o texto original como tradução — caso contrário apareceria texto chinês no diretório inglês. `go run ./i18n/cmd/genen -doc-missing` lista as âncoras ainda não traduzidas.

**Dica LSP:** ao passar o rato sobre o nome de uma função, é apresentado o comentário inteiro traduzido; ao passar sobre a linha da ligação, é apresentado o conteúdo apontado pela própria âncora (também no idioma atual), facilitando a verificação da tradução no local.

## Priorizar a biblioteca padrão

A biblioteca padrão do Nolang oferece inúmeras funcionalidades comuns, incluindo operações com cadeias, conversões de bytes, cálculo de hashes, comunicação em rede, etc.

**Regra: se a biblioteca padrão já tiver a funcionalidade correspondente, não é recomendado reimplementá-la.** Os programadores devem consultar atentamente a documentação da biblioteca padrão (`docs/docs/std/overview.md`) e evitar reinventar a roda.

```no
#{fmt-skip}
; ❌ Errado: implementar manualmente a conversão str → []byte
str-to-bytes = (s str) (out []byte) {
    n = s.len-bytes()
    i = 0
    (i < n) {
        out[i] = s[i]
        i = i + 1
    }
}
```
```

```no
; ✅ Correto: usar o método da biblioteca padrão str.to-bytes()
data []byte = s.to-bytes()
```

Substituições comuns da biblioteca padrão:

- `str.to-bytes()` — cadeia para array de bytes (substitui `str-to-bytes` escrito à mão)
- `[]byte.to-str()` — array de bytes para cadeia (substitui `bytes-to-str` escrito à mão)
- `[n]t.to-vec()` — array de comprimento fixo para fatia (`[20]byte` → `[]byte`)
- `[]byte.to-hex()` / `[]byte.to-hex-lower()` — array de bytes para cadeia hexadecimal
- `str.to-i64()` / `str.to-f64()` — analisar cadeia como número
- `int.to-str()` / `float.to-str()` — número para cadeia
- `std/crypto/sha1`、`std/crypto/sha256`、`std/crypto/sha512` — cálculo de hashes

### Ficheiros de configuração TOML (`toml`)

A biblioteca padrão `toml` oferece análise e serialização de ficheiros de configuração TOML 1.0. O analisador suporta comentários, chaves bare/quoted/dotted, `[table]`, `[[array-of-tables]]`, strings basic/literal/multiline, inteiros (decimal/hexadecimal/octal/binário), números de vírgula flutuante, booleanos, data/hora, arrays e inline tables. Os valores são guardados como literais TOML crus, pelo que não se perde informação como arrays ou data/hora.

```no
cfg = toml.parse('title = "Nolang"\n[server]\nport = 8080\n[[server.backends]]\nname = "local"')

doc = toml.new()
doc.put('name', '"nolang"')
doc.put('server.port', '8080')
doc.put('enabled', 'true')
text = toml.stringify(doc)
```

API comum:

- `toml.new()` — cria um `toml` vazio
- `toml.parse(text)` — analisa TOML, devolve `?toml`
- `doc.put(key, raw-value)` — adiciona ou atualiza um valor TOML cru; valores inválidos são rejeitados e chaves duplicadas são reportadas pelo analisador
- `toml.get(doc, key)` — obtém o valor cru (`?str`)
- `toml.get-str(doc, key)`、`toml.get-i64(doc, key)`、`toml.get-f64(doc, key)`、`toml.get-bool(doc, key)` — obtêm um scalar já convertido
- `toml.stringify(doc)` — produz TOML; array-of-tables produz cabeçalhos `[[...]]`

O modelo de documento atual limita cada documento a um máximo de 4 chaves, 2 tabelas normais e 2 array-of-tables; uma única chave tem no máximo 32 bytes e um único valor no máximo 128 bytes — quando excedido, deve dividir o ficheiro de configuração ou tratar o caso no chamador.

### Documentos YAML (`yaml`)

A biblioteca padrão `yaml` analisa e gera documentos do schema core YAML 1.2, suportando mapeamentos de bloco, sequências de bloco, coleções de fluxo, três tipos de aspas para scalars, scalars de bloco `|` / `>`, comentários, vários documentos, âncoras e aliases, chave de fusão `<<`, chaves explícitas e inferência de tipos (null / bool / int / float / `.inf` / `.nan`).

```no
doc ?yaml = yaml.parse('name: Alice\nserver:\n  port: 8080\ntags:\n  - a\n  - b\n')
doc: {
    nil -> print('nil')

    err -> print(it.msg())

    -> {
        doc = it
        name = doc.find-str('name'); ?str
        port = doc.find-i64('server.port'); ?i64
        name: {
            ok -> print(it)

            -> print('')
        }
        print(doc.to-json())
    }
}
```

API comum:

- `yaml.parse(text)` / `yaml.parse-all(text)` — analisam, devolvem `?yaml`
- `yaml.valid-of(text)` / `yaml.error-of(text)` — apenas validação
- `it.find(path)` / `it.get(key)` — obtêm um nó filho, devolvem `?yaml`
- `it.find-str(path)`、`find-i64`、`find-f64`、`find-bool` — obtenção conveniente de valores, devolvem `?valor`
- `it.to-json()` / `it.to-yaml()` — serialização

> Nota: os nós filho são devolvidos como `?yaml` e as funções auxiliares de obtenção de valores como `?valor`; desembrulhe com match (`child = it.find(path)`, cujo braço `ok` vincula o nó filho a `it` e o braço predefinido trata o valor nulo, cada braço na sua própria linha).

## Nomeação de ficheiros

Os nomes de ficheiros `.no` (incluindo nomes de pastas) usam sempre o hífen `-` para ligar palavras, **não usando underscore `_`**. Isto mantém a coerência com o estilo de nomenclatura de identificadores Nolang como nomes de variáveis, funções e estruturas.

O compilador **verifica obrigatoriamente** esta norma nos comandos `run` / `build` / `test` / `vet` / `fmt` / `ast`; qualquer segmento do caminho (nome de pasta ou corpo do nome de ficheiro, sem contar a extensão `.no`) que viole a norma **reporta diretamente um erro**:

- Caracteres permitidos: letras minúsculas `a-z`, dígitos `0-9`, hífen `-`.
- Caracteres proibidos: underscore `_`, letras maiúsculas e outros símbolos especiais.
- O hífen não pode aparecer no início/fim de um segmento, nem ser consecutivo (`--`).

Por exemplo, `tmp/zz_stridx/r_index.no` é rejeitado por `zz_stridx` e `r_index` conterem underscores; alterando para `tmp/zz-stridx/r-index.no` fica conforme. Caminhos absolutos do SO (como `/Users/me/proj/a.no`) só validam a parte que cai sob o diretório de trabalho atual; nomes de diretórios anfitriões (como `Users`) não geram falsos positivos.

```shell
✅ Recomendado:
utils/
├── string-helper.no
├── hash-table.no
└── http-client.no
```

```shell
❌ Evitar:
utils/
├── string_helper.no
├── hash_table.no
└── http_client.no
```

## Definição de funções

As funções podem produzir resultados através de **parâmetros de resultado nomeados (named result parameters)** &#x8F38;ndo o resultado, o que é, por essência,  
**açúcar sintático de parâmetros de saída (out-parameter)**; `return` (e a sua abreviatura `***`, ainda não recomendada) serve apenas para terminar antecipadamente e não pode ser seguido de resultados.

A forma de definição de função em Nolang é `name = (in-params) (out-params) { body }`: o segundo  
grupo de parênteses `(out-params)` declara os parâmetros de resultado nomeados (açúcar sintático). Atribuir-lhes valores no corpo da função  
(tal como os parâmetros de entrada normais, são todos tipos de referência); **a variável que realmente recebe o resultado tem de ser definida no local da chamada** —  
o nome do parâmetro de resultado na definição é apenas um marcador de posição; o LHS da chamada (ou o argumento de cauda) determina em que  
variável o resultado vai parar:

```no
parse-line = (s str, max-fields ?i64) (fields []str) {
    ...; atribuição a fields no corpo da função
}

; Variável de receção definida no local da chamada (vinculação LHS, tipo inferido pela assinatura)
fields = parse-line(line, nil)

; Múltiplos resultados vinculados por ordem
a, b = swap(x, y)

; Ou na forma de argumento de cauda: res é passado como argumento de saída extra
add1(5, 3, res)
```

As funções em Nolang têm as seguintes características:

- **Parâmetros de resultado nomeados são açúcar sintático**: por baixo, tudo continua a ser feito através de parâmetros de entrada (out-parameter), não é gerado nenhum novo objeto de valor de retorno, pelo que é seguro internamente.
- **Parâmetros de entrada são só de leitura**: escalares são passados por valor, tipos compostos por referência só de leitura; é proibido escrever nos parâmetros de entrada e nos seus subcampos dentro do corpo da função.
- **Parâmetros de saída são graváveis**: o chamador pode vincular uma variável existente ao slot de saída e a função modifica diretamente essa memória; também pode não vincular, cabendo à função gerar um novo valor.
- As variáveis dentro da função são destruídas automaticamente quando a função termina.
- O local da chamada tem de fornecer uma variável de receção (LHS ou argumento de cauda); dizer que «a função não tem valor de retorno» é impreciso —  
  a formulação correta é: o resultado é transmitido através de parâmetros de resultado nomeados (açúcar sintático de out-parameter) e vinculado no local da chamada.
- **Regra de alias**: é permitido que uma referência de entrada só de leitura e um slot de saída apontem para o mesmo objeto. Desde que as escritas ocorram apenas na zona de saída e a entrada seja apenas lida, é legal; o compilador não efetua verificação estática de alias.

### Vincular o slot de saída diretamente a uma variável existente (evitar variáveis intermédias)

O slot de saída no lado da chamada pode ser **vinculado diretamente a uma variável já existente**, e a função reescreve a sua memória no local (ver «Parâmetros de saída são graváveis» acima).
Assim, quando um resultado apenas continua a atualizar um estado existente, não é preciso recebê-lo primeiro numa variável intermédia descartável e só depois reescrever:

```no
; ✅ Recomendado: o resto é escrito diretamente em r (que é também o recetor do método)
qsq, r = r.div-mod(modulus)
```

```no
; ❌ Evitar: msq é usado uma única vez, servindo apenas de transbordo
qsq, msq = r.div-mod(modulus)
r = msq
```

- **O recetor pode ser simultaneamente o destino de saída**: em `r.div-mod(modulus)`, o recetor `r` é uma entrada só de leitura (`self` implícito)
  e `r` está também vinculado ao segundo slot de saída `r bigint`; pela〈Regra de alias〉, entrada só de leitura e saída só de escrita é legal.
  Medido (`no build` / `no vet` / `no fmt`), `q, a = a.div-mod(bigint.from-i64(7))` depois de `a = bigint.from-i64(100)` dá `q = 14` e `a = 2`,
  exatamente como «receber primeiro em `msq` e depois `r = msq`»; o mesmo se verifica mesmo quando `r` é o parâmetro de resultado de uma função exterior (`mod-pow` em `src/std/bigint.no` tem precisamente esta forma).
- **Descartar saídas não usadas com `_`**: se o quociente não for usado, `_, r = r.div-mod(modulus)` não precisa de introduzir sequer uma nova variável,
  sendo a forma mais concisa (ver o `_` de marcador de posição em〈Atribuição múltipla〉mais adiante).
- **Vale também para saída única**: `r = r._mul(r)`, `s = s.trim()`, `out = out.replace-str(...)` são atualizações em já existentes na stdlib.
- **Código válido existente não precisa de ser reescrito por isto**: esta é uma forma recomendada para código novo, não uma ordem de refatoração; siga〈Regra de ouro: não modifique código válido〉e
  não mexa em programas já corretos apenas por estilo.

### Valores predefinidos de parâmetros

**Não são suportados.** `name type = expr` numa lista de parâmetros (por exemplo `foo = (a i64, b ?i64 = 10)`) é um **erro de sintaxe** (`E_DEFAULT_PARAM`): o compilador não analisa a expressão predefinida nem preenche valores predefinidos em nenhum local de chamada. Isto elimina a ambiguidade de «uma declaração, várias aridades válidas» e evita a inflação de monomorfização causada por voltar a reduzir a expressão predefinida em cada local de chamada.

Quando é necessária semântica opcional, declare o parâmetro com um tipo option `?T`, cabendo ao chamador passar **explicitamente** `nil` / `ok(...)`, e o corpo da função trata isso com `match`:

```no
parse-line = (s str, max-fields ?i64) (fields []str) {
    limit = 1024
    max-fields: {
        ok(v) -> limit = v

        nil ->

        err ->
    }
    ...; limit é o limite
}

; O chamador tem de passar o argumento explicitamente
fields = csv.parse-line(line, nil); recorre a 1024
fields = csv.parse-line(line, ok(256)); limite 256
```

```no
add = (a i64, b i64) (result i64) {
    result = a + b; devolve o resultado através do parâmetro
    ...; instrução de marcador de posição: não faz nada, é descartada por inteiro na build (ver〈Instrução de marcador de posição〉)
}

; Parâmetros variádicos
add3 = (a ..i64) {
}

; Chamada de função
sum = add(1, 2); sum == 3

; Função anónima e for? com passagem de parâmetros?
(a i64) {
    print(a)
}(10)

; Chamada de função
add(a, b)

; Também pode haver vários valores de retorno
a, b = swap(5, 3)
```

## Controlo de fluxo

> **Sintaxe Deprecated (removida após a versão n)**: `for { }` / `for cond { }` / `for init, cond, update { }` / `for i in [..) { }` / `match x { }` / `if/elif/else { }` ainda podem ser analisados, mas emitem um aviso de depreciação. Use a「**nova sintaxe**」abaixo.

### Comparação novo/antigo

| Antigo / sufixo                 | Prefixo (predefinição do format)                   |
| -------------------------- | --------------------------------------- |
| `{ } (true)` loop infinito          | `!! { }` ou `true { }`                   |
| `{ } (cond)` loop condicional          | `(cond) { }` (`()` vazio é considerado false, não executa)       |
| `{ } ()` não executa               | `! { }` ou `false { }` ou `() { }`        |
| `{ } * n` contagem constante             | `n * { }` (quando N ≤ 0 o corpo não executa)                |
| `for i=0, i<n, i++ { }` contagem | `n * { }` (contagem constante) ou `i <- [0..n): { }` (variável) |
| `for i <- [a..b] { }` intervalo   | `i <- [a..b]: { }`                      |
| `for i in [a..b) { }` intervalo   | `i <- [a..b): { }`                      |
| `match x { ... }` correspondência       | `x: { ... }`                            |
| `if/elif/else { }` ramos      | `{ cond -> body }` (curto-circuito, tem de ser envolvido em `{}`)      |
| `continue`                 | `*`（abreviatura ainda não recomendada）                        |
| `break`                    | `**`（abreviatura ainda não recomendada）                       |
| `return`                   | `***`（abreviatura ainda não recomendada）                      |

### Loop / While / for-in

> **Duas formas equivalentes**: o mesmo loop suporta a「forma prefixa `(cond) { }`」e a「forma sufixa `{ } (cond)`」,  
> com semântica totalmente idêntica, mudando apenas a ordem da condição e do corpo. `no fmt` produz por predefinição a **forma prefixa**;  
> pode usar `no fmt -loop-style=suffix` para voltar à forma sufixa (ambas garantem idempotência de formatação).

```no
#{fmt-skip}
; === Forma prefixa (prefix, condição escrita antes do corpo) ===

; Loop infinito (condição sempre true) — as duas formas seguintes são equivalentes
!! {
    ...
}
!! {
    ...
}

; Não executa (condição sempre false) — as três formas seguintes são equivalentes
! {
    ...
}
! {
    ...
}
! {
    ...
}

; Loop condicional (verifica cond, executa o corpo quando é verdadeira)
(x == 1) {
    do-something()
}

; Número limitado de execuções (contagem na forma prefixa)
10 * {
    do-something()
}

; Quando N ≤ 0 o corpo não executa (0 ou negativo, ambos ignorados)
0 * {
    print('não vai executar')
}
-3 * {
    print('também não vai executar')
}

; Pode ter etiqueta (label escrito antes da condição/contagem)
#1 (x == 1) {
    do-something()
}

; === Forma sufixa (suffix, condição escrita depois do corpo, forma antiga) ===
!! {
    ...
}

; Não executa (parênteses vazios representam false)
! {
    ...
}

; Loop condicional
(x == 1) {
    do-something()
}

; Número limitado de execuções
10 * {
}

; Quando N ≤ 0 o corpo não executa (0 ou negativo, ambos ignorados)
0 * {
    print('não vai executar')
}

-3 * {
    print('também não vai executar')
}

; Sintaxe de intervalo (já suporta tipos numéricos como i64/u8, arr, vec, str)
; 16 combinações ao todo: '[' inclui o extremo esquerdo, '(' não; ']' inclui o extremo direito, ')' não.
; Limitado (4 tipos): ambos os extremos têm valor
i <- [a..b]: {
}
i <- (a..b]: {
}
i <- [a..b): {
}
i <- (a..b): {
}

; Sem limite superior (4 tipos): omite o extremo direito, itera até ao valor máximo do tipo
i <- [a..]: {
}
i <- [a..): {
}
i <- (a..]: {
}
i <- (a..): {
}

; Sem limite inferior (4 tipos): omite o extremo esquerdo, itera a partir do valor mínimo do tipo
i <- [..b]: {
}
i <- [..b): {
}
i <- (..b]: {
}
i <- (..b): {
}

; Completamente ilimitado (4 tipos): ambos os extremos omitidos, baseado no mínimo/máximo do tipo
i <- [..]: {
}
i <- [..): {
}
i <- (..]: {
}
i <- (..): {
}
i <- [5..0]: {
}
i <- abc: {
}

; Deteção de direção em tempo de execução: quando start > end, a iteração decrementa automaticamente (passo -1).
; As quatro combinações de parênteses suportam decremento:
;   [5..1]  → 5 4 3 2 1   fechado à esquerda e à direita, decrescente
;   (5..1]  → 4 3 2 1     aberto à esquerda, fechado à direita, decrescente
;   [5..1)  → 5 4 3 2     fechado à esquerda, aberto à direita, decrescente
;   (5..1)  → 4 3 2       aberto nos dois lados, decrescente
;   (3..0]  → 2 1 0       aberto à esquerda, fechado à direita, decrescente até 0
; Quando start <= end, itera em sentido crescente (passo +1).
;
; Os extremos de intervalos ilimitados são determinados pelo tipo estático da variável:
;   [..] de i64  vai de i64.MIN (-9223372036854775808) a i64.MAX (9223372036854775807)
;   [..] de u8   vai de 0 a 255
;   [..) de i64  vai de i64.MIN a i64.MAX - 1 (não inclui o máximo)
;   (..] de i64  vai de i64.MIN + 1 a i64.MAX (não inclui o mínimo)
;   (..) de i64  vai de i64.MIN + 1 a i64.MAX - 1 (não inclui nenhum extremo)
; Regra: '[' inclui o mínimo do tipo, '(' começa no mínimo+1;
;        ']' inclui o máximo do tipo, ')' termina no máximo-1.

; ❌ Explicitamente rejeitado
;   Os limites do intervalo têm de ser inteiros; não são suportadas expressões aninhadas
;   for i <- [1.5..5.5] { }   ; erro de compilação
;   for i <- [0..[1..5][0]] { } ; erro de sintaxe

; ⚠️ Evitar ambiguidade com ...
;   O operador de intervalo é .. (dois pontos), a chamada de método self é .len().
;   Escritos seguidos, [0.. .len()) → [0...len()), três pontos que parecem um único operador
;   (e a abreviatura de return já está definida como ***, ainda não recomendada; mas a sequência de três pontos continua a dificultar a leitura).
;   Use self.len() em vez de .len() para desambiguar: i <- [0..self.len()): { }
;   (no corpo do método, self e . são semanticamente equivalentes)

; Loop condicional (novo: prefixo (cond) { }, predefinição do `no fmt`; substitui o antigo for cond { })
; A forma sufixa { } (cond) é-lhe totalmente equivalente (forma antiga)
; Na maioria dos casos pode usar range-for: i <- [0..n): { }
(x == 1) {
    do-something()
}
```
```

### Sair / ignorar / regresso antecipado

> ⚠️ **O conjunto de abreviaturas `*`（ignorar esta iteração）/ `**`（sair do loop）/ `***`（terminar com return）ainda não é recomendado** — escreva
> as palavras-chave em inglês `continue` / `break` / `return`; `no fmt` normaliza todas as abreviaturas suportadas de volta à forma de palavra-chave.

```no
i <- [0..10): {

    ; continue (ignorar esta iteração, ainda não recomendado)
    continue

    ; break (sair do loop, ainda não recomendado)
    break

    ; return/terminate (regresso antecipado, apenas termina a função; planeado, ainda não recomendado)
    break
    continue
}

; Recomenda-se usar as palavras-chave em inglês (consistentes com C/Rust, facilitam a portabilidade e compilam/executam normalmente):
i <- [0..10): {

    ; equivalente a * (ainda não recomendado)
    continue

    ; equivalente a ** (ainda não recomendado)
    break

    ; equivalente a *** (ainda não recomendado), apenas termina a função antecipadamente
    return
}

; Aliases de palavras-chave (totalmente suportados pelo lexer, semanticamente idênticos às palavras originais):
;   ctn ≡ continue、brk ≡ break、rtn ≡ return；`no fmt` preserva a grafia original do alias
i <- [0..10): {

    ; equivalente a continue
    ctn

    ; equivalente a break
    brk
}

; equivalente a return, apenas termina a função antecipadamente
rtn
```

> ⚠️ **`return` (e a sua forma abreviada `***`, ainda não recomendada) só pode ser usado solitário, não pode levar um valor de retorno.**  
> As funções em Nolang não têm mecanismo de «valor de retorno» — o resultado é sempre transmitido por **parâmetros de resultado nomeados (out-param)** &#x5728;com atribuição no corpo da função (ver «Definição de funções»).  
> Por isso `return <valor>`, `return(expr)` e `*** <valor>` são todos **proibidos**, e o compilador / formatador / LSP reportam erro:
>
> - Compilador (`no build`): `Error: compilation error: parser errors: line L, column C: 'return' não pode ser seguido de valor de retorno; … [E_GENERAL]`
> - `no vet`: `<file>:L:C: [ERROR] nolang-compile: 'return' não pode ser seguido de valor de retorno; … [E_GENERAL]` (mesmo formato dos outros diagnósticos, um por linha)
> - `no fmt`: `format error: …` e termina com estado diferente de zero
> - LSP: mostra um diagnóstico de erro vermelho na linha correspondente
>
> Forma errada:
>
> ```no
> has = (n i64) (r i64) {
>     n == 0 -> return 0        ; ❌ erro do compilador / formatador / LSP
>     r = n
> }
> ```
>
> Forma correta (atribuir primeiro ao parâmetro de resultado, depois `return` solitário para terminar antecipadamente; as várias instruções no corpo de um braço só podem ocupar uma linha cada):
>
> ```no
> has = (n i64) (r i64) {
>     n == 0 -> {
>         r = 0
>         return
>     }
>     r = n
> }
> ```
>
> ⚠️ Comprimir tudo numa linha, `n == 0 -> { r = 0; return }`, não é uma abreviatura: todo o trecho `; return }` é comentário,
> pelo que o bloco nunca fecha e todas as instruções seguintes (muitas vezes o resto da função) são silenciosamente engolidas por esse braço —
> mediu-se que `no vet` não reporta erro e a função nunca é chamada.

## Canais (`chan[T]`)


Um canal é uma **fila de valores limitada, expansível, cujo envio não bloqueia e cuja receção espera**, com a extremidade de receção a retirar valores um a um através de um `?T` de três estados.
Preserva apenas um símbolo sintático: `<-` (para os restantes usos, consulte `i <- range:` em «Controlo de fluxo»).

```no
ch chan[i64] = with-cap(8)   ; capacidade inicial explícita
buf chan[i64]                 ; declaração nua: equivalente a with-cap(0), cresce sob demanda no envio

ch <- 1                       ; envio (só conta como envio se o tipo à esquerda for chan[T])
n = ch.len()                  ; número de valores atualmente acumulados
c = ch.cap()                  ; capacidade inicial
ch.close()                    ; fechar: deixa de aceitar envios, os valores existentes ainda podem ser retirados
```

### Receção: loop de três estados

`v <- ch: { ... }` é um **loop** que vincula, em cada iteração, o `?T` de três estados a `v`; `break` pode sair antecipadamente.
`ok` significa que foi retirado um valor, `nil` que o canal está fechado e vazio, e `err` que ocorreu um **erro real** (um handle zero não inicializado — não há nada à espera) — **os dois últimos terminam o loop** (os braços da mesma iteração executam normalmente).

A regra mais importante depois de `ok`: **uma leitura vazia é «esperar por um valor» e não «não há valor»**. Com o canal aberto mas vazio, o leitor
fica suspenso até que alguém faça `send` ou `close`:

- Numa coroutine (o clone `-async` saído de `co`), isso faz mesmo park: o número de estado é escrito num frame no heap, a tarefa regista-se na cadeia de espera do
  canal e devolve o controlo ao escalonador (ver a secção «Coroutines»);
- Numa função normal não é possível fazer park, pelo que **bloqueia**: aciona repetidamente a fila de prontos até haver um valor disponível.

Por isso «esvaziar» tem de terminar o loop com `close()`, forma idêntica ao `range` sobre channel do Go.

```no
ch chan[i64] = with-cap(2)
ch <- 10
ch.close()

<- ch: {
    ok -> print('got {it}')      ; it é o elemento desta iteração
    nil -> print('closed')       ; chega-se aqui depois de esvaziar
    err -> print('bad channel')  ; só um handle zero chega aqui
}
```

Para descartar a variável do loop, escreve-se `<- ch: { ... }`; se a primeira instrução do body não for um match sobre `v`,
então `v` é vinculado diretamente ao **próprio elemento** (`T` em vez de `?T`), e o body só corre nas iterações `ok`:

```no
i <- ch: {
    print(i)                     ; i é i64, não ?i64
}
```

Para distinguir os três estados por conta própria e ainda assim manter a variável do loop, basta fazer match no início do body; escrever apenas os braços (omitindo `v:`) é
uma abreviatura sinónima — o critério incide nos **nomes dos braços**, considerando-se três estados quando aparecem os nomes `ok` / `nil` / `err`:

```no
i <- ch: {
    ok -> print('got {it}')      ; it é o elemento desta iteração, i é o ?i64 desta iteração
    nil -> print('closed')
    err -> print('empty')
}
```

O critério é sintático, não de tipo (ver «Determinação da forma» abaixo). `no fmt` converte as abreviaturas acima no sujeito explícito
`i: { ... }`; ambas as formas podem ser reelaboradas e a formatação é idempotente.

### Determinação da forma

| Início do body | Forma | Tipo de `v` |
|---|---|---|
| `co bar(i)`, `print(i)` ou outra instrução normal | Elemento | `T` |
| `v: { ... }` | Três estados | `?T` |
| `ok -> ...` / `nil -> ...` / `err -> ...` | Três estados (abreviatura) | `?T` |
| Outro match nu (como `i > 5 -> ...`) | Elemento | `T` |

A determinação tem de ser feita **antes de analisar o body**: se o início do bloco for um match, o analisador desaçúcará-o imediatamente numa
cadeia de `if`, após o que já não é possível reconhecer o sentido original. Esta célula chegou a falhar por completo — `i <- ch:` era sempre tratado como forma de elemento,
os braços de três estados degradavam-se em guards independentes sem relação (todos executavam em cada iteração) e `it` era vinculado a um valor nulo.

### Semântica

| Aspeto | Regra |
|---|---|
| Capacidade | O `n` de `with-cap(n)` é a capacidade **inicial**; quando cheia, duplica; o `send` **nunca bloqueia nem perde valores** |
| Leitura vazia | **Espera**: a coroutine faz park, a função normal bloqueia acionando a fila de prontos; `err` fica reservado para erros reais como um handle zero |
| Fecho | Depois de `close()`, `send` aborta o processo; os valores já enfileirados continuam a poder ser retirados por ordem, esgotados os quais se reporta `nil`; o fecho também acorda todos os esperadores |
| Payload | No envio é feita **cópia profunda** (`str` / `%vec` passam por clone); o recetor recebe uma cópia independente — não é preciso temer que a variável de origem seja reescrita ou libertada antecipadamente |
| Concorrência | O próprio canal não usa tranca (escalonamento cooperativo de thread única): os esperadores ficam na cadeia `waiters` do bloco de controlo, e `send` / `close` retiram a cadeia inteira e voltam a enfileirar cada um; **ser acordado não significa ter recebido um valor**, pelo que, ao retomar, é preciso reavaliar a legibilidade — por isso o ponto de retoma da coroutine é o próprio bloco de park |
| Deadlock | Fila de prontos vazia e a tarefa-alvo ainda por terminar ⇒ não há como avançar: o runtime imprime `nolang: deadlock: …` em stderr e aborta, em vez de devolver um valor que ninguém enviou ou de girar em vazio |

> Uma consequência direta da suspensão em leitura vazia: **o loop de esvaziamento tem de terminar com `close()`**. Versões antigas reportavam «aberto mas vazio»
> como `err`, deixando o loop sair naturalmente, pelo que a forma `for` não precisava de fechar o canal; agora esse caminho é uma espera,
> e um programa que se esqueça de `close()` fica preso na última iteração (suspenso numa coroutine, considerado deadlock numa função normal).

### Limitações

- **Apenas implementado no backend LLVM.** `no build --js` / `--wasm` dão um erro explícito perante qualquer nó de canal
  (`[chbkn001]`), em vez de gerarem silenciosamente código incompleto.
- O canal é um **tipo de valor** (um handle de uma palavra de máquina), podendo ser passado como parâmetro, resultado ou elemento;
  `chan[chan[T]]` e `?chan[T]` podem ser declarados (ainda não cobertos por `tests/chan.no`).

## Instrução de marcador de posição (`...`)

`...` é uma **instrução de marcador de posição** da linguagem: sintaticamente válida, semanticamente **não faz nada**. Serve sobretudo para
demonstrações em documentação, esqueletos de código e reservas de `TODO`. Em tempo de compilação, a instrução inteira é descartada na fase AST→HIR,
produzindo um resultado **idêntico** a «nunca ter escrito essa linha» (mediu-se que o `sha256` do binário compilado é o mesmo com e sem `...`).

```no
parse-line = (s str) (fields []str) {

    ; TODO: ainda falta tratar os caracteres de escape aqui
    ...

    fields = csv.parse-line(s)
}

add = (a i64, b i64) (result i64) {
    result = a + b
    ...
}
```

Regras:

- **Só pode ocupar uma linha sozinha** (podendo ter um comentário `;` no fim). Formas como `fail -> ...`, `x = ...` e `[0...n)`
  reportam sempre erro de sintaxe — `...` não tem valor e não pode aparecer numa posição de expressão nem no resto de uma instrução.
- **Não é um alias de `return`**, **nem um operador de intervalo**: o intervalo é `..` (dois pontos) e o regresso antecipado escreve-se `return`.
- `no fmt` preserva-o tal como está, sem o eliminar nem o deslocar.

Todas as formas seguintes são inválidas:

```no
#{fmt-skip}
add = (a i64, b i64) (result i64) {
    result = a + b
    ...          ; ✅ única forma válida: ocupa uma linha sozinha
}

ok -> ...        ; ❌ erro de sintaxe: `...` não pode aparecer numa posição de expressão
x = ...          ; ❌ erro de sintaxe: o mesmo
i <- [0...n): {  ; ❌ erro de sintaxe: `...` não é operador de intervalo, escreva [0..self.n)
}
```

### Match

```no
; Forma simples, it usado para obter o parâmetro
x: {
    err -> log(it.msg()); it é o erro estruturado err.err

    nil -> log('nil')

    -> do-right-thing(it)
}

; Forma de desconstrução
x: {
    err(e) -> log(e.msg())

    nil -> log('nil')

    ok(v) -> do-right-thing(v)
}

; Nota: correspondência de padrões de struct (`user: { User{id=1} -> ... }`) e vinculação de campos ainda não são suportadas, por isso não são demonstradas

score: {
    [0..59] -> print('reprovado')

    [60..89] -> print('bom')

    [90..100] -> print('excelente')

    -> print('nota inválida')
}

; A sintaxe de intervalo tem 16 combinações; regra: '[' inclui o extremo esquerdo, '(' não inclui;
;                     ']' inclui o extremo direito, ')' não inclui.
;
; 【Intervalos limitados】(4 tipos) — ambos os extremos têm valor concreto
;   [a..b]  → x >= a && x <= b   ambos incluídos
;   [a..b)  → x >= a && x <  b   esquerdo incluído, direito excluído
;   (a..b]  → x >  a && x <= b   esquerdo excluído, direito incluído
;   (a..b)  → x >  a && x <  b   ambos excluídos
;
; 【Intervalos sem limite superior】(4 tipos) — omite o extremo direito (End=nil), verifica apenas o limite inferior
;   [a..)   → x >= a              esquerdo incluído, sem limite superior
;   [a..]   → x >= a              esquerdo incluído, sem limite superior
;   (a..)   → x >  a              esquerdo excluído, sem limite superior
;   (a..]   → x >  a              esquerdo excluído, sem limite superior
;
; 【Intervalos sem limite inferior】(4 tipos) — omite o extremo esquerdo (Start=nil), verifica apenas o limite superior
;   [..b]   → x <= b              sem limite inferior, direito incluído
;   [..b)   → x <  b              sem limite inferior, direito excluído
;   (..b]   → x <= b              sem limite inferior, direito incluído
;   (..b)   → x <  b              sem limite inferior, direito excluído
;
; 【Intervalos completamente ilimitados】(4 tipos) — ambos os extremos omitidos, baseados no mínimo/máximo do tipo
;   [..]    → true                mínimo do tipo ≤ x ≤ máximo do tipo (ambos incluídos)
;   [..)    → true                mínimo do tipo ≤ x < máximo do tipo
;   (..]    → true                mínimo do tipo < x ≤ máximo do tipo
;   (..)    → true                mínimo do tipo < x < máximo do tipo
;
; No match, um intervalo completamente ilimitado baseia-se no domínio de valores do tipo da variável correspondida (necessariamente verdadeiro, equivalente a catch-all).
; Em range-for, baseia-se no intervalo do tipo da variável de iteração (como [..] de i64, de i64.MIN a i64.MAX).
;
; Exemplo: usar intervalos ilimitados para uma classificação completa
v = 85
result = v: {
    [..0) -> 'negative'

    [0..60) -> 'fail'

    [60..80) -> 'pass'

    [80..90) -> 'good'

    [90..100] -> 'excellent'

    (100..) -> 'extraordinary'
}
print(result)

num: {
    1 || 3 || 5 || 7 -> print('ímpar pequeno')

    2 || 4 || 6 -> print('par pequeno')

    -> print('número maior')
}

; Tem valor de retorno, a última instrução/valor
result = x: {
    1 -> 1

    2 -> 2 + 1

    -> a + b
}

; Corpo de braço de várias linhas tem de usar chavetas -> { ... }
x: {
    nil -> {
        log('nil')
        do-cleanup()
        return
    }

    err -> {
        log(it.msg())
        do-cleanup()
        return
    }

    ok -> print(it)
}
```

> **Regra do corpo de braço de várias linhas**: quando o corpo de um braço contém várias instruções, tem de ser envolvido em chavetas `-> { ... }`. Um corpo de uma linha pode ser escrito diretamente a seguir a `->`. Isto porque, sem chavetas, a vinculação de `it` do match de option não pode ser inserida corretamente, causando um erro de compilação.

> **Semântica de match dentro do corpo de for-in**: `i <- (a..b]: { 1 -> ... 2 -> ... }` executa o corpo do match uma vez para cada variável de iteração `i` (`1 ->` equivale a `i == 1 ->`, e assim por diante). É açúcar sintático para executar um match por iteração.

#### As três formas do sujeito de match

O sujeito de match pode ser um **valor**, uma **expressão** ou um **bloco**:

| Forma | Escrita | Como é avaliado |
| --- | --- | --- |
| Valor | `x: { ... }` | Corresponde diretamente a `x` |
| Expressão | `x + 1: { ... }` | Calcula primeiro o valor da expressão e depois corresponde |
| Bloco | `{ ... }: { ... }` | Executa um corpo de instruções e corresponde ao resultado de `?=` no seu interior |

As duas primeiras são **expressões**: podem servir como instrução e também ser usadas como valor (`v = x: { ok -> it, -> 0 }`).
A terceira **é apenas uma instrução** — não é expressão, não pode ser atribuída, não tem valor de fim de bloco; `-> _` serve para descartar o resultado.
`no fmt` faz a ida e volta de `}: {` sem alterações, e a regra de linhas em branco entre braços é a mesma de um match normal.

##### Sujeito de expressão (`expr: { ... }`)

O `: {` de `expr: { ... }` tem precedência **superior** a `=`: no lado direito de uma atribuição, é toda a RHS, e não
«primeiro concluir `r = <expr>` e depois vir uma instrução match independente». Assim, a aritmética de option pode ser desembrulhada numa só etapa:

```no
#{overflow=wrap}
rem = seconds % 3600: {
    ok -> it

    -> 0
}

#{overflow=wrap}
sec = rem % 60: {
    ok -> it

    -> 0
}
```

Isto equivale à forma de três etapas que antes era obrigatória: `rem-opt = seconds % 3600` / `rem = 0` /
`rem = rem-opt: { ok -> it  -> 0 }`. A primeira atribuição pode ter tipo explícito
(`rem i64 = seconds % 3600: { ... }`) e a reatribuição escreve-se diretamente `rem = ...: { ... }`;
ambas são match de valor único, com o tipo do resultado determinado pela expressão de cauda do braço.

Semântica e limitações:

- Quando o sujeito é aritmética de option D6 (`+ - * / %`), o resultado é `?T`. O match normaliza-o num temporário sintético
  `__match_subj_L_C ?T`: o sujeito é **avaliado exatamente uma vez**, `ok -> it` vincula o payload,
  a condição usa comparação da tag da opção e `-> 0` cobre `nil` / `err`. O nome sintético não aparece em diagnósticos nem
  na saída de `no fmt`.
- O resultado do match é **T simples** e é registado numa tabela semântica auxiliar; por isso se pode continuar a escrever
  `sec = rem % 60: { ... }`, e vários match consecutivos de valor único não perdem o payload.
- O match de valor único só é acrescentado em **contexto de valor** (lado direito de atribuição／`?=`／atribuição composta) e `: { ... }`
  tem de ser realmente um bloco match. Instruções condicionais de nível de instrução como `cond: { body }` e literais de struct
  continuam nos caminhos de análise existentes, sem alteração.
- **Atribuição com destino de campo também é contexto de valor** (desde 2026-11-09): em `d.month = mp - 9: { ... }` e
  `b.n ?= b.n + 1: { ... }`, o `: {` liga-se ao **valor do lado direito**, não a toda a atribuição. Antes, estes dois
  ramos omitiam empurrar `CTX_EXPR` ao analisar o RHS (destinos identificador via `let` empurravam; `obj.f =` / `a[i] =`
  não), pelo que `: {` era apanhado ao nível de instrução e aplicado por fora da atribuição inteira, analisando-se como
  `match(sujeito = (d.month = mp - 9))`: a atribuição executava como antes e desembrulhava implicitamente o `?T` para o campo,
  enquanto os braços `ok -> it` / `-> 0` ficavam como código morto. Agora o valor do braço volta realmente para o campo.
- Destinos de índice (`a[i] = …: { ... }`) têm a mesma forma, mas D9 já proibia escrita direta por subscrito, pelo que esta escrita
  continua a ser rejeitada pelo checker com `idxwrite`, devendo ser reescrita como place-match (`a[i]: { ok -> it = v }`)
  ou `?=`.
- `no fmt` mantém `expr: { ... }` tal como está, sem o dividir em duas instruções (atribuição + match independente).
- **Limite de visibilidade de tipo**: para normalizar um sujeito de aritmética de option para `?T`, os tipos declarados de ambos os operandos
  têm de estar estaticamente visíveis para o parser. Se um operando vier de uma cadeia de métodos profunda (cujo tipo de recetor nem a replay da AST consegue inferir),
  acrescente um tipo explícito na sua declaração, por exemplo `e i64 = flr.to-i64()`; caso contrário a normalização é
  conservadoramente ignorada, `ok -> it` não tem vinculação, o MIR preenche com zeros e o valor observado é sempre 0. É o que acontece
  em `f64.to-str-general` de `src/std/number.no`: sem essa anotação, `fmt.fmt-f64(2.5, '')` degradaria de
  `'2.5'` para `'3'`. A regressão está fixada em
  `src/parser/value_match_expr_subject_test.go::TestValueMatchExprSubjectASTAnnotatedOperand`
  (sem a anotação, fica vermelho).

`src/std` converteu, a 2026-11-09, **122 sites em 22 módulos** de desembrulho de option para esta forma de uma só etapa:
além da forma de três etapas original (`x-opt = expr` / `x = 0` / `x = x-opt: { ... }`), cobre também a forma de duas etapas
(sem inicialização a zero), a primeira atribuição com tipo explícito (`x i64 = expr: { ... }`) e a forma em que a variável intermédia
é declarada em pares várias vezes dentro da mesma função. Após o recolhimento, o sujeito pode ser uma chamada de método, um índice ou aritmética de option;
`ok -> it`／`ok(v) -> v`／braços constantes／`-> _` foram todos medidos como equivalentes, e `no fmt` é idempotente nestes sites.
A inferência de tipo do compilador trata também do caso de «resíduo com o mesmo nome global em que a tabela semântica encontra `?T`, mas a declaração é afinal `i64`»,
 pelo que `ok -> it` não perde a vinculação por causa disso.

Situações mantidas (9 sites): ① o match interno reescreve a própria variável `-opt` (que é o sujeito de um match de valor único posterior);
② duas camadas de match de option encadeadas; ③ match de valor único com expedição por valor (como errno → mensagem, não desembrulho de option);
④ há outras instruções entre a declaração e o match, ou a variável intermédia é um resultado acumulado entre ramos
(o recolhimento alteraria o momento da avaliação).

##### Sujeito de bloco (`}: {`)

O sujeito de bloco envolve um conjunto de instruções, permitindo que **funções sem parâmetro de resultado `?T`** tratem no local o lançamento de `?=`:

```no
; d i64: divisor, 6/d com 0 dá err
f = (d i64) {
    {
        v ?= 6 / d; a falha já não exige que a função tenha parâmetro de resultado ?T
        print(v.to-str()); não executa em caso de falha
    }: {
        err -> print(it.msg())

        nil -> print('nil')

        -> _; descarta o resultado
    }
    print('continua como habitual'); o fluxo continua depois de os braços terminarem
}
```

Semântica:

- O `?=` no interior do corpo é capturado pelo **mais próximo** match de bloco (quando aninhado, o mais interno tem prioridade; mesmo que a função contenha um parâmetro de resultado `?T`, o match de bloco tem prioridade). A captura é recursiva: `?=` em blocos nus, corpos de if, corpos de loop e braços de match contam todos; uma definição de função é uma fronteira, um literal de função não.
- O que é correspondido é o `?T` de três estados, seguindo a **verificação de exaustividade** do match de option padrão: sem braço de fallback, mesmo que `ok ->` seja inalcançável em tempo de execução, é preciso completar os três braços `nil` / `err` / `ok`.
- O corpo termina sem problemas (zero `?=` ou todos bem-sucedidos) ⇒ corresponde a `nil`; algum `?=` falha ⇒ corresponde ao `nil` ou `err` dessa vez (o `it` do braço `err` é o `err.err` completo, ver〈O tipo de `it`〉).
- **Uma falha para todo o corpo**: as instruções seguintes e as iterações seguintes do loop deixam de executar, e os braços correm uma única vez. É um «bloco de paragem», não um `return` de função; os seus próprios `return` / `break` / `continue` não são afetados.
- A expansão é composta inteiramente por AST central já existente (variável local `?T` sintética + cadeia de guardas `if` + match normal), sem novos nós HIR / MIR. Os nomes sintéticos não aparecem em diagnósticos nem na saída de `no fmt`.

> Divisão de trabalho com a captura local por `=`: `=` **guarda a falha na variável de destino** e o fluxo continua; `}: {` **converge a falha num único match** e para o resto do corpo. Quando é preciso «continuar após a falha», use `=` + match posterior; quando é preciso «saltar para um braço de limpeza assim que falha», use `}: {`.

##### Escolher entre match de bloco e match de valor único

A mesma coisa — trazer o resultado de uma aritmética de option de volta para uma variável simples — tem duas formas. **Quando o corpo do bloco tem apenas esta instrução, são equivalentes; logo que haja outras instruções no corpo, ou que o RHS possa falhar noutro lado, a semântica diverge.**

```no
; A：match de bloco + `?=` («try» ao nível do bloco)
{
    #{overflow=wrap}
    np ?= np + 1
}: {
    err -> _

    -> _
}

; B：match de valor único vincula diretamente o destino (ao nível da instrução)
#{overflow=wrap}
np = np + 1: {
    ok -> it

    -> _
}
```

Com `#{overflow=wrap}`, o resultado de `np + 1` é **sempre `ok` (valor que envolve)**; ambas geram apenas um `add i64` nu, sem ramo de verificação de overflow, e os braços de falha são código morto para «a própria adição» — é por isso que parecem «totalmente equivalentes» na maioria dos sites. A diferença real está no **âmbito** e no **caminho de falha**:

| Dimensão | A：match de bloco + `?=` | B：match de valor único |
| --- | --- | --- |
| Posição sintática | **Instrução**: sem valor de cauda, não pode ser atribuída | **Expressão**: pode ir no RHS de atribuição, destino de campo, RHS de `?=` |
| Ao falhar, o que é escrito no destino | **Valor zero** (`i64` → `0`, `str` → `''`) | Determinado pelo braço de fallback: `-> _` **não escreve** (preserva o valor), `-> 0` escreve 0, `-> sum` reescreve o valor antigo |
| Instruções restantes do corpo | **Todas ignoradas** (incluindo iterações seguintes do loop) | Executam normalmente |
| Um braço `err` cobre | **Todos** os `?=` do bloco | Apenas esta instrução |
| Destino com declaração de tipo | ❌ `q i64 ?= …` é erro de análise (`yh8904fm`) | ✅ `q i64 = …: { … }` |
| RHS é constante dobrável | ❌ `s8gh6opt` (`4 / 2` dobra para plain, deixa de ser option) | ✅ O sujeito é normalizado para `?T` na mesma |
| Pré-requisito de uso | A função tem parâmetro de resultado `?T`, ou está dentro de um match de bloco (caso contrário `l86f91me`) | Nenhum |
| Braço em caso de sucesso | Também entra num braço uma vez (corpo termina e corresponde a `nil`); o braço só deve conter descarte ou processamento puro | Só o braço `ok` corre |

Ordem de escolha:

1. **Instrução única, falha tratada como 0, nada mais a executar a seguir** → equivalentes, prefira B: menos um nível de indentação e menos uma escrita de valor zero; `no fmt` é idempotente em ambas as formas.
2. **Uma série de instruções fallible que partilham o mesmo tratamento de erro** (ler ficheiro → analisar → reescrever) → A: um `?=` por linha, partilhando o mesmo braço `err ->`, e a primeira falha para no braço de limpeza.
3. **A falha deve preservar o valor original／substituir por um fallback concreto／deixar as instruções seguintes correrem** → B (`-> sum`, `-> 0`, `-> it`).
4. **O valor ainda vai participar numa operação** (RHS de atribuição, destino de campo, RHS de `?=`) → B: A é uma instrução e não tem valor.
5. **Processar elemento a elemento, e a falha de um não deve arruinar o lote** → B; **o lote inteiro falha junto e deve abortar** → A (para até as iterações do loop).

Dois escolhos fáceis de pisar:

- 🔴 **Posição da anotação**: `#{overflow=wrap}` tem de ser escrito **dentro do bloco**, imediatamente acima da linha do `?=`. Escrevê-lo fora do match de bloco (acima do `{`) **não tem efeito** — mediu-se que o overflow continua a devolver `err` e `np` é zerado pelo `?=`.
- 🔴 **Não copie o padrão A num acumulador**: `sum ?= 1 / (i - 1)` apaga também o `sum` **já acumulado** na iteração em que falha (escrita do valor zero de `?=`). Para preservar o valor acumulado, use o `-> sum` de B.

#### Diretrizes de estilo para Match

```no
#{fmt-skip}
; ❌ Evitar corpos de braço repetidos
w = tls-c.send(req)
w: {
    nil -> {
        tls-c.close()
        return
    }

    err -> {
        tls-c.close()
        return
    }

    ok -> n = it
}
```
```

```no
; ✅ Lógica comum no -> catch-all
w = tls-c.send(req)
w: {
    ok -> n = it

    -> {
        tls-c.close()
        return
    }
}

; ✅ Ou o contrário: braços simples nomeados, lógica complexa no ->
val: {
    nil -> return

    err -> log(it.msg())

    -> {
        n = it
        total = total + n
        process(n)
    }
}
```

```no
; Uma instrução — sem chavetas
val: {
    ok -> print(it)

    -> print('empty or error')
}

; Várias instruções — chavetas obrigatórias
val: {
    ok -> {
        n = it
        total = total + n
    }

    -> {
        log('failed')
        return
    }
}
```

```no
; Vinculação implícita de it
val: {
    ok -> process(it); it = o valor desembrulhado

    err -> log(it.msg()); it = o erro estruturado err.err (não uma str)

    -> log('empty'); catch-all, aqui nil
}
```

```no
#{fmt-skip}
; Âmbito de it: match aninhados restauram camada a camada
; (o nome de função de marcador de posição é keep — `use` é palavra-chave de importação e não pode ser nome de função;
;  e um braço -> nu lê it quando nil/err não estão ambos reivindicados, o que é erro de compilação — por isso todas as camadas escrevem ok ->)
outer: {
    ok -> {
        keep(it)            ; it externo
        inner ?T = f(it)
        inner: {
            ok -> keep(it)  ; it interno = valor desembrulhado de inner
        }
        keep(it)            ; ✅ após o braço interno, it voltou ao valor externo
    }
}
```

> **Atenção**: a restauração só acontece em match **aninhados**. Após o match mais externo terminar, `it` mantém o valor do último braço;  
> não use `it` fora do match. Quando precisar de o usar entre camadas, guarde-o numa variável com nome (`doc = it`) ou use a vinculação por desconstrução  
> `ok(v) -> ...` para ligar o payload ao seu próprio nome.

> **Melhor prática (preferir `it` no desembrulho de option)**: quando o valor correspondido é uma **opção `?t`**, prefira escrever `ok -> ...(it)...`  
> em vez de `ok(v) -> ...(v)...` — não introduz um nome de vinculação extra e evita naturalmente o problema de «variáveis com o mesmo nome em braços diferentes trocarem de tipo no codegen».  
> Só duas situações exigem a **vinculação nomeada** `ok(v) ->`: (1) o valor correspondido é uma **variante de enum definida pelo utilizador** (como  
> `res { ok(v str) }` ou `shape { rect(w, h) }`) — aí `it` é o **enum inteiro** e não o payload, pelo que só se pode obter o payload por nome;  
> (2) é preciso continuar a usar o valor **depois de um match aninhado** — o match interno religa `it`, pelo que use vinculação nomeada ou copie antes para uma local com nome.

### Escrita por subscrito exige desembrulho explícito

A leitura por índice devolve uma opção (fora dos limites dá `err`, `code.index-out`), pelo que **atribuir diretamente a um subscrito transformaria uma escrita fora dos limites numa escrita não verificada**, o que não é permitido — é obrigatório escolher uma de duas opções:

```no
#{fmt-skip}
; ❌ Não permitido: a[1] = 2
```
```

```no
; ✅ place-match: trate você próprio o braço de fora dos limites (it = posição do elemento, legível e gravável)
a[1]: {
    ok -> it = 2

    -> _
}

; ✅ Lançamento do erro: quando a função tem parâmetro de resultado option (fora dos limites → lança)
a[1] ?= 2
```

- Quando o compilador consegue provar que o subscrito está dentro dos limites (array de comprimento fixo + subscrito constante, variável de loop provavelmente dentro dos limites, etc.), **a verificação é eliminada internamente e a atribuição é direta, sem custo em tempo de execução**; mas, sintaticamente, a forma de desembrulho continua obrigatória — o utilizador não tem qualquer isenção.
- map (como `[str]i64`) **não está de forma alguma sujeito a esta regra**: `m['b'] = c` escreve-se diretamente, sem place-match nem `?=` — o subscrito de map é uma **chave** e não uma posição, e a escrita é put/insert (chave ausente insere, chave existente substitui), sempre definida, não existindo «escrita fora dos limites»; bases de campos de struct (`self.keys[i] = v`) também não estão, por agora, sujeitas a esta regra.
- O lado de **leitura** de map continua a usar a interface de option (exatamente a mesma regra de `a[i]` para arrays/vec/slice): `x = m[k]` dentro de uma função dá `?V`, com `nil` quando a chave não existe. `m[k]` é sinónimo de `m.get(k)` e `m[k] = v` é sinónimo de `m.put(k, v)`, podendo as duas formas ser livremente misturadas.
- `m[k] ?= v` é um **erro de compilação** (não apenas redundante): o braço ok de `?=` é «ler o elemento e voltar a escrevê-lo na **posição**», mas o `?V` lido de map é apenas uma cópia de `get`, e escrever nele não regressa ao map. Escreva diretamente `m[k] = v`. A forma de place-match `m[k]: { ok -> it = v }` continua disponível (equivalente a put).

### A variável de iteração também é a «posição» do elemento

`x <- a` (em que `a` é `arr` / `vec` / `slice` / array de comprimento fixo) vincula `x` à **própria posição do elemento**, e não a uma cópia carregada em cada iteração:

- **Ler** `x` volta a ler `a[__i]` — na mesma iteração, escrever e depois ler permite ver o novo valor; `x = x + 1` é ler-modificar-escrever;
- **Escrever** `x = v` cai de volta em `a[__i]`;
- O `it` num braço de match é um **alias** dessa mesma posição (válido apenas nesse braço), pelo que `x: { ok -> it = v }` é totalmente equivalente a `a[i]: { ok -> it = v }`.

```no
; Os dois trechos seguintes dão o mesmo resultado: cada elemento de a passa a valer 5
a []i64 = [0, 0, 0]
i <- [0..3): {
    a[i]: {
        ok -> it = 5

        -> _
    }
}

b []i64 = [0, 0, 0]
x <- b: {
    x: {
        ok -> it = 5

        -> _
    }
}
```

- Quando o próprio elemento é uma opção (`[]?T`), `it` é o **payload**, que é automaticamente reembalado em `ok` na escrita.
- Após o braço terminar, o alias deixa de ser válido: escrever `it = v` depois do match não regressa ao contentor.

**Situações em que não é posição** (a escrita fica apenas local):

- `c <- s` (`str`): o elemento é o **ponto de código** descodificado, não havendo célula de armazenamento correspondente na cadeia.
- `k <- m` (map): o subscrito é uma **chave** e não uma posição, pelo que a escrita não cai de volta na tabela.

### `it` só pode ser usado em braços «provavelmente de um único caso»

O braço catch-all `->` recebe todos os casos não reivindicados explicitamente por braços anteriores. Só quando o que resta é **exatamente um caso**
é que `it` tem um significado único e inequívoco:

```no
; ✅ -> é ok (nil / err já foram reivindicados)
v: {
    nil -> log('empty')

    err -> log(it.msg())

    -> process(it)
}

; ✅ -> é err (ok / nil já foram reivindicados)
v: {
    ok -> process(it)

    nil -> log('empty')

    -> log(it.msg())
}
```

```no
#{fmt-skip}
; ❌ -> pode ser nil ou err — erro de compilação
v: {
    ok -> process(it)

    -> log(it)
}
```
```

Basta acrescentar `nil ->` / `err ->`, ou mudar para `ok ->`, para passar. Enum e match nu (`{ cond -> ... }`)  
não estão sujeitos a isto: `it` refere-se ao próprio valor correspondido, sem possibilidade de nil / err.

> O `it` do braço `err ->` é **sempre o valor do erro (`err.err`)**, independentemente do tipo de elemento da opção —  
> `option { ok(v t), nil, err(e err.err) }`. Assim, mesmo quando se corresponde uma opção escalar como `?i64` / `?bool`,  
> o `it` no braço `err ->` é um erro estruturado: leia a mensagem com `msg = it.msg()`,  
> compare códigos de erro com `it.is(code.io)`, e o local de ocorrência está em `it.file` / `it.line` / `it.col`  
> (injetados automaticamente pelo compilador no ponto de construção de `err(...)`).

```no
; ✅ Padrão combinado de option: nil || err -> body
; Partilha o mesmo ramo quando a opção é nil ou err
val: {
    nil || err -> {
        cleanup()
        return
    }

    ok -> process(it)
}

; ✅ Também se pode misturar com -> catch-all
val: {
    nil || err -> log('failed')

    ok -> process(it)
}
```

### Pipeline de curto-circuito (`->`)

`->` é um **operador de pipeline de curto-circuito associativo à esquerda**, com semântica fixa, **não afetada por existir ou não uma atribuição por fora, nem por estar dentro de um braço de match**. Instrução nua, corpo de uma linha de um braço de match e RHS de atribuição partilham a mesma lowering.

Os nós dentro do pipeline dividem-se em três tipos:

| Nó                                | Efeito no state do pipeline                             |
| --------------------------------- | ----------------------------------------- |
| Chamada com efeito colateral (sem retorno, como `print('A')`)         | Executa, **não altera o state**; se antes era Ok, continua para o nó seguinte        |
| Chamada que retorna `?T` (pode falhar, como `might-fail()`) | Executa, **substitui o state**; assim que se torna nil/err, **todos os nós seguintes são ignorados** |
| Expressão de valor normal no fim                          | Só é avaliada se o state continuar Ok                        |

**Só os nós que retornam `?T` têm capacidade para mudar o pipeline para falha**; efeitos colaterais como `print` nunca o fazem. Por isso «uma cadeia inteira de `print`» parece não ter curto-circuito — na verdade é apenas não haver nenhum nó capaz de gerar falha.

```no
; Sem nó que possa falhar → tudo executa
ok -> print('A') -> print('B')

; might-fail() retorna ?T → em caso de falha, print('B') não executa
ok -> print('A') -> might-fail(x) -> print('B')

; RHS de atribuição: a mesma semântica
r = print('E') -> might-fail(x) -> 42
```

**Esclarecimento de fronteiras:**

1. A semântica de `->` é única; instrução nua e expressão de atribuição partilham a mesma lowering de pipeline, não havendo mudança de modo conforme a atribuição existe ou não por fora.
2. **O `->` no corpo de uma linha de um braço de match é continuação do pipeline, não um novo braço**; os braços separam-se por **quebra de linha**:

```no
; ✅ Um braço, o corpo é um pipeline → as duas linhas imprimem
ok -> print('A') -> print('B')

; ✅ Dois braços (separados por quebra de linha) → se acertar ok, não imprime 'miss'
v: {
    ok -> print('hit')

    -> print('miss')
}
```

> ⚠️ Os braços separam-se por quebras de linha, pelo que `pat -> A -> B` escrito na mesma linha é o **pipeline de um único braço**, não «if A else B». Quando precisar de if/else, escreva os braços em linhas separadas ou use um grupo de curto-circuito envolvido em `{}`.

1. **O pipeline pode produzir um valor**: quando escrito no RHS de uma atribuição, o resultado do pipeline é o **nó de valor final** (só avaliado se o state continuar Ok).

```no
n = 7
x = 1 > 2 -> 42; condição falsa → x mantém o valor original 7
y = 2 > 1 -> 42; y == 42
s str = 'old'
s = 1 > 2 -> 'new'; curto-circuito → s continua 'old'
r = print('E') -> might-fail(bad) -> 99; falha a meio → r mantém o valor original
```

Em caso de falha (condição falsa ou curto-circuito por um nó `?T`), **a atribuição não tem efeito**: a variável mantém o seu valor original; se for uma declaração pela primeira vez, fica com o valor zero do tipo. Isto é coerente com a forma de instrução — `{ 1 > 2 -> x = 42 }` também não altera `x`.

O tipo do valor é inferido pela expressão de cauda do braço (`str` / `i64` / `?T` são todos possíveis), partilhando a mesma inferência da forma de obtenção de valor de match já existente `x = subject: { arms }`.

### If / Else

O grupo if-else (curto-circuito) **tem** de ser envolvido em `{}`. A primeira condição correspondida vence e as condições seguintes não são verificadas.

```no
; Vários ramos (nova forma recomendada) — cadeia if-else de curto-circuito
{
    a == 1 -> {
        a = 1
        b = 2
    }

    a == 2 || a == 3 -> do-something()

    -> {
        c = 0
    }
}

; if único (preservado) — if independente, sem curto-circuito
x == 1 -> do-something()

; Expressão ternária condition ? true-value : false-value
c = flag ? 1 : 2
max = sum > 10 ? sum : 10
```

**Regras importantes:**

1. **Grupo de curto-circuito** — vários `cond -> body` envolvidos em `{}` formam uma cadeia if-elif-else, executando-se apenas o primeiro ramo correspondido.
2. **if independente** — escrever `cond -> body` diretamente no corpo de uma função/loop (sem envolver em `{}`) é um if independente, não faz curto-circuito com ifs-then adjacentes.
   **Assim que aparece um else, é obrigatório formar grupo** — um braço de fallback `->` numa quebra de linha escrito fora das `{}` é **erro de compilação**: um `cond ->` isolado com `->` reporta `k3r7mbqd`; quando há braços `cond ->` irmãos acima dele, reporta `m7vq3xrt` (a mensagem indica o número de braços perdidos). O analisador só dobra o `->` de quebra de linha no **if nu mais próximo**, e os outros braços ficam silenciosamente sem efeito — o que se lê literalmente é totalmente diferente do que é realmente compilado, pelo que isto é uma regra e não um aviso. Envolva todos os braços em `{ ... }`; **um `cond -> body` isolado sem else continua a poder ser escrito nu**.
3. **Proibição de misturar** — os filhos diretos dentro de um grupo de curto-circuito `{}` têm de ser todos ramos `cond -> body`, não podendo incluir instruções normais.

```no
#{fmt-skip}
; ❌ Não faz curto-circuito — são ifs independentes, todas as condições são verificadas
git-dispatch = (cmd str) {
    cmd == 'a' -> {
        fa()
    }
    cmd == 'b' -> {
        fb()
    }
}
```
```

```no
; ✅ Curto-circuito — envolvido em {}, o primeiro correspondido vence
git-dispatch = (cmd str) {
    {
        cmd == 'a' -> {
            fa()
        }

        cmd == 'b' -> {
            fb()
        }

        true -> {
        }
    }
}
```

```no
#{fmt-skip}
; ❌ if/else escrito fora do grupo de curto-circuito — erro de compilação
; O `-> break` de quebra de linha só se liga a `c == 43`; após acertar `c == 45`, ainda é cortado pelo break
;   c == 45 -> {
;       exp-sign = -1
;       i = i + 1
;   }
;   c == 43 -> i = i + 1
;   -> break
```
```

```no
; ✅ O mesmo código escrito como grupo if — os braços são mutuamente exclusivos e `->` é o verdadeiro braço de fallback
{
    c == 45 -> {
        exp-sign = -1
        i = i + 1
    }

    c == 43 -> i = i + 1

    -> break
}
```

```no
#{fmt-skip}
; ❌ match-block misturado com instruções normais → erro de compilação
{
    cmd == 'a' -> {
        fa()
    }
    print(cmd)
    cmd == 'b' -> {
        fb()
    }
}
```
```

```no
; ✅ Instruções normais vão para dentro do corpo do ramo
{
    cmd == 'a' -> {
        print(cmd)
        fa()
    }

    cmd == 'b' -> {
        fb()
    }

    true -> {
    }
}
```

### Programação assíncrona: grupo de coroutines `awy { ... }` + `co`

O Nolang tem apenas uma sintaxe de concorrência de superfície: o grupo de coroutines `awy { ... }`, dentro do qual se derivam tarefas com `co`.
A palavra-chave `run` foi removida (`run` é agora um identificador normal); o prefixo de espera `awy <handle>` e os grupos de coroutines de bloco nu em posição de instrução foram ambos abolidos.
**Escrever `co` fora de um grupo é erro de compilação** (`E_CO_OUTSIDE_GROUP`).

`co` tem duas formas; ambas iniciam apenas **processos** e nenhuma produz valor:

```no
; Processo: só escreve no canal, sem declarar valor de retorno
produce = (ch chan[i64], v i64) {
    ch <- v
    ch.close()
}

ch chan[i64] = with-cap(2)
awy {
    co produce(ch, 1)          ; ① spawn de processo：co f(args)
    co {                       ; ② spawn de closure：co { ... }
        ch <- 2
        ch.close()
    }
}
sum = 0
i <- ch: {
    sum = sum + i
}
print(sum.to-str())            ; => 3
```

#### ① spawn de processo: `co f(args)`

`f` **não deve declarar valor de retorno**. O resultado do spawn não tem destino (`co` não é um valor), pelo que uma função com valor de retorno
chamada por `co` reporta `c0retval`:

```no
worker = (n i64) (r i64) { r = n }   ; tem valor de retorno declarado
awy { co worker(1) }                  ; ✗ c0retval
```

O `self` implicitamente devolvido por um método **não conta** como valor de retorno — é o parâmetro de saída recetor que todos os métodos têm, existindo para permitir encadear chamadas, pelo que `co obj.flush()` é perfeitamente normal:

```no
queue {
    n i64
}
queue.flush = () {          ; implicitamente (self queue), sem outros valores de retorno
    self.n = 0
}
awy { co q.flush() }        ; ✓
```

Quando é preciso um resultado, use um canal: o processo envia o valor para o canal e faz `close()`, e o chamador esvazia-o fora do grupo. Isto não é uma sugestão de estilo
mas uma regra — o resultado de uma chamada spawned só é legível depois de o grupo ser join, e o local onde é lido não é o ponto de join; é exatamente para isto que `chan` existe.

#### ② spawn de closure: `co { ... }`

`co { ... }` entrega um bloco de processo a uma coroutine para execução no local. As **variáveis livres no bloco são capturadas por valor**
(instantâneo no momento do spawn, a mesma semântica da cópia profunda dos argumentos num spawn normal):

```no
seed = 33
ch chan[i64] = with-cap(1)
awy {
    co {
        ch <- seed
        ch.close()
    }
}
r = 0
<- ch: { ok -> r = it }
print(r.to-str())          ; => 33
```

#### `co` não é valor: só pode constituir uma instrução por si só

`co` produz void, pelo que tem de preencher uma instrução inteira. No RHS de atribuição, na posição de argumento ou na posição de valor de retorno, dá sempre
`E_CO_NOT_STATEMENT` (`c0nostmt`):

```no
r = co produce(ch, 1)      ; ✗ c0nostmt
print(co produce(ch, 1))   ; ✗ c0nostmt
```

Além disso, a seguir a `co` só pode vir uma «chamada» ou um «bloco»; outras formas dão `E_CO_NOT_CALL` (`c0notcal`).

#### async incolor: monomorfização automática

`co` é uma sintaxe incolor (colorless): escreve o nome de função sem cor `produce` e o compilador gera automaticamente a variante `produce-async` por monomorfização
e a executa por baixo como coroutine sem pilha com cor. Não escreva o sufixo `-async` à mão no código de aplicação.

Regras:

- **Monomorfização**: `co produce(ch, 1)` gera automaticamente `produce-async` (copiando o corpo de `produce` e mudando o nome). Se `produce` também for chamada de forma síncrona, ambas coexistem.
- **Transitividade**: se um processo também contiver `awy { co child(n) }` no seu interior, esse processo é ele próprio monomorfizado para `-async`, e a cor assíncrona propaga-se para cima pela cadeia de chamadas.
- **Barreira**: instruções dentro do grupo que não sejam `co` (atribuição normal, `print`, loop, `if`, **leitura de canal**…) esperam primeiro todas as tarefas em voo. Assim, as dependências de dados no grupo escrevem-se como «primeiro ler o canal, depois usar o valor lido para fazer spawn» — a própria leitura de canal é a barreira, mais legível do que uma dependência implícita de variáveis.
- **O corpo da função não é um grupo de coroutines**: só `awy { ... }` introduz um grupo. `co` no corpo de uma função, num ramo de `if` ou num corpo de loop é sempre rejeitado.
- **Descartar o resultado também espera**: `awy { co side(5) }` ainda faz spawn + await, não deixando tarefas por esperar.

#### Modelo de escalonamento: suspensão real

O clone `-async` não é «correr tudo de uma vez e depois reportar», mas uma **máquina de estados retomável**: as variáveis locais são movidas para um frame de heap pendurado em `%task.frame`, cada ponto de suspensão se torna o terminador do bloco (escrevendo o número de estado no frame e regressando),
e a entrada da função passa a ser uma cadeia de comparações de estado (`__state == k` ⇒ saltar diretamente para o k-ésimo ponto de retoma).

Por isso, três locais **suspendem** em vez de bloquear:

| Ponto de suspensão | Condição de retoma |
|---|---|
| `async-yield()` | Volta a enfileirar-se no fim da fila, à espera de chegar à frente |
| `awy` numa subtarefa ainda por terminar | Acordada quando a subtarefa termina (registada na cadeia `%task.waiters`, um acordar desperta toda a cadeia) |
| Receção de canal vazia (`v <- ch:` / `<- ch:`) | `send` / `close` retiram a cadeia de espera inteira e voltam a enfileirar cada um |

O escalonamento é cooperativo de thread única, com fila de prontos FIFO, pelo que **a ordem é totalmente assertível**, sem necessidade de sleep:
duas tarefas que cedem uma vez cada dão obrigatoriamente `a1 b1 a2 b2` (`tests/coro-yield.no` fixa os códigos de evento).
Um esperador **acordado não significa ter recebido um valor** — um `send` acrescenta apenas um elemento mas pode acordar vários esperadores,
pelo que o ponto de retoma do canal é o próprio bloco de park, que volta a avaliar a legibilidade antes de decidir retirar o valor ou continuar à espera.

> O deadlock é detetável: fila de prontos vazia e a tarefa-alvo ainda por terminar ⇒ o runtime imprime
> `nolang: deadlock: …` em stderr e aborta. Isto é muito mais honesto do que «devolver um valor zero que ninguém enviou».

> **Lowering subjacente** (não é sintaxe de utilizador): cada `co` passa a `OpRun` e, no fim do grupo, são inseridos `OpAwait` para cada tarefa;
> `KRun` / `KAwait` / `OpRun` / `OpAwait` existem apenas em HIR/MIR. A transformação de coroutines é feita pelo `PlanCoroutines` do MIR
> depois de `Analyze` (`src/mir/coro.go`).

Cancelamento: `cancelled()` pode ser usado dentro de uma função de coroutine como auto-verificação cooperativa; `cancel(h)` é uma operação de handle de baixo nível,
que a nova sintaxe não expõe e que o código de aplicação não usa.

### Atribuição múltipla

As funções podem devolver vários valores, recebidos na chamada com atribuição múltipla:

```no
; Definição de função que devolve vários parâmetros de resultado
swap = (a i64, b i64) (x i64, y i64) {
    x = b
    y = a
}

; Atribuição múltipla
a, b = swap(5, 3)

; Usar _ para ignorar valores de retorno indesejados (variável de marcador de posição)
_, b = swap(5, 3); só o segundo valor, ignora o primeiro
a, _ = swap(5, 3); só o primeiro valor, ignora o segundo
_, _ = swap(5, 3); ignora todos os valores de retorno (chamada apenas pelo efeito colateral)

; Também suportado como corpo de braço de match
val: {
    ok -> {
        a, b = parse-pair(it)
    }

    -> return
}
```

## Sobrecarga de operadores

Quando o tipo estático do operando esquerdo define um método `_op` específico, o compilador reescreve automaticamente a operação infixa `a op b` na chamada de método `a._op(b)` durante o lowering. O conjunto de operadores sobrecarregáveis é fechado, com 5 operadores aritméticos binários simétricos:

| Operador | Método de sobrecarga |
|--------|----------|
| `+` | `_add` |
| `-` | `_sub` |
| `*` | `_mul` |
| `/` | `_div` |
| `%` | `_mod` |

```no
big {
    v i64
}

; O tipo de retorno escreve-se ?big, e o compilador trata como payload puro big
big._add = (b big) (c ?big) {
    c = big {
        v: .v + b.v
    }
}

f = (a big, b big) (out big) {
    r big = a + b; o compilador reescreve para a._add(b)
    out = r
}
```

**Convenção de tipo**: o parâmetro de resultado de um método de sobrecarga **escreve-se como `?T`, mas o tipo efetivo é o payload puro `T`** — o compilador não faz empacotamento／desembrulho de option; o corpo do método atribui diretamente um valor `T` a `c`, e o chamador também recebe `T`. Erros como divisão por zero fazem trap em tempo de execução, tal como os tipos numéricos embutidos, sem gerar option.

**Pontos semânticos principais**:

- Só reescreve quando o tipo estático do operando esquerdo define o método `_op` correspondente; tipos numéricos embutidos (i64, f64, etc.) continuam a usar aritmética embutida.
- A chamada de método explícita `a._add(b)` é totalmente equivalente a `a + b`.
- Cadeias aninhadas à esquerda são reescritas nível a nível: `a + b + c` → `(a._add(b))._add(c)`; o tipo do resultado intermédio continua a ser `T`, podendo continuar a participar em operações ou chamadas de método.
- A biblioteca padrão `bigint` usa este mecanismo para suportar `+ - * / %` (ver a subsecção bigint em «Biblioteca padrão > Outros módulos»).

## Arrays e fatias

Os contentores armazenam cópias dos dados; a variável original e o contentor são independentes, eliminando referências penduradas.

**Array de comprimento fixo arr:**

```no
; Usar array de comprimento fixo
a [3] = [1, 2, 3]; array i64 de comprimento fixo 3
a [3]u16 = [1, 2, 3]; array de comprimento fixo com tipo especificado

a[nil]
u16 = [1, 2, 3]; inferência automática do comprimento (tipo especificado)
a[nil] = [1, 2, 3]; inferência automática do comprimento (i64)
```

**Array de comprimento variável vec:**

```no
v = [1, 2, 3]; array de comprimento variável i64
bs = [0x11, 0x22, 0x33]
v []u8 = [1, 2, 3]; array de comprimento variável com tipo especificado

; Pré-alocação (evitar expansões repetidas)
v = with-cap(100); len=0, cap=100 (é preciso fazer push antes de indexar)
v = with-len(100); len=100, cap=100 (pode indexar diretamente)
v = with-cap-len(200, 100); len=100, cap=200 (reserva espaço para expansão)
```

**Fatia slice (vista, não um tipo independente):**

Uma fatia é uma **vista (view)** sobre os dados originais; não copia os dados nem cria um novo tipo independente.  
Internamente, a fatia guarda apenas um ponteiro para o buffer original, o comprimento e a capacidade, pelo que:

- Modificar elementos através da fatia afeta os dados originais, e vice-versa
- A fatia não possui os dados; quando a variável original é libertada, a fatia deixa de ser válida
- O tipo da fatia é determinado pelo tipo original, e os métodos aplicam-se naturalmente, sem necessidade de mecanismo de «herança»

```no
; Suporta arr/vec/str
; Suporta intervalos, com a mesma notação de for <-
nums [5]u8 = [0, 1, 2, 3, 4]

nums[..]; [0 1 2 3 4]   completamente ilimitado, equivalente à fatia inteira
nums[1..]; [1 2 3 4]     sem limite superior, do índice 1 ao fim
nums[..4]; [0 1 2 3 4]   sem limite inferior, do início ao índice 4
nums[2..3]; [2 3]        limitado, ambos os extremos incluídos
nums[1..3]; [1 2 3]      limitado, ambos os extremos incluídos
nums[1..3); [1 2]        fechado à esquerda, aberto à direita
nums(1..3); [2]          intervalo aberto nos dois extremos

; Cadeia (o resultado da fatia é uma vista str, partilha a memória subjacente; o subscrito é uma posição de ponto de código, como s[i])
s = 'abc'
s[1..]; 'bc'
s[1..s.len()); 'bc'
b str = s[0..1]; o resultado da fatia já é str, pode ser atribuído diretamente a uma variável str
```

> A sintaxe de fatia suporta as mesmas 16 combinações de intervalo de match/range-for. No contexto de fatia, intervalos ilimitados baseiam-se no comprimento da fatia: `[..]` significa a fatia inteira, `[a..]` do índice a ao fim e `[..b]` do início ao índice b.

**Tipos e métodos da fatia:**

A fatia não gera um novo tipo independente, sendo apenas uma vista do tipo original (com o ponteiro inicial e o comprimento ajustados).  
Por isso, os métodos do tipo original estão diretamente disponíveis:

| Tipo original           | Tipo da vista da fatia       | Métodos disponíveis                                                                             |
| -------------- | ------------ | -------------------------------------------------------------------------------- |
| `arr` (`[n]t`) | `[]t<range>` | Todos os métodos de `[]t` (como `len`, `push`, `pop`, `contains`, `reverse`, `clone`, `fill`, `to-arr`, etc.) |
| `vec` (`[]t`)  | `[]<range>`  | Os mesmos                                                                               |
| `str`          | `str<range>` | Todos os métodos de `str` (como `to-upper`, `to-lower`, `index`, `contains`, `slice`, `copy`, `fill`, etc.)  |

```no
; Fatia de arr → vista vec, partilha a memória subjacente de arr
a [5]u8 = [0, 1, 2, 3, 4]
s = a[1..4]; s é uma vista []u8, aponta para a memória de a
n = s.len(); slice.len()

; Fatia de vec → vista vec, partilha a memória subjacente de vec
v = [10, 20, 30, 40, 50]
s = v[2..]; s é uma vista []i64
s.reverse(s.len()); slice.reverse

; Fatia de str → vista str, partilha a memória subjacente de str
s = 'Hello World'
sub = s[6..]; sub é uma vista de 'World'
upper = sub.to-upper(); str.to-upper

; Modificar elementos através da fatia afeta os dados originais
data = [10, 20, 30, 40, 50]
view = data[1..4]; view = [20, 30, 40]
view[0] = 99; modifica um elemento de view
; data[1] agora também é 99, porque view partilha a memória de data
```

### Indexação

```no
; Indexação de cadeia → devolve ?char (ponto de código Unicode, não um byte)
c char = s[i]; tipo plain explícito traz o resultado de volta para char (ver〈Indexação segura〉)

; arr, vec para obter elemento
arr[i]; → ?elem
vec[i]; → ?elem

; map para obter value
map[str]
```

> **O resultado de uma leitura por índice é sempre `?T`**: `arr[i]` / `vec[i]` / array de comprimento fixo / `slice` → `?elem`,  
> `str` / `txt` indexados `s[i]` → `?char` (D6, a mesma regra unificada da aritmética de inteiros).  
> Por isso o **tipo inferido de `str[i]` é `?char`**; para recuperar um `char` simples há dois caminhos:  
> escrever um tipo plain explícito na vinculação (`c char = s[i]`, que é curto-circuitado na fase lower e lê de volta um valor plain),  
> ou desembrulhar com match / `?=`.  
> A fatia `str[a..b]` continua a devolver `str` (vista com semântica de ponto de código, partilhando a memória subjacente).  
> `char` pode ser **convertido implicitamente em `str`** (codificado como cadeia de um carácter), pelo que todas as formas seguintes são válidas, sem necessidade de chamar manualmente `char.to-str()`:

```no
s = 'héllo'
c char = s[1]; 'é' (char, cujo ponto de código é 233)
a str = s[1]; 'é' (conversão implícita char → str)
b str = s[0..1]; 'hé' (o próprio resultado da fatia já é str)
msg = 'first: ' - s[0]; char converte-se implicitamente em str ao participar na concatenação (-)
ok = s[0] == 'h'; char converte-se implicitamente em str ao ser comparado com str
```

### Indexação segura (Safe Indexing)

Para `arr` / `vec` / `slice` (`[]T`) / array de comprimento fixo / `str` / `txt`, a indexação `v[i]` **dá sempre `?T`**: quando fora dos limites devolve `err` (`err('index out of range', code.index-out)`, com a localização do código-fonte do subscrito), quando dentro dos limites devolve `ok(valor)`, nunca abortando (runtime error: index out of bounds). É a mesma regra do overflow não anotado em `+ - * / %` de inteiros (ver〈Overflow da aritmética de inteiros〉).

Para usar o `?T`, há apenas três caminhos (**a anotação `#{index-out=DEF}` foi descontinuada e removida no D11**, e uma anotação residual é erro de compilação;
também não existe o atalho «tipo plain explícito recupera plain», sendo `v i64 = a[i]` igualmente obrigado a consumir explicitamente):

**1. Leitura — desembrulho com match**

Um match de valor obtém diretamente o valor, ou um match de instrução atribui no braço `ok`. O `-> _` é o braço «descartar»,
que produz **o valor zero do tipo do elemento** em contexto de valor (i64→0, str→'', byte→0, contentor→contentor vazio).

```no
; Match de valor
x = state[5]: {
    ok -> it

    -> _
}

; Match de instrução
state[5]: {
    ok -> x = it

    -> _
}
```

**2. Escrita — place-match**

O `it` no braço `ok` é a **posição do elemento** (legível e gravável), não uma cópia do valor.

```no
state[5]: {
    ok -> it = x

    -> _
}
```

> **Exceção map:** escrever `m['k'] = 1` **não precisa de desembrulho** — o subscrito de map é uma chave e a escrita é put/insert,
> inserindo se a chave não existe e substituindo se existe, sempre definida, sem «escrita fora dos limites» a guardar. Mas a **leitura de map `m['k']` continua a devolver `?V` e tem de ser desembrulhada**.
> `m[k] ?= v` é **erro de compilação** (o `?V` lido por `get` é apenas uma cópia, e escrever nele não regressa ao map); escreva diretamente `m[k] = v`.

**3. Abreviatura `?=` — lançar o erro para cima**

`x ?= state[5]` (leitura) e `state[5] ?= x` (escrita) são abreviaturas dos dois matches acima: em caso de falha, lançam o option inteiro
para cima. São legais em **uma das duas situações seguintes**:

- A função onde estão tem um parâmetro de resultado `?T` — em caso de falha, escrevem-no no parâmetro de resultado e fazem `return` antecipado;
- A instrução está no corpo de um **match de bloco ao nível de instrução** `{ ... }: { ... }` — capturado pelo match de bloco mais próximo,
  o corpo para e os braços correm uma única vez, sem a função **precisar** de parâmetro de resultado `?T`.

Fora destas duas situações é erro de compilação. Por isso, o fallback padrão quando «o valor de retorno não é option mas quero usar a abreviatura» é envolver o corpo da função num match de bloco:

```no
handle = (x i64) {
    {
        y ?= x + 1
        print(1)
    }: {
        err -> _

        -> _
    }
}
```

**Leitura aninhada**: o match interno volta a vincular `it`, pelo que se usa a vinculação nomeada `ok(it1) -> it1` no interior. A forma exata que o utilizador forneceu
(um match de valor escrito diretamente no lado direito do `it = ...` de um place-match) **volta a escrever corretamente no contentor desde 2026-11-06**: o compilador
eleva esse match de valor a um temporário sintético no lowering e volta a vincular `it`; a regressão está no trecho
`place-write-value-match` de `tests/match.no`. **Forma direta preferida**: vincular o resultado do match de valor diretamente ao nome que se quer usar no fim,
sem o colocar primeiro num temporário descartável e copiar outra vez.

```no
#{fmt-skip}
; ✅ Vinculação direta à variável de destino
at = argt[i]: {
    ok -> it

    -> _
}
s.farg.push(at)

; ❌ Não introduza um temporário descartável para uso único
ixout1 = argt[i]: { ok -> it  -> _ }
at = ixout1
s.farg.push(at)
```

«Elevar o match de valor para um temporário nomeado fora do braço e depois fazer `it = ixout1` dentro dele» **não é equivalente à forma direta**, não sendo uma
escolha de formatação livremente intercambiável: os momentos de avaliação diferem (após elevar para fora do braço, o `nil` / `err` / catch-all externo também lê uma vez primeiro);
`it` só é válido dentro do braço e aponta para a posição do elemento, pelo que a cópia de volta tem de ficar no braço; o temporário nomeado é uma nova vinculação, com o seu próprio
move／propriedade e destruição ao sair do âmbito; e, quando aninhado, o interior tem de usar a vinculação nomeada `ok(it1) -> it1`, não podendo copiar o
`ok -> it` de fora do braço. Só eleve quando for realmente reutilizar o mesmo valor em vários locais, e use um nome de variável com significado, não deixando nomes de marcador de posição
gerados por ferramentas de migração como `ixout…`.

#### Usar `?=` e match de bloco para eliminar os temporários descartáveis restantes

O que foi dito acima aplica-se a «usar uma vez, vincular diretamente». Dando um passo em frente: a indexação segura devolve `?T`, **pelo que, quando o
parâmetro de resultado da função é ele próprio `?T`, nem é preciso escrever o match de valor — `?=` lança diretamente numa linha**:

```no
#{fmt-skip}
; ✅ A função tem parâmetro de resultado ?T: sem temporário, sem match
at ?= argt[i]
s.farg.push(at)
```

**Quando o resultado da função não é option** (o mais comum: função auxiliar sem valor de retorno, ou que apenas escreve de volta num contentor), use esta
forma «semelhante a try» para envolver a instrução:

```no
#{fmt-skip}
handle = (x i64) {
    {
        y ?= x + 1
        print(y)
    }: {
        err -> _

        -> _
    }
    print('continues afterwards')
}
```

⚠️ **Não é uma nova sintaxe `try`, e a linguagem não tem palavra-chave `try`.** É apenas um **match de bloco ao nível de instrução**
(`{ ... }: { ... }`), com sujeito `?T` de três estados; `?=` lança a falha para o **match de bloco mais próximo**
(ver〈Lançamento de erro com `?=`〉e〈As três formas do sujeito de match〉). Em caso de falha, as instruções restantes do corpo do bloco — incluindo
iterações seguintes do loop — são todas ignoradas, os braços executam uma única vez e depois **continua a execução do código a seguir ao bloco**, não é um
`return` de função. `err -> _` descarta o erro e `-> _` descarta o resultado de sucesso como fallback; para tratar o erro, escreva
`err -> print(it.msg())`.

⚠️ **A diferença semântica tem de ficar clara** (medida, não por intuição): quando `?=` falha, **o destino é escrito com o valor zero na mesma** — `i64` dá
`0`, `str` dá `''`, `out[1]` dá `0`. Mas o braço de fallback `-> _` do match de valor único **não escreve no destino**: quando o destino é uma
**nova declaração**, já vem inicializado a zero e parece igual; quando o destino **já tem valor**, os dois divergem (`?=` zerou, `-> _`
preservou o valor original; para zerar é preciso escrever `-> 0`). Outra diferença é o controlo de fluxo: `?=` **ignora as instruções do corpo do bloco posteriores a ele**
(numa função `?T` é um `return` antecipado, num match de bloco é um salto direto para os braços).

Por isso: **quando o corpo do bloco tem apenas esta instrução e o valor antigo antes da falha não tem significado**, escrever o braço de fallback como `-> 0` é
totalmente equivalente (é precisamente a forma da reescrita em lote neste repositório, em que os temporários `ixout…` são maioritariamente novas declarações); mas quando o corpo tem
outras instruções depois do `?=` (contagem de loop, atribuições seguintes) ou o valor antigo do destino é significativo, há divergência e esses sites não podem ser substituídos mecanicamente. A tabela completa de
escolha está em〈Escolher entre match de bloco e match de valor único〉.

Outro ponto fácil de pisar: **o braço também executa uma vez em caso de sucesso**, e o catch-all recebe o valor de sucesso (`-> print('x')` imprime).
Por isso o braço só deve conter descarte ou processamento puro; formas como `err -> _` / `-> _` não têm efeito colateral.

Daqui resulta a ordem para limpar os temporários descartáveis `ixout…`:

1. O valor é usado uma única vez no mesmo local → vincule diretamente ao nome final do destino (ver acima).
2. A função tem parâmetro de resultado `?T` → `DESTINO ?= EXPR`, sem temporário.
3. Caso contrário → coloque a instrução num match de bloco e escreva `DESTINO ?= EXPR`; a escrita por place-match
   (`SUBJ: { ok -> it = temp  -> _ }`) também encurta diretamente para `SUBJ ?= EXPR`.
4. Só mantenha um temporário nomeado quando for realmente reutilizar o mesmo valor em vários locais, e use um nome de variável com significado.

Dois limites: `?=` sem parâmetro de resultado `?T` e fora do corpo de um match de bloco é erro de compilação
(`l86f91me`); **um LHS com declaração de tipo não pode ser usado com `?=`** (`ai i64 ?= a[i]` é erro de análise
`yh8904fm`, `expected assignment operator`). Quando for preciso uma promoção explícita como `byte → i64`, use antes um match de valor direto com declaração de tipo:

```no
#{fmt-skip}
ai i64 = a[i]: {
    ok -> it

    -> _
}
```

> **Âmbito de aplicação:** válido para `arr` / `vec` / `slice` / array de comprimento fixo / `str` / `txt`; só a **base de variável direta**
> da indexação (`v[i]`) é reescrita para a forma segura; `receiver.field[i]` segue o caminho de bounds-check original.
> Uma leitura por índice num operando de comparação compara o payload (`a[i] == 5`), não o `{tag, payload}` inteiro.

> ⛔ **Erro grave:** usar um `?T` como valor normal (como `crc = crc ^ s[i]`, `f(s[i])`, `v i64 = a[i]`) bloqueia
> `no build`, e o compilador sugere usar match ou `?=` (com match de bloco quando não há parâmetro de resultado `?T`).
> O corpus existente já foi migrado de uma vez com `no fmt --fix=index-out`, ferramenta que se retirou com o D11.

> **Garantia central:** fora dos limites nunca falha silenciosamente — ou é tratado por match, ou é lançado por `?=`.

> **Leitura por índice em operandos de comparação** aplica igualmente a indexação segura: `a[i] == 5` compara o payload (mesma regra de `?T == 5`),
> não comparando o `{tag, payload}` inteiro. Quando fora dos limites, lê-se `err` (`code.index-out`), cuja comparação com qualquer valor é **falsa**
> (`a[i] == 0` e `a[i] == 5` são ambos falsos), e **não** se lê memória fora dos limites.
> O mesmo para `s[0] == 'a'`: ao comparar um ponto de código com uma cadeia de um carácter, usa-se a conversão implícita `char → str`.

> **Subscritos aninhados** `a[i][j]`: **as duas camadas são indexação segura**. A externa `a[i]` faz primeiro a verificação de limites e determina a existência;
> só quando existe é que se aplica `[j]`; se qualquer camada estiver fora dos limites, toda a expressão é `err`.
> Por isso `a[oob][0] == 'c'` é falso e não aborta.

> **Comparação de contentores** `[]T == []T` (e `[N]T == [N]T`) é uma **comparação estrutural**: compara primeiro os comprimentos e depois elemento a elemento.
> Quando os elementos são eles próprios contentores (`[][]i64`), a recursão é camada a camada; quando o elemento é `str`, compara-se o **conteúdo** e não o ponteiro data.
> Comparações de ordem (`<` / `<=` / `>` / `>=`) não estão definidas para contentores, suportando-se apenas `==` / `!=`.

> **`nil` só pode ser comparado com option**: `?T == nil` / `?T != nil` são válidos; um não-option
> `T == nil` (`str`, `[]i64`, `[N]T`, `txt`, `i64`, estrutura…) é **erro de compilação** (`q4nilcmp`).
> O motivo não é rigor estéril: `str == nil` é sempre falsa (compara-se a estrutura `%str-long` com 0),
> e `[]i64 == nil` cairia no caminho de comparação de inteiros gerando LLVM inválido (`icmp eq i64 %vec, i64`),
> estando ambas a dar silenciosamente respostas erradas. Para exprimir «pode não haver valor», declare o tipo como `?T` e compare com `nil`.

## Estruturas

Tanto a definição como o literal de estrutura têm de usar a forma de várias linhas, com cada campo numa linha própria, sem vírgulas entre campos e sem vírgula no fim.

```no
user {
    name str
    age i64
}

u = user {
    name: 'Alice'
    age: 30
}
u.name = 'Bob'
u.age = 25
print(u.name)
```

### Estrutura implementa interface

Uma estrutura pode implementar uma ou mais interfaces, com os nomes das interfaces escritos a seguir ao nome da estrutura, separados por vírgulas.

```no
; Implementar uma única interface
user json {
    name str
    age i64
}

; Implementar várias interfaces
file enter, leave {
    path str
    fd i64
}
```

#### Implementação de interface entre módulos

Quando se implementa uma interface definida noutro **módulo**, o nome da interface tem de levar o prefixo do módulo (`ShortName.`). Consulte [Prefixo de chamada entre módulos](module.md).

```no
#{fmt-skip}
; ❌ Errado: db, rows, stmt são interfaces definidas no módulo sql, não se pode omitir o prefixo
db-mysql db {
    fd i64
}
```
```

```no
; ✅ Correto: usar sql.db, sql.rows, sql.stmt
db-mysql sql.db {
    fd i64
}

rows-mysql sql.rows {
    fd i64
}

stmt-mysql sql.stmt {
    fd i64
}
```

> As interfaces embutidas (`enter`, `leave`) e as interfaces definidas no mesmo ficheiro não precisam de prefixo de módulo.

## Métodos

Os métodos são definidos sobre tipos e usam `.` para referenciar o recetor (receiver).

### Sintaxe

```no
#{fmt-skip}
type.method-name = (params) (results) {
    ; . é o recetor
}
```

### Regras

1. O nome do método usa o formato `type.method`, em que type tem de ser um tipo já definido
2. O recetor não precisa de ser declarado explicitamente como parâmetro; referencia-se com `.` no corpo do método
3. Na chamada, usa-se a sintaxe `receiver.method(args)`
4. Os valores de retorno vão no segundo grupo de parênteses, tal como nas funções normais
5. **Desaçúcar da sintaxe**: `type.method = (inputs) (rest-outputs...) {}` é equivalente à função normal  
   `method = (inputs) (self type, rest-outputs...) {}`. Ao chamar `instance.method(args)`,  
   a instance é vinculada ao primeiro parâmetro da lista de saída, `self`; no corpo da função, `.` designa esse `self` da zona de saída;  
   as escritas da função em `self` modificam diretamente a instância original. Os restantes resultados da lista de saída, além de `self`,  
   suportam desconstrução opcional na receção ou descarte direto.

### Exemplo

```no
; Método str
str.to-upper = () (out str) {
    n = .len-bytes()
    b []byte = with-cap-len(n, n)
    i <- [0..n): {
        c ?byte = .byte(i)
        v = c: {
            ok -> it

            nil -> 0

            err -> 0
        }
        {
            v >= 97 && v <= 122 -> b[i] = v - 32

            -> b[i] = v
        }
    }
    out = b.to-bytes-str()
}

; Método char
char.is-digit = () (result bool) {
    result = false
    . >= 48 && . <= 57 -> result = true
}

; Método struct
user {
    name str
    age i64
}

user.greet = () {
    print('Hello, ' - .name)
}

; Chamada
s = 'hello'
u = s.to-upper(); receiver.method()
c char = 5
d = c.is-digit(); receiver.method()
u = user {
    name: 'Alice'
    age: 30
}
u.greet()
```

## Interfaces

```no
; Definir interface
json {
    to-json()
}

; Implementação predefinida da interface
json.to-json = () {
}

noml {
    to-noml()
}

serialize {
    json
    noml
}

serialize = json | noml
serialize = json & noml

; Implementação da interface
user json {
    name str
    age i64
}

; Substituir + chamar a implementação pai
user.to-json = () {

    ; Implementação pai
    ..to-json()
}

user.other = () {

    ; Implementação atual
    .to-json()

    ; Implementação pai
    ..to-json()
}
```

### Fusão de interfaces

Uma interface pode «fundir» os métodos de outras interfaces: os nomes das interfaces pai são escritos no corpo da interface,
**um por linha** (podendo levar prefixo de módulo), sem vírgulas nem parênteses:

```no
json {
    to-json()
}
noml {
    to-noml()
}

serialize {
    json
    noml
}

; Fusão transitiva: roundtrip recebe todos os métodos de serialize, json e noml
roundtrip {
    serialize
    roundtrip-json()
}
```

Regras:

- **Fusão recursiva**: a fusão é transitiva — se `roundtrip` funde `serialize` e `serialize` funde `json` / `noml`, então `roundtrip` tem os métodos de todas as três interfaces.
- **Um por linha, sem vírgulas**: `serialize { json, noml }` tem a mesma forma de um enum de estilo C (`color { red, green }`), pelo que continua a ser analisado como enum, não como fusão de interfaces.
- **Métodos com o mesmo nome**: assinaturas iguais (parâmetros e tipos de retorno coincidentes, o nome do recetor pode diferir) ⇒ deduplica-se, mantendo um; assinaturas diferentes ⇒ erro de diagnóstico.
- **Lado da implementação**: o tipo só precisa de escrever a declaração de implementação (lista prefixada `user serialize { name str }`); os métodos das interfaces pai contam também como implementados, e a verificação de assinatura atravessa as interfaces pai.
- **`no fmt`**: a antiga forma de «lista prefixada» (`serialize json, noml { }`) ainda pode ser analisada, mas `no fmt` reescreve-a para a forma no corpo e `no vet` emite um WARNING.

Diagnósticos (`no vet` e diagnósticos do editor partilham a origem, TraceID `ifmrg001`):

| Situação | Mensagem |
| --- | --- |
| Interface pai não declarada | `interface 'serialize': unknown parent interface 'noml'` |
| Ciclo de fusão | `interface 'a': merge cycle a -> b -> a` |
| Mesmo nome, assinaturas diferentes | `interface 'serialize': merged method 'to-str' has conflicting signatures ('json.to-str', 'noml.to-str')` |

> Escritas como `serialize = json \| noml` são sintaxe de **tipo de união** (union type), sem relação com fusão de interfaces; a fusão de interfaces só afeta «quais os métodos que um tipo deve ter».

### Interfaces especiais

```no
file {
    enter
    leave
}
```

## Enumerações

```no
; red=0, green=1, blue=2
color {
    red,
    green,
    blue,
}

; Em métodos normais, a,b,c são na prática os valores definidos a=0, b=1, c=2... Esta é uma diferença relativamente a outras linguagens.
; Por isso, normalmente não se deve definir várias variáveis com vírgulas.

; Esta é uma enumeração especial, pode ter tipos, vírgulas e aliases
enum-name {
    a t,
    b u,
    c v,
}

; Nota: isto é uma struct normal, vários campos sem vírgulas
struct-name {
    a t
    b u
    c v
}
```

### Enum com tag (com payload)

Uma variante pode transportar campos de payload nomeados, escritos na forma entre parênteses `variante(campo tipo)`; variantes sem payload escrevem-se com o nome nu.  
Uma variante pode ter **vários** campos (payloads com vários campos são representados no heap por uma struct sintética):

```no
#{fmt-skip}
; Enum com tag: as variantes podem ter payload, a tag segue a ordem de declaração 0,1,2...
result {
    ok(v t),
    nil,
    err(e str),
}

; Com valor: campo de payload v, tipo t
; Valor vazio: sem payload
; Erro: campo de payload e, tipo str (enum definido pelo utilizador, sem relação com a opção embutida)

shape {
    circle(r f64),
    rect(w f64, h f64),
    dot,
}
; Um campo
; Vários campos
; Sem payload
```
```

Ao fazer match, usa-se o nome da variante como padrão de braço; variantes com payload podem desconstruir e vincular o payload (vários campos são vinculados posicionalmente um a um):

```no
r result = ok(42)
r: {
    ok(v) -> print(v); vincula o payload de ok a v

    nil -> print('empty')

    err(e) -> print(e)
}

s shape = rect(3.0, 4.0)
s: {
    circle(r) -> print(r); vincula um único campo

    rect(w, h) -> print(w * h); vincula vários campos por posição

    dot -> print('dot')
}
```

As variantes podem ser construídas diretamente em **posição de expressão**, como argumento de função ou payload de outra variante:

```no
x f64 = perimeter(rect(3.0, 4.0)); construção inline e passagem como argumento
o outer = wrap(a(5)); o payload da enumeração é ele próprio outra enumeração
```

> **Namespace e resolução de nomes nus**: internamente, o compilador regista as variantes pelo seu «nome totalmente qualificado» (`módulo.enumeração.variante`,  
> como `option.option.ok` ou `some-mod.my-result.ok`), pelo que variantes com o mesmo nome em enumerações diferentes não entram em conflito.  
> O utilizador escreve apenas o nome nu (`ok`, `rect`), e o compilador completa automaticamente o nome completo **com base no tipo estático da variável correspondida/construída**.  
> Assim, mesmo que duas enumerações tenham ambas uma variante `ok` com ordens de tag diferentes, são corretamente distinguidas.
>
> Também se pode usar a antiga forma `ok t` separada por espaço, equivalente a `ok(v t)` (omitindo o nome do campo).  
> Os nomes das variantes de enum com tag são registados na tabela de variantes de enumeração do compilador, para uso no match e na verificação de exaustividade.

### Enum com tag embutida (`#{buildin}`)

Acrescentar `#{buildin}` antes de um enum com tag indica que é uma **enumeração embutida**: as variantes (nomes, ordem, tipos de payload)
servem para match e verificação de exaustividade, mas a representação subjacente e a construção são fornecidas pelo runtime/builtin,
**sem gerar struct/union visível para o utilizador**. A opção `?t` é declarada exatamente assim:

```no
; src/std/global.no
#{buildin}
option {
    ok(v t),
    nil,
    err(e err.err),
}
```

As tags de `option` são, por ordem, `0=ok`, `1=nil`, `2=err`, coerentes com a representação de opção embutida. O utilizador não precisa de
declarar a opção por conta própria; o `#{buildin}` aqui dá uma única fonte para as variantes da opção embutida (ver «Tipos anuláveis (option)»).

### Anotações de enum e layout de memória

Os enums suportam anotações `#{...}`, na mesma posição dos outros alvos (ver〈Posição das anotações〉): **acima** (linha própria) ou
**atrás** (no fim da mesma linha); escrevê-las **à frente do alvo, na mesma linha** é erro de compilação.

As anotações podem ser colocadas em **dois locais**:

1. **Acima de todo o enum** — afeta a definição do enum em si (como `#{buildin}` ou `#{inline=true}`);
2. **Acima de um único membro ou no fim da mesma linha** — afeta esse membro (tanto o «valor» de um enum de estilo C como a «variante» de um enum com tag).

```no
; ① Enum inteiro: linha própria, acima do enum
#{inline=true}
box {
    full(v i64) #{doc='com valor'},
    empty #{doc='valor vazio'},
}

; ② Variante única: linha própria, acima da variante

; ② Variante única: no fim da mesma linha

; `#{inline=false}`: escreve explicitamente «não usar layout inline»
#{inline=false}
plain {
    a,
    b,
}

color {
    red #{deprecated},
    green #{deprecated},
    blue,
}
; Um único valor de um enum de estilo C também suporta estas duas formas
```

**As anotações não alteram a estrutura do enum**: nomes das variantes, ordem de declaração (tag), campos de payload e tipos não são afetados pelas anotações,
e o match e a verificação de exaustividade permanecem exatamente iguais. As anotações são apenas **metadados** acrescentados à definição.

#### Layout de memória (forma de pilha)

O enum com tag usa um **layout em pilha** (inline stack form) na memória — um pequeno objeto armazenado por valor, com o payload disperso pelo heap:

```llvm
%tenum_<name> = type { iN tag, [K x i64] payload }   ; caso geral
%tenum_<name> = type { iN tag }                      ; todas as variantes sem payload
```

- A largura de `tag` varia com o **número de variantes** e é um `u8/u16/u32/u64` completo:
  `<= 256` → `i8`, `<= 65536` → `i16`, `< 2^32` → `i32`, caso contrário `i64`. O valor discriminador segue a ordem de declaração `0, 1, 2...`.
- O `payload` é uma union **partilhada por todas as variantes** (cada variante escreve os seus campos na mesma área de armazenamento); `K` é o
  número de words de 8 bytes necessários à **variante mais larga**, **não a soma de todas as variantes**, com mínimo 1.
- Dentro de uma variante, os campos são dispostos por **alinhamento natural**, pelo que os deslocamentos dos campos não são necessariamente múltiplos de 8 (`(a char, b char, c char)`
  tem deslocamentos 0 / 4 / 8). Leituras e escritas são sempre endereçadas por **deslocamento em bytes** (`bitcast` para `i8*` e depois `getelementptr`),
  pelo que «o deslocamento escrito» e «o deslocamento lido» são determinados pela mesma tabela de layout.

O tamanho é `8 + 8K` (`tag` é preenchido com alinhamento de 8 bytes do array até um word completo), e só enums sem payload são de tamanho de tag puro:

| Caso | `K` | Tamanho |
| ---------------------------------------------- | ----------------- | ---------- |
| Todas as variantes sem payload | — (sem campo payload) | **1 byte** (`{ i8 }`; 2 / 4 / 8 quando há > 256 variantes) |
| Variante mais larga é `i64` / `i32` / `f64` / ponteiro | 1 | 16 bytes |
| Variante mais larga é `char` (4 bytes) / `bool` (1 byte) | 1 | 16 bytes |
| Variante mais larga é `(a char, b char, c char)` (12 bytes) | 2 | 24 bytes |
| Variante mais larga é `str` / `vec` / `[]T` (descritor de 16 bytes) | 2 | 24 bytes |
| Variante mais larga é uma struct `T` | `ceil(sizeof(T) / 8)` | `8 + 8K` |

Ou seja: **por predefinição começa em 16 bytes**, e só cresce quando o payload de alguma variante é realmente mais largo —
«tamanho fixo de 16 bytes por predefinição» e «tamanho varia com as variantes» são duas faces da mesma coisa. Payloads pequenos como `char` / `bool`
determinam apenas o alinhamento, não fazendo crescer a union: a largura do `payload` é «a variante mais larga», não a soma de todas.

> A única fonte do layout é `src/mir/layout.go`: `tagWidth` determina o `iN`, `enumVariantLayout` determina os deslocamentos dos campos e o tamanho próprio de cada
> variante, e `enumUnionLayout` toma o máximo para obter o `K`. `hir2mir.fillEnumLayout` regista estes números em `TaggedEnumInfo` ao construir a tabela,
> e o emitter apenas lê, sem calcular — os dois lados não podem chegar a respostas diferentes.

#### As três formas de `#{inline}`

`inline` é uma anotação **booleana**, com três formas de escrita, e **o valor é realmente respeitado**:

| Escrita                | Significado                          |
| ----------------- | --------------------------- |
| `#{inline}`       | Abreviatura, igual a `inline=true` (chave isolada é verdadeira) |
| `#{inline=true}`  | Explicitamente verdadeiro (`#{inline=1}` também serve)      |
| `#{inline=false}` | Explicitamente falso (`#{inline=0}` também serve)      |

Valores não booleanos são **erro de compilação** (`#{inline=foo}`, `#{inline='true'}`), não sendo silenciosamente tratados como verdadeiro:  
`ValidateFieldTags` (TraceID `fieldtag1`) reporta tanto para campos de struct como para definições de enum.

Escrever `#{inline=true}` acima de um enum inteiro é uma **marca explícita** do layout em pilha, indicando «esta enumeração usa layout em pilha»;  
o compilador regista-o em `TaggedEnumInfo.Inline`. **O layout predefinido atual já é em pilha**, pelo que quer se acrescente a anotação,
quer se escreva `true` ou `false`, o layout é exatamente o mesmo — a marca serve para tornar a intenção explícita e para servir de
chave de escolha estável se o layout predefinido mudar no futuro.

> `#{inline}` escrito **acima de um enum inteiro** significa «esta enumeração usa layout em pilha»; escrito **numa única variante**,  
> atualmente não altera o layout, mantendo-se apenas como metadados dessa variante (ao contrário dos campos de struct, ver secção seguinte).

#### `#{inline}` em campos de struct

Ao contrário dos enums, `#{inline}` num **campo de struct** tem **efeito real**: determina diretamente se o campo é
**armazenado por valor inline** dentro da struct anfitriã (`%pt`) ou como **ponteiro** (`ptr`).

```no
pt {
    x i64
    y i64
}

holder {
    inl pt #{inline=true}; por valor: a struct cresce dentro de holder
    out pt #{inline=false}; ponteiro: o campo guarda um ponteiro para pt
    bare pt #{inline}; abreviatura, igual a inline=true
}
```

| Escrita                                             | Layout do campo                        |
| ---------------------------------------------- | --------------------------- |
| `#{inline}` / `#{inline=true}` / `#{inline=1}` | **Por valor**, inline no anfitrião (`%pt`)         |
| `#{inline=false}` / `#{inline=0}`              | **Ponteiro** (`ptr`)               |
| Sem anotação                                             | Depende de `NOLANG_FIELD_PTR` (ver tabela abaixo) |
| `#{inline=foo}` / `#{inline='true'}`           | **Erro de compilação** — não é booleano              |

**O layout predefinido sem anotação é determinado por `NOLANG_FIELD_PTR`.** `mir.FieldPtrLayout` é  
`os.Getenv("NOLANG_FIELD_PTR") != ""`, **desligado por predefinição**; ativá-lo inicia a «Phase 1 layout flip»,
mudando o predefinido dos campos de struct de por valor para ponteiro.

| Campo                             | Flag desligada (predefinição)   | Flag ligada (`NOLANG_FIELD_PTR=1`) |
| ------------------------------ | --------- | ------------------------- |
| Sem anotação                            | `%pt` (por valor) | `ptr` (ponteiro)                 |
| `#{inline}` / `#{inline=true}` | `%pt`     | `%pt`                     |
| `#{inline=false}`              | `ptr`     | `ptr`                     |

Ou seja: **a anotação prevalece sempre, independentemente dessa variável de ambiente**; a flag só altera o predefinido «quando não há anotação». Assim, na
configuração predefinida, `#{inline=false}` é «pedir explicitamente um ponteiro», enquanto `#{inline=true}` apenas volta a declarar explicitamente
um layout que já era assim; quando a inversão está ativada, os papéis invertem-se.

O checker tem duas regras (TraceID `fieldtag1`, `ValidateFieldTags`):

1. **Só tem significado em campos de tipo struct.** Campos escalares / `str` / `vec` / array / fatia / map / option /
   ponteiro são sempre armazenados por valor, pelo que escrever `#{inline}` não diz nada e dá erro; escrever
   `#{inline=false}` dá igualmente erro, pois equivale a afirmar um layout contrário ao comportamento do compilador.
2. **Inline por valor não pode formar ciclo.** A struct inlined é armazenada **dentro** do anfitrião, pelo que
   `a { #{inline} b b }` e `b { #{inline} a a }` em simultâneo dariam tamanho infinito e têm de dar erro (a autorreferência direta é um caso degenerado).
   Só `=true` cria uma aresta por valor, pelo que structs mutuamente recursivas são legais quando **ambos os lados escrevem
   `#{inline=false}`**.
   > ⚠️ **«Não escrever anotação» não é o mesmo que «não usar inline».** Na configuração predefinida, não escrever anotação significa inline por valor, pelo que
   > `a { x b }` com `b { y a }` (sem anotações em nenhum) **não é legal na configuração predefinida**, falhando com
   > `opt: error: identified structure type 'b' is recursive` — e este erro atualmente **não** é reportado pelo checker, só rebentando na fase de verificação LLVM. Para escrever tipos recursivos na configuração predefinida, é **obrigatório**
   > escrever explicitamente `#{inline=false}`. Só com `NOLANG_FIELD_PTR=1` é que «não escrever» significa ponteiro.

#### Tipos recursivos: por que é necessário `inline=false`

Uma struct que se inline a si própria por valor teria tamanho infinito, pelo que a **autorreferência** não pode ser armazenada por valor. Na configuração
predefinida (`NOLANG_FIELD_PTR` desligada), os campos de struct são por valor, pelo que, para `node` poder referir-se a si próprio,
a única forma é pedir explicitamente um ponteiro:

```no
node {
    v i64
    next node #{inline=false}; ponteiro: assim node tem tamanho finito
}

n node
n.v = 1
n.next.v = 2
print(n.next.v); 2
```

Se se remover o `#{inline=false}` acima (ou se escrever `#{inline=true}`), o LLVM rejeita diretamente o
tipo: `identified structure type 'node' is recursive`.

O campo de ponteiro **possui** o seu pointee, e o ciclo de vida é tratado pelo compilador:

- O pointee é **alocado preguiçosamente na primeira escrita** (`@__nolang_get_<T>`, com `malloc` + zeroing),
  e não de uma vez ao construir o anfitrião;
- Quando o anfitrião sai do âmbito, `@__nolang_drop_<T>` **verifica se não é nulo e liberta-o**;
- A cópia por valor (`b = a`, passagem como argumento, escrita num contentor) **faz uma cópia profunda** do pointee, pelo que os dois anfitriões
  não partilham a mesma memória nem são libertados duas vezes.

O teste ponta a ponta está em `tests/field-inline-annotation.no`.

### Referência a valores de enum

**Regra: os valores de enum têm de ser referidos de forma qualificada, como `tipo-de-enum.valor`, e não diretamente por valor nu.**  
Isto evita conflitos de nomenclatura e impede que pacotes externos usem diretamente valores concretos.

```no
#{fmt-skip}
; ❌ Errado: usar valores nus diretamente
kind = null
yes = e.is(io)
```
```

```no
; ✅ Correto: usar forma qualificada
kind = json-kind.null
yes = e.is(code.io)
```

> O tipo de enum pode ser usado como tipo de campo de struct, tipo de parâmetro de função e tipo de valor de retorno.  
> Tanto dentro como fora do módulo que define a enumeração, os valores devem ser referidos como `tipo-de-enum.valor`.

## enter/leave

Tipos que implementam as interfaces `enter` / `leave` são chamados automaticamente ao entrar e sair do âmbito:

```no
file enter, leave {
    path str
}

file.enter = () {
    .open()
}

file.leave = () {
    .close()
}

read-file = () {

    ; f.enter() automático
    f = file {
        path: 'data.txt'
    }

    ; usar f
    ; f.leave() automático
    read(f)
}
```

### Tipo anulável (option)

Acrescentar `?` antes de um tipo indica um tipo anulável:

Uma variável de tipo anulável pode conter legalmente um valor vazio/de erro, e o compilador faz as verificações de nulo correspondentes.

`?t` é um enum com tag embutido, cujas variantes são definidas em `src/std/global.no`:

```no
#{buildin}
option {
    ok(v t),
    nil,
    err(e err.err),
}
; tag 0: com valor
; tag 1: valor vazio
; tag 2: erro (err.err estruturado de std)
```

O payload da variante `err` **não é `str`**, mas a struct `err` de `std/err` (`code` / `msg` / `file` / `line` / `col`).
`err('msg', code)` é um construtor alias de `err.new('msg', code)` — o código de erro `code` é um parâmetro **obrigatório**
(os valores predefinidos de parâmetros foram removidos da linguagem); quando não há um código de erro concreto, passe explicitamente `code.unknown`;
os campos de localização são injetados automaticamente pelo compilador no ponto de construção. Escrever apenas um argumento, `err('msg')`, não é este alias,
mas o construtor da variante `err` da opção (ver abaixo).

`#{buildin}` indica que a representação subjacente e a construção são fornecidas pelo runtime embutido (tipo `%option`, construtores `ok(...)`/`nil`/`err(...)`
); a declaração de enum de `option` serve apenas para match e verificação de exaustividade. Consulte «Enum com tag» e «Enum com tag embutida».

#### Layout de memória

`?T` é **dimensionado pelo payload**: cada tipo de payload tem o seu próprio tipo LLVM, cujo nome é derivado do tipo do payload (mapeamento injetivo),
pelo que «tipo de payload igual ⇒ layout igual», e `?i32` / `?i64` / `?bool` partilham o mesmo tipo:

```llvm
%option_<key> = type { i8 tag, [K x i64] payload }   ; inline
%option_<key> = type { i8 tag, [1 x i64] payload }   ; empacotado (payload guarda ponteiro i8*)
```

- A `tag` tem **sempre 1 byte**: `option` só tem 3 variantes (`0=ok`, `1=nil`, `2=err`), caindo no escalão
  «`<= 256` variantes → `i8`». A regra de largura de tag dos enums está em〈Layout de memória (forma de pilha)〉.
- A seguir à tag vem o **preenchimento de alinhamento** (tag de 1 byte → 7 bytes de preenchimento) e só depois o payload.
- O tamanho do payload `K` é determinado pelo tamanho real de `T` e arredondado para cima até um múltiplo de 8, **não é um slot fixo**:
  `?i64` tem 16 bytes (8 + 8), `?str` tem 24 bytes (8 + 16) e `?txt` tem 264 bytes (8 + 256).
- Payloads acima do limiar passam a ser **empacotados no heap**: o `payload` guarda apenas um ponteiro de 8 bytes e a opção inteira tem 16 bytes.

O limiar `option-payload-inline-threshold` conta os bytes de **toda a estrutura da opção** (predefinição **264** = 1 byte de tag +
7 bytes de preenchimento + 256 bytes de payload, mínimo **16**): a condição para o payload ser inline é
`8 + roundUp8(sizeof(T)) <= limiar`. Assim, por predefinição «payloads até 256 bytes são inline», e `?txt`
(buffer inline de 256 bytes) cai exatamente no limite.

| `T` de `?T` | Inline? | Tamanho de `%option_<key>` |
| --- | --- | --- |
| `i64` / `i32` / `bool` / `char` / `f64` / ponteiro | Sim | 16 bytes |
| `str` / `[]T` / `vec` (descritor de 16 bytes) | Sim | 24 bytes |
| `txt` (buffer inline de 256 bytes) | Sim | 264 bytes (exatamente no limite predefinido) |
| struct / array com tamanho > 256 bytes | Não | 16 bytes (payload no heap) |
| Payload autorreferencial (`node { next ?node }`) | Não | 16 bytes (autorreferência só se quebra com ponteiro) |

> **O payload de `err` é sempre empacotado**: a variante `err` guarda um ponteiro para a struct `err` de `std/err` e não ocupa o
> orçamento de payload acima. Assim, `?str` tem 24 bytes inline em `ok` e também a mesma struct de 24 bytes em `err` —
> apenas o word do `payload` guarda o ponteiro para o objeto de erro. Esta regra permite que «um mesmo tipo `?T`,
> duas variantes, dois payloads» não precise de espaço extra.

> **Inline e empacotamento só mudam «onde é colocado», não a propriedade**: ambas as vias dão, **quando ocorre uma cópia**, uma nova cópia da
> parte de heap do payload da struct (campos ponteiro recursivamente, buffers `str` / `[]T` possuídos inline clonados), pelo que a semântica de valor de `?T`
> é independente de ser inline ou não — o mesmo programa tem de produzir exatamente o mesmo resultado para qualquer `option-payload-inline-threshold`.
> Mudar um limiar e mudar de comportamento é um defeito do compilador, não uma funcionalidade. (Atribuições de puro movimento não clonam; a opção herda
> o heap da origem.)

> A única fonte do layout é `src/mir/layout.go`: `optLayoutOfPayload` determina o `K` e se há empacotamento,
> e `optionPayloadOffset` (8) é o deslocamento fixo em bytes do payload. `hir2mir` dobra constantes `?T` ao nível de módulo e
> o codegen emite leituras e escritas — ambos perguntam à mesma função.

#### Lançamento de `?=` através de payloads diferentes

`?=` copia primeiro o «option na **raiz** da cadeia do lado direito» para o parâmetro de resultado de opção, deixando `nil` / `err` lançarem diretamente,
e o caminho de sucesso é depois substituído pelo braço `ok`:

```no
pick = (p pool) (out ?i64) {
    out ?= p.nodes[0].kind; a raiz é ?node, o slot de resultado é ?i64
}
```

Os tipos de payload dos dois lados podem, por isso, **ser diferentes**, e esta é uma verdadeira fronteira ABI (`?node` 112 bytes, `?i64` 16 bytes),
não duas grafias do mesmo layout. Entre tipos só podem passar duas coisas:

- `tag` — o byte 0 de cada option;
- `err` — a variante `err` é uma propriedade da opção, independente do payload, e é sempre empacotada, pelo que, dos dois lados, a palavra 0 da área de payload
  é um ponteiro de caixa de 8 bytes (a caixa é **copiada**, não partilhada, porque ambos os lados fazem drop).

O payload `ok` **não é transportado entre tipos**: a área de payload do destino é primeiro zerada e o valor de sucesso é sempre reescrito pelo braço `ok`.
A implementação está em `@__nolang_opt_prop_<dst>__<src>` (`optPropagateState` em `src/mir/codegen.go`),
aparecendo apenas em atribuições de lançamento geradas pelo compilador como `?=`.

```no
o ?i64
o = nil; define como vazio
o = 42; define como com valor
o = err('msg'); define como erro

nullableValue ?[]str
nullableString ?str

; Modificar tipo anulável
nullableString = 'test'

; Definir erro
nullableString = err('some error')

; Pode verificar com match
x: {
    err -> log(it.msg()); it é err.err, a mensagem vem de .msg()

    nil -> log('nil')

    -> do-right-thing(it)
}

; Desembrulho forçado
; Cancelar implementação
; !x.say()
```

### Diretriz de estilo: usar option ?t em vez de (val, ok)

Quando uma função pode falhar ou devolver um valor vazio, **prefira o tipo de option `?t`** em vez do padrão de dois valores de retorno `(val t, ok bool)`.

`?t` é um enum com tag com três estados: `ok` (com valor), `nil` (valor vazio) e `err` (erro). O valor normal é vinculado implicitamente; use `nil` quando a operação simplesmente não encontra um valor e `err(...)` quando encontra um erro real.

```no
#{fmt-skip}
; ❌ Não recomendado: padrão de dois valores de retorno
stack.pop = () (val i64, ok bool) {
    .n == 0 -> return
    val = .data[.n]
    ok = true
}
```
```

```no
; ✅ Recomendado: tipo option (nil significa vazio, err significa erro)
stack.pop = () (val ?i64) {
    .n == 0 -> {
        val = nil
        return
    }
    val = .data[.n]
}

; ✅ Devolver erro
file.read = () (data ?str) {
    .fd < 0 -> {
        data = err('file not open')
        return
    }

    ; ... ler os dados
    data = buf
}
```

Usar match para desembrulhar a opção:

```no
val = s.pop()
val: {
    nil -> print('empty')

    err -> print(it.msg()); it = erro estruturado err.err

    -> print(it); it = o valor extraído
}
```

## Overflow da aritmética de inteiros (anotação `#{overflow}`)

O Nolang não suporta panic — qualquer operação que possa overflow tem de ser tratada com `option` ou anotação explícita, nunca lançando uma exceção de overflow de inteiro em tempo de execução. O comportamento de overflow das seguintes **operações aritméticas de inteiros** é controlado pela anotação `#{overflow = ...}`:

- **`+` `-` `*` com sinal e sem sinal**: aplica-se quando ambos os operandos são inteiros (incluindo literais `int`);
- **`/` com sinal**: só `INT_MIN / -1` pode dar overflow (a divisão sem sinal `a/b ≤ a` nunca dá overflow, não se aplicando).

> Inteiros sem sinal (`u8`/`u16`/`u32`/`u64`/`u128`) seguem **exatamente as mesmas regras** que os inteiros com sinal para `+ - *`: sem anotação também devolvem por predefinição `option<int>`, com overflow → `err`. Esta é uma escolha deliberada de design — «sempre que não for possível provar em tempo de compilação que o overflow é impossível, tratar pela mesma regra».

### Caso especial: aritmética de `byte` e `char`

`byte` e `char` são **casos especiais** da semântica de overflow: a sua aritmética **não produz option** e não é governada pela anotação `#{overflow}`.

- **`byte` (u8)**: `+ - *` com overflow **envolve silenciosamente em complemento de 2** (`250 + 10` → `4`, `0 - 1` → `255`). `byte` destina-se a buffers de bytes, codificação/decodificação hex e outros cenários em que o envolvimento é «naturalmente aceitável», com a mesma semântica de `#{overflow = wrap}` de hashes/contadores, mas embutido, sem necessidade de anotação.
- **`char` (ponto de código Unicode)**: `+ -` é **aritmética de pontos de código**, e quando o resultado sai do intervalo de pontos de código válido (`< 0` ou `> 0x10FFFF`), o resultado é **`-1`** (sentinela de ponto de código inválido) — não dá `err` nem envolve.

**Motivo do design**: `i8`–`i128` / `u8`–`u128` são geralmente usados para **valores de negócio reais** (montantes, contagens, capacidades), em que o envolvimento silencioso esconderia bugs de overflow, pelo que o predefinido é `err`, obrigando a uma escolha explícita; já os usos típicos de `byte` / `char` (fluxos de bytes, iteração de pontos de código) aceitam (ou até esperam) semanticamente o envolvimento, e passar por option só acrescentaria ruído de desembrulho. Se precisar de detetar overflow em operações com `byte` / `char`, converta primeiro para um tipo mais largo (como `b.to-i64()`) e depois calcule.

| Modo          | Anotação                       | Quando há overflow                         | Tipo do resultado   | Cenário de aplicação               |
| ----------- | ------------------------ | --------------------------- | ------ | ------------------ |
| **Predefinido (sem anotação)** | —                        | overflow → `err`, normal → `ok(value)` | `?int` | Operações que precisam de detetar overflow          |
| **Envolvimento**      | `#{overflow = wrap}`     | Envolvimento silencioso em complemento de 2                 | `?int` | Hashes, criptografia, contadores, outros com envolvimento semanticamente aceitável |
| **Zerar**      | `#{overflow = clamp0}`   | Overflow (por excesso ou defecto) dá `0`              | `?int` | Semânticas em que a diferença não pode ser negativa (como quantidades restantes)    |
| **Limite mínimo**   | `#{overflow = min}`      | Overflow fixado no mínimo do tipo                  | `?int` | Limite inferior de contadores, proteção de índices         |
| **Limite máximo**   | `#{overflow = max}`      | Overflow fixado no máximo do tipo                  | `?int` | Limite superior de capacidade, acumulação saturada          |
| **Saturação**      | `#{overflow = saturate}` | Excesso→máximo, defecto→mínimo               | `?int` | Saturação de sinal/cor, etc.           |

> ⚠️ **A anotação não desembrulha** (D1): `#{overflow = …}` **só decide «como obter um valor em caso de falha»**,
> e o tipo do resultado é sempre `?T`. Para trazer o resultado de volta para plain, só se pode consumir com match ou `?=`; as duas formas de consumo
> （`?=` + match de bloco／match de valor único）a diferença e a escolha veem-se em〈Escolher entre match de bloco e match de valor único〉。
> Anotar explicitamente como tipo plain (`w i64 = a - b  #{overflow=wrap}`) **não** é uma escapatória —
> em testes continua a reportar `15w45dqk` (cannot assign `?i8` value to `i8` variable)。

Além disso, todos os modos suportam a **forma com prefixo de tipo**, que especifica com precisão o limite de saturação de um tipo estreito específico, por exemplo `#{overflow = u8-max}`, `#{overflow = i8-min}`, `#{overflow = u16-saturate}`。

> **Granularidade da anotação**: `#{overflow = ...}` é uma **anotação de linha** — só se aplica à **instrução imediatamente a seguir**。
> Esta é a **única forma permitida por design** (para evitar abuso): a semântica de execução (codegen) e o erro rígido `ovfhndld` só a reconhecem,
> e ela só altera «como obter um valor em caso de overflow», não o tipo; alargá-la ao nível de bloco ou de função faria com que a semântica de um bloco inteiro de operações fosse decidida por uma anotação linhas atrás。
> As razões são detalhadas em [〈Âmbito das anotações e comentários〉](annotation-scope.md) §4.1。
>
> ⛔ **Colocar acima de uma definição de função, no início de um bloco ou no início do ficheiro não é válido**. A única falha atual é que uma anotação ao nível de função faz com que
> o lint `ovf-int-default` ignore a função inteira (a lista parece «limpa»), enquanto as operações sem anotação no corpo da função continuam a
> disparar `ovfhndld`; uma anotação ao nível de bloco faz o lint herdar para baixo e calar-se, mas o codegen nunca a reconhece. Por favor, anote instrução a instrução;
> é exatamente esta forma instrução a instrução que `no fmt --fix=overflow` produz。
>
> O modelo completo (L0 módulo / L1 declaração / L2 bloco / L3 linha única, e quais as chaves suportadas em cada nível) está em
> [〈Âmbito das anotações e comentários〉](annotation-scope.md); a §7 dessa página lista o estado atual e as falhas, a §8 a ordem de aperto。

> **Correção rápida LSP**: o editor (nolang-lsp) reporta uma **dica** (`nolang-overflow`, trace id `ovfhndld`) para operações inteiras sem anotação e oferece cinco quickfix — **Add `#{overflow = wrap}`** / **`clamp0`** / **`min`** / **`max`** / **`saturate`** — que inserem automaticamente a anotação correspondente acima da instrução onde a operação se encontra, com a mesma indentação dessa linha, especificando o comportamento em caso de overflow (o resultado continua a ser `?T`, sendo necessário consumi-lo com match / `?=`). O equivalente na linha de comando é `no fmt --fix=overflow` (ver abaixo)。

### Predefinição: retorna `option<int>`

Sem anotação, o tipo do resultado de `a - b` é `option<int>`. O recetor tem de ser `?T` (tipo anulável) e usar match para desconstruir os três ramos `err` / `nil` / `ok`:

```nolang
main = () {
    x i64 = 100
    d ?i64 = x - 1; Predefinição: retorna option<i64>
    d: {
        err -> print(-1); overflow (ex.: underflow em x - 2^63)

        nil -> print(0)

        -> print(1); ok(value): resultado normal
    }
}
main()
```

> O campo `data` de `%option` é `i64`; os resultados de tipos estreitos (`i8`/`i16`/`i32`) são primeiro estendidos com sinal (sext) e depois armazenados, sendo truncados de volta à largura original ao desembrulhar。

### wrap: envolvimento silencioso

```nolang
#{overflow=wrap}
sub-wrap = (a i64, b i64) (r i64) {
    r = a - b; em caso de overflow, envolvimento silencioso em complemento de 2; r é i64 plain (tipo explícito)
}
```

Ao nível de instrução:

```nolang
x i64 = -9223372036854775807

#{overflow=wrap}
w i64 = x - 2; underflow com envolvimento → 9223372036854775807
print(w)
```

### clamp0: overflow a zero

```nolang
x i64 = -9223372036854775807

#{overflow=clamp0}
c i64 = x - 2; underflow → 0 (overflow por excesso também a zero)
print(c)
```

### min / max / saturate: limitação por saturação

Os três modos «fixam» o resultado do overflow dentro do intervalo representável do tipo, em vez de envolver ou zerar:

```nolang
#{overflow=min}
floor = (a i64, b i64) (r i64) {
    r = a - b; underflow → valor mínimo de i64; overflow → valor máximo de i64
}

#{overflow=max}
cap = (a i64, b i64) (r i64) {
    r = a * b; overflow → valor máximo de i64; underflow → valor mínimo de i64
}

#{overflow=saturate}
sat = (a i64, b i64) (r i64) {
    r = a + b; overflow→max, underflow→min
}
```

A forma com prefixo de tipo permite controlar com precisão o limite de saturação de tipos estreitos (uma anotação ao nível de função aplica-se a todas as operações do tipo correspondente no corpo):

```nolang
#{overflow=max}
inc = (x u8) (r u8) {
    r = x + 1; com x = 255, overflow → 255 (em vez de envolver para 0)
}

#{overflow=min}
dec = (x i8) (r i8) {
    r = x - 1; com x = -128, underflow → -128 (em vez de envolver para 127)
}
```

### Aplicação pelo compilador

As operações inteiras sem anotação retornam `option<int>` por predefinição, pelo que «não tratadas» são obrigatoriamente bloqueadas. **O que conta como não tratado é julgado em paralelo** — satisfazer qualquer uma das condições abaixo evita o erro:

- **`?=` com propagação para cima** (exige que a função tenha um parâmetro de resultado option);
- **Captura no local**: vincular com `=` a uma **nova** variável (a variável é inferida automaticamente como `?T`) ou declarar explicitamente `?T` (como `x ?i64 = a + b`);
- **Descarte explícito com `_ = expr`** (a avaliação segue o caminho seguro e o erro é ignorado em conjunto);
- **Adicionar uma anotação** `#{overflow = wrap}` / `clamp0` / `min` / `max` / `saturate` (ou uma forma com prefixo de tipo como `u8-max`, `i8-min`), declarando a aceitação da semântica de envolvimento／saturação。

O erro (`ovfhndld`) só ocorre quando nenhuma das quatro está presente。

> **Aritmética pura não é uma fonte de erro**: `+ - * <<` e a negação não disparam captura por si só (caso contrário toda a aritmética se tornaria option e a biblioteca padrão explodiria). Assim, `d = x - 1` (sem `/` `%`, sem indexação segura, sem operandos option) continua a reportar erro — adicione uma anotação ou escreva `d ?i64 = x - 1`。
>
> **Variáveis existentes não mudam de tipo**: a captura só se aplica ao destino «declarado pela primeira vez nesta instrução». Se a variável já existe (por exemplo, primeiro `d = 0` e depois `d = x - 1`, ou primeiro `v i64 = 0` e depois `v = arr[i]`), o compilador não a converte em option no local, mas reporta um erro que o obriga a escolher explicitamente a semântica。

Há ainda dois erros rígidos não abrangidos pelas isenções acima:

- Se uma operação inteira sem anotação for atribuída a uma variável **plain `int` explícita**, o compilador reporta erro (`cannot assign ?i64 value to i64 variable`), obrigando-o a adicionar `#{overflow = wrap}` / `#{overflow = clamp0}` / `#{overflow = min}` / `#{overflow = max}` / `#{overflow = saturate}` (ou uma forma com prefixo de tipo) ou a usar um `?T` com nome。
- Se uma operação inteira sem anotação for atribuída a uma variável **já existente** e cujo tipo não é option (`d = x - 1` depois de `d = 0`), o compilador reporta erro e sugere adicionar uma anotação ou declarar `?i64`, para evitar IR inválido。

Regras restantes:

- As operações com inteiros sem sinal (`u8`/`u16`/`u32`/`u64`/`u128`) **seguem igualmente** este mecanismo: sem anotação, `+ - *` retornam `option<int>` por predefinição e o overflow → `err`, tal como para os inteiros com sinal。
- As operações com `i128` não suportam o caminho `option` (o campo data só tem `i64`), degradando-se todas para envolvimento silencioso por forma a preservar a correção。

> **Por que a predefinição é option e não wrap?** O envolvimento silencioso esconde bugs de overflow; retornar `option` por predefinição obriga a chamadora a tratar o overflow explicitamente (ou a anotar `wrap`, indicando «aceito a semântica de envolvimento»), tornando «permitir ou não overflow» uma decisão de design visível。

### Divisão por zero (division by zero)

Um divisor `0` em `/` e `%` inteiros é **comportamento indefinido (UB)**, uma coisa **distinta** do overflow — nenhuma anotação o pode isentar:

- **Determinável em tempo de compilação**: o divisor é uma **constante literal 0** (`10 / 0`, `a % 0`, `x / 0`) ⇒ **erro rígido** (trace id `div-by-zero`). `#{overflow = wrap}` não o isenta (essa anotação gere o overflow, não a divisão por zero)。
- **Conhecido apenas em tempo de execução**: o divisor provém de uma variável／entrada externa ⇒ o compilador **insere uma verificação de zero em ambas as vias**, diferindo apenas «como reportar»:

| Tipo do resultado | Condição de disparo | Quando o divisor é 0 |
| ----------- | ----------------------- | ------------------------------------------------------------------------------ |
| `?int` (predefinição) | Instrução sem anotação | option tag = `err`, tratar com `?=` / match |
| `int` plain | Instrução com `#{overflow = ...}` | Resultado forçado a `0`, com impressão no **stderr** de `nolang: integer division or remainder by zero ...` |

```nolang
safe = (a i64, b i64) (r ?i64) {
    r = a / b; b == 0 → err (a chamadora tem de tratar)
}

#{overflow=wrap}
fast = (a i64, b i64) (r i64) {
    r = a / b; b == 0 → 0 e imprime uma linha de diagnóstico no stderr (já não é UB de sdiv nu)
}
```

> **Por que `#{overflow = wrap}` não pode isentar a guarda de divisão por zero**: o que ele altera é o **comportamento em caso de overflow** (o tipo do resultado continua sempre `?int`), não a segurança do divisor. Se o isentasse, o compilador emitiria um `sdiv` nu e o otimizador poderia assumir que o divisor nunca é 0 (reduzindo `a / 0` a qualquer valor), enquanto o `idiv` de x86-64 falha diretamente com #DE — logo UB e sem qualquer diagnóstico. Por isso, a guarda zero-divisor existe **independentemente da anotação**; a via plain imprime uma linha extra no stderr precisamente porque não há option para a chamadora verificar。
>
> **A mesma guarda cobre também `sdiv (INT_MIN, -1)`** (resultado não representável, também UB; em x86-64 causa SIGFPE). A via option integra-o em `err`; a via plain toma o valor de envolvimento em complemento de 2 (`INT_MIN / -1 → INT_MIN`, `INT_MIN % -1 → 0`)。

A divisão de vírgula flutuante não está sujeita a isto: `1.0 / 0.0` é definida por IEEE-754 como `inf` / `nan`, não sendo UB。

### Operações sem anotação listadas como erro (`ovf-int-default`)

`no vet` reporta as operações inteiras **sem anotação** como **dica** (trace id `ovfhndld`, HINT). A razão: o `?T` predefinido é invisível para o utilizador, sendo necessário informá-lo — pode adicionar `#{overflow = ...}` para especificar o comportamento em caso de overflow, ou consumir com `?T` + `?=` / match / tipo plain explícito. O que verdadeiramente deve bloquear a compilação é `optunuse` (option nem propagada para cima nem desembrulhada)。

### Correção automática: `no fmt --fix=overflow`

Para adicionar anotações em massa não é preciso escrevê-las à mão — use o parâmetro de categoria de correção de `no fmt`:

```bash
no fmt --fix=overflow -w src/std          # Corrige o diretório inteiro no local
no fmt --fix=overflow -d src/std/str.no   # Imprime apenas o diff, sem gravar
no fmt --fix=overflow src/std/str.no      # Imprime o conteúdo após a correção
```

A correção é **precisa e não polui**: insere `#{overflow=wrap}` apenas acima da instrução onde se encontra a operação efetivamente reportada pelo lint, sem alterar outras instruções, sem reformatar, sem tocar em anotações existentes, e é idempotente em execuções repetidas (o que já está anotado não muda). Isto difere de executar `no fmt -w` diretamente — este último faz uma reformatação completa。

> Após a correção, execute novamente `no vet` para confirmar que chegou a zero. Na maioria dos casos, uma única execução esvazia a lista (`src/std` passou de 4728 para 0). A única forma excecional conhecida: o report incide sobre a **condição** de um braço de match, e esse braço não tem qualquer instrução anotável — uma anotação de linha só atua na primeira instrução a seguir, não cobrindo a operação da condição do próprio braço, pelo que `no vet` volta a reportar. Nesse caso, extraia primeiro a operação para uma instrução independente e depois anote-a。

### Correção automática: anotações de tipo redundantes

Quando a **anotação de tipo** de uma declaração de variável é **exatamente igual ao tipo inferido a partir do valor à direita**, a anotação é redundante (`no vet` reporta a dica `tcpoxtfd`, `type annotation '<T>' can be omitted (inferred from value)`)。

- **`no fmt` predefinido já remove automaticamente**: `no fmt -w` remove as anotações redundantes ao formatar (fazendo também a reformatação normal), `no fmt -d` pré-visualiza esta alteração e `-w` grava。

```bash
no fmt -w main.no          # Formata e remove anotações redundantes
no fmt -d main.no          # Imprime apenas o diff (inclui remoção de anotações)
no fmt -w src/             # Diretório inteiro
```

- **`--fix=redundant` (cirúrgico, só mexe nas anotações)**: se quiser remover apenas anotações redundantes sem reformatação completa (por exemplo, em `src/std`, porque `no fmt` perderia `#{index-out}`), use esta categoria — elimina só as anotações, sem reformatar:

```bash
no fmt --fix=redundant -w src/std          # Remove anotações redundantes do diretório inteiro no local (sem reformatar)
no fmt --fix=redundant -d src/std/str.no   # Imprime apenas o diff, sem gravar
no fmt --fix=redundant src/std/str.no      # Imprime o conteúdo após a correção
```

```nolang
; Antes da correção
x str = ''
b i64 = 5
m [str]i64 = make-map()

; Depois da correção (anotação == tipo inferido, removida)
x = ''
b = 5
m = make-map()
```

Pontos principais da regra:

- Só remove quando **tipo anotado == tipo inferido** (como `str`/`i64`/`[str]i64` quando correspondem ao valor inferido)。
- Situações de **conversão implícita mas não igualdade** **não são** removidas — manter a anotação preserva a semântica de conversão. Por exemplo `c i16 = 5` (`5` é inferido como `i64`, diferente de `i16`), `p ?i64 = nil`, `last txt = ''` (`txt` e `str` convertem-se implicitamente um no outro, mas são tipos diferentes)。
- As formas **sem anotação de tipo** (como `d = 'x'`) nunca são reportadas e, naturalmente, também não são alteradas。
- As **anotações de array de tamanho fixo `[N]T` / `[?]T` são sempre preservadas** (nunca consideradas redundantes). `[N]` não é uma simples dica de tipo, mas um interruptor que decide a **representação**: só com ele o literal `[...]` à direita é analisado como um array de pilha de tamanho fixo; só com ele o resultado inferido «dá exatamente» a anotação — um raciocínio circular criado pela própria anotação. Ao removê-la, a mesma linha passa a ser analisada como uma fatia `[]T` alocada no heap e que pode crescer, mudando a semântica: em `fixed [3]char = ["x", "y", "z"]`, `.push()` é inválido (`.len()` é sempre 3), mas ao degradar para `fixed = ["x", "y", "z"]`, `.push()` faz `.len()` passar a 4. A anotação de fatia `[]T` não está incluída — removida, continua a ser uma fatia, sendo verdadeiramente redundante。
- **Valores com literais hexadecimais preservam sempre a anotação** (exceção conservadora). A inferência de tipo de `no vet` usa a heurística «inferir como `byte`» para `0xNN`, diferente da semântica real do compilador («literal inteiro sem anotação é `i64` por predefinição»), pelo que `IP-TBL [64]byte = [0x3a, ...]` e `s []byte = [0x50, 0x4b]` são falsamente considerados redundantes; uma vez removida a anotação, os literais degradam-se para elementos `i64` e produzem erros de tipo como `expected '[]byte', got '[]i64'`. Por isso, basta que apareça `0x`/`0X` no texto da expressão de valor para que nem a dica nem a remoção ocorram。
- **Valores que são leitura de campo preservam sempre a anotação** (exceção conservadora). A anotação em escritas como `i i64 = .pos` não é uma simples dica de tipo, mas o interruptor que determina se a aritmética inteira a jusante produz `?T`: ao passar a inferir o tipo a partir do campo, `i + N` deixa de ser uma «operação com sinal» e não retorna `?i64`, o braço `ok` do desembrulho em duas fases nunca é atingido e o contador do loop fica parado. Exemplo em `src/std/yaml.no`, `parser.looks-like-map-entry`: remover `i64` faz `yaml.parse('abc')` entrar num loop infinito. Por isso, anotações em `x = .field` e `x = obj.field` (incluindo com parênteses ou prefixo) nem reportam `tcpoxtfd` nem são eliminadas por `no fmt` predefinido ou `--fix=redundant`。

Tanto com `no fmt` predefinido como com `--fix=redundant`, a remoção elimina apenas a anotação de tipo reportada e o espaço em branco que a precede, sendo idempotente em execuções repetidas (o que já foi removido não muda). Após a correção, recomenda-se executar novamente `no vet` para confirmar que `tcpoxtfd` chegou a zero。

### Correção automática: consumir option (`--fix=unwrap`)

Sob a regra unificada, «gerar option mas nunca a consumir» é o único diagnóstico que deve bloquear a compilação (trace `optunuse`). Para converter em massa para formas de consumo explícito, use `--fix=unwrap`:

```bash
no fmt --fix=unwrap -w src/std            # Corrige o diretório inteiro no local
no fmt --fix=unwrap -d src/std/str.no     # Imprime apenas o diff, sem gravar
no fmt --fix=unwrap src/std/str.no        # Imprime o conteúdo após a correção
```

Formas de correção (ver〈Indexação segura〉): **a função onde se encontra já tem parâmetro de resultado `?T`** → propagar para cima (`x ?= <expr>`); caso contrário → renomear + desembrulhar com match:

```nolang
; Antes da correção
x = a + b

; Depois da correção (a função contém parâmetro de resultado ?T)
x ?= a + b

; Depois da correção (caso contrário, forma padrão de 4 linhas; braço de fallback vazio = valor zero)
x-opt = a + b
x = x-opt: {
    ok -> it

    ->
}
```

Tal como as outras categorias de correção: substituição textual cirúrgica, sem alterar a formatação e sem tocar nas anotações `#{overflow=…}` / `#{index-out=…}` acima das instruções (elas continuam a gerir «como obter um valor em caso de falha»), sendo idempotente em execuções repetidas. Após a correção, execute novamente `no vet` para confirmar que `optunuse` chegou a zero。

### Vinculação de anotações e fusão em linha única

As anotações `#{...}` **vinculam-se a nós**, pelo que uma mesma instrução pode ter várias chaves; várias anotações são sempre **fundidas numa única linha**:

```nolang
#{fmt-skip}
i <- [0..16): {

    #{index-out=0, overflow=wrap}
    buf[base + i] = data[i]
}
```

A instrução acima precisa simultaneamente das duas semânticas «obter o valor predefinido em caso de saída de limites» (`index-out`) e «envolvimento em caso de overflow» (`overflow`). Escrever em duas linhas dá o mesmo resultado de análise, mas `no fmt --fix=overflow` produz a forma fundida numa única linha。

> **Âmbito da anotação de linha**: `#{overflow = ...}` só atua na **instrução imediatamente a seguir**. Mesmo que haja anotações irrelevantes pelo meio, a aplicação à instrução correta continua válida. As escritas com indexação segura (`arr[base + i] = v`, `.buf[.pos + i] = v`) são convertidas (desugar) para a forma match, e `#{overflow = wrap}` é levado consigo para os nós após o desugar; por isso, a expressão do índice também fica sob a alçada da anotação e não é falsamente reportada como sem anotação。

### Propagação de erro para cima (operador `?=`)

`?=` é uma **abreviação** para desembrulhar option + propagar o erro para cima. **Não é necessário que a função retorne option**: basta uma das duas situações abaixo,
por isso uma função cujo valor de retorno não é option pode igualmente escrever `y ?= x + 1` desde que envolva o corpo num match de bloco。

**Sintaxe:**

```no
v ?= expr
```

**Semântica:**

- Se `expr` retornar `ok(value)`, `v` é atribuído com o valor interno desembrulhado (desembrulho automático)
- Se `expr` retornar `nil` ou `err`:
  - Quando a função tem parâmetro de resultado `?T` → escreve essa option no parâmetro de resultado e faz `return` antecipado (propagação automática);
  - Quando a instrução está no corpo de um **match de bloco ao nível de instrução** `{ ... }: { ... }` → é capturada pelo **mais próximo** match de bloco,
    **todo o corpo para**, os braços executam uma única vez (**não** é `return`) e a função não precisa de parâmetro de resultado `?T`。

**Condições de uso (basta uma das duas):**

1. A função onde se encontra tem um parâmetro de resultado do tipo option;
2. A instrução está no corpo de um match de bloco ao nível de instrução `{ ... }: { ... }` (ver〈As três formas do sujeito do match〉)。

Quando nenhuma das duas se verifica, o compilador reporta erro. Os dois braços `err -> _` / `-> _` podem engolir no local o erro propagado, constituindo o recurso padrão para «sem parâmetro de resultado `?T`
mas querendo usar a abreviatura»:

```no
handle = (x i64) {
    {
        y ?= x + 1
        print(1)
    }: {
        err -> _

        -> _
    }
}
```

**Os parâmetros de resultado não precisam de atribuição prévia:** o prologo da função não faz inicialização a zero, mas no `return` os parâmetros de saída não atribuídos explicitamente são preenchidos a zero automaticamente (inteiros → `0`, `str` / structs → valor zero, option → `nil`), pelo que os exemplos não escrevem o boilerplate `result = nil`, bastando a atribuição no caminho de sucesso。

**Exemplo:**

```no
; Lê o ficheiro linha a linha e processa — usa ?= para simplificar a propagação de erros
process-file = (path str) (result ?str) {
    f ?= open(path); em caso de falha, result = f automaticamente; return
    line ?= f.read-line(); EOF ou erro → propagação automática
    result = line
}
```

A forma expandida equivalente (cadeia de match gerada automaticamente pelo compilador):

```no
#{fmt-skip}
process-file = (path str) (result ?str) {
    result = nil
    __tmp = open(path)
    __tmp: {
        nil || err -> {
            result = __tmp
            return
        }
        -> f = it
    }
    __tmp2 = f.read-line()
    __tmp2: {
        nil || err -> {
            result = __tmp2
            return
        }
        -> line = it
    }
    result = line
}
```

**Uso em cadeia:** vários `?=` podem ser encadeados, permitindo uma propagação de erros em pipeline:

```no
pipeline = (input str) (result ?str) {
    a ?= step1(input); em caso de falha, propaga
    b ?= step2(a); em caso de falha, propaga
    result = b
}
```

**Aritmética nua:** o lado direito de `?=` não se limita a chamadas de função — qualquer operação inteira que retorne `option` (como as não anotadas `a - b`, `a + b`, `a * b`) pode usar `?=` diretamente para propagar o erro de overflow, sem precisar de a vincular primeiro a uma variável intermédia:

```no
; Em caso de overflow, result = err; return automaticamente; em condições normais, v é desembrulhado para o valor interno
sub-safe = (a i64, b i64) (result ?i64) {
    v ?= a - b; underflow/overflow → err propagado automaticamente; caso contrário v = i64 interno
    result = v
}

add-safe = (a i64, b i64) (result ?i64) {
    v ?= a + b; igual acima, aplica-se a + * e / com sinal
    result = v
}
```

Expansão equivalente (gerada automaticamente pelo compilador):

```no
#{fmt-skip}
sub-safe = (a i64, b i64) (result ?i64) {
    result = nil
    __unwrap = a - b            ; option<i64>
    __unwrap: {
        nil -> {
            result = __unwrap
            return
        }
        err -> {
            result = __unwrap
            return
        }
        -> v = it               ; ok(value): desembrulha o valor interno
    }
    result = v
}
```

**Lado direito composto (compound RHS):** o lado direito de `?=` pode ser uma expressão composta com vários operandos option. Cada operando option é desembrulhado automaticamente, com injeção de guardas de propagação — assim que qualquer operando é `nil` / `err`, o parâmetro de resultado option da função atual recebe esse valor e executa `return` (propagação para cima); só quando todos têm sucesso é que o valor interno é atribuído ao lado esquerdo:

```no
sum3 = () (result ?i64) {
    a ?i64 = ok(10)
    b ?i64 = ok(20)
    c ?i64 = ok(30)
    total ?= a + b + c; cada operando option é desembrulhado e propagado automaticamente
    result = total
}

comp = () (result ?i64) {
    a ?i64 = ok(4)
    b ?i64 = ok(6)
    v ?= dbl(a + b); a + b pode ser option, também desembrulhado e propagado automaticamente
    result = v
}
```

> Apenas as options em «posição de operando» são desembrulhadas automaticamente (como `a + b + c`, `f(b + c)`); uma variável option passada diretamente como argumento de função não está incluída — segue a isenção já existente de argumento option nu em `?=` (`a ?= f(v)`, em que `v` é `?i64` e `f` recebe `i64` plain)。

**Erro para lado direito não option:** para evitar escrever `=` como `?=` por engano em grande escala, quando o lado direito pode ser determinado em tempo de compilação como «claramente não option», o compilador reporta erro e sugere usar `=`:

- Literal: `v ?= 42`
- Operação inteira com constantes dobráveis: `v ?= 10 + 20`
- Variável local de tipo conhecido e não option: `v ?= x` (em que `x i64 = 5`)

A mensagem de erro tem a forma: `` `?=` RHS `42` is not an option — use `=` instead ``.

> Uma operação aritmética é considerada «possivelmente option» e autorizada logo que qualquer operando seja option (ou de tipo desconhecido / seja uma chamada de função, que pode retornar option), sem falso positivo — por exemplo `v ?= a + b + c` (a/b/c são `?i64`) e `v ?= b + c` (b/c de tipo desconhecido) compilam normalmente。

**Cenários de aplicação:**

- Operações de contentor que podem estar vazias, como `pop` / `peek` → `?t` (`nil` = vazio)
- Operações de I/O como `read-line` / `read-byte` → `?str` / `?i64` (`nil` = EOF, `err` = erro)
- Operações de consulta como `lookup` / `get` → `?t` (`nil` = não encontrado)
- Operações de parsing como `parse` / `from-str` → `?t` (`nil` = vazio, `err` = entrada inválida)
- Ligações de rede como `accept` / `dial` → `?conn` (`nil` = sem ligação, `err` = erro)

**nil vs err:** use `nil` quando a ausência é um resultado normal/esperado (pilha vazia, chave inexistente, EOF); use `err('msg')` quando a ausência representa um estado de erro real (falha de I/O, entrada inválida, ligação recusada)。

**Exceção:** quando a função precisa de retornar vários valores independentes (como `(name str, value str, ok bool)`), pode manter o padrão de múltiplos valores de retorno。

**No máximo um `?T` entre múltiplos valores de retorno (erro rígido):** nos parâmetros de retorno declarados de uma função, há no máximo um `?T`. `?T` é um **único** canal de erro — quando um `?=` no corpo da função retorna antecipadamente, só pode colocar a falha nesse único parâmetro de retorno option; se forem declarados dois `?T`, o compilador só consegue preencher o primeiro e a chamadora recebe uma tupla preenchida a metade (`(err, nil)`), sem forma de determinar qual valor representa a falha:

```no
#{fmt-skip}
pair = () (a ?i64, b ?i64) {; Erro: dois parâmetros de retorno ?T
    a = ok(1)
    v ?= boom(); boom() retorna err('boom')
    a = v
    b = ok(2); nunca executado — a chamadora vê (err, nil)
}
```
```

A forma correta é declarar os valores independentes como tipos plain, mantendo apenas um `?T` como canal de erro:

```no
lookup = (k str) (value str, found bool, err ?str) {
    value = ''
    found = false
    err = nil
}
```

A mensagem de erro tem a forma: `` function 'pair' declares 2 option (?T) results ('a', 'b'); only one result may be ?T … [moptres1] ``. O recetor implícito `self` de uma definição de método é um parâmetro de saída com alias da chamadora e não ocupa este lugar; as declarações `extern` com `#{c}` estão igualmente sujeitas a esta limitação。

### Atribuição com captura de erro (`=` com captura no local)

A semântica de `?=` é «propagar o erro para cima» — exige que a função contenha um parâmetro de resultado option e faz `return` antecipado em caso de falha. Quando não pretende retornar antecipadamente, mas sim tratar o erro no local, use `=` normal: desde que o lado direito contenha uma **fonte de erro**, a variável de destino é inferida automaticamente como `?T`, e `nil` / `err` são **armazenados nessa variável no local**, continuando o fluxo。

```no
handle = (b i64, c i64, d i64) (r i64) {
    a = b + c / d; a inferido como ?i64; divisão por zero → a = err
    a: {
        err -> {
            r = -1; trata o erro no local
        }

        nil -> {
            r = -1
        }

        -> {
            r = it; sucesso: r = b + c/d
        }
    }
}
```

**Fontes de erro (fontes que disparam captura):**

| Fonte | Exemplo | Resultado da captura |
| --------------------- | ----------------------------------- | ----------------------------- |
| Divisão / módulo de inteiros | `a = b + c / d`, `a = b % c` | Divisão por zero, `MIN / -1` → `err` |
| Indexação segura | `a = v[i]` (`arr` / `vec` / `slice`) | Fora de limites → `err` (`code.index-out`) |
| Operando do tipo option | `a = x + 1` (`x ?i64`) | Quando `x` é `nil` / `err`, armazenado tal qual em `a` |
| Chamada que retorna option (como operando aritmético) | `a = f(x) + 1` (`f` retorna `?i64`) | Igual acima |

> As options passadas **diretamente** como argumentos de função não estão incluídas — essas ficam a cargo da função chamada (consistente com a isenção de argumento option nu em `?=`)。

**Aritmética pura não é fonte de erro**: `+ - * <<` e a negação não disparam captura por si só (caso contrário toda a aritmética se tornaria option). Mas, assim que entram num caminho option (por exemplo, se o lado direito tiver outro `/`, ou se o destino estiver explicitamente declarado como `?T`), estas operações passam a ter **verificação de overflow em tempo de execução**, overflow → `err`:

```no
ovf = (x i64) (r i64) {
    a ?i64 = x + 1; ?T explícito → segue o caminho de encapsulamento option
    a: {
        err -> {
            r = 0; com x = i64-max, overflow → err
        }

        nil -> {
            r = 0
        }

        -> {
            r = it
        }
    }
}
```

**Captura vs propagação:**

| Escrita           | Em caso de falha                                             | Precisa de resultado option | Como obter o valor depois     |
| ----------------- | ------------------------------------------------------------ | --------------------------- | ------------------------------ |
| `a ?= expr`       | Define o resultado como `nil`/`err` e faz `return` (retorno antecipado) | Sim                         | Não aplicável (já retornou)    |
| `a = expr`        | `a` guarda `nil`/`err` no local, o fluxo continua              | Não                         | `a: { ok -> ... }`             |
| `a ?T = expr`     | Igual a `=` (anotação explícita, inclui subexpressões propensas a erro) | Não                  | Igual acima                    |
| `_ = expr`        | Avalia mas descarta o resultado e o erro (sem erro, não dispara lint de não usado) | Não     | Não aplicável                  |

**Âmbito:** a captura de indexação segura, tal como `?=`, **só tem efeito dentro de funções que retornam `?T`** (ver〈Indexação segura〉, 3.ª forma). Já a captura de `/`, `%` e de operandos option aplica-se a todas as funções。

**Variáveis existentes não mudam de tipo:** a captura só se aplica ao destino «declarado pela primeira vez nesta instrução». Se o destino já existe (`v = arr[i]` depois de `v i64 = 0`, ou `d = x - 1` depois de `d = 0`), o compilador não altera o seu tipo no local, mas reporta um erro que exige escolher explicitamente a semântica (adicionar `#{overflow=...}` / `#{index-out=...}`, usar `?=` ou declarar `?T`)。

### Genéricos

```no
arr_to_vec = (arr [n]t) (out []t) {
    i <- [0..n): {
        out[i] = arr[i]
    }
}
```

### Conversão forçada de tipo

```no
; Devolve a cadeia com o nome do tipo
a = typeof(x)

; `as` só é permitido para conversões de tipos de ponteiro FFI (como *byte, **byte, *i64)
; Os inteiros são todos i64 internamente, não é necessária conversão explícita
y = x as *byte
```

### Verificação de tipo na atribuição de inteiros

O compilador verifica os tipos nas atribuições de inteiros para impedir um narrowing (estreitamento) inseguro que cause perda de dados. Seguem as regras:

#### Alargamento implícito (seguro, permitido automaticamente)

Um valor de tipo inteiro estreito pode ser atribuído automaticamente a um tipo mais largo, porque o intervalo do tipo de destino contém completamente o do tipo de origem:

```no
b byte = 200
i i64 = b; ✓ intervalo de byte [0,255] ⊆ intervalo de i64
u u32 = b; ✓ intervalo de byte ⊆ intervalo de u32
```

#### Atribuição de literais inteiros

Um literal inteiro (inferido como `i64` por predefinição) pode ser atribuído a qualquer tipo inteiro cujo intervalo contenha o valor:

```no
#{fmt-skip}
n u8 = 200; ✓ 200 ∈ [0,255]
m u8 = 300; ✗ 300 > 255, erro de compilação
big u64 = 18446744073709551615; ✓ 2^64-1, valor máximo de u64
```
```

#### Narrowing inseguro (erro de compilação)

Atribuir uma variável de tipo largo diretamente a um tipo estreito causa erro de compilação, pois pode haver perda de dados. A mensagem de erro inclui uma **dica acionável** que sugere como fazer um narrowing seguro com operações bit a bit:

```no
#{fmt-skip}
d u64 = 42
h u32 = d; ✗ cannot assign u64 value to u32 variable 'h'; hint: narrow safely with a bitwise mask (e.g. `& 4294967295`) or right shift (e.g. `>> 32`)
h u16 = d; ✗ cannot assign u64 value to u16 variable 'h'; hint: narrow safely with a bitwise mask (e.g. `& 65535`) or right shift (e.g. `>> 48`)
h u8 = d; ✗ cannot assign u64 value to u8 variable 'h'; hint: narrow safely with a bitwise mask (e.g. `& 255`) or right shift (e.g. `>> 56`)
x u32 = d + 1; ✗ o resultado da adição continua a ser u64, inseguro
y u32 = foo(); ✗ o tipo do resultado da chamada de função não corresponde
```
```

> **Dica de reparação**: o compilador calcula automaticamente o valor exato da máscara e a quantidade do deslocamento para o tipo de destino. Ao aplicar a máscara ou o deslocamento indicados, o narrowing passa a ser seguro (ver secção seguinte)。
>
> **Tipos de destino com sinal**: para inteiros com sinal como `i8`/`i16`/`i32`/`i64`, a dica indica que o narrowing bit a bit não se aplica (a semântica do truncamento do bit de sinal é ambígua), recomendando antes uma verificação explícita do intervalo。

#### Narrowing seguro com operações bit a bit (permitido automaticamente)

Quando o lado direito da atribuição é uma **expressão bit a bit** (`&`, `|`, `^`, `<<`, `>>`) e o tipo de destino é um **inteiro sem sinal** (`u8`/`u16`/`u32`/`u64`/`byte`), o compilador permite o narrowing implícito — porque truncar os bits altos é a semântica normal das operações bit a bit e não causa perda inesperada de dados:

```no
d u64 = 42

; ✓ operação de máscara: o resultado é sempre ≤ ao valor da máscara, caindo seguro em u32
h u32 = d & 67108863; mask = 2^26-1 < 2^32
h u32 = d & 4294967295; mask = 2^32-1, exatamente o intervalo de u32

; ✓ operação de deslocamento: após deslocar à direita, os bits altos são 0, seguro
hi u32 = d >> 32; u64 >> 32 mantém 32 bits

; ✓ combinação XOR / OR
c u32 = a ^ b; resultado de operação bit a bit
b byte = v & 255; máscara para o intervalo de byte

; ✓ operação bit a bit composta (comum em criptografia/codificação)
s u32 = (key[0] & 255) | ((key[1] & 255) << 8) | ((key[2] & 255) << 16) | ((key[3] & 255) << 24)
```

> **Por que é permitido?** A semântica das operações bit a bit (máscara, deslocamento, XOR, OR) é justamente construir um padrão de bits. Ao atribuir a um tipo sem sinal estreito, truncar os bits altos é uma operação deliberada — quem programa já garantiu o intervalo do resultado por meio da máscara ou do deslocamento, ou descarta deliberadamente os bits altos. É o padrão comum em criptografia (como ChaCha20, Poly1305, Blake2) e em código de codificação/decodificação。

> **Apenas para tipos de destino sem sinal.** Para destinos de inteiros com sinal (`i8`/`i16`/`i32`/`i64`), mesmo que o lado direito seja uma operação bit a bit ocorre erro, porque a semântica do truncamento do bit de sinal é ambígua:
>
> ```no
> d u64 = 42
> h i32 = d & 4294967295   ; ✗ ainda dá erro: destino com sinal não admite narrowing seguro
> ```

> **O nível superior tem de ser uma operação bit a bit.** Só é permitido quando o operador de nível superior da expressão é `&`/`|`/`^`/`<<`/`>>`. Adição, subtração, chamada de função, referência direta a variável, etc. não estão incluídos:
>
> ```no
> d u64 = 42
> h u32 = d              ; ✗ o nível superior é um identificador, não uma operação bit a bit
> h u32 = d + 1          ; ✗ o nível superior é +, não uma operação bit a bit
> ```

### Sistema de módulos

- Cada ficheiro é um módulo
- Os nomes de ficheiros e pastas usam hífen

```shell
utils/
└── helper.no    ; o nome do módulo é utils/helper
```

### Importar módulos

> **A sintaxe nova usa `#` para importar. A palavra-chave antiga `use` ainda funciona, mas está obsoleta (deprecated); recomenda-se usar `#`.**

```no
; Biblioteca padrão (sintaxe nova, recomendada)
# std/math.add
; Módulo remoto (não começa com std/)
# github.com/utils/math.add
; Módulo local, tem de começar com /
# /utils/math.add
; Alias
# std/math.add a

; ── Segue a sintaxe antiga (deprecated, ainda funciona mas não recomendada) ──
; use std/math.add
; use github.com/utils/math.add
; use /utils/math.add
; use std/math.add a
```

#### Importar diretamente ficheiros de configuração `.noml`

Os ficheiros `.noml` podem ser importados diretamente com `#`: a ordem de resolução procura primeiro um `.no` com o mesmo nome e, se não o encontrar, um `.noml`. Após a importação,
**o nome da struct é por predefinição o nome do ficheiro** (sem a extensão, podendo ser reescrito pela anotação `#{name = '...'}` no topo do ficheiro),
cada membro passa a ser um campo dessa struct; ao mesmo tempo é gerado um valor global com o mesmo nome que contém o conteúdo do ficheiro, pelo que se lê a configuração com
`server.port`。

```noml
; cfg/server.noml
{
  host: 'localhost'
  port: 8080
  tags: [] #{type = '[]str'}
  compiler: {
    version: 'v0.1.0'
  }
}
```

```no
# /cfg/server

print(server.port); 8080
print(server.compiler.version); v0.1.0
```

- O tipo do campo é obtido primeiramente da anotação `#{type = '...'}` do membro (o valor é uma expressão de tipo nolang completa, como
  `'[]str'`, `'[]i64'`, `'?i64'`); sem anotação, é inferido a partir do valor: cadeia → `str`, inteiro → `i64`,
  decimal → `f64`, booleano → `bool`, objeto → struct aninhada gerada automaticamente (`server-compiler`),
  array → `[]tipo-do-elemento`。
- Contentores vazios (`[]`, `{}`) **devem** ter `#{type = '...'}`: o valor noml não tem tipo por si só e um contentor vazio
  não pode ser inferido. Quando um membro de objeto tem a anotação `#{type = 'x'}`, usa-se diretamente `x` (deve defini-lo), sem gerar
  automaticamente a struct aninhada。
- O nome da struct de um objeto aninhado tem o nome do ficheiro como prefixo (`server-compiler`); a ordem de definição é primeiro o aninhado e depois o hospedeiro。
- **Anotações no topo (ao nível do documento) podem reescrever o nome da struct**: `#{name = '...'}` (forma equivalente `#{type = '...'}`,
  com `name` a ter prioridade) escreve-se numa linha independente acima do `{` de nível superior (ou no fim da linha do valor de nível superior). Após a reescrita, o nome de vinculação do valor e o prefixo da struct aninhada
  acompanham a alteração, mas**o caminho de importação continua a ser resolvido pelo nome do ficheiro**:

```noml
; cfg/db.noml
#{name = 'db-options'}
{
  host: 'localhost'
}
```

```no
# /cfg/db
# /cfg/db as db2

print(db-options.host); localhost
print(db2.host); localhost
```

- Quando dois ficheiros diferentes geram o mesmo nome de tipo (por exemplo, ficheiros com o mesmo nome em diretórios distintos) ocorre erro; basta distingui-los com a anotação de topo
  `#{name = '...'}`; importar o mesmo ficheiro várias vezes (por exemplo, acrescentando um alias `as`) funde apenas uma definição de tipo。
- Os nomes das chaves são convertidos para minúsculas com hífen segundo as regras do nolang (`anonymous-fn-type`): se começam por um dígito, acrescenta-se `f-`; se têm o mesmo nome que uma
  palavra-chave, acrescenta-se `x-`。
- É permitido o alias `as` para reescrever o nome de vinculação do valor, mantendo-se o nome da struct: `# /cfg/server as cfg`。
- Os tipos `.noml` não participam no prefixo do módulo: mesmo que o nome do ficheiro seja igual ao de um módulo da biblioteca padrão (como `server`), o tipo continua a ter um nome nu。
- `package.noml` / `workspace.noml` também podem ter anotações `#{...}` (como `#{type = '[]str'}`):
  são ignoradas na resolução, servindo apenas para ferramentas externas reconstruírem a estrutura com tipos fortes; a sintaxe é exatamente a mesma das anotações `#{...}` do nolang。

### Exportar módulos

Apenas para lib.no

```no
@ std/math.add a
```

### FFI (anotação `#{c}`)

Declara funções C externas por meio da anotação `#{c}`, implementando FFI (Foreign Function Interface)。

**Sintaxe**: `#{c}` numa linha independente, marcando a linha seguinte como declaração FFI. `#{c}` é a chave de linguagem FFI do sistema de anotações e também suporta `#{cpp}`, `#{rust}` e outras. A sintaxe antiga `#c` continua compatível com versões anteriores。

**Declaração privada**: um nome que começa com `_` indica privado (não exportado); o símbolo C ABI remove automaticamente o prefixo `_` e converte hífens em sublinhados。

**Não é necessário separar ficheiros**: declarações FFI e código comum podem ser escritos no mesmo ficheiro `.no`。

**Sintaxe de tipos de ponteiro**: em FFI usam-se `*T`, `**T`, `***T` ao estilo C para ponteiros, sendo obrigatório um tipo concreto `T`. O código comum não pode usar esta sintaxe。

| Sintaxe   | Significado          | LLVM IR | Uso                     |
| --------- | -------------------- | ------- | ----------------------- |
| `*byte`   | ponteiro para byte   | `i8*`   | ponteiro opaco (ex.: handle de db) |
| `**byte`  | ponteiro duplo       | `i8**`  | parâmetro de saída (ex.: `sqlite3**`) |
| `***byte` | ponteiro triplo      | `i8***` | rara indireção tripla   |

```no
; sqlite.no — vinculação FFI e wrapper seguro no mesmo ficheiro
; O compilador converte automaticamente hífens (-) em sublinhados (_) para corresponder aos símbolos C ABI
; Nomes que começam com _ são privados; o símbolo C ABI remove automaticamente o prefixo _

; Parâmetros de tipo básico
#{c}
c-strlen = (s str) (n i64)

; Parâmetro de ponteiro (*byte = ponteiro opaco), declaração privada
#{c}
_sqlite3-close = (db *byte) (rc i32)

; Ponteiro duplo (**byte = parâmetro de saída, o valor é automaticamente guardado de volta na variável após a chamada), declaração privada
#{c}
_sqlite3-open = (filename str, db **byte) (rc i32)

; Vários parâmetros de ponteiro, declaração privada
#{c}
_sqlite3-exec = (db *byte, sql str, callback *byte, arg *byte, errmsg *byte) (rc i32)
```

```no
; Wrapper seguro no mesmo ficheiro

open = (dsn str) (d db-sqlite) {
    handle i64 = 0
    rc i32 = _sqlite3-open(dsn, handle)
    rc != SQLITE-OK -> {
        return
    }
    d.handle = handle
}
```

**Regras:**

1. `#{c}` numa linha independente, marca a linha seguinte como declaração FFI (a sintaxe antiga `#c` continua compatível)
2. FFI é apenas declaração, sem corpo de função
3. O ponteiro tem de ter um tipo concreto (como `*byte`), não é permitido `ptr` nu
4. `**byte` serve para parâmetros de saída: após a chamada, o valor do ponteiro escrito pela função C é convertido automaticamente em `i64` e guardado na variável da chamadora
5. Todos os ponteiros são armazenados no lado Nolang como `i64` (`ptrtoint`)
6. Parâmetros do tipo `str` são convertidos automaticamente em `i8*` terminado em null
7. Nomes que começam com `_` indicam privado (não exportado); o símbolo C ABI remove o prefixo `_`
8. Declarações FFI e código comum podem ser escritos no mesmo ficheiro `.no`

> A direção inversa (fazer com que C / C++ / Rust / Go / Java / Python / C# chamem Nolang) usa `#{cabi='symbol'}` e `no build --shared`, ver [Chamar Nolang a partir da linguagem anfitriã](./host-interop)。

### Sistema de anotações (`#{...}`)

`#{...}` é o sistema de anotações genérico, uma lista de pares chave-valor separados por vírgulas. Suporta os seguintes tipos de valor:

| Sintaxe | Tipo | Exemplo |
| --- | --- | ------------------------------------ |
| Chave isolada | Booleano | `#{debug}` |
| Valor booleano | Booleano | `#{inline=true}` / `#{inline=false}` |
| Numérico | Inteiro | `#{max=100}` |
| Texto | Cadeia | `#{name='hello'}` |
| Identificador | Identificador | `#{mode=fast}` |
| Array | Array | `#{derive=[Serialize, Deserialize]}` |
| Intervalo | Intervalo | `#{range=[0..256)}` |

Vários pares chave-valor separam-se por vírgulas:

```no
#{derive=[Serialize, Deserialize], range=[0..256), max=100, debug}
```

#### Posição das anotações

`#{...}` só permite duas formas:

- **Acima**: numa linha independente, acima do alvo (instrução, campo de struct, membro de enum, declaração, braço de match);
- **Atrás**: escrito no fim da mesma linha do alvo (`res = arr[i] #{index-out=zero}`, `a pt #{inline}`,  
  `green #{deprecated}`).

Escrever **à frente do alvo na mesma linha** (prefixo, como `#{index-out=zero} res = arr[i]`) é um **erro de compilação**; o compilador e
a LSP reportam o mesmo diagnóstico. O critério: depois do `}` do grupo de anotações, não pode haver código na mesma linha (mudança de linha,  
`; comentário de linha, ou outro grupo de anotações imediatamente a seguir não contam como código).

```no
; ✓ Acima: linha independente
#{overflow=wrap}
x i8 = a + 100

; ✓ Atrás: no fim da mesma linha
x i8 = a + 100 #{overflow=wrap}
```

```no
#{fmt-skip}
; ✗ À frente: prefixo na mesma linha → erro
#{overflow = wrap} x i8 = a + 100
```

As anotações em campos de struct e membros de enum (valores de enum estilo C, variantes de enum etiquetadas) também só permitem acima ou atrás  
(`a pt #{inline}`, `a pt #{inline=true}`, `green #{deprecated}`, `ok(v i64) #{inline}`).  
Note-se que o **valor** de `#{inline}` também é suportado: `#{inline=false}` é válido (ver abaixo  
〈`# {inline}` em campos de struct〉).

Na escrita `if <cond> { ... }`, uma anotação entre a condição e o `{` (como `if x > 0 #{overflow=wrap} {`) também está
em posição de «frente» e dá o mesmo erro. A análise nesse ponto é permissiva e, no passado, engolia todo o grupo de anotações — sem as aplicar nem reportar erro,  
sendo a falha mais difícil de detetar.

A sintaxe de intervalo suporta 16 combinações de parênteses (regra: `[` inclui o limite esquerdo, `(` não; `]` inclui o limite direito, `)` não):

**Intervalos limitados (4)** — ambos os extremos têm valor:

- `[a..b]` — intervalo fechado nos dois extremos (`x >= a && x <= b`)
- `[a..b)` — fechado à esquerda, aberto à direita (`x >= a && x < b`)
- `(a..b]` — aberto à esquerda, fechado à direita (`x > a && x <= b`)
- `(a..b)` — intervalo aberto nos dois extremos (`x > a && x < b`)

**Intervalos sem limite superior (4)** — omite-se o extremo direito:

- `[a..]`、`[a..)` — esquerda incluída, sem limite superior (`x >= a`)
- `(a..]`、`(a..)` — esquerda excluída, sem limite superior (`x > a`)

**Intervalos sem limite inferior (4)** — omite-se o extremo esquerdo:

- `[..b]`、`(..b]` — sem limite inferior, direita incluída (`x <= b`)
- `[..b)`、`(..b)` — sem limite inferior, direita excluída (`x < b`)

**Intervalos completamente ilimitados (4)** — ambos os extremos omitidos, com base no valor mínimo/máximo do tipo:

- `[..]` — mínimo do tipo ≤ x ≤ máximo do tipo (ambos incluídos)
- `[..)` — mínimo do tipo ≤ x < máximo do tipo
- `(..]` — mínimo do tipo < x ≤ máximo do tipo
- `(..)` — mínimo do tipo < x < máximo do tipo

#### Âmbito das anotações

A **posição** da anotação determina o anfitrião; o **tipo** do anfitrião determina o seu âmbito. Há quatro níveis, aplicados de fora para dentro,
com precedência do mais próximo; o corpo da função é uma fronteira rígida de herança:

| Nível | Anfitrião | Âmbito |
| --- | --- | --- |
| **L0 nível de módulo** | cabeçalho do ficheiro | todas as declarações e instruções do ficheiro (anotação no cabeçalho + uma linha em branco) |
| **L1 nível de declaração** | constantes/variáveis globais, struct, interface, enum etiquetado, função, método | toda a definição dessa declaração (a função inclui o corpo e todos os blocos aninhados) |
| **L2 nível de bloco** | grupo de corrotinas, grupo if, `cond -> body`, match, loops `for`/contagem/intervalo | todas as instruções no corpo do bloco (incluindo blocos aninhados, até ao limite L1 interno) |
| **L3 nível de linha única** | uma única instrução (`let`, atribuição, `?=`, `return`, chamada, braço `cond ->`) | a própria instrução e as suas expressões |

```no
; L1: a função inteira
#{overflow=wrap}
sum = (xs []i64) (total i64) {
    ; L3: só a próxima instrução
    #{overflow=wrap}
    total = xs.len() - 1
    ; L2: o corpo inteiro do loop
    #{overflow=wrap}
    i <- [0..xs.len()): {
        total = total + xs[i]
    }
}
```

As regras completas de decisão, a matriz chave × nível e as diferenças face à implementação atual estão em
[〈Âmbito de anotações e comentários〉](annotation-scope.md)。

A anotação FFI `#{c}` é uma forma especial do sistema de anotações; quando a anotação contém uma chave de linguagem FFI (`c`, `cpp`, `rust`, etc.) e é seguida de uma declaração de função, o compilador reconhece-a como uma vinculação FFI:

```no
; #{c} com anotação adicional
#{c, debug}
_sqlite3-open = (filename str, db **byte) (rc i32)
```

#### Anexar anotações a declarações

As anotações não FFI são anexadas automaticamente à declaração imediatamente seguinte (declaração de variável, definição de struct), podendo servir para marcar metadados como limites de intervalo em tipos numéricos (como `num`, `i64`, etc.):

```no
; Declaração de variável com anotação range
#{range=[0..256)}
x num = 42

; Definição de struct com anotação
#{derive=[Serialize, Deserialize]}
point {
    x i64
    y i64
}

; Campo de struct com anotação range (pode usar-se em tipos numéricos como num)
person {
    age num #{range=[0..150]}
    score i64 #{range=[0..256)}
    name str
}
```

A anotação `range` é particularmente útil para o tipo `num` (`num = int | float`), servindo para marcar o intervalo válido de um valor. O valor do intervalo pode ser um inteiro ou um identificador:

```no
; Usar identificador constante como limite do intervalo
#{range=['i8.MIN'..'i8.MAX']}
val i8 = 100
```

#### Incorporação de ficheiros (`#{embed=...}`)

A anotação `#{embed=...}` incorpora o conteúdo de um ficheiro externo em tempo de compilação como uma constante `[]byte` só de leitura, semelhante a `//go:embed` do Go. Os dados incorporados são compilados diretamente no executável, permitindo distribuição num único binário, sem necessidade de ler ficheiros em tempo de execução.

**Sintaxe**:

```no
#{embed='path/to/file'}
ICON []byte

; Ou usar um caminho nu (sem aspas)
#{embed='assets/icon.ico'}
ICON []byte
```

A anotação `embed` anexa-se à declaração de variável `[]byte` imediatamente seguinte. A declaração da variável **não pode** ter um valor inicial explícito — o valor é fornecido pelo conteúdo do ficheiro incorporado.

**Resolução de caminhos**:

- Caminhos relativos resolvem-se a partir da raiz do pacote (o diretório que contém `package.noml`)
- Caminhos absolutos são suportados
- O caminho pode conter os caracteres `/`, `.`, `-`

**Exemplo**:

```no
; Incorporar ícone do Windows
#{embed='assets/win-icon.ico'}
WIN-ICON []byte

; Incorporar certificado SSL
#{embed='certs/server.pem'}
SERVER-CERT []byte

; Incorporar ficheiro de configuração
#{embed='config/default.json'}
CONFIG []byte

; Usar dados incorporados
print('icon size: ', len(WIN-ICON))
print('first byte: ', WIN-ICON[0])
```

**Notas**:

1. Os dados incorporados são constantes **só de leitura**, apontando para uma região de memória só de leitura. Escrever nos dados incorporados é comportamento indefinido (semântica idêntica a `//go:embed` do Go)
2. Variáveis incorporadas não participam na libertação da heap — os seus ponteiros não são libertados quando o programa termina
3. Só se suportam declarações do tipo `[]byte` (também serve um array fixo `[N]byte`)
4. O ficheiro tem de existir em tempo de compilação, caso contrário ocorre erro
5. Não pode coexistir com um valor inicial explícito (como `ICON []byte = [1, 2, 3]`, que dá erro)

#### Incorporação de pastas (`#{embed='dir'}`)

`#{embed=...}` também suporta incorporar um diretório inteiro. Quando o caminho aponta para um diretório, o compilador lê recursivamente todos os ficheiros do diretório e incorpora-os como um sistema de ficheiros só de leitura do tipo `fs.embed`. Em tempo de execução, os métodos `read()` e `exists()` acedem aos ficheiros incorporados por caminho, sem necessidade de qualquer função C — a lógica de pesquisa é implementada inteiramente em Nolang.

**Sintaxe**:

```no
; Incorporar a pasta inteira do frontend
#{embed='../frontend/dist'}
DIST fs.embed

; Ler um ficheiro do sistema de ficheiros incorporado
data = DIST.read('index.html')

; Verificar se um ficheiro existe
ok = DIST.exists('css/style.css')
```

**O tipo `fs.embed`**:

`fs.embed` é uma struct definida no módulo `fs` da biblioteca padrão do Nolang, contendo os metadados dos ficheiros incorporados (caminho, deslocamento, comprimento). O compilador lê o diretório em tempo de compilação e concatena todos os conteúdos dos ficheiros numa variável global constante; em tempo de execução, o método `read`, implementado em Nolang puro, percorre e encontra o ficheiro.

| Método   | Assinatura                            | Descrição |
| -------- | ----------------------------------- | ----------------- |
| `read`   | `(path str) (data ?[]byte)`           | Lê o ficheiro incorporado e devolve o conteúdo (`nil` se não encontrado) |
| `exists` | `(path str) (yes bool)`             | Verifica se o ficheiro incorporado existe |

**Exemplo**:

```no
; Servidor de ficheiros estáticos HTTP: incorpora os ficheiros do frontend num único binário
#{embed='../frontend/dist'}
DIST fs.embed

serve = (conn server-conn) {
    conn.parse()
    path = conn.path
    path == '/' -> path = '/index.html'

    ; Remove a / inicial para obter o caminho relativo
    rel = path.slice(1, path.len-bytes())

    ; Lê do sistema de ficheiros incorporado
    data []byte = DIST.read(rel)
    {
        ok -> it

        -> {
            conn.write-html(404, 'Not Found')
            conn.close()
            return
        }
    }
    conn.write-status(200)
    conn.write-header('Content-Type', get-content-type(path))
    conn.write-body(data)
    conn.close()
}
```

**Notas**:

1. Todos os ficheiros do diretório (incluindo subdiretórios) são incorporados recursivamente, e os caminhos são armazenados relativamente ao diretório incorporado (como `css/style.css`)
2. O separador de caminhos é normalizado para a barra `/` (consistente entre plataformas)
3. Variáveis do tipo `fs.embed` são só de leitura e não participam na libertação da heap
4. Indicado para distribuição num único binário (como servidores HTTP de ficheiros estáticos, ferramentas CLI que incorporam recursos)

#### Anotações de plataforma

As anotações de plataforma são filtros em tempo de compilação que decidem se o código é incluído consoante a plataforma de destino. Usam **chaves planas** (como `#{mac-arm64}`) que especificam o SO e a arquitetura em simultâneo, sem ambiguidade, e anexam-se à declaração imediatamente seguinte. O código que não corresponde não participa de todo na compilação — não gera LLVM IR nem é submetido a verificação de tipos.

**Chaves de plataforma suportadas (6 combinações planas):**

| Chave              | Corresponde a                  |
| ---------------- | -------------------------- |
| `#{linux-amd64}` | Linux x86_64               |
| `#{linux-arm64}` | Linux ARM64                |
| `#{win-amd64}`   | Windows x86_64             |
| `#{win-arm64}`   | Windows ARM64              |
| `#{mac-amd64}`   | macOS x86_64 (Intel)        |
| `#{mac-arm64}`   | macOS ARM64 (Apple Silicon) |

```no
#{mac-arm64}
print('running on macOS ARM64')

#{linux-amd64}
print('running on Linux x86_64')

#{win-amd64}
print('running on Windows x86_64')

; Variáveis específicas da plataforma
#{mac-amd64, mac-arm64}
sep = '/'

#{win-amd64, win-arm64}
sep = '\\'

; Funções específicas da plataforma
#{mac-arm64, mac-amd64}
greet = () {
    print('hello from mac')
}

#{linux-amd64, linux-arm64}
greet = () {
    print('hello from linux')
}

greet()
```

Várias chaves na mesma declaração têm relação de **OR** — basta que uma corresponda para incluir. Como cada chave já especifica o SO e a arquitetura em simultâneo, não é necessária lógica AND。

| Anotação                      | Significado                           |
| --------------------------- | --------------------------------- |
| `#{mac-arm64}`              | Apenas macOS ARM64                     |
| `#{mac-amd64, mac-arm64}`   | Qualquer arquitetura macOS                        |
| `#{linux-amd64, win-amd64}` | Linux x86_64 **ou** Windows x86_64 |
| `#{mac-arm64, linux-arm64}` | macOS ARM64 **ou** Linux ARM64     |

```no
; Executado no macOS e no Linux (todas as arquiteturas)
#{mac-amd64, mac-arm64, linux-amd64, linux-arm64}
shared = () {
    print('unix-like')
}

; Apenas Windows x86_64
#{win-amd64}
reg-key = () {
    print('reading registry on win/x64')
}

; Apenas macOS ARM64 (Apple Silicon)
#{mac-arm64}
neural = () {
    print('Apple Neural Engine available')
}
```

Usar `os.get-arch()` permite obter a arquitetura atual em tempo de execução, enquanto as anotações de plataforma incluem ou excluem código em tempo de compilação。

## Configuração do compilador package.noml

O bloco `compiler` em `package.noml` controla o comportamento do compilador. O valor de `version` usa por convenção um `v` inicial (como `"v0.1.0"`); na análise interna, o compilador é insensível ao prefixo, tratando `"v0.1.0"` e `"0.1.0"` como a mesma versão。

```noml
{
  compiler: {
    version: 'v0.1.0'
    emit: 'js'
  }
}
```

### `emit`

Controla o backend de saída:

| Valor           | Descrição                                      |
| ----------- | --------------------------------------- |
| `""` (vazio, predefinição) | Usa o backend LLVM para gerar um executável nativo                      |
| `"js"`      | Usa o backend JS para gerar JavaScript (apagamento de tipos), sem precisar da cadeia de ferramentas LLVM |

Com `"js"`, `no build` usa automaticamente o backend JS, equivalente a `--js` na linha de comando. As opções da linha de comando têm prioridade sobre o ficheiro de configuração。

```noml
; Gerar JavaScript automaticamente
compiler: {
  emit: 'js'
}
```

### `anonymous-fn-type`

Controla se é permitida a sintaxe de tipos de função anónima (como `cb ()()`). Predefinição `false` (desativado), sendo necessário usar um alias de tipo de função com nome。

### `link-libs`

Especifica a lista de bibliotecas C a ligar:

```noml
compiler: {
  link-libs: ['crypto', 'ssl']
}
```

## Injeção de variáveis em tempo de compilação (-ld)

`no build`, `no run` e `no test` suportam a injeção de constantes globais em tempo de compilação através de opções `-ld-KEY=VALUE`. As variáveis injetadas equivalem a declarar `KEY = VALUE` no topo do código fonte e podem ser usadas diretamente no programa。

### Sintaxe

```bash
no build -ld-KEY=VALUE [mais -ld...] <file>
```

- O prefixo `-ld` é seguido imediatamente pelo nome e pelo valor da variável, separados por `=`
- Várias opções `-ld` podem ser usadas em simultâneo
- Forma abreviada booleana: `-ld-DEBUG` (sem `=VALUE`) equivale a `-ld-DEBUG=true`

### Inferência do tipo do valor

| Forma do valor       | Tipo inferido          | Exemplo                  |
| ---------------- | --------------- | ------------------- |
| Inteiro               | `i64`           | `-ld-COUNT=42`      |
| Ponto flutuante              | `f64`           | `-ld-PI=3.14`       |
| `true` / `false` | `bool`          | `-ld-DEBUG=true`    |
| Outros               | `str` (literal de cadeia entre aspas simples) | `-ld-VERSION=0.1.2` |

### Exemplo

```no
; Assumindo compilação com -ld-VERSION=0.1.2 -ld-COUNT=42 -ld-DEBUG=true
print('VERSION:', VERSION); saída: VERSION: 0.1.2
print('COUNT:', COUNT); saída: COUNT: 42
print('DEBUG:', DEBUG); saída: DEBUG: 1 (bool imprime como 1 no backend nativo)
```

```bash
; Injetar várias variáveis
no build -ld-VERSION=0.1.2 -ld-COUNT=42 -ld-DEBUG=true main.no

; Forma abreviada booleana
no run -ld-RELEASE main.no

; O backend JS também é suportado
no build --js -ld-VERSION=0.1.2 main.no
```

### Relação com declarações no código fonte

As regras de interação entre variáveis injetadas por `-ld` e declarações no código fonte:

- **Já existe declaração com o mesmo nome no código**: o valor injetado **substitui no local** o valor dessa variável no código. O padrão comum é declarar uma variável de placeholder no código (como `VERSION = ''`) e atribuir o valor com `-ld-VERSION=0.1.2` na compilação。
- **Não existe no código**: a variável injetada é antenada no início da AST como uma nova declaração de constante global, participando de todos os passes de compilação seguintes (inferência de tipos, verificação, fusão de módulos, codegen)。
- **Sem injeção**: a declaração no código mantém-se inalterada。

```no
; Declarar variáveis de placeholder no código
VERSION = ''
COUNT = 0

print('VERSION:', VERSION)
print('COUNT:', COUNT)

; no build -ld-VERSION=0.1.2 main.no
; saída: VERSION: 0.1.2  （o valor injetado substituiu o '' do código）
;       COUNT: 0        （não injetado, mantém o valor do código）
```
