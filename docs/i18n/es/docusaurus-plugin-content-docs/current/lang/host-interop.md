---
sidebar_position: 9
---

# Llamar a Nolang desde lenguajes anfitriones
> **Esta es una capacidad nativa del compilador (`#{cabi}` v1, solo escalares).** `no build --shared` exporta las funciones de nivel superior marcadas con `#{cabi='symbol'}` como símbolos C ABI estables y genera automáticamente el archivo de cabecera C junto a la biblioteca compartida. No hace falta escribir un shim a mano, ni postprocesar LLVM IR, ni usar anclas de alcanzabilidad. `#{c}` (Nolang llama a C), `@` (exportaciones entre paquetes Nolang) y `#{cabi}` (un anfitrión llama a Nolang) son tres capacidades de direcciones distintas.


## Ámbito y limitaciones

Esta página cubre siete lenguajes anfitriones: C, C++, Rust, Go, Java, Python y C#. Todos los ejemplos usan únicamente su propia cadena de herramientas y biblioteca estándar — enlace directo, cgo, JNI, `ctypes`, `DllImport` — sin dependencias de JNA, cffi, NuGet ni Maven. Los ejemplos usan solo escalares `i64` / `f64`; `str`, `vec`, `?T`, struct y demás tipos que necesitan una ABI de ciclo de vida son rechazados con un error duro del compilador en v1. El código completo y ejecutable está en `example/host-interop/`, donde `make verify` compila, ejecuta y compara la salida fijada de los siete anfitriones.

Verificado en macOS arm64 con clang 22, Apple g++, rustc 1.98, Go 1.27, OpenJDK 21, Python 3.13 y .NET 9. En Linux, sustituya `.dylib` por `.so`, `DYLD_LIBRARY_PATH` por `LD_LIBRARY_PATH` y `@loader_path` por `$ORIGIN`; en Windows use `.dll`.


## Cómo funciona: de `#{cabi}` a la biblioteca compartida

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


### La anotación de exportación y la alcanzabilidad

Escriba `#{cabi='nolang_add'}` antes de la definición de una función de nivel superior para elegir su símbolo C público; con un `#{cabi}` sin valor, el compilador deriva `nolang_<nombre-sanitizado>`. Un símbolo debe ser un identificador C válido, debe ser único y no puede apropiarse de `main` ni de `_nolang_main`. Los métodos, las declaraciones externas y las funciones cuyo número de resultados no es exactamente uno producen un diagnóstico fatal de tipo `cabi`.

`LowerHIR` normalmente expande solo las funciones alcanzables desde el `main` sintetizado; las funciones no referenciadas nunca llegan al IR, y las llamadas de nivel superior con todos los argumentos constantes pueden además plegarse en tiempo de compilación. Ahora el compilador añade cada función `#{cabi}` como raíz de alcanzabilidad adicional y la marca como no plegable en comptime, de modo que el `main.no` del ejemplo puede constar únicamente de tres definiciones de función — sin anclas ni llamadas falsas.


### Wrappers y la cabecera generada

La ABI de llamada interna de Nolang es `define void @fn(args..., T* out)`: las entradas por valor, el resultado escrito a través de un out-pointer proporcionado por el llamador y un nombre de símbolo interno que puede ganar un sufijo por colisión. Ninguna de estas cosas es usable desde C. El compilador emite un wrapper con un valor de retorno real para cada exportación:

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

La firma del wrapper y la cabecera se generan a partir del MISMO resultado de `cabiSignature`, así que no pueden divergir. El nombre del wrapper se registra primero en `usedFname`, lo que evita colisiones con ayudas del runtime u otras exportaciones.


### Modo de compilación de biblioteca compartida

`--shared` hace que `no build` enlace el LLVM IR en una biblioteca dinámica con `clang -shared -fPIC` (una sola pasada `-x ir`, de modo que PIC se aplica tanto a la generación de código como al enlace) y desactiva la vía de la API C de LLVM. Una ruta de salida automática recibe el sufijo de plataforma `.dylib` / `.so` / `.dll`; un `-o` explícito se usa literalmente. `--header` sobrescribe la ruta de la cabecera, que por defecto es la ruta de salida con la extensión sustituida por `.h`. Una compilación sin ninguna exportación `#{cabi}` falla con un error claro, en lugar de producir una biblioteca sin API.

