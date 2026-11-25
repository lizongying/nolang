---
sidebar_position: 9
---

# 호스트 언어에서 Nolang 호출하기
> **이는 컴파일러 기본 기능입니다(`#{cabi}` v1 스칼라 버전).** `no build --shared`는 `#{cabi='symbol'}`로 표시된 최상위 함수를 안정적인 C ABI 심볼로 내보내고, 공유 라이브러리 옆에 C 헤더 파일을 자동으로 생성합니다. 수동 shim, LLVM IR 파싱, 도달 가능성 앵커가 모두 필요 없습니다. `#{c}`(Nolang가 C를 호출), `@`(Nolang 패키지 간 내보내기), `#{cabi}`(호스트가 Nolang를 호출)는 서로 다른 방향의 세 가지 기능입니다.


## 적용 범위와 제한

이 문서는 C, C++, Rust, Go, Java, Python, C# 일곱 가지 호스트 언어를 다룹니다. 모두 각자의 도구 모음과 표준 라이브러리만 사용합니다. 직접 링크, cgo, JNI, `ctypes`, `DllImport`를 사용하며 JNA, cffi, NuGet, Maven 의존성은 필요하지 않습니다. 예제는 `i64`／`f64` 스칼라만 사용합니다. `str`, `vec`, `?T`, struct처럼 수명 주기 ABI가 필요한 타입은 v1에서 컴파일러가 하드 오류로 거부합니다. 완전히 실행 가능한 코드는 `example/host-interop/`에 있으며, `make verify`가 하나씩 컴파일하고 실행한 뒤 고정된 출력을 비교합니다.

macOS arm64에서 실제 실행으로 검증했습니다: clang 22, Apple g++, rustc 1.98, Go 1.27, OpenJDK 21, Python 3.13, .NET 9. Linux에서는 `.dylib`를 `.so`로, `DYLD_LIBRARY_PATH`를 `LD_LIBRARY_PATH`로, `@loader_path`를 `$ORIGIN`으로 바꾸기만 하면 됩니다. Windows에서는 `.dll`을 사용합니다.


## 원리: `#{cabi}`에서 공유 라이브러리까지

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


### 내보내기 애노테이션과 도달 가능성

최상위 함수 정의 앞에 `#{cabi='nolang_add'}`를 쓰면 외부로 노출할 C 심볼을 지정합니다. 값을 생략하고 `#{cabi}`로 쓰면 컴파일러가 `nolang_`에 함수 이름의 sanitize 결과를 덧붙여 추론합니다. 심볼은 유효한 C 식별자여야 하며, 중복될 수 없고 `main` / `_nolang_main`을 점유할 수도 없습니다. 메서드, 외부 선언, 결과 개수가 1이 아닌 함수는 모두 `cabi` 종류의 하드 오류로 진단됩니다.

`LowerHIR`는 원래 합성된 `main`에서 호출 지점을 따라 도달 가능한 함수만 전개했기 때문에, 호출되지 않는 함수는 IR에 들어가지도 않았습니다. 모두 상수인 최상위 호출은 comptime으로 접히기도 합니다. 컴파일러는 이제 각 `#{cabi}` 함수를 추가적인 도달 가능성 루트에 넣고 comptime 접기 불가로 표시하므로, 예제의 `main.no`에는 세 개의 함수 정의만 있을 뿐 앵커나 가짜 호출이 전혀 필요하지 않습니다.


### Wrapper와 자동 생성되는 헤더 파일

Nolang의 내부 호출 ABI는 `define void @fn(args..., T* out)`입니다. 입력은 값으로 전달되고 결과는 호출자가 제공한 out-pointer에 기록되며, 함수 이름은 충돌에 따라 접미사가 붙을 수 있는 내부 이름입니다. 둘 다 C에서 호출하기에 적합하지 않습니다. 컴파일러는 각 내보내기 함수에 대해 정상적인 반환값을 가진 wrapper를 생성합니다.

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

wrapper의 시그니처와 헤더 파일은 동일한 `cabiSignature` 파싱 결과에서 생성되므로 두 사이에 차이가 생길 수 없습니다. wrapper 이름은 먼저 `usedFname`에 등록되어 runtime helper나 다른 내보내기 심볼과 이름이 충돌하는 것을 방지합니다.


### 공유 라이브러리 빌드 모드

`--shared`는 `no build`가 `clang -shared -fPIC`로 LLVM IR을 직접 동적 라이브러리로 링크하게 합니다(단일 `-x ir` 흐름이며 PIC가 codegen과 링크 양쪽에 적용됩니다). 그리고 LLVM C API 경로는 비활성화합니다. 자동 출력 경로는 플랫폼에 따라 `.dylib` / `.so` / `.dll`을 붙이고, 명시적인 `-o`는 그대로 사용합니다. `--header`로 헤더 파일 위치를 덮어쓸 수 있으며, 기본값은 출력 경로에서 확장자를 제거한 뒤 `.h`를 붙인 것입니다. `#{cabi}` 내보내기가 하나도 없으면 하드 실패하여 API가 없는 라이브러리가 만들어지는 것을 방지합니다.

