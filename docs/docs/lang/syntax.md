\---  
sidebar_position: 2  
\---

# 語法


## 註釋

Nolang 支援**單行註釋**標記與**多行（塊）註釋**標記：

- `;` —— 單行標記（註釋到行尾）
- `;; <內容>` —— 單行標記（`;;` 後**同一行**還有內容時，註釋到行尾，語義等同 `;`）
- `;;\n` —— 多行（塊）註釋：`;;` 後**緊跟換行**（僅允許空白）時觸發多行模式，直到遇到同樣後跟換行/EOF 的 `;;` 結束

```no
; 這是註釋
; 這也是註釋，語義相同
;; 這還是單行註釋（;; 後沒有換行）
x = 1; 行內註釋，註釋到行尾
x = 2;; 行內單行註釋，語義相同

;;
這是多行（塊）註釋
可以跨多行
直到遇到單獨的 ';;'
;;

y = 3
;;
結束 ;; 後必須換行或文件尾
才會被視為結束定界符
;;
```

> **多行觸發規則：** `;;` 後**只能有空白**（空格/制表符）直到換行或 EOF，才會進入多行模式。若 `;;` 後在同一行還有任何非空白字符，則視為單行註釋（註釋到行尾）。
>
> **多行結束規則：** 結束的 `;;` 同樣必須後跟換行或 EOF（中間僅允許空白）。未閉合的多行註釋會一直註釋到文件尾。

> **規則：每行一條語句，禁止單行多條語句寫法。**
> 分號 `;` 是**註釋標記**，不是語句分隔符；逗號 `,` 也不能連接多條語句。同一個塊 `{ ... }` 內的每條語句都必須各自獨佔一行。
> 這條規則同樣適用於註釋中的程式碼範例：即使在註釋裡，也不要用 `;` 或 `,` 把多條語句寫在同一行，以免誤導讀者。

```no
#{fmt-skip}
; ❌ 錯誤：用 `;` 分隔多條語句 —— `;` 之後整行都是註釋，
; 連塊的 `}` 也被一起註釋掉，塊無法閉合，後續程式碼被靜默丟棄
; cond -> { print('X'); return }

; ❌ 錯誤：用逗號連接多條語句
; h0 = 1732584193, h1 = 4023233417
; out = from-i64(v), out = from-u64(v)
; debug(msg), info(msg), warn(msg)
```

```no
; ✅ 正確：每條語句獨佔一行
cond -> {
    print('X')
    return
}

; ✅ 正確：每條語句獨佔一行
; h0 = 1732584193
; h1 = 4023233417
; out = from-i64(v)
; out = from-u64(v)
; debug(msg)
; info(msg)
; warn(msg)
```

> **為什麼 `;` 特別危險：** `cond -> { print('X'); return }` 裡 `; return }` 全被當成註釋，實際程式碼只剩 `cond -> { print('X')`。塊永遠不閉合，`}` 之後的所有語句（往往包含該函數剩餘的整個函數體）都會被吞掉，報錯位置也可能嚴重偏移，極難定位。

## 數據類型

基礎類型

- byte
- bool ; 只允許小寫
- char ; 字符類型：單個 Unicode 標量碼點（rune），底層存儲 i32。雙引號包裹單字符，如 "中"
- str ; 字符串類型，單引號包裹 'hello'，或反引號原始字符串 `多行內容`
- txt ; 定長文本類型，底層固定 256 字節（最多 255 字節數據），必須顯式類型標注，如 t txt = 'abc'
- i8
- i16
- i32
- i64 ; 數字默認類型，不區分架構
- i128 ; 128位有符號整數
- u8
- u16
- u32
- u64
- u128 ; 128位無符號整數
- f32
- f64

### char 類型（Unicode 碼點）

`char` 代表**單個 Unicode 純量碼點**（scalar value）。它的底層存儲類型是 **`i32`**，不是 `i64`：

- **合法範圍**：`0 ..= 0x10FFFF`（即 `0 ..= 1114111`）。碼點最大值只需 21 bit，`i32` 完全放得下。
- **越界報錯**：字面量超出該範圍時，編譯期（含 `no vet`）**直接報錯**。例如 `c char = 0x110000` ✗、`c char = 1114112` ✗；`c char = 1114111` ✓。
- **允許數值運算**：`char` **可以直接參與整數運算**，例如 `z = a + 25`、`u = ch - 32`（`ch` 為 `for ch <- s` 的迭代變數）都是**正常寫法**，按碼點值加減。運算一律按 **i32 寬度**進行，結果視為普通整數值；正因如此，`char` 的 `+ - * /` **不像整數家族那樣預設返回 `option<int>`**，不會觸發「未處理整數溢出」的編譯錯誤。要注意的只有範圍：碼點最大 `0x10FFFF`，超出 `i32`/碼點語義的場合（如做偏移量、累加計數）建議先顯式轉成 `i32`/`i64` 再算，讓意圖更清楚。
- **與 `str` 的關係**：`s[i]` 回傳 `char`；`char` 可隱式轉為 `str`（UTF-8 編碼），詳見 [字串 — 隱式轉換](str.md)。

> **「底層類型」與「調用轉換」是兩回事。** `char` 自身就是 `i32`。內置函數 `str_from_cp(cp char) -> ?str` 的**源碼簽名**參數是 `char`；只有在**調用外部 runtime** 時，編譯器才把該 i32 參數**零擴展**（`zext i32 → i64`）以匹配 `@str_from_cp` 的 IR 入參——這只是參數準備階段的臨時轉換，**不改變 `char` 的類型**。類似 C 的 `char c='a'; f((long)c);`：`c` 本身是 1 字節，調用時臨時提升。

容器類型

- obj ; 對象
- map ; 映射
- arr ; 定長數組
- vec ; 變長數組
- slice ; 切片（視圖）沒有獨立數據結構，必須依附於arr/vec/str/txt

- \* ; 指針 僅限 FFI `#{c}` 宣告與標準庫
- usize ; 僅用於ffi 
- any ; 任意類型 僅限標準庫

高級類型

- bigint
- err

## 類型別名與聯合類型

類型別名為現有類型建立一個新名稱。使用等號語法 `name = type`，支援單一類型別名和多類型聯合。

### 語法

```no
; 聯合類型：多個類型用 | 分隔
int = i8 | i16 | i32 | i64 | i128 | u8 | u16 | u32 | u64 | u128
float = f32 | f64
num = int | float
string = str | txt

; 單一類型別名
bytes = []byte
buf = [16]u8
```

> `int`、`float`、`num` 定義在 `std/number`，`string` 定義在 `std/str`——它們都不是編譯器內建關鍵字，而是標準庫用同一套別名語法寫的聯合類型。

### 聯合類型的鏈式引用

聯合類型可以引用其他聯合類型，形成層次結構：

```no
int = i8 | i16 | i32 | i64 | i128 | u8 | u16 | u32 | u64 | u128
float = f32 | f64
num = int | float; num 是 int 和 float 的聯合
string = str | txt; string 直接聯合兩個具體字符串類型
```

### 在函數中使用

聯合類型可用於函數參數和返回值，編譯器會自動進行單態化（monomorphization），為每個成員類型生成獨立的函數版本：

```no
; 參數類型為 num 聯合
max = (a ..num) (r num) {
    r = a[0]
    n = len(a)
    i <- [1..n): {
        a[i] > r -> r = a[i]
    }
}

; 方法定義在聯合類型上
num.sign = () (r num) {
    {
        . > 0 -> r = 1

        . < 0 -> r = -1

        -> r = 0
    }
}
```

`string`（= `str | txt`）同樣可用於參數與返回值：

```no
; str 與 txt 實參都能傳入，各自單態化一份具體函數
take = (s string) (n i64) {
    n = len(s)
}

a str = 'hello'
b txt = 'hi'
n1 = take(a); take__str
n2 = take(b); take__txt
```

> 聯合類型函數體的既有限制（`num` 與 `string` 相同）：函數體內對 union 參數調用成員方法（如 `s.len()`）尚不支持，需使用全域內建（`len(s)`，字節數）或拆成具體類型的函數；聯合類型的變量聲明（`s string = 'x'`）也尚不支持。另注意不要定義與成員方法同名的別名方法（如 `string.len`）——它會接管 `s.len()` 調用並在方法體中遞迴分发回自身。

### 偵測規則

等號語法在以下情況下被識別為類型別名（而非變數賦值）：

- `name = type | type | ...`：聯合類型（含 `|`）
- `name = []type`：切片類型
- `name = [N]type`：陣列類型
- `name = ?type`：可選類型
- `name = known-type`：單一類型別名，其中 `known-type` 是內建類型名（如 `i64`、`f64`、`bool`、`str` 等）或先前已定義的類型別名名稱

## 變量聲明

```no
#{fmt-skip}
; 變量沒有關鍵字
; i64、f64、byte、bool、byte、str可以省略類型標注
i = 1

; f64 中間有.
f = 1.0

; byte
b = 0x00

; i8 如果變量名和類型一致，可以忽略類型標注
i8 = 3

; 顯式標註若與推斷型別相同則冗餘（no vet 報 tcpoxtfd，no fmt --fix=redundant 可自動去除）
x str = ''
x = ''; 等價，標註冗餘

; 默認0值
; 變量定義不需要提前聲明
u16 u16

; str 單引號包裹
name = 'nolang'

; bool true/false 全小寫
flag = true
flag = false

; bool 簡寫：!! 等價 true、! 等價 false（獨立字面量）
flag = true; true
flag = false; false

; 變量賦值
; 不允許同名，如果同名則視為修改變量
name = 'hello'
name = 'world'

; 字符串拼接
greeting = 'hello, ' - name

; 原始字符串（反引號包裹，多行，不轉義）
sql = 'SELECT id,name
FROM user
WHERE id > 100'

; 顯式類型標注
a u64 = 10

; 十六進制字面量類型推斷
; - 十進制整數字面量（如 771）推斷為 i64（默認整數類型）
; - 十六進制字面量（如 0x0303）推斷為 byte（u8）
; - 如果 hex 值超過 byte 範圍（> 255），必須添加顯式類型標注：
PORT i64 = 0x0303; 正確：顯式 i64，值 = 771

; PORT = 0x0303          ; 錯誤：推斷為 byte，值被截斷！
; PORT = 771             ; 正確：十進制默認為 i64
; - 十六進制字面量建議使用小寫字母（0x00ff，而非 0x00FF）
; - 建議：通用整數常量使用十進制；協議/位掩碼常量使用 hex 並添加顯式 i64 類型標注

; 字符（雙引號 = char/rune，單字符）
c = "中"

; byte類型
b = 0x00

; arr 定長數組
arr [3] = [1, 2, 3]

; vec 動態數組（切片）
vec = [4, 5, 6]

; 顯式類型（切片）
typed []u8 = [1, 2, 3]

; 數組
typed [3]u16 = [1, 2, 3]

; 自動推斷長度（i64）
a[nil] = [1, 2, 3]
```
```

## 原始字符串（Raw String）

使用一對反引號 `` ` `` ` ` `` ` `` 聲明原始字符串，類型為 `str`。

### 語法

```
sql = `
SELECT id,name
FROM user
WHERE id > 100
`
```

### 強制格式約束

1. **開頭** `` ` `` 後必須**緊跟源碼換行**（LF；Windows 檔案的 CRLF 同樣接受）；
2. **結尾** `` ` `` 必須**單獨佔一行**，且該行僅允許空白 + 結束反引號；
3. **首尾兩行的反引號所在行不計入字符串內容**，只作為分隔標記；
4. **轉義規則**：內部所有 `\`、`\n`、`\t`、`\'`、`\"` 全部**原樣保留**，不做任何轉義解析；
5. **源碼內的換行、縮進空格完整保留**到字符串字節裡；CR（`\r`）例外——**一律丟棄**，故 CRLF 檔案與 LF 檔案得到逐位元組相同的值；
6. **無法直接嵌入反引號字符**，如需包含 `` ` `` 只能用普通單引號字符串拼接。

### 示例

```
; 內容為 "SELECT id,name\nFROM user\nWHERE id > 100\n"
; 轉義字符 \n \t 原樣保留，不解釋
raw = `
line1\nline2
\ttabbed
`
```

## 正則字面量（Regex Literal）

Nolang 支援 JavaScript 風格的正則字面量語法 `/pattern/flags`，用於創建已編譯的 `regexp` 實例。

### 語法

```no
; 基本正則字面量
re = /\d+/

; 帶旗標（flags）
re = /hello/gi

; 字符類、錨點、量詞
re = /[a-z]+/
re = /^hello.*world$/

; 轉義斜線
re = /a\/b/
```

### 旗標

| 旗標  | 含義        |
| --- | --------- |
| `g` | 全局匹配      |
| `i` | 不區分大小寫    |
| `m` | 多行模式      |
| `s` | `.` 匹配換行符 |

旗標是可選的，跟在結束 `/` 之後，由 ASCII 字母組成。

### 上下文敏感的詞法分析

`/` 既是除法運算符也是正則字面量的起始符，Nolang 採用與 JavaScript 相同的**上下文敏感**詞法分析來區分：

- **表達式起始位置**（語句開頭、`=` / `(` / `[` / `{` / `,` / `:` / `;` 等之後）→ `/` 開始正則字面量
- **值產生位置**（標識符、字面量、`)` / `]` / `}` 等之後）→ `/` 是除法運算符
- `//` 永遠是行註釋（優先級最高）

```no
; 正則字面量（= 後是表達式起始位置）
re = /\d+/
result = match-text(/[a-z]+/, text)

; 除法運算符（標識符後是值產生位置）
ratio = 100 / 4
x = a / b
```

### 脫糖

正則字面量在代碼生成階段脫糖為對標準庫 `regexp-compile` 函數的調用：

```no
; 源碼
re = /\d+/

; 脫糖後等價於
re = regexp-compile('\\d+')
```

`regexp-compile` 定義在 `std/regexp.no`，創建 `regexp` 結構體並調用 `.compile()` 編譯模式。

### 使用示例

```no
; 創建正則並匹配
re = /\d+/
matched = re.matches('hello 123 world')
print(matched); true

; 查找匹配
re = /[a-z]+/g
result = re.find('hello 42 world')
print(result); "hello"

; 作為函數參數
result = match-text(/\d+/, text)
```

> **注意：** 空 pattern `//` 會與行註釋衝突（與 JavaScript 行為一致）。如需空匹配，使用 `/(?:)/`。

## 命名規則

變量名、函數名、結構体名等可以以下劃線開頭，後續可以使用中連接符、字母、數字組成，不能以數字开头，不能以中連接符結尾，不能連續多個中連接符

**大小寫規則（必須遵守）：**

- **全局常量、全局變量**：**必須**使用大寫字母開頭（如 `NOLANG`、`MAX-SIZE`、`HEX-CHARS`）。私有全局變量以下劃線開頭，其後仍需大寫（如 `_NOLANG`、`_PRIVATE-CONST`）。
- **局部變量、函數參數**：使用小寫字母（如 `hex-chars`、`data-len`）。**不要**使用 `_` 前綴——局部變量本身就在作用域內私有，不需要可見性標記。`_` 前綴僅用於私有全局變量和 FFI 私有宣告。
- **函數名、結構體名**：使用小寫字母（如 `sha1-block`、`db-mysql`）

**函數命名慣例（強烈建議）：**

- **不要在函數名前加上模組名前綴**。模組內的函數只需用簡短直觀的名稱，模組前綴由跨模組調用時的 `ShortName.` 自動帶出。例如在 `tail.no` 中定義入口函數 `tail` 而非 `tail-run`，定義輔助函數 `atoi` 而非 `tail-atoi`。這樣程式碼更簡潔，跨模組調用時形如 `tail.tail()` 也更直觀。
- **入口函數**建議直接使用模組名本身（如 `ping.no` → `ping`、`cat.no` → `cat`）。跨模組導入形如 `# /src/tail.tail`。
- **注意避開關鍵字**：`run`（async 關鍵字）、`match`（條件匹配關鍵字）等不能用作函數名。如需對應語義，改用其他名稱。
- **`main` 是保留名，不可用作函數名**：Nolang 的程式入口由**頂層語句自動合成**（見 `hir2mir.synthesizeMainForTopLevel`），因此無需、也**不允許**顯式定義 `main`。一旦出現 `main = (...) { }`——包含方法 `recv.main`、嵌套在其它函數內的 `main`——編譯器直接報錯（`'main' is a reserved name and cannot be used as a function name`）。要作為入口，把陳述直接寫在頂層即可。

> **全局變量必須大寫字母開頭**，這是強制規則，不是慣例。小寫開頭的頂層變量會被編譯器視為局部變量，可能導致未定義引用等錯誤。

```no
; ✅ 正確：全局數據使用大寫字母
NOLANG = 'nolang'
MAX-SIZE = 1024
HEX-CHARS = '0123456789abcdef'

; ✅ 私有全局變量：下劃線開頭，其後仍需大寫
_NOLANG = 'nolang'
_PRIVATE-CONST = 42
```

```no
#{fmt-skip}
; ❌ 錯誤：全局變量不能使用小寫字母開頭
; x1 = 10
; x = 10
; foo-bar = 42
; hello-world = 'Hello World'
```
```

```no
#{fmt-skip}
; ✅ 局部變量（函數內）使用小寫字母，不加 _ 前綴
; fn-example = () {
;     x1 = 10
;     x = 10
;     foo-bar = 42
;     hello-world = 'Hello World'
;     ; ❌ 錯誤：局部變量不需要 _ 前綴
;     ; _x = 10
; }
```
```

### 模組中避免使用全局變量

**強烈建議：如非必要，不要在模組（`.no` 檔案）中使用全局變量。** 全局變量會帶來以下問題：

- **編譯器 bug 風險**：Nolang 編譯器對全局結構體變量的跨函式記憶體位址處理存在已知限制，不同函式可能看到不同的位址，導致狀態不一致。
- **並發安全**：全局可變狀態在 fork 或異步場景下難以追蹤，容易引發競態條件。
- **可測試性**：全局狀態使函式產生隱式依賴，難以隔離測試。
- **程式碼可讀性**：全局變量使資料流隱晦，讀者需追蹤整個模組才能理解函式行為。

**推薦做法：**

1. **優先使用局部變量**：將狀態保留在函式內的局部變量中，透過參數和返回值傳遞。
2. **使用結構體封裝狀態**：將相關狀態組織到結構體中，透過方法操作（方法接收者為局部變量，位址一致）。
3. **僅在必要時使用全局變量**：如模組級常量（不可變）、單例資源（如全域日誌緩衝區）等場景。
4. **全局變量必須大寫**：這是強制規則（見上文），小寫開頭的頂層變量會被編譯器視為局部變量。

```no
#{fmt-skip}
; ❌ 避免：在模組中使用全局可變變量
; g-conn = tls.conn {}
; g-buf = ' '
;
; fn-a = () {
;     g-conn.send(g-buf)   ; 全局變量跨函式可能位址不一致
; }
```
```

```no
; ✅ 推薦：使用局部變量，狀態透過參數/返回值傳遞
fn-a = () {
    conn = tls.conn {}; 局部變量，位址一致
    buf = ' '
    conn.send(buf)
}

; ✅ 推薦：將完整流程封裝在單一函式中，避免狀態跨函式傳遞
serve-once = (listen-fd fd, body str) (ok bool) {
    ok = false
    client-fd = net.net-accept(listen-fd)
    conn = tls.server-init(client-fd); 局部變量
    conn: {
        ok -> {
            c = it
            c.handshake()
            c.send(body)
            c.close()
            ok = true
        }

        -> fs.close(client-fd)
    }
}
```

> **實踐案例**：`std/net/tls.no` 的 `https-serve-once` 函式將完整的 HTTPS 請求-回應週期（accept + handshake + recv + send + close）封裝在單一函式中，所有 TLS 狀態保留在局部變量中，成功避開了全局變量跨函式位址不一致的編譯器 bug。

## API 文檔規範

函數的文檔註釋應包含完整的參數名與型別、返回參數名與型別。

**規則：**

- 函數定義上方的文檔註釋，必須列出每個參數的名稱和型別，以及返回參數的名稱和型別
- 模組頂部的 API 摘要也應使用完整簽名（含參數名、型別、返回名、型別），不要使用省略寫法

```no
#{fmt-skip}
; ❌ 錯誤：缺少型別，缺少返回參數名
; sha1(data) (hash)
; sha1-block(s, h0..h4)
```
```

```no
; ✅ 正確：包含參數名、型別、返回參數名、型別
; sha1(data []byte) (hash [20]byte) — 完整雜湊
; sha1-block(s []u32, h0 u32, h1 u32, h2 u32, h3 u32, h4 u32) — 處理單一區塊

; 函數定義上方的文檔註釋也應遵守同樣規範：
; sha1: 計算 SHA-1 雜湊
; data []byte: 輸入位元組陣列
; 返回 hash [20]byte: 20 位元組雜湊值
sha1 = (data []byte) (hash [20]byte) {
    ...
}
```

### 可翻譯的文檔註解（Markdown 連結）

文檔註解可以用一條**獨立的 Markdown 連結**收尾，把整段註解指向翻譯目錄中的一個條目：

```no
; get: 獲取第 i 個參數
; 委派至 os.arg(i) builtin（讀取 main 的 argv[i]）；越界回傳 nil
; [get](#args.get)
get = (i i64) (val ?str) {
    val = os.arg(i)
}
```

**規則：**

