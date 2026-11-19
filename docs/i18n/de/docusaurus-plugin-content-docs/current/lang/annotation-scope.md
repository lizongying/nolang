---
sidebar_position: 2.5
---

# Geltungsbereich von Annotationen und Kommentaren

`#{...}`-Annotationen selbst tragen keine Information darüber, „wie groß ihr Geltungsbereich ist" — der Bereich wird dadurch bestimmt, **an welchem Knoten sie haften**.
Diese Seite legt dies fest: eine Position, eine Entscheidungsregel, vier Ebenen, zusätzlich die Regel, dass „ein Schlüssel nur einige dieser Ebenen unterstützen kann"
(bei `overflow` ist es derjenige, der nur L3 akzeptiert, siehe §4.1), damit Autor, `no fmt`, `no vet`, Codegen und LSP
für dieselbe Annotationszeile die gleiche Antwort geben.

> Warum das festgelegt werden muss: Bei derselben `#{overflow=wrap}` könnten `no vet` meinen, sie gelte für die gesamte Funktion, Codegen
> meinen, sie gelte nur für die nächste Zeile — dann entstünde eine stille semantische Drift mit „vet völlig grün, zur Laufzeit trotzdem `err`".
> Der Geltungsbereich ist keine Eigenschaft der Annotation, sondern ein **Konsens unter den Lesern**; wird dieser Konsens nicht schriftlich festgehalten, driftet jeder auseinander.

---

## 1. Ein Grundsatz: Der Geltungsbereich wird vom Wirt bestimmt

Annotationen gibt es nur an zwei erlaubten Positionen (die Positionsregeln selbst stehen in [`syntax.md`](syntax.md) unter 〈Annotationsposition〉):

```no
; ① 上方：獨佔一行，置於目標之上
#{overflow=wrap}
x i8 = a + b

; ② 尾隨：寫在目標同一行的後方
x i8 = a + b #{overflow=wrap}

; 多行目標（函式、區塊）的尾隨寫在收尾 `}` 之後
greet = () {
    print('hi')
} #{mac-arm64}
```

**Beide sind völlig gleichwertig** — derselbe Schlüssel, derselbe Wirt, derselbe Geltungsbereich, und `no fmt` schreibt die eine Form nicht in die
andere um (nachgestellt bleibt nachgestellt, darüber bleibt darüber). Die Wahl richtet sich nur nach Lesbarkeit: Bei kurzen Anweisungen spart die nachgestellte Form eine Zeile,
bei einer Anmerkung zu einem ganzen Ausdrucksteil oder bei langen Anweisungen verwendet man die Form darüber.

**Das Schreiben auf derselben Zeile vor dem Ziel ist ein Kompilierungsfehler**; es gibt keinen Weg à la „Präfix wirkt nach hinten":

```no
#{overflow = wrap} x i8 = a + b   ; ✗ 前綴 → 報錯
```

Die Entscheidung erfolgt in drei Schritten, wobei jeder Schritt nur eine Antwort zulässt:

| Schritt | Frage | Quelle der Antwort |
| --- | --- | --- |
| 1 | Wer ist der Wirt? | Position: die erste Anweisung darüber, die keine Annotations-/Kommentarzeile ist, oder die vorhergehende Anweisung in derselben Zeile |
| 2 | Zu welcher Ebene gehört der Wirt? Unterstützt der Schlüssel diese Ebene? | Tabelle in §2 + Matrix in §4 (nicht unterstützt = Kompilierungsfehler) |
| 3 | Wie weit reicht die Abdeckung? | Teilbaum des Wirts, abzüglich innerer harter Grenzen (§3) |

---

## 2. Die vier Ebenen

| Ebene | Wirt | Wirksamer Bereich | Schreibbeispiel |
| --- | --- | --- | --- |
| **L0 Modul-Ebene** | Dateikopf | Alle Deklarationen und Anweisungen dieser Datei | Ganz oben in der Datei, mit einer Leerzeile zum Folgenden |
| **L1 Deklarations-Ebene** | Konstanten/globale Variablen, Strukturen, Interfaces, Tagged Enums, Funktionen, Methoden | Die gesamte Definition dieser Deklaration (Funktion inkl. Funktionskörper und aller verschachtelter Blöcke) | Direkt über der Deklaration oder nachgestellt in der Deklarationszeile |
| **L2 Block-Ebene** | Coroutine-Gruppen, if-Gruppen, `cond -> body`, match, `for`/Zähl-/Bereichsschleifen, nackte Blöcke | Alle Anweisungen im Blockkörper (inkl. verschachtelter Blöcke, endend an der inneren L1-Grenze) | Direkt über der den Blockkopf bildenden Anweisung oder nach dem abschließenden `}` |
| **L3 Zeilen-Ebene** | Einzelne Anweisung (`let`, Zuweisung, `?=`, `return`, Aufruf, `cond ->`-Arm) | Die Anweisung selbst und ihre Ausdrücke | Direkt über der Anweisung oder in derselben Zeile nachgestellt |

### L0 Modul-Ebene

Die Position ist **der ganz Anfang der Datei** (davor stehen keine Anweisungen oder Imports), und zwischen der Annotationsgruppe und der ersten Anweisung liegt
**mindestens eine Leerzeile** — die Leerzeile wirkt hier als „Geltungsbereichs-Terminator" und bedeutet, dass diese Annotationsgruppe nicht zur nächsten Anweisung gehört,
sondern zur gesamten Datei.

