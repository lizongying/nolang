---
sidebar_position: 3.3
---

## Sistema operativo e ficheiros

### os — interface do sistema operativo

Fornece variáveis de ambiente, operações de diretório, gestão de processos, informações do sistema, tempo, etc. Para funcionalidades relacionadas com leitura e escrita de ficheiros, consulte o módulo `fs`.

```no
; Variáveis de ambiente
val = os.get-env(key); devolve ?str (nil=não definida)
os.set-env(key, 'value')

; Diretórios
dir = os.get-wd()
os.ch-dir(dir)
os.mkdir(path, mode)

; Processos
exit(code); termina o processo (promovido a builtin global, sem prefixo os, ver global)
pid = os.get-pid()

; Informações do sistema
name = os.host-name()
arch = os.get-arch()
msg = os.strerror(errnum)
errno = os.get-errno()

; Atributos de ficheiro e utilizador (obtenção de permissões em os, não em fs)
mode = os.stat-mode(path); obtém o modo do ficheiro (st_mode, devolve ?i64, nil=falha)
uid = os.stat-uid(path); obtém o uid do proprietário do ficheiro (devolve ?i64, nil=falha)
gid = os.stat-gid(path); obtém o gid do grupo do ficheiro (devolve ?i64, nil=falha)
mtime = os.stat-mtime(path); obtém a hora de modificação (segundos Unix, devolve ?i64, nil=falha)
self-uid = os.getuid(); ID do utilizador atual
self-gid = os.getgid(); ID do grupo atual

; Tempo
sec = os.now()
ms = os.now-ms()
us = os.now-us()
ns = os.now-ns()
os.sleep(sec)
os.sleep-us(us)
os.sleep-ns(ns)

; Argumentos da linha de comando
argv = os.args(); todos os argumentos da linha de comando ([]str, inclui o nome do programa)
val = os.arg(idx); obtém o argumento idx (fora de limites devolve nil)
```

### fs — utilitários de sistema de ficheiros

Encapsula o ficheiro aberto na struct `file` e o caminho na struct `path`. Define o newtype `fd` (`fd = i64`) para evitar confusão entre descritores de ficheiro e `i64` arbitrários. Também fornece a struct `embed` para incorporar um sistema de ficheiros em tempo de compilação (através de `#{embed='dir/path'}`).

