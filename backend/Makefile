GOBIN := $(shell go env GOPATH)/bin
# Constitution target: 80% coverage across internal/. The raw-socket DHCP serve
# loop and Kratos bind internals are exercised by the E2E harness (make
# test-e2e) rather than unit/integration tests.
COVER_THRESHOLD := 80

.PHONY: proto
proto: ## generate API code from proto definitions
	buf generate

.PHONY: lint
lint:
	golangci-lint run ./...

.PHONY: build
build:
	mkdir -p bin && go build -o bin/netbootd ./cmd/netbootd

.PHONY: run
run: build
	./bin/netbootd -conf configs/netbootd.example.yaml

.PHONY: migrate
migrate:
	go run ./cmd/netbootd migrate -conf configs/netbootd.example.yaml

.PHONY: test
# Coverage is collected via GOCOVERDIR so counts merge correctly across every
# test binary (the older -coverprofile merge under-counts code exercised by a
# different package's tests, e.g. service methods hit only by integration).
test:
	rm -rf .coverdata && mkdir -p .coverdata
	go test -race -timeout 900s -covermode=atomic -coverpkg=./internal/... \
	  ./internal/... ./tests/integration/... -args -test.gocoverdir=$(PWD)/.coverdata
	@go tool covdata textfmt -i=.coverdata -o=coverage.out >/dev/null
	@$(MAKE) --no-print-directory coverage-check

.PHONY: test-unit
test-unit:
	go test -race ./internal/...

.PHONY: coverage-check
coverage-check:
	@total=$$(go tool cover -func=coverage.out | awk '/^total:/ {gsub("%","",$$3); print $$3}'); \
	echo "total coverage: $${total}%"; \
	awk -v t="$$total" -v min=$(COVER_THRESHOLD) 'BEGIN { exit (t+0 < min) ? 1 : 0 }' || \
	  { echo "FAIL: coverage $${total}% is below $(COVER_THRESHOLD)%"; exit 1; }

.PHONY: test-e2e
test-e2e:
	go test -tags e2e -timeout 60m ./tests/e2e/...

.PHONY: coverage
coverage: test
	go tool cover -html=coverage.out -o coverage.html
