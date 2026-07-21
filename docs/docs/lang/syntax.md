---
sidebar_position: 2
---

# 語法

## 註釋

Nolang 支援三種**單行註釋**標記與一種**多行（塊）註釋**標記：

- `//` —— 傳統單行標記（註釋到行尾）
- `;` —— 單行標記（註釋到行尾）
- `;; <內容>` —— 單行標記（`;;` 後**同一行**還有內容時，註釋到行尾，語義等同 `;`）
- `;;\n` —— 多行（塊）註釋：`;;` 後**緊跟換行**（僅允許空白）時觸發多行模式，直到遇到同樣後跟換行/EOF 的 `;;` 結束

```nolang
// 這是註釋
; 這也是註釋，語義相同
;; 這還是單行註釋（;; 後沒有換行）
x = 1 ; 行內註釋，註釋到行尾
x = 2 ;; 行內單行註釋，語義相同

;;
這是多行（塊）註釋
可以跨多行
直到遇到單獨的 ;;
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

> **規則：每行一條語句，禁止使用逗號 `,` 將多條語句寫在同一行。**（分號 `;` 現在是註釋標記，不再能連接語句。）
> 這條規則同樣適用於註釋中的程式碼範例。即使在註釋裡，也不應該用逗號把多條語句放在同一行，以免給讀者造成困惑。
>
> ```nolang
; ❌ 錯誤：註釋中使用逗號合併多條語句
; h0 = 1732584193, h1 = 4023233417

; ❌ 錯誤：使用逗號合併多條語句
; out = from-i64(v), out = from-u64(v)
; debug(msg), info(msg), warn(msg)

; ✅ 正確：每條語句獨立一行
; h0 = 1732584193
; h1 = 4023233417
; out = from-i64(v)
; out = from-u64(v)
; debug(msg)
; info(msg)
; warn(msg)
```

## 數據類型

基礎類型

- byte
- bool // 只允許小寫
- char // 字符類型（rune），雙引號包裹單字符，如 "中"
- str // 字符串類型，單引號包裹 'hello'，或反引號原始字符串 `多行內容`
- i8
- i16
- i32
- i64 // 數字默認類型，不區分架構
- u8
- u16
- u32
- u64
- usize //僅用於ffi 
- f32
- f64

容器類型

- obj // 對象
- map // 映射
- arr // 定長數組
- vec // 變長數組
- slice // 切片（視圖）沒有獨立數據結構，必須依附於arr/vec

- \* // 指針 僅限 FFI `#{c}` 宣告與標準庫
- any // 任意類型 僅限標準庫

高級類型

- bigint
- err

## 類型別名與聯合類型

類型別名為現有類型建立一個新名稱。使用等號語法 `name = type`，支援單一類型別名和多類型聯合。

### 語法

```nolang
// 聯合類型：多個類型用 | 分隔
int = i8 | i16 | i32 | i64 | u8 | u16 | u32 | u64
float = f32 | f64
num = int | float

// 單一類型別名
bytes = []byte
buf = [16]u8
```

### 聯合類型的鏈式引用

聯合類型可以引用其他聯合類型，形成層次結構：

```nolang
int = i8 | i16 | i32 | i64 | u8 | u16 | u32 | u64
float = f32 | f64
num = int | float     // num 是 int 和 float 的聯合
```

### 在函數中使用

聯合類型可用於函數參數和返回值，編譯器會自動進行單態化（monomorphization），為每個成員類型生成獨立的函數版本：

```nolang
// 參數類型為 num 聯合
max = (a ..num) (r num) {
    r = a[0]
    n = len(a)
    i <- [1..n): {
        a[i] > r -> r = a[i]
    }
}

// 方法定義在聯合類型上
num.sign = () (r num) {
    {
        . > 0 -> r = 1
        . < 0 -> r = -1
        -> r = 0
    }
}
```

### 偵測規則

等號語法在以下情況下被識別為類型別名（而非變數賦值）：

- `name = type | type | ...`：聯合類型（含 `|`）
- `name = []type`：切片類型
- `name = [N]type`：陣列類型
- `name = ?type`：可選類型
- `name = known-type`：單一類型別名，其中 `known-type` 是內建類型名（如 `i64`、`f64`、`bool`、`str` 等）或先前已定義的類型別名名稱

## 變量聲明

```nolang

// 變量沒有關鍵字
// i64、f64、byte、bool、byte、str可以省略類型標注
i = 1

// f64 中間有.
f = 1.0

// byte
b = x00


// i8 如果變量名和類型一致，可以忽略類型標注
i8 = 3

// 默認0值
// 變量定義不需要提前聲明
u16

// str 單引號包裹
name = 'nolang'

// bool true/false 全小寫
flag = true
flag = false

// 變量賦值
// 不允許同名，如果同名則視為修改變量
name = 'hello'
name = 'world'

// 字符串拼接
greeting = 'hello, ' - name

// 原始字符串（反引號包裹，多行，不轉義）
sql = `
SELECT id,name
FROM user
WHERE id > 100
`

// 顯式類型標注
a u64 = 10

// 字符（雙引號 = char/rune，單字符）
c = "中"

// byte類型
b = x00

// arr 定長數組
arr [3] = [1, 2, 3]

// vec 動態數組（切片）
vec = [4, 5, 6]

// 顯式類型（切片）
typed []u8 = [1, 2, 3]

// 數組
typed [3]u16 = [1, 2, 3]
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

1. **開頭** `` ` `` 後必須**緊跟源碼換行**；
2. **結尾** `` ` `` 必須**單獨佔一行**，且該行僅允許空白 + 結束反引號；
3. **首尾兩行的反引號所在行不計入字符串內容**，只作為分隔標記；
4. **轉義規則**：內部所有 `\`、`\n`、`\t`、`\'`、`\"` 全部**原樣保留**，不做任何轉義解析；
5. **源碼內的換行、縮進空格完整保留**到字符串字節裡；
6. **無法直接嵌入反引號字符**，如需包含 `` ` `` 只能用普通單引號字符串拼接。

### 示例

```
// 內容為 "SELECT id,name\nFROM user\nWHERE id > 100\n"
// 轉義字符 \n \t 原樣保留，不解釋
raw = `
line1\nline2
\ttabbed
`
```

## 正則字面量（Regex Literal）

Nolang 支援 JavaScript 風格的正則字面量語法 `/pattern/flags`，用於創建已編譯的 `regexp` 實例。

### 語法

```nolang
// 基本正則字面量
re = /\d+/

