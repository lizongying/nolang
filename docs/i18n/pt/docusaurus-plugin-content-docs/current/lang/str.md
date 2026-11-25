---
sidebar_position: 3
---

# Strings

As strings do Nolang (`str`) são sequências de bytes alocadas na heap, com suporte para vários operadores e métodos.

## Layout interno e API pública

O descritor de `str` (`%str-long`) tem **16 bytes** em plataformas de 64 bits:

```llvm
%str-long = type { i32, i32, i8* }   ; u32 len, u32 cap, data
```

| Campo | Tipo interno | Tipo público | Significado |
|---|---|---|---|
| `len` | `u32` (LLVM `i32`) | `i64` | comprimento lógico em bytes; `s.len-bytes()`, `len(s)`, `s._len()` |
| `cap` | `u32` (LLVM `i32`) | `i64` | capacidade física (apenas str-long); `s.cap()`, `s._cap()` |
| `data` | `i8*` | `[]byte` / `*byte` | ponteiro para o buffer na heap |

Internamente, `len` / `cap` são estreitados para `u32` para reduzir o descritor de 24 para 16 bytes (`i32` + `i32` + um ponteiro alinhado a 8 bytes). **A API pública continua toda em `i64`**: na leitura aplica-se `zext i32 -> i64` e na escrita `trunc i64 -> i32` na fronteira do descritor. O código da biblioteca padrão e dos utilizadores não precisa (nem deve) tocar nessa conversão; as assinaturas de `s.len-bytes()`, `s.len()`, `s.cap()`, `s._len()` / `s._cap()` / `s._set-len()` / `s._set-cap()` mantêm-se em `i64`.

> ⚠️ Como os contadores internos são `u32`, o limite de comprimento/capacidade de uma única `str` é 4 GiB (`4294967295`). Um `i64` superior a esse valor é truncado na fronteira; o processamento normal de strings não é afetado, mas convém verificar antes de pré-alocar buffers gigantes.
>
> O `txt` **não é afetado por esta alteração**: continua a ser o bloco fixo de 256 bytes `{ [255 x i8] data, i8 len }`, sem campo `cap`, com o comprimento num byte.

## Operadores de string

### Concatenação (`-`)

Usa o operador `-` para concatenar strings:

```no
; Concatenação de literais
s = 'Hello' - ' ' - 'World'

; Concatenação com variável
greeting = 'Hello, ' - name
```

### Repetição (`*`)

Usa o operador `*` para repetir uma string:

```no
s = 'Hello' * 3
```

### Comparação (`==` / `!=` / `str.compare`)

`str` **implementa apenas comparação de igualdade**: `==` / `!=` passam pelo `@str_eq` do runtime, e strings de múltiplos caracteres são perfeitamente válidas.

Os operadores de ordem (<code>&lt;</code> `>` <code>&lt;=</code> `>=`) **não têm semântica de ordem lexicográfica para strings**; usá-los numa string é um erro de compilação:

- **Válido**: um literal de string de **um caractere** `'a'`, implicitamente tratado como `char` (ponto de código); pode ser comparado com outro literal de um caractere ou com um `char` (`"a"`).
- **Inválido**: strings de múltiplos caracteres — `'ab'`, uma variável `str`, ou uma chamada que retorna `str` (antes **devolviam false silenciosamente**).

Quando precisas de ordem lexicográfica, usa `str.compare(b)`:

```no
s = 'abc'
b = 'abd'
c = s.compare(b); -1（s < b）/ 0（igual）/ 1（s > b）

'a' <= 'z'; true — string de um caractere é implicitamente char
ch >= 'a'; ch é char, OK
s < b; erro de compilação: string de múltiplos caracteres
s.compare(b) < 0; forma correta
```

## Indexação e slicing

**Indexação**: o tipo inferido de `s[i]` é **`?char`** (ponto de código Unicode), não `byte` — fora dos limites retorna `err` (`code.index-out`) e dentro dos limites retorna `ok(ponto de código)` (o mesmo conjunto de regras unificadas de `+ - * / %` para inteiros). Para strings com caracteres multibyte, `s[i]` descodifica UTF-8 a partir do início da string e conta até ao i-ésimo ponto de código (ver a nota de desempenho abaixo).

