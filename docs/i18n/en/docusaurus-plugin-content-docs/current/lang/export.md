---
sidebar_position: 5
---

# Export

Nolang uses the `@` keyword in the package root `lib.no` file to declare exported items. External packages can only access these exported symbols when importing via `#`.

## Syntax

```nolang
@ path.func [alias]
```

- `path` — Module path (relative to package root, starts with `/`, without `.no` extension)
- `func` — Name of the function/constant/enum to export
- `alias` — Optional alias name used when importing externally

## Rules

- Export statements can **only** be written in the package root `lib.no` file
- One export item per line
- Exported items can only be final symbols such as functions, constants, enums
- Structs and enums referenced by exported functions are **automatically exported** — no manual declaration needed
- If an exported function does not exist in the module, LSP will report an error

## Example

```nolang
; lib.no - package root export file
@ /src/utils.greet a
@ /src/utils.hello b
@ /src/math.pi
```

```nolang
; src/utils.no
; Define exported functions
greet = (name str) {
    print('Hello, ' - name)
}

hello = () {
    print('Hi')
}
```

## Importing Exported Symbols

External packages can only access exported items declared in `lib.no` when importing via `#`:

```nolang
; Import alias a (corresponds to package-name.utils.greet)
# package-name.utils.greet a

; Or use the function name directly
# package-name.utils.greet
```

## LSP Support

- **Go-to-definition**: Click on an exported function name or alias in `lib.no` to jump to its definition in the corresponding module file
- **Auto-completion**: Automatically suggests file paths and function names when typing `@` and a path
- **Error diagnostics**: Shows an error when an exported function does not exist in the module
