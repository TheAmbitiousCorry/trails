.PHONY: run test build frontend

ADDR ?= 127.0.0.1:7777
REPO ?= $(CURDIR)

run:
	cd server && go run ./cmd/trail --repo $(REPO) --addr $(ADDR)

test:
	cd server && go test ./...

# Frontend arrives in a later step; build stays Go-only until then.
frontend:
	@echo "frontend: not built yet"

build: frontend
	cd server && go build -o ../bin/trail ./cmd/trail
