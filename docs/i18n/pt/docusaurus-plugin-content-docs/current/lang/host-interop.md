---
sidebar_position: 9
---

# Chamar o Nolang a partir de linguagens de hospedagem
> **Esta é uma capacidade nativa do compilador (`#{cabi}` v1, apenas escalares).** O `no build --shared` transforma funções de nível superior marcadas com `#{cabi='symbol'}` em símbolos C ABI estáveis e gera o cabeçalho C correspondente ao lado da biblioteca partilhada. Não é necessário shim escrito à mão, nem pós-processamento de LLVM IR, nem âncoras de acessibilidade. `#{c}` (o Nolang chama C), `@` (exportações entre pacotes Nolang) e `#{cabi}` (uma linguagem de hospedagem chama o Nolang) são três direções diferentes.


## Âmbito e limitações

Esta página cobre sete linguagens de hospedagem: C, C++, Rust, Go, Java, Python e C#. Todos os exemplos usam apenas as respetivas ferramentas e bibliotecas padrão — ligação direta, cgo, JNI, `ctypes`, `DllImport` — sem dependências de JNA, cffi, NuGet ou Maven. A demonstração usa apenas escalares `i64` / `f64`; `str`, `vec`, `?T`, structs e outros tipos que precisam de uma ABI de ciclo de vida são rejeitados com um erro de compilação rígido na v1. O código completo e executável está em `example/host-interop/`, onde o `make verify` compila, executa e compara a saída fixa das sete linguagens.

Verificado em macOS arm64 com clang 22, Apple g++, rustc 1.98, Go 1.27, OpenJDK 21, Python 3.13 e .NET 9. No Linux, substitui `.dylib` por `.so`, `DYLD_LIBRARY_PATH` por `LD_LIBRARY_PATH` e `@loader_path` por `$ORIGIN`; no Windows usa `.dll`.


## Como funciona: de `#{cabi}` a biblioteca partilhada

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


### A anotação de exportação e a acessibilidade

Coloca `#{cabi='nolang_add'}` antes da definição de uma função de nível superior para escolher o seu símbolo C público; com um `#{cabi}` simples, o compilador deriva `nolang_<nome-sanitizado>`. Um símbolo deve ser um identificador C válido, deve ser único e não pode usar `main` nem `_nolang_main`. Métodos, declarações externas e funções cujo número de resultados não é exatamente um produzem um diagnóstico fatal `cabi`.

O `LowerHIR` normalmente só expande as funções acessíveis a partir do `main` sintetizado; as funções sem referência nunca chegam ao IR, e chamadas de nível superior com argumentos todos constantes podem ainda ser eliminadas em comptime. O compilador acrescenta agora cada função `#{cabi}` como uma raiz de acessibilidade extra e marca-a como não-pura-em-comptime, pelo que o `main.no` da demonstração pode consistir apenas em três definições de função — sem âncoras nem chamadas falsas.


### Wrappers e o cabeçalho gerado

A ABI de chamada interna do Nolang é `define void @fn(args..., T* out)`: entradas por valor, o resultado escrito através de um out-pointer fornecido pelo chamador e um nome de símbolo interno que pode ganhar um sufixo por colisão. Nenhuma destas coisas é utilizável a partir de C. O compilador emite um wrapper com valor de retorno real para cada exportação:

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

A assinatura do wrapper e o cabeçalho são gerados a partir do **mesmo** resultado de `cabiSignature`, pelo que não podem divergir. O nome do wrapper é registado primeiro em `usedFname`, evitando colisões com helpers do runtime ou outras exportações.


### Modo de compilação de biblioteca partilhada

`--shared` faz o `no build` ligar o LLVM IR numa biblioteca dinâmica com `clang -shared -fPIC` (uma única passagem `-x ir`, pelo que o PIC se aplica tanto ao codegen como à ligação) e desativa a via da API C do LLVM. Um caminho de saída automático recebe o sufixo da plataforma `.dylib` / `.so` / `.dll`; um `-o` explícito é usado tal qual. `--header` substitui o caminho do cabeçalho, que por predefinição é o caminho de saída com a extensão trocada por `.h`. Uma compilação sem nenhuma exportação `#{cabi}` falha de forma evidente, em vez de produzir uma biblioteca sem API.