- 連結必須是註解群組的**最後一行**，且獨佔一行；它上方的所有行構成一個整體，作為一條訊息整段翻譯
- 目標必須是 `#` 開頭的錨點（同一份目錄內的條目）。`[說明](https://example.com)` 這類外鏈是寫給人看的普通連結，**不會**被當成翻譯標記
- 錨點即鍵（`args.get`）。它由作者指定、譯者在目錄裡看到，不從文字派生——所以改寫措辭不會讓既有譯文失效。錨點允許字母、數字、`.`、`_`、`-`，並且是全域的，建議加上模組前綴
- 前綴用**打平後的模組名**，不是目錄名。編譯器把 std 檔案打平成它的檔名：`std/collection/linked-hash-map.no` 的模組就是 `linked-hash-map`，`use std/collection` 也不會給符號加 `collection.` 前綴。所以 `linked-hash-map.clear` 的錨點是 `linked-hash-map.linked-hash-map.clear`（模組名 + 原樣的名字），**不是** `collection.linked-hash-map.linked-hash-map.clear`。`collection`／`net`／`crypto`／`archive`／`encoding`／`database` 這些目錄段不屬於任何符號，不該出現在錨點裡
- 方括號裡的字（`[get]`）只是給讀原始碼的人看的提示，顯示時整行都會被剝掉；`[](#args.get)` 也合法
- 只有 `;` 單行註解可以帶連結（`;;` 與 `;;block` 不算）

譯文放在 `locales/<語言>-doc.noml`，以錨點為鍵。缺少譯文時回退到註解原文，不會變成空白。檔名遵循 `.no` 的命名規則（小寫字母、數字、單一中劃線），標籤本身就是這個形式：`--lang=zh-tw` 讀的是 `zh-tw-doc.noml`。

標準庫裡每個有註解的函數與方法都已帶上錨點（約 2000 條），所以任何一條譯文一落地，LSP 立刻就能用。`make i18n` 只把**已翻譯**的條目寫進目錄，不會把原文當成譯文塞進去——否則英文目錄裡又會出現中文。`go run ./i18n/cmd/genen -doc-missing` 會列出還沒翻譯的錨點。

**LSP 提示：** 滑鼠懸停在函數名上會顯示翻譯後的整段說明；懸停在連結那一行上，則顯示錨點本身指向的內容（同樣是當前語言），方便就地核對譯文。

## 優先使用標準庫

Nolang 標準庫提供了豐富的常用功能，包括字串操作、位元組轉換、雜湊計算、網路通訊等。

**規則：如果標準庫中已有對應功能，不建議自行重新實現。** 開發者應仔細查看標準庫文檔（`docs/docs/std/overview.md`），避免重複造輪子。

```no
#{fmt-skip}
; ❌ 錯誤：自行實現 str → []byte 轉換
str-to-bytes = (s str) (out []byte) {
    n = s.len-bytes()
    i = 0
    (i < n) {
        out[i] = s[i]
        i = i + 1
    }
}
```
```

```no
; ✅ 正確：使用標準庫 str.to-bytes() 方法
data []byte = s.to-bytes()
```

常見的標準庫替代：

- `str.to-bytes()` — 字串轉位元組陣列（替代手寫 `str-to-bytes`）
- `[]byte.to-str()` — 位元組陣列轉字串（替代手寫 `bytes-to-str`）
- `[n]t.to-vec()` — 定長陣列轉切片（`[20]byte` → `[]byte`）
- `[]byte.to-hex()` / `[]byte.to-hex-lower()` — 位元組陣列轉十六進制字串
- `str.to-i64()` / `str.to-f64()` — 字串解析為數字
- `int.to-str()` / `float.to-str()` — 數字轉字串
- `std/crypto/sha1`、`std/crypto/sha256`、`std/crypto/sha512` — 雜湊計算

### TOML 設定檔（`toml`）

標準庫 `toml` 提供 TOML 1.0 設定檔的解析與序列化。解析器支援註解、bare/quoted/dotted keys、`[table]`、`[[array-of-tables]]`、basic/literal/multiline strings、整數（十進位/十六進位/八進位/二進位）、浮點數、布林值、日期時間、陣列與 inline table。值以 TOML 原始字面值保存，因此不會遺失陣列、日期時間等資訊。

```no
cfg = toml.parse('title = "Nolang"\n[server]\nport = 8080\n[[server.backends]]\nname = "local"')

doc = toml.new()
doc.put('name', '"nolang"')
doc.put('server.port', '8080')
doc.put('enabled', 'true')
text = toml.stringify(doc)
```

常用 API：

- `toml.new()` — 建立空的 `toml`
- `toml.parse(text)` — 解析 TOML，回傳 `?toml`
- `doc.put(key, raw-value)` — 新增或更新原始 TOML 值；會拒絕非法值、重複鍵由解析器報錯
- `toml.get(doc, key)` — 取得原始值（`?str`）
- `toml.get-str(doc, key)`、`toml.get-i64(doc, key)`、`toml.get-f64(doc, key)`、`toml.get-bool(doc, key)` — 取得已轉換的 scalar
- `toml.stringify(doc)` — 輸出 TOML；array-of-tables 會輸出 `[[...]]` 標頭

目前文件模型限制單一文件最多 4 個 key、2 個普通表格與 2 個 array-of-tables；單一 key 最長 32 bytes、單一 value 最長 128 bytes，超出時應分割設定檔或由呼叫方處理。

### YAML 文件（`yaml`）

標準庫 `yaml` 解析與產生 YAML 1.2 core schema 文件，支援塊映射、塊序列、流式集合、三種引號標量、塊標量 `|` / `>`、註解、多文檔、錨點與別名、合併鍵 `<<`、顯式鍵，以及型別推斷（null / bool / int / float / `.inf` / `.nan`）。

```no
doc ?yaml = yaml.parse('name: Alice\nserver:\n  port: 8080\ntags:\n  - a\n  - b\n')
doc: {
    nil -> print('nil')

    err -> print(it.msg())

    -> {
        doc = it
        name = doc.find-str('name'); ?str
        port = doc.find-i64('server.port'); ?i64
        name: {
            ok -> print(it)

            -> print('')
        }
        print(doc.to-json())
    }
}
```

常用 API：

- `yaml.parse(text)` / `yaml.parse-all(text)` — 解析，回傳 `?yaml`
- `yaml.valid-of(text)` / `yaml.error-of(text)` — 只做校驗
- `it.find(path)` / `it.get(key)` — 取子節點，回傳 `?yaml`
- `it.find-str(path)`、`find-i64`、`find-f64`、`find-bool` — 便捷取值，回傳 `?值`
- `it.to-json()` / `it.to-yaml()` — 序列化

> 注意：子節點以 `?yaml`、取值協助函式以 `?值` 回傳，用 match 解包（`child = it.find(path)`，其 `ok` 臂以 `it` 綁定子節點、預設臂處理空值，每臂獨立一行）。

## 檔案命名

`.no` 檔名（含文件夾名）一律使用中連字符 `-` 連接單詞，**不使用下劃線 `_`**。這與變量名、函數名、結構體名等 Nolang 標識符的命名風格保持一致。

編譯器在 `run` / `build` / `test` / `vet` / `fmt` / `ast` 命令中**強制檢查**此規範，路徑中任何一段（資料夾名或檔名主體，`.no` 副檔名不計入）違規都會**直接報錯**：

- 允許的字元：小寫字母 `a-z`、數字 `0-9`、中連字符 `-`。
- 禁止的字元：下劃線 `_`、大寫字母、以及其他特殊符號。
- 中連字符不得以段首／段尾出現，且不得連續（`--`）。

例如 `tmp/zz_stridx/r_index.no` 因 `zz_stridx` 與 `r_index` 含下劃線而被拒；改為 `tmp/zz-stridx/r-index.no` 即合規。絕對 OS 路徑（如 `/Users/me/proj/a.no`）只驗證落在目前工作目錄下的部分，宿主目錄名（如 `Users`）不會誤報。

```shell
✅ 推薦：
utils/
├── string-helper.no
├── hash-table.no
└── http-client.no
```

```shell
❌ 避免：
utils/
├── string_helper.no
├── hash_table.no
└── http_client.no
```

## 函數定義

函數可以透過**具名結果參數（named result parameters）**&#x8F38;出結果，本質是  
**輸出參數（out-parameter）的語法糖**；`return`（及其簡寫 `***`，暫不推薦使用）僅用於提前終止，不能跟結果。

Nolang 的函數定義形式為 `name = (in-params) (out-params) { body }`：第二個  
括號組 `(out-params)` 宣告具名結果參數（語法糖）。函數體內對它們賦值  
（與普通入參一樣皆為引用型別）；**真正接收結果的變數必須在調用處定義**——  
定義裡的結果參數名只是佔位符，調用方的 LHS（或尾隨引數）決定結果落在哪個  
變數：

```no
parse-line = (s str, max-fields i64 = 1024) (fields []str) {
    ...; 函數體內對 fields 賦值
}

; 調用處定義接收變數（LHS 綁定，型別由簽名推斷）
fields = parse-line(line)

; 多結果按順序綁定
a, b = swap(x, y)

; 或尾隨引數形式：res 作為額外的輸出引數傳入
add1(5, 3, res)
```

Nolang 的函數有以下特點：

- **具名結果參數是語法糖**：底層仍是透過入參（out-parameter）完成，不會產生  
  新的返回值物件，因此內部是安全的。
- **輸入參數唯讀**：標量傳值，複合類型傳唯讀引用；禁止在函數體內寫入輸入參數及其子欄位。
- **輸出參數可寫**：調用方可將已有變數綁定到輸出槽，函數直接在該記憶體修改；也可不綁定，由函數生成新值交付。
- 函數內的變量在函數退出時自動銷毀。
- 調用處必須提供接收變數（LHS 或尾隨引數）；「函數沒有返回值」的說法不準確——  
  準確說法是：結果透過具名結果參數（out-parameter 的語法糖）傳遞，且由調用處綁定。
- **別名規則**：允許輸入唯讀引用與輸出槽指向同一物件。只要寫操作僅發生在輸出區、輸入僅讀取，即為合法；編譯器不做靜態別名檢查。

### 輸出槽直接綁回既有變數（避免中繼變數）

呼叫端的輸出槽可以**直接綁到已存在的變數**，函式會就地改寫它的記憶體（見上方「輸出參數可寫」）。
因此當某個輸出只是要接續更新既有狀態時，不必先接到一次性中繼變數、再寫回：

```no
; ✅ 推薦：餘數直接寫回 r（r 同時是方法的接收者）
qsq, r = r.div-mod(modulus)
```

```no
; ❌ 避免：msq 只被用一次，純粹是轉存
qsq, msq = r.div-mod(modulus)
r = msq
```

- **接收者可同時是輸出目標**：`r.div-mod(modulus)` 的接收者 `r` 是唯讀輸入（隱式 `self`），
  `r` 又綁到第二個輸出槽 `r bigint`；依〈別名規則〉，輸入只讀、輸出只寫即合法。
  實測（`no build` / `no vet` / `no fmt`）`q, a = a.div-mod(bigint.from-i64(7))` 在
  `a = bigint.from-i64(100)` 之後得到 `q = 14`、`a = 2`，與「先接 `msq` 再 `r = msq`」完全一致；
  即便 `r` 是外層函式的結果參數也一樣成立（`src/std/bigint.no` 的 `mod-pow` 正是這種形狀）。
- **用不到的輸出用 `_` 丟棄**：若商根本沒用到，`_, r = r.div-mod(modulus)` 連一個新變數都不必引入，
  是最精簡的寫法（見後面〈多重賦值〉的 `_` 佔位）。
- **單一輸出同樣成立**：`r = r._mul(r)`、`s = s.trim()`、`out = out.replace-str(...)` 都是 stdlib 既有的就地更新。
- **既有合法程式碼不必為此改寫**：這是新寫程式碼的推薦寫法，不是重構令；遵循〈黃金法則：不要修改合法程式碼〉，
  不要為了風格去動已經正確的程式。

### 參數默認值

函數參數可以使用 `name type = expr` 語法指定默認值。帶有默認值的參數在調用時可以省略，編譯器會自動填充默認值。帶默認值的參數必須放在參數列表末尾。

```no
; 帶默認值的函數定義
parse-line = (s str, max-fields i64 = 1024) (fields []str) {
    ...
}

; 以下兩種調用方式都合法：
fields = csv.parse-line(line); max-fields 默認為 1024
fields = csv.parse-line(line, 256); max-fields = 256
```

```no
add = (a i64, b i64) (result i64) {
    result = a + b; 通過參數返回結果
    ...; 佔位陳述：不做任何事，build 時整條丟棄（見〈佔位陳述〉）
}

; 可變參數
add3 = (a ..i64) {
}

; 函數調用
sum = add(1, 2); sum == 3

; 匿名函數 和for？ 有傳參？
(a i64) {
    print(a)
}(10)

; 函数调用
add(a, b)

; 也可能有多个返回值
a, b = swap(5, 3)
```

## 流程控制

> **Deprecated 語法（n 版本後移除）**：`for { }` / `for cond { }` / `for init, cond, update { }` / `for i in [..) { }` / `match x { }` / `if/elif/else { }` 仍可解析但會輸出 deprecation warning。請改用下方「**新式語法**」。

### 新舊對照

| 舊式 / 後綴式（suffix）           | 前置式（prefix，format 預設）                   |
| -------------------------- | --------------------------------------- |
| `{ } (true)` 無限循環          | `!! { }` 或 `true { }`                   |
| `{ } (cond)` 條件循環          | `(cond) { }`（空 `()` 視為 false，不執行）       |
| `{ } ()` 不執行               | `! { }` 或 `false { }` 或 `() { }`        |
| `{ } * n` 常數計數             | `n * { }`（N ≤ 0 時循環體不執行）                |
| `for i=0, i<n, i++ { }` 計數 | `n * { }`（常數計數）或 `i <- [0..n): { }`（變數） |
| `for i <- [a..b] { }` 範圍   | `i <- [a..b]: { }`                      |
| `for i in [a..b) { }` 範圍   | `i <- [a..b): { }`                      |
| `match x { ... }` 匹配       | `x: { ... }`                            |
| `if/elif/else { }` 分支      | `{ cond -> body }`（短路，必須用 `{}` 包裹）      |
| `continue`                 | `*`（簡寫暫不推薦）                        |
| `break`                    | `**`（簡寫暫不推薦）                       |
| `return`                   | `***`（簡寫暫不推薦）                      |

### Loop / While / for-in

> **兩種等價寫法**：同一個循環支援「前置式 `(cond) { }`」與「後綴式 `{ } (cond)`」，  
> 語意完全相同，只是條件與主體的先後順序不同。`no fmt` 預設輸出**前置式**；  
> 可用 `no fmt -loop-style=suffix` 切回後綴式（兩種寫法都保證格式化冪等）。

```no
#{fmt-skip}
; === 前置式（prefix，條件寫在主體前面）===

; 無限循環（條件恆為 true）—— 以下兩種等價
!! {
    ...
}
!! {
    ...
}

; 不執行（條件恆為 false）—— 以下三種等價
! {
    ...
}
! {
    ...
}
! {
    ...
}

; 條件循環（檢查 cond，為真時執行主體）
(x == 1) {
    do-something()
}

; 限定執行次數（前置式計數）
10 * {
    do-something()
}

; N ≤ 0 時循環體不執行（0 次或負數次均跳過）
0 * {
    print('不會執行')
}
-3 * {
    print('也不會執行')
}

; 可加標籤（label 寫在條件/計數之前）
#1 (x == 1) {
    do-something()
}

; === 後綴式（suffix，條件寫在主體後面，舊式）===
!! {
    ...
}

; 不執行（空括號代表 false）
! {
    ...
}

; 條件循環
(x == 1) {
    do-something()
}

; 限定執行次數
10 * {
}

; N ≤ 0 時循環體不執行（0 次或負數次均跳過）
0 * {
    print('不會執行')
}

-3 * {
    print('也不會執行')
}

; 區間語法（已支持 i64/u8 等數值類型、arr、vec、str）
; 共 16 種組合：'[' 包含左端點，'(' 不包含；']' 包含右端點，')' 不包含。
; 有界（4 種）：兩端都有值
i <- [a..b]: {
}
i <- (a..b]: {
}
i <- [a..b): {
}
i <- (a..b): {
}

; 無上限（4 種）：省略右端點，迭代至類型最大值
i <- [a..]: {
}
i <- [a..): {
}
i <- (a..]: {
}
i <- (a..): {
}

; 無下限（4 種）：省略左端點，從類型最小值開始迭代
i <- [..b]: {
}
i <- [..b): {
}
i <- (..b]: {
}
i <- (..b): {
}

; 完全無界（4 種）：兩端都省略，基於類型最小值/最大值
i <- [..]: {
}
i <- [..): {
}
i <- (..]: {
}
i <- (..): {
}
i <- [5..0]: {
}
i <- abc: {
}

; 运行时方向检测：当 start > end 时，迭代自动递减（步长 -1）。
; 四种括号组合均支持递减：
;   [5..1]  → 5 4 3 2 1   左闭右闭，递减
;   (5..1]  → 4 3 2 1     左开右闭，递减
;   [5..1)  → 5 4 3 2     左闭右开，递减
;   (5..1)  → 4 3 2       左开右开，递减
;   (3..0]  → 2 1 0       左开右闭，递减到 0
; 当 start <= end 时，按递增方向迭代（步长 +1）。
;
; 無界區間的端點由變量的靜態類型決定：
;   i64 的 [..]  從 i64.MIN (-9223372036854775808) 到 i64.MAX (9223372036854775807)
;   u8  的 [..]  從 0 到 255
;   i64 的 [..)  從 i64.MIN 到 i64.MAX - 1（不包含最大值）
;   i64 的 (..]  從 i64.MIN + 1 到 i64.MAX（不包含最小值）
;   i64 的 (..)  從 i64.MIN + 1 到 i64.MAX - 1（兩端都不包含）
; 規律：'[' 包含類型最小值，'(' 從最小值+1 開始；
;       ']' 包含類型最大值，')' 到最大值-1 結束。

; ❌ 明確拒絕
;   區間邊界必須是整數；不支持嵌套表達式
;   for i <- [1.5..5.5] { }   ; 編譯錯誤
;   for i <- [0..[1..5][0]] { } ; 語法錯誤

; ⚠️ 避免 ... 歧義
;   範圍運算子是 ..（兩點），self 方法調用是 .len()。
;   連寫時 [0.. .len()) → [0...len())，三個點看起來像單一運算子
;   （而 return 簡寫已定為 ***，暫不推薦使用；但三點連寫仍干擾辨讀）。
;   應使用 self.len() 代替 .len() 消歧：i <- [0..self.len()): { }
;   （方法體內 self 與 . 語義等價）

; 條件循環（新式：前置式 (cond) { }，`no fmt` 預設；取代舊式 for cond { }）
; 後綴式 { } (cond) 與之完全等價（舊式寫法）
; 大多數情況可改用 range-for：i <- [0..n): { }
(x == 1) {
    do-something()
}
```
```

### 跳出 / 跳過 / 提前返回

> ⚠️ **`*`（跳過本輪）/ `**`（跳出循環）/ `***`（返回終止）這組簡寫暫不推薦使用**——請書寫
> 英文關鍵字 `continue` / `break` / `return`；`no fmt` 會把已支援的簡寫一律規範回關鍵字形式。

```no
i <- [0..10): {

    ; continue（跳過本輪，暫不推薦）
    continue

    ; break（跳出循環，暫不推薦）
    break

    ; return/terminate（提前返回，僅終止函數；規劃中，暫不推薦）
    break
    continue
}

; 推薦使用英文關鍵字（與 C/Rust 一致，便於移植、且能正常編譯執行）：
i <- [0..10): {

    ; 等價 *（暫不推薦）
    continue

    ; 等價 **（暫不推薦）
    break

    ; 等價 ***（暫不推薦），僅提前終止函數
    return
}

; 關鍵字別名（lexer 完整支援，與原關鍵字完全等價）：
;   ctn ≡ continue、brk ≡ break、rtn ≡ return；`no fmt` 保留別名原拼寫
i <- [0..10): {

    ; 等價 continue
    ctn

    ; 等價 break
    brk
}

; 等價 return，僅提前終止函數
rtn
```

> ⚠️ **`return`（及其簡寫形式 `***`，簡寫暫不推薦使用）只能裸用，不能帶返回值。**  
> Nolang 函數沒有「返回值」機制——結果一律通過**具名結果參數（out-param）**&#x5728;函數體內賦值傳出（見「函數定義」）。  
> 因此 `return <值>`、`return(expr)`、`*** <值>` 都**被禁止**，編譯器 / formatter / LSP 都會報錯：
>
> - 編譯器（`no build`）：`Error: compilation error: parser errors: line L, column C: 'return' 後不能跟返回值；… [E_GENERAL]`
> - `no vet`：`<file>:L:C: [ERROR] nolang-compile: 'return' 後不能跟返回值；… [E_GENERAL]`（與其他診斷同一格式，每條一行）
> - `no fmt`：`format error: …` 並以非零狀態退出
> - LSP：在對應行顯示紅色錯誤診斷
>
> 錯誤寫法：
>
> ```no
> has = (n i64) (r i64) {
>     n == 0 -> return 0        ; ❌ 編譯 / formatter / LSP 均報錯
>     r = n
> }
> ```
>
> 正確寫法（先給結果參數賦值，再裸 `return` 提前終止；臂體裡的多條語句只能各佔一行）：
>
> ```no
> has = (n i64) (r i64) {
>     n == 0 -> {
>         r = 0
>         return
>     }
>     r = n
> }
> ```
>
> ⚠️ 壓成一行寫 `n == 0 -> { r = 0; return }` 不是簡寫：`; return }` 整段是註釋，
> 塊因此永不閉合，後續所有語句（常是整個函數體的剩餘部分）都被靜默吞進這個臂裡 ——
> 實測 `no vet` 不報錯，函數從未被呼叫。

## 佔位陳述（`...`）

`...` 是語言的一條**佔位陳述**：語法上合法、語義上**什麼都不做**。主要用於文檔演示、
代碼骨架與 `TODO` 預留位置。編譯時整條陳述在 AST→HIR 階段即被丟棄，產物與「從沒寫過這行」
完全一致（實測同一份源碼加減 `...` 編譯出的二進位 `sha256` 相同）。

