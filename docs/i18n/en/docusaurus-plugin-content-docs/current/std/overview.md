---
sidebar_position: 3
---

# Standard Library

The Nolang standard library (`src/std/`) contains 60+ modules, covering formatting, math, strings, data structures, encoding/decoding, encryption, compression, file operations, I/O abstractions, and more.

Usage: `# std/xxx` (core modules require no import).

> **The legacy `use std/xxx` syntax still works but is deprecated; the new `# std/xxx` syntax is recommended.**

> **Note: All code examples in this document follow the "one statement per line" rule—using semicolons `;` or commas `,` to put multiple statements on one line is forbidden.** For example, `out = from-i64(v), out = from-u64(v)` is incorrect and should be split across multiple lines.

---

## Modules

- [Basic Types](basic-types)
- [Core Library](core-library)
- [Operating System and Files](os-and-files)
- [Time and Date](time-and-date)
- [Logging](logging)
- [Data Structures](data-structures)
- [Database](database)
- [Encoding](encoding)
- [Archives](archives)
- [Cryptography and Hashing](crypto)
- [Data Exchange](data-exchange)
- [Async](async)
- [Global](global)
- [Others](others)
- [Module Overview](module-overview)
