---
sidebar_position: 9
---

# Nolang aus Host-Sprachen aufrufen
> **Dies ist eine native Fähigkeit des Compilers (`#{cabi}` v1, Skalarversion).** `no build --shared` exportiert mit `#{cabi='symbol'}` markierte Top-Level-Funktionen als stabile C-ABI-Symbole und erzeugt neben der gemeinsam genutzten Bibliothek automatisch eine C-Headerdatei. Es ist kein handgeschriebener Shim, kein Parsen von LLVM IR und kein Reachability-Anker nötig. `#{c}` (Nolang ruft C auf), `@` (Export zwischen Nolang-Paketen) und `#{cabi}` (Host ruft Nolang auf) sind drei Fähigkeiten mit unterschiedlicher Richtung.


## Anwendungsbereich und Einschränkungen

Dieser Artikel behandelt sieben Host-Sprachen: C, C++, Rust, Go, Java, Python und C#. Alle verwenden ausschließlich ihre jeweilige Toolchain und Standardbibliothek: direktes Linken, cgo, JNI, `ctypes` und `DllImport`; JNA, cffi, NuGet- oder Maven-Abhängigkeiten sind nicht nötig. Die Beispiele verwenden nur Skalare vom Typ `i64`／`f64`; Typen wie `str`, `vec`, `?T` und struct, die eine Lifecycle-ABI benötigen, werden in v1 vom Compiler mit einem harten Fehler abgelehnt. Der vollständige lauffähige Code liegt in `example/host-interop/`; `make verify` kompiliert, führt aus und vergleicht die Ausgabe mit festen Erwartungswerten.

Verifiziert wurde dies durch tatsächliche Läufe auf macOS arm64 mit clang 22, Apple g++, rustc 1.98, Go 1.27, OpenJDK 21, Python 3.13 und .NET 9. Unter Linux genügt es, `.dylib` durch `.so`, `DYLD_LIBRARY_PATH` durch `LD_LIBRARY_PATH` und `@loader_path` durch `$ORIGIN` zu ersetzen; Windows verwendet `.dll`.


## Funktionsweise: Von `#{cabi}` zur gemeinsam genutzten Bibliothek

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


### Export-Annotation und Erreichbarkeit

Schreibt man vor die Definition einer Top-Level-Funktion `#{cabi='nolang_add'}`, wird damit das nach außen sichtbare C-Symbol festgelegt; lässt man den Wert weg und schreibt `#{cabi}`, leitet der Compiler es aus `nolang_` plus dem bereinigten Funktionsnamen ab. Das Symbol muss ein gültiger C-Bezeichner sein, darf nicht doppelt vorkommen und darf nicht `main` / `_nolang_main` belegen. Methoden, externe Deklarationen und Funktionen mit einer anderen Ergebnisanzahl als 1 werden als harte Fehler der Kategorie `cabi` diagnostiziert.

`LowerHIR` expandierte erreichbare Funktionen ursprünglich nur vom synthetisierten `main` aus über Aufrufstellen; nicht aufgerufene Funktionen gelangten gar nicht erst in die IR, und vollständig konstante Top-Level-Aufrufe wurden zusätzlich zur Kompilierzeit gefaltet. Der Compiler fügt nun jede `#{cabi}`-Funktion als zusätzlichen Erreichbarkeits-Wurzelpunkt hinzu und markiert sie als nicht-kompilierzeitfaltbar, sodass `main.no` im Beispiel nur drei Funktionsdefinitionen enthalten kann, ohne Anker oder Scheinaufrufe.


### Wrapper und automatisch generierte Headerdatei

Nolangs interne Aufruf-ABI lautet `define void @fn(args..., T* out)`: Eingaben werden nach Wert übergeben, das Ergebnis in einen vom Aufrufer bereitgestellten Out-Pointer geschrieben, und der Funktionsname ist ein interner Name, der bei Kollisionen ein Suffix erhalten kann. Beides ist für C-Aufrufe ungeeignet. Der Compiler emittiert für jede exportierte Funktion einen Wrapper mit normaler Rückgabe:

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

Signatur des Wrappers und Headerdatei werden aus demselben Analyseergebnis von `cabiSignature` erzeugt, sodass die beiden nicht auseinanderdriften können; der Wrapper-Name wird zuvor in `usedFname` registriert, um Namenskollisionen mit Runtime-Helpern oder anderen Exportsymbolen zu vermeiden.


### Build-Modus für gemeinsam genutzte Bibliotheken

