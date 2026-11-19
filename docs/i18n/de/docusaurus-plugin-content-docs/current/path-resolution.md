---
sidebar_position: 99
---

# Pfadauflösungskonvention: relativ zum Workspace-Root

## Konvention (übernommen)

Alle **Embed-Pfade** und **Import-Pfade** (`use` / `#`-Direktiven, Embed-Ressourcen) werden zu einem **kanonischen absoluten Pfad relativ zum Workspace-Root** aufgelöst — sie werden **nicht mehr relativ zum „Verzeichnis der aktuellen Quelldatei" aufgelöst**.

- „Workspace-Root" = das Verzeichnis, das `workspace.noml` enthält, also `pkg.WorkspaceRoot()`.
  Hinweis: Das Verzeichnis mit `package.noml` ist der **Package-Root** (`pkg.RootDir`); beide liegen nicht auf derselben Ebene — der Workspace-Root liegt darüber (enthält `workspace.noml`, kann mehrere Packages enthalten), während der Package-Root das Verzeichnis eines bestimmten **Packages** ist (enthält `package.noml`), das mehrere **Module** (`.no`-Quelldateien) beherbergt. Verwechsle die beiden nicht.
- Nach der Auflösung wird eine Kanonisierung vorgenommen (`filepath.Clean`, Entfernen von `.`/`..`, Vereinheitlichung der Trennzeichen), um einen **global eindeutigen** kanonischen Pfad zu erhalten, der als alleinige Kennung für Modul-Laden, Cache-Schlüssel und AST-/semantische Zuordnung dient.
- Dieser kanonische Pfad ist die **Pfadkomponente** der Cache-Schlüssel von `lexer.tokenCache` und `checker.parseProgramFileCache`. Der Cache-Schlüssel ist tatsächlich ein zusammengesetzter Schlüssel `(kanonischer Pfad, Inhalts-Hash)` (`cache.Key`, siehe `src/cache/lru.go`): gleicher Pfad bei geändertem Inhalt → anderer Hash → automatische Invalidierung; veraltete Token/AST werden nicht mehr zurückgegeben. Der Cache ist ein LRU mit begrenzter Kapazität, sodass lang laufende Prozesse (LSP / fmt) kein Gedächtnisleck verursachen (Punkt ③ der ursprünglichen Zwei-Pass-Architekturprüfung).

## Begründung

1. **Cache-Schlüsselkollisionen an der Wurzel beseitigen**. Der alte Token/AST-Cache-Schlüssel war die „Pfadzeichenfolge"; wenn verschiedene Quelldateien mit derselben relativen Zeichenfolge importierten/einbetteten (z. B. `utils/no` oder der Embed-Name `data.json`), trafen sie denselben Cache-Eintrag → Vermischung, falsche Token/AST. Nach dem Wechsel zu workspace-root-relativen kanonischen absoluten Pfaden hat **jede logische Datei einen eindeutigen Schlüssel; dieselbe Datei wird stets demselben Schlüssel zugeordnet**, und Kollisionen verschwinden konstruktionsbedingt (Punkt ④ der ursprünglichen Zwei-Pass-Architekturprüfung).
2. **Deutliche Vereinfachung und Robustheit der Compiler-Logik**. Die Pfadauflösung konvergiert von „über parser / transpiler / checker verstreute Auflösung relativ zur aktuellen Datei" zu einem **einzelnen Kanonisierungsschritt**; Vorwärtsreferenzen, Modul-Laden, Cache-Schlüssel und Pfade in Diagnosemeldungen basieren alle auf derselben Basis und hängen nicht mehr vom Ort der importierenden Datei ab.
3. **Den Weg für dateiübergreifendes / paralleles Build-Caching ebnen**. Kanonische absolute Pfade machen Cache-Schlüssel stabil und global vergleichbar und erleichtern künftige Deep-Clone-Wiederverwendung (Vermeidung doppelter `ParseProgram`) sowie Optimierungen durch verzögerte Auflösung.

## Gültigkeitsgrenzen

- **Absolute Pfade** (lokale Module, die mit `/` beginnen): bereits relativ zum Workspace-Root aufgelöst, unverändert.
- **`std/`-Module**: stammen aus dem eingebetteten `StdFS`, Schlüssel `std/<rel>.no`, unverändert und nie mit lokalen Pfaden verwechselt.
- **Embed-Ressourcen**: unabhängig vom relativen Namen, mit dem sie referenziert werden, intern immer zu einem workspace-root-relativen kanonischen Embed-Schlüssel normalisiert.

