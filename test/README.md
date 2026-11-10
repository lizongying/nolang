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

### 5.0a 快照：2026-11-10 上午（地圖輪，保留供對照；數字已過期）

盤點工具：`python3 scripts/std_test_coverage.py`（`--missing` 列缺口、
`--module <名>` 看單一模組、`--markdown` 出表、`--strict` 有缺口回 rc=1）。

口徑：API 面取自 `src/std/**/*.no` 的頂層宣告（`name = (` 為模組函數、
`type.method = (` 為方法）；「已覆蓋」= 測試檔裡真的出現該呼叫形態
（模組函數看 `<alias>.<name>`，方法看**本模組測試檔**裡的 `.<name>(`）。
方法覆蓋是**近似**——同模組內多個結構體共用同名方法時會高估。

| 項目 | 數值 |
|---|---:|
| std 源檔 / 模組別名 | 119 / 119 |
| API 面（函數 + 方法） | 2139（810 + 1329） |
| 已覆蓋 | **739（34.5%）**（本輪前 675 / 31.6%） |
| 有專用測試檔的模組 | 99 / 119（本輪前 92 / 119） |
| 測試檔數 | 103（本輪 +7） |

本輪新增 7 個測試檔（164 個斷言）＋ 擴充 `aes.no`（8 → 52 個斷言），全綠：

| 檔 | 斷言 | 覆蓋 |
|---|---:|---|
| `float.no` | 14 | `float.div` / `is-nan` / `is-inf` |
| `int.no` | 12 | `int.to-str`（i128/u128）/ `int.div` / `int.mod` |
| `num.no` | 19 | `num.clamp` / `sign` / `abs` |
| `fnv.no` | 8 | `fnv-1a-32` / `fnv-1a-64` |
| `fnv-1a-32.no` | 5 | `fnv-1a-32(s, n)` |
| `sha3.no` | 30 | `rotl64` / `keccak-f1600` / `keccak-sponge` / `sha3-{224,256,384,512}` 及 4 個 `-hex` |
| `map.no` | 76 | `hashmap-{str,int,bool}-tmpl` 的 init/hash/str-eq/put/get/contains/remove/rehash/clear/len/is-empty + 自動擴容 |
| `aes.no`（擴充） | 52 | `crypto/aes` **23/23 全覆蓋**：gf-mul2/3、rcon-val、sub/inv-sub-bytes、shift/inv-shift-rows、mix-col(s)/inv-mix-col(s)、add-round-key(-at)、rot-word、sub-word、key-expand(-256)、enc/dec-block、enc/dec-128/256（FIPS-197 KAT） |

**仍無專用測試檔的 20 個模組**：`archive/{tar,xz,zip,zstd}`、`async`、
`collection/static-hashmap`、`embed`、`net/{client,http,http2,http3,net,pool,proxy,quic,server,sse,tls,unix,ws}`。

其中 `collection/static-hashmap`（31 個方法）**目前測不了**：模組註釋寫的
`m = static-hashmap-str-tmpl {}` 實際編不過（`EmitLLVM: unknown callee
static-hashmap-str-tmpl.init`，泛型參數 v 無從特化），且全 repo 沒有任何
`.no` 檔真正使用這三個模板 —— 屬**死代碼 + 缺實例化語法**，不是漏測。

全量執行（`./bin/no test test/std/`，103 檔）：**100 檔全綠、3 檔失敗**，
失敗檔全部與本輪改動無關（見 §5.2）。

### 5.0b 快照：2026-11-10 下午（archive 輪，數字已被 5.0c 取代）

| 項目 | 數值 |
|---|---:|
| std 源檔 / 模組別名 | 119 / 119 |
| API 面（函數 + 方法） | 2139（810 + 1329） |
| 已覆蓋 | **844（39.5%）**（archive 輪前 739 / 34.5%） |
| 有專用測試檔的模組 | 103 / 119 |
| 測試檔數 | 108 |
| `archive/*` | **115 / 115 = 100%** |

`archive/*` 逐模組（API 面 / 已覆蓋）：

| 模組 | 覆蓋 | 測試檔斷言 |
|---|---|---:|
| `archive/bzip2` | 7 / 7 | 15（原 2，擴充） |
| `archive/gzip` | 12 / 12 | 38（原 28，擴充） |
| `archive/tar` | 27 / 27 | 66（新） |
| `archive/xz` | 19 / 19 | 36（新） |
| `archive/zip` | 30 / 30 | 55（新） |
| `archive/zlib` | 5 / 5 | 35（原 4，擴充） |
| `archive/zstd` | 15 / 15 | 36（新） |

本輪測試斷言合計 **281 條**。全量執行（`./bin/no test test/std/`，108 檔）：
**107 檔綠 / 1 檔紅（`multipart.no` segmentation fault，預存問題，與本輪無關）**，
**2613 條斷言通過、0 條失敗**。`./bin/no vet src/std/archive/` = **0 error / 90 hint**。

> `make test`（Go 單測）在 2026-11-10 這版工作區**是紅的**：14 個失敗全在
> `parser` / `parser/dump` / `mir`（`tests/match-range-pattern.no` 等 fixture 檔不存在、
> `TestAnnotationsLiveOnAST` / `TestBuildSemViewsMatchesFreshParse` …），是另一 session
> 正在改 `src/parser/{annotation,ast,lowering}.go` 的副產物。本輪只動
> `src/std/archive/*.no`、`test/`、`scripts/`，與那批失敗無關；改動落地後需複跑。

⚠️ 本輪同時收緊了盤點工具的口徑：`strip_comment()` 現在**連字符串字面量的內容一起抹掉**。
之前 `runner.test('zip.u16le/u32le', …)` 這種**斷言說明**會被當成真的調用，
把 `zip.u16le` 虛報成已覆蓋（`zip.u32le` 卻沒有，才露了餡）。
所以舊快照裡的百分比含少量虛報，本輪起的數字才是可信的。

### 5.0c 快照：2026-11-10 傍晚（crypto/x25519 輪；數字已被 5.0d 取代）

| 項目 | 數值 |
|---|---:|
| std 源檔 / 模組別名 | 119 / 119 |
| API 面（函數 + 方法） | 2142（813 + 1329） |
| 已覆蓋 | **872（40.7%）**（archive 輪後 844 / 39.5%） |
| 有專用測試檔的模組 | 103 / 119 |
| 測試檔數 | 108 |
| `crypto/x25519` | **30 / 30 = 100%**（本輪 5 → 30） |

本輪測試斷言合計 **87 條**（`test/std/x25519.no`，原 6 條）。全量
`./bin/no test test/std/`（108 檔）：**107 檔綠 / 1 檔紅**（`multipart.no`
segmentation fault，預存問題），**2694 條斷言通過、0 條失敗**（archive 輪為 2613，
差額 81 正好是本輪 x25519 的增量）。`./bin/no vet src/std/crypto/` = **0 error**；
`no fmt -d src/std/crypto/x25519.no` = 空 diff（fixed point）。

`crypto` 家族現況（`python3 scripts/std_test_coverage.py --module crypto`）：

| 模組 | 覆蓋 | 備註 |
|---|---:|---|
| `crypto/x25519` | 30 / 30 | 本輪補齊，含 3 個新增純量輔助 |
| `crypto/ed25519` | 全綠 | 待盤點缺口 |
| `crypto/ecdsa` | 全綠 | 待盤點缺口 |
| `crypto/rsa` | 全綠 | 待盤點缺口 |
| `crypto/x509` | 全綠 | 待盤點缺口 |
| `crypto/aes-gcm` / `blake2` / `chacha20-poly1305` | 仍有缺口 | 下一批 |

⚠️ 本輪新增了 3 個**公開**輔助函式（`x25519.num-add` / `num-sub` / `num-floor`），
所以 API 面分母從 2139 長到 2142。這是為了繞開「`i64 op i64` 回傳 `?i64`」的型別規則
（詳見 §5.1c），不是純粹的測試基建。

### 5.0d 快照：2026-11-10 深夜（crypto 家族收口輪；數字已被 5.0e 取代）

| 項目 | 數值 |
|---|---:|
| std 源檔 / 模組別名 | 119 / 119 |
| API 面（函數 + 方法） | 2117（797 + 1320） |
| 已覆蓋 | **990（46.8%）**（x25519 輪後 872 / 40.7%） |
| 有專用測試檔的模組 | 103 / 119 |
| 測試檔數 | 108 |
| **`crypto` 家族 33 個模組** | **全部 100%** |

分母從 2142 降到 2117：私有符號（`_` 前綴，使用者碼不可呼叫，checker 報
`raqvyltn`）改為不計入分母，共 25 個，詳見 §5.2。

本輪補到 100% 的 10 個模組（缺口 → 全綠）：

| 模組 | 缺口 → 覆蓋 | 測試檔斷言 | 向量來源 |
|---|---|---:|---|
| `crypto/hkdf` | 3 → 6 / 6 | 24 | RFC 5869 + RFC 8446 §7.1 HkdfLabel |
| `crypto/aes-gcm` | 3 → 11 / 11 | 50 | NIST SP 800-38D TC1–6 / 13–16 |
| `crypto/blake2` | 8 → 14 / 14 | 49 | RFC 7693 + 自算 G 函數 |
| `crypto/chacha20-poly1305` | 2 → 9 / 9 | 42 | RFC 8439 §2.3.2 / 2.5.2 / 2.6.2 |
| `crypto/aes-cbc` | 2 → 8 / 8 | 33 | SP 800-38A F.2.1 / F.2.5 |
| `crypto/aes-ctr` | 2 → 4 / 4 | 22 | SP 800-38A F.5.1 / F.5.5 |
| `crypto/des` | 2 → 7 / 7 | 24 | FIPS 46-3 子金鑰排程 |
| `crypto/tdes` | 2 → 4 / 4 | 21 | 3DES-CBC 自算 + 退化驗證 |
| `crypto/scrypt` | 1 → 5 / 5 | 26 | RFC 7914 A.1 / A.2 + 純 Python 核心 |
| `crypto/argon2` | 1 → 5 / 5 | 30 | RFC 9106 §3.5（另見下方缺口） |

環境沒有 `pycryptodome` / `cryptography`，所以全部金標都是**純 Python 自實作 →
先通過官方/標準向量自檢 → 再取金標**。這一步是必要的：`scrypt` 的
`salsa20-8-core` 金標就靠「Python 實作能重現 RFC 7914 A.1/A.2」才取得信任。

### 5.0l 快照：2026-11-10 夜（`time` 輪，**當前**）

| 項目 | 數值 |
|---|---:|
| std 源檔 / 模組別名 | 119 / 119 |
| API 面（函數 + 方法） | 2091（773 + 1318） |
| 已覆蓋 | **1422（68.0%）**（`regexp` 輪結束時 1405 / 67.2%） |
| 有專用測試檔的模組 | 103 / 119 |
| 測試檔數 / 全量 `TEST` 區塊 | 108 / **940** |
| 全量斷言 | **4574**（4525 passed + 49 failed；49 條紅全屬他人未提交改動與預存 `multipart`，見 5.0k） |

| 模組 | 缺口 → 覆蓋 | 測試檔斷言 | 內容 |
|---|---|---|---|
| `time` | 17 → **39 / 39（100%）**（函數 28、方法 11） | 41 → **95** | 三個毫秒/微秒/納秒時鐘（`now-ms`/`now-us`/`now-ns`）、四個粒度睡眠（`sleep-s`/`sleep-ms`/`sleep-us`/`sleep-ns`，以時鐘差為證而非只看不崩）、`duration-ms-between`/`duration-us-between`、`since-s`/`since-ms`/`since-us`/`since-time`、`timer.start`/`stop`/`elapsed-us`、`now-date` |

7 個新 `TEST`（p14–p20）。分母不變。

**時間型斷言的做法**：一律「取時鐘 → 動作 → 再取時鐘 → 斷言差值落在寬鬆區間」，
不假設排程精度（`sleep-ms(30)` 只斷 `>= 20ms`；`sleep-s(0)` 斷 `< 1s` 證明它不會
把 0 當無限等待）。`elapsed-us` 則用 `elapsed-ms × 1000` 精確對齊（同一個
`end-ts - start-ts` 派生，不涉真實時間）。

**算術助手**：Nolang 的算術結果一律是 `?i64`（§5.2 #61），把 `?i64` 當運算元再用
會**靜默取 0**。所以檔頭先定義 `sub-of` / `mul-of` / `div-of` / `abs-of` 四個
`(a i64, b i64) (out i64)` 助手就地解包，測試主體只碰裸 `i64`。

**門檻**：`no vet` 兩檔 0 error / 0 warning；`no fmt -d` 兩檔皆空。

本輪缺陷修法見 **5.1o**，新挖到的編譯器缺口見 **§5.2 #61**。


#### 上一輪（5.0k · `regexp` 輪，數字已被 5.0l 取代）

### 5.0k 快照：2026-11-10 夜（`regexp` 輪）

