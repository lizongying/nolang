# net 与 net/* 全部协议客户端（http/tls/ws/quic/http2/http3/dns/url/cookie/multipart/hpack/proxy/pool/unix/sse/ip）

> 视图切分自原 `nolang-std/SKILL.md`，正文未改写；索引与加载判据见 [`../m20-std.md`](../m20-std.md)。

#### net — Network Operations

Provides TCP networking capabilities, including server listening, client connections, and data send/receive. Underlying implementation uses POSIX socket API:

```no
; Network constants
AF-INET = 2
SOCK-STREAM = 1
SOCK-DGRAM = 2
SOL-SOCKET = 65535
SO-REUSEADDR = 4
SO-RCVTIMEO = 18
BACKLOG = 128
NET-BUF-SIZE = 4096

; listener struct
listener {
    fd i64
}

; Listening operations
l = listener {}
ok = l.listen(host, port); Establish TCP listener (socket+setsockopt+bind+listen)
c = l.accept(); Accept connection (?conn, nil=no connection)
c = l.accept-with-timeout(ms); Accept with timeout (?conn)
fd = l.fd-of(); Get fd
l.set-timeout(ms); Set accept timeout
l.close(); Close listening socket

; conn struct
conn {
    fd i64
}

; Connection operations
c = conn {}
ok = c.dial(host, port); Establish TCP connection (socket+connect)
written = c.send(data); Send string
read-n = c.recv(buf, n); Receive data into buf
line = c.recv-line(); Receive one line (?str, nil=EOF, max 4096 bytes)
content, total = c.recv-all(); Receive all until connection closed
read-n = c.recv-with-timeout(buf, n, ms); Receive with timeout
written = c.send-with-timeout(data, ms); Send with timeout
c.set-recv-timeout(ms); Set receive timeout
c.set-timeout(ms); Set send/receive timeout
c.close(); Close connection
fd = c.fd-of(); Get fd

; Convenience functions
l = net.listen-on(host, port); Create listener and start listening (?listener)
c = net.dial-to(host, port); Create connection and dial (?conn)
c = net.dial-timeout(host, port, ms); Dial with timeout (?conn)

; UDP operations
uc = udp-conn {}
ok = uc.open(host, port); Open UDP connection
uc.send(data); Send string
uc.send-bytes(data); Send []byte
data, n = uc.recv(buf, n); Receive
uc.recv-bytes(buf, n); Receive []byte
uc.set-timeout(ms); Set timeout
uc.close()

; ICMP/ping operations
ic = icmp-conn {}
ok = ic.open(); Open ICMP socket
ok = ic.ping-host(host, count); Ping host
ok = ic.ping-once(host); Single ping
rtt = ping(host); Ping and get RTT
rtts = ping-count(host, count); Ping multiple times

; System info (macOS)
arps = arp-list(); ARP table entries
routes = route-list(); Route table entries
ifs = ifconfig-list(); Network interfaces

; Encrypted connection support
ec = enc-conn {}
ok = ec.init(key); Initialize encryption
ec.send-encrypted(data); Send encrypted
ec.recv-encrypted(buf); Receive encrypted
ec.gen-iv(); Generate IV
c = enc-dial(host, port, key); Dial encrypted
ec = enc-accept(fd, key); Accept encrypted

; WebSocket helper
parts = split-ws(url); Split WebSocket URL into host/port/path
```

#### net/ip — IP Address Operations

Provides IPv4 address parsing, validation, conversion, and classification. Pure Nolang implementation:

```no
; Default address constants
; 0.0.0.0
IP-ZERO

; 127.0.0.1
IP-LOOPBACK

; 0.0.0.0
IP-ANY

; 255.255.255.255
IP-BROADCAST

; ip-addr struct
ip-addr {
    a i64
    b i64
    c i64
    d i64
}

; Parsing and conversion
ip = ip-addr {}
ok = ip.parse('192.168.1.1'); Parse from string
s = ip.to-str(); Convert to string '192.168.1.1'
v = ip.to-u32(); Convert to u32 (big-endian)
ip.from-u32(v); Create from u32

; Address classification
yes = ip.is-loopback(); 127.0.0.0/8
yes = ip.is-private(); 10/8, 172.16/12, 192.168/16
yes = ip.is-zero(); 0.0.0.0
yes = ip.is-broadcast(); 255.255.255.255
yes = ip.is-multicast(); 224.0.0.0/4
yes = ip.is-link-local(); 169.254.0.0/16
yes = ip.is-class-a(); Class A (1~126)
yes = ip.is-class-b(); Class B (128~191)
yes = ip.is-class-c(); Class C (192~223)

; Comparison and subnet
yes = ip.equal(other); Address equality comparison
yes = ip.in-subnet(base, prefix-len); Subnet containment check

; Convenience functions
addr = ip.ip-parse(s); Quick parse (?ip-addr, nil=invalid)
yes = ip.ip-is-loopback(s); Quick loopback check
yes = ip.ip-is-private(s); Quick private check
```