```no
#{fmt-skip}
s = 'Hello World'
c = s[0]: { ok -> it  -> _ }   ; ponto de código de 'H' (?char tem de ser explicitamente desembrulhado, braço vazio obtém '\0')
```

> Há apenas três formas de consumir um `?char` (**`#{index-out=DEF}` foi descontinuado e removido no D11**, e comentários residuais são erro de compilação;
> também não existe o atalho de "puxar explicitamente de/plain para plain", `c char = s[i]` também exige consumo explícito):
> **desembrulhar com match** (`x = s[i]: { ok -> it, -> _ }`, o braço vazio obtém o valor zero de `char` `'\0'`),
> **escrita com place-match** (`s[i]: { ok -> it = x, -> _ }`) ou **abreviação `?=` com propagação para cima** (a função tem um parâmetro de resultado `?T`,
> ou a instrução está dentro do corpo de um match de bloco). Detalhes e exemplos em〈syntax · Indexação segura〉.

**Slicing**: `s[a..b]` retorna uma **vista** da string subjacente, com o tipo ainda **`str`** (partilha memória, sem cópia). Os índices de slice são **posições de ponto de código**, consistentes com `s[i]`:

```no
sub = s[6..]; 'World'
sub = s[6..11]; 'World'
sub = s[0..5); 'Hello'（esquerda fechada, direita aberta）
```

> Os índices de slice são **posições de ponto de código**, não deslocações em bytes. Se precisares de um slice por deslocação de bytes (por exemplo para acompanhar uma iteração com `s.byte(i)`), usa `s.slice-bytes(start, end)`.

```no
; Comprimento
n = s.len(); número de pontos de código (número de caracteres Unicode)
n = s.count(); o mesmo (nome de método antigo, ainda disponível)
n = s.len-bytes(); comprimento em bytes (número de bytes UTF-8)
; Atenção: o campo estrutural s.len nu deixou de ser suportado; leitura e escrita dão erro; usa s.len() / s.len-bytes()
```

### Conversão implícita (char → str)

Um `char` **converte-se implicitamente** em `str`: o ponto de código é codificado em UTF-8 numa nova string. Por isso, todas as seguintes grafias são válidas, **sem necessidade** de chamar manualmente `char.to-str()`:

```no
s = 'héllo'; 'h' 'é' 'l' 'l' 'o'（é é multibyte）

; 1) o char de s[i] atribuído diretamente a uma variável str → convertido implicitamente em string de um caractere
a str = s[0]; 'h'

; 2) o resultado do slice já tem tipo str, basta atribuir diretamente
b str = s[0..1]; 'hé'（índices 0..1, ambas as extremidades incluídas）

; 3) char participa na concatenação de strings (-) e é convertido implicitamente em str
msg = 'first: ' - s[0]; 'first: h'

; 4) char comparado com str é convertido implicitamente em str
ok = s[0] == 'h'; true
```

> **`'...'` é um literal `str` e `"..."` é um literal `char`** — mesmo que `'x'` tenha um único ponto de código, continua a ser `str`. Portanto, quando **ambos os lados** de `+` / `-` são literais (`'...'` com `"..."`), aplica-se a conversão implícita acima e o resultado é **concatenação**, não aritmética inteira:
>
> ```no
> 'x' - "a"                ; "xa"（concatenação；não 120-97 = 23）
> "a" + 'x'                ; "ax"
> 'xy' + "a"               ; "xya"
> ```
>
> Assim que **um** dos lados **não é literal** (variável char, `s[i]`, inteiro), um `'...'` de um caractere participa na aritmética inteira como **byte** — isto é deliberado e é o que permite o idiomático "obter o valor de um dígito":
>
> ```no
> c char = "5"
> n = c - '0'              ; 5（subtração de bytes, não "50"）
> m = 'A' + 1              ; 66
> "z" - "a"                ; 25（ambos os lados são literais char, subtração char）
> 'x' - c                  ; 23（c é variável → operação em bytes）
> ```
>
> Por outro lado, a degradação a byte **só ocorre quando o outro lado é um inteiro ou um char**. Se o outro lado for `bool`, `f64`, struct ou outro valor **sem semântica de byte**, o `'...'` continua a ser string e toda a expressão é **concatenação**, com o outro lado renderizado via `to-str`:
>
> ```no
> b = true
> 'x' - b                  ; "xtrue"（não 120 - 1 = 119）
> 'x' - 1.5                ; "x1.5"
> 'x' - 'y'                ; "xy"（ambos os lados são literais de string）
> ```
>
> Assim, `'x' - b` pode ser atribuído a `str`: `s str = 'x' - b` resulta em `"xtrue"`; já `s str = 'x' - 1` continua a ser rejeitado, porque é uma operação em bytes (o resultado é um número).

