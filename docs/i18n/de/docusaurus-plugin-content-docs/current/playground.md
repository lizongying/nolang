---
sidebar_position: 5
---

# Playground

Der Nolang Playground ist eine Online-Umgebung, in der du Nolang-Programme direkt im Browser bearbeiten, kompilieren und ausführen kannst. Er kompiliert den Nolang-Compiler (`no.wasm`) und den Language Server (`lsp.wasm`) zu WebAssembly und führt Benutzercode über WASI in der Browser-Sandbox aus — ohne dass eine lokale Toolchain installiert werden muss.

Jetzt ausprobieren: [**/playground**](/playground)

## Verwendung

Die Playground-Seite ist in zwei Bereiche unterteilt:

- **Editor links**: Ein Code-Editor basierend auf [CodeMirror 6](https://codemirror.net/) mit Nolang-Syntaxhervorhebung, automatischer Einrückung und Verlauf.
- **Output / stderr / Diagnostics rechts**: Ausführungsresultate, Standardfehlerausgabe und Diagnosemeldungen.

Die Oberfläche folgt automatisch der Docusaurus-Sprache (Vereinfachtes Chinesisch / Englisch), und die Ausgabebereiche unterstützen sowohl helle als auch dunkle Themes.

### Werkzeugleiste

| Schaltfläche  | Funktion                                                            |
| ------------- | ------------------------------------------------------------------- |
| **▶ Run**     | Aktuellen Code nach WASM kompilieren und über WASI ausführen; stdout erscheint im rechten Output-Bereich. |
| **Format**    | Aktuellen Code über den LSP mit `no fmt` formatieren.           |
| **Examples**  | Ein integriertes, nach Kategorien gruppiertes Beispiel laden und den Editor-Inhalt ersetzen. |

Auf der rechten Seite der Werkzeugleiste wird der aktuelle Status angezeigt (Leerlauf / Kompilieren / Läuft / Fertig / Fehler), und unter dem Dropdown erscheint eine zweisprachige Beschreibung des ausgewählten Beispiels. Das zuletzt gewählte Beispiel wird im lokalen Browser-Speicher gespeichert.

### Beispiele

Das Examples-Dropdown enthält **24 Beispiele**, die in sechs Themengruppen unterteilt sind und alle in CI über exakt denselben `--wasm-direct`-Kompilierungspfad wie im Browser validiert werden:

| Kategorie | Anzahl | Inhalt |
| -------- | ----- | ------ |
| Grundlagen | 7 | Hello World, Typinferenz, Funktionen & Rekursion, Fibonacci, Arithmetik & Bitoperationen, Formatierte Ausgabe, Zeichenketten |
| Ablaufsteuerung | 6 | Kurzschluss-Guards, Pattern Matching, FizzBuzz, vier Formen von Bereichsschleifen, bedingte while-Schleife, break / continue |
| Datenstrukturen | 4 | Felder fester Länge, dynamische Slices, Strukturen, Methoden |
| Skalartypen | 4 | char, bool, f64, byte |
| Standardbibliothek | 2 | math.max / math.min / sqrt, rand.rand |
| Fehlerbehandlung | 1 | option und nil mit dreiarmigem match |

Jedes Beispiel enthält eine stabile erwartete Teilzeichenfolge in der Ausgabe. Die CI kompiliert jedes Beispiel, führt es mit `wasmtime` aus und prüft, dass diese Zeichenfolge in stdout erscheint — so können Beispiele bei einer Weiterentwicklung der Sprache oder des Direct-WASM-Backends nicht unbemerkt veralten.

Nach Auswahl eines Beispiels löscht der Playground den aktuellen Output, stderr und die Diagnostics und ersetzt den Editor durch den Inhalt des Beispiels.

### Diagnose

Wenn der LSP Syntax- oder Typfehler erkennt, erscheinen diese im Bereich Diagnostics unten rechts. Ein Klick auf einen Diagnoseeintrag springt zur entsprechenden Zeile und Spalte. `file:line:col`-Meldungen in der Compiler-stderr werden zusätzlich als **Run Errors**-Markierungen geparst, wobei nachgestellte TraceIDs und lokalisierte Schweregrad-Präfixe automatisch entfernt werden.

### Fehler und Zeitüberschreitungen

Früher wurden WASM-Ladefehler nur an `console.warn` geschrieben, sodass der Benutzer einen Run-Button sah, der nichts tat. Das aktuelle Verhalten:

- Wenn `no.wasm` oder `lsp.wasm` nicht heruntergeladen oder kompiliert werden können, erscheint oben auf der Seite ein schließbares Fehlerbanner, das die betroffene Ressource und den Grund nennt.
- Ein Fehler bei der Initialisierung des Language Server zeigt ebenfalls ein Banner; der Editor bleibt benutzbar, aber Vervollständigung, Hover und Formatierung sind nicht verfügbar.
- Jede Ausführung wird von einem Watchdog im Hauptthread überwacht (standardmäßig 8 Sekunden). `wasi.start()` ist ein synchroner Aufruf innerhalb des Workers, sodass eine Endlosschleife verhindert, dass irgendein Timer in diesem Worker ausgelöst wird — der einzige Weg, sie tatsächlich zu stoppen, ist das Beenden des Workers vom Hauptthread aus. Nach einer Zeitüberschreitung wird der Worker beendet, beim nächsten Lauf automatisch neu erstellt und die WASM-Aufwärmung sofort neu gestartet.
- Wiederholte Klicks auf Run werden durch eine Parallelitätssperre abgefangen, und ausstehende Debounce-Timer für Bearbeitungen werden beim Wechseln von Beispielen oder beim Formatieren abgebrochen, damit alte Eingaben neue Inhalte nicht überschreiben können.

`no.wasm` und `lsp.wasm` werden in Browser-Leerlaufzeiten bereits heruntergeladen und als Stream kompiliert, sodass der erste Run nicht mehr den vollständigen 27-MB-Download und die Kompilierung abwarten muss.

## Einschränkungen

Da der Playground in einer Browser-WASM-Sandbox läuft, sind einige Nolang-Funktionen nicht verfügbar:

### Kein FFI

Der Browser kann native C-Bibliotheken nicht linken, daher können FFI-Deklarationen wie `#{c}`, `#{cpp}`, `#{rust}` im Playground nicht verwendet werden. Code mit FFI (etwa `sqlite`- oder `mysql`-Treiber) kann nicht ausgeführt werden.

### Kein fork / exec / pipe

WASI preview1 stellt keine Prozessverwaltungs-APIs bereit, daher ist die Standardbibliothek `process` (`process.start`, `process.wait` usw.) im Playground nicht verfügbar.

### Netzwerk-Sockets nicht verfügbar

WASI preview1 stellt keine Socket-APIs bereit, daher sind die folgenden Standardbibliotheken im Playground nicht verfügbar:

- `net` (TCP-Listener / conn)
- `http` / `http2` / `http3` (HTTP-Client)
- `ws` (WebSocket)
- `tls` (TLS-Verbindung)
- `quic`, `sse`, `dns`

**Eine strengere Einschränkung für `no` / `lsp` selbst**: Die wasm-Artefakte dürfen nicht einmal eine Socket-Funktion *importieren*. Der Browser instanziiert sie über `@wasmer/wasi`, das keine Implementierung für `sock_accept` / `sock_shutdown` besitzt. Sobald ein Artefakt `net` / `net/http` in den wasip1-Link zieht, wirft `WebAssembly.Instance`:

```
Failed to instantiate WASI: RuntimeError: JsValue(LinkError: WebAssembly.Instance():
Import #28 "wasi_snapshot_preview1" "sock_accept": function import requires a callable)
```

Das Programm erreicht dadurch nie `_start` — der Sprachdienst meldet „Initialisierung fehlgeschlagen" (Vervollständigung, Hover und Formatierung fallen aus, während der Editor selbst weiter funktioniert), und der Run-Button scheitert ebenso. Netzwerkcode, der nur in der nativen Umgebung sinnvoll ist, liegt daher hinter `!wasip1`, und auf der wasip1-Seite bleiben nur Fehler-Stubs: `cmd/lsp/pprof_wasip1.go`, `cmd/no/serve_esm_wasip1.go`, `package/download_net_wasip1.go`, `package/release_net_wasip1.go`.
`bun scripts/wasm_imports_check.ts` (`make wasm-imports-check`) überwacht diese Eigenschaft anhand der Importtabelle der Engine selbst.

### `poll_oneoff` wird vom Frontend bereitgestellt

Eine saubere Importtabelle bedeutet nur, dass die Funktionen vorhanden sind, nicht dass sie funktionieren. Der Körper von `poll_oneoff` in `@wasmer/wasi` ist Rusts `unimplemented!()`: Sobald er aufgerufen wird — selbst mit null Subscribers — führt er `unreachable` aus und bringt die gesamte Instanz zum Absturz:

```
Error while running start function: RuntimeError: JsValue(RuntimeError: unreachable)
```

Die Go-Runtime verwandelt sowohl `time.Sleep` als auch `netpoll(delay)` in eine einzige Abonnementierung der monotonen Uhr, sodass **jedes Go-Programm hiervon betroffen ist**: `no fmt` / `no vet` / `no build` starben alle beim ersten Schlafen, während die Importprüfung weiterhin grün blieb.

Die Lösung liegt in `docs/src/playground/wasm-loader.ts`: `instantiateWasi()` erzeugt das Importobjekt selbst aus `wasi.getImports()` und ersetzt `poll_oneoff` durch eine JS-Implementierung (Uhr-Abonnements schlafen bis zum nächsten Fälligkeitszeitpunkt und melden dann ein Uhr-Ereignis; reine fd-Abonnements melden sofort Bereitschaft, damit ein Warten nie ewig blockiert). Beachte, dass die Überschreibung **nicht** an `wasi.instantiate(module, overrides)` übergeben werden darf: Die eigenen Funktionen des Glue haben Vorrang, die Überschreibung wird still ignoriert und das Programm stürzt trotzdem ab. Weil das Importobjekt hier erstellt wird, muss `wasi.start(instance)` die Instanz explizit übergeben werden.

### Kompilierzeit beim ersten Lauf

Run verwendet `no build --wasm-direct`. Messungen bei Hello World zeigen heute etwa **0,5 Sekunden** innerhalb von wasm (native `no`, gleicher Befehl: etwa 0,6 Sekunden; beides nach der Vorbackung des std-Index im November 2026, vorher waren es rund 55 bzw. 11 Sekunden).

Der Engpass lag nie in der Codegenerierung, sondern darin, dass bei jedem Kaltstart die gesamte Standardbibliothek **zweimal** geparst wurde: `ValidateUndefinedVars` sammelt die exportierten Symbole aller 119 std-Module, damit modulqualifizierte Aufrufe wie `math.max(...)` erkannt werden; `CollectStdUnionAliases` parst dieselben Module erneut, um Union-Aliase zu finden. Heute backt `genstdsig` sowohl „die exportierten Symbole jedes Moduls" als auch „welche Module Union-Aliase besitzen" in `stdsig_gen.go`, sodass zur Laufzeit direkt in der Tabelle nachgeschlagen wird. Zwei Wächter halten die Tabellen ehrlich (`hir.TestModuleExportsMatchChecker` vergleicht Modul für Modul und Feld für Feld das aktuell berechnete HIR-Ergebnis, `TestUnionAliasNarrowingMatchesFullScan` vergleicht die schmale mit der vollständigen Suche); veraltete Tabellen lassen die Tests rot werden, statt die Semantik still zu ändern.

Der Watchdog behält seine beiden Budgets (180 Sekunden für Laden + Kompilieren, 5 Sekunden für das Benutzerprogramm): Ersteres hat heute großen Spielraum und bleibt als Puffer für Kaltstarts, große Programme und langsame Geräte erhalten.

### Virtuelles Dateisystem

WASI preview1 stellt nur die Dateideskriptoren stdin / stdout / stderr bereit; es gibt kein echtes Dateisystem. Daher funktionieren `fs`, `path`, `os.get-env`, `os.set-env` und andere datei- oder umgebungsbezogene Standardbibliotheken im Playground möglicherweise nicht wie erwartet.

### Speicherlimit

Browser-WASM ist in der Regel auf einen 32-Bit-Adressraum beschränkt; die nutzbare Speicherobergrenze liegt bei etwa **2 GB bis 4 GB** (je nach Browser und Betriebssystem). Programme mit hohem Speicherbedarf (etwa tiefe Rekursion oder große Arrays) können aufgrund von out-of-memory fehlschlagen.

## Browser-Kompatibilität

Der Playground benötigt einen modernen Browser mit WebAssembly-Unterstützung. Empfohlen werden folgende Versionen oder neuer:

| Browser   | Mindestversion |
| --------- | --------------- |
| Chrome    | 100+            |
| Firefox   | 100+            |
| Safari    | 16+             |
| Edge      | 100+            |

Wenn der Browser WebAssembly nicht unterstützt oder JavaScript deaktiviert ist, kann der Playground den Editor nicht laden oder Code nicht ausführen.

## Entwicklung und Validierung

Nach Änderungen am Playground können in einem sauberen Build-Baum folgende Prüfungen ausgeführt werden:

```bash
make playground-demos                       # 編譯 + 在行程內用 playground 自己的 WASI loader 執行全部範例
make playground-demos DEMO_RUN=wasmtime     # 改用外部 wasmtime 執行（交叉比對用）
make playground-demos DEMO_RUN=none         # 只編譯、不驗輸出（不可當發佈門檻）
make wasm-imports-check                     # 重建兩個 wasm 並驗證導入表無 socket 函式
make wasm-runtime-smoke                     # 用 worker 的同一條載入路徑實跑 version/fmt/vet
RUN_WASM_LSP=1 RUN_WASM_BUILD=1 make wasm-runtime-smoke   # 追加語言服務與完整編譯+執行
bun scripts/playground_ui_check.ts          # 診斷解析、看門狗、worker 恢復、i18n
make playground-smoke                       # dev server 與 WASM 路由 HTTP 200
cd docs && bun run typecheck                # TypeScript 型別檢查
```

Der Compiler von `playground-demos` ist das **native** `no`, aber die Artefakte stammen aus demselben `--wasm-direct -target wasm32-wasi`-Pfad wie im Browser und werden über `instantiateWasi()` in `wasi-glue` + `wasm-loader` ausgeführt — exakt dem Code, den `runner.worker` ausführt. Daher muss wasmtime nicht installiert werden und die CI kann es direkt ausführen. Es prüft gleichzeitig drei Dinge: das Snippet kompiliert, das Artefakt besteht die WebAssembly-Validierung (ein leerer Stack durch `local.set` eines Feldwerts bei value-match zeigt sich hier) und die gedruckte Ausgabe entspricht `expect` (ein Direct-WASM-Backend, das bei `?=` still `0` meldet, zeigt sich hier ebenfalls).

Das Einzige, was es nicht erkennen kann, ist die Importtabelle von `no.wasm` / `lsp.wasm` selbst — diese Artefakte entstehen nicht aus Snippets —, wofür `wasm-imports-check` die Lücke schließt: Fehlt `@wasmer/wasi` die `sock_*`-Funktionen, wirft `WebAssembly.Instance` einen `LinkError`, und der gesamte Playground (einschließlich Sprachdienst) startet nicht. Auf der Worker-Seite gibt es eine weitere Absicherung: `instantiateWasi()` ersetzt jede von `@wasmer/wasi` fehlende WASI-Funktion durch einen Stub, der `ENOSYS` / `NOTSUP` zurückgibt, sodass ein unerwarteter Import zu einem fehlgeschlagenen Systemaufruf degradiert statt den gesamten Dienst zu Fall zu bringen.

`wasm-imports-check` kann ebenfalls nicht erkennen, dass „eine Funktion vorhanden ist, ihr Körper aber abstürzt" (die oben beschriebene `poll_oneoff`-Klasse), daher schließt `make wasm-runtime-smoke` die letzte Lücke: Es führt `no version` / `no fmt` / `no vet` über den echten `instantiateWasi()`-Pfad aus und kann zusätzlich den Sprachdienst sowie eine vollständige Kompilierung + Ausführung prüfen.

`docs/static/wasm/*.wasm` ist durch `.gitignore` ausgeschlossen; lokale Artefakte sollten nie committet werden. `no.wasm` und `lsp.wasm` werden von der CI aus demselben Quellbaum mit demselben `StdSigKey` neu erstellt, sodass der Compiler im Browser, sein eingebetteter std-AST-Blob und das Direct-WASM-Backend aus einem einzigen Build stammen. Versionungsungleichgewicht zwischen diesen dreien war in der Vergangenheit die übliche Ursache für `unreachable`-Traps und WASM-Validierungsfehler.

Die CI (`pr-verify` in `.github/workflows/build.yml` und der Site-Build in `static.yml`) führt nacheinander Go-Tests, Typprüfung, UI-Prüfungen und die Beispiel-Gate aus und baut erst danach WASM aus einem sauberen Baum neu und deployt die Seite.
