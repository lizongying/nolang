# test/ — 標準庫（std）測試套件

這裡是 **Nolang 標準庫的功能測試**：每個 `test/std/<模組>.no` 是一個獨立可執行的
程式，載入 `test/runner.no` 的斷言庫，跑完後依「失敗數」決定退出碼。

> 與 `tests/` 的分工：`tests/**/*.no` 是**編譯器**回歸語料（由 `scripts/mir_golden.sh`
> 對 golden 指紋做 A/B，測 codegen／型別系統／所有權分析）；`test/` 是**標準庫 API**
> 的功能測試（測「這個函數算出來的答案對不對」）。兩者互補，不要合併。

---

## 1. 目錄結構

```
test/
├── runner.no        ; 斷言庫 + 全局計數器（被各測試檔載入）
├── assert.no        ; 另一套斷言庫：每個斷言回傳 ok bool（可選用）
├── package.jsonc    ; 套件設定（output=./dist、ignore=[]）
├── std/             ; 每個 std 模組一個測試檔
│   ├── arr.no
│   ├── vec.no
│   └── …
└── dist/            ; 編譯產物（由 no build/test 產生，可忽略）
```

`test/std/<x>.no` 的檔名對應 `src/std/<x>.no`（子目錄模組用底線，例如
`encoding/base64` → `test/std/base64.no`、`collection/arr-stack` → `test/std/arr-stack.no`）。

每個測試檔開頭都有結構化註釋，寫明**執行指令**與**覆蓋範圍**：

```no
; std-vec.no — vec（切片）模組測試
;
; 執行：no test test/std/vec.no
; 覆蓋：len / push / pop / contains / index-of / first / last / at /
;       insert / remove / reverse / swap / clone / fill / clear / eq /
;       sort-asc / sort-desc
; 注意：[]t.max / []t.min 有預存 codegen bug（1 output params but
;       returned void call），暫不測試。
```

新增測試時請照抄這個格式——「未測／已知 bug」那一段比「覆蓋」那一段更有價值。

---

## 2. 執行方式

所有指令都在 **repo 根目錄**執行（`no` 需要 `workspace.jsonc` 才能解析模組路徑）。

```bash
# 先確保 PATH 有 LLVM（否則連結會失敗）
export PATH="/opt/homebrew/opt/llvm/bin:/Library/Developer/CommandLineTools/usr/bin:$PATH"

# 整個 std 套件
./bin/no test test/std/

# 單一模組
./bin/no test test/std/vec.no

# 多個檔案（逐一列出；no test 一次只吃一個路徑參數）
./bin/no test test/std/str.no
./bin/no test test/std/json.no
```

`no test <file>` 會**編譯後執行**該檔，並把子行程的 stdout/stderr 直接接到終端。

### 為什麼是 `test/std/` 而不是 `test/`

`no test <dir>` 會遞迴收集目錄下所有 `.no`（只排除 `main.no` / `lib.no`），然後
**逐一編譯成可執行檔**。`test/runner.no` 與 `test/assert.no` 是**庫**（沒有 `main`），
被當成測試檔編譯會直接失敗，所以套件根目錄要用 `test/std/`。

`no test` 的兩遍式流程（`src/cmd/no/main.go` 的 `testCommand`）：

1. **Pass 1（並行）**：所有測試檔並行編譯（共用 token/AST 快取）；
2. **Pass 2（循序）**：依序執行編譯產物，避免 stdout 交錯。

### 退出碼

| 退出碼 | 意義 |
|---|---|
| `0` | 全部斷言通過（測試檔末尾 `os.exit(0)`） |
| `1` | 有斷言失敗（測試檔末尾 `f > 0 -> os.exit(1)`），或該檔編譯失敗 |
| 其他 | 執行期崩潰（`trace/BPT trap` = 記憶體／所有權 bug） |

**看 `FAIL:` 行與 `--- Test Summary ---` 的 `failed:` 計數，不要只看退出碼**——
`no test` 逐檔執行，一個檔失敗不會阻止其他檔。

