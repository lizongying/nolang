---
sidebar_position: 9
---

# Calling Nolang From Host Languages
> **This is a compiler-native feature (`#{cabi}` v1, scalars only).** `no build --shared` turns top-level functions marked `#{cabi='symbol'}` into stable C ABI symbols and generates the matching C header next to the shared library. No hand-written shim, no LLVM IR post-processing, no reachability anchors. `#{c}` (Nolang calls C), `@` (exports between Nolang packages) and `#{cabi}` (a host calls Nolang) are three different directions.


## Scope And Limitations

This page covers seven host languages: C, C++, Rust, Go, Java, Python and C#. Every example uses only its own toolchain and standard library — direct linking, cgo, JNI, `ctypes`, `DllImport` — with no JNA, cffi, NuGet or Maven dependency. The demo uses only `i64` / `f64` scalars; `str`, `vec`, `?T`, structs and other types that need a lifecycle ABI are rejected with a hard compiler error in v1. The full runnable code lives in `example/host-interop/`, where `make verify` builds, runs and diffs the pinned output of all seven.

Verified on macOS arm64 with clang 22, Apple g++, rustc 1.98, Go 1.27, OpenJDK 21, Python 3.13 and .NET 9. On Linux, replace `.dylib` with `.so`, `DYLD_LIBRARY_PATH` with `LD_LIBRARY_PATH`, and `@loader_path` with `$ORIGIN`; on Windows use `.dll`.


## How It Works: From `#{cabi}` To A Shared Library

```text
main.no  --(#{cabi='nolang_add'})-->  HIR annotation
   |  LowerHIR: export = extra reachability root (no comptime folding)
   v
MIR Module.CABIExports
   |  codegen: normal-return wrapper + generated C header
   v
no build --shared  (clang -shared -fPIC, no C main entry)
   -> dist/libnolang.dylib | .so | .dll  +  dist/libnolang.h
   -> C / C++ / Rust / Go(cgo) / Python(ctypes) / C#(P/Invoke)
   -> Java -> jni/nolang_jni.c -> libnolangjni -> -lnolang
```


### The Export Annotation And Reachability

Put `#{cabi='nolang_add'}` before a top-level function definition to choose its public C symbol; with a bare `#{cabi}` the compiler derives `nolang_<sanitized-name>`. A symbol must be a valid C identifier, must be unique, and may not claim `main` or `_nolang_main`. Methods, external declarations and functions whose result count is not exactly one produce a fatal `cabi` diagnostic.

`LowerHIR` normally expands only functions reachable from the synthesized `main`; unreferenced functions never reach the IR, and top-level calls with all-constant arguments can additionally be folded away at comptime. The compiler now adds every `#{cabi}` function as an extra reachability root and marks it non-comptime-pure, so the demo `main.no` can consist of three function definitions alone — no anchors or fake calls.


### Wrappers And The Generated Header

Nolang's internal call ABI is `define void @fn(args..., T* out)`: inputs by value, the result written through a caller-supplied out-pointer, and an internal symbol name that may gain a collision suffix. Neither is usable from C. The compiler emits a wrapper with a real return value for each export:

```llvm
; #{cabi} export of nl-add
define i64 @nolang_add(i64 %a0, i64 %a1) {
entry:
  %cabi.r = alloca i64
  call void @nl_add(i64 %a0, i64 %a1, i64* %cabi.r)
  %cabi.v = load i64, i64* %cabi.r
  ret i64 %cabi.v
}
```

The wrapper signature and the header are rendered from the SAME `cabiSignature` result, so they cannot drift. The wrapper name is registered in `usedFname` first, preventing collisions with runtime helpers or other exports.


### Shared Library Build Mode

`--shared` makes `no build` link the LLVM IR into a dynamic library with `clang -shared -fPIC` (a single `-x ir` pass, so PIC applies to codegen as well as linking) and disables the LLVM C API path. An automatic output path gets the platform suffix `.dylib` / `.so` / `.dll`; an explicit `-o` is used verbatim. `--header` overrides the header path, which defaults to the output path with its extension replaced by `.h`. A build with no `#{cabi}` export fails loudly instead of producing a library with no API.

