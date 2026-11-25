\---  
sidebar_position: 2  
\---

# Syntax


## Kommentare

Nolang unterstützt Marker für **einzeilige Kommentare** und für **mehrzeilige (Block-)Kommentare**:

- `;` — einzeiliger Marker (Kommentar bis zum Zeilenende)
- `;; <Inhalt>` — einzeiliger Marker (wenn nach `;;` auf **derselben Zeile** noch Inhalt folgt, Kommentar bis zum Zeilenende, semantisch gleich wie `;`)
- `;;\n` — mehrzeiliger (Block-)Kommentar: Folgt auf `;;` **unmittelbar ein Zeilenumbruch** (nur Leerzeichen erlaubt), wird der mehrzeilige Modus aktiviert und endet bei einem `;;`, dem ebenfalls ein Zeilenumbruch/EOF folgt

```no
; Dies ist ein Kommentar
; Dies ist ebenfalls ein Kommentar, gleiche Semantik
; Dies ist immer noch ein einzeiliger Kommentar (kein Zeilenumbruch nach ;;)
x = 1; Inline-Kommentar, bis zum Zeilenende
x = 2;; Inline-Einzelkommentar, gleiche Semantik

;;
Dies ist ein mehrzeiliger (Block-)Kommentar
Er kann sich über mehrere Zeilen erstrecken
Bis ein alleinstehendes ';;' gefunden wird
;;

y = 3
;;
Nach dem schließenden ;; muss ein Zeilenumbruch oder das Dateiende folgen
Nur dann gilt es als abschließendes Trennzeichen
;;
```

> **Regel zum Start mehrzeiliger Kommentare:** Nach `;;` darf **nur Leerraum** (Leerzeichen/Tabulatoren) bis zum Zeilenumbruch oder EOF stehen, damit der mehrzeilige Modus aktiviert wird. Steht nach `;;` auf derselben Zeile irgendein Nicht-Leerzeichen, gilt es als einzeiliger Kommentar (Kommentar bis zum Zeilenende).
>
> **Regel zum Ende mehrzeiliger Kommentare:** Auch das schließende `;;` muss von einem Zeilenumbruch oder EOF gefolgt werden (dazwischen ist nur Leerraum erlaubt). Ein nicht geschlossener mehrzeiliger Kommentar reicht bis zum Dateiende.

> **Regel: Eine Anweisung pro Zeile, mehrere Anweisungen in einer Zeile sind verboten.**
> Das Semikolon `;` ist ein **Kommentarmarker**, kein Anweisungstrennzeichen; auch ein Komma `,` darf mehrere Anweisungen nicht verbinden. Jede Anweisung innerhalb eines Blocks `{ ... }` muss eine eigene Zeile belegen.
> Diese Regel gilt auch für Codebeispiele in Kommentaren: Auch in Kommentaren sollten `;` oder `,` nicht verwendet werden, um mehrere Anweisungen in eine Zeile zu schreiben, um Leser nicht irrezuführen.

```no
#{fmt-skip}
; ❌ Falsch: `;` zur Trennung mehrerer Anweisungen verwenden — nach `;` ist die ganze Zeile ein Kommentar,
; auch die `}` des Blocks wird mitkommentiert, der Block kann nicht geschlossen werden und nachfolgender Code wird still verworfen
; cond -> { print('X'); return }

; ❌ Falsch: Komma zur Verbindung mehrerer Anweisungen verwenden
; h0 = 1732584193, h1 = 4023233417
; out = from-i64(v), out = from-u64(v)
; debug(msg), info(msg), warn(msg)
```

```no
; ✅ Richtig: Jede Anweisung belegt eine eigene Zeile
cond -> {
    print('X')
    return
}

; ✅ Richtig: Jede Anweisung belegt eine eigene Zeile
; h0 = 1732584193
; h1 = 4023233417
; out = from-i64(v)
; out = from-u64(v)
; debug(msg)
; info(msg)
; warn(msg)
```

> **Warum `;` besonders gefährlich ist:** In `cond -> { print('X'); return }` wird `; return }` vollständig als Kommentar behandelt, sodass nur noch `cond -> { print('X')` als Code übrig bleibt. Der Block wird nie geschlossen, alle Anweisungen nach `}` (häufig der gesamte restliche Funktionskörper) werden verschluckt, und die Fehlerposition kann stark verschoben sein — schwer zu lokalisieren.

## Datentypen

Grundtypen

- byte
- bool ; nur Kleinbuchstaben erlaubt
- char ; Zeichentyp: ein einzelner Unicode-Skalarwert (rune), intern als i32 gespeichert. Ein einzelnes Zeichen wird in doppelte Anführungszeichen eingeschlossen, z. B. "中"
- str ; String-Typ, in einfachen Anführungszeichen 'hello' oder als Rohstring in Backticks `mehrzeiliger Inhalt`
- txt ; Texttyp fester Länge, intern fest 256 Bytes (höchstens 255 Bytes Nutzdaten), erfordert explizite Typannotation, z. B. t txt = 'abc'
- i8
- i16
- i32
- i64 ; Standardzahlentyp, architekturunabhängig
- i128 ; 128-Bit-Ganzzahl mit Vorzeichen
- u8
- u16
- u32
- u64
- u128 ; 128-Bit-Ganzzahl ohne Vorzeichen
- f32
- f64

### Der Typ `char` (Unicode-Codepunkt)

`char` repräsentiert **einen einzelnen Unicode-Skalarwert** (scalar value). Sein interner Speichertyp ist **`i32`**, nicht `i64`:

- **Gültiger Bereich**: `0 ..= 0x10FFFF` (also `0 ..= 1114111`). Der größte Codepunkt benötigt nur 21 Bit, `i32` fasst ihn problemlos.
- **Fehler bei Bereichsüberschreitung**: Liegt ein Literal außerhalb dieses Bereichs, meldet der Compiler (einschließlich `no vet`) **sofort einen Fehler**. Zum Beispiel `c char = 0x110000` ✗, `c char = 1114112` ✗; `c char = 1114111` ✓.
- **Numerische Operationen erlaubt**: `char` **kann direkt an Ganzzahloperationen teilnehmen**, etwa `z = a + 25` oder `u = ch - 32` (wobei `ch` die Iterationsvariable von `for ch <- s` ist) sind **völlig normale Schreibweisen**; gerechnet wird mit dem Codepunktwert. Alle Operationen erfolgen mit **i32-Breite**, das Ergebnis gilt als gewöhnlicher Ganzzahlwert; deshalb geben `+ - * /` bei `char` **nicht standardmäßig `option<int>` zurück** wie die Ganzzahl-Familie und lösen keinen Compilerfehler wegen „unbehandelter Ganzzahlüberlauf“ aus. Zu beachten ist nur der Bereich: Der Codepunkt ist maximal `0x10FFFF`; in Kontexten jenseits der `i32`-/Codepunkt-Semantik (etwa bei Offsets oder akkumulierenden Zählern) empfiehlt sich eine explizite Umwandlung nach `i32`/`i64`, damit die Absicht klarer wird.
- **Beziehung zu `str`**: `s[i]` liefert ein `char`; `char` kann implizit in `str` umgewandelt werden (UTF-8-Kodierung), siehe [Strings — Implizite Konvertierung](str.md).

> **„Interne Typen“ und „Konvertierung beim Aufruf“ sind zwei verschiedene Dinge.** `char` ist selbst ein `i32`. Der **Quellcode-Signatur**parameter der eingebauten Funktion `str_from_cp(cp char) -> ?str` ist `char`; nur beim **Aufruf der externen Runtime** erweitert der Compiler diesen i32-Parameter mit **Null-Extension** (`zext i32 → i64`), um zur IR-Signatur von `@str_from_cp` zu passen — das ist lediglich eine vorübergehende Umwandlung bei der Parameterübergabe und **ändert den Typ von `char` nicht**. Ähnlich wie in C bei `char c='a'; f((long)c);`: `c` ist selbst 1 Byte, wird beim Aufruf aber vorübergehend auf `long` heraufgestuft.

Containertypen

- obj ; Objekt
- map ; Zuordnung (Map)
- arr ; Array fester Länge
- vec ; Array variabler Länge
- slice ; Slice (Ansicht) hat keine eigene Datenstruktur und muss an arr/vec/str/txt haften

- \* ; Zeiger, nur in FFI-Deklarationen (`#{c}`) und in der Standardbibliothek
- usize ; nur für FFI 
- any ; beliebiger Typ, nur in der Standardbibliothek

Fortgeschrittene Typen

- bigint
- err

## Typaliase und Union-Typen

Ein Typalias erzeugt einen neuen Namen für einen bestehenden Typ. Mit der Gleichungs-Syntax `name = type` werden sowohl einzelne Typaliase als auch Union-Typen mehrerer Typen unterstützt.

### Syntax

```no
; Union-Typ: mehrere Typen werden durch | getrennt
int = i8 | i16 | i32 | i64 | i128 | u8 | u16 | u32 | u64 | u128
float = f32 | f64
num = int | float
string = str | txt

; Einfacher Typalias
bytes = []byte
buf = [16]u8
```

> `int`, `float` und `num` sind in `std/number` definiert, `string` in `std/str` — allesamt keine eingebauten Compiler-Schlüsselwörter, sondern Union-Typen, die die Standardbibliothek mit derselben Alias-Syntax geschrieben hat.

### Verkettete Referenzen von Union-Typen

Union-Typen können auf andere Union-Typen verweisen und so eine Hierarchie bilden:

```no
int = i8 | i16 | i32 | i64 | i128 | u8 | u16 | u32 | u64 | u128
float = f32 | f64
num = int | float; num ist die Union aus int und float
string = str | txt; string vereint direkt zwei konkrete String-Typen
```

### Verwendung in Funktionen

Union-Typen können für Funktionsparameter und Rückgabewerte verwendet werden; der Compiler führt automatisch Monomorphisierung durch und erzeugt für jeden Member-Typ eine eigene Funktionsversion:

```no
; Parametertyp ist die Union num
max = (a ..num) (r num) {
    r = a[0]
    n = len(a)
    i <- [1..n): {
        a[i] > r -> r = a[i]
    }
}

; Methoden werden auf dem Union-Typ definiert
num.sign = () (r num) {
    {
        . > 0 -> r = 1

        . < 0 -> r = -1

        -> r = 0
    }
}
```

`string` (= `str | txt`) kann ebenfalls für Parameter und Rückgabewerte verwendet werden:

```no
; Sowohl str- als auch txt-Argumente können übergeben werden, jeweils wird eine konkrete Funktion monomorphisiert
take = (s string) (n i64) {
    n = len(s)
}

a str = 'hello'
b txt = 'hi'
n1 = take(a); take__str
n2 = take(b); take__txt
```

> Bestehende Einschränkungen für Union-Funktionskörper (gleich für `num` und `string`): Das Aufrufen von Member-Methoden auf einem Union-Parameter im Funktionskörper (etwa `s.len()`) wird noch nicht unterstützt; stattdessen sind die globalen Builtins (`len(s)`, Anzahl Bytes) zu verwenden oder die Funktion in eine für den konkreten Typ zu zerlegen. Auch Variablendeklarationen mit Union-Typ (`s string = 'x'`) werden noch nicht unterstützt. Achten Sie außerdem darauf, keine Alias-Methode mit demselben Namen wie eine Member-Methode zu definieren (z. B. `string.len`) — sie würde den Aufruf `s.len()` übernehmen und im Methodenkörper rekursiv zu sich selbst zurückkehren.

### Erkennungsregeln

Die Gleichungs-Syntax wird in folgenden Fällen als Typalias erkannt (nicht als Variablenzuweisung):

- `name = type | type | ...`: Union-Typ (mit `|`)
- `name = []type`: Slice-Typ
- `name = [N]type`: Array-Typ
- `name = ?type`: Optionaler Typ
- `name = known-type`: einfacher Typalias, wobei `known-type` ein eingebauter Typname ist (wie `i64`, `f64`, `bool`, `str` usw.) oder der Name eines zuvor definierten Typaliases

## Variablendeklaration

```no
#{fmt-skip}
; Variablen haben kein Schlüsselwort
; Bei i64, f64, byte, bool, byte und str kann die Typannotation entfallen
i = 1

; f64 enthält einen Punkt
f = 1.0

; byte
b = 0x00

; Bei i8 kann die Typannotation entfallen, wenn Variablenname und Typ übereinstimmen
i8 = 3

; Explizite Annotation ist redundant, wenn sie mit dem inferierten Typ übereinstimmt (no vet meldet tcpoxtfd, no fmt --fix=redundant entfernt sie automatisch)
x str = ''
x = ''; äquivalent, Annotation redundant

; Standardwert 0
; Variablen müssen nicht vorab deklariert werden
u16 u16

; str wird in einfache Anführungszeichen eingeschlossen
name = 'nolang'

; bool: true/false, alles Kleinbuchstaben
flag = true
flag = false

; bool-Kurzform: !! entspricht true, ! entspricht false (eigenständige Literale)
flag = true; true
flag = false; false

; Variablenzuweisung
; Gleiche Namen sind nicht erlaubt; bei gleichem Namen gilt es als Änderung der Variablen
name = 'hello'
name = 'world'

; String-Verkettung
greeting = 'hello, ' - name

; Rohstring (in Backticks, mehrzeilig, keine Escape-Auflösung)
sql = 'SELECT id,name
FROM user
WHERE id > 100'

; Explizite Typannotation
a u64 = 10

; Typinferenz bei Hexadezimalliteralen
; - Dezimale Ganzzahlliterale (wie 771) werden als i64 inferiert (Standard-Ganzzahltyp)
; - Hexadezimalliterale (wie 0x0303) werden als byte (u8) inferiert
; - Überschreitet ein Hex-Wert den byte-Bereich (> 255), muss eine explizite Typannotation hinzugefügt werden:
PORT i64 = 0x0303; richtig: explizites i64, Wert = 771

; PORT = 0x0303          ; falsch: als byte inferiert, Wert wird abgeschnitten!
; PORT = 771             ; richtig: dezimal standardmäßig i64
; - Bei Hexadezimalliteralen sollten Kleinbuchstaben verwendet werden (0x00ff statt 0x00FF)
; - Empfehlung: Allgemeine Ganzzahlkonstanten dezimal schreiben; Protokoll-/Bitmaskenkonstanten als Hex mit expliziter i64-Typannotation

; Zeichen (doppelte Anführungszeichen = char/rune, einzelnes Zeichen)
c = "中"

; Typ byte
b = 0x00

; arr: Array fester Länge
arr [3] = [1, 2, 3]

; vec: dynamisches Array (Slice)
vec = [4, 5, 6]

; Expliziter Typ (Slice)
typed []u8 = [1, 2, 3]

; Array
typed [3]u16 = [1, 2, 3]

; Länge wird automatisch inferiert (i64)
a[nil] = [1, 2, 3]
```
```

## Rohstrings (Raw String)

Ein Rohstring wird mit einem Paar Backticks `` ` `` ` ` `` ` `` deklariert und hat den Typ `str`.

### Syntax

```
sql = `
SELECT id,name
FROM user
WHERE id > 100
`
```

### Zwingende Formatvorgaben

1. Nach dem **öffnenden** `` ` `` muss **unmittelbar ein Quellcode-Zeilenumbruch folgen** (LF; CRLF in Windows-Dateien wird ebenfalls akzeptiert);
2. Der **schließende** `` ` `` muss **allein auf einer Zeile stehen**, wobei diese Zeile nur Leerraum + den schließenden Backtick enthalten darf;
3. Die Zeilen mit dem öffnenden und schließenden Backtick **zählen nicht zum String-Inhalt**, sie dienen nur als Trennzeichen;
4. **Escape-Regel**: Alle internen `\`, `\n`, `\t`, `\'`, `\"` werden **unverändert beibehalten**, es findet keine Escape-Auflösung statt;
5. **Zeilenumbrüche und Einrückungsleerzeichen aus dem Quellcode werden vollständig** in die String-Bytes **übernommen**; eine Ausnahme ist CR (`\r`) — es wird **immer verworfen**, sodass CRLF- und LF-Dateien bytegleiche Werte ergeben;
6. Ein Backtick-Zeichen lässt sich **nicht direkt einbetten**; wenn `` ` `` enthalten sein soll, muss eine gewöhnliche einfache Zeichenkette verkettet werden.

### Beispiel

```
; Inhalt ist "SELECT id,name\nFROM user\nWHERE id > 100\n"
; Escape-Sequenzen \n \t bleiben unverändert, werden nicht interpretiert
raw = `
line1\nline2
\ttabbed
`
```

## Regex-Literale (Regex Literal)

Nolang unterstützt die JavaScript-artige Regex-Literal-Syntax `/pattern/flags` zum Erzeugen einer kompilierten `regexp`-Instanz.

### Syntax

```no
; Einfaches Regex-Literal
re = /\d+/

; Mit Flags
re = /hello/gi

; Zeichenklassen, Anker, Quantoren
re = /[a-z]+/
re = /^hello.*world$/

; Schrägstrich escapen
re = /a\/b/
```

### Flags

| Flag | Bedeutung        |
| --- | --------- |
| `g` | Globale Suche      |
| `i` | Groß-/Kleinschreibung ignorieren    |
| `m` | Mehrzeilenmodus      |
| `s` | `.` trifft auch auf Zeilenumbrüche zu |

Flags sind optional, folgen dem schließenden `/` und bestehen aus ASCII-Buchstaben.

### Kontextsensitive lexikalische Analyse

`/` ist sowohl Divisionsoperator als auch Einleitungszeichen für Regex-Literale; Nolang verwendet wie JavaScript eine **kontextsensitive** lexikalische Analyse zur Unterscheidung:

- **Ausdrucksposition** (Anfang einer Anweisung oder nach `=` / `(` / `[` / `{` / `,` / `:` / `;` usw.) → `/` leitet ein Regex-Literal ein
- **Werterzeugungsposition** (nach Bezeichnern, Literalen, `)` / `]` / `}` usw.) → `/` ist der Divisionsoperator
- `//` ist immer ein Zeilenkommentar (höchste Priorität)

```no
; Regex-Literal (nach `=` beginnt ein Ausdruck)
re = /\d+/
result = match-text(/[a-z]+/, text)

; Divisionsoperator (nach einem Bezeichner folgt eine Werterzeugungsposition)
ratio = 100 / 4
x = a / b
```

### Desugaring

Regex-Literale werden in der Codegenerierungsphase in einen Aufruf der Standardbibliotheksfunktion `regexp-compile` desugart:

```no
; Quellcode
re = /\d+/

; Nach dem Desugaring äquivalent zu
re = regexp-compile('\\d+')
```

`regexp-compile` ist in `std/regexp.no` definiert, erzeugt die Struktur `regexp` und ruft `.compile()` auf, um das Muster zu kompilieren.

### Anwendungsbeispiel

```no
; Regex erzeugen und zuordnen
re = /\d+/
matched = re.matches('hello 123 world')
print(matched); true

; Treffer suchen
re = /[a-z]+/g
result = re.find('hello 42 world')
print(result); "hello"

; Als Funktionsargument
result = match-text(/\d+/, text)
```

> **Hinweis:** Ein leeres Muster `//` kollidiert mit einem Zeilenkommentar (wie in JavaScript). Für einen leeren Treffer verwenden Sie `/(?:)/`.

## Namenskonventionen

Variablen-, Funktions- und Strukturnamen dürfen mit einem Unterstrich beginnen; danach können Bindestriche, Buchstaben und Ziffern folgen. Sie dürfen nicht mit einer Ziffer beginnen, nicht mit einem Bindestrich enden und keine aufeinanderfolgenden Bindestriche enthalten.

**Regeln zur Groß-/Kleinschreibung (zwingend):**

- **Globale Konstanten und globale Variablen**: **müssen** mit einem Großbuchstaben beginnen (z. B. `NOLANG`, `MAX-SIZE`, `HEX-CHARS`). Private globale Variablen beginnen mit einem Unterstrich, danach muss ebenfalls ein Großbuchstabe folgen (z. B. `_NOLANG`, `_PRIVATE-CONST`).
- **Lokale Variablen und Funktionsparameter**: Kleinbuchstaben verwenden (z. B. `hex-chars`, `data-len`). **Kein** `_`-Präfix — lokale Variablen sind bereits im Geltungsbereich privat und benötigen keine Sichtbarkeitsmarkierung. Das `_`-Präfix ist nur für private globale Variablen und private FFI-Deklarationen reserviert.
- **Funktions- und Strukturnamen**: Kleinbuchstaben verwenden (z. B. `sha1-block`, `db-mysql`)

**Funktionsnamenskonventionen (dringend empfohlen):**

- **Keinen Modulnamen als Präfix vor den Funktionsnamen setzen**. Funktionen innerhalb eines Moduls benötigen nur kurze, aussagekräftige Namen; das Modulpräfix wird beim modulübergreifenden Aufruf automatisch über `ShortName.` ergänzt. Beispielsweise definiert man in `tail.no` die Einstiegsfunktion `tail` statt `tail-run` und die Hilfsfunktion `atoi` statt `tail-atoi`. So bleibt der Code knapper und ein Aufruf wie `tail.tail()` ist anschaulicher.
- Bei der **Einstiegsfunktion** empfiehlt sich der Modulname selbst (z. B. `ping.no` → `ping`, `cat.no` → `cat`). Ein modulübergreifender Import sieht aus wie `# /src/tail.tail`.
- **Schlüsselwörter vermeiden**: `awy` (Schlüsselwort für Coroutine-Gruppen), `match` (Schlüsselwort für bedingte Zuordnung) und ähnliche dürfen nicht als Funktionsnamen verwendet werden. `run` wurde entfernt und kann als gewöhnlicher Bezeichner dienen.
- **`main` ist ein reservierter Name und darf nicht als Funktionsname verwendet werden**: Nolangs Programmeinstieg wird **automatisch aus Top-Level-Anweisungen synthetisiert** (siehe `hir2mir.synthesizeMainForTopLevel`), daher ist eine explizite Definition von `main` weder nötig noch **erlaubt**. Sobald `main = (...) { }` auftritt — einschließlich der Methode `recv.main` oder eines in anderen Funktionen verschachtelten `main` — meldet der Compiler direkt einen Fehler (`'main' is a reserved name and cannot be used as a function name`). Um als Einstieg zu dienen, genügt es, die Anweisungen direkt auf Top-Level zu schreiben.

> **Globale Variablen müssen mit einem Großbuchstaben beginnen** — das ist eine zwingende Regel, keine Konvention. Top-Level-Variablen mit Kleinbuchstaben werden vom Compiler als lokale Variablen behandelt, was zu Fehlern wie undefinierten Referenzen führen kann.

```no
; ✅ Richtig: Globale Daten verwenden Großbuchstaben
NOLANG = 'nolang'
MAX-SIZE = 1024
HEX-CHARS = '0123456789abcdef'

; ✅ Private globale Variablen: beginnen mit Unterstrich, danach muss ebenfalls ein Großbuchstabe folgen
_NOLANG = 'nolang'
_PRIVATE-CONST = 42
```

```no
#{fmt-skip}
; ❌ Falsch: Globale Variablen dürfen nicht mit einem Kleinbuchstaben beginnen
; x1 = 10
; x = 10
; foo-bar = 42
; hello-world = 'Hello World'
```
```

```no
#{fmt-skip}
; ✅ Lokale Variablen (innerhalb von Funktionen) verwenden Kleinbuchstaben, ohne `_`-Präfix
; fn-example = () {
;     x1 = 10
;     x = 10
;     foo-bar = 42
;     hello-world = 'Hello World'
;     ; ❌ Falsch: Lokale Variablen benötigen kein `_`-Präfix
;     ; _x = 10
; }
```
```

### Globale Variablen in Modulen vermeiden

**Dringende Empfehlung: Verwenden Sie globale Variablen in Modulen (`.no`-Dateien) nur im Notfall.** Globale Variablen bringen folgende Probleme mit sich:

- **Risiko eines Compiler-Bugs**: Nolangs Compiler hat eine bekannte Einschränkung bei der Behandlung von Speicheradressen globaler Strukturvariablen über Funktionsgrenzen hinweg; verschiedene Funktionen können unterschiedliche Adressen sehen, was zu inkonsistentem Zustand führt.
- **Nebenläufigkeitssicherheit**: Globaler veränderlicher Zustand ist bei Fork- oder Async-Szenarien schwer nachvollziehbar und begünstigt Race Conditions.
- **Testbarkeit**: Globaler Zustand erzeugt implizite Abhängigkeiten in Funktionen und erschwert isolierte Tests.
- **Lesbarkeit**: Globale Variablen machen den Datenfluss undurchsichtig; Leser müssen das gesamte Modul verfolgen, um das Verhalten einer Funktion zu verstehen.

**Empfohlene Vorgehensweise:**

1. **Lokale Variablen bevorzugen**: Zustand in lokalen Variablen innerhalb der Funktion halten und über Parameter und Rückgabewerte weitergeben.
2. **Zustand in Strukturen kapseln**: Zusammengehörigen Zustand in einer Struktur organisieren und über Methoden bearbeiten (der Methodenempfänger ist eine lokale Variable mit konsistenter Adresse).
3. **Globale Variablen nur bei Bedarf verwenden**: Etwa bei modulweiten Konstanten (unveränderlich) oder Singleton-Ressourcen (z. B. einem globalen Log-Puffer).
4. **Globale Variablen müssen großgeschrieben werden**: Das ist eine zwingende Regel (siehe oben); Top-Level-Variablen mit Kleinbuchstaben werden vom Compiler als lokale Variablen behandelt.

```no
#{fmt-skip}
; ❌ Vermeiden: Veränderliche globale Variablen in Modulen verwenden
; g-conn = tls.conn {}
; g-buf = ' '
;
; fn-a = () {
;     g-conn.send(g-buf)   ; Adressen globaler Variablen können über Funktionsgrenzen hinweg abweichen
; }
```
```

```no
; ✅ Empfohlen: Lokale Variablen verwenden, Zustand über Parameter/Rückgabewerte weitergeben
fn-a = () {
    conn = tls.conn {}; lokale Variable, Adresse konsistent
    buf = ' '
    conn.send(buf)
}

; ✅ Empfohlen: Den gesamten Ablauf in einer einzigen Funktion kapseln, um Zustandsübergaben zwischen Funktionen zu vermeiden
serve-once = (listen-fd fd, body str) (ok bool) {
    ok = false
    client-fd = net.net-accept(listen-fd)
    conn = tls.server-init(client-fd); lokale Variable
    conn: {
        ok -> {
            c = it
            c.handshake()
            c.send(body)
            c.close()
            ok = true
        }

        -> fs.close(client-fd)
    }
}
```

> **Praxisbeispiel**: Die Funktion `https-serve-once` in `std/net/tls.no` kapselt den vollständigen HTTPS-Anfrage-/Antwort-Zyklus (accept + handshake + recv + send + close) in einer einzigen Funktion; sämtlicher TLS-Zustand bleibt in lokalen Variablen, wodurch der Compiler-Bug zu globalen Variablen mit abweichenden Adressen über Funktionsgrenzen hinweg erfolgreich umgangen wird.522|||## API-Dokumentationsrichtlinien

## API-Dokumentationsrichtlinien

Die Dokumentationskommentare einer Funktion sollten vollständige Parameternamen und -typen sowie den Namen und Typ des Rückgabeparameters enthalten.

**Regeln:**

- Der Dokumentationskommentar über einer Funktionsdefinition muss Name und Typ jedes Parameters sowie Name und Typ des Rückgabeparameters auflisten
- Auch die API-Zusammenfassung am Modulanfang sollte die vollständige Signatur verwenden (mit Parameternamen, Typen, Rückgabename und -typ) und keine Kurzschreibweise

```no
#{fmt-skip}
; ❌ Falsch: Typen fehlen, Name des Rückgabeparameters fehlt
; sha1(data) (hash)
; sha1-block(s, h0..h4)
```
```

```no
; ✅ Richtig: Parametername, Typ, Name des Rückgabeparameters und Typ enthalten
; sha1(data []byte) (hash [20]byte) — vollständiger Hash
; sha1-block(s []u32, h0 u32, h1 u32, h2 u32, h3 u32, h4 u32) — verarbeitet einen einzelnen Block

; Auch Dokumentationskommentare über Funktionsdefinitionen sollten derselben Vorgabe folgen:
; sha1: berechnet den SHA-1-Hash
; data []byte: Eingabe-Byte-Array
; Rückgabe hash [20]byte: 20-Byte-Hashwert
sha1 = (data []byte) (hash [20]byte) {
    ...
}
```

### Übersetzbare Dokumentationskommentare (Markdown-Links)

Ein Dokumentationskommentar kann mit einem **eigenständigen Markdown-Link** abgeschlossen werden, der den gesamten Kommentar auf einen Eintrag im Übersetzungskatalog verweist:

```no
; get: ruft das i-te Argument ab
; Delegiert an das Builtin os.arg(i) (liest argv[i] von main); bei Bereichsüberschreitung wird nil zurückgegeben
; [get](#args.get)
get = (i i64) (val ?str) {
    val = os.arg(i)
}
```

**Regeln:**

- Der Link muss die **letzte Zeile** der Kommentargruppe sein und allein auf einer Zeile stehen; alle Zeilen darüber bilden eine Einheit und werden als eine Nachricht vollständig übersetzt
- Das Ziel muss ein mit `#` beginnender Anker sein (ein Eintrag im selben Katalog). Externe Links wie `[Erklärung](https://example.com)` sind gewöhnliche Links für menschliche Leser und werden **nicht** als Übersetzungsmarkierung behandelt
- Der Anker ist der Schlüssel (`args.get`). Er wird vom Autor festgelegt und vom Übersetzer im Katalog gesehen, nicht aus dem Text abgeleitet — daher entwertet eine Umformulierung keine bestehende Übersetzung. Anker erlauben Buchstaben, Ziffern, `.`, `_` und `-`, sind global und sollten ein Modulpräfix enthalten
- Als Präfix dient der **geflachte Modulname**, nicht der Verzeichnisname. Der Compiler flacht std-Dateien auf ihren Dateinamen: Aus `std/collection/linked-hash-map.no` wird der Modul `linked-hash-map`, und `use std/collection` ergänzt Symbolen kein `collection.`-Präfix. Der Anker für `linked-hash-map.clear` lautet daher `linked-hash-map.linked-hash-map.clear` (Modulname + unveränderter Name), **nicht** `collection.linked-hash-map.linked-hash-map.clear`. Die Verzeichnissegmente `collection`／`net`／`crypto`／`archive`／`encoding`／`database` gehören zu keinem Symbol und dürfen nicht im Anker auftreten
- Der Text in eckigen Klammern (`[get]`) ist nur ein Hinweis für Leser des Quellcodes; bei der Anzeige wird die gesamte Zeile entfernt; `[](#args.get)` ist ebenfalls zulässig
- Nur `;`-Einzelkommentare dürfen einen Link tragen (`;;` und `;;block` zählen nicht)

Übersetzungen liegen in `locales/<Sprache>-doc.noml`, mit dem Anker als Schlüssel. Fehlt eine Übersetzung, wird auf den ursprünglichen Kommentar zurückgegriffen, es wird nicht leer. Dateinamen folgen der `.no`-Namensregel (Kleinbuchstaben, Ziffern, einzelne Bindestriche); das Tag selbst hat genau diese Form: `--lang=zh-tw` liest `zh-tw-doc.noml`.

Jede Funktion und Methode mit einem Kommentar in der Standardbibliothek trägt bereits einen Anker (etwa 2000 Einträge), sodass jede Übersetzung sofort vom LSP genutzt werden kann, sobald sie vorhanden ist. `make i18n` schreibt nur **übersetzte** Einträge in den Katalog und fügt den Originaltext nicht als Übersetzung ein — sonst stünde Chinesisch in einem englischen Katalog. `go run ./i18n/cmd/genen -doc-missing` listet Anker auf, die noch nicht übersetzt sind.

