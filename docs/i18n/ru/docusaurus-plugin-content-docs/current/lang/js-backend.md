---
sidebar_position: 8
---

# JS-бэкенд

Nolang поддерживает прямую компиляцию исходного кода `.no` в JavaScript без LLVM-инструментария. JS-бэкенд использует стратегию **стирания типов** (type erasure): все аннотации типов Nolang не сохраняются в выводе JS, генерируется только поведение времени выполнения.

## Быстрый старт

```bash
# скомпилировать в JS (вывод в dist/<name>.js)
no build --js main.no

# режим браузера: создать JS + HTML-обвязку
no build --js --browser main.no

# запуск: скомпилировать в JS и выполнить через node
no run --js main.no

# режим браузера: скомпилировать и открыть в браузере по умолчанию
no run --js --browser main.no
```

## Принципы проектирования

Основные принципы JS-бэкенда:

- **Стирание типов**: JS — динамически типизированный язык, поэтому аннотации типов Nolang (`int`/`str`/`bool`/`vec[T]`/`[N]T`/`?T` и др.) полностью не сохраняются в выводе JS
- **Без LLVM**: JavaScript испускается напрямую из AST, без зависимости от clang/LLVM
- **Две среды**: поддерживаются Node.js (по умолчанию) и браузер (`--browser`)
- **Платформенные аннотации**: `#{js}` и `#{js-browser}` управляют видимостью кода для платформы

Реализация находится в каталоге `src/build/js/`:

| Файл | Назначение |
| --- | --- |
| `generator.go` | основная логика codegen AST → JavaScript |
| `expr.go` | генерация выражений |
| `stmt.go` | генерация операторов |
| `builtin.go` | отображение встроенных функций (print→console.log и др.) |
| `html_wrapper.go` | HTML-шаблон для режима браузера |

## Аргументы командной строки

| Аргумент | Описание |
| --- | --- |
| `--js` | использовать JS-бэкенд (испустить JavaScript, минуя LLVM) |
| `--browser` | создать вывод для браузера (HTML + JS, требуется `--js`) |
| `--esm-cdn <url>` | переопределить CDN-префикс import map для браузерного ESM (по умолчанию `https://esm.sh`) |
| `--js-runtime <cmd>` | JS-среда выполнения для `no run --js` (по умолчанию `node`; можно заменить на `bun`, `deno` и др., аргументы допустимы) |
| `-o <path>` | задать путь вывода |

### Сборка

```bash
no build --js main.no                    # вывод dist/main.js
no build --js -o app.js main.no          # задать путь вывода
no build --js --browser main.no          # вывод dist/main.js + dist/main.html
```

### Запуск

```bash
no run --js main.no                      # скомпилировать в JS и выполнить через node
no run --js --browser main.no            # скомпилировать в браузерный JS + HTML и открыть браузер
```

#### Выбор JS-среды выполнения

`--js-runtime` задаёт среду выполнения артефакта, **по умолчанию `node`** (Node.js). Подойдёт любая среда, может запускать ES module:

```bash
no run --js --js-runtime bun main.no             # Bun
no run --js --js-runtime deno run main.no        # Deno (первый токен — исполняемый файл, остальные — аргументы перед скриптом)
no run --js --js-runtime "deno run --allow-read" main.no
no run --js --js-runtime "node --enable-source-maps" main.no
no run --js --js-runtime /opt/js/bin/node main.no   # задать абсолютный путь
```

- Первый разделённый пробелом токен спецификации — исполняемый файл, остальные токены ставятся перед путём скрипта; аргументы самой программы по-прежнему пишутся после имени файла `.no`.
- По умолчанию (без `--js-runtime`) используется `node`; если в системе есть только бинарник `nodejs` в стиле Debian/Ubuntu, происходит автоматический откат на него. Явно указанная среда (`bun`, абсолютный путь, `node --flags`) никогда не откатывается — если её нет, возникает ошибка.
- Если среды нет в `PATH`, сразу возникает ошибка с предложением заменить её через `--js-runtime` (молчаливого отката на node нет).
- ESM-импорты (`# npm/…`) работают и в этих средах: `no run --js`линкует `node_modules` проекта во временный каталог, и node/bun/deno разрешают пакеты по одинаковым правилам.