```no
; newtype fd (i64 subjacente, mutuamente exclusivo com i64 no sistema de tipos)
fd = i64

; Struct de ficheiro
file {
    fd fd
    path str
}

; Ficheiros padrão (literais inteiros podem ser usados para inicializar fd)
stdin = file {
    fd: 0
    path: '<stdin>'
}
stdout = file {
    fd: 1
    path: '<stdout>'
}
stderr = file {
    fd: 2
    path: '<stderr>'
}

; Descritores de ficheiro padrão
STDIN-FD fd = 0
STDOUT-FD fd = 1
STDERR-FD fd = 2

; Abrir ficheiro (com opções)
file-mode {
    read,
    write,
    append,
    read-write,
}
file-perm {
    perm-600,
    perm-644,
    perm-664,
    perm-666,
    perm-755,
    perm-777,
}
file-opts {
    mode file-mode
    perm file-perm
    excl bool
    truncate bool
    append bool
}
f = fs.open(path, opts); abre o ficheiro, em caso de sucesso devolve ?file, em caso de falha devolve err

; Métodos de file
read-n = f.read(buf, n); lê até n bytes para buf
line = f.read-line(); lê uma linha (?str, nil=EOF ou erro)
content = f.read-bytes(); lê o ficheiro inteiro como ?[]byte (nil=vazio, err=falha)
content = f.read-str(); lê o ficheiro inteiro como ?str (nil=vazio, err=falha)
written = f.write(data, n); escreve n bytes
ok = f.write-str(data); escreve cadeia (sobrescreve), devolve ?bool
ok = f.write-bytes(data); escreve []byte (sobrescreve), devolve ?bool
ok = f.append(data, n); acrescenta dados
ok = f.copy-to(dst-path); copia para o caminho de destino
ok = f.close(); fecha (os ficheiros padrão stdin/stdout/stderr não são fechados)
yes = f.is-open(); verifica se está aberto (fd >= 0)
sz = f.size(); tamanho do ficheiro (através de fstat(fd), elimina TOCTOU)

; Funções builtin (registadas em src/builtin/os.go, documentadas como comentários em fs.no)
; Estas são funções builtin ForwardFunc/CLibCall — não necessitam de implementação .no.

; Leitura e escrita de ficheiros (descontinuado, recomenda-se read-bytes/write-bytes para suporte a erros)
; [deprecated] read-file: use read-bytes (suporta tratamento de erros, seguro TOCTOU, read em ciclo)
;   Uma fatia vazia indica falha, não é possível distinguir ficheiro vazio de erro
; [deprecated] write-file: use write-bytes (suporta tratamento de erros, write em ciclo)
;   Não é possível distinguir escrita parcial de falha total
data = fs.read-file(path); [deprecated] lê o ficheiro inteiro como []byte (fatia vazia indica falha)
ok = fs.write-file(path, data); [deprecated] escreve []byte no ficheiro (sobrescreve). Em caso de sucesso devolve true
fd = fs.open-read(path); abre apenas para leitura (O_RDONLY)
fd = fs.open-write(path); abre para escrita (O_WRONLY|O_CREAT|O_TRUNC, 0644)
fd = fs.open-file(path, flags, mode); abre com flags e permissões personalizadas
n = fs.read(fd, buf, n); leitura de baixo nível
written = fs.write(fd, data, n); escrita de baixo nível
ok = fs.close(fd); fecho de baixo nível

; Gestão de ficheiros
ok = fs.remove(path); elimina ficheiro (unlink)
ok = fs.rename(old, new); renomeia
ok = fs.symlink(target, linkpath); cria ligação simbólica
ok = fs.link(oldpath, newpath); cria ligação física (hard link)
ok = fs.copy-file(src, dst); copia o conteúdo do ficheiro

; Informações de ficheiro
ok = fs.exists(path); verifica se o caminho existe (segue ligações simbólicas)
ok = fs.is-file(path); verifica se é um ficheiro normal
ok = fs.is-dir(path); verifica se é um diretório
sz = fs.stat-size(path); obtém o tamanho do ficheiro (devolve ?i64)
sz = fs.file-size(path); igual a stat-size (devolve ?i64)
sz = fs.fstat-size(fd); obtém o tamanho via fstat(fd), elimina TOCTOU (devolve ?i64)

; Nota: as funções para obter modo/propensão/hora de modificação do ficheiro estão no módulo `os`
;     (os.stat-mode / os.stat-uid / os.stat-gid / os.stat-mtime)
ok = fs.lstat(path); obtém informações da ligação simbólica (não segue o alvo da ligação)

; Operações de diretório
dirp = fs.open-dir(path); abre diretório (devolve identificador, 0=falha)
name = fs.read-dir(dirp); lê a próxima entrada do diretório (devolve ?str)
ok = fs.close-dir(dirp); fecha o identificador do diretório
names = fs.list-dir(path); lista todos os nomes das entradas (inclui . e ..)
names = fs.dir-entries(path); lista as entradas (sem . e ..)
paths = fs.walk(root); percorre recursivamente a árvore de diretórios

; Resolução de caminhos
abs = fs.realpath(path); resolve para um caminho canónico absoluto
target = fs.readlink(path); lê o alvo da ligação simbólica (devolve ?str)

; Ficheiros/diretórios temporários
name, fd = fs.mkstemp(tmpl); cria ficheiro temporário (o modelo termina em XXXXXX)
name = fs.mkdtemp(tmpl); cria diretório temporário (o modelo termina em XXXXXX, devolve ?str)

; Ficheiros especiais
ok = fs.mkfifo(path, mode); cria pipe nomeado (FIFO)
ok = fs.mknod(path, mode, dev); cria ficheiro especial (nó de dispositivo)
ok = fs.truncate(path, length); trunca ou expande o ficheiro para o tamanho indicado
fs.sync(); descarrega os buffers do sistema de ficheiros para o disco
ok = fs.touch-file(path); atualiza o timestamp do ficheiro para a hora atual
ok = fs.utime(path, atime, mtime); define os tempos de acesso e modificação
ok = fs.rmdir(path); elimina diretório vazio

; Entrada padrão
line = fs.get-line(); lê uma linha da entrada padrão (?str, nil=EOF)

; Funções de conveniência (implementadas em Nolang em fs.no, chamam internamente as builtin)
content = fs.read-str(path); lê o ficheiro inteiro como ?str (nil=vazio, err=falha)
content = fs.read-bytes(path); lê o ficheiro inteiro como ?[]byte (nil=vazio, err=falha)
ok = fs.write-str(path, data); escreve cadeia no ficheiro (sobrescreve), devolve ?bool
ok = fs.write-bytes(path, data); escreve []byte no ficheiro (sobrescreve), devolve ?bool

; embed: sistema de ficheiros incorporado em tempo de compilação (inicializado por #{embed='dir/path'})
;   #{embed='../frontend/dist'}
;   DIST fs.embed
;   data = DIST.read('index.html')   ; lê ficheiro incorporado, devolve ([]byte, bool)
;   ok = DIST.exists('css/style.css') ; verifica se o ficheiro incorporado existe
embed {
    count i64
    paths str; todos os caminhos concatenados (separados por \0)
    pathStarts []i64; deslocamento inicial de cada caminho em paths
    pathLens []i64; comprimento de cada caminho
    blob []byte; conteúdo de todos os ficheiros concatenado
    dataStarts []i64; deslocamento inicial de cada ficheiro em blob
    dataLens []i64; comprimento de cada ficheiro
}

; Específico do Windows (apenas disponível em win-amd64/win-arm64)
bufptr = os.win-find-first-file(path); FindFirstFileA, devolve identificador (0=falha)
name = os.win-find-next-file(bufptr); FindNextFileA, devolve ?str (nil=sem mais entradas)
ok = os.win-find-close(bufptr); FindClose

; Constantes de flags de open() (específicas da plataforma, aqui valores de macOS)
O-RDONLY = 0
O-WRONLY = 1
O-RDWR = 2
O-CREAT = 512; macOS=512, Linux=64, Windows=256
O-TRUNC = 1024; macOS=1024, Linux=512, Windows=512
O-APPEND = 8; macOS=8, Linux=1024, Windows=8
O-EXCL = 2048; macOS=2048, Linux=128, Windows=1024
```

