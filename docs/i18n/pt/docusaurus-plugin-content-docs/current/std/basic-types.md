---
sidebar_position: 3.1
---

## Tipos básicos

### types — definições de tipos

Mapeamento dos tipos do Nolang para LLVM:

| Nolang           | LLVM                                               |
| ---------------- | -------------------------------------------------- |
| `bool`           | `i1`                                               |
| `byte`           | `i8`                                               |
| `char`           | `i32`                                              |
| `i8/i16/i32/i64/i128` | `i8/i16/i32/i64/i128`                             |
| `u8/u16/u32/u64/u128` | `i8/i16/i32/i64/i128`                          |
| `f32`            | `float`                                            |
| `f64`            | `double`                                           |
| `str`            | `%str-long = { i32, i32, i8* }`（u32 len, u32 cap, data；16 bytes em 64 bits, alocado na heap） |
| `[]t`            | `%vec = { i32, i32, i64 }`（u32 len, u32 cap, ptrtoint data；16 bytes em 64 bits, alocado na heap） |
| `[n]t`           | `[n x t]`（agregado simples, elementos inline；**sem descritor len / cap**） |
| `txt`            | `{ [255 x i8], i8 }`（256 bytes fixos） |

> **Tanto `str` como `[]t` guardam len / cap como `u32` internamente, mas expõem-nos sempre como `i64`.** Na fronteira do descritor, as leituras são alargadas (`zext i32 -> i64`) e as escritas estreitadas (`trunc i64 -> i32`); as assinaturas de `s.len-bytes()`, `s.len()`, `s.cap()` e dos privados `_len` / `_cap` / `_set-len` /
> `_set-cap` mantêm-se todas em `i64`, tal como `v.len()`. Ver [Strings: layout interno e API pública](../lang/str.md)
> e [Modelo de memória: layout de tipos](../lang/memory.md#layout-de-tipos).
> O campo `data` de `%vec` é mantido deliberadamente como `i64` (codificado com `ptrtoint`), ao contrário do `i8*` de `%str-long`, pelo que os dois
> **descritores já não partilham o mesmo layout** e não podem ser tratados com `getelementptr` como se fossem o mesmo descritor.
> O `txt` não tem campo `cap`, mantém o comprimento num único `i8` (0-255) e **não adota u32**.

**Tipos compostos:**

- **Array de comprimento variável `[]t`**: subjacente `%vec = { u32 len, u32 cap, i64 data }` (16 bytes; `cap == 0` assinala uma vista emprestada, sem propriedade do buffer)
- **Array de comprimento fixo `[n]t`**: array de tamanho fixo LLVM `[n x t]`, elementos inline, sem descritor em tempo de execução; `.len` é a constante de compilação `n` e não existe `cap`
- **String `str`**: sequência de bytes alocada na heap com descritor `{ u32 len, u32 cap, i8* data }` (16 bytes em 64 bits); suporta `s[i]`, `s[i..j]`, `s + t`. Comprimento/capacidade são apresentados como `i64` (`s.len-bytes()` / `s.cap()`) e guardados internamente como `u32`
- **String fixa `txt`**: estrutura fixa de 256 bytes `{ [255]byte data, byte len }`, 255 bytes de dados e 1 byte com o comprimento em **bytes** (0-255), sem alocação na heap, exigindo anotação de tipo (`t txt = 'abc'`); `t.len()` retorna o número de pontos de código (alinhado com `str`, equivalente a `t.count()`), `t.len-bytes()` retorna o número de bytes; a indexação `t[i]` **lê** por **ponto de código** (retornando o `char` desse ponto de código, com `err`, `code.index-out`, fora dos limites); **a escrita `t[i] = c` está globalmente desativada desde 2026-11-12** (erro de compilação `idxwrite`) — recodifica `c` em UTF-8 e desloca os bytes seguintes, pelo que o comprimento muda como efeito colateral (acrescenta quando `i` é igual ao número de pontos de código e descarta silenciosamente o que excede 255); para crescer usa `t.push(c char)` / `t.push-str(s)` / `t.push-txt(u)`, e para alterar em lugar usa place-match `t[i]: { ok -> it = c }`; para leitura bruta de bytes usa `t.byte(i)` (retorna `?byte`, nil fora dos limites); para montar um `txt` byte a byte, junta um `[]byte` e copia-o de uma vez com `txt.from-bytes(b)` (o que excede 255 bytes é truncado e o comprimento é definido pelo número real de bytes). O escritor bruto de bytes `txt._set-byte` é **privado do std** desde 2026-11-11 e inacessível ao código do utilizador. O `s[i] = v` de `str` foi **desativado globalmente** a 2026-11-11 (erro de compilação `idxwrite`, sem isenção para bases `with-cap`), restando do lado de `str` apenas `s.fill(val byte)`, o place-match `s[i]: { ok -> it = v }` e o par de crescimento `s.push(c)` / `s.push-str(s)`
- **Enum/Union**: enum tagged `option` (`ok t` / `nil` / `err str`)
- **Struct**: tem de ser definido em várias linhas; os campos não levam vírgulas
- **Map**: subjacente linked-hash-map
- **Iterador**: `for iter.next() {}` (método de interface `next() (ok bool)`)

### option — tipo opção

Enum tagged `option<t>` (tag=0=val, 1=nil, 2=err):

```no
#{fmt-skip}
x ?t                ; declara option<t>
x = 42              ; define com valor
x = nil             ; define como vazio
x = err('msg')      ; define como erro

; match
x: {
    val -> f(it)
    nil ->
    err -> g(it.msg())
}  
```

**Guia de estilo:** quando uma função pode falhar ou retornar um valor vazio, usa a opção `?t` em vez de `(val, ok bool)`. `?t` tem três estados: `ok` (com valor), `nil` (vazio/ausência normal) e `err` (erro). O valor normal é vinculado implicitamente. Por exemplo, `pop()` retorna `?i64` (`nil` = vazio), `read-line()` retorna `?str` (`nil` = EOF, `err` = erro) e `lookup()` retorna `?str` (`nil` = não encontrado). Consulta a documentação da sintaxe para mais detalhes.

---