```no
parse-line = (s str) (fields []str) {

    ; TODO: 這裡還要處理跳脫字元
    ...

    fields = csv.parse-line(s)
}

add = (a i64, b i64) (result i64) {
    result = a + b
    ...
}
```

規則：

- **只能獨佔一行**（行尾可跟 `;` 註釋）。`fail -> ...`、`x = ...`、`[0...n)` 這類寫法
  一律報語法錯誤——`...` 沒有值，不能出現在表達式位置或陳述的其餘部分。
- **不是 `return` 的別名**，也**不是範圍運算子**：範圍是 `..`（兩點），提前返回寫 `return`。
- `no fmt` 原樣保留，既不刪除也不挪位。

以下寫法全部不合法：

```no
#{fmt-skip}
add = (a i64, b i64) (result i64) {
    result = a + b
    ...          ; ✅ 唯一合法形式：獨佔一行
}

ok -> ...        ; ❌ 語法錯誤：`...` 不能出現在表達式位置
x = ...          ; ❌ 語法錯誤：同上
i <- [0...n): {  ; ❌ 語法錯誤：`...` 不是範圍運算子，應寫 [0..self.n)
}
```

### Match

```no
; 簡單寫法，it 用於取參數
x: {
    err -> log(it.msg()); it 是結構化錯誤 err.err

    nil -> log('nil')

    -> do-right-thing(it)
}

; 析構寫法
x: {
    err(e) -> log(e.msg())

    nil -> log('nil')

    ok(v) -> do-right-thing(v)
}

; 註：struct 模式匹配（`user: { User{id=1} -> ... }`）與欄位綁定尚未支援，故不示範

score: {
    [0..59] -> print('不及格')

    [60..89] -> print('良好')

    [90..100] -> print('優秀')

    -> print('分數非法')
}

; 區間語法共 16 種組合，規律：'[' 表示包含左端點，'(' 表示不包含；
;                     ']' 表示包含右端點，')' 表示不包含。
;
; 【有界區間】（4 種）— 兩端都有具體值
;   [a..b]  → x >= a && x <= b   兩端都包含
;   [a..b)  → x >= a && x <  b   左包含右不包含
;   (a..b]  → x >  a && x <= b   左不包含右包含
;   (a..b)  → x >  a && x <  b   兩端都不包含
;
; 【無上限區間】（4 種）— 省略右端點（End=nil），僅檢查下界
;   [a..)   → x >= a              左包含，無上限
;   [a..]   → x >= a              左包含，無上限
;   (a..)   → x >  a              左不包含，無上限
;   (a..]   → x >  a              左不包含，無上限
;
; 【無下限區間】（4 種）— 省略左端點（Start=nil），僅檢查上界
;   [..b]   → x <= b              無下限，右包含
;   [..b)   → x <  b              無下限，右不包含
;   (..b]   → x <= b              無下限，右包含
;   (..b)   → x <  b              無下限，右不包含
;
; 【完全無界區間】（4 種）— 兩端都省略，基於類型最小值/最大值
;   [..]    → true                類型最小值 ≤ x ≤ 類型最大值（兩端都包含）
;   [..)    → true                類型最小值 ≤ x < 類型最大值
;   (..]    → true                類型最小值 < x ≤ 類型最大值
;   (..)    → true                類型最小值 < x < 類型最大值
;
; 在 match 中，完全無界區間基於被匹配變量的類型值域（必然為真，等同 catch-all）。
; 在 range-for 中，基於迭代變量的類型範圍（如 i64 的 [..] 從 i64.MIN 到 i64.MAX）。
;
; 範例：使用無界區間做完整分類
v = 85
result = v: {
    [..0) -> 'negative'

    [0..60) -> 'fail'

    [60..80) -> 'pass'

    [80..90) -> 'good'

    [90..100] -> 'excellent'

    (100..) -> 'extraordinary'
}
print(result)

num: {
    1 || 3 || 5 || 7 -> print('奇數小數')

    2 || 4 || 6 -> print('偶數小數')

    -> print('更大數字')
}

; 有返回值，最後一個語句/值
result = x: {
    1 -> 1

    2 -> 2 + 1

    -> a + b
}

; 多行 arm body 必須使用大括號 -> { ... }
x: {
    nil -> {
        log('nil')
        do-cleanup()
        return
    }

    err -> {
        log(it.msg())
        do-cleanup()
        return
    }

    ok -> print(it)
}
```

> **多行 arm body 規則**：當 arm body 包含多個語句時，必須使用大括號 `-> { ... }` 括起來。單行 body 可直接寫在 `->` 之後。這是因為多行 body 若不使用大括號，option match 的 `it` 綁定將無法正確插入，導致編譯錯誤。

> **for-in 體內 match 語義**：`i <- (a..b]: { 1 -> ... 2 -> ... }` 對每個迭代變量 `i` 執行一次 match 體（`1 ->` 等同於 `i == 1 ->`，依此類推）。這是每輪迭代執行一次 match 的語法糖。

#### Match 主語的三種形態

match 主語可以是**值**、**表達式**或**塊**：

| 形態 | 寫法 | 求值方式 |
| --- | --- | --- |
| 值 | `x: { ... }` | 直接匹配 `x` |
| 表達式 | `x + 1: { ... }` | 先算出表達式的值再匹配 |
| 塊 | `{ ... }: { ... }` | 執行一段陳述主體，匹配其間 `?=` 的結果 |

前兩種是**表達式**：既可當陳述，也可在值語境取值（`v = x: { ok -> it, -> 0 }`）。
第三種**只是陳述**——不是表達式、不能賦值、沒有區塊尾值，`-> _` 用來丟棄結果。
`no fmt` 會把 `}: {` 原樣往返，臂間空行規則與一般 match 相同。

##### 表達式主語（`expr: { ... }`）

`expr: { ... }` 的 `: {` 優先級**高於** `=`：在賦值右側它是整段 RHS，而不是
「先完成 `r = <expr>`、後面再跟一條獨立的 match」。因此 option 算術可以一段式解包：

```no
#{overflow=wrap}
rem = seconds % 3600: {
    ok -> it

    -> 0
}

#{overflow=wrap}
sec = rem % 60: {
    ok -> it

    -> 0
}
```

這等價於先前必須拆開的三段式 `rem-opt = seconds % 3600` / `rem = 0` /
`rem = rem-opt: { ok -> it  -> 0 }`。首次賦值可以帶顯式型別
（`rem i64 = seconds % 3600: { ... }`），二次指派則直接寫 `rem = ...: { ... }`；
兩者都是單一值位 match，結果型別由臂尾表達式決定。

語意與限制：

- 主語是 D6 option 算術（`+ - * / %`）時結果為 `?T`。match 會把它歸一化成
  `__match_subj_L_C ?T` 合成暫存：主語**恰好求值一次**、`ok -> it` 綁到 payload、
  條件走 option tag 比較，`-> 0` 兜底 `nil` / `err`。合成名不會出現在診斷或
  `no fmt` 輸出裡。
- match 的結果是 **plain T**，並會登記進語義副表；因此後續可繼續寫
  `sec = rem % 60: { ... }`，連續多段值位 match 不會丟失 payload。
- 值位 match 只在**值語境**附加（賦值／`?=`／複合指派的右側），且 `: { ... }`
  必須真的是 match 區塊。`cond: { body }` 這類陳述層級的條件循環與 struct 字面量
  仍走既有解析路徑，不受影響。
- **欄位目標的賦值也是值語境**（2026-11-09 起）：`d.month = mp - 9: { ... }`、
  `b.n ?= b.n + 1: { ... }` 的 `: {` 綁在**右值**上，不是整個賦值。此前這兩條
  分支在解析右值時漏推 `CTX_EXPR`（識別字目標走 `let` 有推、`obj.f =` / `a[i] =`
  沒有），結果 `: {` 被語句層撿走、套在整個賦值外層，解析成
  `match(主語 = (d.month = mp - 9))`：賦值照舊執行並把 `?T` 隱式拆包後寫進欄位，
  而 `ok -> it` / `-> 0` 兩臂形同死碼。現在臂值才真正落回欄位。
- 索引目標（`a[i] = …: { ... }`）形狀相同，但 D9 本來就禁止下標直寫，這種寫法
  仍會被 checker 以 `idxwrite` 拒絕，須改寫成 place-match（`a[i]: { ok -> it = v }`）
  或 `?=`。
- `no fmt` 原樣往返 `expr: { ... }`，不會拆成賦值＋獨立 match 兩條陳述。
- **型別可見性邊界**：option 算術主語要歸一化成 `?T`，兩個運算元的宣告型別必須
  能被 parser 靜態看見。若運算元來自深層方法鏈（接收者型別連 AST 回放都推不出），
  就在它的宣告上補一個顯式型別，例如 `e i64 = flr.to-i64()`；否則歸一化會保守
  跳過，`ok -> it` 沒有綁定、MIR 零填，觀測值恆為 0。`src/std/number.no` 的
  `f64.to-str-general` 就是這樣：少了這個標註，`fmt.fmt-f64(2.5, '')` 會從
  `'2.5'` 退化成 `'3'`。迴歸釘在
  `src/parser/value_match_expr_subject_test.go::TestValueMatchExprSubjectASTAnnotatedOperand`
  （拿掉標註即紅）。

`src/std` 已於 2026-11-09 把 **122 處 / 22 個模組**的 option 解包改成這個一段式寫法：
除最初的三段式（`x-opt = expr` / `x = 0` / `x = x-opt: { ... }`）外，也涵蓋兩段式
（無零值初始化）、顯式型別首次賦值（`x i64 = expr: { ... }`）、以及中間變數在同函式
內多次成對宣告的寫法。摺疊後主語可以是方法呼叫、索引或 option 算術；
`ok -> it`／`ok(v) -> v`／常量臂／`-> _` 皆經實測等價，`no fmt` 對這些站點冪等。
編譯器的型別推斷同時照顧「語義副表查到 `?T`、但宣告其實是 `i64`」這種全域同名殘值，
`ok -> it` 不會因此丟失綁定。

保留（9 處）的情形：① 內層 match 會寫回 `-opt` 變數本身（該變數是後續值位 match
的主語）；② 兩層鏈式 option match；③ 按值分派的值位 match（如 errno → 訊息，不是
option 解包）；④ 宣告與 match 之間還有其它陳述、或中間變數是跨分支累積的結果
（摺疊會改變求值時機）。

##### 塊主語（`}: {`）

塊主語把一段陳述包起來，讓**沒有 `?T` 結果參數的函式**也能就地處理 `?=` 的上拋：

```no
; d i64: 除數，0 時 6/d 得 err
f = (d i64) {
    {
        v ?= 6 / d; 失敗不再要求函式有 ?T 結果參數
        print(v.to-str()); 失敗時不會執行
    }: {
        err -> print(it.msg())

        nil -> print('nil')

        -> _; 丟棄結果
    }
    print('之後照常執行'); arms 跑完後流程繼續
}
```

語意：

- 主體內的 `?=` 由**最近的**塊 match 捕捉（巢狀時最內層優先；即使所在函式有
  `?T` 結果參數，也是塊 match 優先）。捕捉是遞迴的：裸塊、if 主體、迴圈主體、
  match 臂裡的 `?=` 都算；函式定義是邊界，函式字面量不是。
- 被匹配的是三態 `?T`，沿用標準 option match 的**窮盡性檢查**：沒有兜底臂時，
  即使 `ok ->` 執行期不可達，也必須補齊 `nil` / `err` / `ok` 三臂。
- 主體順利走完（零個 `?=` 或全部成功）⇒ 匹配到 `nil`；某個 `?=` 失敗 ⇒ 匹配到
  該次的 `nil` 或 `err`（`err` 臂的 `it` 是完整的 `err.err`，見〈`it` 的型別〉）。
- **一次失敗停住整個主體**：後續陳述、後續迴圈 iteration 都不再執行，arms 只跑一次。
  這是「停止區塊」，不是函式 `return`，使用者自己的 `return` / `break` / `continue`
  綁定不受影響。
- 展開完全由既有核心 AST 組成（合成 `?T` 區域變數 + `if` 守衛鏈 + 一般 match），
  沒有新的 HIR / MIR 節點。合成變數名不出現在診斷或 `no fmt` 輸出裡。

> 與 `=` 就地捕獲的分工：`=` 把失敗**存進目標變數**、流程繼續；`}: {` 把失敗
> **收斂成一條 match**、並停止主體剩餘部分。需要「失敗後仍繼續」時用 `=` +
> 後續 match；需要「失敗就跳到清理臂」時用 `}: {`。

#### Match 風格指引

```no
#{fmt-skip}
; ❌ 避免重複分支體
w = tls-c.send(req)
w: {
    nil -> {
        tls-c.close()
        return
    }

    err -> {
        tls-c.close()
        return
    }

    ok -> n = it
}
```
```

```no
; ✅ 公共邏輯放 -> catch-all
w = tls-c.send(req)
w: {
    ok -> n = it

    -> {
        tls-c.close()
        return
    }
}

; ✅ 反之亦然：簡單分支命名，複雜邏輯放 ->
val: {
    nil -> return

    err -> log(it.msg())

    -> {
        n = it
        total = total + n
        process(n)
    }
}
```

```no
; 單語句 — 不加大括號
val: {
    ok -> print(it)

    -> print('empty or error')
}

; 多語句 — 必須加大括號
val: {
    ok -> {
        n = it
        total = total + n
    }

    -> {
        log('failed')
        return
    }
}
```

```no
; it 隱式綁定
val: {
    ok -> process(it); it = 解包後的值

    err -> log(it.msg()); it = 結構化錯誤 err.err（非 str）

    -> log('empty'); catch-all，此處為 nil
}
```

```no
#{fmt-skip}
; it 的作用域：巢狀 match 按層還原
; （佔位函數名寫 keep —— `use` 是導入關鍵字，不能當函數名；
;   且裸 `->` 臂在 nil/err 未同時認領時讀 it 是編譯錯誤，故各層都寫 ok ->）
outer: {
    ok -> {
        keep(it)            ; 外層 it
        inner ?T = f(it)
        inner: {
            ok -> keep(it)  ; 內層 it = inner 的解包值
        }
        keep(it)            ; ✅ 內層臂結束後 it 已還原成外層值
    }
}
```

> **注意**：還原只發生在**巢狀**的 match。最外層 match 結束後 `it` 仍保留最後一個臂的值，  
> 不要在 match 之外使用 `it`。需要跨層使用時請存到具名變數（`doc = it`），或用析構綁定  
> `ok(v) -> ...` 把載荷綁到自己的名字上。

> **最佳實踐（option 解包首選 `it`）**：被匹配值是 **option `?t`** 時，優先寫 `ok -> ...(it)...`  
> 而非 `ok(v) -> ...(v)...`——不引入額外綁定名，也就自然避開「不同臂同名變量導致 codegen 串型」的坑。  
> 只有兩種情形必須保留**具名綁定** `ok(v) ->`：(1) 被匹配值是**用戶自定義枚舉變體**（如  
> `res { ok(v str) }`、`shape { rect(w, h) }`）——那裡的 `it` 是**整個枚舉**而非載荷，取載荷只能具名；  
> (2) 需要在**巢狀 match 之後**繼續使用該值——內層 match 會重綁 `it`，請用具名綁定或先複製到具名局部。

### 下標寫入必須顯式解包

索引讀取傳回值是 option（越界為 nil），**直接給下標賦值會把越界變成未檢查的越界寫**，因此不允許，必須二選一：

```no
#{fmt-skip}
; ❌ 不允許：a[1] = 2
```
```

```no
; ✅ place-match：自行處理越界臂（it = 元素位置，可讀可寫）
a[1]: {
    ok -> it = 2

    -> _
}

; ✅ 錯誤上拋：所在函式有 option 結果參數時（越界 → 上拋）
a[1] ?= 2
```

- 編譯器能證明下標不越界時（定長陣列 + 常量下標、迴圈變數可證明界內等）**內部消除檢查、直接賦值，沒有運行開銷**；但語法上仍必須寫解包形式——用戶側沒有任何豁免。
- map（`[str]i64` 等）**完全不受此規則約束**：`m['b'] = c` 直接寫即可，不需要 place-match、也不需要 `?=`——map 的下標是**鍵**而非位置，寫入是 put/insert（缺鍵插入、有鍵覆蓋），恆有定義、不存在「越界寫」；結構體欄位基底（`self.keys[i] = v`）暫不受此規則約束。
- map 的**讀**側仍走 option 口徑（與陣列/vec/slice 的 `a[i]` 完全同一條規則）：函式內的 `x = m[k]` 得到 `?V`，鍵不存在時為 `nil`。`m[k]` 與 `m.get(k)` 同義，`m[k] = v` 與 `m.put(k, v)` 同義，兩種寫法可自由混用。
- `m[k] ?= v` 是**編譯錯誤**（不只是多餘）：`?=` 的 ok 臂是「讀出元素再寫回**位置**」，map 讀出的 `?V` 只是 `get` 的副本，寫回它不會回到 map。請直接寫 `m[k] = v`。`m[k]: { ok -> it = v }` 這種 place-match 寫法仍然可用（等價於 put）。

### 迭代變數也是元素「位置」

`x <- a`（`a` 為 `arr` / `vec` / `slice` / 定長陣列）綁定的 `x` 就是**元素本身的位置**，不是每輪載入的副本：

- **讀** `x` 會重新讀 `a[__i]` —— 同一輪內先寫後讀能看到新值，`x = x + 1` 就是讀-改-寫；
- **寫** `x = v` 落回 `a[__i]`；
- match 臂裡的 `it` 是同一位置的**別名**（僅在該臂內有效），所以 `x: { ok -> it = v }` 與 `a[i]: { ok -> it = v }` 完全等價。

```no
; 以下兩段結果相同：a 的每個元素都變成 5
a []i64 = [0, 0, 0]
i <- [0..3): {
    a[i]: {
        ok -> it = 5

        -> _
    }
}

b []i64 = [0, 0, 0]
x <- b: {
    x: {
        ok -> it = 5

        -> _
    }
}
```

- 元素本身是 option（`[]?T`）時，`it` 是**載荷**，寫回時自動包回 `ok`。
- 臂結束後別名失效：match 之後再寫 `it = v` 不會回到容器。

**不是位置的情況**（寫入只留在局部）：

- `c <- s`（`str`）：元素是解碼出的**碼點**，字串裡沒有對應的存儲格。
- `k <- m`（map）：下標是**鍵**而非位置，寫入不落回表。

### `it` 只能在「可證明是單一情況」的臂裡使用

catch-all `->` 臂承接的是前面沒有被顯式臂認領的所有情況。只有當剩下的情況**恰好一種**  
時 `it` 才有單一明確的意義：

```no
; ✅ -> 就是 ok（nil / err 都已被認領）
v: {
    nil -> log('empty')

    err -> log(it.msg())

    -> process(it)
}

; ✅ -> 就是 err（ok / nil 都已被認領）
v: {
    ok -> process(it)

    nil -> log('empty')

    -> log(it.msg())
}
```

```no
#{fmt-skip}
; ❌ -> 可能是 nil 也可能是 err —— 編譯錯誤
v: {
    ok -> process(it)

    -> log(it)
}
```
```

補上 `nil ->` / `err ->`，或改用 `ok ->`，即可通過。enum 與裸 match（`{ cond -> ... }`）  
不在此限：`it` 指的是被匹配的值本身，沒有 nil / err 的可能。

> `err ->` 臂的 `it` **永遠是錯誤值（`err.err`）**，與 option 的元素型別無關——  
> `option { ok(v t), nil, err(e err.err) }`。所以就算匹配的是 `?i64` / `?bool` 這類  
> 純量 option，`err ->` 臂裡的 `it` 也是結構化錯誤：讀訊息用 `msg = it.msg()`，  
> 比對錯誤碼用 `it.is(code.io)`，發生位置在 `it.file` / `it.line` / `it.col`  
> （由編譯器在 `err(...)` 的構造點自動注入）。

```no
; ✅ 組合 option 模式：nil || err -> body
; 當 option 為 nil 或 err 時共用同一個分支
val: {
    nil || err -> {
        cleanup()
        return
    }

    ok -> process(it)
}

; ✅ 也可與 -> catch-all 混用
val: {
    nil || err -> log('failed')

    ok -> process(it)
}
```

### 短路管道（`->`）

`->` 是**左結合的短路管道運算符**，語義固定，**不受外層是否賦值、是否在 match 臂內影響**。裸語句、match 臂的單行 body、賦值右側三處共用同一套 lowering。

管道內的節點分三類：

| 節點                                | 對管道 state 的影響                             |
| --------------------------------- | ----------------------------------------- |
| 副作用呼叫（無回傳，如 `print('A')`）         | 執行，**不改變 state**；只要先前是 Ok 就繼續下一個節點        |
| 回傳 `?T` 的呼叫（可失敗，如 `might-fail()`） | 執行，**覆蓋 state**；一旦變成 nil/err，**後續節點全部跳過** |
| 末尾普通值表達式                          | 僅在 state 仍為 Ok 時求值                        |

**只有回傳 `?T` 的節點有能力把管道切到失敗**；`print` 這類副作用永遠不會。所以「整條鏈都是 `print`」看起來像沒有短路，其實只是沒有節點能產生失敗。

```no
; 沒有可失敗節點 → 全部執行
ok -> print('A') -> print('B')

; might-fail() 回傳 ?T → 失敗時 print('B') 不執行
ok -> print('A') -> might-fail(x) -> print('B')

; 賦值右側：同一套語義
r = print('E') -> might-fail(x) -> 42
```

**邊界澄清：**

1. `->` 語義唯一，裸語句與賦值表達式共用同一套管道 lowering，不會因為外層有沒有賦值而切換模式。
2. match 臂**單行 body 裡的 `->` 是管道延續，不是新臂**；臂與臂之間由**換行**分隔：

```no
; ✅ 一個臂，body 是管道 → 兩行都印
ok -> print('A') -> print('B')

