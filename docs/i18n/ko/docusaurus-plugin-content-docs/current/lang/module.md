---
sidebar_position: 6
---

# 모듈 간 호출 접두사

Nolang는 강제적인 모듈 네임스페이스 규칙을 사용합니다. 하나의 `.no` 파일에서 **다른 모듈**(다른 `.no` 파일)이 정의한 함수나 상수를 호출할 때는 반드시 `ShortName.` 접두사를 사용해야 합니다. 이는 모듈 간 이름 충돌을 방지합니다.

표준 라이브러리 모듈은 컴파일러가 자동으로 로드하므로 **명시적으로 가져올 필요가 없습니다**(`# std/...` 애노테이션이 필요 없습니다). `ShortName.` 접두사를 직접 사용해 호출하면 됩니다. `no fmt`는 별칭이 없는 순수 std 모듈 가져오기(예: `# std/noml`, `# std/math`, `# std/fmt`)를 모두 자동으로 제거합니다. 별칭(`# std/noml n`)이나 함수 가져오기(`# std/math.sqrt`)는 유지됩니다(자세한 내용은 [코딩 스타일 · 가져오기 문 자동 정리](./code-style#가져오기-문장-자동-정리중복-제거--별칭-없는-std-가져오기-제거--정렬) 참조).

> **의존성 및 경로 해석**: 로컬 패키지의 의존성 키는 `workspace.noml`을 통해 짧은 이름이나 전체 URL 형태로 매핑할 수 있습니다. 자세한 내용은 [의존성 유형 및 버전 번호 규칙](../usage#依賴類型與版本號規則) 참조.

## ShortName 정의

ShortName은 모듈 경로의 마지막 세그먼트로, 모듈 간 호출 시 접두사로 사용됩니다.

| 파일 경로 | FullPath | ShortName | 설명 |
| --- | --- | --- | --- |
| `std/math.no` | `math` | `math` | 최상위 파일 |
| `std/fs.no` | `fs` | `fs` | 최상위 파일 |
| `std/net/net.no` | `net/net` | `net` | 경로의 마지막 세그먼트 |
| `std/net/client.no` | `net/client` | `client` | 경로의 마지막 세그먼트 |
| `std/crypto/sha256.no` | `crypto/sha256` | `sha256` | 경로의 마지막 세그먼트 |
| `std/archive/gzip.no` | `archive/gzip` | `gzip` | 경로의 마지막 세그먼트 |

ShortName은 FullPath를 슬래시로 구분한 뒤의 마지막 세그먼트입니다(예: `crypto/sha256` → `sha256`).

## 접두사가 필요한 경우

다른 모듈이 정의한 모듈 수준 함수나 상수를 호출할 때는 반드시 `ShortName.` 접두사를 사용해야 합니다.

```no
; 模組級函數
sha256.sha256(data)
sha256.sha256-hex(data)
fs.open(path, opts)
gzip.gzip-decompress(data)
math.pow(x, y)

; 模組常量
net.NET-BUF-SIZE
math.PI
```

### 함수 명명 규칙

**함수 이름 앞에 모듈 이름 접두사를 붙이지 마세요.** 모듈 내부의 함수는 짧고 직관적인 이름이면 충분합니다. 모듈 간 호출 시 `ShortName.` 접두사가 모듈 정보를 자동으로 나타내 줍니다.

```no
; ✅ 正確：函數名簡潔，不加模組名前綴
; tail.no
; 入口函數直接用模組名
tail = () {
    ...
}

; 輔助函數用簡短名稱
atoi = (s str) (v i64) {
    ...
}
```

```no
#{fmt-skip}
; ❌ 避免：函數名冗餘帶上模組名前綴
; tail-run = () { }
; tail-atoi = (s str) (v i64) { }
```

모듈 간 가져오기 때에도 동일하게 유지됩니다.

```no
; ✅ 簡潔直觀
# /src/tail.tail
# /src/mktemp.mktemp
```

```no
#{fmt-skip}
; ❌ 冗餘
; # /src/tail.tail-run
; # /src/mktemp.mktemp-run
```

> **키워드를 피하세요**: `awy`(코루틴 그룹 키워드), `match`(조건 매치 키워드) 등은 함수 이름으로 사용할 수 없습니다. `run`은 제거되어 일반 식별자로 사용할 수 있습니다. 진입점 함수는 모듈 이름 자체를 그대로 사용하는 것이 좋습니다(예: `ping.no` → `ping`).

## 접두사가 필요 없는 경우

다음 경우에는 접두사가 필요하지 않습니다.

### 1. 전역 함수(`with-cap` / `with-len` / `with-cap-len` / `print` / `eprint` / `format` / `exit`)

이 7개 함수는 언어 수준의 전역 내장 함수로, 모듈 접두사 없이 직접 사용할 수 있습니다. 이들의 주석 선언은 개발자가 쉽게 확인할 수 있도록 `std/global.no`에 모두 모여 있습니다.

**용량/길이 생성:**

- `with-cap(cap)` — 지정한 용량의 문자열이나 슬라이스를 생성합니다(len=0). 타입은 할당문 왼쪽에서 추론됩니다.
- `with-len(len)` — 지정한 길이의 문자열이나 슬라이스를 생성합니다.
- `with-cap-len(cap, len)` — 지정한 용량과 길이의 문자열이나 슬라이스를 생성합니다.

**출력/포맷팅:**

Nolang는 **이름이 있는 형식 문자열** 문법 `{name[:spec]}`을 사용합니다. 스코프에 있는 변수를 직접 참조하므로 위치 인자가 필요 없습니다. 컴파일 타임 검증을 지원합니다. 출력은 `io.out`/`io.err` 시스템 호출을 통해 직접 기록되며, libc의 `printf`에 의존하지 않습니다.

- `print(s)` / `print(s0, s1, ...)` — 표준 출력으로 출력하며, 여러 인자는 공백으로 구분하고 **자동으로 줄바꿈**합니다.
- `eprint(s)` / `eprint(s0, s1, ...)` — 표준 오류로 출력하며, 여러 인자는 공백으로 구분하고 **자동으로 줄바꿈**합니다.
- `format(s)` — 형식화된 문자열을 반환합니다(`sprintf` 대체). 자동으로 줄바꿈하지 않습니다.

**프로세스 제어:**

- `exit(code)` — 지정한 상태 코드로 프로세스를 종료합니다(기존 `os.exit`였으나 전역 내장 함수로 승격되어 `os.` 접두사가 필요 없습니다).

**인수가 여러 개일 때 리터럴은 기본적으로 템플릿이며, C 스타일 형식 문자열이 아닙니다.** `print('result={val}', 42, 'result={val}')`에서 각 문자열 리터럴은 **각각** 하나의 템플릿입니다. 즉 `{val}`은 **호출 지점의 스코프**에서 값을 가져오며, 같은 호출의 다른 인자와는 무관합니다. 리터럴은 뒤따르는 인자를 기술하거나 소비하지 **않습니다**. `printf('%d', 42)` 같은 치환이 아닙니다. 리터럴이 아닌 인자(변수, 표현식)는 일반 가변 인자로 처리되어 공백으로 구분되며, 전체 줄 끝에 줄바꿈이 하나만 있습니다.

```no
val = 42
print('result={val}', 42, 'result={val}'); result=42 42 result=42
tmpl = '{val}'
print(tmpl); {val} —— 變數是純文字，不做替換
print('a={val}', val); a=42 42
```

`format(s)`와 이미 폐기된 `sprintf(s)`는 여전히 **단일 형식 문자열**만 받으며, 여러 인자 형태는 없습니다.

> `printf`, `eprintf`는 **제거되었습니다**: 이름은 여전히 심볼 테이블에 남아 있어(호출은 계속 해석되고 편집기에서도 자동 완성이 가능합니다)
> 하지만 **모든 호출은 하드 컴파일 오류**입니다(`[printf-depr]`). 오류 메시지가 마이그레이션을 직접 안내합니다. `sprintf`는 여전히 사용할 수 있지만 **폐기되었습니다**.
> 대체 관계는 다음과 같습니다:
> - `printf(s)` → `print(s)`(자동 줄바꿈) 또는 `io.out(s)`(줄바꿈 없음, 표준 출력)
> - `eprintf(s)` → `eprint(s)`(자동 줄바꿈) 또는 `io.err(s)`(줄바꿈 없음, 표준 오류)
> - `sprintf(s)` → `format(s)`(형식화된 문자열 반환)
>
> `io.out`/`io.err`는 저수준 명령으로, 출력 시 **줄바꿈하지 않습니다**. 모듈 호출에는 모듈 이름을 붙여야 하므로 `io.err`는 모듈 접두사를 명확히 하여 Option 생성 함수 `err()`와 충돌하지 않습니다. 이름이 같더라도 모듈 접두사로 구분할 수 있습니다.

```no
; 容量/長度構造（無前綴）
s str = with-cap(256); ✅ 預分配 256 位元組的 str
v []i64 = with-cap(100); ✅ 預分配 100 個元素的切片
v []i64 = with-cap-len(200, 100); ✅ 容量 200、長度 100
v []i64 = with-len(100); ✅ 長度 100 的切片

; 輸出/格式化（無前綴）
print('hello {n}'); ✅ 自動換行
print(a, b, c); ✅ 多參數，空格分隔，自動換行
print('x={n}', n); ✅ 字面量是模板，變數是值
print('result={val}', 42, 'result={val}'); ✅ 每個字面量各自替換
print(); ✅ 空參數，只輸出換行
s = format('x={x}'); ✅ 返回格式化字串（單一格式字串）
eprint('err: {n}'); ✅ 輸出到 stderr 並換行
eprint('err:', a, b); ✅ 多參數，stderr 空格分隔
print('編號 {id:06} 金額 {money:.2f}'); ✅ 支援對齊、填充、寬度、精度

; 底層命令（需要模組前綴）
io.out('no-newline-here'); ✅ 輸出不換行（替代 printf）
io.err('err-no-newline'); ✅ stderr 不換行（替代 eprintf）

; 其他所有跨模組調用都需要前綴
fs.open(path, opts); ✅ 帶前綴（builtin 也需要）
```

#### 형식 지정자

`{name[:spec]}`에서 `spec`은 다음 필드를 지원합니다(순서는 고정이며 모두 생략 가능합니다):

```
[[fill]align][sign][#][0][width][.precision][type]
```

- `fill` — 채움 문자(기본값은 공백)이며, `align`과 함께 사용해야 합니다.
- `align` — `<`는 왼쪽 정렬, `>`는 오른쪽 정렬, `^`는 가운데 정렬
- `sign` — `+`는 양수에 더하기 기호를 표시, `-`는 음수에만 기호를 표시(기본값)
- `#` — 진수 접두사(`0x`/`0o`/`0b`)
- `0` — 숫자 앞을 0으로 채워 지정한 너비까지 맞춥니다.
- `width` — 최소 필드 너비
- `.precision` — 부동소수점 소수 자릿수 / 문자열 잘라내기 길이
- `type` — `d`(정수), `x`/`X`(16진수), `o`(8진수), `b`(2진수), `c`(문자), `f`(고정소수점), `e`/`E`(과학적 표기법), `g`/`G`(일반), `s`(문자열, 기본값), `t`(데이터 타입 이름, 컴파일 타임에 결정), `v`(리터럴 값, 문자열은 작은따옴표로 감싸고 vec는 to-str 호출)

```no
x i64 = 42
u u64 = 255
pi f64 = 3.14159
s str = 'hello'
print('{x:06}'); 000042
print('{x:>10}'); 右對齊寬度 10
print('{u:#x}'); 0xff
print('{pi:.2f}'); 3.14
print('{pi:8.3e}'); 3.142e+00
print('{s:<10}'); hello     （左對齊）
print('{s:.3}'); hel（截斷為 3 字元）
print('{x:t}'); i64（印出變數 x 的型別名）
print('{s:v}'); 'hello'（字串用單引號包裹）
```

`{{`와 `}}`는 리터럴 `{`와 `}`를 출력하는 데 사용되지만, **같은 문자열에 실제 `{name}` 필드가 하나 이상 있을 때만** 적용됩니다. 이스케이프만 있고 필드가 없는 리터럴은 있는 그대로 출력됩니다. `print('{{n}}')`는 `{{n}}`을 출력하고(`{n}`이 아님), `print('{{')`는 `{{`를 출력합니다. 중괄호 하나를 출력하려면 `print('{')` / `print('}')`를 직접 사용하면 됩니다.

### 2. 같은 파일 내 정의

같은 `.no` 파일 내에 정의된 함수, 상수, 메서드는 접두사 없이 직접 사용할 수 있습니다.

```no
; 在 sha256.no 中：
sha256(data); sha256 定義在本檔案

; 常量定義在本檔案
HMAC-BLOCK-SIZE
```

### 3. 내장 타입 메서드

내장 타입(`str`, `i64`, `vec`, `arr`, `byte`, `char`, `bool` 등)의 메서드 호출에는 접두사가 필요하지 않습니다. 메서드는 타입에 내장되어 있으며 수신자 타입을 통해 직접 해석됩니다.

```no
'hello'.starts-with('he'); str 方法
n.to-str(); int 方法
v.push(42); vec 方法
a.contains(3); arr 方法
c.is-digit(); char 方法
```

### 4. 구조체 인스턴스 메서드

이미 생성된 구조체 인스턴스의 메서드를 호출할 때는 모듈 접두사가 필요하지 않습니다. 메서드는 인스턴스의 타입을 통해 해석되며, 컴파일러가 해당하는 `struct.method` 정의를 자동으로 찾습니다.

```no
f = fs.open(path, opts); fs.open 是模組級函數，需前綴
f.read(buf, n); file.read 是結構體方法，不需前綴
f.close(); file.close 是結構體方法，不需前綴

p = path {
    p: '/tmp'
}
p.exists(); path.exists 是結構體方法，不需前綴
```

## 메서드 호출 vs 모듈 함수 호출

메서드 호출에 접두사가 필요한지는 **메서드 소유자**에 따라 결정됩니다.

- **내장 타입의 메서드**(`str.starts-with`, `i64.to-str` 등) — 접두사 불필요
- **구조체 인스턴스의 메서드**(`f.read`, `p.exists` 등) — 접두사 불필요
- **모듈 수준 함수**(`fs.open`, `sha256.sha256` 등) — **접두사 필요**

`fs.fil()`에서 `fs`는 모듈의 ShortName이고 `fil`은 모듈 수준 함수 이름입니다. `fs.` 접두사는 생략할 수 없습니다. 여기서 `fs`는 변수 이름이 아니라 모듈 경로이기 때문입니다.

### `Name.Function`의 두 가지 의미

`process.cmd(...)`와 `p.start(...)`는 모두 `xxx.yyy()` 형태로 쓰이지만 의미는 완전히 다릅니다.

| 표기 | `xxx` | `yyy` | 의미 |
| --- | --- | --- | --- |
| `process.cmd(...)` | 모듈 ShortName | 모듈 수준 함수 | `xxx`는 모듈 경로이고, `yyy`는 해당 모듈이 정의한 독립 함수 |
| `p.start(...)` | 인스턴스 변수 | 구조체 메서드 | `xxx`는 `process` 타입의 변수이고, `yyy`는 `process.start = ...`로 정의된 메서드 |

**정의할 때의 차이**:
- 모듈 수준 함수는 정의할 때 **접두사를 붙이지 않습니다**: 모듈 내부에서 `cmd = (program str, ...) { }`를 직접 작성합니다.
- 구조체 메서드는 정의할 때 **반드시 `struct.` 접두사를 붙여야 합니다: `process.start = (program str, ...) { }`

**호출할 때의 차이**:
- 모듈 수준 함수는 외부에서 호출할 때 `ModuleName.function()` 형식을 사용합니다: `process.cmd(...)`
- 구조체 메서드는 인스턴스를 통해 호출합니다: `p = process.new()` → `p.start(...)`

> **참고**: 모듈 내부에서도 같은 모듈의 모듈 수준 함수를 호출할 때는 접두사를 붙이지 않으며(`cmd(...)`), 구조체 메서드는 `self`를 통해 암시적으로 또는 `.method()` 문법으로 호출합니다.

## 모듈 간 타입 참조

**다른 모듈**이 정의한 타입(구조체, 인터페이스, 열거형 등)을 참조할 때는 반드시 `ShortName.` 접두사를 사용해야 합니다. 다음에 적용됩니다.

### 구조체가 인터페이스를 구현할 때

구조체가 다른 모듈이 정의한 인터페이스를 구현할 때는 인터페이스 이름에 모듈 접두사를 붙여야 합니다.

```no
#{fmt-skip}
; ❌ 錯誤：db、rows、stmt 是 sql 模組定義的介面，不能省略前綴
db-mysql db {
    fd i64
}
```

```no
; ✅ 正確：使用 sql.db、sql.rows、sql.stmt
db-mysql sql.db {
    fd i64
}

rows-mysql sql.rows {
    fd i64
}

stmt-mysql sql.stmt {
    fd i64
}
```

### 함수 매개변수와 반환 타입

함수 시그니처의 모듈 간 타입에도 접두사가 필요합니다.

```no
; ✅ 正確：返回型別使用 sql.result
db-mysql.exec = (sql str) (r sql.result) {
    ...
}
```

### 구조체 필드 타입

```no
; ✅ 正確：欄位型別使用 sql.connection
conn-mysql sql.db {
    handle sql.connection
}
```

### 접두사가 필요 없는 경우

- **같은 모듈에서 정의한 타입**: 같은 `.no` 파일에 정의된 구조체, 인터페이스, 열거형은 타입 이름을 그대로 사용합니다.
- **내장 타입**: `str`, `i64`, `bool`, `byte` 등의 내장 타입은 접두사가 필요 없습니다.
- **내장 인터페이스**: `enter`, `leave` 등의 언어 내장 인터페이스는 접두사가 필요 없습니다.

```no
; 同檔案定義的型別，不需前綴
result {
    last-id i64
    affected i64
}

; enter/leave 是內置介面，不需前綴
; result 是同檔案定義的結構體，不需前綴
db {
    enter
    leave
    close() (ok bool)
    exec(sql str) (r result)
}
```

## 전체 예제

```no
; 標準庫模組自動載入，無需顯式導入

; ─── 不需前綴 ───

; 全局函數（宣告在 global.no）
s str = with-cap(256)
v []i64 = with-len(100)
print('hello {n}')
s = format('x={x}')

; 同檔案函數
sha256(data)

; 內置類型方法
'hello'.starts-with('he')
n.to-str()
v.push(42)

; 結構體實例方法
f = fs.open(path, opts)
f.read(buf, n)
f.close()

; ─── 需要前綴 ───

; 模組級函數
sha256.sha256(data)
sha256.sha256-hex(data)
fs.open(path, opts)
gzip.gzip-decompress(data)
math.pow(x, y)

; 模組常量
net.NET-BUF-SIZE
math.PI

; 跨模組型別引用（介面實作、參數型別、返回型別、欄位型別）
db-mysql sql.db {
    fd i64
}

r sql.result = d.exec('CREATE TABLE ...')
```
