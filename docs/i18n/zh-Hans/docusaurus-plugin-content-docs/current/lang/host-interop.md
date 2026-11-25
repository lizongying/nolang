---
sidebar_position: 9
---

# 从宿主语言调用 Nolang
> **这是编译器原生能力（`#{cabi}` v1 纯量版）。** `no build --shared` 会把标记 `#{cabi='symbol'}` 的顶层函数导出为稳定的 C ABI 符号，并在共享库旁自动生成 C 标头档。不需要手写 shim、不需要解析 LLVM IR、不需要可达性锚点。`#{c}`（Nolang 呼叫 C）、`@`（Nolang 包之间导出）、`#{cabi}`（宿主呼叫 Nolang）是三个不同方向的能力。


## 适用范围与限制

本文覆盖 C、C++、Rust、Go、Java、Python、C# 七种宿主语言，全部只使用各自工具链与标准库：直接链结、cgo、JNI、`ctypes`、`DllImport`，不需要 JNA、cffi、NuGet 或 Maven 依赖。示例只使用 `i64`／`f64` 标量；`str`、`vec`、`?T`、struct 等需要生命周期 ABI 的型别在 v1 会被编译器硬错误拒绝。完整可运行代码在 `example/host-interop/`，`make verify` 会逐一编译、执行并比对固定输出。

已在 macOS arm64 实跑验证：clang 22、Apple g++、rustc 1.98、Go 1.27、OpenJDK 21、Python 3.13、.NET 9。Linux 只需把 `.dylib` 换成 `.so`、`DYLD_LIBRARY_PATH` 换成 `LD_LIBRARY_PATH`、`@loader_path` 换成 `$ORIGIN`；Windows 使用 `.dll`。


## 原理：从 `#{cabi}` 到共享库

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


### 导出注解与可达性

在顶层函数定义前写 `#{cabi='nolang_add'}` 即指定对外 C 符号；省略值写 `#{cabi}` 时，编译器以 `nolang_` 加上函式名的消毒结果推导。符号必须是合法 C 识别字，不能重复，也不能占用 `main` / `_nolang_main`。方法、外部宣告、结果数量不是 1 的函数都会被诊断为 `cabi` 类硬错误。

`LowerHIR` 原本只从合成的 `main` 沿呼叫点展开可达函数，未被呼叫的函数根本不会进入 IR；全常数的顶层呼叫还会被 comptime 折叠。编译器现在把每个 `#{cabi}` 函数加入额外的可达性根，并把它标记为不可 comptime 折叠，因此示例的 `main.no` 可以只有三个函数定义，不需要任何锚点或假呼叫。


### Wrapper 与自动生成的标头档

Nolang 的内部呼叫 ABI 是 `define void @fn(args..., T* out)`：输入按值、结果写进呼叫端提供的 out-pointer，函式名还是可能随冲突而加后缀的内部名称。两者都不适合 C 呼叫。编译器为每个导出函数发射一个正常返回值的 wrapper：

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

wrapper 的签章与标头档由同一份 `cabiSignature` 解析结果产生，因此两者不可能漂移；wrapper 名称会先注册进 `usedFname`，避免与 runtime helper 或其他导出符号撞名。


### 共享库建置模式

`--shared` 让 `no build` 以 `clang -shared -fPIC` 直接把 LLVM IR 链成动态库（单次 `-x ir` 流程，PIC 同时作用于 codegen 与链结），并停用 LLVM C API 路径。自动输出路径会依平台补上 `.dylib` / `.so` / `.dll`；显式 `-o` 则原样使用。`--header` 可覆写标头档位置，预设是输出路径去副档名后加 `.h`。没有任何 `#{cabi}` 导出时会直接硬失败，避免产出没有 API 的库。

- `--shared` 与 `--js` / `--wasm-direct`、WASI target 互斥（只支援 LLVM 后端）。
- 共享库不会发射合成的 `define i32 @main`，避免与宿主可执行档的 `main` 互相干扰；`os.args` 需要的 `@nolang_argc` / `@nolang_argv` 全域变数仍然保留。
- 可执行档建置完全不受影响：没有 `#{cabi}` 时不产生 wrapper、不产生标头档，IR 保持不变。


## 支援型别（v1 纯量）

v1 只接受下列五种按值传递的标量。表格中的 C 型别就是生成标头档里的宣告型别。

| Nolang | LLVM IR | C 标头 | wrapper |
| --- | --- | --- | --- |
| `i64` | `i64` | `int64_t` | `i64` |
| `i32` | `i32` | `int32_t` | `i32` |
| `i8` | `i8` | `int8_t` | `i8` |
| `bool` | `i1` | `uint8_t` | `i8`（`trunc` / `zext`） |
| `f64` | `double` | `double` | `double` |

`bool` 在 LLVM 是 `i1`，但 C 没有可移植的 1-bit 型别，因此对外放宽为 `uint8_t`，由 wrapper 在边界做 `trunc`／`zext`。`i128`、`str`／`txt`、`vec`／`[]T`、`map`、`?T`、struct、指标、固定阵列，以及任何持有堆记忆体的值都会被 `cabiTypeOf` 拒绝，错误讯息会指出是哪个参数或结果型别不支援。


## 目录结构

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

