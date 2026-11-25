---
sidebar_position: 6
---

# 메모리 관리

Nolang은 **GC가 없는** 언어이며, 메모리 안전은 컴파일러가 `free`를 자동으로 삽입하여 보장합니다. 이 문서에서는 구현된 메모리 설계와 소유권 시맨틱스를 설명합니다.

## 핵심 원칙

### 단일 소유권
모든 힙 `data` 버퍼에는 **소유자가 오직 하나**입니다. 소유권은 move로 전달될 수 있으며, 전달 후 원래 소유자는 free 책임을 포기합니다. 지역 변수 간의 `=`는 깊은 clone을 통해 두 변수가 각각 독립적으로 data를 소유하도록 합니다.

### 할당 시맨틱스
`b = a`는 문맥에 따라 다음 시맨틱스 중 하나를 선택합니다:

| 시맨틱스 | 발생 조건 | 동작 |
|------|---------|------|
| **값 복사** | 기본 타입(i64/f64/bool 등)이고 can_slot_rebind를 만족하지 않음 | 값을 직접 복사하며, 힙 데이터 없음 |
| **스택 슬롯 재바인딩** | 지역 변수 간 `b = a`이며, a가 스택 타입(i64/u64/i128/u128/txt)이고 can_slot_rebind를 만족 | `g.varAlias[b] = a`, b와 a가 동일한 스택 슬롯을 공유하며 0회 복사(값 복사보다 우수); 그렇지 않으면 값 복사로 강등 |
| **깊은 clone** | 지역 변수 간 `b = a`이며, a가 힙 소유 타입(vec/arr/str/clone 가능 구조체/**`?str`, `?[]T`**)이고 할당 후에도 **여전히 참조됨** | 새 data malloc + memcpy + 요소 재귀 clone; a와 b가 각각 독립적으로 data를 소유하며 함수 종료 시 각각 free |
| **move(소스가 더 이상 살아있지 않음)** | 지역 변수 간 `b = a`이며, a가 힙 소유 타입이지만 할당 후 **더 이상 참조되지 않음** | 0회 복사로 소유권 전달; 소스는 free를 건너뜀. 깊은 clone과 **관측 가능하게 동등**하며, 복사 한 번만 절약 |
| **move** | 출력 매개변수 `out = x` | 구조체 얕은 복사 + 소스를 moved로 표시; 소스는 free를 건너뜀 |
| **깊은 clone** | `vec.push(x)`(x가 힙 소유 타입) | 새 data malloc + memcpy + 요소 재귀 clone; 소스는 여전히 독립 data를 소유하며 함수 종료 시 각각 free |

> **값 대 값 할당은 항상 깊은 복사이며, 0회 복사 별칭은 금지됩니다.** `b = a`(RHS가 이름이 붙은 변수 전체인 경우)에는 두 가지 로어링만 존재합니다——
> **소스가 여전히 살아 있음 ⇒ 깊은 clone**, **소스가 죽음 ⇒ move**; **허용되지 않습니다** "두 이름이 하나의 buffer를 공유하고 free는 하나만 남기는 것"은
> (그런 로어링은 읽기 전용 순방향 최적화에 존재했으나 제거되었습니다). **요소/필드 읽기**(`b = a[i]`, `b = a.f`)는
> **뷰 별칭** 시맨틱스를 유지하며, 이 제한을 받지 않습니다.

## 스택 타입 move(스택 슬롯 재바인딩 / slot-rebind)

`i64/u64/i128/u128/txt`는 모두 **스택 타입**(값이 alloca 스택 슬롯에 직접 저장되며 힙 data가 없음)이지만, `b = a`는 시맨틱스상 여전히 기본적으로 "값 복사"(a의 스택 값을 b의 스택 슬롯으로 memcpy)를 따릅니다. 불필요한 복사를 줄이기 위해 컴파일러는 **can_slot_rebind** 제약을 만족하는 스택 타입 `b = a`에 대해 **스택 슬롯 재바인딩**을 수행합니다: `g.varAlias[b] = a`로 두어 b와 a가 동일한 스택 슬롯을 공유하게 합니다(0회 복사, 값 복사보다 우수).

### can_slot_rebind 제약(보수적 정확성)
재바인딩 후 b와 a가 동일한 스택 슬롯을 가리키므로, a가 이후 b에 대해 수행하는 모든 읽기는 "a의 저장값"을 읽게 됩니다. 시맨틱스 오류를 피하기 위해 재바인딩은 소스 a가 **이후에 "완전히 참조되지 않을 때(읽기 또는 쓰기 모두 없음)"**에만 허용됩니다. 그렇지 않으면 **전체 값 복사로 강등**됩니다(시맨틱스는 동일하며 memcpy가 한 번 더 발생할 뿐).

- 안전성은 `computeSlotRebindSafety`(주 함수) / `computeMoveEligibility`(사용자 함수)가 `stmtContainsVarRefAny` / `exprContainsVarRefAny`를 통해 정적으로 판정합니다: move 이후의 모든 문장(분기/반복문 back-edge 포함)을 스캔하여, 소스 변수가 여전히 어떤 방식으로든 참조되면 `slotRebindSafe[stmt] = false` → 복사를 수행합니다.
- 참조 스캔은 **변수를 참조할 수 있는 모든 AST 노드**를 빠짐없이 다루어야 합니다. 그렇지 않으면 다루지 않은 쓰기 참조가 안전하지 않은 재바인딩을 초래할 수 있습니다(별칭 슬롯이 오염됨 → 잘못된 출력, 심지어 무한 반복). 적용 범위: 문장 수준 `LetStatement` / `ExpressionStatement` / `ForStatement`(`Init`/`Update`/`Condition`/`CountExpr`/`Body` 및 **`IterRange` 반복 집합** 포함) / `ReturnStatement` / `MultiAssignStatement` / **`UnwrapAssignStatement`(`?=` 언랩 할당)** / **`BlockStatement`(독립 블록)**; 표현식 수준 `Identifier` / `AssignExpression` / `Infix` / `Prefix` / `Call` / `Dot` / `Index` / `IfExpression`(분기 본문 포함) / `Slice` / `Conditional` / `Grouped` / **`CoExpression`(`co <call>` 코루틴 spawn, 방어적으로; 전체 함수 비활성화는 여전히 `curHasUnsafeConstruct`가 담당)** / **`CastExpression`** / **`RangeExpression`**. 새로 추가되는 모든 변수 참조 구성은 이 두 스캔 함수에 동시에 반영해야 합니다.
- `match`는 `IfExpression`으로 desugar되며, 그 분기 본문 내의 참조는 위 분석의 순회에 포함되므로 **match는 비활성화를 유발하지 않습니다**(`tests/match.no` / `tests/option.no`를 통해 기준선과 일치함을 검증).

### 비활성화 시나리오(항상 값 복사로 강등)
| 비활성화 조건 | 이유 |
|---------|------|
| 대상이 출력 매개변수 / 전역 변수 / 힙 타입 변수 | 출력 매개변수는 포인터로 전달되고, 전역은 함수 간에 보이며, 힙 타입은 깊은 free가 필요하므로 재바인딩이 소유권을 파괴함 |
| 소스가 매개변수 / 전역 변수 | 매개변수는 참조로 전달되므로 재바인딩이 호출자의 스택 프레임을 파괴함 |
| **stdlib 함수**(`curIsStdLib`, `SetStdModules`가 판정) | stdlib 내부의 별칭 바인딩(예: fmt의 `it`이 지역 `n`에 별칭됨)은 참조 분석으로 완전히 모델링하기 어려움 |
| **사용자 함수 본문에 클로저(`FunctionLiteral`) 또는 코루틴 spawn(`RunExpression`)이 포함됨**(`curHasUnsafeConstruct`, `bodyHasUnsafeConstruct`가 재귀 스캔) | 클로저가 캡처한 변수는 "독립된 함수 문맥"에서 평가되고 코루틴은 스레드를 넘나들며 실행되므로, 두 경우 모두 변수의 수명 주기가 현재 함수의 `g.varAlias` 단일 함수 별칭 스코프를 벗어나 재바인딩이 공유 스택 슬롯을 파괴함 |

> 설계 선택: 클로저/코루틴은 "변수별 정밀 계산" 대신 "함수 전체 비활성화"를 채택합니다. 단일 함수 별칭 분석은 본질적으로 함수 간/스레드 간 저장 공유를 모델링할 수 없기 때문입니다. 함수 전체 비활성화는 최적화만 손실할 뿐(복사로 강등) 절대 오류를 유발하지 않으므로 "보수적 정확성" 원칙에 부합합니다.

### 별칭 무효화(alias invalidation)
- 대상이 다시 할당될 때 먼저 `delete(g.varAlias, name)`로 남아 있는 기존 별칭을 지웁니다. 그렇지 않으면 일반 할당 경로 `varAddr(name)`가 여전히 이전 소스의 스택 슬롯을 가리켜 옛 소스를 오염시킵니다.
- 재바인딩은 전이적 해석을 수행합니다: `g.varAlias` 체인을 따라 최종 소스 스택 슬롯(`a → c → …`)을 찾아 다중 홉 별칭의 어긋남을 방지하고, `g.varTypes[name]`도 동기화하여 이후 타입 조회가 일치하도록 합니다.

### 힙 타입 move와의 직교성
스택 슬롯 재바인딩은 스택 타입 `b = a`의 **값 복사** 시맨틱스에만 작용하며, 힙 소유 타입의 "깊은 clone / move(출력 매개변수)"와는 완전히 직교합니다. 힙 타입 경로는 `slotRebindSafe`의 영향을 받지 않습니다.

### 테스트 참고
- `tests/slot-rebind.no`: i64/u64 재바인딩, i128/u128을 `==`로 비교, txt를 `.len-bytes()`로 확인, 복사로 강등(`da` 재사용 → `db`/`dc` 복사), 재할당 후(`ra`=10), 매개변수 move(`pm_fn`의 소스가 매개변수 → 복사), 재바인딩 후 재할당(`rr_fn` → `rr`=99), 소비(`consume(m)` → `cm`=84). 기대 출력은 `42 7 1 1 17 5 5 10 100 99 84`입니다.
- `tests/slot-rebind-unsafe.no`: 코루틴(`awy { co ... }`)이 스택 변수를 캡처하여, `curHasUnsafeConstruct`가 재바인딩을 비활성화한 뒤 출력이 기준선과 일치함을 검증합니다.

### 컴파일러가 삽입하는 free
- 함수 종료 시: moved되지 않은 모든 지역 힙 변수를 해제
- 재할당 전: 이전 값을 해제
- 구조체 필드: 힙 데이터를 포함한 필드를 재귀적으로 해제

## 타입 레이아웃

| Nolang 타입 | 메모리 구조 | 필드 | 할당 전략 |
|------------|-----------|------|---------|
| `[]T`(슬라이스) | **16바이트** | **u32 len, u32 cap, i64 data(ptrtoint)** | malloc(힙) |
| `[N]T`(고정 배열) | N × sizeof(T) | 요소 인라인, **len/cap 디스크립터 없음** | alloca(스택) 또는 malloc |
| `str`(긴 문자열) | **16바이트** | **u32 len, u32 cap, i8\* data** | malloc(힙) |
| `txt`(고정 문자열) | 256바이트 | `[255 x i8]` data, i8 len | alloca(스택) |
| `?T`(nullable) | **페이로드에 따라 결정**: `{ i8 tag, [K x i64] }` | 1바이트 태그 + 7바이트 패딩 + 페이로드(8의 배수) | alloca(스택); 페이로드가 임계값을 초과하면 힙 포인터로 저장 |
| 태그 열거형 | `{ iN tag, [K x i64] }` | tag 너비는 변형 수에 따라 달라짐(`i8`부터 시작), 페이로드는 가장 넓은 변형의 union | alloca(스택) |
| 구조체 | 각 필드의 합 | 각 필드 | alloca(스택) |

**`?T`의 크기는 고정된 것이 아니라 계산된 값입니다**: `?i64` = 16바이트, `?str` = 24, `?txt` = 264이며, `option-payload-inline-threshold`(기본값 264 = 1 + 7 + 256, 최소 16)를 초과하는 페이로드는 힙에 박싱되고 전체 option은 다시 16바이트가 됩니다. 각 페이로드 타입은 고유한 `%option_<key>` 타입을 가집니다(`src/mir/layout.go`). 따라서 "타입이 같음 ⇔ 레이아웃이 같음"입니다. `err`의 페이로드는 항상 박싱되며 페이로드 예산을 차지하지 않습니다. 자세한 내용은 `docs/docs/lang/syntax.md`의 〈옵션 타입(option)・메모리 레이아웃〉을 참고하세요.

**태그 열거형**의 tag 너비는 변형 개수에 따라 달라집니다(`<=256` → `i8`, `<=65536` → `i16`, `<2^32` → `i32`, 그 외에는 `i64`). 페이로드 union의 너비는 "가장 넓은 단 하나의 변형"이며 모두 더한 값이 아닙니다. 필드는 자연 정렬에 따라 배치되고 **바이트 오프셋**으로 주소가 지정됩니다. 모든 변형에 페이로드가 없으면 레이아웃이 `{ iN }`(1바이트부터 시작)으로 퇴화합니다.

**`str`의 u32 헤더**: `%str-long = type { i32, i32, i8* }`(64비트 플랫폼에서 16바이트). `len` / `cap`은 내부적으로 `u32`(LLVM `i32`)이지만, **모든 외부 API는 여전히 `i64`입니다**: 읽을 때 `zext i32 -> i64`, 쓸 때 `trunc i64 -> i32`. `s.len-bytes()`, `s.len()`, `s.cap()`, `s._len()` / `s._cap()` / `s._set-len()` / `s._set-cap()`, `len(s)`의 시그니처는 모두 변경되지 않았습니다. 이 경계는 codegen의 `strLenI64` / `strCapI64` / `strHeaderLoadI64` / `strHeaderStoreI64` / `strLongFromParts`가 일괄 처리합니다: descriptor의 0 / 1번 필드에 `load i64` / `store i64`를 직접 내보내서는 **안 됩니다**. opaque pointer 하에서는 LLVM verifier가 이를 잡아내지 못하지만, 한 번에 8바이트를 읽거나 쓰면서 인접한 헤더 필드까지 함께 바꿔버립니다(실제로 `s.len-bytes()`가 `len | cap<<32`를 반환했던 적이 있습니다).

관련된 크기 상수도 16바이트를 따라야 합니다: `[]str`의 element stride, `os.args()`의 argv backing store, 그리고 전역 문자열 상수의 `{ i32 len, i32 cap, i8* data }` 리터럴. `?str`의 페이로드는 정확히 16바이트(2개의 i64 word, 첫 번째 word에 len과 cap이 패킹됨)이므로 `?str`은 24바이트 인라인 option입니다. 태그 열거형에서 `str` 변형의 페이로드 역시 2개의 word입니다. `txt`는 기존대로 유지되며 `str`을 따라 u32로 바뀌지 않습니다.

**`[]T`의 u32 헤더**: `%vec = type { i32, i32, i64 }`(16바이트). `len` / `cap`은 `str`과 동형이며 내부는 `u32`, 외부 API는 여전히 `i64`입니다. 차이점은 **data 필드가 여전히 `i64`**라는 점입니다(`i8*`가 아닌 `ptrtoint`로 인코딩된 포인터). 이는 `inttoptr` / `ptrtoint`의 기존 호출 지점을 변경하지 않기 위함입니다. 이 경계는 `vecLenI64` / `vecCapI64` / `vecHeaderLoadI64` / `vecHeaderStoreI64` / `vecFromParts`(그리고 범용 `hdrFieldI64` / `hdrTrunc` / `hdrWiden`)가 일괄 처리하며, `emitLenCap` / `emitGetField` / `emitSetField` / `ensureVecBuffer`가 모두 이를 거칩니다. `str`과 동일한 금지 사항도 적용됩니다: `%vec`의 0 / 1번 필드에 `load i64` / `store i64`를 직접 내보내서는 **안 됩니다**.

`cap == 0`은 `%vec`에 대해 **빌린 뷰** 표시자입니다(`s.bytes()`, `[N]T` → `[]T` 빌리기, str → `[]byte` 강제 변환): 힙을 소유하지 않으며 `@vec_free` / `@vec_retain`이 건너뜁니다. `cap > 0`일 때만 `data`가 가리키는 블록을 소유합니다.

`[N]T`에는 어떤 런타임 디스크립터도 없습니다. LLVM 수준에서는 그냥 집계체 `[N x T]`이며, `.len`은 컴파일러가 컴파일 타임 상수 `N`으로 직접 바꾸고 `cap`은 없습니다.

## 얕은 free와 깊은 free

### 얕은 free
컨테이너의 data 버퍼만 해제하고 요소를 순회하지 않습니다. 다음에 적용됩니다:
- `%str-long`(문자열 data는 문자 버퍼이며 중첩된 힙 소유 요소가 없음)
- 요소가 기본 타입(i64, double 등)인 vec/arr

### 깊은 free
먼저 각 요소를 순회하며 재귀적으로 힙 데이터를 해제한 다음, 컨테이너의 data 버퍼를 free합니다. 요소가 힙 소유 타입인 vec/arr에 적용됩니다:
- `[]str`(요소가 %str-long)
- `[][]i64`(요소가 %vec)
- `[]MyType`(요소가 사용자 구조체이며, 필드를 재귀적으로 해제)

### NULL 검사
모든 free 전에 `icmp eq i8* %ptr, null`을 검사하여 free(NULL)이나 초기화되지 않은 포인터의 free를 방지합니다.

## 소유권 전달(move)

### 단일 반환값 move
```no
get-slice = () (out []i64) {
    local = [1, 2, 3]
    out = local; local 標記為 moved，函數結束不 free；out 由呼叫者管理
}

v = get-slice(); v 擁有 data，函數結束時 free
```

### 다중 반환값 move(매개변수 위치 순서대로)
```no
get-pair = () (a []i64, b []i64) {
    x = [1, 2]
    y = [3, 4]
    a = x; 第一個輸出參數，x 標記為 moved
    b = y; 第二個輸出參數，y 標記為 moved
}

a, b = get-pair(); a 擁有 x 的 data，b 擁有 y 的 data
```

**처리 순서**: 출력 매개변수를 함수 시그니처의 **선언 순서**대로 하나씩 처리합니다. 각 `out = src` 할당은 소스 변수를 독립적으로 moved로 표시합니다.

**동일한 소스 변수에 대한 여러 번의 할당**: `a`와 `b`가 같은 소스 변수를 참조하는 경우(예: `a = x; b = x`), 컴파일러는 **Liveness 사전 분석**(`moveEligible`)을 통해 clone 또는 move를 자동으로 결정합니다:

- `a = x`: x가 이후 `b = x`에서 참조됨 → **깊은 clone**, a가 독립적으로 data를 소유
- `b = x`: x가 이후 더 이상 참조되지 않음 → **move**, b가 x의 data를 넘겨받고, x는 moved로 표시되어 free를 건너뜀

이렇게 하면 double-free를 방지할 수 있습니다. 규칙: **마지막 참조는 move를, 그 이전의 참조는 clone을 사용합니다**.

조건 분기 시나리오(예: `if cond { out = x }`)에서는 런타임 비트맵으로 moved 상태를 추적합니다. 다음 절을 참고하세요.

### vec.push의 깊은 clone
```no
inner = [1, 2, 3]
outer.push(inner)
; inner 的 data 被深層 clone 到 outer 新元素位置
; inner 仍擁有獨立 data，函數結束時 inner 與 outer 各自 free
```

push는 힙 소유 요소 타입(`%str-long`/`%vec`/`%arr`/사용자 구조체)에 대해 깊은 clone을 수행합니다: 새 data malloc + memcpy + 요소 재귀 clone. 소스 변수와 외부 vec는 각각 독립적인 data를 소유하므로 **move 표시가 필요하지 않아** double-free를 방지합니다. 기본 타입 요소(i64/f64 등)는 값을 직접 store합니다.

### 런타임 move 추적(힙 변수 인덱스로 색인되는 비트맵)

조건 분기에서의 move는 한 가지 과제를 제기합니다: 컴파일 타임에 어떤 move가 실제로 발생했는지 알 수 없습니다.

```no
cond-move = (flag i64) (out []i64) {
    x = [1, 2, 3]
    if flag == 1 {
        out = x; move 僅在 flag==1 時發生
    }
    ; flag==0 時 x 仍擁有 data，函數結束需 free
    ; flag==1 時 x 所有權已轉移，函數結束需跳過 free
}
```

Nolang은 **힙 변수 인덱스로 색인되는 비트맵**을 사용하여 이 문제를 해결합니다. 컴파일 타임에 각 지역 힙 변수에 고유한 `varIdx`를 할당하며, 런타임 비트맵의 각 bit는 하나의 힙 변수에 대응합니다(출력 매개변수가 아님).

#### 컴파일러 상태

| 필드 | 타입 | 용도 |
|------|------|------|
| `heapVarIndex` | `map[string]int` | 힙 변수 이름 → `varIdx`(지역 힙 변수만 해당) |
| `outBindState` | `[]int` | 각 출력 매개변수가 현재 바인딩한 힙 변수 인덱스(-1=바인딩 없음, -2=불확실) |
| `movedVarBitset` | `[]uint64` | 컴파일 타임 moved 비트맵(런타임 비트맵이 없을 때 사용) |
| `movedBitmapBase` | `string` | 런타임 비트맵 변수 이름 접두사(예: `%__mb`, 빈 값=할당되지 않음) |
| `bitmapCount` | `int` | u64 비트맵 블록 수(= maxVarIdx/64 + 1) |

#### 인덱스 매핑 규칙

하나의 `u64` 블록이 64개의 플래그 비트를 저장하며, 힙 변수 인덱스 `varIdx`는 다음과 같습니다:

- 블록 번호 = `varIdx / 64`
- 블록 내 오프셋 = `varIdx % 64`
- 마스크 = `1u64 << 偏移`

컴파일 타임에 상수를 직접 계산하므로 런타임 계산 오버헤드가 없습니다. 여러 개의 `u64` 블록으로 임의의 수의 힙 변수를 지원하며, **매개변수/반환값 개수 제한이 없습니다**.

#### move 할당 처리(덮어쓰면 이전 bit를 지움)

힙 변수를 출력 매개변수로 move할 때마다:

1. 해당 출력 매개변수가 이전에 다른 변수에 바인딩된 적이 있다면(`outBindState[outIdx] >= 0`), 먼저 옛 변수에 해당하는 bit를 지웁니다
2. 그런 다음 현재 변수에 해당하는 bit를 1로 설정합니다
3. 해당 출력 매개변수가 바인딩한 변수 인덱스를 갱신합니다(`outBindState[outIdx] = srcVarIdx`)

#### 함수 끝에서 해제

모든 힙 변수를 순회하며 독립적인 `if`를 펼칩니다: 해당 bit가 0이면 free하고, bit가 1이면 소유권이 이동했다는 뜻이므로 해제를 건너뜁니다.

#### 비트맵 온디맨드 할당

비트맵 변수는 **필요할 때만** 할당하여 분기가 없는 시나리오에서의 성능 오버헤드를 방지합니다:

| 시나리오 | 비트맵 할당 | free 동작 |
|------|---------|----------|
| move 없음 | 할당 안 함 | 모두 free |
| move가 분기 밖에 있음(결정적 move) | 할당 안 함 | 컴파일 타임 `movedVarBitset`이 free를 직접 건너뜀 |
| move가 분기 안에 있음(조건부 move) | 할당 | 런타임 비트맵 검사: bit=1이면 건너뛰고, bit=0이면 free |

컴파일러는 함수 본문을 생성하기 전에 AST를 미리 스캔하여(`detectBranchMoveToOut`) `IfExpression`/`ForStatement`/`ConditionalExpression` 분기 내에서 출력 매개변수로의 move 할당이 있는지 감지하고, 이러한 패턴이 존재할 때만 런타임 비트맵 변수를 할당합니다. 비트맵 `alloca`는 함수 본문 생성 이후에 삽입됩니다(이때 `nextHeapVarIdx`가 최종 값임). entry block에 기록됩니다.

이 메커니즘은 모든 힙 타입(`vec`/`str-long`/`arr`/사용자 구조체)에 적용됩니다.

## 깊은 clone(지역 변수 간 할당)

```no
a []i64 = [10, 20, 30]
b = a; 深層 clone：malloc 新 data + memcpy + 遞迴 clone 元素
b[0] = 99
; a[0] == 10（a 不受影響）
; b[0] == 99（b 獨立修改）
```

### 깊은 clone 절차
1. 대상 변수의 이전 값을 해제합니다(이미 힙 데이터가 있다면)
2. 새 data 버퍼를 `malloc`하고, 소스 data를 새 data로 `memcpy`합니다
3. 각 힙 소유 요소를 재귀적으로 clone합니다:
   - `%str-long` 요소: malloc + memcpy 문자열 data
   - 사용자 구조체 요소: 구조체 memcpy + 힙 데이터를 포함한 필드 재귀 clone
4. 새 data 포인터, len, cap을 대상 변수에 기록합니다
5. 대상을 힙 변수로 추적합니다(함수 종료 시 free)

### clone 가능한 타입
| 타입 | 깊은 clone 가능 | 설명 |
|------|-------------|------|
| `%vec` / `%arr`(요소가 기본 타입) | ✅ | data를 memcpy하면 됨 |
| `%vec` / `%arr`(요소가 %str-long) | ✅ | 요소별로 malloc+memcpy 문자열 data |
| `%vec` / `%arr`(요소가 clone 가능 구조체) | ✅ | 요소별로 구조체 필드를 재귀 clone |
| `%vec` / `%arr`(요소가 %vec / %arr) | ✅ | `elemElemType` 메커니즘을 통해 내부 컨테이너 요소를 재귀 clone |
| `%str-long` | ✅ | malloc + memcpy 문자열 data |
| 사용자 구조체(중첩 컨테이너 필드 없음) | ✅ | 구조체 memcpy + 힙 필드 재귀 clone |
| 사용자 구조체(중첩 컨테이너 필드 포함) | ✅ | 구조체 memcpy + 중첩 컨테이너를 포함한 필드 재귀 clone(`elemElemType` 사용) |
| `?str` | ✅ | 인라인 페이로드를 가진 option: 옵션 자체와 그 문자열 data를 clone |
| `?[]T` | ✅ | 인라인 페이로드를 가진 option: 옵션 자체와 내부 슬라이스의 data를 clone(요소별 clone, `?[]str`처럼 요소가 힙을 소유하는 경우 재귀) |

> **`?str` / `?[]T`는 인라인 페이로드를 갖지만 힙을 소유합니다.** 이들의 페이로드는 option 자체의 `%option_<key>` 안에 직접 저장됩니다(`?str`의 구조는 `{ i8 tag, [2 x i64] }`이며, 16바이트 페이로드에 `{ u32 len, u32 cap, i8* data }`가 들어감). 하지만 그 안의 `str` data / slice backing store는 힙에 할당됩니다 ⇒ **clone할 때는 페이로드까지 깊은 복사해야 하고, free할 때도 페이로드까지 해제해야 하며**, 양쪽은 반드시 짝을 이루어야 합니다.
> `?[]T`의 해제는 `[]T`와 동일한 깊은 free helper를 사용합니다(요소 타입에 따라 재귀 여부를 결정)므로, "깊은 clone에는 깊은 free를 짝짓는다"는 대응 관계가 여기서도 성립합니다.
> ⚠️ 일반 (`?str`/`?[]T`가 아닌) option의 페이로드는 빌린 값이거나 순수 값으로, 힙을 **소유하지 않으므로** clone/free 모두 이를 건드리지 않습니다.
> 페이로드가 `option-payload-inline-threshold`를 초과할 만큼 크면 option이 힙 포인터를 저장하도록 바뀌며, 이때 free는 박스까지 함께 해제해야 합니다(`Boxed`가 결정하며 `src/mir/layout.go` 참조).

### move와의 차이점
- **깊은 clone**: 소스와 대상이 각각 독립적으로 data를 소유하며 함수 종료 시 각각 free
- **move**: 소스가 소유권을 포기하고(moved로 표시), 대상이 data를 넘겨받으며, 소스는 free를 건너뜀

`b = a`의 판단 규칙:
1. a가 출력 매개변수의 소스라면 → move
2. 그렇지 않고 a가 힙 소유 타입이지만 **할당 후 더 이상 참조되지 않는다면** → move(0회 복사, 깊은 clone과 관측 가능하게 동등)
3. 그렇지 않고 a가 힙 소유 타입이며 깊은 clone이 가능하다면 → 깊은 clone
4. 그 외에는 값 복사

`vec.push(x)`는 이 판단 규칙에 포함되지 않습니다. push는 메서드 호출로, x가 힙 소유 타입인지 여부와 관계없이 힙 소유 요소에 대해 깊은 clone을 수행합니다(앞 절 참조).

## 스택 타입 move(스택 슬롯 재바인딩 / slot-rebind)

`i64`/`u64`/`i128`/`u128`/`txt`는 스택 타입(힙 소유가 아님)이지만, 소스 `a`가 이후 참조되지 않을 때 `b = a`(RHS가 변수)는 값 복사를 수행하는 대신 **스택 슬롯 재바인딩**(stack-slot rebind)을 수행합니다: `g.varAlias[b] = a`로 하여 `b`와 `a`가 동일한 스택 슬롯을 공유하게 합니다(0회 복사). 이는 값 복사(여전히 load+store를 emit해야 함)보다 우수하며, 256바이트 `%txt` 같은 큰 스택 타입에서 특히 효과가 큽니다.

### can_slot_rebind 제약(보수적 정확성)

스택 슬롯 재바인딩은 힙 move의 `moveEligible`보다 **더 엄격합니다**:

- `moveEligible`: 소스 `a`가 이후 **읽히지 않으면** move를 허용합니다(힙 타입에 적용되며, move 후 소스는 free를 건너뛰어 대상에 영향을 주지 않음).
- `can_slot_rebind`(`slotRebindSafe`): 소스 `a`가 이후 **어떤 참조도 없을 때(읽기 또는 쓰기 모두 없음)**에만 재바인딩을 허용합니다. 재바인딩 후 `b`와 `a`가 동일한 스택 슬롯을 공유하므로, `a`가 이후에 쓰이면 `b`의 값이 파괴되기 때문입니다.

계산: `generateFunctionDefinition`은 `computeMoveEligibility`를 호출하여(`moveEligible`과 `slotRebindSafe`를 동시에 채움), `generateMainFunction`은 `computeSlotRebindSafety`를 호출합니다(**`slotRebindSafe`만 채우고 `moveEligible`은 건드리지 않음** — HEAD의 주 함수는 힙 move를 사용하지 않으며 SEGFAULT를 피하기 위해 계속 꺼 두었음). 두 함수 모두 `stmtContainsVarRefAny`를 통해 분기/반복문을 인식하는 참조 스캔을 수행합니다(`AssignExpression`의 좌변, 반복문 back-edge 포함).

`can_slot_rebind`가 만족되지 않으면(소스가 이후에도 참조됨) **전체 값 복사로 강등**되며, 절대 안전하지 않은 동작으로 퇴화하지 않습니다.

### 비활성화 시나리오(일반 할당 경로를 반드시 거쳐야 함)

| 시나리오 | 이유 |
|------|------|
| stdlib 함수(`curIsStdLib`) | stdlib에는 match/closure/coroutine 등 참조 분석이 완전히 모델링할 수 없는 구조가 포함되어 있어 재바인딩이 공유 스택 슬롯을 파괴할 수 있습니다(예: fmt 내부에서 `it`이 지역 `n`에 별칭 바인딩됨). 감지: `g.stdModules`(`transpiler.go`의 `SetStdModules`가 `checker.KnownStdModules()`에서 주입), 폴백은 `g.funcOwner[fd.Name]` |
| 대상이 출력 매개변수 | 출력 매개변수는 호출자가 포인터로 전달하므로, 재바인딩하면 지역 소스 스택 슬롯을 가리키게 되어 호출자가 읽을 수 없음 |
| 대상이 전역 변수 | 재바인딩하면 전역 이름이 지역 소스 스택 슬롯으로 해석되어 전역 시맨틱스가 파괴됨 |
| 대상이 힙 타입 변수 | 스택 슬롯 재바인딩은 스택 타입에만 적용됨 |
| 소스가 매개변수 | 매개변수는 참조로 전달되므로 재바인딩이 호출자의 스택 프레임을 파괴함 |
| 소스가 전역 변수 | 전역 주소가 지역 소스에 별칭되어 시맨틱스 오류 발생 |

### 재할당 후 별칭 무효화

변수가 다시 할당될 때(`b = ...`) 먼저 `delete(g.varAlias, name)`로 남아 있을 수 있는 옛 별칭을 지워, 일반 할당 경로가 `varAddr(name)`를 통해 여전히 이전 소스 스택 슬롯을 가리켜 옛 소스를 오염시키는 것을 방지합니다. 새 별칭을 설정할 때는 별칭 체인을 따라 **전이적 해석**(`src := ident.Value; for { if n2,ok := g.varAlias[src]; ok { src = n2 } else break }`)을 수행하여 최종 소스 스택 슬롯을 찾음으로써 다중 홉 별칭의 어긋남을 방지합니다.

### 힙 move와의 직교성

스택 슬롯 재바인딩은 스택 타입에만 작용하며 소유권 전달이나 `free`와는 무관하고, 힙 타입의 clone/move 메커니즘과 완전히 독립적입니다.

**테스트**: `tests/slot-rebind.no`(i64/u64/i128/u128/txt 재바인딩, copy로의 강등, 대상 재할당 후 별칭 무효화, 매개변수 move 강등, consume 인자 전달을 포괄하며 기대 출력은 `42 7 1 1 17 5 5 10 100 99 84`).

## FFI extern str 반환값

FFI extern 함수(`#{c}`로 표시됨)가 반환하는 C 문자열 포인터(`i8*`)는 정적 메모리(예: `getenv`, `strerror`)를 가리키거나 외부 buffer를 가리킬 수 있습니다(예: `strchr`이 반환하는 포인터는 인자 내부를 가리킴). 이를 `%str-long`에 직접 감싸면 `emitHeapFree`에서 힙이 아닌 메모리를 `free()`하게 되어 UB가 발생합니다.

컴파일러는 FFI extern `str` 반환 경로에 안전한 복사를 삽입합니다:

1. **NULL 검사**: C가 NULL을 반환하면 빈 `%str-long`(data=0, len=0)을 생성합니다. `str`은 option이 아니므로 **`s == nil`은 컴파일 오류**입니다(`q4nilcmp`). NULL과 빈 문자열을 구분하려면 extern을 `?str`을 반환하도록 선언하거나(NULL ⇒ `nil`) `s.len == 0`으로 판별하세요.
2. **NULL이 아님**: `strlen` + `malloc` + `memcpy` + null 종료를 통해 독립 힙 버퍼로 복사합니다
3. **PHI 병합**: 두 경로를 병합하여 `%str-long`을 구성해 반환합니다

```no
#{c}
strchr = (s str, c i64) (r str)

find = () (r str) {
    r = strchr('hello', 108); C 返回指向 'hello' 內部的指標
    ; 編譯器自動 malloc+memcpy 複製，r 獨立擁有 data
    ; 函數結束時 emitHeapFree 安全釋放 r.data
}
```

이 메커니즘은 clib의 `RetCStrToStr` 경로(`get-env`, `get-wd` 등 내장 함수에 사용됨)와 논리적으로 동일하여, 모든 C 문자열 반환값이 독립적인 소유권을 갖도록 보장합니다.

## 모듈 수준 변수 해제

모듈 수준 힙 변수(`vec`/`str`/`arr`/구조체)는 LLVM global(`@name`)로 컴파일되며, 그 `data` 버퍼는 `main` 진입점의 top-level 문장에서 malloc으로 초기화됩니다.

컴파일러는 C 진입점 `main`의 `ret i32 0` 앞에서 다음을 호출합니다:
1. `emitHeapFree` — top-level 지역 힙 변수(globalVars가 아닌 것)를 해제
2. `emitGlobalHeapFree` — `moduleVarTypes`를 순회하며 `globalVars`에 있는 모든 힙 소유 타입을 해제

```no
GLOBAL-STR = 'hello'; LLVM @GLOBAL-STR = global %str-long zeroinitializer
GLOBAL-VEC = [1, 2, 3]; LLVM @GLOBAL-VEC = global %vec zeroinitializer
; top-level 語句 malloc data 並存入 global
; main ret 前 emitGlobalHeapFree 釋放 data
```

이는 장기 실행 서비스(예: 반복문이 있는 데몬)에서 메모리가 누적되는 누수를 방지합니다. 일회용 CLI 도구에는 영향을 주지 않습니다(프로세스 종료 시 OS가 회수함).

## 슬라이스 뷰

슬라이스 표현식 `arr[1..3]`은 뷰를 생성합니다(0회 복사). 원본 배열의 data를 공유하며, 뷰의 세 가지 운명은 다음과 같습니다:

| 대상 | 동작 | 소유권 |
|------|------|--------|
| 지역 변수 `v = arr[1..3]` | 0회 복사 뷰 | 원본 배열 data 공유 |
| 출력 매개변수 `out = arr[1..3]` | clone(malloc+memcpy) | 독립 소유 |
| 명시적 `[]T` 타입 `v []i64 = arr[1..3]` | clone | 독립 소유 |

**이유**: 출력 매개변수는 호출자로 탈출하므로 원본 배열이 함수 종료 전에 free될 수 있습니다. 따라서 뷰를 독립 data로 clone해야 합니다.

## 재할당과 이전 값 해제

```no
s = 'hello'; malloc data 緩衝區
s = 'world'; 釋放 'hello' 的 data，malloc 新 data
```

힙 소유 타입을 다시 할당할 때 컴파일러는 할당 전에 이전 값의 data를 자동으로 해제하여 누수를 방지합니다.

## 구조체 필드 해제

```no
Node {
    name str
    items []i64
}

n = Node {
    name: 'hello'
    items: [1, 2, 3]
}
; 函數結束時遞迴釋放：
;   - n.name.data（%str-long 欄位）
;   - n.items.data（%vec 欄位）
```

구조체를 해제할 때는 모든 필드를 순회하며, 힙 소유 타입 필드의 data를 재귀적으로 해제합니다.

## 고정 배열을 슬라이스로 재할당

```no
local [4]i64 = [100, 200, 300, 400]; local 是固定陣列（16 字節）
local = [100, 200, 300]; 重新賦值為切片（16 字節）
```

고정 배열(`%arr`, 2개 필드)과 슬라이스(`%vec`, 3개 필드)는 메모리 레이아웃이 다릅니다. 재할당 시 컴파일러가 자동으로 새 `%vec` 변수를 할당하고 모든 후속 접근을 리다이렉트하여 버퍼 오버런을 방지합니다.

## 검증된 테스트 사례

테스트는 `tests/mem-safety/`에 있습니다:

| 테스트 | 검증 내용 |
|------|---------|
| `deep-clone.no` | `b = a` 깊은 clone([]i64/[]str/str/구조체)의 독립성 |
| `deep-free-str.no` | `[]str` 깊은 free |
| `deep-free-nested-vec.no` | `[][]i64` 깊은 free + push 깊은 clone |
| `deep-free-struct-vec.no` | `[]MyType` 깊은 free(재귀 구조체) |
| `struct-field-leak.no` | 구조체 필드의 힙 데이터 해제 |
| `slice-view-escape.no` | 슬라이스 뷰를 출력 매개변수에 할당할 때의 clone |
| `reassign-leak.no` | 재할당 시 이전 값 해제 |
| `vec-push-leak.no` | vec.push 확장 시 이전 buffer 해제 + 힙 소유 요소 깊은 clone |
| `ffi-str-return.no` | FFI extern str 반환값의 안전한 복사 |
| `global-heap-free.no` | 모듈 수준 힙 변수가 main 종료 시 해제됨 |
| `double-move-same-source.no` | `a=x; b=x` 동일 소스 다중 할당: 첫 번째는 clone + 마지막은 move |
| `move-clone-liveness.no` | Liveness 사전 분석이 clone/move 결정(조건 분기, 세 번의 할당 포함) |
| `rule1-binding.no` | 규칙 1: `b = a`의 `str`／`[]i64`／struct／`?str`／`?[]i64` 쓰기 독립성, 읽기 전용 별칭, 소스가 죽은 경우의 move |
| `nested-container-clone.no` | 중첩 컨테이너의 깊은 clone／깊은 free 쌍(`[][]str`, struct 내부 vec, map value가 vec 등 12개 사례) |

## 알려진 제한 사항

### map 컨테이너
hashmap은 key/value에 대한 깊은 free가 구현되지 않아 map 컨테이너의 힙 데이터가 누수됩니다.

### 반복문 임시 변수
```no
#{fmt-skip}
loop {
    s = 'temp'   ; 每次迭代 malloc 新 data，舊 data 未釋放
}
```

### 슬라이스 뷰 + 원본 배열 move
```no
view = arr[1..3]; view 共享 arr.data
arr = [9, 8, 7]; 釋放舊 arr.data → view 懸空
```

### async 공유 데이터(2026-09-30 수정 완료)

`awy { r = co f(x) }`의 spawn 경계는 이제 힙을 소유한 인자를 **깊은 복사**하며, 태스크가 호출자와 buffer를 공유하지 않습니다.

코루틴은 협동적이며 단일 스레드이므로 "코루틴 간에 변경 가능한 힙 데이터를 공유하는 것"은 언어 수준에서 성립하지 않습니다. 유일하게 공유되는 것은 **호출자가 spawn 이후에도 여전히 보유한** 값입니다. 수정 전에는 argbuf가 인자 디스크립터의 **비트 복사**에 불과하여 `data`를 호출자와 공유했고, 태스크는 대기 지점에 이르러서야 실행되었습니다. 호출자가 그 전에 재할당하거나 스코프를 벗어나면 buffer가 해제되고, 태스크는 해제된 메모리를 읽게 되었습니다(실측: str은 NUL 40개를 출력하고 `[]i64`는 0을 출력했으며, 둘 다 rc가 0이고 진단도 없었습니다).

이제 spawn 시점에 즉시 깊은 복사를 수행하고, wrapper가 호출 종료 후 복사본을 해제합니다:

| 매개변수 타입 | spawn 경계 | wrapper 해제 |
|---|---|---|
| `str` | `@str_clone` | `@str_free` |
| `[]T`(T에 deep-clone helper가 있음) | `vecDeepClone` | `@vec_free` |
| 힙을 소유한 struct(인라인 `str` 리프／포인터 필드 포함) | 제자리 리프 clone ＋ pointee 복사 | 재귀 구조체 소멸자 |
| 기타(순수 값 타입) | 비트 복사(올바름) | 컨테이너만 해제 |

회귀 테스트: `tests/async-ownership.no`.

### struct 재할당 누수(2026-09-30 수정 완료)

```no
holder {
    s str
}
h holder = holder {
    s: 'first'
}
h = holder {
    s: 'second'
}
; 舊的 'first' buffer 曾洩漏
```

지역 변수를 재바인딩할 때 이전 값은 그 타입이 `Type.Owned`(`str`/`vec`/`[]T`/`map`/`?owned`)일 때만 해제됩니다. struct는 **의도적으로** `Type.Owned`로 간주되지 않습니다(이 플래그는 lowerer가 "바인딩이 별칭인지"를 판단하는 근거이기도 함). 따라서 `h = holder { … }`는 구조체 전체를 덮어써서 옛 `s` buffer가 절대 해제되지 않았습니다. 하지만 drop 메커니즘은 struct를 실제로 인식합니다(`dropOwnsHeap` → `typeOwnsHeap`, 그리고 `emitDrop`이 재귀 소멸자로 해제). 문제는 lowerer의 관문이 너무 좁았던 것입니다. 수정 후에는 재바인딩 전에 이전 필드를 먼저 해제합니다.

2,000,000회 재할당 실측 결과: 최대 RSS가 66.1 MB → 33.8 MB로 감소했으며, 출력은 바이트 단위로 동일합니다.

### struct literal의 첫 번째 필드 값이 메서드 호출인 경우(2026-09-30 수정 완료)

```no
holder {
    s str
}
h = holder {
    s: n.to-str()
}
; 修復前：'holder' is not defined
```

`{ … }`의 분류는 몇 개의 token을 미리 살펴보아 결정됩니다. match의 **arm 구분자도 `:`**이므로, `s: n.to-str()`는 "pattern이 `s`인 match arm"과 "struct literal의 필드"처럼 동시에 보입니다. 둘을 구분하는 유일한 단서는 `ident.ident` **뒤에 오는** token입니다. 즉 메서드 호출은 필드 값일 수밖에 없습니다. 기존 분기에서는 `(`를 놓쳐 전체 블록이 match로 판정되었고, struct 이름을 match 문맥에서 찾지 못해 미정의 오류가 발생했습니다(필드 값이 일반 함수 호출이면 정상이었습니다).

수정 후에는 `h = holder { s: recv.method(args) }`가 정상적으로 파싱됩니다. nolang의 match arm은 `name : X(`로 시작할 수 없습니다(arm 구분자가 `->`이고, `:`로 시작하는 arm은 반드시 wildcard/default이며 본문에 괄호 호출이 없음). 따라서 `(`를 허용해도 정상적인 match를 struct literal로 오판하지 않습니다. 회귀 테스트: `src/parser/struct_literal_field_value_test.go`.

### await 결과 타입(2026-11-17 문법 단순화)

이전 문법은 task handle을 `i64` 변수로 노출하여 "handle 슬롯 재바인딩"을 넘나들며 결과 타입을 옮겨야 했습니다(`t i64 = 0; t = run f(...); v = awy t`). `run`과 `awy <handle>`가 제거된 후에는 `co` 표현식의 결과 타입이 callee에 의해 직접 결정되므로 이 경로는 더 이상 존재하지 않습니다. 회귀 검증은 이제 `src/mir/async_boundary_ownership_test.go`가 하위 MIR의 결과 타입을 고정합니다.

### 태스크 참조 횟수(2026-11-17 문법 단순화)

내부 `%task`는 여전히 유일한 참조 횟수 객체입니다: spawn 생성 시 `rc = 1`이고, 대기가 완료되면 `release`됩니다. 이전 문법은 수동 handle을 노출하여 `h2 = h` / `run h` 같은 복사 경로에서 retain 계산이 누락되는 경우가 있었습니다(void 결과 태스크에서 특히 심각하여 실측 SIGSEGV가 발생했고, `checkRefBalance` 검증기가 이를 차단했습니다). 새로운 `awy { ... }`는 handle을 노출하지 않으며, 그룹 끝에서 각 `co`를 정확히 한 번씩 대기하므로 겉으로 드러난 문법에서는 목록 외 참조가 발생하지 않습니다. handle 별칭 / RC에 대한 `.no` 및 Go 테스트는 `run`과 함께 제거되었습니다. MIR 수준의 `OpTaskRetain` / `OpAwait` 쌍은 여전히 `src/mir/opt_test.go`의 MIR fixture로 검증됩니다.

### spawn 인자의 이동: 소스가 죽었을 때 0회 복사(2026-10-01)

`co f(x)`는 **힙을 소유한** 각 인자에 대해 spawn 경계에서 깊은 복사를 한 번 수행합니다. 태스크가 그룹 끝에서야 실행될 수 있고 호출자가 그 전에 재할당하거나 스코프를 벗어날 수 있기 때문입니다. x가 spawn 이후 **더 이상 사용되지 않는 것이 확실할** 때 그 복사는 불필요하므로, 소유권이 태스크로 직접 **전달**됩니다.

```no
s str = 'hello'
awy {
    v = co echo(s)      ; s 之後不再被讀 -> 零拷貝轉移
}
print(v)                ; hello

s str = 'hello'
awy {
    v = co echo(s)
}
print(s)                ; s 仍被讀 -> 維持深拷貝（兩邊各自擁有）
print(v)                ; hello
```

판단 기준은 세 가지이며 모두 필요합니다:

1. 호출자가 **원래 x를 해제할 예정**이어야 함 — 빌리기(슬라이스 뷰, borrow read)와 매개변수는 호출자가 해제하지 않음;
2. x가 spawn 이후 죽어 있음(**어떤 후속 읽기도 없음**);
3. callee의 해당 위치 매개변수가 `str` / `[]T` / 힙을 소유한 `struct`여야 함 — wrapper가 그 페이로드를 해제함.

이 세 종류에 대해 태스크 wrapper의 해제와 호출자 원래의 drop이 **동일한 helper로 축소**되므로, 전달이란 **같은 해제 한 번**을 호출자에서 태스크로 옮기는 것에 불과하며 더 많이 혹은 더 적게 해제하지 않습니다.

**변하지 않는 사용자 시맨틱스**: 대기되지 않은 태스크의 인자 buffer는 원래 wrapper가 대기 시점에 해제합니다. 이동과 복사 모두 이 점에서 완전히 동일합니다.

구현: `insertDrops`가 `Module.spawnArgMoves`에 기록하고, `emitAsyncRun`이 이를 근거로 깊은 복사를 건너뜁니다. 회귀 테스트: `tests/async-arg-move.no`, `src/mir/spawn_arg_move_test.go`.

### 코루틴 대기자 테이블 별칭(2026-09-30 수정 완료)

스케줄러는 원래 `@nolang_waiters = global [256 x i8*]`를 사용하고 `ptrtoint(task) & 255`를 인덱스로 삼았습니다. 태스크는 `@malloc`으로 할당되고 **16바이트 정렬**되므로 포인터 하위 비트 중 유효한 것은 4개뿐입니다. 즉 256개 슬롯이 실제로는 16개로 축소되었고(실측에서는 8개만 사용됨), 동시에 대기하는 두 태스크가 서로의 대기자를 덮어써 잘못된 태스크를 깨우기 쉬웠습니다.

이제 전체 테이블을 제거하고 대기자를 `%task` 자신의 다섯 번째 필드에 저장합니다(`%task`가 24바이트에서 32바이트로 증가). `@nolang_async_wait`가 현재 태스크를 **대기 대상**의 필드에 기록하고, `@nolang_async_done`이 해당 필드를 읽어 비어 있지 않으면 큐에 넣고 비웁니다. 따라서 조회가 정확해지고 주소 별칭도 사라졌습니다.

> 참고: 현재 MIR 백엔드는 await를 동기적으로 구동하며 `@nolang_async_run`／`@nolang_async_wait`를 호출하지 않습니다. 따라서 이 항목은 오늘날 **잠재된** 결함이지 관측 가능한 고장은 아닙니다. 회귀 테스트: `src/mir/async_boundary_ownership_test.go`.

### `awy`가 블록의 첫 번째 문장이 아닐 때 삼켜지는 문제(2026-09-30 수정 완료; 2026-11-17 재작성)

`skipToStatementEnd()`는 "다음 문장을 **시작할 수 있는**" token에 이르러서야 멈추는데, 허용 목록 `isStatementBoundary()`(`src/parser/stmt.go`)에 접두 키워드가 누락되어 있었습니다. 그 결과 이전 문법의 `run` / `awy t`가 첫 번째 문장이 아닐 때 개행과 함께 삼켜져 `rc=0`이고 진단 없는 시맨틱스 퇴화가 발생할 수 있었습니다.

새 문법에서 `awy`는 자체적인 `parseStatement` case(코루틴 그룹)를 가지며 `co`와 함께 `isStatementBoundary()`에 등록됩니다. `run`은 더 이상 키워드가 아니므로 허용 목록에서 제거되었습니다. 규칙은 다음과 같습니다: **`parseStatement`의 `switch`가 분기할 수 있는 모든 token은 동시에 `isStatementBoundary()`에도 포함되어야 한다**.

회귀 테스트: `tests/stmt-boundary.no`, `src/parser/stmt_boundary_block_test.go`.

### 문장 위치의 맨 `{ ... }`가 선언으로 취급되는 문제(2026-09-30 수정 완료)

`parseStatement`의 `LBRACE` 분기는 `classifyBlockAtCurrent()`를 호출합니다(이때 `currentToken`은 이미 `{`이며 소모할 이름이 없음). 이 함수가 반환하는 `blockEnum` / `blockIface` / `blockTaggedEnum`은 **문장 위치**에서 아무 의미가 없습니다. 진짜 열거형／인터페이스／태그 열거형 선언은 모두 앞에 이름이 필요하며 더 앞선 `classifyBlock()` 분기를 거칩니다. 그러나 문장 분기는 `blockUnknown`만 문장 블록으로 보내므로 다음과 같습니다:

- `{ x = 2 }`는 `blockEnum`으로, `{ print(2) }`는 `blockIface`로 분류됨;
- 둘 다 `parseExpressionStatement`로 보내지고, `expr.go`의 `LBRACE` 분기는 `blockStruct` / `blockMatch`만 인식하여 나머지는 `p.nextToken(); return nil`을 따름;
- `{`가 삼켜져 블록 내부 문장이 바깥 본문의 **형제**가 되고, 정작 블록 자신의 `}`가 **외부** 블록을 닫아버림.

결과는 "삼킴"이 아니라 **문장이 한 겹 새어 나가는 것**입니다. `f = () { print(1); { print(2) }; print(3) }`는 `3,1,2`를 출력하고, `f = () { x = 1; { x = 2 }; print(x) }`는 아무것도 출력하지 않습니다. 마찬가지로 `rc=0`이고 진단이 없습니다.

수정 방법은 문장 분기에서 `blockEnum` / `blockIface` / `blockTaggedEnum`을 함께 문장 블록으로 유도하는 것입니다(`blockStruct`는 **표현식 경로에 남겨야** 함 — `{ field: value }`는 유효한 익명 구조체 리터럴 문장이기 때문).
⚠️ 이 문제를 "고치기" 위해 `classifyBlockAtCurrent`의 `ASSIGN` 분기를 바꾸지 **마세요**: 그것은 세 가지 오분류 중 하나만 다루며, 판단을 "내가 문장 위치에 있다"는 것을 아는 유일한 곳에서 옮겨버립니다.

⚠️ **연쇄 영향**: 이 수정으로 문장 위치의 `{ ... }`가 처음으로 실제 `*parser.BlockStatement`로 살아남게 되었는데, `programUsesPrint`(`src/build/transpiler.go`에 있으며 `fmt`/`io`/`str`/`byte`를 로드할지 결정하는 수작업 AST walker)의 허용 목록에 `BlockStatement`가 없어 ⇒ `print`를 맨 블록 안에만 쓴 프로그램이 더 이상 감지되지 않습니다.
**문장 수준을 건드렸다면 `go test ./build/ -run TestProgramUsesPrint`를 다시 실행해야 하며**, 새로운 컨테이너 타입을 `walkStmt`에 보충해야 합니다. 회귀 테스트: `src/parser/stmt_boundary_block_test.go`, `tests/stmt-boundary.no`.

### 전역 변수 첫 할당의 free 건너뛰기 판단이 충분히 정확하지 않음

컴파일러는 컴파일 타임 map `globalFirstAssigned`를 사용하여 전역 변수의 첫 할당 여부를 추적합니다. 첫 할당에서는 이전 값의 해제를 건너뜁니다(이전 값은 `zeroinitializer`이며 힙 데이터가 아님). 이후의 재할당에서만 옛 힙 값을 해제합니다.

이 map은 전체 컴파일 과정에서 한 번만 초기화되고 함수 단위로 재설정되지 않으며, 조건 분기 경로도 구분하지 않습니다. 전역 변수가 조건 분기 안에서 처음 할당되면 컴파일러는 AST 순서대로 처리합니다. 첫 번째 할당 문장은 "최초"로 표시되어 free를 건너뛰고, 두 번째 할당 문장(다른 분기에 있어도 상관없이)은 재할당 경로를 따라 옛 값을 해제하려 합니다. 런타임에 두 번째 분기가 먼저 실행되면 전역은 여전히 `zeroinitializer`(data=NULL, len=0)이므로 초기화되지 않은 옛 값을 해제하려 시도합니다.

**현재 완화 조치(효과 있음)**:
- 얕은 컨테이너(`%str-long`): `emitNullCheckFree`가 런타임 `icmp eq i8* dataPtr, null` 검사를 생성하여 NULL이면 `call @free`를 건너뜀
- 깊은 컨테이너(`%vec/%arr`): `emitDeepContainerFree`에 `len == 0` 단락 검사가 추가되어 있어, zeroinitializer의 len이 0이므로 전체 해제 루프를 바로 건너뜀

이 두 겹의 런타임 방어 덕분에 컴파일 타임 판단이 충분히 정확하지 않아도 실제로는 충돌하지 않습니다. 다만 논리적으로는 컴파일 타임의 정확한 판단이 아니라 런타임 NULL 검사를 안전망으로 의존하고 있습니다.

**개선 가능성**: `globalFirstAssigned`를 컴파일 타임 map에서 런타임 추적 메커니즘(`movedVarBitset`과 유사한 비트맵)으로 바꾸는 방안이 있으나, 런타임 오버헤드가 증가하고 현재 완화 조치가 이미 충분히 효과적입니다.

## 설계 선택

1. **깊은 clone의 비용.** `b = a`는 data를 하나 할당하고 복사하며, `vec.push(x)` 역시 힙 소유 요소에 대해 깊은 clone을 한 번 수행합니다.
   이는 의도된 설계입니다. **한 번의 복사로 별칭 분석 전체를 없앴습니다.** 값과 값 사이에는 "소유"와 "이동"만 남기고 공유는 남기지 않으므로,
   "누가 소유하고, 누가 해제하며, 쓰기가 관측 가능한지"를 모든 별칭 상황마다 따로 규칙으로 작성할 필요가 없습니다.
2. **spawn 경계의 깊은 복사(= 스냅샷).** `co f(x)`는 힙을 소유한 인자에 대해 spawn 시점에 깊은 복사를 한 번 수행하므로, 태스크는
   **spawn 당시의 값**을 보며 그 이후 호출자의 쓰기와는 무관합니다. **소스가 죽은 경우에는 0회 복사 move로 바뀝니다**.
   ⚠️ 이 복사를 아끼려고 "같은 buffer 공유 ＋ 참조 횟수"로 바꾸지 마세요. 그렇게 하면 태스크가 spawn과 그룹 끝 대기 사이에 호출자가 수행한
   **제자리 쓰기**를(공유를 통해) 보게 되어 4번 원칙을 위반합니다. 복사를 줄이는 올바른 방법은 "**쓸 때 복사**"(copy-on-write)입니다.
3. **태스크 참조 횟수는 순환을 만들지 않음.** 내부 `%task`가 유일한 참조 횟수 객체이며, 새로운 `awy { ... }`는 handle을 노출하지 않고
   각 `co`를 그룹 끝에서 정확히 한 번씩 대기하므로 "태스크가 서로 handle을 보유하는" 순환이 형성되지 않습니다.
4. **코루틴 간에 변경 가능한 힙 데이터를 공유하지 않음.** 코루틴은 **협동적, 단일 스레드**로 스케줄링되므로 코루틴 간에 변경 가능한 힙 데이터를 공유하는 것은 언어 수준에서 성립하지 않습니다;
   공유를 허용하면 데이터 레이스와 댕글링 포인터만 생깁니다. 따라서 spawn 경계에서는 항상 **독립적인 사본**을 전달합니다(또는 소유권을 전달).
