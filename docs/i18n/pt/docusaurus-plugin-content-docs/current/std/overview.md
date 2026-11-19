---
sidebar_position: 3
---

# Biblioteca padrão

A biblioteca padrão do Nolang (`src/std/`) contém mais de 80 módulos, cobrindo formatação, matemática, strings, estruturas de dados, codificação/descodificação, encriptação, compressão, operações de ficheiros, abstrações de I/O, corrotinas assíncronas, deteção de tipos de ficheiro, etc.

Utilização: `# std/xxx` (os módulos principais não precisam de importação).

> **A sintaxe antiga `use std/xxx` ainda funciona, mas está descontinuada (deprecated); recomenda-se a nova sintaxe `# std/xxx`.**

> **Atenção: todos os exemplos de código neste documento seguem a regra "uma instrução por linha" — é proibido usar o ponto e vírgula `;` ou a vírgula `,` para colocar várias instruções na mesma linha.** Por exemplo, `out = from-i64(v), out = from-u64(v)` está errado e deve ser dividido em várias linhas.

---

## Categorias de módulos

- [Tipos básicos](basic-types)
- [Biblioteca principal](core-library)
- [Sistema operativo e ficheiros](os-and-files)
- [Hora e data](time-and-date)
- [Registo de eventos](logging)
- [Estruturas de dados](data-structures)
- [Base de dados](database)
- [Codificação](encoding)
- [Arquivos](archives)
- [Criptografia e hashing](crypto)
- [Intercâmbio de dados](data-exchange)
- [Outros](others)
- [Visão geral dos módulos](module-overview)