## Implementiert (2026-08-02)

Die vereinheitlichten Hilfsfunktionen liegen in `src/package/paths.go` (Paket `pkg`, reine Standardbibliothek, keine zyklischen Abhängigkeiten):

- `FindWorkspaceRoot(start)`: sucht von `start` aufwärts nach dem Verzeichnis mit `workspace.noml`.
- `FindPackageRoot(start)`: sucht von `start` aufwärts nach dem Verzeichnis mit `package.noml` (Rückfall, wenn kein Workspace vorhanden ist).
- `ResolveToWorkspaceRoot(wsRoot, rel)`: normalisiert einen beliebigen Import-/Embed-Rohpfad zu einem workspace-root-relativen absoluten Pfad (entfernt führendes `/`; absolute Pfade unverändert; Rückfall auf das aktuelle Verzeichnis, wenn `wsRoot` leer ist).
- `ResolveEmbedBase(sourcePath)`: Auflösungsbasis für relative Embed-Pfade — Workspace-Root bevorzugt, Rückfall auf Package-Root / Quelldateiverzeichnis.

Vereinheitlichte Einstiegspunkte:

- `src/build/transpiler.go` `resolveUse`: Zweig A (`/`-Präfix) und Zweig E (Alias) wurden auf `pkg.ResolveToWorkspaceRoot(t.workspaceRoot(), path)` umgestellt, mit einer neuen Methode `t.workspaceRoot()` (bevorzugt `pkg.WorkspaceRoot()`, andernfalls Aufwärtssuche ab `sourcePath` nach `workspace.noml`). Die alte redundante Logik `if t.pkg != nil { baseDir = pkg.RootDir; if wsRoot... }` wurde entfernt; der cwd-relative Rückfall nutzt nun `FindWorkspaceRoot`.
- `src/build/transpiler.go` `processEmbeds`: relative Embed-Pfade nutzen nun `pkg.ResolveEmbedBase(sourcePath)` (Workspace-Root bevorzugt).
- `src/checker/checker.go` `ValidateEmbedAnnotations`: Embed-Validierungspfade nutzen ebenfalls `pkg.ResolveEmbedBase`.

Cache-Schlüssel (`tokenCache` / `parseProgramFileCache`) erhalten über `resolveFile(filePath)` den workspace-root-relativen kanonischen absoluten Pfad als Pfadkomponente; zusammen mit dem Inhalts-Hash der Quelldatei zu einem zusammengesetzten Schlüssel (`cache.Key`) geformt, verschwinden Kollisionen konstruktionsbedingt, werden Bearbeitungen desselben Pfads automatisch invalidiert und verhindert der begrenzte LRU Gedächtnislecks.

Eingebettete Module in `std/` und `js/` nutzen weiterhin `StdFS`/`JsFS` (Schlüssel `std/<rel>.no` / `js/<rel>.no`), davon unberührt; `processEmbeds` gilt nur für das Hauptprogramm (Benutzercode) — die eigenen Embeds von std-Modulen werden weiterhin relativ zu ihrem eigenen Ort aufgelöst.

> Status: **Implementiert (2026-08-02)**. Neuer Code sollte direkt `pkg.ResolveToWorkspaceRoot` / `pkg.ResolveEmbedBase` verwenden; verwende **nicht** das „aktuelle Dateiverzeichnis" (`filepath.Dir(t.sourcePath)` usw.) für die relative Auflösung.

## Begriffsabgleich (vereinheitlicht, 2026-08-02)

- **workspace (Arbeitsbereich)** — entspricht in der Regel einem Repo. Im Root-Verzeichnis liegt `workspace.noml`; es kann mehrere Packages enthalten. Der Workspace-Root ist die einzige Basis für die Auflösung aller Embed-/Import-Pfade.
- **package (Paket)** — eine große Kompilierungseinheit: eine Bibliothek oder eine ausführbare Datei. Im Root-Verzeichnis liegt `package.noml` (der **Package-Root**). Abhängigkeiten, emit-Backend usw. werden deklariert. Ein Package enthält mehrere Module.
- **module (Modul)** — in der Regel eine Quelldatei (`.no`). Aktuelle Konvention: **eine Datei = ein Modul**. Ein Modul laden = eine `.no`-Datei laden.

Die Hierarchie lautet: **workspace ⊃ package (eines oder mehrere) ⊃ module (eines oder mehrere)**. Verwende „module" nicht im Sinne von „package" (Kompilierungseinheit) — dafür ist „package" zuständig.
