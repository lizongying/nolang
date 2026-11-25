---
sidebar_position: 9
---

# ホスト言語から Nolang を呼び出す
> **これはコンパイラのネイティブ機能です（`#{cabi}` v1 スカラ版）。** `no build --shared` は `#{cabi='symbol'}` でマークされたトップレベル関数を安定した C ABI シンボルとしてエクスポートし、共有ライブラリと同じ場所に C ヘッダファイルを自動生成します。手書きの shim も、LLVM IR の解析も、到達性アンカーも不要です。`#{c}`（Nolang から C を呼び出す）、`@`（Nolang パッケージ間でエクスポート）、`#{cabi}`（ホストから Nolang を呼び出す）は、それぞれ方向が異なる三つの機能です。


## 適用範囲と制限

本文は C、C++、Rust、Go、Java、Python、C# の七つのホスト言語をカバーし、いずれも各言語のツールチェーンと標準ライブラリのみを使用します：直接リンク、cgo、JNI、`ctypes`、`DllImport` を用い、JNA、cffi、NuGet、Maven の依存は不要です。例では `i64`／`f64` のスカラのみを使用します；`str`、`vec`、`?T`、struct などライフタイム ABI を必要とする型は v1 ではコンパイラによってハードエラーで拒否されます。完全に動作するコードは `example/host-interop/` にあり、`make verify` が順にコンパイル、実行し、固定された出力と照合します。

macOS arm64 での実機検証済み：clang 22、Apple g++、rustc 1.98、Go 1.27、OpenJDK 21、Python 3.13、.NET 9。Linux では `.dylib` を `.so` に、`DYLD_LIBRARY_PATH` を `LD_LIBRARY_PATH` に、`@loader_path` を `$ORIGIN` に置き換えるだけです；Windows では `.dll` を使用します。


## 原理：`#{cabi}` から共有ライブラリへ

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


### エクスポートアノテーションと到達性

トップレベル関数の定義前に `#{cabi='nolang_add'}` と書くと、対外 C シンボルを指定できます；値を省略して `#{cabi}` と書いた場合、コンパイラは `nolang_` に関数名のサニタイズ結果を付けて推論します。シンボルは有効な C 識別子でなければならず、重複できず、`main` / `_nolang_main` を占有することもできません。メソッド、外部宣言、結果の数が 1 でない関数は、いずれも `cabi` 種別のハードエラーとして診断されます。

`LowerHIR` は元々、合成された `main` から呼び出し箇所をたどって到達可能な関数だけを展開していたため、呼び出されない関数はそもそも IR に入りませんでした；すべて定数であるトップレベル呼び出しは comptime で折りたたまれることもあります。コンパイラは現在、各 `#{cabi}` 関数を追加の到達性ルートとして登録し、comptime 折りたたみ不可とマークするため、例の `main.no` には三つの関数定義だけでよく、アンカーやダミー呼び出しは不要です。


### Wrapper と自動生成されるヘッダファイル

Nolang の内部呼び出し ABI は `define void @fn(args..., T* out)` です：入力は値渡し、結果は呼び出し側が用意した out-pointer に書き込まれ、関数名は衝突に応じて接尾辞が付く内部名のままです。どちらも C からの呼び出しには適していません。コンパイラは各エクスポート関数に対して、通常の戻り値を持つ wrapper を発行します：

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

wrapper のシグネチャとヘッダファイルは同じ `cabiSignature` の解析結果から生成されるため、両者が食い違うことはありません；wrapper 名は事前に `usedFname` へ登録され、runtime helper や他のエクスポートシンボルとの名前衝突を避けます。


### 共有ライブラリのビルドモード

`--shared` は `no build` に `clang -shared -fPIC` で LLVM IR を直接ダイナミックライブラリへリンクさせます（単回の `-x ir` フローで、PIC は codegen とリンクの両方に適用されます）、また LLVM C API 経路を無効化します。自動出力パスはプラットフォームに応じて `.dylib` / `.so` / `.dll` を付与します；明示的な `-o` はそのまま使用されます。`--header` でヘッダファイルの位置を上書きでき、デフォルトは出力パスから拡張子を除いたものに `.h` を付けたものです。`#{cabi}` のエクスポートが一つもない場合は即座にハード失敗し、API のないライブラリが生成されるのを防ぎます。

