---
sidebar_position: 4.3
---

## Global

### global — Funciones integradas globales

Funciones que se pueden llamar sin prefijo de módulo. Solo existen las siguientes 7 funciones globales; todas las demás llamadas entre módulos deben llevar un prefijo de módulo (p. ej. `fs.read`, `os.get-env`):

```no
; Constructores de capacidad/longitud (el tipo se infiere del lado izquierdo de la asignación)
s str = with-cap(256); Preasignar str de 256 bytes (len=0)
v []i64 = with-cap(100); Preasignar segmento de 100 elementos (len=0)
s str = with-len(10); str de longitud 10
v []i64 = with-len(100); segmento de longitud 100
v []i64 = with-cap-len(200, 100); segmento con capacidad 200 y longitud 100

; También se puede llamar como método de str/vec:
s = ''.with-cap(256)
s = ''.with-len(10)
s = ''.with-len-cap(10, 256)
v = [].with-cap(100)
v = [].with-len(100)
v = [].with-len-cap(100, 200)

; Salida/formato (cadenas de formato con nombre {name:spec})
print('x={x}'); Formato con nombre, stdout + salto de línea
eprint('err {x}'); Formato con nombre, stderr + salto de línea
s = format('x={x}'); Devuelve la cadena formateada
print(a, b, c); Varios argumentos: separados por espacios, un solo salto de línea al final
print('result={val}', 42, 'result={val}'); Con varios argumentos, cada literal es su propia plantilla

; ({val} toma el valor del ámbito del punto de llamada, no es una cadena de formato estilo C)
;
; Un campo puede ser una variable / ruta de campo / método sin argumentos / índice, y se pueden encadenar por niveles:
print('{user.name} {user.name.len()} {tags[0]}')
print('{m[\'k\']} {m[\'k\'].len()}'); acceso a map (escapar la comilla con \' en la plantilla, o escribir m["k"])

; Solo se aplica el azúcar sintáctico de «quitar un sufijo por nivel»: no se admiten métodos con argumentos ({s.slice(0)}) ni expresiones arbitrarias ({a + 1}).

; Control del proceso
exit(0); Finalizar el proceso con código de estado 0 (antes os.exit, ahora global)
```

---
