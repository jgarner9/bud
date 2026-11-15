-- Quitting with bud's terminals open. Neovim refuses :wqa and :xa (E948)
-- while any terminal still runs a program, and bud keeps three alive: the
-- <leader>t shell, Claude Code's pane and chat. So just before Neovim exits
-- (ExitPre), bud ends Claude Code's pane and chat, whose conversations Claude
-- Code has already saved, and the shell only when it's idle: a shell running
-- something (a server, a build) is left, so the quit stops and says so.
--
-- ExitPre comes before Neovim checks for unsaved files, so with unsaved
-- files bud ends nothing unless the quit is going to save or drop them (:wqa,
-- :xa, a !, or the keys below); a :qa that's about to fail leaves them be.
--
--   <leader>qq  quit, asking first about unsaved files (the dashboard's q)
--   <leader>qw  save every file and quit
--   <leader>qQ  quit, throwing unsaved changes away

local M = {}

-- what the next quit does with unsaved files, set by the keys: "write" or "drop"
local intent

-- unsaved lists the file buffers with changes
function M.unsaved()
  local bufs = {}
  for _, buf in ipairs(vim.api.nvim_list_bufs()) do
    if vim.bo[buf].buflisted and vim.bo[buf].buftype == "" and vim.bo[buf].modified then
      table.insert(bufs, buf)
    end
  end
  return bufs
end

-- settles says whether the quit under way deals with unsaved files: from the
-- keys, else from the command line it was typed on (:wqa, :xa, :qa!, :exit)
function M.settles(cmdline)
  if intent then
    return true
  end
  cmdline = vim.trim(cmdline or vim.fn.histget(":", -1))
  return cmdline:match("^w") ~= nil
    or cmdline:match("^x") ~= nil
    or cmdline:match("^exi") ~= nil
    or cmdline:find("!", 1, true) ~= nil
end

-- busy names what's running in the terminal job's shell, nil when it's idle
function M.busy(job)
  local ok, pid = pcall(vim.fn.jobpid, job)
  if not ok or pid <= 0 then
    return nil
  end
  local children = vim.api.nvim_get_proc_children(pid)
  if #children == 0 then
    return nil
  end
  local proc = vim.api.nvim_get_proc(children[1])
  return proc and proc.name or "a program"
end

-- ending stops bud's terminals that can go: Claude Code's panes and chat
-- always, the shell when it's idle. It returns the jobs it stopped
function M.ending()
  if #M.unsaved() > 0 and not M.settles() then
    return {}
  end
  local jobs = {}
  for _, name in ipairs({ "bud.ai.term", "bud.ai.chat" }) do
    local mod = package.loaded[name]
    if mod then
      vim.list_extend(jobs, mod.stop_all())
    end
  end
  local terminal = package.loaded["bud.terminal"]
  local shell = terminal and terminal.job()
  -- with a ! Neovim ends it anyway, so there's nothing to warn about
  local forced = intent == "drop" or vim.fn.histget(":", -1):find("!", 1, true) ~= nil
  if shell and not forced then
    local what = M.busy(shell)
    if what then
      vim.notify(("bud: the terminal is still running %s; :qa! ends it"):format(what), vim.log.levels.WARN)
    else
      vim.fn.jobstop(shell)
      table.insert(jobs, shell)
    end
  end
  -- Neovim checks for running jobs right after this
  if #jobs > 0 then
    vim.fn.jobwait(jobs, 1000)
  end
  return jobs
end

-- quit quits, asking first when files are unsaved: save them all, drop the
-- changes, or stay
function M.quit()
  local unsaved = M.unsaved()
  if #unsaved == 0 then
    vim.cmd("qall")
    return
  end
  local names = vim.tbl_map(function(buf)
    return vim.fn.fnamemodify(vim.api.nvim_buf_get_name(buf), ":~:.")
  end, unsaved)
  local choice = vim.fn.confirm(
    "Unsaved changes in " .. table.concat(names, ", "),
    "&Save all and quit\n&Quit without saving\n&Cancel",
    3
  )
  if choice == 1 then
    M.write_quit()
  elseif choice == 2 then
    M.force_quit()
  end
end

-- write_quit saves every file and quits
function M.write_quit()
  intent = "write"
  local ok, err = pcall(vim.cmd, "wqall")
  intent = nil
  if not ok then
    vim.notify("bud: " .. tostring(err):gsub("^Vim:", ""), vim.log.levels.ERROR)
  end
end

-- force_quit quits, throwing unsaved changes away
function M.force_quit()
  intent = "drop"
  vim.cmd("qall!")
end

function M.setup()
  vim.api.nvim_create_autocmd("ExitPre", {
    group = vim.api.nvim_create_augroup("bud.quit", { clear = true }),
    desc = "End bud's terminals so quitting isn't refused (E948)",
    callback = function()
      M.ending()
    end,
  })
  vim.keymap.set("n", "<leader>qq", M.quit, { desc = "Quit (asks about unsaved files)" })
  vim.keymap.set("n", "<leader>qw", M.write_quit, { desc = "Save all and quit" })
  vim.keymap.set("n", "<leader>qQ", M.force_quit, { desc = "Quit without saving" })
end

return M
