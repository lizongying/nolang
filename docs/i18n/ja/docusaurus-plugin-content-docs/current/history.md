---
sidebar_position: 2
---

# 更新履歴

このページには、Nolang の各バージョンのリリース記録を収録しています。リポジトリのルートディレクトリにある `HISTORY.md` と同じ内容です。

## v0.4.27

- refactor(async): remove the deprecated run keyword and awy handle form, allowing co only inside awy coroutine groups
- feat(coro): implement stackless coroutines with heap frames, state dispatch and a cooperative FIFO scheduler
- feat(chan): add blocking channel recv with empty-read park and %task waiter chaining for deterministic wakeup
- feat(async): support co procedure and closure spawn with by-value capture, rejecting callees with declared results
- fix(coro): guard nested depth, join sets and channel/drain livelock in the coroutine runtime
- test(coro): add coroutine, channel, join and ownership regressions with deterministic output assertions
- fix(i18n): skip the filesystem root in WASI catalog probing so the playground falls back to embedded catalogs
- docs(playground): add a Hello World snippet and raise the LSP bridge request timeout to 60 seconds
- fix(docs): quote bare braces in the previous changelog and lint future entries against MDX-breaking braces

## v0.4.26

- perf(checker): bake std union-alias locations and per-module exports into stdsig so cold builds skip parsing all 119 std modules (native 11.5s -> 0.6s, wasm heap ~1GB -> ~43MB), with self-healing fallback to full scans on drift
- perf(parser): collapse three full-tree reflection walks in local-type inference into one cached per-function scope index, removing O(vars x nodes) rebuilds
- fix(mir): exclude option operands from the borrowed-parameter struct-clone path so options no longer get their `{tag,data}` header memcpy'd as a struct, causing SIGSEGV/SIGBUS in option-heavy tests
- fix(wasm): correct Direct-WASM option unwrapping -- `?=` target write-back, nested value-match arms, and ok/nil/err tag constant-folding -- plus valid stores when a value match is an assignment/index/field right-hand side
- test(wasm): add unit regressions pinning `?=` and value-match codegen against the native backend
- ci(playground): add a playground-demos gate that compiles and executes every teaching snippet through the in-process WASI shim, catching Direct-WASM drift native CI misses
- build(playground): replace WASMTIME with DEMO_RUN (shim/wasmtime/none), defaulting to the browser's own WASI loader so the gate needs no external runtime
- docs(playground): update examples to use ?i64/?= and value matches so snippets exercise overflow-safe arithmetic and option re-raising
- test(checker): pin baked std export and union-alias tables equivalent to on-the-fly full scans
- fix(checker): use module short name when a value-match diagnostic lacks a per-node source file, and add the vmp8s7tq i18n message across all locales

## v0.4.25

- feat(wasm): add `wasm-imports-check` and `wasm-runtime-smoke` CI/Makefile targets that reject forbidden socket imports and instantiate built wasm artifacts in a simulated loader environment
- feat(fmt): give fmt/vet/LSP one cross-module signature contract (`collectModuleSignatures` + `FmtSigLoader`) so formatting sees std and local module result types, while keeping bool-comparison simplification gated on proven bool operands
- refactor(fmt): consolidate formatting on a single `fmtflow.Format` entry, one `NewSemRebuildConfig` constructor and one `RedundantTypeAnnotationSafe` guard, closing the CLI/LSP annotation divergences found by a 659-file corpus diff
- fix(fmt/lsp): preserve struct-field evidence via `CloneProgramForFormat`, keep enum/fn switches when cross-module signatures are disabled, and invalidate `vetSigContext` on manifest changes
- refactor(compiler): replace manual increments and raw slice reads across the compiler with overflow-safe `ix.inc`/`ix.dec` and bounds-checked `ix.at-i`/`ix.at-s`
- feat(mir): add vec/str `len()`/`push()` method dispatch and `OP-VECPUSHSTR` lowering, with ownership fixes for `[]str` to avoid double-free
- fix(selfhost): unwrap `?i64` offsets in `ck.no` so `no build compiler` reaches a working S0 `noc` again
- chore(repo): update `.gitignore` and formatting docs, add agent red-line rules, and consolidate `nolang-core` skill routing

## v0.4.24

- fix(mir): cover all six new_utsname fields in os.uname() buffer to avoid stack overflow SIGSEGV on Linux
- fix(mir): gate unix sockaddr memcpy on path-length overflow to prevent out-of-bounds stack write
- test(mir): pin uname buffer geometry and sockaddr copy gate as regression invariants
- fix(build): stop treating explicitly non-string locals as a read-only string on len writes
- test(build): add size-map regression tests for the str.len false positive
- docs: update self-host design notes
- docs: refresh test coverage notes

## v0.4.23

