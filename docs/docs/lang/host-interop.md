---
sidebar_position: 9
---

# 從宿主語言調用 Nolang
> **這是編譯器原生能力（`#{cabi}` v1 純量版）。** `no build --shared` 會把標記 `#{cabi='symbol'}` 的頂層函數導出為穩定的 C ABI 符號，並在共享庫旁自動生成 C 標頭檔。不需要手寫 shim、不需要解析 LLVM IR、不需要可達性錨點。`#{c}`（Nolang 呼叫 C）、`@`（Nolang 包之間導出）、`#{cabi}`（宿主呼叫 Nolang）是三個不同方向的能力。


## 適用範圍與限制

本文覆蓋 C、C++、Rust、Go、Java、Python、C# 七種宿主語言，全部只使用各自工具鏈與標準庫：直接鏈結、cgo、JNI、`ctypes`、`DllImport`，不需要 JNA、cffi、NuGet 或 Maven 依賴。示例只使用 `i64`／`f64` 標量；`str`、`vec`、`?T`、struct 等需要生命週期 ABI 的型別在 v1 會被編譯器硬錯誤拒絕。完整可運行代碼在 `example/host-interop/`，`make verify` 會逐一編譯、執行並比對固定輸出。

已在 macOS arm64 實跑驗證：clang 22、Apple g++、rustc 1.98、Go 1.27、OpenJDK 21、Python 3.13、.NET 9。Linux 只需把 `.dylib` 換成 `.so`、`DYLD_LIBRARY_PATH` 換成 `LD_LIBRARY_PATH`、`@loader_path` 換成 `$ORIGIN`；Windows 使用 `.dll`。


## 原理：從 `#{cabi}` 到共享庫

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


### 導出註解與可達性

在頂層函數定義前寫 `#{cabi='nolang_add'}` 即指定對外 C 符號；省略值寫 `#{cabi}` 時，編譯器以 `nolang_` 加上函式名的消毒結果推導。符號必須是合法 C 識別字，不能重複，也不能佔用 `main` / `_nolang_main`。方法、外部宣告、結果數量不是 1 的函數都會被診斷為 `cabi` 類硬錯誤。

`LowerHIR` 原本只從合成的 `main` 沿呼叫點展開可達函數，未被呼叫的函數根本不會進入 IR；全常數的頂層呼叫還會被 comptime 折疊。編譯器現在把每個 `#{cabi}` 函數加入額外的可達性根，並把它標記為不可 comptime 折疊，因此示例的 `main.no` 可以只有三個函數定義，不需要任何錨點或假呼叫。


### Wrapper 與自動生成的標頭檔

Nolang 的內部呼叫 ABI 是 `define void @fn(args..., T* out)`：輸入按值、結果寫進呼叫端提供的 out-pointer，函式名還是可能隨衝突而加後綴的內部名稱。兩者都不適合 C 呼叫。編譯器為每個導出函數發射一個正常返回值的 wrapper：

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

wrapper 的簽章與標頭檔由同一份 `cabiSignature` 解析結果產生，因此兩者不可能漂移；wrapper 名稱會先註冊進 `usedFname`，避免與 runtime helper 或其他導出符號撞名。


### 共享庫建置模式

`--shared` 讓 `no build` 以 `clang -shared -fPIC` 直接把 LLVM IR 鏈成動態庫（單次 `-x ir` 流程，PIC 同時作用於 codegen 與鏈結），並停用 LLVM C API 路徑。自動輸出路徑會依平台補上 `.dylib` / `.so` / `.dll`；顯式 `-o` 則原樣使用。`--header` 可覆寫標頭檔位置，預設是輸出路徑去副檔名後加 `.h`。沒有任何 `#{cabi}` 導出時會直接硬失敗，避免產出沒有 API 的庫。

- `--shared` 與 `--js` / `--wasm-direct`、WASI target 互斥（只支援 LLVM 後端）。
- 共享庫不會發射合成的 `define i32 @main`，避免與宿主可執行檔的 `main` 互相干擾；`os.args` 需要的 `@nolang_argc` / `@nolang_argv` 全域變數仍然保留。
- 可執行檔建置完全不受影響：沒有 `#{cabi}` 時不產生 wrapper、不產生標頭檔，IR 保持不變。


## 支援型別（v1 純量）

v1 只接受下列五種按值傳遞的標量。表格中的 C 型別就是生成標頭檔裡的宣告型別。

| Nolang | LLVM IR | C 標頭 | wrapper |
| --- | --- | --- | --- |
| `i64` | `i64` | `int64_t` | `i64` |
| `i32` | `i32` | `int32_t` | `i32` |
| `i8` | `i8` | `int8_t` | `i8` |
| `bool` | `i1` | `uint8_t` | `i8`（`trunc` / `zext`） |
| `f64` | `double` | `double` | `double` |

`bool` 在 LLVM 是 `i1`，但 C 沒有可移植的 1-bit 型別，因此對外放寬為 `uint8_t`，由 wrapper 在邊界做 `trunc`／`zext`。`i128`、`str`／`txt`、`vec`／`[]T`、`map`、`?T`、struct、指標、固定陣列，以及任何持有堆記憶體的值都會被 `cabiTypeOf` 拒絕，錯誤訊息會指出是哪個參數或結果型別不支援。


## 目錄結構

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

