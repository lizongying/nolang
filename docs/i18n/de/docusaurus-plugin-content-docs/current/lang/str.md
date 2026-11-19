---
sidebar_position: 3
---

# Zeichenketten

Nolang-Zeichenketten (`str`) sind eine heap-allokierte Bytefolge und unterstützen verschiedene Operatoren und Methoden.

## Internes Layout und öffentliche API

Der Descriptor von `str` (`%str-long`) ist auf 64-Bit-Plattformen **16 Bytes** groß:

```llvm
%str-long = type { i32, i32, i8* }   ; u32 len, u32 cap, data
```

| Feld | Interner Typ | Öffentlicher Typ | Bedeutung |
|---|---|---|---|
| `len` | `u32` (LLVM `i32`) | `i64` | logische Bytelänge; `s.len-bytes()`, `len(s)`, `s._len()` |
| `cap` | `u32` (LLVM `i32`) | `i64` | physische Kapazität (nur str-long gültig); `s.cap()`, `s._cap()` |
| `data` | `i8*` | `[]byte` / `*byte` | Zeiger auf den Heap-Puffer |

Intern werden `len` / `cap` auf `u32` verengt, um den Descriptor von 24 auf 16 Bytes zu schrumpfen (`i32` + `i32` + ein 8-Byte-ausgerichteter Zeiger). **Die öffentliche API bleibt durchgängig `i64`**: Beim Lesen erfolgt an der Grenze `zext i32 -> i64`, beim Schreiben `trunc i64 -> i32`. Standardbibliothek und Benutzercode müssen (und sollten) diese Umwandlung nicht selbst vornehmen; die Signaturen von `s.len-bytes()`, `s.len()`, `s.cap()`, `s._len()` / `s._cap()` / `s._set-len()` / `s._set-cap()` bleiben `i64`.

> ⚠️ Da die internen Zähler `u32` sind, ist die maximale Länge bzw. Kapazität einer einzelnen `str` auf 4 GiB (`4294967295`) begrenzt. Ein größerer `i64`-Wert wird an der Grenze abgeschnitten; normale Zeichenkettenverarbeitung ist nicht betroffen, aber vor der Vorausweisung riesiger Puffer sollte man selbst prüfen.
>
> `txt` ist **von dieser Änderung nicht betroffen**: Es bleibt der feste 256-Byte-Aufbau `{ [255 x i8] data, i8 len }`, besitzt kein `cap`-Feld und die Länge ist 1 Byte.

## Zeichenkettenoperatoren

### Verkettung (`-`)

Mit dem Operator `-` werden Zeichenketten verkettet:

```no
; 字面量拼接
s = 'Hello' - ' ' - 'World'

; 與變量拼接
greeting = 'Hello, ' - name
```

### Wiederholung (`*`)

Mit dem Operator `*` wird eine Zeichenkette wiederholt:

```no
s = 'Hello' * 3
```

### Vergleich (`==` / `!=` / `str.compare`)

`str` implementiert **nur den Gleichheitsvergleich**: `==` / `!=` laufen über `@str_eq` der Laufzeit, mehrstellige Zeichenketten sind völlig zulässig.

Die Vergleichsoperatoren (<code>&lt;</code> `>` <code>&lt;=</code> `>=`) besitzen **keine lexikografische Zeichenketten-Semantik**; ihre Verwendung auf Zeichenketten ist ein Kompilierfehler:

- **Erlaubt**: ein **einzelnes Zeichen** als Zeichenkettenliteral `'a'`, implizit als `char` (Codepunkt) behandelt; vergleichbar mit einem anderen Literal eines einzelnen Zeichens oder einem `char` (`"a"`).
- **Fehlerhaft**: mehrstellige Zeichenketten — `'ab'`, eine `str`-Variable oder ein Aufruf mit `str`-Rückgabe (früher wurde **still false** zurückgegeben).

Für lexikografische Reihenfolge `str.compare(b)` verwenden:

```no
s = 'abc'
b = 'abd'
c = s.compare(b); -1（s < b）/ 0（相等）/ 1（s > b）

'a' <= 'z'; true — 單字元字串隱式 char
ch >= 'a'; ch 為 char，OK
s < b; 編譯錯誤：多字元字串
s.compare(b) < 0; 正確寫法
```

## Indizierung und Slicing

**Indizierung**: Der inferierte Typ von `s[i]` ist **`?char`** (ein Unicode-Codepunkt), nicht `byte` — bei Out-of-Bounds wird `err` (`code.index-out`) zurückgegeben, im gültigen Bereich `ok(Codepunkt)` (dasselbe einheitliche Regelwerk wie bei den ganzen Zahlen `+ - * / %`). Bei Strings mit mehrbyteigen Zeichen dekodiert `s[i]` das UTF-8 vom Anfang der Zeichenkette vorwärts bis zum i-ten Codepunkt (siehe untenstehende Leistungsanmerkung).

```no
#{fmt-skip}
s = 'Hello World'
c = s[0]: { ok -> it  -> _ }   ; 'H' 的碼點（?char 必須顯式解包，空臂取 '\0'）
```

> Ein `?char` lässt sich nur auf drei Arten weiterverwenden (**`#{index-out=DEF}` wurde in D11 veraltet und entfernt**, ein verbliebener Kommentar ist ein Kompilierfehler;
> es gibt auch keine Abkürzung „expliziten plain-Typ auf plain zurückführen", `c char = s[i]` muss ebenfalls explizit konsumiert werden):
> **Match-Entpackung** (`x = s[i]: { ok -> it, -> _ }`, leerer Arm liefert den `char`-Nullwert `'\0'`),
> **Place-Match-Schreiben** (`s[i]: { ok -> it = x, -> _ }`) oder **Kurzform `?=`-Weiterwerfen** (die Funktion hat einen `?T`-Ergebnisparameter, oder die Anweisung steht im Rumpf eines Block-Match). Details und Beispiele siehe〈syntax · Sicherer Index〉.

**Slicing**: `s[a..b]` gibt eine **Ansicht** der zugrunde liegenden Zeichenkette zurück; der Typ bleibt **`str`** (Speicher wird geteilt, nicht kopiert). Die Slice-Indizes sind **Codepunkt-Positionen**, konsistent mit `s[i]`:

```no
sub = s[6..]; 'World'
sub = s[6..11]; 'World'
sub = s[0..5); 'Hello'（左閉右開）
```

> Die Slice-Indizes sind **Codepunkt-Positionen**, keine Byte-Offsets. Wenn ein Byte-Offset-Slice benötigt wird (z. B. zusammen mit einer `s.byte(i)`-Iteration), verwende `s.slice-bytes(start, end)`.

```no
; 長度
n = s.len(); code point 數（Unicode 字符數）
n = s.count(); 同上（舊方法名，仍可用）
n = s.len-bytes(); byte 長度（UTF-8 位元組數）
; 注意：裸 s.len（結構體欄位）已不再支援，讀寫都會報錯；請改用 s.len() / s.len-bytes()
```

### Implizite Konvertierung (char → str)

Ein `char` kann **implizit** in `str` konvertiert werden: Der Codepunkt wird UTF-8-kodiert in eine neue Zeichenkette. Daher sind alle folgenden Schreibweisen zulässig, **ohne** `char.to-str()` manuell aufrufen zu müssen:

```no
s = 'héllo'; 'h' 'é' 'l' 'l' 'o'（é 為多字節）

; 1) s[i] 的 char 直接賦值給 str 變數 → 隱式轉成單字元字串
a str = s[0]; 'h'

; 2) 切片結果本身型別已是 str，直接賦值即可
b str = s[0..1]; 'hé'（下標 0..1，兩端皆含）

; 3) char 參與字串拼接（-）時隱式轉為 str
msg = 'first: ' - s[0]; 'first: h'

; 4) char 與 str 比較時隱式轉為 str
ok = s[0] == 'h'; true
```

> **`'...'` ist ein `str`-Literal, `"..."` ein `char`-Literal** — selbst wenn `'x'` nur einen Codepunkt enthält, bleibt es ein `str`. Wenn daher **beide** Seiten von `+` / `-` Literale sind (`'...'` mit `"..."`), greift die obige implizite Konvertierung, und das Ergebnis ist eine **Verkettung**, keine Integer-Arithmetik:
>
> ```no
> 'x' - "a"                ; "xa"（拼接；不是 120-97 = 23）
> "a" + 'x'                ; "ax"
> 'xy' + "a"               ; "xya"
> ```
>
> Sobald eine Seite **kein Literal** ist (char-Variable, `s[i]`, ganze Zahl), nimmt ein einstelliges `'...'` als **Byte** an der Integer-Arithmetik teil — das ist beabsichtigt und ermöglicht die übliche Ziffern-zu-Wert-Umrechnung:
>
> ```no
> c char = "5"
> n = c - '0'              ; 5（位元組相減，不是 "50"）
> m = 'A' + 1              ; 66
> "z" - "a"                ; 25（兩側都是 char 字面量，char 相減）
> 'x' - c                  ; 23（c 是變數 → 位元組運算）
> ```
>
> Umgekehrt tritt die Byte-Rückfaltung **nur dann** ein, wenn die andere Seite eine ganze Zahl oder ein Zeichen ist. Ist die andere Seite ein `bool`, `f64`, eine Struktur oder ein anderer Wert **ohne Byte-Bedeutung**, bleibt `'...'` eine Zeichenkette und der gesamte Ausdruck ist eine **Verkettung**, wobei die andere Seite über `to-str` dargestellt wird:
>
> ```no
> b = true
> 'x' - b                  ; "xtrue"（不是 120 - 1 = 119）
> 'x' - 1.5                ; "x1.5"
> 'x' - 'y'                ; "xy"（兩側都是字串字面量）
> ```
>
> Daher kann `'x' - b` einer `str` zugewiesen werden: `s str = 'x' - b` ergibt `"xtrue"`; `s str = 'x' - 1` wird hingegen abgelehnt, weil das eine Byte-Operation ist (das Ergebnis ist eine Zahl).

> Die implizite Konvertierung kodiert nur **einen einzelnen Codepunkt** als `str`. Wenn der Zahlenwert gewünscht ist, verwende direkt das `char` selbst (z. B. gibt `print(c)` den Dezimalwert des Codepunkts aus); für eine explizite Konvertierung kann weiterhin `c.to-str()` verwendet werden (gleichbedeutend mit der impliziten).
>
> Ein `char` repräsentiert einen einzelnen Unicode-Skalarwert, sein zugrunde liegender Speichertyp ist **`i32`** (gültiger Bereich `0 ..= 0x10FFFF`).
>
> Ein `char` **kann normal an Integer-Operationen teilnehmen** (z. B. `z = a + 25`, `u = ch - 32`), wobei mit dem Codepunktwert in i32-Breite gerechnet wird; anders als die Integer-Familie gibt er nicht standardmäßig `option<int>` zurück, daher wird kein Fehler „unbehandelter Integer-Überlauf" ausgelöst.
>
> **Darstellung**: Wird ein `char` in Text überführt, wird stets der **Dezimalwert des Codepunkts** ausgegeben (`"A"` → `65`, `"é"` → `233`), und die Ergebnisse von `print(c)` und der String-Interpolation `print('{c}')` **müssen übereinstimmen**. char-Arithmetik erfolgt als **vorzeichenbehaftetes i32**, daher ist `"x" - "y"` gleich `-1`, und beide Wege geben `-1` aus (niemals `4294967295` auf einem Weg). Gültige Codepunkte liegen stets im Bereich `0 ..= 0x10FFFF` (Bit 31 ist 0), wo vorzeichenbehaftete und vorzeichenlose Erweiterung übereinstimmen; diese Regel betrifft also nur negative Werte aus char-Arithmetik außerhalb des Bereichs. Wenn das **Zeichen selbst** statt der Zahl gewünscht ist, verwende `c.to-str()` (`"A".to-str()` → `"A"`) — das ist eine andere Operation.

## ASCII-Optimierung und Leistungshinweise

Die Indizierung `s[i]` bei `str`/`txt` liefert den **i-ten Codepunkt (Unicode-Zeichen)**, nicht das i-te Byte. Für Zeichenketten, die mehrbyteige UTF-8-Zeichen enthalten können (etwa Chinesisch, Emoji, `é`), muss jedes `s[i]` das UTF-8 vom Anfang der Zeichenkette durchlaufen, bis der i-te Codepunkt erreicht ist — die Zeitkomplexität beträgt damit **O(n)**.

Kann der Compiler **beweisen**, dass eine Zeichenkette vollständig aus ASCII-Zeichen (Codepunkte 0–127) besteht, ist jedes Byte genau ein Zeichen, und `s[i]` adressiert das zugrunde liegende Byte direkt und degeneriert zu **O(1)**. Der Compiler beweist reines ASCII anhand folgender Regeln:

- Explizite Annotation: `#{ascii}` bei Deklaration oder Zuweisung;
- Zuweisung eines ASCII-Zeichenkettenliterals (z. B. `'hello'`);
- Bezeichner-Propagierung: `b = a`, wenn `a` bereits als ASCII bewiesen ist, ist auch `b` ASCII;
- Verkettung (`-`), bei der beide Seiten als ASCII bewiesen sind.

```no
; ASCII 字面量 → 編譯器證明為純 ASCII，s[i] 為 O(1)
name = 'hello'
c = name[1]; O(1)，取字節 'e'

; 含多字節字符 → 無法證明 ASCII，s[i] 為 O(n) 碼點迭代
s = 'héllo'
c = s[1]; O(n)，取第 1 個碼點 'é'（而非字節）
```

### Leistungswarnung (LSP / `no vet`)

Wenn du **innerhalb einer Schleife manuell nach Codepunkt-Index traversierst** und die `str`/`txt` nicht als ASCII bewiesen ist (also Schreibweisen wie `for i <- [0..s.count()): { s[i] }`, bei der Zeichen einzeln nach Codepunkt-Index geholt werden), geben `no vet` und der Editor-LSP eine statische Warnung der Stufe **WARNING** aus (die Syntax wird nicht verboten, nur hingewiesen). Bei dieser Schreibweise muss jedes `s[i]` das UTF-8 vom Anfang der Zeichenkette durchlaufen, um den i-ten Codepunkt zu erreichen; die gesamte Schleife degeneriert zu **O(n²)**.

Ein einzelnes `s[i]` außerhalb einer Schleife (nur einmal O(n)) löst **keine** Warnung aus; auch eine direkte `for c <- s`-Zeicheniteration löst **keine** Warnung aus (siehe unten).

Es wird eine schnellere Variante empfohlen:

- Wenn jedes Zeichen durchlaufen werden soll, verwende `for c <- s` für eine Vorwärtsiteration — das ist die bevorzugte O(n)-Einmal-Scan-Schreibweise und vermeidet das O(n²) wiederholter `s[i]`-Zugriffe. Der Compiler schreitet nach **UTF-8-Codepunkt (Unicode-Zeichen)** vor und speichert bei jedem Schritt den dekodierten Codepunktwert in der Schleifenvariable `c`; semantisch entspricht dies `for c <- s.to-chars()`:
  ```no
  c <- s: {
      ; c 為當前碼點，類型 char（已按 UTF-8 解碼，非字節）
  }
  ```
- Wenn nur Byte-Zugriff benötigt wird (z. B. Kodierung/Dekodierung, Hashing, memcmp), verwende den Fluchtweg `s.byte(i)` — er adressiert stets direkt das i-te Byte, ist **immer O(1)** und tritt nicht in den std-Funktionsrumpf ein:
  ```no
  b = s.byte(0); 取第 0 個字節，類型 ?byte，O(1)
  out = 0
  b: {
      ok -> {
          out = it; 在界內：ok(byte)
      }

      -> {
          out = 0; 越界：nil（空臂合成零值）
      }
  }
  ```
  > Das Ergebnis von `s.byte(i)` ist **`?byte`** (seit 2026-10-21): Der früher rohe Byte-Lesevorgang führte keine Bereichsprüfung durch, ein Out-of-Bounds-Index war undefiniertes Verhalten. Jetzt wird bei Out-of-Bounds `nil` zurückgegeben — beachte, dass sich dies **unterscheidet** von der Container-Indizierung `arr[i]` / `s[i]`, die bei Out-of-Bounds `err` (`code.index-out`) zurückgibt: `s.byte(i)` ist ein Byte-Ebene-Fluchtweg, Out-of-Bounds bedeutet nur „dieses Byte gibt es nicht", keinen Fehler.
  > (Siehe design/option-unified.md §3.1 Punkt 5). Daher meldet `b i64 = s.byte(0)` den Fehler
  > `15w45dqk` — eine explizite plain-Annotation zusammen mit einem option-Aufruf ist stilles Entpacken und ein harter Kompilierfehler;
  > verwende `?=`-, eine zweistufige Match-Form (`b = s.byte(0): { ok -> it, -> _ }`) oder `_ =` zum Konsumieren.
  > `txt.byte(i)` hat dieselbe Semantik (`byte`-Feld 1 von `txt` ist die Bytelänge).

> Die Byte-Zugriffe in der Standardbibliothek (src/std) wurden alle auf `s.byte(i)` umgestellt: UTF-8-Kodierung/Dekodierung (`decode-cp`), Vergleich (`compare`/`starts-with`/`ends-with`), Hashing, Slice-Kopie (`slice`/`repeat`/`trim`/`copy`), `to-bytes`/`to-upper`/`to-lower`/`reverse`/`replace-char`/`count`/`index` in `str.no`/`txt.no` und weitere. Daher muss `no vet` keine Standardbibliotheksdateien mehr überspringen; der Elementzugriff `x[i]` bei den übrigen Typen `[]byte`/Slice/Array erfolgt ohnehin elementweise (Byte-Index), bleibt unverändert und wird nicht gemeldet.

| Schreibweise | Semantik | Komplexität | Hinweis |
|---|---|---|---|
| `s[i]` (ASCII bewiesen) | i-tes Byte = i-tes Zeichen | O(1) | automatisch bewiesen, keine Änderung nötig |
| `s[i]` (ASCII nicht bewiesen, in Schleife) | i-ter Codepunkt | O(n²) Anti-Pattern | löst LSP-Warnung aus |
| `s[i]` (ASCII nicht bewiesen, außerhalb Schleife) | i-ter Codepunkt | O(n) einmalig | keine Warnung |
| `s.byte(i)` | i-tes Byte (`?byte`, Out-of-Bounds `nil`) | O(1) | Byte-Zugriffs-Fluchtweg |
| `for c <- s` | Codepunkt für Codepunkt | O(n) Einmal-Scan | bevorzugt für Iteration, keine Warnung |

## Zeichenkettenmethoden

```no
#{fmt-skip}
; 比較
ok = a.eq(b, n)               ; 相等比較（方法）
c = s.compare(b)              ; 字典序比較

; 查找（index/rindex 返回「碼點（字符）位置」，與 s[i] 一致）
pos = s.index(sub)             ; 首次出現的碼點下標，未找到 -1
pos = s.index-from(sub, k)     ; 從第 k 個碼點開始查找
pos = s.rindex(sub)            ; 最後一次出現的碼點下標
pos = s.rindex-from(sub, k)    ; 從第 k 個碼點反向查找
pos = s.last-index(sub)        ; rindex 的別名
ok  = s.contains(sub)          ; 是否包含
ok  = s.starts-with(sub)       ; 前綴判斷
ok  = s.ends-with(sub)         ; 後綴判斷
ok  = s.empty()                ; 是否為空

; 注意：index 系列返回的是「碼點位置」，因此 s[s.index(sub)] 取到的是 sub 的首字符，
;       與 s[i] 的碼點索引語義一致。例如 '日本語'.index('語') == 2，且 s[s.index('語')] == '語' 的碼點。
;       若需「碼點位置」切片，s.slice(start, end) 即碼點語義（與 s[i] 一致）；
;       若需「字節偏移」切片（如配合 s.byte 遍歷），用 s.slice-bytes(start, end)。
;       str 內部提供字節級查找 str.find-byte-from（供 split 等複用）。
;       注意：txt 型別的 slice/at 仍為字節語義（txt 是裸字節緩衝），與 str 語義分叉。

; 轉換
out = s.to-upper()             ; 轉大寫
out = s.to-lower()             ; 轉小寫
out = s.trim()                 ; 去首尾空白
out = s.trim-char(c)           ; 去指定字元
out = s.repeat(n)              ; 重複
out = s.reverse()              ; 反轉
out = s.slice(start, end)      ; 切片（下標為碼點位置，與 s[i] 一致）
out = s.slice-bytes(b, e)      ; 切片（下標為字節偏移，內部使用）
val = s.replace-char(old, new) ; 取代字元

; 轉換為其他型別
b = s.to-bytes()               ; 轉 []byte
v = s.to-i64()                 ; 轉 ?i64
v = s.to-f64()                 ; 轉 ?f64
v = s.to-bool()                ; 轉 ?bool

; 分割與連接
parts = s.split(sep)           ; 分割（返回 []str）
out = ss.join(sep)             ; []str 連接

; 複製與填充
dst = s.copy()                 ; 字串複製
s.fill(val byte)               ; 填充 byte 值
```

## Zeichenketten- und Zahlenkonvertierung

```no
; 數值轉字串（方法）
s = i64.to-str(); i64 轉字串
s = f64.to-str(); f64 轉字串
s = bool.to-str(); bool 轉 "true"/"false"
s = byte.to-str(); byte 轉字串
s = char.to-str(); char 轉字串

; 字串轉數值（返回 option）
v = s.to-i64(); 回傳 ?i64
v = s.to-i32(); 回傳 ?i32
v = s.to-u64(); 回傳 ?u64
v = s.to-f64(); 回傳 ?f64
```

## Schreiben per Index deaktiviert (`str` 2026-11-11 / `txt` 2026-11-12)

> **`s[i] = v` ist der Kompilierfehler `idxwrite`**, ohne Ausnahme für die drei Basen
> `with-cap` / `with-len` / `with-cap-len`. **Auch `t[i] = c` bei `txt` ist deaktiviert** (Grund siehe unten).
>
> Der Grund bei `str`: Es war die einzige Quelle für „Schreiben hinter das Ende eines allokierten Puffers" — ein Puffer mit len=0 wuchs durch direkte Stores, und Schreiben bei `i >= cap` trat aus dem allokierten Bereich heraus. Die Wachstumsgarde `ensureStrLongBuffer` im Codegen existierte genau dafür; nach dem Verbot direkter Stores ist sie für Benutzercode nicht mehr erreichbar und bleibt nur noch als defensiver Code.
>
> Der Grund bei `txt` ist ein anderer, aber ebenso verwerflich: `t[i] = c` ist ein **Wieder-Kodieren eines Codepunkts** und schreibt ihn zurück, wodurch sich die Länge implizit ändert (bei `i` gleich der Codepunktzahl entspricht es einem Anhängen, und alles über 255 Bytes hinaus wird still verworfen). `txt` besitzt keinen Heap und kein Wachstum, aber „eine Index-Zuweisung hat heimlich die Länge geändert" ist dieselbe Gefahr.

Vier weiterhin zulässige Schreibweisen:

```no
; ① 逐位元組「構造」：[]byte 緩衝承接，收尾整體轉回 str
b []byte = with-cap-len(n, n)
b[0] = 48
b[i] = src.byte(i): {
    ok -> it

    -> 0
}
s = b.slice(0, k).to-bytes-str(); 也可 b.to-bytes-str()（整段）

; ② 原位「覆寫」既有內容：str.fill 把 len 個位元組全填成同一個值，長度不變
s = 'hello'
s.fill(72); 'HHHHH'

; ③ 下標寫只剩 place-match（以「讀」作閘門：i >= len 時靜默跳過，不增長、不越界）
s[i]: {
    ok -> it = v
    -> _
}

; ④ 要變長：用 push 家族（顯式、可審計的一次呼叫）
s.push("中");      按碼點追加（UTF-8）
s.push-str('de');  按位元組追加（原地擴容）
```

> ⚠️ **Place-Match ist ein „Codepunkt"-Gate, kein Byte-Gate.** Sein Gate stammt aus der **Lese**-Operation `s[i]` — und der Index-Lesevorgang von `str` zählt nach Codepunkten. Daher eignet sich ③ nur für ASCII-Arbeit mit „einem Codepunkt pro Slot" (UUID-Bindestriche, Zeit-Ziffern, Hex-Tabellen-Nachschlagen). Um Inhalte **Byte für Byte** zusammenzusetzen (Bytes `>= 0x80` oder eine beliebige Quellzeichenkette, die nicht-ASCII sein kann), muss ① mit `[]byte` verwendet werden:
> Messungen zufolge landen bei `f = with-len(8)` und place-match-Weisen von 224..231 nur die ersten 4 Slots, der Rest wird vom Gate still übersprungen; genau so wurden bei `path.split` Nicht-ASCII-Dateinamen abgeschnitten.

> **Längenkürzung**: Ein nacktes `s.len = n` wird nicht mehr unterstützt, verwende stattdessen Slicing — `s.slice(0, n)` ist ein **Codepunkt**-Slice, `s.slice-bytes(0, n)` erst ein Byte-Slice. (`b.slice(0, k)` auf einem `[]byte`-Puffer ist natürlich ein Byte-Slice.)
>
> **Wachstum per `with-cap(n)`-gebautem `str`**: `s str = with-cap(256)` ist ein len=0-Puffer, der bereits Kapazität besitzt; das erste `push` schreibt direkt hinein, ohne Expansion (`push-str` reallokiert nur, wenn `len + Anhängelänge` die `cap` überschreitet, und wächst dabei verdoppelt). Ein byteweises `buf[i] = b` bleibt für `str` verboten.

### Wachstum: Die `push`-Familie (2026-11-12)

Nach dem Verbot direkter Stores **muss Wachstum ein expliziter, einmaliger Aufruf sein**:

| Methode | Semantik |
|---|---|
| `s.push(c char)` | einen Codepunkt anhängen, UTF-8-kodiert (intern an `push-str` delegiert) |
| `s.push-str(t str)` | eine Zeichenkette anhängen, **Byte-Kopie**, In-Place-Expansion |

```no
s = 'ab'
s.push("中");      'ab中'，len-bytes 5、len 3
s.push-str('de');  'ab中de'，len-bytes 7

t = 'zz'
t.push-str(t);     'zzzz'（自我追加安全：先 retain 源、再用 memmove 拷貝）

u = ''
u.push-str('');    ''（空源不改變任何東西，也不分配）
```

> `push-str` ist ein Compiler-Builtin (`#{buildin}`, im MIR zu „In-Place-Expansion + memmove + Längenänderung" abgebaut), keine std-Schleife: Ein Anhängen kostet O(Anhängelänge), wiederholtes Anhängen ist amortisiert O(1) (Verdopplung).
> `push` durchläuft die UTF-8-Kodierung, daher wächst `len-bytes` um die Kodierungslänge des Codepunkts, während `len` stets um 1 wächst.

> **`txt`-Schreiben ist ebenfalls deaktiviert (2026-11-12)**: `t[i] = c` kodiert einen Codepunkt neu und schreibt ihn zurück, wodurch sich die Länge als Nebeneffekt ändert — bei `i` gleich der Codepunktzahl entspricht es einem Anhängen, und alles über 255 Bytes hinaus wird still verworfen.
> „Eine Index-Zuweisung hat heimlich die Länge geändert" ist genau das, was das Verbot beseitigen soll; txt wird wie str behandelt. Die Wachstumseinträge von txt sind `t.push(c)` / `t.push-str(s)` / `t.push-txt(u)`; In-Place-Änderungen erfolgen per Place-Match.
> Das Lesen `t[i]` liefert weiterhin den Codepunkt `char`, das hat sich nicht geändert.
>
> Die rohen Byte-Schreiber `str._set-byte` / `txt._set-byte` sind seit 2026-11-11 **std-privat** (nur `str.fill` / `txt.push` innerhalb von std rufen sie auf), Benutzercode kann sie nicht verwenden.
> Um ein `txt` Byte für Byte zusammenzusetzen, erstelle zuerst ein `[]byte` und kopiere es in einem Schritt mit `txt.from-bytes(b)` hinein (über 255 Bytes hinaus wird automatisch abgeschnitten, die Länge wird aus der tatsächlichen Bytezahl gesetzt).

## Vorausweisung (eingebaute Syntax)

Über die drei eingebauten Syntaxformen `with-cap`, `with-len` und `with-cap-len` kann Heap-Speicher vorab reserviert werden, um wiederholte Reallokationen bei späteren `push`-Operationen zu vermeiden:

```no
; with-cap(cap)：配置 cap 容量，len=0（需 push 後才能索引）
s = with-cap(256); str，len=0, cap=256

; with-len(len)：配置 len 容量，len=cap（可直接索引讀）
s = with-len(128); str，len=128, cap=128

; with-cap-len(cap, len)：同時指定容量與長度，適合「預留成長空間」場景
s = with-cap-len(512, 64); str，len=64, cap=512
```

| Eingebaute Syntax | Parameter | len | cap | Anwendungsfall |
|---|---|---|---|---|
| `with-cap(cap)` | 1 | 0 | cap | Nur Obergrenze bekannt, Länge wächst per push |
| `with-len(len)` | 1 | len | len | Feste Länge, direkter Index-Lesezugriff |
| `with-cap-len(cap, len)` | 2 | len | cap | Initiale Länge bekannt und Expansionsspielraum nötig |

> Die Parameter und die Rückgabewerte von `s.cap()` / `s.len-bytes()` sind allesamt **`i64`** (öffentliche API unverändert), intern speichert der Descriptor len / cap jedoch als **`u32`**. Der Aufrufer sieht weiterhin 64-Bit-Werte, die Umwandlung erfolgt im Compiler an der Descriptor-Grenze;
> siehe〈Internes Layout und öffentliche API〉. Auch Feldzuweisungen wie `s.len = n` / `s.cap = n` laufen über diese Grenze und überschreiben daher keine benachbarten Header-Felder.

> ⚠️ **Ein `str`-Puffer erlaubt grundsätzlich keinen direkten Index-Store** (seit 2026-11-11, und `with-cap`-Basen sind ebenfalls nicht mehr ausgenommen).
> Die `idxwrite`-Ausnahme für with-cap gilt jetzt nur noch für **`[]byte` / `[]T` / feste Arrays**: Deren len=0-Puffer wachsen weiterhin über `buf[i] = b`, es gibt keine gleichwertige Alternativschreibweise. Für `str` weiche auf einen `[]byte`-Puffer + `to-bytes-str()` aus (siehe Abschnitt „Schreiben per Index deaktiviert").
> Bei `with-len(n)` / `with-cap-len(cap, len)` ist die Länge bereits vorhanden; **Schreiben erfordert Place-Match `buf[i]: { ok -> it = v / -> _ }` oder `buf[i] ?= v`** — wie bei jedem anderen Container ist ein direkter Store der Kompilierfehler `idxwrite`. (Seit 2026-11-10 ist `with-len` nicht mehr ausgenommen.)

> **Typinferenz**: Der Ergebnistyp der drei eingebauten Syntaxformen wird aus der linken Seite der Zuweisung inferiert; sie können für `str` oder `[]T`-Slices verwendet werden.
