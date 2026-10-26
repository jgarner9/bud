# forward recipe args as "$@" so quoting survives (just run open "my dir")
set positional-arguments

# list recipes
default:
  @just --list --unsorted

# run bud from source, e.g. just run update
run *args:
  go run ./cmd/bud "$@"

# build ./bud for this machine
build:
  go build -trimpath -o bud ./cmd/bud

# install bud into $GOBIN (or ~/go/bin)
install:
  go install ./cmd/bud

# run tests, e.g. just test -run TestOpen ./internal/cli
test *args:
  if [ $# -eq 0 ]; then set -- ./...; fi; go test "$@"

# format Go code in place
fmt:
  gofmt -w .

# fail if any Go file needs formatting
fmt-check:
  @test -z "$(gofmt -l .)" || { gofmt -l .; exit 1; }

# go vet, gofmt check and shellcheck
lint: fmt-check
  go vet ./...
  shellcheck install.sh

# everything CI checks (lint + tests)
check: lint test

# go mod tidy
tidy:
  go mod tidy

# local GoReleaser build into dist/, nothing published
snapshot:
  goreleaser release --snapshot --clean --skip=publish

# remove build outputs
clean:
  rm -rf bud dist
