---
sidebar_position: 4.3
---

## モジュール一覧

| モジュール          | パス   | 説明                       |
| ------------------- | ------ | -------------------------- |
| fmt                 | コア   | 書式付き出力               |
| math                | コア   | 数学関数                   |
| str                 | コア   | 文字列操作                 |
| vec                 | コア   | スライス（[]t）操作        |
| arr                 | コア   | 配列（[n]t）操作           |
| number              | コア   | 数値ユーティリティ関数     |
| byte                | コア   | バイト操作                 |
| char                | コア   | 文字操作（メソッド）       |
| os                  | コア   | オペレーティングシステムインターフェース |
| env                 | コア   | 環境変数のラッパー         |
| fs                  | コア   | ファイルシステムユーティリティ |
| io                  | コア   | 入出力の抽象化             |
| args                | コア   | コマンドライン引数         |
| path                | コア   | パス処理（構造体）         |
| bufio               | コア   | バッファ読み取り           |
| time                | コア   | 時間操作                   |
| log                 | コア   | レベル付きログ             |
| json                | コア   | JSON 解析/生成             |
| toml                | コア   | TOML 1.0 解析/生成         |
| yaml                | コア   | YAML 1.2 解析/生成         |
| types               | コア   | 型定義ドキュメント         |
| sort                | コア   | ソート定数                 |
| set                 | コア   | 集合                       |
| deque               | コア   | 両端キュー（構造体）       |
| heap                | コア   | 最小ヒープ（構造体）       |
| stack               | コア   | スタック（構造体）         |
| regexp              | コア   | 正規表現                   |
| process             | コア   | プロセス操作               |
| uuid                | コア   | UUID v4                    |
| bigint              | コア   | 任意精度整数               |
| bool                | コア   | 真偽値型                   |
| err                 | コア   | エラー処理                 |
| enter               | コア   | 起動フック                 |
| leave               | コア   | 終了フック                 |
| async               | コア   | 非同期コルーチン/キャンセル |
| global              | コア   | グローバル組み込み関数     |
| magic               | コア   | ファイル種別の検出         |
| net                 | コア   | TCP ネットワーク操作       |
| net/http            | サブモジュール | HTTP/1.1 クライアント |
| net/http2           | サブモジュール | HTTP/2.0 クライアント |
| net/http3           | サブモジュール | HTTP/3.0 クライアント |
| net/ws              | サブモジュール | WebSocket           |
| net/quic            | サブモジュール | QUIC プロトコル      |
| net/tls             | サブモジュール | TLS 1.2/1.3         |
| net/sse             | サブモジュール | SSE クライアント     |
| net/client          | サブモジュール | 高レベル TCP クライアント |
| net/server          | サブモジュール | HTTP サーバー        |
| net/dns             | サブモジュール | DNS 解析             |
| net/url             | サブモジュール | URL 解析             |
| net/cookie          | サブモジュール | HTTP Cookie         |
| net/multipart       | サブモジュール | Multipart フォーム  |
| net/hpack           | サブモジュール | HPACK ヘッダー圧縮  |
| net/proxy           | サブモジュール | プロキシサポート    |
| net/pool            | サブモジュール | コネクションプール  |
| net/unix            | サブモジュール | Unix ドメインソケット |
| net/ip              | サブモジュール | IP アドレス操作     |
| encoding/base64     | サブモジュール | Base64 エンコードとデコード |
| encoding/csv        | サブモジュール | CSV 解析            |
| encoding/pem        | サブモジュール | PEM エンコードとデコード |
| archive/tar         | サブモジュール | TAR アーカイブ      |
| archive/zip         | サブモジュール | ZIP アーカイブ      |
| archive/gzip        | サブモジュール | GZIP 圧縮           |
| archive/bzip2       | サブモジュール | BZIP2 展開          |
| archive/xz          | サブモジュール | XZ/LZMA 展開        |
| archive/zlib        | サブモジュール | zlib 圧縮（RFC 1950） |
| archive/zstd        | サブモジュール | Zstandard 展開      |
| map/linked-hash-map | サブモジュール | 順序付きハッシュテーブル |
| map/hash-set        | サブモジュール | i64 ハッシュ集合    |
| map/str-map         | サブモジュール | str→str ハッシュマップ |
| map/str-set         | サブモジュール | str ハッシュ集合    |
| map/tree-map        | サブモジュール | AVL 順序付きマップ  |
| map/tree-set        | サブモジュール | AVL 順序付き集合    |
| collection/queue    | サブモジュール | ジェネリックキュー  |
| collection/arr-stack| サブモジュール | ジェネリックスタック |
| collection/link     | サブモジュール | ジェネリック雙方向連結リスト |
| collection/map      | サブモジュール | ジェネリック動的ハッシュマップ |
| collection/static-hashmap | サブモジュール | ジェネリック固定容量ハッシュマップ |
| database/sql        | サブモジュール | データベースアクセスインターフェース |
| crypto/aes          | サブモジュール | AES ブロックコア（128/256） |
| crypto/aes-cbc      | サブモジュール | AES-CBC モード（128/256） |
| crypto/aes-ctr      | サブモジュール | AES-CTR モード（128/256） |
| crypto/aes-gcm      | サブモジュール | AES-GCM AEAD（128/256） |
| crypto/des            | サブモジュール | DES 暗号化と復号    |
| crypto/tdes           | サブモジュール | トリプル DES        |
| crypto/rsa            | サブモジュール | RSA モジュラー冪乗  |
| crypto/md5            | サブモジュール | MD5 ハッシュ        |
| crypto/sha1           | サブモジュール | SHA-1 ハッシュ      |
| crypto/sha224         | サブモジュール | SHA-224 ハッシュ    |
| crypto/sha256         | サブモジュール | SHA-256 ハッシュ    |
| crypto/sha384         | サブモジュール | SHA-384 ハッシュ    |
| crypto/sha512         | サブモジュール | SHA-512 ハッシュ    |
| crypto/sha3           | サブモジュール | SHA-3 ハッシュ      |
| crypto/blake2         | サブモジュール | BLAKE2 ハッシュ     |
| crypto/crc-16         | サブモジュール | CRC16 チェックサム  |
| crypto/crc-32         | サブモジュール | CRC32 チェックサム  |
| crypto/crc-64         | サブモジュール | CRC64 チェックサム  |
| crypto/fnv            | サブモジュール | FNV-1 ハッシュ      |
| crypto/fnv-1a-32      | サブモジュール | FNV-1a ハッシュ     |
| crypto/hmac           | サブモジュール | HMAC 認証コード     |
| crypto/hkdf           | サブモジュール | HKDF 鍵導出         |
| crypto/pbkdf2         | サブモジュール | PBKDF2 鍵導出       |
| crypto/argon2         | サブモジュール | Argon2 鍵導出       |
| crypto/scrypt         | サブモジュール | scrypt 鍵導出       |
| crypto/chacha20-poly1305 | サブモジュール | ChaCha20-Poly1305 |
| crypto/rc4            | サブモジュール | RC4 ストリーム暗号  |
| crypto/ecdsa          | サブモジュール | ECDSA 署名          |
| crypto/ed25519        | サブモジュール | Ed25519 署名        |
| crypto/x25519         | サブモジュール | X25519 鍵交換       |
| crypto/base32         | サブモジュール | Base32 エンコードとデコード |
| crypto/rand           | サブモジュール | 乱数生成器          |
| crypto/x509           | サブモジュール | X.509 DER 解析      |