```no
#{win-amd64}

; ↑ 空行 → 模組級：本檔只在 Windows x86_64 編譯
; 以下所有宣告與語句都在管轄內
helper = () {
    print('windows only')
}

label = 'win'
```

Ohne Leerzeile liegt ganz normal L1 vor (haftet an der ersten Deklaration); diese Grenze bedeutet, dass bestehender Code nicht migriert werden muss:

```no
#{buildin}
bool.to-str = () (out str) {
}
```

Die Modul-Ebene ist nur für Schlüssel sinnvoll, die „für die gesamte Datei von Bedeutung sind" (derzeit nur Plattform-Schlüssel); Deklarationsschlüssel (`buildin`, `generic`,
`embed` …) sind in L0 ein Fehler, keine „Erweiterung des Geltungsbereichs". `overflow` akzeptiert L0 ebenfalls nicht — es erkennt nur L3.

Dateiübergreifende Konfiguration gehört nicht zu Annotationen und wird in `package.noml` geschrieben.

### L1 Deklarations-Ebene

```no
; 整個結構體：欄位佈局由型別標記決定
#{inline}
holder {
    inl point
    out point
}

; 整個函式：進出日誌自動帶呼叫者
#{track-caller}
log.write = (msg str) {
    ...
}

; 整個常量宣告：嵌入目錄（路徑相對 package.noml）
#{embed='assets'}
ASSETS fs.embed

; 編譯期剔除整棵子樹（含函式體）
#{mac-arm64, linux-arm64}
greet = () {
    print('hello from unix')
}
```

Innerhalb von L1 gibt es zusätzlich **Mitgliedergranularität**: Annotationen an Strukturfeldern, Tagged-Enum-Varianten und C-Enum-Mitgliedern
(`a pt #{inline}`, `ok(v i64) #{inline}`) betreffen dieses Mitglied, nicht den gesamten Typ. Mitgliederannotationen
haben ebenfalls nur zwei Positionen: eine Zeile direkt über dem Mitglied oder hinter dem Mitglied in derselben Zeile (nachgestellt ist üblich).

### L2 Block-Ebene

Ist der Wirt eine „Blockkopf-Anweisung", ist der Geltungsbereich der gesamte Blockkörper:

```no
; 整塊被排除（不生成 IR，也不做型別檢查）
#{win-amd64}
2 * {
    print('windows only')
}

; if 組同樣以整組為單位
x = 1
#{win-amd64}
{
    x == 1 -> print('arm-1')
    -> print('arm-other')
}
```

Alle vier Blockkopf-Formen gelten: Coroutine-Gruppen (`awy { ... }`), if-Gruppen (`{ cond -> … }`), Einzelbedingungsanweisungen
(`cond -> body`), match sowie `for`-/Zähl-/Bereichsschleifen.

Ist der Body von `cond -> body` nur eine einzelne Anweisung, liefern L2 und L3 dasselbe Ergebnis; einen Unterschied gibt es erst bei Blockform.

**Nicht jeder Schlüssel nutzt alle vier Ebenen voll aus.** Ein Schlüssel kann nur einige Ebenen unterstützen; das Schreiben auf einer nicht unterstützten Ebene ist ein **Kompilierungsfehler**,
keine „harmlose Redundanz durch eine zusätzliche Zeile". Derzeit ist `overflow` der einzige eingeschränkte Schlüssel: Er erkennt nur L3, die Begründung steht in §4.1.

### L3 Zeilen-Ebene

```no
a i8 = 100
b i8 = 100
; 只管緊跟的下一條陳述（尾隨寫法 `c = a + b #{overflow=wrap}` 等價）
#{overflow=wrap}
c = a + b
c: {
    ok -> print('wrap:' - (it == -56).to-str())
    nil -> print('nil')
    err -> print('err')
}
```

Eine innerhalb eines Blocks eigenständig stehende Annotation erkennt nur „die unmittelbar darauffolgende Anweisung". Dazwischenliegende irrelevante Annotationszeilen
(etwa ein weiteres `#{...}` nach `#{overflow=wrap}`) unterbrechen die Suche nicht; die erste echte Anweisung wird dennoch gefunden;
hat aber die nächste Annotation denselben Schlüssel, kontrolliert die erstere keine Anweisung (die letztere gewinnt).

⚠️ **Block-match `{ … }: { … }` ist „eine Anweisung" und wird nicht durchdrungen**: Steht die Annotation über seinem `{`,
ist der Wirt die Blockanweisung selbst und die Annotation **steigt nicht** in den `?=`-Ausdruck innerhalb des Blocks ab (Messung: `#{overflow=wrap}` außerhalb des Blocks
führt bei `np ?= np + 1` im Block weiterhin zu Überlauf → `err`, Ziel wird auf null gesetzt). Um `overflow` auf einen Ausdruck im Block anzuwenden, schreibt man es
direkt über der betreffenden `?=`-Anweisung im Block. Die Unterschiede zwischen den beiden Konsumformen siehe [syntax.md](syntax.md) unter
〈Abwägung zwischen Block-match und Wert-match〉.

---

## 3. Vererbung, Überschreibung und harte Grenzen

**Vererbung von außen nach innen, Überschreibung durch die nächste Ebene.** Bei Schlüsseln mit Unterstützung für mehrere Ebenen (Plattform-Schlüssel) wirken L0 → L1 → L2 → L3 Ebene für Ebene;
Am selben Knoten überschreibt bei gleichnamigen Schlüsseln der letztere den ersteren (`#{mac-arm64} #{mac-arm64, win-amd64}`
gilt der letztere).

