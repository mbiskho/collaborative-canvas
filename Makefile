.DEFAULT_GOAL := help

up: ## Full stack: 2 relays, Redis, Postgres, compactor, web on :8088
	docker compose up --build -d
	@echo "open http://localhost:8088  (Prometheus: http://localhost:9091)"

down: ## Stop and remove volumes
	docker compose down -v

dev: ## Relay in memory mode + Vite dev server (no Docker)
	(cd server && go run ./cmd/server) & (cd web && npm install && npm run dev)

test: ## Go unit tests (race) + Yjs convergence unit tests
	cd server && go vet ./... && go test -race ./...
	cd web && npm install --no-audit --no-fund && npm test

typecheck: ## TypeScript in web, compactor, tools
	for d in web compactor tools; do (cd $$d && npm install --no-audit --no-fund >/dev/null && npx tsc -p tsconfig.json --noEmit) || exit 1; done

bench: ## Fan-out latency: 20 rooms x 5 clients x 20 msg/s for 60s against the compose stack
	cd server && go run ./cmd/loadtest -url ws://localhost:8081 -rooms 20 -clients 5 -rate 20 -duration 60s | tee ../reports/bench.json

bench-cross: ## Same, with each room's clients split across both relay instances
	cd server && (go run ./cmd/loadtest -url ws://localhost:8081 -rooms 20 -clients 3 -rate 20 -duration 60s > ../reports/bench-a.json &) && \
	  go run ./cmd/loadtest -url ws://localhost:8082 -rooms 20 -clients 2 -rate 20 -duration 60s > ../reports/bench-b.json

chaos: ## 8 clients, random connection drops for 60s, then check every replica converged
	cd tools && npm install --no-audit --no-fund >/dev/null && URL=ws://localhost:8081 npm run chaos | tee ../reports/chaos.json

images: ## Re-render report mock-ups with headless Chrome
	./scripts/render-mocks.sh

help:
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

.PHONY: up down dev test typecheck bench bench-cross chaos images help