---

## 3. runner 斷言 API

載入方式（路徑相對於 workspace 根目錄）：

```no
# /test/runner
```

`runner.no` 用**模組級全局計數器**（`count-passed` / `count-failed`）累計結果。

| 函數 | 簽名 | 說明 |
|---|---|---|
| `runner.eq` | `(a i64, b i64, msg str)` | `a == b` |
| `runner.ne` | `(a i64, b i64, msg str)` | `a != b` |
| `runner.gt` | `(a i64, b i64, msg str)` | `a > b` |
| `runner.lt` | `(a i64, b i64, msg str)` | `a < b` |
| `runner.is-true` | `(val bool, msg str)` | `val == true` |
| `runner.is-false` | `(val bool, msg str)` | `val == false` |
| `runner.str-eq` | `(a str, b str, n i64, msg str)` | 比較前 `n` 個**位元組** |
| `runner.f64-eq` | `(a f64, b f64, eps f64, msg str)` | `|a-b| < eps` |
| `runner.test` | `(name str, cb callback) (passed i64, failed i64)` | 印 `TEST: <name>` 並執行回呼 |
| `runner.failed-count` | `() (n i64)` | 目前失敗數 |
| `runner.summary` | `() (total i64)` | 印 `--- Test Summary ---` 與 passed/failed/total |

`callback` 是**具名函式型別**：`callback = ()`（無參、無回傳）。所以 `runner.test`
的第二個引數必須是一個已定義的**無參函式**：

```no
t-len = () {
    a = [1, 2, 3, 4, 5]
    runner.eq(a.len(), 5, 'arr.len == 5')
}

p1, f1 = runner.test('arr.len', t-len)
```

### 標準檔尾（每個測試檔都長這樣）

```no
passed = 0
failed = 0

; …每個 test 之後累加（注意 #{overflow=wrap}：整數加法預設會回 option）…
p1, f1 = runner.test('arr.len', t-len)
#{overflow=wrap}
passed = passed + p1
#{overflow=wrap}
failed = failed + f1

; …全部測完…
total = runner.summary()
f = runner.failed-count()
{
    f > 0 -> os.exit(1)

    -> os.exit(0)
}
```

### 三個容易踩的坑

1. **`;` 是行註解，不是陳述分隔符。** `a = 1; b = 2` 只會執行 `a = 1`。
   每個陳述一行。
2. **整數加法要 `#{overflow=wrap}`。** Nolang 的 `a + b` 預設在溢出時回傳
   `option<int>`，未處理是編譯錯誤。累加計數器一定要加註解。
3. **option 的 match 必須寫滿三個臂**（`ok(v)` / `nil` / `err`），否則編譯不過。
   例：`arr.max()` 回 `?i64`：

   ```no
   mx ?i64 = a.max()
   mx: {
       ok(v) -> runner.eq(v, 3, 'arr.max == 3')

       nil -> runner.is-true(false, 'arr.max == nil')

       err -> runner.is-true(false, 'arr.max == err')
   }
   ```

`assert.no` 是另一種風格（每個斷言**回傳** `ok bool`，適合在函式內提早返回），
兩者可以混用，但新測試建議統一用 `runner`（有計數與彙總）。

---

## 4. 新增一個模組測試

1. 看 `src/std/<模組>.no`，列出公開 API（`grep -nE '^[^ ;#].*= \(' src/std/<模組>.no`）。
2. 複製一個既有測試檔當骨架（`test/std/arr.no` 最簡潔）。
3. 每個 API 至少一個正常案例 + 一個邊界案例（空集合、`0`、負數、超長字串）。
4. 跑 `./bin/no test test/std/<模組>.no`，必須 `failed: 0` 且退出碼 `0`。
5. **先讓測試能失敗**：把斷言期望值改錯一次，確認它真的會 `FAIL`——一個永遠
   綠的測試等於沒有測試（尤其當你測的函數其實回空值／回 0 時）。
