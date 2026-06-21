# Nolang

Nolang 是一種無 GC、內存安全、語法極簡的系統級編程語言。

## 核心特性

- **無 GC**：不依賴垃圾回收，自动安全內存管理
- **內存安全**：作用域離開自動釋放，杜絕懸垂引用、內存泄漏
- **語法極簡**：減少關鍵字，無冗余語法
- **行為確定性**：所有操作顯式，行為可預測
- **類型推斷**: 變量無需过度聲明類型
- **函數無返回值**：所有結果透過參數傳遞
- **統一引用傳遞**：所有參數預設為引用
- **模块系统** - 每个文件即一个独立模块
- **命名空间** - 文件夹自动對應命名空间

### 函數作用域變量覆蓋

在函數作用域內，如果重複使用相同的變量名進行賦值，Nolang 將其視為覆蓋重賦值，而非創建新的棧變量。不触发变量遮蔽
如果类型不同，语法不允许

### 統一引用傳遞模型

Nolang 採用統一引用傳遞模型，所有函數參數預設均為引用型別。這意味著：

- 函數內修改 = 外部變量直接改變
- 函數內對參數的任何修改，都會直接作用於調用方的原始數據
- 可修改，但不可销毁

### 內存安全機制

- **變數自動銷毀** 函數結束自動銷毀所有內部變數
- **禁止手動釋放** 避免誤刪導致的懸垂引用
- **值複製容器** 數組 / 切片存副本，與原變數分離， 原變量生命周期結束並銷毀時，容器內的數據不受任何影響
- **無 GC、無分配隱藏成本**

[docs](https://lizongying.github.io/nolang/)

## Usage

推薦使用vscode

[vscode](https://marketplace.visualstudio.com/items?itemName=lizongying.vscode-nolang)

安裝nolang

[nolang](https://github.com/lizongying/nolang/releases/latest)

## CLI 命令

| 命令                                        | 說明                    |
| ------------------------------------------- | ----------------------- |
| `no init`                                   | 初始化專案              |
| `no new <name>`                             | 建立新專案              |
| `no fmt`                                    | 格式化程式碼            |
| `no build`                                  | 構建（輸出 executable） |
| `no run`                                    | 構建並執行 main.no      |
| `no test`                                   | 執行測試                |
| `no add`                                    | 依賴管理                |
| `no remove`                                 | 依賴管理                |
| `no update`                                 | 依賴管理                |
| `no update-all`                             | 依賴管理                |
| `no sync`                                   | 依賴管理                |
| `no list`                                   | 依賴管理                |
| `no install`                                | 安裝 binary 到系統      |
| `no pub --token <token> [--registry <url>]` | 發布套件至 registry     |
| `no sync`                                   | 同步依賴                |

```bash
# 構建（默認尋找 main.no）
no build                    # 構建當前目錄
no build main.no            # 構建指定文件
no build -o output main.no  # 指定輸出路徑
no build -cc zig main.no    # 使用 Zig 編譯器

# 運行（構建 + 執行）
no run                    # 構建並執行 main.no（必須有 main.no）
no run main.no
no run -cc zig main.no

# 測試（構建 + 執行）
no test                   # 執行目錄下所有 .no 文件的 main()，排除 main.no / lib.no
no test my-test.no        # 執行單個測試文件
no test -cc zig my-test.no
```

### 創建新項目

```shell
no new test1

cd test1

no run
#  or
no run .
```

### 入口規則

- **main.no** — 程序入口，不可包含測試斷言
- **lib.no** — 庫入口，不可包含測試斷言
- **其他 .no 文件** — 可包含測試斷言，測試與方法在同一文件

```bash
no test .       # 執行其他 .no 文件的 main()
```

## 测试说明

- `no test` 会遍历目录下所有 .no 文件（跳过 main.no / lib.no）
- 每个测试文件独立构建并运行自己的 main() 函数
- 测试文件和功能代码写在同一个 .no 文件中
- 若任一测试失败，返回非零退出码

---

### 程序結構

- main.no — 入口，執行 main()
- lib.no — 庫入口，導出函數
- 其他 .no 文件 — 可作為測試文件，包含自己的 main()

## cli使用

```bash
# 编译 Nolang 代码
cd src/build && go run . your-file.no

# 格式化代码
cd src/fmt && go run . your-file.no
```

## 標準庫測試

標準庫的實作正確性通過與 **Go 標準庫**的直接比對來驗證。測試架構位於 `tests/` 目錄：

```shell
tests/
  test_std_hash.no       ← Nolang 測試（輸出 KEY=VALUE 格式）
  run-test.sh            ← 自動化測試腳本
  compare/
    compare.go           ← Go 對照程式（同測試向量，同輸出格式）
    go-output.txt        ← Go 執行結果快取（透過 -u 更新）
  nolang-output.txt      ← Nolang 執行結果
  diff-result.txt        ← diff 結果
```

### 執行測試

```bash
# 完整測試：執行 Nolang + Go 對照 + diff 比對
./tests/run-test.sh

# 詳細模式（顯示完整 diff）
./tests/run-test.sh -v

# 更新 Go 對照輸出（首次執行或修改 compare.go 後）
./tests/run-test.sh -u
```

### 測試涵蓋

| 模組      | 測試項目                | 驗證來源          |
| --------- | ----------------------- | ----------------- |
| `crc32`   | 空字串、hello、fox 字串 | `hash/crc32` IEEE |
| `fnv-1a`  | 同上                    | `hash/fnv` New32a |
| `sha256`  | zero block 壓縮         | 內建壓縮函數比對  |
| `sha512`  | zero block 壓縮         | 內建壓縮函數比對  |
| `aes-128` | NIST ECB KAT 加解密     | `crypto/aes`      |
| `des`     | 標準測試向量加解密      | `crypto/des`      |

### 新增測試案例

1. 在 `tests/test_std_hash.no` 中加入測試，輸出格式為：
   ```
   test-name=hexvalue
   ```
2. 在 `tests/compare/compare.go` 中加入對應的 Go 測試，輸出**完全相同的 key**
3. 執行 `./tests/run-test.sh -u` 更新 Go 對照
4. 執行 `./tests/run-test.sh` 確認 diff 通過

### 比對原理

```shell
# 各自執行，產生 KEY=VALUE 格式輸出
go run tests/compare/compare.go    → go-output.txt
nolang run tests/test_std_hash.no  → nolang-output.txt

# 逐行比對
diff go-output.txt nolang-output.txt

# 無輸出 = 完全一致 = 通過
```

## TODO

- [ ] 重载函數
- [ ] 實現錯誤處理
- [ ] 常量使用大寫字母和中連結線，不允許大小寫混合
