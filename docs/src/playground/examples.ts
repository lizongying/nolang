/**
 * Playground example snippets.
 *
 * Each example is a self-contained Nolang program intended to run
 * under the Direct WASM backend in the browser playground.
 *
 * Constraints (per Playground spec):
 *   - No FFI (browser cannot link native libraries)
 *   - No fork/exec/pipe (WASI preview1 limitation)
 *   - No network sockets (WASI preview1 limitation)
 *   - File system is virtual (stdin/stdout only)
 *
 * Syntax reference: docs/docs/lang/syntax.md
 */
export interface Example {
  id: string;
  label: string;
  code: string;
}

export const examples: Example[] = [
  {
    id: 'hello',
    label: 'Hello World',
    code: `; Hello World — the classic first program
; No main entry needed; top-level statements run directly.

print('Hello, World!')

; String concatenation uses the '-' operator
name = 'Nolang'
print('Hello, ' - name - '!')

; printf is also available (no trailing newline)
; Named format strings: {name[:spec]} references variables directly
pi f64 = 3.14
printf('pi = {pi:.2f}\\n')
`,
  },
  {
    id: 'fib',
    label: 'Fibonacci',
    code: `; Fibonacci — iterative and recursive versions.
; Expected output:
;   fib-loop(10) = 55
;   fib-rec(10)  = 55

; Iterative version (preferred for production)
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

; Recursive version
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
print('fib-loop(10) = ' - loop10.to-str())

rec10 = fib-rec(10)
print('fib-rec(10) = ' - rec10.to-str())
`,
  },
  {
    id: 'vars',
    label: 'Variables & Functions',
    code: `; Variables & Functions — declaration, types, calls

; Type inference (i64 is the default integer type)
i = 42
f = 3.14
s = 'hello'
flag = true

; Explicit type annotation
n u64 = 100
arr [3]i64 = [1, 2, 3]
v []u8 = [10, 20, 30]

; Function definition with named result parameter
add = (a i64, b i64) (result i64) {
  result = a + b
}

; Function call — result binds to LHS
sum = add(3, 4)
print('3 + 4 = ' - sum.to-str())

; String concatenation with '-'
greeting = 'Hello, ' - s
print(greeting)

; Array element access
print('arr[0] = ' - arr[0].to-str())
print('v[1]   = ' - v[1].to-str())

; Multi-return function
swap = (a i64, b i64) (x i64, y i64) {
  x = b
  y = a
}
a, b = swap(5, 9)
print('swap(5, 9) = ' - a.to-str() - ', ' - b.to-str())
`,
  },
  {
    id: 'struct',
    label: 'Structs & Methods',
    code: `; Structs & Methods — define, instantiate, attach methods

; Struct definition (multi-line, fields on their own lines)
user {
  name str
  age i64
}

; Method attached to the user type.
; The receiver is referenced via '.' inside the body.
user.greet = () {
  print('Hello, I am ' - .name)
}

user.birthday = () {
  .age = .age + 1
}

; Struct literal (also multi-line, no commas)
u = user{
  name: 'Alice'
  age: 30
}

; Call methods on the instance
u.greet()
print('Age: ' - u.age.to-str())

u.birthday()
print('After birthday: ' - u.age.to-str())

; Mutate fields directly
u.name = 'Bob'
u.greet()
`,
  },
  {
    id: 'match',
    label: 'Match Expression',
    code: `; Match Expression — value matching with the 'x: { ... }' form

; Match on a numeric value with range patterns
score = 85
grade = score: {
  [0..59] -> 'F'
  [60..79] -> 'C'
  [80..89] -> 'B'
  [90..100] -> 'A'
  -> 'invalid'
}
print('Score ' - score.to-str() - ' -> grade ' - grade)

; Match on option type — nil / err / value
safe-div = (a i64, b i64) (r ?i64) {
  b == 0 -> {
    r = nil
    return
  }
  r = a / b
}

result = safe-div(10, 2)
result: {
  nil -> print('cannot divide by zero')
  -> print('10 / 2 = ' - it.to-str())
}

result2 = safe-div(10, 0)
result2: {
  nil -> print('10 / 0 = undefined')
  -> print('10 / 0 = ' - it.to-str())
}

; Match with multiple patterns joined by ||
n = 3
n: {
  1 || 3 || 5 || 7 -> print('odd small')
  2 || 4 || 6 -> print('even small')
  -> print('larger number')
}
`,
  },
  {
    id: 'rand',
    label: 'math/rand',
    code: `; rand — pseudo-random number generation (xorshift32)
; The rand module lives in std/crypto/rand.no; ShortName is 'rand'.
; rand.rand(state) returns (new-state, value) where value is in [0..4294967295].

; Generate 5 random numbers from a seed
seed i64 = 123456789
state = seed
i <- [0..5): {
  state, r = rand.rand(state)
  print('rand[' - i.to-str() - '] = ' - r.to-str())
}

; Bound a random value to [0..99] via modulo
state = seed
state, r = rand.rand(state)
print('rand[0..99] = ' - (r % 100).to-str())

; A few math builtins (LLVM intrinsics)
m = math.max(10.0, 20.0)
print('math.max(10.0, 20.0) = ' - m.to-str())

mn = math.min(10.0, 20.0)
print('math.min(10.0, 20.0) = ' - mn.to-str())

sq = math.sqrt(16.0)
print('math.sqrt(16.0) = ' - sq.to-str())
`,
  },
];

/**
 * Look up an example by id. Returns undefined if not found.
 */
export function getExampleById(id: string): Example | undefined {
  return examples.find((e) => e.id === id);
}

/**
 * The example loaded by default when the playground first mounts.
 */
export const DEFAULT_EXAMPLE_ID = 'hello';

export const DEFAULT_EXAMPLE = getExampleById(DEFAULT_EXAMPLE_ID) ?? examples[0];
