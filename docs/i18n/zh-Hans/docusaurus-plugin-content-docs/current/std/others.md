---
sidebar_position: 4.4
---

## 其他

### unicode — Unicode 支援

Unicode 相关功能已分散至 `char` 和 `str` 模组：

- 字元分类（`is-letter`, `is-digit`, `is-upper` 等）→ 见 `char` 模组
- UTF-8 编解码（`char.to-bytes`, `char.to-str`）→ 见 `char` 模组
- 字串 rune 计数（`str.count`）→ 见 `str` 模组

### uuid — UUID v4 产生与解析

```no
out = uuid.new-v4(state); 产生 UUID v4
out-n = uuid.to-str(out); 转小写字串（方法）
out-n = uuid.to-str-upper(out); 转大写字串（方法）
ok = uuid.from-str(s, sn, out); 从字串解析（支援连字号/不带）
ok = uuid.parse-with-dashes(s, pos, out); 含连字号解析
ok = uuid.parse-no-dashes(s, pos, out); 无连字号解析
ok = uuid.validate(); 验证 UUID 格式（方法）
v = uuid.version(); 取得版本（方法）
v = uuid.variant(); 取得变体（方法）
yes = uuid.is-nil(); 是否为 nil（方法）
yes = uuid.eq(b); 相等比较（方法）
r = uuid.cmp(b); 比较（方法）
uuid.nil-uuid(out); 回传 nil UUID
```

### bigint — 任意精度整数

```no
; 型别
bigint {
    sign i64
    limbs []i64
    len i64
}

; 建构
out = bigint.from-i64(v)
out = bigint.from-u64(v)
out = bigint.zero()
out = bigint.one()
out = bigint.copy(a)

; 比较
r = bigint.cmp(a, b)
r = bigint.eq(a, b)
r = bigint.is-zero(a)
r = bigint.is-neg(a)
r = bigint.is-pos(a)

; 运算（+ - * / % 已对 bigint 运算符重载，见「语法 > 运算符重载」）
c = a + b; 等价 a._add(b)
c = a - b; 等价 a._sub(b)
c = a * b; 等价 a._mul(b)
q = a / b; 等价 a._div(b)，除零依 div-mod 回传零
r = a % b; 等价 a._mod(b)

; 混合运算数：运算符重载参数为 `bigint | i64` 联合，编译期按右运算元型别单态化选臂，
; `a + 1` / `a * 256` 等原生可用（零运行期 tag）。仅限「左运算元是 bigint」，`1 + a` 不支援。
c = a + 1; bigint ⊕ i64
c = a * 256; bigint ⊕ i64
q, r = a.div-mod(b); 同时取商与余数
c = a.pow(n)
r = base.mod-pow(exp, mod)

; 数论
g = a.gcd(b)
l = a.lcm(b)

; 移位
bigint.shl(a, n, c)
bigint.shr(a, n, c)

; 字串转换
n = bigint.to-str(a, out)
out = bigint.from-str(s, sn)
n = bigint.to-hex(a, out)
out = bigint.from-hex(s, sn)
; 小整数快速路径（_add-i64 / _mul-i64 / _div-i64 / _mod-i64）为 bigint 模组私有，
; 混合运算数请一律使用上方的 + - * / % 运算符重载
```

### err — 错误处理

结构化错误型别与工具函式：

```no
; 错误码枚举
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

; 结构体
err {
    code code
    msg str
    file str
    line i64
    col i64
}

; 函数
e = err.new(msg, code.io); 建立错误
e = err.from-errno(errno); 从 C errno 建立
yes = e.is(code.io); 判断错误码
msg = e.msg(); 取得错误讯息
c = e.code(); 取得错误码
s = e.format(); 格式化为字串
```

这个 `err` 结构体就是内建 option 的 `err` 变体载荷（`option { ok(v t), nil, err(e err.err) }`）：
匹配 `?t` 时 `err ->` 臂里隐式绑定的 `it` 就是它，而不是 `str`。

```no
x = f(); f 返回 ?i64
x: {
    ok -> n = it

    nil -> n = 0

    err -> {
        log.error(it.msg()); 纯讯息
        print(it.format()); 'code.unknown: ... at file:line:col'
        it.is(code.io) -> print('io error')
    }
}
```

### bool — 布尔型别

```no
#{fmt-skip}
bool.to-str() (out str)     ; true→"true", false→"false"（方法）
```

### enter / leave — 生命周期钩子

```no
; 启动时执行
enter {
    enter()
}

; 退出时执行
leave {
    leave()
}
```

### magic — 档案类型检测

基于副档名与魔数（magic bytes）判断档案类型，不依赖 libmagic：

```no
kind = magic.detect-type(path); 检测档案类型
; 返回类型描述字串，如 'PNG image'、'ELF executable'、'ASCII text'、'directory'、'unknown'、'data'
```

---
