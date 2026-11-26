---
sidebar_position: 2
---

# 업데이트 로그

이 페이지에는 Nolang 각 버전의 릴리스 기록이 수록되어 있습니다. 저장소 루트 디렉터리에 있는 `HISTORY.md`와 동일한 내용입니다.

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

- feat(i18n): 독일어, 포르투갈어, 러시아어 로케일을 추가하고 모든 언어의 문서 카탈로그를 동기화
- feat(crypto): x25519, ecdsa/x509, argon2, chacha20-poly1305, aes-gcm/cbc/ctr, scrypt, blake2, ed25519, pbkdf2, hmac, base32를 추가하고 sha 및 rsa 프리미티브를 확장
- feat(net): tls, websocket, hpack, http2/http3, quic, proxy 및 관련 네트워킹 프리미티브 추가
- feat(archive): bzip2, gzip, xz, zstd 압축 지원 추가
- fix(parser): 추측성 파싱 경로에서 오탐 진단을 제거하고 no fmt에서 FFI 문서 주석을 보존
- perf(mir): option drop/clone 헬퍼를 인라인 태그 검사와 아웃라인 본문으로 분리하여 컴파일 시간과 바이너리 크기 단축
- build(compiler): 셀프 호스팅 컴파일러의 파서, 렉서, AST 테이블 재생성

## v0.4.12

