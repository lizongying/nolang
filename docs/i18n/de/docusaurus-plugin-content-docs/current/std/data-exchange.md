---
sidebar_position: 4.1
---

## Datenaustausch

### json — JSON-Parsing und -Erzeugung

`json` speichert den JSON-Baum in einem **heap-allokierten Knotenpool** (`json-pool`) und unterstützt verschachtelte Arrays und Objekte, Skalare (String/Zahl/Boolesch/null), Escape-Zeichen und Zahlen im Exponentialformat. Der Knotenpool verwendet jetzt einen dynamischen `vec` und hat **keine feste Kapazitätsgrenze** — die Gesamtzahl der Knoten und die Zahl der KindElemente eines einzelnen Knotens sind nur durch den verfügbaren Speicher begrenzt. Die High-Level-API kapselt Knotenpool und Root-Knotenindex in der Struktur `json`; deren Feld `pool` ist mit `#{inline=false}` (Zeigerfeld) annotiert, sodass `json` sicher nach Wert zurückgegeben und kopiert werden kann.

```no
; 解析：回傳 ?json（ok=成功，err=失敗並附原因）
j = json.parse('{"name":"Alice","age":30,"items":[1,2,3],"active":true}')
j: {
    ok -> {
        name = it.get-str('name'); 取標量：get-str / get-num / get-bool / get-i64（?值）
        age = it.get-i64('age'); 整數取值（?i64）
        items = it.get('items'); 取子節點，回傳 ?json
        s = it.stringify(); 序列化回字串
    }

    err(e) -> print(e)
}

; 產生：從空 json 開始，逐步 set / arr-push
j2 = json.new()
j2.set-str('key', 'value')
j2.set-i64('count', 3)
j2.arr-push-str('a')
print(j2.stringify())
```

**High-Level-API (`json`-Struktur)**

- `json.new()` — erzeugt ein leeres (null) `json`
- `json.parse(s)` — parst und gibt `?json` zurück; bei Misserfolg trägt der `err`-Zweig den genauen Grund (`empty input` / `parse error` / `trailing characters`)
- `j.stringify()` — serialisiert in einen String
- Werte lesen: `j.get-str(key)` / `j.get-num(key)` / `j.get-bool(key)` / `j.get-i64(key)` (jeweils `?val`); `j.get(key)` gibt den Kindknoten als `?json` zurück
- Direkter Skalarzugriff (wenn root ein Skalar ist): `j.str()` / `j.num()` / `j.bool()` / `j.i64()` / `j.str-val()`
- Schreiben: `j.set-str(key,val)` / `j.set-num` / `j.set-i64` / `j.set-bool` / `j.set-null` / `j.set-key(key, val json)`
- Array: `j.arr-get(i)` / `j.arr-len()` / `j.arr-push(val json)` / `j.arr-push-str` / `j.arr-push-num` / `j.arr-push-bool`
- Objekt-Enumeration: `j.obj-len()` / `j.obj-key(i)` / `j.obj-keys-str()` / `j.delete-key(key)`
- Typprüfung: `j.kind()` sowie `j.is-null()` / `j.is-obj()` / `j.is-arr()` / `j.is-str()` / `j.is-num()` / `j.is-bool()`

**Pool-Kapazität (keine feste Obergrenze)**

Frühere Versionen verwendeten ein festes Array als Pool mit den harten Grenzen `JSON-MAX-NODES = 128` / `JSON-MAX-CHILDREN = 32`; Überschreitungen scheiterten still. Heute wird durchgängig ein Heap-`vec` verwendet:

- Weder die Gesamtzahl der Knoten noch die Zahl der KindElemente pro Knoten ist begrenzt, nur der verfügbare Speicher.
- `json.parse` gibt `err` nur bei echten Syntaxfehlern zurück; es gibt keinen Fehlerzweig „Pool erschöpft" mehr.
- `j.set*` / `j.arr-push*` geben nur `false` zurück, wenn der Zieltyp nicht passt (z. B. set auf einen Nicht-Objekt-Knoten, push auf einen Nicht-Array-Knoten). `j.overflowed()` bleibt aus Gründen der Rückwärtskompatibilität erhalten, gibt aber stets `false` zurück.