// 帶旗標（flags）
re = /hello/gi

// 字符類、錨點、量詞
re = /[a-z]+/
re = /^hello.*world$/

// 轉義斜線
re = /a\/b/
```

### 旗標

| 旗標 | 含義 |
| ---- | ---- |
| `g` | 全局匹配 |
| `i` | 不區分大小寫 |
| `m` | 多行模式 |
| `s` | `.` 匹配換行符 |

旗標是可選的，跟在結束 `/` 之後，由 ASCII 字母組成。

### 上下文敏感的詞法分析

`/` 既是除法運算符也是正則字面量的起始符，Nolang 採用與 JavaScript 相同的**上下文敏感**詞法分析來區分：

- **表達式起始位置**（語句開頭、`=` / `(` / `[` / `{` / `,` / `:` / `;` 等之後）→ `/` 開始正則字面量
- **值產生位置**（標識符、字面量、`)` / `]` / `}` 等之後）→ `/` 是除法運算符
- `//` 永遠是行註釋（優先級最高）

```nolang
// 正則字面量（= 後是表達式起始位置）
re = /\d+/
result = match-text(/[a-z]+/, text)

// 除法運算符（標識符後是值產生位置）
ratio = 100 / 4
x = a / b
```

### 脫糖

正則字面量在代碼生成階段脫糖為對標準庫 `regexp-compile` 函數的調用：

```nolang
// 源碼
re = /\d+/

// 脫糖後等價於
re = regexp-compile('\\d+')
```

`regexp-compile` 定義在 `std/regexp.no`，創建 `regexp` 結構體並調用 `.compile()` 編譯模式。

### 使用示例

```nolang
; 創建正則並匹配
re = /\d+/
matched = re.matches('hello 123 world')
print(matched)  ; true

; 查找匹配
re = /[a-z]+/g
result = re.find('hello 42 world')
print(result)  ; "hello"

; 作為函數參數
result = match-text(/\d+/, text)
```

> **注意：** 空 pattern `//` 會與行註釋衝突（與 JavaScript 行為一致）。如需空匹配，使用 `/(?:)/`。

## 命名規則

變量名、函數名、結構体名等可以以下劃線開頭，後續可以使用中連接符、字母、數字組成，不能以數字开头，不能以中連接符結尾，不能連續多個中連接符

**大小寫規則（必須遵守）：**
- **全局常量、全局變量**：**必須**使用大寫字母開頭（如 `NOLANG`、`MAX-SIZE`、`HEX-CHARS`）
- **局部變量、函數參數**：使用小寫字母（如 `hex-chars`、`data-len`）
- **函數名、結構體名**：使用小寫字母（如 `sha1-block`、`db-mysql`）

> **全局變量必須大寫字母開頭**，這是強制規則，不是慣例。小寫開頭的頂層變量會被編譯器視為局部變量，可能導致未定義引用等錯誤。

```nolang
// ✅ 正確：全局數據使用大寫字母
NOLANG = 'nolang'
MAX-SIZE = 1024
HEX-CHARS = '0123456789abcdef'

// ✅ 私有全局變量：下劃線開頭，其後仍需大寫
_NOLANG = 'nolang'
_PRIVATE-CONST = 42

// ❌ 錯誤：全局變量不能使用小寫字母開頭
// x1 = 10
// x = 10
// foo-bar = 42
// hello-world = 'Hello World'

// ✅ 局部變量（函數內）使用小寫字母
// fn-example = () {
//     x1 = 10
//     x = 10
//     foo-bar = 42
//     hello-world = 'Hello World'
// }
```

## API 文檔規範

函數的文檔註釋應包含完整的參數名與型別、返回參數名與型別。

**規則：**
- 函數定義上方的文檔註釋，必須列出每個參數的名稱和型別，以及返回參數的名稱和型別
- 模組頂部的 API 摘要也應使用完整簽名（含參數名、型別、返回名、型別），不要使用省略寫法

```nolang
// ❌ 錯誤：缺少型別，缺少返回參數名
// sha1(data) (hash)
// sha1-block(s, h0..h4)

// ✅ 正確：包含參數名、型別、返回參數名、型別
// sha1(data []byte) (hash [20]byte) — 完整雜湊
// sha1-block(s []u32, h0 u32, h1 u32, h2 u32, h3 u32, h4 u32) — 處理單一區塊

// 函數定義上方的文檔註釋也應遵守同樣規範：
// sha1: 計算 SHA-1 雜湊
// data []byte: 輸入位元組陣列
// 返回 hash [20]byte: 20 位元組雜湊值
sha1 = (data []byte) (hash [20]byte) {
    ...
}
```

## 優先使用標準庫

Nolang 標準庫提供了豐富的常用功能，包括字串操作、位元組轉換、雜湊計算、網路通訊等。

**規則：如果標準庫中已有對應功能，不建議自行重新實現。** 開發者應仔細查看標準庫文檔（`docs/docs/std/overview.md`），避免重複造輪子。

```nolang
// ❌ 錯誤：自行實現 str → []byte 轉換
str-to-bytes = (s str) (out []byte) {
    n = s.len
    i = 0
    {
        out[i] = s[i]
        i = i + 1
    } (i < n)
}

// ✅ 正確：使用標準庫 str.to-bytes() 方法
data []byte = s.to-bytes()
```