| 項目 | 數值 |
|---|---:|
| std 源檔 / 模組別名 | 119 / 119 |
| API 面（函數 + 方法） | 2091（773 + 1318） |
| 已覆蓋 | **1405（67.2%）**（`json` 輪結束時 1379 / 65.9%） |
| 有專用測試檔的模組 | 103 / 119 |
| 測試檔數 / 全量 `TEST` 區塊 | 108 / **933** |
| 全量斷言 | **4520**（其中 49 條紅屬他人未提交改動，見下） |

| 模組 | 缺口 → 覆蓋 | 測試檔斷言 | 內容 |
|---|---|---|---|
| `regexp` | 26 → **26 / 26（100%）**（函數 3、方法 23） | 35 → **240** | 頂層 3 個（`regexp-match` / `regexp-find` / `regexp-compile`，**點式**呼叫）+ 公開方法 3 個（`matches` / `find` / `compile`）+ 編譯流水線 20 個（`emit` / `emit-atom` / `emit-quantized` / `alloc-class` / `init-class-zero` / `init-class-all` / `set-class-bit` / `clear-class-bit` / `in-class` / `set-class-escape` / `make-class-{digits,nondigits,word,nonword,space,nonspace}` / `parse-atom` / `parse-class` / `parse-escape` / `match-at`） |

15 個新 `TEST`（p15–p29）。分母不變（`RE-CLS-MAX` / `RE-OP-MAX` 是常量，不進分母）。

**覆蓋判定要點**：`scripts/std_test_coverage.py` 對**頂層函數**用的是
`<alias>.<name>`（點式）而非裸名，所以裸寫 `regexp-match(...)` 不算命中
`regexp.regexp-match`。三種寫法在 checker 裡的表現實測不同：

- `regexp-match(...)` / `regexp-find(...)` —— 裸名可用（歷史上就是這樣被測的）；
- `regexp-compile(...)` —— 裸名報
  `'regexp-compile' is not defined; call it as 'regexp.regexp-compile' [4bek3xc6]`；
- 點式 `regexp.regexp-match(...)` / `regexp.regexp-find(...)` /
  `regexp.regexp-compile(...)` —— 三者皆可。

測試統一走點式，既點亮覆蓋率也順帶釘住這條跨模組限定名規則。

**門檻**：`no vet` 兩檔 0 error / 0 warning；`no fmt -d` 兩檔皆空（`no fmt` 會把
`regexp{ pattern: '' }` 展開成多行，已落盤）。

**⚠️ 全量回歸的紅檔歸因（非本輪引入）**：`./bin/no test test/std/` 得 100 綠 / 8 紅
（`base64` / `char` / `fmt` / `global` / `multipart` / `pem` / `str` / `url`）。
`multipart` 是預存段錯誤；另 7 檔由**工作區裡另一 session 未提交的 `src/std` 改動**造成，
證據三條：

1. `git status src/std/` 有數十個 `.no` 被改（本輪只碰 `regexp.no`）。
2. 那批改動把 `out[k] = v` 系統性換成 `out[k] ?= v` 包在 `{ … }: { err -> _ }` 裡。
   `out` 是 `with-cap` 出來的緩衝（len 為 0）時，`?=` 的越界 store 走 err 臂 ⇒
   **靜默丟棄，緩衝長度留在 0**。實測 `'abc'.to-hex()` 回 `len = 0`（應為 `616263`），
   而同檔手寫的 `o[i] = 97` with-cap 形態 `len = 3` 正常。
3. `char.no` / `global.no` 自身**無 diff**，是被改壞的 `str.to-hex` / `fmt` 拖紅的。

`make gen no` 會把工作區 `src/std` 一起烤進 blob，所以這批在途改動在重建時被激活。
按「不碰他人未提交改動」的規矩，本輪沒有改它們，只在此記錄。

本輪缺陷修法見 **5.1n**。


#### 上一輪（5.0j · `json` 輪，數字已被 5.0k 取代）

### 5.0j 快照：2026-11-10 深夜（`json` 輪）

| 項目 | 數值 |
|---|---:|
| std 源檔 / 模組別名 | 119 / 119 |
| API 面（函數 + 方法） | 2091（773 + 1318） |
| 已覆蓋 | **1379（65.9%）**（`process` 輪結束時 1346 / 64.4%） |
| 有專用測試檔的模組 | 103 / 119 |
| 測試檔數 / 全量 `TEST` 區塊 | 108 / **918** |
| 全量斷言 | **4315** |

| 模組 | 缺口 → 覆蓋 | 測試檔斷言 | 內容 |
|---|---|---|---|
| `json` | 33 → **61 / 61（100%）** | 28 → **129** | 池級 12 項（`json-pool.init`/`alloc`/`add-child`/`skip-ws`/`parse-str`/`parse-num`/`get-key`/`set-key`/`copy-tree`/`get-kind`、`new-pool`、`clone-json-tree`）+ 高級 21 項（`kind`/`overflowed`/`get`/`get-key`/`alloc-node`/`set`/`set-null`/`set-key`/`delete-key`/`arr-push`/`arr-push-idx`/`arr-push-str`/`-num`/`-bool`/`num`/`bool`/`i64`/`str-val`/`obj-len`/`obj-key`/`obj-keys-str`） |

21 個新 `TEST`（p21–p41）。分母不變（本輪沒有新增/刪除公開符號）。

全量 `./bin/no test test/std/`：**108 檔、107 綠、918 個 TEST / 4315 條斷言**，
唯一紅檔仍是預存的 `multipart.no`；EXIT=1 僅由該檔造成。

`no vet test/std/json.no` = 0 error / 0 warning / 1 hint；`no vet src/std/json.no`
= 0 error / 0 warning / 1 hint；`no fmt -d` 兩個檔案皆空。
`tests/json.no` 與 `tests/json-more.no` 兩個端到端腳本全綠（後者專測
set-key / copy-tree 的自池與跨池行為）。


#### 上一輪（5.0i · `process` 輪，數字已被 5.0j 取代）

| 項目 | 數值 |
|---|---:|
| std 源檔 / 模組別名 | 119 / 119 |
| API 面（函數 + 方法） | **2091**（773 + 1318） |
| 已覆蓋 | **1346（64.4%）**（`str` 輪結束時 1320 / 62.3%） |
| 有專用測試檔的模組 | 103 / 119 |
| 測試檔數 / 全量 `TEST` 區塊 | 108 / **897** |
| 全量斷言 | **4214** |

| 模組 | 缺口 → 覆蓋 | 測試檔斷言 | 內容 |
|---|---|---|---|
| `process` | 47 → **41 / 41（100%）**（函數 21、方法 20） | 13 → **109** | 10 個底層內建（`process-fork`/`pipe`/`waitpid`/`waitpid-nohang`/`exec`/`kill`/`dup2`/`getppid`/`system`/`exec-shell`）、`start-with-opts`（含 `dir`/`input`/`merge-err`）、`start`、`wait-nohang`、`kill`/`terminate`/`force-kill`、`read`/`read-line`/`read-all`/`write`/`close-stdin`/`close-stdout`/`close-stderr`、`pid-of`/`exit-code-of`/`close`、`process-shell`、`shell-quote`、`cmd`（`input`/`env`/命令不存在/timeout 三態）、`exec`+`cmdopts`、`output`、`list-all`/`find-match`/`find-by-name`、`SIG-*`/`WNOHANG` 常量 |

36 個新 `TEST`（p6–p41）。

**分母 2118 → 2091**，兩個來源：

1. **平台不適用（−18，全局）**：`scripts/std_test_coverage.py` 新增平台感知——
   解析聲明前的 `#{mac-arm64, linux-amd64, …}` 標籤，與宿主標籤集合無交集者
   **不計入分母**（`buildin` / `track-caller` / `overflow=…` 不算平台標籤）。
   `--missing` 會單列 `platform N/A` 段。`process` 因此少 10 項（7 個 `#{win-*}`、
   `list-all` 的 linux 與 win 變體）。
2. **`UNREACHABLE`（−9）**：`process` 的 9 個 Win32 內建在 .no 裡只標 `#{buildin}`、
   沒標平台，但 Go 側僅在 Windows 註冊實現。macOS 上逐個實測統一報
   `EmitLLVM: unsupported builtin process.win-create-pipe [2kqboupo]`（僅符號名不同）。
   沒有用「給 src/std 加 `#{win-*}` 標籤」來修：`genstdsig` 不解析 `#{...}`
   （`stdsig_gen.go` 是跨平台共用的扁平表），加標籤既改變不了解析行為，
   還會讓聲明與簽名表不一致。

全量 `./bin/no test test/std/`：**108 檔、107 綠、897 個 TEST / 4214 條斷言**，
唯一紅檔仍是預存的 `multipart.no`（`quoted boundary [len]` + segfault，段錯誤導致
連 summary 都沒印，故只有 107 個 summary 區塊）；EXIT=1 僅由該檔造成。

`no vet test/std/process.no` = 0 error / 0 warning / 4 hint；`no vet src/std/process.no`
= 0 error / 0 warning / 47 hint；`no fmt -d` 兩個檔案皆空。
`./bin/no run tests/process-run.no` = ALL PASS（其中 [6]/[10] 兩個 timeout 用例已
改成子進程自關標準流的形態，原因見 5.1l）。


#### 上一輪（5.0h · `str` 輪，數字已被 5.0i 取代）

> 註：5.0c–5.0g 標題上的「2026-11-11」是前幾輪按錯時區推算的誤植，實際都發生在
> **2026-11-10**（`git log` 的提交時間可證）。本節起一律用本地時間。

| 項目 | 數值 |
|---|---:|
| std 源檔 / 模組別名 | 119 / 119 |
| API 面（函數 + 方法） | 2118（796 + 1322） |
| 已覆蓋 | **1320（62.3%）**（`number` 輪結束時 1272 / 60.0%） |
| 有專用測試檔的模組 | 103 / 119 |
| 測試檔數 / 全量 `TEST` 區塊 | 108 / 861 |

| 模組 | 缺口 → 覆蓋 | 測試檔斷言 | 內容 |
|---|---|---:|---|
| `str` | 48 → **63 / 63（100%）** | 78 → **179** | `byte`/`at`/`set-byte`/`eq-full`、`decode-cp`、`cp-index`/`byte-index`、`find-byte-from`/`rfind-byte-from`、`rindex`/`last-index`/`rindex-from`、`slice-bytes`/`to-str`/`reverse`/`compare`/`count`、`to-i8..i32`、`to-u8..u64`/`to-byte`、`to-f64`/`to-f32`/`str-to-f64`、`to-bool`、`replace-char`/`trim-char`、`empty`/`with-cap`/`with-len`/`with-len-cap`/`new`、`to-hex`/`str-hex-val`/`from-hex`、`to-bigint`/`bigint-bitlen`、`to-txt`、`split`/`split-n`/`fields`/`lines`、`replace-str`/`replace-n` |

`str` 是本輪唯一收口的模組，24 個新 `TEST`（p15–p38）。

**分母 2119 → 2118**：`str.eq-raw` 進 `scripts/std_test_coverage.py` 的
`UNREACHABLE` 清單——它是 `src/builtin/str.go` 裡 `str.eq` 的**內部 forward 目標**
（`ForwardFunc: "eq-raw"`），用戶碼兩種形態都調不到：`a.eq-raw(b, n)` 會被 checker
當方法解析、但 MIR 報 `unknown callee str.eq-raw`；`str.eq-raw(a, b, n)` 報
`module 'str' has no top-level function 'eq-raw'`。`str.eq` 本身已由 `t-eq` 覆蓋。

全量 `./bin/no test test/std/`：**108 檔、107 綠、861 個 TEST / 4114 條斷言**，
唯一紅檔仍是預存的 `multipart.no`（`quoted boundary [len]` + segfault，因段錯誤
連 summary 都沒印出來，故只有 107 個 summary 區塊）；EXIT=1 僅由該檔造成。

`no vet test/std/str.no` = 0 error / 3 warning / 27 hint；`no vet src/std/str.no`
= 0 error / 46 hint；`no fmt -d` 兩個檔案皆空。


#### 上一輪（`number` 輪，數字已被 5.0h 取代）

| 項目 | 數值 |
|---|---:|
| std 源檔 / 模組別名 | 119 / 119 |
| API 面（函數 + 方法） | 2119（797 + 1322） |
| 已覆蓋 | **1272（60.0%）**（`yaml` 輪結束時 1202 / 56.7%） |
| 有專用測試檔的模組 | 103 / 119 |
| 測試檔數 / 全量 `TEST` 區塊 | 108 / 837 |

| 模組 | 缺口 → 覆蓋 | 測試檔斷言 | 內容 |
|---|---|---:|---|
| `number` | 70 → **104 / 104（100%）** | 190 | `to-str-fixed`/`sci`/`general` 全套、`f64→整數`與`整數→f64`、窄整數拓寬矩陣、`to-bits`/`from-bits`/`to-bytes-be/le`、`sqrt/cbrt/sin…tanh/ceil/floor/round/trunc/exp/log/log10/log2/degrees/radians` 的 **f64 + f32 雙份**、`exp-f64` |

