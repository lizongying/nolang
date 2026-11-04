---
sidebar_position: 1
---

# Nolang Introduction

Nolang is a memory-safe, GC-free systems programming language with intuitive semantics and minimal syntax.

## Core Features

- **Memory-safe**: No garbage collector; automatic memory management.
- **Powerful performance**: Compile-time optimization, no GC.
- **Minimal syntax**: Fewer keywords; no pointers, ownership, or lifetimes as implicit mental overhead; simpler syntax.
- **Intent-driven**: Respects developer intent; semantics align with human intuition.
- **Crash-free**: Eliminates implicit crash paths.


## Quick Start

```no
; Hello, World!
; No main entry point needed
print('Hello, Nolang!')

; Variable declaration
count i64

; Function definition
add = (a i64, b i64) (result i64) {
    result = a + b
}

; Standard library method, can be called directly
c = math.max(a, b)

; Struct
user {
    name str
    age i64
}

; Method
user.greet = () {
    print('Hello, {.name}')
}

u = user {
    name: 'Alice'
    age: 30
}

u.greet()
```

## Projects

- [notools](https://github.com/lizongying/notools) — A collection of common Unix command-line tools implemented in Nolang, including cat, ls, grep, wc, head, tail, and more. Demonstrates Nolang's real-world system programming capabilities.
