---
sidebar_position: 4
---

# Operators

## Special Symbols

- `#` ; Import module
- `@` ; Export module
- `->` ; Match arm and if/else branch (e.g., `cond -> body`)
- `:` ; Match expression (e.g., `x: { ... }`)
- `{ } (true)` ; Infinite loop (suffix form, legacy spelling); prefix `!! { }` / `true { }` is equivalent and is the new default (emitted by `no fmt`)
- `<-` ; Range iteration (e.g., `i <- [a..b]: { }`)
- `..` ; Parent class / range operator (e.g., `[a..b)`)
- `.` ; Self (current struct/type) (⚠️ in range bounds, use `self.method` not `.method` to avoid `...` ambiguity with the return operator)
- `?` ; Option type prefix (e.g., `?i64`, `?str`)
- `(cond) { }` ; Conditional loop (prefix form, the new default; equivalent to the suffix `{ } (cond)`)
- `N * { }` ; Counted loop (prefix form, the new default; equivalent to the suffix `{ } * N`; N ≤ 0 skips)
- `!` ; False; also the "never execute" loop prefix (`! { }` never runs)
- `!!` ; True; also the "always execute" loop prefix (`!! { }` loops forever)
- `{ } ()` ; Not executed (empty parens mean false; suffix form, legacy spelling); prefix `! { }` / `false { }` / `() { }` is equivalent and is the new default
- `*` ; Break — replaces break (planned, not yet replaced)
- `**` ; Skip current iteration — replaces continue (planned, not yet replaced)
- `...` ; Return statement, terminate function — replaces return (planned, not yet replaced)
- `run` ; Start async thread
- `awy` ; Wait for async thread completion

## Arithmetic Operators

- `+` ; Addition
- `-` ; Subtraction (also used for string concatenation)
- `*` ; Multiplication (also used for string repetition)
- `/` ; Division

## Comparison Operators

- `==` ; Equal to (real string equality for `str`, multi-character allowed)
- `!=` ; Not equal to (same as above)
- <code>&lt;</code> ; Less than
- `>` ; Greater than
- <code>&lt;=</code> ; Less than or equal to
- `>=` ; Greater than or equal to

:::warning Ordering operators (<code>&lt;</code> `>` <code>&lt;=</code> `>=`) have no string semantics

`str` only implements equality (`==` / `!=`). Ordering a string used to **silently
return false**; it is now a compile error.

- Allowed: a **one-character** string literal `'a'`, implicitly a `char` (its code
  point), comparable with another one-character literal or with a `char` (`"a"`).
  Numbers are unaffected.
- Rejected: multi-character strings — the literal `'ab'`, a `str` variable, or a call
  returning `str`.
- For lexicographic order use `str.compare(b)` (returns -1 / 0 / 1).

```nolang
'a' <= 'z'      ; true  — one-char string is implicitly a char
ch >= 'a'       ; ch is a char, fine
'a' > "b"       ; one-char string vs char, fine
'abc' < 'abd'   ; compile error: multi-character string
s1 < s2         ; compile error: str variables
s1.compare(s2) > 0   ; the correct form
```
:::

## Logical Operators

- `&&` ; Logical AND
- `||` ; Logical OR (also used for match branch combination, e.g., `nil || err -> body`)
- `!` ; Logical NOT

## Bitwise Operators

- `&` ; Bitwise AND
- `|` ; Bitwise OR
- `^` ; Bitwise XOR
- `~` ; Bitwise NOT
- <code>&lt;&lt;</code> ; Left shift
- `>>` ; Right shift

## Assignment Operators

- `=` ; Assignment
- `+=` ; Add-assign
- `-=` ; Subtract-assign
- `*=` ; Multiply-assign
- `/=` ; Divide-assign
- `%=` ; Modulo-assign
- `&=` ; Bitwise AND-assign
- `|=` ; Bitwise OR-assign
- `^=` ; Bitwise XOR-assign
- <code>&lt;&lt;=</code> ; Left shift-assign
- `>>=` ; Right shift-assign

## Others

- `?` ; Ternary operator (e.g., `c = flag ? 1 : 2`)
- `as` ; FFI pointer type conversion (e.g., `y = x as *byte`)
- `..` ; Slice range (e.g., `arr[1..3]`, `arr[1..]`, `arr[..3]`)