; ✅ 兩個臂（換行分隔）→ 命中 ok 就不會印 'miss'
v: {
    ok -> print('hit')

    -> print('miss')
}
```

> ⚠️ 臂是以換行分隔的，所以寫在同一行的 `pat -> A -> B` 是**一個臂的管道**，不是「if A else B」。需要 if/else 時請把臂分行寫，或用 `{}` 包裹的短路組。

1. **管道可以產值**：寫在賦值右側時，管道的結果就是**末尾的值節點**（僅在 state 仍為 Ok 時求值）。

```no
n = 7
x = 1 > 2 -> 42; 條件不成立 → x 保持原值 7
y = 2 > 1 -> 42; y == 42
s str = 'old'
s = 1 > 2 -> 'new'; 短路 → s 仍是 'old'
r = print('E') -> might-fail(bad) -> 99; 中段失敗 → r 保持原值
```

失敗（條件不成立或被 `?T` 節點短路）時**賦值不生效**：變數保留它原本的值；若是首次宣告則為該型別的零值。這一點和敘述形式一致 —— `{ 1 > 2 -> x = 42 }` 同樣不會改動 `x`。

值的型別由臂的末尾表達式推斷（`str` / `i64` / `?T` 皆可），與 `x = subject: { arms }` 這既有的 match 取值形式共用同一套推斷。

### If / Else

if-else 組（短路）**必須**用 `{}` 包裹。第一個匹配的條件勝出，後續條件不再檢查。

```no
; 多分支（推薦新式）— 短路 if-else 鏈
{
    a == 1 -> {
        a = 1
        b = 2
    }

    a == 2 || a == 3 -> do-something()

    -> {
        c = 0
    }
}

; 單 if（保留）— 獨立 if，不短路
x == 1 -> do-something()

; 三元表達式 condition ? true-value : false-value
c = flag ? 1 : 2
max = sum > 10 ? sum : 10
```

**重要規則：**

1. **短路組** — 多個 `cond -> body` 用 `{}` 包裹時，形成 if-elif-else 鏈，只有第一個匹配的分支執行。
2. **獨立 if** — 在函數體/循環體中直接寫 `cond -> body`（不包裹 `{}`）是獨立 if，不會與相鄰的 if-then 短路。
   **一旦出現 else 就必須成組** — 換行的 `->` 兜底臂寫在 `{}` 外面是**編譯錯誤**：孤立 `cond ->` 配 `->` 報 `k3r7mbqd`，它上面還有兄弟 `cond ->` 臂時報 `m7vq3xrt`（訊息會點明被丟掉的臂數）。解析器只把換行的 `->` 摺疊到**最近的一條**裸 if，其餘臂靜默失效——字面讀起來的意思與實際編譯出的結果完全不同，所以定為規則而非警告。請把所有臂包進 `{ ... }`；**不帶 else 的孤立 `cond -> body` 仍允許裸寫**。
3. **禁止混雜** — `{}` 短路組內的直接子節點必須全部是 `cond -> body` 分支，不能混入普通語句。

```no
#{fmt-skip}
; ❌ 不會短路 — 這些是獨立 if，所有條件都會被檢查
git-dispatch = (cmd str) {
    cmd == 'a' -> {
        fa()
    }
    cmd == 'b' -> {
        fb()
    }
}
```
```

```no
; ✅ 短路 — 用 {} 包裹，第一個匹配勝出
git-dispatch = (cmd str) {
    {
        cmd == 'a' -> {
            fa()
        }

        cmd == 'b' -> {
            fb()
        }

        true -> {
        }
    }
}
```

```no
#{fmt-skip}
; ❌ if/else 寫在短路組外面 — 編譯錯誤
; 換行的 `-> break` 只掛在 `c == 43` 上，命中 `c == 45` 之後仍會被 break 掐斷
;   c == 45 -> {
;       exp-sign = -1
;       i = i + 1
;   }
;   c == 43 -> i = i + 1
;   -> break
```
```

```no
; ✅ 同一份代碼寫成 if 組 — 臂間互斥，`->` 才是真正的兜底臂
{
    c == 45 -> {
        exp-sign = -1
        i = i + 1
    }

    c == 43 -> i = i + 1

    -> break
}
```

```no
#{fmt-skip}
; ❌ match-block 混雜普通語句 → 編譯報錯
{
    cmd == 'a' -> {
        fa()
    }
    print(cmd)
    cmd == 'b' -> {
        fb()
    }
}
```
```

```no
; ✅ 普通語句放到分支 body 內部
{
    cmd == 'a' -> {
        print(cmd)
        fa()
    }

    cmd == 'b' -> {
        fb()
    }

    true -> {
    }
}
```

### 異步編程（run / awy）— 已廢棄手寫，推薦協程組

> ⚠️ **已廢棄（手寫原語）**：`run` / `awy` / `cancel` / `cancelled` 已廢棄，不推薦手寫。手動管理 task 句柄不安全（未 await 會洩漏參數緩衝區、複製句柄後重複 await 會崩潰），協作式取消無法強制中斷長阻塞調用。請改用「協程組」（見下方），自動管理句柄與依賴。

Nolang 使用 `run` 和 `awy` 實現異步並發。異步函數的名稱必須以 `-async` 結尾，但不使用 `async` 關鍵字。

- `run` — 啟動異步線程，返回一個 task handle
- `awy` — 等待異步線程完成並取得結果

```no
; 異步函數定義（名稱以 -async 結尾）
compute-async = (n i64) (r i64) {
    r = n * 2
}

; 基本異步調用
test-basic = () {
    h = run compute-async(21)
    r = awy h
    print(r); 42
}

; 並發多個任務
test-concurrent = () {
    h1 = run compute-async(10)
    h2 = run compute-async(20)
    r1 = awy h1
    r2 = awy h2
    print(r1); 20
    print(r2); 40
}

; 內聯等待
test-inline = () {
    r = awy run compute-async(5)
    print(r); 10
}
```

> **命名規則**：異步函數名必須以 `-async` 結尾（如 `compute-async`、`fetch-data-async`）。不使用 `async` 關鍵字聲明。

#### 協程組（coroutine group）— 推薦

語句位置的裸塊 `{ ... }` 是**協程組**：塊內每條「直接調用 `-async` 函數」的語句默認被 **spawn**（等價於 `run`），  
結果在**需要它的地方**才 `awy`。不需要寫 `run` / `awy`。

```no
hello-async = (i i64) (r i64) {
    r = i
}

; 無依賴：兩個任務並發（先後 spawn，最後一起 await）
test-concurrent = () {
    r1 i64
    r2 i64
    {
        r1 = hello-async(1)
        r2 = hello-async(2)
    }
    print(r1); 1
    print(r2); 2
}

; 有依賴：r2 讀 r1，不能並發 —— 退化成 await（順序執行）
test-sequential = () {
    r1 i64
    r2 i64
    {
        r1 = hello-async(1)
        r2 = hello-async(r1)
    }
    print(r1); 1
    print(r2); 1
}
```

規則：

- **降級條件**：後面的語句讀到（或覆寫）前面某條語句綁定的變量時，那條語句的任務必須先落地，  
  於是在該處插入 `awy`，整組退化為順序執行。依賴是傳遞的（`r3` 讀 `r2`、`r2` 讀 `r1` ⇒ 整條鏈順序）。
- **屏障**：組內不是「直接 `-async` 調用」的語句（普通賦值、`print`、迴圈、`if`…）是屏障，  
  它之前所有仍在飛行中的任務都會先 `awy`。這保證組內讀到的永遠是值，不會是 opaque 句柄。
- **函數體不是協程組**：只有語句位置的裸塊才是。函數體、`if` 分支、迴圈體都不是，  
  所以 `f = hello-async(1)` 在函數體裡仍然只是建出一個 future，不自動 await。
- **丟棄結果也要 await**：`{ side-async(5) }` 仍會 spawn + await —— 未 await 的任務會洩漏它的參數緩衝區。

> **等價展開**（上面並發例子的實際 lowering）：  
> `__ag0 = run hello-async(1)` → `__ag1 = run hello-async(2)` → `r1 = awy __ag0` → `r2 = awy __ag1`。

#### 無色 async：`co` 關鍵字 — 推薦

`co` 是建立在協程組之上的**無色（colorless）**&#x8A9E;法：你寫未染色的函式名 `worker`，編譯器自動單態化出 `worker-async` 變體，並在底層以染色無棧協程執行。你不需要手寫 `-async` 後綴，也不需要手寫 `run` / `awy`。

```no
worker = (n i64) (r i64) {

    #{overflow=wrap}
    r = n + 1
}

; 入口由頂層語句自動合成（無需、也不允許顯式 `main`）
r1 i64
r2 i64
{
    r1 = co worker(1); 自動單態 worker-async 並 spawn
    r2 = co worker(2); 與 r1 並發執行
}
print(r1.to-str() + ' ' + r2.to-str())
```

規則：

- **單態化**：`co worker(1)` 會自動生成 `worker-async`（複製 `worker` 的本體、改名）。若 `worker` 同時也被同步呼叫，則 `worker` 與 `worker-async` 兩個方法共存。
- **傳遞性**：若某函式內部呼叫了異步函式（例如 `worker` 內 `co child(...)`），則 `worker` 也會被單態為 `worker-async`，異步染色沿呼叫鏈向上傳遞。
- **語句級 `co`（協程組之外）**：當 `co` 出現在協程組之外（例如在函式體裡 `d = co worker(5)`），編譯器會把它包成 `run worker-async(5)` + `awy` 的內聯 await，使 `d` 取到的是結果值而非 task 句柄。
- **底層仍是染色無棧協程**：`co` 只是語法糖。協程組內 `r1 = co worker(1)` 與 `r2 = co worker(2)` 各佔一行（`;` 是註釋，不能當語句分隔符），等價於協程組展開——先後 spawn `worker-async`，最後一起 `awy`。

### 多重賦值

函數可以返回多個值，調用時使用多重賦值接收：

```no
; 函數定義返回多個結果參數
swap = (a i64, b i64) (x i64, y i64) {
    x = b
    y = a
}

; 多重賦值
a, b = swap(5, 3)

; 使用 _ 忽略不需要的返回值（占位變數）
_, b = swap(5, 3); 只取第二個值，忽略第一個
a, _ = swap(5, 3); 只取第一個值，忽略第二個
_, _ = swap(5, 3); 忽略全部返回值（呼叫僅為副作用）

; 也支援作為 match arm 的 body
val: {
    ok -> {
        a, b = parse-pair(it)
    }

    -> return
}
```

## 運算符重載

當左操作數的靜態型別定義了特定的 `_op` 方法時，編譯器會在 lowering 階段把中綴運算 `a op b` 自動改寫為方法呼叫 `a._op(b)`。重載集合是封閉的 5 個對稱二元算術運算符：

| 運算符 | 重載方法 |
|--------|----------|
| `+` | `_add` |
| `-` | `_sub` |
| `*` | `_mul` |
| `/` | `_div` |
| `%` | `_mod` |

```no
big {
    v i64
}

; 回傳型別書寫 ?big，編譯器視作純 payload big
big._add = (b big) (c ?big) {
    c = big {
        v: .v + b.v
    }
}

f = (a big, b big) (out big) {
    r big = a + b; 編譯器改寫為 a._add(b)
    out = r
}
```

**型別約定**：運算符重載方法的結果參數**書寫為 `?T`，生效型別是純 payload `T`** —— 編譯器不做 option 包裝／拆包，方法體內直接對 `c` 賦 `T` 值，呼叫端拿到的也是 `T`。除零等錯誤與內建數值類型一樣在運行期直接 trap，不產生 option。

**語義要點**：

- 僅當左操作數的靜態型別定義了對應 `_op` 方法時才改寫；內建數值類型（i64、f64 等）仍走內建算術。
- 顯式方法呼叫 `a._add(b)` 與 `a + b` 完全等價。
- 左嵌套鏈式運算逐層改寫：`a + b + c` → `(a._add(b))._add(c)`；中間結果型別仍是 `T`，可繼續參與運算或方法呼叫。
- 標準庫 `bigint` 即以此機制支持 `+ - * / %`（見「標準庫 > 其他模組」的 bigint 小節）。

## 數組與切片

容器存儲數據副本，原變量與容器獨立，杜絕懸垂引用。

**定長数组arr：**

```no
; 使用定長数组
a [3] = [1, 2, 3]; 长度为 3 的定長数组 i64
a [3]u16 = [1, 2, 3]; 指定类型的定長数组

a[nil]
u16 = [1, 2, 3]; 自動推斷長度（指定類型）
a[nil] = [1, 2, 3]; 自動推斷長度（i64）
```

**變長數組vec：**

```no
v = [1, 2, 3]; 變長數組 i64
bs = [0x11, 0x22, 0x33]
v []u8 = [1, 2, 3]; 指定类型的變長數組

; 預分配（避免反覆擴容）
v = with-cap(100); len=0, cap=100（需 push 後索引）
v = with-len(100); len=100, cap=100（可直接索引）
v = with-cap-len(200, 100); len=100, cap=200（預留擴容空間）
```

**切片slice（視圖，非獨立類型）：**

切片是對原始資料的**視圖（view）**，不會複製資料，也不會產生新的獨立類型。  
切片內部只記錄一個指向原始緩衝區的指標、長度和容量，因此：

- 通過切片修改元素會影響原始資料，反之亦然
- 切片不擁有資料，原始變數釋放後切片即失效
- 切片的類型由原始類型決定，方法自然適用，無需「繼承」機制

```no
; 支持arr/vec/str
; 支持範圍 和for <- 的表示一致
nums [5]u8 = [0, 1, 2, 3, 4]

nums[..]; [0 1 2 3 4]   完全無界，等同整個切片
nums[1..]; [1 2 3 4]     無上限，從索引 1 到末尾
nums[..4]; [0 1 2 3 4]   無下限，從開頭到索引 4
nums[2..3]; [2 3]        有界，兩端都包含
nums[1..3]; [1 2 3]      有界，兩端都包含
nums[1..3); [1 2]        左閉右開
nums(1..3); [2]          兩端開區間

; 字符串（切片結果型別為 str 視圖，共享底層記憶體；下標為碼點位置，與 s[i] 一致）
s = 'abc'
s[1..]; 'bc'
s[1..s.len()); 'bc'
b str = s[0..1]; 切片結果已是 str，可直接賦值給 str 變數
```

> 切片語法支援與 match/range-for 相同的 16 種區間組合。在切片上下文中，無界區間基於切片的長度：`[..]` 表示整個切片，`[a..]` 表示從索引 a 到末尾，`[..b]` 表示從開頭到索引 b。

**切片的類型與方法：**

切片不生成新的獨立類型，只是原始類型的一個視圖（調整了起始指標和長度）。  
因此原始類型的方法直接可用：

| 原始類型           | 切片視圖類型       | 可用方法                                                                             |
| -------------- | ------------ | -------------------------------------------------------------------------------- |
| `arr` (`[n]t`) | `[]t<range>` | `[]t` 的所有方法（如 `len`、`push`、`pop`、`contains`、`reverse`、`clone`、`fill`、`to-arr` 等） |
| `vec` (`[]t`)  | `[]<range>`  | 同上                                                                               |
| `str`          | `str<range>` | `str` 的所有方法（如 `to-upper`、`to-lower`、`index`、`contains`、`slice`、`copy`、`fill` 等）  |

```no
; arr 切片 → vec 視圖，共享 arr 的底層記憶體
a [5]u8 = [0, 1, 2, 3, 4]
s = a[1..4]; s 是 []u8 視圖，指向 a 的記憶體
n = s.len(); slice.len()

; vec 切片 → vec 視圖，共享 vec 的底層記憶體
v = [10, 20, 30, 40, 50]
s = v[2..]; s 是 []i64 視圖
s.reverse(s.len()); slice.reverse

; str 切片 → str 視圖，共享 str 的底層記憶體
s = 'Hello World'
sub = s[6..]; sub 是 'World' 視圖
upper = sub.to-upper(); str.to-upper

; 通過切片修改元素會影響原始資料
data = [10, 20, 30, 40, 50]
view = data[1..4]; view = [20, 30, 40]
view[0] = 99; 修改 view 的元素
; data[1] 現在也是 99，因為 view 共享 data 的記憶體
```

### 索引

```no
; 字符串索引 → 回傳 ?char（Unicode 碼點，不是字節）
c char = s[i]; 顯式 plain 型別把結果拉回 char（見〈安全索引〉）

; arr、vec獲取元素
arr[i]; → ?elem
vec[i]; → ?elem

; map 獲取 value
map[str]
```

> **索引讀取的結果恆為 `?T`**：`arr[i]` / `vec[i]` / 定長陣列 / `slice` → `?elem`，  
> `str` / `txt` 索引 `s[i]` → `?char`（D6，與整數算術同一套統一規則）。  
> 因此 `str[i]` 的**推斷型別是 `?char`**；要拿回 plain `char` 有兩條路：  
> 在綁定上寫顯式 plain 型別（`c char = s[i]`，lower 期即短路合成，讀回 plain 值），  
> 或用 match / `?=` 解包。  
> `str[a..b]` 切片仍回傳 `str`（碼點語義視圖，共享底層記憶體）。  
> `char` 可**隱式轉換為 `str`**（編碼為單字元字串），因此下列寫法皆合法，無需手動呼叫 `char.to-str()`：

```no
s = 'héllo'
c char = s[1]; 'é'（char，其碼點為 233）
a str = s[1]; 'é'（char → str 隱式轉換）
b str = s[0..1]; 'hé'（切片結果本身已是 str）
msg = 'first: ' - s[0]; char 參與拼接（-）時隱式轉為 str
ok = s[0] == 'h'; char 與 str 比較時隱式轉為 str
```

### 安全索引（Safe Indexing）

對 `arr` / `vec` / `slice`（`[]T`）/ 定長陣列 / `str` / `txt` 的索引 `v[i]`，**結果一律是 `?T`**：越界時回傳 `nil`，在界內回傳 `ok(值)`，絕不會 abort（runtime error: index out of bounds）。這與整數 `+ - * / %` 的未標註溢出行為是同一條規則（見〈整數運算溢出〉）。

要把 `?T` 用掉，只有三條路（**`#{index-out=DEF}` 註解已於 D11 廢棄並移除**，殘留註解是編譯錯誤；
也沒有「顯式 plain 型別拉回 plain」這條捷徑，`v i64 = a[i]` 同樣必須顯式消費）：

**1. 讀取 —— match 解包**

值 match 直接取值，或陳述 match 在 `ok` 臂內指派。`-> _` 是「丟棄」臂，
在值語境下产出**元素型別的零值**（i64→0、str→''、byte→0、容器→空容器）。

```no
; 值 match
x = state[5]: {
    ok -> it

    -> _
}

; 陳述 match
state[5]: {
    ok -> x = it

    -> _
}
```

**2. 寫入 —— place-match**

`it` 在 `ok` 臂裡是**元素位置**（可讀可寫），不是值副本。

```no
state[5]: {
    ok -> it = x

    -> _
}
```

> **map 例外：** 寫入 `m['k'] = 1` **不需要解包**——map 的下標是鍵、寫入是 put/insert，
> 缺鍵插入、有鍵覆蓋，恆有定義，沒有「越界寫」可守衛。但 **map 讀取 `m['k']` 仍回傳 `?V`，必須解包**。
> `m[k] ?= v` 是**編譯錯誤**（`get` 讀出的 `?V` 只是副本，寫回它不會回到 map），請直接寫 `m[k] = v`。

**3. 簡寫 `?=` —— 上拋錯誤**

`x ?= state[5]`（讀）、`state[5] ?= x`（寫）是上面兩種 match 的簡寫：失敗時把整個 option
向上拋。它在**兩種情形之一**下合法：

- 所在函式有 `?T` 結果參數 —— 失敗時寫入結果參數並提前 `return`；
- 陳述位於**陳述層級塊 match** `{ ... }: { ... }` 的主體內 —— 由最近的塊 match 捕捉，
  主體停止、arms 只跑一次，函式**不需要** `?T` 結果參數。

兩者皆非是編譯錯誤。因此「回傳值不是 option 也想用簡寫」的標準兜底就是把函式體包進塊 match：

```no
handle = (x i64) {
    {
        y ?= x + 1
        print(1)
    }: {
        err -> _

        -> _
    }
}
```

**巢狀讀取**：內層 match 會重新綁定 `it`，故內層用具名綁定 `ok(it1) -> it1`。使用者給的精確形狀
（值 match 直接寫在 place-match 的 `it = ...` 右側）自 2026-11-06 起**會正確寫回容器**：編譯器在
lowering 時把該值 match 提升為合成暫存再重綁 `it`，回歸見 `tests/match.no` 的
`place-write-value-match` 段。**首選直接寫法**：值 match 的結果直接綁到最終要用的那個名字，
不要先塞進一次性暫存再拷一次。

```no
#{fmt-skip}
; ✅ 直接綁定目標變數
at = argt[i]: {
    ok -> it

    -> _
}
s.farg.push(at)

; ❌ 不要為單次使用引入一次性暫存
ixout1 = argt[i]: { ok -> it  -> _ }
at = ixout1
s.farg.push(at)
```

「把值 match 提到臂外的具名暫存、臂內再 `it = ixout1`」**與直接寫法並不等價**，不是可自由
互換的排版選擇：求值時機不同（提到臂外後，外層 `nil` / `err` / catch-all 也會先讀一次）；
`it` 只在臂內有效且指向元素位置，拷回必須留在臂內；具名暫存是新綁定，有自己的 move／
所有權與作用域退出銷毀；且巢狀時內層必須用具名綁定 `ok(it1) -> it1`，不能照抄臂外的
`ok -> it`。只有確實要在多處複用同一個值時才提升，並用有意義的變數名，不要留 `ixout…`
這類遷移工具生成的占位名。

#### 用 `?=` 與塊 match 收掉剩餘的一次性暫存

