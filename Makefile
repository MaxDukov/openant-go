BINARY := goant

.PHONY: all build test test-race bench fuzz vet lint examples integration install-udev clean

all: vet build

build:
	go build ./...

vet:
	go vet ./...

lint: vet

test:
	go test ./...

test-race:
	go test -race ./...

bench:
	go test -run '^$$' -bench . -benchtime 1s ./...

# Quick fuzz smoke over every parser target (CI runs the same targets
# with bigger budgets, see .github/workflows/fuzz.yml).
fuzz:
	go test -run '^FuzzParseFrame$$' -fuzz '^FuzzParseFrame$$' -fuzztime 15s ./ant
	go test -run '^FuzzBaseDeviceOnData$$' -fuzz '^FuzzBaseDeviceOnData$$' -fuzztime 15s ./devices
	go test -run '^FuzzScannerScanData$$' -fuzz '^FuzzScannerScanData$$' -fuzztime 15s ./devices
	go test -run '^FuzzParseBeacon$$' -fuzz '^FuzzParseBeacon$$' -fuzztime 15s ./fs
	go test -run '^FuzzParseCommand$$' -fuzz '^FuzzParseCommand$$' -fuzztime 15s ./fs
	go test -run '^FuzzParsePipeCommand$$' -fuzz '^FuzzParsePipeCommand$$' -fuzztime 15s ./fs
	go test -run '^FuzzParseDirectory$$' -fuzz '^FuzzParseDirectory$$' -fuzztime 15s ./fs
	go test -run '^FuzzApplicationOnData$$' -fuzz '^FuzzApplicationOnData$$' -fuzztime 15s ./fs

# Integration tests require a real ANT USB stick. ANT_TEST_USB_STICK must be
# unset or set to a non-"0" value for them to run.
integration:
	go test -tags integration ./...

examples:
	go build ./examples/...

# Install the udev rules for ANT USB sticks (Linux, needs root):
#   sudo make install-udev
install-udev:
	go run ./cmd/goant udev

clean:
	rm -f $(BINARY)