#### net/sse — Server-Sent Events Client

Supports SSE streaming reception conforming to W3C EventSource spec. Underlying implementation uses HTTP/1.1 long connections, supporting both plain HTTP and HTTPS (TLS):

```no
; sse-event struct
sse-event {
    event str; Event type (default 'message')
    data str; Event data (multi-line data joined with \n)
    id str; Event ID
    retry i64; Reconnect wait milliseconds (-1=not set)
}

; sse-client struct
sse-client {
    fd i64; TCP socket fd
    tls-c tls.conn; TLS connection
    use-tls bool; Whether to use TLS
    connected bool; Connection state
    host str; Server hostname
    port i64; Port number
    path str; Request path
    last-event-id str; Last received event ID
    recv-buf str; Receive buffer
    recv-buf-len i64; Buffer data length
}

; Connection and event reception
client = sse.sse-connect('http://host:3000/events'); Returns ?sse-client
client: {
    nil -> print('connect failed')

    -> {
        ev = client.next-event(); Returns ?sse-event (nil=EOF, err=error)
        ev: {
            nil -> print('connection closed')

            err -> print('error: ' - it.msg())

            -> print(ev.data)
        }
        client.close()
    }
}

; Other methods
yes = client.is-connected(); Check connection state
ok = client.reconnect(); Reconnect (uses last-event-id)
```

#### net/http — HTTP/1.1 Client

Provides an HTTP/1.1 protocol client, supporting GET, POST, PUT, DELETE, PATCH and other methods, with optional TLS:

```no
; Structs (definitions inside std/http; no module prefix on definitions — with a prefix it parses as a struct literal)
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

; Convenience functions
resp = http.get(url); GET request (?http.response)
resp = http.post(url, body); POST request (?http.response)
resp = http.put(url, body); PUT request (?http.response)
resp = http.delete(url); DELETE request (?http.response)
resp = http.patch(url, body); PATCH request (?http.response)
resp = http.do(method, url, body); Custom method (?http.response)
resp = http.get-auth(url, user, password); GET with basic auth (?http.response)
resp = http.post-auth(url, body, user, password); POST with basic auth (?http.response)

; Using request object
req = http.request {}
req.init('POST', url, body)
req.add-header('Content-Type', 'application/json')
ok = http.set-basic-auth(req, user, password); Set Basic auth header (bool)
ok = http.set-bearer-auth(req, token); Set Bearer token header (bool)
ok = http.set-api-key(req, header-name, api-key); Set API-key header (bool)
resp = http.do-req(req); Send request (?http.response)

; Parse & read response headers
resp.parse-headers()
val = resp.get-header(name); Look up a header value (?str)
```

> **Gotcha — `https://` (TLS) is currently unavailable in pure Nolang.** Plain `http://` works: `get`/`do` return a real `ok(response)`. But the pure-Nolang `net/tls` record layer is not functional yet, so any `https://` request via `http.get` / `http.do` / `http.do-req` returns an **explicit `err(...)`** (not a silent `nil`) with messages such as `tls handshake failed: pure nolang TLS unavailable`, `tls send failed: ...`, `tls recv failed: ...`. Handle the `err` branch; for now, target plain HTTP or shell out to an external TLS-capable client.

#### net/http2 — HTTP/2.0 Client (RFC 7540)

Supports HTTP/2 frame parsing and connection management, supports h2c prior knowledge mode:

```no
; Frame struct
http2-frame {
    length i64
    frame-type i64
    flags i64
    stream-id i64
    payload str
}

; Connection struct
http2-conn {
    fd i64
    next-stream-id i64
    send-window i64
    recv-window i64
    initialized bool
    use-tls bool
}

; Connection and request
c = http2.connect(host, port); Establish h2c connection (?http2-conn)
c = http2.connect-tls(host, port); Establish TLS connection (?http2-conn)
resp = http2.do(method, url, body); Send request (?http.response)
resp = http2.get(url); GET request
resp = http2.post(url, body); POST request
resp = http2.put(url, body); PUT request
resp = http2.delete(url); DELETE request
resp = http2.patch(url, body); PATCH request

; Connection methods
ok = c.send-data(data, stream-id); Send data on stream
ok = c.send-headers(headers, count, stream-id, end-stream); Send headers
ok = c.send-settings(); Send SETTINGS frame
frame = c.recv-frame(); Receive frame
c.close()

; Frame operations
frame = http2-frame {}
pos = frame.parse(data, pos); Parse frame (?i64)
pos = frame.serialize(buf, pos); Serialize frame
```

#### net/http3 — HTTP/3.0 Client (RFC 9114)

HTTP/3 client based on QUIC protocol:

```no
; Method constants
HTTP3-METHOD-GET = 'GET'
HTTP3-METHOD-POST = 'POST'
HTTP3-METHOD-PUT = 'PUT'
HTTP3-METHOD-DELETE = 'DELETE'
HTTP3-METHOD-PATCH = 'PATCH'
HTTP3-METHOD-HEAD = 'HEAD'
HTTP3-METHOD-OPTIONS = 'OPTIONS'

; Convenience functions
c = http3.connect(host, port); Establish QUIC connection (?http3-conn)
resp = http3.http3-do(method, url, body); Send request (?http.response)
resp = http3.get(url); GET request
resp = http3.post(url, body); POST request

; Frame operations
frame, pos = http3.parse-frame(data, pos); Parse HTTP/3 frame
pos = http3.encode-frame(buf, pos, type, flags, payload); Encode frame
pos = http3.encode-data-frame(buf, pos, data); Encode DATA frame
pos = http3.encode-headers-frame(buf, pos, headers); Encode HEADERS frame
pos = http3.encode-settings-frame(buf, pos, settings); Encode SETTINGS frame

; Header operations
pos = http3.encode-header(buf, pos, name, value); Encode single header
pos = http3.encode-headers(buf, pos, names, values, count); Encode multiple headers
name, value, pos = http3.decode-header(buf, pos); Decode single header

; URL and response
parts = http3.parse-url(url); Parse URL
val = resp.get-header(name); Get response header
```

#### net/ws — WebSocket Client and Server (RFC 6455)

Supports full-duplex communication over WebSocket protocol, can act as client or server:

```no
; Message struct
ws-message {
    opcode i64; 0=continuation, 1=text, 2=binary, 8=close, 9=ping, 10=pong
    data str
    fin bool
}

; Server
s = ws.listen-on(host, port); Create listener (?ws-server)
c = s.accept(); Accept connection (?ws-server-conn)
msg = c.recv(); Receive message (?ws-message)
ok = c.send-text(text); Send text
ok = c.send-binary(data); Send binary
ok = c.send-ping(); Send ping
ok = c.send-pong(); Send pong
ok = c.send-close(); Send close
c.close()

; Client
c = ws.connect(url); Connect to server (?ws-client)
msg = c.recv(); Receive message (?ws-message)
ok = c.send-text(text); Send text
ok = c.send-binary(data); Send binary
ok = c.send-ping(); Send ping
ok = c.send-pong(); Send pong
ok = c.send-close(); Send close
yes = c.is-connected(); Connection state
c.close()

; WebSocket utilities
frame = build-frame(op, data); Build WebSocket frame
accept = compute-accept(key); Compute Sec-WebSocket-Accept
client-key = gen-client-key(); Generate client key
mask-key = gen-mask-key(); Generate mask key
```

#### net/tls — TLS 1.2/1.3 Client (Pure Nolang Implementation)

Provides TLS encrypted connections, supporting TLS 1.2 and 1.3:

```no
; Connection
c = tls.dial(host, port); Establish TLS connection (?tls.conn)
c = tls.dial-12(host, port); TLS 1.2 connection (?tls.conn)
n = c.send(data); Send encrypted data (?i64)
n = c.recv(buf, n); Receive decrypted data (?i64)
c.close()

; Server support
ok = tls.server-listen(host, port, cert, key); Start TLS server
ok = tls.server-accept(fd); Accept TLS connection
ok = tls.https-serve-once(fd, cert, key); Handle one HTTPS request
```