- `--shared` es incompatible con `--js` / `--wasm-direct` y con un objetivo WASI (solo se admite el backend LLVM).
- Una biblioteca compartida omite el `define i32 @main` sintetizado, para no interponerse en el `main` del ejecutable anfitrión; las variables globales `@nolang_argc` / `@nolang_argv` que necesita `os.args` se siguen emitiendo.
- Las compilaciones de ejecutables no se ven afectadas: sin exportaciones `#{cabi}` no se genera wrapper ni cabecera, y el IR permanece igual.


## Tipos admitidos (escalares v1)

v1 acepta exactamente estos cinco escalares por valor. La columna C es el tipo de declaración que se escribe en la cabecera generada.

| Nolang | LLVM IR | Cabecera C | wrapper |
| --- | --- | --- | --- |
| `i64` | `i64` | `int64_t` | `i64` |
| `i32` | `i32` | `int32_t` | `i32` |
| `i8` | `i8` | `int8_t` | `i8` |
| `bool` | `i1` | `uint8_t` | `i8` (`trunc` / `zext`) |
| `f64` | `double` | `double` | `double` |

`bool` es `i1` en LLVM, pero C no tiene un tipo de un bit portable, así que se expone como `uint8_t`, con `trunc` / `zext` en la frontera del wrapper. `i128`, `str` / `txt`, `vec` / `[]T`, `map`, `?T`, struct, punteros, arreglos fijos y cualquier valor que posea memoria en el heap son rechazados por `cabiTypeOf`; el error nombra el parámetro o tipo de resultado problemático.


## Estructura de directorios

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

El antiguo directorio `shim/` (adaptador y cabecera C escritos a mano) y `abi_guard.py` (protección contra deriva de la ABI interna) ya no existen: ahora el compilador se encarga de los símbolos exportados, los wrappers y la cabecera.


## Paso 1: la biblioteca de ejemplo en Nolang

Tres funciones escalares, sin ninguna sentencia de nivel superior. El valor de `#{cabi='...'}` es el nombre de la función C que ve el anfitrión.

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


## Paso 2: `no build --shared`

Las banderas deben ir delante del archivo (`-o OUT FILE`). Este paso produce a la vez la biblioteca dinámica y el archivo de cabecera:

```bash
cd example/host-interop
mkdir -p dist
no build --shared -o dist/libnolang.dylib main.no
# [shared] dist/libnolang.dylib + dist/libnolang.h (3 exports)
# Build successful: dist/libnolang.dylib
```

> Necesita un `no` que incluya `#{cabi}` / `--shared`. Si el `bin/no` del repositorio sigue siendo antiguo, el objetivo `lib` del Makefile dará un aviso claro; puede usar `make verify NO=/ruta/a/no` para indicar el compilador nuevo, sin sobrescribir el `bin/no` compartido.

El archivo de cabecera generado se puede incluir directamente con `#include`:

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

El objetivo `lib` del Makefile hace exactamente esto y, además, usa `nm -g` para afirmar que los tres símbolos estables están realmente exportados:

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

| Bandera | Efecto |
| --- | --- |
| `--shared` | compila una biblioteca dinámica con `-shared -fPIC`; requiere al menos una exportación `#{cabi}` |
| `-o PATH` | ruta de salida; determina el nombre de la cabecera generada automáticamente |
| `--header PATH` | sobrescribe la ruta de salida de la cabecera (por defecto `<salida-sin-extensión>.h`) |


## Ejemplos de los siete lenguajes anfitriones

