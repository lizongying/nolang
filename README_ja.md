# Nolang

[繁體中文](README.md) · [简体中文](README_zh-CN.md) · [한국어](README_ko.md) · [日本語](README_ja.md) · [English](README_en.md) · [Русский](README_ru.md) · [Français](README_fr.md) · [Español](README_es.md) · [Deutsch](README_de.md) · [Português](README_pt.md)

Nolang は、メモリセーフで高い性能、極めて簡潔な構文を備えたシステムプログラミング言語です。

## 主な特徴

- **メモリ安全性**: 自動メモリ管理
- **高い性能**: コンパイル時最適化、GC なし
- **極めて簡潔な構文**: よりシンプルな構文
- **意図駆動**: 開発者の意図を十分に尊重し、意味が人間の直観に沿う
- **決してクラッシュしない**: 暗黙のクラッシュ経路を排除

## インストール

ワンコマンドでインストール（macOS / Linux / Windows Git Bash）。最新の `no` を `$HOME/.local/bin` にダウンロードします：

```bash
curl -fsSL https://raw.githubusercontent.com/lizongying/nolang/main/install.sh | sh
```

デフォルトは**LLVM 内蔵**版（`no-llvm_*`、LLVM / opt / llc の別途インストール不要）です。サイズの小さい純 Go 版を使う場合は `NO_FLAVOR=plain` を指定し、`no build -cc zig` と組み合わせてください：

```bash
curl -fsSL https://raw.githubusercontent.com/lizongying/nolang/main/install.sh | NO_FLAVOR=plain sh
```

バージョンやインストール先を固定する場合（環境変数を `sh` に渡すため、一度ファイルに保存してから実行します）：

```bash
curl -fsSL https://raw.githubusercontent.com/lizongying/nolang/main/install.sh -o install.sh
NO_VERSION=0.4.28 NO_INSTALL_DIR="$HOME/bin" sh install.sh
```

| 環境変数          | 既定値               | 説明                                                                     |
| ----------------- | -------------------- | ------------------------------------------------------------------------ |
| `NO_VERSION`      | `latest`             | バージョン（`0.4.28` または `v0.4.28`）                                 |
| `NO_FLAVOR`       | `embedded`           | `embedded` は LLVM 内蔵、`plain` は純 Go（システム LLVM が必要）         |
| `NO_INSTALL_DIR`  | `$HOME/.local/bin`   | インストール先                                                           |
| `NO_REPO`         | `lizongying/nolang`  | 取得元リポジトリ                                                         |

インストール後は `no version` で確認できます。`$HOME/.local/bin` が `PATH` にない場合は追加してください：

```bash
export PATH="$HOME/.local/bin:$PATH"
```

> `.no` プログラムのコンパイルには C リンカも必要です。`no build -cc zig`（zig に clang / lld / libc が同梱）を推奨します。clang を別途インストールしても構いません。

## Usage

ドキュメント

