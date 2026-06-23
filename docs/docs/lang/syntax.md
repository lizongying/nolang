---
sidebar_position: 2
---

# 語法

## 註釋

```nolang
// 只允許單行註釋
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

```nolang
// 舊式寫法
// 用for可以替代while/loop

// 无限循环 for { }
for {
    break
}

// 条件循环 for condition { }
for i < 5 {
    continue
}

// 經典三段式
for i=0; i < 5; i++ {
}

// 区间语法
// 未來會支持map, arr, vec
for i <- [a..b] {     // 闭区间：a ≤ i ≤ b
    // a, a + 1, ..., b
}

for i <- (a..b] {     // 左开右闭：a < i ≤ b
    // a + 1, a + 2, ..., b
}

for i <- [a..b) {     // 左闭右开：a ≤ i < b
    // a, a + 1, ..., b - 1
}

for i <- (a..b) {     // 开区间：a < i < b
    // a + 1, a + 2, ..., b - 1
}

for i <- [5..0] {   // 递减
}

for i <- [5..5] {   // 只執行5
}

for i <- (5..5) {   // 無
}

for i <- 'abc' {   // 遍历字符串中的每个字符
}

// ❌ 明确拒绝
for i <- [1.5..5.5] {  // 编译错误：区间边界必须是整数
    // 步长无法确定
}

// ⚠️ 不支持嵌套
for i <- [0..[1..5][0]] {  // ❌ 语法错误
}

// for 循環
for i < 10 {
    print(i)
    i = i + 1
}

// range for
for i in [0..10) {
    print(i)
}

// 命名循環 + break/continue
outer for i in [0..10) {
    inner for j in [0..10) {
        if j == 5 {
            continue outer
        }
        if i == 8 {
            break outer
        }
    }
}

// 舊式寫法
// if/elif/else
if x > 5 {
    a = 1
} elif x < 0 {
    b = 2
} else {
    c = 0
}
```

```nolang

// loop
// 一直循環執行
! {
}

// loop
// 限定執行次數
10 * {
}

// while
x == 1: {
    b = 2
}

// for
// 遍歷
i <- (a..b]: {
}


// continue
i <- (a..b]: {
    *
}

// break
i <- (a..b]: {
    **
}

// return
i <- (a..b]: {
    ...
}

// 舊式寫法
// switch
// 無返回值
switch x {
    case 1:
        a = 1
        b = 2
    case 2:
        do-something()
    default:
        c = 0
}

// switch
// 無返回值
x: {
    1|
        a = 1
        b = 2
    2|
        do-something()
    |
        c = 0
}

// switch
// 有返回值，最後一個語句/值
result = x: {
    1| 1       // 單一值
    2| 2 + 1     //簡單表達式
    | a + b
}

// if/else
{
    a == 1|
        a = 1
        b = 2
    a == 2|
        do-something()
    |
        c = 0
}

// 舊式寫法
// match
match x {
    case err:
     log(it)
    case nil:
     log('nil')
    default:
        do-right-thing(it)
}

// match
// 判讀返回值可能有錯的情況
// it用於取參數
x: {
    err| log(it)
    nil| log('nil')
    |
        do-right-thing(it)
}

// 三元表达式 condition ? true-value : false-value
c = flag ? 1 : 2
max = sum > 10 ? sum : 10

// 建議使用match語法或三元表達式替代if/else
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

```nolang
user {
    name str
    age i64
}

user.greet = () {
    print('Hello, ' - .name)
}
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

file.enter() {
    open(.path)
}

file.leave() {
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
    err| log(it)
    nil|
    |
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
