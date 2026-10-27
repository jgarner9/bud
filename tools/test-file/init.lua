-- test-file: jump between a file and its test, by each language's naming
-- (x.go <-> x_test.go, x.py <-> test_x.py, x.ts <-> x.test.ts). A small tool
-- bud ships, and an example of the tool contract: init.lua returns a table
-- whose setup() bud.tools calls once when the tool is enabled.

local M = {}

-- pairs maps a file name to its counterpart: { pattern, replacement } both ways
M.rules = {
  { "^(.+)_test%.go$", "%1.go" },
  { "^(.+)%.go$", "%1_test.go" },
  { "^test_(.+)%.py$", "%1.py" },
  { "^(.+)%.py$", "test_%1.py" },
  { "^(.+)%.test%.([jt]sx?)$", "%1.%2" },
  { "^(.+)%.spec%.([jt]sx?)$", "%1.%2" },
  { "^(.+)%.([jt]sx?)$", "%1.test.%2" },
}

-- counterpart is the other file's name, or nil when no rule fits
function M.counterpart(name)
  for _, rule in ipairs(M.rules) do
    if name:match(rule[1]) then
      return (name:gsub(rule[1], rule[2]))
    end
  end
end

-- find looks for the counterpart beside the file, then anywhere under the
-- project (Python's tests/ dir), and returns its path, or where to make it
function M.find(path)
  local dir, name = vim.fs.dirname(path), vim.fs.basename(path)
  local other = M.counterpart(name)
  if not other then
    return nil
  end
  local beside = dir .. "/" .. other
  if vim.uv.fs_stat(beside) then
    return beside
  end
  local root = vim.fs.root(path, ".git") or dir
  local found = vim.fs.find(other, { path = root, type = "file", limit = 1 })[1]
  return found or beside
end

function M.setup()
  vim.api.nvim_create_user_command("BudTestFile", function()
    local path = vim.api.nvim_buf_get_name(0)
    local target = path ~= "" and M.find(path)
    if not target then
      vim.notify("test-file: no test naming rule for " .. vim.fs.basename(path), vim.log.levels.INFO)
      return
    end
    vim.cmd.edit(vim.fn.fnameescape(target))
  end, { desc = "Jump between a file and its test" })
end

return M
