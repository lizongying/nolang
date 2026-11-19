---
sidebar_position: 5
---

# Exportación

Nolang usa la palabra clave `@` en el archivo `lib.no` de la raíz del paquete para declarar los elementos exportados. Los paquetes externos solo pueden acceder a estos símbolos exportados al importar mediante `#`.

> **Nota**: `@` solo exporta símbolos entre paquetes Nolang y no genera símbolos ABI de C. Para permitir que C / C++ / Rust / Go / Java / Python / C# llamen a Nolang, use en su lugar la anotación `#{cabi='symbol'}` junto con `no build --shared`. Consulte [Llamar a Nolang desde lenguajes anfitriones](./host-interop).

## Sintaxis

```no
@ path.func
[alias]
```

- `path` — Ruta del módulo (relativa a la raíz del paquete, empieza con `/`, sin la extensión `.no`)
- `func` — Nombre de la función/constante/enumeración que se va a exportar
- `alias` — Alias opcional que se usa al importar desde fuera

## Reglas

- Las sentencias de exportación **solo** se pueden escribir en el archivo `lib.no` de la raíz del paquete (la única excepción es la sección siguiente, «Alias invocables globales»)
- Un elemento exportado por línea
- Los elementos exportados solo pueden ser símbolos finales como funciones, constantes, enumeraciones
- **Se debe indicar el nombre del símbolo**: `@ std/txt` (solo la ruta, sin `func` ni `alias`) es un error (`lgxbfk9a`).
  Esta forma no declara nada — todos los consumidores asumen que hay un nombre de símbolo —, por lo que ya no se ignora silenciosamente.
  Tampoco sirve para «vincular un tipo a un módulo»: los tipos no tienen una capa de nombre global, y el alias de `@` solo tiene sentido para símbolos invocables
- Las estructuras, enumeraciones y otros tipos referenciados por funciones exportadas se exportan **automáticamente**, sin necesidad de declararlos manualmente
- Si una función exportada no existe en el módulo, el LSP muestra un error

## Ejemplo

```no
; lib.no - archivo de exportación de la raíz del paquete
@ /src/utils.greet a
@ /src/utils.hello b
@ /src/math.pi
```

```no
; src/utils.no
; Definir las funciones exportadas
greet = (name str) {
    print('Hello, ' - name)
}

hello = () {
    print('Hi')
}
```

## Alias invocables globales

La misma sintaxis, escrita en **`std/global.no`** (en lugar del `lib.no` de un paquete), tiene otro significado:
no filtra los elementos exportados, sino que declara un **segundo nombre global invocable**: el nombre simple `<alias>` es directamente
el `func` del módulo `path`, con el mismo efecto que escribir `<path>.<func>(...)` completo.

```no
; std/global.no —— dos formas de escribirlo, mismo significado
@ std/err.new err; forma de exportación
err = err.new; forma de asignación
```

```no
e = err('disk full', code.io); equivale a e = err.new('disk full', code.io)
m = e.msg(); 'disk full'
```

Reglas:

- Solo `std/global.no` tiene este significado; `@` en un `lib.no` sigue siendo un filtro de exportación
- Las dos formas son **equivalentes**; el compilador las normaliza a una misma declaración, y escribir ambas a la vez no genera conflicto
- La forma de asignación **solo es válida en `global.no`**. En un programa normal, `x = mod.fn` es una
  variable que contiene un valor de función (utilizable como llamada indirecta), no se trata como un alias global
- El lado derecho de la asignación debe ser una referencia simple `<módulo>.<función>`: no una llamada, no un
  literal, no una lectura de campo ni una vinculación con tipo. Si el objetivo no es una función, la declaración deja de tener efecto
  (las declaraciones de constantes como `PI = math.PI` no se ven afectadas)
- El alias solo actúa en posiciones de **llamada con nombre simple**. Las llamadas cualificadas no se ven afectadas:
  `io.err(...)` sigue siendo el escritor de stderr del módulo `io` y no es capturado por el alias `err`
- Tampoco afecta a un módulo que llama a su propia función del mismo nombre (excepción de automódulo)
- La **lista de parámetros** del alias proviene de la función objetivo; el **tipo de retorno** lo determina la función objetivo en el punto de llamada
- El alias no entra en conflicto con la variante `err` de la opción integrada: en posiciones donde se espera el tipo `?T`
  (`x = err('m')` después de `x ?i64`, una rama de match `err ->`, funciones que devuelven `?T`),
  `err(...)` sigue siendo el constructor de la variante err de la opción (tag 2)

## Importar símbolos exportados

Los paquetes externos solo pueden acceder a los elementos exportados declarados en `lib.no` al importar mediante `#`:

```no
; Importar el alias a (corresponde a package-name.utils.greet)
# package-name.utils.greet a
; O usar directamente el nombre de la función
# package-name.utils.greet
```

## Compatibilidad con LSP

- **Ir a la definición**: haga clic en el nombre de una función exportada o en un alias en `lib.no` para saltar a su definición en el archivo de módulo correspondiente
- **Autocompletado**: sugiere automáticamente rutas de archivo y nombres de funciones al escribir `@` y una ruta
- **Diagnóstico de errores**: muestra un error cuando una función exportada no existe en el módulo
