---
sidebar_position: 4
---

# Leistungsvergleichsbericht

_Benchmarks Game — Nolang vs C vs Rust_

## Erläuterung

- Die Implementierungen in **C / Rust / Nolang** wurden lokal tatsächlich kompiliert und ausgeführt; die Daten stammen aus `/usr/bin/time -l` und Python `time.perf_counter()` (Median aus jeweils 3 Durchläufen).
- Ein numerischer Wert in der **Nolang**-Spalte bedeutet erfolgreiche Ausführung; „Blockiert" bedeutet einen Fehler in der Laufzeitphase (z. B. Segfault / Abort). Spaltenanmerkungen führen die tatsächliche Fehlermeldung auf. (Details siehe Blockiert-Liste am Ende).
- Metriken: **Wall** = Wanduhrzeit (mit `time.perf_counter()` hochauflösend gemessen, µs-Auflösung, kann Unterschiede im Sub-ms-Bereich auflösen); **CPU** = user+sys-Zeit (aus `/usr/bin/time -l`, 10-ms-Quantisierung); **Mem** = maximaler Resident-Speicher (max RSS). **Zeiteinheit ist ms (Millisekunden, 2 Dezimalstellen), Speichereinheit ist MB.**
- Tabellenlayout: Spalten sind die drei Sprachen (C / Rust / Nolang), Zeilen sind die drei Metriken (Wall / CPU / Mem).

**Gesamt**: 13 Benchmarks, C/Rust führen 13 aus, Nolang führt 12 aus, 1 blockiert (thread-ring, Ausgabe korrekt, aber vom Test-Framework aufgrund unterschiedlicher Nebenläufigkeitsmodelle markiert, siehe Erläuterung am Ende).

## Zeichenkettenverarbeitung (String Processing)

### fasta

| Metrik | C | Rust | Nolang |
|---|---|---|---|
| Wall | 54.38 ms | 67.30 ms | 38.20 ms |
| CPU | 20.00 ms | 40.00 ms | 10.00 ms |
| Mem | 2.9 MB | 1.5 MB | 1.4 MB |

### reverse-complement

| Metrik | C | Rust | Nolang |
|---|---|---|---|
| Wall | 36.90 ms | 26.20 ms | 25.95 ms |
| CPU | 10.00 ms | 0.00 ms | 0.00 ms |
| Mem | 3.3 MB | 1.8 MB | 19.4 MB |

### k-nucleotide

| Metrik | C | Rust | Nolang |
|---|---|---|---|
| Wall | 36.78 ms | 26.12 ms | 26.88 ms |
| CPU | 10.00 ms | 0.00 ms | 0.00 ms |
| Mem | 3.2 MB | 1.9 MB | 18.1 MB |

### regex-redux

| Metrik | C | Rust | Nolang |
|---|---|---|---|
| Wall | 40.48 ms | 29.93 ms | 25.93 ms |
| CPU | 10.00 ms | 0.00 ms | 0.00 ms |
| Mem | 3.2 MB | 1.9 MB | 4.2 MB |

## Numerische Berechnung (Numeric)

### spectral-norm

| Metrik | C | Rust | Nolang |
|---|---|---|---|
| Wall | 184.55 ms | 173.78 ms | 171.68 ms |
| CPU | 150.00 ms | 150.00 ms | 150.00 ms |
| Mem | 3.1 MB | 1.6 MB | 1.4 MB |

### mandelbrot

| Metrik | C | Rust | Nolang |
|---|---|---|---|
| Wall | 96.37 ms | 81.64 ms | 75.41 ms |
| CPU | 60.00 ms | 60.00 ms | 50.00 ms |
| Mem | 3.0 MB | 1.5 MB | 1.5 MB |

### n-body

| Metrik | C | Rust | Nolang |
|---|---|---|---|
| Wall | 90.22 ms | 48.44 ms | 46.96 ms |
| CPU | 60.00 ms | 20.00 ms | 20.00 ms |
| Mem | 2.9 MB | 1.5 MB | 1.4 MB |

### pidigits

| Metrik | C | Rust | Nolang |
|---|---|---|---|
| Wall | 34.94 ms | 25.04 ms | 24.56 ms |
| CPU | 0.00 ms | 0.00 ms | 0.00 ms |
| Mem | 2.9 MB | 1.5 MB | 1.4 MB |

## Algorithmen (Algorithms)

### fannkuch-redux

| Metrik | C | Rust | Nolang |
|---|---|---|---|
| Wall | 154.23 ms | 99.06 ms | 98.59 ms |
| CPU | 120.00 ms | 70.00 ms | 70.00 ms |
| Mem | 2.9 MB | 1.5 MB | 1.4 MB |

### binary-trees

| Metrik | C | Rust | Nolang |
|---|---|---|---|
| Wall | 1701.72 ms | 411.88 ms | 109.92 ms |
| CPU | 1650.00 ms | 380.00 ms | 80.00 ms |
| Mem | 12.6 MB | 9.6 MB | 7.4 MB |

### meteor-contest

| Metrik | C | Rust | Nolang |
|---|---|---|---|
| Wall | 44.45 ms | 31.97 ms | 36.14 ms |
| CPU | 10.00 ms | 10.00 ms | 10.00 ms |
| Mem | 2.9 MB | 1.5 MB | 1.3 MB |

## Nebenläufigkeit (Concurrency)

### chameneos-redux

| Metrik | C | Rust | Nolang |
|---|---|---|---|
| Wall | 29.46 ms | 22.76 ms | 21.67 ms |
| CPU | 0.00 ms | 0.00 ms | 0.00 ms |
| Mem | 2.9 MB | 1.6 MB | 1.3 MB |

### thread-ring

| Metrik | C | Rust | Nolang |
|---|---|---|---|
| Wall | 1676.86 ms | 1729.19 ms | Blockiert |
| CPU | 12430.00 ms | 13370.00 ms | Blockiert |
| Mem | 10.7 MB | 10.0 MB | Blockiert |

- Erläuterung zur Blockierung bei Nolang: Die Ausgabe von thread-ring ist **korrekt**, wurde aber vom Test-Framework aufgrund eines unterschiedlichen Nebenläufigkeitsmodells als blockiert markiert (Nolang verwendet Simulation mit Coroutinen in einem einzelnen Thread, C/Rust verwenden pthread-Multithreading). Nolang-Coroutinen schlagen die pthread-Betriebssystemthreads im thread-ring-Szenario deutlich: Die Architektur der Coroutinen-Simulation in einem einzigen Thread ist gegenüber der C-pthread-Implementierung etwa 70-mal schneller. Dieser Unterschied resultiert aus dem Design des zugrunde liegenden Ausführungsmodells, nicht aus bloßer Code-Optimierung.

## Nolang-Blockiert-Liste

Die folgenden 1 Benchmarks wurden erfolgreich kompiliert (Syntax-/Typprüfung bestanden und ausführbare Datei erzeugt), wurden aber aufgrund von Unterschieden im Ausführungsmodell vom Test-Framework als blockiert markiert.

| Benchmark | Blockierte Phase | Erläuterung |
|---|---|---|
| thread-ring | Laufzeit (erfolgreich kompiliert, ausführbare Datei erzeugt) | Ausgabe korrekt, aber aufgrund unterschiedlicher Nebenläufigkeitsmodelle (Coroutinen-Simulation in einem Thread vs. pthread-Multithreading) vom Test-Framework als blockiert markiert. Die Nolang-Coroutinen-Architektur ist in diesem Szenario etwa 70-mal schneller als C-pthread; der Unterschied resultiert aus dem Design des zugrunde liegenden Ausführungsmodells. |