- `--shared` は `--js` / `--wasm-direct`、WASI ターゲットと排他です（LLVM バックエンドのみをサポート）。
- 共有ライブラリは合成された `define i32 @main` を発行せず、ホスト実行ファイルの `main` と干渉するのを防ぎます；`os.args` に必要な `@nolang_argc` / `@nolang_argv` のグローバル変数は引き続き保持されます。
- 実行ファイルのビルドには完全に影響しません：`#{cabi}` がなければ wrapper もヘッダファイルも生成されず、IR は変更されません。


## サポートされる型（v1 スカラ）

v1 では次の五つの値渡しのスカラのみを受け付けます。表内の C 型が、生成されるヘッダファイル内の宣言型そのものです。

| Nolang | LLVM IR | C ヘッダ | wrapper |
| --- | --- | --- | --- |
| `i64` | `i64` | `int64_t` | `i64` |
| `i32` | `i32` | `int32_t` | `i32` |
| `i8` | `i8` | `int8_t` | `i8` |
| `bool` | `i1` | `uint8_t` | `i8`（`trunc` / `zext`） |
| `f64` | `double` | `double` | `double` |

`bool` は LLVM では `i1` ですが、C には移植可能な 1-bit 型がないため、対外的には `uint8_t` に広げられ、境界で wrapper が `trunc`／`zext` を行います。`i128`、`str`／`txt`、`vec`／`[]T`、`map`、`?T`、struct、ポインタ、固定長配列、およびヒープメモリを保持する値はすべて `cabiTypeOf` に拒否され、エラーメッセージはどの引数または戻り値の型が非対応かを示します。


## ディレクトリ構造

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

旧版の例にあった `shim/`（手書きの C アダプタとヘッダ）と `abi_guard.py`（内部 ABI のドリフト防止）は削除されました：エクスポートシンボル、wrapper、ヘッダファイルはすべてコンパイラが生成するようになりました。


## ステップ 1：Nolang の例ライブラリ

三つのスカラ関数で、トップレベルの文は一切ありません。`#{cabi='...'}` の値が、ホストから見える C 関数名そのものです。

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


## ステップ 2：`no build --shared`

フラグはファイル名の前に置く必要があります（`-o OUT FILE`）。このステップでダイナミックライブラリとヘッダファイルを同時に生成します：

```bash
cd example/host-interop
mkdir -p dist
no build --shared -o dist/libnolang.dylib main.no
# [shared] dist/libnolang.dylib + dist/libnolang.h (3 exports)
# Build successful: dist/libnolang.dylib
```

> `#{cabi}` / `--shared` に対応した `no` が必要です。リポジトリの `bin/no` がまだ古い場合、Makefile の `lib` ターゲットが明確に通知します；`make verify NO=/path/to/no` で新しいコンパイラを指定でき、共有の `bin/no` を上書きする必要はありません。

自動生成されるヘッダファイルは、そのまま `#include` できます：

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

Makefile の `lib` ターゲットがこのステップを行い、さらに `nm -g` で三つの安定シンボルが実際にエクスポートされていることを検証します：

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

| フラグ | 作用 |
| --- | --- |
| `--shared` | `-shared -fPIC` でダイナミックライブラリをビルド；少なくとも一つの `#{cabi}` エクスポートが必要 |
| `-o PATH` | 出力パス；自動生成されるヘッダファイルの名前を決定 |
| `--header PATH` | ヘッダファイルの出力パスを上書き（デフォルトは `<出力から拡張子を除いたもの>.h`） |


## 七つのホスト言語の例

