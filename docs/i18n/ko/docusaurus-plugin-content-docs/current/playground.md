---
sidebar_position: 5
---

# Playground

Nolang Playground는 브라우저에서 직접 Nolang 프로그램을 편집, 컴파일하고 실행할 수 있는 온라인 환경입니다. Nolang 컴파일러(`no.wasm`)와 언어 서버(`lsp.wasm`)를 WebAssembly로 컴파일하고, WASI를 통해 브라우저 샌드박스에서 사용자 코드를 실행하므로 로컬에 어떤 도구 모음도 설치할 필요가 없습니다.

지금 바로 이동: [**/playground**](/playground)

## 사용법

Playground 페이지는 좌우 두 개의 창으로 나뉩니다:

- **왼쪽 Editor**: [CodeMirror 6](https://codemirror.net/) 기반의 코드 편집기로, Nolang 문법 강조, 자동 들여쓰기와 기록을 지원합니다.
- **오른쪽 Output / stderr / Diagnostics**: 실행 결과, 표준 오류 출력과 진단 메시지입니다.

인터페이스는 Docusaurus의 로케일을 따라 간체 중국어와 영어로 자동 전환되며, 출력 창은 밝은 테마와 어두운 테마를 모두 지원합니다.

### 도구 모음

| 버튼 | 기능 |
| --------- | ----------------------------------------------------------------- |
| **▶ Run** | 현재 코드를 WASM으로 컴파일하고 WASI를 통해 실행합니다. stdout은 오른쪽 Output에 표시됩니다. |
| **Format**| LSP를 통해 `no fmt`를 호출하여 현재 코드를 포맷합니다. |
| **Examples** | 분류별로 내장 예제를 로드하여 편집기의 현재 내용을 덮어씁니다. |

도구 모음 오른쪽에는 현재 상태(유휴／컴파일 중／실행 중／완료／오류)가 표시되고, 드롭다운 메뉴 아래에는 해당 예제의 한영 이중 언어 설명이 표시됩니다. 마지막으로 선택한 예제는 브라우저 로컬 저장소에 보관됩니다.

### 예제

Examples 드롭다운 메뉴에는 주제별로 분류된 **24개** 예제가 미리 수록되어 있으며, 모두 브라우저와 완전히 동일한 `--wasm-direct` 컴파일 경로를 통해 CI에서 검증됩니다:

| 분류 | 개수 | 내용 |
| ---- | ---- | ---- |
| 기초 | 7 | Hello World, 변수 추론, 함수와 재귀, Fibonacci, 산술 및 비트 연산, 형식화된 출력, 문자열 |
| 흐름 제어 | 6 | 단락 가드, 패턴 매칭, FizzBuzz, 네 가지 범위 반복문, 조건부 while 반복문, break / continue |
| 자료 구조 | 4 | 고정 길이 배열, 동적 슬라이스, 구조체, 메서드 |
| 스칼라 타입 | 4 | char, bool, f64, byte |
| 표준 라이브러리 | 2 | math.max / math.min / sqrt, rand.rand |
| 오류 처리 | 1 | option과 nil의 세 갈래 match |

각 예제에는 안정적인 예상 출력 문자열이 함께 제공됩니다. CI는 각 예제를 컴파일하고 `wasmtime`으로 실행한 뒤, 해당 문자열이 stdout에 나타나는지 확인합니다. 따라서 언어나 Direct-WASM 백엔드가 발전해도 예제가 조용히 깨지지 않습니다.

예제를 선택하면 Playground는 현재 Output, stderr, Diagnostics를 비우고 편집기 내용을 해당 예제로 바꿉니다.

### 진단

LSP가 문법 또는 타입 오류를 감지하면 오른쪽 하단의 Diagnostics 영역에 표시합니다. 진단 항목을 클릭하면 해당 줄과 열로 이동합니다. 컴파일러 stderr의 `file:line:col` 메시지는 별도로 **Run Errors** 표기로 파싱되며, 끝의 TraceID와 현지화된 심각도 접두사는 자동으로 제거됩니다.

### 오류와 시간 초과

과거에는 WASM 로드 실패가 `console.warn`에만 기록되어, 사용자는 아무 반응도 없는 Run 버튼만 보았습니다. 현재 동작은 다음과 같습니다:

- `no.wasm` 또는 `lsp.wasm` 다운로드／컴파일이 실패하면 페이지 맨 위에 닫을 수 있는 오류 배너가 표시되어 어떤 리소스와 원인인지 알려줍니다.
- 언어 서버 초기화 실패 시에도 배너가 표시됩니다. 편집기는 계속 사용할 수 있지만 자동 완성, 호버, 포맷팅은 사용할 수 없습니다.
- 각 실행은 메인 스레드의 워치독이 시간을 재며(기본 8초), `wasi.start()`는 worker에서 동기 호출이므로 무한 반복문이 생기면 worker 안의 어떤 타이머도 실행되지 않습니다. 따라서 유일하게 유효한 중단 방법은 메인 스레드에서 worker를 종료하는 것입니다. 시간 초과 후 worker는 종료되고 다음 실행 시 자동으로 재생성되며, 동시에 WASM을 즉시 다시 예열합니다.
- Run을 반복 클릭하면 병렬 보호에 의해 차단되고, 대기 중인 편집의 디바운스 타이머는 예제 전환이나 포맷팅 시 취소되어 이전 입력이 새 내용을 덮어쓰는 것을 방지합니다.

`no.wasm`과 `lsp.wasm`은 페이지가 유휴 상태일 때 미리 다운로드되어 스트리밍 방식으로 컴파일되므로, 처음 Run을 누를 때 전체 27 MB 다운로드와 컴파일을 기다릴 필요가 없습니다.

## 제한 사항

Playground는 브라우저 WASM 샌드박스에서 실행되므로 일부 Nolang 기능을 사용할 수 없습니다:

### FFI 미지원

브라우저는 네이티브 C 라이브러리를 링크할 수 없으므로 `#{c}`, `#{cpp}`, `#{rust}` 같은 FFI 선언은 Playground에서 사용할 수 없습니다. FFI가 포함된 코드(`sqlite`, `mysql` 드라이버 등)는 실행할 수 없습니다.

### fork / exec / pipe 미지원

WASI preview1은 프로세스 관리 API를 제공하지 않으므로 `process` 표준 라이브러리(`process.start`, `process.wait` 등)는 Playground에서 사용할 수 없습니다.

### 네트워크 socket 사용 불가

WASI preview1은 socket API를 제공하지 않으므로 다음 표준 라이브러리는 Playground에서 사용할 수 없습니다:

- `net` (TCP listener / conn)
- `http` / `http2` / `http3` (HTTP client)
- `ws` (WebSocket)
- `tls` (TLS 연결)
- `quic`, `sse`, `dns`

**`no` / `lsp` 자체에 대한 제약(표준 라이브러리보다 더 엄격)**: wasm 산출물은 socket 함수를 "가져오기"조차 할 수 없습니다.
브라우저 측에서는 `@wasmer/wasi`로 인스턴스화하는데, 이에는 `sock_accept` / `sock_shutdown`이 구현되어 있지 않습니다.
따라서 산출물이 `net` / `net/http` 때문에 링크되면서 이 두 개의 `wasi_snapshot_preview1` 가져오기가 추가되는 순간,
`WebAssembly.Instance`가 즉시 예외를 던집니다:

```
Failed to instantiate WASI: RuntimeError: JsValue(LinkError: WebAssembly.Instance():
Import #28 "wasi_snapshot_preview1" "sock_accept": function import requires a callable)
```

결과적으로 전체 프로그램이 `_start`조차 실행하지 못합니다. 언어 서비스는 "초기화 실패"를 보고하고(자동 완성, 호버, 포맷팅 모두 불가),
실행 버튼도 마찬가지입니다. 따라서 네이티브 환경에서만 의미가 있는 네트워크 코드는 모두 `!wasip1` 파일에 넣고,
wasip1 측에는 오류 스텁만 남겨 둡니다: `cmd/lsp/pprof_wasip1.go`, `cmd/no/serve_esm_wasip1.go`,
`package/download_net_wasip1.go`, `package/release_net_wasip1.go`.
`bun scripts/wasm_imports_check.ts`(`make wasm-imports-check`)는 엔진 자체의 가져오기 표로 이 선을 지킵니다.

### `poll_oneoff`는 프런트엔드에서 보완

가져오기 표가 깨끗하다는 것은 "함수가 존재한다"는 뜻일 뿐, "함수가 터지지 않는다"는 뜻은 아닙니다. `@wasmer/wasi`의 `poll_oneoff` 본체는
Rust의 `unimplemented!()`입니다. 호출되는 순간(구독이 0개여도 마찬가지) `unreachable`을 실행해 전체 인스턴스를
trap시키며, 오류는 다음과 같습니다:

```
Error while running start function: RuntimeError: JsValue(RuntimeError: unreachable)
```

Go의 `runtime`은 `time.Sleep`과 `netpoll(delay)`를 모두 단조 시계 구독으로 바꾸므로,
**모든 Go 프로그램이 이 문제에 걸립니다**. `no fmt` / `no vet` / `no build`가 처음 잠드는 순간 모두 죽지만,
가져오기 표 검사로는 전혀 알 수 없습니다.

수정은 `docs/src/playground/wasm-loader.ts`에 있습니다. `instantiateWasi()`가 직접 `wasi.getImports()`로
가져오기 객체를 구성하고, `poll_oneoff`를 JS 구현으로 교체합니다(시계 구독 → 가장 가까운 만료 시각까지 잠든 뒤 clock 이벤트 보고;
fd 구독만 있는 경우 → 즉시 준비됨을 보고해 영구 블로킹을 피함). 덮어쓰기를
`wasi.instantiate(module, overrides)`에 넘기면 **안 됩니다**. glue 자체 함수가 우선하므로 덮어쓰기가 조용히 무시되고 프로그램은 여전히 trap됩니다.
또한 직접 가져오기 객체를 만들도록 바꿨기 때문에 `wasi.start(instance)`에 인스턴스를 명시적으로 전달해야 합니다.

### 첫 실행의 컴파일 시간

Run은 `no build --wasm-direct`로 wasm을 생성합니다. 실측상 wasm에서의 hello world는 약 **0.5초**
(네이티브 `no`의 같은 명령은 약 0.6초). 두 경우 모두 2026-11의 std 색인 사전 베이킹 이후이며, 이전에는 각각
약 55초와 11초였습니다.

원래 병목은 codegen이 아니라 **콜드 스타트마다 전체 std를 두 번 파싱하는 데** 있었습니다. `validateProgram`
의 `ValidateUndefinedVars`는 `math.max(...)` 같은 모듈 한정 호출을 알아보기 위해 119개 std 모듈 전체의
내보내기 기호를 수집하고, `CollectStdUnionAliases`는 같은 모듈들을 다시 파싱해 union 별명을 찾습니다.
이제 `genstdsig`가 "모듈별 내보내기 기호"와 "어떤 모듈에 union 별명이 있는지"를 함께 `stdsig_gen.go`에
구워 넣어 런타임에 표를 바로 조회합니다. 또한 두 가지 보호 장치(`hir.TestModuleExportsMatchChecker`는
모듈별·필드별로 HIR의 현재 계산 결과를 비교하고, `TestUnionAliasNarrowingMatchesFullScan`은 좁은 스캔과
전체 스캔을 비교합니다)가 있어 표가 낡으면 의미가 조용히 바뀌는 대신 테스트가 실패합니다.

워치독은 "로드 + 컴파일 180초 / 사용자 프로그램 5초"라는 두 단계 예산을 그대로 유지합니다. 전자는 지금 매우 넉넉한 여유가 있지만,
콜드 스타트, 대형 프로그램, 느린 기기의 꼬리 지연을 흡수하기 위해 남겨 두었습니다.

### 파일 시스템은 가상

WASI preview1은 stdin / stdout / stderr 세 개의 파일 디스크립터만 제공하며 실제 파일 시스템은 없습니다. 따라서 `fs`, `path`, `os.get-env`, `os.set-env` 등 파일이나 환경과 관련된 표준 라이브러리는 Playground에서 예상대로 동작하지 않을 수 있습니다.

### 메모리 상한

브라우저 WASM은 일반적으로 32비트 주소 공간 제한을 받아 사용 가능한 메모리 상한이 약 **2GB ~ 4GB**입니다(브라우저와 운영체제에 따라 다름). 많은 메모리가 필요한 프로그램(깊은 재귀, 대형 배열 등)은 out-of-memory로 실패할 수 있습니다.

## 브라우저 호환성

Playground는 WebAssembly를 지원하는 최신 브라우저가 필요합니다. 다음 버전 이상을 권장합니다:

| 브라우저 | 최소 버전 |
| --------- | -------- |
| Chrome    | 100+     |
| Firefox   | 100+     |
| Safari    | 16+      |
| Edge      | 100+     |

브라우저가 WebAssembly를 지원하지 않거나 JavaScript가 활성화되지 않은 경우 Playground는 편집기를 로드하거나 코드를 실행할 수 없습니다.

## 개발 및 검증

Playground를 수정한 뒤에는 깨끗한 빌드 트리에서 다음 검사를 실행할 수 있습니다:

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

`playground-demos`의 컴파일러는 **네이티브** `no`지만, 산출물은 브라우저 경로인 `--wasm-direct -target wasm32-wasi`를 따르고
실행은 `wasi-glue` + `wasm-loader`의 `instantiateWasi()`를 사용합니다. `runner.worker`와 완전히 동일한 코드이므로
wasmtime을 설치할 필요 없이 CI에서 바로 실행할 수 있습니다. 또한 세 가지를 동시에 검증합니다. 스니펫이 컴파일되는지, 산출물이 WebAssembly 검증을 통과하는지
(필드 값 위치 match 같은 빈 스택 오류가 여기서 드러납니다), 출력이 `expect`와 일치하는지(`?=`가 조용히 0을 얻는 종류의 백엔드 편차도 여기서 드러납니다).

이가 잡지 못하는 것은 오직 한 가지뿐입니다. `no.wasm` / `lsp.wasm` 자체의 가져오기 표(그 두 산출물은 스니펫에서 만들어지지 않음)는
`wasm-imports-check`가 보완합니다. `@wasmer/wasi`에 `sock_*`이 없으면 `WebAssembly.Instance`가 즉시 `LinkError`를 던져
전체 playground(언어 서비스 포함)가 시작조차 할 수 없습니다. worker 쪽에는 또 한 겹의 안전망이 있습니다. `wasm-loader.ts`의
`instantiateWasi()`가 `@wasmer/wasi`가 제공하지 않는 WASI 함수를 `ENOSYS` / `NOTSUP`를 반환하는 스텁으로 채워,
예상치 못한 가져오기가 서비스 전체가 죽는 대신 실패한 시스템 호출로 퇴화하게 합니다.

`wasm-imports-check`도 "함수는 존재하지만 본체가 trap하는 경우"(위의 `poll_oneoff` 같은 유형)는 잡지 못하므로,
`make wasm-runtime-smoke`가 마지막 칸을 채웁니다. 이 검사는 `instantiateWasi()`의 실제 경로로
`no version` / `no fmt` / `no vet`를 직접 실행하며, 언어 서비스와 전체 컴파일 + 실행까지 추가로 검증할 수 있습니다.

`docs/static/wasm/*.wasm`은 `.gitignore`로 제외되므로 로컬 산출물을 절대 커밋해서는 안 됩니다. `no.wasm`과 `lsp.wasm`은 CI가 동일한 소스 코드와 동일한 `StdSigKey`에서 함께 재빌드하므로, 브라우저의 컴파일러, 내장 std AST blob, Direct-WASM 백엔드가 같은 빌드에서 나옵니다. 세 가지의 버전 불일치가 바로 과거 `unreachable` 및 검증 오류의 흔한 원인이었습니다.

CI(`.github/workflows/build.yml`의 `pr-verify`, `static.yml`의 사이트 빌드)는 Go 테스트, 타입 검사, UI 검사, 예제 검증을 순서대로 실행한 뒤에야 깨끗한 트리에서 WASM을 재빌드하고 사이트를 배포합니다.