`number` 是本輪唯一仍在缺口裡的模組；分母 2119 不變（新增的 4 個助手是 `_` 私有，
不入分母）。全量 `./bin/no test test/std/`：**108 檔、107 綠、837 個 TEST**，
唯一紅檔仍是預存的 `multipart.no`（`quoted boundary [len]` + segfault）。


#### 上一輪（`yaml` 輪，數字已被 5.0g 取代）

| 模組 | 缺口 → 覆蓋 | 測試檔斷言 | 內容 |
|---|---|---:|---|
| `yaml` | 104 → 72 / 123（58.5%） | 221 | 多文檔/`doc`/`to-json-all`、全部 `is-*`、`style`/`tag`/`anchor`/`line-of`、`get-*`/`key-node`/`value`/`scalar-text`、錯誤與 BOM、21 個內部輔助函式直測；**顯式 `!!` 標簽從不生效被修** |

`yaml` 剩下的 51 個全部需要 `yaml-parser` 或 `yaml-node` 的實例（47 個解析器方法 +
`yaml-{key-text-eq,key-eq,node-is-nan,scalar-repr}`）。**用戶碼構造不出這兩個型別**：
`yaml.yaml-node{}` 能編譯，但型別 raw 是 `yaml.yaml-node` 而 `StructFields` 註冊的是裸名
`yaml-node`，`StructKeyOf` 的「裸名 → 限定名」後備掃不到反向情形 ⇒ 欄位讀到垃圾、
`setfield` 直接報 `3nktiaae`。這是語言層缺陷（見 §5.2 #39），不是該輪遺漏。


#### 更早（`toml` + `dom` + `bigint` + `fs` + `os` 收口，數字已被 5.0f 取代）

| 模組 | 缺口 → 覆蓋 | 測試檔斷言 | 內容 |
|---|---|---:|---|
| `toml` | 16 → 26 / 26 + 方法 3 / 3 | 97 | 條目/表名/`find-eq`/key 正規化/值規則/文檔模型/數字與布林 |
| `dom` | 49 → 全綠 | 313 | 節點池原語、free 助手、裸池解析入口、**15 個單步解析原語** |
| `bigint` | 15 → 29 / 29 | 112 | 絕對值加減、同型加減乘、`shl1`/`shr1`、`normalize`、`to-hex`、`mod-pow` |
| `fs` | 46 → 43 / 46 | 83 | 目錄/連結/fd/`file.*` 方法；**`file.append` 截斷整檔被修** |
| `os` | 40 → 33 / 40 | 61 | 身份/系統資訊/時間/stat 權限/目錄；**`now-ms`/`now-us` 量級錯被修** |

`fs` 剩 3 個（`get-line`/`read-stdin`/`read-stdin-str`）與 `os` 剩 7 個
（`chroot`/`set-sid`/`syslog` 與 4 個 Windows-only）都是**刻意不測**，理由寫在檔頭註解。

`dom` 這一輪把 16 個「內部但公開」的掃描器全部直測（不必繞 `parse-xml`）：
`xml-{add-text,parse-comment,parse-cdata,parse-pi,parse-attr,parse-close,parse-element}`、
`parse-doctype-node`、`html-{add-text,parse-comment,bogus-comment,parse-pi,parse-attr,set-attr-once,parse-open,parse-close-name}`。
斷言全部落在「回傳的下一個位置 + 節點池裡真的寫了什麼」兩個維度上（`ser` / `text-of` / `find-attr` 讀回）。

分母 2117 → 2118 的 +1 是 `dom-pool.err-pos-or`（本輪新增，見 §5.1f）。

### 5.1f 本輪修好的 std 缺陷（`src/std/dom.no` 的錯誤位置回報）

**症狀**：`dom-pool.xml-parse-doc` 的 `failpos` 文件寫「解析失敗的位元組位置」，
但實測 `<r><a></r>` 回 **0**（外層 `<r>` 的起點），不是內層 `<a>` 該失敗的 3。
`dom.parse-xml` 的錯誤字串因此永遠指向文件開頭：`xml parse error at byte 0`。

**根因**：`xml-parse-doc` 每個失敗分支都寫 `failpos = p`，而 `p` 是**頂層**掃描位置；
`xml-parse-element` 遞迴失敗時只回 `?i64` 的 nil，位置信息在遞迴中被丟掉。

**修法**（不動任何既有函式簽名）：

1. `dom-pool` 加純量欄位 `errpos i64`（`init()` 設 -1）；
2. `xml-parse-element` **進入時**寫 `.errpos = p` —— 遞迴是外→內，最後寫入的自然是
   最內層嘗試解析的元素起點，一行就拿到「最深」；
3. 新增 `dom-pool.err-pos-or(dflt)` 取 `max(errpos, dflt)`：嵌套失敗給內層位置，
   頂層失敗（第二個根、非 `<` 字元）給掃描點，兩者都正確；
4. `xml-parse-doc` 10 處 `failpos = p` 全改 `failpos = .err-pos-or(p)`。

實測：`<r><a></r>` → 3；`<r/>x` → 4；成功 → -1。`no vet` 0 error。

### 5.1g 本輪修好的 std 缺陷（`src/std/bigint.no`：`to-hex` 死迴圈、`mod-pow` 不取模）

兩個都是**真錯**，不是文件與實作不一致：

**① `to-hex` 無限迴圈（OOM 被 SIGKILL，`no run` 直接 137 無任何輸出）**

最高 limb 的「略去前導零」內層迴圈把 `shift` 遞減那一步寫在了**迴圈體外**：

```
(shift >= 0) {
    ...
    out[out-n] = hex[nibble]
    { out-n-opt2 = out-n + 1  ... }
}                      ← 迴圈體到此結束
{ shift-opt2 = shift - 4  ... }   ← 每個 i 只跑一次
```

`shift` 永遠停在 28，於是每輪都往 `out` 尾部追加同一字節；`out` 是會自動增長的 vec
⇒ 記憶體爆掉 ⇒ SIGKILL。另有兩處附帶錯誤：`done == 1 || nibble != 0` 是**無效果的裸表達式**
（前導零根本沒被略過），第二個 limb 迴圈從 `shift = 28` 起跑（limb 只有 32 位，
若最高 limb 用 28 起算會少 4 位元）。

修法：最高 limb 用同一套 `started` 旗標（`nibble != 0 -> started = 1`，`started == 1 -> 寫入`），
`shift` 從 28 起、遞減語句放回迴圈內，兩個 limb 共用同一個迴圈體（32 位 = 8 個 nibble）。

驗證：`255 → ff`、`2^32-1 → ffffffff`、`2^32 → 100000000`、`16^20 → 1` + 20 個 `0`、`-16 → -10`、`0 → 0`。

**② `mod-pow` 完全沒有取模**

`mod-pow(exp, modulus)` 只做平方-乘，**從頭到尾沒碰 `modulus`**（`modulus=0` 的特例有處理）。
`2^10 mod 1000` 回 1024 而不是 24；指數一大中間值就無界膨脹，文件卻寫它是
「RSA 簽章/加解密核心運算」。

修法：平方後、乘 `b` 後各做一次 `div-mod(modulus)` 取餘。

驗證（與 Python `pow(a,e,m)` 對齊）：`2^10 mod 1000 = 24`、`3^5 mod 7 = 5`、`7^13 mod 11 = 2`、
`5^117 mod 19 = 1`、`2^64 mod 1e9+7 = 582344008`、`exp=0 → 1`、`modulus=0 → 0`。

### 5.1h 本輪修好的缺陷（`src/std/fs.no` 的 `file.append`、`src/mir/codegen.go` 的 `now-ms`/`now-us`）

**① `file.append` 每次追加都先把整個檔案清空**

實測：寫入 9 位元組 → `append('l4\n', 3)` → 檔案只剩 3 位元組。

根因：追加的實作是「讀出既有內容 → 以 `open-file(path, 1537, 420)` 重開 → 先寫回既有內容再寫新資料」，
但 **1537 這個魔術數字含 O_TRUNC**：

| 平台 | O_WRONLY | O_CREAT | O_TRUNC | O_APPEND | 1537 實際含義 |
|---|---:|---:|---:|---:|---|
| Darwin | 1 | 512 | 1024 | 8 | WRONLY \| CREAT \| **TRUNC** |
| Linux | 1 | 64 | 512 | 1024 | WRONLY \| **TRUNC** \| APPEND |

兩個平台都帶 O_TRUNC ⇒ 一開就把檔案清空。而且模組裡本來就有平台無關常量
`O-WRONLY`/`O-CREAT`/`O-APPEND`（`fs.no:404` 起，按 `darwin/linux/windows/...` 各給一份值）。

修法：刪掉整個「先讀再重寫」的舞蹈（O_APPEND 由內核定位到檔尾，重寫反而會**重複內容**），改成

```
wr-fd = open-file(save-path, O-WRONLY | O-CREAT | O-APPEND, 420)
```

同時把 `file.copy-to` 的 `1537` 也換成 `O-WRONLY | O-CREAT | O-TRUNC`（語義相同但不再依賴平台數值）。
驗證：9 位元組 → append 3 → **12 位元組且內容為 `l1\nl2\nl3\nl4\n`**。

另外更正 `file.read-line` 的文件：它其實一次 `read` 至多 4096 位元組、**不做換行切分**
（真正的逐行需要跨呼叫緩衝，`file` 結構體沒有該欄位）。

**② `os.now-ms` / `os.now-us` 回的不是 epoch 時間（Darwin 上大 ~1e6 倍）**

實測：`now` = 1794297750（正確），`now-ns` 正確，但 `now-ms` = 4124612157701034、
`now-us` = 4124612157701034845 —— 比正確值大三個數量級。

根因在 MIR 前言的 IR（`src/mir/codegen.go`）：`struct timeval` 的第二個欄位 `tv_usec` 在
Darwin 是 **32 位**（`suseconds_t`），Linux 才是 64 位。前言卻用

```
%usec.ptr = getelementptr i64, i64* %sec.ptr, i64 1
%usec = load i64, i64* %usec.ptr      ; ← 連同 4 位元組尾部填充一起讀，高位是棧上垃圾
```

修法：改成讀低 32 位再 `zext`（`tv_usec < 1000000`，兩種平台都正確）：

```
%tv32 = bitcast [16 x i8]* %tv to i32*
%usec.ptr = getelementptr i32, i32* %tv32, i64 2
%usec.raw = load i32, i32* %usec.ptr
%usec = zext i32 %usec.raw to i64
```

驗證：`now-ms/1000 == now`、`now-us/1000000 == now`、`now-ns/1000000000 == now`
（已寫成 `test/std/os.no` 的 `t-clock` 斷言，修前必紅）。

### 5.1i 本輪修好的缺陷（`src/std/yaml.no` 的顯式 `!!` 標簽、`src/std/crypto/aes.no` 的迴圈變數）

**① 顯式 `!!` 標簽只被存成字串，從不改寫節點類型**

實測（YAML 1.2 要求標簽優先於 core schema 推斷）：

| 輸入 | 修前 kind / JSON | 修後 kind / JSON |
|---|---|---|
| `a: !!str 123` | INT(2) → `"a":123` | STR(4) → `"a":"123"` |
| `b: !!int "42"` | STR(4) → `"b":"42"` | INT(2) → `"b":42` |
| `c: !!bool "true"` | STR(4) → `"c":"true"` | BOOL(1) → `"c":true` |
| `d: !!float "1.5"` | STR(4) → `"d":"1.5"` | FLOAT(3) → `"d":1.5` |
| `e: !!null "x"` | STR(4) → `"e":"x"` | NULL(0) → `"e":null` |

根因：節點由 `parse-plain`/`parse-double`/`parse-flow-*` 先按 core schema 建好，
`yaml-parser.set-props` 之後才把標簽寫進節點 —— 但它只做 `.nodes[idx].tag = tag`，
**沒有回頭套用標簽**。真正會套標簽的 `yaml-apply-tag` 只在 `push-scalar` 裡被呼叫，
而所有呼叫端傳進去的 `tag` 都是空串（標簽是稍後才讀到的），所以那段邏輯是死路。

修法（不動簽名、不重複實作）：

1. 新增 `yaml-parser.apply-tag-at(idx)`：把節點身上已有的 `tag` 套回 `kind`，
   並按新 kind 重算 `ival`/`fval`/`bval`（`!!int "42"` 若不重算，kind 對了但 `ival` 仍是 0，
   `yaml.i64` 會取到 0）；集合節點（seq/map）只保留標簽原文。
2. `set-props` 末尾呼叫它 —— 這是所有 scalar/collection 的必經之路；
3. `push-scalar` 的內聯標簽區塊整段刪掉，改為 push 之後呼叫同一個 `apply-tag-at`，
   避免兩份實作漂移。

`apply-tag-at` 讀節點必須用 `?=` 綁定（裸下標 `arr[i]` 回 `?T`，直接當普通值用會被
`no vet` 判 `idxbruse`）。

**② `src/std/crypto/aes.no` 的 `enc-block` 在迴圈裡宣告 `rk-off`，迴圈後又用同一個名字**