### env — variáveis de ambiente (wrapper simplificado)

```no
val = env.get(key); devolve ?str (nil=não encontrada)
val = env.lookup(key); devolve ?str (nil=não encontrada)
env.set(key, val)
env.unset(key)
val = env.get-with-default(key)
ok = env.is-set(key)
```

### args — argumentos da linha de comando

```no
n = args.count()
arg = args.get(i)
name = args.program()
ok = args.has-flag(name)
val = args.get-option(name)
arg = args.get-positional(i)
```

### path — operações de caminho

Encapsula a cadeia do caminho na struct `path`; todas as operações são fornecidas como métodos:

```no
#{fmt-skip}
SEP = 47     ; '/' (ASCII)
DOT = 46     ; '.'

; Struct
path {
    p str
}

; Concatenação e decomposição de caminhos (modifica .p no local)
p = path{
    p: '/a/b/c.txt'
}
p.join(b str)           ; concatena dois caminhos (modifica no local)
p.base() (out)           ; obtém o nome do ficheiro
p.dir()                  ; obtém o diretório (modifica .p no local)
p.ext() (out)            ; obtém a extensão
p.clean()                ; normaliza (modifica .p no local)
p.split() (f str)        ; divide em diretório+nome do ficheiro (.p passa a ser o diretório, devolve o nome do ficheiro)

; Verificações de caminho
p.is-abs() (yes bool)    ; é um caminho absoluto

; Operações de sistema de ficheiros (delegam nas builtin de fs)
p.exists() (yes bool)        ; existe
p.is-dir() (yes bool)        ; é um diretório
p.is-file() (yes bool)       ; é um ficheiro
p.size() (sz ?i64)           ; tamanho do ficheiro (nil = não existe ou falha em stat)
p.make-dir() (ok bool)       ; cria diretório
p.remove() (ok bool)         ; elimina
p.rename(new-p str) (ok bool)    ; renomeia
p.change-dir() (ok bool)     ; muda o diretório de trabalho

; Métodos construtores
path.current() (out path)    ; obtém o diretório de trabalho atual
```