**LSP-Hinweis:** Beim Hover über den Funktionsnamen wird die vollständige übersetzte Beschreibung angezeigt; beim Hover über die Zeile mit dem Link erscheint der Inhalt, auf den der Anker verweist (ebenfalls in der aktuellen Sprache), was eine direkte Prüfung der Übersetzung erleichtert.

## Standardbibliothek bevorzugen

Nolangs Standardbibliothek bietet zahlreiche häufig benötigte Funktionen, darunter String-Operationen, Byte-Konvertierung, Hash-Berechnung und Netzwerkkommunikation.

**Regel: Ist eine Funktion bereits in der Standardbibliothek vorhanden, wird von einer eigenen Neuimplementierung abgeraten.** Entwickler sollten die Dokumentation der Standardbibliothek (`docs/docs/std/overview.md`) sorgfältig lesen, um das Rad nicht neu zu erfinden.

```no
#{fmt-skip}
; ❌ Falsch: Konvertierung str → []byte selbst implementieren
str-to-bytes = (s str) (out []byte) {
    n = s.len-bytes()
    i = 0
    (i < n) {
        out[i] = s[i]
        i = i + 1
    }
}
```
```

```no
; ✅ Richtig: Die Standardbibliotheksmethode str.to-bytes() verwenden
data []byte = s.to-bytes()
```

Häufige Alternativen aus der Standardbibliothek:

- `str.to-bytes()` — String in Byte-Array umwandeln (statt eines selbst geschriebenen `str-to-bytes`)
- `[]byte.to-str()` — Byte-Array in String umwandeln (statt eines selbst geschriebenen `bytes-to-str`)
- `[n]t.to-vec()` — Array fester Länge in Slice umwandeln (`[20]byte` → `[]byte`)
- `[]byte.to-hex()` / `[]byte.to-hex-lower()` — Byte-Array in Hexadezimal-String umwandeln
- `str.to-i64()` / `str.to-f64()` — String in eine Zahl umwandeln
- `int.to-str()` / `float.to-str()` — Zahl in String umwandeln
- `std/crypto/sha1`, `std/crypto/sha256`, `std/crypto/sha512` — Hash-Berechnung

### TOML-Konfigurationsdateien (`toml`)

Die Standardbibliothek `toml` bietet Parsing und Serialisierung von TOML-1.0-Konfigurationsdateien. Der Parser unterstützt Kommentare, bare/quoted/dotted Keys, `[table]`, `[[array-of-tables]]`, Basic-/Literal-/Multiline-Strings, Ganzzahlen (dezimal/hexadezimal/oktal/binär), Gleitkommazahlen, Bool-Werte, Datum/Uhrzeit, Arrays und Inline-Tables. Werte werden als rohe TOML-Literale gespeichert, sodass keine Informationen über Arrays, Datum/Uhrzeit usw. verloren gehen.

```no
cfg = toml.parse('title = "Nolang"\n[server]\nport = 8080\n[[server.backends]]\nname = "local"')

doc = toml.new()
doc.put('name', '"nolang"')
doc.put('server.port', '8080')
doc.put('enabled', 'true')
text = toml.stringify(doc)
```

Häufige API:

- `toml.new()` — ein leeres `toml` erzeugen
- `toml.parse(text)` — TOML parsen, liefert `?toml`
- `doc.put(key, raw-value)` — einen rohen TOML-Wert hinzufügen oder aktualisieren; ungültige Werte werden abgelehnt, doppelte Keys werden vom Parser gemeldet
- `toml.get(doc, key)` — den rohen Wert abrufen (`?str`)
- `toml.get-str(doc, key)`, `toml.get-i64(doc, key)`, `toml.get-f64(doc, key)`, `toml.get-bool(doc, key)` — einen konvertierten Skalar abrufen
- `toml.stringify(doc)` — TOML ausgeben; Array-of-Tables erzeugt `[[...]]`-Header

Das aktuelle Dokumentmodell beschränkt ein Dokument auf höchstens 4 Keys, 2 gewöhnliche Tabellen und 2 Array-of-Tables; ein Key ist maximal 32 Bytes, ein Wert maximal 128 Bytes lang. Bei Überschreitung sollte die Konfigurationsdatei aufgeteilt oder vom Aufrufer verarbeitet werden.

### YAML-Dokumente (`yaml`)

Die Standardbibliothek `yaml` parst und erzeugt Dokumente des YAML-1.2-Core-Schemas und unterstützt Block-Mappings, Block-Sequenzen, Flow-Sammlungen, drei Arten quotierter Skalare, Block-Skalare `|` / `>`, Kommentare, mehrere Dokumente, Anker und Aliase, Merge-Keys `<<`, explizite Keys sowie Typinferenz (null / bool / int / float / `.inf` / `.nan`).

```no
doc ?yaml = yaml.parse('name: Alice\nserver:\n  port: 8080\ntags:\n  - a\n  - b\n')
doc: {
    nil -> print('nil')

    err -> print(it.msg())

    -> {
        doc = it
        name = doc.find-str('name'); ?str
        port = doc.find-i64('server.port'); ?i64
        name: {
            ok -> print(it)

            -> print('')
        }
        print(doc.to-json())
    }
}
```

Häufige API:

- `yaml.parse(text)` / `yaml.parse-all(text)` — parsen, liefert `?yaml`
- `yaml.valid-of(text)` / `yaml.error-of(text)` — nur Validierung
- `it.find(path)` / `it.get(key)` — einen Unterknoten abrufen, liefert `?yaml`
- `it.find-str(path)`, `find-i64`, `find-f64`, `find-bool` — komfortable Wertabfrage, liefert `?Wert`
- `it.to-json()` / `it.to-yaml()` — Serialisierung

> Hinweis: Unterknoten werden als `?yaml` zurückgegeben, Hilfsfunktionen zur Wertabfrage als `?Wert`; mit match auspacken (`child = it.find(path)`, wobei der `ok`-Arm den Unterknoten an `it` bindet und der Default-Arm den leeren Wert behandelt, jeder Arm auf einer eigenen Zeile).

## Dateibenennung

`.no`-Dateinamen (einschließlich Ordnernamen) verwenden grundsätzlich den Bindestrich `-` zur Wortverbindung, **keinen Unterstrich `_`**. Das entspricht dem Namensstil anderer Nolang-Bezeichner wie Variablen-, Funktions- und Strukturnamen.

Der Compiler **erzwingt** diese Regel bei den Befehlen `run` / `build` / `test` / `vet` / `fmt` / `ast`; verstößt irgendein Pfadsegment (Ordnername oder Dateiname ohne `.no`-Erweiterung) dagegen, wird **direkt ein Fehler gemeldet**:

- Erlaubte Zeichen: Kleinbuchstaben `a-z`, Ziffern `0-9`, Bindestrich `-`.
- Verbotene Zeichen: Unterstrich `_`, Großbuchstaben und sonstige Sonderzeichen.
- Ein Bindestrich darf weder am Anfang noch am Ende eines Segments stehen und nicht doppelt auftreten (`--`).

Beispielsweise wird `tmp/zz_stridx/r_index.no` wegen der Unterstriche in `zz_stridx` und `r_index` abgelehnt; `tmp/zz-stridx/r-index.no` ist dagegen konform. Bei absoluten OS-Pfaden (wie `/Users/me/proj/a.no`) wird nur der innerhalb des aktuellen Arbeitsverzeichnisses liegende Teil geprüft; Host-Verzeichnisnamen wie `Users` lösen keinen Fehler aus.

```shell
✅ Empfohlen:
utils/
├── string-helper.no
├── hash-table.no
└── http-client.no
```

```shell
❌ Vermeiden:
utils/
├── string_helper.no
├── hash_table.no
└── http_client.no
```

## Funktionsdefinitionen

Funktionen können Ergebnisse über **benannte Ergebnisparameter** (named result parameters) ausgeben; dies ist im Wesentlichen  
**Syntaxzucker für Ausgabeparameter (out-parameter)**; `return` (und seine Kurzform `***`, derzeit nicht empfohlen) dient nur zum vorzeitigen Beenden und darf kein Ergebnis mitführen.

Nolangs Funktionsdefinition hat die Form `name = (in-params) (out-params) { body }`: die zweite  
Klammerngruppe `(out-params)` deklariert benannte Ergebnisparameter (Syntaxzucker). Im Funktionskörper wird ihnen zugewiesen  
(wie bei gewöhnlichen Eingangsparametern handelt es sich um Referenztypen); **die Variable, die das Ergebnis tatsächlich aufnimmt, muss am Aufrufer definiert werden** —  
die Ergebnisparameternamen in der Definition sind nur Platzhalter; die LHS des Aufrufers (oder der nachgestellte Parameter) bestimmt, in welcher  
Variable das Ergebnis landet:

```no
parse-line = (s str, max-fields ?i64) (fields []str) {
    ...; im Funktionskörper wird fields zugewiesen
}

; Am Aufrufer wird die Empfängervariable definiert (LHS-Bindung, Typ aus der Signatur inferiert)
fields = parse-line(line, nil)

; Mehrere Ergebnisse werden der Reihe nach gebunden
a, b = swap(x, y)

; Oder als nachgestellte Argumente: res wird als zusätzliches Ausgabeargument übergeben
add1(5, 3, res)
```

Nolangs Funktionen haben folgende Eigenschaften:

- **Benannte Ergebnisparameter sind Syntaxzucker**: Intern erfolgt die Übergabe weiter über Eingangsparameter (out-parameter), es entsteht  
  kein neues Rückgabeobjekt, daher ist es intern sicher.
- **Eingangsparameter sind schreibgeschützt**: Skalare werden nach Wert übergeben, zusammengesetzte Typen als schreibgeschützte Referenz; das Beschreiben von Eingangsparametern und ihren Unterfeldern im Funktionskörper ist verboten.
- **Ausgabeparameter sind beschreibbar**: Der Aufrufer kann eine vorhandene Variable an einen Ausgabeslot binden, die Funktion ändert direkt diesen Speicher; er kann ihn auch ungebunden lassen, sodass die Funktion einen neuen Wert erzeugt und übergibt.
- Variablen innerhalb einer Funktion werden beim Verlassen der Funktion automatisch zerstört.
- Am Aufrufer muss eine Empfängervariable angegeben werden (LHS oder nachgestelltes Argument); die Aussage „die Funktion hat keinen Rückgabewert“ ist ungenau —  
  genauer: Das Ergebnis wird über benannte Ergebnisparameter (Syntaxzucker für out-parameter) übermittelt und vom Aufrufer gebunden.
- **Alias-Regel**: Eine schreibgeschützte Eingangsreferenz und ein Ausgabeslot dürfen auf dasselbe Objekt zeigen. Solange Schreibzugriffe nur im Ausgabebereich und Lesezugriffe nur im Eingabebereich erfolgen, ist es zulässig; der Compiler führt keine statische Aliasprüfung durch.

### Ausgabeslot direkt an vorhandene Variable binden (Zwischenvariable vermeiden)

Der Ausgabeslot auf Aufruferseite kann **direkt an eine bereits vorhandene Variable gebunden werden**; die Funktion überschreibt deren Speicher an Ort und Stelle (siehe oben „Ausgabeparameter sind beschreibbar“).
Wenn ein Ausgangsergebnis lediglich zur weiteren Aktualisierung eines bestehenden Zustands dient, muss es daher nicht zuerst in eine einmalige Zwischenvariable aufgenommen und dann zurückgeschrieben werden:

```no
; ✅ Empfohlen: Rest wird direkt nach r geschrieben (r ist zugleich Empfänger der Methode)
qsq, r = r.div-mod(modulus)
```

```no
; ❌ Vermeiden: msq wird nur einmal verwendet, dient reinem Zwischenspeichern
qsq, msq = r.div-mod(modulus)
r = msq
```

- **Der Empfänger kann zugleich das Ausgabesziel sein**: Der Empfänger `r` von `r.div-mod(modulus)` ist eine schreibgeschützte Eingabe (implizites `self`),
  und `r` ist zugleich an den zweiten Ausgabeslot `r bigint` gebunden; gemäß der Alias-Regel ist nur-Lesen der Eingabe und nur-Schreiben der Ausgabe zulässig.
  Messungen (`no build` / `no vet` / `no fmt`) zeigen, dass `q, a = a.div-mod(bigint.from-i64(7))` nach
  `a = bigint.from-i64(100)` ergibt `q = 14` und `a = 2`, identisch mit „zuerst `msq` aufnehmen, dann `r = msq`“;
  auch dann, wenn `r` ein Ergebnisparameter einer äußeren Funktion ist (`mod-pow` in `src/std/bigint.no` hat genau diese Form).
- **Nicht benötigte Ausgaben mit `_` verwerfen**: Wird der Quotient gar nicht gebraucht, muss bei `_, r = r.div-mod(modulus)` nicht einmal eine neue Variable eingeführt werden;
  das ist die knappste Schreibweise (siehe später `_`-Platzhalter unter 〈Mehrfachzuweisung〉).
- **Gilt auch für einzelne Ausgaben**: `r = r._mul(r)`, `s = s.trim()` und `out = out.replace-str(...)` sind bereits vorhandene Aktualisierungen an Ort und Stelle in der Stdlib.
- **Bereits gültiger Code muss deswegen nicht umgeschrieben werden**: Dies ist eine Empfehlung für neuen Code, keine Anweisung zum Refactoring; gemäß der 〈Goldenen Regel: Gültigen Code nicht ändern〉,
  sollte man korrekte Programme nicht aus stilistischen Gründen antasten.

### Standardwerte für Parameter

**Nicht unterstützt.** `name type = expr` in einer Parameterliste (z. B. `foo = (a i64, b ?i64 = 10)`) ist ein **Syntaxfehler** (`E_DEFAULT_PARAM`): Der Compiler parst keine Standardausdrücke und füllt an keiner Aufrufstelle Standardwerte ein. Damit entfällt die Mehrdeutigkeit „eine Deklaration, mehrere gültige Aritys“ und die Monomorphisierung durch wiederholtes Absenken von Standardausdrücken an jeder Aufrufstelle.

Wenn optionale Semantik benötigt wird, deklarieren Sie den Parameter als Optional-Typ `?T`; der Aufrufer übergibt **explizit** `nil` / `ok(...)`, und der Funktionskörper verarbeitet ihn mit `match`:

```no
parse-line = (s str, max-fields ?i64) (fields []str) {
    limit = 1024
    max-fields: {
        ok(v) -> limit = v

        nil ->

        err ->
    }
    ...; limit ist die Obergrenze
}

; Der Aufrufer muss explizit übergeben
fields = csv.parse-line(line, nil); Rückfall 1024
fields = csv.parse-line(line, ok(256)); Obergrenze 256
```

```no
add = (a i64, b i64) (result i64) {
    result = a + b; Ergebnis über den Parameter zurückgeben
    ...; Platzhalteranweisung: tut nichts, wird beim Build vollständig entfernt (siehe 〈Platzhalteranweisung〉)
}

; Variable Argumente
add3 = (a ..i64) {
}

; Funktionsaufruf
sum = add(1, 2); sum == 3

; Anonyme Funktion und for? Mit Parametern?
(a i64) {
    print(a)
}(10)

; Funktionsaufruf
add(a, b)

; Auch mehrere Rückgabewerte möglich
a, b = swap(5, 3)
```

## Ablaufsteuerung

> **Veraltete Syntax (Entfernung nach n Versionen)**: `for { }` / `for cond { }` / `for init, cond, update { }` / `for i in [..) { }` / `match x { }` / `if/elif/else { }` lassen sich noch parsen, erzeugen aber eine Deprecation-Warnung. Bitte verwenden Sie die nachstehende „**neue Syntax**“.

### Alte und neue Syntax im Vergleich

| Alte / Suffix-Form           | Prefix-Form (Standard bei format)                   |
| -------------------------- | --------------------------------------- |
| `{ } (true)` Endlosschleife          | `!! { }` oder `true { }`                   |
| `{ } (cond)` Bedingungsschleife          | `(cond) { }` (leeres `()` gilt als false, wird nicht ausgeführt)       |
| `{ } ()` wird nicht ausgeführt               | `! { }` oder `false { }` oder `() { }`        |
| `{ } * n` konstante Zählung             | `n * { }` (bei N ≤ 0 wird der Schleifenkörper nicht ausgeführt)                |
| `for i=0, i<n, i++ { }` Zählschleife | `n * { }` (konstante Zählung) oder `i <- [0..n): { }` (Variable) |
| `for i <- [a..b] { }` Bereich   | `i <- [a..b]: { }`                      |
| `for i in [a..b) { }` Bereich   | `i <- [a..b): { }`                      |
| `match x { ... }` Zuordnung       | `x: { ... }`                            |
| `if/elif/else { }` Verzweigung      | `{ cond -> body }` (Kurzschluss, muss in `{}` eingeschlossen sein)      |
| `continue`                 | `*` (Kurzform derzeit nicht empfohlen)                        |
| `break`                    | `**` (Kurzform derzeit nicht empfohlen)                       |
| `return`                   | `***` (Kurzform derzeit nicht empfohlen)                      |

### Loop / While / for-in

> **Zwei gleichwertige Schreibweisen**: Dieselbe Schleife unterstützt sowohl die „Prefix-Form `(cond) { }`“ als auch die „Suffix-Form `{ } (cond)`“;  
> die Semantik ist identisch, nur die Reihenfolge von Bedingung und Körper unterscheidet sich. `no fmt` gibt standardmäßig die **Prefix-Form** aus;  
> mit `no fmt -loop-style=suffix` kann auf die Suffix-Form umgestellt werden (bei beiden Formen ist Formatierungs-Idempotenz garantiert).

```no
#{fmt-skip}
; === Prefix-Form (Bedingung vor dem Körper) ===

; Endlosschleife (Bedingung stets true) — die folgenden zwei sind gleichwertig
!! {
    ...
}
!! {
    ...
}

; Wird nicht ausgeführt (Bedingung stets false) — die folgenden drei sind gleichwertig
! {
    ...
}
! {
    ...
}
! {
    ...
}

; Bedingungsschleife (cond prüfen, Körper ausführen, wenn wahr)
(x == 1) {
    do-something()
}

; Anzahl der Durchläufe festlegen (Prefix-Zählung)
10 * {
    do-something()
}

; Bei N ≤ 0 wird der Schleifenkörper nicht ausgeführt (0 oder negativ wird übersprungen)
0 * {
    print('wird nicht ausgeführt')
}
-3 * {
    print('wird ebenfalls nicht ausgeführt')
}

; Ein Label kann hinzugefügt werden (Label vor Bedingung/Zählung)
#1 (x == 1) {
    do-something()
}

; === Suffix-Form (Bedingung nach dem Körper, alte Form) ===
!! {
    ...
}

; Wird nicht ausgeführt (leere Klammern bedeuten false)
! {
    ...
}

; Bedingungsschleife905|||; Anzahl der Durchläufe festlegen
(x == 1) {
    do-something()
}

; Anzahl der Ausführungen begrenzen
10 * {
}

; Bei N ≤ 0 wird der Schleifenkörper nicht ausgeführt (0 oder negativ wird übersprungen)
0 * {
    print('wird nicht ausgeführt')
}

-3 * {
    print('wird ebenfalls nicht ausgeführt')
}

; Bereichssyntax (unterstützt numerische Typen wie i64/u8 sowie arr, vec, str)
; Insgesamt 16 Kombinationen: '[' schließt den linken Endpunkt ein, '(' schließt ihn aus; ']' schließt den rechten Endpunkt ein, ')' schließt ihn aus.
; Begrenzt (4 Varianten): an beiden Enden steht ein Wert
i <- [a..b]: {
}
i <- (a..b]: {
}
i <- [a..b): {
}
i <- (a..b): {
}

; Ohne Obergrenze (4 Varianten): rechter Endpunkt entfällt, Iteration bis zum Maximalwert des Typs
i <- [a..]: {
}
i <- [a..): {
}
i <- (a..]: {
}
i <- (a..): {
}

; Ohne Untergrenze (4 Varianten): linker Endpunkt entfällt, Iteration ab dem Minimalwert des Typs
i <- [..b]: {
}
i <- [..b): {
}
i <- (..b]: {
}
i <- (..b): {
}

; Völlig unbegrenzt (4 Varianten): beide Endpunkte entfallen, basierend auf Minimal-/Maximalwert des Typs
i <- [..]: {
}
i <- [..): {
}
i <- (..]: {
}
i <- (..): {
}
i <- [5..0]: {
}
i <- abc: {
}

; Laufzeit-Richtungserkennung: Wenn start > end, iteriert die Schleife automatisch abwärts (Schritt -1).
; Alle vier Klammerkombinationen unterstützen absteigende Iteration:
;   [5..1]  → 5 4 3 2 1   links geschlossen, rechts geschlossen, absteigend
;   (5..1]  → 4 3 2 1     links offen, rechts geschlossen, absteigend
;   [5..1)  → 5 4 3 2     links geschlossen, rechts offen, absteigend
;   (5..1)  → 4 3 2       links offen, rechts offen, absteigend
;   (3..0]  → 2 1 0       links offen, rechts geschlossen, absteigend bis 0
; Wenn start <= end, wird aufwärts iteriert (Schritt +1).
;
; Die Endpunkte unbegrenzter Bereiche werden durch den statischen Typ der Variablen bestimmt:
;   [..] für i64: von i64.MIN (-9223372036854775808) bis i64.MAX (9223372036854775807)
;   [..] für u8: von 0 bis 255
;   [..) für i64: von i64.MIN bis i64.MAX - 1 (Maximum ausgeschlossen)
;   (..] für i64: von i64.MIN + 1 bis i64.MAX (Minimum ausgeschlossen)
;   (..) für i64: von i64.MIN + 1 bis i64.MAX - 1 (beide Endpunkte ausgeschlossen)
; Regel: '[' schließt den Minimalwert des Typs ein, '(' beginnt bei Minimum + 1;
;       ']' schließt den Maximalwert des Typs ein, ')' endet bei Maximum - 1.

; ❌ Ausdrücklich abgelehnt
;   Bereichsgrenzen müssen Ganzzahlen sein; verschachtelte Ausdrücke werden nicht unterstützt
;   for i <- [1.5..5.5] { }   ; Kompilierungsfehler
;   for i <- [0..[1..5][0]] { } ; Syntaxfehler

; ⚠️ Mehrdeutigkeit bei ... vermeiden
;   Der Bereichsoperator ist .. (zwei Punkte), ein self-Methodenaufruf ist .len().
;   In direkter Folge wird [0.. .len()) zu [0...len()), und drei Punkte sehen wie ein einzelner Operator aus
;   (die Kurzform für return ist bereits als *** festgelegt, derzeit nicht empfohlen; die drei Punkte erschweren aber weiterhin das Lesen).
;   Zur Disambiguierung self.len() statt .len() verwenden: i <- [0..self.len()): { }
;   (innerhalb des Methodenkörpers sind self und . semantisch gleichwertig)

; Bedingungsschleife (neue Form: Prefix `(cond) { }`, Standard bei `no fmt`; ersetzt die alte Form `for cond { }`)
; Die Suffix-Form `{ } (cond)` ist dazu völlig gleichwertig (alte Schreibweise)
; In den meisten Fällen kann stattdessen ein range-for verwendet werden: i <- [0..n): { }
(x == 1) {
    do-something()
}
```
```

### Ausbruch / Überspringen / vorzeitige Rückkehr

> ⚠️ **Die Kurzformen `*` (aktuelle Iteration überspringen), `**` (Schleife verlassen) und `***` (Rückkehr/Beendigung) werden derzeit nicht empfohlen** — bitte schreiben Sie
> die englischen Schlüsselwörter `continue` / `break` / `return`; `no fmt` normalisiert bereits unterstützte Kurzformen grundsätzlich zurück zur Schlüsselwortform.

```no
i <- [0..10): {

    ; continue (aktuelle Iteration überspringen, derzeit nicht empfohlen)
    continue

    ; break (Schleife verlassen, derzeit nicht empfohlen)
    break

    ; return/terminate (vorzeitige Rückkehr, beendet nur die Funktion; geplant, derzeit nicht empfohlen)
    break
    continue
}

; Die englischen Schlüsselwörter werden empfohlen (konsistent mit C/Rust, leichter portierbar und kompilieren/laufen einwandfrei):
i <- [0..10): {

    ; äquivalent zu * (derzeit nicht empfohlen)
    continue

    ; äquivalent zu ** (derzeit nicht empfohlen)
    break

    ; äquivalent zu *** (derzeit nicht empfohlen), beendet nur die Funktion vorzeitig
    return
}

; Schlüsselwort-Aliase (vom Lexer vollständig unterstützt, mit den ursprünglichen Schlüsselwörtern völlig gleichwertig):
;   ctn ≡ continue, brk ≡ break, rtn ≡ return; `no fmt` behält die Alias-Schreibweise bei
i <- [0..10): {

    ; äquivalent zu continue
    ctn

    ; äquivalent zu break
    brk
}

; äquivalent zu return, beendet nur die Funktion vorzeitig
rtn
```

> ⚠️ **`return` (und seine Kurzform `***`, die derzeit nicht empfohlen wird) darf nur ohne Wert verwendet werden, nicht mit einem Rückgabewert.**  
> Nolang-Funktionen haben keinen „Rückgabewert“-Mechanismus — Ergebnisse werden ausschließlich über **benannte Ergebnisparameter (out-param)** im Funktionskörper zugewiesen und nach außen gegeben (siehe „Funktionsdefinitionen“).  
> Daher sind `return <Wert>`, `return(expr)` und `*** <Wert>` **verboten**; Compiler / Formatter / LSP melden alle einen Fehler:
>
> - Compiler (`no build`): `Error: compilation error: parser errors: line L, column C: auf 'return' darf kein Rückgabewert folgen; … [E_GENERAL]`
> - `no vet`: `<file>:L:C: [ERROR] nolang-compile: auf 'return' darf kein Rückgabewert folgen; … [E_GENERAL]` (gleiches Format wie andere Diagnosen, eine pro Zeile)
> - `no fmt`: `format error: …` und Beendigung mit nicht-null-Status
> - LSP: zeigt eine rote Fehlerdiagnose in der entsprechenden Zeile an
>
> Falsche Schreibweise:
>
> ```no
> has = (n i64) (r i64) {
>     n == 0 -> return 0        ; ❌ Compiler / Formatter / LSP melden alle einen Fehler
>     r = n
> }
> ```
>
> Richtige Schreibweise (zuerst dem Ergebnisparameter einen Wert zuweisen, dann mit bloßem `return` vorzeitig beenden; mehrere Anweisungen im Armkörper müssen jeweils eine eigene Zeile belegen):
>
> ```no
> has = (n i64) (r i64) {
>     n == 0 -> {
>         r = 0
>         return
>     }
>     r = n
> }
> ```
>
> ⚠️ Das Zusammendrücken in eine Zeile wie `n == 0 -> { r = 0; return }` ist keine Kurzform: `; return }` ist vollständig ein Kommentar,
> der Block wird daher nie geschlossen und alle folgenden Anweisungen (häufig der gesamte Rest des Funktionskörpers) werden still in diesen Arm hineingenommen —
> In der Praxis meldet `no vet` keinen Fehler und die Funktion wird nie aufgerufen.

## Kanäle (`chan[T]`)

Ein Kanal ist eine **begrenzte, wachsende Werteschlange, bei der Senden nicht blockiert und Warten empfängt**; die Empfängerseite entnimmt Werte einzeln als dreiwertiges `?T`.
Er behält nur ein syntaxisches Symbol: `<-` (weitere Verwendungen siehe die `i <- range:`-Iteration unter „Ablaufsteuerung“).

```no
ch chan[i64] = with-cap(8)   ; explizite Anfangskapazität
buf chan[i64]                 ; bloße Deklaration: entspricht with-cap(0), wächst bei Bedarf beim Senden

ch <- 1                       ; Senden (nur wenn der Typ links chan[T] ist, zählt es als Senden)
n = ch.len()                  ; aktuelle Anzahl ausstehender Werte
c = ch.cap()                  ; Anfangskapazität
ch.close()                    ; Schließen: keine Sendungen mehr akzeptiert, vorhandene Werte können noch entnommen werden
```

### Empfang: dreiwertige Schleife

`v <- ch: { ... }` ist eine **Schleife**, die in jeder Runde den dreiwertigen Wert `?T` an `v` bindet; `break` kann vorzeitig beenden.
`ok` bedeutet, ein Wert wurde entnommen; `nil` bedeutet, der Kanal ist geschlossen und leer; `err` bedeutet einen **echten Fehler** (ein nicht initialisierter
Nullwert-Handle — es gibt nichts, worauf man warten könnte) — **die letzten beiden beenden die Schleife** (die Arme derselben Runde werden wie gewohnt ausgeführt).

Neben `ok` ist die wichtigste Regel: **Ein leerer Lesevorgang bedeutet „auf einen Wert warten“, nicht „kein Wert“**. Ist der Kanal geöffnet, aber leer, hält der Leser
an, bis jemand `send` oder `close` aufruft:

- In einer Coroutine (dem `-async`-Klon aus `co`) parkt er tatsächlich: Die Zustandsnummer wird in einen Frame auf dem Heap geschrieben, die Aufgabe wird von der
  Warteliste des Kanals registriert und dann an den Scheduler zurückgegeben (siehe Abschnitt „Coroutinen“);
- In einer gewöhnlichen Funktion ist Parken nicht möglich, daher **blockiert** sie: Die Bereitschaftswarteschlange wird wiederholt abgearbeitet, bis ein Wert verfügbar ist.

Daher muss „Leer werden“ die Schleife über `close()` beenden, entsprechend Goes `range` über einen Channel.

```no
ch chan[i64] = with-cap(2)
ch <- 10
ch.close()

<- ch: {
    ok -> print('got {it}')      ; it ist das Element dieser Runde
    nil -> print('closed')       ; nach dem Leerwerden landet man hier
    err -> print('bad channel')  ; nur ein Nullwert-Handle führt hierher
}
```

Wird die Schleifenvariable verworfen, schreibt man `<- ch: { ... }`; ist die erste Anweisung im Body kein match über `v`,
dann bindet `v` direkt **das Element selbst** (`T` statt `?T`), und der Body läuft nur in den `ok`-Runden:

```no
i <- ch: {
    print(i)                     ; i ist i64, nicht ?i64
}
```

Um die drei Zustände selbst zu unterscheiden und dabei die Schleifenvariable zu behalten, genügt ein match zu Beginn des Body; nur Arme zu schreiben (`v:` weglassent)
ist eine synonyme Kurzform — das Kriterium liegt in den **Armnamen**: nur wenn die drei Namen `ok` / `nil` / `err` auftreten, liegt ein dreiwertiger Fall vor:

```no
i <- ch: {
    ok -> print('got {it}')      ; it ist das Element dieser Runde, i das ?i64 dieser Runde
    nil -> print('closed')
    err -> print('empty')
}
```

Das Kriterium ist syntaktisch, nicht typbasiert (siehe unten „Formbestimmung“). `no fmt` ergänzt die obigen Kurzformen zum
expliziten Subjekt `i: { ... }`; beide Schreibweisen lassen sich wieder parsen, die Formatierung ist idempotent.

### Formbestimmung

 | Beginn des Body | Form | Typ von `v` |
|---|---|---|
 | Gewöhnliche Anweisungen wie `co bar(i)`, `print(i)` | Element | `T` |
 | `v: { ... }` | Dreiwertig | `?T` |
 | `ok -> ...` / `nil -> ...` / `err -> ...` | Dreiwertig (Kurzform) | `?T` |
 | Sonstige nackte matches (z. B. `i > 5 -> ...`) | Element | `T` |

