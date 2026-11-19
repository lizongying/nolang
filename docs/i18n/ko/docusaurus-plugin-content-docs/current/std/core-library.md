---
sidebar_position: 3.2
---

## 핵심 라이브러리

### fmt — 형식화된 출력

Nolang은 **이름이 있는 형식 문자열** `{name[:spec]}`을 사용해 스코프의 변수를 직접 참조하므로 위치 인자가 필요 없습니다. 출력은 `io.out`/`io.err` 시스템 호출을 통해 이루어지며 libc `printf`에 의존하지 않습니다.

```no
print('x={x}'); 具名格式，自動換行（stdout）
print('result={val}', 42, 'result={val}'); 多參數：空格分隔；每個字面量各自是模板
eprint('err {x}'); 具名格式，自動換行（stderr）
print('編號 {id:06} 金額 {money:.2f}'); 支援對齊/填充/寬度/精度
s = format('x={x}'); 返回格式化字串（替代 sprintf）
io.out('no-newline-here'); 底層命令，輸出不換行（stdout）
io.err('err-no-newline'); 底層命令，輸出不換行（stderr）
; printf/eprintf 已移除（呼叫即編譯錯誤 [printf-depr]）：printf→print/io.out、eprintf→eprint/io.err
; sprintf 仍可用但已廢棄：sprintf→format
; io.err 明確模組前綴，不會與 Option 構造函數 err() 衝突
```

### math — 수학 함수(함수 형태 API)

std 숫자 계열의 "함수 형태" API는 모두 math 모듈(std/math.no)에 모여 있고, "메서드 형태"(`*.to-str`, f64/f32 sqrt/sin/exp…, 정수 sqrt/is-prime)는 모두 number 모듈에 있습니다(아래 number 절 참고). 유니언 타입과 멤버 메서드: num → std/num.no, int → std/int.no, float → std/float.no.

**상수:** `math.PI`, `math.E`, `math.LN10`(ln 10, f64-to-str 구현과 함께 사용)

**비교값(num 제네릭 가변 인자 함수):** `math.max`, `math.min`(`a ..num`, 정수와 부동소수점이 같은 단형화 경로를 따름)

**거듭제곱/근(함수):** `math.pow`(f64 거듭제곱 a^b, 순수 .no 구현: `exp(y*ln|x|)` ＋ 음수 밑의 부호 처리(정수 지수는 홀수면 음수, 짝수면 양수, 정수가 아닌 지수는 NaN 반환), **libm에 의존하지 않음**), `math.hypot`(sqrt(x*x+y*y))

**기타 함수:** `math.atan2`, `math.fmod`(부동소수점 나머지), `math.clamp`(i64 포화 클램프)

**정수 수학(int 제네릭 함수):** `math.even`, `math.odd`, `math.gcd`, `math.lcm`, `math.div`(몫), `math.mod`(나머지)

**타입 변환(예전 함수 형태 `math.i64-to-f64` / `math.f64-to-i64` / `math.f32-to-f64` / `math.f64-to-f32`는 제거되고 메서드 형태로 대체됨):** `v.to-f64()`, `v.to-i64()`, `f32.to-f64()` / `f64.to-f32()`(number 모듈; 모든 정수 너비에 `.to-f64()`가 있고 i128 / u128도 `.to-f64()`를 제공)

**숫자-문자열 변환 하위 계층:** `math.char-to-str`만 남았습니다(사용 중단, `c.to-str()` 사용 권장). `math.i64-to-str` / `math.u64-to-str` / `math.f64-to-str`는 제거되었으니 메서드 형태 `v.to-str()`를 사용하세요.

**비트 연산(함수):** `math.swap`, `math.arr-zero`, `math.rotate-left`, `math.rotate-right`, `math.store-le-u32`

> 정수 거듭제곱: `math.pow-int`(int 제네릭 고속 거듭제곱, f64의 `math.pow`와 다른 모듈로 분리됨). `number.pow`는 기존 단언을 만족하도록 명확한 i64 시그니처를 유지합니다.

### number — 수치 연산(메서드 형태 API)

std 숫자 계열의 "메서드 형태" API는 number 모듈(std/number.no)에 모여 있고, 함수 형태 API는 위의 math 절에 있습니다. 유니언 타입 int / float / num은 각 유니언 절을 참고하세요.

**범위 상수:** i8.MIN / MAX … i128.MIN / MAX, u8.MIN / MAX … u128.MAX; `number.INF`

**정수 거듭제곱(함수):** `r = number.pow(base, n)`(base^n, n ≥ 0, 고속 거듭제곱 O(log n), i64 시그니처, 오버플로 시 랩어라운드; int 제네릭 버전은 위의 `math.pow-int` 참고)

