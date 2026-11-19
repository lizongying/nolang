---
sidebar_position: 8
---

# Backend JS

O Nolang suporta compilar código-fonte `.no` diretamente para JavaScript, sem a ferramenta LLVM. O backend JS usa uma estratégia de **eliminação de tipos** (type erasure): todas as anotações de tipo do Nolang não são preservadas na saída JS, gerando-se apenas o comportamento em tempo de execução.

## Início rápido

```bash
# Compilar para JS (saída em dist/<name>.js)
no build --js main.no

# Modo navegador: gerar JS + wrapper HTML
no build --js --browser main.no

# Executar: compilar para JS e correr com node
no run --js main.no

# Modo navegador: compilar e abrir no navegador predefinido
no run --js --browser main.no
```

## Princípios de conceção

Conceção central do backend JS:

- **Eliminação de tipos**: o JS é dinamicamente tipado e as anotações de tipo do Nolang `int`/`str`/`bool`/`vec[T]`/`[N]T`/`?T` não aparecem de todo na saída JS
- **Sem necessidade de LLVM**: emite código-fonte JavaScript diretamente da AST, sem depender da ferramenta clang/LLVM
- **Duplo ambiente**: suporta Node.js (predefinição) e navegador (`--browser`) como ambientes-alvo
- **Anotações de plataforma**: `#{js}` e `#{js-browser}` controlam a visibilidade do código por plataforma

A implementação está no diretório `src/build/js/`:

| Ficheiro | Responsabilidade |
| --- | --- |
| `generator.go` | Lógica principal de geração de código AST → JavaScript |
| `expr.go` | Geração de expressões |
| `stmt.go` | Geração de instruções |
| `builtin.go` | Mapeamento de funções incorporadas (print→console.log, etc.) |
| `html_wrapper.go` | Modelo HTML do modo navegador |

## Parâmetros da linha de comando

| Parâmetro | Descrição |
| --- | --- |
| `--js` | Usar o backend JS (emitir JavaScript, ignorando o LLVM) |
| `--browser` | Gerar saída orientada ao navegador (HTML + JS, requer `--js`) |
| `--esm-cdn <url>` | Substituir o prefixo CDN do import map ESM do navegador (predefinição `https://esm.sh`) |
| `--js-runtime <cmd>` | Runtime JS usado por `no run --js` (predefinição `node`; pode trocar por `bun`, `deno`, etc., com argumentos adicionais) |
| `-o <path>` | Especificar o caminho de saída |

### Compilar

```bash
no build --js main.no                    # saída dist/main.js
no build --js -o app.js main.no          # especificar caminho de saída
no build --js --browser main.no          # saída dist/main.js + dist/main.html
```

### Executar

```bash
no run --js main.no                      # compila para JS e executa com node
no run --js --browser main.no            # compila para JS de navegador + HTML e abre o navegador
```

#### Escolher o runtime JS

`--js-runtime` especifica o runtime que executa o artefacto; a **predefinição é `node`** (Node.js). Qualquer runtime capaz de executar módulos ES serve:

```bash
no run --js --js-runtime bun main.no             # Bun
no run --js --js-runtime deno run main.no        # Deno (o primeiro token é o executável, os restantes são argumentos antes do script)
no run --js --js-runtime "deno run --allow-read" main.no
no run --js --js-runtime "node --enable-source-maps" main.no
no run --js --js-runtime /opt/js/bin/node main.no   # caminho absoluto
```

- O primeiro token separado por espaços é o executável; os restantes tokens são colocados antes do caminho do script; os argumentos do programa continuam a escrever-se depois do nome do ficheiro `.no`.
- A predefinição (sem `--js-runtime`) é `node`; se o sistema só tiver o binário `nodejs` do estilo Debian/Ubuntu, este é usado como alternativa. Um runtime explicitamente indicado (`bun`, caminho absoluto, `node --flags`) nunca é substituído; se não for encontrado, dá erro.
- Se o runtime não estiver no `PATH`, dá erro de imediato e sugere usar `--js-runtime` para escolher outro (não há regressão silenciosa para node).
- As importações ESM (`# npm/…`) funcionam também com estes runtimes: o `no run --js` liga o `node_modules` do projeto ao diretório temporário, e node/bun/deno resolvem tudo pelas mesmas regras.

