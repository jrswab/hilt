.PHONY: build clean test

BINARY_NAME := hilt
CMD_PATH := ./cmd/hilt

build:
	go build -o $(BINARY_NAME) $(CMD_PATH)

clean:
	rm -f $(BINARY_NAME)

test:
	go test ./...
