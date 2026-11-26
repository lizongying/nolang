---
sidebar_position: 5
---

# 导出

Nolang 使用 `@` 关键字在包的根目录 `lib.no` 文件中声明导出项，外部包通过 `#` 导入时只能访问这些已导出的符号。

> **注意**：`@` 只负责 Nolang 包之间的符号导出，不会产生 C ABI 符号。要让 C / C++ / Rust / Go / Java / Python / C# 呼叫 Nolang，请改用 `#{cabi='symbol'}` 注解搭配 `no build --shared`，见 [从宿主语言调用 Nolang](./host-interop)。

## 语法

```no
@ path[.func]
[alias]
```

- `path` — 模块路径（相对于包根目录或工作区根目录，不带 `.no` 副档名）
- `func` — 可选。要导出的函数/常量/枚举名；省略时表示导出整个模块
- `alias` — 可选别名，外部导入时使用的名称；可含多段（如 `test/assert`）

## 规则

- 导出语句**只能**写在包根目录的 `lib.no` 文件中（唯一例外见下节「全域可呼叫别名」）
- 一行一个导出项
- **符号导出** `@ path.func [alias]`：导出模块中的函数、常量、枚举等最终符号
- **模块导出** `@ path [alias]`：导出整个模块。别名可省略，默认取 `path` 最后一段；
  所以 `@ /test/src/assert` 等价于 `@ /test/src/assert assert`。
  别名也可以是多段模块名（`@ /test/src/assert test/assert`），外部以 `# test/assert` 导入
- 模块导出只注册模块名，不注册 callable alias；不能用来「把一个型别绑到某个模组」——
  型别没有全域名这一层
- 被导出函数引用的结构体、枚举等类型会**自动导出**，无需手动声明
- 如果导出的函数在模块中不存在，LSP 会提示错误

## 范例

```no
; lib.no - 包根目录导出文件
@ /src/utils.greet a
@ /src/utils.hello b
@ /src/math.pi
```

```no
; src/utils.no
; 定义被导出的函数
greet = (name str) {
    print('Hello, ' - name)
}

hello = () {
    print('Hi')
}
```

## 全域可呼叫别名

同一行语法写在 **`std/global.no`**（而不是某个包的 `lib.no`）里时，含义不同：
它不筛选导出项，而是宣告一个**全域可呼叫的第二名称**——裸名 `<alias>` 直接就是
`path` 模块的 `func`，效果与写全 `<path>.<func>(...)` 完全相同。

```no
; std/global.no —— 两种写法，语义相同
@ std/err.new err; 导出式
err = err.new; 赋值式
```

```no
e = err('disk full', code.io); 等同 e = err.new('disk full', code.io)
m = e.msg(); 'disk full'
```

规则：

- 只有 `std/global.no` 具有这个含义；`lib.no` 里的 `@` 仍然是导出过滤
- 两种写法**等价**，编译器把它们正规化成同一个宣告；同时写两个也不会冲突
- 赋值式**只在 `global.no` 有效**。普通程式里的 `x = mod.fn` 是一个持有函数值的
  变数（可用作间接呼叫），不会被当成全域别名
- 赋值式的右边必须是裸的 `<模组>.<函数>` 引用：不是呼叫、不是字面量、不是
  栏位读取、也不是带型别的绑定。目标不是函数时宣告自动失效（`PI = math.PI`
  这类常量宣告不受影响）
- 别名只在**裸名呼叫**位置生效。限定呼叫不受影响：`io.err(...)` 仍是 `io`
  模块的 stderr 写入器，不会被 `err` 别名劫持
- 模块内呼叫自己同名的函数也不受影响（自模块豁免）
- 别名的**参数表**取自目标函数；**回传型别**在呼叫点由目标函数决定
- 别名与内建 option 的 `err` 变体不冲突：在期望型别是 `?T` 的位置
  （`x ?i64` 之后的 `x = err('m')`、`err ->` 匹配臂、回传 `?T` 的函数），
  `err(...)` 仍然是选项的 err 变体构造器（tag 2）

## 导入已导出的符号

外部包通过 `#` 导入时，只能访问 `lib.no` 中声明的导出项：

```no
; 导入别名 a（对应 package-name.utils.greet）
# package-name.utils.greet a
; 或者直接使用函数名
# package-name.utils.greet
```

### 刻意未使用的导入：`_` 别名

有些模块必须由入口文件**直接导入**才会生成其顶层绑定（常量 / 全局 / struct 定义）；
只被间接依赖时跨模块读取会静默得到 0，甚至硬编译失败（bug #85）。
这类导入在本文件里没有符号被用到，`no vet` 会给出 `unimpuse` hint。
加上 `_` 别名即可明确标记「刻意保留、只为副作用」：导入照常生效，hint 消失，
且模块前缀仍是原本的 ShortName（`_` **不是**模块别名）。

```no
# /glossary/src/nav _    ; 保留导入以生成顶层绑定，不报 unimpuse hint
# /glossary/src/drv      ; 本文件确有使用，保持裸导入
code = drv.dispatch()
```

`_` 别名只对**模块导入**（`# path _`）有这个语义。函数导入 `# path.fn _`
是既有写法，代表丢弃该函数绑定，两者不要混淆。

## LSP 支持

- **跳转定义**：在 `lib.no` 中点击导出的函数名或别名，跳转到对应模块文件中的定义位置
- **自动补全**：输入 `@` 和路径时自动提示可选的文件路径和函数名
- **错误提示**：导出的函数在模块中不存在时显示错误诊断