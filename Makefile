.PHONY: all build test lint bench clean install help

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

all: lint test build
