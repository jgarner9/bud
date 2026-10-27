-- Run by scripts/lang-check.sh inside headless Neovim, after bud update has
-- installed every enabled language's parsers and Mason packages. Each case writes
-- a sample project, opens a file in it and checks what the language spec wires
-- up. Quits with exit code 1 and the problems on stderr when anything is wrong.

local cases = {
  {
    lang = "go",
    files = {
      ["go.mod"] = "module example.com/sample\n\ngo 1.22\n",
      ["main.go"] = 'package main\n\nimport "os"\n\nfunc main() {\nos.Remove("x")\n}\n',
    },
    open = "main.go",
    servers = { "gopls" },
    -- golangci-lint reports the linter that found it: os.Remove's error is dropped
    lint = "errcheck",
    formatted = { 6, '\tos.Remove("x")' },
  },
  {
    lang = "python",
    files = {
      ["pyproject.toml"] = '[project]\nname = "sample"\nversion = "0"\n',
      ["main.py"] = "import os\nx=1\n",
    },
    open = "main.py",
    servers = { "basedpyright", "ruff" },
    -- F401: os is never used
    lint = "Ruff",
    -- ruff puts a blank line after the imports
    formatted = { 3, "x = 1" },
  },
  {
    lang = "typescript",
    name = "typescript with prettier",
    files = {
      ["package.json"] = '{ "name": "sample", "private": true }\n',
      ["tsconfig.json"] = '{ "compilerOptions": { "strict": true } }\n',
      [".prettierrc"] = "{}\n",
      ["index.ts"] = "const x:number=1\nconst y: string = x\nexport { y }\n",
    },
    open = "index.ts",
    servers = { "tsc" },
    -- y gets a number
    lint = "",
    formatted = { 1, "const x: number = 1;" },
  },
  {
    lang = "typescript",
    name = "typescript without prettier",
    files = {
      ["package.json"] = '{ "name": "sample", "private": true }\n',
      ["index.ts"] = "const x:number=1\nexport { x }\n",
    },
    open = "index.ts",
    servers = { "tsc" },
    -- prettier needs a config (require_cwd), so tsc formats, without adding a semicolon
    formatted = { 1, "const x: number = 1" },
  },
  {
    lang = "c",
    name = "c with .clang-format",
    files = {
      [".clang-format"] = "BasedOnStyle: LLVM\n",
      ["main.c"] = "int main(void){return y;}\n",
    },
    open = "main.c",
    servers = { "clangd" },
    -- y is undeclared
    lint = "clang",
    formatted = { 1, "int main(void) { return y; }" },
  },
  {
    lang = "c",
    name = "c without .clang-format",
    files = { ["main.c"] = "int main(void){return 0;}\n" },
    open = "main.c",
    servers = { "clangd" },
    -- no style file, so saving leaves it alone (format_markers)
    formatted = { 1, "int main(void){return 0;}" },
  },
  {
    lang = "bash",
    files = { ["run.sh"] = "#!/bin/sh\nif true;then\necho $1\nfi\n" },
    open = "run.sh",
    servers = {},
    -- SC2086: $1 isn't quoted
    lint = "shellcheck",
    formatted = { 2, "if true; then" },
  },
  {
    lang = "lua",
    files = {
      ["stylua.toml"] = 'indent_type = "Spaces"\nindent_width = 2\n',
      ["selene.toml"] = 'std = "lua51"\n',
      ["init.lua"] = "local x=1\nreturn y\n",
    },
    open = "init.lua",
    servers = { "lua_ls" },
    -- y is undefined
    lint = "selene",
    formatted = { 1, "local x = 1" },
  },
  {
    lang = "html",
    files = {
      ["package.json"] = '{ "name": "sample", "private": true }\n',
      [".prettierrc"] = "{}\n",
      ["index.html"] = "<a href='x'>y</a>\n",
    },
    open = "index.html",
    servers = { "html" },
    formatted = { 1, '<a href="x">y</a>' },
  },
  {
    lang = "css",
    files = { ["style.css"] = "a{colr:red}\n" },
    open = "style.css",
    servers = { "cssls" },
    -- unknown property colr
    lint = "css",
    -- no Prettier config, so the server formats
    formatted = { 1, "a {" },
  },
}