6. 若某個 API 因**預存編譯器 bug** 測不了，寫在檔頭 `; 未測：…`，不要留一個
   註解掉的測試。

---

## 5. 目前覆蓋狀況

最後一次全量執行（本輪修復後，`./bin/no`）：**41 檔、906 個斷言通過、14 個失敗**
（6 個檔失敗，**全部是預存問題**，見下節）。

> 本輪修復：`number.div`/`number.mod`（#8）、`bigint` bus error（#7）、`txt.to-f32` 回傳 f64 位元（#5）、
> `format` 的 `:t`/`:v`/`,`/`_`（#6）、`[]char` 切片字面量（#4）已全部修復，並釘迴歸測試。
> 斷言數由 836 → 906（`bigint +27`、`number +37`、`char +6`）。

| 檔案 | passed | failed | rc | 檔 | passed | failed | rc |
|---|---:|---:|---:|---|---:|---:|---:|
| `arr-stack.no` | 9 | 0 | 0 | `log.no` | 9 | 0 | 0 |
| `arr.no` | 11 | 0 | 0 | `magic.no` | 11 | 0 | 0 |
| `base64.no` | 12 | **1** | **1** | `math.no` | 27 | **2** | **1** |
| `bigint.no` | 27 | 0 | 0 | `number.no` | 37 | 0 | 0 |
| `bool.no` | 4 | 0 | 0 | `option.no` | 25 | 0 | 0 |
| `bufio.no` | 3 | **3** | **1** | `os.no` | 5 | 0 | 0 |
| `byte.no` | 15 | 0 | 0 | `path.no` | 8 | 0 | 0 |
| `char.no` | 80 | 0 | 0 | `pem.no` | 3 | 0 | 0 |
| `csv.no` | 30 | 0 | 0 | `process.no` | 9 | 0 | 0 |
| `deque.no` | 16 | 0 | 0 | `queue.no` | 9 | 0 | 0 |
| `enum_cross.no` | 2 | 0 | 0 | `regexp.no` | 35 | 0 | 0 |
| `env.no` | 7 | 0 | 0 | `set.no` | 26 | 0 | 0 |
| `err.no` | 5 | 0 | 0 | `sort.no` | 33 | 0 | 0 |
| `fmt.no` | 35 | 0 | 0 | `stack.no` | 13 | 0 | 0 |
| `fs.no` | 10 | 0 | 0 | `str.no` | 41 | 0 | 0 |
| `global.no` | 61 | 0 | 0 | `time.no` | 31 | 0 | 0 |
| `gzip.no` | 11 | 0 | 0 | `txt.no` | 121 | 0 | 0 |
| `hash-set.no` | 14 | 0 | 0 | `uuid.no` | 16 | **1** | **1** |
| `heap.no` | 11 | **4** | **1** | `vec.no` | 46 | 0 | 0 |
| `io.no` | 9 | 0 | 0 | `zlib.no` | 4 | 0 | 0 |
| `json.no` | 25 | **3** | **1** | | | | |

兩個「0 斷言」的檔**已解決**：
- `bigint.no` —— 原整個 bigint 模組在 MIR 後端 **bus error**（連
  `bigint.from-i64(42).to-str()` 都崩）。根因是編譯器在「方法內對同型別區域變數呼叫方法」
  時，被呼叫方法體內的裸 `.` 會解析到**外層方法接收者**；`to-str` 內對 `tmp` 呼叫
  `tmp.is-zero()` 永遠檢查錯誤物件 → 迴圈不會在零值停住（bus error / 前導零）。
  修法：改用顯式欄位存取 `tmp.len == 1 && tmp.limbs[0] == 0`。現已復原守衛，**27 個斷言全過**。
- `char.no` —— 原為佔位（只斷言 `runner.eq(1,1)`），檔頭寫著「char 方法全部有
  特化問題」。該說明**已過時**：`char` 方法現在全部正常，故 2026-09-26 改寫成
  74 個斷言的真正測試，本輪再補 `[]char` 字面量回歸 → **80 個斷言全過**。

