---
sidebar_position: 3
---

# Biblioteca estándar

La biblioteca estándar de Nolang (`src/std/`) contiene más de 80 módulos, que cubren formato, matemáticas, cadenas, estructuras de datos, codificación y decodificación, cifrado, compresión, operaciones con archivos, abstracciones de E/S, corrutinas asíncronas, detección de tipos de archivo, etc.

Uso: `# std/xxx` (los módulos principales no requieren importación).

> **La sintaxis antigua `use std/xxx` todavía funciona, pero está obsoleta (deprecated); se recomienda usar la nueva sintaxis `# std/xxx`.**

> **Nota: Todos los ejemplos de código de este documento siguen la regla de «una instrucción por línea»: está prohibido usar punto y coma `;` o comas `,` para escribir varias instrucciones en una misma línea.** Por ejemplo, `out = from-i64(v), out = from-u64(v)` es incorrecto y debe dividirse en varias líneas.

---

## Categorías de módulos

- [Tipos básicos](basic-types)
- [Biblioteca principal](core-library)
- [Sistema operativo y archivos](os-and-files)
- [Hora y fecha](time-and-date)
- [Registro](logging)
- [Estructuras de datos](data-structures)
- [Base de datos](database)
- [Codificación](encoding)
- [Archivos comprimidos](archives)
- [Criptografía y hash](crypto)
- [Intercambio de datos](data-exchange)
- [Otros](others)
- [Resumen de módulos](module-overview)