常見的標準庫替代：
- `str.to-bytes()` — 字串轉位元組陣列（替代手寫 `str-to-bytes`）
- `[]byte.to-str()` — 位元組陣列轉字串（替代手寫 `bytes-to-str`）
- `[n]t.to-vec()` — 定長陣列轉切片（`[20]byte` → `[]byte`）
- `[]byte.to-hex()` / `[]byte.to-hex-lower()` — 位元組陣列轉十六進制字串
- `str.to-i64()` / `str.to-f64()` — 字串解析為數字
- `int.to-str()` / `float.to-str()` — 數字轉字串
- `std/hash/sha1`、`std/hash/sha256`、`std/hash/sha512` — 雜湊計算

## 檔案命名

`.no` 檔名（含文件夾名）一律使用中連字符 `-` 連接單詞，**不使用下劃線 `_`**。這與變量名、函數名、結構體名等 Nolang 標識符的命名風格保持一致。

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

函數可以透過**具名結果參數（named result parameters）**輸出結果，本質是
**輸出參數（out-parameter）的語法糖**；`...` 僅用於提前終止，不能跟結果。

Nolang 的函數定義形式為 `name = (in-params) (out-params) { body }`：第二個
括號組 `(out-params)` 宣告具名結果參數（語法糖）。函數體內對它們賦值
（與普通入參一樣皆為引用型別）；**真正接收結果的變數必須在調用處定義**——
定義裡的結果參數名只是佔位符，調用方的 LHS（或尾隨引數）決定結果落在哪個
變數：

```nolang
parse-line = (s str, max-fields i64 = 1024) (fields []str) {
    ...            // 函數體內對 fields 賦值
}

// 調用處定義接收變數（LHS 綁定，型別由簽名推斷）
fields = parse-line(line)
// 多結果按順序綁定
a, b = swap(x, y)
// 或尾隨引數形式：res 作為額外的輸出引數傳入
add1(5, 3, res)
```

Nolang 的函數有以下特點：

- **具名結果參數是語法糖**：底層仍是透過入參（out-parameter）完成，不會產生
  新的返回值物件，因此內部是安全的。
- 所有函數參數（含結果參數）均為引用型別，修改會直接影響調用方資料。
- 函數內的變量在函數退出時自動銷毀。
- 調用處必須提供接收變數（LHS 或尾隨引數）；「函數沒有返回值」的說法不準確——
  準確說法是：結果透過具名結果參數（out-parameter 的語法糖）傳遞，且由調用處綁定。

### 參數默認值

函數參數可以使用 `name type = expr` 語法指定默認值。帶有默認值的參數在調用時可以省略，編譯器會自動填充默認值。帶默認值的參數必須放在參數列表末尾。

```nolang
// 帶默認值的函數定義
parse-line = (s str, max-fields i64 = 1024) (fields []str) {
    ...
}

// 以下兩種調用方式都合法：
fields = csv.parse-line(line)              // max-fields 默認為 1024
fields = csv.parse-line(line, 256)         // max-fields = 256
```

```nolang

add = (a i64, b i64) (result i64) {
    result = a + b             // 通過參數返回結果
    ...                        // 提前終止（可選）
}

// 可變參數
add3 = (a ..i64) {
}

// 函數調用
sum = add(1, 2)                 // sum == 3

// 匿名函數 和for？ 有傳參？
(a i64) { print(a) }(10)

// 函数调用
add(a, b)

// 也可能有多个返回值
a, b = swap(5, 3)
```

## 流程控制

> **Deprecated 語法（n 版本後移除）**：`for { }` / `for cond { }` / `for init, cond, update { }` / `for i in [..) { }` / `!! { }` / `match x { }` / `if/elif/else { }` 仍可解析但會輸出 deprecation warning。請改用下方「**新式語法**」。

### 新舊對照

| 舊式                         | 新式                                               |
| ---------------------------- | -------------------------------------------------- |
| `for { }` / `!! { }` 無限循環 | `{ } ()`                                          |
| `for cond { }` 條件循環      | `{ } (cond)`                                      |
| `for i=0, i<n, i++ { }` 計數 | `{ } * n`（常數計數）或 `i <- [0..n): { }`（變數） |
| `for i <- [a..b] { }` 範圍   | `i <- [a..b]: { }`                                 |
| `for i in [a..b) { }` 範圍   | `i <- [a..b): { }`                                 |
| `match x { ... }` 匹配       | `x: { ... }`                                       |
| `if/elif/else { }` 分支      | `{ cond -> body }`                                 |
| `continue`                   | `**`                                               |
| `break`                      | `*`                                                |
| `return`                     | `...`                                              |

### Loop / While / for-in

```nolang
// 无限循环（空括號代表條件恆真）
{
    ...
} ()

// 條件循環（檢查 cond，為真時執行主體）
{
    do-something()
} (x == 1)

// 限定執行次數
{
} * 10

// N ≤ 0 時循環體不執行（0 次或負數次均跳過）
{
    print('不會執行')
} * 0

{
    print('也不會執行')
} * -3

// 區間語法（未來會支持 map, arr, vec）
i <- [a..b]: {     // 闭区间：a ≤ i ≤ b
}
i <- (a..b]: {     // 左开右闭：a < i ≤ b
}
i <- [a..b): {     // 左闭右开：a ≤ i < b
}
i <- (a..b): {     // 开区间：a < i < b
}
i <- [5..0]: {   // 递减 — 运行时方向检测：start > end 时自动递减
}
i <- 'abc': {   // 遍历字符串中的每个字符
}

// 运行时方向检测：当 start > end 时，迭代自动递减（步长 -1）。
// 四种括号组合均支持递减：
//   [5..1]  → 5 4 3 2 1   左闭右闭，递减
//   (5..1]  → 4 3 2 1     左开右闭，递减
//   [5..1)  → 5 4 3 2     左闭右开，递减
//   (5..1)  → 4 3 2       左开右开，递减
//   (3..0]  → 2 1 0       左开右闭，递减到 0
// 当 start <= end 时，按递增方向迭代（步长 +1）。

// ❌ 明確拒絕
//   區間邊界必須是整數；不支持嵌套表達式
//   for i <- [1.5..5.5] { }   // 編譯錯誤
//   for i <- [0..[1..5][0]] { } // 語法錯誤

// 條件循環（新式 { } (cond) 取代舊式 for cond { }）
// 大多數情況可改用 range-for：i <- [0..n): { }
{
    do-something()
} (x == 1)
```

