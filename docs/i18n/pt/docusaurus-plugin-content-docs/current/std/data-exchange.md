---
sidebar_position: 4.1
---

## Intercâmbio de dados

### json — análise e geração de JSON

O `json` guarda uma árvore JSON num **pool de nós alocado na heap** (`json-pool`), suportando arrays/objetos aninhados, escalares string/número/booleano/null, sequências de escape e números em formato exponencial. O pool passou a usar `vec` dinâmico e **não tem limite fixo de capacidade** — o número total de nós e o número de elementos filhos por nó só estão limitados pela memória disponível. A interface de alto nível encapsula o pool e o índice do nó raiz na struct `json`; o seu campo `pool` está anotado com `#{inline=false}` (campo de ponteiro), o que permite que `json` seja retornado e copiado por valor em segurança.

```no
; Análise：retorna ?json（ok=sucesso，err=falha com motivo）
j = json.parse('{"name":"Alice","age":30,"items":[1,2,3],"active":true}')
j: {
    ok -> {
        name = it.get-str('name'); obter escalar：get-str / get-num / get-bool / get-i64（?valor）
        age = it.get-i64('age'); obter inteiro（?i64）
        items = it.get('items'); obter nó filho, retorna ?json
        s = it.stringify(); serializar de volta para string
    }

    err(e) -> print(e)
}

; Geração：começar com json vazio, acrescentar com set / arr-push
j2 = json.new()
j2.set-str('key', 'value')
j2.set-i64('count', 3)
j2.arr-push-str('a')
print(j2.stringify())
```

**API de alto nível (struct `json`)**

- `json.new()` — cria um `json` vazio (null)
- `json.parse(s)` — analisa e retorna `?json`; em caso de falha, o ramo `err` traz o motivo específico (`empty input` / `parse error` / `trailing characters`)
- `j.stringify()` — serializa para string
- Leitura: `j.get-str(key)` / `j.get-num(key)` / `j.get-bool(key)` / `j.get-i64(key)` (cada um retorna `?valor`); `j.get(key)` retorna um nó filho `?json`
- Leitura direta de escalar (quando a raiz é escalar): `j.str()` / `j.num()` / `j.bool()` / `j.i64()` / `j.str-val()`
- Escrita: `j.set-str(key,val)` / `j.set-num` / `j.set-i64` / `j.set-bool` / `j.set-null` / `j.set-key(key, val json)`
- Array: `j.arr-get(i)` / `j.arr-len()` / `j.arr-push(val json)` / `j.arr-push-str` / `j.arr-push-num` / `j.arr-push-bool`
- Enumeração de objeto: `j.obj-len()` / `j.obj-key(i)` / `j.obj-keys-str()` / `j.delete-key(key)`
- Verificação de tipo: `j.kind()` e `j.is-null()` / `j.is-obj()` / `j.is-arr()` / `j.is-str()` / `j.is-num()` / `j.is-bool()`

**Capacidade do pool (sem limite fixo)**

As versões anteriores usavam um array fixo como pool, com limite rígido `JSON-MAX-NODES = 128` / `JSON-MAX-CHILDREN = 32`, falhando silenciosamente ao exceder. Passou tudo a `vec` na heap:

- O número total de nós e o número de filhos por nó **não têm limite**, apenas o da memória disponível.
- `json.parse` só retorna `err` perante um erro de sintaxe real; deixou de existir o ramo de erro "pool esgotado".
- `j.set*` / `j.arr-push*` só retornam `false` quando o tipo de destino é incompatível (por exemplo, set num nó não-objeto, push num nó não-array). `j.overflowed()` mantém-se por compatibilidade retroativa e agora retorna sempre `false`.