local problems = {}
local function problem(case, msg)
  table.insert(problems, ("%s: %s"):format(case.name or case.lang, msg))
end

local only = vim.split(vim.env.BUD_LANG_CHECK_ONLY or "", " ", { trimempty = true })
local lang = require("bud.lang")
-- mason only loads for its UI
require("bud.pack").load("mason.nvim")
local registry = require("mason-registry")

for _, name in ipairs(lang.failed) do
  table.insert(problems, "language spec failed to load: " .. name)
end

local function check(i, case)
  local spec = lang.specs()[case.lang]
  if not spec then
    problem(case, "no spec")
    return
  end

  for _, parser in ipairs(spec.treesitter or {}) do
    if not pcall(vim.treesitter.language.add, parser) then
      problem(case, "parser not installed: " .. parser)
    end
  end
  for _, pkg in ipairs(spec.mason or {}) do
    if not registry.is_installed(pkg) then
      problem(case, "Mason package not installed: " .. pkg)
    end
  end

  local dir = ("%s/%d-%s"):format(vim.env.BUD_LANG_CHECK_DIR, i, case.lang)
  for path, text in pairs(case.files) do
    vim.fn.mkdir(vim.fs.dirname(dir .. "/" .. path), "p")
    vim.fn.writefile(vim.split(text, "\n", { plain = true }), dir .. "/" .. path, "b")
  end
  vim.cmd.cd(dir)
  vim.cmd.edit(case.open)
  local buf = vim.api.nvim_get_current_buf()

  -- highlighting starts just after the buffer shows (D35)
  if not vim.wait(5000, function()
    return vim.treesitter.highlighter.active[buf] ~= nil
  end, 50) then
    problem(case, "tree-sitter highlighting didn't start")
  end

  local function attached()
    local names = {}
    for _, client in ipairs(vim.lsp.get_clients({ bufnr = buf })) do
      names[client.name] = true
    end
    return names
  end
  vim.wait(30000, function()
    local names = attached()
    return vim.iter(case.servers):all(function(s)
      return names[s]
    end)
  end, 200)
  for _, server in ipairs(case.servers) do
    if not attached()[server] then
      problem(case, "server didn't attach: " .. server)
    end
  end

  if case.lint then
    -- an empty pattern takes any diagnostic, for a server whose source varies
    local function found()
      return vim.iter(vim.diagnostic.get(buf)):any(function(d)
        return (d.source or ""):find(case.lint, 1, true) ~= nil
      end)
    end
    if not vim.wait(60000, found, 200) then
      problem(case, ("no diagnostic from %q; got %s"):format(case.lint, vim.inspect(vim.diagnostic.get(buf))))
    end
  end

  vim.cmd.write()
  local line = vim.api.nvim_buf_get_lines(buf, case.formatted[1] - 1, case.formatted[1], false)[1]
  if line ~= case.formatted[2] then
    problem(case, ("format on save: line %d is %q, want %q"):format(case.formatted[1], line, case.formatted[2]))
  end

  vim.cmd("silent! %bwipeout!")
  print(("lang-check: %s done"):format(case.name or case.lang))
end

for i, case in ipairs(cases) do
  if #only == 0 or vim.tbl_contains(only, case.lang) then
    -- an error in one case is a problem, not a headless Neovim left waiting
    local ok, err = xpcall(check, debug.traceback, i, case)
    if not ok then
      problem(case, err)
    end
  end
end

if #problems > 0 then
  -- selene's Lua 5.1 definitions leave out file methods on io.stderr
  -- selene: allow(incorrect_standard_library_use)
  io.stderr:write("\n" .. table.concat(problems, "\n") .. "\n")
  vim.cmd("cquit 1")
end
print("lang-check: ok")
vim.cmd("qall!")