Die Bestimmung muss **vor dem Parsen des Body** erfolgen: Beginnt der Block mit einem match, desugart der Parser ihn sofort zu einer
`if`-Kette, und die ursprüngliche Bedeutung lässt sich danach nicht mehr erkennen. Diese Zelle fiel einmal vollständig aus — `i <- ch:` wurde immer als Elementform behandelt,
die dreiwertigen Arme degenerierten zu unabhängigen Guards (in jeder Runde alle ausgeführt), und `it` wurde an einen leeren Wert gebunden.

### Semantik

 | Aspekt | Regel |
|---|---|
 | Kapazität | Das `n` in `with-cap(n)` ist die **Anfangs**kapazität; bei Überlauf verdoppelt sie sich, `send` **blockiert nie und verliert keine Werte** |
 | Leerer Lesevorgang | **Warten**: Coroutine parkt, gewöhnliche Funktion blockiert und bearbeitet die Bereitschaftswarteschlange; `err` bleibt nur für echte Fehler wie einen Nullwert-Handle reserviert |
 | Schließen | Nach `close()` beendet `send` den Prozess; bereits eingereihte Werte können weiterhin in Reihenfolge entnommen werden, danach wird `nil` gemeldet; das Schließen weckt zugleich alle Wartenden |
 | Nutzdaten | Beim Senden erfolgt eine **tiefe Kopie** (`str` / `%vec` werden geklont), der Empfänger erhält eine unabhängige Kopie; man muss sich keine Sorge um Überschreiben oder vorzeitiges Freigeben der Quellvariablen machen |
 | Nebenläufigkeit | Der Kanal selbst verwendet keine Sperre (einzelne Thread, kooperatives Scheduling): Wartende hängen an der `waiters`-Liste des Kontrollblocks; `send` / `close` nehmen die gesamte Liste ab und reihen sie einzeln wieder ein. **Aufgeweckt zu werden bedeutet nicht, einen Wert zu erhalten**; nach dem Fortsetzen muss die Lesbarkeitsprüfung erneut erfolgen, daher ist der Wiederaufnahmepunkt einer Coroutine der park-Block selbst |
 | Deadlock | Ist die Bereitschaftswarteschlange leer und die ZIElaufgabe noch nicht abgeschlossen ⇒ kein Fortschritt möglich: Die Laufzeit gibt `nolang: deadlock: …` auf stderr aus und bricht ab, statt einen nie gesendeten Wert zurückzugeben oder im Leerlauf zu drehen |

> Eine direkte Folge des Anhaltens bei leerem Lesen: **Die Entleerungsschleife muss über `close()` beendet werden**. Frühere Versionen meldeten „geöffnet, aber leer“
> als `err`, sodass die Schleife natürlich endete und die `for`-Form den Kanal nicht schließen musste; heute ist dieser Pfad ein Warten,
> und ein Programm, das `close()` vergisst, bleibt in der letzten Runde stehen (Coroutine hält an, gewöhnliche Funktion wird als Deadlock erkannt).

### Einschränkungen

- **Nur im LLVM-Backend implementiert.** `no build --js` / `--wasm` melden bei jedem Kanal-Knoten einen klaren Fehler
  (`[chbkn001]`) statt still unvollständigen Code zu erzeugen.
- Ein Kanal ist ein **Werttyp** (ein Handle von einem Maschinenwort Breite) und kann als Parameter, Ergebnis oder Element übergeben werden;
  `chan[chan[T]]` und `?chan[T]` lassen sich ebenfalls deklarieren (noch nicht in der Abdeckung von `tests/chan.no` enthalten).

## Platzhalteranweisung (`...`)

`...` ist eine **Platzhalteranweisung** der Sprache: syntaktisch gültig, semantisch **ohne Wirkung**. Sie dient vor allem der Dokumentationsdemonstration,
Code-Gerüsten und `TODO`-Platzhaltern. Beim Kompilieren wird die gesamte Anweisung bereits in der Phase AST→HIR verworfen; das Ergebnis ist identisch mit „diese Zeile nie geschrieben zu haben“
(Messung: Das `sha256` des aus derselben Quelle mit oder ohne `...` kompilierten Binärs ist identisch).

```no
parse-line = (s str) (fields []str) {

    ; TODO: Hier müssen noch Escape-Zeichen verarbeitet werden
    ...

    fields = csv.parse-line(s)
}

add = (a i64, b i64) (result i64) {
    result = a + b
    ...
}
```

Regeln:

- **Darf nur eine eigene Zeile belegen** (am Zeilenende darf ein `;`-Kommentar folgen). Schreibweisen wie `fail -> ...`, `x = ...`, `[0...n)`
   ergeben stets einen Syntaxfehler — `...` hat keinen Wert und darf weder an einer Expression-Position noch im restlichen Teil einer Anweisung stehen.
- **Kein Alias für `return`** und **kein Bereichsoperator**: Bereiche schreibt man mit `..` (zwei Punkte), vorzeitige Rückgaben mit `return`.
- `no fmt` behält sie unverändert bei, entfernt sie nicht und verschiebt sie nicht.

Folgende Schreibweisen sind allesamt unzulässig:

```no
#{fmt-skip}
add = (a i64, b i64) (result i64) {
    result = a + b
    ...          ; ✅ einzige korrekte Form: eine eigene Zeile
}

ok -> ...        ; ❌ Syntaxfehler: `...` darf nicht an einer Expression-Position stehen
x = ...          ; ❌ Syntaxfehler: wie oben
i <- [0...n): {  ; ❌ Syntaxfehler: `...` ist kein Bereichsoperator, schreibe [0..self.n)
}
```

### Match

```no
; einfache Form, it dient zum Abrufen des Parameters
x: {
    err -> log(it.msg()); it ist der strukturierte Fehler err.err

    nil -> log('nil')

    -> do-right-thing(it)
}

; Form mit Destrukturierung
x: {
    err(e) -> log(e.msg())

    nil -> log('nil')

    ok(v) -> do-right-thing(v)
}

; Hinweis: struct-Musterabgleich (`user: { User{id=1} -> ... }`) und Feldbindung werden noch nicht unterstützt, daher kein Beispiel

score: {
    [0..59] -> print('不及格')

    [60..89] -> print('良好')

    [90..100] -> print('優秀')

    -> print('分數非法')
}

; die Bereichssyntax hat 16 Kombinationen, Regel: '[' bedeutet linke Grenze einschließen, '(' bedeutet ausschließen;
;                     ']' bedeutet rechte Grenze einschließen, ')' bedeutet ausschließen.
;
; 【Bereich mit beiden Grenzen】(4 Arten) — beide Grenzen haben konkrete Werte
;   [a..b]  → x >= a && x <= b   beide Grenzen eingeschlossen
;   [a..b)  → x >= a && x <  b   linke eingeschlossen, rechte nicht
;   (a..b]  → x >  a && x <= b   linke nicht eingeschlossen, rechte eingeschlossen
;   (a..b)  → x >  a && x <  b   beide Grenzen nicht eingeschlossen
;
; 【Bereich ohne obere Grenze】(4 Arten) — rechte Grenze weggelassen (End=nil), nur die untere Grenze wird geprüft
;   [a..)   → x >= a               linke eingeschlossen, keine obere Grenze
;   [a..]   → x >= a               linke eingeschlossen, keine obere Grenze
;   (a..)   → x >  a               linke nicht eingeschlossen, keine obere Grenze
;   (a..]   → x >  a               linke nicht eingeschlossen, keine obere Grenze
;
; 【Bereich ohne untere Grenze】(4 Arten) — linke Grenze weggelassen (Start=nil), nur die obere Grenze wird geprüft
;   [..b]   → x <= b               keine untere Grenze, rechte eingeschlossen
;   [..b)   → x <  b               keine untere Grenze, rechte nicht eingeschlossen
;   (..b]   → x <= b               keine untere Grenze, rechte eingeschlossen
;   (..b)   → x <  b               keine untere Grenze, rechte nicht eingeschlossen
;
; 【Vollständig unbegrenzter Bereich】(4 Arten) — beide Grenzen weggelassen, basierend auf Minimal-/Maximalwert des Typs
;   [..]    → true                Typ-Minimum ≤ x ≤ Typ-Maximum (beide Grenzen eingeschlossen)
;   [..)    → true                Typ-Minimum ≤ x < Typ-Maximum
;   (..]    → true                Typ-Minimum < x ≤ Typ-Maximum
;   (..)    → true                Typ-Minimum < x < Typ-Maximum
;
; In match basiert ein vollständig unbegrenzter Bereich auf dem Wertebereich des Typs der gematchten Variable (immer wahr, entspricht catch-all).
; In range-for basiert er auf dem Wertebereich des Typs der Iterationsvariablen (z. B. [..] für i64 von i64.MIN bis i64.MAX).
;
; Beispiel: vollständige Klassifikation mit unbegrenzten Bereichen
v = 85
result = v: {
    [..0) -> 'negative'

    [0..60) -> 'fail'

    [60..80) -> 'pass'

    [80..90) -> 'good'

    [90..100] -> 'excellent'

    (100..) -> 'extraordinary'
}
print(result)

num: {
    1 || 3 || 5 || 7 -> print('奇數小數')

    2 || 4 || 6 -> print('偶數小數')

    -> print('更大數字')
}

; Rückgabewert vorhanden — die letzte Anweisung/der letzte Wert
result = x: {
    1 -> 1

    2 -> 2 + 1

    -> a + b
}

; ein mehrzeiliger arm-Body muss geschweifte Klammern verwenden -> { ... }
x: {
    nil -> {
        log('nil')
        do-cleanup()
        return
    }

    err -> {
        log(it.msg())
        do-cleanup()
        return
    }

    ok -> print(it)
}
```

> **Regel für mehrzeilige arm-Bodies**: Wenn ein arm-Body mehrere Anweisungen enthält, muss er in geschweifte Klammern `-> { ... }` eingeschlossen werden. Ein einzeiliger Body kann direkt nach `->` stehen. Der Grund: Ohne geschweifte Klammern kann die `it`-Bindung des option match nicht korrekt eingefügt werden, was zu einem Kompilierungsfehler führt.

> **Semantik von match innerhalb von for-in**: `i <- (a..b]: { 1 -> ... 2 -> ... }` führt den match-Body einmal pro Iterationsvariable `i` aus (`1 ->` entspricht `i == 1 ->` usw.). Dies ist syntaktischer Zucker für einen match pro Iteration.

#### Die drei Formen des match-Subjekts

Das match-Subjekt kann ein **Wert**, ein **Ausdruck** oder ein **Block** sein:

| Form | Schreibweise | Auswertungsart |
| --- | --- | --- |
| Wert | `x: { ... }` | direktes Matching von `x` |
| Ausdruck | `x + 1: { ... }` | zuerst den Wert des Ausdrucks berechnen, dann matchen |
| Block | `{ ... }: { ... }` | führt einen Anweisungsblock aus und matcht das Ergebnis des `?=` darin |

Die ersten beiden sind **Ausdrücke**: Sie können sowohl als Anweisung als auch zur Wertermittlung in einem Wertkontext verwendet werden (`v = x: { ok -> it, -> 0 }`).
Die dritte ist **nur eine Anweisung**: kein Ausdruck, nicht zuweisbar, kein Block-Endwert; `-> _` dient zum Verwerfen des Ergebnisses.
`no fmt` gibt `}: {` unverändert zurück; die Regel für Leerzeilen zwischen Armen ist dieselbe wie bei einem normalen match.

##### Ausdruckssubjekt (`expr: { ... }`)

Bei `expr: { ... }` hat `: {` eine **höhere** Priorität als `=`: auf der rechten Seite einer Zuweisung ist es die gesamte RHS, nicht
„zuerst `r = <expr>` ausführen, danach folgt ein separates match“. Daher lässt sich Option-Arithmetik in einem Schritt auspacken:

```no
#{overflow=wrap}
rem = seconds % 3600: {
    ok -> it

    -> 0
}

#{overflow=wrap}
sec = rem % 60: {
    ok -> it

    -> 0
}
```

Dies entspricht dem früher dreiteiligen Schema `rem-opt = seconds % 3600` / `rem = 0` /
`rem = rem-opt: { ok -> it  -> 0 }`. Die erste Zuweisung kann einen expliziten Typ tragen
(`rem i64 = seconds % 3600: { ... }`), die zweite schreibt man einfach `rem = ...: { ... }`;
beide sind einzelne Wert-Position-matches, der Ergebnistyp wird durch den Arm-Endausdruck bestimmt.

Semantik und Einschränkungen:

- Ist das Subjekt eine D6-Option-Arithmetik (`+ - * / %`), ist das Ergebnis `?T`. Der match normalisiert es in
  ein synthetisches `__match_subj_L_C ?T`-Temporär: das Subjekt wird **genau einmal ausgewertet**, `ok -> it` bindet das Payload,
  die Bedingung prüft das option-Tag, und `-> 0` fängt `nil` / `err` ab. Der synthetische Name erscheint weder in Diagnosen noch in
  `no fmt`-Ausgaben.
- Das Ergebnis des match ist **plain T** und wird in die semantische Nebentabelle eingetragen; daher kann man weiterhin
  `sec = rem % 60: { ... }` schreiben, aufeinanderfolgende Wert-Position-matches verlieren das Payload nicht.
- Wert-Position-match wird nur im **Wertkontext** angehängt (rechts einer Zuweisung / `?=` / Verbundzuweisung), und `: { ... }`
  muss wirklich ein match-Block sein. Anweisungsebene-Bedingungsschleifen wie `cond: { body }` und struct-Literale
  laufen weiter über den bestehenden Parsing-Pfad und sind nicht betroffen.
- **Auch Zuweisungen auf Feldziele sind Wertkontext** (seit 2026-11-09): Bei `d.month = mp - 9: { ... }` und
  `b.n ?= b.n + 1: { ... }` bindet `: {` an den **rechten Wert**, nicht an die gesamte Zuweisung. Zuvor hatten diese beiden
  Pfade beim Parsen des rechten Werts `CTX_EXPR` nicht abgeleitet (Identifikator-Ziele über `let` taten es, `obj.f =` / `a[i] =`
  nicht), sodass `: {` von der Anweisungsebene aufgenommen und um die gesamte Zuweisung gelegt wurde, geparst als
  `match(Subjekt = (d.month = mp - 9))`: Die Zuweisung wurde wie gehabt ausgeführt und das `?T` implizit ausgepackt in das Feld
  geschrieben, während die beiden Arme `ok -> it` / `-> 0` toter Code waren. Jetzt landen die Arm-Werte tatsächlich wieder im Feld.
- Indexziele (`a[i] = …: { ... }`) haben dieselbe Form, aber D9 verbietet ohnehin die direkte Indexzuweisung; diese Schreibweise
  wird vom checker weiterhin mit `idxwrite` abgelehnt und muss in place-match (`a[i]: { ok -> it = v }`)
  oder `?=` umgeschrieben werden.
- `no fmt` gibt `expr: { ... }` unverändert zurück und zerlegt es nicht in Zuweisung plus separaten match.
- **Typ-Sichtbarkeitsgrenze**: Damit ein Option-Arithmetik-Subjekt auf `?T` normalisiert werden kann, müssen die deklarierten Typen
  beider Operanden vom parser statisch sichtbar sein. Stammen die Operanden aus einer tiefen Methodenkette (der Receivertyp lässt
  sich selbst per AST-Replay nicht ableiten), ergänze eine explizite Typangabe bei der Deklaration, z. B. `e i64 = flr.to-i64()`; sonst wird
  die Normalisierung konservativ übersprungen, `ok -> it` bleibt ungebunden, MIR füllt mit Nullen und der beobachtete Wert ist stets 0. In
  `src/std/number.no` ist `f64.to-str-general` genau so: Ohne diese Annotation degradiert `fmt.fmt-f64(2.5, '')` von
  `'2.5'` zu `'3'`. Der Regressionstest ist festgenagelt bei
  `src/parser/value_match_expr_subject_test.go::TestValueMatchExprSubjectASTAnnotatedOperand`
  (ohne Annotation wird es rot).

`src/std` hat am 2026-11-09 **122 Stellen in 22 Modulen** der Option-Unpackung auf diese einteilige Schreibweise umgestellt:
neben der ursprünglichen dreiteiligen Form (`x-opt = expr` / `x = 0` / `x = x-opt: { ... }`) umfasst dies auch die zweiteilige
(ohne Nullinitialisierung), die explizite Typangabe bei der ersten Zuweisung (`x i64 = expr: { ... }`) sowie Muster, bei denen eine
Zwischenvariable innerhalb derselben Funktion mehrfach paarweise deklariert wird. Nach dem Falten kann das Subjekt ein Methodenaufruf,
eine Indexierung oder eine Option-Arithmetik sein; `ok -> it`／`ok(v) -> v`／Konstanten-Arme／`-> _` sind alle empirisch äquivalent, und `no fmt` ist auf diesen Stellen idempotent.
Die Typinferenz des Compilers kümmert sich auch um Fälle wie „im semantischen Nebeneintrag steht `?T`, die Deklaration ist aber eigentlich `i64`“ — globale Residuen mit gleichem Namen —,
`ok -> it` verliert dadurch nicht die Bindung.

Beibehaltung (9 Stellen) in diesen Fällen: ① Der innere match schreibt in die `-opt`-Variable selbst zurück (sie ist das Subjekt des nachfolgenden Wert-Position-match); ② zweistufig verkettete Option-matches;
③ wertbasierte Dispatch-Wert-Position-matches (z. B. errno → Nachricht, kein Option-Unpacking); ④ zwischen Deklaration und match stehen weitere Anweisungen oder die Zwischenvariable ist ein über Zweige hinweg akkumuliertes Ergebnis
option-Unpacking); ④ zwischen Deklaration und match stehen weitere Anweisungen oder die Zwischenvariable ist ein über Zweige hinweg akkumuliertes Ergebnis
(das Falten würde den Zeitpunkt der Auswertung verändern).

##### Blocksubjekt (`}: {`)

Ein Blocksubjekt umschließt eine Folge von Anweisungen, damit auch **Funktionen ohne `?T`-Ergebnisparameter** das Hochwerfen von `?=` vor Ort behandeln können:

```no
; d i64: Divisor, bei 0 ergibt 6/d einen Fehler (err)
f = (d i64) {
    {
        v ?= 6 / d; Fehlschlag erfordert nicht, dass die Funktion einen ?T-Ergebnisparameter besitzt
        print(v.to-str()); wird bei Fehlschlag nicht ausgeführt
    }: {
        err -> print(it.msg())

        nil -> print('nil')

        -> _; Ergebnis verwerfen
    }
    print('之後照常執行'); nach Ende der Arme läuft der Ablauf weiter
}
```

Semantik:

- Das `?=` im Körper wird vom **nächstgelegenen** Block-match abgefangen (bei Verschachtelung gilt der innerste; auch wenn die umgebende Funktion einen
  `?T`-Ergebnisparameter hat, hat der Block-match Vorrang). Das Abfangen ist rekursiv: nackte Blöcke, if-Körper, Schleifenkörper und
  `?=` in match-Armen zählen alle; Funktionsdefinitionen sind eine Grenze, Funktionsliterale nicht.
- Gematcht wird der tri-state `?T`, mit der **Erschöpfheitsprüfung** des standardmäßigen option match: Ohne Auffang-Arm müssen selbst dann die drei Arme `nil` / `err` / `ok` vervollständigt werden,
  wenn `ok ->` zur Laufzeit unerreichbar ist.
- Läuft der Körper fehlerfrei durch (kein `?=` oder alle erfolgreich) ⇒ `nil` wird gematcht; scheitert ein `?=` ⇒ das betreffende
  `nil` oder `err` wird gematcht (das `it` des `err`-Arms ist das vollständige `err.err`, siehe〈Der Typ von `it`〉).
- **Ein Fehlschlag stoppt den gesamten Körper**: Nachfolgende Anweisungen und weitere Schleifeniterationen werden nicht ausgeführt, die Arme laufen nur einmal.
  Es ist ein „Stoppblock“, kein Funktions-`return`; die eigenen `return` / `break` / `continue` des Benutzers bleiben unberührt.
  Bindung bleibt unberührt.
- Die Expansion besteht vollständig aus vorhandenen Core-AST-Komponenten (synthetische `?T`-Lokale + `if`-Wächterkette + normaler match),
  es gibt keine neuen HIR-/MIR-Knoten. Synthetische Variablennamen erscheinen nicht in Diagnosen oder `no fmt`-Ausgaben.

> Arbeitsteilung mit dem lokalen Abfangen durch `=`: `=` speichert den Fehler **in der Zielvariable**, der Ablauf läuft weiter; `}: {` fasst den Fehler
> **zu einer einzigen match zusammen** und stoppt den Rest des Körpers. Wenn „weiterlaufen nach Fehlschlag“ gewünscht ist, `=` +
> anschließender match; wenn „bei Fehlschlag direkt zum Aufräum-Arm“ gewünscht ist, `}: {`.

##### Abwägung: Block-match vs. Wert-Position-match

Dieselbe Sache — das Ergebnis einer Option-Arithmetik in eine plain-Variable zurückzuholen — lässt sich auf zwei Arten schreiben. **Wenn der Blockkörper nur diese eine Anweisung enthält, sind beide äquivalent;
sobald der Körper weitere Anweisungen enthält oder der rechte Wert anderswoher scheitern kann, weichen die Semantiken auseinander.**

```no
; A: Block-match + `?=` (blockweises „try“)
{
    #{overflow=wrap}
    np ?= np + 1
}: {
    err -> _

    -> _
}

; B: match auf Wertposition bindet das Ziel direkt (Anweisungsebene)
#{overflow=wrap}
np = np + 1: {
    ok -> it

    -> _
}
```

Unter `#{overflow=wrap}` ist das Ergebnis von `np + 1` **stets `ok` (der zurückgewickelte Wert)**; beide erzeugen nur ein nacktes
`add i64` ohne Überlaufprüfungszweig, und der Fehler-Arm ist für „die Addition selbst“ toter Code — das ist der Grund, warum sie an den meisten Stellen
„vollständig äquivalent“ wirken. Der eigentliche Unterschied liegt in **Gültigkeitsbereich** und **Fehlerpfad**:

| Dimension | A: Block-match + `?=` | B: Wert-Position-match |
| --- | --- | --- |
| Syntaxposition | **Anweisung**: kein Endwert, nicht zuweisbar | **Ausdruck**: darf rechts einer Zuweisung, als Feldziel, rechts von `?=` stehen |
| Schreiben ins Ziel bei Fehler | **Nullwert** (`i64` → `0`, `str` → `''`) | vom Auffang-Arm bestimmt: `-> _` **schreibt nicht** (behält alten Wert), `-> 0` schreibt 0, `-> sum` schreibt alten Wert zurück |
| Restliche Anweisungen im Körper | **alle übersprungen** (inkl. nachfolgender Schleifeniterationen) | laufen normal weiter |
| Ein `err`-Arm umfasst | **alle** `?=` im Block | nur diese eine Anweisung |
| Ziel mit Typdeklaration | ❌ `q i64 ?= …` ist ein Parsingfehler (`yh8904fm`) | ✅ `q i64 = …: { … }` |
| Rechter Wert ist constant-folded | ❌ `s8gh6opt` (`4 / 2` wird zu plain gefaltet, kein option mehr) | ✅ Subjekt wird weiterhin zu `?T` normalisiert |
| Voraussetzung | Funktion hat einen `?T`-Ergebnisparameter oder liegt selbst in einem Block-match (sonst `l86f91me`) | keine |
| Arm bei Erfolg | läuft ebenfalls einmal (Körper endet mit `nil`-Match), der Arm sollte nur Verwerfen oder reine Verarbeitung enthalten | nur der `ok`-Arm läuft |

Auswahlreihenfolge:

1. **Eine Anweisung, Fehler gilt als 0, danach nichts zu tun** → beide äquivalent, B bevorzugen: eine Einrückungsebene und einen Nullwert-Store weniger,
   `no fmt` ist auf beide Formen idempotent.
2. **Eine Folge fallibler Anweisungen soll eine gemeinsame Fehlerbehandlung nutzen** (Datei lesen → parsen → zurückschreiben) → A: `?=` eine pro Zeile,
   gemeinsamer `err ->`-Arm, der erste Fehler stoppt im Aufräum-Arm.
3. **Fehler soll alten Wert behalten / einen konkreten Fallback-Wert einsetzen / nachfolgende Anweisungen normal laufen lassen** → B (`-> sum`, `-> 0`, `-> it`).
4. **Der Wert soll weiterverarbeitet werden** (rechts einer Zuweisung, Feldziel, rechts von `?=`) → B: A ist eine Anweisung und hat keinen Wert.
5. **Elementweise arbeiten, ein einzelner Fehler soll nicht die ganze Charge zerstören** → B; **die ganze Charge scheitert gemeinsam, dann abbrechen** → A (stoppt auch Schleifeniterationen).

Zwei häufige Stolperfallen:

- 🔴 **Annotierten Position**: `#{overflow=wrap}` muss **im Block** stehen, direkt über der `?=`-Zeile. Außerhalb des Block-match
  (über `{`) **wirkt es nicht** — empirisch liefert ein Überlauf trotzdem `err` und `np` wird durch `?=` auf Null gesetzt.
- 🔴 **Akku nicht von A kopieren**: `sum ?= 1 / (i - 1)` löscht in der Runde mit Fehlschlag den **bereits akkumulierten `sum` mit zu**
  `0` (Nullwert-Schreiben durch `?=`). Um den akkumulierten Wert zu behalten, B mit `-> sum` verwenden.

#### Richtlinien zum Match-Stil

```no
#{fmt-skip}
; ❌ doppelten Arm-Body vermeiden
w = tls-c.send(req)
w: {
    nil -> {
        tls-c.close()
        return
    }

    err -> {
        tls-c.close()
        return
    }

    ok -> n = it
}
```
```

```no
; ✅ gemeinsame Logik in den catch-all-Arm
w = tls-c.send(req)
w: {
    ok -> n = it

    -> {
        tls-c.close()
        return
    }
}

; ✅ umgekehrt genauso: einfache Arme benennen, komplexe Logik in ->
val: {
    nil -> return

    err -> log(it.msg())

    -> {
        n = it
        total = total + n
        process(n)
    }
}
```

```no
; eine Anweisung — keine geschweiften Klammern
val: {
    ok -> print(it)

    -> print('empty or error')
}

; mehrere Anweisungen — geschweifte Klammern erforderlich
val: {
    ok -> {
        n = it
        total = total + n
    }

    -> {
        log('failed')
        return
    }
}
```

```no
; implizite it-Bindung
val: {
    ok -> process(it); it = der ausgepackte Wert

    err -> log(it.msg()); it = der strukturierte Fehler err.err (kein str)

    -> log('empty'); catch-all, hier nil
}
```

```no
#{fmt-skip}
; Gültigkeitsbereich von it: verschachtelte matches stellen ihn ebenenweise wieder her
; (Platzhalterfunktionsname keep — `use` ist ein Import-Schlüsselwort und darf kein Funktionsname sein;
;  und ein nackter `->`-Arm darf it nur lesen, wenn nil/err nicht gleichzeitig beansprucht sind, sonst Kompilierungsfehler — daher überall ok ->)
outer: {
    ok -> {
        keep(it)            ; äußeres it
        inner ?T = f(it)
        inner: {
            ok -> keep(it)  ; inneres it = ausgepackter Wert von inner
        }
        keep(it)            ; ✅ nach Ende des inneren Arms ist it wieder auf den äußeren Wert zurückgesetzt
    }
}
```

> **Hinweis**: Die Wiederherstellung erfolgt nur bei **verschachtelten** matches. Nach Ende des äußersten match behält `it` noch den Wert des letzten Arms,  
> Verwende `it` nicht außerhalb des match. Wenn du es über Ebenen hinweg brauchst, speichere es in einer benannten Variable (`doc = it`) oder verwende die Destrukturierungsbindung  
> `ok(v) -> ...`, um das Payload an einen eigenen Namen zu binden.

> **Best Practice (für option-Unpacking `it` bevorzugen)**: Wenn der gematchte Wert ein **option `?t`** ist, schreibe bevorzugt `ok -> ...(it)...`  
> statt `ok(v) -> ...(v)...` — du führst keinen zusätzlichen Bindungsnamen ein und vermeidest damit auf natürliche Weise die Falle „gleichnamige Variablen in verschiedenen Armen führen zu codegen-Typverwechslungen“.  
> Nur zwei Fälle erfordern zwingend die **benannte Bindung** `ok(v) ->`: (1) Der gematchte Wert ist eine **benutzerdefinierte Enum-Variante** (wie  
> `res { ok(v str) }` oder `shape { rect(w, h) }`) — dort bezeichnet `it` die **gesamte Enum**, nicht das Payload, das Payload muss also benannt werden;  
> (2) du musst den Wert **nach einem verschachtelten match** weiterverwenden — der innere match bindet `it` neu, verwende daher eine benannte Bindung oder kopiere vorher in eine benannte lokale Variable.

### Indexzuweisung erfordert explizites Unpacking

Ein Indexlesen liefert einen option-Wert (bei Bereichsüberschreitung `err`, `code.index-out`); **eine direkte Zuweisung an einen Index würde die Bereichsüberschreitung in einen ungeprüften Out-of-Bounds-Write verwandeln**, daher ist sie nicht erlaubt. Du musst eine von zwei Varianten wählen:

```no
#{fmt-skip}
; ❌ nicht erlaubt: a[1] = 2
```
```

```no
; ✅ place-match: Bereichsüberschreitungs-Arm selbst behandeln (it = Elementposition, lesbar und schreibbar)
a[1]: {
    ok -> it = 2

    -> _
}

; ✅ Fehler hochwerfen: wenn die umgebende Funktion einen option-Ergebnisparameter hat (Bereichsüberschreitung → hochwerfen)
a[1] ?= 2
```

- Kann der Compiler beweisen, dass der Index im Bereich liegt (Array fester Länge + konstanter Index, Schleifenvariable nachweislich im Bereich usw.), **entfernt er die Prüfung intern und weist direkt zu, ohne Laufzeit-Overhead**; syntaktisch muss aber weiterhin die Unpacking-Form geschrieben werden — es gibt auf Benutzerseite keine Ausnahme.
- map (`[str]i64` usw.) ist **völlig unabhängig von dieser Regel**: `m['b'] = c` kann direkt geschrieben werden, weder place-match noch `?=` sind nötig — der Index von map ist ein **Schlüssel** und keine Position, das Schreiben ist put/insert (fehlenden Schlüssel einfügen, vorhandenen überschreiben), immer definiert, es gibt keinen „Out-of-Bounds-Write“; struct-Feldbasen (`self.keys[i] = v`) fallen vorerst ebenfalls nicht unter diese Regel.
- Die **Lese**-Seite von map folgt weiterhin der option-Konvention (genau dieselbe Regel wie `a[i]` bei Arrays/vec/slice): `x = m[k]` innerhalb einer Funktion ergibt `?V`, bei fehlendem Schlüssel `nil`. `m[k]` ist synonym zu `m.get(k)`, `m[k] = v` synonym zu `m.put(k, v)`, beide Schreibweisen sind frei mischbar.
- `m[k] ?= v` ist ein **Kompilierungsfehler** (nicht bloß redundant): Der ok-Arm von `?=` bedeutet „Element auslesen und zurück an die **Position** schreiben“, das von map gelesene `?V` ist aber nur eine Kopie von `get`, das Zurückschreiben erreicht die map nicht. Schreibe direkt `m[k] = v`. Die place-match-Form `m[k]: { ok -> it = v }` bleibt verfügbar (äquivalent zu put).

### Iterationsvariablen sind ebenfalls Element„positionen“

