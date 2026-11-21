# Nolang

[繁體中文](README.md) · [简体中文](README_zh-CN.md) · [한국어](README_ko.md) · [日本語](README_ja.md) · [English](README_en.md) · [Русский](README_ru.md) · [Français](README_fr.md) · [Español](README_es.md) · [Deutsch](README_de.md) · [Português](README_pt.md)

Nolang ist eine speichersichere, leistungsstarke Systemprogrammiersprache mit minimaler Syntax.

## Kernfunktionen

- **Speichersicherheit**: Automatische Speicherverwaltung
- **Leistungsstärke**: Optimierung zum Kompilierungszeitpunkt, kein GC
- **Minimale Syntax**: Einfachere Syntax
- **Intentionsgesteuert**: Respektiert die Absicht des Entwicklers vollständig, die Semantik entspricht der menschlichen Intuition
- **Absturzsicher**: Eliminierung impliziter Absturzpfade

## Installation

Installation mit einem Befehl (macOS / Linux / Windows Git Bash): lädt automatisch die neueste `no` nach `$HOME/.local/bin` herunter:

```bash
curl -fsSL https://raw.githubusercontent.com/lizongying/nolang/main/install.sh | sh
```

Standardmäßig wird die Version mit **eingebettetem LLVM** installiert (`no-llvm_*`, kein separates LLVM / opt / llc nötig). Für die kleinere Pure-Go-Version verwenden Sie `NO_FLAVOR=plain` zusammen mit `no build -cc zig`:

```bash
curl -fsSL https://raw.githubusercontent.com/lizongying/nolang/main/install.sh | NO_FLAVOR=plain sh
```

Um eine Version oder ein benutzerdefiniertes Verzeichnis festzulegen (die Variablen müssen `sh` erreichen, daher zuerst das Skript speichern):

```bash
curl -fsSL https://raw.githubusercontent.com/lizongying/nolang/main/install.sh -o install.sh
NO_VERSION=0.4.28 NO_INSTALL_DIR="$HOME/bin" sh install.sh
```

| Variable          | Standard             | Beschreibung                                                             |
| ----------------- | -------------------- | ------------------------------------------------------------------------ |
| `NO_VERSION`      | `latest`             | Version, z. B. `0.4.28` oder `v0.4.28`                                  |
| `NO_FLAVOR`       | `embedded`           | `embedded`: LLVM eingebettet; `plain`: reines Go (System-LLVM nötig)     |
| `NO_INSTALL_DIR`  | `$HOME/.local/bin`   | Installationsverzeichnis                                                 |
| `NO_REPO`         | `lizongying/nolang`  | Quellrepository                                                          |

Mit `no version` prüfen. Falls `$HOME/.local/bin` nicht in `PATH` liegt, hinzufügen:

```bash
export PATH="$HOME/.local/bin:$PATH"
```

> Zum Kompilieren von `.no`-Programmen ist außerdem ein C-Linker nötig: `no build -cc zig` wird empfohlen (zig enthält clang / lld / libc), alternativ clang selbst installieren.

## Verwendung

doc

