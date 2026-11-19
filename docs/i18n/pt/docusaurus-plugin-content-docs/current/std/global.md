---
sidebar_position: 4.3
---

## Global (global)

### global — funções incorporadas globais

Funções que podem ser chamadas sem prefixo de módulo. Apenas existem as seguintes 7 funções globais; todas as outras chamadas entre módulos exigem o prefixo do módulo (por exemplo `fs.read`, `os.get-env`):

```no
; Construtores de capacidade/comprimento (o tipo é inferido a partir do lado esquerdo da atribuição)
s str = with-cap(256); pré-aloca str de 256 bytes (len=0)
v []i64 = with-cap(100); pré-aloca slice de 100 elementos (len=0)
s str = with-len(10); str de comprimento 10
v []i64 = with-len(100); slice de comprimento 100
v []i64 = with-cap-len(200, 100); slice com capacidade 200 e comprimento 100

; Também podem ser chamadas como métodos de str/vec：
s = ''.with-cap(256)
s = ''.with-len(10)
s = ''.with-len-cap(10, 256)
v = [].with-cap(100)
v = [].with-len(100)
v = [].with-len-cap(100, 200)

; Saída/formatação (strings de formatação nomeadas {name:spec})
print('x={x}'); formato nomeado, stdout + nova linha
eprint('err {x}'); formato nomeado, stderr + nova linha
s = format('x={x}'); retorna a string formatada
print(a, b, c); vários argumentos：separados por espaços, apenas uma nova linha no fim
print('result={val}', 42, 'result={val}'); com vários argumentos, cada literal é um template por si

; （{val} obtém o valor do escopo do local da chamada, não é uma string de formatação ao estilo C）
;
; Um campo pode ser "variável / caminho de campo / método sem argumentos / índice" e pode encadear-se nível a nível：
print('{user.name} {user.name.len()} {tags[0]}')
print('{m[\'k\']} {m[\'k\'].len()}'); acesso a map (usar \' para escapar no template, ou escrever m["k"]）

; Faz apenas "desresugar removendo um sufixo por nível"：métodos com argumentos ({s.slice(0)}) e expressões arbitrárias ({a + 1}) não são suportados.

; Controlo de processo
exit(0); termina o processo com o código de estado 0 (antes os.exit, agora global)
```

---
