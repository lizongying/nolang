---
sidebar_position: 4.1
---

## 资料交换

### json — JSON 解析与产生

`json` 以**堆配置的节点池**（`json-pool`）保存 JSON 树，支援巢状阵列与物件、字串/数值/布林/null 标量、转义字元与指数格式数字。节点池改用动态 `vec`，**没有固定容量上限**——节点总数与单一节点的子元素数仅受可用记忆体限制。高级介面以 `json` 结构体封装节点池与根节点索引；其 `pool` 栏位标注 `#{inline=false}`（指标栏位），使 `json` 可按值安全回传与复制。

```no
; 解析：回传 ?json（ok=成功，err=失败并附原因）
j = json.parse('{"name":"Alice","age":30,"items":[1,2,3],"active":true}')
j: {
    ok -> {
        name = it.get-str('name'); 取标量：get-str / get-num / get-bool / get-i64（?值）
        age = it.get-i64('age'); 整数取值（?i64）
        items = it.get('items'); 取子节点，回传 ?json
        s = it.stringify(); 序列化回字串
    }

    err(e) -> print(e)
}

; 产生：从空 json 开始，逐步 set / arr-push
j2 = json.new()
j2.set-str('key', 'value')
j2.set-i64('count', 3)
j2.arr-push-str('a')
print(j2.stringify())
```

**高级 API（`json` 结构体）**

- `json.new()` — 建立空（null）`json`
- `json.parse(s)` — 解析，回传 `?json`；失败时 `err` 分支带具体原因（`empty input` / `parse error` / `trailing characters`）
- `j.stringify()` — 序列化为字串
- 取值：`j.get-str(key)` / `j.get-num(key)` / `j.get-bool(key)` / `j.get-i64(key)`（各回传 `?val`）；`j.get(key)` 回传子节点 `?json`
- 标量直取（root 为标量时）：`j.str()` / `j.num()` / `j.bool()` / `j.i64()` / `j.str-val()`
- 写入：`j.set-str(key,val)` / `j.set-num` / `j.set-i64` / `j.set-bool` / `j.set-null` / `j.set-key(key, val json)`
- 阵列：`j.arr-get(i)` / `j.arr-len()` / `j.arr-push(val json)` / `j.arr-push-str` / `j.arr-push-num` / `j.arr-push-bool`
- 物件列举：`j.obj-len()` / `j.obj-key(i)` / `j.obj-keys-str()` / `j.delete-key(key)`
- 型别判定：`j.kind()` 及 `j.is-null()` / `j.is-obj()` / `j.is-arr()` / `j.is-str()` / `j.is-num()` / `j.is-bool()`

**池容量（无固定上限）**

早期版本以固定阵列作池，硬限制 `JSON-MAX-NODES = 128` / `JSON-MAX-CHILDREN = 32`，超出即静默失败。现已全面改为堆 `vec`：

- 节点总数、每节点子元素数**皆无上限**，仅受可用记忆体限制。
- `json.parse` 只在真正的语法错误时回传 `err`，不再有「池耗尽」错误分支。
- `j.set*` / `j.arr-push*` 仅在建构目标型别不符时回传 `false`（例如对非物件节点 set、对非阵列节点 push）。`j.overflowed()` 为向后相容而保留，现恒回传 `false`。

> **`json` 的复制语意（`#{inline=false}`）**：`pool` 是指标栏位，`j.get` / `j.arr-get` 回传的子节点 `json` 会**深拷贝**整个池，是父池的独立快照——对快照的写入不会回写父节点。另因 `?json`（option）在 `match` 时会消费其值，对同一个 option **重复 `match`** 会进入 `nil` 分支；若需对同一解析结果多次操作，请在单一 `match` 内完成，或先以一个 `json` 区域变数承接 `it`。

**低阶 API（`json-pool` / `json-value`）**

直接以节点索引操作，池作为区域变数使用（单层结构体按值回传/复制安全）：

- `p = json.new-pool()`；或 `p = json-pool {}` 后 `p.init()`（把 `nodes`/`strs`/`ec`/`ek`/`en` 各 vec 初始化为 `with-len(0)`）
- `idx = p.alloc()` — 分配新节点并回传索引（堆 vec 自动扩容）
- `p.add-child(node-idx, child-idx, key)` — 以边表（`ec`/`ek`/`en` 单向链表）附加子元素，无每节点上限
- `node-idx, next-pos, ok = p.parse(s, pos)` — 从 `pos` 解析一个值，回传节点索引
- `val-idx = p.get-key(obj-idx, key)`（`?i64`）；`p.arr-get(arr-idx, i)`（`?i64`）；`p.arr-len(arr-idx)`
- `p.get-kind(idx)` / `p.get-str(idx)` / `p.get-num(idx)` / `p.get-bool(idx)`
- `p.set-key(obj-idx, key, val-idx)`；`p.copy-tree(src, idx)`（递回深拷贝子树）
- `p.stringify(node-idx)` — 序列化指定节点

### toml — TOML 1.0 解析与产生

`toml` 以 `toml` 保存 TOML 文件，支援注解、bare/quoted/dotted keys、普通表格、array-of-tables、basic/literal/multiline strings、各种整数进位、浮点数、布林值、日期时间、阵列与 inline table。未提供专用 Nolang 型别的值会保留原始字面值。

