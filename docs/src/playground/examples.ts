/**
 * Playground example snippets.
 *
 * Each example is a self-contained Nolang program intended to run under the
 * Direct-WASM backend in the browser playground. scripts/playground_demos.ts
 * imports this file as the single source of truth and compiles every snippet
 * through the exact same "no build --wasm-direct" path the browser uses, so a
 * demo that does not compile (or does not print its expect string) fails CI.
 *
 * Constraints (per Playground spec):
 *   - No FFI (browser cannot link native libraries)
 *   - No fork/exec/pipe (WASI preview1 limitation)
 *   - No network sockets (WASI preview1 limitation)
 *   - File system is virtual (stdin/stdout only)
 *
 * Template-literal trap: every code value lives inside backtick-delimited
 * template literals. A backtick or dollar-brace sequence inside a snippet
 * would terminate or interpolate the string, so both must be escaped. The
 * validation script asserts that this file contains exactly two unescaped
 * backticks per example.
 *
 * Syntax reference: docs/docs/lang/syntax.md
 */
import type {CategoryId, ExampleI18n, Locale} from './i18n';

export type {CategoryId} from './i18n';

export interface Example {
  id: string;
  category: CategoryId;
  i18n: ExampleI18n;
  code: string;
  /** Stable stdout substring asserted by the CI demo gate. */
  expect: string;
}

export interface Category {
  id: CategoryId;
  i18n: Record<Locale, {label: string; description: string}>;
}

/** Order in which categories appear in the Examples dropdown. */
export const CATEGORY_ORDER: Category[] = [
  {
    id: 'basics',
    i18n: {
      'zh-Hant': {label: '基礎', description: '從第一行程序開始：變量、函數與輸出。'},
      'zh-Hans': {label: '基础', description: '从第一行程序开始：变量、函数与输出。'},
      en: {label: 'Basics', description: 'Start here: variables, functions and output.'},
      ja: {label: '基礎', description: '最初のプログラムから：変数、関数、出力。'},
      ko: {label: '기초', description: '첫 번째 프로그램부터 시작합니다: 변수, 함수, 출력.'},
    },
  },
  {
    id: 'control',
    i18n: {
      'zh-Hant': {label: '流程控制', description: '條件、模式匹配與各種循環寫法。'},
      'zh-Hans': {label: '流程控制', description: '条件、模式匹配与各种循环写法。'},
      en: {label: 'Control flow', description: 'Guards, pattern matching and every loop form.'},
      ja: {label: '制御フロー', description: '条件、パターンマッチ、さまざまなループの書き方。'},
      ko: {label: '제어 흐름', description: '조건, 패턴 매칭, 다양한 반복문 작성법.'},
    },
  },
  {
    id: 'data',
    i18n: {
      'zh-Hant': {label: '數據結構', description: '數組、切片、結構體與方法。'},
      'zh-Hans': {label: '数据结构', description: '数组、切片、结构体与方法。'},
      en: {label: 'Data structures', description: 'Arrays, slices, structs and methods.'},
      ja: {label: 'データ構造', description: '配列、スライス、構造体とメソッド。'},
      ko: {label: '데이터 구조', description: '배열, 슬라이스, 구조체, 메서드.'},
    },
  },
  {
    id: 'types',
    i18n: {
      'zh-Hant': {label: '標量類型', description: 'char、bool、f64 與 byte 的行爲。'},
      'zh-Hans': {label: '标量类型', description: 'char、bool、f64 与 byte 的行为。'},
      en: {label: 'Scalar types', description: 'How char, bool, f64 and byte behave.'},
      ja: {label: 'スカラー型', description: 'char、bool、f64、byte の振る舞い。'},
      ko: {label: '스칼라 타입', description: 'char, bool, f64, byte의 동작 방식.'},
    },
  },
  {
    id: 'std',
    i18n: {
      'zh-Hant': {label: '標準庫', description: 'math 與 rand 模塊的常用函數。'},
      'zh-Hans': {label: '标准库', description: 'math 与 rand 模块的常用函数。'},
      en: {label: 'Standard library', description: 'Everyday math and rand module functions.'},
      ja: {label: '標準ライブラリ', description: 'math と rand モジュールの一般的な関数。'},
      ko: {label: '표준 라이브러리', description: 'math 및 rand 모듈의 주요 함수.'},
    },
  },
  {
    id: 'errors',
    i18n: {
      'zh-Hant': {label: '錯誤處理', description: '用 option 值表達可能失敗的計算。'},
      'zh-Hans': {label: '错误处理', description: '用 option 值表达可能失败的计算。'},
      en: {label: 'Error handling', description: 'Model fallible computations with option values.'},
      ja: {label: 'エラー処理', description: 'option 値で失敗する可能性のある計算を表す。'},
      ko: {label: '오류 처리', description: 'option 값으로 실패할 수 있는 계산을 표현합니다.'},
    },
  },
];

