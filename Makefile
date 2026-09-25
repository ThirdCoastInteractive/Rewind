.DEFAULT_GOAL := help

BINDIR ?= bin/local

# Keep GPU compiler layers out of Docker Desktop's small shared cache.
# This selects the builder for this make invocation, not other projects.
BUILDX_BUILDER ?= rewind
export BUILDX_BUILDER
PYTHON ?= python

# Optional standalone runtime images; public service builds remain self-contained.
RUNTIME_FFMPEG_DEBIAN ?= rewind-runtime-ffmpeg-debian:local
RUNTIME_FFMPEG_ALPINE ?= rewind-runtime-ffmpeg-alpine:local

.PHONY: help up up-fast up-web up-workers up-ml build-cache runtime-bases runtime-bases-force down logs status clean generate sqlc templ assets build test test-js lint lint-template-go-files lint-uuid-parse lint-release release-audit release-gates e2e release show-notes-migrate show-notes-preflight

help:
	@echo "Usage: make [target]"
	@echo ""
	@echo "  up                 Start the stack. Rebuilds only services whose sources changed"
	@echo "  up-fast            Start stack without rebuilding anything"
	@echo "  up-web             Force rebuild rewind (UI + in-process workers)"
	@echo "  up-workers         Same as up-web"
	@echo "  up-ml              Force rebuild rewind-ml"
	@echo "  migrate            Run database migrations (rewind migrate)"
	@echo "  build-cache        Ensure the persistent Rewind builder exists"
	@echo "  runtime-bases      Build shared ffmpeg bases if missing (slow, rare)"
	@echo "  runtime-bases-force  Rebuild bases from scratch (no cache)"
	@echo "  down               Stop all services"
	@echo "  logs               Tail logs from all services"
	@echo "  status             Show service status"
	@echo "  clean              Stop services and remove volumes"
	@echo "  generate           Run sqlc + templ + assets"
	@echo "  build              Build all Go binaries"
	@echo "  test               Run Go tests"
	@echo "  test-tools         Test release guardrails in disposable repositories"
	@echo "  test-js            Run all JavaScript unit tests"
	@echo "  release-gates      Run release guardrails and regression tests"
	@echo "  e2e                Run Playwright E2E tests"
	@echo "  show-notes-migrate  Backfill collaborative Markdown documents"
	@echo "  show-notes-preflight Verify every note is safe for workspace cutover"
	@echo "  lint               Run code-pattern guardrails"
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

# Bootstrap once; later invocations reuse this builder and its cache volume.
build-cache:
	@docker buildx inspect "$(BUILDX_BUILDER)" >/dev/null 2>&1 || \
		docker buildx create --name "$(BUILDX_BUILDER)" --driver docker-container \
		--driver-opt default-load=true --buildkitd-config docker/buildkitd.toml
	@docker buildx inspect --bootstrap "$(BUILDX_BUILDER)" >/dev/null
	@echo "Build cache: $(BUILDX_BUILDER)"

.PHONY: test-build-cache
test-build-cache: build-cache
	$(PYTHON) scripts/check-build-cache.py --builder "$(BUILDX_BUILDER)"

runtime-bases: build-cache
	@if ! docker image inspect $(RUNTIME_FFMPEG_DEBIAN) >/dev/null 2>&1; then \
		echo "==> building $(RUNTIME_FFMPEG_DEBIAN) (ffmpeg via apt, one-time)"; \
		docker build -t $(RUNTIME_FFMPEG_DEBIAN) -f docker/runtime-ffmpeg-debian.Dockerfile docker/; \
	else echo "ok  $(RUNTIME_FFMPEG_DEBIAN)"; fi
	@if ! docker image inspect $(RUNTIME_FFMPEG_ALPINE) >/dev/null 2>&1; then \
		echo "==> building $(RUNTIME_FFMPEG_ALPINE) (ffmpeg via apk, one-time)"; \
		docker build -t $(RUNTIME_FFMPEG_ALPINE) -f docker/runtime-ffmpeg-alpine.Dockerfile docker/; \
	else echo "ok  $(RUNTIME_FFMPEG_ALPINE)"; fi

runtime-bases-force: build-cache
	docker build --no-cache -t $(RUNTIME_FFMPEG_DEBIAN) -f docker/runtime-ffmpeg-debian.Dockerfile docker/
	docker build --no-cache -t $(RUNTIME_FFMPEG_ALPINE) -f docker/runtime-ffmpeg-alpine.Dockerfile docker/

