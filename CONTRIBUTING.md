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
`structFields` / `aliases` / `structMod` / `enumVariants` / `embeddedStdModules`). With it, every
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

## Localization (i18n)

Every user-facing message (errors, warnings, lint findings) carries a stable
**8-character base36 ID** — the translation index, printed in the trailing
`[xxxxxxxx]` slot. The English text stays inline at the call site and is also the
fallback, so a missing translation can never produce an empty string, a bare ID,
or a failed build:

```go
Message: i18n.T("j7dzteja", "function '%s' expects %d argument(s), got %d", name, want, got)
return i18n.Errorf("kzsepkro", "loading module %s: %w", path, err)
```

Three entry points, one rule each:

| helper | use for | `%w` allowed |
| --- | --- | --- |
| `i18n.T(id, def, args...)` | formatted text | no |
| `i18n.P(id, def)` | text with no arguments | no |
| `i18n.Errorf(id, def, args...)` | anything that becomes an `error` | **yes** |
| `i18n.Translate(id, def, args...)` | forwarding helpers where the ID is a runtime value | no |

`i18n.Errorf` returns an `*i18n.Error`, so `Error()` renders `message [id]`. Only
**one** ID appears per error chain: a frame suppresses its own ID once an inner
frame is already rendering one, which is what keeps
`compilation error → parser errors → line 1, column 3: …` from printing three
IDs on one line. `i18n.IDOf(err)` returns the ID that is actually rendered.

With `--lang=en` (and on any English system) the output is byte-identical to the
pre-i18n compiler: `i18n.T` returns `fmt.Sprintf(def, args...)` without consulting
the catalog, and zero-argument messages skip `Sprintf` entirely (so a literal `%`
is never mangled into `%!(NOVERB)`).

### Adding a message

1. Pick an unused ID (or leave `"PLACEHOLDER"` and run `scripts/stamp_traceid.sh`).
   Bulk migrations derive the ID from the text itself (`scripts/` history), which
   makes "one ID, one text" true by construction.
2. Wrap the text with `i18n.T` / `i18n.P` / `i18n.Errorf`.
3. Run `make i18n` to regenerate `src/i18n/catalog/locales/en.noml`.
4. Add translations. Keys come from `en.noml`; never invent one. **Keep the format
   verbs in the same order** — `go test ./i18n/catalog/` rejects any translation
   whose verb signature differs from English (a reordered `%s` binds the wrong
   argument at runtime).

`scripts/merge_locale.py <tag> <dict.py>` merges a `TRANSLATIONS = {...}` dict into
a locale file, re-renders it sorted by key, and refuses to run on a key that is
not in `en.noml` (an orphan translation of a message that no longer exists).

### Multi-line `.noml` values

A `.noml` value is either a single-quoted string (escapes, no newlines) or a
**backtick raw block** — the same syntax as nolang source:

```
{
  'w308jsag': `
%s: validation failed:
%s
`
}
```

The rules match the language exactly: the opening backtick must be followed by a
newline, the closing backtick must be alone on its line (leading whitespace is
allowed), the two delimiter lines are not part of the value, and **nothing inside
is unescaped** — a `\n` there is a backslash and an `n`. `package.NomlQuoteValue`
emits this form automatically for any value containing a newline, so catalogs stay
readable instead of collapsing long messages into `\n` escapes. A value that
cannot round-trip through the raw form (a line that is itself the terminating
backtick) falls back to the quoted form.

### Doc comments (Markdown link)

A message written at a Go call site is translated by wrapping it. A **doc
comment** is not at a call site, so it ends with a Markdown link instead:

```nolang
; get: 獲取第 i 個參數
; 委派至 os.arg(i) builtin（讀取 main 的 argv[i]）；越界回傳 nil
; [get](#args.get)
get = (i i64) (val ?str) {
    val = os.arg(i)
}
```

Rules, all of them load-bearing:

- **The link is the last line of the comment, alone on it.** Everything above it
  is the block — one message, translated as one unit. Requiring the last line is
  what makes the block unambiguous: it is the only end of the run that touches the
  declaration, so the generator (raw text) and the LSP (parsed `CommentGroup`)
  read the same block without replicating the parser's doc-group merging.
- **Only `;` comments count.** `;;`, `;;block` and `//` are not eligible: a block
  comment's interior lines carry no `;` prefix in the raw source, so honouring
  them would make the two sides disagree about the block.
- **The target must be a `#fragment`.** `[docs](https://example.com)` is prose a
  human wrote for a human and must never be treated as a key; requiring `#`
  (a same-document anchor) makes external links immune by construction.
