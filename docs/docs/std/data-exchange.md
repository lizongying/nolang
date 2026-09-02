---
sidebar_position: 4.1
---

## 資料交換

### json — JSON 解析與產生

```no
; 型別枚舉
json-kind {
    null,
    bool,
    num,
    str,
    arr,
    obj,
}

; 解析
v = json.parse(s, n)          ; 完整解析
v = json.parse-str(s, n)                 ; 解析字串值
v = json.parse-num(s, n)                 ; 解析數值值

; 產生
n = json.stringify(v, out)    ; 序列化

; 存取
val = json.get-key(v, key)    ; 取得物件屬性
json.set-key(v json-value, key, val)    ; 設定物件屬性
```

---