### 6 個失敗檔（全部預存，與 `no fmt` 無關）

| 檔 | 失敗斷言 | 說明 |
|---|---|---|
| `base64.no` | 1 | `decode QUJD content == ABC` |
| `bufio.no` | 2 | `read-byte 1st == X (88)`、`read-byte 2nd == Y (89)` |
| `heap.no` | 4 | `pop 2nd/3rd/4th`、`dup pop 2nd`、`fill returns true` |
| `json.no` | 3 | `parsed is-obj`、`parsed is-arr`、`get-i64 value` |
| `math.no` | 2 | `abs(-5.5) == 5.5`、`abs(3.25) == 3.25` |
| `uuid.no` | 1 | `from-str roundtrip eq` |

> **歸因**：用 pre-fmt-std 的 binary 與 post-fmt 的 binary 各跑一次全套件，
> `FAIL` 集合**逐行完全相同**（`diff` 為空）⇒ 這 6 檔不是 fmt 造成的。
> `number.no`（原 `unknown callee number.div`）與 `bigint.no`（bus error）本輪已修復。

**尚未覆蓋**（`src/std` 有、`test/std` 沒有）：

| 分類 | 模組 |
|---|---|
| 頂層 | `args` `async` `embed` `enter` `leave` `markdown` `toml` `types` `yaml` |
| `archive/` | `bzip2` `tar` `xz` `zip` `zstd` |
| `collection/` | `link` `linked-hash-map` `map` `str-map` `str-set` `tree-map` `tree-set` |
| `crypto/` | `aes` `aes-cbc` `aes-ctr` `aes-gcm` `argon2` `base32` `blake2` `chacha20-poly1305` `crc-16` `crc-32` `crc-64` `des` `ecdsa` `ed25519` `fnv` `fnv-1a-32` `hkdf` `hmac` `md5` `pbkdf2` `rand` `rc4` `rsa` `scrypt` `sha1` `sha224` `sha256` `sha3` `sha384` `sha512` `tdes` `x25519` `x509` |
| `database/` | `sql` |
| `net/` | `client` `cookie` `dns` `hpack` `http` `http2` `http3` `ip` `multipart` `net` `pool` `proxy` `quic` `server` `sse` `tls` `tls-server-keys` `unix` `url` `ws` |

> `types.no` 是**純註解檔**（只有型別對映表），沒有可測的函式，不需要補測試。

補測試時優先順序建議：**純函數、無檔案系統／無網路依賴**的模組
（`yaml`、`toml`、`crypto/*`（`sha256`/`sha1`/`sha512`/`blake2`/`hmac`/`rc4`/
`crc-32`/`crc-16`/`base32` 已實測可用）、`collection/*`）→ `archive/*`、
`markdown` → 最後才是 `net/*`（需要 socket，CI 上不穩定）。`args` 需要 argv，
`async`/`embed` 需要執行期支援，都不適合放進這個套件。

### 已知 bug 清單（寫測試時挖出來的，全部預存於 HEAD）

每一條都能用 `/tmp/no_std_before`（pre-fmt、HEAD std 的 binary）重現，
**與 2026-09-26 的 `no fmt -w src/std` 無關**。詳細重現檔名寫在各測試檔的檔頭。

### ✅ 已修（2026-09-26，修完 `no vet` 仍 0 error、`no fmt` 仍 fixed point）

| # | 症狀 | 修法 | 迴歸釘 |
|---|---|---|---|
| A | **`txt.slice` 對空區間回傳未初始化的 txt**（`r-start >= r-end -> return` 沒設 `out.len`）⇒ **非決定性**，污染 `split`（前導/尾隨/相鄰分隔符）、`split-n`、`replace-*` | 改成 `r-start >= r-end -> { out.len = 0; return }` | `test/std/txt.no` 的 `split leading/trailing empty`、`slice(0,0).len() == 0` |
| B | **無號數解析家族恆回 0**（`txt.to-u8/u16/u32/u64/to-byte`）。手寫迴圈把累積器 `n=0` **當迴圈上界**用 ⇒ `[start..n)` == 空迴圈 | 另立 `n-len i64 = .len-bytes()` 當上界 | `test/std/txt.no` 的 `to-u8 == 255`、越界/負號 == `err` |
| C | `txt.to-bytes()` 編不過：`out.set-byte(i, …)` 但 out 是 `[]byte`，`set-byte` 內建只認 str/txt receiver | 改用局部 `b []byte` 逐元素寫入 `b[i] = .byte(i)` 後 `out = b` | `test/std/txt.no` 的 `to-bytes len/[0]/[11]` |