`--shared` veranlasst `no build`, die LLVM IR direkt mit `clang -shared -fPIC` zu einer dynamischen Bibliothek zu linken (ein einzelner `-x ir`-Durchlauf, PIC wirkt sowohl bei Codegen als auch beim Linken), und deaktiviert den LLVM-C-API-Pfad. Der automatische Ausgabepfad erhält je nach Plattform `.dylib` / `.so` / `.dll`; ein explizites `-o` wird unverändert verwendet. Mit `--header` lässt sich der Headerpfad überschreiben; standardmäßig wird die Erweiterung des Ausgabepfads entfernt und `.h` angehängt. Ohne `#{cabi}`-Export schlägt der Build hart fehl, um keine Bibliothek ohne API zu erzeugen.

- `--shared` schließt sich mit `--js` / `--wasm-direct` und WASI-Targets gegenseitig aus (nur das LLVM-Backend wird unterstützt).
- Die gemeinsam genutzte Bibliothek emittiert kein synthetisches `define i32 @main`, um nicht mit dem `main` der Host-Anwendung zu kollidieren; die für `os.args` nötigen globalen Variablen `@nolang_argc` / `@nolang_argv` bleiben erhalten.
- Der Build ausführbarer Dateien bleibt vollständig unberührt: Ohne `#{cabi}` werden weder Wrapper noch Headerdateien erzeugt und die IR bleibt unverändert.


## Unterstützte Typen (v1: Skalare)

v1 akzeptiert nur die folgenden fünf nach Wert übergebenen Skalare. Die C-Typen in der Tabelle sind die Deklarationstypen in der erzeugten Headerdatei.

| Nolang | LLVM IR | C-Header | Wrapper |
| --- | --- | --- | --- |
| `i64` | `i64` | `int64_t` | `i64` |
| `i32` | `i32` | `int32_t` | `i32` |
| `i8` | `i8` | `int8_t` | `i8` |
| `bool` | `i1` | `uint8_t` | `i8`（`trunc` / `zext`） |
| `f64` | `double` | `double` | `double` |

`bool` ist in LLVM ein `i1`, doch C kennt keinen portablen 1-Bit-Typ; nach außen wird es daher auf `uint8_t` erweitert, und der Wrapper führt an der Grenze `trunc`／`zext` durch. `i128`, `str`／`txt`, `vec`／`[]T`, `map`, `?T`, struct, Zeiger, feste Arrays sowie alle Werte, die Heap-Speicher halten, werden von `cabiTypeOf` abgelehnt; die Fehlermeldung nennt den nicht unterstützten Parameter- oder Ergebnistyp.


## Verzeichnisstruktur

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

Die `shim/`-Dateien der früheren Beispiele (handgeschriebener C-Adapter und Header) sowie `abi_guard.py` (Schutz vor interner ABI-Drift) wurden entfernt: Exportsymbole, Wrapper und Headerdateien werden nun vollständig vom Compiler erzeugt.


## Schritt 1: Die Nolang-Beispielbibliothek

Drei Skalarfunktionen ohne Top-Level-Anweisungen. Der Wert von `#{cabi='...'}` ist der C-Funktionsname, wie ihn der Host sieht.

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


## Schritt 2: `no build --shared`

Das Flag muss vor der Datei stehen (`-o OUT FILE`). Dieser Schritt erzeugt sowohl die dynamische Bibliothek als auch die Headerdatei:

```bash
cd example/host-interop
mkdir -p dist
no build --shared -o dist/libnolang.dylib main.no
# [shared] dist/libnolang.dylib + dist/libnolang.h (3 exports)
# Build successful: dist/libnolang.dylib
```

> Es wird ein `no` benötigt, das `#{cabi}` / `--shared` unterstützt. Ist `bin/no` im Repository noch eine alte Version, gibt das Makefile-Ziel `lib` einen klaren Hinweis; mit `make verify NO=/path/to/no` lässt sich ein neuer Compiler angeben, ohne das gemeinsame `bin/no` zu überschreiben.

Die automatisch erzeugte Headerdatei kann direkt per `#include` eingebunden werden:

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

Das `lib`-Ziel des Makefiles führt genau diesen Schritt aus und prüft zusätzlich mit `nm -g`, dass die drei stabilen Symbole tatsächlich exportiert sind:

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

| Flag | Wirkung |
| --- | --- |
| `--shared` | Baut eine dynamische Bibliothek mit `-shared -fPIC`; erfordert mindestens einen `#{cabi}`-Export |
| `-o PATH` | Ausgabepfad; bestimmt den Namen der automatisch erzeugten Headerdatei |
| `--header PATH` | Überschreibt den Ausgabepfad der Headerdatei (Standard: `<Ausgabepfad ohne Erweiterung>.h`) |


## Beispiele für sieben Host-Sprachen