```no
cfg = toml.parse('title = "Nolang"\n[server]\nport = 8080\n[[server.backends]]\nname = "local"')

doc = toml.new()
doc.put('name', '"nolang"')
doc.put('server.port', '8080')
doc.put('enabled', 'true')
raw = toml.get(doc, 'server.port')
text = toml.stringify(doc)
```

常用函式：

- `toml.new()` — 建立空文件
- `toml.parse(text)` — 解析并回传 `?toml`
- `doc.put(key, raw-value)` — 验证并新增/更新 TOML 值
- `get(doc, key)` — 取得原始值
- `get-str`、`get-i64`、`get-f64`、`get-bool` — 取得 scalar 值
- `toml.stringify(doc)` — 序列化文件

文件上限为 4 个 key、2 个普通表格、2 个 array-of-tables；key 最长 32 bytes，value 最长 128 bytes。

### yaml — YAML 1.2 解析与产生

`yaml` 以节点池（`pool`）保存 YAML 1.2 core schema 的文件树，支援块映射、块序列、流式集合（`[...]` / `{...}`）、单引号/双引号/plain 标量、块标量（`|` / `>` 与 `-` / `+` chomping）、注解、文件标记（`---` / `...`）与多文档、锚点（`&`）与别名（`*`）、合并键 `<<`、显式键（`? k`）、型别推断（null / bool / int / float / `.inf` / `.nan`）、点号路径查询，以及 JSON / YAML 序列化。

```no
doc ?yaml = yaml.parse('name: Alice\nserver:\n  port: 8080\ntags:\n  - a\n  - b\n')
doc: {
    nil -> print('nil')

    err -> print(it.msg())

    -> {
        doc = it
        name = doc.find-str('name'); ?str
        port = doc.find-i64('server.port'); ?i64
        name: {
            ok -> print(it)

            -> print('')
        }
        port: {
            ok -> print(it.to-str())

            -> print('')
        }
        print(doc.to-json())
        print(doc.to-yaml())
    }
}
```

常用函式与方法：

- `yaml.parse(text)` / `yaml.parse-all(text)` — 解析，回传 `?yaml`（`err` 分支带 `line L, column C` 位置）
- `yaml.valid-of(text)` / `yaml.error-of(text)` — 只做校验，不构造文件
- `yaml.strip-bom(text)` — 去掉 UTF-8 BOM
- `doc-count()` / `doc(i)` — 多文档访问
- `kind()`、`is-null()`、`is-bool()`、`is-int()`、`is-float()`、`is-nan()`、`is-str()`、`is-seq()`、`is-map()`、`len()`、`text()`、`tag()`、`anchor()`、`line-of()` — 节点资讯
- `str()`、`i64()`、`f64()`、`bool()`、`scalar-text()` — 取值，一律回传 `?值`
- `at(i)`、`value(i)`、`key(i)`、`key-node(i)`、`get(key)`、`find(path)` — 取子节点 / 键
- `get-str` / `get-i64` / `get-f64` / `get-bool`、`find-str` / `find-i64` / `find-f64` / `find-bool` — 便捷取值
- `to-json()`、`to-json-all()`、`to-yaml()`、`stringify()`、`source-of()` — 序列化

节点类型常量：`YAML-KIND-NULL` / `BOOL` / `INT` / `FLOAT` / `STR` / `SEQ` / `MAP`（0–6）；标量风格常量 `YAML-STYLE-PLAIN` / `SINGLE` / `DOUBLE` / `LITERAL` / `FOLDED` / `ALIAS`（0–5）与 `YAML-STYLE-BLOCK` / `FLOW`（6–7）。

> **注意一**：子节点视图以 `?yaml` 回传，取值协助函式以 `?值` 回传，用 match 解包（`child = d.at(i)`，其 `ok` 臂以 `it` 绑定子节点、预设臂处理空值，每臂独立一行）。子节点与父文档共享节点池，建议在同一个 match 内完成取值，或先系结一份，别跨多次池变动持有过期 handle。取值一律建议用 `find-str` / `find-i64` 这类路径便捷函式。
>
> **注意二**：巢状 match 里 `it` 是**按层还原**的。内层 match 的臂会把 `it` 重新绑到自己的主体（臂体必须看到内层值），但该臂一结束 `it` 就还原成外层臂的主体，所以下面这种写法是安全的：
>
> ```no
> d ?yaml = yaml.parse(src)
> d: {
>     nil -> print('nil')
>
>     err -> print(it.msg())
>
>     -> {
>         print(it.find-str('a'))        ; 外层 it = 文件
>         v ?yaml = it.at(0)
>         v: {
>             nil -> print('none')
>             err -> print('err')
>             -> print(it.find-str('b')) ; 内层 it = 子节点
>         }
>         print(it.find-str('c'))        ; ✅ it 已还原成文件
>     }
> }
> ```
>
> 唯一的例外是最外层的 match：它结束后 `it` 仍保留最后一个臂的值（不会还原），所以不要在 match 之外依赖 `it`。需要跨层使用时，仍可显式存到具名区域变数（`doc = it`），或用析构绑定 `ok(v) -> ...`。
>
> **注意三**：`.nan` 会被解析成真正的 NaN（`f64()` 回传 NaN，`x != x` 为真）。JSON 没有 NaN，所以 `to-json()` 对它输出 `null`；`to-yaml()` 输出 `.nan`。

---
