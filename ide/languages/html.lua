-- HTML (D10). The server is VS Code's, from vscode-langservers-extracted: its
-- last release is from 2024, but it wraps Microsoft's maintained HTML service.
-- Prettier formats in projects with a Prettier config (bud.plugins.lang), and
-- the server formats the rest
return {
  filetypes = { "html" },
  -- javascript and css for the <script> and <style> blocks
  treesitter = { "html", "javascript", "css" },
  lsp = {
    html = {},
  },
  format = { "prettier" },
  mason = { "html-lsp", "prettier" },
  system = { "node" },
  root = { "package.json", ".git" },
}
