BINARY := mydb$(shell go env GOEXE)
BIN_DIR := bin
PKG := ./...

.DEFAULT_GOAL := help
.PHONY: help build run test test-race cover vet fmt tidy check clean

help: ## Show this help
	@echo "mydb targets:"
	@grep -E '^[a-z-]+:.*##' $(MAKEFILE_LIST) | sed 's/:.*## /\t- /'

build: ## Compile the binary into bin/
	go build -o $(BIN_DIR)/$(BINARY) .

run: ## Run the app from source
	go run .

test: ## Run all tests
	go test $(PKG)

test-race: ## Run tests with the race detector (needs CGO + a C compiler)
	CGO_ENABLED=1 go test -race $(PKG)

cover: ## Run tests and open the HTML coverage report
	go test -coverprofile=coverage.out $(PKG)
	go tool cover -html=coverage.out

vet: ## Run go vet
	go vet $(PKG)

fmt: ## Format all Go source
	go fmt $(PKG)

tidy: ## Sync go.mod / go.sum
	go mod tidy

check: fmt vet test ## Format, vet, then test

clean: ## Remove build output and coverage data
	go clean
	rm -rf $(BIN_DIR) coverage.out
