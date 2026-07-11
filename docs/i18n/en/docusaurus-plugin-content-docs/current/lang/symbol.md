---
sidebar_position: 4
---

# Operators

## Special Symbols

- `#` // Import module
- `@` // Export module
- `->` // Match arm and if/else branch (e.g., `cond -> body`)
- `:` // Match expression (e.g., `x: { ... }`)
- `!!` // Infinite loop (`!! { }`)
- `<-` // Range iteration (e.g., `i <- [a..b]: { }`)
- `..` // Parent class
- `.` // Self (current struct/type)
- `?` // Option type prefix (e.g., `?i64`, `?str`)
- `!` // False, error — replaces false (planned, not yet replaced)
- `!!` // True, correct — replaces true (planned, not yet replaced)
- `!{}` // Loop
- `*` // Break — replaces break (planned, not yet replaced)
- `**` // Skip current iteration — replaces continue (planned, not yet replaced)
- `...` // Return statement, terminate function — replaces return (planned, not yet replaced)
- `run` // Start async thread
- `awy` // Wait for async thread completion

## Arithmetic Operators

- `+` // Addition
- `-` // Subtraction (also used for string concatenation)
- `*` // Multiplication (also used for string repetition)
- `/` // Division

## Comparison Operators

- `==` // Equal to
- `!=` // Not equal to
- <code>&lt;</code> // Less than
- `>` // Greater than
- <code>&lt;=</code> // Less than or equal to
- `>=` // Greater than or equal to

## Logical Operators

- `&&` // Logical AND
- `||` // Logical OR (also used for match branch combination, e.g., `nil || err -> body`)
- `!` // Logical NOT

## Bitwise Operators

- `&` // Bitwise AND
- `|` // Bitwise OR
- `^` // Bitwise XOR
- `~` // Bitwise NOT
- <code>&lt;&lt;</code> // Left shift
- `>>` // Right shift

## Assignment Operators

- `=` // Assignment
- `+=` // Add-assign
- `-=` // Subtract-assign
- `*=` // Multiply-assign
- `/=` // Divide-assign
- `%=` // Modulo-assign
- `&=` // Bitwise AND-assign
- `|=` // Bitwise OR-assign
- `^=` // Bitwise XOR-assign
- <code>&lt;&lt;=</code> // Left shift-assign
- `>>=` // Right shift-assign

## Others

- `?` // Ternary operator (e.g., `c = flag ? 1 : 2`)
- `as` // Type conversion (e.g., `y = x as i64`)
- `..` // Slice range (e.g., `arr[1..3]`, `arr[1..]`, `arr[..3]`)
