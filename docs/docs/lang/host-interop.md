---
sidebar_position: 9
---

# 從宿主語言調用 Nolang

> **這是 workaround，不是編譯器支援的特性。** `no build` 目前只產出可執行檔，沒有 `-shared`、`-fPIC`、`-c` 或標頭檔生成；`#{c}` 是「Nolang 呼叫 C」的外部函數宣告，`@` 只是 Nolang 包之間的符號導出，兩者都不是 C ABI 導出。`docs/design/nolang-native-backend.md` 也把動態庫產出列為非目標。本文示範的是**不改編譯器**、把 `-v` 順便寫出的 LLVM IR 自行鏈成共享庫的做法。

## 適用範圍與限制

本文覆蓋 C、C++、Rust、Go、Java、Python、C# 七種宿主語言，全部只使用各自工具鏈與標準庫：直接鏈結、cgo、JNI、`ctypes`、`DllImport`，不需要 JNA、cffi、NuGet 或 Maven 依賴。示例只用 `i64`／`f64` 標量，不覆蓋 `str`、`vec`、`?T` 等持有堆記憶體的型別。完整可運行代碼在 `example/host-interop/`，`make verify` 會逐一種譯、執行並比對固定輸出。

已在 macOS arm64 實跑驗證：clang 22、Apple g++、rustc 1.98、Go 1.27、OpenJDK 21、Python 3.13、.NET 9，`no v0.4.17`。Linux 只需把 `.dylib` 換成 `.so`、`DYLD_LIBRARY_PATH` 換成 `LD_LIBRARY_PATH`、`@loader_path` 換成 `$ORIGIN`。

## 原理：IR → 共享庫 → C shim

`no build -v` 在產生可執行檔的同時，會把**原始** LLVM IR 寫到 `<輸出路徑>.ll`。我們忽略那個可執行檔，把 `.ll` 用 `clang -fPIC -c` 編成物件檔，再與一層手寫 C shim 一起鏈成共享庫。shim 是唯一對外承諾的穩定面。

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

### 內部 ABI 不穩定

`no build` 把使用者函數發射為 `define void @<name>(輸入按值, 結果 out-pointer)`，函式名稱由 `sanitize()` 把 `-` 轉成 `_`，衝突時還可能加後綴。本次探针得到的實際簽名是：

```llvm
define void @nl_add(i64 %p0, i64 %p1, i64* %p2)
define void @nl_scale(double %p0, double %p1, double* %p2)
define void @nl_sum3(i64 %p0, i64 %p1, i64 %p2, i64* %p3)
```

這不是公開 ABI：函式一律回傳 `void`，結果寫進呼叫方提供的指標；`%str-long` 等結構體配置也屬於編譯器實作細節。因此 `abi_guard.py` 每次構建都重新解析 `.ll` 並 pin 住這三條簽名，任何漂移都直接失敗，而不是讓宿主靜默呼叫錯誤的函數。

### 合成入口 `main`

`LowerHIR` 只從合成的 `main` 沿呼叫點展開，未被呼叫的函數根本不會出現在 `.ll`；而全常數的頂層呼叫又會被 comptime 折疊掉。所以 `main.no` 用 `#{c}` 宣告 libc 的 `getpid` 取得一個**運行期**值 `seed`，三個錨點都消費它，函數才會被保留。錨點位於 `_nolang_main`，宿主永遠不會呼叫它。

`define i32 @main` 總是會被合成。共享庫中的 `main` 只是普通導出符號，dyld／ld.so 不會把它當入口，保留即可。也正因為宿主不執行 `_nolang_main`，示例刻意不使用模組級堆變數、字串或向量，避開入口處的初始化與尾聲的清理邏輯。

## 目錄結構

所有檔案都在 `example/host-interop/` 下，`dist/` 是生成物且已被忽略；根目錄的 `workspace.noml` 不需要登記這個包。

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

## 第一步：Nolang 示例庫

三個純量函數，加上三個可達性錨點。`#{c}` 宣告的 `getpid` 由 libc 提供，只為了讓錨點拿到一個無法被折疊的值。

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

## 第二步：構建共享庫與 shim

注意 `-v` 是全域旗標，但 `-o` 必須放在檔名**之前**；反過來 Go 的 `flag` 會靜默不產出檔案而仍回傳 0。`-O0` 用在 `.ll → .o` 這一步，保證物件檔的簽名就是 guard 剛剛讀到的簽名。

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

