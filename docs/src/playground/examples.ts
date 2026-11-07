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
import type {CategoryId, ExampleI18n} from './i18n';

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
  i18n: Record<'zh-Hans' | 'en', {label: string; description: string}>;
}

/** Order in which categories appear in the Examples dropdown. */
export const CATEGORY_ORDER: Category[] = [
  {
    id: 'basics',
    i18n: {
      'zh-Hans': {label: '基础', description: '从第一行程序开始：变量、函数与输出。'},
      en: {label: 'Basics', description: 'Start here: variables, functions and output.'},
    },
  },
  {
    id: 'control',
    i18n: {
      'zh-Hans': {label: '流程控制', description: '条件、模式匹配与各种循环写法。'},
      en: {label: 'Control flow', description: 'Guards, pattern matching and every loop form.'},
    },
  },
  {
    id: 'data',
    i18n: {
      'zh-Hans': {label: '数据结构', description: '数组、切片、结构体与方法。'},
      en: {label: 'Data structures', description: 'Arrays, slices, structs and methods.'},
    },
  },
  {
    id: 'types',
    i18n: {
      'zh-Hans': {label: '标量类型', description: 'char、bool、f64 与 byte 的行为。'},
      en: {label: 'Scalar types', description: 'How char, bool, f64 and byte behave.'},
    },
  },
  {
    id: 'std',
    i18n: {
      'zh-Hans': {label: '标准库', description: 'math 与 rand 模块的常用函数。'},
      en: {label: 'Standard library', description: 'Everyday math and rand module functions.'},
    },
  },
  {
    id: 'errors',
    i18n: {
      'zh-Hans': {label: '错误处理', description: '用 option 值表达可能失败的计算。'},
      en: {label: 'Error handling', description: 'Model fallible computations with option values.'},
    },
  },
];

