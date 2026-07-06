---
sidebar_position: 2
---

# 語法

## 註釋

```nolang
// 只允許單行註釋
// 單行內不允許使用; 要分成多行
```

> **規則：每行一條語句，禁止使用分號 `;` 將多條語句寫在同一行。**
> 這條規則同樣適用於註釋中的程式碼範例。即使在註釋裡，也不應該用分號把多條語句放在同一行，以免給讀者造成困惑。
>
> ```nolang
// ❌ 錯誤：註釋中使用分號合併多條語句
// h0 = 1732584193; h1 = 4023233417; h2 = 2562383102

// ✅ 正確：每條語句獨立一行
// h0 = 1732584193
// h1 = 4023233417
// h2 = 2562383102
```

## 數據類型

基礎類型

- byte
- bool // 只允許小寫
- char // 字符類型，一個中文一個字符，無引号包裹
- str // 字符串類型，單引號包裹
- i8
- i16
- i32
- i64 // 數字默認類型，不區分架構
- u8
- u16
- u32
- u64
- f32
- f64

容器類型

- obj // 對象
- map // 映射
- arr // 定長數組
- vec // 變長數組
- slice // 切片

- \* // 指針 僅限 FFI #{c} 宣告與標準庫
- any // 任意類型 僅限標準庫

高級類型

- bigint
- err

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

// 顯式類型標注
a u64 = 10

// 字符（不用引號）
c char = 中

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

## 命名規則

變量名、函數名、結構体名等可以以下劃線開頭，後續可以使用中連接符、字母、數字組成，不能以數字开头，不能以中連接符結尾，不能連續多個中連接符

**大小寫慣例：**
- **全局常量、全局變量**：使用大寫字母（如 `NOLANG`、`MAX-SIZE`）
- **局部變量、函數參數**：使用小寫字母（如 `hex-chars`、`data-len`）
- **函數名、結構體名**：使用小寫字母（如 `sha1-block`、`db-mysql`）

```nolang
// 全局數據使用大寫字母，包括全局常量、全局變量等
NOLANG = 'nolang'

// 私有
_NOLANG = 'nolang'

x1 = 10
x = 10
_x = 10
foo-bar = 42
hello-world = 'Hello World'
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
    i < n: {
        out[i] = s[i]
        i = i + 1
    }
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

函數通過**修改入參**來傳遞結果，`...` 僅用於提前終止，不能跟結果。

Nolang 的函數有以下特點：

- 函數預設無返回值，所有數據交互僅通過參數傳遞
- 所有函數參數均為引用型別，修改參數會直接影響調用方的數據
- 函數內的變量在函數退出時自動銷毀

Nolang 的函數不提供返回值機制，所有輸出結果均透過參數本身完成。

系统函数允许语法糖形式的返回值，方便用户使用，由于底层依然是通过入参完成，所以不会有新变量返回，内部是安全的。

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

> **Deprecated 語法（n 版本後移除）**：`for { }` / `for cond { }` / `for init, cond, update { }` / `for i in [..) { }` / `match x { }` / `if/elif/else { }` 仍可解析但會輸出 deprecation warning。請改用下方「**新式語法**」。

### 新舊對照

| 舊式                         | 新式                                               |
| ---------------------------- | -------------------------------------------------- |
| `for { }` 無限循環           | `! { }`                                            |
| `for cond { }` 條件循環      | `cond: { }`（裸條件 for）                          |
| `for i=0, i<n, i++ { }` 計數 | `n * { }`（常數計數）或 `i <- [0..n): { }`（變數） |
| `for i <- [a..b] { }` 範圍   | `i <- [a..b]: { }`                                 |
| `for i in [a..b) { }` 範圍   | `i <- [a..b): { }`                                 |
| `match x { ... }` 匹配       | `x: { ... }`                                       |
| `if/elif/else { }` 分支      | `{ cond -> body }`                                 |
| `continue`                   | `**`                                               |
| `break`                      | `*`                                                |
| `return`                     | `...`                                              |

### Loop / While / for-in

```nolang
// 无限循环
! {
    ...
}

// 限定執行次數
10 * {
}

