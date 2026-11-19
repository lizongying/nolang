---
sidebar_position: 3.2
---

## Biblioteca principal

### fmt — saída formatada

O Nolang usa **strings de formatação nomeadas** `{name[:spec]}`, que referenciam variáveis do escopo diretamente, sem argumentos posicionais. A saída passa pelas chamadas de sistema `io.out`/`io.err`, sem depender do `printf` da libc.

```no
print('x={x}'); formato nomeado, adiciona nova linha automaticamente (stdout)
print('result={val}', 42, 'result={val}'); vários argumentos：separados por espaços；cada literal é um template por si
eprint('err {x}'); formato nomeado, adiciona nova linha automaticamente (stderr)
print('número {id:06} valor {money:.2f}'); suporta alinhamento/preenchimento/largura/precisão
s = format('x={x}'); retorna a string formatada (substitui sprintf)
io.out('no-newline-here'); comando de baixo nível, saída sem nova linha (stdout)
io.err('err-no-newline'); comando de baixo nível, saída sem nova linha (stderr)
; printf/eprintf foram removidos (chamar dá erro de compilação [printf-depr])：printf→print/io.out、eprintf→eprint/io.err
; sprintf ainda funciona, mas está descontinuado：sprintf→format
; io.err traz explicitamente o prefixo do módulo, não entrando em conflito com o construtor de Option err()
```

### math — funções matemáticas (API em forma de função)

Todas as APIs numéricas do std em "forma de função" estão concentradas no módulo math (std/math.no); as "formas de método" (`*.to-str`, sqrt/sin/exp de f64/f32, sqrt/is-prime de inteiros) estão todas no módulo number (ver a secção number abaixo). Tipos union e métodos de membro: num → std/num.no, int → std/int.no, float → std/float.no.

**Constantes:** `math.PI`, `math.E`, `math.LN10`（ln 10, implementação associada a f64-to-str）

**Comparação (funções variádicas genéricas num):** `math.max`, `math.min`（`a ..num`, inteiros e ponto flutuante seguem o mesmo percurso de monomorfização）

**Potência/raiz (funções):** `math.pow` (a^b em f64, implementação pura em .no: `exp(y*ln|x|)` + tratamento do sinal da base negativa (expediente ímpar → negativo, par → positivo; expoente não inteiro → NaN), **sem depender de libm**), `math.hypot` (sqrt(x*x+y*y))

**Outras funções:** `math.atan2`, `math.fmod` (resto de ponto flutuante), `math.clamp` (limitação saturada de i64)

**Matemática de inteiros (funções genéricas int):** `math.even`, `math.odd`, `math.gcd`, `math.lcm`, `math.div` (quociente), `math.mod` (módulo)

**Conversão de tipo (as antigas funções `math.i64-to-f64` / `math.f64-to-i64` / `math.f32-to-f64` / `math.f64-to-f32` foram removidas, usar forma de método):** `v.to-f64()`、`v.to-i64()`、`f32.to-f64()` / `f64.to-f32()` (módulo number; todas as larguras de inteiro têm `.to-f64()`, e i128 / u128 também fornecem `.to-f64()`)

**Número para string de baixo nível:** resta apenas `math.char-to-str` (descontinuado, recomenda-se `c.to-str()`); `math.i64-to-str` / `math.u64-to-str` / `math.f64-to-str` foram removidos — usa a forma de método `v.to-str()`

**Operações de bits (funções):** `math.swap`, `math.arr-zero`, `math.rotate-left`, `math.rotate-right`, `math.store-le-u32`

> Potência inteira: `math.pow-int` (potenciação rápida genérica int, em módulo distinto da `math.pow` de f64); `number.pow` mantém uma assinatura i64 explícita para satisfazer asserções históricas.

### number — operações numéricas (API em forma de método)

As APIs numéricas do std em "forma de método" estão concentradas no módulo number (std/number.no); as APIs em forma de função ver a secção math acima. Os tipos union int / float / num estão nas respetivas secções.

**Constantes de intervalo:** i8.MIN / MAX … i128.MIN / MAX、u8.MIN / MAX … u128.MAX；`number.INF`

**Potência inteira (função):** `r = number.pow(base, n)` (base^n, n ≥ 0, potenciação rápida O(log n), assinatura i64, overflow com retorno circular; versão genérica int ver `math.pow-int` acima)

