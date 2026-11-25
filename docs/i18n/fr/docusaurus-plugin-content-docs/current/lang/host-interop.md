---
sidebar_position: 9
---

# Appeler Nolang depuis les langages hôtes
> **Il s'agit d'une capacité native du compilateur (`#{cabi}` v1, version scalaire).** `no build --shared` exporte les fonctions de premier niveau marquées `#{cabi='symbol'}` en symboles ABI C stables et génère automatiquement l'en-tête C correspondant à côté de la bibliothèque partagée. Aucun shim écrit à la main, aucune analyse de LLVM IR, aucun point d'ancrage de joignabilité. `#{c}` (Nolang appelle C), `@` (export entre paquets Nolang) et `#{cabi}` (un hôte appelle Nolang) sont trois directions distinctes.


## Champ d'application et limites

Cet article couvre sept langages hôtes : C, C++, Rust, Go, Java, Python et C#. Tous n'utilisent que leur propre chaîne d'outils et bibliothèque standard : édition de liens directe, cgo, JNI, `ctypes`, `DllImport` — sans dépendance JNA, cffi, NuGet ou Maven. Les exemples n'utilisent que des scalaires `i64` / `f64` ; les types `str`, `vec`, `?T`, struct et autres qui nécessitent une ABI de cycle de vie sont rejetés par une erreur de compilation bloquante en v1. Le code complet exécutable se trouve dans `example/host-interop/` ; `make verify` compile, exécute et compare la sortie figée de chacun des sept.

Validé en conditions réelles sur macOS arm64 : clang 22, Apple g++, rustc 1.98, Go 1.27, OpenJDK 21, Python 3.13 et .NET 9. Sur Linux, il suffit de remplacer `.dylib` par `.so`, `DYLD_LIBRARY_PATH` par `LD_LIBRARY_PATH` et `@loader_path` par `$ORIGIN` ; sur Windows, on utilise `.dll`.


## Principe : de `#{cabi}` à la bibliothèque partagée

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


### Annotation d'export et joignabilité

Écrivez `#{cabi='nolang_add'}` avant une définition de fonction de premier niveau pour choisir son symbole C public ; avec un `#{cabi}` nu, le compilateur dérive `nolang_<nom-assaini>`. Un symbole doit être un identifiant C valide, unique, et ne peut pas usurper `main` ni `_nolang_main`. Les méthodes, les déclarations externes et les fonctions dont le nombre de résultats n'est pas exactement un produisent un diagnostic `cabi` bloquant.

`LowerHIR` ne déplie normalement que les fonctions joignables depuis le `main` synthétisé ; les fonctions non référencées n'atteignent jamais l'IR, et les appels de premier niveau à arguments tous constants peuvent en outre être repliés à la compilation. Le compilateur ajoute désormais chaque fonction `#{cabi}` comme racine de joignabilité supplémentaire et la marque comme non repliable à la compilation, si bien que le `main.no` de l'exemple peut ne consister qu'en trois définitions de fonctions — sans ancrages ni appels factices.


### Wrappers et en-tête généré

L'ABI d'appel interne de Nolang est `define void @fn(args..., T* out)` : entrées par valeur, résultat écrit via un out-pointer fourni par l'appelant, et un nom de symbole interne qui peut recevoir un suffixe en cas de collision. Ni l'un ni l'autre n'est utilisable depuis C. Le compilateur émet pour chaque export un wrapper à valeur de retour réelle :

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

La signature du wrapper et l'en-tête sont produits à partir du MÊME résultat de `cabiSignature` : ils ne peuvent donc pas dériver. Le nom du wrapper est d'abord enregistré dans `usedFname`, ce qui évite les collisions avec les helpers du runtime ou d'autres exports.


### Mode de compilation de bibliothèque partagée

`--shared` fait que `no build` édite l'IR LLVM en bibliothèque dynamique avec `clang -shared -fPIC` (une seule passe `-x ir`, si bien que le PIC s'applique aussi bien au codegen qu'à l'édition de liens) et désactive le chemin de l'API C LLVM. Un chemin de sortie automatique reçoit le suffixe de plateforme `.dylib` / `.so` / `.dll` ; un `-o` explicite est utilisé tel quel. `--header` permet de remplacer le chemin de l'en-tête, qui vaut par défaut le chemin de sortie sans son extension, suivi de `.h`. Sans aucun export `#{cabi}`, la compilation échoue bruyamment plutôt que de produire une bibliothèque sans API.