穩定 API 的標頭檔：

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

shim 把內部的 out-pointer 呼叫包成真正的 C 返回值：

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

## 第三步：ABI 防護

每次 `make lib` 都會先跑這支腳本。它解析 `.ll` 中的 `define void @nl_*(...)`，逐一比對 `EXPECTED`，出現缺失、`internal` 連結、opaque `ptr` 或型別序列不同就結束並印出實際簽名。**不要盲改 shim**：先重新探针，再同時更新 `EXPECTED` 與 shim。

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

## 七種宿主語言示例

每個範例都印出一行 `<語言> 42 10.0 42`（`20+22`、`2.5*4.0`、`10+20+12`）。以下是各語言原始碼與對應的編譯／執行命令；把它們串起來並比對輸出的是 `make verify`。

### C

直接包含標頭檔並鏈結共享庫。

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

標頭檔自帶 `extern "C"` 守衛，無需額外處理。

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

用 `unsafe extern "C"` 宣告外部函數，`#[link]` 指定庫名。

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

### Go（cgo）

cgo 用 `${SRCDIR}` 指向模組目錄；`go.mod` 只需 `module host-interop-go` 與 `go 1.27`。

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

### Java（JNI）

JDK 21 的 Panama FFM 仍是 preview，故使用零依賴的 JNI：`javac -h` 生成標頭，C 膠水另鏈成 `libnolangjni`。

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

### Python（ctypes）

只用標準庫；以絕對路徑載入，避開搜尋路徑差異，並逐個宣告 `argtypes` / `restype`。

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

### C#（P/Invoke）

`DllImport` 搭配 `NativeLibrary.SetDllImportResolver`：macOS hardened runtime 可能忽略 `DYLD_LIBRARY_PATH`，所以由 `NOLANG_LIB` 傳入絕對路徑。

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

## 驗證

`Makefile` 的 `verify` 目標會依序構建共享庫、JNI 庫與七個宿主程式，把它們的輸出收集到 `dist/actual.txt`，最後與 `expected.txt` 做 `diff`；另外還會用 `nm -g` 斷言三個導出符號、複跑 guard，並在 Linux 檢查非 PIC 重定位。

```bash
cd example/host-interop
make verify
# ALL SEVEN HOST LANGUAGES PASSED
```

`expected.txt`：

```text
c 42 10.0 42
cpp 42 10.0 42
rust 42 10.0 42
go 42 10.0 42
java 42 10.0 42
python 42 10.0 42
csharp 42 10.0 42
```

## 風險與緩解

| 風險 | 緩解 |
| --- | --- |
| 內部 ABI 不穩定（按值／按指標、符號改名、衝突後綴） | `abi_guard.py` pin 住三條簽名，漂移即硬失敗；shim 是唯一穩定面 |
| 未被呼叫的函數被可達性分析丟棄、或錨點被 comptime 折疊 | 用 `getpid()` 提供運行期 `seed`，三個錨點都消費它 |
| `str` / `vec` 等堆型別的所有權與釋放 | 示例只用 `i64` / `f64` 標量；其餘列為非目標 |
| 合成的 `@main` 落在共享庫裡 | 保留即可，dyld／ld.so 不會把它當入口；`nm -g` 做資訊性檢查 |
| macOS 程式碼簽章／隔離屬性 | `codesign -s - --force`；必要時 `xattr -d com.apple.quarantine` |
| Linux 非 PIC 重定位 | `-fPIC` 加在編譯 `.ll` 的那一步，`readelf -r` 斷言 |
| `-o` 放在檔名之後 | Go 的 `flag` 會靜默不產出；一律 `-o OUT FILE` |
| LLVM 移除 typed pointer 自動升級 | 改用 `dist/libnolang_opt.ll`（opaque `ptr`）並更新 guard |
| 浮點輸出受語系影響 | 統一 `.1f`／`F1`＋`Locale.ROOT`／`InvariantCulture`，固定 `expected.txt` |

## 相關文檔

- [`#{c}` FFI 宣告](./syntax) — 反向的「Nolang 呼叫 C」
- [導出](./export) — `@` 是 Nolang 包之間的符號導出
- [原生包：WASM 與 FFI](./js-backend) — `bind=ffi` / `bind=wasm`
- [記憶體](./memory) — 模組級堆變數的生命週期
- [完整可運行示例](https://github.com/lizongying/nolang/tree/main/example/host-interop)
