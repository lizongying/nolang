---
sidebar_position: 3
---

# 표준 라이브러리

Nolang 표준 라이브러리(`src/std/`)는 80개 이상의 모듈을 포함하며, 포맷팅, 수학, 문자열, 자료 구조, 인코딩/디코딩, 암호화, 압축, 파일 조작, I/O 추상화, 비동기 코루틴, 파일 유형 감지 등을 다룹니다.

사용법: `# std/xxx`(핵심 모듈은 임포트할 필요가 없습니다).

> **기존 방식인 `use std/xxx`도 여전히 사용할 수 있지만, 더 이상 사용되지 않으므로(deprecated) 새 방식인 `# std/xxx` 문법을 사용할 것을 권장합니다.**

> **주의: 본 문서의 코드 예제는 모두 "한 줄에 하나의 문장" 규칙을 준수합니다. 세미콜론 `;`이나 쉼표 `,`로 여러 문장을 한 줄에 작성하는 것은 금지됩니다.** 예를 들어 `out = from-i64(v), out = from-u64(v)`는 잘못된 작성법이며, 여러 줄로 분리해야 합니다.

---

## 모듈 분류

- [기본 타입](basic-types)
- [핵심 라이브러리](core-library)
- [운영체제와 파일](os-and-files)
- [시간과 날짜](time-and-date)
- [로깅](logging)
- [자료 구조](data-structures)
- [데이터베이스](database)
- [인코딩](encoding)
- [아카이브](archives)
- [암호학과 해시](crypto)
- [데이터 교환](data-exchange)
- [기타](others)
- [모듈 개요](module-overview)