### 跳出 / 跳過 / 提前返回

```nolang
i <- [0..10): {
    *      // break
    **     // continue
    ...    // return/terminate（提前返回，僅終止函數）
}

// 也可使用英文關鍵字（與 C/Rust 一致，便於移植、且能正常編譯執行）：
i <- [0..10): {
    break     // 等價 *
    continue  // 等價 **
    return    // 等價 ...，僅提前終止函數
}
```

> ⚠️ **`return`（及其符號形式 `...`）只能裸用，不能帶返回值。**
> Nolang 函數沒有「返回值」機制——結果一律通過**具名結果參數（out-param）**在函數體內賦值傳出（見「函數定義」）。
> 因此 `return <值>`、`return(expr)`、`... <值>` 都**被禁止**，編譯器 / formatter / LSP 都會報錯：
> - 編譯器（`no build`）：`parser errors: 'return' 後不能跟返回值；Nolang 函數結果通過具名結果參數（out-param）傳出…`
> - `no fmt`：`format error: …` 並以非零狀態退出
> - LSP：在對應行顯示紅色錯誤診斷
>
> 錯誤寫法：
> ```nolang
> has = (n i64) (r i64) {
>     n == 0 -> return 0        // ❌ 編譯 / formatter / LSP 均報錯
>     r = n
> }
> ```
> 正確寫法（先給結果參數賦值，再裸 `return` 提前終止）：
> ```nolang
> has = (n i64) (r i64) {
>     n == 0 -> { r = 0; return }   // ✓
>     r = n
> }
> ```

### Match

```nolang
// 簡單寫法，it 用於取參數
x: {
    err -> log(it)
    nil -> log('nil')
    ->
        do-right-thing(it)
}

// 析構寫法
x: {
    err(e) -> log(e)
    nil -> log('nil')
    val(v) ->
        do-right-thing(v)
}

user: {
    User{id=1} -> print('管理員')
    User{name=n} -> print('用戶：', n)
    -> print('匿名')
}

score: {
    [0..59] -> print('不及格')
    [60..89] -> print('良好')
    [90..=100] -> print('優秀')
    -> print('分數非法')
}

num: {
    1 || 3 || 5 || 7 -> print('奇數小數')
    2 || 4 || 6 -> print('偶數小數')
    -> print('更大數字')
}

// 有返回值，最後一個語句/值
result = x: {
    1 -> 1
    2 -> 2 + 1
    -> a + b
}

// 多行 arm body 必須使用大括號 -> { ... }
x: {
    nil -> {
        log('nil')
        do-cleanup()
        return
    }
    err -> {
        log(it)
        do-cleanup()
        return
    }
    ok -> print(it)
}
```

> **多行 arm body 規則**：當 arm body 包含多個語句時，必須使用大括號 `-> { ... }` 括起來。單行 body 可直接寫在 `->` 之後。這是因為多行 body 若不使用大括號，option match 的 `it` 綁定將無法正確插入，導致編譯錯誤。

> **for-in 體內 match 語義**：`i <- (a..b]: { 1 -> ... 2 -> ... }` 對每個迭代變量 `i` 執行一次 match 體（`1 ->` 等同於 `i == 1 ->`，依此類推）。這是每輪迭代執行一次 match 的語法糖。

#### Match 風格指引

```nolang
// ❌ 避免重複分支體
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

// ✅ 公共邏輯放 -> catch-all
w = tls-c.send(req)
w: {
    ok -> n = it
    -> {
        tls-c.close()
        return
    }
}

// ✅ 反之亦然：簡單分支命名，複雜邏輯放 ->
val: {
    nil -> return
    err -> log(it)
    -> {
        n = it
        total = total + n
        process(n)
    }
}
```

```nolang
// 單語句 — 不加大括號
val: {
    ok -> print(it)
    -> print('empty or error')
}

// 多語句 — 必須加大括號
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

```nolang
// it 隱式綁定
val: {
    ok -> process(it)       // it = 解包後的值
    err -> log(it)          // it = 錯誤訊息字串
    -> log('empty')         // catch-all，此處為 nil
}
```

```nolang
// ✅ 組合 option 模式：nil || err -> body
// 當 option 為 nil 或 err 時共用同一個分支
val: {
    nil || err -> {
        cleanup()
        return
    }
    ok -> process(it)
}

// ✅ 也可與 -> catch-all 混用
val: {
    nil || err -> log('failed')
    ok -> process(it)
}
```

### If / Else

```nolang
// 多分支（推薦新式）
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

// 單 if（保留）
x == 1 -> do-something()

// 三元表達式 condition ? true-value : false-value
c = flag ? 1 : 2
max = sum > 10 ? sum : 10
```

### 異步編程（run / awy）

Nolang 使用 `run` 和 `awy` 實現異步並發。異步函數的名稱必須以 `-async` 結尾，但不使用 `async` 關鍵字。

- `run` — 啟動異步線程，返回一個 task handle
- `awy` — 等待異步線程完成並取得結果

```nolang
// 異步函數定義（名稱以 -async 結尾）
compute-async = (n i64) (r i64) {
    r = n * 2
}

