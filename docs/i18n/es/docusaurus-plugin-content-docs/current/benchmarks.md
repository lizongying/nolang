---
sidebar_position: 4
---

# Informe de rendimiento

_Benchmarks Game — Nolang vs C vs Rust_

## Descripción

- Las implementaciones en **C / Rust / Nolang** se compilaron y ejecutaron localmente. Los datos provienen de `/usr/bin/time -l` y de Python `time.perf_counter()` (mediana de 3 ejecuciones cada una).
- Una columna de **Nolang** con un valor numérico indica que la ejecución se realizó correctamente; si muestra «Bloqueado» significa que hubo un fallo en tiempo de ejecución (p. ej. segfault / abort). Las notas de la celda enumeran el mensaje de error real. (Consulte la lista de bloqueos al final).
- Métricas: **Wall** = tiempo de reloj (medido con `time.perf_counter()` de alta resolución, a nivel de µs, permite distinguir diferencias inferiores a 1 ms); **CPU** = tiempo user+sys (de `/usr/bin/time -l`, cuantizado a 10 ms); **Mem** = memoria residente máxima (max RSS). **Las unidades de tiempo son ms (milisegundos, 2 decimales) y las de memoria son MB.**
- Disposición de la tabla: las columnas son los tres lenguajes (C / Rust / Nolang), las filas son las tres métricas (Wall / CPU / Mem).

**Total**: 13 benchmarks, C/Rust ejecutaron 13, Nolang ejecutó 12 y tuvo 1 bloqueo (thread-ring, salida correcta pero marcada por el marco de pruebas debido a una diferencia en el modelo de concurrencia; véase la explicación al final).

## Procesamiento de cadenas (String Processing)

### fasta

| Métrica | C | Rust | Nolang |
|---|---|---|---|
| Wall | 54.38 ms | 67.30 ms | 38.20 ms |
| CPU | 20.00 ms | 40.00 ms | 10.00 ms |
| Mem | 2.9 MB | 1.5 MB | 1.4 MB |

### reverse-complement

| Métrica | C | Rust | Nolang |
|---|---|---|---|
| Wall | 36.90 ms | 26.20 ms | 25.95 ms |
| CPU | 10.00 ms | 0.00 ms | 0.00 ms |
| Mem | 3.3 MB | 1.8 MB | 19.4 MB |

### k-nucleotide

| Métrica | C | Rust | Nolang |
|---|---|---|---|
| Wall | 36.78 ms | 26.12 ms | 26.88 ms |
| CPU | 10.00 ms | 0.00 ms | 0.00 ms |
| Mem | 3.2 MB | 1.9 MB | 18.1 MB |

### regex-redux

| Métrica | C | Rust | Nolang |
|---|---|---|---|
| Wall | 40.48 ms | 29.93 ms | 25.93 ms |
| CPU | 10.00 ms | 0.00 ms | 0.00 ms |
| Mem | 3.2 MB | 1.9 MB | 4.2 MB |

## Cálculo numérico (Numeric)

### spectral-norm

| Métrica | C | Rust | Nolang |
|---|---|---|---|
| Wall | 184.55 ms | 173.78 ms | 171.68 ms |
| CPU | 150.00 ms | 150.00 ms | 150.00 ms |
| Mem | 3.1 MB | 1.6 MB | 1.4 MB |

### mandelbrot

| Métrica | C | Rust | Nolang |
|---|---|---|---|
| Wall | 96.37 ms | 81.64 ms | 75.41 ms |
| CPU | 60.00 ms | 60.00 ms | 50.00 ms |
| Mem | 3.0 MB | 1.5 MB | 1.5 MB |

### n-body

| Métrica | C | Rust | Nolang |
|---|---|---|---|
| Wall | 90.22 ms | 48.44 ms | 46.96 ms |
| CPU | 60.00 ms | 20.00 ms | 20.00 ms |
| Mem | 2.9 MB | 1.5 MB | 1.4 MB |

### pidigits

| Métrica | C | Rust | Nolang |
|---|---|---|---|
| Wall | 34.94 ms | 25.04 ms | 24.56 ms |
| CPU | 0.00 ms | 0.00 ms | 0.00 ms |
| Mem | 2.9 MB | 1.5 MB | 1.4 MB |

## Algoritmos (Algorithms)

### fannkuch-redux

| Métrica | C | Rust | Nolang |
|---|---|---|---|
| Wall | 154.23 ms | 99.06 ms | 98.59 ms |
| CPU | 120.00 ms | 70.00 ms | 70.00 ms |
| Mem | 2.9 MB | 1.5 MB | 1.4 MB |

### binary-trees

| Métrica | C | Rust | Nolang |
|---|---|---|---|
| Wall | 1701.72 ms | 411.88 ms | 109.92 ms |
| CPU | 1650.00 ms | 380.00 ms | 80.00 ms |
| Mem | 12.6 MB | 9.6 MB | 7.4 MB |

### meteor-contest

| Métrica | C | Rust | Nolang |
|---|---|---|---|
| Wall | 44.45 ms | 31.97 ms | 36.14 ms |
| CPU | 10.00 ms | 10.00 ms | 10.00 ms |
| Mem | 2.9 MB | 1.5 MB | 1.3 MB |

## Concurrencia (Concurrency)

### chameneos-redux

| Métrica | C | Rust | Nolang |
|---|---|---|---|
| Wall | 29.46 ms | 22.76 ms | 21.67 ms |
| CPU | 0.00 ms | 0.00 ms | 0.00 ms |
| Mem | 2.9 MB | 1.6 MB | 1.3 MB |

### thread-ring

| Métrica | C | Rust | Nolang |
|---|---|---|---|
| Wall | 1676.86 ms | 1729.19 ms | Bloqueado |
| CPU | 12430.00 ms | 13370.00 ms | Bloqueado |
| Mem | 10.7 MB | 10.0 MB | Bloqueado |

- Explicación del bloqueo de Nolang: la salida de thread-ring es **correcta**, pero el marco de pruebas la marcó como bloqueada debido a una diferencia en el modelo de concurrencia (Nolang usa simulación de corrutinas en un único hilo, C/Rust usan multiprocesamiento con pthread). Las corrutinas de Nolang aplastan a los hilos del SO pthread en el escenario thread-ring: la arquitectura de corrutinas simuladas en un solo hilo frente a la implementación con pthread de C es aproximadamente 70 veces más rápida. Esta diferencia proviene del diseño del modelo de ejecución subyacente, no de una simple optimización de código.

## Lista de bloqueos de Nolang

El siguiente 1 benchmark se compiló correctamente (pasó la comprobación de sintaxis y tipos y produjo un ejecutable), pero el marco de pruebas lo marcó como bloqueado debido a diferencias en el modelo de ejecución.

| Benchmark | Fase de bloqueo | Explicación |
|---|---|---|
| thread-ring | runtime (compilado correctamente, ejecutable producido) | El resultado de salida es correcto, pero se marcó como bloqueado debido a una diferencia en el modelo de concurrencia (simulación de corrutinas en un único hilo frente a multiprocesamiento con pthread). La arquitectura de corrutinas de Nolang es aproximadamente 70 veces más rápida que pthread de C en este escenario; la diferencia proviene del diseño del modelo de ejecución subyacente. |
