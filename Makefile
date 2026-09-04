BINARY := goant

.PHONY: all build test test-race vet lint examples integration install-udev clean

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
