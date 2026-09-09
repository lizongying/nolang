---
sidebar_position: 1
---

# Nolang Introduction

Nolang is an experimental systems programming language: memory-safe with no GC, semantically intuitive, and minimally syntactic. It adopts a pass-by-reference model and a safe scope model to achieve absolute memory safety.

## Core Features

- **Memory-safe, no GC**: No garbage collector; automatic, safe memory management. Through the safe scope model, memory is automatically freed when leaving scope — no dangling pointers or memory leaks. Heap allocation is batched up-front, and a single batch free runs when the scope exits.
- **Semantically intuitive**: Respects developer intent; no pointers, ownership, or lifetimes as hidden mental overhead.
- **Minimal syntax**: Fewer keywords, simpler syntax.
- **Pass by reference**: All function parameters are references; functions return results by modifying parameters.
- **Performance-first**: Small strings require no heap allocation; variables can be allocated once and freed once.
- **Method overloading**: Efficient performance through monomorphization.
- **Interfaces**: Support interface declaration, default implementations, and multi-interface inheritance.
- **Generics**: Support type and value generics.
- **Pattern matching**: Unique match design, simpler to use.


## Quick Start

```no
; Hello, World!
; No main entry point needed
print('Hello, Nolang!')

; Variable declaration
i64

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

u = user {
    name: 'Alice'
    age: 30
}

; Method
user.greet = () {
    print('Hello, ' - .name)
}

u.greet()
```

## Projects

- [notools](https://github.com/lizongying/notools) — A collection of common Unix command-line tools implemented in Nolang, including cat, ls, grep, wc, head, tail, and more. Demonstrates Nolang's real-world system programming capabilities.