> A conversão implícita codifica apenas **um único ponto de código** como `str`. Se o que pretendes é o valor numérico, usa o próprio `char` (por exemplo `print(c)` imprime o valor decimal desse ponto de código); se queres uma conversão explícita, podes sempre usar `c.to-str()` (equivalente à implícita).
>
> Um `char` representa um único escalar Unicode e o seu tipo de armazenamento subjacente é **`i32`** (intervalo válido `0 ..= 0x10FFFF`).
>
> Um `char` **participa normalmente em aritmética inteira** (por exemplo `z = a + 25`, `u = ch - 32`), calculada sobre o valor do ponto de código na largura i32; ao contrário da família dos inteiros, não retorna por predefinição `option<int>`, pelo que nunca dispara o erro de compilação "overflow inteiro não tratado".
>
> **Renderização**: um `char` convertido em texto imprime sempre o **valor decimal do seu ponto de código** (`"A"` → `65`, `"é"` → `233`), e `print(c)` e a interpolação `print('{c}')` **devem dar o mesmo resultado**. A aritmética de char é feita em **i32 com sinal**, pelo que `"x" - "y"` é `-1` e ambas as vias imprimem `-1` (nunca `4294967295` numa delas). Todo o ponto de código válido está em `0 ..= 0x10FFFF` (bit 31 a zero), em que a extensão com e sem sinal coincide, pelo que esta regra só afeta os valores negativos resultantes de aritmética de char fora do intervalo. Se queres o **carácter** em vez do número, usa `c.to-str()` (`"A".to-str()` → `"A"`) — essa é outra operação.

## Otimização ASCII e notas de desempenho

A indexação `s[i]` em `str`/`txt` obtém o **i-ésimo ponto de código (carácter Unicode)**, não o i-ésimo byte. Por isso, em strings que possam conter caracteres UTF-8 multibyte (como chinês, emoji ou `é`), cada `s[i]` tem de percorrer o UTF-8 desde o início da string até contar o i-ésimo ponto de código — a complexidade temporal é **O(n)**.

Quando o compilador consegue **provar** que a string é composta inteiramente por caracteres ASCII (pontos de código 0–127), cada byte é exatamente um carácter e `s[i]` endereça diretamente o byte subjacente, degradando para **O(1)**. O compilador prova que é ASCII puro através destas regras:

- Anotação explícita: adicionar a anotação `#{ascii}` na declaração ou atribuição;
- Atribuição de um literal de string ASCII (por exemplo `'hello'`);
- Propagação por identificador: `b = a`; se `a` já estiver provado como ASCII, então `b` também;
- Concatenação (`-`) em que ambos os lados estão provados como ASCII.

```no
; Literal ASCII → o compilador prova ASCII puro, s[i] é O(1)
name = 'hello'
c = name[1]; O(1), obtém o byte 'e'

; Contém caracteres multibyte → não é possível provar ASCII, s[i] é O(n) por ponto de código
s = 'héllo'
c = s[1]; O(n), obtém o 1.º ponto de código 'é' (não um byte)
```

### Aviso de desempenho (LSP / `no vet`)

