---
sidebar_position: 5
---

# 導出

Nolang 使用 `@` 關鍵字在包的根目錄 `lib.no` 文件中聲明導出項，外部包通過 `#` 導入時只能訪問這些已導出的符號。

## 語法

```nolang
@ path.func [alias]
```

- `path` — 模塊路徑（相對於包根目錄，以 `/` 開頭，不帶 `.no` 副檔名）
- `func` — 要導出的函數/常量/枚舉名
- `alias` — 可選別名，外部導入時使用的名稱

## 規則

- 導出語句**只能**寫在包根目錄的 `lib.no` 文件中
- 一行一個導出項
- 導出項只能是函數、常量、枚舉等最終符號
- 被導出函數引用的結構體、枚舉等類型會**自動導出**，無需手動聲明
- 如果導出的函數在模塊中不存在，LSP 會提示錯誤

## 範例

```nolang
// lib.no - 包根目錄導出文件
@ /src/utils.greet a
@ /src/utils.hello b
@ /src/math.pi
```

```nolang
// src/utils.no
// 定義被導出的函數
greet = (name str) {
    print('Hello, ' - name)
}

hello = () {
    print('Hi')
}
```

## 導入已導出的符號

外部包通過 `#` 導入時，只能訪問 `lib.no` 中聲明的導出項：

```nolang
// 导入別名 a（對應 utils.greet）
# package-name.a

// 或者直接使用函數名
# package-name.greet
```

## LSP 支持

- **跳轉定義**：在 `lib.no` 中點擊導出的函數名或別名，跳轉到對應模塊文件中的定義位置
- **自動補全**：輸入 `@` 和路徑時自動提示可選的文件路徑和函數名
- **錯誤提示**：導出的函數在模塊中不存在時顯示錯誤診斷