> **Status — client TLS record layer is not yet functional.** In this environment `tls.conn.connect` / handshake fails against real servers (peer logs `DECRYPTION_FAILED_OR_BAD_RECORD_MAC`), so `https://` requests through `net/http` return an explicit `err(...)` rather than a response. The API above is stable; treat pure-Nolang TLS as unavailable until the record layer is fixed.

#### net/client — High-level TCP Client

Wraps the `conn` struct, providing auto-reconnect and other features:

```no
c = client.client-create(host, port); Create client (?client)
ok = c.connect(host, port); Connect
ok = c.reconnect(); Reconnect
written = c.send(data); Send
read-n = c.recv(buf, n); Receive
line = c.recv-line(); Receive one line (?str)
written = c.send-line(line); Send one line (i64)
response = c.request(data); Request-response mode (?str)
yes = c.is-connected(); Connection state
c.close()
```

#### net/quic — QUIC Protocol (RFC 9000)

Provides QUIC transport protocol implementation, serving as the underlying transport layer for HTTP/3:

```no
c = quic.dial(host, port); Establish QUIC connection (?quic.conn)
n = c.send(data, n); Send data
n = c.recv(buf, n); Receive data
c.close()
```

#### net/server — HTTP Server

```no
; HTTP server
s = server {}
ok = s.listen(host, port); Start listening
c = s.accept(); Accept connection (?server-conn)
c = s.accept-nb(); Non-blocking accept
fd = s.fd-of(); Get server fd
s.close()

; server-conn methods
ok = c.parse(); Parse HTTP request
val = c.get-header(name); Get request header
val = c.get-cookie(name); Get request cookie
val = c.get-signed-cookie(name, key); Get signed cookie
c.set-signed-cookie(name, val, key); Set signed cookie
ok = c.verify-basic-auth(user, pass); Verify basic auth
c.recv-more(); Receive more data
c.reset(); Reset connection state
ok = c.write-status(code); Write status line
ok = c.write-header(name, value); Write header
ok = c.write-headers-done(); End headers
ok = c.write-body(data); Write body
ok = c.write-text(text); Write text response
ok = c.write-html(html); Write HTML response
ok = c.write-json(json-str); Write JSON response
ok = c.write-error(code, msg); Write error response
ok = c.write-redirect(url); Write redirect response
c.close()

; HTTPS server
hs = https-server {}
ok = hs.listen(host, port, cert, key); Start HTTPS listening
c = hs.accept(); Accept HTTPS connection (?https-server-conn)
hs.close()

; Utility
yes = secure-compare(a, b); Constant-time string comparison
```

#### net/dns — DNS Resolution

```no
ip = dns.resolve(host); Resolve hostname (?str)
ips = dns.resolve-all(host); Resolve all IPs ([]str)
ip = dns.resolve-from(server, host); Resolve from specific DNS server
```

#### net/url — URL Parsing

```no
u = url.url-parse(url); Parse URL
s = u.to-str(); Convert to string

; URL operations
encoded = url.url-encode(s); URL encode
encoded = url.url-encode-component(s); URL encode component
decoded = url.url-decode(s); URL decode
query = url.url-build-query(params, values, count); Build query string
joined = url.url-join(base, ref); Join URLs
user, pass = url.url-parse-basic-auth(url); Parse basic auth from URL
auth = url.url-basic-auth(user, pass); Build basic auth header

; url-info methods
q = u.get-query(); Get query parameters
u.set-query(q); Set query parameters
```

#### net/cookie — HTTP Cookie

```no
c = cookie {}
c.parse(set-cookie-header)
s = c.to-str()

; Cookie parsing and building
parsed = cookie.parse-header(header); Parse Cookie header
parsed = cookie.parse-set(set-cookie); Parse Set-Cookie header
header = cookie.build-header(c); Build Cookie header
set = cookie.build-set(c); Build Set-Cookie header

; Signed cookies
ok = cookie.sign(c, key); Sign cookie
ok = cookie.verify(c, key); Verify signed cookie

; Cookie jar
jar = cookie.jar {}
jar.parse-response(set-cookie-headers); Parse response cookies
val = jar.get(name); Get cookie value
val = jar.get-verified(name, key); Get and verify signed cookie
jar.set(name, val); Set cookie
jar.set-signed(name, val, key); Set signed cookie
jar.delete(name); Delete cookie
jar.clear(); Clear all
n = jar.count-of(); Cookie count
header = jar.build-header(); Build Cookie header
```

