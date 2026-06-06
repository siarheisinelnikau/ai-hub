# ai-hub — build / test / deploy shortcuts.
#
# `make check`  runs the full CI gate (same as GitHub: scripts/ci.sh).
# `make deploy` builds the Linux binary, ships it + the UI to the server, restarts.
#
# On the Windows dev box `make` isn't installed — run `sh scripts/ci.sh` instead.

PYTHON  ?= python
BIN     := aihub-linux
MCPBIN  := mcp-linux

.PHONY: help check vet build test parity linux deploy mcp deploy-mcp clean

help:
	@echo "Targets:"
	@echo "  make check    go vet + build + test + parity (full CI gate)"
	@echo "  make vet      go vet ./..."
	@echo "  make build    go build ./..."
	@echo "  make test     go test -count=1 ./..."
	@echo "  make parity   Historical Python vs Go shared API checks (isolated fixture)"
	@echo "  make linux        cross-compile the Linux server binary"
	@echo "  make deploy       build + upload binary/UI + restart (destination from .env)"
	@echo "  make mcp          cross-compile the Linux MCP server binary"
	@echo "  make deploy-mcp   build + scp mcp to $(SERVER) + restart ai-hub-mcp"
	@echo "  make clean        remove build artifacts"

check: ## full CI gate (single source of truth: scripts/ci.sh)
	sh scripts/ci.sh

vet:
	go vet ./...

build:
	go build ./...

test:
	go test -count=1 ./...

parity:
	python tests/parity/run_parity.py

linux: ## cross-compile the static Linux server binary
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o $(BIN) ./cmd/aihub

mcp: ## cross-compile the MCP server (media tools) Linux binary
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o $(MCPBIN) ./cmd/mcp

deploy-mcp: ## build and deploy MCP using .env
	$(PYTHON) scripts/deploy.py --mcp

deploy: ## build and deploy panel using .env
	$(PYTHON) scripts/deploy.py

clean:
	rm -f $(BIN) $(MCPBIN) parity-aihub.exe aihub.exe mcp.exe