[docs](https://lizongying.github.io/nolang/)

VSCode プラグイン

[vscode](https://marketplace.visualstudio.com/items?itemName=lizongying.vscode-nolang)

nolang CLI

[nolang](https://github.com/lizongying/nolang/releases/latest)

nohub パッケージ一覧（コミュニティパッケージ検索）

[nohub](https://lizongying.github.io/nohub/)

### コミュニティパッケージ検索（nohub）

`no` は nohub のパッケージインデックスを直接読み取り、3 つの読み取り専用コマンドを提供します：

```bash
no search <query>    # 名前・キーワード・カテゴリで検索
no info <pkg>        # パッケージ情報とインストール/取り込みコマンドを表示
no docs <pkg>        # ドキュメント URL を表示（docs → homepage → repository）
```

既定では公共インデックスを読み込みます。複数の nohub インスタンスを重ねるには `~/no/sources.noml` にソース一覧を設定します（配列順が優先順位）：

```
{
  sources: [
    'https://lizongying.github.io/nohub',
  ]
}
```

`NOHUB_SOURCES`（カンマ区切り）で一時的に上書きできます。三つのコマンドは `-source <name>` で特定のソースだけを参照でき、`no search` の出力には `SOURCE` 列が入り、同名同バージョンは先のソースが優先されます。取得に失敗したソースは警告のみで他に影響しません。`no` は各ソースの `index.noml`（`<base>/index.noml`、検索ページの最初に読み込むものと同じく各パッケージの最新バージョンを 1 件ずつ保持）だけを fetch し、tarball も `data.js` も読みません。フィールドの対応は `web/app.js` に揃えています。[nohub 検索ページ](https://lizongying.github.io/nohub/) でも同じデータを閲覧できます。


### CLI コマンド

| コマンド                                                     | 説明                                                                              |
| ------------------------------------------------------------ | --------------------------------------------------------------------------------- |
| `no init`                                                    | ワークスペースを定義する (workspace.jsonc を生成し、package.jsonc は含めない)       |
| `no new <name>`                                              | ワークスペース内に新しいパッケージを作成する (サブディレクトリ + package.jsonc、workspace.jsonc に登録) |
| `no fmt [-w] [-d] <file\|dir>`                               | ソースコードをフォーマットする                                                     |
| `no build [-o <file>] [-cc <s>] [-target <s>] [<file\|dir>]` | ビルドする (executable を出力)                                                     |
| `no run [-cc <s>] [-target <s>] [<package\|dir\|file>]`        | ビルドして実行する (パッケージ名/ディレクトリ/ファイル)                             |
| `no test [-cc <s>] [-target <s>] [<file\|dir>]`              | テストを実行する                                                                  |
| `no add <pkg>`                                               | 依存関係を追加する                                                                |
| `no remove <pkg>`                                            | 依存関係を削除する                                                                |
| `no update <pkg>`                                            | 依存関係を更新する                                                                |
| `no update-all`                                              | すべての依存関係を更新する                                                         |
| `no list`                                                    | 依存関係を一覧表示する                                                             |
| `no search <query>`                                            | 名前・キーワード・カテゴリでコミュニティパッケージを検索 |
| `no info <pkg>`                                                | パッケージの情報とインストール/取り込みコマンドを表示 |
| `no docs <pkg>`                                                | パッケージのドキュメント URL を表示 |
| `no sync`                                                    | 依存関係を同期する                                                                |
| `no install [-u] [<tool>[:<version>]@<host>/<owner>/<repo>]` | binary をインストールする (~/no/bin/ + シンボリックリンク)                          |
| `no uninstall <name>`                                        | binary とシンボリックリンクを削除する                                               |
| `no pub --token <token> [--registry <url>]`                  | registry に公開する                                                                |

```bash
# ビルド (既定では main.no から開始)
no build                    # 現在のディレクトリをビルド
no build main.no            # 指定したファイルをビルド
no build -o output          # 出力先を指定
no build -cc zig            # Zig コンパイラを使用
no build -target x86_64-linux-gnu  # クロスコンパイル (対象プラットフォームを指定)

# 実行 (ビルド + 実行)
no run                    # 現在のディレクトリの main.no をビルドして実行
no run foo                # ワークスペース内の foo という名前のパッケージを実行
no run ./foo              # ./foo ディレクトリの main.no を実行
no run -cc zig
no run -target aarch64-macos-gnu

# テスト (ビルド + 実行)
no test                   # `test` ディレクトリ配下のすべての .no ファイルをテスト
no test test/my-test.no        # 単一のテストファイルを実行
no test -cc zig
no test -target x86_64-windows-gnu
```

### 新しいプロジェクトを作成する

```shell
no new test1

cd test1

no run
```

### Hello World

新しいプロジェクトを作成したら、`main.no` を編集します:

```no
print('Hello, World!')
```

実行:

```bash
no run
```

出力:

```
Hello, World!
```

### エントリーポイントの規則

- **main.no** — プログラムのエントリーポイント
- **lib.no** — ライブラリのエントリーポイント、関数をエクスポート
- **tests/ ディレクトリ配下のすべての .no ファイル** — テストのアサーションを含む

### テストの説明

- テストファイルは tests/ ディレクトリにまとめて配置する
- 各テストファイルは個別にビルドする
- いずれかのテストが失敗すると、ゼロ以外の終了コードを返す

```bash
no vet src/std                     # Nolang vet で標準ライブラリを検査
```

## ソースからビルドする

Nolang のビルドには `make` を使用してください:

```bash
make            # すべてのターゲットをビルド (bin/no と vscode-nolang/server/lsp)
make no         # bin/no をビルド
make lsp        # LSP server をビルド
make package    # LSP をビルドし VSCode 拡張をパッケージ化
make no-wasm    # no を WebAssembly にクロスコンパイル
make lsp-wasm   # LSP を WebAssembly にクロスコンパイル
make playground # no.wasm + lsp.wasm + Docusaurus サイトをビルド
make clean      # ビルド成果物を消去
make help       # すべてのビルドターゲットを表示
```

> ⚠️ **Nolang のビルドには必ず `make` を使用し、`cd src && go build` で直接ビルドしないでください。**
>
> `make` はビルド前に自動的に `make gen` を実行し、`src/checker/stdsig_gen.go` を再生成します。
> —— これは**バイナリに埋め込まれた**標準ライブラリのシグニチャ表です (`funcSigs` / `methodSigs` /
> `funcParams` / `methodParams` / `structFields` / `aliases` / `structMod` /
> `enumVariants` / `embeddedStdModules`)。これにより、毎回の `no build` で「すべての std をフル解析」する PASS1 を
> スキップでき、起動が明らかに速くなります。
>
> `src/std` を変更した後、**先に `make gen` を実行せずに**いきなり `go build` すると、コンパイラは
> 埋め込みシグニチャ表と `src/std` の内容の不一致を検出し、**即座にエラーを出してビルドを拒否します**
> (`make no` の実行を促します)。これは意図的な設計です。以前は黙ってフル解析に戻るため、
> 毎回のコンパイルが遅くなり、気づきにくいという問題がありました。`src/std` を変更したら必ず `make no` を
> 実行してください (先に `make gen` を実行してからビルドします)。
>
> テストの実行にも `make test` を使用してください: **単に `go test` を実行するとシグニチャ key が注入されず (`StdSigKey` が空)、エラーとなり拒否されます**。`make test` はテストバイナリをビルドする際に同じ key を注入し、埋め込みシグニチャ表の高速パスを通るようにします。

---

## 用語集 (glossary)

`glossary/` は**項目ごとに整理された Nolang のナレッジベース**です: 各名詞につき、種類、定義、構文、
例、関連参照、一行要約を備えたレコードが 1 件ずつあります。全 **244 件 / 10 の意味領域**(種類、
構文、注釈、メモリ、エラー、モジュール、コンパイラ内部、ツールチェーン、std 全モジュール、既知の落とし穴)で、
データ自体は `.noml` 形式です。

フロントエンドは Nolang で書かれており、CLI と MCP (stdio / HTTP) の両方を提供するため、人間と AI のどちらも検索できます:

```bash
no build glossary                        # 成果物: glossary/dist/glossary

./glossary/dist/glossary get option      # 単一の項目の全文を取得
./glossary/dist/glossary search 三態      # キーワード検索
./glossary/dist/glossary list --domain=traps   # 既知の落とし穴の一覧
./glossary/dist/glossary check           # 整合性チェック (CI 用)

./glossary/dist/glossary mcp             # MCP server (stdio)
./glossary/dist/glossary mcp --http=127.0.0.1:8765
```

出力は `--format=text|md|json` で切り替えられます。連携方法とデータ形式の説明は [`glossary/README.md`](glossary/README.md) を参照してください。

nohub で glossary パッケージを閲覧することもできます: [glossary on nohub](https://lizongying.github.io/nohub/?package=glossary:latest@github.com/lizongying/nolang)

---

## 関連プロジェクト

- [nohub — Nolang エコシステムのコミュニティパッケージ一覧検索](https://lizongying.github.io/nohub/)

  パッケージ名・キーワード・カテゴリで収録済みのコミュニティパッケージを検索し、パッケージ情報、バージョンスナップショット、ソースへの入口を確認できます。インデックスは `web/build.mjs` が `packages/` のスナップショットから生成します。

- [noui — GPUI の精神を Nolang で書き直した UI ツールキット（nohub パッケージ）](https://lizongying.github.io/nohub/?package=noui:latest@github.com/lizongying/nolang)

- [Benchmarks Game — Nolang vs C vs Rust 性能比較レポート](https://github.com/lizongying/no-benchmarks/results/report.md)

- [notools — Nolang で実装した Unix コマンドラインツール群](https://github.com/lizongying/notools)

  Nolang の適用事例: Nolang 言語で実装した Unix の一般的なコマンドラインツール群で、cat、ls、grep、wc、head、tail などの古典的なツールを網羅し、システムプログラミング分野における Nolang の実践的な応用能力を示しています。

---

## TODO

## ライセンス

Apache-2.0
