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

### 5.0e 快照：2026-11-11 凌晨（`toml` + `dom` 收口輪，**當前**）

| 項目 | 數值 |
|---|---:|
| std 源檔 / 模組別名 | 119 / 119 |
| API 面（函數 + 方法） | 2118（797 + 1321） |
| 已覆蓋 | **1057（49.9%）**（crypto 收口時 990 / 46.8%） |
| 有專用測試檔的模組 | 103 / 119 |
| 測試檔數 | 108 |

本輪補到 100% 的 2 個模組：

| 模組 | 缺口 → 覆蓋 | 測試檔斷言 | 內容 |
|---|---|---:|---|
| `toml` | 16 → 26 / 26 + 方法 3 / 3 | 97 | 條目/表名/`find-eq`/key 正規化/值規則/文檔模型/數字與布林 |
| `dom` | 49 → 全綠 | 313 | 節點池原語、free 助手、裸池解析入口、**15 個單步解析原語** |

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

### 5.1d 本輪修好的 std bug（`src/std/crypto/tdes.no`、`src/std/crypto/aes-cbc.no`）

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