**Der Funktionskörper ist eine harte Grenze.** Der Geltungsbereich eines L2-Blocks endet am **syntaktischen Teilbaum**: Er breitet sich nicht über Aufrufbeziehungen aus und dringt nicht in
den Körper von Funktionen/Methoden/anonymen Funktionen ein. Welcher Modus in einem Funktionskörper gilt, bestimmen nur dessen eigene L1-Annotation oder interne
L3-Annotationen — unabhängig von den Annotationen an dem Block, der ihn aufruft.

```no
; L1：整個函式連同函式體一起被剔除（macOS 上不生成 IR）
#{win-amd64}
windows-only = () {
    print('never compiled on mac')
}

; 沒有自己的 L1 註解 → 任何平台都編譯，也不會因為被誰呼叫而改變
always = () {
    print('always compiled')
}
```

Begründung: Könnte eine äußere Annotation in den Funktionskörper durchdringen, würde eine einzige Zeile an der Aufrufstelle die Semantik der aufgerufenen Funktion still verändern — beim Debuggen
wäre das völlig unsichtbar. L1-Annotationen verhalten sich hingegen umgekehrt: Sie stehen direkt am Funktionskopf und sind Teil der Deklaration.

**Liste der harten Grenzen** (hier endet die Vererbung): Funktionskörper, Methodenkörper, Körper anonymer Funktionen/Lambdas, externe Deklarationen (`#{c}`).
**Keine Grenzen** (Vererbung erfolgt normal): nackte `{}`-Blöcke, if-Gruppen, match-Arme, Schleifenkörper.

---

## 4. Matrix: Schlüssel × Ebene

`✓` wirksam / `✗` unzulässig (sollte Kompilierungsfehler melden) / `—` für diese Ebene bedeutungslos (sollte ebenfalls einen Fehler melden, statt still verworfen zu werden)

| Schlüssel | L0 Modul | L1 Deklaration | L2 Block | L3 Zeile |
| --- | --- | --- | --- | --- |
| `overflow` | ✗ | ✗ | ✗ | ✓ |
| Plattform-Schlüssel (`mac-arm64` usw.) | ✓ | ✓ | ✓ | ✓ |
| `buildin` | ✗ | ✓ Funktion / Tagged Enum / Layout-Struktur | ✗ | ✗ |
| `inline` | ✗ | ✓ Struktur / Feld / Variante / Funktion | ✗ | ✓ nachgestellt am Mitglied |
| `generic` | ✗ | ✓ Funktion / Struktur | ✗ | ✗ |
| `track-caller` | ✗ | ✓ Funktion | ✗ | ✗ |
| `embed` | ✗ | ✓ Top-Level-`let` | ✗ | ✗ |
| `bind` | ✗ | ✓ Importzeile | ✗ | ✗ |
| `c` / `cpp` / `rust` | ✗ | ✓ unmittelbar folgende Funktionsdeklaration | ✗ | ✗ |
| `fmt-skip` | ✗ | ✗ | ✓ Markdown-Fence-Block | ✗ |
| Metadaten (`derive` / `range` / `max` / `debug` …) | ✗ | ✓ Deklaration / Mitglied | ✗ | ✗ |
| `index-out` | — | — | — | — in D11 entfernt, Auftreten meldet Fehler |

Sowohl „unzulässig" als auch „bedeutungslos" müssen **einen Fehler melden** und dürfen nicht still wirkungslos bleiben: Eine über einer for-Schleife stehende
`#{buildin}`, die still ignoriert würde, ließe den Autor glauben, die Builtin-Deklaration sei wirksam.

### 4.0 Die drei Träger von `#{buildin}` und die Besonderheit der „Layout-Struktur"

`#{buildin}` steht an einer Deklaration, hat aber für drei Träger völlig unterschiedliche Semantik:

| Träger | Semantik | Beispiel |
| --- | --- | --- |
| Funktion | Leerer Stub: Compiler / FFI stellen die Implementierung bereit, auf Nolang-Seite wird kein Funktionskörper erzeugt | `#{buildin}`<br/>`fs.remove = (p str) (ok bool) { }` |
| Tagged Enum | Varianten für Pattern Matching und Erschöpfheitsprüfung, zugrunde liegende Darstellung und Konstruktion durch eingebaute Runtime bereitgestellt | `#{buildin}`<br/>`option { ok(v t), nil, err(e err.err), }` |
| **Layout-Struktur** | **nur Layout-Dokumentation**: Dieses `struct` existiert semantisch nicht, das einzige Ergebnis sind die Feldtypen (`t.len` ist `u8`) | `#{buildin}`<br/>`txt { data [255]byte; len u8 }` |

Die dritte Form ist gesondert zu erklären, weil sie in die entgegengesetzte Richtung geht: Funktionsstubs und Tagged Enums sind Fälle, in denen „der Compiler tatsächlich
diese Deklaration verwenden will", während die Layout-Struktur bedeutet: „Der Compiler **verwendet** diese Deklaration **nicht**, sie ist nur die Quelle der Feldtypen und
eine lesbare Layout-Beschreibung". Die tatsächliche Layout-Definition und die Dokumentations-Links liegen anderswo:

| Typ | Quelle des tatsächlichen Layouts | Quelle der Dokumentation / Links |
| --- | --- | --- |
| `str` | Handgeschriebener MIR-Deskriptor `%str-long = { i32 len, i32 cap, i8* data }` | `parser.BuiltinTypes()` |
| `vec` | Handgeschriebener MIR-Deskriptor `%vec = { i32 len, i32 cap, i64 data }` | `parser.BuiltinTypes()` |
| `txt` | Handgeschriebener MIR-Deskriptor `%txt = { [255 x i8] data, i8 len }` | `parser.BuiltinTypes()` |
| `arr` | Kein Deskriptor: `[n]t` ist die nackte Aggregatstruktur `[N x T]`, `.len` ist die Kompilierzeitkonstante `n` | `parser.BuiltinTypes()` |

`parser.BuiltinTypes()` (`src/parser/builtintype.go`) ist die einzige Registrierungstabelle für „Name → std-Modul → Moduldokumentation
→ Anker → Layout"; LSP-Hover / Sprung zur Definition / Vervollständigung und `IsBuiltinLayoutStructName`
lesen alle daraus. Genau deshalb trägt die Layout-Struktur **nicht mehr zur Verlinkung bei**: Löscht man sie, funktionieren Hover und F12 weiterhin;
nur verliert der Checker den Feldtyp von `t.len`, deshalb wird sie derzeit beibehalten.

Daraus ergeben sich zwei harte Regeln; jede Verletzung erzeugt sofort eine „Schattenstruktur", die der tatsächlichen Layout-Definition widerspricht:

1. **Ein struct mit `#{buildin}` muss einen eingebauten Layout-Namen verwenden** (`str` / `vec` / `txt` / `arr`).
   Wird es an einem anderen struct angebracht, ist das ein Fehler: Die MIR behandelt die Deklaration als „Layout liegt beim Compiler" und überspringt sie,
   alle Felder werden unwirksam, und der Autor erhält eine Reihe von `unknown field`.
2. **Ein struct mit eingebautem Layout-Namen muss `#{buildin}` tragen**. Schreibt ein Benutzer ein nacktes
   `str { … }`, wird es als gewöhnlicher Typ dieses Moduls mit Präfix versehen (`m.str`), und Codegen erzeugt dadurch
   ein `%m_str`, das mit `%str-long` kollidiert.

Beide werden von `checker.ValidateBuiltinStructDecls` gemeldet (`1a8m445h` / `3lu6n4np`),
sie werden sowohl von `no build` als auch von `no vet` erkannt. Umgekehrt wird **nur** die Kombination aus `#{buildin}` + Layout-Name übersprungen
von der Modul-Präfix-Phase und von MIRs `collectStructFields` (siehe `src/build/module_prefix.go`
Phase 1 sowie `isBuiltinLayoutStruct` in `src/mir/hir2mir.go`) — das Kriterium ist die Kombination „Annotation +
Name", nicht der Name allein.

### 4.1 Warum `overflow` nur L3 erhält

`#{overflow}` ist ein **pro Operation** arbeitender semantischer Schalter, kein Metadatum einer Deklaration. Würde man das Schreiben an Funktionen oder Blöcke erlauben,
hinge „welcher Modus eine bestimmte Operation tatsächlich verwendet" davon ab, wie weit sie von der Annotation entfernt ist, ob dazwischen andere Annotationen stehen und wie viele Blöcke verschachtelt sind —
müsste man zum Verständnis einer Codezeile nach oben zurückverfolgen, und für `no fmt` / `no vet` / Codegen wäre es äußerst schwer, dieselbe Antwort aufrechtzuerhalten
(in der Praxis ist bereits eine Drift aufgetreten, siehe §7).

Außerdem **ändert es den Typ nicht**: Das Ergebnis bleibt stets `?T`. Ein `wrap` auf Block- oder Funktionsebene macht aus `?T` kein `T`, sondern ersetzt nur
„Überlauf bedeutet `err`" stillschweigend durch „stilles Zurückwickeln" — der Fehler wandelt sich von „explizitem Scheitern" zu „einem falschen Wert, der erfolgreich aussieht",
und genau das sollte am wenigsten von einer weit entfernten Annotationszeile bestimmt werden.

Daher ist es auf eine Annotation pro Zeile festgelegt:

- Es wird nur die unmittelbar folgende Anweisung erkannt; die nachgestellte Form (`x = a + b #{overflow=wrap}`) ist gleichwertig.
- Soll ein ganzer Bereich wirksam sein, schreibt man es für jede Anweisung einzeln — sowohl `no fmt --fix=overflow` als auch der LSP-Quickfix erzeugen diese Form.
- Das Schreiben am Funktionskopf, Blockkopf oder Dateikopf führt zu einem **Kompilierungsfehler**, nicht zu „unwirksam, aber harmlos".

Nebenvorteil: Die Annotation steht immer im selben Bildausschnitt wie die Anweisung, die sie steuert; beim Verschieben von Code trennen sie sich nicht voneinander;
`wrap` deckt auch nicht durch eine bloße Einrückungsänderung versehentlich eine benachbarte Operation ab.

---

## 5. Geltungsbereich von Kommentaren (`;` / `//` / `;;`)

Kommentare nehmen nicht an der Kompilierungssemantik teil; ihre Zuordnung beeinflusst nur das Layout durch `no fmt` und die Einbindung in die API-Dokumentation. Die Markierungssyntax siehe
[`code-style.md`](code-style.md) unter 〈Kommentarmarkierungen〉.