- **The anchor is the key.** `#args.get` is what a translator sees and what the
  catalog is keyed by — nothing is derived from the text, so editing the prose
  never orphans a translation. Anchors allow `[A-Za-z0-9._-]` and are global;
  prefix them with the module (`args.get`, not `get`) to keep them unique.
- **The label is decorative.** `[get]` is a hint for whoever reads the source;
  the whole link line is stripped from the hover either way. `[](#args.get)` is
  legal.
- **Hovering the link resolves it.** The link line is not dead syntax: hovering it
  shows the anchor and that entry's text in the active language, so a reader can
  check a translation without leaving the comment.

`make i18n` scans every `.no` file and writes `src/i18n/catalog/locales/en.doc.noml`
(a *separate* file from `en.noml`). That file holds **translations only**. Seeding
every anchor with its source text would put Chinese into the English catalog —
the exact failure `en.noml` was already fixed for — and would make the catalog
claim a translation nobody wrote. An anchor that is absent simply falls back to
the comment text, so an untranslated block degrades to the author's language.
A committed value is preserved verbatim even if the comment is reworded, because
the anchor rather than the text is the key (`catalog.PruneDocTranslations`).
Translations go in `locales/<tag>.doc.noml`; a key that no longer exists in any
`.no` source is rejected as an orphan.

`go run ./i18n/cmd/genen -doc-missing` lists the anchors still waiting for a
translation, so the remaining work is visible without diffing two files.

At runtime `i18n.Doc(key, text)` resolves the block — not `i18n.T`. It differs in
two ways on purpose: it consults the catalog under `en` as well (a comment is
written in the author's language, so English is a translation *target* there,
unlike a message whose inline default already is English), and it never runs
`Sprintf` (doc prose contains literal `%`, and there are no arguments). A
missing or malformed doc catalog is never fatal — it degrades to showing the
source text, so a cosmetic override cannot break `no build`.

### Selecting a language

`--lang=<tag>` selects the language (`no build --lang=zh-CN`, `no vet --lang=ja`).
Precedence is `--lang` > `NO_LANG` > `LC_ALL` > `LANG` > `en`, so a Chinese or
Japanese system locale is honoured without a flag (`zh_CN.UTF-8` → `zh-CN`).
Aliases resolve onto the catalog that actually exists: `zh`/`zh-Hans` → `zh-CN`,
`zh-Hant`/`zh-Hant-TW`/`zh-HK`/`zh-MO` → `zh-TW`. A project may drop
`locales/<tag>.noml` at its root to override the built-in catalogs (project-local
wins). Built-in catalogs live in `src/i18n/catalog/locales/` and are embedded with
`go:embed`.

The LSP has no `--lang` flag. It resolves the language at startup from the same
environment variables, then **overrides it from the client's
`initialize.locale`** — an editor running in English should not show Chinese
diagnostics just because the machine's `$LANG` is `zh_CN`. Precedence there is
`initialize.locale` > `NO_LANG` > `LC_ALL` > `LANG` > `en`; an empty locale means
"no opinion" and the startup resolution stands. Unknown tags warn and fall back
to `en`.

### Coverage

`en.noml` is the complete set (every call site). The shipped translations cover
the checker and the build/package wrappers; anything not yet translated falls
back to English at runtime, per message, with no failure.

Doc comments are tracked separately. Every documented std function or method
carries an anchor (2009 of them), so the LSP can translate any of them the
moment a translation lands. `en.doc.noml` and `zh-CN.doc.noml` cover, end to end:
`args`, `global`, `io`, `fmt`, `str`, `txt`, `json`, `regexp`, `time`, `vec`,
`arr`, `byte`, `char`, `number`, `int`, `num`, `float`, `math`, `os`, `fs`,
`process`, `env`, `set`, `heap`, `bufio`, `path`, `err`, `log`, `bool`, `async`,
`sort`, plus `collection`, `dom`, `yaml`, `toml`, `markdown`, `uuid`, `encoding`,
`bigint` and `magic` — 1186 of the 1995 anchors. `zh-TW`, `ja`, `ko`, `fr` and
`es` ship a starter set (the `args`/`global`/`env`/`bool`/`num`/`int` surface)
and fall back to the source comment for the rest. The `net`, `crypto` and
`archive` modules are anchored but not translated yet; run
`go run ./i18n/cmd/genen -doc-missing` (or `make i18n-missing`) for the exact
list.

`go test ./i18n/...` is the gate: it checks `en.noml` and `en.doc.noml`
freshness, key well-formedness, absence of orphans in both, and verb-signature
equality for every locale. A doc catalog is free to translate an anchor English
has not reached yet — orphans are checked against the source, not against `en`.
