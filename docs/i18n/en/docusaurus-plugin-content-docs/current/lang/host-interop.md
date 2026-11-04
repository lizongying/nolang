---
sidebar_position: 9
---

# Calling Nolang From Host Languages

> **This is a workaround, not a supported compiler feature.** `no build` currently emits executables only; there is no `-shared`, `-fPIC`, `-c`, or header generation. `#{c}` declares an external C function for *Nolang to call*, and `@` exports symbols between Nolang packages — neither is a C ABI export. `docs/design/nolang-native-backend.md` also lists dynamic-library output as a non-goal. This page shows how to link the LLVM IR that `-v` writes into a shared library **without changing the compiler**.

## Scope And Limitations

This page covers seven host languages: C, C++, Rust, Go, Java, Python, and C#. Every example uses only its own toolchain and standard library — direct linking, cgo, JNI, `ctypes`, `DllImport` — with no JNA, cffi, NuGet, or Maven dependency. The demo uses only `i64` / `f64` scalars; heap-owning types such as `str`, `vec`, and `?T` are out of scope. The full runnable code lives in `example/host-interop/`, where `make verify` builds, runs, and diffs the pinned output of all seven.

Verified on macOS arm64 with clang 22, Apple g++, rustc 1.98, Go 1.27, OpenJDK 21, Python 3.13, .NET 9, and `no v0.4.17`. On Linux, replace `.dylib` with `.so`, `DYLD_LIBRARY_PATH` with `LD_LIBRARY_PATH`, and `@loader_path` with `$ORIGIN`.

## How It Works: From IR To Shared Library To C Shim

Alongside the executable, `no build -v` writes the **raw** LLVM IR to `<output>.ll`. We ignore the executable, compile the `.ll` into an object with `clang -fPIC -c`, and link it with a hand-written C shim into a shared library. The shim is the only stable surface we promise.

```text
main.no --(no build -v)--> dist/libnolang.ll
                              | abi_guard.py checks 3 x define void @nl_*(...)
        clang -x ir -fPIC -c --> dist/nolang.o
shim/nolang_shim.c   -fPIC -c --> dist/nolang_shim.o
        clang -shared          --> dist/libnolang.dylib | .so
                                     ^
  C / C++ / Rust / Go(cgo) / Python(ctypes) / C#(P/Invoke) call the stable API
  Java -> jni/nolang_jni.c -> libnolangjni -> -lnolang -> the same stable API
```

### The Internal ABI Is Unstable

`no build` emits every user function as `define void @<name>(inputs by value, result out-pointer)`. `sanitize()` turns `-` into `_` and may append a collision suffix. The signatures measured by the probe are:

```llvm
define void @nl_add(i64 %p0, i64 %p1, i64* %p2)
define void @nl_scale(double %p0, double %p1, double* %p2)
define void @nl_sum3(i64 %p0, i64 %p1, i64 %p2, i64* %p3)
```

This is not a public ABI: every function returns `void` and writes results through a caller-supplied pointer, and struct layouts such as `%str-long` are implementation details. `abi_guard.py` therefore re-parses the `.ll` on every build and pins these three signatures, failing loudly on any drift instead of letting hosts silently call the wrong function.

### The Synthesized `main` Entry

`LowerHIR` lowers only functions reachable from the synthesized `main`, and top-level calls whose arguments are all constants get erased by comptime folding. `main.no` therefore declares libc's `getpid` with `#{c}` to obtain a **runtime** value `seed`, and all three anchors consume it so none can be folded away. The anchors live in `_nolang_main`, which no host ever calls.

`define i32 @main` is always synthesized. Inside a shared library it is just an ordinary exported symbol; dyld / ld.so never treats it as an entry point, so leaving it alone is safe. Because hosts never run `_nolang_main`, the demo also avoids module-level heap variables, strings, and vectors, and so needs neither the entry-point initialization nor its cleanup epilogue.

## Directory Layout

Everything lives under `example/host-interop/`; `dist/` is generated and gitignored, and the root `workspace.noml` does not need to register this package.

