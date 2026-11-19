---
sidebar_position: 4.4
---

## その他

### unicode — Unicode サポート

Unicode 関連機能は `char` モジュールと `str` モジュールに分散されています：

- 文字分類（`is-letter`, `is-digit`, `is-upper` など）→ `char` モジュールを参照
- UTF-8 エンコード／デコード（`char.to-bytes`, `char.to-str`）→ `char` モジュールを参照
- 文字列の rune 数（`str.count`）→ `str` モジュールを参照

### uuid — UUID v4 の生成と解析

```no
out = uuid.new-v4(state); 產生 UUID v4
out-n = uuid.to-str(out); 轉小寫字串（方法）
out-n = uuid.to-str-upper(out); 轉大寫字串（方法）
ok = uuid.from-str(s, sn, out); 從字串解析（支援連字號/不帶）
ok = uuid.parse-with-dashes(s, pos, out); 含連字號解析
ok = uuid.parse-no-dashes(s, pos, out); 無連字號解析
ok = uuid.validate(); 驗證 UUID 格式（方法）
v = uuid.version(); 取得版本（方法）
v = uuid.variant(); 取得變體（方法）
yes = uuid.is-nil(); 是否為 nil（方法）
yes = uuid.eq(b); 相等比較（方法）
r = uuid.cmp(b); 比較（方法）
uuid.nil-uuid(out); 回傳 nil UUID
```

### bigint — 任意精度整数

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

; 運算（+ - * / % 已對 bigint 運算符重載，見「語法 > 運算符重載」）
c = a + b; 等價 a._add(b)
c = a - b; 等價 a._sub(b)
c = a * b; 等價 a._mul(b)
q = a / b; 等價 a._div(b)，除零依 div-mod 回傳零
r = a % b; 等價 a._mod(b)

; 混合運算數：運算符重載參數為 `bigint | i64` 聯合，編譯期按右運算元型別單態化選臂，
; `a + 1` / `a * 256` 等原生可用（零運行期 tag）。僅限「左運算元是 bigint」，`1 + a` 不支援。
c = a + 1; bigint ⊕ i64
c = a * 256; bigint ⊕ i64
q, r = a.div-mod(b); 同時取商與餘數
c = a.pow(n)
r = base.mod-pow(exp, mod)

; 數論
g = a.gcd(b)
l = a.lcm(b)

; 移位
bigint.shl(a, n, c)
bigint.shr(a, n, c)

; 字串轉換
n = bigint.to-str(a, out)
out = bigint.from-str(s, sn)
n = bigint.to-hex(a, out)
out = bigint.from-hex(s, sn)
; 小整數快速路徑（_add-i64 / _mul-i64 / _div-i64 / _mod-i64）為 bigint 模組私有，
; 混合運算數請一律使用上方的 + - * / % 運算符重載
```

### err — エラー処理

構造化されたエラー型とユーティリティ関数：

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
    unknown,
}

; 結構體
err {
    code code
    msg str
    file str
    line i64
    col i64
}

; 函數
e = err.new(msg, code.io); 建立錯誤
e = err.from-errno(errno); 從 C errno 建立
yes = e.is(code.io); 判斷錯誤碼
msg = e.msg(); 取得錯誤訊息
c = e.code(); 取得錯誤碼
s = e.format(); 格式化為字串
```

この `err` 構造体は、組み込み option の `err` バリアントのペイロードそのものです（`option { ok(v t), nil, err(e err.err) }`）。`?t` をマッチさせたとき、`err ->` アーム内で暗黙にバインドされる `it` は `str` ではなく、この構造体です。

```no
x = f(); f 返回 ?i64
x: {
    ok -> n = it

    nil -> n = 0

    err -> {
        log.error(it.msg()); 純訊息
        print(it.format()); 'code.unknown: ... at file:line:col'
        it.is(code.io) -> print('io error')
    }
}
```

### bool — 真偽値型

```no
#{fmt-skip}
bool.to-str() (out str)     ; true→"true", false→"false"（方法）
```

### enter / leave — ライフタイムフック

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

### magic — ファイル種別の検出

拡張子とマジックナンバー（magic bytes）に基づいてファイル種別を判定し、libmagic には依存しません：

```no
kind = magic.detect-type(path); 檢測檔案類型
; 返回類型描述字串，如 'PNG image'、'ELF executable'、'ASCII text'、'directory'、'unknown'、'data'
```

---
