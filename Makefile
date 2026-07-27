SHELL := /bin/bash
GO ?= go
BIN := bin/deployer
WEB_DIST := server/internal/web/dist

.PHONY: build server web test vet run clean

## build: PWA into the embed directory, then the single binary
build: web server

server:
	cd server && $(GO) build -trimpath -ldflags "-s -w" -o ../$(BIN) ./cmd/deployer

## web: build the PWA straight into the server's embed directory
web:
	@if [ -d apps/web ]; then \
		cd apps/web && npm ci && npm run build; \
	else \
		echo "apps/web not present yet — keeping the placeholder in $(WEB_DIST)"; \
	fi

test:
	cd server && $(GO) test ./...

vet:
	cd server && $(GO) vet ./...

## run: build and start the server with verbose logging
run: server
	./$(BIN) -v

clean:
	rm -rf bin
