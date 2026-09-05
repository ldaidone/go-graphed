BINARY=kg
OUTPUT_DIR=.
BANNER := ./scripts/ascii-banner

.DEFAULT_GOAL := help

.PHONY: all build run run-build run-mcp test test-race coverage vet tidy clean help

all: build

build:
	CGO_ENABLED=0 go build -o $(OUTPUT_DIR)/$(BINARY) cmd/kg/main.go

run:
	CGO_ENABLED=0 go run cmd/kg/main.go $(ARGS)

run-build:
	CGO_ENABLED=0 go run cmd/kg/main.go build $(ARGS)

run-mcp:
	CGO_ENABLED=0 go run cmd/kg/main.go mcp $(ARGS)

test:
	CGO_ENABLED=0 go test ./...

test-race:
	CGO_ENABLED=0 go test -race ./...

coverage:
	CGO_ENABLED=0 go test -coverprofile=coverage.out ./...
	CGO_ENABLED=0 go tool cover -html=coverage.out

vet:
	CGO_ENABLED=0 go vet ./...

tidy:
	go mod tidy

clean:
	rm -f $(OUTPUT_DIR)/$(BINARY) coverage.out

install-banner:
	@echo "🔗 Installing ascii-banner to $(HOME)/.local/bin..."
	mkdir -p $(HOME)/.local/bin
	install -m 755 $(BANNER) $(HOME)/.local/bin/ascii-banner

help:
	@echo ""
	@$(BANNER) -c magenta "GRAPHED"
	@echo ""
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@echo "  all        Build the binary (default)"
	@echo "  build      Build the kg CLI binary"
	@echo "  run        Run kg (default help)"
	@echo "  run-build  Run kg build subcommand (ARGS='.')"
	@echo "  run-mcp    Run kg mcp subcommand (ARGS='--file graph.json')"
	@echo "  test       Run all tests"
	@echo "  test-race  Run tests with race detection"
	@echo "  coverage   Run tests and open coverage report"
	@echo "  vet        Run go vet"
	@echo "  tidy       Run go mod tidy"
	@echo "  clean      Remove build artifacts"
	@echo "  help       Show this message"
	@echo ""