- `--shared` is incompatible with `--js` / `--wasm-direct` and with a WASI target (LLVM backend only).
- A shared library omits the synthesized `define i32 @main` so it cannot interpose on the host executable's `main`; the `@nolang_argc` / `@nolang_argv` globals needed by `os.args` are still emitted.
- Executable builds are unaffected: with no `#{cabi}` export there is no wrapper and no header, and the IR is unchanged.


## Supported Types (v1 Scalars)

v1 accepts exactly these five by-value scalars. The C column is the declaration type written into the generated header.

| Nolang | LLVM IR | C header | wrapper |
| --- | --- | --- | --- |
| `i64` | `i64` | `int64_t` | `i64` |
| `i32` | `i32` | `int32_t` | `i32` |
| `i8` | `i8` | `int8_t` | `i8` |
| `bool` | `i1` | `uint8_t` | `i8` (`trunc` / `zext`) |
| `f64` | `double` | `double` | `double` |

`bool` is `i1` in LLVM, but C has no portable one-bit type, so it is exposed as `uint8_t` with `trunc` / `zext` at the wrapper boundary. `i128`, `str` / `txt`, `vec` / `[]T`, `map`, `?T`, structs, pointers, fixed arrays and any heap-owning value are rejected by `cabiTypeOf`; the error names the offending parameter or result type.


## Directory Layout

```text
example/host-interop/
├── main.no                  # three #{cabi} functions
├── package.noml
├── Makefile                 # lib / jni / c / cpp / rust / go / csharp / verify
├── expected.txt             # pinned output of all seven hosts
├── c/main.c
├── cpp/main.cpp
├── rust/main.rs
├── go/main.go               # cgo
├── java/NolangDemo.java
├── jni/nolang_jni.c         # Java -> C ABI glue (the only adapter left)
├── python/main.py           # ctypes
├── csharp/Program.cs        # DllImport
└── dist/                    # generated: libnolang.* , libnolang.h , demos
                             # (ignored by .gitignore)
```

The old `shim/` directory (hand-written C adapter and header) and `abi_guard.py` (internal-ABI drift guard) are gone: the compiler now owns the exported symbols, the wrappers and the header.


## Step 1: The Nolang Demo Library

Three scalar functions and no top-level statements. The `#{cabi='...'}` value is the C function name the host sees.

```no
; Compiler-native C ABI export demo.
;
; `#{cabi='symbol'}` does three things in the compiler itself:
;   1. keeps the function alive as an extra LowerHIR reachability root, so it
;      is emitted even though no Nolang code calls it;
;   2. emits a stable, normal-return C ABI wrapper over the internal
;      `define void @fn(args..., T* out)` shape;
;   3. contributes a declaration to the generated C header.
;
; `no build --shared -o dist/libnolang.dylib main.no` then links the IR into a
; real dynamic library and writes dist/libnolang.h next to it.
;
; v1 of the export path is scalar-only. str / vec / ?T / structs need a
; lifecycle ABI (ownership + runtime init/drop) that a direct C call cannot
; provide yet, so the compiler rejects them with a hard error.

#{cabi='nolang_add'}
nl-add = (a i64, b i64) (r i64) {
    r = a + b
}

#{cabi='nolang_scale'}
nl-scale = (x f64, k f64) (r f64) {
    r = x * k
}