> **Kopiersemantik von `json` (`#{inline=false}`)**: `pool` ist ein Zeigerfeld; die von `j.get` / `j.arr-get` zurückgegebenen Kindknoten-`json` **kopieren den gesamten Pool tief** und sind eine unabhängige Momentaufnahme des Eltern-Pools — Schreiben in die Momentaufnahme wirkt nicht auf den Elternknoten zurück. Da ferner `?json` (Option) seinen Wert beim `match` konsumiert, führt ein **erneutes `match`** auf derselben Option in den `nil`-Zweig; wenn mehrmals mit demselben Parsergebnis gearbeitet werden soll, erledige dies in einem einzigen `match` oder nimm `it` zuvor in einer `json`-Lokalvariablen auf.

**Low-Level-API (`json-pool` / `json-value`)**

Direkte Arbeit mit Knotenindizes, der Pool wird als lokale Variable verwendet (flache Strukturen sind sicher nach Wert zurückzugeben/kopieren):

- `p = json.new-pool()`; oder `p = json-pool {}` gefolgt von `p.init()` (initialisiert die vecs `nodes`/`strs`/`ec`/`ek`/`en` jeweils mit `with-len(0)`)
- `idx = p.alloc()` — allokiert einen neuen Knoten und gibt den Index zurück (Heap-vec expandiert automatisch)
- `p.add-child(node-idx, child-idx, key)` — hängt ein Kindelement über die Kantenliste (`ec`/`ek`/`en` einfach verkettete Listen) an, ohne Limit pro Knoten
- `node-idx, next-pos, ok = p.parse(s, pos)` — parst einen Wert ab `pos` und gibt den Knotenindex zurück
- `val-idx = p.get-key(obj-idx, key)` (`?i64`); `p.arr-get(arr-idx, i)` (`?i64`); `p.arr-len(arr-idx)`
- `p.get-kind(idx)` / `p.get-str(idx)` / `p.get-num(idx)` / `p.get-bool(idx)`
- `p.set-key(obj-idx, key, val-idx)`; `p.copy-tree(src, idx)` (rekursive Tiefenkopie des Teilbaums)
- `p.stringify(node-idx)` — serialisiert den angegebenen Knoten

### toml — TOML-1.0-Parsing und -Erzeugung

`toml` speichert ein TOML-Dokument in `toml` und unterstützt Kommentare, bare/quoted/dotted Keys, normale Tabellen, array-of-tables, basic/literal/multiline strings, verschiedene Integer-Basen, Gleitkommazahlen, boolesche Werte, Datum/Uhrzeit, Arrays und Inline-Tabellen. Werte, für die es keinen eigenen Nolang-Typ gibt, behalten ihren ursprünglichen Literalwert.

```no
cfg = toml.parse('title = "Nolang"\n[server]\nport = 8080\n[[server.backends]]\nname = "local"')

doc = toml.new()
doc.put('name', '"nolang"')
doc.put('server.port', '8080')
doc.put('enabled', 'true')
raw = toml.get(doc, 'server.port')
text = toml.stringify(doc)
```

Häufige Funktionen:

- `toml.new()` — erzeugt ein leeres Dokument
- `toml.parse(text)` — parst und gibt `?toml` zurück
- `doc.put(key, raw-value)` — validiert und fügt einen TOML-Wert hinzu/aktualisiert ihn
- `get(doc, key)` — liefert den Rohwert
- `get-str`, `get-i64`, `get-f64`, `get-bool` — liefern Skalarwerte
- `toml.stringify(doc)` — serialisiert das Dokument

Die Dokumentgrenze liegt bei 4 Keys, 2 normalen Tabellen und 2 array-of-tables; Keys sind maximal 32 Bytes, Werte maximal 128 Bytes lang.

### yaml — YAML-1.2-Parsing und -Erzeugung

