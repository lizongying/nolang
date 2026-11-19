---
sidebar_position: 4.3
---

## Global

### global — Fonctions intégrées globales

Fonctions qui peuvent être appelées sans préfixe de module. Il n'existe que les 7 fonctions globales suivantes ; tout autre appel inter-modules doit porter un préfixe de module (par ex. `fs.read`, `os.get-env`) :

```no
; Constructeurs de capacité/longueur (type inféré depuis le côté gauche de l'affectation)
s str = with-cap(256); préalloue un str de 256 octets (len=0)
v []i64 = with-cap(100); préalloue une tranche de 100 éléments (len=0)
s str = with-len(10); str de longueur 10
v []i64 = with-len(100); tranche de longueur 100
v []i64 = with-cap-len(200, 100); tranche de capacité 200 et de longueur 100

; Également appelables comme méthodes sur str/vec :
s = ''.with-cap(256)
s = ''.with-len(10)
s = ''.with-len-cap(10, 256)
v = [].with-cap(100)
v = [].with-len(100)
v = [].with-len-cap(100, 200)

; Sortie/formatage (chaînes de format nommées {name:spec})
print('x={x}'); format nommé, stdout + retour à la ligne
eprint('err {x}'); format nommé, stderr + retour à la ligne
s = format('x={x}'); retourne la chaîne formatée
print(a, b, c); plusieurs arguments : séparés par des espaces, un seul retour à la ligne à la fin
print('result={val}', 42, 'result={val}'); avec plusieurs arguments, chaque littéral est un modèle

; ({val} prend sa valeur dans la portée du point d'appel, ce n'est pas une chaîne de format de style C)
;
; Un champ peut être une « variable / chemin de champ / méthode sans argument / index » et peut être chaîné :
print('{user.name} {user.name.len()} {tags[0]}')
print('{m[\'k\']} {m[\'k\'].len()}'); accès à une map (échapper le guillemet avec \', ou écrire m["k"])

; Seul un « retrait d'un suffixe par niveau » est fait : les méthodes avec arguments ({s.slice(0)}) et les expressions arbitraires ({a + 1}) ne sont pas prises en charge.

; Contrôle de processus
exit(0); termine le processus avec le code d'état 0 (anciennement os.exit, promu en global)
```

---
