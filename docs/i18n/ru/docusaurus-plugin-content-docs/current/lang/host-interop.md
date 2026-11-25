---
sidebar_position: 9
---

# Вызов Nolang из host-языков
> **Это нативная возможность компилятора (`#{cabi}`, версия v1 для скаляров).** `no build --shared` экспортирует функции верхнего уровня, помеченные `#{cabi='symbol'}`, в стабильные символы C ABI и автоматически создаёт C-заголовок рядом с разделяемой библиотекой. Не нужно писать shim вручную, разбирать LLVM IR или использовать якоря достижимости. `#{c}` (Nolang вызывает C), `@` (экспорт между пакетами Nolang) и `#{cabi}` (host вызывает Nolang) — три разных направления возможностей.


## Область применения и ограничения

В этой статье рассматриваются семь host-языков: C, C++, Rust, Go, Java, Python и C#; все они используют только собственный инструментарий и стандартную библиотеку: прямую компоновку, cgo, JNI, `ctypes`, `DllImport` — без зависимостей JNA, cffi, NuGet или Maven. Примеры используют только скаляры `i64`／`f64`; типы вроде `str`, `vec`, `?T` и struct, которым требуется ABI со временем жизни, в v1 жёстко отклоняются компилятором. Полный рабочий код находится в `example/host-interop/`; `make verify` последовательно компилирует, запускает и сравнивает фиксированный вывод.

Проверено в реальном запуске на macOS arm64: clang 22, Apple g++, rustc 1.98, Go 1.27, OpenJDK 21, Python 3.13, .NET 9. Для Linux достаточно заменить `.dylib` на `.so`, `DYLD_LIBRARY_PATH` на `LD_LIBRARY_PATH`, а `@loader_path` на `$ORIGIN`; Windows использует `.dll`.


## Принцип: от `#{cabi}` к разделяемой библиотеке

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


### Аннотация экспорта и достижимость

Чтобы задать внешний C-символ, напишите `#{cabi='nolang_add'}` перед определением функции верхнего уровня; если значение опущено (`#{cabi}`), компилятор выведет его как `nolang_` плюс санитизированное имя функции. Символ должен быть допустимым C-идентификатором, не должен повторяться и не должен занимать `main` / `_nolang_main`. Методы, внешние объявления и функции с числом результатов не 1 диагностируются как жёсткая ошибка класса `cabi`.

Изначально `LowerHIR` разворачивал достижимые функции только от синтетического `main` по точкам вызова; невызываемые функции вообще не попадали в IR, а полностью константные вызовы верхнего уровня ещё и сворачивались comptime. Теперь компилятор добавляет каждую функцию `#{cabi}` как дополнительный корень достижимости и помечает её как не сворачиваемую comptime, поэтому `main.no` в примере может содержать всего три определения функций без каких-либо якорей или фиктивных вызовов.


### Wrapper и автоматически генерируемый заголовок

Внутренний ABI вызовов Nolang — это `define void @fn(args..., T* out)`: аргументы передаются по значению, результат записывается в предоставленный вызывающей стороной out-указатель, а имя функции всё ещё может быть внутренним с суффиксом, добавленным при конфликтах. Ни то, ни другое не подходит для вызова из C. Для каждой экспортируемой функции компилятор испускает wrapper с обычным возвратом значения:

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

Сигнатура wrapper и заголовок создаются из одного и того же результата разбора `cabiSignature`, поэтому они не могут разойтись; имя wrapper сначала регистрируется в `usedFname`, чтобы избежать коллизии с runtime-хелперами или другими экспортируемыми символами.


### Режим сборки разделяемой библиотеки

`--shared` заставляет `no build` компоновать LLVM IR в динамическую библиотеку напрямую через `clang -shared -fPIC` (один проход `-x ir`, PIC действует и в codegen, и при компоновке) и отключает LLVM C API path. Автоматический путь вывода дополняется платформенным `.dylib` / `.so` / `.dll`; явный `-o` используется как есть. `--header` позволяет переопределить расположение заголовка, по умолчанию это путь вывода без расширения плюс `.h`. Если нет ни одного экспорта `#{cabi}`, сборка сразу завершается жёсткой ошибкой, чтобы не получить библиотеку без API.

- `--shared` несовместим с `--js` / `--wasm-direct` и WASI target (поддерживается только LLVM-бэкенд).
- Разделяемая библиотека не испускает синтетический `define i32 @main`, чтобы не конфликтовать с `main` исполняемого файла host; глобальные переменные `@nolang_argc` / `@nolang_argv`, необходимые `os.args`, сохраняются.
- Сборка исполняемого файла полностью не затрагивается: без `#{cabi}` wrapper и заголовок не создаются, IR остаётся неизменным.


## Поддерживаемые типы (v1 — скаляры)

v1 принимает только следующие пять скаляров, передаваемых по значению. C-типы в таблице — это объявления в генерируемом заголовке.

| Nolang | LLVM IR | C-заголовок | wrapper |
| --- | --- | --- | --- |
| `i64` | `i64` | `int64_t` | `i64` |
| `i32` | `i32` | `int32_t` | `i32` |
| `i8` | `i8` | `int8_t` | `i8` |
| `bool` | `i1` | `uint8_t` | `i8`（`trunc` / `zext`） |
| `f64` | `double` | `double` | `double` |

`bool` в LLVM — это `i1`, но в C нет переносимого 1-битного типа, поэтому наружу он расширяется до `uint8_t`, а wrapper выполняет `trunc`／`zext` на границе. `i128`, `str`／`txt`, `vec`／`[]T`, `map`, `?T`, struct, указатели, массивы фиксированной длины и любые значения, владеющие памятью в куче, отклоняются `cabiTypeOf`; сообщение об ошибке укажет, какой именно параметр или тип результата не поддерживается.