export const examples: Example[] = [
  // ---------------------------------------------------------------- basics --
  {
    id: 'hello',
    category: 'basics',
    i18n: {
      'zh-Hant': {label: 'Hello World', description: '頂層語句直接執行，無需 main 函數；並演示命名格式化字符串與浮點精度。'},
      'zh-Hans': {label: 'Hello World', description: '顶层语句直接执行，无需 main 函数；并演示命名格式化字符串与浮点精度。'},
      en: {label: 'Hello World', description: 'Top-level statements run without a main function; also shows named format strings and float precision.'},
      ja: {label: 'Hello World', description: 'トップレベルの文は main 関数なしで直接実行されます。名前付きフォーマット文字列と浮動小数点数の精度も紹介します。'},
      ko: {label: 'Hello World', description: '최상위 문은 main 함수 없이 바로 실행됩니다. 이름이 지정된 포맷 문자열과 부동소수점 정밀도도 보여줍니다.'},
    },
    expect: 'Hello, Nolang!',
    code: `; Hello, World — top-level statements execute directly; no main function.
print('Hello, World!')

; Named format strings reference variables in scope: {name}
name = 'Nolang'
print('Hello, {name}!')

; Float literals infer f64; ':.<n>f' controls precision.
pi = 3.14
print('pi = {pi:.2f}')
`,
  },
  {
    id: 'variables',
    category: 'basics',
    i18n: {
      'zh-Hant': {label: '變量與類型推斷', description: '類型推斷、顯式類型標註，以及用逗號左側綁定多返回值。'},
      'zh-Hans': {label: '变量与类型推断', description: '类型推断、显式类型标注，以及用逗号左侧绑定多返回值。'},
      en: {label: 'Variables & inference', description: 'Type inference, explicit annotations, and binding multiple results with a comma-separated LHS.'},
      ja: {label: '変数と型推論', description: '型推論、明示的な型注釈、カンマ左辺による複数戻り値のバインド。'},
      ko: {label: '변수와 타입 추론', description: '타입 추론, 명시적 타입 표기, 쉼표 왼쪽에 여러 반환값을 바인딩하는 방법.'},
    },
    expect: 'swap(5, 9) = 9, 5',
    code: `; Type inference, explicit annotations and multiple results.
i = 42
f = 3.14
s = 'hello'
flag = true
n u64 = 100
arr [3]i64 = [1, 2, 3]
v []u8 = [10, 20, 30]

print('i={i} n={n} flag={flag}')
print('f={f:.2f} s={s}')

; A function may return several values, bound with a comma-separated LHS.
swap = (a i64, b i64) (x i64, y i64) {
    x = b
    y = a
}
a, b = swap(5, 9)
print('swap(5, 9) = {a}, {b}')
`,
  },
  {
    id: 'functions',
    category: 'basics',
    i18n: {
      'zh-Hant': {label: '函數與遞歸', description: '參數、具名結果參數、遞歸調用與多返回值函數。'},
      'zh-Hans': {label: '函数与递归', description: '参数、具名结果参数、递归调用与多返回值函数。'},
      en: {label: 'Functions & recursion', description: 'Parameters, named results, recursive calls and multi-result functions.'},
      ja: {label: '関数と再帰', description: '引数、名前付き結果引数、再帰呼び出し、複数戻り値の関数。'},
      ko: {label: '함수와 재귀', description: '매개변수, 이름이 있는 결과 매개변수, 재귀 호출, 여러 반환값 함수.'},
    },
    expect: '5! = 120',
    code: `; Functions — parameters, named results, recursion, multiple results.
; Integer arithmetic can overflow, so + yields ?i64. A ?T result parameter lets
; ?= unwrap it and re-raise the failure instead of silently dropping it.
add = (a i64, b i64) (r ?i64) {
    r ?= a + b
}
s = add(3, 4)
print('3 + 4 = {s}')

fact = (n i64) (r ?i64) {
    n < 2 -> {
        r = 1
        return
    }
    p ?= fact(n - 1)
    r ?= n * p
}
f5 = fact(5)
print('5! = {f5}')

minmax = (a i64, b i64) (lo i64, hi i64) {
    a < b -> {
        lo = a
        hi = b
        return
    }
    lo = b
    hi = a
}
lo, hi = minmax(9, 2)
print('min={lo} max={hi}')
`,
  },
  {
    id: 'fib',
    category: 'basics',
    i18n: {
      'zh-Hant': {label: '斐波那契', description: '同一函數的迭代版與遞歸版，對比兩種寫法。'},
      'zh-Hans': {label: '斐波那契', description: '同一函数的迭代版与递归版，对比两种写法。'},
      en: {label: 'Fibonacci', description: 'Iterative and recursive implementations of the same function side by side.'},
      ja: {label: 'フィボナッチ', description: '同じ関数の反復版と再帰版を比較し、2 つの書き方を紹介します。'},
      ko: {label: '피보나치', description: '같은 함수의 반복 버전과 재귀 버전을 비교합니다.'},
    },
    expect: 'fib-rec(10) = 55',
    code: `; Fibonacci — iterative and recursive implementations.
fib-loop = (n i64) (r ?i64) {
    a i64 = 0
    b i64 = 1
    i <- [0..n): {
        t ?= a + b
        a = b
        b = t
    }
    r = a
}

fib-rec = (n i64) (r ?i64) {
    n < 2 -> {
        r = n
        return
    }
    p1 ?= fib-rec(n - 1)
    p2 ?= fib-rec(n - 2)
    r ?= p1 + p2
}

loop10 = fib-loop(10)
print('fib-loop(10) = {loop10}')
rec10 = fib-rec(10)
print('fib-rec(10) = {rec10}')
`,
  },
  {
    id: 'arithmetic',
    category: 'basics',
    i18n: {
      'zh-Hant': {label: '算術與位運算', description: '整數五則運算與按位與；結果可直接在格式字符串中引用。'},
      'zh-Hans': {label: '算术与位运算', description: '整数五则运算与按位与；结果可直接在格式字符串中引用。'},
      en: {label: 'Arithmetic & bitwise', description: 'Integer arithmetic and bitwise AND; results can be referenced directly in format strings.'},
      ja: {label: '算術演算とビット演算', description: '整数の四則演算とビット単位の AND。結果はフォーマット文字列で直接参照できます。'},
      ko: {label: '산술 및 비트 연산', description: '정수의 사칙연산과 나머지 연산, 비트 AND 연산. 결과를 포맷 문자열에서 바로 참조할 수 있습니다.'},
    },
    expect: '12 & 10 = 8',
    code: `; Integer operators and bitwise operations.
a = 17
b = 5
sum = a + b
diff = a - b
prod = a * b
quot = a / b
rem = a % b
print('{a} + {b} = {sum}')
print('{a} - {b} = {diff}')
print('{a} * {b} = {prod}')
print('{a} / {b} = {quot}')
print('{a} % {b} = {rem}')

x = 12
y = 10
band = x & y
print('12 & 10 = {band}')
`,
  },
  {
    id: 'format',
    category: 'basics',
    i18n: {
      'zh-Hant': {label: '格式化輸出', description: '{name} 命名插值與 {name:spec} 精度控制；無字段的字符串原樣輸出。'},
      'zh-Hans': {label: '格式化输出', description: '{name} 命名插值与 {name:spec} 精度控制；无字段的字符串原样输出。'},
      en: {label: 'Formatting', description: '{name} interpolation and {name:spec} precision; a literal without fields prints verbatim.'},
      ja: {label: 'フォーマット出力', description: '{name} による名前付き補間と {name:spec} による精度制御。フィールドのない文字列はそのまま出力されます。'},
      ko: {label: '포맷 출력', description: '{name} 이름 보간과 {name:spec} 정밀도 제어. 필드가 없는 문자열은 그대로 출력됩니다.'},
    },
    expect: 'Nolang / 2026 / 0.75',
    code: `; Named formatting — {name} and {name:spec}.
lang = 'Nolang'
year = 2026
ratio = 0.75
print('{lang} / {year} / {ratio:.2f}')
print('mix: {lang} and {year}')

; A string literal with no fields prints verbatim.
print('plain text, no fields')
`,
  },
  {
    id: 'strings',
    category: 'basics',
    i18n: {
      'zh-Hant': {label: '字符串', description: "用 '-' 拼接字符串、len() 取長度、to-str() 轉換標量，以及按下標取字節。 "},
      'zh-Hans': {label: '字符串', description: "用 '-' 拼接字符串、len() 取长度、to-str() 转换标量，以及按下标取字节。 "},
      en: {label: 'Strings', description: "Concatenate with '-', measure with len(), convert with to-str(), and index a byte."},
      ja: {label: '文字列', description: "'-' で文字列を連結し、len() で長さを取得し、to-str() でスカラー値を変換し、添字でバイトを取り出します。 "},
      ko: {label: '문자열', description: "'-'로 문자열을 이어 붙이고, len()으로 길이를 구하고, to-str()로 스칼라를 변환하며, 인덱스로 바이트를 가져옵니다."},
    },
    expect: 'Hello, Nolang!',
    code: `; Strings — '-' concatenation, len(), to-str() and indexing.
first = 'Hello, '
last = 'Nolang'
greeting = first - last
print(greeting)
n = len(greeting)
print('len(greeting) = {n}')

; str values can be printed directly or embedded in a format string.
print(first)
print('{first}{last}!')

; Scalar to string conversion.
num = 42
ns = num.to-str()
print('42 as str = {ns}')

; Indexing a string returns the code point at that byte.
s = 'ABC'
c = s[0]
print('s[0] = {c}')
`,
  },

  // --------------------------------------------------------------- control --
  {
    id: 'guards',
    category: 'control',
    i18n: {
      'zh-Hant': {label: '短路守衛', description: 'cond -> action 形式的短路條件語句，條件成立時才執行右側動作。'},
      'zh-Hans': {label: '短路守卫', description: 'cond -> action 形式的短路条件语句，条件成立时才执行右侧动作。'},
      en: {label: 'Short-circuit guards', description: 'The cond -> action form; the action runs only when the condition holds.'},
      ja: {label: '短絡ガード', description: 'cond -> action 形式の短絡条件文。条件が成り立つ場合のみ右辺のアクションが実行されます。'},
      ko: {label: '단락 가드', description: 'cond -> action 형태의 단락 조건문으로, 조건이 참일 때만 오른쪽 동작이 실행됩니다.'},
    },
    expect: 'condition passed',
    code: `; Short-circuit guards: condition -> action.
x = 7
x > 5 -> print('x is big')

y = 3
y < 5 -> print('y is small')

ok = true
ok -> print('condition passed')
print('done')
`,
  },
  {
    id: 'match',
    category: 'control',
    i18n: {
      'zh-Hant': {label: '模式匹配', description: 'subject: { ... } 形式的值匹配：區間模式、開區間與 || 多模式。'},
      'zh-Hans': {label: '模式匹配', description: 'subject: { ... } 形式的值匹配：区间模式、开区间与 || 多模式。'},
      en: {label: 'Match expression', description: 'subject: { ... } matching with interval patterns, open bounds and || alternatives.'},
      ja: {label: 'パターンマッチ', description: 'subject: { ... } 形式の値マッチ：範囲パターン、開区間、|| による複数パターン。'},
      ko: {label: '패턴 매칭', description: 'subject: { ... } 형태의 값 매칭: 범위 패턴, 열린 구간, || 다중 패턴.'},
    },
    expect: 'Score 85 -> grade B',
    code: `; Value / range matching with 'subject: { ... }'.
score = 85
grade = score: {
    [0..60) -> 'F'
    [60..80) -> 'C'
    [80..90) -> 'B'
    [90..100] -> 'A'
    -> 'invalid'
}
print('Score {score} -> grade {grade}')

v = 85
result = v: {
    [..0) -> 'negative'
    [0..60) -> 'fail'
    [60..80) -> 'pass'
    [80..90) -> 'good'
    [90..100] -> 'excellent'
    (100..) -> 'extraordinary'
}
print('classify({v}) = {result}')

; Multiple patterns joined by ||.
m = 3
m: {
    1 || 3 || 5 || 7 -> print('odd small')
    2 || 4 || 6 -> print('even small')
    -> print('larger number')
}
`,
  },
  {
    id: 'fizzbuzz',
    category: 'control',
    i18n: {
      'zh-Hant': {label: 'FizzBuzz', description: '在範圍循環裏用塊匹配替代鏈式 if，展示模式分支的寫法。'},
      'zh-Hans': {label: 'FizzBuzz', description: '在范围循环里用块匹配替代链式 if，展示模式分支的写法。'},
      en: {label: 'FizzBuzz', description: 'Block-match patterns inside a range loop, as an alternative to chained ifs.'},
      ja: {label: 'FizzBuzz', description: '範囲ループ内で連鎖した if の代わりにブロックマッチを使用し、パターン分岐の書き方を示します。'},
      ko: {label: 'FizzBuzz', description: '범위 반복문에서 연쇄 if 대신 블록 매칭을 사용해 패턴 분기를 보여줍니다.'},
    },
    expect: 'FizzBuzz',
    code: `; FizzBuzz — block-match patterns inside a range loop.
i <- [1..16): {
    i % 15: {
        0 -> print('FizzBuzz')
        3 || 6 || 9 || 12 -> print('Fizz')
        5 || 10 -> print('Buzz')
        -> print('{i}')
    }
}
`,
  },
  {
    id: 'range-loop',
    category: 'control',
    i18n: {
      'zh-Hant': {label: '區間循環', description: '四種有界區間寫法，以及必須用 break 終止的無界區間 [0..)。'},
      'zh-Hans': {label: '区间循环', description: '四种有界区间写法，以及必须用 break 终止的无界区间 [0..)。'},
      en: {label: 'Range loops', description: 'Four bounded interval forms plus the unbounded [0..) form, which needs a break.'},
      ja: {label: '範囲ループ', description: '4 種類の有界範囲の書き方と、break で終了させる必要のある無界範囲 [0..)。'},
      ko: {label: '구간 반복문', description: '네 가지 유한 구간 표현과 break로 종료해야 하는 무한 구간 [0..)을 보여줍니다.'},
    },
    expect: '[0..) until 3 :',
    code: `; Range iteration — four bounded interval forms.
print('[0..4)  :')
i <- [0..4): { print('  {i}') }

print('[1..3]  :')
j <- [1..3]: { print('  {j}') }

print('(1..4]  :')
k <- (1..4]: { print('  {k}') }

print('(1..4)  :')
m <- (1..4): { print('  {m}') }

; An unbounded range has no exit condition — break out of it explicitly.
print('[0..) until 3 :')
t <- [0..): {
    print('  {t}')
    t == 3 -> break
}
`,
  },
  {
    id: 'while-loop',
    category: 'control',
    i18n: {
      'zh-Hant': {label: '條件循環', description: '(cond) { ... } 形式的 while 循環，條件由循環體更新。'},
      'zh-Hans': {label: '条件循环', description: '(cond) { ... } 形式的 while 循环，条件由循环体更新。'},
      en: {label: 'Conditional while loop', description: 'The (cond) { ... } while form, where the body updates the condition.'},
      ja: {label: '条件ループ', description: '(cond) { ... } 形式の while ループ。条件はループ本体で更新されます。'},
      ko: {label: '조건 반복문', description: '(cond) { ... } 형태의 while 반복문으로, 반복문 본문에서 조건을 갱신합니다.'},
    },
    expect: 'n = 3',
    code: `; Conditional while-do loop: (cond) { ... }
; n is declared ?i64 so that n = n + 1 is an option-to-option copy (no silent unwrap).
n ?i64 = 0
keep = true
(keep) {
    n = n + 1
    n >= 3 -> keep = false
}
print('n = {n}')
`,
  },
  {
    id: 'break-continue',
    category: 'control',
    i18n: {
      'zh-Hant': {label: 'break / continue', description: 'continue 跳到本輪末尾（仍會執行遞增），break 直接離開循環。'},
      'zh-Hans': {label: 'break / continue', description: 'continue 跳到本轮末尾（仍会执行递增），break 直接离开循环。'},
      en: {label: 'break / continue', description: 'continue jumps to the end of the iteration (the increment still runs); break leaves the loop.'},
      ja: {label: 'break / continue', description: 'continue は現在の繰り返しの末尾へジャンプします（インクリメントは実行されます）。break はループから直接抜けます。'},
      ko: {label: 'break / continue', description: 'continue는 현재 반복의 끝으로 이동하며(증가 연산은 계속 실행), break는 반복문을 바로 빠져나갑니다.'},
    },
    expect: 'i=5',
    code: `; break / continue inside a range loop.
i <- [0..8): {
    i == 2 -> continue
    i == 6 -> break
    print('i={i}')
}
print('done')
`,
  },

  // ------------------------------------------------------------------- data --
  {
    id: 'arrays',
    category: 'data',
    i18n: {
      'zh-Hant': {label: '定長數組', description: '[3]i64 定長數組的字面量、下標讀取與下標寫入。'},
      'zh-Hans': {label: '定长数组', description: '[3]i64 定长数组的字面量、下标读取与下标写入。'},
      en: {label: 'Fixed arrays', description: 'A [3]i64 literal with indexed reads and an indexed write.'},
      ja: {label: '固定長配列', description: '[3]i64 固定長配列のリテラル、添字による読み取りと書き込み。'},
      ko: {label: '고정 길이 배열', description: '[3]i64 고정 길이 배열의 리터럴, 인덱스 읽기, 인덱스 쓰기.'},
    },
    expect: 'after a[1]=99 -> 99',
    code: `; Fixed-size arrays — literal, index read, index write.
a [3]i64 = [10, 20, 30]
x = a[0]
y = a[2]
print('a[0]={x} a[2]={y}')
a[1] = 99
z = a[1]
print('after a[1]=99 -> {z}')
`,
  },
  {
    id: 'slices',
    category: 'data',
    i18n: {
      'zh-Hant': {label: '動態切片', description: '[]T 切片、len/cap 屬性、下標賦值與 with-cap 預分配。'},
      'zh-Hans': {label: '动态切片', description: '[]T 切片、len/cap 属性、下标赋值与 with-cap 预分配。'},
      en: {label: 'Slices', description: '[]T literals, len/cap, index assignment and with-cap preallocation.'},
      ja: {label: '動的スライス', description: '[]T スライス、len/cap プロパティ、添字による代入、with-cap による事前確保。'},
      ko: {label: '동적 슬라이스', description: '[]T 슬라이스, len/cap 속성, 인덱스 할당, with-cap 사전 할당.'},
    },
    expect: 'cap(with-cap(4)) = 4',
    code: `; Slices — []T literal, len/cap, index assignment, with-cap.
v []i64 = [1, 2, 3]
n = len(v)
print('len(v) = {n}')
x = v[1]
print('v[1] = {x}')
v[1] = 20
x = v[1]
print('after write v[1] = {x}')

w []i64 = with-cap(4)
c = w.cap
print('cap(with-cap(4)) = {c}')
`,
  },
  {
    id: 'struct',
    category: 'data',
    i18n: {
      'zh-Hant': {label: '結構體', description: '結構體聲明、字面量構造，以及字段的讀寫。'},
      'zh-Hans': {label: '结构体', description: '结构体声明、字面量构造，以及字段的读写。'},
      en: {label: 'Structs', description: 'Struct declaration, literal construction and field reads/writes.'},
      ja: {label: '構造体', description: '構造体の宣言、リテラルによる構築、フィールドの読み書き。'},
      ko: {label: '구조체', description: '구조체 선언, 리터럴 생성, 필드 읽기와 쓰기.'},
    },
    expect: 'Renamed to Bob',
    code: `; Structs — declaration, literal, field read/write.
user {
    name str
    age i64
}
u = user{
    name: 'Alice'
    age: 30
}
n = u.name
print('Hello, I am {n}')
age = u.age
print('Age: {age}')

; A field target is still a value context: the ?i64 sum needs an explicit
; value match, falling back to the old age when the addition overflows.
u.age = u.age + 1: {
    ok -> it

    -> u.age
}
age = u.age
print('After birthday: {age}')

u.name = 'Bob'
n = u.name
print('Renamed to {n}')
`,
  },
  {
    id: 'methods',
    category: 'data',
    i18n: {
      'zh-Hant': {label: '方法', description: '把函數掛到結構體類型上；方法體內用 . 引用接收者。'},
      'zh-Hans': {label: '方法', description: '把函数挂到结构体类型上；方法体内用 . 引用接收者。'},
      en: {label: 'Methods', description: 'Attach functions to a struct type; . refers to the receiver inside the body.'},
      ja: {label: 'メソッド', description: '関数を構造体型に関連付けます。メソッド本体内では . でレシーバーを参照します。'},
      ko: {label: '메서드', description: '함수를 구조체 타입에 연결합니다. 메서드 본문에서 .로 수신자를 참조합니다.'},
    },
    expect: 'after 2 bumps = 43',
    code: `; Methods — attach functions to a struct; '.' is the receiver.
counter {
    n i64
}
; A field target is still a value context: a value match unwraps the ?i64 sum
; and falls back to the old value when the addition overflows.
counter.bump = () {
    .n = .n + 1: {
        ok -> it

        -> .n
    }
}
counter.read = () (v i64) {
    v = .n
}
c = counter{
    n: 41
}
c.bump()
v = c.read()
print('c.n = {v}')
c.bump()
v = c.read()
print('after 2 bumps = {v}')
`,
  },

  // ------------------------------------------------------------------ types --
  {
    id: 'char',
    category: 'types',
    i18n: {
      'zh-Hant': {label: '字符 char', description: "單引號字面量是 Unicode 碼點；print 會把它渲染成字符。 "},
      'zh-Hans': {label: '字符 char', description: "单引号字面量是 Unicode 码点；print 会把它渲染成字符。 "},
      en: {label: 'char literals', description: "Single-quoted literals are Unicode code points; print renders them as characters."},
      ja: {label: '文字 char', description: '単一引用符のリテラルは Unicode コードポイントです。print はこれを文字として表示します。 '},
      ko: {label: '문자 char', description: '작은따옴표 리터럴은 Unicode 코드 포인트이며, print가 문자로 렌더링합니다.'},
    },
    expect: 'Z = Z',
    code: `; char literals are Unicode code points; print renders the character.
c = 'A'
print('A = {c}')
d = 'Z'
print('Z = {d}')
`,
  },
  {
    id: 'bool',
    category: 'types',
    i18n: {
      'zh-Hant': {label: '布爾 bool', description: '布爾字面量、比較運算、&& / || 與 !；輸出爲 true / false。'},
      'zh-Hans': {label: '布尔 bool', description: '布尔字面量、比较运算、&& / || 与 !；输出为 true / false。'},
      en: {label: 'Booleans', description: 'Boolean literals, comparisons, && / || and !; printed as true / false.'},
      ja: {label: '真偽値 bool', description: '真偽値リテラル、比較演算、&& / || と !。出力は true / false です。'},
      ko: {label: '불리언 bool', description: '불리언 리터럴, 비교 연산, && / || 및 !. 출력은 true / false입니다.'},
    },
    expect: 'both = true',
    code: `; Booleans, comparisons, logical operators and '!'.
t = true
f = false
print('t = {t}, f = {f}')
a = 3
b = 7
print('a = {a}, b = {b}')
lt = a < b
eq = a == b
print('lt = {lt}, eq = {eq}')
both = lt && !eq
print('both = {both}')
`,
  },
  {
    id: 'floats',
    category: 'types',
    i18n: {
      'zh-Hant': {label: '浮點 f64', description: '浮點精度格式化，以及 f64 / i64 上的 sqrt() 方法。'},
      'zh-Hans': {label: '浮点 f64', description: '浮点精度格式化，以及 f64 / i64 上的 sqrt() 方法。'},
      en: {label: 'Floating point', description: 'Precision formatting and the sqrt() method on f64 / i64.'},
      ja: {label: '浮動小数点数 f64', description: '浮動小数点数の精度指定フォーマットと、f64 / i64 上の sqrt() メソッド。'},
      ko: {label: '부동소수점 f64', description: '부동소수점 정밀도 포맷과 f64 / i64의 sqrt() 메서드.'},
    },
    expect: 'sqrt(16) = 4',
    code: `; f64 — formatting precision and the sqrt method.
r = 2.0
area = 3.14159
print('r = {r:.1f}')
print('pi ~ {area:.3f}')

sq = 16.0.sqrt()
print('sqrt(16.0) = {sq:.1f}')
isq = 16.sqrt()
print('sqrt(16) = {isq}')
`,
  },
  {
    id: 'bytes',
    category: 'types',
    i18n: {
      'zh-Hant': {label: '字節 byte', description: '[]byte 字節切片；byte 是 0..255 的無符號值，算術按 2 補數迴繞。'},
      'zh-Hans': {label: '字节 byte', description: '[]byte 字节切片；byte 是 0..255 的无符号值，算术按 2 补数回绕。'},
      en: {label: 'byte slices', description: '[]byte literals; byte is an unsigned 0..255 value with wrapping two-complement arithmetic.'},
      ja: {label: 'バイト byte', description: '[]byte バイトスライス。byte は 0..255 の符号なし値で、算術演算は 2 の補数で折り返します。'},
      ko: {label: '바이트 byte', description: '[]byte 바이트 슬라이스. byte는 0..255의 부호 없는 값이며 산술 연산은 2의 보수로 랩어라운드합니다.'},
    },
    expect: 'b[0] = 65',
    code: `; []byte — a byte slice and its length.
b []byte = [65, 66, 67]
n = len(b)
print('len(b) = {n}')
x = b[0]
print('b[0] = {x}')
`,
  },

  // -------------------------------------------------------------------- std --
  {
    id: 'math',
    category: 'std',
    i18n: {
      'zh-Hant': {label: 'math 模塊', description: 'math.max / math.min 是可變參數函數；sqrt 是數值類型上的方法。'},
      'zh-Hans': {label: 'math 模块', description: 'math.max / math.min 是可变参数函数；sqrt 是数值类型上的方法。'},
      en: {label: 'math module', description: 'math.max / math.min are variadic functions; sqrt is a method on numeric types.'},
      ja: {label: 'math モジュール', description: 'math.max / math.min は可変長引数関数です。sqrt は数値型上のメソッドです。'},
      ko: {label: 'math 모듈', description: 'math.max / math.min은 가변 인자 함수이고, sqrt는 수치 타입의 메서드입니다.'},
    },
    expect: 'max = 20.0',
    code: `; math.max / math.min are variadic; sqrt is a numeric method.
m = math.max(10.0, 20.0)
print('max = {m:.1f}')
mn = math.min(10.0, 20.0)
print('min = {mn:.1f}')
sq = 16.0.sqrt()
print('sqrt = {sq:.1f}')
`,
  },
  {
    id: 'rand',
    category: 'std',
    i18n: {
      'zh-Hant': {label: 'rand 隨機數', description: 'rand.rand(state) 返回 (新狀態, 隨機值)；用取模把結果約束到 [0..99]。'},
      'zh-Hans': {label: 'rand 随机数', description: 'rand.rand(state) 返回 (新状态, 随机值)；用取模把结果约束到 [0..99]。'},
      en: {label: 'rand module', description: 'rand.rand(state) returns (new-state, value); use modulo to bound the result to [0..99].'},
      ja: {label: 'rand 乱数', description: 'rand.rand(state) は (新しい状態, 乱数値) を返します。剰余演算で結果を [0..99] に制限します。'},
      ko: {label: 'rand 난수', description: 'rand.rand(state)는 (새 상태, 난수 값)을 반환합니다. 나머지 연산으로 결과를 [0..99] 범위로 제한합니다.'},
    },
    expect: 'rand[0] = 2714967881',
    code: `; rand.rand(state) returns (new-state, value) — two results.
seed = 123456789
state = seed
i <- [0..5): {
    state, r = rand.rand(state)
    print('rand[{i}] = {r}')
}
state = seed
state, r = rand.rand(state)
r99 = r % 100
print('rand[0..99] = {r99}')
`,
  },

  // ----------------------------------------------------------------- errors --
  {
    id: 'option-match',
    category: 'errors',
    i18n: {
      'zh-Hant': {label: 'option 與 nil', description: '用 ?T 表示可能失敗的結果，並以 nil / 通配臂消費 option 值。'},
      'zh-Hans': {label: 'option 与 nil', description: '用 ?T 表示可能失败的结果，并以 nil / 通配臂消费 option 值。'},
      en: {label: 'option & nil', description: 'Use ?T for a fallible result and consume it with nil / catch-all match arms.'},
      ja: {label: 'option と nil', description: '?T で失敗する可能性のある結果を表し、nil / ワイルドカード腕で option 値を消費します。'},
      ko: {label: 'option과 nil', description: '?T로 실패할 수 있는 결과를 표현하고, nil / 와일드카드 팔로 option 값을 소비합니다.'},
    },
    expect: '10 / 0 = undefined',
    code: `; option values — ok / err / nil arms and the implicit 'it' binding.
safe-div = (a i64, b i64) (r ?i64) {
    b == 0 -> {
        r = nil
        return
    }
    r = a / b
}

div1 = safe-div(10, 2)
div1: {
    nil -> print('cannot divide by zero')
    -> print('10 / 2 = {it}')
}

div2 = safe-div(10, 0)
div2: {
    nil -> print('10 / 0 = undefined')
    -> print('10 / 0 = {it}')
}
`,
  },
];

/** Look up an example by id. Returns undefined if not found. */
export function getExampleById(id: string): Example | undefined {
  return examples.find((e) => e.id === id);
}

/** The example loaded by default when the playground first mounts. */
export const DEFAULT_EXAMPLE_ID = 'hello';

export const DEFAULT_EXAMPLE = getExampleById(DEFAULT_EXAMPLE_ID) ?? examples[0];
