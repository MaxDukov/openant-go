# openant-go

[![CI](https://github.com/MaxDukov/openant-go/actions/workflows/ci.yml/badge.svg)](https://github.com/MaxDukov/openant-go/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/maxdukov/openant-go.svg)](https://pkg.go.dev/github.com/maxdukov/openant-go)
[![Go Report Card](https://goreportcard.com/badge/github.com/maxdukov/openant-go)](https://goreportcard.com/report/github.com/maxdukov/openant-go)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

[English](README.md) | Русский

Библиотека ANT и ANT-FS для Go — порт Python-библиотеки
[openant](https://github.com/Tigge/openant).

> О ANT/ANT-FS/ANT+: библиотека предназначена для разработки и
> тестирования устройств и не претендует на роль эталона. Полную
> документацию по ANT и профилям ANT+ смотрите на
> [thisisant.com](https://www.thisisant.com/). Это неофициальный инструмент.

## Возможности

- Базовый ANT-интерфейс (фреймы, USB/serial-драйверы, конвейер событий):
  поддержка нескольких стиков (`ant.Sticks`, выбор по serial или
  bus:addr), настраиваемые таймауты чтения USB, метрики ошибок и потерь
  (`ant.Core.Metrics`), proximity search, списки channel ID, search
  sharing, LIB config (расширенные данные RSSI/таймстамп/channel-ID) и
  advanced burst — с автоматическим определением ревизии протокола
  (Rev 5.1 против современных message id).
- ANT-FS (command pipe, листинг каталога, download, upload, erase, ...).
- Профили ANT+ устройств и базовый тип для своих (`devices`), включая
  декодирование измерений тонометра.
- Эмуляция устройств (master mode): пульсометр
  (`devices.NewHeartRateMaster`), управление тренажёром (FE-C: target
  power, wind/track resistance, user config, capabilities), generic
  broadcast мастера.
- Четыре пакета, повторяющие структуру openant:
  - `ant` — базовая ANT-библиотека (openant.base),
  - `easy` — блокирующий интерфейс с колбэками (openant.easy),
  - `fs` — библиотека ANT-FS (openant.fs),
  - `devices` — профили ANT+ (openant.devices).
- `anttest` — скриптуемый in-memory драйвер и симулятор стика для
  тестов.
- CLI `goant` (`scan`, `sticks`, `antfs-scan`, `influx`, `mqtt`, `udev`,
  `version`).
- 14 примеров приложений в `examples/` (порты примеров openant).

## Требования

- Go >= 1.25 с включённым cgo и установленной libusb (macOS:
  `brew install libusb`, Debian/Ubuntu: `sudo apt install
  libusb-1.0-0-dev`).
- ANT USB-стик (для тестов не обязателен):
  - ANTUSB2 (0fcf:1008) или ANTUSB-m (0fcf:1009),
  - serial/CDC-стики (0fcf:1004) на Linux.
- На Linux установите `resources/42-ant-usb-sticks.rules` в
  `/etc/udev/rules.d/`, чтобы стики работали без root — CLI делает это
  сам: `sudo goant udev` (или `sudo make install-udev`).

## Установка

```sh
go get github.com/maxdukov/openant-go
```

Документация пакетов: https://pkg.go.dev/github.com/maxdukov/openant-go

## Использование

```go
node, err := easy.New()
if err != nil {
    log.Fatal(err)
}
defer node.Stop()
node.SetNetworkKey(0x00, devices.ANTPLUS_NETWORK_KEY)

hr, err := devices.NewHeartRate(node, 0 /* первый найденный */, 0)
if err != nil {
    log.Fatal(err)
}
hr.OnDeviceData = func(page int, name string, data devices.DeviceData) {
    fmt.Printf("Пульс: %d уд/мин\n", data.(devices.HeartRateData).HeartRate)
}

ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
defer stop()
node.Run(ctx) // блокирующий цикл обработки событий
```

Эмуляция датчика (master mode) столь же компактна:

```go
hrm, err := devices.NewHeartRateMaster(node, 0 /* случайный device id */)
if err != nil {
    log.Fatal(err)
}
hrm.SetHeartRate(150)
```

Полный набор примеров — в `examples/` (пульс, сканер, листинг ANT-FS,
broadcast в мастер-режиме, непрерывное сканирование, тренировки на
тренажёре, ...).

## CLI

```sh
go install github.com/maxdukov/openant-go/cmd/goant@latest

goant version                  # версия
goant sticks                   # список подключённых стиков (serial, bus:addr)
goant scan                     # вывод найденных устройств в терминал
goant scan -auto_create        # плюс вывод страниц данных устройств
goant scan -t HeartRate        # искать только пульсометры
goant scan -i 12345            # искать конкретный device id
goant scan -s <serial>         # сканировать на конкретном стике
goant scan -all                # сканировать на всех стиках (multi-dongle)
goant scan -o devices.json     # сохранить найденные устройства в файл
goant antfs-scan               # слушать маяки ANT-FS (файловые передачи)
sudo goant udev                # установить udev-правила для стиков (Linux)
goant influx -db ant HeartRate # стримить пульсометр в InfluxDB (v1 API)
goant influx -token <tok> -bucket ant -org me BikeSpeedCadence
goant mqtt -host broker.local HeartRate   # JSON-события в openant/HeartRate/<id>
goant mqtt -topic-per-field -device-topic HeartRate:123:sensors/hr HeartRate
goant influx -config devices.json -all    # стримить сохранённый список устройств
```

Стики без читаемого USB serial (некоторые клоны CYCPLUS)
адресуются по bus:addr, например `goant scan -serials 1:5`.

## Тестирование

```sh
make test          # юнит-тесты (симулятор, без железа)
make test-race     # с race-детектором
make bench         # бенчмарки: парсер фреймов, декодеры страниц, InfluxFields
make fuzz          # быстрый fuzz-прогон всех парсерных таргетов
make integration   # нужен реальный ANT USB-стик и ANT_TEST_USB_STICK=1
```

Юнит-тесты полностью работают против симулятора `anttest.SimDriver`;
железо нужно только для `integration` (build tag).

CI запускает `go test -race`, staticcheck и fuzz smoke (15 с на
парсерный таргет); воркфлоу по расписанию каждую ночь фуззит те же
таргеты с бюджетом 3 минуты каждый, хранит инкрементальные корпуса в
build cache и выгружает падающие входы в артефакты.

## Работа с временем (таймзоны)

Все метки времени в openant-go — UTC (issue #119 openant). ANT+-устройства
не передают информацию о таймзоне: страница времени и даты (83)
интерпретируется как UTC (`devices.CommonData.TimeDate`), как и эпоха
ANT-FS в пакете `fs`. Локальное время: `CommonData.Local()` /
`time.Time.Local`, либо `goant scan -localtime`.

## Заметки о дизайне (в сравнении с Python-оригиналом)

- Потоки → горутины; `queue.Queue`/`deque+Condition` → каналы и
  защищённый мьютексом буфер событий с broadcast-уведомлением.
- Stop-флаги → `context.Context` / `atomic.Bool` / закрытие каналов.
- Все бинарные парсеры возвращают ошибки вместо паник (в openant —
  `assert`), читатель фреймов ресинхронизируется на плохом
  sync/checksum.
- Исправленные баги openant: capabilities byte 6, парсинг pipe
  `CreateFile`, out-of-bounds чтение в `shift` странице 3, payload
  ответа 0x47 `controls_device`, гонки на capability-полях.
- Переопределение колбэков (питоновское наследование) → hook-поля
  функций и функциональные опции (`fs.WithOnTransport`, ...).

## Лицензия

MIT — как и openant.
