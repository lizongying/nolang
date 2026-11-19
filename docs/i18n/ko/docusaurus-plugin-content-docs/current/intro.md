---
sidebar_position: 1
---

# Nolang 소개

Nolang은 실험적인 시스템 프로그래밍 언어로, GC 없이 메모리 안전을 제공하고 의미는 직관적이며 문법은 극도로 간결합니다.

## 핵심 특징

- **메모리 안전**：가비지 컬렉션에 의존하지 않으며 자동 메모리 관리를 제공합니다
- **강력한 성능**：컴파일 타임 최적화, GC 없음
- **극도로 간결한 문법**：키워드가 더 적고, 포인터·소유권·수명 주기 같은 암묵적인 부담이 없어 문법이 더 단순합니다
- **의도 중심**: 개발자의 의도를 최대한 존중하며, 의미가 인간의 직관에 부합합니다
- **절대 충돌하지 않음**：암묵적인 충돌 경로를 제거합니다

## 빠른 시작

```no
; 안녕, 세계!
; main 진입점이 필요 없습니다
print('Hello, Nolang!')

; 변수 선언
count i64

; 함수 정의
add = (a i64, b i64) (result i64) {
    result = a + b
}

; 표준 라이브러리 메서드는 직접 호출할 수 있습니다
c = math.max(a, b)

; 구조체
user {
    name str
    age i64
}

; 메서드
user.greet = () {
    print('Hello, {.name}')
}

u = user {
    name: 'Alice'
    age: 30
}

u.greet()
```

## 활용 사례

- [notools](https://github.com/lizongying/notools) — Nolang 언어로 구현한 Unix 표준 명령줄 도구 모음으로, cat, ls, grep, wc, head, tail 같은 고전적인 도구를 포함하여 시스템 프로그래밍 분야에서 Nolang의 실제 활용력을 보여줍니다.