`make test` 的 `build` 包 3 個 `TestNomlImport*`（會編譯含 std 的 fixture）全紅：

```
line 1220: variable 'rk-off' is declared inside a loop body and may be uninitialized
           when used here (loop might execute zero iterations) [sjuueqqo]
```

`enc-block` 裡「迴圈內算本輪輪金鑰偏移」與「迴圈後算最後一輪偏移」原本共用 `rk-off`，
迴圈那一次宣告就成了函式級變數 ⇒ 零次迭代時未初始化。把迴圈內那個改名 `rk-off-i`
即解（`dec-block` 用的 `rk-off-opt` 是同樣手法的既有寫法）。這條規則在 HEAD 就有，
`src/std` 沒跟上，屬預存紅檔；修後 3 個測試全綠。

### 5.1j 本輪修好的缺陷（`src/std/number.no` 的格式化與位元組序列）

覆蓋 `number` 的 70 個缺口時，`to-str-*` 家族有 4 個真 bug、`to-bytes-*` 有 1 個。

**① `f64.to-str-sci` 的負指數輸出成 `1.23e--4`**

取絕對值那段寫成「區塊形式的值位 match」：

```no
ae = e
#{overflow=wrap}
e < 0 -> {
    { ae-opt = 0 - ae
      ae-opt: { ok -> ae = it  -> } }
}
```

實測 `ae` 保持負數（payload 走空 `->` 分支），於是符號位與 `ae` 自帶的負號疊成 `--`。
改成單行 `e < 0 -> ae = 0 - ae` 即正確（同 `f64.to-str` 的既有寫法）。

**② `to-str-sci` / `to-str-general` 的「四捨五入進位」整段沒生效**

`9.99.to-str-sci(1)` 修前得 `1.0e+00`、`9.9.to-str-general(1)` 得 `10`
（Go 分別是 `1.0e+01` / `1e+01`）。根因同 ①：`e-opt = e + 1` + `e-opt: { ok -> e = it }`
在**這些 `cond -> { … }` 區塊裡**寫不回去（§5.2 #43 記了這個形態本身）。

**③ `f64.to-str-general` 剝完尾隨零留下裸小數點**

`9.99.to-str-general(2)` 得 `10.`（Go 是 `10`）。守衛寫反成 `end > dot-pos + 1`，
恰好把「只剩小數點」這個邊界排除在外：

```no
end > dot-pos -> {
    s.byte(end - 1) == 46 -> end = end - 1
}
```

**④ `i64.to-bytes-be` / `to-bytes-le` 返回型別是 `[?]byte`**

元素型別待定的定長陣列 ⇒ 實測長度 0、8 次寫入全部丟失、讀回全 0，`len()` 還撞
`str-len fixed-array [0 x i8] [stbjgjqz]`。改成 `[]byte` + 首行 `out = with-len(8)` 後
`258.to-bytes-be()` 才是 `[0,0,0,0,0,0,1,2]`（`b6=1 b7=2`）、`to-bytes-le()` 才是 `c0=2 c1=1`。

修後的取值全部寫進 `test/std/number.no`（`t-fmt-fixed` / `t-fmt-sci` / `t-fmt-general` /
`t-bits`），並以 Go 的 `strconv.FormatFloat` 同格式為契約（含 `-0`、`inf`、`nan`、
`1.2346e+05` 這些邊界）。

**⑤ 為了同時滿足「執行正確」與「`no vet` 乾淨」而新增的 4 個私有助手**

`e = e + 1`（`#{overflow=wrap}` 不改變 `?i64` 型別）在 `no vet` 是硬錯誤 `15w45dqk`，
而就地兩段式解包在這些區塊裡寫不回去（見 §5.2 #43）。兩者都成立的最小寫法是**過一次
函數邊界**：`_num-add` / `_num-sub` / `_num-mul` / `_num-div`（`_` 前綴私有，不入
覆蓋率分母），函式內 `out = s: { ok -> it  -> a }` 解包，呼叫端拿到普通 `i64`。

### 5.1k 本輪修好的 std 缺陷（`src/std/str.no` 的 `to-bigint` 與覆蓋率口徑）

**① `str.to-bigint` 對任何輸入都回「0」（真的解析成功，但值是零）**

`'255'.to-bigint()` / `'-255'` / `'0x1f'` / `'0b101'` / `'0o17'` 全部回 ok，`to-str()` 都是 `0`，
`str.bigint-bitlen()` 因此恆為 0；`str-to-f64` 用它當精確 strtod 的底，跟著一起歪。

**根因不在 `to-bigint` 的演算法，而在 `bigint` 的運算符重載臂派發**：

```no
    (pos < n) {
        d = _str-dig-val(.byte(pos))   ; ← 未標註的「呼叫結果」
        ...
        acc = acc * radix
        acc = acc + d                   ; ← 這裡選錯聯合臂
    }
```

`bigint._add = (b big) (c ?bigint)` 用 `big { bigint | i64 }` 聯合形參 + 型別 match 靜態選臂。
當右運算元的型別來自**未標註的函數呼叫結果**時，選臂退化成 `bigint` 臂 ⇒ `add-bigint`
把 i64 位元當 bigint 描述符解讀。實測三態：
`acc + 2`（字面量）正常、`x = 2; acc + x`（字面量指派）正常、`d = f(); acc + d`（呼叫結果）
**回 0 或直接 `abort trap`**。

**修法**：在 `src/std/str.no:2452` 給它顯式標註：

```no
        d i64 = _str-dig-val(.byte(pos))
```

修後 `'255'` → `255`、`'0x1f'` → `31`、`bigint-bitlen(from-i64(255))` → `8`。
（編譯器側這個選臂缺陷本身**未修**，見 §5.2 #50。）

**② `str.eq-raw` 從分母移除（不是漏測）**

見 §5.0h 的說明與 `scripts/std_test_coverage.py` 的 `UNREACHABLE` 清單。
順帶把該腳本兩處「私有符號」文案改成「私有 / 不可達」，避免把「調不到」混淆成「私有」。

### 5.1l 本輪修好的 std 缺陷（`src/std/process.no` 的 `cmd` 超時看門狗）

`process.cmd(..., timeout>0, ...)` / `process.exec` 在 `timeout > 0` 時**行為全錯**，
而且是兩個獨立的缺陷疊在一起。

**① 任何瞬間結束的命令都回報 `-2`（超時）—— 二次收割**

看門狗用 `process-waitpid-nohang(pid)` 輪詢。這個內建是**收割式**的
（`waitpid(pid, WNOHANG)`）：它一旦回傳真實退出碼，子進程已被回收。舊實作在迴圈
`break` 之後又對同一 pid 呼叫一次，拿到 `-1`（ECHILD），於是走「仍在執行」分支：

```no
                process-waitpid-nohang(pid) != -1 -> break   ; 已收割
            }
            {
                process-waitpid-nohang(pid) == -1 -> {        ; ← 誤判：其實是 ECHILD
                    process-kill(pid, 9)
                    code = -2
```

實測（修前）：`cmd('sh',['-c','exit 3'], …, 5000, false)` → `-2`；
`cmd('sh',['-c','echo a'], …, 5000, false)` → `-2` 但 `out` 是 `a\n`；
只有 `timeout = 0` 走另一條分支才是對的。

**修法**：把觀測到的退出碼留在 `rc` 裡，迴圈外只判 `rc == -1`：

```no
                rc = process-waitpid-nohang(pid)
                rc != -1 -> break
                …
            {
                rc == -1 -> { process-kill(pid, 9); code = -2; err = 'process timed out' }
                -> code = rc
            }
```

**② `elapsed` 恆為 0，看門狗永不過期 —— 值位 match 靜默不寫回**

累加寫成「先算出 `?i64`、再用**值位** match 回寫」：

```no
                elapsed-opt = elapsed + 10
                elapsed-opt: { ok -> elapsed = it  -> }     ; ← 靜默不寫回
```

在 `timeout > 0 -> { … }` 這類條件塊裡，值位 match 的回寫不生效，`elapsed` 一直 0
⇒ `elapsed >= timeout` 永不成立。改成**語句位** match 後正確（與 §5.2 #43 同源）：

```no
                next = elapsed + 10
                next: {
                    ok -> elapsed = it
                    -> elapsed = timeout
                }
```

> 註：`#{overflow=wrap}` + 直接 `elapsed = elapsed + 10` 在函數內是**硬錯誤**
> `15w45dqk`（`no vet` 會報，`no run` 竟然放過——這也是為什麼這個 bug 能活下來）。

**③ 文件承諾的 `err` 從未設定**

`cmd` 的 doc 寫「`err`: 啟動失敗或超時的錯誤描述」，但超時分支只設 `code = -2`。
已補 `err = 'process timed out'`。

**④ 仍然存在的限制（本輪不修，需要新內建）**

看門狗排在**阻塞式讀完 stdout 之後**：

```no
    !! { n = fs.read(out-read, buf, 4096)  … }        ; ← 子進程不關 stdout 就一直在這
    fs.close(out-read)
    { timeout > 0 -> { …看門狗… } }
```

所以 `cmd('sleep', ['5'], …, 200, false)` 仍會等滿 5 秒、回報自然退出碼 0。
真要修必須有非阻塞讀 + `poll`/`select`，而 `src/std/fs.no` 目前沒有
`poll` / `select` / `fcntl` / `O_NONBLOCK` 任何入口。測試與 `tests/process-run.no`
因此統一用「子進程自關標準流」的形態（`sh -c 'exec 1>&- 2>&-; sleep 5'`）驗證 `-2`
路徑確實可達——實測 200ms 級返回。

> `tests/process-run.no` 的 [6]/[10] 兩個 timeout 用例原本用 `sleep 5` + `timeout=200`
> 並期望 `-2`，它們過去是**靠缺陷①通過的**（任何命令都回 -2）。已改成自關標準流的
> 形態，保留「超時會殺死子進程」的原意，而不是把錯誤行為釘進測試。

### 5.1m 本輪修好的 std 缺陷（`src/std/json.no` 的 `arr-push` 自環）

**症狀**：`json.new()` 之後 `j.arr-push(sub)`（`sub` 是任何 `json.parse` 出來的物件）
再 `j.stringify()` → **segfault**；`j.arr-len()` 卻是正常的 1。

**根因**：`arr-push` 把來源池的節點索引直接當成本池索引用，而 doc 一直宣稱會深拷貝：

```no
json.arr-push = (val json) (ok bool) {
    ok = .arr-push-idx(val.root)      ; ← val.root 是 **來源池** 的索引
}
```

`json.parse` 出來的子樹 root 恆為 `0`；`json.new()` 的池是空的，`arr-push-idx` 先
`alloc` 出 root（也是 `0`）並標成 ARR，然後 `add-child(0, 0, '')` —— 把節點掛成
**自己的子節點**。`stringify` 沿邊表遞迴 → 無限遞迴 → 爆棧。

實測（修前）：

```
sub root> 0          sub pool count> 3
b2 root> 0           b2 pool count> 1     ← 沒有拷貝任何節點
b2 arr-len> 1
Error: signal: segmentation fault
```

**修法**：照 `set-key` 已有的兩跳模式深拷貝（先 `clone-json-tree` 到全新池，再
`copy-tree` 進本池）；兩次拷貝的兩端必不同池，因此不依賴池身份判定，
`arr-push(j)` 這種自引用也安全：

```no
    val.root < 0 || val.root >= val.pool.count -> return
    tmp = clone-json-tree(val.pool, val.root)
    v-idx = .pool.copy-tree(tmp.pool, tmp.root)
    v-idx < 0 -> return
    ok = .arr-push-idx(v-idx)
```

修後同一段：`b2 root> 2`、`b2 pool count> 3`、`stringify` = `[{"x":1}]`。

⚠️ 這個 bug 與 §5.2 #42 (`static-hashmap` 死代碼) 不同類：它是**公開 API 的崩潰**，
而且只在新池上撞號時才觸發（既有 `tests/json.no` 的 `a.arr-push(json.parse('3'))`
沒崩，是因為 `json.parse('3')` 的 `?json` 走的是另一條取值路徑）。
`test/std/json.no` 的 `t-arr-push` 現在把它釘成回歸（含 `pool.count` 斷言與二次追加）。

### 5.1n 本輪修好的 std 缺陷（`src/std/regexp.no` 的兩處靜默截斷）

`regexp` 的編譯結果存在固定大小的結構體陣列裡：`op` / `arg1` / `arg2` 各
`[128]i64`，`cls [512]byte`（每類 32 bytes ⇒ 只能放 16 個類別）。兩個上限都用
「越界 store 回 err、err 臂 `-> _` 吞掉」的寫法落地，所以**超限時既不報錯也不標記**，
`compile()` 照樣把 `compiled` 置真。

修前實測：

| 案例 | 期望 | 實測 |
|---|---|---|
| `[a][b]…[m]`（13 類，3 量詞類 + 13 = 16） | 匹配自身 | ✅ 命中 |
| `[a][b]…[n]`（14 類，3 + 14 = 17） | 匹配自身 | ❌ `matches` 回 false，`ncls` 卻印 17 |
| 127 個字面量字元（+ MATCH = 128 條指令） | 匹配自身 | ✅ 命中 |
| 128 個字面量字元（+ MATCH = 129 條指令） | 匹配自身 | ❌ `n` 停在 128，`MATCH` 發不出去 ⇒ 恆不匹配 |