`x <- a` (`a` vom Typ `arr` / `vec` / `slice` / Array fester Länge) bindet `x` an **die Position des Elements selbst**, nicht an eine pro Runde geladene Kopie:

- **Lesen** von `x` liest `a[__i]` erneut — innerhalb derselben Runde sieht Schreiben danach den neuen Wert, `x = x + 1` ist Read-Modify-Write;
- **Schreiben** von `x = v` landet wieder in `a[__i]`;
- das `it` im match-Arm ist ein **Alias** derselben Position (nur innerhalb dieses Arms gültig), daher sind `x: { ok -> it = v }` und `a[i]: { ok -> it = v }` vollständig äquivalent.

```no
; die folgenden beiden Stücke liefern dasselbe Ergebnis: jedes Element von a wird zu 5
a []i64 = [0, 0, 0]
i <- [0..3): {
    a[i]: {
        ok -> it = 5

        -> _
    }
}

b []i64 = [0, 0, 0]
x <- b: {
    x: {
        ok -> it = 5

        -> _
    }
}
```

- Ist das Element selbst ein option (`[]?T`), ist `it` das **Payload**, beim Zurückschreiben wird automatisch wieder `ok` gewrappt.
- Nach Ende des Arms verfällt der Alias: ein danach geschriebenes `it = v` landet nicht mehr im Container.

**Fälle, in denen es keine Position gibt** (Schreiben bleibt nur lokal):

- `c <- s` (`str`): Das Element ist ein dekodierter **Codepoint**, dafür gibt es keinen Speicherplatz im String.
- `k <- m` (map): Der Index ist ein **Schlüssel** und keine Position, das Schreiben landet nicht wieder in der Tabelle.

### `it` darf nur in Armen mit „beweisbar einem einzigen Fall“ verwendet werden

Der catch-all-Arm `->` übernimmt alle Fälle, die von keinem vorherigen expliziten Arm beansprucht wurden. Nur wenn genau **eine** Möglichkeit übrig bleibt,  
hat `it` eine eindeutige Bedeutung:

```no
; ✅ -> ist ok (nil / err sind bereits beansprucht)
v: {
    nil -> log('empty')

    err -> log(it.msg())

    -> process(it)
}

; ✅ -> ist err (ok / nil sind bereits beansprucht)
v: {
    ok -> process(it)

    nil -> log('empty')

    -> log(it.msg())
}
```

```no
#{fmt-skip}
; ❌ -> kann nil oder err sein — Kompilierungsfehler
v: {
    ok -> process(it)

    -> log(it)
}
```
```

Ergänze `nil ->` / `err ->` oder verwende `ok ->`, dann ist es gültig. enum und nackte matches (`{ cond -> ... }`)  
sind davon ausgenommen: `it` bezeichnet den gematchten Wert selbst, nil / err sind hier nicht möglich.

> Das `it` des `err`-Arms ist **immer der Fehlerwert (`err.err`)**, unabhängig vom Elementtyp des option —  
> `option { ok(v t), nil, err(e err.err) }`. Selbst bei skalaren option-Typen wie `?i64` / `?bool` ist  
> `it` im `err`-Arm ein strukturierter Fehler: Nachricht lesen mit `msg = it.msg()`,  
> Fehlercode vergleichen mit `it.is(code.io)`, Ort des Auftretens in `it.file` / `it.line` / `it.col`  
> (vom Compiler automatisch an der Konstruktionsstelle von `err(...)` eingefügt).

```no
; ✅ kombinierte option-Muster: nil || err -> body
; wenn die option nil oder err ist, denselben Branch verwenden
val: {
    nil || err -> {
        cleanup()
        return
    }

    ok -> process(it)
}

; ✅ auch mit -> catch-all mischbar
val: {
    nil || err -> log('failed')

    ok -> process(it)
}
```

### Kurzschluss-Pipeline (`->`)

`->` ist ein **linksassoziativer Kurzschluss-Pipeline-Operator** mit fester Semantik, **unabhängig davon, ob außen eine Zuweisung steht oder ob man sich in einem match-Arm befindet**. Nackte Anweisungen, einzeilige Arm-Bodies und rechte Seiten von Zuweisungen teilen dieselbe Lowering-Logik.

Die Knoten innerhalb der Pipeline fallen in drei Klassen:

| Knoten                                | Wirkung auf den Pipeline-State                             
| --------------------------------- | ----------------------------------------- |
| Seiteneffekt-Aufruf (ohne Rückgabe, wie `print('A')`)         | wird ausgeführt, **ändert den State nicht**; solange bisher Ok, geht es zum nächsten Knoten        
| Aufruf mit Rückgabe `?T` (kann scheitern, wie `might-fail()`) | wird ausgeführt, **überschreibt den State**; sobald nil/err, **werden alle nachfolgenden Knoten übersprungen** 
| gewöhnlicher Endwert-Ausdruck                          | wird nur ausgewertet, solange der State noch Ok ist                        

**Nur Knoten mit Rückgabe `?T` können die Pipeline in den Fehlerzustand schalten**; Seiteneffekten wie `print` niemals. Deshalb wirkt eine Kette aus lauter `print` wie ohne Kurzschluss — tatsächlich gibt es einfach keinen Knoten, der einen Fehler erzeugen könnte.

```no
; keine fehlerfähigen Knoten → alles wird ausgeführt
ok -> print('A') -> print('B')

; might-fail() liefert ?T → bei Fehler wird print('B') nicht ausgeführt
ok -> print('A') -> might-fail(x) -> print('B')

; rechte Seite einer Zuweisung: dieselbe Semantik
r = print('E') -> might-fail(x) -> 42
```

**Präzisierung der Grenzen:**

1. `->` hat eine eindeutige Semantik, nackte Anweisungen und Zuweisungsausdrücke teilen dieselbe Pipeline-Lowering-Logik; der Modus wechselt nicht je nachdem, ob außen eine Zuweisung steht.
2. `->` im **einzeiligen Body eines match-Arms ist eine Pipeline-Fortsetzung, kein neuer Arm**; Arme werden durch **Zeilenumbrüche** getrennt:

```no
; ✅ ein Arm, Body ist eine Pipeline → beide Zeilen werden ausgegeben
ok -> print('A') -> print('B')

; ✅ zwei Arme (durch Zeilenumbruch getrennt) → bei ok-Treffer wird 'miss' nicht ausgegeben
v: {
    ok -> print('hit')

    -> print('miss')
}
```

> ⚠️ Arme werden durch Zeilenumbrüche getrennt, daher ist `pat -> A -> B` auf derselben Zeile **eine Pipeline innerhalb eines Arms**, kein „if A else B“. Wenn du if/else brauchst, schreibe die Arme in getrennten Zeilen oder verwende eine in `{}` eingeschlossene Kurzschlussgruppe.

1. **Pipelines können Werte liefern**: Auf der rechten Seite einer Zuweisung ist das Pipeline-Ergebnis der **Endwert-Knoten** (nur ausgewertet, solange der State noch Ok ist).

```no
n = 7
x = 1 > 2 -> 42; Bedingung nicht erfüllt → x behält seinen ursprünglichen Wert 7
y = 2 > 1 -> 42; y == 42
s str = 'old'
s = 1 > 2 -> 'new'; Kurzschluss → s bleibt 'old'
r = print('E') -> might-fail(bad) -> 99; Fehler in der Mitte → r behält seinen ursprünglichen Wert
```

Bei Fehlschlag (Bedingung nicht erfüllt oder durch einen `?T`-Knoten kurzgeschlossen) **wird die Zuweisung nicht ausgeführt**: Die Variable behält ihren bisherigen Wert; bei erster Deklaration ist es der Nullwert des Typs. Das entspricht der Anweisungsform — `{ 1 > 2 -> x = 42 }` verändert `x` ebenfalls nicht.

Der Typ des Werts wird aus dem Endausdruck des Arms abgeleitet (`str` / `i64` / `?T` alle möglich) und nutzt dieselbe Inferenz wie die bestehende Match-Wertform `x = subject: { arms }`.

### If / Else

if-else-Gruppen (Kurzschluss) **müssen** in `{}` eingeschlossen sein. Die erste passende Bedingung gewinnt, nachfolgende Bedingungen werden nicht mehr geprüft.

```no
; mehrere Zweige (neue Form empfohlen) — Kurzschluss-if-else-Kette
{
    a == 1 -> {
        a = 1
        b = 2
    }

    a == 2 || a == 3 -> do-something()

    -> {
        c = 0
    }
}

; einzelnes if (beibehalten) — eigenständiges if, kein Kurzschluss
x == 1 -> do-something()

; ternärer Ausdruck condition ? true-value : false-value
c = flag ? 1 : 2
max = sum > 10 ? sum : 10
```

**Wichtige Regeln:**

1. **Kurzschlussgruppe** — Werden mehrere `cond -> body` in `{}` eingeschlossen, entsteht eine if-elif-else-Kette, von der nur der erste passende Branch ausgeführt wird.
2. **Eigenständiges if** — Direkt im Funktions-/Schleifenkörper geschriebenes `cond -> body` (ohne `{}`) ist ein eigenständiges if und schließt nicht mit benachbarten if-then kurz.
   **Sobald ein else auftaucht, muss eine Gruppe gebildet werden** — ein in einer neuen Zeile stehender `->`-Auffang-Arm außerhalb von `{}` ist ein **Kompilierungsfehler**: ein isoliertes `cond ->` mit `->` meldet `k3r7mbqd`, stehen darüber weitere `cond ->`-Arme, `m7vq3xrt` (die Meldung nennt die Zahl der verworfenen Arme). Der Parser faltet einen in einer neuen Zeile stehenden `->` nur in die **nächstgelegene** nackte if, die übrigen Arme werden stillschweigend ignoriert — der wortwörtliche Sinn weicht vom tatsächlich kompilierten Ergebnis völlig ab, daher ist es eine Regel und keine Warnung. Schließe alle Arme in `{ ... }` ein; **ein isoliertes `cond -> body` ohne else darf weiterhin nackt geschrieben werden**.
3. **Kein Mischen** — Die direkten Kindknoten einer `{}`-Kurzschlussgruppe müssen ausschließlich `cond -> body`-Branches sein, normale Anweisungen dürfen nicht dazwischen.

```no
#{fmt-skip}
; ❌ kein Kurzschluss — das sind eigenständige ifs, alle Bedingungen werden geprüft
git-dispatch = (cmd str) {
    cmd == 'a' -> {
        fa()
    }
    cmd == 'b' -> {
        fb()
    }
}
```
```

```no
; ✅ Kurzschluss — in {} eingeschlossen, der erste Treffer gewinnt
git-dispatch = (cmd str) {
    {
        cmd == 'a' -> {
            fa()
        }

        cmd == 'b' -> {
            fb()
        }

        true -> {
        }
    }
}
```

```no
#{fmt-skip}
; ❌ if/else außerhalb der Kurzschlussgruppe geschrieben — Kompilierungsfehler
; der in einer neuen Zeile stehende `-> break` hängt nur an `c == 43`, wird aber nach Treffer von `c == 45` trotzdem ausgeführt
;   c == 45 -> {
;       exp-sign = -1
;       i = i + 1
;   }
;   c == 43 -> i = i + 1
;   -> break
```
```

```no
; ✅ derselbe Code als if-Gruppe — Arme schließen sich gegenseitig aus, `->` ist der echte Auffang-Arm
{
    c == 45 -> {
        exp-sign = -1
        i = i + 1
    }

    c == 43 -> i = i + 1

    -> break
}
```

```no
#{fmt-skip}
; ❌ match-block vermischt mit normalen Anweisungen → Kompilierungsfehler
{
    cmd == 'a' -> {
        fa()
    }
    print(cmd)
    cmd == 'b' -> {
        fb()
    }
}
```
```

```no
; ✅ normale Anweisungen in den Branch-Body verschieben
{
    cmd == 'a' -> {
        print(cmd)
        fa()
    }

    cmd == 'b' -> {
        fb()
    }

    true -> {
    }
}
```

### Asynchrone Programmierung: `awy { ... }`-Coroutinengruppe + `co`

Nolang hat nur eine Oberflächensyntax für Nebenläufigkeit: die Coroutinengruppe `awy { ... }`, in der Aufgaben mit `co` gespawnt werden.
Das Schlüsselwort `run` wurde entfernt (`run` ist jetzt ein gewöhnlicher Bezeichner); die Wait-Präfixform `awy <handle>` und nackte Blöcke als Coroutinengruppe an Anweisungsposition
sind ebenfalls abgeschafft. **`co` außerhalb einer Gruppe ist ein Kompilierungsfehler** (`E_CO_OUTSIDE_GROUP`).

`co` hat zwei Formen, beide starten nur einen **Prozess** und liefern keinen Wert:

```no
; Prozess: schreibt nur in den Kanal, keine Rückgabe deklariert
produce = (ch chan[i64], v i64) {
    ch <- v
    ch.close()
}

ch chan[i64] = with-cap(2)
awy {
    co produce(ch, 1)          ; ① Prozess-spawn: co f(args)
    co {                       ; ② Closure-spawn: co { ... }
        ch <- 2
        ch.close()
    }
}
sum = 0
i <- ch: {
    sum = sum + i
}
print(sum.to-str())            ; => 3
```

#### ① Prozess-spawn: `co f(args)`

`f` **darf keine Rückgabe deklarieren**. Das Ergebnis eines Spawns hat keinen Empfänger (`co` ist kein Wert), daher meldet eine
mit `co` aufgerufene Funktion mit Rückgabewert `c0retval`:

```no
worker = (n i64) (r i64) { r = n }   ; Rückgabewert deklariert
awy { co worker(1) }                  ; ✗ c0retval
```

Das implizite `self` einer Methode zählt **nicht** als Rückgabewert — es ist der bei jeder Methode vorhandene Empfänger-Ausgabeparameter, dessen Zweck es ist,
Aufrufe verkettbar zu machen, daher ist `co obj.flush()` völlig normal:

```no
queue {
    n i64
}
queue.flush = () {          ; implizit (self queue), keine weiteren Rückgaben
    self.n = 0
}
awy { co q.flush() }        ; ✓
```

Wenn du ein Ergebnis brauchst, verwende einen Kanal: Der Prozess sendet den Wert in den Kanal und ruft `close()` auf, der Aufrufer leert ihn außerhalb der Gruppe. Das ist keine Stilempfehlung,
sondern eine Regel — das Ergebnis eines gespawnten Aufrufs ist erst nach dem Join der Gruppe lesbar, und der Lesepunkt liegt nicht am Join-Punkt;
genau dafür gibt es `chan`.

#### ② Closure-spawn: `co { ... }`

`co { ... }` übergibt einen Prozess direkt an eine Coroutine. **Freie Variablen im Block werden nach Wert eingefangen**
(eine Momentaufnahme zum Zeitpunkt des Spawns, dieselbe Semantik wie die Deep-Copy der Argumente bei normalem Spawn):

```no
seed = 33
ch chan[i64] = with-cap(1)
awy {
    co {
        ch <- seed
        ch.close()
    }
}
r = 0
<- ch: { ok -> r = it }
print(r.to-str())          ; => 33
```

#### `co` ist kein Wert: nur als eigenständige Anweisung

`co` liefert void und muss daher eine ganze Anweisung ausfüllen. Rechte Seite einer Zuweisung, Argumentposition, Rückgabeposition — überall
`E_CO_NOT_STATEMENT` (`c0nostmt`):

```no
r = co produce(ch, 1)      ; ✗ c0nostmt
print(co produce(ch, 1))   ; ✗ c0nostmt
```

Außerdem darf auf `co` nur ein „Aufruf“ oder ein „Block“ folgen, andere Formen melden `E_CO_NOT_CALL` (`c0notcal`).

#### Farbloses async: automatische Monomorphisierung

`co` ist farblos (colorless): Du schreibst den unmarkierten Funktionsnamen `produce`, der Compiler erzeugt automatisch die
Variante `produce-async` und führt sie intern als markierte stapellose Coroutine aus. Schreibe das Suffix `-async` nicht von Hand in Anwendungscode.

Regeln:

- **Monomorphisierung**: `co produce(ch, 1)` erzeugt automatisch `produce-async` (Kopie des Rumpfes von `produce`, umbenannt). Wird `produce` auch synchron aufgerufen, koexistieren beide.
- **Transitivität**: Enthält ein Prozess selbst `awy { co child(n) }`, wird auch er zu `-async` monomorphisiert, die asynchrone Markierung wandert die Aufrufkette hinauf.
- **Barriere**: Anweisungen in der Gruppe, die kein `co` sind (normale Zuweisung, `print`, Schleifen, `if`, **Kanal lesen** …), warten zuerst auf alle laufenden Aufgaben. Datenabhängigkeiten innerhalb der Gruppe werden daher als „zuerst Kanal lesen, dann mit dem gelesenen Wert spawnen“ geschrieben — das Kanallesen ist selbst eine Barriere und besser lesbar als eine implizite Variablenabhängigkeit.
- **Funktionskörper ist keine Coroutinengruppe**: Nur `awy { ... }` führt eine Gruppe ein. `co` im Funktionskörper, in `if`-Branches oder in Schleifenkörpern wird immer abgelehnt.
- **Auch verworfene Ergebnisse warten**: `awy { co side(5) }` spawnt und awaitet trotzdem, es bleiben keine unerwarteten Aufgaben übrig.

#### Scheduling-Modell: echtes Suspendieren

Der `-async`-Klon läuft nicht „in einem Stück durch und meldet dann“, sondern ist eine **fortsetzbare Zustandsmaschine**: Lokale Variablen wandern in einen Heap-Frame, der an `%task.frame` hängt,
jeder Suspendierungspunkt wird zum Terminator des Blocks (Zustandsnummer in den Frame schreiben und zurückkehren),
der Funktionsbeginn wird zu einer Kette von Zustandsvergleichen (`__state == k` ⇒ direkt zum k-ten Fortsetzungspunkt springen).

Daher **suspendieren** diese drei Stellen, statt zu blockieren:

| Suspendierungspunkt | Fortsetzungsbedingung 
|---|---|
| `async-yield()` | reiht sich selbst hinten in die Queue ein und wartet, bis es vorne steht 
| `awy` eines noch unfertigen Unterauftrags | wird bei Fertigstellung des Unterauftrags geweckt (in der Kette `%task.waiters` registriert, ein Wecken weckt die ganze Kette) 
| leerer Kanalempfang (`v <- ch:` / `<- ch:`) | `send` / `close` nimmt die ganze Wait-Kette herab und reiht sie einzeln wieder ein 

Das Scheduling ist single-threaded, kooperativ mit FIFO-Ready-Queue, daher ist die **Reihenfolge vollständig vorhersagbar**, ohne sleeps:
Zwei Aufgaben, die jeweils einmal abgeben, ergeben stets `a1 b1 a2 b2` (`tests/coro-yield.no` nagelt die Ereigniscodes fest).
Ein geweckter Wartender **bedeutet nicht, dass er einen Wert bekommt** — ein `send` fügt nur ein Element hinzu, kann aber mehrere Wartende wecken,
daher ist der Fortsetzungspunkt einer Channel-Operation der Park-Block selbst: Er prüft die Lesbarkeit erneut und entscheidet dann, ob er nimmt oder weiter wartet.

> Deadlocks sind erkennbar: Ist die Ready-Queue leer und der Zielauftrag noch unfertig ⇒ gibt die Laufzeit
> `nolang: deadlock: …` auf stderr aus und bricht ab. Das ist ehrlicher als „einen Nullwert zurückzugeben, den niemand gesendet hat“.

> **Low-Level-Lowering** (keine Benutzersyntax): Jedes `co` wird zu `OpRun`, am Gruppenende wird für jede Aufgabe `OpAwait` eingefügt;
> `KRun` / `KAwait` / `OpRun` / `OpAwait` existieren nur in HIR/MIR. Die Coroutinen-Transformation führt MIRs
> `PlanCoroutines` nach `Analyze` durch (`src/mir/coro.go`).

Abbruch: `cancelled()` ermöglicht eine kooperative Selbstprüfung innerhalb einer Coroutine-Funktion; `cancel(h)` ist eine Low-Level-Handle-Operation,
die neue Syntax stellt kein Task-Handle bereit, Anwendungscode verwendet sie nicht.

### Mehrfachzuweisung

Funktionen können mehrere Werte zurückgeben; beim Aufruf nimmt man sie mit einer Mehrfachzuweisung entgegen:

```no
; Funktionsdefinition mit mehreren Ergebnisparametern
swap = (a i64, b i64) (x i64, y i64) {
    x = b
    y = a
}

; Mehrfachzuweisung
a, b = swap(5, 3)

; unerwünschte Rückgabewerte mit _ ignorieren (Platzhaltervariable)
_, b = swap(5, 3); nur den zweiten Wert nehmen, den ersten ignorieren
a, _ = swap(5, 3); nur den ersten Wert nehmen, den zweiten ignorieren
_, _ = swap(5, 3); alle Rückgabewerte ignorieren (Aufruf nur wegen der Seiteneffekte)

; auch als Body eines match-Arms unterstützt
val: {
    ok -> {
        a, b = parse-pair(it)
    }

    -> return
}
```

## Operator-Overloading

Wenn der statische Typ des linken Operanden eine bestimmte `_op`-Methode definiert, schreibt der Compiler in der Lowering-Phase den Infix-Ausdruck `a op b` automatisch in den Methodenaufruf `a._op(b)` um. Die überladbare Menge ist geschlossen und umfasst genau 5 symmetrische binäre Arithmetikoperatoren:

| Operator | Overload-Methode 
|--------|----------|
| `+` | `_add` |
| `-` | `_sub` |
| `*` | `_mul` |
| `/` | `_div` |
| `%` | `_mod` |

```no
big {
    v i64
}

; Rückgabetyp als ?big geschrieben, Compiler behandelt ihn als reines Payload big
big._add = (b big) (c ?big) {
    c = big {
        v: .v + b.v
    }
}

f = (a big, b big) (out big) {
    r big = a + b; Compiler schreibt um zu a._add(b)
    out = r
}
```

**Typkonvention**: Bei Operator-Overloading-Methoden wird das Ergebnis **als `?T` geschrieben, wirksam ist aber der reine Payload-Typ `T`** — der Compiler führt kein option-Wrapping/-Unwrapping durch, im Methodenkörper wird `c` direkt ein `T`-Wert zugewiesen, und der Aufrufer erhält ebenfalls `T`. Fehler wie Division durch null trappen wie bei eingebauten numerischen Typen direkt zur Laufzeit, es entsteht kein option.

**Wichtige Punkte zur Semantik**:

- Nur wenn der statische Typ des linken Operanden die entsprechende `_op`-Methode definiert, wird umgeschrieben; eingebaute numerische Typen (i64, f64 usw.) nutzen weiterhin eingebaute Arithmetik.
- Ein expliziter Methodenaufruf `a._add(b)` ist vollständig äquivalent zu `a + b`.
- Linksverschachtelte Ketten werden Ebene für Ebene umgeschrieben: `a + b + c` → `(a._add(b))._add(c)`; der Zwischentyp bleibt `T` und kann weiter an Operationen oder Methodenaufrufen teilnehmen.
- Die Standardbibliothek `bigint` nutzt genau diesen Mechanismus für `+ - * / %` (siehe bigint-Abschnitt unter „Standardbibliothek > Weitere Module“).

## Arrays und Slices

Container speichern Kopien der Daten, die Originalvariable ist vom Container unabhängig — baumelnde Referenzen sind damit ausgeschlossen.

**Array fester Länge arr:**

```no
; Array fester Länge verwenden
a [3] = [1, 2, 3]; Array fester Länge i64 mit Länge 3
a [3]u16 = [1, 2, 3]; Array fester Länge mit angegebenem Typ

a[nil]
u16 = [1, 2, 3]; Länge automatisch ableiten (Typ angegeben)
a[nil] = [1, 2, 3]; Länge automatisch ableiten (i64)
```

**Array variabler Länge vec:**

```no
v = [1, 2, 3]; Array variabler Länge i64
bs = [0x11, 0x22, 0x33]
v []u8 = [1, 2, 3]; Array variabler Länge mit angegebenem Typ

; vorab reservieren (wiederholtes Vergrößern vermeiden)
v = with-cap(100); len=0, cap=100 (Indexzugriff nach push nötig)
v = with-len(100); len=100, cap=100 (direkter Indexzugriff möglich)
v = with-cap-len(200, 100); len=100, cap=200 (Platz für Vergrößerung reserviert)
```

**Slice (Ansicht, kein eigenständiger Typ):**

Ein Slice ist eine **Ansicht (view)** auf die Originaldaten, er kopiert keine Daten und erzeugt keinen neuen eigenständigen Typ.  
Intern speichert ein Slice nur einen Zeiger auf den ursprünglichen Puffer, Länge und Kapazität, daher:

- Das Ändern von Elementen über den Slice wirkt auf die Originaldaten und umgekehrt
- Der Slice besitzt die Daten nicht; nach Freigabe der Originalvariable ist er ungültig
- Der Typ des Slice wird vom Originaltyp bestimmt, Methoden gelten natürlich ohne „Vererbungs“-Mechanismus

```no
; unterstützt arr/vec/str
; unterstützt Bereiche, dieselbe Schreibweise wie for <-
nums [5]u8 = [0, 1, 2, 3, 4]

nums[..]; [0 1 2 3 4]   vollständig unbegrenzt, entspricht dem gesamten Slice
nums[1..]; [1 2 3 4]     keine obere Grenze, von Index 1 bis zum Ende
nums[..4]; [0 1 2 3 4]   keine untere Grenze, von Anfang bis Index 4
nums[2..3]; [2 3]        begrenzt, beide Enden eingeschlossen
nums[1..3]; [1 2 3]      begrenzt, beide Enden eingeschlossen
nums[1..3); [1 2]        links geschlossen, rechts offen
nums(1..3); [2]          beide Enden offen

; Strings (Slice-Ergebnistyp ist str-Ansicht, teilt den zugrunde liegenden Speicher; Index ist Codepoint-Position, wie bei s[i])
s = 'abc'
s[1..]; 'bc'
s[1..s.len()); 'bc'
b str = s[0..1]; Slice-Ergebnis ist bereits str, kann direkt einer str-Variable zugewiesen werden
```

> Die Slice-Syntax unterstützt dieselben 16 Bereichskombinationen wie match/range-for. Im Slice-Kontext basieren unbegrenzte Bereiche auf der Länge des Slices: `[..]` bedeutet das gesamte Slice, `[a..]` von Index a bis zum Ende, `[..b]` vom Anfang bis Index b.

**Typen und Methoden von Slices:**

Ein Slice erzeugt keinen neuen eigenständigen Typ, sondern ist nur eine Ansicht des Originaltyps (mit angepasstem Startzeiger und Länge).  
Daher sind die Methoden des Originaltyps direkt verfügbar:

| Originaltyp           | Slice-Ansichtstyp       | Verfügbare Methoden                                                                             
| -------------- | ------------ | -------------------------------------------------------------------------------- |
| `arr` (`[n]t`) | `[]t<range>` | alle Methoden von `[]t` (wie `len`, `push`, `pop`, `contains`, `reverse`, `clone`, `fill`, `to-arr` usw.) 
| `vec` (`[]t`)  | `[]<range>`  | wie oben                                                                               
| `str`          | `str<range>` | alle Methoden von `str` (wie `to-upper`, `to-lower`, `index`, `contains`, `slice`, `copy`, `fill` usw.)  

```no
; arr-Slice → vec-Ansicht, teilt den zugrunde liegenden Speicher von arr
a [5]u8 = [0, 1, 2, 3, 4]
s = a[1..4]; s ist eine []u8-Ansicht, zeigt auf den Speicher von a
n = s.len(); slice.len()

; vec-Slice → vec-Ansicht, teilt den zugrunde liegenden Speicher von vec
v = [10, 20, 30, 40, 50]
s = v[2..]; s ist eine []i64-Ansicht
s.reverse(s.len()); slice.reverse

; str-Slice → str-Ansicht, teilt den zugrunde liegenden Speicher von str
s = 'Hello World'
sub = s[6..]; sub ist eine 'World'-Ansicht
upper = sub.to-upper(); str.to-upper

; Ändern von Elementen über den Slice wirkt auf die Originaldaten
data = [10, 20, 30, 40, 50]
view = data[1..4]; view = [20, 30, 40]
view[0] = 99; Element von view ändern
; data[1] ist jetzt ebenfalls 99, da view den Speicher von data teilt
```

### Indexierung

```no
; String-Index → liefert ?char (Unicode-Codepoint, kein Byte)
c char = s[i]; expliziter plain-Typ holt das Ergebnis als char zurück (siehe〈Sichere Indexierung〉)

; Element aus arr / vec holen
arr[i]; → ?elem
vec[i]; → ?elem

; Wert aus map holen
map[str]
```

> **Das Ergebnis eines Indexlesens ist stets `?T`**: `arr[i]` / `vec[i]` / Array fester Länge / `slice` → `?elem`,  
> `str` / `txt`-Index `s[i]` → `?char` (D6, dieselbe vereinheitlichte Regel wie bei Integer-Arithmetik).  
> Daher ist der **abgeleitete Typ von `str[i]` `?char`**; um ein plain `char` zurückzubekommen, gibt es zwei Wege:  
> einen expliziten plain-Typ bei der Bindung schreiben (`c char = s[i]`, in der Lowering-Phase kurzgeschlossen synthetisiert, liest einen plain-Wert zurück),  
> oder mit match / `?=` auspacken.  
> `str[a..b]` als Slice liefert weiterhin `str` (Ansicht mit Codepoint-Semantik, teilt den zugrunde liegenden Speicher).  
> `char` kann **implizit in `str` konvertiert werden** (als ein Zeichen codiert), daher sind alle folgenden Schreibweisen gültig, ohne `char.to-str()` manuell aufzurufen:

```no
s = 'héllo'
c char = s[1]; 'é' (char, sein Codepoint ist 233)
a str = s[1]; 'é' (implizite Konvertierung char → str)
b str = s[0..1]; 'hé' (Slice-Ergebnis ist bereits str)
msg = 'first: ' - s[0]; char wird bei Verkettung (-) implizit zu str
ok = s[0] == 'h'; char wird beim Vergleich mit str implizit zu str
```

### Sichere Indexierung (Safe Indexing)

Bei der Indexierung `v[i]` von `arr` / `vec` / `slice` (`[]T`) / Arrays fester Länge / `str` / `txt` ist das **Ergebnis immer `?T`**: Bei Bereichsüberschreitung wird `err` zurückgegeben (`err('index out of range', code.index-out)`, mit der Quellcode-Position des Index), im Bereich `ok(Wert)`, es wird niemals abgebrochen (runtime error: index out of bounds). Das ist dieselbe Regel wie das nicht annotierte Überlaufverhalten bei `+ - * / %` für Integer (siehe〈Überlauf bei Integer-Operationen〉).

Es gibt nur drei Wege, `?T` zu verbrauchen (**die Annotation `#{index-out=DEF}` wurde in D11 veraltet und entfernt**, eine verbliebene Annotation ist ein Kompilierungsfehler;
es gibt auch keine Abkürzung „expliziter plain-Typ holt plain zurück“, `v i64 = a[i]` muss ebenfalls explizit konsumiert werden):

**1. Lesen — match-Unpacking**

Ein Wert-match holt direkt den Wert, ein Anweisungs-match weist im `ok`-Arm zu. `-> _` ist der Arm zum „Verwerfen“,
in Wertkontext liefert er den **Nullwert des Elementtyps** (i64→0, str→'', byte→0, Container→leerer Container).

```no
; Wert-match
x = state[5]: {
    ok -> it

    -> _
}

; Anweisungs-match
state[5]: {
    ok -> x = it

    -> _
}
```

**2. Schreiben — place-match**

`it` ist im `ok`-Arm die **Elementposition** (lesbar und schreibbar), keine Wertkopie.

