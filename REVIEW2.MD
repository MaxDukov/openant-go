# План исправлений по результатам повторного код-ревью

Дата ревью: 2026-08-31. Объём: все пакеты (`ant`, `easy`, `fs`, `devices`,
`anttest`, `examples`), ~9,4k строк. Сверка с `REVIEW.MD` от 2026-08-31
(дифф: подтверждение ранее найденного, поиск пропущенного, проверка
устаревшего).

Цель этого документа — зафиксировать:
1. Что из REVIEW.MD актуально (исправлений нет).
2. Что в REVIEW.MD **пропущено** — реальные баги, которые требуют
   исправления.
3. Что в REVIEW.MD устарело / неточно.

Фокус ревью: безопасность (данные, приходящие по радиоканалу и от
USB-устройства) и соответствие лучшим практикам Go. Оптимизации ради
оптимизаций не предлагаются.

---

## Главный вывод

Кодовая база зрелая (граничные проверки в парсерах фреймов, ошибки
вместо паник, корректная синхронизация USB-драйвера). REVIEW.MD покрывает
~95% реальных проблем. Ниже — два **пропущенных** в REVIEW.MD пункта
(оба — реальные баги, не косметика) и одно расширение P3 (защитная
глубина).

---

## P0 — паники/DoS на внешних данных (приоритетные)

> Все позиции ниже подтверждены по коду и совпадают с REVIEW.MD.

### 1. `fs/manager.go:178–183` — паника на коротком beacon
Подтверждено: `len(data) == 0` + `ParseBeacon(data[:8])` без `len(data) >= 8`.
Исправление — проверка длины до среза или передача `data` в `ParseBeacon`
(у которого проверка уже есть).

### 2. `devices/common.go` — два места в `baseDevice.onData`
- строки 376–379: extended-блок читает `data[9]..[12]` без `len >= 13`.
- строки 414–470: общие страницы 80–83 читают `data[2]..[7]` без
  `len >= 8`.
Подтверждено. Исправление — добавить обе проверки.

### 3. `devices/scanner.go:64–69` — extended-поля без проверки длины
Подтверждено: `if len(data) <= 8 { return }`, затем `data[9]..[12]`.
Исправление — `if len(data) < 13 { return }`.

### 4. `fs/manager.go:401–413` (`Download`) — переполнение uint32 и OOM
Подтверждено. Исправление — промежуточные суммы в uint64, валидация
`Offset`/`Offset + Remaining ≤ Size`, ограничение максимального размера
файла.

### 5. `fs/manager.go:459–465` (`Upload`) — device-controlled смещения
Подтверждено. Исправление — `offset ≤ len(data)` и `maxBlock > 0` до
среза; сумма в uint64.

### 6. Страховка от паник в цикле диспетчеризации
Опционально, после 1–5. Подтверждено.

### 7. Fuzz-тесты парсеров
Подтверждено. Покрыть `ParseFrame`, `ParseBeacon`, `ParseCommand`,
`ParsePipeCommand`, `ParseDirectory`, `baseDevice.onData`,
`Scanner.scanData`, `Application.onData`.

---

## P1 — конкурентность, жизненный цикл, корректность протокола

### 8. `ant/driver_serial.go` — data race на `s.port` (REVIEW.MD п.8)
Подтверждено. `Open`/`Close`/`Read`/`Write` обращаются к `s.port` без
мьютекса; `usbDriver` для сравнения использует `sync.Mutex`. Исправление
— добавить мьютекс и захватывать локальную ссылку на `port` под
блокировкой перед I/O.

### 9. `ant/node.go:137–146` — busy-spin reader'а (REVIEW.MD п.9)
Подтверждено. Исправление — backoff на последовательных ошибках либо
остановка core с ошибкой наружу. Заодно убрать дубль `!c.running.Load()`
(строки 139 и 142).

### 10. `fs/manager.go:155–166` — deadlock в `NewApplication` (REVIEW.MD п.10)
Подтверждено: при ошибке `SetupChannel` вызывается `app.Stop()`, который
ждёт `<-a.runDone`, но горутина `node.Run` стартует ниже. Исправление —
запускать run-горутину до `SetupChannel` либо не ждать `runDone`, если
run loop ещё не стартовал (`sync.Once`/флаг).

### 11. **НОВОЕ** — баг приоритета операторов в дельте периода
Пропущено в REVIEW.MD. Два симметричных места с одинаковым багом:

`devices/fitness_equipment.go:285`
```go
deltaWheelPeriod := f.wheelPeriod[1] + 65536 - f.wheelPeriod[0]%65536
```
`devices/power_meter.go:100`
```go
deltaCrankPeriod := p.crankPeriod[1] + 65536 - p.crankPeriod[0]%65536
```
`%` имеет приоритет выше `-`. Парсится как
`wheelPeriod[1] + 65536 - (wheelPeriod[0] % 65536)`. Намеренная модулярная
дельта (корректный паттерн виден в `fitness_equipment.go:264` с явными
скобками) не работает: результат всегда `>= 65536`, а
`Power.AngularVelocity` / `Power.Torque` считаются неверно. Исправление
— обернуть целиком в `% 65536`:
```go
deltaWheelPeriod := (f.wheelPeriod[1] + 65536 - f.wheelPeriod[0]) % 65536
deltaCrankPeriod := (p.crankPeriod[1] + 65536 - p.crankPeriod[0]) % 65536
```
Серьёзность: реальное искажение числовых показателей FE-тренажёра и
измерителя мощности (страница 0x12).

### 12. Битовые баги (REVIEW.MD «P1 bitwise precedence»)
Все четыре подтверждены точными совпадениями строк:

- `devices/fitness_equipment.go:287`
  `f.Data.State = FitnessEquipmentState(data[7] & 0x70 >> 4)` —
  парсится как `data[7] & (0x70 >> 4)` = `data[7] & 0x07`. Нужно
  `(data[7] & 0x70) >> 4`.
- `devices/fitness_equipment.go:304` — тот же паттерн.
- `devices/dropper_seatpost.go:106`
  `d.Data.DelayIndicator = DelayIndicator(data[6] & 0x80 >> 7)` —
  парсится как `data[6] & 0x01`. Нужно `(data[6] & 0x80) >> 7`.
- `devices/shift.go:142`
  `FunctionSetEventType((b & 0x03) >> 4)` — всегда 0. Нужна маска
  `0x30` (старшие 2 бита ниббла).

### 13. `devices/lev.go:24–29` — инверсия в `LevErrorMessageFromByte` (REVIEW.MD «P2 logic inversion»)
Подтверждено. Возвращает `LevUnknown` для всех значений 0–15, включая
валидные `LevNoError`(0), `LevBatteryError`(1), `LevDriveTrainError`(2),
`LevBatteryEOL`(3), `LevOverheating`(4). Условие инвертировано. Корректная
логика — `if b < 5 { return LevErrorMessage(b) }; if b < 16 { return LevUnknown }; return LevManufacturerSpecific`.

---

## P2 — корректность протокола и API

### 14. `ant/message.go:259` — молчаливая усечка длины фрейма (REVIEW.MD п.11)
Подтверждено.

### 15. `Scanner.Save` ↔ `ReadJSONDevices` — несовместимые форматы (REVIEW.MD п.12)
Подтверждено.

### 16. Неограниченный рост буферов (REVIEW.MD п.13)
Подтверждено: `c.burst` (`ant/node.go:222`) и `eventBuffer.events`
(`easy/filter.go:33`) растут без cap.

---

## P3 — best practices и косметика

### 17. `ant/node.go:163` — мёртвая ветка `case err == ErrBadSync`
Подтверждено. `ParseFrame` (`ant/message.go:287`) оборачивает
`ErrBadSync` через `%w`, поэтому `err == ErrBadSync` всегда false.
Соседняя ветка `case err == ErrShortFrame:` (строка 161) живая
(`ParseFrame:284` возвращает `ErrShortFrame` напрямую, без обёртки).
Исправление — переписать обе ветки на `errors.Is(err, ...)`; ветка с
`ErrBadSync` станет живой и полезной.

### 18. **НОВОЕ** — профильные `onData` без собственной проверки длины
Пропущено в REVIEW.MD. `devices/bike_speed_cadence.go:194–197`
(`BikeSpeed.onData`), `:223–226` (`BikeCadence.onData`) и
`devices/heart_rate.go:56` (`HeartRate.onData`) читают `data[0]` (а
`heart_rate` ещё и `data[1]..[7]`) без проверки `len(data) >= 8`,
полагаясь на родительский guard `baseDevice.onData:366–368`.

Сейчас это безопасно (родитель отбрасывает пустые буферы до вызова
`onProfileData`), но контракт нигде не зафиксирован и любая будущая
правка, вызывающая эти `onData` напрямую, получит панику. Остальные
профили (`fitness_equipment.go:251`, `dropper_seatpost.go:92`,
`lev.go:182`, `core_temp_environment.go:72`,
`tire_pressure_monitor.go:75`, `bicycle_lights.go:188`) такой guard
имеют. Исправление — добавить `if len(data) < 8 { return }` в начало
трёх указанных `onData`.

### 19. Мёртвый код (staticcheck U1000) (REVIEW.MD P3)
Подтверждено: `ant/message.go:341` `uint16LE`/`uint32LE` (используется
`binary.LittleEndian.Uint16/32` напрямую, например в `fs/command.go`);
`easy/filter.go:81` `eventBuffer.snapshot` (нет вызывающих).

