SHELL := /bin/bash
GO_BINARY := bin/xlwms-server
SHEIN_GO_BINARY := bin/shein-server

.PHONY: setup dev-api dev-shein dev-web test build start clean

setup:
	npm --prefix frontend ci

dev-api:
	@set -a; if [[ -f .env ]]; then source .env; fi; set +a; exec go run ./cmd/server

dev-shein:
	@exec ./scripts/run-shein-server.sh

dev-web:
	npm --prefix frontend run dev

test:
	go test ./...
	go vet ./...
	npm --prefix frontend run typecheck

build:
	mkdir -p bin
	go build -o $(GO_BINARY) ./cmd/server
	go build -o $(SHEIN_GO_BINARY) ./cmd/shein-server
	npm --prefix frontend run build

start: build
	pm2 startOrReload ecosystem.config.cjs


clean:
	go clean
	rm -f $(GO_BINARY) $(SHEIN_GO_BINARY)