Quando **percorres manualmente pontos de código por índice dentro de um loop** sobre uma `str`/`txt` **não provada como ASCII** — ou seja, o padrão `for i <- [0..s.count()): { s[i] }` de obter caracteres por índice de ponto de código — tanto o `no vet` como o LSP do editor emitem um aviso estático de nível **WARNING** (não proíbe a sintaxe, apenas alerta). Cada `s[i]` percorre o UTF-8 desde o início da string até ao i-ésimo ponto de código, pelo que o loop inteiro degrada para **O(n²)**.

Um único `s[i]` fora de um loop (apenas uma consulta O(n)) **não** gera aviso; e uma iteração direta `for c <- s` também **não** gera aviso (ver abaixo).

Recomenda-se usar alternativas mais rápidas:

- Se precisas de iterar sobre todos os caracteres, usa diretamente `for c <- s` — a abordagem preferida, um único scan O(n) para a frente, que evita o O(n²) de `s[i]` repetido. O compilador avança por **ponto de código UTF-8 (carácter Unicode)**, guardando em cada passo o valor do ponto de código descodificado na variável de loop `c`; é semanticamente equivalente a `for c <- s.to-chars()`:
  ```no
  c <- s: {
      ; c é o ponto de código atual, do tipo char (já descodificado em UTF-8, não um byte)
  }
  ```
- Se só precisas de acesso por bytes (por exemplo codificação/descodificação, hashing, memcmp), usa a escotilha de escape `s.byte(i)` — endereça sempre diretamente o i-ésimo byte, é **sempre O(1)** e não entra no corpo da função std:
  ```no
  b = s.byte(0); obtém o byte 0, do tipo ?byte, O(1)
  out = 0
  b: {
      ok -> {
          out = it; dentro dos limites: ok(byte)
      }

      -> {
          out = 0; fora dos limites: nil (o braço vazio sintetiza o valor zero)
      }
  }
  ```
  > O resultado de `s.byte(i)` é **`?byte`** (desde 2026-10-21): antes, a leitura de byte direta não fazia verificação de limites e um índice fora dos limites era comportamento indefinido. Agora, fora dos limites retorna `nil` — atenção que isto é **diferente** da indexação de contentores `arr[i]` / `s[i]`, que retorna `err` (`code.index-out`) fora dos limites: `s.byte(i)` é uma escotilha de escape ao nível de byte, e "fora dos limites" apenas significa "não existe esse byte", não é um erro.
  > (Ver design/option-unified.md §3.1, item 5). Por isso `b i64 = s.byte(0)` reporta
  > `15w45dqk` — uma anotação plain explícita associada a uma chamada option é um desembrulho silencioso, erro de compilação rígido;
  > usa `?=`, um match em duas fases (`b = s.byte(0): { ok -> it, -> _ }`) ou `_ =` para consumir.
  > `txt.byte(i)` tem a mesma semântica (o campo `byte` 1 do `txt` é o comprimento em bytes).

> A biblioteca padrão (src/std) já migrou todos os acessos por byte para `s.byte(i)`: incluindo a codificação/descodificação UTF-8 (`decode-cp`) em `str.no`/`txt.no`, a comparação (`compare`/`starts-with`/`ends-with`), hashing, cópia de slice (`slice`/`repeat`/`trim`/`copy`), `to-bytes`/`to-upper`/`to-lower`/`reverse`/`replace-char`/`count`/`index`, etc. Assim, o `no vet` já não precisa de ignorar ficheiros da biblioteca padrão; o acesso a elementos `x[i]` nos restantes tipos `[]byte`/slice/array já é por índice de elemento (byte), com semântica inalterada e sem aviso.

| Grafia | Semântica | Complexidade | Notas |
|---|---|---|---|
| `s[i]` (ASCII provado) | i-ésimo byte = i-ésimo carácter | O(1) | provado automaticamente, sem alterações |
| `s[i]` (ASCII não provado, dentro de loop) | i-ésimo ponto de código | O(n²), anti-padrão | dispara aviso do LSP |
| `s[i]` (ASCII não provado, fora de loop) | i-ésimo ponto de código | O(n) uma vez | sem aviso |
| `s.byte(i)` | i-ésimo byte (`?byte`, `nil` fora dos limites) | O(1) | escotilha de escape de acesso a byte |
| `for c <- s` | ponto de código a ponto de código | O(n) num único scan | preferido para iteração, sem aviso |

