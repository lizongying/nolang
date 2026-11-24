---
sidebar_position: 2
---

# 설치 및 사용

## 설치

### CLI 설치

[GitHub Releases](https://github.com/lizongying/nolang/releases/latest)에서 해당 플랫폼의 실행 파일을 다운로드하거나, 다음 방법으로 설치할 수 있습니다:

```bash
# macOS / Linux
# 1. 下載二進制文件
# 2. 放到 PATH 中
sudo mv nolang /usr/local/bin/no
```

### VS Code 확장 설치

[VS Code Marketplace](https://marketplace.visualstudio.com/items?itemName=lizongying.vscode-nolang)에서 Nolang 확장을 설치하면 구문 강조, LSP 진단, 정의로 이동, 자동 완성 등을 지원합니다.

## CLI 명령

| 명령                                                         | 설명                    |
| ------------------------------------------------------------ | ----------------------- |
| `no version`                                                 | 버전 정보 출력            |
| `no init`                                                    | 작업 영역 정의(workspace.noml 생성, package.noml은 생성하지 않음) |
| `no new <name>`                                              | 작업 영역 내에 새 패키지 생성(하위 디렉터리 + package.noml, workspace.noml에 등록) |
| `no fmt [-w] [-d] <file\|dir>`                               | 소스 코드 포맷(가져오기 중복 제거/정렬 포함, 별칭 없는 std 모듈 가져오기 자동 제거); `.noml` 설정 파일은 레이아웃을 보존하는 형식 규칙 적용(들여쓰기/공백/빈 줄은 유지, 주석과 키 순서는 변경하지 않음) |
| `no build [-o <file>] [-cc <s>] [-target <s>] [-opt[=N]] [<file\|dir>]` | 빌드(executable 출력) |
| `no run [-cc <s>] [-target <s>] [<package\|dir\|file>]`        | 빌드 후 실행(패키지명/디렉터리/파일) |
| `no test [-cc <s>] [-target <s>] [<file>]`                   | 테스트 실행                |
| `no add <tool>[:<version>]@<host>/<owner>/<repo>`            | 의존성 추가(사전 컴파일된 AST 모듈)|
| `no remove <pkg>`                                            | 의존성 제거                |
| `no update <pkg>`                                            | 의존성 업데이트                |
| `no update-all`                                              | 모든 의존성 업데이트            |
| `no list`                                                    | 의존성 목록                |
| `no sync`                                                    | 의존성 동기화                |
| `no install [-u] [<tool>[:<version>]@<host>/<owner>/<repo>]`  | binary 설치             |
| `no uninstall <name>`                                        | binary 제거             |
| `no pack [-o <file>] [<dir\|file.no>]`                       | AST 패키징(`<tool>-ast.zip`)|
| `no pub [-o <file>]`                                         | `no pack`과 동일(registry 업로드는 미구현)|
| `no cache <stat\|clear>`                                     | AST 캐시 확인 / 삭제(정상적인 경우 삭제할 필요 없음)|

## 빠른 시작

Nolang는 **단일 저장소(프로젝트) 다중 패키지** 구조를 사용합니다. 저장소 루트 디렉터리가 작업 영역(`workspace.noml`만 있음)이고, 각 패키지는 하위 디렉터리(자체 `package.noml` 보유)입니다. `no init`과 `no new`는 서로 별개의 작업입니다:

- `no init` —— 현재 디렉터리에 작업 영역을 정의합니다(`workspace.noml`만 생성).
- `no new <name>` —— 현재 작업 영역 아래에 새 패키지를 생성합니다(하위 디렉터리 `./<name>/` 및 해당 `package.noml` 생성)하고, 그 패키지를 `workspace.noml`에 등록합니다.

### 작업 영역 초기화 및 패키지 생성

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

작업 영역 내에서 `no new`를 반복 사용해 패키지를 더 추가할 수 있습니다:

```bash
no new bar
no new baz
```

이때 `workspace.noml`은 다음과 같은 형태입니다:

```noml
{
  foo: '/foo'
  bar: '/bar'
  baz: '/baz'
}
```

> **경로 형식**: `workspace.noml`의 매핑 값은 `/` 접두사로 작업 영역 상대 경로를 나타냅니다(예: `/foo` → `workspaceRoot/foo`). `./`, `../` 상대 이동 및 운영 체제 절대 경로(예: `/home/user/code/fork`)도 지원하여 작업 영역 외부 디렉터리를 가리킬 수 있습니다.

`no build`를 루트 디렉터리에서 인자 없이 실행하면 작업 영역 내 모든 패키지를 병렬로 빌드합니다.

### 작업 영역만 초기화(패키지 생성 없이)

```bash
# 在當前目錄定義工作區
no init
```

`no init`은 `workspace.noml`(처음에는 빈 `{}`)만 생성하며, **`package.noml`이나 `main.no`는 생성하지 않습니다**. `workspace.noml`이 이미 있으면 덮어쓰지 않습니다. 패키지는 `no new <name>`으로 생성해야 합니다.

### 빌드와 실행

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

> `no build -v`는 출력 경로 옆에 원시 LLVM IR(`<輸出路徑>.ll`)을 추가로 기록합니다. `no build --shared -o libdemo.dylib main.no`는 `#{cabi='symbol'}`로 표시된 최상위 함수를 안정적인 C ABI 기호로 내보내 `-shared -fPIC`로 동적 라이브러리로 링크하며, 같은 이름의 `.h` 헤더 파일을 자동 생성합니다. `--header`로 헤더 경로를 덮어쓸 수 있습니다. 현재 `-c`(컴파일만 하고 링크하지 않음) 옵션은 없습니다. C / C++ / Rust / Go / Java / Python / C#에서 Nolang를 호출하는 전체 방법은 [호스트 언어에서 Nolang 호출](./lang/host-interop)을 참조하세요.

`no run`의 위치 인자는 다음 순서로 해석됩니다:

1. 이미 존재하는 `.no` 파일 → 해당 파일을 직접 실행
2. 이미 존재하는 디렉터리 → 그 안의 `main.no` 실행
3. 작업 영역(가장 가까운 `workspace.noml`)에 등록된 패키지명 → 해당 패키지의 `main.no` 실행

인자가 없으면 현재 디렉터리의 `main.no`를 기본으로 실행합니다. 현재 디렉터리가 작업 영역 루트(`workspace.noml`은 있지만 `main.no`는 없음)이면 `no run <package>`로 패키지를 지정하라는 메시지가 표시됩니다.

### 크로스 컴파일 대상

`-target` 인자 형식은 `<arch>-<os>-<abi>`이며, 다음 대상을 지원합니다:

| 대상 삼중항           | 설명           |
| -------------------- | -------------- |
| `x86_64-linux-gnu`   | Linux x86_64   |
| `aarch64-linux-gnu`  | Linux ARM64    |
| `x86_64-macos-gnu`   | macOS x86_64   |
| `aarch64-macos-gnu`  | macOS ARM64    |
| `x86_64-windows-gnu` | Windows x86_64 |

**현재 플랫폼 자동 감지**: `no build`, `no run`, `no test`는 `-target`을 지정하지 않으면 현재 호스트 플랫폼을 자동으로 감지해 네이티브 코드로 컴파일합니다. 일상 개발에서는 target을 수동으로 지정할 필요가 없습니다:

```bash
no run hello.no          # 本機直接跑
no test                  # 本機跑測試
no build -target aarch64-linux-gnu   # 需要交叉編譯時才顯式指定
```

### 컴파일러 선택

`-cc` 인자는 C 컴파일러 백엔드를 지정합니다:

- `clang`(기본값) — LLVM 설치 필요
- `zig` — Zig 설치 필요, 크로스 컴파일에 적합

### 내장 타입 레이아웃(`?T` 인라인 임계값)

`?T`의 메모리 레이아웃은 **페이로드 타입에 따라 결정**됩니다. 각 페이로드 타입 `T`마다
`%option_<key> = { i8 tag, [K x i64] }`가 하나씩 있습니다(1바이트 태그, 페이로드는 바이트 오프셋 8에 고정,
`K = roundUp8(sizeof(T))/8`). 페이로드가 임계값에 맞지 않으면 힙 박싱으로 전환하고 페이로드 영역 첫 단어에 포인터를 저장합니다.

`--option-inline-threshold=N`은 이 임계값을 지정합니다(**전체 option 구조**의 바이트 수로 계산,
즉 `1 + 7 + sizeof(T)`):

```bash
no build --option-inline-threshold=1032 main.no   # 讓更大的載荷內聯
no build --option-inline-threshold=16 main.no     # 最小可配置值
```

| 값 | 효과 |
| --- | --- |
| 기본값 **264**(= 1 + 7 + 256) | `?i64` 16 B, `?str` 24 B, `?txt`(256 B) 264 B 인라인; 더 큰 페이로드는 박싱 |
| 최소 **16** | `?i64` 종류만 인라인 유지; `?str`(24 B)부터 전부 박싱 |
| < 16 | 컴파일 오류 |

우선순위: 명령줄 > 환경 변수 `NOLANG_OPTION_INLINE_THRESHOLD` > `package.noml`의
`compiler.option-inline-threshold` > 기본값. 레이아웃의 유일한 출처는 `src/mir/layout.go`이며,
의미론적 측면(어떤 값이 `ok`／`nil`／`err`로 간주되는지)은 **영향을 받지 않고**, 메모리 점유와 박싱 여부만 달라집니다.

### 컴파일러 내장 최적화기(MIR opt)

컴파일러에는 **MIR 계층**(HIR 강하 후, LLVM 코드 생성 전)에서 동작하는 최적화기가 내장되어 있습니다. 이는 백엔드의 기존 LLVM `opt`와는 별개의 것으로, 전자는 MIR을 재작성하고 후자는 이미 생성된 LLVM IR을 재작성합니다.

`-opt[=N]`로 켜며, **기본값은 꺼짐**:

```bash
no build main.no          # 預設：不啟用（與歷史行為一致）
no build -opt main.no     # 等同 -opt=1
no build -opt=2 main.no   # 常數折疊 + 死碼消除 + 控制流清理
no build -opt=0 main.no   # 顯式關閉
```

| 수준 | 의미 |
| ---- | ---- |
| `0` / `off` / `false` / `no` / `none` | 꺼짐(기본값) |
| `1` / `on` / `true` / `yes` / `-opt` | 상수 폴딩 + 정수 항등식 재작성 + 복사 전파 + 죽은 스칼라 제거 |
| `2` | 수준 1 + 제어 흐름 정리(상수 분기 폴딩, 도달 불가 블록 삭제, 빈 블록 점프 연결, 단일 전임 블록 병합) + 프롤로그 가지치기 |

`-opt`는 **전역** 스위치(`-v`와 마찬가지로 `main`에서 처리)이므로 `build`, `run`, `test` 모두 지원합니다.

동일한 스위치를 환경 변수 `NOLANG_MIR_OPT`로도 지정할 수 있습니다. 명령줄 플래그는 단지 이 변수에 값을 쓸 뿐이므로 둘 사이에 불일치가 생기지 않습니다. 이번 최적화가 무엇을 했는지 확인하려면:

```bash
NOLANG_MIR_OPT=2 NOLANG_MIR_OPT_STATS=1 no build -o out main.no
# [miropt] level=2 funcs=3 folded=5 (arith=2 cmp=3) ident=1 copies=4 dead=6 cfg{condbr=3 unreachable=4(5 insts) threaded=1 merged=5}
# [miropt] prelude: dropped 27 unreferenced helpers
```

| 필드 | 의미 |
| ---- | ---- |
| `folded`(`arith`/`cmp`) | 상수로 폴딩된 산술／비교 명령 수 |
| `ident` | 어떤 피연산자로 간소화된 정수 항등식(`x+0`, `x*1`, `x&x`, `x^0`…) |
| `copies` | 복사 전파로 제거된 스칼라 복사 |
| `dead` | 제거된 죽은 스칼라 명령 |
| `cfg{condbr, unreachable, threaded, merged}` | 상수 분기 폴딩, 도달 불가 블록 삭제(연관 명령 수 포함), 빈 블록 점프 연결, 단일 전임 블록 병합 |
| `prelude: dropped N` | codegen 프롤로그에서 아무도 참조하지 않는 것으로 확인된 runtime helper 수(아래 참조) |

### 프롤로그 가지치기(수준 2)

`emitPrelude`는 프로그램이 사용하는지 여부와 관계없이 전체 런타임(`str_*`, `print_*`, `eprint_*`, `vec_*`, `nolang_*`)을 **모든** 모듈에 기록합니다. `print('hi')` 프로그램은 60개의 함수 정의를 생성하지만, 그중 진입점에서 도달 가능한 것은 8개뿐이고 **52개는 한 번도 호출되지 않습니다**.

**이 작업은 `opt -O2`로는 할 수 없습니다.** LLVM의 GlobalDCE는 `internal` 정의만 삭제하지만, 프롤로그는 external linkage로 방출됩니다(`no` 모듈은 C와 링크될 수 있고, 링커가 필요로 할 수 있는 helper는 internal이 될 수 없습니다). 해당 프로그램에서 실측한 결과 60개 정의 중 정확히 7개가 `define internal`이었고, `opt -O2`는 정확히 그 7개만 삭제했으며, `.ll`은 오히려 61,812바이트에서 69,874바이트(+8,062)로 늘어났습니다.

따라서 수준 2는 IR 생성이 완료된 뒤 **완성된 텍스트**에 대해 도달 가능성 가지치기를 한 번 수행하여 실제로 참조되는 helper만 남깁니다(helper들이 자체적으로 호출하는 것도 재귀적으로 함께 유지합니다). 동일한 `print('hi')`에서 정의 수는 60 → **16**, `.ll`은 61,812 → **14,548**바이트(**−76.5%**), 실행 파일은 36,616 → 34,648(−5.4%)이 됩니다. 가지치기는 진짜 참조만 인식합니다. 즉 **주석에 등장하는 이름은 계산하지 않습니다**. 이것이 33개가 아니라 44개를 삭제할 수 있는 핵심이며, 함수를 제거할 때 **그 함수 자신의 설명 주석도 함께 제거합니다**(`optTrailingComment`). 그렇지 않으면 이미 존재하지 않는 44개 함수를 설명하는 12,609바이트가 남게 됩니다.

주석을 삭제해도 **실행 파일 크기는 줄어들지 않습니다.** 주석은 `opt`/`llc`가 파싱할 때 버려지므로, 둘 다 실행 파일은 34,648바이트입니다. 그 12,609바이트는 IR 텍스트와 컴파일 타임 파싱량 측면의 이득입니다.

가지치기의 근사치는 항상 보존 쪽으로 기울어집니다. 유일한 이름을 확인할 수 없는 블록은 보존하고, 블록에 닫는 중괄호가 없으면 그 지점에서 가지치기를 멈추며, `declare`／전역／타입 정의는 절대 삭제하지 않습니다. 최적화를 하나 놓치면 몇 바이트 더 커질 뿐이지만, 잘못 삭제하면 링크 오류가 발생합니다.

**프롤로그 전체를 제거할 수 있을까요? 없습니다.** 프롤로그 구간에는 함수뿐 아니라 **프로그램 자체의 문자열 리터럴**(`@.mir.str.N`, 즉 `"hi"` 자체), `%str-long`／`%option`／`%task` 같은 타입, `@malloc`／`@write`／`@llvm.memcpy` 같은 외부 선언도 있습니다. 이런 데이터 영역은 모듈의 약 30%를 차지하며, 삭제하면 `llc`조차 통과하지 못합니다(`error: use of undefined value '@.mir.str.1'`). 삭제할 수 있는 것은 **함수** 절반뿐입니다. 48개의 `define`을 같은 이름의 `declare`로 바꾸면 링커가 보고하는 미정의 기호는 정확히 `print_nl`, `print_str`, `str_free`, `str_from_const` 네 개입니다. 따라서 올바른 질문은 "제거할 수 있는가"가 아니라 "필요할 때만 방출할 수 있는가"입니다.

이 낭비는 **프로그램 크기와 무관한 고정 세금**입니다. 프롤로그의 코드와 주석은 모듈마다 동일합니다(48개 정의, 코드 36,043 + 주석 17,516바이트). 40개 파일을 표본 추출한 결과 보존량은 **0~19개, 중앙값 9**이었습니다. `tests/dup.no`／`tests/q.no`／`tests/u128-types.no`는 **0개**를 보존했습니다. 즉 프롤로그 전체가 이 파일들에는 완전히 죽은 코드입니다. 따라서 절약 비율은 전적으로 모듈 크기에 따라 결정됩니다. 한 줄짜리 프로그램은 −76.5%, 3.0 MB의 `test/std/pbkdf2.no`는 고작 −4.1%입니다.

프롤로그 전체를 제거할 수 있을까요? 없습니다. 구간 안에는 프로그램 자체의 문자열 리터럴(`@.mir.str.N`), 타입, 외부 선언(1,151바이트)이 있어 삭제하면 `llc`조차 통과하지 못합니다. 더구나 `codegen.go:75-83`의 `extraFuncs`／`extraFuncsBody`는 원래 온디맨드 방출기(option／struct／enum／ptr의 helper가 모두 이를 거침)이며, 프롤로그가 유일하게 변환되지 않은 부분입니다. 하지만 이를 온디맨드로 바꿔 추가로 얻을 수 있는 것은 거의 전부 주석이고, 코드 측면에서는 이미 가져올 것이 없습니다.

이 최적화기는 **스칼라**(`i64`/`u64`/`i16`/`i32`/`char`/`bool`/`f32`/`f64`)만 폴딩합니다. `byte`/`u8`/`i8`, `i128`/`u128`, `option`, 슬라이스, 구조체 등은 모두 건너뜁니다. 전자는 `emitConst`에 대응하는 분기가 없어 0으로 퇴화하고, 후자의 산술은 `option` 오버플로 래핑 경로를 타므로 폴딩하면 의미가 바뀝니다. 부동소수점 역시 모든 항등식에서 제외됩니다. `x * 0.0`은 `inf`／`NaN`에 대해 0이 아니고, `x + 0.0`은 `-0.0`에 대해 `x`가 아닙니다.

그 비교의 핵심 결론을 먼저 짚고 가겠습니다. **지역 정리(상수 폴딩, 죽은 코드 제거, 제어 흐름 정리)는 `opt -O2`가 실행된 뒤에는 한계 효용이 거의 0**(−0.06%)입니다. LLVM이 같은 종류의 작업을 더 철저히 하기 때문입니다. 진짜 보상이 되는 것은 LLVM이 **linkage 때문에 하지 못하는** 일, 곧 위의 프롤로그 가지치기입니다. 프롤로그 가지치기(주석 인식 가지치기 포함)를 더하면 `mir+llvm_O2` → `mir_opt+llvm_O2`의 전체 한계값은 **−19.05% 명령 / −15.83% 텍스트**이고, `mir` → `mir+mir_opt`의 텍스트 감소율은 −14.29%에서 **−17.75%**로 커집니다(631개 파일 전체 코퍼스).

## JS 백엔드

JS 백엔드(`no build --js` / `no run --js`)는 `.no` 소스를 JavaScript로 컴파일합니다(타입 소거).
Node.js와 브라우저 두 대상, `#{js}` / `#{js-browser}` 플랫폼 애노테이션, 서드파티 ESM 가져오기
(`# npm/<pkg>`)를 지원합니다.

JS 관련 전체 문서(명령줄 인자, 내장 함수 매핑, `js/*` 표준 라이브러리, 브라우저 import map 등)는
별도의 [JS 백엔드](./lang/js-backend) 문서에 모아 두었으며, 이 페이지에서는 반복하지 않습니다.

## 진입점 규칙

- **main.no** — 프로그램 진입점
- **lib.no** — 라이브러리 진입점, 함수 내보내기(자세한 내용은 [내보내기 문서](lang/export) 참조)
- **tests/ 디렉터리의 모든 .no 파일** — 테스트 단언 포함

## 테스트

```bash
# 測試tests目錄下所有 .no 文件
no test

# 執行單個測試文件
no test my-test.no

# 使用指定編譯器或目標
no test -cc zig
no test -target x86_64-windows-gnu
```

테스트 설명:

- 테스트 파일은 tests/ 디렉터리에 모아 둡니다
- 각 테스트 파일은 독립적으로 빌드됩니다
- 하나라도 테스트가 실패하면 0이 아닌 종료 코드를 반환합니다

## AST 캐시

`no build` / `no run` / `no test`는 각 사용자 소스 파일을 파싱해 얻은 AST를 디스크에 저장합니다
(기본값 `~/.cache/nolang/ast`, `$NOLANG_AST_CACHE_DIR`로 경로 변경 가능). 다음에 내용이 같으면 그대로 재사용하여
lex + parse를 생략합니다. 표준 라이브러리는 컴파일러에 내장된 blob을 사용하므로 이 캐시를 차지하지 않습니다.

**수동으로 정리할 필요가 없습니다.** 캐시 키는

```
sha256( ASTVersion ‖ 解析期 salt ‖ 源文件內容 )
```

여기서 salt의 한 항목이 바로 `checker.StdSigKey`입니다. 즉 `src/hash.noml`에 열거된 모든 소스 파일
(표준 라이브러리 + 컴파일러 자체)의 내용 서명입니다. 따라서 컴파일러나 표준 라이브러리가 조금이라도 바뀌면 `make`로 다시 빌드한
`no`가 새로운 키를 갖게 되어, 오래된 항목은 자연스럽게 miss가 나고 재사용되지 않습니다.

| 명령 | 설명 |
| --- | --- |
| `no cache stat` | 캐시 디렉터리, 항목 수, 전체 크기 및 현재 `ASTVersion` 표시 |
| `no cache clear` | 모든 항목 삭제(응급 시에만: 항목이 오래된 것이 의심될 때) |

환경 변수:

| 변수 | 역할 |
| --- | --- |
| `NOLANG_AST_CACHE_OFF=1` | 캐시 끄기(컴파일러 A/B 비교, "고친 오류가 다시 나타나는" 문제 조사 시 사용 가능) |
| `NOLANG_AST_CACHE_DIR` | 캐시 디렉터리 사용자 지정. **동시에** 사전 컴파일 AST 의존성의 `-ast.zip` 캐시에도 적용됨(해당 캐시 기본 위치는 `~/no/cache/ast`, 아래 "사전 컴파일 AST 의존성" 참조) |
| `NOLANG_AST_CACHE_MAX` | 캐시 항목 수 상한(기본값 4096) |

> 오래된 항목은 단지 더 이상 hit되지 않을 뿐이며, 새 항목이 기록될 때 상한에 따라 제거되므로 `~/.cache/nolang/ast`를 **삭제할 필요가 없습니다**.
> `no`는 반드시 `make gen` / `make no`로 재빌드해야 합니다. `cd src && go build`로 직접 컴파일한 바이너리에는
> `StdSigKey`가 주입되지 않으므로, 캐시를 조용히 재사용하지 않고 곧바로 오류를 내고 종료합니다.

## Binary 설치 및 제거

### 설치

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

원격 설치는 소스가 아닌 **사전 컴파일된 바이너리**를 가져옵니다. 자산 이름은 `<tool>-<os>-<arch>`이며(Windows는
`.exe` 포함), 여기서 `<os>-<arch>`는 `no` 자체가 실행되는 플랫폼(예: `darwin-arm64`,
`linux-amd64`)에서 가져옵니다. 따라서 로컬 LLVM 도구 모음이 필요 없고, 설치되는 도구가 Nolang로 작성되었을 필요도 없습니다.
버전을 생략하면 `releases/latest`를 조회합니다. `$GITHUB_TOKEN`(또는 `$GH_TOKEN`)을 설정하면 GitHub
API 제한을 피할 수 있습니다.

설치 절차:

1. 원격: spec 해석 → release 자산 위치 확인 → 다운로드; 로컬: 현재 디렉터리로 빌드
2. binary를 `~/no/bin/`에 기록
3. `/usr/local/bin/`에 심볼릭 링크 생성

### 제거

```bash
no uninstall pkg-name
```

제거하면 `/usr/local/bin/`의 심볼릭 링크와 `~/no/bin/`의 binary가 삭제됩니다.

## 프로젝트 설정

### noml 설정 형식

Nolang의 모든 설정 파일(`package.noml`, `workspace.noml`, `.workspace.noml`, `nolang.lock.noml`, `sum.noml`)은 자체 개발한 **noml**(Nolang Markup Language) 형식을 사용하며, 기존 JSONC를 대체합니다. 문법 규칙은 Nolang 언어 자체와 맞춰져 있습니다:

| 규칙 | 설명 |
| --- | --- |
| 최상위 객체 | 하나의 객체입니다. `{`와 `}`는 각자 한 줄을 차지하고, 멤버는 한 줄에 하나씩, **쉼표 구분자가 없습니다**(줄바꿈이 곧 구분) |
| 키 | 기본적으로 벌 식별자이며, 규칙은 Nolang 변수명과 같습니다. 소문자/숫자/중간의 하이픈/밑줄로 시작하고 숫자로 시작할 수 없습니다(예: `link-libs`, `anonymous-fn-type`, `dev-dependencies`). 규칙을 만족하지 않는 키(경로, URL, `@scope/pkg`)는 작은따옴표 문자열 키로 씁니다 |
| 문자열 값 | 작은따옴표, `\` 이스케이프: `\' \\ \n \t` |
| 불리언 | `true` / `false`(모두 소문자, Nolang bool 리터럴과 동일) |
| 숫자 | 벌 리터럴(록 파일 등용으로 예약) |
| 배열 | `[`, 요소, `,`, `]` 모두 여러 줄에 걸릴 수 있습니다. 요소는 **쉼표**로 구분하고 **후행 쉼표를 지원**합니다. 빈 배열은 `[]`로 씁니다 |
| 주석 | `;`부터 줄 끝까지(문자열 내부 제외) |
| 들여쓰기 | 의미 없음(가독성에만 영향, 포맷 보장 없음) |

`dependencies` / `dev-dependencies`는 **문자열 배열**이며, 요소는 `'키:버전'` 형태입니다(버전은 생략 가능하며 기본값은 `*`). 예:

```noml
{
  dependencies: [
    '/example/test2',                          ; 本地包，默認任意版本
    'github.com/lizongying/nolang/test1:v0.1.0', ; 線上包，指定版本,
  ]
}
```

프로젝트 루트 디렉터리의 `package.noml` 파일은 프로젝트 정보를 기술합니다:

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
  mirrors: []
  dependencies: [
    'fmt',,
  ]
  compiler: {
    version: 'v0.1.0'
  }
  output: './dist'
  ignore: []
}
```

`compiler.lang`은 이 프로젝트의 메시지 언어를 고정합니다(`'zh-tw'`, `'ja'` 등, 표기법은 `--lang`과 동일). 시스템 locale보다 우선하지만
`--lang`과 `NO_LANG`보다는 낮습니다. 쓰지 않으면 시스템을 따릅니다.

### 의존성 관리

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

### 사전 컴파일 AST 의존성

`no add <tool>@<host>/<owner>/<repo>` 형태의 의존성은 **AST만 있고 소스는 없습니다**. 컴파일 시 release에서
`<tool>-ast.zip`(zip: one .gob entry per module)을 가져와 디코딩한 뒤 곧바로 컴파일 파이프라인에 주입합니다. 사용법:

```nolang
# nogit                      # 패키지 별칭; 모듈은 nogit.<module> 로 참조
# nogit/remote                  # 모듈 remote 를 직접 import (nogit.remote 도 가능)
# nogit.fn                      # nogit 은 패키지 이름일 뿐; fn 을 정의한 모듈을 찾음
# <host>/<owner>/<repo>/nogit/remote
```

하나의 zip은 하나의 패키지이며 여러 모듈을 포함할 수 있습니다(각 모듈은 `<modulePath>.gob` 항목). 진입 모듈은 없습니다: `# nogit`은 패키지 별칭만 만들며 모듈은 `nogit.<module>`로 참조합니다(예: `nogit.remote.greet(...)`). `# nogit/remote`는 모듈을 직접 import하고, `# nogit.fn`은 함수로 모듈을 찾습니다. `no fmt`는 사용하지 않는 `# nogit` / `# nogit/remote`를 정리합니다. 버전은 구체적인 값이어야 합니다
(`no sync` / 컴파일 시 `""` 또는 `"*"`를 만나면 오류가 발생합니다). zip은 `~/no/cache/ast/`에 캐시되며
`$NOLANG_AST_CACHE_DIR`로 덮어쓸 수 있습니다.

산출물은 `no pack`으로 만듭니다(lexer + parser만 필요하고 LLVM 도구 모음은 불필요하여 CI에 적합):

```bash
no pack -o nogit-ast.zip nogit.no      # 單個源文件
no pack -o nogit-ast.zip ./nogit       # 一個包目錄
```

### 의존성 유형과 버전 번호 규칙

`package.noml`의 의존성은 **로컬 패키지**와 **온라인 패키지** 두 종류로 나뉩니다. 컴파일러는 다음 규칙에 따라 의존성 유형을 자동으로 판단하며, 로컬 패키지가 `"*"` 버전을 사용하지 않으면 경고를 출력합니다.

#### 판정 규칙

```
依賴鍵 → 查找 workspace.noml（短名稱或完整鍵匹配）
  ├─ 找到 → 本地包（應使用 "*"）
  └─ 未找到 → 檢查路徑前綴（/ 開頭）
      ├─ 是 → 本地包（應使用 "*"）
      └─ 否 → 線上包（使用版本號，不警告）
```

1. **`workspace.noml` 검색**: 먼저 의존성 키를 짧은 이름 또는 전체 키로 `workspace.noml`에서 찾습니다. 찾으면 로컬 패키지입니다.
2. **경로 접두사 확인**: `workspace.noml`에서 찾지 못했을 때, 의존성 키가 `/`(작업 영역 상대 경로)로 시작하면 역시 로컬 패키지입니다.
3. **나머지는 온라인 패키지**: 위 조건을 만족하지 않는 의존성 키는 온라인 패키지로 간주하며 버전 번호를 지정해야 합니다.

> **작업 영역 경계 제약**: `package.noml`의 로컬 경로는 `/`로 시작해야 하며(작업 영역 상대), `./`, `../` 및 운영 체제 절대 경로는 금지됩니다. 작업 영역 경계를 넘을 수 있는 유일한 통로는 `workspace.noml` / `.workspace.noml` 매핑입니다. 자세한 내용은 [작업 영역 경계 제약](#工作區邊界約束)을 참조하세요.

#### 로컬 패키지의 다양한 참조 형식

로컬 패키지는 다음 세 가지 참조 방식을 지원하며, 버전 번호는 모두 `"*"`를 사용해야 합니다:

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

> **참고**:
> - 세 번째 형식이 로컬 패키지인지는 `workspace.noml`에 대응하는 매핑이 있는지에 따라 달라집니다. `workspace.noml`에서 `github.com/lizongying/nolang/test2`를 로컬 경로로 매핑했다면 해당 의존성은 로컬 패키지(`"*"` 사용)이고, 그렇지 않으면 온라인 패키지(버전 번호 사용)입니다.
> - `./` 및 `../` 접두사는 더 이상 `package.noml`에서 사용할 수 없습니다. 모든 로컬 경로는 `/`로 시작해야 합니다(작업 영역 상대).

#### 고급 사용법: 온라인 패키지를 로컬 패키지로 전환

`workspace.noml`은 짧은 이름을 등록할 수 있을 뿐 아니라 온라인 패키지명을 로컬 경로에 매핑하여 "온라인 패키지를 로컬 패키지로 전환"하는 개발 모드를 구현할 수도 있습니다.

```noml
; workspace.noml
{
  test2: '/example/test2'
  'github.com/lizongying/nolang/test2': '/example/test2'
}
```

이때 `package.noml`에서 `github.com/lizongying/nolang/test2`로 참조한 의존성은 로컬 패키지 `/example/test2`로 해석되며 버전 번호는 `"*"`를 사용해야 합니다. 이는 온라인 패키지를 로컬에서 개발할 때 매우 편리합니다. `workspace.noml`에 매핑 하나만 추가하면 원격 의존성을 로컬 소스로 전환해 함께 디버깅할 수 있습니다.

#### 버전 번호 경고

컴파일 시 로컬 패키지가 `"*"`가 아닌 버전 번호(예: `"v0.1.0"`)를 사용한 것이 감지되면 `"*"`를 사용하라는 경고가 출력됩니다. 온라인 패키지는 이 제약을 받지 않습니다.

#### 재귀 작업 영역 매핑(패키지 간 매핑 체인)

Nolang는 **재귀 작업 영역 매핑**을 지원합니다. 의존 대상 패키지가 자체 `workspace.noml`을 가질 수 있고, 그 안에도 매핑 규칙이 계속 존재하여 자연스러운 패키지 간 해석 체인을 형성합니다. 이는 Nolang의 핵심 차별화 능력입니다. 주류 Go/Cargo의 `replace`/`patch`는 현재 프로젝트에만 적용되고 의존 패키지 내부로 전달되지 않습니다.

**적용 시나리오:**
- 기반 라이브러리의 내부 별칭 통일
- 가져오기 경로 표준화
- 다층 라이브러리 호환 마이그레이션

**동작 원리:**

의존성 키를 해석할 때 컴파일러는 다음과 같이 동작합니다:
1. 현재 `workspace.noml`에서 키를 찾습니다
2. 매핑을 찾으면 대상 디렉터리가 자체 `workspace.noml`을 가지는지 확인합니다
3. 있다면 같은 키를 기준으로 계속 찾아가며 단계별로 해석합니다
4. 더 이상 깊은 매핑이 없을 때까지 반복합니다

**예제:**

```
workspace/                  ← 頂層工作區
├── workspace.noml         ← {"testkey": "/pkgA"}
├── pkgA/
│   ├── workspace.noml     ← {"testkey": "/pkgB"}
│   └── pkgB/
│       ├── workspace.noml ← {"testkey": "/pkgC"}
│       └── pkgC/           ← 最終目標（無 workspace.noml）
```

`testkey`를 해석할 때:
1. 최상위 `workspace.noml` 매핑 → `/pkgA`(작업 영역 상대)
2. `pkgA/workspace.noml` 매핑 → `/pkgB`(pkgA 작업 영역 상대)
3. `pkgB/workspace.noml` 매핑 → `/pkgC`(pkgB 작업 영역 상대)
4. `pkgC`에는 `workspace.noml`이 없음 → 최종적으로 `pkgC`로 해석

**순환 감지:**

컴파일러는 방문한 작업 영역 루트 디렉터리를 추적하는 해석 방문 스택을 유지합니다. 순환 매핑(예: A→B, B→A)이 감지되면 즉시 오류를 내고 전체 체인을 출력합니다:

```
Error: circular workspace mapping detected: /path/to/A → /path/to/B → /path/to/A
```

> **참고**: 이 기능은 패키지 간 자연스러운 재귀 매핑(의존 패키지가 자체 `workspace.noml`을 가짐)만 지원하며, 단일 파일에서 수동으로 체인을 축약하는 방식은 지원하지 않습니다.

### 미러 설정

`package.noml`의 `mirrors` 배열에 미러 주소를 설정하여 원격 패키지 다운로드를 빠르게 할 수 있습니다:

```noml
mirrors: [
  'https://mirror.example.com/',,
]
```

### 작업 영역 설정 workspace.noml

`workspace.noml`은 저장소 루트 디렉터리에 있으며, **단일 저장소 다중 패키지** 작업 영역을 기술하는 "패키지명 → 상대 경로" 매핑입니다. `no build`는 인자 없이 실행하고 `workspace.noml`이 존재하면 그 안에 나열된 모든 패키지를 병렬로 컴파일합니다. `no run`은 패키지명(예: `no run foo`)으로 작업 영역의 단일 패키지를 실행합니다.

```noml
{
  foo: '/foo'
  bar: '/bar'
}
```

`no init`은 작업 영역을 정의합니다. `workspace.noml`이 없으면 빈 객체 `{}`를 생성하고, **어떤 `package.noml`도 생성하지 않습니다**. 이미 있으면 그대로 두고 덮어쓰지 않습니다. 이후 `no new <name>`으로 패키지를 생성하면 `"<name>": "./<name>"`이 `workspace.noml`에 자동 등록됩니다.

#### 로컬 전용 설정 .workspace.noml

공용 `workspace.noml` 외에도 Nolang는 로컬 전용 설정 파일 `.workspace.noml`(앞에 `.`이 붙음에 주의)을 지원합니다. 이 파일은 개발자 개인의 임시 의존성 교체용으로 `.gitignore`에 추가하는 것이 좋습니다.

**로드 논리:**

1. 공용 설정 `workspace.noml`을 먼저 로드합니다
2. 그다음 전용 설정 `.workspace.noml`을 로드합니다
3. **같은 키**: 전용 설정이 공용 설정을 덮어씁니다
4. **새 키**: 서로 합쳐집니다

```
workspace/
├── workspace.noml       ← 工程共享配置（納入版本管理）
├── .workspace.noml      ← 本機私有配置（建議加入 .gitignore）
├── foo/
└── bar/
```

**사용 시나리오:**

- 팀이 공유하는 별칭과 의존성 전달 규칙은 `workspace.noml`에 작성하여 버전 관리에 포함합니다
- 개발자가 로컬에서 디버깅할 때 특정 의존성을 로컬 fork로 임시로 가리켜야 하면 `.workspace.noml`에서 덮어쓰면 됩니다
- 임시 fork 매핑이 프로젝트 공용 설정을 오염시키는 것을 막아, 대규모 팀 협업과 개발자 로컬 개발의 유연성을 모두 충족합니다

**예제:**

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

최종 적용되는 매핑: `foo` → `/my-fork-foo`(전용 덮어씀), `bar` → `/bar`(공용 유지), `github.com/lizongying/nolang/core` → `/home/lzy/code/fork/core`(전용 추가, 작업 영역 탈출), `experimental` → `/experimental`(전용 추가).

### 작업 영역 경계 제약

Nolang는 경로 참조에 대해 엄격한 작업 영역 경계 제어를 적용하여 프로젝트의 이식성과 안전성을 보장합니다.

#### 규칙 요약

| 설정 계층 | `/` 작업 영역 상대 | OS 절대 경로 | `./` `../` 상대 이동 |
| --- | --- | --- | --- |
| `package.noml` 의존성 키 | ✅ 허용 | ❌ 금지 | ❌ 금지 |
| `workspace.noml` 매핑 값 | ✅ 허용 | ✅ 허용(탈출구) | ✅ 허용(탈출구) |
| `.workspace.noml` 매핑 값 | ✅ 허용 | ✅ 허용(탈출구) | ✅ 허용(탈출구) |
| 소스 `#` 가져오기 경로 | ✅ 허용 | ❌ 금지 | ❌ 금지 |

#### 1. package.noml(엄격히 제한되며 작업 영역 탈출 금지)

`package.noml`은 프로젝트의 대외 배포, CI, 다인 협업을 위한 표준 기준 설정이며 코드 저장소에 커밋됩니다.

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

**제약:**
- 로컬 경로는 `/xxx`로만 써야 하며, 기준 = workspace 루트 디렉터리
- 운영 체제 절대 경로는 허용되지 않습니다
- `./`, `../`는 지원되지 않습니다
- 작업 저장소 외부의 어떤 디렉터리도 직접 참조할 수 없습니다

**목적:** 외부 경로를 허용하면 "내 컴퓨터에서는 컴파일되지만 다른 사람에서는 실패"하는 상황이 매우 쉽게 발생하고, 외부의 낯선 코드를 실수로 로드하는 보안 위험도 있습니다.

#### 2. workspace.noml + .workspace.noml(유일한 탈출구)

매핑 대상은 세 가지 로컬 경로를 지원합니다:
- `/xxx` → workspace 루트 내부 디렉터리(package.noml과 의미가 같음)
- `./xxx`, `../xxx` → workspace 루트 디렉터리를 기준으로 해석되며 작업 영역 경계를 넘을 수 있습니다
- 운영 체제 절대 경로 → 작업 영역 외부(fork, 외부 컴포넌트)를 가리킬 수 있습니다

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

**핵심 논리:** `package.noml`은 외부 코드를 직접 참조할 수 없습니다. 사용자가 저장소 외부 소스를 마운트하려면 workspace 설정에 명시적으로 매핑해야 합니다. 이는 "디렉터리 간 로드"에 명확하고 보이는 스위치를 추가하는 것과 같으며, 모든 외부 의존성을 한곳에서 관리합니다.

#### 3. 소스 가져오기 단계의 관련 제약

소스 코드에는 운영 체제 절대 경로 가져오기를 직접 쓸 수 없습니다. 모든 외부 코드 참조는 workspace 별칭이나 원격 패키지 식별자를 통해 간접적으로만 도입할 수 있습니다. `./` 및 `../` 상대 이동도 마찬가지로 금지됩니다.

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

#### 데이터 흐름 경로 시연

시나리오: 비즈니스 프로젝트의 workspace 루트가 `/code/project`이고, 외부 `/code/fork/core`를 사용하려고 합니다

❌ 이렇게 해서는 안 됩니다(package.noml에서 금지):

```noml
; package.noml
dependencies: [
  '/code/fork/core', ; 非法：無法逃逸工作區,
]
```

✅ 표준 방법(workspace 매핑으로 경계 돌파):

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

#### 작업 영역 흐름

컴파일과 실행의 진입 디렉터리는 항상 **작업 영역 디렉터리**(`workspace.noml`이 있는 디렉터리)입니다. 전체 흐름은 다음과 같습니다:

1. 사용자가 작업 영역 디렉터리에서 `no build` 또는 `no run <패키지명>`을 실행합니다
2. 컴파일러가 `workspace.noml`을 읽고 패키지명으로 해당 하위 디렉터리 경로를 찾습니다
3. 해당 하위 디렉터리의 `package.noml`(곧 패키지 루트 디렉터리)을 로드하고, 패키지 루트 디렉터리를 기준으로 빌드/실행합니다
4. 모든 가져오기 경로(`use /path/to/module`)는 **작업 영역 루트 디렉터리를 기준으로 해석**되며([경로 해석 규약](path-resolution.md) 참조), 곧 `workspace.noml`이 있는 디렉터리를 유일한 기준으로 삼고 가져오는 쪽 패키지에는 의존하지 않습니다.

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

> **참고**: 가져오기 경로는 패키지의 `package.noml` 디렉터리가 아닌 **작업 영역 루트 디렉터리**(`workspace.noml`이 있는 디렉터리)를 기준으로 합니다. 패키지 하위 디렉터리에 중첩된 `package.noml`이 있으면 `LoadPackage`가 상위로 검색하여 가장 가까운 `package.noml`을 찾아 패키지 루트 디렉터리로 사용하지만, 가져오기 해석은 항상 작업 영역 루트를 기준으로 합니다.

---

## 적용 사례

- [notools](https://github.com/lizongying/notools) — Nolang 언어로 구현한 Unix 필수 명령줄 도구 모음으로, cat, ls, grep, wc, head, tail 같은 고전 도구를 포함하여 시스템 프로그래밍 분야에서 Nolang의 실제 활용 능력을 보여줍니다.
