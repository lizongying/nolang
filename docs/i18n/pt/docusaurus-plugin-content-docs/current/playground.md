---
sidebar_position: 5
---

# Playground

O Nolang Playground é um ambiente online onde podes editar, compilar e executar programas Nolang diretamente no navegador. Ele compila o compilador Nolang (`no.wasm`) e o servidor de linguagem (`lsp.wasm`) para WebAssembly e executa o código do utilizador dentro da sandbox do navegador via WASI — sem necessidade de instalar qualquer ferramenta localmente.

Vai já a: [**/playground**](/playground)

## Utilização

A página do Playground está dividida em dois painéis:

- **Editor esquerdo**: um editor de código baseado no [CodeMirror 6](https://codemirror.net/), com destaque de sintaxe Nolang, indentação automática e histórico.
- **Output / stderr / Diagnostics direitos**: resultados da execução, saída de erro padrão e mensagens de diagnóstico.

A interface acompanha automaticamente o idioma do Docusaurus (chinês simplificado / inglês), e os painéis de saída suportam tanto o tema claro como o escuro.

### Barra de ferramentas

| Botão        | Função                                                            |
| ------------ | ----------------------------------------------------------------- |
| **▶ Run**    | Compila o código atual para WASM e executa-o via WASI; o stdout aparece no painel Output à direita. |
| **Format**   | Chama `no fmt` através do LSP para formatar o código atual.        |
| **Examples** | Carrega um exemplo integrado agrupado por categoria, substituindo o conteúdo do editor. |

O lado direito da barra de ferramentas mostra o estado atual (ocioso / a compilar / a executar / concluído / erro); por baixo do menu suspenso aparece uma descrição bilingue do exemplo selecionado. O último exemplo selecionado fica guardado no armazenamento local do navegador.

### Exemplos

O menu suspenso Examples inclui **24 exemplos** agrupados por tópico, todos validados em CI através exatamente do mesmo percurso de compilação `--wasm-direct` que o navegador utiliza:

| Categoria | Quantidade | Conteúdo |
| --------- | ---------- | -------- |
| Básicos | 7 | Hello World, inferência, funções e recursão, Fibonacci, aritmética e bitwise, formatação, strings |
| Fluxo de controlo | 6 | Guardas de curto-circuito, match, FizzBuzz, quatro formas de loop de intervalo, while condicional, break / continue |
| Estruturas de dados | 4 | Arrays de tamanho fixo, slices, structs, métodos |
| Tipos escalares | 4 | char, bool, f64, byte |
| Biblioteca padrão | 2 | math.max / math.min / sqrt, rand.rand |
| Tratamento de erros | 1 | option e nil com match de três braços |

Cada exemplo inclui uma subsecção estável de saída esperada; a CI compila cada exemplo, executa-o com `wasmtime` e verifica que essa subsecção surge no stdout. Assim, os exemplos não ficam silenciosamente desatualizados quando a linguagem ou o backend Direct-WASM evolui.

Ao selecionar um exemplo, o Playground limpa o Output, o stderr e os Diagnostics atuais e substitui o editor pelo conteúdo desse exemplo.

### Diagnósticos

Quando o LSP deteta erros de sintaxe ou de tipo, estes aparecem na área Diagnostics no canto inferior direito. Clicar num item de diagnóstico navega para a linha e coluna correspondentes. As mensagens `file:line:col` no stderr do compilador são também analisadas como marcadores de **Run Errors**, com os TraceID finais e os prefixos de severidade localizados removidos automaticamente.

### Erros e tempos limite

Antes, as falhas de carregamento do WASM iam apenas para `console.warn`, deixando o utilizador com um botão Run que não fazia nada. O comportamento atual é:

- Se o `no.wasm` ou o `lsp.wasm` não puderem ser descarregados ou compilados, aparece no topo da página uma faixa de erro dispensável que identifica o recurso e o motivo.
- Uma falha de inicialização do servidor de linguagem mostra igualmente uma faixa; o editor continua utilizável, mas o preenchimento automático, a informação ao passar o rato e a formatação ficam indisponíveis.
- Cada execução é cronometrada por um watchdog na thread principal (8 segundos por predefinição). `wasi.start()` é uma chamada síncrona dentro do worker, pelo que um loop infinito impede que qualquer temporizador nesse worker dispare — a única forma de o interromper é terminar o worker a partir da thread principal. Após um tempo limite, o worker é terminado e recriado automaticamente na execução seguinte, reiniciando também de imediato o pré-aquecimento do WASM.
- Cliques repetidos em Run são bloqueados por uma proteção de concorrência, e os temporizadores de debounce de edições pendentes são cancelados ao trocar de exemplo ou formatar, evitando que entradas antigas substituam conteúdo novo.

O `no.wasm` e o `lsp.wasm` são descarregados e compilados em fluxo durante o tempo de inatividade do navegador, pelo que o primeiro clique em Run já não precisa de esperar pela descarga e compilação completas de 27 MB.

## Limitações

Como o Playground corre dentro de uma sandbox WASM do navegador, algumas funcionalidades do Nolang não estão disponíveis:

### Sem FFI

O navegador não consegue ligar bibliotecas C nativas, pelo que as declarações FFI `#{c}`, `#{cpp}`, `#{rust}` não podem ser usadas no Playground. Código que envolve FFI (como os drivers `sqlite` ou `mysql`) não pode ser executado.

### Sem fork / exec / pipe

O WASI preview1 não fornece APIs de gestão de processos, pelo que a biblioteca padrão `process` (`process.start`, `process.wait`, etc.) não está disponível no Playground.

### Sockets de rede indisponíveis

O WASI preview1 não fornece APIs de socket, pelo que as seguintes bibliotecas padrão não estão disponíveis no Playground:

- `net` (listener / conn TCP)
- `http` / `http2` / `http3` (cliente HTTP)
- `ws` (WebSocket)
- `tls` (ligação TLS)
- `quic`, `sse`, `dns`

**Restrição mais rigorosa sobre o próprio `no` / `lsp`**: os artefactos wasm nem podem *importar* uma função de socket.
O navegador instancia-os através de `@wasmer/wasi`, que não implementa `sock_accept` / `sock_shutdown`,
pelo que um artefacto que puxe `net` / `net/http` para a ligação wasip1 faz o seguinte:

```
Failed to instantiate WASI: RuntimeError: JsValue(LinkError: WebAssembly.Instance():
Import #28 "wasi_snapshot_preview1" "sock_accept": function import requires a callable)
```

A consequência é que o programa nunca chega ao `_start` — o serviço de linguagem reporta "falha de inicialização" (preenchimento automático, informação ao passar o rato e formatação todos inoperáveis),
e o botão de execução falha da mesma forma. Por isso, todo o código de rede que só faz sentido em ambiente nativo é colocado em ficheiros `!wasip1`,
ficando no lado wasip1 apenas stub de erro: `cmd/lsp/pprof_wasip1.go`, `cmd/no/serve_esm_wasip1.go`,
`package/download_net_wasip1.go`, `package/release_net_wasip1.go`.
O `bun scripts/wasm_imports_check.ts` (`make wasm-imports-check`) protege esta invariante usando a própria tabela de importações do motor.

### `poll_oneoff` é fornecido pelo front end

Uma tabela de importações limpa apenas significa que as funções existem, não que funcionem. O corpo de `poll_oneoff` do `@wasmer/wasi` é
`unimplemented!()` em Rust: mal seja chamado (mesmo com zero subscrições), executa `unreachable` e faz o
instance inteiro entrar em trap, com um erro deste tipo:

```
Error while running start function: RuntimeError: JsValue(RuntimeError: unreachable)
```

O runtime do Go transforma tanto `time.Sleep` como `netpoll(delay)` numa única subscrição de relógio monotónico,
pelo que **todos os programas Go se deparam com isto** — `no fmt` / `no vet` / `no build` morriam na primeira vez que dormiam,
e a verificação da tabela de importações não detetava nada.

A correção está em `docs/src/playground/wasm-loader.ts`: `instantiateWasi()` constrói ele próprio o objeto de importações
a partir de `wasi.getImports()` e substitui `poll_oneoff` por uma implementação em JS (subscrições de relógio dormem até ao prazo mais próximo e reportam o evento do relógio;
subscrições apenas de fd reportam prontidão de imediato, evitando bloqueio perpétuo). Atenção que **não se pode** passar a substituição para
`wasi.instantiate(module, overrides)`: as funções próprias do glue têm prioridade, a substituição é ignorada silenciosamente e o programa continua em trap.
Também por se passar a construir o objeto de importações aqui, `wasi.start(instance)` deve receber explicitamente a instance.

### Tempo de compilação da primeira execução

O Run usa `no build --wasm-direct` para gerar o wasm. Em medições, o hello world demora cerca de **0,5 segundos**
dentro do wasm (o `no` nativo com o mesmo comando: cerca de 0,6 segundos; ambos após a pré-cozedura do índice std em 2026-11,
quando antes eram respetivamente cerca de 55 segundos e 11 segundos).

O gargalo não estava no codegen, mas em **analisar toda a std duas vezes em cada arranque a frio**: o
`ValidateUndefinedVars` do `validateProgram`, para reconhecer chamadas qualificadas por módulo como `math.max(...)`,
recolhe os símbolos exportados dos 119 módulos std; o `CollectStdUnionAliases` volta a analisar independentemente o mesmo conjunto de módulos para encontrar aliases de union.
Agora o `genstdsig` cozinha juntos "os símbolos exportados de cada módulo" e "quais os módulos que têm aliases de union"
no `stdsig_gen.go`, consultando a tabela diretamente em tempo de execução; há ainda duas proteções (`hir.TestModuleExportsMatchChecker`
compara campo a campo o resultado HIR recalculado módulo a módulo, e `TestUnionAliasNarrowingMatchesFullScan` compara a varredura estreita com a
varredura completa), pelo que uma tabela desatualizada faz os testes falhar em vez de alterar silenciosamente a semântica.

O watchdog mantém as duas reservas de tempo "carregamento + compilação 180 segundos / programa do utilizador 5 segundos": a primeira tem agora uma margem enorme,
mantendo-se para absorver arranques a frio, programas grandes e dispositivos lentos.

### Sistema de ficheiros virtual

O WASI preview1 fornece apenas os descritores de ficheiro stdin / stdout / stderr, sem sistema de ficheiros real. Assim, bibliotecas padrão que envolvem ficheiros ou ambiente, como `fs`, `path`, `os.get-env` e `os.set-env`, podem não funcionar como esperado no Playground.

### Limite de memória

O WASM em navegador está normalmente limitado por um espaço de endereçamento de 32 bits; o teto de memória utilizável é de aproximadamente **2 GB a 4 GB** (consoante o navegador e o sistema operativo). Programas que exigem muita memória (como recursão profunda ou arrays grandes) podem falhar por falta de memória.

## Compatibilidade com navegadores

O Playground requer um navegador moderno com suporte para WebAssembly. Recomendam-se as seguintes versões ou posteriores:

| Navegador | Versão mínima |
| --------- | ------------- |
| Chrome    | 100+          |
| Firefox   | 100+          |
| Safari    | 16+           |
| Edge      | 100+          |

Se o navegador não suportar WebAssembly ou tiver o JavaScript desativado, o Playground não consegue carregar o editor nem executar código.

## Desenvolvimento e validação

Após alterar o Playground, podes executar as seguintes verificações numa árvore de compilação limpa:

```bash
make playground-demos                       # compila + executa todos os exemplos em processo através do próprio loader WASI do playground
make playground-demos DEMO_RUN=wasmtime     # usa antes um wasmtime externo (para verificação cruzada)
make playground-demos DEMO_RUN=none         # apenas compila, sem verificar a saída (nunca usar como porta de lançamento)
make wasm-imports-check                     # reconstrói os dois wasm e verifica que a tabela de importações não tem funções socket
make wasm-runtime-smoke                     # executa version/fmt/vet pelo mesmo percurso de carregamento do worker
RUN_WASM_LSP=1 RUN_WASM_BUILD=1 make wasm-runtime-smoke   # acrescenta serviço de linguagem e compilação+execução completas
bun scripts/playground_ui_check.ts          # análise de diagnósticos, watchdog, recuperação do worker, i18n
make playground-smoke                       # servidor de desenvolvimento e rotas WASM devolvem HTTP 200
cd docs && bun run typecheck                # verificação de tipos TypeScript
```

O compilador do `playground-demos` é o `no` **nativo**, mas os artefactos seguem o percurso do navegador
`--wasm-direct -target wasm32-wasi` e são executados através de `instantiateWasi()` em
`wasi-glue` + `wasm-loader` — exatamente o mesmo código que o `runner.worker` executa.
Assim, não é preciso instalar o wasmtime e a CI corre-o diretamente. Ele verifica três coisas ao mesmo tempo: que o snippet compila, que o artefacto passa a validação WebAssembly
(um `local.set` com pilha vazia proveniente de um field value-match aparece aqui) e que
a saída impressa corresponde ao `expect` (um backend Direct-WASM que reporta silenciosamente `0` para `?=` aparece
aqui também).

A única coisa que não consegue detetar é a tabela de importações do próprio `no.wasm` / `lsp.wasm` (esses dois artefactos não são gerados a partir de snippets),
sendo isso coberto pelo `wasm-imports-check`: quando o `@wasmer/wasi` não tem `sock_*`, o `WebAssembly.Instance` lança `LinkError`,
e todo o playground (incluindo o serviço de linguagem) falha ao arrancar. O worker acrescenta ainda uma camada: o
`instantiateWasi()` do `wasm-loader.ts` preenche com stubs que devolvem `ENOSYS` / `NOTSUP` qualquer função WASI que o `@wasmer/wasi` não forneça,
fazendo com que uma importação inesperada degrade para uma chamada de sistema falhada em vez de derrubar o serviço inteiro.

O `wasm-imports-check` também não consegue apanhar "a função existe mas o corpo entra em trap" (a classe do `poll_oneoff`
acima), pelo que o `make wasm-runtime-smoke` fecha a última lacuna: executa `no version` /
`no fmt` / `no vet` pelo percurso real do `instantiateWasi()` e pode ainda verificar o
serviço de linguagem e uma compilação + execução completas.

O `docs/static/wasm/*.wasm` é excluído pelo `.gitignore` e artefactos locais nunca devem ser enviados. O `no.wasm` e o `lsp.wasm` são reconstruídos pela CI a partir da mesma árvore de código e com o mesmo `StdSigKey`, garantindo que o compilador no navegador, o blob AST std incorporado e o backend Direct-WASM provêm todos da mesma compilação. Uma discrepância de versões entre os três era a causa habitual dos antigos traps `unreachable` e erros de validação WASM.

A CI (`pr-verify` em `.github/workflows/build.yml` e a compilação do site em `static.yml`) executa por ordem os testes Go, a verificação de tipos, os checks de UI e a validação dos exemplos, e só depois reconstrói o WASM a partir de uma árvore limpa e implementa o site.
