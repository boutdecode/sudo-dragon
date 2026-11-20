# sudo Dragon — common tasks.
#
# Go >= 1.27 is required (see go.mod). If `go` points to an older version:
#   make GO=/path/to/go1.27/bin/go <target>
# Stamp a version into the binaries:
#   make build VERSION=v1.2.3

GO      ?= go
BIN     := bin
ADDR    ?= ws://localhost:8080/ws
# Version stamped into the binaries (make build VERSION=v1.2.3).
VERSION ?= dev
LDFLAGS := -X github.com/kevin/ascii-adventure/internal/version.Version=$(VERSION)

.DEFAULT_GOAL := help
.PHONY: help build test race vet fmt fmt-check check run-server run-client load llm-smoke clean

help: ## List the targets
	@grep -E '^[a-z-]+:.*## ' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-12s %s\n", $$1, $$2}'

build: ## Build server, client, bot and map tool into ./bin
	@mkdir -p $(BIN)
	$(GO) build -ldflags "$(LDFLAGS)" -o $(BIN)/sudo-dragon-server ./cmd/server
	$(GO) build -ldflags "$(LDFLAGS)" -o $(BIN)/sudo-dragon ./cmd/client
	$(GO) build -ldflags "$(LDFLAGS)" -o $(BIN)/sudo-dragon-bot ./cmd/bot
	$(GO) build -ldflags "$(LDFLAGS)" -o $(BIN)/sudo-dragon-worldview ./cmd/worldview

test: ## Run the tests
	$(GO) test ./...

# internal/world is excluded from -race: it is pure deterministic generation (no concurrent test),
# and the detector makes its suite (run once per generator version) about 6x slower, ~12 min.
race: ## Run the tests with the race detector, except internal/world (tested plain)
	$(GO) test -race -count=1 -timeout 30m $$($(GO) list ./... | grep -v '/internal/world$$')
	$(GO) test -count=1 ./internal/world

vet: ## Static analysis
	$(GO) vet ./...

fmt: ## Format the code
	$(GO) fmt ./...

fmt-check: ## Fail if any code is not formatted
	@test -z "$$(gofmt -l .)" || { echo "unformatted files:"; gofmt -l .; exit 1; }

check: fmt-check vet race ## Everything needed before shipping

run-server: ## Run the server (database ./sudo-dragon.db)
	$(GO) run ./cmd/server --seed 13

run-client: ## Run the client (ADDR=ws://host:8080/ws for another server)
	$(GO) run ./cmd/client -addr $(ADDR)

load: ## Load test: make load N=250 (grouped bots, steady-state pace)
	GO=$(GO) scripts/loadtest.sh $(or $(N),100)

llm-smoke: ## Live test of the Mistral API (needs MISTRAL_API_KEY; uses a few tokens)
	$(GO) test -tags live -count=1 -v -run Live ./internal/llm

clean: ## Remove ./bin
	rm -rf $(BIN)