上面說的是「單次使用就直接綁定」。再往前一步：安全索引回傳的是 `?T`，**所以當函式的
結果參數本身就是 `?T` 時，連值 match 都不必寫，`?=` 一行直接上拋**：

```no
#{fmt-skip}
; ✅ 函式有 ?T 結果參數：無暫存、無 match
at ?= argt[i]
s.farg.push(at)
```

**函式結果不是 option 時**（最常見：無回傳值的輔助函式，或只是往容器裡寫回），用這種
「類似 try」的寫法把該陳述包起來：

```no
#{fmt-skip}
handle = (x i64) {
    {
        y ?= x + 1
        print(y)
    }: {
        err -> _

        -> _
    }
    print('continues afterwards')
}
```

⚠️ **這不是新的 `try` 語法，語言裡也沒有 `try` 關鍵字。** 它就是一個**陳述層級的塊
match**（`{ ... }: { ... }`），主語是三態 `?T`；`?=` 把失敗上拋給**最近的**塊 match
（詳見〈`?=` 錯誤上拋〉與〈Match 主語的三種形態〉）。失敗時塊主體剩下的陳述——包括
後續迴圈迭代——全部跳過，臂只執行一次，然後**繼續執行塊之後的程式碼**，不是函式
`return`。`err -> _` 丟棄錯誤，`-> _` 兜底丟棄成功結果；要處理錯誤可寫
`err -> print(it.msg())`。

⚠️ **語意差異要講清楚**（實測，別憑直覺）：`?=` 失敗時**目標照樣被寫成零值**——`i64` 得
`0`、`str` 得 `''`、`out[1]` 得 `0`，這點和舊的 catch-all `-> _` 完全一致。真正的差異只有
一處：`?=` 會**跳過塊主體中它之後的陳述**（在 `?T` 函式裡是提前 `return`，在塊 match 裡
是直接轉到臂）。

所以：**塊主體裡只有這一條陳述時，新舊寫法行為完全等價**（這也正是本倉庫批次改寫的
形態）；只有當同一塊主體裡 `?=` 後面還有別的陳述（迴圈計數、後續賦值）時才會分岔，那些
站點不能機械替換。

另一個容易踩的點：**成功時臂也會執行一次**，catch-all 收到成功值（`-> print('x')` 會印）。
所以臂裡只該放丟棄或純處理；`err -> _` / `-> _` 這種形態沒有副作用。

由此得到清理 `ixout…` 一次性暫存的順序：

1. 值只在同一處用一次 → 直接綁到最終目標名（見上文）。
2. 函式有 `?T` 結果參數 → `TARGET ?= EXPR`，無需暫存。
3. 否則 → 把該陳述放進塊 match 再寫 `TARGET ?= EXPR`；place-match 寫回
   （`SUBJ: { ok -> it = temp  -> _ }`）也直接縮成 `SUBJ ?= EXPR`。
4. 只有確實要在多處複用同一個值時才保留具名暫存，且用有意義的變數名。

兩個邊界：`?=` 在既沒有 `?T` 結果參數、也不在塊 match 主體內時是編譯錯誤
（`l86f91me`）；**帶型別宣告的 LHS 不能配 `?=`**（`ai i64 ?= a[i]` 是語法錯誤
`f5ikdmuc`）。需要 `byte → i64` 這類顯式提升時，改成帶型別宣告的直接值 match：

```no
#{fmt-skip}
ai i64 = a[i]: {
    ok -> it

    -> _
}
```

> **適用範圍：** `arr` / `vec` / `slice` / 定長陣列 / `str` / `txt` 一律有效；只有**直接變數基底**
> 的索引（`v[i]`）會被改寫為安全形式，`receiver.field[i]` 走原有 bounds-check 路徑。
> 比較運算元裡的索引讀取比的是 payload（`a[i] == 5`），不是整個 `{tag, payload}`。

> ⛔ **硬錯誤：** 把 `?T` 當普通值用（如 `crc = crc ^ s[i]`、`f(s[i])`、`v i64 = a[i]`）會阻斷
> `no build`，編譯器提示改用 match 或 `?=`（無 `?T` 結果參數時用塊 match 包住）。
> 既有語料已由 `no fmt --fix=index-out` 一次性遷移完畢，該工具隨 D11 退役。

> **核心保證：** 越界永不靜默崩潰——不是被 match 處理、就是被 `?=` 上拋。

> **比較運算元裡的索引讀取**同樣套用安全索引：`a[i] == 5` 比的是 payload（與 `?T == 5` 同一條規則），
> 不是拿整個 `{tag, payload}` 去比。越界時讀出 `nil`，其 payload 是元素型別的零值，
> 因此越界時 `a[i] == 0` 為真、`a[i] == 5` 為假，且**不會**讀到越界記憶體。
> `s[0] == 'a'` 同理：碼點與單字元字串比較時走 `char → str` 隱式轉換。

> **巢狀下標** `a[i][j]`：**兩層都是安全索引**。外層 `a[i]` 先做界檢查並判斷是否存在，
> 只有存在時才對它取 `[j]`；任一層越界，整個表達式就是 `nil`。
> 因此 `a[oob][0] == 'c'` 為假而不會 abort。

> **容器比較** `[]T == []T`（以及 `[N]T == [N]T`）是**結構比較**：先比長度，再逐元素比。
> 元素本身是容器（`[][]i64`）時逐層遞迴；元素是 `str` 時比**內容**而不是 data 指標。
> 順序比較（`<` / `<=` / `>` / `>=`）對容器沒有定義，只支援 `==` / `!=`。

> **`nil` 只能與 option 比較**：`?T == nil` / `?T != nil` 合法；非 option 的
> `T == nil`（`str`、`[]i64`、`[N]T`、`txt`、`i64`、結構體…）是**編譯錯誤**（`q4nilcmp`）。
> 原因不是潔癖：`str == nil` 恆為 false（拿 `%str-long` 結構體與 0 比），
> `[]i64 == nil` 會掉進整數比較路徑產生非法 LLVM（`icmp eq i64 %vec, i64`），
> 兩者都在靜默給錯答案。要表達「可能沒有值」，把型別宣告成 `?T` 再與 `nil` 比。

## 結構體

結構體定義和字面量都必須使用多行形式，每個字段獨佔一行，字段之間不以逗號分隔，末尾也不跟逗號。

```no
user {
    name str
    age i64
}

u = user {
    name: 'Alice'
    age: 30
}
u.name = 'Bob'
u.age = 25
print(u.name)
```

### 結構體實作介面

結構體可以實作一個或多個介面，介面名寫在結構體名後面，以逗號分隔。

```no
; 實作單個介面
user json {
    name str
    age i64
}

; 實作多個介面
file enter, leave {
    path str
    fd i64
}
```

#### 跨模組介面實作

當實作**其他模組**定義的介面時，介面名必須帶模組前綴（`ShortName.`）。參見[跨模組調用前綴](module.md)。

```no
#{fmt-skip}
; ❌ 錯誤：db、rows、stmt 是 sql 模組定義的介面，不能省略前綴
db-mysql db {
    fd i64
}
```
```

```no
; ✅ 正確：使用 sql.db、sql.rows、sql.stmt
db-mysql sql.db {
    fd i64
}

rows-mysql sql.rows {
    fd i64
}

stmt-mysql sql.stmt {
    fd i64
}
```

> 內置介面（`enter`、`leave`）和同檔案定義的介面不需模組前綴。

## 方法

方法定義在類型上，使用 `.` 引用接收者（receiver）。

### 語法

```no
#{fmt-skip}
type.method-name = (params) (results) {
    ; . 是接收者
}
```

### 規則

1. 方法名使用 `type.method` 格式，type 必須是已定義的類型
2. 接收者不需要顯式聲明參數，在方法體內用 `.` 引用
3. 調用時使用 `receiver.method(args)` 語法
4. 返回值放在第二組括號中，與普通函數一致
5. **語法糖脫糖**：`type.method = (inputs) (rest-outputs...) {}` 等價於普通函數  
   `method = (inputs) (self type, rest-outputs...) {}`。調用 `instance.method(args)`  
   時，instance 綁定到輸出列表第一個參數 `self`；函數體內 `.` 指代輸出區 `self`；  
   函數對 `self` 的寫入直接修改原實例。輸出列表除 `self` 以外的其餘輸出，  
   支持可選解構接收或直接丟棄。

### 示例

```no
; str 方法
str.to-upper = () (out str) {
    n = .len-bytes()
    b []byte = with-cap-len(n, n)
    i <- [0..n): {
        c ?byte = .byte(i)
        v = c: {
            ok -> it

            nil -> 0

            err -> 0
        }
        {
            v >= 97 && v <= 122 -> b[i] = v - 32

            -> b[i] = v
        }
    }
    out = b.to-bytes-str()
}

; char 方法
char.is-digit = () (result bool) {
    result = false
    . >= 48 && . <= 57 -> result = true
}

; struct 方法
user {
    name str
    age i64
}

user.greet = () {
    print('Hello, ' - .name)
}

; 調用
s = 'hello'
u = s.to-upper(); receiver.method()
c char = 5
d = c.is-digit(); receiver.method()
u = user {
    name: 'Alice'
    age: 30
}
u.greet()
```

## 接口

```no
; 定義接口
json {
    to-json()
}

; 接口默認實現
json.to-json = () {
}

noml {
    to-noml()
}

serialize {
    json
    noml
}

serialize = json | noml
serialize = json & noml

; 接口實現
user json {
    name str
    age i64
}

; 重寫 + 調用父實現
user.to-json = () {

    ; 父實現
    ..to-json()
}

user.other = () {

    ; 當前實現
    .to-json()

    ; 父實現
    ..to-json()
}
```

### 接口合併

接口可以「合併」其他接口的方法：父接口名寫在接口體內，**每行一個**（可帶模組前綴），不加逗號、不加括號：

```no
json {
    to-json()
}
noml {
    to-noml()
}

serialize {
    json
    noml
}

; 傳遞合併：roundtrip 同時獲得 serialize、json、noml 的全部方法
roundtrip {
    serialize
    roundtrip-json()
}
```

規則：

- **遞迴合併**：合併是傳遞的——`roundtrip` 合併 `serialize`，而 `serialize` 又合併 `json` / `noml`，則 `roundtrip` 擁有全部三個接口的方法。
- **每行一個，不能加逗號**：`serialize { json, noml }` 與 C 風格枚舉（`color { red, green }`）同形，因此仍按枚舉解析，不是接口合併。
- **同名方法**：簽名相同（參數與回傳型別一致，接收者名可不同）⇒ 去重保留一個；簽名不同 ⇒ 診斷報錯。
- **實現端**：型別只需寫實作聲明（前置清單 `user serialize { name str }`），父接口的方法一併算作已實現，簽名校驗會穿透父接口。
- **`no fmt`**：舊的「前置清單」寫法（`serialize json, noml { }`）仍可解析，但 `no fmt` 會改寫成體內形式，`no vet` 對此給出 WARNING 提示。

診斷（`no vet` 與編輯器診斷同源，TraceID `ifmrg001`）：

| 情況 | 訊息 |
| --- | --- |
| 父接口未宣告 | `interface 'serialize': unknown parent interface 'noml'` |
| 循環合併 | `interface 'a': merge cycle a -> b -> a` |
| 同名異簽名 | `interface 'serialize': merged method 'to-str' has conflicting signatures ('json.to-str', 'noml.to-str')` |

> `serialize = json \| noml` 這類寫法是**聯合型別**（union type）語法，與接口合併無關；接口合併只影響「型別必須具備哪些方法」。

### 特殊接口

```no
file {
    enter
    leave
}
```

## 枚舉

```no
; red=0, green=1, blue=2
color {
    red,
    green,
    blue,
}

; 在普通方法中，a,b,c 實際是定義的a=0，b=1, c=2... 這是和其他語言不一致的地方。
; 所以正常不能用逗號的方式定義多個變量

; 這是一個特殊枚舉, 可以有類型，有逗號， 有別名
enum-name {
    a t,
    b u,
    c v,
}

; 注意這是一個普通的struct，多個字段沒有逗號
struct-name {
    a t
    b u
    c v
}
```

### 標籤列舉（帶載荷）

變體可攜帶具名載荷欄位，寫成括號形式 `變體(欄位 型別)`；無載荷的變體直接寫裸名。  
一個變體可帶**多個**欄位（多欄位載荷在堆上以合成 struct 表示）：

```no
#{fmt-skip}
; 標籤列舉：變體可帶載荷，tag 依聲明順序為 0,1,2...
result {
    ok(v t),
    nil,
    err(e str),
}

; 有值：載荷欄位 v，型別 t
; 空值：無載荷
; 錯誤：載荷欄位 e，型別 str（用戶自定義枚舉，與內建 option 無關）

shape {
    circle(r f64),
    rect(w f64, h f64),
    dot,
}
; 單欄位
; 多欄位
; 無載荷
```
```

匹配時用變體名作為臂模式；帶載荷的變體可析構綁定載荷（多欄位按位置一一綁定）：

```no
r result = ok(42)
r: {
    ok(v) -> print(v); 把 ok 的載荷綁定到 v

    nil -> print('empty')

    err(e) -> print(e)
}

s shape = rect(3.0, 4.0)
s: {
    circle(r) -> print(r); 綁定單欄位

    rect(w, h) -> print(w * h); 按位置綁定多欄位

    dot -> print('dot')
}
```

變體可在**表達式位置**直接建構，作為函數引數或其它變體的載荷：

```no
x f64 = perimeter(rect(3.0, 4.0)); 內聯建構並傳參
o outer = wrap(a(5)); 枚舉載荷本身是另一個枚舉
```

> **命名空間與裸名解析**：編譯器內部以「完整限定名」登記變體（`模組.枚舉.變體`，  
> 如 `option.option.ok`、`some-mod.my-result.ok`），不同枚舉的同名變體互不衝突。  
> 使用者只寫裸名（`ok`、`rect`），由編譯器**依被匹配/被建構變數的靜態型別**自動補全  
> 完整命名。因此兩個枚舉即使都有 `ok` 變體、且 tag 順序不同，也能正確區分。
>
> 也可寫舊式空格分隔型別 `ok t`，等價於 `ok(v t)`（省略欄位名）。  
> 標籤列舉的變體名稱會登記進編譯器的列舉變體表，供匹配與窮盡性檢查使用。

### 內建標籤列舉（`#{buildin}`）

在標籤列舉前加 `#{buildin}` 表示這是**內建列舉**：變體（名稱、順序、載荷型別）供  
匹配與窮盡性檢查使用，但底層表示與構造由 runtime/builtin 提供，**不生成使用者可見的  
struct/union**。`?t` option 即以此方式聲明：

```no
; src/std/global.no
#{buildin}
option {
    ok(v t),
    nil,
    err(e err.err),
}
```

`option` 的 tag 依序為 `0=ok`、`1=nil`、`2=err`，與內建 option 表示一致。使用者無需  
自行聲明 option；此處的 `#{buildin}` 讓內建 option 的變體來源單一化（見「可空類型(option)」）。

### 枚舉註解與內存佈局

枚舉支援 `#{...}` 註解，位置與其他目標相同（見〈註解位置〉）：**上方**（獨立成行）或  
**後方**（同一行尾隨）；寫在目標**前方同一行**是編譯錯誤。

註解可放在**兩處**：

1. **整個 enum 的上方** —— 作用於 enum 定義本身（如 `#{buildin}`、`#{inline=true}`）；
2. **單個成員的上方或同行尾隨** —— 作用於該成員（C 風格枚舉的「值」與標籤列舉的「變體」皆可）。

```no
; ① 整個 enum：獨立成行置於 enum 上方
#{inline=true}
box {
    full(v i64) #{doc='有值'},
    empty #{doc='空值'},
}

; ② 單個變體：獨立成行置於變體上方

; ② 單個變體：同一行尾隨

; `#{inline=false}`：明確寫出「不用 inline 佈局」
#{inline=false}
plain {
    a,
    b,
}

color {
    red #{deprecated},
    green #{deprecated},
    blue,
}
; C 風格枚舉的單個值同樣支援這兩種寫法
```

**註解不改變 enum 的結構**：變體名稱、宣告順序（tag）、載荷欄位與型別都不受註解影響，  
匹配與窮盡性檢查也完全不變。註解只是附加在定義上的**元數據**。

#### 內存佈局（棧式）

標籤列舉在記憶體中採**棧式佈局**（inline stack form）——一個按值存放的小物件，載荷不散落在堆上：

```llvm
%tenum_<name> = type { i64 tag, [N x i64] payload }
```

- `tag` 是判別式（`i64`），依宣告順序取 `0, 1, 2...`；
- `payload` 是**所有變體共用**的槽位陣列（union 語義：每個變體把自己的欄位寫進同一塊儲存）；
- `N` 是所有變體中最寬者所需的 8 位元組槽數，**最小為 1**（不會是零寬）。

因此大小是：

| 情形                                             | `N`               | 大小         |
| ---------------------------------------------- | ----------------- | ---------- |
| 所有變體皆無載荷（如 `color { red, green, blue }` 這類純標籤） | 1                 | **16 位元組** |
| 最寬變體為 `i64` / `f64` / 指標                       | 1                 | 16 位元組     |
| 最寬變體為 `?T`                                     | 2                 | 24 位元組     |
| 最寬變體為 `str` / `vec` / `[]T`                    | 3                 | 32 位元組     |
| 最寬變體為結構體 `T`                                   | `T` 各欄位槽數之和（遞迴展開） | 依 `T` 而定   |

也就是說：**默認就是 16 位元組起**，只有當某個變體的載荷確實更寬時才會長大——  
「默認固定 16 位元組」與「大小隨變體變化」是同一件事的兩面。

#### `#{inline}` 的三種寫法

`inline` 是一個**布爾**註解，有三種寫法，且**值會被真正遵循**：

| 寫法                | 含義                          |
| ----------------- | --------------------------- |
| `#{inline}`       | 簡寫，等於 `inline=true`（獨立鍵即為真） |
| `#{inline=true}`  | 明確為真（`#{inline=1}` 亦可）      |
| `#{inline=false}` | 明確為假（`#{inline=0}` 亦可）      |

非布爾值是**編譯錯誤**（`#{inline=foo}`、`#{inline='true'}`），不會被靜默當成真：  
`ValidateFieldTags`（TraceID `fieldtag1`）對結構體欄位與 enum 定義都會報。

在整個 enum 上方寫 `#{inline=true}` 是棧式佈局的**顯式標記**，表示「此枚舉使用棧式佈局」；  
編譯器把它記在 `TaggedEnumInfo.Inline` 上。**目前的默認佈局本來就是棧式**，所以加不加  
註解、寫 `true` 還是 `false`，佈局完全相同——標記的作用是讓意圖明確，並在日後若變更默認  
佈局時作為穩定的選擇開關。

> `#{inline}` 寫在**整個 enum 上方**表示「這個枚舉用棧式佈局」；寫在**單個變體**上目前  
> 不改變佈局，僅保留為該變體的元數據（與結構體欄位不同，見下節）。

#### 結構體欄位上的 `#{inline}`

與 enum 不同，**結構體欄位**上的 `#{inline}` 是**有實際效果**的：它直接決定該欄位是  
**按值內聯**在宿主結構體內（`%pt`），還是存成**指標**（`ptr`）。

```no
pt {
    x i64
    y i64
}

holder {
    inl pt #{inline=true}; 按值：結構體直接長在 holder 裡
    out pt #{inline=false}; 指標：欄位存的是指向 pt 的指標
    bare pt #{inline}; 簡寫，等於 inline=true
}
```

| 寫法                                             | 欄位佈局                        |
| ---------------------------------------------- | --------------------------- |
| `#{inline}` / `#{inline=true}` / `#{inline=1}` | **按值**內聯在宿主內（`%pt`）         |
| `#{inline=false}` / `#{inline=0}`              | **指標**（`ptr`）               |
| 不寫                                             | 取決於 `NOLANG_FIELD_PTR`（見下表） |
| `#{inline=foo}` / `#{inline='true'}`           | **編譯錯誤**——不是布爾              |

**沒有註解時的默認佈局由 `NOLANG_FIELD_PTR` 決定。** `mir.FieldPtrLayout` 等於  
`os.Getenv("NOLANG_FIELD_PTR") != ""`，**默認為關**；開啟它會啟動「Phase 1 佈局翻轉」，  
把結構體欄位的默認由按值改成指標。

| 欄位                             | 旗標關（默認）   | 旗標開（`NOLANG_FIELD_PTR=1`） |
| ------------------------------ | --------- | ------------------------- |
| 無註解                            | `%pt`（按值） | `ptr`（指標）                 |
| `#{inline}` / `#{inline=true}` | `%pt`     | `%pt`                     |
| `#{inline=false}`              | `ptr`     | `ptr`                     |

也就是說：**註解永遠生效，不受該環境變數影響**，旗標只改「沒寫註解時」的默認。因此在  
默認配置下，`#{inline=false}` 是「主動要求指標」，而 `#{inline=true}` 只是把本來就如此  
的佈局再寫明一次；一旦翻轉開啟，兩者的角色就反過來。

檢查器有兩條規則（TraceID `fieldtag1`，`ValidateFieldTags`）：

1. **只對結構體型別的欄位有意義。** 標量 / `str` / `vec` / 陣列 / 切片 / map / option /  
   指標欄位本來就一律按值存放，寫 `#{inline}` 等於什麼都沒說，因此報錯；寫  
   `#{inline=false}` 同樣報錯，因為那等於宣稱了與編譯器行為相反的佈局。
