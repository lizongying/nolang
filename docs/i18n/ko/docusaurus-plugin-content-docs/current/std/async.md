---
sidebar_position: 4.2
---

## 비동기(Async)

Nolang에는 표면적인 동시성 문법이 하나뿐입니다. `awy { ... }` 코루틴 그룹이며, 그룹 안에서 `co`로 작업을 생성합니다.
`run` 키워드는 제거되었습니다(이제 일반 식별자). `awy <handle>`와 문장 위치의 벌어 블록 코루틴 그룹도 모두 폐지되었습니다.
**그룹 밖의 `co`는 컴파일 오류**입니다(`E_CO_OUTSIDE_GROUP`).

`co`는 **프로시저**만 시작합니다. 호출되는 쪽은 선언된 반환값이 있으면 안 됩니다(메서드가 암시적으로 반환하는 `self`는 예외).
따라서 결과는 항상 채널을 통해 전달됩니다. 프로시저가 값을 채널로 보낸 뒤 `close()`하고, 호출자가 그룹 밖에서 채널을 비웁니다.

### 코루틴 그룹

```no
; 過程：只寫通道，沒有宣告回傳值
produce = (ch chan[i64], v i64) {
    ch <- v
    ch.close()
}

ch1 chan[i64] = with-cap(1)
ch2 chan[i64] = with-cap(1)

; 並發：兩個任務一起跑，組尾統一等待
awy {
    co produce(ch1, 1)
    co produce(ch2, 2)
}

r1 = 0
<- ch1: { ok -> r1 = it }
r2 = 0
<- ch2: { ok -> r2 = it }

; 順序：後一個 spawn 用前一個的結果當實參，靠「讀通道」當屏障
awy {
    co produce(ch1, r1)
}
```

`ch1` / `ch2`에서 얻는 것은 결과값이지 task 핸들이 아닙니다. 자세한 내용은 `lang/syntax.md`의 "비동기 프로그래밍"을 참고하세요.

### 무색 async: `co`

`co`는 무색(colorless) 문법입니다. 색이 칠해지지 않은 함수 이름 `produce`를 쓰면 컴파일러가 자동으로
`produce-async` 변형을 단형화하고, 내부적으로 색이 있는 스택리스 코루틴으로 실행합니다. 애플리케이션 코드에 `-async` 접미사를 직접 쓰지 마세요.

두 가지 형태:

- `co f(args)` — 프로시저 spawn. `f`는 선언된 반환값이 있으면 안 됩니다(메서드의 암시적 `self`는 예외).
- `co { ... }` — 클로저 spawn. 블록 안의 자유 변수는 값으로 캡처합니다(spawn 시점의 스냅샷).

`co`는 값이 아니므로 독립된 문장이어야 합니다. `r = co f()`, `print(co f())`는 모두 `c0nostmt` 오류가 납니다.

규칙:

- **단형화**: `co produce(ch, 1)`은 자동으로 `produce-async`를 생성합니다(`produce`의 본체를 복사하고 이름을 바꿈). `produce`가 동기 호출로도 쓰이면 둘이 공존합니다.
- **전이성**: 어떤 프로시저 내부에도 `awy { co child(n) }`가 있으면 그 프로시저 자체도 `-async`로 단형화됩니다.
- **하위 lowering**(사용자 문법 아님): 각 `co`는 `OpRun`이 되고, 그룹 끝에 `OpAwait`가 삽입됩니다.

### 취소

`cancelled()`는 코루틴 함수 안에서 협력적 자기 검사에 사용할 수 있습니다. `cancel(h)`는 하위 핸들 조작이며,
새 문법은 task 핸들을 노출하지 않으므로 애플리케이션 코드에서는 사용하지 않습니다.

> **참고:** 취소는 선점형이 아니라 협력형입니다. 오래 블로킹되는 호출(네트워크 요청 등)은 강제로 중단할 수 없습니다.
> 작업은 다음 협력 검사점(`cancelled()` 호출 또는 이벤트 루프의 다음 스케줄링)에서 비로소 멈춥니다.

#### 채널로 직접 취소 구현하기(권장)

새 문법에는 `ch.cancel()`가 없고 채널에도 `cancel` 변형이 없습니다. "외부에서 신호를 보내 worker가 마무리하도록 할" 때는
**직접 채널을 하나 유지하고 `close()`를 취소 신호로 사용하세요**. 이것이 `chan[T]`의 유일한 취소 의미입니다.
수신 반복문의 세 가지 상태 중 `nil`은 "닫혔고 비어 있음"을 뜻하며, 바로 이것이 취소 분기입니다. `err`는 초기화되지 않은 핸들만 나타내므로
취소를 받는 데 쓰지 마세요.

```no
; 控制通道：關閉即取消
stop chan[i64] = with-cap(0)

worker = (stop chan[i64]) {
    <- stop: {
        ok  -> { }              ; 收到訊息（本例不用）
        nil -> print('cancel')  ; stop.close() 後落到這裡
        err -> { }              ; 未初始化的句柄
    }
}

stop.close()
awy {
    co worker(stop)
}
; 輸出 cancel
```

실행 중에 다른 작업이 취소를 유발하려면 닫기 동작도 그룹 안에 넣으세요(`awy` 뒤에 close하면 안 됩니다.
그룹 끝에서 모두 기다리므로 worker가 빈 채널에서 대기 중이면 교착 상태가 됩니다). `<- ch: { ... }`는 **수신 반복문**이라는 점에 유의하세요.
값을 하나 받은 뒤 다음 값을 계속 기다리므로, 알리는 쪽이 보낸 뒤 반드시 `close()`해야 canceller가 반복문을 빠져나와
`stop.close()`까지 실행합니다:

```no
stop chan[i64] = with-cap(0)
awake chan[i64] = with-cap(1)

worker = (stop chan[i64]) {
    <- stop: { ok -> { }  nil -> print('cancel')  err -> { } }
}

canceller = (awake chan[i64], stop chan[i64]) {
    <- awake: { ok -> { }  nil -> { }  err -> { } }   ; 等通知，收到後還要等 close 才會離開迴圈
    stop.close()                                       ; 這一句才是取消信號
}

notify = (awake chan[i64]) {
    awake <- 1
    awake.close()                                      ; 讓 canceller 的接收迴圈走到 nil
}

awy {
    co worker(stop)
    co canceller(awake, stop)
    co notify(awake)
}
; 輸出 cancel
```

worker가 **자신의 각 단계 시작에서** 정지 요청이 있었는지만 확인하려면 `cancelled()`가 더 직접적입니다.
하지만 이는 자기 검사만 가능하고 외부가 알릴 핸들이 없습니다. "다른 쪽이 신호를 보내는" 시나리오는 위의 채널 관례를 따르세요.

---