| Sprache | Bindungsart |
| --- | --- |
| C / C++ | Direktes `#include "libnolang.h"` und Linken mit `-lnolang` |
| Rust | `#[link(name = "nolang")] unsafe extern "C"` |
| Go | cgo `#cgo CFLAGS` / `#cgo LDFLAGS` |
| Java | JNI (JDK 21; ohne das Preview-Panama-FFM) |
| Python | Standardbibliothek `ctypes.CDLL(absoluter Pfad)` |
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

cgo bindet die erzeugte Headerdatei direkt ein; `${SRCDIR}` macht die Pfade unabhängig vom Arbeitsverzeichnis.

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

Java kann eine C-ABI nicht direkt aufrufen und benötigt weiterhin eine JNI-Klebeschicht; die anderen sechs Sprachen kommen ohne Adapter aus. `javac -h` erzeugt `NolangDemo.h`, und die JNI-Bibliothek wird anschließend gegen `-lnolang` gelinkt.

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

Die Typen `argtypes` / `restype` werden explizit angegeben, um zu verhindern, dass 64-Bit-Ganzzahlen als C-`int` abgeschnitten werden.

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

Die gehärtete macOS-Laufzeit kann `DYLD_LIBRARY_PATH` ignorieren; daher wird der absolute Pfad über `NOLANG_LIB` übergeben und von `SetDllImportResolver` aufgelöst.

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


## Verifizierung

Suchpfade für Bibliotheken: macOS verwendet `DYLD_LIBRARY_PATH` / `@rpath` / `@loader_path`, Linux `LD_LIBRARY_PATH` / `$ORIGIN`; Java benötigt zusätzlich `-Djava.library.path`; .NET verwendet unter gehärteter Laufzeit `NOLANG_LIB`, um den absoluten Pfad zu übergeben. `make verify` baut die Bibliothek, erstellt die sieben Host-Programme, sammelt deren Ausgabe in `dist/actual.txt` und vergleicht sie Zeile für Zeile mit `expected.txt`.

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


## Einschränkungen, Risiken und Gegenmaßnahmen

| Risiko / Einschränkung | Aktuelle Behandlung |
| --- | --- |
| Interne ABI (`out-pointer`, Symbolbereinigung) ist für direkte C-Aufrufe ungeeignet | Der Compiler emittiert einen Wrapper mit normaler Rückgabe; der Host hängt nur vom `#{cabi}`-Symbol und dem erzeugten Header ab |
| Nicht aufgerufene Funktionen werden von der Erreichbarkeitsanalyse verworfen oder zur Kompilierzeit gefaltet | Exportierte Funktionen dienen als zusätzliche `LowerHIR`-Wurzeln und werden als nicht faltbar markiert; der `getpid()`-Anker entfällt |
| Die gemeinsam genutzte Bibliothek exportiert `main` und kollidiert mit dem Host | `--shared` emittiert kein synthetisches `define i32 @main` |
| Die gemeinsam genutzte Bibliothek exportiert nur drei `#{cabi}`-Symbole | `--shared` markiert in der Codegen-Phase **alle nicht exportierten Funktionen und globalen Variablen** im Bibliotheksmodul als `hidden` und die Wrapper als `default` (LLVM-IR-Sichtbarkeit); `nm -g` zeigt nur noch `_nolang_add/_nolang_scale/_nolang_sum3`, interne Runtime-Symbole (`_nolang_rc_*`, `_nolang_async_*`, `@nolang_argc` usw.) sind unsichtbar. Symbole mit interner/privater Bindung werden ohnehin nicht exportiert und bleiben auf `default` |
| Besitzverhältnisse und Runtime-Initialisierung/-Bereinigung von `str` / `vec` / `?T` | In v1 ausdrücklich abgelehnt; Freigabe erst nach Vorhandensein einer Lifecycle-ABI (`init` / `shutdown` / borrow / free) |
| Kollidierende oder illegale Exportsymbolnamen | Prüfung auf gültige C-Bezeichner, Duplikate, reservierte Wörter `main` / `_nolang_main` und LLVM-interne Symbole; Kollision führt zum harten Fehler |
| Drift zwischen Headerdatei und Wrapper-Typen | Beide werden aus demselben Analyseergebnis von `cabiSignature` erzeugt |
| Java benötigt JNI | Eine dünne JNI-Bibliothek bleibt erhalten; die anderen sechs Sprachen benötigen keinen Adapter |


## Verwandte Dokumentation

- [FFI: Nolang ruft C auf](./syntax) — `#{c}`, entgegengesetzte Richtung
- [Export und Sichtbarkeit](./export) — `@` exportiert nur zwischen Nolang-Paketen
- [Bauen und Ausführen](../usage) — `no build`-Flags und `--shared`
- [README des lokalen Beispiels](https://github.com/lizongying/no/tree/main/example/host-interop) — vollständiger `make verify`-Ablauf
