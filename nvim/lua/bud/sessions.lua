-- Per-project sessions, when dashboard.sessions is on (D37): quitting saves
-- the open files of a registered project, and opening that project again from
-- the dashboard or <leader>p restores them.

local config = require("bud.config")
local projects = require("bud.projects")

local M = {}

-- tests point this elsewhere
M.dir = vim.fn.stdpath("state") .. "/sessions"

function M.enabled()
  return config.get("dashboard.sessions", false) == true
end

-- file is where the session of the project at path lives
function M.file(path)
  path = vim.uv.fs_realpath(path) or path
  return M.dir .. "/" .. path:gsub("[/\\:]", "%%") .. ".vim"
end

-- save writes the session of the project the cwd is in, when sessions are on
-- and there are files open. Run on VimLeavePre
function M.save()
  if not M.enabled() then
    return
  end
  local project = projects.find(vim.fn.getcwd())
  if not project then
    return
  end
  local has_files = vim.iter(vim.api.nvim_list_bufs()):any(function(b)
    return vim.bo[b].buflisted and vim.bo[b].buftype == "" and vim.api.nvim_buf_get_name(b) ~= ""
  end)
  if not has_files then
    return
  end

  local file = M.file(project.path)
  vim.fn.mkdir(vim.fs.dirname(file), "p")
  local saved = vim.o.sessionoptions
  vim.o.sessionoptions = "buffers,curdir,folds,tabpages,winsize"
  pcall(vim.cmd.mksession, { vim.fn.fnameescape(file), bang = true })
  vim.o.sessionoptions = saved
end

-- restore loads the project's session, and reports whether it did: false when
-- sessions are off, there's none, or it failed (which is reported)
function M.restore(path)
  local file = M.file(path)
  if not M.enabled() or vim.fn.filereadable(file) == 0 then
    return false
  end
  local ok, err = pcall(vim.cmd.source, vim.fn.fnameescape(file))
  if not ok then
    vim.notify("bud: restoring the session failed: " .. tostring(err), vim.log.levels.WARN)
  end
  return ok
end

return M
