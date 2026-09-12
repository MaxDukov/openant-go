# Contributing to openant-go

Thanks for your interest in contributing! Issues and PRs in **English
or Russian** are equally welcome — write in whichever you are most
comfortable with.

## Development setup

Requirements: Go >= 1.25, cgo enabled, libusb installed
(macOS: `brew install libusb`, Debian/Ubuntu: `sudo apt install
libusb-1.0-0-dev`).

```sh
git clone git@github.com:MaxDukov/openant-go.git
cd openant-go
go build ./... && go test ./...
```

An ANT USB stick is **not** required for development: the unit tests
run entirely against the `anttest.SimDriver` in-memory stick simulator.

## Things to know before you start

- The four packages mirror the Python
  [openant](https://github.com/Tigge/openant): `ant`, `easy`, `fs`,
  `devices`. Keep API-level compatibility in mind when touching public
  signatures.
- All binary parsers must return errors, never panic — incoming bytes
  are attacker- and noise-controlled. Add a fuzz target when you add a
  parser (see the existing `fuzz_test.go` files).
- Timestamps are UTC everywhere; see the "Time handling" section of
  README.md.
- Planned work lives in [TODO.md](TODO.md). If your idea is on the
  roadmap, mention the item.

## Testing

```sh
make test          # unit tests (simulator, no hardware)
make test-race     # with the race detector
make bench         # benchmarks
make fuzz          # quick fuzz smoke over every parser target
make integration   # real ANT USB stick required, ANT_TEST_USB_STICK=1
```

CI runs `go test -race`, vet, staticcheck and golangci-lint; the fuzz
workflow smoke-runs every parser target on PRs. PRs must keep all of
them green.

## Commit and PR style

- Conventional-commit-ish subjects as used in the history:
  `feat:`, `fix:`, `docs:`, `ci:`, `cleanup:` — lowercase, imperative,
  concise.
- Keep PRs focused; one logical change per PR.
- Fill in the PR template checklist.

## Good first issues

Issues labelled [`good first issue`](https://github.com/MaxDukov/openant-go/labels/good%20first%20issue)
are small, well-scoped and simulator-testable. Claim one by commenting
before you start.

## Device reports

If a sensor does not connect or sends unknown pages, use the
**Device support** issue template — those reports often improve the
library for everyone (see the Magene/BSC stories in TODO.md).

---

## По-русски (кратко)

- Вопросы и PR можно писать **на русском** — это полностью нормально.
- Железо не нужно: юнит-тесты идут на симуляторе `anttest` (`make test`).
- Все парсеры бинарных данных обязаны возвращать ошибки, не паниковать.
- Список задач — в [TODO.md](TODO.md); метка
  [`good first issue`](https://github.com/MaxDukov/openant-go/labels/good%20first%20issue)
  — хорошие задачи для старта.
- Стиль коммитов: `feat:`, `fix:`, `docs:`, `ci:` — см. историю.
