---
sidebar_position: 3
---

# 標準庫

Nolang 標準庫（`src/std/`）包含 80+ 個模組，涵蓋格式化、數學、字串、資料結構、編解碼、加密、壓縮、檔案操作、I/O 抽象、異步協程、檔案類型檢測等。

使用方式：`# std/xxx`（核心模組無需導入）。

> **舊式 `use std/xxx` 仍可使用，但已廢棄（deprecated），建議改用新式 `# std/xxx` 語法。**

> **注意：本文檔中的程式碼範例均遵守「每行一條語句」規則——禁止使用分號 `;` 或逗號 `,` 將多條語句寫在同一行。** 例如 `out = from-i64(v), out = from-u64(v)` 是錯誤寫法，應拆分為多行。

---

## 模組分類

- [基礎型別](basic-types)
- [核心函式庫](core-library)
- [作業系統與檔案](os-and-files)
- [時間與日期](time-and-date)
- [日誌](logging)
- [資料結構](data-structures)
- [資料庫](database)
- [編碼](encoding)
- [歸檔](archives)
- [密碼學與雜湊](crypto)
- [資料交換](data-exchange)
- [其他](others)
- [模組一覽](module-overview)
