# Nolang

[傳統中文](README.md) · [简体中文](README_zh-CN.md) · [한국어](README_ko.md) · [日本語](README_ja.md) · [English](README_en.md) · [Русский](README_ru.md) · [Français](README_fr.md) · [Español](README_es.md) · [Deutsch](README_de.md) · [Português](README_pt.md)

Nolang은 메모리 안전성, 강력한 성능, 극도로 간결한 문법을 갖춘 시스템 프로그래밍 언어입니다.

## 핵심 특징

- **메모리 안전**: 자동 메모리 관리
- **강력한 성능**: 컴파일 타임 최적화, GC 없음
- **극도로 간결한 문법**: 더 간단한 문법
- **의도 중심**: 개발자의 의도를 최대한 존중하며, 의미가 인간의 직관에 부합
- **절대 충돌하지 않음**: 암묵적인 충돌 경로 제거

## 설치

원 커맨드로 설치합니다（macOS / Linux / Windows Git Bash）. 최신 `no` 를 `$HOME/.local/bin` 에 내려받습니다:

```bash
curl -fsSL https://raw.githubusercontent.com/lizongying/nolang/main/install.sh | sh
```

기본값은 **LLVM 내장** 빌드입니다（`no-llvm-*`, LLVM / opt / llc 를 따로 설치할 필요 없음）. 크기가 작은 순수 Go 빌드를 원한다면 `NO_FLAVOR=plain` 을 지정하고 `no build -cc zig` 와 함께 사용하세요:

```bash
curl -fsSL https://raw.githubusercontent.com/lizongying/nolang/main/install.sh | NO_FLAVOR=plain sh
```

버전이나 설치 디렉터리를 고정하려면（환경 변수를 `sh` 에 전달해야 하므로 먼저 파일로 저장한 뒤 실행）:

```bash
curl -fsSL https://raw.githubusercontent.com/lizongying/nolang/main/install.sh -o install.sh
NO_VERSION=0.4.28 NO_INSTALL_DIR="$HOME/bin" sh install.sh
```

| 환경 변수         | 기본값               | 설명                                                             |
| ----------------- | -------------------- | ---------------------------------------------------------------- |
| `NO_VERSION`      | `latest`             | 버전（`0.4.28` 또는 `v0.4.28`）                                  |
| `NO_FLAVOR`       | `embedded`           | `embedded` 는 LLVM 내장, `plain` 은 순수 Go（시스템 LLVM 필요）  |
| `NO_INSTALL_DIR`  | `$HOME/.local/bin`   | 설치 디렉터리                                                    |
| `NO_REPO`         | `lizongying/nolang`  | 소스 저장소                                                      |

설치 후 `no version` 으로 확인하세요. `$HOME/.local/bin` 이 `PATH` 에 없다면 추가하세요:

```bash
export PATH="$HOME/.local/bin:$PATH"
```

> `.no` 프로그램 컴파일에는 C 링커가 추가로 필요합니다. `no build -cc zig` 를 권장하며（zig 에 clang / lld / libc 포함）, clang 을 직접 설치해도 됩니다.

## Usage

문서

