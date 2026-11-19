---
sidebar_position: 4
---

# Operadores

## Símbolos especiais

- `#` ; importar módulo
- `@` ; exportar módulo
- `->` ; braço de match e ramo if/else (por exemplo, `cond -> body`)
- `:` ; expressão match (por exemplo, `x: { ... }`)
- `{ } (true)` ; loop infinito (forma sufixo, grafia antiga); a forma prefixo `!! { }` / `true { }` é equivalente e é a nova predefinição (emitida por `no fmt`)
- `<-` ; iteração por intervalo (por exemplo, `i <- [a..b]: { }`)
- `..` ; operador de classe pai / intervalo (por exemplo, `[a..b)`)
- `.` ; próprio tipo (⚠️ nos limites de intervalo, usa `self.method` em vez de `.method` para evitar a ambiguidade visual de três pontos consecutivos)
- `?` ; prefixo do tipo option (por exemplo, `?i64`, `?str`)
- `(cond) { }` ; loop condicional (forma prefixo, a nova predefinição; equivalente à forma sufixo `{ } (cond)`)
- `N * { }` ; loop contado (forma prefixo, a nova predefinição; equivalente à forma sufixo `{ } * N`; N ≤ 0 ignora)
- `!` ; falso (false); também o prefixo de loop "nunca executar" (`! { }` nunca corre)
- `!!` ; verdadeiro (true); também o prefixo de loop "sempre executar" (`!! { }` corre infinitamente)
- `{ } ()` ; não executado (parênteses vazios significam false; forma sufixo, grafia antiga); a forma prefixo `! { }` / `false { }` / `() { }` é equivalente e é a nova predefinição
- `*` ; ignorar iteração atual — substitui continue (aceite pelo parser, mas ainda não recomendado — usa a palavra-chave)
- `**` ; interromper — substitui break (aceite pelo parser, mas ainda não recomendado — usa a palavra-chave)
- `***` ; instrução de retorno, termina a função — substitui return (planeado, ainda não recomendado — usa a palavra-chave)
- `awy { }` ; grupo de corrotinas; espera por todas as tarefas `co` no fim do grupo
- `co` ; spawn async sem cor, apenas válido dentro de `awy { }`

## Operadores aritméticos

- `+` ; adição
- `-` ; subtração (também usada para concatenação de strings)
- `*` ; multiplicação (também usada para repetição de strings)
- `/` ; divisão

## Operadores de comparação

- `==` ; igual a (igualdade real de strings em `str`, múltiplos caracteres permitidos)
- `!=` ; diferente de (igual ao acima)
- <code>&lt;</code> ; menor que
- `>` ; maior que
- <code>&lt;=</code> ; menor ou igual a
- `>=` ; maior ou igual a

:::warning Os operadores de ordem (<code>&lt;</code> `>` <code>&lt;=</code> `>=`) não têm semântica de string

`str` só implementa igualdade (`==` / `!=`). Ordenar uma string antes **devolvia false silenciosamente**; agora é um erro de compilação.

- Permitido: um literal de string de **um caractere** `'a'`, implicitamente um `char` (o seu ponto de código), comparável com outro literal de um caractere ou com um `char` (`"a"`). Os tipos numéricos não são afetados.
- Rejeitado: strings de múltiplos caracteres — o literal `'ab'`, uma variável `str` ou uma chamada que retorna `str`.
- Para ordem lexicográfica usa `str.compare(b)` (retorna -1 / 0 / 1).

```nolang
'a' <= 'z'; true  — string de um caractere é implicitamente char
ch >= 'a'; ch é char, OK
'a' > "b"; string de um caractere vs char, OK
'abc' < 'abd'; erro de compilação: string de múltiplos caracteres
s1 < s2; erro de compilação: variáveis str
s1.compare(s2) > 0; forma correta
```
:::

## Operadores lógicos

- `&&` ; E lógico
- `||` ; OU lógico (também usado para combinação de ramos de match, por exemplo `nil || err -> body`)
- `!` ; NÃO lógico

## Operadores bitwise

- `&` ; E bitwise
- `|` ; OU bitwise
- `^` ; XOR bitwise
- `~` ; complemento bitwise
- <code>&lt;&lt;</code> ; deslocação para a esquerda
- `>>` ; deslocação para a direita

## Operadores de atribuição

- `=` ; atribuição
- `+=` ; atribuição com adição
- `-=` ; atribuição com subtração
- `*=` ; atribuição com multiplicação
- `/=` ; atribuição com divisão
- `%=` ; atribuição com módulo
- `&=` ; atribuição com E bitwise
- `|=` ; atribuição com OU bitwise
- `^=` ; atribuição com XOR bitwise
- <code>&lt;&lt;=</code> ; atribuição com deslocação para a esquerda
- `>>=` ; atribuição com deslocação para a direita

## Outros

- `?` ; operador ternário (por exemplo `c = flag ? 1 : 2`)
- `as` ; conversão de tipo de ponteiro FFI (por exemplo `y = x as *byte`)
- `..` ; intervalo de slice (por exemplo `arr[1..3]`, `arr[1..]`, `arr[..3]`)