**정수 메서드:** `n.sqrt()`(정수 제곱근 floor(√n), 음수는 0 반환), `n.is-prime()`(소수 판별) — i8 / i16 / i32 / i64 / u8 / u16 / u32 / u64 각 너비 버전을 제공하며 i128 / u128 버전도 제공합니다(128비트 산술은 LLVM i128 경로를 사용해 가능함; `n i128 = .`처럼 지역 변수에 타입을 명시하고 부호성에 맞게 `#{overflow=wrap}` 애노테이션을 올바르게 배치해야 함).

**구체 타입 to-str(메서드):** `v.to-str()` — i8 / i16 / i32 / i64 / u8 / u16 / u32 / u64 / byte / f32 / f64 각 너비; i128 / u128도 지원(네이티브 `@str_from_i128` / `@str_from_u128` 경유)

**부동소수점 메서드(f64 및 동일 이름의 f32):** 삼각 `x.sin()`, `x.cos()`, `x.tan()`, `x.asin()`, `x.acos()`, `x.atan()`; 쌍곡선 `x.sinh()`, `x.cosh()`, `x.tanh()`; 정수화 `x.ceil()`, `x.floor()`, `x.round()`, `x.trunc()`; 지수/로그 `x.exp()`, `x.log()`, `x.log10()`, `x.log2()`; 거듭제곱/근 `x.sqrt()`, `x.cbrt()`; 도/라디안 `x.degrees()`, `x.radians()`. f32는 내부에서 double 정밀도로 계산한 뒤 f32로 좁힙니다.

### char — 문자 연산

char는 본질적으로 i32(Unicode 코드 포인트)이며, 모든 연산은 메서드 형태로 제공됩니다:

```no
c char = 'A'
c.is-digit(); 是否為數字 (0-9)（方法）
c.is-letter(); 是否為字母 (a-z, A-Z)（方法）
c.is-alpha(); is-letter 別名（方法）
c.is-alnum(); 是否為字母或數字（方法）
c.is-space(); 是否為空白字元（方法）
c.is-upper(); 是否為大寫字母（方法）
c.is-lower(); 是否為小寫字母（方法）
c.to-upper(); 轉大寫（ASCII）（方法）
c.to-lower(); 轉小寫（ASCII）（方法）
c.to-bytes(); Unicode → UTF-8 位元組（方法）
c.to-str(); Unicode → 字串（UTF-8，方法）
```

### str — 문자열 연산

`str` 디스크립터는 `{ u32 len, u32 cap, i8* data }`(64비트에서 16바이트)이지만,
`s.len()` / `s.len-bytes()` / `s.cap()` 및 `with-cap` / `with-len` / `with-cap-len`
의 매개변수와 반환값은 **모두 여전히 `i64`**입니다. u32 ↔ i64 변환은 디스크립터 경계에서 컴파일러가 처리합니다.
자세한 내용은 [문자열: 내부 레이아웃과 외부 API](../lang/str.md)를 참고하세요. `txt`는 이 변경의 영향을 받지 않습니다.

슬라이스 `[]t`의 디스크립터도 동일한 구조입니다. `%vec = { u32 len, u32 cap, i64 data }`(16바이트, `data`는
`ptrtoint`로 인코딩된 포인터)이며 `v.len()`도 여전히 `i64`입니다. `[n]t`에는 런타임 디스크립터가 없습니다(요소가
`[n x t]` 안에 인라인되어 있고 `.len`은 컴파일 타임 상수).

