.PHONY: build test run docker sum

build: ## Build the sammati program
	CGO_ENABLED=0 go build -trimpath -o sammati ./cmd/sammati

test: ## Run the tests
	go vet ./...
	go test ./...

run: build ## Run on http://127.0.0.1:8787
	./sammati serve

docker: ## Build and start with Docker Compose
	docker compose up -d --build

sum: ## Write go.sum using Docker, for machines without Go
	docker run --rm -v "$$PWD":/src -w /src golang:1.27 go mod tidy
