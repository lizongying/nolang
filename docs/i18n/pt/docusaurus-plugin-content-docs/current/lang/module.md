---
sidebar_position: 6
---

# Prefixo de chamada entre módulos

O Nolang impõe uma convenção obrigatória de namespace de módulos: ao chamar funções ou constantes definidas em **outros módulos** (outros ficheiros `.no`) a partir de um ficheiro `.no`, deves usar o prefixo `ShortName.`. Isto evita conflitos de nomes entre módulos.

Os módulos da biblioteca padrão são carregados automaticamente pelo compilador — **não é necessária importação explícita** (não precisa de anotação `# std/...`); basta usar o prefixo `ShortName.`. O `# std/fmt` é especialmente redundante — `print` / `eprint` / `format` são incorporações do compilador e o `no fmt` remove automaticamente um `# std/fmt` sem alcunha (ver [Estilo de código · Arranjo de importações](./code-style#arranjo-automático-de-instruções-de-importação-deduplicação--remoção-de-stdfmt--ordenação)).

> **Dependências e resolução de caminhos**: a chave de dependência de um pacote local pode ser mapeada para um nome curto ou para um URL completo através de `workspace.noml`. Consulta [Tipos de dependência e regras de versionamento](../usage#依赖类型与版本号规则).

## Definição de ShortName

O ShortName é o último segmento do caminho do módulo, usado como prefixo nas chamadas entre módulos.

| Caminho do ficheiro | FullPath | ShortName | Descrição |
| --- | --- | --- | --- |
| `std/math.no` | `math` | `math` | Ficheiro de nível superior |
| `std/fs.no` | `fs` | `fs` | Ficheiro de nível superior |
| `std/net/net.no` | `net/net` | `net` | Último segmento do caminho |
| `std/net/client.no` | `net/client` | `client` | Último segmento do caminho |
| `std/crypto/sha256.no` | `crypto/sha256` | `sha256` | Último segmento do caminho |
| `std/archive/gzip.no` | `archive/gzip` | `gzip` | Último segmento do caminho |

O ShortName é o último segmento do FullPath quando dividido por `/` (por exemplo `crypto/sha256` → `sha256`).

## Quando o prefixo é necessário

Ao chamar funções ou constantes de nível de módulo definidas noutros módulos, deves usar o prefixo `ShortName.`.

```no
; Funções de nível de módulo
sha256.sha256(data)
sha256.sha256-hex(data)
fs.open(path, opts)
gzip.gzip-decompress(data)
math.pow(x, y)

; Constantes de módulo
net.NET-BUF-SIZE
math.PI
```

### Convenção de nomenclatura de funções

**Não acrescentes o nome do módulo como prefixo ao nome da função.** As funções dentro de um módulo devem usar apenas nomes curtos e intuitivos; o prefixo `ShortName.` nas chamadas entre módulos já transporta a informação do módulo.

```no
; ✅ Correto：nomes de função concisos, sem prefixo de módulo
; tail.no
; a função de entrada usa diretamente o nome do módulo
tail = () {
    ...
}

; as funções auxiliares usam nomes curtos
atoi = (s str) (v i64) {
    ...
}
```

```no
#{fmt-skip}
; ❌ Evitar：nomes de função com prefixo de módulo redundante
; tail-run = () { }
; tail-atoi = (s str) (v i64) { }
```

Mantém a mesma convenção ao importar entre módulos:

```no
; ✅ Conciso e intuitivo
# /src/tail.tail
# /src/mktemp.mktemp
```

```no
#{fmt-skip}
; ❌ Redundante
; # /src/tail.tail-run
; # /src/mktemp.mktemp-run
```

> **Evita palavras-chave**: `awy` (palavra-chave de grupo de corrotinas), `match` (palavra-chave de correspondência condicional), etc. não podem ser usados como nomes de função. `run` deixou de ser reservado e pode ser usado como identificador comum. Recomenda-se que a função de entrada use o próprio nome do módulo (por exemplo `ping.no` → `ping`).

## Quando o prefixo não é necessário

Os seguintes casos não precisam de prefixo:

### 1. Funções globais (`with-cap` / `with-len` / `with-cap-len` / `print` / `eprint` / `format` / `exit`)

Estas 7 funções são incorporações globais ao nível da linguagem e podem ser usadas diretamente, sem prefixo de módulo. As suas declarações de comentário estão centralizadas em `std/global.no`, para fácil consulta.

**Construtores de capacidade/comprimento:**

- `with-cap(cap)` — cria uma string ou slice com a capacidade especificada (len=0), com o tipo inferido a partir do lado esquerdo da atribuição
- `with-len(len)` — cria uma string ou slice com o comprimento especificado
- `with-cap-len(cap, len)` — cria uma string ou slice com a capacidade e o comprimento especificados

**Saída/formatação:**

O Nolang usa a sintaxe de **strings de formatação nomeadas** `{name[:spec]}`, que referencia diretamente variáveis do escopo, sem argumentos posicionais. É suportada validação em tempo de compilação. A saída é escrita diretamente através das chamadas de sistema `io.out`/`io.err`, sem depender do `printf` da libc.

- `print(s)` / `print(s0, s1, ...)` — escreve para a saída padrão, vários argumentos separados por espaços, **adiciona nova linha automaticamente**
- `eprint(s)` / `eprint(s0, s1, ...)` — escreve para a saída de erro padrão, vários argumentos separados por espaços, **adiciona nova linha automaticamente**
- `format(s)` — retorna a string formatada (substitui `sprintf`), sem adicionar nova linha

**Controlo de processo:**

- `exit(code)` — termina o processo com um código de estado (antes `os.exit`, agora promovido a incorporação global, sem prefixo `os.`)

**Com vários argumentos, cada literal de string é um template — não uma string de formatação ao estilo C.** Em `print('result={val}', 42, 'result={val}')`, cada literal de string é um template **por si próprio**: o seu `{val}` é resolvido a partir do escopo do **local da chamada**, independentemente dos outros argumentos da mesma chamada. Um literal **nunca** descreve nem consome os argumentos seguintes — não é uma substituição ao estilo `printf('%d', 42)`. Os argumentos não literais (variáveis, expressões) são tratados como argumentos variádicos comuns, separados por espaços, com uma única nova linha no fim de toda a chamada.

```no
val = 42
print('result={val}', 42, 'result={val}'); result=42 42 result=42
tmpl = '{val}'
print(tmpl); {val} —— uma variável é texto simples, sem substituição
print('a={val}', val); a=42 42
```

`format(s)` e o `sprintf(s)` já descontinuado continuam a aceitar apenas **uma única string de formatação**, sem forma com vários argumentos.

> `printf` e `eprintf` foram **removidos**: os nomes permanecem na tabela de símbolos (a chamada ainda resolve e o editor ainda os completa),
> mas **qualquer chamada é um erro de compilação rígido** (`[printf-depr]`), cuja mensagem orienta diretamente a migração. `sprintf` ainda funciona, mas está **descontinuado**.
> As substituições são as seguintes:
> - `printf(s)` → `print(s)` (adiciona nova linha) ou `io.out(s)` (sem nova linha, saída padrão)
> - `eprintf(s)` → `eprint(s)` (adiciona nova linha) ou `io.err(s)` (sem nova linha, saída de erro padrão)
> - `sprintf(s)` → `format(s)` (retorna a string formatada)
>
> `io.out`/`io.err` são comandos de baixo nível cuja saída **não adiciona nova linha**. Como as chamadas de módulo devem incluir o prefixo do módulo, `io.err` traz explicitamente o prefixo e não entra em conflito com o construtor de Option `err()`; mesmo havendo nomes iguais, o prefixo do módulo distingue-os.

```no
; Construtores de capacidade/comprimento (sem prefixo)
s str = with-cap(256); ✅ pré-aloca 256 bytes para str
v []i64 = with-cap(100); ✅ pré-aloca 100 elementos para o slice
v []i64 = with-cap-len(200, 100); ✅ capacidade 200, comprimento 100
v []i64 = with-len(100); ✅ slice de comprimento 100

; Saída/formatação (sem prefixo)
print('hello {n}'); ✅ adiciona nova linha
print(a, b, c); ✅ vários argumentos, separados por espaços, adiciona nova linha
print('x={n}', n); ✅ literal é template, variável é valor
print('result={val}', 42, 'result={val}'); ✅ cada literal substitui por si próprio
print(); ✅ sem argumentos, output apenas uma nova linha
s = format('x={x}'); ✅ retorna string formatada (uma única string de formatação)
eprint('err: {n}'); ✅ escreve para stderr e adiciona nova linha
eprint('err:', a, b); ✅ vários argumentos, separados por espaços em stderr
print('número {id:06} valor {money:.2f}'); ✅ suporta alinhamento, preenchimento, largura, precisão

; Comandos de baixo nível (exigem prefixo de módulo)
io.out('no-newline-here'); ✅ saída sem nova linha (substitui printf)
io.err('err-no-newline'); ✅ stderr sem nova linha (substitui eprintf)

; Todas as outras chamadas entre módulos exigem prefixo
fs.open(path, opts); ✅ com prefixo (também para as incorporações)
```

#### Especificadores de formatação

O `spec` em `{name[:spec]}` suporta os seguintes campos (ordem fixa, todos opcionais):

```
[[fill]align][sign][#][0][width][.precision][type]
```

- `fill` — carácter de preenchimento (espaço por predefinição), deve ser usado com `align`
- `align` — `<` alinhado à esquerda, `>` alinhado à direita, `^` centrado
- `sign` — `+` mostra sinal de mais para positivos, `-` mostra sinal apenas para negativos (predefinição)
- `#` — prefixo de base (`0x`/`0o`/`0b`)
- `0` — preenche números com zeros até à largura especificada
- `width` — largura mínima do campo
- `.precision` — casas decimais para floats / comprimento máximo para strings
- `type` — `d`(inteiro), `x`/`X`(hexadecimal), `o`(octal), `b`(binário), `c`(carácter), `f`(ponto fixo), `e`/`E`(notação científica), `g`/`G`(geral), `s`(string, predefinição), `t`(nome do tipo, determinado em tempo de compilação), `v`(valor literal: strings entre aspas simples, vec chama to-str)

```no
x i64 = 42
u u64 = 255
pi f64 = 3.14159
s str = 'hello'
print('{x:06}'); 000042
print('{x:>10}'); alinhado à direita, largura 10
print('{u:#x}'); 0xff
print('{pi:.2f}'); 3.14
print('{pi:8.3e}'); 3.142e+00
print('{s:<10}'); hello     （alinhado à esquerda）
print('{s:.3}'); hel（truncado a 3 caracteres）
print('{x:t}'); i64（imprime o nome do tipo da variável x）
print('{s:v}'); 'hello'（string entre aspas simples）
```

`{{` e `}}` produzem os caracteres literais `{` e `}` — mas **apenas quando a mesma string contém também pelo menos um campo `{name}` real**. Um literal que contenha apenas escapes é emitido tal qual: `print('{{n}}')` imprime `{{n}}` (não `{n}`) e `print('{{')` imprime `{{`. Para imprimir uma chave isolada, usa diretamente `print('{')` / `print('}')`.

### 2. Definições no mesmo ficheiro

Funções, constantes e métodos definidos no mesmo ficheiro `.no` podem ser usados diretamente, sem prefixo.

```no
; Em sha256.no：
sha256(data); sha256 está definido neste ficheiro

; Constante definida neste ficheiro
HMAC-BLOCK-SIZE
```

### 3. Métodos de tipos incorporados

Os métodos de tipos incorporados (`str`, `i64`, `vec`, `arr`, `byte`, `char`, `bool`, etc.) não precisam de prefixo. Os métodos são incorporados no tipo e resolvidos através do tipo do recetor.

```no
'hello'.starts-with('he'); método de str
n.to-str(); método de int
v.push(42); método de vec
a.contains(3); método de arr
c.is-digit(); método de char
```

### 4. Métodos de instância de struct

Chamar métodos numa instância de struct já criada não exige prefixo de módulo. Os métodos são resolvidos através do tipo da instância — o compilador encontra automaticamente a definição `struct.method` correspondente.

```no
f = fs.open(path, opts); fs.open é uma função de nível de módulo, exige prefixo
f.read(buf, n); file.read é um método de struct, não exige prefixo
f.close(); file.close é um método de struct, não exige prefixo

p = path {
    p: '/tmp'
}
p.exists(); path.exists é um método de struct, não exige prefixo
```

## Chamadas de método vs chamadas de função de módulo

Uma chamada de método exigir ou não prefixo depende do **proprietário do método**:

- **Métodos de tipos incorporados** (`str.starts-with`, `i64.to-str`, etc.) — não exigem prefixo
- **Métodos de instância de struct** (`f.read`, `p.exists`, etc.) — não exigem prefixo
- **Funções de nível de módulo** (`fs.open`, `sha256.sha256`, etc.) — **exigem prefixo**

Em `fs.fil()`, `fs` é o ShortName do módulo e `fil` é o nome da função de nível de módulo. O prefixo `fs.` não pode ser omitido porque `fs` aqui não é um nome de variável, mas sim um caminho de módulo.

### As duas semânticas de `Name.Function`

`process.cmd(...)` e `p.start(...)` têm ambos a forma `xxx.yyy()`, mas semânticas completamente diferentes:

| Forma | `xxx` | `yyy` | Significado |
| --- | --- | --- | --- |
| `process.cmd(...)` | ShortName do módulo | Função de nível de módulo | `xxx` é um caminho de módulo, `yyy` é uma função autónoma definida nesse módulo |
| `p.start(...)` | Variável de instância | Método de struct | `xxx` é uma variável do tipo `process`, `yyy` é um método definido como `process.start = ...` |

**Diferença na definição**:
- As funções de nível de módulo definem-se **sem prefixo**: dentro do módulo escreve-se `cmd = (program str, ...) { }`
- Os métodos de struct definem-se **com prefixo `struct.`**: `process.start = (program str, ...) { }`

**Diferença na chamada**:
- As funções de nível de módulo são chamadas externamente como `ModuleName.function()`: `process.cmd(...)`
- Os métodos de struct são chamados através de uma instância: `p = process.new()` → `p.start(...)`

> **Nota**: mesmo dentro de um módulo, chamar uma função de nível de módulo do mesmo módulo não usa prefixo (`cmd(...)`), enquanto os métodos de struct são invocados através do `self` implícito ou da sintaxe `.method()`.

## Referências a tipos entre módulos

Ao referenciar tipos (structs, interfaces, enums, etc.) definidos em **outros módulos**, é obrigatório o prefixo `ShortName.`. Isto aplica-se a:

### Struct que implementa uma interface

Quando uma struct implementa uma interface definida noutro módulo, o nome da interface deve levar o prefixo do módulo:

```no
#{fmt-skip}
; ❌ Errado：db、rows、stmt são interfaces definidas pelo módulo sql, não se pode omitir o prefixo
db-mysql db {
    fd i64
}
```

```no
; ✅ Correto：usar sql.db、sql.rows、sql.stmt
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

### Tipos de parâmetros e de retorno de funções

Os tipos entre módulos em assinaturas de funções também exigem prefixo:

```no
; ✅ Correto：o tipo de retorno usa sql.result
db-mysql.exec = (sql str) (r sql.result) {
    ...
}
```

### Tipos de campos de struct

```no
; ✅ Correto：o tipo do campo usa sql.connection
conn-mysql sql.db {
    handle sql.connection
}
```

### Casos que não precisam de prefixo

- **Tipos definidos no mesmo módulo**: structs, interfaces e enums definidos no mesmo ficheiro `.no` são usados diretamente pelo nome do tipo
- **Tipos incorporados**: tipos incorporados como `str`, `i64`, `bool`, `byte` não precisam de prefixo
- **Interfaces incorporadas**: interfaces incorporadas na linguagem como `enter`, `leave` não precisam de prefixo

```no
; Tipos definidos no mesmo ficheiro não precisam de prefixo
result {
    last-id i64
    affected i64
}

; enter/leave são interfaces incorporadas, não precisam de prefixo
; result é uma struct definida no mesmo ficheiro, não precisa de prefixo
db {
    enter
    leave
    close() (ok bool)
    exec(sql str) (r result)
}
```

## Exemplo completo

```no
; Os módulos da biblioteca padrão são carregados automaticamente, sem importação explícita

; ─── Sem prefixo ───

; Funções globais (declaradas em global.no)
s str = with-cap(256)
v []i64 = with-len(100)
print('hello {n}')
s = format('x={x}')

; Função do mesmo ficheiro
sha256(data)

; Método de tipo incorporado
'hello'.starts-with('he')
n.to-str()
v.push(42)

; Método de instância de struct
f = fs.open(path, opts)
f.read(buf, n)
f.close()

; ─── Com prefixo ───

; Funções de nível de módulo
sha256.sha256(data)
sha256.sha256-hex(data)
fs.open(path, opts)
gzip.gzip-decompress(data)
math.pow(x, y)

; Constantes de módulo
net.NET-BUF-SIZE
math.PI

; Referências a tipos entre módulos (implementação de interface, tipos de parâmetro/retorno, tipos de campo)
db-mysql sql.db {
    fd i64
}

r sql.result = d.exec('CREATE TABLE ...')
```