```text
example/host-interop/
├── package.noml          # Nolang package descriptor
├── main.no               # 3 scalar functions + reachability anchors
├── abi_guard.py          # parse .ll, fail loudly on ABI drift
├── Makefile              # make verify
├── expected.txt          # pinned output, 7 lines
├── shim/nolang.h         # stable public C API
├── shim/nolang_shim.c    # adapts the internal by-reference ABI
├── jni/nolang_jni.c      # JNI glue
├── c/main.c   cpp/main.cpp   rust/main.rs
├── go/go.mod  go/main.go     java/NolangDemo.java
├── python/main.py        csharp/Program.cs  csharp/host-interop.csproj
└── dist/                 # generated, gitignored
```

## Step 1: The Nolang Demo Library

Three scalar functions plus three reachability anchors. The `#{c}` declaration of `getpid` is provided by libc solely so the anchors get a value that cannot be folded.

```no
; Demo library for host-language interop via a hand-written C shim.
; Only i64/f64 scalars: no heap allocation, no module-level heap globals,
; and no dependency on the synthesized main's init/cleanup epilogue.

; getpid supplies a RUNTIME value for the reachability anchors below.
; LowerHIR lowers only functions reachable from the synthesized main, and
; comptime folding erases calls whose arguments are all constants.
#{c}
_getpid = () (pid i32)

nl-add = (a i64, b i64) (r i64) {
    r = a + b
}

nl-scale = (x f64, k f64) (r f64) {
    r = x * k
}

nl-sum3 = (a i64, b i64, c i64) (r i64) {
    r = a + b + c
}

; Reachability anchors. These live in _nolang_main, which the host never
; calls; they exist only to keep the three functions in the emitted LLVM IR.
; Every anchor consumes `seed`, so none of them can be folded away.
seed = _getpid()
a0 = nl-add(seed, 2)
s0 = nl-scale(1.5, seed)
t0 = nl-sum3(seed, 2, 3)
```

```noml
{
  name: 'host-interop'
  description: 'Calling Nolang from seven host languages via a shared-library C shim'
  license: 'Apache-2.0'
  author: 'lizongying'
  keywords: []
  dependencies: []
  compiler: {
    link-libs: []
  }
  output: '/dist'
  ignore: []
}
```

## Step 2: Build The Shared Library And Shim

Note that `-v` is a global flag, but `-o` must come **before** the filename: otherwise Go's `flag` package silently produces nothing while still returning 0. `-O0` on the `.ll → .o` step keeps the object signatures exactly as the guard just read them.

```bash
cd example/host-interop
NO=../../bin/no
mkdir -p dist

# ALL flags must come BEFORE the file: -o OUT FILE
$NO build -v -o dist/libnolang main.no
python3 abi_guard.py dist/libnolang.ll

clang -x ir -O0 -fPIC -c dist/libnolang.ll -o dist/nolang.o
clang -O2 -fPIC -Ishim -c shim/nolang_shim.c -o dist/nolang_shim.o
clang -shared -fPIC -Wl,-install_name,@rpath/libnolang.dylib \
      -o dist/libnolang.dylib dist/nolang.o dist/nolang_shim.o
# Linux: use -o dist/libnolang.so and -Wl,-rpath,'$ORIGIN' instead
nm -g dist/libnolang.dylib | grep nolang_
```

The stable public header:

```c
#ifndef NOLANG_H
#define NOLANG_H

#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

#if defined(_WIN32)
#  define NOLANG_API __declspec(dllexport)
#elif defined(__GNUC__)
#  define NOLANG_API __attribute__((visibility("default")))
#else
#  define NOLANG_API
#endif

/* Stable public API. Never expose the internal nl_* symbols to host code. */
NOLANG_API int64_t nolang_add(int64_t a, int64_t b);
NOLANG_API double  nolang_scale(double x, double k);
NOLANG_API int64_t nolang_sum3(int64_t a, int64_t b, int64_t c);

#ifdef __cplusplus
}
#endif

#endif /* NOLANG_H */
```

The shim wraps the internal out-pointer calls into real C return values:

