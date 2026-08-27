SHELL := /bin/bash
GO_BINARY := bin/xlwms-server

.PHONY: setup dev-api dev-web test build deploy-web start clean

setup:
	npm --prefix frontend ci

dev-api:
	@set -a; if [[ -f .env ]]; then source .env; fi; set +a; exec go run ./cmd/server

dev-web:
	npm --prefix frontend run dev

test:
	go test ./...
	go vet ./...
	npm --prefix frontend run typecheck

build:
	mkdir -p bin
	go build -o $(GO_BINARY) ./cmd/server
	npm --prefix frontend run build

deploy-web:
	test -d /var/www/warehouse-console
	test -f frontend/dist/index.html
	rsync -a --delete frontend/dist/ /var/www/warehouse-console/

start: build deploy-web
	pm2 startOrReload ecosystem.config.cjs

clean:
	go clean
	rm -f $(GO_BINARY)
