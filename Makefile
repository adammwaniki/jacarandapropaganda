.PHONY: help test test-integration test-all run build tidy fmt vet lint dev-up dev-down clean

GO          ?= go
BINARY      := bin/jacaranda
PKGS        := ./...
INTEG_TAGS  := integration

help:
	@echo "Common targets:"
	@echo "  make test              — unit tests (fast, no external deps)"
	@echo "  make test-integration  — integration tests (needs Postgres + MinIO)"
	@echo "  make test-all          — unit + integration"
	@echo "  make run               — run the server against local dev stack"
	@echo "  make build             — build single binary to ./bin/jacaranda"
	@echo "  make dev-up            — start local Postgres + MinIO via compose"
	@echo "  make dev-down          — stop local dev stack"
	@echo "  make fmt vet tidy      — hygiene"

test:
	$(GO) test -race -count=1 $(PKGS)

test-integration:
	$(GO) test -race -count=1 -tags=$(INTEG_TAGS) $(PKGS)

test-all: test test-integration

run:
	$(GO) run ./cmd/server

build:
	mkdir -p bin
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags="-s -w" -o $(BINARY) ./cmd/server

tidy:
	$(GO) mod tidy

fmt:
	$(GO) fmt $(PKGS)

vet:
	$(GO) vet $(PKGS)

dev-up:
	./scripts/dev-up.sh

dev-down:
	./scripts/dev-down.sh

clean:
	rm -rf bin
