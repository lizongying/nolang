# Nolang

[傳統中文](README.md) · [简体中文](README_zh-CN.md) · [한국어](README_ko.md) · [日本語](README_ja.md) · [English](README_en.md) · [Русский](README_ru.md) · [Français](README_fr.md) · [Español](README_es.md) · [Deutsch](README_de.md) · [Português](README_pt.md)

Nolang — это системный язык программирования, обеспечивающий безопасность работы с памятью, высокую производительность и минималистичный синтаксис.

## Ключевые особенности

- **Безопасность памяти**: автоматическое управление памятью
- **Высокая производительность**: оптимизация на этапе компиляции, без GC
- **Минималистичный синтаксис**: более простой синтаксис
- **Ориентация на намерение разработчика**: в полной мере учитывает намерения разработчика, семантика соответствует человеческой интуиции
- **Никогда не аварийно завершается**: устранены скрытые пути к сбоям

## Установка

Установка одной командой (macOS / Linux / Windows Git Bash): скрипт загрузит последнюю версию `no` в `$HOME/.local/bin`:

```bash
curl -fsSL https://raw.githubusercontent.com/lizongying/nolang/main/install.sh | sh
```

По умолчанию устанавливается сборка **со встроенным LLVM** (`no-llvm-*`, отдельно ставить LLVM / opt / llc не нужно). Для более компактной сборки на чистом Go используйте `NO_FLAVOR=plain` вместе с `no build -cc zig`:

```bash
curl -fsSL https://raw.githubusercontent.com/lizongying/nolang/main/install.sh | NO_FLAVOR=plain sh
```

Чтобы зафиксировать версию или каталог установки (переменные должны дойти до `sh`, поэтому сначала сохраните скрипт):

```bash
curl -fsSL https://raw.githubusercontent.com/lizongying/nolang/main/install.sh -o install.sh
NO_VERSION=0.4.28 NO_INSTALL_DIR="$HOME/bin" sh install.sh
```

| Переменная        | Значение по умолчанию | Описание                                                        |
| ----------------- | --------------------- | --------------------------------------------------------------- |
| `NO_VERSION`      | `latest`              | Версия, например `0.4.28` или `v0.4.28`                        |
| `NO_FLAVOR`       | `embedded`            | `embedded` — со встроенным LLVM; `plain` — чистый Go (нужен системный LLVM) |
| `NO_INSTALL_DIR`  | `$HOME/.local/bin`    | Каталог установки                                               |
| `NO_REPO`         | `lizongying/nolang`   | Исходный репозиторий                                            |

Проверьте установку командой `no version`. Если `$HOME/.local/bin` отсутствует в `PATH`, добавьте его:

```bash
export PATH="$HOME/.local/bin:$PATH"
```

> Для компиляции программ `.no` также нужен C-линкер: рекомендуется `no build -cc zig` (zig включает clang / lld / libc), либо установите clang самостоятельно.

## Использование

doc