## Платформенные аннотации

JS-бэкенд вводит два дополнительных ключа платформенных аннотаций:

| Ключ | Сценарий совпадения |
| --- | --- |
| `#{js}` | JS-бэкенд (Node.js и браузер) |
| `#{js-browser}` | режим браузера (вместе с `--browser`) |

```no
; сохраняется только при компиляции через JS-бэкенд
#{js}
js-helper = () {
    print('JS only code')
}

; сохраняется только в режиме браузера
#{js-browser}
print('running in browser mode')

; сохраняется только в нативном бэкенде (исключается при компиляции в JS)
#{mac-arm64}
print('running on macOS ARM64')
```

## Отображение встроенных функций

JS-бэкенд отображает встроенные функции Nolang в их JavaScript-эквиваленты:

| Nolang | JavaScript | Описание |
| --- | --- | --- |
| `print(x)` | `console.log(x)` | автоматический перевод строки |
| `eprint(x)` | `console.error(x)` | вывод в stderr |
| `format(...)` | конкатенация строк | упрощение v1: склейка через `"" +` |
| `len(x)` | `x.length` | для строк и массивов |
| `with-len(n)` | `new Array(n)` | создать массив заданной длины |

## Стандартная библиотека JS-бэкенда

Каталог `src/js/` содержит модули, специально предназначенные для JS-бэкенда; все они несут аннотацию `#{js}`:

### Браузерные API

#### `js/dom` — работа с DOM

```no
# js/dom

; создать элемент
el = dom.create-element('div')
heading = dom.create-element('h2')

; найти элемент
el = dom.get-element-by-id('my-id')
el = dom.query-selector('.my-class')

; получить body
body = dom.body()

; методы элемента (отображаются в builtin.go)
el.set-text('Hello')
el.set-style('color', 'red')
el.set-attr('data-id', '42')
el.append-child(child)
```

#### `js/canvas` — рисование Canvas 2D

```no
# js/canvas

canvasEl = dom.create-element('canvas')
canvasEl.set-attr('width', '240')
canvasEl.set-attr('height', '140')
body.append-child(canvasEl)

ctx = canvas.get-context-2d(canvasEl)
ctx.set-fill('red')
ctx.fill-rect(10, 10, 60, 60)
ctx.set-stroke('orange')
ctx.begin-path()
ctx.move-to(120, 80)
ctx.line-to(80, 130)
ctx.line-to(160, 130)
ctx.fill()
ctx.stroke()
```

#### `js/events` — обработка событий

```no
# js/events

btn = dom.create-element('button')
btn.set-text('Click me')
body.append-child(btn)

; анонимный колбэк
events.on-click(btn, () {
    print('button was clicked!')
})

; загрузка страницы
events.on-load(() {
    print('page loaded')
})
```

#### `js/storage` — localStorage

```no
# js/storage

storage.set-item('key', 'value')
val = storage.get-item('key')
storage.remove-item('key')
storage.clear()
```

#### `js/location` — Location API

```no
# js/location

href = location.href()
search = location.search()
path = location.path()
location.redirect('https://example.com')
```

#### `js/history` — History API

```no
# js/history

history.back()
history.forward()
history.push('/page2')
n = history.length()
```

#### `js/animation` — кадры анимации

```no
# js/animation

id = animation.request-frame(() {

    ; колбэк на каждый кадр
    print('frame')
})
animation.cancel-frame(id)
```

### Node.js API

#### `js/fs-read-file` / `js/fs-write-file` — чтение и запись файлов

```no
# js/fs-read-file
# js/fs-write-file

data = fs-read-file('input.txt')
fs-write-file('output.txt', data)
```

#### `js/http-fetch` — HTTP-запросы

```no
# js/http-fetch

data = http-fetch('https://api.example.com/data')
print(data)
```

#### `js/process-exit` — выход из процесса

```no
# js/process-exit

process-exit(0)
```

