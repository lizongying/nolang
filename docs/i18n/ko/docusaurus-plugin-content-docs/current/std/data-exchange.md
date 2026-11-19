---
sidebar_position: 4.1
---

## 데이터 교환

### json — JSON 파싱과 생성

`json`은 **힙에 할당된 노드 풀**(`json-pool`)에 JSON 트리를 저장하며, 중첩 배열과 객체, 문자열/숫자/불리언/null 스칼라, 이스케이프 문자와 지수 형식 숫자를 지원합니다. 노드 풀은 동적 `vec`를 사용하도록 변경되어 **고정 용량 상한이 없습니다**——노드 총수와 단일 노드의 자식 요소 수는 사용 가능한 메모리에 의해서만 제한됩니다. 고급 인터페이스는 `json` 구조체로 노드 풀과 루트 노드 인덱스를 캡슐화하며, `pool` 필드에 `#{inline=false}`(포인터 필드)가 표시되어 있어 `json`을 값으로 안전하게 반환하고 복사할 수 있습니다.

```no
; 解析：回傳 ?json（ok=成功，err=失敗並附原因）
j = json.parse('{"name":"Alice","age":30,"items":[1,2,3],"active":true}')
j: {
    ok -> {
        name = it.get-str('name'); 取標量：get-str / get-num / get-bool / get-i64（?值）
        age = it.get-i64('age'); 整數取值（?i64）
        items = it.get('items'); 取子節點，回傳 ?json
        s = it.stringify(); 序列化回字串
    }

    err(e) -> print(e)
}

; 產生：從空 json 開始，逐步 set / arr-push
j2 = json.new()
j2.set-str('key', 'value')
j2.set-i64('count', 3)
j2.arr-push-str('a')
print(j2.stringify())
```

**고급 API (`json` 구조체)**

- `json.new()` — 빈(null) `json`을 생성
- `json.parse(s)` — 파싱하여 `?json`을 반환하며, 실패 시 `err` 분기에 구체적인 원인이 담깁니다(`empty input` / `parse error` / `trailing characters`)
- `j.stringify()` — 문자열로 직렬화
- 값 가져오기: `j.get-str(key)` / `j.get-num(key)` / `j.get-bool(key)` / `j.get-i64(key)`(각각 `?val` 반환); `j.get(key)`는 자식 노드 `?json`을 반환
- 스칼라 직접 가져오기(root가 스칼일 때): `j.str()` / `j.num()` / `j.bool()` / `j.i64()` / `j.str-val()`
- 쓰기: `j.set-str(key,val)` / `j.set-num` / `j.set-i64` / `j.set-bool` / `j.set-null` / `j.set-key(key, val json)`
- 배열: `j.arr-get(i)` / `j.arr-len()` / `j.arr-push(val json)` / `j.arr-push-str` / `j.arr-push-num` / `j.arr-push-bool`
- 객체 열거: `j.obj-len()` / `j.obj-key(i)` / `j.obj-keys-str()` / `j.delete-key(key)`
- 타입 판별: `j.kind()` 및 `j.is-null()` / `j.is-obj()` / `j.is-arr()` / `j.is-str()` / `j.is-num()` / `j.is-bool()`

**풀 용량 (고정 상한 없음)**

초기 버전은 고정 배열을 풀로 사용해 `JSON-MAX-NODES = 128` / `JSON-MAX-CHILDREN = 32`로 하드 제한했고, 초과하면 조용히 실패했습니다. 현재는 모두 힙 `vec`로 변경되었습니다:

- 노드 총수와 노드별 자식 요소 수 모두 **상한이 없으며**, 사용 가능한 메모리에 의해서만 제한됩니다.
- `json.parse`는 실제 문법 오류가 있을 때만 `err`를 반환하며, 더 이상 "풀 고갈" 오류 분기가 없습니다.
- `j.set*` / `j.arr-push*`는 구성 대상 타입이 일치하지 않을 때만 `false`를 반환합니다(예: 객체가 아닌 노드에 set, 배열이 아닌 노드에 push). `j.overflowed()`는 하위 호환을 위해 유지되며, 현재는 항상 `false`를 반환합니다.