```no
state[5]: {
    ok -> it = x

    -> _
}
```

> **map-Ausnahme:** Das Schreiben `m['k'] = 1` **braucht kein Unpacking** — der Index von map ist ein Schlüssel, das Schreiben ist put/insert,
> fehlende Schlüssel werden eingefügt, vorhandene überschrieben, immer definiert, es gibt keinen „Out-of-Bounds-Write“ zu bewachen. Aber **das Lesen `m['k']` aus map liefert weiterhin `?V` und muss ausgepackt werden**.
> `m[k] ?= v` ist ein **Kompilierungsfehler** (das von `get` gelesene `?V` ist nur eine Kopie, Zurückschreiben erreicht die map nicht), schreibe direkt `m[k] = v`.

**3. Kurzform `?=` — Fehler hochwerfen**

`x ?= state[5]` (lesen), `state[5] ?= x` (schreiben) sind Kurzformen der beiden obigen match-Formen: Bei Fehlschlag wird die ganze option
nach oben geworfen. Sie ist in **einem von zwei Fällen** zulässig:

- Die umgebende Funktion hat einen `?T`-Ergebnisparameter — bei Fehlschlag wird in den Ergebnisparameter geschrieben und vorzeitig `return` ausgeführt;
- Die Anweisung steht im Körper eines **Block-match auf Anweisungsebene** `{ ... }: { ... }` — sie wird vom nächstgelegenen Block-match abgefangen,
  der Körper stoppt, die Arme laufen nur einmal, die Funktion **braucht keinen** `?T`-Ergebnisparameter.

Trifft keines von beiden zu, ist es ein Kompilierungsfehler. Daher ist die Standardlösung für „der Rückgabewert ist kein option, ich will aber die Kurzform nutzen“, den Funktionskörper in einen Block-match zu packen:

```no
handle = (x i64) {
    {
        y ?= x + 1
        print(1)
    }: {
        err -> _

        -> _
    }
}
```

**Verschachteltes Lesen**: Der innere match bindet `it` neu, daher im Inneren die benannte Bindung `ok(it1) -> it1`. Die genaue Form, die du angegeben hast
(Wert-match direkt rechts von `it = ...` im place-match), schreibt seit 2026-11-06 **korrekt zurück in den Container**: Der Compiler hebt den Wert-match beim
Lowering in ein synthetisches Temporär und bindet `it` neu, Regression siehe Abschnitt `place-write-value-match` in `tests/match.no`.
**Bevorzugte direkte Schreibweise**: Das Ergebnis des Wert-match direkt an den Namen binden, der am Ende gebraucht wird,
nicht erst in ein Einweg-Temporär stecken und dann noch einmal kopieren.

```no
#{fmt-skip}
; ✅ direkt an die Zielvariable binden
at = argt[i]: {
    ok -> it

    -> _
}
s.farg.push(at)

; ❌ für eine einmalige Verwendung kein Einweg-Temporär einführen
ixout1 = argt[i]: { ok -> it  -> _ }
at = ixout1
s.farg.push(at)
```

„Den Wert-match in ein benanntes Temporär außerhalb des Arms zu ziehen und im Arm dann `it = ixout1` zu schreiben“ ist **nicht äquivalent** zur direkten Schreibweise,
keine frei austauschbare Formatierungsfrage: Der Auswertungszeitpunkt unterscheidet sich (nach dem Herausziehen liest auch der äußere `nil` / `err` / catch-all einmal);
`it` ist nur im Arm gültig und zeigt auf die Elementposition, das Zurückkopieren muss im Arm bleiben; das benannte Temporär ist eine neue Bindung mit eigener move-/
Eigentümergeschichte und Zerstörung beim Verlassen des Gültigkeitsbereichs; und bei Verschachtelung muss innen zwingend `ok(it1) -> it1` benannt werden, nicht das
`ok -> it` vom äußeren Arm kopiert werden. Nur wenn derselbe Wert wirklich an mehreren Stellen wiederverwendet wird, heraufziehen — und dann mit einem aussagekräftigen Variablennamen, keine Platzhalternamen wie `ixout…`
aus Migrationswerkzeugen stehen lassen.

#### `?=` und Block-match für die restlichen Einweg-Temporäre

Das oben galt für „bei einmaliger Verwendung direkt binden“. Ein Schritt weiter: Sichere Indexierung liefert ein `?T`, **also wenn der
Ergebnisparameter der Funktion selbst `?T` ist, muss man nicht einmal einen Wert-match schreiben, `?=` wirft in einer Zeile hoch**:

```no
#{fmt-skip}
; ✅ Funktion hat ?T-Ergebnisparameter: kein Temporär, kein match
at ?= argt[i]
s.farg.push(at)
```

**Wenn das Funktionsergebnis kein option ist** (am häufigsten: Hilfsfunktionen ohne Rückgabewert oder die nur in einen Container zurückschreiben), packt man diese Anweisung
in die folgende „try-ähnliche“ Schreibweise:

```no
#{fmt-skip}
handle = (x i64) {
    {
        y ?= x + 1
        print(y)
    }: {
        err -> _

        -> _
    }
    print('continues afterwards')
}
```

⚠️ **Das ist keine neue `try`-Syntax, es gibt kein `try`-Schlüsselwort in der Sprache.** Es ist einfach ein **Block-match auf Anweisungsebene**
(`{ ... }: { ... }`) mit dem tri-state `?T` als Subjekt; `?=` wirft den Fehler an den **nächstgelegenen** Block-match hoch
(siehe〈`?=`-Fehler hochwerfen〉 und〈Die drei Formen des match-Subjekts〉). Bei Fehlschlag werden die restlichen Anweisungen im Blockkörper — einschließlich
nachfolgender Schleifeniterationen — übersprungen, die Arme laufen einmal, und es geht **mit dem Code nach dem Block weiter**, nicht um ein Funktions-
`return`. `err -> _` verwirft den Fehler, `-> _` verwirft das Erfolgsergebnis; um den Fehler zu behandeln, schreibe
`err -> print(it.msg())`.

⚠️ **Der semantische Unterschied muss klar gesagt werden** (empirisch, nicht nach Gefühl): Bei `?=`-Fehlschlag **wird das Ziel trotzdem mit dem Nullwert beschrieben** — `i64` ergibt
`0`, `str` `''`, `out[1]` `0`. Der Auffang-Arm `-> _` des Wert-Position-match **schreibt dagegen nicht ins Ziel**: Bei **neuer Deklaration** ist es ohnehin nullinitialisiert, sieht also konsistent aus;
bei einem Ziel **mit bereits vorhandenem Wert** weichen die beiden ab (`?=` nullt, `-> _` behält den alten Wert, zum Nullen muss man `-> 0` schreiben). Ein weiterer Unterschied ist die Kontrollfluss-Kaskade: `?=` **überspringt die Anweisungen nach sich im Blockkörper**
(in `?T`-Funktionen ein vorzeitiges `return`, in einem Block-match direkter Sprung zum Arm).
Also: **Wenn der Blockkörper nur diese eine Anweisung enthält und der alte Wert vor dem Fehlschlag keine Bedeutung hat**, ist der Auffang-Arm als `-> 0` vollständig

äquivalent (genau die Form, in der dieser Repo-Batch umgeschrieben wurde, die `ixout…`-Temporäre sind meist neue Deklarationen); stehen im Körper nach dem `?=` aber noch
andere Anweisungen (Schleifenzähler, nachfolgende Zuweisungen) oder hat der alte Wert eine Bedeutung, weicht es ab — solche Stellen lassen sich nicht mechanisch ersetzen. Die vollständige
Abwägungstabelle steht in〈Abwägung: Block-match vs. Wert-Position-match〉.
Ein weiterer häufiger Stolperpunkt: **Der Erfolgs-Arm läuft ebenfalls einmal**, der catch-all erhält den Erfolgswert (`-> print('x')` gibt aus).

Daher sollte im Arm nur verworfen oder rein verarbeitet werden; Formen wie `err -> _` / `-> _` haben keine Seiteneffekte.
Daraus ergibt sich diese Reihenfolge zum Aufräumen der `ixout…`-Einweg-Temporäre:

1. Wert wird nur an einer Stelle einmal verwendet → direkt an den finalen Zielnamen binden (siehe oben).

2. Funktion hat einen `?T`-Ergebnisparameter → `TARGET ?= EXPR`, kein Temporär nötig.
3. Andernfalls → die Anweisung in einen Block-match packen und `TARGET ?= EXPR` schreiben; place-match-Zurückschreiben
   (`SUBJ: { ok -> it = temp  -> _ }`) reduziert sich zu `SUBJ ?= EXPR`.
4. Ein benanntes Temporär nur behalten, wenn derselbe Wert wirklich mehrfach gebraucht wird — mit aussagekräftigem Namen.
Zwei Grenzen: `?=` ist ein Kompilierungsfehler, wenn weder ein `?T`-Ergebnisparameter vorhanden ist noch die Anweisung im Körper eines Block-match steht

(also `l86f91me`); **eine LHS mit Typdeklaration darf nicht mit `?=` kombiniert werden** (`ai i64 ?= a[i]` ist ein Parsingfehler
(`l86f91me`); **eine LHS mit Typdeklaration darf nicht mit `?=` kombiniert werden** (`ai i64 ?= a[i]` ist ein Parsingfehler
`yh8904fm`, `expected assignment operator`). Für eine explizite Höherstufung wie `byte → i64` verwende einen direkten Wert-match mit Typdeklaration:

```no
#{fmt-skip}
ai i64 = a[i]: {
    ok -> it

    -> _
}
```

> **Gültigkeitsbereich:** `arr` / `vec` / `slice` / Arrays fester Länge / `str` / `txt` sind alle betroffen; nur die Indexierung mit **direkter Variablenbasis**
> (`v[i]`) wird in die sichere Form umgeschrieben, `receiver.field[i]` läuft über den ursprünglichen Bounds-Check-Pfad.
> Indexlesen in Vergleichsoperanden vergleicht das Payload (`a[i] == 5`), nicht das ganze `{tag, payload}`.

> ⛔ **Harter Fehler:** `?T` als gewöhnlichen Wert zu verwenden (wie `crc = crc ^ s[i]`, `f(s[i])`, `v i64 = a[i]`) blockiert
> `no build`, der Compiler rät zu match oder `?=` (ohne `?T`-Ergebnisparameter in einen Block-match packen).
> Bestehendes Korpus wurde mit `no fmt --fix=index-out` einmalig migriert, das Werkzeug wurde mit D11 außer Dienst gestellt.

> **Kernzusage:** Bereichsüberschreitung stürzt nie still ab — sie wird entweder per match behandelt oder per `?=` hochgeworfen.

> **Indexlesen in Vergleichsoperanden** unterliegt ebenfalls der sicheren Indexierung: `a[i] == 5` vergleicht das Payload (dieselbe Regel wie `?T == 5`),
> nicht das ganze `{tag, payload}`. Bei Bereichsüberschreitung wird `err` gelesen (`code.index-out`), der Vergleich mit irgendeinem Wert ist dann **falsch**
> (`a[i] == 0` und `a[i] == 5` sind beide falsch), und es wird **kein** Speicher außerhalb des Bereichs gelesen.
> `s[0] == 'a'` analog: Beim Vergleich eines Codepoints mit einem ein-Zeichen-String greift die implizite Konvertierung `char → str`.

> **Verschachtelte Indizes** `a[i][j]`: **Beide Ebenen sind sicher indexiert**. Die äußere Ebene `a[i]` prüft zuerst die Grenzen und ob das Element existiert,
> erst dann wird `[j]` darauf angewendet; ist eine der beiden Ebenen außerhalb des Bereichs, ist der ganze Ausdruck `err`.
> Daher ist `a[oob][0] == 'c'` falsch, ohne abzubrechen.

> **Container-Vergleich** `[]T == []T` (sowie `[N]T == [N]T`) ist ein **struktureller Vergleich**: zuerst die Länge, dann Element für Element.
> Sind die Elemente selbst Container (`[][]i64`), wird rekursiv verglichen; sind sie `str`, wird der **Inhalt** und nicht der data-Zeiger verglichen.
> Ordnungsvergleiche (`<` / `<=` / `>` / `>=`) sind für Container nicht definiert, nur `==` / `!=` werden unterstützt.

> **`nil` darf nur mit option verglichen werden**: `?T == nil` / `?T != nil` ist erlaubt; bei Nicht-option-Typen ist
> `T == nil` (`str`, `[]i64`, `[N]T`, `txt`, `i64`, Strukturen …) ein **Kompilierungsfehler** (`q4nilcmp`).
> Der Grund ist keine Pedanterie: `str == nil` ist immer false (die `%str-long`-Struktur wird mit 0 verglichen),
> `[]i64 == nil` fällt in den Integer-Vergleichspfad und erzeugt illegales LLVM (`icmp eq i64 %vec, i64`),
> beide geben still falsche Antworten. Um „möglicherweise kein Wert“ auszudrücken, deklariere den Typ als `?T` und vergleiche dann mit `nil`.

## Strukturen

Sowohl Strukturdefinitionen als auch Literale müssen die mehrzeilige Form verwenden, jedes Feld steht in einer eigenen Zeile, Felder werden nicht durch Kommas getrennt und auch am Ende folgt kein Komma.

```no
user {
    name str
    age i64
}

u = user {
    name: 'Alice'
    age: 30
}
u.name = 'Bob'
u.age = 25
print(u.name)
```

### Strukturen implementieren Interfaces

Eine Struktur kann ein oder mehrere Interfaces implementieren; die Interface-Namen stehen hinter dem Strukturnamen, getrennt durch Kommas.

```no
; ein einzelnes Interface implementieren
user json {
    name str
    age i64
}

; mehrere Interfaces implementieren
file enter, leave {
    path str
    fd i64
}
```

#### Interface-Implementierung über Module hinweg

Bei der Implementierung eines von **anderen Modulen** definierten Interfaces muss der Interface-Name den Modul-Präfix tragen (`ShortName.`). Siehe [Cross-Modul-Aufrufpräfix](module.md).

```no
#{fmt-skip}
; ❌ Fehler: db, rows, stmt sind Interfaces des sql-Moduls, der Präfix darf nicht weggelassen werden
db-mysql db {
    fd i64
}
```
```

```no
; ✅ richtig: sql.db, sql.rows, sql.stmt verwenden
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

> Eingebaute Interfaces (`enter`, `leave`) und Interfaces aus derselben Datei brauchen keinen Modul-Präfix.

## Methoden

Methoden werden auf einem Typ definiert und referenzieren den Empfänger (receiver) mit `.`.

### Syntax

```no
#{fmt-skip}
type.method-name = (params) (results) {
    ; . ist der Empfänger
}
```

### Regeln

1. Methodennamen haben das Format `type.method`, type muss ein bereits definierter Typ sein
2. Der Empfänger muss nicht explizit als Parameter deklariert werden, im Methodenkörper wird er mit `.` referenziert
3. Der Aufruf erfolgt mit der Syntax `receiver.method(args)`
4. Rückgabewerte stehen in der zweiten Klammergruppe, wie bei normalen Funktionen
5. **Syntaxzucker-Desugaring**: `type.method = (inputs) (rest-outputs...) {}` entspricht der normalen Funktion  
   `method = (inputs) (self type, rest-outputs...) {}`. Beim Aufruf `instance.method(args)`  
   wird instance an den ersten Parameter der Ausgabeliste `self` gebunden; im Funktionskörper bezeichnet `.` das `self` im Ausgabebereich;  
   Schreibzugriffe der Funktion auf `self` verändern die Originalinstanz direkt. Die übrigen Ausgaben außer `self`  
   unterstützen optionales Destrukturierungs-Empfangen oder können einfach verworfen werden.

### Beispiel

```no
; str-Methode
str.to-upper = () (out str) {
    n = .len-bytes()
    b []byte = with-cap-len(n, n)
    i <- [0..n): {
        c ?byte = .byte(i)
        v = c: {
            ok -> it

            nil -> 0

            err -> 0
        }
        {
            v >= 97 && v <= 122 -> b[i] = v - 32

            -> b[i] = v
        }
    }
    out = b.to-bytes-str()
}

; char-Methode
char.is-digit = () (result bool) {
    result = false
    . >= 48 && . <= 57 -> result = true
}

; struct-Methode
user {
    name str
    age i64
}

user.greet = () {
    print('Hello, ' - .name)
}

; Aufruf
s = 'hello'
u = s.to-upper(); receiver.method()
c char = 5
d = c.is-digit(); receiver.method()
u = user {
    name: 'Alice'
    age: 30
}
u.greet()
```

## Interfaces

```no
; Interface definieren
json {
    to-json()
}

; Standardimplementierung des Interface
json.to-json = () {
}

noml {
    to-noml()
}

serialize {
    json
    noml
}

serialize = json | noml
serialize = json & noml

; Interface-Implementierung
user json {
    name str
    age i64
}

; überschreiben + Parent-Implementierung aufrufen
user.to-json = () {

    ; Parent-Implementierung
    ..to-json()
}

user.other = () {

    ; aktuelle Implementierung
    .to-json()

    ; Parent-Implementierung
    ..to-json()
}
```

### Interface-Zusammenführung

Ein Interface kann die Methoden anderer Interfaces „zusammenführen“: Die Namen der Eltern-Interfaces stehen im Interface-Körper, **einer pro Zeile** (mit Modul-Präfix möglich), ohne Komma und ohne Klammern:

```no
json {
    to-json()
}
noml {
    to-noml()
}

serialize {
    json
    noml
}

; transitive Zusammenführung: roundtrip erhält alle Methoden von serialize, json und noml
roundtrip {
    serialize
    roundtrip-json()
}
```

Regeln:

- **Rekursive Zusammenführung**: Die Zusammenführung ist transitiv — führt `roundtrip` `serialize` zusammen und dieses wieder `json` / `noml`, dann besitzt `roundtrip` die Methoden aller drei Interfaces.
- **Einer pro Zeile, kein Komma**: `serialize { json, noml }` hat dieselbe Form wie eine C-Style-Enumeration (`color { red, green }`), wird also weiterhin als Enumeration geparst, nicht als Interface-Zusammenführung.
- **Gleichnamige Methoden**: Gleiche Signatur (Parameter und Rückgabetyp identisch, Empfängername darf abweichen) ⇒ Duplikat entfernen, eine behalten; unterschiedliche Signaturen ⇒ Diagnosefehler.
- **Implementierungsseite**: Der Typ braucht nur die Implementierungsdeklaration zu schreiben (vorangestellte Liste `user serialize { name str }`), die Methoden der Eltern-Interfaces zählen ebenfalls als implementiert, die Signaturprüfung greift durch die Eltern-Interfaces hindurch.
- **`no fmt`**: Die alte Form mit „vorangestellter Liste“ (`serialize json, noml { }`) lässt sich weiterhin parsen, aber `no fmt` schreibt sie in die Körperform um, `no vet` gibt dafür eine WARNING aus.

Diagnosen (`no vet` und Editor-Diagnosen aus derselben Quelle, TraceID `ifmrg001`):

| Situation | Meldung 
| --- | --- |
| Eltern-Interface nicht deklariert | `interface 'serialize': unknown parent interface 'noml'` 
| Zyklische Zusammenführung | `interface 'a': merge cycle a -> b -> a` 
| Gleicher Name, unterschiedliche Signatur | `interface 'serialize': merged method 'to-str' has conflicting signatures ('json.to-str', 'noml.to-str')` 

> Schreibweisen wie `serialize = json \| noml` sind eine **Vereinigungstyp**-Syntax (union type) und haben mit Interface-Zusammenführung nichts zu tun; diese beeinflusst nur, „welche Methoden ein Typ besitzen muss“.

### Spezielle Interfaces

```no
file {
    enter
    leave
}
```

## Enumerationen

```no
; red=0, green=1, blue=2
color {
    red,
    green,
    blue,
}

; Bei normalen Methoden sind a,b,c tatsächlich die definierten a=0, b=1, c=2... Das weicht von anderen Sprachen ab.
; daher kann man normalerweise nicht mehrere Variablen mit Kommas definieren

; dies ist eine spezielle Enumeration, sie darf Typen, Kommas und Aliase haben
enum-name {
    a t,
    b u,
    c v,
}

; beachte: das ist ein gewöhnliches struct, mehrere Felder ohne Kommas
struct-name {
    a t
    b u
    c v
}
```

### Tagged Enum (mit Payload)

Varianten können benannte Payload-Felder tragen, geschrieben in Klammern als `Variante(Feld Typ)`; Varianten ohne Payload stehen direkt als nackter Name.  
Eine Variante kann **mehrere** Felder haben (Mehrfeld-Payload wird auf dem Heap als synthetisches struct dargestellt):

```no
#{fmt-skip}
; Tagged Enum: Varianten können Payload tragen, tag folgt der Deklarationsreihenfolge 0,1,2...
result {
    ok(v t),
    nil,
    err(e str),
}

; mit Wert: Payload-Feld v, Typ t
; ohne Wert: kein Payload
; Fehler: Payload-Feld e, Typ str (benutzerdefinierte Enumeration, nichts mit eingebautem option zu tun)

shape {
    circle(r f64),
    rect(w f64, h f64),
    dot,
}
; ein Feld
; mehrere Felder
; kein Payload
```
```

Beim Matching dient der Variantenname als Arm-Muster; Varianten mit Payload können das Payload per Destrukturierung binden (Mehrfachfelder werden positionsweise eins zu eins gebunden):

```no
r result = ok(42)
r: {
    ok(v) -> print(v); Payload von ok an v binden

    nil -> print('empty')

    err(e) -> print(e)
}

s shape = rect(3.0, 4.0)
s: {
    circle(r) -> print(r); ein Feld binden

    rect(w, h) -> print(w * h); mehrere Felder positionsweise binden

    dot -> print('dot')
}
```

Varianten können in **Expression-Position** direkt konstruiert werden, als Funktionsargument oder als Payload einer anderen Variante:

```no
x f64 = perimeter(rect(3.0, 4.0)); inline konstruieren und übergeben
o outer = wrap(a(5)); Payload der Enumeration ist selbst eine Enumeration
```

> **Namensraum und Auflösung nackter Namen**: Der Compiler registriert Varianten intern unter dem „voll qualifizierten Namen“ (`Modul.Enum.Variante`,  
> wie `option.option.ok` oder `some-mod.my-result.ok`), gleichnamige Varianten verschiedener Enumerationen kollidieren nicht.  
> Der Benutzer schreibt nur den nackten Namen (`ok`, `rect`), der Compiler vervollständigt ihn **anhand des statischen Typs der gematchten/konstruierten Variable**  
> zum vollständigen Namen. Daher lassen sich zwei Enumerationen mit `ok`-Variante und unterschiedlicher tag-Reihenfolge trotzdem korrekt unterscheiden.
>
> Die alte, durch Leerzeichen getrennte Form `ok t` ist ebenfalls erlaubt und entspricht `ok(v t)` (Feldname weggelassen).  
> Varianten von Tagged Enums werden in die Enum-Varianten-Tabelle des Compilers eingetragen für Matching und Erschöpfheitsprüfung.

### Eingebaute Tagged Enums (`#{buildin}`)

Ein `#{buildin}` vor einer Tagged Enum kennzeichnet diese als **eingebaute Enumeration**: Die Varianten (Namen, Reihenfolge, Payload-Typen) dienen  
dem Matching und der Erschöpfheitsprüfung, die zugrunde liegende Darstellung und Konstruktion liefert jedoch die runtime/builtin, **es wird keine  
benutzersichtbare struct/union erzeugt**. Das `?t`-option wird auf diese Weise deklariert:

```no
; src/std/global.no
#{buildin}
option {
    ok(v t),
    nil,
    err(e err.err),
}
```

Die tags von `option` sind der Reihe nach `0=ok`, `1=nil`, `2=err`, passend zur eingebauten option-Darstellung. Benutzer müssen  
option nicht selbst deklarieren; das `#{buildin}` hier sorgt dafür, dass die Varianten der eingebauten option eine einzige Quelle haben (siehe „Nullable-Typ (option)“).

### Enum-Annotationen und Speicherlayout

Enumerationen unterstützen `#{...}`-Annotationen an denselben Positionen wie andere Ziele (siehe〈Annotationsposition〉): **darüber** (eigene Zeile) oder  
**danach** (am Zeilenende); direkt **davor auf derselben Zeile** ist ein Kompilierungsfehler.

Annotationen können an **zwei Stellen** stehen:

1. **über der gesamten enum** — wirkt auf die Enum-Definition selbst (wie `#{buildin}`, `#{inline=true}`);
2. **über einem einzelnen Mitglied oder am Zeilenende** — wirkt auf dieses Mitglied (sowohl „Werte“ von C-Style-Enumerationen als auch „Varianten“ von Tagged Enums).

```no
; ① gesamte enum: eigene Zeile über der enum
#{inline=true}
box {
    full(v i64) #{doc='mit Wert'},
    empty #{doc='ohne Wert'},
}

; ② einzelne Variante: eigene Zeile über der Variante

; ② einzelne Variante: am Zeilenende

; `#{inline=false}`: explizit „kein inline-Layout“ angeben
#{inline=false}
plain {
    a,
    b,
}

color {
    red #{deprecated},
    green #{deprecated},
    blue,
}
; einzelne Werte von C-Style-Enumerationen unterstützen ebenfalls beide Formen
```

**Annotationen verändern die Struktur der enum nicht**: Variantenname, Deklarationsreihenfolge (tag), Payload-Felder und -Typen bleiben unberührt,  
Matching und Erschöpfheitsprüfung sind ebenfalls völlig unverändert. Annotationen sind lediglich **Metadaten**, die an der Definition hängen.

#### Speicherlayout (Stack-Form)

Tagged Enums verwenden im Speicher ein **Stack-Layout** (inline stack form) — ein kleines Objekt nach Wert, das Payload liegt nicht verstreut auf dem Heap:

```llvm
%tenum_<name> = type { iN tag, [K x i64] payload }   ; allgemeiner Fall
%tenum_<name> = type { iN tag }                      ; alle Varianten ohne Payload
```

- Die Breite von `tag` richtet sich nach der **Anzahl der Varianten** und ist ein vollständiges `u8/u16/u32/u64`:  
  `<= 256` → `i8`, `<= 65536` → `i16`, `< 2^32` → `i32`, sonst `i64`. Der Diskriminator folgt der Deklarationsreihenfolge `0, 1, 2...`.
- `payload` ist eine von **allen Varianten gemeinsam genutzte** Union (jede Variante schreibt ihre Felder in denselben Speicherbereich); `K` ist die  
  Anzahl der 8-Byte-Wörter, die die **breiteste Variante** benötigt, **nicht die Summe aller Varianten**, mindestens 1.
- Felder innerhalb einer Variante liegen nach **natürlicher Ausrichtung**, daher sind Feld-Offsets nicht unbedingt Vielfache von 8 (bei `(a char, b char, c char)`  
  sind die Offsets 0 / 4 / 8). Lese- und Schreibzugriff adressieren immer über **Byte-Offset** (`bitcast` auf `i8*`, dann `getelementptr`),  
  daher werden „geschriebener Offset“ und „gelesener Offset“ von derselben Layout-Tabelle bestimmt.

Die Größe ist `8 + 8K` (`tag` wird durch die 8-Byte-Ausrichtung des Arrays auf ein ganzes Wort aufgefüllt), nur Enumerationen ohne Payload haben reine Tag-Größe:

| Fall | `K` | Größe 
| ---------------------------------------------- | ----------------- | ---------- |
| alle Varianten ohne Payload | — (kein payload-Feld) | **1 Byte** (`{ i8 }`; bei > 256 Varianten 2 / 4 / 8) 
| breiteste Variante `i64` / `i32` / `f64` / Zeiger | 1 | 16 Bytes 
| breiteste Variante `char` (4 Bytes) / `bool` (1 Byte) | 1 | 16 Bytes 
| breiteste Variante `(a char, b char, c char)` (12 Bytes) | 2 | 24 Bytes 
| breiteste Variante `str` / `vec` / `[]T` (16-Byte-Deskriptor) | 2 | 24 Bytes 
| breiteste Variante Struktur `T` | `ceil(sizeof(T) / 8)` | `8 + 8K` 

Das heißt: **Mindestens 16 Bytes ab Werk**, es wächst nur, wenn der Payload einer Variante tatsächlich breiter ist —  
„standardmäßig fest 16 Bytes“ und „Größe variiert mit den Varianten“ sind zwei Seiten derselben Sache. Kleine Payloads wie `char` / `bool`  
bestimmen nur die Ausrichtung, sie blähen die Union nicht auf: Die Breite von `payload` ist „die breiteste Variante“, nicht die Summe aller.

> Die einzige Quelle des Layouts ist `src/mir/layout.go`: `tagWidth` bestimmt `iN`, `enumVariantLayout` bestimmt  
> die Feld-Offsets und die eigene Größe jeder Variante, `enumUnionLayout` nimmt das Maximum, um `K` zu erhalten. `hir2mir.fillEnumLayout`  
> schreibt diese Zahlen beim Tabellaufbau in `TaggedEnumInfo`, der emitter liest nur und rechnet nicht — beide Seiten können unmöglich zu verschiedenen Ergebnissen kommen.

#### Die drei Schreibweisen von `#{inline}`

`inline` ist eine **boolesche** Annotation mit drei Schreibweisen, und **der Wert wird tatsächlich befolgt**:

| Schreibweise                | Bedeutung                          
| ----------------- | --------------------------- |
| `#{inline}`       | Kurzform, entspricht `inline=true` (alleinstehender Schlüssel ist wahr) 
| `#{inline=true}`  | explizit wahr (`#{inline=1}` ebenfalls)      
| `#{inline=false}` | explizit falsch (`#{inline=0}` ebenfalls)      

Nicht-boolesche Werte sind **Kompilierungsfehler** (`#{inline=foo}`, `#{inline='true'}`) und werden nicht still als wahr behandelt:  
`ValidateFieldTags` (TraceID `fieldtag1`) meldet das sowohl für Strukturfelder als auch für enum-Definitionen.

Über der gesamten enum geschriebenes `#{inline=true}` ist die **explizite Markierung** für Stack-Layout, bedeutet also „diese Enumeration verwendet Stack-Layout“;  
der Compiler vermerkt es in `TaggedEnumInfo.Inline`. **Das Standardlayout ist derzeit bereits Stack-Form**, daher sind  
Annotation oder nicht, `true` oder `false` – das Layout ist identisch — die Markierung macht nur die Absicht deutlich und dient später, falls sich das Standardlayout ändert,
als stabiler Umschalter.

> `#{inline}` **über der gesamten enum** bedeutet „diese Enumeration verwendet Stack-Layout“; auf einer **einzelnen Variante** ändert es  
> derzeit nichts am Layout und bleibt nur als Metadaten für diese Variante erhalten (anders als bei Strukturfeldern, siehe nächsten Abschnitt).

#### `#{inline}` auf Strukturfeldern

Anders als bei enums hat `#{inline}` auf **Strukturfeldern** eine **tatsächliche Wirkung**: Es bestimmt direkt, ob das Feld  
**by value im Host-struct inline liegt** (`%pt`) oder als **Zeiger** (`ptr`) gespeichert wird.

```no
pt {
    x i64
    y i64
}

holder {
    inl pt #{inline=true}; nach Wert: struct wächst direkt in holder
    out pt #{inline=false}; Zeiger: Feld speichert einen Zeiger auf pt
    bare pt #{inline}; Kurzform, entspricht inline=true
}
```

| Schreibweise                                             | Feld-Layout                        
| ---------------------------------------------- | --------------------------- |
| `#{inline}` / `#{inline=true}` / `#{inline=1}` | **nach Wert** inline im Host (`%pt`)         
| `#{inline=false}` / `#{inline=0}`              | **Zeiger** (`ptr`)               
| nicht geschrieben                                             | abhängig von `NOLANG_FIELD_PTR` (siehe unten) 
| `#{inline=foo}` / `#{inline='true'}`           | **Kompilierungsfehler** — kein Boolean              

**Ohne Annotation wird das Standardlayout von `NOLANG_FIELD_PTR` bestimmt.** `mir.FieldPtrLayout` entspricht  
`os.Getenv("NOLANG_FIELD_PTR") != ""`, **standardmäßig aus**; das Einschalten aktiviert den „Phase-1-Layout-Flip“,  
der den Standard für Strukturfelder von nach-Wert auf Zeiger umstellt.

| Feld                             | Flag aus (Standard)   | Flag an (`NOLANG_FIELD_PTR=1`) 
| ------------------------------ | --------- | ------------------------- |
| ohne Annotation                            | `%pt` (nach Wert) | `ptr` (Zeiger)                 
| `#{inline}` / `#{inline=true}` | `%pt`     | `%pt`                     |
| `#{inline=false}`              | `ptr`     | `ptr`                     |

Das heißt: **Annotationen wirken immer, unabhängig von dieser Umgebungsvariablen**, das Flag ändert nur den Standard „ohne Annotation“. Daher ist  
im Standard-Aufbau `#{inline=false}` eine „aktive Anforderung für einen Zeiger“, während `#{inline=true}` nur  
das ohnehin schon geltende Layout nochmals hinschreibt; wird der Flip aktiviert, tauschen die beiden ihre Rollen.

Der Checker hat zwei Regeln (TraceID `fieldtag1`, `ValidateFieldTags`):

1. **Nur für Felder von Struktur-Typen sinnvoll.** Skalare / `str` / `vec` / Arrays / Slices / map / option /  
   Zeigerfelder werden ohnehin immer nach Wert gespeichert, `#{inline}` zu schreiben sagt also nichts aus und führt zum Fehler;  
   `#{inline=false}` meldet ebenfalls einen Fehler, denn das behauptet ein zum Compiler-Verhalten gegenteiliges Layout.
2. **Inline-nach-Wert darf keinen Zyklus bilden.** Ein inlines gespeicherter struct liegt **im Inneren** des Hosts, daher sind  
   `a { #{inline} b b }` und `b { #{inline} a a }` zusammen unendlich groß und müssen einen Fehler melden (direkte Selbstreferenz ist ein degenerierter Fall). Nur `=true` erzeugt eine nach-Wert-Kante, daher sind sich gegenseitig rekursive Strukturen sind bei **beidseitigem `#{inline=false}`** zulässig.
   Verweis ist ein degenerierter Fall). Nur `=true` erzeugt eine nach-Wert-Kante, daher sind sich gegenseitig rekursive Strukturen bei **beidseitigem
   `#{inline=false}`** zulässig.
   > ⚠️ **„Keine Annotation schreiben“ heißt nicht „kein inline“.** Im Standard-Aufbau bedeutet das Fehlen der Annotation Inline-nach-Wert, daher sind  
   > `a { x b }` mit `b { y a }` (beide ohne Annotation) im **Standard-Aufbau nicht zulässig** und scheitern mit  
   > `opt: error: identified structure type 'b' is recursive` — und dieser Fehler wird derzeit  
   > **nicht** vom Checker gemeldet, sondern erst in der LLVM-Verifizierungsphase hochgesprengt. Um im Standard-Aufbau rekursive Typen zu schreiben, **muss** man  
   > explizit `#{inline=false}` angeben. Nur bei `NOLANG_FIELD_PTR=1` bedeutet „nicht schreiben“ einen Zeiger.