#### `js/fetch` — Fetch API（async）

```no
# js/fetch

; асинхронное получение данных по URL
data = fetch.async('https://api.example.com/data')
json-data = fetch.json-async('https://api.example.com/data')
```

## Режим браузера

При использовании `--browser` компилятор создаёт:

1. **JS-файл**: скомпилированный исходный код JavaScript
2. **HTML-файл**: HTML-обвязка, ссылающаяся на JS, с div `#nolang-output`

Особенности HTML-обвязки:

- вывод `print()` перенаправляется в `<div id="nolang-output">`
- страница содержит базовые стили (шрифт, рамка, область вывода моноширинным шрифтом)
- артефакт подключается через `<script src="name.js">`

Путь вывода по умолчанию — каталог `dist/`.

## Импорт сторонних ESM-модулей

JS-бэкенд может напрямую подключать пакеты из экосистемы npm (эквивалент `import` в TypeScript) — **новый оператор `import` не требуется**: используется существующая директива `#` use с зарезервированным префиксом `npm/`:

```no
# npm/lodash; весь объект модуля: lodash.get(x, 'a.b')
# npm/lodash.get as get; один именованный экспорт: get(x, 'a.b')
# npm/lodash.default as d; экспорт пакета по умолчанию: d(props)
# npm/@scope/pkg; scoped-пакет
# npm/lodash/fp.get as fpg; импорт по подпути
```

Правила:

- **Обязательно JS-бэкенд**. LLVM и Direct-WASM-бэкенды при `# npm/...` сразу дают ошибку с предложением использовать `no build --js`.
- **Пакет должен быть объявлен** в `dependencies` (или `devDependencies`) файла `package.noml`, иначе сборка завершается неудачей. Импорт по подпути (`lodash/fp`) допустим, если родительский пакет (`lodash`) есть в манифесте.
- **Использование с коротким именем не меняется**: после `# npm/lodash` можно писать `lodash.get(...)`; при именованном импорте (`as get`) вызываем напрямую `get(...)`.
- **v1: типы не пробрасываются**: импортированные связывания не несут типов Nolang (checker не изменяется), поэтому проверка сигнатур не выполняется; при коллизии с локальной функцией с тем же именем импорт автоматически переименовывается (суффикс `$esm`).
- **При совпадении имени со встроенным модулем приоритет у импорта**: после `# npm/math` запись `math.sin(x)` обращается к пакету npm `math`, а не к `Math.sin`.

### Запуск в Node.js

Артефакт — стандартный ES module (операторы import, и `import` может встречаться только в области module). `no build --js` создаёт рядом с артефактом `package.json` (`{"type": "module"}`, соглашение npm), чтобы node выполнял его как ESM; если в каталоге уже есть `package.json` не для ESM, предлагается использовать `-o out.mjs`.

```bash
npm install lodash                    # сначала установить пакет
no build --js main.no                 # создать dist/main.js (+ dist/package.json)
node dist/main.js                     # выполнить как ESM
no run --js main.no                   # собрать во временный каталог и автоматически выполнить через node
```

`no run --js`линкует `node_modules` проекта во временный каталог (node разрешает пакеты вверх от каталога скрипта); если пакет отсутствует, сразу возникает ошибка с подсказкой `npm install lodash`; `no build --js` при отсутствии пакета выдаёт лишь warning (артефакт корректен, пакет нужен только во время выполнения).

### Запуск в браузере

В режиме `--browser` артефакт HTML превращается в `<script type="module">` и содержит import map, которая разрешает «голые» спецификаторы через CDN (по умолчанию `https://esm.sh`):

```html
<script type="importmap">
{"imports": {"lodash": "https://esm.sh/lodash"}}
</script>
<script type="module" src="main.js"></script>
```

Способы смены CDN (по убыванию приоритета):

1. `no build --js --browser --esm-cdn https://cdn.jsdelivr.net/npm main.no`
2. `compiler: { esm-cdn: 'https://cdn.jsdelivr.net/npm' }` в `package.noml`