根因不是「上限太小」而是**上限被靜默吞掉**：`emit` / `set-class-bit` 這類函數用
`{ .op[.n] ?= op … }: { err -> _ }` 落地，位元組陣列的「越界 err」與「這次 store
沒成功」在語意上是同一件事，但編譯器狀態上必須區分，否則呼叫方無從得知編出來的
program 是截斷的。

修法（三處，都在 `src/std/regexp.no`）：

1. 引入常量 `RE-CLS-MAX = 32`、`RE-OP-MAX = 256`，陣列改成
   `op/arg1/arg2 [256]i64`、`cls [1024]byte`。結構體從 ~3.5 KB 漲到 ~7 KB
   （`regexp` 是堆疊值，可接受），實用上限翻倍。
2. 新增 `overflowed bool` 欄位。`emit` 在 `.n >= RE-OP-MAX` 時
   `.overflowed = true` 並 `return`；`alloc-class` 在 `.ncls >= RE-CLS-MAX` 時
   同樣置位並**返回 -1**。
3. `compile()` 開頭 `.overflowed = false`，讓重複編譯不殘留上一次的狀態。

`alloc-class` 選 -1（而非「返回最後一個合法索引」）是刻意的：-1 讓後續
`base = cls-idx * RE-CLS-SIZE`（`#{overflow=wrap}`，得 -32）統一落到所有 bitmap
操作的越界 err 分支，於是該類別**恆不匹配任何字元**，不會誤用相鄰類別的 bitmap。

修後實測：

| 案例 | `n` / `ncls` | `overflowed` | `matches` 自身 |
|---|---|---|---|
| 13 類 | 14 / 16 | false | ✅ |
| 29 類（3 + 29 = 32，剛好佔滿） | — / 32 | false | ✅ |
| 30 類（3 + 30 = 33，溢出） | — / 32（飽和） | **true** | ❌（有旗標可判） |
| 127 字元 | 128 | false | ✅ |
| 255 字元（+ MATCH = 256，剛好佔滿） | 256 | false | ✅ |
| 256 字元（+ MATCH = 257，溢出） | 256（飽和） | **true** | ❌（有旗標可判） |

`t-capacity-class` / `t-capacity-op` 把上表釘成回歸。`alloc-class` 溢出後
`ncls` 飽和、`in-class(31, 97)` 仍為 true、`in-class(-1, 97)` 為 false（同時證明
溢出既不崩、也不會誤命中）也一併釘住。

⚠️ 這仍是「有上限」的設計（無動態配置），`overflowed` 只是把「靜默錯」降級成
「可判讀的錯」。超過 32 個字元類別或 256 條指令的 pattern 目前無法正確編譯。

### 5.1o 本輪修好的 std 缺陷（`src/std/time.no` 的 `timer.elapsed-s` / `elapsed-us` 恆回 0）

`timer.elapsed-s` 與 `timer.elapsed-us` 對**任何非零差都回 0**，只有 `elapsed-ms` 正常。
修前實測（`time.timer{start-ts: 0, end-ts: 1500}`）：

| 方法 | 期望 | 修前 | 修後 |
|---|---:|---:|---:|
| `elapsed-ms` | 1500 | 1500 ✅ | 1500 |
| `elapsed-s` | 1 | **0** ❌ | 1 |
| `elapsed-us` | 1500000 | **0** ❌ | 1500000 |

根因不是算術，而是**算術結果沒拿到值**：

```no
; 修前
#{overflow=wrap}
ms = .end-ts - .start-ts        ; ← ms 的型別是 ?i64，內容是 nil
#{overflow=wrap}
{
    #{overflow=wrap}
    s-opt = ms / 1000           ; ← nil 當運算元 ⇒ 靜默 0，不報錯
    s-opt: { ok -> s = it  -> }
}
```

`elapsed-ms` 之所以正常，是它把算術寫成**值位 match 直接賦值**（`ms = .end-ts - .start-ts: { ok -> it  -> }`），
賦值處就完成解包；`elapsed-s` / `elapsed-us` 少了這一步，
而 `nil` 作運算元既不報錯也不進 `ok` 臂，於是恆 0。

`no vet src/std/time.no` 修前後都是 **0 error** —— 這個形態完全不在 vet 的視野裡，
只能靠行為測試抓（本輪就是靠「同一物件的三種粒度互相比對」抓到的：`elapsed-ms`
有值而另兩個為 0，不可能是真實時間差）。

修法是同一條：算術就地走值位 match。

```no
; 修後
#{overflow=wrap}
ms = .end-ts - .start-ts: {
    ok -> it

    -> 0
}

#{overflow=wrap}
s = ms / 1000: {
    ok -> it

    -> 0
}
```

`elapsed-us` 同形（`us = ms * 1000: { … }`）。修後含負差在內全對：
`elapsed-s(5000→1000) == -4`、`elapsed-us(5000→1000) == -4000000`
（與 C 的整數除法同向取整）。

迴歸釘在 `test/std/time.no` 的 `t-timer-run`：`elapsed-ms == 1500`、
`elapsed-s == 1`、`elapsed-us == 1500000`，外加負差兩條，並斷言
`elapsed-us == elapsed-ms × 1000`、`elapsed-s == elapsed-ms / 1000`。

⚠️ 這個形態不只出現在 `time`。全庫只要寫成「`#{overflow=wrap}` + 純賦值 +
稍後拿該變數參與算術」都命中，`no vet` 一律不報。排查方式與完整六變體對照見
**§5.2 #61**。

### 5.1d 本輪修好的 std bug（CBC 模式殘塊越界寫）

**症狀**：CBC 加密在輸入長度不是塊大小整數倍時，輸出長度 = **向上取整**到塊的倍數，
尾端多出一個「用零填充的殘塊」加密結果。`tdes-cbc-enc(11 bytes)` 回 16 位元組，
`aes-cbc.cbc-enc(20 bytes)` 同理回 32 位元組。

但四處 doc 都寫「不足一區塊的尾部會被丟棄」「長度與 in 相同」⇒ 文件與實作相反。

**根因**：迴圈條件寫成 `(blk < in.len())`，但內層固定跑 8（或 16）次下標讀寫：

```
out = with-len(in.len())
blk = 0
(blk < in.len()) {                       ; ← 少了 blk + 8 <= in.len()
    i <- [0..8): { ... in[blk + i] ... out[blk + i] ... }
    blk += 8
}
```

`in[blk + i]` 越界時走 option 空分支（被靜默吞掉，當 0 用），而 `out[blk + i]`
越界時**會自動把 vec 增長到 blk+8**，所以最終長度變成 `ceil(len/8)*8`，不會 SIGSEGV
但語意錯、且多吐一段密文。這條「vec 下標寫越界會自動增長」的行為本輪順手實測確認。

**修法**（`tdes-enc` / `tdes-dec` / `tdes-cbc-enc` / `tdes-cbc-dec` /
`aes-cbc.cbc-enc` / `aes-cbc.cbc-dec` 共 6 處）：

```
out = with-len((in.len() / 8) * 8)       ; aes 版為 /16*16
blk = 0
(blk + 8 <= in.len()) {
```

四處 doc 的「長度與 in 相同」同步改成「長度為 (in.len()/8)*8，不足一整塊的尾部丟棄」。

**影響面**：`tdes-cbc-*` 除測試外無呼叫者；`aes-cbc.enc-128/dec-128` 的呼叫者
（`src/std/net/tls.no` 四處）傳的都是 pkcs7 對齊後的資料，行為不變。

**回歸**：`test/std/tdes.no` 13/14 → **21/21**（新增 11B/9B 未對齊、解密未對齊、
`tdes-enc` 的 n=20 等）；`test/std/aes-cbc.no` 33/33 不變。

### 5.1e `crypto/argon2` 的實測結論（仍未修，但已量化）

模組頭注原本只籠統寫「與 RFC 9106 不一致」。本輪用 RFC 參考實作逐函式比對，
把缺口縮小到兩個函式：

| 函式 | 與 RFC 9106 | 說明 |
|---|---|---|
| `rotr` | ✅ 完全一致 | 64-bit 循環右移 |
| `blake-g` | ✅ 完全一致 | §3.5 GB（含 `2*lo32(a)*lo32(b)` 乘數）|
| `h-prime` | ❌ 只有 out-len=64、以及 out-len>64 的前 32 位元組對 | 見下 |
| `compress` | ❌ 列置換仍與 §3.4 有差異 | 見下 |

`h-prime` 的病灶很明確：它只有 `blake2.b()`（固定 64 位元組）可用，
於是 **≤64 的分支直接截斷 64 位元組摘要**，而 RFC 要的是
`Blake2b(LE32(T)||X, digest_size=T)` —— BLAKE2 的 digest_size 會改變參數塊
（`h[0] ^= 0x01010000 ^ digest_len`），**不是截斷**。同理 >64 分支的最後一段
`last-hash = blake2.b(v)` 也丟掉了「長度 = remaining」這個參數（T=72 時應為 40，
實作給 64 截斷；T=1024 時 remaining 恰為 64 才僥倖正確）。

要修就得先給 `src/std/crypto/blake2.no` 加一個可變摘要長度的入口（目前只有
`b` 固定 64、`b-256` 固定 32），再重寫 `compress` 的列索引。這是獨立一輪的工作，
故本輪只寫「可精確對齊的部分 + 結構不變量」，不寫假金標：

- `h-prime`：長度契約（1/32/200/1024）、確定性、
  **`h-prime(x,64) == blake2.b(LE32(64)||x)`**（這條是真·RFC 斷言且通過）、
  `h-prime(x,128)` 前 32 位元組 == `blake2.b(LE32(128)||x)` 前 32 位元組。
- `compress`：長度 1024、**`compress(X,X)` 全零**（X⊕X=0 ⇒ 所有 GB 作用在全零上）、
  **對稱性 `compress(X,Y)==compress(Y,X)`**、確定性。

### 5.1 (a) 地圖輪修好的 std bug（`src/std/collection/map.no`）

寫 `map.no` 時撞出來的，兩處都已修並被 `test/std/map.no` 釘住：

1. **`rehash` 死循環**：遷移條目的探測循環寫成 `used != 0 -> break`
   （應該是「找到空槽才停」），舊表只要有一條目就永遠找不到停點。
   修法：改 `used == 0 -> break`（str / int 兩個模板各一處）。
2. **探測遞增不生效**：`#{overflow=wrap} { #{overflow=wrap} idx-opt = idx + 1
   idx-opt: { ok -> idx = it; -> } }` 這種「裸塊 + 臨時變數 + match 臂賦值」
   的形態，在 `!!` 循環裡對外層 `idx` 的寫入**靜默丟失**（實測 `idx` 卡在同一槽），
   於是哈希一旦衝突就死循環 —— 表現為 `map[str]i64` 插到第 13 個鍵整支程式掛死
   （12 個鍵以內不衝突，所以從前沒被發現）。
   修法：5 處一律改成直接賦值形態 `idx = idx + 1: { ok -> it; -> }`，
   改完 20 鍵自動擴容、`rehash` 後續插全部正常。
   ⚠️ 這是 std 端的繞道，**編譯器「裸塊內 match 臂賦值丟寫」的根因還沒修**。

### 5.1b 本輪修好的 std bug（`src/std/archive/*`）

把 `archive/{gzip,zip,tar,zstd,xz,zlib}` 的每個公開符號都寫成 round-trip 測試時，
一共挖出並修好 8 個缺陷。每個都以真實工具（`tar` / `unzip` / `zstd` / CPython
`zlib`・`gzip`・`zipfile`）交叉驗證，並在 `test/std/<alias>.no` 釘住。