## Métodos de string

```no
#{fmt-skip}
; Comparação
ok = a.eq(b, n)               ; comparação de igualdade (método)
c = s.compare(b)              ; comparação lexicográfica

; Pesquisa (index/rindex retornam uma posição de PONTO DE CÓDIGO (carácter), consistente com s[i])
pos = s.index(sub)             ; índice de ponto de código da primeira ocorrência, -1 se não encontrado
pos = s.index-from(sub, k)     ; pesquisa a partir do k-ésimo ponto de código
pos = s.rindex(sub)            ; índice de ponto de código da última ocorrência
pos = s.rindex-from(sub, k)    ; pesquisa inversa a partir do k-ésimo ponto de código
pos = s.last-index(sub)        ; alcunha de rindex
ok  = s.contains(sub)          ; contém a subcadeia
ok  = s.starts-with(sub)       ; verificação de prefixo
ok  = s.ends-with(sub)         ; verificação de sufixo
ok  = s.empty()                ; está vazia

; Nota: a família index retorna uma posição de PONTO DE CÓDIGO, logo s[s.index(sub)] obtém o primeiro
;       carácter de sub, consistente com a semântica de índice por ponto de código de s[i]. Por exemplo '日本語'.index('語') == 2,
;       e s[s.index('語')] é o ponto de código de '語'.
;       Para slice por PONTO DE CÓDIGO, s.slice(start, end) já tem semântica de ponto de código (consistente com s[i]);
;       para slice por DESLOCAÇÃO EM BYTES (por exemplo para acompanhar s.byte), usa s.slice-bytes(start, end).
;       Internamente, str disponibiliza um localizador ao nível de byte str.find-byte-from (reutilizado por split, etc.).
;       Atenção: o tipo txt mantém slice/at com semântica de bytes (txt é um buffer de bytes cru), divergindo de str.

; Conversão
out = s.to-upper()             ; converter em maiúsculas
out = s.to-lower()             ; converter em minúsculas
out = s.trim()                 ; remover espaços em branco no início e no fim
out = s.trim-char(c)           ; remover um carácter especificado
out = s.repeat(n)              ; repetir
out = s.reverse()              ; inverter
out = s.slice(start, end)      ; slice (índices são posições de ponto de código, consistentes com s[i])
out = s.slice-bytes(b, e)      ; slice (índices são deslocações em bytes, uso interno)
val = s.replace-char(old, new) ; substituir carácter

; Converter para outros tipos
b = s.to-bytes()               ; converter para []byte
v = s.to-i64()                 ; converter para ?i64
v = s.to-f64()                 ; converter para ?f64
v = s.to-bool()                ; converter para ?bool

; Dividir e juntar
parts = s.split(sep)           ; dividir (retorna []str)
out = ss.join(sep)             ; juntar []str

; Copiar e preencher
dst = s.copy()                 ; cópia da string
s.fill(val byte)               ; preencher com um valor byte
```

## Conversão entre string e número

```no
; Número para string (método)
s = i64.to-str(); i64 para string
s = f64.to-str(); f64 para string
s = bool.to-str(); bool para "true"/"false"
s = byte.to-str(); byte para string
s = char.to-str(); char para string

; String para número (retorna option)
v = s.to-i64(); retorna ?i64
v = s.to-i32(); retorna ?i32
v = s.to-u64(); retorna ?u64
v = s.to-f64(); retorna ?f64
```

## Escrita por índice desativada (`str` 2026-11-11 / `txt` 2026-11-12)

> **`s[i] = v` é o erro de compilação `idxwrite`**, sem exceção para as três bases `with-cap` / `with-len` / `with-cap-len`. **O `t[i] = c` do `txt` também está desativado** (motivo abaixo).
>
> O motivo em `str`: era a única fonte de "escrita para além do fim de um buffer alocado" — um buffer com len=0 crescia através de escritas diretas, e escrever em `i >= cap` saía da região alocada. A guarda de crescimento `ensureStrLongBuffer` do codegen existia exatamente para isso; com a proibição da escrita direta, a guarda deixou de ser acessível ao código do utilizador, mantendo-se apenas como código defensivo.
>
> O motivo em `txt` é diferente mas igualmente desqualificador: `t[i] = c` **recodifica um ponto de código** e volta a escrevê-lo, pelo que o comprimento muda como efeito colateral (`i` igual ao número de pontos de código equivale a acrescentar, e o que excede 255 bytes é descartado silenciosamente). O txt não tem heap nem expansão, mas "uma escrita por índice mudou secretamente o comprimento" é o mesmo tipo de perigo.

