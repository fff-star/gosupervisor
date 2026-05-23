.PHONY: build test test-all test-fuzz test-burnin vet coverage clean lint

BINARY := gosupervisor
FUZZ_TIME ?= 30s
BURNIN_TIME ?= 2h

## Build

build:
	go build -o $(BINARY) ./cmd/gosupervisor
	go build -o gosupervisorctl ./cmd/gosupervisorctl

## Standard tests (race detector, all packages)

# test runs the full suite with multi-iteration to detect flaky races.
# For a faster single pass, use test-quick.
test:
	go test -race -count=5 ./...

test-quick:
	go test -race -count=1 ./...

test-v:
	go test -race -count=5 -v ./...

test-short:
	go test -race -count=1 -short ./...

## Fuzz tests (duration controlled by FUZZ_TIME, default 30s)

test-fuzz:
	@echo "=== Fuzz config parsers ==="
	go test -fuzz=FuzzLoadINI -fuzztime=$(FUZZ_TIME) ./internal/config/
	go test -fuzz=FuzzLoadYAML -fuzztime=$(FUZZ_TIME) ./internal/config/
	go test -fuzz=FuzzLoadJSON -fuzztime=$(FUZZ_TIME) ./internal/config/
	@echo "=== Fuzz socket protocol ==="
	go test -fuzz=FuzzHandleCommand -fuzztime=$(FUZZ_TIME) ./internal/socket/
	@echo "=== Fuzz process name validation ==="
	go test -fuzz=FuzzValidateProcessName -fuzztime=$(FUZZ_TIME) ./internal/web/
	@echo "=== All fuzz tests passed ==="

## Burn-in reliability test (duration controlled by BURNIN_TIME, default 2h)

test-burnin:
	@echo "=== Burn-in test $(BURNIN_TIME) ==="
	go test -tags burnin -run TestBurnIn -timeout $(BURNIN_TIME) -v ./internal/process/

## Full test suite (standard + fuzz + burn-in)

test-all: test test-fuzz test-burnin
	@echo "=== All tests completed ==="

## Code quality

vet:
	go vet ./...

lint:
	golangci-lint run ./...

## Coverage

coverage:
	go test -race -count=1 -coverprofile=cover.out ./...
	go tool cover -func=cover.out
	@echo "HTML report: go tool cover -html=cover.out"

coverage-html: coverage
	go tool cover -html=cover.out -o cover.html
	@echo "Wrote cover.html"

## Clean

clean:
	rm -f $(BINARY) cover.out cover.html
	rm -rf ./test_logs*
