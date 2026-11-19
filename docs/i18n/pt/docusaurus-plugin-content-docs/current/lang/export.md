---
sidebar_position: 5
---

# Exportação

O Nolang usa a palavra-chave `@` no ficheiro `lib.no` da raiz do pacote para declarar os itens exportados. Os pacotes externos só podem aceder a estes símbolos exportados ao importar via `#`.

> **Nota**: `@` apenas exporta símbolos entre pacotes Nolang e não produz símbolos ABI C. Para permitir que C / C++ / Rust / Go / Java / Python / C# chamem o Nolang, usa antes a anotação `#{cabi='symbol'}` com `no build --shared`. Consulta [Chamar o Nolang a partir de linguagens de hospedagem](./host-interop).

## Sintaxe

```no
@ path.func
[alias]
```

- `path` — caminho do módulo (relativo à raiz do pacote, começa com `/`, sem a extensão `.no`)
- `func` — nome da função/constante/enum a exportar
- `alias` — alcunha opcional usada na importação externa

## Regras

- As instruções de exportação só podem **ser escritas** no ficheiro `lib.no` da raiz do pacote (a única exceção é a secção seguinte, "Alcunhas chamáveis globais")
- Um item de exportação por linha
- Os itens exportados só podem ser símbolos finais como funções, constantes, enums
- **É obrigatório indicar o nome do símbolo**: `@ std/txt` (apenas o caminho, sem `func` nem `alias`) é um erro (`lgxbfk9a`).
  Essa grafia não declara nada — todos os consumidores partem do princípio de que "há um nome de símbolo" — pelo que deixa de ser ignorada silenciosamente.
  Também não serve para "associar um tipo a um módulo": os tipos não têm essa camada de nome global, e a alcunha do `@` só tem significado para símbolos chamáveis
- Os structs, enums e outros tipos referenciados pelas funções exportadas são **exportados automaticamente**, sem necessidade de declaração manual
- Se uma função exportada não existir no módulo, o LSP apresenta um erro

## Exemplo

```no
; lib.no - ficheiro de exportação da raiz do pacote
@ /src/utils.greet a
@ /src/utils.hello b
@ /src/math.pi
```

```no
; src/utils.no
; Define as funções exportadas
greet = (name str) {
    print('Hello, ' - name)
}

hello = () {
    print('Hi')
}
```

## Alcunhas chamáveis globais

A mesma sintaxe, escrita em **`std/global.no`** (em vez do `lib.no` de um pacote), tem outro significado:
não filtra exportações, mas declara um **segundo nome global chamável** — o nome nu `<alias>` é simplesmente
o `func` do módulo `path`, com exatamente o mesmo efeito de escrever `<path>.<func>(...)` por completo.

```no
; std/global.no — duas grafias, mesmo significado
@ std/err.new err; estilo exportação
err = err.new; estilo atribuição
```

```no
e = err('disk full', code.io); equivale a e = err.new('disk full', code.io)
m = e.msg(); 'disk full'
```

Regras:

- Só `std/global.no` tem este significado; o `@` num `lib.no` continua a ser um filtro de exportação
- As duas grafias são **equivalentes**, o compilador normaliza-as para a mesma declaração; escrever ambas não cria conflito
- A grafia de atribuição **só é válida em `global.no`**. Num programa comum, `x = mod.fn` é uma
  variável que contém um valor de função (utilizável como chamada indireta), não sendo tratada como alcunha global
- O lado direito da atribuição deve ser uma referência nua `<modulo>.<funcao>`: não uma chamada, não um
  literal, não uma leitura de campo, nem uma ligação com tipo. Quando o alvo não é uma função, a declaração
  é automaticamente ignorada (`PI = math.PI` e outras declarações de constante não são afetadas)
- A alcunha só tem efeito em **chamadas por nome nu**. Chamadas qualificadas não são afetadas: `io.err(...)` continua a ser
  o escritor de stderr do módulo `io`, não sendo capturado pela alcunha `err`
- Chamadas de um módulo à sua própria função com o mesmo nome também não são afetadas (exceção de automódulo)
- A **lista de parâmetros** da alcunha provém da função alvo; o **tipo de retorno** é determinado no local da chamada pela função alvo
- A alcunha não entra em conflito com a variante `err` da opção incorporada: em posições em que se espera um tipo `?T`
  (`x = err('m')` depois de `x ?i64`, um braço de match `err ->`, funções que retornam `?T`),
  `err(...)` continua a ser o construtor da variante err da opção (tag 2)

## Importar símbolos exportados

Os pacotes externos só podem aceder aos itens exportados declarados em `lib.no` ao importar via `#`:

```no
; Importa a alcunha a (corresponde a package-name.utils.greet)
# package-name.utils.greet a
; Ou utiliza diretamente o nome da função
# package-name.utils.greet
```

## Suporte LSP

- **Ir para a definição**: clicar no nome de uma função exportada ou na alcunha em `lib.no` salta para a definição no ficheiro de módulo correspondente
- **Preenchimento automático**: sugere automaticamente caminhos de ficheiro e nomes de função ao escrever `@` e um caminho
- **Diagnóstico de erros**: mostra um erro quando uma função exportada não existe no módulo
