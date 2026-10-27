-- Python (D10). basedpyright checks types; ruff's server lints (with fixes as
-- code actions), and ruff formats and sorts imports on write
return {
  filetypes = { "python" },
  treesitter = { "python" },
  lsp = {
    basedpyright = {
      settings = {
        basedpyright = {
          -- ruff sorts imports
          disableOrganizeImports = true,
          -- basedpyright's own default, "recommended", flags too much in code
          -- that wasn't written for it; a project's pyproject.toml still wins
          analysis = { typeCheckingMode = "standard" },
        },
      },
    },
    ruff = {},
  },
  format = { "ruff_organize_imports", "ruff_format" },
  mason = { "basedpyright", "ruff" },
  -- Mason installs basedpyright from PyPI, into a venv
  system = { "python3" },
  root = { "pyproject.toml", "setup.py", "setup.cfg", "requirements.txt", "Pipfile", ".git" },
  minimal = true,
}
