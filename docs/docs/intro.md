---
sidebar_position: 1
---

# Nolang 簡介

Nolang 是一門實驗性的系統編程語言：內存安全無 GC、語義符合直覺、語法極簡。

## 核心特性

- **內存安全**：不依賴垃圾回收，自动內存管理
- **性能強勁**：編譯期優化，無 GC
- **語法極簡**：更少關鍵字，沒有指針、所有權、生命週期等隱式心智負擔，語法更簡單
- **意圖驅動**: 充分尊重開發者意圖，語義符合人類直覺
- **永不崩溃**：消除隱式崩潰路徑

## 快速開始

```no
; 你好，世界！
; 不需要main入口
print('Hello, Nolang!')

; 變量聲明
count i64

; 函數定義
add = (a i64, b i64) (result i64) {
    result = a + b
}

; 標準庫方法，可以直接調用
c = math.max(a, b)

; 結構體
user {
    name str
    age i64
}

; 方法
user.greet = () {
    print('Hello, {.name}')
}

u = user {
    name: 'Alice'
    age: 30
}

u.greet()
```

## 應用案例

- [notools](https://github.com/lizongying/notools) — 使用 Nolang 語言實現的 Unix 常用命令行工具集，涵蓋 cat、ls、grep、wc、head、tail 等經典工具，展示了 Nolang 在系統編程領域的實際應用能力。
