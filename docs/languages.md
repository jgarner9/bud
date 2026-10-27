# Adding a language

A language is one Lua file, a spec, that says which tree-sitter parsers, LSP servers, linters and formatters it uses. bud wires them up; you don't write plugin config.

## The quick way

```sh
bud lang add zig        # copies the template to ~/.local/share/bud/user-tools/languages/zig.lua
$EDITOR ~/.local/share/bud/user-tools/languages/zig.lua
bud lang enable zig     # installs its tools and turns it on
```

Or ask Claude: `:BudGen "add Zig support"` drafts the spec and shows it to you as a diff first.

## The spec

Every field but `filetypes` is optional, and a field bud doesn't know is an error, so a typo can't slip by. The template, `languages/_template.lua`, explains each one:

- `filetypes`: the Neovim filetypes it covers.
- `treesitter`: parsers to build.
- `lsp`: servers by nvim-lspconfig name, with settings.
- `lint` and `format`: nvim-lint and conform.nvim names, a list or a table by filetype.
- `format_markers`: format on save only in projects with one of these files.
- `mason`: the Mason packages that provide the above.
- `system`: binaries it needs on PATH (`go`, `python3`, `node`), which `bud install` and `bud lang enable` install through your package manager.
- `root`: files that mark a project root, ahead of `.git`.
- `minimal = true`: part of the minimal profile's preset.

`bud lang ls` shows every spec, which are on, and why a broken one is broken. `bud doctor` checks them too.

## Shipping it with bud

Your specs stay on your machine. To ship one with bud, put it in a checkout's `languages/`, add a case for it to `scripts/lang_check.lua`, and run `just lang-check zig`, which installs its tooling in a throwaway dir and checks it on a sample project.
