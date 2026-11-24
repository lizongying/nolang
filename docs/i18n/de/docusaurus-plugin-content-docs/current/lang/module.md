---
sidebar_position: 6
---

# Präfixe für modulübergreifende Aufrufe

Nolang schreibt eine verbindliche Modul-Namensraumkonvention vor: Wenn man in einer `.no`-Datei Funktionen oder Konstanten aus **anderen Modulen** (anderen `.no`-Dateien) aufruft, muss das Präfix `ShortName.` verwendet werden. Dadurch werden Namenskonflikte zwischen Modulen vermieden.

Standardbibliotheksmodule werden vom Compiler automatisch geladen, **eine explizite Importanweisung ist nicht nötig** (keine `# std/...`-Annotation erforderlich). Einfach das Präfix `ShortName.` verwenden. `no fmt` entfernt automatisch alle reinen std-Modulimports ohne Alias (z. B. `# std/noml`, `# std/math`, `# std/fmt`); ein Alias (`# std/noml n`) oder ein Funktionsimport (`# std/math.sqrt`) bleibt erhalten (siehe [Codierrichtlinien · Automatische Bereinigung von Importanweisungen](./code-style#automatische-bereinigung-von-importanweisungen-dedup--std-imports-ohne-alias-entfernen--sortieren)).

> **Abhängigkeiten und Pfadauflösung**: Der Abhängigkeitsschlüssel lokaler Pakete kann über `workspace.noml` auf einen Kurznamen oder eine vollständige URL abgebildet werden. Siehe [Abhängigkeitstypen und Versionsregeln](../usage#依賴類型與版本號規則).

## Definition von ShortName

ShortName ist das letzte Segment des Modulpfads und wird als Präfix für modulübergreifende Aufrufe verwendet.

| Dateipfad | FullPath | ShortName | Beschreibung |
| --- | --- | --- | --- |
| `std/math.no` | `math` | `math` | Datei auf oberster Ebene |
| `std/fs.no` | `fs` | `fs` | Datei auf oberster Ebene |
| `std/net/net.no` | `net/net` | `net` | Letztes Pfadsegment |
| `std/net/client.no` | `net/client` | `client` | Letztes Pfadsegment |
| `std/crypto/sha256.no` | `crypto/sha256` | `sha256` | Letztes Pfadsegment |
| `std/archive/gzip.no` | `archive/gzip` | `gzip` | Letztes Pfadsegment |

ShortName ist also das letzte Segment von FullPath nach Aufteilung durch `/` (z. B. `crypto/sha256` → `sha256`).

## Wann ein Präfix erforderlich ist

Beim Aufruf von Funktionen oder Konstanten auf Modulebene, die in anderen Modulen definiert sind, muss das Präfix `ShortName.` verwendet werden.

```no
; 模組級函數
sha256.sha256(data)
sha256.sha256-hex(data)
fs.open(path, opts)
gzip.gzip-decompress(data)
math.pow(x, y)

; 模組常量
net.NET-BUF-SIZE
math.PI
```

### Namenskonvention für Funktionen

**Funktionsnamen dürfen nicht mit dem Modulnamen präfigiert werden.** Funktionen innerhalb eines Moduls sollten einfach kurze, aussagekräftige Namen tragen; das Präfix `ShortName.` bei modulübergreifenden Aufrufen trägt die Modulinformation bereits bei.

```no
; ✅ 正確：函數名簡潔，不加模組名前綴
; tail.no
; 入口函數直接用模組名
tail = () {
    ...
}

; 輔助函數用簡短名稱
atoi = (s str) (v i64) {
    ...
}
```

```no
#{fmt-skip}
; ❌ 避免：函數名冗餘帶上模組名前綴
; tail-run = () { }
; tail-atoi = (s str) (v i64) { }
```

Bei modulübergreifenden Importen gilt dasselbe:

```no
; ✅ 簡潔直觀
# /src/tail.tail
# /src/mktemp.mktemp
```

```no
#{fmt-skip}
; ❌ 冗餘
; # /src/tail.tail-run
; # /src/mktemp.mktemp-run
```

> **Schlüsselwörter vermeiden**: `awy` (Schlüsselwort für Coroutinen-Gruppen), `match` (Schlüsselwort für bedingtes Matching) usw. dürfen nicht als Funktionsnamen verwendet werden. `run` ist nicht mehr reserviert und kann als gewöhnlicher Bezeichner dienen. Für die Einstiegsfunktion empfiehlt sich der Modulname selbst (z. B. `ping.no` → `ping`).

## Wann kein Präfix erforderlich ist

In folgenden Fällen ist kein Präfix nötig:

### 1. Globale Funktionen (`with-cap` / `with-len` / `with-cap-len` / `print` / `eprint` / `format` / `exit`)

Diese 7 Funktionen sind sprachweite globale Builtins und können direkt ohne Modulpräfix verwendet werden. Ihre Kommentardeklarationen sind zur besseren Einsicht zentral in `std/global.no` abgelegt.

**Kapazitäts-/Längen-Konstruktoren:**

- `with-cap(cap)` — erzeugt einen String oder Slice mit angegebener Kapazität (len=0), Typ wird aus der Zuweisung links inferiert
- `with-len(len)` — erzeugt einen String oder Slice mit angegebener Länge
- `with-cap-len(cap, len)` — erzeugt einen String oder Slice mit angegebener Kapazität und Länge

**Ausgabe/Formatierung:**

Nolang verwendet **benannte Formatstrings** mit der Syntax `{name[:spec]}`, die Variablen direkt aus dem Gültigkeitsbereich referenzieren — positionsabhängige Argumente sind nicht nötig. Eine Validierung zur Kompilierzeit wird unterstützt. Die Ausgabe erfolgt direkt über die Syscalls `io.out`/`io.err`, ohne Abhängigkeit von libc `printf`.

- `print(s)` / `print(s0, s1, ...)` — Ausgabe auf stdout, mehrere Argumente durch Leerzeichen getrennt, **automatischer Zeilenumbruch**
- `eprint(s)` / `eprint(s0, s1, ...)` — Ausgabe auf stderr, mehrere Argumente durch Leerzeichen getrennt, **automatischer Zeilenumbruch**
- `format(s)` — gibt den formatierten String zurück (Ersatz für `sprintf`), kein automatischer Zeilenumbruch

**Prozesssteuerung:**

- `exit(code)` — beendet den Prozess mit einem Statuscode (früher `os.exit`, jetzt globales Builtin, kein `os.`-Präfix nötig)

**Bei mehreren Argumenten ist jedes Stringliteral eine Vorlage, kein C-artiger Formatstring.** In `print('result={val}', 42, 'result={val}')` ist jedes Stringliteral **für sich** eine Vorlage: sein `{val}` wird aus dem Gültigkeitsbereich der **Aufrufstelle** aufgelöst, unabhängig von den anderen Argumenten desselben Aufrufs. Ein Literal **beschreibt oder konsumiert nie** die folgenden Argumente — es handelt sich nicht um eine Ersetzung nach Art von `printf('%d', 42)`. Nicht-literale Argumente (Variablen, Ausdrücke) werden als gewöhnliche variable Argumente behandelt, durch Leerzeichen getrennt, mit einem einzigen abschließenden Zeilenumbruch für den gesamten Aufruf.

```no
val = 42
print('result={val}', 42, 'result={val}'); result=42 42 result=42
tmpl = '{val}'
print(tmpl); {val} —— 變數是純文字，不做替換
print('a={val}', val); a=42 42
```

`format(s)` und das veraltete `sprintf(s)` akzeptieren weiterhin nur **einen einzigen Formatstring**; es gibt keine Mehrargument-Form.

> `printf` und `eprintf` wurden **entfernt**: Die Namen bleiben in der Symboltabelle (ein Aufruf wird weiterhin aufgelöst und der Editor vervollständigt sie), aber **jeder Aufruf ist ein harter Kompilierfehler** (`[printf-depr]`), dessen Meldung direkt auf die Migration hinweist. `sprintf` funktioniert weiterhin, ist aber **veraltet**. Die Entsprechungen:
> - `printf(s)` → `print(s)` (automatischer Zeilenumbruch) oder `io.out(s)` (kein Zeilenumbruch, stdout)
> - `eprintf(s)` → `eprint(s)` (automatischer Zeilenumbruch) oder `io.err(s)` (kein Zeilenumbruch, stderr)
> - `sprintf(s)` → `format(s)` (gibt formatierten String zurück)
>
> `io.out`/`io.err` sind Low-Level-Befehle, die **ohne Zeilenumbruch** ausgeben. Da Modulaufrufe das Modulpräfix enthalten müssen, trägt `io.err` explizit das Modulpräfix und kollidiert nicht mit dem Option-Konstruktor `err()`; selbst bei Namensgleichheit unterscheidet das Modulpräfix.

```no
; 容量/長度構造（無前綴）
s str = with-cap(256); ✅ 預分配 256 位元組的 str
v []i64 = with-cap(100); ✅ 預分配 100 個元素的切片
v []i64 = with-cap-len(200, 100); ✅ 容量 200、長度 100
v []i64 = with-len(100); ✅ 長度 100 的切片

; 輸出/格式化（無前綴）
print('hello {n}'); ✅ 自動換行
print(a, b, c); ✅ 多參數，空格分隔，自動換行
print('x={n}', n); ✅ 字面量是模板，變數是值
print('result={val}', 42, 'result={val}'); ✅ 每個字面量各自替換
print(); ✅ 空參數，只輸出換行
s = format('x={x}'); ✅ 返回格式化字串（單一格式字串）
eprint('err: {n}'); ✅ 輸出到 stderr 並換行
eprint('err:', a, b); ✅ 多參數，stderr 空格分隔
print('編號 {id:06} 金額 {money:.2f}'); ✅ 支援對齊、填充、寬度、精度

; 底層命令（需要模組前綴）
io.out('no-newline-here'); ✅ 輸出不換行（替代 printf）
io.err('err-no-newline'); ✅ stderr 不換行（替代 eprintf）

; 其他所有跨模組調用都需要前綴
fs.open(path, opts); ✅ 帶前綴（builtin 也需要）
```

#### Formatbezeichner

`spec` in `{name[:spec]}` unterstützt folgende Felder (feste Reihenfolge, alle optional):

```
[[fill]align][sign][#][0][width][.precision][type]
```

- `fill` — Füllzeichen (Standard: Leerzeichen), muss zusammen mit `align` verwendet werden
- `align` — `<` linksbündig, `>` rechtsbündig, `^` zentriert
- `sign` — `+` zeigt bei positiven Zahlen ein Plus, `-` nur bei negativen (Standard)
- `#` — Basispräfix (`0x`/`0o`/`0b`)
- `0` — Zahlen mit Nullen bis zur angegebenen Breite auffüllen
- `width` — Mindestfeldbreite
- `.precision` — Dezimalstellen bei Gleitkommazahlen / maximale Länge bei Strings
- `type` — `d`(Integer), `x`/`X`(hexadezimal), `o`(oktal), `b`(binär), `c`(Zeichen), `f`(Festkomma), `e`/`E`(wissenschaftlich), `g`/`G`(allgemein), `s`(String, Standard), `t`(Typname, zur Kompilierzeit bestimmt), `v`(Literalwert: Strings in einfache Anführungszeichen gesetzt, vec ruft to-str auf)

```no
x i64 = 42
u u64 = 255
pi f64 = 3.14159
s str = 'hello'
print('{x:06}'); 000042
print('{x:>10}'); 右對齊寬度 10
print('{u:#x}'); 0xff
print('{pi:.2f}'); 3.14
print('{pi:8.3e}'); 3.142e+00
print('{s:<10}'); hello     （左對齊）
print('{s:.3}'); hel（截斷為 3 字元）
print('{x:t}'); i64（印出變數 x 的型別名）
print('{s:v}'); 'hello'（字串用單引號包裹）
```

`{{` und `}}` geben ein literales `{` bzw. `}` aus, **aber nur, wenn derselbe String mindestens ein echtes `{name}`-Feld enthält**. Ein Literal, das nur Escape-Sequenzen enthält, wird unverändert ausgegeben: `print('{{n}}')` gibt `{{n}}` aus (nicht `{n}`), `print('{{')` gibt `{{` aus. Für eine einzelne geschweifte Klammer einfach `print('{')` / `print('}')` verwenden.

### 2. Definitionen in derselben Datei

Funktionen, Konstanten und Methoden, die in derselben `.no`-Datei definiert sind, können direkt ohne Präfix verwendet werden.

```no
; 在 sha256.no 中：
sha256(data); sha256 定義在本檔案

; 常量定義在本檔案
HMAC-BLOCK-SIZE
```

### 3. Methoden eingebauter Typen

Methoden eingebauter Typen (`str`, `i64`, `vec`, `arr`, `byte`, `char`, `bool` usw.) benötigen kein Präfix. Die Methoden sind im Typ eingebaut und werden über den Empfängertyp aufgelöst.

```no
'hello'.starts-with('he'); str 方法
n.to-str(); int 方法
v.push(42); vec 方法
a.contains(3); arr 方法
c.is-digit(); char 方法
```

### 4. Methoden von Strukturinstanzen

Der Aufruf von Methoden auf einer bereits erzeugten Strukturinstanz benötigt kein Modulpräfix. Die Methoden werden über den Typ der Instanz aufgelöst; der Compiler findet automatisch die passende Definition `struct.method`.

```no
f = fs.open(path, opts); fs.open 是模組級函數，需前綴
f.read(buf, n); file.read 是結構體方法，不需前綴
f.close(); file.close 是結構體方法，不需前綴

p = path {
    p: '/tmp'
}
p.exists(); path.exists 是結構體方法，不需前綴
```

## Methodenaufruf vs. Modulaufruf

Ob ein Methodenaufruf ein Präfix benötigt, hängt vom **Eigentümer der Methode** ab:

- **Methoden eingebauter Typen** (`str.starts-with`, `i64.to-str` usw.) — kein Präfix nötig
- **Methoden von Strukturinstanzen** (`f.read`, `p.exists` usw.) — kein Präfix nötig
- **Funktionen auf Modulebene** (`fs.open`, `sha256.sha256` usw.) — **Präfix erforderlich**

In `fs.fil()` ist `fs` der ShortName des Moduls und `fil` der Name der Funktion auf Modulebene. Das Präfix `fs.` darf nicht weggelassen werden, weil `fs` hier kein Variablenname, sondern ein Modulpfad ist.

### Zwei Bedeutungen von `Name.Function`

`process.cmd(...)` und `p.start(...)` sehen beide wie `xxx.yyy()` aus, haben aber völlig unterschiedliche Bedeutungen:

| Schreibweise | `xxx` | `yyy` | Bedeutung |
| --- | --- | --- | --- |
| `process.cmd(...)` | Modul-ShortName | Funktion auf Modulebene | `xxx` ist ein Modulpfad, `yyy` eine in diesem Modul definierte eigenständige Funktion |
| `p.start(...)` | Instanzvariable | Strukturmethode | `xxx` ist eine Variable vom Typ `process`, `yyy` eine als `process.start = ...` definierte Methode |

**Unterschied bei der Definition**:
- Funktionen auf Modulebene werden **ohne Präfix** definiert: Innerhalb des Moduls schreibt man direkt `cmd = (program str, ...) { }`
- Strukturmethoden werden **mit `struct.`-Präfix** definiert: `process.start = (program str, ...) { }`

**Unterschied beim Aufruf**:
- Funktionen auf Modulebene werden von außen als `ModuleName.function()` aufgerufen: `process.cmd(...)`
- Strukturmethoden werden über eine Instanz aufgerufen: `p = process.new()` → `p.start(...)`

> **Wichtig**: Auch innerhalb desselben Moduls wird eine Funktion auf Modulebene des eigenen Moduls ohne Präfix aufgerufen (`cmd(...)`), während Strukturmethoden über das implizite `self` oder die Syntax `.method()` aufgerufen werden.

## Modulübergreifende Typreferenzen

Bei der Referenzierung von Typen (Strukturen, Interfaces, Enumerationen usw.), die in **anderen Modulen** definiert sind, ist das Präfix `ShortName.` erforderlich. Das gilt für:

### Struktur implementiert ein Interface

Wenn eine Struktur ein in einem anderen Modul definiertes Interface implementiert, muss der Interface-Name das Modulpräfix tragen:

```no
#{fmt-skip}
; ❌ 錯誤：db、rows、stmt 是 sql 模組定義的介面，不能省略前綴
db-mysql db {
    fd i64
}
```

```no
; ✅ 正確：使用 sql.db、sql.rows、sql.stmt
db-mysql sql.db {
    fd i64
}

rows-mysql sql.rows {
    fd i64
}

stmt-mysql sql.stmt {
    fd i64
}
```

### Funktionsparameter und Rückgabetypen

Modulübergreifende Typen in Funktionssignaturen benötigen ebenfalls ein Präfix:

```no
; ✅ 正確：返回型別使用 sql.result
db-mysql.exec = (sql str) (r sql.result) {
    ...
}
```

### Strukturfeldtypen

```no
; ✅ 正確：欄位型別使用 sql.connection
conn-mysql sql.db {
    handle sql.connection
}
```

### Fälle ohne Präfixbedarf

- **Im selben Modul definierte Typen**: Strukturen, Interfaces und Enumerationen, die in derselben `.no`-Datei definiert sind, werden direkt über ihren Typnamen verwendet
- **Eingebaute Typen**: Eingebaute Typen wie `str`, `i64`, `bool`, `byte` benötigen kein Präfix
- **Eingebaute Interfaces**: Sprachinterne Interfaces wie `enter`, `leave` benötigen kein Präfix

```no
; 同檔案定義的型別，不需前綴
result {
    last-id i64
    affected i64
}

; enter/leave 是內置介面，不需前綴
; result 是同檔案定義的結構體，不需前綴
db {
    enter
    leave
    close() (ok bool)
    exec(sql str) (r result)
}
```

## Vollständiges Beispiel

```no
; 標準庫模組自動載入，無需顯式導入

; ─── 不需前綴 ───

; 全局函數（宣告在 global.no）
s str = with-cap(256)
v []i64 = with-len(100)
print('hello {n}')
s = format('x={x}')

; 同檔案函數
sha256(data)

; 內置類型方法
'hello'.starts-with('he')
n.to-str()
v.push(42)

; 結構體實例方法
f = fs.open(path, opts)
f.read(buf, n)
f.close()

; ─── 需要前綴 ───

; 模組級函數
sha256.sha256(data)
sha256.sha256-hex(data)
fs.open(path, opts)
gzip.gzip-decompress(data)
math.pow(x, y)

; 模組常量
net.NET-BUF-SIZE
math.PI

; 跨模組型別引用（介面實作、參數型別、返回型別、欄位型別）
db-mysql sql.db {
    fd i64
}

r sql.result = d.exec('CREATE TABLE ...')
```
