---
sidebar_position: 4.2
---

## Async (asíncrono)

Nolang tiene una única sintaxis superficial para la concurrencia: el grupo de corrutinas `awy { ... }`, dentro del cual se generan tareas con `co`.
La palabra clave `run` se eliminó (ahora es un identificador normal); `awy <handle>` y los grupos de corrutinas de bloque desnudo en posición de sentencia también se han eliminado;
**`co` fuera de un grupo es un error de compilación** (`E_CO_OUTSIDE_GROUP`).

`co` solo inicia un **procedimiento**: quien recibe la llamada no puede declarar un valor de retorno (el `self` implícito de un método no cuenta),
por lo que los resultados siempre van por un canal: el procedimiento envía el valor al canal y luego llama a `close()`, y quien llama lo vacía fuera del grupo.

### Grupos de corrutinas

```no
; Procedimiento: solo escribe en el canal, no declara valor de retorno
produce = (ch chan[i64], v i64) {
    ch <- v
    ch.close()
}

ch1 chan[i64] = with-cap(1)
ch2 chan[i64] = with-cap(1)

; Concurrencia: dos tareas se ejecutan juntas, el grupo espera al final
awy {
    co produce(ch1, 1)
    co produce(ch2, 2)
}

r1 = 0
<- ch1: { ok -> r1 = it }
r2 = 0
<- ch2: { ok -> r2 = it }

; Orden: el siguiente spawn usa el resultado del anterior como argumento, la «lectura del canal» sirve de barrera
awy {
    co produce(ch1, r1)
}
```

Lo que se obtiene en `ch1` / `ch2` son valores de resultado, no identificadores de tarea. Consulte `lang/syntax.md`, sección «Programación asíncrona».

### `co` asíncrono sin color (colorless)

`co` es una sintaxis sin color (colorless): escribe el nombre sin teñir `produce` y el compilador genera automáticamente por monomorfización
la variante `produce-async` y la ejecuta internamente como una corrutina sin pila teñida. No escriba a mano el sufijo `-async` en el código de aplicación.

Dos formas:

- `co f(args)` — generación de un procedimiento; `f` no debe declarar un valor de retorno (se exceptúa el `self` implícito de un método).
- `co { ... }` — generación de una clausura; las variables libres del bloque se capturan por valor (instantánea en el momento del spawn).

`co` no es un valor, debe constituir una sentencia por sí solo: `r = co f()` y `print(co f())` producen `c0nostmt`.

Reglas:

- **Monomorfización**: `co produce(ch, 1)` genera automáticamente `produce-async` (copia el cuerpo de `produce` y lo renombra). Si `produce` también se llama de forma síncrona, ambas variantes coexisten.
- **Transitividad**: si un procedimiento contiene a su vez `awy { co child(n) }`, ese procedimiento también se monomorfiza a `-async`.
- **Reducción interna (lowering)** (no es sintaxis de usuario): cada `co` se convierte en `OpRun` y al final del grupo se inserta un `OpAwait`.

### Cancelación

`cancelled()` se puede usar dentro de una función corrutina para una autocomprobación cooperativa. `cancel(h)` es una operación de identificador de bajo nivel;
la nueva sintaxis no expone identificadores de tarea y el código de aplicación no la utiliza.

> **Nota:** la cancelación es cooperativa, no preventiva. Una llamada bloqueante prolongada (como una petición de red) no se puede interrumpir por la fuerza;
> la tarea se detiene realmente en el siguiente punto de control cooperativo (una llamada a `cancelled()` o la siguiente programación del bucle de eventos).

#### Hacer la cancelación uno mismo con un canal (recomendado)

La nueva sintaxis no tiene `ch.cancel()` y los canales tampoco tienen variante `cancel`. Cuando se necesite «una señal externa para que el worker termine»,
**mantenga usted mismo un canal y use `close()` como señal de cancelación**: esta es la única semántica de cancelación de `chan[T]`.
En los tres estados del bucle de recepción, `nil` significa «cerrado y vacío», que es justamente la rama de cancelación; `err` solo indica un identificador no inicializado,
no se debe usar para cancelar.

```no
; Canal de control: cerrar significa cancelar
stop chan[i64] = with-cap(0)

worker = (stop chan[i64]) {
    <- stop: {
        ok  -> { }              ; Mensaje recibido (no se usa en este ejemplo)
        nil -> print('cancel')  ; Tras stop.close() se llega aquí
        err -> { }              ; Identificador no inicializado
    }
}

stop.close()
awy {
    co worker(stop)
}
; Salida: cancel
```

Para disparar la cancelación durante la ejecución desde otra tarea, ponga también la acción de cierre dentro del grupo (no se puede hacer `close()` después de `awy`:
el grupo espera a todos al final y, si el worker ya está aparcado en un canal vacío, se produce un interbloqueo). Tenga en cuenta que `<- ch: { ... }` es un **bucle de recepción**:
tras recibir un valor continúa esperando el siguiente, por lo que quien avisa debe llamar a `close()` para que el canceller pueda salir del bucle y ejecutar
`stop.close()`:

```no
stop chan[i64] = with-cap(0)
awake chan[i64] = with-cap(1)

worker = (stop chan[i64]) {
    <- stop: { ok -> { }  nil -> print('cancel')  err -> { } }
}

canceller = (awake chan[i64], stop chan[i64]) {
    <- awake: { ok -> { }  nil -> { }  err -> { } }   ; Esperar notificación; tras recibirla hay que esperar el close para salir del bucle
    stop.close()                                       ; Esta línea es la señal de cancelación
}

notify = (awake chan[i64]) {
    awake <- 1
    awake.close()                                      ; Hacer que el bucle de recepción de canceller llegue a nil
}

awy {
    co worker(stop)
    co canceller(awake, stop)
    co notify(awake)
}
; Salida: cancel
```

Si solo quiere comprobar al **inicio de cada paso del worker** si se ha solicitado la detención, use `cancelled()`, que es más directo;
pero solo permite autocomprobación y no hay identificador externo con que avisar: para el caso «otro emite la señal», siga la convención del canal descrita arriba.

---
