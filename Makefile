.PHONY: run test build frontend proof

ADDR ?= 127.0.0.1:7777
REPO ?= $(CURDIR)

run:
	cd server && go run ./cmd/trails --repo $(REPO) --addr $(ADDR)

test:
	cd server && go test ./...
	./scripts/check-adapters.sh
	./scripts/test-hooks.sh

# Writes the Vue build into server/internal/web/dist, which the binary embeds; it overwrites the committed placeholder index.html.
frontend:
	cd frontend && npm ci && npm run build

build: frontend
	cd server && go build -o ../bin/trails ./cmd/trails

# Screenshots of the project list and every page of one project (PROJECT, else the first listed) into OUT (default .proof) against the server at ADDR; exits 1 on any page or console error. PROOF_WRITES=1 adds the edit flows, which write entries: use it only against a scratch server.
proof:
	cd frontend && ADDR=$(ADDR) PROOF_WRITES=$(PROOF_WRITES) OUT=$(abspath $(or $(OUT),.proof)) node ../scripts/proof.mjs
