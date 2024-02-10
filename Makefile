GO_VARS := CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GO111MODULE=on

.PHONY: resolve
resolve:
	@echo "Resolving dependencies..."
	$(GO_VARS) go mod tidy
	$(GO_VARS) go mod vendor

.PHONY: test
test:
	@echo "Running tests..."
	@go test -v ./...

.PHONY: build
build:
	@echo "Building..."
	$(GO_VARS) go build -mod=vendor -a -o ./bin/zookeeper ./cmd/main.go

.PHONY: run
run:
	@echo "Running..."
	@./bin/zookeeper