[docs](https://lizongying.github.io/nolang/)

VSCode-Plugin

[vscode](https://marketplace.visualstudio.com/items?itemName=lizongying.vscode-nolang)

nolang cli

[nolang](https://github.com/lizongying/nolang/releases/latest)

nohub — Paketindex (Community-Pakete)

[nohub](https://lizongying.github.io/nohub/)

### Suche nach Community-Paketen (nohub)

`no` liest nohubs Paketindex direkt und bietet drei schreibgeschützte Befehle:

```bash
no search <query>    # Community-Pakete nach Name / Stichwort / Kategorie suchen
no info <pkg>        # Paketmetadaten sowie Installations-/Importbefehle anzeigen
no docs <pkg>        # Dokumentations-URL ausgeben (docs → homepage → repository)
```

Standardmäßig wird der öffentliche Index verwendet. Um mehrere nohub-Instanzen zu kombinieren, wird eine Quellenliste in `~/no/sources.noml` konfiguriert (Reihenfolge des Arrays = Priorität):

```
{
  sources: [
    'https://lizongying.github.io/nohub',
    'corp = https://corp.example/nohub',
  ]
}
```

`NOHUB_SOURCES` (kommagetrennt) überschreibt die Liste vorübergehend. Alle drei Befehle akzeptieren `-source <name>`; `no search` gibt eine `SOURCE`-Spalte aus, und bei gleichem Namen und gleicher Version gewinnt die früher genannte Quelle. Eine fehlgeschlagene Quelle erzeugt nur eine Warnung. `no` lädt nur `index.noml` jeder Quelle (`<base>/index.noml` — dieselbe Datei, die die Suchseite zuerst lädt, mit einem Eintrag pro Paket in der neuesten Version); kein Tarball, kein `data.js`, die Feldzuordnung entspricht `web/app.js`. Dieselben Daten sind auf der [nohub-Suchseite](https://lizongying.github.io/nohub/) durchsuchbar.


### CLI-Befehle

| Befehl                                                       | Beschreibung                                                                    |
| ------------------------------------------------------------ | ------------------------------------------------------------------------------- |
| `no init`                                                    | Definiert den Arbeitsbereich (erzeugt workspace.jsonc, ohne package.jsonc)      |
| `no new <name>`                                              | Erstellt ein neues Paket im Arbeitsbereich (Unterverzeichnis + package.jsonc, in workspace.jsonc registriert) |
| `no fmt [-w] [-d] <file\|dir>`                               | Formatiert den Quellcode                                                        |
| `no build [-o <file>] [-cc <s>] [-target <s>] [<file\|dir>]` | Baut das Projekt (gibt executable aus)                                          |
| `no run [-cc <s>] [-target <s>] [<package\|dir\|file>]`        | Baut und führt aus (Paketname/Verzeichnis/Datei)                                |
| `no test [-cc <s>] [-target <s>] [<file\|dir>]`              | Führt Tests aus                                                                 |
| `no add <pkg>`                                               | Fügt eine Abhängigkeit hinzu                                                    |
| `no remove <pkg>`                                            | Entfernt eine Abhängigkeit                                                      |
| `no update <pkg>`                                            | Aktualisiert eine Abhängigkeit                                                  |
| `no update-all`                                              | Aktualisiert alle Abhängigkeiten                                                |
| `no list`                                                    | Listet Abhängigkeiten auf                                                       |
| `no search <query>`                                            | Community-Pakete nach Name / Stichwort / Kategorie suchen |
| `no info <pkg>`                                                | Paketmetadaten sowie Installations-/Importbefehle anzeigen |
| `no docs <pkg>`                                                | Dokumentations-URL eines Pakets ausgeben |
| `no sync`                                                    | Synchronisiert Abhängigkeiten                                                   |
| `no install [-u] [<tool>[:<version>]@<host>/<owner>/<repo>]` | Installiert binary (~/no/bin/ + Symlink)                                        |
| `no uninstall <name>`                                        | Entfernt binary und Symlink                                                     |
| `no pub --token <token> [--registry <url>]`                  | Veröffentlicht in der registry                                                  |

```bash
# Bauen (beginnt standardmäßig bei main.no)
no build                    # aktuelles Verzeichnis bauen
no build main.no            # angegebene Datei bauen
no build -o output          # Ausgabepfad angeben
no build -cc zig            # Zig-Compiler verwenden
no build -target x86_64-linux-gnu  # Cross-Compiling (Zielplattform angeben)

# Ausführen (Bauen + Ausführen)
no run                    # main.no des aktuellen Verzeichnisses bauen und ausführen
no run foo                # Paket namens foo im Arbeitsbereich ausführen
no run ./foo              # main.no des Verzeichnisses ./foo ausführen
no run -cc zig
no run -target aarch64-macos-gnu

# Testen (Bauen + Ausführen)
no test                   # Alle .no-Dateien im Verzeichnis `test` testen
no test test/my-test.no        # Eine einzelne Testdatei ausführen
no test -cc zig
no test -target x86_64-windows-gnu
```

### Neues Projekt erstellen

```shell
no new test1

cd test1

no run
```

### Hello World

Nach dem Erstellen des Projekts `main.no` bearbeiten:

```no
print('Hello, World!')
```

Ausführen:

```bash
no run
```

Ausgabe:

```
Hello, World!
```

### Einstiegsregeln

- **main.no** — Programmeinstiegspunkt
- **lib.no** — Bibliothekseinstiegspunkt, exportiert Funktionen
- **Alle .no-Dateien im Verzeichnis tests/** — enthalten Testzusicherungen

### Hinweise zu Tests

- Testdateien werden einheitlich im Verzeichnis tests/ abgelegt
- Jede Testdatei wird unabhängig gebaut
- Wenn ein Test fehlschlägt, wird ein von null verschiedener Exit-Code zurückgegeben

```bash
no vet src/std                     # Nolang vet prüft die Standardbibliothek
```

## Aus dem Quellcode bauen

Nolang sollte mit `make` gebaut werden:

```bash
make            # Alle Ziele bauen (bin/no und vscode-nolang/server/lsp)
make no         # bin/no bauen
make lsp        # LSP-Server bauen
make package    # LSP bauen und VSCode-Erweiterung paketieren
make no-wasm    # no nach WebAssembly übersetzen
make lsp-wasm   # LSP nach WebAssembly übersetzen
make playground # no.wasm + lsp.wasm + Docusaurus-Seite bauen
make clean      # Build-Artefakte bereinigen
make help       # Alle Build-Ziele anzeigen
```

> ⚠️ **Nolang muss mit `make` gebaut werden; bitte niemals direkt mit `cd src && go build` bauen.**
>
> `make` führt vor dem Bau automatisch `make gen` aus und erzeugt `src/checker/stdsig_gen.go`
> neu — eine **in die Binärdatei eingebettete** Signaturtabelle der Standardbibliothek (`funcSigs` / `methodSigs` /
> `funcParams` / `methodParams` / `structFields` / `aliases` / `structMod` /
> `enumVariants` / `embeddedStdModules`). Damit überspringt jeder `no build` die
> PASS1 „vollständiges Parsen aller std", und der Start ist deutlich schneller.
>
> Wenn Sie `src/std` geändert haben, aber **ohne** vorheriges `make gen` direkt `go build` ausführen, erkennt der Compiler
> eine Abweichung zwischen der eingebetteten Signaturtabelle und dem Inhalt von `src/std` und **lehnt den Bau
> unter sofortiger Fehlermeldung ab** (mit dem Hinweis, `make no` auszuführen). Das ist absichtlich so gestaltet: Früher führte ein
> stiller Rückfall auf vollständiges Parsen dazu, dass jede Kompilierung langsamer und schwer zu bemerken war. Nach Änderungen an `src/std` bitte immer `make no` ausführen (es führt zuerst `make gen` aus und baut dann).
>
> Führen Sie Tests ebenfalls mit `make test` aus: **Direktes `go test` schlägt fehl und wird abgelehnt, weil der Signaturschlüssel nicht injiziert wurde (`StdSigKey` ist leer)**. `make test` injiziert beim Bauen der Test-Binärdatei denselben Schlüssel, sodass der Pfad über die eingebettete Signaturtabelle genutzt wird.

---

## Glossar (glossary)

`glossary/` ist eine **in Einträge gegliederte Nolang-Wissensdatenbank**: jeder Begriff ein Eintrag mit Typ, Definition, Syntax, Beispiel, verwandten Verweisen und einer einzeiligen Zusammenfassung. Insgesamt **244 Einträge / 10 semantische Bereiche** (Typen, Syntax, Anmerkungen, Speicher, Fehler, Module, Compiler-Interna, Toolchain, alle std-Module, bekannte Fallstricke); die Daten selbst liegen im Format `.noml` vor.

Das Frontend ist in Nolang geschrieben und bietet zugleich CLI und MCP (stdio / HTTP), sodass Menschen und AI darin suchen können:

```bash
no build glossary                        # Ergebnis: glossary/dist/glossary

./glossary/dist/glossary get option      # Volltext eines einzelnen Eintrags abrufen
./glossary/dist/glossary search 三態      # Stichwortsuche
./glossary/dist/glossary list --domain=traps   # Übersicht der bekannten Fallstricke
./glossary/dist/glossary check           # Konsistenzprüfung (für CI)

./glossary/dist/glossary mcp             # MCP-Server (stdio)
./glossary/dist/glossary mcp --http=127.0.0.1:8765
```

Die Ausgabe kann mit `--format=text|md|json` umgeschaltet werden. Hinweise zur Integration und zum Datenformat finden Sie in [`glossary/README.md`](glossary/README.md).

---

## Verwandte Projekte

- [nohub — Suche im Paketindex der Nolang-Community](https://lizongying.github.io/nohub/)

  Community-Pakete nach Name, Stichwort oder Kategorie durchsuchen; Paketinformationen, Versions-Snapshots und Einstiegspunkte zum Quellcode ansehen. Der Index wird von `web/build.mjs` aus den `packages/`-Snapshots erzeugt.

- [Benchmarks Game — Leistungsvergleichsbericht Nolang vs C vs Rust](https://github.com/lizongying/no-benchmarks/results/report.md)

- [notools — Eine in Nolang implementierte Sammlung von Unix-Kommandozeilenwerkzeugen](https://github.com/lizongying/notools)

  Ein Anwendungsbeispiel für Nolang: eine Sammlung gängiger Unix-Kommandozeilenwerkzeuge, implementiert in der Sprache Nolang, darunter klassische Werkzeuge wie cat, ls, grep, wc, head und tail. Sie zeigt das praktische Anwendungspotenzial von Nolang im Bereich der Systemprogrammierung.

---

## TODO

## Lizenz

Apache-2.0