| # | 位置 | 症狀 | 修法 |
|---|---|---|---|
| A1 | `gzip.deflate-stored` | 空輸入回 **0 位元組** ⇒ zlib/gzip 串流沒有 DEFLATE 主體，任何解壓端都報 `incomplete or truncated stream` | `n == 0` 時輸出單一 BFINAL 空 block `01 00 00 FF FF` |
| A2 | `gzip.huffman.decode` | 「裸塊 + 臨時變數 + match 臂賦值」丟寫 ⇒ `index`/`len` 不前進，deflate（method=8）條目解不出 | 3 處改成兩步解包 `x = x + cnt: { ok -> it; -> x }` |
| A3 | `tar.write-octal` | 填充位元組寫成空格（32）而非 `'0'`；同時踩到同一個丟寫 ⇒ 整個 header 的校驗和欄位全錯 | 重寫：先鋪 `'0'`，再從右往左寫八進位，`?=` 直寫 |
| A4 | `zip.find-eocd` | `! n >= 22 -> return`（恆真，直接返回）＋ 裸塊雙重賦值，兩缺陷疊加 ⇒ **永遠回 -1**，所有 zip 都讀不到中央目錄 | 重寫成「正向掃 `PK\x05\x06`，每命中就覆寫 `off`」⇒ 留下最後一個命中，即真 EOCD（**刻意避開迴圈內減法**：`start - i` 在 `no vet` 是 `?i64`、在 `no test` 是純 i64，套 match 會在單檔模式靜默取 0） |
| A5 | `zip.entry-off` | 裸塊 match 臂賦值丟寫 ⇒ 偏移不前進，回錯條目 | `cur = ct: { ok -> it; -> cur }` |
| A6 | `zstd-compress` | 幀頭寫 `0xA0`（聲稱帶 4-byte FCS）卻只寫 1 位元組 ⇒ 解壓端多吃 3 位元組 | 依 `n <= 255` / `n > 255` 分別寫 `0x20`(+1 byte) / `0x60`(+2 bytes) |
| A7 | `zstd.bits-for-count` | 裸塊丟寫 ⇒ 回傳值恆為初值 | 兩步解包 |
| A8 | `xz` 的 4 個 `update-state-{literal,match,rep,shortrep}` | **裸塊內多個條件臂會依序全部執行**（不是 if/else-if）⇒ 狀態機一次跳好幾格 | 改成 `cond -> { … ; return }` early return |

交叉驗證結果：

| 模組 | 驗證方式 | 結果 |
|---|---|---|
| `tar` | `tar -tvf` / `tar -xOf` | `-rw-r--r-- 0 0 11 …hello.txt`、`drw-r--r-- … subdir/`，內容正確 |
| `zip` | `unzip -t` | `No errors detected in compressed data` |
| `gzip` / `zlib` | CPython `gzip.decompress` / `zlib.decompress` | 含空輸入在內全部可解 |
| `bzip2` | 反向：真實 `bzip2 -c` 的流由本模組解出 | 23 位元組原文正確 |
| `zstd` | `zstd -d -c` | `hello hello hello world` 正確 |
| `xz` | `xz -d -c` | ❌ `Compressed data is corrupt`（見 §5.2 #14） |

另外 `test/runner.no` 的 `summary()` 也修了一處 **測試基建** bug：
`#{overflow=wrap}` 只決定溢位時取什麼值，加法結果仍是 `?i64`；`total = count-passed + count-failed`
直接把 `?i64` 落進 i64 回傳槽，會讓 `summary` 的回傳型別退化成 `?i64`，
於是**每個測試檔尾**的 `total = runner.summary()` 都撞上 `optunuse`
（`no test` / `no vet` 全紅，`no run` 不報，所以很容易誤判成「測試檔寫壞了」）。
改成先收 `total-opt` 再顯式解包。

> A2 / A3 / A5 / A7 是同一個**編譯器根因**（裸塊 + 臨時變數 + match 臂賦值會靜默丟寫，
> 見 §5.2 #15）；A8 是第二個根因（裸塊內多條件臂不互斥，見 §5.2 #16）。
> 本輪只在 std 端繞過，根因仍在。

### 5.1c 本輪修好的 std bug（`src/std/crypto/x25519.no`）

`x25519` 的 RFC 7748 向量一直是綠的，但**域元素的位元組往返是壞的**：
`fe-tobytes(fe-frombytes(x))` 對 `inv(2)`（`f7ff…ff3f`）會回
`f7 ff ff ff 00 … 00 40`，跟輸入差了一整段高位。兩個缺陷疊在一起：

| # | 位置 | 症狀 | 修法 |
|---|---|---|---|
| C1 | `fe-frombytes` 的進位順序 | 載入時是「跨界」的 loose limb（`h[0]` 含 bits 24–31、`h[9]` 含 bits 248–254），原碼照 `h[0] → h[1] → … → h[9]` 順序做進位。`h[9]` 的 wrap-to-`h[0]` 發生在**同一輪的最後**，之後沒有再進位，於是 `h[0]` 停在非規範值（`inv(2)` 得到 `[10, 0…0, -16777216]`）。值在模 p 下「對」，但**形狀**不對，而 `fe-tobytes` 依賴形狀 | 改成 ref10 順序：**先 25-bit 奇數 limb（9,1,3,5,7），再 26-bit 偶數 limb（0,2,4,6,8）**。修完 `inv(2)` 得到 `[-9, 0…0, 2^24]`（半規約，與 ref10 一致） |
| C2 | `fe-tobytes` 的進位鏈 | 中間結果寫進區域 `[]i64` 暫存陣列再讀回，**讀到的仍是原值** ⇒ 整條進位鏈等於沒跑（`t` 從頭到尾就是輸入 limb）。表現為「負 limb 原樣被遮罩進位元組」 | 整支改寫成純量區域變數（`h0..h9`），並照 ref10 做 q 條件約減 + 逐級進位 |
| C3 | `fe-tobytes` 的 floor 除法 | 原碼假設「`>>` 對 i64 是邏輯右移」（檔內註解就這麼寫），用 `-(x >> 63) << (64-b)` 補償成 floor。實測 `>>` 對 i64 是**算術**右移（`-9 >> 26 == -1`，見 §5.2 #22），補償項反而把結果弄壞 | 一律寫成 `((x + 2^40) >> b) - (2^40 >> b)`：先把被除數抬到非負，邏輯/算術右移結果就一致了 |

修完 `test/std/x25519.no` 由 6 條斷言擴到 **87 條**，`crypto/x25519` 從 5/30 到 **30/30**。
三條缺陷各有斷言釘住：`fe-frombytes inv(2) limb0 = -9`（釘 C1 的進位順序）、
往返恆等 `f7ff…ff3f`（釘 C2/C3）、`fe-frombytes p-1 limb0 = -1`。

> C2 與 §5.2 #15（裸塊 + 臨時變數 + match 臂賦值丟寫）**不是同一個根因**：
> C2 是「區域 vec 寫入後讀回拿到舊值」，在最小重現（複製參數 vec → 改寫 → 讀回）裡
> 是**正常**的，只有把整條 200 段的進位鏈串起來才復現。尚未縮到最小。

### 5.2 本輪新挖到、**尚未修**的問題

