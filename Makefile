.DEFAULT_GOAL := help

BINDIR ?= bin/local

.PHONY: help up down logs status clean generate sqlc templ assets build test lint lint-template-go-files lint-uuid-parse

help:
	@echo "Usage: make [target]"
	@echo ""
	@echo "  up          Build and start all services"
	@echo "  down        Stop all services"
	@echo "  logs        Tail logs from all services"
	@echo "  status      Show service status"
	@echo "  clean       Stop services and remove volumes"
	@echo "  generate    Run sqlc + templ + assets"
	@echo "  build       Build all Go binaries"
	@echo "  test        Run Go tests"
	@echo "  lint        Run code-pattern guardrails"

up:
	docker compose up -d --build --remove-orphans

down:
	docker compose down

logs:
	docker compose logs -f

status:
	docker compose ps

clean:
	docker compose down -v

# Development targets

generate: sqlc templ assets

sqlc:
	sqlc generate -f internal/db/sql/sqlc.yaml

templ:
	templ fmt .
	templ generate

assets:
	pnpm run build

build: generate
	go build -o $(BINDIR)/web ./cmd/web
	go build -o $(BINDIR)/downloader ./cmd/downloader
	go build -o $(BINDIR)/ingest ./cmd/ingest
	go build -o $(BINDIR)/encoder ./cmd/encoder
	go build -o $(BINDIR)/pg-migrator ./cmd/pg-migrator

test:
	go test ./...

# Lint / code-pattern guardrails — run these in CI to prevent regression.

lint: lint-template-go-files lint-uuid-parse

lint-template-go-files:
	@echo "Checking for hand-written .go files in templates..."
	@FOUND=$$(find cmd/web/templates -name '*.go' ! -name '*_templ.go' 2>/dev/null); \
	if [ -n "$$FOUND" ]; then \
		echo "FAIL: Non-generated .go files found in templates package:"; \
		echo "$$FOUND"; \
		echo "Move these to pkg/ or cmd/web/viewtypes/"; \
		exit 1; \
	fi
	@echo "OK: templates/ contains only generated files."

lint-uuid-parse:
	@echo "Checking for inline UUID parsing (use common.RequireUUIDParam)..."
	@FOUND=$$(grep -rn 'pgtype\.UUID' cmd/web/handlers/ --include='*.go' \
		| grep -v '_test\.go' \
		| grep -v 'common/' \
		| grep '\.Scan(c\.Param' || true); \
	if [ -n "$$FOUND" ]; then \
		echo "FAIL: Inline UUID parsing found — use common.RequireUUIDParam instead:"; \
		echo "$$FOUND"; \
		exit 1; \
	fi
	@echo "OK: no inline UUID parsing in handlers."