### 20. `resources/42-ant-usb-sticks.rules` — `MODE="0666"` (REVIEW.MD P3)
Подтверждено.

### 21. README ↔ код — рассинхрон версий и CLI (REVIEW.MD P3)
Подтверждено: README заявляет `Go >= 1.22`, `go.mod` — `go 1.25`;
описан CLI `cmd/goant`, отсутствующий в репозитории.

---

## Что проверено и **не требует действий** (дополнение к REVIEW)

- `ParseFrame`/`ParseFrames` (`ant/message.go:282–329`) — граничные
  проверки корректны, ресинхронизация работает.
- `ParseBeacon` (`fs/beacon.go:39`) — явная проверка `len(data) < 8`.
- `ParseCommand` (`fs/command.go:400`) — для каждого `Kind` есть
  проверка минимальной длины.
- `ParsePipeCommand` (`fs/commandpipe.go:212`) — проверка `len < 8` и
  `len < 14/16` где требуется.
- `ParseDirectory`/`ParseFile` (`fs/file.go:112,59`) — проверка на 16
  байт.
- `ParseCapabilities` (`ant/capabilities.go:84`) — `len < 6 → error`.
- `SerialNumber` (`ant/capabilities.go:115`) — `len < 4 → 0`,
  безопасно.
- `AntVersion` (`ant/capabilities.go:104`) — корректно обрабатывает
  буфер любой длины.
- Все профильные парсеры, кроме трёх из п.18, имеют явный
  `if len(data) < 8 { return }`.
- `easy/filter.go` `eventBuffer.push/waitFor` — под `mu`; `notify`
  корректно ротируется, пропущенных событий нет (событие добавляется
  до смены `notify` под тем же `mu`).
- `easy/node.go` `Run`/`pushData` — имеют `stopCh` селект, утечек
  горутин нет.
- `ant/core.go` — гонок между `reader` и `dispatcher` нет (общаются
  через канал `events`); `drainTimeslot` корректно лочит `txMu`;
  `dispatch` читает только из аргумента `m` и `c.burst` (writer-only из
  `reader`).
- `usbDriver.Read` (`ant/driver_usb_gousb.go:182`) — захватывает
  ссылку на `u.in` под мьютексом перед `in.Read`, гонок нет.
- `go vet` чисто, `staticcheck` U1000 совпадает с REVIEW.
- Нет `unsafe`, нет `exec`, нет сетевого кода, нет reflection над
  пользовательскими типами.
- Ключи ANT+/ANT-FS задокументированы как public-by-design, в логах не
  светятся.

---

## Что в REVIEW.MD **неточно / заслуживает уточнения**

- В REVIEW.MD «P3» упоминается, что `err == ErrBadSync` мёртвая ветка.
  Это верно. Но соседняя ветка `err == ErrShortFrame` — **живая**
  (`ParseFrame:284` возвращает sentinel напрямую, без `%w`). REVIEW
  корректен, но в коде стоит рядом обе — упрощение в REVIEW можно
  сохранить; для правки см. п.17.
- В REVIEW.MD сказано «`devices/shift.go:142` — P1 неправильная
  маска/сдвиг». По тексту: маска `0x03` действительно неправильная
  (нужна `0x30`), сдвиг `>> 4` корректный. Описание в REVIEW точно.

---

## Дифф относительно REVIEW.MD (кратко)

| Категория | Что добавлено / изменено |
|---|---|
| **Новый P1** | п.11 — баг приоритета операторов в дельте периода (`fitness_equipment.go:285`, `power_meter.go:100`) |
| **Новый P3** | п.18 — профильные `onData` без собственной проверки длины (`bike_speed_cadence.go` `BikeSpeed`/`BikeCadence`, `heart_rate.go`) |
| **P3 уточнение** | п.17 — мёртвая ветка `err == ErrBadSync` теперь с предлагаемой правкой через `errors.Is` |
| **Без изменений** | все P0 (1–7), P1 (8–10, 12–13), P2 (14–16), остальные P3 из REVIEW.MD подтверждены |

---

## Порядок выполнения

1. **P0 (1–5)** — точечные проверки длин/смещений; локальные правки без
   смены архитектуры.
2. **P0 (6–7)** — страховка + fuzz-тесты.
3. **P1 (новое 11)** — баг приоритета операторов в дельте периода
   (два файла); один коммит.
4. **P1 (8–10, 12–13)** — синхронизация serial-драйвера, backoff
   reader'а, исправление deadlock, битовые баги, инверсия в
   `LevErrorMessageFromByte`.
5. **P2 (14–16)** — валидация длин фреймов, формат devices.json, лимиты
   буферов.
6. **P3 (17–21)** — по ходу работы над соответствующими файлами;
   п.18 (защитная глубина профильных `onData`) можно объединить с
   п.1–2 в один коммит.