| # | 症狀 | 復現 |
|---|---|---|
| 9 | `map` 的 `for-each`（str/int/bool 三模板，以及 str-map / str-set / hash-set / tree-map 等所有容器）**沒有可用的用戶態調用語法**：`m.for-each(k, v) { … }` 報 `'k' is not defined`，`k, v = m.for-each()` 也不行 | `test/std/map.no` 檔頭「未測」 |
| 10 | `?i128` / `?u128` 的 **option 載荷**被按 i64 槽讀寫 ⇒ `int.div` / `int.mod` 在 i128/u128 上回垃圾（`10.div(3)` 印 `4349468368`）；同一 binary 上 i64/u64 正常 | `test/std/int.no` 檔頭 |
| 11 | **模組函數名與另一模組別名撞名 ⇒ 函數體內無法調用**：函數體內寫 `fnv.fnv-1a-32(...)` 報 `EmitLLVM: unknown callee fnv-1a-32`（函數名 = 模組別名 `fnv-1a-32`）；同一調用在**頂層**正常 | `test/std/fnv.no`、`test/std/fnv-1a-32.no` 檔頭 |
| 12 | **固定長度陣列的切片若直接接在表達式裡**（`h.slice(0,4).to-hex()`）會退化成未特化的 `[]t.slice` → `unknown callee []t.slice`；先落到局部變數就正常 | `test/std/sha3.no` 註解 |
| 13 | **浮點的 `num.abs` 回 0**（`abs(-3.5)` → `0.0`，`abs(3.5)` → `3.5`）；`0.0 - x` 直接算正常 ⇒ 問題在 `num.abs` 的 f64 單態化裡那句 `r = 0 - .` | `test/std/num.no` 檔頭 |
| 14 | **LZMA 解碼器未完成**：`lzma-decoder.decode` 對真 LZMA1 位元流（Python `lzma` FORMAT_RAW 產生）產出的不是原文；`xz.lzma-decompress` 對合法 `.lzma` 回 0 位元組；`xz-compress` 產出的流缺正確 CRC32（真 `xz -d` 判 corrupt）；真 `.xz`（LZMA2 壓縮塊）解不出 | `test/std/xz.no` 檔頭、`t-lzma-decode` |
| 15 | **編譯器根因：裸塊 + 臨時變數 + match 臂賦值會靜默丟寫**。`{ p-opt = p + 1; p-opt: { ok -> p = it; -> } }` 裡 `p` 不變；同樣形態在 `!!` 循環內、`while` 內、函式內都會踩 | §5.1b A2/A3/A5/A7 |
| 16 | **編譯器根因：裸塊內多個條件臂會依序全部執行**（不是 if/else-if），只有最後成立的臂結果留下 | §5.1b A8 的 `xz` 狀態機 |
| 17 | **`x = <非 option 表達式>: { ok -> it; -> 預設 }` 會靜默走「預設」臂**。最易踩的是資料依賴 `len()` 的減法：`len()` 保證非負 ⇒ `n - 22` 被推成純 i64，套 match 只會拿到預設值 | `zip.find-eocd` 修復過程 |
| 18 | `zlib-decompress` **不驗證 Adler-32**（尾端 4 位元組只當邊界，數值錯照樣回資料）；FDICT 只跳過 4-byte DICTID，不套用字典 | `test/std/zlib.no` 檔頭 |
| 19 | `bzip2-compress` 只寫 24 位元組檔頭，**不產生任何真實區塊** ⇒ 輸出無法被 `bzip2 -d` 解開（反向可用：真實 `bzip2` 的流本模組解得開） | `test/std/bzip2.no` |
| 20 | **真 zstd 壓縮塊（block type 2）解不出**：解壓端只實作了 raw/RLE 與自產的 stored 幀 | `test/std/zstd.no` |
| 21 | **同一段程式在 `no vet`（包上下文）與 `no test`（單檔）對 `x - i` 的型別推論相反**：包上下文判 `?i64`（於是 `off = idx` 報 `15w45dqk`），單檔判純 i64（於是套 `{ ok -> it; -> 0 }` **靜默取 0**）。任何一種寫法都無法同時通過兩者，只能繞開（避免在迴圈裡做減法） | `src/std/archive/zip.no` 的 `find-eocd` 註解 |
| 22 | **`>>` 對 i64 的語意不固定且與註解相反**：`x = -9` 這種字面量來源的 i64 是**算術**右移（`-9 >> 26 == -1`），但從 `arr[i]: { ok -> it; -> 0 }` 取出、或帶 `i64` 註解的值卻是**邏輯**右移（`== 274877906943`）。`src/std/crypto/x25519.no` 的註解因此寫錯了兩年（「`>>` 對 i64 是邏輯右移」），並據此加了 `- ((x >> 63) << (64-b))` 的 floor 補償項 ⇒ 對負數反而算錯。**寫 floor 除法請一律先加 2^40 抬正** | §5.1c C3、探針 `/tmp/x5.no` vs `/tmp/x9.no` |
| 23 | **`i64 op i64` 回傳 `?i64`（檢查式算術），但 `i64 op 字面量` 回傳 `i64`** ⇒ 純量累加式程式碼（進位鏈）每一行都撞 `15w45dqk`，只能寫成 `#{overflow=wrap}` + 兩步解包，或整段包進 i64→i64 的輔助函式。本案的繞法見 `x25519.num-add` / `num-sub` / `num-floor`（同時也是 API 面分母 +3 的原因） | §5.0c 註記、`src/std/crypto/x25519.no` 檔尾 |
| 25 | **match 的各 arm 必須以換行分隔**：寫成單行 `{ ok -> it  -> 0 }` 會被當成**同一條 arm**，報 `match arm never returns a value [fdwcn8i8]`（訊息本身也提示 "match arms must be separated by newlines"）。本輪在探針與 `blake2`/chacha 測試上來回踩了三次 | `test/std/{blake2,chacha20-poly1305}.no` 的取值助手 |
| 26 | **`> i64.MAX` 的整數字面量直接當 `u64` 實參傳入時，位元樣式對、但移位/比較仍按 i64 求值**：`rotr64(0x8000000000000000, 1)` 會走**算術**右移得到 `0xc000000000000000`，`0x8000000000000000 > 0` 也回 `false`。先落到 `u64` 局部變數（`top u64 = 0x8000000000000000`）即恢復正常。**所有 `crypto/argon2.rotr` 與 `blake2.rotr64` 的邊界斷言都必須這樣寫** | `test/std/blake2.no`、`test/std/argon2.no` 的 `w1 u64 = ...` |
| 27 | **`vec` 下標寫越界不會崩，而是把長度自動增長到 `idx+1`**：`out = with-len(11)` 後寫 `out[8..16]`，`out.len()` 變成 16。這是 §5.1d 那個 CBC 殘塊 bug 的「為什麼沒 SIGSEGV」的答案，也意味著**任何「先 with-len(n) 再按塊寫」的迴圈都要自己加對齊守衛** | §5.1d、探針 `tmp/probe/ptdes.no` |
| 28 | **單元測試檔名只允許小寫字母、數字、連字號**：`test/std/_tmp_salsa.no` 直接被拒（`[83xbz15x]`，底線禁用、連字號不可開頭/結尾/連寫） | 本輪抽離 `t-salsa` 單測時 |
| 29 | **`in` 是保留字**：探針裡宣告 `in []byte` 會報 `'in' is not defined [4bek3xc6]`（不是「保留字」的友善訊息）。改用 `buf`。std 內部同名參數是特例 | 探針 `tmp/probe/ptdes.no` |
| 30 | **私有符號（`_` 前綴）無法從測試呼叫**，checker 硬性禁止（`raqvyltn`）⇒ 已從覆蓋率分母剔除（2142 → 2117，共 25 個）。想知道某個 `_` 函式是否被覆蓋，只能靠它被公開函式間接命中 | `scripts/std_test_coverage.py` |
| 31 | **名為 `len` 的結構體欄位無法用屬性讀取**：使用者碼寫 `n.len` 會報 `[gf3kb05g] recv.len property is removed; use recv.len()`。`bigint.len` 正好中招 ⇒ 測試裡只能 `${a,b}.limbs`/`.to-str()` 側面驗證，**無法斷言 limb 數**；`normalize` 的「裁前導零 limb」因此測不到（構造不出髒狀態）。std 內部同一行碼不受此檢查影響 | 本輪 `test/std/bigint.no` |
| 32 | **`?i64` 算術在「賦值給已定型 i64 變數」時是硬錯誤**（`[15w45dqk]`），但**當函式實參、下標、range 邊界時會自動解包**。所以 `x = x + 1` 要寫兩步解包（`y = x + 1` 再 `y: { ok -> x = it  -> _ }`），而 `.slice-bytes(i, i + 1)`、`arr[n - 1 - k]`、`i <- [nl - 1..0]` 都可以直接寫 | 本輪 `src/std/bigint.no` 改寫 `to-hex` |
| 33 | **斷言訊息字串裡不能出現 `'>'`**：`'… 回 '>' 之後位置'` 會被 lexer 當成「字串結束 + 裸 `>` + 新字串」，報 `relational operator on a multi-character string [go7p9knv]`。改成裸 `>` 或改寫措辭 | 本輪 `test/std/dom.no` |
| 34 | **`no run` 單檔編譯約 14 秒**（stdlib + LLVM 全鏈路），`timeout 8/15` 會被誤判成「程式掛死」。查疑似死迴圈時一律 `no build` 後直接跑 `dist/<name>`，別用 `no run` 配短 timeout | 本輪排查 `bigint.to-hex` |
| 35 | **把 `bool` 直接傳給 `?bool` 參數會靜默變成「非 ok」**：`file.copy-to` 回 `bool`，測試助手寫成 `u-b(o ?bool)` 後 `u-b(it.copy-to(p))` 恆回 0，害我一度以為 `copy-to` 壞了。斷言回傳 `bool` 的函式請直接 `runner.is-true(f())`，不要繞 `?bool` | 本輪 `test/std/fs.no` |
| 36 | **`read-dir` 在 `close-dir` 之後呼叫會讓整個進程靜默結束**（rc=0、無任何輸出、後面的 `print` 全不執行）。目錄控制代碼用完即棄，不要在關閉後再讀 | 本輪 `tmp/probe/pfs3.no` |
| 37 | **`no test` 的 stdin 連著終端**：測 `fs.get-line`/`read-stdin`/`read-stdin-str` 會阻塞並吃掉跑測器的輸入 ⇒ 這 3 個 API 刻意不測（要測得 `./bin/no test ... < 檔`，會破壞一鍵門禁） | 本輪 `test/std/fs.no` |
| 38 | **`os.win-find-first-file` 等 4 個 Windows-only 內建在非 Windows 上連編譯都過不去**（`EmitLLVM: unsupported builtin`）⇒ 不能靠「呼叫一下拿個預設回傳值」來湊覆蓋率，只能列為刻意不測 | 本輪 `test/std/os.no` |
| 39 | **用戶碼構造不出 std 內部的結構體**：`yaml.yaml-node{}` 能編譯，但拿到的型別 raw 是 `yaml.yaml-node`，而 `StructFields` 註冊的是裸名 `yaml-node`；`StructKeyOf` 只有「裸名 → 限定名」後備，反向掃不到。後果是**欄位讀到垃圾值、`setfield` 直接報 `[3nktiaae]`**（`setfield: field "kind" not found on "yaml.yaml-node"`），而且完全不報錯地編不過驗不過。同理 `yaml.yaml-parser{...}` 的初始化寫法也失敗 ⇒ 這兩型別的方法在測試裡**無法直呼**（`unknown callee yaml.yaml-parser.eof [4qhyyund]`） | 本輪 `tmp/probe/py4.no`–`py6.no` |
| 40 | **`yaml.line-of()` 回的是「掃描結束行」而非文件寫的「起始行」**：`flag: true` 在第 1 行，`line-of()` 回 2（block 標量掃描時把行尾換行一起吃掉了）；流式文檔 `{a: 1}` 則回 1（不消耗換行）。同一個函式兩種偏移，故測試只斷言流式情形，block 情形標為已知偏差 | 本輪 `tmp/probe/py9.no` |
| 41 | **值位 match 寫成單行 `x: { ok -> it  -> 0 }` 放進 `print(...)` 實參會觸發 `bug #85`（`consumer reads value 0 (NoVal)`）**：`print('f=', it.f64(): { ok -> it  -> 0.0 })` 直接編譯失敗。改成兩步（先 `o = it.f64()`，再 `o: { ok -> v = it  ... }`）即正常 ⇒ 測試裡所有取值都改寫成兩步 | 本輪 `tmp/probe/py7.no`/`py8.no` |
| 42 | **`collection/static-hashmap` 的 31 個方法全部不可達**：`monomorphizeGenericStructs` 只挑 `tmplName := "hashmap-" + cat + "-tmpl"`，`static-hashmap-{str,int,bool}-tmpl` 沒有任何驅動路徑（`map[K]V` 字面量只特化 `hashmap-*`），整個模組是死代碼 ⇒ 覆蓋率分母裡這 31 項永遠填不滿，除非先讓編譯器能特化它 | 本輪 `src/build/generic_structs.go:419` |
| 43 | **`s = x OP y` + `s: { ok -> v = it  -> … }` 在 `cond -> { … }` 區塊裡寫不回去**：`to-str-sci` 的「指數加一」用它就完全沒效果（`9.99@1` 的指數少一），而在函式頂層的同樣寫法正常（探針 `v3` 印 6）。本輪只能繞：把運算搬進私有助手 `_num-add/_num-sub/_num-mul/_num-div`，呼叫端拿普通 `i64` | 本輪 `src/std/number.no` 的 `_num-add` 註解、§5.1j ⑤ |
| 44 | **窄整數宣告配負字面量會丟掉標註型別**：`v i8 = -100` 之後 `v.to-i64()` 報 `unknown callee i64.to-i64`（`i8.to-i64` 明明已註冊），`w i8 = v` 再呼叫也一樣；把 `-100` 改成 `100` 就正常。`no vet` 不報、`no build` 也不報，只有 codegen 撞 `4qhyyund`。繞法：負值經**帶型別參數的助手**傳入（`narrow-i8 = (x i8) (out i64)`），參數入口完全正常（`-100`/`-30000`/`-2e9` 符號擴展正確） | 本輪 `tmp/probe/pnum9.no`（pos 綠 / neg 紅）、`pnum10.no`、`test/std/number.no` 的 `narrow-i8` |
| 45 | **`u64` 沒有 `.to-i64`**：拓寬矩陣只有 `{byte,u8,i8,u16,i16,u32,i32}.to-i64` 與 `{byte,u8,u16,u32}.to-u64`，`u64.to-i64` / `i64.to-u64` 都不存在 ⇒ `f64.to-u64()`、`f64.to-bits()` 的回傳值無法轉成 `i64` 給 `runner.eq`，只能改用 `.to-str()` 比字串 | `src/builtin/strconv.go` 的 `intWidenMethods`、本輪 `t-bits` |
| 46 | **從下標取的元素同樣丟標註型別**：`be = 258.to-bytes-be()` 之後 `b6 byte = be[6]` 再 `.to-i64()` 一樣撞 `i64.to-i64`（顯式 `byte` 標註也救不了）。而先宣告 `be []byte = …` 會讓裸下標變成 `?byte`（`idxbruse`），`?=` 又只能在「有 option 結果參數」的函數裡用 ⇒ 唯一可行寫法是 `byte-of = (b byte) (out i64)` 這種參數助手 | 本輪 `tmp/probe/bits*.no`、`test/std/number.no` 的 `byte-of` |
| 47 | **一元負號前綴 + 方法呼叫（`-2.9.to-i64()`、`-1.2.ceil()`）會 emit `sub void 0, %lv`**，opt 驗證失敗：`void type only allowed for function results`。繞法：先 `neg-f(x)`（`out = 0.0 - x`）成形再調方法 | 本輪 `t-math-f64` 首次編譯失敗 |
| 48 | **Nolang 沒有科學記數字面量**：`1.0e308` 直接被 lexer 拒（`scientific notation literals (e.g. 1e40) are not supported [1gybkrrq]`）。斷言 `+Inf` 只能比大小（`inf > 1000000000000000.0`）或比 `to-str-sci` 的字串 | 本輪 `t-exp-f64-fn` |
| 49 | **`run` 是保留字**：頂層寫 `run()` 呼叫自有函數會報 `expected right parenthesis, got NEWLINE [5qjs01mp]`（連 `no ast` 也一樣）。探針/測試的入口函數別叫 `run` | 本輪 `tmp/pnum/bits.no` |
| 50 | **「未標註型別的呼叫結果」參與運算符重載的聯合臂派發會選錯臂**：`bigint._add = (b big)`（`big = bigint \| i64`）在 `d = f()` 之後寫 `acc + d` 時靜默走 `bigint` 臂，把 i64 位元當 bigint 描述符 ⇒ 算術得 0，有時直接 `abort trap`。同一個值改成字面量、字面量指派、或加 `d i64 =` 標註都正常。**`no vet` / `no build` 都不報** | 本輪 `tmp/probe/pa.no`（abort）、`pc.no`（標註後正確）；修法見 §5.1k ① |
| 51 | **已帶 `err(...)` 的 `?T` 具名變數當實參跨函數傳遞會雙重釋放**：函數內 `v: { ok(x) -> …  -> … }` 讀完後，程式**結束時** `trace/BPT trap` 或 `segfault`（值本身印得對，崩在 exit）。`nil` 與 `ok` 載荷無事，只有 `err` 載荷會踩；把 option 的建立與 match 都收進同一個函數（helper 收 `str` 而不是 `?T`）即完全正常 | 本輪 `tmp/probe/pi8c.no`（err 崩）vs `pi8e.no`（全綠、EXIT=0） |
| 52 | **用「值位 match」寫窄整數助手會把兩臂型別統一成 `byte`**：`out = v: { ok(x) -> x  -> d }`（`x: byte`、`d: i64`）之後哨兵 `-1` 被截成 `255`，越界斷言全失真（`s.byte(3)` 應回 nil 卻顯示 255）。改成語句位 match + `ok` 臂顯式 `.to-i64()` 才對 | 本輪 `t-byte-at` 首版假紅 |
| 53 | **`?i8` / `?u8` 直接傳進 `(v ?i64)` 形參會 bus error**（`?i32`/`?u32` 同理）。每個寬度必須有獨立形參，靠隱式拓寬是不行的 | 本輪 `t-to-narrow` 首版 |
| 54 | **把「值位 match」直接當實參**（`print('x', a: { … })`、`dig(s.byte(pos): { … })`）會撞 `EmitLLVM: … consumer reads value 0 (NoVal) … [zodppsnm]`（bug #85）；先落到區域變數再傳就正常 | 本輪 `tmp/probe/pstr4.no`、`pi8.no` |
| 55 | **`str.reverse` 按「位元組」逆序**（doc 明寫），多字節 UTF-8 會產出非法串：`'中文'.reverse()` → 亂碼。它和同模組的 `count` / `cp-index` / `decode-cp` 的**碼點模型不一致**，用戶很容易誤用。本輪**未改行為**（要改得同時動 9 個 locale 的 doc），只在 `t-reverse` 釘住 ASCII 語義並記在這裡 | `src/std/str.no:1649` 的註解、本輪 `t-reverse` |
| 56 | **`process.cmd` 的超時看門狗排在「阻塞式讀完 stdout」之後** ⇒ 只要子進程持有 stdout，`timeout` 形同虛設：`cmd('sleep',['5'], …, 200, false)` 會等滿 5 秒並回報自然退出碼 0。真修需要非阻塞讀 + `poll`/`select`，而 `src/std/fs.no` 沒有 `poll`/`select`/`fcntl`/`O_NONBLOCK` 任何入口。本輪只修好「`-2` 路徑可達 + 正常退出碼正確」，限制見 §5.1l ④ | `test/std/process.no` 檔頭限制 2 |
| 57 | **`process.write` / `process.close-stdin` 在使用者碼裡恆為 no-op**：`start-with-opts` 只在 `input` 非空時才建 stdin 管道，且寫完立刻 `fs.close`，`.stdin-fd` 永遠是 `-1` ⇒ `write` 恆回 0。屬 API 設計缺口（要支援串流寫入得保留 fd 並定義 EOF 語義），本輪只在測試裡釘住現行契約，未改行為 | `test/std/process.no` 的 `t-write` |
| 58 | **同一段程式「換個檔名 / 換幾條字串字面量」就從 `EmitLLVM: unknown callee i64.to_str` 變成正常編譯**（觸發行是 `w i64 = s.wait()` 之後的 `w.to-str()`）。已排除變數名、三元式、`read-all`、路徑長度等因素：**同內容不同檔名兩次都失敗**（`pp-mini2.no` / `pp-w.no`），而形狀相同、僅字串字面量不同的 `pp-v5-all.no` 一次通過。未定位，疑與 #50 同屬「未標註呼叫結果」的推斷狀態污染 | `tmp/probe/pp-{mini2,w,v5-all}.no` |
| 59 | **`x ?i64 = <call>()` + 三分支 match 之後，同檔其它地方對 i64 用 `.to-str()` 會報 `unknown callee i64.to_str`**（`no vet` 不報，只有 codegen 撞 `4qhyyund`）。`tmp/probe/pj-e.no` 裡把 `ns ?i64 = p.parse-str(…)` 改成不帶標註的 `ns = …` 則改撞 `optunuse`（option 綁定未消費）⇒ 兩條路都堵。可行繞法：**用 `(v ?i64) (out i64)` 助手把三態壓成哨兵值，斷言走 `runner.eq`，全程不經 `.to-str()`**（`test/std/json.no` 的 `i64-of` / `str-of` / `f64-of` / `bool-of` / `json-state` 五個助手即此形） | `tmp/probe/pj-e.no`（紅）vs `pj-f.no` / `test/std/json.no`（綠） |
| 60 | **`json-pool.parse-str` / `parse-num` 不設定節點 `kind`**：低級 API 的分工是「呼叫端自己標 kind」，doc 只寫「`node-idx`: 承接…值的節點索引」。漏標的後果是 `get-str` / `get-num` 一律回 nil（它們先檢查 `kind`），`parse` 本身正常（它在上游先設定）。不是 bug，但**第一次用低級 API 必定踩**，已在 `test/std/json.no` 的 `t-pool-parse-str` / `t-pool-parse-num` 用註解釘住寫法 | `src/std/json.no:584`、`:674` 的 doc vs `get-str` 的 kind 檢查 |
| 61 | **`#{overflow=wrap}` 下的算術賦值得到的是 `?i64`；若操作數未標註，結果會變成 `nil`，而這個 `nil` 再當算術運算元時**靜默回 0**，`no vet` / `no build` 都不報**。方法內實測（`a=0, b=1500`，六變體）：`x = .b - .a` / `x = .b + .a` / `x = .b * 2` / `x = .b - 0` / `x = p - q`（`p`、`q` 由 `.b`、`.a` 未標註推出）⇒ 事後語句位 match **全部走非 `ok` 臂（nil）**；只有先把操作數標成 `i64`（`p i64 = .b`）才拿到 1500。**操作數是形參時正常**（`p * 1000` → `ok`），所以 `time.sleep-ms` 沒事。同一 `x` 拿去 `x / 1000` 不報錯、直接得 0 —— `time.timer.elapsed-s` / `elapsed-us` 就是這樣恆回 0（§5.1o）。繞法：算術就地走值位 match（`x = <expr>: { ok -> it  -> 0 }`），或抽成 `(a i64, b i64) (out i64)` 助手 | `tmp/probe/tm-h.no`（5 條 `15w45dqk` 靜態報錯）、`tm-j.no`（v1–v6）、`tm-i.no`；修法見 §5.1o |