| Kommentar | Zuordnung |
| --- | --- |
| Inline (`x = 1 ; Erläuterung`) | Diese Zeile |
| Aufeinanderfolgende Kommentarzeilen über einer Zeile | Die unmittelbar nächste Anweisung/Deklaration (L1 oder L3, nach derselben Wirtsregel wie Annotationen) |
| Leerzeile zwischen Kommentar und Ziel | Keiner Zuordnung zu einem Knoten, dient als trennende Erläuterung für Block/Datei (außer am Dateikopf, siehe L0) |
| `;; … ;;`-Blockkommentar | Endet am eigenen schließenden Begrenzer, überschreitet keine Geltungsbereiche |
| Kommentar zwischen Annotation und Ziel | Wandert mit dem Ziel, die Ausgabereihenfolge ist fest „Dokumentationskommentar → Annotation → Anweisung" |

`;` und `//` sind semantisch identisch; `no fmt` behält die ursprüngliche Markierung bei und schreibt sie nicht um. Der Übersetzungsanker eines Dokumentationskommentars
(`[get](#args.get)`) muss die **letzte Zeile** der Kommentargruppe sein, demselben Wirt angehören und darf nicht über Knoten hinweggehen.

---

## 6. Vier Leser müssen übereinstimmen

Für dieselbe Annotation müssen vier Leser zum selben Geltungsbereich kommen:

| Leser | Was er wissen muss |
| --- | --- |
| Parser / Codegen | Welcher Modus für diesen Knoten und seinen Teilbaum gilt |
| Checker / `no vet` | Der Bereich, in dem „annotiert" gilt |
| `no fmt` | Annotationen werden an ihrer ursprünglichen Position ausgegeben und dürfen nicht über Wirte hinweg verschoben werden |
| LSP | Die Position, an die Hover/Quickfix einfügt, entspricht der Position, an der die Annotation tatsächlich wirkt |

Daraus folgen vier Invarianten:

1. **Konsistente Annotationserkennung**: Der Bereich, den Codegen als „annotiert" ansieht, ist zwangsläufig auch der Bereich, den `no vet` als behandelt ansieht — und umgekehrt.
2. **fmt verschiebt nicht**: `no fmt` darf Positionen nur innerhalb desselben Wirts anpassen (darüber ↔ nachgestellt); ein Verschieben über Wirte hinweg bedeutet eine Änderung der Semantik.
3. **Ungültigkeit muss gemeldet werden**: Landet eine Annotation auf einer Ebene ohne passende Semantik (siehe `✗` / `—` in §4), ist ein Fehler zu melden, kein stilles Ignorieren.
4. **Ebenenbeschränkung**: Ein Schlüssel wirkt nur auf den in der Matrix erlaubten Ebenen. Wird `overflow` in L0/L1/L2 geschrieben, müssen alle vier Leser
   es als **Fehler** behandeln — eine Aufspaltung à la „vet akzeptiert, Codegen nicht" darf nicht auftreten (aktueller Stand siehe §7).

---

## 7. Aktueller Stand und Abweichungen (Messung vom 2026-11-11)

Die Überprüfung erfolgte Punkt für Punkt mit `bin/no`, wobei durchgängig die Form „zwei `i8 = 100` werden addiert, und das Ergebnis ist `err` oder `-56`
(wrap)" verwendet wurde; Plattform-Schlüssel wurden mit `#{win-amd64}` auf macOS darauf getestet, ob sie ausgeschlossen werden.

| Ebene | `overflow` (Design: nur L3) | Plattform-Schlüssel | Deklarationsschlüssel |
| --- | --- | --- | --- |
| L0 Modul | ✗ designmäßig verboten; derzeit haftet es an der ersten Anweisung (entspricht L1) | Nicht implementiert | Nicht implementiert |
| L1 Deklaration | ✗ designmäßig verboten; **aktuell ein Loch**: `no vet` akzeptiert (gesamte Funktion übersprungen), Codegen nicht (weiterhin `err`) | ✓ gesamter Teilbaum ausgeschlossen | ✓ |
| L2 Block | ✗ designmäßig verboten; Codegen akzeptiert nicht. `for`-, if-Gruppen- / match-Armköpfe können `no vet` dazu bringen, den Block zum Schweigen zu bringen; **ein nackter `{` an Anweisungsposition schafft nicht einmal das** (siehe unten) | ✗ im Block wird weiterhin kompiliert | — |
| L3 Zeile | ✓ | ✓ | — |

Messbefunde:

```text
a-none.no    (無註解)                     → err
b-line.no    (陳述上方 #{overflow=wrap})   → result:true  (-56，生效)
c-fn.no      (函式上方 #{overflow=wrap})   → err           (vet: No issues found)
d-block.no   (for 上方 #{overflow=wrap})   → err ×2        (vet: 2 hints)

p1-block.no  (#{win-amd64} + 2 * {…})      → 塊體仍執行    (未過濾)
p2-stmt.no   (#{win-amd64} + 單條 print)   → 該行被排除    (生效)
p4-fn.no     (#{win-amd64} + 函式宣告)     → 函式被排除    (生效)
p6-ifgroup.no(#{win-amd64} + if 組)        → 臂仍執行      (未過濾)

; 編譯期對照：`for` 頭加註解 vs 不加，15w45dqk 逐字相同
k2.no (for 頭 #{overflow=wrap} + 體內 `x i8 = a + a`) → cannot assign ?i8 to i8
k3.no (刪掉那一行，其餘逐字相同)                       → cannot assign ?i8 to i8
```