Поскольку `<script type="module">` при `file://` блокируется CORS, `no run --js --browser` поднимает одноразовый статический сервер на временном порту `127.0.0.1` и затем открывает браузер (остановка — Ctrl+C). Полный пример — в `example/esm-imports/`.

## Нативные пакеты: WASM и FFI

Помимо импорта JS-пакетов, JS-бэкенд может вызывать нативные пакеты (разделяемые библиотеки C ABI / WASM). Новый синтаксис не вводится — путь вызова выбирается аннотацией на существующем объявлении. Существующее объявление C-функции (`#{c}` + `name = (params) (results)`, см. главу FFI) и есть нативный интерфейс; раньше JS-бэкенд просто пропускал его, а с аннотацией `bind` для каждого `#{c}` extern создаётся привязка вызова.

### Аннотации

`bind` — это **стратегия сборки**: её можно написать над строкой `# import` (отдельной строкой) или в конце строки, а также прямо в группе аннотаций над `#{c}` extern.

| Ключ | Значение | Описание |
| --- | --- | --- |
| `bind` | `auto` / `ffi` / `wasm` | способ вызова; одна сборка JS допускает только одну стратегию |
| `wasm` | строка | спецификатор WASM-модуля; обязателен при `bind=wasm` или `auto` в браузере |
| `lib` | строка | переопределить имя FFI-библиотеки (по умолчанию из `compiler.link-libs`) |
| `init` | идентификатор | имя экспорта инициализации WASM, по умолчанию `default` |

```no
; pow из libm: FFI идёт через koffi, для браузера нужен отдельный specifier wasm=
#{c, bind=auto}
pow = (x f64, y f64) (r f64)
```

### Матрица цель × стратегия

| Цель | `bind=ffi` | `bind=wasm` | `bind=auto` |
| --- | --- | --- | --- |
| node / bun / deno | koffi-привязка extern | WASM-импорт + ready | есть `link-libs` / `lib=` → FFI; иначе нужен `wasm=` → WASM; иначе ошибка |
| browser | **ошибка на этапе сборки** (koffi не работает в браузере) | WASM-импорт + ready | есть `wasm=` → WASM; иначе ошибка |

Дополнительные правила: `bind=ffi` требует хотя бы один `#{c}` extern, иначе ошибка; отсутствие и `link-libs`, и `lib=` тоже даёт ошибку. При 여러 конфликтующих стратегиях `bind` / `wasm` сборка завершается неудачей.

### FFI (локально / node)