- `--shared`는 `--js` / `--wasm-direct`, WASI 타깃과 상호 배타적입니다(LLVM 백엔드만 지원합니다).
- 공유 라이브러리는 합성된 `define i32 @main`을 생성하지 않아 호스트 실행 파일의 `main`과 서로 간섭하는 것을 피합니다. `os.args`에 필요한 `@nolang_argc` / `@nolang_argv` 전역 변수는 그대로 유지됩니다.
- 실행 파일 빌드는 전혀 영향을 받지 않습니다. `#{cabi}`가 없으면 wrapper도 헤더 파일도 생성하지 않으며 IR은 변경되지 않습니다.


## 지원 타입(v1 스칼라)

v1은 다음 다섯 가지 값 전달 방식의 스칼라만 허용합니다. 표의 C 타입은 생성된 헤더 파일에 들어가는 선언 타입입니다.

| Nolang | LLVM IR | C 헤더 | wrapper |
| --- | --- | --- | --- |
| `i64` | `i64` | `int64_t` | `i64` |
| `i32` | `i32` | `int32_t` | `i32` |
| `i8` | `i8` | `int8_t` | `i8` |
| `bool` | `i1` | `uint8_t` | `i8`（`trunc` / `zext`） |
| `f64` | `double` | `double` | `double` |

`bool`은 LLVM에서 `i1`이지만 C에는 이식 가능한 1비트 타입이 없으므로, 외부에는 `uint8_t`로 넓혀 노출하고 wrapper가 경계에서 `trunc`／`zext`를 수행합니다. `i128`, `str`／`txt`, `vec`／`[]T`, `map`, `?T`, struct, 포인터, 고정 배열, 그리고 힙 메모리를 보유한 모든 값은 `cabiTypeOf`가 거부하며, 오류 메시지에 지원되지 않는 매개변수 또는 결과 타입이 표시됩니다.


## 디렉터리 구조

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

이전 버전 예제에 있던 `shim/`(수작업 C adapter와 헤더)과 `abi_guard.py`(내부 ABI 드리프트 방어)는 모두 삭제되었습니다. 내보내기 심볼, wrapper, 헤더 파일은 이제 전부 컴파일러가 생성합니다.


## 첫째 단계: Nolang 예제 라이브러리

스칼라 함수 세 개이며 최상위 문장은 전혀 없습니다. `#{cabi='...'}`의 값이 호스트가 보게 될 C 함수 이름입니다.

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


## 둘째 단계: `no build --shared`

플래그는 파일 앞에 두어야 합니다(`-o OUT FILE`). 이 단계에서 동적 라이브러리와 헤더 파일을 동시에 생성합니다.

```bash
cd example/host-interop
mkdir -p dist
no build --shared -o dist/libnolang.dylib main.no
# [shared] dist/libnolang.dylib + dist/libnolang.h (3 exports)
# Build successful: dist/libnolang.dylib
```

> `#{cabi}` / `--shared`를 지원하는 `no`가 필요합니다. 저장소의 `bin/no`가 아직 옛 버전이면 Makefile의 `lib` 타깃이 명확한 안내를 출력합니다. 공유 `bin/no`를 덮어쓸 필요 없이 `make verify NO=/path/to/no`로 새 컴파일러를 지정할 수 있습니다.

자동 생성된 헤더 파일은 바로 `#include`할 수 있습니다.

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

Makefile의 `lib` 타깃이 바로 이 단계이며, 추가로 `nm -g`로 안정적인 심볼 세 개가 실제로 내보내졌는지 확인합니다.

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

| 플래그 | 동작 |
| --- | --- |
| `--shared` | `-shared -fPIC`로 동적 라이브러리를 빌드합니다. `#{cabi}` 내보내기가 적어도 하나 필요합니다 |
| `-o PATH` | 출력 경로이며 자동 생성되는 헤더 파일의 이름을 결정합니다 |
| `--header PATH` | 헤더 파일 출력 경로를 덮어씁니다(기본값 `<출력 경로에서 확장자 제거>.h`) |


## 일곱 가지 호스트 언어 예제

| 언어 | 바인딩 방식 |
| --- | --- |
| C / C++ | `#include "libnolang.h"`를 직접 포함하고 `-lnolang`으로 링크합니다 |
| Rust | `#[link(name = "nolang")] unsafe extern "C"` |
| Go | cgo `#cgo CFLAGS` / `#cgo LDFLAGS` |
| Java | JNI(JDK 21, preview 상태인 Panama FFM은 사용하지 않음) |
| Python | 표준 라이브러리 `ctypes.CDLL(절대 경로)` |
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


