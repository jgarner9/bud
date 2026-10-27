-- Keeps cocoindex-code's semantic index warm (JGA-119), so Claude's first
-- search in a project doesn't wait for it to be built. Once per project per
-- Neovim, after the project's session starts, `ccc index` runs in the
-- background at low priority (nice); it's incremental, so an unchanged
-- project costs little. Only for projects already set up with `ccc init`:
-- indexing a new one would create .cocoindex_code/ and edit its .gitignore,
-- which bud doesn't do to a repo unasked. Nothing waits on it.

local M = {}

local runs = {} -- root -> "indexing" | "done" | "failed"

-- swappable in tests
M.bin = function()
  return vim.fn.exepath("ccc") ~= "" and vim.fn.exepath("ccc") or nil
end
M.nice = function()
  return vim.fn.exepath("nice") ~= "" and { "nice", "-n", "10" } or {}
end

-- set_up says whether root has been through ccc init
function M.set_up(root)
  return vim.fn.isdirectory(root .. "/.cocoindex_code") == 1
end

-- warm starts the index update for root, once. It returns whether it did
function M.warm(root)
  local bin = M.bin()
  if not bin or runs[root] or not M.set_up(root) then
    return false
  end
  runs[root] = "indexing"
  vim.cmd.redrawstatus({ bang = true })
  local argv = vim.list_extend(M.nice(), { bin, "index" })
  local ok = pcall(vim.system, argv, { cwd = root, text = true }, function(res)
    vim.schedule(function()
      runs[root] = res.code == 0 and "done" or "failed"
      vim.cmd.redrawstatus({ bang = true })
    end)
  end)
  if not ok then
    runs[root] = "failed"
  end
  return ok
end

-- state is root's index state, or nil when bud hasn't touched it
function M.state(root)
  return runs[root]
end

-- statusline shows the index only while it's building
function M.statusline()
  local root = vim.uv.fs_realpath(vim.fn.getcwd()) or vim.fn.getcwd()
  return runs[root] == "indexing" and (require("bud.icons").search .. " indexing") or ""
end

function M._reset()
  runs = {}
end

return M