## Anotações de plataforma

O backend JS introduz duas chaves adicionais de anotação de plataforma:

| Chave | Quando corresponde |
| --- | --- |
| `#{js}` | Backend JS (tanto Node.js como navegador) |
| `#{js-browser}` | Modo navegador (com `--browser`) |

```no
; Preservado apenas quando compilado pelo backend JS
#{js}
js-helper = () {
    print('JS only code')
}

; Preservado apenas no modo navegador
#{js-browser}
print('running in browser mode')

; Preservado apenas nos backends nativos (excluído na compilação JS)
#{mac-arm64}
print('running on macOS ARM64')
```

## Mapeamento de funções incorporadas

O backend JS mapeia as funções incorporadas do Nolang para os seus equivalentes JavaScript:

| Nolang | JavaScript | Notas |
| --- | --- | --- |
| `print(x)` | `console.log(x)` | Adiciona nova linha automaticamente |
| `eprint(x)` | `console.error(x)` | Escreve para stderr |
| `format(...)` | Concatenação de strings | v1 simplificada: usa concatenação com `"" +` |
| `len(x)` | `x.length` | Válido para strings e arrays |
| `with-len(n)` | `new Array(n)` | Cria um array com o comprimento especificado |

## Biblioteca padrão do backend JS

O diretório `src/js/` fornece módulos exclusivos do backend JS, todos com a anotação `#{js}`:

### API do navegador

#### `js/dom` — Manipulação do DOM

```no
# js/dom

; Criar elementos
el = dom.create-element('div')
heading = dom.create-element('h2')

; Consultar elementos
el = dom.get-element-by-id('my-id')
el = dom.query-selector('.my-class')

; Obter o body
body = dom.body()

; Métodos de elemento (mapeados por builtin.go)
el.set-text('Hello')
el.set-style('color', 'red')
el.set-attr('data-id', '42')
el.append-child(child)
```

#### `js/canvas` — Desenho Canvas 2D

```no
# js/canvas

canvasEl = dom.create-element('canvas')
canvasEl.set-attr('width', '240')
canvasEl.set-attr('height', '140')
body.append-child(canvasEl)

ctx = canvas.get-context-2d(canvasEl)
ctx.set-fill('red')
ctx.fill-rect(10, 10, 60, 60)
ctx.set-stroke('orange')
ctx.begin-path()
ctx.move-to(120, 80)
ctx.line-to(80, 130)
ctx.line-to(160, 130)
ctx.fill()
ctx.stroke()
```

#### `js/events` — Processamento de eventos

```no
# js/events

btn = dom.create-element('button')
btn.set-text('Click me')
body.append-child(btn)

; Retorno de chamada anónimo
events.on-click(btn, () {
    print('button was clicked!')
})

; Carregamento da página
events.on-load(() {
    print('page loaded')
})
```

#### `js/storage` — localStorage

```no
# js/storage

storage.set-item('key', 'value')
val = storage.get-item('key')
storage.remove-item('key')
storage.clear()
```

#### `js/location` — Location API

```no
# js/location

href = location.href()
search = location.search()
path = location.path()
location.redirect('https://example.com')
```

#### `js/history` — History API

```no
# js/history

history.back()
history.forward()
history.push('/page2')
n = history.length()
```

#### `js/animation` — Fotogramas de animação

```no
# js/animation

id = animation.request-frame(() {

    ; Retorno de chamada por fotograma
    print('frame')
})
animation.cancel-frame(id)
```

### API do Node.js

#### `js/fs-read-file` / `js/fs-write-file` — Leitura e escrita de ficheiros

```no
# js/fs-read-file
# js/fs-write-file

data = fs-read-file('input.txt')
fs-write-file('output.txt', data)
```

#### `js/http-fetch` — Obtenção HTTP

```no
# js/http-fetch

data = http-fetch('https://api.example.com/data')
print(data)
```

#### `js/process-exit` — Saída do processo