Warum ein nackter Block nicht einmal zum Schweigen gebracht werden kann: Die anfügbaren Ziele von `parseAnnotationStatement` sind nur IDENT / USE /
IN / RARROW / Array-Typmethoden; `{` ist nicht darunter. Daher haftet die Annotation in der folgenden Schreibweise weder am Block noch
an einer Anweisung im Block — sie degeneriert zu einer eigenständigen `AnnotationStatement` und ist für Parser, Codegen
und `no vet` eine tote Zeile:

```no
#{overflow=wrap}
{
    ...
}
```

Die Köpfe von `for`-, if-Gruppen und match-Armen beginnen mit einem Bezeichner oder `in` und liegen genau deshalb im Bereich der anfügbaren Ziele.

Drei zu schließende Lücken (die Beschränkung von `overflow` auf L3 ist noch nicht erzwungen):

1. **`no vet` überspringt die gesamte Funktion, sobald es ein `overflow` auf Funktionsebene sieht** (Kurzschluss von
   `fn.OverflowMode == ""` in `walkStmtForOverflow`) → Prüfung völlig grün, zur Laufzeit weiterhin `err`.
2. **`no vet` vererbt in Blöcke nach unten** (`enclosingOverflow`) → eine einzige Annotation am Blockkopf bringt den gesamten Block zum Schweigen,
   selbst wenn Codegen es überhaupt nicht akzeptiert.
3. **Parser / Codegen ignorieren `overflow` in L1/L2 stillschweigend**, ohne Fehler zu melden → der Autor erfährt nicht, dass es nicht wirkt.

Bestandsaufnahme und Behandlung (`src/std`, drei Durchgänge mit `no fmt --fix=overflow` zur Herabsetzung):

| Kategorie | Vor Migration | Nach Runde 1 | Nach Runde 2 | Nach Runde 3 |
| --- | --- | --- | --- | --- |
| `overflow`-Annotationszeilen insgesamt | 4580 | 4083 | 3861 | 4080 |
| Davon tote Zeilen, die „an keinem Knoten haften" | 330 (nach Form gezählt) | 87 | **0** | **0** |
| Von `no vet` gemeldet, aber nicht annotierbar | — | ~270 | 270 | **51** |

Runde 1 (2026-11-11), Nettoveränderung: 816 Zeilen mit Geltungsbereichs-Annotationen gelöscht, 325 Zeilen mit Einzelannotationen eingefügt. Nach A/B aufgeschlüsselt:
von den 325 Zeilen betreffen etwa 218 Stellen, denen ohnehin die Markierung fehlte und die nichts mit der Herabsetzung zu tun haben (sie würden auch bei nur eingefügten, nicht entfernten Überschattungen ergänzt),
etwa 97 Zeilen sind Blockabsichten, die in Einzelannotationen materialisiert wurden.

Runde 2 (2026-11-12) war eine **reine Bereinigung**, verursacht dadurch, dass `findFixTargetNode` nicht in Armkörper hinabstieg und keine „Selbstprüfung nach dem Einfügen" gab:
381 gelöschte, 159 hinzugefügte Zeilen, **ausschließlich `#{...}`-Annotationszeilen, 0 Codezeilen** (Zeile für Zeile per `diff` geprüft). Davon
sind 159 Zeilen Verschiebungen „vom Block-/Armkopf über die Anweisung", die übrigen 222 Zeilen entfernen tote Zeilen und überflüssige Geltungsbereichs-Annotationen direkt,
ohne Ersetzung — diese Positionen waren ohnehin unwirksam, das Löschen ändert kein Kompilierungs- oder Prüfergebnis.

Drei Werkzeugfehler, die in Runde 2 ebenfalls behoben wurden:

1. **Der vom `checker` gemeldete Zeilennummer kollabiert auf den Container**: Das `emit` von `walkExprForIntOverflow` vermischte die
   `stmt.Pos().Line` des Containers mit der Spalte des Operators, sodass jede Meldung in `{ ... }` / if-Gruppen / match-Armen
   dieselbe Zeilennummer wie der Header hatte. Die falsche Diagnoseposition ist nur ein Symptom; das eigentliche Problem ist, dass `--fix` mit `findFixTargetNode`
   „die innerste Anweisung, die diese Zeile enthält" auswählte und damit nur den Container zurückbekam, wodurch die Annotation wieder am Blockkopf landete. Jetzt wird **die Zeile des Operators selbst** gewählt
   (`x.Token.Line`), mit Rückfall auf die Containerzeile, falls die Token-Zeilennummer fehlt.
2. **Kein Abstieg in Armkörper**: `findFixTargetNode` blieb ursprünglich bewusst über der gesamten match-Anweisung stehen. Jetzt wird in
   **Armkörper in Blockform** hinabgestiegen (`{ ... }` bei `cond -> {`, einschließlich `BlockMatchStatement.Body` bei `{ ... }: { ... }`),
   Inline-Armkörper werden weiterhin nicht durchsucht (bei `w i64 -> w + 1` stehen Anweisung und Armmuster in derselben Zeile; ein Einfügen darüber hieße, in die match-Anweisung hineinzufügen,
   vor der Zeile des Armmusters zu landen).