- `--shared` é incompatível com `--js` / `--wasm-direct` e com um alvo WASI (apenas backend LLVM).
- Uma biblioteca partilhada omite o `define i32 @main` sintetizado, para não interferir com o `main` do executável de hospedagem; as globais `@nolang_argc` / `@nolang_argv` de que `os.args` precisa continuam a ser emitidas.
- As compilações de executáveis não são afetadas: sem exportações `#{cabi}` não há wrapper nem cabeçalho, e o IR mantém-se inalterado.


## Tipos suportados (v1 escalares)

A v1 aceita exatamente estes cinco escalares por valor. A coluna C é o tipo de declaração escrito no cabeçalho gerado.

| Nolang | LLVM IR | Cabeçalho C | wrapper |
| --- | --- | --- | --- |
| `i64` | `i64` | `int64_t` | `i64` |
| `i32` | `i32` | `int32_t` | `i32` |
| `i8` | `i8` | `int8_t` | `i8` |
| `bool` | `i1` | `uint8_t` | `i8`（`trunc` / `zext`） |
| `f64` | `double` | `double` | `double` |

`bool` é `i1` em LLVM, mas C não tem um tipo portável de um bit, pelo que é exposto como `uint8_t`, com `trunc` / `zext` na fronteira do wrapper. `i128`, `str` / `txt`, `vec` / `[]T`, `map`, `?T`, structs, ponteiros, arrays fixos e qualquer valor que detenha memória na heap são rejeitados por `cabiTypeOf`; a mensagem de erro identifica o parâmetro ou tipo de resultado ofensor.


## Estrutura de diretórios

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

O antigo diretório `shim/` (adaptador e cabeçalho C escritos à mão) e o `abi_guard.py` (proteção contra desvio da ABI interna) foram removidos: o compilador passou a ser responsável pelos símbolos exportados, pelos wrappers e pelo cabeçalho.


## Passo 1: a biblioteca de demonstração Nolang

Três funções escalares, sem instruções de nível superior. O valor de `#{cabi='...'}` é o nome da função C que a hospedagem vê.

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


## Passo 2: `no build --shared`

As opções devem vir **antes** do ficheiro (`-o OUT FILE`). Este passo produz simultaneamente a biblioteca dinâmica e o cabeçalho:

```bash
cd example/host-interop
mkdir -p dist
no build --shared -o dist/libnolang.dylib main.no
# [shared] dist/libnolang.dylib + dist/libnolang.h (3 exports)
# Build successful: dist/libnolang.dylib
```

> É necessário um `no` que inclua `#{cabi}` / `--shared`. Se o `bin/no` do repositório ainda for antigo, o alvo `lib` do Makefile avisa claramente; podes usar `make verify NO=/path/to/no` para apontar para um compilador recém-compilado, sem precisar de substituir o `bin/no` partilhado.

O cabeçalho gerado, pronto para `#include`:

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

O alvo `lib` do Makefile é exatamente esse passo, mais uma verificação com `nm -g` de que os três símbolos estáveis estão realmente exportados:

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

| Opção | Efeito |
| --- | --- |
| `--shared` | Compila uma biblioteca dinâmica com `-shared -fPIC`; exige pelo menos uma exportação `#{cabi}` |
| `-o PATH` | Caminho de saída; determina também o nome predefinido do cabeçalho |
| `--header PATH` | Substitui o caminho de saída do cabeçalho (predefinição: `<saída-sem-extensão>.h`) |


## Exemplos para sete linguagens de hospedagem

