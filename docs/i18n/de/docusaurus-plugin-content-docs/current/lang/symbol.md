---
sidebar_position: 4
---

# Operatoren

## Besondere Symbole

- `#` ; Modul importieren
- `@` ; Modul exportieren
- `->` ; Match-Zweig und if/else-Verzweigung (z. B. `cond -> body`)
- `:` ; Match-Ausdruck (z. B. `x: { ... }`)
- `{ } (true)` ; Endlosschleife (Suffixform, alte Schreibweise); die Präfixform `!! { }` / `true { }` ist gleichbedeutend und die neue Standardform (Ausgabe von `no fmt`)
- `<-` ; Bereichsdurchlauf (z. B. `i <- [a..b]: { }`)
- `..` ; Elternklasse / Bereichsoperator (z. B. `[a..b)`)
- `.` ; Selbst (⚠️ in Bereichsgrenzen sollte `self.method` statt `.method` verwendet werden, um die visuelle Mehrdeutigkeit von drei aufeinanderfolgenden Punkten zu vermeiden)
- `?` ; Präfix für Option-Typ (z. B. `?i64`, `?str`)
- `(cond) { }` ; Bedingte Schleife (Präfixform, neue Standardform, gleichbedeutend mit der Suffixform `{ } (cond)`)
- `N * { }` ; Zählschleife (Präfixform, neue Standardform, gleichbedeutend mit der Suffixform `{ } * N`; N ≤ 0 wird übersprungen)
- `!` ; Falsch (false); außerdem das Präfix „nie ausführen" für Schleifen (`! { }` läuft nie)
- `!!` ; Wahr (true); außerdem das Präfix „immer ausführen" für Schleifen (`!! { }` läuft endlos)
- `{ } ()` ; Nicht ausgeführt (leere Klammern bedeuten false; Suffixform, alte Schreibweise); die Präfixform `! { }` / `false { }` / `() { }` ist gleichbedeutend und die neue Standardform
- `*` ; Aktuelle Iteration überspringen, Ersatz für continue (vom Parser akzeptiert, aber derzeit nicht empfohlen — bitte das Schlüsselwort verwenden)
- `**` ; Abbrechen, Ersatz für break (vom Parser akzeptiert, aber derzeit nicht empfohlen — bitte das Schlüsselwort verwenden)
- `***` ; return-Anweisung, Funktion beenden — Ersatz für return (geplant, derzeit nicht empfohlen — bitte das Schlüsselwort verwenden)
- `awy { }` ; Coroutinen-Gruppe, wartet am Gruppenende auf alle `co`-Aufgaben
- `co` ; Farbloses async-Spawn, nur innerhalb einer `awy { }`-Gruppe erlaubt

## Arithmetische Operatoren

- `+` ; Addition
- `-` ; Subtraktion (auch für Zeichenkettenverknüpfung)
- `*` ; Multiplikation (auch für Zeichenkettenwiederholung)
- `/` ; Division

## Vergleichsoperatoren

- `==` ; Gleich (`str` besitzt einen echten Zeichenkettenvergleich, mehrere Zeichen erlaubt)
- `!=` ; Ungleich (wie oben)
- <code>&lt;</code> ; Kleiner als
- `>` ; Größer als
- <code>&lt;=</code> ; Kleiner oder gleich
- `>=` ; Größer oder gleich

:::warning Vergleichsoperatoren (<code>&lt;</code> `>` <code>&lt;=</code> `>=`) besitzen keine Zeichenketten-Semantik

`str` unterstützt nur den Gleichheitsvergleich (`==` / `!=`). Ein Vergleich von Zeichenketten lieferte früher **still false** zurück, jetzt ist er ein Kompilierfehler.

- Erlaubt: ein **einzelnes Zeichen** als Zeichenkettenliteral `'a'`, implizit als `char` (Codepunkt) behandelt, vergleichbar mit einem anderen Literal eines einzelnen Zeichens oder mit einem `char` (`"a"`). Zahlenwerte sind nicht betroffen.
- Fehlerhaft: mehrstellige Zeichenketten — das Literal `'ab'`, eine `str`-Variable oder ein Aufruf, der `str` zurückgibt.
- Für lexikografische Vergleiche `str.compare(b)` verwenden (gibt -1 / 0 / 1 zurück).

```nolang
'a' <= 'z'; true  — 單字元字串隱式 char
ch >= 'a'; ch 為 char，OK
'a' > "b"; 單字元字串 vs char，OK
'abc' < 'abd'; 編譯錯誤：多字元字串
s1 < s2; 編譯錯誤：str 變數
s1.compare(s2) > 0; 正確寫法
```
:::

## Logische Operatoren

- `&&` ; Logisches UND
- `||` ; Logisches ODER (auch für die Kombination von Match-Zweigen, z. B. `nil || err -> body`)
- `!` ; Logisches NICHT

## Bitweise Operatoren

- `&` ; Bitweises UND
- `|` ; Bitweises ODER
- `^` ; Bitweises XOR
- `~` ; Bitweises NOT
- <code>&lt;&lt;</code> ; Linksverschiebung
- `>>` ; Rechtsverschiebung

## Zuweisungsoperatoren

- `=` ; Zuweisung
- `+=` ; Additionszuweisung
- `-=` ; Subtraktionszuweisung
- `*=` ; Multiplikationszuweisung
- `/=` ; Divisionszuweisung
- `%=` ; Modulo-Zuweisung
- `&=` ; Bitweises-UND-Zuweisung
- `|=` ; Bitweises-ODER-Zuweisung
- `^=` ; Bitweises-XOR-Zuweisung
- <code>&lt;&lt;=</code> ; Linksverschiebungszuweisung
- `>>=` ; Rechtsverschiebungszuweisung

## Sonstiges

- `?` ; Ternärer Operator (z. B. `c = flag ? 1 : 2`)
- `as` ; FFI-Zeiger-Typkonvertierung (z. B. `y = x as *byte`)
- `..` ; Slice-Bereich (z. B. `arr[1..3]`, `arr[1..]`, `arr[..3]`)
