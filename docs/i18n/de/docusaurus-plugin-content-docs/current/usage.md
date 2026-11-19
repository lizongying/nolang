---
sidebar_position: 2
---

# Installation und Verwendung

## Installation

### CLI installieren

Laden Sie die ausführbare Datei für Ihre Plattform von [GitHub Releases](https://github.com/lizongying/nolang/releases/latest) herunter oder installieren Sie auf folgende Weise:

```bash
# macOS / Linux
# 1. 下載二進制文件
# 2. 放到 PATH 中
sudo mv nolang /usr/local/bin/no
```

### VS Code-Erweiterung installieren

Installieren Sie die Nolang-Erweiterung aus dem [VS Code Marketplace](https://marketplace.visualstudio.com/items?itemName=lizongying.vscode-nolang), die Syntaxhervorhebung, LSP-Diagnosen, Sprung zur Definition, automatische Vervollständigung und mehr bietet.

## CLI-Befehle

| Befehl                                                         | Beschreibung                    |
| ------------------------------------------------------------ | ----------------------- |
| `no version`                                                 | Gibt Versionsinformationen aus            |
| `no init`                                                    | Definiert den Workspace (erzeugt workspace.noml, ohne package.noml) |
| `no new <name>`                                              | Erstellt ein neues Paket im Workspace (Unterverzeichnis + package.noml, in workspace.noml registriert) |
| `no fmt [-w] [-d] <file\|dir>`                               | Formatiert Quellcode (inkl. Dedup/Sortierung der Importe, entfernt `# std/fmt` automatisch); `.noml`-Konfigurationsdateien folgen einer layout-erhaltenden Formatierungsregel (Einrückung/Leerzeichen/Leerzeilen, Kommentare und Schlüsselreihenfolge bleiben unangetastet) |
| `no build [-o <file>] [-cc <s>] [-target <s>] [-opt[=N]] [<file\|dir>]` | Baut das Projekt (Ausgabe: ausführbare Datei) |
| `no run [-cc <s>] [-target <s>] [<package\|dir\|file>]`        | Baut und führt aus (Paketname/Verzeichnis/Datei) |
| `no test [-cc <s>] [-target <s>] [<file>]`                   | Führt Tests aus                |
| `no add <tool>[:<version>]@<host>/<owner>/<repo>`            | Fügt eine Abhängigkeit hinzu (vorkompiliertes AST-Modul)|
| `no remove <pkg>`                                            | Entfernt eine Abhängigkeit                |
| `no update <pkg>`                                            | Aktualisiert eine Abhängigkeit                |
| `no update-all`                                              | Aktualisiert alle Abhängigkeiten            |
| `no list`                                                    | Listet Abhängigkeiten auf                |
| `no sync`                                                    | Synchronisiert Abhängigkeiten                |
| `no install [-u] [<tool>[:<version>]@<host>/<owner>/<repo>]`  | Installiert ein Binary             |
| `no uninstall <name>`                                        | Entfernt ein Binary             |
| `no pack [-o <file>] [<dir\|file.no>]`                       | Packt ein AST (`<tool>.ast.zip`)|
| `no pub [-o <file>]`                                         | Wie `no pack` (Upload zur Registry nicht implementiert)|
| `no cache <stat\|clear>`                                     | Zeigt / leert den AST-Cache (unter normalen Umständen ist ein Leeren nicht nötig)|

## Schnellstart

Nolang verwendet eine **Single-Repo-(Projekt-)-Multi-Package**-Architektur: Das Repository-Stammverzeichnis ist der Workspace (nur `workspace.noml`), jedes Paket ist ein Unterverzeichnis (mit eigenem `package.noml`). `no init` und `no new` sind zwei getrennte Vorgänge:

- `no init` — definiert den Workspace im aktuellen Verzeichnis (erzeugt nur `workspace.noml`).
- `no new <name>` — erstellt ein neues Paket im aktuellen Workspace (erzeugt das Unterverzeichnis `./<name>/` und dessen `package.noml`) und registriert das Paket in `workspace.noml`.

### Workspace initialisieren und Paket erstellen

```bash
# 1) 在倉庫根目錄定義工作區
no init

# 2) 在工作區內新建包（會自動註冊到 workspace.noml）
no new foo

# 3) 進入包目錄
cd foo

# 直接運行（自動構建並執行 main.no）
no run
```

Innerhalb des Workspaces können Sie `no new` wiederholt verwenden, um weitere Pakete hinzuzufügen:

```bash
no new bar
no new baz
```

Zu diesem Zeitpunkt sieht `workspace.noml` so aus:

```noml
{
  foo: '/foo'
  bar: '/bar'
  baz: '/baz'
}
```

> **Pfadformat**: Zuordnungswerte in `workspace.noml` verwenden das Präfix `/` für workspace-relative Pfade (z. B. `/foo` → `workspaceRoot/foo`). Relative Sprünge wie `./` und `../` sowie absolute Betriebssystempfade (z. B. `/home/user/code/fork`) werden ebenfalls unterstützt und erlauben das Verweisen auf Verzeichnisse außerhalb des Workspaces.

Beim Ausführen von `no build` ohne Argumente im Stammverzeichnis werden alle Pakete im Workspace parallel gebaut.

### Nur Workspace initialisieren (ohne Paket zu erstellen)

```bash
# 在當前目錄定義工作區
no init
```

`no init` erzeugt nur `workspace.noml` (anfangs leer `{}`), **es werden weder `package.noml` noch `main.no` erzeugt**. Falls `workspace.noml` bereits existiert, wird es nicht überschrieben. Pakete müssen mit `no new <name>` erstellt werden.

### Bauen und Ausführen

```bash
# 構建（默認尋找 main.no）
no build                    # 構建當前目錄
no build main.no            # 構建指定文件
no build -o output          # 指定輸出路徑
no build -cc zig            # 使用 Zig 編譯器
no build -target x86_64-linux-gnu  # 交叉編譯（指定目標平台）

# 運行（構建 + 執行）
no run                      # 構建並執行當前目錄的 main.no
no run foo                  # 運行工作區中名為 foo 的包（解析 workspace.noml）
no run ./foo                # 運行 ./foo 目錄的 main.no
no run main.no              # 運行指定 .no 文件
no run -cc zig
no run -target aarch64-macos-gnu
```

> `no build -v` schreibt zusätzlich das rohe LLVM IR neben den Ausgabepfad (`<Ausgabepfad>.ll`). `no build --shared -o libdemo.dylib main.no` exportiert mit `#{cabi='symbol'}` markierte Top-Level-Funktionen als stabile C-ABI-Symbole, linkt sie mit `-shared -fPIC` zu einer dynamischen Bibliothek und erzeugt automatisch eine gleichnamige `.h`-Headerdatei; mit `--header` kann der Headerpfad überschrieben werden. Derzeit gibt es keine `-c`-Option (nur kompilieren, nicht linken). Das vollständige Vorgehen, um Nolang aus C / C++ / Rust / Go / Java / Python / C# aufzurufen, finden Sie unter [Nolang aus Host-Sprachen aufrufen](./lang/host-interop).

Positionsargumente von `no run` werden in folgender Reihenfolge aufgelöst:

1. Existierende `.no`-Datei → diese Datei direkt ausführen
2. Existierendes Verzeichnis → dessen `main.no` ausführen
3. In einem Workspace (nächstgelegenes `workspace.noml`) registrierter Paketname → `main.no` dieses Pakets ausführen

Ohne Argumente wird standardmäßig `main.no` des aktuellen Verzeichnisses ausgeführt. Ist das aktuelle Verzeichnis das Workspace-Stammverzeichnis (mit `workspace.noml`, aber ohne `main.no`), werden Sie aufgefordert, mit `no run <package>` ein Paket anzugeben.

### Cross-Compilation-Ziele

Das Argument `-target` hat das Format `<arch>-<os>-<abi>` und unterstützt folgende Ziele:

| Ziel-Triple | Beschreibung |
| -------------------- | -------------- |
| `x86_64-linux-gnu`   | Linux x86_64   |
| `aarch64-linux-gnu`  | Linux ARM64    |
| `x86_64-macos-gnu`   | macOS x86_64   |
| `aarch64-macos-gnu`  | macOS ARM64    |
| `x86_64-windows-gnu` | Windows x86_64 |

**Automatisches Erkennen der aktuellen Plattform**: `no build`, `no run` und `no test` erkennen ohne Angabe von `-target` automatisch die aktuelle Host-Plattform und kompilieren nativen Code. Im Alltag muss kein Target manuell angegeben werden:

```bash
no run hello.no          # 本機直接跑
no test                  # 本機跑測試
no build -target aarch64-linux-gnu   # 需要交叉編譯時才顯式指定
```

### Compiler-Auswahl

Das Argument `-cc` legt das C-Compiler-Backend fest:

- `clang` (Standard) — erfordert eine installierte LLVM
- `zig` — erfordert eine installierte Zig-Toolchain, gut geeignet für Cross-Compilation

### Layout integrierter Typen (`?T`-Inline-Schwellenwert)

Die Speicherlayouts von `?T` **richten sich nach dem Payload-Typ**: Für jeden Payload-Typ `T` gibt es ein eigenes
`%option_<key> = { i8 tag, [K x i64] }` (1-Byte-Tag, Payload fest bei Byte-Offset 8,
`K = roundUp8(sizeof(T))/8`). Passt die Payload nicht in den Schwellenwert, wird sie auf dem Heap geboxt und das erste Wort des Payload-Bereichs speichert einen Zeiger.

`--option-inline-threshold=N` legt diesen Schwellenwert fest (gezählt in Bytes der **gesamten Option-Struktur**,
also `1 + 7 + sizeof(T)`):

```bash
no build --option-inline-threshold=1032 main.no   # 讓更大的載荷內聯
no build --option-inline-threshold=16 main.no     # 最小可配置值
```

| Wert | Wirkung |
| --- | --- |
| Standard **264** (= 1 + 7 + 256) | `?i64` 16 B, `?str` 24 B, `?txt` (256 B) 264 B inline; größere Payloads werden geboxt |
| Minimum **16** | Nur Typen wie `?i64` bleiben inline; ab `?str` (24 B) wird alles geboxt |
| < 16 | Kompilierungsfehler |

Priorität: Kommandozeile > Umgebungsvariable `NOLANG_OPTION_INLINE_THRESHOLD` > `compiler.option-inline-threshold` in
`package.noml` > Standardwert. Die einzige Quelle des Layouts ist `src/mir/layout.go`,
auf semantischer Ebene (welche Werte als `ok`／`nil`／`err` gelten) **ändert sich nichts**; nur der Speicherverbrauch und die Box-Entscheidung können variieren.

### Integrierter Optimierer des Compilers (MIR opt)

Der Compiler enthält einen auf der **MIR-Ebene** arbeitenden Optimierer (nach der HIR-Absenkung, vor der LLVM-Codegenerierung). Dieser ist etwas anderes als das bereits vorhandene LLVM-`opt` im Backend: Ersteres schreibt die MIR um, Letzteres die bereits erzeugte LLVM IR.

Mit `-opt[=N]` aktivieren, **standardmäßig deaktiviert**:

```bash
no build main.no          # 預設：不啟用（與歷史行為一致）
no build -opt main.no     # 等同 -opt=1
no build -opt=2 main.no   # 常數折疊 + 死碼消除 + 控制流清理
no build -opt=0 main.no   # 顯式關閉
```

| Stufe | Bedeutung |
| ---- | ---- |
| `0` / `off` / `false` / `no` / `none` | Aus (Standard) |
| `1` / `on` / `true` / `yes` / `-opt` | Konstantenfaltung + Integer-Identitätsumschreibungen + Copy-Propagation + Eliminierung toter Skalare |
| `2` | Stufe 1 + Control-Flow-Bereinigung (Faltung konstanter Verzweigungen, Entfernung unreachable Blöcke, Verkettung leerer Block-Sprünge, Zusammenführung von Blöcken mit einem Vorgänger) + Prelude-Pruning |

`-opt` ist ein **globaler** Schalter (wird wie `-v` in `main` verarbeitet), daher wird er von `build`, `run` und `test` unterstützt.

Derselbe Schalter kann auch über die Umgebungsvariable `NOLANG_MIR_OPT` angegeben werden; das Kommandozeilen-Flag schreibt lediglich in diese Variable, sodass beide nicht auseinanderlaufen können. So sehen Sie, was die Optimierung getan hat:

```bash
NOLANG_MIR_OPT=2 NOLANG_MIR_OPT_STATS=1 no build -o out main.no
# [miropt] level=2 funcs=3 folded=5 (arith=2 cmp=3) ident=1 copies=4 dead=6 cfg{condbr=3 unreachable=4(5 insts) threaded=1 merged=5}
# [miropt] prelude: dropped 27 unreferenced helpers
```

| Feld | Bedeutung |
| ---- | ---- |
| `folded` (`arith`/`cmp`) | Anzahl der zu Konstanten gefalteten Arithmetik-/Vergleichsanweisungen |
| `ident` | Integer-Identitäten, die auf einen Operanden reduziert wurden (`x+0`, `x*1`, `x&x`, `x^0`…) |
| `copies` | Durch Copy-Propagation entfernte Skalar-Kopien |
| `dead` | Entfernte tote Skalar-Anweisungen |
| `cfg{condbr, unreachable, threaded, merged}` | Faltung konstanter Verzweigungen, Entfernung unreachable Blöcke (inkl. Anzahl Anweisungen), Verkettung leerer Block-Sprünge, Zusammenführung von Blöcken mit einem Vorgänger |
| `prelude: dropped N` | Anzahl der Runtime-Helper im Codegen-Prelude, die nachweislich nicht referenziert werden (siehe unten) |

### Prelude-Pruning (Stufe 2)

`emitPrelude` schreibt die gesamte Runtime (`str_*`, `print_*`, `eprint_*`, `vec_*`, `nolang_*`) in **jedes** Modul, unabhängig davon, ob das Programm sie verwendet — ein `print('hi')`-Programm erzeugt 60 Funktionsdefinitionen, von denen nur 8 vom Einstieg erreichbar sind und **52 niemals aufgerufen werden**.

**Das kann `opt -O2` nicht**: LLVMs GlobalDCE entfernt nur `internal`-Definitionen, das Prelude wird jedoch mit externer Linkage emittiert (`no`-Module können mit C gelinkt werden, Helfer, die der Linker eventuell benötigt, dürfen nicht internal sein). In der Praxis sind bei diesem Programm genau 7 der 60 Definitionen `define internal`, `opt -O2` entfernt genau diese 7, und die `.ll` wächst sogar von 61,812 auf 69,874 Bytes (+8,062).

Daher führt Stufe 2 nach Abschluss der IR-Generierung ein Reachability-Pruning auf dem **fertigen Text** durch und behält nur die tatsächlich referenzierten Helper (inklusive rekursiv derer, die sie selbst aufrufen). Dasselbe `print('hi')`: Anzahl Definitionen 60 → **16**, `.ll` 61,812 → **14,548** Bytes (**−76.5%**), ausführbare Datei 36,616 → 34,648 (−5.4%). Beim Pruning zählen nur echte Referenzen — **in Kommentaren auftauchende Namen zählen nicht**, das ist der Schlüssel dafür, dass es 44 statt 33 entfernen kann; und wenn eine Funktion entfernt wird, **wird auch ihr eigener beschreibender Kommentar entfernt** (`optTrailingComment`), sonst blieben 12,609 Bytes übrig, die 44 längst nicht mehr existierende Funktionen beschreiben.

Das Entfernen von Kommentaren **verkleinert die ausführbare Datei nicht** — Kommentare werden beim Parsen durch `opt`/`llc` ohnehin verworfen, die ausführbare Datei ist in beiden Fällen 34,648 Bytes. Die 12,609 Bytes sind der Gewinn bei IR-Text und Compile-Time-Parsing-Aufwand.

Die Heuristik des Prunings ist stets auf Erhalt ausgelegt — Blöcke, aus denen kein eindeutiger Name abgeleitet werden kann, werden behalten; fehlt einem Block die schließende Klammer, wird das Pruning ab dort gestoppt; `declare`／Globals／Typdefinitionen werden nie entfernt. Eine verpasste Optimierung kostet nur ein paar Bytes, ein falsch entferntes Symbol führt zu einem Linkerfehler.

**Kann das Prelude ganz entfernt werden? Nein.** Im Prelude-Bereich stehen nicht nur Funktionen, sondern auch **die String-Literale des Programms selbst** (`@.mir.str.N`, also `"hi"` selbst), Typen wie `%str-long`／`%option`／`%task` sowie externe Deklarationen wie `@malloc`／`@write`／`@llvm.memcpy` — diese Daten machen etwa 30 % des Moduls aus, und ohne sie scheitert selbst `llc` (`error: use of undefined value '@.mir.str.1'`). Entfernen lässt sich nur die **Funktions**-Hälfte: Ersetzt man die 48 `define` durch gleichnamige `declare`, meldet der Linker genau die vier undefinierten Symbole `print_nl`, `print_str`, `str_free` und `str_from_const`. Die richtige Frage lautet also „Kann man es bedarfsgerecht emittieren?", nicht „Kann man es entfernen?".

Dieser Overhead ist eine **fixe, von der Programmgröße unabhängige Abgabe**: Code und Kommentare des Preludes sind in jedem Modul identisch (48 Definitionen, Code 36,043 + Kommentare 17,516 Bytes). Bei einer Stichprobe von 40 Dateien aus dem Korpus betrug die Beibehaltung **0–19, Median 9**. `tests/dup.no`／`tests/q.no`／`tests/u128-types.no` behalten **0** — das gesamte Prelude ist für sie tot. Die Ersparnis hängt daher vollständig von der Modulgröße ab: Einzeiler −76.5%, die 3.0 MB große Datei `test/std/pbkdf2.no` nur −4.1%.

Kann das Prelude ganz entfernt werden? Nein — im Bereich liegen weiterhin die String-Literale des Programms (`@.mir.str.N`), Typen und externe Deklarationen (1,151 Bytes); ohne sie scheitert selbst `llc`. Und `extraFuncs`／`extraFuncsBody` in `codegen.go:75-83` ist ohnehin bereits ein bedarfsgerechter Emittent (die Helper für option／struct／enum／ptr laufen alle darüber); das Prelude ist der einzige noch nicht umgestellte Teil. Würde man es umstellen, wäre der zusätzliche Gewinn fast ausschließlich Kommentare — auf Code-Ebene ist nichts mehr zu holen.

Der Optimierer faltet nur **Skalare** (`i64`/`u64`/`i16`/`i32`/`char`/`bool`/`f32`/`f64`). `byte`/`u8`/`i8`, `i128`/`u128`, `option`, Slices, Strukturen und dergleichen werden übersprungen: Erstere haben in `emitConst` keinen passenden Zweig (sie degenerieren zu 0), bei Letzteren läuft die Arithmetik über den `option`-Overflow-Wrapping-Pfad, und Faltung würde die Semantik verändern. Gleitkommawerte sind ebenfalls von allen Identitäten ausgeschlossen — `x * 0.0` ist für `inf`/`NaN` nicht 0, `x + 0.0` für `-0.0` nicht `x`.

Die Kernaussage dieses Vergleichs ist es wert, hier vorab genannt zu werden: **Lokale Bereinigung (Konstantenfaltung, Eliminierung toter Codes, Control-Flow-Bereinigung) hat nach einem Lauf von `opt -O2` praktisch keinen Grenznutzen mehr** (−0.06%), weil LLVM dieselbe Arbeit gründlicher erledigt. Was sich wirklich auszahlt, sind die Dinge, die LLVM **wegen der Linkage nicht tun kann** — namely das oben beschriebene Prelude-Pruning. Mit Prelude-Pruning (inklusive kommentar-aware Pruning) beträgt der Gesamt-Grenznutzen von `mir+llvm_O2` → `mir_opt+llvm_O2` **−19.05% Anweisungen / −15.83% Text**; die Textreduktion von `mir` → `mir+mir_opt` steigt von −14.29% auf **−17.75%** (631 Dateien, gesamter Korpus).

## JS-Backend

Das JS-Backend (`no build --js` / `no run --js`) kompiliert `.no`-Quellcode zu JavaScript (Type Erasure),
unterstützt sowohl Node.js als auch Browser als Ziele, die Plattform-Annotationen `#{js}` / `#{js-browser}` sowie
ESM-Imports von Drittanbietern (`# npm/<pkg>`).

Die vollständige Dokumentation zum JS-Backend (Kommandozeilenparameter, Abbildung integrierter Funktionen,
`js/*`-Standardbibliothek, Browser-Import-Map usw.) finden Sie in der eigenen Dokumentation [JS-Backend](./lang/js-backend); auf dieser Seite wird sie nicht wiederholt.

## Einstiegspunkt-Regeln

- **main.no** — Programmeinstiegspunkt
- **lib.no** — Bibliothekseinstiegspunkt, exportiert Funktionen (siehe [Export-Dokumentation](lang/export))
- **Alle .no-Dateien im Verzeichnis tests/** — enthalten Testbehauptungen (Assertions)

## Tests

```bash
# 測試tests目錄下所有 .no 文件
no test

# 執行單個測試文件
no test my-test.no

# 使用指定編譯器或目標
no test -cc zig
no test -target x86_64-windows-gnu
```

Hinweise zu Tests:

- Testdateien werden zentral im Verzeichnis tests/ abgelegt
- Jede Testdatei wird unabhängig gebaut
- Schlägt ein beliebiger Test fehl, wird ein von null verschiedener Exit-Code zurückgegeben

## AST-Cache

`no build` / `no run` / `no test` schreiben den aus jeder Benutzerquelldatei geparsten AST auf die Festplatte
(standardmäßig `~/.cache/nolang/ast`, der Pfad lässt sich über `$NOLANG_AST_CACHE_DIR` ändern). Bei unverändertem Inhalt wird er beim nächsten Mal direkt wiederverwendet,
wodurch Lexing und Parsing entfallen. Die Standardbibliothek verwendet ein im Compiler eingebettetes Blob und belegt diesen Cache nicht.

**Ein manuelles Leeren ist nicht nötig.** Der Cache-Schlüssel lautet:

```
sha256( ASTVersion ‖ 解析期 salt ‖ 源文件內容 )
```

Einer der Bestandteile des Salts ist `checker.StdSigKey` — also die Inhaltssignatur aller in `src/hash.noml` aufgeführten
Quelldateien (Standardbibliothek + Compiler selbst). Jede Änderung am Compiler oder an der Standardbibliothek führt daher
zu einem neuen Schlüssel für das mit `make` neu gebaute `no`; alte Einträge werden ganz natürlich nicht mehr getroffen und nicht wiederverwendet.

| Befehl | Beschreibung |
| --- | --- |
| `no cache stat` | Zeigt Cache-Verzeichnis, Anzahl Einträge, Gesamtgröße und aktuelle `ASTVersion` |
| `no cache clear` | Leert alle Einträge (nur im Notfall: bei Verdacht auf veraltete Einträge) |

Umgebungsvariablen:

| Variable | Wirkung |
| --- | --- |
| `NOLANG_AST_CACHE_OFF=1` | Deaktiviert den Cache (nützlich für A/B-Vergleiche von Compilern und zur Fehlersuche, wenn „bereits behobene Fehler wieder auftauchen") |
| `NOLANG_AST_CACHE_DIR` | Eigener Cache-Pfad. Wirkt **gleichzeitig** auf den `.ast.zip`-Cache vorkompilierter AST-Abhängigkeiten (dieser liegt standardmäßig unter `~/no/cache/ast`, siehe unten „Vorkompilierte AST-Abhängigkeiten") |
| `NOLANG_AST_CACHE_MAX` | Obergrenze für die Anzahl der Cache-Einträge (Standard 4096) |

> Alte Einträge werden einfach nicht mehr getroffen und beim Schreiben neuer Einträge gemäß der Obergrenze ausgeschieden, daher **muss** `~/.cache/nolang/ast` **nicht** gelöscht werden.
> Beachten Sie, dass `no` mit `make gen` / `make no` neu gebaut werden muss: Ein mit `cd src && go build` selbst kompiliertes Binary
> enthält kein injiziertes `StdSigKey` und bricht mit einer Fehlermeldung ab, statt den Cache stillschweigend wiederzuverwenden.

## Binaries installieren und deinstallieren

### Installation

```bash
# 安裝當前目錄的包（本地構建）
no install
no install -u                                   # 強制重構（更新）

# 從 GitHub Release 安裝預編 binary（推薦）：<tool>[:<version>]@<host>/<owner>/<repo>
no install nogit@github.com/lizongying/notools            # 最新版
no install nogit:v0.1.33@github.com/lizongying/notools    # 指定版本
no install nogit@lizongying/notools                       # 省略 host，默認 github.com
no install -u nogit@github.com/lizongying/notools         # 強制重新下載
```

Bei der Remote-Installation wird ein **vorkompiliertes Binary** heruntergeladen, kein Quellcode: Der Asset-Name lautet
`<tool>-<os>-<arch>` (unter Windows mit `.exe`), wobei `<os>-<arch>` der Plattform entspricht, auf der `no` selbst läuft
(z. B. `darwin-arm64`, `linux-amd64`). Daher ist keine lokale LLVM-Toolchain nötig und das installierte Tool muss nicht in
Nolang geschrieben sein. Wird die Version weggelassen, wird `releases/latest` abgefragt; das Setzen von `$GITHUB_TOKEN`
(oder `$GH_TOKEN`) verhindert die GitHub-API-Ratenbegrenzung.

Installationsablauf:

1. Remote: Spec auflösen → Release-Asset lokalisieren → herunterladen; lokal: Build aus dem aktuellen Verzeichnis verwenden
2. Das Binary nach `~/no/bin/` schreiben
3. Einen Symlink in `/usr/local/bin/` erstellen

### Deinstallation

```bash
no uninstall pkg-name
```

Die Deinstallation entfernt den Symlink in `/usr/local/bin/` und das Binary in `~/no/bin/`.

## Projektkonfiguration

### noml-Konfigurationsformat

Alle Konfigurationsdateien von Nolang (`package.noml`, `workspace.noml`, `.workspace.noml`, `nolang.lock.noml`, `sum.noml`) verwenden das eigene Format **noml** (Nolang Markup Language), das das frühere JSONC ersetzt. Die Syntaxregeln orientieren sich an der Nolang-Sprache selbst:

| Regel | Beschreibung |
| --- | --- |
| Top-Level-Objekt | Ist ein Objekt: `{` und `}` stehen jeweils auf einer eigenen Zeile, ein Member pro Zeile, **keine Komma-Trennung** (der Zeilenumbruch trennt) |
| Schlüssel | Standardmäßig nackte Bezeichner, mit denselben Regeln wie Nolang-Variablennamen: Kleinbuchstaben/Ziffern/Minus (in der Mitte)/Unterstrich am Anfang, nicht mit einer Ziffer beginnend (z. B. `link-libs`, `anonymous-fn-type`, `dev-dependencies`); Schlüssel, die diesen Regeln nicht genügen (Pfade, URLs, `@scope/pkg`), werden als einfachquotierte String-Schlüssel geschrieben |
| String-Werte | Einfache Anführungszeichen, Escaping mit `\`: `\' \\ \n \t` |
| Boolean | `true` / `false` (alles Kleinbuchstaben, wie Nolang-bool-Literale) |
| Zahlen | Nackte Literale (für Lock-Dateien und dergleichen reserviert) |
| Arrays | `[`, Elemente, `,` und `]` dürfen sich über mehrere Zeilen erstrecken; Elemente werden mit **Kommas** getrennt, **Trailing-Kommas werden unterstützt**; leere Arrays schreibt man `[]` |
| Kommentare | `;` bis zum Zeilenende (außerhalb von Strings) |
| Einrückung | Ohne Bedeutung (nur für die Lesbarkeit, keine Formatierungsgarantie) |

`dependencies` / `dev-dependencies` sind **String-Arrays**, deren Elemente die Form `'Schlüssel:Version'` haben (die Version kann entfallen, Standard ist `*`), zum Beispiel:

```noml
{
  dependencies: [
    '/example/test2',                          ; 本地包，默認任意版本
    'github.com/lizongying/nolang/test1:v0.1.0', ; 線上包，指定版本,
  ]
}
```

Die Datei `package.noml` im Projektstammverzeichnis beschreibt die Projektinformationen:

```noml
{
  name: 'my-project'
  version: 'v0.1.0'
  description: 'A new Nolang project'
  keywords: []
  author: ''
  email: ''
  organization: ''
  repository: ''
  homepage: ''
  license: 'MIT'
  mirrors: []
  dependencies: [
    'fmt',,
  ]
  compiler: {
    version: 'v0.1.0'
  }
  output: './dist'
  ignore: []
}
```

`compiler.lang` legt die Nachrichtensprache dieses Projekts fest (`'zh-tw'`, `'ja'` usw., Notation wie bei `--lang`): Sie hat
Vorrang vor der System-Locale, jedoch niedrigeren Rang als `--lang` und `NO_LANG`; ohne Angabe folgt sie dem System.

### Abhängigkeitsverwaltung

```bash
# 添加依賴：<tool>[:<version>]@<host>/<owner>/<repo>
# 版本號可省略（取最新 release）；寫入 package.noml 的鍵是 <tool>@<host>/<owner>/<repo>
no add nogit@github.com/lizongying/notools
no add nogit:v0.1.33@github.com/lizongying/notools

# 移除依賴
no remove pkg-name

# 更新依賴
no update pkg-name

# 更新所有依賴
no update-all

# 列出依賴
no list

# 同步依賴（下載並生成鎖文件）
no sync
```

### Vorkompilierte AST-Abhängigkeiten

`no add <tool>@<host>/<owner>/<repo>`-Abhängigkeiten dieser Art **enthalten nur AST, keinen Quellcode**: Zur Kompilierzeit
wird `<tool>.ast.zip` (MetaMessage + zip) aus dem Release geholt, dekodiert und direkt in die Compiler-Pipeline injiziert. Verwendung:

```nolang
# nogit; 或 use <host>/<owner>/<repo>/nogit
```

Ein zip entspricht genau einem Modul; tiefere Pfade wie `use <repo>/<submod>` werden nicht unterstützt. Die Version muss ein
konkreter Wert sein (`no sync` / der Compiler meldet bei `""` oder `"*"` einen Fehler). Das zip wird unter `~/no/cache/ast/`
zwischengespeichert und kann über `$NOLANG_AST_CACHE_DIR` überschrieben werden.

Um das Asset zu erzeugen, verwenden Sie `no pack` (nur Lexer + Parser nötig, keine LLVM-Toolchain — gut geeignet für CI):

```bash
no pack -o nogit.ast.zip nogit.no      # 單個源文件
no pack -o nogit.ast.zip ./nogit       # 一個包目錄
```

### Abhängigkeitstypen und Versionsregeln

Abhängigkeiten in `package.noml` werden in **lokale Pakete** und **Online-Pakete** unterteilt. Der Compiler bestimmt den Abhängigkeitstyp automatisch anhand der folgenden Regeln und gibt eine Warnung aus, wenn ein lokales Paket nicht die Version `"*"` verwendet.

#### Entscheidungsregeln

```
依賴鍵 → 查找 workspace.noml（短名稱或完整鍵匹配）
  ├─ 找到 → 本地包（應使用 "*"）
  └─ 未找到 → 檢查路徑前綴（/ 開頭）
      ├─ 是 → 本地包（應使用 "*"）
      └─ 否 → 線上包（使用版本號，不警告）
```

1. **Suche in `workspace.noml`**: Der Abhängigkeitsschlüssel wird zunächst als Kurzname oder vollständiger Schlüssel in `workspace.noml` gesucht. Ein Treffer bedeutet ein lokales Paket.
2. **Pfadpräfix prüfen**: Wird nichts in `workspace.noml` gefunden, ist der Abhängigkeitsschlüssel auch dann ein lokales Paket, wenn er mit `/` (workspace-relativer Pfad) beginnt.
3. **Alle übrigen sind Online-Pakete**: Abhängigkeitsschlüssel, die keine der obigen Bedingungen erfüllen, gelten als Online-Pakete und sollten eine Version angeben.

> **Workspace-Grenzregel**: Lokale Pfade in `package.noml` müssen mit `/` beginnen (workspace-relativ); `./`, `../` und absolute Betriebssystempfade sind verboten. Der einzige erlaubte Weg, die Workspace-Grenze zu durchbrechen, sind Zuordnungen in `workspace.noml` / `.workspace.noml`. Siehe [Workspace-Grenzregeln](#工作區邊界約束).

#### Verschiedene Formen der Referenzierung lokaler Pakete

Lokale Pakete können auf drei Arten referenziert werden; die Version sollte jeweils `"*"` sein:

```noml
dependencies: [
  ; 1. 短名稱：workspace.noml 中註冊的鍵
  'test2',
  ; 2. 工作區相對路徑：以 / 開頭，相對工作區根目錄
  '/example/test2',
  ; 3. 完整 URL：若 workspace.noml 中有對應映射則為本地包
  'github.com/lizongying/nolang/test2',,
]
```

> **Hinweis**:
> - Ob die dritte Form ein lokales Paket ist, hängt davon ab, ob es in `workspace.noml` eine entsprechende Zuordnung gibt. Wird `github.com/lizongying/nolang/test2` in `workspace.noml` einem lokalen Pfad zugeordnet, ist die Abhängigkeit ein lokales Paket (Version `"*"` verwenden); andernfalls ist sie ein Online-Paket (Version angeben).
> - Die Präfixe `./` und `../` sind in `package.noml` nicht mehr erlaubt. Alle lokalen Pfade müssen mit `/` beginnen (workspace-relativ).

#### Fortgeschritten: Online-Paket in lokales Paket umwandeln

Mit `workspace.noml` lassen sich nicht nur Kurznamen registrieren, sondern auch Online-Paketnamen auf lokale Pfade abbilden — ein Entwicklungsmodus, der „Online-Paket wird lokales Paket" ermöglicht:

```noml
; workspace.noml
{
  test2: '/example/test2'
  'github.com/lizongying/nolang/test2': '/example/test2'
}
```

Eine in `package.noml` als `github.com/lizongying/nolang/test2` referenzierte Abhängigkeit wird dann als lokales Paket `/example/test2` aufgelöst und sollte die Version `"*"` verwenden. Das ist bei der lokalen Entwicklung von Online-Paketen sehr praktisch — es genügt eine Zuordnung in `workspace.noml`, um die Remote-Abhängigkeit auf lokalen Quellcode für gemeinsames Debuggen umzustellen.

#### Warnung zu Versionsnummern

Erkennt der Compiler bei der Kompilierung, dass ein lokales Paket eine andere Version als `"*"` verwendet (z. B. `"v0.1.0"`), gibt er eine Warnung aus, die auf `"*"` hinweist. Für Online-Pakete gilt diese Einschränkung nicht.

#### Rekursive Workspace-Zuordnungen (paketübergreifende Zuordnungsketten)

Nolang unterstützt **rekursive Workspace-Zuordnungen**: Ein abhängiges Paket kann ein eigenes `workspace.noml` mitbringen, das weitere Zuordnungsregeln enthält, wodurch eine natürliche paketübergreifende Auflösungskette entsteht. Das ist Nolangs zentrales Unterscheidungsmerkmal — die `replace`/`patch`-Mechanismen gängiger Go/Cargo-Workflows wirken nur im aktuellen Projekt und werden nicht in abhängige Pakete hineingetragen.

**Anwendungsfälle:**
- Einheitliche interne Aliase in Basisbibliotheken
- Standardisierte Importpfade
- Kompatibilitätsmigrationen über mehrere Bibliotheksebenen

**Funktionsweise:**

Bei der Auflösung eines Abhängigkeitsschlüssels geht der Compiler wie folgt vor:
1. Den Schlüssel im aktuellen `workspace.noml` suchen
2. Wird eine Zuordnung gefunden, prüfen, ob das Zielverzeichnis ein eigenes `workspace.noml` mitbringt
3. Falls ja, die Suche auf Basis desselben Schlüssels fortsetzen und Ebene für Ebene auflösen
4. Bis keine tiefere Zuordnung mehr vorhanden ist

**Beispiel:**

```
workspace/                  ← 頂層工作區
├── workspace.noml         ← {"testkey": "/pkgA"}
├── pkgA/
│   ├── workspace.noml     ← {"testkey": "/pkgB"}
│   └── pkgB/
│       ├── workspace.noml ← {"testkey": "/pkgC"}
│       └── pkgC/           ← 最終目標（無 workspace.noml）
```

Bei der Auflösung von `testkey`:
1. Zuordnung im Top-Level-`workspace.noml` → `/pkgA` (workspace-relativ)
2. Zuordnung in `pkgA/workspace.noml` → `/pkgB` (relativ zur pkgA-Workspace)
3. Zuordnung in `pkgB/workspace.noml` → `/pkgC` (relativ zur pkgB-Workspace)
4. `pkgC` hat kein `workspace.noml` → abschließende Auflösung zu `pkgC`

**Zyklenerkennung:**

Der Compiler führt einen Auflösungs-Visited-Stack, in dem bereits besuchte Workspace-Stammverzeichnisse nachgehalten werden. Sobald eine zyklische Zuordnung erkannt wird (z. B. A→B, B→A), bricht er mit einer Fehlermeldung ab und gibt die vollständige Kette aus:

```
Error: circular workspace mapping detected: /path/to/A → /path/to/B → /path/to/A
```

> **Hinweis**: Diese Fähigkeit unterstützt nur die natürliche rekursive Zuordnung über Pakete hinweg (das abhängige Paket bringt selbst ein `workspace.noml` mit); eine manuelle Kurzschreibweise mit verketteten Einträgen in einer einzelnen Datei wird nicht unterstützt.

### Mirror-Konfiguration

In der `mirrors`-Array von `package.noml` lassen sich Mirror-Adressen konfigurieren, um Remote-Paket-Downloads zu beschleunigen:

```noml
mirrors: [
  'https://mirror.example.com/',,
]
```

### Workspace-Konfiguration: workspace.noml

`workspace.noml` liegt im Repository-Stammverzeichnis und beschreibt einen **Single-Repo-Multi-Package**-Workspace als Zuordnung „Paketname → relativer Pfad". `no build` kompiliert ohne Argumente und bei vorhandenem `workspace.noml` alle darin aufgeführten Pakete parallel; `no run` führt ein einzelnes Paket des Workspaces über seinen Paketnamen aus (z. B. `no run foo`).

```noml
{
  foo: '/foo'
  bar: '/bar'
}
```

`no init` definiert den Workspace: Existiert `workspace.noml` nicht, wird ein leeres Objekt `{}` erzeugt, **es werden keine `package.noml` erzeugt**; existiert es bereits, bleibt es unverändert und wird nicht überschrieben. Erstellt man anschließend mit `no new <name>` ein Paket, wird `"<name>": "./<name>"` automatisch in `workspace.noml` registriert.

#### Private lokale Konfiguration: .workspace.noml

Zusätzlich zum öffentlichen `workspace.noml` unterstützt Nolang eine private lokale Konfigurationsdatei `.workspace.noml` (mit führendem `.`). Diese sollte in die `.gitignore` aufgenommen werden und dient dem Entwickler für temporäre, persönliche Ersetzungen von Abhängigkeiten.

**Ladelogik:**

1. Zuerst die öffentliche Konfiguration `workspace.noml` laden
2. Danach die private Konfiguration `.workspace.noml` laden
3. **Gleicher Schlüssel**: Die private Konfiguration überschreibt die öffentliche
4. **Neuer Schlüssel**: Die Einträge werden addiert (vereinigt)

```
workspace/
├── workspace.noml       ← 工程共享配置（納入版本管理）
├── .workspace.noml      ← 本機私有配置（建議加入 .gitignore）
├── foo/
└── bar/
```

**Anwendungsfälle:**

- Einheitliche Team-Aliase und Regeln zur Abhängigkeitsweiterleitung stehen in `workspace.noml` und werden versioniert
- muss ein Entwickler beim lokalen Debuggen eine Abhängigkeit vorübergehend auf einen lokalen Fork umlenken, genügt eine Überschreibung in `.workspace.noml`
- Verhindert, dass temporäre Fork-Zuordnungen die öffentliche Projektkonfiguration verschmutzen, und vereint große Team-Zusammenarbeit mit der Flexibilität lokaler Entwicklung

**Beispiel:**

```noml
; workspace.noml（公共，版本管理）
{
  foo: '/foo'
  bar: '/bar'
}

; .workspace.noml（私有，本地調試）
{
  ; 覆蓋公共配置：foo 指向本地 fork（工作區內）
  foo: '/my-fork-foo'
  ; 新增私有映射：指向工作區外部的 fork（OS 絕對路徑，唯一逃逸出口）
  'github.com/lizongying/nolang/core': '/home/lzy/code/fork/core'
  ; 新增私有映射（工作區內）
  experimental: '/experimental'
}
```

Die letztlich wirksamen Zuordnungen: `foo` → `/my-fork-foo` (private Überschreibung), `bar` → `/bar` (aus der öffentlichen Konfiguration beibehalten), `github.com/lizongying/nolang/core` → `/home/lzy/code/fork/core` (privat hinzugefügt, verlässt den Workspace), `experimental` → `/experimental` (privat hinzugefügt).

### Workspace-Grenzregeln

Nolang wendet eine strenge Kontrolle der Workspace-Grenzen für Pfadreferenzen an, um Portabilität und Sicherheit des Projekts zu gewährleisten.

#### Übersicht der Regeln

| Konfigurationsebene | `/` workspace-relativ | Absoluter OS-Pfad | `./` `../` relativer Sprung |
| --- | --- | --- | --- |
| Abhängigkeitsschlüssel in `package.noml` | ✅ erlaubt | ❌ verboten | ❌ verboten |
| Zuordnungswert in `workspace.noml` | ✅ erlaubt | ✅ erlaubt (Fluchtweg) | ✅ erlaubt (Fluchtweg) |
| Zuordnungswert in `.workspace.noml` | ✅ erlaubt | ✅ erlaubt (Fluchtweg) | ✅ erlaubt (Fluchtweg) |
| `#`-Importpfad im Quellcode | ✅ erlaubt | ❌ verboten | ❌ verboten |

#### 1. package.noml (streng eingeschränkt, keine Flucht aus dem Workspace)

`package.noml` ist die Standard-Basiskonfiguration für die Veröffentlichung nach außen, CI und die Zusammenarbeit mehrerer Personen und wird ins Coderepository eingecheckt.

```noml
#{fmt-skip}
dependencies: [
  ; ✅ 合法：以 / 開頭，相對於 workspace 根
  '/vendor/test',
  ; ❌ 禁止：作業系統絕對路徑 /home/xxx/...
  ; ❌ 禁止：./ ../ 相對跳轉
]
```
```

**Einschränkungen:**
- Lokale Pfade dürfen nur `/xxx` lauten, Basis = Workspace-Stammverzeichnis
- Absolute Betriebssystempfade werden nicht akzeptiert
- `./` und `../` werden nicht unterstützt
- Verzeichnisse außerhalb des Arbeits-Repositories können nicht direkt referenziert werden

**Zweck:** Würde man externe Pfade erlauben, käme es leicht zu „bei mir kompiliert es, bei anderen schlägt es fehl", außerdem bestünde die Sicherheitsgefahr, ungewollt fremden externen Code zu laden.

#### 2. workspace.noml + .workspace.noml (der einzige Fluchtweg)

Zuordnungsziele unterstützen drei Arten lokaler Pfade:
- `/xxx` → Verzeichnis innerhalb des Workspace-Stamms (semantisch identisch zu package.noml)
- `./xxx`, `../xxx` → relativ zum Workspace-Stammverzeichnis aufgelöst, erlaubt das Verlassen der Workspace-Grenze
- Absoluter Betriebssystempfad → darf auf Orte außerhalb des Workspace verweisen (Forks, externe Komponenten)

```noml
; .workspace.noml 私有配置，用於本地開發
{
  ; 工作區內部
  foo: '/vendor/foo'
  ; 相對跳轉：指向工作區外部的 fork
  'github.com/lizongying/nolang/core': '../fork/core'
  ; OS 絕對路徑：指向任意外部目錄
  'github.com/lizongying/nolang/utils': '/home/lzy/code/utils'
}
```

**Kernlogik:** `package.noml` kann externen Code nicht direkt referenzieren; wer Quellcode außerhalb des Repositories einbinden möchte, muss dies explizit in der Workspace-Konfiguration zuordnen. Das entspricht einem klaren, sichtbaren Schalter für „Laden über Verzeichnisgrenzen hinweg", wodurch alle externen Abhängigkeiten zentral verwaltet werden.

#### 3. Entsprechende Einschränkungen auf Ebene der Quellcode-Imports

Im Quellcode dürfen keine absoluten Betriebssystempfade als Import geschrieben werden; alle Verweise auf externen Code müssen indirekt über Workspace-Aliase oder Remote-Paketkennungen erfolgen. Relative Sprünge mit `./` und `../` sind ebenfalls verboten.

```no
; ✅ 合法：工作區相對路徑
# /vendor/utils.helper
; ✅ 合法：遠端包標識
# github.com/lizongying/nolang/core.process
; ✅ 合法：標準庫
# std/fs
```

```no
#{fmt-skip}
; ❌ 禁止：相對跳轉
; # ./utils.helper
; # ../lib/helper.process

; ❌ 禁止：作業系統絕對路徑
; # /home/user/code/core.process
```

#### Demonstration des Datenflusses

Szenario: Das Workspace-Stammverzeichnis eines Projekts liegt unter `/code/project` und soll den externen Pfad `/code/fork/core` verwenden

❌ So geht es nicht (von package.noml verboten):

```noml
; package.noml
dependencies: [
  '/code/fork/core', ; 非法：無法逃逸工作區,
]
```

✅ Richtige Vorgehensweise (Durchbrechen der Grenze über eine Workspace-Zuordnung):

```noml
; .workspace.noml
{
  'github.com/lizongying/nolang/core': '/code/fork/core'
}
```

```no
; 源碼正常導入
# github.com/lizongying/nolang/core
```

#### Workspace-Ablauf

Einstiegsverzeichnis für Kompilierung und Ausführung ist stets das **Workspace-Verzeichnis** (das Verzeichnis mit `workspace.noml`). Der Gesamtablauf sieht so aus:

1. Der Benutzer führt im Workspace-Verzeichnis `no build` oder `no run <Paketname>` aus
2. Der Compiler liest `workspace.noml` und sucht anhand des Paketnamens den entsprechenden Unterverzeichnispfad
3. Er lädt die `package.noml` in diesem Unterverzeichnis (das Paketstammverzeichnis) und führt Build/Ausführung basierend auf dem Paketstamm durch
4. Alle Importpfade (`use /path/to/module`) werden **relativ zum Workspace-Stammverzeichnis aufgelöst** (siehe [Pfadauflösungskonventionen](path-resolution.md)), d. h. ausschließlich basierend auf dem Verzeichnis mit `workspace.noml`, unabhängig vom Paket, aus dem der Import stammt.

```
workspace/               ← 工作區目錄（workspace.noml 所在此處）
├── workspace.noml      ← 包名 → 路徑映射
├── foo/                 ← 包 foo
│   ├── package.noml        ← foo 的包根目錄
│   ├── main.no
│   └── lib.no
└── bar/                 ← 包 bar
    ├── package.noml        ← bar 的包根目錄
    └── main.no
```

> **Hinweis**: Importpfade richten sich nach dem **Workspace-Stammverzeichnis** (dem Verzeichnis mit `workspace.noml`), nicht nach dem `package.noml`-Verzeichnis eines Pakets. Befindet sich in einem Paketunterverzeichnis ein verschachteltes `package.noml`, sucht `LoadPackage` nach oben, findet das nächste `package.noml` und behandelt es als Paketstamm; die Importauflösung erfolgt jedoch einheitlich relativ zum Workspace-Stamm.

---

## Anwendungsfälle

- [notools](https://github.com/lizongying/notools) — eine Sammlung gängiger Unix-Kommandozeilen-Tools, in Nolang implementiert, darunter klassische Werkzeuge wie cat, ls, grep, wc, head und tail. Sie zeigt Nolangs praktische Eignung für die Systemprogrammierung.