`yaml` speichert den Dokumentbaum des YAML-1.2-Core-Schemas in einem Knotenpool (`pool`) und unterstützt Block-Mappings, Block-Sequenzen, Flow-Sammlungen (`[...]` / `{...}`), single-/double-quoted und plain Scalare, Block-Scalare (`|` / `>` mit `-` / `+` Chomping), Kommentare, Dokumentmarkierungen (`---` / `...`) und Mehrdokumente, Anker (`&`) und Aliase (`*`), den Merge-Key `<<`, explizite Keys (`? k`), Typinferenz (null / bool / int / float / `.inf` / `.nan`), Punktpfad-Abfragen sowie JSON-/YAML-Serialisierung.

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
        port: {
            ok -> print(it.to-str())

            -> print('')
        }
        print(doc.to-json())
        print(doc.to-yaml())
    }
}
```

Häufige Funktionen und Methoden:

- `yaml.parse(text)` / `yaml.parse-all(text)` — parsen, Rückgabe `?yaml` (`err`-Zweig mit Position `line L, column C`)
- `yaml.valid-of(text)` / `yaml.error-of(text)` — nur Validierung, ohne Dokument aufzubauen
- `yaml.strip-bom(text)` — entfernt die UTF-8-BOM
- `doc-count()` / `doc(i)` — Zugriff auf mehrere Dokumente
- `kind()`、`is-null()`、`is-bool()`、`is-int()`、`is-float()`、`is-nan()`、`is-str()`、`is-seq()`、`is-map()`、`len()`、`text()`、`tag()`、`anchor()`、`line-of()` — Knoteninformationen
- `str()`、`i64()`、`f64()`、`bool()`、`scalar-text()` — Werte lesen, stets `?Wert`
- `at(i)`、`value(i)`、`key(i)`、`key-node(i)`、`get(key)`、`find(path)` — Kindknoten / Keys lesen
- `get-str` / `get-i64` / `get-f64` / `get-bool`、`find-str` / `find-i64` / `find-f64` / `find-bool` — bequeme Wertabfrage
- `to-json()`、`to-json-all()`、`to-yaml()`、`stringify()`、`source-of()` — Serialisierung

Konstanten für Knotentypen: `YAML-KIND-NULL` / `BOOL` / `INT` / `FLOAT` / `STR` / `SEQ` / `MAP` (0–6); Konstanten für Scalar-Stile `YAML-STYLE-PLAIN` / `SINGLE` / `DOUBLE` / `LITERAL` / `FOLDED` / `ALIAS` (0–5) sowie `YAML-STYLE-BLOCK` / `FLOW` (6–7).

> **Hinweis eins**: Kindknoten-Ansichten werden als `?yaml` zurückgegeben, die Hilfsfunktionen zum Lesen liefern `?Wert`. Entpacke sie per Match (`child = d.at(i)`, dessen `ok`-Arm den Kindknoten an `it` bindet, der Standardarm behandelt leere Werte, jeder Arm auf einer eigenen Zeile). Kindknoten teilen den Knotenpool mit dem Elterndokument; es empfiehlt sich, das Lesen im selben Match vorzunehmen oder sich zuvor eine Kopie zu binden und keine veralteten Handles über mehrere Pool-Änderungen hinweg zu halten. Zum Lesen werden durchgängig Pfad-Hilfsfunktionen wie `find-str` / `find-i64` empfohlen.
>
> **Hinweis zwei**: In verschachtelten Matches wird `it` **pro Ebene wiederhergestellt**. Die Arme eines inneren Matches binden `it` auf ihren eigenen Rumpf neu (der Arm muss den inneren Wert sehen), aber sobald der Arm endet, wird `it` wieder auf den Rumpf des äußeren Arms zurückgesetzt, daher ist die folgende Schreibweise sicher:
>
> ```no
> d ?yaml = yaml.parse(src)
> d: {
>     nil -> print('nil')
>
>     err -> print(it.msg())
>
>     -> {
>         print(it.find-str('a'))        ; 外層 it = 文件
>         v ?yaml = it.at(0)
>         v: {
>             nil -> print('none')
>             err -> print('err')
>             -> print(it.find-str('b')) ; 內層 it = 子節點
>         }
>         print(it.find-str('c'))        ; ✅ it 已還原成文件
>     }
> }
> ```
>
> Die einzige Ausnahme ist das äußerste Match: Nach seinem Ende behält `it` den Wert des letzten Arms (es wird nicht zurückgesetzt), daher sollte man sich außerhalb eines Matches nicht auf `it` verlassen. Wenn ein Wert über Ebenen hinweg gebraucht wird, kann er weiterhin explizit in einer benannten Lokalvariablen gespeichert werden (`doc = it`) oder destructuring-Binding `ok(v) -> ...` verwendet werden.
>
> **Hinweis drei**: `.nan` wird zu einem echten NaN geparst (`f64()` gibt NaN zurück, `x != x` ist wahr). JSON kennt kein NaN, daher gibt `to-json()` dafür `null` aus; `to-yaml()` gibt `.nan` aus.

---
