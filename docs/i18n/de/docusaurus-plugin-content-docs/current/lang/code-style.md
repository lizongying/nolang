---
sidebar_position: 4
---

# Codierrichtlinien

## Dateiende-Zeilenumbruch (Trailing Newline / EOF)

Jede **nicht leere** `.no`-Quelldatei **muss mit genau einem abschließenden Zeilenumbruch enden** (das heißt, am Dateiende muss ein einzelner Zeilenumbruch stehen).

- Datei hat **keinen** abschließenden Zeilenumbruch → einen hinzufügen.
- Datei endet mit **mehreren** Leerzeilen → auf einen einzigen abschließenden Zeilenumbruch reduzieren.
- **Leere Datei** (0 Bytes) → unverändert lassen.

Ausgeschlossene Verzeichnisse: `dist/`, `vscode-nolang/`, `node_modules/`.

Begründung: Ein einheitlicher, einzelner EOF-Zeilenumbruch verhindert, dass `git diff` „no newline at end of file" anzeigt, macht Unterschiede sauberer und macht Tool-Verkettung und -Zusammenfügen vorhersehbar.

### Automatische Durchsetzung (in der Toolchain integriert)

Diese Regel wird **automatisch** von der Toolchain durchgesetzt; es gibt kein eigenständiges Skript:

- **`no fmt`**: Der Unterbefehl `fmt` in `src/cmd/no/main.go` formatiert Dateien direkt und ruft `fmt.FormatFile` auf, das genau einen abschließenden Zeilenumbruch garantiert.
- **LSP-Formatierung beim Speichern / `textDocument/formatting`**: `formatNolangCode` → `fmt.FormatFile` in `src/lsp/server.go` fügt den EOF-Zeilenumbruch beim Speichern oder Formatieren einer `.no`-Datei im Editor automatisch hinzu bzw. reduziert ihn.

Die Implementierung liegt in `src/fmt/formatter.go`:

- `FormatFile(code)` formatiert die gesamte Datei und ruft `ensureTrailingNewline` auf, das alle abschließenden `\r` / `\n` entfernt (CRLF- und mehrfache Leerzeilen-kompatibel) und ein einzelnes `\n` anhängt. Leere oder nicht parsbare Inhalte werden unverändert zurückgegeben, um die Datei nicht zu beschädigen.
- `Format(code)` ist die reine Fragment-Formatierungsfunktion (ohne abschließenden Zeilenumbruch), hauptsächlich für Unit-Tests; beim Schreiben echter Quelldateien ist `FormatFile` zu bevorzugen.

## Automatische Bereinigung von Importanweisungen (Dedup / std-Imports ohne Alias entfernen / Sortieren)

`no fmt` (und die LSP-Formatierung beim Speichern) normalisiert automatisch den **zusammenhängenden Importblock am Dateianfang** (`# path[.fn] [alias]`); die Implementierung liegt in `src/fmt/imports.go`:

1. **Dedup**: Byte-identische Importe (gleicher Pfad, Funktionsname und Alias) werden auf das erste Vorkommen reduziert.
2. **Automatisches Entfernen aller reinen std-Modulimports ohne Alias**: Standardbibliotheksmodule sind implizit ladbar (siehe [Präfixe für modulübergreifende Aufrufe](./module)) — ein `# std/<module>` ohne Alias und ohne `.Funktion` (z. B. `# std/noml`, `# std/math`, `# std/database/sql`) bindet keinen Namen und ist redundant; der Formatter entfernt es. Drei Ausnahmen bewahren es:
   - Es wurde ein **Alias** (`# std/noml n`) oder ein **Funktionsimport** (`# std/fmt.sprintf fm`) geschrieben — eine bewusste Namensbindung;
   - Die Importzeile trägt einen **Kommentar** oder eine angehängte `#{...}`-Annotation — vom Benutzer geschriebener Inhalt wird nie still verworfen.
3. **Automatisches Sortieren**: Importe werden stabil lexikografisch nach Pfad → Funktion → Alias sortiert.

Konservative Grenze: Trägt irgendein Import im Block einen Kommentar (eigene Zeile darüber oder nachgestellt in derselben Zeile) oder eine angehängte `#{...}`-Annotation (z. B. die Strategie `#{bind=ffi}` für native Bindung), behält der gesamte Block **seine Quellreihenfolge und es wird nichts entfernt** — das Sortieren würde die Zuordnung von Kommentar und Anweisung zerstören und das Entfernen würde den Kommentar mitnehmen. Exakte Duplikate ohne angehängten Inhalt werden in diesem Fall weiterhin dedupliziert.