```c
#include <stdint.h>

#include "nolang.h"

/*
 * Internal Nolang symbols. `no build` emits every user function as
 * `define void @<name>(<inputs by value>, <result out-pointer>)`.
 * These signatures are NOT a public ABI: abi_guard.py re-checks them against
 * dist/libnolang.ll on every build and fails loudly if they ever change.
 * The `nl_` prefix avoids collisions with the compiler runtime prelude.
 */
extern void nl_add(int64_t a, int64_t b, int64_t *out);
extern void nl_scale(double x, double k, double *out);
extern void nl_sum3(int64_t a, int64_t b, int64_t c, int64_t *out);

int64_t nolang_add(int64_t a, int64_t b) {
    int64_t out = 0;
    nl_add(a, b, &out);
    return out;
}

double nolang_scale(double x, double k) {
    double out = 0.0;
    nl_scale(x, k, &out);
    return out;
}

int64_t nolang_sum3(int64_t a, int64_t b, int64_t c) {
    int64_t out = 0;
    nl_sum3(a, b, c, &out);
    return out;
}
```

## Step 3: The ABI Guard

Every `make lib` runs this script first. It parses `define void @nl_*(...)` from the `.ll` and compares each signature against `EXPECTED`; a missing definition, `internal` linkage, opaque `ptr`, or a different type sequence exits non-zero and prints the actual signatures. **Do not patch the shim blindly**: re-probe, then update `EXPECTED` and the shim together.

```python
#!/usr/bin/env python3
"""Fail loudly when Nolang's internal ABI no longer matches the C shim.

`no build` emits user functions with an INTERNAL by-reference ABI: inputs are
passed by value, results are written through a caller-supplied out-pointer.
That ABI is not a public contract, so this script pins the exact signatures
that shim/nolang_shim.c declares. Run it after every `no build -v` and before
compiling the shim.

Exit 1 => do NOT patch the shim blindly: re-read the .ll, re-probe, and update
both EXPECTED below and shim/nolang_shim.c together.
"""

import re
import sys
from pathlib import Path

# symbol -> exact LLVM parameter type sequence the shim is written against.
EXPECTED = {
    "nl_add": ["i64", "i64", "i64*"],
    "nl_scale": ["double", "double", "double*"],
    "nl_sum3": ["i64", "i64", "i64", "i64*"],
}

DEFINE = re.compile(
    r"^define\s+(?P<linkage>(?:internal|private|linkonce|weak)\s+)?"
    r"void\s+@(?P<name>[A-Za-z0-9_.$]+)\s*\((?P<params>.*?)\)\s*\{",
    re.M,
)
ATTRIBUTE = re.compile(
    r"\b(?:nocapture|readonly|writeonly|nonnull|noalias|noundef|signext|"
    r"zeroext|align\s+\d+|dereferenceable(?:_or_null)?\([^)]*\)|"
    r"captures\([^)]*\)|initializes\([^)]*\)|byval\([^)]*\)|sret\([^)]*\))",
)


def type_of(param: str) -> str:
    """Extract the LLVM type from one parameter declaration."""
    param = param.strip()
    # Everything before the first " %name" token is the type (plus attrs).
    match = re.match(r"^(.*?)\s+%\S", param)
    ty = match.group(1) if match else param
    ty = ATTRIBUTE.sub("", ty)
    return re.sub(r"\s+", " ", ty).strip()


def main() -> int:
    if len(sys.argv) not in (2, 3):
        print("usage: abi_guard.py <libnolang.ll> [signature-report.txt]", file=sys.stderr)
        return 2

    ll_path = Path(sys.argv[1])
    if not ll_path.exists():
        print(f"[abi-guard] FAIL: {ll_path} does not exist", file=sys.stderr)
        return 1
    text = ll_path.read_text()

    found = {}
    for match in DEFINE.finditer(text):
        name = match.group("name")
        types = [type_of(p) for p in match.group("params").split(",") if p.strip()]
        found.setdefault(name, []).append((match.group("linkage") or "", types))

    problems = []
    for name, want in EXPECTED.items():
        entries = found.get(name, [])
        if not entries:
            problems.append(
                f"@{name} is MISSING. Usually this means (a) the top-level "
                f"reachability anchors were folded away by comptime, or "
                f"(b) the symbol gained a collision suffix / was renamed."
            )
            continue
        if len(entries) != 1:
            problems.append(f"@{name} is defined {len(entries)} times")
            continue
        linkage, got = entries[0]
        if linkage:
            problems.append(f"@{name} is '{linkage.strip()}' and cannot be linked from C")
        if any(t == "ptr" or "ptr" in t for t in got):
            problems.append(
                f"@{name} uses opaque `ptr`; the compiler switched pointer style. "
                f"Re-probe and update EXPECTED, or use the unoptimized .ll."
            )
        if got != want:
            problems.append(
                f"@{name} ABI DRIFT\n      actual   {got}\n      expected {want}"
            )

    has_main = bool(re.search(r"^define\s+i32\s+@main\s*\(", text, re.M))
    triple = re.search(r'^target triple\s*=\s*"([^"]+)"', text, re.M)

    if problems:
        print("=== NOLANG ABI GUARD FAILED ===", file=sys.stderr)
        for problem in problems:
            print(f"  - {problem}", file=sys.stderr)
        print(f"  define i32 @main present: {has_main}", file=sys.stderr)
        print(
            "  Inspect with: grep -nE '^define .*@nl_' " + str(ll_path),
            file=sys.stderr,
        )
        return 1

    print(
        f"[abi-guard] ok | @main present={has_main} | "
        f"triple={triple.group(1) if triple else 'n/a'}"
    )
    report_lines = []
    for name in EXPECTED:
        got = found[name][0][1]
        print(f"[abi-guard]   define void @{name}({', '.join(got)})")
        report_lines.append(f"{name}: {' '.join(got)}")
    if len(sys.argv) == 3:
        Path(sys.argv[2]).write_text("\n".join(report_lines) + "\n")
    return 0


if __name__ == "__main__":
    sys.exit(main())
```

