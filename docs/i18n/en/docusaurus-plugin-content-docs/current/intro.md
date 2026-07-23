---
sidebar_position: 1
---

# Nolang Introduction

Nolang is an experimental systems programming language that adopts a pass-by-reference model and safe scope model to achieve absolute memory safety. No GC.

## Core Features

- **Developer-friendly**: No pointers, no ownership, no lifetimes...
- **Pass by reference**: All function parameters are references; functions return results by modifying parameters
- **Automatic memory management**: Through the safe scope model, memory is automatically released when leaving scope — no dangling pointers or memory leaks
- **No GC**: No memory leaks, so no GC needed
- **Performance-first**:
Small strings have no heap allocation; variables can be allocated once and freed once
- **Method overloading**: Efficient performance through monomorphization
- **Interfaces**: Support interface declaration, default implementations, and multi-interface inheritance
- **Generics**: Support type and value generics
- **Pattern matching**: Unique match design, simpler to use


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
