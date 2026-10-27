-- A language spec: copy this file to languages/<name>.lua and fill it in. The
-- file name is the language's name, the one config.json's "languages" list and
-- `bud lang` use. Files starting with _ are never loaded.
--
-- A spec is data, not code: bud.lang wires it up, and the Go CLI reads the same
-- file (BASE.md 2). Every field but filetypes is optional, and a field bud
-- doesn't know is an error, so a typo can't slip by.
return {
  -- Neovim filetypes this language covers (:set filetype? in a buffer shows one)
  filetypes = { "lang" },

  -- nvim-treesitter parsers, built when bud install / bud update runs. bud
  -- starts tree-sitter highlighting for the filetypes above
  treesitter = { "lang" },

  -- LSP servers by nvim-lspconfig name, each with settings merged over
  -- nvim-lspconfig's (see :help vim.lsp.config). {} keeps its defaults
  lsp = {
    lang_ls = {},
  },

  -- nvim-lint linter names, run on open, on write and on leaving insert mode.
  -- A list covers every filetype above; a table by filetype, like
  -- { lang = { "lang-lint" } }, covers only the ones it names
  lint = { "lang-lint" },

  -- conform.nvim formatter names, run in order on write unless config.json sets
  -- "format_on_save": false, and by hand with <leader>cf. A list or a table by
  -- filetype, like lint. A filetype with none gets its LSP server's formatting
  format = { lang = { "lang-fmt" } },

  -- format on save only in projects that have one of these files (looked for
  -- from the file's directory up), for languages whose projects keep their own
  -- style. Leave out to always format; <leader>cf formats either way
  format_markers = { ".lang-fmt.toml" },

  -- Mason packages: the servers, linters and formatters above, installed when
  -- bud install / bud update runs. Mason names can differ from the ones above
  mason = { "lang-language-server", "lang-lint", "lang-fmt" },

  -- binaries the language needs from the system package manager (a compiler, a
  -- runtime), which bud install makes sure exist
  system = { "lang" },

  -- files that mark a project root, nearest first, for finding the project a
  -- file belongs to when bud opens it
  root = { "lang.toml", ".git" },

  -- true puts this language in the minimal profile's preset, used when
  -- config.json has no "languages" list
  minimal = false,

  -- the debug adapter's Mason name. Debugging isn't wired up yet (JGA-108)
  dap = nil,
}