## Examples For Seven Host Languages

Each example prints one line, `<lang> 42 10.0 42` (`20+22`, `2.5*4.0`, `10+20+12`). Below are the sources and their build / run commands; `make verify` chains them and diffs the output.

### C

Include the header and link against the shared library.

```c
#include <stdio.h>

#include "nolang.h"

int main(void) {
    printf("c %lld %.1f %lld\n",
           (long long)nolang_add(20, 22),
           nolang_scale(2.5, 4.0),
           (long long)nolang_sum3(10, 20, 12));
    return 0;
}
```

```bash
clang -O2 -Ishim -o dist/c_demo c/main.c -Ldist -lnolang -Wl,-rpath,@loader_path
./dist/c_demo
```

### C++

The header carries its own `extern "C"` guard, so nothing else is needed.

```cpp
#include <cstdio>

#include "nolang.h"

int main() {
    std::printf("cpp %lld %.1f %lld\n",
                static_cast<long long>(nolang_add(20, 22)),
                nolang_scale(2.5, 4.0),
                static_cast<long long>(nolang_sum3(10, 20, 12)));
    return 0;
}
```

```bash
g++ -std=c++17 -O2 -Ishim -o dist/cpp_demo cpp/main.cpp -Ldist -lnolang -Wl,-rpath,@loader_path
./dist/cpp_demo
```

### Rust

Declare the functions in an `unsafe extern "C"` block and name the library with `#[link]`.

```rust
#[link(name = "nolang")]
unsafe extern "C" {
    fn nolang_add(a: i64, b: i64) -> i64;
    fn nolang_scale(x: f64, k: f64) -> f64;
    fn nolang_sum3(a: i64, b: i64, c: i64) -> i64;
}

fn main() {
    unsafe {
        println!(
            "rust {} {:.1} {}",
            nolang_add(20, 22),
            nolang_scale(2.5, 4.0),
            nolang_sum3(10, 20, 12)
        );
    }
}
```

```bash
rustc -O -L native="$(pwd)/dist" -C link-arg=-Wl,-rpath,"$(pwd)/dist" \
      -o dist/rust_demo rust/main.rs
./dist/rust_demo
```

### Go (cgo)

