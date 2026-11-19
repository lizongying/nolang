---
sidebar_position: 1
---

# Nolang の概要

Nolang は実験的なシステムプログラミング言語です。ガベージコレクションなしでメモリ安全を実現し、セマンティクスは直感的で、構文は極限まで簡潔です。

## 主な特徴

- **メモリ安全**：ガベージコレクションに依存せず、自動的にメモリを管理
- **高性能**：コンパイル時の最適化、GC なし
- **極簡な構文**：キーワードが少なく、ポインタ、所有権、ライフタイムといった暗黙の認知的負担がなく、構文がより単純
- **意図駆動**: 開発者の意図を十分に尊重し、セマンティクスは人間の直感に合致
- **決してクラッシュしない**：暗黙のクラッシュ経路を排除

## クイックスタート

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

## 応用例

- [notools](https://github.com/lizongying/notools) — Nolang 言語で実装された Unix の一般的なコマンドラインツール集で、cat、ls、grep、wc、head、tail などの古典的なツールを網羅しています。システムプログラミング分野における Nolang の実用的な応用能力を示しています。
