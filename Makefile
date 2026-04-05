APP_NAME := agent-router
GO := go
GOFLAGS := -ldflags="-s -w"

.PHONY: build run test clean docker-build

build:
	$(GO) build $(GOFLAGS) -o bin/$(APP_NAME) ./cmd/router

run:
	$(GO) run ./cmd/router

test:
	$(GO) test ./... -cover

clean:
	rm -rf bin/

docker-build:
	docker build -t $(APP_NAME):latest .