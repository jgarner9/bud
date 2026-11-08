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

# format Go code in place, with the gofmt of the Go toolchain go.mod selects (what CI runs), not the one on PATH
fmt:
  "$(go env GOROOT)/bin/gofmt" -w .

# fail if any Go file needs formatting, by that same gofmt
fmt-check:
  @gofmt="$(go env GOROOT)/bin/gofmt"; test -z "$("$gofmt" -l .)" || { "$gofmt" -l .; exit 1; }

# format Lua in place
lua-fmt: (_need "stylua")
  stylua nvim scripts tests languages layouts migrations tools

# stylua check and selene lint over the Lua config
lua-lint: (_need "stylua") (_need "selene")
  stylua --check nvim scripts tests languages layouts migrations tools
  selene nvim scripts tests languages layouts migrations tools

# go vet, gofmt check, Lua lint and shellcheck
lint: fmt-check lua-lint
  go vet ./...
  shellcheck install.sh scripts/*.sh

# run the headless Lua tests in tests/lua (or the files given), 60 s each
lua-test *tests:
  sh scripts/lua-test.sh "$@"

# rotate-release-key.sh against a throwaway repo, with fake op and gh
rotate-test:
  sh scripts/rotate_test.sh

# release-plan.sh's decisions for each way the release workflow starts, with a fake gh
release-plan-test:
  sh scripts/release_plan_test.sh

# sync the config into a throwaway dir, restore plugins, start nvim headless (needs network)
smoke:
  sh scripts/smoke.sh

# install.sh's checksum and signature checks against fake signed releases
install-test:
  sh scripts/install-test.sh </dev/null

# install every language's tooling into a throwaway dir and check it on sample
# projects, e.g. just lang-check go (needs network, tree-sitter, node, python3)
lang-check *langs:
  sh scripts/lang-check.sh "$@"

# everything CI checks (lint + tests + smoke)
check: lint test lua-test install-test rotate-test release-plan-test smoke

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
