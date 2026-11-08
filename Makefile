# sudo Dragon — tâches courantes.
#
# Go >= 1.27 est requis (voir go.mod). Si `go` pointe vers une version plus
# ancienne: make GO=/chemin/vers/go1.27/bin/go <cible>

GO      ?= go
BIN     := bin
ADDR    ?= ws://localhost:8080/ws

.DEFAULT_GOAL := help
.PHONY: help build test race vet fmt fmt-check check run-server run-client load clean

help: ## Liste les cibles
	@grep -E '^[a-z-]+:.*## ' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-12s %s\n", $$1, $$2}'

build: ## Compile serveur, client, bot et outil de carte dans ./bin
	@mkdir -p $(BIN)
	$(GO) build -o $(BIN)/sudo-dragon-server ./cmd/server
	$(GO) build -o $(BIN)/sudo-dragon ./cmd/client
	$(GO) build -o $(BIN)/sudo-dragon-bot ./cmd/bot
	$(GO) build -o $(BIN)/sudo-dragon-worldview ./cmd/worldview

test: ## Lance les tests
	$(GO) test ./...

race: ## Lance les tests avec le détecteur de courses (plus lent)
	$(GO) test -race -count=1 ./...

vet: ## Analyse statique
	$(GO) vet ./...

fmt: ## Formate le code
	$(GO) fmt ./...

fmt-check: ## Échoue si du code n'est pas formaté
	@test -z "$$(gofmt -l .)" || { echo "fichiers non formatés:"; gofmt -l .; exit 1; }

check: fmt-check vet race ## Tout ce qu'il faut avant de livrer

run-server: ## Lance le serveur (base ./sudo-dragon.db)
	$(GO) run ./cmd/server

run-client: ## Lance le client (ADDR=ws://hote:8080/ws pour un autre serveur)
	$(GO) run ./cmd/client -addr $(ADDR)

load: ## Test de charge: make load N=250 (bots regroupés, cadence en régime établi)
	GO=$(GO) scripts/loadtest.sh $(or $(N),100)

clean: ## Supprime ./bin
	rm -rf $(BIN)
