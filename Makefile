# =============================================================================
# fantastic-spoon — Makefile
# =============================================================================
#
# PURPOSE
#   Automate linting, tests, Go module checks, and release-style builds for this
#   repository. The default goal runs the full quality pipeline and produces a
#   single binary for Apple Silicon (darwin/arm64).
#
# PREREQUISITES
#   - Go toolchain on PATH (see go.mod for the required version).
#   - Optional: golangci-lint on PATH for stricter linting (lint target skips
#     it gracefully if missing).
#
# USAGE
#   make                  # same as `make all` — lint, test, deps, build
#   make all
#   make <target>         # run one of the targets below
#
# TARGETS (summary)
#   all         — Run lint, test, deps, then build (recommended before commit).
#   lint        — Enforce formatting (gofmt), go vet, and golangci-lint if installed.
#   test        — Run all packages’ tests without test-cache reuse (-count=1).
#   deps        — Verify module checksums and download modules (no upgrades).
#   deps-update — Upgrade direct module dependencies and tidy go.mod / go.sum.
#   build       — Cross-compile to bin/fantastic-spoon for GOOS/GOARCH below.
#   clean       — Remove the build output directory (bin/ by default).
#
# OVERRIDES (optional)
#   make GO=/path/to/go        # use a specific go binary
#   make BIN_DIR=dist BIN=dist/app   # change output path / binary name
#   Variables: GO, BIN_DIR, BIN (defaults shown below).
#
# OUTPUT
#   build writes $(BIN), by default bin/fantastic-spoon, built for darwin/arm64
#   only (see GOOS / GOARCH). The bin/ directory is gitignored.
#
# =============================================================================

.DEFAULT_GOAL := all

# Go command (override if you use a versioned binary, e.g. go1.26.1).
GO      ?= go
# Directory and filename for the compiled binary produced by `make build`.
BIN_DIR ?= bin
BIN     ?= $(BIN_DIR)/fantastic-spoon

# Cross-compilation: production binary is Apple Silicon (darwin/arm64) only.
# Override only if you intentionally need a different OS/arch for testing.
GOOS   := darwin
GOARCH := arm64

.PHONY: all lint test build deps deps-update clean

# -----------------------------------------------------------------------------
# all — default pipeline: static checks, tests, module integrity, then binary.
# Run from repo root: `make` or `make all`. Fails fast on lint; then runs tests
# and deps before build so you do not produce a binary from failing code.
# -----------------------------------------------------------------------------
all: lint test deps build

# -----------------------------------------------------------------------------
# lint — formatting and static analysis.
#   - gofmt: fail if any .go file would change (suggests: gofmt -w .).
#   - go vet: standard Go analyzer for suspicious constructs.
#   - golangci-lint: optional; if not installed, prints a skip message (non-fatal).
# -----------------------------------------------------------------------------
lint:
	@test -z "$$(gofmt -l .)" || ( \
		echo "gofmt: these files need formatting (run: gofmt -w .)" >&2; \
		gofmt -l . >&2; \
		exit 1 \
	)
	$(GO) vet ./...
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run ./...; \
	else \
		echo "make: golangci-lint not in PATH; skipping (https://golangci-lint.run/)"; \
	fi

# -----------------------------------------------------------------------------
# test — run the full test suite for all packages.
# Uses -count=1 to disable test result caching so repeated `make test` reflects
# real passes/failures (useful in CI and before releases).
# -----------------------------------------------------------------------------
test:
	$(GO) test ./... -count=1

# -----------------------------------------------------------------------------
# deps — check and fetch modules (no version bumps).
# go mod verify: ensure go.sum matches downloaded module contents.
# go mod download: populate the module cache (fails on missing or bad modules).
# Does not modify go.mod or go.sum. For upgrades, use deps-update.
# -----------------------------------------------------------------------------
deps:
	$(GO) mod verify
	$(GO) mod download

# -----------------------------------------------------------------------------
# deps-update — upgrade dependencies and refresh the module graph.
# go get -u ./... bumps direct dependencies to newer versions (per go get rules).
# go mod tidy removes unused requires and adds any missing ones.
# Review go.mod / go.sum diffs before committing; run tests after upgrading.
# -----------------------------------------------------------------------------
deps-update:
	$(GO) get -u ./...
	$(GO) mod tidy

# -----------------------------------------------------------------------------
# build — compile the main package to $(BIN) for $(GOOS)/$(GOARCH).
# Uses -trimpath for reproducible paths in the binary. Creates $(BIN_DIR) if
# needed. Example output: bin/fantastic-spoon (Mach-O arm64 on Apple Silicon).
# -----------------------------------------------------------------------------
build:
	@mkdir -p $(BIN_DIR)
	GOOS=$(GOOS) GOARCH=$(GOARCH) $(GO) build -trimpath -o $(BIN) .

# -----------------------------------------------------------------------------
# clean — remove the entire build output directory ($(BIN_DIR)).
# Safe to run repeatedly; only deletes generated artifacts under bin/ (default).
# -----------------------------------------------------------------------------
clean:
	rm -rf $(BIN_DIR)