```no
# js/process-exit

process-exit(0)
```

#### `js/fetch` — Fetch API (async)

```no
# js/fetch

; Obter dados de um URL de forma assíncrona
data = fetch.async('https://api.example.com/data')
json-data = fetch.json-async('https://api.example.com/data')
```

## Modo navegador

Quando se usa `--browser`, o compilador gera:

1. **Ficheiro JS**: o código-fonte JavaScript compilado
2. **Ficheiro HTML**: um wrapper HTML que referencia o JS, contendo uma div `#nolang-output`

Características do wrapper HTML:

- A saída de `print()` é redirecionada para `<div id="nolang-output">`
- A página inclui estilos básicos (tipo de letra, borda, área de saída monoespaçada)
- O artefacto compilado é referenciado via `<script src="name.js">`

O caminho de saída predefinido é o diretório `dist/`.

## Importação de módulos ESM de terceiros

O backend JS pode consumir pacotes do ecossistema npm (o equivalente ao `import` de TypeScript), **sem nova instrução `import`** — reutiliza a diretiva `#` com o prefixo reservado `npm/`:

```no
# npm/lodash; objeto do módulo inteiro：lodash.get(x, 'a.b')
# npm/lodash.get as get; uma exportação nomeada：get(x, 'a.b')
# npm/lodash.default as d; exportação predefinida do pacote：d(props)
# npm/@scope/pkg; pacote com scope
# npm/lodash/fp.get as fpg; importação de subcampo
```

Regras:

- **Tem de ser o backend JS.** Os backends LLVM e Direct-WASM rejeitam `# npm/...` com erro e sugerem usar `no build --js`.
- **O pacote tem de ser declarado** em `dependencies` (ou `devDependencies`) de `package.noml`, caso contrário a compilação falha. Importações de subcampo (`lodash/fp`) só precisam de o pacote pai (`lodash`) estar na lista.
- **A qualificação por nome curto mantém-se**: depois de `# npm/lodash` podes escrever `lodash.get(...)`; uma importação nomeada (`as get`) é chamada diretamente como `get(...)`.
- **v1 sem tipos e sem passagem de tipos**: as ligações importadas não transportam tipos Nolang (zero alterações no checker), pelo que não há verificação de assinaturas; quando uma importação colide com uma função local de mesmo nome, o lado importado é automaticamente renomeado (sufixo `$esm`).
- **Em caso de colisão com módulo incorporado, a importação prevalece**: depois de `# npm/math`, `math.sin(x)` aponta para o pacote npm `math`, não para `Math.sin`.

### Executar no Node.js

O artefacto é um módulo ES padrão (instruções `import` só podem aparecer no escopo de módulo). O `no build --js` coloca um `package.json` (`{"type": "module"}`, convenção npm) ao lado do artefacto para que o node o execute como ESM; se o diretório já tiver um `package.json` que não seja ESM, a compilação sugere usar `-o out.mjs`.

```bash
npm install lodash                    # instalar primeiro o pacote
no build --js main.no                 # produz dist/main.js (+ dist/package.json)
node dist/main.js                     # executar como ESM
no run --js main.no                   # compila para um diretório temporário e executa com node
```

O `no run --js` liga o `node_modules` do projeto ao diretório temporário (o node resolve pacotes subindo a partir do diretório do script) e falha com uma dica de instalação quando falta um pacote; o `no build --js` apenas avisa sobre pacotes em falta (o artefacto continua válido — os pacotes só são necessários em tempo de execução).

### Executar no navegador

Com `--browser`, o HTML passa a ser `<script type="module">` com um import map que resolve especificadores nus através de um CDN (predefinição `https://esm.sh`):

```html
<script type="importmap">
{"imports": {"lodash": "https://esm.sh/lodash"}}
</script>
<script type="module" src="main.js"></script>
```

Formas de trocar o CDN (prioridade da mais alta para a mais baixa):

1. `no build --js --browser --esm-cdn https://cdn.jsdelivr.net/npm main.no`
2. `compiler: { esm-cdn: 'https://cdn.jsdelivr.net/npm' }` em `package.noml`