> 這三條的修復**同時也驗證了測試是真的**：用修復前的 binary（`/tmp/no_std_after`）
> 跑新的 `txt.no` 會**連編都編不過**（`to-bytes` → `unsupported builtin []t.set-byte`），
> 跑出來唯一的 diff 就是 `FAIL: test/std/txt.no` 消失。

### ✅ 已修（本輪 —— `txt.to-f32` / `format` / `number.div` / `bigint` / `[]char`）

| # | 症狀 | 修法 | 迴歸釘 |
|---|---|---|---|
| D | **`[]char` 切片字面量**編不過（global 宣告 `[N x i64]`、元素卻是 `i32` → opt-verify / `unsupported builtin []t.set-byte`） | 編譯器已修（切片字面量降級為正確元素型別） | `test/std/char.no` 的 `t-slice-char-literal`（len / 索引 / 元素賦值 / `[N]char` 固定陣列） |
| E | **`txt.to-f32` 回傳 f64 的位元**（`42.0` 印成 `4631107791820423168`）。`?f32` 裝箱被 `optionPayloadLLVMType` 當成 `i64` 存/讀 | `src/mir/codegen.go` 的 `optionPayloadLLVMType` 把 `f32`/`float` 納入 `double` 分支；`src/mir/hir2mir.go` 兩處 format-field dispatch 補 `f32` | `src/mir/f32_option_payload_test.go`（`TestOptionF32PayloadUsesDoubleType` / `TestOptionF32PeelEmitsDoubleLoad` / `TestFormatFieldF32Lowers`）；`txt.to-f32()` 現印 `3.5` |
| F | **`format` 的 `:t`/`:v`/`,`/`_` 沒實作** | `src/std/fmt.no`：`fmt-int`/`fmt-uint`/`fmt-f64`/`fmt-str`/`fmt-bool` 加 `:t`（型別名）；`fmt-str` 加 `:v`（單引號包裹）；新增 `fmt-group-digits` 並在 `:` 規格的 grouping 分支套用（**必須在零填充之前**） | `print('{n:t}')`→`i64`、`'{s:v}'`→`'abc'`、`'{g:,}'`→`1,234,567`、`'{h:08,d}'`→`0001,234` |
| G | **`bigint` 全模組 bus error**（`from-i64(42).to-str()` 都崩）。根因：編譯器在「方法內對同型別區域變數呼叫方法」時，被呼叫方法體內裸 `.` 解析到**外層方法接收者** ⇒ `to-str` 內 `tmp.is-zero()` 永遠檢查錯誤物件 → 迴圈不會在零值停住 | `src/std/bigint.no` 的 `to-str` 把 `tmp.is-zero()` 改為顯式欄位存取 `tmp.len == 1 && tmp.limbs[0] == 0`（並加 `while` 計數保護替代 `!!` 無限迴圈） | `test/std/bigint.no` 解封 `runner.test` 註冊（**27 斷言全過**：`from-i64`/`zero`/`cmp`/`eq`/`is-zero`/`is-neg`/`add`/`sub`/`mul`/`div-mod`/`mod-i64`/`pow`/`gcd`/`lcm`） |
| H | **`number.div`/`number.mod` → `unknown callee number.div`**（`number.no` 編不過） | `src/std/number.no` 補 `div`/`mod` 實作（`#{overflow=wrap}` 的 `a/b`、`a%b`，向零截斷，與 C/Go/Java 一致） | `test/std/number.no` 現 **37 斷言全過** |

