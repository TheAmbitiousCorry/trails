.PHONY: run test build frontend

ADDR ?= 127.0.0.1:7777
REPO ?= $(CURDIR)

run:
	cd server && go run ./cmd/trails --repo $(REPO) --addr $(ADDR)

test:
	cd server && go test ./...
	./scripts/check-adapters.sh

# Frontend arrives in a later step; build stays Go-only until then.
frontend:
	@echo "frontend: not built yet"

build: frontend
	cd server && go build -o ../bin/trails ./cmd/trails
