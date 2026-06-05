---
sidebar_position: 3
---

# 标准库

## fmt — 格式化输出

```nolang
printf(fmt str, ...)     // 格式化输出
print(...)               // 打印
println(...)             // 打印并换行
println-empty()          // 打印空行
```

## math — 数学函数

```nolang
abs, sqrt, sin, cos, tan, asin, acos, atan, atan2
sinh, cosh, tanh, ceil, floor, round, trunc
exp, log, log10, log2, pow, hypot, cbrt, fmod
max, min, degrees, radians
```

## strconv — 字符串转换

```nolang
// 字符串 → 数值
str-to-i8(s), str-to-i16(s), str-to-i32(s), str-to-i64(s)
str-to-u8(s), str-to-u16(s), str-to-u32(s), str-to-u64(s)
str-to-f32(s), str-to-f64(s)
str-to-bool(s), str-to-byte(s), str-to-char(s)

// 数值 → 字符串
i8-to-str(v), i16-to-str(v), i32-to-str(v), i64-to-str(v)
u8-to-str(v), u16-to-str(v), u32-to-str(v), u64-to-str(v)
f32-to-str(v), f64-to-str(v)
bool-to-str(v), byte-to-str(v), char-to-str(v)
```

## os — 操作系统接口

```nolang
get-env(key)       // 获取环境变量
set-env(key, val)  // 设置环境变量
get-wd()           // 获取当前目录
ch-dir(path)       // 切换目录
mkdir(path, mode)  // 创建目录
remove(path)       // 删除文件/目录
rename(old, new)   // 重命名

open-read(path)    // 打开文件读
open-write(path)   // 打开文件写
read(fd)           // 读取
write(fd, data)    // 写入
close(fd)          // 关闭

exit(code)         // 退出进程
get-pid()          // 获取进程 ID
host-name()        // 获取主机名
now()              // 当前时间戳
sleep(seconds)     // 睡眠
```

## map/linked-hash-map — 有序哈希表

```nolang
// i64→i64 有序哈希表（固定容量，线性探测）
// 需要调用方提供平行数组

cap = 16
keys [16]i64 = [0:16]
vals [16]i64 = [0:16]
occ  [16]i64 = [0:16]
nxt  [16]i64 = [-1:16]
prv  [16]i64 = [-1:16]
head = -1
tail = -1
sz   = 0

map-put(cap, keys, vals, occ, nxt, prv, head, tail, sz, 42, 100, _)
map-get(cap, keys, vals, occ, nxt, prv, head, tail, sz, 42, found, result)
map-remove(cap, keys, vals, occ, nxt, prv, head, tail, sz, 42, _)
map-contains(cap, keys, vals, occ, nxt, prv, head, tail, sz, 42, found)
map-len(size, n)
map-is-empty(size, empty)
map-for-each(cap, keys, vals, occ, nxt, prv, head, tail, size, cb-key, cb-val)
```
