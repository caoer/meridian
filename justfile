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

# Generate a benchmark corpus: just perf-gen /tmp/corpus 10000
perf-gen out docs="10000" seed="1":
    go run ./test/perf/gen -out {{out}} -docs {{docs}} -seed {{seed}} -rules ./rules

# Home-wiki-shaped ×N corpus (default ×10≈38k docs): 3-pack rules + effect pins + ~17 fixture git repos + .runs.md sidecars; prints `export CCC_LLM_WIKI_REPOS_ROOT=…` (eval before check). Usage: just perf-gen-wiki /tmp/wiki10x 10 1
perf-gen-wiki out mult="10" seed="1":
    go run ./test/perf/gen -profile wiki -out {{out}} -mult {{mult}} -seed {{seed}}

# Time md check over a corpus (min of 3 runs, prints ms)
perf corpus: build
    ./test/perf/bench.sh ./md {{corpus}}
