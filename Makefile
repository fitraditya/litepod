BINARY     := litepod
MODULE     := $(shell go list -m)
BUILD_DIR  := bin
COVER_OUT  := coverage.out
COVER_HTML := coverage.html

.PHONY: all run build test coverage swag clean

all: swag build

## run: run the agent (requires config.yaml)
run:
	go run main.go

## build: compile the binary to ./bin/litepod
build:
	mkdir -p $(BUILD_DIR)
	go build -o $(BUILD_DIR)/$(BINARY) main.go

## test: run all tests
test:
	go test ./...

## coverage: run tests and open an HTML coverage report
coverage:
	go test -coverprofile=$(COVER_OUT) ./...
	go tool cover -html=$(COVER_OUT) -o $(COVER_HTML)
	@echo "Coverage report: $(COVER_HTML)"

## swag: regenerate Swagger docs from annotations
swag:
	swag init -g main.go --parseInternal

## clean: remove build artifacts and generated files
clean:
	rm -rf $(BUILD_DIR) $(COVER_OUT) $(COVER_HTML)

## help: list available targets
help:
	@grep -E '^## ' Makefile | sed 's/## /  /'
