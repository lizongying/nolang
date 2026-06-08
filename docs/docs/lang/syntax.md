---
sidebar_position: 2
---

# 語法參考

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
- arr // 數組
- vec // 切片

- * //  指針（ptr） 標準庫專用
- any // 任意類型 標準庫專用

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

// arr 定長數組
arr [3] = [1, 2, 3]  

// vec 動態數組（切片）
vec = [4, 5, 6]    

// 顯式類型（切片）
typed []u8 = [1, 2, 3]  

// 數組
typed [3]u16 = [1, 2, 3]

// 變量名只可以使用中連接符和小寫字母
foo-bar = 42
hello-world = 'Hello World'
```

## 函數定義

函數通過**修改入參**來傳遞結果，`return` 僅用於提前終止，不能跟結果。

```nolang
add(a i64, b i64, result i64) {
    result = a + b             // 通過參數返回結果
    return                     // 提前終止（可選）
}

// 可變參數
add3(a ..i64) {
}


// 函數調用
add(1, 2, sum)                 // sum == 3
```

## 流程控制

```nolang

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

// match

// ✅ match 语句：分支体是代码块
x {
    1|
        a = 1
        b = 2
        // 多行，不返回值
    2|
        do-something()
    |
        c = 0
}

// ✅ match 表達式
result = x {
    1| 1       // 單一值
    2| 2 + 1     //簡單表達式
    | a + b
}

// ❌ 編譯錯誤：分支裡不能有表達式
result = x {
    1|
        a = 1     
}

// ❌ 編譯錯誤：語句形式不能有返回值
x {
    1| 1          
}

// 特殊match，沒有需要返回的值
{
    a == 1|
        a = 1
        b = 2

        // 多行 不返回值
    a == 2|
        do-something()
    |
        c = 0
}

// 判讀返回值可能有錯的情況
// it用於取參數
x {
    err| log(it)
    nil| log('nil')
    |
        do-right-thing(it)
}

// 三元表达式 condition ? true-value : false-value
c = flag ? 1 : 2
max = sum > 10 ? sum : 10

// 建議使用match語法或三元表達式替代if/else

// if/elif/else
if x > 5 {
    a = 1
} elif x < 0 {
    b = 2
} else {
    c = 0
}

// 作為表達式
max = if x > y { x } else { y }
```

## 數組與切片

```nolang
nums [5]u8 = [0, 1, 2, 3, 4]

// 操作（返回 vec）
nums[..]    // [0, 1, 2, 3, 4]
nums[1..]   // [1, 2, 3, 4]
nums[..3]   // [0, 1, 2]
nums[1..3]  // [1, 2, 3]
nums[1..3)  // [1, 2]
```

## 結構體

```nolang
user {
    name str
    age i64
}

u = user { 
    name: 'Alice',
    age: 30
}
u.name = 'Bob'
```

## 方法

```nolang
user.greet() {
    print('Hello, ' + .name)
}
```

## 接口

```nolang
// 定義接口
json {
    to-json()
}

// 接口實現
user json {
    name str
    age i64
}

// 接口默認實現
json.to-json() {
    return '{...}'
}

// 重寫 + 調用父實現
user.to-json() {
    ..to-json()
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

// 在普通方法中，a,b,c   實際是定義的a=0，b=1, c=2... 這是和其他語言不一致的地方。
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

read-file() {

    // 自動 f.enter()
    f = file{ 
        path: 'data.txt',
    }
    
    // 使用 f
    // 自動 f.leave()
    read(f) 
}
```

**數組arr（固定大小）：**

```nolang

// 使用數組
numbers = [1, 2, 3, 4, 5]
print(numbers)

a [3] = [1, 2, 3]   
a [3]u16 = [1, 2, 3] 
```

**切片vec（動態大小）：**

```nolang
v = [1, 2, 3]   
v []u8 = [1, 2, 3] 

b = 0x00
bs = [0x11, 0x22, 0x33]
```

### 可空類型(option)

在類型前面加 `?` 表示可空類型：

可空類型變量可以合法持有空值/错误值，編譯器會進行相應的空值檢查。

```nolang

nullableValue ?[]str
nullableString ?str

// 修改可空類型
nullableString = 'test'

// 設置錯誤
nullableString = err('some error')

// 可通過match判斷
x {
    err| log(it)
    nil| 
    |
        do-right-thing(it)
}

### 泛形

```nolang
// 只允許單字母 a-z
arr_to_vec(arr [n]t) (out []t) {
    for i in [0..n) {
        out[i] = arr[i]
    }
}
```

### 類型轉換

```nolang

// 返回類型名稱字符串
a = typeof(x)

y = x as i64
```