#{cabi='nolang_sum3'}
nl-sum3 = (a i64, b i64, c i64) (r i64) {
    r = a + b + c
}
```

```noml
{
  name: 'host-interop'
  description: 'Calling Nolang from seven host languages via compiler-native C ABI exports'
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


## Step 2: `no build --shared`

Flags must come BEFORE the file (`-o OUT FILE`). This one step produces both the dynamic library and the header:

```bash
cd example/host-interop
mkdir -p dist
no build --shared -o dist/libnolang.dylib main.no
# [shared] dist/libnolang.dylib + dist/libnolang.h (3 exports)
# Build successful: dist/libnolang.dylib
```

> This requires a `no` that includes `#{cabi}` / `--shared`. If the repository's `bin/no` is older, the Makefile `lib` target reports it clearly; use `make verify NO=/path/to/no` to point at a freshly built compiler instead of overwriting the shared `bin/no`.

The generated header, ready to `#include`:

```c
/* Generated by `no build --shared` — do not edit. */
#ifndef NOLANG_CABI_H
#define NOLANG_CABI_H

#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

int64_t nolang_add(int64_t a0, int64_t a1);
double nolang_scale(double a0, double a1);
int64_t nolang_sum3(int64_t a0, int64_t a1, int64_t a2);

#ifdef __cplusplus
}
#endif

#endif /* NOLANG_CABI_H */
```

The Makefile `lib` target is exactly that step, plus an `nm -g` assertion that the three stable symbols really are exported:

```make
lib:
	mkdir -p dist
	@$(NO) build --help 2>&1 | grep -q -- '--shared' || { \
	  echo "FAIL: $(NO) has no --shared support. Rebuild the compiler, or pass NO=/path/to/no to make."; \
	  exit 1; }
	$(NO) build --shared -o dist/$(LIB) main.no
	@test -f dist/$(HEADER) || { echo "FAIL: generated header dist/$(HEADER) missing"; exit 1; }
	@if [ "$(UNAME)" = Darwin ]; then codesign -s - --force dist/$(LIB) >/dev/null 2>&1; fi
	@count=$$(nm -g dist/$(LIB) | grep -cE 'nolang_(add|scale|sum3)$$' || true); \
	 if [ "$$count" -ne 3 ]; then echo "FAIL: expected 3 exported symbols, got $$count"; exit 1; fi; \
	 echo "[lib] OK: 3 stable symbols exported from dist/$(LIB)"
```

| Flag | Effect |
| --- | --- |
| `--shared` | Build a dynamic library with `-shared -fPIC`; requires at least one `#{cabi}` export |
| `-o PATH` | Output path; also determines the default header name |
| `--header PATH` | Override the header output path (default: `<output-without-extension>.h`) |


## Examples For Seven Host Languages

| Language | Binding |
| --- | --- |
| C / C++ | Direct `#include "libnolang.h"` plus `-lnolang` |
| Rust | `#[link(name = "nolang")] unsafe extern "C"` |
| Go | cgo `#cgo CFLAGS` / `#cgo LDFLAGS` |
| Java | JNI (JDK 21; not the preview Panama FFM) |
| Python | stdlib `ctypes.CDLL(absolute_path)` |
| C# | `DllImport` + `NativeLibrary.SetDllImportResolver` |


### C

```c
#include <stdio.h>

#include "libnolang.h"

int main(void) {
    printf("c %lld %.1f %lld\n",
           (long long)nolang_add(20, 22),
           nolang_scale(2.5, 4.0),
           (long long)nolang_sum3(10, 20, 12));
    return 0;
}
```

```bash
clang -O2 -Idist -o dist/c_demo c/main.c -Ldist -lnolang -Wl,-rpath,@loader_path
```


### C++

```cpp
#include <cstdio>

#include "libnolang.h"

int main() {
    std::printf("cpp %lld %.1f %lld\n",
                static_cast<long long>(nolang_add(20, 22)),
                nolang_scale(2.5, 4.0),
                static_cast<long long>(nolang_sum3(10, 20, 12)));
    return 0;
}
```

```bash
g++ -std=c++17 -O2 -Idist -o dist/cpp_demo cpp/main.cpp -Ldist -lnolang -Wl,-rpath,@loader_path
```


### Rust

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
rustc -O -L native=$PWD/dist -C link-arg=-Wl,-rpath,$PWD/dist -o dist/rust_demo rust/main.rs
```


### Go (cgo)

cgo includes the generated header directly; `${SRCDIR}` keeps the path independent of the working directory.

```go
package main

/*
#cgo CFLAGS: -I${SRCDIR}/../dist
#cgo LDFLAGS: -L${SRCDIR}/../dist -lnolang -Wl,-rpath,${SRCDIR}/../dist
#include "libnolang.h"
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
```


### Java (JNI)

Java cannot call a C ABI directly, so it still needs one JNI glue layer; the other six languages need no adapter at all. `javac -h` generates `NolangDemo.h`, and the JNI library links back to `-lnolang`.

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
#include "libnolang.h"

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
clang -O2 -fPIC $JNI_INCLUDES -Idist -c jni/nolang_jni.c -o dist/nolang_jni.o
clang -shared -fPIC -Wl,-rpath,$PWD/dist -o dist/libnolangjni.dylib \
      dist/nolang_jni.o -Ldist -lnolang
java -Djava.library.path=dist -cp dist/classes NolangDemo
```


### Python (ctypes)

Pin `argtypes` / `restype` explicitly so 64-bit integers are not truncated to a C `int`.

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

A macOS hardened runtime may ignore `DYLD_LIBRARY_PATH`, so pass an absolute path via `NOLANG_LIB` and resolve it with `SetDllImportResolver`.

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

```xml
<Project Sdk="Microsoft.NET.Sdk">
  <PropertyGroup>
    <OutputType>Exe</OutputType>
    <TargetFramework>net9.0</TargetFramework>
    <ImplicitUsings>disable</ImplicitUsings>
    <Nullable>enable</Nullable>
    <AssemblyName>host-interop</AssemblyName>
  </PropertyGroup>
</Project>
```

```bash
cd csharp && dotnet build -c Release -o ../dist/csharp_out -v q
NOLANG_LIB=$PWD/dist/libnolang.dylib dotnet dist/csharp_out/host-interop.dll
```


## Verification

Library search paths: macOS uses `DYLD_LIBRARY_PATH` / `@rpath` / `@loader_path`, Linux uses `LD_LIBRARY_PATH` / `$ORIGIN`; Java additionally needs `-Djava.library.path`; .NET under a hardened runtime uses `NOLANG_LIB` for an absolute path. `make verify` builds the library and all seven host programs, collects their output in `dist/actual.txt`, and diffs it against `expected.txt`.

```bash
cd example/host-interop
make verify
# ALL SEVEN HOST LANGUAGES PASSED
```

```text
c 42 10.0 42
cpp 42 10.0 42
rust 42 10.0 42
go 42 10.0 42
java 42 10.0 42
python 42 10.0 42
csharp 42 10.0 42
```


## Limitations, Risks And Mitigations

| Risk / limitation | Current handling |
| --- | --- |
| Internal ABI (out-pointer, sanitized symbols) is not C-callable | The compiler emits normal-return wrappers; hosts depend only on `#{cabi}` symbols and the generated header |
| Unreferenced functions dropped by reachability or folded at comptime | Exports are extra `LowerHIR` roots and are marked non-foldable; no more `getpid()` anchors |
| A shared library exporting `main` collides with the host | `--shared` omits the synthesized `define i32 @main` |
| Nolang internal/runtime globals are still exported from the shared library | v1 does not narrow symbol visibility: besides the three `#{cabi}` symbols, `nm -g` also shows `__nolang_main`, `_nolang_rc_*`, `_nolang_async_*` and similar. Hosts must depend only on declarations from the generated header; hiding internals needs a future codegen change that marks them `hidden` for shared builds |
| Ownership and runtime init/cleanup for `str` / `vec` / `?T` | Explicitly rejected in v1; needs a lifecycle ABI (init / shutdown / borrow / free) |
| Invalid or colliding export symbol names | Valid C identifier, duplicate, reserved `main` / `_nolang_main`, and LLVM symbol collisions are checked; any conflict is a hard error |
| Header and wrapper types drifting apart | Both are rendered from the same `cabiSignature` result |
| Java needs JNI | One thin JNI library remains; the other six languages have no adapter |


## Related Documentation

- [FFI: Nolang calls C](./syntax) — `#{c}`, the opposite direction
- [Exports and visibility](./export) — `@` exports between Nolang packages only
- [Build and run](../usage) — `no build` flags and `--shared`
- [Example README](https://github.com/lizongying/no/tree/main/example/host-interop) — the full `make verify` flow
