-- CSS, SCSS and Less (D10). The server is VS Code's, from
-- vscode-langservers-extracted (see html.lua), and also lints. Prettier formats
-- in projects with a Prettier config (bud.plugins.lang), and the server formats the rest
return {
  filetypes = { "css", "scss", "less" },
  -- nvim-treesitter has no Less parser, so Less keeps regex highlighting
  treesitter = { "css", "scss" },
  lsp = {
    cssls = {},
  },
  format = { "prettier" },
  mason = { "css-lsp", "prettier" },
  system = { "node" },
  root = { "package.json", ".git" },
}