3. **Einfügen auch an Stellen, an denen nichts haftet**: `#{overflow}` führt nach dem Einfügen jetzt eine **Reparse-Selbstprüfung** durch und zieht Annotationen zurück, die „eingefügt wurden, aber
   zu einer eigenständigen `AnnotationStatement` degeneriert sind" (es bleiben keine toten Zeilen mehr). Nach dem Zurückziehen erhält die zweite `--fix`-Runde
   den Quellcode unverändert, die Idempotenz bleibt erhalten.

Runde 3 (2026-11-12) betraf den Parser: Den anfügbaren Zielen von `parseAnnotationStatement` wurde **DOT** hinzugefügt.
`.n = .n + 1` / `.cnt == 0 -> …` sind Anweisungen, die mit einem Empfänger beginnen, und gehören derselben Kategorie an wie `x = …` (einzelne Anweisung, L3),
waren aber früher nicht unter den anfügbaren Zielen ⇒ die Annotation degenerierte zu einer eigenständigen `AnnotationStatement` (tote Zeile), die Stelle wurde dauerhaft gemeldet,
und auch vom `--fix` Eingefügtes wurde durch die Selbstprüfung zurückgezogen. Nach der Lockerung erhielten 219 Stellen eine L3-Annotation (Annotationszeilen 3861 → 4080),
nicht annotierbare Stellen sanken von 270 auf 51. Die AST-Form änderte sich dadurch (eine AnnotationStatement weniger, eine zusätzliche Side-Table-Eintragung und
`OverflowMode` bei der Zielanweisung), `parser.ASTVersion` wurde auf `2026-11-12.1` angehoben, danach muss
`make gen no` ausgeführt werden.

**`{` und `(` wurden bewusst nicht gelockert**: `{` (nackter Block an Anweisungsposition / match mit Klammern) und `(` (`(cond) { }`-Schleifenkopf)
sind L2-Blockwirte, während `overflow` der Auslegung nach nur L3 erkennt. Würde man sie zulassen, würde `overflowAnnotatedNode`
für den gesamten Block vorzeitig zurückkehren, was L2-Semantik legalisieren würde — im Widerspruch zur Reihenfolge „zuerst verschärfen, dann lockern" in §8. Die verbleibenden 51 Stellen
gehören größtenteils zu diesen beiden Kategorien und fallen unter Schritt 4b in §8.