// 基本異步調用
test-basic = () {
    h = run compute-async(21)
    r = awy h
    print(r)  // 42
}

// 並發多個任務
test-concurrent = () {
    h1 = run compute-async(10)
    h2 = run compute-async(20)
    r1 = awy h1
    r2 = awy h2
    print(r1)  // 20
    print(r2)  // 40
}

// 內聯等待
test-inline = () {
    r = awy run compute-async(5)
    print(r)  // 10
}
```

> **命名規則**：異步函數名必須以 `-async` 結尾（如 `compute-async`、`fetch-data-async`）。不使用 `async` 關鍵字聲明。

### 多重賦值

函數可以返回多個值，調用時使用多重賦值接收：

```nolang
// 函數定義返回多個結果參數
swap = (a i64, b i64) (x i64, y i64) {
    x = b
    y = a
}

// 多重賦值
a, b = swap(5, 3)

// 也支援作為 match arm 的 body
val: {
    ok -> a, b = parse-pair(it)
    -> return
}
```

## 數組與切片

容器存儲數據副本，原變量與容器獨立，杜絕懸垂引用。

**定長数组arr：**

```nolang

// 使用定長数组
a [3] = [1, 2, 3]    // 长度为 3 的定長数组 i64
a [3]u16 = [1, 2, 3] // 指定类型的定長数组

a [?]u16 = [1, 2, 3] // 自動推斷長度
```

**變長數組vec：**

```nolang
v = [1, 2, 3]     // 變長數組 i64
bs = [0x11, 0x22, 0x33]
v []u8 = [1, 2, 3] // 指定类型的變長數組

// 預分配（避免反覆擴容）
v = with-cap(100)          // len=0, cap=100（需 push 後索引）
v = with-len(100)          // len=100, cap=100（可直接索引）
v = with-cap-len(200, 100) // len=100, cap=200（預留擴容空間）
```

**切片slice（視圖，非獨立類型）：**

切片是對原始資料的**視圖（view）**，不會複製資料，也不會產生新的獨立類型。
切片內部只記錄一個指向原始緩衝區的指標、長度和容量，因此：

- 通過切片修改元素會影響原始資料，反之亦然
- 切片不擁有資料，原始變數釋放後切片即失效
- 切片的類型由原始類型決定，方法自然適用，無需「繼承」機制

```nolang
// 支持arr/vec/str
// 支持範圍 和for <- 的表示一致
nums [5]u8 = [0, 1, 2, 3, 4]

nums[..] //  [0 1 2 3 4]
nums[1..] // [1 2 3 4]
nums[..4] // [0 1 2 3 4]
nums[2..3] // [2 3]
nums[1..3] // [1 2 3]
nums[1..3) // [1 2]
nums(1..3) // [2]

// 字符串
s = 'abc'
s[1..]   // 'bc'
s[1..s.len) // 'bc'
```

**切片的類型與方法：**

切片不生成新的獨立類型，只是原始類型的一個視圖（調整了起始指標和長度）。
因此原始類型的方法直接可用：

| 原始類型 | 切片視圖類型 | 可用方法 |
| -------- | ------------ | -------- |
| `arr` (`[n]t`) | `[]t<range>` | `[]t` 的所有方法（如 `len`、`push`、`pop`、`contains`、`reverse`、`clone`、`fill`、`to-arr` 等） |
| `vec` (`[]t`) | `[]<range>` | 同上 |
| `str` | `str<range>` | `str` 的所有方法（如 `to-upper`、`to-lower`、`index`、`contains`、`slice`、`copy`、`fill` 等） |

```nolang
// arr 切片 → vec 視圖，共享 arr 的底層記憶體
a [5]u8 = [0, 1, 2, 3, 4]
s = a[1..4]    // s 是 []u8 視圖，指向 a 的記憶體
n = s.len      // slice.len

// vec 切片 → vec 視圖，共享 vec 的底層記憶體
v = [10, 20, 30, 40, 50]
s = v[2..]     // s 是 []i64 視圖
s.reverse(s.len)  // slice.reverse

// str 切片 → str 視圖，共享 str 的底層記憶體
s = 'Hello World'
sub = s[6..]   // sub 是 'World' 視圖
upper = sub.to-upper()  // str.to-upper

// 通過切片修改元素會影響原始資料
data = [10, 20, 30, 40, 50]
view = data[1..4]    // view = [20, 30, 40]
view[0] = 99         // 修改 view 的元素
// data[1] 現在也是 99，因為 view 共享 data 的記憶體
```

### 索引

```nolang

// 字符串獲取char （字符，不是字節）
str[i]

 // arr、vec獲取元素
arr[i]
vec[i]

 // map 獲取 value
map[str]

```

## 結構體

結構體定義和字面量都必須使用多行形式，每個字段獨佔一行，字段之間不以逗號分隔，末尾也不跟逗號。

```nolang
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

## 方法

方法定義在類型上，使用 `.` 引用接收者（receiver）。

### 語法

```nolang
type.method-name = (params) (results) {
    // . 是接收者
}
```

### 規則

1. 方法名使用 `type.method` 格式，type 必須是已定義的類型
2. 接收者不需要顯式聲明參數，在方法體內用 `.` 引用
3. 調用時使用 `receiver.method(args)` 語法
4. 返回值放在第二組括號中，與普通函數一致

### 示例

```nolang
// str 方法
str.to-upper = () (out str) {
    out.len = .len
    i = 0
    {
        c = .[i]
        {
            c >= 97 && c <= 122 -> out[i] = c - 32
            -> out[i] = c
        }
        i = i + 1
    } (i < .len)
}

// char 方法
char.is-digit = () (result bool) {
    result = false
    . >= 48 && . <= 57 -> result = true
}

// struct 方法
user {
    name str
    age i64
}

user.greet = () {
    print('Hello, ' - .name)
}

// 調用
s = 'hello'
u = s.to-upper()     // receiver.method()
c char = 5
d = c.is-digit()     // receiver.method()
u = user{
    name: 'Alice'
    age: 30
}
u.greet()
```