[docs](https://lizongying.github.io/nolang/)

Расширение VSCode

[vscode](https://marketplace.visualstudio.com/items?itemName=lizongying.vscode-nolang)

CLI Nolang

[nolang](https://github.com/lizongying/nolang/releases/latest)

nohub — индекс пакетов (пакеты сообщества)

[nohub](https://lizongying.github.io/nohub/)

### Поиск пакетов сообщества (nohub)

`no` напрямую читает индекс пакетов nohub и предоставляет три команды только для чтения:

```bash
no search <query>    # поиск по имени / ключевому слову / категории
no info <pkg>        # метаданные пакета и команды установки/импорта
no docs <pkg>        # URL документации (docs → homepage → repository)
```

По умолчанию используется публичный индекс. Чтобы объединить несколько экземпляров nohub, настройте список источников в `~/no/sources.noml` (порядок в массиве задаёт приоритет):

```
{
  sources: [
    'https://lizongying.github.io/nohub',
  ]
}
```

`NOHUB_SOURCES` (через запятую) временно переопределяет список. Все три команды принимают `-source <name>`; `no search` выводит столбец `SOURCE`, а пакет с тем же именем и версией берётся из более раннего источника. Ошибка загрузки одного источника лишь выдаёт предупреждение. `no` загружает только `index.noml` каждого источника (`<base>/index.noml` — тот же файл, который страница поиска читает первым, по одной записи на пакет в последней версии); без tarball и `data.js`, сопоставление полей совпадает с `web/app.js`. Те же данные доступны на [странице поиска nohub](https://lizongying.github.io/nohub/).


### Команды CLI

| Команда                                                      | Описание                                                                                                 |
| ------------------------------------------------------------ | -------------------------------------------------------------------------------------------------------- |
| `no init`                                                    | Определяет рабочую область (создаёт workspace.jsonc, но не package.jsonc)                               |
| `no new <name>`                                              | Создаёт новый пакет в рабочей области (подкаталог + package.jsonc и регистрирует его в workspace.jsonc) |
| `no fmt [-w] [-d] <file\|dir>`                               | Форматирует исходный код                                                                                 |
| `no build [-o <file>] [-cc <s>] [-target <s>] [<file\|dir>]` | Собирает проект (создаёт исполняемый файл)                                                               |
| `no run [-cc <s>] [-target <s>] [<package\|dir\|file>]`       | Собирает и запускает (имя пакета/каталог/файл)                                                           |
| `no test [-cc <s>] [-target <s>] [<file\|dir>]`              | Запускает тесты                                                                                          |
| `no add <pkg>`                                               | Добавляет зависимость                                                                                    |
| `no remove <pkg>`                                            | Удаляет зависимость                                                                                      |
| `no update <pkg>`                                            | Обновляет зависимость                                                                                    |
| `no update-all`                                              | Обновляет все зависимости                                                                                |
| `no list`                                                    | Выводит список зависимостей                                                                              |
| `no search <query>`                                            | Поиск пакетов сообщества по имени / ключевому слову / категории |
| `no info <pkg>`                                                | Показать метаданные и команды установки/импорта |
| `no docs <pkg>`                                                | Вывести URL документации пакета |
| `no sync`                                                    | Синхронизирует зависимости                                                                               |
| `no install [-u] [<tool>[:<version>]@<host>/<owner>/<repo>]` | Устанавливает бинарный файл (~/no/bin/ + символическая ссылка)                                          |
| `no uninstall <name>`                                        | Удаляет бинарный файл и символическую ссылку                                                             |
| `no pub --token <token> [--registry <url>]`                  | Публикует в registry                                                                                     |

```bash
# сборка (по умолчанию начинается с main.no)
no build                    # собрать текущий каталог
no build main.no            # собрать указанный файл
no build -o output          # задать путь вывода
no build -cc zig            # использовать компилятор Zig
no build -target x86_64-linux-gnu  # кросскомпиляция (указать целевую платформу)

# запуск (сборка + выполнение)
no run                    # собрать и запустить main.no в текущем каталоге
no run foo                # запустить пакет foo в рабочей области
no run ./foo              # запустить main.no из каталога ./foo
no run -cc zig
no run -target aarch64-macos-gnu

# тестирование (сборка + выполнение)
no test                   # протестировать все файлы .no в каталоге `test`
no test test/my-test.no        # запустить один тестовый файл
no test -cc zig
no test -target x86_64-windows-gnu
```

### Создание нового проекта

```shell
no new test1

cd test1

no run
```

### Hello World

После создания нового проекта отредактируйте `main.no`:

```no
print('Hello, World!')
```

Запустите его:

```bash
no run
```

Вывод:

```
Hello, World!
```

### Правила точек входа

- **main.no** — точка входа программы
- **lib.no** — точка входа библиотеки, экспортирует функции
- **все файлы .no в каталоге tests/** — содержат тестовые утверждения

### О тестировании

- Все файлы тестов находятся в каталоге tests/
- Каждый тестовый файл собирается отдельно
- Если какой-либо тест завершается неудачей, возвращается ненулевой код выхода

```bash
no vet src/std                     # Nolang vet проверяет стандартную библиотеку
```

## Сборка из исходного кода

Nolang следует собирать с помощью `make`:

```bash
make            # собрать все цели (bin/no и vscode-nolang/server/lsp)
make no         # собрать bin/no
make lsp        # собрать LSP-сервер
make package    # собрать LSP и упаковать расширение VSCode
make no-wasm    # транслировать no в WebAssembly
make lsp-wasm   # транслировать LSP в WebAssembly
make playground # собрать no.wasm + lsp.wasm + сайт Docusaurus
make clean      # очистить артефакты сборки
make help       # показать все цели сборки
```

> ⚠️ **Для сборки Nolang необходимо использовать `make`; никогда не собирайте его напрямую через `cd src && go build`.**
>
> Перед сборкой `make` автоматически выполняет `make gen`, который заново создаёт `src/checker/stdsig_gen.go`
> — таблицу сигнатур стандартной библиотеки, **встроенную в бинарный файл** (`funcSigs` / `methodSigs` /
> `funcParams` / `methodParams` / `structFields` / `aliases` / `structMod` /
> `enumVariants` / `embeddedStdModules`). Благодаря ей каждая `no build` пропускает
> проход PASS1 («полный разбор всех std»), и запуск становится заметно быстрее.
>
> Если вы изменили `src/std`, но запустили `go build` **не выполнив сначала `make gen`**, компилятор
> обнаружит несоответствие между встроенной таблицей сигнатур и содержимым `src/std`
> и **выдаст ошибку и откажет в сборке** (с предложением выполнить `make no`). Это сделано намеренно: раньше молчаливый откат к полному разбору замедлял каждую компиляцию, и это было трудно заметить. После изменения `src/std` всегда выполняйте `make no` (он сначала запускает `make gen`, а затем собирает проект).
>
> Для запуска тестов также используйте `make test`: **прямой `go test` завершится ошибкой и будет отклонён, поскольку ключ сигнатур не внедрён (`StdSigKey` пуст)**. `make test` внедряет тот же ключ при сборке тестового бинарного файла, задействуя быстрый путь через встроенную таблицу сигнатур.

---

## Словарь терминов (glossary)

`glossary/` — это **база знаний Nolang в форме словаря терминов**: по одной записи на каждое понятие, с типом, определением, синтаксисом,
примерами, связанными ссылками и кратким описанием в одном предложении. Всего **244 записи в 10 семантических областях** (типы, синтаксис, аннотации, память,
ошибки, модули, внутреннее устройство компилятора, инструментарий, все модули std, известные ловушки), а сами данные хранятся в `.noml`.

Фронтенд написан на Nolang и предоставляет как CLI, так и MCP (stdio / HTTP), поэтому делать запросы могут и люди, и ИИ:

```bash
no build glossary                        # результат: glossary/dist/glossary

./glossary/dist/glossary get option      # получить полный текст одной записи
./glossary/dist/glossary search 三態      # поиск по ключевым словам
./glossary/dist/glossary list --domain=traps   # список известных ловушек
./glossary/dist/glossary check           # проверка согласованности (для CI)

./glossary/dist/glossary mcp             # MCP-сервер (stdio)
./glossary/dist/glossary mcp --http=127.0.0.1:8765
```

Формат вывода можно переключить с помощью `--format=text|md|json`. Описание подключения и формата данных см. в [`glossary/README.md`](glossary/README.md).

Также можно просмотреть пакет glossary на nohub: [glossary on nohub](https://lizongying.github.io/nohub/?package=glossary:latest@github.com/lizongying/nolang)

---

## Связанные проекты

- [nohub — поиск реестра пакетов сообщества экосистемы Nolang](https://lizongying.github.io/nohub/)

  Поиск пакетов сообщества по имени, ключевому слову или категории; просмотр информации о пакете, снимков версий и ссылок на исходный код. Индекс создаётся `web/build.mjs` из снимков `packages/`.

- [noui — набор инструментов интерфейса, переписанный на Nolang в духе GPUI (пакет nohub)](https://lizongying.github.io/nohub/?package=noui:latest@github.com/lizongying/nolang)

- [Benchmarks Game — отчёт о сравнении производительности Nolang, C и Rust](https://github.com/lizongying/no-benchmarks/results/report.md)

- [notools — набор утилит командной строки Unix, реализованных на Nolang](https://github.com/lizongying/notools)

  Пример применения Nolang: набор часто используемых утилит командной строки Unix, реализованных на языке Nolang, включая классические инструменты cat, ls, grep, wc, head, tail и другие. Он демонстрирует практические возможности Nolang в области системного программирования.

---

## TODO

## Лицензия

Apache-2.0