> **`json`의 복사 의미 (`#{inline=false}`)**: `pool`은 포인터 필드이며, `j.get` / `j.arr-get`이 반환하는 자식 노드 `json`은 전체 풀을 **깊은 복사**하므로 부모 풀과 독립적인 스냅샷입니다——스냅샷에 대한 쓰기는 부모 노드에 반영되지 않습니다. 또한 `?json`(option)은 `match` 시 값을 소비하므로, 동일한 option에 대해 **반복해서 `match`**하면 `nil` 분기로 진입합니다. 동일한 파싱 결과를 여러 번 조작해야 한다면 단일 `match` 안에서 처리하거나, 먼저 `json` 지역 변수에 `it`을 받아 두세요.

**저수준 API (`json-pool` / `json-value`)**

노드 인덱스로 직접 조작하며, 풀은 지역 변수로 사용합니다(단일 계층 구조체는 값에 의한 반환/복사가 안전합니다):

- `p = json.new-pool()`; 또는 `p = json-pool {}` 후 `p.init()`(`nodes`/`strs`/`ec`/`ek`/`en` 각 vec를 `with-len(0)`으로 초기화)
- `idx = p.alloc()` — 새 노드를 할당하고 인덱스를 반환(힙 vec가 자동으로 확장)
- `p.add-child(node-idx, child-idx, key)` — 간선 표(`ec`/`ek`/`en` 단일 연결 리스트)로 자식 요소를 추가하며, 노드별 상한이 없습니다
- `node-idx, next-pos, ok = p.parse(s, pos)` — `pos`에서 값 하나를 파싱하여 노드 인덱스를 반환
- `val-idx = p.get-key(obj-idx, key)`(`?i64`); `p.arr-get(arr-idx, i)`(`?i64`); `p.arr-len(arr-idx)`
- `p.get-kind(idx)` / `p.get-str(idx)` / `p.get-num(idx)` / `p.get-bool(idx)`
- `p.set-key(obj-idx, key, val-idx)`; `p.copy-tree(src, idx)`(하위 트리를 재귀적으로 깊은 복사)
- `p.stringify(node-idx)` — 지정한 노드를 직렬화

### toml — TOML 1.0 파싱과 생성

`toml`은 TOML 문서를 `toml`에 저장하며, 주석, bare/quoted/dotted keys, 일반 테이블, array-of-tables, basic/literal/multiline strings, 다양한 정수 진법, 부동소수점, 불리언 값, 날짜/시간, 배열, inline table을 지원합니다. 전용 Nolang 타입이 제공되지 않은 값은 원래 리터럴을 유지합니다.

```no
cfg = toml.parse('title = "Nolang"\n[server]\nport = 8080\n[[server.backends]]\nname = "local"')

doc = toml.new()
doc.put('name', '"nolang"')
doc.put('server.port', '8080')
doc.put('enabled', 'true')
raw = toml.get(doc, 'server.port')
text = toml.stringify(doc)
```

주요 함수:

- `toml.new()` — 빈 문서 생성
- `toml.parse(text)` — 파싱하여 `?toml` 반환
- `doc.put(key, raw-value)` — 검증하고 TOML 값을 추가/업데이트
- `get(doc, key)` — 원래 값 가져오기
- `get-str`, `get-i64`, `get-f64`, `get-bool` — 스칼라 값 가져오기
- `toml.stringify(doc)` — 문서 직렬화

문서 상한은 key 4개, 일반 테이블 2개, array-of-tables 2개입니다. key는 최대 32바이트, value는 최대 128바이트입니다.

### yaml — YAML 1.2 파싱과 생성

`yaml`은 노드 풀(`pool`)에 YAML 1.2 core schema 문서 트리를 저장하며, 블록 매핑, 블록 시퀀스, 흐름 컬렉션(`[...]` / `{...}`), 작은따옴표/큰따옴표/plain 스칼라, 블록 스칼라(`|` / `>` 및 `-` / `+` chomping), 주석, 문서 마커(`---` / `...`)와 다중 문서, 앵커(`&`)와 별칭(`*`), 병합 키 `<<`, 명시적 키(`? k`), 타입 추론(null / bool / int / float / `.inf` / `.nan`), 점 경로 조회, 그리고 JSON / YAML 직렬화를 지원합니다.

```no
doc ?yaml = yaml.parse('name: Alice\nserver:\n  port: 8080\ntags:\n  - a\n  - b\n')
doc: {
    nil -> print('nil')

    err -> print(it.msg())

    -> {
        doc = it
        name = doc.find-str('name'); ?str
        port = doc.find-i64('server.port'); ?i64
        name: {
            ok -> print(it)

            -> print('')
        }
        port: {
            ok -> print(it.to-str())

            -> print('')
        }
        print(doc.to-json())
        print(doc.to-yaml())
    }
}
```