# Rebuild only services whose sources are newer than the image, then start.
# Ollama / vision wheels live in ./bin/runtime and are not part of the image.
up: build-cache
	$(PYTHON) scripts/compose-up.py

# No image builds — just start whatever is already built.
up-fast:
	$(PYTHON) scripts/compose-up.py --no-build

# Force a single service when the mtime check is wrong or you want a clean image.
up-web: build-cache
	$(PYTHON) scripts/compose-up.py --force rewind

up-workers: build-cache
	$(PYTHON) scripts/compose-up.py --force rewind

up-ml: build-cache
	$(PYTHON) scripts/compose-up.py --force rewind-ml

.PHONY: migrate
migrate: build-cache
	docker compose run --rm --build --no-deps rewind migrate

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
	templ fmt cmd/web/templates
	templ generate -path cmd/web/templates

assets:
	pnpm run build

build: generate
	go build -o $(BINDIR)/rewind ./cmd/web
	go build -o $(BINDIR)/web ./cmd/web
	go build -o $(BINDIR)/sfu ./cmd/sfu
	go build -o $(BINDIR)/downloader ./cmd/downloader
	go build -o $(BINDIR)/ingest ./cmd/ingest
	go build -o $(BINDIR)/encoder ./cmd/encoder
	go build -o $(BINDIR)/ml ./cmd/ml
	go build -o $(BINDIR)/pg-migrator ./cmd/pg-migrator

test:
	go test ./...

.PHONY: test-stitch-core
test-stitch-core:
	go test ./internal/stitch

.PHONY: test-stitch-integration
test-stitch-integration:
	go test -tags=integration ./internal/integration -run Stitch -count=1 -v

.PHONY: test-stitch-render test-stitch-client
test-stitch-render:
	go test -tags=integration ./internal/encode -run Canonical -count=1 -v

test-stitch-client:
	node --test tests/stitch-workspace.behavior.test.mjs

.PHONY: test-stitch-mcp
test-stitch-mcp:
	go test -tags=integration ./internal/mcp -run TestStitchEditorMCP -count=1 -v

.PHONY: test-generation-retry
test-generation-retry:
	go test ./cmd/ml/... ./cmd/web/handlers/api/video_api ./cmd/web/templates ./internal/contextwindow ./internal/transcription

.PHONY: test-tools
test-tools:
	bash scripts/test-release.sh

.PHONY: test-js
test-js:
	node scripts/test-js.mjs

.PHONY: release-audit
release-audit: test-js
	go test -p 1 -tags=releaseaudit ./internal/integration ./internal/agent ./cmd/web/handlers/api/shownote_api -run 'TestV003|TestV010|TestFollowupRetains|TestOfflineScene' -count=1

.PHONY: release-gates
release-gates: test test-stitch-client test-tools lint release-audit integration-test

.PHONY: vision-build vision-test vision-install postgres-vector-build integration-up integration-down integration-test
VISION_MODEL ?= ViT-B-32__openai
VISION_LICENSE_ARGS ?=
vision-build: build-cache
	docker compose -f docker-compose.integration.yml build vision

vision-test:
	docker compose -f docker-compose.integration.yml run --rm --no-deps vision

vision-install:
	docker compose run --rm --no-deps -w /opt/vision rewind-ml python3 models.py $(VISION_MODEL) $(VISION_LICENSE_ARGS)

vision-install-canary:
	docker compose -f docker-compose.integration.yml run --rm --no-deps -e VISION_MODEL_DIR=/models/vision -v ./bin/models/vision:/models/vision vision python3 models.py $(VISION_MODEL) $(VISION_LICENSE_ARGS)

vision-lock:
	docker compose -f docker-compose.integration.yml run --rm --no-deps vision pip freeze --exclude onnxruntime --exclude onnxruntime-gpu > services/vision/requirements.lock

vision-canary:
	docker compose -f docker-compose.integration.yml run --rm --no-deps -e VISION_MODEL_DIR=/models/vision -v ./bin/models/vision:/models/vision:ro -v ./services/vision:/app:ro vision python3 model_canary.py

postgres-vector-build: build-cache
	docker compose -f docker-compose.integration.yml build postgres

integration-up:
	docker compose -f docker-compose.integration.yml up -d --wait postgres

integration-down:
	docker compose -f docker-compose.integration.yml down

