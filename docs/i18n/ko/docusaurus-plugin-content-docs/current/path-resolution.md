---
sidebar_position: 99
---

# 경로 해석 규칙: 워크스페이스 루트 기준 (Workspace-Root-Relative)

## 규칙 (Adopted Convention)

모든 **embed 경로**와 **가져오기 경로**(`use` / `#` 지시어, embed 리소스)는
**워크스페이스 루트(workspace root)를 기준으로 하는 정규화된 절대 경로**로 해석되며,
**"현재 소스 파일이 있는 디렉터리"를 상대 기준으로 삼지 않습니다**.

- "워크스페이스 루트" = `workspace.noml`이 있는 디렉터리, 즉 `pkg.WorkspaceRoot()`입니다.
  주의: `package.noml`이 있는 디렉터리는 **패키지 루트**(`pkg.RootDir`)입니다. 둘은 같은 계층이 아닙니다——워크스페이스 루트는 상위 계층(`workspace.noml`이 있으며 여러 패키지를 포함할 수 있음)이고, 패키지 루트는 구체적인 **패키지**의 디렉터리(`package.noml` 포함)이며, 패키지 안에 여러 **모듈**(``.no` 소스 파일)이 있습니다. 혼동하지 마세요.
- 해석 후 정규화(`filepath.Clean`, `.`/`..` 제거, 구분자 통일)하여 **전역에서 유일한** 정규 경로를 얻습니다.
  이 경로는 모듈 로드, 캐시 키, AST/시맨틱 소속의 유일한 식별자로 사용됩니다.
- 이 정규 경로는 `lexer.tokenCache`와 `checker.parseProgramFileCache` 캐시 키의 **경로 구성 요소**입니다.
  캐시 키는 실제로 복합 키 `(정규 경로, 내용 해시)`입니다(`cache.Key`, `src/cache/lru.go` 참조):
  같은 경로의 내용이 바뀌면 → 해시가 바뀌고 → 자동으로 무효화되어 오래된 token/AST를 받지 않습니다. 또한 캐시는 용량 상한이 있는
  LRU라서 장기 실행 프로세스(LSP / fmt)에서 메모리 누수가 없습니다(기존 2패스 아키텍처 검토의 ③번 항목).

## 이유

1. **캐시 이름 충돌을 근원에서 제거합니다.** 기존 token/AST 캐시 키는 "경로 문자열"이었으므로, 서로 다른 소스 파일이 같은
   상대 문자열(예: `utils/no` 또는 embed 이름 `data.json`)으로 가져오기/임베드되면 같은 캐시 항목에 히트하여 →
   정보가 섞이고 잘못된 token/AST를 받았습니다. 워크스페이스 루트 기준의 정규 절대 경로로 바꾸면 **각 논리 파일이 고유 키를 갖고,
   같은 파일은 항상 동일한 키를 가지므로** 충돌이 구조적으로 사라집니다(기존 2패스 아키텍처 검토의 ④번 항목).
2. **컴파일러 로직이 크게 단순해지고 견고해집니다.** 경로 해석이 "parser / transpiler / checker 곳곳에 흩어져 있던
   현재 파일 기준 상대 해석"에서 **단일 정규화 단계**로 수렴되었습니다. 순방향 참조, 모듈 로드, 캐시 키, 진단 메시지의
   경로가 모두 같은 기준을 사용하며 가져오는 쪽 파일의 위치에 의존하지 않습니다.
3. **크로스 파일 / 병렬 빌드 캐시의 길을 엽니다.** 정규 절대 경로는 캐시 키를 안정적이고 전역적으로 비교 가능하게 만들어, 향후
   deep-clone 재사용(`ParseProgram` 중복 회피)과 지연 파싱 최적화를 쉽게 합니다.

## 적용 범위

- **절대 경로**(`/`로 시작하는 로컬 모듈): 이미 워크스페이스 루트 기준으로 해석되므로 그대로 유지합니다.
- **`std/` 모듈**: 내장 `StdFS`에서 오며 키가 `std/<rel>.no`입니다. 그대로 유지하며 로컬 경로와 혼동하지 않습니다.
- **embed 리소스**: 어떤 상대 이름으로 참조하든 내부적으로 모두 워크스페이스 루트 기준의 정규 embed 키로 통일됩니다.

## 구현됨 (Implemented, 2026-08-02)

통일된 helper가 `src/package/paths.go`에 구현되어 있습니다(package `pkg`, 표준 라이브러리만 사용, 순환 의존 없음):

- `FindWorkspaceRoot(start)`: `start`에서 위로 올라가며 `workspace.noml`이 있는 디렉터리를 찾습니다.
- `FindPackageRoot(start)`: `start`에서 위로 올라가며 `package.noml`이 있는 디렉터리를 찾습니다(workspace가 없을 때의 후보).
- `ResolveToWorkspaceRoot(wsRoot, rel)`: 임의의 가져오기/embed 원본 경로를 워크스페이스 루트 기준의
  절대 경로로 정규화합니다(앞선 `/` 제거, 절대 경로는 그대로 반환, `wsRoot`가 비어 있으면 현재 디렉터리로 후퇴).
- `ResolveEmbedBase(sourcePath)`: 상대 embed 경로의 해석 기준——워크스페이스 루트 우선, 후퇴하여 패키지 루트 / 소스 파일 디렉터리.

수렴된 진입점:

- `src/build/transpiler.go`의 `resolveUse`: 분기 A(`/` 접두사)와 분기 E(alias)를 모두
  `pkg.ResolveToWorkspaceRoot(t.workspaceRoot(), path)`로 통일하고 `t.workspaceRoot()` 메서드를
  추가했습니다(`pkg.WorkspaceRoot()` 우선, 그렇지 않으면 `sourcePath`에서 위로 올라가며 `workspace.noml` 검색).
  기존의 `if t.pkg != nil { baseDir = pkg.RootDir; if wsRoot... }` 중복 로직은 삭제했고, cwd 상대
  후퇴는 `FindWorkspaceRoot`를 따르도록 바꿨습니다.
- `src/build/transpiler.go`의 `processEmbeds`: 상대 embed 경로가 `pkg.ResolveEmbedBase(sourcePath)`를 따르도록 변경
  (워크스페이스 루트 우선).
- `src/checker/checker.go`의 `ValidateEmbedAnnotations`: embed 검증 경로도 `pkg.ResolveEmbedBase`를 사용합니다.

캐시 키(`tokenCache` / `parseProgramFileCache`)는 `resolveFile(filePath)`를 통해 워크스페이스 루트 기준의
정규 절대 경로를 경로 구성 요소로 얻습니다. 다시 소스 파일 내용 해시와 결합해 복합 키(`cache.Key`)를 만들므로 충돌이 구조적으로 사라지고,
같은 경로를 편집하면 자동으로 무효화되며, LRU 용량 상한으로 누수를 방지합니다.

`std/`와 `js/` 내장 모듈은 여전히 `StdFS`/`JsFS`를 사용합니다(key는 `std/<rel>.no` / `js/<rel>.no`). 영향을 받지
않습니다. `processEmbeds`는 주 프로그램(사용자 소스 코드)에만 적용되며, std 모듈 자체의 embed는 여전히 자기 위치 기준으로 해석됩니다.

> 상태: **구현됨 (2026-08-02)**. 새 코드는 `pkg.ResolveToWorkspaceRoot` /
> `pkg.ResolveEmbedBase`를 직접 사용하세요. `filepath.Dir(t.sourcePath)` 같은 "현재 파일 디렉터리"로
> 상대 해석을 하지 마세요.

## 용어 대조 (통일, 2026-08-02)

- **workspace 워크스페이스** — 보통 하나의 repo입니다. 루트에 `workspace.noml`을 두며 여러 패키지를 포함할 수 있습니다. 워크스페이스 루트는
  모든 embed/가져오기 경로 해석의 유일한 기준입니다.
- **package 패키지** — 하나의 큰 컴파일 단위: 라이브러리 하나 또는 실행 파일 하나입니다. 루트에 `package.noml`을 둡니다(**패키지 루트**).
  의존성, emit 백엔드 등을 선언합니다. 하나의 패키지는 여러 모듈을 포함합니다.
- **module 모듈** — 보통 하나의 소스 파일(`.no`)입니다. 현재 규칙은 **한 파일 = 한 모듈**입니다. 모듈 하나를 로드한다는 것은
  `.no` 파일 하나를 로드한다는 뜻입니다.

세 계층의 관계: **워크스페이스 ⊃ 패키지(하나 이상) ⊃ 모듈(하나 이상)**. 더 이상 "module"로 "패키지"를 가리키지 마세요
(컴파일 단위)——그것은 "package"의 역할입니다.
