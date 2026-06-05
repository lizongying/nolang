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

## 语法特性

### 標識符

由字母、數字和中连接符組成，但不能以數字/中连接符开头
變量、函數名、結構体名等全部使用小寫子母。

```nolang
x
my-variable
user-name
```

### 變量聲明

Nolang 採用可推断聲明方式，尽量简洁

```nolang

// 变量声明（如可推断，可不用声明）
x = 1          // i64
y = 1.0        // f64 中间有.
name = 'hello' // str 单引号包裹
flag = true    // bool true/false 全小寫
name = 'World'
greeting = 'Hello, ' - name

// 需显式类型标注
a i8 = 2     // i8 类型无法自動推断的，需要标注
c char = 中  // char類型（一些語言裡叫rune）不用引號
b = x00 // byte類型

// 特殊类型标注
i8 = 3     // i8 如果变量名是类型名，则可以省略类型标注

// 中连接符变量名
foo-bar = 42
hello-world = 'Hello World'
```

### 数据类型

基礎類型

- byte // 字节类型
- bool // 布尔类型，true/false
- char // 字符类型，比如一個中文一個字符，無引号包裹
- str // 字符串类型，单引号包裹
- i8 // 8位有符号整数
- i16 // 16位有符号整数
- i32 // 32位有符号整数
- i64 // 整数类型，默认int64，不判断架构
- u8 // 8位无符号整数
- u16 // 16位无符号整数
- u32 // 32位无符号整数
- u64 // 64位无符号整数
- f32 // 32位浮点数
- f64 // 64位浮点数，默认浮点类型

容器類型

- obj // 对象类型
- map // 映射类型
- arr // 数组类型
- vec // 切片类型

- ptr // 標準庫專用
- any // 標準庫專用

高級類型

- bigint
- err

### 關鍵字

以下關鍵字具有特殊意義：

```nolnag
return  // 返回語句，终止函数
if      // 條件判斷
elif
else    // 條件分支
true    // 布爾真值
false   // 布爾假值
nil    // 空值
for
in
break
continue
defer // 暫時留著，不打算用
use
as
self
super
it
```

### 基本表達式

### 函數作用域變量覆蓋

在函數作用域內，如果重複使用相同的變量名進行賦值，Nolang 將其視為覆蓋重賦值，而非創建新的棧變量。不触发变量遮蔽
如果类型不同，语法不允许

### 統一引用傳遞模型

Nolang 採用統一引用傳遞模型，所有函數參數預設均為引用型別。這意味著：

- 函數內修改 = 外部變數直接改變
- 函數內對參數的任何修改，都會直接作用於調用方的原始數據
- 可修改，但不可销毁

### 函數定義

Nolang 的函數有以下特點：

- 函數預設無返回值，所有數據交互僅通過參數傳遞
- 所有函數參數均為引用型別，修改參數會直接影響調用方的數據
- 函數內的變量在函數退出時自動銷毀

Nolang 的函數不提供返回值機制，所有輸出結果均透過參數本身完成。

系统函数允许语法糖形式的返回值，方便用户使用，由于底层依然是通过入参完成，所以不会有新变量返回，内部是安全的。

```nolang
// 保留return，但return 后面不用跟返回值，仅用于终止函数
// 函数定义，普通用户不可以定义返回值，通过修改入参达到目的
add(a i64, b i64) {
    result = a + b
    return
    result2 = a + b
}

// 标准库内允许使用返回值形式，实际是一种语法糖，返回值会展开为入参
// add2(a i64, b i64)(c i64, d i64) {
//    c = a + b
//    d = c
// }

// 可變參數
add3(a ..i64) {
}

// 函数定义，无需关键字
add(a i64, b i64) {
    result = a + b
    println('result:', result)
}

// 匿名函数，函数定义的另一种方式
add = (a i64, b i64) {
}


(a i64) { println(a) }(10)

// 函数调用
add(a, b)


// 这是一种特例，由于add是标准库函数，有返回值
result = add(5, 3)

// 也可能有多个返回值
a, b = swap(5, 3)

res = 0

// 如果是用户自己的函数，只能这样调用。因为用户不允许定义返回值
add1(5, 3, res)

// 计算总和
sum = 0
for i < 10 {
    sum = sum + i
    i = i + 1
}
println('Sum:', sum)

// 使用数组
numbers = 5[1, 2, 3, 4, 5]
println(numbers)


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

// ✅ match 表达式：分支体是单一表达式
result = x {
    1| 1       // 单一值
    2| 2 + 1     //簡單表達式
    | a + b
}

// ❌ 编译错误：表达式分支里不能有代码块
result = x {
    1|
        a = 1      // 错误！表达式不能有语句
}

// ❌ 编译错误：语句形式不能返回值
x {
    1| 1           // 错误！
}

// 特殊match，没有需要判断的值
{
    a == 1|
        a = 1
        b = 2

        // 多行，不返回值
    a == 2|
        do-something()
    |
        c = 0
}

// 判讀返回值可能有錯的情況
x {
    err| log(it)
    nil| log('nil')
    |
        do-right-thing(it)
}

// arr/vec切割 返回vec
// 支持範圍 和for in 的表示一致
nums [5]u8 = [0, 1, 2, 3, 4]

nums[..] //  [0 1 2 3 4]
nums[1..] // [1 2 3 4]
nums[..4] // [0 1 2 3 4]
nums[2..3] // [2 3]
nums[1..3] // [1 2 3]
nums[1..3) // [1 2]
nums(1..3) // [2]

// 字符串返回字符串
s = 'abc'
s[1..]   // 'bc'
s[1..s.len) // 'bc'
```

