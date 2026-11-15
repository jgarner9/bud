-- TypeScript and JavaScript (D10). tsc is TypeScript 7's native language server;
-- ESLint's server only attaches in projects with an ESLint config. Prettier only
-- formats in projects with a Prettier config (bud.plugins.lang), and tsc formats
-- the rest
return {
  filetypes = { "typescript", "typescriptreact", "javascript", "javascriptreact" },
  treesitter = { "typescript", "tsx", "javascript", "jsdoc" },
  lsp = {
    tsc = {},
    -- lint only: formatting is prettier's or tsc's
    eslint = { settings = { format = false } },
  },
  format = { "prettier" },
  mason = { "tsc", "eslint-lsp", "prettier" },
  -- Mason installs all three from npm
  system = { "node" },
  root = { "tsconfig.json", "jsconfig.json", "package.json", ".git" },
  dap = "js-debug-adapter",
}
