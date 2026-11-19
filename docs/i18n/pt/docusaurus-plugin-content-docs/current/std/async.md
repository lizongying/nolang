---
sidebar_position: 4.2
---

## Async (assíncrono)

O Nolang tem apenas uma sintaxe de superfície para concorrência: o grupo de corrotinas `awy { ... }`, dentro do qual se derivam tarefas com `co`.
A palavra-chave `run` foi removida (agora é um identificador comum); `awy <handle>` e os grupos de corrotinas de bloco nu também foram descontinuados;
**`co` fora de um grupo é erro de compilação** (`E_CO_OUTSIDE_GROUP`).

`co` apenas inicia um **procedimento** — o chamado não pode declarar um valor de retorno (o `self` implícito dos métodos não conta),
pelo que os resultados passam sempre por um canal: o procedimento envia o valor para o canal e chama `close()`, e o chamador esvazia o canal fora do grupo.

### Grupos de corrotinas

```no
; Procedimento：só escreve no canal, sem retorno declarado
produce = (ch chan[i64], v i64) {
    ch <- v
    ch.close()
}

ch1 chan[i64] = with-cap(1)
ch2 chan[i64] = with-cap(1)

; Concorrência：duas tarefas correm juntas, o grupo espera por todas no fim
awy {
    co produce(ch1, 1)
    co produce(ch2, 2)
}

r1 = 0
<- ch1: { ok -> r1 = it }
r2 = 0
<- ch2: { ok -> r2 = it }

; Ordem：o spawn seguinte usa o resultado do anterior como argumento, usando a "leitura do canal" como barreira
awy {
    co produce(ch1, r1)
}
```

O que se obtém em `ch1` / `ch2` são valores de resultado, não handles de tarefa. Para mais detalhes, ver `lang/syntax.md`「Programação assíncrona」.

### Async sem cor: `co`

`co` é uma sintaxe sem cor (colorless): escreves o nome não corado `produce` e o compilador gera automaticamente a variante monomórfica
`produce-async`, executando-a por baixo como corrotina sem pilha com cor. Não escrevas manualmente o sufixo `-async` no código da aplicação.

Duas formas:

- `co f(args)` — spawn de procedimento; `f` não pode declarar retorno (exceto o `self` implícito dos métodos).
- `co { ... }` — spawn de closure; as variáveis livres do bloco são capturadas por valor (um instantâneo no momento do spawn).

`co` não é um valor e deve constituir uma instrução independente: `r = co f()` e `print(co f())` dão `c0nostmt`.

Regras:

- **Monomorfização**: `co produce(ch, 1)` gera automaticamente `produce-async` (uma cópia renomeada do corpo de `produce`). Se `produce` também for chamado de forma síncrona, ambas as variantes coexistem.
- **Transitividade**: se um procedimento contiver ele próprio `awy { co child(n) }`, esse procedimento também é monomorfizado para `-async`.
- **Lowering subjacente** (não é sintaxe de utilizador): cada `co` torna-se `OpRun` e, no fim do grupo, insere-se `OpAwait`.

### Cancelamento

`cancelled()` pode ser usado dentro de uma corrotina como auto-verificação cooperativa. `cancel(h)` é uma operação de handle de baixo nível;
a nova sintaxe não expõe handles de tarefa e o código da aplicação não a usa.

> **Atenção:** o cancelamento é cooperativo, não preemptivo. Chamadas longas e bloqueantes (como pedidos de rede) não podem ser interrompidas à força;
> a tarefa só para realmente no próximo ponto de verificação cooperativo (chamada a `cancelled()` ou próxima agendação do event loop).

#### Fazer cancelamento com um canal próprio (recomendado)

A nova sintaxe não tem `ch.cancel()` e os canais não têm variante `cancel`. Quando precisas de "sinalizar externamente ao worker que deve terminar",
**mantém um canal próprio e usa `close()` como sinal de cancelamento** — esta é a única semântica de cancelamento de `chan[T]`.
No estado triplo do loop de receção, `nil` significa "fechado e esvaziado", sendo exatamente o ramo de cancelamento; `err` apenas indica handle não inicializado
e não deve ser usado para cancelamento.

```no
; Canal de controlo：fechar é cancelar
stop chan[i64] = with-cap(0)

worker = (stop chan[i64]) {
    <- stop: {
        ok  -> { }              ; recebeu mensagem (não usada neste exemplo)
        nil -> print('cancel')  ; chega aqui após stop.close()
        err -> { }              ; handle não inicializado
    }
}

stop.close()
awy {
    co worker(stop)
}
; Saída：cancel
```

Para acionar o cancelamento a partir de outra tarefa durante a execução, coloca também o fecho dentro do grupo (não se pode fazer close depois do `awy` —
o grupo espera por todas no fim e o worker, se estiver parado num canal vazio, entra em deadlock). Atenção que `<- ch: { ... }` é um **loop de receção**:
depois de receber um valor continua à espera do seguinte, pelo que o notificador tem de fazer `close()` para que o canceller avance no loop e chegue a
`stop.close()`:

```no
stop chan[i64] = with-cap(0)
awake chan[i64] = with-cap(1)

worker = (stop chan[i64]) {
    <- stop: { ok -> { }  nil -> print('cancel')  err -> { } }
}

canceller = (awake chan[i64], stop chan[i64]) {
    <- awake: { ok -> { }  nil -> { }  err -> { } }   ; espera notificação; depois ainda espera o close para sair do loop
    stop.close()                                       ; esta instrução é o sinal de cancelamento
}

notify = (awake chan[i64]) {
    awake <- 1
    awake.close()                                      ; faz o loop de receção do canceller chegar a nil
}

awy {
    co worker(stop)
    co canceller(awake, stop)
    co notify(awake)
}
; Saída：cancel
```

Se queres apenas verificar no **início de cada passo do próprio worker** se foi pedida a paragem, usa `cancelled()`, mais direto;
mas ele só faz auto-verificação e não há handle externo para notificar — para o cenário "alguém dá o sinal", segue a convenção de canal acima.

---
