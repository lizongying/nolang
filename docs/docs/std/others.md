---
sidebar_position: 4.2
---

## 其他

### unicode — Unicode 支援

Unicode 相關功能已分散至 `char` 和 `str` 模組：

- 字元分類（`is-letter`, `is-digit`, `is-upper` 等）→ 見 `char` 模組
- UTF-8 編解碼（`char.to-bytes`, `char.to-str`）→ 見 `char` 模組
- 字串 rune 計數（`str.count`）→ 見 `str` 模組

### uuid — UUID v4 產生與解析

```no
out = uuid.new-v4(state)                  ; 產生 UUID v4
out-n = uuid.to-str(out)             ; 轉小寫字串（方法）
out-n = uuid.to-str-upper(out)       ; 轉大寫字串（方法）
ok = uuid.from-str(s, sn, out)            ; 從字串解析（支援連字號/不帶）
ok = uuid.parse-with-dashes(s, pos, out)  ; 含連字號解析
ok = uuid.parse-no-dashes(s, pos, out)    ; 無連字號解析
ok = uuid.validate()                 ; 驗證 UUID 格式（方法）
v = uuid.version()                   ; 取得版本（方法）
v = uuid.variant()                   ; 取得變體（方法）
yes = uuid.is-nil()                  ; 是否為 nil（方法）
yes = uuid.eq(b)                     ; 相等比較（方法）
r = uuid.cmp(b)                      ; 比較（方法）
uuid.nil-uuid(out)                        ; 回傳 nil UUID
```

### bigint — 任意精度整數

```no
; 型別
bigint {
    sign i64
    limbs []i64
    len i64
}

; 建構
out = bigint.from-i64(v)
out = bigint.from-u64(v)
out = bigint.zero()
out = bigint.one()
out = bigint.copy(a)

; 比較
r = bigint.cmp(a, b)
r = bigint.eq(a, b)
r = bigint.is-zero(a)
r = bigint.is-neg(a)
r = bigint.is-pos(a)

; 運算
c = bigint.add(a, b)
c = bigint.sub(a, b)
c = bigint.mul(a, b)
q, r = bigint.div-mod(a, b)
r = bigint.mod(a, b)
q = bigint.div-i64(a, v)
r = bigint.mod-i64(a, v)
c = bigint.pow(a, n)
r = bigint.mod-pow(base, exp, mod, r)

; 數論
bigint.gcd(a, b, g)
bigint.lcm(a, b, l)

; 移位
bigint.shl(a, n, c)
bigint.shr(a, n, c)

; 字串轉換
n = bigint.to-str(a, out)
out = bigint.from-str(s, sn)
n = bigint.to-hex(a, out)
out = bigint.from-hex(s, sn)

; 小整數輔助
bigint.add-i64(a, v, c)
bigint.mul-i64(a, v, c)
```

### err — 錯誤處理

結構化錯誤型別與工具函式：

```no
; 錯誤碼枚舉
code {
    ok,
    not-found,
    permission,
    io,
    timeout,
    parse,
    invalid,
    overflow,
}

; 結構體
error {
    code code
    msg str
}

; 函數
e = err.new(code.io, msg)            ; 建立錯誤
e = err.err-from-errno(errno)         ; 從 C errno 建立
yes = e.is(code.io)                  ; 判斷錯誤碼
msg = e.msg()                       ; 取得錯誤訊息
c = e.code()                        ; 取得錯誤碼
s = e.format()                       ; 格式化為字串
```

### bool — 布爾型別

```no
bool.to-str() (out str)     ; true→"true", false→"false"（方法）
```

### enter / leave — 生命週期鉤子

```no

; 啟動時執行
enter { 
    enter()
}     

; 退出時執行
leave {
    leave()
}     
```

### async — 異步協程與取消原語

Nolang 提供協作式、單執行緒、無棧的異步協程模型：

```no
; 啟動 -async 函數為後台任務，返回不透明 task 句柄
h = run worker-async(args)

; 等待後台任務完成，返回結果
r = awy h

; 取消後台任務（協作式）
async.async-cancel(h)                    ; 設置任務 h 的取消標誌

; 協作式自我取消檢查（在異步函數內調用）
yes = async.async-cancelled()            ; 返回當前任務是否已被取消
```

> **注意：** 取消是協作式而非搶占式。長阻塞調用（如網路請求）無法被強制中斷。任務會在下一個協作檢查點（`async-cancelled()` 調用或事件迴圈下次調度）真正停止。

### global — 全域內建函數

無需模組前綴即可調用的函數。僅有以下 6 個全域函數，其餘跨模組調用都必須加模組前綴。

```no
; 容量/長度構造（型別由賦值左側推斷）
s str = with-cap(256)                   ; 預分配 256 位元組 str（len=0）
v []i64 = with-cap(100)                 ; 預分配 100 元素切片（len=0）
s str = with-len(10)                    ; 長度為 10 的 str
v []i64 = with-len(100)                 ; 長度為 100 的切片
v []i64 = with-cap-len(200, 100)        ; 容量 200、長度 100 的切片

; 也可作為 str/vec 方法調用：
s = ''.with-cap(256)
v = [].with-cap(100)
v = [].with-len-cap(100, 200)           ; 長度 100，容量 200

; 輸出/格式化
print('x={x}')                          ; 具名格式，stdout + 換行
eprint('err {x}')                       ; 具名格式，stderr + 換行
s = format('x={x}')                      ; 返回格式化字串
```

### magic — 檔案類型檢測

基於副檔名與魔數（magic bytes）判斷檔案類型，不依賴 libmagic：

```no
kind = magic.detect-type(path)                  ; 檢測檔案類型
; 返回類型描述字串，如 'PNG image'、'ELF executable'、'ASCII text'、'directory'、'unknown'、'data'
```

---