**Métodos de inteiro:** `n.sqrt()` (raiz quadrada inteira floor(√n), negativos retornam 0)、`n.is-prime()` (teste de primalidade) — disponíveis nas larguras i8 / i16 / i32 / i64 / u8 / u16 / u32 / u64, e também nas versões i128 / u128 (aritmética de 128 bits pela via LLVM i128, utilizável; exige variável local com tipo explícito como `n i128 = .` e disposição correta da anotação `#{overflow=wrap}` conforme o sinal)

**to-str de tipo concreto (método):** `v.to-str()` — larguras i8 / i16 / i32 / i64 / u8 / u16 / u32 / u64 / byte / f32 / f64; i128 / u128 também suportados (via `@str_from_i128` / `@str_from_u128` nativos)

**Métodos de ponto flutuante (f64 e os de mesmo nome em f32):** trigonométricos `x.sin()`, `x.cos()`, `x.tan()`, `x.asin()`, `x.acos()`, `x.atan()`；hiperbólicos `x.sinh()`, `x.cosh()`, `x.tanh()`；arredondamento `x.ceil()`, `x.floor()`, `x.round()`, `x.trunc()`；exponenciais/logarítmicos `x.exp()`, `x.log()`, `x.log10()`, `x.log2()`；potência/raiz `x.sqrt()`, `x.cbrt()`；graus/radianos `x.degrees()`, `x.radians()`. O f32 é calculado internamente com precisão double e depois estreitado para f32.

### char — operações com caracteres

O char é essencialmente um i32 (ponto de código Unicode) e todas as operações são fornecidas em forma de método:

```no
c char = 'A'
c.is-digit(); é um dígito (0-9) (método)
c.is-letter(); é uma letra (a-z, A-Z) (método)
c.is-alpha(); alcunha de is-letter (método)
c.is-alnum(); é letra ou dígito (método)
c.is-space(); é um carácter de espaço em branco (método)
c.is-upper(); é letra maiúscula (método)
c.is-lower(); é letra minúscula (método)
c.to-upper(); converter em maiúsculas (ASCII) (método)
c.to-lower(); converter em minúsculas (ASCII) (método)
c.to-bytes(); Unicode → bytes UTF-8 (método)
c.to-str(); Unicode → string (UTF-8, método)
```

### str — operações com strings

O descritor de `str` é `{ u32 len, u32 cap, i8* data }` (16 bytes em 64 bits), mas
`s.len()` / `s.len-bytes()` / `s.cap()` e os parâmetros e valores retornados de `with-cap` / `with-len` / `with-cap-len`
**continuam todos em `i64`**: a conversão u32 ↔ i64 é feita pelo compilador na fronteira do descritor.
Ver [Strings: layout interno e API pública](../lang/str.md). O `txt` não é afetado por esta alteração.

O descritor do slice `[]t` é isomorfo: `%vec = { u32 len, u32 cap, i64 data }` (16 bytes, em que `data` é
um ponteiro codificado com `ptrtoint`), e `v.len()` também continua em `i64`. O `[n]t` não tem descritor em tempo de execução (os elementos ficam inline
em `[n x t]` e `.len` é uma constante de compilação).

```no
#{fmt-skip}
ok = a.eq(b, n)               ; comparação de igualdade (método)
dst = s.copy()                ; cópia da string (método)
s.fill(val byte)              ; preencher com um valor byte (método)
pos = s.index(sub)            ; posição da substring
ok = s.contains(sub)          ; contém
ok = s.starts-with(sub)       ; verificação de prefixo
ok = s.ends-with(sub)         ; verificação de sufixo
s.to-upper()                  ; converter em maiúsculas
s.to-lower()                  ; converter em minúsculas
out = s.trim()                ; remover espaços no início e no fim
out = s.repeat(n)             ; repetir
out = s.slice(start, end)     ; slice (índices são posições de ponto de código)
b = s.to-bytes()              ; converter para []byte
s = b.to-str()                ; []byte para str (método)
v = s.to-i64()                ; string para i64 (retorna ?i64)
v = s.to-i8()                 ; string para i8 (retorna ?i8)
v = s.to-i16()                ; string para i16 (retorna ?i16)
v = s.to-i32()                ; string para i32 (retorna ?i32)
v = s.to-u8()                 ; string para u8 (retorna ?u8)
v = s.to-u16()                ; string para u16 (retorna ?u16)
v = s.to-u32()                ; string para u32 (retorna ?u32)
v = s.to-u64()                ; string para u64 (retorna ?u64)
v = s.to-byte()               ; string para byte (retorna ?byte)
v = s.to-f64()                ; string para f64 (retorna ?f64)
v = s.to-bool()               ; string "true"/"false" para bool (retorna ?bool)
s = v.to-str()                ; i64 para string (método)
out = s.reverse()             ; inverter
c = s.compare(b)              ; comparação lexicográfica
n = s.count()                 ; total de pontos de código
val = s.replace-char(old, new) ; substituir carácter (retorna a string resultante)
out = s.trim-char(c)          ; remover um carácter especificado
ok = s.empty()                ; está vazia
s.clear()                     ; limpar (len=0, modificação in-place)
s = with-cap(cap)            ; sintaxe incorporada：cria nova string com a capacidade dada (len=0)
s = with-len(len)            ; sintaxe incorporada：cria string com o comprimento dado (len=cap)
s = with-cap-len(cap, len)   ; sintaxe incorporada：cria string com capacidade e comprimento dados
parts = s.split(sep)          ; dividir pelo separador (retorna []str, método)
out = ss.join(sep)            ; juntar []str com o separador (método)
```