| Linguagem | Ligação |
| --- | --- |
| C / C++ | `#include "libnolang.h"` direto mais `-lnolang` |
| Rust | `#[link(name = "nolang")] unsafe extern "C"` |
| Go | cgo `#cgo CFLAGS` / `#cgo LDFLAGS` |
| Java | JNI (JDK 21; não o Panama FFM em preview) |
| Python | `ctypes.CDLL(caminho_absoluto)` da biblioteca padrão |
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

O cgo inclui diretamente o cabeçalho gerado; `${SRCDIR}` mantém o caminho independente do diretório de trabalho.

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

O Java não consegue chamar uma ABI C diretamente, pelo que ainda precisa de uma camada de cola JNI; as outras seis linguagens não precisam de nenhum adaptador. O `javac -h` gera `NolangDemo.h` e a biblioteca JNI liga-se de volta a `-lnolang`.

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

Especifica explicitamente `argtypes` / `restype` para que inteiros de 64 bits não sejam truncados para um `int` C.

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

O runtime reforçado (hardened runtime) do macOS pode ignorar `DYLD_LIBRARY_PATH`, pelo que se passa um caminho absoluto via `NOLANG_LIB` e este é resolvido com `SetDllImportResolver`.

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


## Verificação

Caminhos de pesquisa de biblioteca: o macOS usa `DYLD_LIBRARY_PATH` / `@rpath` / `@loader_path`, o Linux usa `LD_LIBRARY_PATH` / `$ORIGIN`; o Java precisa além disso de `-Djava.library.path`; o .NET sob runtime reforçado usa `NOLANG_LIB` para um caminho absoluto. O `make verify` compila a biblioteca e os sete programas de hospedagem, recolhe as saídas em `dist/actual.txt` e compara com `expected.txt`.

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


## Limitações, riscos e mitigações

| Risco / limitação | Tratamento atual |
| --- | --- |
| A ABI interna (out-pointer, símbolos sanitizados) não é chamável a partir de C | O compilador emite wrappers com retorno normal; as hospedagens dependem apenas dos símbolos `#{cabi}` e do cabeçalho gerado |
| Funções sem referência eliminadas pela acessibilidade ou reduzidas em comptime | As exportações são raízes `LowerHIR` extra e são marcadas como não-redutíveis; deixaram de ser necessárias âncoras `getpid()` |
| Uma biblioteca partilhada que exporta `main` colide com a hospedagem | `--shared` omite o `define i32 @main` sintetizado |
| Só os três símbolos `#{cabi}` são exportados da biblioteca partilhada | Para `--shared`, o codegen marca todas as funções e globais **não exportadas** como `hidden` e os wrappers como `default` (visibilidade LLVM IR); o `nm -g` mostra apenas `_nolang_add` / `_nolang_scale` / `_nolang_sum3`, e os símbolos internos do runtime (`_nolang_rc_*`, `_nolang_async_*`, `@nolang_argc`, etc.) deixam de estar visíveis. Símbolos com ligação local (internal/private) já não eram exportados e mantêm a visibilidade default |
| Propriedade e inicialização/limpeza do runtime para `str` / `vec` / `?T` | Rejeitado explicitamente na v1; necessita de uma ABI de ciclo de vida (`init` / `shutdown` / borrow / free) |
| Nomes de símbolo exportado inválidos ou em colisão | Verificam-se identificador C válido, duplicação, palavras reservadas `main` / `_nolang_main` e colisões com símbolos LLVM; qualquer conflito é erro rígido |
| Tipos do cabeçalho e do wrapper divergirem | Ambos são gerados a partir do mesmo resultado de `cabiSignature` |
| O Java precisa de JNI | Mantém-se uma biblioteca JNI fina; as outras seis linguagens não têm adaptador |


## Documentação relacionada

- [FFI: o Nolang chama C](./syntax) — `#{c}`, a direção oposta
- [Exportações e visibilidade](./export) — `@` só exporta entre pacotes Nolang
- [Compilar e executar](../usage) — opções de `no build` e `--shared`
- [README do exemplo](https://github.com/lizongying/no/tree/main/example/host-interop) — o fluxo completo de `make verify`
