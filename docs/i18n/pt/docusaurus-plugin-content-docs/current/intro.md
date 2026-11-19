---
sidebar_position: 1
---

# Introdução ao Nolang

O Nolang é uma linguagem de programação de sistemas experimental, segura em relação à memória, sem GC, com semântica intuitiva e sintaxe mínima.

## Funcionalidades principais

- **Segurança de memória**: sem coletor de lixo; gestão automática de memória.
- **Desempenho poderoso**: otimização em tempo de compilação, sem GC.
- **Sintaxe mínima**: menos palavras-chave; sem ponteiros, propriedade ou tempos de vida como sobrecarga mental implícita; sintaxe mais simples.
- **Orientada à intenção**: respeita a intenção do programador; a semântica está alinhada com a intuição humana.
- **Livre de falhas**: elimina caminhos implícitos de falha.

## Início rápido

```no
; Olá, Mundo!
; Não é necessário um ponto de entrada main
print('Hello, Nolang!')

; Declaração de variável
count i64

; Definição de função
add = (a i64, b i64) (result i64) {
    result = a + b
}

; Método da biblioteca padrão, pode ser chamado diretamente
c = math.max(a, b)

; Struct
user {
    name str
    age i64
}

; Método
user.greet = () {
    print('Hello, {.name}')
}

u = user {
    name: 'Alice'
    age: 30
}

u.greet()
```

## Projetos

- [notools](https://github.com/lizongying/notools) — uma coleção de ferramentas de linha de comando Unix comuns implementadas em Nolang, incluindo cat, ls, grep, wc, head, tail e muito mais. Demonstra as capacidades reais de programação de sistemas do Nolang.