旧版示例里的 `shim/`（手写 C adapter 与标头）与 `abi_guard.py`（内部 ABI 漂移防护）都已删除：导出符号、wrapper 与标头档现在全部由编译器产生。


## 第一步：Nolang 示例库

三个纯量函数，没有任何顶层叙述。`#{cabi='...'}` 的值就是宿主看到的 C 函式名。

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
    version: 'v0.1.0'
  }
  ignore: []
}
```


## 第二步：`no build --shared`

旗标必须放在档案前面（`-o OUT FILE`）。这一步同时产出动态库与标头档：

```bash
cd example/host-interop
mkdir -p dist
no build --shared -o dist/libnolang.dylib main.no
# [shared] dist/libnolang.dylib + dist/libnolang.h (3 exports)
# Build successful: dist/libnolang.dylib
```

> 需要包含 `#{cabi}` / `--shared` 的 `no`。若仓库的 `bin/no` 还是旧版，Makefile 的 `lib` 目标会给出明确提示；可用 `make verify NO=/path/to/no` 指定新编译器，不必覆写共享的 `bin/no`。

自动生成的标头档，可直接 `#include`：

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

Makefile 的 `lib` 目标就是这一步，另外用 `nm -g` 断言三个稳定符号确实导出：

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

| 旗标 | 作用 |
| --- | --- |
| `--shared` | 以 `-shared -fPIC` 建置动态库；需要至少一个 `#{cabi}` 导出 |
| `-o PATH` | 输出路径；决定自动生成的标头档名称 |
| `--header PATH` | 覆写标头档输出路径（预设 `<输出去副档名>.h`） |


## 七种宿主语言示例

| 语言 | 绑定方式 |
| --- | --- |
| C / C++ | 直接 `#include "libnolang.h"` 并链 `-lnolang` |
| Rust | `#[link(name = "nolang")] unsafe extern "C"` |
| Go | cgo `#cgo CFLAGS` / `#cgo LDFLAGS` |
| Java | JNI（JDK 21；不使用 preview 的 Panama FFM） |
| Python | 标准库 `ctypes.CDLL(绝对路径)` |
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

cgo 直接 include 生成的标头档；`${SRCDIR}` 让路径与工作目录无关。

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

Java 不能直接呼叫 C ABI，仍需一层 JNI 胶水；除此之外六种语言都不需要转接层。`javac -h` 产生 `NolangDemo.h`，JNI 库再链回 `-lnolang`。

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

明确指定 `argtypes` / `restype`，避免 64 位元整数被当成 C `int` 截断。

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

macOS hardened runtime 可能忽略 `DYLD_LIBRARY_PATH`，因此用 `NOLANG_LIB` 传绝对路径，再由 `SetDllImportResolver` 解析。

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


## 验证

函式库搜寻路径：macOS 用 `DYLD_LIBRARY_PATH` / `@rpath` / `@loader_path`，Linux 用 `LD_LIBRARY_PATH` / `$ORIGIN`；Java 另需 `-Djava.library.path`；.NET 在 hardened runtime 下用 `NOLANG_LIB` 传绝对路径。`make verify` 会建库、建七种宿主程式、把它们的输出收进 `dist/actual.txt`，再与 `expected.txt` 逐行比对。

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


## 限制、风险与缓解

| 风险 / 限制 | 现况处理 |
| --- | --- |
| 内部 ABI（`out-pointer`、符号消毒）不适合 C 直接呼叫 | 编译器发射正常返回值 wrapper；宿主只依赖 `#{cabi}` 符号与生成标头 |
| 未被呼叫的函数被可达性分析丢弃、或被 comptime 折叠 | 导出函数作为额外 `LowerHIR` 根并标记不可折叠；不再需要 `getpid()` 锚点 |
| 共享库导出 `main` 与宿主冲突 | `--shared` 不发射合成的 `define i32 @main` |
| 共享库只导出三个 `#{cabi}` 符号 | `--shared` 在 codegen 阶段把共享库模组内**所有非导出函数与全域变数**标 `hidden`、wrapper 标 `default`（LLVM IR visibility）；`nm -g` 只剩 `_nolang_add/_nolang_scale/_nolang_sum3`，内部 runtime 符号（`_nolang_rc_*`、`_nolang_async_*`、`@nolang_argc` 等）已不可见。局部连结（internal/private）符号本就不导出，保持 default |
| `str` / `vec` / `?T` 的所有权与 runtime 初始化／清理 | v1 明确拒绝；需要生命周期 ABI（`init` / `shutdown` / borrow / free）后才开放 |
| 导出符号名称撞名或非法 | 检查合法 C 识别字、重复、`main` / `_nolang_main` 保留字与 LLVM 内部符号，冲突即硬错误 |
| 标头档与 wrapper 型别漂移 | 两者由同一份 `cabiSignature` 解析结果产生 |
| Java 需要 JNI | 保留一层薄 JNI 库；其余六种语言零转接层 |


## 相关文档

- [FFI：Nolang 呼叫 C](./syntax) — `#{c}`，方向相反
- [导出与可见性](./export) — `@` 只在 Nolang 包之间导出
- [建置与执行](../usage) — `no build` 旗标与 `--shared`
- [本地示例 README](https://github.com/lizongying/no/tree/main/example/host-interop) — `make verify` 完整流程
