BINARY_NAME=argocd-app-diff
BUILD_DIR=bin
COVERAGE_PROFILE=$(CURDIR)/coverage.out

.PHONY: build run test lint fmt fmt-check vet cover clean

build:
	go build -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/argocd-app-diff

run:
	go run ./cmd/argocd-app-diff

test:
	go test -v -race ./...

lint:
	golangci-lint run ./...

fmt:
	go fmt ./...

fmt-check:
	./scripts/fmt-check.sh

vet:
	go vet ./...

cover:
	go test -coverprofile=$(COVERAGE_PROFILE) ./internal/...
	go tool cover -func=$(COVERAGE_PROFILE)

clean:
	rm -rf $(BUILD_DIR)