#### Rekursive Typen: warum `inline=false` nötig ist

Ein struct, der sich selbst nach Wert inline speichert, ist unendlich groß, daher **darf die Selbstreferenz nicht nach Wert gespeichert werden**. Im Standard-Aufbau  
(`NOLANG_FIELD_PTR` aus) sind Strukturfelder standardmäßig nach Wert, damit `node` sich selbst referenzieren kann,  
bleibt nur die Möglichkeit, explizit einen Zeiger zu fordern:

```no
node {
    v i64
    next node #{inline=false}; Zeiger: nur so hat node eine endliche Größe
}

n node
n.v = 1
n.next.v = 2
print(n.next.v); 2
```

Entfernt man das obige `#{inline=false}` (oder schreibt `#{inline=true}`), lehnt LLVM den Typ direkt ab:  
`identified structure type 'node' is recursive`.

Ein Zeigerfeld **besitzt** seinen pointee, die Lebensdauer wird vom Compiler verwaltet:

- Der pointee wird **bei der ersten Schreibung lazy allokiert** (`@__nolang_get_<T>`, enthält `malloc` + Nullsetzen),  
  nicht schon beim Konstruieren des Hosts;
- beim Verlassen des Gültigkeitsbereichs des Hosts gibt `@__nolang_drop_<T>` ihn **nach Nullzeiger-Prüfung frei**;
- Beim Kopieren nach Wert (`b = a`, als Argument übergeben, in Container schreiben) wird der pointee **tief kopiert**, daher teilen die beiden Hosts  
  weder denselben Speicherbereich noch werden sie doppelt freigegeben.

Der End-to-End-Test steht in `tests/field-inline-annotation.no`.

### Enum-Wert-Referenzen

**Regel: Enum-Werte müssen qualifiziert als `EnumTyp.Wert` referenziert werden, ein nackter Wert ist nicht erlaubt.**  
Das verhindert Namenskonflikte und hält externe Pakete davon ab, konkrete Werte direkt zu verwenden.

```no
#{fmt-skip}
; ❌ Fehler: nackten Wert direkt verwenden
kind = null
yes = e.is(io)
```
```

```no
; ✅ richtig: qualifizierte Form verwenden
kind = json-kind.null
yes = e.is(code.io)
```

> Enum-Typen können als Struktur-Feldtyp, Funktionsparametertyp und Rückgabetyp verwendet werden.  
> Sowohl innerhalb als auch außerhalb des Moduls, das die Enum definiert, sollten Enum-Werte als `EnumTyp.Wert` referenziert werden.

## enter/leave

Typen, die `enter` / `leave` implementieren, werden beim Eintritt bzw. Austritt in den Gültigkeitsbereich automatisch aufgerufen:

```no
file enter, leave {
    path str
}

file.enter = () {
    .open()
}

file.leave = () {
    .close()
}

read-file = () {

    ; automatisch f.enter()
    f = file {
        path: 'data.txt'
    }

    ; f verwenden
    ; automatisch f.leave()
    read(f)
}
```

### Nullable-Typ (option)

Ein `?` vor einem Typ kennzeichnet einen Nullable-Typ:

Eine Nullable-Variable darf legal einen leeren Wert / Fehlerwert halten, der Compiler führt die entsprechenden Nullprüfungen durch.

`?t` ist eine eingebaute Tagged Enum, ihre Varianten sind in `src/std/global.no` definiert:

```no
#{buildin}
option {
    ok(v t),
    nil,
    err(e err.err),
}
; tag 0: mit Wert
; tag 1: leerer Wert
; tag 2: Fehler (std-Strukturfehler err.err)
```

Die Payload der `err`-Variante ist **kein `str`**, sondern die `err`-Struktur aus `std/err` (`code` / `msg` / `file` / `line` / `col`).
`err('msg', code)` ist ein Alias-Konstruktor für `err.new('msg', code)` — der Fehlercode `code` ist ein **Pflicht**parameter
(Parameter-Defaultwerte wurden aus der Sprache entfernt), ohne konkreten Fehlercode übergib explizit `code.unknown`;
die Positionsfelder werden vom Compiler an der Konstruktionsstelle automatisch injiziert. `err('msg')` mit nur einem Argument ist nicht dieser Alias,
sondern der `err`-Variantenkonstruktor von option (siehe unten).

`#{buildin}` bedeutet, dass die zugrunde liegende Darstellung und Konstruktion von der eingebauten runtime geliefert werden (`%option`-Typ, Konstruktoren `ok(...)` / `nil` / `err(...)`),
die Enum-Deklaration von `option` dient nur Matching und Erschöpfheitsprüfung. Details unter „Tagged Enum“ und „Eingebaute Tagged Enums“.

#### Speicherlayout

`?T` ist **nach Payload typisiert**: Es gibt für jeden Payload-Typ einen eigenen LLVM-Typ, dessen Name sich aus dem Payload-Typ ableitet (injektive Abbildung),
daher gilt „gleicher Payload-Typ ⇒ gleiches Layout“, `?i32` / `?i64` / `?bool` teilen denselben Typ:

```llvm
%option_<key> = type { i8 tag, [K x i64] payload }   ; inline
%option_<key> = type { i8 tag, [1 x i64] payload }   ; geboxt (payload speichert i8*-Zeiger)
```

- `tag` ist **stets 1 Byte**: `option` hat nur 3 Varianten (`0=ok`, `1=nil`, `2=err`) und fällt damit in die Stufe „`<= 256` Varianten → `i8`“.
  Die Tag-Breitenregel von Enumerationen steht in〈Speicherlayout (Stack-Form)〉.
- Auf das Tag folgt **Alignment-Padding** (1-Byte-Tag → 7 Bytes Padding), danach erst der Payload.
- Die Payload-Größe `K` wird von der tatsächlichen Größe von `T` bestimmt und auf ein Vielfaches von 8 aufgerundet, **kein fester Slot**:
  `?i64` ist 16 Bytes (8 + 8), `?str` ist 24 Bytes (8 + 16), `?txt` ist 264 Bytes (8 + 256).
- Payloads über dem Schwellwert werden auf den **Heap geboxt**: `payload` speichert nur einen 8-Byte-Zeiger, das ganze option ist 16 Bytes.

Der Schwellwert `option-payload-inline-threshold` zählt in Bytes über die **gesamte option-Struktur** (Standard **264** = 1 Byte Tag +
7 Bytes Padding + 256 Bytes Payload, Minimum **16**): Die Bedingung für Inline-Payload lautet
`8 + roundUp8(sizeof(T)) <= Schwellwert`. Standardmäßig gilt also „Payload bis 256 Bytes wird inline“, `?txt`
(256 Bytes Inline-Puffer) liegt genau innerhalb der Grenze.

| `T` von `?T` | Inline? | `%option_<key>`-Größe 
| --- | --- | --- |
| `i64` / `i32` / `bool` / `char` / `f64` / Zeiger | ja | 16 Bytes 
| `str` / `[]T` / `vec` (16-Byte-Deskriptor) | ja | 24 Bytes 
| `txt` (256-Byte-Inline-Puffer) | ja | 264 Bytes (genau am Standardlimit) 
| Struktur / Array mit Größe > 256 Bytes | nein | 16 Bytes (Payload auf dem Heap) 
| selbstreferenzielle Payloads (`node { next ?node }`) | nein | 16 Bytes (Selbstreferenz nur per Zeiger auflösbar) 

> **Die Payload von `err` ist immer geboxt**: Die `err`-Variante speichert einen Zeiger auf die `err`-Struktur aus `std/err` und belegt nicht das
> obige Payload-Budget. Daher ist `?str` bei `ok` ein 24-Byte-Inline-String und bei `err` dieselben 24 Bytes
> Struktur — nur das `payload`-Word enthält einen Zeiger auf das Fehlerobjekt. Diese Regel erlaubt es, „ein `?T`-Typ,
> zwei Varianten, zwei Payloads“ ohne zusätzlichen Platz auszukommen.

> **Inline vs. Boxing ändert nur „wo es liegt“, nicht die Ownership**: Beide Pfade legen bei einer **Kopie** eine eigene Kopie des Heap-Anteils der Struktur-Payload an
> (Zeigerfelder rekursiv, inline gehaltene `str` / `[]T`-Puffer geklont), daher ist die Wertesemantik von `?T`
> unabhängig davon, ob inline oder nicht — dasselbe Programm muss bei jedem `option-payload-inline-threshold` identische Ausgaben liefern.
> Ein anderes Verhalten nur wegen eines anderen Schwellwerts wäre ein Compilerfehler, kein Feature. (Reine Move-Zuweisungen klonen nicht, der option übernimmt
> den Heap der Quelle.)

> Die einzige Quelle des Layouts ist `src/mir/layout.go`: `optLayoutOfPayload` bestimmt `K` und ob geboxt wird,
> `optionPayloadOffset` (8) ist der feste Byte-Offset des Payloads. `hir2mir` faltet modulweite `?T`-Konstanten,
> codegen emittiert Lese- und Schreibzugriffe — alle fragen dieselbe Funktion.

#### Cross-Payload-Hochwerfen durch `?=`

`?=` kopiert zuerst „die option an der **Wurzel** der rechten Wertkette“ in den option-Ergebnisparameter, damit `nil` / `err` direkt hochgeworfen werden,
der Erfolgspfad wird anschließend vom `ok`-Arm überschrieben:

```no
pick = (p pool) (out ?i64) {
    out ?= p.nodes[0].kind; Wurzel ist ?node, Ergebnis-Slot ist ?i64
}
```

Die Payload-Typen beider Seiten dürfen daher **verschieden sein**, und das ist eine echte ABI-Grenze (`?node` 112 Bytes, `?i64` 16 Bytes),
keine zwei Schreibweisen desselben Layouts. Über die Typgrenze hinweg mitgenommen werden können nur zwei Dinge:

- `tag` — Byte 0 jeder option;
- `err` — die `err`-Variante ist eine Eigenschaft der option, unabhängig vom Payload, und immer geboxt, daher liegt im Payload-Bereich bei beiden Seiten im Wort 0
  ein 8-Byte-Box-Zeiger (die Box wird **kopiert**, nicht geteilt, weil beide Seiten sie droppen).

Die `ok`-Payload **wandert nicht über die Typgrenze**: Der Payload-Bereich des Ziels wird zuerst genullt, der Erfolgswert wird immer vom `ok`-Arm neu geschrieben.
Die Implementierung liegt in `@__nolang_opt_prop_<dst>__<src>` (`optPropagateState` in `src/mir/codegen.go`),
sie tritt nur bei compilererzeugten Hochwerfe-Zuweisungen wie `?=` auf.

```no
o ?i64
o = nil; auf leer setzen
o = 42; auf Wert setzen
o = err('msg'); auf Fehler setzen

nullableValue ?[]str
nullableString ?str

; Nullable-Typ ändern
nullableString = 'test'

; Fehler setzen
nullableString = err('some error')

; per match prüfbar
x: {
    err -> log(it.msg()); it ist err.err, Nachricht über .msg()

    nil -> log('nil')

    -> do-right-thing(it)
}

; explizites Unpacking
; Implementierung aufheben
; !x.say()
```

### Stilrichtlinie: ?t option statt (val, ok) verwenden

Wenn eine Funktion scheitern oder einen leeren Wert zurückgeben kann, **sollte man bevorzugt den Typ `?t` option verwenden**, statt des Musters `(val t, ok bool)` mit zwei Rückgabewerten.

`?t` ist eine Tagged Enum mit drei Zuständen: `ok` (mit Wert), `nil` (leer), `err` (Fehler). Normale Werte werden implizit gebunden; wenn eine Operation nur keinen Wert findet, verwendet man `nil`, bei einem tatsächlichen Fehler `err(...)`.

```no
#{fmt-skip}
; ❌ nicht empfohlen: Muster mit zwei Rückgabewerten
stack.pop = () (val i64, ok bool) {
    .n == 0 -> return
    val = .data[.n]
    ok = true
}
```
```

```no
; ✅ empfohlen: option-Typ (nil = leer, err = Fehler)
stack.pop = () (val ?i64) {
    .n == 0 -> {
        val = nil
        return
    }
    val = .data[.n]
}

; ✅ Fehler zurückgeben
file.read = () (data ?str) {
    .fd < 0 -> {
        data = err('file not open')
        return
    }

    ; ... Daten lesen
    data = buf
}
```

Mit match die option auspacken:

```no
val = s.pop()
val: {
    nil -> print('empty')

    err -> print(it.msg()); it = strukturierter Fehler err.err

    -> print(it); it = der gepopte Wert
}
```

## Integer-Überlauf (`#{overflow}`-Annotation)

Nolang unterstützt kein panic — jede Operation, die überlaufen könnte, muss über `option` oder eine explizite Annotation behandelt werden, zur Laufzeit wird niemals eine Integer-Überlaufausnahme geworfen. Das Überlaufverhalten der folgenden **ganzzahligen Grundrechenarten** wird durch die Annotation `#{overflow = ...}` gesteuert:

- Vorzeichenbehaftetes und vorzeichenloses **`+` `-` `*`**: gilt jeweils, wenn beide Operanden Integer sind (inkl. `int`-Literale);
- Vorzeichenbehaftetes **`/`**: nur der Fall `INT_MIN / -1` kann überlaufen (vorzeichenlose Division `a/b ≤ a` läuft nie über, nicht anwendbar).

> Vorzeichenlose Integer (`u8`/`u16`/`u32`/`u64`/`u128`) folgen bei `+ - *` **exakt denselben Regeln** wie vorzeichenbehaftete: Ohne Annotation wird ebenfalls standardmäßig `option<int>` zurückgegeben, bei Überlauf `err`. Das ist eine bewusste Designentscheidung — „wenn sich ein Überlauf nicht zur Kompilierzeit ausschließen lässt, gilt dieselbe Regel“.

### Sonderfall: `byte`- und `char`-Arithmetik

`byte` und `char` sind **Sonderfälle** der Überlaufsemantik: Ihre Arithmetik **erzeugt kein option** und unterliegt nicht der `#{overflow}`-Annotation.

- **`byte` (u8)**: Bei Überlauf von `+ - *` wird **still 2er-Komplement zurückgewickelt** (`250 + 10` → `4`, `0 - 1` → `255`). `byte` ist für Byte-Puffer, hex-Kodierung/-Dekodierung und ähnliche Fälle gedacht, die „natürlich ein Wrap-around erlauben“, dieselbe Semantik wie `#{overflow = wrap}` für Hash/Counter, nur eingebaut und ohne Annotation.
- **`char` (Unicode-Codepoint)**: `+ -` ist **Codepoint-Arithmetik**, liegt das Ergebnis außerhalb des gültigen Codepoint-Bereichs (`< 0` oder `> 0x10FFFF`), ist das Ergebnis **`-1`** (Sentinel für ungültigen Codepoint) — kein `err`, kein Wrap-around.

**Designbegründung**: `i8`–`i128` / `u8`–`u128` werden üblicherweise für **reale Geschäftswerte** verwendet (Beträge, Zähler, Kapazitäten), stilles Wrap-around würde Überlauf-Bugs verstecken, daher standardmäßig `err` und die Pflicht zur expliziten Wahl; die typischen Anwendungen von `byte` / `char` (Byte-Streams, Codepoint-Iteration) erlauben semantisch von Natur aus (sogar erwarten) ein Wrap-around, option würde nur Unpacking-Rauschen erzeugen. Wenn du Überläufe bei `byte` / `char` erkennen willst, konvertiere erst in einen breiteren Typ (wie `b.to-i64()`) und rechne dann.

| Modus          | Annotation                       | bei Überlauf                          | Ergebnistyp   | Anwendungsfall               
| ----------- | ------------------------ | --------------------------- | ------ | ------------------ |
| **Standard (ohne Annotation)** | —                        | Überlauf → `err`, normal → `ok(value)` | `?int` | Operationen, deren Überlauf erkannt werden muss 
| **Wrap-around**      | `#{overflow = wrap}`     | stilles 2er-Komplement-Wrap-around                  | `?int` | Hash, Kryptografie, Zähler und ähnliche Fälle, die Wrap-around semantisch erlauben 
| **Nullen**      | `#{overflow = clamp0}`   | Überlauf (oben oder unten) ergibt `0`              | `?int` | Semantiken, bei denen die Differenz nicht negativ werden darf (z. B. Restmenge)    
| **Minimum-Clamp**   | `#{overflow = min}`      | Überlauf auf Typ-Minimum begrenzt                  | `?int` | Zähler-Untergrenze, Index-Schutz        
| **Maximum-Clamp**   | `#{overflow = max}`      | Überlauf auf Typ-Maximum begrenzt                  | `?int` | Kapazitätsgrenze, sättigende Akkumulation          
| **Sättigung**      | `#{overflow = saturate}` | Überlauf → Maximum, Unterlauf → Minimum               | `?int` | Signal-/Farbsättigung usw.           

> ⚠️ **Die Annotation entpackt nicht** (D1): `#{overflow = …}` **bestimmt nur „wie bei Fehlschlag ein Wert entsteht“**,
> das Ergebnis bleibt immer `?T`. Um das Ergebnis nach plain zurückzuholen, kann man es nur mit match oder `?=` konsumieren; die Unterschiede
> der beiden Konsumformen (`?=` + Block-match／Wert-Position-match) und die Auswahl stehen in〈Abwägung: Block-match vs. Wert-Position-match〉.
> Explizit als plain-Typ zu annotieren (`w i64 = a - b  #{overflow=wrap}`) ist **kein** Ausweg —
> empirisch meldet es weiterhin `15w45dqk` (cannot assign `?i8` value to `i8` variable).

Darüber hinaus unterstützen alle Modi die **Typ-Präfix-Form**, mit der die Sättigungsgrenzen eines bestimmten engen Typs präzise angegeben werden können, z. B. `#{overflow = u8-max}`, `#{overflow = i8-min}`, `#{overflow = u16-saturate}`.

> **Annotationsgranularität**: `#{overflow = ...}` ist eine **Zeilenannotation** — sie wirkt nur auf die **unmittelbar darauf folgende Anweisung**.
> Das ist die **einzige designmäßig erlaubte** Form (zur Verhinderung von Missbrauch): Die Ausführungssemantik (codegen) und der `ovfhndld`-Hardfehler erkennen nur diese,
> und sie ändert nur „welchen Wert es bei Überlauf gibt“, nicht den Typ. Eine Ausweitung auf Block- oder Funktions-Ebene würde die Semantik eines ganzen Bereichs von einer
> weit entfernten Zeile abhängig machen. Die Begründung steht in [〈Scope von Annotationen und Kommentaren〉](annotation-scope.md) §4.1.
>
> ⛔ **Über einer Funktionsdefinition, am Blockanfang oder am Dateianfang ist sie nicht zulässig**. Derzeit einziges Schlupfloch: Eine Funktions-Annotation lässt den
> `ovf-int-default`-Lint die ganze Funktion überspringen (die Liste sieht „sauber“ aus), nicht annotierte Operationen im Funktionskörper lösen aber weiterhin
> `ovfhndld` aus; eine Block-Annotation lässt den Lint nach unten erben und verstummen, codegen erkennt sie grundsätzlich nicht. Bitte pro Anweisung annotieren;
> genau diese pro-Anweisung-Form erzeugt `no fmt --fix=overflow`.
>
> Das vollständige Modell (L0 Modul / L1 Deklaration / L2 Block / L3 einzelne Zeile, und welche Schlüssel welche Ebenen unterstützen) steht in
> [〈Scope von Annotationen und Kommentaren〉](annotation-scope.md); §7 listet Bestand und Schlupflöcher, §8 die Reihenfolge der Verschärfung.

> **LSP-Quickfix**: Der Editor (nolang-lsp) meldet bei nicht annotierten Integer-Operationen einen **Hinweis** (`nolang-overflow`, trace id `ovfhndld`) und bietet fünf Quickfixes — **Add `#{overflow = wrap}`** / **`clamp0`** / **`min`** / **`max`** / **`saturate`** —, die automatisch über der Anweisung der Operation mit derselben Einrückung wie diese Zeile die passende Annotation einfügen und das Überlaufverhalten festlegen (das Ergebnis bleibt `?T`, muss also zusätzlich mit match / `?=` konsumiert werden). Das Kommandozeilen-Äquivalent ist `no fmt --fix=overflow` (siehe unten).

### Standard: `option<int>` zurückgeben

Ohne Annotation ist das Ergebnis von `a - b` vom Typ `option<int>`. Der Empfänger muss `?T` sein (Nullable-Typ), und die drei Zweige `err` / `nil` / `ok` müssen per match zerlegt werden:

```nolang
main = () {
    x i64 = 100
    d ?i64 = x - 1; Standard: gibt option<i64> zurück
    d: {
        err -> print(-1); Überlauf (z. B. x - 2^63 Unterlauf)

        nil -> print(0)

        -> print(1); ok(value): normales Ergebnis
    }
}
main()
```

> Das `data`-Feld von `%option` ist `i64`; Ergebnisse enger Typen (`i8`/`i16`/`i32`) werden zuerst vorzeichenerweitert (sext) und dann gespeichert, beim Unpacking wieder auf die ursprüngliche Breite abgeschnitten.

### wrap: stilles Wrap-around

```nolang
#{overflow=wrap}
sub-wrap = (a i64, b i64) (r i64) {
    r = a - b; bei Überlauf stilles 2er-Komplement-Wrap-around; r ist plain i64 (expliziter Typ)
}
```

Anweisungsebene:

```nolang
x i64 = -9223372036854775807

#{overflow=wrap}
w i64 = x - 2; Unterlauf-Wrap-around → 9223372036854775807
print(w)
```

### clamp0: Überlauf auf Null

```nolang
x i64 = -9223372036854775807

#{overflow=clamp0}
c i64 = x - 2; Unterlauf → 0 (Überlauf ebenfalls 0)
print(c)
```

### min / max / saturate: Sättigungs-Clamping

Die drei Modi „klemmen“ das Überlauf-Ergebnis in den darstellbaren Bereich des Typs, statt zu wrappen oder auf Null zu setzen:

```nolang
#{overflow=min}
floor = (a i64, b i64) (r i64) {
    r = a - b; Unterlauf → i64-Minimum; Überlauf → i64-Maximum
}

#{overflow=max}
cap = (a i64, b i64) (r i64) {
    r = a * b; Überlauf → i64-Maximum; Unterlauf → i64-Minimum
}

#{overflow=saturate}
sat = (a i64, b i64) (r i64) {
    r = a + b; Überlauf→max, Unterlauf→min
}
```

Die Typ-Präfix-Form erlaubt es, die Sättigungsgrenzen enger Typen präzise zu steuern (eine Funktions-Annotation wird auf alle entsprechenden Typ-Operationen im Körper angewendet):

```nolang
#{overflow=max}
inc = (x u8) (r u8) {
    r = x + 1; bei x = 255 Überlauf → 255 (statt Wrap-around zu 0)
}

#{overflow=min}
dec = (x i8) (r i8) {
    r = x - 1; bei x = -128 Unterlauf → -128 (statt Wrap-around zu 127)
}
```

### Compiler-Erzwingung

Nicht annotierte Integer-Operationen geben standardmäßig `option<int>` zurück, daher wird „unbehandelt“ zwangsweise abgefangen. **Ob es als unbehandelt gilt, wird parallel entschieden** — wenn eine der folgenden Bedingungen zutrifft, gibt es keinen Fehler:

- **`?=`-Hochwerfen** (verlangt, dass die Funktion einen option-Ergebnisparameter hat);
- **Lokales Abfangen**: Binden an eine **neue** Variable mit `=` (die Variable wird automatisch zu `?T` abgeleitet) oder explizite Deklaration als `?T` (wie `x ?i64 = a + b`);
- **`_ = expr` explizites Verwerfen** (die Auswertung selbst läuft über den sicheren Pfad, der Fehler wird ebenfalls ignoriert);
- **Annotation** `#{overflow = wrap}` / `clamp0` / `min` / `max` / `saturate` (oder Typ-Präfix-Form wie `u8-max`, `i8-min`), mit der Wrap-around-/Sättigungssemantik akzeptiert wird.

Nur wenn keine der vier zutrifft, gibt es einen Fehler (`ovfhndld`).

> **Reine Arithmetik ist keine Fehlerquelle**: `+ - * <<` und Negation lösen selbst kein Abfangen aus (sonst würde alle Arithmetik zu option und die Standardbibliothek explodieren). Daher meldet `d = x - 1` (ohne `/` `%`, ohne sichere Indexierung, ohne option-Operanden) weiterhin einen Fehler — füge eine Annotation hinzu oder schreibe `d ?i64 = x - 1`.
>
> **Bestehende Variablen ändern ihren Typ nicht**: Das Abfangen wirkt nur auf Ziele, die „in dieser Anweisung erstmals deklariert“ werden. Existiert die Variable bereits (z. B. zuerst `d = 0`, dann `d = x - 1`, oder zuerst `v i64 = 0`, dann `v = arr[i]`), ändert der Compiler sie nicht inplace zu option, sondern meldet einen Fehler und verlangt eine explizite Semantikwahl.

Es gibt zwei weitere Hardfehler, die von den obigen Ausnahmen unberührt bleiben:

- Wird eine nicht annotierte Integer-Operation einer **explizit plain-`int`**-Variable zugewiesen, meldet der Compiler einen Fehler (`cannot assign ?i64 value to i64 variable`) und zwingt dich, `#{overflow = wrap}` / `#{overflow = clamp0}` / `#{overflow = min}` / `#{overflow = max}` / `#{overflow = saturate}` (oder eine Typ-Präfix-Form) hinzuzufügen oder ein benanntes `?T` zu verwenden.
- Wird eine nicht annotierte Integer-Operation einer **bereits existierenden**, nicht-option-Variablen zugewiesen (`d = 0` gefolgt von `d = x - 1`), meldet der Compiler einen Fehler und rät zur Annotation oder zur Deklaration als `?i64`, um ungültiges IR zu vermeiden.

Weitere Regeln:

- Vorzeichenlose Integer-Operationen (`u8`/`u16`/`u32`/`u64`/`u128`) **unterliegen ebenfalls** diesem Mechanismus: Ohne Annotation geben `+ - *` standardmäßig `option<int>` zurück, Überlauf → `err`, konsistent mit der Regel für vorzeichenbehaftete Integer.
- `i128`-Operationen unterstützen den `option`-Pfad nicht (das data-Feld ist nur `i64`) und fallen einheitlich auf stilles Wrap-around zurück, um Korrektheit zu bewahren.

> **Warum standardmäßig option statt wrap?** Stilles Wrap-around versteckt Überlauf-Bugs; die Standardrückgabe von `option` zwingt den Aufrufer, den Überlauf explizit zu behandeln (oder aktiv `wrap` zu annotieren, um „ich akzeptiere Wrap-around-Semantik“ zu erklären), und macht „ob Überlauf erlaubt ist“ zu einer sichtbaren Designentscheidung.

### Division durch null (division by zero)

Ein Divisor von `0` bei Integer-`/` und `%` ist **undefiniertes Verhalten (UB)** und **eine von Überlauf unabhängige Sache** — keine Annotation kann davon befreien:

- **Zur Kompilierzeit entscheidbar**: Der Divisor ist ein **Literal-Konstant 0** (`10 / 0`, `a % 0`, `x / 0`) ⇒ **Hardfehler** (trace id `div-by-zero`). `#{overflow = wrap}` befreit davon nicht (diese Annotation regelt Überlauf, nicht Division durch null).
- **Erst zur Laufzeit bekannt**: Der Divisor stammt aus einer Variable / externer Eingabe ⇒ der Compiler fügt **auf beiden Pfaden** eine Nullprüfung ein, der Unterschied liegt nur in der „Art der Meldung“:

| Ergebnistyp        | Auslösebedingung                    | bei Divisor 0                                                                        
| ----------- | ----------------------- | ------------------------------------------------------------------------------ |
| `?int` (Standard)  | Anweisung nicht annotiert                   | option tag = `err`, mit `?=` / match behandeln                                           
| plain `int` | Anweisung mit `#{overflow = ...}` | Ergebnis wird auf `0` erzwungen und auf **stderr** wird `nolang: integer division or remainder by zero ...` ausgegeben 

