# 從七種宿主語言呼叫 Nolang

C、C++、Rust、Go、Java、Python、C# 呼叫同一個 Nolang 共享庫的可運行示例。

```bash
cd example/host-interop
make verify          # 一鍵：建庫 + 編譯七種語言 + 比對固定輸出
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

> **這是 workaround，不是編譯器支援的特性。**
> `no build` 目前只能產出可執行檔，沒有 `-shared` / `-fPIC` / `-c` /
> header 生成；`#{c}` 是「Nolang 呼叫 C」的外部函數宣告，`@` 只是 Nolang
> 包之間的符號導出，**兩者都不是 C ABI 導出**。
> `docs/design/nolang-native-backend.md` 也把「動態庫產出」列為非目標。
> 本示例不改編譯器，而是把 `no build -v` 順便寫出的 LLVM IR 自己鏈成共享庫。
> 內部 ABI 不受官方保證，因此每次構建都由 `abi_guard.py` 校驗，漂移即硬失敗。

## 原理

```
main.no --(no build -v)--> dist/libnolang.ll
                              | abi_guard.py 校驗 3 個 define void @nl_*(...)
        clang -x ir -fPIC -c -> dist/nolang.o
shim/nolang_shim.c   -fPIC -c -> dist/nolang_shim.o
        clang -shared        -> dist/libnolang.dylib | .so
                                 ^
  C / C++ / Rust / Go(cgo) / Python(ctypes) / C#(P/Invoke) 直接呼叫穩定 API
  Java -> jni/nolang_jni.c -> libnolangjni -> -lnolang -> 同一穩定 API
```

`no build` 把使用者函數發射為
`define void @<name>(輸入按值, 結果 out-pointer)`，例如
`define void @nl_add(i64, i64, i64*)`。`shim/nolang_shim.c` 把它包成
`nolang_add(int64_t, int64_t)` 等三個真正的 C API，宿主只依賴這個穩定面。

### 兩個容易踩的坑

1. **可達性 + comptime 折疊**。`LowerHIR` 只從合成的 `main` 沿呼叫點展開，
   未被呼叫的函數根本不會出現在 `.ll`；而全常數的頂層呼叫又會被 comptime
   折疊掉。所以 `main.no` 用 `#{c}` 宣告 libc 的 `getpid` 取得一個**運行期**
   值 `seed`，三個錨點都消費它，函數才會被保留。錨點位於 `_nolang_main`，
   宿主永遠不會呼叫它。
2. **合成的 `define i32 @main`**。它總是出現在 `.ll` 裡；共享庫中的 `main`
   只是普通導出符號，dyld/ld.so 不會把它當入口，保留即可。

示例刻意**只用 `i64` / `f64` 標量**：`str` 是 `%str-long = {i64, i64, i8*}`，
其釋放路徑在永不執行的 `main` 尾聲，宿主側沒有對應的 free API，必然洩漏。

## 目錄

| 路徑 | 內容 |
| --- | --- |
| `main.no`, `package.noml` | Nolang 示例庫（3 個純量函數 + 可達性錨點） |
| `abi_guard.py` | 解析 `.ll`、校驗內部 ABI、漂移即失敗 |
| `shim/nolang.h`, `shim/nolang_shim.c` | 穩定 C API |
| `jni/nolang_jni.c`, `java/NolangDemo.java` | Java JNI 膠水 |
| `c/`, `cpp/`, `rust/`, `go/`, `python/`, `csharp/` | 七種宿主示例 |
| `Makefile`, `expected.txt` | 一鍵驗證 |
| `dist/` | 生成物（已被 `.gitignore` 忽略） |

## 綁定方式（零第三方依賴）

| 語言 | 方式 |
| --- | --- |
| C / C++ | 直接 `#include "nolang.h"` 並鏈 `-lnolang` |
| Rust | `#[link(name = "nolang")] unsafe extern "C"` |
| Go | cgo `#cgo CFLAGS/LDFLAGS` |
| Java | JNI（JDK 21；不使用 preview 的 Panama FFM） |
| Python | 標準庫 `ctypes.CDLL(絕對路徑)` |
| C# | `DllImport` + `NativeLibrary.SetDllImportResolver` |

## 常用目標

```bash
make probe    # 只產生 .ll 並印出實際 ABI 簽名
make lib      # 產出 libnolang.dylib / .so 並用 nm -g 檢查導出符號
make jni      # 額外產出 libnolangjni
make verify   # 全部語言實跑 + diff
make clean    # 刪除 dist/
```

函式庫搜尋路徑：macOS 用 `DYLD_LIBRARY_PATH` / `@rpath`，Linux 用
`LD_LIBRARY_PATH` / `$ORIGIN`；Java 另需 `-Djava.library.path`；.NET 在
hardened runtime 下可能忽略 `DYLD_LIBRARY_PATH`，故以 `NOLANG_LIB`
環境變數傳絕對路徑。

## 工具鏈兜底

| 情況 | 處理 |
| --- | --- |
| `go: command not found` | `export PATH="/opt/homebrew/bin:$PATH"`（Makefile 已處理） |
| `g++` 缺失 | 把 `cpp` 目標改成 `clang++ -std=c++17` |
| JDK 缺失 / `JAVA_HOME` 為空 | 安裝 JDK（JNI 需要 `jni.h` 與 `javac -h`）；Makefile 會從 `javac` 路徑推導 |
| `dotnet` restore 被網路阻斷 | `dotnet build --no-restore -c Release` |
| macOS `code signature invalid` | `codesign -s - --force dist/*.dylib`（Makefile 已處理） |
| 下載後被隔離 | `xattr -d com.apple.quarantine dist/*.dylib` |
| Linux `R_X86_64_32S` 重定位 | `-fPIC` 必須加在**編譯 `.ll`** 那一步（`lib` 目標已處理並斷言） |
| LLVM 不再接受 typed pointer IR | 改用 `dist/libnolang_opt.ll`（opaque `ptr`），並同步更新 `abi_guard.py` |

## 已驗證環境

macOS arm64 · clang 22.1.8 · Apple g++ · rustc 1.98.1 · go 1.27.3 ·
OpenJDK 21.0.6 · Python 3.13 · .NET 9.0.106 · `no v0.4.17-6-g35c57169-dirty`。

完整說明見文件頁 [從宿主語言調用 Nolang](../../docs/docs/lang/host-interop.md)。
