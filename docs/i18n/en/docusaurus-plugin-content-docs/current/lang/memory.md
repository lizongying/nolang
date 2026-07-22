---
sidebar_position: 6
---

# Memory Management

Nolang is a **GC-free** language. Memory safety is guaranteed by compiler-inserted `free` calls. This document describes the implemented memory design and ownership semantics.

## Core Principles

### Single Ownership
Each heap `data` buffer has **exactly one owner**. Ownership can be transferred via move; after transfer, the original owner relinquishes free responsibility. For `=` between local variables, a deep clone makes both variables independently own their data.

### Three Assignment Semantics
`b = a` selects one of three semantics based on context:

| Semantic | Trigger | Behavior |
|----------|---------|----------|
| **Value copy** | Primitive types (i64/f64/bool, etc.) | Direct value copy, no heap data |
| **Deep clone** | `b = a` between locals, a is heap-owning (vec/arr/str/cloneable struct) | malloc new data + memcpy + recursively clone elements; a and b independently own data, each freed at function exit |
| **move** | Output param `out = x`, `vec.push(x)` | Shallow copy struct + mark source as moved; source skips free |

### Compiler-Inserted Free
- Function exit: free all non-moved local heap variables
- Before reassignment: free the old value
- Struct fields: recursively free fields containing heap data

## Type Layout

| Nolang Type | Memory Layout | Fields | Allocation |
|------------|---------------|--------|-----------|
| `[]T` (slice) | 24 bytes | len, cap, data | malloc (heap) |
| `[N]T` (fixed array) | 16 bytes | len, data | alloca (stack) or malloc |
| `str` (long string) | 24 bytes | len, cap, data | malloc (heap) |
| struct | sum of fields | each field | alloca (stack) |

## Shallow Free vs Deep Free

### Shallow Free
Only frees the container's data buffer without iterating elements. Applies to:
- `%str-long` (string data is a character buffer, no nested heap-owning elements)
- vec/arr whose elements are primitive types (i64, double, etc.)

### Deep Free
Iterates each element to recursively free its heap data, then frees the container's data buffer. Applies to vec/arr whose elements are heap-owning types:
- `[]str` (elements are %str-long)
- `[][]i64` (elements are %vec)
- `[]MyType` (elements are user structs; recursively free fields)

### NULL Check
All frees are preceded by `icmp eq i8* %ptr, null` to avoid free(NULL) or freeing uninitialized pointers.

## Ownership Transfer (move)

### Single Return Value move
```nolang
get-slice = () (out []i64) {
    local = [1, 2, 3]
    out = local   ; local marked as moved, not freed at function exit; out managed by caller
}

v = get-slice()  ; v owns data, freed at function exit
```

### Multi-Return Value move (by parameter position order)
```nolang
get-pair = () (a []i64, b []i64) {
    x = [1, 2]
    y = [3, 4]
    a = x   ; first output param, x marked as moved
    b = y   ; second output param, y marked as moved
}

a, b = get-pair()  ; a owns x's data, b owns y's data
```

**Processing order**: Output parameters are processed in their **declaration order** in the function signature. Each `out = src` assignment independently marks the source variable as moved.

**Note**: If `a` and `b` reference the same source variable (e.g., `a = x; b = x`), within the callee only one move occurs (x marked moved); both a and b receive a shallow copy of x (sharing the same data pointer). But in the caller, a and b are independent local variables, each tracked as a heap variable, and both will be freed at function exit → **double-free**. Nolang currently has no reference/borrow semantics; b does not automatically become an alias of a. **Avoid this pattern**.

### Implicit move in vec.push
```nolang
inner = [1, 2, 3]
outer.push(inner)
; inner marked as moved, data ownership transferred to outer
; inner skips free at function exit, outer deep-frees inner's data
```

push only shallow-copies inner's struct into outer's element slot **without cloning data**. Thus the source variable and the outer vec share the same data pointer; the source must be marked as moved to avoid double-free.

## Deep Clone (Assignment Between Locals)

```nolang
a []i64 = [10, 20, 30]
b = a          ; deep clone: malloc new data + memcpy + recursively clone elements
b[0] = 99
; a[0] == 10 (a unaffected)
; b[0] == 99 (b modified independently)
```