舊版示例裡的 `shim/`（手寫 C adapter 與標頭）與 `abi_guard.py`（內部 ABI 漂移防護）都已刪除：導出符號、wrapper 與標頭檔現在全部由編譯器產生。


## 第一步：Nolang 示例庫

三個純量函數，沒有任何頂層敘述。`#{cabi='...'}` 的值就是宿主看到的 C 函式名。

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


## 第二步：`no build --shared`

旗標必須放在檔案前面（`-o OUT FILE`）。這一步同時產出動態庫與標頭檔：

```bash
cd example/host-interop
mkdir -p dist
no build --shared -o dist/libnolang.dylib main.no
# [shared] dist/libnolang.dylib + dist/libnolang.h (3 exports)
# Build successful: dist/libnolang.dylib
```

> 需要包含 `#{cabi}` / `--shared` 的 `no`。若倉庫的 `bin/no` 還是舊版，Makefile 的 `lib` 目標會給出明確提示；可用 `make verify NO=/path/to/no` 指定新編譯器，不必覆寫共享的 `bin/no`。

自動生成的標頭檔，可直接 `#include`：

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

Makefile 的 `lib` 目標就是這一步，另外用 `nm -g` 斷言三個穩定符號確實導出：

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

| 旗標 | 作用 |
| --- | --- |
| `--shared` | 以 `-shared -fPIC` 建置動態庫；需要至少一個 `#{cabi}` 導出 |
| `-o PATH` | 輸出路徑；決定自動生成的標頭檔名稱 |
| `--header PATH` | 覆寫標頭檔輸出路徑（預設 `<輸出去副檔名>.h`） |


## 七種宿主語言示例

| 語言 | 綁定方式 |
| --- | --- |
| C / C++ | 直接 `#include "libnolang.h"` 並鏈 `-lnolang` |
| Rust | `#[link(name = "nolang")] unsafe extern "C"` |
| Go | cgo `#cgo CFLAGS` / `#cgo LDFLAGS` |
| Java | JNI（JDK 21；不使用 preview 的 Panama FFM） |
| Python | 標準庫 `ctypes.CDLL(絕對路徑)` |
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


### Go（cgo）

cgo 直接 include 生成的標頭檔；`${SRCDIR}` 讓路徑與工作目錄無關。

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


### Java（JNI）

Java 不能直接呼叫 C ABI，仍需一層 JNI 膠水；除此之外六種語言都不需要轉接層。`javac -h` 產生 `NolangDemo.h`，JNI 庫再鏈回 `-lnolang`。

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


### Python（ctypes）

明確指定 `argtypes` / `restype`，避免 64 位元整數被當成 C `int` 截斷。

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

macOS hardened runtime 可能忽略 `DYLD_LIBRARY_PATH`，因此用 `NOLANG_LIB` 傳絕對路徑，再由 `SetDllImportResolver` 解析。

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


## 驗證

函式庫搜尋路徑：macOS 用 `DYLD_LIBRARY_PATH` / `@rpath` / `@loader_path`，Linux 用 `LD_LIBRARY_PATH` / `$ORIGIN`；Java 另需 `-Djava.library.path`；.NET 在 hardened runtime 下用 `NOLANG_LIB` 傳絕對路徑。`make verify` 會建庫、建七種宿主程式、把它們的輸出收進 `dist/actual.txt`，再與 `expected.txt` 逐行比對。

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


## 限制、風險與緩解

| 風險 / 限制 | 現況處理 |
| --- | --- |
| 內部 ABI（`out-pointer`、符號消毒）不適合 C 直接呼叫 | 編譯器發射正常返回值 wrapper；宿主只依賴 `#{cabi}` 符號與生成標頭 |
| 未被呼叫的函數被可達性分析丟棄、或被 comptime 折疊 | 導出函數作為額外 `LowerHIR` 根並標記不可折疊；不再需要 `getpid()` 錨點 |
| 共享庫導出 `main` 與宿主衝突 | `--shared` 不發射合成的 `define i32 @main` |
| 共享庫只導出三個 `#{cabi}` 符號 | `--shared` 在 codegen 階段把共享庫模組內**所有非導出函數與全域變數**標 `hidden`、wrapper 標 `default`（LLVM IR visibility）；`nm -g` 只剩 `_nolang_add/_nolang_scale/_nolang_sum3`，內部 runtime 符號（`_nolang_rc_*`、`_nolang_async_*`、`@nolang_argc` 等）已不可見。局部連結（internal/private）符號本就不導出，保持 default |
| `str` / `vec` / `?T` 的所有權與 runtime 初始化／清理 | v1 明確拒絕；需要生命週期 ABI（`init` / `shutdown` / borrow / free）後才開放 |
| 導出符號名稱撞名或非法 | 檢查合法 C 識別字、重複、`main` / `_nolang_main` 保留字與 LLVM 內部符號，衝突即硬錯誤 |
| 標頭檔與 wrapper 型別漂移 | 兩者由同一份 `cabiSignature` 解析結果產生 |
| Java 需要 JNI | 保留一層薄 JNI 庫；其餘六種語言零轉接層 |


## 相關文檔

- [FFI：Nolang 呼叫 C](./syntax) — `#{c}`，方向相反
- [導出與可見性](./export) — `@` 只在 Nolang 包之間導出
- [建置與執行](../usage) — `no build` 旗標與 `--shared`
- [本地示例 README](https://github.com/lizongying/no/tree/main/example/host-interop) — `make verify` 完整流程