| 24 | **`tests/match-test.no` 已被 `ed7b5236`（「同類測試合併為單一檔案」）刪掉，但 `src/parser/dump/warnings_shape_test.go::TestUnreachableArmHintSurvivesBothShapes` 仍硬編該路徑** ⇒ `make test` 的 `parser/dump` 必紅（`match-test.no did not parse`）。與本次改動無關，屬預存缺陷 | `git log --oneline -- tests/match-test.no` |

> 以上都在 `bin/no`（v0.4.19-40-g72e157cb-dirty，2026-11-10）上複現。
> 注意：該 binary 由**共享工作區**建出，含其它 session 未提交的
> `src/parser/*`、`src/checker/checker.go` 改動；那批改動落地後應重測一遍。

archive 輪全量執行（108 檔）只剩 **1 個紅檔**（`dns.no`、`ecdsa.no` 已轉綠）：

| 檔 | 現象 |
|---|---|
| `multipart.no` | `quoted boundary [len]` 失敗，然後 **segmentation fault** |

（下表是上一輪的紀錄，保留供對照：當時 `dns.no` / `ecdsa.no` 也是紅的。）

| 檔 | 現象 |
|---|---|
| `dns.no` | `parse-name` / `parse-response` 6 條斷言失敗，最後 **segmentation fault**（A 記錄解析後崩） |
| `ecdsa.no` | 5 條失敗：`publickey` 與 OpenSSL 不一致、OpenSSL golden 簽名驗不過、自有 round-trip 驗不過、兩條 ECDH 共享密鑰不符 |
| `multipart.no` | `quoted boundary [len]` 失敗，然後 **segmentation fault** |

### 5.3 歷史快照（2026-09-26，保留供對照）

最後一次全量執行（本輪修復後，`./bin/no`）：**41 檔、906 個斷言通過、14 個失敗**
（6 個檔失敗，**全部是預存問題**，見下節）。

> 本輪修復：`math.div`/`math.mod`（#8）、`bigint` bus error（#7）、`txt.to-f32` 回傳 f64 位元（#5）、
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
| `enum-cross.no` | 2 | 0 | 0 | `regexp.no` | 35 | 0 | 0 |
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
> `number.no`（原 `unknown callee math.div`）與 `bigint.no`（bus error）本輪已修復。

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

### ✅ 已修（本輪 —— `txt.to-f32` / `format` / `math.div` / `bigint` / `[]char`）

| # | 症狀 | 修法 | 迴歸釘 |
|---|---|---|---|
| D | **`[]char` 切片字面量**編不過（global 宣告 `[N x i64]`、元素卻是 `i32` → opt-verify / `unsupported builtin []t.set-byte`） | 編譯器已修（切片字面量降級為正確元素型別） | `test/std/char.no` 的 `t-slice-char-literal`（len / 索引 / 元素賦值 / `[N]char` 固定陣列） |
| E | **`txt.to-f32` 回傳 f64 的位元**（`42.0` 印成 `4631107791820423168`）。`?f32` 裝箱被 `optionPayloadLLVMType` 當成 `i64` 存/讀 | `src/mir/codegen.go` 的 `optionPayloadLLVMType` 把 `f32`/`float` 納入 `double` 分支；`src/mir/hir2mir.go` 兩處 format-field dispatch 補 `f32` | `src/mir/f32_option_payload_test.go`（`TestOptionF32PayloadUsesDoubleType` / `TestOptionF32PeelEmitsDoubleLoad` / `TestFormatFieldF32Lowers`）；`txt.to-f32()` 現印 `3.5` |
| F | **`format` 的 `:t`/`:v`/`,`/`_` 沒實作** | `src/std/fmt.no`：`fmt-int`/`fmt-uint`/`fmt-f64`/`fmt-str`/`fmt-bool` 加 `:t`（型別名）；`fmt-str` 加 `:v`（單引號包裹）；新增 `fmt-group-digits` 並在 `:` 規格的 grouping 分支套用（**必須在零填充之前**） | `print('{n:t}')`→`i64`、`'{s:v}'`→`'abc'`、`'{g:,}'`→`1,234,567`、`'{h:08,d}'`→`0001,234` |
| G | **`bigint` 全模組 bus error**（`from-i64(42).to-str()` 都崩）。根因：編譯器在「方法內對同型別區域變數呼叫方法」時，被呼叫方法體內裸 `.` 解析到**外層方法接收者** ⇒ `to-str` 內 `tmp.is-zero()` 永遠檢查錯誤物件 → 迴圈不會在零值停住 | `src/std/bigint.no` 的 `to-str` 把 `tmp.is-zero()` 改為顯式欄位存取 `tmp.len == 1 && tmp.limbs[0] == 0`（並加 `while` 計數保護替代 `!!` 無限迴圈） | `test/std/bigint.no` 解封 `runner.test` 註冊（**27 斷言全過**：`from-i64`/`zero`/`cmp`/`eq`/`is-zero`/`is-neg`/`add`/`sub`/`mul`/`div-mod`/`mod-i64`/`pow`/`gcd`/`lcm`） |
| H | **`math.div`/`math.mod` → `unknown callee math.div`**（`number.no` 編不過） | `src/std/number.no` 補 `div`/`mod` 實作（`#{overflow=wrap}` 的 `a/b`、`a%b`，向零截斷，與 C/Go/Java 一致） | `test/std/number.no` 現 **37 斷言全過** |

> ⚠️ `:t` 的已知小瑕疵：`{b:t}`（bool）與 `{u:t}`（u64）仍會印 `i64`——因為 bool/u64 被 dispatch 到
> `fmt-int`（回傳 `'i64'`），非本輪修復範圍；其餘 `int`/`f64`/`str`/`bool` 的 `:t`/`:v` 均正確。

### ✅ 已修（本輪 —— option / match 的 #1–#3）

| # | 症狀 | 修法 | 迴歸釘 |
|---|---|---|---|
| 1 | **模組頂層 option 初始化後 match，一個臂都不執行**（靜默，rc 仍 0）。`v ?i64 = 7` 後 `v: {…}` 直接跳過；結果**取決於變數名**（`a`/`w`/`x`/`y`/`res` 正常，其餘絕大多數靜默；大寫＝真全局一律靜默） | `src/mir/hir2mir.go` 的 `lowerGlobalRef` 偵測 option 型別全域（`parseOptionElem`），改走 `foldOptionGlobalConst` emit 真正的 `%option { i64 0, [N x i64] <payload> }` 初值。原本 `foldConstText` 只回裸純量 `"i64 7"`，而 `emitGlobals` 又從 ConstText 首個 token 推型別 ⇒ 全域被 emit 成 `global i64 7`，main 卻以 %option 讀它，`=== ok` 永不成立 | `src/mir/option_match_three_bugs_test.go` 的 `TestOptionGlobalEmitsOptionInitializer`；`tests/option-match-silent-wrong.no` 印 `7` |
| 2 | **`?bool` 的 match 取值恆為 false**。`print(x)` 印 `true`（包裝對），`ok(b) -> print(b)` 印 `0`。`?i64`/`?str`/`?f64`/`?struct`/`?slice` 全正常，只有 bool 壞。連帶 `txt.to-bool` 全滅 | `src/mir/builtin_call.go` 的 `coerce` 補 `i64 → i1`（`trunc i64 … to i1`）。剝殼把槽讀成 i64，coerce 原本沒有 i64→i1，於是把 i64 經 bitcast 直接 store 進 `b` 的 i1 槽，接著 `load i1` 讀到 0 | `TestOptionBoolPeelTruncatesToI1`（IR 須含 `trunc i64 %mvu… to i1`）；`tests/option-match-silent-wrong.no` 印 `1` / `0`；`'true'.to-bool()` 現印 `1` |
| 3 | **option 當函式參數 + match → 編譯失敗**：`opt-verify: '%option' … but expected '%str-long'`（`str_clone` 被套到 option 上，option 載荷槽 24 bytes 剛好＝`%str-long`）。只傳不 match 沒事 | `src/mir/codegen.go` 的 `emitMove`，「借用參數」clone 閘門加 `&& !isOptionType(srcT)`。該閘門原本對 `move str = <參數>` 直接 `str_clone(參數)`；參數是 `?str` 時載入的是**整個** %option ⇒ clone 吃到 option。跳過後改走 option 剝殼分支（先 GEP 進載荷槽、再 clone 載荷） | `TestOptionParamMatchPeelsBeforeClone`（register 精準：同一個暫存器被 `load %option` 又餵給 `@str_clone` 才算 bug）；`tests/option-match-silent-wrong.no` 印 `hello` |

> 三條的 Go 測試都做過「還原成 HEAD ⇒ 紅、帶修復 ⇒ 綠」的雙向驗證。
> 修完 `no vet src/std` 仍 **0 error**、`no fmt -w src/std` 仍 fixed point、
> `no test test/std/` 的失敗集合與控制組**逐行相同**（控制組＝只還原這 3 個檔案重新編出的
> binary，避免把別人未提交的工作算進來）。

### ❌ 未修（待處理）

（目前沒有。）

> #1–#3（本節上方）、A–C（`txt` 三兄弟）、#4–#8（[]char 字面量、`txt.to-f32` 位元、
> `format :t/:v/千分位`、`bigint` bus error、`math.div`）**至此已全部修復**。

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