cgo uses `${SRCDIR}` for module-relative paths; `go.mod` only needs `module host-interop-go` and `go 1.27`.

```go
package main

/*
#cgo CFLAGS: -I${SRCDIR}/../shim
#cgo LDFLAGS: -L${SRCDIR}/../dist -lnolang -Wl,-rpath,${SRCDIR}/../dist
#include "nolang.h"
*/
import "C"

import "fmt"

func main() {
	fmt.Printf("go %d %.1f %d\n",
		int64(C.nolang_add(C.int64_t(20), C.int64_t(22))),
		float64(C.nolang_scale(C.double(2.5), C.double(4.0))),
		int64(C.nolang_sum3(C.int64_t(10), C.int64_t(20), C.int64_t(12))))
}
```

```bash
cd go && CGO_ENABLED=1 CC=clang go build -o ../dist/go_demo .
../dist/go_demo
```

### Java (JNI)

Panama FFM is still preview on JDK 21, so we use dependency-free JNI: `javac -h` generates the header and the C glue links into a separate `libnolangjni`.

```java
import java.util.Locale;

public class NolangDemo {
    static {
        System.loadLibrary("nolangjni");
    }

    private static native long add(long a, long b);

    private static native double scale(double x, double k);

    private static native long sum3(long a, long b, long c);

    public static void main(String[] args) {
        System.out.printf(Locale.ROOT, "java %d %.1f %d%n",
                add(20, 22), scale(2.5, 4.0), sum3(10, 20, 12));
    }
}
```

```c
#include <jni.h>
#include <stdint.h>

#include "NolangDemo.h"
#include "nolang.h"

JNIEXPORT jlong JNICALL Java_NolangDemo_add
        (JNIEnv *env, jclass cls, jlong a, jlong b) {
    (void)env;
    (void)cls;
    return (jlong)nolang_add((int64_t)a, (int64_t)b);
}

JNIEXPORT jdouble JNICALL Java_NolangDemo_scale
        (JNIEnv *env, jclass cls, jdouble x, jdouble k) {
    (void)env;
    (void)cls;
    return (jdouble)nolang_scale((double)x, (double)k);
}

JNIEXPORT jlong JNICALL Java_NolangDemo_sum3
        (JNIEnv *env, jclass cls, jlong a, jlong b, jlong c) {
    (void)env;
    (void)cls;
    return (jlong)nolang_sum3((int64_t)a, (int64_t)b, (int64_t)c);
}
```

```bash
javac -h dist -d dist/classes java/NolangDemo.java
clang -O2 -fPIC -I"$JAVA_HOME/include" -I"$JAVA_HOME/include/darwin" \
      -Ishim -Idist -c jni/nolang_jni.c -o dist/nolang_jni.o
clang -shared -fPIC -Wl,-rpath,"$(pwd)/dist" -o dist/libnolangjni.dylib \
      dist/nolang_jni.o -Ldist -lnolang
java -Djava.library.path=dist -cp dist/classes NolangDemo
```

### Python (ctypes)

Standard library only. Load by absolute path to avoid search-path differences, and declare `argtypes` / `restype` for each function.

```python
#!/usr/bin/env python3
"""Call the Nolang shared library through ctypes (stdlib only)."""

import ctypes
import pathlib
import sys

root = pathlib.Path(__file__).resolve().parents[1]
name = "libnolang.dylib" if sys.platform == "darwin" else "libnolang.so"
lib = ctypes.CDLL(str(root / "dist" / name))

lib.nolang_add.argtypes = [ctypes.c_int64, ctypes.c_int64]
lib.nolang_add.restype = ctypes.c_int64
lib.nolang_scale.argtypes = [ctypes.c_double, ctypes.c_double]
lib.nolang_scale.restype = ctypes.c_double
lib.nolang_sum3.argtypes = [ctypes.c_int64, ctypes.c_int64, ctypes.c_int64]
lib.nolang_sum3.restype = ctypes.c_int64

print(f"python {lib.nolang_add(20, 22)} "
      f"{lib.nolang_scale(2.5, 4.0):.1f} "
      f"{lib.nolang_sum3(10, 20, 12)}")
```