- `--shared` est incompatible avec `--js` / `--wasm-direct` et avec une cible WASI (backend LLVM uniquement).
- Une bibliothèque partagée omet le `define i32 @main` synthétisé, afin de ne pas interférer avec le `main` de l'exécutable hôte ; les variables globales `@nolang_argc` / `@nolang_argv` nécessaires à `os.args` sont tout de même émises.
- Les compilations d'exécutables ne sont pas affectées : sans export `#{cabi}`, aucun wrapper ni en-tête n'est produit, et l'IR reste inchangée.


## Types pris en charge (scalaires v1)

La v1 accepte exactement les cinq scalaires suivants, passés par valeur. La colonne C correspond au type de déclaration écrit dans l'en-tête généré.

| Nolang | LLVM IR | En-tête C | wrapper |
| --- | --- | --- | --- |
| `i64` | `i64` | `int64_t` | `i64` |
| `i32` | `i32` | `int32_t` | `i32` |
| `i8` | `i8` | `int8_t` | `i8` |
| `bool` | `i1` | `uint8_t` | `i8`（`trunc` / `zext`） |
| `f64` | `double` | `double` | `double` |

`bool` est `i1` en LLVM, mais C n'a pas de type portable sur un bit : il est donc exposé vers l'extérieur en `uint8_t`, le wrapper effectuant `trunc` / `zext` à la frontière. `i128`, `str` / `txt`, `vec` / `[]T`, `map`, `?T`, les structs, les pointeurs, les tableaux de taille fixe et toute valeur possédant de la mémoire sur le tas sont rejetés par `cabiTypeOf` ; le message d'erreur indique quel paramètre ou type de résultat est en cause.


## Structure du répertoire

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

L'ancien répertoire `shim/` (adaptateur C et en-tête écrits à la main) et `abi_guard.py` (protection contre la dérive de l'ABI interne) ont été supprimés : les symboles exportés, les wrappers et l'en-tête sont désormais tous générés par le compilateur.


## Première étape : la bibliothèque d'exemple Nolang

Trois fonctions scalaires, sans aucune instruction de premier niveau. La valeur de `#{cabi='...'}` est le nom de fonction C tel que le voit l'hôte.

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


## Deuxième étape : `no build --shared`

Les indicateurs doivent être placés AVANT le fichier (`-o OUT FILE`). Cette étape produit à la fois la bibliothèque dynamique et l'en-tête :

```bash
cd example/host-interop
mkdir -p dist
no build --shared -o dist/libnolang.dylib main.no
# [shared] dist/libnolang.dylib + dist/libnolang.h (3 exports)
# Build successful: dist/libnolang.dylib
```

> Cela nécessite un `no` qui prenne en charge `#{cabi}` / `--shared`. Si le `bin/no` du dépôt est plus ancien, la cible `lib` du Makefile le signale clairement ; utilisez `make verify NO=/path/to/no` pour désigner un compilateur fraîchement construit, sans écraser le `bin/no` partagé.

L'en-tête généré, prêt à être `#include` :

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

La cible `lib` du Makefile fait exactement cette étape, avec en plus une assertion `nm -g` vérifiant que les trois symboles stables sont réellement exportés :

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

| Indicateur | Effet |
| --- | --- |
| `--shared` | Construit une bibliothèque dynamique avec `-shared -fPIC` ; nécessite au moins un export `#{cabi}` |
| `-o PATH` | Chemin de sortie ; détermine aussi le nom de l'en-tête généré |
| `--header PATH` | Remplace le chemin de sortie de l'en-tête (par défaut `<sortie-sans-extension>.h`) |


## Exemples pour sept langages hôtes

