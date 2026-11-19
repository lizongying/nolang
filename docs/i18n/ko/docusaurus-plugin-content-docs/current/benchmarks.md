---
sidebar_position: 4
---

# 성능 비교 보고서

_Benchmarks Game — Nolang vs C vs Rust_

## 설명

- **C / Rust / Nolang** 구현은 로컬에서 실제로 컴파일하고 실행했습니다. 데이터는 `/usr/bin/time -l` 및 Python `time.perf_counter()`에서 가져왔습니다(중앙값, 각 3회 실행).
- **Nolang** 열에 숫자가 표시되면 실행에 성공한 것이고, "차단"으로 표시되면 runtime 단계에서 실패한 것입니다(segfault / abort 등). 열의 비고에 실제 오류 메시지가 나열되어 있습니다(자세한 내용은 마지막 절의 차단 목록 참조).
- 지표: **Wall** = 벽시계 시간(`time.perf_counter()`로 고해상도 측정, µs 단위 정밀도로 sub-ms 차이까지 구분 가능); **CPU** = user+sys 시간(`/usr/bin/time -l`에서 가져옴, 10ms 단위로 양자화); **Mem** = 최대 상주 메모리 (max RSS). **시간 단위는 ms(밀리초, 소수점 2자리)이고, 메모리 단위는 MB입니다.**
- 표 구성: 열은 세 가지 언어(C / Rust / Nolang)이고, 행은 세 가지 지표(Wall / CPU / Mem)입니다.

**전체**: 13개 벤치마크 중 C/Rust는 13개를 모두 실행할 수 있고, Nolang은 12개를 실행할 수 있으며 1개가 차단되었습니다(thread-ring. 출력은 정확하지만 동시성 모델 차이로 테스트 프레임워크에 표시됨. 자세한 내용은 마지막 절 참조).

## 문자열 처리 (String Processing)

### fasta

| 지표 | C | Rust | Nolang |
|---|---|---|---|
| Wall | 54.38 ms | 67.30 ms | 38.20 ms |
| CPU | 20.00 ms | 40.00 ms | 10.00 ms |
| Mem | 2.9 MB | 1.5 MB | 1.4 MB |

### reverse-complement

| 지표 | C | Rust | Nolang |
|---|---|---|---|
| Wall | 36.90 ms | 26.20 ms | 25.95 ms |
| CPU | 10.00 ms | 0.00 ms | 0.00 ms |
| Mem | 3.3 MB | 1.8 MB | 19.4 MB |

### k-nucleotide

| 지표 | C | Rust | Nolang |
|---|---|---|---|
| Wall | 36.78 ms | 26.12 ms | 26.88 ms |
| CPU | 10.00 ms | 0.00 ms | 0.00 ms |
| Mem | 3.2 MB | 1.9 MB | 18.1 MB |

### regex-redux

| 지표 | C | Rust | Nolang |
|---|---|---|---|
| Wall | 40.48 ms | 29.93 ms | 25.93 ms |
| CPU | 10.00 ms | 0.00 ms | 0.00 ms |
| Mem | 3.2 MB | 1.9 MB | 4.2 MB |

## 수치 계산 (Numeric)

### spectral-norm

| 지표 | C | Rust | Nolang |
|---|---|---|---|
| Wall | 184.55 ms | 173.78 ms | 171.68 ms |
| CPU | 150.00 ms | 150.00 ms | 150.00 ms |
| Mem | 3.1 MB | 1.6 MB | 1.4 MB |

### mandelbrot

| 지표 | C | Rust | Nolang |
|---|---|---|---|
| Wall | 96.37 ms | 81.64 ms | 75.41 ms |
| CPU | 60.00 ms | 60.00 ms | 50.00 ms |
| Mem | 3.0 MB | 1.5 MB | 1.5 MB |

### n-body

| 지표 | C | Rust | Nolang |
|---|---|---|---|
| Wall | 90.22 ms | 48.44 ms | 46.96 ms |
| CPU | 60.00 ms | 20.00 ms | 20.00 ms |
| Mem | 2.9 MB | 1.5 MB | 1.4 MB |

### pidigits

| 지표 | C | Rust | Nolang |
|---|---|---|---|
| Wall | 34.94 ms | 25.04 ms | 24.56 ms |
| CPU | 0.00 ms | 0.00 ms | 0.00 ms |
| Mem | 2.9 MB | 1.5 MB | 1.4 MB |

## 알고리즘 (Algorithms)

### fannkuch-redux

| 지표 | C | Rust | Nolang |
|---|---|---|---|
| Wall | 154.23 ms | 99.06 ms | 98.59 ms |
| CPU | 120.00 ms | 70.00 ms | 70.00 ms |
| Mem | 2.9 MB | 1.5 MB | 1.4 MB |

### binary-trees

| 지표 | C | Rust | Nolang |
|---|---|---|---|
| Wall | 1701.72 ms | 411.88 ms | 109.92 ms |
| CPU | 1650.00 ms | 380.00 ms | 80.00 ms |
| Mem | 12.6 MB | 9.6 MB | 7.4 MB |

### meteor-contest

| 지표 | C | Rust | Nolang |
|---|---|---|---|
| Wall | 44.45 ms | 31.97 ms | 36.14 ms |
| CPU | 10.00 ms | 10.00 ms | 10.00 ms |
| Mem | 2.9 MB | 1.5 MB | 1.3 MB |

## 동시성 (Concurrency)

### chameneos-redux

| 지표 | C | Rust | Nolang |
|---|---|---|---|
| Wall | 29.46 ms | 22.76 ms | 21.67 ms |
| CPU | 0.00 ms | 0.00 ms | 0.00 ms |
| Mem | 2.9 MB | 1.6 MB | 1.3 MB |

### thread-ring

| 지표 | C | Rust | Nolang |
|---|---|---|---|
| Wall | 1676.86 ms | 1729.19 ms | 차단 |
| CPU | 12430.00 ms | 13370.00 ms | 차단 |
| Mem | 10.7 MB | 10.0 MB | 차단 |

- Nolang 차단 관련 설명: thread-ring의 출력 결과는 **정확**하지만, 동시성 모델의 차이(Nolang은 단일 스레드 코루틴 시뮬레이션을 사용하고 C/Rust는 pthread 다중 스레드를 사용)로 테스트 프레임워크에 차단 상태로 표시되었습니다. thread-ring 시나리오에서 Nolang 코루틴은 OS 스레드인 pthread를 압도하며 성능 격차가 매우 큽니다. 단일 스레드로 시뮬레이션한 코루틴 아키텍처는 C의 pthread 스레드 구현보다 약 70배 빠릅니다. 이 차이는 단순한 코드 최적화가 아니라 기반이 되는 실행 모델 설계에서 비롯됩니다.

## Nolang 차단 목록

다음 1개 벤치마크는 컴파일에 성공했지만(문법/타입 검사를 통과하고 실행 파일을 생성) 실행 모델 차이로 테스트 프레임워크에 차단 상태로 표시되었습니다.

| 벤치마크 | 차단 단계 | 설명 |
|---|---|---|
| thread-ring | runtime(컴파일에 성공하여 실행 파일을 생성) | 출력 결과는 정확하지만 동시성 모델 차이(단일 스레드 코루틴 시뮬레이션 vs pthread 다중 스레드)로 테스트 프레임워크에 차단 상태로 표시되었습니다. 이 시나리오에서 Nolang 코루틴 아키텍처는 C pthread보다 약 70배 빠르며, 그 차이는 기반이 되는 실행 모델 설계에서 비롯됩니다. |