2. **按值內聯不得成環。** 被內聯的結構體存放在宿主**內部**，所以  
   `a { #{inline} b b }` 與 `b { #{inline} a a }` 併存時大小無限，必須報錯（直接自我  
   引用是退化情形）。只有 `=true` 才會建立按值邊，所以互相遞迴的結構體在**兩側都寫  
   `#{inline=false}`** 時合法。
   > ⚠️ **「不寫注解」不等於「不用 inline」。** 默認配置下不寫注解就是按值內聯，因此  
   > `a { x b }` 配 `b { y a }`（都不寫注解）在**默認配置下並不合法**，會以  
   > `opt: error: identified structure type 'b' is recursive` 失敗——而且這條錯誤目前  
   > **不會**由檢查器報出，要到 LLVM 驗證階段才炸。要在默認配置下寫遞迴型別，**必須**  
   > 顯式寫 `#{inline=false}`。只有 `NOLANG_FIELD_PTR=1` 時，「不寫」才等於指標。

#### 遞迴型別：為什麼需要 `inline=false`

一個按值內聯自己的結構體大小無限，所以**自我引用**不能按值存放。在默認配置  
（`NOLANG_FIELD_PTR` 關閉）下，結構體欄位預設就是按值，因此 `node` 要能參考自己，  
唯一的寫法就是明確要求指標：

```no
node {
    v i64
    next node #{inline=false}; 指標：這樣 node 才有有限大小
}

n node
n.v = 1
n.next.v = 2
print(n.next.v); 2
```

若把上面的 `#{inline=false}` 拿掉（或寫成 `#{inline=true}`），LLVM 會直接拒絕這個  
型別：`identified structure type 'node' is recursive`。

指標欄位**擁有**它的 pointee，生命週期由編譯器處理：

- pointee 在**首次寫入時惰性分配**（`@__nolang_get_<T>`，內含 `malloc` + 清零），  
  不是在建構宿主時一次分配；
- 宿主離開作用域時 `@__nolang_drop_<T>` 會**空指標檢查後釋放**它；
- 按值拷貝（`b = a`、作為參數傳遞、寫入容器）會**深拷貝** pointee，因此兩個宿主  
  不會共用同一塊記憶體、也不會被釋放兩次。

端到端測試見 `tests/field-inline-annotation.no`。

### 枚舉值引用

**規則：枚舉值必須使用 `枚舉類型.值` 的限定方式引用，不能直接使用裸值。**  
這樣可以防止命名衝突，也使外部包無法直接使用具體值。

```no
#{fmt-skip}
; ❌ 錯誤：直接使用裸值
kind = null
yes = e.is(io)
```
```

```no
; ✅ 正確：使用限定方式
kind = json-kind.null
yes = e.is(code.io)
```

> 枚舉類型可用於結構體字段類型、函數參數類型和返回值類型。  
> 定義枚舉的模組內部和外部都應使用 `枚舉類型.值` 的方式引用枚舉值。

## enter/leave

實現了 `enter` / `leave` 接口的類型，在作用域進入和離開的時候自動調用：

```no
file enter, leave {
    path str
}

file.enter = () {
    .open()
}

file.leave = () {
    .close()
}

read-file = () {

    ; 自動 f.enter()
    f = file {
        path: 'data.txt'
    }

    ; 使用 f
    ; 自動 f.leave()
    read(f)
}
```

### 可空類型(option)

在類型前面加 `?` 表示可空類型：

可空類型變量可以合法持有空值/错误值，編譯器會進行相應的空值檢查。

`?t` 是內建標籤列舉，其變體定義於 `src/std/global.no`：

```no
#{buildin}
option {
    ok(v t),
    nil,
    err(e err.err),
}
; tag 0：有值
; tag 1：空值
; tag 2：錯誤（std 結構化錯誤 err.err）
```

`err` 變體的載荷**不是 `str`**，而是 `std/err` 的 `err` 結構體（`code` / `msg` / `file` / `line` / `col`）。
`err('msg')` 是 `err.new('msg')` 的別名構造器，預設 `code.unknown`；可寫 `err('msg', code.io)` 指定錯誤碼；
位置欄位由編譯器在構造點自動注入。

`#{buildin}` 表示底層表示與構造由內建 runtime 提供（`%option` 型別、`ok(...)`/`nil`/`err(...)`  
構造器），`option` 的列舉聲明僅用於匹配與窮盡性檢查。詳見「標籤列舉」與「內建標籤列舉」。

```no
o ?i64
o = nil; 設為空
o = 42; 設為有值
o = err('msg'); 設為錯誤

nullableValue ?[]str
nullableString ?str

; 修改可空類型
nullableString = 'test'

; 設置錯誤
nullableString = err('some error')

; 可通過match判斷
x: {
    err -> log(it.msg()); it 是 err.err，訊息靠 .msg()

    nil -> log('nil')

    -> do-right-thing(it)
}

; 強制解包
; 取消實現
; !x.say()
```

### 風格指引：使用 ?t option 取代 (val, ok)

當函數可能失敗或返回空值時，**應優先使用 `?t` option 類型**，而非 `(val t, ok bool)` 雙返回值模式。

`?t` 是標籤列舉，有三種狀態：`ok`（有值）、`nil`（空值）、`err`（錯誤）。正常值會隱性綁定，當操作只是找不到值時用 `nil`，當操作遇到實際錯誤時用 `err(...)`。

```no
#{fmt-skip}
; ❌ 不推薦：雙返回值模式
stack.pop = () (val i64, ok bool) {
    .n == 0 -> return
    val = .data[.n]
    ok = true
}
```
```

```no
; ✅ 推薦：option 類型（nil 表示空，err 表示錯誤）
stack.pop = () (val ?i64) {
    .n == 0 -> {
        val = nil
        return
    }
    val = .data[.n]
}

; ✅ 返回錯誤
file.read = () (data ?str) {
    .fd < 0 -> {
        data = err('file not open')
        return
    }

    ; ... 讀取資料
    data = buf
}
```

使用 match 解包 option：

```no
val = s.pop()
val: {
    nil -> print('empty')

    err -> print(it.msg()); it = 結構化錯誤 err.err

    -> print(it); it = 彈出的值
}
```

## 整數運算溢出（`#{overflow}` 註解）

Nolang 不支援 panic——任何可能溢出的操作都必須以 `option` 或顯式註解處理，絕不會在執行期拋出整數溢位異常。下列**整數四則運算**的溢出行為由 `#{overflow = ...}` 註解控制：

- 有號與無號的 **`+` `-` `*`**：左右運算元為整數（含 `int` 字面量）時皆適用；
- 有號 **`/`**：僅 `INT_MIN / -1` 這一種情況會溢出（無號除法 `a/b ≤ a` 永不溢出，不適用）。

> 無號整數（`u8`/`u16`/`u32`/`u64`/`u128`）的 `+ - *` 與有號整數**遵循完全相同的規則**：未標註時同樣預設回傳 `option<int>`，溢出 → `err`。這是設計上的刻意選擇——「只要無法在編譯期證明不可能溢出，就按相同規則處理」。

### 特例：`byte` 與 `char` 算術

`byte` 與 `char` 是溢出語義的**特例**：它們的算術**不產生 option**、不受 `#{overflow}` 註解管轄。

- **`byte`（u8）**：`+ - *` 溢出時**靜默 2's 補數回繞**（`250 + 10` → `4`、`0 - 1` → `255`）。`byte` 面向字節緩衝、hex 編解碼等「天然容許回繞」的場景，與哈希／計數器的 `#{overflow = wrap}` 同一語義，只是內建、無需註解。
- **`char`（Unicode 碼點）**：`+ -` 是**碼點算術**，結果超出合法碼點範圍（`< 0` 或 `> 0x10FFFF`）時為 **`-1`**（無效碼點哨兵）——不會 `err`、也不回繞。

**設計理由**：`i8`–`i128` / `u8`–`u128` 一般用於**真實業務數值**（金額、計數、容量），靜默回繞會隱藏溢出 bug，因此默認 `err`、強制顯式選擇；而 `byte` / `char` 的典型用途（字節流、碼點遍歷）語義上天然容許（甚至期望）回繞，走 option 徒增解包噪音。若需要檢測 `byte` / `char` 運算的溢出，先轉寬型別（如 `b.to-i64()`）再計算。

| 模式          | 註解                       | 溢出時                         | 結果型別   | 適用場景               |
| ----------- | ------------------------ | --------------------------- | ------ | ------------------ |
| **默認（未標註）** | —                        | 溢出 → `err`，正常 → `ok(value)` | `?int` | 需要偵測溢出的運算          |
| **回繞**      | `#{overflow = wrap}`     | 靜默 2's 補數回繞                 | `?int` | 哈希、密碼學、計數器等語義上容許回繞 |
| **歸零**      | `#{overflow = clamp0}`   | 溢出（上溢或下溢）取 `0`              | `?int` | 差值不得為負的語義（如剩餘量）    |
| **最小值箝位**   | `#{overflow = min}`      | 溢出箝位到型別最小值                  | `?int` | 計數器下限、索引保護         |
| **最大值箝位**   | `#{overflow = max}`      | 溢出箝位到型別最大值                  | `?int` | 容量上限、飽和累加          |
| **飽和**      | `#{overflow = saturate}` | 上溢→最大值、下溢→最小值               | `?int` | 訊號/顏色飽和等           |

> ⚠️ **註解不解包**（D1）：`#{overflow = …}` **只決定「失敗時怎麼取值」**，
> 結果型別恆為 `?T`。要把結果拉回 plain，只能用 match 或 `?=` 消費。
> 顯式標註成 plain 型別（`w i64 = a - b  #{overflow=wrap}`）**不是**逃生口 ——
> 實測仍報 `15w45dqk`（cannot assign `?i8` value to `i8` variable）。

此外，所有模式都支援**型別前綴形式**，精確指定某個窄型別的飽和界限，例如 `#{overflow = u8-max}`、`#{overflow = i8-min}`、`#{overflow = u16-saturate}`。

> **註解粒度**：`#{overflow = ...}` 是**行注解**——只作用於**緊接其後的下一條陳述**。
> 這是**設計上唯一允許**的形式（防止濫用）：執行語意（codegen）與 `ovfhndld` 硬錯誤都只認它，
> 而且它只改「溢出時的取值」、不改型別，放寬到塊級或函式級會讓整片運算的語義由一行遠處
> 註解決定。理由詳見 [〈註解與註釋的作用域〉](annotation-scope.md) §4.1。
>
> ⛔ **放在函數定義上方、塊頭或檔案頭都不合法**。目前唯一的漏洞是：函式級註解會讓
> `ovf-int-default` lint 跳過整個函式（清單看起來「乾淨」），函式體內的未標註運算照舊
> 觸發 `ovfhndld`；塊級註解則讓 lint 向下繼承而閉嘴，codegen 一律不認。請逐陳述標註；
> `no fmt --fix=overflow` 產生的正是這種逐陳述形式。
>
> 完整模型（L0 模組 / L1 宣告 / L2 區塊 / L3 單行，以及哪些鍵支援哪幾層）見
> [〈註解與註釋的作用域〉](annotation-scope.md)；該頁 §7 列存量與漏洞，§8 是收緊順序。

> **LSP 快速修復**：編輯器（nolang-lsp）會對未標註的整數運算報出**提示**（`nolang-overflow`，trace id `ovfhndld`），並提供五個 quickfix——**Add `#{overflow = wrap}`** / **`clamp0`** / **`min`** / **`max`** / **`saturate`**——自動在該運算所在語句上方、以與該行一致的縮排插入對應註解，指定溢出行為（結果仍是 `?T`，需另用 match / `?=` 消費）。命令列等價物為 `no fmt --fix=overflow`（見下文）。

### 默認：返回 `option<int>`

未標註時，`a - b` 的結果型別是 `option<int>`。接收端必須是 `?T`（可空型別），並用 match 解構 `err` / `nil` / `ok` 三個分支：

```nolang
main = () {
    x i64 = 100
    d ?i64 = x - 1; 默認：返回 option<i64>
    d: {
        err -> print(-1); 溢出（如 x - 2^63 下溢）

        nil -> print(0)

        -> print(1); ok(value)：正常結果
    }
}
main()
```

> `%option` 的 `data` 欄為 `i64`；窄型別（`i8`/`i16`/`i32`）的結果會先符號擴展（sext）再存入，解包時再截斷回原寬度。

### wrap：靜默回繞

```nolang
#{overflow=wrap}
sub-wrap = (a i64, b i64) (r i64) {
    r = a - b; 溢出時靜默 2's 補數回繞；r 是 plain i64（顯式型別）
}
```

語句級：

```nolang
x i64 = -9223372036854775807

#{overflow=wrap}
w i64 = x - 2; 下溢回繞 → 9223372036854775807
print(w)
```

### clamp0：溢出歸零

```nolang
x i64 = -9223372036854775807

#{overflow=clamp0}
c i64 = x - 2; 下溢 → 0（上溢同樣歸零）
print(c)
```

### min / max / saturate：飽和箝位

三種模式把溢出結果「夾」在型別可表示區間內，而非回繞或歸零：

```nolang
#{overflow=min}
floor = (a i64, b i64) (r i64) {
    r = a - b; 下溢 → i64 最小值；上溢 → i64 最大值
}

#{overflow=max}
cap = (a i64, b i64) (r i64) {
    r = a * b; 上溢 → i64 最大值；下溢 → i64 最小值
}

#{overflow=saturate}
sat = (a i64, b i64) (r i64) {
    r = a + b; 上溢→max、下溢→min
}
```

型別前綴形式可精確控制窄型別的飽和界限（函數級註解會套用到體內所有對應型別運算）：

```nolang
#{overflow=max}
inc = (x u8) (r u8) {
    r = x + 1; x = 255 時上溢 → 255（而非回繞成 0）
}

#{overflow=min}
dec = (x i8) (r i8) {
    r = x - 1; x = -128 時下溢 → -128（而非回繞成 127）
}
```

### 編譯器強制

未標註的整數運算預設回傳 `option<int>`，因此「未處理」會被強制攔下。**是否算未處理採並行判斷**——滿足下列任一項即不報錯：

- **`?=` 上拋**（要求函式有 option 結果參數）；
- **就地捕獲**：用 `=` 綁定到一個**新**變數（變數自動推斷為 `?T`），或顯式宣告 `?T`（如 `x ?i64 = a + b`）；
- **`_ = expr` 顯式丟棄**（求值本身走安全路徑，錯誤一併忽略）；
- **加註解** `#{overflow = wrap}` / `clamp0` / `min` / `max` / `saturate`（或型別前綴形式如 `u8-max`、`i8-min`），聲明接受回繞／飽和語義。

四者皆無時才報錯（`ovfhndld`）。

> **純算術不是可錯源**：`+ - * <<` 與取負本身不觸發捕獲（否則所有算術都會變成 option，標準庫爆炸）。所以 `d = x - 1`（不含 `/` `%`、不含安全索引、不含 option 運算元）仍然報錯——請加註解，或寫成 `d ?i64 = x - 1`。
>
> **既有變數不改型別**：捕獲只對「本陳述首次宣告」的目標生效。若變數已經存在（例如先有 `d = 0` 再 `d = x - 1`，或先有 `v i64 = 0` 再 `v = arr[i]`），編譯器不會把它就地改成 option，而是報錯要求你顯式選擇語義。

另有兩條硬性錯誤不受上述豁免影響：

- 未標註的整數運算若賦值給**顯式 plain `int`** 變數，編譯器報錯（`cannot assign ?i64 value to i64 variable`），強制你加 `#{overflow = wrap}` / `#{overflow = clamp0}` / `#{overflow = min}` / `#{overflow = max}` / `#{overflow = saturate}`（或型別前綴形式）或改用具名 `?T`。
- 未標註的整數運算若賦值給**已存在**、且型別非 option 的變數（`d = 0` 之後的 `d = x - 1`），編譯器報錯並提示加上註解或宣告 `?i64`，避免產生非法 IR。

其餘規則：

- 無號整數運算（`u8`/`u16`/`u32`/`u64`/`u128`）**同樣適用**此機制：未標註時 `+ - *` 預設回傳 `option<int>`，溢出 → `err`，與有號整數規則一致。
- `i128` 運算不支援 `option` 路徑（data 欄僅 `i64`），統一退化為靜默回繞以保留正確性。

> **為什麼默認是 option 而非 wrap？** 靜默回繞會隱藏溢出 bug；預設回傳 `option` 強制呼叫方顯式處理溢出（或主動標註 `wrap` 表明「我接受回繞語義」），把「是否容許溢出」變成可見的設計決策。

### 除零（division by zero）

整數 `/` 與 `%` 的除數為 `0` 是**未定義行為（UB）**，與溢出是**兩件獨立的事**——沒有任何註解能豁免它：

- **編譯期可判定**：除數是**字面常數 0**（`10 / 0`、`a % 0`、`x / 0`）⇒ **硬錯誤**（trace id `div-by-zero`）。`#{overflow = wrap}` 不豁免它（該註解管的是溢出，不是除零）。
- **執行期才知道**：除數來自變數／外部輸入 ⇒ 編譯器在**兩條路徑上都插入零檢查**，差別只在「怎麼報告」：

| 結果型別        | 觸發條件                    | 除數為 0 時                                                                        |
| ----------- | ----------------------- | ------------------------------------------------------------------------------ |
| `?int`（預設）  | 陳述未標註                   | option tag = `err`，用 `?=` / match 處理                                           |
| plain `int` | 陳述帶 `#{overflow = ...}` | 結果強制為 `0`，並在 **stderr** 印出 `nolang: integer division or remainder by zero ...` |

```nolang
safe = (a i64, b i64) (r ?i64) {
    r = a / b; b == 0 → err（呼叫方必須處理）
}

#{overflow=wrap}
fast = (a i64, b i64) (r i64) {
    r = a / b; b == 0 → 0 並印一行 stderr 診斷（不再是裸 sdiv 的 UB）
}
```

> **為什麼 `#{overflow = wrap}` 不能豁免零除守衛**：它改變的是**溢出行為**（結果型別仍恆為 `?int`），不是除數的安全性。若讓它豁免，編譯器就會發出裸 `sdiv`，最佳化器便有權假設除數永遠非 0（把 `a / 0` 折成任意值），而 x86-64 的 `idiv` 直接以 #DE 崩潰——既 UB、又沒有任何診斷。因此 zero-divisor 守衛**獨立於註解**存在；plain 路徑之所以多印一行 stderr，正是因為該路徑沒有 option 可供呼叫方檢查。
>
> **同一守衛也覆蓋 `sdiv (INT_MIN, -1)`**（結果不可表示，同屬 UB；x86-64 上會 SIGFPE）。option 路徑把它併入 `err`；plain 路徑取 2 補數回繞值（`INT_MIN / -1 → INT_MIN`、`INT_MIN % -1 → 0`）。

浮點除法不受管轄：`1.0 / 0.0` 由 IEEE-754 定義為 `inf` / `nan`，不是 UB。

### 未標註運算列為錯誤（`ovf-int-default`）

`no vet` 把**未標註**的整數運算回報為**提示**（trace id `ovfhndld`，HINT）。原因：預設的 `?T` 對使用者不可見，必須讓他知情——可加 `#{overflow = ...}` 指定溢出行為，或用 `?T` + `?=` / match / 顯式 plain 型別消費。真正該阻斷建置的是 `optunuse`（option 既沒上拋也沒解包）。

### 自動修復：`no fmt --fix=overflow`

批量補註解不必手寫，用 `no fmt` 的修復類別參數：

```bash
no fmt --fix=overflow -w src/std          # 就地修復整個目錄
no fmt --fix=overflow -d src/std/str.no   # 只印 diff，不寫檔
no fmt --fix=overflow src/std/str.no      # 印出修復後的內容
```

修復是**精準且不污染**的：只在 lint 實際報告的那條運算所在陳述上方插入 `#{overflow=wrap}`，不改動其他陳述、不重排版、不動既有註解，且重跑冪等（已標註者不再變動）。這與直接跑 `no fmt -w` 不同——後者是完整重新格式化。

> 修復後請再跑一次 `no vet` 確認歸零。絕大多數情況一次即清空（`src/std` 由 4728 條降為 0）。已知的唯一例外形狀：報告落在 match arm 的**條件**上，而該 arm 本體沒有任何可標註陳述——行注解只作用於其後的第一條陳述，涵蓋不到 arm 自己的條件運算，此時 `no vet` 會再次報出。這種情況請先把該運算抽成獨立陳述再標註。

### 自動修復：冗餘型別標註

當變量聲明的**類型標註與右側值推斷出的類型完全相同**時，該標註是冗餘的（`no vet` 會以 hint `tcpoxtfd`、`type annotation '<T>' can be omitted (inferred from value)` 報出）。

- **預設 `no fmt` 就會自動去除**：`no fmt -w` 在格式化時一併移除冗餘標註（同時做正常重新格式化），`no fmt -d` 預覽含此變更，`-w` 寫入。

```bash
no fmt -w main.no          # 格式化並去掉冗餘標註
no fmt -d main.no          # 只印 diff（含標註移除）
no fmt -w src/             # 整個目錄
```

- **`--fix=redundant`（手術式、只動標註）**：若只想移除冗餘標註、不要完整重新格式化（例如對 `src/std`，因 `no fmt` 會丟掉 `#{index-out}`），用此類別——它只刪標註、不重排版：

```bash
no fmt --fix=redundant -w src/std          # 就地移除整個目錄的冗餘標註（不重排版）
no fmt --fix=redundant -d src/std/str.no   # 只印 diff，不寫檔
no fmt --fix=redundant src/std/str.no      # 印出修復後的內容
```

```nolang
; 修復前
x str = ''
b i64 = 5
m [str]i64 = make-map()

; 修復後（標註 == 推斷型別，去掉）
x = ''
b = 5
m = make-map()
```

規則要點：

