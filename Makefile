.PHONY: all build test lint bench clean install local help

BINARY := hledger-lsp
BIN_DIR := ./bin
CMD_DIR := ./cmd/hledger-lsp

help:
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@echo "  build    Build binary to $(BIN_DIR)/$(BINARY)"
	@echo "  test     Run all tests"
	@echo "  lint     Run golangci-lint with autofix"
	@echo "  bench    Run benchmarks"
	@echo "  clean    Remove build artifacts"
	@echo "  install  Install to GOPATH/bin"
	@echo "  local    Build binary to ~/Library/Application\ Support/Code/User/globalStorage/evsyukov.hledger/$(BINARY)"
	@echo "  all      Run lint, test, and build"

build:
	go build -o $(BIN_DIR)/$(BINARY) $(CMD_DIR)

test:
	go test ./...

lint:
	golangci-lint run --fix ./...

bench:
	go test -bench=. ./...

clean:
	rm -rf $(BIN_DIR)

install:
	go install $(CMD_DIR)

local:
	go build -o $(BIN_DIR)/$(BINARY) $(CMD_DIR)
	cp $(BIN_DIR)/$(BINARY) ~/Library/Application\ Support/Code/User/globalStorage/evsyukov.hledger/$(BINARY)

all: lint test build