```no
#{fmt-skip}
ok = a.eq(b, n)               ; 相等比較（方法）
dst = s.copy()                ; 字串複製（方法）
s.fill(val byte)              ; 填充 byte 值（方法）
pos = s.index(sub)            ; 子字串位置
ok = s.contains(sub)          ; 是否包含
ok = s.starts-with(sub)       ; 前綴判斷
ok = s.ends-with(sub)         ; 後綴判斷
s.to-upper()                  ; 轉大寫
s.to-lower()                  ; 轉小寫
out = s.trim()                ; 去首尾空白
out = s.repeat(n)             ; 重複
out = s.slice(start, end)     ; 切片（下標為碼點位置）
b = s.to-bytes()              ; 轉 []byte
s = b.to-str()                ; []byte 轉 str（方法）
v = s.to-i64()                ; 字串轉 i64（回傳 ?i64）
v = s.to-i8()                 ; 字串轉 i8（回傳 ?i8）
v = s.to-i16()                ; 字串轉 i16（回傳 ?i16）
v = s.to-i32()                ; 字串轉 i32（回傳 ?i32）
v = s.to-u8()                 ; 字串轉 u8（回傳 ?u8）
v = s.to-u16()                ; 字串轉 u16（回傳 ?u16）
v = s.to-u32()                ; 字串轉 u32（回傳 ?u32）
v = s.to-u64()                ; 字串轉 u64（回傳 ?u64）
v = s.to-byte()               ; 字串轉 byte（回傳 ?byte）
v = s.to-f64()                ; 字串轉 f64（回傳 ?f64）
v = s.to-bool()               ; 字串 "true"/"false" 轉 bool（回傳 ?bool）
s = v.to-str()                ; i64 轉字串（方法）
out = s.reverse()             ; 反轉
c = s.compare(b)              ; 字典序比較
n = s.count()                 ; code point 總數
val = s.replace-char(old, new) ; 取代字元（返回結果字串）
out = s.trim-char(c)          ; 去指定字元
ok = s.empty()                ; 是否為空
s.clear()                     ; 清空（len=0，原地修改）
s = with-cap(cap)            ; 內建語法：建立指定容量的新字串（len=0）
s = with-len(len)            ; 內建語法：建立指定長度的字串（len=cap）
s = with-cap-len(cap, len)   ; 內建語法：建立指定容量和長度的字串
parts = s.split(sep)          ; 用分隔符分割（返回 []str，方法）
out = ss.join(sep)            ; []str 用分隔符連接（方法）
```

### num — num 유니언 타입과 num 메서드

`num = int | float`(std/num.no). num 타입의 메서드는 num 모듈에 있습니다(제네릭 함수 max/min은 위의 math 참고):

```no
r = num.abs(); 絕對值（方法）
r = num.clamp(lo, hi); 限制範圍（方法）
r = num.sign(); 正負號（-1/0/1，方法）
```

### int — int 유니언 타입과 int 메서드

`int = i8 | i16 | i32 | i64 | i128 | u8 | u16 | u32 | u64 | u128`(std/int.no). int 타입의 메서드는 int 모듈에 있습니다:

```no
s = v.to-str(); 整數轉字串（方法）
q = v.div(b); 除法取商（回傳 option）
r = v.mod(b); 取模（回傳 option）
```

### float — float 유니언 타입과 float 메서드

`float = f32 | f64`(std/float.no). float 타입의 메서드는 float 모듈에 있습니다:

```no
q = v.div(b); 浮點除法
yes = v.is-nan(); NaN 判斷（方法）
yes = v.is-inf(); Inf 判斷（方法）
```

(float.to-str 유니언 메서드는 정의하지 않습니다. f64/f32 변수는 구체 타입 메서드 `v.to-str()`를 직접 사용합니다.)

### number — 메서드 사용 예

(전체 API는 위의 "number — 수치 연산(메서드 형태 API)" 참고; 함수 형태는 "math" 절 참고.)

```no
; 方法形式（number 模組）
s = v.to-str(); 整數/浮點轉字串（各寬度）
r = n.sqrt(); 整數平方根 floor(√n)
yes = n.is-prime(); 質數判斷（int/u8…）
q = x.sqrt(); 浮點平方根（f64/f32 方法）
q = x.floor(); 取整（f64/f32 方法）
r = number.pow(a, n); 整數冪（i64 簽名；int 泛型版為 math.pow-int）

; 函數形式（math 模組）
m = math.max(1, 2, 3); num 泛型變參
g = math.gcd(a, b); 最大公因數
l = math.lcm(a, b); 最小公倍數
pi = math.pow-int(a, n); 整數冪（int 泛型）
sw = math.swap(a, b); 交換

; 數值轉換（方法形式，舊 math.*-to-* 函數已移除）
f = v.to-f64(); 整數 → f64
n = x.to-i64(); f64 → 整數（截斷）
w = f32v.to-f64(); f32 → f64；反向 f64.to-f32()

; 範圍常數（number 模組）
i8.MIN / MAX; -128 / 127
i16.MIN / MAX; -32768 / 32767
i32.MIN / MAX; -2147483648 / 2147483647
i64.MIN / MAX; -2^63 / 2^63-1
i128.MIN / MAX; -2^127 / 2^127-1
u8.MIN / MAX; 0 / 255
u16.MIN / MAX; 0 / 65535
u32.MIN / MAX; 0 / 4294967295
u64.MIN / MAX; 0 / 2^64-1
u128.MIN / MAX; 0 / 2^128-1
number.INF; 浮點無窮大
```

