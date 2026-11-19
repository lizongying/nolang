---
sidebar_position: 3
---

# 标准库

Nolang 标准库（`src/std/`）包含 80+ 个模组，涵盖格式化、数学、字串、资料结构、编解码、加密、压缩、档案操作、I/O 抽象、异步协程、档案类型检测等。

使用方式：`# std/xxx`（核心模组无需导入）。

> **旧式 `use std/xxx` 仍可使用，但已废弃（deprecated），建议改用新式 `# std/xxx` 语法。**

> **注意：本文档中的程式码范例均遵守「每行一条语句」规则——禁止使用分号 `;` 或逗号 `,` 将多条语句写在同一行。** 例如 `out = from-i64(v), out = from-u64(v)` 是错误写法，应拆分为多行。

---

## 模组分类

- [基础型别](basic-types)
- [核心函式库](core-library)
- [作业系统与档案](os-and-files)
- [时间与日期](time-and-date)
- [日志](logging)
- [资料结构](data-structures)
- [资料库](database)
- [编码](encoding)
- [归档](archives)
- [密码学与杂凑](crypto)
- [资料交换](data-exchange)
- [其他](others)
- [模组一览](module-overview)
