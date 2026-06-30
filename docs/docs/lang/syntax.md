---
sidebar_position: 2
---

# 語法

## 註釋

```nolang
// 只允許單行註釋
// 單行內不允許使用; 要分成多行
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

- \* // 指針 僅限標準庫
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

**切片slice：**

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
u = user{name: 'Alice', age: 30}
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
    open(.path)
}

file.leave = () {
    close(self)
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
    nil -> log('nil')
    ->
        do-right-thing(it)
}

// 強制解包
// 取消實現
//!x.say()
```

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