- **標註型別 == 推斷型別**才移除（如 `str`/`i64`/`[str]i64` 與其推斷值一致）。
- **可隱式轉換但不相等**的情況**不會**移除——保留標註才有轉換語意。例如 `c i16 = 5`（`5` 推斷為 `i64`，與 `i16` 不等）、`p ?i64 = nil`、`last txt = ''`（`txt` 與 `str` 可隱式互轉但型別不同）。
- **無型別標註**的寫法（如 `d = 'x'`）本就不會被報，自然也不動。
- **定長陣列標註 `[N]T` / `[?]T` 一律保留**（永不視為冗餘）。`[N]` 不是單純的型別提示，而是決定**表示形式**的開關：寫了它，右側的 `[...]` 字面量才會被解析成固定長度的棧陣列；寫了它，推斷結果才會「剛好等於」標註——這是標註自己造出來的循環論證。刪掉後同一行重解析就變成堆上可增長的切片 `[]T`，語意隨之改變：`fixed [3]char = ["x", "y", "z"]` 的 `.push()` 無效（`.len()` 恆為 3），退化成 `fixed = ["x", "y", "z"]` 後 `.push()` 會讓 `.len()` 變 4。切片標註 `[]T` 不在其列——它刪掉後仍是切片，屬真冗餘。
- **含十六進位字面量的值一律保留標註**（保守例外）。`no vet` 的型別推斷對 `0xNN` 採用「推斷為 `byte`」的啟發式，與編譯器「無標註時整数字面量預設 `i64`」的實際語義不同，因此 `IP-TBL [64]byte = [0x3a, ...]`、`s []byte = [0x50, 0x4b]` 會被誤判為冗餘；一旦移除，字面量會退化成 `i64` 元素而產生 `expected '[]byte', got '[]i64'` 之類的型別錯誤。故只要值運算式文本中出現 `0x`/`0X`，hint 與移除都不會發生。

不論走預設 `no fmt` 還是 `--fix=redundant`，移除都只刪除被報告的那個型別標註及其前導空白、重跑冪等（已移除者不再變動）。修復後建議再跑一次 `no vet` 確認 `tcpoxtfd` 歸零。

### 自動修復：消費 option（`--fix=unwrap`）

統一規則下「產生 option 卻從未消費」是唯一該阻斷建置的診斷（trace `optunuse`）。批量把它改成顯式消費形式，用 `--fix=unwrap`：

```bash
no fmt --fix=unwrap -w src/std            # 就地修復整個目錄
no fmt --fix=unwrap -d src/std/str.no     # 只印 diff，不寫檔
no fmt --fix=unwrap src/std/str.no        # 印出修復後的內容
```

修復形態（見〈安全索引〉）：**所在函式已有 `?T` 結果參數** → 上拋（`x ?= <expr>`）；否則 → 改名 + match 解包：

```nolang
; 修復前
x = a + b

; 修復後（所在函式有 ?T 結果參數）
x ?= a + b

; 修復後（否則，4 行標準形；空兜底臂 = 零值）
x-opt = a + b
x = x-opt: {
    ok -> it

    ->
}
```

與其他 fix 類別一樣：手術式文字替換、不改排版、不動陳述上方的 `#{overflow=…}` / `#{index-out=…}` 註解（它們仍負責「失敗時怎麼取值」），重跑冪等。修復後請再跑一次 `no vet` 確認 `optunuse` 歸零。

### 註解綁定與單行合併

`#{...}` 註解**綁定到節點**，因此同一條陳述可同時帶多個鍵；多個註解一律**合併成單行**：

```nolang
#{fmt-skip}
i <- [0..16): {

    #{index-out=0, overflow=wrap}
    buf[base + i] = data[i]
}
```

上面這一條同時需要「越界取默認值」（`index-out`）與「溢出回繞」（`overflow`）兩個語意。分成兩行寫解析結果相同，但 `no fmt --fix=overflow` 產出合併後的單行形式。

> **行注解的作用範圍**：`#{overflow = ...}` 只作用於**緊接其後的下一條陳述**。中間若夾著無關的註解仍會正確套用到該陳述。安全索引寫入（`arr[base + i] = v`、`.buf[.pos + i] = v`）會被 desugar 成 match 形式，`#{overflow = wrap}` 會一併帶到 desugar 之後的節點，故索引算式同樣受註解管轄，不會被誤報為未標註。

### 錯誤上拋（`?=` 運算子）

`?=` 是 option 解包 + 向上拋錯誤的**簡便寫法**。**函數回傳 option 並非必要條件**：下列兩種情形居其一即可，
因此回傳值不是 option 的函式只要用塊 match 包住主體，照樣能寫 `y ?= x + 1`。

**語法：**

```no
v ?= expr
```

**語義：**

- 如果 `expr` 返回 `ok(value)`，則 `v` 被賦值為解包後的內部值（自動解包）
- 如果 `expr` 返回 `nil` 或 `err`：
  - 函式有 `?T` 結果參數時 → 把該 option 寫入結果參數並提前 `return`（自動上拋）；
  - 陳述位於**陳述層級塊 match** `{ ... }: { ... }` 主體內時 → 由**最近的**塊 match 捕捉，
    **整個主體停止**、arms 只跑一次（**不是** `return`），函式無需 `?T` 結果參數。

**可用條件（二者其一即可）：**

1. 所在函式具有 option 類型結果參數；
2. 陳述位於陳述層級塊 match `{ ... }: { ... }` 的主體內（見〈Match 主語的三種形態〉）。

兩者皆非時編譯器會報錯。`err -> _` / `-> _` 兩臂可把上拋的錯誤就地吞掉，是「無 `?T` 結果參數
也想用簡寫」的標準兜底：

```no
handle = (x i64) {
    {
        y ?= x + 1
        print(1)
    }: {
        err -> _

        -> _
    }
}
```

**結果參數無需預先賦值：** 函式 prologue 不做零初始化，但 return 時未顯式賦值的輸出參數會被自動零填充（整數 → `0`、`str` / 結構體 → 零值、option → `nil`），因此示例裡不寫 `result = nil` 樣板，只寫成功路徑的賦值即可。

**示例：**

```no
; 逐行讀取檔案並處理 — 使用 ?= 簡化錯誤傳播
process-file = (path str) (result ?str) {
    f ?= open(path); 失敗時自動 result = f; return
    line ?= f.read-line(); EOF 或錯誤時自動上拋
    result = line
}
```

等價的展開形式（編譯器自動生成的 match 鏈）：

```no
#{fmt-skip}
process-file = (path str) (result ?str) {
    result = nil
    __tmp = open(path)
    __tmp: {
        nil || err -> {
            result = __tmp
            return
        }
        -> f = it
    }
    __tmp2 = f.read-line()
    __tmp2: {
        nil || err -> {
            result = __tmp2
            return
        }
        -> line = it
    }
    result = line
}
```

**鏈式使用：** 多個 `?=` 可以串聯使用，實現管道式錯誤傳播：

```no
pipeline = (input str) (result ?str) {
    a ?= step1(input); 失敗則上拋
    b ?= step2(a); 失敗則上拋
    result = b
}
```

**裸算術運算：** `?=` 的右側不限於函數呼叫——任何回傳 `option` 的整數運算（如未標註的 `a - b`、`a + b`、`a * b`）都可以直接用 `?=` 上拋溢出錯誤，無需先綁定到中間變數：

```no
; 溢出時自動 result = err; return，正常時 v 解包為內部值
sub-safe = (a i64, b i64) (result ?i64) {
    v ?= a - b; 下溢/上溢 → err 自動上拋；否則 v = 內部 i64
    result = v
}

add-safe = (a i64, b i64) (result ?i64) {
    v ?= a + b; 同上，適用於 + * 與有號 /
    result = v
}
```

等價展開（編譯器自動生成）：

```no
#{fmt-skip}
sub-safe = (a i64, b i64) (result ?i64) {
    result = nil
    __unwrap = a - b            ; option<i64>
    __unwrap: {
        nil -> {
            result = __unwrap
            return
        }
        err -> {
            result = __unwrap
            return
        }
        -> v = it               ; ok(value)：解包內部值
    }
    result = v
}
```

**複合右側（compound RHS）：** `?=` 的右側可以是一個含多個 option 運算元的複合運算式。每個 option 運算元會被自動解包，並注入傳播守衛——只要任一運算元為 `nil` / `err`，當前函數的 option 結果參數即被設為該值並 `return`（向上拋）；全部成功才把內部值賦給左值：

```no
sum3 = () (result ?i64) {
    a ?i64 = ok(10)
    b ?i64 = ok(20)
    c ?i64 = ok(30)
    total ?= a + b + c; 每個 option 運算元自動解包並傳播
    result = total
}

comp = () (result ?i64) {
    a ?i64 = ok(4)
    b ?i64 = ok(6)
    v ?= dbl(a + b); a + b 可能是 option，同樣自動解包並傳播
    result = v
}
```

> 只有「運算元位置」上的 option 會被自動解包（如 `a + b + c`、`f(b + c)`）；作為函式引數直接傳入的 option 變數不在此列——它會走 `?=` 既有的裸 option 引數豁免（`a ?= f(v)`，其中 `v` 為 `?i64`、`f` 接收 plain `i64`）。

**非 option 右側報錯：** 為了避免大範圍地把 `=` 誤寫成 `?=`，當右側在編譯期可被確定為「明確非 option」時，編譯器會報錯並提示改用 `=`：

- 字面量：`v ?= 42`
- 常數可折疊的整數運算：`v ?= 10 + 20`
- 已知型別且非 option 的區域變數：`v ?= x`（其中 `x i64 = 5`）

報錯訊息形如：`` `?=` RHS `42` is not an option — use `=` instead ``。

> 算術運算只要任一運算元是 option（或型別未知 / 為函式呼叫，可能回傳 option），即視為「可能 option」而放行，不會誤報——例如 `v ?= a + b + c`（a/b/c 為 `?i64`）或 `v ?= b + c`（b/c 未知型別）都可正常編譯。

**適用場景：**

- `pop` / `peek` 等可能為空的容器操作 → `?t`（`nil` = 空）
- `read-line` / `read-byte` 等 I/O 操作 → `?str` / `?i64`（`nil` = EOF，`err` = 錯誤）
- `lookup` / `get` 等查找操作 → `?t`（`nil` = 未找到）
- `parse` / `from-str` 等解析操作 → `?t`（`nil` = 空，`err` = 無效輸入）
- `accept` / `dial` 等網路連接 → `?conn`（`nil` = 無連接，`err` = 錯誤）

**nil vs err：** 當缺失是正常/預期的結果（空堆疊、鍵不存在、EOF）時用 `nil`；當缺失代表實際的錯誤狀態（I/O 失敗、輸入無效、連接被拒）時用 `err('msg')`。

**例外：** 當函數需要返回多個獨立值（如 `(name str, value str, ok bool)`）時，可保留多返回值模式。

**多返回值最多只能有一個 `?T`（硬錯誤）：** 函式宣告的返回參數中，`?T` 至多一個。`?T` 是**單一**的錯誤通道——函式體內的 `?=` 提前返回時，只能把失敗放進那個唯一的 option 返回參數；若同時宣告兩個 `?T`，編譯器只填得上第一個，呼叫方會拿到半填的 tuple（`(err, nil)`），且無從判斷哪個值代表失敗：

```no
#{fmt-skip}
pair = () (a ?i64, b ?i64) {; 錯誤：兩個 ?T 返回參數
    a = ok(1)
    v ?= boom(); boom() 回 err('boom')
    a = v
    b = ok(2); 永不執行 —— 呼叫方看到 (err, nil)
}
```
```

正確寫法是把獨立值宣告成 plain 型別，只留一個 `?T` 當錯誤通道：

```no
lookup = (k str) (value str, found bool, err ?str) {
    value = ''
    found = false
    err = nil
}
```

報錯訊息形如：`` function 'pair' declares 2 option (?T) results ('a', 'b'); only one result may be ?T … [moptres1] ``。方法定義的隱式 `self` 接收者是呼叫方別名的出參，不佔用這個名額；`#{c}` extern 宣告同樣受限。

### 錯誤捕獲賦值（`=` 就地捕獲）

`?=` 的語義是「錯誤向上拋」——它要求所在函式有 option 結果參數，失敗時提前 `return`。當你不希望提前返回、而是想在原地處理錯誤時，用普通 `=`：只要右側含**可錯源**，目標變數就會自動推斷為 `?T`，`nil` / `err` **就地存進該變數**，流程繼續。

```no
handle = (b i64, c i64, d i64) (r i64) {
    a = b + c / d; a 推斷為 ?i64；除零 → a = err
    a: {
        err -> {
            r = -1; 就地處理錯誤
        }

        nil -> {
            r = -1
        }

        -> {
            r = it; 成功：r = b + c/d
        }
    }
}
```

**可錯源（捕獲觸發源）：**

| 來源                    | 例                                   | 捕獲結果                          |
| --------------------- | ----------------------------------- | ----------------------------- |
| 整數除法 / 取模             | `a = b + c / d`、`a = b % c`         | 除零、`MIN / -1` → `err`         |
| 安全索引                  | `a = v[i]`（`arr` / `vec` / `slice`） | 越界 → `nil`                    |
| option 型運算元           | `a = x + 1`（`x ?i64`）               | `x` 為 `nil` / `err` 時原樣存入 `a` |
| 回傳 option 的呼叫（作算術運算元） | `a = f(x) + 1`（`f` 回傳 `?i64`）       | 同上                            |

> 作為函式引數**直接**傳入的 option 不在此列——那交給被呼叫方處理（與 `?=` 的裸 option 引數豁免一致）。

**純算術不是可錯源**：`+ - * <<` 與取負本身不觸發捕獲（否則所有算術都會變成 option）。但一旦落在 option 路徑上（例如右側另有 `/`，或目標顯式宣告 `?T`），這些運算會帶**運行期溢出檢查**，溢出 → `err`：

```no
ovf = (x i64) (r i64) {
    a ?i64 = x + 1; 顯式 ?T → 走 option 包裝路徑
    a: {
        err -> {
            r = 0; x = i64-max 時溢出 → err
        }

        nil -> {
            r = 0
        }

        -> {
            r = it
        }
    }
}
```

**捕獲 vs 上拋：**

| 寫法            | 失敗時                                 | 需要 option 結果參數 | 之後如何取值             |
| ------------- | ----------------------------------- | -------------- | ------------------ |
| `a ?= expr`   | 設結果參數為 `nil`/`err` 並 `return`（提前返回） | 是              | 不適用（已返回）           |
| `a = expr`    | `a` 就地存 `nil`/`err`，流程繼續            | 否              | `a: { ok -> ... }` |
| `a ?T = expr` | 同 `=`（顯式標註，含內部可錯子表達式）               | 否              | 同上                 |
| `_ = expr`    | 求值但丟棄結果與錯誤（不報錯、不觸發 unused lint）     | 否              | 不適用                |

**適用範圍：** 安全索引的捕獲與 `?=` 一樣，**只在回傳 `?T` 結果的函式內生效**（見〈安全索引〉第 3 種寫法）。`/` `%` 與 option 運算元的捕獲則適用於所有函式。

**既有變數不改型別：** 捕獲只對「本陳述首次宣告」的目標生效。若目標已經存在（`v i64 = 0` 之後的 `v = arr[i]`、或 `d = 0` 之後的 `d = x - 1`），編譯器不會就地改變它的型別，而是報錯要求顯式選擇語義（加 `#{overflow=...}` / `#{index-out=...}` 註解、用 `?=`、或宣告 `?T`）。

### 泛形

```no
arr_to_vec = (arr [n]t) (out []t) {
    i <- [0..n): {
        out[i] = arr[i]
    }
}
```

### 類型強制轉換

```no
; 返回類型名稱字符串
a = typeof(x)

; `as` 僅允許用於 FFI 指標型別轉換（如 *byte、**byte、*i64）
; 整數內部皆為 i64，無需顯式轉換
y = x as *byte
```

### 整數賦值型別檢查

編譯器會對整數賦值進行型別檢查，防止不安全的窄化（narrowing）導致資料遺失。以下是規則：

#### 隱式拓寬（安全，自動允許）

窄整數型別的值可以自動賦值給更寬的型別，因為目標型別的範圍完全包含來源型別：

```no
b byte = 200
i i64 = b; ✓ byte 範圍 [0,255] ⊆ i64 範圍
u u32 = b; ✓ byte 範圍 ⊆ u32 範圍
```

#### 整數字面量賦值

整數字面量（預設推斷為 `i64`）可以賦值給任何範圍包含該值的整數型別：

```no
#{fmt-skip}
n u8 = 200; ✓ 200 ∈ [0,255]
m u8 = 300; ✗ 300 > 255，編譯錯誤
big u64 = 18446744073709551615; ✓ 2^64-1，u64 最大值
```
```

#### 不安全的窄化（編譯錯誤）

將寬型別的變數直接賦值給窄型別會導致編譯錯誤，因為可能造成資料遺失。錯誤訊息會附帶**可操作的修復提示**，建議如何用位元運算安全窄化：

```no
#{fmt-skip}
d u64 = 42
h u32 = d; ✗ cannot assign u64 value to u32 variable 'h'; hint: narrow safely with a bitwise mask (e.g. `& 4294967295`) or right shift (e.g. `>> 32`)
h u16 = d; ✗ cannot assign u64 value to u16 variable 'h'; hint: narrow safely with a bitwise mask (e.g. `& 65535`) or right shift (e.g. `>> 48`)
h u8 = d; ✗ cannot assign u64 value to u8 variable 'h'; hint: narrow safely with a bitwise mask (e.g. `& 255`) or right shift (e.g. `>> 56`)
x u32 = d + 1; ✗ 加法結果仍為 u64，不安全
y u32 = foo(); ✗ 函式呼叫結果型別不匹配
```
```

> **修復提示**：編譯器會根據目標型別自動計算精確的 mask 值和位移量。按照提示套用 mask 或位移後，即可安全窄化（見下節）。
>
> **有號目標型別**：對 `i8`/`i16`/`i32`/`i64` 等有號整數，提示會說明位元窄化不適用（符號位元截斷語義不明確），建議改用顯式範圍檢查。

#### 位元運算安全窄化（自動允許）

當賦值的右側是**位元運算表達式**（`&`、`|`、`^`、`<<`、`>>`）且目標型別為**無號整數**（`u8`/`u16`/`u32`/`u64`/`byte`）時，編譯器允許隱式窄化——因為截斷高位是位元操作的標準語義，不會造成非預期的資料遺失：

```no
d u64 = 42

; ✓ mask 運算：結果必 ≤ mask 值，安全落入 u32
h u32 = d & 67108863; mask = 2^26-1 < 2^32
h u32 = d & 4294967295; mask = 2^32-1，正好 u32 範圍

; ✓ 位移運算：右移後高位為 0，安全
hi u32 = d >> 32; u64 >> 32 留 32 bits

; ✓ XOR / OR 組合
c u32 = a ^ b; 位元運算結果
b byte = v & 255; mask 到 byte 範圍

; ✓ 複合位元運算（常見於密碼學/編解碼）
s u32 = (key[0] & 255) | ((key[1] & 255) << 8) | ((key[2] & 255) << 16) | ((key[3] & 255) << 24)
```

> **為什麼允許？** 位元運算（mask、位移、XOR、OR）的語義就是構造一個位元模式。賦值給窄的無號型別時，截斷高位是刻意的操作——開發者已透過 mask 或位移保證了結果的範圍，或刻意丟棄高位。這是密碼學（如 ChaCha20、Poly1305、Blake2）和編解碼代碼中的標準模式。

> **僅限無號目標型別。** 對有號整數目標（`i8`/`i16`/`i32`/`i64`），即使右側是位元運算也會報錯，因為符號位元的截斷語義不明確：
>
> ```no
> d u64 = 42
> h i32 = d & 4294967295   ; ✗ 仍報錯：有號目標不適用安全窄化
> ```

> **頂層必須是位元運算。** 只有當表達式的頂層運算子是 `&`/`|`/`^`/`<<`/`>>` 時才放行。加法、減法、函式呼叫、直接變數引用等不在此列：
>
> ```no
> d u64 = 42
> h u32 = d              ; ✗ 頂層是 Identifier，不是位元運算
> h u32 = d + 1          ; ✗ 頂層是 +，不是位元運算
> ```

### 模块系统

- 每个文件就是一个模块
- 文件名和文件夹名使用中连接符

```shell
utils/
└── helper.no    ; 模块名为 utils/helper
```

### 導入模塊

> **新式語法使用 `#` 導入。舊式 `use` 關鍵字仍可使用，但已廢棄（deprecated），建議改用 `#`。**

```no
; 標準庫（新式語法，推薦）
# std/math.add
; 遠程模塊（非std/開頭）
# github.com/utils/math.add
; 本地模塊，必須/開頭
# /utils/math.add
; 別名
# std/math.add a

; ── 以下為舊式語法（deprecated，仍可使用但不推薦）──
; use std/math.add
; use github.com/utils/math.add
; use /utils/math.add
; use std/math.add a
```

#### 直接引入 `.noml` 設定檔

`.noml` 檔案可以用 `#` 直接引入：解析順序先找同名 `.no`，找不到再找 `.noml`。引入後
**結構體名預設就是檔名**（去掉副檔名，可由檔案頂部的 `#{name = '...'}` 註解改寫），
各成員成為該結構體的欄位；同時生成一個同名的全局值承載檔案內容，因此用
`server.port` 讀取設定。

```noml
; cfg/server.noml
{
  host: 'localhost'
  port: 8080
  tags: [] #{type = '[]str'}
  compiler: {
    version: 'v0.1.0'
  }
}
```

```no
# /cfg/server

print(server.port); 8080
print(server.compiler.version); v0.1.0
```

- 欄位型別優先取成員的 `#{type = '...'}` 註解（值是完整的 nolang 型別表達式，如
  `'[]str'`、`'[]i64'`、`'?i64'`）；未標註時由值推斷：字串 → `str`、整數 → `i64`、
  小數 → `f64`、布林 → `bool`、物件 → 自動生成的巢狀結構體（`server-compiler`）、
  陣列 → `[]元素型別`。
- 空容器（`[]`、`{}`）**必須**寫 `#{type = '...'}`：noml 的值本身沒有型別，空容器
  無法推斷。物件成員標註 `#{type = 'x'}` 時直接使用 `x`（需自行定義），不再自動生成
  巢狀結構體。