Abnahmekriterium (isolierte Bäume A/B, gleicher Quellstand und Compiler, Unterschied nur `src/std`, `no vet` dateiweise): 73 betroffene Dateien
**alle 0 Fehler** (kein Kompilierungsbruch), 25 Dateien mit weniger Diagnosen, 19 mit mehr — die Zunahmen sind ausschließlich nach dem Entfernen der Überschattung freigelegte
`ovfhndld`-Meldungen („an `?T` gebunden, aber keine Überlauf-Entnahme deklariert"), wie in Schritt 1 von §8 erwartet, keine neuen Defekte.
Weitere 9 Warnungen vom Typ `nolang-overflow-ineffective` sind ein altes Problem des werkzeugseitigen „Lückenfüllens" bei bigint-/
String-Überladungsoperationen, ohne Zusammenhang mit der Herabsetzung. Nach Runde 2 wurde der vollständige Lauf `bin/no run test/std/*.no` wiederholt
(zstd / tls / str / json / set / base32 …) **alle PASSED**.

Wiederholungsprüfung für Runde 3: Die 3 roten `parser`-Tests (`TestAnnotationsLiveOnAST` /
TestCachedASTLowersToIdenticalHIR / `TestBuildSemViewsMatchesFreshParse`) sind **vor und nach der Lockerung
wortgleich** — `only 288 files parsed cleanly` lautete beide Male dasselbe, die Lockerung brachte also keine neuen Parse-Fehler;
sie befinden sich im Flugzustand der parallelen Parser-/Annotations-Umstrukturierung. Die Ausgabe von `no fmt -d src/std` war vor und nach der Lockerung
**bytegleich**, d. h. der Formatter gibt Annotationen an `.` unverändert an ihrer Position aus, ohne Formatierungsdrift. `test/std`
mit heap / zstd / tls / json / set / map / regexp ist vollständig grün.

**76 verbleibende Armkopf-Annotationen** (`#{overflow=wrap}` direkt über `cond -> {`): Sie sind **keine toten Zeilen** —
der Parser kopiert Annotationen auf Arm-Ebene auf jede Anweisung im Armkörper, sodass sie für den Armkörper wirken (Messung: eine Annotation am Armkopf bringt den Armkörper
`r = i * 2` zum Schweigen, nur die Header-Bedingung wird weiterhin gemeldet). Für die **Armbedingung** selbst sind sie unwirksam (eine Bedingung ist keine Anweisung).
Diese 76 Stellen bleiben unverändert: Eine Einzel-Einfügung erreicht sie nicht (die Armkörper-Stellen sind bereits zum Schweigen gebracht und werden nicht mehr gemeldet), und sie zu entfernen und erneut
einzufügen würde nur an der Header-Zeile im Kreis laufen.

**51 Stellen, die weiterhin von `no vet` gemeldet werden und in der aktuellen Sprache nicht annotierbar sind** (Runde 2: 270 Stellen; nach der DOT-Lockerung in Runde 3
bleiben nur diese): Die Zielanweisung beginnt mit `{` (nackter Block an Anweisungsposition / match mit Klammern) oder `(` (`(cond) { }`-Schleifenkopf),
sowie in wenigen Fällen mit einer Arm-Header-Bedingung. Auch heute werden sie **weiterhin** gemeldet (die ursprünglichen Annotationen waren ohnehin unwirksam), daher stellt das Zurückziehen keine
Regression dar; um sie auf null zu bringen, müssen zuerst die Schritte 1–2 in §8 erfolgen (`overflow` in L2 als Fehler werten), und erst dann ist zu entscheiden, ob L2 tatsächlich unterstützt werden soll —
siehe Schritt 4b in §8.

Bei stichprobenartiger Prüfung einer Stelle wie `archive/bzip2.no:315` finden sich **sowohl** am Blockkopf **als auch** im Block `#{overflow=wrap}`:
Die Ebene am Blockkopf wirkt derzeit nicht; tatsächlich arbeitet die Ebene mit den Einzelannotationen im Block (bereits durch `--fix=overflow` ergänzt). Das Löschen
der Block-Ebene ändert die Semantik also nicht; führt man danach `no vet` erneut aus, werden die Stellen sichtbar, die nur durch die äußere Annotation zum Schweigen gebracht wurden — genau
das ist der Zweck der Verschärfung, kein Nebeneffekt. In der Messung stiegen die `ovfhndld`-Hinweise in `crypto/rsa.no` von 19 auf 22,
alle auf Anweisungen vom Typ „an `?T` gebunden, aber keine Überlauf-Entnahme deklariert"; die Semantik blieb unverändert.

Auswirkung auf den L0-Bestand: In `src/**/*.no` gibt es 21 Dateien, in denen eine Annotation die erste wirksame Zeile der Datei ist; 17 davon sind
`#{js}`, der Rest sind `#{buildin}` direkt vor einer Deklaration usw. — alle **ohne** Leerzeile; nach dem L0-Kriterium in §2
handelt es sich weiterhin um L1, und `no fmt` sowie das Kompilierungsverhalten bleiben unverändert. `str.no` enthält 2 Fälle von „Annotation + Leerzeile + cond-Arm",
die sich innerhalb eines Funktionskörpers befinden und das L0-Kriterium nicht auslösen.

---

## 8. Reihenfolge der Verschärfung

1. **Zuerst `no vet` dahin bringen, `overflow` in L1/L2 nicht mehr zu akzeptieren**: Den Kurzschluss „gesamte Funktion bei Funktionsebene überspringen" entfernen,
   und `enclosingOverflow` auf den Teilbaum derselben Anweisung beschränken (keine Weitergabe über Blöcke hinweg).
2. **Dann den Parser Fehler melden lassen**: `#{overflow}` in L0/L1/L2 → Kompilierungsfehler, mit einem Hinweis wie „Bitte für jede Anweisung einzeln annotieren,
   oder `no fmt --fix=overflow` ausführen". Dieser Schritt legt den gesamten Bestand auf einmal offen.
3. **Bestand bereinigen**: `no fmt --fix=overflow` beherrscht die Herabsetzung bereits (die Geltungsbereichs-Ebene entfernen, dann gemäß den Checker-
   Meldungen einzeln L3 ergänzen); `src/std` wurde drei Runden durchlaufen (siehe Bestandstabelle in §7). Danach muss `make gen no` ausgeführt werden (Blob und
   StdSigKey neu backen), und `no fmt -w` darf nicht auf `src/std` angewendet werden — die Migration betrifft nur Annotationszeilen und erfolgt dateiweise mit
   `no fmt -fix=overflow -w <f>`; `diff` stellt sicher, dass es keine anderen Änderungen gibt.
4. **Anfügbare Ziele des Parsers lockern** (in Runde 3 zur Hälfte erledigt):
   - **4a erledigt**: `parseAnnotationStatement` akzeptiert Anweisungen, die mit **DOT** beginnen. `.n = .n + 1` /
     `.cnt == 0 -> …` gehören wie `x = …` zu den einzelnen Anweisungen (L3), konnten früher aber keine Annotation aufnehmen ⇒ in `src/std` waren 219 Stellen
     `#{overflow=wrap}` dauerhaft unwirksam. Nach der Lockerung sanken die nicht annotierbaren Stellen von 270 auf 51 und `parser.ASTVersion` wurde angehoben auf
     `2026-11-12.1`.
   - **4b Entscheidung offen**: `{` (nackter Block an Anweisungsposition / match mit Klammern) und `(` (`(cond) { }`-Schleifenkopf) sind **L2-
     Blockwirte**. Sie zuzulassen hieße, L2-Semantik für `overflow` zu legalisieren (`overflowAnnotatedNode` würde für den gesamten
     Block vorzeitig zurückkehren), was den Verschärfungsschritten 1–2 zuwiderläuft. Die richtige Reihenfolge ist, zuerst die Schritte 1–2 auszuführen (`overflow` in L2
     als Fehler werten) und erst dann zu entscheiden, ob die verbleibenden 51 Stellen durch „Umformulierung in eine Anweisung ohne Block" oder „einzelne Ergänzung des Arm-Bodys" gelöst werden,
     statt `{` / `(` bedingungslos freizugeben.
5. **Weitere Schlüssel schrittweise verschärfen**: Deklarationsschlüssel (`buildin` / `generic` / `embed` …) in L2/L3 von
   „unwirksam" auf „Fehler melden" umstellen.
6. Bei jedem Schritt muss `no fmt` idempotent bleiben und alle vier Leser bei derselben Eingabe zu derselben Schlussfolgerung kommen (§6).