[docs](https://lizongying.github.io/nolang/)

VSCode 플러그인

[vscode](https://marketplace.visualstudio.com/items?itemName=lizongying.vscode-nolang)

nolang CLI

[nolang](https://github.com/lizongying/nolang/releases/latest)

nohub 패키지 목록（커뮤니티 패키지 검색）

[nohub](https://lizongying.github.io/nohub/)

### 커뮤니티 패키지 검색 (nohub)

`no` 는 nohub 의 패키지 인덱스를 직접 읽어 세 가지 읽기 전용 명령을 제공합니다:

```bash
no search <query>    # 이름/키워드/카테고리로 커뮤니티 패키지 검색
no info <pkg>        # 패키지 메타데이터와 설치/가져오기 명령 표시
no docs <pkg>        # 패키지 문서 URL 출력 (docs → homepage → repository)
```

기본값은 공개 인덱스입니다. 여러 nohub 인스턴스를 겹치려면 `~/no/sources.noml` 에 소스 목록을 설정합니다 (배열 순서가 우선순위):

```
{
  sources: [
    'https://lizongying.github.io/nohub',
  ]
}
```

`NOHUB_SOURCES` (쉼표 구분) 는 일시적으로 덮어씁니다. 세 명령 모두 `-source <name>` 로 특정 소스만 볼 수 있고, `no search` 출력에는 `SOURCE` 열이 포함되며 이름과 버전이 같은 패키지는 앞쪽 소스가 우선합니다. 한 소스의 조회 실패는 경고만 출력하고 나머지에는 영향을 주지 않습니다. `no` 는 각 소스의 `index.noml` (`<base>/index.noml` — 검색 페이지가 먼저 읽는 것과 동일하며 패키지마다 최신 버전을 한 건씩 포함) 만 fetch 하고 tarball 이나 `data.js` 는 읽지 않으며, 필드 매핑은 `web/app.js` 와 동일합니다. [nohub 검색 페이지](https://lizongying.github.io/nohub/) 에서도 같은 데이터를 볼 수 있습니다.


### CLI 명령

| 명령                                                         | 설명                                                                          |
| ------------------------------------------------------------ | ----------------------------------------------------------------------------- |
| `no init`                                                    | 작업 공간 정의 (workspace.jsonc 생성, package.jsonc는 포함하지 않음)           |
| `no new <name>`                                              | 작업 공간 내에 새 패키지 생성 (하위 디렉터리 + package.jsonc, workspace.jsonc에 등록) |
| `no fmt [-w] [-d] <file\|dir>`                               | 소스 코드 포맷                                                                 |
| `no build [-o <file>] [-cc <s>] [-target <s>] [<file\|dir>]` | 빌드 (executable 출력)                                                         |
| `no run [-cc <s>] [-target <s>] [<package\|dir\|file>]`        | 빌드 후 실행 (패키지명/디렉터리/파일)                                           |
| `no test [-cc <s>] [-target <s>] [<file\|dir>]`              | 테스트 실행                                                                    |
| `no add <pkg>`                                               | 의존성 추가                                                                    |
| `no remove <pkg>`                                            | 의존성 제거                                                                    |
| `no update <pkg>`                                            | 의존성 업데이트                                                                |
| `no update-all`                                              | 모든 의존성 업데이트                                                           |
| `no list`                                                    | 의존성 목록 표시                                                               |
| `no search <query>`                                            | 이름/키워드/카테고리로 커뮤니티 패키지 검색 |
| `no info <pkg>`                                                | 패키지 메타데이터와 설치/가져오기 명령 표시 |
| `no docs <pkg>`                                                | 패키지 문서 URL 출력 |
| `no sync`                                                    | 의존성 동기화                                                                  |
| `no install [-u] [<tool>[:<version>]@<host>/<owner>/<repo>]` | binary 설치 (~/no/bin/ + 심볼릭 링크)                                          |
| `no uninstall <name>`                                        | binary 및 심볼릭 링크 제거                                                     |
| `no pub --token <token> [--registry <url>]`                  | registry에 게시                                                                |

```bash
# 빌드 (기본적으로 main.no부터 시작)
no build                    # 현재 디렉터리 빌드
no build main.no            # 지정한 파일 빌드
no build -o output          # 출력 경로 지정
no build -cc zig            # Zig 컴파일러 사용
no build -target x86_64-linux-gnu  # 크로스 컴파일 (대상 플랫폼 지정)

# 실행 (빌드 + 실행)
no run                    # 현재 디렉터리의 main.no를 빌드 후 실행
no run foo                # 작업 공간에 foo라는 이름의 패키지 실행
no run ./foo              # ./foo 디렉터리의 main.no 실행
no run -cc zig
no run -target aarch64-macos-gnu

# 테스트 (빌드 + 실행)
no test                   # `test` 디렉터리의 모든 .no 파일 테스트
no test test/my-test.no        # 단일 테스트 파일 실행
no test -cc zig
no test -target x86_64-windows-gnu
```

### 새 프로젝트 만들기

```shell
no new test1

cd test1

no run
```

### Hello World

새 프로젝트를 만든 후 `main.no`를 편집합니다:

```no
print('Hello, World!')
```

실행:

```bash
no run
```

출력:

```
Hello, World!
```

### 진입점 규칙

- **main.no** — 프로그램 진입점
- **lib.no** — 라이브러리 진입점, 함수 내보내기
- **tests/ 디렉터리의 모든 .no 파일** — 테스트 단언 포함

### 테스트 설명

- 테스트 파일은 tests/ 디렉터리에 통일하여 배치
- 각 테스트 파일은 독립적으로 빌드
- 하나라도 테스트가 실패하면 0이 아닌 종료 코드 반환

```bash
no vet src/std                     # Nolang vet로 표준 라이브러리 검사
```

## 소스에서 빌드하기

Nolang은 `make`를 사용하여 빌드해야 합니다:

```bash
make            # 모든 대상 빌드 (bin/no 및 vscode-nolang/server/lsp)
make no         # bin/no 빌드
make lsp        # LSP server 빌드
make package    # LSP 빌드 및 VSCode 확장 패키징
make no-wasm    # no를 WebAssembly로 크로스 컴파일
make lsp-wasm   # LSP를 WebAssembly로 크로스 컴파일
make playground # no.wasm + lsp.wasm + Docusaurus 사이트 빌드
make clean      # 빌드 산출물 정리
make help       # 모든 빌드 대상 확인
```

> ⚠️ **Nolang은 반드시 `make`로 빌드해야 하며, `cd src && go build`로 직접 빌드해서는 안 됩니다.**
>
> `make`는 빌드 전에 자동으로 `make gen`을 실행하여 `src/checker/stdsig_gen.go`를 다시 생성합니다.
> —— 이는 **바이너리에 내장된** 표준 라이브러리 시그니처 테이블입니다 (`funcSigs` / `methodSigs` /
> `funcParams` / `methodParams` / `structFields` / `aliases` / `structMod` /
> `enumVariants` / `embeddedStdModules`). 이 덕분에 매번 `no build`할 때 "모든 std 전체 파싱" PASS1을
> 건너뛰어 시작 속도가 눈에 띄게 빨라집니다.
>
> `src/std`를 수정한 뒤 **먼저 `make gen`을 실행하지 않고** 바로 `go build`하면, 컴파일러가
> 내장 시그니처 테이블과 `src/std` 내용의 불일치를 감지하여 **즉시 오류를 내고 빌드를 거부합니다**
> (`make no` 실행을 안내합니다). 이는 의도된 설계입니다. 과거에는 조용히 전체 파싱으로 되돌아가
> 매 컴파일이 느려지고 쉽게 알아차리기 어려웠습니다. `src/std`를 수정한 후에는 항상 `make no`를
> 실행하세요 (`make gen`을 먼저 실행한 뒤 빌드합니다).
>
> 테스트 실행 시에도 `make test`를 사용하세요: **그냥 `go test`를 실행하면 시그니처 key가 주입되지 않아(`StdSigKey`가 비어 있음) 오류가 나고 거부됩니다**. `make test`는 테스트 바이너리를 빌드할 때 동일한 key를 주입하여 내장 시그니처 테이블 빠른 경로를 타도록 합니다.

---

## 용어집(glossary)

`glossary/`는 **항목별로 정리된 Nolang 지식 베이스**입니다: 각 명사마다 유형, 정의, 문법,
예제, 관련 참조, 한 줄 요약을 담은 레코드가 하나씩 있습니다. 총 **244개 항목 / 10개 의미 영역**(유형,
문법, 주석, 메모리, 오류, 모듈, 컴파일러 내부, 도구 체인, std 전체 모듈, 알려진 함정)이며,
데이터 자체는 `.noml` 형식입니다.

프런트엔드는 Nolang으로 작성되었으며 CLI와 MCP(stdio / HTTP)를 모두 제공하여 사람과 AI 모두 조회할 수 있습니다:

```bash
no build glossary                        # 산출물: glossary/dist/glossary

./glossary/dist/glossary get option      # 단일 항목 전문 가져오기
./glossary/dist/glossary search 三態      # 키워드 검색
./glossary/dist/glossary list --domain=traps   # 알려진 함정 목록
./glossary/dist/glossary check           # 일관성 검사 (CI용)

./glossary/dist/glossary mcp             # MCP server (stdio)
./glossary/dist/glossary mcp --http=127.0.0.1:8765
```

출력은 `--format=text|md|json`으로 전환할 수 있습니다. 연동 및 데이터 형식 설명은 [`glossary/README.md`](glossary/README.md)를 참조하세요.

nohub에서 glossary 패키지를 볼 수도 있습니다: [glossary on nohub](https://lizongying.github.io/nohub/?package=glossary:latest@github.com/lizongying/nolang)

---

## 관련 프로젝트

- [nohub — Nolang 생태계의 커뮤니티 패키지 목록 검색](https://lizongying.github.io/nohub/)

  패키지 이름・키워드・카테고리로 수록된 커뮤니티 패키지를 검색하고, 패키지 정보・버전 스냅샷・소스 진입점을 확인할 수 있습니다. 색인은 `web/build.mjs` 가 `packages/` 스냅샷에서 생성합니다.

- [noui — GPUI의 정신을 Nolang로 다시 구현한 UI 툴킷(nohub 패키지)](https://lizongying.github.io/nohub/?package=noui:latest@github.com/lizongying/nolang)

- [Benchmarks Game — Nolang vs C vs Rust 성능 비교 보고서](https://github.com/lizongying/no-benchmarks/results/report.md)

- [notools — Nolang로 구현한 Unix 명령어 도구 모음](https://github.com/lizongying/notools)

  Nolang 적용 사례: Nolang 언어로 구현한 Unix 표준 명령어 도구 모음으로, cat, ls, grep, wc, head, tail 등 고전적인 도구를 포함하여 시스템 프로그래밍 분야에서 Nolang의 실질적인 활용 능력을 보여줍니다.

---

## TODO

## 라이선스

Apache-2.0
