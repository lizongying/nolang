---
sidebar_position: 3
---

# Strings

Nolang strings (`str`) are a union type (short ≤127 bytes stored on stack / long stored on heap), supporting various operators and methods.

## String Operators

### Concatenation (`-`)

Use the `-` operator to concatenate strings:

```nolang
// Literal concatenation
s = 'Hello' - ' ' - 'World'

// Concatenation with variable
greeting = 'Hello, ' - name
```

### Repetition (`*`)

Use the `*` operator to repeat a string:

```nolang
s = 'Hello' * 3
```

## Indexing & Slicing

```nolang
s = 'Hello World'

// Index returns char (character, not byte)
c = s[0]           // c = code point of 'H'

// Slice (view, shares underlying memory)
sub = s[6..]       // 'World'
sub = s[6..11]     // 'World'
sub = s[0..5)      // 'Hello'

// Length
n = s.len          // byte length
n = s.count()      // code point count (Unicode character count)
```

## String Methods

```nolang
// Comparison
ok = a.eq(b, n)               // Equality comparison (method)
c = s.compare(b)              // Lexicographic comparison

// Search
pos = s.index(sub)             // Substring position
ok = s.contains(sub)           // Contains substring
ok = s.starts-with(sub)        // Prefix check
ok = s.ends-with(sub)          // Suffix check
ok = s.empty()                 // Is empty

// Conversion
out = s.to-upper()             // Convert to uppercase
out = s.to-lower()             // Convert to lowercase
out = s.trim()                 // Trim leading/trailing whitespace
out = s.trim-char(c)           // Trim specified character
out = s.repeat(n)              // Repeat
out = s.reverse()              // Reverse
out = s.slice(start, end)      // Slice
val = s.replace-char(old, new) // Replace character

// Convert to other types
b = s.to-bytes()               // Convert to []byte
v = s.to-i64()                 // Convert to ?i64
v = s.to-f64()                 // Convert to ?f64
v = s.to-bool()                // Convert to ?bool

// Split & Join
parts = s.split(sep)           // Split (returns []str)
out = ss.join(sep)             // Join []str with separator

// Copy & Fill
dst = s.copy()                 // String copy
s.fill(val byte)               // Fill with byte value
```

## String & Number Conversion

```nolang
// Number to string (method)
s = i64.to-str()               // i64 to string
s = f64.to-str()               // f64 to string
s = bool.to-str()              // bool to "true"/"false"
s = byte.to-str()              // byte to string
s = char.to-str()              // char to string

// String to number (returns option)
v = s.to-i64()                 // Returns ?i64
v = s.to-i32()                 // Returns ?i32
v = s.to-u64()                 // Returns ?u64
v = s.to-f64()                 // Returns ?f64
```

## Automatic Length Tracking

When assigning `s[i] = v`, LLVM codegen automatically updates the `len` field to `max(len, idx+1)` — no need to manually set `.len`:

```nolang
s = ''
s[0] = 72                      // len automatically becomes 1
s[1] = 105                     // len automatically becomes 2

// Manually setting .len is only for truncation (shortening)
s.len = 5
```