### byte — 바이트 연산

```no
#{fmt-skip}
out = i64.to-bytes-be()         ; i64 → big-endian [8]byte
out = i64.to-bytes-le()         ; i64 → little-endian [8]byte
v = []byte.to-i64-be()          ; big-endian []byte → i64（1~8 位元組）
v = []byte.to-i64-le()          ; little-endian []byte → i64（1~8 位元組）
s = []byte.to-str()             ; []byte 轉 str（方法）
s = []byte.to-hex()             ; []byte → 大寫十六進制字串
s = []byte.to-hex-lower()       ; []byte → 小寫十六進制字串
s = byte.to-str()               ; byte 轉 str（方法）
```

### txt — 고정 길이 텍스트 타입

`txt`는 고정 256바이트 텍스트 타입으로, 짧은 텍스트에 적합합니다:
- 앞 255바이트에 데이터 저장(`data [255]byte`)
- 마지막 1바이트에 길이 저장(`len byte`, 단위는 "바이트", 범위 0-255)
- 힙 할당이 필요 없고 모두 스택에 위치
- 타입 표기가 필수

길이 의미(`str`과 맞춤):
- `t.len()` → 문자(code point) 개수(`t.count()`와 동일)
- `t.len-bytes()` → 바이트 개수(내부 UTF-8 버퍼의 실제 길이)
- 인덱스 / 슬라이스 / 추가 등 바이트 연산은 `len-bytes()`를 기준으로 합니다. ASCII에서는 둘이 같고, 멀티바이트 UTF-8이 포함되면 `len() < len-bytes()`입니다.

```no
#{fmt-skip}
t txt = 'hello'              ; 必須類型標註
n = t.len()                  ; 字元（code point）數量（i64）
b = t.len-bytes()            ; 字節數量（i64）
c = t[0]                     ; 索引存取（byte）
ok = t.eq(b txt)             ; 相等比較
dst = t.copy()               ; 複製
s = t.to-str()              ; 轉 str
out = t.to-bytes()           ; 轉 []byte
pos = t.index(sub txt)       ; 查找子串
ok = t.contains(sub txt)     ; 是否包含
ok = t.starts-with(sub txt)  ; 前綴檢查
ok = t.ends-with(sub txt)    ; 後綴檢查
t.push(c char)              ; 追加一個碼點（整字元裝不下 255 上限則整條忽略）
t.push-str(s str)            ; 追加 str（按位元組截斷至 255）
t.push-txt(t2 txt)           ; 追加 txt（按位元組截斷至 255）
out = t.reverse()            ; 反轉
out = t.slice(start, end)   ; 截取 [start, end)
r = t.compare(b txt)        ; 字典序比較 (-1/0/1)
```

### vec — 슬라이스 연산

```no
#{fmt-skip}
v = vec.vec-create(n, val)         ; 建立長度 n 的切片，全部填充 val
ok = []t.eq(a, b, n)           ; 相等比較
n = []t.len()                  ; 長度
[]t.push(val)                   ; 追加（自動擴容）
[]t.clear()                     ; 清空（len=0，cap/data 不變）
v = with-cap(cap)             ; 內建語法：建立指定容量的新切片（len=0）
v = with-len(len)             ; 內建語法：建立指定長度的切片（len=cap）
v = with-cap-len(cap, len)    ; 內建語法：建立指定容量和長度的切片
val, new-n = []t.pop()         ; 彈出
found = []t.contains(n, val)   ; 是否包含（n 為長度）
[]t.reverse(n)                  ; 反轉前 n 個元素
[]t.clone(dst)                  ; 複製到 dst
[]t.fill(n, val)                ; 前 n 個元素填充
arr = []t.to-arr()             ; 轉陣列
[]t.sort-asc()                  ; 升序排序（方法）
[]t.sort-desc()                 ; 降序排序（方法）
```

### arr — 배열 연산

```no
out = [n]
t.clone(); 複製
ok = [n]
t.eq(b); 相等比較
[n]
t.fill(val); 填充
[n]
t.reverse(); 反轉
ok = [n]
t.contains(val); 是否包含
v = [n]
t.to-vec(); 轉切片
v = [n]
t.max(); 最大值
v = [n]
t.min(); 最小值
v = [n]
t.sum(); 總和
i = [n]
t.index-of(val); 索引
v = [n]
t.last(); 最後元素
v = [n]
t.first(); 首元素
[n]
t.sort-asc(); 升序排序
[n]
t.sort-desc(); 降序排序
```

### sort — 정렬 상수

```no
sort.ast; 升序
sort.desc; 降序
```

---
