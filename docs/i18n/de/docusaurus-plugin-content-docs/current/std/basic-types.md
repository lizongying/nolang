---
sidebar_position: 3.1
---

## Grundtypen

### types — Typdefinitionen

Zuordnung der Nolang-Typen zu LLVM:

| Nolang           | LLVM                                               |
| ---------------- | -------------------------------------------------- |
| `bool`           | `i1`                                               |
| `byte`           | `i8`                                               |
| `char`           | `i32`                                              |
| `i8/i16/i32/i64/i128` | `i8/i16/i32/i64/i128`                             |
| `u8/u16/u32/u64/u128` | `i8/i16/i32/i64/i128`                          |
| `f32`            | `float`                                            |
| `f64`            | `double`                                           |
| `str`            | `%str-long = { i32, i32, i8* }` (u32 len, u32 cap, data; 16 Bytes auf 64-Bit, heap-allokiert) |
| `[]t`            | `%vec = { i32, i32, i64 }` (u32 len, u32 cap, ptrtoint data; 16 Bytes auf 64-Bit, heap-allokiert) |
| `[n]t`           | `[n x t]` (nacktes Aggregat, Elemente inline; **kein len-/cap-Descriptor**) |
| `txt`            | `{ [255 x i8], i8 }` (feste 256 Bytes) |

> **Sowohl `str` als auch `[]t` speichern len / cap intern als `u32`, expose sie aber durchgängig als `i64`.** An der Descriptor-Grenze wird beim Lesen `zext i32 -> i64` und beim Schreiben `trunc i64 -> i32` angewandt; die Signaturen von `s.len-bytes()`, `s.len()`, `s.cap()` sowie der privaten `_len` / `_cap` / `_set-len` /
> `_set-cap` bleiben alle `i64`, ebenso `v.len()`. Siehe [Zeichenketten: internes Layout und öffentliche API](../lang/str.md)
> und [Speichermodell: Typ-Layout](../lang/memory.md#型別佈局).
> Das `data`-Feld von `%vec` bleibt bewusst `i64` (über `ptrtoint` kodiert), anders als das `i8*` in `%str-long`; daher
> **teilen beide kein Layout mehr** und dürfen nicht per `getelementptr` wie derselbe Descriptor behandelt werden.
> `txt` besitzt kein `cap`-Feld; die Länge ist ein einzelnes `i8` (0–255) und **wendet nicht u32 an**.

**Zusammengesetzte Typen:**

- **Variabel langes Array `[]t`**: zugrunde liegendes `%vec = { u32 len, u32 cap, i64 data }` (16 Bytes; `cap == 0` kennzeichnet eine geliehene Ansicht ohne eigenen Puffer)
- **Festlanges Array `[n]t`**: LLVM-Array fester Größe `[n x t]`, Elemente inline, kein Laufzeit-Descriptor; `.len` ist die Kompilierzeitkonstante `n`, es gibt kein `cap`
- **Zeichenkette `str`**: heap-allokierte Bytefolge mit Descriptor `{ u32 len, u32 cap, i8* data }` (16 Bytes auf 64-Bit); unterstützt `s[i]`, `s[i..j]`, `s + t`. Länge/Kapazität werden nach außen als `i64` dargestellt (`s.len-bytes()` / `s.cap()`), intern als `u32` gespeichert
- **Feststring `txt`**: feste 256-Byte-Struktur `{ [255]byte data, byte len }`, die ersten 255 Bytes speichern die Daten, das letzte Byte die **Byte**-Länge (0–255), keine Heap-Allokation nötig, Typannotation erforderlich (`t txt = 'abc'`); `t.len()` gibt die Anzahl der Codepunkte zurück (an `str` angeglichen, entspricht `t.count()`), `t.len-bytes()` die Byte-Anzahl; die **Lese**-Indizierung `t[i]` erfolgt nach **Codepunkten** (gibt das `char` dieses Codepunkts zurück, bei Out-of-Bounds `err`, `code.index-out`); **Schreiben `t[i] = c` ist seit 2026-11-12 global verboten** (Kompilierfehler `idxwrite`) — es kodiert `c` neu als UTF-8 und verschiebt die nachfolgenden Bytes, die Länge ändert sich implizit mit dem Schreiben (Anhängen bei `i` gleich der Codepunktzahl, alles über 255 Bytes hinaus wird still verworfen); für Wachstum verwende `t.push(c char)` / `t.push-str(s)` / `t.push-txt(u)`, für In-Place-Änderungen place-match `t[i]: { ok -> it = c }`; Byte-Lesen erfolgt mit `t.byte(i)` (gibt `?byte` zurück, Out-of-Bounds nil); um Byte für Byte zusammenzusetzen, sammle direkt ein `[]byte` und kopiere es mit `txt.from-bytes(b)` in einem Schritt hinein (über 255 Bytes hinaus abgeschnitten, Länge aus der tatsächlichen Bytezahl). Der Byte-Schreiber `txt._set-byte` ist seit 2026-11-11 **std-privat** und für Benutzercode nicht aufrufbar. Das `s[i] = v` von `str` wurde am 2026-11-11 **global verboten** (Kompilierfehler `idxwrite`, auch `with-cap`-Basen sind nicht mehr ausgenommen); auf der `str`-Seite bleiben nur `s.fill(val byte)`, place-match `s[i]: { ok -> it = v }` sowie die Wachstumspaare `s.push(c)` / `s.push-str(s)`
- **Enumeration/Union**: `option`-Tagged-Enum (`ok t` / `nil` / `err str`)
- **Struktur**: muss über mehrere Zeilen definiert werden, Felder ohne Kommas
- **Map (Zuordnungstabelle)**: zugrunde liegend linked-hash-map
- **Iterator**: `for iter.next() {}` (Interface-Methode `next() (ok bool)`)

### option — Option-Typ

`option<t>` Tagged-Enum (tag=0=val, 1=nil, 2=err):

```no
#{fmt-skip}
x ?t                ; 宣告 option<t>
x = 42              ; 設為有值
x = nil             ; 設為空
x = err('msg')      ; 設為錯誤

; match
x: {
    val -> f(it)
    nil ->
    err -> g(it.msg())
}  
```

**Stilrichtlinie:** Wenn eine Funktion fehlschlagen oder einen leeren Wert zurückgeben kann, verwende `?t` statt `(val, ok bool)`. `?t` hat drei Zustände: `ok` (Wert vorhanden), `nil` (leer/normales Fehlen), `err` (Fehler). Der normale Wert wird implizit gebunden. Zum Beispiel gibt `pop()` `?i64` zurück (`nil` = leer), `read-line()` `?str` (`nil` = EOF, `err` = Fehler), `lookup()` `?str` (`nil` = nicht gefunden). Details siehe Syntaxdokumentation.

---
