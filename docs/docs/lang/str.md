---
sidebar_position: 3
---

# 字串

Nolang 字串（`str`）為 union 型別（short ≤127 byte 存棧上 / long 存堆上），支援多種運算符與方法。

## 字串運算符

### 拼接（`-`）

使用 `-` 運算符拼接字串：

```nolang
// 字面量拼接
s = 'Hello' - ' ' - 'World'

// 與變量拼接
greeting = 'Hello, ' - name
```

### 重複（`*`）

使用 `*` 運算符重複字串：

```nolang
s = 'Hello' * 3
```

## 索引與切片

```nolang
s = 'Hello World'

// 索引取得 char（字符，不是字節）
c = s[0]           // c = 'H' 的碼點

// 切片（視圖，共享底層記憶體）
sub = s[6..]       // 'World'
sub = s[6..11]     // 'World'
sub = s[0..5)      // 'Hello'

// 長度
n = s.len          // byte 長度
n = s.count()      // code point 數（Unicode 字符數）
```

## 字串方法

```nolang
// 比較
ok = a.eq(b, n)               // 相等比較（方法）
c = s.compare(b)              // 字典序比較

// 查找
pos = s.index(sub)             // 子字串位置
ok = s.contains(sub)           // 是否包含
ok = s.starts-with(sub)        // 前綴判斷
ok = s.ends-with(sub)          // 後綴判斷
ok = s.empty()                 // 是否為空

// 轉換
out = s.to-upper()             // 轉大寫
out = s.to-lower()             // 轉小寫
out = s.trim()                 // 去首尾空白
out = s.trim-char(c)           // 去指定字元
out = s.repeat(n)              // 重複
out = s.reverse()              // 反轉
out = s.slice(start, end)      // 切片
val = s.replace-char(old, new) // 取代字元

// 轉換為其他型別
b = s.to-bytes()               // 轉 []byte
v = s.to-i64()                 // 轉 ?i64
v = s.to-f64()                 // 轉 ?f64
v = s.to-bool()                // 轉 ?bool

// 分割與連接
parts = s.split(sep)           // 分割（返回 []str）
out = ss.join(sep)             // []str 連接

// 複製與填充
dst = s.copy()                 // 字串複製
s.fill(val byte)               // 填充 byte 值
```

## 字串與數值轉換

```nolang
// 數值轉字串（方法）
s = i64.to-str()               // i64 轉字串
s = f64.to-str()               // f64 轉字串
s = bool.to-str()              // bool 轉 "true"/"false"
s = byte.to-str()              // byte 轉字串
s = char.to-str()              // char 轉字串

// 字串轉數值（返回 option）
v = s.to-i64()                 // 回傳 ?i64
v = s.to-i32()                 // 回傳 ?i32
v = s.to-u64()                 // 回傳 ?u64
v = s.to-f64()                 // 回傳 ?f64
```

## 自動長度追蹤

對 `s[i] = v` 賦值時，LLVM codegen 會自動更新 `len` 欄位為 `max(len, idx+1)`，不需手動設置 `.len`：

```nolang
s = ''
s[0] = 72                      // len 自動變為 1
s[1] = 105                     // len 自動變為 2

// 手動設置 .len 僅用於截斷（縮短）
s.len = 5
```
