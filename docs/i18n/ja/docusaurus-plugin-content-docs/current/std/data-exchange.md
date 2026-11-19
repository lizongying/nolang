---
sidebar_position: 4.1
---

## データ交換

### json — JSON 解析と生成

`json` は**ヒープ確保されたノードプール**（`json-pool`）で JSON ツリーを保持し、入れ子の配列とオブジェクト、文字列/数値/真偽値/null のスカラー、エスケープ文字、指数表記の数値をサポートします。ノードプールは動的な `vec` を使用するようになり、**固定容量の上限はありません**。ノード総数と単一ノードの子要素数は、利用可能なメモリによってのみ制限されます。高レベルインターフェースは `json` 構造体でノードプールとルートノードのインデックスをカプセル化します。`pool` フィールドには `#{inline=false}`（ポインタフィールド）が注釈付けされており、これにより `json` は値として安全に返却・コピーできます。

```no
; 解析：回傳 ?json（ok=成功，err=失敗並附原因）
j = json.parse('{"name":"Alice","age":30,"items":[1,2,3],"active":true}')
j: {
    ok -> {
        name = it.get-str('name'); 取標量：get-str / get-num / get-bool / get-i64（?值）
        age = it.get-i64('age'); 整數取值（?i64）
        items = it.get('items'); 取子節點，回傳 ?json
        s = it.stringify(); 序列化回字串
    }

    err(e) -> print(e)
}

; 產生：從空 json 開始，逐步 set / arr-push
j2 = json.new()
j2.set-str('key', 'value')
j2.set-i64('count', 3)
j2.arr-push-str('a')
print(j2.stringify())
```

**高レベル API（`json` 構造体）**

- `json.new()` — 空（null）の `json` を作成
- `json.parse(s)` — 解析して `?json` を返します。失敗時は `err` 分岐に具体的な理由（`empty input` / `parse error` / `trailing characters`）が入ります
- `j.stringify()` — 文字列にシリアライズ
- 値取得：`j.get-str(key)` / `j.get-num(key)` / `j.get-bool(key)` / `j.get-i64(key)`（それぞれ `?val` を返す）；`j.get(key)` は子ノード `?json` を返す
- スカラー直接取得（root がスカラーの場合）：`j.str()` / `j.num()` / `j.bool()` / `j.i64()` / `j.str-val()`
- 書き込み：`j.set-str(key,val)` / `j.set-num` / `j.set-i64` / `j.set-bool` / `j.set-null` / `j.set-key(key, val json)`
- 配列：`j.arr-get(i)` / `j.arr-len()` / `j.arr-push(val json)` / `j.arr-push-str` / `j.arr-push-num` / `j.arr-push-bool`
- オブジェクト列挙：`j.obj-len()` / `j.obj-key(i)` / `j.obj-keys-str()` / `j.delete-key(key)`
- 型判定：`j.kind()` および `j.is-null()` / `j.is-obj()` / `j.is-arr()` / `j.is-str()` / `j.is-num()` / `j.is-bool()`

**プール容量（固定上限なし）**

初期バージョンでは固定長配列をプールとして使用し、`JSON-MAX-NODES = 128` / `JSON-MAX-CHILDREN = 32` というハードリミットがあり、超過すると黙って失敗していました。現在は全面がヒープ `vec` に変更されました：

- ノード総数、各ノードの子要素数**ともに上限はなく**、利用可能なメモリによってのみ制限されます。
- `json.parse` は本当の構文エラー時にのみ `err` を返し、「プール枯渇」エラー分岐はなくなりました。
- `j.set*` / `j.arr-push*` は、構築対象の型が一致しない場合にのみ `false` を返します（例：非オブジェクトノードへの set、非配列ノードへの push）。`j.overflowed()` は後方互換性のために残されていますが、現在は常に `false` を返します。

> **`json` のコピー意味論（`#{inline=false}`）**：`pool` はポインタフィールドであり、`j.get` / `j.arr-get` が返す子ノード `json` はプール全体を**ディープコピー**したもので、親プールの独立したスナップショットです。スナップショットへの書き込みは親ノードに反映されません。また `?json`（option）は `match` 時にその値を消費するため、同じ option に対して**同じ `match` を繰り返す**と `nil` 分岐に入ります。同じ解析結果を複数回操作する場合は、単一の `match` 内で完了させるか、あらかじめ `json` のローカル変数で `it` を受け取ってください。

**低レベル API（`json-pool` / `json-value`）**

ノードインデックスを直接操作します。プールはローカル変数として使用します（単層構造体は値としての返却・コピーが安全です）：

- `p = json.new-pool()`。または `p = json-pool {}` の後に `p.init()`（`nodes`/`strs`/`ec`/`ek`/`en` の各 vec を `with-len(0)` に初期化）
- `idx = p.alloc()` — 新しいノードを確保してインデックスを返す（ヒープ vec が自動拡張）
- `p.add-child(node-idx, child-idx, key)` — 辺表（`ec`/`ek`/`en` の単方向連結リスト）で子要素を追加し、ノードごとの上限はありません
- `node-idx, next-pos, ok = p.parse(s, pos)` — `pos` から一つの値を解析し、ノードインデックスを返す
- `val-idx = p.get-key(obj-idx, key)`（`?i64`）；`p.arr-get(arr-idx, i)`（`?i64`）；`p.arr-len(arr-idx)`
- `p.get-kind(idx)` / `p.get-str(idx)` / `p.get-num(idx)` / `p.get-bool(idx)`
- `p.set-key(obj-idx, key, val-idx)`；`p.copy-tree(src, idx)`（部分木を再帰的にディープコピー）
- `p.stringify(node-idx)` — 指定したノードをシリアライズ