### bufio — leitura com buffer

```no
r = reader.init(fd, buf); inicializa o leitor com buffer (devolve reader)
ok = reader.fill(); enche o buffer
b = reader.read-byte(); lê um byte (?byte, nil=EOF)
line = reader.read-line(); lê uma linha (?str, nil=EOF ou erro)
reader.close(); fecha
```

### io — abstração de entrada e saída

Fornece as structs `io-reader` e `io-writer`, unificando as operações de leitura e escrita de fluxos de dados como ficheiros, entrada e saída padrão:

```no
; Descritores de ficheiro padrão
STDIN-FD = 0
STDOUT-FD = 1
STDERR-FD = 2

; Struct io-reader
io-reader {
    fd i64
}
r = io-reader.from-fd(fd); cria a partir de fd
r = io-reader.from-stdin(); cria a partir da entrada padrão
read-n = r.read(buf, n); lê n bytes
b = r.read-byte(); lê um byte (?byte, nil=EOF)
line = r.read-line(); lê uma linha (?str, nil=EOF)
total = r.read-all(buf, size); lê tudo

; Struct io-writer
io-writer {
    fd i64
}
w = io-writer.from-fd(fd); cria a partir de fd
w = io-writer.from-stdout(); cria a partir da saída padrão
w = io-writer.from-stderr(); cria a partir do erro padrão
written = w.write(data, n); escreve n bytes
written = w.write-str(s); escreve a cadeia inteira
written = w.write-byte(b); escreve um byte
written = w.write-line(s); escreve cadeia+newline

; Funções de conveniência
n = io.out(s); escreve em stdout (sem newline)
n = io.outln(s); escreve em stdout (com newline)
n = io.err(s); escreve em stderr (sem newline)
n = io.errln(s); escreve em stderr (com newline)
line = io.read-line(); lê uma linha de stdin (?str, nil=EOF)
```

### regexp — expressões regulares

Encapsula o pattern na struct `regexp`, usando inteiramente Nolang para implementar o motor de expressões regulares (VM imperativo + correspondência por retrocesso), sem depender de regex.h da biblioteca padrão C:

```no
; Struct
regexp {
    pattern str
}

; Métodos
re = regexp {
    pattern: '^hello'
}
matched = re.matches(text); verifica se corresponde
result = re.find(text); procura a primeira substring correspondente
```

Suporta **sintaxe de literal regex** `/pattern/flags` (estilo JavaScript), convertida (desugar) numa chamada à função `regexp-compile` na fase de geração de código:

```no
; Literal regex (forma recomendada)
re = /\d+/
matched = re.matches('hello 123 world'); true

; Com flags
re = /[a-z]+/gi

; Chamada explícita equivalente
re = regexp-compile('\\d+')
```

### process — operações de processo

Fornece criação de processos, obtenção de fluxos padrão, espera por processos, consulta de informações do processo, etc. Internamente usa POSIX fork/exec/pipe/waitpid:

```no
; Constantes de sinal
SIG-TERM = 15
SIG-KILL = 9
SIG-INT = 2
SIG-STOP = 19
SIG-CONT = 18
SIG-CHLD = 17
WNOHANG = 1

; Struct
process {
    pid i64
    stdin-fd i64
    stdout-fd i64
    stderr-fd i64
    exit-code i64
    running i64
}

; Criação de processo
p = process.new()
ok = p.start-with-opts(program, args, dir, input, merge-err); inicia subprocesso (sem esperar)
ok = p.start(program, arg); método de conveniência: fork + exec, captura stdout

; Espera por processo
status = p.wait(); espera bloqueante pelo fim do subprocesso
status = p.wait-nohang(); sondagem não bloqueante; nil=ainda em execução

; Controlo do processo
ok = p.kill(sig); envia sinal
ok = p.terminate(); SIG-TERM
ok = p.force-kill(); SIG-KILL

; Operações de fluxo padrão
read-n = p.read(buf, n); lê de stdout
line = p.read-line(); lê uma linha (?str, nil=EOF)
content, n = p.read-all(); lê todo o stdout
written = p.write(data, n); escreve em stdin
p.close-stdin(); fecha o pipe de stdin
p.close-stdout(); fecha o pipe de stdout
p.close-stderr(); fecha o pipe de stderr

; Informações do processo
pid = p.pid-of(); ID do subprocesso
code = p.exit-code-of(); código de saída
yes = p.is-running(); ainda em execução
pid = process.parent-pid(); ID do processo pai

; Ciclo de vida
p.close(); fecha todos os pipes e espera

; Execução única entre plataformas (recomendada, parâmetros planos): fork/exec + captura de stdout/stderr, com timeout e ambiente
;   Entrada: program é o caminho do executável, args é a fatia de argumentos, dir é o diretório de trabalho, input é o conteúdo escrito no stdin do subprocesso,
;            env é a fatia de variáveis de ambiente em forma K=V, timeout em milissegundos (<=0 significa sem limite), merge-err=true funde stderr em out
;   Saída: out é o stdout capturado (stderr é fundido conforme merge-err), stderr é a saída de stderr capturada, code é o código de saída (-2=morto por timeout, -1=falha ao iniciar), err é a mensagem de erro
out, stderr, code, err = process.cmd('echo', ['hello'], '', '', [], 0, false)

; Forma de struct de opções (recomendada, mais legível): demonstração de reenvio de struct opts entre módulos
;   cmdopts é definido neste módulo; a chamadora constrói process.cmdopts{...} noutro módulo e passa-o por referência,
;   demonstrando a capacidade de reenvio de definições de struct entre módulos em nolang.
cmdopts {
    dir str; diretório de trabalho; cadeia vazia = herda o diretório de trabalho do pai
    stdin str; conteúdo escrito no stdin do subprocesso; cadeia vazia = não fornecer stdin
    env []str; variáveis de ambiente (["K=V", ...]); array vazio = herda o ambiente do pai
    timeout i64; tempo limite (milissegundos), <=0 = espera infinita
    merge-err bool; true = funde stderr no out capturado
}
o = process.cmdopts {
    dir: ''
    stdin: 'piped-data'
    env: ['K=V']
    timeout: 200
    merge-err: false
}

; exec-result { stdout str, stderr str, code i64 }
res = process.exec('echo', ['hello'], o); devolve exec-result (res.stdout / res.code ...)

; Funções de conveniência
code = process.shell(cmd); executa o comando através da shell, devolve o código de saída
pid = process.spawn(cmd); gera um subprocesso, devolve o pid
status = process.wait(pid); espera o fim do pid indicado
content, code = process.new().output(program, arg); executa e captura a saída
```

### net — operações de rede

Fornece capacidades de programação de rede TCP, incluindo escuta de servidor, ligação de cliente, envio e receção de dados, etc. Internamente usa a API de socket POSIX:

```no
; Constantes de rede
AF-INET = 2
SOCK-STREAM = 1
SOL-SOCKET = 65535
SO-REUSEADDR = 4
BACKLOG = 128

; Struct listener
listener {
    fd i64
}

; Operações de escuta
l = listener {}
ok = l.listen(host, port); estabelece escuta TCP (socket+setsockopt+bind+listen)
c = l.accept(); aceita ligação (?conn, nil=sem ligação)
l.close(); fecha o socket de escuta
fd = l.fd-of(); obtém o fd

; Struct conn
conn {
    fd i64
}

; Operações de ligação
c = conn {}
ok = c.dial(host, port); estabelece ligação TCP (socket+connect)
written = c.send(data); envia cadeia
read-n = c.recv(buf, n); recebe dados para buf
line = c.recv-line(); recebe uma linha (?str, nil=EOF, até 4096 bytes)
content, total = c.recv-all(); recebe tudo até a ligação fechar
c.close(); fecha a ligação
fd = c.fd-of(); obtém o fd

; Funções de conveniência
l = net.listen-on(host, port); cria um listener e começa a escutar (?listener)
c = net.dial-to(host, port); cria uma ligação e liga (?conn)
```