| Langage | Mode de liaison |
| --- | --- |
| C / C++ | `#include "libnolang.h"` direct + édition de liens `-lnolang` |
| Rust | `#[link(name = "nolang")] unsafe extern "C"` |
| Go | cgo `#cgo CFLAGS` / `#cgo LDFLAGS` |
| Java | JNI (JDK 21 ; pas le Panama FFM en preview) |
| Python | `ctypes.CDLL(chemin_absolu)` de la bibliothèque standard |
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

cgo inclut directement l'en-tête généré ; `${SRCDIR}` rend le chemin indépendant du répertoire de travail.

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

Java ne peut pas appeler directement une ABI C : il a tout de même besoin d'une couche de glue JNI ; les six autres langages n'ont besoin d'aucun adaptateur. `javac -h` génère `NolangDemo.h`, et la bibliothèque JNI s'édite à nouveau avec `-lnolang`.

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

Déclarez explicitement `argtypes` / `restype` afin que les entiers 64 bits ne soient pas tronqués en `int` C.

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

Un runtime hardened de macOS peut ignorer `DYLD_LIBRARY_PATH` : on passe donc un chemin absolu via `NOLANG_LIB`, résolu par `SetDllImportResolver`.

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


## Vérification

Chemins de recherche des bibliothèques : macOS utilise `DYLD_LIBRARY_PATH` / `@rpath` / `@loader_path`, Linux `LD_LIBRARY_PATH` / `$ORIGIN` ; Java a en plus besoin de `-Djava.library.path` ; .NET, sous un runtime hardened, utilise `NOLANG_LIB` pour passer un chemin absolu. `make verify` construit la bibliothèque et les sept programmes hôtes, collecte leur sortie dans `dist/actual.txt`, puis la compare ligne à ligne avec `expected.txt`.

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


## Limites, risques et mesures d'atténuation

| Risque / limite | Traitement actuel |
| --- | --- |
| L'ABI interne (out-pointer, symboles assainis) n'est pas appelable depuis C | Le compilateur émet des wrappers à retour normal ; les hôtes ne dépendent que des symboles `#{cabi}` et de l'en-tête généré |
| Les fonctions non référencées sont écartées par l'analyse de joignabilité ou repliées à la compilation | Les exports sont des racines `LowerHIR` supplémentaires marquées non repliables ; fin des ancrages `getpid()` |
| Une bibliothèque partagée exportant `main` entre en conflit avec l'hôte | `--shared` omet le `define i32 @main` synthétisé |
| Seuls les trois symboles `#{cabi}` sont exportés de la bibliothèque partagée | Pour `--shared`, le codegen marque `hidden` **toutes les fonctions et variables globales non exportées** du module de la bibliothèque partagée, et `default` les wrappers (visibilité LLVM IR) ; `nm -g` ne laisse plus que `_nolang_add` / `_nolang_scale` / `_nolang_sum3` ; les symboles internes du runtime (`_nolang_rc_*`, `_nolang_async_*`, `@nolang_argc`, etc.) ne sont plus visibles. Les symboles à liaison locale (internal/private) ne sont de toute façon pas exportés et gardent leur visibilité default |
| Propriété et initialisation/nettoyage du runtime pour `str` / `vec` / `?T` | Rejet explicite en v1 ; ouverture après une ABI de cycle de vie (`init` / `shutdown` / borrow / free) |
| Noms de symboles exportés invalides ou en collision | Vérification des identifiants C valides, des doublons, des mots réservés `main` / `_nolang_main` et des symboles internes LLVM ; tout conflit est une erreur bloquante |
| Dérive entre l'en-tête et les types du wrapper | Les deux sont produits à partir du même résultat de `cabiSignature` |
| Java nécessite JNI | Une fine bibliothèque JNI subsiste ; les six autres langages n'ont aucun adaptateur |


## Documentation associée

- [FFI : Nolang appelle C](./syntax) — `#{c}`, la direction opposée
- [Exports et visibilité](./export) — `@` n'exporte qu'entre paquets Nolang
- [Compilation et exécution](../usage) — indicateurs de `no build` et `--shared`
- [README de l'exemple local](https://github.com/lizongying/no/tree/main/example/host-interop) — le parcours complet de `make verify`
