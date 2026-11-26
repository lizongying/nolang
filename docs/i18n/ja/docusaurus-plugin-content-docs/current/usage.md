---
sidebar_position: 2
---

# インストールと使用

## インストール

### CLI のインストール

対応するプラットフォームの実行ファイルを [GitHub Releases](https://github.com/lizongying/nolang/releases/latest) からダウンロードするか、以下の方法でインストールしてください：

```bash
# macOS / Linux
# 1. 下載二進制文件
# 2. 放到 PATH 中
sudo mv nolang /usr/local/bin/no
```

### VS Code 拡張機能のインストール

[VS Code Marketplace](https://marketplace.visualstudio.com/items?itemName=lizongying.vscode-nolang) から Nolang 拡張機能をインストールすると、構文ハイライト、LSP 診断、定義へのジャンプ、自動補完などをサポートします。

## CLI コマンド

| コマンド                                                      | 説明                    |
| ------------------------------------------------------------ | ----------------------- |
| `no version`                                                 | バージョン情報を出力            |
| `no init`                                                    | ワークスペースを定義（workspace.noml を生成、package.noml は含まない） |
| `no new <name>`                                              | ワークスペース内に新しいパッケージを作成（サブディレクトリ + package.noml、workspace.noml に登録） |
| `no fmt [-w] [-d] <file\|dir>`                               | ソースコードを整形（インポートの重複排除/並べ替えを含み、別名なし std モジュールインポートを自動削除）；`.noml` 設定ファイルはレイアウトを保持する整形ルールに従う（インデント/空白/空行、コメントとキーの順序は変更しない） |
| `no build [-o <file>] [-cc <s>] [-target <s>] [-opt[=N]] [<file\|dir>]` | ビルド（executable を出力） |
| `no run [-cc <s>] [-target <s>] [<package\|dir\|file>]`        | ビルドして実行（パッケージ名/ディレクトリ/ファイル） |
| `no test [-cc <s>] [-target <s>] [<file>]`                   | テストを実行                |
| `no add <tool>[:<version>]@<host>/<owner>/<repo>`            | 依存関係を追加（事前コンパイル済み AST モジュール）|
| `no remove <pkg>`                                            | 依存関係を削除                |
| `no update <pkg>`                                            | 依存関係を更新                |
| `no update-all`                                              | すべての依存関係を更新            |
| `no list`                                                    | 依存関係を一覧表示                |
| `no sync`                                                    | 依存関係を同期                |
| `no install [-u] [<tool>[:<version>]@<host>/<owner>/<repo>]`  | binary をインストール             |
| `no uninstall <name>`                                        | binary を削除             |
| `no pack [-o <file>] [<dir\|file.no>]`                       | AST をパッケージング（`<tool>-ast.zip`）|
| `no pub [-o <file>]`                                         | `no pack` と同じ（registry へのアップロードは未実装）|
| `no cache <stat\|clear>`                                     | AST キャッシュの表示 / クリア（通常はクリア不要）|

## クイックスタート

Nolang は**単一リポジトリ（プロジェクト）・複数パッケージ**アーキテクチャを採用しています：リポジトリのルートディレクトリがワークスペース（`workspace.noml` のみ）で、各パッケージはサブディレクトリ（独自の `package.noml` を持つ）です。`no init` と `no new` は独立した二つの操作です：

- `no init` —— カレントディレクトリにワークスペースを定義（`workspace.noml` のみを生成）。
- `no new <name>` —— 現在のワークスペース内に新しいパッケージを作成（サブディレクトリ `./<name>/` とその `package.noml` を生成）し、そのパッケージを `workspace.noml` に登録。

### ワークスペースの初期化とパッケージの作成

```bash
# 1) 在倉庫根目錄定義工作區
no init

# 2) 在工作區內新建包（會自動註冊到 workspace.noml）
no new foo

# 3) 進入包目錄
cd foo

# 直接運行（自動構建並執行 main.no）
no run
```

ワークスペース内で `no new` を繰り返し使って、さらにパッケージを追加できます：

```bash
no new bar
no new baz
```

このときの `workspace.noml` は次のようになります：

```noml
{
  foo: '/foo'
  bar: '/bar'
  baz: '/baz'
}
```

> **パスの形式**：`workspace.noml` のマッピング値は、`/` 接頭辞でワークスペース相対パスを表します（例：`/foo` → `workspaceRoot/foo`）。`./`、`../` による相対ジャンプや OS の絶対パス（例：`/home/user/code/fork`）もサポートし、ワークスペース外のディレクトリを指すことができます。

`no build` をルートディレクトリで引数なしで実行すると、ワークスペース内のすべてのパッケージを並列でビルドします。

### ワークスペースのみ初期化（パッケージは作成しない）

```bash
# 在當前目錄定義工作區
no init
```

`no init` は `workspace.noml`（初期状態は空の `{}`）のみを生成し、**`package.noml` や `main.no` は生成しません**。既に `workspace.noml` が存在する場合は上書きされません。パッケージは `no new <name>` で作成する必要があります。

### ビルドと実行

```bash
# 構建（默認尋找 main.no）
no build                    # 構建當前目錄
no build main.no            # 構建指定文件
no build -o output          # 指定輸出路徑
no build -cc zig            # 使用 Zig 編譯器
no build -target x86_64-linux-gnu  # 交叉編譯（指定目標平台）

# 運行（構建 + 執行）
no run                      # 構建並執行當前目錄的 main.no
no run foo                  # 運行工作區中名為 foo 的包（解析 workspace.noml）
no run ./foo                # 運行 ./foo 目錄的 main.no
no run main.no              # 運行指定 .no 文件
no run -cc zig
no run -target aarch64-macos-gnu
```

> `no build -v` は出力パスの旁に元の LLVM IR（`<出力パス>.ll`）を追加で書き出します。`no build --shared -o libdemo.dylib main.no` は `#{cabi='symbol'}` でマークされたトップレベル関数を安定した C ABI シンボルとしてエクスポートし、`-shared -fPIC` で動的ライブラリにリンクするとともに、同名の `.h` ヘッダーファイルを自動生成します。`--header` でヘッダーパスを上書きできます。現在 `-c`（コンパイルのみ、リンクなし）オプションはありません。C / C++ / Rust / Go / Java / Python / C# から Nolang を呼び出す完全な方法は [ホスト言語から Nolang を呼び出す](./lang/host-interop) を参照してください。

`no run` の位置引数は次の順序で解決されます：

1. 既存の `.no` ファイル → そのファイルを直接実行
2. 既存のディレクトリ → その中の `main.no` を実行
3. ワークスペース（最も近い `workspace.noml`）に登録されたパッケージ名 → そのパッケージの `main.no` を実行

引数がない場合は、カレントディレクトリの `main.no` を実行します。カレントディレクトリがワークスペースルート（`workspace.noml` はあるが `main.no` はない）の場合は、`no run <package>` でパッケージを指定するよう促されます。

### クロスコンパイルターゲット

`-target` 引数の形式は `<arch>-<os>-<abi>` で、以下のターゲットをサポートします：

| ターゲット三元組      | 説明           |
| -------------------- | -------------- |
| `x86_64-linux-gnu`   | Linux x86_64   |
| `aarch64-linux-gnu`  | Linux ARM64    |
| `x86_64-macos-gnu`   | macOS x86_64   |
| `aarch64-macos-gnu`  | macOS ARM64    |
| `x86_64-windows-gnu` | Windows x86_64 |

**現在のプラットフォームの自動検出**：`no build`、`no run`、`no test` は `-target` が指定されていない場合、現在のホストプラットフォームを自動検出してネイティブコードにコンパイルします。日々の開発では target を手動で指定する必要はありません：

```bash
no run hello.no          # 本機直接跑
no test                  # 本機跑測試
no build -target aarch64-linux-gnu   # 需要交叉編譯時才顯式指定
```

### コンパイラの選択

`-cc` 引数は C コンパイラバックエンドを指定します：

- `clang`（デフォルト）— LLVM のインストールが必要
- `zig` — Zig のインストールが必要、クロスコンパイルに適しています

### 組み込み型のレイアウト（`?T` インライン閾値）

`?T` のメモリレイアウトは**ペイロードの型に応じて決まります**：各ペイロード型 `T` はそれぞれ
`%option_<key> = { i8 tag, [K x i64] }` を持ち（1 バイトのタグ、ペイロードはバイトオフセット 8 に固定、
`K = roundUp8(sizeof(T))/8`）。ペイロードが閾値に収まらない場合はヒープボックスに切り替わり、ペイロード領域の先頭語にポインタを格納します。

`--option-payload-inline-threshold=N` はこの閾値を指定します（**option 構造全体**のバイト数で計算、
つまり `1 + 7 + sizeof(T)`）：

```bash
no build --option-payload-inline-threshold=1032 main.no   # 讓更大的載荷內聯
no build --option-payload-inline-threshold=16 main.no     # 最小可配置值
```

| 値 | 効果 |
| --- | --- |
| デフォルト **264**（= 1 + 7 + 256） | `?i64` 16 B、`?str` 24 B、`?txt`（256 B）264 B がインライン；より大きなペイロードはボックス化 |
| 最小 **16** | `?i64` のみインラインのまま；`?str`（24 B）以降はすべてボックス化 |
| < 16 | コンパイルエラー |

優先順位：コマンドライン > 環境変数 `NOLANG_OPTION_PAYLOAD_INLINE_THRESHOLD` > `package.noml` の
`compiler.option-payload-inline-threshold` > デフォルト値。レイアウトの唯一の情報源は `src/mir/layout.go` で、
セマンティクス層（どの値が `ok`／`nil`／`err` とみなされるか）は**影響を受けず**、メモリ使用量とボックス化するかどうかだけが変わります。

### コンパイラ内蔵オプティマイザ（MIR opt）

コンパイラには **MIR 層**（HIR のローアリング後、LLVM コード生成前）で動作するオプティマイザが内蔵されています。これはバックエンドの LLVM `opt` とは別のものです：前者は MIR を書き換え、後者は生成済みの LLVM IR を書き換えます。

`-opt[=N]` で有効化します。**デフォルトはオフ**です：

```bash
no build main.no          # 預設：不啟用（與歷史行為一致）
no build -opt main.no     # 等同 -opt=1
no build -opt=2 main.no   # 常數折疊 + 死碼消除 + 控制流清理
no build -opt=0 main.no   # 顯式關閉
```

| レベル | 意味 |
| ---- | ---- |
| `0` / `off` / `false` / `no` / `none` | オフ（デフォルト） |
| `1` / `on` / `true` / `yes` / `-opt` | 定数折りたたみ + 整数恒等式の書き換え + コピー伝播 + 死んだスカラの除去 |
| `2` | レベル 1 + 制御フローの整理（定数分岐の折りたたみ、到達不能ブロックの削除、空ブロックのジャンプ連結、単一前駆ブロックの合体）+ プロローグのトリミング |

`-opt` は**グローバル**なスイッチ（`-v` と同様に `main` で処理される）なので、`build`、`run`、`test` すべてでサポートされます。

同じスイッチは環境変数 `NOLANG_MIR_OPT` でも指定できます。コマンドラインのフラグは単にこの変数に書き込むだけなので、両者が不一致になることはありません。今回の最適化で何が行われたかを確認します：

```bash
NOLANG_MIR_OPT=2 NOLANG_MIR_OPT_STATS=1 no build -o out main.no
# [miropt] level=2 funcs=3 folded=5 (arith=2 cmp=3) ident=1 copies=4 dead=6 cfg{condbr=3 unreachable=4(5 insts) threaded=1 merged=5}
# [miropt] prelude: dropped 27 unreferenced helpers
```

| フィールド | 意味 |
| ---- | ---- |
| `folded`（`arith`/`cmp`） | 定数に折りたたまれた算術／比較命令の数 |
| `ident` | あるオペランドに単純化された整数恒等式（`x+0`、`x*1`、`x&x`、`x^0`…） |
| `copies` | コピー伝播で削除されたスカラのコピー |
| `dead` | 削除された死んだスカラ命令 |
| `cfg{condbr, unreachable, threaded, merged}` | 定数分岐の折りたたみ、到達不能ブロックの削除（伴う命令数）、空ブロックのジャンプ連結、単一前駆ブロックの合体 |
| `prelude: dropped N` | codegen のプロローグ内で未参照と証明された runtime helper の数（下記参照） |

### プロローグのトリミング（レベル 2）

`emitPrelude` はプログラムが使用するかどうかに関わらず、runtime 全体（`str_*`、`print_*`、`eprint_*`、`vec_*`、`nolang_*`）を**すべての**モジュールに書き込みます——`print('hi')` というプログラムは 60 個の関数定義を生成し、そのうちエントリから到達可能なのは 8 個だけで、**52 個は一度も呼び出されません**。

**これは `opt -O2` ではできません**：LLVM の GlobalDCE は `internal` 定義しか削除しませんが、プロローグは external linkage で出力されます（`no` モジュールは C とリンクできるため、リンカが必要とする可能性のある helper は internal にできません）。このプログラムでの実測では、60 個の定義のうちちょうど 7 個が `define internal` で、`opt -O2` はこの 7 個だけを削除し、しかも `.ll` は逆に 61,812 から 69,874 bytes（+8,062）に増えました。

そのためレベル 2 は、IR 生成完了後に**完成済みのテキスト**に対して到達可能性のトリミングを一度行い、実際に参照されている helper だけを保持します（それらが呼び出す helper も再帰的に保持）。同じ `print('hi')` で：定義数 60 → **16**、`.ll` 61,812 → **14,548** bytes（**−76.5%**）、実行ファイル 36,616 → 34,648（−5.4%）。トリミングは本当の参照だけを認識し——**コメント内に現れる名前はカウントされません**。これが 33 個ではなく 44 個を削除できる鍵です。また、関数を捨てる際は**その関数の説明コメントも一緒に捨てます**（`optTrailingComment`）。そうしないと、存在しなくなった 44 個の関数を説明する 12,609 bytes が残ってしまいます。

コメントを削除しても**実行ファイルは小さくなりません**——コメントは `opt`/`llc` の解析時に破棄されるため、両者の実行ファイルはどちらも 34,648 bytes です。あの 12,609 bytes は IR テキストとコンパイル時の解析量の削減効果です。

トリミングの近似は常に保持側に寄せます——一意な名前を解決できないブロックは保持し、ブロックが閉じ波括弧を持たない場合はそこからトリミングを停止し、`declare`／グローバル／型定義は決して削除しません。最適化を見逃しても数バイト増えるだけですが、誤って削除するとリンクエラーになります。

**プロローグをまるごと取り除けるか？できません。** プロローグ領域には関数だけでなく、**プログラム自身の文字列リテラル**（`@.mir.str.N`、つまり `"hi"` そのもの）、`%str-long`／`%option`／`%task` などの型、`@malloc`／`@write`／`@llvm.memcpy` などの外部宣言もあります——これらのデータ面はモジュールの約 30% を占め、削除すると `llc` すら通りません（`error: use of undefined value '@.mir.str.1'`）。削除できるのは**関数**の半分だけです：48 個の `define` を同名の `declare` に置き換えると、リンカが報告する未定義シンボルはちょうど `print_nl`、`print_str`、`str_free`、`str_from_const` の 4 つです。したがって正しい問い方は「オンデマンドで出力できるか」であり、「取り除けるか」ではありません。

この無駄は**プログラムサイズと無関係な固定税**です：プロローグのコードとコメントはモジュールごとに同一で（48 個の定義、コード 36,043 ＋ コメント 17,516 bytes）、コーパスから抽出した 40 ファイルの保持数は **0〜19 個、中央値 9** でした。`tests/dup.no`／`tests/q.no`／`tests/u128-types.no` は **0 個**を保持——プロローグ全体がそれらにとって完全に死んでいます。したがって削減率はモジュールサイズだけで決まります：1 行のプログラムで −76.5%、3.0 MB の `test/std/pbkdf2.no` では −4.1% にとどまります。

プロローグをまるごと取り除けるか？できません——領域にはプログラム自身の文字列リテラル（`@.mir.str.N`）、型、外部宣言（1,151 bytes）があり、削除すると `llc` すら通りません。さらに `codegen.go:75-83` の `extraFuncs`／`extraFuncsBody` は元々オンデマンド出力器（option／struct／enum／ptr の helper はすべてここを通ります）で、プロローグは変換されていない唯一の部分です。しかしこれをオンデマンド化しても追加で得られるのはほぼコメントだけで、コード面にはもう取り立てるものがありません。

このオプティマイザが折りたたむのは **スカラ** だけです（`i64`/`u64`/`i16`/`i32`/`char`/`bool`/`f32`/`f64`）。`byte`/`u8`/`i8`、`i128`/`u128`、`option`、スライス、構造体などはすべてスキップされます：前者は `emitConst` に対応する分岐がなく（0 に退化します）、後者の算術は `option` のオーバーフローラップアラウンド経路を通るため、折りたたむとセマンティクスが変わってしまいます。浮動小数点数もすべての恒等式から除外されています——`x * 0.0` は `inf`／`NaN` に対して 0 ではなく、`x + 0.0` は `-0.0` に対して `x` ではありません。

その比較の核心的な結論をここで先に述べる価値があります：**局所的な整理（定数折りたたみ、死コード除去、制御フローの整理）は `opt -O2` を実行した後では限界効果がほぼゼロ**（−0.06%）です。LLVM が同じ種類の作業をより徹底的に行うためです。本当に効果があるのは LLVM が **linkage のためにできない**こと——つまり上記のプロローグのトリミングです。プロローグのトリミング（コメントを考慮したトリミングを含む）を加えると、`mir+llvm_O2` → `mir_opt+llvm_O2` の全体の限界値は **−19.05% 命令 / −15.83% テキスト** になります。`mir` → `mir+mir_opt` のテキスト削減率は −14.29% から **−17.75%** に変わります（631 ファイルの全コーパス）。

## JS バックエンド

JS バックエンド（`no build --js` / `no run --js`）は `.no` ソースコードを JavaScript にコンパイルします（型消去）。
Node.js とブラウザの 2 つのターゲット、`#{js}` / `#{js-browser}` プラットフォームアノテーション、
サードパーティの ESM インポート（`# npm/<pkg>`）をサポートします。

JS に関する完全なドキュメント（コマンドライン引数、組み込み関数のマッピング、`js/*` 標準ライブラリ、ブラウザの import map など）は
独立した [JS バックエンド](./lang/js-backend) ドキュメントにまとまっているため、このページでは重複しません。

## エントリールール

- **main.no** — プログラムのエントリー
- **lib.no** — ライブラリのエントリー、関数をエクスポート（詳細は[エクスポートのドキュメント](lang/export)を参照）
- **tests/ ディレクトリ以下のすべての .no ファイル** — テストのアサーションを含む

## テスト

```bash
# 測試tests目錄下所有 .no 文件
no test

# 執行單個測試文件
no test my-test.no

# 使用指定編譯器或目標
no test -cc zig
no test -target x86_64-windows-gnu
```

テストの説明：

- テストファイルは tests/ ディレクトリにまとめて配置する
- 各テストファイルは独立してビルドされる
- いずれかのテストが失敗すると、非ゼロの終了コードを返す

## AST キャッシュ

`no build` / `no run` / `no test` は各ユーザーソースファイルを解析して得た AST をディスクに書き出します
（デフォルトは `~/.cache/nolang/ast`、`$NOLANG_AST_CACHE_DIR` でパスを変更できます）。次回内容が変わっていなければ直接再利用し、
字句解析 + 構文解析をスキップします。標準ライブラリはコンパイラに埋め込まれた blob を使用するため、このキャッシュは使いません。

**手動でのクリアは不要です。** キャッシュキーは

```
sha256( ASTVersion ‖ 解析期 salt ‖ 源文件內容 )
```

salt の 1 つが `checker.StdSigKey` です——これは `src/hash.noml` に列挙されたすべてのソースファイル
（標準ライブラリ + コンパイラ自身）のコンテンツ署名です。したがってコンパイラや標準ライブラリの変更は、`make` で再ビルドされた
`no` に新しいキーをもたらし、古いエントリは自然にミスして再利用されません。

| コマンド | 説明 |
| --- | --- |
| `no cache stat` | キャッシュディレクトリ、エントリ数、合計サイズ、現在の `ASTVersion` を表示 |
| `no cache clear` | すべてのエントリをクリア（緊急時のみ：エントリが古いと疑われる場合） |

環境変数：

| 変数 | 作用 |
| --- | --- |
| `NOLANG_AST_CACHE_OFF=1` | キャッシュをオフにする（コンパイラの A/B 比較や「修正したはずのエラーが再び出る」問題の調査に使用） |
| `NOLANG_AST_CACHE_DIR` | キャッシュディレクトリをカスタマイズ。**同時に**事前コンパイル済み AST 依存の `-ast.zip` キャッシュにも作用します（このキャッシュはデフォルトで `~/no/cache/ast` にあり、下記「事前コンパイル済み AST 依存」を参照） |
| `NOLANG_AST_CACHE_MAX` | キャッシュエントリ数の上限（デフォルト 4096） |

> 古いエントリは単にヒットしなくなるだけで、新しいエントリの書き込みに伴って上限で淘汰されるため、`~/.cache/nolang/ast` を**削除する必要はありません**。
> `no` は `make gen` / `make no` で再ビルドする必要があります。`cd src && go build` で自前コンパイルしたバイナリには
> `StdSigKey` が注入されていないため、キャッシュを黙って再利用するのではなく、エラーを出して終了します。

## Binary のインストールとアンインストール

### インストール

```bash
# 安裝當前目錄的包（本地構建）
no install
no install -u                                   # 強制重構（更新）

# 從 GitHub Release 安裝預編 binary（推薦）：<tool>[:<version>]@<host>/<owner>/<repo>
no install nogit@github.com/lizongying/notools            # 最新版
no install nogit:v0.1.33@github.com/lizongying/notools    # 指定版本
no install nogit@lizongying/notools                       # 省略 host，默認 github.com
no install -u nogit@github.com/lizongying/notools         # 強制重新下載
```

リモートインストールで取得するのは**事前コンパイル済みバイナリ**で、ソースコードではありません：アセット名は `<tool>-<os>-<arch>-<version>`（Windows では
`.exe` 付き）で、`<os>-<arch>` は `no` 自身が動作するプラットフォーム（例：`darwin-arm64`、
`linux-amd64`）から取得されます。そのためローカルの LLVM ツールチェーンは不要で、インストールされるツールが Nolang で書かれている必要もありません。
バージョンを省略すると `releases/latest` を参照します。`$GITHUB_TOKEN`（または `$GH_TOKEN`）を設定すると GitHub
API のレート制限を回避できます。

インストールの流れ：

1. リモート：spec を解決 → release アセットを特定 → ダウンロード；ローカル：カレントディレクトリでビルド
2. binary を `~/no/bin/` に書き込む
3. `/usr/local/bin/` にシンボリックリンクを作成

### アンインストール

```bash
no uninstall pkg-name
```

アンインストールは `/usr/local/bin/` のシンボリックリンクと `~/no/bin/` の binary を削除します。

## プロジェクト設定

### noml 設定フォーマット

Nolang のすべての設定ファイル（`package.noml`、`workspace.noml`、`.workspace.noml`、`nolang.lock.noml`、`sum.noml`）は独自開発の **noml**（Nolang Markup Language）フォーマットを採用しており、従来の JSONC に代わって使用されます。構文ルールは Nolang 言語自体に合わせています：

| ルール | 説明 |
| --- | --- |
| トップレベルオブジェクト | 1 つのオブジェクトです：`{` と `}` はそれぞれ独立した行に置き、メンバーは 1 行に 1 つ、**カンマ区切りなし**（改行が区切り） |
| キー | デフォルトは裸の識別子で、ルールは Nolang の変数名と同じ：小文字/数字/ハイフン（途中）/アンダースコアで始まり、数字では始まれません（例：`link-libs`、`anonymous-fn-type`、`dev-dependencies`）。ルールを満たさないキー（パス、URL、`@scope/pkg`）は単引用符の文字列キーとして記述します |
| 文字列値 | 単引用符、`\` でエスケープ：`\' \\ \n \t` |
| 真偽値 | `true` / `false`（すべて小文字、Nolang の bool リテラルと同じ） |
| 数値 | 裸のリテラル（ロックファイルなど用に予約） |
| 配列 | `[`、要素、`,`、`]` はいずれも複数行に渡って良い；要素は**カンマ**で区切り、**末尾カンマをサポート**；空配列は `[]` と書く |
| コメント | `;` から行末まで（文字列内は除く） |
| インデント | 意味なし（可読性にのみ影響し、整形の保証はしない） |

`dependencies` / `dev-dependencies` は**文字列配列**で、要素は `'キー:バージョン'` の形式（バージョンは省略可能、デフォルト `*`）です。例：

```noml
{
  dependencies: [
    '/example/test2',                          ; 本地包，默認任意版本
    'github.com/lizongying/nolang/test1:v0.1.0', ; 線上包，指定版本,
  ]
}
```

プロジェクトのルートディレクトリにある `package.noml` ファイルはプロジェクト情報を記述します：

```noml
{
  name: 'my-project'
  version: 'v0.1.0'
  description: 'A new Nolang project'
  keywords: []
  author: ''
  email: ''
  organization: ''
  repository: ''
  homepage: ''
  license: 'MIT'
  dependencies: [
    'fmt',,
  ]
  compiler: {
    version: 'v0.1.0'
  }
  ignore: []
}
```

`compiler.lang` はこのプロジェクトのメッセージ言語を固定します（`'zh-tw'`、`'ja'` など、書き方は `--lang` と同じ）。システムのロケールより優先されますが、
`--lang` と `NO_LANG` よりは優先度が低く、記述しない場合はシステムに従います。

### 依存関係の管理

```bash
# 添加依賴：<tool>[:<version>]@<host>/<owner>/<repo>
# 版本號可省略（取最新 release）；寫入 package.noml 的鍵是 <tool>@<host>/<owner>/<repo>
no add nogit@github.com/lizongying/notools
no add nogit:v0.1.33@github.com/lizongying/notools

# 移除依賴
no remove pkg-name

# 更新依賴
no update pkg-name

# 更新所有依賴
no update-all

# 列出依賴
no list

# 同步依賴（下載並生成鎖文件）
no sync
```

### 事前コンパイル済み AST 依存

`no add <tool>@<host>/<owner>/<repo>` のような依存関係は**AST のみでソースコードはありません**：コンパイル時に release から
`<tool>-ast.zip`（zip，每个模组一个 .gob 条目）を取得し、デコードして直接コンパイルパイプラインに注入します。使い方：

```nolang
# nogit                      # パッケージ別名; モジュールは nogit.<module>
# nogit/remote                  # モジュール remote を直接import (nogit.remote も可)
# nogit.fn                      # nogit はパッケージ名のみ；fn を定義するモジュールを探索
# <host>/<owner>/<repo>/nogit/remote
```

1 つの zip は 1 つのパッケージで、複数のモジュールを含められます（各モジュールは `<modulePath>.gob` エントリ）。入口モジュールはありません：`# nogit` はパッケージ別名を作るだけで、モジュールは `nogit.<module>` で参照します（例：`nogit.remote.greet(...)`）。`# nogit/remote` はモジュールを直接 import し、`# nogit.fn` は関数からモジュールを探します。`no fmt` は未使用の `# nogit` / `# nogit/remote` を削除します。バージョンは具体的な値である必要があり
（`no sync` / コンパイル時に `""` や `"*"` に遭遇するとエラーになります）。zip は `~/no/cache/ast/` にキャッシュされ、
`$NOLANG_AST_CACHE_DIR` で上書きできます。

成果物アセットの生成には `no pack` を使用します（字句解析器 + 構文解析器のみで、LLVM ツールチェーンは不要、CI に適しています）：

```bash
no pack -o nogit-ast.zip nogit.no      # 單個源文件
no pack -o nogit-ast.zip ./nogit       # 一個包目錄
```

### 依存関係の種類とバージョン番号ルール

`package.noml` の依存関係は**ローカルパッケージ**と**リモートパッケージ**の 2 種類に分かれます。コンパイラは以下のルールに基づいて依存関係の種類を自動判定し、ローカルパッケージが `"*"` バージョンを使用していない場合に警告を発します。

#### 判定ルール

```
依賴鍵 → 查找 workspace.noml（短名稱或完整鍵匹配）
  ├─ 找到 → 本地包（應使用 "*"）
  └─ 未找到 → 檢查路徑前綴（/ 開頭）
      ├─ 是 → 本地包（應使用 "*"）
      └─ 否 → 線上包（使用版本號，不警告）
```

1. **`workspace.noml` を検索**：まず依存キーを短縮名または完全キーとして `workspace.noml` 内で検索します。見つかればローカルパッケージです。
2. **パスの接頭辞を確認**：`workspace.noml` で見つからなかった場合、依存キーが `/`（ワークスペース相対パス）で始まっていれば、これもローカルパッケージです。
3. **それ以外はリモートパッケージ**：上記の条件を満たさない依存キーはリモートパッケージとみなされ、バージョン番号を指定する必要があります。

> **ワークスペース境界の制約**：`package.noml` 内のローカルパスは `/` で始まる必要があり（ワークスペース相対）、`./`、`../` や OS の絶対パスは禁止されています。ワークスペース境界を突破できる唯一の入口は `workspace.noml` / `.workspace.noml` のマッピングです。詳細は [ワークスペース境界の制約](#工作區邊界約束) を参照してください。

#### ローカルパッケージのさまざまな参照形式

ローカルパッケージは以下の 3 つの参照方法をサポートし、バージョン番号はいずれも `"*"` を使用する必要があります：

```noml
dependencies: [
  ; 1. 短名稱：workspace.noml 中註冊的鍵
  'test2',
  ; 2. 工作區相對路徑：以 / 開頭，相對工作區根目錄
  '/example/test2',
  ; 3. 完整 URL：若 workspace.noml 中有對應映射則為本地包
  'github.com/lizongying/nolang/test2',,
]
```

> **注意**：
> - 第 3 の形式がローカルパッケージかどうかは、`workspace.noml` に対応するマッピングが存在するかどうかで決まります。`workspace.noml` で `github.com/lizongying/nolang/test2` がローカルパスにマッピングされている場合は、その依存関係はローカルパッケージ（`"*"` を使用すべき）であり、そうでない場合はリモートパッケージ（バージョン番号を使用すべき）です。
> - `./` と `../` の接頭辞は `package.noml` では使用できなくなりました。すべてのローカルパスは `/` で始まる必要があります（ワークスペース相対）。

#### 高度な使い方：リモートパッケージからローカルパッケージへ

`workspace.noml` は短縮名を登録できるだけでなく、リモートパッケージ名をローカルパスにマッピングして、「リモートパッケージからローカルパッケージへ」の開発モードを実現できます：

```noml
; workspace.noml
{
  test2: '/example/test2'
  'github.com/lizongying/nolang/test2': '/example/test2'
}
```

このとき `package.noml` で `github.com/lizongying/nolang/test2` として参照されている依存関係はローカルパッケージ `/example/test2` として解決され、バージョン番号には `"*"` を使用する必要があります。これはリモートパッケージをローカルで開発する際に非常に便利です——`workspace.noml` にマッピングを追加するだけで、リモート依存をローカルソースに切り替えて連携デバッグできます。

#### バージョン番号の警告

コンパイル時にローカルパッケージが `"*"` 以外のバージョン番号（例：`"v0.1.0"`）を使用している検出すると、`"*"` の使用を促す警告を出力します。リモートパッケージにはこの制限はありません。

#### 再帰的なワークスペースマッピング（パッケージ間のマッピングチェーン）

Nolang は**再帰的なワークスペースマッピング**をサポートします。依存先のパッケージが独自の `workspace.noml` を持ち、その内部にもマッピングルールが存在し続けることで、自然なパッケージ間の解決チェーンを形成します。これは Nolang の核となる差別化能力です——主流の Go/Cargo の `replace`/`patch` は現在のプロジェクトにのみ有効で、依存パッケージの内部には伝播しません。

**適用シナリオ：**
- 基盤ライブラリ内でエイリアスを統一する
- インポートパスを標準化する
- 多層ライブラリの互換移行

**動作原理：**

依存キーを解決する際、コンパイラは次のように動作します。
1. 現在の `workspace.noml` でキーを検索する
2. マッピングが見つかったら、対象ディレクトリが独自の `workspace.noml` を持つか確認する
3. 持っていれば、同じキーに基づいて検索を続け、層ごとに解決する
4. これをこれ以上深いマッピングがなくなるまで繰り返す

**例：**

```
workspace/                  ← 頂層工作區
├── workspace.noml         ← {"testkey": "/pkgA"}
├── pkgA/
│   ├── workspace.noml     ← {"testkey": "/pkgB"}
│   └── pkgB/
│       ├── workspace.noml ← {"testkey": "/pkgC"}
│       └── pkgC/           ← 最終目標（無 workspace.noml）
```

`testkey` を解決する際：
1. 最上位の `workspace.noml` のマッピング → `/pkgA`（ワークスペース相対）
2. `pkgA/workspace.noml` のマッピング → `/pkgB`（pkgA ワークスペース相対）
3. `pkgB/workspace.noml` のマッピング → `/pkgC`（pkgB ワークスペース相対）
4. `pkgC` に `workspace.noml` なし → 最終的に `pkgC` に解決

**循環検出：**

コンパイラは解決済みのワークスペースルートディレクトリを追跡するアクセススタックを維持します。循環マッピング（例：A→B、B→A）を検出すると、直ちにエラーを出力して完全なチェーンを表示します：

```
Error: circular workspace mapping detected: /path/to/A → /path/to/B → /path/to/A
```

> **注意**：この機能はパッケージ間の自然な再帰マッピング（依存先パッケージが `workspace.noml` を自前で持つ場合）のみをサポートし、単一ファイルでの手動の連鎖的な省略表記はサポートしません。

### ミラー設定

`package.noml` の `mirrors` 配列にミラーアドレスを設定すると、リモートパッケージのダウンロードを高速化できます：

```noml
mirrors: [
  'https://mirror.example.com/',,
]
```

### ワークスペース設定 workspace.noml

`workspace.noml` はリポジトリのルートディレクトリに置かれ、**単一リポジトリ・複数パッケージ**のワークスペースを記述する「パッケージ名 → 相対パス」のマッピングです。`no build` は引数がなく `workspace.noml` が存在する場合、そこに列挙されたすべてのパッケージを並列コンパイルします。`no run` はパッケージ名（例：`no run foo`）でワークスペース内の単一のパッケージを実行します。

```noml
{
  foo: '/foo'
  bar: '/bar'
}
```

`no init` はワークスペースの定義を担当します：`workspace.noml` が存在しなければ空のオブジェクト `{}` を生成し、**`package.noml` は一切生成しません**。既に存在する場合は変更せず、上書きしません。その後 `no new <name>` でパッケージを作成すると、`"<name>": "./<name>"` が自動的に `workspace.noml` に登録されます。

#### ローカル専用設定 .workspace.noml

共通の `workspace.noml` に加えて、Nolang はローカル専用の設定ファイル `.workspace.noml`（先頭の `.` に注意）もサポートします。このファイルは `.gitignore` に追加することをお勧めします。開発者個人の臨時の依存関係差し替えに使用します。

**読み込みのロジック：**

1. まず共通設定 `workspace.noml` を読み込む
2. 次に専用設定 `.workspace.noml` を読み込む
3. **同じキー**：専用設定が共通設定を上書きする
4. **新しいキー**：互いに積み重なる

```
workspace/
├── workspace.noml       ← 工程共享配置（納入版本管理）
├── .workspace.noml      ← 本機私有配置（建議加入 .gitignore）
├── foo/
└── bar/
```

**使用シナリオ：**

- チーム共通のエイリアスや依存関係の転送ルールは `workspace.noml` に記述し、バージョン管理に含める
- 開発者がローカルでデバッグする際に一時的に依存をローカルの fork に向けたい場合は、`.workspace.noml` で上書きすればよい
- 一時的な fork マッピングでプロジェクトの共通設定を汚染するのを避け、大規模チームの協業と開発者のローカル開発の柔軟性を両立する

**例：**

```noml
; workspace.noml（公共，版本管理）
{
  foo: '/foo'
  bar: '/bar'
}

; .workspace.noml（私有，本地調試）
{
  ; 覆蓋公共配置：foo 指向本地 fork（工作區內）
  foo: '/my-fork-foo'
  ; 新增私有映射：指向工作區外部的 fork（OS 絕對路徑，唯一逃逸出口）
  'github.com/lizongying/nolang/core': '/home/lzy/code/fork/core'
  ; 新增私有映射（工作區內）
  experimental: '/experimental'
}
```

最終的に有効になるマッピング：`foo` → `/my-fork-foo`（専用設定が上書き）、`bar` → `/bar`（共通設定が保持）、`github.com/lizongying/nolang/core` → `/home/lzy/code/fork/core`（専用設定で追加、ワークスペース外へ脱出）、`experimental` → `/experimental`（専用設定で追加）。

### ワークスペース境界の制約

Nolang はパス参照に対して厳格なワークスペース境界の制御を実施し、プロジェクトの移植性と安全性を確保します。

#### ルール一覧

| 設定レイヤー | `/` ワークスペース相対 | OS 絶対パス | `./` `../` 相対ジャンプ |
| --- | --- | --- | --- |
| `package.noml` の依存キー | ✅ 許可 | ❌ 禁止 | ❌ 禁止 |
| `workspace.noml` のマッピング値 | ✅ 許可 | ✅ 許可（脱出出口） | ✅ 許可（脱出出口） |
| `.workspace.noml` のマッピング値 | ✅ 許可 | ✅ 許可（脱出出口） | ✅ 許可（脱出出口） |
| ソースコードの `#` インポートパス | ✅ 許可 | ❌ 禁止 | ❌ 禁止 |

#### 1. package.noml（厳格に制限、ワークスペース外への脱出は禁止）

`package.noml` はプロジェクトの対外リリース、CI、複数人での協業における標準のベースライン設定で、コードリポジトリにコミットされます。

```noml
#{fmt-skip}
dependencies: [
  ; ✅ 合法：以 / 開頭，相對於 workspace 根
  '/vendor/test',
  ; ❌ 禁止：作業系統絕對路徑 /home/xxx/...
  ; ❌ 禁止：./ ../ 相對跳轉
]
```
```

**制約：**
- ローカルパスは `/xxx` のみ記述可能、基準 = workspace ルートディレクトリ
- OS の絶対パスは受け付けない
- `./`、`../` はサポートしない
- 作業リポジトリ外のディレクトリを直接参照することはできない

**目的：** 外部パスを許可すると、「自分のマシンではコンパイルできるが他人では失敗する」事態が起きやすく、見知らぬ外部コードを誤って読み込むセキュリティ上の危険も生じます。

#### 2. workspace.noml + .workspace.noml（唯一の脱出出口）

マッピング先は 3 種類のローカルパスをサポートします：
- `/xxx` → workspace ルート内部のディレクトリ（package.noml と意味は同じ）
- `./xxx`、`../xxx` → workspace ルートディレクトリを基準に解決し、ワークスペース境界を越えることを許可
- OS の絶対パス → ワークスペース外を指すことを許可（fork、外部コンポーネント）

```noml
; .workspace.noml 私有配置，用於本地開發
{
  ; 工作區內部
  foo: '/vendor/foo'
  ; 相對跳轉：指向工作區外部的 fork
  'github.com/lizongying/nolang/core': '../fork/core'
  ; OS 絕對路徑：指向任意外部目錄
  'github.com/lizongying/nolang/utils': '/home/lzy/code/utils'
}
```

**重要なロジック：** `package.noml` は外部コードを直接参照する能力を持ちません。ユーザーがリポジトリ外のソースをマウントしたい場合は、workspace 設定で明示的にマッピングする必要があります。これは「ディレクトリを越えた読み込み」に明確で可視なスイッチを追加し、すべての外部依存を一箇所で集中管理することに相当します。

#### 3. ソースコードのインポート層における関連制約

ソースコード内では OS の絶対パスによるインポートを直接記述できません。すべての外部コード参照は、workspace エイリアスまたはリモートパッケージ識別子を介して間接的に取り込む必要があります。`./` と `../` の相対ジャンプも同様に禁止されています。

```no
; ✅ 合法：工作區相對路徑
# /vendor/utils.helper
; ✅ 合法：遠端包標識
# github.com/lizongying/nolang/core.process
; ✅ 合法：標準庫
# std/fs
```

```no
#{fmt-skip}
; ❌ 禁止：相對跳轉
; # ./utils.helper
; # ../lib/helper.process

; ❌ 禁止：作業系統絕對路徑
; # /home/user/code/core.process
```

#### データフローのデモ

シナリオ：業務プロジェクトの workspace ルートが `/code/project` にあり、外部の `/code/fork/core` を使用したい場合

❌ これはできません（package.noml で禁止）：

```noml
; package.noml
dependencies: [
  '/code/fork/core', ; 非法：無法逃逸工作區,
]
```

✅ 標準的な方法（workspace マッピングで境界を突破）：

```noml
; .workspace.noml
{
  'github.com/lizongying/nolang/core': '/code/fork/core'
}
```

```no
; 源碼正常導入
# github.com/lizongying/nolang/core
```

#### ワークスペースの流れ

コンパイルと実行のエントリーディレクトリは常に**ワークスペースディレクトリ**（`workspace.noml` があるディレクトリ）です。全体の流れは次のとおりです：

1. ユーザーがワークスペースディレクトリで `no build` または `no run <パッケージ名>` を実行する
2. コンパイラが `workspace.noml` を読み込み、パッケージ名に対応するサブディレクトリパスを検索する
3. そのサブディレクトリの `package.noml`（つまりパッケージルートディレクトリ）を読み込み、パッケージルートを基準にビルド/実行する
4. すべてのインポートパス（`use /path/to/module`）は**ワークスペースルートディレクトリを基準に解決**され（[パス解決の取り決め](path-resolution.md)を参照）、つまり `workspace.noml` があるディレクトリが唯一の基準となり、インポート元のパッケージには依存しません。

```
workspace/               ← 工作區目錄（workspace.noml 所在此處）
├── workspace.noml      ← 包名 → 路徑映射
├── foo/                 ← 包 foo
│   ├── package.noml        ← foo 的包根目錄
│   ├── main.no
│   └── lib.no
└── bar/                 ← 包 bar
    ├── package.noml        ← bar 的包根目錄
    └── main.no
```

> **注意**：インポートパスは**ワークスペースルートディレクトリ**（`workspace.noml` があるディレクトリ）を基準とし、パッケージの `package.noml` ディレクトリではありません。パッケージのサブディレクトリに入れ子の `package.noml` が存在する場合、`LoadPackage` は上位へ検索して最も近い `package.noml` を見つけてこれをパッケージルートとしますが、インポートの解決は統一してワークスペースルートを基準に行います。

---

## 応用事例

- [notools](https://github.com/lizongying/notools) — Nolang 言語で実装された Unix の一般的なコマンドラインツール群で、cat、ls、grep、wc、head、tail などの古典的なツールを網羅し、システムプログラミング分野における Nolang の実用的な応用能力を示しています。
