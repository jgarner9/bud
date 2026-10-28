-- Trimming Mason's packages after bud update installs them (JGA-121). Each
-- step is idempotent, since every restore runs them over everything installed:
--
-- prune deletes files a package ships but never uses, dedupe hard-links the
-- identical files of packages built from one source (css-lsp, html-lsp and
-- eslint-lsp are all vscode-langservers-extracted), and orphans names the
-- packages a disabled language left behind.

local M = {}

-- globs under a package's dir that it never needs at run time
M.prune = {
  -- compiler-rt's static libraries (~190 MB), for linking sanitizers and
  -- fuzzers; clangd only reads the builtin headers in lib/clang/*/include
  clangd = { "*/lib/clang/*/lib" },
  -- source maps (~28 MB), only read by node --enable-source-maps
  basedpyright = { "**/*.map" },
}

-- prune_package deletes what M.prune lists for one installed package, and
-- returns the paths it removed
function M.prune_package(name, dir)
  local removed = {}
  for _, pattern in ipairs(M.prune[name] or {}) do
    for _, path in ipairs(vim.fn.glob(dir .. "/" .. pattern, true, true)) do
      if vim.fn.delete(path, "rf") == 0 then
        table.insert(removed, path)
      end
    end
  end
  return removed
end

-- source reads the id of what a package was built from out of its Mason
-- receipt (pkg:npm/vscode-langservers-extracted@4.10.0), or nil
local function source(dir)
  local f = io.open(dir .. "/mason-receipt.json")
  if not f then
    return nil
  end
  local ok, receipt = pcall(vim.json.decode, f:read("*a"))
  f:close()
  return ok and type(receipt) == "table" and type(receipt.source) == "table" and receipt.source.id or nil
end

local function same_content(a, b)
  local fa, fb = io.open(a, "rb"), io.open(b, "rb")
  local same = fa ~= nil and fb ~= nil
  while same do
    local ca, cb = fa:read(1048576), fb:read(1048576)
    same = ca == cb
    if ca == nil then
      break
    end
  end
  if fa then
    fa:close()
  end
  if fb then
    fb:close()
  end
  return same
end

-- link_same replaces each file under dir that matches the one at the same
-- path under canon (size, mode, content) with a hard link to it, and returns
-- how many bytes that freed
local function link_same(canon, dir)
  local freed = 0
  for rel, kind in vim.fs.dir(dir, { depth = math.huge }) do
    if kind == "file" then
      local path, other = dir .. "/" .. rel, canon .. "/" .. rel
      local st, ost = vim.uv.fs_lstat(path), vim.uv.fs_lstat(other)
      if
        st
        and ost
        and ost.type == "file"
        and st.ino ~= ost.ino
        and st.size == ost.size
        and st.mode == ost.mode
        and same_content(path, other)
      then
        local tmp = path .. ".bud-link"
        if vim.uv.fs_link(other, tmp) then
          if vim.uv.fs_rename(tmp, path) then
            freed = freed + st.size
          else
            vim.uv.fs_unlink(tmp)
          end
        end
      end
    end
  end
  return freed
end

-- dedupe hard-links identical files across the packages under root that share
-- a source, onto the first by name, and returns the bytes freed. Mason
-- replaces a package's whole dir to update it, which leaves the others' links
-- intact
function M.dedupe(root)
  local groups = {}
  if vim.fn.isdirectory(root) == 0 then
    return 0
  end
  for name, kind in vim.fs.dir(root) do
    local id = kind == "directory" and source(root .. "/" .. name)
    if id then
      groups[id] = groups[id] or {}
      table.insert(groups[id], name)
    end
  end
  local freed = 0
  for _, names in pairs(groups) do
    table.sort(names)
    for i = 2, #names do
      freed = freed + link_same(root .. "/" .. names[1], root .. "/" .. names[i])
    end
  end
  return freed
end

-- orphans returns the installed packages some spec names (known) that no
-- enabled spec wants, sorted. Ones no spec names were installed by hand
-- (:Mason) and are left alone
function M.orphans(installed, wanted, known)
  local out = {}
  for _, name in ipairs(installed) do
    if vim.tbl_contains(known, name) and not vim.tbl_contains(wanted, name) then
      table.insert(out, name)
    end
  end
  table.sort(out)
  return out
end

return M
