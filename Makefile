VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X github.com/caoer/meridian/internal/version.version=$(VERSION)
BINARY  := md
GOFLAGS := -trimpath

.PHONY: build test fmt lint clean release

build:
	go build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $(BINARY) ./cmd/md/

test:
	go test ./...

fmt:
	gofmt -w .

lint: fmt
	go vet ./...

clean:
	rm -f $(BINARY)
	rm -rf dist/

# Release matrix. No darwin/amd64: the fleet's Macs are Apple Silicon and there
# is no Intel-Mac target, so shipping an x86_64-darwin binary would publish a
# tarball whose nix closure is never built or pushed to niks3 (release.yml's
# cache matrix has no macos-13 leg) — an Intel-Mac consumer would cache-miss and
# rebuild. Keeping the release surface == the niks3 cache surface (aarch64-darwin,
# aarch64-linux, x86_64-linux) keeps every shipped platform substitutable.
PLATFORMS := darwin/arm64 linux/arm64 linux/amd64

release: clean
	@mkdir -p dist
	@for platform in $(PLATFORMS); do \
		os=$$(echo $$platform | cut -d/ -f1); \
		arch=$$(echo $$platform | cut -d/ -f2); \
		name=$(BINARY)_$(VERSION)_$${os}_$${arch}; \
		echo "Building $$name..."; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o dist/$$name/$(BINARY) ./cmd/md/ || exit 1; \
		tar -czf dist/$$name.tar.gz -C dist $$name; \
		rm -rf dist/$$name; \
	done
	@echo "Release archives in dist/"
	@ls -la dist/