### Deep Clone Flow
1. Free the target variable's old value (if it already has heap data)
2. `malloc` a new data buffer, `memcpy` source data to new data
3. Recursively clone each heap-owning element:
   - `%str-long` element: malloc + memcpy string data
   - User struct element: memcpy struct + recursively clone heap-owning fields
4. Write new data pointer, len, cap into the target variable
5. Track target as a heap variable (freed at function exit)

### Cloneable Types
| Type | Deep cloneable | Notes |
|------|----------------|-------|
| `%vec` / `%arr` (primitive elements) | Yes | memcpy data suffices |
| `%vec` / `%arr` (elements are %str-long) | Yes | per-element malloc+memcpy of string data |
| `%vec` / `%arr` (elements are cloneable structs) | Yes | per-element recursive clone of struct fields |
| `%vec` / `%arr` (elements are %vec / %arr) | No | nested container element type unknown, falls back to move |
| `%str-long` | Yes | malloc + memcpy string data |
| User struct (no nested container fields) | Yes | memcpy struct + recursive clone of heap fields |
| User struct (with nested container fields) | No | falls back to move |

### Difference from move
- **Deep clone**: source and target each independently own data; each freed at function exit
- **move**: source relinquishes ownership (marked moved), target takes over data, source skips free

Decision rules for `b = a`:
1. If a is the source of an output param → move
2. If a is the source of vec.push → move
3. Otherwise, if a is a heap-owning type and deep-cloneable → deep clone
4. Otherwise value copy

## Slice Views

A slice expression `arr[1..3]` produces a view (zero-copy) that shares the original array's data. Three fates of a view:

| Target | Behavior | Ownership |
|--------|----------|-----------|
| Local var `v = arr[1..3]` | zero-copy view | shares original data |
| Output param `out = arr[1..3]` | clone (malloc+memcpy) | independent |
| Explicit `[]T` type `v []i64 = arr[1..3]` | clone | independent |

**Reason**: Output params escape to the caller; the original array may be freed before the function exits, so the view must clone to independent data.

## Reassignment and Old Value Free

```nolang
s = 'hello'     ; malloc data buffer
s = 'world'     ; free 'hello's data, malloc new data
```

When reassigning a heap-owning type, the compiler automatically frees the old value's data before the assignment to prevent leaks.

## Struct Field Free

```nolang
Node {
    name str
    items []i64
}

n = Node{
    name: 'hello'
    items: [1, 2, 3]
}
; At function exit, recursively free:
;   - n.name.data (%str-long field)
;   - n.items.data (%vec field)
```

When freeing a struct, all fields are traversed; heap-owning type fields are recursively freed.

## Fixed Array Reassigned to Slice

```nolang
local [4]i64 = [100, 200, 300, 400]   ; local is fixed array (16 bytes)
local = [100, 200, 300]                ; reassigned as slice (24 bytes)
```

Fixed arrays (`%arr`, 2 fields) and slices (`%vec`, 3 fields) have different memory layouts. On reassignment the compiler automatically allocates a new `%vec` variable and redirects all subsequent accesses to avoid buffer overflow.

## Verified Test Cases

Tests are in `tests/mem-safety/`:

| Test | Verifies |
|------|----------|
| `deep-clone.no` | `b = a` deep clone ([]i64/[]str/str/struct) independence |
| `deep-free-str.no` | `[]str` deep free |
| `deep-free-nested-vec.no` | `[][]i64` deep free + push moved |
| `deep-free-struct-vec.no` | `[]MyType` deep free (recursive struct) |
| `struct-field-leak.no` | struct field heap data free |
| `slice-view-escape.no` | slice view assigned to output param clone |
| `reassign-leak.no` | reassignment old value free |
| `vec-push-leak.no` | vec.push moved marking |

## Known Limitations

### map Container
hashmap does not implement deep free of key/value; map container heap data leaks.

### Loop Temporary Variables
```nolang
loop {
    s = 'temp'   ; each iteration mallocs new data, old data not freed
}
```

### Slice View + Original Array move
```nolang
view = arr[1..3]   ; view shares arr.data
arr = [9, 8, 7]    ; free old arr.data → view dangling
```

### async Shared Data
When async threads share heap data with the main thread, free order is nondeterministic.

### Global Variables
Module-level variable heap data relies on process exit; long-running services may leak.