## Структура каталога

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

В старом варианте примера `shim/` (рукописный C-адаптер и заголовок) и `abi_guard.py` (защита от внутреннего дрейфа ABI) удалены: экспортируемые символы, wrapper и заголовок теперь полностью создаются компилятором.


## Шаг первый: пример библиотеки на Nolang

Три скалярные функции, без каких-либо операторов верхнего уровня. Значение `#{cabi='...'}` — это имя C-функции, которое видит host.

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


## Шаг второй: `no build --shared`

Флаг должен стоять перед файлами (`-o OUT FILE`). На этом шаге одновременно создаются динамическая библиотека и заголовок:

```bash
cd example/host-interop
mkdir -p dist
no build --shared -o dist/libnolang.dylib main.no
# [shared] dist/libnolang.dylib + dist/libnolang.h (3 exports)
# Build successful: dist/libnolang.dylib
```

> Нужен `no` с поддержкой `#{cabi}` / `--shared`. Если `bin/no` в репозитории ещё старый, цель Makefile `lib` даст явное указание; можно задать новый компилятор через `make verify NO=/path/to/no`, не перезаписывая общий `bin/no`.

Автоматически созданный заголовок можно включать напрямую через `#include`:

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

Цель `lib` в Makefile делает именно это, а также через `nm -g` проверяет, что три стабильных символа действительно экспортированы:

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

| Флаг | Действие |
| --- | --- |
| `--shared` | собрать динамическую библиотеку с `-shared -fPIC`; требуется хотя бы один экспорт `#{cabi}` |
| `-o PATH` | путь вывода; определяет имя автоматически создаваемого заголовка |
| `--header PATH` | переопределить путь вывода заголовка (по умолчанию `<путь вывода без расширения>.h`) |


## Примеры для семи host-языков

| Язык | Способ привязки |
| --- | --- |
| C / C++ | напрямую `#include "libnolang.h"` и компоновка `-lnolang` |
| Rust | `#[link(name = "nolang")] unsafe extern "C"` |
| Go | cgo `#cgo CFLAGS` / `#cgo LDFLAGS` |
| Java | JNI (JDK 21; preview-механизм Panama FFM не используется) |
| Python | стандартная библиотека `ctypes.CDLL(абсолютный путь)` |
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

cgo напрямую включает созданный заголовок; `${SRCDIR}` делает путь независимым от рабочего каталога.

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

Java не может напрямую вызывать C ABI, поэтому нужен слой JNI-обвязки; остальным шести языкам адаптер не требуется. `javac -h` создаёт `NolangDemo.h`, а JNI-библиотека компонуется обратно с `-lnolang`.

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

Явно задайте `argtypes` / `restype`, чтобы 64-битные целые не усекались как C `int`.

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

Hardened runtime macOS может игнорировать `DYLD_LIBRARY_PATH`, поэтому абсолютный путь передаётся через `NOLANG_LIB`, а разрешается он через `SetDllImportResolver`.

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


## Проверка

Путь поиска библиотек: macOS использует `DYLD_LIBRARY_PATH` / `@rpath` / `@loader_path`, Linux — `LD_LIBRARY_PATH` / `$ORIGIN`; для Java дополнительно нужен `-Djava.library.path`; .NET в hardened runtime получает абсолютный путь через `NOLANG_LIB`. `make verify` собирает библиотеку, строит семь host-программ, собирает их вывод в `dist/actual.txt` и затем построчно сравнивает с `expected.txt`.

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


## Ограничения, риски и меры

| Риск / ограничение | Текущая обработка |
| --- | --- |
| Внутренний ABI (`out-pointer`, санитизация символов) не годится для прямого вызова из C | компилятор испускает wrapper с обычным возвратом; host зависит только от символов `#{cabi}` и созданного заголовка |
| Невызываемые функции отбрасываются анализом достижимости или сворачиваются comptime | экспортируемые функции — дополнительные корни `LowerHIR` и помечены как не сворачиваемые; якорь `getpid()` больше не нужен |
| Разделяемая библиотека экспортирует `main` и конфликтует с host | `--shared` не испускает синтетический `define i32 @main` |
| Разделяемая библиотека экспортирует только три символа `#{cabi}` | `--shared` на этапе codegen помечает **все неэкспортируемые функции и глобальные переменные** модуля разделяемой библиотеки как `hidden`, а wrapper — как `default` (LLVM IR visibility); в `nm -g` остаются только `_nolang_add/_nolang_scale/_nolang_sum3`, внутренние runtime-символы (`_nolang_rc_*`, `_nolang_async_*`, `@nolang_argc` и др.) стали невидимыми. Символы с локальной компоновкой (internal/private) изначально не экспортируются и остаются default |
| Владение и инициализация／очистка runtime для `str` / `vec` / `?T` | в v1 явно отклоняется; будет открыто после появления ABI со временем жизни (`init` / `shutdown` / borrow / free) |
| Имя экспортируемого символа конфликтует или некорректно | проверка на допустимый C-идентификатор, дубликаты, зарезервированные `main` / `_nolang_main` и внутренние символы LLVM; конфликт — жёсткая ошибка |
| Дрейф типов между заголовком и wrapper | оба создаются из одного и того же результата разбора `cabiSignature` |
| Java требует JNI | сохранён тонкий слой JNI-библиотеки; остальные шесть языков — без адаптера |


## Связанные документы

- [FFI: Nolang вызывает C](./syntax) — `#{c}`, обратное направление
- [Экспорт и видимость](./export) — `@` экспортирует только между пакетами Nolang
- [Сборка и запуск](../usage) — флаги `no build` и `--shared`
- [README локального примера](https://github.com/lizongying/no/tree/main/example/host-interop) — полный цикл `make verify`