FFI использует настоящий FFI через [`koffi`](https://koffi.dev): `koffi.load(<библиотека>)` + `lib.func(symbol, ret, args)`. Источник библиотеки — `compiler.link-libs` из `package.noml` (`lib=` может переопределить); библиотеки перебираются по порядку, первая подходящая используется. Для каждого `#{c}` extern создаётся привязка с локальным именем `jsIdent(name)` (`_sqlite3-open` → `_sqlite3Open`), а имя C-символа получается удалением ведущего `_` и заменой `-` на `_` (`sqlite3_open`). Соответствие типов:

| Nolang | koffi | Nolang | koffi |
| --- | --- | --- | --- |
| `i8` / `u8` / `byte` | `int8` / `uint8` | `i32` / `u32` | `int` / `uint` |
| `i16` / `u16` | `int16` / `uint16` | `i64` / `u64` | `int64` / `uint64` |
| `f32` / `f64` | `float` / `double` | `str` | `str` |
| `bool` | `bool` | `void` / без возврата | `void` |
| `*T` | `koffi.pointer(<T>)` | `**T` | `koffi.pointer(koffi.pointer(<T>))` |

Остальные типы (срезы／массивы／структуры) в v1 получают `"void *"` как запасной вариант. `koffi` добавляется как синтетическая ESM-зависимость: `no build` при отсутствии пакета лишь предупреждает, `no run --js` сразу даёт ошибку с подсказкой `npm install koffi` (как и для ESM). koffi — CJS, поэтому используется слой совместимости `import * as` + `.default ??`, а не «голый» named import.

### WASM (браузер / `bind=wasm`)

WASM-режим импортирует указанный модуль `.wasm` и создаёт для каждого `#{c}` extern **перенаправление с тем же именем** (`_sqlite3-open` → экспорт wasm `sqlite3_open`). При инициализации создаётся Promise ready (**без await**): синхронно инициализируемые модули готовы сразу, асинхронные переключают state при готовности и открывают ready Promise через `globalThis.__nolang_wasm_ready[<короткое имя>]` для внешнего JS. Вызов до готовности выбрасывает явную ошибку.

Спецификатор WASM имеет две формы, остальные правила зависят от формы:

| Форма | Пример | Объявление в `dependencies` | import map браузера | `no run --js` |
| --- | --- | --- | --- | --- |
| bare (npm-пакет) | `@sqlite.org/sqlite-wasm` | **обязательно** | разрешается через `--esm-cdn` (по умолчанию `https://esm.sh`) | нужна `npm install` |
| относительная (локальный файл) | `./math-wasm.mjs` | **не нужна** | **не попадает в map** (браузер всё равно не переопределяет относительные спецификаторы) | автоматическилинкует файл (и его относительные импорты) во временный каталог |

Относительная форма подходит для случая «модуль лежит прямо в проекте»: артефакт `no build --js` и модуль должны быть в одном каталоге, `no run --js` самлинкует модуль во временный каталог bundle; для браузера модуль нужно скопировать вместе с ним в `dist/` (`import` разрешается относительно расположения файла).

### Полные примеры

- `example/native-libm/`: `#{bind = auto, c}` объявляет `pow` из libm, `no run --js main.no` печатает `1024` (для `no run` нужно сначала выполнить `npm install` в этом каталоге, чтобы поставить `koffi`).
- `example/native-wasm/`: локальный WASM-модуль (`math.wasm` + `math-wasm.mjs`, без npm-зависимостей), `no run --js main.no` печатает `42.25` и `42`; import map сборки `no build --js --browser main.no` не содержит относительных спецификаторов.

### Известные ограничения (v1)
- WASM выполняет только **перенаправление экспорта с тем же именем**, не создавая адаптер API (например, семантические различия вроде `better-sqlite3` → `sql.js` требуют отдельного nolang-wrapper-модуля).
- Стратегия **уровня программы**: одна сборка JS допускает только одну стратегию `bind`; привязка нескольких нативных библиотек по модулям — в будущем.
- FFI привязывает только разделяемые библиотеки C ABI; нативные addon Node (`.node`) используйте через `bind=wasm` или обычный ESM-импорт.

## Полный пример

```no
; main.no — демонстрация браузерного приложения
# js/dom
# js/canvas
# js/events
# js/storage

print('=== Nolang Browser Demo ===')

; DOM: создать заголовок и добавить в body
heading = dom.create-element('h2')
heading.set-text('Hello from Nolang!')
body = dom.body()
body.append-child(heading)

; DOM: создать кнопку
btn = dom.create-element('button')
btn.set-text('Click me')
btn.set-style('margin', '8px')
body.append-child(btn)

; события: нажатие кнопки
events.on-click(btn, () {
    print('button was clicked!')
})

; Canvas: нарисовать прямоугольник
canvasEl = dom.create-element('canvas')
canvasEl.set-attr('width', '240')
canvasEl.set-attr('height', '140')
body.append-child(canvasEl)

ctx = canvas.get-context-2d(canvasEl)
ctx.set-fill('red')
ctx.fill-rect(10, 10, 60, 60)
ctx.set-fill('blue')
ctx.fill-rect(80, 10, 60, 60)

; localStorage: сохранить и прочитать
storage.set-item('greeting', 'Hello from localStorage')
g = storage.get-item('greeting')
print('stored:', g)

; итерация
nums = [10, 20, 30]
i <- nums: {
    print('elem:', i)
}

; ветвление по платформе
#{js-browser}
print('running in browser mode')

print('=== done ===')
```

Сборка:

```bash
no build --js --browser main.no
# вывод: dist/main.js, dist/main.html
# открыть dist/main.html в браузере
```
