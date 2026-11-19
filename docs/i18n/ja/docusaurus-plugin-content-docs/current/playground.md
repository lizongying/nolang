---
sidebar_position: 5
---

# Playground

Nolang Playground は、ブラウザ上で直接 Nolang プログラムを編集、コンパイル、実行できるオンライン環境です。Nolang コンパイラ（`no.wasm`）と言語サーバー（`lsp.wasm`）を WebAssembly にコンパイルし、WASI を通じてブラウザのサンドボックス内でユーザーコードを実行するため、ローカルにツールチェーンをインストールする必要はありません。

今すぐこちらへ：[**/playground**](/playground)

## 使い方

Playground の画面は左右 2 つのペインに分かれています：

- **左側 Editor**：[CodeMirror 6](https://codemirror.net/) ベースのコードエディタで、Nolang の構文ハイライト、自動インデント、履歴をサポートします。
- **右側 Output / stderr / Diagnostics**：実行結果、標準エラー出力、診断メッセージ。

インターフェースは Docusaurus のロケールに従って簡体字中国語と英語を自動的に切り替え、出力ペインはライトテーマとダークテーマの両方をサポートします。

### ツールバー

| ボタン      | 機能                                                              |
| ----------- | ----------------------------------------------------------------- |
| **▶ Run** | 現在のコードを WASM にコンパイルし、WASI 経由で実行します。stdout は右側の Output に表示されます。 |
| **Format**| LSP 経由で `no fmt` を呼び出し、現在のコードを整形します。                          |
| **Examples** | カテゴリ別に組み込み例を読み込み、エディタの現在の内容を上書きします。                        |

ツールバーの右側には現在の状態（アイドル／コンパイル中／実行中／完了／エラー）が表示され、ドロップダウンメニューの下にはその例の中国語と英語のバイリンガル説明が表示されます。最後に選択した例はブラウザのローカルストレージに保存されます。

### 例

Examples ドロップダウンメニューには、テーマ別にグループ化された **24 個**の例があらかじめ収録されており、すべてブラウザとまったく同じ `--wasm-direct` コンパイルパスで CI 内で検証されています：

| カテゴリ | 数量 | 内容 |
| -------- | ---- | ---- |
| 基礎 | 7 | Hello World、変数推論、関数と再帰、Fibonacci、算術とビット演算、整形出力、文字列 |
| フロー制御 | 6 | 短絡ガード、パターンマッチング、FizzBuzz、4 種類の範囲ループ、条件付き while ループ、break / continue |
| データ構造 | 4 | 固定長配列、動的スライス、構造体、メソッド |
| スカラ型 | 4 | char、bool、f64、byte |
| 標準ライブラリ | 2 | math.max / math.min / sqrt、rand.rand |
| エラー処理 | 1 | option と nil の 3 分岐 match |

各例には安定した期待出力文字列が付属しています。CI は各例をコンパイルし、`wasmtime` で実行して、その文字列が stdout に出現することを検証します。そのため、言語や Direct-WASM バックエンドが進化しても、例がこっそり失敗することはありません。

いずれかの例を選択すると、Playground は現在の Output、stderr、Diagnostics をクリアし、エディタの内容をその例の内容で置き換えます。

### 診断

LSP が構文エラーや型エラーを検出すると、右下の Diagnostics 領域に一覧表示されます。いずれかの診断項目をクリックすると、対応する行と列にジャンプできます。コンパイラの stderr に含まれる `file:line:col` メッセージは別途解析され、**Run Errors** マーカーに変換されます。また、末尾の TraceID とローカライズされた重大度プレフィックスは自動的に除去されます。

### エラーとタイムアウト

以前は WASM の読み込み失敗が `console.warn` に書き込まれるだけで、ユーザーには反応しない Run ボタンが表示されていました。現在の動作は次のとおりです：

- `no.wasm` または `lsp.wasm` のダウンロード／コンパイルに失敗すると、ページ上部に閉じ可能なエラーバナーが表示され、どのリソースでどのような理由で失敗したかが示されます。
- 言語サーバーの初期化失敗時も同様にバナーが表示されます。エディタは引き続き使用できますが、補完、ホバー、整形は利用できません。
- 各実行はメインスレッドのウォッチドッグによって計時されます（デフォルト 8 秒）。`wasi.start()` は worker 内で同期呼び出しされるため、無限ループすると worker 内のどのタイマーも発火せず、唯一有効な中断方法はメインスレッドから worker を終了させることです。タイムアウト後、worker は終了され、次回実行時に自動的に再構築されると同時に、WASM の再ウォームアップも直ちに行われます。
- Run の重複クリックは並行ガードによってブロックされ、保留中の編集に対するデバウンスタイマーは例の切り替えや整形時にキャンセルされます。これにより、古い入力が新しい内容を上書きするのを防ぎます。

`no.wasm` と `lsp.wasm` はページがアイドル時にあらかじめダウンロードされ、ストリーミング方式でコンパイルされます。そのため、最初に Run を押すときに 27 MB 全体のダウンロードとコンパイルを待つ必要はありません。

## 制限

Playground はブラウザの WASM サンドボックス内で実行されるため、一部の Nolang 機能は使用できません：

### FFI 非対応

ブラウザはネイティブの C ライブラリをリンクできないため、`#{c}`、`#{cpp}`、`#{rust}` などの FFI 宣言は Playground では使用できません。FFI を含むコード（`sqlite`、`mysql` ドライバなど）は実行できません。

### fork / exec / pipe 非対応

WASI preview1 はプロセス管理 API を提供しないため、`process` 標準ライブラリ（`process.start`、`process.wait` など）は Playground では利用できません。

### ネットワーク socket は利用不可

WASI preview1 は socket API を提供しないため、以下の標準ライブラリは Playground では使用できません：

- `net`（TCP リスナー / conn）
- `http` / `http2` / `http3`（HTTP クライアント）
- `ws`（WebSocket）
- `tls`（TLS 接続）
- `quic`、`sse`、`dns`

**`no` / `lsp` 自体に対する制約（標準ライブラリより厳格）**：wasm 成果物は socket 関数を「インポート」することすらできません。
ブラウザ側では `@wasmer/wasi` でインスタンス化しますが、これは `sock_accept` / `sock_shutdown` を実装していません。
そのため、`net` / `net/http` によってリンクされてこれら 2 つの `wasi_snapshot_preview1` インポートが追加されると、
`WebAssembly.Instance` は直接例外をスローします：

```
Failed to instantiate WASI: RuntimeError: JsValue(LinkError: WebAssembly.Instance():
Import #28 "wasi_snapshot_preview1" "sock_accept": function import requires a callable)
```

結果として、プログラム全体が `_start` にすら到達できません。言語サーバーは「初期化失敗」を報告し（補完、ホバー、整形はすべて使用不可）、
実行ボタンも同様です。そのため、ネイティブ環境でのみ意味のあるネットワークコードはすべて `!wasip1` のファイルに置き、
wasip1 側にはエラーを返すスタブだけを残しています：`cmd/lsp/pprof_wasip1.go`、`cmd/no/serve_esm_wasip1.go`、
`package/download_net_wasip1.go`、`package/release_net_wasip1.go`。
`bun scripts/wasm_imports_check.ts`（`make wasm-imports-check`）は、エンジン自身のインポートテーブルでこの線を守ります。

### `poll_oneoff` はフロントエンドで補う

インポートテーブルがきれいなのは「関数が存在する」ことを意味するだけで、「関数がクラッシュしない」ことを意味するわけではありません。`@wasmer/wasi` の `poll_oneoff` の本体は
Rust の `unimplemented!()` です。呼び出されると（0 件の購読でも同様に）、`unreachable` を実行してインスタンス全体を
trap させます。エラーは次のような内容です：

```
Error while running start function: RuntimeError: JsValue(RuntimeError: unreachable)
```

Go の `runtime` は `time.Sleep` と `netpoll(delay)` の両方を単一の単調クロック購読に変換するため、
**すべての Go プログラムがこの問題に直面します**。`no fmt` / `no vet` / `no build` はすべて最初に睡眠した時点で死に、
インポートテーブルのチェックではこれをまったく検出できません。

修正は `docs/src/playground/wasm-loader.ts` にあります。`instantiateWasi()` が自ら `wasi.getImports()` を使って
インポートオブジェクトを構築し、`poll_oneoff` を JS 実装に置き換えます（クロック購読 → 最も近い満了時刻まで睡眠して clock イベントを報告、
fd 購読のみ → 直ちに準備完了を報告し、永久ブロックを回避）。オーバーライドを
`wasi.instantiate(module, overrides)` に渡すことは**できません**。グルー自身の関数が優先され、オーバーライドは黙って無視され、プログラムは同じく trap します。
また、インポートオブジェクトを自前で構築したため、`wasi.start(instance)` にはインスタンスを明示的に渡す必要があります。

### 初回実行時のコンパイル時間

Run は `no build --wasm-direct` を使って wasm を生成します。実測では wasm 内の hello world は約 **0.5 秒**
（ネイティブの `no` で同じコマンドだと約 0.6 秒。どちらも 2026-11 の std インデックスの事前ベイク後であり、以前はそれぞれ
約 55 秒と 11 秒でした）。

元々のボトルネックは codegen ではなく、**コールドスタートごとに std 全体を 2 回解析していたこと**でした。`validateProgram`
の `ValidateUndefinedVars` は `math.max(...)` のようなモジュール修飾呼び出しを認識するために、すべての 119 個の
std モジュールからエクスポートシンボルを収集し、`CollectStdUnionAliases` がさらに同じ一群のモジュールを個別に解析して union エイリアスを探していました。
現在は `genstdsig` が「各モジュールのエクスポートシンボル」と「どのモジュールに union エイリアスがあるか」をまとめて
`stdsig_gen.go` に焼き込み、実行時に直接テーブルを参照します。さらに 2 つのガード（`hir.TestModuleExportsMatchChecker`
はモジュールごと・フィールドごとに HIR の現在の計算結果と比較し、`TestUnionAliasNarrowingMatchesFullScan` は狭いスキャンと
全量スキャンを比較します）。テーブルが古くなった場合は、黙って意味を変更するのではなくテストが失敗します。

ウォッチドッグは「読み込み + コンパイル 180 秒 / ユーザープログラム 5 秒」の 2 段階の予算を引き続き保持しています。前者には現在大きな余裕がありますが、
コールドスタート、大きなプログラム、遅いデバイスによるロングテールを吸収するために残しています。

### ファイルシステムは仮想

WASI preview1 は stdin / stdout / stderr の 3 つのファイルディスクリプタのみを提供し、実際のファイルシステムはありません。そのため、`fs`、`path`、`os.get-env`、`os.set-env` などファイルや環境に関わる標準ライブラリは、Playground では期待どおりに動作しない可能性があります。

### メモリ上限

ブラウザの WASM は通常 32 ビットのアドレス空間に制限され、利用可能なメモリの上限は約 **2 GB ～ 4 GB** です（ブラウザとオペレーティングシステムによって異なります）。大量のメモリを必要とするプログラム（深い再帰、大きな配列など）は、out-of-memory で失敗する可能性があります。

## ブラウザの互換性

Playground には WebAssembly をサポートするモダンなブラウザが必要です。以下のバージョン以降の使用を推奨します：

| ブラウザ    | 最低バージョン |
| ----------- | -------- |
| Chrome    | 100+     |
| Firefox   | 100+     |
| Safari    | 16+      |
| Edge      | 100+     |

ブラウザが WebAssembly をサポートしていない場合、または JavaScript が有効になっていない場合、Playground はエディタを読み込めず、コードも実行できません。

## 開発と検証

Playground を変更した後、クリーンなビルドツリーで次のチェックを実行できます：

```bash
make playground-demos                       # 編譯 + 在行程內用 playground 自己的 WASI loader 執行全部範例
make playground-demos DEMO_RUN=wasmtime     # 改用外部 wasmtime 執行（交叉比對用）
make playground-demos DEMO_RUN=none         # 只編譯、不驗輸出（不可當發佈門檻）
make wasm-imports-check                     # 重建兩個 wasm 並驗證導入表無 socket 函式
make wasm-runtime-smoke                     # 用 worker 的同一條載入路徑實跑 version/fmt/vet
RUN_WASM_LSP=1 RUN_WASM_BUILD=1 make wasm-runtime-smoke   # 追加語言服務與完整編譯+執行
bun scripts/playground_ui_check.ts          # 診斷解析、看門狗、worker 恢復、i18n
make playground-smoke                       # dev server 與 WASM 路由 HTTP 200
cd docs && bun run typecheck                # TypeScript 型別檢查
```

`playground-demos` のコンパイラは**ネイティブ**の `no` ですが、成果物はブラウザと同じ `--wasm-direct -target wasm32-wasi` のパスを通り、
実行には `wasi-glue` + `wasm-loader` の `instantiateWasi()` を使用します（`runner.worker` と完全に同一のコードです）。
そのため wasmtime のインストールは不要で、CI で直接実行できます。同時に 3 点を検証します：スニペットがコンパイルできること、成果物が WebAssembly 検証を通過すること
（フィールド値のビット match のような空スタックエラーはここで露呈）、出力が `expect` に一致すること（`?=` が黙って 0 になるといったバックエンドの偏差もここで露呈）。

ブロックできないのは 1 点だけです。`no.wasm` / `lsp.wasm` 自身のインポートテーブルです（これら 2 つの成果物はスニペットから生成されません）。
これは `wasm-imports-check` が補います。`@wasmer/wasi` に `sock_*` が欠けていると `WebAssembly.Instance` は直接 `LinkError` となり、
playground 全体（言語サービス含む）が起動すらできません。worker 側にはもう 1 つの安全網があります。`wasm-loader.ts` の
`instantiateWasi()` が `@wasmer/wasi` の未提供の WASI 関数を、`ENOSYS` / `NOTSUP` を返すスタブに補完し、
予期しないインポートがサービス全体のダウンではなく、失敗した 1 回のシステムコールに退化するようにします。

`wasm-imports-check` では「関数は存在するが本体が trap する」（上記 `poll_oneoff` の種類）もブロックできません。
そのため `make wasm-runtime-smoke` が最後の 1 マスを補います。これは `instantiateWasi()` の実際のパスを通して
`no version` / `no fmt` / `no vet` を実行し、さらに言語サービスと完全なコンパイル + 実行も検証できます。

`docs/static/wasm/*.wasm` は `.gitignore` で除外されており、ローカルの成果物をコミットしてはいけません。`no.wasm` と `lsp.wasm` は CI によって同じソースコードと同じ `StdSigKey` から一緒に再ビルドされます。これにより、ブラウザ内のコンパイラ、埋め込み済みの std AST blob、Direct-WASM バックエンドが同じビルドに由来することが保証されます。この 3 つのバージョンの不一致は、過去に `unreachable` や検証エラーが発生する一般的な原因でした。

CI（`.github/workflows/build.yml` の `pr-verify`、`static.yml` のサイトビルド）は、Go テスト、型チェック、UI チェック、例の検証を順番に実行してから、クリーンなツリーで WASM を再ビルドし、サイトをデプロイします。