Como `<script type="module">` é bloqueado por CORS com `file://`, o `no run --js --browser` inicia um servidor estático descartável em `127.0.0.1` numa porta temporária e abre o navegador (Ctrl+C para parar). Um exemplo completo está em `example/esm-imports/`.

## Pacotes nativos: WASM e FFI

Além de importar pacotes JS, o backend JS também pode chamar pacotes nativos (bibliotecas partilhadas com ABI C ou WASM). Não se acrescenta nova sintaxe — escolhe-se o percurso de chamada através de uma anotação nas declarações já existentes. As declarações de funções C (`#{c}` + `name = (params) (results)`, ver o capítulo FFI) são a interface nativa, que o backend JS antes ignorava; com a anotação `bind`, é gerada uma ligação de chamada para cada extern `#{c}`.

### Anotações

`bind` é uma **estratégia de compilação**: pode escrever-se acima da linha `# import` (em linha própria) ou no fim da linha, ou ainda dentro do grupo de anotações acima de um extern `#{c}`.

| Chave | Valores | Descrição |
| --- | --- | --- |
| `bind` | `auto` / `ffi` / `wasm` | forma de chamada; uma compilação JS só permite uma estratégia |
| `wasm` | string | especificador do módulo WASM; obrigatório para `bind=wasm` e para `auto` no navegador |
| `lib` | string | substitui o nome da biblioteca FFI (predefinição: `compiler.link-libs`) |
| `init` | identificador | nome da exportação de inicialização WASM, predefinição `default` |

```no
; pow da libm：FFI via koffi；o navegador precisa de um specifier wasm=
#{c, bind=auto}
pow = (x f64, y f64) (r f64)
```

### Matriz alvo × estratégia

| Alvo | `bind=ffi` | `bind=wasm` | `bind=auto` |
| --- | --- | --- | --- |
| node / bun / deno | ligação koffi dos externs | importação WASM + ready | com `link-libs` / `lib=` → FFI; senão exige `wasm=` → WASM; senão erro |
| navegador | **erro em tempo de compilação** (koffi não corre no navegador) | importação WASM + ready | com `wasm=` → WASM; senão erro |

Regras adicionais: `bind=ffi` exige pelo menos um extern `#{c}` (senão erro) e uma biblioteca de `link-libs` ou `lib=` (senão erro). Estratégias `bind` / `wasm` conflituantes em vários pontos do programa fazem a compilação falhar.

### FFI (local / node)

