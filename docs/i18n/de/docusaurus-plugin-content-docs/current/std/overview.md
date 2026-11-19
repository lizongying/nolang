---
sidebar_position: 3
---

# Standardbibliothek

Die Nolang-Standardbibliothek (`src/std/`) enthält über 80 Module und deckt Formatierung, Mathematik, Zeichenketten, Datenstrukturen, Kodierung/Dekodierung, Kryptografie, Kompression, Dateioperationen, I/O-Abstraktionen, asynchrone Coroutinen, Dateityp-Erkennung und mehr ab.

Verwendung: `# std/xxx` (Kernmodule benötigen keinen Import).

> **Die alte Syntax `use std/xxx` funktioniert weiterhin, ist aber veraltet (deprecated); die neue Syntax `# std/xxx` wird empfohlen.**

> **Hinweis: Alle Codebeispiele in diesem Dokument folgen der Regel „eine Anweisung pro Zeile" — es ist verboten, mit Semikolon `;` oder Komma `,` mehrere Anweisungen in eine Zeile zu schreiben.** Zum Beispiel ist `out = from-i64(v), out = from-u64(v)` falsch und muss auf mehrere Zeilen aufgeteilt werden.

---

## Modulkategorien

- [Grundtypen](basic-types)
- [Kernbibliothek](core-library)
- [Betriebssystem und Dateien](os-and-files)
- [Zeit und Datum](time-and-date)
- [Protokollierung](logging)
- [Datenstrukturen](data-structures)
- [Datenbank](database)
- [Kodierung](encoding)
- [Archive](archives)
- [Kryptografie und Hashing](crypto)
- [Datenaustausch](data-exchange)
- [Sonstiges](others)
- [Modulübersicht](module-overview)