```nolang
safe = (a i64, b i64) (r ?i64) {
    r = a / b; b == 0 → err (Aufrufer muss es behandeln)
}

#{overflow=wrap}
fast = (a i64, b i64) (r i64) {
    r = a / b; b == 0 → 0 und eine stderr-Diagnosezeile (kein nacktes sdiv-UB mehr)
}
```

> **Warum `#{overflow = wrap}` die Null-Divisor-Wache nicht aufheben kann**: Es ändert das **Überlaufverhalten** (der Ergebnistyp bleibt immer `?int`), nicht die Sicherheit des Divisors. Würde man es aufheben, emittierte der Compiler ein nacktes `sdiv`, und der Optimierer dürfte annehmen, der Divisor sei nie 0 (würde `a / 0` zu einem beliebigen Wert falten), während x86-64-`idiv` direkt mit #DE abstürzt — UB und ohne jede Diagnose. Daher existiert die zero-divisor-Wache **unabhängig von Annotationen**; dass der plain-Pfad eine zusätzliche stderr-Zeile ausgibt, liegt genau daran, dass dieser Pfad kein option für den Aufrufer zum Prüfen hat.
>
> **Dieselbe Wache deckt auch `sdiv (INT_MIN, -1)` ab** (Ergebnis nicht darstellbar, ebenfalls UB; auf x86-64 SIGFPE). Der option-Pfad nimmt es in `err` auf; der plain-Pfad nimmt den 2er-Komplement-Wrap-around-Wert (`INT_MIN / -1 → INT_MIN`, `INT_MIN % -1 → 0`).

Gleitkomma-Division ist nicht betroffen: `1.0 / 0.0` ist durch IEEE-754 als `inf` / `nan` definiert, kein UB.

### Nicht annotierte Operationen als Fehler (`ovf-int-default`)

`no vet` meldet **nicht annotierte** Integer-Operationen als **Hinweis** (trace id `ovfhndld`, HINT). Grund: Das Standard-`?T` ist für den Benutzer unsichtbar, er muss davon erfahren — er kann `#{overflow = ...}` hinzufügen, um das Überlaufverhalten festzulegen, oder `?T` + `?=` / match / expliziten plain-Typ zum Konsumieren verwenden. Was den Build wirklich blockieren sollte, ist `optunuse` (option weder hochgeworfen noch ausgepackt).

### Automatische Reparatur: `no fmt --fix=overflow`

Annotations-Korrekturen en masse müssen nicht von Hand geschrieben werden, verwenden Sie den Reparatur-Klassenparameter von `no fmt`:

```bash
no fmt --fix=overflow -w src/std          # gesamtes Verzeichnis inplace reparieren
no fmt --fix=overflow -d src/std/str.no   # nur Diff ausgeben, keine Datei schreiben
no fmt --fix=overflow src/std/str.no      # reparierten Inhalt ausgeben
```

Die Reparatur ist **präzise und nicht verschmutzend**: Sie fügt `#{overflow=wrap}` nur über der Anweisung derjenigen Operation ein, die der Lint tatsächlich gemeldet hat, ändert keine anderen Anweisungen, formatiert nicht neu, rührt bestehende Annotationen nicht an und ist bei erneutem Lauf idempotent (bereits Annotiertes bleibt unverändert). Das unterscheidet sich von einem direkten `no fmt -w` — letzteres formatiert vollständig neu.

> Führen Sie nach der Reparatur noch einmal `no vet` aus, um Null zu bestätigen. In den allermeisten Fällen ist die Liste nach einem Durchlauf leer (`src/std` von 4728 Einträgen auf 0). Die einzige bekannte Ausnahmeform: Der Treffer liegt in der **Bedingung** eines match-Arms, während der Arm-Körper keine annotierbare Anweisung enthält — eine Zeilenannotation wirkt nur auf die erste Anweisung danach und erfasst die Bedingungsoperation des Arms selbst nicht, daher meldet `no vet` erneut. Ziehen Sie in diesem Fall die Operation zuerst in eine eigenständige Anweisung heraus und annotieren Sie sie dann.

### Automatische Reparatur: redundante Typannotationen

Wenn der **Typannotation einer Variablendeklaration exakt dem aus dem rechten Wert abgeleiteten Typ entspricht**, ist die Annotation redundant (`no vet` meldet sie als Hint `tcpoxtfd`, `type annotation '<T>' can be omitted (inferred from value)`).

- **Standardmäßig entfernt `no fmt` sie automatisch**: `no fmt -w` entfernt redundante Annotationen beim Formatieren (und führt zugleich eine normale Neformatierung durch), `no fmt -d` zeigt die Änderung in der Vorschau, `-w` schreibt sie.

```bash
no fmt -w main.no          # formatieren und redundante Annotationen entfernen
no fmt -d main.no          # nur Diff ausgeben (inkl. Entfernung der Annotation)
no fmt -w src/             # gesamtes Verzeichnis
```

- **`--fix=redundant` (chirurgisch, nur Annotationen)**: Wenn Sie nur redundante Annotationen entfernen, aber nicht vollständig neu formatieren wollen (etwa bei `src/std`, weil `no fmt` `#{index-out}` verliert), verwenden Sie diese Klasse — sie löscht nur Annotationen, formatiert nicht neu:

```bash
no fmt --fix=redundant -w src/std          # redundante Annotationen im ganzen Verzeichnis inplace entfernen (ohne Neformatierung)
no fmt --fix=redundant -d src/std/str.no   # nur Diff ausgeben, keine Datei schreiben
no fmt --fix=redundant src/std/str.no      # reparierten Inhalt ausgeben
```

```nolang
; vor der Reparatur
x str = ''
b i64 = 5
m [str]i64 = make-map()

; nach der Reparatur (Annotation == abgeleiteter Typ, entfernt)
x = ''
b = 5
m = make-map()
```

Kernpunkte der Regel:

- **Nur wenn Annotationstyp == abgeleiteter Typ** entfernen (etwa `str`/`i64`/`[str]i64` passend zum abgeleiteten Wert).
- Fälle mit **impliziter Konvertierbarkeit, aber Ungleichheit** werden **nicht** entfernt — nur die Annotation bewahrt die Konvertierungssemantik. Z. B. `c i16 = 5` (`5` wird zu `i64` abgeleitet, nicht gleich `i16`), `p ?i64 = nil`, `last txt = ''` (`txt` und `str` sind implizit ineinander konvertierbar, aber verschieden).
- Schreibweisen **ohne Typannotation** (wie `d = 'x'`) werden ohnehin nicht gemeldet und bleiben unberührt.
- **Annotationen von Arrays fester Länge `[N]T` / `[?]T` bleiben immer erhalten** (nie als redundant angesehen). `[N]` ist kein bloßer Typ-Hinweis, sondern ein Schalter für die **Darstellungsform**: Nur wenn man es schreibt, wird das `[...]`-Literal rechts zu einem Array fester Länge auf dem Stack geparst; nur dann ist das Ableitungsergebnis „genau gleich“ der Annotation — ein von der Annotation selbst erzeugter Zirkelschluss. Löscht man es, wird dieselbe Zeile zu einem auf dem Heap wachsenden Slice `[]T` neu geparst, und die Semantik ändert sich: Bei `fixed [3]char = ["x", "y", "z"]` ist `.push()` ungültig (`.len()` bleibt immer 3), nach dem Reduzieren auf `fixed = ["x", "y", "z"]` lässt `.push()` `.len()` auf 4 steigen. Slice-Annotationen `[]T` fallen nicht darunter — nach dem Löschen bleibt es ein Slice, echte Redundanz.
- **Werte mit Hexadezimal-Literalen behalten immer die Annotation** (konservative Ausnahme). Die Typableitung von `no vet` nutzt für `0xNN` die Heuristik „ableiten als `byte`“, was von der tatsächlichen Semantik des Compilers abweicht („ohne Annotation ist ein Integer-Literal standardmäßig `i64`“), daher würden `IP-TBL [64]byte = [0x3a, ...]` und `s []byte = [0x50, 0x4b]` fälschlich als redundant erkannt; nach dem Entfernen würden die Literale zu `i64`-Elementen degenerieren und Typfehler wie `expected '[]byte', got '[]i64'` erzeugen. Deshalb treten Hint und Entfernung nicht auf, sobald im Wertausdruckstext `0x`/`0X` vorkommt.
- **Wenn der rechte Wert ein Feldlesen ist, bleibt die Annotation immer erhalten** (konservative Ausnahme). Die Annotation bei Schreibweisen wie `i i64 = .pos` ist kein bloßer Typ-Hinweis, sondern ein Schalter dafür, ob nachgelagerte Integer-Arithmetik `?T` erzeugt: Wird der Typ aus dem Feld abgeleitet, ist `i + N` keine „vorzeichenbehaftete Operation“ mehr, gibt kein `?i64` zurück, der `ok`-Arm der zweistufigen Entpackung wird nie erreicht und der Schleifenzähler bleibt unverändert. Beispiel `src/std/yaml.no`, `parser.looks-like-map-entry`: Das Entfernen von `i64` lässt `yaml.parse('abc')` in eine Endlosschleife laufen. Daher werden Annotationen bei `x = .field`, `x = obj.field` (auch mit Klammern oder Präfix umgeben) weder als `tcpoxtfd` gemeldet noch von Standard-`no fmt` oder `--fix=redundant` gelöscht.

Ob über Standard-`no fmt` oder `--fix=redundant`, die Entfernung löscht nur die gemeldete Typannotation samt vorangegangenem Leerraum und ist bei erneutem Lauf idempotent (bereits Entferntes bleibt unverändert). Nach der Reparatur empfiehlt sich ein erneutes `no vet` zur Bestätigung, dass `tcpoxtfd` null ist.

### Automatische Reparatur: option konsumieren (`--fix=unwrap`)

Unter der vereinheitlichten Regel ist „option erzeugt, aber nie konsumiert“ die einzige Diagnose, die den Build blockieren sollte (trace `optunuse`). Für die Massenumstellung auf explizite Konsumformen verwenden Sie `--fix=unwrap`:

```bash
no fmt --fix=unwrap -w src/std            # gesamtes Verzeichnis inplace reparieren
no fmt --fix=unwrap -d src/std/str.no     # nur Diff ausgeben, keine Datei schreiben
no fmt --fix=unwrap src/std/str.no        # reparierten Inhalt ausgeben
```

Reparaturform (siehe〈Sichere Indexierung〉): **Die umgebende Funktion hat bereits einen `?T`-Ergebnisparameter** → Hochwerfen (`x ?= <expr>`); andernfalls → Umbenennen + match-Unpacking:

```nolang
; vor der Reparatur
x = a + b

; nach der Reparatur (die umgebende Funktion hat einen ?T-Ergebnisparameter)
x ?= a + b

; nach der Reparatur (andernfalls, 4-Zeilen-Standardform; leerer Auffang-Arm = Nullwert)
x-opt = a + b
x = x-opt: {
    ok -> it

    ->
}
```

Wie bei anderen Fix-Klassen: chirurgischer Textersatz, keine Änderung des Layouts, die `#{overflow=…}` / `#{index-out=…}`-Annotationen über der Anweisung bleiben unberührt (sie sind weiterhin für „wie bei Fehlschlag ein Wert entsteht“ zuständig), bei erneutem Lauf idempotent. Führen Sie nach der Reparatur noch einmal `no vet` aus, um zu bestätigen, dass `optunuse` null ist.

### Annotationenbindung und Zusammenführen in einer Zeile

`#{...}`-Annotationen **binden an Knoten**, daher kann eine Anweisung gleichzeitig mehrere Schlüssel tragen; mehrere Annotationen werden stets **zu einer einzigen Zeile zusammengeführt**:

```nolang
#{fmt-skip}
i <- [0..16): {

    #{index-out=0, overflow=wrap}
    buf[base + i] = data[i]
}
```

Die obige Anweisung braucht gleichzeitig zwei Semantiken: „bei Bereichsüberschreitung einen Standardwert nehmen“ (`index-out`) und „Überlauf-Wrap-around“ (`overflow`). Auf zwei Zeilen geschrieben ist das Parsingergebnis identisch, aber `no fmt --fix=overflow` erzeugt die zusammengefasste einzeilige Form.

> **Gültigkeitsbereich von Zeilenannotationen**: `#{overflow = ...}` wirkt nur auf die **unmittelbar darauf folgende Anweisung**. Dazwischen liegende, unverwandte Annotationen werden trotzdem korrekt auf die Anweisung angewendet. Sichere Index-Schreibzugriffe (`arr[base + i] = v`, `.buf[.pos + i] = v`) werden in eine match-Form desugart, und `#{overflow = wrap}` wird mit auf den Knoten nach dem Desugaring übertragen, daher fällt auch der Indexausdruck unter die Annotation und wird nicht fälschlich als nicht annotiert gemeldet.

### Fehler hochwerfen (`?=`-Operator)

`?=` ist eine **Kurzschreibweise** für option-Unpacking plus Hochwerfen des Fehlers. **Die Funktion muss keinen option-Rückgabetyp haben**: Es genügt, wenn eine der beiden folgenden Situationen zutrifft,
daher kann auch eine Funktion, deren Rückgabewert kein option ist, `y ?= x + 1` schreiben, wenn sie ihren Körper in einen Block-match packt.

**Syntax:**

```no
v ?= expr
```

**Semantik:**

- Gibt `expr` `ok(value)` zurück, wird `v` mit dem ausgepackten inneren Wert belegt (automatisches Unpacking)
- Gibt `expr` `nil` oder `err` zurück:
  - hat die Funktion einen `?T`-Ergebnisparameter → wird der option in den Ergebnisparameter geschrieben und vorzeitig `return` ausgeführt (automatisches Hochwerfen);
  - steht die Anweisung im Körper eines **Block-match auf Anweisungsebene** `{ ... }: { ... }` → wird sie vom **nächstgelegenen** Block-match abgefangen,
    der **gesamte Körper stoppt** und die Arme laufen nur einmal (**kein** `return`), die Funktion braucht keinen `?T`-Ergebnisparameter.

**Verwendungsbedingungen (eine der beiden genügt):**

1. Die umgebende Funktion hat einen Ergebnisparameter vom option-Typ;
2. Die Anweisung steht im Körper eines Block-match auf Anweisungsebene `{ ... }: { ... }` (siehe〈Die drei Formen des match-Subjekts〉).

Trifft keines von beiden zu, meldet der Compiler einen Fehler. Die beiden Arme `err -> _` / `-> _` können den hochgeworfenen Fehler inplace verschlucken, das ist die Standardlösung für „ich will
die Kurzform nutzen, habe aber keinen `?T`-Ergebnisparameter“:

```no
handle = (x i64) {
    {
        y ?= x + 1
        print(1)
    }: {
        err -> _

        -> _
    }
}
```

**Ergebnisparameter müssen nicht vorab belegt werden:** Der Funktions-Prolog macht keine Nullinitialisierung, aber beim return werden nicht explizit belegte Output-Parameter automatisch mit Nullen gefüllt (Integer → `0`, `str` / Strukturen → Nullwert, option → `nil`), daher kann man im Beispiel das `result = nil`-Boilerplate weglassen und nur die Erfolgspfad-Zuweisung schreiben.

**Beispiel:**

```no
; Datei zeilenweise lesen und verarbeiten — ?= vereinfacht die Fehlerausbreitung
process-file = (path str) (result ?str) {
    f ?= open(path); bei Fehler automatisch result = f; return
    line ?= f.read-line(); EOF oder Fehler wird automatisch hochgeworfen
    result = line
}
```

Äquivalente expandierte Form (vom Compiler erzeugte match-Kette):

```no
#{fmt-skip}
process-file = (path str) (result ?str) {
    result = nil
    __tmp = open(path)
    __tmp: {
        nil || err -> {
            result = __tmp
            return
        }
        -> f = it
    }
    __tmp2 = f.read-line()
    __tmp2: {
        nil || err -> {
            result = __tmp2
            return
        }
        -> line = it
    }
    result = line
}
```

**Kettenverwendung:** Mehrere `?=` können verkettet werden für eine Pipeline-artige Fehlerausbreitung:

```no
pipeline = (input str) (result ?str) {
    a ?= step1(input); bei Fehler hochwerfen
    b ?= step2(a); bei Fehler hochwerfen
    result = b
}
```

**Nackte Arithmetik:** Die rechte Seite von `?=` ist nicht auf Funktionsaufrufe beschränkt — jede `option` zurückgebende Integer-Operation (wie nicht annotiertes `a - b`, `a + b`, `a * b`) kann direkt mit `?=` den Überlauffehler hochwerfen, ohne vorher an eine Zwischenvariable gebunden zu werden:

```no
; bei Überlauf automatisch result = err; return, im Normalfall wird v zum inneren Wert ausgepackt
sub-safe = (a i64, b i64) (result ?i64) {
    v ?= a - b; Unterlauf/Überlauf → err automatisch hochgeworfen; sonst v = inneres i64
    result = v
}

add-safe = (a i64, b i64) (result ?i64) {
    v ?= a + b; wie oben, gilt für + * und vorzeichenbehaftetes /
    result = v
}
```

Äquivalente Expansion (vom Compiler automatisch erzeugt):

```no
#{fmt-skip}
sub-safe = (a i64, b i64) (result ?i64) {
    result = nil
    __unwrap = a - b            ; option<i64>
    __unwrap: {
        nil -> {
            result = __unwrap
            return
        }
        err -> {
            result = __unwrap
            return
        }
        -> v = it               ; ok(value): inneren Wert auspacken
    }
    result = v
}
```

**Zusammengesetzte rechte Seite (compound RHS):** Die rechte Seite von `?=` kann ein zusammengesetzter Ausdruck mit mehreren option-Operanden sein. Jeder option-Operand wird automatisch ausgepackt und mit einem Ausbreitungs-Guard versehen — sobald ein Operand `nil` / `err` ist, wird der option-Ergebnisparameter der aktuellen Funktion auf diesen Wert gesetzt und `return` ausgeführt (Hochwerfen); nur wenn alle erfolgreich sind, werden die inneren Werte der linken Seite zugewiesen:

```no
sum3 = () (result ?i64) {
    a ?i64 = ok(10)
    b ?i64 = ok(20)
    c ?i64 = ok(30)
    total ?= a + b + c; jeder option-Operand wird automatisch ausgepackt und ausgebreitet
    result = total
}

comp = () (result ?i64) {
    a ?i64 = ok(4)
    b ?i64 = ok(6)
    v ?= dbl(a + b); a + b kann option sein, wird ebenfalls automatisch ausgepackt und ausgebreitet
    result = v
}
```

> Nur option-Operanden an **Operand-Position** werden automatisch ausgepackt (wie `a + b + c`, `f(b + c)`); eine als Funktionsargument direkt übergebene option-Variable fällt nicht darunter — sie läuft über die bestehende nackte-option-Argument-Ausnahme von `?=` (`a ?= f(v)`, wobei `v` ein `?i64` ist und `f` ein plain `i64` erwartet).

**Fehler bei nicht-option rechter Seite:** Um zu verhindern, dass `=` großflächig versehentlich als `?=` geschrieben wird, meldet der Compiler einen Fehler und rät zu `=`, wenn die rechte Seite zur Kompilierzeit eindeutig als „bestimmt kein option“ festgestellt werden kann:

- Literal: `v ?= 42`
- konstantenfaltbare Integer-Operation: `v ?= 10 + 20`
- bekannter, nicht-option lokaler Variable: `v ?= x` (wobei `x i64 = 5`)

Die Fehlermeldung hat die Form: `` `?=` RHS `42` is not an option — use `=` instead ``.

> Arithmetik gilt als „möglicherweise option“, sobald ein Operand option ist (oder der Typ unbekannt / ein Funktionsaufruf ist, der option zurückgeben kann), und wird nicht fälschlich gemeldet — etwa `v ?= a + b + c` (a/b/c sind `?i64`) oder `v ?= b + c` (b/c unbekannt) kompilieren normal.

**Anwendungsfälle:**

- `pop` / `peek` und ähnliche Containeroperationen, die leer sein können → `?t` (`nil` = leer)
- `read-line` / `read-byte` und ähnliche I/O-Operationen → `?str` / `?i64` (`nil` = EOF, `err` = Fehler)
- `lookup` / `get` und ähnliche Suchoperationen → `?t` (`nil` = nicht gefunden)
- `parse` / `from-str` und ähnliche Parseroperationen → `?t` (`nil` = leer, `err` = ungültige Eingabe)
- `accept` / `dial` und ähnliche Netzwerkverbindungen → `?conn` (`nil` = keine Verbindung, `err` = Fehler)

**nil vs err:** Verwende `nil`, wenn das Fehlen ein normales/erwartetes Ergebnis ist (leerer Stack, fehlender Schlüssel, EOF); verwende `err('msg')`, wenn das Fehlen einen tatsächlichen Fehlerzustand bedeutet (I/O-Fehler, ungültige Eingabe, verweigerte Verbindung).

**Ausnahme:** Wenn eine Funktion mehrere unabhängige Werte zurückgeben muss (wie `(name str, value str, ok bool)`), darf das Muster mit mehreren Rückgabewerten beibehalten werden.

**Höchstens ein `?T` bei mehreren Rückgabewerten (Hardfehler):** Unter den Ergebnisparametern einer Funktionsdeklaration darf es höchstens ein `?T` geben. `?T` ist der **einzelne** Fehlerkanal — bei einem vorzeitigen `?=`-Return in einem solchen Funktionskörper kann der Fehler nur in diesen einen option-Ergebnisparameter gelegt werden; deklariert man zwei `?T`, kann der Compiler nur den ersten füllen, der Aufrufer erhält ein halb gefülltes Tuple (`(err, nil)`) und kann nicht erkennen, welcher Wert den Fehler darstellt:

```no
#{fmt-skip}
pair = () (a ?i64, b ?i64) {; Fehler: zwei ?T-Ergebnisparameter
    a = ok(1)
    v ?= boom(); boom() gibt err('boom') zurück
    a = v
    b = ok(2); wird nie ausgeführt — der Aufrufer sieht (err, nil)
}
```
```

Die richtige Schreibweise ist, die unabhängigen Werte als plain-Typen zu deklarieren und nur ein `?T` als Fehlerkanal zu behalten:

```no
lookup = (k str) (value str, found bool, err ?str) {
    value = ''
    found = false
    err = nil
}
```

Die Fehlermeldung hat die Form: `` function 'pair' declares 2 option (?T) results ('a', 'b'); only one result may be ?T … [moptres1] ``. Der implizite `self`-Empfänger einer Methodendefinition ist ein Output-Parameter für den Aufrufer-Alias und zählt nicht in dieses Kontingent; `#{c}`-extern-Deklarationen unterliegen derselben Einschränkung.

### Fehler abfangen mit Zuweisung (`=` lokales Abfangen)

Die Semantik von `?=` ist „Fehler hochwerfen“ — sie verlangt, dass die umgebende Funktion einen option-Ergebnisparameter hat, und kehrt bei Fehlschlag vorzeitig zurück. Wenn du nicht vorzeitig zurückkehren, sondern den Fehler an Ort und Stelle behandeln willst, verwende ein normales `=`: Enthält die rechte Seite eine **Fehlerquelle**, wird die Zielvariable automatisch zu `?T` abgeleitet, `nil` / `err` **wird inplace in dieser Variable gespeichert**, und der Ablauf läuft weiter.

```no
handle = (b i64, c i64, d i64) (r i64) {
    a = b + c / d; a wird zu ?i64 abgeleitet; Division durch 0 → a = err
    a: {
        err -> {
            r = -1; Fehler inplace behandeln
        }

        nil -> {
            r = -1
        }

        -> {
            r = it; Erfolg: r = b + c/d
        }
    }
}
```

**Fehlerquellen (Auslöser des Abfangens):**

| Quelle                    | Beispiel                                   | Abfangergebnis                          
| --------------------- | ----------------------------------- | ----------------------------- |
| Integer-Division / Modulo             | `a = b + c / d`, `a = b % c`         | Division durch 0, `MIN / -1` → `err`         
| Sichere Indexierung                  | `a = v[i]` (`arr` / `vec` / `slice`) | Bereichsüberschreitung → `err` (`code.index-out`)  
| Operanden vom option-Typ           | `a = x + 1` (`x ?i64`)               | wenn `x` `nil` / `err` ist, unverändert in `a` gespeichert 
| Aufruf mit option-Rückgabe (als Arithmetik-Operand) | `a = f(x) + 1` (`f` gibt `?i64` zurück)       | wie oben                            

> Als Funktionsargument **direkt** übergebene option-Werte fallen nicht darunter — das überlässt man dem Aufgerufenen (konsistent mit der nackten-option-Argument-Ausnahme bei `?=`).

**Reine Arithmetik ist keine Fehlerquelle**: `+ - * <<` und Negation lösen selbst kein Abfangen aus (sonst würde alle Arithmetik zu option). Sobald man aber auf dem option-Pfad landet (etwa weil die rechte Seite noch ein `/` enthält oder das Ziel explizit als `?T` deklariert ist), tragen diese Operationen eine **Laufzeit-Überlaufprüfung**, bei Überlauf → `err`:

```no
ovf = (x i64) (r i64) {
    a ?i64 = x + 1; explizites ?T → option-Wrapping-Pfad
    a: {
        err -> {
            r = 0; bei x = i64-max Überlauf → err
        }

        nil -> {
            r = 0
        }

        -> {
            r = it
        }
    }
}
```

**Abfangen vs. Hochwerfen:**

| Schreibweise            | bei Fehlschlag                                 | option-Ergebnisparameter nötig | danach Wert entnehmen             
| ------------- | ----------------------------------- | -------------- | ------------------ |
| `a ?= expr`   | setzt Ergebnisparameter auf `nil`/`err` und `return` (vorzeitige Rückkehr) | ja              | entfällt (bereits zurückgekehrt)           
| `a = expr`    | `a` speichert `nil`/`err` inplace, Ablauf läuft weiter            | nein              | `a: { ok -> ... }` 
| `a ?T = expr` | wie `=` (explizite Annotierung, enthält innere fehlerfähige Teilausdrücke)               | nein              | wie oben                 
| `_ = expr`    | wertet aus, verwirft aber Ergebnis und Fehler (kein Fehler, löst unused-lint nicht aus)     | nein              | entfällt                

**Gültigkeitsbereich:** Das Abfangen bei sicherer Indexierung wirkt wie bei `?=` **nur in Funktionen mit `?T`-Rückgabetyp** (siehe〈Sichere Indexierung〉, dritte Schreibweise). Das Abfangen von `/` `%` und option-Operanden gilt dagegen für alle Funktionen.

**Bestehende Variablen ändern ihren Typ nicht:** Das Abfangen wirkt nur auf Ziele, die „in dieser Anweisung erstmals deklariert“ werden. Existiert das Ziel bereits (`v i64 = 0` gefolgt von `v = arr[i]`, oder `d = 0` gefolgt von `d = x - 1`), ändert der Compiler seinen Typ nicht inplace, sondern meldet einen Fehler und verlangt eine explizite Semantikwahl (`#{overflow=...}` / `#{index-out=...}` annotieren, `?=` verwenden oder `?T` deklarieren).

### Generics

```no
arr_to_vec = (arr [n]t) (out []t) {
    i <- [0..n): {
        out[i] = arr[i]
    }
}
```

### Typumwandlung (cast)

```no
; Typnamen als String zurückgeben
a = typeof(x)

; `as` ist nur für FFI-Zeiger-Typkonvertierungen erlaubt (wie *byte, **byte, *i64)
; Integer sind intern alle i64, keine explizite Konvertierung nötig
y = x as *byte
```

### Typprüfung bei Integer-Zuweisung

Der Compiler prüft Integer-Zuweisungen, um unsichere Narrowing-Vorgänge zu verhindern, die zu Datenverlust führen. Die Regeln:

#### Implizite Erweiterung (sicher, automatisch erlaubt)

Werte eines schmalen Integer-Typs können automatisch einem breiteren Typ zugewiesen werden, weil der Zieltyp den Quelltyp vollständig umfasst:

```no
b byte = 200
i i64 = b; ✓ byte-Bereich [0,255] ⊆ i64-Bereich
u u32 = b; ✓ byte-Bereich ⊆ u32-Bereich
```

#### Zuweisung von Integer-Literalen

Ein Integer-Literal (standardmäßig zu `i64` abgeleitet) kann jedem Integer-Typ zugewiesen werden, dessen Bereich den Wert umfasst:

```no
#{fmt-skip}
n u8 = 200; ✓ 200 ∈ [0,255]
m u8 = 300; ✗ 300 > 255, Kompilierungsfehler
big u64 = 18446744073709551615; ✓ 2^64-1, u64-Maximum
```
```

#### Unsicheres Narrowing (Kompilierungsfehler)

Die direkte Zuweisung einer Variable eines breiten Typs zu einem schmalen Typ führt zu einem Kompilierungsfehler, weil Datenverlust möglich ist. Die Fehlermeldung enthält einen **umsetzbaren Reparaturhinweis**, der zeigt, wie man mit Bitoperationen sicher narrowt:

```no
#{fmt-skip}
d u64 = 42
h u32 = d; ✗ cannot assign u64 value to u32 variable 'h'; hint: narrow safely with a bitwise mask (e.g. `& 4294967295`) or right shift (e.g. `>> 32`)
h u16 = d; ✗ cannot assign u64 value to u16 variable 'h'; hint: narrow safely with a bitwise mask (e.g. `& 65535`) or right shift (e.g. `>> 48`)
h u8 = d; ✗ cannot assign u64 value to u8 variable 'h'; hint: narrow safely with a bitwise mask (e.g. `& 255`) or right shift (e.g. `>> 56`)
x u32 = d + 1; ✗ Additionsergebnis bleibt u64, unsicher
y u32 = foo(); ✗ Ergebnistyp des Funktionsaufrufs passt nicht
```
```

> **Reparaturhinweis**: Der Compiler berechnet den genauen Maskenwert und die Verschiebung automatisch aus dem Zieltyp. Wendet man den Hinweis an, ist sicheres Narrowing möglich (siehe nächsten Abschnitt).
>
> **Vorzeichenbehaftete Zieltypen**: Bei `i8`/`i16`/`i32`/`i64` erklärt der Hinweis, dass Bit-Narrowing nicht anwendbar ist (die Semantik der Vorzeichenbit-Abschneidung ist unklar), und rät zu einer expliziten Bereichsprüfung.

#### Sicheres Narrowing durch Bitoperationen (automatisch erlaubt)

Wenn die rechte Seite der Zuweisung ein **Bitoperationsausdruck** (`&`, `|`, `^`, `<<`, `>>`) ist und der Zieltyp eine **vorzeichenlose Ganzzahl** (`u8`/`u16`/`u32`/`u64`/`byte`), erlaubt der Compiler implizites Narrowing — das Abschneiden hoher Bits ist die Standardsemantik von Bitoperationen und führt nicht zu unerwartetem Datenverlust:

```no
d u64 = 42

; ✓ Maskenoperation: Ergebnis immer ≤ Maskenwert, sicher in u32
h u32 = d & 67108863; mask = 2^26-1 < 2^32
h u32 = d & 4294967295; Maske = 2^32-1, genau der u32-Bereich

; ✓ Verschiebung: nach Rechtsverschiebung sind hohe Bits 0, sicher
hi u32 = d >> 32; u64 >> 32 lässt 32 Bits

; ✓ XOR / OR-Kombination
c u32 = a ^ b; Bitoperationsergebnis
b byte = v & 255; auf byte-Bereich maskieren

; ✓ zusammengesetzte Bitoperation (häufig in Kryptografie/Kodierung)
s u32 = (key[0] & 255) | ((key[1] & 255) << 8) | ((key[2] & 255) << 16) | ((key[3] & 255) << 24)
```