### net/ip — operações de endereço IP

Fornece análise, validação, conversão e classificação de endereços IPv4. Implementado inteiramente em Nolang:

```no
; Constantes de endereço predefinido
; 0.0.0.0
IP-ZERO

; 127.0.0.1
IP-LOOPBACK

; 0.0.0.0
IP-ANY

; 255.255.255.255
IP-BROADCAST

; Struct ip-addr
ip-addr {
    a i64
    b i64
    c i64
    d i64
}

; Análise e conversão
ip = ip-addr {}
ok = ip.parse('192.168.1.1'); analisa a partir de cadeia
s = ip.to-str(); converte para cadeia '192.168.1.1'
v = ip.to-u32(); converte para u32 (big-endian)
ip.from-u32(v); cria a partir de u32

; Classificação de endereço
yes = ip.is-loopback(); 127.0.0.0/8
yes = ip.is-private(); 10/8, 172.16/12, 192.168/16
yes = ip.is-zero(); 0.0.0.0
yes = ip.is-broadcast(); 255.255.255.255
yes = ip.is-multicast(); 224.0.0.0/4
yes = ip.is-link-local(); 169.254.0.0/16
yes = ip.is-class-a(); Classe A (1~126)
yes = ip.is-class-b(); Classe B (128~191)
yes = ip.is-class-c(); Classe C (192~223)

; Comparação e sub-rede
yes = ip.equal(other); compara igualdade de endereços
yes = ip.in-subnet(base, prefix-len); verifica contenção na sub-rede

; Funções de conveniência
addr = ip.ip-parse(s); análise rápida (?ip-addr, nil=inválido)
yes = ip.ip-is-loopback(s); verificação rápida de loopback
yes = ip.ip-is-private(s); verificação rápida de privado
```

### net/sse — cliente de Server-Sent Events

Suporta receção de fluxos SSE conforme a especificação W3C EventSource. Internamente usa uma ligação HTTP/1.1 de longa duração, suportando HTTP em claro e HTTPS (TLS):

```no
; Struct sse-event
sse-event {
    event str; tipo de evento (predefinição 'message')
    data str; dados do evento (linhas data múltiplas ligadas por \n)
    id str; ID do evento
    retry i64; milissegundos de espera para reconexão (-1=não definido)
}

; Struct sse-client
sse-client {
    fd i64; fd do socket TCP
    tls-c tls.conn; ligação TLS
    use-tls bool; se usa TLS
    connected bool; estado da ligação
    host str; nome do host do servidor
    port i64; número da porta
    path str; caminho do pedido
    last-event-id str; ID do último evento recebido
    recv-buf str; buffer de receção
    recv-buf-len i64; comprimento dos dados no buffer
}

; Ligação e receção de eventos
client = sse.connect('http://host:3000/events'); devolve ?sse-client
client: {
    nil -> print('connect failed')

    -> {
        ev = client.next-event(); devolve ?sse-event (nil=EOF, err=erro)
        ev: {
            nil -> print('connection closed')

            err -> print('error: ' - it.msg())

            -> print(ev.data)
        }
        client.close()
    }
}

; Outros métodos
yes = client.is-connected(); verifica o estado da ligação
ok = client.reconnect(); reconecta (usa last-event-id)
```

### net/http — cliente HTTP/1.1

Fornece um cliente do protocolo HTTP/1.1, com suporte para os métodos GET, POST, PUT, DELETE, PATCH, entre outros, com TLS opcional:

```no
; Structs (definições no módulo std/http; definições dentro do módulo não levam prefixo do módulo, senão são lidas como struct literal)
request {
    method str
    url str
    body str
    headers [16]str
    header-count i64
}
response {
    status-code i64
    status-text str
    headers str
    header-names [32]str
    header-values [32]str
    header-count i64
    body str
}

; Funções de conveniência
resp = http.get(url); pedido GET (?http.response)
resp = http.post(url, body); pedido POST (?http.response)
resp = http.put(url, body); pedido PUT (?http.response)
resp = http.delete(url); pedido DELETE (?http.response)
resp = http.patch(url, body); pedido PATCH (?http.response)
resp = http.do(method, url, body); método personalizado (?http.response)
resp = http.get-auth(url, user, password); GET com autenticação Basic (?http.response)
resp = http.post-auth(url, body, user, password); POST com autenticação Basic (?http.response)

; Usar objeto request
req = http.request {}
req.init('POST', url, body)
req.add-header('Content-Type', 'application/json')
ok = http.set-basic-auth(req, user, password); define cabeçalho de autenticação Basic (bool)
ok = http.set-bearer-auth(req, token); define cabeçalho Bearer (bool)
ok = http.set-api-key(req, header-name, api-key); define cabeçalho de API-key (bool)
resp = http.do-req(req); envia o pedido (?http.response)

; Analisar e ler cabeçalhos da resposta
resp.parse-headers()
val = resp.get-header(name); consulta o valor de um cabeçalho (?str)
```

### net/http2 — cliente HTTP/2.0 (RFC 7540)

Suporta análise de frames HTTP/2 e gestão de ligações, incluindo o modo h2c prior knowledge:

```no
; Struct de frame
http2-frame {
    length i64
    frame-type i64
    flags i64
    stream-id i64
    payload str
}

; Struct de ligação
http2-conn {
    fd i64
    next-stream-id i64
    send-window i64
    recv-window i64
    initialized bool
    use-tls bool
}

; Ligação e pedido
c = http2.connect(host, port); estabelece ligação (?http2-conn)
resp = http2.do(method, url, body); envia pedido (?http.response)

; Operações de frame
frame = http2-frame {}
pos = frame.parse(data, pos); analisa frame (?i64)
pos = frame.serialize(buf, pos); serializa frame
ok = c.send-frame(frame); envia frame
frame = c.recv-frame(); recebe frame (?http2-frame)
```

### net/http3 — cliente HTTP/3.0 (RFC 9114)

Cliente HTTP/3 baseado no protocolo QUIC:

```no
; Constantes de método
HTTP3-METHOD-GET = 'GET'
HTTP3-METHOD-POST = 'POST'
HTTP3-METHOD-PUT = 'PUT'
HTTP3-METHOD-DELETE = 'DELETE'
HTTP3-METHOD-PATCH = 'PATCH'
HTTP3-METHOD-HEAD = 'HEAD'
HTTP3-METHOD-OPTIONS = 'OPTIONS'

; Funções de conveniência
c = http3.connect(host, port); estabelece ligação QUIC (?http3-conn)
resp = http3.send-request(c, method, path, host, body); envia pedido (?http.response)
resp = http3.get(url); pedido GET (?http.response)
resp = http3.post(url, body); pedido POST (?http.response)

; Codificação/decodificação de cabeçalhos QPACK
buf, n = http3.encode-header(name, value)
buf, n = http3.encode-headers(names, values, count)
name, value, pos = http3.decode-header(buf, pos)
```

### net/ws — cliente e servidor WebSocket (RFC 6455)

Suporta comunicação full-duplex do protocolo WebSocket, podendo atuar como cliente ou servidor:

```no
; Struct de mensagem
ws-message {
    opcode i64; 0=continuation, 1=text, 2=binary, 8=close, 9=ping, 10=pong
    data str
    fin bool
}

; Servidor
s = ws.listen-on(host, port); estabelece escuta (?ws-server)
c = s.accept(); aceita ligação (?ws-server-conn)
msg = c.recv(); recebe mensagem (?ws-message)
ok = c.send-text(text); envia texto
ok = c.send-binary(data); envia binário
c.close()

; Cliente
c = ws.connect(url); liga ao servidor (?ws-client)
msg = c.recv(); recebe mensagem (?ws-message)
ok = c.send-text(text); envia texto
ok = c.send-binary(data); envia binário
c.close()
```

### net/tls — cliente TLS 1.2/1.3 (implementação em Nolang puro)

Fornece ligações cifradas TLS, com suporte para TLS 1.2 e 1.3:

```no
; Ligação
c = tls.dial(host, port); estabelece ligação TLS (?tls.conn)
n = c.send(data); envia dados cifrados (?i64)
n = c.recv(buf, n); recebe dados descifrados (?i64)
c.close()
```

### net/client — cliente TCP de alto nível

Encapsula a struct `conn`, fornecendo funcionalidades como reconexão automática:

```no
c = client.client-create(host, port); cria cliente (?client)
ok = c.connect(host, port); liga
ok = c.reconnect(); reconecta
written = c.send(data); envia
read-n = c.recv(buf, n); recebe
line = c.recv-line(); recebe uma linha (?str)
written = c.send-line(line); envia uma linha (i64)
response = c.request(data); modo pedido-resposta (?str)
yes = c.is-connected(); estado da ligação
c.close()
```

### net/quic — protocolo QUIC (RFC 9000)

Fornece uma implementação do protocolo de transporte QUIC, servindo como camada de transporte subjacente do HTTP/3:

```no
c = quic.dial(host, port); estabelece ligação QUIC (?quic.conn)
n = c.send(data, n); envia dados
n = c.recv(buf, n); recebe dados
c.close()
```

### net/server — servidor HTTP

Fornece funcionalidade de servidor HTTP:

```no
s = server {}
ok = s.listen(host, port); começa a escutar
c = s.accept(); aceita uma ligação (?server-conn)
s.close()
```

### net/dns — resolução DNS

Fornece funcionalidade de consulta DNS:

```no
ip = dns.resolve(host); resolve o nome de host (?str)
```

### net/url — análise de URL

Fornece análise e construção de URL:

```no
u = url.url-parse(url); analisa o URL
s = u.to-str(); converte para cadeia
```

### net/cookie — Cookie HTTP

Fornece análise e gestão de cookies HTTP:

```no
c = cookie {}
c.parse(set-cookie-header)
s = c.to-str()
```

### net/multipart — dados de formulário Multipart

Fornece análise e construção de multipart/form-data:

```no
; Escrita (construir multipart/form-data)
w = multipart.multipart-create(); cria o escritor (?multipart-writer)
w.write-field(name, value); escreve um campo
body = w.body-of(); obtém o body codificado
ct = w.content-type(); obtém o content-type (inclui boundary)

; Análise
r = multipart.multipart-parse(content-type, body); analisa (?multipart-reader)
```

### net/hpack — compressão de cabeçalhos HPACK (HTTP/2)

Fornece codificação e decodificação do algoritmo HPACK, usado na compressão de cabeçalhos HTTP/2:

```no
; Operações da tabela de cabeçalhos
t = hpack.tables {}
t.init(); inicializa as tabelas estática e dinâmica
out = t.encode-header(name, value); codifica um único cabeçalho (str)
names, values, count, ok = t.decode-headers(data); decodifica cabeçalhos ([]str)
t.dyn-add(name, value); acrescenta à tabela dinâmica
idx = t.find(name, value); procura name/value completo
idx = t.find-name(name); procura apenas por name
name, value, ok = t.lookup(idx); procura por índice

; Codificação/decodificação de primitivos inteiro/cadeia
out = hpack.encode-int(val, prefix-bits, prefix-byte)
val, next-pos = hpack.decode-int(data, pos, prefix-bits)
out = hpack.encode-str(s)
out, next-pos = hpack.decode-str(data, pos)
```

### net/proxy — suporte a proxy

Fornece ligações através de proxy HTTP/SOCKS:

```no
c = proxy.dial(proxy-url, target-host, target-port)
```

### net/pool — pool de ligações

Fornece gestão em pool de ligações de rede, reutilizando ligações para melhorar o desempenho:

```no
p = pool {}
p.init(capacity)
c = p.get(); obtém uma ligação do pool
p.put(c); devolve a ligação
p.close()
```

### net/unix — sockets de domínio Unix

Fornece comunicação por sockets de domínio Unix:

```no
fd = unix.unix-listen(path); escuta (devolve fd)
fd = unix.unix-dial(path); liga (devolve fd)
l = unix.unix-listen-on(path); cria um listener (?unix-listener)
c = l.accept(); aceita ligação (?unix-conn)
```

---