### Go(cgo)

cgo는 생성된 헤더 파일을 직접 include합니다. `${SRCDIR}`를 사용하면 경로가 작업 디렉터리와 무관해집니다.

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


### Java(JNI)

Java는 C ABI를 직접 호출할 수 없으므로 JNI 글루 계층이 여전히 필요합니다. 그 밖의 여섯 언어는 어댑터 계층이 필요 없습니다. `javac -h`가 `NolangDemo.h`를 생성하고, JNI 라이브러리가 다시 `-lnolang`에 링크됩니다.

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


### Python(ctypes)

`argtypes` / `restype`를 명시적으로 지정하여 64비트 정수가 C `int`로 잘려 나가는 것을 방지합니다.

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


### C#(P/Invoke)

macOS hardened runtime은 `DYLD_LIBRARY_PATH`를 무시할 수 있으므로, `NOLANG_LIB`로 절대 경로를 전달한 뒤 `SetDllImportResolver`가 이를 해석하도록 합니다.

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


## 검증

라이브러리 검색 경로: macOS는 `DYLD_LIBRARY_PATH` / `@rpath` / `@loader_path`를 사용하고, Linux는 `LD_LIBRARY_PATH` / `$ORIGIN`을 사용합니다. Java는 추가로 `-Djava.library.path`가 필요하며, .NET은 hardened runtime에서 `NOLANG_LIB`로 절대 경로를 전달합니다. `make verify`는 라이브러리를 빌드하고 일곱 종류의 호스트 프로그램을 만든 뒤 그 출력을 `dist/actual.txt`에 모아 `expected.txt`와 줄 단위로 비교합니다.

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


## 제한, 위험 및 완화

| 위험 / 제한 | 현재 처리 방식 |
| --- | --- |
| 내부 ABI(`out-pointer`, 심볼 sanitize)는 C에서 직접 호출하기에 부적합 | 컴파일러가 정상 반환값 wrapper를 생성합니다. 호스트는 `#{cabi}` 심볼과 생성된 헤더에만 의존합니다 |
| 호출되지 않는 함수가 도달 가능성 분석에서 제거되거나 comptime으로 접히는 문제 | 내보내기 함수를 추가 `LowerHIR` 루트로 두고 접기 불가로 표시합니다. 더 이상 `getpid()` 앵커가 필요하지 않습니다 |
| 공유 라이브러리가 `main`을 내보내 호스트와 충돌하는 문제 | `--shared`는 합성된 `define i32 @main`을 생성하지 않습니다 |
| 공유 라이브러리가 세 개의 `#{cabi}` 심볼만 내보내도록 | `--shared`가 codegen 단계에서 공유 라이브러리 모듈 안의 **모든 비내보내기 함수와 전역 변수**에 `hidden`을, wrapper에 `default`를 표시합니다(LLVM IR visibility). `nm -g`에는 `_nolang_add/_nolang_scale/_nolang_sum3`만 남고 내부 runtime 심볼(`_nolang_rc_*`, `_nolang_async_*`, `@nolang_argc` 등)은 보이지 않게 됩니다. 지역 링크(internal/private) 심볼은 원래 내보내지지 않으므로 default를 유지합니다 |
| `str` / `vec` / `?T`의 소유권과 runtime 초기화／정리 | v1에서 명확히 거부합니다. 수명 주기 ABI(`init` / `shutdown` / borrow / free)가 마련된 뒤에만 허용합니다 |
| 내보내기 심볼 이름이 중복되거나 유효하지 않은 문제 | 유효한 C 식별자인지, 중복인지, `main` / `_nolang_main` 예약어 및 LLVM 내부 심볼인지 검사하고 충돌 시 하드 오류로 처리합니다 |
| 헤더 파일과 wrapper의 타입 불일치 | 둘 다 동일한 `cabiSignature` 파싱 결과에서 생성됩니다 |
| Java는 JNI가 필요 | 얇은 JNI 라이브러리 한 계층을 유지합니다. 나머지 여섯 언어는 어댑터 계층이 전혀 없습니다 |


## 관련 문서

- [FFI: Nolang에서 C 호출하기](./syntax) — `#{c}`, 방향이 반대입니다
- [내보내기와 가시성](./export) — `@`는 Nolang 패키지 간에만 내보냅니다
- [빌드와 실행](../usage) — `no build` 플래그와 `--shared`
- [로컬 예제 README](https://github.com/lizongying/no/tree/main/example/host-interop) — `make verify` 전체 흐름