### 控制流

### if 表達式

Nolang 的 `if` 表達式用於條件判斷：

```nolang

if x > 5 {
    do-something()
} elif {

} else {
    b = 1
}

// 条件语句
if sum > 10 {
    max = sum
} else {
    max = 10
}

// 三元表达式 condition ? trueValue : falseValue
c = flag ? 1 : 2
max = sum > 10 ? sum : 10
```

### for循環

```nolang
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
for i=0; i < 5;i++ {
}

// 区间语法
// 未來會支持map, arr, vec 
for i in [a..b] {     // 闭区间：a ≤ i ≤ b
    // a, a+1, ..., b
}

for i in (a..b] {     // 左开右闭：a < i ≤ b
    // a+1, a+2, ..., b
}

for i in [a..b) {     // 左闭右开：a ≤ i < b
    // a, a+1, ..., b-1
}

for i in (a..b) {     // 开区间：a < i < b
    // a+1, a+2, ..., b-1
}

for i in [5..0] {   // 递减
}

for i in [5..5] {   // 只執行5
}

for i in (5..5) {   // 無
}

for i in 'abc' {   // 遍历字符串中的每个字符
}

// ❌ 明确拒绝
for i in [1.5..5.5] {  // 编译错误：区间边界必须是整数
    // 步长无法确定
}

// ⚠️ 不支持嵌套
for i in [0..[1..5][0]] {  // ❌ 语法错误
}

// 命名循环
outer for i in [0..10) {
    inter for j in [0..10) {
        break outer  // 直接跳出外层循环
    }
}

```

### 数组和切片

容器存儲數據副本，原變數與容器獨立，杜絕懸垂引用。

**数组arr（固定大小）：**

```nolang
a [3] = [1, 2, 3]       // 长度为 3 的数组 i64
a [3]u16 = [1, 2, 3] //指定类型的数组
```

**切片vec（动态大小）：**

```nolang
v = [1, 2, 3]     // 动态切片 i64
v []u8 = [1, 2, 3] // 指定类型的切片

b = 0x00
bs = [0x11, 0x22, 0x33]
```

### 結構體

```nolang
user {
    name str
    age i64
}

u = user {
    name: 'abc'
    age: 20
}

u.name = 'def'
u.age = 25
println(u.name)
```

### 方法

```nolang
user {
    name str
    age i64
}

user.foo(a i64) {
    println(self.name)  // self 自动可用
}
```

### 繼承、接口

```nolang
// trait/interface
// to-json由於沒有實現，這個時候json實際就是接口。
// 但json又能有自己的默認實現
json {
    to-json()
}

json.to-json() {
    do-something()
}

user json {
    name str
    age i64
}

user.to-json() {
    super.to-json()
}
```

### 特殊接口

```nolang
file enter, leave {
}
```

### 枚舉

```nolang
// 這是一個枚舉/自增，有逗號
enum-name {
    a,
    b,
    c,
}

// 這是一個特殊枚舉，有逗號， 有別名
// 實現方式是tag+data
enum-name {
    a t,
    b u,
    c v,
}

// 這是一個普通的struct，多個字段沒有逗號
struct-name {
    a
    b
    c
}

// 在普通方法中，a,b,c   實際是定義的a=0，b=1, c=2... 這是和其他語言不一致的地方。
// 所以正常不能用逗號的方式定義多個變量

```

### 注释

```nolang
// 僅支持单行注释
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

// 強制解包
// 取消實現
//!x.say() 
```

```nolang

// 字符串獲取char （字符，不是字節）
str[i]

 // arr、vec獲取元素 
arr[i]
vec[i]

 // map 獲取 value 
map[str]

```

```nolang

//字符串拼接
'Hello' - ' ' - 'World'

//有變量
'Hello' - space - 'World'


 // arr、vec交集、並集
arr1 + arr2
arr1 - arr2

```

### 泛形

```nolang
// 只允许单字母 a-z
arr_to_vec(arr [n]t) (out []t) {
    for i in [0..n) {
        out[i] = arr[i]
    }
}
```

