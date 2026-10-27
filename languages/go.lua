-- Go (D10). gopls also formats go.mod and go.work, which have no formatter here
return {
  filetypes = { "go", "gomod", "gowork", "gosum" },
  treesitter = { "go", "gomod", "gowork", "gosum" },
  lsp = {
    gopls = {},
  },
  -- with no .golangci.yml, golangci-lint runs its standard set (govet, staticcheck, ...)
  lint = { go = { "golangcilint" } },
  -- gofmt plus import fixing, so the output passes `gofmt -l`
  format = { go = { "goimports" } },
  mason = { "gopls", "golangci-lint", "goimports" },
  -- Mason builds gopls and goimports with the go toolchain
  system = { "go" },
  root = { "go.work", "go.mod", ".git" },
}