| Lenguaje | Forma de enlace |
| --- | --- |
| C / C++ | `#include "libnolang.h"` directo y enlace con `-lnolang` |
| Rust | `#[link(name = "nolang")] unsafe extern "C"` |
| Go | cgo `#cgo CFLAGS` / `#cgo LDFLAGS` |
| Java | JNI (JDK 21; sin usar Panama FFM en preview) |
| Python | `ctypes.CDLL(ruta_absoluta)` de la biblioteca estándar |
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

cgo incluye directamente la cabecera generada; `${SRCDIR}` hace que la ruta sea independiente del directorio de trabajo.

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

Java no puede llamar directamente a una ABI C, así que necesita una capa de pegamento JNI; aparte de eso, los otros seis lenguajes no necesitan adaptador. `javac -h` genera `NolangDemo.h`, y la biblioteca JNI se enlaza de vuelta con `-lnolang`.

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

Especifique explícitamente `argtypes` / `restype` para evitar que los enteros de 64 bits se truncen como `int` de C.

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

El hardened runtime de macOS puede ignorar `DYLD_LIBRARY_PATH`, así que se pasa una ruta absoluta mediante `NOLANG_LIB` y se resuelve con `SetDllImportResolver`.

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


## Verificación

Ruta de búsqueda de bibliotecas: macOS usa `DYLD_LIBRARY_PATH` / `@rpath` / `@loader_path`, Linux usa `LD_LIBRARY_PATH` / `$ORIGIN`; Java necesita además `-Djava.library.path`; .NET, bajo hardened runtime, usa `NOLANG_LIB` con una ruta absoluta. `make verify` compila la biblioteca, compila los siete programas anfitriones, recoge sus salidas en `dist/actual.txt` y las compara línea a línea con `expected.txt`.

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


## Limitaciones, riesgos y mitigaciones

| Riesgo / limitación | Tratamiento actual |
| --- | --- |
| La ABI interna (`out-pointer`, sanitización de símbolos) no es llamable directamente desde C | El compilador emite un wrapper con valor de retorno normal; el anfitrión solo depende de los símbolos `#{cabi}` y de la cabecera generada |
| Las funciones no llamadas se pierden en el análisis de alcanzabilidad o se pliegan en comptime | Las funciones exportadas se añaden como raíces `LowerHIR` adicionales y se marcan como no plegables; ya no hace falta el ancla `getpid()` |
| La biblioteca compartida exporta `main` y entra en conflicto con el anfitrión | `--shared` no emite el `define i32 @main` sintetizado |
| La biblioteca compartida solo exporta tres símbolos `#{cabi}` | `--shared` marca en la fase de codegen **todas las funciones y variables globales no exportadas** del módulo de la biblioteca como `hidden`, y los wrappers como `default` (visibilidad LLVM IR); `nm -g` solo muestra `_nolang_add/_nolang_scale/_nolang_sum3`, y los símbolos internos del runtime (`_nolang_rc_*`, `_nolang_async_*`, `@nolang_argc`, etc.) ya no son visibles. Los símbolos de enlace local (internal/private) no se exportan por definición y permanecen con visibilidad default |
| Propiedad e inicialización/limpieza del runtime de `str` / `vec` / `?T` | v1 lo rechaza explícitamente; se abrirá cuando exista una ABI de ciclo de vida (`init` / `shutdown` / borrow / free) |
| Colisión o ilegalidad en los nombres de símbolos exportados | Se comprueba identificador C válido, duplicados, palabras reservadas `main` / `_nolang_main` y símbolos internos de LLVM; cualquier conflicto es error duro |
| Deriva de tipos entre la cabecera y el wrapper | Ambos se generan a partir del mismo resultado de `cabiSignature` |
| Java necesita JNI | Se conserva una biblioteca JNI delgada; los otros seis lenguajes no requieren adaptador |


## Documentación relacionada

- [FFI: Nolang llama a C](./syntax) — `#{c}`, la dirección contraria
- [Exportación y visibilidad](./export) — `@` solo exporta entre paquetes Nolang
- [Compilación y ejecución](../usage) — banderas de `no build` y `--shared`
- [README del ejemplo local](https://github.com/lizongying/no/tree/main/example/host-interop) — flujo completo de `make verify`
