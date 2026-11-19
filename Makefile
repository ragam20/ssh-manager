PREFIX ?= $(HOME)/.local
BIN    := sshm

.PHONY: build install uninstall test clean

build:
	go mod tidy
	CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o $(BIN) ./cmd/sshm

install: build
	install -d $(PREFIX)/bin
	install -m 0755 $(BIN) $(PREFIX)/bin/$(BIN)
	@echo "installed at $(PREFIX)/bin/$(BIN) (make sure it is on PATH)"

uninstall:
	rm -f $(PREFIX)/bin/$(BIN)

test:
	go test ./...

clean:
	rm -f $(BIN)