> ⚠️ `:t` 的已知小瑕疵：`{b:t}`（bool）與 `{u:t}`（u64）仍會印 `i64`——因為 bool/u64 被 dispatch 到
> `fmt-int`（回傳 `'i64'`），非本輪修復範圍；其餘 `int`/`f64`/`str`/`bool` 的 `:t`/`:v` 均正確。

### ❌ 未修（待處理）

| # | 症狀 | 嚴重度 | 位置 |
|---|---|---|---|
| 1 | **模組頂層 option 初始化後 match，一個臂都不執行**（靜默，rc 仍 0）。`v ?i64 = 7` 後 `v: {…}` 直接跳過；結果**取決於變數名**（`a`/`w`/`x`/`y`/`res` 正常，其餘絕大多數靜默；大寫＝真全局一律靜默）。安全寫法：在函式內宣告、或 `v ?i64 = mk()` 由函式回傳 | 高（靜默錯答） | compiler（option 初始化） |
| 2 | **`?bool` 的 match 取值恆為 false**。`print(f())` 印 `true`（包裝對），`ok(b) -> print(b)` 印 `0`。`?i64`/`?str`/`?f64`/`?struct`/`?slice` 全正常，只有 bool 壞。連帶 `txt.to-bool` 全滅 | 高 | compiler（option 載荷） |
| 3 | **option 當函式參數 + match → 編譯失敗**：`opt-verify: '%option' … but expected '%str-long'`（`str_clone` 被套到 option 上，option 載荷槽 24 bytes 剛好＝`%str-long`）。只傳不 match 沒事 | 中（硬錯誤） | compiler（參數 marshal） |

> 原本的 #4–#8（[]char 字面量、`txt.to-f32` 位元、`format :t/:v/千分位`、`bigint` bus error、
> `number.div`）**本輪已全部修復**（見下方「✅ 已修」D–H）。

### 寫測試時的三個型別陷阱（會讓你寫出「永遠綠」的錯測試）

1. **`str.len()` / `txt.len()` 回的是碼點數，不是位元組數。**
   `'中'.len() == 1`，`'中'.to-bytes().len() == 3`。要位元組數得走
   `.to-bytes().len()` 或 `txt.len-bytes()`。
2. **`'A'` 是 str，`"A"` 才是 char。** `char` 方法（`to-upper` 等）吃的是 char，
   傳 str 字面量會報型別錯；char 的 `print` 出來是**碼點數字**（65），
   要比對字串得先 `.to-str()`。
3. **match 的主體只能是裸名。** `b.q: { … }` 會被 parser 拒絕
   （`expected left parenthesis, got LBRACE`），要先落到區域變數。

---

## 6. 其他相關的驗證入口

```bash
# std 的靜態檢查（必須 0 error；warning/hint 會漂移，不當門檻）
./bin/no vet src/std

# 編譯器單元測試
cd src && go test ./mir/ ./fmt/ ./parser/ ./lexer/ ./hir/ ./checker/

# 編譯器回歸語料（tests/ 全量 A/B，約 7 分鐘）
GOLDEN=tests/golden/mir-baseline.tsv GOLDEN_MIR=default bash scripts/mir_golden.sh

# 格式化（改過 src/std 後要確認仍是 fixed point）
./bin/no fmt -w src/std
```

### `no fmt` 的 overflow 註解剪枝（2026-09-26 實例）

`no fmt` 會刪除它判定「無效」的 `#{overflow=…}` 註解，所以**改完 std 一定要重跑
`./bin/no vet src/std`**：若某條被刪的註解其實是有效的，vet 會立刻由 0 error 變成
有 error。

這不是假設——2026-09-26 的 `no fmt -w src/std`（116 檔）真的踩到了：

- 症狀：vet 由 `0 error` 變成 **2 error**，都在 `src/std/net/hpack.no:444/447`
  （`prev-name-len = .dyn-names[i - 1].len-bytes()`）。
