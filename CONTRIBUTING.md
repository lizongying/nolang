# Contributing to Nolang

Nolang is an open source project.

We appreciate your help!

## Filing issues

Sensitive security-related issues should be reported to [lizongying@msn.com](mailto:lizongying@msn.com).

Otherwise, when filing an issue, make sure to answer these five questions:

1. What version of Nolang are you using (`nolang version`)?
2. What operating system and processor architecture are you using?
3. What did you do?
4. What did you expect to see?
5. What did you see instead?

## Building from source

Always build with `make` — **never** `cd src && go build`:

```bash
make no      # build bin/no (runs `make gen` first to regenerate the std signature table)
make lsp     # build the LSP server
make         # build everything (no + lsp)
```

`make` runs `make gen` before compiling. `make gen` regenerates
`src/checker/stdsig_gen.go`, a **baked-in** signature table for the standard
library (`funcSigs` / `methodSigs` / `funcParams` / `methodParams` /
`structFields` / `aliases` / `structMod` / `enumVariants`). With it, every
`no build` skips the full std-parse PASS1 and starts much faster.

If you edit `src/std` and build **without** regenerating that table, the compiler
detects the mismatch and **refuses to build**, telling you to run `make no`. This
is intentional: a stale table used to silently fall back to parsing the entire
std library on every compile — slow and easy to miss. So after touching
`src/std`, always `make no` (it does `make gen` then builds).

The std-signature key is injected into the binary at build time via `-X`
(`make` derives it from the current `src/std` with the independent `genstdsig`
tool). Run the test suite with `make test` — it injects the same key into the
test binaries. A bare `go test` leaves the key empty and the compiler **hard-fails**,
by design: the only supported build path is `make`.