#### net/multipart — Multipart Form Data

```no
; Reader
r = multipart-reader {}
r.init(data, boundary)
ok = r.next(); Move to next part
name = r.field-name(); Current field name
filename = r.field-filename(); Current filename
data = r.field-data(); Current field data
ct = r.field-content-type(); Current content-type
yes = r.is-file(); Whether current part is a file
yes = r.has-current-part(); Whether has current part

; Writer
w = multipart-writer {}
w.init()
w.init-boundary(boundary)
boundary = w.boundary-of(); Get boundary
ct = w.content-type(); Get content-type header
w.write-field(name, value); Write text field
w.write-file(name, filename, data); Write file field
w.write-encrypted-field(name, value, key); Write encrypted field
w.write-encrypted-file(name, filename, data, key); Write encrypted file
w.close-form(); Close form

; Top-level functions
boundary = multipart-create-boundary(); Create random boundary
boundary = multipart-gen-boundary(); Generate boundary
boundary = multipart-extract-boundary(content-type); Extract boundary from content-type
data = multipart-create(fields, count, boundary); Create multipart data
fields = multipart-parse(data, boundary); Parse multipart data
data = multipart-decrypt-field(encrypted, key); Decrypt field
```

#### net/hpack — HPACK Header Compression (HTTP/2)

```no
; Table operations
t = hpack.tables {}
t.init(); Initialize static+dynamic table
out = t.encode-header(name, value); Encode one header (str)
names, values, count, ok = t.decode-headers(data); Decode headers ([]str)
t.dyn-add(name, value); Add to dynamic table
idx = t.find(name, value); Find full name/value pair
idx = t.find-name(name); Find by name only
name, value, ok = t.lookup(idx); Lookup by index

; Primitive integer/string codecs
out = hpack.encode-int(val, prefix-bits, prefix-byte); Encode integer (str)
val, next-pos = hpack.decode-int(data, pos, prefix-bits); Decode integer
out = hpack.encode-str(s); Encode string literal (str)
out, next-pos = hpack.decode-str(data, pos); Decode string literal
```

#### net/proxy — Proxy Support

```no
c = proxy.dial(proxy-url, target-host, target-port); Dial via proxy
c = proxy.connect(proxy-url, host, port); Connect via proxy
c = proxy.dial-auth(proxy-url, host, port, user, pass); Dial with auth
c = proxy.socks5-dial(host, port, target-host, target-port); SOCKS5 dial
c = proxy.socks5-connect(host, port, target); SOCKS5 connect

; Config-based
cfg = proxy.config {}
c = cfg.connect(host, port); Connect via config
c = cfg.tls-connect(host, port); TLS connect via config
c = proxy.proxy-tls-dial(host, port, target); TLS proxy dial
```

#### net/pool — Connection Pool

```no
p = pool.create(capacity); Create pool
p = pool.with-size(n); Create with size
p.init(capacity)
c = p.get(); Get connection from pool
p.put(c); Return connection
p.close-conn(c); Close single connection
p.close-all(); Close all connections
n = p.total-count(); Total connections
n = p.active-count(); Active connections
n = p.idle-count(); Idle connections
```

#### net/unix — Unix Domain Socket

```no
l = unix.unix-listen-on(path); Listen on Unix socket
l = unix-listener {}
ok = l.listen(path); Listen
c = l.accept(); Accept (?unix-conn)
c = l.accept-with-timeout(ms); Accept with timeout
fd = l.fd-of(); Get fd
p = l.path-of(); Get socket path
l.set-timeout(ms); Set timeout
l.close()

c = unix.unix-dial-to(path); Dial Unix socket
c = unix-dial-timeout(path, ms); Dial with timeout
c = unix-conn {}
ok = c.dial(path); Connect
n = c.send(data); Send
n = c.recv(buf, n); Receive
n = c.recv-all(buf, n); Receive all
n = c.recv-with-timeout(buf, n, ms); Receive with timeout
c.set-recv-timeout(ms); Set recv timeout
c.set-timeout(ms); Set timeout
c.close()
```

---