Quatro grafias continuam legais:

```no
; ① CONSTRUÇÃO byte a byte: buffer []byte recebe os dados, converte-se tudo no fim para str
b []byte = with-cap-len(n, n)
b[0] = 48
b[i] = src.byte(i): {
    ok -> it

    -> 0
}
s = b.slice(0, k).to-str(); também se pode usar b.to-str() (bloco inteiro)

; ② SOBRESCRITA in-place do conteúdo existente: str.fill preenche todos os len bytes com o mesmo valor, comprimento inalterado
s = 'hello'
s.fill(72); 'HHHHH'

; ③ A única escrita por índice restante é place-match (comporta-se como uma LEITURA: se i >= len, ignora silenciosamente, sem crescer nem sair dos limites)
s[i]: {
    ok -> it = v
    -> _
}

; ④ Para MUDAR DE COMPRIMENTO: a família push (uma chamada explícita e auditável)
s.push("中");      acrescenta um ponto de código (UTF-8)
s.push-str('de');  acrescenta uma string (expansão in-place)
```

> ⚠️ **place-match é uma comporta de PONTOS DE CÓDIGO, não de bytes.** A sua comporta vem da **leitura** `s[i]`, e a leitura por índice em `str` conta pontos de código. Portanto, ③ só serve para trabalho ASCII de "um ponto de código por posição" (hífens em uuid, dígitos em time, consulta em tabela hex). Para montar conteúdo **byte a byte** (incluindo bytes `>= 0x80`, ou qualquer string de origem que possa não ser ASCII) tens de usar `[]byte` como em ①:
> em medições, com `f = with-len(8)` e escrita sucessiva de 224..231 via place-match, só as primeiras 4 posições ficam gravadas,
> sendo as restantes ignoradas silenciosamente pela comporta — foi exatamente assim que `path.split` truncou nomes de ficheiro não ASCII.

> **Truncação de comprimento**: a atribuição nua `s.len = n` deixou de ser suportada; usa antes slicing — `s.slice(0, n)` é slice por **ponto de código**,
> `s.slice-bytes(0, n)` é que é slice por byte. (`b.slice(0, k)` num buffer `[]byte` é naturalmente um slice por byte.)
>
> **Crescer uma `str` construída com `with-cap(n)`**: `s str = with-cap(256)` é um buffer com len=0 mas com capacidade já alocada, e o primeiro `push` escreve diretamente sem desencadear realloc (`push-str` só faz realloc quando `len + comprimento acrescentado` excede `cap`, crescendo depois em dobro). A escrita byte a byte `buf[i] = b` continua proibida em `str`.

### Crescimento: a família `push` (2026-11-12)

Com as escritas diretas proibidas, **o crescimento tem de ser uma chamada explícita**:

| Método | Semântica |
|---|---|
| `s.push(c char)` | acrescenta um ponto de código, codificado em UTF-8 (internamente delega em `push-str`) |
| `s.push-str(t str)` | acrescenta uma string, copiada **byte a byte**, com expansão in-place |

```no
s = 'ab'
s.push("中");      'ab中', len-bytes 5, len 3
s.push-str('de');  'ab中de', len-bytes 7

t = 'zz'
t.push-str(t);     'zzzz'（acrescentar a si próprio é seguro: retém-se a origem e depois copia-se com memmove）

u = ''
u.push-str('');    ''（origem vazia não altera nada nem aloca nada）
```

> `push-str` é uma incorporação do compilador (`#{buildin}`, reduzida pelo MIR a "expandir in-place + memmove + gravar o novo comprimento"),
> não um loop std: uma acrescentação custa O(comprimento acrescentado) e acrescentações repetidas são O(1) amortizado (duplicação).
> `push` passa pela codificação UTF-8, pelo que `len-bytes` aumenta do comprimento codificado do ponto de código e `len` aumenta sempre 1.

