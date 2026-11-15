-- Python (D10). ty checks types (D68: a Rust binary of ~30 MB, where basedpyright
-- brought its own Node and took ~290 MB); ruff's server lints (with fixes as code
-- actions), and ruff formats and sorts imports on write
return {
  filetypes = { "python" },
  treesitter = { "python" },
  lsp = {
    ty = {},
    ruff = {},
  },
  format = { "ruff_organize_imports", "ruff_format" },
  mason = { "ty", "ruff" },
  -- Mason installs ty and ruff from PyPI, into a venv
  system = { "python3" },
  root = { "pyproject.toml", "ty.toml", "setup.py", "setup.cfg", "requirements.txt", "Pipfile", ".git" },
  minimal = true,
  dap = "debugpy",
}
