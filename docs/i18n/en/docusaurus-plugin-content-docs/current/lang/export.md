---
sidebar_position: 5
---

# Export

Nolang uses the `@` keyword in the package root `lib.no` file to declare exported items. External packages can only access these exported symbols when importing via `#`.

## Syntax

```no
@ path.func [alias]
```

- `path` — Module path (relative to package root, starts with `/`, without `.no` extension)
- `func` — Name of the function/constant/enum to export
- `alias` — Optional alias name used when importing externally

## Rules

- Export statements can **only** be written in the package root `lib.no` file (the one exception is the next section, "Global callable aliases")
- One export item per line
- Exported items can only be final symbols such as functions, constants, enums
- Structs and enums referenced by exported functions are **automatically exported** — no manual declaration needed
- If an exported function does not exist in the module, LSP will report an error

## Example

```no
; lib.no - package root export file
@ /src/utils.greet a
@ /src/utils.hello b
@ /src/math.pi
```

```no
; src/utils.no
; Define exported functions
greet = (name str) {
    print('Hello, ' - name)
}

hello = () {
    print('Hi')
}
```

## Global callable aliases

The same declaration, written in **`std/global.no`** instead of a package's `lib.no`,
means something different: it does not filter exports, it declares a **global second
name that can be called**. The bare name `<alias>` simply *is* `path`'s `func` —
exactly as if the source had written `<path>.<func>(...)`.

```no
; std/global.no — two spellings, same meaning
@ std/err.new err      ; export-style
err = err.new          ; assignment-style
```

```no
e = err('disk full')        ; same as e = err.new('disk full')
m = e.msg()                 ; 'disk full'
```

Rules:

- Only `std/global.no` has this meaning; `@` in a `lib.no` is still an export filter
- The two spellings are **equivalent** — the compiler normalises them to one
  declaration, and writing both at once does not conflict
- The assignment spelling is valid **only in `global.no`**. In an ordinary program
  `x = mod.fn` is a variable holding a function value (usable as an indirect call),
  not a global alias
- Its right-hand side must be a bare `<module>.<function>` reference: not a call,
  not a literal, not a field read, not a typed binding. When the target is not a
  function the declaration simply has no effect, so constant declarations such as
  `PI = math.PI` are unaffected
- The alias applies to **bare-name calls** only. Qualified calls are unaffected:
  `io.err(...)` is still `io`'s stderr writer and is not captured by the `err` alias
- A module calling its own same-named function is unaffected (self-module carve-out)
- The alias's **parameter list** comes from the target; the **result type** is
  determined at the call site by the target function
- The alias does not conflict with the builtin option's `err` variant: where an
  option type is expected (`x = err('m')` after `x ?i64`, an `err ->` match arm,
  a function returning `?T`), `err(...)` is still the option's err variant
  constructor (tag 2)

## Importing Exported Symbols

External packages can only access exported items declared in `lib.no` when importing via `#`:

```no
; Import alias a (corresponds to package-name.utils.greet)
# package-name.utils.greet a

; Or use the function name directly
# package-name.utils.greet
```

## LSP Support

- **Go-to-definition**: Click on an exported function name or alias in `lib.no` to jump to its definition in the corresponding module file
- **Auto-completion**: Automatically suggests file paths and function names when typing `@` and a path
- **Error diagnostics**: Shows an error when an exported function does not exist in the module