### toml — TOML 1.0 解析と生成

`toml` は `toml` で TOML ドキュメントを保持し、コメント、bare/quoted/dotted keys、通常のテーブル、array-of-tables、basic/literal/multiline strings、様々な整数の基数、浮動小数点数、真偽値、日付と時間、配列、inline table をサポートします。専用の Nolang 型が提供されていない値は、元のリテラル値が保持されます。

```no
cfg = toml.parse('title = "Nolang"\n[server]\nport = 8080\n[[server.backends]]\nname = "local"')

doc = toml.new()
doc.put('name', '"nolang"')
doc.put('server.port', '8080')
doc.put('enabled', 'true')
raw = toml.get(doc, 'server.port')
text = toml.stringify(doc)
```

よく使われる関数：

- `toml.new()` — 空のドキュメントを作成
- `toml.parse(text)` — 解析して `?toml` を返す
- `doc.put(key, raw-value)` — 検証して TOML 値を追加/更新
- `get(doc, key)` — 元の値を取得
- `get-str`、`get-i64`、`get-f64`、`get-bool` — スカラー値を取得
- `toml.stringify(doc)` — ドキュメントをシリアライズ

ドキュメントの上限は 4 個の key、2 個の通常テーブル、2 個の array-of-tables です。key は最大 32 bytes、value は最大 128 bytes です。

### yaml — YAML 1.2 解析と生成

`yaml` はノードプール（`pool`）で YAML 1.2 core schema のドキュメントツリーを保持し、ブロックマッピング、ブロックシーケンス、フローコレクション（`[...]` / `{...}`）、シングルクォート/ダブルクォート/plain スカラー、ブロックスカラー（`|` / `>` と `-` / `+` chomping）、コメント、ドキュメントマーカー（`---` / `...`）と複数ドキュメント、アンカー（`&`）とエイリアス（`*`）、マージキー `<<`、明示キー（`? k`）、型推論（null / bool / int / float / `.inf` / `.nan`）、ドットパス検索、および JSON / YAML シリアライズをサポートします。

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

よく使われる関数とメソッド：

- `yaml.parse(text)` / `yaml.parse-all(text)` — 解析して `?yaml` を返す（`err` 分岐に `line L, column C` の位置が入る）
- `yaml.valid-of(text)` / `yaml.error-of(text)` — 検証のみ行い、ドキュメントを構築しない
- `yaml.strip-bom(text)` — UTF-8 BOM を除去
- `doc-count()` / `doc(i)` — 複数ドキュメントへのアクセス
- `kind()`、`is-null()`、`is-bool()`、`is-int()`、`is-float()`、`is-nan()`、`is-str()`、`is-seq()`、`is-map()`、`len()`、`text()`、`tag()`、`anchor()`、`line-of()` — ノード情報
- `str()`、`i64()`、`f64()`、`bool()`、`scalar-text()` — 値取得、常に `?値` を返す
- `at(i)`、`value(i)`、`key(i)`、`key-node(i)`、`get(key)`、`find(path)` — 子ノード / キーの取得
- `get-str` / `get-i64` / `get-f64` / `get-bool`、`find-str` / `find-i64` / `find-f64` / `find-bool` — 便捷な値取得
- `to-json()`、`to-json-all()`、`to-yaml()`、`stringify()`、`source-of()` — シリアライズ

ノード種別定数：`YAML-KIND-NULL` / `BOOL` / `INT` / `FLOAT` / `STR` / `SEQ` / `MAP`（0–6）；スカラースタイル定数 `YAML-STYLE-PLAIN` / `SINGLE` / `DOUBLE` / `LITERAL` / `FOLDED` / `ALIAS`（0–5）と `YAML-STYLE-BLOCK` / `FLOW`（6–7）。

> **注意一**：子ノードビューは `?yaml` で返され、値取得ヘルパー関数は `?値` を返します。`match` でアンパックします（`child = d.at(i)` の `ok` アームは `it` で子ノードをバインドし、デフォルトアームが空値を処理します。各アームは独立した行にしてください）。子ノードと親ドキュメントはノードプールを共有するため、同じ `match` 内で値取得を完了させるか、先に一つバインドしておき、複数のプール変更をまたいで古い handle を保持しないでください。値取得には常に `find-str` / `find-i64` のようなパス便捷関数を使用することを推奨します。
>
> **注意二**：入れ子の `match` では `it` は**階層ごとに復元**されます。内側の `match` のアームは自身の本体に `it` を再バインドし（アーム本体は内側の値を見る必要があります）、そのアームが終わるとすぐに `it` は外側アームの本体に復元されます。したがって以下の書き方は安全です：
>
> ```no
> d ?yaml = yaml.parse(src)
> d: {
>     nil -> print('nil')
>
>     err -> print(it.msg())
>
>     -> {
>         print(it.find-str('a'))        ; 外層 it = 文件
>         v ?yaml = it.at(0)
>         v: {
>             nil -> print('none')
>             err -> print('err')
>             -> print(it.find-str('b')) ; 內層 it = 子節點
>         }
>         print(it.find-str('c'))        ; ✅ it 已還原成文件
>     }
> }
> ```
>
> 唯一の例外は最外層の `match` です。終了後も `it` は最後のアームの値を保持し（復元されません）、したがって `match` の外側で `it` に依存しないでください。階層をまたいで使用する必要がある場合は、明示的に名前付きローカル変数に保存する（`doc = it`）か、分解バインディング `ok(v) -> ...` を使用できます。
>
> **注意三**：`.nan` は実際の NaN として解析されます（`f64()` は NaN を返し、`x != x` が真になります）。JSON には NaN がないため、`to-json()` はそれに対して `null` を出力し、`to-yaml()` は `.nan` を出力します。

---
