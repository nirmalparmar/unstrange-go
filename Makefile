BIN := ./bin/server

.PHONY: build run dev tidy

build:
	go build -o $(BIN) ./cmd/server

run: build
	$(BIN)

dev:
	go run ./cmd/server

tidy:
	go mod tidy
