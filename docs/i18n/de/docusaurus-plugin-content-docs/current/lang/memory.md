---
sidebar_position: 6
---

# Speicherverwaltung

Nolang ist eine **GC-freie** Sprache; Speichersicherheit wird durch vom Compiler eingefügte `free`-Aufrufe garantiert. Dieses Dokument beschreibt das implementierte Speicherdesign und die Besitzsemantik (Ownership).

## Kernprinzipien

### Einzelner Besitzer (Single Ownership)
Jeder Heap-`data`-Puffer hat **genau einen Besitzer**. Besitz kann per move übertragen werden; nach der Übertragung gibt der ursprüngliche Besitzer die Verantwortung für `free` ab. Bei `=` zwischen lokalen Variablen sorgt ein tiefer Clone dafür, dass beide Variablen ihre Daten unabhängig besitzen.

### Zuweisungssemantik
`b = a` wählt je nach Kontext eine der folgenden Semantiken:

| Semantik | Auslöser | Verhalten |
|------|---------|------|
| **Wertkopie** | Grundtypen (i64/f64/bool usw.) und `can_slot_rebind` nicht erfüllt | Direktes Kopieren des Werts, keine Heap-Daten |
| **Stack-Slot-Rebind** | lokales `b = a`, a ist ein Stack-Typ (i64/u64/i128/u128/txt) und `can_slot_rebind` erfüllt | `g.varAlias[b] = a`, b und a teilen denselben Stack-Slot, 0 Kopien (besser als Wertkopie); andernfalls Rückfall auf Wertkopie |
| **Tiefen-Clone** | `b = a` zwischen lokalen Variablen, a besitzt Heap-Daten (vec/arr/str/clonbare Struktur/**`?str`, `?[]T`**) und wird nach der Zuweisung **noch referenziert** | malloc neue Daten + memcpy + rekursives Clonen der Elemente; a und b besitzen Daten unabhängig und werden bei Funktionsende jeweils freigegeben |
| **move (Quelle tot)** | `b = a` zwischen lokalen Variablen, a besitzt Heap-Daten, wird aber nach der Zuweisung **nicht mehr referenziert** | Zero-Copy-Übertragung des Besitzes; Quelle überspringt `free`. **Beobachtbar äquivalent** zu einem Tiefen-Clone, nur ohne eine Kopie |
| **move** | Ausgabeparameter `out = x` | Flache Kopie der Struktur + Markierung der Quelle als moved; Quelle überspringt `free` |
| **Tiefen-Clone** | `vec.push(x)` (x besitzt Heap-Daten) | malloc neue Daten + memcpy + rekursives Clonen der Elemente; Quelle besitzt weiterhin eigene Daten, beide werden bei Funktionsende freigegeben |

> **Wert-zu-Wert-Zuweisungen werden immer tief kopiert; Zero-Copy-Aliasing ist verboten.** `b = a` (RHS ist eine gesamte benannte Variable) hat genau zwei Absenkungen (Lowerings):
> **Quelle noch lebend ⇒ Tiefen-Clone**, **Quelle tot ⇒ move**; **nicht erlaubt** ist „zwei Namen teilen einen Puffer, nur ein `free`"
> (ein solches Lowering existierte einst in der schreibgeschützten Forward-Optimierung und wurde entfernt). **Element-/Feldlesen** (`b = a[i]`, `b = a.f`)
> behält seine **Ansicht-Alias**-Semantik und ist davon nicht betroffen.

## Stack-Typ-move (Stack-Slot-Rebind / slot-rebind)

Obwohl `i64/u64/i128/u128/txt` alles **Stack-Typen** sind (Werte liegen direkt im alloca-Stack-Slot, keine Heap-`data`), läuft `b = a` semantisch standardmäßig auf „**Wertkopie**" hinaus (memcpy des Stack-Werts von a in den Stack-Slot von b). Um unnötige Kopien zu vermeiden, führt der Compiler für Stack-Typ-`b = a`, die die **can_slot_rebind**-Bedingung erfüllen, ein **Stack-Slot-Rebind** durch: Er setzt `g.varAlias[b] = a`, sodass b und a denselben Stack-Slot teilen (0 Kopien, besser als Wertkopie).

### can_slot_rebind-Bedingung (konservative Korrektheit)
Nach dem Rebind zeigen b und a auf denselben Stack-Slot, daher würde jedes spätere Lesen von b „a's Speicher" lesen. Um semantische Fehler zu vermeiden, ist ein Rebind nur erlaubt, wenn die Quelle a **anschließend keinerlei Referenz (lesend oder schreibend) mehr hat**; andernfalls wird er **auf eine vollständige Wertkopie zurückgestuft** (Semantik unverändert, nur ein zusätzliches memcpy).

- Die Sicherheit wird statisch gelöst durch `computeSlotRebindSafety` (Hauptfunktion) / `computeMoveEligibility` (Benutzerfunktionen) über `stmtContainsVarRefAny` / `exprContainsVarRefAny`: Es werden alle Anweisungen nach dem move gescannt (inkl. Verzweigungs-/Schleifen-Rückkanten); wird die Quellvariable in irgendeiner Form referenziert, ist `slotRebindSafe[stmt] = false` → Kopie-Pfad.
- Der Referenz-Scan muss **alle AST-Knoten aufzählen, die eine Variable referenzieren können**, sonst würde eine nicht erfasste Schreibreferenz zu einem unsicheren Rebind führen (verunreinigter Alias-Slot → falsche Ausgabe oder sogar Endlosschleife). Abgedeckt: Anweisungsebene `LetStatement` / `ExpressionStatement` / `ForStatement` (inkl. `Init`/`Update`/`Condition`/`CountExpr`/`Body` und der **`IterRange`-Iterationsmenge**) / `ReturnStatement` / `MultiAssignStatement` / **`UnwrapAssignStatement` (`?=`-Entpackzuweisung)** / **`BlockStatement` (nackter Block)**; Ausdrucksebene `Identifier` / `AssignExpression` / `Infix` / `Prefix` / `Call` / `Dot` / `Index` / `IfExpression` (inkl. Zweigkörper) / `Slice` / `Conditional` / `Grouped` / **`CoExpression` (`co <call>`-Coroutinen-Spawn, defensiv; die Abschaltung der gesamten Funktion übernimmt weiterhin `curHasUnsafeConstruct`)** / **`CastExpression`** / **`RangeExpression`**. Jedes neue Konstrukt, das Variablen referenziert, muss in beide Scan-Funktionen aufgenommen werden.
- `match` wurde zu `IfExpression` desugart; Referenzen in seinen Zweigkörpern werden von der obigen Analyse erfasst, daher **löst match keine Abschaltung aus** (verifiziert gegen die Baseline in `tests/match.no` / `tests/option.no`).

### Abschalt-Szenarien (immer Rückstufung auf Wertkopie)
| Abschaltbedingung | Grund |
|---------|------|
| Ziel ist Ausgabeparameter / globale Variable / Heap-Typ-Variable | Ausgabeparameter werden per Zeiger übergeben, Globale sind funktionsübergreifend sichtbar, Heap-Typen brauchen tiefes `free` — ein Rebind zerstört den Besitz |
| Quelle ist Parameter / globale Variable | Parameter werden als Referenz übergeben, ein Rebind würde den Stack-Frame des Aufrufers zerstören |
| **stdlib-Funktion** (`curIsStdLib`, bestimmt durch `SetStdModules`) | Interne Alias-Bindungen der stdlib (z. B. fmt's `it` alias auf ein lokales `n`) lassen sich für die Referenzanalyse schwer vollständig modellieren |
| **Benutzerfunktionskörper enthält Closure (`FunctionLiteral`) oder Coroutinen-Spawn (`RunExpression`)** (`curHasUnsafeConstruct`, rekursiv gescannt durch `bodyHasUnsafeConstruct`) | Von einer Closure erfasste Variablen werden in einem „eigenen Funktionskontext" ausgewertet, Coroutinen laufen threadübergreifend; beide haben eine Lebensdauer jenseits des `g.varAlias`-Aliaskopos der einzelnen Funktion, ein Rebind würde den geteilten Stack-Slot zerstören |

> Design-Kompromiss: Closures/Coroutinen werden über eine **Abschaltung der gesamten Funktion** statt präziser Pro-Variablen-Berechnung behandelt, weil eine Single-Function-Aliasanalyse grundsätzlich keine funktions-/threadübergreifende Speicherfreigabe modellieren kann. Eine Abschaltung der gesamten Funktion kostet nur die Optimierung (Rückstufung auf Kopie) und führt nie zu einem Fehler — im Einklang mit dem Prinzip „konservative Korrektheit".

### Alias-Ungültigmachung (alias invalidation)
- Wenn das Ziel neu zugewiesen wird, wird zuerst `delete(g.varAlias, name)` aufgerufen, um einen verbliebenen alten Alias zu entfernen; sonst zeigt der allgemeine Zuweisungspfad `varAddr(name)` weiterhin auf den alten Quell-Slot und verunreinigt die alte Quelle.
- Der Rebind führt eine transitive Auflösung entlang der `g.varAlias`-Kette durch, um den endgültigen Quell-Slot zu finden (`a → c → …`), und vermeidet so Fehlversätze bei mehrstufigen Aliasen; gleichzeitig wird `g.varTypes[name]` synchronisiert, damit spätere Typabfragen konsistent bleiben.

### Orthogonalität zum Heap-Typ-move
Stack-Slot-Rebind wirkt nur auf die **Wertkopie**-Semantik von Stack-Typ-`b = a` und ist vollständig orthogonal zu den Pfaden „Tiefen-Clone / move (Ausgabeparameter)" heap-besitzender Typen; der Heap-Typ-Pfad wird von `slotRebindSafe` nicht beeinflusst.

### Testreferenzen
- `tests/slot-rebind.no`: i64/u64-Rebind, i128/u128 über `==` verglichen, txt über `.len-bytes()`, Rückstufung auf Kopie (`da` wiederverwendet → `db`/`dc` kopiert), nach Neuzuweisung (`ra`=10), Parameter-move (`pm_fn` Quelle ist Parameter → Kopie), Rebind dann Neuzuweisung (`rr_fn` → `rr`=99), Konsumierung (`consume(m)` → `cm`=84). Erwartete Ausgabe `42 7 1 1 17 5 5 10 100 99 84`.
- `tests/slot-rebind-unsafe.no`: Coroutine (`awy { co ... }`) erfasst eine Stack-Variable; prüft, dass `curHasUnsafeConstruct` den Rebind abschaltet und die Ausgabe mit der Baseline übereinstimmt.

### Vom Compiler eingefügtes `free`
- Bei Funktionsende: alle nicht-moved lokalen Heap-Variablen freigeben
- Vor Neuzuweisung: alten Wert freigeben
- Strukturfelder: Felder mit Heap-Daten rekursiv freigeben

## Typ-Layout

| Nolang-Typ | Speicherstruktur | Felder | Allokationsstrategie |
|------------|-----------|------|---------|
| `[]T` (Slice) | **16 Bytes** | **u32 len, u32 cap, i64 data (ptrtoint)** | malloc (Heap) |
| `[N]T` (festes Array) | N × sizeof(T) | Elemente inline, **kein len/cap-Descriptor** | alloca (Stack) oder malloc |
| `str` (lange Zeichenkette) | **16 Bytes** | **u32 len, u32 cap, i8\* data** | malloc (Heap) |
| `txt` (feste Zeichenkette) | 256 Bytes | `[255 x i8]` data, i8 len | alloca (Stack) |
| `?T` (nullable) | **typabhängig von der Nutzlast**: `{ i8 tag, [K x i64] }` | 1 Byte Tag + 7 Bytes Padding + Nutzlast (Vielfaches von 8) | alloca (Stack); Heap-Zeiger, sobald die Nutzlast die Schwelle überschreitet |
| Tagged Enum | `{ iN tag, [K x i64] }` | Tag-Breite folgt der Variantenanzahl (ab `i8`); Nutzlast ist die Union der breitesten Variante | alloca (Stack) |
| Struktur | Summe der Felder | jedes Feld | alloca (Stack) |

**Die Größe von `?T` wird berechnet, nicht festgelegt**: `?i64` = 16 Bytes, `?str` = 24, `?txt` = 264; eine Nutzlast über
`option-inline-threshold` (Standard 264 = 1 + 7 + 256, Minimum 16) wird als Heap-Box gespeichert, womit die gesamte Option
wieder 16 Bytes beträgt. Jeder Nutzlasttyp besitzt seinen eigenen `%option_<key>`-Typ (`src/mir/layout.go`), daher gilt
„gleicher Typ ⇔ gleiches Layout". Die `err`-Nutzlast wird immer geboxt und belegt nie das Nutzlast-Budget. Siehe
`docs/docs/lang/syntax.md`〈Nullable Typen (option) · Speicherlayout〉.

Die **Tag-Breite eines Tagged Enum** folgt der Variantenanzahl (`<=256` → `i8`, `<=65536` → `i16`, `<2^32` →
`i32`, sonst `i64`); die Nutzlast-Union ist so breit wie die **einzelne breiteste Variante**, nicht die Summe aller,
und ihre Felder werden mit natürlicher Ausrichtung angelegt und über **Byte-Offsets** adressiert. Haben alle Varianten
keine Nutzlast, degeneriert das Layout zu `{ iN }` (ab 1 Byte).

**`str`s u32-Header**: `%str-long = type { i32, i32, i8* }` (16 Bytes auf 64-Bit-Plattformen).
`len` / `cap` sind intern `u32` (LLVM `i32`), **alle öffentlichen APIs bleiben aber `i64`**: beim Lesen `zext i32 -> i64`,
beim Schreiben `trunc i64 -> i32`. Die Signaturen von `s.len-bytes()`, `s.len()`, `s.cap()`, `s._len()` / `s._cap()` /
`s._set-len()` / `s._set-cap()` und `len(s)` sind unverändert. Diese Grenze wird zentral von den Codegen-Helfern
`strLenI64` / `strCapI64` / `strHeaderLoadI64` / `strHeaderStoreI64` / `strLongFromParts` behandelt:
**Es darf niemals** ein rohes `load i64` / `store i64` auf Feld 0 / 1 des Descriptors ausgegeben werden. Bei opaque Pointers
hält der LLVM-Verifier das nicht auf, aber der 8-Byte-Zugriff überschreibt still das benachbarte Header-Feld (gemessen:
`s.len-bytes()` gab einmal `len | cap<<32` zurück).

Alle Größenkonstanten müssen ebenfalls 16 Bytes folgen: der Element-Stride von `[]str`, der argv-Backing-Store von `os.args()` sowie globale Zeichenkettenkonstanten als Literale `{ i32 len, i32 cap, i8* data }`. Die Nutzlast von
`?str` sind genau diese 16 Bytes (zwei i64-Wörter, das erste packt len und cap), daher ist `?str` eine 24-Byte-Inline-Option;
die Nutzlast einer `str`-Variante in einem Tagged Enum sind ebenfalls zwei Wörter. `txt` bleibt unverändert und übernimmt nicht u32 von `str`.

**`[]T`s u32-Header**: `%vec = type { i32, i32, i64 }` (16 Bytes). `len` / `cap` sind zu `str`
isomorph, intern `u32`, öffentliche API weiterhin `i64`; der Unterschied ist, dass **das Datenfeld `i64` bleibt**
(ein über `ptrtoint` kodierter Zeiger statt `i8*`), damit die bestehenden `inttoptr` / `ptrtoint`-Aufrufstellen
unverändert bleiben. Diese Grenze liegt in `vecLenI64` / `vecCapI64` / `vecHeaderLoadI64` / `vecHeaderStoreI64` / `vecFromParts`
(sowie den generischen `hdrFieldI64` / `hdrTrunc` / `hdrWiden`); `emitLenCap` / `emitGetField` /
`emitSetField` / `ensureVecBuffer` gehen alle darüber. Das gleiche Verbot wie bei `str` gilt: **Es darf niemals**
ein rohes `load i64` / `store i64` auf die Felder 0 / 1 von `%vec` ausgegeben werden.

`cap == 0` ist für `%vec` eine **Markierung für eine geliehene Ansicht** (`s.bytes()`, `[N]T` → `[]T`-Ausleihe,
`str` → `[]byte`-Zwangskonvertierung): Es besitzt keinen Heap, `@vec_free` / `@vec_retain` überspringen es; nur
`cap > 0` besitzt den Block hinter `data`.

`[N]T` besitzt überhaupt keinen Laufzeit-Descriptor: Auf LLVM-Ebene ist es das nackte Aggregat `[N x T]`,
`.len` wird vom Compiler direkt durch die Kompilierzeitkonstante `N` ersetzt, es gibt kein `cap`.

## Flaches free vs. tiefes free

### Flaches free
Gibt nur den Datenpuffer des Containers frei, ohne die Elemente zu durchlaufen. Anwendbar auf:
- `%str-long` (Zeichendaten sind ein Zeichenpuffer, keine verschachtelten heap-besitzenden Elemente)
- vec/arr, deren Elemente Grundtypen sind (i64, double usw.)

### Tiefes free
Durchläuft zuerst jedes Element und gibt dessen Heap-Daten rekursiv frei, dann wird der Datenpuffer des Containers freigegeben. Anwendbar auf vec/arr, deren Elemente Heap-Typen besitzen:
- `[]str` (Elemente sind %str-long)
- `[][]i64` (Elemente sind %vec)
- `[]MyType` (Elemente sind Benutzerstrukturen; Felder werden rekursiv freigegeben)

### NULL-Prüfung
Vor jedem `free` steht `icmp eq i8* %ptr, null`, um `free(NULL)` oder das Freigeben nicht initialisierter Zeiger zu vermeiden.

## Besitzübertragung (move)

### move bei einem einzelnen Rückgabewert
```no
get-slice = () (out []i64) {
    local = [1, 2, 3]
    out = local; local 標記為 moved，函數結束不 free；out 由呼叫者管理
}

v = get-slice(); v 擁有 data，函數結束時 free
```

### move bei mehreren Rückgabewerten (nach Parameterpositionsreihenfolge)
```no
get-pair = () (a []i64, b []i64) {
    x = [1, 2]
    y = [3, 4]
    a = x; 第一個輸出參數，x 標記為 moved
    b = y; 第二個輸出參數，y 標記為 moved
}

a, b = get-pair(); a 擁有 x 的 data，b 擁有 y 的 data
```

**Verarbeitungsreihenfolge**: Ausgabeparameter werden in ihrer **Deklarationsreihenfolge** in der Funktionssignatur einzeln verarbeitet. Jede `out = src`-Zuweisung markiert die Quellvariable unabhängig als moved.

**Mehrfache Zuweisung derselben Quellvariable**: Wenn `a` und `b` auf dieselbe Quellvariable verweisen (z. B. `a = x; b = x`), entscheidet der Compiler über eine **Liveness-Voranalyse** (`moveEligible`) automatisch zwischen clone und move:

- `a = x`: x wird später noch durch `b = x` referenziert → **Tiefen-Clone**, a besitzt Daten unabhängig
- `b = x`: x wird später nicht mehr referenziert → **move**, b übernimmt x's Daten, x wird als moved markiert und überspringt `free`

Dies vermeidet double-free. Regel: **die letzte Referenz nutzt move, frühere Referenzen nutzen clone**.

Szenarien mit bedingten Verzweigungen (z. B. `if cond { out = x }`) verfolgen den moved-Zustand über eine Laufzeit-Bitmap, siehe nächsten Abschnitt.

### Tiefen-Clone bei vec.push
```no
inner = [1, 2, 3]
outer.push(inner)
; inner 的 data 被深層 clone 到 outer 新元素位置
; inner 仍擁有獨立 data，函數結束時 inner 與 outer 各自 free
```

push führt für heap-besitzende Elementtypen (`%str-long`/`%vec`/`%arr`/Benutzerstrukturen) einen Tiefen-Clone durch: malloc neue Daten + memcpy + rekursives Clonen der Elemente. Die Quellvariable und der äußere vec besitzen jeweils unabhängige Daten, es ist **keine move-Markierung nötig**, was double-free vermeidet. Grundtyp-Elemente (i64/f64 usw.) werden direkt als Wert gespeichert.

### Laufzeit-move-Verfolgung (Bitmap, indiziert über Heap-Variablen-Slot)

move unter bedingten Verzweigungen stellt eine Herausforderung dar: Zur Kompilierzeit kann nicht festgestellt werden, ob ein move tatsächlich stattfindet.

```no
cond-move = (flag i64) (out []i64) {
    x = [1, 2, 3]
    if flag == 1 {
        out = x; move 僅在 flag==1 時發生
    }
    ; flag==0 時 x 仍擁有 data，函數結束需 free
    ; flag==1 時 x 所有權已轉移，函數結束需跳過 free
}
```

Nolang löst dies mit einer **über den Heap-Variablen-Slot indizierten Bitmap**. Zur Kompilierzeit erhält jede lokale Heap-Variable einen eindeutigen `varIdx`, und jedes Bit der Laufzeit-Bitmap entspricht einer Heap-Variablen (nicht einem Ausgabeparameter).

#### Compiler-Zustand

| Feld | Typ | Zweck |
|------|------|------|
| `heapVarIndex` | `map[string]int` | Heap-Variablenname → `varIdx` (nur lokale Heap-Variablen) |
| `outBindState` | `[]int` | der aktuell an jeden Ausgabeparameter gebundene Heap-Variablen-Slot (-1 = nicht gebunden, -2 = unbestimmt) |
| `movedVarBitset` | `[]uint64` | Compile-time-moved-Bitmap (verwendet ohne Laufzeit-Bitmap) |
| `movedBitmapBase` | `string` | Präfix des Namens der Laufzeit-Bitmap-Variable (z. B. `%__mb`, leer = nicht allokiert) |
| `bitmapCount` | `int` | Anzahl der u64-Bitmap-Blöcke (= maxVarIdx/64 + 1) |

#### Slot-Zuordnungsregel

Ein `u64`-Block speichert 64 Markierungsbits; für einen Heap-Variablen-Slot `varIdx`:

- Blocknummer = `varIdx / 64`
- Offset im Block = `varIdx % 64`
- Maske = `1u64 << Offset`

Diese werden zur Kompilierzeit als Konstanten berechnet, es entstehen keine Laufzeitkosten. Mehrere `u64`-Blöcke unterstützen eine beliebige Anzahl von Heap-Variablen, **ohne Grenze für die Anzahl von Parametern/Rückgabewerten**.

#### move-Zuweisungsbehandlung (Überschreiben löscht das alte Bit)

Jedes Mal, wenn eine Heap-Variable in einen Ausgabeparameter move'd wird:

1. Falls dieser Ausgabeparameter vorher an eine andere Variable gebunden war (`outBindState[outIdx] >= 0`), zuerst das Bit der alten Variable löschen
2. Dann das Bit der aktuellen Variable auf 1 setzen
3. Den an diesen Ausgabeparameter gebundenen Variablen-Slot aktualisieren (`outBindState[outIdx] = srcVarIdx`)

#### Freigabe am Funktionsende

Alle Heap-Variablen werden durchlaufen und unabhängige `if`-Prüfungen angelegt: Ist das entsprechende Bit 0, wird freigegeben; ein Bit von 1 bedeutet, dass der Besitz wegtransferiert wurde, und die Freigabe wird übersprungen.

#### Bitmap bei Bedarf allokiert

Die Bitmap-Variable wird nur **bei Bedarf** allokiert, um Overhead in verzweigungsfreiem Code zu vermeiden:

| Szenario | Bitmap-Allokation | free-Verhalten |
|------|---------|----------|
| Kein move | keine | alle freigeben |
| move nicht in Verzweigung (deterministischer move) | keine | Compile-time-`movedVarBitset` überspringt `free` direkt |
| move in Verzweigung (bedingter move) | allokiert | Laufzeit-Bitmap-Prüfung: Bit=1 überspringen, Bit=0 freigeben |

Vor der Generierung des Funktionskörpers prüft der Compiler den AST vorab (`detectBranchMoveToOut`), ob eine move-Zuweisung an einen Ausgabeparameter innerhalb eines `IfExpression`/`ForStatement`/`ConditionalExpression`-Zweigs existiert, und allokiert die Laufzeit-Bitmap-Variable nur bei Vorliegen eines solchen Musters. Das Bitmap-`alloca` wird nach der Generierung des Funktionskörpers eingefügt (zu diesem Zeitpunkt hat `nextHeapVarIdx` seinen Endwert) und in den Entry-Block geschrieben.

Dieser Mechanismus gilt für alle Heap-Typen (`vec`/`str-long`/`arr`/Benutzerstrukturen).

## Tiefen-Clone (Zuweisung zwischen lokalen Variablen)

```no
a []i64 = [10, 20, 30]
b = a; 深層 clone：malloc 新 data + memcpy + 遞迴 clone 元素
b[0] = 99
; a[0] == 10（a 不受影響）
; b[0] == 99（b 獨立修改）
```

### Ablauf des Tiefen-Clones
1. Alten Wert der Zielvariablen freigeben (falls bereits Heap-Daten vorhanden)
2. Neuen Datenpuffer per `malloc` allokieren, Quelldaten per `memcpy` hineinkopieren
3. Jedes heap-besitzende Element rekursiv clonen:
   - `%str-long`-Element: malloc + memcpy der Zeichendaten
   - Benutzerstruktur-Element: memcpy der Struktur + rekursives Clonen der Heap-Felder
4. Neuen Datenzeiger, len, cap in die Zielvariable schreiben
5. Ziel als Heap-Variable erfassen (wird bei Funktionsende freigegeben)

### Clonbare Typen
| Typ | tief clonbar | Anmerkung |
|------|-------------|------|
| `%vec` / `%arr` (Grundtyp-Elemente) | ✅ | memcpy der Daten genügt |
| `%vec` / `%arr` (Elemente sind %str-long) | ✅ | pro Element malloc+memcpy der Zeichendaten |
| `%vec` / `%arr` (Elemente sind clonbare Strukturen) | ✅ | pro Element rekursives Clonen der Strukturfelder |
| `%vec` / `%arr` (Elemente sind %vec / %arr) | ✅ | rekursives Clonen der inneren Container-Elemente über den `elemElemType`-Mechanismus |
| `%str-long` | ✅ | malloc + memcpy der Zeichendaten |
| Benutzerstruktur (ohne verschachtelte Containerfelder) | ✅ | memcpy der Struktur + rekursives Clonen der Heap-Felder |
| Benutzerstruktur (mit verschachtelten Containerfeldern) | ✅ | memcpy der Struktur + rekursives Clonen der Felder mit verschachtelten Containern (über `elemElemType`) |
| `?str` | ✅ | Option mit Inline-Nutzlast: Option und ihre Zeichendaten clonen |
| `?[]T` | ✅ | Option mit Inline-Nutzlast: Option und Daten des inneren Slices clonen (pro Element; rekursiv bei heap-besitzenden Elementen wie `?[]str`) |

> **`?str` / `?[]T` haben Inline-Nutzlast, besitzen aber Heap.** Ihre Nutzlast liegt direkt im eigenen
> `%option_<key>` der Option (`?str` hat die Struktur `{ i8 tag, [2 x i64] }`, die 16-Byte-Nutzlast enthält
> `{ u32 len, u32 cap, i8* data }`), aber die `str`-Daten bzw. der Slice-Backing-Store darin sind heap-allokiert
> ⇒ **beim Clonen muss die Nutzlast mit tief kopiert werden, beim Freigaben muss sie mit freigegeben werden**, beide Seiten
> müssen gepaart bleiben. Das Freigeben von `?[]T` läuft über denselben Tiefen-free-Helper wie `[]T` (Rekursion nach Elementtyp),
> daher gilt auch hier die Dualität „Tiefen-Clone passt zu Tiefen-free".
> ⚠️ Eine gewöhnliche (nicht `?str`/`?[]T`-) Option hat eine geliehene oder reine Wert-Nutzlast und **besitzt keinen**
> Heap; clone und free lassen sie unangetastet.
> Ist die Nutzlast größer als `option-inline-threshold`, speichert die Option stattdessen einen Heap-Zeiger; dann muss
> beim Freigaben auch die Box freigegeben werden (bestimmt durch `Boxed`, siehe `src/mir/layout.go`).

### Unterschied zu move
- **Tiefen-Clone**: Quelle und Ziel besitzen Daten unabhängig, werden bei Funktionsende jeweils freigegeben
- **move**: Quelle gibt Besitz auf (als moved markiert), Ziel übernimmt die Daten, Quelle überspringt `free`

Entscheidungsregeln für `b = a`:
1. Falls a die Quelle eines Ausgabeparameters ist → move
2. Andernfalls, falls a Heap-Typ besitzt, aber **nach der Zuweisung nicht mehr referenziert** wird → move (Zero-Copy, beobachtbar äquivalent zum Tiefen-Clone)
3. Andernfalls, falls a Heap-Typ besitzt und tief clonbar ist → Tiefen-Clone
4. Andernfalls Wertkopie

`vec.push(x)` fällt nicht unter diese Regeln: push ist ein Methodenaufruf und führt unabhängig davon, ob x Heap besitzt, bei heap-besitzenden Elementen einen Tiefen-Clone durch (siehe vorheriger Abschnitt).

## Stack-Typ-move (Stack-Slot-Rebind / slot-rebind)

`i64`/`u64`/`i128`/`u128`/`txt` sind zwar Stack-Typen (kein Heap-Besitz), aber bei `b = a` (RHS ist eine Variable) wird, wenn die Quelle `a` anschließend nicht mehr referenziert wird, keine Wertkopie mehr durchgeführt, sondern ein **Stack-Slot-Rebind**: Er setzt `g.varAlias[b] = a`, sodass `b` und `a` denselben Stack-Slot teilen (0 Kopien). Das ist besser als eine Wertkopie (die weiterhin load+store emittieren müsste) und bringt bei großen Stack-Typen wie dem 256-Byte-`%txt` besonderen Nutzen.

### can_slot_rebind-Bedingung (konservative Korrektheit)

Stack-Slot-Rebind ist **strenger** als der Heap-move `moveEligible`:

- `moveEligible`: Quelle `a` wird anschließend nicht **gelesen** → move erlaubt (gilt für Heap-Typen; nach dem move überspringt die Quelle `free`, das Ziel ist unberührt).
- `can_slot_rebind` (`slotRebindSafe`): Quelle `a` hat anschließend **keinerlei Referenz (weder lesend noch schreibend)** → Rebind erlaubt. Nach dem Rebind teilen `b` und `a` denselben Slot, ein späteres Schreiben auf `a` würde den Wert von `b` zerstören.

Berechnung: `generateFunctionDefinition` ruft `computeMoveEligibility` auf (füllt sowohl `moveEligible` als auch `slotRebindSafe`); `generateMainFunction` ruft `computeSlotRebindSafety` auf (**nur** `slotRebindSafe` wird gefüllt, `moveEligible` bleibt unangetastet — die Hauptfunktion in HEAD aktiviert keine Heap-moves, aus Gründen der SEGFAULT-Vermeidung deaktiviert gehalten). Beide nutzen `stmtContainsVarRefAny` für eine Verzweigungs-/Schleifen-aware Referenzprüfung (inkl. `AssignExpression`-Lvalues und Schleifen-Rückkanten).

Ist `can_slot_rebind` nicht erfüllt (Quelle wird später noch referenziert) → **Rückstufung auf eine vollständige Wertkopie**, niemals unsicheres Verhalten.

### Abschalt-Szenarien (müssen den normalen Zuweisungspfad nutzen)

| Szenario | Grund |
|------|------|
| stdlib-Funktion (`curIsStdLib`) | stdlib enthält match/closure/coroutine-Konstrukte, die die Referenzanalyse nicht vollständig modellieren kann; ein Rebind würde geteilte Stack-Slots zerstören (z. B. fmt-internes `it`, alias-gebunden an ein lokales `n`). Erkennung: `g.stdModules` (von `transpiler.go` `SetStdModules` aus `checker.KnownStdModules()` injiziert), Rückfall auf `g.funcOwner[fd.Name]` |
| Ziel ist ein Ausgabeparameter | Ausgabeparameter ist ein vom Aufrufer übergebener Zeiger; ein Rebind würde ihn auf den lokalen Quell-Slot zeigen lassen, der Aufrufer könnte ihn nicht lesen |
| Ziel ist eine globale Variable | Ein Rebind lässt den globalen Namen auf einen lokalen Quell-Slot auflösen und zerstört die globale Semantik |
| Ziel ist eine Heap-Typ-Variable | Stack-Slot-Rebind gilt nur für Stack-Typen |
| Quelle ist ein Parameter | Parameter werden als Referenz übergeben; ein Rebind zerstört den Stack-Frame des Aufrufers |
| Quelle ist eine globale Variable | Die globale Adresse wird auf eine lokale Quelle aliasiert, semantisch falsch |

### Alias-Ungültigmachung nach Neuzuweisung

Wird eine Variable neu zugewiesen (`b = ...`), wird zuerst `delete(g.varAlias, name)` aufgerufen, um einen möglicherweise verbliebenen alten Alias zu entfernen, damit der allgemeine Zuweisungspfad nicht über `varAddr(name)` weiterhin auf den alten Quell-Slot zeigt und die alte Quelle verunreinigt. Beim Setzen eines neuen Alias wird entlang der Alias-Kette eine **transitive Auflösung** vorgenommen (`src := ident.Value; for { if n2,ok := g.varAlias[src]; ok { src = n2 } else break }`), um den endgültigen Quell-Slot zu finden und Fehlversätze bei mehrstufigen Aliasen zu vermeiden.

### Orthogonalität zum Heap-move

Stack-Slot-Rebind wirkt nur auf Stack-Typen, beinhaltet weder Besitzübertragung noch `free` und ist vollständig unabhängig vom clone/move-Mechanismus der Heap-Typen.

**Test**: `tests/slot-rebind.no` (deckt i64/u64/i128/u128/txt-Rebind, Rückstufung auf copy, Alias-Ungültigmachung nach Ziel-Neuzuweisung, Parameter-move-Rückstufung, consume-Übergabe ab; erwartete Ausgabe `42 7 1 1 17 5 5 10 100 99 84`).

## FFI-extern-str-Rückgabewerte

FFI-externe Funktionen (markiert mit `#{c}`) geben C-Zeichenkettenzeiger (`i8*`) zurück, die auf statischen Speicher (z. B. `getenv`, `strerror`) oder externe Puffer (z. B. `strchr` gibt einen Zeiger in sein Argument zurück) zeigen können. Ein direktes Verpacken in `%str-long` würde dazu führen, dass `emitHeapFree` Nicht-Heap-Speicher mit `free()` freigibt → UB.

Der Compiler fügt auf dem FFI-extern-`str`-Rückgabepfad eine sichere Kopie ein:

1. **NULL-Prüfung**: Gibt C NULL zurück, wird ein leeres `%str-long` konstruiert (data=0, len=0). Beachte, dass `str` keine Option ist, also **`s == nil` ein Kompilierfehler ist** (`q4nilcmp`); um NULL von der leeren Zeichenkette zu unterscheiden, deklariere das extern als Rückgabe von `?str` (NULL ⇒ `nil`) oder prüfe stattdessen `s.len == 0`
2. **Nicht-NULL**: `strlen` + `malloc` + `memcpy` + Null-Terminierung, Kopie in einen unabhängigen Heap-Puffer
3. **PHI-Zusammenführung**: Beide Pfade werden zusammengeführt und der `%str-long`-Rückgabewert konstruiert

```no
#{c}
strchr = (s str, c i64) (r str)

find = () (r str) {
    r = strchr('hello', 108); C 返回指向 'hello' 內部的指標
    ; 編譯器自動 malloc+memcpy 複製，r 獨立擁有 data
    ; 函數結束時 emitHeapFree 安全釋放 r.data
}
```

Dieser Mechanismus entspricht dem clib-Pfad `RetCStrToStr` (verwendet von Builtins wie `get-env`, `get-wd`) und stellt sicher, dass alle C-Zeichenketten-Rückgabewerte unabhängigen Besitz haben.

## Freigabe von Modulvariablen

Modulweite Heap-Variablen (`vec`/`str`/`arr`/Strukturen) werden als LLVM-Globals (`@name`) kompiliert; ihre `data`-Puffer werden durch Top-Level-Anweisungen am `main`-Einstieg per malloc initialisiert.

Der Compiler ruft vor `ret i32 0` im C-Einstieg `main` Folgendes auf:
1. `emitHeapFree` — gibt Top-Level-lokale Heap-Variablen frei (nicht in globalVars)
2. `emitGlobalHeapFree` — durchläuft `moduleVarTypes` und gibt alle heap-besitzenden Typen in `globalVars` frei

```no
GLOBAL-STR = 'hello'; LLVM @GLOBAL-STR = global %str-long zeroinitializer
GLOBAL-VEC = [1, 2, 3]; LLVM @GLOBAL-VEC = global %vec zeroinitializer
; top-level 語句 malloc data 並存入 global
; main ret 前 emitGlobalHeapFree 釋放 data
```

Dies verhindert Speicheransammlungs-Leaks in langlebigen Diensten (z. B. Daemons mit Schleifen). Für einmalige CLI-Werkzeuge gibt es keine Auswirkung (Prozessende wird vom OS zurückgefordert).

## Slice-Ansichten

Ein Slice-Ausdruck `arr[1..3]` erzeugt eine Ansicht (Zero-Copy), die die Daten des Originalarrays teilt. Drei Schicksale einer Ansicht:

| Ziel | Verhalten | Besitz |
|------|------|--------|
| lokale Variable `v = arr[1..3]` | Zero-Copy-Ansicht | teilt Original-Array-Daten |
| Ausgabeparameter `out = arr[1..3]` | Clone (malloc+memcpy) | unabhängig |
| expliziter `[]T`-Typ `v []i64 = arr[1..3]` | Clone | unabhängig |

**Grund**: Ausgabeparameter entkommen zum Aufrufer; das Originalarray könnte vor Funktionsende freigegeben werden, daher muss die Ansicht zu unabhängigen Daten klonen.

## Neuzuweisung und Freigabe des alten Werts

```no
s = 'hello'; malloc data 緩衝區
s = 'world'; 釋放 'hello' 的 data，malloc 新 data
```

Bei Neuzuweisung eines heap-besitzenden Typs gibt der Compiler vor der Zuweisung automatisch die Daten des alten Werts frei, um Leaks zu vermeiden.

## Freigabe von Strukturfeldern

```no
Node {
    name str
    items []i64
}

n = Node {
    name: 'hello'
    items: [1, 2, 3]
}
; 函數結束時遞迴釋放：
;   - n.name.data（%str-long 欄位）
;   - n.items.data（%vec 欄位）
```

Beim Freigeben einer Struktur werden alle Felder durchlaufen; heap-besitzende Felder werden rekursiv freigegeben.

## Festes Array, das als Slice neu zugewiesen wird

```no
local [4]i64 = [100, 200, 300, 400]; local 是固定陣列（16 字節）
local = [100, 200, 300]; 重新賦值為切片（16 字節）
```

Feste Arrays (`%arr`, 2 Felder) und Slices (`%vec`, 3 Felder) haben unterschiedliche Speicherlayouts. Bei der Neuzuweisung allokiert der Compiler automatisch eine neue `%vec`-Variable und leitet alle nachfolgenden Zugriffe um, um Pufferüberläufe zu vermeiden.

## Verifizierte Testfälle

Die Tests liegen in `tests/mem-safety/`:

| Test | Prüfinhalt |
|------|---------|
| `deep-clone.no` | `b = a` Tiefen-Clone ([]i64/[]str/str/Struktur) Unabhängigkeit |
| `deep-free-str.no` | `[]str` tiefes free |
| `deep-free-nested-vec.no` | `[][]i64` tiefes free + push-Tiefen-Clone |
| `deep-free-struct-vec.no` | `[]MyType` tiefes free (rekursive Struktur) |
| `struct-field-leak.no` | Freigabe von Heap-Daten in Strukturfeldern |
| `slice-view-escape.no` | Clone einer Slice-Ansicht, die einem Ausgabeparameter zugewiesen wird |
| `reassign-leak.no` | Freigabe des alten Werts bei Neuzuweisung |
| `vec-push-leak.no` | Freigabe des alten Puffers bei vec.push-Expansion + Tiefen-Clone heap-besitzender Elemente |
| `ffi-str-return.no` | sichere Kopie von FFI-extern-str-Rückgabewerten |
| `global-heap-free.no` | Freigabe modulweiter Heap-Variablen bei main-Exit |
| `double-move-same-source.no` | `a=x; b=x` mehrfache Zuweisung aus derselben Quelle: erster Clone + letzter move |
| `move-clone-liveness.no` | Liveness-Voranalyse entscheidet clone/move (inkl. bedingter Verzweigungen, dreifache Zuweisung) |
| `rule1-binding.no` | Regel 1: `b = a` Schreibunabhängigkeit für `str`/`[]i64`/struct/`?str`/`?[]i64`, schreibgeschütztes Aliasing, Dead-Source-move |
| `nested-container-clone.no` | Dualität Tiefen-Clone/Tiefen-free für verschachtelte Container (`[][]str`, vec in struct, map-Wert ist vec usw., 12 Fälle) |

## Bekannte Einschränkungen

### map-Container
hashmap implementiert kein tiefes `free` von key/value; Heap-Daten des map-Containers leaken.

### Temporäre Variablen in Schleifen
```no
#{fmt-skip}
loop {
    s = 'temp'   ; 每次迭代 malloc 新 data，舊 data 未釋放
}
```

### Slice-Ansicht + move des Originalarrays
```no
view = arr[1..3]; view 共享 arr.data
arr = [9, 8, 7]; 釋放舊 arr.data → view 懸空
```

### Async-geteilte Daten (am 2026-09-30 behoben)

Die Spawn-Grenze von `awy { r = co f(x) }` **kopiert jetzt tief** heap-besitzende Argumente; Aufgaben teilen keine Puffer mehr mit dem Aufrufer.

Coroutinen sind kooperativ und single-threaded, daher gibt es „geteilte veränderliche Heap-Daten über Coroutinen hinweg" auf Sprachebene nicht; das Einzige, was tatsächlich geteilt wurde, war **die Kopie, die der Aufrufer nach dem Spawn noch hält**. Vor der Korrektur war `argbuf` nur eine **Bitkopie** des Argument-Descriptors, `data` wurde mit dem Aufrufer geteilt, und die Aufgabe wurde erst am Wartepunkt ausgeführt — wenn der Aufrufer davor neu zuwies oder den Gültigkeitsbereich verließ, wurde der Puffer freigegeben und die Aufgabe las bereits freigegebenen Speicher (gemessen: `str` gab 40 NUL-Zeichen aus, `[]i64` gab 0 aus, beide mit rc=0 und ohne Diagnose).

Jetzt wird beim Spawn tief kopiert, und der Wrapper gibt die Kopie nach Ende des Aufrufs frei:

| Parametertyp | Spawn-Grenze | Wrapper-free |
|---|---|---|
| `str` | `@str_clone` | `@str_free` |
| `[]T` (T hat Deep-Clone-Helper) | `vecDeepClone` | `@vec_free` |
| Heap-besitzende Struktur (Inline-`str`-Blätter / Zeigerfelder) | In-Place-Blatt-Clone + Kopie des Pointee | rekursiver Strukturdestuktor |
| Sonstige (reine Werttypen) | Bitkopie (korrekt) | nur Container freigeben |

Regression: `tests/async-ownership.no`.

### struct-Neuzuweisungs-Leak (am 2026-09-30 behoben)

```no
holder {
    s str
}
h holder = holder {
    s: 'first'
}
h = holder {
    s: 'second'
}
; 舊的 'first' buffer 曾洩漏
```

Beim Rebind einer lokalen Variable wird der alte Wert nur freigegeben, wenn sein Typ `Type.Owned` ist (`str`/`vec`/`[]T`/`map`/`?owned`). Eine Struktur zählt **bewusst nicht** als `Type.Owned` (dieses Flag dient zugleich dem Lowerer als Kriterium dafür, ob „eine Bindung ein Alias ist"), daher überschreibt `h = holder { … }` die gesamte Struktur, ohne den alten `s`-Puffer je freizugeben. Der Drop-Mechanismus kennt Strukturen aber sehr wohl (`dropOwnsHeap` → `typeOwnsHeap`, und `emitDrop` gibt Felder über den rekursiven Destuktor frei); nur das Gate des Lowerers war zu eng. Nach der Korrektur werden alte Felder vor dem Rebind freigegeben.

Messung mit 2.000.000 Neuzuweisungen: Peak-RSS von 66,1 MB auf 33,8 MB gesunken, Ausgabe byte-genau identisch.

### Erstes Feld eines struct-Literals ist ein Methodenaufruf (am 2026-09-30 behoben)

```no
holder {
    s str
}
h = holder {
    s: n.to-str()
}
; 修復前：'holder' is not defined
```

Die Einordnung von `{ … }` hängt von einer Vorausschau um einige Tokens ab. Der **Zweigseparator eines match ist ebenfalls `:`**, daher sieht `s: n.to-str()` sowohl wie „ein match-Zweig mit Pattern `s`" als auch wie „ein Feld eines struct-Literals" aus. Das einzige Unterscheidungskriterium ist das Token **nach** `ident.ident` — ein Methodenaufruf kann nur ein Feldwert sein. Der alte Zweig übersah `(`, sodass der ganze Block als match eingestuft wurde und der Strukturname im match-Kontext nicht gefunden wurde (ein gewöhnlicher Funktionsaufruf als Feldwert funktionierte).

Nach der Korrektur wird `h = holder { s: recv.method(args) }` korrekt geparst. Ein Nolang-match-Zweig kann nicht mit `name : X(` beginnen (der Zweigseparator ist `->`; mit `:` beginnende Zweige sind immer Wildcard/Default und ihr Rumpf enthält keinen geklammerten Aufruf), daher führt das Zulassen von `(` nicht dazu, irgendein legitimes match als struct-Literal fehlzueinordnen. Regression: `src/parser/struct_literal_field_value_test.go`.

### Await-Ergebnistyp (Syntaxvereinfachung 2026-11-17)

Die alte Syntax legte ein Task-Handle als `i64`-Variable offen, wofür der Ergebnistyp über „Handle-Slot-Rebind" mitgeführt werden musste
(`t i64 = 0; t = run f(...); v = awy t`). Nach Entfernung von `run` und `awy <handle>` ergibt sich der Ergebnistyp eines
`co`-Ausdrucks direkt aus dem Aufgerufenen; dieser Pfad existiert nicht mehr. Die Regression wird jetzt über
`src/mir/async_boundary_ownership_test.go` am unteren MIR-Ergebnistyp festgenagelt.

### Task-Referenzzählung (Syntaxvereinfachung 2026-11-17)

Intern bleibt `%task` das einzige referenzzählte Objekt: beim Spawn `rc = 1`, beim Abschluss des Wartens `release`.
Die alte Syntax legte handgeschriebene Handles offen; Kopierpfade wie `h2 = h` / `run h` vergaßen retain
(besonders gravierend bei Tasks mit void-Ergebnis, gemessen SIGSEGV) und wurden vom Verifizierer `checkRefBalance` abgefangen.
Das neue `awy { ... }` legt keine Handles offen, und am Gruppenende wird jedes `co` genau einmal erwartet, daher kann
die Oberflächensyntax keine Referenzen außerhalb der Liste erzeugen. Die Handle-Alias-/RC-`.no`- und Go-Tests wurden
mit `run` entfernt; die MIR-Paarung `OpTaskRetain` / `OpAwait` bleibt durch MIR-Fixtures in `src/mir/opt_test.go` abgedeckt.

### Verschieben von Spawn-Argumenten: Zero-Copy, wenn die Quelle tot ist (2026-10-01)

`co f(x)` führt für jedes **heap-besitzende** Argument an der Spawn-Grenze eine Tiefenkopie durch, weil die Aufgabe erst
am Gruppenende ausgeführt werden könnte und der Aufrufer davor neu zuweisen oder den Gültigkeitsbereich verlassen kann.
Wenn x nach dem Spawn **sicher nicht mehr verwendet wird**, ist diese Kopie überflüssig: Der Besitz wird direkt **in die
Aufgabe übertragen**.

```no
s str = 'hello'
awy {
    v = co echo(s)      ; s 之後不再被讀 -> 零拷貝轉移
}
print(v)                ; hello

s str = 'hello'
awy {
    v = co echo(s)
}
print(s)                ; s 仍被讀 -> 維持深拷貝（兩邊各自擁有）
print(v)                ; hello
```

Drei Kriterien, alle notwendig:

1. Der Aufrufer **würde x ohnehin** freigeben — geliehene Werte (Slice-Ansichten, borrow-read) und Parameter werden vom Aufrufer nicht freigegeben;
2. x ist nach dem Spawn tot (**keinerlei spätere Lesen**);
3. Der Parameter an dieser Position im Aufgerufenen ist `str` / `[]T` / heap-besitzendes `struct` — der Wrapper gibt seine Nutzlast frei.

Für diese drei Typen wird die Freigabe im Task-Wrapper und der ursprüngliche Drop des Aufrufers **auf dieselbe Helper-Funktion reduziert**, daher bedeutet die Übertragung nur, **dieselbe Freigabe** vom Aufrufer in die Aufgabe zu verlegen — es wird weder zu viel noch zu wenig freigegeben.

**Unveränderte Benutzersemantik**: Bei nicht erwarteten Aufgaben werden die Argumentpuffer ohnehin erst beim Warten vom Wrapper freigegeben; Verschieben und Kopieren verhalten sich diesbezüglich identisch.

Implementierung: `insertDrops` vermerkt es in `Module.spawnArgMoves`, `emitAsyncRun` überspringt daraufhin die Tiefenkopie.
Regression: `tests/async-arg-move.no`, `src/mir/spawn_arg_move_test.go`.

### Alias der Coroutinen-Waiter-Tabelle (am 2026-09-30 behoben)

Der Scheduler nutzte ursprünglich `@nolang_waiters = global [256 x i8*]` mit `ptrtoint(task) & 255` als Index.
Tasks werden von `@malloc` mit **16-Byte-Ausrichtung** allokiert, die unteren Bits des Zeigers haben nur 4 nutzbare Bits —
256 Slots kollabierten effektiv auf 16 (gemessen: nur 8 wurden je verwendet), zwei gleichzeitig erwartete Tasks konnten
sich gegenseitig überschreiben und den falschen Task aufwecken.

Jetzt wurde die gesamte Tabelle entfernt; Waiter werden im 5. Feld von `%task` gespeichert (`%task` wuchs von 24 auf
32 Bytes): `@nolang_async_wait` schreibt den aktuellen Task in das Feld des **erwarteten Tasks**,
`@nolang_async_done` liest dieses Feld, reiht den Waiter bei Nicht-NULL ein und leert es. Die Suche ist damit präzise,
es gibt keinen Adress-Alias mehr.

> Hinweis: Das MIR-Backend treibt await derzeit synchron an und ruft nie `@nolang_async_run` / `@nolang_async_wait` auf,
> daher ist dies heute ein **latenter** Defekt kein beobachtbarer Fehler. Regression:
> `src/mir/async_boundary_ownership_test.go`.

### `awy` wird verschluckt, wenn es nicht die erste Anweisung eines Blocks ist (am 2026-09-30 behoben; 2026-11-17 umgeschrieben)

`skipToStatementEnd()` schreitet bis zu einem Token vor, das die nächste Anweisung **beginnen** kann; die Whitelist
`isStatementBoundary()` (`src/parser/stmt.go`) hatte einst Präfix-Schlüsselwörter übersehen. Unter der alten Syntax konnten
`run` / `awy t` daher, wenn sie nicht die erste Anweisung waren, zusammen mit dem Zeilenumbruch übersprungen werden,
was eine semantische Degradation mit `rc=0` und ohne Diagnose ergab.

In der neuen Syntax hat `awy` einen eigenen `parseStatement`-Fall (Coroutinen-Gruppe) und steht zusammen mit `co` in
`isStatementBoundary()`; `run` ist kein Schlüsselwort mehr und wurde aus der Whitelist entfernt. Regel: **Jedes Token, das
der `switch` von `parseStatement` dispatchieren kann, muss auch in `isStatementBoundary()` stehen**.

Regression: `tests/stmt-boundary.no`, `src/parser/stmt_boundary_block_test.go`.

### Nacktes `{ ... }` in Anweisungsposition wurde als Deklaration behandelt (am 2026-09-30 behoben)

Der `LBRACE`-Zweig von `parseStatement` ruft `classifyBlockAtCurrent()` auf (zu diesem Zeitpunkt ist `currentToken`
bereits `{` und es kann kein Name mehr konsumiert werden). Die Antworten `blockEnum` / `blockIface` / `blockTaggedEnum`,
die er zurückgeben kann, sind **in Anweisungsposition bedeutungslos** — echte Enum-/Interface-/Tagged-Enum-Deklarationen
benötigen alle einen vorangestellten Namen und werden früher über den `classifyBlock()`-Zweig abgehandelt. Die
Anweisungs-Distribution leitete aber nur `blockUnknown` in den Anweisungsblock-Pfad, daher:

- `{ x = 2 }` wurde als `blockEnum` klassifiziert, `{ print(2) }` als `blockIface`;
- beide landeten in `parseExpressionStatement`, dessen `LBRACE`-Fall in `expr.go` nur
  `blockStruct` / `blockMatch` akzeptiert und sonst `p.nextToken(); return nil` ausführt;
- das `{` wurde gegessen, die inneren Anweisungen des Blocks wurden zu **Geschwistern** des umgebenden Rumpfs, und das
  `}` des Blocks schloss tatsächlich den **umgebenden** Block.

Das Ergebnis ist kein „Verschlucken", sondern ein **Statement-Leck um eine Ebene**: `f = () { print(1); { print(2) }; print(3) }` gab
`3,1,2` aus; `f = () { x = 1; { x = 2 }; print(x) }` gab gar nichts aus. Wieder `rc=0`, keine Diagnose.

Die Korrektur leitet `blockEnum` / `blockIface` / `blockTaggedEnum` ebenfalls in den Anweisungsblock
(`blockStruct` muss **im** Ausdruckspfad bleiben — `{ field: value }` ist ein legitimes anonymes struct-literal expression statement).
⚠️ **Ändere nicht** den `ASSIGN`-Zweig von `classifyBlockAtCurrent`, um dies zu „reparieren": Das deckt nur eine der drei
Fehlklassifikationen ab und verlagert die Entscheidung weg von der einzigen Stelle, die weiß „ich bin in Anweisungsposition".

⚠️ **Nebenwirkung**: Diese Korrektur ließ einen `{ ... }` in Anweisungsposition erstmals tatsächlich als
`*parser.BlockStatement` überleben, und die Whitelist von `programUsesPrint`
(`src/build/transpiler.go`, der handgeschriebene AST-Walker, der entscheidet, ob `fmt`/`io`/`str`/`byte` geladen werden)
hatte keinen `BlockStatement`-Fall ⇒ Programme, die `print` nur in einem nackten Block enthalten, wurden nicht mehr erkannt.
**Nach jeder Änderung an der Anweisungsebene `go test ./build/ -run TestProgramUsesPrint` erneut ausführen** und den neuen
Containertyp zu `walkStmt` hinzufügen. Regression: `src/parser/stmt_boundary_block_test.go`, `tests/stmt-boundary.no`.

### Unpräzise free-Skip-Heuristik bei der ersten Zuweisung einer globalen Variable

Der Compiler verwendet eine Compile-Time-Map `globalFirstAssigned`, um zu verfolgen, ob eine globale Variable bereits ihre
erste Zuweisung hatte: Die erste Zuweisung überspringt das Freigeben des alten Werts (der alte Wert ist `zeroinitializer`,
keine Heap-Daten), erst spätere Neuzuweisungen geben den vorherigen Heap-Wert frei.

Diese Map wird während der gesamten Kompilierung nur einmal initialisiert, nicht pro Funktion zurückgesetzt und
unterscheidet keine bedingten Verzweigungs-Pfade. Wenn die erste Zuweisung einer globalen Variable innerhalb einer
Bedingung erfolgt, verarbeitet der Compiler sie in AST-Reihenfolge: Die erste Zuweisungsanweisung wird als „die erste"
behandelt (free übersprungen), während eine zweite Zuweisungsanweisung (selbst in einem anderen Zweig) den
Neuzuweisungs-Pfad nimmt (versucht, den alten Wert freizugeben). Wird zur Laufzeit zuerst der zweite Zweig ausgeführt,
ist die Globale noch `zeroinitializer` (`data=NULL, len=0`) und es wird versucht, einen nicht initialisierten alten Wert freizugeben.

**Aktuelle Abhilfemaßnahmen (wirksam)**:
- Flache Container (`%str-long`): `emitNullCheckFree` erzeugt eine Laufzeitprüfung `icmp eq i8* dataPtr, null` und überspringt `call @free` bei NULL
- Tiefe Container (`%vec/%arr`): `emitDeepContainerFree` hat zusätzlich einen `len == 0`-Kurzschluss; `zeroinitializer` hat len 0, daher wird die gesamte Free-Schleife direkt übersprungen

Diese zwei Laufzeit-Schutzschichten bedeuten, dass trotz unpräziser Compile-Time-Entscheidung kein tatsächlicher Absturz auftritt. Logisch stützt man sich dabei aber auf Laufzeit-NULL-Prüfungen als Sicherheitsnetz statt auf eine präzise Compile-Time-Entscheidung.

**Möglicher Verbesserungsweg**: `globalFirstAssigned` von einer Compile-Time-Map in einen Laufzeit-Verfolgungsmechanismus umwandeln (eine Bitmap ähnlich `movedVarBitset`), was aber Laufzeit-Overhead hinzufügt; die aktuellen Abhilfen sind bereits ausreichend wirksam.

## Design-Kompromisse

1. **Die Kosten eines Tiefen-Clones.** `b = a` allokiert und kopiert die Daten; `vec.push(x)` führt ebenfalls einen Tiefen-Clone eines heap-besitzenden Elements durch.
   Das ist beabsichtigt: **eine Kopie kauft eine ganze Klasse von Alias-Analyse zurück** — zwischen Wert und Wert gibt es nur „besitzen" und „übertragen", nie „teilen",
   daher muss „wer besitzt, wer gibt frei, ist ein Schreiben beobachtbar" nicht für jede Alias-Form eine eigene Regelmenge haben.
2. **Tiefe Kopie an der Spawn-Grenze (= eine Momentaufnahme).** `co f(x)` kopiert heap-besitzende Argumente beim Spawn tief, daher sieht die Aufgabe den Wert
   **zum Zeitpunkt des Spawns**, unabhängig von späteren Schreibungen des Aufrufers. **Wenn die Quelle bereits tot ist, wird daraus ein Zero-Copy-move.**
   ⚠️ Ändere diese Kopie nicht aus Sparsamkeit in „einen Puffer teilen + Referenzzählung": Das würde die Aufgabe die **In-Place-Schreibungen** des Aufrufers
   zwischen Spawn und Gruppenende sehen lassen (Teilen), was Punkt 4 verletzt. Der richtige Weg zum Einsparen der Kopie ist **Copy-on-Write**.
3. **Task-Referenzzählung bildet keine Zyklen.** Intern ist `%task` das einzige referenzzählte Objekt; die neue `awy { ... }`-Syntax legt kein Handle offen,
   jedes `co` wird am Gruppenende genau einmal erwartet, daher kann sich kein „Tasks halten gegenseitig Handles"-Zyklus bilden.
4. **Coroutinen teilen keine veränderlichen Heap-Daten.** Coroutinen werden **kooperativ auf einem einzigen Thread** geplant, geteilte veränderliche Heap-Daten
   über Coroutinen hinweg gibt es auf Sprachebene nicht; das zu erlauben würde nur zu Data Races und baumelnden Zeigern führen. Die Spawn-Grenze übergibt
   daher stets eine **unabhängige Kopie** (oder überträgt den Besitz).
