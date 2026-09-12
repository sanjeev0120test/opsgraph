# opsgraph Makefile
# Philosophy: CI does the heavy validation (race, matrix, coverage). Locally,
# use `make quick` (fast) so your machine stays responsive.

BINARY      := opsgraph
PKG         := ./cmd/opsgraph
BIN_DIR     := bin
FIXTURE     := ./fixtures/incident_checkout
HEALTHY     := ./fixtures/fleet_healthy
ifeq ($(OS),Windows_NT)
EXE := .exe
else
EXE :=
endif
BIN := $(BIN_DIR)/$(BINARY)$(EXE)

VERSION     ?= dev
COMMIT      ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE        ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS     := -s -w -buildid= \
	-X github.com/sanjeev0120test/opsgraph/internal/version.Version=$(VERSION) \
	-X github.com/sanjeev0120test/opsgraph/internal/version.Commit=$(COMMIT) \
	-X github.com/sanjeev0120test/opsgraph/internal/version.Date=$(DATE)

export CGO_ENABLED := 0

.PHONY: build test fixture-test validate-fixture fleet-healthy demo quick lint fmt vet tidy-check staticcheck deadcode govulncheck race ci cross clean

build: ## Build the static binary
	go build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN) $(PKG)

test: ## Run unit tests (no race; -short on Windows so the laptop stays light)
ifeq ($(OS),Windows_NT)
	go test -short ./...
else
	go test ./...
endif

race: ## Run unit tests with the race detector (heavy - prefer CI)
	CGO_ENABLED=1 go test -race ./...

fixture-test: build ## Run the golden fixture test
	$(BIN) test $(FIXTURE)

validate-fixture: build ## Validate the primary fixture pack
	$(BIN) validate-fixture $(FIXTURE)

fleet-healthy: build ## Validate the all-green fixture + health --strict
	$(BIN) validate-fixture $(HEALTHY)
	$(BIN) health --fixture $(HEALTHY) --strict

demo: build ## Run the built-in incident demo
	$(BIN) demo

fmt: ## Check formatting
	@test -z "$$(gofmt -l .)" || (echo "unformatted files:"; gofmt -l .; exit 1)

vet: ## go vet
	go vet ./...

tidy-check: ## Ensure go.mod/go.sum are tidy
	go mod tidy
	git diff --exit-code go.mod go.sum

staticcheck: ## Run staticcheck via go tool (sum-pinned in go.mod)
	go tool staticcheck ./...

deadcode: ## Fail if unreachable funcs remain (go tool, sum-pinned)
	@out="$$(go tool deadcode -test ./...)"; \
	if [ -n "$$out" ]; then echo "$$out"; exit 1; fi

govulncheck: ## Run govulncheck via go tool (sum-pinned in go.mod)
	go tool govulncheck ./...

quick: fmt vet tidy-check staticcheck build test validate-fixture fleet-healthy fixture-test demo ## Fast local validation (recommended)

# Local "ci" stays laptop-friendly (no -race). GitHub Actions runs the race matrix.
ci: fmt vet tidy-check staticcheck build test validate-fixture fleet-healthy fixture-test demo ## Local gate (race/matrix live in Actions)

cross: ## Cross-compile release binaries (linux/darwin/windows × amd64/arm64)
	@mkdir -p dist
	GOOS=linux   GOARCH=amd64 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/$(BINARY)-linux-amd64   $(PKG)
	GOOS=linux   GOARCH=arm64 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/$(BINARY)-linux-arm64   $(PKG)
	GOOS=darwin  GOARCH=amd64 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/$(BINARY)-darwin-amd64  $(PKG)
	GOOS=darwin  GOARCH=arm64 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/$(BINARY)-darwin-arm64  $(PKG)
	GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/$(BINARY)-windows-amd64.exe $(PKG)
	GOOS=windows GOARCH=arm64 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/$(BINARY)-windows-arm64.exe $(PKG)

pack-release: cross ## Package archives + SHA256SUMS via scripts/pack-release.sh (VERSION=vX.Y.Z)
	@test -n "$(filter-out dev,$(VERSION))" || (echo "set VERSION=vX.Y.Z" >&2; exit 1)
	bash scripts/pack-release.sh $(VERSION) dist dist-release

clean: ## Remove build artifacts
	go clean
	-rm -rf $(BIN_DIR) dist cover.out