### num — tipo union num e métodos num

`num = int | float`（std/num.no）。Os métodos do tipo num ficam no módulo num (funções genéricas max/min ver math acima):

```no
r = num.abs(); valor absoluto (método)
r = num.clamp(lo, hi); limitar ao intervalo (método)
r = num.sign(); sinal (-1/0/1, método)
```

### int — tipo union int e métodos int

`int = i8 | i16 | i32 | i64 | i128 | u8 | u16 | u32 | u64 | u128`（std/int.no）。Os métodos do tipo int ficam no módulo int:

```no
s = v.to-str(); inteiro para string (método)
q = v.div(b); divisão com quociente (retorna option)
r = v.mod(b); módulo (retorna option)
```

### float — tipo union float e métodos float

`float = f32 | f64`（std/float.no）。Os métodos do tipo float ficam no módulo float:

```no
q = v.div(b); divisão de ponto flutuante
yes = v.is-nan(); verificação de NaN (método)
yes = v.is-inf(); verificação de Inf (método)
```

(Não é definido o método union float.to-str; variáveis f64/f32 usam diretamente o método do tipo concreto `v.to-str()`.)

### number — exemplo de uso de métodos

(Para a API completa ver「number — operações numéricas (API em forma de método)」acima; para a forma de função ver a secção「math」.)

```no
; Forma de método (módulo number)
s = v.to-str(); inteiro/ponto flutuante para string (várias larguras)
r = n.sqrt(); raiz quadrada inteira floor(√n)
yes = n.is-prime(); teste de primalidade (int/u8…)
q = x.sqrt(); raiz quadrada de ponto flutuante (método f64/f32)
q = x.floor(); arredondamento (método f64/f32)
r = number.pow(a, n); potência inteira (assinatura i64; versão genérica int é math.pow-int)

; Forma de função (módulo math)
m = math.max(1, 2, 3); variádico genérico num
g = math.gcd(a, b); máximo divisor comum
l = math.lcm(a, b); mínimo múltiplo comum
pi = math.pow-int(a, n); potência inteira (genérico int)
sw = math.swap(a, b); trocar

; Conversão numérica (forma de método, antigas funções math.*-to-* removidas)
f = v.to-f64(); inteiro → f64
n = x.to-i64(); f64 → inteiro (truncamento)
w = f32v.to-f64(); f32 → f64；a inversa é f64.to-f32()

; Constantes de intervalo (módulo number)
i8.MIN / MAX; -128 / 127
i16.MIN / MAX; -32768 / 32767
i32.MIN / MAX; -2147483648 / 2147483647
i64.MIN / MAX; -2^63 / 2^63-1
i128.MIN / MAX; -2^127 / 2^127-1
u8.MIN / MAX; 0 / 255
u16.MIN / MAX; 0 / 65535
u32.MIN / MAX; 0 / 4294967295
u64.MIN / MAX; 0 / 2^64-1
u128.MIN / MAX; 0 / 2^128-1
number.INF; infinito de ponto flutuante
```

### byte — operações com bytes