| 言語 | バインディング方法 |
| --- | --- |
| C / C++ | 直接 `#include "libnolang.h"` して `-lnolang` をリンク |
| Rust | `#[link(name = "nolang")] unsafe extern "C"` |
| Go | cgo `#cgo CFLAGS` / `#cgo LDFLAGS` |
| Java | JNI（JDK 21；preview の Panama FFM は使用しない） |
| Python | 標準ライブラリ `ctypes.CDLL(絶対パス)` |
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

cgo は生成されたヘッダファイルを直接 include します；`${SRCDIR}` によりパスは作業ディレクトリに依存しません。

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

Java は C ABI を直接呼び出せないため、やはり一層の JNI グルーが必要です；それ以外の六つの言語には変換層は不要です。`javac -h` が `NolangDemo.h` を生成し、JNI ライブラリがさらに `-lnolang` へリンクします。

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

64 ビット整数が C の `int` として切り詰められないよう、`argtypes` / `restype` を明示的に指定します。

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

macOS の hardened runtime は `DYLD_LIBRARY_PATH` を無視する場合があるため、`NOLANG_LIB` で絶対パスを渡し、`SetDllImportResolver` で解決します。

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


## 検証

ライブラリの検索パス：macOS では `DYLD_LIBRARY_PATH` / `@rpath` / `@loader_path`、Linux では `LD_LIBRARY_PATH` / `$ORIGIN` を使用；Java にはさらに `-Djava.library.path` が必要；.NET は hardened runtime 下で `NOLANG_LIB` により絶対パスを渡します。`make verify` はライブラリをビルドし、七つのホストプログラムをビルドし、その出力を `dist/actual.txt` に収めてから `expected.txt` と行単位で照合します。

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


## 制限、リスクと緩和

| リスク / 制限 | 現状の対応 |
| --- | --- |
| 内部 ABI（`out-pointer`、シンボルのサニタイズ）は C から直接呼び出すのに適さない | コンパイラが通常の戻り値を持つ wrapper を発行；ホストは `#{cabi}` シンボルと生成ヘッダのみに依存 |
| 呼び出されない関数が到達性解析で破棄されたり、comptime で折りたたまれたりする | エクスポート関数を追加の `LowerHIR` ルートとして扱い、折りたたみ不可とマーク；`getpid()` アンカーは不要に |
| 共有ライブラリが `main` をエクスポートしてホストと衝突する | `--shared` は合成された `define i32 @main` を発行しない |
| 共有ライブラリが三つの `#{cabi}` シンボルのみをエクスポート | `--shared` は codegen 段階で共有ライブラリモジュール内の**すべての非エクスポート関数とグローバル変数**に `hidden` を、wrapper に `default` を付与（LLVM IR の visibility）；`nm -g` には `_nolang_add/_nolang_scale/_nolang_sum3` のみが残り、内部 runtime シンボル（`_nolang_rc_*`、`_nolang_async_*`、`@nolang_argc` など）は不可視になります。ローカルリンケージ（internal/private）のシンボルは元々エクスポートされないため、default のまま |
| `str` / `vec` / `?T` の所有権と runtime の初期化／破棄 | v1 では明確に拒否；ライフタイム ABI（`init` / `shutdown` / borrow / free）を用意したうえで開放 |
| エクスポートシンボル名の衝突や不正 | 有効な C 識別子、重複、`main` / `_nolang_main` の予約語、LLVM 内部シンボルを検査し、衝突時はハードエラー |
| ヘッダファイルと wrapper の型の食い違い | 両者は同じ `cabiSignature` の解析結果から生成される |
| Java には JNI が必要 | 薄い JNI ライブラリを一層残す；他の六言語は変換層ゼロ |


## 関連ドキュメント

- [FFI：Nolang から C を呼び出す](./syntax) — `#{c}`、方向は逆
- [エクスポートと可視性](./export) — `@` は Nolang パッケージ間のみでエクスポート
- [ビルドと実行](../usage) — `no build` のフラグと `--shared`
- [ローカル例の README](https://github.com/lizongying/no/tree/main/example/host-interop) — `make verify` の完全な流れ
