---
sidebar_position: 4.4
---

## Autres

### unicode — Prise en charge d'Unicode

Les fonctionnalités liées à Unicode sont réparties entre les modules `char` et `str` :

- Classification des caractères (`is-letter`, `is-digit`, `is-upper`, etc.) → voir le module `char`
- Encodage/décodage UTF-8 (`char.to-bytes`, `char.to-str`) → voir le module `char`
- Comptage des runes d'une chaîne (`str.count`) → voir le module `str`

### uuid — Génération et analyse de UUID v4

```no
out = uuid.new-v4(state); génère un UUID v4
out-n = uuid.to-str(out); convertit en chaîne minuscule (méthode)
out-n = uuid.to-str-upper(out); convertit en chaîne majuscule (méthode)
ok = uuid.from-str(s, sn, out); analyse depuis une chaîne (avec ou sans tirets)
ok = uuid.parse-with-dashes(s, pos, out); analyse avec tirets
ok = uuid.parse-no-dashes(s, pos, out); analyse sans tirets
ok = uuid.validate(); valide le format UUID (méthode)
v = uuid.version(); obtient la version (méthode)
v = uuid.variant(); obtient la variante (méthode)
yes = uuid.is-nil(); est-ce nil (méthode)
yes = uuid.eq(b); comparaison d'égalité (méthode)
r = uuid.cmp(b); comparaison (méthode)
uuid.nil-uuid(out); retourne l'UUID nil
```

### bigint — Entier de précision arbitraire

```no
; Type
bigint {
    sign i64
    limbs []i64
    len i64
}

; Construction
out = bigint.from-i64(v)
out = bigint.from-u64(v)
out = bigint.zero()
out = bigint.one()
out = bigint.copy(a)

; Comparaison
r = bigint.cmp(a, b)
r = bigint.eq(a, b)
r = bigint.is-zero(a)
r = bigint.is-neg(a)
r = bigint.is-pos(a)

; Opérations (+ - * / % sont surchargés pour bigint, voir « Syntaxe > Surcharge d'opérateurs »)
c = a + b; équivalent à a._add(b)
c = a - b; équivalent à a._sub(b)
c = a * b; équivalent à a._mul(b)
q = a / b; équivalent à a._div(b), la division par zéro retourne zéro selon div-mod
r = a % b; équivalent à a._mod(b)

; Opérandes mixtes : la surcharge d'opérateur prend une union `bigint | i64` en paramètre, le compilateur choisit la branche par monomorphisation selon le type de l'opérande droit à la compilation,
; `a + 1` / `a * 256` fonctionnent donc nativement (zéro tag à l'exécution). Limité au cas « opérande gauche est bigint », `1 + a` n'est pas pris en charge.
c = a + 1; bigint ⊕ i64
c = a * 256; bigint ⊕ i64
q, r = a.div-mod(b); obtient à la fois le quotient et le reste
c = a.pow(n)
r = base.mod-pow(exp, mod)

; Théorie des nombres
g = a.gcd(b)
l = a.lcm(b)

; Décalages
bigint.shl(a, n, c)
bigint.shr(a, n, c)

; Conversion en chaîne
n = bigint.to-str(a, out)
out = bigint.from-str(s, sn)
n = bigint.to-hex(a, out)
out = bigint.from-hex(s, sn)
; Les chemins rapides pour petits entiers (_add-i64 / _mul-i64 / _div-i64 / _mod-i64) sont privés au module bigint,
; pour les opérandes mixtes utilisez toujours les surcharges + - * / % ci-dessus
```

### err — Gestion des erreurs

Type d'erreur structuré et fonctions utilitaires :

```no
; Énumération des codes d'erreur
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

; Structure
err {
    code code
    msg str
    file str
    line i64
    col i64
}

; Fonctions
e = err.new(msg, code.io); crée une erreur
e = err.from-errno(errno); crée depuis un errno C
yes = e.is(code.io); teste le code d'erreur
msg = e.msg(); obtient le message d'erreur
c = e.code(); obtient le code d'erreur
s = e.format(); formate en chaîne
```

Cette structure `err` est la charge utile de la variante `err` de l'option intégrée (`option { ok(v t), nil, err(e err.err) }`) :
lorsqu'on filtre un `?t`, le `it` lié implicitement dans la branche `err ->` est cette structure, et non une `str`.

```no
x = f(); f retourne ?i64
x: {
    ok -> n = it

    nil -> n = 0

    err -> {
        log.error(it.msg()); message brut
        print(it.format()); 'code.unknown: ... at file:line:col'
        it.is(code.io) -> print('io error')
    }
}
```

### bool — Type booléen

```no
#{fmt-skip}
bool.to-str() (out str)     ; true→"true", false→"false"（méthode）
```

### enter / leave — Hooks de cycle de vie

```no
; Exécuté au démarrage
enter {
    enter()
}

; Exécuté à la sortie
leave {
    leave()
}
```

### magic — Détection du type de fichier

Détermine le type d'un fichier à partir de son extension et de ses nombres magiques (*magic bytes*), sans dépendre de libmagic :

```no
kind = magic.detect-type(path); détecte le type du fichier
; retourne une chaîne de description du type, comme 'PNG image', 'ELF executable', 'ASCII text', 'directory', 'unknown', 'data'
```

---