```bash
python3 python/main.py
```

### C# (P/Invoke)

`DllImport` plus `NativeLibrary.SetDllImportResolver`: the macOS hardened runtime may ignore `DYLD_LIBRARY_PATH`, so `NOLANG_LIB` supplies an absolute path.

```csharp
using System;
using System.Globalization;
using System.Runtime.InteropServices;

internal static class Program
{
    [DllImport("nolang")]
    private static extern long nolang_add(long a, long b);

    [DllImport("nolang")]
    private static extern double nolang_scale(double x, double k);

    [DllImport("nolang")]
    private static extern long nolang_sum3(long a, long b, long c);

    private static void Main()
    {
        // macOS hardened runtimes can ignore DYLD_LIBRARY_PATH, so resolve
        // the dylib by absolute path when NOLANG_LIB is provided.
        string? path = Environment.GetEnvironmentVariable("NOLANG_LIB");
        if (!string.IsNullOrEmpty(path))
        {
            NativeLibrary.SetDllImportResolver(typeof(Program).Assembly,
                (name, assembly, searchPath) =>
                    name == "nolang" ? NativeLibrary.Load(path) : IntPtr.Zero);
        }

        CultureInfo.CurrentCulture = CultureInfo.InvariantCulture;
        Console.WriteLine($"csharp {nolang_add(20, 22)} "
                          + $"{nolang_scale(2.5, 4.0):F1} "
                          + $"{nolang_sum3(10, 20, 12)}");
    }
}
```

```bash
dotnet build csharp/host-interop.csproj -c Release -o dist/csharp_out -v q
NOLANG_LIB="$(pwd)/dist/libnolang.dylib" dotnet dist/csharp_out/host-interop.dll
```

## Verification

The `verify` target builds the shared library, the JNI library, and all seven host programs, collects their output into `dist/actual.txt`, and diffs it against `expected.txt`. It also asserts the three exported symbols with `nm -g`, re-runs the guard, and checks for non-PIC relocations on Linux.

```bash
cd example/host-interop
make verify
# ALL SEVEN HOST LANGUAGES PASSED
```

`expected.txt`:

```text
c 42 10.0 42
cpp 42 10.0 42
rust 42 10.0 42
go 42 10.0 42
java 42 10.0 42
python 42 10.0 42
csharp 42 10.0 42
```

## Risks And Mitigations

| Risk | Mitigation |
| --- | --- |
| Unstable internal ABI (by value / by pointer, symbol renaming, collision suffixes) | `abi_guard.py` pins all three signatures and hard-fails on drift; the shim is the only stable surface |
| Unreferenced functions dropped by reachability, or anchors folded by comptime | `getpid()` supplies a runtime `seed` that all three anchors consume |
| Ownership and freeing of heap types such as `str` / `vec` | Demo uses only `i64` / `f64` scalars; everything else is a non-goal |
| Synthesized `@main` inside the shared library | Leave it: dyld / ld.so never treats it as an entry point; `nm -g` checks it informationally |
| macOS code signing / quarantine | `codesign -s - --force`; `xattr -d com.apple.quarantine` if needed |
| Non-PIC relocations on Linux | `-fPIC` on the step that compiles the `.ll`, plus a `readelf -r` assertion |
| `-o` placed after the filename | Go's `flag` silently produces nothing; always use `-o OUT FILE` |
| LLVM drops typed-pointer auto-upgrade | Switch to `dist/libnolang_opt.ll` (opaque `ptr`) and update the guard |
| Locale-dependent float output | Uniform `.1f` / `F1` plus `Locale.ROOT` / `InvariantCulture`, pinned by `expected.txt` |

## Related Documentation

- [The `#{c}` FFI annotation](./syntax) — the reverse direction, Nolang calling C
- [Export](./export) — `@` exports symbols between Nolang packages
- [Native packages: WASM and FFI](./js-backend) — `bind=ffi` / `bind=wasm`
- [Memory](./memory) — lifetime of module-level heap values
- [Full runnable example](https://github.com/lizongying/nolang/tree/main/example/host-interop)