- fix(net): use unprivileged SOCK_DGRAM ping socket on linux via ping_group_range
- fix(net): only filter icmp identifier for SOCK_RAW replies, skip on unprivileged socket
- fix(mir): declare late-registered option layouts to fix flaky codegen build failure
- fix(test): normalize uname.machine to GOARCH vocabulary before comparing to get-arch
- test(net): skip icmp assertions when no ping socket is available
- docs(i18n): add doc translations for de, es, fr, ja, ko, pt, ru, zh-cn, zh-tw locales
- docs(test): document std coverage and icmp skip rationale in test README

## v0.4.22

- fix(parser): infer value-match ok-arm tail type to fix option payload mismatch
- docs(std): normalize doc-comment parameter and return lines to name-only style
- fix(mir): free previous owned buffer on result-parameter reassignment
- build: add NOLANG_LEAK_DETECT env gate for LeakSanitizer linking
- test(memsafety): fix stale else-arm syntax in 3 regression tests
- chore(compiler): remove stale h2m-probe debugging script

## v0.4.21

- refactor(std): overhaul the standard library across ~80 modules, cutting ~6.6k lines while preserving the public API surface
- fix(mir): lower module-level tagged-enum and struct-literal constants referenced from helper functions to their real variant and payload instead of a zeroinitializer, fixing silent wrong values and bug #85
- fix(mir): resolve module-level binding types through the registered global type so match subjects type correctly
- fix(checker): detect overflow option-arithmetic silently dropped by place-match writeback and require explicit `?=` consumption
- fix(parser): improve block and value-match lowering and the AST disk cache
- fix(test): guard macOS-only EINVAL assertions in the unix socket test behind a Darwin check
- fix(docs): remove invalid `<br>` tokens from the annotation-scope reference
- test(std): expand the standard-library functional suite to 129 files / ~7479 assertions at 100% API coverage
- test(compiler): add codegen regression tests for mem-safety, tls13, x25519 and crypto, and update parser error-model tests
- docs: update language references (annotation-scope, str, syntax, basic-types) and the history page

## v0.4.20

- feat(pkg): implement package module loading and multi-module codegen, resolving cross-package imports from `workspace.noml`
- refactor(compiler): BREAKING -- remove support for default parameter values in function signatures
- feat(mir): shrink the `str` and `[]T` descriptors from 24B to 16B (`i32 len`, `i32 cap`, `i64 data` / `i8* data`) while the public API stays `i64`
- refactor(compiler): BREAKING(D11) -- remove the `#{index-out=DEF}` annotation; `a[i]` always yields `?T` and must be consumed by match or `?=`, and using it as a plain value is now a hard error
- feat(parser): add the block form of a match subject `{ body }: { arms }` so a `?=` failure inside the body is handled locally
- feat(parser): allow the `?=` shorthand inside a statement-level block match body, with `err -> _` / `-> _` as the standard fallbacks when the function has no `?T` result
- feat(h2m): add scope-exit release for str and vec ownership across straight-line and control-flow functions, including struct fields
- feat(compiler): support `#{overflow=wrap}` on arithmetic statements, including DOT-leading statements, and keep the annotation across `no fmt`
- fix(parser): complete the option arithmetic type chain, range-for iteration typing, value-match call subjects, and union monomorphization inference
- fix(checker): resolve the `?T` receiver of a desugared operator chain so the union-argument guard fires again
- feat(checker): move index and `?T` len/cap diagnostics into the checker and attach DiagnosticFix suggestions for `no vet`
- refactor(std): ban `s[i] = v` index writes in favour of the `push` family, place-match, `fill`, and `[]byte`
- feat(fmt): fold same-target arm assignments into value-context matches
- feat(glossary): add the structured glossary knowledge base with a Nolang CLI and MCP front end
- feat(cli): default `no test -timeout` to 60s so hung processes do not leave orphans
- feat(playground): rework the browser playground with 24 demos and Direct-WASM alignment
- fix(checker): rewrite the range-index loop hint to separate the read and write forms of container iteration
- test: merge similar cases into themed clusters and drop batch placeholder names
- docs: ban section divider banners and attach standalone comments with a `...` placeholder statement

## v0.4.19

- feat(mir): enable plain u64 arithmetic (`+`, `-`, `*`) and lower u64 bitwise, shift, division, and modulo as unsigned ops
- feat(mir): add f64 binary arithmetic, comparison, and prefix negation via h2m lowering
- feat(mir): lower i64 right shift as logical `lshr` and add bit-complement `~x` and logical `&&`/`||` operators
- feat(checker): add `it` implicit binding ty-of prescan for self-host ledger
- fix(parser): prevent option capture for boolean infix expressions
- fix(funcargs): reject invalid extra targets in std method multi-assign
- fix(codegen): hide internal symbols in shared library builds
- docs(host-interop): add detailed guide for calling Nolang from host languages

## v0.4.18

