---
sidebar_position: 4.3
---

## 全域（Global）

### global — 全域內建函數

無需模組前綴即可調用的函數。僅有以下 6 個全域函數，其餘跨模組調用都必須加模組前綴（例如 `fs.read`、`os.exit`）：

```no
; 容量/長度構造（型別由賦值左側推斷）
s str = with-cap(256)                   ; 預分配 256 位元組 str（len=0）
v []i64 = with-cap(100)                 ; 預分配 100 元素切片（len=0）
s str = with-len(10)                    ; 長度為 10 的 str
v []i64 = with-len(100)                 ; 長度為 100 的切片
v []i64 = with-cap-len(200, 100)        ; 容量 200、長度 100 的切片

; 也可作為 str/vec 方法調用：
s = ''.with-cap(256)
s = ''.with-len(10)
s = ''.with-len-cap(10, 256)
v = [].with-cap(100)
v = [].with-len(100)
v = [].with-len-cap(100, 200)

; 輸出/格式化（具名格式字串 {name:spec}）
print('x={x}')                          ; 具名格式，stdout + 換行
eprint('err {x}')                       ; 具名格式，stderr + 換行
s = format('x={x}')                      ; 返回格式化字串
```

---
