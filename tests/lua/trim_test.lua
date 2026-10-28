-- Run with: nvim --clean --headless -l tests/lua/trim_test.lua (just lua-test)

vim.opt.rtp:prepend(vim.fn.getcwd() .. "/nvim")

local trim = require("bud.lang.trim")
local root = vim.fn.tempname()

local function eq(got, want, what)
  if not vim.deep_equal(got, want) then
    error(("%s: got %s, want %s"):format(what, vim.inspect(got), vim.inspect(want)), 2)
  end
end

local function write(path, text)
  vim.fn.mkdir(vim.fs.dirname(path), "p")
  local f = assert(io.open(path, "wb"))
  f:write(text)
  f:close()
end

local function exists(path)
  return vim.uv.fs_lstat(path) ~= nil
end

local function receipt(dir, id)
  write(dir .. "/mason-receipt.json", vim.json.encode({ name = vim.fs.basename(dir), source = { id = id } }))
end

-- prune: clangd keeps its headers and loses compiler-rt
local clangd = root .. "/clangd"
write(clangd .. "/clangd_23/bin/clangd", "bin")
write(clangd .. "/clangd_23/lib/clang/23/include/stddef.h", "h")
write(clangd .. "/clangd_23/lib/clang/23/lib/darwin/libclang_rt.fuzzer_osx.a", "a")
eq(#trim.prune_package("clangd", clangd), 1, "clangd paths removed")
assert(exists(clangd .. "/clangd_23/bin/clangd"), "clangd binary removed")
assert(exists(clangd .. "/clangd_23/lib/clang/23/include/stddef.h"), "builtin headers removed")
assert(not exists(clangd .. "/clangd_23/lib/clang/23/lib"), "compiler-rt kept")
eq(trim.prune_package("clangd", clangd), {}, "second prune")

-- prune: basedpyright's source maps, at any depth
local pyright = root .. "/basedpyright"
write(pyright .. "/venv/lib/dist/pyright.js", "js")
write(pyright .. "/venv/lib/dist/pyright.js.map", "map")
trim.prune_package("basedpyright", pyright)
assert(exists(pyright .. "/venv/lib/dist/pyright.js"), "pyright.js removed")
assert(not exists(pyright .. "/venv/lib/dist/pyright.js.map"), "source map kept")

-- a package with no rules is untouched
write(root .. "/gopls/gopls", "x")
eq(trim.prune_package("gopls", root .. "/gopls"), {}, "gopls prune")

-- dedupe: same source, identical files get one inode; different ones don't
local id = "pkg:npm/vscode-langservers-extracted@4.10.0"
for _, name in ipairs({ "css-lsp", "html-lsp", "eslint-lsp" }) do
  local dir = root .. "/" .. name
  receipt(dir, id)
  write(dir .. "/node_modules/x/server.js", ("s"):rep(5000))
  write(dir .. "/node_modules/.package-lock.json", name)
end
write(root .. "/eslint-lsp/node_modules/x/only.js", "only")
-- same bytes, other source: never linked
receipt(root .. "/prettier", "pkg:npm/prettier@3")
write(root .. "/prettier/node_modules/x/server.js", ("s"):rep(5000))

local freed = trim.dedupe(root)
-- css-lsp sorts first, so html-lsp and eslint-lsp link onto it
eq(freed, 2 * 5000, "bytes freed")
local function ino(path)
  return vim.uv.fs_stat(path).ino
end
local canon = ino(root .. "/css-lsp/node_modules/x/server.js")
eq(ino(root .. "/html-lsp/node_modules/x/server.js"), canon, "html-lsp linked")
eq(ino(root .. "/eslint-lsp/node_modules/x/server.js"), canon, "eslint-lsp linked")
assert(ino(root .. "/prettier/node_modules/x/server.js") ~= canon, "other source linked")
assert(
  ino(root .. "/html-lsp/node_modules/.package-lock.json") ~= ino(root .. "/css-lsp/node_modules/.package-lock.json"),
  "differing files linked"
)
eq(vim.fn.readfile(root .. "/html-lsp/node_modules/.package-lock.json"), { "html-lsp" }, "lockfile content")
eq(trim.dedupe(root), 0, "second dedupe")

-- removing one package (as Mason does to update it) leaves the others whole
vim.fn.delete(root .. "/css-lsp", "rf")
eq(vim.fn.readfile(root .. "/html-lsp/node_modules/x/server.js"), { ("s"):rep(5000) }, "after css-lsp removed")

eq(trim.dedupe(root .. "/missing"), 0, "missing dir")

-- orphans: known but unwanted, never hand-installed ones
eq(
  trim.orphans(
    { "clangd", "gopls", "prettier", "my-thing" },
    { "gopls", "prettier" },
    { "clangd", "gopls", "prettier" }
  ),
  { "clangd" },
  "orphans"
)
eq(trim.orphans({}, {}, { "clangd" }), {}, "nothing installed")

vim.fn.delete(root, "rf")
print("trim_test: ok")
