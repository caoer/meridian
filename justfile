# Meridian — md CLI

default:
    @just --list

# Build md binary
build:
    go build -o md ./cmd/md

# Build and install to ~/.local/bin
install: build
    cp md ~/.local/bin/md

# Build only (no install)
check:
    go build ./...

# Run tests
test:
    go test ./...

# Clean build artifacts
clean:
    rm -f md