export const examples: Example[] = [
  // ---------------------------------------------------------------- basics --
  {
    id: 'hello',
    category: 'basics',
    i18n: {
      'zh-Hans': {label: 'Hello World', description: '顶层语句直接执行，无需 main 函数；并演示命名格式化字符串与浮点精度。'},
      en: {label: 'Hello World', description: 'Top-level statements run without a main function; also shows named format strings and float precision.'},
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
      'zh-Hans': {label: '变量与类型推断', description: '类型推断、显式类型标注，以及用逗号左侧绑定多返回值。'},
      en: {label: 'Variables & inference', description: 'Type inference, explicit annotations, and binding multiple results with a comma-separated LHS.'},
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
      'zh-Hans': {label: '函数与递归', description: '参数、具名结果参数、递归调用与多返回值函数。'},
      en: {label: 'Functions & recursion', description: 'Parameters, named results, recursive calls and multi-result functions.'},
    },
    expect: '5! = 120',
    code: `; Functions — parameters, named results, recursion, multiple results.
add = (a i64, b i64) (r i64) {
    r = a + b
}
s = add(3, 4)
print('3 + 4 = {s}')

fact = (n i64) (r i64) {
    n < 2 -> {
        r = 1
        return
    }
    p = fact(n - 1)
    r = n * p
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
      'zh-Hans': {label: '斐波那契', description: '同一函数的迭代版与递归版，对比两种写法。'},
      en: {label: 'Fibonacci', description: 'Iterative and recursive implementations of the same function side by side.'},
    },
    expect: 'fib-rec(10) = 55',
    code: `; Fibonacci — iterative and recursive implementations.
fib-loop = (n i64) (r i64) {
    a i64 = 0
    b i64 = 1
    i <- [0..n): {
        t = a + b
        a = b
        b = t
    }
    r = a
}

fib-rec = (n i64) (r i64) {
    n < 2 -> {
        r = n
        return
    }
    p1 = fib-rec(n - 1)
    p2 = fib-rec(n - 2)
    r = p1 + p2
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
      'zh-Hans': {label: '算术与位运算', description: '整数五则运算与按位与；结果可直接在格式字符串中引用。'},
      en: {label: 'Arithmetic & bitwise', description: 'Integer arithmetic and bitwise AND; results can be referenced directly in format strings.'},
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
      'zh-Hans': {label: '格式化输出', description: '{name} 命名插值与 {name:spec} 精度控制；无字段的字符串原样输出。'},
      en: {label: 'Formatting', description: '{name} interpolation and {name:spec} precision; a literal without fields prints verbatim.'},
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
      'zh-Hans': {label: '字符串', description: "用 '-' 拼接字符串、len() 取长度、to-str() 转换标量，以及按下标取字节。 "},
      en: {label: 'Strings', description: "Concatenate with '-', measure with len(), convert with to-str(), and index a byte."},
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
      'zh-Hans': {label: '短路守卫', description: 'cond -> action 形式的短路条件语句，条件成立时才执行右侧动作。'},
      en: {label: 'Short-circuit guards', description: 'The cond -> action form; the action runs only when the condition holds.'},
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
      'zh-Hans': {label: '模式匹配', description: 'subject: { ... } 形式的值匹配：区间模式、开区间与 || 多模式。'},
      en: {label: 'Match expression', description: 'subject: { ... } matching with interval patterns, open bounds and || alternatives.'},
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
      'zh-Hans': {label: 'FizzBuzz', description: '在范围循环里用块匹配替代链式 if，展示模式分支的写法。'},
      en: {label: 'FizzBuzz', description: 'Block-match patterns inside a range loop, as an alternative to chained ifs.'},
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
      'zh-Hans': {label: '区间循环', description: '四种有界区间写法，以及必须用 break 终止的无界区间 [0..)。'},
      en: {label: 'Range loops', description: 'Four bounded interval forms plus the unbounded [0..) form, which needs a break.'},
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
      'zh-Hans': {label: '条件循环', description: '(cond) { ... } 形式的 while 循环，条件由循环体更新。'},
      en: {label: 'Conditional while loop', description: 'The (cond) { ... } while form, where the body updates the condition.'},
    },
    expect: 'n = 3',
    code: `; Conditional while-do loop: (cond) { ... }
n = 0
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
      'zh-Hans': {label: 'break / continue', description: 'continue 跳到本轮末尾（仍会执行递增），break 直接离开循环。'},
      en: {label: 'break / continue', description: 'continue jumps to the end of the iteration (the increment still runs); break leaves the loop.'},
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
      'zh-Hans': {label: '定长数组', description: '[3]i64 定长数组的字面量、下标读取与下标写入。'},
      en: {label: 'Fixed arrays', description: 'A [3]i64 literal with indexed reads and an indexed write.'},
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
      'zh-Hans': {label: '动态切片', description: '[]T 切片、len/cap 属性、下标赋值与 with-cap 预分配。'},
      en: {label: 'Slices', description: '[]T literals, len/cap, index assignment and with-cap preallocation.'},
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
      'zh-Hans': {label: '结构体', description: '结构体声明、字面量构造，以及字段的读写。'},
      en: {label: 'Structs', description: 'Struct declaration, literal construction and field reads/writes.'},
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

u.age = u.age + 1
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
      'zh-Hans': {label: '方法', description: '把函数挂到结构体类型上；方法体内用 . 引用接收者。'},
      en: {label: 'Methods', description: 'Attach functions to a struct type; . refers to the receiver inside the body.'},
    },
    expect: 'after 2 bumps = 43',
    code: `; Methods — attach functions to a struct; '.' is the receiver.
counter {
    n i64
}
counter.bump = () {
    .n = .n + 1
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
      'zh-Hans': {label: '字符 char', description: "单引号字面量是 Unicode 码点；print 会把它渲染成字符。 "},
      en: {label: 'char literals', description: "Single-quoted literals are Unicode code points; print renders them as characters."},
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
      'zh-Hans': {label: '布尔 bool', description: '布尔字面量、比较运算、&& / || 与 !；输出为 true / false。'},
      en: {label: 'Booleans', description: 'Boolean literals, comparisons, && / || and !; printed as true / false.'},
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
      'zh-Hans': {label: '浮点 f64', description: '浮点精度格式化，以及 f64 / i64 上的 sqrt() 方法。'},
      en: {label: 'Floating point', description: 'Precision formatting and the sqrt() method on f64 / i64.'},
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
      'zh-Hans': {label: '字节 byte', description: '[]byte 字节切片；byte 是 0..255 的无符号值，算术按 2 补数回绕。'},
      en: {label: 'byte slices', description: '[]byte literals; byte is an unsigned 0..255 value with wrapping two-complement arithmetic.'},
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
      'zh-Hans': {label: 'math 模块', description: 'math.max / math.min 是可变参数函数；sqrt 是数值类型上的方法。'},
      en: {label: 'math module', description: 'math.max / math.min are variadic functions; sqrt is a method on numeric types.'},
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
      'zh-Hans': {label: 'rand 随机数', description: 'rand.rand(state) 返回 (新状态, 随机值)；用取模把结果约束到 [0..99]。'},
      en: {label: 'rand module', description: 'rand.rand(state) returns (new-state, value); use modulo to bound the result to [0..99].'},
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
      'zh-Hans': {label: 'option 与 nil', description: '用 ?T 表示可能失败的结果，并以 nil / 通配臂消费 option 值。'},
      en: {label: 'option & nil', description: 'Use ?T for a fallible result and consume it with nil / catch-all match arms.'},
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
