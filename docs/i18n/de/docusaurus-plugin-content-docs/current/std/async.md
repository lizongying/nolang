---
sidebar_position: 4.2
---

## Asynchronität (Async)

Nolang besitzt nur eine oberflächliche Nebenläufigkeitssyntax: die Coroutinen-Gruppe `awy { ... }`, in der Aufgaben mit `co` gespawnt werden.
Das Schlüsselwort `run` wurde entfernt (es ist jetzt ein gewöhnlicher Bezeichner); `awy <handle>` und nackte Block-Coroutinen-Gruppen in Anweisungsposition sind veraltet;
**`co` außerhalb einer Gruppe ist ein Kompilierfehler** (`E_CO_OUTSIDE_GROUP`).

`co` startet nur eine **Prozedur** — der Aufgerufene darf keinen deklarierten Rückgabewert haben (das implizite `self` einer Methode zählt nicht),
daher laufen Ergebnisse stets über einen Kanal: Die Prozedur sendet den Wert in den Kanal und ruft `close()` auf, der Aufrufer leert ihn außerhalb der Gruppe.

### Coroutinen-Gruppe

```no
; 過程：只寫通道，沒有宣告回傳值
produce = (ch chan[i64], v i64) {
    ch <- v
    ch.close()
}

ch1 chan[i64] = with-cap(1)
ch2 chan[i64] = with-cap(1)

; 並發：兩個任務一起跑，組尾統一等待
awy {
    co produce(ch1, 1)
    co produce(ch2, 2)
}

r1 = 0
<- ch1: { ok -> r1 = it }
r2 = 0
<- ch2: { ok -> r2 = it }

; 順序：後一個 spawn 用前一個的結果當實參，靠「讀通道」當屏障
awy {
    co produce(ch1, r1)
}
```

In `ch1` / `ch2` liegen die Ergebniswerte, keine Task-Handles. Details siehe `lang/syntax.md` „Asynchrone Programmierung".

### Farbloses async: `co`

`co` ist eine farblose (colorless) Syntax: Du schreibst den ungefärbten Funktionsnamen `produce`, und der Compiler generiert automatisch die Variante `produce-async` und führt sie im Hintergrund als gefärbte stapellose Coroutine aus. Schreibe in Anwendungscode nicht manuell das Suffix `-async`.

Zwei Formen:

- `co f(args)` — Prozedur-Spawn; `f` darf keinen Rückgabewert deklarieren (das implizite `self` einer Methode ist ausgenommen).
- `co { ... }` — Closure-Spawn; freie Variablen des Blocks werden nach Wert erfasst (Momentaufnahme zum Zeitpunkt des Spawns).

`co` ist kein Wert und muss als eigenständige Anweisung stehen: `r = co f()` und `print(co f())` melden beide `c0nostmt`.

Regeln:

- **Monomorphisierung**: `co produce(ch, 1)` erzeugt automatisch `produce-async` (eine umbenannte Kopie des Rumpfs von `produce`). Wird `produce` auch synchron aufgerufen, existieren beide Varianten nebeneinander.
- **Transitivität**: Enthält eine Prozedur selbst `awy { co child(n) }`, wird auch sie zu `-async` monomorphisiert.
- **Lowering im Hintergrund** (keine Benutzersyntax): Jedes `co` wird zu `OpRun`, am Gruppenende wird `OpAwait` eingefügt.

### Abbruch

`cancelled()` kann innerhalb einer Coroutine-Funktion für eine kooperative Selbstprüfung verwendet werden. `cancel(h)` ist eine Low-Level-Handle-Operation;
die neue Syntax stellt kein Task-Handle bereit, Anwendungscode verwendet es nicht.

> **Hinweis:** Der Abbruch ist kooperativ, nicht präemptiv. Ein lang anhaltender blockierender Aufruf (z. B. eine Netzwerkanfrage) kann nicht zwangsweise unterbrochen werden;
> die Aufgabe stoppt erst am nächsten kooperativen Prüfpunkt (einem Aufruf von `cancelled()` oder der nächsten Planung durch die Event-Loop).

#### Abbruch selbst über einen Kanal realisieren (empfohlen)

Die neue Syntax kennt kein `ch.cancel()`, und Kanäle besitzen keine `cancel`-Variante. Wenn „ein externes Signal den Worker aufräumen lässt" benötigt wird,
**verwalte selbst einen Kanal und verwende `close()` als Abbruchsignal** — das ist die einzige Abbruch-Semantik von `chan[T]`.
Im Drei-Zustands-Rumpf der Empfangsschleife bedeutet `nil` „geschlossen und leer", genau der Abbruchzweig; `err` bedeutet nur ein nicht initialisiertes Handle und darf nicht für den Abbruch verwendet werden.

```no
; 控制通道：關閉即取消
stop chan[i64] = with-cap(0)

worker = (stop chan[i64]) {
    <- stop: {
        ok  -> { }              ; 收到訊息（本例不用）
        nil -> print('cancel')  ; stop.close() 後落到這裡
        err -> { }              ; 未初始化的句柄
    }
}

stop.close()
awy {
    co worker(stop)
}
; 輸出 cancel
```

Um den Abbruch während der Ausführung durch eine andere Aufgabe auszulösen, packe auch die Schließaktion in die Gruppe (das `close()` darf nicht erst nach `awy` erfolgen —
das Gruppenende wartet einheitlich, und ein Worker, der bereits auf einem leeren Kanal parkt, würde deadlocken). Beachte, dass `<- ch: { ... }` eine **Empfangsschleife** ist:
Nach Empfang eines Werts wartet sie auf den nächsten, daher muss der Benachrichtiger nach dem Senden `close()` aufrufen, damit der Canceller die Schleife verlässt und
`stop.close()` erreicht:

```no
stop chan[i64] = with-cap(0)
awake chan[i64] = with-cap(1)

worker = (stop chan[i64]) {
    <- stop: { ok -> { }  nil -> print('cancel')  err -> { } }
}

canceller = (awake chan[i64], stop chan[i64]) {
    <- awake: { ok -> { }  nil -> { }  err -> { } }   ; 等通知，收到後還要等 close 才會離開迴圈
    stop.close()                                       ; 這一句才是取消信號
}

notify = (awake chan[i64]) {
    awake <- 1
    awake.close()                                      ; 讓 canceller 的接收迴圈走到 nil
}

awy {
    co worker(stop)
    co canceller(awake, stop)
    co notify(awake)
}
; 輸出 cancel
```

Wenn du lediglich **am Anfang jedes eigenen Schritts im Worker** prüfen willst, ob ein Stop angefordert wurde, ist `cancelled()` direkter;
es kann aber nur selbst prüfen, es gibt kein Handle für externe Signale — für das Szenario „jemand anderes gibt das Signal" verwende die oben beschriebene Kanal-Gepflogenheit.

---
