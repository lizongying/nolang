# 從七種宿主語言呼叫 Nolang

C、C++、Rust、Go、Java、Python、C# 呼叫同一個 Nolang 共享庫的可運行示例。

```bash
cd example/host-interop
make verify          # 一鍵：建庫 + 編譯七種語言 + 比對固定輸出
# 若 bin/no 尚未重建：make verify NO=/path/to/no
```

成功時輸出 `ALL SEVEN HOST LANGUAGES PASSED`，`dist/actual.txt` 與
`expected.txt` 逐字節一致：

```text
c 42 10.0 42
cpp 42 10.0 42
rust 42 10.0 42
go 42 10.0 42
java 42 10.0 42
python 42 10.0 42
csharp 42 10.0 42
```

> **這是編譯器原生能力，不是 workaround。**
> `no build --shared` 會把 `#{cabi='symbol'}` 標記的頂層函數導出成穩定的
> C ABI 符號，並自動產生 C 標頭檔。不再需要手寫 shim、解析 LLVM IR、
> 或用 `getpid()` 之類的運行期錨點把函數「釘」在可達圖裡。
> 需使用包含 `#{cabi}` / `--shared` 的 `no`（此功能目前尚無穩定版號）。

## 原理

```
main.no  --(#{cabi='nolang_add'})-->  HIR annotation
   |  LowerHIR: 導出函數 = 額外可達性根（且不 comptime 折疊）
   v
MIR Module.CABIExports
   |  codegen: 正常返回值的 wrapper + 同一份簽章生成的 C 標頭
   v
no build --shared  (clang -shared -fPIC，不發射 C main)
   -> dist/libnolang.dylib | .so | .dll  +  dist/libnolang.h
   -> C / C++ / Rust / Go(cgo) / Python(ctypes) / C#(P/Invoke)
   -> Java -> jni/nolang_jni.c -> libnolangjni -> -lnolang
```

Nolang 內部 ABI 是 `define void @fn(args..., T* out)`（結果走 out-pointer），
編譯器為每個導出函數發射一層正常返回值的 wrapper，例如：

```llvm
define i64 @nolang_add(i64 %a0, i64 %a1) {
entry:
  %cabi.r = alloca i64
  call void @nl_add(i64 %a0, i64 %a1, i64* %cabi.r)
  %cabi.v = load i64, i64* %cabi.r
  ret i64 %cabi.v
}
```

wrapper 與 `dist/libnolang.h` 由同一份簽章解析結果產生，不會漂移。

## v1 限制

只支援按值傳遞的純量：`i64` / `i32` / `i8` / `bool`（對外為 `uint8_t`）/ `f64`。
`str`、`txt`、`vec`、`[]T`、`map`、`?T`、struct、`i128`、指標與任何持有堆記憶體的
值都會被編譯器硬錯誤拒絕——它們需要一套生命週期 ABI（init / shutdown / borrow /
free），目前尚未定義。導出函數必須是頂層函數、恰好一個結果、C 符號合法且不重複。

共享庫只導出三個 `#{cabi}` 穩定符號（`nm -g` 只剩 `nolang_add/scale/sum3`），
不導出合成的 C `main`，也不導出任何 Nolang 內部／runtime 符號——`--shared`
在 codegen 階段把共享庫模組內所有非導出函數與全域變數標 `hidden` visibility，
wrapper 標 `default`。宿主程式只需依賴 `dist/libnolang.h` 的宣告。

## 目錄

| 路徑 | 內容 |
| --- | --- |
| `main.no`, `package.noml` | Nolang 示例庫（3 個 `#{cabi}` 純量函數） |
| `jni/nolang_jni.c`, `java/NolangDemo.java` | Java JNI 膠水（唯一保留的轉接層） |
| `c/`, `cpp/`, `rust/`, `go/`, `python/`, `csharp/` | 七種宿主示例 |
| `Makefile`, `expected.txt` | 一鍵驗證 |
| `dist/` | 生成物：動態庫、`libnolang.h`、各語言可執行檔（已被 `.gitignore` 忽略） |

已刪除：`shim/nolang.h`、`shim/nolang_shim.c`、`abi_guard.py`。

## 綁定方式（零第三方依賴）

| 語言 | 方式 |
| --- | --- |
| C / C++ | 直接 `#include "libnolang.h"` 並鏈 `-lnolang` |
| Rust | `#[link(name = "nolang")] unsafe extern "C"` |
| Go | cgo `#cgo CFLAGS/LDFLAGS` |
| Java | JNI（JDK 21；不使用 preview 的 Panama FFM） |
| Python | 標準庫 `ctypes.CDLL(絕對路徑)`，明確指定 `argtypes` / `restype` |
| C# | `DllImport` + `NativeLibrary.SetDllImportResolver` |

## 常用目標

```bash
make lib      # no build --shared：動態庫 + 標頭，並用 nm -g 斷言 3 個導出符號
make jni      # 額外產出 libnolangjni
make verify   # 七種語言實跑 + diff
make clean    # 刪除 dist/
```

函式庫搜尋路徑：macOS 用 `DYLD_LIBRARY_PATH` / `@loader_path`，Linux 用
`LD_LIBRARY_PATH` / `$ORIGIN`；Java 另需 `-Djava.library.path`；.NET 在
hardened runtime 下可能忽略 `DYLD_LIBRARY_PATH`，故以 `NOLANG_LIB`
環境變數傳絕對路徑。

`--shared` 另可用 `--header PATH` 覆寫標頭位置；與 `--js` / `--wasm-direct`、
WASI target 互斥；沒有任何 `#{cabi}` 導出時會硬失敗。

## 工具鏈兜底

| 情況 | 處理 |
| --- | --- |
| `go: command not found` | `export PATH="/opt/homebrew/bin:$PATH"`（Makefile 已處理） |
| `g++` 缺失 | 把 `cpp` 目標改成 `clang++ -std=c++17` |
| JDK 缺失 / `JAVA_HOME` 為空 | 安裝 JDK（JNI 需要 `jni.h` 與 `javac -h`）；Makefile 會從 `javac` 路徑推導 |
| `dotnet` restore 被網路阻斷 | `dotnet build --no-restore -c Release` |
| macOS `code signature invalid` | `codesign -s - --force dist/*.dylib`（Makefile 已處理） |
| 下載後被隔離 | `xattr -d com.apple.quarantine dist/*.dylib` |

## 已驗證環境

macOS arm64 · clang 22.1.8 · Apple g++ · rustc 1.98.1 · go 1.27.3 ·
OpenJDK 21.0.6 · Python 3.13 · .NET 9.0.106 · `no`（含 `--shared` 的開發版）。

完整說明見文件頁 [從宿主語言調用 Nolang](../../docs/docs/lang/host-interop.md)。
