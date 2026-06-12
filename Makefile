.DEFAULT_GOAL := help

BINDIR ?= bin/local

.PHONY: help up down logs status clean generate sqlc templ assets build test lint lint-template-go-files lint-uuid-parse e2e release

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
	@echo "  e2e         Run Playwright E2E tests"
	@echo "  lint        Run code-pattern guardrails"
	@echo ""
	@echo "Performance testing:"
	@echo "  perf            Run all sitespeed.io page tests"
	@echo "  perf-player     Run video player performance tests (longest video)"
	@echo "  perf-scroll     Run scroll/CLS tests on videos page"
	@echo "  perf-editors    Run stitch/compose editor tests"
	@echo "  perf-admin      Run admin page tests"
	@echo "  perf-graphite   Run all tests and send metrics to Graphite"
	@echo "  perf-dashboards Start Grafana + Graphite dashboards"
	@echo "  perf-clean      Remove sitespeed.io results"

setup:
	go install github.com/a-h/templ/cmd/templ@latest
	go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest
	pnpm install

up:
	docker compose up -d --build --remove-orphans

down:
	docker compose down

logs:
	docker compose logs --tail 30 -f

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

e2e:
	pnpm exec playwright test

release:
	@echo "Squashing master → public/main (CLAUDE.md excluded)..."
	git fetch public
	git checkout -b release-squash public/main
	git merge --squash origin/master
	git restore --staged CLAUDE.md
	git restore CLAUDE.md
	@read -p "Commit message (e.g. 'release: HLS player + faststart'): " msg && git commit -m "$$msg"
	git push public HEAD:main
	git checkout master
	git branch -D release-squash
	@echo "Done. public/main updated."

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

# Performance testing targets (sitespeed.io)

SITESPEED_ITERATIONS ?= 3
SITESPEED_RUN = docker compose run --rm sitespeed

.PHONY: perf perf-player perf-scroll perf-editors perf-admin perf-graphite perf-dashboards perf-clean

perf:
	$(SITESPEED_RUN) --multi /scripts/main-pages.mjs -n $(SITESPEED_ITERATIONS) --budget.configPath /config/budgets.json

perf-player:
	$(SITESPEED_RUN) --multi /scripts/video-player.mjs -n $(SITESPEED_ITERATIONS) --budget.configPath /config/budgets.json

perf-scroll:
	$(SITESPEED_RUN) --multi /scripts/scroll-test.mjs -n $(SITESPEED_ITERATIONS) --budget.configPath /config/budgets.json

perf-editors:
	$(SITESPEED_RUN) --multi /scripts/suites/editors.mjs -n $(SITESPEED_ITERATIONS) --budget.configPath /config/budgets.json

perf-admin:
	$(SITESPEED_RUN) --multi /scripts/suites/admin.mjs -n $(SITESPEED_ITERATIONS) --budget.configPath /config/budgets.json

perf-graphite:
	docker compose --profile perf up -d graphite
	sleep 5
	$(SITESPEED_RUN) --multi /scripts/main-pages.mjs -n $(SITESPEED_ITERATIONS) --budget.configPath /config/budgets.json --graphite.host graphite
	$(SITESPEED_RUN) --multi /scripts/video-player.mjs -n $(SITESPEED_ITERATIONS) --budget.configPath /config/budgets.json --graphite.host graphite
	$(SITESPEED_RUN) --multi /scripts/scroll-test.mjs -n $(SITESPEED_ITERATIONS) --budget.configPath /config/budgets.json --graphite.host graphite
	$(SITESPEED_RUN) --multi /scripts/suites/editors.mjs -n $(SITESPEED_ITERATIONS) --budget.configPath /config/budgets.json --graphite.host graphite
	$(SITESPEED_RUN) --multi /scripts/suites/admin.mjs -n $(SITESPEED_ITERATIONS) --budget.configPath /config/budgets.json --graphite.host graphite

perf-dashboards:
	docker compose --profile perf up -d graphite grafana
	@echo "Grafana: http://localhost:3000 (admin/admin)"
	@echo "Graphite: http://localhost:8080"

perf-clean:
	rm -rf bin/dev/sitespeed/results/*
	rm -rf bin/dev/sitespeed/video/*