## 接口

```nolang
// 定義接口
json {
    to-json()
}

// 接口默認實現
json.to-json = () {
}

// 接口實現
user json {
    name str
    age i64
}

// 重寫 + 調用父實現
user.to-json = () {
    // 父實現
    ..to-json()
}

user.other = () {
    // 當前實現
    .to-json()

    // 父實現
    ..to-json()
}
```

### 特殊接口

```nolang
file enter, leave {
}
```

## 枚舉

```nolang

// red=0, green=1, blue=2
color {
    red,
    green,
    blue,
}

// 在普通方法中，a,b,c 實際是定義的a=0，b=1, c=2... 這是和其他語言不一致的地方。
// 所以正常不能用逗號的方式定義多個變量

// 這是一個特殊枚舉, 可以有類型，有逗號， 有別名
enum-name {
    a t,
    b u,
    c v,
}

// 注意這是一個普通的struct，多個字段沒有逗號
struct-name {
    a t
    b u
    c v
}
```

### 枚舉值引用

**規則：枚舉值必須使用 `枚舉類型.值` 的限定方式引用，不能直接使用裸值。**
這樣可以防止命名衝突，也使外部包無法直接使用具體值。

```nolang
// ❌ 錯誤：直接使用裸值
kind = null
yes = e.is(io)

// ✅ 正確：使用限定方式
kind = json-kind.null
yes = e.is(code.io)
```

> 枚舉類型可用於結構體字段類型、函數參數類型和返回值類型。
> 定義枚舉的模組內部和外部都應使用 `枚舉類型.值` 的方式引用枚舉值。

## enter/leave

實現了 `enter` / `leave` 接口的類型，在作用域進入和離開的時候自動調用：

```nolang
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

    // 自動 f.enter()
    f = file{
        path: 'data.txt',
    }

    // 使用 f
    // 自動 f.leave()
    read(f)
}
```

### 可空類型(option)

在類型前面加 `?` 表示可空類型：

可空類型變量可以合法持有空值/错误值，編譯器會進行相應的空值檢查。

```nolang

o ?i64
o = nil          // 設為空
o = 42           // 設為有值
o = err('msg')   // 設為錯誤

nullableValue ?[]str
nullableString ?str

// 修改可空類型
nullableString = 'test'

// 設置錯誤
nullableString = err('some error')

// 可通過match判斷
x: {
    err -> log(it)
    nil -> log(it)
    ->
        do-right-thing(it)
}

// 強制解包
// 取消實現
//!x.say()
```

### 風格指引：使用 ?t option 取代 (val, ok)

當函數可能失敗或返回空值時，**應優先使用 `?t` option 類型**，而非 `(val t, ok bool)` 雙返回值模式。

`?t` 是標籤列舉，有三種狀態：`ok`（有值）、`nil`（空值）、`err`（錯誤）。正常值會隱性綁定，當操作只是找不到值時用 `nil`，當操作遇到實際錯誤時用 `err(...)`。

```nolang
// ❌ 不推薦：雙返回值模式
stack.pop = () (val i64, ok bool) {
    .n == 0 -> return
    val = .data[.n]
    ok = true
}

// ✅ 推薦：option 類型（nil 表示空，err 表示錯誤）
stack.pop = () (val ?i64) {
    .n == 0 -> {
        val = nil
        return
    }
    val = .data[.n]
}

// ✅ 返回錯誤
file.read = () (data ?str) {
    .fd < 0 -> {
        data = err('file not open')
        return
    }
    // ... 讀取資料
    data = buf
}
```

使用 match 解包 option：

```nolang
val = s.pop()
val: {
    nil -> print('empty')
    err -> print(it)          // it = 錯誤訊息
    -> print(it)              // it = 彈出的值
}
```

**適用場景：**
- `pop` / `peek` 等可能為空的容器操作 → `?t`（`nil` = 空）
- `read-line` / `read-byte` 等 I/O 操作 → `?str` / `?i64`（`nil` = EOF，`err` = 錯誤）
- `lookup` / `get` 等查找操作 → `?t`（`nil` = 未找到）
- `parse` / `from-str` 等解析操作 → `?t`（`nil` = 空，`err` = 無效輸入）
- `accept` / `dial` 等網路連接 → `?conn`（`nil` = 無連接，`err` = 錯誤）

**nil vs err：** 當缺失是正常/預期的結果（空堆疊、鍵不存在、EOF）時用 `nil`；當缺失代表實際的錯誤狀態（I/O 失敗、輸入無效、連接被拒）時用 `err('msg')`。

**例外：** 當函數需要返回多個獨立值（如 `(name str, value str, ok bool)`）時，可保留多返回值模式。

### 泛形

```nolang
arr_to_vec = (arr [n]t) (out []t) {
    i <- [0..n): {
        out[i] = arr[i]
    }
}
```

### 類型強制轉換

```nolang

// 返回類型名稱字符串
a = typeof(x)

// `as` 僅允許用於 FFI 指標型別轉換（如 *byte、**byte、*i64）
// 整數內部皆為 i64，無需顯式轉換
y = x as *byte
```

### 整數賦值型別檢查

編譯器會對整數賦值進行型別檢查，防止不安全的窄化（narrowing）導致資料遺失。以下是規則：

#### 隱式拓寬（安全，自動允許）

窄整數型別的值可以自動賦值給更寬的型別，因為目標型別的範圍完全包含來源型別：

```nolang
b byte = 200
i i64 = b        ; ✓ byte 範圍 [0,255] ⊆ i64 範圍
u u32 = b        ; ✓ byte 範圍 ⊆ u32 範圍
```

