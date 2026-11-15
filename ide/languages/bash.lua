-- Bash and POSIX sh (D10). No language server: bash-language-server needs node,
-- which the minimal profile leaves out, so shellcheck lints and shfmt formats
-- (with the project's .editorconfig when it has one)
return {
  filetypes = { "sh", "bash" },
  treesitter = { "bash" },
  lint = { "shellcheck" },
  format = { "shfmt" },
  mason = { "shellcheck", "shfmt" },
  root = { ".git" },
  minimal = true,
}
