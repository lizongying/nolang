---
sidebar_position: 4.4
---

## Прочее

### unicode — поддержка Unicode

Функции, связанные с Unicode, распределены по модулям `char` и `str`:

- Классификация символов (`is-letter`, `is-digit`, `is-upper` и др.) → см. модуль `char`
- Кодирование/декодирование UTF-8 (`char.to-bytes`, `char.to-str`) → см. модуль `char`
- Подсчёт рун в строке (`str.count`) → см. модуль `str`

### uuid — генерация и разбор UUID v4

```no
out = uuid.new-v4(state); создать UUID v4
out-n = uuid.to-str(out); преобразовать в строку в нижнем регистре (метод)
out-n = uuid.to-str-upper(out); преобразовать в строку в верхнем регистре (метод)
ok = uuid.from-str(s, sn, out); разобрать из строки (с дефисами/без)
ok = uuid.parse-with-dashes(s, pos, out); разбор с дефисами
ok = uuid.parse-no-dashes(s, pos, out); разбор без дефисов
ok = uuid.validate(); проверить формат UUID (метод)
v = uuid.version(); получить версию (метод)
v = uuid.variant(); получить вариант (метод)
yes = uuid.is-nil(); является ли nil (метод)
yes = uuid.eq(b); сравнение на равенство (метод)
r = uuid.cmp(b); сравнение (метод)
uuid.nil-uuid(out); вернуть nil UUID
```

### bigint — целые числа произвольной точности

```no
; Тип
bigint {
    sign i64
    limbs []i64
    len i64
}

; Создание
out = bigint.from-i64(v)
out = bigint.from-u64(v)
out = bigint.zero()
out = bigint.one()
out = bigint.copy(a)

; Сравнение
r = bigint.cmp(a, b)
r = bigint.eq(a, b)
r = bigint.is-zero(a)
r = bigint.is-neg(a)
r = bigint.is-pos(a)

; Операции (+ - * / % перегружены для bigint, см. «Синтаксис > Перегрузка операторов»)
c = a + b; эквивалентно a._add(b)
c = a - b; эквивалентно a._sub(b)
c = a * b; эквивалентно a._mul(b)
q = a / b; эквивалентно a._div(b), деление на ноль возвращает ноль согласно div-mod
r = a % b; эквивалентно a._mod(b)

; Смешанные операнды: параметры перегрузки операторов — объединение `bigint | i64`, на этапе компиляции выбирается ветка по типу правого операнда (мономорфизация),
; `a + 1` / `a * 256` и т. п. работают изначально (тег времени выполнения отсутствует). Только «левый операнд — bigint», `1 + a` не поддерживается.
c = a + 1; bigint ⊕ i64
c = a * 256; bigint ⊕ i64
q, r = a.div-mod(b); одновременно частное и остаток
c = a.pow(n)
r = base.mod-pow(exp, mod)

; Теория чисел
g = a.gcd(b)
l = a.lcm(b)

; Сдвиги
bigint.shl(a, n, c)
bigint.shr(a, n, c)

; Преобразование в строку
n = bigint.to-str(a, out)
out = bigint.from-str(s, sn)
n = bigint.to-hex(a, out)
out = bigint.from-hex(s, sn)
; быстрые пути для малых целых (_add-i64 / _mul-i64 / _div-i64 / _mod-i64) являются приватными для модуля bigint,
; для смешанных операций всегда используйте перегрузки + - * / % выше
```

### err — обработка ошибок

Структурированный тип ошибки и вспомогательные функции:

```no
; Перечисление кодов ошибок
code {
    ok,
    not-found,
    permission,
    io,
    timeout,
    parse,
    invalid,
    overflow,
    unknown,
}

; Структура
err {
    code code
    msg str
    file str
    line i64
    col i64
}

; Функции
e = err.new(msg, code.io); создать ошибку
e = err.from-errno(errno); создать из C errno
yes = e.is(code.io); проверить код ошибки
msg = e.msg(); получить сообщение об ошибке
c = e.code(); получить код ошибки
s = e.format(); отформатировать в строку
```

Эта структура `err` и есть нагрузка варианта `err` встроенного option (`option { ok(v t), nil, err(e err.err) }`):
при сопоставлении `?t` неявно связанное `it` в ветке `err ->` — это она, а не `str`.

```no
x = f(); f возвращает ?i64
x: {
    ok -> n = it

    nil -> n = 0

    err -> {
        log.error(it.msg()); только сообщение
        print(it.format()); 'code.unknown: ... at file:line:col'
        it.is(code.io) -> print('io error')
    }
}
```

### bool — логический тип

```no
#{fmt-skip}
bool.to-str() (out str)     ; true→"true", false→"false" (метод)
```

### enter / leave — хуки жизненного цикла

```no
; выполняется при запуске
enter {
    enter()
}

; выполняется при выходе
leave {
    leave()
}
```

### magic — определение типов файлов

Определение типа файла по расширению и магическим байтам (magic bytes), без зависимости от libmagic:

```no
kind = magic.detect-type(path); определить тип файла
; возвращает строку-описание типа, например 'PNG image', 'ELF executable', 'ASCII text', 'directory', 'unknown', 'data'
```

---