#### 整數字面量賦值

整數字面量（預設推斷為 `i64`）可以賦值給任何範圍包含該值的整數型別：

```nolang
n u8 = 200       ; ✓ 200 ∈ [0,255]
m u8 = 300       ; ✗ 300 > 255，編譯錯誤
big u64 = 18446744073709551615  ; ✓ 2^64-1，u64 最大值
```

#### 不安全的窄化（編譯錯誤）

將寬型別的變數直接賦值給窄型別會導致編譯錯誤，因為可能造成資料遺失。錯誤訊息會附帶**可操作的修復提示**，建議如何用位元運算安全窄化：

```nolang
d u64 = 42
h u32 = d        ; ✗ cannot assign u64 value to u32 variable 'h'; hint: narrow safely with a bitwise mask (e.g. `& 4294967295`) or right shift (e.g. `>> 32`)
h u16 = d        ; ✗ cannot assign u64 value to u16 variable 'h'; hint: narrow safely with a bitwise mask (e.g. `& 65535`) or right shift (e.g. `>> 48`)
h u8 = d         ; ✗ cannot assign u64 value to u8 variable 'h'; hint: narrow safely with a bitwise mask (e.g. `& 255`) or right shift (e.g. `>> 56`)
x u32 = d + 1    ; ✗ 加法結果仍為 u64，不安全
y u32 = foo()    ; ✗ 函式呼叫結果型別不匹配
```

> **修復提示**：編譯器會根據目標型別自動計算精確的 mask 值和位移量。按照提示套用 mask 或位移後，即可安全窄化（見下節）。
>
> **有號目標型別**：對 `i8`/`i16`/`i32`/`i64` 等有號整數，提示會說明位元窄化不適用（符號位元截斷語義不明確），建議改用顯式範圍檢查。

#### 位元運算安全窄化（自動允許）

當賦值的右側是**位元運算表達式**（`&`、`|`、`^`、`<<`、`>>`）且目標型別為**無號整數**（`u8`/`u16`/`u32`/`u64`/`byte`）時，編譯器允許隱式窄化——因為截斷高位是位元操作的標準語義，不會造成非預期的資料遺失：

```nolang
d u64 = 42

; ✓ mask 運算：結果必 ≤ mask 值，安全落入 u32
h u32 = d & 67108863          ; mask = 2^26-1 < 2^32
h u32 = d & 4294967295        ; mask = 2^32-1，正好 u32 範圍

; ✓ 位移運算：右移後高位為 0，安全
hi u32 = d >> 32              ; u64 >> 32 留 32 bits

; ✓ XOR / OR 組合
c u32 = a ^ b                 ; 位元運算結果
b byte = v & 255              ; mask 到 byte 範圍

; ✓ 複合位元運算（常見於密碼學/編解碼）
s u32 = (key[0] & 255) | ((key[1] & 255) << 8) | ((key[2] & 255) << 16) | ((key[3] & 255) << 24)
```

> **為什麼允許？** 位元運算（mask、位移、XOR、OR）的語義就是構造一個位元模式。賦值給窄的無號型別時，截斷高位是刻意的操作——開發者已透過 mask 或位移保證了結果的範圍，或刻意丟棄高位。這是密碼學（如 ChaCha20、Poly1305、Blake2）和編解碼代碼中的標準模式。

> **僅限無號目標型別。** 對有號整數目標（`i8`/`i16`/`i32`/`i64`），即使右側是位元運算也會報錯，因為符號位元的截斷語義不明確：
> ```nolang
> d u64 = 42
> h i32 = d & 4294967295   ; ✗ 仍報錯：有號目標不適用安全窄化
> ```

> **頂層必須是位元運算。** 只有當表達式的頂層運算子是 `&`/`|`/`^`/`<<`/`>>` 時才放行。加法、減法、函式呼叫、直接變數引用等不在此列：
> ```nolang
> d u64 = 42
> h u32 = d              ; ✗ 頂層是 Identifier，不是位元運算
> h u32 = d + 1          ; ✗ 頂層是 +，不是位元運算
> ```

### 模块系统

- 每个文件就是一个模块
- 文件名和文件夹名使用中连接符

```shell
utils/
└── helper.no    // 模块名为 utils/helper
```

### 導入模塊

> **新式語法使用 `#` 導入。舊式 `use` 關鍵字仍可使用，但已廢棄（deprecated），建議改用 `#`。**

```nolang
// 標準庫（新式語法，推薦）
# std/math.add

// 遠程模塊（非std/開頭）
# github.com/utils/math.add

// 本地模塊，必須/開頭
# /utils/math.add

// 別名
# std/math.add a

// ── 以下為舊式語法（deprecated，仍可使用但不推薦）──
// use std/math.add
// use github.com/utils/math.add
// use /utils/math.add
// use std/math.add a
```

### 導出模塊

僅適用於lib.no

```nolang
@ std/math.add a
```

### FFI（`#{c}` 註解）

透過 `#{c}` 註解宣告外部 C 函式，實現 FFI（Foreign Function Interface）。

**語法**：`#{c}` 獨立一行，標記下一行為 FFI 宣告。`#{c}` 是註解系統的 FFI 語言鍵，也支援 `#{cpp}`、`#{rust}` 等其他語言。舊語法 `#c` 仍向後相容。

**私有宣告**：名稱以 `_` 開頭表示私有（不導出），C ABI 符號自動去除前綴 `_` 並將連字號轉為底線。

**不再需要分開檔案**：FFI 宣告與普通代碼可寫在同一個 `.no` 檔案中。

**指針型別語法**：FFI 中使用 C 風格的 `*T`、`**T`、`***T` 表示指針，必須有具體型別 `T`。普通代碼不能使用此語法。