Importblöcke, die an einer anderen Stelle als am Dateianfang auftauchen (nach Code), bleiben unverändert.

```no
#{fmt-skip}
; vorher                            ; nachher
# std/vec                           # aaa/lib
# std/noml                          # std/io
# std/io                            # std/vec
# std/noml n
# aaa/lib        ;→  std-Imports ohne Alias (# std/noml, # std/vec, # std/io) entfernt;
                    ;   # std/noml n mit Alias behalten; Rest lexikographisch sortiert
```

## Kommentarmarkierungen (Comment Markers)

Nolang unterstützt zwei **einzeilige Kommentar**-Markierungen und eine **mehrzeilige (Block-)**Kommentar-Markierung mit folgender Semantik:

- `//` — traditionelle einzeilige Markierung (Kommentar bis zum Zeilenende)
- `;` — alternative einzeilige Markierung (implementiert 2026-07-17, Kommentar bis zum Zeilenende)
- `;; ... ;;` — mehrzeiliger (Block-)Kommentar (implementiert 2026-07-18, symmetrische Begrenzer; bleibt er ungeschlossen, läuft der Kommentar bis zum Dateiende)

```no
#{fmt-skip}
// 這是註釋
; 這也是註釋，語義相同
x = 1 ; 行內註釋，註釋到行尾

;;
   這是多行（塊）註釋
   可以跨多行
   以 ;; 結尾 ;;
y = 2 ;; 行內塊註釋 ;;
```

**Blockbegrenzung**: `;;` öffnet und `;;` schließt; alles dazwischen (einschließlich Zeilenumbrüche) ist Kommentarinhalt. Ein einzelnes `;` im Inneren beendet den Block nicht — nur `;;` tut das. Wird kein schließendes `;;` gefunden, läuft der Kommentar vom öffnenden `;;` bis zum Dateiende. Das öffnende `;;` versetzt nur dann in den Mehrzeilenmodus, wenn **dahinter in derselben Zeile nichts als Leerzeichen folgt** — steht nach dem `;;` auf derselben Zeile noch Text, ist es ein gewöhnlicher einzeiliger Kommentar (`;; Hinweistext` kommentiert nur diese Zeile, die nächste Zeile bleibt Code). Das gilt entsprechend für den schließenden Begrenzer: Auch nach dem schließenden `;;` darf nur Leerraum stehen.

**Der Formatter bewahrt die ursprüngliche Markierung**: `no fmt` ändert nie eine Kommentarmarkierung — `;` bleibt `;`, `//` bleibt `//`, `;; ... ;;` bleibt `;; ... ;;`. Der Formatter merkt sich die ursprüngliche Markierung über `Comment.Marker` und gibt sie unverändert aus; auch der Inhalt von Blockkommentaren (einschließlich interner Zeilenumbrüche) bleibt wörtlich erhalten, sodass das Formatieren idempotent ist.

**Sicherer Bereich**: Wenn `;` / `;;` innerhalb von Zeichenkettenliteralen (z. B. `'text/plain; charset=utf-8'`, `index-from(';', pos)`) oder innerhalb von `//`-Kommentaren auftauchen, werden sie zuerst von den Zeichenketten- bzw. Kommentar-Scannern des Lexers konsumiert und nie als Kommentarmarkierungen behandelt.

**Hinweis zu `cond -> X; Y`**: Da `;` jetzt ein Kommentar ist, wird `cond -> X; Y` als „führe `cond -> X` aus (X auswerten und verwerfen)" plus ein Inline-Kommentar `; Y` geparst — **`X` wird nicht zugewiesen**. Die korrekte Form ist `cond -> X = Y` (eine etablierte Stdlib-Gepflogenheit, siehe `arr.no`, `uuid.no`, `path.no`, `err.no`, `assert.no`). Wenn du `cond -> X; Y` im Quellcode siehst, ändere es vorrangig zu `cond -> X = Y`.

**Mehrere Anweisungen in einer Zeile sind verboten**: `;` ist nur eine Kommentarmarkierung und kann nicht mehrere Anweisungen in einer Zeile verbinden; das Komma `,` kann Anweisungen ebenfalls nicht verbinden. Jede Anweisung in einem Block `{ ... }` muss eine eigene Zeile einnehmen. Einen Block auf eine Zeile zu pressen ist der gefährlichste Fehler:

```no
#{fmt-skip}
; ❌ 錯誤：`; return }` 整段變成註釋，塊的 `}` 隨之消失
cond -> { print('X'); return }
```

```no
; ✅ 正確：每條語句獨佔一行
cond -> {
    print('X')
    return
}
```

`cond -> { print('X'); return }` lässt tatsächlich nur `cond -> { print('X')` übrig: Der Block wird nie geschlossen, und alle Anweisungen nach dem „verwaisten" `}` (meist der gesamte Rest des Funktionskörpers, manchmal sogar nachfolgende Funktionen auf Modulebene) werden verschluckt. Der Compiler meldet unter Umständen gar keinen Fehler, und eine Diagnose kann weit vom eigentlichen Fehler entfernt zeigen.

## Boolesche Literale und Vereinfachung von `== true` / `== false` (implementiert 2026-09-24)

Neben den Schlüsselwörtern `true` / `false` akzeptiert Nolang zwei **Kurzformen für boolesche Literale**:

- `!!` — ein eigenständiges Literal, gleichbedeutend mit `true`.
- `!` — ein eigenständiges Literal, gleichbedeutend mit `false` (nur wenn auf `!` kein Token folgt, das einen Operanden beginnen kann; folgt auf `!` ein `Zeilenumbruch` / `;` / Dateiende / `)` / `}` / `]` / `->`, wird es als `false` geparst).

> Hinweis: Hier ist `!!` / `!` in **Ausdrucksposition** gemeint, anders als die Formen in Schleifenposition `!! { }` (immer ausführen) und `! { }` (nie ausführen).

Beide Kurzformen werden bereits **beim Parsen** in ein gewöhnliches `BooleanLiteral` umgewandelt, sodass die vier Formen `f == true`, `f == !!`, `f == false`, `f == !` syntaktisch vollständig äquivalent sind und dieselbe Verarbeitungslogik durchlaufen.

Die Regeln von `no fmt` für Boolesche Werte (implementiert in `src/fmt/expr.go`):