> **Semântica de cópia de `json` (`#{inline=false}`)**: `pool` é um campo de ponteiro, e os nós filho retornados por `j.get` / `j.arr-get` **copiam profundamente** o pool inteiro, sendo um instantâneo independente do pool pai — escritas no instantâneo não são refletidas no nó pai. Além disso, como `?json` (option) consome o seu valor no `match`, **voltar a fazer match do mesmo option** entra no ramo `nil`; se precisares de várias operações sobre o mesmo resultado da análise, fá-las dentro de um único `match` ou recebe primeiro `it` numa variável local `json`.

**API de baixo nível (`json-pool` / `json-value`)**

Opera diretamente por índice de nó, usando o pool como variável local (uma struct de nível único é segura para retornar/copiar por valor):

- `p = json.new-pool()`; ou `p = json-pool {}` seguido de `p.init()` (inicializa cada vec `nodes`/`strs`/`ec`/`ek`/`en` com `with-len(0)`)
- `idx = p.alloc()` — aloca um novo nó e retorna o índice (o vec na heap expande automaticamente)
- `p.add-child(node-idx, child-idx, key)` — acrescenta um filho via lista de arestas (`ec`/`ek`/`en` como lista ligada unidirecional), sem limite por nó
- `node-idx, next-pos, ok = p.parse(s, pos)` — analisa um valor a partir de `pos` e retorna o índice do nó
- `val-idx = p.get-key(obj-idx, key)`（`?i64`）；`p.arr-get(arr-idx, i)`（`?i64`）；`p.arr-len(arr-idx)`
- `p.get-kind(idx)` / `p.get-str(idx)` / `p.get-num(idx)` / `p.get-bool(idx)`
- `p.set-key(obj-idx, key, val-idx)`；`p.copy-tree(src, idx)`（cópia profunda recursiva da subárvore）
- `p.stringify(node-idx)` — serializa o nó indicado

### toml — análise e geração de TOML 1.0

`toml` guarda documentos TOML em `toml` e suporta comentários, chaves bare/quoted/dotted, tabelas normais, array-of-tables, strings basic/literal/multiline, várias bases de inteiros, números de ponto flutuante, booleanos, datas/horas, arrays e inline tables. Valores sem tipo Nolang dedicado mantêm-se como literais originais.

```no
cfg = toml.parse('title = "Nolang"\n[server]\nport = 8080\n[[server.backends]]\nname = "local"')

doc = toml.new()
doc.put('name', '"nolang"')
doc.put('server.port', '8080')
doc.put('enabled', 'true')
raw = toml.get(doc, 'server.port')
text = toml.stringify(doc)
```

Funções comuns:

- `toml.new()` — cria um documento vazio
- `toml.parse(text)` — analisa e retorna `?toml`
- `doc.put(key, raw-value)` — valida e adiciona/atualiza um valor TOML
- `get(doc, key)` — obtém o valor bruto
- `get-str`、`get-i64`、`get-f64`、`get-bool` — obtêm valores escalares
- `toml.stringify(doc)` — serializa o documento

Os limites do documento são 4 chaves, 2 tabelas normais e 2 array-of-tables; as chaves têm no máximo 32 bytes e os valores 128 bytes.

### yaml — análise e geração de YAML 1.2

`yaml` guarda a árvore de documentos do core schema YAML 1.2 num pool de nós (`pool`), suportando mapeamentos de bloco, sequências de bloco, coleções de fluxo (`[...]` / `{...}`), escalares entre aspas simples/aspas duplos/plain, escalares de bloco (`|` / `>` com chomping `-` / `+`), comentários, marcadores de documento (`---` / `...`) e múltiplos documentos, âncoras (`&`) e aliases (`*`), chaves de fusão `<<`, chaves explícitas (`? k`), inferência de tipos (null / bool / int / float / `.inf` / `.nan`), consulta por caminho com pontos e serialização JSON / YAML.

```no
doc ?yaml = yaml.parse('name: Alice\nserver:\n  port: 8080\ntags:\n  - a\n  - b\n')
doc: {
    nil -> print('nil')

    err -> print(it.msg())

    -> {
        doc = it
        name = doc.find-str('name'); ?str
        port = doc.find-i64('server.port'); ?i64
        name: {
            ok -> print(it)

            -> print('')
        }
        port: {
            ok -> print(it.to-str())

            -> print('')
        }
        print(doc.to-json())
        print(doc.to-yaml())
    }
}
```