```no
#{fmt-skip}
out = i64.to-bytes-be()         ; i64 → [8]byte big-endian
out = i64.to-bytes-le()         ; i64 → [8]byte little-endian
v = []byte.to-i64-be()          ; []byte big-endian → i64 (1~8 bytes)
v = []byte.to-i64-le()          ; []byte little-endian → i64 (1~8 bytes)
s = []byte.to-str()             ; []byte para str (método)
s = []byte.to-hex()             ; []byte → string hexadecimal maiúscula
s = []byte.to-hex-lower()       ; []byte → string hexadecimal minúscula
s = byte.to-str()               ; byte para str (método)
```

### txt — tipo de texto de comprimento fixo

`txt` é um tipo de texto fixo de 256 bytes, adequado para texto curto:
- Os primeiros 255 bytes guardam os dados (`data [255]byte`)
- O último byte guarda o comprimento (`len byte`, em "bytes", intervalo 0-255)
- Não precisa de alocação na heap, tudo fica na stack
- Exige anotação de tipo

Semântica de comprimento (alinhada com `str`):
- `t.len()` → número de caracteres (pontos de código) (equivalente a `t.count()`)
- `t.len-bytes()` → número de bytes (comprimento real do buffer UTF-8 subjacente)
- Operações de byte como indexação / slice / acrescentar usam `len-bytes()` como referência; em ASCII ambos são iguais, com UTF-8 multibyte `len() < len-bytes()`

```no
#{fmt-skip}
t txt = 'hello'              ; exige anotação de tipo
n = t.len()                  ; número de caracteres (pontos de código) (i64)
b = t.len-bytes()            ; número de bytes (i64)
c = t[0]                     ; acesso por índice (byte)
ok = t.eq(b txt)             ; comparação de igualdade
dst = t.copy()               ; cópia
s = t.to-str()              ; converter para str
out = t.to-bytes()           ; converter para []byte
pos = t.index(sub txt)       ; pesquisar substring
ok = t.contains(sub txt)     ; contém
ok = t.starts-with(sub txt)  ; verificação de prefixo
ok = t.ends-with(sub txt)    ; verificação de sufixo
t.push(c char)              ; acrescenta um ponto de código (se o carácter inteiro não couber no limite 255, ignora tudo)
t.push-str(s str)            ; acrescenta str (truncado a 255 por bytes)
t.push-txt(t2 txt)           ; acrescenta txt (truncado a 255 por bytes)
out = t.reverse()            ; inverter
out = t.slice(start, end)   ; extrair [start, end)
r = t.compare(b txt)        ; comparação lexicográfica (-1/0/1)
```

### vec — operações com slices

```no
#{fmt-skip}
v = vec.vec-create(n, val)         ; cria slice de comprimento n, preenchido com val
ok = []t.eq(a, b, n)           ; comparação de igualdade
n = []t.len()                  ; comprimento
[]t.push(val)                   ; acrescentar (expansão automática)
[]t.clear()                     ; limpar (len=0, cap/data inalterados)
v = with-cap(cap)             ; sintaxe incorporada：cria novo slice com a capacidade dada (len=0)
v = with-len(len)             ; sintaxe incorporada：cria slice com o comprimento dado (len=cap)
v = with-cap-len(cap, len)    ; sintaxe incorporada：cria slice com capacidade e comprimento dados
val, new-n = []t.pop()         ; retirar
found = []t.contains(n, val)   ; contém (n é o comprimento)
[]t.reverse(n)                  ; inverter os primeiros n elementos
[]t.clone(dst)                  ; copiar para dst
[]t.fill(n, val)                ; preencher os primeiros n elementos
arr = []t.to-arr()             ; converter em array
[]t.sort-asc()                  ; ordenar por ordem ascendente (método)
[]t.sort-desc()                 ; ordenar por ordem descendente (método)
```

### arr — operações com arrays

```no
out = [n]
t.clone(); copiar
ok = [n]
t.eq(b); comparação de igualdade
[n]
t.fill(val); preencher
[n]
t.reverse(); inverter
ok = [n]
t.contains(val); contém
v = [n]
t.to-vec(); converter em slice
v = [n]
t.max(); valor máximo
v = [n]
t.min(); valor mínimo
v = [n]
t.sum(); soma
i = [n]
t.index-of(val); índice
v = [n]
t.last(); último elemento
v = [n]
t.first(); primeiro elemento
[n]
t.sort-asc(); ordenar por ordem ascendente
[n]
t.sort-desc(); ordenar por ordem descendente
```

### sort — constantes de ordenação

```no
sort.ast; ascendente
sort.desc; descendente
```

---