- feat(build,package): .noml을 타입이 지정된 모듈로 가져오고 noml 멤버에 애노테이션 추가
- feat(checker,build): 스칼라를 구조체에 대입하는 것을 거부하고 정수를 bigint로 승격
- fix(parser,checker,mir): 연산자 오버로드 ?T 결과를 유지하고 option은 사용 위치에서만 벗겨냄
- feat(i18n,parser,lsp): 진단을 문서 링크 앵커가 있는 카탈로그 기반 i18n.T로 마이그레이션
- feat(i18n,docs): es/fr/ja/ko/zh-TW 로케일 추가; 문서 카탈로그에는 번역만 저장
- docs(std,compiler,tests,example): [label](#anchor) 문서 링크를 추가하고 name: 접두사 제거
- docs(agents): 반복(iteration)을 반복문(loops)에서 분리하고 문장 구분자 예제 수정

## v0.4.11

- feat(err): error 타입을 위치 페이로드(err.msg/code/file/line/col)가 있는 `err`로 재구성
- feat(bigint): bigint 연산자 오버로딩과 + - * / %에 대한 bigint/i64 혼합 피연산자 추가
- feat(parser): Go 오라클과 바이트 단위로 정확한 구문 오류 위치 일치 달성 (NEWLINE, call-arg, cascade 계열)
- feat(std): 메서드 형태를 통한 정밀한 strtod 및 최단 왕복 f64 포맷팅
- feat(js): npm ESM 가져오기 및 네이티브 패키지 / FFI-koffi 호출 지원
- feat(ty): 파서, HIR, THIR, MIR에서 공유하는 단일 구조적 타입 권한 도입
- refactor(config): JSONC 구성을 자체 제작한 noml 형식으로 대체
- refactor(parser): SurfaceMatch에 대한 match를 통합하고 기존 desugar 경로 제거
- refactor(std): 숫자 to-str 루틴과 bigint 기반 f64 파싱을 통합하고 std 모듈 전반의 타입 애노테이션 정리
- fix(mir): 좁은 (byte/u8/i8) 스택 슬롯을 할당된 너비로 로드하고 확장하며, 더 좁은 대상에는 `trunc`를 선택 (crypto/aes SBOX의 조용한 과다 읽기 세그폴트 및 잘못된 `sext i64 to i32` 수정)
- fix(std/crypto): blake2 압축 작업 벡터를 할당하고 argon2 버퍼를 미리 할당하여 실제 압축 및 인덱스 기반 쓰기 복원
- fix(build): union match 암에 대해 암별 타입 디스패치를 적용하여 제네릭 호출 재작성 전에 암 로컬 바인딩이 해석되도록 함 (union 연산에서 조용히 0이 나오는 문제 수정)
- fix(compiler): 호출 반환 타입 추론을 해결하고 병합된 std 진단을 자체 소스 파일에 귀속
- perf(parser): 리플렉티브 AST 순회를 위해 내보낸 필드 인덱스를 캐시하고 vec push/insert 코드젠 및 셀프 호스팅 렉서/파서의 O(N^2) 제거
- fix(mir,std,ci): test-std/src 실패 수정 (shl err-option drop 충돌, i8 저장 너비, dns/err/chacha) 및 blake2 골든 벡터 테스트 추가
- fix(package,cmd/no): 릴리스 다운로드 경로 강화
- build: Go 툴체인을 1.27.3으로 업그레이드하고 Windows 릴리스 빌드를 수정하기 위해 Setpgid/syscall.Kill을 빌드 태그 뒤에 배치
- feat(compiler): 비활성 셀프 호스트 MIR/codegen 스켈레톤 (mir, cg, cg-rc, cg-runtime) 추가 및 셀프 호스트 설계 문서 확장
- docs(design): 파서 충실성, match-lowering 불변식, S7 이중 트랙 결과 기록
- test(std,mir,checker): union-arg, grow-write-lower, 다중 대입 연산자 디스패치, option-struct 페이로드 match, slice/vec option 쓰기 커버리지 추가

## v0.4.10

- refactor(mmast): AST 인코딩을 재귀적 루트 포레스트 구조로 전환
- feat(conversion): 포화 float-to-int를 포함한 숫자 타입 변환 매트릭스 완성
- refactor(std): 숫자 변환 및 to-str 메서드를 number 모듈로 이전
- feat(std): `txt.set-byte` / `str.set-byte`를 실패 가능하도록 변경 — `(i i64, b byte) (ok ?bool)`, 바이트 인덱스가 수신자의 실제 용량을 초과하면 nil을 반환하고 저장을 건너뜀
- refactor(bounds): 범위 내 인덱스 읽기에 대한 2계층 option 처리 완성
- fix(checker): 범위를 벗어난 char 리터럴과 지원되지 않는 과학적 표기법 거부
- refactor(tests): 오래된 컴파일러 단위 테스트 제거 및 주석 오타 수정

## v0.4.9

- feat(parser): 페이로드 구조 분해, 빈 식별자 버리기, union 타입에 대한 정적 타입-match 디스패치를 갖춘 태그된 enum variant 암 추가
- feat(builtin/mir): MIR에 의해 인라인으로 lowered되는 비공개 str/txt 필드 접근자/변경자 추가 (emitBuiltinPrivateField)
- feat(checker): 밑줄 접두사 builtin 호출에 컴파일 타임 비공개성 적용 및 점진적 디스크 std AST 캐시 추가
- fix(type-inference): 대입과 바인딩에서 엄격한 option 산술 타입 규칙 적용
- feat(lint): range-index 및 range-ellipsis (암묵적 self '...') lint 검사 추가
- feat(std): crypto (aes-gcm/blake2/hmac/x25519/x509), tls, str, txt, char 모듈 업데이트; tls에서 slice-to-slice 복사 사용
- feat(lsp): 공유 std AST 캐시를 기반으로 한 std 타입 정의 조회 추가
- fix(fmt): --fix=redundant에서 추론된 view 타입의 LHS 이름 보존
- fix(build/ci): Windows no-llvm 바이너리에 .exe 접미사 추가; lipo로 x86_64 정적 라이브러리 검증 및 darwin 사전 빌드에서 macOS librt 스텁 처리
- refactor(compiler): 중복된 독립 ast/lx/tok/util 테스트 파일 제거 및 셀프 호스트 설계 문서 업데이트
- feat(lexer): 상수, 전역 변수, 메서드 이름에 단일 밑줄 식별자 허용; 선행 이중 밑줄은 명확한 어휘 오류로 거부
- fix(parser): 점 뒤의 잘못된 '__'에 대해 혼란스러운 'got UNDERSCORE' 대신 어휘 오류 메시지 표시
- test(lexer): 단일 밑줄 식별자 및 이중 밑줄 거부에 대한 렉서 테스트 추가
- fix(parser): 통합 option 산술 추론을 지원하기 위해 'x = y' 대입 전반에 let 바인딩 타입 전파
- fix(std): str 모듈에서 임시 private-builtin 스모크 블록 제거
- chore(cache): std 및 파서 AST 캐시 쓰기 실패에 대해 최선의 디버그 로깅 추가

## v0.4.8

- fix(ci): windows no-llvm 교차 빌드 아티팩트에 .exe 접미사 추가
- feat(parser): ps.no에서 포인터/view 타입 접두사 및 메서드 시그니처 테이블 지원
- docs(design): 통합 산술/index option 반환의 구현 상태 기록

## v0.4.7

- fix(build): Windows 대상에 대한 LLVM 정적 링킹 플래그 조정

## v0.4.6

- fix(build): genstdsig의 호스트 환경 보장 및 릴리스 태그 검사 수정

## v0.4.5

- feat(parser): bare match desugaring, star break/continue 축약형, err/nil 암 바인딩 구현
- feat(parser): 파싱 중 지역 변수 및 함수 반환값의 타입 추론
- feat(compiler): 함수 호출 파싱에서 범위 slice 표현식 지원
- fix(parser): 값 match 표현식에서 빈 암 및 조기 반환 암 지원
- fix(parser): 범위 검사 복원 및 범위 내 인덱스 예외 되돌림
- fix(compiler): async 접두 연산자 및 후행 주석 지원
- refactor(main): 명시적 main 함수 정의 제거
- build(llvm-windows): Windows LLVM 빌드 및 패키징에 zig cc/zig c++ 사용으로 전환
- fix(ci): LLVM 버전 추출 및 LLVM 배포 구성 요소 이름 수정
- chore(release): 릴리스 과정에서 더티 작업 트리 허용

## v0.4.4

- fix(build): 공백이 있는 Windows 경로를 처리하기 위해 Makefile에서 LLVM_CONFIG를 따옴표로 감쌈
- fix(ci): llvm-config.exe를 폴링하고 Windows LLVM 설치 단계에 타임아웃/오류 처리 추가
- fix(ci): CC/CXX를 `zig cc`/`zig c++`로 직접 설정하고 Windows에서 깨진 zig 래퍼 스크립트 제거

## v0.4.3

- feat(parser): 값이 없는 명시적 타입 선언 지원 추가
- fix(build): 지원되지 않는 `-x ir` 대신 .ll 확장자를 통해 zig에 LLVM IR 제공

## v0.4.2

- fix(build): zig 기반 링킹을 위해 Apple LLVM 타입 triple을 zig의 `macos` OS 이름으로 변환
- fix(build): no-llvm 정적 링킹 중 누락된 `-lPolly*` 정적 라이브러리 필터링
- build(ci): 크로스 플랫폼 LLVM 툴체인을 위한 릴리스 워크플로 강화
- fix(parser): 파싱 로직 수정 및 오버플로 래핑 표시자 추가

## v0.4.1

- fix(astcache): Tier-0 std AST blob을 재현 가능하고 구성 검증되도록 수정
- refactor(build): no-llvm 릴리스용으로 모든 백엔드가 포함된 LLVM 임베드
- fix(parser): 깊이 처리 및 오버플로 래핑 동작 수정

## v0.4.0

- feat(build): 호스트 아키텍처별로 백엔드를 단순화한 LLVM C API를 갖춘 임베드형 static-libLLVM 변형 추가
- feat(build): 단일 프로세스 clang -x ir, 중앙 집중식 백엔드 선택, 임베드된 std 모듈 목록으로 백엔드 파이프라인 재구성
- feat(build): 제네릭 모노모피제이션 및 오버로드 맹글링 구현
- feat(compiler): 점진적 빌드 AST 캐시 (Tier 0 구운 std AST + Tier 1 디스크) 및 초기 셀프 호스팅 컴파일러 추가
- feat(parser): 배열, map, slice, 범위, 구조체, match 표현식에 대한 AST 파싱 강화
- fix(checker): 독립 도구를 통해 std sig 키 주입; make로 빌드하지 않으면 강제 실패
- fix(mir): percent-str-long 인덱스 저장소 증가 가드 및 메모리 안전 테스트 추가
- fix(build): 처리되지 않은 범위 초과 인덱싱에 대해 컴파일 오류 강제
- refactor(build): 죽은 toOpaquePointers 정규식 제거 및 C API 경로에서 IR 내보내기 단계 건너뛰기
- chore(checker): 생성된 stdsig_gen.go 및 임베드된 표준 시그니처 제거
- feat(parser): 문서 주석 첨부 및 메서드 정의 파싱 추가
- refactor(stdmodule): 조회를 위해 std 모듈 정보 slice를 map KV로 대체
- fix(build): 크로스 컴파일된 바이너리에 std sig 키 임베드

## v0.3.19

- fix(os): sysconf 및 getpid 호출에 Windows 호환성 추가

## v0.3.18

- fix(ci): 여러 개의 선행 "v" 문자를 제거하여 버전 태그 정규화

## v0.3.17

- fix(build): 버전 처리 업데이트 및 Windows builtin 지원 추가

## v0.3.16

- fix(codegen): windows 미지원 검사 및 Linux sysctl procfs 폴백 추가
- chore(audit): 오래된 Nolang 감사 및 소유권 모델 문서 제거
- docs(design): 네이티브 백엔드 (nocg/nold) 선행 설계 추가
- docs(design): 네이티브 백엔드 설계의 깨진 텍스트 수정

## v0.3.15

- feat(mir): `-opt[=N]` / `NOLANG_MIR_OPT` 뒤에 컴파일러 내 MIR 최적화기 추가
- feat(mir): 상수 폴딩, 죽은 스칼라 제거, 제어 흐름 정리 패스 추가
- feat(mir): 정수 항등 재작성 (`x+0`, `x*1`, `x&x`, `x^0`, …) 및 상수 생성 법칙 (`x-x`, `x%1`, …) 추가
- feat(mir): 블록별 복사 전파 및 단일 선행자 블록 병합 추가
- feat(codegen): `-opt=2`가 켜져 있을 때 아무도 참조하지 않는 런타임 헬퍼 제거 — prelude가 외부 링키지로 내보내지기 때문에 `opt -O2`는 이를 수행할 수 없음
- feat(codegen): 제거된 헬퍼의 문서 주석도 함께 삭제하여 한 줄짜리 `print('hi')` 모듈이 −56.1%가 아닌 61,812바이트에서 14,548바이트(−76.5%)로 축소
- feat(build): scripts/miropt_vs_llvm.py를 통해 IR 명령어, IR 크기, 컴파일 시간, 바이너리 크기, 실행 시간 측면에서 MIR 최적화기를 LLVM `opt -O2`와 비교
- fix(mir): 코드젠이 여전히 필요로 하는 값의 유일한 생산자인 도달 불가능 블록 유지
- fix(mir): 같은 블록에서 나중에 재할당된 소스를 통해 독자를 리다이렉트하는 복사 전파 중지
- fix(mir): 빈 블록 스레딩이 블록의 선행자 집합을 확장하는 것을 중지 — 이로 인해 631개 파일 코퍼스 중 49개에서 간선별 drop이 이중 해제로 변했음
- fix(mir): 내부 task-handle 복사를 병합하는 복사 전파를 중지 — 이로 인해 두 번째 wait가 첫 번째가 0으로 만든 핸들 슬롯을 읽었음
- refactor(checker): 중복된 16진수 리터럴 검사 제거 및 타입 추론 등록 개선
- fix(checker): 타입 추론에서 암묵적 self float 메서드 처리
- fix(builtin): math.pow의 음수 밑 부호 및 builtin 처리 감사 및 수정
- refactor(std/math): math.pow 구현에서 libm 의존성 제거

## v0.3.14

- refactor(math): libm 의존성 제거; 수학 함수를 순수 Nolang으로 구현
- fix(build): R_X86_64_32 rodata 재배치를 허용하기 위해 Linux 네이티브 바이너리를 -no-pie로 링크
- fix(wasm): Writer.WriteByte가 std 메서드 시그니처 WriteByte(byte) error와 일치하도록 수정
- fix(parser): MapType에 IsInferred 플래그 추가 및 관련 로직 업데이트
- docs(site): 릴리스 기록 페이지 추가 및 사이드바에 등록

## v0.3.13

- feat(ownership): 계층적 건전성, 깊은 복사 대입 규칙, 정밀한 RC 처리를 갖춘 하이브리드 소유권 모델 구현
- feat(mir): 올바른 drop과 복제를 위해 move/소유권 의미를 정교화하고, vec 요소를 깊은 복사하며 slice view 소스를 유지
- feat(mir): 소유 option에 대한 규칙 1 완성 및 깊은 해제 회귀 수정
- feat(ownership): hashmap 타입에 대해 tier 1 소유 %vec 리프 적용
- feat(number): 128비트 정수 범위, sqrt, is-prime 메서드 추가
- fix(mir): 문자열 복제 및 필드 대입에서 여러 메모리 누수 수정
- fix(fmt): index-out 애노테이션 처리 개선 및 잘못된 제거 방지
- fix(checker): LSP surface AST 모드에서 index-out 애노테이션 지원
- perf(json): 중복된 index-out 애노테이션 제거로 메모리 사용 최적화
- refactor(async): async-cancel 프리미티브를 cancel 변형으로 이름 변경
- docs(std): math 및 number API 재구성; EN std 문서를 CN 원본과 동기화
- docs(ownership): 소유권 모델을 문서화하고 감사 재검증 결과 기록

## v0.3.12

- feat(json): 힙 할당 동적 풀로 구현을 리팩터링하고 오버플로 플래그로 노드 풀 확장
- fix(mir): option 슬롯의 memcpy 대신 OpClone 하에서 box에서 option 페이로드 읽기
- fix(mir): emitSetField에서 [N]T 구조체 필드의 배열 리터럴 초기화 수정
- fix(parser): 타입 해석 개선 및 디버그 로그 제거
- fix(match): 함수 로컬 타입에 대한 enum variant 조회 개선
- feat(lint): readdir-unfiltered lint을 raw read-dir로 확장하고 list-dir-unfiltered lint 추가

## v0.3.11

- feat(build): make 타깃에 전역 no 바이너리 재설치 추가
- fix(release): release.py tag 서브커맨드를 통해 태그 생성을 멱등하게 수정

## v0.3.10

- fix(mir): %str-long 인덱스 저장소에 null 버퍼 가드 추가
- fix(parser): 안전하지 않은 인덱스 lowering이 포맷된 소스를 손상시키는 것 방지
- fix(build): 모노모프 복사 전반에 오버플로 애노테이션 보존 및 진단 소스 파일 정보 수정
- refactor(mysql): 오버플로 래핑 처리 추가 및 slice 안전성 개선
- fix(bench): fib 벤치마크에 오버플로 애노테이션 추가
- refactor(parser): 구조체 필드 주석 및 파서 상태 포맷 정렬
- chore(audit): 2026-09-27 Nolang 감사 보고서 추가

## v0.3.9

- fix(mir): bigint 버스 오류 수정 및 테스트와 함께 div/mod 구현
- fix(mir): option/match LLVM 백엔드 버그 해결 및 전역 상수 개선
- feat(parser): option match에서 err variant 구조 분해 바인딩 추가
- fix(checker): 16진수 배열에 대한 오탐 중복 타입 힌트 억제 및 option-vs-scalar 비교 완화
- fix(checker): 문자열 연결 오버플로 오탐 수정 및 비효과적인 오버플로 lint 검사에 중복 제거 적용
- feat(cmd): AST 기반 no fmt --fix=match 추가 및 no fmt --fix=redundant에 패키지 컨텍스트 강화
- fix(release): 릴리스 태그 비교 및 조회 로직 정교화

## v0.3.8

- fix(mir): 존재하지 않는 _truncate CRT 심볼 대신 kernel32를 통해 windows에서 truncate 구현
- test(mir): 선언된 모든 windows shim 심볼을 import-library 내보내기 목록과 검증

## v0.3.7

- feat(fmt): 인자별 이름이 지정된 포맷 템플릿과 함께 가변 인자 print/eprint 지원
- fix(builtin): printf 및 eprintf 제거하고 마이그레이션을 안내하는 명확한 컴파일 오류 발생
- refactor(mir): 모듈 간 덮어쓰기를 방지하기 위해 모듈 선언으로 모듈 전역 바인딩 범위 지정

## v0.3.6

- feat(mir): 단계적 설계로 태그된 enum 페이로드 소유권 구현
- fix(mir): zero-match 태그된 enum에 대해 단계 3 소유권 완료
- fix(mir): 두 가지 OpMove 인코딩을 처리하여 조용한 이중 해제 수정
- fix(mir): 구조체 필드와 요소에서 set-byte의 수신자 쓰기 복구 수정
- fix(mir,std): 마지막 소유권 공백 해소; 바이트 인덱싱에 대해 crypto/ 감사
- feat(mir): Windows Winsock 초기화 지원 추가
- fix(mir): resolveCallee에서 builtin 이름에 대한 bare-name 폴백 건너뛰기
- fix(checker): 중첩된 문자열 연결 체인에 대해 operandIntKind 재귀 수정
- refactor(lang): 언어에서 `xNN` 바이트 리터럴 표기 제거
- docs(design): resolveCallee 가드 기록 및 대규모 회귀 상태 수정

## v0.3.5

- feat(mir): 패키지 초기화 대신 대상별로 libc 호출 스펙 테이블 구축
- fix(mir): windows에서 realpath, touch-file, sync, stat, num-cpu에 대해 msvcrt 진입점 대체
- fix(mir): 16비트 st_uid 및 st_gid를 갖는 Windows 구조체 _stat64 레이아웃 사용
- feat(mir): 링크 실패 대신 컴파일 타임에 POSIX 전용 builtin 보고
- feat(nolang-release): 릴리스 액션이 실패했을 때 최신 태그 재사용

## v0.3.4

- feat(txt): txt 타입에 대한 코드 포인트 인덱싱 및 업데이트 구현
- docs(str): str과 txt의 쓰기 의미 차이 명확화
- fix(fmt): bare match 포맷팅에서 원래 match 대상 복구

## v0.3.3

- feat(compiler): option 캡처 대입 구현
- feat(mir): 산술 명령어에 오버플로 애노테이션 지원 추가
- feat(platform): MIR 및 코드젠에서 크로스 컴파일 대상 플랫폼 지원
- fix(parser): option-match 암의 주석 보존 및 단일 식별자 조건 반복문 올바르게 파싱
- fix(checker): ASCII 문자열 변수를 함수별로 범위 지정하고 합성된 메서드 수신자 처리
- fix(mir): 이중 해제를 피하기 위해 빌린 %vec 매개변수를 깊은 복제
- refactor(txt): 길이 의미를 바이트 기반 연산으로 통합
- fix(fmt): 불리언 비교 단순화 되돌리고 관련 없는 오버플로 애노테이션 제거
- docs(std): 고정 길이 txt 타입의 길이 의미 명확화
- test(index): option-match와 함께 리터럴 배열 인덱싱 테스트 추가

## v0.3.2

- feat(mir): option 오류 처리와 함께 복구 가능한 정수 나눗셈 및 나머지 연산 구현
- fix(parser): 부호 있는 정수 나눗셈 및 나머지 연산에 대해 option 타입 추론
- fix(checker): 정수 0 나눗셈에 대한 컴파일 타임 검사 추가
- fix(module): std 한정 호출의 bare-name 가로채기 방지
- feat(nolang-docs-i18n): 영어 i18n 문서 패리티 스킬 추가
- refactor(format): 중복된 타입 애노테이션 자동 제거
- refactor(tests): 코퍼스 .no 파일명에서 tmp- 및 test- 접두사 제거
- refactor(dataflow): 오래된 데이터 흐름 분석 문서 제거
- chore(git): 컴파일된 python 캐시 파일 무시 및 제거

## v0.3.1

- feat(mir): os.args builtin 추가 및 fs.read-file 결과 처리 개선
- feat(checker): 메서드 호출에 대해 std 메서드 매개변수 타입 검사 추가
- fix(fmt): index-out 애노테이션 보존 및 빈 줄 처리 개선
- refactor(checker): 정수 오버플로 lint 분석에 파일 컨텍스트 추가
- refactor(cmd): 기존 오버플로 마이그레이션 명령어 및 도구 제거
- refactor(mysql-driver): 반복문 문법 및 오버플로 애노테이션 표준화
- refactor(mir_golden): 부분 실행 지원 개선 및 안전장치 업데이트
- docs: 제어 흐름 문법 참조 업데이트 및 데이터베이스 및 ffi builtin 제거

## v0.3.0

- feat(mir): MIR을 기본 백엔드로 만들고 기존 LLVM 백엔드 폐기
- feat(mir): 경로 민감형 use-after-move 분석 및 제어 흐름 인식 drop 추가
- feat(mir): 태그된 enum 및 구성 가능한 option 인라인 임계값 구현
- feat(mir): 파일시스템, POSIX, 범용 vec builtin 추가
- feat(checker): option 안전 lint 추가 및 정수 오버플로 검사 강화
- refactor(parser): 메서드 수신자를 self out-param으로 표현
- fix(codegen): 가변 인자 호출 오분류 및 스칼라-투-str 강제 변환 수정
- feat(std): YAML 1.2 및 TOML 1.0 파싱 및 생성 추가
- refactor(crypto): 해시 모듈 재구성 및 더 이상 사용되지 않는 AES 및 RSA 코드 제거
- fix(parser): 연결된 `->` 조건에 대한 중첩 if lowering 수정
- docs(lang): str 슬라이싱, `it` 바인딩 규칙, 필드 애노테이션 명확화
