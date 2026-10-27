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

# format Lua in place
lua-fmt: (_need "stylua")
  stylua nvim scripts

# stylua check and selene lint over the Lua config
lua-lint: (_need "stylua") (_need "selene")
  stylua --check nvim scripts
  selene nvim scripts

# go vet, gofmt check, Lua lint and shellcheck
lint: fmt-check lua-lint
  go vet ./...
  shellcheck install.sh scripts/smoke.sh

# sync the config into a throwaway dir, restore plugins, start nvim headless (needs network)
smoke:
  sh scripts/smoke.sh

# everything CI checks (lint + tests + smoke)
check: lint test smoke

# fail with an install hint when a tool isn't on PATH
[private]
_need tool:
  @command -v {{tool}} >/dev/null || { echo "{{tool}} is not installed: brew install {{tool}} (or see its GitHub releases)"; exit 1; }

# go mod tidy
tidy:
  go mod tidy

# local GoReleaser build into dist/, nothing published
snapshot:
  goreleaser release --snapshot --clean --skip=publish

# remove build outputs
clean:
  rm -rf bud dist