| 語法      | 含義              | LLVM IR  | 用途                     |
| --------- | ----------------- | -------- | ------------------------ |
| `*byte`   | 指向 byte 的指針  | `i8*`    | 不透明指標（如 db handle） |
| `**byte`  | 雙重指針          | `i8**`   | 輸出參數（如 `sqlite3**`） |
| `***byte` | 三重指針          | `i8***`  | 極少見的三重間接           |

```nolang
// sqlite.no — FFI 綁定與安全包裝在同一檔案
// 編譯器自動將連字號 (-) 轉為底線 (_) 以匹配 C ABI 符號
// 以 _ 開頭的名稱為私有，C ABI 符號自動去除前綴 _

// 基本型別參數
#{c}
c-strlen = (s str) (n i64)

// 指針參數（*byte = 不透明指標），私有宣告
#{c}
_sqlite3-close = (db *byte) (rc i32)

// 雙重指針（**byte = 輸出參數，呼叫後值自動存回變數），私有宣告
#{c}
_sqlite3-open = (filename str, db **byte) (rc i32)

// 多個指針參數，私有宣告
#{c}
_sqlite3-exec = (db *byte, sql str, callback *byte, arg *byte, errmsg *byte) (rc i32)
```

```nolang
// 同一檔案中的安全包裝

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

### 註解系統（`#{...}`）

`#{...}` 是通用註解系統，以逗號分隔的鍵值對列表。支援以下值類型：

| 語法 | 類型 | 範例 |
| --- | --- | --- |
| 獨立鍵 | 布爾 | `#{debug}` |
| 數值 | 整數 | `#{max=100}` |
| 文字 | 字串 | `#{name='hello'}` |
| 識別字 | 識別字 | `#{mode=fast}` |
| 陣列 | 陣列 | `#{derive=[Serialize, Deserialize]}` |
| 範圍 | 範圍 | `#{range=[0..256)}` |

多個鍵值對以逗號分隔：

```nolang
#{derive=[Serialize, Deserialize], range=[0..256), max=100, debug}
```

範圍語法支援四種括號組合：
- `[a..b]` — 兩端閉區間
- `[a..b)` — 左閉右開
- `(a..b)` — 兩端開區間
- `(a..b]` — 左開右閉

FFI 註解 `#{c}` 是註解系統的特殊形式，當註解包含 FFI 語言鍵（`c`、`cpp`、`rust` 等）且後續為函式宣告時，編譯器將其識別為 FFI 綁定：

```nolang
// #{c} 帶額外註解
#{c, debug}
_sqlite3-open = (filename str, db **byte) (rc i32)
```

#### 註解附加到宣告

非 FFI 註解會自動附加到緊隨其後的宣告（變數宣告、結構體定義），可用於為數值型別（如 `num`、`i64` 等）標記範圍限制等元數據：

```nolang
// 變數宣告帶 range 註解
#{range=[0..256)}
x num = 42

// 結構體定義帶註解
#{derive=[Serialize, Deserialize]}
point {
    x i64
    y i64
}

// 結構體欄位帶 range 註解（可用於 num 等數值型別）
person {
    #{range=[0..150]}
    age num
    #{range=[0..256)}
    score i64
    name str
}
```

`range` 註解特別適用於 `num` 型別（`num = int | float`），用於標記數值的有效範圍。範圍值可以是整數或識別字：

```nolang
// 使用常量識別字作為範圍邊界
#{range=[i8.MIN..i8.MAX]}
val i8 = 100
```

#### 平台註解

平台註解是編譯期過濾器，根據目標平台決定是否包含代碼。使用**扁平化鍵**（如 `#{mac-arm64}`）同時指定 OS 與架構，無歧義，附加到緊隨其後的宣告上。不匹配的代碼完全不參與編譯——不生成 LLVM IR，不進行類型檢查。

**支援的平台鍵（6 種扁平組合）：**

| 鍵 | 匹配 |
| --- | --- |
| `#{linux-amd64}` | Linux x86_64 |
| `#{linux-arm64}` | Linux ARM64 |
| `#{win-amd64}` | Windows x86_64 |
| `#{win-arm64}` | Windows ARM64 |
| `#{mac-amd64}` | macOS x86_64（Intel） |
| `#{mac-arm64}` | macOS ARM64（Apple Silicon） |

```nolang
#{mac-arm64}
print('running on macOS ARM64')

#{linux-amd64}
print('running on Linux x86_64')

#{win-amd64}
print('running on Windows x86_64')

// 平台特定的變數
#{mac-amd64}
#{mac-arm64}
sep = '/'

#{win-amd64}
#{win-arm64}
sep = '\\'

// 平台特定的函數
#{mac-arm64}
#{mac-amd64}
greet = () {
    print('hello from mac')
}

#{linux-amd64}
#{linux-arm64}
greet = () {
    print('hello from linux')
}

greet()
```

同一宣告上的多個鍵為 **OR** 關係——任一匹配即包含。因每個鍵已同時指定 OS 與架構，無需 AND 邏輯。

| 註解 | 含義 |
| --- | --- |
| `#{mac-arm64}` | 僅 macOS ARM64 |
| `#{mac-amd64, mac-arm64}` | macOS 任意架構 |
| `#{linux-amd64, win-amd64}` | Linux x86_64 **或** Windows x86_64 |
| `#{mac-arm64, linux-arm64}` | macOS ARM64 **或** Linux ARM64 |

```nolang
// macOS 和 Linux（所有架構）都執行
#{mac-amd64, mac-arm64, linux-amd64, linux-arm64}
shared = () {
    print('unix-like')
}

// 僅 Windows x86_64
#{win-amd64}
reg-key = () {
    print('reading registry on win/x64')
}

// 僅 macOS ARM64（Apple Silicon）
#{mac-arm64}
neural = () {
    print('Apple Neural Engine available')
}
```

使用 `os.get-arch()` 可在執行期取得當前架構，使用平台註解則在編譯期包含或排除代碼。