### option  

```nolang
// 使用 ?t 宣告 option 型別
o ?i64           // 宣告

o = nil          // 設為空 
o = val(42)      // 設為有值
o = err('msg')   // 設為錯誤 

// 等價
o = val(42) 
!?o = 42
```

### 類型轉換

```nolang

// 返回字符串？
a = typeof(x)

?y = x as i64
```

### 引用

```nolang
// 這裡是示例，實際上標準庫可能不需要明確引入
use std/math.add

// 遠程模塊（非std/開頭）
use github.com/utils/math.add

// 本地模塊，必須/開頭
use /utils/math.add

// 別名
use std/math.add a
```

### 入口函数

无需显式 `main()` 函数，文件最外层代码自动成为入口

```nolang
x = 10
println(x)
```

### 模块系统

- 每个文件就是一个模块
- 文件名和文件夹名使用中连接符
- 文件夹结构自动成为命名空间

```shell
utils/
└── helper.no    // 模块名为 utils-helper
```

### 運算符

```shell
=       // 賦值
==      // 等於比較
!=      // 不等於比較
+       // 加法
-       // 減法
*       // 乘法
/       // 除法
<       // 小於
>       // 大於
<=      // 小於等於
>=      // 大於等於
&&      // 邏輯與
||      // 邏輯或
!       // 邏輯非
?=      // 可空類型標記
(       // 左括號
)       // 右括號
{       // 左花括號
}       // 右花括號
,       // 逗號
```

#### 算術運算符

```shell
+   // 加法
-   // 減法
*   // 乘法
/   // 除法
```

#### 比較運算符

```shell
==  // 等於
!=  // 不等於
<   // 小於
>   // 大於
<=  // 小於等於
>=  // 大於等於
```

#### 邏輯運算符

```shell
&&  // 邏輯與
||  // 邏輯或
!   // 邏輯非
```

## 项目结构

```shell
nolang/
├── lexer/          # 词法分析器
├── parser/         # 语法解析器
├── build/          # 代码生成器
├── fmt/            # 代码格式化工具
├── lsp/            # Language Server Protocol
├── cli/            # 工具
└── std/            # 標準庫
```

---

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

| 模組 | 測試項目 | 驗證來源 |
|------|---------|---------|
| `crc32` | 空字串、hello、fox 字串 | `hash/crc32` IEEE |
| `fnv-1a` | 同上 | `hash/fnv` New32a |
| `sha256` | zero block 壓縮 | 內建壓縮函數比對 |
| `sha512` | zero block 壓縮 | 內建壓縮函數比對 |
| `aes-128` | NIST ECB KAT 加解密 | `crypto/aes` |
| `des` | 標準測試向量加解密 | `crypto/des` |

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

### 程序結構

- 檢查函數所有變量，儘量一次分配
- 函數內的變量在函數退出時自動銷毀
- 默認執行 main (main.no) 模塊
- 默認測試 test (test.no) 模塊
- 默認導出 lib (lib.no) 模塊

### nolang-cli

- init
- add
- remove
- update
- list

## cli使用

```bash
# 编译 Nolang 代码
cd src/build && go run . your_file.no

# 格式化代码
cd src/fmt && go run . your_file.no
```

### vscode插件

```shell
cd vscode-nolang
bun run package

magick -background none icon16x16.svg icon16x16.png
```

## TODO

- [ ] 重载函數
- [ ] 實現類型檢查器
- [ ] 實現編譯器
- [ ] 實現標準庫
- [ ] 實現錯誤處理
- [ ] 實現模塊引用
- [ ] 常量使用大寫字母和中連結線，不允許大小寫混合

### 內存安全機制

- **變數自動銷毀** 函數結束自動銷毀所有內部變數
- **禁止手動釋放** 避免誤刪導致的懸垂引用
- **值複製容器** 數組 / 切片存副本，與原變數分離， 原變量生命周期結束並銷毀時，容器內的數據不受任何影響
- **無 GC、無分配隱藏成本**

```nolang
// 
// 1. 协程创建：go 关键字
go {
    // 协程体
}

// 2. 通道：chan 类型
let ch: chan(int) = chan()

// 3. 收发：<- 操作符
ch <- 42          // 发送
let v = <-ch      // 接收

// 4. 带缓冲通道
let ch2: chan(str, 10) = chan(10) -->



do-some(ch chan) {
    ch <- 42    // 发送
    v = <-ch    // 接收
}

```

```shell
printf 'Content-Length: 130\r\n\r\n{"jsonrpc":"2.0","id":1,"method":"textDocument/formatting","params":{"textDocument":{"uri":"file:///test.no"},"options":{"tabSize":4,"insertSpaces":true}}}' | vscode-nolang/server/nolang-lsp 2>&1 | head -1
```