- 根因：`checker.StatementsWithIntOverflow` 的 `CallExpression` 分支只走
  `Function` 與 `Arguments`，但方法呼叫在原始 AST 裡是
  `CallExpression{Function: DotExpression{Receiver: recv}}` ⇒ **receiver 整個沒被
  遞迴**。而 `ValidateIntOverflow` 跑在 lowering 之後（receiver 已被移進
  `Arguments`），兩邊不對稱 ⇒「需要註解的陳述集合」被低估 ⇒ 有效註解被當垃圾刪掉。
- 修法：`src/checker/checker.go` 補一個 `case *parser.DotExpression:` 遞迴
  `x.Receiver`（relevant 集合必須是**超集**，所以一律往「保留註解」的方向保守補）。
  迴歸：`src/fmt/overflow_ineffective_test.go` 的
  `TestFormatPreservesEffectiveOverflowInMethodReceiverIndex`。

**relevant 集合必須是超集**這條是硬規則：低估會靜默刪掉活註解（vet 立刻炸），
高估只是留了幾條多餘註解（無害）。

### 三次 golden sweep 對照（`tests/` 全量，`scripts/mir_golden.sh`）

| 時間點 | binary | SAME | DIVERGE | REGRESS | IMPROVED | BOTH_FAIL |
|---|---|---:|---:|---:|---:|---:|
| fmt 之前（HEAD std） | `/tmp/no_std_before` | 485 | 3 | 0 | 0 | 0 |
| fmt ＋ checker 修正後 | `/tmp/no_std_after` | 484 | 4 | 0 | 0 | 0 |
| 再修好 std 的 3 個 txt bug 後 | `./bin/no` | 485 | 3 | 0 | 0 | 0 |

三次都是 `REGRESS=0 IMPROVED=0 BOTH_FAIL=0` ⇒ 沒有回歸。SAME 在 484/485 之間跳動、
多出來的那一筆 DIVERGE 是 **`tests/markdown.no`** —— 它**本身非決定性**
（同一支 binary 跑 6 次出現 2 種 hash），所以會在 SAME 與 DIVERGE 之間抖動，
屬已知噪聲，不要拿它當訊號。另外三次都有的 3 筆 DIVERGE
（`net-client` / `slice-heavy` / `test-div-mod-option`）是既有偏差。

### 改完 std 後的驗收順序

```bash
export PATH="/opt/homebrew/opt/llvm/bin:/Library/Developer/CommandLineTools/usr/bin:$PATH"

# 1. std 是 go:embed 的，改了 std 原始碼要讓 embed 重新計算
touch src/std_embed.go && make no

# 2. 靜態檢查必須 0 error
./bin/no vet src/std

# 3. 格式化必須是 fixed point（先把所有 .no 的 hash 記下來，fmt 後再比一次）
find src/std -name '*.no' | sort | xargs shasum -a 256 > /tmp/fp1.txt
./bin/no fmt -w src/std
find src/std -name '*.no' | sort | xargs shasum -a 256 > /tmp/fp2.txt
diff /tmp/fp1.txt /tmp/fp2.txt      # 必須是空的

# 4. 標準庫功能測試
./bin/no test test/std/

# 5. 編譯器單元測試
cd src && go test ./mir/ ./fmt/ ./parser/ ./lexer/ ./hir/ ./checker/

# 6. 編譯器回歸語料（tests/ 全量 A/B）
GOLDEN=tests/golden/mir-baseline.tsv GOLDEN_MIR=default bash scripts/mir_golden.sh
```

> ⚠️ **A/B 一定要真的隔離因果**：共用工作樹的 `bin/no` 會**把別人未提交的工作烘進去**，
> 不能拿它當對照組。要指名 SHA、用 `git archive <SHA>` 建乾淨快照再自建 binary
> （`/tmp/no_std_before` 就是這樣來的）。另外 `ps`/`pgrep` 的訊號受限，「看不到行程」
> **不等於跑完了**，判斷完成要看輸出檔。
