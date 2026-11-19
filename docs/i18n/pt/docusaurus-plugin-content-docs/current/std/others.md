---
sidebar_position: 4.4
---

## Outros

### unicode — suporte Unicode

A funcionalidade relacionada com Unicode foi distribuída pelos módulos `char` e `str`:

- Classificação de caracteres (`is-letter`, `is-digit`, `is-upper`, etc.) → ver módulo `char`
- Codificação/descodificação UTF-8 (`char.to-bytes`, `char.to-str`) → ver módulo `char`
- Contagem de runes de string (`str.count`) → ver módulo `str`

### uuid — geração e análise de UUID v4

```no
out = uuid.new-v4(state); gera UUID v4
out-n = uuid.to-str(out); converte em string minúscula (método)
out-n = uuid.to-str-upper(out); converte em string maiúscula (método)
ok = uuid.from-str(s, sn, out); analisa a partir de string (com/sem hífens)
ok = uuid.parse-with-dashes(s, pos, out); análise com hífens
ok = uuid.parse-no-dashes(s, pos, out); análise sem hífens
ok = uuid.validate(); valida o formato UUID (método)
v = uuid.version(); obtém a versão (método)
v = uuid.variant(); obtém a variante (método)
yes = uuid.is-nil(); é nil (método)
yes = uuid.eq(b); comparação de igualdade (método)
r = uuid.cmp(b); comparação (método)
uuid.nil-uuid(out); retorna o UUID nil
```

### bigint — inteiro de precisão arbitrária

```no
; Tipo
bigint {
    sign i64
    limbs []i64
    len i64
}

; Construção
out = bigint.from-i64(v)
out = bigint.from-u64(v)
out = bigint.zero()
out = bigint.one()
out = bigint.copy(a)

; Comparação
r = bigint.cmp(a, b)
r = bigint.eq(a, b)
r = bigint.is-zero(a)
r = bigint.is-neg(a)
r = bigint.is-pos(a)

; Operações (+ - * / % estão sobrecarregados para bigint, ver「Sintaxe > Sobrecarga de operadores」)
c = a + b; equivalente a a._add(b)
c = a - b; equivalente a a._sub(b)
c = a * b; equivalente a a._mul(b)
q = a / b; equivalente a a._div(b), divisão por zero retorna zero segundo div-mod
r = a % b; equivalente a a._mod(b)

; Operandos mistos：os operadores sobrecarregados recebem uma união `bigint | i64`, o compilador
; monomorfiza e escolhe o braço em tempo de compilação pelo tipo do operando direito,
; pelo que `a + 1` / `a * 256` funcionam nativamente (sem tag em tempo de execução). Só se suporta "operando esquerdo é bigint"; `1 + a` não é suportado.
c = a + 1; bigint ⊕ i64
c = a * 256; bigint ⊕ i64
q, r = a.div-mod(b); obtém quociente e resto em simultâneo
c = a.pow(n)
r = base.mod-pow(exp, mod)

; Teoria dos números
g = a.gcd(b)
l = a.lcm(b)

; Deslocações
bigint.shl(a, n, c)
bigint.shr(a, n, c)

; Conversão para string
n = bigint.to-str(a, out)
out = bigint.from-str(s, sn)
n = bigint.to-hex(a, out)
out = bigint.from-hex(s, sn)
; Os atalhos rápidos para inteiros pequenos (_add-i64 / _mul-i64 / _div-i64 / _mod-i64) são privados
; do módulo bigint；para operandos mistos usa sempre os operadores sobrecarregados + - * / % acima
```

### err — tratamento de erros

Tipo de erro estruturado e funções utilitárias:

```no
; Enum de códigos de erro
code {
    ok,
    not-found,
    permission,
    io,
    timeout,
    parse,
    invalid,
    overflow,
    unknown,
}

; Estrutura
err {
    code code
    msg str
    file str
    line i64
    col i64
}

; Funções
e = err.new(msg, code.io); cria um erro
e = err.from-errno(errno); cria a partir de um errno C
yes = e.is(code.io); verifica o código de erro
msg = e.msg(); obtém a mensagem de erro
c = e.code(); obtém o código de erro
s = e.format(); formata como string
```

Esta struct `err` é a carga da variante `err` da opção incorporada (`option { ok(v t), nil, err(e err.err) }`):
ao fazer match de `?t`, o `it` vinculado implicitamente no braço `err ->` é ela, e não uma `str`.

```no
x = f(); f retorna ?i64
x: {
    ok -> n = it

    nil -> n = 0

    err -> {
        log.error(it.msg()); mensagem simples
        print(it.format()); 'code.unknown: ... at file:line:col'
        it.is(code.io) -> print('io error')
    }
}
```

### bool — tipo booleano

```no
#{fmt-skip}
bool.to-str() (out str)     ; true→"true", false→"false"（método）
```

### enter / leave — hooks de ciclo de vida

```no
; Executado ao arrancar
enter {
    enter()
}

; Executado ao sair
leave {
    leave()
}
```

### magic — deteção de tipo de ficheiro

Determina o tipo de ficheiro com base na extensão e nos magic bytes, sem depender de libmagic:

```no
kind = magic.detect-type(path); deteta o tipo de ficheiro
; Retorna uma string de descrição do tipo, como 'PNG image'、'ELF executable'、'ASCII text'、'directory'、'unknown'、'data'
```

---
