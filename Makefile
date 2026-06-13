# Jenny cross-platform build targets

.PHONY: build build-portal build-portal-all test test-portal lint clean

# Build the jenny binary for the current platform
build:
	go build -o dist/jenny ./cmd/jenny/

# Build the portal package for the current platform
build-portal:
	go build -o dist/jenny-portal ./cmd/jenny/

# Build jenny binary for all three major platforms
build-portal-all:
	mkdir -p dist
	GOOS=linux GOARCH=amd64 go build -o dist/jenny-linux-amd64 ./cmd/jenny/
	GOOS=darwin GOARCH=amd64 go build -o dist/jenny-darwin-amd64 ./cmd/jenny/
	GOOS=windows GOARCH=amd64 go build -o dist/jenny-windows-amd64.exe ./cmd/jenny/

# Run all tests
test:
	go test ./...

# Run portal tests only
test-portal:
	go test ./internal/portal/...

# Run linter
lint:
	go fmt ./...
	go vet ./...

# Clean build artifacts
clean:
	rm -rf dist/jenny*
	rm -f internal/portal/*.tmp