- feat(selfhost): add s62 container-iteration lowering for vec-backed `i <- xs` loops
- feat(selfhost): add s63 value-match lowering for integer pattern arms
- feat(selfhost): bind implicit `it` values in match arms and pop loop/name stacks on scope exit
- refactor(compiler): decouple call arguments with a dedicated side pool for scattered value ids
- fix(mir): unwrap option payloads on element stores before writing non-option elements
- fix(crypto): widen rc4 key-schedule byte arithmetic to i64 before modulo 256
- fix(checker): fall back to redundant std call signatures when inferring types in single-file mode
- test(no): pin implicit ok() wrapping on scalar writes into []?T elements and sibling-loop targets
- docs(selfhost): record s62-s64 lowering slices and the call argument side pool ledger

## v0.4.17

- feat(compiler): add option ordered comparisons via new MIR ops (int, f64, scalar, and f64-scalar wings)
- feat(compiler): implement AST-to-MIR lowering slices (s55-s61) for straight-line, control-flow, block terminator, signature/call, brace short-circuit chain, and range/count-loop traversal
- feat(parser): make `...` a no-op placeholder statement that is dropped at build time; it is no longer a return alias or a range operator (`[0...n)` is now a syntax error, write `[0..self.n)`)
- feat(fmt): keep `self` explicit when a range upper bound follows `..`, so formatting can no longer emit the `...` token
- feat(fmt): support `#{fmt-skip}` in Markdown Nolang fences (`no fmt -md`), keeping docs snippets byte-identical
- fix(fmt): stop dropping a trailing comment on `v ?= expr` (`parser.setComment` had no `UnwrapAssignStatement` case, so only the same-line form lost its comment)
- fix(fmt): a comment-only Markdown block is no longer reported as "not parseable as Nolang" (zero statements is "nothing to format", not a parse failure)
- feat(parser): force an else arm to group the preceding if-chain and migrate std/tests/compiler to grouped form
- refactor(parser): swap meanings of `*` and `**` shorthands for continue and break
- feat(lexer): add keyword aliases ctn/brk/rtn for continue/break/return across fmt, lsp, vscode and docs
- feat(std): expose rsa public bytes API and privatize limb internals
- fix(build): upgrade GitHub Actions to latest versions
- docs(overflow): clarify special overflow semantics for byte and char types
- docs(selfhost): document M4-e lowering slices s55-s61

## v0.4.16

- feat(compiler): add structural equality codegen for str and vec via @str_eq and @__nolang_vec_eq_i64 helpers
- feat(compiler): add option payload equality codegen for value, str, and f64 tags
- feat(compiler): add option f64 inline load/store codegen and vec to str reinterpret codegen
- feat(compiler): add unary ALU negation and not codegen via OP-NEG and OP-NOT

## v0.4.15

- feat(checker): forbid comparing non-option types with nil (`T == nil` is now a hard error; `?T == nil` stays legal)
- feat(cg): inline read/write of option vec payload (`?[]i64`) without boxing (s40, §11 B8)
- feat(cg): integer div/mod by-zero select guardrail with stderr diagnostic (s38/s39, §11 B14)
- feat(i18n): embed localized multi-line compiler help texts and version labels; expand translator catalogs
- fix(ci): restrict GitHub Pages deploy to release tags with manual dispatch; drop main-branch deploy trigger
- fix(std): update archive and crypto modules (bzip2/gzip/xz/zip/zlib/zstd, aes-gcm/des/ecdsa/ed25519/hmac/sha256/sha3); add bzip2 test
- perf(mir): add vec equality and refined index comparison codegen
- test(build): remove obsolete binary test file and expect bool as "true"/"false" in format interpolation

## v0.4.14

- feat(i18n): add an alias catalog of localized display names and keystroke columns for std
- feat(lsp): complete std identifiers from alias keystrokes (initials, pinyin, zhuyin)
- refactor(i18n): store alias rows as positional arrays and merge pinyin/zhuyin into one full column
- feat(build): accept module-alias imports (`# path alias`) on both backends
- feat(mir): render bool as true/false and char as signed decimal on every str path
- feat(checker): reject arithmetic mixing char with non-numeric operands
- refactor(fmt): collapse short multi-line infix expressions onto a single line
- fix(lexer): accept CRLF raw strings and stop eating the first byte of continuation lines
- fix(std): make noml index writes exhaustive and fix parse-raw dropping every other byte

## v0.4.13

