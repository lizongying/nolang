---
sidebar_position: 3.4
---

## Heure et date

### time — Opérations sur l'heure

```no
sec = time.now-s(); horodatage Unix actuel (secondes)
ms = time.now-ms(); horodatage actuel (millisecondes)
us = time.now-us(); horodatage actuel (microsecondes)
ns = time.now-ns(); horodatage actuel (nanosecondes)
sec = time.since-s(start); temps écoulé depuis start (secondes)
ms = time.since-ms(start); temps écoulé depuis start (millisecondes)
us = time.since-us(start); temps écoulé depuis start (microsecondes)
time.sleep-s(sec); pause (secondes)
time.sleep-ms(ms); pause (millisecondes)
time.sleep-us(us); pause (microsecondes)
time.sleep-ns(ns); pause (nanosecondes)
d = time.duration-between(start, end); durée (secondes)
d = time.duration-ms-between(s, e); durée (millisecondes)
d = time.duration-us-between(s, e); durée (microsecondes)

; Opérations sur les dates
yes = time.is-leap(year); l'année est-elle bissextile
days = time.days-in-month-fn(y, m); nombre de jours du mois
d = time.unix-to-date(ts); convertit un horodatage Unix en structure date
ts = time.date-to-unix(d); convertit une structure date en horodatage Unix
d = time.now-date(); obtient la structure date courante
s = time.format-date(d); formate une date en chaîne
s = time.format-time-str(ts); formate un horodatage en chaîne horaire
ts = time.parse-date(s); analyse une chaîne de date (?i64)
s = time.format-duration(sec); formate une durée en chaîne lisible

; structure timer
t = timer {}
t.start(); démarre le chronomètre
t.stop(); arrête le chronomètre
us = t.elapsed-us(); microsecondes écoulées
ms = t.elapsed-ms(); millisecondes écoulées
s = t.elapsed-s(); secondes écoulées
```

---