integration-test:
	go test -tags=integration ./internal/integration -count=1 -v
	go test -tags=integration ./internal/mcp -count=1 -v
	NETWORK_QUERY_TEST_DSN='postgres://rewind_test:disposable-test-only@127.0.0.1:15439/rewind_test?sslmode=disable' go test ./cmd/web/handlers/content -run TestCommenterNetworkSparseCandidatesSynthetic -count=1 -v

.PHONY: agent-model-test
agent-model-test:
	go test -tags=realmodel ./internal/agent -run TestQwenArchiveCompilation -count=1 -v -timeout 15m

e2e:
	pnpm exec playwright test

show-notes-migrate: build-cache
	docker compose run --rm --build --no-deps --entrypoint ./show-note-workspace rewind

show-notes-preflight: build-cache
	docker compose run --rm --build --no-deps --entrypoint ./show-note-workspace rewind --preflight

release:
	@echo "Squashing master → public/main (private paths excluded, ThirdCoast identity)..."
	git fetch public
	git checkout -b release-squash public/main
	git merge --squash origin/master
	bash scripts/check-release.sh --strip-index
	git restore --worktree -- AGENTS.md CLAUDE.md .grok .claude 2>/dev/null || true
	bash scripts/check-release.sh
	@read -p "Commit message (e.g. 'release: HLS player + faststart'): " msg && \
		GIT_AUTHOR_NAME=ThirdCoast GIT_AUTHOR_EMAIL=git@thirdcoast.tv \
		GIT_COMMITTER_NAME=ThirdCoast GIT_COMMITTER_EMAIL=git@thirdcoast.tv \
		git commit -m "$$msg"
	bash scripts/check-release.sh --commit HEAD
	git push public HEAD:main
	git checkout master
	git branch -D release-squash
	@echo "Done. public/main updated."

# Lint / code-pattern guardrails — run these in CI to prevent regression.

lint: lint-template-go-files lint-uuid-parse lint-release

lint-release:
	bash scripts/check-release.sh

lint-template-go-files:
	@echo "Checking template source and generated output..."
	@FOUND=$$(find cmd/web/templates -type f -name '*.go' ! -name '*_templ.go' ! -name '*_test.go' 2>/dev/null | grep -Ev '^(cmd/web/templates/(helpers|investigate_graph|model_picker|network_commenters|network_wiki|video_processing|watch_context)\.go|cmd/web/templates/components/ssr_types\.go)$$' || true); \
	if [ -n "$$FOUND" ]; then \
		echo "FAIL: unexpected hand-written production Go files found in templates package:"; \
		echo "$$FOUND"; \
		echo "Add intentional view support code to the explicit allowlist or move it out of templates."; \
		exit 1; \
	fi
	@FOUND=$$(find cmd/web/templates -name '*.templ' -print 2>/dev/null | while IFS= read -r source; do generated="$${source%.templ}_templ.go"; if [ ! -f "$$generated" ]; then echo "$$source -> $$generated"; fi; done); \
	if [ -n "$$FOUND" ]; then \
		echo "FAIL: Generated template output is missing:"; \
		echo "$$FOUND"; \
		echo "Run make templ to regenerate template output."; \
		exit 1; \
	fi
	@echo "OK: every .templ file has generated _templ.go output."

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

# Full inference diagnostics use explicit installed models and never modify media.
.PHONY: ml-canary vision-cuda-build vision-cuda-canary
ml-canary:
	docker compose -f docker-compose.yml -f docker-compose.gpu.yml run --rm --no-deps -e CANARY_CONTEXT=32768 -v $(CURDIR)/scripts/ml-canary.sh:/tmp/ml-canary.sh:ro --entrypoint sh rewind-ml /tmp/ml-canary.sh
vision-cuda-build: build-cache
	docker build -f ml.Dockerfile --target runtime-cuda -t rewind-ml:cuda-test .
vision-cuda-canary:
	docker run --rm --gpus all -e VISION_MODEL_DIR=/models/vision -v $(CURDIR)/bin/models/vision:/models/vision:ro -v $(CURDIR)/services/vision:/app:ro rewind-ml:cuda-test python3 /app/gpu_canary.py

.PHONY: agent-handshake-test
agent-handshake-test:
	go test -tags=agenthandshake ./internal/agentprotocol -run TestInstalledProviderHandshake -count=1 -v -timeout 1m
