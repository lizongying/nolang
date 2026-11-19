---
sidebar_position: 5
---

# Export

Nolang verwendet das Schlüsselwort `@` in der `lib.no`-Datei im Paket-Stammverzeichnis, um Exporteinträge zu deklarieren. Externe Pakete können beim Import über `#` nur auf diese exportierten Symbole zugreifen.

> **Hinweis**: `@` exportiert nur Symbole zwischen Nolang-Paketen und erzeugt keine C-ABI-Symbole. Um C / C++ / Rust / Go / Java / Python / C# Nolang aufrufen zu lassen, verwende stattdessen die Annotation `#{cabi='symbol'}` zusammen mit `no build --shared`. Siehe [Nolang aus Host-Sprachen aufrufen](./host-interop).

## Syntax

```no
@ path.func
[alias]
```

- `path` — Modulpfad (relativ zum Paket-Stammverzeichnis, beginnt mit `/`, ohne `.no`-Endung)
- `func` — Name der zu exportierenden Funktion/Konstante/Enumeration
- `alias` — Optionaler Aliasname, der beim externen Import verwendet wird

## Regeln

- Exportanweisungen dürfen **nur** in der `lib.no`-Datei im Paket-Stammverzeichnis stehen (die einzige Ausnahme siehe nächsten Abschnitt „Globale aufrufbare Aliase")
- Ein Exporteintrag pro Zeile
- Exporteinträge dürfen nur finale Symbole wie Funktionen, Konstanten, Enumerationen sein
- **Der Symbolname muss angegeben werden**: `@ std/txt` (nur ein Pfad, weder `func` noch `alias`) ist ein Fehler (`lgxbfk9a`).
  Eine solche Schreibweise deklariert nichts — alle Verwender setzen einen Symbolnamen voraus —, sie wird daher nicht mehr still ignoriert.
  Sie kann auch nicht dazu dienen, „einen Typ an ein Modul zu binden": Typen haben keine Ebene globaler Namen, der Alias von `@` ist nur für aufrufbare Symbole sinnvoll
- Von exportierten Funktionen referenzierte Strukturen, Enumerationen und andere Typen werden **automatisch exportiert**, ohne manuelle Deklaration
- Wenn eine exportierte Funktion im Modul nicht existiert, meldet der LSP einen Fehler

## Beispiel

```no
; lib.no - 包根目錄導出文件
@ /src/utils.greet a
@ /src/utils.hello b
@ /src/math.pi
```

```no
; src/utils.no
; 定義被導出的函數
greet = (name str) {
    print('Hello, ' - name)
}

hello = () {
    print('Hi')
}
```

## Globale aufrufbare Aliase

Dieselbe Syntax, aber in **`std/global.no`** (statt in der `lib.no` eines Pakets) geschrieben, hat eine andere Bedeutung:
Sie filtert nicht die Exporte, sondern deklariert einen **globalen zweiten Namen, der aufgerufen werden kann** — der nackte Name `<alias>` ist direkt die `func` des Moduls `path`, mit exakt derselben Wirkung wie die ausgeschriebene Form `<path>.<func>(...)`.

```no
; std/global.no —— 兩種寫法，語義相同
@ std/err.new err; 導出式
err = err.new; 賦值式
```

```no
e = err('disk full', code.io); 等同 e = err.new('disk full', code.io)
m = e.msg(); 'disk full'
```

Regeln:

- Nur `std/global.no` hat diese Bedeutung; `@` in einer `lib.no` bleibt ein Exportfilter
- Die beiden Schreibweisen sind **äquivalent**, der Compiler normalisiert sie zu einer einzigen Deklaration; beide gleichzeitig zu schreiben verursacht keinen Konflikt
- Die Zuweisungsform gilt **nur in `global.no`**. In einem gewöhnlichen Programm ist `x = mod.fn` eine Variable, die einen Funktionswert hält (verwendbar als indirekter Aufruf), und wird nicht als globaler Alias behandelt
- Die rechte Seite der Zuweisungsform muss eine nackte Referenz `<Modul>.<Funktion>` sein: kein Aufruf, kein Literal, kein Feldzugriff und keine typisierte Bindung. Wenn das Ziel keine Funktion ist, wird die Deklaration automatisch unwirksam (Konstantendeklarationen wie `PI = math.PI` bleiben unberührt)
- Der Alias wirkt nur an **Stellen mit nacktem Namen**. Qualifizierte Aufrufe bleiben unberührt: `io.err(...)` bleibt der stderr-Schreiber des Moduls `io` und wird nicht vom `err`-Alias gekapert
- Auch wenn ein Modul eine gleichnamige Funktion im eigenen Modul aufruft, bleibt es unberührt (Selbstmodul-Ausnahme)
- Die **Parameterliste** des Alias stammt von der Zielfunktion; der **Rückgabetyp** wird am Aufrufpunkt von der Zielfunktion bestimmt
- Der Alias kollidiert nicht mit der `err`-Variante des eingebauten option-Typs: An Stellen, an denen ein `?T` erwartet wird (`x = err('m')` nach `x ?i64`, ein `err ->`-Match-Zweig, eine Funktion mit Rückgabetyp `?T`), ist `err(...)` weiterhin der err-Varianten-Konstruktor der Option (Tag 2)

## Importieren exportierter Symbole

Externe Pakete können beim Import über `#` nur auf die in `lib.no` deklarierten Exporteinträge zugreifen:

```no
; 导入別名 a（對應 package-name.utils.greet）
# package-name.utils.greet a
; 或者直接使用函數名
# package-name.utils.greet
```

## LSP-Unterstützung

- **Zur Definition springen**: Klicke in `lib.no` auf einen exportierten Funktionsnamen oder Alias, um zur Definition in der entsprechenden Moduldatei zu springen
- **Auto-Vervollständigung**: Schlägt beim Eingeben von `@` und eines Pfads automatisch mögliche Dateipfade und Funktionsnamen vor
- **Fehlerdiagnose**: Zeigt einen Fehler an, wenn eine exportierte Funktion im Modul nicht existiert
