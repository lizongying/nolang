---
sidebar_position: 1
---

# Einführung in Nolang

Nolang ist eine experimentelle Systemprogrammiersprache mit Speichersicherheit ohne GC, intuitiver Semantik und minimaler Syntax.

## Kernmerkmale

- **Speichersicher**: Keine Garbage Collection, automatische Speicherverwaltung
- **Leistungsstark**: Optimierung zur Kompilierzeit, kein GC
- **Minimale Syntax**: Weniger Schlüsselwörter, keine implizite Denklast durch Zeiger, Ownership oder Lifetimes, einfachere Syntax
- **Intentionsgesteuert**: Respektiert die Absicht der Entwickler voll und ganz, die Semantik entspricht der menschlichen Intuition
- **Absturzsicher**: Beseitigt implizite Absturzpfade

## Schnellstart

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

## Anwendungsfälle

- [notools](https://github.com/lizongying/notools) — Eine Sammlung gängiger Unix-Kommandozeilenwerkzeuge, die in Nolang implementiert sind, darunter klassische Tools wie cat, ls, grep, wc, head und tail. Sie zeigt die praktische Eignung von Nolang im Bereich der Systemprogrammierung.