- 巢狀物件的結構體名以檔名為前綴（`server-compiler`）；定義順序先巢狀後宿主。
- **頂部（文件級）註解可改寫結構體名**：`#{name = '...'}`（等價寫法 `#{type = '...'}`，
  `name` 優先）寫在頂層 `{` 上方的獨立一行（或頂層值同一行尾隨）。改寫後值綁定名與
  巢狀結構體前綴一併跟隨，但**導入路徑仍按檔名解析**：

```noml
; cfg/db.noml
#{name = 'db-options'}
{
  host: 'localhost'
}
```

```no
# /cfg/db
# /cfg/db as db2

print(db-options.host); localhost
print(db2.host); localhost
```

- 兩個不同檔案生成同一型別名時（例如不同目錄下的同名檔案）會報錯，用頂部
  `#{name = '...'}` 區分即可；同一檔案被多次導入（例如再加 `as` 別名）只合併一份型別定義。
- 鍵名按 nolang 規則轉成中連接符小寫（`anonymous-fn-type`）：數字開頭加 `f-`，與
  關鍵字同名的加 `x-`。
- 允許 `as` 別名改寫值綁定名，結構體名不變：`# /cfg/server as cfg`。
- `.noml` 型別不參與模組前綴：即使檔名與標準庫模組同名（如 `server`），型別仍是裸名。
- `package.noml` / `workspace.noml` 同樣可寫 `#{...}` 註解（如 `#{type = '[]str'}`）：
  解析時忽略，只供外部工具重建強型別結構；語法與 nolang 的 `#{...}` 完全一致。

### 導出模塊

僅適用於lib.no

```no
@ std/math.add a
```

### FFI（`#{c}` 註解）

透過 `#{c}` 註解宣告外部 C 函式，實現 FFI（Foreign Function Interface）。

**語法**：`#{c}` 獨立一行，標記下一行為 FFI 宣告。`#{c}` 是註解系統的 FFI 語言鍵，也支援 `#{cpp}`、`#{rust}` 等其他語言。舊語法 `#c` 仍向後相容。

**私有宣告**：名稱以 `_` 開頭表示私有（不導出），C ABI 符號自動去除前綴 `_` 並將連字號轉為底線。

**不再需要分開檔案**：FFI 宣告與普通代碼可寫在同一個 `.no` 檔案中。

**指針型別語法**：FFI 中使用 C 風格的 `*T`、`**T`、`***T` 表示指針，必須有具體型別 `T`。普通代碼不能使用此語法。

| 語法        | 含義          | LLVM IR | 用途                  |
| --------- | ----------- | ------- | ------------------- |
| `*byte`   | 指向 byte 的指針 | `i8*`   | 不透明指標（如 db handle）  |
| `**byte`  | 雙重指針        | `i8**`  | 輸出參數（如 `sqlite3**`） |
| `***byte` | 三重指針        | `i8***` | 極少見的三重間接            |

```no
; sqlite.no — FFI 綁定與安全包裝在同一檔案
; 編譯器自動將連字號 (-) 轉為底線 (_) 以匹配 C ABI 符號
; 以 _ 開頭的名稱為私有，C ABI 符號自動去除前綴 _

; 基本型別參數
#{c}
c-strlen = (s str) (n i64)

; 指針參數（*byte = 不透明指標），私有宣告
#{c}
_sqlite3-close = (db *byte) (rc i32)

; 雙重指針（**byte = 輸出參數，呼叫後值自動存回變數），私有宣告
#{c}
_sqlite3-open = (filename str, db **byte) (rc i32)

; 多個指針參數，私有宣告
#{c}
_sqlite3-exec = (db *byte, sql str, callback *byte, arg *byte, errmsg *byte) (rc i32)
```

```no
; 同一檔案中的安全包裝

open = (dsn str) (d db-sqlite) {
    handle i64 = 0
    rc i32 = _sqlite3-open(dsn, handle)
    rc != SQLITE-OK -> {
        return
    }
    d.handle = handle
}
```

**規則：**

1. `#{c}` 獨立一行，標記下一行為 FFI 宣告（舊語法 `#c` 仍向後相容）
2. FFI 僅為宣告，無函式主體
3. 指針必須有具體型別（如 `*byte`），不允許裸 `ptr`
4. `**byte` 用於輸出參數：呼叫後 C 函式寫入的指針值會自動轉為 `i64` 存回呼叫端變數
5. 所有指針在 Nolang 端以 `i64` 儲存（`ptrtoint`）
6. `str` 型別參數自動轉為 null-terminated `i8*`
7. 名稱以 `_` 開頭表示私有（不導出），C ABI 符號去除前綴 `_`
8. FFI 宣告與普通代碼可寫在同一個 `.no` 檔案中

> 反向方向（讓 C / C++ / Rust / Go / Java / Python / C# 反過來呼叫 Nolang）用 `#{cabi='symbol'}` 與 `no build --shared`，見 [從宿主語言調用 Nolang](./host-interop)。

### 註解系統（`#{...}`）

`#{...}` 是通用註解系統，以逗號分隔的鍵值對列表。支援以下值類型：

| 語法  | 類型  | 範例                                   |
| --- | --- | ------------------------------------ |
| 獨立鍵 | 布爾  | `#{debug}`                           |
| 布爾值 | 布爾  | `#{inline=true}` / `#{inline=false}` |
| 數值  | 整數  | `#{max=100}`                         |
| 文字  | 字串  | `#{name='hello'}`                    |
| 識別字 | 識別字 | `#{mode=fast}`                       |
| 陣列  | 陣列  | `#{derive=[Serialize, Deserialize]}` |
| 範圍  | 範圍  | `#{range=[0..256)}`                  |

多個鍵值對以逗號分隔：

```no
#{derive=[Serialize, Deserialize], range=[0..256), max=100, debug}
```

#### 註解位置

`#{...}` 只允許兩種寫法：

- **上方**：獨立成行，置於目標（陳述、結構體欄位、枚舉成員、宣告、match 臂）上方；
- **後方**：寫在目標同一行的尾隨位置（`res = arr[i] #{index-out=zero}`、`a pt #{inline}`、  
  `green #{deprecated}`）。

寫在目標**前方同一行**（前綴，如 `#{index-out=zero} res = arr[i]`）是**編譯錯誤**，編譯器與  
LSP 會回報同一條診斷。判定方式：註解群組的 `}` 之後、同一行上不得再有程式碼（換行、  
`;` 行註釋、或緊接的另一個註解群組不算程式碼）。

```no
; ✓ 上方：獨立成行
#{overflow=wrap}
x i8 = a + 100

; ✓ 後方：同一行尾隨
x i8 = a + 100 #{overflow=wrap}
```

```no
#{fmt-skip}
; ✗ 前方：同一行前綴 → 報錯
#{overflow = wrap} x i8 = a + 100
```

結構體欄位與枚舉成員（C 風格枚舉的值、標籤列舉的變體）的註解同樣只允許上方或後方  
（`a pt #{inline}`、`a pt #{inline=true}`、`green #{deprecated}`、`ok(v i64) #{inline}`）。  
注意 `#{inline}` 的**值**同樣受支援：`#{inline=false}` 是合法的（見下文  
〈結構體欄位上的 `#{inline}`〉）。

`if <cond> { ... }` 寫法中，註解夾在條件與 `{` 之間（`if x > 0 #{overflow=wrap} {`）也是  
「前方」位置，同樣報錯。該處的掃描是寬鬆的，過去會把整組註解吞掉——既不生效也不報錯，  
是最難察覺的一種失效。

範圍語法支援 16 種括號組合（規律：`[` 包含左端點，`(` 不包含；`]` 包含右端點，`)` 不包含）：

**有界區間（4 種）**— 兩端都有值：

- `[a..b]` — 兩端閉區間（`x >= a && x <= b`）
- `[a..b)` — 左閉右開（`x >= a && x < b`）
- `(a..b]` — 左開右閉（`x > a && x <= b`）
- `(a..b)` — 兩端開區間（`x > a && x < b`）

**無上限區間（4 種）**— 省略右端點：

- `[a..]`、`[a..)` — 左包含，無上限（`x >= a`）
- `(a..]`、`(a..)` — 左不包含，無上限（`x > a`）

**無下限區間（4 種）**— 省略左端點：

- `[..b]`、`(..b]` — 無下限，右包含（`x <= b`）
- `[..b)`、`(..b)` — 無下限，右不包含（`x < b`）

**完全無界區間（4 種）**— 兩端都省略，基於類型最小值/最大值：

- `[..]` — 類型最小值 ≤ x ≤ 類型最大值（兩端都包含）
- `[..)` — 類型最小值 ≤ x < 類型最大值
- `(..]` — 類型最小值 < x ≤ 類型最大值
- `(..)` — 類型最小值 < x < 類型最大值

#### 註解的作用域

註解的**位置**決定宿主，宿主的**種類**決定它管多大範圍。一共四層，由外向内生效、
就近覆蓋，函式體是繼承的硬邊界：

| 層級 | 宿主 | 生效範圍 |
| --- | --- | --- |
| **L0 模組級** | 檔案頭 | 本檔全部宣告與語句（檔案頭註解 + 一個空行） |
| **L1 宣告級** | 常量/全局變數、結構體、介面、標籤列舉、函式、方法 | 該宣告的整個定義（函式含函式體與全部巢狀區塊） |
| **L2 區塊級** | 協程組、if 組、`cond -> body`、match、`for`/計數/區間循環 | 該塊體內全部語句（含巢狀塊，止於內層 L1 邊界） |
| **L3 單行級** | 單條陳述（`let`、賦值、`?=`、`return`、呼叫、`cond ->` 臂） | 該陳述自身及其運算式 |

```no
; L1：整個函式
#{overflow=wrap}
sum = (xs []i64) (total i64) {
    ; L3：只管下一條陳述
    #{overflow=wrap}
    total = xs.len() - 1
    ; L2：整個循環體
    #{overflow=wrap}
    i <- [0..xs.len()): {
        total = total + xs[i]
    }
}
```

完整的判定規則、鍵 × 層級矩陣、與現行實作的差距，見
[〈註解與註釋的作用域〉](annotation-scope.md)。

FFI 註解 `#{c}` 是註解系統的特殊形式，當註解包含 FFI 語言鍵（`c`、`cpp`、`rust` 等）且後續為函式宣告時，編譯器將其識別為 FFI 綁定：

```no
; #{c} 帶額外註解
#{c, debug}
_sqlite3-open = (filename str, db **byte) (rc i32)
```

#### 註解附加到宣告

非 FFI 註解會自動附加到緊隨其後的宣告（變數宣告、結構體定義），可用於為數值型別（如 `num`、`i64` 等）標記範圍限制等元數據：

```no
; 變數宣告帶 range 註解
#{range=[0..256)}
x num = 42

; 結構體定義帶註解
#{derive=[Serialize, Deserialize]}
point {
    x i64
    y i64
}

; 結構體欄位帶 range 註解（可用於 num 等數值型別）
person {
    age num #{range=[0..150]}
    score i64 #{range=[0..256)}
    name str
}
```

`range` 註解特別適用於 `num` 型別（`num = int | float`），用於標記數值的有效範圍。範圍值可以是整數或識別字：

```no
; 使用常量識別字作為範圍邊界
#{range=['i8.MIN'..'i8.MAX']}
val i8 = 100
```

#### 文件嵌入（`#{embed=...}`）

`#{embed=...}` 註解在編譯期將外部文件內容嵌入為 `[]byte` 只讀常量，類似 Go 的 `//go:embed`。嵌入的數據直接編譯進可執行文件，實現單二進制分發，無需運行時讀取文件。

**語法**：

```no
#{embed='path/to/file'}
ICON []byte

; 或使用裸路徑（無引號）
#{embed='assets/icon.ico'}
ICON []byte
```

`embed` 註解附加到緊隨其後的 `[]byte` 變數宣告上。變數宣告**不能**帶有顯式初始值——值由嵌入的文件內容提供。

**路徑解析**：

- 相對路徑相對於包根目錄（`package.noml` 所在目錄）解析
- 支援絕對路徑
- 路徑中可包含 `/`、`.`、`-` 字符

**範例**：

```no
; 嵌入 Windows 圖標
#{embed='assets/win-icon.ico'}
WIN-ICON []byte

; 嵌入 SSL 證書
#{embed='certs/server.pem'}
SERVER-CERT []byte

; 嵌入配置文件
#{embed='config/default.json'}
CONFIG []byte

; 使用嵌入數據
print('icon size: ', len(WIN-ICON))
print('first byte: ', WIN-ICON[0])
```

**注意事項**：

1. 嵌入數據為**只讀**常量，指向只讀內存區域。寫入嵌入數據的行為是未定義的（與 Go `//go:embed` 語義一致）
2. 嵌入變數不參與堆釋放——程序退出時不會 free 嵌入數據的指針
3. 僅支援 `[]byte` 類型宣告（`[N]byte` 固定陣列亦可）
4. 文件必須在編譯期存在，否則報錯
5. 不能與顯式初始值共存（如 `ICON []byte = [1, 2, 3]` 會報錯）

#### 文件夾嵌入（`#{embed='dir'}`）

`#{embed=...}` 也支援嵌入整個目錄。當路徑指向目錄時，編譯器遞迴讀取目錄下所有文件，嵌入為 `fs.embed` 類型的只讀文件系統。運行時通過 `read()`、`exists()` 方法按路徑訪問嵌入的文件，無需任何 C 函數——查找邏輯完全由 Nolang 實現。

**語法**：

```no
; 嵌入整個前端目錄
#{embed='../frontend/dist'}
DIST fs.embed

; 從嵌入的文件系統讀取文件
data = DIST.read('index.html')

; 檢查文件是否存在
ok = DIST.exists('css/style.css')
```

**`fs.embed` 類型**：

`fs.embed` 是 Nolang 標準庫 `fs` 模組中定義的結構體，包含嵌入文件的元資料（路徑、偏移、長度）。編譯器在編譯期讀取目錄並將所有文件內容拼接為常量全局變數，運行時由純 Nolang 實現的 `read` 方法遍歷查找。

| 方法       | 簽名                                  | 說明                |
| -------- | ----------------------------------- | ----------------- |
| `read`   | `(path str) (data ?[]byte)`           | 讀取嵌入的文件，返回內容（未找到時為 nil） |
| `exists` | `(path str) (yes bool)`             | 檢查嵌入的文件是否存在       |

**範例**：

```no
; HTTP 靜態文件伺服器：將前端文件嵌入單一二進制
#{embed='../frontend/dist'}
DIST fs.embed

serve = (conn server-conn) {
    conn.parse()
    path = conn.path
    path == '/' -> path = '/index.html'

    ; 去掉前導 / 得到相對路徑
    rel = path.slice(1, path.len-bytes())

    ; 從嵌入的文件系統讀取
    data []byte = DIST.read(rel)
    {
        ok -> it

        -> {
            conn.write-html(404, 'Not Found')
            conn.close()
            return
        }
    }
    conn.write-status(200)
    conn.write-header('Content-Type', get-content-type(path))
    conn.write-body(data)
    conn.close()
}
```

**注意事項**：

1. 目錄中所有文件（含子目錄）遞迴嵌入，路徑以相對於嵌入目錄的路徑存儲（如 `css/style.css`）
2. 路徑分隔符統一為正斜槓 `/`（跨平台一致）
3. `fs.embed` 類型變數為只讀，不參與堆釋放
4. 適用於單二進制分發場景（如 HTTP 靜態文件伺服器、CLI 工具嵌入資源）

#### 平台註解

平台註解是編譯期過濾器，根據目標平台決定是否包含代碼。使用**扁平化鍵**（如 `#{mac-arm64}`）同時指定 OS 與架構，無歧義，附加到緊隨其後的宣告上。不匹配的代碼完全不參與編譯——不生成 LLVM IR，不進行類型檢查。

**支援的平台鍵（6 種扁平組合）：**

| 鍵                | 匹配                         |
| ---------------- | -------------------------- |
| `#{linux-amd64}` | Linux x86_64               |
| `#{linux-arm64}` | Linux ARM64                |
| `#{win-amd64}`   | Windows x86_64             |
| `#{win-arm64}`   | Windows ARM64              |
| `#{mac-amd64}`   | macOS x86_64（Intel）        |
| `#{mac-arm64}`   | macOS ARM64（Apple Silicon） |

```no
#{mac-arm64}
print('running on macOS ARM64')

#{linux-amd64}
print('running on Linux x86_64')

#{win-amd64}
print('running on Windows x86_64')

; 平台特定的變數
#{mac-amd64, mac-arm64}
sep = '/'

#{win-amd64, win-arm64}
sep = '\\'

; 平台特定的函數
#{mac-arm64, mac-amd64}
greet = () {
    print('hello from mac')
}

#{linux-amd64, linux-arm64}
greet = () {
    print('hello from linux')
}

greet()
```

同一宣告上的多個鍵為 **OR** 關係——任一匹配即包含。因每個鍵已同時指定 OS 與架構，無需 AND 邏輯。

| 註解                          | 含義                                |
| --------------------------- | --------------------------------- |
| `#{mac-arm64}`              | 僅 macOS ARM64                     |
| `#{mac-amd64, mac-arm64}`   | macOS 任意架構                        |
| `#{linux-amd64, win-amd64}` | Linux x86_64 **或** Windows x86_64 |
| `#{mac-arm64, linux-arm64}` | macOS ARM64 **或** Linux ARM64     |

```no
; macOS 和 Linux（所有架構）都執行
#{mac-amd64, mac-arm64, linux-amd64, linux-arm64}
shared = () {
    print('unix-like')
}

; 僅 Windows x86_64
#{win-amd64}
reg-key = () {
    print('reading registry on win/x64')
}

; 僅 macOS ARM64（Apple Silicon）
#{mac-arm64}
neural = () {
    print('Apple Neural Engine available')
}
```

使用 `os.get-arch()` 可在執行期取得當前架構，使用平台註解則在編譯期包含或排除代碼。

## package.noml 編譯器配置

`package.noml` 中的 `compiler` 區塊控制編譯器行為。`version` 值約定帶前導 `v`（如 `"v0.1.0"`）；編譯器內部解析時對前綴不敏感，`"v0.1.0"` 與 `"0.1.0"` 皆視為同一版本。

```noml
{
  compiler: {
    version: 'v0.1.0'
    anonymous-fn-type: false
    emit: 'js'
  }
}
```

### `emit`

控制輸出目標後端：

| 值           | 說明                                      |
| ----------- | --------------------------------------- |
| `""` （空，預設） | 使用 LLVM 後端生成原生可執行檔                      |
| `"js"`      | 使用 JS 後端生成 JavaScript（型別擦除），無需 LLVM 工具鏈 |

設定為 `"js"` 後，`no build` 自動走 JS 後端，等效於命令列 `--js`。命令列旗標優先級高於配置文件。

```noml
; 自動生成 JavaScript
compiler: {
  emit: 'js'
}
```

### `anonymous-fn-type`

控制是否允許匿名函式型別語法（如 `cb ()()`）。預設 `false`（禁用），需使用具名函式型別別名。

### `link-libs`

指定連結的 C 函式庫列表：

```noml
compiler: {
  link-libs: ['crypto', 'ssl']
}
```

## 編譯期變數注入（-ld）

`no build`、`no run`、`no test` 支援通過 `-ld-KEY=VALUE` 旗標在編譯期注入全域常量。注入的變數等同於在源碼頂部聲明 `KEY = VALUE`，可在程式中直接使用。

### 語法

```bash
no build -ld-KEY=VALUE [更多 -ld...] <file>
```

- `-ld` 前綴後緊跟變數名和值，用 `=` 分隔
- 多個 `-ld` 旗標可同時使用
- 布林簡寫：`-ld-DEBUG`（不含 `=VALUE`）等同於 `-ld-DEBUG=true`

### 值的型別推斷

| 值的形式             | 推斷型別            | 範例                  |
| ---------------- | --------------- | ------------------- |
| 整數               | `i64`           | `-ld-COUNT=42`      |
| 浮點數              | `f64`           | `-ld-PI=3.14`       |
| `true` / `false` | `bool`          | `-ld-DEBUG=true`    |
| 其他               | `str`（單引號字串字面量） | `-ld-VERSION=0.1.2` |

### 範例

```no
; 假設使用 -ld-VERSION=0.1.2 -ld-COUNT=42 -ld-DEBUG=true 編譯
print('VERSION:', VERSION); 輸出: VERSION: 0.1.2
print('COUNT:', COUNT); 輸出: COUNT: 42
print('DEBUG:', DEBUG); 輸出: DEBUG: 1 (bool 在原生後端印為 1)
```

```bash
; 注入多個變數
no build -ld-VERSION=0.1.2 -ld-COUNT=42 -ld-DEBUG=true main.no

; 布林簡寫
no run -ld-RELEASE main.no

; JS 後端也支援
no build --js -ld-VERSION=0.1.2 main.no
```

### 與源碼聲明的關係

`-ld` 注入的變數與源碼聲明的互動規則：

- **源碼已有同名聲明**：注入的值會**就地替換**源碼中該變數的值。常用模式是在源碼中聲明占位變數（如 `VERSION = ''`），構建時用 `-ld-VERSION=0.1.2` 賦值。
- **源碼中不存在**：注入的變數會被前置到 AST 最前面，作為新的全域常量宣告，參與所有後續編譯 pass（型別推斷、驗證、模組合併、codegen）。
- **不注入時**：源碼中的聲明保持原樣，不受影響。

```no
; 源碼聲明占位變數
VERSION = ''
COUNT = 0

print('VERSION:', VERSION)
print('COUNT:', COUNT)

; no build -ld-VERSION=0.1.2 main.no
; 輸出: VERSION: 0.1.2  （注入值覆蓋了源碼的 ''）
;       COUNT: 0        （未注入，保持源碼值）
```