- **Eigenständige Literale werden auf die Schlüsselwortschreibweise normalisiert**: `c = !!` → `c = true`, `c = !` → `c = false`.
- **Bekanntes wird vereinfacht, Unbekanntes nicht**: Wenn eine Seite von `==` / `!=` ein boolesches Literal ist **und** sich der andere Operand **als boolescher Wert verifizieren lässt**, gibt der Formatter die einfachste Form aus. Alle vier Pipelines teilen dieselben **Sammelregeln** für Signaturen (`collectModuleSignatures`: eingebackene std-Tabellen + über `use` / `#` erreichbare lokale Module + EnumVariants) und denselben Injektionsvertrag `SemRebuildConfig.Apply`; nur der Lieferant unterscheidet sich:
  `no fmt` / LSP nutzen `build.FmtSigLoader`, `no build` / `no vet` laufen weiter über `preloadModuleSignatures` + `astParseConfig` (beide Loader sind noch nicht zusammengeführt). `no fmt` injiziert vor dem frischen `SkipLowering`-Parse; der LSP injiziert in `ParseDocument` und veröffentlicht ein `parseSnapshot`, sodass die Formatierung beim Speichern eine tiefe Kopie dieses Sugar-AST wiederverwendet (`fmt.CloneProgramForFormat`) und bei Standardeinstellungen nicht erneut parst. Als Beweis gelten drei Quellen:

  1. Explizite Typannotation: `b bool = true`, Parameter `(b bool)`;
  2. Zuweisung eines booleschen Literals: `flag = true` / `flag = false`;
  3. Aufruf einer Funktion, deren deklariertes Ergebnis `bool` ist: **in dieser Datei definiert** (z. B. `bytes-eq = ... (eq bool)`) oder über die Signaturtabelle sichtbar (z. B. std `args.has-flag`, lokales Modul `helper.is-ok`). Ein Methodenaufruf auf einem Variablen-Receiver (`s.starts-with('a')`) wird über den von Sem bereits inferierten Empfängertyp (`s: str`) gegen `str.starts-with` aufgelöst; ein nicht inferierter Empfänger oder eine gleichnamige Variable, die in verschiedenen Gültigkeitsbereichen unterschiedliche Typen inferiert, bleibt unbekannt (bewusst kein Raten nach „eindeutig globaler Methodenname", damit `?bool` nicht mit reinem bool verwechselt wird). Findet die Signaturtabelle den Ergebnistyp nicht, bleibt der Aufruf unbekannt.

  | Ursprüngliche Schreibweise (`b` unten ist eine „bekannte" Variable) | Formatiert |
  | ------------------------------------------ | -------- |
  | `b == true` / `b == !!`                    | `b`      |
  | `b == false` / `b == !`                    | `! b`    |
  | `b != true` / `b != !!`                    | `! b`    |
  | `b != false` / `b != !`                    | `b`      |
  | `true == b` (Literal auf der linken Seite)                | `b`      |

  `!=` kehrt lediglich die Polarität um; `true == false` (beide Seiten Literale) bleibt unverändert. Ein nackter Name ohne bool-Beweis wie `f == true` bleibt ebenfalls unverändert.
- **`?T` und jeder unbekannte Operand behalten den Vergleich**: Optionen (`?T`), modulübergreifende Aufrufe, deren Ergebnistyp die Signaturtabelle nicht auflösen kann, nackte Namen ohne Zuweisung, Punktzugriffe mit nicht inferiertem Empfängertyp, Aufrufausdrücke und Berechnungsergebnisse behalten alle `== true` / `== false`. Das Reduzieren auf ein nacktes `x` / `! x` als Kontrollflussbedingung löst einen MIR-Codegen-Crash „option-as-i1" aus — genau deshalb hat v0.3.3 die ganze Regel zurückgenommen (`8a8ecb59`); am 2026-11-15 wurde sie unter einer Regel „nur bei positivem Beweis vereinfachen" wiederhergestellt, ohne die umgekehrte Schlussfolgerung „wenn kein `?T` sichtbar ist, muss es ein Wert sein". Seit demselben Tag injizieren `no fmt` / LSP auch modulübergreifende Signaturen, sodass option-Rückgaben wie `fs.write-str(...)` (`?bool`) **sichtbar** sind und vom harten Option-Gate gestoppt werden, statt durch Unsichtbarkeit. Eine Option ist ein **unabhängiges hartes Gate** (`boolOperandIsOptionTyped`), das vor der Prüfung auf bool-Werte greift, und wird daher unabhängig davon, ob die Typannotation im Quellcode erhalten bleibt, nie vereinfacht: Sowohl `ok ?bool = fs.write-str(...)` als auch `ok = fs.write-str(...)` behalten den Vergleich. Das Entfernen redundanter Typannotationen durch `no fmt` gemäß bestehender Richtlinie (`fixRedundantTypeSource`) ist **korrektes Verhalten** — deaktiviere diese Entfernung nicht, um die Option-Erkennung zu bewahren. Regressionstests: `src/fmt/bool_comparison_test.go`, `src/fmt/signatures_test.go`, `src/lsp/fmt_gate_test.go`.
- **Gilt auch in `->`-Zweigen / eigenständigen if-then-Bedingungen** (für bekannte Variablen): `b == ! -> return` → `! b -> return`.
- **Prioritätssicher**: Die Negation wird als `! ` (mit einem Leerzeichen) geschrieben, um der bestehenden Konvention für Präfixoperatoren zu entsprechen und `no fmt` idempotent zu halten. Derzeit sind alle verifizierbaren Operanden Bezeichner, `! ` kann daher nicht neu binden; die Implementierung behält jedoch für nicht-atomare Operanden den Klammerschutz (`! (a + b)` statt `! a + b`), falls die Beweisregel später erweitert wird.

## Verzögerte Nullinitialisierung für Rückgabewerte (Deferred Zero-Init for Return Values)

Nolang-Funktionen verwenden für ihre benannten Ergebnisparameter (out-Parameter) eine **verzögerte Nullinitialisierung**: Der Funktions-Prolog **führt keine** Nullinitialisierung der out-Parameter durch. Stattdessen verfolgt der Compiler über ein Bitmap `%__ret_init_bitmap`, ob ein out-Parameter im Funktionskörper explizit zugewiesen wurde. Bei der Rückgabe erhält jeder **nicht explizit zugewiesene** out-Parameter automatisch einen Nullwert-Store seines Typs (Integer → `0`, str-long → `zeroinitializer`, struct → `zeroinitializer`, option → `nil`). Dieser Mechanismus ist symmetrisch zum bestehenden Mechanismus `%__move_bitmap` für verzögerte Freigabe.

### Warum dieses Design

- **Leistung**: Vermeidet einen `store zeroinitializer` für jeden out-Parameter im Prolog, der anschließend durch eine Zuweisung im Funktionskörper wieder überschrieben würde — ein rein redundanter Store. Die verzögerte Strategie schreibt nur dann Null, wenn „tatsächlich keine Zuweisung erfolgte".
- **Lesbarkeit**: Man muss keine Vorlagen wie `found = false` / `result = nil` am Funktionsanfang schreiben, um früh Null zuzuweisen; der Leser sieht direkt die Absicht „nur der Erfolgspfad weist zu".
- **Konsistenz**: Verwendet dasselbe Bitmap-Tracking-Muster wie der verzögerte Freigabemechanismus `%__move_bitmap`, wodurch die Compiler-Interna symmetrisch bleiben.

### Empfohlene Schreibweise

Weise out-Parameter nicht am Funktionsanfang vorab Null zu — der Compiler füllt bei der Rückgabe automatisch Null. Schreibe nur die Zuweisungen des „Erfolgspfads":

```no
; Good：不寫提前賦零值，編譯器自動處理
hashmap-str-tmpl.contains = (key str) (found bool) {
    val ?v = .get(key)
    val: {
        ok -> found = true

        err -> {
        }

        nil -> {
        }
    }
}
```

### Anti-Pattern (vermeiden)

Schreibe keine redundanten vorzeitigen Nullzuweisungen — sie sind unwirksame Stores, die von einer späteren Zuweisung überschrieben werden:

```no
#{fmt-skip}
; Bad：冗餘的提前賦零值
hashmap-str-tmpl.contains = (key str) (found bool) {
    found = false              ; 冗餘——編譯器會在返回時自動補零
    val ?v = .get(key)
    val: {
        ok -> found = true
        err -> {}
        nil -> {}
    }
}
```

### Option ist standardmäßig nil

Für out-Parameter vom option-Typ `?T` schreibt der Compiler beim Nullauffüllen direkt `nil` (tag=1, data=0), gleichbedeutend mit `result = nil`. Daher benötigen Pfade wie „nicht gefunden / leere Menge / EOF" nur ein nacktes `return`, ohne `result = nil` im Quellcode:

```no
#{fmt-skip}
; Good：未命中路徑用裸 return，編譯器自動補 result = nil
hashmap-str-tmpl.get = (key str) (result ?v) {
    .size == 0 -> return        ; 編譯器補 result = nil
    ...
    ; fall-through：編譯器補 result = nil
}
```

### Debug-Hinweis

Wenn eine Funktion einen unerwarteten Nullwert zurückgibt (z. B. `found` sollte `true` sein, ist aber `false`, oder `result` sollte einen Wert haben, ist aber `nil`), prüfe, ob **alle Pfade, die einen nicht-Nullwert zurückgeben sollten**, im Funktionskörper eine explizite Zuweisung an den out-Parameter vornehmen. Der Compiler errät keine Absichten — er füllt nur nicht zugewiesene out-Parameter mit Null. Siehe den Debug-Hinweis in `.agents/skills/nolang-debug/SKILL.md`.

## Ausgabeslots direkt an bestehende Variablen binden (Zwischenvariablen vermeiden)

**Empfohlene Schreibweise**: Wenn das Ergebnis eines Aufrufs beim Schreiben neuen Codes nur dazu dient, eine bestehende Variable weiter zu aktualisieren, binde den Ausgabeslot **direkt an diese Variable** und führe keine nur einmal verwendete Zwischenvariable ein. Dies ist besonders häufig bei mehreren Ausgabewerten, wenn der Empfänger selbst der zu aktualisierende Zustand ist.

```no
; ✅ 推薦：餘數直接寫回 r（r 同時是方法接收者）
qsq, r = r.div-mod(modulus)
```

```no
; ❌ 避免：msq 只被寫回一次，沒有其它用途
qsq, msq = r.div-mod(modulus)
r = msq
```

- Unbenutzte Ausgaben werden mit `_` belegt: `_, r = r.div-mod(modulus)` führt nicht einmal eine neue Variable ein.
- Schreibgeschützte Eingabereferenz und Ausgabeslot, die auf dasselbe Objekt zeigen, sind gemäß der „Alias-Regel" zulässig; `r = r._mul(r)` in `src/std/bigint.no` ist dieselbe In-Place-Aktualisierung, die auch für einen einzelnen Ausgabewert gilt.
- Dies ist eine Empfehlung für **neu geschriebenen Code**, kein Refactoring-Auftrag: Bestehender, gültiger Code sollte nicht aus Stilgründen umgeschrieben werden (siehe „Goldene Regel").
- Vollständige Semantik, Messergebnisse und Anwendungsbereich siehe [Sprachreferenz: Funktionsdefinitionen](./syntax).

## Markdown-Codeblöcke (`no fmt -md`)

`no fmt -md` formatiert nur den **Innenbereich** der Fences ```` ```no ```` / ```` ```nolang ````; die Fence-Zeilen und das übrige Markdown bleiben byte-genau erhalten. Der Inhalt eines Blocks wird als Nolang-Quellcode geparst; lässt er sich nicht parsen, bleibt der Block unverändert und seine Zeilennummer wird gemeldet — das Formatieren kann also keine Datei beschädigen, die es nicht versteht. (`-md` und `-fix` schließen sich gegenseitig aus; `-md` formatiert nur.)

### Richtige und falsche Beispiele jeweils in eigenen Blöcken

Wenn „die richtige Form" und „die falsche Form" im selben Block stehen, behandelt der Parser beide als ein Programm: Die richtige Hälfte verliert ihre `no fmt`-Prüfung, und die falsche Hälfte kann vom Formatter still „korrigiert" werden, bis der Fehler nicht mehr erkennbar ist. Daher:

- Ein **richtiges Beispiel** erhält einen eigenen Block ohne Annotation, damit `no fmt` es formatiert und validiert;
- Ein **falsches Beispiel** erhält einen eigenen Block, dessen erste Zeile `#{fmt-skip}` ist, sodass es weder validiert noch verändert wird.

```no
; ✅ 正確：每條語句獨佔一行
cond -> {
    print('X')
    return
}
```

```no
#{fmt-skip}
; ❌ 錯誤：`;` 之後整行都是註釋，塊的 `}` 隨之消失
cond -> { print('X'); return }
```

### Die drei legitimen Zwecke von `#{fmt-skip}`

1. **Falsche Beispiele / Gegenbeispiele** — von Anfang an keine gültigen Programme; Formatieren hilft hier nur kontraproduktiv.
2. **Pseudocode und Signaturübersichten** — Fragmente mit reiner Form ohne Implementierung (z. B. das Signaturskelett `f = (a i64) (r i64) { ... }`).
3. **Spickzettel mit ausgerichteten Kommentarspalten** — `no fmt` verdichtet `code      ; Anmerkung` zu `code; Anmerkung`, während die Spaltenausrichtung gerade die Lesbarkeit solcher Tabellen ausmacht; sie muss herausgefiltert werden.

### Keine Annotation nur zur Verhinderung des Aufklappens

`no fmt` klappt einen leeren Block `{ }` und einen einzeiligen Arm `-> { x() }` auf mehrere Zeilen auf:

```no
ok = (b bool) (yes bool) {
}

dispatch = (cmd str) {
    cmd == 'a' -> {
        fa()
    }
}
```

Dies ist die **kanonische** Form und sollte akzeptiert werden — erst nach dem Aufklappen hat der Block eine eigene Zeile, in die weitere Anweisungen eingefügt werden können; sonst beginnt jede hinzugefügte Anweisung mit dem manuellen Aufteilen des Blocks. Ebenso ist `true -> {}`, das zu `true -> {\n}` wird, kein Grund für einen Rückschritt. Nur die drei oben genannten Fälle (Gegenbeispiele, Pseudocode, ausgerichtete Tabellen) benötigen wirklich `#{fmt-skip}`.

### Kommentar-reine Blöcke benötigen keine Annotation

Ein Block, der nur Kommentare enthält (die übliche Art, die richtige Form „auszukommentieren"), wird zu **null Anweisungen** geparst. `no fmt` behandelt das als „nichts zu formatieren", lässt den Block unverändert und meldet nichts — es ist kein Syntaxfehler, und `#{fmt-skip}` würde diese Tatsache nur verbergen.

### Reihenfolge: zuerst annotieren, dann formatieren

`no fmt -md -w` schreibt jeden nicht herausgefilterten Block direkt um. Sobald ein falsches Beispiel vom Formatter „korrigiert" wurde, ist das Gegenbeispiel nicht mehr aussagekräftig und die ursprüngliche Schreibweise ist unwiederbringlich verloren. **Zuerst `#{fmt-skip}` ergänzen, dann `-w` ausführen.**