> **As escritas em `txt` também estão proibidas (2026-11-12)**: `t[i] = c` **recodifica um ponto de código** e volta a escrevê-lo, pelo que o comprimento muda como efeito colateral — acrescenta quando `i` é igual ao número de pontos de código e descarta silenciosamente o que excede 255 bytes.
> O que a proibição quer eliminar é exatamente "uma escrita por índice mudou secretamente o comprimento", e o txt é tratado como o str. A via de crescimento do txt são `t.push(c)` / `t.push-str(s)` / `t.push-txt(u)`; alterações in-place usam place-match.
> A leitura `t[i]` continua a retornar o `char` do ponto de código, isso não mudou.
>
> Os escritores brutos ao nível de byte `str._set-byte` / `txt._set-byte` são **privados do std** desde 2026-11-11
> (só `str.fill` / `txt.push` etc. dentro do std os chamam), e o código do utilizador não os pode usar.
> Para montar um `txt` byte a byte, junta primeiro um `[]byte` e copia-o de uma vez com `txt.from-bytes(b)`
> (o que excede 255 bytes é truncado automaticamente e o comprimento é definido pelo número real de bytes).

## Pré-alocação (sintaxe incorporada)

Através das três sintaxes incorporadas `with-cap`, `with-len` e `with-cap-len` é possível pré-alocar memória na heap, evitando que os `push` seguintes desencadeiem expansões repetidas:

```no
; with-cap(cap): aloca capacidade cap, len=0 (é preciso dar push antes de indexar)
s = with-cap(256); str, len=0, cap=256

; with-len(len): aloca capacidade len, len=cap (leitura por índice direta permitida)
s = with-len(128); str, len=128, cap=128

; with-cap-len(cap, len): especifica capacidade e comprimento em simultâneo, ideal para "reservar espaço para crescer"
s = with-cap-len(512, 64); str, len=64, cap=512
```

| Sintaxe incorporada | Parâmetros | len | cap | Caso de uso |
|---|---|---|---|---|
| `with-cap(cap)` | 1 | 0 | cap | Só se conhece o limite; o comprimento cresce com push |
| `with-len(len)` | 1 | len | len | Comprimento fixo; leitura por índice direta |
| `with-cap-len(cap, len)` | 2 | len | cap | Comprimento inicial conhecido + espaço de crescimento reservado |

> Os parâmetros e os valores retornados por `s.cap()` / `s.len-bytes()` são todos **`i64`** (a API pública não muda), mas o descritor guarda internamente len / cap como **`u32`**. O que o chamador vê continua a ser um valor de 64 bits, e a conversão é feita pelo compilador na fronteira do descritor;
> ver〈Layout interno e API pública〉. Atribuições de campo como `s.len = n` / `s.cap = n` também passam por essa camada de fronteira,
> pelo que não sobrescrevem campos vizinhos do cabeçalho.

> ⚠️ **Um buffer `str` nunca permite escrita direta por índice** (desde 2026-11-11, as bases `with-cap` também deixaram de estar isentas).
> A isenção with-cap de `idxwrite` agora serve apenas **`[]byte` / `[]T` / arrays fixos**: os seus buffers com len=0 ainda crescem com `buf[i] = b` e não têm grafia equivalente.
> Para `str`, passa a um buffer `[]byte` + `to-str()` (ver a secção〈Escrita por índice desativada〉).
> `with-len(n)` / `with-cap-len(cap, len)` já têm o comprimento no lugar, **e a escrita exige place-match `buf[i]: { ok -> it = v / -> _ }` ou `buf[i] ?= v`** — tal como qualquer outro contentor, a escrita direta é
> o erro de compilação `idxwrite`. (`with-len` deixou de estar isento a 2026-11-10.)

> **Inferência de tipo**: o tipo de resultado das três sintaxes incorporadas é inferido a partir do lado esquerdo da atribuição e pode ser usado para `str` ou slices `[]T`.