Funções e métodos comuns:

- `yaml.parse(text)` / `yaml.parse-all(text)` — analisa e retorna `?yaml` (o ramo `err` traz a posição `line L, column C`)
- `yaml.valid-of(text)` / `yaml.error-of(text)` — apenas valida, sem construir o documento
- `yaml.strip-bom(text)` — remove a BOM UTF-8
- `doc-count()` / `doc(i)` — acesso a múltiplos documentos
- `kind()`、`is-null()`、`is-bool()`、`is-int()`、`is-float()`、`is-nan()`、`is-str()`、`is-seq()`、`is-map()`、`len()`、`text()`、`tag()`、`anchor()`、`line-of()` — informação do nó
- `str()`、`i64()`、`f64()`、`bool()`、`scalar-text()` — leitura de valores, todos retornam `?valor`
- `at(i)`、`value(i)`、`key(i)`、`key-node(i)`、`get(key)`、`find(path)` — leitura de nós filho / chaves
- `get-str` / `get-i64` / `get-f64` / `get-bool`、`find-str` / `find-i64` / `find-f64` / `find-bool` — atalhos de leitura
- `to-json()`、`to-json-all()`、`to-yaml()`、`stringify()`、`source-of()` — serialização

Constantes de tipo de nó: `YAML-KIND-NULL` / `BOOL` / `INT` / `FLOAT` / `STR` / `SEQ` / `MAP`（0–6）；constantes de estilo de escalar `YAML-STYLE-PLAIN` / `SINGLE` / `DOUBLE` / `LITERAL` / `FOLDED` / `ALIAS`（0–5）e `YAML-STYLE-BLOCK` / `FLOW`（6–7）。

> **Nota um**: as vistas de nós filho são retornadas como `?yaml` e as funções auxiliares de leitura como `?valor`; desembrulha-as com match (`child = d.at(i)`, em que o braço `ok` vincula `it` ao nó filho e o braço predefinido trata a ausência, um braço por linha). O nó filho partilha o pool do documento pai, pelo que é recomendável concluir a leitura dentro do mesmo match, ou ligar uma cópia primeiro, e não manter handles desatualizados através de várias alterações ao pool. Para leitura, usa sempre funções de caminho convenientes como `find-str` / `find-i64`.
>
> **Nota dois**: em matches aninhados, `it` é **restaurado nível a nível**. O braço de um match interno volta a vincular `it` ao seu próprio sujeito (o corpo do braço tem de ver o valor interno), mas mal esse braço termina `it` reverte para o sujeito do braço exterior — pelo que a seguinte grafia é segura:
>
> ```no
> d ?yaml = yaml.parse(src)
> d: {
>     nil -> print('nil')
>
>     err -> print(it.msg())
>
>     -> {
>         print(it.find-str('a'))        ; it exterior = documento
>         v ?yaml = it.at(0)
>         v: {
>             nil -> print('none')
>             err -> print('err')
>             -> print(it.find-str('b')) ; it interior = nó filho
>         }
>         print(it.find-str('c'))        ; ✅ it voltou a ser o documento
>     }
> }
> ```
>
> A única exceção é o match mais exterior: depois de terminar, `it` mantém o valor do último braço (não é restaurado), pelo que não deves depender de `it` fora de um match. Para usar o valor através de vários níveis, guarda-o explicitamente numa variável local com nome (`doc = it`) ou usa uma ligação destruturadora `ok(v) -> ...`.
>
> **Nota três**: `.nan` é analisado como um NaN real (`f64()` retorna NaN e `x != x` é verdadeiro). O JSON não tem NaN, pelo que `to-json()` emite `null` para ele; `to-yaml()` emite `.nan`.

---
