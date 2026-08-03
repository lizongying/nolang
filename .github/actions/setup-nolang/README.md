# setup-nolang

A reusable GitHub Action (composite) that downloads a **nolang** release — the
`no` compiler binary for the current runner — plus the matching standard-library
source, then puts `no` on `PATH` and points `NOLANG_STD_SRC` / `NOLANG_SRC` at
the std lib so `no build` works.

## Why both a binary and the std source?

The `no` release binary does **not** bundle the standard library. `no build`
resolves `src/std` from `$NOLANG_STD_SRC` (or a path relative to the binary, or
`~/no/src/std`). This action downloads the source tarball for the matching tag
and exports the correct `NOLANG_STD_SRC`, so downstream builds are reproducible
and version-pinned.

## Inputs

| Input    | Default             | Description |
|----------|---------------------|-------------|
| `version`| `latest`            | `latest` or an explicit version like `1.2.3` (leading `v` optional). |
| `repo`   | `lizongying/nolang` | Owner/name of the nolang release repo. |
| `token`  | `${{ github.token }}` | Token for the releases API (private repos / rate limits). |

## Outputs

| Output       | Description |
|--------------|-------------|
| `version`    | Resolved version without the leading `v`. |
| `nolang-path`| Absolute path to the downloaded `no` binary. |
| `std-src`    | Absolute path to the downloaded `src/std` directory. |

## Usage

```yaml
- uses: ./.github/actions/setup-nolang
  with:
    version: "1.2.3"
- run: no build -o app main.no
```

Used internally by [`build-nolang`](../build-nolang/README.md).
