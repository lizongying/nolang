---
sidebar_position: 1
---

# Introducción a Nolang

Nolang es un lenguaje de programación de sistemas experimental, seguro en la gestión de memoria, sin GC, con semántica intuitiva y sintaxis mínima.

## Características principales

- **Seguridad de memoria**: Sin recolección de basura; gestión automática de memoria.
- **Rendimiento potente**: Optimización en tiempo de compilación, sin GC.
- **Sintaxis mínima**: Menos palabras clave; sin punteros, propiedad ni ciclos de vida como carga mental implícita; sintaxis más simple.
- **Orientado a la intención**: Respeta la intención del desarrollador; la semántica se alinea con la intuición humana.
- **Sin fallos**: Elimina las rutas de fallo implícitas.

## Inicio rápido

```no
; ¡Hola, mundo!
; No se necesita punto de entrada main
print('Hello, Nolang!')

; Declaración de variable
count i64

; Definición de función
add = (a i64, b i64) (result i64) {
    result = a + b
}

; Método de la biblioteca estándar, se puede llamar directamente
c = math.max(a, b)

; Estructura
user {
    name str
    age i64
}

; Método
user.greet = () {
    print('Hello, {.name}')
}

u = user {
    name: 'Alice'
    age: 30
}

u.greet()
```

## Casos de uso

- [notools](https://github.com/lizongying/notools) — Una colección de herramientas de línea de comandos comunes de Unix implementadas en Nolang, que incluye cat, ls, grep, wc, head, tail y más. Demuestra las capacidades reales de programación de sistemas de Nolang.
