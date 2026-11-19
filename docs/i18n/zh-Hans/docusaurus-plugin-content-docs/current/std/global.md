---
sidebar_position: 4.3
---

## 全域（Global）

### global — 全域内建函数

无需模组前缀即可调用的函数。仅有以下 7 个全域函数，其余跨模组调用都必须加模组前缀（例如 `fs.read`、`os.get-env`）：

```no
; 容量/长度构造（型别由赋值左侧推断）
s str = with-cap(256); 预分配 256 位元组 str（len=0）
v []i64 = with-cap(100); 预分配 100 元素切片（len=0）
s str = with-len(10); 长度为 10 的 str
v []i64 = with-len(100); 长度为 100 的切片
v []i64 = with-cap-len(200, 100); 容量 200、长度 100 的切片

; 也可作为 str/vec 方法调用：
s = ''.with-cap(256)
s = ''.with-len(10)
s = ''.with-len-cap(10, 256)
v = [].with-cap(100)
v = [].with-len(100)
v = [].with-len-cap(100, 200)

; 输出/格式化（具名格式字串 {name:spec}）
print('x={x}'); 具名格式，stdout + 换行
eprint('err {x}'); 具名格式，stderr + 换行
s = format('x={x}'); 返回格式化字串
print(a, b, c); 多参数：空格分隔，末尾只有一个换行
print('result={val}', 42, 'result={val}'); 多参数时，每个字面量各自是模板

; （{val} 从呼叫点作用域取值，不是 C 风格格式串）
;
; 栏位可以是「变数 / 栏位路径 / 无参方法 / 下标」，且可以逐层串接：
print('{user.name} {user.name.len()} {tags[0]}')
print('{m[\'k\']} {m[\'k\'].len()}'); map 取值（模板里用 \' 转义，或写 m["k"]）

; 只做「每层剥一个后缀」的降糖：带参方法（{s.slice(0)}）与任意表达式（{a + 1}）不支援。

; 行程控制
exit(0); 以状态码 0 结束行程（原 os.exit，已提升为全域）
```

---