주요 함수와 메서드:

- `yaml.parse(text)` / `yaml.parse-all(text)` — 파싱하여 `?yaml`을 반환(`err` 분기에 `line L, column C` 위치 포함)
- `yaml.valid-of(text)` / `yaml.error-of(text)` — 검증만 수행하고 문서를 구성하지 않음
- `yaml.strip-bom(text)` — UTF-8 BOM 제거
- `doc-count()` / `doc(i)` — 다중 문서 접근
- `kind()`, `is-null()`, `is-bool()`, `is-int()`, `is-float()`, `is-nan()`, `is-str()`, `is-seq()`, `is-map()`, `len()`, `text()`, `tag()`, `anchor()`, `line-of()` — 노드 정보
- `str()`, `i64()`, `f64()`, `bool()`, `scalar-text()` — 값 가져오기, 항상 `?값` 반환
- `at(i)`, `value(i)`, `key(i)`, `key-node(i)`, `get(key)`, `find(path)` — 자식 노드 / 키 가져오기
- `get-str` / `get-i64` / `get-f64` / `get-bool`, `find-str` / `find-i64` / `find-f64` / `find-bool` — 편리한 값 가져오기
- `to-json()`, `to-json-all()`, `to-yaml()`, `stringify()`, `source-of()` — 직렬화

노드 타입 상수: `YAML-KIND-NULL` / `BOOL` / `INT` / `FLOAT` / `STR` / `SEQ` / `MAP`(0–6); 스칼라 스타일 상수 `YAML-STYLE-PLAIN` / `SINGLE` / `DOUBLE` / `LITERAL` / `FOLDED` / `ALIAS`(0–5) 및 `YAML-STYLE-BLOCK` / `FLOW`(6–7).

> **주의 1**: 자식 노드 뷰는 `?yaml`로 반환되고, 값 가져오기 도우미 함수는 `?값`을 반환하므로 match로 언패킹합니다(`child = d.at(i)` — `ok` 암은 `it`에 자식 노드를 바인딩하고, 기본 암은 빈 값을 처리하며, 각 암은 별도 줄에 씁니다). 자식 노드와 부모 문서는 노드 풀을 공유하므로, 같은 match 안에서 값을 가져오거나 먼저 한 벌을 바인딩해 두는 것이 좋습니다. 풀이 여러 번 변경되는 동안 만료된 handle을 들고 있지 마세요. 값은 `find-str` / `find-i64` 같은 경로 편의 함수를 사용하는 것이 좋습니다.
>
> **주의 2**: 중첩 match에서 `it`은 **계층별로 복원**됩니다. 내부 match의 암은 `it`을 자신의 대상으로 다시 바인딩하며(암 본문은 내부 값을 보아야 합니다), 해당 암이 끝나면 `it`은 외부 암의 대상으로 복원됩니다. 따라서 아래와 같은 작성법은 안전합니다:
>
> ```no
> d ?yaml = yaml.parse(src)
> d: {
>     nil -> print('nil')
>
>     err -> print(it.msg())
>
>     -> {
>         print(it.find-str('a'))        ; 外層 it = 文件
>         v ?yaml = it.at(0)
>         v: {
>             nil -> print('none')
>             err -> print('err')
>             -> print(it.find-str('b')) ; 內層 it = 子節點
>         }
>         print(it.find-str('c'))        ; ✅ it 已還原成文件
>     }
> }
> ```
>
> 유일한 예외는 가장 바깥쪽 match입니다. 이 match가 끝난 후에도 `it`은 마지막 암의 값을 유지합니다(복원되지 않음). 따라서 match 밖에서 `it`에 의존하지 마세요. 계층을 넘어 사용해야 한다면 명시적으로 이름이 있는 지역 변수에 저장(`doc = it`)하거나 구조 분해 바인딩 `ok(v) -> ...`을 사용하세요.
>
> **주의 3**: `.nan`은 실제 NaN으로 파싱됩니다(`f64()`가 NaN을 반환하고 `x != x`가 참). JSON에는 NaN이 없으므로 `to-json()`은 이에 대해 `null`을 출력하고, `to-yaml()`은 `.nan`을 출력합니다.

---