A FFI corre através de [`koffi`](https://koffi.dev), FFI real: `koffi.load(<lib>)` + `lib.func(symbol, ret, args)`. A biblioteca vem de `compiler.link-libs` de `package.noml` (`lib=` substitui); várias bibliotecas são tentadas por ordem e a primeira que servir é usada. Cada extern `#{c}` gera uma ligação cujo nome local é `jsIdent(name)` (`_sqlite3-open` → `_sqlite3Open`) e cujo símbolo C retira o `_` inicial e troca `-` por `_` (`sqlite3_open`). Mapeamento de tipos:

| Nolang | koffi | Nolang | koffi |
| --- | --- | --- | --- |
| `i8` / `u8` / `byte` | `int8` / `uint8` | `i32` / `u32` | `int` / `uint` |
| `i16` / `u16` | `int16` / `uint16` | `i64` / `u64` | `int64` / `uint64` |
| `f32` / `f64` | `float` / `double` | `str` | `str` |
| `bool` | `bool` | `void` / sem retorno | `void` |
| `*T` | `koffi.pointer(<T>)` | `**T` | `koffi.pointer(koffi.pointer(<T>))` |

Todo o resto (slices / arrays / structs) usa `"void *"` como fallback na v1. O `koffi` é acrescentado como dependência ESM sintética: o `no build` apenas avisa se faltar, enquanto o `no run --js` dá erro e sugere `npm install koffi` (mesma política do ESM). O koffi é CJS, pelo que passa pela camada de interoperabilidade `import * as` + `.default ??`, em vez de um named import direto.

### WASM (navegador / `bind=wasm`)

O modo WASM importa o módulo `.wasm` indicado e encaminha cada extern `#{c}` para a sua exportação de mesmo nome (`_sqlite3-open` → exportação wasm `sqlite3_open`). A inicialização produz uma Promise de prontidão (**não aguardada**): um módulo inicializado de forma síncrona fica pronto de imediato; um assíncrono muda de estado quando fica pronto e expõe a Promise de prontidão em `globalThis.__nolang_wasm_ready[<curto>]` para o JS externo a aguardar. Uma chamada antes da prontidão lança um erro claro.

O especificador WASM tem duas formas, e as regras diferem conforme a forma:

| Forma | Exemplo | Entrada em `dependencies` | Import map do navegador | `no run --js` |
| --- | --- | --- | --- | --- |
| bare (pacote npm) | `@sqlite.org/sqlite-wasm` | **obrigatória** | resolvida via `--esm-cdn` (predefinição `https://esm.sh`) | precisa de `npm install` |
| relativa (ficheiro local) | `./math-wasm.mjs` | **não necessária** | **não entra no map** (o navegador nunca remapeia especificadores relativos) | liga o ficheiro (e as suas próprias importações relativas) ao diretório temporário do bundle |

A forma relativa cobre o caso "o módulo está no projeto": um bundle de `no build --js` colocado ao lado do módulo funciona diretamente, o `no run --js` liga o módulo ao seu diretório temporário de bundle, e um alvo navegador precisa de copiar o módulo também para `dist/` (a importação resolve-se em relação à localização do documento).

### Exemplo completo

- `example/native-libm/`: `#{bind = auto, c}` declara o `pow` da libm e `no run --js main.no` imprime `1024` (o `no run` precisa primeiro de `npm install` nesse diretório para instalar o `koffi`).
- `example/native-wasm/`: um módulo WASM local (`math.wasm` + `math-wasm.mjs`, sem dependências npm); `no run --js main.no` imprime `42.25` e `42`, e o import map de `no build --js --browser main.no` não inclui o especificador relativo.

### Limitações conhecidas (v1)

- O WASM apenas faz **encaminhamento de exportações de mesmo nome**, sem camada de adaptação de API (mapear uma API para outro pacote WASM, como `better-sqlite3` → `sql.js`, exige um módulo wrapper em nolang próprio).
- A estratégia é **por programa**: uma compilação JS permite uma única estratégia `bind` para todos os externs `#{c}`; a associação por módulo fica para trabalho futuro.
- A FFI só liga bibliotecas partilhadas com ABI C; para addons nativos do Node (`.node`), usa `bind=wasm` ou uma importação ESM normal.

## Exemplo completo

```no
; main.no — demonstração de aplicação para navegador
# js/dom
# js/canvas
# js/events
# js/storage

print('=== Nolang Browser Demo ===')

; DOM：criar título e acrescentar ao body
heading = dom.create-element('h2')
heading.set-text('Hello from Nolang!')
body = dom.body()
body.append-child(heading)

; DOM：criar botão
btn = dom.create-element('button')
btn.set-text('Click me')
btn.set-style('margin', '8px')
body.append-child(btn)

; Eventos：clique no botão
events.on-click(btn, () {
    print('button was clicked!')
})

; Canvas：desenhar retângulos
canvasEl = dom.create-element('canvas')
canvasEl.set-attr('width', '240')
canvasEl.set-attr('height', '140')
body.append-child(canvasEl)

ctx = canvas.get-context-2d(canvasEl)
ctx.set-fill('red')
ctx.fill-rect(10, 10, 60, 60)
ctx.set-fill('blue')
ctx.fill-rect(80, 10, 60, 60)

; localStorage：guardar e ler
storage.set-item('greeting', 'Hello from localStorage')
g = storage.get-item('greeting')
print('stored:', g)

; Iteração
nums = [10, 20, 30]
i <- nums: {
    print('elem:', i)
}

; Ramificação de plataforma
#{js-browser}
print('running in browser mode')

print('=== done ===')
```

Compilar:

```bash
no build --js --browser main.no
# Saída: dist/main.js, dist/main.html
# Abrir dist/main.html num navegador
```