// 區間語法（未來會支持 map, arr, vec）
i <- [a..b]: {     // 闭区间：a ≤ i ≤ b
}
i <- (a..b]: {     // 左开右闭：a < i ≤ b
}
i <- [a..b): {     // 左闭右开：a ≤ i < b
}
i <- (a..b): {     // 开区间：a < i < b
}
i <- [5..0]: {   // 递减
}
i <- 'abc': {   // 遍历字符串中的每个字符
}

// ❌ 明確拒絕
//   區間邊界必須是整數；不支持嵌套表達式
//   for i <- [1.5..5.5] { }   // 編譯錯誤
//   for i <- [0..[1..5][0]] { } // 語法錯誤

// 條件循環（沒有專用新式）
for x == 1 {
    do-something()
}
```

### 跳出 / 跳過 / 提前返回

```nolang
i <- [0..10): {
    *      // break
    **     // continue
    ...    // return/terminate
}
```

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
    User{id=1} -> print("管理員")
    User{name=n} -> print("用戶：", n)
    -> print("匿名")
}

score: {
    [0..59] -> print("不及格")
    [60..89] -> print("良好")
    [90..=100] -> print("優秀")
    -> print("分數非法")
}

num: {
    1 || 3 || 5 || 7 -> print("奇數小數")
    2 || 4 || 6 -> print("偶數小數")
    -> print("更大數字")
}

// 有返回值，最後一個語句/值
result = x: {
    1 -> 1
    2 -> 2 + 1
    -> a + b
}
```

> **for-in 體內 match 語義**：`i <- (a..b]: { 1 -> ... 2 -> ... }` 對每個迭代變量 `i` 執行一次 match 體（`1 ->` 等同於 `i == 1 ->`，依此類推）。這是每輪迭代執行一次 match 的語法糖。

### If / Else

```nolang
// 多分支（推薦新式）
{
    a == 1 ->
        a = 1
        b = 2
    a == 2 || a == 3 ->
        do-something()
    ->
        c = 0
}

// 單 if（保留）
x == 1 -> do-something()

// 三元表達式 condition ? true-value : false-value
c = flag ? 1 : 2
max = sum > 10 ? sum : 10
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
    for i < .len {
        c = .[i]
        if c >= 97 && c <= 122 {
            out[i] = c - 32
        } else {
            out[i] = c
        }
        i = i + 1
    }
}

// char 方法
char.is-digit = () (result bool) {
    result = false
    if . >= 48 && . <= 57 {
        result = true
    }
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
    if .n == 0 {
        return
    }
    val = .data[.n]
    ok = true
}

// ✅ 推薦：option 類型（nil 表示空，err 表示錯誤）
stack.pop = () (val ?i64) {
    if .n == 0 {
        val = nil
        return
    }
    val = .data[.n]
}

// ✅ 返回錯誤
file.read = () (data ?str) {
    if .fd < 0 {
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
    nil -> println('empty')
    err -> println(it)          // it = 錯誤訊息
    -> println(it)              // it = 彈出的值
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
    for i in [0..n) {
        out[i] = arr[i]
    }
}
```

### 類型強制轉換

```nolang

// 返回類型名稱字符串
a = typeof(x)

y = x as i64
```

### 模块系统

- 每个文件就是一个模块
- 文件名和文件夹名使用中连接符

```shell
utils/
└── helper.no    // 模块名为 utils/helper
```

### 導入模塊

```nolang
// 這裡是示例，實際上標準庫可能不需要明確導入
# std/math.add

// 遠程模塊（非std/開頭）
# github.com/utils/math.add

// 本地模塊，必須/開頭
# /utils/math.add

// 別名
# std/math.add a
```

### 導出模塊

僅適用於lib.no

```nolang
@ std/math.add a
```

### FFI（#{c} 註解）

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

### 註解系統（#{...}）

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

`range` 註解特別適用於 `num` 型別（`num int | float`），用於標記數值的有效範圍。範圍值可以是整數或識別字：

```nolang
// 使用常量識別字作為範圍邊界
#{range=[i8.MIN..i8.MAX]}
val i8 = 100
```