> **Warum erlaubt?** Die Semantik von Bitoperationen (Maskieren, Verschieben, XOR, OR) besteht gerade darin, ein Bitmuster zu konstruieren. Bei der Zuweisung an einen schmalen vorzeichenlosen Typ ist das Abschneiden hoher Bits eine bewusste Operation — der Entwickler hat durch Maske oder Verschiebung den Wertebereich garantiert oder die hohen Bits absichtlich verworfen. Das ist ein Standardmuster in Kryptografie (wie ChaCha20, Poly1305, Blake2) und Kodierungscode.

> **Nur vorzeichenlose Zieltypen.** Bei vorzeichenbehafteten Integer-Zielen (`i8`/`i16`/`i32`/`i64`) wird auch bei einer Bitoperation auf der rechten Seite ein Fehler gemeldet, weil die Semantik des Vorzeichenbit-Abschneidens unklar ist:
>
> ```no
> d u64 = 42
> h i32 = d & 4294967295   ; ✗ weiterhin Fehler: sicheres Narrowing gilt nicht für vorzeichenbehaftete Ziele
> ```

> **Der oberste Knoten muss eine Bitoperation sein.** Nur wenn der oberste Operator des Ausdrucks `&`/`|`/`^`/`<<`/`>>` ist, wird es erlaubt. Addition, Subtraktion, Funktionsaufrufe, direkte Variablenreferenzen usw. fallen nicht darunter:
>
> ```no
> d u64 = 42
> h u32 = d              ; ✗ oberster Knoten ist Identifier, keine Bitoperation
> h u32 = d + 1          ; ✗ oberster Knoten ist +, keine Bitoperation
> ```

### Modulsystem

- Jede Datei ist ein Modul
- Datei- und Ordnernamen verwenden Bindestriche

```shell
utils/
└── helper.no    ; Modulname ist utils/helper
```

### Module importieren

> **Die neue Syntax verwendet `#` zum Importieren. Das alte Schlüsselwort `use` funktioniert weiterhin, ist aber veraltet (deprecated) — bitte auf `#` umsteigen.**

```no
; Standardbibliothek (neue Syntax, empfohlen)
# std/math.add
; Remote-Modul (beginnt nicht mit std/)
# github.com/utils/math.add
; lokales Modul, muss mit / beginnen
# /utils/math.add
; Alias
# std/math.add a

; ── Folgendes ist alte Syntax (deprecated, weiterhin nutzbar, aber nicht empfohlen) ──
; use std/math.add
; use github.com/utils/math.add
; use /utils/math.add
; use std/math.add a
```

#### `.noml`-Konfigurationsdateien direkt einbinden

`.noml`-Dateien können mit `#` direkt eingebunden werden: Die Auflösung sucht zuerst eine gleichnamige `.no`-Datei, falls nicht gefunden eine `.noml`. Nach dem Einbinden
**heißt die Struktur standardmäßig nach der Datei** (ohne Endung, überschreibbar durch `#{name = '...'}` am Dateianfang),
die Mitglieder werden zu Feldern dieser Struktur; zusätzlich wird ein gleichnamiger globaler Wert erzeugt, der den Dateiinhalt trägt, daher liest man mit
`server.port` die Konfiguration.

```noml
; cfg/server.noml
{
  host: 'localhost'
  port: 8080
  tags: [] #{type = '[]str'}
  compiler: {
    version: 'v0.1.0'
  }
}
```

```no
# /cfg/server

print(server.port); 8080
print(server.compiler.version); v0.1.0
```

- Der Feldtyp wird zuerst aus der `#{type = '...'}`-Annotation des Mitglieds genommen (der Wert ist ein vollständiger Nolang-Typausdruck, wie
  `'[]str'`, `'[]i64'`, `'?i64'`); ohne Annotation wird aus dem Wert abgeleitet: String → `str`, Integer → `i64`,
  Dezimalzahl → `f64`, Boolean → `bool`, Objekt → automatisch erzeugte verschachtelte Struktur (`server-compiler`),
  Array → `[]Elementtyp`.
- Leere Container (`[]`, `{}`) **müssen** `#{type = '...'}` angeben: Der Wert einer noml-Datei hat selbst keinen Typ, ein leerer Container
  lässt sich nicht ableiten. Ist ein Objektmember mit `#{type = 'x'}` annotiert, wird `x` direkt verwendet (muss selbst definiert sein), es wird keine
  verschachtelte Struktur mehr automatisch erzeugt.
- Der Strukturname verschachtelter Objekte hat den Dateinamen als Präfix (`server-compiler`); die Definitionsreihenfolge ist zuerst verschachtelt, dann Host.
- **Annotationen am Anfang (Dateiebene) können den Strukturnamen überschreiben**: `#{name = '...'}` (äquivalent `#{type = '...'}`,
  `name` hat Vorrang), auf einer eigenen Zeile über dem äußeren `{` (oder am Zeilenende des äußeren Werts). Nach dem Überschreiben folgen sowohl der Wertbindungsname als auch
  das Präfix verschachtelter Strukturen nach, aber **der Import-Pfad wird weiterhin nach dem Dateinamen aufgelöst**:

```noml
; cfg/db.noml
#{name = 'db-options'}
{
  host: 'localhost'
}
```

```no
# /cfg/db
# /cfg/db as db2

print(db-options.host); localhost
print(db2.host); localhost
```

- Erzeugen zwei verschiedene Dateien denselben Typnamen (etwa gleichnamige Dateien in verschiedenen Verzeichnissen), ist das ein Fehler; mit einem `#{name = '...'}` am Anfang
  unterscheidet man sie; dieselbe mehrfach importierte Datei (z. B. mit zusätzlichem `as`-Alias) wird zu einer einzigen Typdefinition zusammengeführt.
- Schlüsselnamen werden nach Nolang-Regeln in kleingeschriebene Bindestrichform übersetzt (`anonymous-fn-type`): Zahlen am Anfang bekommen `f-` vorangestellt,
  Namen gleich einem Schlüsselwort bekommen `x-`.
- `as`-Aliase dürfen den Wertbindungsnamen ändern, der Strukturname bleibt: `# /cfg/server as cfg`.
- `.noml`-Typen nehmen nicht am Modul-Präfix teil: Selbst wenn der Dateiname mit einem Standardbibliotheksmodul übereinstimmt (wie `server`), bleibt der Typ ein nackter Name.
- `package.noml` / `workspace.noml` können ebenfalls `#{...}`-Annotationen enthalten (wie `#{type = '[]str'}`):
  beim Parsen ignoriert, nur für externe Werkzeuge zum Rekonstruieren stark typisierter Strukturen; die Syntax ist identisch mit Nolangs `#{...}`.

### Module exportieren

Gilt nur für lib.no

```no
@ std/math.add a
```

### FFI (`#{c}`-Annotation)

Über die `#{c}`-Annotation deklariert man externe C-Funktionen und realisiert damit FFI (Foreign Function Interface).

**Syntax**: `#{c}` steht auf eigenständiger Zeile und markiert die nächste Zeile als FFI-Deklaration. `#{c}` ist der FFI-Sprachenschlüssel des Annotationssystems, es werden auch `#{cpp}`, `#{rust}` und andere unterstützt. Die alte Syntax `#c` bleibt rückwärtskompatibel.

**Private Deklarationen**: Namen, die mit `_` beginnen, sind privat (nicht exportiert); das C-ABI-Symbol verliert automatisch das Präfix `_`, und Bindestriche werden zu Unterstrichen.

**Keine getrennten Dateien mehr nötig**: FFI-Deklarationen und normaler Code können in derselben `.no`-Datei stehen.

**Zeiger-Typ-Syntax**: In FFI werden C-artige `*T`, `**T`, `***T` für Zeiger verwendet, dabei muss ein konkreter Typ `T` angegeben sein. Normaler Code darf diese Syntax nicht verwenden.

| Syntax        | Bedeutung          | LLVM IR | Zweck                  
| --------- | ----------- | ------- | ------------------- |
| `*byte`   | Zeiger auf byte | `i8*`   | opaker Zeiger (z. B. db-Handle)  
| `**byte`  | Doppelzeiger        | `i8**`  | Output-Parameter (wie `sqlite3**`) 
| `***byte` | Dreifachzeiger        | `i8***` | seltene dreifache Indirektion            

```no
; sqlite.no — FFI-Bindung und sichere Wrapper in einer Datei
; der Compiler wandelt Bindestriche (-) automatisch in Unterstriche (_), um C-ABI-Symbolen zu entsprechen
; Namen mit führendem _ sind privat, das C-ABI-Symbol verliert automatisch das Präfix _

; einfache Typ-Parameter
#{c}
c-strlen = (s str) (n i64)

; Zeiger-Parameter (*byte = opaker Zeiger), private Deklaration
#{c}
_sqlite3-close = (db *byte) (rc i32)

; Doppelzeiger (**byte = Output-Parameter, Wert wird nach dem Aufruf automatisch in die Variable zurückgespeichert), private Deklaration
#{c}
_sqlite3-open = (filename str, db **byte) (rc i32)

; mehrere Zeiger-Parameter, private Deklaration
#{c}
_sqlite3-exec = (db *byte, sql str, callback *byte, arg *byte, errmsg *byte) (rc i32)
```

```no
; sichere Wrapper in derselben Datei

open = (dsn str) (d db-sqlite) {
    handle i64 = 0
    rc i32 = _sqlite3-open(dsn, handle)
    rc != SQLITE-OK -> {
        return
    }
    d.handle = handle
}
```

**Regeln:**

1. `#{c}` steht auf eigener Zeile und markiert die nächste Zeile als FFI-Deklaration (alte Syntax `#c` bleibt rückwärtskompatibel)
2. FFI ist nur eine Deklaration, ohne Funktionskörper
3. Zeiger müssen einen konkreten Typ haben (wie `*byte`), nacktes `ptr` ist nicht erlaubt
4. `**byte` dient Output-Parametern: Der vom C-Programm geschriebene Zeigerwert wird nach dem Aufruf automatisch in `i64` umgewandelt und in die Aufrufer-Variable zurückgespeichert
5. Alle Zeiger werden auf Nolang-Seite als `i64` gespeichert (`ptrtoint`)
6. `str`-Parameter werden automatisch in nullterminiertes `i8*` umgewandelt
7. Namen mit führendem `_` sind privat (nicht exportiert), das C-ABI-Symbol verliert das Präfix `_`
8. FFI-Deklarationen und normaler Code können in derselben `.no`-Datei stehen

> Die umgekehrte Richtung (C / C++ / Rust / Go / Java / Python / C# rufen Nolang auf) verwendet `#{cabi='symbol'}` und `no build --shared`, siehe [Nolang aus einer Host-Sprache aufrufen](./host-interop).

### Annotationssystem (`#{...}`)

`#{...}` ist ein universelles Annotationssystem, eine durch Kommas getrennte Liste von Schlüssel-Wert-Paaren. Folgende Werttypen werden unterstützt:

| Syntax  | Typ  | Beispiel                                   
| --- | --- | ------------------------------------ |
| standalone key | Boolean | `#{debug}`                           
| Boolean-Wert | Boolean | `#{inline=true}` / `#{inline=false}` 
| Zahl  | Integer | `#{max=100}`                         
| Text  | String  | `#{name='hello'}`                    
| Bezeichner | Bezeichner | `#{mode=fast}`                       
| Array  | Array  | `#{derive=[Serialize, Deserialize]}` 
| Bereich  | Bereich  | `#{range=[0..256)}`                  

Mehrere Schlüssel-Wert-Paare werden durch Kommas getrennt:

```no
#{derive=[Serialize, Deserialize], range=[0..256), max=100, debug}
```

#### Annotationsposition

`#{...}` erlaubt nur zwei Schreibweisen:

- **Darüber**: auf eigener Zeile über dem Ziel (Anweisung, Struktur-Feld, Enum-Mitglied, Deklaration, match-Arm);
- **Danach**: am Zeilenende desselben Ziels (`res = arr[i] #{index-out=zero}`, `a pt #{inline}`,  
  `green #{deprecated}`）。

Direkt **davor auf derselben Zeile** (Präfix, wie `#{index-out=zero} res = arr[i]`) ist ein **Kompilierungsfehler**, Compiler und  
LSP melden dieselbe Diagnose. Entscheidungsregel: Nach der `}` der Annotationsgruppe darf auf derselben Zeile kein Code mehr folgen (Zeilenumbruch,  
`;`-Zeilenkommentar oder eine unmittelbar folgende weitere Annotationsgruppe zählt nicht als Code).

```no
; ✓ darüber: eigene Zeile
#{overflow=wrap}
x i8 = a + 100

; ✓ danach: am Zeilenende
x i8 = a + 100 #{overflow=wrap}
```

```no
#{fmt-skip}
; ✗ davor: Zeilenpräfix → Fehler
#{overflow = wrap} x i8 = a + 100
```

Auch Annotationen auf Strukturfeldern und Enum-Mitgliedern (Werte von C-Style-Enumerationen, Varianten von Tagged Enums) erlauben nur „darüber“ oder „danach“  
（`a pt #{inline}`、`a pt #{inline=true}`、`green #{deprecated}`、`ok(v i64) #{inline}`）。  
Beachte, dass der **Wert** von `#{inline}` ebenfalls unterstützt wird: `#{inline=false}` ist zulässig (siehe unten  
〈`#{inline}` auf Strukturfeldern〉).

In der Schreibweise `if <cond> { ... }` ist eine Annotation zwischen Bedingung und `{` (`if x > 0 #{overflow=wrap} {`) ebenfalls eine  
„davor“-Position und führt zum selben Fehler. Der Scanner an dieser Stelle war früher tolerant und hat die ganze Annotationsgruppe verschluckt — sie wirkte weder, noch wurde ein Fehler gemeldet,
die am schwersten zu entdeckende Form des Versagens.

Die Bereichssyntax unterstützt 16 Klammerkombinationen (Regel: `[` schließt die linke Grenze ein, `(` nicht; `]` schließt die rechte ein, `)` nicht):

**Begrenzte Bereiche (4 Arten)** — beide Enden haben Werte:

- `[a..b]` — beidseits geschlossen (`x >= a && x <= b`)
- `[a..b)` — links geschlossen, rechts offen (`x >= a && x < b`)
- `(a..b]` — links offen, rechts geschlossen (`x > a && x <= b`)
- `(a..b)` — beidseits offen (`x > a && x < b`)

**Bereiche ohne obere Grenze (4 Arten)** — rechtes Ende weggelassen:

- `[a..]`, `[a..)` — links eingeschlossen, keine obere Grenze (`x >= a`)
- `(a..]`, `(a..)` — links nicht eingeschlossen, keine obere Grenze (`x > a`)

**Bereiche ohne untere Grenze (4 Arten)** — linkes Ende weggelassen:

- `[..b]`, `(..b]` — keine untere Grenze, rechts eingeschlossen (`x <= b`)
- `[..b)`, `(..b)` — keine untere Grenze, rechts nicht eingeschlossen (`x < b`)

**Völlig unbegrenzte Bereiche (4 Arten)** — beide Enden weggelassen, basierend auf Typ-Minimum/-Maximum:

- `[..]` — Typ-Minimum ≤ x ≤ Typ-Maximum (beide Enden eingeschlossen)
- `[..)` — Typ-Minimum ≤ x < Typ-Maximum
- `(..]` — Typ-Minimum < x ≤ Typ-Maximum
- `(..)` — Typ-Minimum < x < Typ-Maximum

#### Scope von Annotationen

Die **Position** einer Annotation bestimmt den Host, die **Art** des Hosts bestimmt ihren Gültigkeitsbereich. Es gibt vier Ebenen, sie wirken von außen nach innen,
die näher gelegene überschreibt, der Funktionskörper ist eine harte Vererbungsgrenze:

| Ebene | Host | Gültigkeitsbereich 
| --- | --- | --- |
| **L0 Modul-Ebene** | Dateikopf | alle Deklarationen und Anweisungen dieser Datei (Dateikopf-Annotation + eine Leerzeile) 
| **L1 Deklarations-Ebene** | Konstante/globalen Variable, Struktur, Interface, Tagged Enum, Funktion, Methode | die gesamte Definition dieser Deklaration (Funktion inkl. Körper und allen verschachtelten Blöcken) 
| **L2 Block-Ebene** | Coroutinengruppe, if-Gruppe, `cond -> body`, match, `for`/Zähl-/Bereichsschleife | alle Anweisungen im Blockkörper (inkl. verschachtelter Blöcke, bis zur inneren L1-Grenze) 
| **L3 Zeilen-Ebene** | eine einzelne Anweisung (`let`, Zuweisung, `?=`, `return`, Aufruf, `cond ->`-Arm) | nur diese Anweisung und ihre Ausdrücke 

```no
; L1: gesamte Funktion
#{overflow=wrap}
sum = (xs []i64) (total i64) {
    ; L3: nur die nächste Anweisung
    #{overflow=wrap}
    total = xs.len() - 1
    ; L2: gesamter Schleifenkörper
    #{overflow=wrap}
    i <- [0..xs.len()): {
        total = total + xs[i]
    }
}
```

Die vollständigen Entscheidungsregeln, die Schlüssel-×-Ebenen-Matrix und die Unterschiede zur aktuellen Implementierung stehen in
[〈Scope von Annotationen und Kommentaren〉](annotation-scope.md).

Die FFI-Annotation `#{c}` ist eine Sonderform des Annotationssystems: Enthält eine Annotation einen FFI-Sprachenschlüssel (`c`, `cpp`, `rust` usw.) und folgt eine Funktionsdeklaration, erkennt der Compiler sie als FFI-Bindung:

```no
; #{c} mit zusätzlichen Annotationen
#{c, debug}
_sqlite3-open = (filename str, db **byte) (rc i32)
```

#### Annotationen an Deklarationen anfügen

Nicht-FFI-Annotationen werden automatisch an die unmittelbar folgende Deklaration angefügt (Variablendeklaration, Strukturdefinition) und können verwendet werden, um numerischen Typen (wie `num`, `i64` usw.) Bereichsgrenzen und andere Metadaten zu markieren:

```no
; Variablendeklaration mit range-Annotation
#{range=[0..256)}
x num = 42

; Strukturdefinition mit Annotation
#{derive=[Serialize, Deserialize]}
point {
    x i64
    y i64
}

; Struktur-Feld mit range-Annotation (nutzbar bei num und anderen numerischen Typen)
person {
    age num #{range=[0..150]}
    score i64 #{range=[0..256)}
    name str
}
```

Die `range`-Annotation eignet sich besonders für den Typ `num` (`num = int | float`), um den gültigen Wertebereich einer Zahl zu markieren. Der Bereichswert kann eine ganze Zahl oder ein Bezeichner sein:

```no
; Konstanten-Bezeichner als Bereichsgrenze verwenden
#{range=['i8.MIN'..'i8.MAX']}
val i8 = 100
```

#### Datei einbetten (`#{embed=...}`)

Die Annotation `#{embed=...}` bettet den Inhalt einer externen Datei zur Kompilierzeit als schreibgeschützte `[]byte`-Konstante ein, ähnlich Go `//go:embed`. Die eingebetteten Daten werden direkt in die ausführbare Datei kompiliert, was eine Single-Binary-Auslieferung ohne Dateizugriff zur Laufzeit ermöglicht.

**Syntax**:

```no
#{embed='path/to/file'}
ICON []byte

; oder nackten Pfad verwenden (ohne Anführungszeichen)
#{embed='assets/icon.ico'}
ICON []byte
```

Die `embed`-Annotation wird an die unmittelbar folgende `[]byte`-Variablendeklaration angefügt. Die Variablendeklaration **darf keinen** expliziten Initialwert haben — der Wert stammt aus dem eingebetteten Dateiinhalt.

**Pfadauflösung**:

- Relative Pfade werden relativ zum Paketwurzelverzeichnis aufgelöst (dem Verzeichnis mit `package.noml`)
- Absolute Pfade werden unterstützt
- Pfade dürfen die Zeichen `/`, `.`, `-` enthalten

**Beispiel**:

```no
; Windows-Icon einbetten
#{embed='assets/win-icon.ico'}
WIN-ICON []byte

; SSL-Zertifikat einbetten
#{embed='certs/server.pem'}
SERVER-CERT []byte

; Konfigurationsdatei einbetten
#{embed='config/default.json'}
CONFIG []byte

; eingebettete Daten verwenden
print('icon size: ', len(WIN-ICON))
print('first byte: ', WIN-ICON[0])
```

**Hinweise**:

1. Eingebettete Daten sind **schreibgeschützte** Konstanten und zeigen auf einen nur-lesbaren Speicherbereich. Schreiben in eingebettete Daten ist undefiniertes Verhalten (konsistent mit Go `//go:embed`)
2. Eingebettete Variablen nehmen nicht an Heap-Freigaben teil — beim Programmende wird der Zeiger auf eingebettete Daten nicht gefree't
3. Es werden nur `[]byte`-Deklarationen unterstützt (`[N]byte`-Festarrays gehen auch)
4. Die Datei muss zur Kompilierzeit existieren, sonst wird ein Fehler gemeldet
5. Kann nicht mit einem expliziten Initialwert kombiniert werden (z. B. `ICON []byte = [1, 2, 3]` meldet einen Fehler)

#### Verzeichnis einbetten (`#{embed='dir'}`)

`#{embed=...}` unterstützt auch das Einbetten eines ganzen Verzeichnisses. Zeigt der Pfad auf ein Verzeichnis, liest der Compiler rekursiv alle Dateien darin und bettet sie als schreibgeschütztes Dateisystem vom Typ `fs.embed` ein. Zur Laufzeit greift man über die Methoden `read()` und `exists()` pfadbasiert auf die eingebetteten Dateien zu, ganz ohne C-Funktionen — die Suchlogik ist vollständig in Nolang implementiert.

**Syntax**:

```no
; gesamtes Frontend-Verzeichnis einbetten
#{embed='../frontend/dist'}
DIST fs.embed

; Datei aus dem eingebetteten Dateisystem lesen
data = DIST.read('index.html')

; prüfen, ob die Datei existiert
ok = DIST.exists('css/style.css')
```

**Typ `fs.embed`**:

`fs.embed` ist eine Struktur aus dem Standardbibliotheksmodul `fs` von Nolang und enthält die Metadaten eingebetteter Dateien (Pfad, Offset, Länge). Der Compiler liest zur Kompilierzeit das Verzeichnis und fügt alle Dateiinhalte zu einer globalen Konstante zusammen; zur Laufzeit durchsucht die rein in Nolang implementierte `read`-Methode diese:

| Methode       | Signatur                                  | Beschreibung                
| -------- | ----------------------------------- | ----------------- |
| `read`   | `(path str) (data ?[]byte)`           | eingebettete Datei lesen, Inhalt zurückgeben (nil, wenn nicht gefunden) 
| `exists` | `(path str) (yes bool)`             | prüfen, ob die eingebettete Datei existiert       

**Beispiel**:

```no
; HTTP-Static-File-Server: Frontend-Dateien in eine Single Binary einbetten
#{embed='../frontend/dist'}
DIST fs.embed

serve = (conn server-conn) {
    conn.parse()
    path = conn.path
    path == '/' -> path = '/index.html'

    ; führendes / entfernen, um einen relativen Pfad zu erhalten
    rel = path.slice(1, path.len-bytes())

    ; aus dem eingebetteten Dateisystem lesen
    data []byte = DIST.read(rel)
    {
        ok -> it

        -> {
            conn.write-html(404, 'Not Found')
            conn.close()
            return
        }
    }
    conn.write-status(200)
    conn.write-header('Content-Type', get-content-type(path))
    conn.write-body(data)
    conn.close()
}
```

**Hinweise**:

1. Alle Dateien im Verzeichnis (inkl. Unterverzeichnisse) werden rekursiv eingebettet, die Pfade werden relativ zum eingebetteten Verzeichnis gespeichert (wie `css/style.css`)
2. Pfadtrennzeichen ist immer der Schrägstrich `/` (plattformübergreifend konsistent)
3. `fs.embed`-Typvariablen sind schreibgeschützt und nehmen nicht an Heap-Freigaben teil
4. Geeignet für Single-Binary-Auslieferung (etwa HTTP-Static-File-Server, CLI-Tools mit eingebetteten Ressourcen)

#### Plattform-Annotationen

Plattform-Annotationen sind Kompilierzeit-Filter, die anhand der Zielplattform entscheiden, ob Code aufgenommen wird. Verwenden Sie **flache Schlüssel** (wie `#{mac-arm64}`), die OS und Architektur gleichzeitig und eindeutig angeben, angefügt an die unmittelbar folgende Deklaration. Nicht passender Code nimmt vollständig nicht an der Kompilierung teil — es wird keine LLVM IR erzeugt, keine Typprüfung durchgeführt.

**Unterstützte Plattform-Schlüssel (6 flache Kombinationen):**

| Schlüssel                | Passt auf                         
| ---------------- | -------------------------- |
| `#{linux-amd64}` | Linux x86_64               |
| `#{linux-arm64}` | Linux ARM64                |
| `#{win-amd64}`   | Windows x86_64             |
| `#{win-arm64}`   | Windows ARM64              |
| `#{mac-amd64}`   | macOS x86_64 (Intel)        
| `#{mac-arm64}`   | macOS ARM64 (Apple Silicon) 

```no
#{mac-arm64}
print('running on macOS ARM64')

#{linux-amd64}
print('running on Linux x86_64')

#{win-amd64}
print('running on Windows x86_64')

; plattformspezifische Variable
#{mac-amd64, mac-arm64}
sep = '/'

#{win-amd64, win-arm64}
sep = '\\'

; plattformspezifische Funktion
#{mac-arm64, mac-amd64}
greet = () {
    print('hello from mac')
}

#{linux-amd64, linux-arm64}
greet = () {
    print('hello from linux')
}

greet()
```

Mehrere Schlüssel auf derselben Deklaration haben eine **OR**-Beziehung — passt einer, wird sie aufgenommen. Da jeder Schlüssel bereits OS und Architektur festlegt, ist keine AND-Logik nötig.

| Annotation                          | Bedeutung                                
| --------------------------- | --------------------------------- |
| `#{mac-arm64}`              | nur macOS ARM64                     
| `#{mac-amd64, mac-arm64}`   | macOS, jede Architektur                        
| `#{linux-amd64, win-amd64}` | Linux x86_64 **oder** Windows x86_64 
| `#{mac-arm64, linux-arm64}` | macOS ARM64 **oder** Linux ARM64     

```no
; läuft auf macOS und Linux (alle Architekturen)
#{mac-amd64, mac-arm64, linux-amd64, linux-arm64}
shared = () {
    print('unix-like')
}

; nur Windows x86_64
#{win-amd64}
reg-key = () {
    print('reading registry on win/x64')
}

; nur macOS ARM64 (Apple Silicon)
#{mac-arm64}
neural = () {
    print('Apple Neural Engine available')
}
```

Mit `os.get-arch()` erhält man die aktuelle Architektur zur Laufzeit, mit Plattform-Annotationen wird Code zur Kompilierzeit ein- oder ausgeschlossen.

## package.noml-Compilerkonfiguration

Der `compiler`-Block in `package.noml` steuert das Compilerverhalten. Der `version`-Wert hat conventiongemäß ein führendes `v` (wie `"v0.1.0"`); beim internen Parsen ist das Präfix irrelevant, `"v0.1.0"` und `"0.1.0"` gelten als dieselbe Version.

```noml
{
  compiler: {
    version: 'v0.1.0'
    emit: 'js'
  }
}
```

### `emit`

Steuert das Ausgabe-Backend:

| Wert           | Beschreibung                                      
| ----------- | --------------------------------------- |
| `""` (leer, Standard) | LLVM-Backend verwenden, um eine native ausführbare Datei zu erzeugen                      
| `"js"`      | JS-Backend verwenden, um JavaScript zu erzeugen (Type Erasure), ohne LLVM-Toolchain 

Auf `"js"` gesetzt, verwendet `no build` automatisch das JS-Backend, äquivalent zum Kommandozeilen-Flag `--js`. Kommandozeilen-Flags haben Vorrang vor der Konfigurationsdatei.

```noml
; automatisch JavaScript erzeugen
compiler: {
  emit: 'js'
}
```

### `anonymous-fn-type`

Steuert, ob die Syntax für anonyme Funktionstypen erlaubt ist (wie `cb ()()`). Standard ist `false` (deaktiviert), es muss ein benannter Funktionstyp-Alias verwendet werden.

### `link-libs`

Gibt die Liste der zu linkenden C-Bibliotheken an:

```noml
compiler: {
  link-libs: ['crypto', 'ssl']
}
```

## Compile-Time-Variablen-Injektion (-ld)

`no build`, `no run` und `no test` unterstützen das Injizieren globaler Konstanten zur Kompilierzeit über das Flag `-ld-KEY=VALUE`. Injizierte Variablen entsprechen einer Deklaration `KEY = VALUE` am Dateianfang und können direkt im Programm verwendet werden.

### Syntax

```bash
no build -ld-KEY=VALUE [weitere -ld...] <file>
```

- Dem Präfix `-ld` folgt direkt Variablenname und Wert, getrennt durch `=`
- Mehrere `-ld`-Flags können gleichzeitig verwendet werden
- Boolean-Kurzform: `-ld-DEBUG` (ohne `=VALUE`) entspricht `-ld-DEBUG=true`

### Typableitung des Werts

| Form des Werts             | Abgeleiteter Typ            | Beispiel                  
| ---------------- | --------------- | ------------------- |
| Integer               | `i64`           | `-ld-COUNT=42`      
| Gleitkommazahl              | `f64`           | `-ld-PI=3.14`       
| `true` / `false` | `bool`          | `-ld-DEBUG=true`    |
| Sonstiges               | `str` (String-Literal in einfachen Anführungszeichen) | `-ld-VERSION=0.1.2` 

### Beispiel

```no
; angenommen, kompiliert mit -ld-VERSION=0.1.2 -ld-COUNT=42 -ld-DEBUG=true
print('VERSION:', VERSION); Ausgabe: VERSION: 0.1.2
print('COUNT:', COUNT); Ausgabe: COUNT: 42
print('DEBUG:', DEBUG); Ausgabe: DEBUG: 1 (bool wird im nativen Backend als 1 gedruckt)
```

```bash
; mehrere Variablen injizieren
no build -ld-VERSION=0.1.2 -ld-COUNT=42 -ld-DEBUG=true main.no

; Boolean-Kurzform
no run -ld-RELEASE main.no

; wird auch vom JS-Backend unterstützt
no build --js -ld-VERSION=0.1.2 main.no
```

### Beziehung zu Deklarationen im Quellcode

Die Interaktionsregeln zwischen `-ld`-injizierten Variablen und Deklarationen im Quellcode:

- **Quellcode hat bereits eine gleichnamige Deklaration**: Der injizierte Wert **ersetzt inplace** den Wert dieser Variable im Quellcode. Ein gängiges Muster ist es, eine Platzhaltervariable im Quellcode zu deklarieren (wie `VERSION = ''`) und sie beim Build mit `-ld-VERSION=0.1.2` zu belegen.
- **Existiert im Quellcode nicht**: Die injizierte Variable wird ganz vorne an den AST gestellt als neue globale Konstante und nimmt an allen nachfolgenden Compiler-Pässen teil (Typableitung, Verifikation, Modulzusammenführung, codegen).
- **Nicht injiziert**: Die Deklaration im Quellcode bleibt unverändert.

```no
; Platzhaltervariable im Quellcode deklarieren
VERSION = ''
COUNT = 0

print('VERSION:', VERSION)
print('COUNT:', COUNT)

; no build -ld-VERSION=0.1.2 main.no
; Ausgabe: VERSION: 0.1.2  (injizierter Wert hat das '' im Quellcode überschrieben)
;       COUNT: 0        (nicht injiziert, behält den Quellcode-Wert)
```