- feat(i18n): ドイツ語、ポルトガル語、ロシア語のロケールを追加し、すべての言語間でドキュメントカタログを同期
- feat(crypto): x25519、ecdsa/x509、argon2、chacha20-poly1305、aes-gcm/cbc/ctr、scrypt、blake2、ed25519、pbkdf2、hmac、base32 を追加し、sha および rsa プリミティブを拡張
- feat(net): tls、websocket、hpack、http2/http3、quic、proxy および関連するネットワークプリミティブを追加
- feat(archive): bzip2、gzip、xz、zstd の圧縮サポートを追加
- fix(parser): 推測的パース経路からの誤検知診断を排除し、no fmt で FFI ドキュメントコメントを保持
- perf(mir): オプションの drop/clone ヘルパーをインラインのタグ判定とアウトラインの本体に分割し、コンパイル時間とバイナリサイズを削減
- build(compiler): セルフホスト型コンパイラ向けにパーサー、レクサー、AST テーブルを再生成

## v0.4.12

- feat(build,package): .noml を型付きモジュールとしてインポートし、noml メンバーに型注釈を付与
- feat(checker,build): スカラーから構造体への代入を拒否し、整数を bigint に昇格
- fix(parser,checker,mir): 演算子オーバーロードの ?T 結果を保持し、オプションの剥離は消費位置のみで行う
- feat(i18n,parser,lsp): 診断をカタログベースの i18n.T へ移行し、ドキュメントリンクのアンカーを追加
- feat(i18n,docs): es/fr/ja/ko/zh-TW ロケールを追加。ドキュメントカタログには翻訳のみを格納
- docs(std,compiler,tests,example): [label](#anchor) 形式のドキュメントリンクを追加し、name: プレフィックスを削除
- docs(agents): イテレーションをループから分離し、文区切りの例を修正

## v0.4.11

- feat(err): エラー型を位置情報ペイロードを持つ `err` へ再構築 (err.msg/code/file/line/col)
- feat(bigint): bigint の演算子オーバーロードと、+ - * / % における bigint/i64 混在オペランドを追加
- feat(parser): Go のオラクルとバイト単位で完全一致する構文エラー位置に到達 (NEWLINE、call-arg、cascade ファミリー)
- feat(std): メソッド形式による正確な strtod と最短ラウンドトリップ f64 フォーマットを追加
- feat(js): npm ESM インポートとネイティブパッケージ / FFI-koffi 呼び出しをサポート
- feat(ty): パーサー、HIR、THIR、MIR 間で共有される単一の構造的型管理機構を導入
- refactor(config): JSONC 設定を独自作成の noml 形式に置き換え
- refactor(parser): SurfaceMatch の match を統一し、旧来の desugar 経路を削除
- refactor(std): 数値の to-str ルーチンと bigint ベースの f64 パースを統合し、std モジュール全体の型注釈を整理
- fix(mir): 狭い (byte/u8/i8) スタックスロットを割り当て幅で読み込み拡張し、狭い代入先には `trunc` を選択 (crypto/aes SBOX での透過的な過剰読み取りによるセグフォルトと、不正な `sext i64 to i32` を修正)
- fix(std/crypto): blake2 の圧縮作業ベクタを確保し、argon2 のバッファを事前に確保して、実際の圧縮とインデックス付き書き込みを復元
- fix(build): union match のアームごとに型ディスパッチを行い、アームローカルのバインディングがジェネリック呼び出しの書き換え前に解決されるようにする (union 演算が黙って 0 を返す問題を修正)
- fix(compiler): 呼び出し戻り値の型推論を解決し、マージされた std 診断をそれぞれのソースファイルに帰属させる
- perf(parser): リフレクティブな AST 走査のためにエクスポート済みフィールドインデックスをキャッシュし、vec push/insert のコード生成とセルフホスト型レクサー/パーサーの O(N^2) を排除
- fix(mir,std,ci): テスト/std の失敗 (shl の err-option drop クラッシュ、i8 ストア幅、dns/err/chacha) を修正し、blake2 のゴールデンベクタテストを追加
- fix(package,cmd/no): リリースダウンロード経路を強化
- build: Go ツールチェーンを 1.27.3 にアップグレード。Windows リリースビルドを修正するため Setpgid/syscall.Kill をビルドタグの背後に配置
- feat(compiler): 動作しないセルフホスト MIR/codegen の骨格 (mir, cg, cg-rc, cg-runtime) を追加し、セルフホスト設計ドキュメントを拡充
- docs(design): パーサーの忠実性、match のローイング不変条件、S7 のデュアルトラック調査結果を記録
- test(std,mir,checker): union-arg、grow-write-lower、multiassign 演算子ディスパッチ、option-struct ペイロード match、slice/vec の option 書き込みのカバレッジを追加

## v0.4.10

- refactor(mmast): AST エンコーディングを再帰的なルートフォレスト構造に変更
- feat(conversion): 飽和処理付き浮動小数点数から整数への変換で数値型変換マトリクスを完成
- refactor(std): 数値変換と to-str メソッドを number モジュールへ移行
- feat(std): `txt.set-byte` / `str.set-byte` を失敗可能に変更 — `(i i64, b byte) (ok ?bool)`。バイトインデックスがレシーバーの物理容量を超える場合、nil を返して書き込みをスキップ
- refactor(bounds): 範囲内のインデックス読み取りに対する二層のオプション処理を完成
- fix(checker): 範囲外の文字リテラルと未サポートの科学表記を拒否
- refactor(tests): 古くなったコンパイラの単体テストを削除し、コメントのタイポを修正

## v0.4.9

- feat(parser): ペイロードの分割代入、空識別子による破棄、union 型に対する静的な型 match ディスパッチを備えたタグ付き列挙型のバリアントアームを追加
- feat(builtin/mir): MIR によりインライン化される str/txt のプライベートフィールドのアクセサー/ミューテーターを追加 (emitBuiltinPrivateField)
- feat(checker): アンダースコア接頭辞付き組み込み呼び出しに対してコンパイル時のプライバシーを強制し、増分ディスク std AST キャッシュを追加
- fix(type-inference): 代入とバインディングにおいて厳格なオプション算術の型規則を適用
- feat(lint): 範囲インデックスと範囲省略記号 (暗黙の self '...') の lint チェックを追加
- feat(std): crypto (aes-gcm/blake2/hmac/x25519/x509)、tls、str、txt、char モジュールを更新。tls ではスライスからスライスへのコピーを使用
- feat(lsp): 共有 std AST キャッシュを基盤とする std 型定義ルックアップを追加
- fix(fmt): --fix=redundant において推論されたビュー型の LHS 名を保持
- fix(build/ci): Windows の no-llvm バイナリに .exe サフィックスを付与してリネーム。lipo で x86_64 静的ライブラリを検証し、darwin プレビルドで macOS の librt をスタブ化
- refactor(compiler): 冗長な独立した ast/lx/tok/util テストファイルを削除し、セルフホスト設計ドキュメントを更新
- feat(lexer): 定数、グローバル、メソッド名に単一アンダースコアの識別子を許可。先頭の二重アンダースコアは明確な字句的エラーとして拒否
- fix(parser): ドット後の不正な '__' に対して、分かりにくい 'got UNDERSCORE' ではなく字句的エラーメッセージを表示
- test(lexer): 単一アンダースコア識別子と二重アンダースコア拒否のレクサーテストを追加
- fix(parser): 統一されたオプション算術の推論をサポートするため、'x = y' 代入全体で let バインディングの型を伝播
- fix(std): str モジュールから一時的なプライベート組み込みのスモークブロックを削除
- chore(cache): std およびパーサーの AST キャッシュ書き込み失敗に対するベストエフォートのデバッグログを追加

## v0.4.8

- fix(ci): Windows の no-llvm クロスビルド成果物に .exe サフィックスを付与
- feat(parser): ps.no におけるポインタ/ビュー型の接頭辞とメソッドシグネチャ表をサポート
- docs(design): 統一された算術/インデックスのオプション戻り値の実装状況を記録

## v0.4.7

- fix(build): Windows ターゲット向けに LLVM 静的リンクフラグを調整

## v0.4.6

- fix(build): genstdsig のホスト環境を保証し、リリースタグのチェックを修正

## v0.4.5

- feat(parser): ベア match の desugar、アスタリスクによる break/continue の短縮形、err/nil アームバインディングを実装
- feat(parser): パース中にローカル変数と関数戻り値の型を推論
- feat(compiler): 関数呼び出しのパースにおいて範囲スライス式をサポート
- fix(parser): 値 match 式における空のアームと早期 return アームをサポート
- fix(parser): 境界チェックを復元し、範囲内インデックスの例外を元に戻す
- fix(compiler): 非同期 (async) 前置演算子と末尾コメントをサポート
- refactor(main): 明示的な main 関数定義を削除
- build(llvm-windows): Windows の LLVM ビルドとパッケージングを zig cc/zig c++ に切り替え
- fix(ci): LLVM バージョンの抽出と LLVM ディストリビューションのコンポーネント名を修正
- chore(release): リリースプロセスで汚れた作業ツリーを許可

## v0.4.4

- fix(build): スペースを含む Windows パスを扱うため Makefile の LLVM_CONFIG をクォート
- fix(ci): llvm-config.exe をポーリングし、Windows の LLVM インストール手順にタイムアウト/エラー処理を追加
- fix(ci): CC/CXX に `zig cc`/`zig c++` を直接設定し、Windows の壊れた zig ラッパースクリプトを削除

## v0.4.3

- feat(parser): 値を持たない明示的な型宣言をサポート
- fix(build): 未サポートの `-x ir` の代わりに .ll 拡張子で LLVM IR を zig に渡す

## v0.4.2

- fix(build): zig ベースのリンクのため、Apple の LLVM ターゲットトリプルを zig の `macos` OS 名に変換
- fix(build): no-llvm 静的リンク時に存在しない `-lPolly*` 静的ライブラリを除外
- build(ci): クロスプラットフォームの LLVM ツールチェーンに向けてリリースワークフローを強化
- fix(parser): パースロジックを修正し、オーバーフローのラップ指標を追加

## v0.4.1

- fix(astcache): Tier-0 の std AST ブロブを再現可能にし、設定チェックを追加
- refactor(build): no-llvm リリース向けに全バックエンドを含む LLVM を埋め込み
- fix(parser): 深さの処理とオーバーフローのラップ動作を修正

## v0.4.0

- feat(build): ホストアーキテクチャごとにバックエンドを削った LLVM C API を備えた、埋め込み型 static-libLLVM バリアントを追加
- feat(build): 単一プロセスの clang -x ir、集中型バックエンド選択、埋め込み std モジュールリストによりバックエンドパイプラインを再構築
- feat(build): ジェネリックの単相化とオーバーロードのマングリングを実装
- feat(compiler): 増分ビルド用 AST キャッシュ (Tier 0 の焼き込み std AST + Tier 1 のディスク上キャッシュ) と初期のセルフホスティングコンパイラを追加
- feat(parser): 配列、マップ、スライス、範囲、構造体、match 式の AST パースを強化
- fix(checker): 独立したツールで std sig キーを注入。make でビルドされていない場合はハードフェイル
- fix(mir): %str-long のインデックスストアに対する成長ガードとメモリ安全テストを追加
- fix(build): 未処理の範囲外インデックスに対してコンパイルエラーを強制
- refactor(build): 不要になった toOpaquePointers の正規表現を削除し、C API 経路では IR 出力ステップをスキップ
- chore(checker): 生成された stdsig_gen.go と埋め込み標準シグネチャを削除
- feat(parser): ドキュメントコメントの添付とメソッド定義のパースを追加
- refactor(stdmodule): ルックアップのため std モジュール情報のスライスを map の KV に置き換え
- fix(build): クロスコンパイルされたバイナリに std sig キーを埋め込み

## v0.3.19

- fix(os): sysconf および getpid 呼び出しに Windows 互換性を追加

## v0.3.18

- fix(ci): 先頭の複数の "v" 文字を除去してバージョンタグを正規化

## v0.3.17

- fix(build): バージョン処理を更新し、Windows の組み込みサポートを追加

## v0.3.16

- fix(codegen): Windows の未サポートチェックと Linux の sysctl procfs フォールバックを追加
- chore(audit): 古くなった Nolang の監査および所有権モデルのドキュメントを削除
- docs(design): ネイティブバックエンド (nocg/nold) の前方設計を追加
- docs(design): ネイティブバックエンド設計の文字化けを修正

## v0.3.15

- feat(mir): `-opt[=N]` / `NOLANG_MIR_OPT` の背後にコンパイラ内蔵の MIR オプティマイザーを追加
- feat(mir): 定数畳み込み、デッドスカラー除去、制御フローのクリーンアップパスを追加
- feat(mir): 整数の同一性書き換え (`x+0`、`x*1`、`x&x`、`x^0`、…) と定数を生成する法則 (`x-x`、`x%1`、…) を追加
- feat(mir): ブロックごとのコピー伝播と単一前駆ブロックのマージを追加
- feat(codegen): `-opt=2` が有効な場合、どこからも参照されないランタイムヘルパーを削除。prelude が外部リンケージで出力されるため `opt -O2` にはできない
- feat(codegen): 削除したヘルパー自身のドキュメントコメントも併せて削除し、1 行の `print('hi')` モジュールを 61,812 バイトから 14,548 バイトへ (−76.5%)、−56.1% ではなく削減
- feat(build): scripts/miropt_vs_llvm.py により、IR 命令数、IR サイズ、コンパイル時間、バイナリサイズ、実行時間の観点で MIR オプティマイザーと LLVM `opt -O2` を比較
- fix(mir): コード生成が必要とする値の唯一の生成元である到達不能ブロックを保持
- fix(mir): 同じブロック内で後から再代入されるソース経由で読み手をリダイレクトするコピー伝播を停止
- fix(mir): 空ブロックのスレッディングがブロックの前駆集合を拡大するのを停止。これによりエッジごとの drop が 631 ファイルのコーパスのうち 49 ファイルで二重解放になっていた
- fix(mir): コピー伝播が内部のタスクハンドルのコピーを折りたたむのを停止。これにより 2 回目の待機が 1 回目にゼロクリアされたハンドルスロットを読んでいた
- refactor(checker): 冗長な 16 進リテラルチェックを削除し、型推論の登録を改善
- fix(checker): 型推論において暗黙の self 浮動小数点数メソッドを処理
- fix(builtin): math.pow の負の底の符号と組み込み処理を監査して修正
- refactor(std/math): math.pow の実装から libm 依存を削除

## v0.3.14

- refactor(math): libm 依存を削除し、数学関数を純粋な Nolang で実装
- fix(build): R_X86_64_32 の rodata 再配置を許可するため、Linux ネイティブバイナリを -no-pie でリンク
- fix(wasm): Writer.WriteByte を std のメソッドシグネチャ WriteByte(byte) error に一致させる
- fix(parser): MapType に IsInferred フラグを追加し、関連ロジックを更新
- docs(site): リリース履歴ページを追加し、サイドバーに登録

## v0.3.13

- feat(ownership): 段階的な健全性、ディープコピー代入規則、正確な RC 処理を備えたハイブリッド所有権モデルを実装
- feat(mir): 正しい drop とクローンのために move/所有権のセマンティクスを洗練。vec 要素をディープクローンし、スライスビューの生成元を生存させる
- feat(mir): 所有権付きオプションのルール 1 を完成させ、深い解放の回帰を修正
- feat(ownership): hashmap 型に対して Tier 1 の所有権付き %vec リーフを導入
- feat(number): 128 ビット整数の範囲、sqrt、is-prime メソッドを追加
- fix(mir): 文字列のクローンとフィールド代入における複数のメモリリークを修正
- fix(fmt): index-out 注釈の処理を改善し、誤った削除を防止
- fix(checker): LSP の surface AST モードで index-out 注釈をサポート
- perf(json): 冗長な index-out 注釈を削除してメモリ使用量を最適化
- refactor(async): 非同期キャンセルのプリミティブを cancel バリアントにリネーム
- docs(std): math および number の API を再構築。EN の std ドキュメントを CN のソースと同期
- docs(ownership): 所有権モデルをドキュメント化し、監査の再検証結果を記録

## v0.3.12

- feat(json): ヒープ割り当ての動的プールで実装をリファクタリングし、オーバーフローフラグでノードプールを拡張
- fix(mir): OpClone の下でオプションスロットの memcpy ではなく box からオプションペイロードを読み取る
- fix(mir): emitSetField における [N]T 構造体フィールドの配列リテラル初期化を修正
- fix(parser): 型解決を改善し、デバッグログを削除
- fix(match): 関数ローカル型の列挙型バリアントルックアップを改善
- feat(lint): readdir-unfiltered lint を生の read-dir に拡張し、list-dir-unfiltered lint を追加

## v0.3.11

- feat(build): make ターゲットにグローバルな no バイナリの再インストールを追加
- fix(release): release.py の tag サブコマンドによりタグ作成を冪等に

## v0.3.10

- fix(mir): %str-long のインデックスストアに null バッファガードを追加
- fix(parser): 安全でないインデックスのローイングがフォーマット済みソースを破壊するのを防止
- fix(build): 単相化クローン間でオーバーフロー注釈を保持し、診断のソースファイル情報を修正
- refactor(mysql): オーバーフローのラップ処理を追加し、スライスの安全性を改善
- fix(bench): fib ベンチマークにオーバーフロー注釈を追加
- refactor(parser): 構造体フィールドのコメントとパーサー状態のフォーマットを整列
- chore(audit): 2026-09-27 の Nolang 監査レポートを追加

## v0.3.9

- fix(mir): bigint のバスエラーを修正し、div/mod をテスト付きで実装
- fix(mir): option/match の LLVM バックエンドのバグを解決し、グローバル定数を改善
- feat(parser): option match における err バリアントの分割代入バインディングを追加
- fix(checker): 16 進配列に対する冗長型ヒントの誤検知を抑制し、option とスカラーの比較を緩和
- fix(checker): 文字列連結のオーバーフロー誤検知を修正し、効果のないオーバーフロー lint チェックを重複排除してスコープ化
- feat(cmd): AST 駆動の no fmt --fix=match を追加し、no fmt --fix=redundant をパッケージコンテキストで強化
- fix(release): リリースタグの比較とクエリロジックを洗練

## v0.3.8

- fix(mir): 存在しない _truncate CRT シンボルではなく kernel32 経由で Windows の truncate を実装
- test(mir): 宣言されたすべての Windows シムシンボルをインポートライブラリのエクスポートリストに対して検証

## v0.3.7

- feat(fmt): 引数ごとの名前付きフォーマットテンプレートを備えた可変長 print/eprint をサポート
- fix(builtin): printf と eprintf を削除し、移行を案内する明確なコンパイルエラーを出力
- refactor(mir): モジュール間の上書きを防ぐため、モジュールを宣言してモジュールグローバルバインディングをスコープ化

## v0.3.6

- feat(mir): 段階的設計によりタグ付き列挙型のペイロード所有権を実装
- fix(mir): match がゼロのタグ付き列挙型に対するフェーズ 3 の所有権を完成
- fix(mir): 両方の OpMove エンコーディングを処理し、黙った二重解放を修正
- fix(mir): 構造体フィールドと要素に対する set-byte のレシーバー書き戻しを修正
- fix(mir,std): 最後の所有権の穴をふさぎ、crypto/ のバイトインデックスを監査
- feat(mir): Windows の Winsock 初期化サポートを追加
- fix(mir): resolveCallee において組み込み名に対するベア名フォールバックをスキップ
- fix(checker): 入れ子の文字列連結チェーンに対する operandIntKind の再帰を修正
- refactor(lang): 言語から `xNN` バイトリテラルの表記を削除
- docs(design): resolveCallee のガードを記録し、大規模回帰のステータスを修正

## v0.3.5

- feat(mir): パッケージ初期化時ではなくターゲットごとに libc 呼び出し仕様表を構築
- fix(mir): Windows において realpath、touch-file、sync、stat、num-cpu を msvcrt のエントリポイントに置換
- fix(mir): 16 ビットの st_uid と st_gid を持つ Windows の struct _stat64 レイアウトを使用
- feat(mir): リンク時に失敗するのではなく、POSIX 限定の組み込みをコンパイル時に報告
- feat(nolang-release): リリースアクションが失敗した場合に最新タグを再利用

## v0.3.4

- feat(txt): txt 型に対してコードポイント単位のインデックスと更新を実装
- docs(str): str と txt の書き込みセマンティクスの違いを明確化
- fix(fmt): ベア match のフォーマット時に元の match 対象を復元

## v0.3.3

- feat(compiler): オプションのキャプチャ代入を実装
- feat(mir): 算術命令にオーバーフロー注釈サポートを追加
- feat(platform): MIR とコード生成においてクロスコンパイル対象プラットフォームをサポート
- fix(parser): option-match のアーム内のコメントを保持し、単一識別子の条件ループを正しくパース
- fix(checker): ASCII 文字列変数を関数単位でスコープ化し、合成されたメソッドレシーバーを処理
- fix(mir): 二重解放を避けるため借用された %vec パラメーターをディープクローン
- refactor(txt): 長さのセマンティクスをバイト単位の操作に統一
- fix(fmt): 真偽値比較の簡略化を元に戻し、無関係なオーバーフロー注釈を削除
- docs(std): 固定長 txt 型の長さセマンティクスを明確化
- test(index): option-match を伴うリテラル配列のインデックス付けのテストを追加

## v0.3.2

- feat(mir): オプションによるエラー処理を伴う、回復可能な整数除算と剰余を実装
- fix(parser): 符号付き整数の除算と剰余演算に対してオプション型を推論
- fix(checker): 整数のゼロ除算に対するコンパイル時チェックを追加
- fix(module): std 修飾呼び出しのベア名による乗っ取りを防止
- feat(nolang-docs-i18n): 英語 i18n ドキュメントのパリティスキルを追加
- refactor(format): 冗長な型注釈を自動的に削除
- refactor(tests): コーパスの .no ファイル名から tmp- および test- プレフィックスを削除
- refactor(dataflow): 古くなったデータフロー解析のドキュメントを削除
- chore(git): コンパイル済み Python キャッシュファイルを無視して削除

## v0.3.1

- feat(mir): os.args 組み込みを追加し、fs.read-file の結果処理を改善
- feat(checker): メソッド呼び出しに対して std メソッドのパラメータ型チェックを追加
- fix(fmt): index-out 注釈を保持し、空行の処理を改善
- refactor(checker): 整数オーバーフローの lint 解析にファイルコンテキストを追加
- refactor(cmd): 旧来のオーバーフロー移行コマンドとツールを削除
- refactor(mysql-driver): ループ構文とオーバーフロー注釈を標準化
- refactor(mir_golden): サブセット実行のサポートを改善し、安全策を更新
- docs: 制御フローの構文リファレンスを更新し、database および ffi の組み込みを削除

## v0.3.0

- feat(mir): MIR をデフォルトのバックエンドとし、旧来の LLVM バックエンドを引退
- feat(mir): パス感受性の use-after-move 解析と制御フローを考慮した drop を追加
- feat(mir): タグ付き列挙型と設定可能なオプションのインライン閾値を実装
- feat(mir): ファイルシステム、POSIX、汎用 vec の組み込みを追加
- feat(checker): オプションの安全性 lint を追加し、整数オーバーフローチェックを厳格化
- refactor(parser): メソッドレシーバーを self の out パラメーターとして表現
- fix(codegen): 可変長呼び出しの誤分類とスカラーから文字列への強制変換を修正
- feat(std): YAML 1.2 と TOML 1.0 のパースと生成を追加
- refactor(crypto): ハッシュモジュールを再編成し、非推奨の AES と RSA のコードを削除
- fix(parser): 連鎖した `->` 条件の入れ子 if のローイングを修正
- docs(lang): str のスライス、`it` のバインディング規則、フィールド注釈を明確化
